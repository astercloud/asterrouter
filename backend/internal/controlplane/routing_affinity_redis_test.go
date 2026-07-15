package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRedisRoutingAffinityCoordinatorConfiguration(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { _ = client.Close() })
	if _, err := NewRedisRoutingAffinityCoordinator(nil, RedisRoutingAffinityCoordinatorConfig{}); !errors.Is(err, ErrRoutingAffinityCoordinatorConfig) {
		t.Fatalf("nil client error=%v", err)
	}
	if _, err := NewRedisRoutingAffinityCoordinator(client, RedisRoutingAffinityCoordinatorConfig{Namespace: "invalid namespace"}); !errors.Is(err, ErrRoutingAffinityCoordinatorConfig) {
		t.Fatalf("invalid namespace error=%v", err)
	}
}

func TestRedisRoutingAffinityCoordinatorContract(t *testing.T) {
	coordinator, client := newRedisRoutingAffinityCoordinatorTest(t)
	ctx := context.Background()
	now := time.Now().UTC()
	left := routingAffinityRedisTestBinding("affinity_contract", "provider-a", "account-a", now)
	right := routingAffinityRedisTestBinding("affinity_contract", "provider-b", "account-b", now)

	start := make(chan struct{})
	results := make(chan RoutingAffinityBinding, 64)
	errorsSeen := make(chan error, 64)
	var wait sync.WaitGroup
	for index := 0; index < 64; index++ {
		wait.Add(1)
		binding := left
		if index%2 == 1 {
			binding = right
		}
		go func() {
			defer wait.Done()
			<-start
			winner, _, err := coordinator.Claim(ctx, binding, time.Second)
			if err != nil {
				errorsSeen <- err
				return
			}
			results <- winner
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errorsSeen)
	for err := range errorsSeen {
		t.Errorf("claim: %v", err)
	}
	var first RoutingAffinityBinding
	for winner := range results {
		if first.ScopeKey == "" {
			first = winner
		}
		if !sameRoutingAffinityOwner(first, winner) {
			t.Fatalf("multiple first-write winners: first=%+v other=%+v", first, winner)
		}
	}
	stored, found, err := coordinator.Find(ctx, left.ScopeKey)
	if err != nil || !found || !sameRoutingAffinityOwner(first, stored) {
		t.Fatalf("stored winner=%+v found=%t err=%v", stored, found, err)
	}
	loser := left
	if sameRoutingAffinityOwner(stored, left) {
		loser = right
	}
	if refreshed, err := coordinator.Refresh(ctx, loser, time.Second); err != nil || refreshed {
		t.Fatalf("losing owner refresh=%t err=%v", refreshed, err)
	}
	stored.LastReusedAt = now.Add(time.Minute)
	stored.ExpiresAt = now.Add(80 * time.Millisecond)
	if refreshed, err := coordinator.Refresh(ctx, stored, 80*time.Millisecond); err != nil || !refreshed {
		t.Fatalf("winning owner refresh=%t err=%v", refreshed, err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		_, found, err = coordinator.Find(ctx, stored.ScopeKey)
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if found {
		t.Fatal("routing affinity binding did not expire")
	}

	poisonKey, err := coordinator.key("affinity_poison")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.HSet(ctx, poisonKey, "owner", "poison", "value", "not-json").Err(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := coordinator.Find(ctx, "affinity_poison"); !errors.Is(err, ErrRoutingAffinityStateInvalid) {
		t.Fatalf("poison state error=%v", err)
	}
	partial := routingAffinityRedisTestBinding("affinity_partial", "provider-partial", "account-partial", now)
	partialPayload, _ := json.Marshal(partial)
	partialKey, err := coordinator.key(partial.ScopeKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.HSet(ctx, partialKey, "value", string(partialPayload)).Err(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := coordinator.Find(ctx, partial.ScopeKey); !errors.Is(err, ErrRoutingAffinityStateInvalid) {
		t.Fatalf("partial state error=%v", err)
	}
}

func TestRedisRoutingAffinityCoordinatorTenThousandBindings(t *testing.T) {
	coordinator, client := newRedisRoutingAffinityCoordinatorTest(t)
	ctx := context.Background()
	now := time.Now().UTC()
	const bindingCount = 10_000
	for index := 0; index < bindingCount; index++ {
		binding := routingAffinityRedisTestBinding(
			fmt.Sprintf("affinity_scale_%05d", index),
			fmt.Sprintf("provider-%03d", index%100),
			fmt.Sprintf("account-%05d", index),
			now,
		)
		if winner, claimed, err := coordinator.Claim(ctx, binding, time.Minute); err != nil || !claimed || !sameRoutingAffinityOwner(winner, binding) {
			t.Fatalf("claim index=%d winner=%+v claimed=%t err=%v", index, winner, claimed, err)
		}
	}
	for _, index := range []int{0, bindingCount / 2, bindingCount - 1} {
		binding, found, err := coordinator.Find(ctx, fmt.Sprintf("affinity_scale_%05d", index))
		if err != nil || !found || binding.ProviderAccountID != fmt.Sprintf("account-%05d", index) {
			t.Fatalf("find index=%d binding=%+v found=%t err=%v", index, binding, found, err)
		}
	}
	iterator := client.Scan(ctx, 0, coordinator.keyPrefix+"*", 1000).Iterator()
	count := 0
	for iterator.Next(ctx) {
		count++
	}
	if err := iterator.Err(); err != nil || count != bindingCount {
		t.Fatalf("Redis affinity key count=%d err=%v", count, err)
	}
}

func newRedisRoutingAffinityCoordinatorTest(t *testing.T) (*RedisRoutingAffinityCoordinator, *redis.Client) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		t.Skipf("test Redis is unavailable: %v", err)
	}
	coordinator, err := NewRedisRoutingAffinityCoordinator(client, RedisRoutingAffinityCoordinatorConfig{Namespace: "affinity-test-" + strings.ToLower(randomID(8))})
	if err != nil {
		_ = client.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		iterator := client.Scan(cleanupCtx, 0, coordinator.keyPrefix+"*", 100).Iterator()
		keys := []string{}
		for iterator.Next(cleanupCtx) {
			keys = append(keys, iterator.Val())
		}
		if len(keys) > 0 {
			_ = client.Del(cleanupCtx, keys...).Err()
		}
		_ = client.Close()
	})
	return coordinator, client
}

func routingAffinityRedisTestBinding(scopeKey, providerID, accountID string, now time.Time) RoutingAffinityBinding {
	return RoutingAffinityBinding{
		ScopeKey: scopeKey, Kind: AffinityBindingAccount, ProviderID: providerID, ProviderAccountID: accountID,
		RouteID: "route-" + accountID, Model: "model", Protocol: "openai_chat_completions", PolicyVersion: 1,
		CreatedAt: now, LastReusedAt: now, ExpiresAt: now.Add(time.Second),
	}
}
