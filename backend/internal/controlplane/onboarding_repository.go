package controlplane

import (
	"context"
	"database/sql"
)

const onboardingSessionColumns = `id, actor, idempotency_key, status, current_step,
provider_id, provider_account_id, provider_health_check_id, gateway_model_id, model_route_id,
api_key_id, verification_client, verification_model, verification_operation_id, verification_trace_id,
verification_http_status, verification_error_code, verification_recovery_action,
failure_stage, failure_code, recovery_hint, version, created_at, updated_at, expires_at`

type onboardingSessionScanner interface {
	Scan(dest ...any) error
}

func scanOnboardingSession(scanner onboardingSessionScanner) (OnboardingSession, error) {
	var session OnboardingSession
	err := scanner.Scan(
		&session.ID, &session.Actor, &session.IdempotencyKey, &session.Status, &session.CurrentStep,
		&session.ProviderID, &session.ProviderAccountID, &session.ProviderHealthCheckID, &session.GatewayModelID, &session.ModelRouteID,
		&session.APIKeyID, &session.VerificationClient, &session.VerificationModel, &session.VerificationOperationID, &session.VerificationTraceID,
		&session.VerificationHTTPStatus, &session.VerificationErrorCode, &session.VerificationRecoveryAction,
		&session.FailureStage, &session.FailureCode, &session.RecoveryHint, &session.Version,
		&session.CreatedAt, &session.UpdatedAt, &session.ExpiresAt,
	)
	return session, err
}

func (r *MemoryRepository) CreateOrGetOnboardingSession(_ context.Context, session OnboardingSession) (OnboardingSession, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.onboardingSessions {
		if existing.Actor == session.Actor && existing.IdempotencyKey == session.IdempotencyKey {
			return existing, false, nil
		}
	}
	r.onboardingSessions[session.ID] = session
	return session, true, nil
}

func (r *MemoryRepository) FindOnboardingSession(_ context.Context, id string) (OnboardingSession, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	session, ok := r.onboardingSessions[id]
	return session, ok, nil
}

func (r *MemoryRepository) UpdateOnboardingSession(_ context.Context, session OnboardingSession, expectedVersion int64) (OnboardingSession, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.onboardingSessions[session.ID]
	if !ok || existing.Version != expectedVersion {
		return OnboardingSession{}, false, nil
	}
	session.Version = expectedVersion + 1
	r.onboardingSessions[session.ID] = session
	return session, true, nil
}

func (r *PostgresRepository) CreateOrGetOnboardingSession(ctx context.Context, session OnboardingSession) (OnboardingSession, bool, error) {
	result, err := r.db.ExecContext(ctx, `
INSERT INTO onboarding_sessions(`+onboardingSessionColumns+`)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25)
ON CONFLICT(actor, idempotency_key) DO NOTHING
`, onboardingSessionArgs(session)...)
	if err != nil {
		return OnboardingSession{}, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return OnboardingSession{}, false, err
	}
	if rows == 1 {
		return session, true, nil
	}
	row := r.db.QueryRowContext(ctx, `SELECT `+onboardingSessionColumns+` FROM onboarding_sessions WHERE actor=$1 AND idempotency_key=$2`, session.Actor, session.IdempotencyKey)
	existing, err := scanOnboardingSession(row)
	return existing, false, err
}

func (r *PostgresRepository) FindOnboardingSession(ctx context.Context, id string) (OnboardingSession, bool, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+onboardingSessionColumns+` FROM onboarding_sessions WHERE id=$1`, id)
	session, err := scanOnboardingSession(row)
	if err == sql.ErrNoRows {
		return OnboardingSession{}, false, nil
	}
	return session, err == nil, err
}

func (r *PostgresRepository) UpdateOnboardingSession(ctx context.Context, session OnboardingSession, expectedVersion int64) (OnboardingSession, bool, error) {
	row := r.db.QueryRowContext(ctx, `
UPDATE onboarding_sessions SET
status=$2, current_step=$3, provider_id=$4, provider_account_id=$5, provider_health_check_id=$6,
gateway_model_id=$7, model_route_id=$8, api_key_id=$9,
verification_client=$10, verification_model=$11, verification_operation_id=$12, verification_trace_id=$13,
verification_http_status=$14, verification_error_code=$15, verification_recovery_action=$16,
failure_stage=$17, failure_code=$18, recovery_hint=$19, updated_at=$20, expires_at=$21, version=version+1
WHERE id=$1 AND version=$22
RETURNING `+onboardingSessionColumns,
		session.ID, session.Status, session.CurrentStep, session.ProviderID, session.ProviderAccountID, session.ProviderHealthCheckID,
		session.GatewayModelID, session.ModelRouteID, session.APIKeyID,
		session.VerificationClient, session.VerificationModel, session.VerificationOperationID, session.VerificationTraceID,
		session.VerificationHTTPStatus, session.VerificationErrorCode, session.VerificationRecoveryAction,
		session.FailureStage, session.FailureCode, session.RecoveryHint, session.UpdatedAt, session.ExpiresAt, expectedVersion,
	)
	updated, err := scanOnboardingSession(row)
	if err == sql.ErrNoRows {
		return OnboardingSession{}, false, nil
	}
	return updated, err == nil, err
}

func onboardingSessionArgs(session OnboardingSession) []any {
	return []any{
		session.ID, session.Actor, session.IdempotencyKey, session.Status, session.CurrentStep,
		session.ProviderID, session.ProviderAccountID, session.ProviderHealthCheckID, session.GatewayModelID, session.ModelRouteID,
		session.APIKeyID, session.VerificationClient, session.VerificationModel, session.VerificationOperationID, session.VerificationTraceID,
		session.VerificationHTTPStatus, session.VerificationErrorCode, session.VerificationRecoveryAction,
		session.FailureStage, session.FailureCode, session.RecoveryHint, session.Version,
		session.CreatedAt, session.UpdatedAt, session.ExpiresAt,
	}
}
