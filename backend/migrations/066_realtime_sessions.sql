CREATE TABLE IF NOT EXISTS realtime_sessions (
  id TEXT PRIMARY KEY,
  operation_id TEXT NOT NULL UNIQUE REFERENCES ai_operations(id) ON DELETE RESTRICT,
  attempt_id TEXT NOT NULL UNIQUE REFERENCES ai_attempts(id) ON DELETE RESTRICT,
  profile_scope TEXT NOT NULL DEFAULT '',
  tenant_id TEXT NOT NULL DEFAULT '',
  credential_id TEXT NOT NULL,
  principal_type TEXT NOT NULL DEFAULT '',
  principal_id TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL,
  provider_id TEXT NOT NULL DEFAULT '',
  provider_account_id TEXT NOT NULL DEFAULT '',
  upstream_model TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  version INTEGER NOT NULL,
  input_audio_bytes BIGINT NOT NULL DEFAULT 0,
  output_audio_bytes BIGINT NOT NULL DEFAULT 0,
  client_message_count BIGINT NOT NULL DEFAULT 0,
  provider_message_count BIGINT NOT NULL DEFAULT 0,
  transfer_bytes BIGINT NOT NULL DEFAULT 0,
  usage_version INTEGER NOT NULL DEFAULT 0,
  session_duration_ms BIGINT NOT NULL DEFAULT 0,
  error_type TEXT NOT NULL DEFAULT '',
  connected_at TIMESTAMPTZ,
  closed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  CHECK (status IN ('connecting', 'connected', 'completed', 'failed', 'canceled')),
  CHECK (version > 0),
  CHECK (input_audio_bytes >= 0 AND output_audio_bytes >= 0),
  CHECK (client_message_count >= 0 AND provider_message_count >= 0),
  CHECK (transfer_bytes >= 0 AND usage_version >= 0 AND session_duration_ms >= 0)
);

CREATE INDEX IF NOT EXISTS realtime_sessions_tenant_created_idx
  ON realtime_sessions(profile_scope, tenant_id, created_at DESC);

CREATE INDEX IF NOT EXISTS realtime_sessions_status_updated_idx
  ON realtime_sessions(status, updated_at);
