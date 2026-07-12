CREATE TABLE IF NOT EXISTS official_feed_sync_runs (
  id TEXT PRIMARY KEY,
  service_key TEXT NOT NULL,
  feed_id TEXT NOT NULL DEFAULT '',
  mode TEXT NOT NULL,
  status TEXT NOT NULL,
  request_id TEXT NOT NULL DEFAULT '',
  source_url TEXT NOT NULL DEFAULT '',
  error_code TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  started_at TIMESTAMPTZ NOT NULL,
  finished_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS official_feed_sync_runs_service_idx
  ON official_feed_sync_runs(service_key, started_at DESC);
