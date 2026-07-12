CREATE TABLE IF NOT EXISTS gateway_models (
  id TEXT PRIMARY KEY,
  model_id TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  modality TEXT NOT NULL DEFAULT 'chat',
  default_route_group TEXT NOT NULL DEFAULT 'default',
  sticky_enabled BOOLEAN NOT NULL DEFAULT false,
  sticky_ttl_seconds INTEGER NOT NULL DEFAULT 1800,
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS gateway_models_status_model_idx
  ON gateway_models(status, model_id);

CREATE TABLE IF NOT EXISTS model_routes (
  id TEXT PRIMARY KEY,
  gateway_model_id TEXT NOT NULL REFERENCES gateway_models(id) ON DELETE CASCADE,
  route_group TEXT NOT NULL DEFAULT 'default',
  provider_account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
  upstream_model TEXT NOT NULL,
  priority INTEGER NOT NULL DEFAULT 100,
  weight INTEGER NOT NULL DEFAULT 100,
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE(gateway_model_id, route_group, provider_account_id, upstream_model)
);

CREATE INDEX IF NOT EXISTS model_routes_resolution_idx
  ON model_routes(gateway_model_id, route_group, status, priority);

CREATE INDEX IF NOT EXISTS model_routes_account_idx
  ON model_routes(provider_account_id);
