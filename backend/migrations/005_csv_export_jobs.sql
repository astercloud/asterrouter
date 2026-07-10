CREATE TABLE IF NOT EXISTS csv_export_jobs (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  status TEXT NOT NULL,
  filename TEXT NOT NULL,
  content_type TEXT NOT NULL,
  row_count INTEGER NOT NULL DEFAULT 0,
  size_bytes INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  parameters TEXT NOT NULL DEFAULT '{}',
  body BYTEA,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS csv_export_jobs_created_idx
  ON csv_export_jobs(created_at DESC);

CREATE INDEX IF NOT EXISTS csv_export_jobs_expires_idx
  ON csv_export_jobs(expires_at);
