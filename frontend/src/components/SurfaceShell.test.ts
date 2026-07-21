import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { defineComponent } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getCurrentUser } from '@/api/auth'
import { getPluginCatalog, getPluginFrontendContribution } from '@/api/plugins'
import i18n, { setLocale } from '@/i18n'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { makeAuthUser, makePublicSettings } from '@/test/fixtures'
import SurfaceShell from './SurfaceShell.vue'

vi.mock('@/api/auth', () => ({
  completeTOTPLogin: vi.fn(),
  getCurrentUser: vi.fn(),
  login: vi.fn()
}))
vi.mock('@/api/customer', () => ({
  getCustomerNotifications: vi.fn().mockResolvedValue({ items: [], total: 0, unread: 0, limit: 20, offset: 0 }),
  markAllCustomerNotificationsRead: vi.fn(),
  markCustomerNotificationRead: vi.fn()
}))
vi.mock('@/api/plugins', () => ({
  getPluginCatalog: vi.fn().mockResolvedValue({ summary: { total: 0, enabled: 0, free: 0, paid_locked: 0, configurable: 0 }, plugins: [] }),
  getPluginFrontendContribution: vi.fn()
}))

const icon = defineComponent({ template: '<span aria-hidden="true"></span>' })

describe('SurfaceShell', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('en-US')
    vi.mocked(getCurrentUser).mockResolvedValue(makeAuthUser({ role: 'demo_admin' }))
  })

  async function mountShell(enabledProfiles = ['personal', 'relay_operator', 'enterprise']) {
    const pinia = createPinia()
    setActivePinia(pinia)
    const app = useAppStore()
    app.publicSettings = makePublicSettings({
      demo_mode: true,
      enabled_profiles: enabledProfiles
    })
    const auth = useAuthStore()
    auth.token = 'test-token'
    auth.user = makeAuthUser({ role: 'demo_admin' })

    const child = defineComponent({ template: '<main><h1>Overview</h1></main>' })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/console/overview', component: child, meta: { titleKey: 'console.overview', descriptionKey: 'console.subtitle' } },
        { path: '/login', component: child },
        { path: '/:pathMatch(.*)*', component: child }
      ]
    })
    await router.push('/console/overview')
    await router.isReady()

    const wrapper = mount(SurfaceShell, {
      props: {
        homeTo: '/console/overview',
        navLabel: 'nav.console',
        surface: 'personal',
        navGroups: [
          { label: 'nav.overview', items: [{ to: '/console/overview', label: 'console.overview', icon }] },
          { label: 'nav.inference', items: [] }
        ]
      },
      global: { plugins: [pinia, router, i18n] }
    })
    return { wrapper, router }
  }

  it('renders allowed workspace links for an operator-capable user', async () => {
    const { wrapper } = await mountShell()

    expect(wrapper.get('nav').attributes('aria-label')).toBe('Console')
    expect(wrapper.findAll('a').map((link) => link.text())).toEqual(expect.arrayContaining([
      expect.stringContaining('Operator'),
      expect.stringContaining('Customer Portal'),
      expect.stringContaining('Admin'),
      expect.stringContaining('Portal')
    ]))

    wrapper.unmount()
  })

  it('hides every other workspace when only the current deployment profile is enabled', async () => {
    const { wrapper } = await mountShell(['personal'])

    expect(wrapper.find('.sidebar-workspaces').exists()).toBe(false)
    expect(wrapper.findAll('a').map((link) => link.text()).join(' ')).not.toContain('Operator')
    expect(wrapper.findAll('a').map((link) => link.text()).join(' ')).not.toContain('Admin')
    expect(wrapper.findAll('a').map((link) => link.text()).join(' ')).not.toContain('Platform')

    wrapper.unmount()
  })

  it('shows installed personal plugin workbenches in their own navigation group', async () => {
    vi.mocked(getPluginCatalog).mockResolvedValueOnce({
      summary: { total: 1, enabled: 1, free: 1, paid_locked: 0, configurable: 0 },
      plugins: [{
        id: 'imagegen',
        plugin_id: 'com.asterrouter.imagegen.workbench',
        name: '图片生成工作台',
        description: 'Image creation',
        category: 'content',
        type: 'remote',
        tier: 'free_core',
        version: '0.3.2',
        vendor: 'AsterCloud',
        status: 'enabled',
        entitlement_status: 'free',
        surfaces: ['personal'],
        entry_point: '/admin/plugins',
        configurable: false,
        packages: [{ install_status: 'installed' } as never],
        created_at: '',
        updated_at: ''
      }]
    })
    vi.mocked(getPluginFrontendContribution).mockResolvedValueOnce({
      schema_version: 'astercloud.plugin-frontend-contribution.v1',
      plugin_id: 'com.asterrouter.imagegen.workbench',
      surfaces: [{ surface: 'console.plugins', slot: 'plugin-workbench', title: '图片生成工作台', asset: 'assets/index.js' }]
    })
    const { wrapper } = await mountShell()
    await flushPromises()
    const pluginNavigation = wrapper.get('[data-installed-plugin-navigation]')
    expect(pluginNavigation.text()).toContain('图片生成工作台')
    expect(pluginNavigation.get('.sidebar-plugin-count').text()).toBe('1')
    expect(pluginNavigation.get('a').attributes('href')).toBe('/console/plugins/com.asterrouter.imagegen.workbench/workbench')
    wrapper.unmount()
  })

  it('does not expose disabled or uninstalled plugins as personal launchers', async () => {
    vi.mocked(getPluginCatalog).mockResolvedValueOnce({
      summary: { total: 2, enabled: 0, free: 2, paid_locked: 0, configurable: 0 },
      plugins: [
        {
          id: 'disabled-imagegen',
          plugin_id: 'com.asterrouter.imagegen.disabled',
          name: 'Disabled workbench',
          description: '',
          category: 'content',
          type: 'remote',
          tier: 'free_core',
          version: '0.3.2',
          vendor: 'AsterCloud',
          status: 'disabled',
          entitlement_status: 'free',
          surfaces: ['personal'],
          entry_point: '/admin/plugins',
          configurable: false,
          packages: [{ install_status: 'installed' } as never],
          created_at: '',
          updated_at: ''
        },
        {
          id: 'remote-imagegen',
          plugin_id: 'com.asterrouter.imagegen.remote',
          name: 'Remote workbench',
          description: '',
          category: 'content',
          type: 'remote',
          tier: 'free_core',
          version: '0.3.2',
          vendor: 'AsterCloud',
          status: 'enabled',
          entitlement_status: 'free',
          surfaces: ['personal'],
          entry_point: '/admin/plugins',
          configurable: false,
          packages: [{ install_status: 'not_downloaded' } as never],
          created_at: '',
          updated_at: ''
        }
      ]
    })

    const { wrapper } = await mountShell()
    await flushPromises()

    expect(wrapper.find('[data-installed-plugin-navigation]').exists()).toBe(false)
    expect(getPluginFrontendContribution).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('persists theme and sidebar state and exposes the mobile menu', async () => {
    const { wrapper } = await mountShell()

    await wrapper.get('button[aria-label="Open navigation"]').trigger('click')
    expect(wrapper.get('aside').classes()).toContain('mobile-open')
    await wrapper.get('button[aria-label="Close navigation"]').trigger('click')
    expect(wrapper.get('aside').classes()).not.toContain('mobile-open')

    await wrapper.get('button[title="Dark mode"]').trigger('click')
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(localStorage.getItem('asterrouter_theme')).toBe('dark')

    await wrapper.get('.sidebar-collapse').trigger('click')
    expect(wrapper.get('aside').classes()).toContain('collapsed')
    expect(localStorage.getItem('asterrouter_sidebar_collapsed')).toBe('true')

    wrapper.unmount()
  })
})
