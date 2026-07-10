<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { Download, Plus, RefreshCw, Save, Search, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { createExportJob, downloadExportJob, getExportJobs } from '@/api/control'
import type { ExportJob, ExportJobKind, RecordListQuery } from '@/types'
import { datetimeLocalToISOString } from '@/utils/timeRange'

const { t } = useI18n()
const loading = ref(false)
const creating = ref(false)
const downloadingID = ref('')
const error = ref('')
const message = ref('')
const query = ref('')
const statusFilter = ref('')
const kindFilter = ref('')
const showCreate = ref(false)
const jobs = ref<ExportJob[]>([])
let pollTimer: number | undefined

const form = reactive({
  kind: 'usage' as ExportJobKind,
  q: '',
  model: '',
  status: '',
  action: '',
  resource_type: '',
  from: '',
  to: '',
  limit: 50000
})

const filteredJobs = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return jobs.value.filter((job) => {
    if (statusFilter.value && job.status !== statusFilter.value) return false
    if (kindFilter.value && job.kind !== kindFilter.value) return false
    if (!keyword) return true
    return [job.id, job.kind, job.status, job.filename, job.error, parameterSummary(job)].some((value) =>
      value.toLowerCase().includes(keyword)
    )
  })
})

const summary = computed(() => ({
  total: jobs.value.length,
  running: jobs.value.filter((job) => job.status === 'queued' || job.status === 'running').length,
  succeeded: jobs.value.filter((job) => job.status === 'succeeded').length,
  failed: jobs.value.filter((job) => job.status === 'failed').length
}))

async function load() {
  loading.value = true
  error.value = ''
  try {
    jobs.value = await getExportJobs(100)
    schedulePoll()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

function schedulePoll() {
  if (pollTimer) window.clearTimeout(pollTimer)
  const hasActive = jobs.value.some((job) => job.status === 'queued' || job.status === 'running')
  if (hasActive) {
    pollTimer = window.setTimeout(() => void load(), 1500)
  }
}

function openCreate() {
  Object.assign(form, {
    kind: 'usage',
    q: '',
    model: '',
    status: '',
    action: '',
    resource_type: '',
    from: '',
    to: '',
    limit: 50000
  })
  showCreate.value = true
  error.value = ''
  message.value = ''
}

function closeCreate() {
  showCreate.value = false
}

async function createJob() {
  creating.value = true
  error.value = ''
  message.value = ''
  try {
    const params: RecordListQuery = {
      limit: form.limit,
      q: form.q.trim() || undefined,
      from: datetimeLocalToISOString(form.from),
      to: datetimeLocalToISOString(form.to)
    }
    if (form.kind === 'audit_logs') {
      params.action = form.action.trim() || undefined
      params.resource_type = form.resource_type.trim() || undefined
    } else {
      params.model = form.model.trim() || undefined
      params.status = form.status.trim() || undefined
    }
    await createExportJob(form.kind, params)
    message.value = t('exports.created')
    showCreate.value = false
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    creating.value = false
  }
}

async function download(job: ExportJob) {
  downloadingID.value = job.id
  error.value = ''
  try {
    await downloadExportJob(job)
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    downloadingID.value = ''
  }
}

function kindLabel(kind: ExportJobKind): string {
  return t(`exports.kinds.${kind}`)
}

function statusLabel(status: string): string {
  return t(`exports.statuses.${status}`)
}

function statusClass(status: string): string {
  if (status === 'succeeded') return 'status-success'
  if (status === 'failed') return 'status-danger'
  return 'status-warning'
}

function parameterSummary(job: ExportJob): string {
  return Object.entries(job.parameters || {})
    .map(([key, value]) => `${key}=${value}`)
    .join(' · ')
}

function formatTime(value: string): string {
  return new Date(value).toLocaleString()
}

function formatSize(value: number): string {
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`
  return `${(value / 1024 / 1024).toFixed(1)} MB`
}

onMounted(load)
onBeforeUnmount(() => {
  if (pollTimer) window.clearTimeout(pollTimer)
})
</script>

<template>
  <main class="content crud-page">
    <section class="page-header">
      <div>
        <h1>{{ t('admin.exports') }}</h1>
        <p>{{ t('exports.subtitle') }}</p>
      </div>
      <div class="row-actions">
        <button class="button secondary" type="button" :disabled="loading" @click="load">
          <RefreshCw :size="17" />
          {{ t('common.refresh') }}
        </button>
        <button class="button" type="button" @click="openCreate">
          <Plus :size="17" />
          {{ t('exports.create') }}
        </button>
      </div>
    </section>

    <div v-if="message" class="notice success">{{ message }}</div>
    <div v-if="error" class="notice">{{ error }}</div>

    <div class="crud-summary">
      <span><strong>{{ summary.total }}</strong>{{ t('exports.jobs') }}</span>
      <span><strong>{{ summary.running }}</strong>{{ t('exports.running') }}</span>
      <span><strong>{{ summary.succeeded }}</strong>{{ t('exports.succeeded') }}</span>
      <span><strong>{{ summary.failed }}</strong>{{ t('exports.failed') }}</span>
    </div>

    <section class="table-toolbar">
      <label class="search-box">
        <Search :size="17" />
        <input v-model="query" :placeholder="t('exports.searchPlaceholder')" />
      </label>
      <select v-model="kindFilter">
        <option value="">{{ t('exports.allKinds') }}</option>
        <option value="usage">{{ t('exports.kinds.usage') }}</option>
        <option value="gateway_traces">{{ t('exports.kinds.gateway_traces') }}</option>
        <option value="audit_logs">{{ t('exports.kinds.audit_logs') }}</option>
      </select>
      <select v-model="statusFilter">
        <option value="">{{ t('exports.allStatuses') }}</option>
        <option value="queued">{{ t('exports.statuses.queued') }}</option>
        <option value="running">{{ t('exports.statuses.running') }}</option>
        <option value="succeeded">{{ t('exports.statuses.succeeded') }}</option>
        <option value="failed">{{ t('exports.statuses.failed') }}</option>
      </select>
    </section>

    <section class="panel table-panel content-fit">
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead>
            <tr>
              <th>{{ t('exports.job') }}</th>
              <th>{{ t('exports.kind') }}</th>
              <th>{{ t('providers.status') }}</th>
              <th>{{ t('exports.result') }}</th>
              <th>{{ t('exports.range') }}</th>
              <th>{{ t('audit.time') }}</th>
              <th>{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="job in filteredJobs" :key="job.id">
              <td>
                <strong>{{ job.filename }}</strong>
                <span>{{ job.id }}</span>
              </td>
              <td>{{ kindLabel(job.kind) }}</td>
              <td>
                <span class="pill" :class="statusClass(job.status)">{{ statusLabel(job.status) }}</span>
                <span v-if="job.error">{{ job.error }}</span>
              </td>
              <td>
                <strong>{{ job.row_count }} {{ t('exports.rows') }}</strong>
                <span>{{ formatSize(job.size_bytes) }}</span>
              </td>
              <td>
                <span>{{ parameterSummary(job) || t('exports.allData') }}</span>
              </td>
              <td>
                <strong>{{ formatTime(job.created_at) }}</strong>
                <span>{{ t('exports.expires') }} {{ formatTime(job.expires_at) }}</span>
              </td>
              <td>
                <button
                  class="button secondary"
                  type="button"
                  :disabled="job.status !== 'succeeded' || downloadingID === job.id"
                  @click="download(job)"
                >
                  <Download :size="16" />
                  {{ downloadingID === job.id ? t('exports.downloading') : t('exports.download') }}
                </button>
              </td>
            </tr>
            <tr v-if="!filteredJobs.length">
              <td colspan="7" class="empty-cell">{{ loading ? t('common.loading') : t('exports.empty') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <div v-if="showCreate" class="modal-backdrop" @click.self="closeCreate">
      <section class="modal-card">
        <header class="modal-header">
          <div>
            <h2>{{ t('exports.create') }}</h2>
            <p>{{ t('exports.createSubtitle') }}</p>
          </div>
          <button
            class="icon-button"
            type="button"
            :aria-label="t('common.cancel')"
            :title="t('common.cancel')"
            @click="closeCreate"
          >
            <X :size="19" />
          </button>
        </header>

        <div class="modal-body">
          <div class="form-grid">
            <label>
              <span>{{ t('exports.kind') }}</span>
              <select v-model="form.kind">
                <option value="usage">{{ t('exports.kinds.usage') }}</option>
                <option value="gateway_traces">{{ t('exports.kinds.gateway_traces') }}</option>
                <option value="audit_logs">{{ t('exports.kinds.audit_logs') }}</option>
              </select>
            </label>
            <label>
              <span>{{ t('exports.limit') }}</span>
              <input v-model.number="form.limit" type="number" min="1" max="50000" />
            </label>
            <label class="form-span-2">
              <span>{{ t('exports.keyword') }}</span>
              <input v-model="form.q" type="text" :placeholder="t('exports.keywordPlaceholder')" />
            </label>
            <template v-if="form.kind === 'audit_logs'">
              <label>
                <span>{{ t('audit.action') }}</span>
                <input v-model="form.action" type="text" :placeholder="t('exports.actionPlaceholder')" />
              </label>
              <label>
                <span>{{ t('audit.resource') }}</span>
                <input
                  v-model="form.resource_type"
                  type="text"
                  :placeholder="t('exports.resourcePlaceholder')"
                />
              </label>
            </template>
            <template v-else>
              <label>
                <span>{{ t('usage.model') }}</span>
                <input v-model="form.model" type="text" :placeholder="t('exports.modelPlaceholder')" />
              </label>
              <label>
                <span>{{ t('providers.status') }}</span>
                <input v-model="form.status" type="text" :placeholder="t('exports.statusPlaceholder')" />
              </label>
            </template>
            <label>
              <span>{{ t('common.from') }}</span>
              <input v-model="form.from" type="datetime-local" />
            </label>
            <label>
              <span>{{ t('common.to') }}</span>
              <input v-model="form.to" type="datetime-local" />
            </label>
          </div>
        </div>

        <footer class="modal-footer">
          <button class="button secondary" type="button" @click="closeCreate">{{ t('common.cancel') }}</button>
          <button class="button" type="button" :disabled="creating" @click="createJob">
            <Save :size="17" />
            {{ creating ? t('common.saving') : t('exports.create') }}
          </button>
        </footer>
      </section>
    </div>
  </main>
</template>
