import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as control from '@/api/control'
import AdminModelRoutesView from './AdminModelRoutesView.vue'

vi.mock('@/api/control', () => ({
  bulkCreateModelRoutes: vi.fn(),
  createModelRoute: vi.fn(),
  deleteModelRoute: vi.fn(),
  getGatewayModels: vi.fn(),
  getModelRoutes: vi.fn(),
  getProviderAccounts: vi.fn(),
  updateModelRoute: vi.fn()
}))

describe('AdminModelRoutesView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('en-US')
    vi.mocked(control.getGatewayModels).mockResolvedValue([
      { id: 'gateway-gpt', model_id: 'gpt-latest', name: 'GPT', default_route_group: 'default', status: 'active' },
      { id: 'gateway-claude', model_id: 'claude-public', name: 'Claude', default_route_group: 'default', status: 'active' },
      { id: 'gateway-retired', model_id: 'retired-public', name: 'Retired', default_route_group: 'default', status: 'disabled' }
    ] as never)
    vi.mocked(control.getProviderAccounts).mockResolvedValue([
      { id: 'account-1', name: 'Account One', models: ['gpt-latest', 'claude-upstream'] }
    ] as never)
    vi.mocked(control.getModelRoutes).mockResolvedValue([
      {
        id: 'route-existing',
        gateway_model_id: 'gateway-gpt',
        route_group: 'default',
        provider_account_id: 'account-1',
        upstream_model: 'gpt-latest',
        priority: 100,
        weight: 100,
        status: 'active'
      }
    ] as never)
    vi.mocked(control.bulkCreateModelRoutes).mockResolvedValue({ routes: [{ id: 'route-new' }] as never })
  })

  it('auto-matches exact IDs, excludes existing routes, and submits reviewed mappings as one batch', async () => {
    const wrapper = mount(AdminModelRoutesView, { global: { plugins: [i18n] } })
    await flushPromises()

    const bulkButton = wrapper.findAll('button').find((button) => button.text().includes('Bulk match models'))
    await bulkButton!.trigger('click')

    expect(wrapper.get('.bulk-route-summary').text()).toContain('1unmatched')
    expect(wrapper.get('.bulk-route-summary').text()).toContain('1existing')
    const publicModel = wrapper.get('select[aria-label="Gateway model for upstream model claude-upstream"]')
    expect(publicModel.find('option[value="gateway-retired"]').exists()).toBe(false)
    await publicModel.setValue('gateway-claude')

    const createButton = wrapper.findAll('.modal-footer button').find((button) => button.text().includes('Create 1 routes'))
    expect(createButton!.attributes('disabled')).toBeUndefined()
    await wrapper.get('form.modal-card-wide').trigger('submit')
    await flushPromises()

    expect(control.bulkCreateModelRoutes).toHaveBeenCalledWith({
      routes: [{
        gateway_model_id: 'gateway-claude',
        route_group: 'default',
        provider_account_id: 'account-1',
        upstream_model: 'claude-upstream',
        priority: 100,
        weight: 100,
        status: 'active'
      }]
    })
    expect(wrapper.text()).toContain('Created 1 model routes')
    wrapper.unmount()
  })
})
