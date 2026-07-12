ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS key_type TEXT NOT NULL DEFAULT 'workspace';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS customer_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS api_keys_customer_idx
  ON api_keys(customer_id, status)
  WHERE customer_id <> '';
