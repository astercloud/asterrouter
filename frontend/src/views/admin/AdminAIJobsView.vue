<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Ban, Eye, RefreshCw, RotateCw, Search, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import {
  cancelAIJob,
  getAIJob,
  getAIJobRuntime,
  getAIJobSummary,
  getAIJobs,
  scheduleAIJobAttemptReconciliation
} from '@/api/control'
import type { AIAttemptAdminRecord, AIJobAdminDetail, AIJobAdminRecord, AIJobListQuery, AIJobRuntimeStatus, AIJobSummary } from '@/types'

const { t } = useI18n()
const jobs = ref<AIJobAdminRecord[]>([])
const summary = ref<AIJobSummary>({ total: 0, by_status: {} })
const runtime = ref<AIJobRuntimeStatus | null>(null)
const selected = ref<AIJobAdminDetail | null>(null)
const loading = ref(false)
const detailLoading = ref(false)
const actionLoading = ref('')
const error = ref('')
const notice = ref('')
const search = ref('')
const status = ref('')
const modality = ref('')
const operation = ref('')
const artifactPolicy = ref('')
const pageSize = ref(25)
const offset = ref(0)

const statusOptions = ['accepted', 'queued', 'dispatching', 'running', 'canceling', 'canceled', 'succeeded', 'failed', 'unknown', 'expired']
const modalityOptions = ['text', 'image', 'video', 'audio']
const policyOptions = ['temporary', 'managed', 'metadata_only', 'customer_sink', 'proxy_only']
const pageNumber = computed(() => Math.floor(offset.value / pageSize.value) + 1)
const canPrevious = computed(() => offset.value > 0)
const canNext = computed(() => jobs.value.length >= pageSize.value)
const activeCount = computed(() => ['accepted', 'queued', 'dispatching', 'running', 'canceling'].reduce((total, key) => total + (summary.value.by_status[key] || 0), 0))
const attentionCount = computed(() => (summary.value.by_status.unknown || 0) + (summary.value.by_status.failed || 0))
const runtimeRows = computed(() => {
  if (!runtime.value) return []
  return [
    ['scheduler', runtime.value.scheduler],
    ['delivery', runtime.value.delivery],
    ['reconciler', runtime.value.reconciler],
    ['rebuilder', runtime.value.rebuilder]
  ] as const
})

function listQuery(): AIJobListQuery {
  return {
    q: search.value.trim() || undefined,
    status: status.value || undefined,
    modality: modality.value || undefined,
    operation: operation.value.trim() || undefined,
    artifact_policy: artifactPolicy.value || undefined,
    limit: pageSize.value,
    offset: offset.value
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const query = listQuery()
    const [jobData, summaryData, runtimeData] = await Promise.all([getAIJobs(query), getAIJobSummary(query), getAIJobRuntime()])
    jobs.value = jobData
    summary.value = summaryData
    runtime.value = runtimeData
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
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

async function showDetail(id: string) {
  detailLoading.value = true
  error.value = ''
  try {
    selected.value = await getAIJob(id)
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    detailLoading.value = false
  }
}

async function cancelSelected() {
  if (!selected.value || !canCancel(selected.value.job.status) || !window.confirm(t('aiJobOps.cancelConfirm'))) return
  actionLoading.value = `cancel:${selected.value.job.id}`
  error.value = ''
  notice.value = ''
  try {
    await cancelAIJob(selected.value.job.id)
    notice.value = t('aiJobOps.cancelScheduled')
    await load()
    selected.value = await getAIJob(selected.value.job.id)
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    actionLoading.value = ''
  }
}

async function reconcileAttempt(attempt: AIAttemptAdminRecord) {
  if (!selected.value || !canReconcile(attempt) || !window.confirm(t('aiJobOps.reconcileConfirm'))) return
  actionLoading.value = `reconcile:${attempt.id}`
  error.value = ''
  notice.value = ''
  try {
    await scheduleAIJobAttemptReconciliation(selected.value.job.id, attempt.id)
    notice.value = t('aiJobOps.reconcileScheduled')
    selected.value = await getAIJob(selected.value.job.id)
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    actionLoading.value = ''
  }
}

function closeDetail() {
  selected.value = null
}

function canCancel(value: string) {
  return ['accepted', 'queued', 'dispatching', 'running'].includes(value)
}

function canReconcile(attempt: AIAttemptAdminRecord) {
  return attempt.status === 'running' && ['submitted', 'accepted', 'unknown'].includes(attempt.dispatch_state)
}

function statusClass(value: string): string {
  if (['succeeded', 'canceled', 'accepted', 'running', 'registered'].includes(value)) return 'status-success'
  if (['failed', 'unknown', 'unavailable'].includes(value)) return 'status-danger'
  return 'status-warning'
}

function humanize(value?: string): string {
  return value ? value.replace(/_/g, ' ') : '-'
}

function formatDate(value?: string): string {
  return value ? new Date(value).toLocaleString() : '-'
}

onMounted(load)
</script>

<template>
  <main class="content crud-page ai-job-page">
    <section class="page-header">
      <div>
        <h1>{{ t('aiJobOps.title') }}</h1>
        <p>{{ t('aiJobOps.subtitle') }}</p>
      </div>
      <button class="button secondary" type="button" :disabled="loading" @click="load">
        <RefreshCw :size="17" />{{ t('common.refresh') }}
      </button>
    </section>

    <div class="crud-summary">
      <span><strong>{{ summary.total }}</strong>{{ t('aiJobOps.total') }}</span>
      <span><strong>{{ activeCount }}</strong>{{ t('aiJobOps.active') }}</span>
      <span><strong>{{ summary.by_status.queued || 0 }}</strong>{{ t('aiJobOps.queued') }}</span>
      <span><strong>{{ summary.by_status.succeeded || 0 }}</strong>{{ t('aiJobOps.succeeded') }}</span>
      <span><strong>{{ attentionCount }}</strong>{{ t('aiJobOps.attention') }}</span>
    </div>

    <section class="ai-runtime-strip" aria-labelledby="ai-runtime-title">
      <div class="runtime-heading">
        <strong id="ai-runtime-title">{{ t('aiJobOps.runtime') }}</strong>
        <span class="pill" :class="statusClass(runtime?.running ? 'running' : 'unknown')">{{ runtime?.running ? t('aiJobOps.online') : t('aiJobOps.offline') }}</span>
        <span class="muted-copy">{{ runtime?.queue_driver || t('aiJobOps.unavailable') }} · {{ runtime?.worker_id || '-' }}</span>
      </div>
      <div class="ai-runtime-components">
        <span v-for="[name, component] in runtimeRows" :key="name" class="runtime-component">
          <span>{{ t(`aiJobOps.${name}`) }}</span>
          <strong>{{ component.runs }}</strong>
          <small v-if="component.errors" class="runtime-error">{{ component.errors }} {{ t('aiJobOps.errors') }}</small>
          <small v-else class="runtime-ok">{{ t('aiJobOps.healthy') }}</small>
        </span>
      </div>
    </section>

    <section class="table-toolbar ai-job-toolbar">
      <label class="search-box"><Search :size="17" /><input v-model="search" :placeholder="t('aiJobOps.searchPlaceholder')" @keyup.enter="applyFilters" /></label>
      <select v-model="status" :aria-label="t('aiJobOps.allStatuses')" @change="applyFilters"><option value="">{{ t('aiJobOps.allStatuses') }}</option><option v-for="item in statusOptions" :key="item" :value="item">{{ humanize(item) }}</option></select>
      <select v-model="modality" :aria-label="t('aiJobOps.allModalities')" @change="applyFilters"><option value="">{{ t('aiJobOps.allModalities') }}</option><option v-for="item in modalityOptions" :key="item" :value="item">{{ humanize(item) }}</option></select>
      <select v-model="artifactPolicy" :aria-label="t('aiJobOps.allPolicies')" @change="applyFilters"><option value="">{{ t('aiJobOps.allPolicies') }}</option><option v-for="item in policyOptions" :key="item" :value="item">{{ humanize(item) }}</option></select>
      <input v-model="operation" :placeholder="t('aiJobOps.operationPlaceholder')" @keyup.enter="applyFilters" />
      <button class="button secondary" type="button" @click="applyFilters">{{ t('common.apply') }}</button>
    </section>

    <div v-if="error" class="notice">{{ error }}</div>
    <div v-if="notice" class="notice success-notice">{{ notice }}</div>

    <section class="panel table-panel content-fit">
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead><tr><th>{{ t('aiJobOps.created') }}</th><th>{{ t('aiJobOps.request') }}</th><th>{{ t('aiJobOps.mode') }}</th><th>{{ t('aiJobOps.state') }}</th><th>{{ t('aiJobOps.destination') }}</th><th>{{ t('common.actions') }}</th></tr></thead>
          <tbody>
            <tr v-for="job in jobs" :key="job.id">
              <td>{{ formatDate(job.created_at) }}</td>
              <td><strong>{{ job.model }}</strong><span>{{ job.id }} · {{ job.operation_id }}</span></td>
              <td><strong>{{ humanize(job.modality) }}</strong><span>{{ humanize(job.operation) }} · {{ humanize(job.protocol) }}</span></td>
              <td><span class="pill" :class="statusClass(job.status)">{{ humanize(job.status) }}</span><span v-if="job.error_type">{{ humanize(job.error_type) }}</span></td>
              <td><strong>{{ job.artifact_sink_id || humanize(job.artifact_policy) }}</strong><span>{{ job.tenant_id || t('aiJobOps.internal') }}</span></td>
              <td><button class="icon-button" type="button" :title="t('common.details')" :aria-label="t('common.details')" @click="showDetail(job.id)"><Eye :size="17" /></button></td>
            </tr>
            <tr v-if="!jobs.length"><td colspan="6" class="empty-cell">{{ loading ? t('common.loading') : t('aiJobOps.empty') }}</td></tr>
          </tbody>
        </table>
      </div>
    </section>

    <section class="pagination-bar"><button class="button secondary" type="button" :disabled="!canPrevious || loading" @click="previousPage">{{ t('common.previous') }}</button><span>{{ t('common.page') }} {{ pageNumber }}</span><button class="button secondary" type="button" :disabled="!canNext || loading" @click="nextPage">{{ t('common.next') }}</button></section>

    <div v-if="selected" class="modal-backdrop" @click.self="closeDetail">
      <section class="modal-card wide ai-job-detail" role="dialog" aria-modal="true" aria-labelledby="ai-job-detail-title">
        <header class="modal-header"><div><h2 id="ai-job-detail-title">{{ selected.job.model }}</h2><p>{{ selected.job.id }} · {{ humanize(selected.job.status) }}</p></div><button class="icon-button" type="button" :aria-label="t('common.close')" @click="closeDetail"><X :size="18" /></button></header>
        <div class="modal-body ai-job-detail-body">
          <dl class="ai-job-detail-grid"><div><dt>{{ t('aiJobOps.request') }}</dt><dd>{{ selected.job.operation_id }}<br />{{ humanize(selected.job.operation) }} · {{ humanize(selected.job.modality) }}</dd></div><div><dt>{{ t('aiJobOps.state') }}</dt><dd><span class="pill" :class="statusClass(selected.job.status)">{{ humanize(selected.job.status) }}</span><br />v{{ selected.job.status_version }} · {{ formatDate(selected.job.updated_at) }}</dd></div><div><dt>{{ t('aiJobOps.delivery') }}</dt><dd>{{ humanize(selected.job.artifact_policy) }}<br />{{ selected.job.artifact_sink_id || '-' }}</dd></div><div><dt>{{ t('aiJobOps.retention') }}</dt><dd>{{ formatDate(selected.job.expires_at) }}</dd></div></dl>
          <section class="ai-job-section"><h3>{{ t('aiJobOps.attempts') }}</h3><div class="table-scroll"><table class="data-table crud-table"><thead><tr><th>#</th><th>{{ t('aiJobOps.provider') }}</th><th>{{ t('aiJobOps.adapter') }}</th><th>{{ t('aiJobOps.dispatch') }}</th><th>{{ t('aiJobOps.task') }}</th><th>{{ t('common.actions') }}</th></tr></thead><tbody><tr v-for="attempt in selected.attempts" :key="attempt.id"><td>{{ attempt.attempt_number }}</td><td><strong>{{ attempt.provider_id }}</strong><span>{{ attempt.provider_account_id }} · {{ attempt.route_id }}</span></td><td>{{ attempt.provider_adapter_id }}<span>{{ attempt.upstream_model }}</span></td><td><span class="pill" :class="statusClass(attempt.dispatch_state)">{{ humanize(attempt.dispatch_state) }}</span><span>{{ humanize(attempt.status) }}</span></td><td>{{ attempt.provider_task_id || '-' }}<span>{{ humanize(attempt.provider_task_status) }}</span></td><td><button v-if="canReconcile(attempt)" class="icon-button" type="button" :title="t('aiJobOps.reconcile')" :aria-label="t('aiJobOps.reconcile')" :disabled="actionLoading !== ''" @click="reconcileAttempt(attempt)"><RotateCw :size="17" /></button></td></tr><tr v-if="!selected.attempts.length"><td colspan="6" class="empty-cell">{{ t('aiJobOps.noAttempts') }}</td></tr></tbody></table></div></section>
          <section class="ai-job-section"><h3>{{ t('aiJobOps.events') }}</h3><div class="event-list"><div v-for="event in selected.events" :key="event.id" class="event-row"><strong>v{{ event.version }} · {{ humanize(event.event_type) }}</strong><span>{{ humanize(event.from_status) }} → {{ humanize(event.to_status) }} · {{ formatDate(event.created_at) }}</span><small v-if="event.reason">{{ humanize(event.reason) }}</small></div><p v-if="!selected.events.length" class="muted-copy">{{ t('aiJobOps.noEvents') }}</p></div></section>
          <section class="ai-job-section"><h3>{{ t('aiJobOps.artifacts') }}</h3><div class="artifact-chip-list"><span v-for="artifact in selected.artifacts" :key="artifact.id" class="artifact-chip"><strong>{{ artifact.media_type || artifact.id }}</strong><span>{{ humanize(artifact.status) }} · {{ humanize(artifact.policy) }}</span></span><span v-if="!selected.artifacts.length" class="muted-copy">{{ t('aiJobOps.noArtifacts') }}</span></div></section>
        </div>
        <footer class="modal-footer"><button class="button secondary" type="button" @click="closeDetail">{{ t('common.close') }}</button><button v-if="canCancel(selected.job.status)" class="button danger" type="button" :disabled="actionLoading !== ''" @click="cancelSelected"><Ban :size="17" />{{ t('aiJobOps.cancel') }}</button></footer>
      </section>
    </div>
    <span v-if="detailLoading" class="sr-only" aria-live="polite">{{ t('common.loading') }}</span>
  </main>
</template>

<style scoped>
.ai-job-toolbar { grid-template-columns: minmax(230px, 1.5fr) repeat(3, minmax(130px, .8fr)) minmax(160px, 1fr) auto; }
.ai-runtime-strip { display: flex; align-items: center; justify-content: space-between; gap: 18px; padding: 12px 0; flex-wrap: wrap; }
.runtime-heading, .ai-runtime-components, .runtime-component { display: flex; align-items: center; gap: 9px; flex-wrap: wrap; }
.ai-runtime-components { gap: 18px; }
.runtime-component { min-height: 32px; }
.runtime-ok { color: var(--success); }
.runtime-error { color: var(--danger); }
.ai-job-detail-body { display: grid; gap: 22px; }
.ai-job-detail-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 14px; margin: 0; }
.ai-job-detail-grid > div { border: 1px solid var(--border); padding: 12px; background: var(--surface-subtle); }
.ai-job-detail-grid dt { color: var(--text-muted); font-size: .78rem; text-transform: uppercase; }
.ai-job-detail-grid dd { margin: 7px 0 0; line-height: 1.5; }
.ai-job-section h3 { margin: 0 0 10px; font-size: 1rem; }
.event-list { display: grid; gap: 8px; }
.event-row { display: grid; gap: 3px; border-left: 2px solid var(--border-strong); padding: 7px 10px; }
.event-row span, .event-row small { color: var(--text-muted); }
.artifact-chip-list { display: flex; gap: 8px; flex-wrap: wrap; }
.artifact-chip { display: grid; gap: 3px; border: 1px solid var(--border); padding: 9px 11px; min-width: 170px; }
.artifact-chip span { color: var(--text-muted); }
@media (max-width: 900px) { .ai-job-toolbar { grid-template-columns: 1fr 1fr; } .ai-job-detail-grid { grid-template-columns: 1fr 1fr; } }
@media (max-width: 620px) { .ai-job-toolbar { grid-template-columns: 1fr; } .ai-job-detail-grid { grid-template-columns: 1fr; } .ai-runtime-strip { align-items: flex-start; } }
</style>
