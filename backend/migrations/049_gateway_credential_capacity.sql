CREATE TABLE IF NOT EXISTS gateway_credential_rate_samples (
  id TEXT PRIMARY KEY,
  application_id TEXT NOT NULL DEFAULT '',
  credential_id TEXT NOT NULL,
  estimated_tokens INTEGER NOT NULL DEFAULT 0,
  occurred_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS gateway_credential_rate_samples_window_idx
  ON gateway_credential_rate_samples(application_id, credential_id, occurred_at);

CREATE TABLE IF NOT EXISTS gateway_credential_capacity_leases (
  id TEXT PRIMARY KEY,
  application_id TEXT NOT NULL DEFAULT '',
  credential_id TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS gateway_credential_capacity_leases_expiry_idx
  ON gateway_credential_capacity_leases(application_id, credential_id, expires_at);
