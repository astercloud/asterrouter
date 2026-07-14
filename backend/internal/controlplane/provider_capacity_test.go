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

type providerCapacityTestHarness struct {
	store   ProviderCapacityStore
	advance func(time.Duration)
}

func TestProviderCapacityStoreContract(t *testing.T) {
	tests := []struct {
		name string
		open func(*testing.T) providerCapacityTestHarness
	}{
		{name: "memory", open: newMemoryProviderCapacityTestHarness},
		{name: "redis", open: newRedisProviderCapacityTestHarness},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := test.open(t)
			ctx := context.Background()
			first := providerCapacityTestRequest("account-concurrency", "lease-a")
			first.CapacityUnits = 2
			first.ConcurrencyLimit = 3
			lease, reason, acquired, err := harness.store.Acquire(ctx, first)
			if err != nil || !acquired || reason != "" {
				t.Fatalf("first lease=%+v reason=%q acquired=%t err=%v", lease, reason, acquired, err)
			}
			replayed, reason, acquired, err := harness.store.Acquire(ctx, first)
			if err != nil || !acquired || reason != "" || replayed.ID != lease.ID {
				t.Fatalf("replayed lease=%+v reason=%q acquired=%t err=%v", replayed, reason, acquired, err)
			}
			blocked := first
			blocked.LeaseID = "lease-b"
			if _, reason, acquired, err := harness.store.Acquire(ctx, blocked); err != nil || acquired || reason != "concurrency_exhausted" {
				t.Fatalf("concurrency reason=%q acquired=%t err=%v", reason, acquired, err)
			}
			conflict := first
			conflict.ProviderAccountID = "account-other"
			if _, _, _, err := harness.store.Acquire(ctx, conflict); !errors.Is(err, ErrProviderCapacityConflict) {
				t.Fatalf("conflicting lease error=%v", err)
			}
			if err := harness.store.Release(ctx, lease); err != nil {
				t.Fatal(err)
			}
			if _, reason, acquired, err := harness.store.Acquire(ctx, blocked); err != nil || !acquired || reason != "" {
				t.Fatalf("released capacity reason=%q acquired=%t err=%v", reason, acquired, err)
			}

			rate := providerCapacityTestRequest("account-rate", "rate-a")
			rate.RPMLimit = 1
			rate.TPMLimit = 10
			rate.EstimatedTokens = 6
			rateLease, _, acquired, err := harness.store.Acquire(ctx, rate)
			if err != nil || !acquired {
				t.Fatalf("rate acquire=%t err=%v", acquired, err)
			}
			if _, _, acquired, err := harness.store.Acquire(ctx, rate); err != nil || !acquired {
				t.Fatalf("idempotent rate acquire=%t err=%v", acquired, err)
			}
			snapshot, err := harness.store.Snapshot(ctx, rate.ProviderAccountID)
			if err != nil || snapshot.Requests != 1 || snapshot.Tokens != 6 || snapshot.CapacityUnits != 1 {
				t.Fatalf("rate snapshot=%+v err=%v", snapshot, err)
			}
			_ = harness.store.Release(ctx, rateLease)
			rate.LeaseID = "rate-b"
			if _, reason, acquired, err := harness.store.Acquire(ctx, rate); err != nil || acquired || reason != "rpm_exhausted" {
				t.Fatalf("rpm reason=%q acquired=%t err=%v", reason, acquired, err)
			}

			expiring := providerCapacityTestRequest("account-expiry", "expiry-a")
			expiring.ConcurrencyLimit = 1
			expiring.LeaseDuration = 300 * time.Millisecond
			expiringLease, _, acquired, err := harness.store.Acquire(ctx, expiring)
			if err != nil || !acquired {
				t.Fatalf("expiring acquire=%t err=%v", acquired, err)
			}
			harness.advance(100 * time.Millisecond)
			extended, found, err := harness.store.Extend(ctx, expiringLease, 300*time.Millisecond)
			if err != nil || !found {
				t.Fatalf("extend found=%t err=%v", found, err)
			}
			harness.advance(150 * time.Millisecond)
			if snapshot, err := harness.store.Snapshot(ctx, expiring.ProviderAccountID); err != nil || snapshot.CapacityUnits != 1 {
				t.Fatalf("extended snapshot=%+v err=%v", snapshot, err)
			}
			harness.advance(200 * time.Millisecond)
			if snapshot, err := harness.store.Snapshot(ctx, expiring.ProviderAccountID); err != nil || snapshot.CapacityUnits != 0 {
				t.Fatalf("expired snapshot=%+v err=%v extended=%+v", snapshot, err, extended)
			}

			restoreBase := providerCapacityTestRequest("account-restore", "restore-a")
			restoreBase.ConcurrencyLimit = 1
			if _, _, acquired, err := harness.store.Acquire(ctx, restoreBase); err != nil || !acquired {
				t.Fatalf("restore base acquired=%t err=%v", acquired, err)
			}
			restored, err := harness.store.Restore(ctx, ProviderCapacityLease{
				ID: "restore-existing-task", ProviderAccountID: restoreBase.ProviderAccountID, CapacityUnits: 1,
			}, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if snapshot, err := harness.store.Snapshot(ctx, restoreBase.ProviderAccountID); err != nil || snapshot.CapacityUnits != 2 {
				t.Fatalf("restored snapshot=%+v err=%v lease=%+v", snapshot, err, restored)
			}
		})
	}
}

func TestProviderCapacityDoesNotOversellAcrossConcurrentInstances(t *testing.T) {
	tests := []struct {
		name string
		open func(*testing.T) (ProviderCapacityStore, ProviderCapacityStore)
	}{
		{name: "memory", open: func(*testing.T) (ProviderCapacityStore, ProviderCapacityStore) {
			store := NewMemoryProviderCapacityStore()
			return store, store
		}},
		{name: "redis", open: func(t *testing.T) (ProviderCapacityStore, ProviderCapacityStore) {
			harness := newRedisProviderCapacityTestHarness(t)
			first := harness.store.(*RedisProviderCapacityStore)
			second := *first
			return first, &second
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			first, second := test.open(t)
			var acquired atomic.Int32
			var wait sync.WaitGroup
			for index := 0; index < 20; index++ {
				wait.Add(1)
				go func(index int) {
					defer wait.Done()
					store := first
					if index%2 == 1 {
						store = second
					}
					request := providerCapacityTestRequest("account-shared", "shared-"+string(rune('a'+index)))
					request.ConcurrencyLimit = 3
					if _, _, ok, err := store.Acquire(context.Background(), request); err != nil {
						t.Errorf("acquire: %v", err)
					} else if ok {
						acquired.Add(1)
					}
				}(index)
			}
			wait.Wait()
			if acquired.Load() != 3 {
				t.Fatalf("acquired=%d want=3", acquired.Load())
			}
		})
	}
}

func TestRedisProviderCapacityStoreConfiguration(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	defer client.Close()
	if _, err := NewRedisProviderCapacityStore(nil, RedisProviderCapacityStoreConfig{}); !errors.Is(err, ErrProviderCapacityConfig) {
		t.Fatalf("nil client error=%v", err)
	}
	if _, err := NewRedisProviderCapacityStore(client, RedisProviderCapacityStoreConfig{Namespace: "invalid namespace"}); !errors.Is(err, ErrProviderCapacityConfig) {
		t.Fatalf("invalid namespace error=%v", err)
	}
}

func providerCapacityTestRequest(accountID, leaseID string) ProviderCapacityRequest {
	return ProviderCapacityRequest{
		LeaseID: leaseID, ProviderAccountID: accountID, CapacityUnits: 1, LeaseDuration: time.Minute,
	}
}

func newMemoryProviderCapacityTestHarness(*testing.T) providerCapacityTestHarness {
	base := time.Date(2026, time.July, 15, 14, 0, 0, 0, time.UTC)
	now := base
	store := NewMemoryProviderCapacityStore()
	store.now = func() time.Time { return now }
	return providerCapacityTestHarness{store: store, advance: func(duration time.Duration) { now = now.Add(duration) }}
}

func newRedisProviderCapacityTestHarness(t *testing.T) providerCapacityTestHarness {
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
	store, err := NewRedisProviderCapacityStore(client, RedisProviderCapacityStoreConfig{Namespace: "capacity-test-" + strings.ToLower(randomID(8))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = deleteRedisProviderCapacityTestNamespace(cleanupCtx, client, store)
		_ = client.Close()
	})
	return providerCapacityTestHarness{store: store, advance: func(duration time.Duration) { time.Sleep(duration + 15*time.Millisecond) }}
}

func deleteRedisProviderCapacityTestNamespace(ctx context.Context, client *redis.Client, store *RedisProviderCapacityStore) error {
	var cursor uint64
	for {
		keys, next, err := client.Scan(ctx, cursor, redisProviderCapacityTestKeyPattern(store), 100).Result()
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
