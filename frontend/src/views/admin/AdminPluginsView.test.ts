import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as plugins from '@/api/plugins'
import AdminPluginsView from './AdminPluginsView.vue'

vi.mock('@/api/plugins', () => ({
  activateOfficialLicense: vi.fn(),
  createPluginAPIToken: vi.fn(),
  disablePlugin: vi.fn(),
  downloadPluginPackage: vi.fn(),
  enablePlugin: vi.fn(),
  getOfficialCatalogStatus: vi.fn(),
  getOfficialFeedClientInfo: vi.fn(),
  getOfficialFeedStatuses: vi.fn(),
  getOfficialFeedSyncRuns: vi.fn(),
  getOfficialLicenseStatus: vi.fn(),
  getPluginAPITokens: vi.fn(),
  getPluginCatalog: vi.fn(),
  getPluginConfig: vi.fn(),
  getPluginDeliveries: vi.fn(),
  getSidecarRuntimeStatus: vi.fn(),
  importOfficialFeed: vi.fn(),
  importOfficialLicense: vi.fn(),
  importPluginPackage: vi.fn(),
  installPluginPackage: vi.fn(),
  redeemOfficialLicense: vi.fn(),
  revokePluginAPIToken: vi.fn(),
  syncOfficialCatalog: vi.fn(),
  syncOfficialFeed: vi.fn(),
  uninstallPluginPackage: vi.fn(),
  updatePluginConfig: vi.fn()
}))

const catalogPlugin = {
  id: 'plugin-webhook',
  plugin_id: 'com.asterrouter.notification.webhook',
  name: 'Webhook notifications',
  description: 'Deliver alerts to a signed webhook endpoint.',
  category: 'notification',
  type: 'builtin',
  tier: 'core',
  version: '1.0.0',
  vendor: 'AsterRouter',
  status: 'enabled',
  entitlement_status: 'included',
  surfaces: ['platform'],
  entry_point: '',
  configurable: true,
  packages: [],
  created_at: '2026-07-14T00:00:00Z',
  updated_at: '2026-07-14T00:00:00Z'
}

function mockPluginState(options: { trust?: boolean; paidLocked?: number; enabled?: number } = {}) {
  const trust = options.trust ?? true
  const paidLocked = options.paidLocked ?? 0
  const enabled = options.enabled ?? 1

  vi.mocked(plugins.getPluginCatalog).mockResolvedValue({
    summary: { total: 1, enabled, free: 1, paid_locked: paidLocked, configurable: 1 },
    plugins: [{ ...catalogPlugin, status: enabled ? 'enabled' : 'disabled' }]
  })
  vi.mocked(plugins.getOfficialCatalogStatus).mockResolvedValue({
    mode: 'online',
    source_url: 'https://catalog.example.test/plugins.json',
    trust_configured: trust,
    catalog_version: 1,
    payload_sha256: 'sha256:test',
    key_id: trust ? 'test-key' : '',
    plugin_count: 1,
    advisory_count: 0,
    status: trust ? 'succeeded' : 'disabled'
  })
  vi.mocked(plugins.getOfficialLicenseStatus).mockResolvedValue({
    configured: false,
    status: 'not_imported',
    entitlements: []
  })
  vi.mocked(plugins.getPluginAPITokens).mockResolvedValue([])
  vi.mocked(plugins.getOfficialFeedStatuses).mockResolvedValue([])
  vi.mocked(plugins.getOfficialFeedSyncRuns).mockResolvedValue([])
  vi.mocked(plugins.getSidecarRuntimeStatus).mockResolvedValue({
    plugin_id: catalogPlugin.id,
    enabled: Boolean(enabled),
    installed: true,
    running: Boolean(enabled),
    supervised: true,
    supervisor_state: enabled ? 'running' : 'stopped'
  })
}

describe('AdminPluginsView workbench', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('en-US')
    mockPluginState()
  })

  it('opens on the workbench and keeps each operational area behind a dedicated tab', async () => {
    const wrapper = mount(AdminPluginsView, { global: { plugins: [i18n] } })
    await flushPromises()

    expect(wrapper.findAll('.plugin-center-tab')).toHaveLength(5)
    expect(wrapper.get('[data-section="workbench"]').isVisible()).toBe(true)
    expect(wrapper.text()).toContain('4/4 healthy')
    expect(wrapper.find('[data-section="registry"]').exists()).toBe(false)

    await wrapper.get('[data-tab="registry"]').trigger('click')
    expect(wrapper.get('[data-section="registry"]').isVisible()).toBe(true)
    expect(wrapper.findAll('.plugin-tree-item')).toHaveLength(1)

    await wrapper.get('[data-tab="distribution"]').trigger('click')
    expect(wrapper.get('[data-section="distribution"]').text()).toContain('Official catalog')
    expect(wrapper.get('[data-section="distribution"]').text()).toContain('Official License')

    await wrapper.get('[data-tab="feeds"]').trigger('click')
    expect(wrapper.get('[data-section="feeds"]').text()).toContain('Official encrypted feeds')

    await wrapper.get('[data-tab="api"]').trigger('click')
    expect(wrapper.get('[data-section="api"]').text()).toContain('Plugin Open API')

    wrapper.unmount()
  })

  it('surfaces trust, entitlement, and runtime risks as actionable checklist items', async () => {
    mockPluginState({ trust: false, paidLocked: 1, enabled: 0 })
    const wrapper = mount(AdminPluginsView, { global: { plugins: [i18n] } })
    await flushPromises()

    expect(wrapper.text()).toContain('1/4 healthy')
    expect(wrapper.findAll('.workbench-state-icon.attention')).toHaveLength(3)
    expect(wrapper.text()).toContain('The catalog has not synchronized')
    expect(wrapper.text()).toContain('Locked plugins need')
    expect(wrapper.text()).toContain('No plugins are enabled')

    wrapper.unmount()
  })
})
