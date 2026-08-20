<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import {
  Activity,
  ChevronDown,
  ChevronRight,
  CircleAlert,
  ExternalLink,
  Gauge,
  Layers3,
  List,
  RefreshCw,
  Search,
  Star,
  X
} from '@lucide/vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  getAPIKeys,
  getGatewayModels,
  getModelRoutes,
  getProcurementPrices,
  getProviderAccountHealthChecks,
  getProviderAccounts,
  getProviders,
  getRoutingPolicies,
  getSupplyUtilization,
  updateRoutingPolicy
} from '@/api/control'
import type {
  APIKeyRecord,
  GatewayModel,
  ModelRoute,
  ProcurementPrice,
  ProviderAccount,
  ProviderAccountHealthCheck,
  ProviderConnection,
  RoutingPolicy,
  SupplyUtilizationReport
} from '@/types'
import {
  accountBatchIndex,
  assignAccountToBatch,
  buildSupplyCatalogRows,
  togglePreferredAccount,
  protocolLabelKey,
  type ModelFamily,
  type SupplyCatalogRow,
  type SupplyCatalogTag
} from '@/utils/supplyCatalog'

type ViewMode = 'model' | 'route'

const familyOrder: ModelFamily[] = ['claude', 'openai', 'gemini', 'grok', 'deepseek', 'qwen', 'glm', 'other']

const { t, locale } = useI18n()
const loading = ref(false)
const actionAccountID = ref('')
const error = ref('')
const message = ref('')
const models = ref<GatewayModel[]>([])
const routes = ref<ModelRoute[]>([])
const providers = ref<ProviderConnection[]>([])
const accounts = ref<ProviderAccount[]>([])
const prices = ref<ProcurementPrice[]>([])
const healthChecks = ref<ProviderAccountHealthCheck[]>([])
const utilization = ref<SupplyUtilizationReport | null>(null)
const policies = ref<RoutingPolicy[]>([])
const apiKeys = ref<APIKeyRecord[]>([])
const selectedPolicyID = ref('')
const query = ref('')
const familyFilter = ref<ModelFamily | ''>('')
const modalityFilter = ref('')
const modelFilter = ref('')
const routeGroupFilter = ref('')
const providerFilter = ref('')
const protocolFilter = ref('')
const tagFilter = ref('')
const schedulableOnly = ref(false)
const viewMode = ref<ViewMode>('model')
const expandedModels = ref(new Set<string>())
const selectedRouteID = ref('')

const rows = computed(() => buildSupplyCatalogRows({
  models: models.value,
  routes: routes.value,
  providers: providers.value,
  accounts: accounts.value,
  prices: prices.value,
  healthChecks: healthChecks.value,
  utilization: utilization.value
}))
const activePolicies = computed(() => policies.value.filter((policy) => policy.status === 'active'))
const selectedPolicy = computed(() => policies.value.find((policy) => policy.id === selectedPolicyID.value) || null)
const policyBindingCount = computed(() => apiKeys.value.filter((key) => key.routing_policy_id === selectedPolicyID.value).length)
const selectedRow = computed(() => rows.value.find((row) => row.id === selectedRouteID.value) || null)
const modelOptions = computed(() => Array.from(new Set(rows.value.map((row) => row.modelID))).sort())
const routeGroupOptions = computed(() => Array.from(new Set(rows.value.map((row) => row.routeGroup))).sort())
const providerOptions = computed(() => Array.from(new Map(rows.value.map((row) => [row.providerID, row.providerName])).entries()).sort((left, right) => left[1].localeCompare(right[1])))
const protocolOptions = computed(() => Array.from(new Set(rows.value.map((row) => row.upstreamFormat))).sort())
const modalityOptions = computed(() => Array.from(new Set(rows.value.map((row) => row.modality))).sort())
const familyOptions = computed(() => familyOrder.filter((family) => rows.value.some((row) => row.modelFamily === family)))
const tagOptions: SupplyCatalogTag[] = ['healthy', 'low_cost', 'low_latency', 'unpriced', 'unavailable']

const filteredRows = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return rows.value.filter((row) => {
    if (modalityFilter.value && row.modality !== modalityFilter.value) return false
    if (familyFilter.value && row.modelFamily !== familyFilter.value) return false
    if (modelFilter.value && row.modelID !== modelFilter.value) return false
    if (routeGroupFilter.value && row.routeGroup !== routeGroupFilter.value) return false
    if (providerFilter.value && row.providerID !== providerFilter.value) return false
    if (protocolFilter.value && row.upstreamFormat !== protocolFilter.value) return false
    if (tagFilter.value && !row.tags.includes(tagFilter.value as SupplyCatalogTag)) return false
    if (schedulableOnly.value && !row.available) return false
    if (!keyword) return true
    return [row.modelID, row.modelName, row.providerName, row.accountName, row.upstreamModel, row.routeGroup, row.providerType]
      .some((value) => value.toLowerCase().includes(keyword))
  })
})

const modelGroups = computed(() => {
  const grouped = new Map<string, SupplyCatalogRow[]>()
  for (const row of filteredRows.value) {
    const items = grouped.get(row.modelID) || []
    items.push(row)
    grouped.set(row.modelID, items)
  }
  return Array.from(grouped.entries()).map(([modelID, items]) => {
    const priced = items.filter((row) => row.price)
    const observed = items.filter((row) => (row.utilization?.demand.requests || 0) > 0)
    return {
      modelID,
      name: items[0]?.modelName || modelID,
      family: items[0]?.modelFamily || 'other' as ModelFamily,
      modality: items[0]?.modality || '',
      routeGroup: items[0]?.routeGroup || '',
      rows: items.sort((left, right) => compareRows(left, right)),
      startPrice: priced.length ? Math.min(...priced.map((row) => row.price!.uncached_input_micros_per_1m_tokens)) : null,
      bestSuccessRate: observed.length ? Math.max(...observed.map((row) => row.utilization!.demand.success_rate)) : null,
      availableCount: items.filter((row) => row.available).length,
      tags: Array.from(new Set(items.flatMap((row) => row.tags))).slice(0, 3)
    }
  }).sort((left, right) => left.modelID.localeCompare(right.modelID))
})

const summary = computed(() => ({
  routes: rows.value.length,
  models: new Set(rows.value.map((row) => row.modelID)).size,
  available: rows.value.filter((row) => row.available).length,
  unpriced: rows.value.filter((row) => !row.price).length
}))

function compareRows(left: SupplyCatalogRow, right: SupplyCatalogRow): number {
  if (left.available !== right.available) return left.available ? -1 : 1
  const leftPrice = left.price?.uncached_input_micros_per_1m_tokens ?? Number.MAX_SAFE_INTEGER
  const rightPrice = right.price?.uncached_input_micros_per_1m_tokens ?? Number.MAX_SAFE_INTEGER
  return leftPrice - rightPrice || left.priority - right.priority || left.accountName.localeCompare(right.accountName)
}

function resetFilters() {
  query.value = ''
  familyFilter.value = ''
  modalityFilter.value = ''
  modelFilter.value = ''
  routeGroupFilter.value = ''
  providerFilter.value = ''
  protocolFilter.value = ''
  tagFilter.value = ''
  schedulableOnly.value = false
}

function toggleModel(modelID: string) {
  const next = new Set(expandedModels.value)
  if (next.has(modelID)) next.delete(modelID)
  else next.add(modelID)
  expandedModels.value = next
}

function formatPrice(micros?: number | null): string {
  if (micros == null) return '—'
  return new Intl.NumberFormat(locale.value === 'zh-CN' ? 'zh-CN' : 'en-US', {
    style: 'currency', currency: 'USD', minimumFractionDigits: 4, maximumFractionDigits: 6
  }).format(micros / 1_000_000)
}

function formatPercent(value?: number | null): string {
  if (value == null) return '—'
  return new Intl.NumberFormat(locale.value === 'zh-CN' ? 'zh-CN' : 'en-US', {
    style: 'percent', minimumFractionDigits: 1, maximumFractionDigits: 1
  }).format(value)
}

function formatMultiplier(value?: number | null): string {
  return value == null ? '—' : `${value.toFixed(2)}×`
}

function formatDate(value?: string): string {
  if (!value) return '—'
  return new Intl.DateTimeFormat(locale.value === 'zh-CN' ? 'zh-CN' : 'en-US', {
    dateStyle: 'medium', timeStyle: 'short'
  }).format(new Date(value))
}

function healthLabel(row: SupplyCatalogRow): string {
  if (!row.available) return t('supplyCatalog.health.unavailable')
  return t(`supplyCatalog.health.${row.health?.status || 'unchecked'}`)
}

function healthClass(row: SupplyCatalogRow): string {
  if (!row.available || row.health?.status === 'error') return 'status-danger'
  if (row.health?.status === 'warning') return 'status-warning'
  if (row.health?.status === 'ok') return 'status-success'
  return ''
}

function tagLabel(tag: SupplyCatalogTag): string {
  return t(`supplyCatalog.tags.${tag}`)
}

function modalityLabel(modality: string): string {
  const supported = ['chat', 'embedding', 'image', 'video', 'audio', 'multimodal']
  return supported.includes(modality) ? t(`supplyCatalog.modalities.${modality}`) : modality
}

function familyLabel(family: ModelFamily): string {
  return t(`supplyCatalog.families.${family}`)
}

function providerInitial(name: string): string {
  return name.trim().slice(0, 1).toUpperCase() || '?'
}

function protocolLabel(protocol: string): string {
  const key = protocolLabelKey(protocol)
  const translated = t(`routingPolicy.protocols.${key}`)
  return translated === `routingPolicy.protocols.${key}` ? protocol : translated
}

function isPreferred(row: SupplyCatalogRow): boolean {
  return selectedPolicy.value?.strategy.preferred_provider_account_ids.includes(row.accountID) || false
}

function batchIndex(row: SupplyCatalogRow): number | null {
  return selectedPolicy.value ? accountBatchIndex(selectedPolicy.value, row.accountID) : null
}

function batchLabel(row: SupplyCatalogRow): string {
  const index = batchIndex(row)
  return index == null ? t('supplyCatalog.actions.notInBatch') : t('supplyCatalog.actions.batchNumber', { number: index + 1 })
}

function policyActionDisabled(row: SupplyCatalogRow): boolean {
  return loading.value || actionAccountID.value !== '' || !selectedPolicy.value || selectedPolicy.value.route_group !== row.routeGroup
}

function actionHint(row: SupplyCatalogRow): string {
  if (!selectedPolicy.value) return t('supplyCatalog.actions.selectPolicyFirst')
  if (selectedPolicy.value.route_group !== row.routeGroup) {
    return t('supplyCatalog.actions.routeGroupMismatch', { group: selectedPolicy.value.route_group })
  }
  return ''
}

async function persistPolicy(row: SupplyCatalogRow, request: ReturnType<typeof togglePreferredAccount>, successKey: string): Promise<boolean> {
  if (!selectedPolicy.value) return false
  actionAccountID.value = row.accountID
  error.value = ''
  message.value = ''
  try {
    const updated = await updateRoutingPolicy(selectedPolicy.value.id, request)
    policies.value = policies.value.map((policy) => policy.id === updated.id ? updated : policy)
    message.value = t(successKey, { account: row.accountName })
    return true
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
    return false
  } finally {
    actionAccountID.value = ''
  }
}

async function togglePreferred(row: SupplyCatalogRow) {
  if (!selectedPolicy.value || policyActionDisabled(row)) return
  const preferred = isPreferred(row)
  await persistPolicy(
    row,
    togglePreferredAccount(selectedPolicy.value, row.accountID),
    preferred ? 'supplyCatalog.messages.preferredRemoved' : 'supplyCatalog.messages.preferredAdded'
  )
}

async function changeBatch(row: SupplyCatalogRow, event: Event) {
  if (!selectedPolicy.value || policyActionDisabled(row)) return
  const value = (event.target as HTMLSelectElement).value
  const target = value === '__new' ? 'new' : value === '' ? null : Number(value)
  const persisted = await persistPolicy(
    row,
    assignAccountToBatch(selectedPolicy.value, row.accountID, target, t('supplyCatalog.actions.primaryBatch')),
    target == null ? 'supplyCatalog.messages.batchRemoved' : 'supplyCatalog.messages.batchUpdated'
  )
  if (!persisted) (event.target as HTMLSelectElement).value = String(batchIndex(row) ?? '')
}

function openDetails(row: SupplyCatalogRow) {
  selectedRouteID.value = row.id
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [modelData, routeData, providerData, accountData, priceData, policyData, healthData, utilizationData, keyData] = await Promise.all([
      getGatewayModels(),
      getModelRoutes(),
      getProviders(),
      getProviderAccounts(),
      getProcurementPrices(),
      getRoutingPolicies(),
      getProviderAccountHealthChecks().catch(() => []),
      getSupplyUtilization(24).catch(() => null),
      getAPIKeys().catch(() => [])
    ])
    models.value = modelData
    routes.value = routeData
    providers.value = providerData
    accounts.value = accountData
    prices.value = priceData
    policies.value = policyData
    healthChecks.value = healthData
    utilization.value = utilizationData
    apiKeys.value = keyData

    const savedPolicyID = localStorage.getItem('asterrouter_supply_catalog_policy') || ''
    const nextPolicy = policyData.find((policy) => policy.id === selectedPolicyID.value)
      || policyData.find((policy) => policy.id === savedPolicyID && policy.status === 'active')
      || policyData.find((policy) => policy.status === 'active' && policy.is_default)
      || policyData.find((policy) => policy.status === 'active')
    selectedPolicyID.value = nextPolicy?.id || ''
    if (!expandedModels.value.size && rows.value[0]) expandedModels.value = new Set([rows.value[0].modelID])
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

watch(selectedPolicyID, (value) => {
  if (value) localStorage.setItem('asterrouter_supply_catalog_policy', value)
})

watch(modelGroups, (groups) => {
  if (!groups.length || groups.some((group) => expandedModels.value.has(group.modelID))) return
  expandedModels.value = new Set([...expandedModels.value, groups[0].modelID])
})

onMounted(load)
</script>

<template>
  <main class="content supply-catalog-page">
    <section class="page-header">
      <div class="catalog-heading">
        <h1>{{ t('supplyCatalog.title') }}</h1>
        <p>{{ t('supplyCatalog.subtitle') }}</p>
      </div>
      <div class="catalog-heading-actions">
        <RouterLink class="catalog-policy-link" to="/console/policies/routing">{{ t('supplyCatalog.policyScope.manage') }}<ExternalLink :size="14" /></RouterLink>
        <button class="icon-button catalog-refresh" type="button" :disabled="loading" :aria-label="t('common.refresh')" :title="t('common.refresh')" @click="load">
          <RefreshCw :size="16" :class="{ spinning: loading }" />
        </button>
      </div>
    </section>

    <section class="catalog-summary" :aria-label="t('supplyCatalog.summaryLabel')">
      <span><strong>{{ summary.models }}</strong>{{ t('supplyCatalog.summary.models') }}</span>
      <span><strong>{{ summary.routes }}</strong>{{ t('supplyCatalog.summary.routes') }}</span>
      <span><strong>{{ summary.available }}</strong>{{ t('supplyCatalog.summary.available') }}</span>
      <span><strong>{{ summary.unpriced }}</strong>{{ t('supplyCatalog.summary.unpriced') }}</span>
    </section>

    <section class="policy-scope-bar" :aria-label="t('supplyCatalog.policyScope.title')">
      <Layers3 :size="19" aria-hidden="true" />
      <div class="policy-scope-select">
        <label for="supply-catalog-policy">{{ t('supplyCatalog.policyScope.actsOn') }}</label>
        <select id="supply-catalog-policy" v-model="selectedPolicyID" :disabled="!activePolicies.length">
          <option value="">{{ t('supplyCatalog.policyScope.select') }}</option>
          <option v-for="policy in activePolicies" :key="policy.id" :value="policy.id">{{ policy.name }} · {{ policy.route_group }}</option>
        </select>
      </div>
      <p v-if="selectedPolicy" class="policy-scope-copy">
        {{ t('supplyCatalog.policyScope.summary', {
          group: selectedPolicy.route_group,
          version: selectedPolicy.version,
          bindings: policyBindingCount,
          preferred: selectedPolicy.strategy.preferred_provider_account_ids.length,
          batches: selectedPolicy.strategy.resource_batches.length
        }) }}
      </p>
      <p v-else class="policy-scope-copy">{{ t('supplyCatalog.policyScope.empty') }}</p>
      <RouterLink class="button secondary compact" to="/console/policies/routing">
        {{ t('supplyCatalog.policyScope.manage') }}<ExternalLink :size="14" />
      </RouterLink>
    </section>

    <div v-if="message" class="notice success" role="status">{{ message }}</div>
    <div v-if="error" class="notice" role="alert">{{ error }}</div>

    <section class="catalog-controls" :aria-label="t('supplyCatalog.filters.title')">
      <div class="catalog-control-row">
        <div class="model-family-tabs" role="tablist" :aria-label="t('supplyCatalog.filters.family')">
          <button type="button" role="tab" :aria-selected="!familyFilter" :class="{ active: !familyFilter }" @click="familyFilter = ''">
            {{ t('supplyCatalog.filters.all') }}
          </button>
          <button v-for="family in familyOptions" :key="family" type="button" role="tab" :aria-selected="familyFilter === family" :class="{ active: familyFilter === family }" @click="familyFilter = family">
            <span class="family-mark" aria-hidden="true">{{ familyLabel(family).slice(0, 1) }}</span>{{ familyLabel(family) }}
          </button>
        </div>
        <div class="view-toggle" role="group" :aria-label="t('supplyCatalog.filters.view')">
          <button type="button" :class="{ active: viewMode === 'route' }" :aria-pressed="viewMode === 'route'" @click="viewMode = 'route'"><List :size="15" />{{ t('supplyCatalog.filters.byRoute') }}</button>
          <button type="button" :class="{ active: viewMode === 'model' }" :aria-pressed="viewMode === 'model'" @click="viewMode = 'model'"><Layers3 :size="15" />{{ t('supplyCatalog.filters.byModel') }}</button>
        </div>
      </div>
      <div class="filter-grid">
        <label class="search-box"><Search :size="17" /><input v-model="query" :placeholder="t('supplyCatalog.filters.search')" :aria-label="t('supplyCatalog.filters.searchAria')" /></label>
        <select v-model="modalityFilter" :aria-label="t('supplyCatalog.filters.modality')">
          <option value="">{{ t('supplyCatalog.filters.allModalities') }}</option>
          <option v-for="modality in modalityOptions" :key="modality" :value="modality">{{ modalityLabel(modality) }}</option>
        </select>
        <select v-model="modelFilter" :aria-label="t('supplyCatalog.filters.model')">
          <option value="">{{ t('supplyCatalog.filters.allModels') }}</option>
          <option v-for="model in modelOptions" :key="model" :value="model">{{ model }}</option>
        </select>
        <select v-model="routeGroupFilter" :aria-label="t('supplyCatalog.filters.routeGroup')">
          <option value="">{{ t('supplyCatalog.filters.allRouteGroups') }}</option>
          <option v-for="group in routeGroupOptions" :key="group" :value="group">{{ group }}</option>
        </select>
        <select v-model="providerFilter" :aria-label="t('supplyCatalog.filters.provider')">
          <option value="">{{ t('supplyCatalog.filters.allProviders') }}</option>
          <option v-for="([id, name]) in providerOptions" :key="id" :value="id">{{ name }}</option>
        </select>
        <select v-model="protocolFilter" :aria-label="t('supplyCatalog.filters.protocol')">
          <option value="">{{ t('supplyCatalog.filters.allProtocols') }}</option>
          <option v-for="protocol in protocolOptions" :key="protocol" :value="protocol">{{ protocolLabel(protocol) }}</option>
        </select>
        <select v-model="tagFilter" :aria-label="t('supplyCatalog.filters.tag')">
          <option value="">{{ t('supplyCatalog.filters.allTags') }}</option>
          <option v-for="tag in tagOptions" :key="tag" :value="tag">{{ tagLabel(tag) }}</option>
        </select>
        <label class="catalog-switch"><input v-model="schedulableOnly" type="checkbox" />{{ t('supplyCatalog.filters.schedulableOnly') }}</label>
        <button class="button tertiary compact" type="button" @click="resetFilters">{{ t('common.reset') }}</button>
      </div>
    </section>

    <section v-if="!loading && !filteredRows.length" class="catalog-empty">
      <CircleAlert :size="24" />
      <h2>{{ rows.length ? t('supplyCatalog.empty.filteredTitle') : t('supplyCatalog.empty.title') }}</h2>
      <p>{{ rows.length ? t('supplyCatalog.empty.filteredDescription') : t('supplyCatalog.empty.description') }}</p>
      <button v-if="rows.length" class="button secondary" type="button" @click="resetFilters">{{ t('common.reset') }}</button>
      <RouterLink v-else class="button secondary" to="/console/model-services/routes">{{ t('supplyCatalog.empty.manageRoutes') }}</RouterLink>
    </section>

    <section v-else-if="viewMode === 'model'" class="model-catalog-list" :aria-label="t('supplyCatalog.modelViewLabel')">
      <article v-for="group in modelGroups" :key="group.modelID" class="model-catalog-group">
        <button class="model-group-header" type="button" :aria-expanded="expandedModels.has(group.modelID)" @click="toggleModel(group.modelID)">
          <ChevronDown v-if="expandedModels.has(group.modelID)" :size="17" />
          <ChevronRight v-else :size="17" />
          <span class="model-group-name"><strong><span class="family-mark" aria-hidden="true">{{ familyLabel(group.family).slice(0, 1) }}</span>{{ group.modelID }}</strong><small>{{ group.name }} · {{ familyLabel(group.family) }} · {{ modalityLabel(group.modality) }}</small></span>
          <span class="model-group-stat"><small>{{ t('supplyCatalog.table.startPrice') }}</small><strong>{{ formatPrice(group.startPrice) }}</strong></span>
          <span class="model-group-stat"><small>{{ t('supplyCatalog.table.routeCount') }}</small><strong>{{ group.availableCount }}/{{ group.rows.length }}</strong></span>
          <span class="model-group-stat"><small>{{ t('supplyCatalog.table.bestSuccess') }}</small><strong>{{ formatPercent(group.bestSuccessRate) }}</strong></span>
          <span class="catalog-tags"><span v-for="tag in group.tags" :key="tag" class="pill">{{ tagLabel(tag) }}</span></span>
        </button>
        <div v-if="expandedModels.has(group.modelID)" class="catalog-table-scroll">
          <table class="data-table supply-route-table">
            <thead><tr><th>{{ t('supplyCatalog.table.supply') }}</th><th>{{ t('supplyCatalog.table.inputPrice') }}</th><th>{{ t('supplyCatalog.table.outputPrice') }}</th><th>{{ t('supplyCatalog.table.multiplier') }}</th><th>{{ t('supplyCatalog.table.successRate') }}</th><th>{{ t('supplyCatalog.table.latency') }}</th><th>{{ t('supplyCatalog.table.health') }}</th><th>{{ t('common.actions') }}</th></tr></thead>
            <tbody>
              <tr v-for="row in group.rows" :key="row.id" class="clickable-row" tabindex="0" @click="openDetails(row)" @keydown.enter="openDetails(row)">
                <td><div class="supply-source"><span class="provider-avatar" aria-hidden="true">{{ providerInitial(row.providerName) }}</span><span><strong>{{ row.providerName }}</strong><small>{{ row.accountName }} · {{ row.upstreamModel }}</small></span></div></td>
                <td class="num price-cell"><strong>{{ formatPrice(row.price?.uncached_input_micros_per_1m_tokens) }}</strong><span>{{ t('supplyCatalog.table.reference') }} {{ formatPrice(row.price?.reference_input_micros_per_1m_tokens) }}</span></td>
                <td class="num price-cell"><strong>{{ formatPrice(row.price?.output_micros_per_1m_tokens) }}</strong><span>{{ t('supplyCatalog.table.reference') }} {{ formatPrice(row.price?.reference_output_micros_per_1m_tokens) }}</span></td>
                <td class="num"><span class="multiplier-pill">{{ formatMultiplier(row.price?.quoted_multiplier) }}</span></td>
                <td class="success-cell"><span class="success-meter" aria-hidden="true"><i :style="{ width: `${Math.max(0, Math.min(100, (row.utilization?.demand.success_rate || 0) * 100))}%` }"></i></span><strong class="num">{{ formatPercent(row.utilization?.demand.requests ? row.utilization.demand.success_rate : null) }}</strong><span>{{ row.utilization?.demand.requests || 0 }} {{ t('supplyCatalog.table.samples') }}</span></td>
                <td class="num">{{ row.health?.latency_ms ? `${row.health.latency_ms} ms` : '—' }}</td>
                <td><span class="pill" :class="healthClass(row)">{{ healthLabel(row) }}</span></td>
                <td class="catalog-actions" @click.stop @keydown.stop>
                  <button class="button secondary compact preferred-action" :class="{ active: isPreferred(row) }" type="button" :disabled="policyActionDisabled(row)" :title="actionHint(row) || (isPreferred(row) ? t('supplyCatalog.actions.removePreferred') : t('supplyCatalog.actions.addPreferred'))" :aria-label="isPreferred(row) ? t('supplyCatalog.actions.removePreferredFor', { account: row.accountName }) : t('supplyCatalog.actions.addPreferredFor', { account: row.accountName })" @click="togglePreferred(row)"><Star :size="14" :fill="isPreferred(row) ? 'currentColor' : 'none'" />{{ isPreferred(row) ? t('supplyCatalog.actions.preferred') : t('supplyCatalog.actions.addPreferred') }}</button>
                  <select :value="batchIndex(row) ?? ''" :disabled="policyActionDisabled(row)" :title="actionHint(row)" :aria-label="t('supplyCatalog.actions.batchFor', { account: row.accountName })" @change="changeBatch(row, $event)">
                    <option value="">{{ t('supplyCatalog.actions.notInBatch') }}</option>
                    <option v-for="(batch, index) in selectedPolicy?.strategy.resource_batches || []" :key="`${index}-${batch.name}`" :value="index">{{ index + 1 }}. {{ batch.name }}</option>
                    <option v-if="!selectedPolicy?.strategy.resource_batches.length" value="__new">{{ t('supplyCatalog.actions.createPrimaryBatch') }}</option>
                  </select>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </article>
    </section>

    <section v-else class="catalog-table-scroll route-catalog-table" :aria-label="t('supplyCatalog.routeViewLabel')">
      <table class="data-table supply-route-table">
        <thead><tr><th>{{ t('supplyCatalog.table.model') }}</th><th>{{ t('supplyCatalog.table.supply') }}</th><th>{{ t('supplyCatalog.table.inputPrice') }}</th><th>{{ t('supplyCatalog.table.outputPrice') }}</th><th>{{ t('supplyCatalog.table.multiplier') }}</th><th>{{ t('supplyCatalog.table.successRate') }}</th><th>{{ t('supplyCatalog.table.latency') }}</th><th>{{ t('supplyCatalog.table.tags') }}</th><th>{{ t('common.actions') }}</th></tr></thead>
        <tbody>
          <tr v-for="row in filteredRows" :key="row.id" class="clickable-row" tabindex="0" @click="openDetails(row)" @keydown.enter="openDetails(row)">
            <td><strong>{{ row.modelID }}</strong><span>{{ row.routeGroup }} · {{ row.upstreamFormat }}</span></td>
            <td><div class="supply-source"><span class="provider-avatar" aria-hidden="true">{{ providerInitial(row.providerName) }}</span><span><strong>{{ row.providerName }}</strong><small>{{ row.accountName }} · {{ row.upstreamModel }}</small></span></div></td>
            <td class="num price-cell"><strong>{{ formatPrice(row.price?.uncached_input_micros_per_1m_tokens) }}</strong><span>{{ t('supplyCatalog.table.reference') }} {{ formatPrice(row.price?.reference_input_micros_per_1m_tokens) }}</span></td>
            <td class="num price-cell"><strong>{{ formatPrice(row.price?.output_micros_per_1m_tokens) }}</strong><span>{{ t('supplyCatalog.table.reference') }} {{ formatPrice(row.price?.reference_output_micros_per_1m_tokens) }}</span></td>
            <td class="num"><span class="multiplier-pill">{{ formatMultiplier(row.price?.quoted_multiplier) }}</span></td>
            <td class="success-cell"><span class="success-meter" aria-hidden="true"><i :style="{ width: `${Math.max(0, Math.min(100, (row.utilization?.demand.success_rate || 0) * 100))}%` }"></i></span><strong class="num">{{ formatPercent(row.utilization?.demand.requests ? row.utilization.demand.success_rate : null) }}</strong><span>{{ row.utilization?.demand.requests || 0 }} {{ t('supplyCatalog.table.samples') }}</span></td>
            <td class="num">{{ row.health?.latency_ms ? `${row.health.latency_ms} ms` : '—' }}</td>
            <td><span class="catalog-tags"><span v-for="tag in row.tags" :key="tag" class="pill">{{ tagLabel(tag) }}</span></span></td>
            <td class="catalog-actions" @click.stop @keydown.stop>
              <button class="button secondary compact preferred-action" :class="{ active: isPreferred(row) }" type="button" :disabled="policyActionDisabled(row)" :title="actionHint(row) || (isPreferred(row) ? t('supplyCatalog.actions.removePreferred') : t('supplyCatalog.actions.addPreferred'))" :aria-label="isPreferred(row) ? t('supplyCatalog.actions.removePreferredFor', { account: row.accountName }) : t('supplyCatalog.actions.addPreferredFor', { account: row.accountName })" @click="togglePreferred(row)"><Star :size="14" :fill="isPreferred(row) ? 'currentColor' : 'none'" />{{ isPreferred(row) ? t('supplyCatalog.actions.preferred') : t('supplyCatalog.actions.addPreferred') }}</button>
              <select :value="batchIndex(row) ?? ''" :disabled="policyActionDisabled(row)" :title="actionHint(row)" :aria-label="t('supplyCatalog.actions.batchFor', { account: row.accountName })" @change="changeBatch(row, $event)">
                <option value="">{{ t('supplyCatalog.actions.notInBatch') }}</option>
                <option v-for="(batch, index) in selectedPolicy?.strategy.resource_batches || []" :key="`${index}-${batch.name}`" :value="index">{{ index + 1 }}. {{ batch.name }}</option>
                <option v-if="!selectedPolicy?.strategy.resource_batches.length" value="__new">{{ t('supplyCatalog.actions.createPrimaryBatch') }}</option>
              </select>
            </td>
          </tr>
        </tbody>
      </table>
    </section>

    <section class="mobile-route-list" :aria-label="t('supplyCatalog.routeViewLabel')">
      <article v-for="row in filteredRows" :key="row.id" class="mobile-route-item" tabindex="0" @click="openDetails(row)" @keydown.enter="openDetails(row)" @keydown.space.prevent="openDetails(row)">
        <header><div><strong>{{ row.modelID }}</strong><span>{{ row.providerName }} · {{ row.accountName }}</span></div><span class="pill" :class="healthClass(row)">{{ healthLabel(row) }}</span></header>
        <dl><div><dt>{{ t('supplyCatalog.table.inputPrice') }}</dt><dd><strong>{{ formatPrice(row.price?.uncached_input_micros_per_1m_tokens) }}</strong><span>{{ t('supplyCatalog.table.reference') }} {{ formatPrice(row.price?.reference_input_micros_per_1m_tokens) }}</span></dd></div><div><dt>{{ t('supplyCatalog.table.outputPrice') }}</dt><dd><strong>{{ formatPrice(row.price?.output_micros_per_1m_tokens) }}</strong><span>{{ t('supplyCatalog.table.reference') }} {{ formatPrice(row.price?.reference_output_micros_per_1m_tokens) }}</span></dd></div><div><dt>{{ t('supplyCatalog.table.multiplier') }}</dt><dd>{{ formatMultiplier(row.price?.quoted_multiplier) }}</dd></div><div><dt>{{ t('supplyCatalog.table.successRate') }}</dt><dd>{{ formatPercent(row.utilization?.demand.requests ? row.utilization.demand.success_rate : null) }}</dd></div><div><dt>{{ t('supplyCatalog.table.latency') }}</dt><dd>{{ row.health?.latency_ms ? `${row.health.latency_ms} ms` : '—' }}</dd></div></dl>
        <div class="catalog-tags"><span v-for="tag in row.tags" :key="tag" class="pill">{{ tagLabel(tag) }}</span></div>
        <footer @click.stop @keydown.stop>
          <button class="icon-button" :class="{ active: isPreferred(row) }" type="button" :disabled="policyActionDisabled(row)" :aria-label="isPreferred(row) ? t('supplyCatalog.actions.removePreferredFor', { account: row.accountName }) : t('supplyCatalog.actions.addPreferredFor', { account: row.accountName })" @click="togglePreferred(row)"><Star :size="16" :fill="isPreferred(row) ? 'currentColor' : 'none'" /></button>
          <select :value="batchIndex(row) ?? ''" :disabled="policyActionDisabled(row)" :aria-label="t('supplyCatalog.actions.batchFor', { account: row.accountName })" @change="changeBatch(row, $event)">
            <option value="">{{ t('supplyCatalog.actions.notInBatch') }}</option>
            <option v-for="(batch, index) in selectedPolicy?.strategy.resource_batches || []" :key="`${index}-${batch.name}`" :value="index">{{ index + 1 }}. {{ batch.name }}</option>
            <option v-if="!selectedPolicy?.strategy.resource_batches.length" value="__new">{{ t('supplyCatalog.actions.createPrimaryBatch') }}</option>
          </select>
        </footer>
      </article>
    </section>

    <div v-if="selectedRow" class="modal-backdrop" @click.self="selectedRouteID = ''">
      <section class="modal-card supply-detail-dialog" role="dialog" aria-modal="true" :aria-labelledby="`supply-detail-${selectedRow.id}`">
        <header class="modal-header">
          <div><p class="detail-eyebrow">{{ selectedRow.providerName }} · {{ selectedRow.accountName }}</p><h2 :id="`supply-detail-${selectedRow.id}`">{{ selectedRow.modelID }}</h2><p>{{ selectedRow.upstreamModel }} · {{ selectedRow.upstreamFormat }}</p></div>
          <button class="icon-button" type="button" :aria-label="t('common.close')" @click="selectedRouteID = ''"><X :size="18" /></button>
        </header>
        <div class="modal-body supply-detail-body">
          <section class="detail-section">
            <h3><Activity :size="17" />{{ t('supplyCatalog.detail.identity') }}</h3>
            <dl class="detail-grid">
              <div><dt>{{ t('supplyCatalog.detail.routeID') }}</dt><dd><code>{{ selectedRow.id }}</code></dd></div>
              <div><dt>{{ t('supplyCatalog.detail.routeGroup') }}</dt><dd>{{ selectedRow.routeGroup }}</dd></div>
              <div><dt>{{ t('supplyCatalog.detail.providerType') }}</dt><dd>{{ selectedRow.providerType }}</dd></div>
              <div><dt>{{ t('supplyCatalog.detail.priorityWeight') }}</dt><dd>{{ selectedRow.priority }} / {{ selectedRow.weight }}</dd></div>
            </dl>
          </section>
          <section class="detail-section">
            <h3><Gauge :size="17" />{{ t('supplyCatalog.detail.price') }}</h3>
            <dl class="detail-grid">
              <div><dt>{{ t('supplyCatalog.table.inputPrice') }}</dt><dd class="num">{{ formatPrice(selectedRow.price?.uncached_input_micros_per_1m_tokens) }}</dd></div>
              <div><dt>{{ t('supplyCatalog.table.outputPrice') }}</dt><dd class="num">{{ formatPrice(selectedRow.price?.output_micros_per_1m_tokens) }}</dd></div>
              <div><dt>{{ t('supplyCatalog.detail.referenceInput') }}</dt><dd class="num">{{ formatPrice(selectedRow.price?.reference_input_micros_per_1m_tokens) }}</dd></div>
              <div><dt>{{ t('supplyCatalog.detail.referenceOutput') }}</dt><dd class="num">{{ formatPrice(selectedRow.price?.reference_output_micros_per_1m_tokens) }}</dd></div>
              <div><dt>{{ t('supplyCatalog.detail.cacheRead') }}</dt><dd class="num">{{ formatPrice(selectedRow.price?.cache_read_micros_per_1m_tokens) }}</dd></div>
              <div><dt>{{ t('supplyCatalog.detail.cacheWrite') }}</dt><dd class="num">{{ formatPrice(selectedRow.price?.cache_write_5m_micros_per_1m_tokens) }}</dd></div>
              <div><dt>{{ t('supplyCatalog.detail.requestPrice') }}</dt><dd class="num">{{ formatPrice(selectedRow.price?.request_micros) }}</dd></div>
              <div><dt>{{ t('supplyCatalog.detail.quotedMultiplier') }}</dt><dd class="num">{{ formatMultiplier(selectedRow.price?.quoted_multiplier) }}</dd></div>
              <div><dt>{{ t('supplyCatalog.detail.rechargeMultiplier') }}</dt><dd class="num">{{ formatMultiplier(selectedRow.price?.recharge_multiplier) }}</dd></div>
              <div><dt>{{ t('supplyCatalog.detail.priceSource') }}</dt><dd>{{ selectedRow.price?.source_kind || '—' }}</dd></div>
              <div><dt>{{ t('supplyCatalog.detail.priceConfidence') }}</dt><dd>{{ selectedRow.price?.confidence || '—' }}</dd></div>
            </dl>
            <p class="detail-note">{{ t('supplyCatalog.detail.priceNote') }}</p>
          </section>
          <section class="detail-section">
            <h3><Activity :size="17" />{{ t('supplyCatalog.detail.evidence') }}</h3>
            <dl class="detail-grid">
              <div><dt>{{ t('supplyCatalog.table.health') }}</dt><dd><span class="pill" :class="healthClass(selectedRow)">{{ healthLabel(selectedRow) }}</span></dd></div>
              <div><dt>{{ t('supplyCatalog.table.latency') }}</dt><dd class="num">{{ selectedRow.health?.latency_ms ? `${selectedRow.health.latency_ms} ms` : '—' }}</dd></div>
              <div><dt>{{ t('supplyCatalog.table.successRate') }}</dt><dd class="num">{{ formatPercent(selectedRow.utilization?.demand.requests ? selectedRow.utilization.demand.success_rate : null) }}</dd></div>
              <div><dt>{{ t('supplyCatalog.detail.requests24h') }}</dt><dd class="num">{{ selectedRow.utilization?.demand.requests || 0 }}</dd></div>
              <div><dt>{{ t('supplyCatalog.detail.fallbackRate') }}</dt><dd class="num">{{ formatPercent(selectedRow.utilization?.demand.requests ? selectedRow.utilization.demand.fallback_rate : null) }}</dd></div>
              <div><dt>{{ t('supplyCatalog.detail.lastChecked') }}</dt><dd>{{ formatDate(selectedRow.health?.checked_at) }}</dd></div>
            </dl>
            <p v-if="selectedRow.health?.message" class="detail-note">{{ selectedRow.health.message }}</p>
          </section>
          <section class="detail-section detail-policy-section">
            <h3><Layers3 :size="17" />{{ t('supplyCatalog.detail.policyUse') }}</h3>
            <p>{{ selectedPolicy ? t('supplyCatalog.detail.policySummary', { name: selectedPolicy.name, group: selectedPolicy.route_group }) : t('supplyCatalog.actions.selectPolicyFirst') }}</p>
            <div class="detail-policy-actions">
              <button class="button secondary" type="button" :disabled="policyActionDisabled(selectedRow)" @click="togglePreferred(selectedRow)"><Star :size="16" :fill="isPreferred(selectedRow) ? 'currentColor' : 'none'" />{{ isPreferred(selectedRow) ? t('supplyCatalog.actions.removePreferred') : t('supplyCatalog.actions.addPreferred') }}</button>
              <label><span>{{ t('supplyCatalog.actions.batch') }}</span><select :value="batchIndex(selectedRow) ?? ''" :disabled="policyActionDisabled(selectedRow)" :aria-label="t('supplyCatalog.actions.batchFor', { account: selectedRow.accountName })" @change="changeBatch(selectedRow, $event)"><option value="">{{ t('supplyCatalog.actions.notInBatch') }}</option><option v-for="(batch, index) in selectedPolicy?.strategy.resource_batches || []" :key="`${index}-${batch.name}`" :value="index">{{ index + 1 }}. {{ batch.name }}</option><option v-if="!selectedPolicy?.strategy.resource_batches.length" value="__new">{{ t('supplyCatalog.actions.createPrimaryBatch') }}</option></select></label>
              <span class="pill">{{ batchLabel(selectedRow) }}</span>
            </div>
            <p v-if="actionHint(selectedRow)" class="detail-note warning">{{ actionHint(selectedRow) }}</p>
          </section>
        </div>
        <footer class="modal-footer detail-footer">
          <RouterLink class="button tertiary" to="/console/model-services/accounts">{{ t('supplyCatalog.detail.manageAccount') }}</RouterLink>
          <RouterLink class="button tertiary" to="/console/model-services/effective-pricing">{{ t('supplyCatalog.detail.reviewPricing') }}</RouterLink>
          <RouterLink class="button secondary" to="/console/model-services/simulator">{{ t('supplyCatalog.detail.openSimulator') }}</RouterLink>
        </footer>
      </section>
    </div>
  </main>
</template>

<style scoped>
.supply-catalog-page { display: grid; grid-template-columns: minmax(0, 1fr); min-width: 0; gap: 18px; overflow-x: clip; }
.catalog-summary { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); border-block: 1px solid var(--border); }
.catalog-summary span { display: flex; align-items: baseline; gap: 8px; padding: 14px 18px; color: var(--muted); font-size: 13px; }
.catalog-summary span + span { border-left: 1px solid var(--border); }
.catalog-summary strong { color: var(--text); font-size: 22px; }
.policy-scope-bar { display: grid; grid-template-columns: 20px minmax(260px, 430px) minmax(220px, 1fr) auto; align-items: center; gap: 14px; min-width: 0; padding: 14px 16px; border: 1px solid var(--border); border-radius: 6px; background: var(--surface); }
.policy-scope-select { display: grid; min-width: 0; gap: 5px; }
.policy-scope-select label { font-size: 13px; font-weight: 600; white-space: nowrap; }
.policy-scope-select select { width: 100%; min-width: 0; max-width: 100%; }
.policy-scope-copy { min-width: 0; margin: 0; color: var(--muted); font-size: 12px; line-height: 1.5; overflow-wrap: anywhere; }
.catalog-controls { display: grid; gap: 12px; }
.catalog-control-row { display: flex; align-items: center; justify-content: space-between; gap: 14px; }
.model-family-tabs, .view-toggle { display: inline-flex; align-items: center; gap: 2px; padding: 3px; border: 1px solid var(--border); border-radius: 6px; background: var(--surface-muted); overflow-x: auto; }
.model-family-tabs button, .view-toggle button { display: inline-flex; align-items: center; justify-content: center; gap: 6px; min-height: 30px; padding: 5px 10px; border: 0; border-radius: 4px; background: transparent; color: var(--muted); font: inherit; font-size: 12px; white-space: nowrap; cursor: pointer; }
.model-family-tabs button.active, .view-toggle button.active { background: var(--surface); color: var(--text); box-shadow: 0 0 0 1px var(--border); }
.family-mark { display: inline-grid; place-items: center; width: 20px; height: 20px; flex: 0 0 20px; border: 1px solid var(--border); border-radius: 50%; background: var(--surface); color: var(--text); font-size: 10px; font-weight: 700; }
.filter-grid { display: grid; grid-template-columns: minmax(260px, 2fr) repeat(4, minmax(130px, 1fr)); align-items: center; gap: 9px; }
.filter-grid > * { min-width: 0; }
.filter-grid select { width: 100%; max-width: 100%; }
.filter-grid .search-box input { min-width: 0; }
.filter-grid select, .catalog-actions select, .detail-policy-actions select { min-height: 34px; }
.catalog-switch { display: inline-flex; align-items: center; gap: 7px; color: var(--text); font-size: 13px; white-space: nowrap; }
.catalog-empty { display: grid; justify-items: center; gap: 10px; padding: 56px 20px; border-block: 1px solid var(--border); text-align: center; color: var(--muted); }
.catalog-empty h2, .catalog-empty p { margin: 0; }
.catalog-empty h2 { color: var(--text); font-size: 17px; }
.model-catalog-list { border-top: 1px solid var(--border); }
.model-catalog-group { border-bottom: 1px solid var(--border); }
.model-group-header { display: grid; grid-template-columns: 20px minmax(220px, 1fr) repeat(3, minmax(90px, 130px)) minmax(160px, auto); align-items: center; width: 100%; gap: 14px; min-height: 68px; padding: 12px 14px; border: 0; background: transparent; color: inherit; text-align: left; cursor: pointer; }
.model-group-header:hover { background: var(--surface-hover); }
.model-group-name, .model-group-stat { display: grid; gap: 3px; min-width: 0; }
.model-group-name strong { display: flex; align-items: center; gap: 8px; }
.model-group-name strong, .model-group-name small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.model-group-name small, .model-group-stat small { color: var(--muted); font-size: 11px; }
.model-group-stat strong { font-size: 13px; }
.catalog-table-scroll { max-width: 100%; overflow-x: auto; border-top: 1px solid var(--border); }
.route-catalog-table { border: 1px solid var(--border); border-radius: 6px; }
.supply-route-table { min-width: 1080px; }
.supply-route-table td strong, .supply-route-table td span { display: block; }
.supply-route-table td > span:not(.pill):not(.catalog-tags) { margin-top: 3px; color: var(--muted); font-size: 11px; }
.clickable-row { cursor: pointer; }
.clickable-row:focus-visible { outline: 2px solid var(--primary); outline-offset: -2px; }
.catalog-tags { display: flex; flex-wrap: wrap; gap: 5px; }
.catalog-tags .pill { font-size: 10px; }
.price-cell strong { font-size: 13px; }
.supply-route-table td .multiplier-pill { display: inline-flex; width: fit-content; padding: 3px 7px; border-radius: 999px; background: color-mix(in srgb, var(--success) 12%, var(--surface)); color: var(--success); font-weight: 700; }
.success-cell { min-width: 150px; }
.supply-route-table td.success-cell strong, .supply-route-table td.success-cell > span { display: inline-flex; }
.supply-route-table td.success-cell > span:last-child { display: block; margin-top: 3px; color: var(--muted); font-size: 11px; }
.success-meter { width: 48px; height: 7px; margin-right: 7px; overflow: hidden; border-radius: 2px; background: var(--surface-muted); vertical-align: middle; }
.success-meter i { display: block; height: 100%; background: var(--success); }
.catalog-actions { display: flex; align-items: center; justify-content: flex-end; gap: 6px; min-width: 250px; }
.catalog-actions .icon-button.active { color: var(--warning, #a66700); background: var(--surface-muted); }
.preferred-action.active { color: var(--warning, #a66700); border-color: currentColor; }
.catalog-actions select { width: 132px; }
.mobile-route-list { display: none; }
.detail-eyebrow { margin: 0 0 4px; color: var(--muted); font-size: 12px; }
.supply-detail-dialog { width: min(880px, calc(100vw - 32px)); max-height: calc(100vh - 32px); }
.supply-detail-body { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 24px; overflow-y: auto; }
.detail-section { padding: 18px 0; border-bottom: 1px solid var(--border); }
.detail-section h3 { display: flex; align-items: center; gap: 7px; margin: 0 0 14px; font-size: 14px; }
.detail-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 13px; margin: 0; }
.detail-grid div { min-width: 0; }
.detail-grid dt { margin-bottom: 4px; color: var(--muted); font-size: 11px; }
.detail-grid dd { margin: 0; overflow-wrap: anywhere; font-size: 13px; }
.detail-note { margin: 14px 0 0; color: var(--muted); font-size: 12px; line-height: 1.55; }
.detail-note.warning { color: var(--warning, #8a5a00); }
.detail-policy-section { grid-column: 1 / -1; }
.detail-policy-section > p { color: var(--muted); font-size: 13px; }
.detail-policy-actions { display: flex; align-items: end; gap: 10px; flex-wrap: wrap; }
.detail-policy-actions label { display: grid; gap: 5px; min-width: 220px; font-size: 11px; color: var(--muted); }
.detail-footer { flex-wrap: wrap; }
.num { font-variant-numeric: tabular-nums; }
.spinning { animation: spin 0.8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }

@media (max-width: 1100px) {
  .filter-grid { grid-template-columns: minmax(220px, 1fr) repeat(2, minmax(130px, 1fr)); }
  .policy-scope-bar { grid-template-columns: 20px minmax(260px, 1fr) auto; align-items: start; }
  .policy-scope-copy { grid-column: 2 / -1; }
  .model-group-header { grid-template-columns: 20px minmax(200px, 1fr) repeat(2, minmax(90px, 120px)); }
  .model-group-header .model-group-stat:nth-of-type(4), .model-group-header .catalog-tags { display: none; }
}

@media (min-width: 721px) and (max-width: 1300px) {
  .supply-route-table { min-width: 880px; table-layout: fixed; }
  .model-catalog-list .supply-route-table th:nth-child(6),
  .model-catalog-list .supply-route-table td:nth-child(6),
  .route-catalog-table .supply-route-table th:nth-child(7),
  .route-catalog-table .supply-route-table td:nth-child(7),
  .route-catalog-table .supply-route-table th:nth-child(8),
  .route-catalog-table .supply-route-table td:nth-child(8) { display: none; }
  .catalog-actions { min-width: 0; gap: 5px; }
  .catalog-actions .preferred-action { padding-inline: 8px; }
  .catalog-actions select { width: 118px; }
  .model-catalog-list .supply-route-table th:nth-child(1) { width: 22%; }
  .model-catalog-list .supply-route-table th:nth-child(2),
  .model-catalog-list .supply-route-table th:nth-child(3) { width: 13%; }
  .model-catalog-list .supply-route-table th:nth-child(4) { width: 9%; }
  .model-catalog-list .supply-route-table th:nth-child(5) { width: 11%; }
  .model-catalog-list .supply-route-table th:nth-child(7) { width: 10%; }
  .model-catalog-list .supply-route-table th:nth-child(8) { width: 22%; }
  .route-catalog-table .supply-route-table th:nth-child(1),
  .route-catalog-table .supply-route-table th:nth-child(2) { width: 18%; }
  .route-catalog-table .supply-route-table th:nth-child(3),
  .route-catalog-table .supply-route-table th:nth-child(4) { width: 12%; }
  .route-catalog-table .supply-route-table th:nth-child(5) { width: 9%; }
  .route-catalog-table .supply-route-table th:nth-child(6) { width: 10%; }
  .route-catalog-table .supply-route-table th:nth-child(9) { width: 21%; }
}

@media (max-width: 720px) {
  .supply-catalog-page { gap: 14px; }
  .catalog-summary { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .catalog-summary span:nth-child(3) { border-left: 0; border-top: 1px solid var(--border); }
  .catalog-summary span:nth-child(4) { border-top: 1px solid var(--border); }
  .policy-scope-bar { display: grid; grid-template-columns: 20px minmax(0, 1fr); }
  .policy-scope-select { display: grid; gap: 6px; }
  .policy-scope-select select { width: 100%; min-width: 0; }
  .policy-scope-copy, .policy-scope-bar > .button { grid-column: 1 / -1; width: 100%; min-width: 0; }
  .catalog-control-row { align-items: stretch; flex-direction: column; }
  .model-family-tabs, .view-toggle { width: 100%; min-width: 0; max-width: 100%; }
  .model-family-tabs { overflow-x: auto; }
  .view-toggle { overflow: hidden; }
  .view-toggle button { flex: 1; }
  .filter-grid { grid-template-columns: 1fr 1fr; }
  .filter-grid .search-box { grid-column: 1 / -1; }
  .catalog-switch { min-height: 34px; }
  .model-catalog-list, .route-catalog-table { display: none; }
  .mobile-route-list { display: grid; gap: 9px; }
  .mobile-route-item { display: grid; gap: 12px; padding: 14px; border: 1px solid var(--border); border-radius: 6px; background: var(--surface); }
  .mobile-route-item:focus-visible { outline: 2px solid var(--primary); outline-offset: 2px; }
  .mobile-route-item header { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; }
  .mobile-route-item header div { display: grid; gap: 3px; min-width: 0; }
  .mobile-route-item header span { color: var(--muted); font-size: 11px; overflow-wrap: anywhere; }
  .mobile-route-item dl { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px 8px; margin: 0; }
  .mobile-route-item dl div { min-width: 0; }
  .mobile-route-item dt { color: var(--muted); font-size: 10px; }
  .mobile-route-item dd { margin: 4px 0 0; font-size: 12px; overflow-wrap: anywhere; }
  .mobile-route-item dd strong, .mobile-route-item dd span { display: block; }
  .mobile-route-item dd span { margin-top: 3px; color: var(--muted); font-size: 10px; }
  .mobile-route-item footer { display: flex; gap: 8px; }
  .mobile-route-item footer select { min-width: 0; flex: 1; }
  .supply-detail-dialog { width: calc(100vw - 16px); max-height: calc(100vh - 16px); }
  .supply-detail-body { grid-template-columns: 1fr; }
  .detail-policy-section { grid-column: auto; }
  .detail-grid { grid-template-columns: 1fr 1fr; }
  .detail-footer .button { flex: 1 1 100%; }
}

/* The model hub is a dense comparison workspace, not a card dashboard. */
.supply-catalog-page {
  width: min(1180px, 100%);
  margin-inline: auto;
  gap: 20px;
  padding-top: 30px;
  color: #172033;
}
.supply-catalog-page .page-header {
  position: relative;
  display: block;
  margin: 0;
  padding: 2px 150px 4px;
  text-align: center;
}
.supply-catalog-page .catalog-heading h1 {
  margin: 0;
  color: #111827;
  font-size: 25px;
  font-weight: 700;
  letter-spacing: 0;
}
.supply-catalog-page .catalog-heading p {
  max-width: 760px;
  margin: 7px auto 0;
  color: #7b8494;
  font-size: 12px;
  line-height: 1.65;
}
.catalog-heading-actions {
  position: absolute;
  top: 4px;
  right: 0;
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.catalog-policy-link {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: #3d79d8;
  font-size: 12px;
  white-space: nowrap;
}
.catalog-policy-link:hover { color: #2563c7; text-decoration: underline; }
.catalog-refresh { width: 30px; height: 30px; border: 1px solid #e5e8ee; border-radius: 6px; color: #8a94a5; }
.catalog-refresh:hover:not(:disabled) { background: #f4f7fb; color: #3d79d8; }
.catalog-summary { display: none; }
.supply-catalog-page .policy-scope-bar {
  grid-template-columns: 19px minmax(260px, 360px) minmax(220px, 1fr) auto;
  gap: 13px;
  padding: 11px 14px;
  border: 1px solid #e4e7ec;
  border-radius: 8px;
  background: #fff;
  box-shadow: 0 1px 2px rgb(15 23 42 / 2%);
}
.supply-catalog-page .policy-scope-bar > svg { color: #8893a4; }
.supply-catalog-page .policy-scope-select { gap: 4px; }
.supply-catalog-page .policy-scope-select label { color: #596579; font-size: 11px; font-weight: 600; }
.supply-catalog-page .policy-scope-select select,
.supply-catalog-page .filter-grid select,
.supply-catalog-page .filter-grid input {
  min-height: 34px;
  border: 1px solid #dfe3ea;
  border-radius: 5px;
  background: #fff;
  color: #2f3b4f;
  font-size: 12px;
}
.supply-catalog-page .policy-scope-copy { color: #8a94a5; font-size: 11px; }
.supply-catalog-page .policy-scope-bar > .button { min-height: 32px; border-radius: 5px; font-size: 11px; }
.supply-catalog-page .notice { margin: -5px 0 0; border-radius: 6px; font-size: 12px; }
.supply-catalog-page .catalog-controls { gap: 11px; }
.supply-catalog-page .catalog-control-row { align-items: end; gap: 18px; }
.supply-catalog-page .model-family-tabs,
.supply-catalog-page .view-toggle {
  padding: 0;
  border: 0;
  border-radius: 0;
  background: transparent;
}
.supply-catalog-page .model-family-tabs { flex: 1; overflow-x: auto; }
.supply-catalog-page .model-family-tabs button,
.supply-catalog-page .view-toggle button {
  min-height: 34px;
  padding: 5px 10px;
  border-radius: 0;
  color: #8993a3;
  font-size: 11px;
  font-weight: 600;
}
.supply-catalog-page .model-family-tabs button:hover,
.supply-catalog-page .view-toggle button:hover { color: #4a6fae; background: #f6f8fb; }
.supply-catalog-page .model-family-tabs button.active,
.supply-catalog-page .view-toggle button.active {
  color: #2563c7;
  background: transparent;
  box-shadow: inset 0 -2px 0 #3579dd;
}
.supply-catalog-page .family-mark {
  width: 19px;
  height: 19px;
  border-color: #e1e5eb;
  background: #f8fafc;
  color: #667085;
  font-size: 9px;
}
.supply-catalog-page .filter-grid {
  grid-template-columns: minmax(220px, 1.6fr) repeat(6, minmax(92px, 1fr)) max-content max-content;
  gap: 7px;
  padding: 10px;
  border: 1px solid #e6e9ee;
  border-radius: 8px;
  background: #fafbfc;
}
.supply-catalog-page .filter-grid > select,
.supply-catalog-page .filter-grid > .search-box { min-width: 0; }
.supply-catalog-page .search-box {
  min-height: 34px;
  border: 1px solid #dfe3ea;
  border-radius: 5px;
  background: #fff;
}
.supply-catalog-page .search-box svg { color: #a0a9b8; }
.supply-catalog-page .search-box input { border: 0; background: transparent; }
.supply-catalog-page .catalog-switch { min-height: 34px; padding: 0 6px; color: #667085; font-size: 11px; }
.supply-catalog-page .catalog-switch input { accent-color: #3579dd; }
.supply-catalog-page .filter-grid .button { min-height: 32px; padding-inline: 10px; border-radius: 5px; font-size: 11px; box-shadow: none; }
.supply-catalog-page .model-catalog-list,
.supply-catalog-page .route-catalog-table {
  overflow: hidden;
  border: 1px solid #e3e7ed;
  border-radius: 8px;
  background: #fff;
  box-shadow: 0 1px 2px rgb(15 23 42 / 2%);
}
.supply-catalog-page .model-catalog-list { border-top: 1px solid #e3e7ed; }
.supply-catalog-page .model-catalog-group { border-bottom-color: #e7eaf0; }
.supply-catalog-page .model-catalog-group:last-child { border-bottom: 0; }
.supply-catalog-page .model-group-header {
  grid-template-columns: 18px minmax(240px, 1fr) repeat(3, minmax(100px, 125px)) minmax(145px, auto);
  min-height: 62px;
  gap: 12px;
  padding: 10px 14px;
  background: #fff;
}
.supply-catalog-page .model-group-header:hover { background: #fafbfc; }
.supply-catalog-page .model-group-header > svg { color: #9aa4b3; }
.supply-catalog-page .model-group-name strong { color: #202b3c; font-size: 13px; font-weight: 700; }
.supply-catalog-page .model-group-name small,
.supply-catalog-page .model-group-stat small { color: #98a1af; font-size: 10px; }
.supply-catalog-page .model-group-stat strong { color: #3a4658; font-size: 12px; font-weight: 650; }
.supply-catalog-page .catalog-tags { justify-content: flex-end; }
.supply-catalog-page .catalog-tags .pill,
.supply-catalog-page .model-group-header .pill { padding: 3px 6px; border: 1px solid #dbe7f8; border-radius: 4px; background: #f4f8fe; color: #4c79b7; font-size: 9px; }
.supply-catalog-page .catalog-table-scroll { border-top-color: #eef0f3; }
.supply-catalog-page .data-table { min-width: 1100px; font-size: 11px; }
.supply-catalog-page .data-table th { padding: 9px 12px; border-bottom-color: #e7eaf0; background: #fafbfc; color: #8993a3; font-size: 10px; font-weight: 600; text-transform: none; }
.supply-catalog-page .data-table td { padding: 12px; border-bottom-color: #eef0f3; color: #5f6b7d; }
.supply-catalog-page .data-table tbody tr:hover { background: #fbfcfe; }
.supply-catalog-page .data-table td strong { color: #303b4d; font-size: 11px; font-weight: 650; }
.supply-catalog-page .data-table td > span:not(.pill):not(.catalog-tags) { color: #9aa3b2; font-size: 10px; }
.supply-source { display: flex; align-items: center; gap: 8px; min-width: 170px; }
.supply-source > span:last-child { display: grid; gap: 2px; min-width: 0; }
.supply-source small { overflow: hidden; color: #9aa3b2; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.provider-avatar { display: inline-grid; width: 24px; height: 24px; flex: 0 0 24px; place-items: center; border: 1px solid #e1e6ee; border-radius: 50%; background: #f7f9fc; color: #667085; font-size: 10px; font-weight: 700; }
.supply-catalog-page .price-cell strong { color: #273449; font-size: 12px; font-variant-numeric: tabular-nums; }
.supply-catalog-page .price-cell span { font-variant-numeric: tabular-nums; }
.supply-catalog-page .multiplier-pill { padding: 3px 6px; border: 1px solid #d6eddf; border-radius: 4px; background: #f0fbf4; color: #37a361; font-size: 10px; font-weight: 650; }
.supply-catalog-page .success-cell { min-width: 142px; }
.supply-catalog-page .success-meter { width: 54px; height: 5px; margin-right: 6px; border-radius: 1px; background: #edf0f3; }
.supply-catalog-page .success-meter i { background: #38a866; }
.supply-catalog-page .success-cell strong { color: #36a260; font-size: 11px; }
.supply-catalog-page .success-cell > span:last-child { color: #a0a8b5; font-size: 10px; }
.supply-catalog-page .catalog-actions { min-width: 225px; gap: 6px; }
.supply-catalog-page .catalog-actions .preferred-action { min-height: 30px; padding-inline: 9px; border: 1px solid #d7e5fb; border-radius: 5px; background: #fff; color: #3478d3; box-shadow: none; font-size: 10px; }
.supply-catalog-page .catalog-actions .preferred-action.active { border-color: #3478d3; background: #3478d3; color: #fff; }
.supply-catalog-page .catalog-actions select { width: 96px; min-height: 30px; border-color: #dfe3ea; border-radius: 5px; font-size: 10px; }
.supply-catalog-page .pill.status-success { border-color: #d4eedf; background: #f0fbf4; color: #32a05d; }
.supply-catalog-page .pill.status-warning { border-color: #f3e5be; background: #fffbef; color: #ad7b16; }
.supply-catalog-page .pill.status-danger { border-color: #f3d2d2; background: #fff6f6; color: #c44d4d; }

:global(:root[data-theme="dark"]) .supply-catalog-page { color: var(--text); }
:global(:root[data-theme="dark"]) .supply-catalog-page .catalog-heading h1,
:global(:root[data-theme="dark"]) .supply-catalog-page .model-group-name strong,
:global(:root[data-theme="dark"]) .supply-catalog-page .model-group-stat strong,
:global(:root[data-theme="dark"]) .supply-catalog-page .data-table td strong,
:global(:root[data-theme="dark"]) .supply-catalog-page .price-cell strong { color: var(--text); }
:global(:root[data-theme="dark"]) .supply-catalog-page .catalog-heading p,
:global(:root[data-theme="dark"]) .supply-catalog-page .policy-scope-copy,
:global(:root[data-theme="dark"]) .supply-catalog-page .model-group-name small,
:global(:root[data-theme="dark"]) .supply-catalog-page .model-group-stat small,
:global(:root[data-theme="dark"]) .supply-catalog-page .supply-source small { color: var(--text-muted); }
:global(:root[data-theme="dark"]) .supply-catalog-page .policy-scope-bar,
:global(:root[data-theme="dark"]) .supply-catalog-page .model-catalog-list,
:global(:root[data-theme="dark"]) .supply-catalog-page .route-catalog-table,
:global(:root[data-theme="dark"]) .supply-catalog-page .model-group-header,
:global(:root[data-theme="dark"]) .supply-catalog-page .policy-scope-select select,
:global(:root[data-theme="dark"]) .supply-catalog-page .filter-grid select,
:global(:root[data-theme="dark"]) .supply-catalog-page .filter-grid input,
:global(:root[data-theme="dark"]) .supply-catalog-page .search-box { border-color: var(--border); background: var(--surface); color: var(--text-secondary); }
:global(:root[data-theme="dark"]) .supply-catalog-page .filter-grid,
:global(:root[data-theme="dark"]) .supply-catalog-page .data-table th { border-color: var(--border); background: var(--surface-subtle); }
:global(:root[data-theme="dark"]) .supply-catalog-page .model-group-header:hover,
:global(:root[data-theme="dark"]) .supply-catalog-page .data-table tbody tr:hover { background: var(--surface-hover); }
:global(:root[data-theme="dark"]) .supply-catalog-page .family-mark,
:global(:root[data-theme="dark"]) .supply-catalog-page .provider-avatar { border-color: var(--border); background: var(--surface-subtle); color: var(--text-secondary); }
:global(:root[data-theme="dark"]) .supply-catalog-page .data-table td,
:global(:root[data-theme="dark"]) .supply-catalog-page .model-catalog-group,
:global(:root[data-theme="dark"]) .supply-catalog-page .catalog-table-scroll { border-color: var(--border); }

@media (max-width: 1200px) {
  .supply-catalog-page .page-header { padding-inline: 92px; }
  .supply-catalog-page .filter-grid { grid-template-columns: minmax(240px, 1.7fr) repeat(3, minmax(120px, 1fr)); }
  .supply-catalog-page .catalog-switch { grid-column: span 1; }
}
@media (min-width: 721px) and (max-width: 1300px) {
  .supply-catalog-page .supply-route-table { min-width: 0; width: 100%; table-layout: fixed; }
  .supply-catalog-page .catalog-actions { min-width: 0; width: 190px; }
  .supply-catalog-page .catalog-actions .preferred-action { padding-inline: 7px; }
  .supply-catalog-page .catalog-actions select { width: 88px; }
}
@media (max-width: 720px) {
  .supply-catalog-page { padding: 20px 14px 32px; gap: 14px; }
  .supply-catalog-page .page-header { padding: 0 42px; }
  .supply-catalog-page .catalog-heading h1 { font-size: 21px; }
  .supply-catalog-page .catalog-heading p { font-size: 11px; }
  .catalog-heading-actions { top: 0; right: 0; }
  .catalog-policy-link { display: none; }
  .supply-catalog-page .policy-scope-bar { grid-template-columns: 19px minmax(0, 1fr); padding: 11px; }
  .supply-catalog-page .policy-scope-copy,
  .supply-catalog-page .policy-scope-bar > .button { grid-column: 1 / -1; }
  .supply-catalog-page .filter-grid { grid-template-columns: 1fr 1fr; padding: 8px; }
  .supply-catalog-page .filter-grid .search-box { grid-column: 1 / -1; }
  .supply-catalog-page .model-family-tabs { max-width: calc(100vw - 28px); }
  .supply-catalog-page .view-toggle { width: 100%; justify-content: center; }
  .supply-catalog-page .model-group-header { grid-template-columns: 18px minmax(0, 1fr) auto; gap: 8px; padding: 11px; }
  .supply-catalog-page .model-group-stat:nth-of-type(3),
  .supply-catalog-page .model-group-stat:nth-of-type(4),
  .supply-catalog-page .model-group-header .catalog-tags { display: none; }
  .supply-catalog-page .mobile-route-item { border-color: #e3e7ed; border-radius: 8px; box-shadow: 0 1px 2px rgb(15 23 42 / 2%); }
}
</style>
