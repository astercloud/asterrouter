package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const aiJobFairSelectColumns = `id, profile_scope, tenant_id, credential_source, integration_id,
principal_type, principal_id, external_subject_reference, status, priority, next_eligible_at, queue_lease_until, created_at`

func listPostgresAIJobFairCandidates(ctx context.Context, tx *sql.Tx, now time.Time, perPrincipalLimit int) ([]AIJob, error) {
	rows, err := tx.QueryContext(ctx, `
WITH ranked AS (
  SELECT job.id, job.profile_scope, job.tenant_id, job.credential_source, job.integration_id,
         job.principal_type, job.principal_id, job.external_subject_reference, job.status, job.priority,
         job.next_eligible_at, job.queue_lease_until, job.created_at,
         ROW_NUMBER() OVER (
           PARTITION BY profile_scope, tenant_id, credential_source, integration_id, principal_type, principal_id, external_subject_reference
           ORDER BY
             CASE WHEN status = $3 THEN 0 ELSE 1 END,
             LEAST($5, GREATEST(0, priority) + GREATEST(0, FLOOR(EXTRACT(EPOCH FROM ($2::timestamptz - next_eligible_at)) / 60)::INTEGER)) DESC,
             next_eligible_at ASC,
             created_at ASC,
             id ASC
         ) AS owner_position
  FROM ai_jobs job
  WHERE (status = $1 AND next_eligible_at <= $2 AND (queue_lease_until IS NULL OR queue_lease_until <= $2))
     OR (status = $3 AND queue_lease_until IS NOT NULL AND queue_lease_until <= $2)
)
SELECT `+aiJobFairSelectColumns+`
FROM ranked
WHERE owner_position <= $4
ORDER BY created_at ASC, id ASC`, AIJobStatusQueued, now, AIJobStatusDispatching, perPrincipalLimit, aiJobMaxPriority)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AIJob, 0)
	for rows.Next() {
		job, scanErr := scanAIJobFairFields(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func listPostgresAIJobInFlight(ctx context.Context, tx *sql.Tx) ([]AIJob, error) {
	rows, err := tx.QueryContext(ctx, `SELECT `+aiJobFairSelectColumns+` FROM ai_jobs WHERE status IN ($1,$2,$3,$4)`,
		AIJobStatusDispatching, AIJobStatusRunning, AIJobStatusCanceling, AIJobStatusUnknown)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AIJob, 0)
	for rows.Next() {
		job, scanErr := scanAIJobFairFields(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func listPostgresAIJobDispatchActivity(ctx context.Context, tx *sql.Tx) ([]aiJobDispatchActivity, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT job.profile_scope, job.tenant_id, job.credential_source, job.integration_id,
       job.principal_type, job.principal_id, job.external_subject_reference, MAX(event.created_at)
FROM ai_job_events event
JOIN ai_jobs job ON job.id = event.job_id
WHERE event.event_type = $1
GROUP BY job.profile_scope, job.tenant_id, job.credential_source, job.integration_id,
         job.principal_type, job.principal_id, job.external_subject_reference`, AIJobEventScheduled)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]aiJobDispatchActivity, 0)
	for rows.Next() {
		var activity aiJobDispatchActivity
		if err := rows.Scan(
			&activity.Job.ProfileScope, &activity.Job.TenantID, &activity.Job.CredentialSource, &activity.Job.IntegrationID,
			&activity.Job.PrincipalType, &activity.Job.PrincipalID, &activity.Job.ExternalSubjectReference, &activity.DispatchedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, activity)
	}
	return out, rows.Err()
}

func lockPostgresAIJobForClaim(ctx context.Context, tx *sql.Tx, id string, now time.Time) (AIJob, bool, error) {
	job, err := scanAIJob(tx.QueryRowContext(ctx, `SELECT `+aiJobSelectColumns+` FROM ai_jobs
WHERE id=$1 AND (
  (status=$2 AND next_eligible_at <= $3 AND (queue_lease_until IS NULL OR queue_lease_until <= $3))
  OR (status=$4 AND queue_lease_until IS NOT NULL AND queue_lease_until <= $3)
)
FOR UPDATE SKIP LOCKED`, id, AIJobStatusQueued, now, AIJobStatusDispatching))
	if errors.Is(err, sql.ErrNoRows) {
		return AIJob{}, false, nil
	}
	return job, err == nil, err
}

func scanAIJobFairFields(scanner apiKeyScanner) (AIJob, error) {
	var job AIJob
	var leaseUntil sql.NullTime
	if err := scanner.Scan(
		&job.ID, &job.ProfileScope, &job.TenantID, &job.CredentialSource, &job.IntegrationID,
		&job.PrincipalType, &job.PrincipalID, &job.ExternalSubjectReference, &job.Status, &job.Priority,
		&job.NextEligibleAt, &leaseUntil, &job.CreatedAt,
	); err != nil {
		return AIJob{}, err
	}
	if leaseUntil.Valid {
		job.QueueLeaseUntil = timePointer(leaseUntil.Time)
	}
	return job, nil
}
