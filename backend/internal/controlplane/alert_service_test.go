package controlplane

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProjectBudgetAlertEscalatesAndDeduplicates(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	project, err := svc.CreateProject(ctx, "tester", ProjectRequest{
		Name:               "Budget Alert Project",
		CostCenter:         "FIN",
		MonthlyBudgetCents: 100,
		Status:             ProjectStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateProject(): %v", err)
	}
	app, err := svc.CreateApplication(ctx, "tester", ApplicationRequest{
		ProjectID:   project.ID,
		Name:        "Budget Alert App",
		Environment: "prod",
		Owner:       "platform",
		Status:      ApplicationStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateApplication(): %v", err)
	}
	created, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{
		ProjectID:         project.ID,
		ApplicationID:     app.ID,
		Name:              "Budget alert key",
		ModelAllowlist:    []string{"gpt-budget"},
		QPSLimit:          0,
		MonthlyTokenLimit: 0,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	auth, err := svc.AuthorizeGatewayModel(ctx, created.Key, "gpt-budget")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel(): %v", err)
	}
	if err := svc.RecordGatewayUsage(ctx, auth, GatewayUsageInput{Model: "gpt-budget", Status: "forwarded", CostCents: 80}); err != nil {
		t.Fatalf("RecordGatewayUsage warning(): %v", err)
	}
	alerts, err := svc.ListAlertEventsQuery(ctx, AlertQuery{Type: AlertTypeProjectBudget, Status: AlertStatusActive})
	if err != nil {
		t.Fatalf("ListAlertEventsQuery warning(): %v", err)
	}
	if len(alerts) != 1 || alerts[0].Severity != AlertSeverityWarning || alerts[0].ProjectID != project.ID {
		t.Fatalf("warning alert mismatch: %+v", alerts)
	}
	alertID := alerts[0].ID

	if err := svc.RecordGatewayUsage(ctx, auth, GatewayUsageInput{Model: "gpt-budget", Status: "forwarded", CostCents: 20}); err != nil {
		t.Fatalf("RecordGatewayUsage critical(): %v", err)
	}
	alerts, err = svc.ListAlertEventsQuery(ctx, AlertQuery{Type: AlertTypeProjectBudget, Status: AlertStatusActive})
	if err != nil {
		t.Fatalf("ListAlertEventsQuery critical(): %v", err)
	}
	if len(alerts) != 1 || alerts[0].ID != alertID || alerts[0].Severity != AlertSeverityCritical {
		t.Fatalf("critical alert should reuse existing event: before=%s after=%+v", alertID, alerts)
	}

	acknowledged, err := svc.AcknowledgeAlert(ctx, "ops", alertID)
	if err != nil {
		t.Fatalf("AcknowledgeAlert(): %v", err)
	}
	if acknowledged.Status != AlertStatusAcknowledged || acknowledged.AcknowledgedBy != "ops" || acknowledged.AcknowledgedAt == nil {
		t.Fatalf("acknowledge mismatch: %+v", acknowledged)
	}
	resolved, err := svc.ResolveAlert(ctx, "ops", alertID)
	if err != nil {
		t.Fatalf("ResolveAlert(): %v", err)
	}
	if resolved.Status != AlertStatusResolved || resolved.ResolvedBy != "ops" || resolved.ResolvedAt == nil {
		t.Fatalf("resolve mismatch: %+v", resolved)
	}
}

func TestAPIKeyQuotaAlertEscalatesAndDeduplicates(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	auth, rawKey := createAlertTestAuth(t, ctx, svc, APIKeyCreateRequest{
		Name:              "Quota alert key",
		ModelAllowlist:    []string{"gpt-quota"},
		QPSLimit:          0,
		MonthlyTokenLimit: 100,
	})
	if err := svc.RecordGatewayUsage(ctx, auth, GatewayUsageInput{
		Model:        "gpt-quota",
		Status:       "forwarded",
		InputTokens:  50,
		OutputTokens: 30,
	}); err != nil {
		t.Fatalf("RecordGatewayUsage warning(): %v", err)
	}
	alerts, err := svc.ListAlertEventsQuery(ctx, AlertQuery{Type: AlertTypeAPIKeyQuota, Status: AlertStatusActive})
	if err != nil {
		t.Fatalf("ListAlertEventsQuery warning(): %v", err)
	}
	if len(alerts) != 1 || alerts[0].Severity != AlertSeverityWarning || alerts[0].ResourceID != auth.APIKey.ID {
		t.Fatalf("quota warning alert mismatch: %+v", alerts)
	}
	alertID := alerts[0].ID

	if err := svc.RecordGatewayUsage(ctx, auth, GatewayUsageInput{
		Model:        "gpt-quota",
		Status:       "forwarded",
		InputTokens:  10,
		OutputTokens: 10,
	}); err != nil {
		t.Fatalf("RecordGatewayUsage critical(): %v", err)
	}
	if _, err := svc.AuthorizeGatewayModel(ctx, rawKey, "gpt-quota"); err != nil {
		t.Fatalf("AuthorizeGatewayModel after quota usage(): %v", err)
	}
	if err := svc.EnforceGatewayPolicy(ctx, auth); !errors.Is(err, ErrGatewayQuotaExceeded) {
		t.Fatalf("EnforceGatewayPolicy() err = %v", err)
	}
	alerts, err = svc.ListAlertEventsQuery(ctx, AlertQuery{Type: AlertTypeAPIKeyQuota, Status: AlertStatusActive})
	if err != nil {
		t.Fatalf("ListAlertEventsQuery critical(): %v", err)
	}
	if len(alerts) != 1 || alerts[0].ID != alertID || alerts[0].Severity != AlertSeverityCritical {
		t.Fatalf("quota critical alert should reuse existing event: before=%s after=%+v", alertID, alerts)
	}
}

func TestGatewayErrorRateAlertWarnsAndResolves(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	auth, _ := createAlertTestAuth(t, ctx, svc, APIKeyCreateRequest{
		Name:              "Error rate alert key",
		ModelAllowlist:    []string{"gpt-errors"},
		QPSLimit:          0,
		MonthlyTokenLimit: 0,
	})
	for i := 0; i < 8; i++ {
		if err := svc.RecordGatewayUsage(ctx, auth, GatewayUsageInput{Model: "gpt-errors", Status: "forwarded"}); err != nil {
			t.Fatalf("RecordGatewayUsage success %d: %v", i, err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := svc.RecordGatewayUsage(ctx, auth, GatewayUsageInput{Model: "gpt-errors", Status: "error", ErrorType: "upstream_error"}); err != nil {
			t.Fatalf("RecordGatewayUsage error %d: %v", i, err)
		}
	}
	active, err := svc.ListAlertEventsQuery(ctx, AlertQuery{Type: AlertTypeGatewayErrorRate, Status: AlertStatusActive})
	if err != nil {
		t.Fatalf("ListAlertEventsQuery active(): %v", err)
	}
	if len(active) != 1 || active[0].Severity != AlertSeverityWarning || active[0].ProjectID != auth.Project.ID {
		t.Fatalf("error-rate active alert mismatch: %+v", active)
	}
	if err := svc.RecordGatewayUsage(ctx, auth, GatewayUsageInput{Model: "gpt-errors", Status: "forwarded"}); err != nil {
		t.Fatalf("RecordGatewayUsage recovery(): %v", err)
	}
	resolved, err := svc.ListAlertEventsQuery(ctx, AlertQuery{Type: AlertTypeGatewayErrorRate, Status: AlertStatusResolved})
	if err != nil {
		t.Fatalf("ListAlertEventsQuery resolved(): %v", err)
	}
	if len(resolved) != 1 || resolved[0].ResourceID != auth.Project.ID || resolved[0].ResolvedBy != systemActor {
		t.Fatalf("error-rate resolved alert mismatch: %+v", resolved)
	}
}

func TestProviderHealthAlertResolvesAfterRecovery(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret-key")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer upstream-secret" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-real"}]}`))
	}))
	defer upstream.Close()

	provider, err := svc.CreateProvider(ctx, "tester", ProviderRequest{
		Name:    "Recovering provider",
		Type:    "openai_compatible",
		BaseURL: upstream.URL + "/v1",
		Status:  ProviderStatusActive,
		Models:  []string{"manual-model"},
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	warningCheck, err := svc.CheckProvider(ctx, "tester", provider.ID)
	if err != nil {
		t.Fatalf("CheckProvider warning(): %v", err)
	}
	if warningCheck.Status != "warning" {
		t.Fatalf("warning check status = %+v", warningCheck)
	}
	active, err := svc.ListAlertEventsQuery(ctx, AlertQuery{Type: AlertTypeProviderHealth, Status: AlertStatusActive})
	if err != nil {
		t.Fatalf("ListAlertEventsQuery active(): %v", err)
	}
	if len(active) != 1 || active[0].ResourceID != provider.ID || active[0].Severity != AlertSeverityWarning {
		t.Fatalf("provider active alert mismatch: %+v", active)
	}

	updated, err := svc.UpdateProvider(ctx, "tester", provider.ID, ProviderRequest{
		Name:    "Recovering provider",
		Type:    "openai_compatible",
		BaseURL: upstream.URL + "/v1",
		Status:  ProviderStatusActive,
		Models:  []string{"manual-model"},
		APIKey:  "upstream-secret",
	})
	if err != nil {
		t.Fatalf("UpdateProvider(): %v", err)
	}
	okCheck, err := svc.CheckProvider(ctx, "tester", updated.ID)
	if err != nil {
		t.Fatalf("CheckProvider ok(): %v", err)
	}
	if okCheck.Status != "ok" {
		t.Fatalf("ok check status = %+v", okCheck)
	}
	resolved, err := svc.ListAlertEventsQuery(ctx, AlertQuery{Type: AlertTypeProviderHealth, Status: AlertStatusResolved})
	if err != nil {
		t.Fatalf("ListAlertEventsQuery resolved(): %v", err)
	}
	if len(resolved) != 1 || resolved[0].ResourceID != provider.ID || resolved[0].ResolvedBy != systemActor {
		t.Fatalf("provider resolved alert mismatch: %+v", resolved)
	}
}

func createAlertTestAuth(t *testing.T, ctx context.Context, svc *Service, req APIKeyCreateRequest) (GatewayAuthContext, string) {
	t.Helper()
	project, err := svc.CreateProject(ctx, "tester", ProjectRequest{
		Name:               req.Name + " project",
		CostCenter:         "ALERT",
		MonthlyBudgetCents: 0,
		Status:             ProjectStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateProject(): %v", err)
	}
	app, err := svc.CreateApplication(ctx, "tester", ApplicationRequest{
		ProjectID:   project.ID,
		Name:        req.Name + " app",
		Environment: "prod",
		Owner:       "platform",
		Status:      ApplicationStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateApplication(): %v", err)
	}
	req.ProjectID = project.ID
	req.ApplicationID = app.ID
	created, err := svc.CreateAPIKey(ctx, "tester", req)
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	auth, err := svc.AuthorizeGatewayModel(ctx, created.Key, req.ModelAllowlist[0])
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel(): %v", err)
	}
	return auth, created.Key
}
