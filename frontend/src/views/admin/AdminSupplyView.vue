<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Activity, AlertTriangle, Boxes, ExternalLink, Gauge, KeyRound, RadioTower, RefreshCw, Route, WalletCards } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { getCapacityRecommendations, getSupplyUtilization } from '@/api/control'
import type { CapacityRecommendation, CapacityRecommendationReport, SupplyDimension, SupplyUtilizationReport, SupplyUtilizationRow, SupplyWatermark } from '@/types'

type WindowPreset = 24 | 168 | 720

const { t } = useI18n()
const loading = ref(false)
const error = ref('')
const windowHours = ref<WindowPreset>(24)
const activeDimension = ref<SupplyDimension>('provider_account')
const report = ref<SupplyUtilizationReport | null>(null)
const recommendations = ref<CapacityRecommendationReport | null>(null)

const dimensionTabs = computed(() => [
  { value: 'provider_account' as const, label: t('supply.dimensions.providerAccount'), icon: RadioTower },
  { value: 'route_group' as const, label: t('supply.dimensions.routeGroup'), icon: Route },
  { value: 'published_model' as const, label: t('supply.dimensions.publishedModel'), icon: Boxes },
  { value: 'application' as const, label: t('supply.dimensions.application'), icon: KeyRound }
])

const visibleRows = computed(() => (report.value?.rows || []).filter((row) => row.dimension === activeDimension.value))
const modelRows = computed(() => (report.value?.rows || []).filter((row) => row.dimension === 'published_model'))
const totalRequests = computed(() => modelRows.value.reduce((total, row) => total + row.demand.requests, 0))
const capacityRejected = computed(() => modelRows.value.reduce((total, row) => total + row.demand.capacity_rejected_requests, 0))
const supplyAttention = computed(() => (report.value?.rows || []).filter((row) => ['saturated', 'degraded', 'stranded'].includes(row.capacity_status)).length)
const actionableRecommendations = computed(() => recommendations.value?.summary.actionable || 0)
const metrics = computed(() => [
  { label: t('supply.metrics.requests'), value: formatNumber(totalRequests.value), sub: t('supply.metrics.window', { hours: windowHours.value }), icon: Activity },
  { label: t('supply.metrics.capacityRejected'), value: formatNumber(capacityRejected.value), sub: t('supply.metrics.rejectedSub'), icon: AlertTriangle },
  { label: t('supply.metrics.attention'), value: formatNumber(supplyAttention.value), sub: t('supply.metrics.attentionSub'), icon: Gauge },
  { label: t('supply.metrics.recommendations'), value: formatNumber(actionableRecommendations.value), sub: recommendations.value?.mode || 'observe_only', icon: WalletCards }
])

function formatNumber(value: number): string {
  return new Intl.NumberFormat().format(value || 0)
}

function formatPercent(value: number): string {
  return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format((value || 0) * 100)}%`
}

function formatDate(value?: string): string {
  return value ? new Intl.DateTimeFormat(undefined, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(value)) : '-'
}

function formatTokens(row: SupplyUtilizationRow): string {
  return formatNumber(row.tokens.input_tokens + row.tokens.output_tokens)
}

function formatCost(row: SupplyUtilizationRow): string {
  if (!row.costs.length) return '-'
  return row.costs.map((cost) => new Intl.NumberFormat(undefined, { style: 'currency', currency: cost.currency || 'USD', maximumFractionDigits: 2 }).format(cost.cost_micros / 1_000_000)).join(' · ')
}

function stateClass(state: string): string {
  if (state === 'available') return 'status-success'
  if (state === 'saturated' || state === 'stranded') return 'status-danger'
  return 'status-warning'
}

function recommendationClass(status: string): string {
  return status === 'actionable' ? 'status-warning' : 'status-muted'
}

function stateLabel(state: string): string {
  const labels: Record<string, string> = {
    available: t('supply.states.available'), degraded: t('supply.states.degraded'), saturated: t('supply.states.saturated'), idle: t('supply.states.idle'),
    stranded: t('supply.states.stranded'), unknown: t('supply.states.unknown'), no_evidence: t('supply.states.noEvidence')
  }
  return labels[state] || state
}

function evidenceStatusLabel(status: string): string {
  const labels: Record<string, string> = {
    known: t('supply.evidence.known'), unknown: t('supply.evidence.unknown'), not_applicable: t('supply.evidence.notApplicable'), not_comparable: t('supply.evidence.notComparable')
  }
  return labels[status] || status
}

function constraintLabel(value: string): string {
  const labels: Record<string, string> = {
    concurrency: t('supply.constraints.concurrency'), rpm: t('supply.constraints.rpm'), tpm: t('supply.constraints.tpm'),
    health: t('supply.constraints.health'), routing: t('supply.constraints.routing'), unknown: t('supply.constraints.unknown')
  }
  return labels[value] || value
}

function primaryWatermark(row: SupplyUtilizationRow): SupplyWatermark {
  return row.watermarks[row.primary_constraint as 'concurrency' | 'rpm' | 'tpm'] || row.watermarks.concurrency
}

function watermarkLabel(row: SupplyUtilizationRow): string {
  const watermark = primaryWatermark(row)
  if (watermark.status !== 'known') return evidenceStatusLabel(watermark.status)
  return `${formatNumber(watermark.peak)} / ${formatNumber(watermark.limit)} · ${formatPercent(watermark.peak_ratio)}`
}

function watermarkWidth(row: SupplyUtilizationRow): string {
  const watermark = primaryWatermark(row)
  if (watermark.status !== 'known') return '0%'
  return `${Math.max(0, Math.min(100, watermark.peak_ratio * 100))}%`
}

function dimensionCount(dimension: SupplyDimension): number {
  return report.value?.by_dimension?.[dimension] || 0
}

function reasonLabel(code: string): string {
  const labels: Record<string, string> = {
    account_inactive: t('supply.reasons.accountInactive'), account_unschedulable: t('supply.reasons.accountUnschedulable'), cooldown_active: t('supply.reasons.cooldownActive'),
    circuit_open: t('supply.reasons.circuitOpen'), no_active_route: t('supply.reasons.noActiveRoute'), no_configured_route: t('supply.reasons.noConfiguredRoute'),
    all_route_capacity_stranded: t('supply.reasons.allRouteCapacityStranded'), evidence_gate_not_met: t('supply.reasons.evidenceGateNotMet'),
    sustained_capacity_pressure: t('supply.reasons.sustainedCapacityPressure'), capacity_rejection_observed: t('supply.reasons.capacityRejectionObserved'),
    headroom_observed: t('supply.reasons.headroomObserved'), no_capacity_rejection_observed: t('supply.reasons.noCapacityRejectionObserved'),
    no_stable_capacity_signal: t('supply.reasons.noStableCapacitySignal'), window_truncated: t('supply.reasons.windowTruncated'),
    insufficient_samples: t('supply.reasons.insufficientSamples'), unknown_capacity: t('supply.reasons.unknownCapacity'),
    unclassified_failures: t('supply.reasons.unclassifiedFailures'), provider_failures_require_classification: t('supply.reasons.providerFailuresRequireClassification'),
    health_evidence_incomplete: t('supply.reasons.healthEvidenceIncomplete'), policy_limit_is_primary: t('supply.reasons.policyLimitIsPrimary'),
    additional_observation_window_required: t('supply.reasons.additionalObservationWindowRequired'), fallback_capacity_observed: t('supply.reasons.fallbackCapacityObserved'),
    peak_below_expansion_threshold: t('supply.reasons.peakBelowExpansionThreshold'), health_coverage_complete: t('supply.reasons.healthCoverageComplete')
  }
  return labels[code] || code
}

function recommendationLabel(item: CapacityRecommendation): string {
  const labels: Record<string, string> = {
    increase_capacity: t('supply.recommendations.increaseCapacity'), defer_expansion: t('supply.recommendations.deferExpansion'), review_stranded_capacity: t('supply.recommendations.reviewStrandedCapacity')
  }
  return labels[item.type] || item.type
}

function confidenceLabel(value: string): string {
  const labels: Record<string, string> = { high: t('supply.confidence.high'), medium: t('supply.confidence.medium'), low: t('supply.confidence.low') }
  return labels[value] || value
}

function evidenceQuery(row: SupplyUtilizationRow): Record<string, string> {
  const filter = row.evidence.filter
  const query: Record<string, string> = {}
  for (const [key, value] of Object.entries({
    api_key_id: filter.api_key_id,
    gateway_principal_id: filter.gateway_principal_id,
    model: filter.model,
    provider_account_id: filter.provider_account_id,
    route_group: filter.route_group,
    gateway_model_id: filter.gateway_model_id,
    from: report.value?.window.from,
    to: report.value?.window.to
  })) {
    if (value) query[key] = value
  }
  return query
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [utilization, capacityRecommendations] = await Promise.all([
      getSupplyUtilization(windowHours.value),
      getCapacityRecommendations(windowHours.value)
    ])
    report.value = utilization
    recommendations.value = capacityRecommendations
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

function selectWindow(value: WindowPreset) {
  if (windowHours.value === value) return
  windowHours.value = value
  void load()
}

onMounted(load)
</script>

<template>
  <main class="content crud-page supply-page">
    <section class="page-header">
      <div>
        <h1>{{ t('supply.title') }}</h1>
        <p>{{ t('supply.subtitle') }}</p>
      </div>
      <div class="page-header-actions">
        <div class="supply-window-control" role="group" :aria-label="t('supply.windowLabel')">
          <button v-for="preset in ([24, 168, 720] as WindowPreset[])" :key="preset" type="button" :class="{ active: windowHours === preset }" @click="selectWindow(preset)">
            {{ preset === 24 ? t('supply.windows.day') : preset === 168 ? t('supply.windows.week') : t('supply.windows.month') }}
          </button>
        </div>
        <button class="button secondary" type="button" :disabled="loading" @click="load"><RefreshCw :size="17" />{{ t('common.refresh') }}</button>
      </div>
    </section>

    <div v-if="error" class="notice">{{ error }}</div>

    <section class="metric-grid supply-metric-grid">
      <article v-for="metric in metrics" :key="metric.label" class="metric-card">
        <span class="metric-icon"><component :is="metric.icon" :size="19" /></span>
        <div><span>{{ metric.label }}</span><strong>{{ metric.value }}</strong><small>{{ metric.sub }}</small></div>
      </article>
    </section>

    <section class="supply-tabs" :aria-label="t('supply.dimensionLabel')">
      <button v-for="tab in dimensionTabs" :key="tab.value" type="button" :class="{ active: activeDimension === tab.value }" @click="activeDimension = tab.value">
        <component :is="tab.icon" :size="16" /><span>{{ tab.label }}</span><small>{{ dimensionCount(tab.value) }}</small>
      </button>
    </section>

    <section class="panel table-panel supply-table-panel">
      <div class="panel-header supply-panel-header">
        <div><h2>{{ dimensionTabs.find((tab) => tab.value === activeDimension)?.label }}</h2><p>{{ t('supply.windowObserved', { from: formatDate(report?.window.from), to: formatDate(report?.window.to) }) }}</p></div>
        <span v-if="report?.window.truncated" class="pill status-warning">{{ t('supply.truncated') }}</span>
      </div>
      <div class="panel-body table-scroll">
        <table class="data-table supply-table">
          <thead>
            <tr>
              <th>{{ t('supply.table.resource') }}</th>
              <th>{{ t('supply.table.state') }}</th>
              <th>{{ t('supply.table.demand') }}</th>
              <th>{{ t('supply.table.reliability') }}</th>
              <th>{{ t('supply.table.usage') }}</th>
              <th>{{ t('supply.table.capacity') }}</th>
              <th>{{ t('supply.table.evidence') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in visibleRows" :key="`${row.dimension}:${row.id}`">
              <td>
                <strong>{{ row.name }}</strong>
                <span>{{ row.provider_id || row.route_group || row.gateway_principal_id || row.api_key_id || row.id }}</span>
              </td>
              <td>
                <span class="pill" :class="stateClass(row.capacity_status)">{{ stateLabel(row.capacity_status) }}</span>
                <span>{{ constraintLabel(row.primary_constraint) }}</span>
              </td>
              <td>
                <strong>{{ formatNumber(row.demand.requests) }}</strong>
                <span>{{ formatPercent(row.demand.success_rate) }} {{ t('supply.table.success') }}</span>
              </td>
              <td>
                <strong>{{ formatPercent(row.demand.fallback_rate) }} {{ t('supply.table.fallback') }}</strong>
                <span>{{ formatNumber(row.demand.http_429_requests + row.demand.http_5xx_requests) }} {{ t('supply.table.upstreamErrors') }}</span>
              </td>
              <td>
                <strong>{{ formatTokens(row) }} {{ t('supply.table.tokens') }}</strong>
                <span>{{ formatCost(row) }}</span>
              </td>
              <td>
                <strong>{{ watermarkLabel(row) }}</strong>
                <span class="supply-watermark-track"><i :class="{ warning: row.capacity_status === 'saturated' }" :style="{ width: watermarkWidth(row) }" /></span>
              </td>
              <td>
                <strong>{{ formatPercent(row.period.health_coverage) }} {{ t('supply.table.health') }}</strong>
                <RouterLink class="supply-evidence-link" :to="{ path: '/console/traces', query: evidenceQuery(row) }"><ExternalLink :size="14" />{{ t('supply.table.traces', { count: row.evidence.trace_count }) }}</RouterLink>
              </td>
            </tr>
            <tr v-if="!visibleRows.length">
              <td colspan="7" class="empty-cell">{{ loading ? t('common.loading') : t('supply.empty') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section class="panel supply-recommendations-panel">
      <div class="panel-header supply-panel-header">
        <div><h2>{{ t('supply.recommendations.title') }}</h2><p>{{ t('supply.recommendations.mode', { mode: recommendations?.mode || 'observe_only' }) }}</p></div>
        <span class="pill status-warning">{{ recommendations?.summary.inconclusive || 0 }} {{ t('supply.recommendations.inconclusive') }}</span>
      </div>
      <div class="supply-recommendations-list">
        <article v-for="item in recommendations?.items || []" :key="item.id" class="supply-recommendation-row">
          <div class="supply-recommendation-head">
            <div><strong>{{ item.target_name }}</strong><span>{{ recommendationLabel(item) }} · {{ constraintLabel(item.primary_constraint) }}</span></div>
            <div class="supply-recommendation-status"><span class="pill" :class="recommendationClass(item.status)">{{ item.status === 'actionable' ? t('supply.recommendations.actionable') : t('supply.recommendations.inconclusive') }}</span><span>{{ confidenceLabel(item.confidence) }}</span></div>
          </div>
          <div class="supply-recommendation-evidence">
            <span>{{ t('supply.recommendations.samples', { count: item.evidence.sample_count }) }}</span>
            <span>{{ t('supply.recommendations.peak', { value: formatPercent(item.evidence.peak_watermark) }) }}</span>
            <span>{{ t('supply.recommendations.capacityRejected', { count: item.evidence.capacity_rejected_requests }) }}</span>
            <span>{{ t('supply.recommendations.health', { value: formatPercent(item.evidence.health_coverage) }) }}</span>
          </div>
          <div v-if="item.reason_codes.length || item.missing_evidence.length || item.counter_evidence.length" class="supply-recommendation-codes">
            <span v-for="code in item.reason_codes" :key="`reason:${code}`" class="pill status-warning">{{ reasonLabel(code) }}</span>
            <span v-for="code in item.missing_evidence" :key="`missing:${code}`" class="pill status-danger">{{ reasonLabel(code) }}</span>
            <span v-for="code in item.counter_evidence" :key="`counter:${code}`" class="pill status-muted">{{ reasonLabel(code) }}</span>
          </div>
        </article>
        <p v-if="!(recommendations?.items || []).length" class="supply-empty-recommendations">{{ loading ? t('common.loading') : t('supply.recommendations.empty') }}</p>
      </div>
    </section>
  </main>
</template>

<style scoped>
.supply-page { gap: 16px; }
.supply-page .page-header { margin-bottom: 0; }
.supply-window-control { display: inline-flex; min-height: 40px; padding: 3px; border: 1px solid var(--border); border-radius: 7px; background: var(--surface-hover); }
.supply-window-control button { min-width: 58px; border: 0; border-radius: 5px; background: transparent; color: var(--text-muted); cursor: pointer; font-size: 12px; font-weight: 650; }
.supply-window-control button.active { background: var(--surface); color: var(--text); box-shadow: var(--shadow-sm); }
.supply-tabs { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 8px; }
.supply-tabs button { display: inline-flex; min-height: 42px; min-width: 0; align-items: center; justify-content: center; gap: 7px; padding: 0 10px; border: 1px solid var(--border); border-radius: 7px; background: var(--panel-bg); color: var(--text-muted); cursor: pointer; font-size: 12px; font-weight: 650; }
.supply-tabs button.active { border-color: var(--primary-400); background: var(--primary-50); color: var(--primary-700); }
.supply-tabs small { display: inline-grid; min-width: 20px; height: 20px; place-items: center; border-radius: 5px; background: var(--surface-hover); color: inherit; font-size: 11px; }
.supply-panel-header { justify-content: space-between; align-items: center; }
.supply-panel-header > div { display: grid; min-width: 0; gap: 3px; }
.supply-panel-header p { margin: 0; color: var(--text-muted); font-size: 12px; }
.supply-table { min-width: 1120px; }
.supply-watermark-track { display: block; width: 132px; height: 5px; margin-top: 6px; overflow: hidden; border-radius: 3px; background: var(--surface-hover); }
.supply-watermark-track i { display: block; height: 100%; border-radius: inherit; background: var(--primary-500); }
.supply-watermark-track i.warning { background: var(--warning); }
.supply-evidence-link { display: inline-flex; align-items: center; gap: 5px; margin-top: 4px; color: var(--primary-700); font-size: 11px; font-weight: 650; text-decoration: none; }
.supply-evidence-link:hover { text-decoration: underline; }
.supply-recommendations-list { display: grid; }
.supply-recommendation-row { display: grid; gap: 12px; padding: 16px 20px; border-bottom: 1px solid var(--border); }
.supply-recommendation-row:last-child { border-bottom: 0; }
.supply-recommendation-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
.supply-recommendation-head > div:first-child { display: grid; gap: 3px; min-width: 0; }
.supply-recommendation-head strong { color: var(--text); font-size: 13px; }
.supply-recommendation-head span { color: var(--text-muted); font-size: 12px; }
.supply-recommendation-status { display: inline-flex; flex: 0 0 auto; align-items: center; gap: 8px; }
.supply-recommendation-evidence { display: flex; flex-wrap: wrap; gap: 12px; color: var(--text-secondary); font-size: 12px; }
.supply-recommendation-codes { display: flex; flex-wrap: wrap; gap: 6px; }
.supply-empty-recommendations { margin: 0; padding: 22px; color: var(--text-muted); font-size: 13px; text-align: center; }
@media (max-width: 860px) {
  .supply-tabs { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .supply-recommendation-head { align-items: flex-start; flex-direction: column; }
}
@media (max-width: 620px) {
  .supply-page .page-header-actions { width: 100%; justify-content: stretch; }
  .supply-window-control, .supply-page .page-header-actions .button { flex: 1 1 auto; }
  .supply-window-control button { min-width: 0; flex: 1; }
  .supply-tabs { grid-template-columns: 1fr; }
  .supply-metric-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .supply-metric-grid .metric-card { min-height: 102px; padding: 14px; }
  .supply-metric-grid .metric-icon { width: 34px; height: 34px; flex-basis: 34px; }
  .supply-metric-grid .metric-card strong { font-size: 20px; }
}
</style>
