ALTER TABLE platform_tenants
  ADD COLUMN IF NOT EXISTS concurrency_limit INTEGER NOT NULL DEFAULT 0;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conname = 'platform_tenants_concurrency_limit_non_negative'
      AND conrelid = 'platform_tenants'::regclass
  ) THEN
    ALTER TABLE platform_tenants
      ADD CONSTRAINT platform_tenants_concurrency_limit_non_negative
      CHECK (concurrency_limit >= 0);
  END IF;
END $$;
