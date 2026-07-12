<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Download, RefreshCw, Search } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { exportGatewayTracesCSV, getGatewayTraces, getGatewayTraceSummary } from '@/api/control'
import type { GatewayTrace, GatewayTraceSummary, RecordListQuery } from '@/types'
import { datetimeLocalToISOString } from '@/utils/timeRange'

const { t } = useI18n()
const loading = ref(false)
const error = ref('')
const traces = ref<GatewayTrace[]>([])
const summary = ref<GatewayTraceSummary>({ total: 0, routed: 0, errors: 0, tokens: 0, avg_latency_ms: 0 })
const query = ref('')
const modelFilter = ref('')
const statusFilter = ref('')
const fromTime = ref('')
const toTime = ref('')
const pageSize = ref(25)
const offset = ref(0)

const filteredTraces = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return traces.value.filter((trace) => {
    if (modelFilter.value && trace.model !== modelFilter.value) return false
    if (statusFilter.value && trace.status !== statusFilter.value) return false
    if (!keyword) return true
    return [
      trace.model,
      trace.status,
      trace.error_type,
      trace.provider_id,
      trace.provider_account_id,
      trace.route_source,
      trace.route_reason,
      trace.policy_id,
      trace.policy_name,
      trace.policy_source,
      trace.policy_snapshot,
      trace.api_fingerprint,
      trace.request_summary,
      trace.response_summary
    ].some((value) => String(value || '').toLowerCase().includes(keyword))
  })
})

const modelOptions = computed(() => Array.from(new Set(traces.value.map((item) => item.model))).filter(Boolean).sort())
const statusOptions = computed(() => Array.from(new Set(traces.value.map((item) => item.status))).filter(Boolean).sort())
const pageNumber = computed(() => Math.floor(offset.value / pageSize.value) + 1)
const canPrevious = computed(() => offset.value > 0)
const canNext = computed(() => traces.value.length >= pageSize.value)

async function load() {
  loading.value = true
  error.value = ''
  try {
    const currentQuery = listQuery()
    const [traceData, summaryData] = await Promise.all([
      getGatewayTraces(currentQuery),
      getGatewayTraceSummary(currentQuery)
    ])
    traces.value = traceData
    summary.value = summaryData
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

function listQuery(): RecordListQuery {
  return {
    limit: pageSize.value,
    offset: offset.value,
    q: query.value.trim() || undefined,
    model: modelFilter.value || undefined,
    status: statusFilter.value || undefined,
    from: datetimeLocalToISOString(fromTime.value),
    to: datetimeLocalToISOString(toTime.value)
  }
}

async function exportCSV() {
  error.value = ''
  try {
    await exportGatewayTracesCSV({ ...listQuery(), limit: 5000, offset: 0 })
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  }
}

function applyFilters() {
  offset.value = 0
  void load()
}

function previousPage() {
  if (!canPrevious.value) return
  offset.value = Math.max(0, offset.value - pageSize.value)
  void load()
}

function nextPage() {
  if (!canNext.value) return
  offset.value += pageSize.value
  void load()
}

function statusClass(status: string) {
  if (status === 'forwarded' || status === 'accepted') return 'status-success'
  if (status === 'upstream_error' || status === 'error') return 'status-danger'
  return 'status-warning'
}

function formatTime(value: string): string {
  return new Date(value).toLocaleString()
}

function formatNumber(value: number): string {
  return new Intl.NumberFormat().format(value)
}

function formatPolicySource(source: string): string {
  return source ? source.replace(/_/g, ' ') : '-'
}

onMounted(load)
</script>

<template>
  <main class="content crud-page">
    <section class="page-header">
      <div>
        <h1>{{ t('admin.traces') }}</h1>
        <p>{{ t('traces.subtitle') }}</p>
      </div>
      <div class="row-actions">
        <button class="button secondary" type="button" :disabled="!filteredTraces.length" @click="exportCSV">
          <Download :size="17" />
          {{ t('common.export') }}
        </button>
        <button class="button secondary" :disabled="loading" @click="load">
          <RefreshCw :size="17" />
          {{ t('common.refresh') }}
        </button>
      </div>
    </section>

    <div class="crud-summary">
      <span><strong>{{ summary.total }}</strong>{{ t('traces.traces') }}</span>
      <span><strong>{{ summary.routed }}</strong>{{ t('traces.routed') }}</span>
      <span><strong>{{ summary.errors }}</strong>{{ t('usage.errors') }}</span>
      <span><strong>{{ formatNumber(summary.avg_latency_ms) }} ms</strong>{{ t('usage.latency') }}</span>
    </div>

    <section class="table-toolbar">
      <label class="search-box">
        <Search :size="17" />
        <input v-model="query" :placeholder="t('traces.searchPlaceholder')" @keyup.enter="applyFilters" />
      </label>
      <select v-model="modelFilter" @change="applyFilters">
        <option value="">{{ t('usage.allModels') }}</option>
        <option v-for="model in modelOptions" :key="model" :value="model">{{ model }}</option>
      </select>
      <select v-model="statusFilter" @change="applyFilters">
        <option value="">{{ t('traces.allStatuses') }}</option>
        <option v-for="status in statusOptions" :key="status" :value="status">{{ status }}</option>
      </select>
      <label class="time-filter">
        <span>{{ t('common.from') }}</span>
        <input v-model="fromTime" type="datetime-local" @change="applyFilters" />
      </label>
      <label class="time-filter">
        <span>{{ t('common.to') }}</span>
        <input v-model="toTime" type="datetime-local" @change="applyFilters" />
      </label>
      <button class="button secondary" type="button" @click="applyFilters">{{ t('common.apply') }}</button>
    </section>

    <div v-if="error" class="notice">{{ error }}</div>

    <section class="panel table-panel content-fit">
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead>
            <tr>
              <th>{{ t('audit.time') }}</th>
              <th>{{ t('usage.model') }}</th>
              <th>{{ t('usage.route') }}</th>
              <th>{{ t('traces.policy') }}</th>
              <th>{{ t('providers.status') }}</th>
              <th>{{ t('traces.http') }}</th>
              <th>{{ t('usage.tokens') }}</th>
              <th>{{ t('usage.latency') }}</th>
              <th>{{ t('traces.summary') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="trace in filteredTraces" :key="trace.id">
              <td>{{ formatTime(trace.created_at) }}</td>
              <td>
                <strong>{{ trace.model || '-' }}</strong>
                <span>{{ trace.upstream_model || '-' }} · {{ trace.stream ? t('traces.stream') : t('traces.nonStream') }}</span>
              </td>
              <td>
                <strong>{{ trace.provider_id || '-' }}</strong>
                <span>{{ trace.route_group || '-' }} · {{ trace.provider_account_id || trace.route_source || '-' }}</span>
              </td>
              <td>
                <strong>{{ trace.policy_name || trace.policy_id || '-' }}</strong>
                <span>{{ formatPolicySource(trace.policy_source) }}</span>
              </td>
              <td>
                <span class="pill" :class="statusClass(trace.status)">{{ trace.status }}</span>
                <span>{{ trace.error_type || '-' }}</span>
              </td>
              <td>{{ trace.http_status || '-' }}</td>
              <td>{{ formatNumber(trace.input_tokens + trace.output_tokens) }}</td>
              <td>{{ formatNumber(trace.latency_ms) }} ms</td>
              <td>
                <strong>{{ trace.response_summary || '-' }}</strong>
                <span>{{ trace.route_reason || trace.request_summary || '-' }}</span>
              </td>
            </tr>
            <tr v-if="!filteredTraces.length">
              <td colspan="9" class="empty-cell">{{ loading ? t('common.loading') : t('traces.empty') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section class="pagination-bar">
      <button class="button secondary" type="button" :disabled="!canPrevious || loading" @click="previousPage">
        {{ t('common.previous') }}
      </button>
      <span>{{ t('common.page') }} {{ pageNumber }}</span>
      <button class="button secondary" type="button" :disabled="!canNext || loading" @click="nextPage">
        {{ t('common.next') }}
      </button>
    </section>
  </main>
</template>
