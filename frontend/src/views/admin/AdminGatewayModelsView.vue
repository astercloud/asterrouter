<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Boxes, Edit3, Plus, RefreshCw, Save, Search, Trash2, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { createGatewayModel, deleteGatewayModel, getGatewayModels, updateGatewayModel } from '@/api/control'
import type { GatewayModel, GatewayModelRequest } from '@/types'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const message = ref('')
const models = ref<GatewayModel[]>([])
const query = ref('')
const statusFilter = ref('')
const modalOpen = ref(false)
const editing = ref<GatewayModel | null>(null)
const form = reactive<GatewayModelRequest>({
  model_id: '', name: '', description: '', modality: 'chat', default_route_group: 'default', sticky_enabled: false, sticky_ttl_seconds: 1800, status: 'active'
})

const filteredModels = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return models.value.filter((model) => {
    if (statusFilter.value && model.status !== statusFilter.value) return false
    return !keyword || [model.model_id, model.name, model.modality, model.default_route_group].some((value) => value.toLowerCase().includes(keyword))
  })
})

const summary = computed(() => ({
  total: models.value.length,
  active: models.value.filter((model) => model.status === 'active').length,
  routes: models.value.reduce((sum, model) => sum + model.route_count, 0)
}))

function resetForm() {
  Object.assign(form, { model_id: '', name: '', description: '', modality: 'chat', default_route_group: 'default', sticky_enabled: false, sticky_ttl_seconds: 1800, status: 'active' })
}

function openCreate() {
  editing.value = null
  resetForm()
  modalOpen.value = true
}

function openEdit(model: GatewayModel) {
  editing.value = model
  Object.assign(form, {
    model_id: model.model_id,
    name: model.name,
    description: model.description,
    modality: model.modality,
    default_route_group: model.default_route_group,
    sticky_enabled: model.sticky_enabled,
    sticky_ttl_seconds: model.sticky_ttl_seconds,
    status: model.status
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
    models.value = await getGatewayModels()
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
      await updateGatewayModel(editing.value.id, { ...form })
      message.value = t('gatewayModels.updated')
    } else {
      await createGatewayModel({ ...form })
      message.value = t('gatewayModels.created')
    }
    closeModal()
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

async function remove(model: GatewayModel) {
  if (!window.confirm(t('gatewayModels.deleteConfirm', { model: model.model_id }))) return
  error.value = ''
  message.value = ''
  try {
    await deleteGatewayModel(model.id)
    message.value = t('gatewayModels.deleted')
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  }
}

onMounted(load)
</script>

<template>
  <main class="content crud-page">
    <section class="page-header">
      <div><h1>{{ t('admin.gatewayModels') }}</h1><p>{{ t('gatewayModels.subtitle') }}</p></div>
      <button class="button" type="button" @click="openCreate"><Plus :size="17" />{{ t('gatewayModels.newModel') }}</button>
    </section>

    <div class="crud-summary">
      <span><strong>{{ summary.total }}</strong>{{ t('gatewayModels.models') }}</span>
      <span><strong>{{ summary.active }}</strong>{{ t('dashboard.active') }}</span>
      <span><strong>{{ summary.routes }}</strong>{{ t('gatewayModels.routes') }}</span>
    </div>

    <section class="table-toolbar">
      <label class="search-box"><Search :size="17" /><input v-model="query" :placeholder="t('gatewayModels.searchPlaceholder')" /></label>
      <select v-model="statusFilter"><option value="">{{ t('providers.allStatuses') }}</option><option value="active">active</option><option value="disabled">disabled</option></select>
      <button class="button secondary" type="button" :disabled="loading" @click="load"><RefreshCw :size="17" />{{ t('common.refresh') }}</button>
    </section>

    <div v-if="message" class="notice success">{{ message }}</div>
    <div v-if="error" class="notice">{{ error }}</div>

    <section class="panel table-panel">
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead><tr><th>{{ t('gatewayModels.modelId') }}</th><th>{{ t('gatewayModels.modality') }}</th><th>{{ t('gatewayModels.defaultRouteGroup') }}</th><th>{{ t('gatewayModels.routes') }}</th><th>{{ t('providers.status') }}</th><th>{{ t('common.actions') }}</th></tr></thead>
          <tbody>
            <tr v-for="model in filteredModels" :key="model.id">
              <td><strong>{{ model.model_id }}</strong><span>{{ model.name }}</span></td>
              <td><span class="pill"><Boxes :size="13" />{{ model.modality }}</span></td>
              <td><code>{{ model.default_route_group }}</code></td>
              <td>{{ model.route_count }}</td>
              <td><span class="pill" :class="model.status === 'active' ? 'status-success' : 'status-danger'">{{ model.status }}</span></td>
              <td class="table-actions">
                <button class="icon-button" type="button" :title="t('common.edit')" @click="openEdit(model)"><Edit3 :size="16" /></button>
                <button class="icon-button danger" type="button" :title="t('gatewayModels.delete')" @click="remove(model)"><Trash2 :size="16" /></button>
              </td>
            </tr>
            <tr v-if="!filteredModels.length"><td colspan="6" class="empty-cell">{{ t('gatewayModels.empty') }}</td></tr>
          </tbody>
        </table>
      </div>
    </section>

    <div v-if="modalOpen" class="modal-backdrop">
      <form class="modal-card" @submit.prevent="save">
        <header class="modal-header"><div><h2>{{ editing ? t('gatewayModels.editModel') : t('gatewayModels.newModel') }}</h2><p>{{ t('gatewayModels.modalSubtitle') }}</p></div><button class="icon-button" type="button" @click="closeModal"><X :size="18" /></button></header>
        <div class="modal-body form-grid">
          <div class="field"><label>{{ t('gatewayModels.modelId') }}</label><input v-model="form.model_id" required placeholder="gateway-chat" /></div>
          <div class="field"><label>{{ t('gatewayModels.name') }}</label><input v-model="form.name" required /></div>
          <div class="field"><label>{{ t('gatewayModels.modality') }}</label><select v-model="form.modality"><option v-for="item in ['chat','embedding','image','video','audio','multimodal']" :key="item" :value="item">{{ item }}</option></select></div>
          <div class="field"><label>{{ t('gatewayModels.defaultRouteGroup') }}</label><input v-model="form.default_route_group" required placeholder="default" /></div>
          <div class="field"><label>{{ t('gatewayModels.stickyTTL') }}</label><input v-model.number="form.sticky_ttl_seconds" type="number" min="60" max="604800" /></div>
          <label class="field checkbox-line"><input v-model="form.sticky_enabled" type="checkbox" /><span>{{ t('gatewayModels.stickyEnabled') }}</span></label>
          <div class="field field-wide"><label>{{ t('gatewayModels.description') }}</label><textarea v-model="form.description" rows="3" /></div>
          <div class="field"><label>{{ t('providers.status') }}</label><select v-model="form.status"><option value="active">active</option><option value="disabled">disabled</option></select></div>
        </div>
        <footer class="modal-footer"><button class="button secondary" type="button" @click="closeModal">{{ t('common.cancel') }}</button><button class="button" type="submit" :disabled="saving"><Save :size="17" />{{ saving ? t('common.saving') : t('common.save') }}</button></footer>
      </form>
    </div>
  </main>
</template>
