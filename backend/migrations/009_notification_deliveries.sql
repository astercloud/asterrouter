CREATE TABLE IF NOT EXISTS notification_deliveries (
  id TEXT PRIMARY KEY,
  plugin_id TEXT NOT NULL,
  alert_id TEXT NOT NULL,
  alert_type TEXT NOT NULL,
  alert_severity TEXT NOT NULL,
  status TEXT NOT NULL,
  target TEXT NOT NULL DEFAULT '',
  http_status INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS notification_deliveries_plugin_created_idx
  ON notification_deliveries(plugin_id, created_at DESC);

CREATE INDEX IF NOT EXISTS notification_deliveries_alert_idx
  ON notification_deliveries(alert_id, created_at DESC);
