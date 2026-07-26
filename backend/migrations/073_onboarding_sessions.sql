CREATE TABLE IF NOT EXISTS onboarding_sessions (
  id TEXT PRIMARY KEY,
  actor TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  status TEXT NOT NULL,
  current_step TEXT NOT NULL,
  provider_id TEXT NOT NULL DEFAULT '',
  provider_account_id TEXT NOT NULL DEFAULT '',
  provider_health_check_id TEXT NOT NULL DEFAULT '',
  gateway_model_id TEXT NOT NULL DEFAULT '',
  model_route_id TEXT NOT NULL DEFAULT '',
  api_key_id TEXT NOT NULL DEFAULT '',
  verification_client TEXT NOT NULL DEFAULT '',
  verification_model TEXT NOT NULL DEFAULT '',
  verification_operation_id TEXT NOT NULL DEFAULT '',
  verification_trace_id TEXT NOT NULL DEFAULT '',
  verification_http_status INTEGER NOT NULL DEFAULT 0,
  verification_error_code TEXT NOT NULL DEFAULT '',
  verification_recovery_action TEXT NOT NULL DEFAULT '',
  failure_stage TEXT NOT NULL DEFAULT '',
  failure_code TEXT NOT NULL DEFAULT '',
  recovery_hint TEXT NOT NULL DEFAULT '',
  version BIGINT NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  UNIQUE(actor, idempotency_key),
  CHECK (status IN ('in_progress', 'failed', 'completed')),
  CHECK (current_step IN ('started', 'model_source', 'published_model', 'api_key', 'verification')),
  CHECK (version > 0),
  CHECK (verification_http_status >= 0 AND verification_http_status <= 599),
  CHECK (expires_at >= created_at)
);

CREATE INDEX IF NOT EXISTS onboarding_sessions_actor_updated_idx
  ON onboarding_sessions(actor, updated_at DESC);

CREATE INDEX IF NOT EXISTS onboarding_sessions_expiry_idx
  ON onboarding_sessions(expires_at);
