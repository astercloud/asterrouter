CREATE TABLE IF NOT EXISTS plugin_configs (
  plugin_id TEXT PRIMARY KEY,
  settings_json TEXT NOT NULL DEFAULT '{}',
  secret_ciphertexts_json TEXT NOT NULL DEFAULT '{}',
  secret_hints_json TEXT NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);
