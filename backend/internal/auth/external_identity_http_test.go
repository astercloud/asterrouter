package auth

import (
	"net/http"
	"testing"
)

func TestExternalIdentityServicesUseBoundedHTTPClients(t *testing.T) {
	const redirectURL = "https://router.example.test/api/v1/auth/callback"
	oidc, err := NewOIDCService(OIDCConfig{Enabled: true, IssuerURL: "https://id.example.test", ClientID: "client", RedirectURL: redirectURL})
	if err != nil {
		t.Fatal(err)
	}
	feishu, err := NewFeishuService(FeishuConfig{Enabled: true, Region: FeishuRegionChina, AppID: "app", AppSecret: "secret", RedirectURL: redirectURL})
	if err != nil {
		t.Fatal(err)
	}
	dingTalk, err := NewDingTalkService(DingTalkConfig{Enabled: true, ClientID: "app", ClientSecret: "secret", RedirectURL: redirectURL})
	if err != nil {
		t.Fatal(err)
	}
	github, err := NewSocialOAuthService(SocialOAuthConfig{Enabled: true, Provider: "github", ClientID: "client", ClientSecret: "secret", RedirectURL: redirectURL})
	if err != nil {
		t.Fatal(err)
	}
	google, err := NewSocialOAuthService(SocialOAuthConfig{Enabled: true, Provider: "google", ClientID: "client", ClientSecret: "secret", RedirectURL: redirectURL})
	if err != nil {
		t.Fatal(err)
	}

	for name, client := range map[string]*http.Client{
		"oidc": oidc.client, "feishu": feishu.client, "dingtalk": dingTalk.client,
		"github": github.client, "google": google.client,
	} {
		if client == nil {
			t.Fatalf("%s client is nil", name)
		}
		if client.Timeout != externalIdentityHTTPTimeout {
			t.Fatalf("%s client timeout = %v, want %v", name, client.Timeout, externalIdentityHTTPTimeout)
		}
	}
}

func TestExternalIdentityServicesRejectInsecureCallbackURLs(t *testing.T) {
	for name, create := range map[string]func() error{
		"oidc": func() error {
			_, err := NewOIDCService(OIDCConfig{Enabled: true, IssuerURL: "https://id.example.test", ClientID: "client", RedirectURL: "http://router.example.test/callback"})
			return err
		},
		"feishu": func() error {
			_, err := NewFeishuService(FeishuConfig{Enabled: true, Region: FeishuRegionChina, AppID: "app", AppSecret: "secret", RedirectURL: "http://router.example.test/callback"})
			return err
		},
		"dingtalk": func() error {
			_, err := NewDingTalkService(DingTalkConfig{Enabled: true, ClientID: "app", ClientSecret: "secret", RedirectURL: "http://router.example.test/callback"})
			return err
		},
		"github": func() error {
			_, err := NewSocialOAuthService(SocialOAuthConfig{Enabled: true, Provider: "github", ClientID: "client", ClientSecret: "secret", RedirectURL: "http://router.example.test/callback"})
			return err
		},
		"google": func() error {
			_, err := NewSocialOAuthService(SocialOAuthConfig{Enabled: true, Provider: "google", ClientID: "client", ClientSecret: "secret", RedirectURL: "http://router.example.test/callback"})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := create(); err == nil {
				t.Fatal("insecure callback URL was accepted")
			}
		})
	}
}
