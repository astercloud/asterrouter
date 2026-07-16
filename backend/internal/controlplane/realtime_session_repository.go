package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

const realtimeSessionSelectColumns = `id, operation_id, attempt_id, profile_scope, tenant_id, credential_id, principal_type, principal_id,
model, provider_id, provider_account_id, upstream_model, status, version, input_audio_bytes, output_audio_bytes,
client_message_count, provider_message_count, transfer_bytes, usage_version, session_duration_ms, error_type,
connected_at, closed_at, created_at, updated_at`

type realtimeSessionScanner interface {
	Scan(...any) error
}

func scanRealtimeSession(scanner realtimeSessionScanner) (RealtimeSession, error) {
	var session RealtimeSession
	err := scanner.Scan(
		&session.ID, &session.OperationID, &session.AttemptID, &session.ProfileScope, &session.TenantID, &session.CredentialID,
		&session.PrincipalType, &session.PrincipalID, &session.Model, &session.ProviderID, &session.ProviderAccountID, &session.UpstreamModel,
		&session.Status, &session.Version, &session.InputAudioBytes, &session.OutputAudioBytes, &session.ClientMessageCount,
		&session.ProviderMessageCount, &session.TransferBytes, &session.UsageVersion, &session.SessionDurationMS, &session.ErrorType,
		&session.ConnectedAt, &session.ClosedAt, &session.CreatedAt, &session.UpdatedAt,
	)
	return session, err
}

func (r *MemoryRepository) CreateOrGetRealtimeSession(_ context.Context, session RealtimeSession) (RealtimeSession, bool, error) {
	if err := validateRealtimeSession(session); err != nil {
		return RealtimeSession{}, false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, found := r.aiOperations[session.OperationID]; !found {
		return RealtimeSession{}, false, errors.New("realtime operation not found")
	}
	if _, found := r.aiAttempts[session.AttemptID]; !found {
		return RealtimeSession{}, false, errors.New("realtime attempt not found")
	}
	for _, current := range r.realtimeSessions {
		if current.OperationID == session.OperationID || current.AttemptID == session.AttemptID {
			return current, false, nil
		}
	}
	if _, found := r.realtimeSessions[session.ID]; found {
		return RealtimeSession{}, false, errors.New("realtime session id already exists")
	}
	r.realtimeSessions[session.ID] = session
	return session, true, nil
}

func (r *MemoryRepository) FindRealtimeSession(_ context.Context, id string) (RealtimeSession, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	session, found := r.realtimeSessions[strings.TrimSpace(id)]
	return session, found, nil
}

func (r *MemoryRepository) UpdateRealtimeSession(_ context.Context, id string, expectedVersion int, update RealtimeSessionUpdate) (RealtimeSession, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, found := r.realtimeSessions[strings.TrimSpace(id)]
	if !found || current.Version != expectedVersion {
		return current, false, nil
	}
	updated, err := applyRealtimeSessionUpdate(current, update)
	if err != nil {
		return RealtimeSession{}, false, err
	}
	r.realtimeSessions[current.ID] = updated
	return updated, true, nil
}

func (r *PostgresRepository) CreateOrGetRealtimeSession(ctx context.Context, session RealtimeSession) (RealtimeSession, bool, error) {
	if err := validateRealtimeSession(session); err != nil {
		return RealtimeSession{}, false, err
	}
	result, err := r.db.ExecContext(ctx, `
INSERT INTO realtime_sessions(id,operation_id,attempt_id,profile_scope,tenant_id,credential_id,principal_type,principal_id,
model,provider_id,provider_account_id,upstream_model,status,version,input_audio_bytes,output_audio_bytes,client_message_count,
provider_message_count,transfer_bytes,usage_version,session_duration_ms,error_type,connected_at,closed_at,created_at,updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,0,0,0,0,0,0,0,'',NULL,NULL,$15,$16)
ON CONFLICT(operation_id) DO NOTHING
`, session.ID, session.OperationID, session.AttemptID, session.ProfileScope, session.TenantID, session.CredentialID,
		session.PrincipalType, session.PrincipalID, session.Model, session.ProviderID, session.ProviderAccountID, session.UpstreamModel,
		session.Status, session.Version, session.CreatedAt, session.UpdatedAt)
	if err != nil {
		return RealtimeSession{}, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return RealtimeSession{}, false, err
	}
	if rows == 1 {
		return session, true, nil
	}
	current, err := scanRealtimeSession(r.db.QueryRowContext(ctx, `SELECT `+realtimeSessionSelectColumns+` FROM realtime_sessions WHERE operation_id=$1`, session.OperationID))
	return current, false, err
}

func (r *PostgresRepository) FindRealtimeSession(ctx context.Context, id string) (RealtimeSession, bool, error) {
	session, err := scanRealtimeSession(r.db.QueryRowContext(ctx, `SELECT `+realtimeSessionSelectColumns+` FROM realtime_sessions WHERE id=$1`, strings.TrimSpace(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return RealtimeSession{}, false, nil
	}
	return session, err == nil, err
}

func (r *PostgresRepository) UpdateRealtimeSession(ctx context.Context, id string, expectedVersion int, update RealtimeSessionUpdate) (RealtimeSession, bool, error) {
	current, found, err := r.FindRealtimeSession(ctx, id)
	if err != nil || !found || current.Version != expectedVersion {
		return current, false, err
	}
	updated, err := applyRealtimeSessionUpdate(current, update)
	if err != nil {
		return RealtimeSession{}, false, err
	}
	result, err := r.db.ExecContext(ctx, `
UPDATE realtime_sessions SET status=$1,version=$2,input_audio_bytes=$3,output_audio_bytes=$4,client_message_count=$5,
provider_message_count=$6,transfer_bytes=$7,usage_version=$8,session_duration_ms=$9,error_type=$10,connected_at=$11,
closed_at=$12,updated_at=$13 WHERE id=$14 AND version=$15
`, updated.Status, updated.Version, updated.InputAudioBytes, updated.OutputAudioBytes, updated.ClientMessageCount,
		updated.ProviderMessageCount, updated.TransferBytes, updated.UsageVersion, updated.SessionDurationMS, updated.ErrorType,
		updated.ConnectedAt, updated.ClosedAt, updated.UpdatedAt, current.ID, expectedVersion)
	if err != nil {
		return RealtimeSession{}, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows == 0 {
		latest, _, findErr := r.FindRealtimeSession(ctx, id)
		if err != nil {
			return RealtimeSession{}, false, err
		}
		return latest, false, findErr
	}
	return updated, true, nil
}
