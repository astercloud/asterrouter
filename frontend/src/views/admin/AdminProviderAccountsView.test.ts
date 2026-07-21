import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as control from '@/api/control'
import type { ProviderAccount } from '@/types'
import AdminProviderAccountsView from './AdminProviderAccountsView.vue'

vi.mock('@/api/control', () => ({
  checkProviderAccount: vi.fn(),
  clearProviderAccountCooldown: vi.fn(),
  createProviderAccount: vi.fn(),
  deleteProviderAccount: vi.fn(),
  getProviderAccountHealthChecks: vi.fn(),
  getProviderAccounts: vi.fn(),
  getProviders: vi.fn(),
  getRoutingGroups: vi.fn(),
  updateProviderAccount: vi.fn()
}))

const discoverModels = vi.fn(async () => {})
const ModelEditorStub = defineComponent({
  emits: ['update:modelValue', 'update:autoEnableNewModels'],
  setup(_, { expose }) {
    expose({ discover: discoverModels })
    return {}
  },
  template: '<button class="set-models" type="button" @click="$emit(\'update:modelValue\', [\'vendor-model-latest\']); $emit(\'update:autoEnableNewModels\', true)">Set models</button>'
})

function providerAccount(overrides: Partial<ProviderAccount> = {}): ProviderAccount {
  return {
    id: 'account-1', provider_id: 'provider-1', name: 'Primary image account', platform: 'openai_compatible', auth_type: 'api_key',
    adapter_config: { asterrouter_settings: JSON.stringify({ notes: 'Production image route', quota_enabled: true, quota_daily_limit: 12, quota_weekly_limit: 60, quota_total_limit: 200 }) },
    status: 'active', schedulable: true, priority: 50, weight: 100, concurrency: 3, rpm_limit: 120, tpm_limit: 80_000,
    load_factor: undefined, rate_multiplier: 1, models: ['gpt-image-2'], auto_enable_new_models: false, group_ids: ['group-1'],
    secret_configured: true, secret_hint: 'sk-...test', error_message: '', last_used_at: '2026-07-18T08:00:00Z', expires_at: '2027-07-18T08:00:00Z',
    cooldown_until: '', circuit_state: 'closed', circuit_failure_threshold: 5, circuit_open_seconds: 60, consecutive_failures: 0,
    circuit_opened_until: '', last_failure_at: '', temp_unschedulable_rules: [], temp_unschedulable_reason: '',
    created_at: '2026-07-17T08:00:00Z', updated_at: '2026-07-18T08:00:00Z',
    ...overrides
  }
}

function mountView() {
  return mount(AdminProviderAccountsView, {
    global: {
      plugins: [i18n],
      stubs: { ProviderAccountModelEditor: ModelEditorStub }
    }
  })
}

describe('AdminProviderAccountsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    discoverModels.mockClear()
    localStorage.clear()
    setLocale('en-US')
    vi.mocked(control.getProviderAccounts).mockResolvedValue([])
    vi.mocked(control.getProviderAccountHealthChecks).mockResolvedValue([])
    vi.mocked(control.getProviders).mockResolvedValue([{ id: 'provider-1', name: 'Provider One', type: 'openai_compatible' }] as never)
    vi.mocked(control.getRoutingGroups).mockResolvedValue([{ id: 'group-1', name: 'Default' }] as never)
    vi.mocked(control.createProviderAccount).mockResolvedValue({
      id: 'account-1', provider_id: 'provider-1', name: 'Discovered account', platform: 'openai_compatible', auth_type: 'api_key',
      adapter_config: {},
      status: 'active', schedulable: true, priority: 50, weight: 100, concurrency: 3, rpm_limit: 0, tpm_limit: 0,
      load_factor: null, rate_multiplier: 1, models: [], auto_enable_new_models: false, group_ids: ['group-1'],
      expires_at: '', circuit_failure_threshold: 5, circuit_open_seconds: 60, temp_unschedulable_rules: []
    } as never)
    vi.mocked(control.updateProviderAccount).mockImplementation(async (id, payload) => providerAccount({ ...payload, load_factor: payload.load_factor ?? undefined, id, secret_configured: true }) as never)
  })

  afterEach(() => { vi.useRealTimers() })

  it('creates an API key account with the full advanced settings payload', async () => {
    const wrapper = mountView()
    await flushPromises()

    const newButton = wrapper.findAll('button').find((button) => button.text().includes('New route resource'))
    await newButton!.trigger('click')
    await wrapper.get('#account-name').setValue('Discovered account')
    await wrapper.get('#account-secret').setValue('test-secret')
    await wrapper.get('#account-base-url').setValue('https://api.example.test/v1')
    await wrapper.get('#account-notes').setValue('Managed test account')
    await wrapper.findAll('.provider-platform-tab').find((button) => button.text().includes('OpenAI'))!.trigger('click')
    await wrapper.get('.account-settings-form').trigger('submit')
    await flushPromises()

    expect(control.createProviderAccount).toHaveBeenCalledWith(expect.objectContaining({
      provider_id: 'provider-1',
      auth_type: 'api_key',
      secret: 'test-secret',
      models: [],
      auto_enable_new_models: false,
      adapter_config: expect.objectContaining({
        asterrouter_settings: expect.stringContaining('Managed test account')
      })
    }))
    expect(discoverModels).toHaveBeenCalledOnce()
    expect(wrapper.get('[role="dialog"]').attributes('aria-label')).toBe('Edit route resource')
    wrapper.unmount()
  })

  it('renders the operational account columns and toggles schedulability independently', async () => {
    vi.mocked(control.getProviderAccounts).mockResolvedValue([providerAccount()])
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('Account ID')
    expect(wrapper.text()).toContain('Platform / type')
    expect(wrapper.text()).toContain('OpenAI')
    expect(wrapper.text()).toContain('API key')
    expect(wrapper.text()).toContain('Default')
    expect(wrapper.text()).toContain('RPM 120 · TPM 80,000')
    expect(wrapper.text()).toContain('Production image route')

    await wrapper.get('.schedulable-switch').trigger('click')
    await flushPromises()

    expect(control.updateProviderAccount).toHaveBeenCalledWith('account-1', expect.objectContaining({
      status: 'active',
      schedulable: false,
      secret: ''
    }))
    wrapper.unmount()
  })

  it('applies a schedulability change to every selected account', async () => {
    vi.mocked(control.getProviderAccounts).mockResolvedValue([
      providerAccount(),
      providerAccount({ id: 'account-2', name: 'Fallback image account' })
    ])
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[aria-label="Select the filtered accounts"]').trigger('change')
    const bulkButton = wrapper.findAll('.account-bulk-actions button').find((button) => button.text().includes('Disable scheduling'))
    await bulkButton!.trigger('click')
    await flushPromises()

    expect(control.updateProviderAccount).toHaveBeenCalledTimes(2)
    expect(control.updateProviderAccount).toHaveBeenNthCalledWith(1, 'account-1', expect.objectContaining({ schedulable: false, status: 'active' }))
    expect(control.updateProviderAccount).toHaveBeenNthCalledWith(2, 'account-2', expect.objectContaining({ schedulable: false, status: 'active' }))
    expect(wrapper.find('.account-bulk-bar').exists()).toBe(false)
    wrapper.unmount()
  })

  it('supports column visibility and exposes existing row actions through the more menu', async () => {
    vi.mocked(control.getProviderAccounts).mockResolvedValue([providerAccount()])
    vi.mocked(control.checkProviderAccount).mockResolvedValue({ id: 'health-1', account_id: 'account-1', provider_id: 'provider-1', status: 'ok', latency_ms: 86, message: 'reachable', models: ['gpt-image-2'], checked_at: '2026-07-18T08:00:00Z' })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[aria-label="Columns"]').trigger('click')
    const accountIDColumnButton = wrapper.findAll('.account-column-menu button').find((button) => button.text().includes('Account ID'))
    await accountIDColumnButton!.trigger('click')
    expect(wrapper.findAll('th').some((header) => header.text() === 'Account ID')).toBe(false)

    await wrapper.get('[aria-label="More actions"]').trigger('click')
    const checkButton = wrapper.findAll('.row-action-menu button').find((button) => button.text().includes('Check'))
    await checkButton!.trigger('click')
    await flushPromises()
    expect(control.checkProviderAccount).toHaveBeenCalledWith('account-1')
    wrapper.unmount()
  })

  it('refreshes the account list on the selected automatic interval', async () => {
    vi.useFakeTimers()
    vi.mocked(control.getProviderAccounts).mockResolvedValue([providerAccount()])
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[aria-label="Auto refresh"]').trigger('click')
    const enableButton = wrapper.findAll('.account-refresh-menu button').find((button) => button.text().includes('Enable auto refresh'))
    await enableButton!.trigger('click')
    await vi.advanceTimersByTimeAsync(30_000)
    await flushPromises()

    expect(control.getProviderAccounts).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })
})
