CREATE TABLE IF NOT EXISTS billing_holds (
  id TEXT PRIMARY KEY,
  operation_id TEXT NOT NULL UNIQUE REFERENCES ai_operations(id) ON DELETE RESTRICT,
  profile_scope TEXT NOT NULL DEFAULT '',
  tenant_id TEXT NOT NULL DEFAULT '',
  credential_id TEXT NOT NULL,
  credential_source TEXT NOT NULL,
  integration_id TEXT NOT NULL DEFAULT '',
  principal_type TEXT NOT NULL DEFAULT '',
  principal_id TEXT NOT NULL DEFAULT '',
  external_subject_reference TEXT NOT NULL DEFAULT '',
  request_fingerprint TEXT NOT NULL,
  status TEXT NOT NULL,
  version INTEGER NOT NULL,
  reserved_amount_cents INTEGER NOT NULL DEFAULT 0,
  settled_amount_cents INTEGER NOT NULL DEFAULT 0,
  currency TEXT NOT NULL DEFAULT 'USD',
  price_snapshot_id TEXT NOT NULL DEFAULT '',
  estimate_source TEXT NOT NULL DEFAULT '',
  reason TEXT NOT NULL DEFAULT '',
  budget_period_start TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  settled_at TIMESTAMPTZ,
  released_at TIMESTAMPTZ,
  CHECK (status IN ('reserved', 'committed', 'settled', 'released', 'disputed')),
  CHECK (version > 0),
  CHECK (reserved_amount_cents >= 0),
  CHECK (settled_amount_cents >= 0),
  CHECK (char_length(currency) = 3)
);

CREATE INDEX IF NOT EXISTS billing_holds_budget_idx
  ON billing_holds(profile_scope, tenant_id, credential_id, budget_period_start, status);

CREATE INDEX IF NOT EXISTS billing_holds_expiry_idx
  ON billing_holds(status, expires_at);
