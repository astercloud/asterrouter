<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Edit3, Plus, RefreshCw, Save, Search, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { createRoutingGroup, getRoutingGroups, updateRoutingGroup } from '@/api/control'
import type { RoutingGroup, RoutingGroupRequest } from '@/types'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const message = ref('')
const groups = ref<RoutingGroup[]>([])
const query = ref('')
const statusFilter = ref('')
const platformFilter = ref('')
const modalOpen = ref(false)
const editing = ref<RoutingGroup | null>(null)

const form = reactive<RoutingGroupRequest>({
  name: '',
  description: '',
  platform: 'openai_compatible',
  rate_multiplier: 1,
  status: 'active',
  sort_order: 100
})

const platforms = computed(() => Array.from(new Set(groups.value.map((item) => item.platform))).filter(Boolean).sort())

const filteredGroups = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return groups.value.filter((group) => {
    if (statusFilter.value && group.status !== statusFilter.value) return false
    if (platformFilter.value && group.platform !== platformFilter.value) return false
    if (!keyword) return true
    return [group.name, group.description, group.platform].some((value) => value.toLowerCase().includes(keyword))
  })
})

const summary = computed(() => ({
  total: groups.value.length,
  active: groups.value.filter((item) => item.status === 'active').length,
  disabled: groups.value.filter((item) => item.status === 'disabled').length,
  accounts: groups.value.reduce((total, item) => total + item.account_count, 0),
  schedulable: groups.value.reduce((total, item) => total + item.active_account_count, 0)
}))

function resetForm() {
  Object.assign(form, {
    name: '',
    description: '',
    platform: 'openai_compatible',
    rate_multiplier: 1,
    status: 'active',
    sort_order: 100
  })
}

function openCreate() {
  editing.value = null
  resetForm()
  modalOpen.value = true
}

function openEdit(group: RoutingGroup) {
  editing.value = group
  Object.assign(form, {
    name: group.name,
    description: group.description,
    platform: group.platform,
    rate_multiplier: group.rate_multiplier,
    status: group.status,
    sort_order: group.sort_order
  })
  modalOpen.value = true
}

function closeModal() {
  modalOpen.value = false
  editing.value = null
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    groups.value = await getRoutingGroups()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  error.value = ''
  message.value = ''
  try {
    if (editing.value) {
      await updateRoutingGroup(editing.value.id, { ...form })
      message.value = t('routingGroups.updated')
    } else {
      await createRoutingGroup({ ...form })
      message.value = t('routingGroups.created')
    }
    closeModal()
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

function statusClass(status: string) {
  return status === 'active' ? 'status-success' : 'status-danger'
}

onMounted(load)
</script>

<template>
  <main class="content crud-page">
    <section class="page-header">
      <div>
        <h1>{{ t('admin.routingGroups') }}</h1>
        <p>{{ t('routingGroups.subtitle') }}</p>
      </div>
      <button class="button" type="button" @click="openCreate">
        <Plus :size="17" />
        {{ t('routingGroups.newGroup') }}
      </button>
    </section>

    <div class="notice">{{ t('routingGroups.advancedNotice') }}</div>

    <div class="crud-summary">
      <span><strong>{{ summary.total }}</strong>{{ t('routingGroups.groups') }}</span>
      <span><strong>{{ summary.active }}</strong>{{ t('dashboard.active') }}</span>
      <span><strong>{{ summary.disabled }}</strong>{{ t('providers.disabled') }}</span>
      <span><strong>{{ summary.accounts }}</strong>{{ t('providerAccounts.accounts') }}</span>
      <span><strong>{{ summary.schedulable }}</strong>{{ t('providerAccounts.schedulable') }}</span>
    </div>

    <section class="table-toolbar">
      <label class="search-box">
        <Search :size="17" />
        <input v-model="query" :placeholder="t('routingGroups.searchPlaceholder')" />
      </label>
      <select v-model="platformFilter">
        <option value="">{{ t('routingGroups.allPlatforms') }}</option>
        <option v-for="platform in platforms" :key="platform" :value="platform">{{ platform }}</option>
      </select>
      <select v-model="statusFilter">
        <option value="">{{ t('providers.allStatuses') }}</option>
        <option value="active">active</option>
        <option value="disabled">disabled</option>
      </select>
      <button class="button secondary" type="button" :disabled="loading" @click="load">
        <RefreshCw :size="17" />
        {{ t('common.refresh') }}
      </button>
    </section>

    <div v-if="message" class="notice success">{{ message }}</div>
    <div v-if="error" class="notice">{{ error }}</div>

    <section class="panel table-panel content-fit">
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead>
            <tr>
              <th>{{ t('routingGroups.name') }}</th>
              <th>{{ t('routingGroups.platform') }}</th>
              <th>{{ t('providers.status') }}</th>
              <th>{{ t('routingGroups.rateMultiplier') }}</th>
              <th>{{ t('routingGroups.accounts') }}</th>
              <th>{{ t('routingGroups.sortOrder') }}</th>
              <th>{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="group in filteredGroups" :key="group.id">
              <td>
                <strong>{{ group.name }}</strong>
                <span>{{ group.description || '-' }}</span>
              </td>
              <td>{{ group.platform }}</td>
              <td><span class="pill" :class="statusClass(group.status)">{{ group.status }}</span></td>
              <td>{{ group.rate_multiplier }}</td>
              <td>
                <strong>{{ group.active_account_count }} / {{ group.account_count }}</strong>
                <span>{{ t('providerAccounts.schedulable') }}</span>
              </td>
              <td>{{ group.sort_order }}</td>
              <td>
                <button class="button secondary" type="button" @click="openEdit(group)">
                  <Edit3 :size="15" />
                  {{ t('common.edit') }}
                </button>
              </td>
            </tr>
            <tr v-if="!filteredGroups.length">
              <td colspan="7" class="empty-cell">
                {{ loading ? t('common.loading') : t('routingGroups.empty') }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <div v-if="modalOpen" class="modal-backdrop" @click.self="closeModal">
      <section class="modal-card">
        <header class="modal-header">
          <div>
            <h2>{{ editing ? t('routingGroups.editGroup') : t('routingGroups.newGroup') }}</h2>
            <p>{{ t('routingGroups.modalSubtitle') }}</p>
          </div>
          <button class="icon-button" type="button" @click="closeModal">
            <X :size="19" />
          </button>
        </header>

        <div class="modal-body form-grid">
          <div class="field">
            <label>{{ t('routingGroups.name') }}</label>
            <input v-model="form.name" placeholder="Default OpenAI Pool" />
          </div>
          <div class="field">
            <label>{{ t('routingGroups.platform') }}</label>
            <input v-model="form.platform" placeholder="openai_compatible" />
          </div>
          <div class="field form-span-2">
            <label>{{ t('projects.description') }}</label>
            <input v-model="form.description" />
          </div>
          <div class="field">
            <label>{{ t('routingGroups.rateMultiplier') }}</label>
            <input v-model.number="form.rate_multiplier" type="number" min="0" step="0.01" />
          </div>
          <div class="field">
            <label>{{ t('routingGroups.sortOrder') }}</label>
            <input v-model.number="form.sort_order" type="number" min="0" />
          </div>
          <div class="field form-span-2">
            <label>{{ t('providers.status') }}</label>
            <select v-model="form.status">
              <option value="active">active</option>
              <option value="disabled">disabled</option>
            </select>
          </div>
        </div>

        <footer class="modal-footer">
          <button class="button secondary" type="button" @click="closeModal">{{ t('common.cancel') }}</button>
          <button class="button" type="button" :disabled="saving" @click="save">
            <Save :size="17" />
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </footer>
      </section>
    </div>
  </main>
</template>
