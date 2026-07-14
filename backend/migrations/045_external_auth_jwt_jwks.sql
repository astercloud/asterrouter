ALTER TABLE external_auth_integrations ADD COLUMN IF NOT EXISTS issuer TEXT NOT NULL DEFAULT '';
ALTER TABLE external_auth_integrations ADD COLUMN IF NOT EXISTS jwks_url TEXT NOT NULL DEFAULT '';
ALTER TABLE external_auth_integrations ADD COLUMN IF NOT EXISTS subject_claim TEXT NOT NULL DEFAULT '';
ALTER TABLE external_auth_integrations ALTER COLUMN subject_claim SET DEFAULT '';
ALTER TABLE external_auth_integrations ADD COLUMN IF NOT EXISTS models_claim TEXT NOT NULL DEFAULT '';
ALTER TABLE external_auth_integrations ADD COLUMN IF NOT EXISTS qps_limit_claim TEXT NOT NULL DEFAULT '';
ALTER TABLE external_auth_integrations ADD COLUMN IF NOT EXISTS monthly_token_limit_claim TEXT NOT NULL DEFAULT '';
