<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Boxes, CheckCircle2, Eye, LockKeyhole, Plug, RefreshCw, Search, Settings2, X, XCircle } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { disablePlugin, enablePlugin, getPluginCatalog } from '@/api/plugins'
import type { Plugin, PluginCatalog } from '@/types'

const { t } = useI18n()
const loading = ref(false)
const actionID = ref('')
const error = ref('')
const message = ref('')
const query = ref('')
const categoryFilter = ref('')
const tierFilter = ref('')
const statusFilter = ref('')
const selectedPlugin = ref<Plugin | null>(null)
const catalog = ref<PluginCatalog>({
  summary: { total: 0, enabled: 0, free: 0, paid_locked: 0, configurable: 0 },
  plugins: []
})

const metrics = computed(() => [
  { label: t('plugins.total'), value: catalog.value.summary.total, sub: t('plugins.installed'), icon: Plug },
  { label: t('plugins.enabled'), value: catalog.value.summary.enabled, sub: t('plugins.runtime'), icon: CheckCircle2 },
  { label: t('plugins.free'), value: catalog.value.summary.free, sub: t('plugins.neverCharged'), icon: Boxes },
  { label: t('plugins.paidLocked'), value: catalog.value.summary.paid_locked, sub: t('plugins.requiresLicense'), icon: LockKeyhole }
])

const filteredPlugins = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return catalog.value.plugins.filter((plugin) => {
    if (categoryFilter.value && plugin.category !== categoryFilter.value) return false
    if (tierFilter.value && plugin.tier !== tierFilter.value) return false
    if (statusFilter.value && plugin.status !== statusFilter.value) return false
    if (!keyword) return true
    return [plugin.name, plugin.description, plugin.plugin_id, plugin.category, plugin.vendor, plugin.surfaces.join(' ')].some((value) =>
      value.toLowerCase().includes(keyword)
    )
  })
})

const categoryOptions = computed(() => Array.from(new Set(catalog.value.plugins.map((item) => item.category))).filter(Boolean).sort())
const tierOptions = computed(() => Array.from(new Set(catalog.value.plugins.map((item) => item.tier))).filter(Boolean).sort())
const statusOptions = computed(() => Array.from(new Set(catalog.value.plugins.map((item) => item.status))).filter(Boolean).sort())

async function load() {
  loading.value = true
  error.value = ''
  try {
    catalog.value = await getPluginCatalog()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

async function setEnabled(plugin: Plugin, enabled: boolean) {
  actionID.value = plugin.id
  error.value = ''
  message.value = ''
  try {
    if (enabled) {
      await enablePlugin(plugin.id)
      message.value = t('plugins.enabledMessage')
    } else {
      await disablePlugin(plugin.id)
      message.value = t('plugins.disabledMessage')
    }
    await load()
    const updated = catalog.value.plugins.find((item) => item.id === plugin.id)
    if (updated && selectedPlugin.value?.id === plugin.id) {
      selectedPlugin.value = updated
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    actionID.value = ''
  }
}

function canEnable(plugin: Plugin) {
  return plugin.status !== 'enabled' && plugin.status !== 'locked'
}

function canDisable(plugin: Plugin) {
  return plugin.status === 'enabled' && plugin.tier !== 'core'
}

function statusClass(status: string) {
  if (status === 'enabled') return 'status-success'
  if (status === 'locked') return 'status-warning'
  return 'status-danger'
}

onMounted(load)
</script>

<template>
  <main class="content crud-page">
    <section class="page-header">
      <div>
        <h1>{{ t('admin.plugins') }}</h1>
        <p>{{ t('plugins.subtitle') }}</p>
      </div>
      <button class="button secondary" :disabled="loading" @click="load">
        <RefreshCw :size="17" />
        {{ t('common.refresh') }}
      </button>
    </section>

    <div v-if="message" class="notice success">{{ message }}</div>
    <div v-if="error" class="notice">{{ error }}</div>

    <section class="metric-grid">
      <article v-for="metric in metrics" :key="metric.label" class="metric-card">
        <span class="metric-icon"><component :is="metric.icon" :size="20" /></span>
        <div>
          <span>{{ metric.label }}</span>
          <strong>{{ metric.value }}</strong>
          <small>{{ metric.sub }}</small>
        </div>
      </article>
    </section>

    <section class="table-toolbar">
      <label class="search-box">
        <Search :size="17" />
        <input v-model="query" :placeholder="t('plugins.searchPlaceholder')" />
      </label>
      <select v-model="categoryFilter">
        <option value="">{{ t('plugins.allCategories') }}</option>
        <option v-for="category in categoryOptions" :key="category" :value="category">{{ category }}</option>
      </select>
      <select v-model="tierFilter">
        <option value="">{{ t('plugins.allTiers') }}</option>
        <option v-for="tier in tierOptions" :key="tier" :value="tier">{{ tier }}</option>
      </select>
      <select v-model="statusFilter">
        <option value="">{{ t('providers.allStatuses') }}</option>
        <option v-for="status in statusOptions" :key="status" :value="status">{{ status }}</option>
      </select>
    </section>

    <section class="panel table-panel content-fit">
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead>
            <tr>
              <th>{{ t('plugins.plugin') }}</th>
              <th>{{ t('plugins.category') }}</th>
              <th>{{ t('plugins.tier') }}</th>
              <th>{{ t('plugins.entitlement') }}</th>
              <th>{{ t('plugins.status') }}</th>
              <th>{{ t('plugins.surfaces') }}</th>
              <th>{{ t('plugins.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="plugin in filteredPlugins" :key="plugin.id">
              <td>
                <strong>{{ plugin.name }}</strong>
                <span>{{ plugin.description }}</span>
                <span>{{ plugin.plugin_id }} · v{{ plugin.version }} · {{ plugin.vendor }}</span>
              </td>
              <td><span class="pill">{{ plugin.category }}</span></td>
              <td><span class="pill">{{ plugin.tier }}</span></td>
              <td><span class="pill">{{ plugin.entitlement_status }}</span></td>
              <td><span class="pill" :class="statusClass(plugin.status)">{{ plugin.status }}</span></td>
              <td>
                <div class="chip-list">
                  <span v-for="surface in plugin.surfaces" :key="surface" class="pill">{{ surface }}</span>
                </div>
              </td>
              <td>
                <div class="row-actions">
                  <button class="button secondary" type="button" @click="selectedPlugin = plugin">
                    <Eye :size="15" />
                    {{ t('common.details') }}
                  </button>
                  <button class="button secondary" type="button" :disabled="actionID === plugin.id || !canEnable(plugin)" @click="setEnabled(plugin, true)">
                    <CheckCircle2 :size="15" />
                    {{ t('plugins.enable') }}
                  </button>
                  <button class="button danger" type="button" :disabled="actionID === plugin.id || !canDisable(plugin)" @click="setEnabled(plugin, false)">
                    <XCircle :size="15" />
                    {{ t('plugins.disable') }}
                  </button>
                </div>
              </td>
            </tr>
            <tr v-if="!filteredPlugins.length">
              <td colspan="7" class="empty-cell">{{ loading ? t('common.loading') : t('plugins.empty') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <div v-if="selectedPlugin" class="modal-backdrop" @click.self="selectedPlugin = null">
      <section class="modal-card">
        <header class="modal-header">
          <div>
            <h2>{{ selectedPlugin.name }}</h2>
            <p>{{ selectedPlugin.plugin_id }} · v{{ selectedPlugin.version }} · {{ selectedPlugin.vendor }}</p>
          </div>
          <button class="icon-button" type="button" @click="selectedPlugin = null"><X :size="19" /></button>
        </header>
        <div class="modal-body detail-grid">
          <div>
            <label>{{ t('plugins.description') }}</label>
            <p>{{ selectedPlugin.description }}</p>
          </div>
          <div>
            <label>{{ t('plugins.category') }}</label>
            <p>{{ selectedPlugin.category }} / {{ selectedPlugin.type }}</p>
          </div>
          <div>
            <label>{{ t('plugins.tier') }}</label>
            <p>{{ selectedPlugin.tier }}</p>
          </div>
          <div>
            <label>{{ t('plugins.entitlement') }}</label>
            <p>{{ selectedPlugin.entitlement_status }}</p>
          </div>
          <div>
            <label>{{ t('plugins.entryPoint') }}</label>
            <p>{{ selectedPlugin.entry_point || '-' }}</p>
          </div>
          <div>
            <label>{{ t('plugins.configurable') }}</label>
            <p>{{ selectedPlugin.configurable ? t('common.yes') : t('common.no') }}</p>
          </div>
          <div class="form-span-2">
            <label>{{ t('plugins.surfaces') }}</label>
            <div class="chip-list">
              <span v-for="surface in selectedPlugin.surfaces" :key="surface" class="pill">{{ surface }}</span>
            </div>
          </div>
        </div>
        <footer class="modal-footer">
          <button class="button secondary" type="button" @click="selectedPlugin = null">{{ t('common.cancel') }}</button>
          <button class="button secondary" type="button" :disabled="actionID === selectedPlugin.id || !canEnable(selectedPlugin)" @click="setEnabled(selectedPlugin, true)">
            <CheckCircle2 :size="17" />
            {{ t('plugins.enable') }}
          </button>
          <button class="button danger" type="button" :disabled="actionID === selectedPlugin.id || !canDisable(selectedPlugin)" @click="setEnabled(selectedPlugin, false)">
            <XCircle :size="17" />
            {{ t('plugins.disable') }}
          </button>
        </footer>
      </section>
    </div>
  </main>
</template>
