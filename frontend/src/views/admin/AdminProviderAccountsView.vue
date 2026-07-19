<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref } from 'vue'
import { Activity, Bot, Check, ChevronDown, CircleCheck, Cloud, Columns3, Edit3, KeyRound, MoreHorizontal, Plus, RefreshCw, Route, Save, Search, ShieldCheck, ShieldOff, Sparkles, Trash2, X, Zap } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import ProviderAccountModelEditor from '@/components/provider/ProviderAccountModelEditor.vue'
import { checkProviderAccount, clearProviderAccountCooldown, createProviderAccount, deleteProviderAccount, getProviderAccountHealthChecks, getProviderAccounts, getProviders, getRoutingGroups, updateProviderAccount } from '@/api/control'
import type { ProviderAccount, ProviderAccountHealthCheck, ProviderAccountRequest, ProviderAccountTempUnschedulableRule, ProviderConnection, RoutingGroup } from '@/types'

const { t, locale } = useI18n()
const SETTINGS_KEY = 'asterrouter_settings'
type PlatformID = 'anthropic' | 'openai' | 'gemini' | 'antigravity' | 'grok'
type ModelMapping = { from: string; to: string }
type AccountColumn = 'name' | 'id' | 'platform' | 'capacity' | 'status' | 'schedulable' | 'groups' | 'usage' | 'models' | 'health' | 'last_used' | 'created' | 'expires'
type BulkAction = 'schedule' | 'unschedule' | 'enable' | 'disable' | 'check'

const ACCOUNT_LIST_PREFERENCES_KEY = 'asterrouter-provider-account-list-v2'
const AUTO_REFRESH_INTERVALS = [15, 30, 60] as const
const DEFAULT_VISIBLE_COLUMNS: AccountColumn[] = ['name', 'id', 'platform', 'capacity', 'status', 'schedulable', 'groups', 'usage', 'last_used', 'created', 'expires']
const ACCOUNT_COLUMN_ORDER: AccountColumn[] = ['name', 'id', 'platform', 'capacity', 'status', 'schedulable', 'groups', 'usage', 'models', 'health', 'last_used', 'created', 'expires']

const PLATFORM_CONFIG: Record<PlatformID, { label: string; icon: typeof Sparkles; type: string; baseURL: string; placeholder: string }> = {
  anthropic: { label: 'Anthropic', icon: Sparkles, type: 'anthropic_compatible', baseURL: 'https://api.anthropic.com/v1', placeholder: 'sk-ant-api03-...' },
  openai: { label: 'OpenAI', icon: Zap, type: 'openai_compatible', baseURL: 'https://api.openai.com/v1', placeholder: 'sk-proj-...' },
  gemini: { label: 'Gemini', icon: Sparkles, type: 'gemini_compatible', baseURL: 'https://generativelanguage.googleapis.com/v1beta', placeholder: 'AIza...' },
  antigravity: { label: 'Antigravity', icon: Cloud, type: 'openai_compatible', baseURL: 'https://cloudcode-pa.googleapis.com', placeholder: 'sk-...' },
  grok: { label: 'Grok', icon: Bot, type: 'openai_compatible', baseURL: 'https://api.x.ai/v1', placeholder: 'xai-...' }
}
const platformEntries = Object.entries(PLATFORM_CONFIG) as Array<[PlatformID, (typeof PLATFORM_CONFIG)[PlatformID]]>
const platform = ref<PlatformID>('openai')

interface AdvancedSettings {
  notes: string
  base_url: string
  model_restriction_mode: 'whitelist' | 'mapping'
  model_mappings: ModelMapping[]
  pool_mode_enabled: boolean
  pool_retry_count: number
  pool_retry_status_codes: string
  custom_error_codes_enabled: boolean
  custom_error_codes: number[]
  header_override_enabled: boolean
  header_override_json: string
  quota_enabled: boolean
  quota_total_limit: number | null
  quota_daily_limit: number | null
  quota_weekly_limit: number | null
  quota_reset_mode: 'rolling' | 'fixed'
  quota_reset_timezone: string
  proxy_url: string
  intercept_warmup_requests: boolean
  auto_pause_on_expired: boolean
  auto_passthrough: boolean
  ws_mode: 'off' | 'ctx_pool' | 'passthrough' | 'http_bridge'
  long_context_billing: boolean
  compact_mode: 'auto' | 'force_on' | 'force_off'
  compact_model_mappings: ModelMapping[]
  responses_mode: 'auto' | 'force_responses' | 'force_chat_completions'
  endpoint_capabilities: string[]
}

function defaultAdvanced(): AdvancedSettings {
  return {
    notes: '', base_url: '', model_restriction_mode: 'whitelist', model_mappings: [],
    pool_mode_enabled: false, pool_retry_count: 3, pool_retry_status_codes: '401, 403, 429',
    custom_error_codes_enabled: false, custom_error_codes: [], header_override_enabled: false,
    header_override_json: '', quota_enabled: false, quota_total_limit: null, quota_daily_limit: null,
    quota_weekly_limit: null, quota_reset_mode: 'rolling', quota_reset_timezone: 'UTC', proxy_url: '',
    intercept_warmup_requests: false, auto_pause_on_expired: true, auto_passthrough: false,
    ws_mode: 'off', long_context_billing: false, compact_mode: 'auto', compact_model_mappings: [],
    responses_mode: 'auto', endpoint_capabilities: ['chat_completions', 'embeddings']
  }
}

const loading = ref(false)
const saving = ref(false)
const actionID = ref('')
const batchBusy = ref(false)
const error = ref('')
const message = ref('')
const accounts = ref<ProviderAccount[]>([])
const groups = ref<RoutingGroup[]>([])
const providers = ref<ProviderConnection[]>([])
const healthChecks = ref<Record<string, ProviderAccountHealthCheck>>({})
const query = ref('')
const statusFilter = ref('')
const platformFilter = ref('')
const selectedIDs = ref<string[]>([])
const visibleColumns = ref<AccountColumn[]>([...DEFAULT_VISIBLE_COLUMNS])
const columnsMenuOpen = ref(false)
const autoRefreshMenuOpen = ref(false)
const rowMenuID = ref('')
const autoRefreshEnabled = ref(false)
const autoRefreshInterval = ref<(typeof AUTO_REFRESH_INTERVALS)[number]>(30)
const autoRefreshCountdown = ref(0)
const modalOpen = ref(false)
const editing = ref<ProviderAccount | null>(null)
const modelEditor = ref<{ discover: () => Promise<void> } | null>(null)
const customModel = ref('')
const customErrorCode = ref<number | null>(null)
const advanced = reactive<AdvancedSettings>(defaultAdvanced())
let autoRefreshTimer: number | undefined

const form = reactive<ProviderAccountRequest>({
  provider_id: '', name: '', platform: 'openai_compatible', auth_type: 'api_key', adapter_config: {}, status: 'active', schedulable: true,
  priority: 50, weight: 100, concurrency: 3, rpm_limit: 0, tpm_limit: 0, load_factor: null, rate_multiplier: 1,
  models: [], auto_enable_new_models: false, group_ids: [], secret: '', expires_at: '', circuit_failure_threshold: 5,
  circuit_open_seconds: 60, temp_unschedulable_rules: []
})

const providerByID = computed(() => new Map(providers.value.map((item) => [item.id, item])))
const groupByID = computed(() => new Map(groups.value.map((item) => [item.id, item])))
const selectedProvider = computed(() => providerByID.value.get(form.provider_id))
const currentPlatform = computed(() => PLATFORM_CONFIG[platform.value])
const modelDiscoveryEnabled = computed(() => ['openai_compatible', 'anthropic_compatible', 'gemini_compatible'].includes(selectedProvider.value?.type || ''))
const platforms = computed(() => Array.from(new Set(accounts.value.map((item) => inferPlatform(item)))).sort())
const filteredAccounts = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return accounts.value.filter((account) => {
    if (statusFilter.value && account.status !== statusFilter.value) return false
    if (platformFilter.value && inferPlatform(account) !== platformFilter.value) return false
    const providerName = providerByID.value.get(account.provider_id)?.name || ''
    const groupNames = account.group_ids.map((id) => groupByID.value.get(id)?.name || '')
    return !keyword || [account.id, account.name, providerName, account.platform, account.auth_type, ...groupNames, ...account.models].some((value) => value.toLowerCase().includes(keyword))
  })
})
const selectedIDSet = computed(() => new Set(selectedIDs.value))
const selectedAccounts = computed(() => accounts.value.filter((account) => selectedIDSet.value.has(account.id)))
const allVisibleSelected = computed(() => filteredAccounts.value.length > 0 && filteredAccounts.value.every((account) => selectedIDSet.value.has(account.id)))
const someVisibleSelected = computed(() => filteredAccounts.value.some((account) => selectedIDSet.value.has(account.id)) && !allVisibleSelected.value)
const visibleColumnCount = computed(() => visibleColumns.value.length + 2)
const columnOptions = computed<Array<{ key: AccountColumn; label: string }>>(() => [
  { key: 'name', label: t('providerAccounts.listName') },
  { key: 'id', label: t('providerAccounts.accountId') },
  { key: 'platform', label: t('providerAccounts.platformType') },
  { key: 'capacity', label: t('providerAccounts.capacity') },
  { key: 'status', label: t('providers.status') },
  { key: 'schedulable', label: t('providerAccounts.listScheduling') },
  { key: 'groups', label: t('providerAccounts.groups') },
  { key: 'usage', label: t('providerAccounts.usageWindow') },
  { key: 'models', label: t('providers.models') },
  { key: 'health', label: t('providerAccounts.health') },
  { key: 'last_used', label: t('providerAccounts.lastUsed') },
  { key: 'created', label: t('providerAccounts.createdAt') },
  { key: 'expires', label: t('providerAccounts.expiresAt') }
])
const orderedVisibleColumns = computed(() => ACCOUNT_COLUMN_ORDER.filter((column) => visibleColumns.value.includes(column)))
const summary = computed(() => ({
  total: accounts.value.length,
  schedulable: accounts.value.filter((item) => item.status === 'active' && item.schedulable).length,
  healthy: Object.values(healthChecks.value).filter((item) => item.status === 'ok').length,
  attention: accounts.value.filter((item) => item.status === 'error' || !item.secret_configured).length
}))

function cloneRules(rules: ProviderAccountTempUnschedulableRule[]) { return rules.map((rule) => ({ ...rule, keywords: [...rule.keywords] })) }
function cloneMappings(rows: ModelMapping[]) { return rows.map((row) => ({ from: row.from, to: row.to })) }
function parseSettings(config: Record<string, string> = {}): Partial<AdvancedSettings> {
  try { return JSON.parse(config[SETTINGS_KEY] || '{}') as Partial<AdvancedSettings> } catch { return {} }
}
const accountSettingsByID = computed(() => new Map(accounts.value.map((account) => [account.id, parseSettings(account.adapter_config)])))
function loadSettings(config: Record<string, string>) {
  Object.assign(advanced, defaultAdvanced(), parseSettings(config))
  advanced.model_mappings = cloneMappings(advanced.model_mappings || [])
  advanced.compact_model_mappings = cloneMappings(advanced.compact_model_mappings || [])
  advanced.endpoint_capabilities = [...(advanced.endpoint_capabilities || ['chat_completions', 'embeddings'])]
}
function settingsConfig() { return { [SETTINGS_KEY]: JSON.stringify({ ...advanced, model_mappings: cloneMappings(advanced.model_mappings), compact_model_mappings: cloneMappings(advanced.compact_model_mappings), endpoint_capabilities: [...advanced.endpoint_capabilities] }) } }
function accountToRequest(account: ProviderAccount): ProviderAccountRequest {
  return {
    provider_id: account.provider_id, name: account.name, platform: account.platform, auth_type: 'api_key', adapter_config: { ...(account.adapter_config || {}) }, status: account.status,
    schedulable: account.schedulable, priority: account.priority, weight: account.weight, concurrency: account.concurrency, rpm_limit: account.rpm_limit, tpm_limit: account.tpm_limit,
    load_factor: account.load_factor ?? null, rate_multiplier: account.rate_multiplier, models: [...account.models], auto_enable_new_models: account.auto_enable_new_models, group_ids: [...account.group_ids], secret: '',
    expires_at: account.expires_at ? account.expires_at.slice(0, 10) : '', circuit_failure_threshold: account.circuit_failure_threshold, circuit_open_seconds: account.circuit_open_seconds, temp_unschedulable_rules: cloneRules(account.temp_unschedulable_rules)
  }
}
function inferPlatform(account: ProviderAccount | ProviderConnection): PlatformID {
  const source = `${account.name} ${'base_url' in account ? account.base_url : ''}`.toLowerCase()
  const providerType = 'platform' in account ? account.platform : account.type
  if (source.includes('x.ai') || source.includes('grok')) return 'grok'
  if (source.includes('cloudcode') || source.includes('antigravity')) return 'antigravity'
  if (providerType === 'anthropic_compatible' || source.includes('anthropic')) return 'anthropic'
  if (providerType === 'gemini_compatible' || source.includes('generativelanguage')) return 'gemini'
  return 'openai'
}
function providerForPlatform(id: PlatformID) {
  const type = PLATFORM_CONFIG[id].type
  const exact = providers.value.find((provider) => provider.type === type && inferPlatform(provider) === id)
  if (exact) return exact
  if (id === 'grok' || id === 'antigravity') return undefined
  return providers.value.find((provider) => provider.type === type)
}
function selectPlatform(id: PlatformID) {
  platform.value = id
  form.platform = PLATFORM_CONFIG[id].type
  const provider = providerForPlatform(id)
  form.provider_id = provider?.id || ''
  advanced.base_url = PLATFORM_CONFIG[id].baseURL
}
function syncProvider() {
  const provider = selectedProvider.value
  if (!provider) return
  platform.value = inferPlatform(provider)
  form.platform = provider.type
  if (!advanced.base_url) advanced.base_url = provider.base_url
}
function resetAdvanced() { Object.assign(advanced, defaultAdvanced()) }
function resetForm() {
  const provider = providers.value[0]
  Object.assign(form, { provider_id: provider?.id || '', name: '', platform: provider?.type || 'openai_compatible', auth_type: 'api_key', adapter_config: {}, status: 'active', schedulable: true, priority: 50, weight: 100, concurrency: 3, rpm_limit: 0, tpm_limit: 0, load_factor: null, rate_multiplier: 1, models: [], auto_enable_new_models: false, group_ids: groups.value[0] ? [groups.value[0].id] : [], secret: '', expires_at: '', circuit_failure_threshold: 5, circuit_open_seconds: 60, temp_unschedulable_rules: [] })
  platform.value = provider ? inferPlatform(provider) : 'openai'
  advanced.base_url = provider?.base_url || currentPlatform.value.baseURL
}
function openCreate() { editing.value = null; resetAdvanced(); resetForm(); modalOpen.value = true }
function openEdit(account: ProviderAccount) { editing.value = account; Object.assign(form, accountToRequest(account)); loadSettings(account.adapter_config); platform.value = inferPlatform(account); advanced.base_url = advanced.base_url || providerByID.value.get(account.provider_id)?.base_url || currentPlatform.value.baseURL; modalOpen.value = true }
function closeModal() { modalOpen.value = false; editing.value = null; customModel.value = ''; customErrorCode.value = null }
function toggleGroup(id: string) { form.group_ids = form.group_ids.includes(id) ? form.group_ids.filter((item) => item !== id) : [...form.group_ids, id] }
function addRule() { form.temp_unschedulable_rules = [...form.temp_unschedulable_rules, { status_code: 429, keywords: [], duration_minutes: 30 }] }
function removeRule(index: number) { form.temp_unschedulable_rules = form.temp_unschedulable_rules.filter((_, item) => item !== index) }
function setRuleKeywords(rule: ProviderAccountTempUnschedulableRule, value: string) { rule.keywords = value.split(',').map((item) => item.trim()).filter(Boolean) }
function addModel() { const value = customModel.value.trim(); if (!value || form.models.includes(value)) return; form.models = [...form.models, value]; customModel.value = '' }
function removeModel(model: string) { form.models = form.models.filter((item) => item !== model) }
function addMapping(target: 'model_mappings' | 'compact_model_mappings') { advanced[target].push({ from: '', to: '' }) }
function removeMapping(target: 'model_mappings' | 'compact_model_mappings', index: number) { advanced[target].splice(index, 1) }
function addErrorCode() { const code = Number(customErrorCode.value); if (Number.isInteger(code) && code >= 100 && code <= 599 && !advanced.custom_error_codes.includes(code)) advanced.custom_error_codes.push(code); customErrorCode.value = null }
function removeErrorCode(code: number) { advanced.custom_error_codes = advanced.custom_error_codes.filter((item) => item !== code) }
function toggleCapability(value: string) { advanced.endpoint_capabilities = advanced.endpoint_capabilities.includes(value) ? advanced.endpoint_capabilities.filter((item) => item !== value) : [...advanced.endpoint_capabilities, value] }
function statusClass(status: string) { return status === 'active' || status === 'ok' ? 'status-success' : status === 'error' ? 'status-warning' : 'status-danger' }
function activeCooldownUntil(account: ProviderAccount) { if (!account.cooldown_until) return ''; const until = new Date(account.cooldown_until); return until.getTime() > Date.now() ? until.toLocaleTimeString(locale.value, { hour: '2-digit', minute: '2-digit' }) : '' }
function accountReady(account: ProviderAccount) { return account.status === 'active' && account.schedulable && account.secret_configured }
function accountPlatform(account: ProviderAccount) { return PLATFORM_CONFIG[inferPlatform(account)] }
function accountGroups(account: ProviderAccount) { return account.group_ids.map((id) => groupByID.value.get(id)).filter((group): group is RoutingGroup => Boolean(group)) }
function accountNotes(account: ProviderAccount) { return accountSettingsByID.value.get(account.id)?.notes?.trim() || '' }
function accountQuota(account: ProviderAccount) {
  const settings = accountSettingsByID.value.get(account.id)
  if (!settings?.quota_enabled) return ''
  const format = (value: number | null | undefined) => value && value > 0 ? `$${new Intl.NumberFormat(locale.value, { maximumFractionDigits: 2 }).format(value)}` : '∞'
  return t('providerAccounts.quotaConfigured', { daily: format(settings.quota_daily_limit), weekly: format(settings.quota_weekly_limit), total: format(settings.quota_total_limit) })
}
function formatLimit(value: number) { return value > 0 ? new Intl.NumberFormat(locale.value).format(value) : '∞' }
function accountCapacity(account: ProviderAccount) {
  const max = account.load_factor && account.load_factor > 0 ? account.load_factor : account.concurrency
  const safeMax = Math.max(max || 0, 1)
  const configured = Math.max(account.concurrency || 0, 0)
  return { configured, max: safeMax, percent: Math.min(100, Math.round((configured / safeMax) * 100)) }
}
function formatDateTime(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return new Intl.DateTimeFormat(locale.value, { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }).format(date)
}
function formatRelativeTime(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  const seconds = (date.getTime() - Date.now()) / 1000
  const ranges: Array<[Intl.RelativeTimeFormatUnit, number]> = [['year', 31_536_000], ['month', 2_592_000], ['week', 604_800], ['day', 86_400], ['hour', 3_600], ['minute', 60]]
  const formatter = new Intl.RelativeTimeFormat(locale.value, { numeric: 'auto' })
  for (const [unit, size] of ranges) {
    if (Math.abs(seconds) >= size) return formatter.format(Math.round(seconds / size), unit)
  }
  return formatter.format(Math.round(seconds), 'second')
}
function isExpired(value?: string) { return Boolean(value && new Date(value).getTime() <= Date.now()) }
function isColumnVisible(column: AccountColumn) { return visibleColumns.value.includes(column) }
function toggleColumn(column: AccountColumn) {
  visibleColumns.value = isColumnVisible(column) ? visibleColumns.value.filter((item) => item !== column) : [...visibleColumns.value, column]
  saveListPreferences()
}
function toggleAccountSelection(id: string) {
  selectedIDs.value = selectedIDSet.value.has(id) ? selectedIDs.value.filter((item) => item !== id) : [...selectedIDs.value, id]
}
function toggleSelectAllVisible() {
  const visibleIDs = new Set(filteredAccounts.value.map((account) => account.id))
  if (allVisibleSelected.value) selectedIDs.value = selectedIDs.value.filter((id) => !visibleIDs.has(id))
  else selectedIDs.value = Array.from(new Set([...selectedIDs.value, ...visibleIDs]))
}
function clearSelection() { selectedIDs.value = [] }
function saveListPreferences() {
  try {
    localStorage.setItem(ACCOUNT_LIST_PREFERENCES_KEY, JSON.stringify({ columns: visibleColumns.value, auto_refresh: autoRefreshEnabled.value, interval: autoRefreshInterval.value }))
  } catch { /* Browser storage can be unavailable in hardened sessions. */ }
}
function loadListPreferences() {
  try {
    const raw = localStorage.getItem(ACCOUNT_LIST_PREFERENCES_KEY)
    if (!raw) return
    const parsed = JSON.parse(raw) as { columns?: string[]; auto_refresh?: boolean; interval?: number }
    const allowed = new Set<AccountColumn>(DEFAULT_VISIBLE_COLUMNS)
    const columns = (parsed.columns || []).filter((column): column is AccountColumn => allowed.has(column as AccountColumn))
    if (columns.length) visibleColumns.value = columns
    autoRefreshEnabled.value = parsed.auto_refresh === true
    const interval = Number(parsed.interval)
    if (AUTO_REFRESH_INTERVALS.includes(interval as (typeof AUTO_REFRESH_INTERVALS)[number])) autoRefreshInterval.value = interval as (typeof AUTO_REFRESH_INTERVALS)[number]
  } catch { /* Ignore malformed local preferences and retain defaults. */ }
}
function stopAutoRefresh() {
  if (autoRefreshTimer !== undefined) window.clearInterval(autoRefreshTimer)
  autoRefreshTimer = undefined
}
function startAutoRefresh() {
  stopAutoRefresh()
  if (!autoRefreshEnabled.value) { autoRefreshCountdown.value = 0; return }
  autoRefreshCountdown.value = autoRefreshInterval.value
  autoRefreshTimer = window.setInterval(() => {
    if (loading.value || saving.value || batchBusy.value) return
    if (autoRefreshCountdown.value > 1) { autoRefreshCountdown.value -= 1; return }
    autoRefreshCountdown.value = autoRefreshInterval.value
    void load()
  }, 1000)
}
function setAutoRefreshEnabled(enabled: boolean) { autoRefreshEnabled.value = enabled; startAutoRefresh(); saveListPreferences() }
function setAutoRefreshInterval(interval: (typeof AUTO_REFRESH_INTERVALS)[number]) { autoRefreshInterval.value = interval; startAutoRefresh(); saveListPreferences() }
function toggleAutoRefreshMenu() { autoRefreshMenuOpen.value = !autoRefreshMenuOpen.value; columnsMenuOpen.value = false; rowMenuID.value = '' }
function toggleColumnsMenu() { columnsMenuOpen.value = !columnsMenuOpen.value; autoRefreshMenuOpen.value = false; rowMenuID.value = '' }
function toggleRowMenu(id: string) { rowMenuID.value = rowMenuID.value === id ? '' : id; columnsMenuOpen.value = false; autoRefreshMenuOpen.value = false }

async function load() {
  loading.value = true; error.value = ''
  try {
    const [groupData, providerData, accountData, healthData] = await Promise.all([getRoutingGroups(), getProviders(), getProviderAccounts(), getProviderAccountHealthChecks()])
    groups.value = groupData; providers.value = providerData; accounts.value = accountData; healthChecks.value = Object.fromEntries(healthData.map((item) => [item.account_id, item]))
    const accountIDs = new Set(accountData.map((account) => account.id))
    selectedIDs.value = selectedIDs.value.filter((id) => accountIDs.has(id))
  } catch (err) { error.value = err instanceof Error ? err.message : t('common.failed') }
  finally { loading.value = false }
}
async function save() {
  saving.value = true; error.value = ''; message.value = ''
  try {
    if (!form.provider_id || !form.name.trim()) throw new Error(t('providerAccounts.validationBasic'))
    if (!editing.value && !form.secret.trim()) throw new Error(t('providerAccounts.validationSecret'))
    const payload: ProviderAccountRequest = { ...form, auth_type: 'api_key', adapter_config: { ...form.adapter_config, ...settingsConfig() }, models: [...form.models], group_ids: [...form.group_ids], temp_unschedulable_rules: cloneRules(form.temp_unschedulable_rules), load_factor: form.load_factor ? Number(form.load_factor) : null }
    if (editing.value) {
      const updated = await updateProviderAccount(editing.value.id, payload); editing.value = updated; Object.assign(form, accountToRequest(updated)); loadSettings(updated.adapter_config); message.value = t('providerAccounts.updated'); await load()
    } else {
      const created = await createProviderAccount(payload); editing.value = created; Object.assign(form, accountToRequest(created)); loadSettings(created.adapter_config); accounts.value = [...accounts.value, created]; message.value = t('providerAccounts.created'); await nextTick(); if (modelDiscoveryEnabled.value) await modelEditor.value?.discover()
    }
  } catch (err) { error.value = err instanceof Error ? err.message : t('common.failed') }
  finally { saving.value = false }
}
async function runAccountAction(account: ProviderAccount, action: 'check' | 'toggle' | 'cooldown' | 'schedulable' | 'delete') {
  actionID.value = `${action}:${account.id}`; error.value = ''; message.value = ''
  try {
    if (action === 'delete') {
      if (!window.confirm(t('providerAccounts.deleteConfirm', { name: account.name }))) return
      await deleteProviderAccount(account.id)
      message.value = t('providerAccounts.deleted')
    }
    if (action === 'check') { const result = await checkProviderAccount(account.id); healthChecks.value = { ...healthChecks.value, [account.id]: result }; message.value = result.message }
    if (action === 'toggle') { await updateProviderAccount(account.id, { ...accountToRequest(account), status: account.status === 'disabled' ? 'active' : 'disabled' }); message.value = account.status === 'disabled' ? t('providerAccounts.enabled') : t('providerAccounts.disabled') }
    if (action === 'cooldown') { await clearProviderAccountCooldown(account.id); message.value = t('providerAccounts.cooldownCleared') }
    if (action === 'schedulable') { await updateProviderAccount(account.id, { ...accountToRequest(account), schedulable: !account.schedulable }); message.value = t('providerAccounts.schedulabilityUpdated') }
    await load()
  } catch (err) { error.value = err instanceof Error ? err.message : t('common.failed') }
  finally { actionID.value = '' }
}
async function runBulkAction(action: BulkAction) {
  const targets = [...selectedAccounts.value]
  if (!targets.length || batchBusy.value) return
  batchBusy.value = true; error.value = ''; message.value = ''; rowMenuID.value = ''
  try {
    const results = await Promise.allSettled(targets.map(async (account) => {
      if (action === 'check') return { id: account.id, health: await checkProviderAccount(account.id) }
      const request = accountToRequest(account)
      if (action === 'schedule') request.schedulable = true
      if (action === 'unschedule') request.schedulable = false
      if (action === 'enable') request.status = 'active'
      if (action === 'disable') request.status = 'disabled'
      return { id: account.id, account: await updateProviderAccount(account.id, request) }
    }))
    const failedIDs: string[] = []
    results.forEach((result, index) => {
      if (result.status === 'rejected') { failedIDs.push(targets[index].id); return }
      if (result.value.health) healthChecks.value = { ...healthChecks.value, [result.value.id]: result.value.health }
    })
    await load()
    const succeeded = targets.length - failedIDs.length
    selectedIDs.value = failedIDs
    if (succeeded) message.value = t('providerAccounts.bulkUpdated', { count: succeeded })
    if (failedIDs.length) error.value = t('providerAccounts.bulkFailed', { failed: failedIDs.length, total: targets.length })
  } catch (err) { error.value = err instanceof Error ? err.message : t('common.failed') }
  finally { batchBusy.value = false }
}
function handleRowAction(account: ProviderAccount, action: 'check' | 'edit' | 'toggle' | 'cooldown' | 'delete') {
  rowMenuID.value = ''
  if (action === 'edit') { openEdit(account); return }
  void runAccountAction(account, action)
}
function handleModelsSynced(account: ProviderAccount) { editing.value = account; Object.assign(form, accountToRequest(account)); loadSettings(account.adapter_config); accounts.value = accounts.value.map((item) => item.id === account.id ? account : item) }
onMounted(() => { loadListPreferences(); startAutoRefresh(); void load() })
onUnmounted(stopAutoRefresh)
</script>

<template>
  <main class="content crud-page account-workbench">
    <section class="page-header"><div><h1>{{ t('admin.providerAccounts') }}</h1><p>{{ t('providerAccounts.subtitle') }}</p></div><button class="button" type="button" @click="openCreate"><Plus :size="17" />{{ t('providerAccounts.newAccount') }}</button></section>
    <div class="crud-summary"><span><strong>{{ summary.total }}</strong>{{ t('providerAccounts.accounts') }}</span><span><strong>{{ summary.schedulable }}</strong>{{ t('providerAccounts.schedulable') }}</span><span><strong>{{ summary.healthy }}</strong>{{ t('providerAccounts.health') }}</span><span><strong>{{ summary.attention }}</strong>{{ t('providerAccounts.error') }}</span></div>
    <section class="table-toolbar account-list-toolbar">
      <div class="account-list-filters">
        <label class="search-box"><Search :size="17" /><input v-model="query" :placeholder="t('providerAccounts.searchPlaceholder')" /></label>
        <select v-model="platformFilter" :aria-label="t('providers.platform')"><option value="">{{ t('routingGroups.allPlatforms') }}</option><option v-for="item in platforms" :key="item" :value="item">{{ PLATFORM_CONFIG[item].label }}</option></select>
        <select v-model="statusFilter" :aria-label="t('providers.status')"><option value="">{{ t('providers.allStatuses') }}</option><option value="active">active</option><option value="error">error</option><option value="disabled">disabled</option></select>
      </div>
      <div class="account-toolbar-actions">
        <button class="icon-button" type="button" :disabled="loading" :aria-label="t('common.refresh')" :title="t('common.refresh')" @click="load"><RefreshCw :class="{ spinning: loading }" :size="17" /></button>
        <div class="account-menu-wrap">
          <button class="button secondary toolbar-menu-button" type="button" :aria-expanded="autoRefreshMenuOpen" :aria-label="t('providerAccounts.autoRefresh')" @click="toggleAutoRefreshMenu"><RefreshCw :class="{ spinning: autoRefreshEnabled }" :size="16" /><span>{{ autoRefreshEnabled ? t('providerAccounts.autoRefreshCountdown', { seconds: autoRefreshCountdown }) : t('providerAccounts.autoRefresh') }}</span><ChevronDown :size="14" /></button>
          <div v-if="autoRefreshMenuOpen" class="account-menu account-refresh-menu">
            <button type="button" @click="setAutoRefreshEnabled(!autoRefreshEnabled)"><span>{{ t('providerAccounts.enableAutoRefresh') }}</span><Check v-if="autoRefreshEnabled" :size="16" /></button>
            <hr />
            <button v-for="seconds in AUTO_REFRESH_INTERVALS" :key="seconds" type="button" @click="setAutoRefreshInterval(seconds)"><span>{{ t('providerAccounts.refreshEvery', { seconds }) }}</span><Check v-if="autoRefreshInterval === seconds" :size="16" /></button>
          </div>
        </div>
        <div class="account-menu-wrap">
          <button class="button secondary toolbar-menu-button" type="button" :aria-expanded="columnsMenuOpen" :aria-label="t('providerAccounts.columnSettings')" @click="toggleColumnsMenu"><Columns3 :size="16" /><span>{{ t('providerAccounts.columnSettings') }}</span><ChevronDown :size="14" /></button>
          <div v-if="columnsMenuOpen" class="account-menu account-column-menu">
            <strong>{{ t('providerAccounts.visibleColumns') }}</strong>
            <button v-for="column in columnOptions" :key="column.key" type="button" @click="toggleColumn(column.key)"><span>{{ column.label }}</span><Check v-if="isColumnVisible(column.key)" :size="16" /></button>
          </div>
        </div>
      </div>
    </section>
    <div v-if="message" class="notice success">{{ message }}</div><div v-if="error && !modalOpen" class="notice">{{ error }}</div>
    <section v-if="selectedIDs.length" class="account-bulk-bar" :aria-label="t('providerAccounts.bulkActions')">
      <div><strong>{{ t('providerAccounts.selectedCount', { count: selectedIDs.length }) }}</strong><button type="button" @click="clearSelection">{{ t('providerAccounts.clearSelection') }}</button></div>
      <div class="account-bulk-actions">
        <button class="button secondary" type="button" :disabled="batchBusy" @click="runBulkAction('schedule')"><ShieldCheck :size="15" />{{ t('providerAccounts.bulkSchedule') }}</button>
        <button class="button secondary" type="button" :disabled="batchBusy" @click="runBulkAction('unschedule')"><ShieldOff :size="15" />{{ t('providerAccounts.bulkUnschedule') }}</button>
        <button class="button secondary" type="button" :disabled="batchBusy" @click="runBulkAction('enable')">{{ t('providerAccounts.bulkEnable') }}</button>
        <button class="button secondary" type="button" :disabled="batchBusy" @click="runBulkAction('disable')">{{ t('providerAccounts.bulkDisable') }}</button>
        <button class="button secondary" type="button" :disabled="batchBusy" @click="runBulkAction('check')"><Activity :size="15" />{{ t('providerAccounts.bulkCheck') }}</button>
      </div>
    </section>
    <section class="panel table-panel account-table-panel"><div class="panel-body table-scroll"><table class="data-table crud-table accounts-data-table">
      <colgroup><col class="account-select-column" /><col v-for="column in orderedVisibleColumns" :key="`col-${column}`" :class="`account-column-${column}`" /><col class="account-actions-column" /></colgroup>
      <thead><tr>
        <th class="account-select-column"><input type="checkbox" :checked="allVisibleSelected" :aria-checked="someVisibleSelected ? 'mixed' : allVisibleSelected" :aria-label="t('providerAccounts.selectVisible')" @change="toggleSelectAllVisible" /></th>
        <th v-for="column in orderedVisibleColumns" :key="`head-${column}`">{{ columnOptions.find((item) => item.key === column)?.label }}</th>
        <th class="account-actions-column">{{ t('common.actions') }}</th>
      </tr></thead>
      <tbody>
        <tr v-for="account in filteredAccounts" :key="account.id" :class="{ 'account-row-selected': selectedIDSet.has(account.id) }" :data-account-id="account.id">
          <td class="account-select-column"><input type="checkbox" :checked="selectedIDSet.has(account.id)" :aria-label="t('providerAccounts.selectAccount', { name: account.name })" @change="toggleAccountSelection(account.id)" /></td>
          <td v-for="column in orderedVisibleColumns" :key="`${account.id}-${column}`" :class="`account-cell-${column}`">
            <template v-if="column === 'name'"><strong>{{ account.name }}</strong><span v-if="accountNotes(account)" :title="accountNotes(account)">{{ accountNotes(account) }}</span><span v-else>{{ providerByID.get(account.provider_id)?.name || '-' }}</span></template>
            <template v-else-if="column === 'id'"><span class="account-id-value" :title="account.id">{{ account.id }}</span></template>
            <template v-else-if="column === 'platform'"><div class="platform-badges"><span class="platform-badge" :class="`platform-${inferPlatform(account)}`">{{ accountPlatform(account).label }}</span><span class="platform-badge api-type-badge">{{ t('providerAccounts.apiKeyTitle') }}</span></div><span class="provider-type-label">{{ account.platform }}</span></template>
            <template v-else-if="column === 'capacity'"><div class="capacity-summary"><div class="capacity-track"><span :style="{ width: `${accountCapacity(account).percent}%` }" /></div><strong>{{ accountCapacity(account).configured }} / {{ accountCapacity(account).max }}</strong></div></template>
            <template v-else-if="column === 'status'"><div class="account-cell-stack"><span class="pill" :class="accountReady(account) ? 'status-success' : statusClass(account.status)">{{ accountReady(account) ? t('providerAccounts.ready') : account.status }}</span><span v-if="activeCooldownUntil(account)" class="pill status-warning">cooldown · {{ activeCooldownUntil(account) }}</span><span v-if="account.error_message" class="account-error-text" :title="account.error_message">{{ account.error_message }}</span></div></template>
            <template v-else-if="column === 'schedulable'"><button class="schedulable-switch" type="button" role="switch" :class="{ active: account.schedulable }" :aria-checked="account.schedulable" :aria-label="t('providerAccounts.toggleSchedulable', { name: account.name })" :title="account.schedulable ? t('providerAccounts.schedulable') : t('providerAccounts.notSchedulable')" :disabled="actionID === `schedulable:${account.id}`" @click="runAccountAction(account, 'schedulable')"><span /></button><small>{{ account.schedulable ? t('providerAccounts.schedulable') : t('providerAccounts.notSchedulable') }}</small></template>
            <template v-else-if="column === 'groups'"><div class="account-group-list"><span v-for="group in accountGroups(account).slice(0, 3)" :key="group.id" class="account-group-chip" :title="group.description">{{ group.name }}</span><span v-if="accountGroups(account).length > 3" class="account-group-chip">+{{ accountGroups(account).length - 3 }}</span><span v-if="!accountGroups(account).length" class="hint">-</span></div></template>
            <template v-else-if="column === 'usage'"><strong>RPM {{ formatLimit(account.rpm_limit) }} · TPM {{ formatLimit(account.tpm_limit) }}</strong><span>{{ t('providerAccounts.concurrency') }} {{ account.concurrency }} · P{{ account.priority }} · W{{ account.weight }}</span><span v-if="accountQuota(account)" class="account-quota-summary">{{ accountQuota(account) }}</span></template>
            <template v-else-if="column === 'models'"><strong>{{ account.models.length }}</strong><span class="account-model-summary" :title="account.models.join(', ')">{{ account.models.slice(0, 2).join(' · ') || '-' }}</span></template>
            <template v-else-if="column === 'health'"><template v-if="healthChecks[account.id]"><span class="pill" :class="statusClass(healthChecks[account.id].status)">{{ healthChecks[account.id].status }} · {{ healthChecks[account.id].latency_ms }}ms</span><span class="account-health-message" :title="healthChecks[account.id].message">{{ healthChecks[account.id].message }}</span></template><span v-else class="hint">{{ t('providers.notChecked') }}</span></template>
            <template v-else-if="column === 'last_used'"><time :datetime="account.last_used_at" :title="formatDateTime(account.last_used_at)">{{ formatRelativeTime(account.last_used_at) }}</time></template>
            <template v-else-if="column === 'created'"><time :datetime="account.created_at" :title="formatDateTime(account.created_at)">{{ formatDateTime(account.created_at) }}</time></template>
            <template v-else-if="column === 'expires'"><time :datetime="account.expires_at" :title="formatDateTime(account.expires_at)">{{ account.expires_at ? formatRelativeTime(account.expires_at) : t('providerAccounts.noExpiry') }}</time><span v-if="isExpired(account.expires_at)" class="pill status-warning">{{ t('providerAccounts.expired') }}</span></template>
          </td>
          <td class="account-actions-column"><div class="row-actions account-row-actions"><button class="icon-button" type="button" :aria-label="t('common.edit')" :title="t('common.edit')" @click="openEdit(account)"><Edit3 :size="16" /></button><button class="icon-button danger" type="button" :disabled="actionID === `delete:${account.id}`" :aria-label="t('providerAccounts.delete')" :title="t('providerAccounts.delete')" @click="runAccountAction(account, 'delete')"><Trash2 :size="16" /></button><div class="account-menu-wrap row-menu-wrap"><button class="icon-button" type="button" :aria-expanded="rowMenuID === account.id" :aria-label="t('providerAccounts.moreActions')" :title="t('providerAccounts.moreActions')" @click="toggleRowMenu(account.id)"><MoreHorizontal :size="17" /></button><div v-if="rowMenuID === account.id" class="account-menu row-action-menu"><button type="button" :disabled="actionID === `check:${account.id}`" @click="handleRowAction(account, 'check')"><Activity :size="15" /><span>{{ t('providers.check') }}</span></button><button type="button" :disabled="actionID === `toggle:${account.id}`" @click="handleRowAction(account, 'toggle')"><ShieldCheck v-if="account.status === 'disabled'" :size="15" /><ShieldOff v-else :size="15" /><span>{{ account.status === 'disabled' ? t('providerAccounts.enable') : t('providerAccounts.disable') }}</span></button><button v-if="activeCooldownUntil(account)" type="button" @click="handleRowAction(account, 'cooldown')"><CircleCheck :size="15" /><span>{{ t('providerAccounts.clearCooldown') }}</span></button></div></div></div></td>
        </tr>
        <tr v-if="!filteredAccounts.length"><td :colspan="visibleColumnCount" class="empty-cell">{{ loading ? t('common.loading') : t('providerAccounts.empty') }}</td></tr>
      </tbody>
    </table></div></section>

    <div v-if="modalOpen" class="modal-backdrop" @click.self="closeModal">
      <section class="modal-card modal-card-wide account-wizard account-settings-modal" role="dialog" aria-modal="true" :aria-label="editing ? t('providerAccounts.editAccount') : t('providerAccounts.newAccount')">
        <header class="modal-header"><div><h2>{{ editing ? t('providerAccounts.editAccount') : t('providerAccounts.newAccount') }}</h2><p>{{ t('providerAccounts.modalSubtitle') }}</p></div><button class="icon-button" type="button" :aria-label="t('common.close')" @click="closeModal"><X :size="19" /></button></header>
        <form class="account-settings-form" @submit.prevent="save">
          <div class="modal-body account-settings-body">
            <section class="account-section">
              <div class="section-title"><div><h3>{{ t('providerAccounts.basicInfo') }}</h3><p>{{ t('providerAccounts.basicInfoHint') }}</p></div></div>
              <div class="form-grid">
                <div class="field form-span-2"><label for="account-provider">{{ t('providerAccounts.provider') }}</label><select id="account-provider" v-model="form.provider_id" required @change="syncProvider"><option value="" disabled>{{ t('providerAccounts.selectProvider') }}</option><option v-for="provider in providers" :key="provider.id" :value="provider.id">{{ provider.name }} · {{ provider.type }}</option></select></div>
                <div class="field"><label for="account-name">{{ t('providerAccounts.name') }}</label><input id="account-name" v-model="form.name" required :placeholder="t('providerAccounts.namePlaceholder')" /></div>
                <div class="field"><label for="account-status">{{ t('providers.status') }}</label><select id="account-status" v-model="form.status"><option value="active">active</option><option value="disabled">disabled</option></select></div>
                <div class="field form-span-2"><label for="account-notes">{{ t('providerAccounts.notes') }}</label><textarea id="account-notes" v-model="advanced.notes" rows="2" :placeholder="t('providerAccounts.notesPlaceholder')" /></div>
              </div>
              <div class="field provider-platform-field"><label>{{ t('providers.platform') }}</label><div class="provider-platform-tabs" role="tablist" :aria-label="t('providers.platform')"><button v-for="[id, config] in platformEntries" :key="id" class="provider-platform-tab" :class="{ active: platform === id }" type="button" role="tab" :aria-selected="platform === id" @click="selectPlatform(id)"><component :is="config.icon" :size="16" />{{ config.label }}</button></div></div>
            </section>

            <section class="account-section">
              <div class="field"><label>{{ t('providerAccounts.authType') }}</label><div class="account-type-card" aria-current="true"><span class="account-type-icon"><KeyRound :size="18" /></span><span><strong>{{ t('providerAccounts.apiKeyTitle') }}</strong><small>{{ t('providerAccounts.apiKeyDescription', { platform: currentPlatform.label }) }}</small></span><Check class="account-type-check" :size="18" /></div></div>
              <div class="form-grid credential-grid"><div class="field"><label for="account-base-url">{{ t('providerAccounts.baseUrl') }}</label><input id="account-base-url" v-model="advanced.base_url" required class="provider-mono-input" :placeholder="currentPlatform.baseURL" /><span class="hint">{{ t('providerAccounts.baseUrlHint', { platform: currentPlatform.label }) }}</span></div><div class="field"><label for="account-secret">{{ t('providerAccounts.apiKey') }}</label><input id="account-secret" v-model="form.secret" type="password" :required="!editing" autocomplete="new-password" class="provider-mono-input" :placeholder="editing ? t('providers.keepSecret') : currentPlatform.placeholder" /><span class="hint">{{ t('providerAccounts.apiKeyHint') }}</span></div></div>
            </section>

            <section class="account-section"><div class="section-title"><div><h3>{{ t('providerAccounts.modelRestriction') }}</h3><p>{{ t('providerAccounts.modelRestrictionHint') }}</p></div></div><div class="segmented-control"><button type="button" :class="{ active: advanced.model_restriction_mode === 'whitelist' }" @click="advanced.model_restriction_mode = 'whitelist'"><Check :size="15" />{{ t('providerAccounts.modelWhitelist') }}</button><button type="button" :class="{ active: advanced.model_restriction_mode === 'mapping' }" @click="advanced.model_restriction_mode = 'mapping'"><Route :size="15" />{{ t('providerAccounts.modelMapping') }}</button></div><div v-if="advanced.model_restriction_mode === 'whitelist'"><ProviderAccountModelEditor v-if="editing" ref="modelEditor" v-model="form.models" v-model:auto-enable-new-models="form.auto_enable_new_models" :account-id="editing.id" :discovery-enabled="modelDiscoveryEnabled" @synced="handleModelsSynced" /><div v-else class="model-precreate"><div class="tag-list"><span v-for="model in form.models" :key="model" class="model-tag">{{ model }}<button type="button" :aria-label="t('providerAccounts.removeModel', { model })" @click="removeModel(model)"><X :size="12" /></button></span><span v-if="!form.models.length" class="hint">{{ t('providerAccounts.modelSaveToDiscover') }}</span></div><div class="inline-fields"><input v-model="customModel" :placeholder="t('providerAccounts.customModelPlaceholder')" @keydown.enter.prevent="addModel" /><button class="button secondary" type="button" :disabled="!customModel.trim()" @click="addModel"><Plus :size="15" />{{ t('providerAccounts.addCustomModel') }}</button></div></div></div><div v-else class="mapping-list"><ProviderAccountModelEditor v-if="editing" ref="modelEditor" v-model="form.models" v-model:auto-enable-new-models="form.auto_enable_new_models" :account-id="editing.id" :discovery-enabled="modelDiscoveryEnabled" @synced="handleModelsSynced" /><div v-else class="model-precreate"><div class="tag-list"><span v-for="model in form.models" :key="model" class="model-tag">{{ model }}<button type="button" :aria-label="t('providerAccounts.removeModel', { model })" @click="removeModel(model)"><X :size="12" /></button></span><span v-if="!form.models.length" class="hint">{{ t('providerAccounts.modelSaveToDiscover') }}</span></div><div class="inline-fields"><input v-model="customModel" :placeholder="t('providerAccounts.actualModel')" @keydown.enter.prevent="addModel" /><button class="button secondary" type="button" :disabled="!customModel.trim()" @click="addModel"><Plus :size="15" />{{ t('providerAccounts.addCustomModel') }}</button></div></div><div v-for="(mapping, index) in advanced.model_mappings" :key="index" class="mapping-row"><input v-model="mapping.from" :placeholder="t('providerAccounts.requestModel')" /><span>→</span><input v-model="mapping.to" :placeholder="t('providerAccounts.actualModel')" /><button class="icon-button danger" type="button" :aria-label="t('common.delete')" @click="removeMapping('model_mappings', index)"><Trash2 :size="15" /></button></div><button class="button secondary" type="button" @click="addMapping('model_mappings')"><Plus :size="15" />{{ t('providerAccounts.addMapping') }}</button></div></section>

            <section class="account-section setting-section"><div class="section-title"><div><h3>{{ t('providerAccounts.poolMode') }}</h3><p>{{ t('providerAccounts.poolModeHint') }}</p></div><label class="switch"><input v-model="advanced.pool_mode_enabled" type="checkbox" /><span /></label></div><div v-if="advanced.pool_mode_enabled" class="setting-grid"><div class="field"><label>{{ t('providerAccounts.poolRetryCount') }}</label><input v-model.number="advanced.pool_retry_count" type="number" min="0" max="10" /></div><div class="field"><label>{{ t('providerAccounts.poolRetryStatusCodes') }}</label><input v-model="advanced.pool_retry_status_codes" placeholder="401, 403, 429" /></div></div></section>
            <section class="account-section setting-section"><div class="section-title"><div><h3>{{ t('providerAccounts.customErrorCodes') }}</h3><p>{{ t('providerAccounts.customErrorCodesHint') }}</p></div><label class="switch"><input v-model="advanced.custom_error_codes_enabled" type="checkbox" /><span /></label></div><div v-if="advanced.custom_error_codes_enabled" class="inline-fields"><div class="tag-list"><span v-for="code in advanced.custom_error_codes" :key="code" class="model-tag">{{ code }}<button type="button" @click="removeErrorCode(code)"><X :size="12" /></button></span></div><input v-model.number="customErrorCode" type="number" min="100" max="599" placeholder="429" @keydown.enter.prevent="addErrorCode" /><button class="button secondary" type="button" @click="addErrorCode"><Plus :size="15" />{{ t('providerAccounts.addErrorCode') }}</button></div></section>
            <section class="account-section setting-section"><div class="section-title"><div><h3>{{ t('providerAccounts.headerOverride') }}</h3><p>{{ t('providerAccounts.headerOverrideHint') }}</p></div><label class="switch"><input v-model="advanced.header_override_enabled" type="checkbox" /><span /></label></div><textarea v-if="advanced.header_override_enabled" v-model="advanced.header_override_json" rows="4" class="provider-mono-input" placeholder="{\n  &quot;X-Client-Name&quot;: &quot;asterrouter&quot;\n}" /></section>
            <section class="account-section"><div class="section-title"><div><h3>{{ t('providerAccounts.quotaControl') }}</h3><p>{{ t('providerAccounts.quotaControlHint') }}</p></div><label class="switch"><input v-model="advanced.quota_enabled" type="checkbox" /><span /></label></div><div v-if="advanced.quota_enabled" class="setting-grid quota-grid"><div class="field"><label>{{ t('providerAccounts.quotaDaily') }}</label><input v-model.number="advanced.quota_daily_limit" type="number" min="0" step="0.01" placeholder="0" /></div><div class="field"><label>{{ t('providerAccounts.quotaWeekly') }}</label><input v-model.number="advanced.quota_weekly_limit" type="number" min="0" step="0.01" placeholder="0" /></div><div class="field"><label>{{ t('providerAccounts.quotaTotal') }}</label><input v-model.number="advanced.quota_total_limit" type="number" min="0" step="0.01" placeholder="0" /></div><div class="field"><label>{{ t('providerAccounts.quotaResetMode') }}</label><select v-model="advanced.quota_reset_mode"><option value="rolling">{{ t('providerAccounts.quotaRolling') }}</option><option value="fixed">{{ t('providerAccounts.quotaFixed') }}</option></select></div><div v-if="advanced.quota_reset_mode === 'fixed'" class="field"><label>{{ t('providerAccounts.quotaTimezone') }}</label><input v-model="advanced.quota_reset_timezone" /></div></div></section>

            <section class="account-section"><div class="section-title"><div><h3>{{ t('providerAccounts.routingControls') }}</h3><p>{{ t('providerAccounts.routingControlsHint') }}</p></div></div><div class="setting-grid"><div class="field"><label>{{ t('providerAccounts.concurrency') }}</label><input v-model.number="form.concurrency" type="number" min="0" /></div><div class="field"><label>{{ t('providerAccounts.loadFactor') }}</label><input v-model.number="form.load_factor" type="number" min="0" :placeholder="t('providerAccounts.loadFactorPlaceholder')" /></div><div class="field"><label>{{ t('providers.priority') }}</label><input v-model.number="form.priority" type="number" min="1" /></div><div class="field"><label>{{ t('providerAccounts.weight') }}</label><input v-model.number="form.weight" type="number" min="1" max="10000" /></div><div class="field"><label>{{ t('providerAccounts.multiplier') }}</label><input v-model.number="form.rate_multiplier" type="number" min="0" step="0.01" /></div><div class="field"><label>{{ t('providerAccounts.rpmLimit') }}</label><input v-model.number="form.rpm_limit" type="number" min="0" /></div><div class="field"><label>{{ t('providerAccounts.tpmLimit') }}</label><input v-model.number="form.tpm_limit" type="number" min="0" /></div><div class="field"><label>{{ t('providerAccounts.expiresAt') }}</label><input v-model="form.expires_at" type="date" /></div></div><div class="setting-grid"><div class="field"><label>{{ t('providerAccounts.proxy') }}</label><input v-model="advanced.proxy_url" placeholder="http://user:pass@host:port" /></div><div class="field"><label>{{ t('providerAccounts.circuitFailureThreshold') }}</label><input v-model.number="form.circuit_failure_threshold" type="number" min="1" max="100" /></div><div class="field"><label>{{ t('providerAccounts.circuitOpenSeconds') }}</label><input v-model.number="form.circuit_open_seconds" type="number" min="1" max="86400" /></div></div></section>

            <section class="account-section setting-section"><div class="section-title"><div><h3>{{ t('providerAccounts.transportFeatures') }}</h3><p>{{ t('providerAccounts.transportFeaturesHint') }}</p></div></div><div class="feature-list"><label class="feature-row"><span><strong>{{ t('providerAccounts.autoPassthrough') }}</strong><small>{{ t('providerAccounts.autoPassthroughHint') }}</small></span><input v-model="advanced.auto_passthrough" type="checkbox" /></label><label class="feature-row"><span><strong>{{ t('providerAccounts.interceptWarmup') }}</strong><small>{{ t('providerAccounts.interceptWarmupHint') }}</small></span><input v-model="advanced.intercept_warmup_requests" type="checkbox" /></label><label class="feature-row"><span><strong>{{ t('providerAccounts.autoPauseOnExpired') }}</strong><small>{{ t('providerAccounts.autoPauseOnExpiredHint') }}</small></span><input v-model="advanced.auto_pause_on_expired" type="checkbox" /></label><label class="feature-row"><span><strong>{{ t('providerAccounts.longContextBilling') }}</strong><small>{{ t('providerAccounts.longContextBillingHint') }}</small></span><input v-model="advanced.long_context_billing" type="checkbox" /></label></div><div class="setting-grid"><div class="field"><label>{{ t('providerAccounts.wsMode') }}</label><select v-model="advanced.ws_mode"><option value="off">{{ t('providerAccounts.wsOff') }}</option><option value="ctx_pool">{{ t('providerAccounts.wsContextPool') }}</option><option value="passthrough">{{ t('providerAccounts.wsPassthrough') }}</option><option value="http_bridge">{{ t('providerAccounts.wsHttpBridge') }}</option></select></div><div class="field"><label>{{ t('providerAccounts.compactMode') }}</label><select v-model="advanced.compact_mode"><option value="auto">Auto</option><option value="force_on">Force On</option><option value="force_off">Force Off</option></select></div><div class="field"><label>{{ t('providerAccounts.responsesMode') }}</label><select v-model="advanced.responses_mode"><option value="auto">Auto</option><option value="force_responses">Responses API</option><option value="force_chat_completions">Chat Completions</option></select></div></div><div class="capability-list"><label v-for="capability in [{ value: 'chat_completions', label: t('providerAccounts.capabilityChat') }, { value: 'embeddings', label: t('providerAccounts.capabilityEmbeddings') }, { value: 'responses', label: t('providerAccounts.capabilityResponses') }]" :key="capability.value" class="checkbox-line"><input type="checkbox" :checked="advanced.endpoint_capabilities.includes(capability.value)" @change="toggleCapability(capability.value)" /><span>{{ capability.label }}</span></label></div></section>

            <section class="account-section"><div class="section-title"><div><h3>{{ t('providerAccounts.tempUnschedulableRules') }}</h3><p>{{ t('providerAccounts.tempUnschedulableRulesHelp') }}</p></div></div><div class="rule-list"><div v-for="(rule, index) in form.temp_unschedulable_rules" :key="index" class="rule-row"><input v-model.number="rule.status_code" type="number" min="100" max="599" :placeholder="t('providerAccounts.ruleStatusCode')" /><input :value="rule.keywords.join(', ')" :placeholder="t('providerAccounts.ruleKeywords')" @input="setRuleKeywords(rule, ($event.target as HTMLInputElement).value)" /><input v-model.number="rule.duration_minutes" type="number" min="1" :placeholder="t('providerAccounts.ruleDurationMinutes')" /><button class="icon-button danger" type="button" :aria-label="t('common.delete')" @click="removeRule(index)"><Trash2 :size="15" /></button></div><button class="button secondary" type="button" @click="addRule"><Plus :size="15" />{{ t('providerAccounts.addRule') }}</button></div></section>

            <section class="account-section"><div class="section-title"><div><h3>{{ t('providerAccounts.groups') }}</h3><p>{{ t('providerAccounts.groupsHint') }}</p></div></div><div class="check-list"><label v-for="group in groups" :key="group.id" class="checkbox-line"><input type="checkbox" :checked="form.group_ids.includes(group.id)" @change="toggleGroup(group.id)" /><span><strong>{{ group.name }}</strong><small>{{ group.platform }} · {{ group.group_type }}</small></span></label><span v-if="!groups.length" class="hint">{{ t('providerAccounts.noGroups') }}</span></div></section>
          </div>
          <div v-if="error && modalOpen" class="notice account-modal-error">{{ error }}</div>
          <footer class="modal-footer"><button class="button secondary" type="button" @click="closeModal">{{ t('common.cancel') }}</button><button class="button" type="submit" :disabled="saving"><Save :size="17" />{{ saving ? t('common.saving') : t('common.save') }}</button></footer>
        </form>
      </section>
    </div>
  </main>
</template>

<style scoped>
.account-list-toolbar { position: relative; align-items: stretch; }
.account-list-filters, .account-toolbar-actions { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; gap: 8px; }
.account-list-filters { flex: 1 1 560px; }
.account-list-filters .search-box { min-width: min(360px, 100%); flex: 1 1 280px; }
.account-list-filters select { min-width: 150px; }
.account-toolbar-actions { flex: 0 1 auto; justify-content: flex-end; }
.toolbar-menu-button { min-height: 38px; gap: 7px; white-space: nowrap; }
.account-menu-wrap { position: relative; }
.account-menu { position: absolute; z-index: 35; top: calc(100% + 7px); right: 0; display: grid; width: 230px; padding: 6px; border: 1px solid var(--border); border-radius: 8px; background: var(--surface); box-shadow: var(--shadow-lg); }
.account-menu > strong { padding: 8px 10px 6px; color: var(--text-muted); font-size: 11px; text-transform: uppercase; }
.account-menu > button { display: flex; min-height: 38px; align-items: center; justify-content: space-between; gap: 10px; padding: 8px 10px; border: 0; border-radius: 6px; background: transparent; color: var(--text-secondary); cursor: pointer; font-size: 13px; text-align: left; }
.account-menu > button:hover { background: var(--surface-hover); color: var(--text); }
.account-menu > button:disabled { cursor: not-allowed; opacity: .55; }
.account-menu hr { width: 100%; margin: 5px 0; border: 0; border-top: 1px solid var(--border); }
.account-column-menu { max-height: min(520px, 70vh); overflow-y: auto; }
.account-bulk-bar { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 12px; padding: 12px 14px; border: 1px solid var(--primary-200); border-radius: 8px; background: var(--primary-50); }
.account-bulk-bar > div:first-child { display: flex; align-items: center; gap: 10px; color: var(--text); font-size: 13px; }
.account-bulk-bar > div:first-child button { border: 0; background: transparent; color: var(--primary-700); cursor: pointer; font-size: 12px; }
.account-bulk-actions { display: flex; flex-wrap: wrap; gap: 6px; }
.account-bulk-actions .button { min-height: 34px; padding: 7px 10px; font-size: 12px; }
.account-table-panel { overflow: visible; }
.accounts-data-table { width: max(100%, 1840px); min-width: 1840px; table-layout: fixed; font-size: 13px; }
.accounts-data-table col.account-column-name { width: 190px; }
.accounts-data-table col.account-column-id { width: 150px; }
.accounts-data-table col.account-column-platform { width: 190px; }
.accounts-data-table col.account-column-capacity { width: 115px; }
.accounts-data-table col.account-column-status { width: 150px; }
.accounts-data-table col.account-column-schedulable { width: 100px; }
.accounts-data-table col.account-column-groups { width: 150px; }
.accounts-data-table col.account-column-usage { width: 230px; }
.accounts-data-table col.account-column-models { width: 180px; }
.accounts-data-table col.account-column-health { width: 210px; }
.accounts-data-table col.account-column-last_used { width: 125px; }
.accounts-data-table col.account-column-created { width: 165px; }
.accounts-data-table col.account-column-expires { width: 135px; }
.accounts-data-table th { overflow: hidden; padding-block: 13px; font-size: 12px; letter-spacing: 0; text-overflow: ellipsis; white-space: nowrap; }
.accounts-data-table td { min-height: 58px; font-size: 13px; }
.accounts-data-table td, .accounts-data-table th { vertical-align: middle; }
.accounts-data-table td strong { font-size: 13px; line-height: 1.45; }
.accounts-data-table td > span:not(.pill), .accounts-data-table time { color: var(--text-muted); font-size: 12px; line-height: 1.45; }
.accounts-data-table input[type='checkbox'] { width: 16px; height: 16px; accent-color: var(--primary-600); cursor: pointer; }
.account-row-selected { background: color-mix(in srgb, var(--primary-50) 75%, transparent); }
.account-select-column { width: 42px; padding-inline: 12px !important; text-align: center !important; }
.account-id-value { display: block; overflow: hidden; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; text-overflow: ellipsis; white-space: nowrap; }
.account-cell-name { max-width: 220px; }
.account-cell-name > span { overflow: hidden; max-width: 210px; text-overflow: ellipsis; white-space: nowrap; }
.account-name-cell > span, .account-model-summary, .account-health-message, .account-error-text { overflow: hidden; max-width: 210px; text-overflow: ellipsis; white-space: nowrap; }
.platform-badges { display: flex !important; flex-wrap: wrap; gap: 5px; }
.platform-badge { display: inline-flex !important; min-height: 24px; align-items: center; padding: 3px 7px; border-radius: 5px; font-size: 11px; font-weight: 700; }
.platform-openai { background: var(--success-bg); color: var(--success); }
.platform-anthropic { background: var(--warning-bg); color: var(--warning); }
.platform-gemini { background: var(--info-bg); color: var(--info); }
.platform-antigravity { background: color-mix(in srgb, var(--success-bg) 70%, var(--info-bg)); color: var(--primary-700); }
.platform-grok { background: var(--surface-hover); color: var(--text); }
.api-type-badge { border: 1px solid var(--border); background: var(--surface); color: var(--text-secondary); }
.provider-type-label { margin-top: 5px !important; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.capacity-summary { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: center; gap: 8px; }
.capacity-summary strong { color: var(--text-secondary); font-size: 11px !important; white-space: nowrap; }
.capacity-track { width: 58px; height: 7px; overflow: hidden; border-radius: 999px; background: var(--surface-hover); }
.capacity-track span { display: block; height: 100%; border-radius: inherit; background: var(--primary-500); }
.account-cell-stack { display: flex; max-width: 190px; flex-wrap: wrap; align-items: center; gap: 5px; }
.account-error-text { display: block !important; width: 100%; color: var(--danger) !important; }
.account-cell-schedulable { width: 120px; text-align: center !important; }
.account-cell-schedulable small { display: block; margin-top: 5px; color: var(--text-muted); font-size: 11px; white-space: nowrap; }
.schedulable-switch { position: relative; display: inline-flex; width: 40px; height: 23px; padding: 0; border: 0; border-radius: 999px; background: var(--accent-300); cursor: pointer; transition: background 150ms ease; }
.schedulable-switch span { position: absolute; top: 3px; left: 3px; width: 17px; height: 17px; border-radius: 50%; background: #fff; box-shadow: 0 1px 3px rgb(15 23 42 / 24%); pointer-events: none; transition: transform 150ms ease; }
.schedulable-switch.active { background: var(--primary-500); }
.schedulable-switch.active span { transform: translateX(17px); }
.schedulable-switch:focus-visible { outline: 2px solid var(--primary-500); outline-offset: 2px; }
.schedulable-switch:disabled { cursor: wait; opacity: .6; }
.account-group-list { display: flex !important; max-width: 210px; flex-wrap: wrap; gap: 5px; }
.account-group-chip { display: inline-flex !important; max-width: 120px; min-height: 24px; align-items: center; overflow: hidden; padding: 3px 7px; border: 1px solid var(--border); border-radius: 5px; background: var(--surface-subtle); color: var(--text-secondary) !important; font-size: 11px !important; text-overflow: ellipsis; white-space: nowrap; }
.account-quota-summary { color: var(--primary-700) !important; }
.accounts-data-table time { display: block; max-width: 145px; }
.account-actions-column { position: sticky; z-index: 5; right: 0; width: 92px; min-width: 92px; background: var(--surface); box-shadow: -8px 0 12px -12px rgb(15 23 42 / 42%); }
.accounts-data-table th.account-actions-column { z-index: 7; background: var(--surface-subtle); }
.accounts-data-table tbody tr:hover .account-actions-column, .account-row-selected .account-actions-column { background: var(--surface-hover); }
.account-row-actions { flex-wrap: nowrap; justify-content: flex-end; }
.row-menu-wrap { position: relative; }
.row-action-menu { top: 50%; right: 42px; width: 180px; transform: translateY(-50%); }
.row-action-menu > button { justify-content: flex-start; }
.spinning { animation: account-spin 1s linear infinite; }
@keyframes account-spin { to { transform: rotate(360deg); } }
.account-settings-modal { width: min(920px, 100%); max-height: min(900px, calc(100vh - 28px)); }
.account-settings-form { display: flex; min-height: 0; flex: 1; flex-direction: column; }
.account-settings-body { display: grid; gap: 18px; padding: 20px 24px 26px; }
.account-section { display: grid; gap: 14px; padding: 18px; border: 1px solid var(--border); border-radius: 8px; background: var(--surface-subtle); }
.account-section:first-child { border-top: 3px solid var(--primary-500); }
.section-title { display: flex; align-items: flex-start; justify-content: space-between; gap: 14px; }
.section-title h3 { margin: 0; color: var(--text); font-size: 14px; }
.section-title p { margin: 4px 0 0; color: var(--text-muted); font-size: 11px; line-height: 1.5; }
.provider-platform-tabs { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 4px; padding: 4px; border-radius: 8px; background: var(--surface-hover); }
.provider-platform-tab { display: inline-flex; min-height: 40px; align-items: center; justify-content: center; gap: 6px; border: 0; border-radius: 7px; background: transparent; color: var(--text-muted); cursor: pointer; font-size: 12px; font-weight: 650; }
.provider-platform-tab.active { background: var(--surface); color: var(--primary-700); box-shadow: var(--shadow-sm); }
.account-type-card { display: flex; min-height: 70px; align-items: center; gap: 12px; padding: 12px 14px; border: 2px solid var(--primary-500); border-radius: 8px; background: var(--primary-50); }
.account-type-icon { display: inline-flex; width: 38px; height: 38px; align-items: center; justify-content: center; border-radius: 8px; background: var(--primary-500); color: white; }
.account-type-card > span:nth-child(2) { display: grid; gap: 3px; }
.account-type-card small { color: var(--text-muted); font-size: 11px; }
.account-type-check { margin-left: auto; color: var(--primary-700); }
.credential-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.segmented-control { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 6px; }
.segmented-control button { display: inline-flex; min-height: 38px; align-items: center; justify-content: center; gap: 6px; border: 1px solid var(--border); border-radius: 7px; background: var(--surface); color: var(--text-muted); cursor: pointer; font-size: 12px; font-weight: 650; }
.segmented-control button.active { border-color: var(--primary-300); background: var(--primary-100); color: var(--primary-700); }
.setting-section { background: var(--surface); }
.setting-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.quota-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); }
.feature-list { display: grid; gap: 8px; }
.feature-row { display: flex; min-height: 54px; align-items: center; justify-content: space-between; gap: 16px; padding: 10px 12px; border: 1px solid var(--border); border-radius: 7px; background: var(--surface-subtle); cursor: pointer; }
.feature-row span { display: grid; gap: 3px; }
.feature-row strong { color: var(--text); font-size: 12px; }
.feature-row small { color: var(--text-muted); font-size: 10px; line-height: 1.4; }
.feature-row input, .capability-list input, .checkbox-line input { width: 16px; height: 16px; accent-color: var(--primary-600); }
.capability-list { display: flex; flex-wrap: wrap; gap: 12px; padding-top: 4px; }
.checkbox-line { display: flex; min-width: 0; align-items: flex-start; gap: 8px; color: var(--text-secondary); font-size: 12px; cursor: pointer; }
.checkbox-line > span { display: grid; gap: 3px; }
.checkbox-line small { color: var(--text-muted); font-size: 10px; }
.tag-list { display: flex; min-height: 34px; flex-wrap: wrap; align-items: center; gap: 6px; }
.model-tag { display: inline-flex; align-items: center; gap: 5px; min-height: 26px; padding: 0 8px; border-radius: 5px; background: var(--surface-hover); color: var(--text-secondary); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 11px; }
.model-tag button { display: inline-flex; border: 0; background: transparent; color: var(--text-muted); cursor: pointer; }
.inline-fields { display: flex; align-items: center; gap: 8px; }
.inline-fields > input { min-width: 0; flex: 1; }
.mapping-list, .rule-list { display: grid; gap: 8px; }
.mapping-row, .rule-row { display: grid; grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr) auto; align-items: center; gap: 8px; }
.rule-row { grid-template-columns: 120px minmax(0, 1fr) 150px auto; }
.account-modal-error { margin: 0 24px 12px; }
.form-span-2 { grid-column: 1 / -1; }
@media (max-width: 760px) {
  .account-list-filters, .account-toolbar-actions { width: 100%; }
  .account-list-filters select { min-width: 0; flex: 1 1 130px; }
  .account-toolbar-actions { justify-content: flex-start; }
  .toolbar-menu-button span { display: none; }
  .toolbar-menu-button { width: 40px; min-width: 40px; justify-content: center; padding-inline: 0; }
  .toolbar-menu-button svg:last-child { display: none; }
  .account-menu { position: fixed; z-index: 80; top: auto; right: 12px; left: 12px; width: auto; }
  .account-refresh-menu, .account-column-menu { bottom: 76px; }
  .account-bulk-bar { align-items: stretch; }
  .account-bulk-actions { display: grid; width: 100%; grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .account-bulk-actions .button { width: 100%; }
  .accounts-data-table { font-size: 13px; }
  .row-action-menu { position: absolute; top: 50%; right: 42px; left: auto; width: 180px; transform: translateY(-50%); }
  .account-settings-modal { max-height: calc(100vh - 12px); }
  .account-settings-body { padding: 14px 12px 20px; }
  .account-section { padding: 14px; }
  .credential-grid, .setting-grid, .quota-grid { grid-template-columns: 1fr; }
  .provider-platform-tabs { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .provider-platform-tab:last-child { grid-column: 1 / -1; }
  .inline-fields { align-items: stretch; flex-direction: column; }
  .inline-fields .button { width: 100%; }
  .mapping-row, .rule-row { grid-template-columns: 1fr; }
  .mapping-row > span { display: none; }
  .mapping-row .icon-button, .rule-row .icon-button { justify-self: end; }
  .form-span-2 { grid-column: auto; }
}
</style>
