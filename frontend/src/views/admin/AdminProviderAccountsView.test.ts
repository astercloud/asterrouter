import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as control from '@/api/control'
import AdminProviderAccountsView from './AdminProviderAccountsView.vue'

vi.mock('@/api/control', () => ({
  checkProviderAccount: vi.fn(),
  clearProviderAccountCooldown: vi.fn(),
  createProviderAccount: vi.fn(),
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

describe('AdminProviderAccountsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    discoverModels.mockClear()
    setLocale('en-US')
    vi.mocked(control.getProviderAccounts).mockResolvedValue([])
    vi.mocked(control.getProviderAccountHealthChecks).mockResolvedValue([])
    vi.mocked(control.getProviders).mockResolvedValue([{ id: 'provider-1', name: 'Provider One', type: 'openai_compatible' }] as never)
    vi.mocked(control.getRoutingGroups).mockResolvedValue([{ id: 'group-1', name: 'Default' }] as never)
    vi.mocked(control.createProviderAccount).mockResolvedValue({
      id: 'account-1', provider_id: 'provider-1', name: 'Discovered account', platform: 'openai_compatible', auth_type: 'api_key',
      status: 'active', schedulable: true, priority: 50, weight: 100, concurrency: 3, rpm_limit: 0, tpm_limit: 0,
      load_factor: null, rate_multiplier: 1, models: [], auto_enable_new_models: false, group_ids: ['group-1'],
      expires_at: '', circuit_failure_threshold: 5, circuit_open_seconds: 60, temp_unschedulable_rules: []
    } as never)
  })

  it('creates an empty account and immediately starts dynamic model discovery', async () => {
    const wrapper = mount(AdminProviderAccountsView, {
      global: {
        plugins: [i18n],
        stubs: { ProviderAccountModelEditor: ModelEditorStub }
      }
    })
    await flushPromises()

    const newButton = wrapper.findAll('button').find((button) => button.text().includes('New route resource'))
    await newButton!.trigger('click')
    await wrapper.get('#account-name').setValue('Discovered account')
    let nextButton = wrapper.findAll('.modal-footer button').find((button) => button.text().includes('Next'))
    await nextButton!.trigger('click')
    await wrapper.get('#account-secret').setValue('test-secret')
    for (let step = 0; step < 2; step++) {
      nextButton = wrapper.findAll('.modal-footer button').find((button) => button.text().includes('Next'))
      expect(nextButton!.attributes('disabled')).toBeUndefined()
      await nextButton!.trigger('click')
    }
    const saveButton = wrapper.findAll('.modal-footer button').find((button) => button.text().includes('Save'))
    expect(saveButton!.attributes('disabled')).toBeUndefined()
    await saveButton!.trigger('click')
    await flushPromises()

    expect(control.createProviderAccount).toHaveBeenCalledWith(expect.objectContaining({
      provider_id: 'provider-1',
      adapter_config: {},
      models: [],
      auto_enable_new_models: false
    }))
    expect(discoverModels).toHaveBeenCalledOnce()
    expect(wrapper.get('[role="dialog"]').attributes('aria-label')).toBe('Edit route resource')
    wrapper.unmount()
  })
})
