ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS password_reset_sent_at TIMESTAMPTZ;
