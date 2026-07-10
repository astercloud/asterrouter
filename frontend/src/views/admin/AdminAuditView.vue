<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Download, RefreshCw, Search } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { exportAuditLogsCSV, getAuditLogSummary, getAuditLogs } from '@/api/control'
import type { AuditLog, AuditLogSummary, RecordListQuery } from '@/types'
import { datetimeLocalToISOString } from '@/utils/timeRange'

const { t } = useI18n()
const loading = ref(false)
const error = ref('')
const logs = ref<AuditLog[]>([])
const summary = ref<AuditLogSummary>({ total: 0, actors: 0, resources: 0, actions: 0 })
const query = ref('')
const actionFilter = ref('')
const resourceFilter = ref('')
const fromTime = ref('')
const toTime = ref('')
const pageSize = ref(25)
const offset = ref(0)

const filteredLogs = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return logs.value.filter((log) => {
    if (actionFilter.value && log.action !== actionFilter.value) return false
    if (resourceFilter.value && log.resource_type !== resourceFilter.value) return false
    if (!keyword) return true
    return [log.actor, log.action, log.resource_type, log.resource_id, log.summary].some((value) =>
      value.toLowerCase().includes(keyword)
    )
  })
})

const actionOptions = computed(() => Array.from(new Set(logs.value.map((item) => item.action))).filter(Boolean).sort())
const resourceOptions = computed(() => Array.from(new Set(logs.value.map((item) => item.resource_type))).filter(Boolean).sort())
const pageNumber = computed(() => Math.floor(offset.value / pageSize.value) + 1)
const canPrevious = computed(() => offset.value > 0)
const canNext = computed(() => logs.value.length >= pageSize.value)

async function load() {
  loading.value = true
  error.value = ''
  try {
    const currentQuery = listQuery()
    const [logData, summaryData] = await Promise.all([
      getAuditLogs(currentQuery),
      getAuditLogSummary(currentQuery)
    ])
    logs.value = logData
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
    action: actionFilter.value || undefined,
    resource_type: resourceFilter.value || undefined,
    from: datetimeLocalToISOString(fromTime.value),
    to: datetimeLocalToISOString(toTime.value)
  }
}

async function exportCSV() {
  error.value = ''
  try {
    await exportAuditLogsCSV({ ...listQuery(), limit: 5000, offset: 0 })
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

function formatTime(value: string): string {
  return new Date(value).toLocaleString()
}

onMounted(load)
</script>

<template>
  <main class="content crud-page">
    <section class="page-header">
      <div>
        <h1>{{ t('admin.audit') }}</h1>
        <p>{{ t('audit.subtitle') }}</p>
      </div>
      <div class="row-actions">
        <button class="button secondary" type="button" :disabled="!filteredLogs.length" @click="exportCSV">
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
      <span><strong>{{ summary.total }}</strong>{{ t('audit.events') }}</span>
      <span><strong>{{ summary.actors }}</strong>{{ t('audit.actors') }}</span>
      <span><strong>{{ summary.resources }}</strong>{{ t('audit.resources') }}</span>
      <span><strong>{{ summary.actions }}</strong>{{ t('common.actions') }}</span>
    </div>

    <section class="table-toolbar">
      <label class="search-box">
        <Search :size="17" />
        <input v-model="query" :placeholder="t('audit.searchPlaceholder')" @keyup.enter="applyFilters" />
      </label>
      <select v-model="actionFilter" @change="applyFilters">
        <option value="">{{ t('audit.allActions') }}</option>
        <option v-for="action in actionOptions" :key="action" :value="action">{{ action }}</option>
      </select>
      <select v-model="resourceFilter" @change="applyFilters">
        <option value="">{{ t('audit.allResources') }}</option>
        <option v-for="resource in resourceOptions" :key="resource" :value="resource">{{ resource }}</option>
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
              <th>{{ t('audit.actor') }}</th>
              <th>{{ t('audit.action') }}</th>
              <th>{{ t('audit.resource') }}</th>
              <th>{{ t('audit.summary') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="log in filteredLogs" :key="log.id">
              <td>{{ formatTime(log.created_at) }}</td>
              <td>{{ log.actor }}</td>
              <td><span class="pill">{{ log.action }}</span></td>
              <td>
                <strong>{{ log.resource_type }}</strong>
                <span>{{ log.resource_id }}</span>
              </td>
              <td>{{ log.summary }}</td>
            </tr>
            <tr v-if="!filteredLogs.length">
              <td colspan="5" class="empty-cell">{{ loading ? t('common.loading') : t('audit.empty') }}</td>
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
