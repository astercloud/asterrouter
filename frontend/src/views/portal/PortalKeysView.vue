<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Check, Copy, KeyRound, Plus, RefreshCw, RotateCw, Search, ShieldCheck, Trash2 } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { createPortalAPIKey, disablePortalAPIKey, getPortalWorkspace, rotatePortalAPIKey } from '@/api/control'
import { useAppStore } from '@/stores/app'
import type { APIKeyCreateRequest, APIKeyRecord, PortalWorkspace } from '@/types'
import { apiKeyLifecycleClass, apiKeyLifecycleLabelKey, apiKeyLifecycleStatus, canDisableAPIKey, canRotateAPIKey } from '@/utils/apiKeys'

const { t } = useI18n()
const app = useAppStore()
const workspace = ref<PortalWorkspace | null>(null)
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const search = ref('')
const createOpen = ref(false)
const copied = ref('')
const revealedSecret = ref('')
const revealedKeyID = ref('')
const selectedKeyID = ref('')

const form = reactive<APIKeyCreateRequest>({
  name: '',
  policy_id: '',
  routing_policy_id: '',
  model_allowlist: [],
  qps_limit: 0,
  monthly_token_limit: 0,
  expires_at: ''
})

const apiKeys = computed(() => workspace.value?.api_keys || [])
const activeKeys = computed(() => apiKeys.value.filter((key) => apiKeyLifecycleStatus(key) === 'active'))
const canManageKeys = computed(() => Boolean(workspace.value?.can_manage_keys))
const models = computed(() => workspace.value?.models || [])
const filteredKeys = computed(() => {
  const query = search.value.trim().toLowerCase()
  if (!query) return apiKeys.value
  return apiKeys.value.filter((key) => [key.name, key.prefix, key.fingerprint, key.policy_id, ...key.model_allowlist].join(' ').toLowerCase().includes(query))
})
const selectedKey = computed(() => activeKeys.value.find((key) => key.id === selectedKeyID.value) || activeKeys.value[0] || apiKeys.value[0] || null)
const baseURL = computed(() => {
  const settings = app.publicSettings
  const base = (settings?.public_base_url || window.location.origin).replace(/\/$/, '')
  const path = workspace.value?.gateway_path || settings?.gateway_base_path || '/v1'
  return /^https?:\/\//i.test(path) ? path.replace(/\/$/, '') : `${base}/${path.replace(/^\//, '')}`.replace(/\/$/, '')
})
const installCommand = computed(() => 'npm install -g opencode-ai')
const displayedGlobalKey = computed(() => {
  if (!selectedKey.value) return ''
  if (revealedKeyID.value === selectedKey.value.id && revealedSecret.value) return revealedSecret.value
  return `${selectedKey.value.prefix || 'ar_'}...${selectedKey.value.fingerprint}`
})

function clearFeedback() {
  error.value = ''
  notice.value = ''
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    workspace.value = await getPortalWorkspace()
    const preferred = apiKeys.value.find((key) => key.id === selectedKeyID.value)
    selectedKeyID.value = preferred?.id || activeKeys.value[0]?.id || apiKeys.value[0]?.id || ''
    if (!form.model_allowlist.length && models.value[0]) form.model_allowlist = [models.value[0]]
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

function toggleModel(model: string) {
  form.model_allowlist = form.model_allowlist.includes(model)
    ? form.model_allowlist.filter((item) => item !== model)
    : [...form.model_allowlist, model]
}

function openCreate() {
  clearFeedback()
  createOpen.value = !createOpen.value
  if (createOpen.value && !form.name) form.name = `${app.siteName} ${t('portalKeys.defaultKeyName')}`
}

function resetForm() {
  form.name = `${app.siteName} ${t('portalKeys.defaultKeyName')}`
  form.policy_id = ''
  form.qps_limit = 0
  form.monthly_token_limit = 0
  form.expires_at = ''
  form.model_allowlist = models.value[0] ? [models.value[0]] : []
}

async function createKey() {
  if (!canManageKeys.value || !form.name.trim() || !form.model_allowlist.length) return
  saving.value = true
  clearFeedback()
  try {
    const result = await createPortalAPIKey({ ...form, name: form.name.trim() })
    revealedSecret.value = result.key
    revealedKeyID.value = result.record.id
    selectedKeyID.value = result.record.id
    notice.value = t('portalKeys.createdOnce')
    createOpen.value = false
    resetForm()
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

async function rotateKey(key: APIKeyRecord) {
  if (!canManageKeys.value || !window.confirm(t('portalKeys.rotateConfirm', { name: key.name }))) return
  saving.value = true
  clearFeedback()
  try {
    const result = await rotatePortalAPIKey(key.id)
    revealedSecret.value = result.key
    revealedKeyID.value = result.record.id
    selectedKeyID.value = result.record.id
    notice.value = t('portalKeys.rotatedOnce')
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

async function disableKey(key: APIKeyRecord) {
  if (!canManageKeys.value || !window.confirm(t('portalKeys.disableConfirm', { name: key.name }))) return
  saving.value = true
  clearFeedback()
  try {
    await disablePortalAPIKey(key.id)
    notice.value = t('portalKeys.disabled')
    if (revealedKeyID.value === key.id) {
      revealedKeyID.value = ''
      revealedSecret.value = ''
    }
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

async function copyText(value: string, field: string) {
  if (!value) return
  try {
    await navigator.clipboard.writeText(value)
  } catch {
    const textarea = document.createElement('textarea')
    textarea.value = value
    textarea.style.position = 'fixed'
    textarea.style.opacity = '0'
    document.body.appendChild(textarea)
    textarea.select()
    document.execCommand('copy')
    textarea.remove()
  }
  copied.value = field
  window.setTimeout(() => { if (copied.value === field) copied.value = '' }, 1600)
}

function formatDate(value?: string) {
  return value ? new Date(value).toLocaleString() : t('portalKeys.neverUsed')
}

onMounted(() => {
  resetForm()
  load()
})
</script>

<template>
  <main class="content portal-keys-page">
    <section class="page-header portal-keys-heading">
      <div>
        <h1>{{ t('portalKeys.title') }}</h1>
        <p>{{ t('portalKeys.subtitle') }}</p>
      </div>
      <div class="row-actions">
        <button class="button secondary" type="button" :disabled="loading" @click="load"><RefreshCw :size="16" />{{ t('common.refresh') }}</button>
        <button class="button primary" type="button" :disabled="!canManageKeys" @click="openCreate"><Plus :size="16" />{{ t('portalKeys.create') }}</button>
      </div>
    </section>

    <div v-if="error" class="notice">{{ error }}</div>
    <div v-if="notice" class="notice success">{{ notice }}</div>

    <template v-if="!loading">
      <section class="portal-access-grid" data-portal-section="access">
        <div class="portal-key-banner" :class="{ 'is-ready': models.length }">
          <div class="portal-key-banner-icon"><KeyRound :size="18" /></div>
          <div>
            <strong>{{ t('portalKeys.modelBanner', { count: models.length }) }}</strong>
            <span>{{ models.length ? t('portalKeys.modelBannerHelp') : t('portalKeys.noModelsHelp') }}</span>
          </div>
        </div>

        <div class="portal-endpoint-stack">
          <div class="portal-endpoint-row">
            <strong>Base URL</strong>
            <code>{{ baseURL }}</code>
            <button class="icon-button" type="button" :aria-label="`${t('common.copy')} Base URL`" :title="t('common.copy')" @click="copyText(baseURL, 'base')"><Check v-if="copied === 'base'" :size="16" /><Copy v-else :size="16" /></button>
          </div>
          <div class="portal-endpoint-row">
            <strong>{{ t('portalKeys.installCommand') }}</strong>
            <code>{{ installCommand }}</code>
            <button class="icon-button" type="button" :aria-label="`${t('common.copy')} ${t('portalKeys.installCommand')}`" :title="t('common.copy')" @click="copyText(installCommand, 'install')"><Check v-if="copied === 'install'" :size="16" /><Copy v-else :size="16" /></button>
          </div>
        </div>
      </section>

      <section v-if="createOpen" class="panel portal-create-panel">
        <div class="panel-header split-header">
          <div><h2>{{ t('portalKeys.createTitle') }}</h2><p>{{ t('portalKeys.createHelp') }}</p></div>
          <KeyRound :size="18" />
        </div>
        <form class="panel-body" @submit.prevent="createKey">
          <fieldset class="form-fieldset" :disabled="saving || !canManageKeys">
            <label class="field"><span>{{ t('apiKeys.name') }}</span><input v-model="form.name" required /></label>
            <div class="field"><span>{{ t('apiKeys.modelAllowlist') }}</span><div class="chip-list">
              <button v-for="model in models" :key="model" class="pill" :class="{ 'status-success': form.model_allowlist.includes(model) }" type="button" @click="toggleModel(model)">{{ model }}</button>
              <span v-if="!models.length" class="hint">{{ t('portalKeys.noModels') }}</span>
            </div></div>
            <div class="form-grid"><label class="field"><span>{{ t('apiKeys.qps') }}</span><input v-model.number="form.qps_limit" type="number" min="0" /></label><label class="field"><span>{{ t('apiKeys.monthlyTokens') }}</span><input v-model.number="form.monthly_token_limit" type="number" min="0" /></label></div>
            <div class="row-actions"><button class="button primary" type="submit" :disabled="!form.model_allowlist.length || saving"><KeyRound :size="16" />{{ t('portalKeys.create') }}</button><button class="button secondary" type="button" @click="createOpen = false">{{ t('common.cancel') }}</button></div>
          </fieldset>
        </form>
      </section>

      <section class="panel portal-global-key" data-portal-section="current-key">
        <div class="panel-header split-header">
          <div><h2>{{ t('portalKeys.globalTitle') }}</h2><p>{{ t('portalKeys.globalHelp') }}</p></div>
          <button v-if="selectedKey" class="button secondary compact-button" type="button" :disabled="saving || !canManageKeys || !canRotateAPIKey(selectedKey)" @click="rotateKey(selectedKey)"><RotateCw :size="15" />{{ t('portalKeys.rotate') }}</button>
        </div>
        <div class="panel-body">
          <div v-if="selectedKey" class="global-key-line"><span>Key</span><code>{{ displayedGlobalKey }}</code><button class="button secondary compact-button" type="button" :disabled="!revealedSecret || revealedKeyID !== selectedKey.id" @click="copyText(revealedSecret, 'key')"><Check v-if="copied === 'key'" :size="15" /><Copy v-else :size="15" />{{ t('common.copy') }}</button></div>
          <div v-else class="empty-state"><KeyRound :size="22" /><span>{{ t('portalKeys.emptyGlobal') }}</span><button class="button primary compact-button" type="button" :disabled="!canManageKeys" @click="openCreate"><Plus :size="15" />{{ t('portalKeys.create') }}</button></div>
          <p v-if="selectedKey && revealedKeyID === selectedKey.id" class="hint">{{ t('portalKeys.secretOnce') }}</p>
        </div>
      </section>

      <section class="panel portal-key-list" data-portal-section="key-list">
        <div class="panel-header portal-list-heading">
          <h2>{{ t('portalKeys.listTitle') }}</h2>
          <span class="hint">{{ t('portalKeys.count', { count: filteredKeys.length }) }}</span>
        </div>
        <div class="portal-list-toolbar">
          <div class="portal-search"><Search :size="16" /><input v-model="search" :aria-label="t('portalKeys.searchPlaceholder')" :placeholder="t('portalKeys.searchPlaceholder')" /></div>
        </div>
        <div class="table-scroll">
          <table class="data-table portal-keys-table">
            <thead><tr><th>{{ t('portalKeys.nameColumn') }}</th><th>{{ t('portalKeys.keyColumn') }}</th><th>{{ t('portalKeys.modelsColumn') }}</th><th>{{ t('portalKeys.policyColumn') }}</th><th>{{ t('portalKeys.usageColumn') }}</th><th>{{ t('portalKeys.lastUsedColumn') }}</th><th>{{ t('portalKeys.statusColumn') }}</th><th>{{ t('common.actions') }}</th></tr></thead>
            <tbody>
              <tr v-for="key in filteredKeys" :key="key.id">
                <td><strong>{{ key.name }}</strong><span>{{ key.key_type }}</span></td>
                <td><code>{{ key.prefix }}...{{ key.fingerprint }}</code></td>
                <td><span>{{ key.model_allowlist.join(', ') || '-' }}</span></td>
                <td><span>{{ key.policy_id || t('portalKeys.defaultPolicy') }}</span></td>
                <td><span>{{ key.qps_limit || t('portalKeys.unlimited') }} QPS</span><span>{{ key.monthly_token_limit || t('portalKeys.unlimited') }} {{ t('usage.tokens') }}</span></td>
                <td><span>{{ formatDate(key.last_used_at) }}</span></td>
                <td><span class="pill" :class="apiKeyLifecycleClass(key)">{{ t(apiKeyLifecycleLabelKey(key)) }}</span></td>
                <td><div class="row-actions"><button class="icon-button" type="button" :title="t('portalKeys.rotate')" :disabled="saving || !canManageKeys || !canRotateAPIKey(key)" @click="rotateKey(key)"><RotateCw :size="15" /></button><button class="icon-button danger-icon" type="button" :title="t('portalKeys.disable')" :disabled="saving || !canManageKeys || !canDisableAPIKey(key)" @click="disableKey(key)"><Trash2 :size="15" /></button></div></td>
              </tr>
              <tr v-if="!filteredKeys.length"><td colspan="8" class="empty-cell">{{ search ? t('portalKeys.noSearchResults') : t('portalKeys.emptyList') }}</td></tr>
            </tbody>
          </table>
        </div>
      </section>

      <section class="panel portal-security-panel" data-portal-section="security">
        <div class="panel-header"><div><h2>{{ t('portalKeys.securityTitle') }}</h2><p>{{ t('portalKeys.securityHelp') }}</p></div><ShieldCheck :size="18" /></div>
        <div class="portal-security-grid"><div><strong>{{ t('portalKeys.securityOneTitle') }}</strong><span>{{ t('portalKeys.securityOneHelp') }}</span></div><div><strong>{{ t('portalKeys.securityTwoTitle') }}</strong><span>{{ t('portalKeys.securityTwoHelp') }}</span></div><div><strong>{{ t('portalKeys.securityThreeTitle') }}</strong><span>{{ t('portalKeys.securityThreeHelp') }}</span></div></div>
      </section>
    </template>
  </main>
</template>

<style scoped>
.portal-keys-page { display: grid; width: min(1360px, 100%); margin-inline: auto; align-content: start; gap: 20px; }
.portal-keys-page > * { min-width: 0; }
.portal-keys-heading { margin-bottom: 0; align-items: flex-start; }
.portal-access-grid { display: grid; grid-template-columns: minmax(300px, .8fr) minmax(480px, 1.5fr); align-items: stretch; gap: 16px; }
.portal-key-banner { display: flex; min-height: 112px; align-items: flex-start; gap: 12px; padding: 17px; border: 1px solid color-mix(in srgb, var(--warning) 34%, var(--border)); border-radius: 8px; background: color-mix(in srgb, var(--warning-bg) 52%, var(--surface)); box-shadow: var(--shadow-sm); }
.portal-key-banner.is-ready { border-color: color-mix(in srgb, var(--primary-500) 28%, var(--border)); background: color-mix(in srgb, var(--primary-50) 66%, var(--surface)); }
.portal-key-banner-icon { display: grid; width: 34px; height: 34px; flex: 0 0 34px; place-items: center; border-radius: 6px; background: var(--warning-bg); color: var(--warning); }
.portal-key-banner.is-ready .portal-key-banner-icon { background: var(--primary-100); color: var(--primary-700); }
.portal-key-banner div:last-child { display: grid; gap: 2px; }
.portal-key-banner strong { color: var(--text); font-size: 13px; }
.portal-key-banner span { color: var(--text-muted); font-size: 12px; }
.portal-endpoint-stack { display: grid; overflow: hidden; border: 1px solid var(--border); border-radius: 8px; background: var(--surface); box-shadow: var(--shadow-sm); }
.portal-endpoint-row { display: grid; grid-template-columns: minmax(120px, auto) minmax(0, 1fr) 32px; align-items: center; gap: 12px; min-height: 55px; padding: 0 12px 0 16px; }
.portal-endpoint-row + .portal-endpoint-row { border-top: 1px solid var(--border); }
.portal-endpoint-row strong { color: var(--text-secondary); font-size: 11px; white-space: nowrap; }
.portal-endpoint-row code { min-width: 0; overflow: hidden; color: var(--text); font: 12px/1.4 ui-monospace, SFMono-Regular, Menlo, monospace; text-overflow: ellipsis; white-space: nowrap; }
.portal-endpoint-row .icon-button { width: 30px; height: 30px; color: var(--text-muted); }
.portal-create-panel { border-radius: 8px; }
.compact-button { min-height: 32px; padding: 0 10px; font-size: 11px; }
.portal-global-key .panel-body { gap: 8px; }
.global-key-line { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 10px; }
.global-key-line > span { color: var(--text-muted); font-size: 12px; }
.global-key-line code { min-width: 0; padding: 10px 12px; overflow: hidden; border: 1px solid var(--border); border-radius: 7px; background: var(--surface-subtle); color: var(--text); font: 12px ui-monospace, SFMono-Regular, Menlo, monospace; text-overflow: ellipsis; white-space: nowrap; }
.empty-state { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; color: var(--text-muted); font-size: 13px; }
.portal-list-heading { justify-content: space-between; }
.portal-list-toolbar { display: flex; align-items: center; min-height: 54px; padding: 9px 12px; border-bottom: 1px solid var(--border); background: var(--surface-subtle); }
.portal-search { display: flex; width: min(360px, 100%); align-items: center; gap: 8px; min-height: 36px; padding: 0 10px; border: 1px solid var(--border); border-radius: 8px; background: var(--surface); color: var(--text-muted); }
.portal-search input { width: 100%; min-width: 0; border: 0; outline: 0; background: transparent; color: var(--text); font-size: 12px; }
.portal-keys-table { min-width: 1080px; }
.portal-keys-table td code { display: block; max-width: 170px; overflow: hidden; color: var(--text-secondary); font: 11px ui-monospace, SFMono-Regular, Menlo, monospace; text-overflow: ellipsis; white-space: nowrap; }
.portal-security-panel .panel-header { justify-content: space-between; }
.portal-security-panel .panel-header > div { display: grid; gap: 2px; }
.portal-security-panel .panel-header p { margin: 0; color: var(--text-muted); font-size: 12px; }
.portal-security-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 0; padding: 18px 20px; }
.portal-security-grid > div { display: grid; gap: 5px; padding: 0 18px; border-left: 1px solid var(--border); }
.portal-security-grid > div:first-child { padding-left: 0; border-left: 0; }
.portal-security-grid strong { color: var(--text); font-size: 12px; }
.portal-security-grid span { color: var(--text-muted); font-size: 11px; line-height: 1.55; }
.danger-icon { color: var(--danger); }
@media (max-width: 1080px) {
  .portal-access-grid { grid-template-columns: 1fr; }
  .portal-key-banner { min-height: 0; }
}
@media (max-width: 760px) {
  .portal-keys-heading { display: grid; }
  .portal-keys-heading .row-actions { justify-content: flex-start; }
  .portal-keys-page { gap: 16px; }
  .portal-access-grid { gap: 12px; }
  .portal-endpoint-row { grid-template-columns: 1fr 32px; gap: 6px; padding: 9px 10px; }
  .portal-endpoint-row strong { grid-column: 1 / -1; }
  .global-key-line { grid-template-columns: 1fr auto; }
  .global-key-line > span { grid-column: 1 / -1; }
  .portal-list-toolbar { padding: 9px 10px; }
  .portal-search { width: 100%; }
  .portal-security-grid { grid-template-columns: 1fr; gap: 14px; }
  .portal-security-grid > div, .portal-security-grid > div:first-child { padding: 0; border-left: 0; }
}
</style>
