package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/settings"
)

func TestExternalLoginStartsBindStateToBrowserCookie(t *testing.T) {
	oidc, feishu, dingTalk, github, google := newExternalOAuthTestServices(t)

	for _, test := range []struct {
		provider string
		path     string
		options  Options
	}{
		{provider: "oidc", path: "/api/v1/auth/oidc", options: Options{OIDCService: oidc}},
		{provider: "feishu", path: "/api/v1/auth/feishu", options: Options{FeishuService: feishu}},
		{provider: "dingtalk", path: "/api/v1/auth/dingtalk", options: Options{DingTalkService: dingTalk}},
		{provider: "github", path: "/api/v1/auth/oauth/github", options: Options{GitHubOAuthService: github}},
		{provider: "google", path: "/api/v1/auth/oauth/google", options: Options{GoogleOAuthService: google}},
	} {
		t.Run(test.provider, func(t *testing.T) {
			test.options.SettingsService = settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"})
			recorder := httptest.NewRecorder()
			New(test.options).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))
			if recorder.Code != http.StatusFound {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			location, err := url.Parse(recorder.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			state := location.Query().Get("state")
			if state == "" {
				t.Fatal("authorization redirect omitted state")
			}
			cookie := responseCookie(t, recorder, externalOAuthStateCookieName(test.provider))
			if cookie.Value != state || cookie.Path != externalOAuthCookiePath || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge <= 0 {
				t.Fatalf("state cookie = %#v", cookie)
			}
			if recorder.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("Cache-Control=%q", recorder.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestExternalLoginCallbacksRejectMismatchedBrowserState(t *testing.T) {
	oidc, feishu, dingTalk, github, google := newExternalOAuthTestServices(t)
	for _, test := range []struct {
		provider string
		path     string
		options  Options
		begin    func(time.Time) (auth.OIDCState, error)
	}{
		{provider: "oidc", path: "/api/v1/auth/oidc/callback", options: Options{OIDCService: oidc}, begin: oidc.Begin},
		{provider: "feishu", path: "/api/v1/auth/feishu/callback", options: Options{FeishuService: feishu}, begin: feishu.Begin},
		{provider: "dingtalk", path: "/api/v1/auth/dingtalk/callback", options: Options{DingTalkService: dingTalk}, begin: dingTalk.Begin},
		{provider: "github", path: "/api/v1/auth/oauth/github/callback", options: Options{GitHubOAuthService: github}, begin: github.Begin},
		{provider: "google", path: "/api/v1/auth/oauth/google/callback", options: Options{GoogleOAuthService: google}, begin: google.Begin},
	} {
		t.Run(test.provider, func(t *testing.T) {
			entry, err := test.begin(time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			test.options.AuthService = auth.NewService(auth.Config{SecretKey: "test-secret"})
			test.options.SettingsService = settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"})
			test.options.ControlService = controlplane.NewService(controlplane.NewMemoryRepository(), "/v1", "test-secret")

			request := httptest.NewRequest(http.MethodGet, test.path+"?state="+url.QueryEscape(entry.Value)+"&code=synthetic-code", nil)
			request.AddCookie(&http.Cookie{Name: externalOAuthStateCookieName(test.provider), Value: "different-state", Path: externalOAuthCookiePath})
			recorder := httptest.NewRecorder()
			New(test.options).ServeHTTP(recorder, request)

			wantLocation := "/login?external=error&provider=" + test.provider
			if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != wantLocation {
				t.Fatalf("status=%d location=%q body=%s", recorder.Code, recorder.Header().Get("Location"), recorder.Body.String())
			}
			cleared := responseCookie(t, recorder, externalOAuthStateCookieName(test.provider))
			if cleared.MaxAge != -1 || cleared.Value != "" {
				t.Fatalf("cleared state cookie = %#v", cleared)
			}
		})
	}
}

func TestExternalAccountBindingStartsBindStateToBrowserCookie(t *testing.T) {
	oidc, feishu, dingTalk, github, google := newExternalOAuthTestServices(t)
	for _, test := range []struct {
		provider string
		options  Options
	}{
		{provider: "oidc", options: Options{OIDCService: oidc}},
		{provider: "feishu", options: Options{FeishuService: feishu}},
		{provider: "dingtalk", options: Options{DingTalkService: dingTalk}},
		{provider: "github", options: Options{GitHubOAuthService: github}},
		{provider: "google", options: Options{GoogleOAuthService: google}},
	} {
		t.Run(test.provider, func(t *testing.T) {
			test.options.Runtime.AdminToken = "test-admin-token"
			test.options.SettingsService = settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"})
			test.options.ControlService = controlplane.NewService(controlplane.NewMemoryRepository(), "/v1", "test-secret")

			request := httptest.NewRequest(http.MethodPost, "/api/v1/account/identities/"+test.provider+"/bind", bytes.NewBufferString(`{"return_path":"/account"}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Admin-Token", "test-admin-token")
			recorder := httptest.NewRecorder()
			New(test.options).ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			var response struct {
				Data struct {
					AuthorizationURL string `json:"authorization_url"`
				} `json:"data"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			authorizationURL, err := url.Parse(response.Data.AuthorizationURL)
			if err != nil {
				t.Fatal(err)
			}
			state := authorizationURL.Query().Get("state")
			if state == "" {
				t.Fatal("authorization URL omitted state")
			}
			cookie := responseCookie(t, recorder, externalOAuthStateCookieName(test.provider))
			if cookie.Value != state || cookie.Path != externalOAuthCookiePath || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
				t.Fatalf("state cookie = %#v", cookie)
			}
		})
	}
}

func TestExternalLoginCallbackRequiresMatchingBrowserState(t *testing.T) {
	for _, test := range []struct {
		name         string
		cookieState  func(string) string
		wantConsumed bool
	}{
		{name: "missing cookie", cookieState: func(string) string { return "" }},
		{name: "mismatched cookie", cookieState: func(string) string { return "different-state" }},
		{name: "matching cookie", cookieState: func(state string) string { return state }, wantConsumed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			oidc, err := auth.NewOIDCService(auth.OIDCConfig{Enabled: true, IssuerURL: "https://id.example.test", ClientID: "client", RedirectURL: "https://router.example.test/api/v1/auth/oidc/callback"})
			if err != nil {
				t.Fatal(err)
			}
			entry, err := oidc.Begin(time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			handler := New(Options{
				AuthService:     auth.NewService(auth.Config{SecretKey: "test-secret"}),
				OIDCService:     oidc,
				SettingsService: settings.NewService(settings.NewMemoryRepository(), settings.ServiceOptions{Version: "test", StorageMode: "memory"}),
				ControlService:  controlplane.NewService(controlplane.NewMemoryRepository(), "/v1", "test-secret"),
			})
			request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?state="+url.QueryEscape(entry.Value)+"&code=synthetic-code", nil)
			if cookieState := test.cookieState(entry.Value); cookieState != "" {
				request.AddCookie(&http.Cookie{Name: externalOAuthStateCookieName("oidc"), Value: cookieState, Path: externalOAuthCookiePath})
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/login?external=error&provider=oidc" {
				t.Fatalf("status=%d location=%q body=%s", recorder.Code, recorder.Header().Get("Location"), recorder.Body.String())
			}

			_, consumeErr := oidc.Consume(entry.Value, time.Now().UTC())
			consumed := errors.Is(consumeErr, auth.ErrOIDCInvalidState)
			if consumed != test.wantConsumed {
				t.Fatalf("state consumed=%v error=%v, want %v", consumed, consumeErr, test.wantConsumed)
			}
			cleared := responseCookie(t, recorder, externalOAuthStateCookieName("oidc"))
			if cleared.MaxAge != -1 || cleared.Value != "" {
				t.Fatalf("cleared state cookie = %#v", cleared)
			}
		})
	}
}

func newExternalOAuthTestServices(t *testing.T) (*auth.OIDCService, *auth.FeishuService, *auth.DingTalkService, *auth.SocialOAuthService, *auth.SocialOAuthService) {
	t.Helper()
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
	return oidc, feishu, dingTalk, github, google
}
