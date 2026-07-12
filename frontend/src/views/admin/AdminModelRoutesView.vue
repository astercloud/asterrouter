<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { Edit3, Plus, RefreshCw, Route, Save, Search, Trash2, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { createModelRoute, deleteModelRoute, getGatewayModels, getModelRoutes, getProviderAccounts, updateModelRoute } from '@/api/control'
import type { GatewayModel, ModelRoute, ModelRouteRequest, ProviderAccount } from '@/types'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const message = ref('')
const routes = ref<ModelRoute[]>([])
const models = ref<GatewayModel[]>([])
const accounts = ref<ProviderAccount[]>([])
const query = ref('')
const modelFilter = ref('')
const modalOpen = ref(false)
const editing = ref<ModelRoute | null>(null)
const form = reactive<ModelRouteRequest>({ gateway_model_id: '', route_group: 'default', provider_account_id: '', upstream_model: '', priority: 100, weight: 100, status: 'active' })

const modelById = computed(() => Object.fromEntries(models.value.map((model) => [model.id, model])))
const accountById = computed(() => Object.fromEntries(accounts.value.map((account) => [account.id, account])))
const selectedAccount = computed(() => accountById.value[form.provider_account_id])
const filteredRoutes = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return routes.value.filter((route) => {
    if (modelFilter.value && route.gateway_model_id !== modelFilter.value) return false
    const model = modelById.value[route.gateway_model_id]
    const account = accountById.value[route.provider_account_id]
    return !keyword || [model?.model_id || '', route.route_group, account?.name || '', route.upstream_model].some((value) => value.toLowerCase().includes(keyword))
  })
})

watch(() => form.provider_account_id, () => {
  if (!editing.value && selectedAccount.value?.models.length === 1) form.upstream_model = selectedAccount.value.models[0]
})

function resetForm() {
  Object.assign(form, { gateway_model_id: models.value[0]?.id || '', route_group: models.value[0]?.default_route_group || 'default', provider_account_id: accounts.value[0]?.id || '', upstream_model: '', priority: 100, weight: 100, status: 'active' })
}

function openCreate() { editing.value = null; resetForm(); modalOpen.value = true }
function openEdit(route: ModelRoute) { editing.value = route; Object.assign(form, route); modalOpen.value = true }
function closeModal() { modalOpen.value = false; editing.value = null }

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [routeData, modelData, accountData] = await Promise.all([getModelRoutes(), getGatewayModels(), getProviderAccounts()])
    routes.value = routeData
    models.value = modelData
    accounts.value = accountData
  } catch (err) { error.value = err instanceof Error ? err.message : t('common.failed') } finally { loading.value = false }
}

async function save() {
  saving.value = true; error.value = ''; message.value = ''
  try {
    if (editing.value) { await updateModelRoute(editing.value.id, { ...form }); message.value = t('modelRoutes.updated') }
    else { await createModelRoute({ ...form }); message.value = t('modelRoutes.created') }
    closeModal(); await load()
  } catch (err) { error.value = err instanceof Error ? err.message : t('common.failed') } finally { saving.value = false }
}

async function remove(route: ModelRoute) {
  if (!window.confirm(t('modelRoutes.deleteConfirm'))) return
  try { await deleteModelRoute(route.id); message.value = t('modelRoutes.deleted'); await load() }
  catch (err) { error.value = err instanceof Error ? err.message : t('common.failed') }
}

onMounted(load)
</script>

<template>
  <main class="content crud-page">
    <section class="page-header"><div><h1>{{ t('admin.modelRoutes') }}</h1><p>{{ t('modelRoutes.subtitle') }}</p></div><button class="button" type="button" :disabled="!models.length || !accounts.length" @click="openCreate"><Plus :size="17" />{{ t('modelRoutes.newRoute') }}</button></section>
    <div class="crud-summary"><span><strong>{{ routes.length }}</strong>{{ t('modelRoutes.routes') }}</span><span><strong>{{ routes.filter((route) => route.status === 'active').length }}</strong>{{ t('dashboard.active') }}</span><span><strong>{{ new Set(routes.map((route) => route.route_group)).size }}</strong>{{ t('modelRoutes.routeGroups') }}</span></div>
    <section class="table-toolbar">
      <label class="search-box"><Search :size="17" /><input v-model="query" :placeholder="t('modelRoutes.searchPlaceholder')" /></label>
      <select v-model="modelFilter"><option value="">{{ t('modelRoutes.allModels') }}</option><option v-for="model in models" :key="model.id" :value="model.id">{{ model.model_id }}</option></select>
      <button class="button secondary" type="button" :disabled="loading" @click="load"><RefreshCw :size="17" />{{ t('common.refresh') }}</button>
    </section>
    <div v-if="message" class="notice success">{{ message }}</div><div v-if="error" class="notice">{{ error }}</div>
    <section class="panel table-panel"><div class="panel-body table-scroll"><table class="data-table crud-table">
      <thead><tr><th>{{ t('modelRoutes.gatewayModel') }}</th><th>{{ t('modelRoutes.routeGroup') }}</th><th>{{ t('modelRoutes.account') }}</th><th>{{ t('modelRoutes.upstreamModel') }}</th><th>{{ t('modelRoutes.order') }}</th><th>{{ t('providers.status') }}</th><th>{{ t('common.actions') }}</th></tr></thead>
      <tbody>
        <tr v-for="route in filteredRoutes" :key="route.id">
          <td><strong>{{ modelById[route.gateway_model_id]?.model_id || route.gateway_model_id }}</strong><span>{{ modelById[route.gateway_model_id]?.name }}</span></td>
          <td><span class="pill"><Route :size="13" />{{ route.route_group }}</span></td>
          <td><strong>{{ accountById[route.provider_account_id]?.name || route.provider_account_id }}</strong><span>{{ route.provider_account_id }}</span></td>
          <td><code>{{ route.upstream_model }}</code></td><td><strong>P{{ route.priority }}</strong><span>W{{ route.weight }}</span></td>
          <td><span class="pill" :class="route.status === 'active' ? 'status-success' : 'status-danger'">{{ route.status }}</span></td>
          <td class="table-actions"><button class="icon-button" type="button" :title="t('common.edit')" @click="openEdit(route)"><Edit3 :size="16" /></button><button class="icon-button danger" type="button" :title="t('modelRoutes.delete')" @click="remove(route)"><Trash2 :size="16" /></button></td>
        </tr>
        <tr v-if="!filteredRoutes.length"><td colspan="7" class="empty-cell">{{ t('modelRoutes.empty') }}</td></tr>
      </tbody>
    </table></div></section>
    <div v-if="modalOpen" class="modal-backdrop"><form class="modal-card" @submit.prevent="save">
      <header class="modal-header"><div><h2>{{ editing ? t('modelRoutes.editRoute') : t('modelRoutes.newRoute') }}</h2><p>{{ t('modelRoutes.modalSubtitle') }}</p></div><button class="icon-button" type="button" @click="closeModal"><X :size="18" /></button></header>
      <div class="modal-body form-grid">
        <div class="field"><label>{{ t('modelRoutes.gatewayModel') }}</label><select v-model="form.gateway_model_id" required><option v-for="model in models" :key="model.id" :value="model.id">{{ model.model_id }}</option></select></div>
        <div class="field"><label>{{ t('modelRoutes.routeGroup') }}</label><input v-model="form.route_group" required placeholder="default" /></div>
        <div class="field"><label>{{ t('modelRoutes.account') }}</label><select v-model="form.provider_account_id" required><option v-for="account in accounts" :key="account.id" :value="account.id">{{ account.name }}</option></select></div>
        <div class="field"><label>{{ t('modelRoutes.upstreamModel') }}</label><select v-if="selectedAccount?.models.length" v-model="form.upstream_model" required><option v-for="model in selectedAccount.models" :key="model" :value="model">{{ model }}</option></select><input v-else v-model="form.upstream_model" required /></div>
        <div class="field"><label>{{ t('modelRoutes.priority') }}</label><input v-model.number="form.priority" type="number" min="0" required /></div>
        <div class="field"><label>{{ t('modelRoutes.weight') }}</label><input v-model.number="form.weight" type="number" min="1" max="10000" required /></div>
        <div class="field"><label>{{ t('providers.status') }}</label><select v-model="form.status"><option value="active">active</option><option value="disabled">disabled</option></select></div>
      </div>
      <footer class="modal-footer"><button class="button secondary" type="button" @click="closeModal">{{ t('common.cancel') }}</button><button class="button" type="submit" :disabled="saving"><Save :size="17" />{{ saving ? t('common.saving') : t('common.save') }}</button></footer>
    </form></div>
  </main>
</template>
