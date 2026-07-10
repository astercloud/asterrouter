package controlplane

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEnsureSeedDataCreatesProductBaselineResources(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	if err := svc.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}

	dashboard, err := svc.Dashboard(context.Background())
	if err != nil {
		t.Fatalf("Dashboard(): %v", err)
	}
	if dashboard.ProviderCount != 1 || dashboard.ProjectCount != 1 || dashboard.ApplicationCount != 1 {
		t.Fatalf("unexpected dashboard counts: %+v", dashboard)
	}
	if len(dashboard.Models) != 2 {
		t.Fatalf("models = %+v", dashboard.Models)
	}
}

func TestCreateAPIKeyReturnsSecretOnceAndStoresHash(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	if err := svc.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}

	created, err := svc.CreateAPIKey(context.Background(), "tester", APIKeyCreateRequest{
		ProjectID:         "proj_platform",
		ApplicationID:     "app_internal_sandbox",
		Name:              "CI key",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          5,
		MonthlyTokenLimit: 100000,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	if created.Key == "" || created.Record.KeyHash == "" {
		t.Fatalf("secret/hash not generated: %+v", created)
	}
	if created.Record.KeyHash == created.Key {
		t.Fatal("api key stored without hashing")
	}

	keys, err := svc.ListAPIKeys(context.Background())
	if err != nil {
		t.Fatalf("ListAPIKeys(): %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("key count = %d", len(keys))
	}
	if keys[0].KeyHash == "" || keys[0].Fingerprint == "" || keys[0].Prefix == "" {
		t.Fatalf("stored key metadata incomplete: %+v", keys[0])
	}
}

func TestProjectAndApplicationUpdateLifecycle(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	project, err := svc.CreateProject(context.Background(), "tester", ProjectRequest{
		Name:               "Data Platform",
		Description:        "Initial",
		CostCenter:         "DATA",
		MonthlyBudgetCents: 10000,
		Status:             ProjectStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateProject(): %v", err)
	}
	updatedProject, err := svc.UpdateProject(context.Background(), "tester", project.ID, ProjectRequest{
		Name:               "Data Platform Updated",
		Description:        "Updated",
		CostCenter:         "DATA-OPS",
		MonthlyBudgetCents: 25000,
		Status:             ProjectStatusArchived,
	})
	if err != nil {
		t.Fatalf("UpdateProject(): %v", err)
	}
	if updatedProject.ID != project.ID || updatedProject.CreatedAt != project.CreatedAt || updatedProject.Name != "Data Platform Updated" || updatedProject.Status != ProjectStatusArchived {
		t.Fatalf("project update mismatch: before=%+v after=%+v", project, updatedProject)
	}

	app, err := svc.CreateApplication(context.Background(), "tester", ApplicationRequest{
		ProjectID:   project.ID,
		Name:        "Console",
		Environment: "dev",
		Owner:       "team-a",
		Status:      ApplicationStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateApplication(): %v", err)
	}
	updatedApp, err := svc.UpdateApplication(context.Background(), "tester", app.ID, ApplicationRequest{
		ProjectID:   project.ID,
		Name:        "Console API",
		Environment: "prod",
		Owner:       "team-b",
		Status:      ApplicationStatusDisabled,
	})
	if err != nil {
		t.Fatalf("UpdateApplication(): %v", err)
	}
	if updatedApp.ID != app.ID || updatedApp.CreatedAt != app.CreatedAt || updatedApp.Name != "Console API" || updatedApp.Status != ApplicationStatusDisabled {
		t.Fatalf("application update mismatch: before=%+v after=%+v", app, updatedApp)
	}
}

func TestEnforceGatewayPolicyRejectsQPSLimit(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	if err := svc.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}
	created, err := svc.CreateAPIKey(context.Background(), "tester", APIKeyCreateRequest{
		ProjectID:         "proj_platform",
		ApplicationID:     "app_internal_sandbox",
		Name:              "QPS key",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          1,
		MonthlyTokenLimit: 0,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	auth, err := svc.AuthorizeGatewayModel(context.Background(), created.Key, "gpt-4o-mini")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel(): %v", err)
	}
	if err := svc.EnforceGatewayPolicy(context.Background(), auth); err != nil {
		t.Fatalf("first EnforceGatewayPolicy(): %v", err)
	}
	if err := svc.EnforceGatewayPolicy(context.Background(), auth); !errors.Is(err, ErrGatewayRateLimited) {
		t.Fatalf("second EnforceGatewayPolicy() err = %v", err)
	}
}

func TestEnforceGatewayPolicyRejectsMonthlyTokenQuota(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	if err := svc.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}
	created, err := svc.CreateAPIKey(context.Background(), "tester", APIKeyCreateRequest{
		ProjectID:         "proj_platform",
		ApplicationID:     "app_internal_sandbox",
		Name:              "Quota key",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          0,
		MonthlyTokenLimit: 10,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	auth, err := svc.AuthorizeGatewayModel(context.Background(), created.Key, "gpt-4o-mini")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel(): %v", err)
	}
	if err := svc.RecordGatewayUsage(context.Background(), auth, GatewayUsageInput{
		Model:        "gpt-4o-mini",
		Status:       "forwarded",
		InputTokens:  4,
		OutputTokens: 6,
	}); err != nil {
		t.Fatalf("RecordGatewayUsage(): %v", err)
	}
	if err := svc.EnforceGatewayPolicy(context.Background(), auth); !errors.Is(err, ErrGatewayQuotaExceeded) {
		t.Fatalf("EnforceGatewayPolicy() err = %v", err)
	}
}

func TestUsageReportQueryAggregatesBeyondCurrentPage(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	if err := svc.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}
	created, err := svc.CreateAPIKey(context.Background(), "tester", APIKeyCreateRequest{
		ProjectID:         "proj_platform",
		ApplicationID:     "app_internal_sandbox",
		Name:              "Usage aggregate key",
		ModelAllowlist:    []string{"model-a", "model-b"},
		QPSLimit:          0,
		MonthlyTokenLimit: 0,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	auth, err := svc.AuthorizeGatewayModel(context.Background(), created.Key, "model-a")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel(): %v", err)
	}
	inputs := []GatewayUsageInput{
		{Model: "model-a", Status: "forwarded", InputTokens: 1, OutputTokens: 1, CostCents: 100, LatencyMS: 10},
		{Model: "model-b", Status: "error", ErrorType: "policy_error", InputTokens: 2, OutputTokens: 2, CostCents: 200, LatencyMS: 20},
		{Model: "model-b", Status: "forwarded", InputTokens: 3, OutputTokens: 3, CostCents: 300, LatencyMS: 30},
	}
	for _, input := range inputs {
		if err := svc.RecordGatewayUsage(context.Background(), auth, input); err != nil {
			t.Fatalf("RecordGatewayUsage(): %v", err)
		}
	}

	report, err := svc.UsageReportQuery(context.Background(), UsageQuery{Limit: 1})
	if err != nil {
		t.Fatalf("UsageReportQuery(): %v", err)
	}
	if len(report.Recent) != 1 {
		t.Fatalf("recent should remain paginated, got %d", len(report.Recent))
	}
	if report.TotalRequests != 3 || report.ErrorRequests != 1 || report.TotalTokens != 12 || report.TotalCostCents != 600 || report.AvgLatencyMS != 20 {
		t.Fatalf("aggregate does not include all records: %+v", report)
	}
	byModel := map[string]UsageModelSummary{}
	for _, item := range report.ByModel {
		byModel[item.Model] = item
	}
	if byModel["model-b"].Requests != 2 || byModel["model-b"].Errors != 1 || byModel["model-b"].Tokens != 10 {
		t.Fatalf("model aggregate does not include all matching records: %+v", report.ByModel)
	}

	filtered, err := svc.UsageReportQuery(context.Background(), UsageQuery{Limit: 1, Model: "model-b"})
	if err != nil {
		t.Fatalf("filtered UsageReportQuery(): %v", err)
	}
	if len(filtered.Recent) != 1 || filtered.TotalRequests != 2 || len(filtered.ByModel) != 1 || filtered.ByModel[0].Model != "model-b" {
		t.Fatalf("filtered aggregate mismatch: %+v", filtered)
	}
}

func TestCostAllocationReportAggregatesByGovernanceDimension(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	if err := svc.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}
	project, err := svc.CreateProject(context.Background(), "tester", ProjectRequest{
		Name:               "Billing Platform",
		CostCenter:         "FINOPS",
		MonthlyBudgetCents: 1000,
		Status:             ProjectStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateProject(): %v", err)
	}
	app, err := svc.CreateApplication(context.Background(), "tester", ApplicationRequest{
		ProjectID:   project.ID,
		Name:        "Invoice Agent",
		Environment: "prod",
		Owner:       "finops",
		Status:      ApplicationStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateApplication(): %v", err)
	}
	platformKey, err := svc.CreateAPIKey(context.Background(), "tester", APIKeyCreateRequest{
		ProjectID:         "proj_platform",
		ApplicationID:     "app_internal_sandbox",
		Name:              "Platform key",
		ModelAllowlist:    []string{"model-a"},
		MonthlyTokenLimit: 0,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey platform: %v", err)
	}
	billingKey, err := svc.CreateAPIKey(context.Background(), "tester", APIKeyCreateRequest{
		ProjectID:         project.ID,
		ApplicationID:     app.ID,
		Name:              "Billing key",
		ModelAllowlist:    []string{"model-a", "model-b"},
		MonthlyTokenLimit: 0,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey billing: %v", err)
	}
	platformAuth, err := svc.AuthorizeGatewayModel(context.Background(), platformKey.Key, "model-a")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel platform: %v", err)
	}
	billingAuth, err := svc.AuthorizeGatewayModel(context.Background(), billingKey.Key, "model-a")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel billing: %v", err)
	}
	inputs := []struct {
		auth  GatewayAuthContext
		usage GatewayUsageInput
	}{
		{platformAuth, GatewayUsageInput{Model: "model-a", Status: "forwarded", InputTokens: 10, OutputTokens: 10, CostCents: 100, LatencyMS: 10}},
		{billingAuth, GatewayUsageInput{Model: "model-a", Status: "forwarded", InputTokens: 20, OutputTokens: 5, CostCents: 250, LatencyMS: 20}},
		{billingAuth, GatewayUsageInput{Model: "model-b", Status: "error", ErrorType: "policy_error", InputTokens: 7, OutputTokens: 3, CostCents: 150, LatencyMS: 40}},
	}
	for _, input := range inputs {
		if err := svc.RecordGatewayUsage(context.Background(), input.auth, input.usage); err != nil {
			t.Fatalf("RecordGatewayUsage(): %v", err)
		}
	}

	report, err := svc.CostAllocationReportQuery(context.Background(), CostAllocationByProject, UsageQuery{Limit: 1})
	if err != nil {
		t.Fatalf("CostAllocationReportQuery project: %v", err)
	}
	if report.TotalRequests != 3 || report.TotalCostCents != 500 || len(report.Rows) != 1 {
		t.Fatalf("project report should have full totals and paginated rows: %+v", report)
	}
	if report.Rows[0].ProjectID != project.ID || report.Rows[0].TotalCostCents != 400 || report.Rows[0].BudgetCents != 1000 || report.Rows[0].BudgetUsedPercent != 40 {
		t.Fatalf("project allocation row mismatch: %+v", report.Rows[0])
	}

	keyReport, err := svc.CostAllocationReportQuery(context.Background(), CostAllocationByAPIKey, UsageQuery{ProjectID: project.ID})
	if err != nil {
		t.Fatalf("CostAllocationReportQuery api_key: %v", err)
	}
	if len(keyReport.Rows) != 1 || keyReport.Rows[0].APIKeyName != "Billing key" || keyReport.Rows[0].TotalTokens != 35 || keyReport.Rows[0].ErrorRequests != 1 {
		t.Fatalf("api key allocation mismatch: %+v", keyReport)
	}

	modelReport, err := svc.CostAllocationReportQuery(context.Background(), CostAllocationByModel, UsageQuery{ProjectID: project.ID})
	if err != nil {
		t.Fatalf("CostAllocationReportQuery model: %v", err)
	}
	if len(modelReport.Rows) != 2 || modelReport.Rows[0].Model != "model-a" || modelReport.Rows[0].TotalCostCents != 250 {
		t.Fatalf("model allocation ordering mismatch: %+v", modelReport.Rows)
	}

	if _, err := svc.CostAllocationReportQuery(context.Background(), "department", UsageQuery{}); !errors.Is(err, ErrInvalidCostAllocationDimension) {
		t.Fatalf("invalid dimension err = %v", err)
	}
}

func TestCreateProviderRequiresAbsoluteURL(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	_, err := svc.CreateProvider(context.Background(), "tester", ProviderRequest{
		Name:    "bad",
		Type:    "openai_compatible",
		BaseURL: "/relative",
		Models:  []string{"gpt-4o-mini"},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestCreateProviderEncryptsSecret(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret-key")
	provider, err := svc.CreateProvider(context.Background(), "tester", ProviderRequest{
		Name:    "OpenAI-compatible test",
		Type:    "openai_compatible",
		BaseURL: "https://provider.example/v1",
		Status:  ProviderStatusActive,
		Models:  []string{"gpt-4o-mini"},
		APIKey:  "upstream-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	if !provider.SecretConfigured || provider.SecretCiphertext == "" {
		t.Fatalf("secret metadata not configured: %+v", provider)
	}
	if provider.SecretCiphertext == "upstream-secret" {
		t.Fatal("provider secret stored in plaintext")
	}

	selected, ok, err := svc.GatewayProviderForModel(context.Background(), "gpt-4o-mini")
	if err != nil {
		t.Fatalf("GatewayProviderForModel(): %v", err)
	}
	if !ok || selected.APIKey != "upstream-secret" {
		t.Fatalf("provider secret not recovered for gateway: %+v ok=%v", selected, ok)
	}
}

func TestCheckProviderProbesModelsAndPersistsHealth(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "test-secret-key")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer upstream-secret" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-real"},{"id":"gpt-fast"}]}`))
	}))
	defer upstream.Close()

	provider, err := svc.CreateProvider(context.Background(), "tester", ProviderRequest{
		Name:    "Probe provider",
		Type:    "openai_compatible",
		BaseURL: upstream.URL + "/v1",
		Status:  ProviderStatusActive,
		Models:  []string{"manual-model"},
		APIKey:  "upstream-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}

	check, err := svc.CheckProvider(context.Background(), "tester", provider.ID)
	if err != nil {
		t.Fatalf("CheckProvider(): %v", err)
	}
	if check.Status != "ok" || len(check.Models) != 2 {
		t.Fatalf("unexpected health check: %+v", check)
	}

	health, err := svc.ListProviderHealthChecks(context.Background())
	if err != nil {
		t.Fatalf("ListProviderHealthChecks(): %v", err)
	}
	if len(health) != 1 || health[0].ProviderID != provider.ID || health[0].Status != "ok" {
		t.Fatalf("health not persisted: %+v", health)
	}

	providers, err := svc.ListProviders(context.Background())
	if err != nil {
		t.Fatalf("ListProviders(): %v", err)
	}
	if len(providers) != 1 || !contains(providers[0].Models, "manual-model") || !contains(providers[0].Models, "gpt-real") {
		t.Fatalf("discovered models not merged: %+v", providers)
	}
}

func TestProviderAccountLifecyclePreservesEncryptedSecretAndUpdatesGroupCounts(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret-key")
	provider, err := svc.CreateProvider(context.Background(), "tester", ProviderRequest{
		Name:    "Account provider",
		Type:    "openai_compatible",
		BaseURL: "https://provider.example/v1",
		Status:  ProviderStatusActive,
		Models:  []string{"gpt-4o-mini"},
		APIKey:  "provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}

	group, err := svc.CreateRoutingGroup(context.Background(), "tester", RoutingGroupRequest{
		Name:           "OpenAI default",
		Platform:       "openai_compatible",
		RateMultiplier: 1,
		Status:         RoutingGroupStatusActive,
		SortOrder:      10,
	})
	if err != nil {
		t.Fatalf("CreateRoutingGroup(): %v", err)
	}

	schedulable := true
	account, err := svc.CreateProviderAccount(context.Background(), "tester", ProviderAccountRequest{
		ProviderID:     provider.ID,
		Name:           "Account A",
		Platform:       "openai_compatible",
		AuthType:       "api_key",
		Status:         AccountStatusActive,
		Schedulable:    &schedulable,
		Priority:       20,
		Concurrency:    4,
		RateMultiplier: 1.2,
		Models:         []string{"gpt-4o-mini"},
		GroupIDs:       []string{group.ID},
		Secret:         "account-secret",
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount(): %v", err)
	}
	if !account.SecretConfigured || account.SecretCiphertext == "" {
		t.Fatalf("secret metadata not configured: %+v", account)
	}
	if account.SecretCiphertext == "account-secret" {
		t.Fatal("provider account secret stored in plaintext")
	}

	groups, err := svc.ListRoutingGroups(context.Background())
	if err != nil {
		t.Fatalf("ListRoutingGroups(): %v", err)
	}
	if len(groups) != 1 || groups[0].AccountCount != 1 || groups[0].ActiveAccounts != 1 {
		t.Fatalf("unexpected group counts: %+v", groups)
	}

	schedulable = false
	updated, err := svc.UpdateProviderAccount(context.Background(), "tester", account.ID, ProviderAccountRequest{
		ProviderID:     provider.ID,
		Name:           "Account A updated",
		Platform:       "openai_compatible",
		AuthType:       "api_key",
		Status:         AccountStatusActive,
		Schedulable:    &schedulable,
		Priority:       30,
		Concurrency:    2,
		RateMultiplier: 1,
		Models:         []string{"gpt-4o-mini"},
		GroupIDs:       []string{group.ID},
	})
	if err != nil {
		t.Fatalf("UpdateProviderAccount(): %v", err)
	}
	if updated.SecretCiphertext != account.SecretCiphertext || !updated.SecretConfigured {
		t.Fatalf("secret not preserved: before=%+v after=%+v", account, updated)
	}

	groups, err = svc.ListRoutingGroups(context.Background())
	if err != nil {
		t.Fatalf("ListRoutingGroups(): %v", err)
	}
	if groups[0].AccountCount != 1 || groups[0].ActiveAccounts != 0 {
		t.Fatalf("schedulable count not updated: %+v", groups[0])
	}
}

func TestGatewayProviderForModelPrefersSchedulableProviderAccount(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret-key")
	provider, err := svc.CreateProvider(context.Background(), "tester", ProviderRequest{
		Name:    "Route provider",
		Type:    "openai_compatible",
		BaseURL: "https://provider.example/v1",
		Status:  ProviderStatusActive,
		Models:  []string{"gpt-4o-mini"},
		APIKey:  "provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}

	schedulable := true
	_, err = svc.CreateProviderAccount(context.Background(), "tester", ProviderAccountRequest{
		ProviderID:     provider.ID,
		Name:           "Account slow",
		Platform:       "openai_compatible",
		AuthType:       "api_key",
		Status:         AccountStatusActive,
		Schedulable:    &schedulable,
		Priority:       30,
		Concurrency:    3,
		RateMultiplier: 1,
		Models:         []string{"gpt-4o-mini"},
		Secret:         "slow-secret",
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount slow: %v", err)
	}
	fast, err := svc.CreateProviderAccount(context.Background(), "tester", ProviderAccountRequest{
		ProviderID:     provider.ID,
		Name:           "Account fast",
		Platform:       "openai_compatible",
		AuthType:       "api_key",
		Status:         AccountStatusActive,
		Schedulable:    &schedulable,
		Priority:       10,
		Concurrency:    3,
		RateMultiplier: 1,
		Models:         []string{"gpt-4o-mini"},
		Secret:         "fast-secret",
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount fast: %v", err)
	}

	selected, ok, err := svc.GatewayProviderForModel(context.Background(), "gpt-4o-mini")
	if err != nil {
		t.Fatalf("GatewayProviderForModel(): %v", err)
	}
	if !ok || selected.ID != provider.ID || selected.AccountID != fast.ID || selected.APIKey != "fast-secret" {
		t.Fatalf("unexpected selected route: %+v ok=%v", selected, ok)
	}

	accounts, err := svc.ListProviderAccounts(context.Background())
	if err != nil {
		t.Fatalf("ListProviderAccounts(): %v", err)
	}
	for _, account := range accounts {
		if account.ID == fast.ID && account.LastUsedAt == nil {
			t.Fatalf("selected account last_used_at not updated: %+v", account)
		}
	}
}

func TestCheckProviderAccountProbesModelsAndPersistsHealth(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret-key")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer account-secret" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-account"},{"id":"gpt-fast"}]}`))
	}))
	defer upstream.Close()

	provider, err := svc.CreateProvider(context.Background(), "tester", ProviderRequest{
		Name:    "Account probe provider",
		Type:    "openai_compatible",
		BaseURL: upstream.URL + "/v1",
		Status:  ProviderStatusActive,
		Models:  []string{"manual-provider-model"},
		APIKey:  "provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}

	schedulable := true
	account, err := svc.CreateProviderAccount(context.Background(), "tester", ProviderAccountRequest{
		ProviderID:     provider.ID,
		Name:           "Account probe",
		Platform:       "openai_compatible",
		AuthType:       "api_key",
		Status:         AccountStatusActive,
		Schedulable:    &schedulable,
		Priority:       20,
		Concurrency:    4,
		RateMultiplier: 1,
		Models:         []string{"manual-account-model"},
		Secret:         "account-secret",
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount(): %v", err)
	}

	check, err := svc.CheckProviderAccount(context.Background(), "tester", account.ID)
	if err != nil {
		t.Fatalf("CheckProviderAccount(): %v", err)
	}
	if check.Status != "ok" || check.ProviderID != provider.ID || len(check.Models) != 2 {
		t.Fatalf("unexpected account check: %+v", check)
	}

	health, err := svc.ListProviderAccountHealthChecks(context.Background())
	if err != nil {
		t.Fatalf("ListProviderAccountHealthChecks(): %v", err)
	}
	if len(health) != 1 || health[0].AccountID != account.ID || health[0].Status != "ok" {
		t.Fatalf("account health not persisted: %+v", health)
	}

	accounts, err := svc.ListProviderAccounts(context.Background())
	if err != nil {
		t.Fatalf("ListProviderAccounts(): %v", err)
	}
	if len(accounts) != 1 || !contains(accounts[0].Models, "manual-account-model") || !contains(accounts[0].Models, "gpt-account") {
		t.Fatalf("account discovered models not merged: %+v", accounts)
	}
}
