ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS external_issuer TEXT NOT NULL DEFAULT '';
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS external_subject TEXT NOT NULL DEFAULT '';
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS department_id TEXT NOT NULL DEFAULT '';
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS totp_enabled BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS totp_secret_ciphertext TEXT NOT NULL DEFAULT '';
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS totp_recovery_hashes TEXT NOT NULL DEFAULT '[]';
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS password_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS email_verified BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS email_verify_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS email_verify_expires_at TIMESTAMPTZ;
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS password_reset_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS password_reset_expires_at TIMESTAMPTZ;

CREATE UNIQUE INDEX IF NOT EXISTS workspace_users_external_identity_unique
ON workspace_users(external_issuer, external_subject)
WHERE external_issuer <> '' AND external_subject <> '';
