CREATE TABLE IF NOT EXISTS routing_policies (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  route_group TEXT NOT NULL DEFAULT 'default',
  status TEXT NOT NULL DEFAULT 'active',
  is_default BOOLEAN NOT NULL DEFAULT false,
  strategy JSONB NOT NULL DEFAULT '{}'::jsonb,
  version INTEGER NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  CONSTRAINT routing_policies_name_check CHECK (btrim(name) <> ''),
  CONSTRAINT routing_policies_route_group_check CHECK (btrim(route_group) <> '' AND route_group !~ '[ :\t\r\n]'),
  CONSTRAINT routing_policies_status_check CHECK (status IN ('active', 'disabled')),
  CONSTRAINT routing_policies_strategy_object_check CHECK (jsonb_typeof(strategy) = 'object'),
  CONSTRAINT routing_policies_version_check CHECK (version > 0)
);

DO $$ BEGIN
  ALTER TABLE routing_policies ADD CONSTRAINT routing_policies_name_check CHECK (btrim(name) <> '');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
  ALTER TABLE routing_policies ADD CONSTRAINT routing_policies_route_group_check CHECK (btrim(route_group) <> '' AND route_group !~ '[ :\t\r\n]');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
  ALTER TABLE routing_policies ADD CONSTRAINT routing_policies_status_check CHECK (status IN ('active', 'disabled'));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
  ALTER TABLE routing_policies ADD CONSTRAINT routing_policies_strategy_object_check CHECK (jsonb_typeof(strategy) = 'object');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
  ALTER TABLE routing_policies ADD CONSTRAINT routing_policies_version_check CHECK (version > 0);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE INDEX IF NOT EXISTS routing_policies_route_group_idx
  ON routing_policies(route_group, status);

CREATE UNIQUE INDEX IF NOT EXISTS routing_policies_one_default_per_group_idx
  ON routing_policies(route_group) WHERE status = 'active' AND is_default;
