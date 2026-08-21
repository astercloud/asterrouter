import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as control from '@/api/control'
import type { RoutingPolicy, RoutingPolicyRequest, RoutingPolicyStrategy } from '@/types'
import AdminSupplyCatalogView from './AdminSupplyCatalogView.vue'

vi.mock('@/api/control', () => ({
  getAPIKeys: vi.fn(),
  getGatewayModels: vi.fn(),
  getModelRoutes: vi.fn(),
  getProcurementPrices: vi.fn(),
  getProviderAccountHealthChecks: vi.fn(),
  getProviderAccounts: vi.fn(),
  getProviders: vi.fn(),
  getRoutingPolicies: vi.fn(),
  getSupplyUtilization: vi.fn(),
  updateRoutingPolicy: vi.fn()
}))

function strategy(overrides: Partial<RoutingPolicyStrategy> = {}): RoutingPolicyStrategy {
  return {
    preset: 'balanced', smart_optimization: true, strict_order: false, failover_before_first_byte: true,
    sticky_routing: true, sticky_ttl_seconds: 900, native_protocol_only: false,
    absolute_max_input_per_1m: 0, absolute_max_output_per_1m: 0, max_price_multiple_of_cheapest: 2,
    low_price_pool_mode: 'auto', low_price_pool_percent: 70, low_price_pool_min_candidates: 2, missing_price_action: 'allow',
    model_price_limits: [], resource_batches: [], preferred_provider_account_ids: [], allowed_models: [], denied_models: [],
    allowed_protocols: [], denied_protocols: [], ...overrides
  }
}

function policyFixture(overrides: Partial<RoutingPolicy> = {}): RoutingPolicy {
  return {
    id: 'policy-1', name: 'Production routing', description: 'Critical traffic', route_group: 'stable', status: 'active', is_default: true,
    strategy: strategy({
      resource_batches: [
        { name: 'Primary', provider_account_ids: ['account-a'] },
        { name: 'Fallback', provider_account_ids: ['account-b'] }
      ]
    }),
    version: 1, created_at: '2026-08-16T00:00:00Z', updated_at: '2026-08-16T00:00:00Z', ...overrides
  }
}

function mountView() {
  return mount(AdminSupplyCatalogView, {
    global: {
      plugins: [i18n],
      stubs: { RouterLink: { template: '<a><slot /></a>' } }
    }
  })
}

describe('AdminSupplyCatalogView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    setLocale('en-US')
    vi.mocked(control.getGatewayModels).mockResolvedValue([
      { id: 'model-1', model_id: 'enterprise-chat', name: 'Enterprise Chat', modality: 'chat', default_route_group: 'stable', status: 'active' }
    ] as never)
    vi.mocked(control.getProviders).mockResolvedValue([
      { id: 'provider-1', name: 'Vendor One', type: 'openai_compatible', status: 'active' }
    ] as never)
    vi.mocked(control.getProviderAccounts).mockResolvedValue([
      { id: 'account-a', provider_id: 'provider-1', name: 'Primary account', status: 'active', schedulable: true, circuit_state: 'closed' },
      { id: 'account-b', provider_id: 'provider-1', name: 'Fallback account', status: 'active', schedulable: true, circuit_state: 'closed' }
    ] as never)
    vi.mocked(control.getModelRoutes).mockResolvedValue([
      { id: 'route-a', gateway_model_id: 'model-1', provider_account_id: 'account-a', route_group: 'stable', upstream_model: 'upstream-a', upstream_format: 'openai_chat', priority: 10, weight: 100, status: 'active' },
      { id: 'route-b', gateway_model_id: 'model-1', provider_account_id: 'account-b', route_group: 'stable', upstream_model: 'upstream-b', upstream_format: 'anthropic', priority: 20, weight: 100, status: 'active' }
    ] as never)
    vi.mocked(control.getProcurementPrices).mockResolvedValue([
      { id: 'price-a', provider_account_id: 'account-a', upstream_model: 'upstream-a', protocol: 'openai_chat_completions', status: 'active', currency: 'USD', uncached_input_micros_per_1m_tokens: 100_000, output_micros_per_1m_tokens: 200_000 },
      { id: 'price-b', provider_account_id: 'account-b', upstream_model: 'upstream-b', protocol: 'anthropic_messages', status: 'active', currency: 'USD', uncached_input_micros_per_1m_tokens: 300_000, reference_input_micros_per_1m_tokens: 600_000, output_micros_per_1m_tokens: 400_000, reference_output_micros_per_1m_tokens: 800_000, request_micros: 2500, quoted_multiplier: 1.1, recharge_multiplier: 1.2 }
    ] as never)
    vi.mocked(control.getProviderAccountHealthChecks).mockResolvedValue([
      { id: 'health-a', account_id: 'account-a', status: 'ok', latency_ms: 80, message: 'Reachable', checked_at: '2026-08-16T00:00:00Z' },
      { id: 'health-b', account_id: 'account-b', status: 'warning', latency_ms: 140, message: 'Slow', checked_at: '2026-08-16T00:00:00Z' }
    ] as never)
    vi.mocked(control.getSupplyUtilization).mockResolvedValue({
      rows: [
        { dimension: 'provider_account', id: 'account-a', demand: { requests: 100, success_rate: 0.99, fallback_rate: 0.01 } },
        { dimension: 'provider_account', id: 'account-b', demand: { requests: 20, success_rate: 0.9, fallback_rate: 0.1 } }
      ]
    } as never)
    vi.mocked(control.getAPIKeys).mockResolvedValue([{ id: 'key-1', routing_policy_id: 'policy-1' }] as never)
    vi.mocked(control.getRoutingPolicies).mockResolvedValue([policyFixture()])
    vi.mocked(control.updateRoutingPolicy).mockImplementation(async (id: string, request: RoutingPolicyRequest) => ({
      ...policyFixture(), ...request, id, version: 2, updated_at: '2026-08-16T01:00:00Z'
    }))
  })

  it('renders model and route views with filters and route detail evidence', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('h1').text()).toBe('Model Hub')
    expect(wrapper.get('.policy-scope-copy').text()).toContain('1 credential bindings')
    expect(wrapper.get('.model-catalog-group').text()).toContain('enterprise-chat')
    expect(wrapper.get('.model-catalog-group').text()).toContain('$0.1000')

    await wrapper.findAll('.view-toggle button').find((button) => button.text().includes('By route'))!.trigger('click')
    expect(wrapper.get('.route-catalog-table').text()).toContain('Primary account')
    await wrapper.get('input[aria-label="Search Model Hub"]').setValue('Fallback account')
    expect(wrapper.findAll('.route-catalog-table tbody tr')).toHaveLength(1)
    expect(wrapper.get('.route-catalog-table tbody tr').text()).toContain('Fallback account')
    await wrapper.get('select[aria-label="Filter by protocol"]').setValue('anthropic')
    expect(wrapper.findAll('.route-catalog-table tbody tr')).toHaveLength(1)
    await wrapper.get('select[aria-label="Filter by protocol"]').setValue('openai_chat')
    expect(wrapper.findAll('.route-catalog-table tbody tr')).toHaveLength(0)
    await wrapper.get('select[aria-label="Filter by protocol"]').setValue('anthropic')

    await wrapper.get('.route-catalog-table tbody tr').trigger('click')
    expect(wrapper.get('[role="dialog"]').text()).toContain('upstream-b')
    expect(wrapper.get('[role="dialog"]').text()).toContain('90.0%')
    expect(wrapper.get('[role="dialog"]').text()).toContain('140 ms')
    expect(wrapper.get('[role="dialog"]').text()).toContain('1.10×')
    expect(wrapper.get('[role="dialog"]').text()).toContain('$0.6000')
    expect(wrapper.get('[role="dialog"]').text()).toContain('$0.8000')
    wrapper.unmount()
  })

  it('opens mobile route details from the keyboard', async () => {
    const wrapper = mountView()
    await flushPromises()

    const mobileRoute = wrapper.get('.mobile-route-item')
    expect(mobileRoute.attributes('tabindex')).toBe('0')
    await mobileRoute.trigger('keydown', { key: 'Enter' })
    expect(wrapper.get('[role="dialog"]').text()).toContain('upstream-a')
    wrapper.unmount()
  })

  it('persists preferred resources and ordered batch movement through complete policy updates', async () => {
    const wrapper = mountView()
    await flushPromises()

    const preferred = wrapper.get('button[aria-label="Set Primary account as preferred"]')
    await preferred.trigger('click')
    await flushPromises()
    expect(control.updateRoutingPolicy).toHaveBeenNthCalledWith(1, 'policy-1', expect.objectContaining({
      name: 'Production routing', route_group: 'stable', is_default: true,
      strategy: expect.objectContaining({
        preferred_provider_account_ids: ['account-a'],
        resource_batches: [
          { name: 'Primary', provider_account_ids: ['account-a'] },
          { name: 'Fallback', provider_account_ids: ['account-b'] }
        ]
      })
    }))

    const batchSelect = wrapper.get('select[aria-label="Set ordered batch for Fallback account"]')
    await batchSelect.setValue('0')
    await flushPromises()
    expect(control.updateRoutingPolicy).toHaveBeenNthCalledWith(2, 'policy-1', expect.objectContaining({
      strategy: expect.objectContaining({
        preferred_provider_account_ids: ['account-a'],
        resource_batches: [{ name: 'Primary', provider_account_ids: ['account-a', 'account-b'] }]
      })
    }))
    expect(wrapper.text()).toContain('Updated the ordered batch for Fallback account')
    wrapper.unmount()
  })

  it('serializes full-policy updates so concurrent actions cannot overwrite saved changes', async () => {
    let resolveUpdate!: (policy: RoutingPolicy) => void
    vi.mocked(control.updateRoutingPolicy).mockReturnValueOnce(new Promise((resolve) => {
      resolveUpdate = resolve
    }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('button[aria-label="Set Primary account as preferred"]').trigger('click')
    const fallbackButton = wrapper.get('button[aria-label="Set Fallback account as preferred"]')
    const fallbackBatch = wrapper.get('select[aria-label="Set ordered batch for Fallback account"]')
    expect((fallbackButton.element as HTMLButtonElement).disabled).toBe(true)
    expect((fallbackBatch.element as HTMLSelectElement).disabled).toBe(true)
    await fallbackButton.trigger('click')
    expect(control.updateRoutingPolicy).toHaveBeenCalledTimes(1)

    resolveUpdate(policyFixture({
      version: 2,
      strategy: strategy({
        preferred_provider_account_ids: ['account-a'],
        resource_batches: [
          { name: 'Primary', provider_account_ids: ['account-a'] },
          { name: 'Fallback', provider_account_ids: ['account-b'] }
        ]
      })
    }))
    await flushPromises()
    expect((fallbackButton.element as HTMLButtonElement).disabled).toBe(false)
    wrapper.unmount()
  })

  it('disables policy actions for a different route group', async () => {
    vi.mocked(control.getRoutingPolicies).mockResolvedValue([policyFixture({ route_group: 'other' })])
    const wrapper = mountView()
    await flushPromises()

    const button = wrapper.get('button[aria-label="Set Primary account as preferred"]')
    expect((button.element as HTMLButtonElement).disabled).toBe(true)
    expect(button.attributes('title')).toContain('route group other')
    wrapper.unmount()
  })

  it('restores the previous batch selection when policy persistence fails', async () => {
    vi.mocked(control.updateRoutingPolicy).mockRejectedValueOnce(new Error('policy update rejected'))
    const wrapper = mountView()
    await flushPromises()

    const batchSelect = wrapper.get('select[aria-label="Set ordered batch for Fallback account"]')
    await batchSelect.setValue('0')
    await flushPromises()

    expect((batchSelect.element as HTMLSelectElement).value).toBe('1')
    expect(wrapper.get('[role="alert"]').text()).toContain('policy update rejected')
    wrapper.unmount()
  })

  it('shows required-load failures as a recoverable page error', async () => {
    vi.mocked(control.getModelRoutes).mockRejectedValueOnce(new Error('routes unavailable'))
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[role="alert"]').text()).toContain('routes unavailable')
    expect(wrapper.find('.model-catalog-list').exists()).toBe(false)
    wrapper.unmount()
  })

  it('degrades optional evidence failures without hiding usable routes', async () => {
    vi.mocked(control.getProviderAccountHealthChecks).mockRejectedValueOnce(new Error('health unavailable'))
    vi.mocked(control.getSupplyUtilization).mockRejectedValueOnce(new Error('utilization unavailable'))
    vi.mocked(control.getAPIKeys).mockRejectedValueOnce(new Error('keys unavailable'))
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('.model-catalog-list').exists()).toBe(true)
    expect(wrapper.text()).toContain('Unchecked')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows an explicit empty state when there are no routes or active policies', async () => {
    vi.mocked(control.getModelRoutes).mockResolvedValueOnce([] as never)
    vi.mocked(control.getRoutingPolicies).mockResolvedValueOnce([])
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('.catalog-empty').text()).toContain('No supply routes')
    expect((wrapper.get('#supply-catalog-policy').element as HTMLSelectElement).disabled).toBe(true)
    wrapper.unmount()
  })

  it('keeps supply visible but disables policy actions when no policy is active', async () => {
    vi.mocked(control.getRoutingPolicies).mockResolvedValueOnce([policyFixture({ status: 'disabled', is_default: false })])
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('.model-catalog-list').text()).toContain('Primary account')
    expect(wrapper.get('.policy-scope-copy').text()).toContain('Select an active routing policy')
    expect((wrapper.get('button[aria-label="Set Primary account as preferred"]').element as HTMLButtonElement).disabled).toBe(true)
    wrapper.unmount()
  })

  it('recovers from a failed initial load after refresh', async () => {
    vi.mocked(control.getModelRoutes).mockRejectedValueOnce(new Error('temporary routes failure'))
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('temporary routes failure')

    const refreshButton = wrapper.get('button[aria-label="Refresh"]')
    await refreshButton.trigger('click')
    await flushPromises()
    expect(wrapper.find('.model-catalog-group').text()).toContain('enterprise-chat')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows unavailable routes by default and keeps schedulable-only as an explicit filter', async () => {
    vi.mocked(control.getProviderAccounts).mockResolvedValue([
      { id: 'account-a', provider_id: 'provider-1', name: 'Primary account', status: 'error', schedulable: true, circuit_state: 'closed' },
      { id: 'account-b', provider_id: 'provider-1', name: 'Fallback account', status: 'active', schedulable: true, circuit_state: 'closed' }
    ] as never)
    const wrapper = mountView()
    await flushPromises()

    const schedulableOnly = wrapper.get('input[type="checkbox"]')
    expect((schedulableOnly.element as HTMLInputElement).checked).toBe(false)
    expect(wrapper.get('.model-catalog-list').text()).toContain('Primary account')
    await schedulableOnly.setValue(true)
    expect(wrapper.get('.model-catalog-list').text()).not.toContain('Primary account')
    expect(wrapper.get('.model-catalog-list').text()).toContain('Fallback account')
    wrapper.unmount()
  })

  it('expands the first matching model when filtering hides the previously expanded group', async () => {
    vi.mocked(control.getGatewayModels).mockResolvedValue([
      { id: 'model-1', model_id: 'enterprise-chat', name: 'Enterprise Chat', modality: 'chat', default_route_group: 'stable', status: 'active' },
      { id: 'model-2', model_id: 'gpt-enterprise-review', name: 'GPT Enterprise Review', modality: 'chat', default_route_group: 'stable', status: 'active' }
    ] as never)
    vi.mocked(control.getModelRoutes).mockResolvedValue([
      { id: 'route-a', gateway_model_id: 'model-1', provider_account_id: 'account-a', route_group: 'stable', upstream_model: 'upstream-a', upstream_format: 'openai_chat', priority: 10, weight: 100, status: 'active' },
      { id: 'route-c', gateway_model_id: 'model-2', provider_account_id: 'account-a', route_group: 'stable', upstream_model: 'upstream-a', upstream_format: 'openai_chat', priority: 10, weight: 100, status: 'active' }
    ] as never)
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('input[aria-label="Search Model Hub"]').setValue('gpt-enterprise-review')
    await flushPromises()
    expect(wrapper.get('.model-catalog-group').text()).toContain('gpt-enterprise-review')
    expect(wrapper.get('.model-catalog-group .catalog-table-scroll').text()).toContain('Primary account')
    wrapper.unmount()
  })
})
