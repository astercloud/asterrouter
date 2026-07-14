package controlplane

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRedisAIJobReadyIndexConfiguration(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	defer client.Close()
	if _, err := NewRedisAIJobReadyIndex(nil, RedisAIJobReadyIndexConfig{}); !errors.Is(err, ErrAIJobReadyIndexConfig) {
		t.Fatalf("nil client error=%v", err)
	}
	if _, err := NewRedisAIJobReadyIndex(client, RedisAIJobReadyIndexConfig{Namespace: "invalid namespace"}); !errors.Is(err, ErrAIJobReadyIndexConfig) {
		t.Fatalf("invalid namespace error=%v", err)
	}
}

func TestRedisAIJobReadyIndexContract(t *testing.T) {
	ctx := context.Background()
	index, client := newRedisAIJobReadyIndexTest(t)
	base := time.Now().UTC().Truncate(time.Millisecond)
	principalAOld := readyIndexTestEntry("job-a-old", "principal-a", 1, base.Add(-3*time.Minute))
	principalANew := readyIndexTestEntry("job-a-new", "principal-a", 1, base.Add(-2*time.Minute))
	principalB := readyIndexTestEntry("job-b", "principal-b", 1, base.Add(-time.Minute))
	future := readyIndexTestEntry("job-future", "principal-c", 1, base.Add(time.Minute))
	for _, entry := range []AIJobReadyEntry{principalAOld, principalANew, principalB, future} {
		if err := index.Register(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}
	candidates, err := index.Candidates(ctx, AIJobReadyQuery{ReadyAt: base, Limit: 2})
	if err != nil || len(candidates) != 2 || candidates[0].JobID != principalAOld.JobID || candidates[1].JobID != principalB.JobID {
		t.Fatalf("fair candidates=%+v err=%v", candidates, err)
	}
	assertAIJobReadyCount(t, index, AIJobReadyScope{Level: AIJobReadyScopeAll}, 4)
	assertAIJobReadyCount(t, index, aiJobReadyScopeForEntry(AIJobReadyScopePrincipal, principalAOld), 2)

	newer := principalAOld
	newer.StatusVersion = 2
	newer.FenceToken = 1
	if err := index.Register(ctx, newer); err != nil {
		t.Fatal(err)
	}
	if err := index.Remove(ctx, principalAOld.reference()); err != nil {
		t.Fatal(err)
	}
	current, err := index.Candidates(ctx, AIJobReadyQuery{ReadyAt: base, Limit: 10})
	if err != nil || !containsReadyJobVersion(current, newer.JobID, newer.StatusVersion) {
		t.Fatalf("stale remove deleted newer entry: candidates=%+v err=%v", current, err)
	}
	conflict := newer
	conflict.Priority++
	if err := index.Register(ctx, conflict); !errors.Is(err, ErrAIJobReadyIndexConflict) {
		t.Fatalf("same-version conflict error=%v", err)
	}

	poison := readyIndexTestEntry("job-poison", "principal-poison", 1, base.Add(-time.Minute))
	if err := index.Register(ctx, poison); err != nil {
		t.Fatal(err)
	}
	if err := client.HSet(ctx, index.entriesKey, poison.JobID, strings.Repeat("x", index.maxPayload+1)).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := index.Candidates(ctx, AIJobReadyQuery{ReadyAt: base, Limit: 20}); err != nil {
		t.Fatal(err)
	}
	if exists, err := client.HExists(ctx, index.entriesKey, poison.JobID).Result(); err != nil || exists {
		t.Fatalf("poison entry exists=%t err=%v", exists, err)
	}
}

func TestRedisAIJobReadyIndexCoordinatesConcurrentSchedulers(t *testing.T) {
	ctx := context.Background()
	index, _ := newRedisAIJobReadyIndexTest(t)
	base := time.Now().UTC().Truncate(time.Millisecond)
	svc := newAIJobTestService(t, NewMemoryRepository())
	svc.now = func() time.Time { return base }
	svc.SetAIJobReadyIndex(index)
	for jobIndex := 0; jobIndex < 20; jobIndex++ {
		marker := "redis-scheduler-" + string(rune('a'+jobIndex))
		if _, _, err := svc.BeginDurableAIJob(ctx, aiJobTestAuth("tenant-redis", "principal-"+marker), aiJobTestRequest(marker, marker)); err != nil {
			t.Fatal(err)
		}
	}
	queue, err := NewMemoryAIJobDeliveryQueue(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	queue.now = func() time.Time { return base }
	var claimed atomic.Int32
	var published atomic.Int32
	errorsSeen := make(chan error, 4)
	var wait sync.WaitGroup
	for scheduler := 0; scheduler < 4; scheduler++ {
		wait.Add(1)
		go func(scheduler int) {
			defer wait.Done()
			report, runErr := svc.RunDurableAIJobSchedulerOnce(ctx, "redis-scheduler-"+string(rune('a'+scheduler)), time.Minute, 5, queue)
			if runErr != nil {
				errorsSeen <- runErr
				return
			}
			claimed.Add(int32(report.Claimed))
			published.Add(int32(report.Published))
		}(scheduler)
	}
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Errorf("scheduler: %v", err)
	}
	if claimed.Load() != 20 || published.Load() != 20 {
		t.Fatalf("claimed=%d published=%d", claimed.Load(), published.Load())
	}
	deliveries, err := queue.Receive(ctx, "delivery-verifier", 20, 0)
	if err != nil || len(deliveries) != 20 {
		t.Fatalf("deliveries=%d err=%v", len(deliveries), err)
	}
	seen := make(map[string]bool, len(deliveries))
	for _, delivery := range deliveries {
		if seen[delivery.Envelope.JobID] {
			t.Fatalf("duplicate job delivery: %s", delivery.Envelope.JobID)
		}
		seen[delivery.Envelope.JobID] = true
	}
}

func newRedisAIJobReadyIndexTest(t *testing.T) (*RedisAIJobReadyIndex, *redis.Client) {
	t.Helper()
	rawURL := strings.TrimSpace(os.Getenv("ASTER_TEST_REDIS_URL"))
	if rawURL == "" {
		t.Skip("ASTER_TEST_REDIS_URL is not set")
	}
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		t.Fatalf("parse ASTER_TEST_REDIS_URL: %v", err)
	}
	client := redis.NewClient(options)
	namespace := "ready-test-" + strings.ToLower(randomID(8))
	index, err := NewRedisAIJobReadyIndex(client, RedisAIJobReadyIndexConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = deleteRedisAIJobReadyTestNamespace(cleanupCtx, client, index)
		_ = client.Close()
	})
	return index, client
}

func deleteRedisAIJobReadyTestNamespace(ctx context.Context, client *redis.Client, index *RedisAIJobReadyIndex) error {
	var cursor uint64
	for {
		keys, next, err := client.Scan(ctx, cursor, redisAIJobReadyTestKeyPattern(index), 100).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := client.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}
