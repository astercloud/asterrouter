ALTER TABLE usage_records
  ADD COLUMN IF NOT EXISTS usage_dimensions JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE billing_holds
  ADD COLUMN IF NOT EXISTS reserved_usage_dimensions JSONB NOT NULL DEFAULT '{}'::jsonb;
