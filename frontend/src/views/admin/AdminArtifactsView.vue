<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { Download, Eye, FileWarning, LoaderCircle, RefreshCw, RotateCcw, Search, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import {
  getArtifact as getAdminArtifact,
  getArtifactContent as getAdminArtifactContent,
  getArtifactRuntimes as getAdminArtifactRuntimes,
  getArtifacts as getAdminArtifacts,
  getArtifactSummary as getAdminArtifactSummary,
  retryArtifactDelivery as retryAdminArtifactDelivery
} from '@/api/control'
import {
  getPlatformArtifact,
  getPlatformArtifactContent,
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
      getArtifactContent: getPlatformArtifactContent,
      getRuntimes: getPlatformArtifactRuntimes,
      getArtifacts: getPlatformArtifacts,
      getSummary: getPlatformArtifactSummary,
      retryDelivery: retryPlatformArtifactDelivery
    }
  : {
      getArtifact: getAdminArtifact,
      getArtifactContent: getAdminArtifactContent,
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
const previewLoading = ref(false)
const retrying = ref(false)
const error = ref('')
const notice = ref('')
const previewError = ref('')
const previewKind = ref<'image' | 'video' | 'audio' | 'pdf' | 'text' | 'unsupported'>('unsupported')
const previewObjectURL = ref('')
const previewText = ref('')
const search = ref(new URLSearchParams(window.location.search).get('q')?.trim() || '')
const policy = ref('')
const status = ref('')
const role = ref('')
const pageSize = ref(25)
const offset = ref(0)
let previewRequestVersion = 0

const MAX_TEXT_PREVIEW_CHARS = 200_000

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
  const requestVersion = ++previewRequestVersion
  resetPreview()
  detailLoading.value = true
  error.value = ''
  try {
    const detail = await operations.getArtifact(id)
    if (requestVersion !== previewRequestVersion) return
    selected.value = detail
    await loadArtifactPreview(detail.artifact, requestVersion)
  } catch (err) {
    if (requestVersion === previewRequestVersion) {
      error.value = err instanceof Error ? err.message : t('common.failed')
    }
  } finally {
    if (requestVersion === previewRequestVersion) detailLoading.value = false
  }
}

function resolvePreviewKind(mediaType: string): typeof previewKind.value {
  const normalized = mediaType.toLowerCase().split(';', 1)[0]?.trim() || ''
  if (normalized.startsWith('image/')) return 'image'
  if (normalized.startsWith('video/')) return 'video'
  if (normalized.startsWith('audio/')) return 'audio'
  if (normalized === 'application/pdf') return 'pdf'
  if (
    normalized.startsWith('text/') ||
    normalized === 'application/json' ||
    normalized.endsWith('+json') ||
    normalized === 'application/xml' ||
    normalized.endsWith('+xml') ||
    normalized === 'application/javascript' ||
    normalized === 'application/yaml'
  ) return 'text'
  return 'unsupported'
}

function formatTextPreview(value: string, mediaType: string): string {
  let formatted = value
  const normalized = mediaType.toLowerCase().split(';', 1)[0]?.trim() || ''
  if (normalized === 'application/json' || normalized.endsWith('+json')) {
    try {
      formatted = JSON.stringify(JSON.parse(value), null, 2)
    } catch {
      formatted = value
    }
  }
  if (formatted.length <= MAX_TEXT_PREVIEW_CHARS) return formatted
  return `${formatted.slice(0, MAX_TEXT_PREVIEW_CHARS)}\n\n${t('artifactOps.previewTruncated')}`
}

function releasePreviewObjectURL() {
  if (previewObjectURL.value) URL.revokeObjectURL(previewObjectURL.value)
  previewObjectURL.value = ''
}

function resetPreview() {
  releasePreviewObjectURL()
  previewLoading.value = false
  previewError.value = ''
  previewKind.value = 'unsupported'
  previewText.value = ''
}

async function loadArtifactPreview(artifact: ArtifactAdminRecord, requestVersion: number) {
  if (!['ready', 'delivered'].includes(artifact.status) || artifact.size_bytes <= 0) {
    previewError.value = artifact.size_bytes <= 0
      ? t('artifactOps.previewEmpty')
      : t('artifactOps.previewUnavailable')
    return
  }
  previewLoading.value = true
  try {
    const blob = await operations.getArtifactContent(artifact.id)
    if (requestVersion !== previewRequestVersion) return
    const mediaType = blob.type || artifact.media_type || 'application/octet-stream'
    const kind = resolvePreviewKind(mediaType)
    const text = kind === 'text' ? formatTextPreview(await blob.text(), mediaType) : ''
    if (requestVersion !== previewRequestVersion) return
    previewKind.value = kind
    previewText.value = text
    previewObjectURL.value = URL.createObjectURL(blob)
  } catch (err) {
    if (requestVersion === previewRequestVersion) {
      const message = err instanceof Error ? err.message : t('common.failed')
      previewError.value = t('artifactOps.previewFailed', { message })
    }
  } finally {
    if (requestVersion === previewRequestVersion) previewLoading.value = false
  }
}

function previewFileName(artifact: ArtifactAdminRecord): string {
  const mediaType = (artifact.media_type || '').toLowerCase().split(';', 1)[0]?.trim() || ''
  const extensionByMediaType: Record<string, string> = {
    'application/json': 'json',
    'application/pdf': 'pdf',
    'image/jpeg': 'jpg',
    'image/png': 'png',
    'image/webp': 'webp',
    'video/mp4': 'mp4',
    'audio/mpeg': 'mp3',
    'audio/wav': 'wav'
  }
  const extension = extensionByMediaType[mediaType] || mediaType.split('/')[1]?.replace(/[^a-z0-9.+-]/g, '') || 'bin'
  const id = artifact.id.replace(/[^a-zA-Z0-9._-]/g, '_') || 'artifact'
  return `${id}.${extension}`
}

function downloadPreview() {
  if (!selected.value || !previewObjectURL.value) return
  const link = document.createElement('a')
  link.href = previewObjectURL.value
  link.download = previewFileName(selected.value.artifact)
  document.body.appendChild(link)
  link.click()
  link.remove()
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
  previewRequestVersion += 1
  resetPreview()
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
onBeforeUnmount(() => {
  previewRequestVersion += 1
  releasePreviewObjectURL()
})
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
          <section class="artifact-preview" aria-labelledby="artifact-preview-title">
            <header class="artifact-preview-header">
              <div>
                <h3 id="artifact-preview-title">{{ t('artifactOps.preview') }}</h3>
                <span>{{ selected.artifact.media_type || t('artifactOps.unknownMedia') }} · {{ formatBytes(selected.artifact.size_bytes) }}</span>
              </div>
              <button v-if="previewObjectURL" class="button secondary" type="button" @click="downloadPreview">
                <Download :size="16" />
                {{ t('common.download') }}
              </button>
            </header>
            <div v-if="previewLoading" class="artifact-preview-state" aria-live="polite">
              <LoaderCircle class="artifact-preview-spinner" :size="22" />
              <span>{{ t('artifactOps.previewLoading') }}</span>
            </div>
            <div v-else-if="previewError" class="artifact-preview-state artifact-preview-error" role="status">
              <FileWarning :size="24" />
              <span>{{ previewError }}</span>
            </div>
            <img
              v-else-if="previewKind === 'image'"
              class="artifact-preview-media"
              :src="previewObjectURL"
              :alt="t('artifactOps.previewAlt', { id: selected.artifact.id })"
            />
            <video v-else-if="previewKind === 'video'" class="artifact-preview-media" :src="previewObjectURL" controls preload="metadata" />
            <audio v-else-if="previewKind === 'audio'" class="artifact-preview-audio" :src="previewObjectURL" controls preload="metadata" />
            <iframe
              v-else-if="previewKind === 'pdf'"
              class="artifact-preview-pdf"
              :src="previewObjectURL"
              :title="t('artifactOps.previewAlt', { id: selected.artifact.id })"
            />
            <pre v-else-if="previewKind === 'text'" class="artifact-preview-text">{{ previewText }}</pre>
            <div v-else class="artifact-preview-state">
              <FileWarning :size="24" />
              <span>{{ t('artifactOps.previewUnsupported') }}</span>
            </div>
          </section>
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

.artifact-detail .modal-header > div {
  min-width: 0;
}

.artifact-detail .modal-header h2 {
  overflow-wrap: anywhere;
}

.artifact-detail .modal-header .icon-button {
  flex: 0 0 auto;
}

.artifact-detail-body {
  display: grid;
  gap: 24px;
  min-width: 0;
}

.artifact-preview {
  display: grid;
  gap: 12px;
  min-width: 0;
}

.artifact-preview-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.artifact-preview-header h3 {
  margin: 0 0 4px;
  font-size: 1rem;
}

.artifact-preview-header span {
  color: var(--text-muted);
  font-size: 0.82rem;
}

.artifact-preview-state {
  min-height: 220px;
  display: grid;
  place-content: center;
  justify-items: center;
  gap: 10px;
  padding: 32px;
  border: 1px dashed var(--border);
  background: var(--surface-muted, var(--surface));
  color: var(--text-muted);
  text-align: center;
}

.artifact-preview-error {
  color: var(--danger);
}

.artifact-preview-spinner {
  animation: artifact-preview-spin 0.8s linear infinite;
}

.artifact-preview-media,
.artifact-preview-pdf {
  width: 100%;
  height: min(460px, 56vh);
  min-height: 280px;
  border: 1px solid var(--border);
  background: var(--surface-subtle);
}

.artifact-preview-pdf { background: #ffffff; }

img.artifact-preview-media,
video.artifact-preview-media {
  object-fit: contain;
}

.artifact-preview-audio {
  width: 100%;
  min-height: 54px;
}

.artifact-preview-text {
  max-height: min(460px, 56vh);
  margin: 0;
  padding: 16px;
  overflow: auto;
  border: 1px solid var(--border);
  background: var(--surface-subtle);
  color: var(--text);
  font: 0.82rem/1.55 ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

@keyframes artifact-preview-spin {
  to { transform: rotate(360deg); }
}

.artifact-detail-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px 24px;
  min-width: 0;
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

.artifact-events {
  min-width: 0;
}

@media (max-width: 640px) {
  .artifact-preview-header {
    align-items: flex-start;
    flex-direction: column;
  }

  .artifact-preview-media,
  .artifact-preview-pdf {
    min-height: 220px;
  }

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
