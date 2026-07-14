package controlplane

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

func TestEffectivePricingRepositoryContract(t *testing.T) {
	tests := []struct {
		name string
		open func(*testing.T) Repository
	}{
		{name: "memory", open: func(*testing.T) Repository { return NewMemoryRepository() }},
		{name: "postgres", open: func(t *testing.T) Repository {
			schema := testutil.NewPostgresSchema(t)
			repo, err := NewPostgresRepository(context.Background(), schema.URL)
			if err != nil {
				t.Fatalf("NewPostgresRepository(): %v", err)
			}
			return repo
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repo := test.open(t)
			t.Cleanup(func() { _ = repo.Close() })
			now := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)

			capability := ProviderCacheCapability{
				ID: "capability-contract", ProviderAccountID: "account-contract", UpstreamModel: "model-contract",
				Protocol: "openai_chat_completions", SupportStatus: CacheSupportObserved,
				AffinityTransport: AffinityTransportHeader, AffinityField: "X-Session-ID",
				CacheControlMode: CacheControlModePromptCacheKey, CreatedAt: now, UpdatedAt: now,
			}
			if err := repo.SaveProviderCacheCapability(ctx, capability); err != nil {
				t.Fatalf("SaveProviderCacheCapability(): %v", err)
			}
			foundCapability, found, err := repo.FindProviderCacheCapability(ctx, capability.ProviderAccountID, capability.UpstreamModel, capability.Protocol)
			if err != nil || !found || foundCapability.ID != capability.ID || foundCapability.AffinityField != capability.AffinityField {
				t.Fatalf("FindProviderCacheCapability() found=%t capability=%+v err=%v", found, foundCapability, err)
			}
			if _, found, err := repo.FindProviderCacheCapability(ctx, capability.ProviderAccountID, capability.UpstreamModel, "anthropic_messages"); err != nil || found {
				t.Fatalf("protocol-specific capability lookup found=%t err=%v", found, err)
			}

			for index := 1; index <= 20; index++ {
				record := UsageRecord{
					ID: fmt.Sprintf("usage-contract-%02d", index), ProviderID: "provider-contract",
					ProviderAccountID: capability.ProviderAccountID, UpstreamModel: capability.UpstreamModel,
					Protocol: capability.Protocol, Status: "forwarded", LatencyMS: int64(index * 10), CreatedAt: now,
				}
				if err := repo.SaveUsageRecord(ctx, record); err != nil {
					t.Fatalf("SaveUsageRecord(%s): %v", record.ID, err)
				}
			}
			failed := UsageRecord{
				ID: "usage-contract-failed", ProviderID: "provider-contract", ProviderAccountID: capability.ProviderAccountID,
				UpstreamModel: capability.UpstreamModel, Protocol: capability.Protocol, Status: "upstream_error",
				ErrorType: "upstream_status", LatencyMS: 9999, CreatedAt: now,
			}
			if err := repo.SaveUsageRecord(ctx, failed); err != nil {
				t.Fatalf("SaveUsageRecord(failed): %v", err)
			}

			aggregates, err := repo.SummarizeEffectivePricingUsage(ctx, now.Add(-time.Minute), now.Add(time.Minute))
			if err != nil {
				t.Fatalf("SummarizeEffectivePricingUsage(): %v", err)
			}
			if len(aggregates) != 1 {
				t.Fatalf("aggregate count = %d: %+v", len(aggregates), aggregates)
			}
			aggregate := aggregates[0]
			if aggregate.RequestCount != 21 || aggregate.SuccessfulRequestCount != 20 || aggregate.ErrorCount != 1 {
				t.Fatalf("request counts = %+v", aggregate)
			}
			if aggregate.P95LatencyMS != 190 {
				t.Fatalf("P95 latency = %d, want nearest-rank 190", aggregate.P95LatencyMS)
			}
		})
	}
}
