ALTER TABLE official_catalog_snapshots
  ADD COLUMN IF NOT EXISTS advisory_count INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS official_security_advisories (
  public_id TEXT PRIMARY KEY,
  advisory_id TEXT NOT NULL,
  severity TEXT NOT NULL,
  title TEXT NOT NULL,
  summary TEXT NOT NULL DEFAULT '',
  published_at TIMESTAMPTZ NOT NULL,
  signature_json TEXT NOT NULL DEFAULT '{}',
  synced_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS official_security_advisories_published_idx
  ON official_security_advisories(published_at DESC);

CREATE TABLE IF NOT EXISTS official_security_advisory_affected_versions (
  public_id TEXT PRIMARY KEY,
  advisory_public_id TEXT NOT NULL REFERENCES official_security_advisories(public_id) ON DELETE CASCADE,
  advisory_id TEXT NOT NULL,
  advisory_severity TEXT NOT NULL,
  advisory_title TEXT NOT NULL,
  plugin_id TEXT NOT NULL DEFAULT '',
  plugin_slug TEXT NOT NULL DEFAULT '',
  version_range TEXT NOT NULL,
  fixed_version TEXT NOT NULL DEFAULT '',
  revoked BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS official_security_advisory_affected_plugin_idx
  ON official_security_advisory_affected_versions(plugin_slug, revoked);

CREATE INDEX IF NOT EXISTS official_security_advisory_affected_plugin_id_idx
  ON official_security_advisory_affected_versions(plugin_id, revoked);
