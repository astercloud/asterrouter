import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as control from '@/api/control'
import AdminGatewaySimulatorView from './AdminGatewaySimulatorView.vue'

vi.mock('@/api/control', () => ({
  getGatewayModels: vi.fn(),
  simulateGatewayRouting: vi.fn()
}))

describe('AdminGatewaySimulatorView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('en-US')
    vi.mocked(control.getGatewayModels).mockResolvedValue([
      { id: 'active', model_id: 'gateway-current', name: 'Current', status: 'active' },
      { id: 'disabled', model_id: 'gateway-retired', name: 'Retired', status: 'disabled' }
    ] as never)
    vi.mocked(control.simulateGatewayRouting).mockResolvedValue({
      status: 'ok', resolved_model: 'gateway-current', route_group: 'default', summary: 'ok', candidates: []
    } as never)
  })

  it('simulates only models from the active gateway catalog', async () => {
    const wrapper = mount(AdminGatewaySimulatorView, { global: { plugins: [i18n] } })
    await flushPromises()

    const modelSelect = wrapper.get('select')
    expect((modelSelect.element as HTMLSelectElement).value).toBe('gateway-current')
    expect(modelSelect.text()).not.toContain('gateway-retired')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(control.simulateGatewayRouting).toHaveBeenCalledWith('gateway-current', 1000)
    wrapper.unmount()
  })
})
