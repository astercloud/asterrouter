package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
)

func TestOIDCStateIsSingleUseAndPKCEURLIsBound(t *testing.T) {
	svc, err := NewOIDCService(OIDCConfig{Enabled: true, IssuerURL: "https://id.example.test", ClientID: "client", RedirectURL: "https://router.example.test/api/v1/auth/oidc/callback"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	state, err := svc.Begin(now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(svc.AuthorizationURL(state), "code_challenge_method=S256") {
		t.Fatal("missing PKCE method")
	}
	if _, err := svc.Consume(state.Value, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Consume(state.Value, now.Add(time.Minute)); err != ErrOIDCInvalidState {
		t.Fatalf("second consume error = %v", err)
	}
}

func TestOIDCPendingStateCapacityRecoversAfterConsumeAndExpiry(t *testing.T) {
	service, err := NewOIDCService(OIDCConfig{
		Enabled: true, IssuerURL: "https://id.example.test", ClientID: "client",
		RedirectURL: "https://router.example.test/api/v1/auth/oidc/callback", StateTTL: time.Minute, MaxPendingStates: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	first, err := service.Begin(now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Begin(now); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Begin(now); err != ErrOIDCStateCapacity {
		t.Fatalf("capacity error = %v, want %v", err, ErrOIDCStateCapacity)
	}
	if _, err := service.Consume(first.Value, now); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Begin(now); err != nil {
		t.Fatalf("consumed state did not release capacity: %v", err)
	}
	if _, err := service.Begin(now.Add(2 * time.Minute)); err != nil {
		t.Fatalf("expired states did not release capacity: %v", err)
	}
}

func TestMapOIDCProfileUsesStableSubjectAndFallbackName(t *testing.T) {
	profile, err := MapOIDCProfile(map[string]any{"sub": "id-1", "email": "USER@EXAMPLE.TEST", "preferred_username": "user", "department": "eng"})
	if err != nil {
		t.Fatal(err)
	}
	if profile.Subject != "id-1" || profile.Email != "user@example.test" || profile.DisplayName != "user" || profile.Department != "eng" {
		t.Fatalf("profile = %+v", profile)
	}
	if _, err := MapOIDCProfile(map[string]any{}); err != ErrOIDCInvalidProfile {
		t.Fatalf("missing subject error = %v", err)
	}
}

func TestOIDCEmailVerifiedClaimRequiresBooleanTrue(t *testing.T) {
	if !oidcEmailVerified(map[string]any{"email_verified": true}) {
		t.Fatal("boolean true must be accepted")
	}
	for _, claims := range []map[string]any{{}, {"email_verified": false}, {"email_verified": "true"}, {"email_verified": 1}} {
		if oidcEmailVerified(claims) {
			t.Fatalf("unexpected verified claim: %#v", claims)
		}
	}
}

func TestOIDCEmailTrustRequiresExplicitProviderPolicy(t *testing.T) {
	claims := map[string]any{"email_verified": true}
	if trustedOIDCEmailVerified(false, claims) {
		t.Fatal("an optional email_verified claim must not silently change local verification state")
	}
	if !trustedOIDCEmailVerified(true, claims) {
		t.Fatal("a required boolean email_verified claim must be trusted")
	}
	if trustedOIDCEmailVerified(true, map[string]any{"email_verified": "true"}) {
		t.Fatal("a non-boolean email_verified claim must not be trusted")
	}
}

func TestOIDCInitializeConfiguresConfidentialClientSecret(t *testing.T) {
	var issuer string
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": issuer, "authorization_endpoint": issuer + "/authorize",
			"token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	}))
	defer provider.Close()
	issuer = provider.URL

	service, err := NewOIDCService(OIDCConfig{
		Enabled: true, IssuerURL: issuer, ClientID: "client", ClientSecret: "confidential-secret",
		RedirectURL: "https://router.example.test/api/v1/auth/oidc/callback",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := coreoidc.ClientContext(t.Context(), provider.Client())
	if err := service.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	config, err := service.OAuthConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.ClientSecret != "confidential-secret" {
		t.Fatal("OIDC client secret was not passed to the token exchange configuration")
	}
}
