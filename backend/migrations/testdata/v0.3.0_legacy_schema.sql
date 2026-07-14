-- Minimal PostgreSQL schema emitted by the v0.3.0 runtime for the persisted
-- control-plane records exercised by the upgrade contract. It intentionally
-- predates workspace_users.session_version and customer notification tables.
CREATE TABLE provider_connections (
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

CREATE TABLE workspace_users (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL DEFAULT '',
  avatar_data_url TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active',
  role TEXT NOT NULL DEFAULT 'developer',
  balance_cents INTEGER NOT NULL DEFAULT 0,
  concurrency_limit INTEGER NOT NULL DEFAULT 5,
  rpm_limit INTEGER NOT NULL DEFAULT 0,
  external_issuer TEXT NOT NULL DEFAULT '',
  external_subject TEXT NOT NULL DEFAULT '',
  department_id TEXT NOT NULL DEFAULT '',
  totp_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  totp_secret_ciphertext TEXT NOT NULL DEFAULT '',
  totp_recovery_hashes TEXT NOT NULL DEFAULT '[]',
  password_hash TEXT NOT NULL DEFAULT '',
  email_verified BOOLEAN NOT NULL DEFAULT FALSE,
  email_verify_hash TEXT NOT NULL DEFAULT '',
  email_verify_expires_at TIMESTAMPTZ,
  password_reset_hash TEXT NOT NULL DEFAULT '',
  password_reset_expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE api_keys (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  key_hash TEXT NOT NULL UNIQUE,
  fingerprint TEXT NOT NULL,
  prefix TEXT NOT NULL,
  status TEXT NOT NULL,
  key_type TEXT NOT NULL DEFAULT 'workspace',
  customer_id TEXT NOT NULL DEFAULT '',
  owner_user_id TEXT NOT NULL DEFAULT '',
  policy_id TEXT NOT NULL DEFAULT '',
  model_allowlist TEXT NOT NULL DEFAULT '[]',
  qps_limit INTEGER NOT NULL DEFAULT 0,
  monthly_token_limit INTEGER NOT NULL DEFAULT 0,
  expires_at TIMESTAMPTZ,
  last_used_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE usage_records (
  id TEXT PRIMARY KEY,
  api_key_id TEXT NOT NULL,
  customer_id TEXT NOT NULL DEFAULT '',
  api_fingerprint TEXT NOT NULL,
  model TEXT NOT NULL,
  upstream_model TEXT NOT NULL DEFAULT '',
  provider_id TEXT NOT NULL DEFAULT '',
  provider_account_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  error_type TEXT NOT NULL DEFAULT '',
  latency_ms BIGINT NOT NULL DEFAULT 0,
  input_tokens INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  cost_cents INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL
);
