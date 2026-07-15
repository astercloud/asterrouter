CREATE TABLE IF NOT EXISTS ai_job_progress_events (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES ai_jobs(id) ON DELETE RESTRICT,
  attempt_id TEXT NOT NULL REFERENCES ai_attempts(id) ON DELETE RESTRICT,
  provider_task_id TEXT NOT NULL,
  provider_sequence BIGINT NOT NULL,
  percent INTEGER,
  stage TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  UNIQUE(attempt_id, provider_sequence),
  CHECK (provider_sequence > 0),
  CHECK (percent IS NULL OR (percent >= 0 AND percent <= 100)),
  CHECK (percent IS NOT NULL OR stage <> ''),
  CHECK (char_length(stage) <= 64),
  CHECK (stage = lower(stage)),
  CHECK (stage = '' OR stage ~ '^[a-z0-9][a-z0-9._-]*$')
);

CREATE INDEX IF NOT EXISTS ai_job_progress_events_job_created_idx
  ON ai_job_progress_events(job_id, created_at, id);
