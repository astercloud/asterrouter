CREATE TABLE IF NOT EXISTS provider_billing_sources (
  id TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL REFERENCES provider_connections(id) ON DELETE RESTRICT,
  provider_account_id TEXT NOT NULL UNIQUE REFERENCES provider_accounts(id) ON DELETE RESTRICT,
  adapter_id TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'observe_only' CHECK (status IN ('observe_only','active','disabled')),
  automatic_sync_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  sync_interval_seconds INTEGER NOT NULL DEFAULT 3600 CHECK (sync_interval_seconds BETWEEN 60 AND 86400),
  cursor TEXT NOT NULL DEFAULT '',
  usage_cost_lines BOOLEAN NOT NULL DEFAULT FALSE,
  aggregate_usage BOOLEAN NOT NULL DEFAULT FALSE,
  balance_supported BOOLEAN NOT NULL DEFAULT FALSE,
  incremental_sync BOOLEAN NOT NULL DEFAULT FALSE,
  price_feed BOOLEAN NOT NULL DEFAULT FALSE,
  detection_status TEXT NOT NULL DEFAULT '',
  contract_version TEXT NOT NULL DEFAULT '',
  evidence_hash TEXT NOT NULL DEFAULT '',
  warnings TEXT NOT NULL DEFAULT '[]',
  next_sync_at TIMESTAMPTZ,
  last_sync_started_at TIMESTAMPTZ,
  last_sync_completed_at TIMESTAMPTZ,
  last_success_at TIMESTAMPTZ,
  consecutive_failures INTEGER NOT NULL DEFAULT 0 CHECK (consecutive_failures >= 0),
  last_error_code TEXT NOT NULL DEFAULT '',
  lease_token TEXT NOT NULL DEFAULT '',
  lease_expires_at TIMESTAMPTZ,
  version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
  created_by TEXT NOT NULL DEFAULT '',
  updated_by TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS provider_billing_sources_due_idx
  ON provider_billing_sources(next_sync_at, id)
  WHERE automatic_sync_enabled = TRUE AND status <> 'disabled';

CREATE INDEX IF NOT EXISTS provider_billing_sources_lease_idx
  ON provider_billing_sources(lease_expires_at)
  WHERE lease_token <> '';

CREATE TABLE IF NOT EXISTS provider_billing_sync_runs (
  id TEXT PRIMARY KEY,
  source_id TEXT NOT NULL REFERENCES provider_billing_sources(id) ON DELETE CASCADE,
  provider_id TEXT NOT NULL REFERENCES provider_connections(id) ON DELETE RESTRICT,
  provider_account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE RESTRICT,
  trigger TEXT NOT NULL CHECK (trigger IN ('manual','scheduled')),
  triggered_by TEXT NOT NULL DEFAULT '',
  adapter_id TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('running','succeeded','failed','lease_expired')),
  usage_cost_lines BOOLEAN NOT NULL DEFAULT FALSE,
  aggregate_usage BOOLEAN NOT NULL DEFAULT FALSE,
  balance_supported BOOLEAN NOT NULL DEFAULT FALSE,
  incremental_sync BOOLEAN NOT NULL DEFAULT FALSE,
  price_feed BOOLEAN NOT NULL DEFAULT FALSE,
  detection_status TEXT NOT NULL DEFAULT '',
  contract_version TEXT NOT NULL DEFAULT '',
  discovered_lines INTEGER NOT NULL DEFAULT 0 CHECK (discovered_lines >= 0),
  imported_lines INTEGER NOT NULL DEFAULT 0 CHECK (imported_lines >= 0),
  skipped_lines INTEGER NOT NULL DEFAULT 0 CHECK (skipped_lines >= 0),
  evidence_hash TEXT NOT NULL DEFAULT '',
  warnings TEXT NOT NULL DEFAULT '[]',
  error_code TEXT NOT NULL DEFAULT '',
  started_at TIMESTAMPTZ NOT NULL,
  finished_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS provider_billing_sync_runs_source_idx
  ON provider_billing_sync_runs(source_id, started_at DESC);

CREATE INDEX IF NOT EXISTS provider_billing_sync_runs_status_idx
  ON provider_billing_sync_runs(status, started_at DESC);

CREATE TABLE IF NOT EXISTS provider_balance_snapshots (
  id TEXT PRIMARY KEY,
  source_id TEXT NOT NULL REFERENCES provider_billing_sources(id) ON DELETE CASCADE,
  sync_run_id TEXT NOT NULL REFERENCES provider_billing_sync_runs(id) ON DELETE CASCADE,
  provider_account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE RESTRICT,
  kind TEXT NOT NULL CHECK (kind IN ('wallet_balance','api_key_quota_remaining','subscription_period_remaining')),
  amount_micros BIGINT NOT NULL,
  unlimited BOOLEAN NOT NULL DEFAULT FALSE,
  currency TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
  evidence_hash TEXT NOT NULL DEFAULT '',
  observed_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  UNIQUE(sync_run_id, kind)
);

CREATE INDEX IF NOT EXISTS provider_balance_snapshots_source_idx
  ON provider_balance_snapshots(source_id, observed_at DESC);

CREATE TABLE IF NOT EXISTS provider_usage_aggregate_snapshots (
  id TEXT PRIMARY KEY,
  source_id TEXT NOT NULL REFERENCES provider_billing_sources(id) ON DELETE CASCADE,
  sync_run_id TEXT NOT NULL REFERENCES provider_billing_sync_runs(id) ON DELETE CASCADE,
  provider_account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE RESTRICT,
  scope TEXT NOT NULL,
  model TEXT NOT NULL DEFAULT '',
  request_count BIGINT NOT NULL DEFAULT 0 CHECK (request_count >= 0),
  input_tokens BIGINT NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
  output_tokens BIGINT NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
  cache_creation_tokens BIGINT NOT NULL DEFAULT 0 CHECK (cache_creation_tokens >= 0),
  cache_read_tokens BIGINT NOT NULL DEFAULT 0 CHECK (cache_read_tokens >= 0),
  list_cost_micros BIGINT CHECK (list_cost_micros IS NULL OR list_cost_micros >= 0),
  actual_cost_micros BIGINT CHECK (actual_cost_micros IS NULL OR actual_cost_micros >= 0),
  currency TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
  evidence_hash TEXT NOT NULL DEFAULT '',
  observed_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  UNIQUE(sync_run_id, scope, model)
);

CREATE INDEX IF NOT EXISTS provider_usage_aggregate_snapshots_source_idx
  ON provider_usage_aggregate_snapshots(source_id, observed_at DESC);

CREATE INDEX IF NOT EXISTS provider_usage_aggregate_snapshots_model_idx
  ON provider_usage_aggregate_snapshots(provider_account_id, model, observed_at DESC)
  WHERE model <> '';
