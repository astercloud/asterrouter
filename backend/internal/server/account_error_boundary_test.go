package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/settings"
)

type accountFailureRepository struct {
	controlplane.Repository
	operation string
	err       error
}

type failNthAccountLookupRepository struct {
	controlplane.Repository
	mu     sync.Mutex
	reads  int
	failAt int
	err    error
}

func (r *failNthAccountLookupRepository) FindWorkspaceUserByID(ctx context.Context, id string) (controlplane.WorkspaceUser, bool, error) {
	r.mu.Lock()
	r.reads++
	shouldFail := r.reads == r.failAt
	r.mu.Unlock()
	if shouldFail {
		return controlplane.WorkspaceUser{}, false, r.err
	}
	return r.Repository.FindWorkspaceUserByID(ctx, id)
}

func (r accountFailureRepository) SaveWorkspaceUser(ctx context.Context, user controlplane.WorkspaceUser) error {
	if r.operation == "save_workspace_user" {
		return r.err
	}
	return r.Repository.SaveWorkspaceUser(ctx, user)
}

func (r accountFailureRepository) ListAuthIdentities(ctx context.Context, userID string) ([]controlplane.AuthIdentity, error) {
	if r.operation == "list_auth_identities" {
		return nil, r.err
	}
	return r.Repository.ListAuthIdentities(ctx, userID)
}

func TestAccountRoutesDoNotExposeRepositoryErrors(t *testing.T) {
	const sensitiveMarker = "postgres://account-user:private-password@database.internal/asterrouter"
	tests := []struct {
		name      string
		operation string
		method    string
		path      string
		body      string
		prepare   func(*testing.T, *controlplane.Service) string
	}{
		{name: "profile read", operation: "list_auth_identities", method: http.MethodGet, path: "/api/v1/account/profile"},
		{name: "profile update", operation: "save_workspace_user", method: http.MethodPut, path: "/api/v1/account/profile", body: `{"display_name":"Updated"}`},
		{name: "password update", operation: "save_workspace_user", method: http.MethodPut, path: "/api/v1/account/password", body: `{"current_password":"secret","new_password":"updated-password"}`},
		{name: "identity unbind", operation: "list_auth_identities", method: http.MethodDelete, path: "/api/v1/account/identities/github"},
		{name: "TOTP setup", operation: "save_workspace_user", method: http.MethodPost, path: "/api/v1/account/totp/setup", body: `{"current_password":"secret"}`},
		{
			name: "TOTP confirm", operation: "save_workspace_user", method: http.MethodPost, path: "/api/v1/account/totp/confirm",
			prepare: func(t *testing.T, service *controlplane.Service) string {
				t.Helper()
				setup, err := service.BeginTOTPSetup(t.Context(), "admin", "secret")
				if err != nil {
					t.Fatalf("BeginTOTPSetup(): %v", err)
				}
				return `{"code":"` + auth.GenerateTOTPCode(setup.Secret, time.Now().UTC()) + `"}`
			},
		},
		{name: "session revoke", operation: "save_workspace_user", method: http.MethodPost, path: "/api/v1/account/sessions/revoke-others"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, token, baseService := newAccountFailureTestRuntime(t, test.operation, errors.New(sensitiveMarker))
			body := test.body
			if test.prepare != nil {
				body = test.prepare(t, baseService)
			}
			req := httptest.NewRequest(test.method, test.path, strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+token)
			if body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), sensitiveMarker) || strings.Contains(rec.Body.String(), "database.internal") {
				t.Fatalf("repository failure leaked internal detail: %s", rec.Body.String())
			}
		})
	}
}

func TestAccountRoutesPreservePublicValidationErrors(t *testing.T) {
	handler, _ := newAuthTestRuntime(t)
	token := accountLoginToken(t, handler, "admin", "secret")
	tests := []struct {
		name    string
		method  string
		path    string
		body    string
		message string
	}{
		{name: "display name", method: http.MethodPut, path: "/api/v1/account/profile", body: `{"display_name":""}`, message: controlplane.ErrAccountDisplayNameRequired.Error()},
		{name: "current password", method: http.MethodPut, path: "/api/v1/account/password", body: `{"current_password":"wrong","new_password":"updated-password"}`, message: controlplane.ErrCurrentPasswordIncorrect.Error()},
		{name: "identity not bound", method: http.MethodDelete, path: "/api/v1/account/identities/github", message: controlplane.ErrAuthIdentityNotBound.Error()},
		{name: "TOTP enrollment", method: http.MethodPost, path: "/api/v1/account/totp/confirm", body: `{"code":"000000"}`, message: controlplane.ErrTOTPEnrollmentNotStarted.Error()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			req.Header.Set("Authorization", "Bearer "+token)
			if test.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), test.message) {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestLocalLoginFailsClosedWhenTOTPStateCannotBeLoaded(t *testing.T) {
	const sensitiveMarker = "postgres://mfa-user:private-password@database.internal/asterrouter"
	baseRepository := controlplane.NewMemoryRepository()
	baseService := controlplane.NewService(baseRepository, "/v1", "test-secret")
	localAdmin, err := baseService.EnsureLocalAdmin(t.Context(), "admin", "secret")
	if err != nil {
		t.Fatalf("EnsureLocalAdmin(): %v", err)
	}
	setup, err := baseService.BeginTOTPSetup(t.Context(), localAdmin.ID, "secret")
	if err != nil {
		t.Fatalf("BeginTOTPSetup(): %v", err)
	}
	if err := baseService.ConfirmTOTP(t.Context(), localAdmin.ID, auth.GenerateTOTPCode(setup.Secret, time.Now().UTC())); err != nil {
		t.Fatalf("ConfirmTOTP(): %v", err)
	}

	repository := &failNthAccountLookupRepository{Repository: baseRepository, failAt: 2, err: errors.New(sensitiveMarker)}
	service := controlplane.NewService(repository, "/v1", "test-secret")
	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory", DemoMode: true})
	handler := New(Options{
		AuthService:     auth.NewService(auth.Config{Username: "admin", Password: "secret", PasswordHash: localAdmin.PasswordHash, SecretKey: "test-secret"}),
		SettingsService: settingsService,
		ControlService:  service,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), sensitiveMarker) || strings.Contains(rec.Body.String(), "database.internal") || strings.Contains(rec.Body.String(), "access_token") {
		t.Fatalf("failed MFA state lookup leaked details or issued a session: %s", rec.Body.String())
	}
}

func newAccountFailureTestRuntime(t *testing.T, operation string, failure error) (http.Handler, string, *controlplane.Service) {
	t.Helper()
	baseRepository := controlplane.NewMemoryRepository()
	baseService := controlplane.NewService(baseRepository, "/v1", "test-secret")
	localAdmin, err := baseService.EnsureLocalAdmin(t.Context(), "admin", "secret")
	if err != nil {
		t.Fatalf("EnsureLocalAdmin(): %v", err)
	}
	settingsService := settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory", DemoMode: true})
	adminSettings, err := settingsService.Admin(t.Context())
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	adminSettings.TOTPEnabled = true
	if _, err := settingsService.Update(t.Context(), adminSettings); err != nil {
		t.Fatalf("enable TOTP setting: %v", err)
	}

	service := controlplane.NewService(accountFailureRepository{Repository: baseRepository, operation: operation, err: failure}, "/v1", "test-secret")
	handler := New(Options{
		AuthService:     auth.NewService(auth.Config{Username: "admin", Password: "secret", PasswordHash: localAdmin.PasswordHash, SecretKey: "test-secret"}),
		SettingsService: settingsService,
		ControlService:  service,
	})
	return handler, accountLoginToken(t, handler, "admin", "secret"), baseService
}

func accountLoginToken(t *testing.T, handler http.Handler, username, password string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"`+username+`","password":"`+password+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var response struct {
		Data auth.LoginResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || response.Data.AccessToken == "" {
		t.Fatalf("login status=%d body=%s err=%v", rec.Code, rec.Body.String(), err)
	}
	return response.Data.AccessToken
}
