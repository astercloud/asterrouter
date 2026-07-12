<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { AlertTriangle, Database, Download, FileText, KeyRound, Mail, Power, RefreshCw, RotateCcw, Save, ServerCog, ShieldCheck, SlidersHorizontal, ToggleLeft, UserRound } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { getAdminSettings, updateAdminSettings } from '@/api/settings'
import {
  checkSystemUpdates,
  createDiagnosticBundle,
  createSystemBackup,
  downloadDiagnosticBundle,
  downloadSystemBackup,
  listSystemBackups,
  performSystemUpdate,
  restartSystem,
  restoreSystemBackup,
  rollbackSystemUpdate
} from '@/api/system'
import { setPublicSettingsCache } from '@/router'
import { useAppStore } from '@/stores/app'
import type { AdminSettings, SystemArchiveInfo, SystemUpdateInfo } from '@/types'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const app = useAppStore()
const loading = ref(false)
const saving = ref(false)
const message = ref('')
const error = ref('')
const updateInfo = ref<SystemUpdateInfo | null>(null)
const updateAction = ref('')
const archiveAction = ref('')
const backups = ref<SystemArchiveInfo[]>([])
const activeSettingsTab = ref<'general' | 'terms' | 'features' | 'security' | 'defaults' | 'gateway' | 'email' | 'backup'>('general')
const originalEnabledProfiles = ref<string[]>([])
const originalDefaultProfile = ref('')
const profileConfirmOpen = ref(false)

const settingsTabs = [
  { id: 'general', label: 'settings.general', icon: SlidersHorizontal },
  { id: 'terms', label: 'settings.loginTerms', icon: FileText },
  { id: 'features', label: 'settings.featureFlags', icon: ToggleLeft },
  { id: 'security', label: 'settings.securityAndAuth', icon: ShieldCheck },
  { id: 'defaults', label: 'settings.userDefaults', icon: UserRound },
  { id: 'gateway', label: 'settings.gatewayServices', icon: ServerCog },
  { id: 'email', label: 'settings.emailSettings', icon: Mail },
  { id: 'backup', label: 'settings.dataBackup', icon: Database }
] as const

const form = reactive<AdminSettings>({
  site_name: 'AsterRouter',
  site_subtitle: 'AI Gateway Control Plane',
  public_base_url: '',
  api_base_url: '/api/v1',
  gateway_base_path: '/v1',
  default_profile: '',
  enabled_profiles: [],
  setup_completed: false,
  default_locale: 'en-US',
  enabled_locales: ['en-US', 'zh-CN'],
  oidc_enabled: false,
  oidc_provider_name: 'OIDC',
	feishu_enabled: false,
	feishu_region: 'cn',
	registration_enabled: false,
	email_verify_enabled: false,
	totp_enabled: false,
	turnstile_enabled: false,
  service_center_mode: 'disabled',
  version: '',
  server_timezone: '',
  server_utc_offset: '',
  storage_mode: '',
  demo_mode: false,
  oidc_issuer_url: '',
  oidc_client_id: '',
	feishu_app_id: '',
	feishu_app_secret: '',
	feishu_configured: false,
	allowed_email_domains: [],
	invitation_required: false,
  login_agreement_enabled: false,
	login_agreement_mode: 'modal',
	login_agreement_updated_at: '',
	legal_documents: [],
	backend_mode: false,
	support_contact: '',
	documentation_url: '',
	invitation_codes: [],
	trusted_proxy_headers: false,
	turnstile_site_key: '',
	turnstile_secret_key: '',
	turnstile_configured: false,
	default_balance_cents: 0,
	default_concurrency: 5,
	default_rpm: 0,
	smtp_host: '',
	smtp_port: 587,
	smtp_username: '',
	smtp_password: '',
	smtp_from: '',
	smtp_configured: false,
	login_agreement_title: 'Terms of Service',
	login_agreement_content: '',
	default_page_size: 20,
	page_size_options: [10, 20, 50],
	home_content: '',
	hide_import_button: false,
  data_retention_days: 30,
  prompt_logging_mode: 'metadata_only',
  update_channel: 'stable'
})

const gatewayBaseUrl = computed(() => {
  const base = form.public_base_url || window.location.origin
  return `${base.replace(/\/$/, '')}${form.gateway_base_path}`
})
const feishuCallbackUrl = computed(() => `${(form.public_base_url || window.location.origin).replace(/\/$/, '')}/api/v1/auth/feishu/callback`)

const normalizedCurrentProfiles = computed(() => normalizeProfileList(form.enabled_profiles))
const normalizedOriginalProfiles = computed(() => normalizeProfileList(originalEnabledProfiles.value))
const addedProfiles = computed(() =>
  normalizedCurrentProfiles.value.filter((profile) => !normalizedOriginalProfiles.value.includes(profile))
)
const removedProfiles = computed(() =>
  normalizedOriginalProfiles.value.filter((profile) => !normalizedCurrentProfiles.value.includes(profile))
)
const defaultProfileChanged = computed(() => form.default_profile !== originalDefaultProfile.value)
const profileChanged = computed(() => {
  return (
    normalizedCurrentProfiles.value.join('|') !== normalizedOriginalProfiles.value.join('|') ||
    defaultProfileChanged.value
  )
})

function profileRoute(profile: string): string {
  if (profile === 'personal') return '/console/overview'
  if (profile === 'relay_operator') return '/operator/overview'
  return '/admin/dashboard'
}

function currentSurfaceDisabled(settings: AdminSettings): boolean {
  const profiles = settings.enabled_profiles || []
  if (route.path.startsWith('/console')) return !profiles.includes('personal')
  if (route.path.startsWith('/operator')) return !profiles.includes('relay_operator')
  if (route.path.startsWith('/portal')) return !profiles.includes('enterprise')
  if (route.path.startsWith('/admin')) return !profiles.includes('enterprise')
  return false
}

function normalizeProfileList(profiles: string[]): string[] {
  const order = ['personal', 'enterprise', 'relay_operator']
  const unique = Array.from(new Set((profiles || []).filter(Boolean)))
  return unique.sort((a, b) => {
    const left = order.indexOf(a)
    const right = order.indexOf(b)
    if (left === -1 && right === -1) return a.localeCompare(b)
    if (left === -1) return 1
    if (right === -1) return -1
    return left - right
  })
}

function profileLabel(profile: string): string {
  if (profile === 'personal') return t('setup.personal')
  if (profile === 'relay_operator') return t('setup.relay')
  if (profile === 'enterprise') return t('setup.enterprise')
  return profile
}

const updateState = computed(() => {
  if (!updateInfo.value) return t('settings.updateUnknown')
  if (updateInfo.value.has_update) return t('settings.updateAvailable')
  return t('settings.upToDate')
})

const updateSourceLabel = computed(() => {
  const source = updateInfo.value?.source
  if (!source || source === 'none') return ''
  if (source === 'official_catalog') return t('settings.signedCatalog')
  if (source === 'manifest') return t('settings.updateManifest')
  return source
})

function assignSettings(data: AdminSettings) {
  Object.assign(form, data)
  originalEnabledProfiles.value = normalizeProfileList(data.enabled_profiles || [])
  originalDefaultProfile.value = data.default_profile || ''
  profileConfirmOpen.value = false
}

function addLegalDocument() {
  const sequence = form.legal_documents.length + 1
  form.legal_documents.push({ id: crypto.randomUUID(), name: `文档 ${sequence}`, slug: `document-${sequence}`, content: '' })
}

function removeLegalDocument(index: number) {
  form.legal_documents.splice(index, 1)
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    assignSettings(await getAdminSettings())
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

async function refreshUpdates(force = false) {
  updateAction.value = 'check'
  error.value = ''
  try {
    updateInfo.value = await checkSystemUpdates(force)
    if (force) {
      message.value = t('settings.updateChecked')
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    updateAction.value = ''
  }
}

async function runUpdate() {
  updateAction.value = 'update'
  error.value = ''
  message.value = ''
  try {
    const result = await performSystemUpdate()
    message.value = result.message
    await refreshUpdates(false)
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    updateAction.value = ''
  }
}

async function runRollback() {
  updateAction.value = 'rollback'
  error.value = ''
  message.value = ''
  try {
    const result = await rollbackSystemUpdate()
    message.value = result.message
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    updateAction.value = ''
  }
}

async function runRestart() {
  updateAction.value = 'restart'
  error.value = ''
  message.value = ''
  try {
    const result = await restartSystem()
    message.value = result.message
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    updateAction.value = ''
  }
}

async function loadBackups() {
  try {
    backups.value = await listSystemBackups()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  }
}

async function runBackup() {
  archiveAction.value = 'backup'
  error.value = ''
  message.value = ''
  try {
    await createSystemBackup()
    message.value = t('settings.backupCreated')
    await loadBackups()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    archiveAction.value = ''
  }
}

async function runRestore(backup: SystemArchiveInfo) {
  if (!window.confirm(t('settings.restoreConfirm', { id: backup.id }))) return
  archiveAction.value = backup.id
  error.value = ''
  message.value = ''
  try {
    const result = await restoreSystemBackup(backup.id)
    message.value = result.message
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    archiveAction.value = ''
  }
}

async function runDiagnostic() {
  archiveAction.value = 'diagnostic'
  error.value = ''
  message.value = ''
  try {
    const bundle = await createDiagnosticBundle()
    await downloadDiagnosticBundle(bundle)
    message.value = t('settings.diagnosticCreated')
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    archiveAction.value = ''
  }
}

function formatArchiveSize(value: number): string {
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`
  return `${(value / 1024 / 1024).toFixed(1)} MB`
}

async function save(confirmedProfileChange = false) {
  if (profileChanged.value && confirmedProfileChange !== true) {
    activeSettingsTab.value = 'gateway'
    profileConfirmOpen.value = true
    return
  }
  saving.value = true
  error.value = ''
  message.value = ''
  try {
    profileConfirmOpen.value = false
    const nextSettings = await updateAdminSettings({ ...form })
    assignSettings(nextSettings)
    setPublicSettingsCache(nextSettings)
    await app.loadPublicSettings()
    message.value = t('common.saved')
    if (currentSurfaceDisabled(nextSettings)) {
      await router.replace(profileRoute(nextSettings.default_profile || nextSettings.enabled_profiles[0] || 'enterprise'))
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

function toggleLocale(locale: string) {
  const set = new Set(form.enabled_locales)
  if (set.has(locale)) {
    set.delete(locale)
  } else {
    set.add(locale)
  }
  form.enabled_locales = Array.from(set)
}

function toggleProfile(profile: string) {
  const set = new Set(form.enabled_profiles)
  if (set.has(profile)) {
    if (set.size === 1) return
    set.delete(profile)
  } else {
    set.add(profile)
  }
  form.enabled_profiles = Array.from(set)
  if (!form.default_profile || !set.has(form.default_profile)) {
    form.default_profile = form.enabled_profiles[0] || ''
  }
}

onMounted(async () => {
  await load()
  await Promise.all([refreshUpdates(false), loadBackups()])
})
</script>

<template>
  <main class="content">
    <section class="page-header">
      <div>
        <h1>{{ t('admin.title') }}</h1>
        <p>{{ t('admin.subtitle') }}</p>
        <div class="status-line">
          <span class="pill">{{ t('common.version') }} {{ form.version || '-' }}</span>
          <span class="pill">{{ t('common.storage') }} {{ form.storage_mode || '-' }}</span>
          <span class="pill">{{ gatewayBaseUrl }}</span>
        </div>
      </div>
      <div class="row-actions">
        <button class="button secondary" :disabled="loading" @click="load">
          <RefreshCw :size="17" />
          {{ t('common.refresh') }}
        </button>
        <button class="button" :disabled="saving" @click="save()">
          <Save :size="17" />
          {{ saving ? t('common.saving') : t('common.save') }}
        </button>
      </div>
    </section>

    <div v-if="message" class="notice success">{{ message }}</div>
    <div v-if="error" class="notice">{{ error }}</div>

    <nav class="settings-tabs" :aria-label="t('admin.settings')">
      <button
        v-for="tab in settingsTabs"
        :key="tab.id"
        type="button"
        :class="{ active: activeSettingsTab === tab.id }"
        @click="activeSettingsTab = tab.id"
      >
        <component :is="tab.icon" :size="17" />
        {{ t(tab.label) }}
      </button>
    </nav>

    <section class="grid section-gap">
      <div v-if="activeSettingsTab === 'general'" class="panel">
        <div class="panel-header">
          <SlidersHorizontal :size="18" />
          <h2>{{ t('settings.general') }}</h2>
        </div>
        <div class="panel-body">
          <div class="field">
            <label>{{ t('settings.siteName') }}</label>
            <input v-model="form.site_name" />
          </div>
          <div class="auth-provider-header"><div><strong>Backend Mode</strong><p>仅提供 API 与管理控制面，不展示门户首页。</p></div><label class="switch"><input v-model="form.backend_mode" type="checkbox"/><span></span></label></div>
          <div class="field">
            <label>{{ t('settings.siteSubtitle') }}</label>
            <input v-model="form.site_subtitle" />
          </div>
          <div class="field">
            <label>{{ t('settings.publicBaseUrl') }}</label>
            <input v-model="form.public_base_url" placeholder="https://ai.company.internal" />
          </div>
          <div class="auth-credential-grid">
            <div class="field"><label>默认分页数量</label><input v-model.number="form.default_page_size" type="number" min="5" max="1000"/></div>
            <div class="field"><label>可选分页数量</label><input :value="form.page_size_options.join(', ')" @change="form.page_size_options = ($event.target as HTMLInputElement).value.split(',').map(Number).filter(Number.isFinite)"/></div>
            <div class="field"><label>客服联系方式</label><input v-model="form.support_contact"/></div>
            <div class="field"><label>文档链接</label><input v-model="form.documentation_url" placeholder="https://docs.example.com"/></div>
          </div>
          <div class="field"><label>首页 Markdown / HTML</label><textarea v-model="form.home_content" rows="10"/></div>
          <div class="auth-provider-header"><div><strong>隐藏导入按钮</strong><p>在用户界面隐藏配置导入入口。</p></div><label class="switch"><input v-model="form.hide_import_button" type="checkbox"/><span></span></label></div>
          <div class="field">
            <label>{{ t('settings.defaultLocale') }}</label>
            <select v-model="form.default_locale">
              <option value="en-US">English</option>
              <option value="zh-CN">简体中文</option>
            </select>
          </div>
          <div class="field">
            <label>{{ t('settings.enabledLocales') }}</label>
            <div class="status-line">
              <button class="pill" type="button" @click="toggleLocale('en-US')">en-US</button>
              <button class="pill" type="button" @click="toggleLocale('zh-CN')">zh-CN</button>
            </div>
            <span class="hint">{{ form.enabled_locales.join(', ') }}</span>
          </div>
        </div>
      </div>

      <div v-if="activeSettingsTab === 'terms'" class="panel"><div class="panel-header"><FileText :size="18"/><h2>{{ t('settings.loginTerms') }}</h2></div><div class="panel-body auth-provider-list"><div class="auth-provider-card"><div class="auth-provider-header"><div><strong>{{ t('settings.enableLoginTerms') }}</strong><p>{{ t('settings.loginTermsHelp') }}</p></div><label class="switch"><input v-model="form.login_agreement_enabled" type="checkbox"/><span></span></label></div><div v-if="form.login_agreement_enabled" class="auth-provider-config"><div class="auth-credential-grid"><div class="field"><label>展示模式</label><div class="segmented-control"><button type="button" :class="{active:form.login_agreement_mode==='modal'}" @click="form.login_agreement_mode='modal'">Modal</button><button type="button" :class="{active:form.login_agreement_mode==='checkbox'}" @click="form.login_agreement_mode='checkbox'">Checkbox</button></div></div><div class="field"><label>更新日期</label><input v-model="form.login_agreement_updated_at" type="date"/></div></div></div></div><section v-for="(document,index) in form.legal_documents" :key="document.id" class="auth-provider-card"><div class="auth-provider-header"><div><strong>{{ document.name || `文档 ${index+1}` }}</strong><p>/legal/{{ document.slug }}</p></div><button class="button danger" type="button" @click="removeLegalDocument(index)">删除</button></div><div class="auth-provider-config"><div class="auth-credential-grid"><div class="field"><label>文档名称</label><input v-model="document.name"/></div><div class="field"><label>URL Slug</label><input v-model="document.slug" pattern="[a-z0-9-]+"/></div></div><div class="field"><label>Markdown 内容</label><textarea v-model="document.content" rows="12"/></div></div></section><button class="button secondary" type="button" @click="addLegalDocument">添加文档</button></div></div>

      <div v-if="activeSettingsTab === 'features'" class="panel"><div class="panel-header"><ToggleLeft :size="18"/><h2>{{ t('settings.featureFlags') }}</h2></div><div class="panel-body auth-provider-list"><section v-for="item in [{label:'settings.registrationEnabled',help:'settings.registrationHelp',value:form.registration_enabled,set:(v:boolean)=>form.registration_enabled=v},{label:'settings.emailVerifyEnabled',help:'settings.emailVerifyHelp',value:form.email_verify_enabled,set:(v:boolean)=>form.email_verify_enabled=v},{label:'settings.invitationRequired',help:'settings.invitationHelp',value:form.invitation_required,set:(v:boolean)=>form.invitation_required=v},{label:'settings.totpEnabled',help:'settings.totpHelp',value:form.totp_enabled,set:(v:boolean)=>form.totp_enabled=v}]" :key="item.label" class="auth-provider-card"><div class="auth-provider-header"><div><strong>{{ t(item.label) }}</strong><p>{{ t(item.help) }}</p></div><label class="switch"><input :checked="item.value" type="checkbox" @change="item.set(($event.target as HTMLInputElement).checked)"/><span></span></label></div></section></div></div>

      <div v-if="activeSettingsTab === 'defaults'" class="panel"><div class="panel-header"><UserRound :size="18"/><h2>{{ t('settings.userDefaults') }}</h2></div><div class="panel-body"><div class="auth-credential-grid"><div class="field"><label>{{ t('settings.defaultBalance') }}</label><input v-model.number="form.default_balance_cents" type="number" min="0"/></div><div class="field"><label>{{ t('settings.defaultConcurrency') }}</label><input v-model.number="form.default_concurrency" type="number" min="0"/></div><div class="field"><label>{{ t('settings.defaultRpm') }}</label><input v-model.number="form.default_rpm" type="number" min="0"/></div></div></div></div>

      <div v-if="activeSettingsTab === 'email'" class="panel"><div class="panel-header"><Mail :size="18"/><h2>{{ t('settings.emailSettings') }}</h2></div><div class="panel-body"><div class="auth-credential-grid"><div class="field"><label>SMTP Host</label><input v-model="form.smtp_host"/></div><div class="field"><label>SMTP Port</label><input v-model.number="form.smtp_port" type="number" min="1" max="65535"/></div><div class="field"><label>{{ t('auth.username') }}</label><input v-model="form.smtp_username"/></div><div class="field"><label>{{ t('auth.password') }}</label><input v-model="form.smtp_password" type="password" :placeholder="form.smtp_configured?t('plugins.keepSecret'):''"/></div><div class="field auth-config-span"><label>{{ t('settings.smtpFrom') }}</label><input v-model="form.smtp_from" type="email"/></div></div></div></div>

      <div v-if="activeSettingsTab === 'gateway'" class="panel">
        <div class="panel-header">
          <ServerCog :size="18" />
          <h2>{{ t('settings.deployment') }}</h2>
        </div>
        <div class="panel-body">
          <div class="notice profile-danger-notice">
            <strong>
              <AlertTriangle :size="15" />
              {{ t('settings.profileDangerTitle') }}
            </strong>
            <span>{{ t('settings.profileDangerHelp') }}</span>
          </div>
          <div class="field">
            <label>{{ t('settings.enabledProfiles') }}</label>
            <div class="status-line">
              <button
                class="pill"
                type="button"
                :class="{ 'status-success': form.enabled_profiles.includes('personal') }"
                @click="toggleProfile('personal')"
              >
                {{ profileLabel('personal') }}
              </button>
              <button
                class="pill"
                type="button"
                :class="{ 'status-success': form.enabled_profiles.includes('relay_operator') }"
                @click="toggleProfile('relay_operator')"
              >
                {{ profileLabel('relay_operator') }}
              </button>
              <button
                class="pill"
                type="button"
                :class="{ 'status-success': form.enabled_profiles.includes('enterprise') }"
                @click="toggleProfile('enterprise')"
              >
                {{ profileLabel('enterprise') }}
              </button>
            </div>
            <span class="hint">{{ form.enabled_profiles.join(', ') || '-' }}</span>
          </div>
          <div class="field">
            <label>{{ t('settings.defaultProfile') }}</label>
            <select v-model="form.default_profile">
              <option v-for="profile in form.enabled_profiles" :key="profile" :value="profile">{{ profileLabel(profile) }}</option>
            </select>
          </div>
          <div class="field">
            <label>{{ t('settings.gatewayBasePath') }}</label>
            <input v-model="form.gateway_base_path" />
          </div>
          <div class="field">
            <label>{{ t('settings.updateChannel') }}</label>
            <select v-model="form.update_channel">
              <option value="stable">stable</option>
              <option value="beta">beta</option>
              <option value="manual">manual</option>
            </select>
          </div>
        </div>
      </div>

      <div v-if="activeSettingsTab === 'backup'" class="panel">
        <div class="panel-header">
          <Download :size="18" />
          <h2>{{ t('settings.systemUpdate') }}</h2>
        </div>
        <div class="panel-body">
          <div class="status-line">
            <span class="pill">{{ updateState }}</span>
            <span class="pill">{{ updateInfo?.build_type || '-' }}</span>
            <span class="pill">{{ updateInfo?.platform || '-' }}</span>
            <span v-if="updateSourceLabel" class="pill">{{ updateSourceLabel }}</span>
            <span v-if="updateInfo?.signed_metadata" class="pill">{{ t('settings.signedMetadata') }}</span>
          </div>
          <div class="field">
            <label>{{ t('settings.latestVersion') }}</label>
            <input :value="updateInfo?.latest_version || form.version || '-'" readonly />
            <span v-if="updateInfo?.warning" class="hint">{{ updateInfo.warning }}</span>
          </div>
          <div class="status-line">
            <button class="button secondary" type="button" :disabled="!!updateAction" @click="refreshUpdates(true)">
              <RefreshCw :size="16" />
              {{ updateAction === 'check' ? t('common.loading') : t('settings.checkUpdates') }}
            </button>
            <button class="button" type="button" :disabled="!!updateAction || !updateInfo?.has_update" @click="runUpdate">
              <Download :size="16" />
              {{ updateAction === 'update' ? t('common.loading') : t('settings.oneClickUpdate') }}
            </button>
          </div>
          <div class="status-line">
            <button class="button secondary" type="button" :disabled="!!updateAction" @click="runRollback">
              <RotateCcw :size="16" />
              {{ t('settings.rollback') }}
            </button>
            <button class="button secondary" type="button" :disabled="!!updateAction || !updateInfo?.restart_supported" @click="runRestart">
              <Power :size="16" />
              {{ t('settings.restart') }}
            </button>
          </div>
        </div>
      </div>

      <div v-if="activeSettingsTab === 'backup'" class="panel">
        <div class="panel-header">
          <Database :size="18" />
          <h2>{{ t('settings.backupAndDiagnostics') }}</h2>
        </div>
        <div class="panel-body">
          <div class="notice profile-danger-notice">
            <strong><AlertTriangle :size="15" />{{ t('settings.restoreDangerTitle') }}</strong>
            <span>{{ t('settings.restoreDangerHelp') }}</span>
          </div>
          <div class="status-line">
            <button class="button" type="button" :disabled="!!archiveAction" @click="runBackup">
              <Download :size="16" />
              {{ archiveAction === 'backup' ? t('common.loading') : t('settings.createBackup') }}
            </button>
            <button class="button secondary" type="button" :disabled="!!archiveAction" @click="runDiagnostic">
              <ShieldCheck :size="16" />
              {{ archiveAction === 'diagnostic' ? t('common.loading') : t('settings.createDiagnostic') }}
            </button>
          </div>
          <div class="table-scroll">
            <table class="data-table crud-table">
              <thead>
                <tr>
                  <th>{{ t('settings.backup') }}</th>
                  <th>{{ t('audit.time') }}</th>
                  <th>{{ t('settings.archiveSize') }}</th>
                  <th>{{ t('common.actions') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="backup in backups" :key="backup.id">
                  <td><strong>{{ backup.id }}</strong><span>{{ backup.path }}</span></td>
                  <td>{{ new Date(backup.created_at).toLocaleString() }}</td>
                  <td>{{ formatArchiveSize(backup.size_bytes) }}</td>
                  <td class="table-actions">
                    <button class="icon-button" type="button" :title="t('common.download')" @click="downloadSystemBackup(backup)">
                      <Download :size="16" />
                    </button>
                    <button class="icon-button danger" type="button" :disabled="!!archiveAction" :title="t('settings.restore')" @click="runRestore(backup)">
                      <RotateCcw :size="16" />
                    </button>
                  </td>
                </tr>
                <tr v-if="!backups.length"><td colspan="4" class="empty-cell"></td></tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>

      <div v-if="activeSettingsTab === 'security'" class="panel">
        <div class="panel-header">
          <KeyRound :size="18" />
          <h2>{{ t('settings.identity') }}</h2>
        </div>
        <div class="panel-body auth-provider-list">
          <section class="auth-provider-card">
            <div class="auth-provider-header">
              <div><strong>{{ t('settings.feishuProviderTitle') }}</strong><p>{{ t('settings.feishuProviderHelp') }}</p></div>
              <label class="switch"><input v-model="form.feishu_enabled" type="checkbox" /><span></span></label>
            </div>
            <div v-if="form.feishu_enabled" class="auth-provider-config">
              <div class="field"><label>{{ t('settings.feishuRegion') }}</label><div class="segmented-control"><button type="button" :class="{ active: form.feishu_region === 'cn' }" @click="form.feishu_region = 'cn'">{{ t('settings.feishuChina') }}</button><button type="button" :class="{ active: form.feishu_region === 'global' }" @click="form.feishu_region = 'global'">{{ t('settings.larkGlobal') }}</button></div></div>
              <div class="auth-credential-grid"><div class="field"><label>{{ t('settings.feishuAppId') }}</label><input v-model="form.feishu_app_id" autocomplete="off" /></div><div class="field"><label>{{ t('settings.feishuAppSecret') }}</label><input v-model="form.feishu_app_secret" type="password" autocomplete="new-password" :placeholder="form.feishu_configured ? t('plugins.keepSecret') : ''" /></div></div>
              <span class="hint">{{ t('settings.feishuCallbackHelp') }} {{ feishuCallbackUrl }}</span>
            </div>
          </section>

          <section class="auth-provider-card">
            <div class="auth-provider-header">
              <div><strong>{{ t('settings.oidcProviderTitle') }}</strong><p>{{ t('settings.oidcProviderHelp') }}</p></div>
              <label class="switch"><input v-model="form.oidc_enabled" type="checkbox" /><span></span></label>
            </div>
            <div v-if="form.oidc_enabled" class="auth-provider-config auth-credential-grid">
              <div class="field"><label>{{ t('settings.oidcProviderName') }}</label><input v-model="form.oidc_provider_name" /></div>
              <div class="field"><label>{{ t('settings.oidcClientId') }}</label><input v-model="form.oidc_client_id" /></div>
              <div class="field auth-config-span"><label>{{ t('settings.oidcIssuerUrl') }}</label><input v-model="form.oidc_issuer_url" placeholder="https://idp.example.com" /></div>
            </div>
          </section>

          <section class="auth-provider-card auth-provider-static">
            <div class="auth-provider-header"><div><strong>{{ t('settings.localLoginTitle') }}</strong><p>{{ t('settings.localLoginHelp') }}</p></div><span class="status-badge success">{{ t('settings.alwaysEnabled') }}</span></div>
          </section>
        </div>
      </div>

      <div v-if="activeSettingsTab === 'backup'" class="panel">
        <div class="panel-header">
          <ShieldCheck :size="18" />
          <h2>{{ t('settings.governance') }}</h2>
        </div>
        <div class="panel-body">
          <div class="field">
            <label>{{ t('settings.retentionDays') }}</label>
            <input v-model.number="form.data_retention_days" type="number" min="1" max="3650" />
          </div>
          <div class="field">
            <label>{{ t('settings.promptLoggingMode') }}</label>
            <select v-model="form.prompt_logging_mode">
              <option value="metadata_only">{{ t('settings.metadataOnly') }}</option>
              <option value="disabled">{{ t('settings.disabled') }}</option>
              <option value="full">{{ t('settings.full') }}</option>
            </select>
          </div>
          <div class="field">
            <label>{{ t('settings.serviceCenterMode') }}</label>
            <select v-model="form.service_center_mode">
              <option value="disabled">disabled</option>
              <option value="online">online</option>
              <option value="private_mirror">private_mirror</option>
              <option value="offline">offline</option>
            </select>
          </div>
          <div class="field">
            <label>{{ t('settings.serviceCenter') }}</label>
            <div class="status-line">
              <span class="pill"><Database :size="14" />{{ form.service_center_mode }}</span>
              <span class="pill"><ShieldCheck :size="14" />{{ form.prompt_logging_mode }}</span>
            </div>
          </div>
        </div>
      </div>
    </section>

    <div v-if="profileConfirmOpen" class="modal-backdrop" @click.self="profileConfirmOpen = false">
      <section class="modal-card" role="dialog" aria-modal="true" :aria-label="t('settings.profileChangeConfirmTitle')">
        <header class="modal-header">
          <div>
            <h2>{{ t('settings.profileChangeConfirmTitle') }}</h2>
            <p>{{ t('settings.profileChangeConfirmSubtitle') }}</p>
          </div>
          <span class="pill status-danger">
            <AlertTriangle :size="16" />
            {{ t('settings.profileChangeImpact') }}
          </span>
        </header>
        <div class="modal-body profile-change-body">
          <div class="notice profile-danger-notice">
            <strong>
              <AlertTriangle :size="15" />
              {{ t('settings.currentSurfaceMayRedirect') }}
            </strong>
            <span>{{ t('settings.profileDangerHelp') }}</span>
          </div>
          <div class="setup-review-grid">
            <div v-if="addedProfiles.length">
              <label>{{ t('settings.profilesAdded') }}</label>
              <div class="chip-list">
                <span v-for="profile in addedProfiles" :key="profile" class="pill status-success">
                  {{ profileLabel(profile) }}
                </span>
              </div>
            </div>
            <div v-if="removedProfiles.length">
              <label>{{ t('settings.profilesRemoved') }}</label>
              <div class="chip-list">
                <span v-for="profile in removedProfiles" :key="profile" class="pill status-danger">
                  {{ profileLabel(profile) }}
                </span>
              </div>
            </div>
            <div v-if="defaultProfileChanged">
              <label>{{ t('settings.defaultProfileChanged') }}</label>
              <strong>{{ profileLabel(originalDefaultProfile || '-') }} -> {{ profileLabel(form.default_profile || '-') }}</strong>
              <span>{{ t('settings.defaultProfileChangedHelp') }}</span>
            </div>
          </div>
        </div>
        <footer class="modal-footer">
          <button class="button secondary" type="button" @click="profileConfirmOpen = false">
            {{ t('settings.keepEditing') }}
          </button>
          <button class="button danger" type="button" :disabled="saving" @click="save(true)">
            <Save :size="17" />
            {{ saving ? t('common.saving') : t('settings.confirmProfileChange') }}
          </button>
        </footer>
      </section>
    </div>
  </main>
</template>
