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
      model_allowlist: ['gateway-current']
    }))
    wrapper.unmount()
  })
})
