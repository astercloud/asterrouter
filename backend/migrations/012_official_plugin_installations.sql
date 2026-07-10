CREATE TABLE IF NOT EXISTS official_plugin_installations (
  plugin_id TEXT PRIMARY KEY,
  package_id TEXT NOT NULL,
  version TEXT NOT NULL,
  os TEXT NOT NULL,
  arch TEXT NOT NULL,
  cache_path TEXT NOT NULL,
  status TEXT NOT NULL,
  installed_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS official_plugin_installations_status_idx
  ON official_plugin_installations(status, updated_at DESC);
