import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as control from '@/api/control'
import type { CapacityRecommendationReport, SupplyUtilizationReport, SupplyUtilizationRow } from '@/types'
import AdminSupplyView from './AdminSupplyView.vue'

vi.mock('@/api/control', () => ({
  getSupplyUtilization: vi.fn(),
  getCapacityRecommendations: vi.fn()
}))

const watermark = { status: 'known' as const, source: 'provider_account_config', limit: 10, current: 2, peak: 9, current_ratio: 0.2, peak_ratio: 0.9 }

function row(overrides: Partial<SupplyUtilizationRow>): SupplyUtilizationRow {
  return {
    dimension: 'provider_account', id: 'account-a', name: 'Primary account', provider_id: 'provider-a', capacity_status: 'saturated', primary_constraint: 'rpm', unknown_capacity: false, stranded_capacity: false, stranded_reasons: [],
    demand: { requests: 24, successful_requests: 23, rejected_requests: 1, success_rate: 23 / 24, http_429_requests: 1, http_5xx_requests: 0, fallback_requests: 2, fallback_rate: 2 / 24, no_candidate_requests: 0, capacity_rejected_requests: 1, policy_rejected_requests: 0, account_error_requests: 0, protocol_incompatible_requests: 0, unclassified_failure_requests: 0 },
    tokens: { input_tokens: 2400, output_tokens: 600, cache_read_tokens: 100, cache_write_tokens: 0, reasoning_status: 'unknown', normalization_gaps: 0 },
    costs: [{ currency: 'USD', cost_micros: 240_000, priced_requests: 24 }], unpriced_requests: 0,
    concurrency: { current: 1, p50: 1, p95: 2, p99: 2, peak: 2 }, watermarks: { rpm: watermark, tpm: { ...watermark, status: 'not_comparable', limit: 0, peak_ratio: 0 }, concurrency: { ...watermark, limit: 2, peak: 2, peak_ratio: 1 } },
    period: { peak_minute: '2026-07-26T10:00:00Z', peak_minute_calls: 9, idle_hours: 20, cooldown_seconds: 0, health_coverage: 1 },
    evidence: { trace_count: 24, usage_record_count: 24, attempt_count: 25, sources: ['gateway_trace', 'usage_record'], filter: { provider_account_id: 'account-a', model: 'public-model' }, complete: true },
    ...overrides
  }
}

const utilization: SupplyUtilizationReport = {
  window: { from: '2026-07-25T12:00:00Z', to: '2026-07-26T12:00:00Z', duration_seconds: 86_400, trace_count: 24, usage_record_count: 24, truncated: false },
  freshness: { trace_as_of: '2026-07-26T11:59:00Z', usage_as_of: '2026-07-26T11:59:00Z', health_as_of: '2026-07-26T11:58:00Z', capacity_as_of: '2026-07-26T12:00:00Z' },
  rows: [
    row({}),
    row({ dimension: 'published_model', id: 'model-a', name: 'Public model', gateway_model_id: 'model-a', capacity_status: 'unknown', unknown_capacity: true, watermarks: { rpm: { ...watermark, status: 'not_comparable', limit: 0, peak_ratio: 0 }, tpm: { ...watermark, status: 'not_comparable', limit: 0, peak_ratio: 0 }, concurrency: { ...watermark, status: 'not_comparable', limit: 0, peak_ratio: 0 } }, evidence: { trace_count: 24, usage_record_count: 24, attempt_count: 25, sources: ['gateway_trace'], filter: { model: 'public-model', gateway_model_id: 'model-a' }, complete: true } })
  ],
  by_dimension: { provider_account: 1, published_model: 1, route_group: 0, application: 0 }
}

const recommendations: CapacityRecommendationReport = {
  mode: 'observe_only', generated_at: '2026-07-26T12:00:00Z', window: utilization.window, summary: { total: 1, actionable: 1, inconclusive: 0 },
  items: [{ id: 'increase:provider_account:account-a', status: 'actionable', type: 'increase_capacity', target: { provider_account_id: 'account-a' }, target_name: 'Primary account', primary_constraint: 'rpm', confidence: 'high', reason_codes: ['sustained_capacity_pressure'], counter_evidence: [], missing_evidence: [], affected_applications: ['key-a'], affected_models: ['public-model'], affected_route_groups: ['model-a:default'], evidence: { sample_count: 24, peak_watermark: 0.9, capacity_rejected_requests: 1, policy_rejected_requests: 0, unclassified_failure_requests: 0, fallback_rate: 2 / 24, success_rate: 23 / 24, health_coverage: 1, observed_from: utilization.window.from, observed_to: utilization.window.to }, rollback: 'restore_previous_capacity' }]
}

describe('AdminSupplyView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('en-US')
    vi.mocked(control.getSupplyUtilization).mockResolvedValue(utilization)
    vi.mocked(control.getCapacityRecommendations).mockResolvedValue(recommendations)
  })

  it('renders the account view, switches dimensions, and reloads the selected window', async () => {
    const wrapper = mount(AdminSupplyView, { global: { plugins: [i18n], stubs: { RouterLink: { template: '<a><slot /></a>' } } } })
    await flushPromises()

    expect(wrapper.get('h1').text()).toBe('Supply Utilization')
    expect(wrapper.text()).toContain('Primary account')
    expect(wrapper.text()).toContain('Increase capacity')
    expect(wrapper.findAll('.supply-tabs button')).toHaveLength(4)

    await wrapper.findAll('.supply-tabs button')[2].trigger('click')
    expect(wrapper.text()).toContain('Public model')

    await wrapper.get('.supply-window-control button:nth-child(2)').trigger('click')
    await flushPromises()
    expect(control.getSupplyUtilization).toHaveBeenLastCalledWith(168)
    expect(control.getCapacityRecommendations).toHaveBeenLastCalledWith(168)
  })
})
