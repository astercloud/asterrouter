import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as plugins from '@/api/plugins'
import AdminPluginsView from './AdminPluginsView.vue'

const { routerPush } = vi.hoisted(() => ({ routerPush: vi.fn() }))

vi.mock('vue-router', () => ({ useRouter: () => ({ push: routerPush }) }))

vi.mock('@/api/plugins', () => ({
  activateOfficialLicense: vi.fn(),
  createPluginAPIToken: vi.fn(),
  deleteArtifactSinkDestination: vi.fn(),
  disablePlugin: vi.fn(),
  downloadPluginPackage: vi.fn(),
  enablePlugin: vi.fn(),
  getOfficialCatalogStatus: vi.fn(),
  getOfficialFeedClientInfo: vi.fn(),
  getOfficialFeedStatuses: vi.fn(),
  getOfficialFeedSyncRuns: vi.fn(),
  getOfficialLicenseStatus: vi.fn(),
  getArtifactSinkDestinations: vi.fn(),
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
  upsertArtifactSinkDestination: vi.fn(),
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
  entry_point: '',
  configurable: true,
  packages: [],
  created_at: '2026-07-14T00:00:00Z',
  updated_at: '2026-07-14T00:00:00Z'
}

const activeAPIToken = {
  id: 'pat-browser',
  name: 'Browser plugin token',
  plugin_id: 'com.asterrouter.notification.webhook',
  token_prefix: 'arpt_browser',
  scopes: ['catalog:read', 'plugin:action'],
  status: 'active',
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
  vi.mocked(plugins.getArtifactSinkDestinations).mockResolvedValue([])
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
  afterEach(() => {
    vi.unstubAllGlobals()
  })

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
    expect(wrapper.get('[data-section="registry"]').text()).toContain('No packages are available for this plugin.')

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

  it('uses the dedicated destination manager for the artifact sink plugin', async () => {
    vi.mocked(plugins.getPluginCatalog).mockResolvedValue({
      summary: { total: 1, enabled: 0, free: 1, paid_locked: 0, configurable: 1 },
      plugins: [{
        ...catalogPlugin,
        id: 'com.asterrouter.artifact.s3-compatible-sink',
        plugin_id: 'com.asterrouter.artifact.s3-compatible-sink',
        name: 'S3-compatible Artifact Delivery',
        category: 'artifact_sink',
        status: 'disabled'
      }]
    })
    const wrapper = mount(AdminPluginsView, { global: { plugins: [i18n] } })
    await flushPromises()
    await wrapper.get('[data-tab="registry"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-artifact-sinks]').exists()).toBe(true)
    expect(plugins.getArtifactSinkDestinations).toHaveBeenCalledWith('com.asterrouter.artifact.s3-compatible-sink')
    expect(wrapper.text()).not.toContain('Sidecar')

    wrapper.unmount()
  })

  it('surfaces the installed video generation workbench in navigation and featured workbenches', async () => {
    const videoPlugin = {
      ...catalogPlugin,
      id: 'com.asterrouter.videogen.workbench',
      plugin_id: 'com.asterrouter.videogen.workbench',
      name: 'Video generation workbench',
      description: 'Create videos from cases, storyboards, and prompts.',
      category: 'content',
      type: 'remote',
      tier: 'free_core',
      version: '0.2.1',
      vendor: 'AsterCloud',
      status: 'enabled',
      frontend_available: true,
      packages: [{
        plugin_id: 'com.asterrouter.videogen.workbench',
        package_id: 'pkg-video',
        version: '0.2.1',
        channel: 'stable',
        os: 'darwin',
        arch: 'arm64',
        sha256: 'sha256:video',
        size_bytes: 1,
        required_entitlement: false,
        revoked: false,
        revoked_by_advisory: false,
        compatible: true,
        install_status: 'installed'
      }]
    }
    vi.mocked(plugins.getPluginCatalog).mockResolvedValue({
      summary: { total: 2, enabled: 2, free: 2, paid_locked: 0, configurable: 1 },
      plugins: [{ ...catalogPlugin, status: 'enabled' }, videoPlugin]
    })

    const wrapper = mount(AdminPluginsView, { global: { plugins: [i18n] } })
    await flushPromises()

    expect(wrapper.get('[data-featured-plugin="videogen"] h2').text()).toBe('Video generation workbench')
    const videoNavigationItem = wrapper.findAll('.plugin-launcher-item').find((item) => item.text().includes('Video generation workbench'))
    expect(videoNavigationItem).toBeDefined()
    expect(videoNavigationItem?.find('button.button').exists()).toBe(true)
    await videoNavigationItem?.get('button.button').trigger('click')
    expect(routerPush).toHaveBeenCalledWith('/console/system/plugins/com.asterrouter.videogen.workbench/workbench')

    wrapper.unmount()
  })

  it('opens any installed plugin that exposes a frontend contribution', async () => {
    const monitorPrice = {
      ...catalogPlugin,
      id: 'com.asterrouter.monitorprice.workbench',
      plugin_id: 'com.asterrouter.monitorprice.workbench',
      name: 'MonitorPrice',
      category: 'finops',
      type: 'remote',
      status: 'enabled',
      frontend_available: true,
      packages: [{
        plugin_id: 'com.asterrouter.monitorprice.workbench',
        package_id: 'pkg-monitorprice',
        version: '0.1.0',
        channel: 'stable',
        os: 'darwin',
        arch: 'arm64',
        sha256: 'sha256:monitorprice',
        size_bytes: 1,
        required_entitlement: false,
        revoked: false,
        revoked_by_advisory: false,
        compatible: true,
        install_status: 'installed'
      }]
    }
    vi.mocked(plugins.getPluginCatalog).mockResolvedValue({
      summary: { total: 2, enabled: 2, free: 2, paid_locked: 0, configurable: 1 },
      plugins: [{ ...catalogPlugin, status: 'enabled' }, monitorPrice]
    })

    const wrapper = mount(AdminPluginsView, { global: { plugins: [i18n] } })
    await flushPromises()

    const item = wrapper.findAll('.plugin-launcher-item').find((entry) => entry.text().includes('MonitorPrice'))
    expect(item).toBeDefined()
    expect(item?.find('button.button').exists()).toBe(true)
    wrapper.unmount()
  })

  it('does not let an older refresh overwrite a revoked API token', async () => {
    vi.mocked(plugins.getPluginAPITokens).mockResolvedValueOnce([activeAPIToken])
    vi.mocked(plugins.revokePluginAPIToken).mockResolvedValue({
      ...activeAPIToken,
      status: 'revoked',
      updated_at: '2026-07-14T00:01:00Z'
    })
    vi.stubGlobal('confirm', vi.fn(() => true))
    const wrapper = mount(AdminPluginsView, { global: { plugins: [i18n] } })
    await flushPromises()
    await wrapper.get('[data-tab="api"]').trigger('click')

    let resolveStaleTokens!: (tokens: typeof activeAPIToken[]) => void
    vi.mocked(plugins.getPluginAPITokens).mockReturnValueOnce(new Promise((resolve) => {
      resolveStaleTokens = resolve
    }))
    await wrapper.findAll('.plugin-page-actions button').find((button) => button.text().includes('Refresh'))!.trigger('click')
    await wrapper.get('button[title="Revoke token"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-section="api"] tbody tr').text()).toContain('revoked')

    resolveStaleTokens([activeAPIToken])
    await flushPromises()
    expect(wrapper.get('[data-section="api"] tbody tr').text()).toContain('revoked')

    wrapper.unmount()
  })
})
