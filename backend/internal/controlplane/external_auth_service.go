package controlplane

import (
	"context"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

const (
	externalAuthContextVersion        = 1
	externalAuthContextMaxBytes       = 16 << 10
	externalAuthSubjectMaxCharacters  = 256
	externalAuthAudienceMaxCharacters = 512
	externalAuthDefaultTTLSeconds     = 300
	externalAuthMinimumTTLSeconds     = 30
	externalAuthMaximumTTLSeconds     = 3600
	externalAuthClockSkew             = 30 * time.Second
	externalAuthJWKSCacheTTL          = 5 * time.Minute
	externalAuthJWKSResponseMaxBytes  = 1 << 20
)

var (
	externalAuthKeyIDPattern     = regexp.MustCompile(`^[A-Za-z0-9_-]{1,120}$`)
	externalAuthClaimNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,119}$`)
)

// ExternalAuthContextClaims is the bounded, short-lived access decision a
// connected product signs for a single end user. It deliberately excludes
// login tokens, refresh tokens, profile data, subscriptions, and provider
// credentials.
type ExternalAuthContextClaims struct {
	Version           int      `json:"v"`
	IntegrationID     string   `json:"integration_id"`
	KeyID             string   `json:"key_id"`
	TenantID          string   `json:"tenant_id"`
	SubjectReference  string   `json:"subject_ref"`
	Audience          string   `json:"aud"`
	IssuedAt          int64    `json:"iat"`
	ExpiresAt         int64    `json:"exp"`
	ModelAllowlist    []string `json:"models"`
	QPSLimit          int      `json:"qps_limit"`
	MonthlyTokenLimit int      `json:"monthly_token_limit"`
}

type externalAuthJWKSFetcher func(ctx context.Context, rawURL string) (jose.JSONWebKeySet, error)

type externalAuthJWKSCacheEntry struct {
	set       jose.JSONWebKeySet
	expiresAt time.Time
}

func (s *Service) ListExternalAuthIntegrations(ctx context.Context) ([]ExternalAuthIntegration, error) {
	integrations, err := s.repo.ListExternalAuthIntegrations(ctx)
	if err != nil {
		return nil, err
	}
	for index := range integrations {
		integrations[index] = externalAuthIntegrationPublic(integrations[index])
	}
	return integrations, nil
}

func (s *Service) CreateExternalAuthIntegration(ctx context.Context, actor string, req ExternalAuthIntegrationRequest) (ExternalAuthIntegrationCreateResponse, error) {
	integration, secret, identity, err := s.externalAuthIntegrationFromRequest(ctx, req, nil, true)
	if err != nil {
		return ExternalAuthIntegrationCreateResponse{}, err
	}
	integration.ID = "eai_" + randomID(10)
	integration.CreatedAt = s.nowUTC()
	integration.UpdatedAt = integration.CreatedAt
	if err := s.requireExternalAuthIntegrationUnique(ctx, integration, ""); err != nil {
		return ExternalAuthIntegrationCreateResponse{}, err
	}
	if err := s.repo.SaveExternalAuthIntegration(ctx, integration); err != nil {
		return ExternalAuthIntegrationCreateResponse{}, err
	}
	if err := s.auditPlatform(ctx, actor, "create", "external_auth_integration", integration.ID, fmt.Sprintf("Created external auth integration %s", integration.Name), &identity.tenant, &identity.principal); err != nil {
		return ExternalAuthIntegrationCreateResponse{}, err
	}
	return ExternalAuthIntegrationCreateResponse{Record: externalAuthIntegrationPublic(integration), Secret: secret}, nil
}

func (s *Service) UpdateExternalAuthIntegration(ctx context.Context, actor, id string, req ExternalAuthIntegrationRequest) (ExternalAuthIntegration, error) {
	existing, err := s.externalAuthIntegrationByID(ctx, id)
	if err != nil {
		return ExternalAuthIntegration{}, err
	}
	if strings.TrimSpace(req.Protocol) == "" {
		req.Protocol = existing.Protocol
	}
	if strings.TrimSpace(req.TenantID) == "" {
		req.TenantID = existing.TenantID
	}
	if strings.TrimSpace(req.GatewayPrincipalID) == "" {
		req.GatewayPrincipalID = existing.GatewayPrincipalID
	}
	if req.TenantID != existing.TenantID || req.GatewayPrincipalID != existing.GatewayPrincipalID {
		return ExternalAuthIntegration{}, errors.New("external auth integration tenant_id and gateway_principal_id are immutable")
	}
	if strings.TrimSpace(req.KeyID) != existing.KeyID {
		return ExternalAuthIntegration{}, errors.New("external auth integration key_id is immutable")
	}
	if strings.TrimSpace(req.Protocol) != existing.Protocol {
		return ExternalAuthIntegration{}, errors.New("external auth integration protocol is immutable")
	}
	if existing.Protocol == ExternalAuthIntegrationProtocolJWT && (strings.TrimSpace(req.Issuer) != existing.Issuer || strings.TrimSpace(req.JWKSURL) != existing.JWKSURL || strings.TrimSpace(req.Audience) != existing.Audience || normalizedExternalAuthClaim(req.SubjectClaim, "sub") != existing.SubjectClaim || strings.TrimSpace(req.ModelsClaim) != existing.ModelsClaim || strings.TrimSpace(req.QPSLimitClaim) != existing.QPSLimitClaim || strings.TrimSpace(req.MonthlyTokenClaim) != existing.MonthlyTokenClaim) {
		return ExternalAuthIntegration{}, errors.New("jwt/jwks issuer, audience, jwks_url, and claim mappings are immutable; create a new integration for a different trust boundary")
	}
	if strings.TrimSpace(req.Secret) != "" {
		return ExternalAuthIntegration{}, errors.New("rotate the external auth integration secret through its dedicated endpoint")
	}
	integration, _, identity, err := s.externalAuthIntegrationFromRequest(ctx, req, &existing, false)
	if err != nil {
		return ExternalAuthIntegration{}, err
	}
	integration.ID = existing.ID
	integration.CreatedAt = existing.CreatedAt
	integration.UpdatedAt = s.nowUTC()
	if err := s.requireExternalAuthIntegrationUnique(ctx, integration, existing.ID); err != nil {
		return ExternalAuthIntegration{}, err
	}
	if err := s.repo.SaveExternalAuthIntegration(ctx, integration); err != nil {
		return ExternalAuthIntegration{}, err
	}
	if err := s.auditPlatform(ctx, actor, "update", "external_auth_integration", integration.ID, fmt.Sprintf("Updated external auth integration %s", integration.Name), &identity.tenant, &identity.principal); err != nil {
		return ExternalAuthIntegration{}, err
	}
	return externalAuthIntegrationPublic(integration), nil
}

func (s *Service) RotateExternalAuthIntegrationSecret(ctx context.Context, actor, id string) (ExternalAuthIntegrationCreateResponse, error) {
	integration, err := s.externalAuthIntegrationByID(ctx, id)
	if err != nil {
		return ExternalAuthIntegrationCreateResponse{}, err
	}
	if integration.Protocol != ExternalAuthIntegrationProtocolHMAC {
		return ExternalAuthIntegrationCreateResponse{}, errors.New("jwt/jwks integrations do not have a shared secret to rotate")
	}
	identity, err := s.platformCredentialIdentity(ctx, integration.TenantID, integration.GatewayPrincipalID)
	if err != nil {
		return ExternalAuthIntegrationCreateResponse{}, err
	}
	secret := "asc_" + randomToken(32)
	ciphertext, err := encryptSecret(s.secretKey, secret)
	if err != nil {
		return ExternalAuthIntegrationCreateResponse{}, err
	}
	integration.SecretConfigured = true
	integration.SecretHint = maskSecret(secret)
	integration.SecretCiphertext = ciphertext
	integration.UpdatedAt = s.nowUTC()
	if err := s.repo.SaveExternalAuthIntegration(ctx, integration); err != nil {
		return ExternalAuthIntegrationCreateResponse{}, err
	}
	if err := s.auditPlatform(ctx, actor, "rotate_secret", "external_auth_integration", integration.ID, "Rotated external auth integration secret", &identity.tenant, &identity.principal); err != nil {
		return ExternalAuthIntegrationCreateResponse{}, err
	}
	return ExternalAuthIntegrationCreateResponse{Record: externalAuthIntegrationPublic(integration), Secret: secret}, nil
}

func (s *Service) externalAuthIntegrationFromRequest(ctx context.Context, req ExternalAuthIntegrationRequest, existing *ExternalAuthIntegration, create bool) (ExternalAuthIntegration, string, platformCredentialIdentity, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > 120 {
		return ExternalAuthIntegration{}, "", platformCredentialIdentity{}, errors.New("external auth integration name must contain 1 to 120 characters")
	}
	protocol := strings.TrimSpace(req.Protocol)
	if protocol == "" {
		protocol = ExternalAuthIntegrationProtocolHMAC
	}
	if protocol != ExternalAuthIntegrationProtocolHMAC && protocol != ExternalAuthIntegrationProtocolJWT {
		return ExternalAuthIntegration{}, "", platformCredentialIdentity{}, errors.New("external auth integration protocol must be hmac_signed_context or jwt_jwks")
	}
	keyID := strings.TrimSpace(req.KeyID)
	if !externalAuthKeyIDPattern.MatchString(keyID) {
		return ExternalAuthIntegration{}, "", platformCredentialIdentity{}, errors.New("external auth integration key_id must use 1 to 120 letters, digits, underscores, or hyphens")
	}
	audience := strings.TrimSpace(req.Audience)
	if audience == "" || len([]rune(audience)) > externalAuthAudienceMaxCharacters {
		return ExternalAuthIntegration{}, "", platformCredentialIdentity{}, errors.New("external auth integration audience must contain 1 to 512 characters")
	}
	issuer, jwksURL, subjectClaim, modelsClaim, qpsLimitClaim, monthlyTokenClaim, err := externalAuthJWTConfiguration(req, protocol)
	if err != nil {
		return ExternalAuthIntegration{}, "", platformCredentialIdentity{}, err
	}
	models := cleanStringList(req.ModelAllowlist)
	if len(models) == 0 {
		return ExternalAuthIntegration{}, "", platformCredentialIdentity{}, errors.New("external auth integration model_allowlist must not be empty")
	}
	if req.QPSLimit <= 0 || req.MonthlyTokenLimit <= 0 {
		return ExternalAuthIntegration{}, "", platformCredentialIdentity{}, errors.New("external auth integration qps_limit and monthly_token_limit must be greater than zero")
	}
	maxTTL := req.MaxTTLSeconds
	if maxTTL == 0 {
		maxTTL = externalAuthDefaultTTLSeconds
	}
	if maxTTL < externalAuthMinimumTTLSeconds || maxTTL > externalAuthMaximumTTLSeconds {
		return ExternalAuthIntegration{}, "", platformCredentialIdentity{}, fmt.Errorf("external auth integration max_ttl_seconds must be between %d and %d", externalAuthMinimumTTLSeconds, externalAuthMaximumTTLSeconds)
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = ExternalAuthIntegrationStatusActive
	}
	if !oneOf(status, ExternalAuthIntegrationStatusActive, ExternalAuthIntegrationStatusDisabled) {
		return ExternalAuthIntegration{}, "", platformCredentialIdentity{}, errors.New("external auth integration status must be active or disabled")
	}
	identity, err := s.platformCredentialIdentity(ctx, req.TenantID, req.GatewayPrincipalID)
	if err != nil {
		return ExternalAuthIntegration{}, "", platformCredentialIdentity{}, err
	}
	if identity.principal.PrincipalType != GatewayPrincipalTypeService && identity.principal.PrincipalType != GatewayPrincipalTypeIntegration {
		return ExternalAuthIntegration{}, "", platformCredentialIdentity{}, errors.New("external auth integration gateway principal must be a service or integration")
	}
	if identity.tenant.Status != PlatformTenantStatusActive || identity.principal.Status != GatewayPrincipalStatusActive {
		return ExternalAuthIntegration{}, "", platformCredentialIdentity{}, errors.New("external auth integration tenant and gateway principal must be active")
	}
	policyID := strings.TrimSpace(req.PolicyID)
	if policyID != "" {
		policy, err := s.governancePolicyByID(ctx, policyID)
		if err != nil {
			return ExternalAuthIntegration{}, "", platformCredentialIdentity{}, err
		}
		if policy.Status != GovernancePolicyStatusActive {
			return ExternalAuthIntegration{}, "", platformCredentialIdentity{}, errors.New("external auth integration policy must be active")
		}
	}
	integration := ExternalAuthIntegration{
		TenantID: req.TenantID, GatewayPrincipalID: req.GatewayPrincipalID, Name: name,
		Protocol: protocol, KeyID: keyID, Audience: audience, PolicyID: policyID,
		Issuer: issuer, JWKSURL: jwksURL, SubjectClaim: subjectClaim, ModelsClaim: modelsClaim,
		QPSLimitClaim: qpsLimitClaim, MonthlyTokenClaim: monthlyTokenClaim,
		ModelAllowlist: models, QPSLimit: req.QPSLimit, MonthlyTokenLimit: req.MonthlyTokenLimit,
		MaxTTLSeconds: maxTTL, Status: status,
	}
	secret := ""
	if existing != nil {
		integration.SecretConfigured = existing.SecretConfigured
		integration.SecretHint = existing.SecretHint
		integration.SecretCiphertext = existing.SecretCiphertext
	} else if create && protocol == ExternalAuthIntegrationProtocolHMAC {
		secret = strings.TrimSpace(req.Secret)
		if secret == "" {
			secret = "asc_" + randomToken(32)
		}
		if len(secret) < 24 || len(secret) > 4096 {
			return ExternalAuthIntegration{}, "", platformCredentialIdentity{}, errors.New("external auth integration secret must contain 24 to 4096 characters")
		}
		ciphertext, err := encryptSecret(s.secretKey, secret)
		if err != nil {
			return ExternalAuthIntegration{}, "", platformCredentialIdentity{}, err
		}
		integration.SecretConfigured = true
		integration.SecretHint = maskSecret(secret)
		integration.SecretCiphertext = ciphertext
	}
	if integration.Status == ExternalAuthIntegrationStatusActive && integration.Protocol == ExternalAuthIntegrationProtocolHMAC && !integration.SecretConfigured {
		return ExternalAuthIntegration{}, "", platformCredentialIdentity{}, errors.New("active external auth integration requires a secret")
	}
	return integration, secret, identity, nil
}

func externalAuthJWTConfiguration(req ExternalAuthIntegrationRequest, protocol string) (issuer, jwksURL, subjectClaim, modelsClaim, qpsLimitClaim, monthlyTokenClaim string, err error) {
	issuer = strings.TrimSpace(req.Issuer)
	jwksURL = strings.TrimSpace(req.JWKSURL)
	subjectClaim = normalizedExternalAuthClaim(req.SubjectClaim, "sub")
	modelsClaim = strings.TrimSpace(req.ModelsClaim)
	qpsLimitClaim = strings.TrimSpace(req.QPSLimitClaim)
	monthlyTokenClaim = strings.TrimSpace(req.MonthlyTokenClaim)
	if protocol == ExternalAuthIntegrationProtocolHMAC {
		if issuer != "" || jwksURL != "" || strings.TrimSpace(req.SubjectClaim) != "" || modelsClaim != "" || qpsLimitClaim != "" || monthlyTokenClaim != "" {
			return "", "", "", "", "", "", errors.New("hmac_signed_context integrations cannot configure issuer, jwks_url, or jwt claim mappings")
		}
		return "", "", "", "", "", "", nil
	}
	if issuer == "" || len([]rune(issuer)) > 512 {
		return "", "", "", "", "", "", errors.New("jwt/jwks integration issuer must contain 1 to 512 characters")
	}
	issuerURL, parseErr := url.Parse(issuer)
	if parseErr != nil || issuerURL.Scheme != "https" || issuerURL.Host == "" || issuerURL.User != nil || issuerURL.Fragment != "" {
		return "", "", "", "", "", "", errors.New("jwt/jwks integration issuer must be an absolute https URL without credentials or fragment")
	}
	if err := validateExternalAuthJWKSURL(jwksURL); err != nil {
		return "", "", "", "", "", "", err
	}
	for _, claim := range []string{subjectClaim, modelsClaim, qpsLimitClaim, monthlyTokenClaim} {
		if claim != "" && !externalAuthClaimNamePattern.MatchString(claim) {
			return "", "", "", "", "", "", errors.New("jwt/jwks claim mappings must use 1 to 120 letters, digits, dots, underscores, or hyphens and start with a letter")
		}
	}
	return issuer, jwksURL, subjectClaim, modelsClaim, qpsLimitClaim, monthlyTokenClaim, nil
}

func normalizedExternalAuthClaim(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func validateExternalAuthJWKSURL(value string) error {
	if len([]rune(value)) == 0 || len([]rune(value)) > 2048 {
		return errors.New("jwt/jwks integration jwks_url must contain 1 to 2048 characters")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return errors.New("jwt/jwks integration jwks_url must be an absolute https URL without credentials or fragment")
	}
	return nil
}

func (s *Service) requireExternalAuthIntegrationUnique(ctx context.Context, integration ExternalAuthIntegration, exceptID string) error {
	integrations, err := s.repo.ListExternalAuthIntegrations(ctx)
	if err != nil {
		return err
	}
	for _, item := range integrations {
		if item.ID == exceptID {
			continue
		}
		if item.TenantID == integration.TenantID && strings.EqualFold(item.Name, integration.Name) {
			return errors.New("external auth integration name already exists for platform tenant")
		}
		if item.KeyID == integration.KeyID {
			return errors.New("external auth integration key_id already exists")
		}
		if integration.Protocol == ExternalAuthIntegrationProtocolJWT && integration.Status == ExternalAuthIntegrationStatusActive && item.Protocol == ExternalAuthIntegrationProtocolJWT && item.Status == ExternalAuthIntegrationStatusActive && item.Issuer == integration.Issuer && item.Audience == integration.Audience {
			return errors.New("only one active jwt/jwks integration may use the same issuer and audience")
		}
	}
	return nil
}

func externalAuthIntegrationPublic(integration ExternalAuthIntegration) ExternalAuthIntegration {
	integration.SecretCiphertext = ""
	return integration
}

func (s *Service) externalAuthIntegrationByID(ctx context.Context, id string) (ExternalAuthIntegration, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return ExternalAuthIntegration{}, errors.New("external auth integration id is required")
	}
	integrations, err := s.repo.ListExternalAuthIntegrations(ctx)
	if err != nil {
		return ExternalAuthIntegration{}, err
	}
	for _, integration := range integrations {
		if integration.ID == id {
			return integration, nil
		}
	}
	return ExternalAuthIntegration{}, errors.New("external auth integration not found")
}

// AuthenticateExternalAuthContext verifies the HMAC signed context supplied
// by an external product and returns a synthetic request principal. The
// synthetic APIKeyRecord is never persisted and cannot be used on a control
// plane endpoint.
func (s *Service) AuthenticateExternalAuthContext(ctx context.Context, token string) (GatewayAuthContext, error) {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > externalAuthContextMaxBytes {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(payload) == 0 || len(payload) > externalAuthContextMaxBytes {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(signature) != sha256.Size {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	var claims ExternalAuthContextClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	if claims.Version != externalAuthContextVersion {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	integration, err := s.externalAuthIntegrationByID(ctx, claims.IntegrationID)
	if err != nil || integration.Status != ExternalAuthIntegrationStatusActive || integration.Protocol != ExternalAuthIntegrationProtocolHMAC || !integration.SecretConfigured || integration.SecretCiphertext == "" {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	if claims.KeyID != integration.KeyID || claims.TenantID != integration.TenantID || claims.Audience != integration.Audience {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	secret, err := decryptSecret(s.secretKey, integration.SecretCiphertext)
	if err != nil || secret == "" {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	if err := validateExternalAuthContextClaims(claims, integration, s.nowUTC()); err != nil {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	identity, err := s.activePlatformCredentialIdentity(ctx, integration.TenantID, integration.GatewayPrincipalID)
	if err != nil || (identity.principal.PrincipalType != GatewayPrincipalTypeService && identity.principal.PrincipalType != GatewayPrincipalTypeIntegration) {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	policy, source, err := s.externalAuthGatewayPolicy(ctx, integration)
	if err != nil {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	subjectHash := hashAPIKey(integration.ID + "\x00" + claims.SubjectReference)
	key := APIKeyRecord{
		ID: "eai_subject_" + prefix(subjectHash, 32), Name: "External delegated subject",
		Fingerprint: prefix(subjectHash, 12), Prefix: "ctx_", Status: APIKeyStatusActive,
		KeyType: APIKeyTypeService, ProfileScope: ProfileScopePlatform,
		PlatformTenantID: integration.TenantID, GatewayPrincipalID: integration.GatewayPrincipalID,
		PolicyID: integration.PolicyID, ModelAllowlist: cleanStringList(claims.ModelAllowlist),
		QPSLimit: claims.QPSLimit, MonthlyTokenLimit: claims.MonthlyTokenLimit,
	}
	return GatewayAuthContext{
		APIKey: key, Policy: policy, PolicySource: source, PlatformTenant: &identity.tenant,
		GatewayPrincipal: &identity.principal, ExternalAuthIntegration: &integration,
		ExternalSubjectReference: claims.SubjectReference,
	}, nil
}

// AuthenticateExternalJWT accepts a signed JWT from a connected product.
// It selects a configured Platform integration using verified issuer and
// audience claims, then resolves only bounded AI access facts. It neither
// stores the JWT nor creates an AsterRouter user or session for its subject.
func (s *Service) AuthenticateExternalJWT(ctx context.Context, token string) (GatewayAuthContext, error) {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > externalAuthContextMaxBytes || strings.Count(token, ".") != 2 {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	unsigned, err := jwt.ParseSigned(token, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil || len(unsigned.Headers) != 1 || strings.TrimSpace(unsigned.Headers[0].KeyID) == "" {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	var unverified jwt.Claims
	if err := unsigned.UnsafeClaimsWithoutVerification(&unverified); err != nil || strings.TrimSpace(unverified.Issuer) == "" || len(unverified.Audience) == 0 {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	integration, err := s.externalAuthJWTIntegrationByIssuerAudience(ctx, unverified.Issuer, []string(unverified.Audience))
	if err != nil {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	key, err := s.externalAuthJWKSKey(ctx, integration.JWKSURL, unsigned.Headers[0].KeyID)
	if err != nil {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	var verified jwt.Claims
	var verifiedValues map[string]json.RawMessage
	if err := unsigned.Claims(key.Key, &verified, &verifiedValues); err != nil {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	if err := verified.ValidateWithLeeway(jwt.Expected{Issuer: integration.Issuer, AnyAudience: jwt.Audience{integration.Audience}, Time: s.nowUTC()}, externalAuthClockSkew); err != nil || verified.Expiry == nil || verified.IssuedAt == nil || !verified.Expiry.Time().After(verified.IssuedAt.Time()) || verified.Expiry.Time().Sub(verified.IssuedAt.Time()) > time.Duration(integration.MaxTTLSeconds)*time.Second {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	subject, models, qpsLimit, monthlyTokenLimit, err := externalJWTAccessFacts(verifiedValues, integration)
	if err != nil {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	identity, err := s.activePlatformCredentialIdentity(ctx, integration.TenantID, integration.GatewayPrincipalID)
	if err != nil || (identity.principal.PrincipalType != GatewayPrincipalTypeService && identity.principal.PrincipalType != GatewayPrincipalTypeIntegration) {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	policy, source, err := s.externalAuthGatewayPolicy(ctx, integration)
	if err != nil {
		return GatewayAuthContext{}, ErrGatewayUnauthorized
	}
	subjectHash := hashAPIKey(integration.ID + "\x00" + subject)
	keyRecord := APIKeyRecord{
		ID: "eai_subject_" + prefix(subjectHash, 32), Name: "External delegated subject",
		Fingerprint: prefix(subjectHash, 12), Prefix: "jwt_", Status: APIKeyStatusActive,
		KeyType: APIKeyTypeService, ProfileScope: ProfileScopePlatform,
		PlatformTenantID: integration.TenantID, GatewayPrincipalID: integration.GatewayPrincipalID,
		PolicyID: integration.PolicyID, ModelAllowlist: models, QPSLimit: qpsLimit, MonthlyTokenLimit: monthlyTokenLimit,
	}
	return GatewayAuthContext{
		APIKey: keyRecord, Policy: policy, PolicySource: source, PlatformTenant: &identity.tenant,
		GatewayPrincipal: &identity.principal, ExternalAuthIntegration: &integration,
		ExternalSubjectReference: subject,
	}, nil
}

func (s *Service) externalAuthJWTIntegrationByIssuerAudience(ctx context.Context, issuer string, audiences []string) (ExternalAuthIntegration, error) {
	integrations, err := s.repo.ListExternalAuthIntegrations(ctx)
	if err != nil {
		return ExternalAuthIntegration{}, err
	}
	var found *ExternalAuthIntegration
	for index := range integrations {
		candidate := integrations[index]
		if candidate.Protocol != ExternalAuthIntegrationProtocolJWT || candidate.Status != ExternalAuthIntegrationStatusActive || candidate.Issuer != issuer || !contains(audiences, candidate.Audience) {
			continue
		}
		if found != nil {
			return ExternalAuthIntegration{}, errors.New("multiple active jwt/jwks integrations match token issuer and audience")
		}
		copy := candidate
		found = &copy
	}
	if found == nil {
		return ExternalAuthIntegration{}, errors.New("no active jwt/jwks integration matches token issuer and audience")
	}
	return *found, nil
}

func externalJWTAccessFacts(values map[string]json.RawMessage, integration ExternalAuthIntegration) (string, []string, int, int, error) {
	subject, err := externalJWTStringClaim(values, integration.SubjectClaim)
	if err != nil || strings.TrimSpace(subject) == "" || len([]rune(subject)) > externalAuthSubjectMaxCharacters {
		return "", nil, 0, 0, errors.New("jwt subject claim is invalid")
	}
	models := integration.ModelAllowlist
	if integration.ModelsClaim != "" {
		models, err = externalJWTStringListClaim(values, integration.ModelsClaim)
		if err != nil || len(models) == 0 || !stringSetSubset(models, integration.ModelAllowlist) {
			return "", nil, 0, 0, errors.New("jwt model claim exceeds integration ceiling")
		}
	}
	qpsLimit := integration.QPSLimit
	if integration.QPSLimitClaim != "" {
		qpsLimit, err = externalJWTPositiveIntClaim(values, integration.QPSLimitClaim)
		if err != nil || qpsLimit > integration.QPSLimit {
			return "", nil, 0, 0, errors.New("jwt qps claim exceeds integration ceiling")
		}
	}
	monthlyTokenLimit := integration.MonthlyTokenLimit
	if integration.MonthlyTokenClaim != "" {
		monthlyTokenLimit, err = externalJWTPositiveIntClaim(values, integration.MonthlyTokenClaim)
		if err != nil || monthlyTokenLimit > integration.MonthlyTokenLimit {
			return "", nil, 0, 0, errors.New("jwt monthly token claim exceeds integration ceiling")
		}
	}
	return subject, models, qpsLimit, monthlyTokenLimit, nil
}

func externalJWTStringClaim(values map[string]json.RawMessage, name string) (string, error) {
	raw, ok := values[name]
	if !ok || len(raw) == 0 {
		return "", errors.New("jwt claim is missing")
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func externalJWTStringListClaim(values map[string]json.RawMessage, name string) ([]string, error) {
	raw, ok := values[name]
	if !ok || len(raw) == 0 {
		return nil, errors.New("jwt claim is missing")
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil {
		var single string
		if singleErr := json.Unmarshal(raw, &single); singleErr != nil {
			return nil, err
		}
		list = strings.FieldsFunc(single, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' })
	}
	return cleanStringList(list), nil
}

func externalJWTPositiveIntClaim(values map[string]json.RawMessage, name string) (int, error) {
	raw, ok := values[name]
	if !ok || len(raw) == 0 {
		return 0, errors.New("jwt claim is missing")
	}
	var value int
	if err := json.Unmarshal(raw, &value); err != nil || value <= 0 {
		return 0, errors.New("jwt numeric claim must be a positive integer")
	}
	return value, nil
}

func (s *Service) externalAuthJWKSKey(ctx context.Context, rawURL, kid string) (jose.JSONWebKey, error) {
	set, err := s.externalAuthJWKS(ctx, rawURL, false)
	if err != nil {
		return jose.JSONWebKey{}, err
	}
	keys := set.Key(kid)
	if len(keys) == 0 {
		set, err = s.externalAuthJWKS(ctx, rawURL, true)
		if err != nil {
			return jose.JSONWebKey{}, err
		}
		keys = set.Key(kid)
	}
	if len(keys) != 1 || !keys[0].Valid() || keys[0].Algorithm != "" && keys[0].Algorithm != string(jose.RS256) {
		return jose.JSONWebKey{}, errors.New("jwt jwks key is missing, ambiguous, or invalid")
	}
	if _, ok := keys[0].Key.(*rsa.PublicKey); !ok {
		return jose.JSONWebKey{}, errors.New("jwt jwks key must be RSA")
	}
	return keys[0], nil
}

func (s *Service) externalAuthJWKS(ctx context.Context, rawURL string, forceRefresh bool) (jose.JSONWebKeySet, error) {
	now := s.nowUTC()
	s.jwksMu.Lock()
	entry, found := s.externalAuthJWKSCache[rawURL]
	s.jwksMu.Unlock()
	if found && !forceRefresh && now.Before(entry.expiresAt) {
		return entry.set, nil
	}
	fetcher := s.externalAuthJWKSFetcher
	if fetcher == nil {
		fetcher = fetchExternalAuthJWKS
	}
	set, err := fetcher(ctx, rawURL)
	if err != nil {
		return jose.JSONWebKeySet{}, err
	}
	s.jwksMu.Lock()
	s.externalAuthJWKSCache[rawURL] = externalAuthJWKSCacheEntry{set: set, expiresAt: now.Add(externalAuthJWKSCacheTTL)}
	s.jwksMu.Unlock()
	return set, nil
}

func fetchExternalAuthJWKS(ctx context.Context, rawURL string) (jose.JSONWebKeySet, error) {
	if err := validateExternalAuthJWKSURL(rawURL); err != nil {
		return jose.JSONWebKeySet{}, err
	}
	parsed, _ := url.Parse(rawURL)
	host := parsed.Hostname()
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()) {
		return jose.JSONWebKeySet{}, errors.New("jwt/jwks URL must not target a private address")
	}
	transport := &http.Transport{DialContext: externalAuthSafeDialContext, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second}
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || req.URL.Scheme != "https" || req.URL.User != nil {
			return errors.New("invalid jwt/jwks redirect")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return jose.JSONWebKeySet{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return jose.JSONWebKeySet{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return jose.JSONWebKeySet{}, errors.New("jwt/jwks endpoint did not return HTTP 200")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, externalAuthJWKSResponseMaxBytes+1))
	if err != nil || len(body) == 0 || len(body) > externalAuthJWKSResponseMaxBytes {
		return jose.JSONWebKeySet{}, errors.New("jwt/jwks response is invalid or too large")
	}
	var set jose.JSONWebKeySet
	if err := json.Unmarshal(body, &set); err != nil || len(set.Keys) == 0 {
		return jose.JSONWebKeySet{}, errors.New("jwt/jwks response does not contain keys")
	}
	return set, nil
}

func externalAuthSafeDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 {
		return nil, errors.New("unable to resolve jwt/jwks host")
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	var lastErr error
	for _, address := range addresses {
		if !externalAuthPublicIP(address.IP) {
			return nil, errors.New("jwt/jwks host resolves to a private address")
		}
		connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
		if dialErr == nil {
			return connection, nil
		}
		lastErr = dialErr
	}
	if lastErr == nil {
		lastErr = errors.New("no public jwt/jwks address is available")
	}
	return nil, lastErr
}

func externalAuthPublicIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return false
	}
	if ipv4 := ip.To4(); ipv4 != nil && ipv4[0] == 100 && ipv4[1] >= 64 && ipv4[1] <= 127 {
		return false
	}
	return true
}

func validateExternalAuthContextClaims(claims ExternalAuthContextClaims, integration ExternalAuthIntegration, now time.Time) error {
	if strings.TrimSpace(claims.SubjectReference) == "" || len([]rune(claims.SubjectReference)) > externalAuthSubjectMaxCharacters {
		return errors.New("external subject reference is invalid")
	}
	if claims.IssuedAt <= 0 || claims.ExpiresAt <= 0 {
		return errors.New("external auth context lifetime is invalid")
	}
	issuedAt := time.Unix(claims.IssuedAt, 0).UTC()
	expiresAt := time.Unix(claims.ExpiresAt, 0).UTC()
	if issuedAt.After(now.Add(externalAuthClockSkew)) || !expiresAt.After(now) || !expiresAt.After(issuedAt) {
		return errors.New("external auth context has expired or is not yet valid")
	}
	if expiresAt.Sub(issuedAt) > time.Duration(integration.MaxTTLSeconds)*time.Second {
		return errors.New("external auth context exceeds the integration ttl ceiling")
	}
	models := cleanStringList(claims.ModelAllowlist)
	if len(models) == 0 || !stringSetSubset(models, integration.ModelAllowlist) {
		return errors.New("external auth context models exceed the integration ceiling")
	}
	if claims.QPSLimit <= 0 || claims.QPSLimit > integration.QPSLimit {
		return errors.New("external auth context qps limit exceeds the integration ceiling")
	}
	if claims.MonthlyTokenLimit <= 0 || claims.MonthlyTokenLimit > integration.MonthlyTokenLimit {
		return errors.New("external auth context monthly token limit exceeds the integration ceiling")
	}
	return nil
}

func stringSetSubset(values, ceiling []string) bool {
	for _, value := range values {
		if !contains(ceiling, value) {
			return false
		}
	}
	return true
}

func (s *Service) externalAuthGatewayPolicy(ctx context.Context, integration ExternalAuthIntegration) (*GovernancePolicy, string, error) {
	if strings.TrimSpace(integration.PolicyID) != "" {
		policy, err := s.governancePolicyByID(ctx, integration.PolicyID)
		if err != nil {
			return nil, "", err
		}
		if policy.Status != GovernancePolicyStatusActive {
			return nil, "", errors.New("external auth integration policy is not active")
		}
		return &policy, GatewayPolicySourceExternalAuthIntegration, nil
	}
	policies, err := s.repo.ListGovernancePolicies(ctx)
	if err != nil {
		return nil, "", err
	}
	if policy, ok := activePolicyByScope(policies, GovernancePolicyScopeGlobal, ""); ok {
		return &policy, GatewayPolicySourceGlobalScope, nil
	}
	return nil, "", nil
}
