import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as platform from '@/api/platform'
import type { PlatformTenant } from '@/types'
import PlatformTenantsView from './PlatformTenantsView.vue'

vi.mock('@/api/platform', () => ({
  createGatewayPrincipal: vi.fn(),
  createPlatformTenant: vi.fn(),
  getGatewayPrincipals: vi.fn(),
  getPlatformTenants: vi.fn(),
  updateGatewayPrincipal: vi.fn(),
  updatePlatformTenant: vi.fn()
}))

const tenant: PlatformTenant = {
  id: 'tenant-1',
  name: 'Application team',
  slug: 'application-team',
  entitlement_reference: 'plan-1',
  concurrency_limit: 5,
  status: 'active',
  created_at: '2026-07-26T00:00:00Z',
  updated_at: '2026-07-26T00:00:00Z'
}

describe('PlatformTenantsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('en-US')
    vi.mocked(platform.getPlatformTenants).mockResolvedValue([tenant])
    vi.mocked(platform.getGatewayPrincipals).mockResolvedValue([])
    vi.mocked(platform.updatePlatformTenant).mockResolvedValue(tenant)
  })

  it('loads and updates the tenant concurrency limit', async () => {
    const wrapper = mount(PlatformTenantsView, { global: { plugins: [i18n] } })
    await flushPromises()

    expect(wrapper.text()).toContain('Tenant concurrency limit')
    const editButton = wrapper.findAll('button').find((button) => button.text().includes('Edit'))
    await editButton!.trigger('click')

    const concurrencyInput = wrapper.get('#platform-tenant-concurrency')
    expect((concurrencyInput.element as HTMLInputElement).value).toBe('5')
    await concurrencyInput.setValue('8')
    const saveButton = wrapper.findAll('.modal-footer button').find((button) => button.text().includes('Save'))
    await saveButton!.trigger('click')
    await flushPromises()

    expect(platform.updatePlatformTenant).toHaveBeenCalledWith('tenant-1', expect.objectContaining({ concurrency_limit: 8 }))
    wrapper.unmount()
  })
})
