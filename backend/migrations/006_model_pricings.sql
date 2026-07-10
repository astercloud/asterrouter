CREATE TABLE IF NOT EXISTS model_pricings (
  id TEXT PRIMARY KEY,
  model TEXT NOT NULL UNIQUE,
  currency TEXT NOT NULL DEFAULT 'USD',
  input_price_cents_per_1m_tokens INTEGER NOT NULL DEFAULT 0,
  output_price_cents_per_1m_tokens INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);
