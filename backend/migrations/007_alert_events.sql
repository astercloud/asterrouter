CREATE TABLE IF NOT EXISTS alert_events (
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  severity TEXT NOT NULL,
  status TEXT NOT NULL,
  title TEXT NOT NULL,
  summary TEXT NOT NULL,
  resource_type TEXT NOT NULL DEFAULT '',
  resource_id TEXT NOT NULL DEFAULT '',
  project_id TEXT NOT NULL DEFAULT '',
  dedupe_key TEXT NOT NULL UNIQUE,
  metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  first_seen_at TIMESTAMPTZ NOT NULL,
  last_seen_at TIMESTAMPTZ NOT NULL,
  acknowledged_at TIMESTAMPTZ,
  acknowledged_by TEXT NOT NULL DEFAULT '',
  resolved_at TIMESTAMPTZ,
  resolved_by TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS alert_events_status_last_seen_idx
  ON alert_events(status, last_seen_at DESC);

CREATE INDEX IF NOT EXISTS alert_events_resource_idx
  ON alert_events(resource_type, resource_id, last_seen_at DESC);

CREATE INDEX IF NOT EXISTS alert_events_project_idx
  ON alert_events(project_id, last_seen_at DESC);
