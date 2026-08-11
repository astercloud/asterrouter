import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getPublicSettings } from '@/api/settings'
import { makeAuthUser, makePublicSettings } from '@/test/fixtures'
import router, { clearPublicSettingsCache } from './index'

vi.mock('@/api/settings', () => ({ getPublicSettings: vi.fn() }))

const getPublicSettingsMock = vi.mocked(getPublicSettings)

describe('enterprise router guards', () => {
  beforeEach(async () => {
    localStorage.clear()
    getPublicSettingsMock.mockReset()
    getPublicSettingsMock.mockResolvedValue(makePublicSettings())
    clearPublicSettingsCache()
    await router.replace('/legal/test-fixture')
    clearPublicSettingsCache()
  })

  it('sends an incomplete instance to enterprise setup', async () => {
    getPublicSettingsMock.mockResolvedValue(makePublicSettings({ setup_completed: false }))
    await router.push('/')
    expect(router.currentRoute.value.fullPath).toBe('/setup')
  })

  it('opens the management console for an administrator', async () => {
    localStorage.setItem('asterrouter_admin_token', 'token')
    localStorage.setItem('asterrouter_admin_user', JSON.stringify(makeAuthUser({ role: 'super_admin' })))
    await router.push('/')
    expect(router.currentRoute.value.fullPath).toBe('/console/workbench')
  })

  it('opens the service portal for a developer', async () => {
    localStorage.setItem('asterrouter_admin_token', 'token')
    localStorage.setItem('asterrouter_admin_user', JSON.stringify(makeAuthUser({ role: 'developer' })))
    await router.push('/')
    expect(router.currentRoute.value.fullPath).toBe('/portal/overview')
  })

  it('preserves an anonymous protected target through login', async () => {
    await router.push('/console/model-services?status=active')
    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.redirect).toBe('/console/model-services?status=active')
  })

  it('prevents a developer from entering the management console', async () => {
    localStorage.setItem('asterrouter_admin_token', 'token')
    localStorage.setItem('asterrouter_admin_user', JSON.stringify(makeAuthUser({ role: 'developer' })))
    await router.push('/console/organization')
    expect(router.currentRoute.value.fullPath).toBe('/portal/overview')
  })

  it('registers only the two product entry trees', () => {
    const paths = router.getRoutes().map((route) => route.path)
    for (const legacy of ['/admin', '/operator', '/customer', '/platform']) expect(paths).not.toContain(legacy)
    for (const current of [
      '/console/workbench', '/console/applications', '/console/model-services',
      '/console/policies/access', '/console/policies/routing', '/console/usage',
      '/console/organization', '/console/system', '/portal/overview',
      '/portal/applications', '/portal/access', '/portal/usage', '/portal/account'
    ]) expect(paths).toContain(current)
  })

  it('registers every public account workflow as a first-class route', () => {
    for (const path of ['/login', '/register', '/forgot-password', '/resend-verification', '/reset-password', '/verify-email']) {
      expect(router.getRoutes().some((route) => route.path === path)).toBe(true)
    }
  })
})
