package server

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/config"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/plugins"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/astercloud/asterrouter/backend/internal/system"
)

func newTestRuntime(t *testing.T, cfg config.Config) (http.Handler, *controlplane.Service) {
	t.Helper()
	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"})
	controlService := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	if err := controlService.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}
	pluginService := plugins.NewService(plugins.NewMemoryRepository())
	if err := pluginService.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("Plugin EnsureSeedData(): %v", err)
	}
	systemService := system.NewService(system.Config{Version: "test", BuildType: "source"})
	return New(Options{Config: cfg, SettingsService: settingsService, ControlService: controlService, PluginService: pluginService, SystemService: systemService}), controlService
}

func newTestHandler(t *testing.T, cfg config.Config) http.Handler {
	t.Helper()
	handler, _ := newTestRuntime(t, cfg)
	return handler
}

func newAuthTestHandler(t *testing.T) http.Handler {
	t.Helper()
	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"})
	controlService := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	if err := controlService.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}
	pluginService := plugins.NewService(plugins.NewMemoryRepository())
	if err := pluginService.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("Plugin EnsureSeedData(): %v", err)
	}
	return New(Options{
		Config:          config.Config{},
		AuthService:     auth.NewService(auth.Config{Username: "admin", Password: "secret", SecretKey: "test-secret"}),
		SettingsService: settingsService,
		ControlService:  controlService,
		PluginService:   pluginService,
		SystemService:   system.NewService(system.Config{Version: "test", BuildType: "source"}),
	})
}

func TestPublicSettingsEndpoint(t *testing.T) {
	handler := newTestHandler(t, config.Config{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings/public", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code int                     `json:"code"`
		Data settings.PublicSettings `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.SiteName != "AsterRouter" {
		t.Fatalf("site_name = %q", resp.Data.SiteName)
	}
}

func TestAdminSettingsRequiresToken(t *testing.T) {
	handler := newTestHandler(t, config.Config{AdminToken: "secret"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminSettingsRequiresLoginWhenAuthServiceEnabled(t *testing.T) {
	handler := newAuthTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLoginAllowsAdminSettingsAccess(t *testing.T) {
	handler := newAuthTestHandler(t)

	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"secret"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", loginRec.Code, loginRec.Body.String())
	}
	var loginResp struct {
		Data auth.LoginResult `json:"data"`
	}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if loginResp.Data.AccessToken == "" {
		t.Fatalf("empty access token: %+v", loginResp.Data)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	req.Header.Set("Authorization", "Bearer "+loginResp.Data.AccessToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("settings status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSetupProfileEndpoint(t *testing.T) {
	repo := settings.NewMemoryRepository()
	svc := settings.NewService(repo, settings.ServiceOptions{Version: "test", StorageMode: "memory"})
	controlService := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	pluginService := plugins.NewService(plugins.NewMemoryRepository())
	if err := pluginService.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("Plugin EnsureSeedData(): %v", err)
	}
	systemService := system.NewService(system.Config{Version: "test", BuildType: "source"})
	handler := New(Options{Config: config.Config{}, SettingsService: svc, ControlService: controlService, PluginService: pluginService, SystemService: systemService})

	body := bytes.NewBufferString(`{"profile":"enterprise"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/profile", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	got, err := svc.Admin(context.Background())
	if err != nil {
		t.Fatalf("Admin(): %v", err)
	}
	if got.Profile != "enterprise" || !got.SetupCompleted {
		t.Fatalf("setup not persisted: %+v", got)
	}
}

func TestAdminDashboardEndpoint(t *testing.T) {
	handler := newTestHandler(t, config.Config{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/dashboard", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code int                    `json:"code"`
		Data controlplane.Dashboard `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.ProviderCount != 1 || resp.Data.ProjectCount != 1 {
		t.Fatalf("unexpected dashboard: %+v", resp.Data)
	}
}

func TestAdminProjectAndApplicationUpdateEndpoints(t *testing.T) {
	handler := newTestHandler(t, config.Config{})

	projectBody := bytes.NewBufferString(`{"name":"Finance AI","description":"finance sandbox","cost_center":"FIN","monthly_budget_cents":12000,"status":"active"}`)
	projectReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/projects", projectBody)
	projectReq.Header.Set("Content-Type", "application/json")
	projectRec := httptest.NewRecorder()
	handler.ServeHTTP(projectRec, projectReq)
	if projectRec.Code != http.StatusOK {
		t.Fatalf("project create status = %d body=%s", projectRec.Code, projectRec.Body.String())
	}
	var projectResp struct {
		Data controlplane.Project `json:"data"`
	}
	if err := json.Unmarshal(projectRec.Body.Bytes(), &projectResp); err != nil {
		t.Fatalf("decode project create: %v", err)
	}

	updateProjectBody := bytes.NewBufferString(`{"name":"Finance AI Updated","description":"finance prod","cost_center":"FIN-OPS","monthly_budget_cents":36000,"status":"archived"}`)
	updateProjectReq := httptest.NewRequest(http.MethodPut, "/api/v1/admin/projects/"+projectResp.Data.ID, updateProjectBody)
	updateProjectReq.Header.Set("Content-Type", "application/json")
	updateProjectRec := httptest.NewRecorder()
	handler.ServeHTTP(updateProjectRec, updateProjectReq)
	if updateProjectRec.Code != http.StatusOK {
		t.Fatalf("project update status = %d body=%s", updateProjectRec.Code, updateProjectRec.Body.String())
	}
	var updatedProjectResp struct {
		Data controlplane.Project `json:"data"`
	}
	if err := json.Unmarshal(updateProjectRec.Body.Bytes(), &updatedProjectResp); err != nil {
		t.Fatalf("decode project update: %v", err)
	}
	if updatedProjectResp.Data.ID != projectResp.Data.ID || updatedProjectResp.Data.Name != "Finance AI Updated" || updatedProjectResp.Data.Status != controlplane.ProjectStatusArchived {
		t.Fatalf("unexpected updated project: %+v", updatedProjectResp.Data)
	}

	appBody := bytes.NewBufferString(`{"name":"Budget Bot","environment":"dev","owner":"finance","status":"active"}`)
	appReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/projects/"+projectResp.Data.ID+"/applications", appBody)
	appReq.Header.Set("Content-Type", "application/json")
	appRec := httptest.NewRecorder()
	handler.ServeHTTP(appRec, appReq)
	if appRec.Code != http.StatusOK {
		t.Fatalf("app create status = %d body=%s", appRec.Code, appRec.Body.String())
	}
	var appResp struct {
		Data controlplane.Application `json:"data"`
	}
	if err := json.Unmarshal(appRec.Body.Bytes(), &appResp); err != nil {
		t.Fatalf("decode app create: %v", err)
	}

	updateAppBody := bytes.NewBufferString(`{"project_id":"` + projectResp.Data.ID + `","name":"Budget Bot API","environment":"prod","owner":"platform","status":"disabled"}`)
	updateAppReq := httptest.NewRequest(http.MethodPut, "/api/v1/admin/applications/"+appResp.Data.ID, updateAppBody)
	updateAppReq.Header.Set("Content-Type", "application/json")
	updateAppRec := httptest.NewRecorder()
	handler.ServeHTTP(updateAppRec, updateAppReq)
	if updateAppRec.Code != http.StatusOK {
		t.Fatalf("app update status = %d body=%s", updateAppRec.Code, updateAppRec.Body.String())
	}
	var updatedAppResp struct {
		Data controlplane.Application `json:"data"`
	}
	if err := json.Unmarshal(updateAppRec.Body.Bytes(), &updatedAppResp); err != nil {
		t.Fatalf("decode app update: %v", err)
	}
	if updatedAppResp.Data.ID != appResp.Data.ID || updatedAppResp.Data.Name != "Budget Bot API" || updatedAppResp.Data.Status != controlplane.ApplicationStatusDisabled {
		t.Fatalf("unexpected updated app: %+v", updatedAppResp.Data)
	}
}

func TestAdminRecordEndpointsSupportQueryParameters(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		ProjectID:         "proj_platform",
		ApplicationID:     "app_internal_sandbox",
		Name:              "query key",
		ModelAllowlist:    []string{"model-a", "model-b"},
		QPSLimit:          0,
		MonthlyTokenLimit: 0,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	auth, err := control.AuthorizeGatewayModel(context.Background(), created.Key, "model-a")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel(): %v", err)
	}
	if err := control.RecordGatewayUsage(context.Background(), auth, controlplane.GatewayUsageInput{Model: "model-a", Status: "forwarded", ProviderID: "provider-a", InputTokens: 1}); err != nil {
		t.Fatalf("RecordGatewayUsage a: %v", err)
	}
	if err := control.RecordGatewayUsage(context.Background(), auth, controlplane.GatewayUsageInput{Model: "model-b", Status: "error", ProviderID: "provider-b", ErrorType: "policy_error", InputTokens: 2}); err != nil {
		t.Fatalf("RecordGatewayUsage b: %v", err)
	}
	if err := control.RecordGatewayTrace(context.Background(), auth, controlplane.GatewayTraceInput{Model: "model-a", Status: "forwarded", ProviderID: "provider-a", ResponseSummary: "ok"}); err != nil {
		t.Fatalf("RecordGatewayTrace a: %v", err)
	}
	if err := control.RecordGatewayTrace(context.Background(), auth, controlplane.GatewayTraceInput{Model: "model-b", Status: "error", ProviderID: "provider-b", ErrorType: "policy_error", ResponseSummary: "blocked"}); err != nil {
		t.Fatalf("RecordGatewayTrace b: %v", err)
	}
	other, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		ProjectID:         "proj_platform",
		ApplicationID:     "app_internal_sandbox",
		Name:              "other query key",
		ModelAllowlist:    []string{"model-a"},
		QPSLimit:          0,
		MonthlyTokenLimit: 0,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey other(): %v", err)
	}
	otherAuth, err := control.AuthorizeGatewayModel(context.Background(), other.Key, "model-a")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel other(): %v", err)
	}
	if err := control.RecordGatewayUsage(context.Background(), otherAuth, controlplane.GatewayUsageInput{Model: "model-a", Status: "forwarded", ProviderID: "provider-other", InputTokens: 3}); err != nil {
		t.Fatalf("RecordGatewayUsage other: %v", err)
	}
	if err := control.RecordGatewayTrace(context.Background(), otherAuth, controlplane.GatewayTraceInput{Model: "model-a", Status: "forwarded", ProviderID: "provider-other", ResponseSummary: "other"}); err != nil {
		t.Fatalf("RecordGatewayTrace other: %v", err)
	}
	if err := control.RecordGatewayCall(context.Background(), auth, "model-a", "forwarded", "Pagination query audit marker"); err != nil {
		t.Fatalf("RecordGatewayCall(): %v", err)
	}

	usageReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/usage?model=model-b&status=error&limit=1", nil)
	usageRec := httptest.NewRecorder()
	handler.ServeHTTP(usageRec, usageReq)
	if usageRec.Code != http.StatusOK {
		t.Fatalf("usage status = %d body=%s", usageRec.Code, usageRec.Body.String())
	}
	var usageResp struct {
		Data controlplane.UsageReport `json:"data"`
	}
	if err := json.Unmarshal(usageRec.Body.Bytes(), &usageResp); err != nil {
		t.Fatalf("decode usage: %v", err)
	}
	if len(usageResp.Data.Recent) != 1 || usageResp.Data.Recent[0].Model != "model-b" || usageResp.Data.Recent[0].Status != "error" {
		t.Fatalf("usage query not applied: %+v", usageResp.Data.Recent)
	}

	usageKeyReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/usage?api_key_id="+url.QueryEscape(created.Record.ID)+"&limit=10", nil)
	usageKeyRec := httptest.NewRecorder()
	handler.ServeHTTP(usageKeyRec, usageKeyReq)
	if usageKeyRec.Code != http.StatusOK {
		t.Fatalf("usage key status = %d body=%s", usageKeyRec.Code, usageKeyRec.Body.String())
	}
	var usageKeyResp struct {
		Data controlplane.UsageReport `json:"data"`
	}
	if err := json.Unmarshal(usageKeyRec.Body.Bytes(), &usageKeyResp); err != nil {
		t.Fatalf("decode usage key: %v", err)
	}
	if len(usageKeyResp.Data.Recent) != 2 || usageKeyResp.Data.TotalRequests != 2 {
		t.Fatalf("usage api_key_id filter count mismatch: %+v", usageKeyResp.Data)
	}
	for _, record := range usageKeyResp.Data.Recent {
		if record.APIKeyID != created.Record.ID {
			t.Fatalf("usage api_key_id leaked another key: %+v", usageKeyResp.Data.Recent)
		}
	}

	traceReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/gateway-traces?status=error&q=provider-b", nil)
	traceRec := httptest.NewRecorder()
	handler.ServeHTTP(traceRec, traceReq)
	if traceRec.Code != http.StatusOK {
		t.Fatalf("trace status = %d body=%s", traceRec.Code, traceRec.Body.String())
	}
	var traceResp struct {
		Data []controlplane.GatewayTrace `json:"data"`
	}
	if err := json.Unmarshal(traceRec.Body.Bytes(), &traceResp); err != nil {
		t.Fatalf("decode traces: %v", err)
	}
	if len(traceResp.Data) != 1 || traceResp.Data[0].ProviderID != "provider-b" || traceResp.Data[0].Status != "error" {
		t.Fatalf("trace query not applied: %+v", traceResp.Data)
	}

	traceKeyReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/gateway-traces?api_key_id="+url.QueryEscape(created.Record.ID)+"&limit=10", nil)
	traceKeyRec := httptest.NewRecorder()
	handler.ServeHTTP(traceKeyRec, traceKeyReq)
	if traceKeyRec.Code != http.StatusOK {
		t.Fatalf("trace key status = %d body=%s", traceKeyRec.Code, traceKeyRec.Body.String())
	}
	var traceKeyResp struct {
		Data []controlplane.GatewayTrace `json:"data"`
	}
	if err := json.Unmarshal(traceKeyRec.Body.Bytes(), &traceKeyResp); err != nil {
		t.Fatalf("decode trace key: %v", err)
	}
	if len(traceKeyResp.Data) != 2 {
		t.Fatalf("trace api_key_id filter count mismatch: %+v", traceKeyResp.Data)
	}
	for _, trace := range traceKeyResp.Data {
		if trace.APIKeyID != created.Record.ID {
			t.Fatalf("trace api_key_id leaked another key: %+v", traceKeyResp.Data)
		}
	}

	traceSummaryReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/gateway-traces/summary?limit=1", nil)
	traceSummaryRec := httptest.NewRecorder()
	handler.ServeHTTP(traceSummaryRec, traceSummaryReq)
	if traceSummaryRec.Code != http.StatusOK {
		t.Fatalf("trace summary status = %d body=%s", traceSummaryRec.Code, traceSummaryRec.Body.String())
	}
	var traceSummaryResp struct {
		Data controlplane.GatewayTraceSummary `json:"data"`
	}
	if err := json.Unmarshal(traceSummaryRec.Body.Bytes(), &traceSummaryResp); err != nil {
		t.Fatalf("decode trace summary: %v", err)
	}
	if traceSummaryResp.Data.Total != 3 || traceSummaryResp.Data.Routed != 3 || traceSummaryResp.Data.Errors != 1 {
		t.Fatalf("trace summary should ignore pagination and include matching records: %+v", traceSummaryResp.Data)
	}

	traceKeySummaryReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/gateway-traces/summary?api_key_id="+url.QueryEscape(created.Record.ID)+"&limit=1", nil)
	traceKeySummaryRec := httptest.NewRecorder()
	handler.ServeHTTP(traceKeySummaryRec, traceKeySummaryReq)
	if traceKeySummaryRec.Code != http.StatusOK {
		t.Fatalf("trace key summary status = %d body=%s", traceKeySummaryRec.Code, traceKeySummaryRec.Body.String())
	}
	var traceKeySummaryResp struct {
		Data controlplane.GatewayTraceSummary `json:"data"`
	}
	if err := json.Unmarshal(traceKeySummaryRec.Body.Bytes(), &traceKeySummaryResp); err != nil {
		t.Fatalf("decode trace key summary: %v", err)
	}
	if traceKeySummaryResp.Data.Total != 2 || traceKeySummaryResp.Data.Routed != 2 || traceKeySummaryResp.Data.Errors != 1 {
		t.Fatalf("trace key summary mismatch: %+v", traceKeySummaryResp.Data)
	}

	auditReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit-logs?action=invoke&q=Pagination", nil)
	auditRec := httptest.NewRecorder()
	handler.ServeHTTP(auditRec, auditReq)
	if auditRec.Code != http.StatusOK {
		t.Fatalf("audit status = %d body=%s", auditRec.Code, auditRec.Body.String())
	}
	var auditResp struct {
		Data []controlplane.AuditLog `json:"data"`
	}
	if err := json.Unmarshal(auditRec.Body.Bytes(), &auditResp); err != nil {
		t.Fatalf("decode audit: %v", err)
	}
	if len(auditResp.Data) != 1 || auditResp.Data[0].Action != "invoke" || !strings.Contains(auditResp.Data[0].Summary, "Pagination") {
		t.Fatalf("audit query not applied: %+v", auditResp.Data)
	}

	auditSummaryReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit-logs/summary?action=invoke&limit=1", nil)
	auditSummaryRec := httptest.NewRecorder()
	handler.ServeHTTP(auditSummaryRec, auditSummaryReq)
	if auditSummaryRec.Code != http.StatusOK {
		t.Fatalf("audit summary status = %d body=%s", auditSummaryRec.Code, auditSummaryRec.Body.String())
	}
	var auditSummaryResp struct {
		Data controlplane.AuditLogSummary `json:"data"`
	}
	if err := json.Unmarshal(auditSummaryRec.Body.Bytes(), &auditSummaryResp); err != nil {
		t.Fatalf("decode audit summary: %v", err)
	}
	if auditSummaryResp.Data.Total != 1 || auditSummaryResp.Data.Actors != 1 || auditSummaryResp.Data.Resources != 1 || auditSummaryResp.Data.Actions != 1 {
		t.Fatalf("audit summary mismatch: %+v", auditSummaryResp.Data)
	}

	future := url.QueryEscape(time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano))
	usageTimeReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/usage?from="+future, nil)
	usageTimeRec := httptest.NewRecorder()
	handler.ServeHTTP(usageTimeRec, usageTimeReq)
	if usageTimeRec.Code != http.StatusOK {
		t.Fatalf("usage time status = %d body=%s", usageTimeRec.Code, usageTimeRec.Body.String())
	}
	var usageTimeResp struct {
		Data controlplane.UsageReport `json:"data"`
	}
	if err := json.Unmarshal(usageTimeRec.Body.Bytes(), &usageTimeResp); err != nil {
		t.Fatalf("decode usage time: %v", err)
	}
	if len(usageTimeResp.Data.Recent) != 0 {
		t.Fatalf("usage time range not applied: %+v", usageTimeResp.Data.Recent)
	}

	traceTimeReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/gateway-traces?from="+future, nil)
	traceTimeRec := httptest.NewRecorder()
	handler.ServeHTTP(traceTimeRec, traceTimeReq)
	if traceTimeRec.Code != http.StatusOK {
		t.Fatalf("trace time status = %d body=%s", traceTimeRec.Code, traceTimeRec.Body.String())
	}
	var traceTimeResp struct {
		Data []controlplane.GatewayTrace `json:"data"`
	}
	if err := json.Unmarshal(traceTimeRec.Body.Bytes(), &traceTimeResp); err != nil {
		t.Fatalf("decode trace time: %v", err)
	}
	if len(traceTimeResp.Data) != 0 {
		t.Fatalf("trace time range not applied: %+v", traceTimeResp.Data)
	}

	auditTimeReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit-logs?from="+future, nil)
	auditTimeRec := httptest.NewRecorder()
	handler.ServeHTTP(auditTimeRec, auditTimeReq)
	if auditTimeRec.Code != http.StatusOK {
		t.Fatalf("audit time status = %d body=%s", auditTimeRec.Code, auditTimeRec.Body.String())
	}
	var auditTimeResp struct {
		Data []controlplane.AuditLog `json:"data"`
	}
	if err := json.Unmarshal(auditTimeRec.Body.Bytes(), &auditTimeResp); err != nil {
		t.Fatalf("decode audit time: %v", err)
	}
	if len(auditTimeResp.Data) != 0 {
		t.Fatalf("audit time range not applied: %+v", auditTimeResp.Data)
	}
}

func TestAdminRecordExportEndpointsSupportQueryParameters(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		ProjectID:         "proj_platform",
		ApplicationID:     "app_internal_sandbox",
		Name:              "export key",
		ModelAllowlist:    []string{"model-a", "model-b"},
		QPSLimit:          0,
		MonthlyTokenLimit: 0,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	auth, err := control.AuthorizeGatewayModel(context.Background(), created.Key, "model-a")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel(): %v", err)
	}
	if err := control.RecordGatewayUsage(context.Background(), auth, controlplane.GatewayUsageInput{Model: "model-a", Status: "forwarded", ProviderID: "provider-a", InputTokens: 1}); err != nil {
		t.Fatalf("RecordGatewayUsage a: %v", err)
	}
	if err := control.RecordGatewayUsage(context.Background(), auth, controlplane.GatewayUsageInput{Model: "model-b", Status: "error", ProviderID: "provider-b", ErrorType: "policy_error", InputTokens: 2}); err != nil {
		t.Fatalf("RecordGatewayUsage b: %v", err)
	}
	if err := control.RecordGatewayTrace(context.Background(), auth, controlplane.GatewayTraceInput{Model: "model-a", Status: "forwarded", ProviderID: "provider-a", ResponseSummary: "ok"}); err != nil {
		t.Fatalf("RecordGatewayTrace a: %v", err)
	}
	if err := control.RecordGatewayTrace(context.Background(), auth, controlplane.GatewayTraceInput{Model: "model-b", Status: "error", ProviderID: "provider-b", ErrorType: "policy_error", ResponseSummary: "export blocked"}); err != nil {
		t.Fatalf("RecordGatewayTrace b: %v", err)
	}
	if err := control.RecordGatewayCall(context.Background(), auth, "model-b", "error", "Export query audit marker"); err != nil {
		t.Fatalf("RecordGatewayCall(): %v", err)
	}

	usageReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/usage/export?model=model-b&status=error&limit=10", nil)
	usageRec := httptest.NewRecorder()
	handler.ServeHTTP(usageRec, usageReq)
	usageRows := readCSVRows(t, usageRec)
	if len(usageRows) != 2 || usageRows[0][5] != "model" || usageRows[1][5] != "model-b" || usageRows[1][8] != "error" {
		t.Fatalf("usage export query not applied: %+v", usageRows)
	}
	if strings.Contains(usageRec.Body.String(), "model-a") {
		t.Fatalf("usage export leaked filtered record: %s", usageRec.Body.String())
	}

	traceReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/gateway-traces/export?status=error&q=blocked&limit=10", nil)
	traceRec := httptest.NewRecorder()
	handler.ServeHTTP(traceRec, traceReq)
	traceRows := readCSVRows(t, traceRec)
	if len(traceRows) != 2 || traceRows[0][5] != "model" || traceRows[1][5] != "model-b" || traceRows[1][11] != "error" {
		t.Fatalf("trace export query not applied: %+v", traceRows)
	}
	if strings.Contains(traceRec.Body.String(), "provider-a") {
		t.Fatalf("trace export leaked filtered record: %s", traceRec.Body.String())
	}

	auditReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit-logs/export?action=invoke&resource_type=gateway_call&q=Export&limit=10", nil)
	auditRec := httptest.NewRecorder()
	handler.ServeHTTP(auditRec, auditReq)
	auditRows := readCSVRows(t, auditRec)
	if len(auditRows) != 2 || auditRows[0][2] != "action" || auditRows[1][2] != "invoke" || !strings.Contains(auditRows[1][5], "Export") {
		t.Fatalf("audit export query not applied: %+v", auditRows)
	}
}

func TestAdminAsyncExportJobLifecycle(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		ProjectID:         "proj_platform",
		ApplicationID:     "app_internal_sandbox",
		Name:              "async export key",
		ModelAllowlist:    []string{"model-a"},
		QPSLimit:          0,
		MonthlyTokenLimit: 0,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	auth, err := control.AuthorizeGatewayModel(context.Background(), created.Key, "model-a")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel(): %v", err)
	}
	if err := control.RecordGatewayCall(context.Background(), auth, "model-a", "forwarded", "AsyncExport marker"); err != nil {
		t.Fatalf("RecordGatewayCall(): %v", err)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/export-jobs?kind=audit_logs&action=invoke&q=AsyncExport&limit=10", nil)
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var createResp struct {
		Data csvExportJob `json:"data"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if createResp.Data.ID == "" || createResp.Data.Kind != "audit_logs" {
		t.Fatalf("unexpected created job: %+v", createResp.Data)
	}

	job := waitExportJob(t, handler, createResp.Data.ID)
	if job.Status != exportJobStatusSucceeded || job.RowCount != 1 || job.SizeBytes == 0 {
		t.Fatalf("job did not succeed with expected metadata: %+v", job)
	}

	downloadReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/export-jobs/"+job.ID+"/download", nil)
	downloadRec := httptest.NewRecorder()
	handler.ServeHTTP(downloadRec, downloadReq)
	rows := readCSVRows(t, downloadRec)
	if len(rows) != 2 || rows[1][2] != "invoke" || !strings.Contains(rows[1][5], "AsyncExport") {
		t.Fatalf("async export CSV mismatch: %+v", rows)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/export-jobs?limit=5", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), job.ID) {
		t.Fatalf("list missing job status=%d body=%s", listRec.Code, listRec.Body.String())
	}

	badReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/export-jobs?kind=unknown", nil)
	badRec := httptest.NewRecorder()
	handler.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("bad kind status = %d body=%s", badRec.Code, badRec.Body.String())
	}
}

func waitExportJob(t *testing.T, handler http.Handler, id string) csvExportJob {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var last csvExportJob
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/export-jobs/"+id, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("job status = %d body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Data csvExportJob `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode job: %v", err)
		}
		last = resp.Data
		if last.Status == exportJobStatusSucceeded || last.Status == exportJobStatusFailed {
			return last
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("export job did not finish: %+v", last)
	return csvExportJob{}
}

func readCSVRows(t *testing.T, rec *httptest.ResponseRecorder) [][]string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("csv status = %d body=%s", rec.Code, rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/csv") {
		t.Fatalf("csv content-type = %q", contentType)
	}
	rows, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v body=%s", err, rec.Body.String())
	}
	return rows
}

func TestCreateAPIKeyEndpoint(t *testing.T) {
	handler := newTestHandler(t, config.Config{})

	body := bytes.NewBufferString(`{"project_id":"proj_platform","application_id":"app_internal_sandbox","name":"demo","model_allowlist":["gpt-4o-mini"],"qps_limit":2,"monthly_token_limit":1000}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/api-keys", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code int                               `json:"code"`
		Data controlplane.APIKeyCreateResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Key == "" || resp.Data.Record.Fingerprint == "" {
		t.Fatalf("api key response incomplete: %+v", resp.Data)
	}
}

func TestUpdateProviderEndpointKeepsExistingSecret(t *testing.T) {
	handler := newTestHandler(t, config.Config{})

	createBody := bytes.NewBufferString(`{"name":"Vendor A","type":"openai_compatible","base_url":"https://example.com/v1","status":"active","models":["gpt-4o-mini"],"priority":10,"api_key":"sk-test-123456"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/providers", createBody)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var createResp struct {
		Data controlplane.ProviderConnection `json:"data"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	updateBody := bytes.NewBufferString(`{"name":"Vendor A Updated","type":"openai_compatible","base_url":"https://example.com/v1","status":"active","models":["gpt-4o-mini","gpt-4.1-mini"],"priority":20,"api_key":""}`)
	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/admin/providers/"+createResp.Data.ID, updateBody)
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	handler.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", updateRec.Code, updateRec.Body.String())
	}
	var updateResp struct {
		Data controlplane.ProviderConnection `json:"data"`
	}
	if err := json.Unmarshal(updateRec.Body.Bytes(), &updateResp); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if updateResp.Data.Status != controlplane.ProviderStatusActive || !updateResp.Data.SecretConfigured {
		t.Fatalf("secret/status not preserved: %+v", updateResp.Data)
	}
}

func TestCheckProviderEndpoint(t *testing.T) {
	handler := newTestHandler(t, config.Config{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/providers/prov_openai_compatible/check", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data controlplane.ProviderHealthCheck `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.ProviderID != "prov_openai_compatible" || resp.Data.Status == "" || resp.Data.Message == "" {
		t.Fatalf("incomplete check response: %+v", resp.Data)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/provider-health-checks", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listResp struct {
		Data []controlplane.ProviderHealthCheck `json:"data"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.Data) != 1 || listResp.Data[0].ProviderID != "prov_openai_compatible" {
		t.Fatalf("health list missing check: %+v", listResp.Data)
	}
}

func TestAdminRoutingGroupsAndProviderAccountsEndpoints(t *testing.T) {
	handler := newTestHandler(t, config.Config{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer account-secret" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-account"}]}`))
	}))
	defer upstream.Close()

	groupBody := bytes.NewBufferString(`{"name":"OpenAI default","platform":"openai_compatible","rate_multiplier":1,"status":"active","sort_order":10}`)
	groupReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/routing-groups", groupBody)
	groupReq.Header.Set("Content-Type", "application/json")
	groupRec := httptest.NewRecorder()
	handler.ServeHTTP(groupRec, groupReq)
	if groupRec.Code != http.StatusOK {
		t.Fatalf("group status = %d body=%s", groupRec.Code, groupRec.Body.String())
	}
	var groupResp struct {
		Data controlplane.RoutingGroup `json:"data"`
	}
	if err := json.Unmarshal(groupRec.Body.Bytes(), &groupResp); err != nil {
		t.Fatalf("decode group: %v", err)
	}
	if groupResp.Data.ID == "" {
		t.Fatalf("group id missing: %+v", groupResp.Data)
	}

	providerPayload := `{"name":"Account Provider","type":"openai_compatible","base_url":"` + upstream.URL + `/v1","status":"active","models":["gpt-4o-mini"],"priority":10,"api_key":"provider-secret"}`
	providerReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/providers", bytes.NewBufferString(providerPayload))
	providerReq.Header.Set("Content-Type", "application/json")
	providerRec := httptest.NewRecorder()
	handler.ServeHTTP(providerRec, providerReq)
	if providerRec.Code != http.StatusOK {
		t.Fatalf("provider status = %d body=%s", providerRec.Code, providerRec.Body.String())
	}
	var providerResp struct {
		Data controlplane.ProviderConnection `json:"data"`
	}
	if err := json.Unmarshal(providerRec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("decode provider: %v", err)
	}

	accountPayload := `{"provider_id":"` + providerResp.Data.ID + `","name":"Account A","platform":"openai_compatible","auth_type":"api_key","status":"active","schedulable":true,"priority":10,"concurrency":3,"rate_multiplier":1,"models":["gpt-4o-mini"],"group_ids":["` + groupResp.Data.ID + `"],"secret":"account-secret"}`
	accountReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/provider-accounts", bytes.NewBufferString(accountPayload))
	accountReq.Header.Set("Content-Type", "application/json")
	accountRec := httptest.NewRecorder()
	handler.ServeHTTP(accountRec, accountReq)
	if accountRec.Code != http.StatusOK {
		t.Fatalf("account status = %d body=%s", accountRec.Code, accountRec.Body.String())
	}
	var accountResp struct {
		Data controlplane.ProviderAccount `json:"data"`
	}
	if err := json.Unmarshal(accountRec.Body.Bytes(), &accountResp); err != nil {
		t.Fatalf("decode account: %v", err)
	}
	if !accountResp.Data.SecretConfigured || accountResp.Data.SecretHint == "" {
		t.Fatalf("account secret metadata missing: %+v", accountResp.Data)
	}
	if accountResp.Data.ProviderID != providerResp.Data.ID {
		t.Fatalf("account provider binding missing: %+v", accountResp.Data)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/provider-accounts", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listResp struct {
		Data []controlplane.ProviderAccount `json:"data"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.Data) != 1 || listResp.Data[0].GroupIDs[0] != groupResp.Data.ID {
		t.Fatalf("unexpected account list: %+v", listResp.Data)
	}

	checkReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/provider-accounts/"+accountResp.Data.ID+"/check", nil)
	checkRec := httptest.NewRecorder()
	handler.ServeHTTP(checkRec, checkReq)
	if checkRec.Code != http.StatusOK {
		t.Fatalf("account check status = %d body=%s", checkRec.Code, checkRec.Body.String())
	}
	var checkResp struct {
		Data controlplane.ProviderAccountHealthCheck `json:"data"`
	}
	if err := json.Unmarshal(checkRec.Body.Bytes(), &checkResp); err != nil {
		t.Fatalf("decode account check: %v", err)
	}
	if checkResp.Data.Status != "ok" || checkResp.Data.AccountID != accountResp.Data.ID {
		t.Fatalf("unexpected account check: %+v", checkResp.Data)
	}

	healthReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/provider-account-health-checks", nil)
	healthRec := httptest.NewRecorder()
	handler.ServeHTTP(healthRec, healthReq)
	if healthRec.Code != http.StatusOK {
		t.Fatalf("account health list status = %d body=%s", healthRec.Code, healthRec.Body.String())
	}
	var healthResp struct {
		Data []controlplane.ProviderAccountHealthCheck `json:"data"`
	}
	if err := json.Unmarshal(healthRec.Body.Bytes(), &healthResp); err != nil {
		t.Fatalf("decode account health list: %v", err)
	}
	if len(healthResp.Data) != 1 || healthResp.Data[0].AccountID != accountResp.Data.ID {
		t.Fatalf("account health list missing check: %+v", healthResp.Data)
	}
}

func TestAdminSystemCheckUpdatesEndpoint(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/check-updates?force=true", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code int               `json:"code"`
		Data system.UpdateInfo `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.CurrentVersion != "test" || resp.Data.Warning == "" {
		t.Fatalf("unexpected update info: %+v", resp.Data)
	}
	audit, err := control.ListAuditLogs(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListAuditLogs(): %v", err)
	}
	for _, event := range audit {
		if event.ResourceType == "system" && event.Action == "check_update" {
			return
		}
	}
	t.Fatalf("system update audit event not found: %+v", audit)
}

func TestAdminSystemUpdateWithoutManifestRequiresManualConfiguration(t *testing.T) {
	handler, _ := newTestRuntime(t, config.Config{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/update", nil)
	req.Header.Set("Idempotency-Key", "test-update")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "manifest") {
		t.Fatalf("expected manifest guidance: %s", rec.Body.String())
	}
}

func TestAdminPluginsCatalogEndpoint(t *testing.T) {
	handler := newTestHandler(t, config.Config{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/plugins", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code int             `json:"code"`
		Data plugins.Catalog `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Summary.Total == 0 || resp.Data.Summary.PaidLocked == 0 {
		t.Fatalf("unexpected plugin summary: %+v", resp.Data.Summary)
	}
}

func TestAdminPluginsEnableFreePluginAudits(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plugins/com.asterrouter.notification.webhook/enable", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code int            `json:"code"`
		Data plugins.Plugin `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Status != plugins.StatusEnabled {
		t.Fatalf("status = %q", resp.Data.Status)
	}
	audit, err := control.ListAuditLogs(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListAuditLogs(): %v", err)
	}
	for _, event := range audit {
		if event.ResourceType == "plugin" && event.Action == "enable" {
			return
		}
	}
	t.Fatalf("plugin audit event not found: %+v", audit)
}

func TestAdminPluginsRejectLockedPaidPlugin(t *testing.T) {
	handler := newTestHandler(t, config.Config{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plugins/com.asterrouter.notification.slack/enable", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGatewayModelsRequiresAPIKey(t *testing.T) {
	handler := newTestHandler(t, config.Config{})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGatewayModelsUsesAPIKeyAllowlist(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		ProjectID:         "proj_platform",
		ApplicationID:     "app_internal_sandbox",
		Name:              "gateway",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          2,
		MonthlyTokenLimit: 1000,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+created.Key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].ID != "gpt-4o-mini" {
		t.Fatalf("unexpected models: %+v", resp.Data)
	}
}

func TestGatewayChatCompletionAuthorizesModelAndAudits(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		ProjectID:         "proj_platform",
		ApplicationID:     "app_internal_sandbox",
		Name:              "gateway",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          2,
		MonthlyTokenLimit: 1000,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}

	body := bytes.NewBufferString(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+created.Key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	audit, err := control.ListAuditLogs(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListAuditLogs(): %v", err)
	}
	for _, event := range audit {
		if event.ResourceType == "gateway_call" && event.Action == "invoke" {
			return
		}
	}
	t.Fatalf("gateway audit event not found: %+v", audit)
}

func TestGatewayChatCompletionEnforcesQPSLimitAndRecordsTrace(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		ProjectID:         "proj_platform",
		ApplicationID:     "app_internal_sandbox",
		Name:              "gateway limited",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          1,
		MonthlyTokenLimit: 1000,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}

	for i := 0; i < 2; i++ {
		body := bytes.NewBufferString(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+created.Key)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if i == 0 && rec.Code != http.StatusOK {
			t.Fatalf("first status = %d body=%s", rec.Code, rec.Body.String())
		}
		if i == 1 {
			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("second status = %d body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "rate_limit_exceeded") {
				t.Fatalf("rate limit error not returned: %s", rec.Body.String())
			}
		}
	}

	usage, err := control.UsageReport(context.Background(), 10)
	if err != nil {
		t.Fatalf("UsageReport(): %v", err)
	}
	var foundUsage bool
	for _, record := range usage.Recent {
		if record.ErrorType == "rate_limit_exceeded" && record.Status == "error" {
			foundUsage = true
		}
	}
	if !foundUsage {
		t.Fatalf("rate limited usage record not found: %+v", usage.Recent)
	}

	traces, err := control.ListGatewayTraces(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListGatewayTraces(): %v", err)
	}
	for _, trace := range traces {
		if trace.ErrorType == "rate_limit_exceeded" && trace.HTTPStatus == http.StatusTooManyRequests {
			return
		}
	}
	t.Fatalf("rate limited trace not found: %+v", traces)
}

func TestGatewayChatCompletionRejectsDisallowedModel(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		ProjectID:         "proj_platform",
		ApplicationID:     "app_internal_sandbox",
		Name:              "gateway",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          2,
		MonthlyTokenLimit: 1000,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}

	body := bytes.NewBufferString(`{"model":"gpt-4.1-mini","messages":[{"role":"user","content":"ping"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+created.Key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGatewayChatCompletionForwardsToConfiguredProvider(t *testing.T) {
	var gotAuthorization string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("upstream path = %s", r.URL.Path)
		}
		gotAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"upstream-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"upstream-ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	handler, control := newTestRuntime(t, config.Config{})
	_, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name:    "test provider",
		Type:    "openai_compatible",
		BaseURL: upstream.URL + "/v1",
		Status:  "active",
		Models:  []string{"gpt-4o-mini"},
		APIKey:  "upstream-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		ProjectID:         "proj_platform",
		ApplicationID:     "app_internal_sandbox",
		Name:              "gateway",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          2,
		MonthlyTokenLimit: 1000,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}

	body := bytes.NewBufferString(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+created.Key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if gotAuthorization != "Bearer upstream-secret" {
		t.Fatalf("upstream authorization = %q", gotAuthorization)
	}
	if !strings.Contains(rec.Body.String(), "upstream-ok") {
		t.Fatalf("upstream response not returned: %s", rec.Body.String())
	}
}

func TestGatewayChatCompletionRoutesThroughProviderAccountPool(t *testing.T) {
	var gotAuthorization string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("upstream path = %s", r.URL.Path)
		}
		gotAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"upstream-account-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"account-ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":11}}`))
	}))
	defer upstream.Close()

	handler, control := newTestRuntime(t, config.Config{})
	provider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name:    "account route provider",
		Type:    "openai_compatible",
		BaseURL: upstream.URL + "/v1",
		Status:  "active",
		Models:  []string{"gpt-4o-mini"},
		APIKey:  "provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	schedulable := true
	account, err := control.CreateProviderAccount(context.Background(), "tester", controlplane.ProviderAccountRequest{
		ProviderID:     provider.ID,
		Name:           "Primary account",
		Platform:       "openai_compatible",
		AuthType:       "api_key",
		Status:         controlplane.AccountStatusActive,
		Schedulable:    &schedulable,
		Priority:       10,
		Concurrency:    3,
		RateMultiplier: 1,
		Models:         []string{"gpt-4o-mini"},
		Secret:         "account-secret",
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount(): %v", err)
	}
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		ProjectID:         "proj_platform",
		ApplicationID:     "app_internal_sandbox",
		Name:              "gateway",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          2,
		MonthlyTokenLimit: 1000,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}

	body := bytes.NewBufferString(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+created.Key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if gotAuthorization != "Bearer account-secret" {
		t.Fatalf("upstream authorization = %q", gotAuthorization)
	}
	if !strings.Contains(rec.Body.String(), "account-ok") {
		t.Fatalf("upstream response not returned: %s", rec.Body.String())
	}
	usage, err := control.UsageReport(context.Background(), 10)
	if err != nil {
		t.Fatalf("UsageReport(): %v", err)
	}
	if len(usage.Recent) != 1 || usage.Recent[0].ProviderID != provider.ID || usage.Recent[0].ProviderAccountID != account.ID {
		t.Fatalf("usage route metadata not recorded: %+v", usage.Recent)
	}
	if usage.Recent[0].InputTokens != 7 || usage.Recent[0].OutputTokens != 11 {
		t.Fatalf("usage tokens not parsed: %+v", usage.Recent[0])
	}

	traces, err := control.ListGatewayTraces(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListGatewayTraces(): %v", err)
	}
	if len(traces) != 1 {
		t.Fatalf("trace count = %d traces=%+v", len(traces), traces)
	}
	trace := traces[0]
	if trace.ProviderID != provider.ID || trace.ProviderAccountID != account.ID || trace.RouteSource != "provider_account" {
		t.Fatalf("trace route metadata not recorded: %+v", trace)
	}
	if trace.Status != "forwarded" || trace.HTTPStatus != http.StatusOK || trace.InputTokens != 7 || trace.OutputTokens != 11 {
		t.Fatalf("trace response metadata not recorded: %+v", trace)
	}

	traceReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/gateway-traces", nil)
	traceRec := httptest.NewRecorder()
	handler.ServeHTTP(traceRec, traceReq)
	if traceRec.Code != http.StatusOK {
		t.Fatalf("trace endpoint status = %d body=%s", traceRec.Code, traceRec.Body.String())
	}
	var traceResp struct {
		Data []controlplane.GatewayTrace `json:"data"`
	}
	if err := json.Unmarshal(traceRec.Body.Bytes(), &traceResp); err != nil {
		t.Fatalf("decode trace response: %v", err)
	}
	if len(traceResp.Data) != 1 || traceResp.Data[0].ProviderAccountID != account.ID {
		t.Fatalf("unexpected trace endpoint data: %+v", traceResp.Data)
	}
}

func TestGatewayChatCompletionRejectsOversizedRequestBody(t *testing.T) {
	handler := newTestHandler(t, config.Config{})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(strings.Repeat("x", gatewayRequestBodyLimit+1)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGatewayChatCompletionPassesThroughUpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
	}))
	defer upstream.Close()

	handler, control := newTestRuntime(t, config.Config{})
	_, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name:    "limited provider",
		Type:    "openai_compatible",
		BaseURL: upstream.URL + "/v1",
		Status:  "active",
		Models:  []string{"gpt-4o-mini"},
		APIKey:  "upstream-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		ProjectID:         "proj_platform",
		ApplicationID:     "app_internal_sandbox",
		Name:              "gateway",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          2,
		MonthlyTokenLimit: 1000,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}

	body := bytes.NewBufferString(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+created.Key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "rate_limit_error") {
		t.Fatalf("upstream error body not returned: %s", rec.Body.String())
	}
}

func TestGatewayChatCompletionStreamsConfiguredProvider(t *testing.T) {
	var gotAccept string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chunk-1\"}\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	handler, control := newTestRuntime(t, config.Config{})
	_, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name:    "stream provider",
		Type:    "openai_compatible",
		BaseURL: upstream.URL + "/v1",
		Status:  "active",
		Models:  []string{"gpt-4o-mini"},
		APIKey:  "upstream-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		ProjectID:         "proj_platform",
		ApplicationID:     "app_internal_sandbox",
		Name:              "gateway",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          2,
		MonthlyTokenLimit: 1000,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}

	body := bytes.NewBufferString(`{"model":"gpt-4o-mini","stream":true,"messages":[{"role":"user","content":"ping"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+created.Key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if gotAccept != "text/event-stream" {
		t.Fatalf("upstream accept = %q", gotAccept)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("content-type = %q", rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), "chunk-1") || !strings.Contains(rec.Body.String(), "[DONE]") {
		t.Fatalf("stream body not returned: %s", rec.Body.String())
	}
}

func TestGatewayChatCompletionRejectsStreamingWithoutProvider(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		ProjectID:         "proj_platform",
		ApplicationID:     "app_internal_sandbox",
		Name:              "gateway",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          2,
		MonthlyTokenLimit: 1000,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}

	body := bytes.NewBufferString(`{"model":"gpt-4o-mini","stream":true,"messages":[{"role":"user","content":"ping"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+created.Key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}
