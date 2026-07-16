ALTER TABLE provider_accounts
  ADD COLUMN IF NOT EXISTS auto_enable_new_models BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE IF NOT EXISTS provider_account_models (
  provider_account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
  model_id TEXT NOT NULL,
  source TEXT NOT NULL CHECK (source IN ('discovered', 'manual')),
  enabled BOOLEAN NOT NULL DEFAULT false,
  availability TEXT NOT NULL CHECK (availability IN ('available', 'missing', 'unverified')),
  first_seen_at TIMESTAMPTZ NOT NULL,
  last_seen_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (provider_account_id, model_id)
);

CREATE INDEX IF NOT EXISTS provider_account_models_account_enabled_idx
  ON provider_account_models(provider_account_id, enabled, model_id);

CREATE INDEX IF NOT EXISTS provider_account_models_availability_idx
  ON provider_account_models(availability, last_seen_at DESC);
