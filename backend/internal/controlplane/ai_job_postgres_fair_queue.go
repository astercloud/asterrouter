package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/lib/pq"
)

const aiJobFairSelectColumns = `id, application_id, credential_source, integration_id,
principal_type, principal_id, external_subject_reference, status, priority, next_eligible_at, queue_lease_until, created_at`

const aiJobFairQualifiedSelectColumns = `job.id, job.application_id, job.credential_source, job.integration_id,
job.principal_type, job.principal_id, job.external_subject_reference, job.status, job.priority,
job.next_eligible_at, job.queue_lease_until, job.created_at`

func listPostgresAIJobFairCandidates(ctx context.Context, tx *sql.Tx, now time.Time, perPrincipalLimit int) ([]AIJob, error) {
	rows, err := tx.QueryContext(ctx, `
WITH ranked AS (
  SELECT job.id, job.application_id, job.credential_source, job.integration_id,
         job.principal_type, job.principal_id, job.external_subject_reference, job.status, job.priority,
         job.next_eligible_at, job.queue_lease_until, job.created_at,
         ROW_NUMBER() OVER (
           PARTITION BY application_id, credential_source, integration_id, principal_type, principal_id, external_subject_reference
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
SELECT job.application_id, job.credential_source, job.integration_id,
       job.principal_type, job.principal_id, job.external_subject_reference, MAX(event.created_at)
FROM ai_job_events event
JOIN ai_jobs job ON job.id = event.job_id
WHERE event.event_type = $1
GROUP BY job.application_id, job.credential_source, job.integration_id,
         job.principal_type, job.principal_id, job.external_subject_reference`, AIJobEventScheduled)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]aiJobDispatchActivity, 0)
	for rows.Next() {
		var activity aiJobDispatchActivity
		if err := rows.Scan(
			&activity.Job.ApplicationID, &activity.Job.CredentialSource, &activity.Job.IntegrationID,
			&activity.Job.PrincipalType, &activity.Job.PrincipalID, &activity.Job.ExternalSubjectReference, &activity.DispatchedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, activity)
	}
	return out, rows.Err()
}

func listPostgresAIJobFairCandidatesByReferences(ctx context.Context, tx *sql.Tx, references []AIJobReadyReference, now time.Time) ([]AIJob, map[string]int, error) {
	ids := make([]string, 0, len(references))
	versions := make([]int64, 0, len(references))
	expected := make(map[string]int, len(references))
	for _, reference := range references {
		id := strings.TrimSpace(reference.JobID)
		if id == "" || reference.StatusVersion <= 0 || expected[id] != 0 {
			continue
		}
		expected[id] = reference.StatusVersion
		ids = append(ids, id)
		versions = append(versions, int64(reference.StatusVersion))
	}
	if len(ids) == 0 {
		return []AIJob{}, expected, nil
	}
	rows, err := tx.QueryContext(ctx, `
WITH ready_references AS (
  SELECT * FROM UNNEST($1::text[], $2::bigint[]) AS reference(id, status_version)
)
SELECT `+aiJobFairQualifiedSelectColumns+`
FROM ai_jobs job
JOIN ready_references reference ON reference.id=job.id AND reference.status_version=job.status_version
WHERE (job.status=$3 AND job.next_eligible_at <= $4 AND (job.queue_lease_until IS NULL OR job.queue_lease_until <= $4))
   OR (job.status=$5 AND job.queue_lease_until IS NOT NULL AND job.queue_lease_until <= $4)`,
		pq.Array(ids), pq.Array(versions), AIJobStatusQueued, now, AIJobStatusDispatching)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	candidates := make([]AIJob, 0, len(ids))
	for rows.Next() {
		job, scanErr := scanAIJobFairFields(rows)
		if scanErr != nil {
			return nil, nil, scanErr
		}
		candidates = append(candidates, job)
	}
	return candidates, expected, rows.Err()
}

func lockPostgresAIJobForClaim(ctx context.Context, tx *sql.Tx, id string, expectedVersion int, now time.Time) (AIJob, bool, error) {
	job, err := scanAIJob(tx.QueryRowContext(ctx, `SELECT `+aiJobSelectColumns+` FROM ai_jobs
WHERE id=$1 AND ($2=0 OR status_version=$2) AND (
  (status=$3 AND next_eligible_at <= $4 AND (queue_lease_until IS NULL OR queue_lease_until <= $4))
  OR (status=$5 AND queue_lease_until IS NOT NULL AND queue_lease_until <= $4)
)
FOR UPDATE SKIP LOCKED`, id, expectedVersion, AIJobStatusQueued, now, AIJobStatusDispatching))
	if errors.Is(err, sql.ErrNoRows) {
		return AIJob{}, false, nil
	}
	return job, err == nil, err
}

func lockPostgresAIJobAdmissionScopes(ctx context.Context, tx *sql.Tx, job AIJob, limits AIJobAdmissionLimits) error {
	keys := make([]string, 0, 3)
	if limits.Organization > 0 {
		keys = append(keys, "ai-job-admission:organization")
	}
	if limits.Application > 0 {
		keys = append(keys, "ai-job-admission:application:"+aiJobApplicationFairKey(job))
	}
	if limits.Principal > 0 {
		keys = append(keys, "ai-job-admission:principal:"+aiJobPrincipalFairKey(job))
	}
	for _, key := range keys {
		key = strings.ReplaceAll(key, "\x00", "\n")
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key); err != nil {
			return err
		}
	}
	return nil
}

func countPostgresAIJobAdmissionScopes(ctx context.Context, tx *sql.Tx, job AIJob) (aiJobAdmissionCounts, error) {
	var counts aiJobAdmissionCounts
	err := tx.QueryRowContext(ctx, `
SELECT
  COUNT(*),
  COUNT(*) FILTER (WHERE application_id=$1),
  COUNT(*) FILTER (WHERE application_id=$1 AND credential_source=$2 AND integration_id=$3
    AND principal_type=$4 AND principal_id=$5 AND external_subject_reference=$6)
FROM ai_jobs
WHERE status IN ($7,$8)`,
		strings.TrimSpace(job.ApplicationID), strings.TrimSpace(job.CredentialSource),
		strings.TrimSpace(job.IntegrationID), strings.TrimSpace(job.PrincipalType), strings.TrimSpace(job.PrincipalID),
		strings.TrimSpace(job.ExternalSubjectReference), AIJobStatusQueued, AIJobStatusDispatching,
	).Scan(&counts.Organization, &counts.Application, &counts.Principal)
	return counts, err
}

func scanAIJobFairFields(scanner apiKeyScanner) (AIJob, error) {
	var job AIJob
	var leaseUntil sql.NullTime
	if err := scanner.Scan(
		&job.ID, &job.ApplicationID, &job.CredentialSource, &job.IntegrationID,
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
