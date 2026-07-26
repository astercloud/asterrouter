-- Representative v0.17.0 tables that contain records requiring upgrade.
-- The candidate runtime creates the remaining tables that were empty at upgrade time.
CREATE TABLE workspace_users (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active',
  role TEXT NOT NULL DEFAULT 'developer',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  external_issuer TEXT NOT NULL DEFAULT '',
  external_subject TEXT NOT NULL DEFAULT '',
  department_id TEXT NOT NULL DEFAULT '',
  totp_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  totp_secret_ciphertext TEXT NOT NULL DEFAULT '',
  totp_recovery_hashes TEXT NOT NULL DEFAULT '[]',
  password_hash TEXT NOT NULL DEFAULT '',
  email_verified BOOLEAN NOT NULL DEFAULT FALSE,
  email_verify_hash TEXT NOT NULL DEFAULT '',
  email_verify_expires_at TIMESTAMPTZ,
  password_reset_hash TEXT NOT NULL DEFAULT '',
  password_reset_expires_at TIMESTAMPTZ,
  balance_micros BIGINT NOT NULL DEFAULT 0,
  concurrency_limit INTEGER NOT NULL DEFAULT 5,
  rpm_limit INTEGER NOT NULL DEFAULT 0,
  avatar_data_url TEXT NOT NULL DEFAULT '',
  session_version BIGINT NOT NULL DEFAULT 1
);

CREATE UNIQUE INDEX workspace_users_external_identity_unique
  ON workspace_users(external_issuer, external_subject)
  WHERE external_issuer <> '' AND external_subject <> '';

CREATE TABLE platform_tenants (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  slug TEXT NOT NULL UNIQUE,
  entitlement_reference TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);
