package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/config"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	operatorcore "github.com/astercloud/asterrouter/backend/internal/operator"
	"github.com/astercloud/asterrouter/backend/internal/plugins"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/astercloud/asterrouter/backend/internal/system"
)

func newTestRuntime(t *testing.T, cfg config.Config) (http.Handler, *controlplane.Service) {
	t.Helper()
	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory", DemoMode: true, EnabledProfiles: []string{"personal", "relay_operator", "enterprise"}})
	controlService := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	if err := controlService.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}
	pluginService := plugins.NewService(plugins.NewMemoryRepository())
	operatorService := operatorcore.NewService(operatorcore.NewMemoryRepository(), controlService)
	if err := pluginService.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("Plugin EnsureSeedData(): %v", err)
	}
	systemService := system.NewService(system.Config{Version: "test", BuildType: "source"})
	return New(Options{Config: cfg, SettingsService: settingsService, ControlService: controlService, OperatorService: operatorService, PluginService: pluginService, SystemService: systemService}), controlService
}

func newTestHandler(t *testing.T, cfg config.Config) http.Handler {
	t.Helper()
	handler, _ := newTestRuntime(t, cfg)
	return handler
}

func newAuthTestHandler(t *testing.T) http.Handler {
	t.Helper()
	handler, _ := newAuthTestRuntime(t)
	return handler
}

func newAuthTestRuntime(t *testing.T) (http.Handler, *controlplane.Service) {
	t.Helper()
	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory", DemoMode: true, EnabledProfiles: []string{"personal", "relay_operator", "enterprise"}})
	controlService := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	if err := controlService.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}
	localAdmin, err := controlService.EnsureLocalAdmin(context.Background(), "admin", "secret", controlplane.WorkspaceUserDefaults{ConcurrencyLimit: 5})
	if err != nil {
		t.Fatalf("EnsureLocalAdmin(): %v", err)
	}
	pluginService := plugins.NewService(plugins.NewMemoryRepository())
	operatorService := operatorcore.NewService(operatorcore.NewMemoryRepository(), controlService)
	if err := pluginService.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("Plugin EnsureSeedData(): %v", err)
	}
	return New(Options{
		Config:          config.Config{},
		AuthService:     auth.NewService(auth.Config{Username: "admin", Password: "secret", PasswordHash: localAdmin.PasswordHash, SecretKey: "test-secret"}),
		SettingsService: settingsService,
		ControlService:  controlService,
		OperatorService: operatorService,
		PluginService:   pluginService,
		SystemService:   system.NewService(system.Config{Version: "test", BuildType: "source"}),
	}), controlService
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

func TestAuthenticationResponsesExposeServerDerivedAllowedSurfaces(t *testing.T) {
	handler, control := newAuthTestRuntime(t)
	user, _, err := control.RegisterWorkspaceUser(t.Context(), "surface-summary@example.test", "synthetic-password-123", "Surface Summary", false)
	if err != nil {
		t.Fatal(err)
	}
	login := func() auth.LoginResult {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"surface-summary@example.test","password":"synthetic-password-123"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		var response struct {
			Data auth.LoginResult `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("login status=%d body=%s err=%v", rec.Code, rec.Body.String(), err)
		}
		return response.Data
	}

	initial := login()
	if !containsSurface(initial.User.AllowedSurfaces, controlplane.SurfaceCustomer) || !containsSurface(initial.User.AllowedSurfaces, controlplane.SurfacePortal) || containsSurface(initial.User.AllowedSurfaces, controlplane.SurfaceRelayOperator) || containsSurface(initial.User.AllowedSurfaces, controlplane.SurfaceEnterprise) {
		t.Fatalf("initial allowed surfaces=%v", initial.User.AllowedSurfaces)
	}
	if _, err := control.CreateRoleBinding(t.Context(), "tester", controlplane.RoleBindingRequest{
		UserID: user.ID, Role: controlplane.RolePlatformAdmin, ScopeType: controlplane.RoleScopeSurface, ScopeID: controlplane.SurfaceRelayOperator,
	}); err != nil {
		t.Fatal(err)
	}
	bound := login()
	if !containsSurface(bound.User.AllowedSurfaces, controlplane.SurfaceRelayOperator) {
		t.Fatalf("bound allowed surfaces=%v", bound.User.AllowedSurfaces)
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+bound.AccessToken)
	meRec := httptest.NewRecorder()
	handler.ServeHTTP(meRec, meReq)
	var meResponse struct {
		Data auth.User `json:"data"`
	}
	if err := json.Unmarshal(meRec.Body.Bytes(), &meResponse); err != nil || meRec.Code != http.StatusOK || !containsSurface(meResponse.Data.AllowedSurfaces, controlplane.SurfaceRelayOperator) {
		t.Fatalf("auth/me status=%d body=%s response=%+v err=%v", meRec.Code, meRec.Body.String(), meResponse, err)
	}
}

func containsSurface(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestLoginAgreementIsEnforcedAfterPayloadBinding(t *testing.T) {
	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"})
	current, err := settingsService.Admin(context.Background())
	if err != nil {
		t.Fatalf("Admin(): %v", err)
	}
	current.LoginAgreementEnabled = true
	current.LegalDocuments = []settings.LegalDocument{{ID: "terms", Name: "Terms", Slug: "terms", Content: "Terms"}}
	if _, err := settingsService.Update(context.Background(), current); err != nil {
		t.Fatalf("Update(): %v", err)
	}
	handler := New(Options{
		AuthService:     auth.NewService(auth.Config{Username: "admin", Password: "secret", SecretKey: "test-secret"}),
		SettingsService: settingsService,
		ControlService:  controlplane.NewService(controlplane.NewMemoryRepository(), "/v1"),
		SystemService:   system.NewService(system.Config{Version: "test", BuildType: "source"}),
	})

	for _, test := range []struct {
		name     string
		accepted bool
		status   int
	}{
		{name: "missing acceptance is rejected", accepted: false, status: http.StatusForbidden},
		{name: "explicit acceptance allows login", accepted: true, status: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"username":"admin","password":"secret","agreement_accepted":%t}`, test.accepted)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != test.status {
				t.Fatalf("status = %d, want %d, body=%s", rec.Code, test.status, rec.Body.String())
			}
		})
	}
}

func TestLegacyCaptchaEndpointDisablesCaptcha(t *testing.T) {
	handler := newAuthTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/iam/get-captcha-code?locale=zh_CN", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			CaptchaOnOff bool   `json:"captchaOnOff"`
			Img          string `json:"img"`
			UUID         string `json:"uuid"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.CaptchaOnOff || resp.Data.Img != "" || resp.Data.UUID != "" {
		t.Fatalf("captcha response = %+v", resp.Data)
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

	body := bytes.NewBufferString(`{"profile":"platform"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/profiles", body)
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
	if got.DefaultProfile != "platform" || len(got.EnabledProfiles) != 1 || got.EnabledProfiles[0] != "platform" || !got.SetupCompleted {
		t.Fatalf("setup not persisted: %+v", got)
	}
}

func TestSetupProfileEndpointRequiresOneValidProfile(t *testing.T) {
	svc := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"})
	handler := New(Options{
		SettingsService: svc,
		ControlService:  controlplane.NewService(controlplane.NewMemoryRepository(), "/v1"),
		SystemService:   system.NewService(system.Config{Version: "test", BuildType: "source"}),
	})

	for _, body := range []string{
		`{"profiles":["enterprise","platform"],"default_profile":"enterprise"}`,
		`{"profile":"unsupported"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/profiles", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d, want %d, response=%s", body, rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	}
}
