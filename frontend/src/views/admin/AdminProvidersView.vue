<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Activity, Edit3, Plus, RefreshCw, Save, Search, ServerCog, X } from '@lucide/vue'
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
const query = ref('')
const statusFilter = ref('')
const modalOpen = ref(false)
const editing = ref<ProviderConnection | null>(null)
const modelsText = ref('')
const healthChecks = ref<Record<string, ProviderHealthCheck>>({})

const form = reactive<ProviderRequest>({
  name: '',
  type: 'openai_compatible',
  base_url: '',
  status: 'active',
  models: [],
  priority: 100,
  api_key: ''
})

const filteredProviders = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return providers.value.filter((provider) => {
    if (statusFilter.value && provider.status !== statusFilter.value) return false
    if (!keyword) return true
    return [provider.name, provider.type, provider.base_url, provider.models.join(' ')].some((value) =>
      value.toLowerCase().includes(keyword)
    )
  })
})

const summary = computed(() => ({
  total: providers.value.length,
  active: providers.value.filter((item) => item.status === 'active').length,
  warning: providers.value.filter((item) => item.status === 'needs_secret').length,
  disabled: providers.value.filter((item) => item.status === 'disabled').length
}))

function splitLines(value: string): string[] {
  return value.split(/\n|,/).map((item) => item.trim()).filter(Boolean)
}

function resetForm() {
  Object.assign(form, {
    name: '',
    type: 'openai_compatible',
    base_url: '',
    status: 'active',
    models: [],
    priority: 100,
    api_key: ''
  })
  modelsText.value = 'gpt-4o-mini\ngpt-4.1-mini'
}

function openCreate() {
  editing.value = null
  resetForm()
  modalOpen.value = true
}

function openEdit(provider: ProviderConnection) {
  editing.value = provider
  Object.assign(form, {
    name: provider.name,
    type: provider.type,
    base_url: provider.base_url,
    status: provider.status,
    models: [...provider.models],
    priority: provider.priority,
    api_key: ''
  })
  modelsText.value = provider.models.join('\n')
  modalOpen.value = true
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
    const payload = { ...form, models: splitLines(modelsText.value) }
    if (editing.value) {
      await updateProvider(editing.value.id, payload)
      message.value = t('providers.updated')
    } else {
      await createProvider(payload)
      message.value = t('providers.created')
    }
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
  message.value = ''
  try {
    const result = await checkProvider(provider.id)
    healthChecks.value = { ...healthChecks.value, [provider.id]: result }
    message.value = result.message
    await load()
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

function formatHealth(check: ProviderHealthCheck): string {
  const time = new Date(check.checked_at).toLocaleString()
  return `${check.status} / ${check.latency_ms}ms / ${time}`
}

onMounted(load)
</script>

<template>
  <main class="content crud-page">
    <section class="page-header">
      <div>
        <h1>{{ t('admin.providers') }}</h1>
        <p>{{ t('providers.subtitle') }}</p>
      </div>
      <button class="button" type="button" @click="openCreate">
        <Plus :size="17" />
        {{ t('providers.newProvider') }}
      </button>
    </section>

    <div class="crud-summary">
      <span><strong>{{ summary.total }}</strong>{{ t('providers.total') }}</span>
      <span><strong>{{ summary.active }}</strong>{{ t('providers.active') }}</span>
      <span><strong>{{ summary.warning }}</strong>{{ t('providers.warning') }}</span>
      <span><strong>{{ summary.disabled }}</strong>{{ t('providers.disabled') }}</span>
    </div>

    <section class="table-toolbar">
      <label class="search-box">
        <Search :size="17" />
        <input v-model="query" :placeholder="t('providers.searchPlaceholder')" />
      </label>
      <select v-model="statusFilter">
        <option value="">{{ t('providers.allStatuses') }}</option>
        <option value="active">active</option>
        <option value="needs_secret">needs_secret</option>
        <option value="disabled">disabled</option>
      </select>
      <button class="button secondary" type="button" :disabled="loading" @click="load">
        <RefreshCw :size="17" />
        {{ t('common.refresh') }}
      </button>
    </section>

    <div v-if="message" class="notice success">{{ message }}</div>
    <div v-if="error" class="notice">{{ error }}</div>

    <section class="panel table-panel">
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead>
            <tr>
              <th>{{ t('providers.name') }}</th>
              <th>{{ t('providers.type') }}</th>
              <th>{{ t('providers.status') }}</th>
              <th>{{ t('providers.models') }}</th>
              <th>{{ t('providers.priority') }}</th>
              <th>{{ t('providers.health') }}</th>
              <th>{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="provider in filteredProviders" :key="provider.id">
              <td>
                <strong>{{ provider.name }}</strong>
                <span>{{ provider.base_url }}</span>
              </td>
              <td>{{ provider.type }}</td>
              <td><span class="pill" :class="statusClass(provider.status)">{{ provider.status }}</span></td>
              <td>
                <div class="chip-list">
                  <span v-for="model in provider.models.slice(0, 3)" :key="model" class="pill">{{ model }}</span>
                  <span v-if="provider.models.length > 3" class="pill">+{{ provider.models.length - 3 }}</span>
                </div>
              </td>
              <td>{{ provider.priority }}</td>
              <td>
                <template v-if="healthChecks[provider.id]">
                  <span class="pill" :class="statusClass(healthChecks[provider.id].status)">
                    {{ formatHealth(healthChecks[provider.id]) }}
                  </span>
                  <span>{{ healthChecks[provider.id].message }}</span>
                </template>
                <span v-else class="hint">{{ t('providers.notChecked') }}</span>
              </td>
              <td>
                <div class="row-actions">
                  <button class="button secondary" type="button" :disabled="checkingID === provider.id" @click="runCheck(provider)">
                    <Activity :size="15" />
                    {{ checkingID === provider.id ? t('common.loading') : t('providers.check') }}
                  </button>
                  <button class="button secondary" type="button" @click="openEdit(provider)">
                    <Edit3 :size="15" />
                    {{ t('common.edit') }}
                  </button>
                </div>
              </td>
            </tr>
            <tr v-if="!filteredProviders.length">
              <td colspan="7" class="empty-cell">
                {{ loading ? t('common.loading') : t('providers.empty') }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <div v-if="modalOpen" class="modal-backdrop" @click.self="closeModal">
      <section class="modal-card">
        <header class="modal-header">
          <div>
            <h2>{{ editing ? t('providers.editProvider') : t('providers.newProvider') }}</h2>
            <p>{{ t('providers.modalSubtitle') }}</p>
          </div>
          <button class="icon-button" type="button" @click="closeModal">
            <X :size="19" />
          </button>
        </header>
        <div class="modal-body form-grid">
          <div class="field">
            <label>{{ t('providers.name') }}</label>
            <input v-model="form.name" placeholder="OpenAI US East" />
          </div>
          <div class="field">
            <label>{{ t('providers.type') }}</label>
            <select v-model="form.type">
              <option value="openai_compatible">OpenAI-compatible</option>
              <option value="azure_openai">Azure OpenAI</option>
              <option value="anthropic">Anthropic Claude</option>
              <option value="gemini">Gemini</option>
              <option value="self_hosted">Self-hosted</option>
            </select>
          </div>
          <div class="field form-span-2">
            <label>{{ t('providers.baseUrl') }}</label>
            <input v-model="form.base_url" placeholder="https://api.openai.com/v1" />
          </div>
          <div class="field">
            <label>{{ t('providers.status') }}</label>
            <select v-model="form.status">
              <option value="active">active</option>
              <option value="needs_secret">needs_secret</option>
              <option value="disabled">disabled</option>
            </select>
          </div>
          <div class="field">
            <label>{{ t('providers.priority') }}</label>
            <input v-model.number="form.priority" type="number" min="1" />
          </div>
          <div class="field form-span-2">
            <label>{{ t('providers.models') }}</label>
            <textarea v-model="modelsText" rows="4" />
          </div>
          <div class="field form-span-2">
            <label>{{ t('providers.apiKey') }}</label>
            <input v-model="form.api_key" type="password" autocomplete="off" :placeholder="editing ? t('providers.keepSecret') : ''" />
            <span class="hint">{{ t('providers.secretHelp') }}</span>
          </div>
        </div>
        <footer class="modal-footer">
          <button class="button secondary" type="button" @click="closeModal">{{ t('common.cancel') }}</button>
          <button class="button" type="button" :disabled="saving" @click="save">
            <Save :size="17" />
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </footer>
      </section>
    </div>
  </main>
</template>
