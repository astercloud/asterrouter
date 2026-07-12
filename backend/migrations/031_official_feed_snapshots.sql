CREATE TABLE IF NOT EXISTS official_feed_snapshots (
  service_key TEXT NOT NULL,
  feed_id TEXT NOT NULL,
  feed_version TEXT NOT NULL,
  data_schema_version TEXT NOT NULL,
  status TEXT NOT NULL,
  signature_verified BOOLEAN NOT NULL DEFAULT false,
  payload_sha256 TEXT NOT NULL,
  size_bytes BIGINT NOT NULL,
  payload_ciphertext TEXT NOT NULL,
  envelope_json TEXT NOT NULL,
  issued_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  imported_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY(service_key, feed_id)
);

CREATE INDEX IF NOT EXISTS official_feed_snapshots_service_idx
  ON official_feed_snapshots(service_key, imported_at DESC);
