<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Eye, RefreshCw, RotateCcw, Search, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import {
  getArtifact as getAdminArtifact,
  getArtifactRuntimes as getAdminArtifactRuntimes,
  getArtifacts as getAdminArtifacts,
  getArtifactSummary as getAdminArtifactSummary,
  retryArtifactDelivery as retryAdminArtifactDelivery
} from '@/api/control'
import {
  getPlatformArtifact,
  getPlatformArtifactRuntimes,
  getPlatformArtifacts,
  getPlatformArtifactSummary,
  retryPlatformArtifactDelivery
} from '@/api/platform'
import type {
  ArtifactAdminDetail,
  ArtifactAdminRecord,
  ArtifactListQuery,
  ArtifactRuntime,
  ArtifactSummary
} from '@/types'

const props = withDefaults(defineProps<{ surface?: 'admin' | 'platform' }>(), { surface: 'admin' })
const { t } = useI18n()
const operations = props.surface === 'platform'
  ? {
      getArtifact: getPlatformArtifact,
      getRuntimes: getPlatformArtifactRuntimes,
      getArtifacts: getPlatformArtifacts,
      getSummary: getPlatformArtifactSummary,
      retryDelivery: retryPlatformArtifactDelivery
    }
  : {
      getArtifact: getAdminArtifact,
      getRuntimes: getAdminArtifactRuntimes,
      getArtifacts: getAdminArtifacts,
      getSummary: getAdminArtifactSummary,
      retryDelivery: retryAdminArtifactDelivery
    }
const artifacts = ref<ArtifactAdminRecord[]>([])
const summary = ref<ArtifactSummary>({ total: 0, size_bytes: 0, by_status: {} })
const runtimes = ref<ArtifactRuntime[]>([])
const selected = ref<ArtifactAdminDetail | null>(null)
const loading = ref(false)
const detailLoading = ref(false)
const retrying = ref(false)
const error = ref('')
const notice = ref('')
const search = ref('')
const policy = ref('')
const status = ref('')
const role = ref('')
const pageSize = ref(25)
const offset = ref(0)

const policyOptions = ['proxy_only', 'temporary', 'managed', 'customer_sink', 'metadata_only']
const statusOptions = [
  'pending', 'uploading', 'ready', 'failed', 'delivering', 'delivered', 'delivery_failed',
  'delete_requested', 'deleting', 'deleted', 'delete_failed', 'expired'
]
const roleOptions = ['input', 'preview', 'final', 'derived', 'provider_reference', 'metadata']
const pageNumber = computed(() => Math.floor(offset.value / pageSize.value) + 1)
const canPrevious = computed(() => offset.value > 0)
const canNext = computed(() => artifacts.value.length >= pageSize.value)
const attentionCount = computed(() => ['failed', 'delivery_failed', 'delete_failed'].reduce((total, key) => total + (summary.value.by_status[key] || 0), 0))

function listQuery(): ArtifactListQuery {
  return {
    q: search.value.trim() || undefined,
    policy: policy.value || undefined,
    status: status.value || undefined,
    role: role.value || undefined,
    limit: pageSize.value,
    offset: offset.value
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const query = listQuery()
    const [artifactData, summaryData, runtimeData] = await Promise.all([
      operations.getArtifacts(query),
      operations.getSummary(query),
      operations.getRuntimes()
    ])
    artifacts.value = artifactData
    summary.value = summaryData
    runtimes.value = runtimeData
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
    selected.value = await operations.getArtifact(id)
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    detailLoading.value = false
  }
}

async function retryDelivery() {
  const artifact = selected.value?.artifact
  if (!artifact || artifact.status !== 'delivery_failed' || !window.confirm(t('artifactOps.retryConfirm'))) return
  retrying.value = true
  error.value = ''
  notice.value = ''
  try {
    await operations.retryDelivery(artifact.id)
    notice.value = t('artifactOps.retryScheduled')
    await load()
    selected.value = await operations.getArtifact(artifact.id)
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    retrying.value = false
  }
}

function closeDetail() {
  selected.value = null
}

function statusClass(value: string): string {
  if (['ready', 'delivered', 'registered'].includes(value)) return 'status-success'
  if (['failed', 'delivery_failed', 'delete_failed', 'unavailable'].includes(value)) return 'status-danger'
  return 'status-warning'
}

function humanize(value?: string): string {
  return value ? value.replace(/_/g, ' ') : '-'
}

function formatDate(value?: string): string {
  return value ? new Date(value).toLocaleString() : '-'
}

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
  return `${(value / 1024 ** index).toFixed(index === 0 ? 0 : 1)} ${units[index]}`
}

onMounted(load)
</script>

<template>
  <main class="content crud-page">
    <section class="page-header">
      <div>
        <h1>{{ t('artifactOps.title') }}</h1>
        <p>{{ t('artifactOps.subtitle') }}</p>
      </div>
      <button class="button secondary" type="button" :disabled="loading" @click="load">
        <RefreshCw :size="17" />
        {{ t('common.refresh') }}
      </button>
    </section>

    <div class="crud-summary">
      <span><strong>{{ summary.total }}</strong>{{ t('artifactOps.total') }}</span>
      <span><strong>{{ summary.by_status.ready || 0 }}</strong>{{ t('artifactOps.available') }}</span>
      <span><strong>{{ summary.by_status.delivered || 0 }}</strong>{{ t('artifactOps.delivered') }}</span>
      <span><strong>{{ attentionCount }}</strong>{{ t('artifactOps.attention') }}</span>
      <span><strong>{{ formatBytes(summary.size_bytes) }}</strong>{{ t('artifactOps.volume') }}</span>
    </div>

    <section class="artifact-runtime-strip" aria-labelledby="artifact-runtime-title">
      <strong id="artifact-runtime-title">{{ t('artifactOps.runtimes') }}</strong>
      <div v-if="runtimes.length" class="artifact-runtime-list">
        <span v-for="runtime in runtimes" :key="`${runtime.kind}:${runtime.id}`" class="runtime-item">
          <span>{{ runtime.kind === 'sink' ? t('artifactOps.sink') : t('artifactOps.proxy') }}</span>
          <code>{{ runtime.id }}</code>
          <span class="pill" :class="statusClass(runtime.status)">{{ t('artifactOps.registered') }}</span>
        </span>
      </div>
      <span v-else class="muted-copy">{{ t('artifactOps.noRuntimes') }}</span>
    </section>

    <section class="table-toolbar">
      <label class="search-box">
        <Search :size="17" />
        <input v-model="search" :placeholder="t('artifactOps.searchPlaceholder')" @keyup.enter="applyFilters" />
      </label>
      <select v-model="policy" :aria-label="t('artifactOps.allPolicies')" @change="applyFilters">
        <option value="">{{ t('artifactOps.allPolicies') }}</option>
        <option v-for="item in policyOptions" :key="item" :value="item">{{ humanize(item) }}</option>
      </select>
      <select v-model="status" :aria-label="t('artifactOps.allStatuses')" @change="applyFilters">
        <option value="">{{ t('artifactOps.allStatuses') }}</option>
        <option v-for="item in statusOptions" :key="item" :value="item">{{ humanize(item) }}</option>
      </select>
      <select v-model="role" :aria-label="t('artifactOps.allRoles')" @change="applyFilters">
        <option value="">{{ t('artifactOps.allRoles') }}</option>
        <option v-for="item in roleOptions" :key="item" :value="item">{{ humanize(item) }}</option>
      </select>
      <button class="button secondary" type="button" @click="applyFilters">{{ t('common.apply') }}</button>
    </section>

    <div v-if="error" class="notice">{{ error }}</div>
    <div v-if="notice" class="notice success-notice">{{ notice }}</div>

    <section class="panel table-panel content-fit">
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead>
            <tr>
              <th>{{ t('artifactOps.created') }}</th>
              <th>{{ t('artifactOps.artifact') }}</th>
              <th>{{ t('artifactOps.media') }}</th>
              <th>{{ t('artifactOps.delivery') }}</th>
              <th>{{ t('artifactOps.destination') }}</th>
              <th>{{ t('artifactOps.retention') }}</th>
              <th>{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="artifact in artifacts" :key="artifact.id">
              <td>{{ formatDate(artifact.created_at) }}</td>
              <td><strong>{{ artifact.id }}</strong><span>{{ artifact.operation_id }}</span></td>
              <td><strong>{{ artifact.media_type || '-' }}</strong><span>{{ humanize(artifact.role) }} · {{ formatBytes(artifact.size_bytes) }}</span></td>
              <td>
                <span class="pill" :class="statusClass(artifact.status)">{{ humanize(artifact.status) }}</span>
                <span>{{ humanize(artifact.policy) }}<template v-if="artifact.error_type"> · {{ humanize(artifact.error_type) }}</template></span>
              </td>
              <td>
                <strong>{{ artifact.sink_id || artifact.provider_id || artifact.store_driver || '-' }}</strong>
                <span v-if="artifact.runtime_status" class="pill" :class="statusClass(artifact.runtime_status)">{{ humanize(artifact.runtime_status) }}</span>
              </td>
              <td>{{ formatDate(artifact.retain_until) }}</td>
              <td>
                <button class="icon-button" type="button" :title="t('common.details')" :aria-label="t('common.details')" @click="showDetail(artifact.id)">
                  <Eye :size="17" />
                </button>
              </td>
            </tr>
            <tr v-if="!artifacts.length">
              <td colspan="7" class="empty-cell">{{ loading ? t('common.loading') : t('artifactOps.empty') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section class="pagination-bar">
      <button class="button secondary" type="button" :disabled="!canPrevious || loading" @click="previousPage">{{ t('common.previous') }}</button>
      <span>{{ t('common.page') }} {{ pageNumber }}</span>
      <button class="button secondary" type="button" :disabled="!canNext || loading" @click="nextPage">{{ t('common.next') }}</button>
    </section>

    <div v-if="selected" class="modal-backdrop" @click.self="closeDetail">
      <section class="modal-card wide artifact-detail" role="dialog" aria-modal="true" aria-labelledby="artifact-detail-title">
        <header class="modal-header">
          <div>
            <h2 id="artifact-detail-title">{{ selected.artifact.id }}</h2>
            <p>{{ humanize(selected.artifact.policy) }} · {{ humanize(selected.artifact.status) }}</p>
          </div>
          <button class="icon-button" type="button" :aria-label="t('common.close')" @click="closeDetail"><X :size="18" /></button>
        </header>
        <div class="modal-body artifact-detail-body">
          <dl class="artifact-detail-grid">
            <div><dt>{{ t('artifactOps.identifiers') }}</dt><dd>{{ selected.artifact.operation_id }}<br />{{ selected.artifact.job_id || '-' }}<br />{{ selected.artifact.attempt_id || '-' }}</dd></div>
            <div><dt>{{ t('artifactOps.media') }}</dt><dd>{{ selected.artifact.media_type || '-' }}<br />{{ humanize(selected.artifact.role) }} · {{ formatBytes(selected.artifact.size_bytes) }}</dd></div>
            <div><dt>{{ t('artifactOps.destination') }}</dt><dd>{{ selected.artifact.sink_id || selected.artifact.provider_id || selected.artifact.store_driver || '-' }}<br />{{ humanize(selected.artifact.runtime_status) }}</dd></div>
            <div><dt>{{ t('artifactOps.integrity') }}</dt><dd>{{ selected.artifact.sha256 || '-' }}</dd></div>
            <div><dt>{{ t('artifactOps.created') }}</dt><dd>{{ formatDate(selected.artifact.created_at) }}</dd></div>
            <div><dt>{{ t('artifactOps.retainedUntil') }}</dt><dd>{{ formatDate(selected.artifact.retain_until) }}</dd></div>
          </dl>
          <section class="artifact-events" aria-labelledby="artifact-events-title">
            <h3 id="artifact-events-title">{{ t('artifactOps.events') }}</h3>
            <div class="table-scroll">
              <table class="data-table crud-table">
                <thead><tr><th>{{ t('artifactOps.version') }}</th><th>{{ t('audit.time') }}</th><th>{{ t('artifactOps.transition') }}</th><th>{{ t('artifactOps.reason') }}</th></tr></thead>
                <tbody>
                  <tr v-for="event in selected.events" :key="event.id">
                    <td>{{ event.version }}</td>
                    <td>{{ formatDate(event.created_at) }}</td>
                    <td>{{ humanize(event.from_status) }} → {{ humanize(event.to_status) }}</td>
                    <td>{{ humanize(event.reason) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>
        </div>
        <footer class="modal-footer">
          <button class="button secondary" type="button" @click="closeDetail">{{ t('common.close') }}</button>
          <button v-if="selected.artifact.status === 'delivery_failed'" class="button" type="button" :disabled="retrying" @click="retryDelivery">
            <RotateCcw :size="17" />{{ retrying ? t('artifactOps.retrying') : t('artifactOps.retry') }}
          </button>
        </footer>
      </section>
    </div>
    <span v-if="detailLoading" class="sr-only" aria-live="polite">{{ t('common.loading') }}</span>
  </main>
</template>

<style scoped>
.artifact-runtime-strip {
  display: flex;
  align-items: center;
  gap: 16px;
  min-height: 42px;
  padding: 4px 0;
  flex-wrap: wrap;
}

.artifact-runtime-list {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.table-toolbar {
  display: grid;
  grid-template-columns: minmax(240px, 1.5fr) repeat(3, minmax(130px, 0.8fr)) auto;
}

.table-toolbar .search-box,
.table-toolbar select {
  min-width: 0;
  width: 100%;
}

.runtime-item {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-height: 34px;
}

.muted-copy {
  color: var(--text-muted);
}

.success-notice {
  border-color: var(--success);
}

.artifact-detail {
  width: min(920px, calc(100vw - 32px));
}

.artifact-detail-body {
  display: grid;
  gap: 24px;
}

.artifact-detail-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px 24px;
  margin: 0;
}

.artifact-detail-grid div {
  min-width: 0;
}

.artifact-detail-grid dt {
  color: var(--text-muted);
  font-size: 0.78rem;
  margin-bottom: 6px;
}

.artifact-detail-grid dd {
  margin: 0;
  overflow-wrap: anywhere;
  line-height: 1.55;
}

.artifact-events h3 {
  margin: 0 0 12px;
  font-size: 1rem;
}

@media (max-width: 640px) {
  .artifact-detail-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 1180px) {
  .table-toolbar {
    grid-template-columns: 1fr;
  }
}
</style>
