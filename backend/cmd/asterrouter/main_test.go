package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/config"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/redis/go-redis/v9"
)

func TestConfigureAIJobInfrastructureSupportsRedisAffinityWithMemoryQueue(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("ASTER_TEST_REDIS_URL"))
	if rawURL == "" {
		t.Skip("ASTER_TEST_REDIS_URL is not set")
	}
	namespace := fmt.Sprintf("affinity-wiring-%d", time.Now().UnixNano())
	service := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1", "affinity-wiring-secret")
	queue, closeInfrastructure, err := configureAIJobInfrastructure(context.Background(), config.Config{
		AIJobQueueDriver: "memory", RoutingAffinityDriver: "redis", RedisURL: rawURL, RedisNamespace: namespace,
	}, service)
	if err != nil {
		t.Fatalf("configureAIJobInfrastructure(): %v", err)
	}
	defer closeInfrastructure()
	if _, ok := queue.(*controlplane.MemoryAIJobDeliveryQueue); !ok {
		t.Fatalf("queue type=%T, want memory queue", queue)
	}
	input := controlplane.GatewayAffinityInput{
		TenantID: "tenant", PrincipalID: "principal", CredentialID: "credential", Model: "model",
		Protocol: "openai_chat_completions", RouteGroup: "default", PolicyVersion: 1,
	}
	if err := service.BindGatewayCandidateAffinity(context.Background(), input, controlplane.GatewayProvider{ID: "provider-a"}); err != nil {
		t.Fatal(err)
	}
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(options)
	t.Cleanup(func() { _ = client.Close() })
	pattern := "asterrouter:{" + namespace + ":routing_affinity}:*"
	keys, err := client.Keys(context.Background(), pattern).Result()
	if err != nil || len(keys) != 1 {
		t.Fatalf("routing affinity keys=%v err=%v", keys, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Del(cleanupCtx, keys...).Err()
	})
}

func TestConfigureAIJobInfrastructureRejectsUnknownDrivers(t *testing.T) {
	service := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	if _, _, err := configureAIJobInfrastructure(context.Background(), config.Config{AIJobQueueDriver: "unknown"}, service); err == nil {
		t.Fatal("unknown AI Job queue driver was accepted")
	}
	if _, _, err := configureAIJobInfrastructure(context.Background(), config.Config{RoutingAffinityDriver: "unknown"}, service); err == nil {
		t.Fatal("unknown routing affinity driver was accepted")
	}
}
