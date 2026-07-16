<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { AlertTriangle, CheckCircle2, Plus, RefreshCw, Save, Search, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { discoverProviderAccountModels, getProviderAccountModelInventory, syncProviderAccountModels } from '@/api/control'
import type { ProviderAccount, ProviderAccountModel, ProviderAccountModelDiscovery } from '@/types'

const props = defineProps<{
  modelValue: string[]
  autoEnableNewModels: boolean
  accountId?: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string[]]
  'update:autoEnableNewModels': [value: boolean]
  synced: [account: ProviderAccount]
}>()

const { t } = useI18n()
const rows = ref<ProviderAccountModel[]>([])
const query = ref('')
const availability = ref('')
const customModel = ref('')
const loading = ref(false)
const discovering = ref(false)
const syncing = ref(false)
const error = ref('')
const notice = ref('')
const discovery = ref<ProviderAccountModelDiscovery | null>(null)

const selected = computed(() => new Set(props.modelValue))

const effectiveRows = computed(() => {
  const byID = new Map(rows.value.map((row) => [row.model_id, { ...row, enabled: selected.value.has(row.model_id) }]))
  for (const modelID of props.modelValue) {
    if (!byID.has(modelID)) {
      byID.set(modelID, {
        provider_account_id: props.accountId || '',
        model_id: modelID,
        source: 'manual',
        enabled: true,
        availability: 'unverified',
        route_count: 0,
        first_seen_at: '',
        updated_at: ''
      })
    }
  }
  return [...byID.values()].sort((a, b) => a.model_id.localeCompare(b.model_id))
})

const filteredRows = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return effectiveRows.value.filter((row) => {
    if (availability.value && row.availability !== availability.value) return false
    return !keyword || row.model_id.toLowerCase().includes(keyword)
  })
})

const availableCount = computed(() => effectiveRows.value.filter((row) => row.availability === 'available').length)
const missingCount = computed(() => effectiveRows.value.filter((row) => row.availability === 'missing').length)

function setEnabled(modelID: string, enabled: boolean) {
  const next = new Set(props.modelValue)
  if (enabled) next.add(modelID)
  else next.delete(modelID)
  emit('update:modelValue', [...next].sort())
}

function addCustomModel() {
  const modelID = customModel.value.trim()
  if (!modelID) return
  setEnabled(modelID, true)
  customModel.value = ''
}

function enableAvailable() {
  const next = new Set(props.modelValue)
  for (const row of effectiveRows.value) {
    if (row.availability === 'available') next.add(row.model_id)
  }
  emit('update:modelValue', [...next].sort())
}

function clearEnabled() {
  emit('update:modelValue', [])
}

function availabilityClass(value: ProviderAccountModel['availability']): string {
  if (value === 'available') return 'status-success'
  if (value === 'missing') return 'status-danger'
  return 'status-muted'
}

async function loadInventory() {
  if (!props.accountId) {
    rows.value = []
    return
  }
  loading.value = true
  error.value = ''
  try {
    const inventory = await getProviderAccountModelInventory(props.accountId)
    rows.value = inventory.models
    emit('update:autoEnableNewModels', inventory.auto_enable_new_models)
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

async function discover() {
  if (!props.accountId) return
  discovering.value = true
  error.value = ''
  notice.value = ''
  try {
    const result = await discoverProviderAccountModels(props.accountId)
    discovery.value = result
    rows.value = result.models
    if (props.autoEnableNewModels) {
      const next = new Set(props.modelValue)
      result.added_models.forEach((model) => next.add(model))
      emit('update:modelValue', [...next].sort())
    }
    notice.value = t('providerAccounts.discoveryComplete', { count: result.models.filter((row) => row.availability === 'available').length })
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    discovering.value = false
  }
}

async function applySync() {
  if (!props.accountId) return
  syncing.value = true
  error.value = ''
  notice.value = ''
  try {
    const result = await syncProviderAccountModels(props.accountId, {
      enabled_models: props.modelValue,
      auto_enable_new_models: props.autoEnableNewModels
    })
    rows.value = result.inventory.models
    discovery.value = result.discovery
    emit('update:modelValue', [...result.account.models])
    emit('update:autoEnableNewModels', result.account.auto_enable_new_models)
    emit('synced', result.account)
    notice.value = t('providerAccounts.modelsSynced', { count: result.account.models.length })
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    syncing.value = false
  }
}

defineExpose({ discover })

watch(() => props.accountId, loadInventory)
onMounted(loadInventory)
</script>

<template>
  <section class="model-inventory" :aria-label="t('providerAccounts.upstreamModels')">
    <header class="model-inventory-header">
      <div>
        <strong>{{ t('providerAccounts.upstreamModels') }}</strong>
        <span>{{ t('providerAccounts.modelInventorySummary', { enabled: modelValue.length, available: availableCount }) }}</span>
      </div>
      <button class="button secondary" type="button" :disabled="!accountId || discovering" @click="discover">
        <RefreshCw :size="15" />
        {{ discovering ? t('providerAccounts.discoveringModels') : t('providerAccounts.discoverModels') }}
      </button>
    </header>

    <div class="model-inventory-toolbar">
      <label class="search-box compact-search">
        <Search :size="16" />
        <input v-model="query" :placeholder="t('providerAccounts.searchModels')" />
      </label>
      <select v-model="availability" :aria-label="t('providerAccounts.modelAvailability')">
        <option value="">{{ t('providerAccounts.allModelStates') }}</option>
        <option value="available">{{ t('providerAccounts.modelAvailable') }}</option>
        <option value="missing">{{ t('providerAccounts.modelMissing') }}</option>
        <option value="unverified">{{ t('providerAccounts.modelUnverified') }}</option>
      </select>
      <button class="icon-button" type="button" :title="t('providerAccounts.enableAvailable')" @click="enableAvailable"><CheckCircle2 :size="16" /></button>
      <button class="icon-button" type="button" :title="t('providerAccounts.clearEnabled')" @click="clearEnabled"><X :size="16" /></button>
    </div>

    <div class="model-inventory-custom">
      <input v-model="customModel" :placeholder="t('providerAccounts.customModelPlaceholder')" @keydown.enter.prevent="addCustomModel" />
      <button class="button secondary" type="button" :disabled="!customModel.trim()" @click="addCustomModel"><Plus :size="15" />{{ t('providerAccounts.addCustomModel') }}</button>
    </div>

    <div v-if="error" class="notice model-inventory-notice">{{ error }}</div>
    <div v-else-if="notice" class="notice success model-inventory-notice">{{ notice }}</div>
    <div v-if="discovery?.missing_models.length" class="notice warning model-inventory-notice">
      <AlertTriangle :size="16" />
      <span>{{ t('providerAccounts.missingModelsWarning', { models: discovery.missing_models.join(', '), routes: discovery.affected_route_ids.length }) }}</span>
    </div>

    <div class="model-inventory-table-wrap">
      <table class="model-inventory-table">
        <thead><tr><th>{{ t('providerAccounts.modelEnabled') }}</th><th>{{ t('providerAccounts.modelId') }}</th><th>{{ t('providerAccounts.modelSource') }}</th><th>{{ t('providerAccounts.modelAvailability') }}</th><th>{{ t('gatewayModels.routes') }}</th></tr></thead>
        <tbody>
          <tr v-for="row in filteredRows" :key="row.model_id">
            <td><input type="checkbox" :checked="selected.has(row.model_id)" :aria-label="t('providerAccounts.toggleModel', { model: row.model_id })" @change="setEnabled(row.model_id, ($event.target as HTMLInputElement).checked)" /></td>
            <td><code>{{ row.model_id }}</code><span v-if="row.change === 'added'" class="model-change added">{{ t('providerAccounts.modelNew') }}</span></td>
            <td>{{ row.source === 'discovered' ? t('providerAccounts.modelDiscovered') : t('providerAccounts.modelManual') }}</td>
            <td><span class="pill" :class="availabilityClass(row.availability)">{{ t(`providerAccounts.modelState.${row.availability}`) }}</span></td>
            <td>{{ row.route_count }}</td>
          </tr>
          <tr v-if="!filteredRows.length"><td colspan="5" class="empty-cell">{{ loading ? t('common.loading') : t('providerAccounts.noModels') }}</td></tr>
        </tbody>
      </table>
    </div>

    <footer class="model-inventory-footer">
      <label class="model-auto-enable">
        <input type="checkbox" :checked="autoEnableNewModels" @change="emit('update:autoEnableNewModels', ($event.target as HTMLInputElement).checked)" />
        <span><strong>{{ t('providerAccounts.autoEnableNewModels') }}</strong><small>{{ t('providerAccounts.autoEnableNewModelsHint') }}</small></span>
      </label>
      <div class="model-inventory-actions">
        <span v-if="missingCount" class="status-text danger">{{ t('providerAccounts.missingCount', { count: missingCount }) }}</span>
        <button v-if="accountId" class="button" type="button" :disabled="syncing" @click="applySync"><Save :size="15" />{{ syncing ? t('common.saving') : t('providerAccounts.applyModelChanges') }}</button>
      </div>
    </footer>
  </section>
</template>

<style scoped>
.model-inventory { display: grid; gap: 12px; min-width: 0; }
.model-inventory-header, .model-inventory-footer, .model-inventory-toolbar, .model-inventory-custom, .model-auto-enable, .model-inventory-actions { display: flex; align-items: center; gap: 10px; }
.model-inventory-header, .model-inventory-footer { justify-content: space-between; }
.model-inventory-header > div { display: grid; gap: 2px; }
.model-inventory-header strong { color: var(--text); font-size: 13px; }
.model-inventory-header span, .model-auto-enable small { color: var(--text-muted); font-size: 11px; }
.model-inventory-toolbar { flex-wrap: wrap; }
.compact-search { min-width: 220px; flex: 1; }
.model-inventory-toolbar select { min-height: 38px; border: 1px solid var(--border-strong); border-radius: var(--radius-control); background: var(--surface); color: var(--text); padding: 0 12px; }
.model-inventory-custom input { min-width: 0; min-height: 40px; flex: 1; border: 1px solid var(--border-strong); border-radius: var(--radius-control); background: var(--surface); color: var(--text); padding: 0 12px; }
.model-inventory-notice { margin: 0; display: flex; align-items: flex-start; gap: 8px; }
.model-inventory-table-wrap { max-height: 300px; overflow: auto; border-block: 1px solid var(--border); }
.model-inventory-table { width: 100%; min-width: 620px; border-collapse: collapse; font-size: 12px; }
.model-inventory-table th, .model-inventory-table td { padding: 9px 10px; border-bottom: 1px solid var(--border); text-align: left; }
.model-inventory-table th { position: sticky; z-index: 1; top: 0; background: var(--surface-subtle); color: var(--text-muted); font-size: 10px; text-transform: uppercase; }
.model-inventory-table td code { color: var(--text); }
.model-inventory-table input[type='checkbox'], .model-auto-enable input { width: 16px; height: 16px; accent-color: var(--primary-600); }
.model-change { display: inline-flex; margin-left: 6px; padding: 1px 5px; border-radius: 4px; font-size: 10px; }
.model-change.added { background: var(--success-bg); color: var(--success); }
.model-auto-enable { align-items: flex-start; }
.model-auto-enable span { display: grid; gap: 2px; }
.model-auto-enable strong { color: var(--text-secondary); font-size: 12px; }
.status-text { font-size: 11px; }
.status-text.danger { color: var(--danger); }
@media (max-width: 720px) {
  .model-inventory-header, .model-inventory-footer { align-items: stretch; flex-direction: column; }
  .model-inventory-header .button, .model-inventory-actions .button { width: 100%; }
  .model-inventory-custom { align-items: stretch; flex-direction: column; }
  .model-inventory-actions { align-items: stretch; flex-direction: column; }
}
</style>
