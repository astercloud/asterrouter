ALTER TABLE ai_operations ADD COLUMN IF NOT EXISTS artifact_policy TEXT NOT NULL DEFAULT 'proxy_only';
