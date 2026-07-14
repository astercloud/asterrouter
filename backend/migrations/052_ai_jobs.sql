CREATE UNIQUE INDEX IF NOT EXISTS ai_operations_idempotency_scope_idx
  ON ai_operations(profile_scope, tenant_id, credential_source, credential_id, integration_id, principal_type, principal_id, external_subject_reference, operation, idempotency_key)
  WHERE idempotency_key <> '';

DROP INDEX IF EXISTS ai_operations_idempotency_idx;

CREATE TABLE IF NOT EXISTS ai_jobs (
  id TEXT PRIMARY KEY,
  operation_id TEXT NOT NULL UNIQUE REFERENCES ai_operations(id) ON DELETE RESTRICT,
  profile_scope TEXT NOT NULL DEFAULT '',
  tenant_id TEXT NOT NULL,
  credential_id TEXT NOT NULL,
  credential_source TEXT NOT NULL,
  integration_id TEXT NOT NULL DEFAULT '',
  principal_type TEXT NOT NULL DEFAULT '',
  principal_id TEXT NOT NULL DEFAULT '',
  external_subject_reference TEXT NOT NULL DEFAULT '',
  request_fingerprint TEXT NOT NULL,
  idempotency_key TEXT NOT NULL DEFAULT '',
  protocol TEXT NOT NULL,
  operation TEXT NOT NULL,
  modality TEXT NOT NULL,
  model TEXT NOT NULL,
  artifact_policy TEXT NOT NULL,
  request_payload_ciphertext TEXT NOT NULL,
  status TEXT NOT NULL,
  status_version INTEGER NOT NULL,
  priority INTEGER NOT NULL DEFAULT 0,
  next_eligible_at TIMESTAMPTZ NOT NULL,
  queue_lease_until TIMESTAMPTZ,
  queue_lease_token TEXT NOT NULL DEFAULT '',
  queue_worker_id TEXT NOT NULL DEFAULT '',
  fence_token BIGINT NOT NULL DEFAULT 0,
  error_type TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  completed_at TIMESTAMPTZ,
  expires_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS ai_jobs_idempotency_idx
  ON ai_jobs(profile_scope, tenant_id, credential_source, credential_id, integration_id, principal_type, principal_id, external_subject_reference, operation, idempotency_key)
  WHERE idempotency_key <> '';

CREATE INDEX IF NOT EXISTS ai_jobs_owner_created_idx
  ON ai_jobs(profile_scope, tenant_id, integration_id, principal_type, principal_id, external_subject_reference, created_at DESC);

CREATE INDEX IF NOT EXISTS ai_jobs_ready_idx
  ON ai_jobs(status, next_eligible_at, priority DESC, created_at);

CREATE TABLE IF NOT EXISTS ai_job_events (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES ai_jobs(id) ON DELETE RESTRICT,
  version INTEGER NOT NULL,
  event_type TEXT NOT NULL,
  from_status TEXT NOT NULL DEFAULT '',
  to_status TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  UNIQUE(job_id, version)
);

CREATE INDEX IF NOT EXISTS ai_job_events_job_version_idx
  ON ai_job_events(job_id, version);
