CREATE TABLE IF NOT EXISTS gateway_provider_rate_samples (
  id TEXT PRIMARY KEY,
  provider_account_id TEXT NOT NULL,
  estimated_tokens INTEGER NOT NULL DEFAULT 0 CHECK (estimated_tokens >= 0),
  occurred_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS gateway_provider_rate_samples_window_idx
  ON gateway_provider_rate_samples(provider_account_id, occurred_at);

CREATE TABLE IF NOT EXISTS gateway_provider_capacity_leases (
  id TEXT PRIMARY KEY,
  provider_account_id TEXT NOT NULL,
  capacity_units INTEGER NOT NULL CHECK (capacity_units > 0),
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS gateway_provider_capacity_leases_expiry_idx
  ON gateway_provider_capacity_leases(provider_account_id, expires_at);
