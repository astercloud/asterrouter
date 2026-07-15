package controlplane

import (
	"context"
	"testing"
	"time"
)

func TestEffectivePricingDecisionMonitorPromotesAfterConsecutiveHealthyWindows(t *testing.T) {
	svc, repo, now := newEffectivePricingMonitorFixture(t, EffectivePricingDecisionCanary, 50)

	*now = time.Date(2026, 7, 15, 11, 5, 0, 0, time.UTC)
	first, err := svc.EvaluateEffectivePricingDecisionWindows(context.Background(), "monitor")
	if err != nil || len(first) != 1 || first[0].Verdict != EffectivePricingEvaluationHealthy || first[0].AutomaticAction != "" {
		t.Fatalf("first evaluation=%+v err=%v", first, err)
	}
	decision := effectivePricingMonitorDecision(t, svc)
	if decision.Status != EffectivePricingDecisionCanary || decision.HealthyWindowCount != 1 {
		t.Fatalf("first healthy window decision=%+v", decision)
	}

	*now = time.Date(2026, 7, 15, 12, 5, 0, 0, time.UTC)
	second, err := svc.EvaluateEffectivePricingDecisionWindows(context.Background(), "monitor")
	if err != nil || len(second) != 1 || second[0].AutomaticAction != "activate" {
		t.Fatalf("second evaluation=%+v err=%v", second, err)
	}
	decision = effectivePricingMonitorDecision(t, svc)
	if decision.Status != EffectivePricingDecisionActive || decision.HealthyWindowCount != 2 || decision.LastAutomaticAction != "activate" {
		t.Fatalf("promoted decision=%+v", decision)
	}
	evaluations, err := svc.ListEffectivePricingDecisionEvaluations(context.Background(), decision.ID, 10)
	if err != nil || len(evaluations) != 2 {
		t.Fatalf("evaluation history=%+v err=%v", evaluations, err)
	}
	if len(repo.auditLogs) != 1 {
		t.Fatalf("automatic action audit count=%d", len(repo.auditLogs))
	}

	duplicate, err := svc.EvaluateEffectivePricingDecisionWindows(context.Background(), "monitor")
	if err != nil || len(duplicate) != 0 {
		t.Fatalf("same-window duplicate=%+v err=%v", duplicate, err)
	}
}

func TestEffectivePricingDecisionMonitorRollsBackAfterConsecutiveDegradedWindows(t *testing.T) {
	svc, _, now := newEffectivePricingMonitorFixture(t, EffectivePricingDecisionActive, 150)

	*now = time.Date(2026, 7, 15, 11, 5, 0, 0, time.UTC)
	first, err := svc.EvaluateEffectivePricingDecisionWindows(context.Background(), "monitor")
	if err != nil || len(first) != 1 || first[0].Verdict != EffectivePricingEvaluationDegraded || first[0].AutomaticAction != "" {
		t.Fatalf("first degradation=%+v err=%v", first, err)
	}
	decision := effectivePricingMonitorDecision(t, svc)
	if decision.Status != EffectivePricingDecisionActive || decision.DegradedWindowCount != 1 {
		t.Fatalf("first degraded window decision=%+v", decision)
	}

	*now = time.Date(2026, 7, 15, 12, 5, 0, 0, time.UTC)
	second, err := svc.EvaluateEffectivePricingDecisionWindows(context.Background(), "monitor")
	if err != nil || len(second) != 1 || second[0].AutomaticAction != "rollback" {
		t.Fatalf("second degradation=%+v err=%v", second, err)
	}
	decision = effectivePricingMonitorDecision(t, svc)
	if decision.Status != EffectivePricingDecisionRolledBack || decision.DegradedWindowCount != 2 || decision.LastAutomaticAction != "rollback" {
		t.Fatalf("rolled back decision=%+v", decision)
	}
}

func TestEffectivePricingDecisionMonitorDoesNotRollbackInconclusiveEvidence(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 11, 5, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "monitor-test-secret")
	svc.now = func() time.Time { return now }
	startedAt := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	if err := repo.SaveEffectivePricingPolicy(ctx, effectivePricingMonitorPolicy(startedAt)); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveEffectivePricingDecision(ctx, EffectivePricingDecision{
		ID: "decision-inconclusive", Model: "gateway-model", UpstreamModel: "upstream-model",
		Protocol: "openai_chat_completions", CurrentProviderAccountID: "account-current",
		CandidateProviderAccountID: "account-candidate", Status: EffectivePricingDecisionActive,
		MonitoringStartedAt: &startedAt, LastEvaluatedWindowEnd: &startedAt,
		CreatedAt: startedAt, UpdatedAt: startedAt,
	}); err != nil {
		t.Fatal(err)
	}

	evaluations, err := svc.EvaluateEffectivePricingDecisionWindows(ctx, "monitor")
	if err != nil || len(evaluations) != 1 || evaluations[0].Verdict != EffectivePricingEvaluationInconclusive {
		t.Fatalf("inconclusive evaluation=%+v err=%v", evaluations, err)
	}
	decision := effectivePricingMonitorDecision(t, svc)
	if decision.Status != EffectivePricingDecisionActive || decision.HealthyWindowCount != 0 || decision.DegradedWindowCount != 0 {
		t.Fatalf("inconclusive evidence changed routing=%+v", decision)
	}
}

func TestEffectivePricingDecisionMonitorRespectsAutomaticActionKillSwitch(t *testing.T) {
	svc, repo, now := newEffectivePricingMonitorFixture(t, EffectivePricingDecisionCanary, 50)
	policy := effectivePricingMonitorPolicy(time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC))
	policy.AutomaticActionsEnabled = false
	policy.PromotionWindowCount = 1
	if err := repo.SaveEffectivePricingPolicy(context.Background(), policy); err != nil {
		t.Fatal(err)
	}
	*now = time.Date(2026, 7, 15, 11, 5, 0, 0, time.UTC)

	evaluations, err := svc.EvaluateEffectivePricingDecisionWindows(context.Background(), "monitor")
	if err != nil || len(evaluations) != 1 || evaluations[0].Verdict != EffectivePricingEvaluationHealthy || evaluations[0].AutomaticAction != "" {
		t.Fatalf("kill-switch evaluation=%+v err=%v", evaluations, err)
	}
	decision := effectivePricingMonitorDecision(t, svc)
	if decision.Status != EffectivePricingDecisionCanary || decision.HealthyWindowCount != 1 || decision.LastAutomaticAction != "" {
		t.Fatalf("kill switch allowed automatic promotion=%+v", decision)
	}
}

func newEffectivePricingMonitorFixture(t *testing.T, status string, candidateCost int64) (*Service, *MemoryRepository, *time.Time) {
	t.Helper()
	ctx := context.Background()
	startedAt := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	now := startedAt
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "monitor-test-secret")
	svc.now = func() time.Time { return now }
	seedEffectivePricingProvider(t, repo, startedAt, "provider-current", "account-current", "Current")
	seedEffectivePricingProvider(t, repo, startedAt, "provider-candidate", "account-candidate", "Candidate")
	for _, item := range []struct {
		providerID string
		accountID  string
	}{
		{providerID: "provider-current", accountID: "account-current"},
		{providerID: "provider-candidate", accountID: "account-candidate"},
	} {
		if err := repo.SaveProcurementPrice(ctx, ProcurementPrice{
			ID: "price-" + item.accountID, ProviderID: item.providerID, ProviderAccountID: item.accountID,
			UpstreamModel: "upstream-model", Protocol: "openai_chat_completions", Currency: "USD",
			UncachedInputMicrosPer1MTokens: 1_000_000, ReferenceInputMicrosPer1MTokens: 1_000_000,
			Confidence: ProcurementCostConfidenceExact, Status: ProcurementPriceStatusActive,
			EffectiveFrom: startedAt.Add(-time.Hour), CreatedAt: startedAt, UpdatedAt: startedAt,
		}); err != nil {
			t.Fatal(err)
		}
	}
	total, uncached := 100, 100
	currentCost := int64(100)
	for _, record := range []UsageRecord{
		{ID: "usage-current", ProviderID: "provider-current", ProviderAccountID: "account-current", UpstreamModel: "upstream-model", Protocol: "openai_chat_completions", Status: "forwarded", LatencyMS: 100, InputTokens: 100, TotalInputTokens: &total, UncachedInputTokens: &uncached, CacheFieldsPresent: true, ProcurementCostMicros: &currentCost, CreatedAt: startedAt.Add(30 * time.Minute)},
		{ID: "usage-candidate", ProviderID: "provider-candidate", ProviderAccountID: "account-candidate", UpstreamModel: "upstream-model", Protocol: "openai_chat_completions", Status: "forwarded", LatencyMS: 100, InputTokens: 100, TotalInputTokens: &total, UncachedInputTokens: &uncached, CacheFieldsPresent: true, ProcurementCostMicros: &candidateCost, CreatedAt: startedAt.Add(30 * time.Minute)},
	} {
		if err := repo.SaveUsageRecord(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.SaveEffectivePricingPolicy(ctx, effectivePricingMonitorPolicy(startedAt)); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveEffectivePricingDecision(ctx, EffectivePricingDecision{
		ID: "decision-monitor", Model: "gateway-model", UpstreamModel: "upstream-model",
		Protocol: "openai_chat_completions", CurrentProviderAccountID: "account-current",
		CandidateProviderAccountID: "account-candidate", CurrentCostMicrosPer1M: 1_000_000,
		CandidateCostMicrosPer1M: candidateCost * 10_000, Status: status, Confidence: ProcurementCostConfidenceExact,
		MonitoringStartedAt: &startedAt, LastEvaluatedWindowEnd: &startedAt,
		CreatedAt: startedAt, UpdatedAt: startedAt,
	}); err != nil {
		t.Fatal(err)
	}
	return svc, repo, &now
}

func effectivePricingMonitorPolicy(now time.Time) EffectivePricingPolicy {
	return EffectivePricingPolicy{
		ID: defaultEffectivePricingPolicyID, Mode: EffectivePricingModeBalanced, WindowHours: 24,
		MinSampleCount: 1, MinMetricsCoverage: 0, MinBillingConsistency: 0, MinCostImprovement: 0.08,
		MinCacheHitRateImprovement: 0.10, MinAffinityImprovement: 0.10,
		MaxCacheTiebreakCostRegression: 0.02, MaxErrorRateRegression: 0.01,
		MaxP95LatencyRegression: 0.20, CanaryPercent: 5, SupplierAffinityTTLSeconds: 3600,
		AccountAffinityTTLSeconds: 1800, AutomaticActionsEnabled: true,
		EvaluationIntervalMinutes: 60, PromotionWindowCount: 2, DegradationWindowCount: 2,
		CreatedAt: now, UpdatedAt: now,
	}
}

func effectivePricingMonitorDecision(t *testing.T, svc *Service) EffectivePricingDecision {
	t.Helper()
	decisions, err := svc.ListEffectivePricingDecisions(context.Background())
	if err != nil || len(decisions) != 1 {
		t.Fatalf("decisions=%+v err=%v", decisions, err)
	}
	return decisions[0]
}
