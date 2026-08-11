package controlplane

import (
	"context"
	"sort"
)

func (r *MemoryRepository) ListApplications(context.Context) ([]Application, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Application, 0, len(r.applications))
	for _, application := range r.applications {
		out = append(out, application)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].Name < out[j].Name
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (r *MemoryRepository) SaveApplication(_ context.Context, application Application) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.applications[application.ID] = application
	return nil
}

func (r *MemoryRepository) ListGatewayPrincipals(context.Context) ([]GatewayPrincipal, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]GatewayPrincipal, 0, len(r.gatewayPrincipals))
	for _, principal := range r.gatewayPrincipals {
		out = append(out, principal)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].Name < out[j].Name
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (r *MemoryRepository) SaveGatewayPrincipal(_ context.Context, principal GatewayPrincipal) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gatewayPrincipals[principal.ID] = principal
	return nil
}

func (r *MemoryRepository) ListExternalAuthIntegrations(context.Context) ([]ExternalAuthIntegration, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ExternalAuthIntegration, 0, len(r.externalAuthIntegrations))
	for _, integration := range r.externalAuthIntegrations {
		out = append(out, integration)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].Name < out[j].Name
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (r *MemoryRepository) SaveExternalAuthIntegration(_ context.Context, integration ExternalAuthIntegration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.externalAuthIntegrations[integration.ID] = integration
	return nil
}

func (r *PostgresRepository) ListApplications(ctx context.Context) ([]Application, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, name, slug, entitlement_reference, concurrency_limit, status, created_at, updated_at
FROM applications
ORDER BY created_at ASC, name ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Application{}
	for rows.Next() {
		var application Application
		if err := rows.Scan(&application.ID, &application.Name, &application.Slug, &application.EntitlementReference, &application.ConcurrencyLimit, &application.Status, &application.CreatedAt, &application.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, application)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveApplication(ctx context.Context, application Application) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO applications(id, name, slug, entitlement_reference, concurrency_limit, status, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT(id) DO UPDATE SET
  name = EXCLUDED.name,
  slug = EXCLUDED.slug,
  entitlement_reference = EXCLUDED.entitlement_reference,
  concurrency_limit = EXCLUDED.concurrency_limit,
  status = EXCLUDED.status,
  updated_at = EXCLUDED.updated_at
`, application.ID, application.Name, application.Slug, application.EntitlementReference, application.ConcurrencyLimit, application.Status, application.CreatedAt, application.UpdatedAt)
	return err
}

func (r *PostgresRepository) ListGatewayPrincipals(ctx context.Context) ([]GatewayPrincipal, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, application_id, name, principal_type, external_subject_reference, status, created_at, updated_at
FROM gateway_principals
ORDER BY created_at ASC, name ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GatewayPrincipal{}
	for rows.Next() {
		var principal GatewayPrincipal
		if err := rows.Scan(&principal.ID, &principal.ApplicationID, &principal.Name, &principal.PrincipalType, &principal.ExternalSubjectReference, &principal.Status, &principal.CreatedAt, &principal.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, principal)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveGatewayPrincipal(ctx context.Context, principal GatewayPrincipal) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO gateway_principals(id, application_id, name, principal_type, external_subject_reference, status, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT(id) DO UPDATE SET
  application_id = EXCLUDED.application_id,
  name = EXCLUDED.name,
  principal_type = EXCLUDED.principal_type,
  external_subject_reference = EXCLUDED.external_subject_reference,
  status = EXCLUDED.status,
  updated_at = EXCLUDED.updated_at
`, principal.ID, principal.ApplicationID, principal.Name, principal.PrincipalType, principal.ExternalSubjectReference, principal.Status, principal.CreatedAt, principal.UpdatedAt)
	return err
}

func (r *PostgresRepository) ListExternalAuthIntegrations(ctx context.Context) ([]ExternalAuthIntegration, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, application_id, gateway_principal_id, name, protocol, key_id, secret_configured, secret_hint, secret_ciphertext,
       issuer, jwks_url, subject_claim, models_claim, qps_limit_claim, monthly_token_limit_claim,
       audience, policy_id, model_allowlist, qps_limit, monthly_token_limit, max_ttl_seconds,
       status, created_at, updated_at
FROM external_auth_integrations
ORDER BY created_at ASC, name ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExternalAuthIntegration{}
	for rows.Next() {
		var integration ExternalAuthIntegration
		var allowlist string
		if err := rows.Scan(
			&integration.ID, &integration.ApplicationID, &integration.GatewayPrincipalID, &integration.Name, &integration.Protocol, &integration.KeyID,
			&integration.SecretConfigured, &integration.SecretHint, &integration.SecretCiphertext,
			&integration.Issuer, &integration.JWKSURL, &integration.SubjectClaim, &integration.ModelsClaim,
			&integration.QPSLimitClaim, &integration.MonthlyTokenClaim,
			&integration.Audience, &integration.PolicyID, &allowlist, &integration.QPSLimit,
			&integration.MonthlyTokenLimit, &integration.MaxTTLSeconds, &integration.Status,
			&integration.CreatedAt, &integration.UpdatedAt,
		); err != nil {
			return nil, err
		}
		integration.ModelAllowlist = parseStringList(allowlist)
		out = append(out, integration)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveExternalAuthIntegration(ctx context.Context, integration ExternalAuthIntegration) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO external_auth_integrations(
  id, application_id, gateway_principal_id, name, protocol, key_id, secret_configured, secret_hint, secret_ciphertext,
  issuer, jwks_url, subject_claim, models_claim, qps_limit_claim, monthly_token_limit_claim,
  audience, policy_id, model_allowlist, qps_limit, monthly_token_limit, max_ttl_seconds,
  status, created_at, updated_at
)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)
ON CONFLICT(id) DO UPDATE SET
  application_id = EXCLUDED.application_id,
  gateway_principal_id = EXCLUDED.gateway_principal_id,
  name = EXCLUDED.name,
  protocol = EXCLUDED.protocol,
  key_id = EXCLUDED.key_id,
  secret_configured = EXCLUDED.secret_configured,
  secret_hint = EXCLUDED.secret_hint,
  secret_ciphertext = EXCLUDED.secret_ciphertext,
  issuer = EXCLUDED.issuer,
  jwks_url = EXCLUDED.jwks_url,
  subject_claim = EXCLUDED.subject_claim,
  models_claim = EXCLUDED.models_claim,
  qps_limit_claim = EXCLUDED.qps_limit_claim,
  monthly_token_limit_claim = EXCLUDED.monthly_token_limit_claim,
  audience = EXCLUDED.audience,
  policy_id = EXCLUDED.policy_id,
  model_allowlist = EXCLUDED.model_allowlist,
  qps_limit = EXCLUDED.qps_limit,
  monthly_token_limit = EXCLUDED.monthly_token_limit,
  max_ttl_seconds = EXCLUDED.max_ttl_seconds,
  status = EXCLUDED.status,
  updated_at = EXCLUDED.updated_at
`, integration.ID, integration.ApplicationID, integration.GatewayPrincipalID, integration.Name, integration.Protocol, integration.KeyID,
		integration.SecretConfigured, integration.SecretHint, integration.SecretCiphertext,
		integration.Issuer, integration.JWKSURL, integration.SubjectClaim, integration.ModelsClaim,
		integration.QPSLimitClaim, integration.MonthlyTokenClaim,
		integration.Audience, integration.PolicyID, marshalStringList(integration.ModelAllowlist),
		integration.QPSLimit, integration.MonthlyTokenLimit, integration.MaxTTLSeconds,
		integration.Status, integration.CreatedAt, integration.UpdatedAt)
	return err
}
