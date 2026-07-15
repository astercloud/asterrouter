package controlplane

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (r *MemoryRepository) ListPlatformTenants(context.Context) ([]PlatformTenant, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]PlatformTenant, 0, len(r.platformTenants))
	for _, tenant := range r.platformTenants {
		out = append(out, tenant)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].Name < out[j].Name
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (r *MemoryRepository) SavePlatformTenant(_ context.Context, tenant PlatformTenant) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.platformTenants[tenant.ID] = tenant
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

func (r *MemoryRepository) ListPlatformUsageSinks(context.Context) ([]PlatformUsageSink, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]PlatformUsageSink, 0, len(r.platformUsageSinks))
	for _, sink := range r.platformUsageSinks {
		out = append(out, sink)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].Name < out[j].Name
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (r *MemoryRepository) SavePlatformUsageSink(_ context.Context, sink PlatformUsageSink) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.platformUsageSinks[sink.ID] = sink
	return nil
}

func (r *MemoryRepository) QueryPlatformUsageDeliveryEvents(_ context.Context, query PlatformUsageDeliveryQuery) ([]PlatformUsageDeliveryEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]PlatformUsageDeliveryEvent, 0, len(r.platformUsageDeliveryEvents))
	for _, event := range r.platformUsageDeliveryEvents {
		if strings.TrimSpace(query.SinkID) != "" && event.SinkID != query.SinkID {
			continue
		}
		if strings.TrimSpace(query.DeliveryID) != "" && event.ID != query.DeliveryID {
			continue
		}
		if strings.TrimSpace(query.Status) != "" && event.Status != query.Status {
			continue
		}
		out = append(out, event)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	limit, offset := normalizeListWindow(query.Limit, query.Offset, 50, 500)
	if offset >= len(out) {
		return []PlatformUsageDeliveryEvent{}, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], nil
}

func (r *MemoryRepository) SaveUsageRecordAndEnqueuePlatformUsage(_ context.Context, record UsageRecord, events []PlatformUsageDeliveryEvent) error {
	usageDimensions, err := NormalizeUsageDimensions(record.UsageDimensions)
	if err != nil {
		return err
	}
	record.UsageDimensions = usageDimensions
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.usageRecords[record.ID]; exists {
		return fmt.Errorf("usage record %q already exists", record.ID)
	}
	for _, event := range events {
		if _, exists := r.platformUsageDeliveryEvents[event.ID]; exists {
			return fmt.Errorf("platform usage delivery event %q already exists", event.ID)
		}
		for _, current := range r.platformUsageDeliveryEvents {
			if current.EventID == event.EventID || (current.SinkID == event.SinkID && current.UsageRecordID == event.UsageRecordID) {
				return fmt.Errorf("platform usage delivery event is not unique")
			}
		}
	}
	r.usageRecords[record.ID] = record
	for _, event := range events {
		r.platformUsageDeliveryEvents[event.ID] = event
	}
	return nil
}

func (r *MemoryRepository) ClaimDuePlatformUsageDeliveryEvents(_ context.Context, now, leaseUntil time.Time, leaseToken string, limit int) ([]PlatformUsageDeliveryEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit <= 0 {
		return []PlatformUsageDeliveryEvent{}, nil
	}
	candidates := make([]PlatformUsageDeliveryEvent, 0, len(r.platformUsageDeliveryEvents))
	for _, event := range r.platformUsageDeliveryEvents {
		sink, found := r.platformUsageSinks[event.SinkID]
		if !found || sink.Status != PlatformUsageSinkStatusActive || event.NextAttemptAt.After(now) {
			continue
		}
		if event.Status == PlatformUsageDeliveryStatusDelivered || event.Status == PlatformUsageDeliveryStatusDeadLetter {
			continue
		}
		if event.Status == PlatformUsageDeliveryStatusDelivering && event.LeaseUntil != nil && event.LeaseUntil.After(now) {
			continue
		}
		candidates = append(candidates, event)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].NextAttemptAt.Equal(candidates[j].NextAttemptAt) {
			return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
		}
		return candidates[i].NextAttemptAt.Before(candidates[j].NextAttemptAt)
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	for index := range candidates {
		event := candidates[index]
		event.Status = PlatformUsageDeliveryStatusDelivering
		event.AttemptCount++
		event.LeaseToken = leaseToken
		event.LeaseUntil = &leaseUntil
		event.UpdatedAt = now
		r.platformUsageDeliveryEvents[event.ID] = event
		candidates[index] = event
	}
	return candidates, nil
}

func (r *MemoryRepository) CompletePlatformUsageDeliveryEvent(_ context.Context, id, leaseToken string, deliveredAt time.Time, httpStatus int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	event, found := r.platformUsageDeliveryEvents[id]
	if !found || event.Status != PlatformUsageDeliveryStatusDelivering || event.LeaseToken != leaseToken {
		return fmt.Errorf("platform usage delivery event is not leased")
	}
	event.Status = PlatformUsageDeliveryStatusDelivered
	event.DeliveredAt = &deliveredAt
	event.LastHTTPStatus = httpStatus
	event.LastError = ""
	event.LeaseToken = ""
	event.LeaseUntil = nil
	event.UpdatedAt = deliveredAt
	r.platformUsageDeliveryEvents[id] = event
	return nil
}

func (r *MemoryRepository) ReschedulePlatformUsageDeliveryEvent(_ context.Context, id, leaseToken string, nextAttemptAt time.Time, httpStatus int, lastError string, deadLetter bool, updatedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	event, found := r.platformUsageDeliveryEvents[id]
	if !found || event.Status != PlatformUsageDeliveryStatusDelivering || event.LeaseToken != leaseToken {
		return fmt.Errorf("platform usage delivery event is not leased")
	}
	event.Status = PlatformUsageDeliveryStatusPending
	if deadLetter {
		event.Status = PlatformUsageDeliveryStatusDeadLetter
	}
	event.NextAttemptAt = nextAttemptAt
	event.LastHTTPStatus = httpStatus
	event.LastError = trimPlatformUsageDeliveryError(lastError)
	event.LeaseToken = ""
	event.LeaseUntil = nil
	event.UpdatedAt = updatedAt
	r.platformUsageDeliveryEvents[id] = event
	return nil
}

func (r *MemoryRepository) RequeuePlatformUsageDeliveryEvent(_ context.Context, id string, nextAttemptAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	event, found := r.platformUsageDeliveryEvents[id]
	if !found {
		return fmt.Errorf("platform usage delivery event not found")
	}
	if event.Status != PlatformUsageDeliveryStatusDeadLetter {
		return fmt.Errorf("only dead-letter platform usage events can be requeued")
	}
	event.Status = PlatformUsageDeliveryStatusPending
	event.NextAttemptAt = nextAttemptAt
	event.LeaseToken = ""
	event.LeaseUntil = nil
	event.LastError = ""
	event.UpdatedAt = nextAttemptAt
	r.platformUsageDeliveryEvents[id] = event
	return nil
}

func (r *PostgresRepository) ListPlatformTenants(ctx context.Context) ([]PlatformTenant, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, name, slug, entitlement_reference, status, created_at, updated_at
FROM platform_tenants
ORDER BY created_at ASC, name ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlatformTenant{}
	for rows.Next() {
		var tenant PlatformTenant
		if err := rows.Scan(&tenant.ID, &tenant.Name, &tenant.Slug, &tenant.EntitlementReference, &tenant.Status, &tenant.CreatedAt, &tenant.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, tenant)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SavePlatformTenant(ctx context.Context, tenant PlatformTenant) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO platform_tenants(id, name, slug, entitlement_reference, status, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT(id) DO UPDATE SET
  name = EXCLUDED.name,
  slug = EXCLUDED.slug,
  entitlement_reference = EXCLUDED.entitlement_reference,
  status = EXCLUDED.status,
  updated_at = EXCLUDED.updated_at
`, tenant.ID, tenant.Name, tenant.Slug, tenant.EntitlementReference, tenant.Status, tenant.CreatedAt, tenant.UpdatedAt)
	return err
}

func (r *PostgresRepository) ListGatewayPrincipals(ctx context.Context) ([]GatewayPrincipal, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, tenant_id, name, principal_type, external_subject_reference, status, created_at, updated_at
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
		if err := rows.Scan(&principal.ID, &principal.TenantID, &principal.Name, &principal.PrincipalType, &principal.ExternalSubjectReference, &principal.Status, &principal.CreatedAt, &principal.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, principal)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveGatewayPrincipal(ctx context.Context, principal GatewayPrincipal) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO gateway_principals(id, tenant_id, name, principal_type, external_subject_reference, status, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT(id) DO UPDATE SET
  tenant_id = EXCLUDED.tenant_id,
  name = EXCLUDED.name,
  principal_type = EXCLUDED.principal_type,
  external_subject_reference = EXCLUDED.external_subject_reference,
  status = EXCLUDED.status,
  updated_at = EXCLUDED.updated_at
`, principal.ID, principal.TenantID, principal.Name, principal.PrincipalType, principal.ExternalSubjectReference, principal.Status, principal.CreatedAt, principal.UpdatedAt)
	return err
}

func (r *PostgresRepository) ListExternalAuthIntegrations(ctx context.Context) ([]ExternalAuthIntegration, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, tenant_id, gateway_principal_id, name, protocol, key_id, secret_configured, secret_hint, secret_ciphertext,
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
			&integration.ID, &integration.TenantID, &integration.GatewayPrincipalID, &integration.Name, &integration.Protocol, &integration.KeyID,
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
  id, tenant_id, gateway_principal_id, name, protocol, key_id, secret_configured, secret_hint, secret_ciphertext,
  issuer, jwks_url, subject_claim, models_claim, qps_limit_claim, monthly_token_limit_claim,
  audience, policy_id, model_allowlist, qps_limit, monthly_token_limit, max_ttl_seconds,
  status, created_at, updated_at
)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)
ON CONFLICT(id) DO UPDATE SET
  tenant_id = EXCLUDED.tenant_id,
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
`, integration.ID, integration.TenantID, integration.GatewayPrincipalID, integration.Name, integration.Protocol, integration.KeyID,
		integration.SecretConfigured, integration.SecretHint, integration.SecretCiphertext,
		integration.Issuer, integration.JWKSURL, integration.SubjectClaim, integration.ModelsClaim,
		integration.QPSLimitClaim, integration.MonthlyTokenClaim,
		integration.Audience, integration.PolicyID, marshalStringList(integration.ModelAllowlist),
		integration.QPSLimit, integration.MonthlyTokenLimit, integration.MaxTTLSeconds,
		integration.Status, integration.CreatedAt, integration.UpdatedAt)
	return err
}

func (r *PostgresRepository) ListPlatformUsageSinks(ctx context.Context) ([]PlatformUsageSink, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, tenant_id, external_auth_integration_id, name, endpoint_url_ciphertext, endpoint_url_hint,
       signing_secret_ciphertext, signing_secret_hint, status, max_attempts, created_at, updated_at
FROM platform_usage_sinks
ORDER BY created_at ASC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlatformUsageSink{}
	for rows.Next() {
		var sink PlatformUsageSink
		if err := rows.Scan(&sink.ID, &sink.TenantID, &sink.ExternalAuthIntegrationID, &sink.Name, &sink.EndpointURLCiphertext, &sink.EndpointURLHint, &sink.SigningSecretCiphertext, &sink.SigningSecretHint, &sink.Status, &sink.MaxAttempts, &sink.CreatedAt, &sink.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, sink)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SavePlatformUsageSink(ctx context.Context, sink PlatformUsageSink) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO platform_usage_sinks(id, tenant_id, external_auth_integration_id, name, endpoint_url_ciphertext, endpoint_url_hint, signing_secret_ciphertext, signing_secret_hint, status, max_attempts, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT(id) DO UPDATE SET
  tenant_id = EXCLUDED.tenant_id,
  external_auth_integration_id = EXCLUDED.external_auth_integration_id,
  name = EXCLUDED.name,
  endpoint_url_ciphertext = EXCLUDED.endpoint_url_ciphertext,
  endpoint_url_hint = EXCLUDED.endpoint_url_hint,
  signing_secret_ciphertext = EXCLUDED.signing_secret_ciphertext,
  signing_secret_hint = EXCLUDED.signing_secret_hint,
  status = EXCLUDED.status,
  max_attempts = EXCLUDED.max_attempts,
  updated_at = EXCLUDED.updated_at`, sink.ID, sink.TenantID, sink.ExternalAuthIntegrationID, sink.Name, sink.EndpointURLCiphertext, sink.EndpointURLHint, sink.SigningSecretCiphertext, sink.SigningSecretHint, sink.Status, sink.MaxAttempts, sink.CreatedAt, sink.UpdatedAt)
	return err
}

func (r *PostgresRepository) QueryPlatformUsageDeliveryEvents(ctx context.Context, query PlatformUsageDeliveryQuery) ([]PlatformUsageDeliveryEvent, error) {
	limit, offset := normalizeListWindow(query.Limit, query.Offset, 50, 500)
	clauses := []string{}
	args := []any{}
	if value := strings.TrimSpace(query.SinkID); value != "" {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("sink_id = $%d", len(args)))
	}
	if value := strings.TrimSpace(query.DeliveryID); value != "" {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("id = $%d", len(args)))
	}
	if value := strings.TrimSpace(query.Status); value != "" {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)))
	}
	statement := `SELECT id, sink_id, usage_record_id, event_id, payload_json, status, attempt_count, max_attempts, next_attempt_at, lease_until, lease_token, delivered_at, last_http_status, last_error, target_hint, created_at, updated_at FROM platform_usage_delivery_events`
	if len(clauses) > 0 {
		statement += " WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, limit, offset)
	statement += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlatformUsageDeliveryEvent{}
	for rows.Next() {
		event, err := scanPlatformUsageDeliveryEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveUsageRecordAndEnqueuePlatformUsage(ctx context.Context, record UsageRecord, events []PlatformUsageDeliveryEvent) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := saveUsageRecord(ctx, tx, record); err != nil {
		return err
	}
	for _, event := range events {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO platform_usage_delivery_events(id, sink_id, usage_record_id, event_id, payload_json, status, attempt_count, max_attempts, next_attempt_at, lease_until, lease_token, delivered_at, last_http_status, last_error, target_hint, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,NULL,'',NULL,0,'',$10,$11,$12)`, event.ID, event.SinkID, event.UsageRecordID, event.EventID, event.PayloadJSON, event.Status, event.AttemptCount, event.MaxAttempts, event.NextAttemptAt, event.TargetHint, event.CreatedAt, event.UpdatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *PostgresRepository) ClaimDuePlatformUsageDeliveryEvents(ctx context.Context, now, leaseUntil time.Time, leaseToken string, limit int) ([]PlatformUsageDeliveryEvent, error) {
	if limit <= 0 {
		return []PlatformUsageDeliveryEvent{}, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `
SELECT event.id, event.sink_id, event.usage_record_id, event.event_id, event.payload_json, event.status, event.attempt_count, event.max_attempts, event.next_attempt_at, event.lease_until, event.lease_token, event.delivered_at, event.last_http_status, event.last_error, event.target_hint, event.created_at, event.updated_at
FROM platform_usage_delivery_events event
JOIN platform_usage_sinks sink ON sink.id = event.sink_id
WHERE sink.status = $1
  AND event.status IN ($2, $3)
  AND event.next_attempt_at <= $4
  AND (event.status = $2 OR event.lease_until IS NULL OR event.lease_until <= $4)
ORDER BY event.next_attempt_at ASC, event.created_at ASC
FOR UPDATE SKIP LOCKED
LIMIT $5`, PlatformUsageSinkStatusActive, PlatformUsageDeliveryStatusPending, PlatformUsageDeliveryStatusDelivering, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []PlatformUsageDeliveryEvent{}
	for rows.Next() {
		event, scanErr := scanPlatformUsageDeliveryEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range events {
		event := &events[index]
		if _, err := tx.ExecContext(ctx, `UPDATE platform_usage_delivery_events SET status=$1, attempt_count=attempt_count+1, lease_until=$2, lease_token=$3, updated_at=$4 WHERE id=$5`, PlatformUsageDeliveryStatusDelivering, leaseUntil, leaseToken, now, event.ID); err != nil {
			return nil, err
		}
		event.Status = PlatformUsageDeliveryStatusDelivering
		event.AttemptCount++
		event.LeaseUntil = &leaseUntil
		event.LeaseToken = leaseToken
		event.UpdatedAt = now
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return events, nil
}

func (r *PostgresRepository) CompletePlatformUsageDeliveryEvent(ctx context.Context, id, leaseToken string, deliveredAt time.Time, httpStatus int) error {
	result, err := r.db.ExecContext(ctx, `UPDATE platform_usage_delivery_events SET status=$1, delivered_at=$2, last_http_status=$3, last_error='', lease_until=NULL, lease_token='', updated_at=$2 WHERE id=$4 AND status=$5 AND lease_token=$6`, PlatformUsageDeliveryStatusDelivered, deliveredAt, httpStatus, id, PlatformUsageDeliveryStatusDelivering, leaseToken)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return fmt.Errorf("platform usage delivery event is not leased")
	}
	return nil
}

func (r *PostgresRepository) ReschedulePlatformUsageDeliveryEvent(ctx context.Context, id, leaseToken string, nextAttemptAt time.Time, httpStatus int, lastError string, deadLetter bool, updatedAt time.Time) error {
	status := PlatformUsageDeliveryStatusPending
	if deadLetter {
		status = PlatformUsageDeliveryStatusDeadLetter
	}
	result, err := r.db.ExecContext(ctx, `UPDATE platform_usage_delivery_events SET status=$1, next_attempt_at=$2, last_http_status=$3, last_error=$4, lease_until=NULL, lease_token='', updated_at=$5 WHERE id=$6 AND status=$7 AND lease_token=$8`, status, nextAttemptAt, httpStatus, trimPlatformUsageDeliveryError(lastError), updatedAt, id, PlatformUsageDeliveryStatusDelivering, leaseToken)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return fmt.Errorf("platform usage delivery event is not leased")
	}
	return nil
}

func (r *PostgresRepository) RequeuePlatformUsageDeliveryEvent(ctx context.Context, id string, nextAttemptAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE platform_usage_delivery_events SET status=$1, next_attempt_at=$2, last_error='', lease_until=NULL, lease_token='', updated_at=$2 WHERE id=$3 AND status=$4`, PlatformUsageDeliveryStatusPending, nextAttemptAt, id, PlatformUsageDeliveryStatusDeadLetter)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return fmt.Errorf("platform usage delivery event not found or not dead-lettered")
	}
	return nil
}

type platformUsageDeliveryEventScanner interface {
	Scan(dest ...any) error
}

func scanPlatformUsageDeliveryEvent(scanner platformUsageDeliveryEventScanner) (PlatformUsageDeliveryEvent, error) {
	var event PlatformUsageDeliveryEvent
	var leaseUntil, deliveredAt sql.NullTime
	if err := scanner.Scan(&event.ID, &event.SinkID, &event.UsageRecordID, &event.EventID, &event.PayloadJSON, &event.Status, &event.AttemptCount, &event.MaxAttempts, &event.NextAttemptAt, &leaseUntil, &event.LeaseToken, &deliveredAt, &event.LastHTTPStatus, &event.LastError, &event.TargetHint, &event.CreatedAt, &event.UpdatedAt); err != nil {
		return PlatformUsageDeliveryEvent{}, err
	}
	if leaseUntil.Valid {
		event.LeaseUntil = &leaseUntil.Time
	}
	if deliveredAt.Valid {
		event.DeliveredAt = &deliveredAt.Time
	}
	return event, nil
}

func trimPlatformUsageDeliveryError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 500 {
		return value
	}
	return value[:500]
}
