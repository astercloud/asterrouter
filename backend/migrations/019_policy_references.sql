ALTER TABLE projects ADD COLUMN IF NOT EXISTS policy_id TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS policy_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS projects_policy_idx
  ON projects(policy_id);

CREATE INDEX IF NOT EXISTS api_keys_policy_idx
  ON api_keys(policy_id);
