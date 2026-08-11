CREATE TABLE IF NOT EXISTS applications (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  slug TEXT NOT NULL UNIQUE,
  entitlement_reference TEXT NOT NULL DEFAULT '',
  concurrency_limit INTEGER NOT NULL DEFAULT 0 CHECK (concurrency_limit >= 0),
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS gateway_principals (
  id TEXT PRIMARY KEY,
  application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE RESTRICT,
  name TEXT NOT NULL,
  principal_type TEXT NOT NULL DEFAULT 'service',
  external_subject_reference TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE(application_id, name)
);

CREATE INDEX IF NOT EXISTS gateway_principals_application_status_idx
  ON gateway_principals(application_id, status);

ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS application_id TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS gateway_principal_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS api_keys_application_principal_idx
  ON api_keys(application_id, gateway_principal_id, status)
;

ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS application_id TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS application_name TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS gateway_principal_id TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS gateway_principal_name TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS usage_records_application_principal_created_idx
  ON usage_records(application_id, gateway_principal_id, created_at DESC)
;

ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS application_id TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS application_name TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS gateway_principal_id TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS gateway_principal_name TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS gateway_traces_application_principal_created_idx
  ON gateway_traces(application_id, gateway_principal_id, created_at DESC)
;

ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS application_id TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS application_name TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS gateway_principal_id TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS gateway_principal_name TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS audit_logs_application_principal_created_idx
  ON audit_logs(application_id, gateway_principal_id, created_at DESC)
;

ALTER TABLE alert_events ADD COLUMN IF NOT EXISTS application_id TEXT NOT NULL DEFAULT '';
ALTER TABLE alert_events ADD COLUMN IF NOT EXISTS application_name TEXT NOT NULL DEFAULT '';
ALTER TABLE alert_events ADD COLUMN IF NOT EXISTS gateway_principal_id TEXT NOT NULL DEFAULT '';
ALTER TABLE alert_events ADD COLUMN IF NOT EXISTS gateway_principal_name TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS alert_events_application_principal_last_seen_idx
  ON alert_events(application_id, gateway_principal_id, last_seen_at DESC)
;
