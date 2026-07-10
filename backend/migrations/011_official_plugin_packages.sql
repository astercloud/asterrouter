CREATE TABLE IF NOT EXISTS official_plugin_packages (
  package_id TEXT PRIMARY KEY,
  plugin_id TEXT NOT NULL,
  plugin_slug TEXT NOT NULL,
  plugin_public_id TEXT NOT NULL DEFAULT '',
  version_public_id TEXT NOT NULL DEFAULT '',
  version TEXT NOT NULL,
  channel TEXT NOT NULL DEFAULT '',
  required_entitlement BOOLEAN NOT NULL DEFAULT false,
  min_core_version TEXT NOT NULL DEFAULT '',
  max_core_version TEXT NOT NULL DEFAULT '',
  os TEXT NOT NULL,
  arch TEXT NOT NULL,
  sha256 TEXT NOT NULL,
  size_bytes BIGINT NOT NULL DEFAULT 0,
  signature_json TEXT NOT NULL DEFAULT '{}',
  revoked BOOLEAN NOT NULL DEFAULT false,
  compatibility_json TEXT NOT NULL DEFAULT '[]',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS official_plugin_packages_plugin_idx
  ON official_plugin_packages(plugin_id, version DESC);

CREATE TABLE IF NOT EXISTS official_plugin_package_caches (
  package_id TEXT PRIMARY KEY,
  plugin_id TEXT NOT NULL,
  version TEXT NOT NULL,
  os TEXT NOT NULL,
  arch TEXT NOT NULL,
  sha256 TEXT NOT NULL,
  size_bytes BIGINT NOT NULL DEFAULT 0,
  cache_path TEXT NOT NULL,
  status TEXT NOT NULL,
  error TEXT NOT NULL DEFAULT '',
  cached_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS official_plugin_package_caches_plugin_idx
  ON official_plugin_package_caches(plugin_id, updated_at DESC);
