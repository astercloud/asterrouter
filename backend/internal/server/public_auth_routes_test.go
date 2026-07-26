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
	"sync"
	"testing"

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

func (s *recordingAuthEmailSender) Send(_ context.Context, _, _, _, actionURL string) error {
	s.mu.Lock()
	s.urls = append(s.urls, actionURL)
	s.mu.Unlock()
	return s.err
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
		email := "rate-user-" + strconv.Itoa(attempt) + "@example.test"
		body := `{"email":"` + email + `","password":"long-password","display_name":"User","turnstile_token":"human-token"}`
		rec = postAuthJSONFromAddress(t, handler, "192.0.2.30:12345", "/api/v1/auth/register", body)
		if attempt <= 30 && rec.Code != http.StatusOK {
			t.Fatalf("registration %d status=%d body=%s", attempt, rec.Code, rec.Body.String())
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
