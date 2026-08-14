import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as control from '@/api/control'
import type { RoutingPolicy, RoutingPolicyStrategy } from '@/types'
import AdminRoutingPolicyView from './AdminRoutingPolicyView.vue'

vi.mock('@/api/control', () => ({
  createRoutingPolicy: vi.fn(),
  getGatewayModels: vi.fn(),
  getProcurementPrices: vi.fn(),
  getProviderAccounts: vi.fn(),
  getRoutingPolicies: vi.fn(),
  simulateGatewayRouting: vi.fn(),
  updateRoutingPolicy: vi.fn()
}))

function strategy(overrides: Partial<RoutingPolicyStrategy> = {}): RoutingPolicyStrategy {
  return {
    preset: 'balanced',
    smart_optimization: true,
    strict_order: false,
    failover_before_first_byte: true,
    sticky_routing: true,
    sticky_ttl_seconds: 900,
    native_protocol_only: false,
    absolute_max_input_per_1m: 0,
    absolute_max_output_per_1m: 0,
    max_price_multiple_of_cheapest: 0,
    low_price_pool_mode: 'none',
    low_price_pool_percent: 30,
    low_price_pool_min_candidates: 2,
    missing_price_action: 'allow',
    model_price_limits: [],
    resource_batches: [],
    preferred_provider_account_ids: [],
    allowed_models: [],
    denied_models: [],
    allowed_protocols: [],
    denied_protocols: [],
    ...overrides
  }
}

function policyFixture(): RoutingPolicy {
  return {
    id: 'routing-policy-1',
    name: 'Enterprise stable routing',
    description: 'Critical production traffic',
    route_group: 'stable',
    status: 'active',
    is_default: true,
    version: 3,
    created_at: '2026-08-14T00:00:00Z',
    updated_at: '2026-08-14T01:00:00Z',
    strategy: strategy({
      preset: 'stability',
      strict_order: true,
      missing_price_action: 'block',
      model_price_limits: [{ model: 'gateway-current', absolute_max_input_per_1m: 1, absolute_max_output_per_1m: 2 }],
      resource_batches: [{ name: 'Production', provider_account_ids: ['account-a', 'account-b'] }],
      preferred_provider_account_ids: ['account-b'],
      allowed_models: ['gateway-current'],
      allowed_protocols: ['openai_chat_completions']
    })
  }
}

describe('AdminRoutingPolicyView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('en-US')
    vi.mocked(control.getRoutingPolicies).mockResolvedValue([policyFixture()])
    vi.mocked(control.getProviderAccounts).mockResolvedValue([
      { id: 'account-a', name: 'Primary account', status: 'active' },
      { id: 'account-b', name: 'Backup account', status: 'active' },
      { id: 'account-disabled', name: 'Disabled account', status: 'disabled' }
    ] as never)
    vi.mocked(control.getProcurementPrices).mockResolvedValue([])
    vi.mocked(control.getGatewayModels).mockResolvedValue([
      { id: 'model-current', model_id: 'gateway-current', name: 'Current', default_route_group: 'stable', status: 'active' },
      { id: 'model-other', model_id: 'gateway-other', name: 'Other', default_route_group: 'other', status: 'active' }
    ] as never)
    vi.mocked(control.updateRoutingPolicy).mockResolvedValue(policyFixture())
    vi.mocked(control.createRoutingPolicy).mockResolvedValue(policyFixture())
    vi.mocked(control.simulateGatewayRouting).mockResolvedValue({
      requested_model: 'gateway-current', resolved_model: 'gateway-current', route_group: 'stable', status: 'ready', summary: 'ready',
      routing_policy_id: 'routing-policy-1', routing_policy_version: 3, routing_policy_preset: 'stability',
      candidates: [{
        rank: 1, route_id: 'route-a', route_group: 'stable', provider_id: 'provider-a', provider_account_id: 'account-a',
        upstream_model: 'upstream-current', provider_type: 'openai_compatible', upstream_format: 'openai_chat', adapter: 'openai_compatible',
        headroom: 0.8, rpm_limit: 100, tpm_limit: 10000, concurrency: 10, circuit_state: 'closed', eligible: true, reason: '',
        policy_batch_order: 0, policy_batch_name: 'Production', policy_batch_position: 0, price_fact_present: true,
        estimated_input_micros_per_1m: 500000, estimated_output_micros_per_1m: 800000,
        observed_success_rate: 0.99, observed_avg_latency_ms: 120, observed_sample_count: 100,
        selection_reason: 'strict declared order'
      }]
    } as never)
  })

  it('round-trips declared resource order and renders explicit-policy simulation evidence', async () => {
    const wrapper = mount(AdminRoutingPolicyView, { global: { plugins: [i18n] } })
    await flushPromises()

    expect((wrapper.get('input[aria-label="Strict declared order"]').element as HTMLInputElement).checked).toBe(true)
    const missingPriceField = wrapper.findAll('label.field').find((field) => field.text().includes('When price facts are missing'))
    expect((missingPriceField!.get('select').element as HTMLSelectElement).value).toBe('block')
    expect(wrapper.get('.batch-resource-order').text()).toContain('Primary account')
    expect(wrapper.get('.batch-resource-order').text()).toContain('Backup account')

    const simulationModel = wrapper.get('.simulation-controls select')
    expect(simulationModel.text()).toContain('gateway-current')
    expect(simulationModel.text()).not.toContain('gateway-other')
    await wrapper.get('.simulation-controls').trigger('submit')
    await flushPromises()
    expect(control.simulateGatewayRouting).toHaveBeenCalledWith(
      'gateway-current', 1000, 'openai_chat_completions', ['text'], 'routing-policy-1'
    )
    expect(wrapper.get('.simulation-results').text()).toContain('Production')
    expect(wrapper.get('.simulation-results').text()).toContain('99.0%')
    expect(wrapper.get('.simulation-results').text()).toContain('$0.5000 / $0.8000')

    await wrapper.get('.batch-resource-order button[aria-label="Move resource down"]').trigger('click')
    await wrapper.findAll('button').find((button) => button.text().includes('Save policy'))!.trigger('click')
    await flushPromises()
    expect(control.updateRoutingPolicy).toHaveBeenCalledWith('routing-policy-1', expect.objectContaining({
      is_default: true,
      strategy: expect.objectContaining({
        strict_order: true,
        missing_price_action: 'block',
        resource_batches: [{ name: 'Production', provider_account_ids: ['account-b', 'account-a'] }],
        preferred_provider_account_ids: ['account-b']
      })
    }))
    wrapper.unmount()
  })

  it('creates a complete enterprise policy through visible controls', async () => {
    vi.mocked(control.getRoutingPolicies).mockResolvedValue([])
    const wrapper = mount(AdminRoutingPolicyView, { global: { plugins: [i18n] } })
    await flushPromises()

    await wrapper.findAll('button').find((button) => button.text().includes('Add batch'))!.trigger('click')
    const batch = wrapper.get('.batch-row')
    await batch.findAll('.account-chip').find((button) => button.text().includes('Primary account'))!.trigger('click')
    await batch.findAll('.account-chip').find((button) => button.text().includes('Backup account'))!.trigger('click')
    await batch.get('button[aria-label="Move resource up"]:not([disabled])').trigger('click')
    await wrapper.findAll('.preferred-picker .account-chip').find((button) => button.text().includes('Backup account'))!.trigger('click')
    await wrapper.get('input[aria-label="Strict declared order"]').setValue(true)

    const missingPriceField = wrapper.findAll('label.field').find((field) => field.text().includes('When price facts are missing'))
    await missingPriceField!.get('select').setValue('block')
    await wrapper.findAll('button').find((button) => button.text().includes('Add model limit'))!.trigger('click')
    const modelLimit = wrapper.get('.model-price-limit-row')
    await modelLimit.findAll('input')[0].setValue(1.25)
    await modelLimit.findAll('input')[1].setValue(2.5)
    await wrapper.get('.protocol-rule-row select').setValue('allow')
    await wrapper.findAll('button').find((button) => button.text().includes('Save policy'))!.trigger('click')
    await flushPromises()

    expect(control.createRoutingPolicy).toHaveBeenCalledWith(expect.objectContaining({
      route_group: 'default',
      strategy: expect.objectContaining({
        strict_order: true,
        missing_price_action: 'block',
        model_price_limits: [{ model: 'gateway-current', absolute_max_input_per_1m: 1.25, absolute_max_output_per_1m: 2.5 }],
        resource_batches: [{ name: 'Batch 1', provider_account_ids: ['account-b', 'account-a'] }],
        preferred_provider_account_ids: ['account-b'],
        allowed_protocols: ['openai_chat_completions']
      })
    }))
    wrapper.unmount()
  })
})
