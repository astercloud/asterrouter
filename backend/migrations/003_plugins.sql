CREATE TABLE IF NOT EXISTS plugins (
  id TEXT PRIMARY KEY,
  plugin_id TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  category TEXT NOT NULL,
  type TEXT NOT NULL,
  tier TEXT NOT NULL,
  version TEXT NOT NULL,
  vendor TEXT NOT NULL,
  status TEXT NOT NULL,
  entitlement_status TEXT NOT NULL,
  surfaces TEXT NOT NULL DEFAULT '[]',
  entry_point TEXT NOT NULL DEFAULT '',
  configurable BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);
