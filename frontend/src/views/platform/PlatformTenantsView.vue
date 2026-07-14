<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Edit3, Plus, RefreshCw, UsersRound, X } from '@lucide/vue'
import { createGatewayPrincipal, createPlatformTenant, getGatewayPrincipals, getPlatformTenants, updateGatewayPrincipal, updatePlatformTenant } from '@/api/platform'
import type { GatewayPrincipal, GatewayPrincipalRequest, PlatformTenant, PlatformTenantRequest } from '@/types'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const message = ref('')
const tenantModalOpen = ref(false)
const principalModalOpen = ref(false)
const editingTenant = ref<PlatformTenant | null>(null)
const editingPrincipal = ref<GatewayPrincipal | null>(null)
const tenants = ref<PlatformTenant[]>([])
const principals = ref<GatewayPrincipal[]>([])

const tenantForm = reactive<PlatformTenantRequest>({
  name: '', slug: '', entitlement_reference: '', status: 'active'
})
const principalForm = reactive<GatewayPrincipalRequest>({
  tenant_id: '', name: '', principal_type: 'service', external_subject_reference: '', status: 'active'
})

const tenantNameByID = computed(() => new Map(tenants.value.map((tenant) => [tenant.id, tenant.name])))
const activeTenants = computed(() => tenants.value.filter((tenant) => tenant.status === 'active'))

function resetTenantForm() {
  Object.assign(tenantForm, { name: '', slug: '', entitlement_reference: '', status: 'active' })
}

function resetPrincipalForm() {
  Object.assign(principalForm, { tenant_id: activeTenants.value[0]?.id || '', name: '', principal_type: 'service', external_subject_reference: '', status: 'active' })
}

function openTenantCreate() {
  editingTenant.value = null
  resetTenantForm()
  tenantModalOpen.value = true
}

function openTenantEdit(tenant: PlatformTenant) {
  editingTenant.value = tenant
  Object.assign(tenantForm, {
    name: tenant.name,
    slug: tenant.slug,
    entitlement_reference: tenant.entitlement_reference,
    status: tenant.status
  })
  tenantModalOpen.value = true
}

function openPrincipalCreate() {
  editingPrincipal.value = null
  resetPrincipalForm()
  principalModalOpen.value = true
}

function openPrincipalEdit(principal: GatewayPrincipal) {
  editingPrincipal.value = principal
  Object.assign(principalForm, {
    tenant_id: principal.tenant_id,
    name: principal.name,
    principal_type: principal.principal_type,
    external_subject_reference: principal.external_subject_reference,
    status: principal.status
  })
  principalModalOpen.value = true
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [nextTenants, nextPrincipals] = await Promise.all([getPlatformTenants(), getGatewayPrincipals()])
    tenants.value = nextTenants
    principals.value = nextPrincipals
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

async function saveTenant() {
  saving.value = true
  error.value = ''
  try {
    if (editingTenant.value) {
      await updatePlatformTenant(editingTenant.value.id, { ...tenantForm })
      message.value = t('apiKeys.updated')
    } else {
      await createPlatformTenant({ ...tenantForm })
      message.value = t('apiKeys.created')
    }
    tenantModalOpen.value = false
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

async function savePrincipal() {
  saving.value = true
  error.value = ''
  try {
    if (editingPrincipal.value) {
      await updateGatewayPrincipal(editingPrincipal.value.id, { ...principalForm })
      message.value = t('apiKeys.updated')
    } else {
      await createGatewayPrincipal({ ...principalForm })
      message.value = t('apiKeys.created')
    }
    principalModalOpen.value = false
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <main class="content crud-page">
    <section class="page-header">
      <div>
        <h1>{{ t('platform.tenants') }}</h1>
        <p>{{ t('platform.tenantsSubtitle') }}</p>
      </div>
      <div class="row-actions">
        <button class="button secondary" type="button" :disabled="loading" @click="load"><RefreshCw :size="17" />{{ t('common.refresh') }}</button>
        <button class="button secondary" type="button" @click="openTenantCreate"><Plus :size="17" />{{ t('platform.newTenant') }}</button>
        <button class="button" type="button" :disabled="!tenants.length" @click="openPrincipalCreate"><Plus :size="17" />{{ t('platform.newPrincipal') }}</button>
      </div>
    </section>

    <div v-if="message" class="notice success">{{ message }}</div>
    <div v-if="error" class="notice">{{ error }}</div>

    <section class="panel table-panel content-fit">
      <div class="panel-header"><UsersRound :size="18" /><h2>{{ t('platform.tenant') }}</h2></div>
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead><tr><th>{{ t('platform.tenantName') }}</th><th>{{ t('platform.tenantSlug') }}</th><th>{{ t('platform.entitlementReference') }}</th><th>{{ t('providers.status') }}</th><th>{{ t('common.actions') }}</th></tr></thead>
          <tbody>
            <tr v-for="tenant in tenants" :key="tenant.id">
              <td><strong>{{ tenant.name }}</strong><span>{{ tenant.id }}</span></td>
              <td>{{ tenant.slug }}</td>
              <td>{{ tenant.entitlement_reference || '-' }}</td>
              <td><span class="pill" :class="tenant.status === 'active' ? 'status-success' : 'status-danger'">{{ tenant.status }}</span></td>
              <td><button class="button secondary" type="button" @click="openTenantEdit(tenant)"><Edit3 :size="15" />{{ t('common.edit') }}</button></td>
            </tr>
            <tr v-if="!tenants.length"><td colspan="5" class="empty-cell">{{ loading ? t('common.loading') : t('platform.noTenants') }}</td></tr>
          </tbody>
        </table>
      </div>
    </section>

    <section class="panel table-panel content-fit section-gap">
      <div class="panel-header"><UsersRound :size="18" /><h2>{{ t('platform.principals') }}</h2></div>
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead><tr><th>{{ t('platform.principalName') }}</th><th>{{ t('platform.tenant') }}</th><th>{{ t('platform.principalType') }}</th><th>{{ t('platform.externalSubjectReference') }}</th><th>{{ t('providers.status') }}</th><th>{{ t('common.actions') }}</th></tr></thead>
          <tbody>
            <tr v-for="principal in principals" :key="principal.id">
              <td><strong>{{ principal.name }}</strong><span>{{ principal.id }}</span></td>
              <td>{{ tenantNameByID.get(principal.tenant_id) || principal.tenant_id }}</td>
              <td>{{ principal.principal_type }}</td>
              <td>{{ principal.external_subject_reference || '-' }}</td>
              <td><span class="pill" :class="principal.status === 'active' ? 'status-success' : 'status-danger'">{{ principal.status }}</span></td>
              <td><button class="button secondary" type="button" @click="openPrincipalEdit(principal)"><Edit3 :size="15" />{{ t('common.edit') }}</button></td>
            </tr>
            <tr v-if="!principals.length"><td colspan="6" class="empty-cell">{{ loading ? t('common.loading') : t('platform.noPrincipals') }}</td></tr>
          </tbody>
        </table>
      </div>
    </section>

    <div v-if="tenantModalOpen" class="modal-backdrop" @click.self="tenantModalOpen = false">
      <section class="modal-card" role="dialog" aria-modal="true" :aria-label="editingTenant ? t('platform.editTenant') : t('platform.newTenant')">
        <header class="modal-header"><div><h2>{{ editingTenant ? t('platform.editTenant') : t('platform.newTenant') }}</h2></div><button class="icon-button" type="button" :title="t('common.close')" @click="tenantModalOpen = false"><X :size="19" /></button></header>
        <div class="modal-body form-grid">
          <div class="field form-span-2"><label for="platform-tenant-name">{{ t('platform.tenantName') }}</label><input id="platform-tenant-name" v-model="tenantForm.name" /></div>
          <div class="field"><label for="platform-tenant-slug">{{ t('platform.tenantSlug') }}</label><input id="platform-tenant-slug" v-model="tenantForm.slug" :disabled="editingTenant?.id === 'ptn_default'" /></div>
          <div class="field"><label for="platform-tenant-status">{{ t('providers.status') }}</label><select id="platform-tenant-status" v-model="tenantForm.status"><option value="active">active</option><option value="disabled">disabled</option></select></div>
          <div class="field form-span-2"><label for="platform-tenant-entitlement">{{ t('platform.entitlementReference') }}</label><input id="platform-tenant-entitlement" v-model="tenantForm.entitlement_reference" /></div>
        </div>
        <footer class="modal-footer"><button class="button secondary" type="button" @click="tenantModalOpen = false">{{ t('common.cancel') }}</button><button class="button" type="button" :disabled="saving" @click="saveTenant">{{ saving ? t('common.saving') : t('common.save') }}</button></footer>
      </section>
    </div>

    <div v-if="principalModalOpen" class="modal-backdrop" @click.self="principalModalOpen = false">
      <section class="modal-card" role="dialog" aria-modal="true" :aria-label="editingPrincipal ? t('platform.editPrincipal') : t('platform.newPrincipal')">
        <header class="modal-header"><div><h2>{{ editingPrincipal ? t('platform.editPrincipal') : t('platform.newPrincipal') }}</h2></div><button class="icon-button" type="button" :title="t('common.close')" @click="principalModalOpen = false"><X :size="19" /></button></header>
        <div class="modal-body form-grid">
          <div class="field"><label for="platform-principal-tenant">{{ t('platform.tenant') }}</label><select id="platform-principal-tenant" v-model="principalForm.tenant_id" :disabled="Boolean(editingPrincipal)"><option v-for="tenant in activeTenants" :key="tenant.id" :value="tenant.id">{{ tenant.name }}</option></select></div>
          <div class="field"><label for="platform-principal-type">{{ t('platform.principalType') }}</label><select id="platform-principal-type" v-model="principalForm.principal_type"><option value="service">service</option><option value="developer">developer</option><option value="integration">integration</option></select></div>
          <div class="field form-span-2"><label for="platform-principal-name">{{ t('platform.principalName') }}</label><input id="platform-principal-name" v-model="principalForm.name" /></div>
          <div class="field"><label for="platform-principal-status">{{ t('providers.status') }}</label><select id="platform-principal-status" v-model="principalForm.status"><option value="active">active</option><option value="disabled">disabled</option></select></div>
          <div class="field"><label for="platform-principal-external-ref">{{ t('platform.externalSubjectReference') }}</label><input id="platform-principal-external-ref" v-model="principalForm.external_subject_reference" /></div>
        </div>
        <footer class="modal-footer"><button class="button secondary" type="button" @click="principalModalOpen = false">{{ t('common.cancel') }}</button><button class="button" type="button" :disabled="saving" @click="savePrincipal">{{ saving ? t('common.saving') : t('common.save') }}</button></footer>
      </section>
    </div>
  </main>
</template>
