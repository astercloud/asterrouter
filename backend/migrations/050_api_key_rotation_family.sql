ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS replaces_key_id TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS replaced_by_key_id TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS rotation_grace_expires_at TIMESTAMPTZ;

CREATE UNIQUE INDEX IF NOT EXISTS api_keys_replaces_key_idx
  ON api_keys(replaces_key_id)
  WHERE replaces_key_id <> '';
