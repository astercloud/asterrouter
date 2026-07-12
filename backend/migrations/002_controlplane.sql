CREATE TABLE IF NOT EXISTS provider_connections (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  base_url TEXT NOT NULL,
  status TEXT NOT NULL,
  models TEXT NOT NULL DEFAULT '[]',
  priority INTEGER NOT NULL DEFAULT 100,
  secret_configured BOOLEAN NOT NULL DEFAULT false,
  secret_hint TEXT NOT NULL DEFAULT '',
  secret_ciphertext TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE provider_connections ADD COLUMN IF NOT EXISTS secret_ciphertext TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS api_keys (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  key_hash TEXT NOT NULL UNIQUE,
  fingerprint TEXT NOT NULL,
  prefix TEXT NOT NULL,
  status TEXT NOT NULL,
  key_type TEXT NOT NULL DEFAULT 'workspace',
  customer_id TEXT NOT NULL DEFAULT '',
  policy_id TEXT NOT NULL DEFAULT '',
  model_allowlist TEXT NOT NULL DEFAULT '[]',
  qps_limit INTEGER NOT NULL DEFAULT 0,
  monthly_token_limit INTEGER NOT NULL DEFAULT 0,
  expires_at TIMESTAMPTZ,
  last_used_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_logs (
  id TEXT PRIMARY KEY,
  actor TEXT NOT NULL,
  action TEXT NOT NULL,
  resource_type TEXT NOT NULL,
  resource_id TEXT NOT NULL,
  summary TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
);
