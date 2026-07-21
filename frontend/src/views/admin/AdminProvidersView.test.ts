import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as control from '@/api/control'
import AdminProvidersView from './AdminProvidersView.vue'

vi.mock('@/api/control', () => ({
  checkProvider: vi.fn(),
  createProvider: vi.fn(),
  getProviderHealthChecks: vi.fn(),
  getProviders: vi.fn(),
  updateProvider: vi.fn()
}))

describe('AdminProvidersView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('en-US')
    vi.mocked(control.getProviders).mockResolvedValue([])
    vi.mocked(control.getProviderHealthChecks).mockResolvedValue([])
  })

  it('restores the API-only provider setup flow', async () => {
    const wrapper = mount(AdminProvidersView, { global: { plugins: [i18n] } })
    await flushPromises()

    const createButton = wrapper.findAll('button').find((button) => button.text().includes('New provider'))
    await createButton!.trigger('click')

    const dialog = wrapper.get('[role="dialog"]')
    expect(dialog.text()).toContain('New provider connection')
    expect(dialog.findAll('.provider-platform-tab')).toHaveLength(5)
    expect(dialog.text()).toContain('API key')
    expect(dialog.text()).not.toContain('AWS Bedrock')
    expect(dialog.text()).not.toContain('Vertex')
    expect(dialog.text()).not.toContain('OAuth')
    expect(wrapper.find('.provider-model-section').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Recommended models')
    expect(wrapper.text()).not.toMatch(/gpt-\d|claude-|gemini-\d|grok-\d/)

    await dialog.findAll('.provider-platform-tab').find((button) => button.text() === 'Grok')!.trigger('click')
    expect(dialog.get('#provider-base-url').element).toHaveProperty('value', 'https://api.x.ai/v1')

    await dialog.get('#provider-account-name').setValue('Grok connection')
    await dialog.get('form').trigger('submit')
    await flushPromises()
    expect(control.createProvider).toHaveBeenCalledWith({
      name: 'Grok connection',
      type: 'openai_compatible',
      base_url: 'https://api.x.ai/v1',
      status: 'active',
      priority: 100
    })

    wrapper.unmount()
  })
})
