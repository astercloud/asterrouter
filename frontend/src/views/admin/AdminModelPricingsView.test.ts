import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as control from '@/api/control'
import AdminModelPricingsView from './AdminModelPricingsView.vue'

vi.mock('@/api/control', () => ({
  createModelPricing: vi.fn(),
  getGatewayModels: vi.fn(),
  getModelPricings: vi.fn(),
  updateModelPricing: vi.fn()
}))

describe('AdminModelPricingsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('en-US')
    vi.mocked(control.getGatewayModels).mockResolvedValue([
      { id: 'active', model_id: 'gateway-current', name: 'Current', status: 'active' },
      { id: 'disabled', model_id: 'gateway-retired', name: 'Retired', status: 'disabled' }
    ] as never)
    vi.mocked(control.getModelPricings).mockResolvedValue([])
    vi.mocked(control.createModelPricing).mockResolvedValue({ id: 'pricing-current' } as never)
  })

  it('creates pricing for an active gateway model', async () => {
    const wrapper = mount(AdminModelPricingsView, { global: { plugins: [i18n] } })
    await flushPromises()
    await wrapper.findAll('button').find((button) => button.text().includes('New model price'))!.trigger('click')

    const modelSelect = wrapper.get('.modal-card select')
    expect((modelSelect.element as HTMLSelectElement).value).toBe('gateway-current')
    expect(modelSelect.text()).not.toContain('gateway-retired')
    await wrapper.get('form.modal-card').trigger('submit')
    await flushPromises()

    expect(control.createModelPricing).toHaveBeenCalledWith(expect.objectContaining({ model: 'gateway-current' }))
    wrapper.unmount()
  })

  it('preserves a historical model while editing existing pricing', async () => {
    vi.mocked(control.getModelPricings).mockResolvedValue([{
      id: 'legacy-pricing', model: 'legacy-model', currency: 'USD', input_price_cents_per_1m_tokens: 1,
      output_price_cents_per_1m_tokens: 2, status: 'active'
    }] as never)
    const wrapper = mount(AdminModelPricingsView, { global: { plugins: [i18n] } })
    await flushPromises()
    await wrapper.findAll('button').find((button) => button.text().includes('Edit'))!.trigger('click')

    const modelSelect = wrapper.get('.modal-card select')
    expect((modelSelect.element as HTMLSelectElement).value).toBe('legacy-model')
    expect(modelSelect.text()).toContain('legacy-model · Historical models')
    wrapper.unmount()
  })
})
