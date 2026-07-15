ALTER TABLE effective_pricing_policies
  ADD COLUMN IF NOT EXISTS automatic_actions_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS evaluation_interval_minutes INTEGER NOT NULL DEFAULT 60,
  ADD COLUMN IF NOT EXISTS promotion_window_count INTEGER NOT NULL DEFAULT 3,
  ADD COLUMN IF NOT EXISTS degradation_window_count INTEGER NOT NULL DEFAULT 2;

ALTER TABLE effective_pricing_decisions
  ADD COLUMN IF NOT EXISTS healthy_window_count INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS degraded_window_count INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS last_evaluation_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS last_evaluation_verdict TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS last_evaluation_reason_codes TEXT NOT NULL DEFAULT '[]',
  ADD COLUMN IF NOT EXISTS last_evaluated_window_end TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS monitoring_started_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS last_healthy_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS last_automatic_action TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS effective_pricing_decision_evaluations (
  id TEXT PRIMARY KEY,
  decision_id TEXT NOT NULL REFERENCES effective_pricing_decisions(id) ON DELETE CASCADE,
  window_start TIMESTAMPTZ NOT NULL,
  window_end TIMESTAMPTZ NOT NULL,
  verdict TEXT NOT NULL,
  reason_codes TEXT NOT NULL DEFAULT '[]',
  current_snapshot_id TEXT NOT NULL DEFAULT '',
  candidate_snapshot_id TEXT NOT NULL DEFAULT '',
  current_request_count BIGINT NOT NULL DEFAULT 0,
  candidate_request_count BIGINT NOT NULL DEFAULT 0,
  current_cost_micros_per_1m BIGINT NOT NULL DEFAULT 0,
  candidate_cost_micros_per_1m BIGINT NOT NULL DEFAULT 0,
  cost_improvement DOUBLE PRECISION NOT NULL DEFAULT 0,
  current_cache_token_hit_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
  candidate_cache_token_hit_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
  current_cache_savings_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
  candidate_cache_savings_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
  current_affinity_consistency_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
  candidate_affinity_consistency_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
  current_error_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
  candidate_error_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
  current_p95_latency_ms BIGINT NOT NULL DEFAULT 0,
  candidate_p95_latency_ms BIGINT NOT NULL DEFAULT 0,
  current_metrics_coverage DOUBLE PRECISION NOT NULL DEFAULT 0,
  candidate_metrics_coverage DOUBLE PRECISION NOT NULL DEFAULT 0,
  current_billing_consistency_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
  candidate_billing_consistency_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
  automatic_action TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  UNIQUE(decision_id, window_end)
);

CREATE INDEX IF NOT EXISTS effective_pricing_decision_evaluations_lookup_idx
  ON effective_pricing_decision_evaluations(decision_id, window_end DESC);
