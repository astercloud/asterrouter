CREATE TABLE IF NOT EXISTS official_license_snapshots (
  license_id TEXT NOT NULL,
  customer_id TEXT NOT NULL DEFAULT '',
  instance_id TEXT NOT NULL DEFAULT '',
  snapshot_version BIGINT NOT NULL DEFAULT 0,
  status TEXT NOT NULL,
  edition TEXT NOT NULL DEFAULT '',
  key_id TEXT NOT NULL DEFAULT '',
  envelope_sha256 TEXT PRIMARY KEY,
  envelope_json TEXT NOT NULL,
  activation_secret_ciphertext TEXT NOT NULL DEFAULT '',
  activation_secret_hint TEXT NOT NULL DEFAULT '',
  entitlements_json TEXT NOT NULL DEFAULT '[]',
  issued_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  imported_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  error TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS official_license_snapshots_imported_idx
  ON official_license_snapshots(imported_at DESC);
