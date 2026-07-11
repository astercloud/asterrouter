ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS policy_id TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS policy_name TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS policy_source TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS policy_snapshot TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS gateway_traces_policy_idx
  ON gateway_traces(policy_id, created_at DESC);
