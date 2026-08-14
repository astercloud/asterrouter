import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as control from '@/api/control'
import AdminApiKeysView from './AdminApiKeysView.vue'

vi.mock('@/api/control', () => ({
  createAPIKey: vi.fn(),
  disableAPIKey: vi.fn(),
  getAPIKeys: vi.fn(),
  getAPIKeyPolicyExplanation: vi.fn(),
  getGatewayModels: vi.fn(),
  getGatewayTraces: vi.fn(),
  getGovernancePolicies: vi.fn(),
  getRoutingPolicies: vi.fn(),
  getUsageReport: vi.fn(),
  getWorkspaceUsers: vi.fn(),
  rotateAPIKey: vi.fn(),
  updateAPIKey: vi.fn()
}))

describe('AdminApiKeysView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('en-US')
    vi.mocked(control.getAPIKeys).mockResolvedValue([])
    vi.mocked(control.getGovernancePolicies).mockResolvedValue([])
    vi.mocked(control.getRoutingPolicies).mockResolvedValue([])
    vi.mocked(control.getWorkspaceUsers).mockResolvedValue([])
    vi.mocked(control.getGatewayModels).mockResolvedValue([
      { id: 'model-current', model_id: 'gateway-current', name: 'Current', status: 'active' },
      { id: 'model-retired', model_id: 'gateway-retired', name: 'Retired', status: 'disabled' }
    ] as never)
    vi.mocked(control.createAPIKey).mockResolvedValue({ key: 'ar_secret', record: { id: 'key-1' } } as never)
  })

  it('defaults new keys to the active gateway model catalog', async () => {
    const wrapper = mount(AdminApiKeysView, { global: { plugins: [i18n] } })
    await flushPromises()

    const createButton = wrapper.findAll('button').find((button) => button.text().includes('New workspace key'))
    await createButton!.trigger('click')

    expect(wrapper.get('[data-model-state="active"]').text()).toBe('gateway-current')
    expect(wrapper.text()).not.toContain('gateway-retired')
    await wrapper.get('.modal-body input').setValue('Dynamic catalog key')
    const saveButton = wrapper.findAll('.modal-footer button').find((button) => button.text().includes('Save'))
    await saveButton!.trigger('click')
    await flushPromises()

    expect(control.createAPIKey).toHaveBeenCalledWith(expect.objectContaining({
      name: 'Dynamic catalog key',
      model_allowlist: ['gateway-current'],
      scopes: ['gateway:invoke', 'models:read'],
      allowed_modalities: ['metadata', 'text'],
      allowed_operations: ['list_models', 'chat_completion'],
      artifact_policy: 'proxy_only'
    }))
    wrapper.unmount()
  })

  it('applies the image workbench gateway policy without moving policy ownership into the plugin', async () => {
    const wrapper = mount(AdminApiKeysView, { global: { plugins: [i18n] } })
    await flushPromises()

    const createButton = wrapper.findAll('button').find((button) => button.text().includes('New workspace key'))
    await createButton!.trigger('click')
    await wrapper.get('[data-policy-preset="image-workbench"]').trigger('click')

    const checkedValues = wrapper.findAll('.token-option-grid input:checked').map((input) => input.attributes('value'))
    expect(checkedValues).toEqual(expect.arrayContaining([
      'gateway:invoke',
      'models:read',
      'metadata',
      'image',
      'list_models',
      'image_generation'
    ]))
    expect(checkedValues).not.toContain('chat_completion')
    expect((wrapper.get('[data-artifact-policy]').element as HTMLSelectElement).value).toBe('managed')

    await wrapper.get('.modal-body input').setValue('Image workbench key')
    const saveButton = wrapper.findAll('.modal-footer button').find((button) => button.text().includes('Save'))
    await saveButton!.trigger('click')
    await flushPromises()

    expect(control.createAPIKey).toHaveBeenCalledWith(expect.objectContaining({
      scopes: ['gateway:invoke', 'models:read'],
      allowed_modalities: ['metadata', 'image'],
      allowed_operations: ['list_models', 'image_generation'],
      artifact_policy: 'managed'
    }))
    wrapper.unmount()
  })

  it('binds a workspace key only to routing policies compatible with its model route group', async () => {
    vi.mocked(control.getRoutingPolicies).mockResolvedValue([
      { id: 'routing-default', name: 'Default routing', route_group: 'default', status: 'active', is_default: true },
      { id: 'routing-other', name: 'Other routing', route_group: 'other', status: 'active', is_default: true }
    ] as never)
    const wrapper = mount(AdminApiKeysView, { global: { plugins: [i18n] } })
    await flushPromises()

    await wrapper.findAll('button').find((button) => button.text().includes('New workspace key'))!.trigger('click')
    const routingPolicyField = wrapper.findAll('.modal-body .field').find((field) => field.text().includes('Routing policy'))
    const routingPolicySelect = routingPolicyField!.get('select')
    expect(routingPolicySelect.text()).toContain('Default routing')
    expect(routingPolicySelect.text()).not.toContain('Other routing')
    await routingPolicySelect.setValue('routing-default')
    await wrapper.get('.modal-body input').setValue('Bound workspace key')
    await wrapper.findAll('.modal-footer button').find((button) => button.text().includes('Save'))!.trigger('click')
    await flushPromises()

    expect(control.createAPIKey).toHaveBeenCalledWith(expect.objectContaining({
      name: 'Bound workspace key',
      routing_policy_id: 'routing-default',
      model_allowlist: ['gateway-current']
    }))
    wrapper.unmount()
  })
})
