CREATE TABLE IF NOT EXISTS official_catalog_snapshots (
  id TEXT PRIMARY KEY,
  mode TEXT NOT NULL,
  source_url TEXT NOT NULL DEFAULT '',
  catalog_version BIGINT NOT NULL DEFAULT 0,
  payload_sha256 TEXT NOT NULL DEFAULT '',
  key_id TEXT NOT NULL DEFAULT '',
  signature TEXT NOT NULL DEFAULT '',
  plugin_count INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL,
  error TEXT NOT NULL DEFAULT '',
  payload_json TEXT NOT NULL DEFAULT '{}',
  synced_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS official_catalog_snapshots_synced_idx
  ON official_catalog_snapshots(synced_at DESC);
