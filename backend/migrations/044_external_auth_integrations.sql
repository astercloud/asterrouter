CREATE TABLE IF NOT EXISTS external_auth_integrations (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES platform_tenants(id) ON DELETE RESTRICT,
  gateway_principal_id TEXT NOT NULL REFERENCES gateway_principals(id) ON DELETE RESTRICT,
  name TEXT NOT NULL,
  protocol TEXT NOT NULL DEFAULT 'hmac_signed_context',
  key_id TEXT NOT NULL,
  secret_configured BOOLEAN NOT NULL DEFAULT false,
  secret_hint TEXT NOT NULL DEFAULT '',
  secret_ciphertext TEXT NOT NULL DEFAULT '',
  audience TEXT NOT NULL DEFAULT '',
  policy_id TEXT NOT NULL DEFAULT '',
  model_allowlist TEXT NOT NULL DEFAULT '[]',
  qps_limit INTEGER NOT NULL DEFAULT 0,
  monthly_token_limit INTEGER NOT NULL DEFAULT 0,
  max_ttl_seconds INTEGER NOT NULL DEFAULT 300,
  status TEXT NOT NULL DEFAULT 'disabled',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE(tenant_id, name)
);

CREATE INDEX IF NOT EXISTS external_auth_integrations_tenant_status_idx
  ON external_auth_integrations(tenant_id, status);

CREATE UNIQUE INDEX IF NOT EXISTS external_auth_integrations_key_id_idx
  ON external_auth_integrations(key_id);

ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS external_auth_integration_id TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS external_subject_reference TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS usage_records_external_auth_created_idx
  ON usage_records(profile_scope, external_auth_integration_id, external_subject_reference, created_at DESC)
  WHERE external_auth_integration_id <> '';

ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS external_auth_integration_id TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS external_subject_reference TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS gateway_traces_external_auth_created_idx
  ON gateway_traces(profile_scope, external_auth_integration_id, external_subject_reference, created_at DESC)
  WHERE external_auth_integration_id <> '';

ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS external_auth_integration_id TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS external_subject_reference TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS audit_logs_external_auth_created_idx
  ON audit_logs(profile_scope, external_auth_integration_id, external_subject_reference, created_at DESC)
  WHERE external_auth_integration_id <> '';

ALTER TABLE alert_events ADD COLUMN IF NOT EXISTS external_auth_integration_id TEXT NOT NULL DEFAULT '';
ALTER TABLE alert_events ADD COLUMN IF NOT EXISTS external_subject_reference TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS alert_events_external_auth_last_seen_idx
  ON alert_events(profile_scope, external_auth_integration_id, external_subject_reference, last_seen_at DESC)
  WHERE external_auth_integration_id <> '';
