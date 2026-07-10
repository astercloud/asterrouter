CREATE TABLE IF NOT EXISTS usage_records (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  application_id TEXT NOT NULL,
  api_key_id TEXT NOT NULL,
  api_fingerprint TEXT NOT NULL,
  model TEXT NOT NULL,
  provider_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  error_type TEXT NOT NULL DEFAULT '',
  latency_ms BIGINT NOT NULL DEFAULT 0,
  input_tokens INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  cost_cents INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL
);
