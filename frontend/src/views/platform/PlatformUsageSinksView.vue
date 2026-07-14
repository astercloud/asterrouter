<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Edit3, History, Link2, Plus, RefreshCw, RotateCw, Send, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { createPlatformUsageSink, getExternalAuthIntegrations, getPlatformTenants, getPlatformUsageDeliveries, getPlatformUsageSinks, requeuePlatformUsageDelivery, rotatePlatformUsageSinkEndpoint, updatePlatformUsageSink } from '@/api/platform'
import type { ExternalAuthIntegration, PlatformTenant, PlatformUsageDeliveryEvent, PlatformUsageSink, PlatformUsageSinkRequest } from '@/types'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const deliveriesLoading = ref(false)
const error = ref('')
const message = ref('')
const modalOpen = ref(false)
const rotateModalOpen = ref(false)
const editing = ref<PlatformUsageSink | null>(null)
const rotating = ref<PlatformUsageSink | null>(null)
const selectedSink = ref<PlatformUsageSink | null>(null)
const deliveryStatus = ref('')
const sinks = ref<PlatformUsageSink[]>([])
const tenants = ref<PlatformTenant[]>([])
const integrations = ref<ExternalAuthIntegration[]>([])
const deliveries = ref<PlatformUsageDeliveryEvent[]>([])
const oneTimeSecret = ref('')
const form = reactive<PlatformUsageSinkRequest>({
  tenant_id: '', external_auth_integration_id: '', name: '', endpoint_url: '', signing_secret: '', status: 'active', max_attempts: 10
})
const rotation = reactive({ endpoint_url: '', signing_secret: '' })

const activeTenants = computed(() => tenants.value.filter((tenant) => tenant.status === 'active'))
const compatibleIntegrations = computed(() => integrations.value.filter((integration) => integration.status === 'active' && integration.tenant_id === form.tenant_id))
const tenantNameByID = computed(() => new Map(tenants.value.map((tenant) => [tenant.id, tenant.name])))
const integrationNameByID = computed(() => new Map(integrations.value.map((integration) => [integration.id, integration.name])))

function formatDate(value?: string) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

function resetForm() {
  const tenantID = activeTenants.value[0]?.id || ''
  Object.assign(form, {
    tenant_id: tenantID,
    external_auth_integration_id: integrations.value.find((integration) => integration.status === 'active' && integration.tenant_id === tenantID)?.id || '',
    name: '', endpoint_url: '', signing_secret: '', status: 'active', max_attempts: 10
  })
}

function selectTenant() {
  if (!compatibleIntegrations.value.some((integration) => integration.id === form.external_auth_integration_id)) {
    form.external_auth_integration_id = compatibleIntegrations.value[0]?.id || ''
  }
}

function openCreate() {
  editing.value = null
  resetForm()
  modalOpen.value = true
}

function openEdit(sink: PlatformUsageSink) {
  editing.value = sink
  Object.assign(form, {
    tenant_id: sink.tenant_id,
    external_auth_integration_id: sink.external_auth_integration_id,
    name: sink.name,
    endpoint_url: '',
    signing_secret: '',
    status: sink.status,
    max_attempts: sink.max_attempts
  })
  modalOpen.value = true
}

function openRotate(sink: PlatformUsageSink) {
  rotating.value = sink
  rotation.endpoint_url = ''
  rotation.signing_secret = ''
  rotateModalOpen.value = true
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [sinkResult, tenantResult, integrationResult] = await Promise.allSettled([getPlatformUsageSinks(), getPlatformTenants(), getExternalAuthIntegrations()])
    if (sinkResult.status === 'rejected') throw sinkResult.reason
    if (tenantResult.status === 'rejected') throw tenantResult.reason
    if (integrationResult.status === 'rejected') throw integrationResult.reason
    sinks.value = sinkResult.value
    tenants.value = tenantResult.value
    integrations.value = integrationResult.value
    if (selectedSink.value) {
      selectedSink.value = sinks.value.find((sink) => sink.id === selectedSink.value?.id) || null
      if (!selectedSink.value) deliveries.value = []
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  error.value = ''
  oneTimeSecret.value = ''
  try {
    const payload: PlatformUsageSinkRequest = { ...form }
    if (editing.value) {
      payload.endpoint_url = ''
      payload.signing_secret = ''
      await updatePlatformUsageSink(editing.value.id, payload)
      message.value = t('platform.usageSinkUpdated')
    } else {
      const created = await createPlatformUsageSink(payload)
      oneTimeSecret.value = created.signing_secret
      message.value = t('platform.usageSinkCreated')
    }
    modalOpen.value = false
    editing.value = null
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

async function rotateEndpoint() {
  if (!rotating.value) return
  saving.value = true
  error.value = ''
  oneTimeSecret.value = ''
  try {
    const rotated = await rotatePlatformUsageSinkEndpoint(rotating.value.id, rotation.endpoint_url, rotation.signing_secret)
    oneTimeSecret.value = rotated.signing_secret
    message.value = t('platform.usageSinkRotated')
    rotateModalOpen.value = false
    rotating.value = null
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

async function selectDeliveries(sink: PlatformUsageSink) {
  selectedSink.value = sink
  deliveriesLoading.value = true
  error.value = ''
  try {
    deliveries.value = await getPlatformUsageDeliveries(sink.id, deliveryStatus.value)
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    deliveriesLoading.value = false
  }
}

async function refreshDeliveries() {
  if (selectedSink.value) await selectDeliveries(selectedSink.value)
}

async function requeue(event: PlatformUsageDeliveryEvent) {
  if (!selectedSink.value) return
  error.value = ''
  try {
    await requeuePlatformUsageDelivery(selectedSink.value.id, event.id)
    message.value = t('platform.usageDeliveryRequeued')
    await refreshDeliveries()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  }
}

onMounted(load)
</script>

<template>
  <main class="content crud-page">
    <section class="page-header">
      <div>
        <h1>{{ t('platform.usageSinks') }}</h1>
        <p>{{ t('platform.usageSinksSubtitle') }}</p>
      </div>
      <div class="row-actions">
        <button class="button secondary" type="button" :disabled="loading" @click="load"><RefreshCw :size="17" />{{ t('common.refresh') }}</button>
        <button class="button" type="button" @click="openCreate"><Plus :size="17" />{{ t('platform.newUsageSink') }}</button>
      </div>
    </section>

    <div v-if="message" class="notice success">{{ message }}</div>
    <div v-if="error" class="notice">{{ error }}</div>
    <div v-if="oneTimeSecret" class="notice success"><strong>{{ t('platform.usageSinkSecretOnce') }}</strong><input :value="oneTimeSecret" readonly /></div>

    <section class="panel table-panel content-fit">
      <div class="panel-header"><Send :size="18" /><h2>{{ t('platform.usageSinks') }}</h2></div>
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead><tr><th>{{ t('platform.usageSinkName') }}</th><th>{{ t('platform.tenant') }}</th><th>{{ t('platform.integrations') }}</th><th>{{ t('platform.usageSinkTarget') }}</th><th>{{ t('providers.status') }}</th><th>{{ t('common.actions') }}</th></tr></thead>
          <tbody>
            <tr v-for="sink in sinks" :key="sink.id">
              <td><strong>{{ sink.name }}</strong><span>{{ t('platform.usageSinkAttempts', { count: sink.max_attempts }) }}</span></td>
              <td>{{ tenantNameByID.get(sink.tenant_id) || sink.tenant_id }}</td>
              <td>{{ integrationNameByID.get(sink.external_auth_integration_id) || sink.external_auth_integration_id }}</td>
              <td><span>{{ sink.endpoint_url_hint || '-' }}</span><span>{{ sink.signing_secret_hint || '-' }}</span></td>
              <td><span class="pill" :class="sink.status === 'active' ? 'status-success' : 'status-danger'">{{ sink.status }}</span></td>
              <td><div class="row-actions"><button class="button secondary" type="button" @click="openEdit(sink)"><Edit3 :size="15" />{{ t('common.edit') }}</button><button class="button secondary" type="button" @click="openRotate(sink)"><RotateCw :size="15" />{{ t('platform.rotateUsageSink') }}</button><button class="button secondary" type="button" @click="selectDeliveries(sink)"><History :size="15" />{{ t('platform.usageDeliveries') }}</button></div></td>
            </tr>
            <tr v-if="!sinks.length"><td colspan="6" class="empty-cell">{{ loading ? t('common.loading') : t('platform.noUsageSinks') }}</td></tr>
          </tbody>
        </table>
      </div>
    </section>

    <section v-if="selectedSink" class="panel table-panel content-fit">
      <div class="panel-header"><Link2 :size="18" /><h2>{{ t('platform.usageDeliveriesFor', { name: selectedSink.name }) }}</h2></div>
      <div class="panel-body table-scroll">
        <div class="row-actions delivery-filters">
          <select v-model="deliveryStatus" :aria-label="t('platform.usageDeliveryStatus')" @change="refreshDeliveries"><option value="">{{ t('platform.usageDeliveryAll') }}</option><option value="pending">pending</option><option value="delivering">delivering</option><option value="delivered">delivered</option><option value="dead_letter">dead_letter</option></select>
          <button class="button secondary" type="button" :disabled="deliveriesLoading" @click="refreshDeliveries"><RefreshCw :size="16" />{{ t('common.refresh') }}</button>
        </div>
        <table class="data-table crud-table">
          <thead><tr><th>{{ t('platform.usageDeliveryEvent') }}</th><th>{{ t('providers.status') }}</th><th>{{ t('platform.usageDeliveryAttempts') }}</th><th>{{ t('platform.usageDeliveryResult') }}</th><th>{{ t('common.actions') }}</th></tr></thead>
          <tbody>
            <tr v-for="event in deliveries" :key="event.id">
              <td><strong>{{ event.event_id }}</strong><span>{{ formatDate(event.created_at) }}</span></td>
              <td><span class="pill" :class="event.status === 'delivered' ? 'status-success' : event.status === 'dead_letter' ? 'status-danger' : ''">{{ event.status }}</span></td>
              <td>{{ event.attempt_count }} / {{ event.max_attempts }}</td>
              <td><span>{{ event.last_http_status || '-' }}</span><span>{{ event.last_error || formatDate(event.delivered_at || event.next_attempt_at) }}</span></td>
              <td><button v-if="event.status === 'dead_letter'" class="button secondary" type="button" @click="requeue(event)"><RefreshCw :size="15" />{{ t('platform.requeueUsageDelivery') }}</button></td>
            </tr>
            <tr v-if="!deliveries.length"><td colspan="5" class="empty-cell">{{ deliveriesLoading ? t('common.loading') : t('platform.noUsageDeliveries') }}</td></tr>
          </tbody>
        </table>
      </div>
    </section>

    <div v-if="modalOpen" class="modal-backdrop" @click.self="modalOpen = false">
      <section class="modal-card" role="dialog" aria-modal="true" :aria-label="editing ? t('platform.editUsageSink') : t('platform.newUsageSink')">
        <header class="modal-header"><div><h2>{{ editing ? t('platform.editUsageSink') : t('platform.newUsageSink') }}</h2><p>{{ t('platform.usageSinkModalSubtitle') }}</p></div><button class="icon-button" type="button" :title="t('common.close')" @click="modalOpen = false"><X :size="19" /></button></header>
        <div class="modal-body form-grid">
          <div class="field"><label for="usage-sink-tenant">{{ t('platform.tenant') }}</label><select id="usage-sink-tenant" v-model="form.tenant_id" :disabled="Boolean(editing)" @change="selectTenant"><option value="" disabled>{{ t('platform.tenant') }}</option><option v-for="tenant in activeTenants" :key="tenant.id" :value="tenant.id">{{ tenant.name }}</option></select></div>
          <div class="field"><label for="usage-sink-integration">{{ t('platform.integrations') }}</label><select id="usage-sink-integration" v-model="form.external_auth_integration_id" :disabled="Boolean(editing)"><option value="" disabled>{{ t('platform.integrations') }}</option><option v-for="integration in compatibleIntegrations" :key="integration.id" :value="integration.id">{{ integration.name }}</option></select></div>
          <div class="field form-span-2"><label for="usage-sink-name">{{ t('platform.usageSinkName') }}</label><input id="usage-sink-name" v-model="form.name" /></div>
          <template v-if="!editing">
            <div class="field form-span-2"><label for="usage-sink-endpoint">{{ t('platform.usageSinkEndpoint') }}</label><input id="usage-sink-endpoint" v-model="form.endpoint_url" type="url" placeholder="https://billing.example/events" /></div>
            <div class="field form-span-2"><label for="usage-sink-secret">{{ t('platform.usageSinkSecret') }}</label><input id="usage-sink-secret" v-model="form.signing_secret" type="password" autocomplete="new-password" /></div>
          </template>
          <div class="field"><label for="usage-sink-status">{{ t('providers.status') }}</label><select id="usage-sink-status" v-model="form.status"><option value="active">active</option><option value="disabled">disabled</option></select></div>
          <div class="field"><label for="usage-sink-attempts">{{ t('platform.usageSinkMaxAttempts') }}</label><input id="usage-sink-attempts" v-model.number="form.max_attempts" type="number" min="1" max="100" /></div>
        </div>
        <footer class="modal-footer"><button class="button secondary" type="button" @click="modalOpen = false">{{ t('common.cancel') }}</button><button class="button" type="button" :disabled="saving" @click="save"><Send :size="16" />{{ saving ? t('common.saving') : t('common.save') }}</button></footer>
      </section>
    </div>

    <div v-if="rotateModalOpen" class="modal-backdrop" @click.self="rotateModalOpen = false">
      <section class="modal-card" role="dialog" aria-modal="true" :aria-label="t('platform.rotateUsageSink')">
        <header class="modal-header"><div><h2>{{ t('platform.rotateUsageSink') }}</h2><p>{{ t('platform.rotateUsageSinkSubtitle') }}</p></div><button class="icon-button" type="button" :title="t('common.close')" @click="rotateModalOpen = false"><X :size="19" /></button></header>
        <div class="modal-body form-grid">
          <div class="field form-span-2"><label for="usage-sink-rotate-endpoint">{{ t('platform.usageSinkEndpoint') }}</label><input id="usage-sink-rotate-endpoint" v-model="rotation.endpoint_url" type="url" placeholder="https://billing.example/events" /></div>
          <div class="field form-span-2"><label for="usage-sink-rotate-secret">{{ t('platform.usageSinkSecret') }}</label><input id="usage-sink-rotate-secret" v-model="rotation.signing_secret" type="password" autocomplete="new-password" /></div>
        </div>
        <footer class="modal-footer"><button class="button secondary" type="button" @click="rotateModalOpen = false">{{ t('common.cancel') }}</button><button class="button" type="button" :disabled="saving" @click="rotateEndpoint"><RotateCw :size="16" />{{ saving ? t('common.saving') : t('platform.rotateUsageSink') }}</button></footer>
      </section>
    </div>
  </main>
</template>
