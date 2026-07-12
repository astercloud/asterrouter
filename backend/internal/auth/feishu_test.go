package auth

import (
	"strings"
	"testing"
	"time"
)

func TestFeishuAuthorizationURLUsesRegionalOfficialDomain(t *testing.T) {
	for _, tc := range []struct{ region, host string }{{FeishuRegionChina, "open.feishu.cn"}, {FeishuRegionGlobal, "open.larksuite.com"}} {
		svc, err := NewFeishuService(FeishuConfig{Enabled: true, Region: tc.region, AppID: "app", AppSecret: "secret", RedirectURL: "https://router.example.test/api/v1/auth/feishu/callback"})
		if err != nil {
			t.Fatal(err)
		}
		state, err := svc.Begin(time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		u := svc.AuthorizationURL(state.Value, PKCEChallenge(state.Verifier))
		if !strings.Contains(u, tc.host) || !strings.Contains(u, "code_challenge_method=S256") || !strings.Contains(u, "state=") {
			t.Fatalf("authorization URL = %s", u)
		}
	}
}

func TestFeishuRegionValidation(t *testing.T) {
	if _, err := NewFeishuService(FeishuConfig{Enabled: true, Region: "invalid", AppID: "app", AppSecret: "secret", RedirectURL: "https://router.example.test/callback"}); err == nil {
		t.Fatal("invalid region should fail")
	}
}
