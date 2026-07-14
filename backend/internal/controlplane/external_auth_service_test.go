package controlplane

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

func TestExternalAuthContextAuthenticatesWithinIntegrationCeilingAndSnapshotsEvidence(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "external-auth-test-secret")
	now := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	identity := createExternalAuthIdentity(t, ctx, svc)
	created, err := svc.CreateExternalAuthIntegration(ctx, "operator", ExternalAuthIntegrationRequest{
		TenantID: identity.tenant.ID, GatewayPrincipalID: identity.principal.ID,
		Name: "Product backend", KeyID: "product-v1", Audience: "https://gateway.example/v1",
		ModelAllowlist: []string{"model-a", "model-b"}, QPSLimit: 10, MonthlyTokenLimit: 1000,
		MaxTTLSeconds: 300, Status: ExternalAuthIntegrationStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateExternalAuthIntegration(): %v", err)
	}
	if created.Secret == "" || !created.Record.SecretConfigured || created.Record.SecretCiphertext != "" {
		t.Fatalf("created integration leaked or omitted secret state: %+v", created)
	}
	stored, err := svc.ListExternalAuthIntegrations(ctx)
	if err != nil || len(stored) != 1 || stored[0].SecretCiphertext != "" || stored[0].SecretHint == "" || stored[0].GatewayPrincipalID != identity.principal.ID {
		t.Fatalf("stored integration=%+v err=%v", stored, err)
	}

	claims := validExternalAuthClaims(created.Record, "subject_opaque_1", now)
	claims.ModelAllowlist = []string{"model-a"}
	claims.QPSLimit = 4
	claims.MonthlyTokenLimit = 400
	token := signExternalAuthContext(t, claims, created.Secret)
	auth, err := svc.AuthorizeGatewayCredential(ctx, "", token, "model-a")
	if err != nil {
		t.Fatalf("AuthorizeGatewayCredential(): %v", err)
	}
	if auth.ExternalAuthIntegration == nil || auth.ExternalAuthIntegration.ID != created.Record.ID || auth.ExternalSubjectReference != claims.SubjectReference {
		t.Fatalf("external auth context=%+v", auth)
	}
	if auth.APIKey.ID == "" || auth.APIKey.ID == created.Record.ID || auth.APIKey.QPSLimit != 4 || auth.APIKey.MonthlyTokenLimit != 400 || auth.APIKey.ProfileScope != ProfileScopePlatform {
		t.Fatalf("synthetic gateway principal=%+v", auth.APIKey)
	}
	if err := svc.RecordGatewayCall(ctx, auth, "model-a", "forwarded", "external context call"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordGatewayUsage(ctx, auth, GatewayUsageInput{Model: "model-a", Status: "forwarded", InputTokens: 2, OutputTokens: 3}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordGatewayTrace(ctx, auth, GatewayTraceInput{Model: "model-a", Status: "forwarded"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.syncAPIKeyQuotaAlert(ctx, auth, 500, now); err != nil {
		t.Fatal(err)
	}
	usage, err := svc.UsageReportQuery(ctx, UsageQuery{ProfileScope: ProfileScopePlatform, ExternalAuthIntegrationID: created.Record.ID})
	if err != nil || len(usage.Recent) != 1 || usage.Recent[0].ExternalSubjectReference != claims.SubjectReference {
		t.Fatalf("usage=%+v err=%v", usage, err)
	}
	traces, err := svc.ListGatewayTracesQuery(ctx, GatewayTraceQuery{ProfileScope: ProfileScopePlatform, ExternalAuthIntegrationID: created.Record.ID})
	if err != nil || len(traces) != 1 || traces[0].ExternalSubjectReference != claims.SubjectReference {
		t.Fatalf("traces=%+v err=%v", traces, err)
	}
	audit, err := svc.ListAuditLogsQuery(ctx, AuditLogQuery{ProfileScope: ProfileScopePlatform, ExternalAuthIntegrationID: created.Record.ID})
	if err != nil || len(audit) == 0 {
		t.Fatalf("audit=%+v err=%v", audit, err)
	}
	for _, event := range audit {
		if strings.Contains(event.Actor, created.Secret) || strings.Contains(event.Summary, created.Secret) || event.ExternalSubjectReference != claims.SubjectReference {
			t.Fatalf("external auth audit leaked secret or missed subject: %+v", event)
		}
	}
	alerts, err := svc.ListAlertEventsQuery(ctx, AlertQuery{ProfileScope: ProfileScopePlatform, ExternalAuthIntegrationID: created.Record.ID})
	if err != nil || len(alerts) != 1 || alerts[0].ExternalSubjectReference != claims.SubjectReference {
		t.Fatalf("alerts=%+v err=%v", alerts, err)
	}
}

func TestExternalAuthContextFailsClosedForInvalidOrExpandedClaims(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "external-auth-test-secret")
	now := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	identity := createExternalAuthIdentity(t, ctx, svc)
	created, err := svc.CreateExternalAuthIntegration(ctx, "operator", ExternalAuthIntegrationRequest{
		TenantID: identity.tenant.ID, GatewayPrincipalID: identity.principal.ID,
		Name: "Product backend", KeyID: "product-v1", Audience: "https://gateway.example/v1",
		ModelAllowlist: []string{"model-a"}, QPSLimit: 2, MonthlyTokenLimit: 100,
		MaxTTLSeconds: 300, Status: ExternalAuthIntegrationStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	base := validExternalAuthClaims(created.Record, "subject_opaque_1", now)
	tests := []struct {
		name   string
		mutate func(*ExternalAuthContextClaims)
		secret string
	}{
		{name: "wrong signature", secret: "wrong-secret"},
		{name: "expired", mutate: func(c *ExternalAuthContextClaims) {
			c.IssuedAt = now.Add(-2 * time.Minute).Unix()
			c.ExpiresAt = now.Add(-time.Minute).Unix()
		}},
		{name: "future issue time", mutate: func(c *ExternalAuthContextClaims) {
			c.IssuedAt = now.Add(time.Minute).Unix()
			c.ExpiresAt = now.Add(2 * time.Minute).Unix()
		}},
		{name: "ttl exceeds ceiling", mutate: func(c *ExternalAuthContextClaims) { c.ExpiresAt = now.Add(301 * time.Second).Unix() }},
		{name: "wrong audience", mutate: func(c *ExternalAuthContextClaims) { c.Audience = "https://other.example/v1" }},
		{name: "wrong tenant", mutate: func(c *ExternalAuthContextClaims) { c.TenantID = "ptn_other" }},
		{name: "wrong key id", mutate: func(c *ExternalAuthContextClaims) { c.KeyID = "other" }},
		{name: "model expands ceiling", mutate: func(c *ExternalAuthContextClaims) { c.ModelAllowlist = []string{"model-a", "model-b"} }},
		{name: "qps expands ceiling", mutate: func(c *ExternalAuthContextClaims) { c.QPSLimit = 3 }},
		{name: "quota expands ceiling", mutate: func(c *ExternalAuthContextClaims) { c.MonthlyTokenLimit = 101 }},
		{name: "empty subject", mutate: func(c *ExternalAuthContextClaims) { c.SubjectReference = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			claims := base
			if test.mutate != nil {
				test.mutate(&claims)
			}
			secret := created.Secret
			if test.secret != "" {
				secret = test.secret
			}
			if _, err := svc.AuthenticateExternalAuthContext(ctx, signExternalAuthContext(t, claims, secret)); !errors.Is(err, ErrGatewayUnauthorized) {
				t.Fatalf("AuthenticateExternalAuthContext() err=%v, want unauthorized", err)
			}
		})
	}

	if _, err := svc.UpdateGatewayPrincipal(ctx, "operator", identity.principal.ID, GatewayPrincipalRequest{TenantID: identity.tenant.ID, Name: identity.principal.Name, PrincipalType: identity.principal.PrincipalType, Status: GatewayPrincipalStatusDisabled}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateExternalAuthContext(ctx, signExternalAuthContext(t, base, created.Secret)); !errors.Is(err, ErrGatewayUnauthorized) {
		t.Fatalf("disabled principal err=%v, want unauthorized", err)
	}
}

func TestExternalAuthIntegrationLifecyclePreservesSecretAndRejectsInvalidBinding(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "external-auth-test-secret")
	identity := createExternalAuthIdentity(t, ctx, svc)
	if _, err := svc.CreateExternalAuthIntegration(ctx, "operator", ExternalAuthIntegrationRequest{
		TenantID: identity.tenant.ID, GatewayPrincipalID: "missing", Name: "bad", KeyID: "bad", Audience: "aud", ModelAllowlist: []string{"model"}, QPSLimit: 1, MonthlyTokenLimit: 1,
	}); err == nil {
		t.Fatal("accepted missing gateway principal")
	}
	created, err := svc.CreateExternalAuthIntegration(ctx, "operator", ExternalAuthIntegrationRequest{
		TenantID: identity.tenant.ID, GatewayPrincipalID: identity.principal.ID, Name: "Product", KeyID: "product-key", Audience: "aud", ModelAllowlist: []string{"model"}, QPSLimit: 1, MonthlyTokenLimit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := svc.UpdateExternalAuthIntegration(ctx, "operator", created.Record.ID, ExternalAuthIntegrationRequest{
		TenantID: identity.tenant.ID, GatewayPrincipalID: identity.principal.ID, Name: "Product renamed", KeyID: "product-key", Audience: "aud", ModelAllowlist: []string{"model"}, QPSLimit: 1, MonthlyTokenLimit: 1, Status: ExternalAuthIntegrationStatusDisabled,
	})
	if err != nil || !updated.SecretConfigured || updated.SecretHint != created.Record.SecretHint || updated.SecretCiphertext != "" {
		t.Fatalf("update=%+v err=%v", updated, err)
	}
	rotated, err := svc.RotateExternalAuthIntegrationSecret(ctx, "operator", created.Record.ID)
	if err != nil || rotated.Secret == "" || rotated.Secret == created.Secret || rotated.Record.SecretHint == created.Record.SecretHint {
		t.Fatalf("rotate=%+v err=%v", rotated, err)
	}
	if _, err := svc.UpdateExternalAuthIntegration(ctx, "operator", created.Record.ID, ExternalAuthIntegrationRequest{TenantID: identity.tenant.ID, GatewayPrincipalID: identity.principal.ID, Name: "Product", KeyID: "product-key", Audience: "aud", ModelAllowlist: []string{"model"}, QPSLimit: 1, MonthlyTokenLimit: 1, Secret: "should-not-update"}); err == nil {
		t.Fatal("update accepted secret rotation bypass")
	}
	if _, err := svc.UpdateExternalAuthIntegration(ctx, "operator", created.Record.ID, ExternalAuthIntegrationRequest{TenantID: identity.tenant.ID, GatewayPrincipalID: identity.principal.ID, Name: "Product", KeyID: "changed-key-id", Audience: "aud", ModelAllowlist: []string{"model"}, QPSLimit: 1, MonthlyTokenLimit: 1}); err == nil {
		t.Fatal("update accepted key_id reassignment")
	}
}

func TestExternalJWTAuthenticatesWithinIntegrationCeilingAndFailsClosed(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "external-auth-test-secret")
	now := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	svc.externalAuthJWKSFetcher = func(context.Context, string) (jose.JSONWebKeySet, error) {
		return jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &privateKey.PublicKey, KeyID: "rotation-1", Algorithm: string(jose.RS256), Use: "sig"}}}, nil
	}
	identity := createExternalAuthIdentity(t, ctx, svc)
	created, err := svc.CreateExternalAuthIntegration(ctx, "operator", ExternalAuthIntegrationRequest{
		TenantID: identity.tenant.ID, GatewayPrincipalID: identity.principal.ID,
		Name: "JWT product", Protocol: ExternalAuthIntegrationProtocolJWT, KeyID: "jwt-product-v1",
		Issuer: "https://identity.example", JWKSURL: "https://identity.example/.well-known/jwks.json",
		SubjectClaim: "subject_ref", ModelsClaim: "ai_models", QPSLimitClaim: "ai_qps", MonthlyTokenClaim: "ai_monthly_tokens",
		Audience: "https://gateway.example/v1", ModelAllowlist: []string{"model-a", "model-b"}, QPSLimit: 10, MonthlyTokenLimit: 1000,
		MaxTTLSeconds: 300, Status: ExternalAuthIntegrationStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateExternalAuthIntegration(): %v", err)
	}
	if created.Secret != "" || created.Record.SecretConfigured || created.Record.Issuer != "https://identity.example" || created.Record.JWKSURL == "" {
		t.Fatalf("jwt integration=%+v", created)
	}

	validToken := signExternalJWT(t, privateKey, "rotation-1", now, map[string]any{
		"iss": "https://identity.example", "aud": []string{"https://gateway.example/v1"}, "sub": "not-used",
		"subject_ref": "opaque-user-1", "ai_models": []string{"model-a"}, "ai_qps": 4, "ai_monthly_tokens": 400,
	})
	auth, err := svc.AuthorizeGatewayCredential(ctx, validToken, "", "model-a")
	if err != nil {
		t.Fatalf("AuthorizeGatewayCredential() jwt error = %v", err)
	}
	if auth.ExternalAuthIntegration == nil || auth.ExternalAuthIntegration.ID != created.Record.ID || auth.ExternalSubjectReference != "opaque-user-1" || auth.APIKey.QPSLimit != 4 || auth.APIKey.MonthlyTokenLimit != 400 || len(auth.APIKey.ModelAllowlist) != 1 {
		t.Fatalf("jwt auth=%+v", auth)
	}
	if err := svc.RecordGatewayUsage(ctx, auth, GatewayUsageInput{Model: "model-a", Status: "forwarded", InputTokens: 2, OutputTokens: 3}); err != nil {
		t.Fatal(err)
	}
	usage, err := svc.UsageReportQuery(ctx, UsageQuery{ProfileScope: ProfileScopePlatform, ExternalAuthIntegrationID: created.Record.ID})
	if err != nil || len(usage.Recent) != 1 || usage.Recent[0].ExternalSubjectReference != "opaque-user-1" {
		t.Fatalf("jwt usage=%+v err=%v", usage, err)
	}

	tests := []struct {
		name   string
		key    *rsa.PrivateKey
		kid    string
		claims map[string]any
	}{
		{name: "unknown kid", key: privateKey, kid: "unknown", claims: map[string]any{"iss": "https://identity.example", "aud": "https://gateway.example/v1", "subject_ref": "opaque-user-1", "ai_models": []string{"model-a"}, "ai_qps": 1, "ai_monthly_tokens": 1}},
		{name: "wrong issuer", key: privateKey, kid: "rotation-1", claims: map[string]any{"iss": "https://other.example", "aud": "https://gateway.example/v1", "subject_ref": "opaque-user-1", "ai_models": []string{"model-a"}, "ai_qps": 1, "ai_monthly_tokens": 1}},
		{name: "claim expands models", key: privateKey, kid: "rotation-1", claims: map[string]any{"iss": "https://identity.example", "aud": "https://gateway.example/v1", "subject_ref": "opaque-user-1", "ai_models": []string{"model-a", "model-c"}, "ai_qps": 1, "ai_monthly_tokens": 1}},
		{name: "claim expands qps", key: privateKey, kid: "rotation-1", claims: map[string]any{"iss": "https://identity.example", "aud": "https://gateway.example/v1", "subject_ref": "opaque-user-1", "ai_models": []string{"model-a"}, "ai_qps": 11, "ai_monthly_tokens": 1}},
		{name: "claim expands quota", key: privateKey, kid: "rotation-1", claims: map[string]any{"iss": "https://identity.example", "aud": "https://gateway.example/v1", "subject_ref": "opaque-user-1", "ai_models": []string{"model-a"}, "ai_qps": 1, "ai_monthly_tokens": 1001}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			token := signExternalJWT(t, test.key, test.kid, now, test.claims)
			if _, authErr := svc.AuthenticateGatewayCredential(ctx, token, ""); !errors.Is(authErr, ErrGatewayUnauthorized) {
				t.Fatalf("AuthenticateGatewayCredential() error=%v, want unauthorized", authErr)
			}
		})
	}

	if _, err := svc.RotateExternalAuthIntegrationSecret(ctx, "operator", created.Record.ID); err == nil {
		t.Fatal("RotateExternalAuthIntegrationSecret() accepted jwt/jwks integration")
	}
	if _, err := svc.UpdateExternalAuthIntegration(ctx, "operator", created.Record.ID, ExternalAuthIntegrationRequest{
		TenantID: identity.tenant.ID, GatewayPrincipalID: identity.principal.ID, Name: "JWT product", Protocol: ExternalAuthIntegrationProtocolJWT, KeyID: "jwt-product-v1",
		Issuer: "https://other.example", JWKSURL: "https://identity.example/.well-known/jwks.json", SubjectClaim: "subject_ref", ModelsClaim: "ai_models", QPSLimitClaim: "ai_qps", MonthlyTokenClaim: "ai_monthly_tokens",
		Audience: "https://gateway.example/v1", ModelAllowlist: []string{"model-a", "model-b"}, QPSLimit: 10, MonthlyTokenLimit: 1000, MaxTTLSeconds: 300, Status: ExternalAuthIntegrationStatusActive,
	}); err == nil {
		t.Fatal("UpdateExternalAuthIntegration() accepted jwt issuer change")
	}
}

func signExternalJWT(t testing.TB, privateKey *rsa.PrivateKey, kid string, now time.Time, values map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: privateKey}, (&jose.SignerOptions{}).WithHeader("kid", kid))
	if err != nil {
		t.Fatal(err)
	}
	claims := jwt.Claims{Issuer: stringValue(values, "iss"), Subject: stringValue(values, "sub"), IssuedAt: jwt.NewNumericDate(now.Add(-time.Minute)), Expiry: jwt.NewNumericDate(now.Add(time.Minute))}
	switch audience := values["aud"].(type) {
	case string:
		claims.Audience = jwt.Audience{audience}
	case []string:
		claims.Audience = jwt.Audience(audience)
	}
	privateClaims := map[string]any{}
	for name, value := range values {
		if name != "iss" && name != "sub" && name != "aud" {
			privateClaims[name] = value
		}
	}
	token, err := jwt.Signed(signer).Claims(claims).Claims(privateClaims).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func stringValue(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

type externalAuthTestIdentity struct {
	tenant    PlatformTenant
	principal GatewayPrincipal
}

func createExternalAuthIdentity(t *testing.T, ctx context.Context, svc *Service) externalAuthTestIdentity {
	t.Helper()
	if err := svc.EnsurePlatformBootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	tenant, err := svc.CreatePlatformTenant(ctx, "operator", PlatformTenantRequest{Name: "External Product", Slug: "external-product"})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := svc.CreateGatewayPrincipal(ctx, "operator", GatewayPrincipalRequest{TenantID: tenant.ID, Name: "Product backend", PrincipalType: GatewayPrincipalTypeIntegration})
	if err != nil {
		t.Fatal(err)
	}
	return externalAuthTestIdentity{tenant: tenant, principal: principal}
}

func validExternalAuthClaims(integration ExternalAuthIntegration, subject string, now time.Time) ExternalAuthContextClaims {
	return ExternalAuthContextClaims{
		Version: externalAuthContextVersion, IntegrationID: integration.ID, KeyID: integration.KeyID,
		TenantID: integration.TenantID, SubjectReference: subject, Audience: integration.Audience,
		IssuedAt: now.Unix(), ExpiresAt: now.Add(2 * time.Minute).Unix(),
		ModelAllowlist: []string{"model-a"}, QPSLimit: 1, MonthlyTokenLimit: 1,
	}
}

func signExternalAuthContext(t testing.TB, claims ExternalAuthContextClaims, secret string) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
