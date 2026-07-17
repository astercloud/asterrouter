<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { Edit3, ListPlus, Network, Plus, RefreshCw, Route, Save, Search, Table2, Trash2, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { bulkCreateModelRoutes, createModelRoute, deleteModelRoute, getGatewayModels, getModelRoutes, getProviderAccounts, updateModelRoute } from '@/api/control'
import type { GatewayModel, ModelRoute, ModelRouteRequest, ProviderAccount } from '@/types'

interface BulkRouteRow {
  upstream_model: string
  gateway_model_id: string
  upstream_format: string
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
const activeView = ref<'routes' | 'matrix'>('routes')
const modalOpen = ref(false)
const editing = ref<ModelRoute | null>(null)
const bulkModalOpen = ref(false)
const bulkSaving = ref(false)
const bulkAccountID = ref('')
const bulkRouteGroup = ref('default')
const bulkPriority = ref(100)
const bulkWeight = ref(100)
const bulkRows = ref<BulkRouteRow[]>([])
const form = reactive<ModelRouteRequest>({ gateway_model_id: '', route_group: 'default', provider_account_id: '', upstream_model: '', upstream_format: 'openai_chat', priority: 100, weight: 100, status: 'active' })

const textFormatsByProvider: Record<string, string[]> = {
  openai_compatible: ['openai_chat', 'openai_responses'],
  anthropic_compatible: ['anthropic_messages'],
  gemini_compatible: ['gemini_generate_content'],
  aws_bedrock: ['bedrock_converse'],
  gcp_vertex: ['anthropic_messages', 'gemini_generate_content'],
  azure_openai: ['openai_chat', 'openai_responses']
}

const nativeMediaProviderTypes = new Set(Object.keys(textFormatsByProvider))

const modelById = computed(() => Object.fromEntries(models.value.map((model) => [model.id, model])))
const activeModels = computed(() => models.value.filter((model) => model.status === 'active'))
const formModels = computed(() => {
  const current = editing.value ? modelById.value[form.gateway_model_id] : undefined
  const compatible = routableModelsForAccount(selectedAccount.value)
  return current && !compatible.some((model) => model.id === current.id) ? [current, ...compatible] : compatible
})
const accountById = computed(() => Object.fromEntries(accounts.value.map((account) => [account.id, account])))
const selectedAccount = computed(() => accountById.value[form.provider_account_id])
const formAccounts = computed(() => {
  const compatible = accounts.value.filter((account) => account.models.length > 0 && routableModelsForAccount(account).length > 0)
  const current = editing.value ? selectedAccount.value : undefined
  return current && !compatible.some((account) => account.id === current.id) ? [current, ...compatible] : compatible
})
const selectedGatewayModel = computed(() => modelById.value[form.gateway_model_id])
const selectedFormats = computed(() => routeFormats(selectedAccount.value, selectedGatewayModel.value))
const bulkAccount = computed(() => accountById.value[bulkAccountID.value])
const bulkAccounts = computed(() => accounts.value.filter((account) => account.models.length > 0 && routableModelsForAccount(account).length > 0))
const hasRoutablePair = computed(() => accounts.value.some((account) => account.models.length > 0 && routableModelsForAccount(account).length > 0))
const bulkSelectedCount = computed(() => bulkRows.value.filter((row) => row.selected && row.gateway_model_id && row.upstream_format && !isBulkDuplicate(row)).length)
const bulkUnmatchedCount = computed(() => bulkRows.value.filter((row) => !row.gateway_model_id || !row.upstream_format).length)
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
const supportMatrix = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return accounts.value.map((account) => {
    const inventory = [...new Set(account.models)].sort().map((upstreamModel) => {
      const modelRoutes = routes.value.filter((route) => route.provider_account_id === account.id && route.upstream_model === upstreamModel)
      return { upstreamModel, routes: modelRoutes }
    }).filter((item) => !keyword || [account.name, account.platform, item.upstreamModel, ...item.routes.map((route) => modelById.value[route.gateway_model_id]?.model_id || '')].some((value) => value.toLowerCase().includes(keyword)))
    return { account, inventory, routed: inventory.filter((item) => item.routes.length > 0).length }
  }).filter((entry) => entry.inventory.length > 0)
})

watch([() => form.provider_account_id, () => form.gateway_model_id], () => {
  if (!editing.value && selectedAccount.value && !routeFormats(selectedAccount.value, selectedGatewayModel.value).length) {
    const replacement = routableModelsForAccount(selectedAccount.value)[0]
    if (replacement && replacement.id !== form.gateway_model_id) {
      form.gateway_model_id = replacement.id
      return
    }
  }
  if (!editing.value && selectedAccount.value?.models.length === 1) form.upstream_model = selectedAccount.value.models[0]
  if (!selectedFormats.value.includes(form.upstream_format)) form.upstream_format = selectedFormats.value[0] || ''
})

watch(bulkAccountID, () => {
  resetBulkRows()
})

function routeFormats(account?: ProviderAccount, model?: GatewayModel): string[] {
  if (!account || !model) return []
  const textFormats = textFormatsByProvider[account.platform] || []
  if (model.modality === 'chat') return textFormats
  if (model.modality === 'multimodal') return nativeMediaProviderTypes.has(account.platform) ? [...textFormats, 'native_media'] : textFormats
  if (model.modality === 'image' || model.modality === 'video') return nativeMediaProviderTypes.has(account.platform) ? ['native_media'] : []
  if (model.modality === 'audio') return account.platform === 'openai_compatible' ? ['native_media'] : []
  return []
}

function routableModelsForAccount(account?: ProviderAccount): GatewayModel[] {
  return account ? activeModels.value.filter((model) => routeFormats(account, model).length > 0) : []
}

function routeCapabilitySupported(route: ModelRoute): boolean {
  return routeFormats(accountById.value[route.provider_account_id], modelById.value[route.gateway_model_id]).includes(route.upstream_format)
}

function routeCapabilityLabel(route: ModelRoute): string {
  if (!routeCapabilitySupported(route)) return t('modelRoutes.capabilityMismatch')
  if (route.upstream_format === 'native_media') {
    const model = modelById.value[route.gateway_model_id]
    return model?.modality === 'audio' ? t('modelRoutes.builtinDirect') : t('modelRoutes.mediaAdapter')
  }
  return t('modelRoutes.textCore')
}

function resetForm() {
  const account = accounts.value.find((candidate) => candidate.models.length > 0 && routableModelsForAccount(candidate).length > 0)
  const model = routableModelsForAccount(account)[0]
  Object.assign(form, { gateway_model_id: model?.id || '', route_group: model?.default_route_group || 'default', provider_account_id: account?.id || '', upstream_model: '', upstream_format: routeFormats(account, model)[0] || '', priority: 100, weight: 100, status: 'active' })
}

function openCreate() { editing.value = null; resetForm(); modalOpen.value = true }
function openEdit(route: ModelRoute) { editing.value = route; Object.assign(form, route); modalOpen.value = true }
function closeModal() { modalOpen.value = false; editing.value = null }

function resetBulkRows() {
  const publicModelByID = new Map(routableModelsForAccount(bulkAccount.value).map((model) => [model.model_id, model.id]))
  bulkRows.value = [...new Set(bulkAccount.value?.models || [])].sort().map((upstreamModel) => {
    const gatewayModelID = publicModelByID.get(upstreamModel) || ''
    const gatewayModel = modelById.value[gatewayModelID]
    const upstreamFormat = routeFormats(bulkAccount.value, gatewayModel)[0] || ''
    const row = { upstream_model: upstreamModel, gateway_model_id: gatewayModelID, upstream_format: upstreamFormat, selected: Boolean(gatewayModelID && upstreamFormat) }
    if (isBulkDuplicate(row)) row.selected = false
    return row
  })
}

function openBulk() {
  bulkRouteGroup.value = 'default'
  bulkPriority.value = 100
  bulkWeight.value = 100
  bulkAccountID.value = bulkAccounts.value[0]?.id || ''
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
  const formats = routeFormats(bulkAccount.value, modelById.value[row.gateway_model_id])
  if (!formats.includes(row.upstream_format)) row.upstream_format = formats[0] || ''
  row.selected = Boolean(row.gateway_model_id && row.upstream_format) && !isBulkDuplicate(row)
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
  const selected = bulkRows.value.filter((row) => row.selected && row.gateway_model_id && row.upstream_format && !isBulkDuplicate(row))
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
        upstream_format: row.upstream_format,
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
    <section class="page-header"><div><h1>{{ t('admin.modelRoutes') }}</h1><p>{{ t('modelRoutes.subtitle') }}</p></div><div class="page-header-actions"><button class="button secondary" type="button" :disabled="!bulkAccounts.length" @click="openBulk"><ListPlus :size="17" />{{ t('modelRoutes.bulkMatch') }}</button><button class="button" type="button" :disabled="!hasRoutablePair" @click="openCreate"><Plus :size="17" />{{ t('modelRoutes.newRoute') }}</button></div></section>
    <div class="crud-summary"><span><strong>{{ routes.length }}</strong>{{ t('modelRoutes.routes') }}</span><span><strong>{{ routes.filter((route) => route.status === 'active').length }}</strong>{{ t('dashboard.active') }}</span><span><strong>{{ new Set(routes.map((route) => route.route_group)).size }}</strong>{{ t('modelRoutes.routeGroups') }}</span><span><strong>{{ new Set(routes.map((route) => route.upstream_format)).size }}</strong>{{ t('modelRoutes.formats') }}</span></div>
    <nav class="route-view-tabs" role="tablist" :aria-label="t('modelRoutes.viewsLabel')">
      <button type="button" role="tab" :aria-selected="activeView === 'routes'" :class="{ active: activeView === 'routes' }" @click="activeView = 'routes'"><Table2 :size="16" />{{ t('modelRoutes.routeList') }}</button>
      <button type="button" role="tab" :aria-selected="activeView === 'matrix'" :class="{ active: activeView === 'matrix' }" @click="activeView = 'matrix'"><Network :size="16" />{{ t('modelRoutes.supportMatrix') }}</button>
    </nav>
    <section class="table-toolbar">
      <label class="search-box"><Search :size="17" /><input v-model="query" :placeholder="t('modelRoutes.searchPlaceholder')" /></label>
      <select v-model="modelFilter"><option value="">{{ t('modelRoutes.allModels') }}</option><option v-for="model in models" :key="model.id" :value="model.id">{{ model.model_id }}</option></select>
      <button class="icon-button" type="button" :disabled="loading" :aria-label="t('common.refresh')" :title="t('common.refresh')" @click="load"><RefreshCw :size="17" /></button>
    </section>
    <div v-if="message" class="notice success">{{ message }}</div><div v-if="error" class="notice">{{ error }}</div>
    <section v-if="activeView === 'routes'" class="panel table-panel"><div class="panel-body table-scroll"><table class="data-table crud-table">
      <thead><tr><th>{{ t('modelRoutes.gatewayModel') }}</th><th>{{ t('modelRoutes.routeGroup') }}</th><th>{{ t('modelRoutes.account') }}</th><th>{{ t('modelRoutes.upstreamFormat') }}</th><th>{{ t('modelRoutes.upstreamModel') }}</th><th>{{ t('modelRoutes.order') }}</th><th>{{ t('providers.status') }}</th><th>{{ t('common.actions') }}</th></tr></thead>
      <tbody>
        <tr v-for="route in filteredRoutes" :key="route.id">
          <td><strong>{{ modelById[route.gateway_model_id]?.model_id || route.gateway_model_id }}</strong><span>{{ modelById[route.gateway_model_id]?.name }}</span></td>
          <td><span class="pill"><Route :size="13" />{{ route.route_group }}</span></td>
          <td><strong>{{ accountById[route.provider_account_id]?.name || route.provider_account_id }}</strong><span>{{ route.provider_account_id }}</span></td>
          <td><code>{{ route.upstream_format }}</code><span>{{ accountById[route.provider_account_id]?.platform }}</span></td>
          <td><code>{{ route.upstream_model }}</code></td><td><strong>P{{ route.priority }}</strong><span>W{{ route.weight }}</span></td>
          <td><span class="pill" :class="route.status === 'active' && routeCapabilitySupported(route) ? 'status-success' : route.status === 'active' ? 'status-warning' : 'status-danger'">{{ route.status }}</span><span class="route-capability" :class="routeCapabilitySupported(route) ? '' : 'capability-error'">{{ routeCapabilityLabel(route) }}</span><span v-if="route.disabled_reason" class="hint">{{ route.disabled_reason }}</span></td>
          <td class="table-actions"><button class="icon-button" type="button" :title="t('common.edit')" @click="openEdit(route)"><Edit3 :size="16" /></button><button class="icon-button danger" type="button" :title="t('modelRoutes.delete')" @click="remove(route)"><Trash2 :size="16" /></button></td>
        </tr>
        <tr v-if="!filteredRoutes.length"><td colspan="8" class="empty-cell">{{ t('modelRoutes.empty') }}</td></tr>
      </tbody>
    </table></div></section>
    <section v-else class="model-support-matrix" :aria-label="t('modelRoutes.supportMatrix')">
      <details v-for="entry in supportMatrix" :key="entry.account.id" class="matrix-account" open>
        <summary><div><strong>{{ entry.account.name }}</strong><span>{{ entry.account.platform }} · {{ entry.account.id }}</span></div><div class="matrix-account-counts"><span>{{ entry.inventory.length }} {{ t('modelRoutes.inventoryModels') }}</span><span>{{ entry.routed }} {{ t('modelRoutes.routed') }}</span></div></summary>
        <div class="matrix-model-list">
          <div v-for="item in entry.inventory" :key="item.upstreamModel" class="matrix-model-row">
            <code>{{ item.upstreamModel }}</code>
            <div class="matrix-route-list"><span v-for="route in item.routes" :key="route.id" class="matrix-route" :class="{ 'capability-error': !routeCapabilitySupported(route) }"><Route :size="13" />{{ modelById[route.gateway_model_id]?.model_id || route.gateway_model_id }}<small>{{ route.route_group }} · {{ route.upstream_format }} · {{ routeCapabilityLabel(route) }}</small></span><span v-if="!item.routes.length" class="pill status-warning">{{ t('modelRoutes.unrouted') }}</span></div>
          </div>
        </div>
      </details>
      <div v-if="!supportMatrix.length" class="empty-cell">{{ t('modelRoutes.empty') }}</div>
    </section>
    <div v-if="modalOpen" class="modal-backdrop"><form class="modal-card" @submit.prevent="save">
        <header class="modal-header"><div><h2>{{ editing ? t('modelRoutes.editRoute') : t('modelRoutes.newRoute') }}</h2><p>{{ t('modelRoutes.modalSubtitle') }}</p></div><button class="icon-button" type="button" :aria-label="t('common.close')" :title="t('common.close')" @click="closeModal"><X :size="18" /></button></header>
      <div class="modal-body form-grid">
        <div class="field"><label>{{ t('modelRoutes.gatewayModel') }}</label><select v-model="form.gateway_model_id" required><option v-for="model in formModels" :key="model.id" :value="model.id">{{ model.model_id }}<template v-if="model.status !== 'active'"> · {{ t('apiKeys.historicalModels') }}</template></option></select></div>
        <div class="field"><label>{{ t('modelRoutes.routeGroup') }}</label><input v-model="form.route_group" required placeholder="default" /></div>
        <div class="field"><label>{{ t('modelRoutes.account') }}</label><select v-model="form.provider_account_id" required><option v-for="account in formAccounts" :key="account.id" :value="account.id">{{ account.name }}</option></select></div>
        <div class="field"><label>{{ t('modelRoutes.upstreamFormat') }}</label><select v-model="form.upstream_format" required :disabled="!selectedFormats.length"><option v-for="format in selectedFormats" :key="format" :value="format">{{ format }}</option></select><span v-if="!selectedFormats.length" class="field-error">{{ t('modelRoutes.unsupportedPair') }}</span></div>
        <div class="field"><label>{{ t('modelRoutes.upstreamModel') }}</label><select v-if="selectedAccount?.models.length" v-model="form.upstream_model" required><option v-for="model in selectedAccount.models" :key="model" :value="model">{{ model }}</option></select><input v-else v-model="form.upstream_model" required /></div>
        <div class="field"><label>{{ t('modelRoutes.priority') }}</label><input v-model.number="form.priority" type="number" min="0" required /></div>
        <div class="field"><label>{{ t('modelRoutes.weight') }}</label><input v-model.number="form.weight" type="number" min="1" max="10000" required /></div>
        <div class="field"><label>{{ t('providers.status') }}</label><select v-model="form.status"><option value="active">active</option><option value="disabled">disabled</option></select></div>
      </div>
      <footer class="modal-footer"><button class="button secondary" type="button" @click="closeModal">{{ t('common.cancel') }}</button><button class="button" type="submit" :disabled="saving || !selectedFormats.length"><Save :size="17" />{{ saving ? t('common.saving') : t('common.save') }}</button></footer>
    </form></div>
    <div v-if="bulkModalOpen" class="modal-backdrop" @click.self="closeBulk">
      <form class="modal-card modal-card-wide" role="dialog" aria-modal="true" :aria-label="t('modelRoutes.bulkMatch')" @submit.prevent="saveBulk">
        <header class="modal-header"><div><h2>{{ t('modelRoutes.bulkMatch') }}</h2><p>{{ t('modelRoutes.bulkMatchSubtitle') }}</p></div><button class="icon-button" type="button" :aria-label="t('common.close')" :title="t('common.close')" @click="closeBulk"><X :size="18" /></button></header>
        <div class="modal-body bulk-route-body">
          <div class="form-grid bulk-route-settings">
            <div class="field form-span-2"><label>{{ t('modelRoutes.account') }}</label><select v-model="bulkAccountID" required :aria-label="t('modelRoutes.account')"><option v-for="account in bulkAccounts" :key="account.id" :value="account.id">{{ account.name }} · {{ account.models.length }} {{ t('gatewayModels.models') }}</option></select></div>
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
              <thead><tr><th>{{ t('modelRoutes.selected') }}</th><th>{{ t('modelRoutes.upstreamModel') }}</th><th>{{ t('modelRoutes.gatewayModel') }}</th><th>{{ t('modelRoutes.upstreamFormat') }}</th><th>{{ t('providers.status') }}</th></tr></thead>
              <tbody>
                <tr v-for="row in bulkRows" :key="row.upstream_model">
                  <td><input v-model="row.selected" type="checkbox" :disabled="!row.gateway_model_id || !row.upstream_format || isBulkDuplicate(row)" :aria-label="t('modelRoutes.toggleBulkRoute', { model: row.upstream_model })" /></td>
                  <td :data-label="t('modelRoutes.upstreamModel')"><code>{{ row.upstream_model }}</code></td>
                  <td :data-label="t('modelRoutes.gatewayModel')"><select v-model="row.gateway_model_id" :aria-label="t('modelRoutes.gatewayModelFor', { model: row.upstream_model })" @change="updateBulkRow(row)"><option value="">{{ t('modelRoutes.noMatch') }}</option><option v-for="model in routableModelsForAccount(bulkAccount)" :key="model.id" :value="model.id">{{ model.model_id }} · {{ model.name }}</option></select></td>
                  <td :data-label="t('modelRoutes.upstreamFormat')"><select v-model="row.upstream_format" :disabled="!row.gateway_model_id" @change="updateBulkRow(row)"><option v-for="format in routeFormats(bulkAccount, modelById[row.gateway_model_id])" :key="format" :value="format">{{ format }}</option></select></td>
                  <td :data-label="t('providers.status')"><span v-if="isBulkDuplicate(row)" class="pill status-muted">{{ t('modelRoutes.alreadyExists') }}</span><span v-else-if="row.gateway_model_id && row.upstream_format" class="pill status-success">{{ t('modelRoutes.ready') }}</span><span v-else class="pill status-warning">{{ t('modelRoutes.needsMatch') }}</span></td>
                </tr>
                <tr v-if="!bulkRows.length"><td colspan="5" class="empty-cell">{{ t('modelRoutes.noAccountModels') }}</td></tr>
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
.route-view-tabs { display: flex; gap: 3px; border-bottom: 1px solid var(--border); }
.route-view-tabs button { display: inline-flex; min-height: 40px; align-items: center; gap: 7px; padding: 0 14px; border: 0; border-bottom: 2px solid transparent; background: transparent; color: var(--text-muted); cursor: pointer; font-weight: 700; }
.route-view-tabs button.active { border-bottom-color: var(--primary-600); color: var(--primary-700); }
.model-support-matrix { border-block: 1px solid var(--border); }
.matrix-account { border-bottom: 1px solid var(--border); background: var(--surface); }
.matrix-account:last-child { border-bottom: 0; }
.matrix-account summary { display: flex; min-height: 62px; align-items: center; justify-content: space-between; gap: 16px; padding: 10px 16px; cursor: pointer; }
.matrix-account summary > div:first-child { display: grid; gap: 3px; min-width: 0; }
.matrix-account summary strong { color: var(--text); font-size: 13px; }
.matrix-account summary span { color: var(--text-muted); font-size: 11px; overflow-wrap: anywhere; }
.matrix-account-counts { display: flex; flex: 0 0 auto; gap: 12px; }
.matrix-model-list { border-top: 1px solid var(--border); background: var(--surface-subtle); }
.matrix-model-row { display: grid; grid-template-columns: minmax(240px, .8fr) minmax(0, 1.2fr); gap: 18px; align-items: center; min-height: 50px; padding: 8px 16px 8px 36px; border-bottom: 1px solid var(--border); }
.matrix-model-row:last-child { border-bottom: 0; }
.matrix-model-row > code { min-width: 0; overflow-wrap: anywhere; }
.matrix-route-list { display: flex; flex-wrap: wrap; gap: 6px; }
.matrix-route { display: inline-flex; min-width: 0; align-items: center; gap: 5px; padding: 5px 8px; border: 1px solid var(--border); border-radius: var(--radius-control); background: var(--surface); color: var(--text-secondary); font-size: 11px; }
.matrix-route small { color: var(--text-muted); }
.matrix-route.capability-error { border-color: var(--danger); color: var(--danger); }
.route-capability { display: block; color: var(--text-muted); font-size: 11px; }
.route-capability.capability-error, .field-error { color: var(--danger); }
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
  .route-view-tabs { overflow-x: auto; }
  .route-view-tabs button { flex: 1 0 auto; }
  .matrix-account summary { align-items: flex-start; flex-direction: column; }
  .matrix-model-row { grid-template-columns: 1fr; gap: 7px; padding: 11px 12px 11px 24px; }
  .bulk-route-table-wrap { overflow-x: hidden; }
  .bulk-route-table, .bulk-route-table tbody { display: block; min-width: 0; }
  .bulk-route-table thead { display: none; }
  .bulk-route-table tr { display: grid; grid-template-columns: 22px minmax(0, 1fr); gap: 8px 10px; padding: 12px 4px; border-bottom: 1px solid var(--border); }
  .bulk-route-table td { min-width: 0; padding: 0; border: 0; }
  .bulk-route-table td:first-child { grid-row: 1 / 5; }
  .bulk-route-table td:not(:first-child) { grid-column: 2; }
  .bulk-route-table td[data-label]::before { display: block; margin-bottom: 3px; color: var(--text-muted); content: attr(data-label); font-size: 10px; font-weight: 700; text-transform: uppercase; }
  .bulk-route-table select { width: 100%; min-width: 0; }
}
</style>
