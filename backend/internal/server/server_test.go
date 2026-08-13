package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/astercloud/asterrouter/backend/internal/plugins"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/astercloud/asterrouter/backend/internal/system"
	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

type allowDurableAIJobs struct{}

type unhealthySettingsRepository struct {
	settings.Repository
	healthErr error
}

type failingSessionStateRepository struct {
	controlplane.Repository
	err error
}

func (r failingSessionStateRepository) FindWorkspaceUserByID(context.Context, string) (controlplane.WorkspaceUser, bool, error) {
	return controlplane.WorkspaceUser{}, false, r.err
}

func (r unhealthySettingsRepository) Health(context.Context) error {
	return r.healthErr
}

func (allowDurableAIJobs) SupportsDurableAIJob(context.Context, gatewaycore.CanonicalAuthContext, gatewaycore.CanonicalRequest) (bool, error) {
	return true, nil
}

func newTestRuntime(t *testing.T, cfg RuntimeConfig) (http.Handler, *controlplane.Service) {
	return newTestRuntimeWithDurableAdmission(t, cfg, allowDurableAIJobs{})
}

func newTestRuntimeWithDurableAdmission(t *testing.T, cfg RuntimeConfig, durableJobs DurableAIJobAdmission) (http.Handler, *controlplane.Service) {
	t.Helper()
	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory", DemoMode: true})
	controlService := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	if err := controlService.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}
	pluginService := plugins.NewService(plugins.NewMemoryRepository())
	if err := pluginService.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("Plugin EnsureSeedData(): %v", err)
	}
	systemService := system.NewService(system.Config{Version: "test", BuildType: "source"})
	var runtime AIJobRuntimeStatusProvider
	if value, ok := durableJobs.(AIJobRuntimeStatusProvider); ok {
		runtime = value
	}
	return New(Options{Runtime: cfg, SettingsService: settingsService, ControlService: controlService, PluginService: pluginService, SystemService: systemService, DurableAIJobs: durableJobs, AIJobRuntime: runtime}), controlService
}

func newTestHandler(t *testing.T, cfg RuntimeConfig) http.Handler {
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
	return newAuthTestRuntimeWithTOTP(t, true)
}

func newAuthTestRuntimeWithTOTP(t *testing.T, totpEnabled bool) (http.Handler, *controlplane.Service) {
	t.Helper()
	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory", DemoMode: true})
	adminSettings, err := settingsService.Admin(t.Context())
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	adminSettings.TOTPEnabled = totpEnabled
	if _, err := settingsService.Update(t.Context(), adminSettings); err != nil {
		t.Fatalf("enable TOTP setting: %v", err)
	}
	controlService := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	if err := controlService.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}
	localAdmin, err := controlService.EnsureLocalAdmin(context.Background(), "admin", "secret", controlplane.WorkspaceUserDefaults{ConcurrencyLimit: 5})
	if err != nil {
		t.Fatalf("EnsureLocalAdmin(): %v", err)
	}
	pluginService := plugins.NewService(plugins.NewMemoryRepository())
	if err := pluginService.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("Plugin EnsureSeedData(): %v", err)
	}
	return New(Options{
		Runtime:         RuntimeConfig{},
		AuthService:     auth.NewService(auth.Config{Username: "admin", Password: "secret", PasswordHash: localAdmin.PasswordHash, SecretKey: "test-secret"}),
		SettingsService: settingsService,
		ControlService:  controlService,
		PluginService:   pluginService,
		SystemService:   system.NewService(system.Config{Version: "test", BuildType: "source"}),
	}), controlService
}

func TestPublicSettingsEndpoint(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{})

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

func TestDemoSessionVersionResolverAllowsExplicitDemoPrincipal(t *testing.T) {
	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory", DemoMode: true})
	controlService := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	handler := New(Options{
		Runtime:         RuntimeConfig{DemoMode: true},
		AuthService:     auth.NewService(auth.Config{Username: "admin", Password: "secret", SecretKey: "test-secret", DemoMode: true}),
		SettingsService: settingsService,
		ControlService:  controlService,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"demo","password":"demo"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("demo login status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDemoLoginRequiresDemoModeAtHTTPBoundary(t *testing.T) {
	handler := New(Options{
		Runtime:         RuntimeConfig{DemoMode: false},
		AuthService:     auth.NewService(auth.Config{Username: "admin", Password: "secret", SecretKey: "test-secret"}),
		SettingsService: settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"}),
		ControlService:  controlplane.NewService(controlplane.NewMemoryRepository(), "/v1"),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"demo","password":"demo"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("demo login status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "access_token") {
		t.Fatalf("demo login issued a session while demo mode was disabled: %s", rec.Body.String())
	}
}

func TestSessionStateRepositoryFailureReturnsServiceUnavailable(t *testing.T) {
	const sensitiveMarker = "postgres://private-user:private-password@database.internal/asterrouter"
	authService := auth.NewService(auth.Config{Username: "admin", Password: "secret", SecretKey: "test-secret"})
	session, err := authService.Login(t.Context(), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	repository := failingSessionStateRepository{Repository: controlplane.NewMemoryRepository(), err: errors.New(sensitiveMarker)}
	handler := New(Options{
		AuthService:     authService,
		SettingsService: settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"}),
		ControlService:  controlplane.NewService(repository, "/v1"),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+session.AccessToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "authentication service is unavailable") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), sensitiveMarker) || strings.Contains(rec.Body.String(), "private-password") {
		t.Fatalf("session state error leaked internal detail: %s", rec.Body.String())
	}
}

func TestReadinessFailsClosedWithoutLeakingDependencyError(t *testing.T) {
	const sensitiveMarker = "postgres://private-user:private-password@database.internal/asterrouter"
	repository := unhealthySettingsRepository{
		Repository: settings.NewMemoryRepository(),
		healthErr:  errors.New(sensitiveMarker),
	}
	handler := New(Options{
		SettingsService: settings.NewService(repository, settings.ServiceOptions{Version: "test", StorageMode: "postgres"}),
	})

	healthReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	healthRec := httptest.NewRecorder()
	handler.ServeHTTP(healthRec, healthReq)
	if healthRec.Code != http.StatusOK {
		t.Fatalf("health status=%d body=%s", healthRec.Code, healthRec.Body.String())
	}

	readyReq := httptest.NewRequest(http.MethodGet, "/ready", nil)
	readyRec := httptest.NewRecorder()
	handler.ServeHTTP(readyRec, readyReq)
	if readyRec.Code != http.StatusServiceUnavailable || !strings.Contains(readyRec.Body.String(), `"code":1001`) {
		t.Fatalf("ready status=%d body=%s", readyRec.Code, readyRec.Body.String())
	}
	if strings.Contains(readyRec.Body.String(), sensitiveMarker) || !strings.Contains(readyRec.Body.String(), "service dependency is unavailable") {
		t.Fatalf("ready response leaked dependency detail: %s", readyRec.Body.String())
	}
}

func TestReadinessFailsClosedAfterPostgresRepositoryStops(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	repository, err := settings.NewPostgresRepository(context.Background(), schema.URL)
	if err != nil {
		t.Fatalf("NewPostgresRepository(): %v", err)
	}
	handler := New(Options{
		SettingsService: settings.NewService(repository, settings.ServiceOptions{Version: "test", StorageMode: "postgres"}),
	})

	readyReq := httptest.NewRequest(http.MethodGet, "/ready", nil)
	readyRec := httptest.NewRecorder()
	handler.ServeHTTP(readyRec, readyReq)
	if readyRec.Code != http.StatusOK {
		t.Fatalf("initial ready status=%d body=%s", readyRec.Code, readyRec.Body.String())
	}
	if err := repository.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}

	failedReq := httptest.NewRequest(http.MethodGet, "/ready", nil)
	failedRec := httptest.NewRecorder()
	handler.ServeHTTP(failedRec, failedReq)
	if failedRec.Code != http.StatusServiceUnavailable || !strings.Contains(failedRec.Body.String(), `"code":1001`) {
		t.Fatalf("failed ready status=%d body=%s", failedRec.Code, failedRec.Body.String())
	}
	if strings.Contains(failedRec.Body.String(), "database is closed") || !strings.Contains(failedRec.Body.String(), "service dependency is unavailable") {
		t.Fatalf("failed ready response leaked database detail: %s", failedRec.Body.String())
	}
}

func TestTwoInstancesRecoverAfterPostgresNetworkInterruption(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	proxy, proxiedURL := testutil.NewTCPProxy(t, schema.URL)
	repositories := make([]*settings.PostgresRepository, 0, 2)
	handlers := make([]http.Handler, 0, 2)
	for index := 0; index < 2; index++ {
		repository, err := settings.NewPostgresRepository(context.Background(), proxiedURL)
		if err != nil {
			t.Fatalf("NewPostgresRepository(instance=%d): %v", index, err)
		}
		repositories = append(repositories, repository)
		handlers = append(handlers, New(Options{
			Runtime:         RuntimeConfig{MetricsToken: "network-fault-metrics"},
			SettingsService: settings.NewService(repository, settings.ServiceOptions{Version: "test", StorageMode: "postgres"}),
		}))
	}
	defer func() {
		for _, repository := range repositories {
			_ = repository.Close()
		}
	}()

	for index, handler := range handlers {
		if status, body := readinessStatus(handler); status != http.StatusOK {
			t.Fatalf("initial readiness instance=%d status=%d body=%s", index, status, body)
		}
	}
	proxy.Disable()
	for index, handler := range handlers {
		status, body := awaitReadinessStatus(t, handler, http.StatusServiceUnavailable)
		if status != http.StatusServiceUnavailable || !strings.Contains(body, `"code":1001`) || !strings.Contains(body, "service dependency is unavailable") {
			t.Fatalf("blocked readiness instance=%d status=%d body=%s", index, status, body)
		}
		if strings.Contains(body, schema.Name) || strings.Contains(body, "127.0.0.1") {
			t.Fatalf("blocked readiness leaked database details instance=%d body=%s", index, body)
		}
		healthRequest := httptest.NewRequest(http.MethodGet, "/health", nil)
		healthResponse := httptest.NewRecorder()
		handler.ServeHTTP(healthResponse, healthRequest)
		if healthResponse.Code != http.StatusOK {
			t.Fatalf("blocked health instance=%d status=%d body=%s", index, healthResponse.Code, healthResponse.Body.String())
		}
	}

	proxy.Enable()
	for index, handler := range handlers {
		status, body := awaitReadinessStatus(t, handler, http.StatusOK)
		if status != http.StatusOK {
			t.Fatalf("recovered readiness instance=%d status=%d body=%s", index, status, body)
		}
		metricsRequest := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		metricsRequest.Header.Set("Authorization", "Bearer network-fault-metrics")
		metricsResponse := httptest.NewRecorder()
		handler.ServeHTTP(metricsResponse, metricsRequest)
		metricsBody := metricsResponse.Body.String()
		if !strings.Contains(metricsBody, `asterrouter_readiness_checks_total{result="unavailable"}`) || !strings.Contains(metricsBody, `asterrouter_readiness_checks_total{result="ready"}`) {
			t.Fatalf("recovery metrics instance=%d body=%s", index, metricsBody)
		}
	}
}

func readinessStatus(handler http.Handler) (int, string) {
	request := httptest.NewRequest(http.MethodGet, "/ready", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response.Code, response.Body.String()
}

func awaitReadinessStatus(t *testing.T, handler http.Handler, expected int) (int, string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		status, body := readinessStatus(handler)
		if status == expected || time.Now().After(deadline) {
			return status, body
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestConsoleSettingsRequiresToken(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{AdminToken: "secret"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/console/settings", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestConsoleSettingsRequiresLoginWhenAuthServiceEnabled(t *testing.T) {
	handler := newAuthTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/console/settings", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLoginAllowsConsoleSettingsAccess(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/console/settings", nil)
	req.Header.Set("Authorization", "Bearer "+loginResp.Data.AccessToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("settings status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCookieBackedLogoutRevokesTheServerSession(t *testing.T) {
	handler, control := newAuthTestRuntime(t)
	if _, _, err := control.RegisterWorkspaceUser(t.Context(), "external@example.test", "synthetic-password-123", "External User", false); err != nil {
		t.Fatal(err)
	}
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"external@example.test","password":"synthetic-password-123","session_mode":"cookie"}`))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, loginRequest)
	var login struct {
		Data auth.LoginResult `json:"data"`
	}
	if err := json.Unmarshal(loginResponse.Body.Bytes(), &login); err != nil || loginResponse.Code != http.StatusOK || login.Data.AccessToken != cookieSessionTokenMarker {
		t.Fatalf("login status=%d body=%s err=%v", loginResponse.Code, loginResponse.Body.String(), err)
	}
	sessionCookie := responseCookie(t, loginResponse, sessionCookieName)
	csrfCookie := responseCookie(t, loginResponse, csrfCookieName)
	if !sessionCookie.HttpOnly || !sessionCookie.Secure || csrfCookie.HttpOnly || !csrfCookie.Secure {
		t.Fatalf("cookie attributes: session=%#v csrf=%#v", sessionCookie, csrfCookie)
	}
	if strings.Contains(loginResponse.Body.String(), sessionCookie.Value) {
		t.Fatal("HttpOnly session token leaked into login response body")
	}
	profileBody := `{"display_name":"External Updated","avatar_data_url":""}`
	missingProfileCSRF := httptest.NewRequest(http.MethodPut, "/api/v1/account/profile", bytes.NewBufferString(profileBody))
	missingProfileCSRF.Header.Set("Content-Type", "application/json")
	missingProfileCSRF.Header.Set("Authorization", "Bearer "+cookieSessionTokenMarker)
	missingProfileCSRF.AddCookie(sessionCookie)
	missingProfileCSRF.AddCookie(csrfCookie)
	missingProfileCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingProfileCSRFResponse, missingProfileCSRF)
	if missingProfileCSRFResponse.Code != http.StatusForbidden {
		t.Fatalf("profile update without CSRF status=%d body=%s", missingProfileCSRFResponse.Code, missingProfileCSRFResponse.Body.String())
	}

	crossSiteProfile := httptest.NewRequest(http.MethodPut, "/api/v1/account/profile", bytes.NewBufferString(profileBody))
	crossSiteProfile.Header.Set("Content-Type", "application/json")
	crossSiteProfile.Header.Set("Authorization", "Bearer "+cookieSessionTokenMarker)
	crossSiteProfile.Header.Set(csrfHeaderName, csrfCookie.Value)
	crossSiteProfile.Header.Set("Sec-Fetch-Site", "cross-site")
	crossSiteProfile.AddCookie(sessionCookie)
	crossSiteProfile.AddCookie(csrfCookie)
	crossSiteProfileResponse := httptest.NewRecorder()
	handler.ServeHTTP(crossSiteProfileResponse, crossSiteProfile)
	if crossSiteProfileResponse.Code != http.StatusForbidden {
		t.Fatalf("cross-site profile update status=%d body=%s", crossSiteProfileResponse.Code, crossSiteProfileResponse.Body.String())
	}

	profileRequest := httptest.NewRequest(http.MethodPut, "/api/v1/account/profile", bytes.NewBufferString(profileBody))
	profileRequest.Header.Set("Content-Type", "application/json")
	profileRequest.Header.Set("Authorization", "Bearer "+cookieSessionTokenMarker)
	profileRequest.Header.Set(csrfHeaderName, csrfCookie.Value)
	profileRequest.AddCookie(sessionCookie)
	profileRequest.AddCookie(csrfCookie)
	profileResponse := httptest.NewRecorder()
	handler.ServeHTTP(profileResponse, profileRequest)
	if profileResponse.Code != http.StatusOK {
		t.Fatalf("profile update with CSRF status=%d body=%s", profileResponse.Code, profileResponse.Body.String())
	}

	missingCSRFRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	missingCSRFRequest.Header.Set("Authorization", "Bearer "+cookieSessionTokenMarker)
	missingCSRFRequest.AddCookie(sessionCookie)
	missingCSRFRequest.AddCookie(csrfCookie)
	missingCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingCSRFResponse, missingCSRFRequest)
	if missingCSRFResponse.Code != http.StatusForbidden {
		t.Fatalf("logout without CSRF status=%d body=%s", missingCSRFResponse.Code, missingCSRFResponse.Body.String())
	}

	logoutRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logoutRequest.Header.Set("Authorization", "Bearer "+cookieSessionTokenMarker)
	logoutRequest.Header.Set(csrfHeaderName, csrfCookie.Value)
	logoutRequest.AddCookie(sessionCookie)
	logoutRequest.AddCookie(csrfCookie)
	logoutResponse := httptest.NewRecorder()
	handler.ServeHTTP(logoutResponse, logoutRequest)
	if logoutResponse.Code != http.StatusOK {
		t.Fatalf("logout status=%d body=%s", logoutResponse.Code, logoutResponse.Body.String())
	}

	meRequest := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	meRequest.Header.Set("Authorization", "Bearer "+sessionCookie.Value)
	meResponse := httptest.NewRecorder()
	handler.ServeHTTP(meResponse, meRequest)
	if meResponse.Code != http.StatusUnauthorized {
		t.Fatalf("session remained valid after logout: status=%d body=%s", meResponse.Code, meResponse.Body.String())
	}
}

func responseCookie(t *testing.T, recorder *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("response cookie %q was not set", name)
	return nil
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
	controlService := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	localAdmin, err := controlService.EnsureLocalAdmin(t.Context(), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	handler := New(Options{
		AuthService:     auth.NewService(auth.Config{Username: "admin", Password: "secret", PasswordHash: localAdmin.PasswordHash, SecretKey: "test-secret"}),
		SettingsService: settingsService,
		ControlService:  controlService,
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

func TestLegalDocumentEndpointReturnsPublishedDocumentAndRejectsUnknownSlug(t *testing.T) {
	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"})
	current, err := settingsService.Admin(t.Context())
	if err != nil {
		t.Fatalf("Admin(): %v", err)
	}
	current.LegalDocuments = []settings.LegalDocument{
		{ID: "privacy", Name: "Privacy Policy", Slug: "privacy-policy", Content: "# Privacy\n\nEnterprise data is isolated."},
	}
	if _, err := settingsService.Update(t.Context(), current); err != nil {
		t.Fatalf("Update(): %v", err)
	}
	handler := New(Options{SettingsService: settingsService})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/legal/privacy-policy", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("published document status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Data settings.LegalDocument `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode legal document: %v", err)
	}
	if response.Data != current.LegalDocuments[0] {
		t.Fatalf("legal document mismatch: %+v", response.Data)
	}

	missingReq := httptest.NewRequest(http.MethodGet, "/api/v1/legal/unknown", nil)
	missingRec := httptest.NewRecorder()
	handler.ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusNotFound || !strings.Contains(missingRec.Body.String(), "legal document not found") {
		t.Fatalf("unknown document status=%d body=%s", missingRec.Code, missingRec.Body.String())
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

func TestSetupEndpointCompletesEnterpriseInitialization(t *testing.T) {
	repo := settings.NewMemoryRepository()
	svc := settings.NewService(repo, settings.ServiceOptions{Version: "test", StorageMode: "memory"})
	handler := New(Options{SettingsService: svc})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/setup", bytes.NewBufferString(`{"organization_name":"  Aster Cloud  "}`))
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
	if !got.SetupCompleted || got.SiteName != "Aster Cloud" {
		t.Fatalf("setup not persisted: %+v", got)
	}
}

func TestSetupEndpointSerializesConcurrentRequests(t *testing.T) {
	svc := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"})
	handler := New(Options{SettingsService: svc})
	start := make(chan struct{})
	responses := make(chan *httptest.ResponseRecorder, 2)
	for _, organizationName := range []string{"First Organization", "Second Organization"} {
		go func(name string) {
			<-start
			req := httptest.NewRequest(http.MethodPost, "/api/v1/setup", bytes.NewBufferString(fmt.Sprintf(`{"organization_name":%q}`, name)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			responses <- rec
		}(organizationName)
	}
	close(start)

	succeeded := 0
	conflicted := 0
	for range 2 {
		rec := <-responses
		switch rec.Code {
		case http.StatusOK:
			succeeded++
		case http.StatusConflict:
			conflicted++
		default:
			t.Fatalf("unexpected status = %d body=%s", rec.Code, rec.Body.String())
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("concurrent setup results: succeeded=%d conflicted=%d", succeeded, conflicted)
	}
}

type failingSetupRepository struct {
	*settings.MemoryRepository
}

func (r *failingSetupRepository) CompleteSetup(context.Context, string) error {
	return errors.New("database secret detail")
}

func TestSetupEndpointReturnsSanitizedServerErrorWhenPersistenceFails(t *testing.T) {
	svc := settings.NewService(&failingSetupRepository{MemoryRepository: settings.NewMemoryRepository()}, settings.ServiceOptions{
		Version: "test", StorageMode: "memory",
	})
	handler := New(Options{SettingsService: svc})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/setup", bytes.NewBufferString(`{"organization_name":"Aster Cloud"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("database secret detail")) {
		t.Fatalf("setup response exposed repository error: %s", rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("failed to complete setup")) {
		t.Fatalf("setup response did not include the public error category: %s", rec.Body.String())
	}
}

func TestSetupEndpointRejectsInvalidOrganization(t *testing.T) {
	svc := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"})
	handler := New(Options{SettingsService: svc})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/setup", bytes.NewBufferString(`{"organization_name":"  "}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want %d, response=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestRemovedAPIRoutesReturnNotFound(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{})
	for _, path := range []string{
		"/api/v1/admin/settings",
		"/api/v1/operator/dashboard",
		"/api/v1/customer/billing",
		"/api/v1/platform/dashboard",
		"/api/v1/setup/profiles",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("path=%s status=%d, want %d, response=%s", path, rec.Code, http.StatusNotFound, rec.Body.String())
		}
	}
}
