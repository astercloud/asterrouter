package server

import (
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
