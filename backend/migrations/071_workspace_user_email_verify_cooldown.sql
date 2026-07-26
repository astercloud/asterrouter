ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS email_verify_sent_at TIMESTAMPTZ;
