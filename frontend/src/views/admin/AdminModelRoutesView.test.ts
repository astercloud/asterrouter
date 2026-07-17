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
      { id: 'gateway-gpt', model_id: 'gpt-latest', name: 'GPT', modality: 'chat', default_route_group: 'default', status: 'active' },
      { id: 'gateway-claude', model_id: 'claude-public', name: 'Claude', modality: 'chat', default_route_group: 'default', status: 'active' },
      { id: 'gateway-retired', model_id: 'retired-public', name: 'Retired', modality: 'chat', default_route_group: 'default', status: 'disabled' }
    ] as never)
    vi.mocked(control.getProviderAccounts).mockResolvedValue([
      { id: 'account-1', name: 'Account One', platform: 'openai_compatible', models: ['gpt-latest', 'claude-upstream'] }
    ] as never)
    vi.mocked(control.getModelRoutes).mockResolvedValue([
      {
        id: 'route-existing',
        gateway_model_id: 'gateway-gpt',
        route_group: 'default',
        provider_account_id: 'account-1',
        upstream_model: 'gpt-latest',
        upstream_format: 'openai_chat',
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
        upstream_format: 'openai_chat',
        priority: 100,
        weight: 100,
        status: 'active'
      }]
    })
    expect(wrapper.text()).toContain('Created 1 model routes')
    wrapper.unmount()
  })

  it('uses native media format for non-chat checklist rows', async () => {
    vi.mocked(control.getGatewayModels).mockResolvedValue([
      { id: 'gateway-image', model_id: 'image-upstream', name: 'Image', modality: 'image', default_route_group: 'default', status: 'active' }
    ] as never)
    vi.mocked(control.getProviderAccounts).mockResolvedValue([
      { id: 'account-1', name: 'Account One', platform: 'openai_compatible', models: ['image-upstream'] }
    ] as never)
    vi.mocked(control.getModelRoutes).mockResolvedValue([])

    const wrapper = mount(AdminModelRoutesView, { global: { plugins: [i18n] } })
    await flushPromises()
    const bulkButton = wrapper.findAll('button').find((button) => button.text().includes('Bulk match models'))
    await bulkButton!.trigger('click')

    expect(wrapper.find('option[value="native_media"]').exists()).toBe(true)
    await wrapper.get('form.modal-card-wide').trigger('submit')
    await flushPromises()

    expect(control.bulkCreateModelRoutes).toHaveBeenCalledWith({
      routes: [{
        gateway_model_id: 'gateway-image',
        route_group: 'default',
        provider_account_id: 'account-1',
        upstream_model: 'image-upstream',
        upstream_format: 'native_media',
        priority: 100,
        weight: 100,
        status: 'active'
      }]
    })
    wrapper.unmount()
  })

  it('does not turn inventory-only or unsupported cloud audio models into executable routes', async () => {
    vi.mocked(control.getGatewayModels).mockResolvedValue([
      { id: 'gateway-audio', model_id: 'audio-upstream', name: 'Audio', modality: 'audio', default_route_group: 'default', status: 'active' },
      { id: 'gateway-embedding', model_id: 'embedding-upstream', name: 'Embedding', modality: 'embedding', default_route_group: 'default', status: 'active' },
      { id: 'gateway-chat', model_id: 'chat-upstream', name: 'Chat', modality: 'chat', default_route_group: 'default', status: 'active' }
    ] as never)
    vi.mocked(control.getProviderAccounts).mockResolvedValue([
      { id: 'account-gcp', name: 'Vertex', platform: 'gcp_vertex', models: ['audio-upstream', 'embedding-upstream', 'chat-upstream'] }
    ] as never)
    vi.mocked(control.getModelRoutes).mockResolvedValue([])

    const wrapper = mount(AdminModelRoutesView, { global: { plugins: [i18n] } })
    await flushPromises()
    await wrapper.findAll('button').find((button) => button.text().includes('Bulk match models'))!.trigger('click')

    const audioSelect = wrapper.get('select[aria-label="Gateway model for upstream model audio-upstream"]')
    expect(audioSelect.find('option[value="gateway-audio"]').exists()).toBe(false)
    expect(wrapper.get('select[aria-label="Gateway model for upstream model embedding-upstream"]').find('option[value="gateway-embedding"]').exists()).toBe(false)
    expect((wrapper.get('select[aria-label="Gateway model for upstream model chat-upstream"]').element as HTMLSelectElement).value).toBe('gateway-chat')
    expect(wrapper.get('.bulk-route-summary').text()).toContain('2unmatched')
    wrapper.unmount()
  })

  it('does not offer route creation for an account with no enabled inventory', async () => {
    vi.mocked(control.getProviderAccounts).mockResolvedValue([
      { id: 'account-empty', name: 'Empty account', platform: 'openai_compatible', models: [] }
    ] as never)
    vi.mocked(control.getModelRoutes).mockResolvedValue([])

    const wrapper = mount(AdminModelRoutesView, { global: { plugins: [i18n] } })
    await flushPromises()
    const createButton = wrapper.findAll('button').find((button) => button.text().includes('New model route'))
    const bulkButton = wrapper.findAll('button').find((button) => button.text().includes('Bulk match models'))
    expect(createButton!.attributes('disabled')).toBeDefined()
    expect(bulkButton!.attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('shows both text and media route formats for multimodal models', async () => {
    vi.mocked(control.getGatewayModels).mockResolvedValue([
      { id: 'gateway-multimodal', model_id: 'omni-upstream', name: 'Omni', modality: 'multimodal', default_route_group: 'default', status: 'active' }
    ] as never)
    vi.mocked(control.getProviderAccounts).mockResolvedValue([
      { id: 'account-1', name: 'Account One', platform: 'openai_compatible', models: ['omni-upstream'] }
    ] as never)
    vi.mocked(control.getModelRoutes).mockResolvedValue([])

    const wrapper = mount(AdminModelRoutesView, { global: { plugins: [i18n] } })
    await flushPromises()
    await wrapper.findAll('button').find((button) => button.text().includes('New model route'))!.trigger('click')

    const values = wrapper.findAll('.modal-card option').map((option) => option.attributes('value'))
    expect(values).toContain('openai_chat')
    expect(values).toContain('openai_responses')
    expect(values).toContain('native_media')
    wrapper.unmount()
  })

  it('shows account inventory and public mappings in the support matrix', async () => {
    const wrapper = mount(AdminModelRoutesView, { global: { plugins: [i18n] } })
    await flushPromises()
    const matrixTab = wrapper.findAll('[role="tab"]').find((tab) => tab.text().includes('Support matrix'))
    await matrixTab!.trigger('click')

    expect(wrapper.get('.model-support-matrix').text()).toContain('Account One')
    expect(wrapper.get('.model-support-matrix').text()).toContain('gpt-latest')
    expect(wrapper.get('.model-support-matrix').text()).toContain('openai_chat')
    expect(wrapper.get('.model-support-matrix').text()).toContain('claude-upstream')
    expect(wrapper.get('.model-support-matrix').text()).toContain('Unrouted')
    wrapper.unmount()
  })
})
