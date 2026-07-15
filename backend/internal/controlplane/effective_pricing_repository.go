package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"sort"
	"strings"
	"time"
)

func (r *MemoryRepository) ListProcurementPrices(context.Context) ([]ProcurementPrice, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProcurementPrice, 0, len(r.procurementPrices))
	for _, price := range r.procurementPrices {
		out = append(out, price)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ProviderAccountID == out[j].ProviderAccountID {
			if out[i].UpstreamModel == out[j].UpstreamModel {
				return out[i].EffectiveFrom.After(out[j].EffectiveFrom)
			}
			return out[i].UpstreamModel < out[j].UpstreamModel
		}
		return out[i].ProviderAccountID < out[j].ProviderAccountID
	})
	return out, nil
}

func (r *MemoryRepository) SaveProcurementPrice(_ context.Context, price ProcurementPrice) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.procurementPrices[price.ID] = price
	return nil
}

func (r *MemoryRepository) ListProviderBillingLines(context.Context) ([]ProviderBillingLine, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProviderBillingLine, 0, len(r.providerBillingLines))
	for _, line := range r.providerBillingLines {
		out = append(out, line)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (r *MemoryRepository) SaveProviderBillingLine(_ context.Context, line ProviderBillingLine) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providerBillingLines[line.ID] = line
	return nil
}

func (r *MemoryRepository) SaveProviderBillingLineAndReconcileUsage(_ context.Context, line ProviderBillingLine, record UsageRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, found := r.usageRecords[record.ID]; !found {
		return errors.New("usage record not found")
	}
	r.providerBillingLines[line.ID] = line
	r.usageRecords[record.ID] = record
	return nil
}

func (r *MemoryRepository) ListProviderCacheCapabilities(context.Context) ([]ProviderCacheCapability, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProviderCacheCapability, 0, len(r.providerCacheCapabilities))
	for _, capability := range r.providerCacheCapabilities {
		out = append(out, capability)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ProviderAccountID == out[j].ProviderAccountID {
			return out[i].UpstreamModel < out[j].UpstreamModel
		}
		return out[i].ProviderAccountID < out[j].ProviderAccountID
	})
	return out, nil
}

func (r *MemoryRepository) FindProviderCacheCapability(_ context.Context, providerAccountID, upstreamModel, protocol string) (ProviderCacheCapability, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, capability := range r.providerCacheCapabilities {
		if capability.ProviderAccountID == providerAccountID && capability.UpstreamModel == upstreamModel && capability.Protocol == protocol {
			return capability, true, nil
		}
	}
	return ProviderCacheCapability{}, false, nil
}

func (r *MemoryRepository) SaveProviderCacheCapability(_ context.Context, capability ProviderCacheCapability) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providerCacheCapabilities[capability.ID] = capability
	return nil
}

func (r *MemoryRepository) UpsertProviderCacheProductionMetrics(_ context.Context, metrics ProviderCacheProductionMetrics) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	capability := applyProviderCacheProductionMetrics(r.providerCacheCapabilities[metrics.ID], metrics)
	r.providerCacheCapabilities[metrics.ID] = capability
	return nil
}

func (r *MemoryRepository) ListProviderCacheProbeRuns(_ context.Context, limit int) ([]ProviderCacheProbeRun, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProviderCacheProbeRun, 0, len(r.providerCacheProbeRuns))
	for _, run := range r.providerCacheProbeRuns {
		out = append(out, run)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	limit, _ = normalizeListWindow(limit, 0, 100, 500)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *MemoryRepository) ReserveProviderCacheProbeRun(_ context.Context, run ProviderCacheProbeRun, limits CacheProbeReservationLimits) (bool, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var reservedTokens, reservedCost int64
	for _, current := range r.providerCacheProbeRuns {
		if current.Status == CacheProbeStatusSkipped {
			continue
		}
		if current.Status == CacheProbeStatusRunning && current.StartedAt.After(limits.Now.Add(-limits.StaleAfter)) {
			return false, "probe_concurrency_limit", nil
		}
		if !current.StartedAt.Before(limits.DayStart) {
			reservedTokens += current.PrefixTokens * 3
			reservedCost += current.EstimatedCostMicros
		}
		if current.ProviderAccountID == run.ProviderAccountID && current.UpstreamModel == run.UpstreamModel && current.Protocol == run.Protocol && current.StartedAt.After(limits.Now.Add(-limits.Cooldown)) {
			return false, "probe_cooldown_active", nil
		}
	}
	if limits.DailyTokenBudget <= 0 || run.PrefixTokens > (limits.DailyTokenBudget-reservedTokens)/3 {
		return false, "probe_daily_token_budget_exceeded", nil
	}
	if limits.DailyCostBudgetMicros <= 0 || run.EstimatedCostMicros > limits.DailyCostBudgetMicros-reservedCost {
		return false, "probe_daily_cost_budget_exceeded", nil
	}
	r.providerCacheProbeRuns[run.ID] = run
	return true, "", nil
}

func (r *MemoryRepository) SaveProviderCacheProbeRun(_ context.Context, run ProviderCacheProbeRun) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providerCacheProbeRuns[run.ID] = run
	return nil
}

func (r *MemoryRepository) GetEffectivePricingPolicy(context.Context) (EffectivePricingPolicy, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	policy, ok := r.effectivePricingPolicies[defaultEffectivePricingPolicyID]
	return policy, ok, nil
}

func (r *MemoryRepository) SaveEffectivePricingPolicy(_ context.Context, policy EffectivePricingPolicy) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.effectivePricingPolicies[policy.ID] = policy
	return nil
}

func (r *MemoryRepository) ListEffectivePriceSnapshots(context.Context) ([]EffectivePriceSnapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]EffectivePriceSnapshot, 0, len(r.effectivePriceSnapshots))
	for _, snapshot := range r.effectivePriceSnapshots {
		out = append(out, snapshot)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ProviderAccountID == out[j].ProviderAccountID {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ProviderAccountID < out[j].ProviderAccountID
	})
	return out, nil
}

func (r *MemoryRepository) SaveEffectivePriceSnapshot(_ context.Context, snapshot EffectivePriceSnapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.effectivePriceSnapshots[snapshot.ID] = snapshot
	return nil
}

func (r *MemoryRepository) ListEffectivePricingDecisions(context.Context) ([]EffectivePricingDecision, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]EffectivePricingDecision, 0, len(r.effectivePricingDecisions))
	for _, decision := range r.effectivePricingDecisions {
		decision.ReasonCodes = append([]string(nil), decision.ReasonCodes...)
		decision.LastEvaluationReasonCodes = append([]string(nil), decision.LastEvaluationReasonCodes...)
		out = append(out, decision)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (r *MemoryRepository) SaveEffectivePricingDecision(_ context.Context, decision EffectivePricingDecision) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	decision.ReasonCodes = append([]string(nil), decision.ReasonCodes...)
	decision.LastEvaluationReasonCodes = append([]string(nil), decision.LastEvaluationReasonCodes...)
	r.effectivePricingDecisions[decision.ID] = decision
	return nil
}

func (r *MemoryRepository) UpdateEffectivePricingDecision(_ context.Context, decision EffectivePricingDecision, expectedStatus string, expectedUpdatedAt time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, found := r.effectivePricingDecisions[decision.ID]
	if !found || current.Status != expectedStatus || !current.UpdatedAt.Equal(expectedUpdatedAt) {
		return false, nil
	}
	decision.ReasonCodes = append([]string(nil), decision.ReasonCodes...)
	decision.LastEvaluationReasonCodes = append([]string(nil), decision.LastEvaluationReasonCodes...)
	r.effectivePricingDecisions[decision.ID] = decision
	return true, nil
}

func (r *MemoryRepository) ListEffectivePricingDecisionEvaluations(_ context.Context, decisionID string, limit int) ([]EffectivePricingDecisionEvaluation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	decisionID = strings.TrimSpace(decisionID)
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	out := make([]EffectivePricingDecisionEvaluation, 0, min(limit, len(r.effectivePricingEvaluations)))
	for _, evaluation := range r.effectivePricingEvaluations {
		if decisionID != "" && evaluation.DecisionID != decisionID {
			continue
		}
		evaluation.ReasonCodes = append([]string(nil), evaluation.ReasonCodes...)
		out = append(out, evaluation)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WindowEnd.After(out[j].WindowEnd) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *MemoryRepository) CommitEffectivePricingDecisionEvaluation(_ context.Context, commit EffectivePricingDecisionEvaluationCommit) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, found := r.effectivePricingDecisions[commit.Decision.ID]
	if !found || current.Status != commit.ExpectedStatus || !current.UpdatedAt.Equal(commit.ExpectedUpdatedAt) {
		return false, nil
	}
	for _, evaluation := range r.effectivePricingEvaluations {
		if evaluation.DecisionID == commit.Evaluation.DecisionID && evaluation.WindowEnd.Equal(commit.Evaluation.WindowEnd) {
			return false, nil
		}
	}
	if commit.CurrentSnapshot != nil {
		r.effectivePriceSnapshots[commit.CurrentSnapshot.ID] = *commit.CurrentSnapshot
	}
	if commit.CandidateSnapshot != nil {
		r.effectivePriceSnapshots[commit.CandidateSnapshot.ID] = *commit.CandidateSnapshot
	}
	evaluation := commit.Evaluation
	evaluation.ReasonCodes = append([]string(nil), evaluation.ReasonCodes...)
	r.effectivePricingEvaluations[evaluation.ID] = evaluation
	decision := commit.Decision
	decision.ReasonCodes = append([]string(nil), decision.ReasonCodes...)
	decision.LastEvaluationReasonCodes = append([]string(nil), decision.LastEvaluationReasonCodes...)
	r.effectivePricingDecisions[decision.ID] = decision
	if commit.Audit != nil {
		r.auditLogs[commit.Audit.ID] = *commit.Audit
	}
	return true, nil
}

func (r *MemoryRepository) FindRoutingAffinityBinding(_ context.Context, scopeKey string, now time.Time) (RoutingAffinityBinding, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	binding, ok := r.routingAffinityBindings[scopeKey]
	if ok && !now.Before(binding.ExpiresAt) {
		delete(r.routingAffinityBindings, scopeKey)
		return RoutingAffinityBinding{}, false, nil
	}
	return binding, ok, nil
}

func (r *MemoryRepository) SaveRoutingAffinityBinding(_ context.Context, binding RoutingAffinityBinding) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routingAffinityBindings[binding.ScopeKey] = binding
	return nil
}

func (r *MemoryRepository) DeleteRoutingAffinityBinding(_ context.Context, scopeKey string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.routingAffinityBindings, scopeKey)
	return nil
}

func (r *MemoryRepository) SummarizeEffectivePricingUsage(_ context.Context, from, to time.Time) ([]EffectivePricingUsageAggregate, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	byKey := map[string]*EffectivePricingUsageAggregate{}
	latenciesByKey := map[string][]int64{}
	for _, record := range r.usageRecords {
		if (!from.IsZero() && record.CreatedAt.Before(from)) || (!to.IsZero() && record.CreatedAt.After(to)) || record.ProviderAccountID == "" {
			continue
		}
		protocol := record.Protocol
		key := record.ProviderAccountID + "\x00" + record.UpstreamModel + "\x00" + protocol
		aggregate := byKey[key]
		if aggregate == nil {
			aggregate = &EffectivePricingUsageAggregate{ProviderID: record.ProviderID, ProviderAccountID: record.ProviderAccountID, UpstreamModel: record.UpstreamModel, Protocol: protocol}
			byKey[key] = aggregate
		}
		aggregate.RequestCount++
		failed := record.ErrorType != "" || record.Status == "error" || record.Status == "upstream_error"
		if failed {
			aggregate.ErrorCount++
		} else {
			aggregate.SuccessfulRequestCount++
			if record.LatencyMS > 0 {
				latenciesByKey[key] = append(latenciesByKey[key], record.LatencyMS)
			}
		}
		if !failed && record.CacheFieldsPresent {
			aggregate.CacheMetricsRequestCount++
			if valueOr(record.CacheReadTokens, 0) > 0 {
				aggregate.CacheHitRequestCount++
			}
			if aggregate.LastCacheObservedAt == nil || record.CreatedAt.After(*aggregate.LastCacheObservedAt) {
				observedAt := record.CreatedAt
				aggregate.LastCacheObservedAt = &observedAt
			}
		}
		aggregate.TotalInputTokens += int64(valueOr(record.TotalInputTokens, record.InputTokens))
		aggregate.UncachedInputTokens += int64(valueOr(record.UncachedInputTokens, 0))
		aggregate.CacheReadTokens += int64(valueOr(record.CacheReadTokens, 0))
		aggregate.CacheWrite5mTokens += int64(valueOr(record.CacheWrite5mTokens, 0))
		aggregate.CacheWrite1hTokens += int64(valueOr(record.CacheWrite1hTokens, 0))
		aggregate.OutputTokens += int64(record.OutputTokens)
		aggregate.LatencyTotalMS += record.LatencyMS
		if record.ProcurementCostMicros != nil {
			aggregate.ProcurementCostMicros += *record.ProcurementCostMicros
			aggregate.ProcurementCostRecordCount++
		}
	}
	out := make([]EffectivePricingUsageAggregate, 0, len(byKey))
	for key, aggregate := range byKey {
		aggregate.P95LatencyMS = nearestRankPercentile(latenciesByKey[key], 0.95)
		out = append(out, *aggregate)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ProviderAccountID == out[j].ProviderAccountID {
			return out[i].UpstreamModel < out[j].UpstreamModel
		}
		return out[i].ProviderAccountID < out[j].ProviderAccountID
	})
	return out, nil
}

func nearestRankPercentile(values []int64, percentile float64) int64 {
	if len(values) == 0 || percentile <= 0 || percentile > 1 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	index := int(math.Ceil(percentile*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	return sorted[index]
}

func (r *PostgresRepository) ListProcurementPrices(ctx context.Context) ([]ProcurementPrice, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, provider_id, provider_account_id, upstream_model, protocol, currency,
       uncached_input_micros_per_1m_tokens, cache_read_micros_per_1m_tokens,
       cache_write_5m_micros_per_1m_tokens, cache_write_1h_micros_per_1m_tokens,
       output_micros_per_1m_tokens, request_micros, reference_input_micros_per_1m_tokens,
       reference_output_micros_per_1m_tokens, quoted_multiplier, recharge_multiplier,
       source_kind, source_reference, evidence_hash, confidence, status,
       effective_from, expires_at, created_at, updated_at
FROM procurement_prices
ORDER BY provider_account_id, upstream_model, effective_from DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProcurementPrice{}
	for rows.Next() {
		var price ProcurementPrice
		if err := rows.Scan(&price.ID, &price.ProviderID, &price.ProviderAccountID, &price.UpstreamModel, &price.Protocol, &price.Currency,
			&price.UncachedInputMicrosPer1MTokens, &price.CacheReadMicrosPer1MTokens, &price.CacheWrite5mMicrosPer1MTokens, &price.CacheWrite1hMicrosPer1MTokens,
			&price.OutputMicrosPer1MTokens, &price.RequestMicros, &price.ReferenceInputMicrosPer1MTokens, &price.ReferenceOutputMicrosPer1MTokens,
			&price.QuotedMultiplier, &price.RechargeMultiplier, &price.SourceKind, &price.SourceReference, &price.EvidenceHash, &price.Confidence,
			&price.Status, &price.EffectiveFrom, &price.ExpiresAt, &price.CreatedAt, &price.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, price)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveProcurementPrice(ctx context.Context, price ProcurementPrice) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO procurement_prices(
  id, provider_id, provider_account_id, upstream_model, protocol, currency,
  uncached_input_micros_per_1m_tokens, cache_read_micros_per_1m_tokens,
  cache_write_5m_micros_per_1m_tokens, cache_write_1h_micros_per_1m_tokens,
  output_micros_per_1m_tokens, request_micros, reference_input_micros_per_1m_tokens,
  reference_output_micros_per_1m_tokens, quoted_multiplier, recharge_multiplier,
  source_kind, source_reference, evidence_hash, confidence, status,
  effective_from, expires_at, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25)
ON CONFLICT(id) DO UPDATE SET
  provider_id=EXCLUDED.provider_id, provider_account_id=EXCLUDED.provider_account_id,
  upstream_model=EXCLUDED.upstream_model, protocol=EXCLUDED.protocol, currency=EXCLUDED.currency,
  uncached_input_micros_per_1m_tokens=EXCLUDED.uncached_input_micros_per_1m_tokens,
  cache_read_micros_per_1m_tokens=EXCLUDED.cache_read_micros_per_1m_tokens,
  cache_write_5m_micros_per_1m_tokens=EXCLUDED.cache_write_5m_micros_per_1m_tokens,
  cache_write_1h_micros_per_1m_tokens=EXCLUDED.cache_write_1h_micros_per_1m_tokens,
  output_micros_per_1m_tokens=EXCLUDED.output_micros_per_1m_tokens,
  request_micros=EXCLUDED.request_micros,
  reference_input_micros_per_1m_tokens=EXCLUDED.reference_input_micros_per_1m_tokens,
  reference_output_micros_per_1m_tokens=EXCLUDED.reference_output_micros_per_1m_tokens,
  quoted_multiplier=EXCLUDED.quoted_multiplier, recharge_multiplier=EXCLUDED.recharge_multiplier,
  source_kind=EXCLUDED.source_kind, source_reference=EXCLUDED.source_reference,
  evidence_hash=EXCLUDED.evidence_hash, confidence=EXCLUDED.confidence, status=EXCLUDED.status,
  effective_from=EXCLUDED.effective_from, expires_at=EXCLUDED.expires_at, updated_at=EXCLUDED.updated_at`,
		price.ID, price.ProviderID, price.ProviderAccountID, price.UpstreamModel, price.Protocol, price.Currency,
		price.UncachedInputMicrosPer1MTokens, price.CacheReadMicrosPer1MTokens, price.CacheWrite5mMicrosPer1MTokens, price.CacheWrite1hMicrosPer1MTokens,
		price.OutputMicrosPer1MTokens, price.RequestMicros, price.ReferenceInputMicrosPer1MTokens, price.ReferenceOutputMicrosPer1MTokens,
		price.QuotedMultiplier, price.RechargeMultiplier, price.SourceKind, price.SourceReference, price.EvidenceHash, price.Confidence,
		price.Status, price.EffectiveFrom, price.ExpiresAt, price.CreatedAt, price.UpdatedAt)
	return err
}

func (r *PostgresRepository) ListProviderBillingLines(ctx context.Context) ([]ProviderBillingLine, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, provider_id, provider_account_id, external_line_id, external_request_id,
       usage_record_id, upstream_model, currency, amount_micros, input_cost_micros,
       output_cost_micros, cache_read_cost_micros, cache_write_cost_micros,
       source_kind, confidence, reconciliation_status, raw_payload_hash,
       usage_started_at, usage_ended_at, created_at, updated_at
FROM provider_billing_lines ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProviderBillingLine{}
	for rows.Next() {
		var line ProviderBillingLine
		if err := rows.Scan(&line.ID, &line.ProviderID, &line.ProviderAccountID, &line.ExternalLineID, &line.ExternalRequestID,
			&line.UsageRecordID, &line.UpstreamModel, &line.Currency, &line.AmountMicros, &line.InputCostMicros,
			&line.OutputCostMicros, &line.CacheReadCostMicros, &line.CacheWriteCostMicros, &line.SourceKind,
			&line.Confidence, &line.ReconciliationStatus, &line.RawPayloadHash, &line.UsageStartedAt, &line.UsageEndedAt,
			&line.CreatedAt, &line.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, line)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveProviderBillingLine(ctx context.Context, line ProviderBillingLine) error {
	return saveProviderBillingLine(ctx, r.db, line)
}

func saveProviderBillingLine(ctx context.Context, executor usageRecordExecutor, line ProviderBillingLine) error {
	_, err := executor.ExecContext(ctx, `
INSERT INTO provider_billing_lines(
  id, provider_id, provider_account_id, external_line_id, external_request_id,
  usage_record_id, upstream_model, currency, amount_micros, input_cost_micros,
  output_cost_micros, cache_read_cost_micros, cache_write_cost_micros,
  source_kind, confidence, reconciliation_status, raw_payload_hash,
  usage_started_at, usage_ended_at, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
ON CONFLICT(id) DO UPDATE SET
  provider_id=EXCLUDED.provider_id, provider_account_id=EXCLUDED.provider_account_id,
  external_line_id=EXCLUDED.external_line_id, external_request_id=EXCLUDED.external_request_id,
  usage_record_id=EXCLUDED.usage_record_id, upstream_model=EXCLUDED.upstream_model,
  currency=EXCLUDED.currency, amount_micros=EXCLUDED.amount_micros,
  input_cost_micros=EXCLUDED.input_cost_micros, output_cost_micros=EXCLUDED.output_cost_micros,
  cache_read_cost_micros=EXCLUDED.cache_read_cost_micros, cache_write_cost_micros=EXCLUDED.cache_write_cost_micros,
  source_kind=EXCLUDED.source_kind, confidence=EXCLUDED.confidence,
  reconciliation_status=EXCLUDED.reconciliation_status, raw_payload_hash=EXCLUDED.raw_payload_hash,
  usage_started_at=EXCLUDED.usage_started_at, usage_ended_at=EXCLUDED.usage_ended_at,
  updated_at=EXCLUDED.updated_at`,
		line.ID, line.ProviderID, line.ProviderAccountID, line.ExternalLineID, line.ExternalRequestID,
		line.UsageRecordID, line.UpstreamModel, line.Currency, line.AmountMicros, line.InputCostMicros,
		line.OutputCostMicros, line.CacheReadCostMicros, line.CacheWriteCostMicros, line.SourceKind,
		line.Confidence, line.ReconciliationStatus, line.RawPayloadHash, line.UsageStartedAt, line.UsageEndedAt,
		line.CreatedAt, line.UpdatedAt)
	return err
}

func (r *PostgresRepository) SaveProviderBillingLineAndReconcileUsage(ctx context.Context, line ProviderBillingLine, record UsageRecord) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := saveProviderBillingLine(ctx, tx, line); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE usage_records SET
  procurement_cost_micros=$1, procurement_cost_currency=$2,
  procurement_cost_source=$3, procurement_cost_confidence=$4,
  provider_billing_line_id=$5
WHERE id=$6`, record.ProcurementCostMicros, record.ProcurementCostCurrency,
		record.ProcurementCostSource, record.ProcurementCostConfidence,
		record.ProviderBillingLineID, record.ID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return errors.New("usage record not found")
	}
	return tx.Commit()
}

func (r *PostgresRepository) ListProviderCacheCapabilities(ctx context.Context) ([]ProviderCacheCapability, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, provider_account_id, upstream_model, protocol, support_status,
       pool_affinity_grade, affinity_transport, affinity_field, cache_control_mode,
       usage_schema, metrics_coverage, eligible_request_hit_rate, cache_token_hit_rate,
       cache_write_read_ratio, affinity_consistency_rate, billing_consistency_rate,
       production_sample_count, probe_sample_count, degraded_reason,
       last_observed_at, last_verified_at, created_at, updated_at
FROM provider_cache_capabilities ORDER BY provider_account_id, upstream_model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProviderCacheCapability{}
	for rows.Next() {
		capability, err := scanProviderCacheCapability(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, capability)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) FindProviderCacheCapability(ctx context.Context, providerAccountID, upstreamModel, protocol string) (ProviderCacheCapability, bool, error) {
	capability, err := scanProviderCacheCapability(r.db.QueryRowContext(ctx, `
SELECT id, provider_account_id, upstream_model, protocol, support_status,
       pool_affinity_grade, affinity_transport, affinity_field, cache_control_mode,
       usage_schema, metrics_coverage, eligible_request_hit_rate, cache_token_hit_rate,
       cache_write_read_ratio, affinity_consistency_rate, billing_consistency_rate,
       production_sample_count, probe_sample_count, degraded_reason,
       last_observed_at, last_verified_at, created_at, updated_at
FROM provider_cache_capabilities
WHERE provider_account_id=$1 AND upstream_model=$2 AND protocol=$3`, providerAccountID, upstreamModel, protocol))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProviderCacheCapability{}, false, nil
		}
		return ProviderCacheCapability{}, false, err
	}
	return capability, true, nil
}

type providerCacheCapabilityScanner interface {
	Scan(dest ...any) error
}

func scanProviderCacheCapability(scanner providerCacheCapabilityScanner) (ProviderCacheCapability, error) {
	var capability ProviderCacheCapability
	err := scanner.Scan(&capability.ID, &capability.ProviderAccountID, &capability.UpstreamModel, &capability.Protocol,
		&capability.SupportStatus, &capability.PoolAffinityGrade, &capability.AffinityTransport, &capability.AffinityField,
		&capability.CacheControlMode, &capability.UsageSchema, &capability.MetricsCoverage, &capability.EligibleRequestHitRate,
		&capability.CacheTokenHitRate, &capability.CacheWriteReadRatio, &capability.AffinityConsistencyRate,
		&capability.BillingConsistencyRate, &capability.ProductionSampleCount, &capability.ProbeSampleCount,
		&capability.DegradedReason, &capability.LastObservedAt, &capability.LastVerifiedAt,
		&capability.CreatedAt, &capability.UpdatedAt)
	return capability, err
}

func (r *PostgresRepository) SaveProviderCacheCapability(ctx context.Context, capability ProviderCacheCapability) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO provider_cache_capabilities(
  id, provider_account_id, upstream_model, protocol, support_status,
  pool_affinity_grade, affinity_transport, affinity_field, cache_control_mode,
  usage_schema, metrics_coverage, eligible_request_hit_rate, cache_token_hit_rate,
  cache_write_read_ratio, affinity_consistency_rate, billing_consistency_rate,
  production_sample_count, probe_sample_count, degraded_reason,
  last_observed_at, last_verified_at, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)
ON CONFLICT(id) DO UPDATE SET
  provider_account_id=EXCLUDED.provider_account_id, upstream_model=EXCLUDED.upstream_model,
  protocol=EXCLUDED.protocol, support_status=EXCLUDED.support_status,
  pool_affinity_grade=EXCLUDED.pool_affinity_grade, affinity_transport=EXCLUDED.affinity_transport,
  affinity_field=EXCLUDED.affinity_field, cache_control_mode=EXCLUDED.cache_control_mode,
  usage_schema=EXCLUDED.usage_schema, metrics_coverage=EXCLUDED.metrics_coverage,
  eligible_request_hit_rate=EXCLUDED.eligible_request_hit_rate, cache_token_hit_rate=EXCLUDED.cache_token_hit_rate,
  cache_write_read_ratio=EXCLUDED.cache_write_read_ratio, affinity_consistency_rate=EXCLUDED.affinity_consistency_rate,
  billing_consistency_rate=EXCLUDED.billing_consistency_rate, production_sample_count=EXCLUDED.production_sample_count,
  probe_sample_count=EXCLUDED.probe_sample_count, degraded_reason=EXCLUDED.degraded_reason,
  last_observed_at=EXCLUDED.last_observed_at, last_verified_at=EXCLUDED.last_verified_at,
  updated_at=EXCLUDED.updated_at`,
		capability.ID, capability.ProviderAccountID, capability.UpstreamModel, capability.Protocol, capability.SupportStatus,
		capability.PoolAffinityGrade, capability.AffinityTransport, capability.AffinityField, capability.CacheControlMode,
		capability.UsageSchema, capability.MetricsCoverage, capability.EligibleRequestHitRate, capability.CacheTokenHitRate,
		capability.CacheWriteReadRatio, capability.AffinityConsistencyRate, capability.BillingConsistencyRate,
		capability.ProductionSampleCount, capability.ProbeSampleCount, capability.DegradedReason,
		capability.LastObservedAt, capability.LastVerifiedAt, capability.CreatedAt, capability.UpdatedAt)
	return err
}

func (r *PostgresRepository) UpsertProviderCacheProductionMetrics(ctx context.Context, metrics ProviderCacheProductionMetrics) error {
	supportStatus := CacheSupportUnknown
	var lastObservedAt *time.Time
	if metrics.MetricsObserved {
		supportStatus = CacheSupportAccepted
		lastObservedAt = &metrics.ObservedAt
	}
	if metrics.CacheActivityObserved {
		supportStatus = CacheSupportObserved
	}
	_, err := r.db.ExecContext(ctx, `
INSERT INTO provider_cache_capabilities(
  id, provider_account_id, upstream_model, protocol, support_status,
  pool_affinity_grade, affinity_transport, cache_control_mode, usage_schema,
  metrics_coverage, eligible_request_hit_rate, cache_token_hit_rate,
  cache_write_read_ratio, billing_consistency_rate, production_sample_count,
  last_observed_at, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$17)
ON CONFLICT(provider_account_id, upstream_model, protocol) DO UPDATE SET
  metrics_coverage=EXCLUDED.metrics_coverage,
  eligible_request_hit_rate=EXCLUDED.eligible_request_hit_rate,
  cache_token_hit_rate=EXCLUDED.cache_token_hit_rate,
  cache_write_read_ratio=EXCLUDED.cache_write_read_ratio,
  billing_consistency_rate=EXCLUDED.billing_consistency_rate,
  production_sample_count=EXCLUDED.production_sample_count,
	  support_status=CASE
	    WHEN EXCLUDED.support_status=$18 AND provider_cache_capabilities.support_status IN ($19,$20,$21)
	      THEN $18
	    WHEN EXCLUDED.support_status=$21 AND provider_cache_capabilities.support_status IN ($19,$20)
	      THEN $21
    ELSE provider_cache_capabilities.support_status
  END,
  last_observed_at=COALESCE(EXCLUDED.last_observed_at, provider_cache_capabilities.last_observed_at),
  updated_at=EXCLUDED.updated_at`,
		metrics.ID, metrics.ProviderAccountID, metrics.UpstreamModel, metrics.Protocol, supportStatus,
		PoolAffinityUnknown, AffinityTransportNone, "passthrough_if_present", "auto",
		metrics.MetricsCoverage, metrics.EligibleRequestHitRate, metrics.CacheTokenHitRate,
		metrics.CacheWriteReadRatio, metrics.BillingConsistencyRate, metrics.ProductionSampleCount,
		lastObservedAt, metrics.ObservedAt, CacheSupportObserved, CacheSupportUnknown, CacheSupportClaimed, CacheSupportAccepted)
	return err
}

func (r *PostgresRepository) ListProviderCacheProbeRuns(ctx context.Context, limit int) ([]ProviderCacheProbeRun, error) {
	limit, _ = normalizeListWindow(limit, 0, 100, 500)
	rows, err := r.db.QueryContext(ctx, `
SELECT id, provider_id, provider_account_id, upstream_model, protocol,
       probe_series_id, session_hash, prefix_fingerprint, prefix_tokens,
	       warm_cache_read_tokens, warm_cache_write_tokens, warm_ttft_ms, warm_upstream_request_id,
	       reuse_cache_read_tokens, reuse_cache_write_tokens, reuse_ttft_ms, reuse_upstream_request_id,
	       control_cache_read_tokens, control_cache_write_tokens, control_ttft_ms, control_upstream_request_id,
       cache_fields_present, estimated_cost_micros, status, failure_reason,
       started_at, finished_at
FROM provider_cache_probe_runs ORDER BY started_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProviderCacheProbeRun{}
	for rows.Next() {
		var run ProviderCacheProbeRun
		if err := rows.Scan(&run.ID, &run.ProviderID, &run.ProviderAccountID, &run.UpstreamModel, &run.Protocol,
			&run.ProbeSeriesID, &run.SessionHash, &run.PrefixFingerprint, &run.PrefixTokens,
			&run.WarmCacheReadTokens, &run.WarmCacheWriteTokens, &run.WarmTTFTMS, &run.WarmUpstreamRequestID,
			&run.ReuseCacheReadTokens, &run.ReuseCacheWriteTokens, &run.ReuseTTFTMS, &run.ReuseUpstreamRequestID,
			&run.ControlCacheReadTokens, &run.ControlCacheWriteTokens, &run.ControlTTFTMS, &run.ControlUpstreamRequestID,
			&run.CacheFieldsPresent, &run.EstimatedCostMicros, &run.Status, &run.FailureReason,
			&run.StartedAt, &run.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveProviderCacheProbeRun(ctx context.Context, run ProviderCacheProbeRun) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO provider_cache_probe_runs(
  id, provider_id, provider_account_id, upstream_model, protocol,
  probe_series_id, session_hash, prefix_fingerprint, prefix_tokens,
	  warm_cache_read_tokens, warm_cache_write_tokens, warm_ttft_ms, warm_upstream_request_id,
	  reuse_cache_read_tokens, reuse_cache_write_tokens, reuse_ttft_ms, reuse_upstream_request_id,
	  control_cache_read_tokens, control_cache_write_tokens, control_ttft_ms, control_upstream_request_id,
	  cache_fields_present, estimated_cost_micros, status, failure_reason,
	  started_at, finished_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27)
ON CONFLICT(id) DO UPDATE SET
  warm_cache_read_tokens=EXCLUDED.warm_cache_read_tokens,
	  warm_cache_write_tokens=EXCLUDED.warm_cache_write_tokens, warm_ttft_ms=EXCLUDED.warm_ttft_ms,
	  warm_upstream_request_id=EXCLUDED.warm_upstream_request_id,
	  reuse_cache_read_tokens=EXCLUDED.reuse_cache_read_tokens,
	  reuse_cache_write_tokens=EXCLUDED.reuse_cache_write_tokens, reuse_ttft_ms=EXCLUDED.reuse_ttft_ms,
	  reuse_upstream_request_id=EXCLUDED.reuse_upstream_request_id,
	  control_cache_read_tokens=EXCLUDED.control_cache_read_tokens,
	  control_cache_write_tokens=EXCLUDED.control_cache_write_tokens, control_ttft_ms=EXCLUDED.control_ttft_ms,
	  control_upstream_request_id=EXCLUDED.control_upstream_request_id,
  cache_fields_present=EXCLUDED.cache_fields_present, estimated_cost_micros=EXCLUDED.estimated_cost_micros,
  status=EXCLUDED.status, failure_reason=EXCLUDED.failure_reason, finished_at=EXCLUDED.finished_at`,
		run.ID, run.ProviderID, run.ProviderAccountID, run.UpstreamModel, run.Protocol,
		run.ProbeSeriesID, run.SessionHash, run.PrefixFingerprint, run.PrefixTokens,
		run.WarmCacheReadTokens, run.WarmCacheWriteTokens, run.WarmTTFTMS, run.WarmUpstreamRequestID,
		run.ReuseCacheReadTokens, run.ReuseCacheWriteTokens, run.ReuseTTFTMS, run.ReuseUpstreamRequestID,
		run.ControlCacheReadTokens, run.ControlCacheWriteTokens, run.ControlTTFTMS, run.ControlUpstreamRequestID,
		run.CacheFieldsPresent, run.EstimatedCostMicros, run.Status, run.FailureReason,
		run.StartedAt, run.FinishedAt)
	return err
}

func (r *PostgresRepository) ReserveProviderCacheProbeRun(ctx context.Context, run ProviderCacheProbeRun, limits CacheProbeReservationLimits) (reserved bool, reason string, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, "", err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var policyID string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM effective_pricing_policies WHERE id=$1 FOR UPDATE`, defaultEffectivePricingPolicyID).Scan(&policyID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			_ = tx.Rollback()
			return false, "probe_disabled", nil
		}
		return false, "", err
	}
	var activeRuns int
	if err = tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM provider_cache_probe_runs
WHERE status=$1 AND started_at>$2`, CacheProbeStatusRunning, limits.Now.Add(-limits.StaleAfter)).Scan(&activeRuns); err != nil {
		return false, "", err
	}
	if activeRuns > 0 {
		_ = tx.Rollback()
		return false, "probe_concurrency_limit", nil
	}
	var latestStartedAt time.Time
	err = tx.QueryRowContext(ctx, `
SELECT started_at FROM provider_cache_probe_runs
WHERE provider_account_id=$1 AND upstream_model=$2 AND protocol=$3 AND status<>$4
ORDER BY started_at DESC LIMIT 1`, run.ProviderAccountID, run.UpstreamModel, run.Protocol, CacheProbeStatusSkipped).Scan(&latestStartedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, "", err
	}
	if err == nil && latestStartedAt.After(limits.Now.Add(-limits.Cooldown)) {
		_ = tx.Rollback()
		return false, "probe_cooldown_active", nil
	}
	var reservedTokens, reservedCost int64
	if err = tx.QueryRowContext(ctx, `
SELECT COALESCE(SUM(prefix_tokens * 3), 0), COALESCE(SUM(estimated_cost_micros), 0)
FROM provider_cache_probe_runs WHERE started_at>=$1 AND status<>$2`, limits.DayStart, CacheProbeStatusSkipped).Scan(&reservedTokens, &reservedCost); err != nil {
		return false, "", err
	}
	if limits.DailyTokenBudget <= 0 || run.PrefixTokens > (limits.DailyTokenBudget-reservedTokens)/3 {
		_ = tx.Rollback()
		return false, "probe_daily_token_budget_exceeded", nil
	}
	if limits.DailyCostBudgetMicros <= 0 || run.EstimatedCostMicros > limits.DailyCostBudgetMicros-reservedCost {
		_ = tx.Rollback()
		return false, "probe_daily_cost_budget_exceeded", nil
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO provider_cache_probe_runs(
  id, provider_id, provider_account_id, upstream_model, protocol,
  probe_series_id, session_hash, prefix_fingerprint, prefix_tokens,
  warm_cache_read_tokens, warm_cache_write_tokens, warm_ttft_ms,
  reuse_cache_read_tokens, reuse_cache_write_tokens, reuse_ttft_ms,
  control_cache_read_tokens, control_cache_write_tokens, control_ttft_ms,
  cache_fields_present, estimated_cost_micros, status, failure_reason,
  started_at, finished_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,0,0,0,0,0,0,0,0,0,FALSE,$10,$11,'',$12,$12)`,
		run.ID, run.ProviderID, run.ProviderAccountID, run.UpstreamModel, run.Protocol,
		run.ProbeSeriesID, run.SessionHash, run.PrefixFingerprint, run.PrefixTokens,
		run.EstimatedCostMicros, CacheProbeStatusRunning, run.StartedAt)
	if err != nil {
		return false, "", err
	}
	if err = tx.Commit(); err != nil {
		return false, "", err
	}
	return true, "", nil
}

func (r *PostgresRepository) GetEffectivePricingPolicy(ctx context.Context) (EffectivePricingPolicy, bool, error) {
	var policy EffectivePricingPolicy
	err := r.db.QueryRowContext(ctx, `
	SELECT id, mode, window_hours, min_sample_count, min_metrics_coverage,
	       min_billing_consistency, min_cost_improvement, min_cache_hit_rate_improvement,
	       min_affinity_improvement, max_cache_tiebreak_cost_regression, max_error_rate_regression,
       max_p95_latency_regression, canary_percent, supplier_affinity_ttl_seconds,
       account_affinity_ttl_seconds, automatic_actions_enabled, evaluation_interval_minutes,
       promotion_window_count, degradation_window_count, probe_enabled, probe_daily_token_budget,
       probe_daily_cost_budget_micros, probe_cooldown_seconds, updated_by,
       created_at, updated_at
FROM effective_pricing_policies WHERE id=$1`, defaultEffectivePricingPolicyID).Scan(
		&policy.ID, &policy.Mode, &policy.WindowHours, &policy.MinSampleCount, &policy.MinMetricsCoverage,
		&policy.MinBillingConsistency, &policy.MinCostImprovement, &policy.MinCacheHitRateImprovement,
		&policy.MinAffinityImprovement, &policy.MaxCacheTiebreakCostRegression, &policy.MaxErrorRateRegression,
		&policy.MaxP95LatencyRegression, &policy.CanaryPercent, &policy.SupplierAffinityTTLSeconds,
		&policy.AccountAffinityTTLSeconds, &policy.AutomaticActionsEnabled, &policy.EvaluationIntervalMinutes,
		&policy.PromotionWindowCount, &policy.DegradationWindowCount, &policy.ProbeEnabled, &policy.ProbeDailyTokenBudget,
		&policy.ProbeDailyCostBudgetMicros, &policy.ProbeCooldownSeconds, &policy.UpdatedBy,
		&policy.CreatedAt, &policy.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return EffectivePricingPolicy{}, false, nil
		}
		return EffectivePricingPolicy{}, false, err
	}
	return policy, true, nil
}

func (r *PostgresRepository) SaveEffectivePricingPolicy(ctx context.Context, policy EffectivePricingPolicy) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO effective_pricing_policies(
	  id, mode, window_hours, min_sample_count, min_metrics_coverage,
	  min_billing_consistency, min_cost_improvement, min_cache_hit_rate_improvement,
	  min_affinity_improvement, max_cache_tiebreak_cost_regression, max_error_rate_regression,
  max_p95_latency_regression, canary_percent, supplier_affinity_ttl_seconds,
  account_affinity_ttl_seconds, automatic_actions_enabled, evaluation_interval_minutes,
  promotion_window_count, degradation_window_count, probe_enabled, probe_daily_token_budget,
  probe_daily_cost_budget_micros, probe_cooldown_seconds, updated_by,
  created_at, updated_at)
	VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26)
ON CONFLICT(id) DO UPDATE SET
  mode=EXCLUDED.mode, window_hours=EXCLUDED.window_hours,
  min_sample_count=EXCLUDED.min_sample_count, min_metrics_coverage=EXCLUDED.min_metrics_coverage,
	  min_billing_consistency=EXCLUDED.min_billing_consistency,
	  min_cost_improvement=EXCLUDED.min_cost_improvement,
	  min_cache_hit_rate_improvement=EXCLUDED.min_cache_hit_rate_improvement,
	  min_affinity_improvement=EXCLUDED.min_affinity_improvement,
	  max_cache_tiebreak_cost_regression=EXCLUDED.max_cache_tiebreak_cost_regression,
  max_error_rate_regression=EXCLUDED.max_error_rate_regression,
  max_p95_latency_regression=EXCLUDED.max_p95_latency_regression,
  canary_percent=EXCLUDED.canary_percent,
  supplier_affinity_ttl_seconds=EXCLUDED.supplier_affinity_ttl_seconds,
  account_affinity_ttl_seconds=EXCLUDED.account_affinity_ttl_seconds,
	automatic_actions_enabled=EXCLUDED.automatic_actions_enabled,
	evaluation_interval_minutes=EXCLUDED.evaluation_interval_minutes,
	promotion_window_count=EXCLUDED.promotion_window_count,
	degradation_window_count=EXCLUDED.degradation_window_count,
  probe_enabled=EXCLUDED.probe_enabled, probe_daily_token_budget=EXCLUDED.probe_daily_token_budget,
  probe_daily_cost_budget_micros=EXCLUDED.probe_daily_cost_budget_micros,
  probe_cooldown_seconds=EXCLUDED.probe_cooldown_seconds,
  updated_by=EXCLUDED.updated_by, updated_at=EXCLUDED.updated_at`,
		policy.ID, policy.Mode, policy.WindowHours, policy.MinSampleCount, policy.MinMetricsCoverage,
		policy.MinBillingConsistency, policy.MinCostImprovement, policy.MinCacheHitRateImprovement,
		policy.MinAffinityImprovement, policy.MaxCacheTiebreakCostRegression, policy.MaxErrorRateRegression,
		policy.MaxP95LatencyRegression, policy.CanaryPercent, policy.SupplierAffinityTTLSeconds,
		policy.AccountAffinityTTLSeconds, policy.AutomaticActionsEnabled, policy.EvaluationIntervalMinutes,
		policy.PromotionWindowCount, policy.DegradationWindowCount, policy.ProbeEnabled, policy.ProbeDailyTokenBudget,
		policy.ProbeDailyCostBudgetMicros, policy.ProbeCooldownSeconds, policy.UpdatedBy,
		policy.CreatedAt, policy.UpdatedAt)
	return err
}

func (r *PostgresRepository) ListEffectivePriceSnapshots(ctx context.Context) ([]EffectivePriceSnapshot, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, provider_id, provider_account_id, upstream_model, protocol, currency,
       effective_cost_micros_per_1m, effective_multiplier, quoted_multiplier,
       cache_token_hit_rate, metrics_coverage, billing_consistency_rate,
       request_count, cost_confidence, price_id, window_start, window_end,
       expires_at, created_at
FROM effective_price_snapshots ORDER BY provider_account_id, upstream_model, created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EffectivePriceSnapshot{}
	for rows.Next() {
		var snapshot EffectivePriceSnapshot
		if err := rows.Scan(&snapshot.ID, &snapshot.ProviderID, &snapshot.ProviderAccountID, &snapshot.UpstreamModel,
			&snapshot.Protocol, &snapshot.Currency, &snapshot.EffectiveCostMicrosPer1M, &snapshot.EffectiveMultiplier,
			&snapshot.QuotedMultiplier, &snapshot.CacheTokenHitRate, &snapshot.MetricsCoverage,
			&snapshot.BillingConsistencyRate, &snapshot.RequestCount, &snapshot.CostConfidence,
			&snapshot.PriceID, &snapshot.WindowStart, &snapshot.WindowEnd, &snapshot.ExpiresAt,
			&snapshot.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, snapshot)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveEffectivePriceSnapshot(ctx context.Context, snapshot EffectivePriceSnapshot) error {
	return saveEffectivePriceSnapshot(ctx, r.db, snapshot)
}

func saveEffectivePriceSnapshot(ctx context.Context, executor usageRecordExecutor, snapshot EffectivePriceSnapshot) error {
	_, err := executor.ExecContext(ctx, `
INSERT INTO effective_price_snapshots(
  id, provider_id, provider_account_id, upstream_model, protocol, currency,
  effective_cost_micros_per_1m, effective_multiplier, quoted_multiplier,
  cache_token_hit_rate, metrics_coverage, billing_consistency_rate,
  request_count, cost_confidence, price_id, window_start, window_end,
  expires_at, created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
ON CONFLICT(id) DO UPDATE SET
  effective_cost_micros_per_1m=EXCLUDED.effective_cost_micros_per_1m,
  effective_multiplier=EXCLUDED.effective_multiplier,
  quoted_multiplier=EXCLUDED.quoted_multiplier,
  cache_token_hit_rate=EXCLUDED.cache_token_hit_rate,
  metrics_coverage=EXCLUDED.metrics_coverage,
  billing_consistency_rate=EXCLUDED.billing_consistency_rate,
  request_count=EXCLUDED.request_count, cost_confidence=EXCLUDED.cost_confidence,
  price_id=EXCLUDED.price_id, window_start=EXCLUDED.window_start,
  window_end=EXCLUDED.window_end, expires_at=EXCLUDED.expires_at,
  created_at=EXCLUDED.created_at`,
		snapshot.ID, snapshot.ProviderID, snapshot.ProviderAccountID, snapshot.UpstreamModel,
		snapshot.Protocol, snapshot.Currency, snapshot.EffectiveCostMicrosPer1M, snapshot.EffectiveMultiplier,
		snapshot.QuotedMultiplier, snapshot.CacheTokenHitRate, snapshot.MetricsCoverage,
		snapshot.BillingConsistencyRate, snapshot.RequestCount, snapshot.CostConfidence,
		snapshot.PriceID, snapshot.WindowStart, snapshot.WindowEnd, snapshot.ExpiresAt, snapshot.CreatedAt)
	return err
}

func (r *PostgresRepository) ListEffectivePricingDecisions(ctx context.Context) ([]EffectivePricingDecision, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, model, protocol, current_provider_account_id, candidate_provider_account_id,
       current_snapshot_id, candidate_snapshot_id, current_cost_micros_per_1m,
       candidate_cost_micros_per_1m, cost_improvement, status, reason_codes,
       canary_percent, sample_count, confidence, healthy_window_count, degraded_window_count,
       last_evaluation_id, last_evaluation_verdict, last_evaluation_reason_codes,
       last_evaluated_window_end, monitoring_started_at, last_healthy_at, last_automatic_action,
       created_by, created_at, updated_at
FROM effective_pricing_decisions ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EffectivePricingDecision{}
	for rows.Next() {
		var decision EffectivePricingDecision
		var reasonCodes, lastEvaluationReasonCodes string
		if err := rows.Scan(&decision.ID, &decision.Model, &decision.Protocol, &decision.CurrentProviderAccountID,
			&decision.CandidateProviderAccountID, &decision.CurrentSnapshotID, &decision.CandidateSnapshotID,
			&decision.CurrentCostMicrosPer1M, &decision.CandidateCostMicrosPer1M, &decision.CostImprovement,
			&decision.Status, &reasonCodes, &decision.CanaryPercent, &decision.SampleCount,
			&decision.Confidence, &decision.HealthyWindowCount, &decision.DegradedWindowCount,
			&decision.LastEvaluationID, &decision.LastEvaluationVerdict, &lastEvaluationReasonCodes,
			&decision.LastEvaluatedWindowEnd, &decision.MonitoringStartedAt, &decision.LastHealthyAt,
			&decision.LastAutomaticAction, &decision.CreatedBy, &decision.CreatedAt, &decision.UpdatedAt); err != nil {
			return nil, err
		}
		decision.ReasonCodes = parseStringList(reasonCodes)
		decision.LastEvaluationReasonCodes = parseStringList(lastEvaluationReasonCodes)
		out = append(out, decision)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveEffectivePricingDecision(ctx context.Context, decision EffectivePricingDecision) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO effective_pricing_decisions(
  id, model, protocol, current_provider_account_id, candidate_provider_account_id,
  current_snapshot_id, candidate_snapshot_id, current_cost_micros_per_1m,
  candidate_cost_micros_per_1m, cost_improvement, status, reason_codes,
  canary_percent, sample_count, confidence, healthy_window_count, degraded_window_count,
  last_evaluation_id, last_evaluation_verdict, last_evaluation_reason_codes,
  last_evaluated_window_end, monitoring_started_at, last_healthy_at, last_automatic_action,
  created_by, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27)
ON CONFLICT(id) DO UPDATE SET
  current_provider_account_id=EXCLUDED.current_provider_account_id,
  candidate_provider_account_id=EXCLUDED.candidate_provider_account_id,
  current_snapshot_id=EXCLUDED.current_snapshot_id,
  candidate_snapshot_id=EXCLUDED.candidate_snapshot_id,
  current_cost_micros_per_1m=EXCLUDED.current_cost_micros_per_1m,
  candidate_cost_micros_per_1m=EXCLUDED.candidate_cost_micros_per_1m,
  cost_improvement=EXCLUDED.cost_improvement, status=EXCLUDED.status,
  reason_codes=EXCLUDED.reason_codes, canary_percent=EXCLUDED.canary_percent,
  sample_count=EXCLUDED.sample_count, confidence=EXCLUDED.confidence,
	healthy_window_count=EXCLUDED.healthy_window_count,
	degraded_window_count=EXCLUDED.degraded_window_count,
	last_evaluation_id=EXCLUDED.last_evaluation_id,
	last_evaluation_verdict=EXCLUDED.last_evaluation_verdict,
	last_evaluation_reason_codes=EXCLUDED.last_evaluation_reason_codes,
	last_evaluated_window_end=EXCLUDED.last_evaluated_window_end,
	monitoring_started_at=EXCLUDED.monitoring_started_at,
	last_healthy_at=EXCLUDED.last_healthy_at,
	last_automatic_action=EXCLUDED.last_automatic_action,
  created_by=EXCLUDED.created_by, updated_at=EXCLUDED.updated_at`,
		decision.ID, decision.Model, decision.Protocol, decision.CurrentProviderAccountID,
		decision.CandidateProviderAccountID, decision.CurrentSnapshotID, decision.CandidateSnapshotID,
		decision.CurrentCostMicrosPer1M, decision.CandidateCostMicrosPer1M, decision.CostImprovement,
		decision.Status, marshalStringList(decision.ReasonCodes), decision.CanaryPercent,
		decision.SampleCount, decision.Confidence, decision.HealthyWindowCount, decision.DegradedWindowCount,
		decision.LastEvaluationID, decision.LastEvaluationVerdict, marshalStringList(decision.LastEvaluationReasonCodes),
		decision.LastEvaluatedWindowEnd, decision.MonitoringStartedAt, decision.LastHealthyAt, decision.LastAutomaticAction,
		decision.CreatedBy, decision.CreatedAt, decision.UpdatedAt)
	return err
}

func (r *PostgresRepository) UpdateEffectivePricingDecision(ctx context.Context, decision EffectivePricingDecision, expectedStatus string, expectedUpdatedAt time.Time) (bool, error) {
	return updateEffectivePricingDecision(ctx, r.db, decision, expectedStatus, expectedUpdatedAt)
}

func updateEffectivePricingDecision(ctx context.Context, executor usageRecordExecutor, decision EffectivePricingDecision, expectedStatus string, expectedUpdatedAt time.Time) (bool, error) {
	result, err := executor.ExecContext(ctx, `
UPDATE effective_pricing_decisions SET
  current_provider_account_id=$1, candidate_provider_account_id=$2,
  current_snapshot_id=$3, candidate_snapshot_id=$4,
  current_cost_micros_per_1m=$5, candidate_cost_micros_per_1m=$6,
  cost_improvement=$7, status=$8, reason_codes=$9, canary_percent=$10,
  sample_count=$11, confidence=$12, healthy_window_count=$13,
  degraded_window_count=$14, last_evaluation_id=$15,
  last_evaluation_verdict=$16, last_evaluation_reason_codes=$17,
  last_evaluated_window_end=$18, monitoring_started_at=$19, last_healthy_at=$20,
  last_automatic_action=$21, created_by=$22, updated_at=$23
WHERE id=$24 AND status=$25 AND updated_at=$26`,
		decision.CurrentProviderAccountID, decision.CandidateProviderAccountID,
		decision.CurrentSnapshotID, decision.CandidateSnapshotID,
		decision.CurrentCostMicrosPer1M, decision.CandidateCostMicrosPer1M,
		decision.CostImprovement, decision.Status, marshalStringList(decision.ReasonCodes), decision.CanaryPercent,
		decision.SampleCount, decision.Confidence, decision.HealthyWindowCount, decision.DegradedWindowCount,
		decision.LastEvaluationID, decision.LastEvaluationVerdict, marshalStringList(decision.LastEvaluationReasonCodes),
		decision.LastEvaluatedWindowEnd, decision.MonitoringStartedAt, decision.LastHealthyAt,
		decision.LastAutomaticAction, decision.CreatedBy, decision.UpdatedAt,
		decision.ID, expectedStatus, expectedUpdatedAt)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (r *PostgresRepository) ListEffectivePricingDecisionEvaluations(ctx context.Context, decisionID string, limit int) ([]EffectivePricingDecisionEvaluation, error) {
	decisionID = strings.TrimSpace(decisionID)
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT id, decision_id, window_start, window_end, verdict, reason_codes,
       current_snapshot_id, candidate_snapshot_id, current_request_count,
       candidate_request_count, current_cost_micros_per_1m, candidate_cost_micros_per_1m,
       cost_improvement, current_cache_token_hit_rate, candidate_cache_token_hit_rate,
       current_cache_savings_rate, candidate_cache_savings_rate,
       current_affinity_consistency_rate, candidate_affinity_consistency_rate,
       current_error_rate, candidate_error_rate, current_p95_latency_ms,
       candidate_p95_latency_ms, current_metrics_coverage, candidate_metrics_coverage,
       current_billing_consistency_rate, candidate_billing_consistency_rate,
       automatic_action, created_at
FROM effective_pricing_decision_evaluations
WHERE ($1 = '' OR decision_id = $1)
ORDER BY window_end DESC
LIMIT $2`, decisionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EffectivePricingDecisionEvaluation{}
	for rows.Next() {
		var evaluation EffectivePricingDecisionEvaluation
		var reasonCodes string
		if err := rows.Scan(
			&evaluation.ID, &evaluation.DecisionID, &evaluation.WindowStart, &evaluation.WindowEnd,
			&evaluation.Verdict, &reasonCodes, &evaluation.CurrentSnapshotID, &evaluation.CandidateSnapshotID,
			&evaluation.CurrentRequestCount, &evaluation.CandidateRequestCount,
			&evaluation.CurrentCostMicrosPer1M, &evaluation.CandidateCostMicrosPer1M,
			&evaluation.CostImprovement, &evaluation.CurrentCacheTokenHitRate,
			&evaluation.CandidateCacheTokenHitRate, &evaluation.CurrentCacheSavingsRate,
			&evaluation.CandidateCacheSavingsRate, &evaluation.CurrentAffinityConsistencyRate,
			&evaluation.CandidateAffinityConsistencyRate, &evaluation.CurrentErrorRate,
			&evaluation.CandidateErrorRate, &evaluation.CurrentP95LatencyMS,
			&evaluation.CandidateP95LatencyMS, &evaluation.CurrentMetricsCoverage,
			&evaluation.CandidateMetricsCoverage, &evaluation.CurrentBillingConsistencyRate,
			&evaluation.CandidateBillingConsistencyRate, &evaluation.AutomaticAction,
			&evaluation.CreatedAt,
		); err != nil {
			return nil, err
		}
		evaluation.ReasonCodes = parseStringList(reasonCodes)
		out = append(out, evaluation)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) CommitEffectivePricingDecisionEvaluation(ctx context.Context, commit EffectivePricingDecisionEvaluationCommit) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var currentStatus string
	var currentUpdatedAt time.Time
	if err := tx.QueryRowContext(ctx, `
SELECT status, updated_at FROM effective_pricing_decisions WHERE id=$1 FOR UPDATE`, commit.Decision.ID).Scan(&currentStatus, &currentUpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if currentStatus != commit.ExpectedStatus || !currentUpdatedAt.Equal(commit.ExpectedUpdatedAt) {
		return false, nil
	}
	evaluation := commit.Evaluation
	result, err := tx.ExecContext(ctx, `
INSERT INTO effective_pricing_decision_evaluations(
  id, decision_id, window_start, window_end, verdict, reason_codes,
  current_snapshot_id, candidate_snapshot_id, current_request_count,
  candidate_request_count, current_cost_micros_per_1m, candidate_cost_micros_per_1m,
  cost_improvement, current_cache_token_hit_rate, candidate_cache_token_hit_rate,
  current_cache_savings_rate, candidate_cache_savings_rate,
  current_affinity_consistency_rate, candidate_affinity_consistency_rate,
  current_error_rate, candidate_error_rate, current_p95_latency_ms,
  candidate_p95_latency_ms, current_metrics_coverage, candidate_metrics_coverage,
  current_billing_consistency_rate, candidate_billing_consistency_rate,
  automatic_action, created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29)
ON CONFLICT(decision_id, window_end) DO NOTHING`,
		evaluation.ID, evaluation.DecisionID, evaluation.WindowStart, evaluation.WindowEnd,
		evaluation.Verdict, marshalStringList(evaluation.ReasonCodes), evaluation.CurrentSnapshotID,
		evaluation.CandidateSnapshotID, evaluation.CurrentRequestCount, evaluation.CandidateRequestCount,
		evaluation.CurrentCostMicrosPer1M, evaluation.CandidateCostMicrosPer1M,
		evaluation.CostImprovement, evaluation.CurrentCacheTokenHitRate,
		evaluation.CandidateCacheTokenHitRate, evaluation.CurrentCacheSavingsRate,
		evaluation.CandidateCacheSavingsRate, evaluation.CurrentAffinityConsistencyRate,
		evaluation.CandidateAffinityConsistencyRate, evaluation.CurrentErrorRate,
		evaluation.CandidateErrorRate, evaluation.CurrentP95LatencyMS,
		evaluation.CandidateP95LatencyMS, evaluation.CurrentMetricsCoverage,
		evaluation.CandidateMetricsCoverage, evaluation.CurrentBillingConsistencyRate,
		evaluation.CandidateBillingConsistencyRate, evaluation.AutomaticAction, evaluation.CreatedAt)
	if err != nil {
		return false, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if inserted != 1 {
		return false, nil
	}
	if commit.CurrentSnapshot != nil {
		if err := saveEffectivePriceSnapshot(ctx, tx, *commit.CurrentSnapshot); err != nil {
			return false, err
		}
	}
	if commit.CandidateSnapshot != nil {
		if err := saveEffectivePriceSnapshot(ctx, tx, *commit.CandidateSnapshot); err != nil {
			return false, err
		}
	}
	updated, err := updateEffectivePricingDecision(ctx, tx, commit.Decision, commit.ExpectedStatus, commit.ExpectedUpdatedAt)
	if err != nil {
		return false, err
	}
	if !updated {
		return false, nil
	}
	if commit.Audit != nil {
		if err := insertAuditLog(ctx, tx, *commit.Audit); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (r *PostgresRepository) FindRoutingAffinityBinding(ctx context.Context, scopeKey string, now time.Time) (RoutingAffinityBinding, bool, error) {
	var binding RoutingAffinityBinding
	err := r.db.QueryRowContext(ctx, `
SELECT scope_key, kind, provider_id, provider_account_id, route_id, model,
       protocol, policy_version, created_at, last_reused_at, expires_at
FROM routing_affinity_bindings WHERE scope_key=$1 AND expires_at>$2`, scopeKey, now).Scan(
		&binding.ScopeKey, &binding.Kind, &binding.ProviderID, &binding.ProviderAccountID,
		&binding.RouteID, &binding.Model, &binding.Protocol, &binding.PolicyVersion,
		&binding.CreatedAt, &binding.LastReusedAt, &binding.ExpiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			_, _ = r.db.ExecContext(ctx, `DELETE FROM routing_affinity_bindings WHERE scope_key=$1`, scopeKey)
			return RoutingAffinityBinding{}, false, nil
		}
		return RoutingAffinityBinding{}, false, err
	}
	return binding, true, nil
}

func (r *PostgresRepository) SaveRoutingAffinityBinding(ctx context.Context, binding RoutingAffinityBinding) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO routing_affinity_bindings(
  scope_key, kind, provider_id, provider_account_id, route_id, model,
  protocol, policy_version, created_at, last_reused_at, expires_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT(scope_key) DO UPDATE SET
  kind=EXCLUDED.kind, provider_id=EXCLUDED.provider_id,
  provider_account_id=EXCLUDED.provider_account_id, route_id=EXCLUDED.route_id,
  model=EXCLUDED.model, protocol=EXCLUDED.protocol, policy_version=EXCLUDED.policy_version,
  last_reused_at=EXCLUDED.last_reused_at, expires_at=EXCLUDED.expires_at`,
		binding.ScopeKey, binding.Kind, binding.ProviderID, binding.ProviderAccountID,
		binding.RouteID, binding.Model, binding.Protocol, binding.PolicyVersion,
		binding.CreatedAt, binding.LastReusedAt, binding.ExpiresAt)
	return err
}

func (r *PostgresRepository) DeleteRoutingAffinityBinding(ctx context.Context, scopeKey string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM routing_affinity_bindings WHERE scope_key=$1`, scopeKey)
	return err
}

func (r *PostgresRepository) SummarizeEffectivePricingUsage(ctx context.Context, from, to time.Time) ([]EffectivePricingUsageAggregate, error) {
	rows, err := r.db.QueryContext(ctx, `
	SELECT provider_id, provider_account_id, upstream_model, protocol,
	       COUNT(*),
	       COALESCE(SUM(CASE WHEN status NOT IN ('error','upstream_error') AND error_type='' THEN 1 ELSE 0 END),0),
	       COALESCE(SUM(CASE WHEN status IN ('error','upstream_error') OR error_type<>'' THEN 1 ELSE 0 END),0),
	       COALESCE(SUM(CASE WHEN status NOT IN ('error','upstream_error') AND error_type='' AND cache_fields_present THEN 1 ELSE 0 END),0),
	       COALESCE(SUM(CASE WHEN status NOT IN ('error','upstream_error') AND error_type='' AND cache_fields_present AND COALESCE(cache_read_tokens,0)>0 THEN 1 ELSE 0 END),0),
       COALESCE(SUM(COALESCE(total_input_tokens,input_tokens)),0),
       COALESCE(SUM(COALESCE(uncached_input_tokens,0)),0),
       COALESCE(SUM(COALESCE(cache_read_tokens,0)),0),
       COALESCE(SUM(COALESCE(cache_write_5m_tokens,0)),0),
       COALESCE(SUM(COALESCE(cache_write_1h_tokens,0)),0),
       COALESCE(SUM(output_tokens),0),
       COALESCE(SUM(COALESCE(procurement_cost_micros,0)),0),
	       COALESCE(SUM(CASE WHEN procurement_cost_micros IS NOT NULL THEN 1 ELSE 0 END),0),
	       COALESCE(SUM(latency_ms),0),
	       COALESCE(PERCENTILE_DISC(0.95) WITHIN GROUP (ORDER BY latency_ms)
	         FILTER (WHERE status NOT IN ('error','upstream_error') AND error_type='' AND latency_ms>0),0),
	       MAX(CASE WHEN status NOT IN ('error','upstream_error') AND error_type='' AND cache_fields_present THEN created_at END)
FROM usage_records
WHERE provider_account_id<>'' AND created_at >= $1 AND created_at <= $2
GROUP BY provider_id, provider_account_id, upstream_model, protocol
ORDER BY provider_account_id, upstream_model, protocol`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EffectivePricingUsageAggregate{}
	for rows.Next() {
		var aggregate EffectivePricingUsageAggregate
		if err := rows.Scan(&aggregate.ProviderID, &aggregate.ProviderAccountID, &aggregate.UpstreamModel,
			&aggregate.Protocol, &aggregate.RequestCount, &aggregate.SuccessfulRequestCount, &aggregate.ErrorCount,
			&aggregate.CacheMetricsRequestCount, &aggregate.CacheHitRequestCount, &aggregate.TotalInputTokens,
			&aggregate.UncachedInputTokens, &aggregate.CacheReadTokens,
			&aggregate.CacheWrite5mTokens, &aggregate.CacheWrite1hTokens,
			&aggregate.OutputTokens, &aggregate.ProcurementCostMicros,
			&aggregate.ProcurementCostRecordCount, &aggregate.LatencyTotalMS, &aggregate.P95LatencyMS,
			&aggregate.LastCacheObservedAt); err != nil {
			return nil, err
		}
		out = append(out, aggregate)
	}
	return out, rows.Err()
}

func valueOr(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}
