package controlplane

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestEffectivePricingReportRanksRealCostInsteadOfQuotedMultiplier(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	svc.now = func() time.Time { return now }
	seedEffectivePricingProvider(t, repo, now, "provider-a", "account-a", "Channel A")
	seedEffectivePricingProvider(t, repo, now, "provider-b", "account-b", "Channel B")
	for _, price := range []ProcurementPrice{
		{ID: "price-a", ProviderID: "provider-a", ProviderAccountID: "account-a", UpstreamModel: "model", Protocol: "openai_chat_completions", Currency: "USD", UncachedInputMicrosPer1MTokens: 1_000_000, CacheReadMicrosPer1MTokens: 100_000, ReferenceInputMicrosPer1MTokens: 1_000_000, QuotedMultiplier: 0.2, Confidence: ProcurementCostConfidenceEstimated, Status: ProcurementPriceStatusActive, EffectiveFrom: now.Add(-time.Hour), CreatedAt: now, UpdatedAt: now},
		{ID: "price-b", ProviderID: "provider-b", ProviderAccountID: "account-b", UpstreamModel: "model", Protocol: "openai_chat_completions", Currency: "USD", UncachedInputMicrosPer1MTokens: 1_000_000, CacheReadMicrosPer1MTokens: 100_000, ReferenceInputMicrosPer1MTokens: 1_000_000, QuotedMultiplier: 0.5, Confidence: ProcurementCostConfidenceEstimated, Status: ProcurementPriceStatusActive, EffectiveFrom: now.Add(-time.Hour), CreatedAt: now, UpdatedAt: now},
	} {
		if err := repo.SaveProcurementPrice(ctx, price); err != nil {
			t.Fatal(err)
		}
	}
	uncachedA, totalA := 100, 100
	uncachedB, cachedB, totalB := 10, 90, 100
	inputs := []GatewayUsageInput{
		{Model: "public", UpstreamModel: "model", Protocol: "openai_chat_completions", ProviderID: "provider-a", ProviderAccountID: "account-a", Status: "forwarded", LatencyMS: 150, InputTokens: 100, TotalInputTokens: &totalA, UncachedInputTokens: &uncachedA, CacheFieldsPresent: true, UsageNormalizationStatus: "normalized_openai"},
		{Model: "public", UpstreamModel: "model", Protocol: "openai_chat_completions", ProviderID: "provider-b", ProviderAccountID: "account-b", Status: "forwarded", LatencyMS: 450, InputTokens: 100, TotalInputTokens: &totalB, UncachedInputTokens: &uncachedB, CacheReadTokens: &cachedB, CacheFieldsPresent: true, UsageNormalizationStatus: "normalized_openai"},
	}
	for index, input := range inputs {
		if err := svc.RecordGatewayUsage(ctx, GatewayAuthContext{APIKey: APIKeyRecord{ID: "key-" + string(rune('a'+index))}}, input); err != nil {
			t.Fatal(err)
		}
	}
	report, err := svc.EffectivePricingReport(ctx, EffectivePricingReportQuery{Model: "model", Protocol: "openai_chat_completions", WindowHours: 24})
	if err != nil {
		t.Fatalf("EffectivePricingReport(): %v", err)
	}
	if len(report.Rows) != 2 {
		t.Fatalf("report rows = %+v", report.Rows)
	}
	if report.Rows[0].ProviderAccountID != "account-b" || report.Rows[0].QuotedMultiplier <= report.Rows[1].QuotedMultiplier || report.Rows[0].EffectiveCostMicrosPer1M >= report.Rows[1].EffectiveCostMicrosPer1M {
		t.Fatalf("report did not rank real cost over quoted multiplier: %+v", report.Rows)
	}
	if report.Rows[0].P95LatencyMS != 450 || report.Rows[1].P95LatencyMS != 150 {
		t.Fatalf("report p95 latency = %+v", report.Rows)
	}
	if !report.Rows[0].CacheEconomicsAvailable || report.Rows[0].UncachedCostMicrosPer1M != 1_000_000 || report.Rows[0].CacheSavingsMicrosPer1M != 810_000 || report.Rows[0].CacheSavingsRate != 0.81 {
		t.Fatalf("cache economics = %+v", report.Rows[0])
	}
}

func TestAggregateCacheEconomicsPreservesNegativeSavingsAndCoverageGate(t *testing.T) {
	aggregate := EffectivePricingUsageAggregate{
		RequestCount: 1, SuccessfulRequestCount: 1, CacheMetricsRequestCount: 1,
		TotalInputTokens: 100, CacheWrite5mTokens: 100,
	}
	price := ProcurementPrice{
		UncachedInputMicrosPer1MTokens: 1_000_000,
		CacheWrite5mMicrosPer1MTokens:  1_250_000,
	}
	uncached, savings, rate, available := aggregateCacheEconomics(aggregate, price, true)
	if !available || uncached != 1_000_000 || savings != -250_000 || rate != -0.25 {
		t.Fatalf("negative cache economics uncached=%d savings=%d rate=%f available=%t", uncached, savings, rate, available)
	}
	aggregate.CacheMetricsRequestCount = 0
	uncached, savings, rate, available = aggregateCacheEconomics(aggregate, price, true)
	if available || uncached != 1_000_000 || savings != 0 || rate != 0 {
		t.Fatalf("coverage-gated cache economics uncached=%d savings=%d rate=%f available=%t", uncached, savings, rate, available)
	}
}

func TestImportProviderBillingLineReconcilesUsageByUpstreamRequestID(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	svc.now = func() time.Time { return now }
	seedEffectivePricingProvider(t, repo, now, "provider-a", "account-a", "Channel A")
	if err := svc.RecordGatewayUsage(ctx, GatewayAuthContext{APIKey: APIKeyRecord{ID: "key-a"}}, GatewayUsageInput{
		Model: "public", UpstreamModel: "model", Protocol: "openai_chat_completions", ProviderID: "provider-a", ProviderAccountID: "account-a",
		Status: "forwarded", InputTokens: 100, UpstreamRequestID: "upstream-request-1",
	}); err != nil {
		t.Fatal(err)
	}
	line, err := svc.ImportProviderBillingLine(ctx, "tester", ProviderBillingLineRequest{
		ProviderID: "provider-a", ProviderAccountID: "account-a", ExternalLineID: "external-line-1",
		ExternalRequestID: "upstream-request-1", UpstreamModel: "model", Currency: "USD", AmountMicros: 77,
		SourceKind: "api", Confidence: ProcurementCostConfidenceExact,
	})
	if err != nil {
		t.Fatalf("ImportProviderBillingLine(): %v", err)
	}
	if line.ReconciliationStatus != BillingReconciliationMatched || line.UsageRecordID == "" {
		t.Fatalf("billing line = %+v", line)
	}
	records, err := repo.QueryUsageRecords(ctx, UsageQuery{ID: line.UsageRecordID, Limit: 1})
	if err != nil || len(records) != 1 {
		t.Fatalf("usage records=%+v err=%v", records, err)
	}
	record := records[0]
	if record.ProcurementCostMicros == nil || *record.ProcurementCostMicros != 77 || record.ProcurementCostSource != "billing" || record.ProviderBillingLineID != line.ID {
		t.Fatalf("reconciled usage = %+v", record)
	}
}

func TestEffectivePricingDecisionCanaryOrdersCandidateAndRollbackStopsIt(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "decision-test-secret")
	svc.now = func() time.Time { return now }
	decision := EffectivePricingDecision{
		ID: "decision-a", Model: "public-model", Protocol: "openai_chat_completions",
		CurrentProviderAccountID: "account-a", CandidateProviderAccountID: "account-b",
		Status: EffectivePricingDecisionRecommended, CanaryPercent: 100,
		Confidence: ProcurementCostConfidenceExact, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.SaveEffectivePricingDecision(ctx, decision); err != nil {
		t.Fatal(err)
	}
	decision, err := svc.ActOnEffectivePricingDecision(ctx, "tester", decision.ID, EffectivePricingDecisionActionRequest{Action: "approve_canary", CanaryPercent: 100})
	if err != nil || decision.Status != EffectivePricingDecisionCanary {
		t.Fatalf("approve canary decision=%+v err=%v", decision, err)
	}
	candidates := []GatewayProvider{{ID: "provider-a", AccountID: "account-a"}, {ID: "provider-b", AccountID: "account-b"}}
	ordered := svc.OrderGatewayCandidatesByEffectivePricing(ctx, "public-model", "openai_chat_completions", "fingerprint-a", candidates)
	if ordered[0].AccountID != "account-b" || !strings.Contains(ordered[0].SelectionReason, decision.ID) {
		t.Fatalf("canary candidate order=%+v", ordered)
	}
	decision, err = svc.ActOnEffectivePricingDecision(ctx, "tester", decision.ID, EffectivePricingDecisionActionRequest{Action: "rollback"})
	if err != nil || decision.Status != EffectivePricingDecisionRolledBack {
		t.Fatalf("rollback decision=%+v err=%v", decision, err)
	}
	ordered = svc.OrderGatewayCandidatesByEffectivePricing(ctx, "public-model", "openai_chat_completions", "fingerprint-a", candidates)
	if ordered[0].AccountID != "account-a" {
		t.Fatalf("rolled back decision still changed order=%+v", ordered)
	}
}

func TestEffectivePricingCanaryUsesStableCohortDistribution(t *testing.T) {
	const percent = 25
	selected := 0
	for index := 0; index < 1000; index++ {
		cohortKey := fmt.Sprintf("customer-cohort-%d", index)
		first := inEffectivePricingCanary("canary-test-secret", "decision-a", cohortKey, percent)
		second := inEffectivePricingCanary("canary-test-secret", "decision-a", cohortKey, percent)
		if first != second {
			t.Fatalf("cohort %q was not stable", cohortKey)
		}
		if first {
			selected++
		}
	}
	if selected < 200 || selected > 300 {
		t.Fatalf("25%% canary selected %d/1000 cohorts", selected)
	}
	if inEffectivePricingCanary("canary-test-secret", "decision-a", "", percent) {
		t.Fatal("empty cohort entered canary")
	}
}

func TestEvaluateEffectivePricingDecisionRequiresUpstreamModel(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	_, err := svc.EvaluateEffectivePricingDecision(context.Background(), "tester", EffectivePricingDecisionEvaluationRequest{
		Model: "public-model", Protocol: "openai_chat_completions",
		CurrentProviderAccountID: "account-a", CandidateProviderAccountID: "account-b",
	})
	if err == nil || !strings.Contains(err.Error(), "upstream_model") {
		t.Fatalf("missing upstream_model err=%v", err)
	}
}

func TestEffectivePricingDecisionKeepsGatewayAndUpstreamModelEvidenceSeparate(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	svc.now = func() time.Time { return now }
	seedEffectivePricingProvider(t, repo, now, "provider-a", "account-a", "Channel A")
	seedEffectivePricingProvider(t, repo, now, "provider-b", "account-b", "Channel B")

	for _, model := range []string{"upstream-a", "upstream-b"} {
		for _, account := range []struct {
			providerID string
			accountID  string
		}{
			{providerID: "provider-a", accountID: "account-a"},
			{providerID: "provider-b", accountID: "account-b"},
		} {
			if err := repo.SaveProcurementPrice(ctx, ProcurementPrice{
				ID: "price-" + model + "-" + account.accountID, ProviderID: account.providerID,
				ProviderAccountID: account.accountID, UpstreamModel: model, Protocol: "openai_chat_completions",
				Currency: "USD", Confidence: ProcurementCostConfidenceExact, Status: ProcurementPriceStatusActive,
				EffectiveFrom: now.Add(-time.Hour), CreatedAt: now, UpdatedAt: now,
			}); err != nil {
				t.Fatal(err)
			}
		}
	}

	totalTokens := 100
	costs := map[string]int64{
		"upstream-a:account-a": 10,
		"upstream-a:account-b": 9,
		"upstream-b:account-a": 100,
		"upstream-b:account-b": 50,
	}
	for key, cost := range costs {
		parts := strings.Split(key, ":")
		providerID := "provider-a"
		if parts[1] == "account-b" {
			providerID = "provider-b"
		}
		cost := cost
		if err := svc.RecordGatewayUsage(ctx, GatewayAuthContext{APIKey: APIKeyRecord{ID: "key-" + key}}, GatewayUsageInput{
			Model: "gateway-public", UpstreamModel: parts[0], Protocol: "openai_chat_completions",
			ProviderID: providerID, ProviderAccountID: parts[1], Status: "forwarded", LatencyMS: 100,
			InputTokens: totalTokens, TotalInputTokens: &totalTokens, ProcurementCostMicros: &cost,
			ProcurementCostConfidence: ProcurementCostConfidenceExact,
		}); err != nil {
			t.Fatal(err)
		}
	}

	decision, err := svc.EvaluateEffectivePricingDecision(ctx, "tester", EffectivePricingDecisionEvaluationRequest{
		Model: "gateway-public", UpstreamModel: "upstream-b", Protocol: "openai_chat_completions",
		CurrentProviderAccountID: "account-a", CandidateProviderAccountID: "account-b",
	})
	if err != nil {
		t.Fatalf("EvaluateEffectivePricingDecision(): %v", err)
	}
	if decision.Model != "gateway-public" || decision.UpstreamModel != "upstream-b" || decision.CurrentCostMicrosPer1M != 1_000_000 || decision.CandidateCostMicrosPer1M != 500_000 {
		t.Fatalf("decision used evidence from the wrong model: %+v", decision)
	}

	listed, err := svc.ListEffectivePricingDecisions(ctx)
	if err != nil || len(listed) != 1 || listed[0].UpstreamModel != "upstream-b" {
		t.Fatalf("listed decisions=%+v err=%v", listed, err)
	}
	report, err := svc.EffectivePricingReport(ctx, EffectivePricingReportQuery{Model: "upstream-b", Protocol: "openai_chat_completions"})
	if err != nil || len(report.Decisions) != 1 || report.Decisions[0].UpstreamModel != "upstream-b" {
		t.Fatalf("upstream-b report decisions=%+v err=%v", report.Decisions, err)
	}
	otherReport, err := svc.EffectivePricingReport(ctx, EffectivePricingReportQuery{Model: "upstream-a", Protocol: "openai_chat_completions"})
	if err != nil || len(otherReport.Decisions) != 0 {
		t.Fatalf("upstream-a report leaked decisions=%+v err=%v", otherReport.Decisions, err)
	}
}

func TestEffectivePricingDecisionCacheQualityTiebreaker(t *testing.T) {
	tests := []struct {
		name                    string
		candidateCostMicros     int64
		candidateLatencyMS      int64
		cacheReadPriceMicros    int64
		wantStatus              string
		wantCacheTiebreaker     bool
		wantCostThresholdReason bool
		wantP95RegressionReason bool
	}{
		{
			name:                "recommends candidate when effective cost does not regress",
			candidateCostMicros: 50,
			candidateLatencyMS:  100,
			wantStatus:          EffectivePricingDecisionRecommended,
			wantCacheTiebreaker: true,
		},
		{
			name:                    "holds candidate when effective cost regression exceeds policy",
			candidateCostMicros:     52,
			candidateLatencyMS:      100,
			wantStatus:              EffectivePricingDecisionHold,
			wantCostThresholdReason: true,
		},
		{
			name:                    "holds candidate when p95 latency regression exceeds policy",
			candidateCostMicros:     50,
			candidateLatencyMS:      130,
			wantStatus:              EffectivePricingDecisionHold,
			wantP95RegressionReason: true,
		},
		{
			name:                    "holds high cache hit candidate when cache has no economic savings",
			candidateCostMicros:     50,
			candidateLatencyMS:      100,
			cacheReadPriceMicros:    1_000_000,
			wantStatus:              EffectivePricingDecisionHold,
			wantCostThresholdReason: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
			repo := NewMemoryRepository()
			svc := NewService(repo, "/v1")
			svc.now = func() time.Time { return now }
			seedEffectivePricingProvider(t, repo, now, "provider-a", "account-a", "Channel A")
			seedEffectivePricingProvider(t, repo, now, "provider-b", "account-b", "Channel B")
			cacheReadPrice := tt.cacheReadPriceMicros
			if cacheReadPrice == 0 {
				cacheReadPrice = 100_000
			}
			for _, accountID := range []string{"account-a", "account-b"} {
				providerID := "provider-a"
				if accountID == "account-b" {
					providerID = "provider-b"
				}
				if err := repo.SaveProcurementPrice(ctx, ProcurementPrice{
					ID: "price-" + accountID, ProviderID: providerID, ProviderAccountID: accountID,
					UpstreamModel: "model", Protocol: "openai_chat_completions", Currency: "USD",
					UncachedInputMicrosPer1MTokens: 1_000_000, CacheReadMicrosPer1MTokens: cacheReadPrice,
					ReferenceInputMicrosPer1MTokens: 1_000_000, Confidence: ProcurementCostConfidenceExact,
					Status: ProcurementPriceStatusActive, EffectiveFrom: now.Add(-time.Hour), CreatedAt: now, UpdatedAt: now,
				}); err != nil {
					t.Fatal(err)
				}
			}
			currentTotal, currentUncached, currentCached := 100, 90, 10
			candidateTotal, candidateUncached, candidateCached := 100, 20, 80
			currentCost := int64(50)
			for _, input := range []GatewayUsageInput{
				{Model: "public", UpstreamModel: "model", Protocol: "openai_chat_completions", ProviderID: "provider-a", ProviderAccountID: "account-a", Status: "forwarded", LatencyMS: 100, InputTokens: 100, TotalInputTokens: &currentTotal, UncachedInputTokens: &currentUncached, CacheReadTokens: &currentCached, CacheFieldsPresent: true, UsageNormalizationStatus: "normalized_openai", ProcurementCostMicros: &currentCost, ProcurementCostConfidence: ProcurementCostConfidenceExact},
				{Model: "public", UpstreamModel: "model", Protocol: "openai_chat_completions", ProviderID: "provider-b", ProviderAccountID: "account-b", Status: "forwarded", LatencyMS: tt.candidateLatencyMS, InputTokens: 100, TotalInputTokens: &candidateTotal, UncachedInputTokens: &candidateUncached, CacheReadTokens: &candidateCached, CacheFieldsPresent: true, UsageNormalizationStatus: "normalized_openai", ProcurementCostMicros: &tt.candidateCostMicros, ProcurementCostConfidence: ProcurementCostConfidenceExact},
			} {
				if err := svc.RecordGatewayUsage(ctx, GatewayAuthContext{APIKey: APIKeyRecord{ID: "cache-tiebreak-key"}}, input); err != nil {
					t.Fatal(err)
				}
			}
			for _, capability := range []ProviderCacheCapability{
				{ID: "cachecap-a", ProviderAccountID: "account-a", UpstreamModel: "model", Protocol: "openai_chat_completions", SupportStatus: CacheSupportObserved, PoolAffinityGrade: PoolAffinityProbable, BillingConsistencyRate: 1, CreatedAt: now, UpdatedAt: now},
				{ID: "cachecap-b", ProviderAccountID: "account-b", UpstreamModel: "model", Protocol: "openai_chat_completions", SupportStatus: CacheSupportObserved, PoolAffinityGrade: PoolAffinityProbable, BillingConsistencyRate: 1, CreatedAt: now, UpdatedAt: now},
			} {
				if err := repo.SaveProviderCacheCapability(ctx, capability); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := svc.UpdateEffectivePricingPolicy(ctx, "tester", EffectivePricingPolicyRequest{
				Mode: EffectivePricingModeRecommend, WindowHours: 24, MinSampleCount: 1, MinMetricsCoverage: 0.8,
				MinBillingConsistency: 0.95, MinCostImprovement: 0.08, MinCacheHitRateImprovement: 0.10,
				MinAffinityImprovement: 0.10, MaxCacheTiebreakCostRegression: 0.02, MaxErrorRateRegression: 0.01,
				MaxP95LatencyRegression: 0.2, CanaryPercent: 5, SupplierAffinityTTLSeconds: 3600,
				AccountAffinityTTLSeconds: 1800, ProbeDailyTokenBudget: 1000, ProbeDailyCostBudgetMicros: 1000,
			}); err != nil {
				t.Fatal(err)
			}
			decision, err := svc.EvaluateEffectivePricingDecision(ctx, "tester", EffectivePricingDecisionEvaluationRequest{
				Model: "public", UpstreamModel: "model", Protocol: "openai_chat_completions",
				CurrentProviderAccountID: "account-a", CandidateProviderAccountID: "account-b",
			})
			if err != nil {
				t.Fatalf("EvaluateEffectivePricingDecision(): %v", err)
			}
			if decision.Status != tt.wantStatus || contains(decision.ReasonCodes, "cache_quality_tiebreaker") != tt.wantCacheTiebreaker || contains(decision.ReasonCodes, "cost_improvement_below_threshold") != tt.wantCostThresholdReason || contains(decision.ReasonCodes, "p95_latency_regression_exceeded") != tt.wantP95RegressionReason {
				t.Fatalf("cache tiebreak decision = %+v", decision)
			}
		})
	}
}

func TestEffectivePricingQualityRegressionReasons(t *testing.T) {
	basePolicy := EffectivePricingPolicy{Mode: EffectivePricingModeRecommend, MaxErrorRateRegression: 0.005, MaxP95LatencyRegression: 0.20}
	current := EffectivePricingReportRow{ErrorRate: 0.01, P95LatencyMS: 100}
	tests := []struct {
		name      string
		candidate EffectivePricingReportRow
		policy    EffectivePricingPolicy
		want      []string
	}{
		{name: "within quality limits", candidate: EffectivePricingReportRow{ErrorRate: 0.015, P95LatencyMS: 120}, policy: basePolicy},
		{name: "error rate regression", candidate: EffectivePricingReportRow{ErrorRate: 0.016, P95LatencyMS: 100}, policy: basePolicy, want: []string{"error_rate_regression_exceeded"}},
		{name: "p95 latency regression", candidate: EffectivePricingReportRow{ErrorRate: 0.01, P95LatencyMS: 121}, policy: basePolicy, want: []string{"p95_latency_regression_exceeded"}},
		{name: "missing p95 evidence", candidate: EffectivePricingReportRow{ErrorRate: 0.01}, policy: basePolicy, want: []string{"p95_latency_evidence_missing"}},
		{name: "cost first permits missing p95", candidate: EffectivePricingReportRow{ErrorRate: 0.01}, policy: EffectivePricingPolicy{Mode: EffectivePricingModeCostFirst, MaxErrorRateRegression: 0.005, MaxP95LatencyRegression: 0.20}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := effectivePricingQualityRegressionReasons(current, tt.candidate, tt.policy)
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("reasons=%v want=%v", got, tt.want)
			}
		})
	}
}

func TestUpsertProviderCacheCapabilityRejectsReservedAffinityField(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	seedEffectivePricingProvider(t, repo, now, "provider-a", "account-a", "Channel A")
	_, err := svc.UpsertProviderCacheCapability(ctx, "tester", ProviderCacheCapabilityRequest{
		ProviderAccountID: "account-a", UpstreamModel: "model", Protocol: "openai_chat_completions",
		SupportStatus: CacheSupportAccepted, AffinityTransport: AffinityTransportBody, AffinityField: "model",
	})
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("reserved affinity field err=%v", err)
	}
}

func seedEffectivePricingProvider(t *testing.T, repo *MemoryRepository, now time.Time, providerID, accountID, name string) {
	t.Helper()
	if err := repo.SaveProvider(context.Background(), ProviderConnection{ID: providerID, Name: name, Type: "openai_compatible", BaseURL: "https://provider.example/v1", Status: ProviderStatusActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveProviderAccount(context.Background(), ProviderAccount{ID: accountID, ProviderID: providerID, Name: name + " Account", Platform: "openai_compatible", AuthType: "api_key", Status: AccountStatusActive, Models: []string{"model"}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
}
