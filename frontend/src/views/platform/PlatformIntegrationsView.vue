<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Edit3, KeyRound, Link2, Plus, RefreshCw, RotateCw, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import GatewayModelPicker from '@/components/model/GatewayModelPicker.vue'
import { getGatewayModels, getGovernancePolicies } from '@/api/control'
import { createExternalAuthIntegration, getExternalAuthIntegrations, getGatewayPrincipals, getPlatformTenants, rotateExternalAuthIntegrationSecret, updateExternalAuthIntegration } from '@/api/platform'
import type { ExternalAuthIntegration, ExternalAuthIntegrationRequest, GatewayModel, GatewayPrincipal, GovernancePolicy, PlatformTenant } from '@/types'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const message = ref('')
const modalOpen = ref(false)
const editing = ref<ExternalAuthIntegration | null>(null)
const integrations = ref<ExternalAuthIntegration[]>([])
const tenants = ref<PlatformTenant[]>([])
const principals = ref<GatewayPrincipal[]>([])
const policies = ref<GovernancePolicy[]>([])
const gatewayModels = ref<GatewayModel[]>([])
const oneTimeSecret = ref('')
const form = reactive<ExternalAuthIntegrationRequest>({
  tenant_id: '', gateway_principal_id: '', name: '', protocol: 'hmac_signed_context', key_id: '', audience: '', policy_id: '',
  issuer: '', jwks_url: '', subject_claim: '', models_claim: '', qps_limit_claim: '', monthly_token_limit_claim: '',
  model_allowlist: [], qps_limit: 10, monthly_token_limit: 1_000_000, max_ttl_seconds: 300, status: 'active'
})

const isJWTIntegration = computed(() => form.protocol === 'jwt_jwks')

const activeTenants = computed(() => tenants.value.filter((tenant) => tenant.status === 'active'))
const compatiblePrincipals = computed(() => principals.value.filter((principal) => principal.status === 'active' && principal.tenant_id === form.tenant_id && (principal.principal_type === 'service' || principal.principal_type === 'integration')))
const activePolicies = computed(() => policies.value.filter((policy) => policy.status === 'active'))
const defaultGatewayModel = computed(() => gatewayModels.value.find((item) => item.status === 'active')?.model_id || '')
const tenantNameByID = computed(() => new Map(tenants.value.map((tenant) => [tenant.id, tenant.name])))
const principalNameByID = computed(() => new Map(principals.value.map((principal) => [principal.id, principal.name])))

function resetForm() {
  const tenantID = activeTenants.value[0]?.id || ''
  Object.assign(form, {
    tenant_id: tenantID,
    gateway_principal_id: principals.value.find((principal) => principal.status === 'active' && principal.tenant_id === tenantID && (principal.principal_type === 'service' || principal.principal_type === 'integration'))?.id || '',
    name: '', protocol: 'hmac_signed_context', key_id: '', audience: '', policy_id: '', issuer: '', jwks_url: '', subject_claim: '', models_claim: '', qps_limit_claim: '', monthly_token_limit_claim: '', model_allowlist: defaultGatewayModel.value ? [defaultGatewayModel.value] : [], qps_limit: 10,
    monthly_token_limit: 1_000_000, max_ttl_seconds: 300, status: 'active'
  })
}

function openCreate() {
  editing.value = null
  resetForm()
  modalOpen.value = true
}

function openEdit(integration: ExternalAuthIntegration) {
  editing.value = integration
  Object.assign(form, {
    tenant_id: integration.tenant_id, gateway_principal_id: integration.gateway_principal_id, name: integration.name,
    protocol: integration.protocol, key_id: integration.key_id, audience: integration.audience, policy_id: integration.policy_id,
    issuer: integration.protocol === 'jwt_jwks' ? integration.issuer : '', jwks_url: integration.protocol === 'jwt_jwks' ? integration.jwks_url : '', subject_claim: integration.protocol === 'jwt_jwks' ? integration.subject_claim || 'sub' : '', models_claim: integration.protocol === 'jwt_jwks' ? integration.models_claim : '', qps_limit_claim: integration.protocol === 'jwt_jwks' ? integration.qps_limit_claim : '', monthly_token_limit_claim: integration.protocol === 'jwt_jwks' ? integration.monthly_token_limit_claim : '',
    model_allowlist: [...integration.model_allowlist], qps_limit: integration.qps_limit, monthly_token_limit: integration.monthly_token_limit,
    max_ttl_seconds: integration.max_ttl_seconds, status: integration.status
  })
  modalOpen.value = true
}

function selectTenant() {
  if (!compatiblePrincipals.value.some((principal) => principal.id === form.gateway_principal_id)) {
    form.gateway_principal_id = compatiblePrincipals.value[0]?.id || ''
  }
}

function selectProtocol() {
  if (form.protocol === 'jwt_jwks') {
    if (!form.subject_claim) form.subject_claim = 'sub'
    return
  }
  Object.assign(form, {
    issuer: '', jwks_url: '', subject_claim: '', models_claim: '', qps_limit_claim: '', monthly_token_limit_claim: ''
  })
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [integrationResult, tenantResult, principalResult, policyResult, modelResult] = await Promise.allSettled([getExternalAuthIntegrations(), getPlatformTenants(), getGatewayPrincipals(), getGovernancePolicies(), getGatewayModels()])
    if (integrationResult.status === 'rejected') throw integrationResult.reason
    if (tenantResult.status === 'rejected') throw tenantResult.reason
    if (principalResult.status === 'rejected') throw principalResult.reason
    if (modelResult.status === 'rejected') throw modelResult.reason
    integrations.value = integrationResult.value
    tenants.value = tenantResult.value
    principals.value = principalResult.value
    policies.value = policyResult.status === 'fulfilled' ? policyResult.value : []
    gatewayModels.value = modelResult.value
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
    const payload: ExternalAuthIntegrationRequest = { ...form, model_allowlist: [...form.model_allowlist] }
    if (payload.protocol === 'hmac_signed_context') {
      Object.assign(payload, {
        issuer: '', jwks_url: '', subject_claim: '', models_claim: '', qps_limit_claim: '', monthly_token_limit_claim: ''
      })
    }
    if (editing.value) {
      await updateExternalAuthIntegration(editing.value.id, payload)
      message.value = t('platform.integrationUpdated')
    } else {
      const created = await createExternalAuthIntegration(payload)
      oneTimeSecret.value = created.secret
      message.value = t('platform.integrationCreated')
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

async function rotate(integration: ExternalAuthIntegration) {
  error.value = ''
  oneTimeSecret.value = ''
  try {
    if (integration.protocol !== 'hmac_signed_context') return
    oneTimeSecret.value = (await rotateExternalAuthIntegrationSecret(integration.id)).secret
    message.value = t('platform.integrationSecretRotated')
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
      <div>
        <h1>{{ t('platform.integrations') }}</h1>
        <p>{{ t('platform.integrationsSubtitle') }}</p>
      </div>
      <div class="row-actions">
        <button class="button secondary" type="button" :disabled="loading" @click="load"><RefreshCw :size="17" />{{ t('common.refresh') }}</button>
        <button class="button" type="button" @click="openCreate"><Plus :size="17" />{{ t('platform.newIntegration') }}</button>
      </div>
    </section>

    <div v-if="message" class="notice success">{{ message }}</div>
    <div v-if="error" class="notice">{{ error }}</div>
    <div v-if="oneTimeSecret" class="notice success"><strong>{{ t('platform.integrationSecretOnce') }}</strong><input :value="oneTimeSecret" readonly /></div>

    <section class="panel table-panel content-fit">
      <div class="panel-header"><Link2 :size="18" /><h2>{{ t('platform.integrations') }}</h2></div>
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead><tr><th>{{ t('platform.integrationName') }}</th><th>{{ t('platform.tenant') }}</th><th>{{ t('platform.principal') }}</th><th>{{ t('platform.integrationAudience') }}</th><th>{{ t('providers.status') }}</th><th>{{ t('common.actions') }}</th></tr></thead>
          <tbody>
            <tr v-for="integration in integrations" :key="integration.id">
              <td><strong>{{ integration.name }}</strong><span>{{ integration.protocol }} · {{ integration.key_id }}</span></td>
              <td>{{ tenantNameByID.get(integration.tenant_id) || integration.tenant_id }}</td>
              <td>{{ principalNameByID.get(integration.gateway_principal_id) || integration.gateway_principal_id }}</td>
              <td><span>{{ integration.audience }}</span><span>{{ integration.protocol === 'jwt_jwks' ? integration.issuer : integration.secret_hint || '-' }}</span></td>
              <td><span class="pill" :class="integration.status === 'active' ? 'status-success' : 'status-danger'">{{ integration.status }}</span></td>
              <td><div class="row-actions"><button class="button secondary" type="button" @click="openEdit(integration)"><Edit3 :size="15" />{{ t('common.edit') }}</button><button v-if="integration.protocol === 'hmac_signed_context'" class="button secondary" type="button" @click="rotate(integration)"><RotateCw :size="15" />{{ t('platform.rotateIntegrationSecret') }}</button></div></td>
            </tr>
            <tr v-if="!integrations.length"><td colspan="6" class="empty-cell">{{ loading ? t('common.loading') : t('platform.noIntegrations') }}</td></tr>
          </tbody>
        </table>
      </div>
    </section>

    <div v-if="modalOpen" class="modal-backdrop" @click.self="modalOpen = false">
      <section class="modal-card" role="dialog" aria-modal="true" :aria-label="editing ? t('platform.editIntegration') : t('platform.newIntegration')">
        <header class="modal-header"><div><h2>{{ editing ? t('platform.editIntegration') : t('platform.newIntegration') }}</h2><p>{{ t('platform.integrationModalSubtitle') }}</p></div><button class="icon-button" type="button" :title="t('common.close')" @click="modalOpen = false"><X :size="19" /></button></header>
        <div class="modal-body form-grid">
          <div class="field"><label for="integration-tenant">{{ t('platform.tenant') }}</label><select id="integration-tenant" v-model="form.tenant_id" :disabled="Boolean(editing)" @change="selectTenant"><option value="" disabled>{{ t('platform.tenant') }}</option><option v-for="tenant in activeTenants" :key="tenant.id" :value="tenant.id">{{ tenant.name }}</option></select></div>
          <div class="field"><label for="integration-principal">{{ t('platform.principal') }}</label><select id="integration-principal" v-model="form.gateway_principal_id" :disabled="Boolean(editing)"><option value="" disabled>{{ t('platform.principal') }}</option><option v-for="principal in compatiblePrincipals" :key="principal.id" :value="principal.id">{{ principal.name }} ({{ principal.principal_type }})</option></select></div>
          <div class="field form-span-2"><label for="integration-name">{{ t('platform.integrationName') }}</label><input id="integration-name" v-model="form.name" /></div>
          <div class="field"><label for="integration-protocol">{{ t('platform.integrationProtocol') }}</label><select id="integration-protocol" v-model="form.protocol" :disabled="Boolean(editing)" @change="selectProtocol"><option value="hmac_signed_context">{{ t('platform.integrationProtocolHMAC') }}</option><option value="jwt_jwks">{{ t('platform.integrationProtocolJWT') }}</option></select></div>
          <div class="field"><label for="integration-key-id">{{ t('platform.integrationKeyID') }}</label><input id="integration-key-id" v-model="form.key_id" :disabled="Boolean(editing)" /></div>
          <div class="field"><label for="integration-status">{{ t('providers.status') }}</label><select id="integration-status" v-model="form.status"><option value="active">active</option><option value="disabled">disabled</option></select></div>
          <div class="field form-span-2"><label for="integration-audience">{{ t('platform.integrationAudience') }}</label><input id="integration-audience" v-model="form.audience" /></div>
          <template v-if="isJWTIntegration">
            <div class="field form-span-2"><label for="integration-issuer">{{ t('platform.integrationIssuer') }}</label><input id="integration-issuer" v-model="form.issuer" type="url" :disabled="Boolean(editing)" /></div>
            <div class="field form-span-2"><label for="integration-jwks">{{ t('platform.integrationJWKSURL') }}</label><input id="integration-jwks" v-model="form.jwks_url" type="url" :disabled="Boolean(editing)" /></div>
            <div class="field"><label for="integration-subject-claim">{{ t('platform.integrationSubjectClaim') }}</label><input id="integration-subject-claim" v-model="form.subject_claim" :disabled="Boolean(editing)" /></div>
            <div class="field"><label for="integration-models-claim">{{ t('platform.integrationModelsClaim') }}</label><input id="integration-models-claim" v-model="form.models_claim" :disabled="Boolean(editing)" /></div>
            <div class="field"><label for="integration-qps-claim">{{ t('platform.integrationQPSClaim') }}</label><input id="integration-qps-claim" v-model="form.qps_limit_claim" :disabled="Boolean(editing)" /></div>
            <div class="field"><label for="integration-monthly-claim">{{ t('platform.integrationMonthlyClaim') }}</label><input id="integration-monthly-claim" v-model="form.monthly_token_limit_claim" :disabled="Boolean(editing)" /></div>
          </template>
          <div class="field form-span-2"><label for="integration-policy">{{ t('policies.policy') }}</label><select id="integration-policy" v-model="form.policy_id"><option value="">{{ t('policies.inherit') }}</option><option v-for="policy in activePolicies" :key="policy.id" :value="policy.id">{{ policy.name }}</option></select></div>
          <div class="field form-span-2"><label id="integration-models-label">{{ t('apiKeys.models') }}</label><GatewayModelPicker v-model="form.model_allowlist" :models="gatewayModels" :disabled="saving" aria-labelledby="integration-models-label" /></div>
          <div class="field"><label for="integration-qps">{{ t('apiKeys.qps') }}</label><input id="integration-qps" v-model.number="form.qps_limit" type="number" min="1" /></div>
          <div class="field"><label for="integration-tokens">{{ t('apiKeys.monthlyTokens') }}</label><input id="integration-tokens" v-model.number="form.monthly_token_limit" type="number" min="1" /></div>
          <div class="field"><label for="integration-ttl">{{ t('platform.integrationMaxTTL') }}</label><input id="integration-ttl" v-model.number="form.max_ttl_seconds" type="number" min="30" max="3600" /></div>
        </div>
        <footer class="modal-footer"><button class="button secondary" type="button" @click="modalOpen = false">{{ t('common.cancel') }}</button><button class="button" type="button" :disabled="saving || !form.model_allowlist.length" @click="save"><KeyRound :size="16" />{{ saving ? t('common.saving') : t('common.save') }}</button></footer>
      </section>
    </div>
  </main>
</template>
