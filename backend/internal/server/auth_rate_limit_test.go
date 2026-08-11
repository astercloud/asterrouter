package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/settings"
)

func TestAuthAttemptLimiterBlocksAndResets(t *testing.T) {
	l := newAuthAttemptLimiter(2, time.Minute)
	now := time.Now()
	if !l.Allow("ip", now) {
		t.Fatal("limiter rejected the first attempt")
	}
	if !l.Allow("ip", now) {
		t.Fatal("limiter rejected the second attempt")
	}
	if l.Allow("ip", now) {
		t.Fatal("limiter allowed an attempt above the threshold")
	}
	if allowed, retryAfter := l.AllowWithRetry("ip", now); allowed || retryAfter != time.Minute {
		t.Fatalf("AllowWithRetry() = (%v, %v), want (false, 1m)", allowed, retryAfter)
	}
	l.Reset("ip")
	if !l.Allow("ip", now) {
		t.Fatal("reset did not clear attempts")
	}
	if !l.Allow("other", now) {
		t.Fatal("keys must be isolated")
	}
}

func TestAccountBindingStartIsLimitedPerAccountAndClient(t *testing.T) {
	handler := newAuthTestHandler(t)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"secret"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	var loginResponse struct {
		Data auth.LoginResult `json:"data"`
	}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginResponse); err != nil || loginRec.Code != http.StatusOK || loginResponse.Data.AccessToken == "" {
		t.Fatalf("login status=%d body=%s err=%v", loginRec.Code, loginRec.Body.String(), err)
	}

	for attempt := 1; attempt <= 10; attempt++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/account/identities/unsupported/bind", strings.NewReader(`{"return_path":"/console/account"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+loginResponse.Data.AccessToken)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("attempt %d status=%d body=%s", attempt, rec.Code, rec.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/identities/unsupported/bind", strings.NewReader(`{"return_path":"/console/account"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+loginResponse.Data.AccessToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("rate-limited status=%d retry-after=%q body=%s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
}

func TestAuthAttemptLimiterBoundsActiveKeysAndReclaimsCapacity(t *testing.T) {
	limiter := newAuthAttemptLimiter(2, time.Minute)
	limiter.maxKeys = 2
	now := time.Now().UTC()
	if !limiter.Allow("first", now) || !limiter.Allow("second", now) {
		t.Fatal("limiter rejected keys below capacity")
	}
	if allowed, retryAfter := limiter.AllowWithRetry("third", now); allowed || retryAfter <= 0 {
		t.Fatalf("capacity attempt = (%v, %v), want rejection with retry", allowed, retryAfter)
	}
	limiter.Reset("first")
	if !limiter.Allow("third", now) {
		t.Fatal("reset did not release key capacity")
	}
	if !limiter.Allow("fourth", now.Add(time.Minute)) {
		t.Fatal("expired keys were not reclaimed")
	}
}

func TestSuccessfulLoginsResetClientRateLimit(t *testing.T) {
	handler := newAuthTestHandler(t)
	for attempt := 1; attempt <= 61; attempt++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"secret"}`))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusOK {
			t.Fatalf("successful login %d status=%d body=%s", attempt, recorder.Code, recorder.Body.String())
		}
	}
}

func TestExternalLoginStartEndpointsAreRateLimited(t *testing.T) {
	const redirectURL = "https://router.example.test/api/v1/auth/callback"
	oidc, err := auth.NewOIDCService(auth.OIDCConfig{Enabled: true, IssuerURL: "https://id.example.test", ClientID: "client", RedirectURL: redirectURL})
	if err != nil {
		t.Fatal(err)
	}
	feishu, err := auth.NewFeishuService(auth.FeishuConfig{Enabled: true, Region: auth.FeishuRegionChina, AppID: "app", AppSecret: "secret", RedirectURL: redirectURL})
	if err != nil {
		t.Fatal(err)
	}
	dingTalk, err := auth.NewDingTalkService(auth.DingTalkConfig{Enabled: true, ClientID: "app", ClientSecret: "secret", RedirectURL: redirectURL})
	if err != nil {
		t.Fatal(err)
	}
	github, err := auth.NewSocialOAuthService(auth.SocialOAuthConfig{Enabled: true, Provider: "github", ClientID: "client", ClientSecret: "secret", RedirectURL: redirectURL})
	if err != nil {
		t.Fatal(err)
	}
	google, err := auth.NewSocialOAuthService(auth.SocialOAuthConfig{Enabled: true, Provider: "google", ClientID: "client", ClientSecret: "secret", RedirectURL: redirectURL})
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name    string
		path    string
		options Options
	}{
		{name: "OIDC", path: "/api/v1/auth/oidc", options: Options{OIDCService: oidc}},
		{name: "Feishu", path: "/api/v1/auth/feishu", options: Options{FeishuService: feishu}},
		{name: "DingTalk", path: "/api/v1/auth/dingtalk", options: Options{DingTalkService: dingTalk}},
		{name: "GitHub", path: "/api/v1/auth/oauth/github", options: Options{GitHubOAuthService: github}},
		{name: "Google", path: "/api/v1/auth/oauth/google", options: Options{GoogleOAuthService: google}},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.options.SettingsService = settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"})
			handler := New(test.options)
			for attempt := 1; attempt <= 30; attempt++ {
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))
				if recorder.Code != http.StatusFound {
					t.Fatalf("attempt %d status=%d body=%s", attempt, recorder.Code, recorder.Body.String())
				}
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))
			if recorder.Code != http.StatusTooManyRequests {
				t.Fatalf("rate-limited status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if recorder.Header().Get("Retry-After") == "" {
				t.Fatal("rate-limited response omitted Retry-After")
			}
		})
	}
}

func TestExternalLoginCallbackFailuresRedirectWithoutLeakingProviderErrors(t *testing.T) {
	const redirectURL = "https://router.example.test/api/v1/auth/callback"
	oidc, err := auth.NewOIDCService(auth.OIDCConfig{Enabled: true, IssuerURL: "https://id.example.test", ClientID: "client", RedirectURL: redirectURL})
	if err != nil {
		t.Fatal(err)
	}
	feishu, err := auth.NewFeishuService(auth.FeishuConfig{Enabled: true, Region: auth.FeishuRegionChina, AppID: "app", AppSecret: "secret", RedirectURL: redirectURL})
	if err != nil {
		t.Fatal(err)
	}
	dingTalk, err := auth.NewDingTalkService(auth.DingTalkConfig{Enabled: true, ClientID: "app", ClientSecret: "secret", RedirectURL: redirectURL})
	if err != nil {
		t.Fatal(err)
	}
	github, err := auth.NewSocialOAuthService(auth.SocialOAuthConfig{Enabled: true, Provider: "github", ClientID: "client", ClientSecret: "secret", RedirectURL: redirectURL})
	if err != nil {
		t.Fatal(err)
	}
	google, err := auth.NewSocialOAuthService(auth.SocialOAuthConfig{Enabled: true, Provider: "google", ClientID: "client", ClientSecret: "secret", RedirectURL: redirectURL})
	if err != nil {
		t.Fatal(err)
	}
	handler := New(Options{
		AuthService:        auth.NewService(auth.Config{SecretKey: "test-secret"}),
		OIDCService:        oidc,
		FeishuService:      feishu,
		DingTalkService:    dingTalk,
		GitHubOAuthService: github,
		GoogleOAuthService: google,
		SettingsService:    settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"}),
		ControlService:     controlplane.NewService(controlplane.NewMemoryRepository(), "/v1", "test-secret"),
	})

	for _, test := range []struct {
		provider string
		path     string
	}{
		{provider: "oidc", path: "/api/v1/auth/oidc/callback?state=missing&code=sensitive-code"},
		{provider: "feishu", path: "/api/v1/auth/feishu/callback?state=missing&code=sensitive-code"},
		{provider: "dingtalk", path: "/api/v1/auth/dingtalk/callback?state=missing&code=sensitive-code"},
		{provider: "github", path: "/api/v1/auth/oauth/github/callback?state=missing&code=sensitive-code"},
		{provider: "google", path: "/api/v1/auth/oauth/google/callback?state=missing&code=sensitive-code"},
	} {
		t.Run(test.provider, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))
			if recorder.Code != http.StatusFound {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			wantLocation := "/login?external=error&provider=" + test.provider
			if recorder.Header().Get("Location") != wantLocation {
				t.Fatalf("Location=%q, want %q", recorder.Header().Get("Location"), wantLocation)
			}
			if strings.Contains(strings.ToLower(recorder.Body.String()), "invalid state") || strings.Contains(recorder.Body.String(), "sensitive-code") {
				t.Fatalf("callback response leaked provider details: %s", recorder.Body.String())
			}
		})
	}
}
