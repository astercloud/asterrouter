package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/settings"
)

type testHumanVerifier struct {
	token string
}

func (v testHumanVerifier) Verify(_ context.Context, _, response, _ string) error {
	if response != v.token {
		return errors.New("human verification failed")
	}
	return nil
}

type recordingAuthEmailSender struct {
	mu   sync.Mutex
	err  error
	urls []string
}

type deferredFailureAuthEmailSender struct {
	mu        sync.Mutex
	urls      []string
	callbacks []func(context.Context) error
}

func (s *deferredFailureAuthEmailSender) Send(context.Context, string, string, string, string) error {
	return errors.New("failure-aware send method was not used")
}

func (s *deferredFailureAuthEmailSender) SendWithFailureCallback(_ context.Context, _, _, _, actionURL string, onFailure func(context.Context) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.urls = append(s.urls, actionURL)
	s.callbacks = append(s.callbacks, onFailure)
	return nil
}

func (s *deferredFailureAuthEmailSender) failLast(t *testing.T) string {
	t.Helper()
	s.mu.Lock()
	if len(s.urls) == 0 || len(s.callbacks) != len(s.urls) {
		s.mu.Unlock()
		t.Fatal("authentication email callback was not recorded")
	}
	actionURL := s.urls[len(s.urls)-1]
	callback := s.callbacks[len(s.callbacks)-1]
	s.mu.Unlock()
	if callback == nil {
		t.Fatal("authentication email failure callback is nil")
	}
	if err := callback(context.Background()); err != nil {
		t.Fatalf("authentication email failure callback: %v", err)
	}
	return actionURL
}

func (s *deferredFailureAuthEmailSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.urls)
}

type failNthSettingsReadRepository struct {
	*settings.MemoryRepository
	mu     sync.Mutex
	reads  int
	failAt int
	err    error
}

type failingEmailVerificationRepository struct {
	controlplane.Repository
	err error
}

type failingWorkspaceLoginRepository struct {
	controlplane.Repository
	err error
}

type failingMFAUserRepository struct {
	controlplane.Repository
	err error
}

type failingRegistrationRepository struct {
	controlplane.Repository
	err error
}

func (r failingEmailVerificationRepository) ConsumeWorkspaceUserEmailVerification(context.Context, string, time.Time) (controlplane.WorkspaceUser, bool, error) {
	return controlplane.WorkspaceUser{}, false, r.err
}

func (r failingWorkspaceLoginRepository) FindWorkspaceUserByEmail(context.Context, string) (controlplane.WorkspaceUser, bool, error) {
	return controlplane.WorkspaceUser{}, false, r.err
}

func (r failingMFAUserRepository) FindWorkspaceUserByID(context.Context, string) (controlplane.WorkspaceUser, bool, error) {
	return controlplane.WorkspaceUser{}, false, r.err
}

func (r failingRegistrationRepository) SaveWorkspaceUser(context.Context, controlplane.WorkspaceUser) error {
	return r.err
}

func (r *failNthSettingsReadRepository) GetAll(ctx context.Context) (map[string]string, error) {
	r.mu.Lock()
	r.reads++
	shouldFail := r.failAt > 0 && r.reads == r.failAt
	err := r.err
	r.mu.Unlock()
	if shouldFail {
		return nil, err
	}
	return r.MemoryRepository.GetAll(ctx)
}

func (r *failNthSettingsReadRepository) reset(failAt int) {
	r.mu.Lock()
	r.reads = 0
	r.failAt = failAt
	r.mu.Unlock()
}

func (s *recordingAuthEmailSender) Send(_ context.Context, _, _, _, actionURL string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.urls = append(s.urls, actionURL)
	return s.err
}

func (s *recordingAuthEmailSender) setError(err error) {
	s.mu.Lock()
	s.err = err
	s.mu.Unlock()
}

func (s *recordingAuthEmailSender) lastURL(t *testing.T) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.urls) == 0 {
		t.Fatal("authentication email was not attempted")
	}
	return s.urls[len(s.urls)-1]
}

func (s *recordingAuthEmailSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.urls)
}

func newPublicAuthTestHandler(t *testing.T, sender AuthenticationEmailSender) http.Handler {
	t.Helper()
	settingsRepo := settings.NewMemoryRepository()
	if err := settingsRepo.SetMultiple(t.Context(), map[string]string{
		settings.KeyRegistrationEnabled:  "true",
		settings.KeyEmailVerifyEnabled:   "true",
		settings.KeyPasswordResetEnabled: "true",
		settings.KeyPublicBaseURL:        "https://router.example.test",
		settings.KeyTurnstileEnabled:     "true",
		settings.KeyTurnstileSecretKey:   "turnstile-secret",
	}); err != nil {
		t.Fatal(err)
	}
	settingsService := settings.NewService(settingsRepo, settings.ServiceOptions{Version: "test", StorageMode: "memory"})
	controlService := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1", "test-secret")
	admin, err := controlService.EnsureLocalAdmin(t.Context(), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	return New(Options{
		AuthService:     auth.NewService(auth.Config{Username: "admin", Password: "secret", PasswordHash: admin.PasswordHash, SecretKey: "test-secret"}),
		SettingsService: settingsService,
		ControlService:  controlService,
		HumanVerifier:   testHumanVerifier{token: "human-token"},
		AuthEmailSender: sender,
	})
}

func postAuthJSON(t *testing.T, handler http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func postAuthJSONFromAddress(t *testing.T, handler http.Handler, address, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.RemoteAddr = address
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestPublicAuthenticationWorkflowSecurityContract(t *testing.T) {
	sender := &recordingAuthEmailSender{err: errors.New("synthetic SMTP failure")}
	handler := newPublicAuthTestHandler(t, sender)

	blocked := postAuthJSON(t, handler, "/api/v1/auth/register", `{"email":"user@example.test","password":"long-password","display_name":"User"}`)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("registration without Turnstile status=%d body=%s", blocked.Code, blocked.Body.String())
	}

	registered := postAuthJSON(t, handler, "/api/v1/auth/register", `{"email":"user@example.test","password":"long-password","display_name":"User","turnstile_token":"human-token"}`)
	if registered.Code != http.StatusOK {
		t.Fatalf("registration status=%d body=%s", registered.Code, registered.Body.String())
	}
	var registrationResponse struct {
		Data struct {
			VerificationRequired bool `json:"verification_required"`
			EmailDeliveryFailed  bool `json:"email_delivery_failed"`
		} `json:"data"`
	}
	if err := json.Unmarshal(registered.Body.Bytes(), &registrationResponse); err != nil {
		t.Fatal(err)
	}
	if !registrationResponse.Data.VerificationRequired || !registrationResponse.Data.EmailDeliveryFailed {
		t.Fatalf("registration response=%s", registered.Body.String())
	}
	verificationURL, err := url.Parse(sender.lastURL(t))
	if err != nil {
		t.Fatal(err)
	}
	verificationToken := verificationURL.Query().Get("token")
	if verificationURL.Path != "/verify-email" || verificationToken == "" {
		t.Fatalf("verification URL=%q", verificationURL.String())
	}
	sender.setError(nil)
	resent := postAuthJSON(t, handler, "/api/v1/auth/resend-verification", `{"email":"user@example.test","turnstile_token":"human-token"}`)
	if resent.Code != http.StatusOK || sender.count() != 2 {
		t.Fatalf("immediate resend after delivery failure status=%d sends=%d body=%s", resent.Code, sender.count(), resent.Body.String())
	}
	resentURL, err := url.Parse(sender.lastURL(t))
	if err != nil {
		t.Fatal(err)
	}
	verificationToken = resentURL.Query().Get("token")
	verified := postAuthJSON(t, handler, "/api/v1/auth/verify-email", `{"token":"`+verificationToken+`"}`)
	if verified.Code != http.StatusOK {
		t.Fatalf("verification status=%d body=%s", verified.Code, verified.Body.String())
	}
	reused := postAuthJSON(t, handler, "/api/v1/auth/verify-email", `{"token":"`+verificationToken+`"}`)
	if reused.Code != http.StatusBadRequest || !bytes.Contains(reused.Body.Bytes(), []byte("invalid or expired")) {
		t.Fatalf("reused verification status=%d body=%s", reused.Code, reused.Body.String())
	}

	known := postAuthJSON(t, handler, "/api/v1/auth/forgot-password", `{"email":"user@example.test","turnstile_token":"human-token"}`)
	unknown := postAuthJSON(t, handler, "/api/v1/auth/forgot-password", `{"email":"missing@example.test","turnstile_token":"human-token"}`)
	if known.Code != http.StatusOK || unknown.Code != http.StatusOK || known.Body.String() != unknown.Body.String() {
		t.Fatalf("password reset enumeration leak: known=%d/%s unknown=%d/%s", known.Code, known.Body.String(), unknown.Code, unknown.Body.String())
	}
	invalidReset := postAuthJSON(t, handler, "/api/v1/auth/reset-password", `{"token":"invalid-token","password":"another-long-password"}`)
	if invalidReset.Code != http.StatusBadRequest || !bytes.Contains(invalidReset.Body.Bytes(), []byte("password reset link is invalid or expired")) {
		t.Fatalf("invalid reset status=%d body=%s", invalidReset.Code, invalidReset.Body.String())
	}
}

func TestEmailVerificationDistinguishesRepositoryFailureFromInvalidToken(t *testing.T) {
	const sensitiveMarker = "postgres://private-user:private-password@database.internal/asterrouter"
	repository := failingEmailVerificationRepository{
		Repository: controlplane.NewMemoryRepository(),
		err:        errors.New(sensitiveMarker),
	}
	handler := New(Options{
		SettingsService: settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"}),
		ControlService:  controlplane.NewService(repository, "/v1", "test-secret"),
	})

	failed := postAuthJSON(t, handler, "/api/v1/auth/verify-email", `{"token":"opaque-token"}`)
	if failed.Code != http.StatusInternalServerError || !strings.Contains(failed.Body.String(), "email verification failed") {
		t.Fatalf("repository failure status=%d body=%s", failed.Code, failed.Body.String())
	}
	if strings.Contains(failed.Body.String(), sensitiveMarker) || strings.Contains(failed.Body.String(), "private-password") {
		t.Fatalf("repository failure leaked internal detail: %s", failed.Body.String())
	}
}

func TestWorkspaceLoginInfrastructureFailuresAreNotReportedAsInvalidCredentials(t *testing.T) {
	const sensitiveMarker = "postgres://private-user:private-password@database.internal/asterrouter"

	t.Run("registration policy", func(t *testing.T) {
		settingsRepo := &failNthSettingsReadRepository{MemoryRepository: settings.NewMemoryRepository(), err: errors.New(sensitiveMarker)}
		settingsService := settings.NewService(settingsRepo, settings.ServiceOptions{Version: "test", StorageMode: "memory"})
		controlService := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1", "test-secret")
		if _, _, err := controlService.RegisterWorkspaceUser(t.Context(), "policy-failure@example.test", "long-password", "Policy", false); err != nil {
			t.Fatal(err)
		}
		handler := New(Options{
			AuthService:     auth.NewService(auth.Config{Username: "deployment-admin", Password: "admin-password", SecretKey: "test-secret"}),
			SettingsService: settingsService,
			ControlService:  controlService,
		})

		settingsRepo.reset(3)
		response := postAuthJSON(t, handler, "/api/v1/auth/login", `{"username":"policy-failure@example.test","password":"long-password"}`)
		if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "authentication settings are unavailable") {
			t.Fatalf("policy failure status=%d body=%s", response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), sensitiveMarker) || strings.Contains(response.Body.String(), "invalid username or password") {
			t.Fatalf("policy failure was misclassified or leaked details: %s", response.Body.String())
		}
	})

	t.Run("workspace repository", func(t *testing.T) {
		baseRepository := controlplane.NewMemoryRepository()
		registrationService := controlplane.NewService(baseRepository, "/v1", "test-secret")
		if _, _, err := registrationService.RegisterWorkspaceUser(t.Context(), "repository-failure@example.test", "long-password", "Repository", false); err != nil {
			t.Fatal(err)
		}
		controlService := controlplane.NewService(failingWorkspaceLoginRepository{Repository: baseRepository, err: errors.New(sensitiveMarker)}, "/v1", "test-secret")
		handler := New(Options{
			AuthService:     auth.NewService(auth.Config{Username: "deployment-admin", Password: "admin-password", SecretKey: "test-secret"}),
			SettingsService: settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"}),
			ControlService:  controlService,
		})

		response := postAuthJSON(t, handler, "/api/v1/auth/login", `{"username":"repository-failure@example.test","password":"long-password"}`)
		if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "authentication failed") {
			t.Fatalf("repository failure status=%d body=%s", response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), sensitiveMarker) || strings.Contains(response.Body.String(), "invalid username or password") {
			t.Fatalf("repository failure was misclassified or leaked details: %s", response.Body.String())
		}
	})
}

func TestRegistrationRepositoryFailureIsSanitizedServerError(t *testing.T) {
	const sensitiveMarker = "postgres://private-user:private-password@database.internal/asterrouter"
	settingsRepository := settings.NewMemoryRepository()
	if err := settingsRepository.SetMultiple(t.Context(), map[string]string{settings.KeyRegistrationEnabled: "true"}); err != nil {
		t.Fatal(err)
	}
	handler := New(Options{
		AuthService: auth.NewService(auth.Config{SecretKey: "test-secret"}),
		SettingsService: settings.NewService(settingsRepository, settings.ServiceOptions{
			Version: "test", StorageMode: "memory",
		}),
		ControlService: controlplane.NewService(failingRegistrationRepository{
			Repository: controlplane.NewMemoryRepository(),
			err:        errors.New(sensitiveMarker),
		}, "/v1", "test-secret"),
	})

	response := postAuthJSON(t, handler, "/api/v1/auth/register", `{"email":"failure@example.test","password":"long-password","display_name":"Failure"}`)
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "registration could not be completed") {
		t.Fatalf("registration failure status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), sensitiveMarker) || strings.Contains(response.Body.String(), "private-password") {
		t.Fatalf("registration failure leaked internal detail: %s", response.Body.String())
	}

	invalid := postAuthJSON(t, handler, "/api/v1/auth/register", `{"email":"not-an-email","password":"long-password","display_name":"Invalid"}`)
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), controlplane.ErrInvalidWorkspaceEmail.Error()) {
		t.Fatalf("invalid email status=%d body=%s", invalid.Code, invalid.Body.String())
	}
}

func TestMFARepositoryFailureDoesNotConsumeChallengeAttempts(t *testing.T) {
	const sensitiveMarker = "postgres://private-user:private-password@database.internal/asterrouter"
	authService := auth.NewService(auth.Config{SecretKey: "test-secret"})
	challenge, _, err := authService.BeginMFA("usr_mfa_failure", controlplane.RoleDeveloper)
	if err != nil {
		t.Fatal(err)
	}
	controlService := controlplane.NewService(failingMFAUserRepository{
		Repository: controlplane.NewMemoryRepository(),
		err:        errors.New(sensitiveMarker),
	}, "/v1", "test-secret")
	handler := New(Options{
		AuthService:     authService,
		SettingsService: settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"}),
		ControlService:  controlService,
	})

	for attempt := 1; attempt <= 5; attempt++ {
		response := postAuthJSON(t, handler, "/api/v1/auth/totp/login", `{"challenge":"`+challenge+`","code":"123456"}`)
		if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "authentication failed") {
			t.Fatalf("attempt %d status=%d body=%s", attempt, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), sensitiveMarker) || strings.Contains(response.Body.String(), "invalid TOTP code") {
			t.Fatalf("attempt %d was misclassified or leaked details: %s", attempt, response.Body.String())
		}
	}
	if _, _, ok := authService.InspectMFA(challenge); !ok {
		t.Fatal("repository failures consumed the MFA challenge attempt budget")
	}
}

func TestMFACompletionUsesCurrentWorkspaceRole(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	controlService := controlplane.NewService(repository, "/v1", "test-secret")
	user, _, err := controlService.RegisterWorkspaceUser(t.Context(), "mfa-role@example.test", "long-password", "MFA Role", false)
	if err != nil {
		t.Fatal(err)
	}
	setup, err := controlService.BeginTOTPSetup(t.Context(), user.ID, "long-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := controlService.ConfirmTOTP(t.Context(), user.ID, auth.GenerateTOTPCode(setup.Secret, time.Now().UTC())); err != nil {
		t.Fatal(err)
	}

	authService := auth.NewService(auth.Config{SecretKey: "test-secret"})
	challenge, _, err := authService.BeginMFA(user.ID, controlplane.RoleSuperAdmin)
	if err != nil {
		t.Fatal(err)
	}
	handler := New(Options{
		AuthService:     authService,
		SettingsService: settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"}),
		ControlService:  controlService,
	})

	response := postAuthJSON(t, handler, "/api/v1/auth/totp/login", `{"challenge":"`+challenge+`","code":"`+auth.GenerateTOTPCode(setup.Secret, time.Now().UTC())+`"}`)
	var payload struct {
		Data auth.LoginResult `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || payload.Data.User.Role != controlplane.RoleDeveloper {
		t.Fatalf("MFA completion status=%d role=%q body=%s", response.Code, payload.Data.User.Role, response.Body.String())
	}
	principal, err := authService.VerifyWithError(payload.Data.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if principal.Role != controlplane.RoleDeveloper {
		t.Fatalf("signed role = %q, want current role %q", principal.Role, controlplane.RoleDeveloper)
	}
}

func TestLogoutSessionFailuresAreReportedAndCookiesAreCleared(t *testing.T) {
	const sensitiveMarker = "postgres://private-user:private-password@database.internal/asterrouter"

	t.Run("revoke write", func(t *testing.T) {
		baseRepository := controlplane.NewMemoryRepository()
		baseService := controlplane.NewService(baseRepository, "/v1", "test-secret")
		user, _, err := baseService.RegisterWorkspaceUser(t.Context(), "logout-write@example.test", "long-password", "Logout", false)
		if err != nil {
			t.Fatal(err)
		}
		controlService := controlplane.NewService(failingRegistrationRepository{
			Repository: baseRepository,
			err:        errors.New(sensitiveMarker),
		}, "/v1", "test-secret")
		authService := auth.NewService(auth.Config{SecretKey: "test-secret"})
		handler := New(Options{
			AuthService:     authService,
			SettingsService: settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"}),
			ControlService:  controlService,
		})
		login, err := authService.LoginOIDC(user.ID, user.Role)
		if err != nil {
			t.Fatal(err)
		}

		response := postLogout(t, handler, login.AccessToken)
		assertLogoutFailure(t, response, sensitiveMarker)
	})

	t.Run("session read", func(t *testing.T) {
		authService := auth.NewService(auth.Config{SecretKey: "test-secret"})
		login, err := authService.LoginOIDC("usr_logout_read", controlplane.RoleDeveloper)
		if err != nil {
			t.Fatal(err)
		}
		controlService := controlplane.NewService(failingMFAUserRepository{
			Repository: controlplane.NewMemoryRepository(),
			err:        errors.New(sensitiveMarker),
		}, "/v1", "test-secret")
		handler := New(Options{
			AuthService:     authService,
			SettingsService: settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"}),
			ControlService:  controlService,
		})

		response := postLogout(t, handler, login.AccessToken)
		assertLogoutFailure(t, response, sensitiveMarker)
	})
}

func postLogout(t *testing.T, handler http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertLogoutFailure(t *testing.T, response *httptest.ResponseRecorder, sensitiveMarker string) {
	t.Helper()
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "logout could not be completed") {
		t.Fatalf("logout failure status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), sensitiveMarker) || strings.Contains(response.Body.String(), "private-password") {
		t.Fatalf("logout failure leaked internal detail: %s", response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) < 2 {
		t.Fatalf("logout failure did not clear session cookies: %#v", cookies)
	}
	for _, cookie := range cookies {
		if (cookie.Name == sessionCookieName || cookie.Name == csrfCookieName) && cookie.MaxAge >= 0 {
			t.Fatalf("cookie was not cleared: %#v", cookie)
		}
	}
}

func TestAuthenticationEmailQueueRejectionRollsBackIssuedTokens(t *testing.T) {
	sender := &recordingAuthEmailSender{err: errAuthenticationEmailDispatcherBusy}
	handler := newPublicAuthTestHandler(t, sender)
	registered := postAuthJSON(t, handler, "/api/v1/auth/register", `{"email":"retry@example.test","password":"long-password","display_name":"Retry","turnstile_token":"human-token"}`)
	if registered.Code != http.StatusOK || sender.count() != 1 {
		t.Fatalf("registration status=%d sends=%d body=%s", registered.Code, sender.count(), registered.Body.String())
	}

	failedResend := postAuthJSON(t, handler, "/api/v1/auth/resend-verification", `{"email":"retry@example.test","turnstile_token":"human-token"}`)
	if failedResend.Code != http.StatusOK || sender.count() != 2 {
		t.Fatalf("failed resend status=%d sends=%d body=%s", failedResend.Code, sender.count(), failedResend.Body.String())
	}
	failedVerificationURL, err := url.Parse(sender.lastURL(t))
	if err != nil {
		t.Fatal(err)
	}
	sender.setError(nil)
	successfulResend := postAuthJSON(t, handler, "/api/v1/auth/resend-verification", `{"email":"retry@example.test","turnstile_token":"human-token"}`)
	if successfulResend.Code != http.StatusOK || sender.count() != 3 {
		t.Fatalf("retry resend status=%d sends=%d body=%s", successfulResend.Code, sender.count(), successfulResend.Body.String())
	}
	failedToken := failedVerificationURL.Query().Get("token")
	failedVerification := postAuthJSON(t, handler, "/api/v1/auth/verify-email", `{"token":"`+failedToken+`"}`)
	if failedVerification.Code != http.StatusBadRequest {
		t.Fatalf("undispatched verification token status=%d body=%s", failedVerification.Code, failedVerification.Body.String())
	}
	successfulVerificationURL, err := url.Parse(sender.lastURL(t))
	if err != nil {
		t.Fatal(err)
	}
	verified := postAuthJSON(t, handler, "/api/v1/auth/verify-email", `{"token":"`+successfulVerificationURL.Query().Get("token")+`"}`)
	if verified.Code != http.StatusOK {
		t.Fatalf("retried verification status=%d body=%s", verified.Code, verified.Body.String())
	}

	sender.setError(errAuthenticationEmailDispatcherBusy)
	failedReset := postAuthJSON(t, handler, "/api/v1/auth/forgot-password", `{"email":"retry@example.test","turnstile_token":"human-token"}`)
	if failedReset.Code != http.StatusOK || sender.count() != 4 {
		t.Fatalf("failed reset status=%d sends=%d body=%s", failedReset.Code, sender.count(), failedReset.Body.String())
	}
	failedResetURL, err := url.Parse(sender.lastURL(t))
	if err != nil {
		t.Fatal(err)
	}
	sender.setError(nil)
	successfulReset := postAuthJSON(t, handler, "/api/v1/auth/forgot-password", `{"email":"retry@example.test","turnstile_token":"human-token"}`)
	if successfulReset.Code != http.StatusOK || sender.count() != 5 {
		t.Fatalf("retry reset status=%d sends=%d body=%s", successfulReset.Code, sender.count(), successfulReset.Body.String())
	}
	failedResetAttempt := postAuthJSON(t, handler, "/api/v1/auth/reset-password", `{"token":"`+failedResetURL.Query().Get("token")+`","password":"another-long-password"}`)
	if failedResetAttempt.Code != http.StatusBadRequest {
		t.Fatalf("undispatched reset token status=%d body=%s", failedResetAttempt.Code, failedResetAttempt.Body.String())
	}
	successfulResetURL, err := url.Parse(sender.lastURL(t))
	if err != nil {
		t.Fatal(err)
	}
	reset := postAuthJSON(t, handler, "/api/v1/auth/reset-password", `{"token":"`+successfulResetURL.Query().Get("token")+`","password":"another-long-password"}`)
	if reset.Code != http.StatusOK {
		t.Fatalf("retried reset status=%d body=%s", reset.Code, reset.Body.String())
	}
}

func TestAuthenticationEmailAsyncDeliveryFailureRollsBackIssuedTokens(t *testing.T) {
	sender := &deferredFailureAuthEmailSender{}
	handler := newPublicAuthTestHandler(t, sender)

	registered := postAuthJSON(t, handler, "/api/v1/auth/register", `{"email":"async-failure@example.test","password":"long-password","display_name":"Retry","turnstile_token":"human-token"}`)
	if registered.Code != http.StatusOK || sender.count() != 1 || bytes.Contains(registered.Body.Bytes(), []byte(`"email_delivery_failed":true`)) {
		t.Fatalf("registration status=%d sends=%d body=%s", registered.Code, sender.count(), registered.Body.String())
	}
	failedVerificationURL, err := url.Parse(sender.failLast(t))
	if err != nil {
		t.Fatal(err)
	}
	failedVerification := postAuthJSON(t, handler, "/api/v1/auth/verify-email", `{"token":"`+failedVerificationURL.Query().Get("token")+`"}`)
	if failedVerification.Code != http.StatusBadRequest {
		t.Fatalf("failed-delivery verification status=%d body=%s", failedVerification.Code, failedVerification.Body.String())
	}

	resent := postAuthJSON(t, handler, "/api/v1/auth/resend-verification", `{"email":"async-failure@example.test","turnstile_token":"human-token"}`)
	if resent.Code != http.StatusOK || sender.count() != 2 {
		t.Fatalf("resend status=%d sends=%d body=%s", resent.Code, sender.count(), resent.Body.String())
	}
	sender.mu.Lock()
	verificationURL := sender.urls[len(sender.urls)-1]
	sender.mu.Unlock()
	parsedVerificationURL, err := url.Parse(verificationURL)
	if err != nil {
		t.Fatal(err)
	}
	verified := postAuthJSON(t, handler, "/api/v1/auth/verify-email", `{"token":"`+parsedVerificationURL.Query().Get("token")+`"}`)
	if verified.Code != http.StatusOK {
		t.Fatalf("verification status=%d body=%s", verified.Code, verified.Body.String())
	}

	forgot := postAuthJSON(t, handler, "/api/v1/auth/forgot-password", `{"email":"async-failure@example.test","turnstile_token":"human-token"}`)
	if forgot.Code != http.StatusOK || sender.count() != 3 {
		t.Fatalf("forgot status=%d sends=%d body=%s", forgot.Code, sender.count(), forgot.Body.String())
	}
	failedResetURL, err := url.Parse(sender.failLast(t))
	if err != nil {
		t.Fatal(err)
	}
	failedReset := postAuthJSON(t, handler, "/api/v1/auth/reset-password", `{"token":"`+failedResetURL.Query().Get("token")+`","password":"another-long-password"}`)
	if failedReset.Code != http.StatusBadRequest {
		t.Fatalf("failed-delivery reset status=%d body=%s", failedReset.Code, failedReset.Body.String())
	}
	retried := postAuthJSON(t, handler, "/api/v1/auth/forgot-password", `{"email":"async-failure@example.test","turnstile_token":"human-token"}`)
	if retried.Code != http.StatusOK || sender.count() != 4 {
		t.Fatalf("retry forgot status=%d sends=%d body=%s", retried.Code, sender.count(), retried.Body.String())
	}
}

func TestAuthenticationEmailLinkSettingsFailureIsSanitizedAndRetryable(t *testing.T) {
	const sensitiveMarker = "postgres://private-user:private-password@database.internal/asterrouter"
	repo := &failNthSettingsReadRepository{MemoryRepository: settings.NewMemoryRepository(), err: errors.New(sensitiveMarker)}
	if err := repo.SetMultiple(t.Context(), map[string]string{
		settings.KeyRegistrationEnabled:  "true",
		settings.KeyEmailVerifyEnabled:   "true",
		settings.KeyPasswordResetEnabled: "true",
		settings.KeyPublicBaseURL:        "https://router.example.test",
		settings.KeyTurnstileEnabled:     "true",
		settings.KeyTurnstileSecretKey:   "turnstile-secret",
	}); err != nil {
		t.Fatal(err)
	}
	settingsService := settings.NewService(repo, settings.ServiceOptions{Version: "test", StorageMode: "memory"})
	controlService := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1", "test-secret")
	sender := &recordingAuthEmailSender{}
	handler := New(Options{
		AuthService:     auth.NewService(auth.Config{SecretKey: "test-secret"}),
		SettingsService: settingsService,
		ControlService:  controlService,
		HumanVerifier:   testHumanVerifier{token: "human-token"},
		AuthEmailSender: sender,
	})

	repo.reset(5)
	registered := postAuthJSON(t, handler, "/api/v1/auth/register", `{"email":"link-failure@example.test","password":"long-password","display_name":"Failure","turnstile_token":"human-token"}`)
	if registered.Code != http.StatusOK || sender.count() != 0 || !bytes.Contains(registered.Body.Bytes(), []byte(`"email_delivery_failed":true`)) {
		t.Fatalf("registration status=%d sends=%d body=%s", registered.Code, sender.count(), registered.Body.String())
	}
	if strings.Contains(registered.Body.String(), sensitiveMarker) {
		t.Fatalf("registration leaked settings error: %s", registered.Body.String())
	}

	resendUser, initialVerificationToken, err := controlService.RegisterWorkspaceUser(t.Context(), "resend-link@example.test", "long-password", "Resend", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := controlService.CancelEmailVerificationIssue(t.Context(), resendUser.ID, initialVerificationToken); err != nil {
		t.Fatal(err)
	}
	repo.reset(3)
	failedResend := postAuthJSON(t, handler, "/api/v1/auth/resend-verification", `{"email":"`+resendUser.Email+`","turnstile_token":"human-token"}`)
	if failedResend.Code != http.StatusOK || sender.count() != 0 || strings.Contains(failedResend.Body.String(), sensitiveMarker) {
		t.Fatalf("failed resend status=%d sends=%d body=%s", failedResend.Code, sender.count(), failedResend.Body.String())
	}
	repo.reset(0)
	retriedResend := postAuthJSON(t, handler, "/api/v1/auth/resend-verification", `{"email":"`+resendUser.Email+`","turnstile_token":"human-token"}`)
	if retriedResend.Code != http.StatusOK || sender.count() != 1 {
		t.Fatalf("retried resend status=%d sends=%d body=%s", retriedResend.Code, sender.count(), retriedResend.Body.String())
	}

	resetUser, _, err := controlService.RegisterWorkspaceUser(t.Context(), "reset-link@example.test", "long-password", "Reset", false)
	if err != nil {
		t.Fatal(err)
	}
	repo.reset(3)
	failedReset := postAuthJSON(t, handler, "/api/v1/auth/forgot-password", `{"email":"`+resetUser.Email+`","turnstile_token":"human-token"}`)
	if failedReset.Code != http.StatusOK || sender.count() != 1 || strings.Contains(failedReset.Body.String(), sensitiveMarker) {
		t.Fatalf("failed reset status=%d sends=%d body=%s", failedReset.Code, sender.count(), failedReset.Body.String())
	}
	repo.reset(0)
	retriedReset := postAuthJSON(t, handler, "/api/v1/auth/forgot-password", `{"email":"`+resetUser.Email+`","turnstile_token":"human-token"}`)
	if retriedReset.Code != http.StatusOK || sender.count() != 2 {
		t.Fatalf("retried reset status=%d sends=%d body=%s", retriedReset.Code, sender.count(), retriedReset.Body.String())
	}
}

func TestAuthenticationRateLimitIncludesRetryAfter(t *testing.T) {
	handler := newPublicAuthTestHandler(t, &recordingAuthEmailSender{})
	var rec *httptest.ResponseRecorder
	for range 11 {
		rec = postAuthJSON(t, handler, "/api/v1/auth/login", `{"username":"admin","password":"wrong-password","turnstile_token":"human-token"}`)
	}
	if rec == nil || rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("rate limit response status=%d retry-after=%q body=%s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
}

func TestAuthenticationPrincipalRateLimitSpansClientAddresses(t *testing.T) {
	handler := newPublicAuthTestHandler(t, &recordingAuthEmailSender{})
	var rec *httptest.ResponseRecorder
	for attempt := 1; attempt <= 11; attempt++ {
		address := "192.0.2." + strconv.Itoa(attempt) + ":12345"
		rec = postAuthJSONFromAddress(t, handler, address, "/api/v1/auth/login", `{"username":"admin","password":"wrong-password","turnstile_token":"human-token"}`)
	}
	if rec == nil || rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("distributed rate limit response status=%d retry-after=%q body=%s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
}

func TestRegistrationRateLimitAllowsDifferentPrincipalsFromSameAddress(t *testing.T) {
	handler := newPublicAuthTestHandler(t, &recordingAuthEmailSender{})
	for attempt := 1; attempt <= 6; attempt++ {
		email := "batch-user-" + strconv.Itoa(attempt) + "@example.test"
		body := `{"email":"` + email + `","password":"long-password","display_name":"User","turnstile_token":"human-token"}`
		rec := postAuthJSONFromAddress(t, handler, "192.0.2.10:12345", "/api/v1/auth/register", body)
		if rec.Code != http.StatusOK {
			t.Fatalf("registration %d status=%d body=%s", attempt, rec.Code, rec.Body.String())
		}
	}
}

func TestRegistrationPrincipalRateLimitNormalizesEmail(t *testing.T) {
	handler := newPublicAuthTestHandler(t, &recordingAuthEmailSender{})
	emails := []string{
		"User@example.test",
		" user@example.test ",
		"USER@example.test",
		"user@example.test",
		"User@Example.Test",
		"user@example.test",
	}
	var rec *httptest.ResponseRecorder
	for attempt, email := range emails {
		body := `{"email":"` + email + `","password":"long-password","display_name":"User","turnstile_token":"human-token"}`
		rec = postAuthJSONFromAddress(t, handler, "192.0.2.20:12345", "/api/v1/auth/register", body)
		if attempt == 0 && rec.Code != http.StatusOK {
			t.Fatalf("initial registration status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
	if rec == nil || rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("principal rate limit status=%d retry-after=%q body=%s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
}

func TestRegistrationRateLimitCapsTotalAttemptsPerAddress(t *testing.T) {
	handler := newPublicAuthTestHandler(t, &recordingAuthEmailSender{})
	var rec *httptest.ResponseRecorder
	for attempt := 1; attempt <= 31; attempt++ {
		rec = postAuthJSONFromAddress(t, handler, "192.0.2.30:12345", "/api/v1/auth/register", `{`)
		if attempt <= 30 && rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid registration %d status=%d body=%s", attempt, rec.Code, rec.Body.String())
		}
	}
	if rec == nil || rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("address rate limit status=%d retry-after=%q body=%s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
}

func TestAuthenticationResponsesDisableReferrerForwarding(t *testing.T) {
	handler := newPublicAuthTestHandler(t, &recordingAuthEmailSender{})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q, want no-referrer", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

func TestAuthenticationActionURLRejectsUnsafePersistedBaseURL(t *testing.T) {
	for _, baseURL := range []string{
		"http://router.example.test",
		"https://user:password@router.example.test",
		"https://router.example.test/untrusted-path",
		"https://router.example.test?redirect=https://attacker.example",
	} {
		t.Run(baseURL, func(t *testing.T) {
			repository := settings.NewMemoryRepository()
			if err := repository.SetMultiple(t.Context(), map[string]string{settings.KeyPublicBaseURL: baseURL}); err != nil {
				t.Fatal(err)
			}
			service := settings.NewService(repository, settings.ServiceOptions{Version: "test", StorageMode: "memory"})
			generated, err := authenticationActionURL(t.Context(), service, "/verify-email", "sensitive-token")
			if err == nil || generated != "" {
				t.Fatalf("authenticationActionURL() = %q, %v; want rejection", generated, err)
			}
			if strings.Contains(err.Error(), "sensitive-token") {
				t.Fatalf("validation error leaked token: %v", err)
			}
		})
	}
}
