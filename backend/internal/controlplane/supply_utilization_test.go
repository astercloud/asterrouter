package controlplane

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestSupplyUtilizationProjectsConfiguredAndObservedEvidence(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	service := NewService(repo, "/v1")
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	seedSupplyAccount(t, repo, now, ProviderAccount{ID: "account-a", ProviderID: "provider-a", Name: "Primary account", Status: AccountStatusActive, Schedulable: true, Concurrency: 2, RPMLimit: 10, TPMLimit: 1000})
	model := GatewayModel{ID: "model-a", ModelID: "public-model", Name: "Public model", Modality: "chat", DefaultRouteGroup: "stable", Status: GatewayModelStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := repo.SaveGatewayModel(ctx, model); err != nil {
		t.Fatalf("SaveGatewayModel(): %v", err)
	}
	if err := repo.SaveModelRoute(ctx, ModelRoute{ID: "route-a", GatewayModelID: model.ID, RouteGroup: "stable", ProviderAccountID: "account-a", UpstreamModel: "upstream-model", UpstreamFormat: UpstreamFormatOpenAIChat, Status: ModelRouteStatusActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveModelRoute(): %v", err)
	}
	if err := repo.SaveAPIKey(ctx, APIKeyRecord{ID: "key-a", Name: "Build application", Status: APIKeyStatusActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveAPIKey(): %v", err)
	}
	if err := repo.SaveProviderAccountHealthCheck(ctx, ProviderAccountHealthCheck{ID: "health-a", AccountID: "account-a", ProviderID: "provider-a", Status: "ok", CheckedAt: now.Add(-5 * time.Minute)}); err != nil {
		t.Fatalf("SaveProviderAccountHealthCheck(): %v", err)
	}

	for index := 0; index < 20; index++ {
		observedAt := now.Add(-30 * time.Minute).Add(time.Duration(index) * time.Millisecond)
		operationID := fmt.Sprintf("operation-%02d", index)
		traceID := fmt.Sprintf("trace-%02d", index)
		usageID := fmt.Sprintf("usage-%02d", index)
		if err := repo.SaveGatewayTrace(ctx, GatewayTrace{
			ID: traceID, OperationID: operationID, APIKeyID: "key-a", Model: model.ModelID, ProviderID: "provider-a", ProviderAccountID: "account-a",
			GatewayModelID: model.ID, RouteID: "route-a", RouteGroup: "stable", Status: "forwarded", HTTPStatus: 200, LatencyMS: 100,
			RouteAttempts: `[{"account_id":"account-a","provider_id":"provider-a","route_id":"route-a","route_group":"stable","outcome":"selected"}]`, CreatedAt: observedAt,
		}); err != nil {
			t.Fatalf("SaveGatewayTrace(): %v", err)
		}
		cost := int64(25)
		totalInput := 120
		cacheRead := 20
		if err := repo.SaveUsageRecord(ctx, UsageRecord{
			ID: usageID, OperationID: operationID, APIKeyID: "key-a", Model: model.ModelID, UpstreamModel: "upstream-model", ProviderID: "provider-a", ProviderAccountID: "account-a",
			Status: "forwarded", InputTokens: 100, TotalInputTokens: &totalInput, OutputTokens: 30, CacheReadTokens: &cacheRead,
			UsageNormalizationStatus: "normalized_openai", ProcurementCostMicros: &cost, ProcurementCostCurrency: "USD", CreatedAt: observedAt,
		}); err != nil {
			t.Fatalf("SaveUsageRecord(): %v", err)
		}
	}

	report, err := service.SupplyUtilization(ctx, SupplyUtilizationQuery{From: now.Add(-time.Hour), To: now})
	if err != nil {
		t.Fatalf("SupplyUtilization(): %v", err)
	}
	account := findSupplyRow(t, report.Rows, SupplyDimensionProviderAccount, "account-a")
	if account.Demand.Requests != 20 || account.Demand.SuccessfulRequests != 20 || account.Demand.SuccessRate != 1 {
		t.Fatalf("account demand=%+v", account.Demand)
	}
	if account.Tokens.InputTokens != 2400 || account.Tokens.OutputTokens != 600 || account.Tokens.CacheReadTokens != 400 {
		t.Fatalf("account tokens=%+v", account.Tokens)
	}
	if len(account.Costs) != 1 || account.Costs[0].Currency != "USD" || account.Costs[0].CostMicros != 500 {
		t.Fatalf("account costs=%+v", account.Costs)
	}
	if account.Watermarks.RPM.Status != SupplyEvidenceKnown || account.Watermarks.RPM.Peak != 20 || account.Watermarks.RPM.PeakRatio != 2 {
		t.Fatalf("account rpm=%+v", account.Watermarks.RPM)
	}
	if account.Period.HealthCoverage != 1 || !account.Evidence.Complete {
		t.Fatalf("account period=%+v evidence=%+v", account.Period, account.Evidence)
	}
	application := findSupplyRow(t, report.Rows, SupplyDimensionApplication, "key-a")
	if application.Demand.Requests != 20 || application.Evidence.Filter.APIKeyID != "key-a" {
		t.Fatalf("application=%+v", application)
	}
	routeGroup := findSupplyRow(t, report.Rows, SupplyDimensionRouteGroup, "model-a:stable")
	if routeGroup.Tokens.InputTokens != 2400 || routeGroup.Watermarks.RPM.Status != SupplyEvidenceNotComparable {
		t.Fatalf("route group=%+v", routeGroup)
	}
}

func TestSupplyUtilizationClassifiesFallbackAndCapacityRejection(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	service := NewService(repo, "/v1")
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	seedSupplyAccount(t, repo, now, ProviderAccount{ID: "account-primary", ProviderID: "provider-a", Name: "Primary", Status: AccountStatusActive, Schedulable: true, Concurrency: 1})
	seedSupplyAccount(t, repo, now, ProviderAccount{ID: "account-fallback", ProviderID: "provider-b", Name: "Fallback", Status: AccountStatusActive, Schedulable: true, Concurrency: 1})
	model := GatewayModel{ID: "model-a", ModelID: "public", Name: "Public", Modality: "chat", Status: GatewayModelStatusActive, CreatedAt: now, UpdatedAt: now}
	_ = repo.SaveGatewayModel(ctx, model)
	_ = repo.SaveModelRoute(ctx, ModelRoute{ID: "route-primary", GatewayModelID: model.ID, RouteGroup: "default", ProviderAccountID: "account-primary", UpstreamModel: "upstream", UpstreamFormat: UpstreamFormatOpenAIChat, Status: ModelRouteStatusActive, CreatedAt: now, UpdatedAt: now})
	_ = repo.SaveModelRoute(ctx, ModelRoute{ID: "route-fallback", GatewayModelID: model.ID, RouteGroup: "default", ProviderAccountID: "account-fallback", UpstreamModel: "upstream", UpstreamFormat: UpstreamFormatOpenAIChat, Status: ModelRouteStatusActive, CreatedAt: now, UpdatedAt: now})
	if err := repo.SaveGatewayTrace(ctx, GatewayTrace{
		ID: "trace-fallback", Model: "public", GatewayModelID: model.ID, RouteGroup: "default", ProviderID: "provider-b", ProviderAccountID: "account-fallback",
		Status: "forwarded", HTTPStatus: 200, RouteAttempts: `[{"account_id":"account-primary","outcome":"failed","detail":"upstream http status 429"},{"account_id":"account-fallback","outcome":"selected"}]`, CreatedAt: now.Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveGatewayTrace(ctx, GatewayTrace{
		ID: "trace-capacity", Model: "public", GatewayModelID: model.ID, RouteGroup: "default", Status: "route_unavailable", ErrorType: "route_unavailable",
		RouteAttempts: `[{"account_id":"account-primary","outcome":"skipped","detail":"at_capacity"}]`, CreatedAt: now.Add(-30 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	report, err := service.SupplyUtilization(ctx, SupplyUtilizationQuery{From: now.Add(-time.Hour), To: now})
	if err != nil {
		t.Fatal(err)
	}
	fallback := findSupplyRow(t, report.Rows, SupplyDimensionProviderAccount, "account-fallback")
	if fallback.Demand.FallbackRequests != 1 || fallback.Demand.FallbackRate != 1 {
		t.Fatalf("fallback demand=%+v", fallback.Demand)
	}
	primary := findSupplyRow(t, report.Rows, SupplyDimensionProviderAccount, "account-primary")
	if primary.Demand.HTTP429Requests != 1 || primary.Demand.CapacityRejected != 1 || primary.Evidence.AttemptCount != 2 {
		t.Fatalf("primary demand=%+v evidence=%+v", primary.Demand, primary.Evidence)
	}
	modelRow := findSupplyRow(t, report.Rows, SupplyDimensionPublishedModel, model.ID)
	if modelRow.Demand.NoCandidateRequests != 1 || modelRow.Demand.CapacityRejected != 1 {
		t.Fatalf("model demand=%+v", modelRow.Demand)
	}
}

func TestCapacityRecommendationsRemainConservative(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	service := NewService(repo, "/v1")
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	seedSupplyAccount(t, repo, now, ProviderAccount{ID: "known", ProviderID: "provider", Name: "Known", Status: AccountStatusActive, Schedulable: true, Concurrency: 2, RPMLimit: 10, TPMLimit: 1000})
	seedSupplyAccount(t, repo, now, ProviderAccount{ID: "unknown", ProviderID: "provider", Name: "Unknown", Status: AccountStatusActive, Schedulable: true})
	seedSupplyAccount(t, repo, now, ProviderAccount{ID: "stranded", ProviderID: "provider", Name: "Stranded", Status: AccountStatusActive, Schedulable: true, Concurrency: 1})
	model := GatewayModel{ID: "model", ModelID: "public", Name: "Public", Modality: "chat", Status: GatewayModelStatusActive, CreatedAt: now, UpdatedAt: now}
	_ = repo.SaveGatewayModel(ctx, model)
	for _, accountID := range []string{"known", "unknown"} {
		_ = repo.SaveModelRoute(ctx, ModelRoute{ID: "route-" + accountID, GatewayModelID: model.ID, RouteGroup: "default", ProviderAccountID: accountID, UpstreamModel: "upstream", UpstreamFormat: UpstreamFormatOpenAIChat, Status: ModelRouteStatusActive, CreatedAt: now, UpdatedAt: now})
		_ = repo.SaveProviderAccountHealthCheck(ctx, ProviderAccountHealthCheck{ID: "health-" + accountID, AccountID: accountID, ProviderID: "provider", Status: "ok", CheckedAt: now.Add(-time.Minute)})
		for index := 0; index < supplyRecommendationMinimumSamples; index++ {
			_ = repo.SaveGatewayTrace(ctx, GatewayTrace{ID: fmt.Sprintf("trace-%s-%d", accountID, index), Model: "public", GatewayModelID: model.ID, RouteGroup: "default", ProviderAccountID: accountID, Status: "forwarded", HTTPStatus: 200, CreatedAt: now.Add(-30 * time.Minute).Add(time.Duration(index) * time.Millisecond)})
		}
	}
	report, err := service.CapacityRecommendations(ctx, SupplyUtilizationQuery{From: now.Add(-time.Hour), To: now})
	if err != nil {
		t.Fatalf("CapacityRecommendations(): %v", err)
	}
	known := findCapacityRecommendation(t, report.Items, "known")
	if known.Status != CapacityRecommendationActionable || known.Type != CapacityRecommendationIncrease || known.PrimaryConstraint != CapacityConstraintRPM {
		t.Fatalf("known recommendation=%+v", known)
	}
	unknown := findCapacityRecommendation(t, report.Items, "unknown")
	if unknown.Status != CapacityRecommendationInconclusive || !contains(unknown.MissingEvidence, "unknown_capacity") {
		t.Fatalf("unknown recommendation=%+v", unknown)
	}
	stranded := findCapacityRecommendation(t, report.Items, "stranded")
	if stranded.Status != CapacityRecommendationActionable || stranded.Type != CapacityRecommendationReviewStranded || !contains(stranded.ReasonCodes, "no_active_route") {
		t.Fatalf("stranded recommendation=%+v", stranded)
	}
	if report.Mode != CapacityRecommendationObserveOnly || report.Summary.Actionable != 2 || report.Summary.Inconclusive != 1 {
		t.Fatalf("report summary=%+v", report)
	}
}

func seedSupplyAccount(t *testing.T, repo *MemoryRepository, now time.Time, account ProviderAccount) {
	t.Helper()
	account.CreatedAt, account.UpdatedAt = now, now
	if err := repo.SaveProviderAccount(context.Background(), account); err != nil {
		t.Fatalf("SaveProviderAccount(): %v", err)
	}
}

func findSupplyRow(t *testing.T, rows []SupplyUtilizationRow, dimension, id string) SupplyUtilizationRow {
	t.Helper()
	for _, row := range rows {
		if row.Dimension == dimension && row.ID == id {
			return row
		}
	}
	t.Fatalf("supply row not found dimension=%s id=%s", dimension, id)
	return SupplyUtilizationRow{}
}

func findCapacityRecommendation(t *testing.T, items []CapacityRecommendation, accountID string) CapacityRecommendation {
	t.Helper()
	for _, item := range items {
		if item.Target.ProviderAccountID == accountID {
			return item
		}
	}
	t.Fatalf("capacity recommendation not found account=%s", accountID)
	return CapacityRecommendation{}
}
