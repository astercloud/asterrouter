CREATE TABLE IF NOT EXISTS platform_tenants (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  slug TEXT NOT NULL UNIQUE,
  entitlement_reference TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS gateway_principals (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES platform_tenants(id) ON DELETE RESTRICT,
  name TEXT NOT NULL,
  principal_type TEXT NOT NULL DEFAULT 'service',
  external_subject_reference TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE(tenant_id, name)
);

CREATE INDEX IF NOT EXISTS gateway_principals_tenant_status_idx
  ON gateway_principals(tenant_id, status);

ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS profile_scope TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS platform_tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS gateway_principal_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS api_keys_platform_scope_idx
  ON api_keys(profile_scope, platform_tenant_id, gateway_principal_id, status)
  WHERE profile_scope = 'platform';

ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS profile_scope TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS platform_tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS platform_tenant_name TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS gateway_principal_id TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS gateway_principal_name TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS usage_records_platform_scope_created_idx
  ON usage_records(profile_scope, platform_tenant_id, gateway_principal_id, created_at DESC)
  WHERE profile_scope = 'platform';

ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS profile_scope TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS platform_tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS platform_tenant_name TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS gateway_principal_id TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS gateway_principal_name TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS gateway_traces_platform_scope_created_idx
  ON gateway_traces(profile_scope, platform_tenant_id, gateway_principal_id, created_at DESC)
  WHERE profile_scope = 'platform';

ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS profile_scope TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS platform_tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS platform_tenant_name TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS gateway_principal_id TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS gateway_principal_name TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS audit_logs_platform_scope_created_idx
  ON audit_logs(profile_scope, platform_tenant_id, gateway_principal_id, created_at DESC)
  WHERE profile_scope = 'platform';

ALTER TABLE alert_events ADD COLUMN IF NOT EXISTS profile_scope TEXT NOT NULL DEFAULT '';
ALTER TABLE alert_events ADD COLUMN IF NOT EXISTS platform_tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE alert_events ADD COLUMN IF NOT EXISTS platform_tenant_name TEXT NOT NULL DEFAULT '';
ALTER TABLE alert_events ADD COLUMN IF NOT EXISTS gateway_principal_id TEXT NOT NULL DEFAULT '';
ALTER TABLE alert_events ADD COLUMN IF NOT EXISTS gateway_principal_name TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS alert_events_platform_scope_last_seen_idx
  ON alert_events(profile_scope, platform_tenant_id, gateway_principal_id, last_seen_at DESC)
  WHERE profile_scope = 'platform';
