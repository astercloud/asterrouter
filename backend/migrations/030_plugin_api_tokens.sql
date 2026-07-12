CREATE TABLE IF NOT EXISTS plugin_api_tokens (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  plugin_id TEXT NOT NULL DEFAULT '',
  token_prefix TEXT NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  scopes_json TEXT NOT NULL DEFAULT '[]',
  surfaces_json TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL,
  expires_at TIMESTAMPTZ,
  last_used_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS plugin_api_tokens_plugin_idx
  ON plugin_api_tokens(plugin_id, created_at DESC);

CREATE INDEX IF NOT EXISTS plugin_api_tokens_status_idx
  ON plugin_api_tokens(status, created_at DESC);
