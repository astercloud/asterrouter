import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { defineComponent } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getCurrentUser } from '@/api/auth'
import { getPluginCatalog, getPluginWorkbench } from '@/api/plugins'
import i18n, { setLocale } from '@/i18n'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { makeAuthUser, makePublicSettings } from '@/test/fixtures'
import ProductShell from './ProductShell.vue'

vi.mock('@/api/auth', () => ({ completeTOTPLogin: vi.fn(), getCurrentUser: vi.fn(), login: vi.fn() }))
vi.mock('@/api/plugins', () => ({
  getPluginCatalog: vi.fn().mockResolvedValue({ summary: { total: 0, enabled: 0, free: 0, paid_locked: 0, configurable: 0 }, plugins: [] }),
  getPluginWorkbench: vi.fn()
}))

const icon = defineComponent({ template: '<span aria-hidden="true"></span>' })

describe('ProductShell', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('en-US')
    vi.mocked(getCurrentUser).mockResolvedValue(makeAuthUser({ role: 'super_admin' }))
  })

  async function mountShell() {
    const pinia = createPinia()
    setActivePinia(pinia)
    useAppStore().publicSettings = makePublicSettings({ demo_mode: true })
    const auth = useAuthStore()
    auth.token = 'test-token'
    auth.user = makeAuthUser({ role: 'super_admin' })
    const child = defineComponent({ template: '<main><h1>Workbench</h1></main>' })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/console/workbench', component: child, meta: { titleKey: 'console.workbench', descriptionKey: 'console.workbenchSubtitle' } }, { path: '/:pathMatch(.*)*', component: child }]
    })
    await router.push('/console/workbench')
    await router.isReady()
    const wrapper = mount(ProductShell, {
      props: {
        homeTo: '/console/workbench', navLabel: 'nav.console', entry: 'console',
        navGroups: [
          { label: 'nav.enterpriseManagement', items: [{ to: '/console/workbench', label: 'console.workbench', icon }] },
          { label: 'nav.systemManagement', items: [] }
        ]
      },
      global: { plugins: [pinia, router, i18n] }
    })
    return { wrapper }
  }

  it('renders enterprise navigation without workspace switching', async () => {
    const { wrapper } = await mountShell()
    expect(wrapper.get('nav').attributes('aria-label')).toBe('Console')
    expect(wrapper.text()).toContain('Enterprise management')
    expect(wrapper.find('.sidebar-workspaces').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows installed plugin workbenches under system management', async () => {
    vi.mocked(getPluginCatalog).mockResolvedValueOnce({
      summary: { total: 1, enabled: 1, free: 1, paid_locked: 0, configurable: 0 },
      plugins: [{
        id: 'imagegen', plugin_id: 'com.asterrouter.imagegen.workbench', name: 'Image Workbench', description: 'Image creation',
        category: 'content', type: 'remote', tier: 'free_core', version: '1.0.0', vendor: 'AsterCloud', status: 'enabled',
        entitlement_status: 'free', entry_point: '', configurable: false,
        packages: [{ install_status: 'installed' } as never], created_at: '', updated_at: ''
      }]
    })
    vi.mocked(getPluginWorkbench).mockResolvedValueOnce({
      schema_version: 'astercloud.plugin-workbench.v1', plugin_id: 'com.asterrouter.imagegen.workbench',
      workbench: { title: 'Image Workbench', asset: 'assets/index.js' }
    })
    const { wrapper } = await mountShell()
    await flushPromises()
    expect(wrapper.get('[data-installed-plugin-navigation] a').attributes('href')).toBe('/console/system/plugins/com.asterrouter.imagegen.workbench/workbench')
    wrapper.unmount()
  })

  it('persists theme and sidebar state and exposes the mobile menu', async () => {
    const { wrapper } = await mountShell()
    await wrapper.get('button[aria-label="Open navigation"]').trigger('click')
    expect(wrapper.get('aside').classes()).toContain('mobile-open')
    await wrapper.get('button[aria-label="Close navigation"]').trigger('click')
    await wrapper.get('button[title="Dark mode"]').trigger('click')
    expect(document.documentElement.dataset.theme).toBe('dark')
    await wrapper.get('.sidebar-collapse').trigger('click')
    expect(localStorage.getItem('asterrouter_sidebar_collapsed')).toBe('true')
    wrapper.unmount()
  })
})
