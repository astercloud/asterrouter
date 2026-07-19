<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Activity, Bot, Check, Cloud, Edit3, KeyRound, Plus, RefreshCw, Save, Search, Server, Sparkles, X, Zap } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { checkProvider, createProvider, getProviderHealthChecks, getProviders, updateProvider } from '@/api/control'
import type { ProviderConnection, ProviderHealthCheck, ProviderRequest } from '@/types'

const { t } = useI18n()
const router = useRouter()
const loading = ref(false)
const saving = ref(false)
const checkingID = ref('')
const error = ref('')
const message = ref('')
const providers = ref<ProviderConnection[]>([])
const healthChecks = ref<Record<string, ProviderHealthCheck>>({})
const query = ref('')
const statusFilter = ref('')
const typeFilter = ref('')
const modalOpen = ref(false)
const editing = ref<ProviderConnection | null>(null)

type ProviderPlatform = 'anthropic' | 'openai' | 'gemini' | 'antigravity' | 'grok'

const PLATFORM_CONFIG = {
  anthropic: {
    label: 'Anthropic',
    icon: Sparkles,
    type: 'anthropic_compatible',
    baseURL: 'https://api.anthropic.com/v1'
  },
  openai: {
    label: 'OpenAI',
    icon: Zap,
    type: 'openai_compatible',
    baseURL: 'https://api.openai.com/v1'
  },
  gemini: {
    label: 'Gemini',
    icon: Sparkles,
    type: 'gemini_compatible',
    baseURL: 'https://generativelanguage.googleapis.com/v1beta'
  },
  antigravity: {
    label: 'Antigravity',
    icon: Cloud,
    type: 'openai_compatible',
    baseURL: 'https://cloudcode-pa.googleapis.com'
  },
  grok: {
    label: 'Grok',
    icon: Bot,
    type: 'openai_compatible',
    baseURL: 'https://api.x.ai/v1'
  }
} as const

const platformEntries = Object.entries(PLATFORM_CONFIG) as Array<
  [ProviderPlatform, (typeof PLATFORM_CONFIG)[ProviderPlatform]]
>
const platform = ref<ProviderPlatform>('openai')
const currentPlatform = computed(() => PLATFORM_CONFIG[platform.value])

const providerTypes = [
  { value: 'openai_compatible', label: 'OpenAI Compatible', baseURL: 'https://api.openai.com/v1' },
  { value: 'anthropic_compatible', label: 'Anthropic Compatible', baseURL: 'https://api.anthropic.com/v1' },
  { value: 'gemini_compatible', label: 'Gemini Compatible', baseURL: 'https://generativelanguage.googleapis.com/v1beta' },
  { value: 'aws_bedrock', label: 'AWS Bedrock', baseURL: 'https://bedrock-runtime.us-east-1.amazonaws.com' },
  { value: 'gcp_vertex', label: 'GCP Vertex AI', baseURL: 'https://aiplatform.googleapis.com/v1' },
  { value: 'azure_openai', label: 'Azure OpenAI', baseURL: 'https://example.openai.azure.com' }
] as const

const form = reactive<ProviderRequest>({ name: '', type: 'openai_compatible', base_url: '', status: 'active', priority: 100 })
const typeByID = new Map(providerTypes.map((item) => [item.value, item]))
const distinctTypes = computed(() => new Set(providers.value.map((item) => item.type)).size)
const healthyCount = computed(() => Object.values(healthChecks.value).filter((item) => item.status === 'ok').length)

const filteredProviders = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return providers.value.filter((provider) => {
    if (statusFilter.value && provider.status !== statusFilter.value) return false
    if (typeFilter.value && provider.type !== typeFilter.value) return false
    return !keyword || [provider.name, provider.type, provider.base_url].some((value) => value.toLowerCase().includes(keyword))
  })
})

function resetForm() {
  Object.assign(form, { name: '', type: 'openai_compatible', base_url: providerTypes[0].baseURL, status: 'active', priority: 100 })
  platform.value = 'openai'
}

function openCreate() {
  editing.value = null
  resetForm()
  modalOpen.value = true
}

function openEdit(provider: ProviderConnection) {
  editing.value = provider
  platform.value = inferPlatform(provider)
  Object.assign(form, { name: provider.name, type: provider.type, base_url: provider.base_url, status: provider.status, priority: provider.priority })
  modalOpen.value = true
}

function inferPlatform(provider: ProviderConnection): ProviderPlatform {
  const baseURL = provider.base_url.toLowerCase()
  if (baseURL.includes('api.x.ai') || baseURL.includes('grok')) return 'grok'
  if (baseURL.includes('cloudcode-pa') || baseURL.includes('antigravity')) return 'antigravity'
  if (provider.type === 'anthropic_compatible' || baseURL.includes('anthropic')) return 'anthropic'
  if (provider.type === 'gemini_compatible' || baseURL.includes('generativelanguage')) return 'gemini'
  return 'openai'
}

function selectPlatform(nextPlatform: ProviderPlatform) {
  platform.value = nextPlatform
  const config = PLATFORM_CONFIG[nextPlatform]
  form.type = config.type
  form.base_url = config.baseURL
}

function updateEnabled(event: Event) {
  form.status = (event.target as HTMLInputElement).checked ? 'active' : 'disabled'
}

function openProviderAccounts() {
  closeModal()
  void router.push('/admin/provider-accounts')
}

function closeModal() {
  modalOpen.value = false
  editing.value = null
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [providerData, healthData] = await Promise.all([getProviders(), getProviderHealthChecks()])
    providers.value = providerData
    healthChecks.value = Object.fromEntries(healthData.map((item) => [item.provider_id, item]))
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  error.value = ''
  message.value = ''
  try {
    if (editing.value) await updateProvider(editing.value.id, { ...form })
    else await createProvider({ ...form })
    message.value = editing.value ? t('providers.updated') : t('providers.created')
    closeModal()
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

async function runCheck(provider: ProviderConnection) {
  checkingID.value = provider.id
  error.value = ''
  try {
    const result = await checkProvider(provider.id)
    healthChecks.value = { ...healthChecks.value, [provider.id]: result }
    message.value = result.message
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    checkingID.value = ''
  }
}

function statusClass(status: string) {
  if (status === 'active' || status === 'ok') return 'status-success'
  if (status === 'disabled' || status === 'error') return 'status-danger'
  return 'status-warning'
}

function formatHealth(check?: ProviderHealthCheck) {
  if (!check) return t('providers.notChecked')
  return `${check.status} · ${check.latency_ms}ms · ${new Date(check.checked_at).toLocaleString()}`
}

onMounted(load)
</script>

<template>
  <main class="content crud-page provider-workbench">
    <section class="page-header">
      <div><h1>{{ t('admin.providers') }}</h1><p>{{ t('providers.subtitle') }}</p></div>
      <div class="page-header-actions">
        <button class="button secondary" type="button" @click="openProviderAccounts"><KeyRound :size="17" />{{ t('providers.configureApiKey') }}</button>
        <button class="button" type="button" @click="openCreate"><Plus :size="17" />{{ t('providers.newProvider') }}</button>
      </div>
    </section>

    <div class="crud-summary provider-summary">
      <span><Server :size="18" /><strong>{{ providers.length }}</strong>{{ t('providers.total') }}</span>
      <span><Cloud :size="18" /><strong>{{ distinctTypes }}</strong>{{ t('providers.type') }}</span>
      <span><Activity :size="18" /><strong>{{ healthyCount }}</strong>{{ t('providers.health') }}</span>
      <span><strong>{{ providers.filter((item) => item.status === 'disabled').length }}</strong>{{ t('providers.disabled') }}</span>
    </div>

    <section class="table-toolbar">
      <label class="search-box"><Search :size="17" /><input v-model="query" :placeholder="t('providers.searchPlaceholder')" /></label>
      <select v-model="typeFilter" :aria-label="t('providers.type')">
        <option value="">{{ t('common.all') }}</option>
        <option v-for="item in providerTypes" :key="item.value" :value="item.value">{{ item.label }}</option>
      </select>
      <select v-model="statusFilter" :aria-label="t('providers.status')">
        <option value="">{{ t('providers.allStatuses') }}</option><option value="active">active</option><option value="disabled">disabled</option>
      </select>
      <button class="icon-button" type="button" :disabled="loading" :aria-label="t('common.refresh')" :title="t('common.refresh')" @click="load"><RefreshCw :size="17" /></button>
    </section>

    <div v-if="message" class="notice success">{{ message }}</div>
    <div v-if="error" class="notice">{{ error }}</div>

    <section class="panel table-panel">
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead><tr><th>{{ t('providers.name') }}</th><th>{{ t('providers.type') }}</th><th>{{ t('providers.status') }}</th><th>{{ t('providers.priority') }}</th><th>{{ t('providers.health') }}</th><th>{{ t('common.actions') }}</th></tr></thead>
          <tbody>
            <tr v-for="provider in filteredProviders" :key="provider.id">
              <td><strong>{{ provider.name }}</strong><span class="provider-endpoint">{{ provider.base_url }}</span></td>
              <td>{{ typeByID.get(provider.type as never)?.label || provider.type }}</td>
              <td><span class="pill" :class="statusClass(provider.status)">{{ provider.status }}</span></td>
              <td>{{ provider.priority }}</td>
              <td><span class="pill" :class="statusClass(healthChecks[provider.id]?.status || '')">{{ formatHealth(healthChecks[provider.id]) }}</span><span v-if="healthChecks[provider.id]">{{ healthChecks[provider.id].message }}</span></td>
              <td><div class="row-actions">
                <button class="icon-button" type="button" :disabled="checkingID === provider.id" :aria-label="t('providers.check')" :title="t('providers.check')" @click="runCheck(provider)"><Activity :size="16" /></button>
                <button class="icon-button" type="button" :aria-label="t('common.edit')" :title="t('common.edit')" @click="openEdit(provider)"><Edit3 :size="16" /></button>
              </div></td>
            </tr>
            <tr v-if="!filteredProviders.length"><td colspan="6" class="empty-cell">{{ loading ? t('common.loading') : t('providers.empty') }}</td></tr>
          </tbody>
        </table>
      </div>
    </section>

    <div v-if="modalOpen" class="modal-backdrop" @click.self="closeModal">
      <section
        class="modal-card modal-card-wide provider-api-modal"
        :data-platform="platform"
        role="dialog"
        aria-modal="true"
        :aria-label="editing ? t('providers.editAccount') : t('providers.addAccount')"
      >
        <header class="modal-header">
          <div>
            <h2>{{ editing ? t('providers.editAccount') : t('providers.addAccount') }}</h2>
            <p>{{ t('providers.accountModalSubtitle') }}</p>
          </div>
          <button class="icon-button" type="button" :aria-label="t('common.close')" @click="closeModal"><X :size="19" /></button>
        </header>
        <form class="provider-modal-form" @submit.prevent="save">
          <div class="modal-body provider-modal-body">
            <div class="field">
              <label for="provider-account-name">{{ t('providers.accountName') }}</label>
              <input id="provider-account-name" v-model="form.name" required :placeholder="t('providers.accountNamePlaceholder')" />
            </div>

            <div class="field provider-platform-field">
              <label>{{ t('providers.platform') }}</label>
              <div class="provider-platform-tabs" role="tablist" :aria-label="t('providers.platform')">
                <button
                  v-for="[platformID, config] in platformEntries"
                  :key="platformID"
                  class="provider-platform-tab"
                  :class="{ active: platform === platformID }"
                  type="button"
                  role="tab"
                  :aria-selected="platform === platformID"
                  @click="selectPlatform(platformID)"
                >
                  <component :is="config.icon" :size="17" />
                  {{ config.label }}
                </button>
              </div>
            </div>

            <div class="field">
              <label>{{ t('providers.accountType') }}</label>
              <div class="provider-account-type-card" aria-current="true">
                <span class="provider-account-type-icon"><KeyRound :size="18" /></span>
                <span>
                  <strong>{{ t('providers.apiKeyTypeLabel') }}</strong>
                  <small>{{ t('providers.apiOnlyDescription', { platform: currentPlatform.label }) }}</small>
                </span>
                <Check class="provider-account-type-check" :size="18" />
              </div>
            </div>

            <section class="provider-form-section">
              <div class="field">
                <label for="provider-base-url">{{ t('providers.baseUrl') }}</label>
                <input id="provider-base-url" v-model="form.base_url" required class="provider-mono-input" :placeholder="currentPlatform.baseURL" />
                <span class="hint">{{ t('providers.baseUrlHint', { platform: currentPlatform.label }) }}</span>
              </div>
              <div class="provider-credential-link">
                <div>
                  <strong>{{ t('providers.apiKeyTypeLabel') }}</strong>
                  <span>{{ t('providers.apiKeyAccountHint') }}</span>
                </div>
                <button class="button secondary" type="button" @click="openProviderAccounts">
                  <KeyRound :size="16" />
                  {{ t('providers.configureApiKey') }}
                </button>
              </div>
            </section>

            <section class="provider-form-section provider-common-section">
              <div class="provider-section-heading">
                <div>
                  <h3>{{ t('providers.commonConfiguration') }}</h3>
                  <p>{{ t('providers.commonConfigurationHint') }}</p>
                </div>
              </div>
              <div class="provider-config-grid">
                <div class="provider-toggle-row">
                  <div>
                    <strong>{{ t('providers.enabledStatus') }}</strong>
                    <small>{{ t('providers.enabledStatusHint') }}</small>
                  </div>
                  <label class="switch">
                    <input type="checkbox" :checked="form.status === 'active'" @change="updateEnabled" />
                    <span />
                  </label>
                </div>
                <div class="field">
                  <label for="provider-priority">{{ t('providers.priority') }}</label>
                  <input id="provider-priority" v-model.number="form.priority" type="number" min="1" required />
                  <span class="hint">{{ t('providers.priorityHint') }}</span>
                </div>
              </div>
            </section>
          </div>
          <footer class="modal-footer">
            <button class="button secondary" type="button" @click="closeModal">{{ t('common.cancel') }}</button>
            <button class="button" type="submit" :disabled="saving">
              <Save :size="17" />
              {{ saving ? t('common.saving') : editing ? t('providers.updateAccount') : t('providers.createAccount') }}
            </button>
          </footer>
        </form>
      </section>
    </div>
  </main>
</template>

<style scoped>
.provider-summary span { display: flex; align-items: center; gap: 8px; }
.provider-endpoint { display: block; max-width: 440px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.provider-credential-link { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 14px; border: 1px solid var(--border); border-radius: 8px; background: var(--surface-subtle); }
.provider-credential-link > div { display: grid; gap: 4px; min-width: 0; }
.provider-credential-link strong { color: var(--text); font-size: 12px; }
.provider-credential-link span { color: var(--text-muted); font-size: 11px; line-height: 1.45; }
.provider-credential-link .button { flex: 0 0 auto; }
.form-span-2 { grid-column: 1 / -1; }
@media (max-width: 760px) { .provider-endpoint { max-width: 240px; } .provider-credential-link { align-items: stretch; flex-direction: column; } .provider-credential-link .button { width: 100%; } .form-span-2 { grid-column: auto; } }
</style>
