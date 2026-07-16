import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as control from '@/api/control'
import ProviderAccountModelEditor from './ProviderAccountModelEditor.vue'

vi.mock('@/api/control', () => ({
  discoverProviderAccountModels: vi.fn(),
  getProviderAccountModelInventory: vi.fn(),
  syncProviderAccountModels: vi.fn()
}))

const inventoryModels = [
  {
    provider_account_id: 'account-1',
    model_id: 'model-a',
    source: 'discovered',
    enabled: true,
    availability: 'available',
    route_count: 1,
    first_seen_at: '2026-07-15T00:00:00Z',
    last_seen_at: '2026-07-15T00:00:00Z',
    updated_at: '2026-07-15T00:00:00Z'
  },
  {
    provider_account_id: 'account-1',
    model_id: 'model-old',
    source: 'discovered',
    enabled: true,
    availability: 'missing',
    route_count: 1,
    first_seen_at: '2026-07-14T00:00:00Z',
    updated_at: '2026-07-15T00:00:00Z'
  }
] as const

describe('ProviderAccountModelEditor', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('en-US')
    vi.mocked(control.getProviderAccountModelInventory).mockResolvedValue({
      account_id: 'account-1',
      auto_enable_new_models: false,
      last_discovered_at: '2026-07-15T00:00:00Z',
      models: inventoryModels as never
    })
    vi.mocked(control.discoverProviderAccountModels).mockResolvedValue({
      account_id: 'account-1',
      discovered_at: '2026-07-15T01:00:00Z',
      models: [
        { ...inventoryModels[0], change: 'unchanged' },
        { ...inventoryModels[1], change: 'missing' },
        {
          ...inventoryModels[0],
          model_id: 'model-new',
          enabled: false,
          route_count: 0,
          change: 'added'
        }
      ] as never,
      added_models: ['model-new'],
      missing_models: ['model-old'],
      unchanged_models: ['model-a'],
      affected_route_ids: ['route-old']
    })
  })

  it('previews discovered and missing models, then applies an explicit enabled set', async () => {
    vi.mocked(control.syncProviderAccountModels).mockResolvedValue({
      account: {
        id: 'account-1',
        models: ['model-a', 'model-new', 'model-old'],
        auto_enable_new_models: false
      } as never,
      inventory: {
        account_id: 'account-1',
        auto_enable_new_models: false,
        models: inventoryModels as never
      },
      discovery: {
        account_id: 'account-1',
        discovered_at: '2026-07-15T01:00:00Z',
        models: inventoryModels as never,
        added_models: [],
        missing_models: ['model-old'],
        unchanged_models: ['model-a'],
        affected_route_ids: ['route-old']
      }
    })
    const wrapper = mount(ProviderAccountModelEditor, {
      props: {
        modelValue: ['model-a', 'model-old'],
        autoEnableNewModels: false,
        accountId: 'account-1'
      },
      global: { plugins: [i18n] }
    })
    await flushPromises()

    expect(wrapper.text()).toContain('model-old')
    expect(wrapper.text()).toContain('Missing')

    const discoverButton = wrapper.findAll('button').find((button) => button.text().includes('Discover models'))
    await discoverButton!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('model-new')
    expect(wrapper.text()).toContain('No longer reported upstream: model-old')
    await wrapper.get('input[aria-label="Toggle model model-new"]').setValue(true)
    await wrapper.setProps({ modelValue: ['model-a', 'model-new', 'model-old'] })

    const applyButton = wrapper.findAll('button').find((button) => button.text().includes('Discover and apply'))
    await applyButton!.trigger('click')
    await flushPromises()

    expect(control.syncProviderAccountModels).toHaveBeenCalledWith('account-1', {
      enabled_models: ['model-a', 'model-new', 'model-old'],
      auto_enable_new_models: false
    })
    expect(wrapper.emitted('synced')).toHaveLength(1)
    wrapper.unmount()
  })

  it('supports a manual model before a provider account has been created', async () => {
    const wrapper = mount(ProviderAccountModelEditor, {
      props: { modelValue: [], autoEnableNewModels: false },
      global: { plugins: [i18n] }
    })

    const input = wrapper.get('input[placeholder="Enter an upstream model ID"]')
    await input.setValue('vendor-model-latest')
    await input.trigger('keydown.enter')

    const updates = wrapper.emitted('update:modelValue') || []
    expect(updates[updates.length - 1]).toEqual([['vendor-model-latest']])
    expect(control.getProviderAccountModelInventory).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('applies an explicitly empty enabled set without deleting discovered inventory', async () => {
    vi.mocked(control.syncProviderAccountModels).mockResolvedValue({
      account: { id: 'account-1', models: [], auto_enable_new_models: false } as never,
      inventory: {
        account_id: 'account-1', auto_enable_new_models: false,
        models: [{ ...inventoryModels[0], enabled: false }] as never
      },
      discovery: {
        account_id: 'account-1', discovered_at: '2026-07-15T01:00:00Z',
        models: [{ ...inventoryModels[0], enabled: false }] as never,
        added_models: [], missing_models: [], unchanged_models: ['model-a'], affected_route_ids: []
      }
    })
    const wrapper = mount(ProviderAccountModelEditor, {
      props: { modelValue: [], autoEnableNewModels: false, accountId: 'account-1' },
      global: { plugins: [i18n] }
    })
    await flushPromises()

    const applyButton = wrapper.findAll('button').find((button) => button.text().includes('Discover and apply'))
    expect(applyButton!.attributes('disabled')).toBeUndefined()
    await applyButton!.trigger('click')
    await flushPromises()

    expect(control.syncProviderAccountModels).toHaveBeenCalledWith('account-1', {
      enabled_models: [],
      auto_enable_new_models: false
    })
    wrapper.unmount()
  })
})
