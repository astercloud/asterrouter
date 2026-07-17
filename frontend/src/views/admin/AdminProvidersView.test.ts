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

  it('configures a provider connection without a static recommended model catalog', async () => {
    const wrapper = mount(AdminProvidersView, { global: { plugins: [i18n] } })
    await flushPromises()

    const createButton = wrapper.findAll('button').find((button) => button.text().includes('New provider'))
    await createButton!.trigger('click')

    expect(wrapper.get('[role="dialog"]').text()).toContain('New provider')
    expect(wrapper.find('.provider-model-section').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Recommended models')
    expect(wrapper.text()).not.toMatch(/gpt-\d|claude-|gemini-\d|grok-\d/)
    wrapper.unmount()
  })
})
