ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS customer_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS usage_records_customer_created_idx ON usage_records(customer_id, created_at DESC) WHERE customer_id <> '';
