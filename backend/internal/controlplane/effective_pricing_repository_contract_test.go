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
			provider := ProviderConnection{
				ID: "provider-contract", Name: "Contract Provider", Type: "openai_compatible",
				BaseURL: "https://provider.example/v1", Status: ProviderStatusActive,
				Models: []string{"model-contract"}, CreatedAt: now, UpdatedAt: now,
			}
			if err := repo.SaveProvider(ctx, provider); err != nil {
				t.Fatalf("SaveProvider(): %v", err)
			}
			account := ProviderAccount{
				ID: "account-contract", ProviderID: provider.ID, Name: "Contract Account",
				Platform: "openai_compatible", AuthType: "api_key", Status: AccountStatusActive,
				Models: []string{"model-contract"}, CreatedAt: now, UpdatedAt: now,
			}
			if err := repo.SaveProviderAccount(ctx, account); err != nil {
				t.Fatalf("SaveProviderAccount(): %v", err)
			}

			capability := ProviderCacheCapability{
				ID: "capability-contract", ProviderAccountID: account.ID, UpstreamModel: "model-contract",
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

			decision := EffectivePricingDecision{
				ID: "decision-contract", Model: "gateway-model", UpstreamModel: "model-contract",
				Protocol: "openai_chat_completions", CurrentProviderAccountID: "account-current",
				CandidateProviderAccountID: account.ID, Status: EffectivePricingDecisionCanary,
				CreatedAt: now, UpdatedAt: now,
			}
			if err := repo.SaveEffectivePricingDecision(ctx, decision); err != nil {
				t.Fatalf("SaveEffectivePricingDecision(): %v", err)
			}
			windowEnd := now.Add(time.Hour)
			evaluation := EffectivePricingDecisionEvaluation{
				ID: "evaluation-contract", DecisionID: decision.ID, WindowStart: now,
				WindowEnd: windowEnd, Verdict: EffectivePricingEvaluationHealthy,
				ReasonCodes: []string{"contract_evidence"}, CreatedAt: windowEnd,
			}
			updatedDecision := decision
			updatedDecision.HealthyWindowCount = 1
			updatedDecision.LastEvaluationID = evaluation.ID
			updatedDecision.LastEvaluationReasonCodes = append([]string(nil), evaluation.ReasonCodes...)
			updatedDecision.UpdatedAt = windowEnd
			applied, err := repo.CommitEffectivePricingDecisionEvaluation(ctx, EffectivePricingDecisionEvaluationCommit{
				Evaluation: evaluation, Decision: updatedDecision, ExpectedStatus: decision.Status, ExpectedUpdatedAt: decision.UpdatedAt,
			})
			if err != nil || !applied {
				t.Fatalf("CommitEffectivePricingDecisionEvaluation() applied=%t err=%v", applied, err)
			}
			applied, err = repo.CommitEffectivePricingDecisionEvaluation(ctx, EffectivePricingDecisionEvaluationCommit{
				Evaluation: evaluation, Decision: updatedDecision, ExpectedStatus: decision.Status, ExpectedUpdatedAt: decision.UpdatedAt,
			})
			if err != nil || applied {
				t.Fatalf("duplicate evaluation applied=%t err=%v", applied, err)
			}
			history, err := repo.ListEffectivePricingDecisionEvaluations(ctx, decision.ID, 10)
			if err != nil || len(history) != 1 || history[0].ReasonCodes[0] != "contract_evidence" {
				t.Fatalf("evaluation history=%+v err=%v", history, err)
			}
		})
	}
}

func TestEffectivePricingDecisionEvaluationPostgresRestartPersistence(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	ctx := context.Background()
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	repo, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatalf("NewPostgresRepository(): %v", err)
	}
	decision := EffectivePricingDecision{
		ID: "decision-restart", Model: "gateway-model", UpstreamModel: "upstream-model",
		Protocol: "openai_chat_completions", CurrentProviderAccountID: "account-current",
		CandidateProviderAccountID: "account-candidate", Status: EffectivePricingDecisionCanary,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.SaveEffectivePricingDecision(ctx, decision); err != nil {
		t.Fatal(err)
	}
	evaluation := EffectivePricingDecisionEvaluation{
		ID: "evaluation-restart", DecisionID: decision.ID, WindowStart: now,
		WindowEnd: now.Add(time.Hour), Verdict: EffectivePricingEvaluationHealthy,
		ReasonCodes: []string{"restart_evidence"}, CreatedAt: now.Add(time.Hour),
	}
	updated := decision
	updated.HealthyWindowCount = 1
	updated.LastEvaluationID = evaluation.ID
	updated.LastEvaluationVerdict = evaluation.Verdict
	updated.LastEvaluationReasonCodes = append([]string(nil), evaluation.ReasonCodes...)
	updated.LastEvaluatedWindowEnd = &evaluation.WindowEnd
	updated.UpdatedAt = evaluation.CreatedAt
	applied, err := repo.CommitEffectivePricingDecisionEvaluation(ctx, EffectivePricingDecisionEvaluationCommit{
		Evaluation: evaluation, Decision: updated, ExpectedStatus: decision.Status, ExpectedUpdatedAt: decision.UpdatedAt,
	})
	if err != nil || !applied {
		t.Fatalf("commit before restart applied=%t err=%v", applied, err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatalf("reopen NewPostgresRepository(): %v", err)
	}
	defer reopened.Close()
	history, err := reopened.ListEffectivePricingDecisionEvaluations(ctx, decision.ID, 10)
	if err != nil || len(history) != 1 || history[0].ReasonCodes[0] != "restart_evidence" {
		t.Fatalf("history after restart=%+v err=%v", history, err)
	}
	decisions, err := reopened.ListEffectivePricingDecisions(ctx)
	if err != nil || len(decisions) != 1 || decisions[0].HealthyWindowCount != 1 || decisions[0].LastEvaluationID != evaluation.ID {
		t.Fatalf("decision after restart=%+v err=%v", decisions, err)
	}
}
