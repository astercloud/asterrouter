package controlplane

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

func aiJobProgressEventID(attemptID string, sequence int64) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(attemptID) + "\n" + fmt.Sprint(sequence)))
	return "job_progress_" + hex.EncodeToString(digest[:16])
}

func normalizeAIJobProgressEvent(event *AIJobProgressEvent) {
	event.ID = strings.TrimSpace(event.ID)
	event.JobID = strings.TrimSpace(event.JobID)
	event.AttemptID = strings.TrimSpace(event.AttemptID)
	event.ProviderTaskID = strings.TrimSpace(event.ProviderTaskID)
	event.Stage = strings.ToLower(strings.TrimSpace(event.Stage))
}

func validateAIJobProgressEvent(event AIJobProgressEvent) error {
	normalizeAIJobProgressEvent(&event)
	if event.ID == "" || event.ID != aiJobProgressEventID(event.AttemptID, event.ProviderSequence) || event.JobID == "" || event.AttemptID == "" || event.ProviderTaskID == "" || event.ProviderSequence <= 0 {
		return ErrAIJobProgressInvalid
	}
	if event.Percent == nil && event.Stage == "" {
		return ErrAIJobProgressInvalid
	}
	if event.Percent != nil && (*event.Percent < 0 || *event.Percent > 100) {
		return ErrAIJobProgressInvalid
	}
	if event.Stage != "" && !validAIJobProgressStage(event.Stage) {
		return ErrAIJobProgressInvalid
	}
	return nil
}

func validAIJobProgressStage(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for index, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || strings.ContainsRune("._-", character) {
			if index == 0 && strings.ContainsRune("._-", character) {
				return false
			}
			continue
		}
		return false
	}
	return true
}

func cloneAIJobProgressEvent(event AIJobProgressEvent) AIJobProgressEvent {
	if event.Percent != nil {
		percent := *event.Percent
		event.Percent = &percent
	}
	return event
}

func progressEventsEquivalent(left, right AIJobProgressEvent) bool {
	return left.ID == right.ID && left.JobID == right.JobID && left.AttemptID == right.AttemptID &&
		left.ProviderTaskID == right.ProviderTaskID && left.ProviderSequence == right.ProviderSequence &&
		(left.Percent == nil && right.Percent == nil || left.Percent != nil && right.Percent != nil && *left.Percent == *right.Percent) &&
		left.Stage == right.Stage
}

func progressPercentRegresses(previous, next AIJobProgressEvent) bool {
	return previous.Percent != nil && next.Percent != nil && *next.Percent < *previous.Percent
}

func (r *MemoryRepository) AppendAIJobProgressEvent(_ context.Context, event AIJobProgressEvent) (AIJobProgressEvent, bool, error) {
	normalizeAIJobProgressEvent(&event)
	if err := validateAIJobProgressEvent(event); err != nil {
		return AIJobProgressEvent{}, false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	job, found := r.aiJobs[event.JobID]
	if !found {
		return AIJobProgressEvent{}, false, ErrAIJobNotFound
	}
	attempt, found := r.aiAttempts[event.AttemptID]
	if !found {
		return AIJobProgressEvent{}, false, ErrAIAttemptNotFound
	}
	if attempt.OperationID != job.OperationID || attempt.ProviderTaskID == "" || attempt.ProviderTaskID != event.ProviderTaskID {
		return AIJobProgressEvent{}, false, ErrAIJobProgressInvalid
	}
	var same, latest, latestWithPercent *AIJobProgressEvent
	for _, current := range r.aiJobProgressEvents {
		if current.AttemptID != event.AttemptID {
			continue
		}
		if current.ProviderSequence == event.ProviderSequence {
			value := cloneAIJobProgressEvent(current)
			same = &value
		}
		if latest == nil || current.ProviderSequence > latest.ProviderSequence {
			value := cloneAIJobProgressEvent(current)
			latest = &value
		}
		if current.Percent != nil && (latestWithPercent == nil || current.ProviderSequence > latestWithPercent.ProviderSequence) {
			value := cloneAIJobProgressEvent(current)
			latestWithPercent = &value
		}
	}
	if same != nil {
		if !progressEventsEquivalent(*same, event) {
			return AIJobProgressEvent{}, false, ErrAIJobProgressConflict
		}
		return cloneAIJobProgressEvent(*same), false, nil
	}
	if latest != nil && latest.ProviderSequence > event.ProviderSequence {
		return AIJobProgressEvent{}, false, nil
	}
	if latestWithPercent != nil && progressPercentRegresses(*latestWithPercent, event) {
		return AIJobProgressEvent{}, false, ErrAIJobProgressInvalid
	}
	event = cloneAIJobProgressEvent(event)
	r.aiJobProgressEvents[event.ID] = event
	return cloneAIJobProgressEvent(event), true, nil
}

func (r *MemoryRepository) ListAIJobProgressEvents(_ context.Context, jobID string) ([]AIJobProgressEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]AIJobProgressEvent, 0)
	for _, event := range r.aiJobProgressEvents {
		if strings.TrimSpace(jobID) == "" || event.JobID == strings.TrimSpace(jobID) {
			out = append(out, cloneAIJobProgressEvent(event))
		}
	}
	sortAIJobProgressEvents(out)
	return out, nil
}

func (r *PostgresRepository) AppendAIJobProgressEvent(ctx context.Context, event AIJobProgressEvent) (AIJobProgressEvent, bool, error) {
	normalizeAIJobProgressEvent(&event)
	if err := validateAIJobProgressEvent(event); err != nil {
		return AIJobProgressEvent{}, false, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return AIJobProgressEvent{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	var operationID, attemptOperationID, providerTaskID string
	err = tx.QueryRowContext(ctx, `SELECT j.operation_id, a.operation_id, a.provider_task_id
FROM ai_jobs j JOIN ai_attempts a ON a.operation_id=j.operation_id
WHERE j.id=$1 AND a.id=$2 FOR UPDATE OF a`, event.JobID, event.AttemptID).Scan(&operationID, &attemptOperationID, &providerTaskID)
	if errors.Is(err, sql.ErrNoRows) {
		return AIJobProgressEvent{}, false, ErrAIJobProgressInvalid
	}
	if err != nil {
		return AIJobProgressEvent{}, false, err
	}
	if operationID != attemptOperationID || providerTaskID == "" || providerTaskID != event.ProviderTaskID {
		return AIJobProgressEvent{}, false, ErrAIJobProgressInvalid
	}
	current, found, err := scanAIJobProgressEvent(tx.QueryRowContext(ctx, `SELECT id, job_id, attempt_id, provider_task_id, provider_sequence, percent, stage, created_at
FROM ai_job_progress_events WHERE attempt_id=$1 AND provider_sequence=$2`, event.AttemptID, event.ProviderSequence))
	if err != nil {
		return AIJobProgressEvent{}, false, err
	}
	if found {
		if !progressEventsEquivalent(current, event) {
			return AIJobProgressEvent{}, false, ErrAIJobProgressConflict
		}
		if err := tx.Commit(); err != nil {
			return AIJobProgressEvent{}, false, err
		}
		return current, false, nil
	}
	var latest AIJobProgressEvent
	latest, found, err = scanAIJobProgressEvent(tx.QueryRowContext(ctx, `SELECT id, job_id, attempt_id, provider_task_id, provider_sequence, percent, stage, created_at
FROM ai_job_progress_events WHERE attempt_id=$1 ORDER BY provider_sequence DESC LIMIT 1`, event.AttemptID))
	if err != nil {
		return AIJobProgressEvent{}, false, err
	}
	if found && latest.ProviderSequence > event.ProviderSequence {
		if err := tx.Commit(); err != nil {
			return AIJobProgressEvent{}, false, err
		}
		return AIJobProgressEvent{}, false, nil
	}
	if event.Percent != nil {
		latestWithPercent, found, err := scanAIJobProgressEvent(tx.QueryRowContext(ctx, `SELECT id, job_id, attempt_id, provider_task_id, provider_sequence, percent, stage, created_at
FROM ai_job_progress_events WHERE attempt_id=$1 AND percent IS NOT NULL ORDER BY provider_sequence DESC LIMIT 1`, event.AttemptID))
		if err != nil {
			return AIJobProgressEvent{}, false, err
		}
		if found && progressPercentRegresses(latestWithPercent, event) {
			return AIJobProgressEvent{}, false, ErrAIJobProgressInvalid
		}
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO ai_job_progress_events(id, job_id, attempt_id, provider_task_id, provider_sequence, percent, stage, created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (attempt_id, provider_sequence) DO NOTHING`, event.ID, event.JobID, event.AttemptID, event.ProviderTaskID, event.ProviderSequence, event.Percent, event.Stage, event.CreatedAt)
	if err != nil {
		return AIJobProgressEvent{}, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return AIJobProgressEvent{}, false, err
	}
	if rows == 0 {
		current, found, err = scanAIJobProgressEvent(tx.QueryRowContext(ctx, `SELECT id, job_id, attempt_id, provider_task_id, provider_sequence, percent, stage, created_at
FROM ai_job_progress_events WHERE attempt_id=$1 AND provider_sequence=$2`, event.AttemptID, event.ProviderSequence))
		if err != nil {
			return AIJobProgressEvent{}, false, err
		}
		if !found || !progressEventsEquivalent(current, event) {
			return AIJobProgressEvent{}, false, ErrAIJobProgressConflict
		}
		if err := tx.Commit(); err != nil {
			return AIJobProgressEvent{}, false, err
		}
		return current, false, nil
	}
	if err := tx.Commit(); err != nil {
		return AIJobProgressEvent{}, false, err
	}
	return event, true, nil
}

func (r *PostgresRepository) ListAIJobProgressEvents(ctx context.Context, jobID string) ([]AIJobProgressEvent, error) {
	query := `SELECT id, job_id, attempt_id, provider_task_id, provider_sequence, percent, stage, created_at FROM ai_job_progress_events`
	args := []any{}
	if strings.TrimSpace(jobID) != "" {
		query += ` WHERE job_id=$1`
		args = append(args, strings.TrimSpace(jobID))
	}
	query += ` ORDER BY created_at, id`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AIJobProgressEvent, 0)
	for rows.Next() {
		event, found, scanErr := scanAIJobProgressEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		if found {
			out = append(out, event)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortAIJobProgressEvents(out)
	return out, nil
}

func sortAIJobProgressEvents(events []AIJobProgressEvent) {
	sort.SliceStable(events, func(i, j int) bool {
		if !events[i].CreatedAt.Equal(events[j].CreatedAt) {
			return events[i].CreatedAt.Before(events[j].CreatedAt)
		}
		if events[i].AttemptID == events[j].AttemptID && events[i].ProviderSequence != events[j].ProviderSequence {
			return events[i].ProviderSequence < events[j].ProviderSequence
		}
		return events[i].ID < events[j].ID
	})
}

type aiJobProgressScanner interface {
	Scan(dest ...any) error
}

func scanAIJobProgressEvent(scanner aiJobProgressScanner) (AIJobProgressEvent, bool, error) {
	var event AIJobProgressEvent
	var percent sql.NullInt64
	if err := scanner.Scan(&event.ID, &event.JobID, &event.AttemptID, &event.ProviderTaskID, &event.ProviderSequence, &percent, &event.Stage, &event.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AIJobProgressEvent{}, false, nil
		}
		return AIJobProgressEvent{}, false, err
	}
	if percent.Valid {
		value := int(percent.Int64)
		event.Percent = &value
	}
	return event, true, nil
}
