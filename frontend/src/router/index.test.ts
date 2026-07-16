import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getPublicSettings } from '@/api/settings'
import { makeAuthUser, makePublicSettings } from '@/test/fixtures'
import router, { clearPublicSettingsCache } from './index'

vi.mock('@/api/settings', () => ({ getPublicSettings: vi.fn() }))

const getPublicSettingsMock = vi.mocked(getPublicSettings)

describe('router guards', () => {
  beforeEach(async () => {
    getPublicSettingsMock.mockReset()
    getPublicSettingsMock.mockResolvedValue(makePublicSettings())
    clearPublicSettingsCache()
    await router.replace('/legal/test-fixture')
    clearPublicSettingsCache()
    getPublicSettingsMock.mockReset()
  })

  it('sends an incomplete deployment to setup', async () => {
    getPublicSettingsMock.mockResolvedValue(makePublicSettings({ setup_completed: false }))

    await router.push('/')

    expect(router.currentRoute.value.fullPath).toBe('/setup')
  })

  it('uses the enabled default profile as the authenticated entry', async () => {
    localStorage.setItem('asterrouter_admin_token', 'token')
    getPublicSettingsMock.mockResolvedValue(makePublicSettings({ default_profile: 'personal', enabled_profiles: ['personal'] }))

    await router.push('/')

    expect(router.currentRoute.value.fullPath).toBe('/console/overview')
  })

  it('routes relay customers and operators to different entries', async () => {
    localStorage.setItem('asterrouter_admin_token', 'token')
    getPublicSettingsMock.mockResolvedValue(makePublicSettings({ default_profile: 'relay_operator', enabled_profiles: ['relay_operator'] }))
    localStorage.setItem('asterrouter_admin_user', JSON.stringify(makeAuthUser({ role: 'developer' })))

    await router.push('/')
    expect(router.currentRoute.value.fullPath).toBe('/customer/overview')

    clearPublicSettingsCache()
    localStorage.setItem('asterrouter_admin_user', JSON.stringify(makeAuthUser({ role: 'super_admin' })))
    await router.replace('/login')
    await router.push('/')
    expect(router.currentRoute.value.fullPath).toBe('/operator/overview')
  })

  it('redirects anonymous protected navigation and preserves the target', async () => {
    getPublicSettingsMock.mockResolvedValue(makePublicSettings())

    await router.push('/admin/providers?status=active')

    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.redirect).toBe('/admin/providers?status=active')
  })

  it('redirects a disabled surface to the configured entry', async () => {
    localStorage.setItem('asterrouter_admin_token', 'token')
    getPublicSettingsMock.mockResolvedValue(makePublicSettings({ default_profile: 'personal', enabled_profiles: ['personal'] }))

    await router.push('/admin/dashboard')

    expect(router.currentRoute.value.fullPath).toBe('/console/overview')
  })

  it('does not render unavailable administrator surfaces for a developer session', async () => {
    localStorage.setItem('asterrouter_admin_token', 'token')
    localStorage.setItem('asterrouter_admin_user', JSON.stringify(makeAuthUser({ role: 'developer', allowed_surfaces: ['portal', 'customer'] })))
    getPublicSettingsMock.mockResolvedValue(makePublicSettings({ default_profile: 'relay_operator', enabled_profiles: ['relay_operator', 'enterprise'] }))

    await router.push('/admin/dashboard')
    expect(router.currentRoute.value.fullPath).toBe('/customer/overview')

    await router.push('/operator/overview')
    expect(router.currentRoute.value.fullPath).toBe('/customer/overview')
  })

  it('honors server-derived surface bindings for a developer session', async () => {
    localStorage.setItem('asterrouter_admin_token', 'token')
    localStorage.setItem('asterrouter_admin_user', JSON.stringify(makeAuthUser({ role: 'developer', allowed_surfaces: ['portal', 'customer', 'relay_operator'] })))
    getPublicSettingsMock.mockResolvedValue(makePublicSettings({ default_profile: 'relay_operator', enabled_profiles: ['relay_operator'] }))

    await router.push('/operator/overview')
    expect(router.currentRoute.value.fullPath).toBe('/operator/overview')
  })

  it('uses the platform entry only for an explicitly bound platform operator', async () => {
    localStorage.setItem('asterrouter_admin_token', 'token')
    localStorage.setItem('asterrouter_admin_user', JSON.stringify(makeAuthUser({
      role: 'platform_admin',
      allowed_surfaces: ['platform']
    })))
    getPublicSettingsMock.mockResolvedValue(makePublicSettings({ default_profile: 'platform', enabled_profiles: ['platform'] }))

    await router.push('/')

    expect(router.currentRoute.value.fullPath).toBe('/platform/overview')
  })

  it('redirects an unbound user away from the enabled platform surface', async () => {
    localStorage.setItem('asterrouter_admin_token', 'token')
    localStorage.setItem('asterrouter_admin_user', JSON.stringify(makeAuthUser({
      role: 'platform_admin',
      allowed_surfaces: ['personal']
    })))
    getPublicSettingsMock.mockResolvedValue(makePublicSettings({ default_profile: 'personal', enabled_profiles: ['personal', 'platform'] }))

    await router.push('/platform/overview')

    expect(router.currentRoute.value.fullPath).toBe('/console/overview')
  })

  it('passes an explicit platform surface to shared operation views', () => {
    for (const path of ['/platform/ai-jobs', '/platform/artifacts']) {
      const route = router.getRoutes().find((item) => item.path === path)
      expect(route?.props.default).toEqual({ surface: 'platform' })
    }
  })
})
