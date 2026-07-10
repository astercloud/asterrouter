<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Boxes, CheckCircle2, Download, Eye, FileClock, LockKeyhole, Plug, RefreshCw, Search, Settings2, Upload, X, XCircle } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import {
  activateOfficialLicense,
  disablePlugin,
  downloadPluginPackage,
  enablePlugin,
  getOfficialCatalogStatus,
  getOfficialLicenseStatus,
  getPluginCatalog,
  getPluginConfig,
  getPluginDeliveries,
  importOfficialLicense,
  importPluginPackage,
  installPluginPackage,
  syncOfficialCatalog,
  uninstallPluginPackage,
  updatePluginConfig
} from '@/api/plugins'
import type { OfficialCatalogStatus, OfficialLicenseStatus, Plugin, PluginCatalog, PluginConfig, PluginDeliveryAttempt, PluginPackage } from '@/types'

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
const licenseModal = ref<'activate' | 'import' | null>(null)
const packageImportTarget = ref<{ plugin: Plugin; pkg: PluginPackage } | null>(null)
const packageImportFileJSON = ref('')
const licenseForm = ref({
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

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [catalogData, catalogStatus, licenseStatus] = await Promise.all([getPluginCatalog(), loadOfficialCatalogStatus(), loadOfficialLicenseStatus()])
    catalog.value = catalogData
    officialCatalogStatus.value = catalogStatus
    officialLicenseStatus.value = licenseStatus
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
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
        <button class="button secondary" type="button" :disabled="catalogSyncing || catalogStatusLoading || officialCatalogStatus?.mode !== 'online'" @click="syncCatalog">
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
          <label>{{ t('plugins.catalogSyncedAt') }}</label>
          <p>{{ formatOptionalTime(officialCatalogStatus?.synced_at) }}</p>
        </div>
        <div class="form-span-2">
          <label>{{ t('plugins.catalogSource') }}</label>
          <p>{{ officialCatalogStatus?.source_url || '-' }}</p>
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

    <section class="table-toolbar">
      <label class="search-box">
        <Search :size="17" />
        <input v-model="query" :placeholder="t('plugins.searchPlaceholder')" />
      </label>
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
    </section>

    <section class="panel table-panel content-fit">
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead>
            <tr>
              <th>{{ t('plugins.plugin') }}</th>
              <th>{{ t('plugins.category') }}</th>
              <th>{{ t('plugins.tier') }}</th>
              <th>{{ t('plugins.entitlement') }}</th>
              <th>{{ t('plugins.packages') }}</th>
              <th>{{ t('plugins.status') }}</th>
              <th>{{ t('plugins.surfaces') }}</th>
              <th>{{ t('plugins.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="plugin in filteredPlugins" :key="plugin.id">
              <td>
                <strong>{{ plugin.name }}</strong>
                <span>{{ plugin.description }}</span>
                <span>{{ plugin.plugin_id }} · v{{ plugin.version }} · {{ plugin.vendor }}</span>
              </td>
              <td><span class="pill">{{ plugin.category }}</span></td>
              <td><span class="pill">{{ plugin.tier }}</span></td>
              <td><span class="pill">{{ plugin.entitlement_status }}</span></td>
              <td><span class="pill">{{ pluginPackages(plugin).length }}</span></td>
              <td><span class="pill" :class="statusClass(plugin.status)">{{ plugin.status }}</span></td>
              <td>
                <div class="chip-list">
                  <span v-for="surface in plugin.surfaces" :key="surface" class="pill">{{ surface }}</span>
                </div>
              </td>
              <td>
                <div class="row-actions">
                  <button class="button secondary" type="button" @click="selectedPlugin = plugin">
                    <Eye :size="15" />
                    {{ t('common.details') }}
                  </button>
                  <button class="button secondary" type="button" :disabled="!canConfigure(plugin)" @click="openConfig(plugin)">
                    <Settings2 :size="15" />
                    {{ t('plugins.configure') }}
                  </button>
                  <button class="button secondary" type="button" :disabled="plugin.category !== 'notification'" @click="openDeliveries(plugin)">
                    <FileClock :size="15" />
                    {{ t('plugins.deliveries') }}
                  </button>
                  <button v-if="pluginPackages(plugin).length" class="button secondary" type="button" :disabled="packageDownloadingID === bestPackage(plugin)?.package_id || !canDownloadPackage(bestPackage(plugin))" @click="cachePackage(plugin, bestPackage(plugin))">
                    <Download :size="15" />
                    {{ packageDownloadingID === bestPackage(plugin)?.package_id ? t('common.loading') : t('plugins.downloadPackage') }}
                  </button>
                  <button v-if="pluginPackages(plugin).length" class="button secondary" type="button" :disabled="packageImportingID === bestPackage(plugin)?.package_id || !canImportPackage(bestPackage(plugin))" @click="openPackageImport(plugin, bestPackage(plugin))">
                    <Upload :size="15" />
                    {{ packageImportingID === bestPackage(plugin)?.package_id ? t('common.loading') : t('plugins.importPackage') }}
                  </button>
                  <button v-if="pluginPackages(plugin).length" class="button secondary" type="button" :disabled="packageInstallingID === bestPackage(plugin)?.package_id || !canInstallPackage(bestPackage(plugin))" @click="installPackage(plugin, bestPackage(plugin))">
                    <CheckCircle2 :size="15" />
                    {{ packageInstallingID === bestPackage(plugin)?.package_id ? t('common.loading') : t('plugins.installPackage') }}
                  </button>
                  <button v-if="pluginPackages(plugin).length" class="button danger" type="button" :disabled="packageInstallingID === bestPackage(plugin)?.package_id || !canUninstallPackage(bestPackage(plugin))" @click="uninstallPackage(plugin, bestPackage(plugin))">
                    <XCircle :size="15" />
                    {{ packageInstallingID === bestPackage(plugin)?.package_id ? t('common.loading') : t('plugins.uninstallPackage') }}
                  </button>
                  <button class="button secondary" type="button" :disabled="actionID === plugin.id || !canEnable(plugin)" @click="setEnabled(plugin, true)">
                    <CheckCircle2 :size="15" />
                    {{ t('plugins.enable') }}
                  </button>
                  <button class="button danger" type="button" :disabled="actionID === plugin.id || !canDisable(plugin)" @click="setEnabled(plugin, false)">
                    <XCircle :size="15" />
                    {{ t('plugins.disable') }}
                  </button>
                </div>
              </td>
            </tr>
            <tr v-if="!filteredPlugins.length">
              <td colspan="8" class="empty-cell">{{ loading ? t('common.loading') : t('plugins.empty') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <div v-if="selectedPlugin" class="modal-backdrop" @click.self="selectedPlugin = null">
      <section class="modal-card">
        <header class="modal-header">
          <div>
            <h2>{{ selectedPlugin.name }}</h2>
            <p>{{ selectedPlugin.plugin_id }} · v{{ selectedPlugin.version }} · {{ selectedPlugin.vendor }}</p>
          </div>
          <button class="icon-button" type="button" @click="selectedPlugin = null"><X :size="19" /></button>
        </header>
        <div class="modal-body detail-grid">
          <div>
            <label>{{ t('plugins.description') }}</label>
            <p>{{ selectedPlugin.description }}</p>
          </div>
          <div>
            <label>{{ t('plugins.category') }}</label>
            <p>{{ selectedPlugin.category }} / {{ selectedPlugin.type }}</p>
          </div>
          <div>
            <label>{{ t('plugins.tier') }}</label>
            <p>{{ selectedPlugin.tier }}</p>
          </div>
          <div>
            <label>{{ t('plugins.entitlement') }}</label>
            <p>{{ selectedPlugin.entitlement_status }}</p>
          </div>
          <div>
            <label>{{ t('plugins.entryPoint') }}</label>
            <p>{{ selectedPlugin.entry_point || '-' }}</p>
          </div>
          <div>
            <label>{{ t('plugins.configurable') }}</label>
            <p>{{ selectedPlugin.configurable ? t('common.yes') : t('common.no') }}</p>
          </div>
          <div v-if="canConfigure(selectedPlugin)">
            <label>{{ t('plugins.configuration') }}</label>
            <p>{{ t('plugins.notificationConfig') }}</p>
          </div>
          <div class="form-span-2">
            <label>{{ t('plugins.surfaces') }}</label>
            <div class="chip-list">
              <span v-for="surface in selectedPlugin.surfaces" :key="surface" class="pill">{{ surface }}</span>
            </div>
          </div>
          <div class="form-span-2">
            <label>{{ t('plugins.packages') }}</label>
            <div v-if="pluginPackages(selectedPlugin).length" class="table-scroll compact-table">
              <table class="data-table crud-table">
                <thead>
                  <tr>
                    <th>{{ t('plugins.packageTarget') }}</th>
                    <th>{{ t('plugins.packageSize') }}</th>
                    <th>{{ t('plugins.packageHash') }}</th>
                    <th>{{ t('plugins.status') }}</th>
                    <th>{{ t('plugins.actions') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="pkg in pluginPackages(selectedPlugin)" :key="pkg.package_id">
                    <td>
                      <strong>{{ pkg.version }}</strong>
                      <span>{{ pkg.os }}/{{ pkg.arch }} · {{ pkg.channel || '-' }}</span>
                    </td>
                    <td>{{ formatSize(pkg.size_bytes) }}</td>
                    <td>{{ shortHash(pkg.sha256) }}</td>
                    <td>
                      <div class="chip-list">
                        <span class="pill" :class="packageStatusClass(pkg)">
                          {{ packageStatusLabel(pkg) }}
                        </span>
                        <span v-if="pkg.advisory_id" class="pill status-danger">{{ pkg.advisory_id }}</span>
                        <span v-if="pkg.compatibility_error" class="pill status-danger">{{ pkg.compatibility_error }}</span>
                        <span v-if="pkg.installed_at" class="pill">{{ formatOptionalTime(pkg.installed_at) }}</span>
                      </div>
                    </td>
                    <td>
                      <div class="row-actions">
                        <button class="button secondary" type="button" :disabled="packageDownloadingID === pkg.package_id || !canDownloadPackage(pkg)" @click="cachePackage(selectedPlugin, pkg)">
                          <Download :size="15" />
                          {{ packageDownloadingID === pkg.package_id ? t('common.loading') : t('plugins.downloadPackage') }}
                        </button>
                        <button class="button secondary" type="button" :disabled="packageImportingID === pkg.package_id || !canImportPackage(pkg)" @click="openPackageImport(selectedPlugin, pkg)">
                          <Upload :size="15" />
                          {{ packageImportingID === pkg.package_id ? t('common.loading') : t('plugins.importPackage') }}
                        </button>
                        <button class="button secondary" type="button" :disabled="packageInstallingID === pkg.package_id || !canInstallPackage(pkg)" @click="installPackage(selectedPlugin, pkg)">
                          <CheckCircle2 :size="15" />
                          {{ packageInstallingID === pkg.package_id ? t('common.loading') : t('plugins.installPackage') }}
                        </button>
                        <button class="button danger" type="button" :disabled="packageInstallingID === pkg.package_id || !canUninstallPackage(pkg)" @click="uninstallPackage(selectedPlugin, pkg)">
                          <XCircle :size="15" />
                          {{ packageInstallingID === pkg.package_id ? t('common.loading') : t('plugins.uninstallPackage') }}
                        </button>
                      </div>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
            <p v-else>{{ t('plugins.noPackages') }}</p>
          </div>
        </div>
        <footer class="modal-footer">
          <button class="button secondary" type="button" @click="selectedPlugin = null">{{ t('common.cancel') }}</button>
          <button class="button secondary" type="button" :disabled="!canConfigure(selectedPlugin)" @click="openConfig(selectedPlugin)">
            <Settings2 :size="17" />
            {{ t('plugins.configure') }}
          </button>
          <button class="button secondary" type="button" :disabled="selectedPlugin.category !== 'notification'" @click="openDeliveries(selectedPlugin)">
            <FileClock :size="17" />
            {{ t('plugins.deliveries') }}
          </button>
          <button v-if="pluginPackages(selectedPlugin).length" class="button secondary" type="button" :disabled="packageDownloadingID === bestPackage(selectedPlugin)?.package_id || !canDownloadPackage(bestPackage(selectedPlugin))" @click="cachePackage(selectedPlugin, bestPackage(selectedPlugin))">
            <Download :size="17" />
            {{ packageDownloadingID === bestPackage(selectedPlugin)?.package_id ? t('common.loading') : t('plugins.downloadPackage') }}
          </button>
          <button v-if="pluginPackages(selectedPlugin).length" class="button secondary" type="button" :disabled="packageImportingID === bestPackage(selectedPlugin)?.package_id || !canImportPackage(bestPackage(selectedPlugin))" @click="openPackageImport(selectedPlugin, bestPackage(selectedPlugin))">
            <Upload :size="17" />
            {{ packageImportingID === bestPackage(selectedPlugin)?.package_id ? t('common.loading') : t('plugins.importPackage') }}
          </button>
          <button v-if="pluginPackages(selectedPlugin).length" class="button secondary" type="button" :disabled="packageInstallingID === bestPackage(selectedPlugin)?.package_id || !canInstallPackage(bestPackage(selectedPlugin))" @click="installPackage(selectedPlugin, bestPackage(selectedPlugin))">
            <CheckCircle2 :size="17" />
            {{ packageInstallingID === bestPackage(selectedPlugin)?.package_id ? t('common.loading') : t('plugins.installPackage') }}
          </button>
          <button v-if="pluginPackages(selectedPlugin).length" class="button danger" type="button" :disabled="packageInstallingID === bestPackage(selectedPlugin)?.package_id || !canUninstallPackage(bestPackage(selectedPlugin))" @click="uninstallPackage(selectedPlugin, bestPackage(selectedPlugin))">
            <XCircle :size="17" />
            {{ packageInstallingID === bestPackage(selectedPlugin)?.package_id ? t('common.loading') : t('plugins.uninstallPackage') }}
          </button>
          <button class="button secondary" type="button" :disabled="actionID === selectedPlugin.id || !canEnable(selectedPlugin)" @click="setEnabled(selectedPlugin, true)">
            <CheckCircle2 :size="17" />
            {{ t('plugins.enable') }}
          </button>
          <button class="button danger" type="button" :disabled="actionID === selectedPlugin.id || !canDisable(selectedPlugin)" @click="setEnabled(selectedPlugin, false)">
            <XCircle :size="17" />
            {{ t('plugins.disable') }}
          </button>
        </footer>
      </section>
    </div>

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

    <div v-if="licenseModal" class="modal-backdrop" @click.self="licenseModal = null">
      <section class="modal-card">
        <header class="modal-header">
          <div>
            <h2>{{ licenseModal === 'activate' ? t('plugins.activateLicense') : t('plugins.importLicense') }}</h2>
            <p>{{ licenseModal === 'activate' ? t('plugins.activateLicenseSubtitle') : t('plugins.importLicenseSubtitle') }}</p>
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
          <button class="button" type="button" :disabled="licenseSaving" @click="licenseModal === 'activate' ? saveLicenseActivation() : saveLicenseImport()">
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
            <input v-model="configForm.alertTypes" placeholder="project_budget,api_key_quota" />
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
