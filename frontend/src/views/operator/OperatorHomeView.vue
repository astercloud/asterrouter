<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Activity, Code2, KeyRound, RadioTower, RefreshCw, Route, Server, WalletCards } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { getAPIKeys, getDashboard, getProviderAccounts, getRoutingGroups, getUsageReport } from '@/api/control'
import type { APIKeyRecord, Dashboard, ProviderAccount, RoutingGroup, UsageReport } from '@/types'

const { t } = useI18n()
const route = useRoute()
const loading = ref(false)
const error = ref('')
const dashboard = ref<Dashboard | null>(null)
const routingGroups = ref<RoutingGroup[]>([])
const routeResources = ref<ProviderAccount[]>([])
const apiKeys = ref<APIKeyRecord[]>([])
const usage = ref<UsageReport | null>(null)

const activeResources = computed(() => routeResources.value.filter((item) => item.status === 'active').length)
const schedulableResources = computed(() => routeResources.value.filter((item) => item.schedulable).length)
const activeGroups = computed(() => routingGroups.value.filter((item) => item.status === 'active').length)
const activeKeys = computed(() => apiKeys.value.filter((item) => item.status === 'active').length)
const activePanel = computed(() => (typeof route.meta.operatorPanel === 'string' ? route.meta.operatorPanel : 'overview'))
const sortedGroups = computed(() =>
  [...routingGroups.value].sort((a, b) => {
    if (a.status !== b.status) return a.status === 'active' ? -1 : 1
    if (a.sort_order !== b.sort_order) return a.sort_order - b.sort_order
    return a.name.localeCompare(b.name)
  })
)
const sortedResources = computed(() =>
  [...routeResources.value].sort((a, b) => {
    if (a.status !== b.status) return a.status === 'active' ? -1 : 1
    if (a.schedulable !== b.schedulable) return a.schedulable ? -1 : 1
    if (a.priority !== b.priority) return a.priority - b.priority
    return a.name.localeCompare(b.name)
  })
)

function formatNumber(value?: number): string {
  return new Intl.NumberFormat().format(value || 0)
}

function formatCost(cents?: number): string {
  return new Intl.NumberFormat(undefined, { style: 'currency', currency: 'USD' }).format((cents || 0) / 100)
}

function formatDate(value?: string): string {
  return value ? new Date(value).toLocaleString() : '-'
}

function formatLimit(value: number): string {
  return value > 0 ? formatNumber(value) : t('apiKeys.unlimited')
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [dashboardData, groups, resources, keys, usageReport] = await Promise.all([
      getDashboard(),
      getRoutingGroups(),
      getProviderAccounts(),
      getAPIKeys(),
      getUsageReport()
    ])
    dashboard.value = dashboardData
    routingGroups.value = groups
    routeResources.value = resources
    apiKeys.value = keys
    usage.value = usageReport
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <main class="content">
      <section class="page-header">
        <div>
          <h1>{{ t('operator.title') }}</h1>
          <p>{{ t('operator.subtitle') }}</p>
        </div>
        <div class="row-actions">
          <button class="button secondary" type="button" :disabled="loading" @click="load">
            <RefreshCw :size="17" />
            {{ t('common.refresh') }}
          </button>
        </div>
      </section>

      <div v-if="error" class="notice">{{ error }}</div>

      <section v-if="activePanel === 'overview' || activePanel === 'usage'" class="metric-grid">
        <article class="metric-card">
          <span class="metric-icon"><Server :size="18" /></span>
          <div>
            <span>{{ t('operator.providers') }}</span>
            <strong>{{ dashboard?.provider_count || 0 }}</strong>
            <small>{{ dashboard?.active_provider_count || 0 }} {{ t('providers.active') }}</small>
          </div>
        </article>
        <article class="metric-card">
          <span class="metric-icon"><Route :size="18" /></span>
          <div>
            <span>{{ t('operator.routeResources') }}</span>
            <strong>{{ activeResources }}</strong>
            <small>{{ schedulableResources }} {{ t('providerAccounts.schedulable') }}</small>
          </div>
        </article>
        <article class="metric-card">
          <span class="metric-icon"><KeyRound :size="18" /></span>
          <div>
            <span>{{ t('operator.customerKeys') }}</span>
            <strong>{{ activeKeys }}</strong>
            <small>{{ apiKeys.length }} {{ t('admin.apiKeys') }}</small>
          </div>
        </article>
        <article class="metric-card">
          <span class="metric-icon"><WalletCards :size="18" /></span>
          <div>
            <span>{{ t('operator.cost') }}</span>
            <strong>{{ formatCost(usage?.total_cost_cents) }}</strong>
            <small>{{ formatNumber(usage?.total_requests) }} {{ t('usage.requests') }}</small>
          </div>
        </article>
      </section>

      <section v-if="activePanel === 'overview'" class="grid section-gap">
        <section class="panel">
          <div class="panel-header split-header">
            <div>
              <h2>{{ t('operator.dispatch') }}</h2>
              <p>{{ t('operator.dispatchHelp') }}</p>
            </div>
            <RadioTower :size="18" />
          </div>
          <div class="panel-body">
            <div class="status-line">
              <span class="pill">{{ routingGroups.length }} {{ t('admin.routingGroups') }}</span>
              <span class="pill">{{ activeGroups }} {{ t('dashboard.active') }}</span>
              <span class="pill">{{ routeResources.length }} {{ t('admin.providerAccounts') }}</span>
              <span class="pill">{{ dashboard?.models.length || 0 }} {{ t('dashboard.models') }}</span>
            </div>
            <div class="row-actions">
              <RouterLink class="button secondary" to="/operator/routing-groups">{{ t('operator.groupList') }}</RouterLink>
              <RouterLink class="button secondary" to="/operator/resources">{{ t('operator.resourceList') }}</RouterLink>
            </div>
          </div>
        </section>

        <section class="panel">
          <div class="panel-header split-header">
            <div>
              <h2>{{ t('operator.traffic') }}</h2>
              <p>{{ t('operator.trafficHelp') }}</p>
            </div>
            <Activity :size="18" />
          </div>
          <div class="panel-body">
            <div class="status-line">
              <span class="pill">{{ formatNumber(usage?.total_tokens) }} {{ t('usage.tokens') }}</span>
              <span class="pill">{{ formatNumber(usage?.error_requests) }} {{ t('usage.errors') }}</span>
            </div>
            <div class="row-actions">
              <RouterLink class="button secondary" to="/operator/keys">{{ t('operator.keyList') }}</RouterLink>
              <RouterLink class="button secondary" to="/operator/usage">{{ t('operator.traffic') }}</RouterLink>
            </div>
          </div>
        </section>
      </section>

      <section v-if="activePanel === 'routing-groups'" class="panel section-gap">
          <div class="panel-header split-header">
            <div>
              <h2>{{ t('operator.groupList') }}</h2>
              <p>{{ t('operator.groupSummary') }}</p>
            </div>
            <Route :size="18" />
          </div>
          <div class="panel-body table-scroll">
            <table class="data-table crud-table">
              <thead>
                <tr>
                  <th>{{ t('routingGroups.name') }}</th>
                  <th>{{ t('routingGroups.platform') }}</th>
                  <th>{{ t('routingGroups.accounts') }}</th>
                  <th>{{ t('routingGroups.sortOrder') }}</th>
                  <th>{{ t('providers.status') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="group in sortedGroups" :key="group.id">
                  <td>
                    <strong>{{ group.name }}</strong>
                    <span>{{ group.description || '-' }}</span>
                  </td>
                  <td>{{ group.platform || '-' }}</td>
                  <td>
                    <strong>{{ group.active_account_count }}</strong>
                    <span>{{ group.account_count }} {{ t('admin.providerAccounts') }}</span>
                  </td>
                  <td>{{ group.sort_order }}</td>
                  <td><span class="pill" :class="group.status === 'active' ? 'status-success' : 'status-warning'">{{ group.status }}</span></td>
                </tr>
                <tr v-if="!sortedGroups.length">
                  <td colspan="5" class="empty-cell">{{ loading ? t('common.loading') : t('operator.emptyGroups') }}</td>
                </tr>
              </tbody>
            </table>
          </div>
      </section>

      <section v-if="activePanel === 'resources'" class="panel section-gap">
          <div class="panel-header split-header">
            <div>
              <h2>{{ t('operator.resourceList') }}</h2>
              <p>{{ t('operator.resourceSummary') }}</p>
            </div>
            <RadioTower :size="18" />
          </div>
          <div class="panel-body table-scroll">
            <table class="data-table crud-table">
              <thead>
                <tr>
                  <th>{{ t('providerAccounts.name') }}</th>
                  <th>{{ t('providerAccounts.provider') }}</th>
                  <th>{{ t('providerAccounts.authType') }}</th>
                  <th>{{ t('providers.models') }}</th>
                  <th>{{ t('providerAccounts.capacity') }}</th>
                  <th>{{ t('providers.status') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="resource in sortedResources" :key="resource.id">
                  <td>
                    <strong>{{ resource.name }}</strong>
                    <span>{{ resource.platform || '-' }} · {{ resource.secret_configured ? t('providerAccounts.secret') : t('providerAccounts.error') }}</span>
                  </td>
                  <td>{{ resource.provider_id }}</td>
                  <td>{{ resource.auth_type }}</td>
                  <td>
                    <span>{{ resource.models.length ? resource.models.slice(0, 3).join(', ') : t('apiKeys.unlimited') }}</span>
                    <span v-if="resource.models.length > 3">+{{ resource.models.length - 3 }}</span>
                  </td>
                  <td>
                    <strong>{{ resource.concurrency }}</strong>
                    <span>{{ t('providerAccounts.multiplier') }} {{ resource.rate_multiplier }}</span>
                  </td>
                  <td>
                    <span class="pill" :class="resource.status === 'active' ? 'status-success' : 'status-warning'">{{ resource.status }}</span>
                    <span>{{ resource.schedulable ? t('providerAccounts.schedulable') : t('providerAccounts.notSchedulable') }}</span>
                  </td>
                </tr>
                <tr v-if="!sortedResources.length">
                  <td colspan="6" class="empty-cell">{{ loading ? t('common.loading') : t('operator.emptyResources') }}</td>
                </tr>
              </tbody>
            </table>
          </div>
      </section>

      <section v-if="activePanel === 'keys'" class="panel section-gap">
        <div class="panel-header split-header">
          <div>
            <h2>{{ t('operator.keyList') }}</h2>
            <p>{{ t('operator.keySummary') }}</p>
          </div>
          <KeyRound :size="18" />
        </div>
        <div class="panel-body table-scroll">
          <table class="data-table crud-table">
            <thead>
              <tr>
                <th>{{ t('apiKeys.name') }}</th>
                <th>{{ t('apiKeys.models') }}</th>
                <th>{{ t('apiKeys.limits') }}</th>
                <th>{{ t('providers.status') }}</th>
                <th>{{ t('apiKeys.lastUsed') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="key in apiKeys.slice(0, 10)" :key="key.id">
                <td>
                  <strong>{{ key.name }}</strong>
                  <span>{{ key.prefix }} · {{ key.fingerprint }}</span>
                </td>
                <td>{{ key.model_allowlist.join(', ') || t('apiKeys.unlimited') }}</td>
                <td>
                  <strong>{{ formatLimit(key.qps_limit) }} QPS</strong>
                  <span>{{ formatLimit(key.monthly_token_limit) }} {{ t('usage.tokens') }}</span>
                </td>
                <td><span class="pill" :class="key.status === 'active' ? 'status-success' : 'status-warning'">{{ key.status }}</span></td>
                <td>{{ formatDate(key.last_used_at) }}</td>
              </tr>
              <tr v-if="!apiKeys.length">
                <td colspan="5" class="empty-cell">{{ loading ? t('common.loading') : t('operator.emptyKeys') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section v-if="activePanel === 'usage'" class="grid section-gap">
        <section class="panel">
          <div class="panel-header split-header">
            <div>
              <h2>{{ t('usage.byModel') }}</h2>
              <p>{{ t('operator.trafficHelp') }}</p>
            </div>
            <Activity :size="18" />
          </div>
          <div class="panel-body table-scroll">
            <table class="data-table">
              <thead>
                <tr>
                  <th>{{ t('usage.model') }}</th>
                  <th>{{ t('usage.requests') }}</th>
                  <th>{{ t('usage.errors') }}</th>
                  <th>{{ t('usage.tokens') }}</th>
                  <th>{{ t('usage.cost') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="item in usage?.by_model || []" :key="item.model">
                  <td><strong>{{ item.model }}</strong></td>
                  <td>{{ formatNumber(item.requests) }}</td>
                  <td>{{ formatNumber(item.errors) }}</td>
                  <td>{{ formatNumber(item.tokens) }}</td>
                  <td>{{ formatCost(item.cost_cents) }}</td>
                </tr>
                <tr v-if="!(usage?.by_model || []).length">
                  <td colspan="5" class="empty-cell"></td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>

        <section class="panel">
          <div class="panel-header split-header">
            <div>
              <h2>{{ t('usage.recentRequests') }}</h2>
              <p>{{ t('usage.subtitle') }}</p>
            </div>
            <Code2 :size="18" />
          </div>
          <div class="panel-body table-scroll">
            <table class="data-table">
              <thead>
                <tr>
                  <th>{{ t('usage.model') }}</th>
                  <th>{{ t('usage.route') }}</th>
                  <th>{{ t('providers.status') }}</th>
                  <th>{{ t('usage.tokens') }}</th>
                  <th>{{ t('usage.createdAt') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="item in usage?.recent || []" :key="item.id">
                  <td><strong>{{ item.model }}</strong></td>
                  <td>
                    <strong>{{ item.provider_id || '-' }}</strong>
                    <span>{{ item.provider_account_id || '-' }}</span>
                  </td>
                  <td><span class="pill" :class="item.status === 'success' ? 'status-success' : 'status-danger'">{{ item.status }}</span></td>
                  <td>{{ formatNumber(item.input_tokens + item.output_tokens) }}</td>
                  <td>{{ formatDate(item.created_at) }}</td>
                </tr>
                <tr v-if="!(usage?.recent || []).length">
                  <td colspan="5" class="empty-cell"></td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
      </section>
  </main>
</template>
