ALTER TABLE ai_attempts ADD COLUMN IF NOT EXISTS dispatch_state TEXT NOT NULL DEFAULT 'pending';
ALTER TABLE ai_attempts ADD COLUMN IF NOT EXISTS dispatch_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE ai_attempts ADD COLUMN IF NOT EXISTS dispatch_key TEXT NOT NULL DEFAULT '';
ALTER TABLE ai_attempts ADD COLUMN IF NOT EXISTS dispatch_intent_json TEXT NOT NULL DEFAULT '';
ALTER TABLE ai_attempts ADD COLUMN IF NOT EXISTS dispatch_submitted_at TIMESTAMPTZ;
ALTER TABLE ai_attempts ADD COLUMN IF NOT EXISTS provider_task_id TEXT NOT NULL DEFAULT '';
ALTER TABLE ai_attempts ADD COLUMN IF NOT EXISTS provider_request_id TEXT NOT NULL DEFAULT '';
ALTER TABLE ai_attempts ADD COLUMN IF NOT EXISTS provider_task_status TEXT NOT NULL DEFAULT '';
ALTER TABLE ai_attempts ADD COLUMN IF NOT EXISTS provider_accepted_at TIMESTAMPTZ;
ALTER TABLE ai_attempts ADD COLUMN IF NOT EXISTS last_reconciled_at TIMESTAMPTZ;
ALTER TABLE ai_attempts ADD COLUMN IF NOT EXISTS reconcile_after TIMESTAMPTZ;

UPDATE ai_attempts SET dispatch_key = id WHERE dispatch_key = '';

CREATE INDEX IF NOT EXISTS ai_attempts_reconciliation_idx
  ON ai_attempts(dispatch_state, reconcile_after, updated_at)
  WHERE status = 'running' AND dispatch_state IN ('submitted', 'accepted', 'unknown');

CREATE UNIQUE INDEX IF NOT EXISTS ai_attempts_provider_task_idx
  ON ai_attempts(provider_account_id, provider_task_id)
  WHERE provider_task_id <> '';
