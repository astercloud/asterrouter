<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { Edit3, ListPlus, Plus, RefreshCw, Route, Save, Search, Trash2, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { bulkCreateModelRoutes, createModelRoute, deleteModelRoute, getGatewayModels, getModelRoutes, getProviderAccounts, updateModelRoute } from '@/api/control'
import type { GatewayModel, ModelRoute, ModelRouteRequest, ProviderAccount } from '@/types'

interface BulkRouteRow {
  upstream_model: string
  gateway_model_id: string
  selected: boolean
}

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const message = ref('')
const routes = ref<ModelRoute[]>([])
const models = ref<GatewayModel[]>([])
const accounts = ref<ProviderAccount[]>([])
const query = ref('')
const modelFilter = ref('')
const modalOpen = ref(false)
const editing = ref<ModelRoute | null>(null)
const bulkModalOpen = ref(false)
const bulkSaving = ref(false)
const bulkAccountID = ref('')
const bulkRouteGroup = ref('default')
const bulkPriority = ref(100)
const bulkWeight = ref(100)
const bulkRows = ref<BulkRouteRow[]>([])
const form = reactive<ModelRouteRequest>({ gateway_model_id: '', route_group: 'default', provider_account_id: '', upstream_model: '', priority: 100, weight: 100, status: 'active' })

const modelById = computed(() => Object.fromEntries(models.value.map((model) => [model.id, model])))
const activeModels = computed(() => models.value.filter((model) => model.status === 'active'))
const formModels = computed(() => {
  const current = editing.value ? modelById.value[form.gateway_model_id] : undefined
  return current && current.status !== 'active' ? [current, ...activeModels.value] : activeModels.value
})
const accountById = computed(() => Object.fromEntries(accounts.value.map((account) => [account.id, account])))
const selectedAccount = computed(() => accountById.value[form.provider_account_id])
const bulkAccount = computed(() => accountById.value[bulkAccountID.value])
const bulkSelectedCount = computed(() => bulkRows.value.filter((row) => row.selected && row.gateway_model_id && !isBulkDuplicate(row)).length)
const bulkUnmatchedCount = computed(() => bulkRows.value.filter((row) => !row.gateway_model_id).length)
const bulkDuplicateCount = computed(() => bulkRows.value.filter(isBulkDuplicate).length)
const filteredRoutes = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return routes.value.filter((route) => {
    if (modelFilter.value && route.gateway_model_id !== modelFilter.value) return false
    const model = modelById.value[route.gateway_model_id]
    const account = accountById.value[route.provider_account_id]
    return !keyword || [model?.model_id || '', route.route_group, account?.name || '', route.upstream_model].some((value) => value.toLowerCase().includes(keyword))
  })
})

watch(() => form.provider_account_id, () => {
  if (!editing.value && selectedAccount.value?.models.length === 1) form.upstream_model = selectedAccount.value.models[0]
})

watch(bulkAccountID, resetBulkRows)

function resetForm() {
  Object.assign(form, { gateway_model_id: activeModels.value[0]?.id || '', route_group: activeModels.value[0]?.default_route_group || 'default', provider_account_id: accounts.value[0]?.id || '', upstream_model: '', priority: 100, weight: 100, status: 'active' })
}

function openCreate() { editing.value = null; resetForm(); modalOpen.value = true }
function openEdit(route: ModelRoute) { editing.value = route; Object.assign(form, route); modalOpen.value = true }
function closeModal() { modalOpen.value = false; editing.value = null }

function resetBulkRows() {
  const publicModelByID = new Map(activeModels.value.map((model) => [model.model_id, model.id]))
  bulkRows.value = [...new Set(bulkAccount.value?.models || [])].sort().map((upstreamModel) => {
    const gatewayModelID = publicModelByID.get(upstreamModel) || ''
    const row = { upstream_model: upstreamModel, gateway_model_id: gatewayModelID, selected: Boolean(gatewayModelID) }
    if (isBulkDuplicate(row)) row.selected = false
    return row
  })
}

function openBulk() {
  bulkRouteGroup.value = 'default'
  bulkPriority.value = 100
  bulkWeight.value = 100
  bulkAccountID.value = accounts.value.find((account) => account.models.length > 0)?.id || ''
  resetBulkRows()
  bulkModalOpen.value = true
}

function closeBulk() {
  bulkModalOpen.value = false
  bulkRows.value = []
}

function isBulkDuplicate(row: Pick<BulkRouteRow, 'gateway_model_id' | 'upstream_model'>): boolean {
  if (!row.gateway_model_id || !bulkAccountID.value) return false
  return routes.value.some((route) =>
    route.gateway_model_id === row.gateway_model_id &&
    route.route_group === bulkRouteGroup.value.trim() &&
    route.provider_account_id === bulkAccountID.value &&
    route.upstream_model === row.upstream_model
  )
}

function updateBulkRow(row: BulkRouteRow) {
  row.selected = Boolean(row.gateway_model_id) && !isBulkDuplicate(row)
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [routeData, modelData, accountData] = await Promise.all([getModelRoutes(), getGatewayModels(), getProviderAccounts()])
    routes.value = routeData
    models.value = modelData
    accounts.value = accountData
  } catch (err) { error.value = err instanceof Error ? err.message : t('common.failed') } finally { loading.value = false }
}

async function save() {
  saving.value = true; error.value = ''; message.value = ''
  try {
    if (editing.value) { await updateModelRoute(editing.value.id, { ...form }); message.value = t('modelRoutes.updated') }
    else { await createModelRoute({ ...form }); message.value = t('modelRoutes.created') }
    closeModal(); await load()
  } catch (err) { error.value = err instanceof Error ? err.message : t('common.failed') } finally { saving.value = false }
}

async function saveBulk() {
  const selected = bulkRows.value.filter((row) => row.selected && row.gateway_model_id && !isBulkDuplicate(row))
  if (!selected.length) return
  bulkSaving.value = true
  error.value = ''
  message.value = ''
  try {
    const result = await bulkCreateModelRoutes({
      routes: selected.map((row) => ({
        gateway_model_id: row.gateway_model_id,
        route_group: bulkRouteGroup.value.trim() || 'default',
        provider_account_id: bulkAccountID.value,
        upstream_model: row.upstream_model,
        priority: bulkPriority.value,
        weight: bulkWeight.value,
        status: 'active'
      }))
    })
    message.value = t('modelRoutes.bulkCreated', { count: result.routes.length })
    closeBulk()
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    bulkSaving.value = false
  }
}

async function remove(route: ModelRoute) {
  if (!window.confirm(t('modelRoutes.deleteConfirm'))) return
  try { await deleteModelRoute(route.id); message.value = t('modelRoutes.deleted'); await load() }
  catch (err) { error.value = err instanceof Error ? err.message : t('common.failed') }
}

onMounted(load)
</script>

<template>
  <main class="content crud-page">
    <section class="page-header"><div><h1>{{ t('admin.modelRoutes') }}</h1><p>{{ t('modelRoutes.subtitle') }}</p></div><div class="page-header-actions"><button class="button secondary" type="button" :disabled="!activeModels.length || !accounts.some((account) => account.models.length)" @click="openBulk"><ListPlus :size="17" />{{ t('modelRoutes.bulkMatch') }}</button><button class="button" type="button" :disabled="!activeModels.length || !accounts.length" @click="openCreate"><Plus :size="17" />{{ t('modelRoutes.newRoute') }}</button></div></section>
    <div class="crud-summary"><span><strong>{{ routes.length }}</strong>{{ t('modelRoutes.routes') }}</span><span><strong>{{ routes.filter((route) => route.status === 'active').length }}</strong>{{ t('dashboard.active') }}</span><span><strong>{{ new Set(routes.map((route) => route.route_group)).size }}</strong>{{ t('modelRoutes.routeGroups') }}</span></div>
    <section class="table-toolbar">
      <label class="search-box"><Search :size="17" /><input v-model="query" :placeholder="t('modelRoutes.searchPlaceholder')" /></label>
      <select v-model="modelFilter"><option value="">{{ t('modelRoutes.allModels') }}</option><option v-for="model in models" :key="model.id" :value="model.id">{{ model.model_id }}</option></select>
      <button class="button secondary" type="button" :disabled="loading" @click="load"><RefreshCw :size="17" />{{ t('common.refresh') }}</button>
    </section>
    <div v-if="message" class="notice success">{{ message }}</div><div v-if="error" class="notice">{{ error }}</div>
    <section class="panel table-panel"><div class="panel-body table-scroll"><table class="data-table crud-table">
      <thead><tr><th>{{ t('modelRoutes.gatewayModel') }}</th><th>{{ t('modelRoutes.routeGroup') }}</th><th>{{ t('modelRoutes.account') }}</th><th>{{ t('modelRoutes.upstreamModel') }}</th><th>{{ t('modelRoutes.order') }}</th><th>{{ t('providers.status') }}</th><th>{{ t('common.actions') }}</th></tr></thead>
      <tbody>
        <tr v-for="route in filteredRoutes" :key="route.id">
          <td><strong>{{ modelById[route.gateway_model_id]?.model_id || route.gateway_model_id }}</strong><span>{{ modelById[route.gateway_model_id]?.name }}</span></td>
          <td><span class="pill"><Route :size="13" />{{ route.route_group }}</span></td>
          <td><strong>{{ accountById[route.provider_account_id]?.name || route.provider_account_id }}</strong><span>{{ route.provider_account_id }}</span></td>
          <td><code>{{ route.upstream_model }}</code></td><td><strong>P{{ route.priority }}</strong><span>W{{ route.weight }}</span></td>
          <td><span class="pill" :class="route.status === 'active' ? 'status-success' : 'status-danger'">{{ route.status }}</span></td>
          <td class="table-actions"><button class="icon-button" type="button" :title="t('common.edit')" @click="openEdit(route)"><Edit3 :size="16" /></button><button class="icon-button danger" type="button" :title="t('modelRoutes.delete')" @click="remove(route)"><Trash2 :size="16" /></button></td>
        </tr>
        <tr v-if="!filteredRoutes.length"><td colspan="7" class="empty-cell">{{ t('modelRoutes.empty') }}</td></tr>
      </tbody>
    </table></div></section>
    <div v-if="modalOpen" class="modal-backdrop"><form class="modal-card" @submit.prevent="save">
      <header class="modal-header"><div><h2>{{ editing ? t('modelRoutes.editRoute') : t('modelRoutes.newRoute') }}</h2><p>{{ t('modelRoutes.modalSubtitle') }}</p></div><button class="icon-button" type="button" @click="closeModal"><X :size="18" /></button></header>
      <div class="modal-body form-grid">
        <div class="field"><label>{{ t('modelRoutes.gatewayModel') }}</label><select v-model="form.gateway_model_id" required><option v-for="model in formModels" :key="model.id" :value="model.id">{{ model.model_id }}<template v-if="model.status !== 'active'"> · {{ t('apiKeys.historicalModels') }}</template></option></select></div>
        <div class="field"><label>{{ t('modelRoutes.routeGroup') }}</label><input v-model="form.route_group" required placeholder="default" /></div>
        <div class="field"><label>{{ t('modelRoutes.account') }}</label><select v-model="form.provider_account_id" required><option v-for="account in accounts" :key="account.id" :value="account.id">{{ account.name }}</option></select></div>
        <div class="field"><label>{{ t('modelRoutes.upstreamModel') }}</label><select v-if="selectedAccount?.models.length" v-model="form.upstream_model" required><option v-for="model in selectedAccount.models" :key="model" :value="model">{{ model }}</option></select><input v-else v-model="form.upstream_model" required /></div>
        <div class="field"><label>{{ t('modelRoutes.priority') }}</label><input v-model.number="form.priority" type="number" min="0" required /></div>
        <div class="field"><label>{{ t('modelRoutes.weight') }}</label><input v-model.number="form.weight" type="number" min="1" max="10000" required /></div>
        <div class="field"><label>{{ t('providers.status') }}</label><select v-model="form.status"><option value="active">active</option><option value="disabled">disabled</option></select></div>
      </div>
      <footer class="modal-footer"><button class="button secondary" type="button" @click="closeModal">{{ t('common.cancel') }}</button><button class="button" type="submit" :disabled="saving"><Save :size="17" />{{ saving ? t('common.saving') : t('common.save') }}</button></footer>
    </form></div>
    <div v-if="bulkModalOpen" class="modal-backdrop" @click.self="closeBulk">
      <form class="modal-card modal-card-wide" role="dialog" aria-modal="true" :aria-label="t('modelRoutes.bulkMatch')" @submit.prevent="saveBulk">
        <header class="modal-header"><div><h2>{{ t('modelRoutes.bulkMatch') }}</h2><p>{{ t('modelRoutes.bulkMatchSubtitle') }}</p></div><button class="icon-button" type="button" :title="t('common.close')" @click="closeBulk"><X :size="18" /></button></header>
        <div class="modal-body bulk-route-body">
          <div class="form-grid bulk-route-settings">
            <div class="field form-span-2"><label>{{ t('modelRoutes.account') }}</label><select v-model="bulkAccountID" required :aria-label="t('modelRoutes.account')"><option v-for="account in accounts.filter((item) => item.models.length)" :key="account.id" :value="account.id">{{ account.name }} · {{ account.models.length }} {{ t('gatewayModels.models') }}</option></select></div>
            <div class="field"><label>{{ t('modelRoutes.routeGroup') }}</label><input v-model="bulkRouteGroup" required placeholder="default" /></div>
            <div class="field"><label>{{ t('modelRoutes.priority') }}</label><input v-model.number="bulkPriority" type="number" min="0" required /></div>
            <div class="field"><label>{{ t('modelRoutes.weight') }}</label><input v-model.number="bulkWeight" type="number" min="1" max="10000" required /></div>
          </div>
          <div class="bulk-route-summary">
            <span><strong>{{ bulkSelectedCount }}</strong>{{ t('modelRoutes.selected') }}</span>
            <span><strong>{{ bulkUnmatchedCount }}</strong>{{ t('modelRoutes.unmatched') }}</span>
            <span><strong>{{ bulkDuplicateCount }}</strong>{{ t('modelRoutes.existing') }}</span>
          </div>
          <div class="table-scroll bulk-route-table-wrap">
            <table class="data-table crud-table bulk-route-table">
              <thead><tr><th>{{ t('modelRoutes.selected') }}</th><th>{{ t('modelRoutes.upstreamModel') }}</th><th>{{ t('modelRoutes.gatewayModel') }}</th><th>{{ t('providers.status') }}</th></tr></thead>
              <tbody>
                <tr v-for="row in bulkRows" :key="row.upstream_model">
                  <td><input v-model="row.selected" type="checkbox" :disabled="!row.gateway_model_id || isBulkDuplicate(row)" :aria-label="t('modelRoutes.toggleBulkRoute', { model: row.upstream_model })" /></td>
                  <td :data-label="t('modelRoutes.upstreamModel')"><code>{{ row.upstream_model }}</code></td>
                  <td :data-label="t('modelRoutes.gatewayModel')"><select v-model="row.gateway_model_id" :aria-label="t('modelRoutes.gatewayModelFor', { model: row.upstream_model })" @change="updateBulkRow(row)"><option value="">{{ t('modelRoutes.noMatch') }}</option><option v-for="model in activeModels" :key="model.id" :value="model.id">{{ model.model_id }} · {{ model.name }}</option></select></td>
                  <td :data-label="t('providers.status')"><span v-if="isBulkDuplicate(row)" class="pill status-muted">{{ t('modelRoutes.alreadyExists') }}</span><span v-else-if="row.gateway_model_id" class="pill status-success">{{ t('modelRoutes.ready') }}</span><span v-else class="pill status-warning">{{ t('modelRoutes.needsMatch') }}</span></td>
                </tr>
                <tr v-if="!bulkRows.length"><td colspan="4" class="empty-cell">{{ t('modelRoutes.noAccountModels') }}</td></tr>
              </tbody>
            </table>
          </div>
        </div>
        <footer class="modal-footer"><span class="hint">{{ t('modelRoutes.bulkAtomicHint') }}</span><div class="modal-footer-actions"><button class="button secondary" type="button" @click="closeBulk">{{ t('common.cancel') }}</button><button class="button" type="submit" :disabled="bulkSaving || bulkSelectedCount === 0"><Save :size="17" />{{ bulkSaving ? t('common.saving') : t('modelRoutes.createSelected', { count: bulkSelectedCount }) }}</button></div></footer>
      </form>
    </div>
  </main>
</template>

<style scoped>
.page-header-actions, .modal-footer-actions { display: flex; align-items: center; gap: 10px; }
.bulk-route-body { display: grid; gap: 16px; }
.bulk-route-settings { flex: 0 0 auto; padding: 0; }
.bulk-route-summary { display: flex; gap: 20px; padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--radius-control); background: var(--surface-subtle); }
.bulk-route-summary span { display: flex; align-items: baseline; gap: 5px; color: var(--text-muted); font-size: 11px; }
.bulk-route-summary strong { color: var(--text); font-size: 15px; }
.bulk-route-table-wrap { max-height: 360px; border-block: 1px solid var(--border); }
.bulk-route-table { min-width: 680px; }
.bulk-route-table select { min-width: 260px; }
.bulk-route-table input[type='checkbox'] { width: 16px; height: 16px; accent-color: var(--primary-600); }
@media (max-width: 720px) {
  .page-header-actions { width: 100%; align-items: stretch; flex-direction: column; }
  .page-header-actions .button { width: 100%; }
  .modal-footer { align-items: stretch; flex-direction: column; }
  .modal-footer-actions { align-items: stretch; flex-direction: column-reverse; }
  .modal-footer-actions .button { width: 100%; }
  .bulk-route-summary { flex-wrap: wrap; }
  .bulk-route-table-wrap { overflow-x: hidden; }
  .bulk-route-table, .bulk-route-table tbody { display: block; min-width: 0; }
  .bulk-route-table thead { display: none; }
  .bulk-route-table tr { display: grid; grid-template-columns: 22px minmax(0, 1fr); gap: 8px 10px; padding: 12px 4px; border-bottom: 1px solid var(--border); }
  .bulk-route-table td { min-width: 0; padding: 0; border: 0; }
  .bulk-route-table td:first-child { grid-row: 1 / 4; }
  .bulk-route-table td:not(:first-child) { grid-column: 2; }
  .bulk-route-table td[data-label]::before { display: block; margin-bottom: 3px; color: var(--text-muted); content: attr(data-label); font-size: 10px; font-weight: 700; text-transform: uppercase; }
  .bulk-route-table select { width: 100%; min-width: 0; }
}
</style>
