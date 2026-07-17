ALTER TABLE provider_accounts
  ADD COLUMN IF NOT EXISTS adapter_config JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE model_routes
  ADD COLUMN IF NOT EXISTS upstream_format TEXT NOT NULL DEFAULT '';

ALTER TABLE model_routes
  ADD COLUMN IF NOT EXISTS disabled_reason TEXT NOT NULL DEFAULT '';

DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = current_schema()
      AND table_name = 'provider_connections'
      AND column_name IN ('secret_configured', 'secret_hint', 'secret_ciphertext')
    GROUP BY table_name
    HAVING COUNT(*) = 3
  ) THEN
    EXECUTE $migration$
      UPDATE provider_accounts AS account
      SET secret_configured = provider.secret_configured,
          secret_hint = provider.secret_hint,
          secret_ciphertext = provider.secret_ciphertext
      FROM provider_connections AS provider
      WHERE account.provider_id = provider.id
        AND account.secret_ciphertext = ''
        AND provider.secret_ciphertext <> ''
    $migration$;
  END IF;
END $$;

UPDATE provider_connections SET type = 'anthropic_compatible' WHERE type = 'anthropic';
UPDATE provider_connections SET type = 'gemini_compatible' WHERE type = 'gemini';
UPDATE provider_connections SET type = 'openai_compatible' WHERE type = 'self_hosted';

UPDATE model_routes AS route
SET upstream_format = CASE
  WHEN gateway_model.modality IN ('image','video','multimodal') AND provider.type IN ('openai_compatible','anthropic_compatible','gemini_compatible','aws_bedrock','gcp_vertex','azure_openai') THEN 'native_media'
  WHEN gateway_model.modality = 'audio' AND provider.type = 'openai_compatible' THEN 'native_media'
  WHEN gateway_model.modality = 'chat' THEN CASE provider.type
    WHEN 'anthropic_compatible' THEN 'anthropic_messages'
    WHEN 'gemini_compatible' THEN 'gemini_generate_content'
    WHEN 'aws_bedrock' THEN 'bedrock_converse'
    WHEN 'azure_openai' THEN 'openai_chat'
    WHEN 'openai_compatible' THEN 'openai_chat'
    ELSE ''
  END
  ELSE ''
END
FROM provider_accounts AS account
JOIN provider_connections AS provider ON provider.id = account.provider_id
CROSS JOIN gateway_models AS gateway_model
WHERE route.provider_account_id = account.id
  AND gateway_model.id = route.gateway_model_id
  AND route.upstream_format = '';

UPDATE model_routes
SET status = 'disabled', disabled_reason = 'migration_requires_explicit_upstream_format'
WHERE upstream_format = '';

ALTER TABLE provider_connections DROP COLUMN IF EXISTS models;
ALTER TABLE provider_connections DROP COLUMN IF EXISTS secret_configured;
ALTER TABLE provider_connections DROP COLUMN IF EXISTS secret_hint;
ALTER TABLE provider_connections DROP COLUMN IF EXISTS secret_ciphertext;
ALTER TABLE provider_health_checks DROP COLUMN IF EXISTS models;
