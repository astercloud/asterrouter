<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { Boxes, CheckCircle2, Copy, Download, FileClock, LockKeyhole, Plus, Plug, RefreshCw, Search, Settings2, Trash2, Upload, X, XCircle } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import {
  activateOfficialLicense,
  createPluginAPIToken,
  disablePlugin,
  downloadPluginPackage,
  enablePlugin,
  getOfficialCatalogStatus,
  getOfficialFeedClientInfo,
  getOfficialFeedStatuses,
  getOfficialFeedSyncRuns,
  getOfficialLicenseStatus,
  getPluginAPITokens,
  getPluginCatalog,
  getPluginConfig,
  getPluginDeliveries,
  getSidecarRuntimeStatus,
  importOfficialLicense,
  importOfficialFeed,
  redeemOfficialLicense,
  revokePluginAPIToken,
  importPluginPackage,
  installPluginPackage,
  syncOfficialCatalog,
  syncOfficialFeed,
  uninstallPluginPackage,
  updatePluginConfig
} from '@/api/plugins'
import type { OfficialCatalogStatus, OfficialFeedClientInfo, OfficialFeedStatus, OfficialFeedSyncRun, OfficialLicenseStatus, Plugin, PluginAPIToken, PluginCatalog, PluginConfig, PluginDeliveryAttempt, PluginPackage, SidecarRuntimeStatus } from '@/types'

const { t } = useI18n()
const loading = ref(false)
const catalogStatusLoading = ref(false)
const catalogSyncing = ref(false)
const licenseLoading = ref(false)
const licenseSaving = ref(false)
const actionID = ref('')
const packageDownloadingID = ref('')
const packageImportingID = ref('')
const packageInstallingID = ref('')
const error = ref('')
const message = ref('')
const query = ref('')
const categoryFilter = ref('')
const tierFilter = ref('')
const statusFilter = ref('')
const selectedPlugin = ref<Plugin | null>(null)
const configPlugin = ref<Plugin | null>(null)
const configLoading = ref(false)
const configSaving = ref(false)
const pluginConfig = ref<PluginConfig | null>(null)
const deliveryPlugin = ref<Plugin | null>(null)
const deliveries = ref<PluginDeliveryAttempt[]>([])
const deliveryLoading = ref(false)
const deliveryStatusFilter = ref('')
const licenseModal = ref<'activate' | 'import' | 'redeem' | null>(null)
const packageImportTarget = ref<{ plugin: Plugin; pkg: PluginPackage } | null>(null)
const packageImportFileJSON = ref('')
const licenseForm = ref({
  code: '',
  licenseID: '',
  activationSecret: '',
  instanceID: '',
  fingerprint: '',
  displayName: '',
  fileJSON: ''
})
const configForm = ref({
  secrets: {} as Record<string, string>,
  minSeverity: 'warning',
  alertTypes: ''
})
const catalog = ref<PluginCatalog>({
  summary: { total: 0, enabled: 0, free: 0, paid_locked: 0, configurable: 0 },
  plugins: []
})
const officialCatalogStatus = ref<OfficialCatalogStatus | null>(null)
const officialLicenseStatus = ref<OfficialLicenseStatus | null>(null)
const runtimeStatus = ref<SidecarRuntimeStatus | null>(null)
const runtimeStatusLoading = ref(false)
const apiTokenSaving = ref(false)
const apiTokenRevokeID = ref('')
const apiTokenModal = ref(false)
const apiTokenSecret = ref('')
const apiTokens = ref<PluginAPIToken[]>([])
const currentPluginSurface = window.location.pathname.startsWith('/console')
  ? 'personal'
  : window.location.pathname.startsWith('/operator')
    ? 'relay_operator'
    : window.location.pathname.startsWith('/platform')
      ? 'platform'
      : 'enterprise'
const apiTokenForm = ref({
  name: '',
  pluginID: '',
  scopes: ['catalog:read'],
  surfaces: [currentPluginSurface],
  expiresAt: ''
})
const apiTokenScopeOptions = ['catalog:read', 'plugin:read', 'plugin:action', 'artifact:write', 'job:write', 'event:read']
const apiTokenSurfaceOptions = [currentPluginSurface]
const feedClientInfo = ref<OfficialFeedClientInfo | null>(null)
const feedStatuses = ref<OfficialFeedStatus[]>([])
const feedImportModal = ref(false)
const feedImportJSON = ref('')
const feedImporting = ref(false)
const feedSyncing = ref(false)
const feedSyncServiceKey = ref('')
const feedSyncRuns = ref<OfficialFeedSyncRun[]>([])

type SecretField = {
  key: string
  labelKey: string
  inputType: 'url' | 'password'
  placeholderKey: string
}

type NotificationConfigSchema = {
  secretFields: SecretField[]
}

const notificationConfigSchemas: Record<string, NotificationConfigSchema> = {
  'com.asterrouter.notification.webhook': {
    secretFields: [
      { key: 'webhook_url', labelKey: 'plugins.webhookUrl', inputType: 'url', placeholderKey: 'plugins.keepSecret' },
      { key: 'bearer_token', labelKey: 'plugins.bearerToken', inputType: 'password', placeholderKey: 'plugins.optionalSecret' }
    ]
  },
  'com.asterrouter.notification.slack': {
    secretFields: [{ key: 'webhook_url', labelKey: 'plugins.slackWebhookUrl', inputType: 'url', placeholderKey: 'plugins.keepSecret' }]
  },
  'com.asterrouter.notification.lark': {
    secretFields: [
      { key: 'webhook_url', labelKey: 'plugins.larkWebhookUrl', inputType: 'url', placeholderKey: 'plugins.keepSecret' },
      { key: 'signing_secret', labelKey: 'plugins.signingSecret', inputType: 'password', placeholderKey: 'plugins.optionalSecret' }
    ]
  },
  'com.asterrouter.notification.wecom': {
    secretFields: [{ key: 'webhook_url', labelKey: 'plugins.wecomWebhookUrl', inputType: 'url', placeholderKey: 'plugins.keepSecret' }]
  },
  'com.asterrouter.notification.dingtalk': {
    secretFields: [
      { key: 'webhook_url', labelKey: 'plugins.dingtalkWebhookUrl', inputType: 'url', placeholderKey: 'plugins.keepSecret' },
      { key: 'signing_secret', labelKey: 'plugins.signingSecret', inputType: 'password', placeholderKey: 'plugins.optionalSecret' }
    ]
  }
}

const metrics = computed(() => [
  { label: t('plugins.total'), value: catalog.value.summary.total, sub: t('plugins.installed'), icon: Plug },
  { label: t('plugins.enabled'), value: catalog.value.summary.enabled, sub: t('plugins.runtime'), icon: CheckCircle2 },
  { label: t('plugins.free'), value: catalog.value.summary.free, sub: t('plugins.neverCharged'), icon: Boxes },
  { label: t('plugins.paidLocked'), value: catalog.value.summary.paid_locked, sub: t('plugins.requiresLicense'), icon: LockKeyhole }
])

const filteredPlugins = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return catalog.value.plugins.filter((plugin) => {
    if (categoryFilter.value && plugin.category !== categoryFilter.value) return false
    if (tierFilter.value && plugin.tier !== tierFilter.value) return false
    if (statusFilter.value && plugin.status !== statusFilter.value) return false
    if (!keyword) return true
    return [plugin.name, plugin.description, plugin.plugin_id, plugin.category, plugin.vendor, plugin.surfaces.join(' ')].some((value) =>
      value.toLowerCase().includes(keyword)
    )
  })
})

const categoryOptions = computed(() => Array.from(new Set(catalog.value.plugins.map((item) => item.category))).filter(Boolean).sort())
const tierOptions = computed(() => Array.from(new Set(catalog.value.plugins.map((item) => item.tier))).filter(Boolean).sort())
const statusOptions = computed(() => Array.from(new Set(catalog.value.plugins.map((item) => item.status))).filter(Boolean).sort())
const activeConfigSchema = computed(() => notificationConfigSchema(configPlugin.value))
const canSyncOfficialCatalog = computed(() => ['online', 'private_mirror'].includes(officialCatalogStatus.value?.mode || ''))
const feedServiceOptions = computed(() => {
  const entitled = (officialLicenseStatus.value?.entitlements || [])
    .filter((item) => item.type === 'data_feed' && item.status === 'active')
    .map((item) => item.resource_key.trim())
    .filter(Boolean)
  const cached = feedStatuses.value.map((item) => item.service_key.trim()).filter(Boolean)
  return Array.from(new Set([...entitled, ...cached])).sort()
})
const pluginTree = computed(() => {
  const groups = new Map<string, Plugin[]>()
  for (const plugin of filteredPlugins.value) {
    const key = plugin.category || t('plugins.uncategorized')
    const items = groups.get(key) || []
    items.push(plugin)
    groups.set(key, items)
  }
  return Array.from(groups.entries())
    .map(([category, plugins]) => ({
      category,
      plugins: plugins.slice().sort((left, right) => left.name.localeCompare(right.name))
    }))
    .sort((left, right) => left.category.localeCompare(right.category))
})
const activePlugin = computed(() => {
  const selectedID = selectedPlugin.value?.id
  if (selectedID) {
    const matched = filteredPlugins.value.find((plugin) => plugin.id === selectedID)
    if (matched) return matched
  }
  return filteredPlugins.value[0] || null
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [catalogData, catalogStatus, licenseStatus, tokenData, feedData, syncRuns] = await Promise.all([
      getPluginCatalog(),
      loadOfficialCatalogStatus(),
      loadOfficialLicenseStatus(),
      getPluginAPITokens(),
      getOfficialFeedStatuses().catch(() => []),
      getOfficialFeedSyncRuns().catch(() => [])
    ])
    catalog.value = catalogData
    officialCatalogStatus.value = catalogStatus
    officialLicenseStatus.value = licenseStatus
    apiTokens.value = tokenData
    feedStatuses.value = feedData
    feedClientInfo.value = licenseStatus.status === 'active' ? await getOfficialFeedClientInfo().catch(() => null) : null
    feedSyncRuns.value = syncRuns
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

async function savePluginAPIToken() {
  apiTokenSaving.value = true
  error.value = ''
  message.value = ''
  try {
    const result = await createPluginAPIToken({
      name: apiTokenForm.value.name,
      plugin_id: apiTokenForm.value.pluginID || undefined,
      scopes: apiTokenForm.value.scopes,
      surfaces: apiTokenForm.value.surfaces,
      expires_at: apiTokenForm.value.expiresAt ? new Date(apiTokenForm.value.expiresAt).toISOString() : undefined
    })
    apiTokens.value = [result.token, ...apiTokens.value]
    apiTokenSecret.value = result.secret
    apiTokenForm.value.name = ''
    apiTokenForm.value.pluginID = ''
    apiTokenForm.value.scopes = ['catalog:read']
    apiTokenForm.value.surfaces = [currentPluginSurface]
    apiTokenForm.value.expiresAt = ''
    message.value = t('plugins.apiTokenCreated')
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    apiTokenSaving.value = false
  }
}

async function revokePluginToken(token: PluginAPIToken) {
  if (token.status === 'revoked' || !window.confirm(t('plugins.revokeTokenConfirm'))) return
  apiTokenRevokeID.value = token.id
  error.value = ''
  try {
    const revoked = await revokePluginAPIToken(token.id)
    const index = apiTokens.value.findIndex((item) => item.id === token.id)
    if (index >= 0) apiTokens.value[index] = revoked
    message.value = t('plugins.apiTokenRevoked')
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    apiTokenRevokeID.value = ''
  }
}

function closeAPITokenModal() {
  apiTokenModal.value = false
  apiTokenSecret.value = ''
}

async function copyAPITokenSecret() {
  if (!apiTokenSecret.value || !navigator.clipboard) return
  await navigator.clipboard.writeText(apiTokenSecret.value)
  message.value = t('plugins.apiTokenCopied')
}

async function saveOfficialFeedImport() {
  feedImporting.value = true
  error.value = ''
  message.value = ''
  try {
    const parsed = JSON.parse(feedImportJSON.value)
    const imported = await importOfficialFeed({ file_json: parsed })
    feedStatuses.value = [imported, ...feedStatuses.value.filter((item) => item.feed_id !== imported.feed_id || item.service_key !== imported.service_key)]
    feedClientInfo.value = await getOfficialFeedClientInfo()
    feedImportJSON.value = ''
    feedImportModal.value = false
    message.value = t('plugins.feedImported')
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    feedImporting.value = false
  }
}

async function copyFeedPublicKey() {
  if (!feedClientInfo.value?.encryption_public_key || !navigator.clipboard) return
  await navigator.clipboard.writeText(feedClientInfo.value.encryption_public_key)
  message.value = t('plugins.feedPublicKeyCopied')
}

async function syncFeed() {
  const serviceKey = feedSyncServiceKey.value.trim()
  if (!serviceKey) return
  feedSyncing.value = true
  error.value = ''
  message.value = ''
  try {
    const result = await syncOfficialFeed(serviceKey)
    feedStatuses.value = await getOfficialFeedStatuses()
    feedSyncRuns.value = await getOfficialFeedSyncRuns('', 20)
    message.value = t('plugins.feedSynced', { service: result.feed.service_key, version: result.feed.feed_version })
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
    feedSyncRuns.value = await getOfficialFeedSyncRuns('', 20).catch(() => feedSyncRuns.value)
  } finally {
    feedSyncing.value = false
  }
}

async function loadRuntimeStatus(plugin: Plugin | null) {
  runtimeStatus.value = null
  if (!plugin) return
  runtimeStatusLoading.value = true
  try {
    runtimeStatus.value = await getSidecarRuntimeStatus(plugin.id)
  } catch {
    runtimeStatus.value = null
  } finally {
    runtimeStatusLoading.value = false
  }
}

async function loadOfficialCatalogStatus() {
  catalogStatusLoading.value = true
  try {
    return await getOfficialCatalogStatus()
  } finally {
    catalogStatusLoading.value = false
  }
}

async function loadOfficialLicenseStatus() {
  licenseLoading.value = true
  try {
    return await getOfficialLicenseStatus()
  } finally {
    licenseLoading.value = false
  }
}

async function syncCatalog() {
  catalogSyncing.value = true
  error.value = ''
  message.value = ''
  try {
    officialCatalogStatus.value = await syncOfficialCatalog()
    catalog.value = await getPluginCatalog()
    message.value = t('plugins.catalogSynced')
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
    try {
      officialCatalogStatus.value = await loadOfficialCatalogStatus()
    } catch {
      // Keep the original sync error visible.
    }
  } finally {
    catalogSyncing.value = false
  }
}

async function setEnabled(plugin: Plugin, enabled: boolean) {
  actionID.value = plugin.id
  error.value = ''
  message.value = ''
  try {
    if (enabled) {
      await enablePlugin(plugin.id)
      message.value = t('plugins.enabledMessage')
    } else {
      await disablePlugin(plugin.id)
      message.value = t('plugins.disabledMessage')
    }
    await load()
    const updated = catalog.value.plugins.find((item) => item.id === plugin.id)
    if (updated && selectedPlugin.value?.id === plugin.id) {
      selectedPlugin.value = updated
    }
    await loadRuntimeStatus(activePlugin.value)
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    actionID.value = ''
  }
}

function pluginPackages(plugin: Plugin): PluginPackage[] {
  return plugin.packages || []
}

function bestPackage(plugin: Plugin): PluginPackage | null {
  const packages = pluginPackages(plugin)
  return packages.find((item) => canDownloadPackage(item)) || packages[0] || null
}

function canDownloadPackage(pkg: PluginPackage | null) {
  return Boolean(
    pkg &&
      pkg.compatible &&
      !pkg.revoked &&
      pkg.cache_status !== 'cached' &&
      (!pkg.required_entitlement || officialLicenseStatus.value?.status === 'active')
  )
}

function canImportPackage(pkg: PluginPackage | null) {
  return Boolean(
    pkg &&
      pkg.compatible &&
      !pkg.revoked &&
      pkg.cache_status !== 'cached' &&
      (!pkg.required_entitlement || officialLicenseStatus.value?.status === 'active')
  )
}

function canInstallPackage(pkg: PluginPackage | null) {
  return Boolean(pkg && pkg.compatible && !pkg.revoked && pkg.cache_status === 'cached' && pkg.install_status !== 'installed')
}

function canUninstallPackage(pkg: PluginPackage | null) {
  return Boolean(pkg && pkg.install_status === 'installed')
}

async function cachePackage(plugin: Plugin, pkg: PluginPackage | null) {
  if (!pkg || !canDownloadPackage(pkg)) return
  packageDownloadingID.value = pkg.package_id
  error.value = ''
  message.value = ''
  try {
    await downloadPluginPackage(plugin.id, pkg.package_id)
    message.value = t('plugins.packageDownloaded')
    await load()
    const updated = catalog.value.plugins.find((item) => item.id === plugin.id)
    if (updated && selectedPlugin.value?.id === plugin.id) {
      selectedPlugin.value = updated
    }
    await loadRuntimeStatus(activePlugin.value)
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    packageDownloadingID.value = ''
  }
}

function openPackageImport(plugin: Plugin, pkg: PluginPackage | null) {
  if (!pkg || !canImportPackage(pkg)) return
  packageImportTarget.value = { plugin, pkg }
  packageImportFileJSON.value = ''
}

async function savePackageImport() {
  const target = packageImportTarget.value
  if (!target) return
  packageImportingID.value = target.pkg.package_id
  error.value = ''
  message.value = ''
  try {
    const parsed = JSON.parse(packageImportFileJSON.value)
    await importPluginPackage(target.plugin.id, target.pkg.package_id, { file_json: parsed })
    message.value = t('plugins.packageImported')
    packageImportTarget.value = null
    packageImportFileJSON.value = ''
    await load()
    const updated = catalog.value.plugins.find((item) => item.id === target.plugin.id)
    if (updated && selectedPlugin.value?.id === target.plugin.id) {
      selectedPlugin.value = updated
    }
    await loadRuntimeStatus(activePlugin.value)
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    packageImportingID.value = ''
  }
}

async function installPackage(plugin: Plugin, pkg: PluginPackage | null) {
  if (!pkg || !canInstallPackage(pkg)) return
  packageInstallingID.value = pkg.package_id
  error.value = ''
  message.value = ''
  try {
    await installPluginPackage(plugin.id, pkg.package_id)
    message.value = t('plugins.packageInstalled')
    await load()
    const updated = catalog.value.plugins.find((item) => item.id === plugin.id)
    if (updated && selectedPlugin.value?.id === plugin.id) {
      selectedPlugin.value = updated
    }
    await loadRuntimeStatus(activePlugin.value)
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    packageInstallingID.value = ''
  }
}

async function uninstallPackage(plugin: Plugin, pkg: PluginPackage | null) {
  if (!pkg || !canUninstallPackage(pkg)) return
  packageInstallingID.value = pkg.package_id
  error.value = ''
  message.value = ''
  try {
    await uninstallPluginPackage(plugin.id, pkg.package_id)
    message.value = t('plugins.packageUninstalled')
    await load()
    const updated = catalog.value.plugins.find((item) => item.id === plugin.id)
    if (updated && selectedPlugin.value?.id === plugin.id) {
      selectedPlugin.value = updated
    }
    await loadRuntimeStatus(activePlugin.value)
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    packageInstallingID.value = ''
  }
}

async function saveLicenseActivation() {
  licenseSaving.value = true
  error.value = ''
  message.value = ''
  try {
    officialLicenseStatus.value = await activateOfficialLicense({
      license_id: licenseForm.value.licenseID,
      activation_secret: licenseForm.value.activationSecret,
      instance_id: licenseForm.value.instanceID || undefined,
      instance_fingerprint: licenseForm.value.fingerprint || undefined,
      display_name: licenseForm.value.displayName || undefined
    })
    licenseModal.value = null
    licenseForm.value.activationSecret = ''
    message.value = t('plugins.licenseActivated')
    catalog.value = await getPluginCatalog()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    licenseSaving.value = false
  }
}

async function saveLicenseRedeem() {
  licenseSaving.value = true
  error.value = ''
  message.value = ''
  try {
    officialLicenseStatus.value = await redeemOfficialLicense({
      code: licenseForm.value.code,
      instance_id: licenseForm.value.instanceID || undefined,
      instance_fingerprint: licenseForm.value.fingerprint || undefined,
      display_name: licenseForm.value.displayName || undefined
    })
    licenseModal.value = null
    licenseForm.value.code = ''
    message.value = t('plugins.licenseRedeemed')
    catalog.value = await getPluginCatalog()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    licenseSaving.value = false
  }
}

async function saveLicenseImport() {
  licenseSaving.value = true
  error.value = ''
  message.value = ''
  try {
    const parsed = JSON.parse(licenseForm.value.fileJSON)
    officialLicenseStatus.value = await importOfficialLicense({
      file_json: parsed,
      activation_secret: licenseForm.value.activationSecret || undefined
    })
    licenseModal.value = null
    licenseForm.value.fileJSON = ''
    licenseForm.value.activationSecret = ''
    message.value = t('plugins.licenseImported')
    catalog.value = await getPluginCatalog()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    licenseSaving.value = false
  }
}

function canEnable(plugin: Plugin) {
  return plugin.status !== 'enabled' && plugin.status !== 'locked'
}

function canDisable(plugin: Plugin) {
  return plugin.status === 'enabled' && plugin.tier !== 'core'
}

function canConfigure(plugin: Plugin) {
  return plugin.configurable && plugin.category === 'notification' && plugin.status !== 'locked' && Boolean(notificationConfigSchema(plugin))
}

function notificationConfigSchema(plugin: Plugin | null): NotificationConfigSchema | null {
  if (!plugin) return null
  return notificationConfigSchemas[plugin.id] || null
}

function statusClass(status: string) {
  if (status === 'enabled') return 'status-success'
  if (status === 'locked') return 'status-warning'
  return 'status-danger'
}

function packageStatusClass(pkg: PluginPackage) {
  if (pkg.install_status === 'installed') return 'status-success'
  if (pkg.cache_status === 'cached') return 'status-success'
  if (pkg.revoked || !pkg.compatible) return 'status-danger'
  if (pkg.required_entitlement) return 'status-warning'
  return 'status-success'
}

function packageStatusLabel(pkg: PluginPackage) {
  if (pkg.install_status === 'installed') return t('plugins.packageInstalledStatus')
  if (pkg.revoked_by_advisory) return t('plugins.revokedByAdvisory')
  return pkg.cache_status || (pkg.required_entitlement ? t('plugins.packageRequiredLicense') : pkg.compatible ? t('plugins.compatible') : t('plugins.incompatible'))
}

async function openConfig(plugin: Plugin) {
  const schema = notificationConfigSchema(plugin)
  if (!schema) {
    error.value = t('plugins.configUnavailable')
    return
  }
  configPlugin.value = plugin
  pluginConfig.value = null
  const secrets: Record<string, string> = {}
  for (const field of schema.secretFields) {
    secrets[field.key] = ''
  }
  configForm.value = {
    secrets,
    minSeverity: 'warning',
    alertTypes: ''
  }
  configLoading.value = true
  error.value = ''
  try {
    const config = await getPluginConfig(plugin.id)
    pluginConfig.value = config
    configForm.value.minSeverity = config.settings.min_severity || 'warning'
    configForm.value.alertTypes = config.settings.alert_types || ''
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
    configPlugin.value = null
  } finally {
    configLoading.value = false
  }
}

async function saveConfig() {
  if (!configPlugin.value) return
  configSaving.value = true
  error.value = ''
  message.value = ''
  try {
    pluginConfig.value = await updatePluginConfig(configPlugin.value.id, {
      settings: {
        min_severity: configForm.value.minSeverity,
        alert_types: configForm.value.alertTypes
      },
      secrets: configForm.value.secrets
    })
    Object.keys(configForm.value.secrets).forEach((key) => {
      configForm.value.secrets[key] = ''
    })
    message.value = t('plugins.configSaved')
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    configSaving.value = false
  }
}

async function openDeliveries(plugin: Plugin) {
  deliveryPlugin.value = plugin
  deliveries.value = []
  deliveryLoading.value = true
  error.value = ''
  try {
    deliveries.value = await getPluginDeliveries(plugin.id, {
      limit: 25,
      status: deliveryStatusFilter.value || undefined
    })
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
    deliveryPlugin.value = null
  } finally {
    deliveryLoading.value = false
  }
}

function deliveryStatusClass(status: string) {
  if (status === 'succeeded') return 'status-success'
  if (status === 'skipped') return 'status-warning'
  return 'status-danger'
}

function catalogStatusClass(status: string) {
  if (status === 'succeeded') return 'status-success'
  if (status === 'failed') return 'status-danger'
  return 'status-warning'
}

function licenseStatusClass(status: string) {
  if (status === 'active') return 'status-success'
  if (status === 'not_imported') return 'status-warning'
  return 'status-danger'
}

function formatTime(value: string): string {
  return new Date(value).toLocaleString()
}

function formatOptionalTime(value?: string): string {
  if (!value) return '-'
  return formatTime(value)
}

function runtimeStateClass(status?: SidecarRuntimeStatus | null) {
  if (status?.running) return 'status-success'
  if (status?.supervisor_state === 'backing_off' || status?.supervisor_state === 'starting') return 'status-warning'
  if (status?.error || status?.last_error) return 'status-danger'
  return 'status-warning'
}

function shortHash(value: string): string {
  if (!value) return '-'
  if (value.length <= 18) return value
  return `${value.slice(0, 10)}...${value.slice(-6)}`
}

function formatSize(bytes: number): string {
  if (!bytes) return '-'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

watch(
  () => activePlugin.value?.id,
  () => {
    void loadRuntimeStatus(activePlugin.value)
  }
)

watch(
  feedServiceOptions,
  (options) => {
    if (!feedSyncServiceKey.value && options.length) feedSyncServiceKey.value = options[0]
  },
  { immediate: true }
)

onMounted(load)
</script>

<template>
  <main class="content crud-page">
    <section class="page-header">
      <div>
        <h1>{{ t('admin.plugins') }}</h1>
        <p>{{ t('plugins.subtitle') }}</p>
      </div>
      <button class="button secondary" :disabled="loading" @click="load">
        <RefreshCw :size="17" />
        {{ t('common.refresh') }}
      </button>
    </section>

    <div v-if="message" class="notice success">{{ message }}</div>
    <div v-if="error" class="notice">{{ error }}</div>

    <section class="metric-grid">
      <article v-for="metric in metrics" :key="metric.label" class="metric-card">
        <span class="metric-icon"><component :is="metric.icon" :size="20" /></span>
        <div>
          <span>{{ metric.label }}</span>
          <strong>{{ metric.value }}</strong>
          <small>{{ metric.sub }}</small>
        </div>
      </article>
    </section>

    <section class="panel section-gap">
      <header class="panel-header split-header">
        <div>
          <h2>{{ t('plugins.officialCatalog') }}</h2>
          <p>{{ t('plugins.officialCatalogSubtitle') }}</p>
        </div>
        <button class="button secondary" type="button" :disabled="catalogSyncing || catalogStatusLoading || !canSyncOfficialCatalog" @click="syncCatalog">
          <RefreshCw :size="15" />
          {{ catalogSyncing ? t('plugins.syncingCatalog') : t('plugins.syncCatalog') }}
        </button>
      </header>
      <div class="panel-body detail-grid">
        <div>
          <label>{{ t('plugins.catalogMode') }}</label>
          <p>{{ officialCatalogStatus?.mode || '-' }}</p>
        </div>
        <div>
          <label>{{ t('plugins.catalogStatus') }}</label>
          <p>
            <span class="pill" :class="catalogStatusClass(officialCatalogStatus?.status || '')">
              {{ officialCatalogStatus?.status || '-' }}
            </span>
          </p>
        </div>
        <div>
          <label>{{ t('plugins.catalogVersion') }}</label>
          <p>{{ officialCatalogStatus?.catalog_version || '-' }}</p>
        </div>
        <div>
          <label>{{ t('plugins.catalogPluginCount') }}</label>
          <p>{{ officialCatalogStatus?.plugin_count || 0 }}</p>
        </div>
        <div>
          <label>{{ t('plugins.catalogAdvisoryCount') }}</label>
          <p>{{ officialCatalogStatus?.advisory_count || 0 }}</p>
        </div>
        <div>
          <label>{{ t('plugins.catalogKey') }}</label>
          <p>{{ officialCatalogStatus?.key_id || '-' }}</p>
        </div>
        <div>
          <label>{{ t('plugins.catalogTrust') }}</label>
          <p>
            <span class="pill" :class="officialCatalogStatus?.trust_configured ? 'status-success' : 'status-warning'">
              {{ officialCatalogStatus?.trust_configured ? t('plugins.trustConfigured') : t('plugins.trustMissing') }}
            </span>
          </p>
        </div>
        <div>
          <label>{{ t('plugins.catalogSyncedAt') }}</label>
          <p>{{ formatOptionalTime(officialCatalogStatus?.synced_at) }}</p>
        </div>
        <div class="form-span-2">
          <label>{{ t('plugins.catalogBootstrap') }}</label>
          <p>{{ officialCatalogStatus?.bootstrap_url || '-' }}</p>
        </div>
        <div class="form-span-2">
          <label>{{ t('plugins.catalogSource') }}</label>
          <p>{{ officialCatalogStatus?.source_url || '-' }}</p>
        </div>
        <div class="form-span-2">
          <label>{{ t('plugins.catalogLicenseURL') }}</label>
          <p>{{ officialCatalogStatus?.license_url || '-' }}</p>
        </div>
        <div class="form-span-2">
          <label>{{ t('plugins.catalogRedeemURL') }}</label>
          <p>{{ officialCatalogStatus?.redeem_url || '-' }}</p>
        </div>
        <div class="form-span-2">
          <label>{{ t('plugins.catalogPayload') }}</label>
          <p>{{ shortHash(officialCatalogStatus?.payload_sha256 || '') }}</p>
        </div>
        <div v-if="officialCatalogStatus?.error" class="form-span-2">
          <label>{{ t('plugins.error') }}</label>
          <p>{{ officialCatalogStatus.error }}</p>
        </div>
      </div>
    </section>

    <section class="panel section-gap">
      <header class="panel-header split-header">
        <div>
          <h2>{{ t('plugins.officialLicense') }}</h2>
          <p>{{ t('plugins.officialLicenseSubtitle') }}</p>
        </div>
        <div class="row-actions">
          <button class="button secondary" type="button" :disabled="licenseLoading" @click="licenseModal = 'import'">
            <Download :size="15" />
            {{ t('plugins.importLicense') }}
          </button>
          <button class="button secondary" type="button" :disabled="licenseLoading" @click="licenseModal = 'activate'">
            <CheckCircle2 :size="15" />
            {{ t('plugins.activateLicense') }}
          </button>
          <button class="button secondary" type="button" :disabled="licenseLoading" @click="licenseModal = 'redeem'">
            <LockKeyhole :size="15" />
            {{ t('plugins.redeemCode') }}
          </button>
        </div>
      </header>
      <div class="panel-body detail-grid">
        <div>
          <label>{{ t('plugins.licenseStatus') }}</label>
          <p>
            <span class="pill" :class="licenseStatusClass(officialLicenseStatus?.status || '')">
              {{ officialLicenseStatus?.status || '-' }}
            </span>
          </p>
        </div>
        <div>
          <label>{{ t('plugins.licenseID') }}</label>
          <p>{{ officialLicenseStatus?.license_id || '-' }}</p>
        </div>
        <div>
          <label>{{ t('plugins.licenseEdition') }}</label>
          <p>{{ officialLicenseStatus?.edition || '-' }}</p>
        </div>
        <div>
          <label>{{ t('plugins.licenseInstance') }}</label>
          <p>{{ officialLicenseStatus?.instance_id || '-' }}</p>
        </div>
        <div>
          <label>{{ t('plugins.licenseExpiresAt') }}</label>
          <p>{{ formatOptionalTime(officialLicenseStatus?.expires_at) }}</p>
        </div>
        <div>
          <label>{{ t('plugins.licenseEntitlements') }}</label>
          <p>{{ officialLicenseStatus?.entitlements?.length || 0 }}</p>
        </div>
        <div class="form-span-2">
          <label>{{ t('plugins.licenseEnvelope') }}</label>
          <p>{{ shortHash(officialLicenseStatus?.envelope_sha256 || '') }}</p>
        </div>
      </div>
    </section>

    <section class="panel section-gap">
      <header class="panel-header split-header">
        <div>
          <h2>{{ t('plugins.officialFeeds') }}</h2>
          <p>{{ t('plugins.officialFeedsSubtitle') }}</p>
        </div>
        <div class="row-actions feed-actions">
          <select v-model="feedSyncServiceKey" class="feed-service-select" :aria-label="t('plugins.feedService')">
            <option value="">{{ t('plugins.selectFeedService') }}</option>
            <option v-for="service in feedServiceOptions" :key="service" :value="service">{{ service }}</option>
          </select>
          <button class="button secondary" type="button" :disabled="feedSyncing || !canSyncOfficialCatalog || !feedSyncServiceKey" @click="syncFeed">
            <RefreshCw :size="16" />
            {{ feedSyncing ? t('plugins.syncingFeed') : t('plugins.syncFeed') }}
          </button>
          <button class="button secondary" type="button" @click="feedImportModal = true">
            <Upload :size="16" />
            {{ t('plugins.importFeed') }}
          </button>
        </div>
      </header>
      <div v-if="feedClientInfo" class="panel-body detail-grid">
        <div>
          <label>{{ t('plugins.licenseInstance') }}</label>
          <p>{{ feedClientInfo.instance_id }}</p>
        </div>
        <div>
          <label>{{ t('plugins.licenseID') }}</label>
          <p>{{ feedClientInfo.license_id }}</p>
        </div>
        <div class="form-span-2">
          <label>{{ t('plugins.feedEncryption') }}</label>
          <p>{{ feedClientInfo.encryption_algorithm }}</p>
        </div>
        <div class="form-span-2">
          <label>{{ t('plugins.feedPublicKey') }}</label>
          <div class="inline-code-row">
            <code>{{ feedClientInfo.encryption_public_key }}</code>
            <button class="icon-button" type="button" :title="t('plugins.copyFeedPublicKey')" @click="copyFeedPublicKey">
              <Copy :size="15" />
            </button>
          </div>
        </div>
      </div>
      <div class="panel-body table-scroll feed-table-body">
        <table class="data-table crud-table">
          <thead>
            <tr>
              <th>{{ t('plugins.feedService') }}</th>
              <th>{{ t('plugins.feedVersion') }}</th>
              <th>{{ t('plugins.feedSchema') }}</th>
              <th>{{ t('plugins.feedVerification') }}</th>
              <th>{{ t('plugins.feedFreshness') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="feed in feedStatuses" :key="`${feed.service_key}:${feed.feed_id}`">
              <td>
                <strong>{{ feed.service_key }}</strong>
                <span>{{ feed.feed_id }}</span>
              </td>
              <td>
                <strong>{{ feed.feed_version }}</strong>
                <span>{{ formatSize(feed.size_bytes) }}</span>
              </td>
              <td><span>{{ feed.data_schema_version }}</span></td>
              <td>
                <span class="pill" :class="feed.signature_verified ? 'status-success' : 'status-danger'">
                  {{ feed.signature_verified ? t('plugins.signatureVerified') : t('plugins.signatureInvalid') }}
                </span>
                <span>{{ shortHash(feed.payload_sha256) }}</span>
              </td>
              <td>
                <span class="pill" :class="feed.status === 'active' ? 'status-success' : 'status-warning'">{{ feed.status }}</span>
                <span>{{ t('plugins.licenseExpiresAt') }}: {{ formatOptionalTime(feed.expires_at) }}</span>
              </td>
            </tr>
            <tr v-if="!feedStatuses.length">
              <td colspan="5" class="empty-cell">{{ t('plugins.feedEmpty') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="feedSyncRuns.length" class="panel-body table-scroll feed-runs-body">
        <table class="data-table crud-table">
          <thead>
            <tr>
              <th>{{ t('plugins.feedSyncTime') }}</th>
              <th>{{ t('plugins.feedService') }}</th>
              <th>{{ t('plugins.catalogMode') }}</th>
              <th>{{ t('plugins.catalogStatus') }}</th>
              <th>{{ t('plugins.feedRequestID') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="run in feedSyncRuns" :key="run.id">
              <td>{{ formatTime(run.started_at) }}</td>
              <td>
                <strong>{{ run.service_key }}</strong>
                <span>{{ run.feed_id || '-' }}</span>
              </td>
              <td>{{ run.mode }}</td>
              <td>
                <span class="pill" :class="run.status === 'succeeded' ? 'status-success' : 'status-danger'">{{ run.status }}</span>
                <span>{{ run.error_code || run.error || '-' }}</span>
              </td>
              <td>
                <strong>{{ run.request_id || '-' }}</strong>
                <span>{{ run.source_url || '-' }}</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section class="panel section-gap">
      <header class="panel-header split-header">
        <div>
          <h2>{{ t('plugins.openAPI') }}</h2>
          <p>{{ t('plugins.openAPISubtitle') }}</p>
        </div>
        <button class="button" type="button" @click="apiTokenSecret = ''; apiTokenModal = true">
          <Plus :size="16" />
          {{ t('plugins.createAPIToken') }}
        </button>
      </header>
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead>
            <tr>
              <th>{{ t('plugins.apiTokenName') }}</th>
              <th>{{ t('plugins.apiTokenPlugin') }}</th>
              <th>{{ t('plugins.apiTokenScopes') }}</th>
              <th>{{ t('plugins.surfaces') }}</th>
              <th>{{ t('plugins.apiTokenActivity') }}</th>
              <th>{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="token in apiTokens" :key="token.id">
              <td>
                <strong>{{ token.name }}</strong>
                <span>{{ token.token_prefix }}...</span>
              </td>
              <td><span>{{ token.plugin_id || t('plugins.catalogOnly') }}</span></td>
              <td><span>{{ token.scopes.join(', ') }}</span></td>
              <td><span>{{ token.surfaces.join(', ') }}</span></td>
              <td>
                <span class="pill" :class="statusClass(token.status)">{{ token.status }}</span>
                <span>{{ t('plugins.lastUsed') }}: {{ formatOptionalTime(token.last_used_at) }}</span>
              </td>
              <td>
                <button
                  class="icon-button danger-item"
                  type="button"
                  :disabled="token.status === 'revoked' || apiTokenRevokeID === token.id"
                  :title="t('plugins.revokeToken')"
                  @click="revokePluginToken(token)"
                >
                  <Trash2 :size="16" />
                </button>
              </td>
            </tr>
            <tr v-if="!apiTokens.length">
              <td colspan="6" class="empty-cell">{{ t('plugins.apiTokenEmpty') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section class="plugin-workbench section-gap">
      <aside class="plugin-tree-panel">
        <div class="plugin-filter-bar">
          <label class="search-box compact-search">
            <Search :size="17" />
            <input v-model="query" :placeholder="t('plugins.searchPlaceholder')" />
          </label>
          <div class="plugin-filter-grid">
            <select v-model="categoryFilter">
              <option value="">{{ t('plugins.allCategories') }}</option>
              <option v-for="category in categoryOptions" :key="category" :value="category">{{ category }}</option>
            </select>
            <select v-model="tierFilter">
              <option value="">{{ t('plugins.allTiers') }}</option>
              <option v-for="tier in tierOptions" :key="tier" :value="tier">{{ tier }}</option>
            </select>
            <select v-model="statusFilter">
              <option value="">{{ t('providers.allStatuses') }}</option>
              <option v-for="status in statusOptions" :key="status" :value="status">{{ status }}</option>
            </select>
          </div>
        </div>
        <nav class="plugin-tree">
          <div v-for="group in pluginTree" :key="group.category" class="plugin-tree-group">
            <div class="plugin-tree-heading">
              <span>{{ group.category }}</span>
              <strong>{{ group.plugins.length }}</strong>
            </div>
            <button
              v-for="plugin in group.plugins"
              :key="plugin.id"
              class="plugin-tree-item"
              :class="{ active: activePlugin?.id === plugin.id }"
              type="button"
              @click="selectedPlugin = plugin"
            >
              <span class="tree-branch" />
              <span class="plugin-tree-main">
                <strong>{{ plugin.name }}</strong>
                <small>{{ plugin.plugin_id }} · v{{ plugin.version }}</small>
              </span>
              <span class="pill" :class="statusClass(plugin.status)">{{ plugin.status }}</span>
            </button>
          </div>
          <div v-if="!filteredPlugins.length" class="plugin-tree-empty">
            {{ loading ? t('common.loading') : t('plugins.empty') }}
          </div>
        </nav>
      </aside>

      <section v-if="activePlugin" class="plugin-detail-panel">
        <header class="plugin-detail-header">
          <div>
            <span class="pill">{{ activePlugin.category }}</span>
            <h2>{{ activePlugin.name }}</h2>
            <p>{{ activePlugin.description }}</p>
          </div>
          <div class="row-actions">
            <button class="button secondary" type="button" :disabled="!canConfigure(activePlugin)" @click="openConfig(activePlugin)">
              <Settings2 :size="15" />
              {{ t('plugins.configure') }}
            </button>
            <button class="button secondary" type="button" :disabled="activePlugin.category !== 'notification'" @click="openDeliveries(activePlugin)">
              <FileClock :size="15" />
              {{ t('plugins.deliveries') }}
            </button>
            <button class="button secondary" type="button" :disabled="actionID === activePlugin.id || !canEnable(activePlugin)" @click="setEnabled(activePlugin, true)">
              <CheckCircle2 :size="15" />
              {{ t('plugins.enable') }}
            </button>
            <button class="button danger" type="button" :disabled="actionID === activePlugin.id || !canDisable(activePlugin)" @click="setEnabled(activePlugin, false)">
              <XCircle :size="15" />
              {{ t('plugins.disable') }}
            </button>
          </div>
        </header>

        <div class="plugin-detail-meta">
          <div>
            <label>{{ t('plugins.status') }}</label>
            <span class="pill" :class="statusClass(activePlugin.status)">{{ activePlugin.status }}</span>
          </div>
          <div>
            <label>{{ t('plugins.tier') }}</label>
            <span class="pill">{{ activePlugin.tier }}</span>
          </div>
          <div>
            <label>{{ t('plugins.entitlement') }}</label>
            <span class="pill">{{ activePlugin.entitlement_status }}</span>
          </div>
          <div>
            <label>{{ t('plugins.packages') }}</label>
            <span class="pill">{{ pluginPackages(activePlugin).length }}</span>
          </div>
          <div>
            <label>{{ t('plugins.vendor') }}</label>
            <p>{{ activePlugin.vendor }}</p>
          </div>
          <div>
            <label>{{ t('plugins.entryPoint') }}</label>
            <p>{{ activePlugin.entry_point || '-' }}</p>
          </div>
        </div>

        <section class="plugin-detail-section">
          <div class="plugin-section-title">
            <h3>{{ t('plugins.runtimeStatus') }}</h3>
            <button class="button secondary tiny-button" type="button" :disabled="runtimeStatusLoading" @click="loadRuntimeStatus(activePlugin)">
              <RefreshCw :size="14" />
              {{ t('common.refresh') }}
            </button>
          </div>
          <div class="plugin-detail-meta compact-meta">
            <div>
              <label>{{ t('plugins.runtimeInstalled') }}</label>
              <span class="pill" :class="runtimeStatus?.installed ? 'status-success' : 'status-warning'">
                {{ runtimeStatus?.installed ? t('plugins.yes') : t('plugins.no') }}
              </span>
            </div>
            <div>
              <label>{{ t('plugins.runtimeEnabled') }}</label>
              <span class="pill" :class="runtimeStatus?.enabled ? 'status-success' : 'status-warning'">
                {{ runtimeStatus?.enabled ? t('plugins.yes') : t('plugins.no') }}
              </span>
            </div>
            <div>
              <label>{{ t('plugins.runtimeRunning') }}</label>
              <span class="pill" :class="runtimeStateClass(runtimeStatus)">
                {{ runtimeStatus?.running ? t('plugins.running') : runtimeStatus?.supervisor_state || '-' }}
              </span>
            </div>
            <div>
              <label>{{ t('plugins.runtimeSupervisor') }}</label>
              <span class="pill" :class="runtimeStatus?.supervised ? 'status-success' : 'status-warning'">
                {{ runtimeStatus?.supervisor_state || (runtimeStatus?.supervised ? 'supervised' : '-') }}
              </span>
            </div>
            <div>
              <label>{{ t('plugins.runtimeRestarts') }}</label>
              <p>{{ runtimeStatus?.restart_count ?? 0 }}</p>
            </div>
            <div>
              <label>{{ t('plugins.runtimeStartedAt') }}</label>
              <p>{{ formatOptionalTime(runtimeStatus?.last_started_at) }}</p>
            </div>
            <div>
              <label>{{ t('plugins.runtimeExitedAt') }}</label>
              <p>{{ formatOptionalTime(runtimeStatus?.last_exited_at) }}</p>
            </div>
            <div>
              <label>{{ t('plugins.runtimeNextRestartAt') }}</label>
              <p>{{ formatOptionalTime(runtimeStatus?.next_restart_at) }}</p>
            </div>
            <div v-if="runtimeStatus?.last_error || runtimeStatus?.error" class="form-span-2">
              <label>{{ t('plugins.error') }}</label>
              <p>{{ runtimeStatus.last_error || runtimeStatus.error }}</p>
            </div>
          </div>
        </section>

        <section class="plugin-detail-section">
          <div class="plugin-section-title">
            <h3>{{ t('plugins.surfaces') }}</h3>
          </div>
          <div class="chip-list">
            <span v-for="surface in activePlugin.surfaces" :key="surface" class="pill">{{ surface }}</span>
          </div>
        </section>

        <section class="plugin-detail-section">
          <div class="plugin-section-title">
            <h3>{{ t('plugins.packages') }}</h3>
          </div>
          <div v-if="pluginPackages(activePlugin).length" class="package-list">
            <article v-for="pkg in pluginPackages(activePlugin)" :key="pkg.package_id" class="package-row">
              <div class="package-main">
                <strong>{{ pkg.version }}</strong>
                <span>{{ pkg.os }}/{{ pkg.arch }} · {{ pkg.channel || '-' }} · {{ formatSize(pkg.size_bytes) }}</span>
                <small>{{ shortHash(pkg.sha256) }}</small>
              </div>
              <div class="package-state">
                <span class="pill" :class="packageStatusClass(pkg)">{{ packageStatusLabel(pkg) }}</span>
                <span v-if="pkg.advisory_id" class="pill status-danger">{{ pkg.advisory_id }}</span>
                <span v-if="pkg.compatibility_error" class="pill status-danger">{{ pkg.compatibility_error }}</span>
                <span v-if="pkg.installed_at" class="pill">{{ formatOptionalTime(pkg.installed_at) }}</span>
              </div>
              <div class="row-actions package-actions">
                <button class="button secondary" type="button" :disabled="packageDownloadingID === pkg.package_id || !canDownloadPackage(pkg)" @click="cachePackage(activePlugin, pkg)">
                  <Download :size="15" />
                  {{ packageDownloadingID === pkg.package_id ? t('common.loading') : t('plugins.downloadPackage') }}
                </button>
                <button class="button secondary" type="button" :disabled="packageImportingID === pkg.package_id || !canImportPackage(pkg)" @click="openPackageImport(activePlugin, pkg)">
                  <Upload :size="15" />
                  {{ packageImportingID === pkg.package_id ? t('common.loading') : t('plugins.importPackage') }}
                </button>
                <button class="button secondary" type="button" :disabled="packageInstallingID === pkg.package_id || !canInstallPackage(pkg)" @click="installPackage(activePlugin, pkg)">
                  <CheckCircle2 :size="15" />
                  {{ packageInstallingID === pkg.package_id ? t('common.loading') : t('plugins.installPackage') }}
                </button>
                <button class="button danger" type="button" :disabled="packageInstallingID === pkg.package_id || !canUninstallPackage(pkg)" @click="uninstallPackage(activePlugin, pkg)">
                  <XCircle :size="15" />
                  {{ packageInstallingID === pkg.package_id ? t('common.loading') : t('plugins.uninstallPackage') }}
                </button>
              </div>
            </article>
          </div>
          <p v-else class="empty-inline">{{ t('plugins.noPackages') }}</p>
        </section>
      </section>

      <section v-else class="plugin-detail-panel empty-state">
        {{ loading ? t('common.loading') : t('plugins.empty') }}
      </section>
    </section>

    <div v-if="packageImportTarget" class="modal-backdrop" @click.self="packageImportTarget = null">
      <section class="modal-card">
        <header class="modal-header">
          <div>
            <h2>{{ t('plugins.importPackage') }}</h2>
            <p>{{ packageImportTarget.plugin.name }} · {{ packageImportTarget.pkg.version }} · {{ packageImportTarget.pkg.os }}/{{ packageImportTarget.pkg.arch }}</p>
          </div>
          <button class="icon-button" type="button" @click="packageImportTarget = null"><X :size="19" /></button>
        </header>
        <form class="modal-body form-grid" @submit.prevent="savePackageImport">
          <label class="form-span-2">
            <span>{{ t('plugins.offlinePackageFile') }}</span>
            <textarea v-model="packageImportFileJSON" rows="10" spellcheck="false"></textarea>
          </label>
          <p class="form-span-2 hint">{{ t('plugins.importPackageSubtitle') }}</p>
        </form>
        <footer class="modal-footer">
          <button class="button secondary" type="button" @click="packageImportTarget = null">{{ t('common.cancel') }}</button>
          <button class="button" type="button" :disabled="!!packageImportingID" @click="savePackageImport">
            <Upload :size="17" />
            {{ packageImportingID ? t('common.saving') : t('plugins.importPackage') }}
          </button>
        </footer>
      </section>
    </div>

    <div v-if="feedImportModal" class="modal-backdrop" @click.self="feedImportModal = false">
      <section class="modal-card wide">
        <header class="modal-header">
          <div>
            <h2>{{ t('plugins.importFeed') }}</h2>
            <p>{{ t('plugins.importFeedSubtitle') }}</p>
          </div>
          <button class="icon-button" type="button" @click="feedImportModal = false"><X :size="19" /></button>
        </header>
        <form class="modal-body form-grid" @submit.prevent="saveOfficialFeedImport">
          <label class="form-span-2">
            <span>{{ t('plugins.feedPackageJSON') }}</span>
            <textarea v-model="feedImportJSON" rows="14" spellcheck="false"></textarea>
          </label>
        </form>
        <footer class="modal-footer">
          <button class="button secondary" type="button" @click="feedImportModal = false">{{ t('common.cancel') }}</button>
          <button class="button" type="button" :disabled="feedImporting" @click="saveOfficialFeedImport">
            <Upload :size="16" />
            {{ feedImporting ? t('common.saving') : t('plugins.importFeed') }}
          </button>
        </footer>
      </section>
    </div>

    <div v-if="apiTokenModal" class="modal-backdrop" @click.self="closeAPITokenModal">
      <section class="modal-card wide">
        <header class="modal-header">
          <div>
            <h2>{{ t('plugins.createAPIToken') }}</h2>
            <p>{{ t('plugins.createAPITokenSubtitle') }}</p>
          </div>
          <button class="icon-button" type="button" @click="closeAPITokenModal"><X :size="19" /></button>
        </header>
        <div v-if="apiTokenSecret" class="modal-body form-grid">
          <div class="form-span-2 notice success token-secret-panel">
            <strong>{{ t('plugins.apiTokenSecretOnce') }}</strong>
            <code>{{ apiTokenSecret }}</code>
          </div>
        </div>
        <form v-else class="modal-body form-grid" @submit.prevent="savePluginAPIToken">
          <label>
            <span>{{ t('plugins.apiTokenName') }}</span>
            <input v-model="apiTokenForm.name" required autocomplete="off" />
          </label>
          <label>
            <span>{{ t('plugins.apiTokenPlugin') }}</span>
            <select v-model="apiTokenForm.pluginID">
              <option value="">{{ t('plugins.catalogOnly') }}</option>
              <option v-for="plugin in catalog.plugins" :key="plugin.id" :value="plugin.id">{{ plugin.name }}</option>
            </select>
          </label>
          <fieldset class="form-span-2 token-option-group">
            <legend>{{ t('plugins.apiTokenScopes') }}</legend>
            <div class="token-option-grid">
              <label v-for="scope in apiTokenScopeOptions" :key="scope" class="checkbox-row">
                <input v-model="apiTokenForm.scopes" type="checkbox" :value="scope" />
                <span>{{ scope }}</span>
              </label>
            </div>
          </fieldset>
          <fieldset class="form-span-2 token-option-group">
            <legend>{{ t('plugins.surfaces') }}</legend>
            <div class="token-option-grid">
              <label v-for="surface in apiTokenSurfaceOptions" :key="surface" class="checkbox-row">
                <input v-model="apiTokenForm.surfaces" type="checkbox" :value="surface" />
                <span>{{ surface }}</span>
              </label>
            </div>
          </fieldset>
          <label class="form-span-2">
            <span>{{ t('plugins.apiTokenExpiresAt') }}</span>
            <input v-model="apiTokenForm.expiresAt" type="datetime-local" />
          </label>
        </form>
        <footer class="modal-footer">
          <button class="button secondary" type="button" @click="closeAPITokenModal">{{ t('common.cancel') }}</button>
          <button v-if="apiTokenSecret" class="button" type="button" @click="copyAPITokenSecret">
            <Copy :size="16" />
            {{ t('plugins.copyAPIToken') }}
          </button>
          <button v-else class="button" type="button" :disabled="apiTokenSaving" @click="savePluginAPIToken">
            <Plus :size="16" />
            {{ apiTokenSaving ? t('common.saving') : t('plugins.createAPIToken') }}
          </button>
        </footer>
      </section>
    </div>

    <div v-if="licenseModal" class="modal-backdrop" @click.self="licenseModal = null">
      <section class="modal-card">
        <header class="modal-header">
          <div>
            <h2>{{ licenseModal === 'activate' ? t('plugins.activateLicense') : licenseModal === 'redeem' ? t('plugins.redeemCode') : t('plugins.importLicense') }}</h2>
            <p>{{ licenseModal === 'activate' ? t('plugins.activateLicenseSubtitle') : licenseModal === 'redeem' ? t('plugins.redeemCodeSubtitle') : t('plugins.importLicenseSubtitle') }}</p>
          </div>
          <button class="icon-button" type="button" @click="licenseModal = null"><X :size="19" /></button>
        </header>
        <form v-if="licenseModal === 'activate'" class="modal-body form-grid" @submit.prevent="saveLicenseActivation">
          <label>
            <span>{{ t('plugins.licenseID') }}</span>
            <input v-model="licenseForm.licenseID" autocomplete="off" />
          </label>
          <label>
            <span>{{ t('plugins.activationSecret') }}</span>
            <input v-model="licenseForm.activationSecret" type="password" autocomplete="off" />
          </label>
          <label>
            <span>{{ t('plugins.licenseInstance') }}</span>
            <input v-model="licenseForm.instanceID" autocomplete="off" />
          </label>
          <label>
            <span>{{ t('plugins.instanceDisplayName') }}</span>
            <input v-model="licenseForm.displayName" autocomplete="off" />
          </label>
          <label class="form-span-2">
            <span>{{ t('plugins.instanceFingerprint') }}</span>
            <input v-model="licenseForm.fingerprint" placeholder="sha256:..." autocomplete="off" />
          </label>
        </form>
        <form v-else-if="licenseModal === 'redeem'" class="modal-body form-grid" @submit.prevent="saveLicenseRedeem">
          <label class="form-span-2">
            <span>{{ t('plugins.redeemCode') }}</span>
            <input v-model="licenseForm.code" autocomplete="off" spellcheck="false" />
          </label>
          <label>
            <span>{{ t('plugins.licenseInstance') }}</span>
            <input v-model="licenseForm.instanceID" autocomplete="off" />
          </label>
          <label>
            <span>{{ t('plugins.instanceDisplayName') }}</span>
            <input v-model="licenseForm.displayName" autocomplete="off" />
          </label>
          <label class="form-span-2">
            <span>{{ t('plugins.instanceFingerprint') }}</span>
            <input v-model="licenseForm.fingerprint" placeholder="sha256:..." autocomplete="off" />
          </label>
        </form>
        <form v-else class="modal-body form-grid" @submit.prevent="saveLicenseImport">
          <label class="form-span-2">
            <span>{{ t('plugins.offlineLicenseFile') }}</span>
            <textarea v-model="licenseForm.fileJSON" rows="10" spellcheck="false"></textarea>
          </label>
          <label class="form-span-2">
            <span>{{ t('plugins.activationSecretOptional') }}</span>
            <input v-model="licenseForm.activationSecret" type="password" autocomplete="off" />
          </label>
        </form>
        <footer class="modal-footer">
          <button class="button secondary" type="button" @click="licenseModal = null">{{ t('common.cancel') }}</button>
          <button class="button" type="button" :disabled="licenseSaving" @click="licenseModal === 'activate' ? saveLicenseActivation() : licenseModal === 'redeem' ? saveLicenseRedeem() : saveLicenseImport()">
            <CheckCircle2 :size="17" />
            {{ licenseSaving ? t('common.saving') : t('common.save') }}
          </button>
        </footer>
      </section>
    </div>

    <div v-if="deliveryPlugin" class="modal-backdrop" @click.self="deliveryPlugin = null">
      <section class="modal-card wide">
        <header class="modal-header">
          <div>
            <h2>{{ t('plugins.deliveries') }} · {{ deliveryPlugin.name }}</h2>
            <p>{{ t('plugins.deliverySubtitle') }}</p>
          </div>
          <button class="icon-button" type="button" @click="deliveryPlugin = null"><X :size="19" /></button>
        </header>
        <div class="modal-body">
          <section class="table-toolbar compact-toolbar">
            <select v-model="deliveryStatusFilter" @change="openDeliveries(deliveryPlugin)">
              <option value="">{{ t('plugins.allDeliveryStatuses') }}</option>
              <option value="succeeded">{{ t('plugins.deliveryStatuses.succeeded') }}</option>
              <option value="failed">{{ t('plugins.deliveryStatuses.failed') }}</option>
              <option value="skipped">{{ t('plugins.deliveryStatuses.skipped') }}</option>
            </select>
            <button class="button secondary" type="button" :disabled="deliveryLoading" @click="openDeliveries(deliveryPlugin)">
              <RefreshCw :size="15" />
              {{ t('common.refresh') }}
            </button>
          </section>
          <div class="table-scroll">
            <table class="data-table crud-table">
              <thead>
                <tr>
                  <th>{{ t('audit.time') }}</th>
                  <th>{{ t('alerts.alert') }}</th>
                  <th>{{ t('plugins.status') }}</th>
                  <th>{{ t('plugins.target') }}</th>
                  <th>{{ t('traces.http') }}</th>
                  <th>{{ t('plugins.error') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="delivery in deliveries" :key="delivery.id">
                  <td>{{ formatTime(delivery.created_at) }}</td>
                  <td>
                    <strong>{{ delivery.alert_type }}</strong>
                    <span>{{ delivery.alert_id }} · {{ delivery.alert_severity }}</span>
                  </td>
                  <td><span class="pill" :class="deliveryStatusClass(delivery.status)">{{ delivery.status }}</span></td>
                  <td>{{ delivery.target || '-' }}</td>
                  <td>{{ delivery.http_status || '-' }}</td>
                  <td>{{ delivery.error || '-' }}</td>
                </tr>
                <tr v-if="!deliveries.length">
                  <td colspan="6" class="empty-cell">{{ deliveryLoading ? t('common.loading') : t('plugins.noDeliveries') }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
        <footer class="modal-footer">
          <button class="button secondary" type="button" @click="deliveryPlugin = null">{{ t('common.cancel') }}</button>
        </footer>
      </section>
    </div>

    <div v-if="configPlugin" class="modal-backdrop" @click.self="configPlugin = null">
      <section class="modal-card">
        <header class="modal-header">
          <div>
            <h2>{{ t('plugins.configure') }} · {{ configPlugin.name }}</h2>
            <p>{{ t('plugins.configSubtitle') }}</p>
          </div>
          <button class="icon-button" type="button" @click="configPlugin = null"><X :size="19" /></button>
        </header>
        <form class="modal-body form-grid" @submit.prevent="saveConfig">
          <label v-for="field in activeConfigSchema?.secretFields || []" :key="field.key" class="form-span-2">
            <span>{{ t(field.labelKey) }}</span>
            <input
              v-model="configForm.secrets[field.key]"
              :type="field.inputType"
              :placeholder="pluginConfig?.secret_hints[field.key] || t(field.placeholderKey)"
            />
          </label>
          <label>
            <span>{{ t('plugins.minSeverity') }}</span>
            <select v-model="configForm.minSeverity">
              <option value="info">{{ t('alerts.severities.info') }}</option>
              <option value="warning">{{ t('alerts.severities.warning') }}</option>
              <option value="critical">{{ t('alerts.severities.critical') }}</option>
            </select>
          </label>
          <label>
            <span>{{ t('plugins.alertTypes') }}</span>
            <input v-model="configForm.alertTypes" placeholder="api_key_quota,gateway_error_rate" />
          </label>
        </form>
        <footer class="modal-footer">
          <button class="button secondary" type="button" @click="configPlugin = null">{{ t('common.cancel') }}</button>
          <button class="button" type="button" :disabled="configLoading || configSaving" @click="saveConfig">
            <Settings2 :size="17" />
            {{ configSaving ? t('common.saving') : t('common.save') }}
          </button>
        </footer>
      </section>
    </div>
  </main>
</template>
