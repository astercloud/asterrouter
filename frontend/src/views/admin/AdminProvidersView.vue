<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Activity, Cloud, Edit3, Plus, RefreshCw, Save, Search, Server, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { checkProvider, createProvider, getProviderHealthChecks, getProviders, updateProvider } from '@/api/control'
import type { ProviderConnection, ProviderHealthCheck, ProviderRequest } from '@/types'

const { t } = useI18n()
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
}

function openCreate() {
  editing.value = null
  resetForm()
  modalOpen.value = true
}

function openEdit(provider: ProviderConnection) {
  editing.value = provider
  Object.assign(form, { name: provider.name, type: provider.type, base_url: provider.base_url, status: provider.status, priority: provider.priority })
  modalOpen.value = true
}

function selectProviderType(type: string) {
  form.type = type
  const preset = typeByID.get(type as (typeof providerTypes)[number]['value'])
  if (!editing.value && preset) form.base_url = preset.baseURL
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
      <button class="button" type="button" @click="openCreate"><Plus :size="17" />{{ t('providers.newProvider') }}</button>
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
      <section class="modal-card" role="dialog" aria-modal="true" :aria-label="editing ? t('common.edit') : t('providers.newProvider')">
        <header class="modal-header"><div><h2>{{ editing ? t('common.edit') : t('providers.newProvider') }}</h2><p>{{ t('providers.subtitle') }}</p></div><button class="icon-button" type="button" :aria-label="t('common.close')" @click="closeModal"><X :size="19" /></button></header>
        <form @submit.prevent="save">
          <div class="modal-body form-grid">
            <div class="field"><label for="provider-name">{{ t('providers.name') }}</label><input id="provider-name" v-model="form.name" required /></div>
            <div class="field"><label for="provider-type">{{ t('providers.type') }}</label><select id="provider-type" :value="form.type" required @change="selectProviderType(($event.target as HTMLSelectElement).value)"><option v-for="item in providerTypes" :key="item.value" :value="item.value">{{ item.label }}</option></select></div>
            <div class="field form-span-2"><label for="provider-url">{{ t('providers.baseUrl') }}</label><input id="provider-url" v-model="form.base_url" class="provider-mono-input" type="url" required /></div>
            <div class="field"><label for="provider-status">{{ t('providers.status') }}</label><select id="provider-status" v-model="form.status"><option value="active">active</option><option value="disabled">disabled</option></select></div>
            <div class="field"><label for="provider-priority">{{ t('providers.priority') }}</label><input id="provider-priority" v-model.number="form.priority" type="number" min="1" required /></div>
          </div>
          <footer class="modal-footer"><button class="button secondary" type="button" @click="closeModal">{{ t('common.cancel') }}</button><button class="button" type="submit" :disabled="saving"><Save :size="17" />{{ saving ? t('common.saving') : t('common.save') }}</button></footer>
        </form>
      </section>
    </div>
  </main>
</template>

<style scoped>
.provider-summary span { display: flex; align-items: center; gap: 8px; }
.provider-endpoint { display: block; max-width: 440px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.form-span-2 { grid-column: 1 / -1; }
@media (max-width: 760px) { .provider-endpoint { max-width: 240px; } .form-span-2 { grid-column: auto; } }
</style>
