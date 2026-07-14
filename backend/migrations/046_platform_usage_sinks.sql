CREATE TABLE IF NOT EXISTS platform_usage_sinks (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES platform_tenants(id) ON DELETE RESTRICT,
  external_auth_integration_id TEXT NOT NULL REFERENCES external_auth_integrations(id) ON DELETE RESTRICT,
  name TEXT NOT NULL,
  endpoint_url_ciphertext TEXT NOT NULL DEFAULT '',
  endpoint_url_hint TEXT NOT NULL DEFAULT '',
  signing_secret_ciphertext TEXT NOT NULL DEFAULT '',
  signing_secret_hint TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'disabled',
  max_attempts INTEGER NOT NULL DEFAULT 10,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE(external_auth_integration_id, name)
);

CREATE INDEX IF NOT EXISTS platform_usage_sinks_integration_status_idx
  ON platform_usage_sinks(external_auth_integration_id, status);

CREATE TABLE IF NOT EXISTS platform_usage_delivery_events (
  id TEXT PRIMARY KEY,
  sink_id TEXT NOT NULL REFERENCES platform_usage_sinks(id) ON DELETE RESTRICT,
  usage_record_id TEXT NOT NULL REFERENCES usage_records(id) ON DELETE RESTRICT,
  event_id TEXT NOT NULL UNIQUE,
  payload_json TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  attempt_count INTEGER NOT NULL DEFAULT 0,
  max_attempts INTEGER NOT NULL DEFAULT 10,
  next_attempt_at TIMESTAMPTZ NOT NULL,
  lease_until TIMESTAMPTZ NULL,
  lease_token TEXT NOT NULL DEFAULT '',
  delivered_at TIMESTAMPTZ NULL,
  last_http_status INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  target_hint TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE(sink_id, usage_record_id)
);

CREATE INDEX IF NOT EXISTS platform_usage_delivery_due_idx
  ON platform_usage_delivery_events(status, next_attempt_at, lease_until);
