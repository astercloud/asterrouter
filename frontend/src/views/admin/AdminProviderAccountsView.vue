<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import { Activity, ChevronLeft, ChevronRight, CircleCheck, Edit3, Plus, RefreshCw, Save, Search, ShieldCheck, ShieldOff, Trash2, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import ProviderAccountModelEditor from '@/components/provider/ProviderAccountModelEditor.vue'
import { checkProviderAccount, clearProviderAccountCooldown, createProviderAccount, getProviderAccountHealthChecks, getProviderAccounts, getProviders, getRoutingGroups, updateProviderAccount } from '@/api/control'
import type { ProviderAccount, ProviderAccountHealthCheck, ProviderAccountRequest, ProviderAccountTempUnschedulableRule, ProviderConnection, RoutingGroup } from '@/types'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const actionID = ref('')
const error = ref('')
const message = ref('')
const accounts = ref<ProviderAccount[]>([])
const groups = ref<RoutingGroup[]>([])
const providers = ref<ProviderConnection[]>([])
const healthChecks = ref<Record<string, ProviderAccountHealthCheck>>({})
const query = ref('')
const statusFilter = ref('')
const platformFilter = ref('')
const modalOpen = ref(false)
const editing = ref<ProviderAccount | null>(null)
const wizardStep = ref(1)
const modelEditor = ref<{ discover: () => Promise<void> } | null>(null)

const authByProvider: Record<string, string[]> = {
  openai_compatible: ['api_key', 'bearer'], anthropic_compatible: ['api_key'], gemini_compatible: ['api_key'],
  aws_bedrock: ['aws_default_chain', 'aws_access_key'], gcp_vertex: ['gcp_adc', 'gcp_service_account'],
  azure_openai: ['api_key', 'azure_managed_identity']
}
const configFields: Record<string, Array<{ key: string; required?: boolean; placeholder?: string }>> = {
  anthropic_compatible: [{ key: 'anthropic_version', placeholder: '2023-06-01' }],
  aws_bedrock: [{ key: 'region', required: true, placeholder: 'us-east-1' }, { key: 'endpoint' }],
  gcp_vertex: [{ key: 'project', required: true }, { key: 'location', required: true, placeholder: 'us-central1' }, { key: 'endpoint' }],
  azure_openai: [{ key: 'api_version', required: true, placeholder: '2025-04-01-preview' }, { key: 'audience', placeholder: 'https://cognitiveservices.azure.com/.default' }, { key: 'managed_identity_client_id' }]
}
const wizardSteps = computed(() => [
  { id: 1, label: t('providerAccounts.provider') }, { id: 2, label: t('providerAccounts.authType') },
  { id: 3, label: t('providerAccounts.capacity') }, { id: 4, label: t('providerAccounts.upstreamModels') }
])

const form = reactive<ProviderAccountRequest>({
  provider_id: '', name: '', platform: 'openai_compatible', auth_type: 'api_key', adapter_config: {}, status: 'active', schedulable: true,
  priority: 50, weight: 100, concurrency: 3, rpm_limit: 0, tpm_limit: 0, load_factor: null, rate_multiplier: 1,
  models: [], auto_enable_new_models: false, group_ids: [], secret: '', expires_at: '', circuit_failure_threshold: 5,
  circuit_open_seconds: 60, temp_unschedulable_rules: []
})

const providerByID = computed(() => new Map(providers.value.map((item) => [item.id, item])))
const groupByID = computed(() => new Map(groups.value.map((item) => [item.id, item])))
const selectedProvider = computed(() => providerByID.value.get(form.provider_id))
const modelDiscoveryEnabled = computed(() => ['openai_compatible', 'anthropic_compatible', 'gemini_compatible'].includes(selectedProvider.value?.type || ''))
const authOptions = computed(() => authByProvider[selectedProvider.value?.type || 'openai_compatible'] || ['api_key'])
const adapterConfigFields = computed(() => configFields[selectedProvider.value?.type || ''] || [])
const platforms = computed(() => Array.from(new Set(accounts.value.map((item) => item.platform))).filter(Boolean).sort())
const authRequiresSecret = computed(() => ['api_key', 'bearer', 'aws_access_key', 'gcp_service_account'].includes(form.auth_type))
const secretPlaceholder = computed(() => {
  if (editing.value) return t('providers.keepSecret')
  if (form.auth_type === 'aws_access_key') return '{ "access_key_id": "...", "secret_access_key": "..." }'
  if (form.auth_type === 'gcp_service_account') return '{ "type": "service_account", ... }'
  return '••••••••'
})

const filteredAccounts = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return accounts.value.filter((account) => {
    if (statusFilter.value && account.status !== statusFilter.value) return false
    if (platformFilter.value && account.platform !== platformFilter.value) return false
    const source = providerByID.value.get(account.provider_id)?.name || ''
    return !keyword || [account.name, source, account.platform, account.auth_type, ...account.models].some((value) => value.toLowerCase().includes(keyword))
  })
})
const summary = computed(() => ({
  total: accounts.value.length,
  schedulable: accounts.value.filter((item) => item.status === 'active' && item.schedulable).length,
  healthy: Object.values(healthChecks.value).filter((item) => item.status === 'ok').length,
  attention: accounts.value.filter((item) => item.status === 'error' || !item.secret_configured && ['api_key', 'bearer', 'aws_access_key', 'gcp_service_account'].includes(item.auth_type)).length
}))

function cloneRules(rules: ProviderAccountTempUnschedulableRule[]) { return rules.map((rule) => ({ ...rule, keywords: [...rule.keywords] })) }
function dateInputValue(value?: string) { return value ? value.slice(0, 10) : '' }
function splitValues(value: string) { return value.split(/\n|,/).map((item) => item.trim()).filter(Boolean) }

function accountToRequest(account: ProviderAccount, status = account.status): ProviderAccountRequest {
  return {
    provider_id: account.provider_id, name: account.name, platform: account.platform, auth_type: account.auth_type,
    adapter_config: { ...account.adapter_config }, status, schedulable: account.schedulable, priority: account.priority,
    weight: account.weight, concurrency: account.concurrency, rpm_limit: account.rpm_limit, tpm_limit: account.tpm_limit,
    load_factor: account.load_factor ?? null, rate_multiplier: account.rate_multiplier, models: [...account.models],
    auto_enable_new_models: account.auto_enable_new_models, group_ids: [...account.group_ids], secret: '',
    expires_at: dateInputValue(account.expires_at), circuit_failure_threshold: account.circuit_failure_threshold,
    circuit_open_seconds: account.circuit_open_seconds, temp_unschedulable_rules: cloneRules(account.temp_unschedulable_rules)
  }
}

function resetForm() {
  const provider = providers.value[0]
  Object.assign(form, {
    provider_id: provider?.id || '', name: '', platform: provider?.type || 'openai_compatible',
    auth_type: authByProvider[provider?.type || 'openai_compatible']?.[0] || 'api_key', adapter_config: {}, status: 'active',
    schedulable: true, priority: 50, weight: 100, concurrency: 3, rpm_limit: 0, tpm_limit: 0, load_factor: null,
    rate_multiplier: 1, models: [], auto_enable_new_models: false, group_ids: groups.value[0] ? [groups.value[0].id] : [],
    secret: '', expires_at: '', circuit_failure_threshold: 5, circuit_open_seconds: 60, temp_unschedulable_rules: []
  })
}

function syncProvider() {
  const provider = selectedProvider.value
  if (!provider) return
  form.platform = provider.type
  form.auth_type = authByProvider[provider.type]?.[0] || 'api_key'
  form.adapter_config = {}
}

function openCreate() { editing.value = null; resetForm(); wizardStep.value = 1; modalOpen.value = true }
function openEdit(account: ProviderAccount) { editing.value = account; Object.assign(form, accountToRequest(account)); wizardStep.value = 1; modalOpen.value = true }
function closeModal() { modalOpen.value = false; editing.value = null }
function toggleGroup(id: string) { form.group_ids = form.group_ids.includes(id) ? form.group_ids.filter((item) => item !== id) : [...form.group_ids, id] }
function addRule() { form.temp_unschedulable_rules = [...form.temp_unschedulable_rules, { status_code: 429, keywords: [], duration_minutes: 30 }] }
function removeRule(index: number) { form.temp_unschedulable_rules = form.temp_unschedulable_rules.filter((_, item) => item !== index) }
function setRuleKeywords(rule: ProviderAccountTempUnschedulableRule, value: string) { rule.keywords = splitValues(value) }

function canContinue() {
  if (wizardStep.value === 1) return Boolean(form.provider_id && form.name.trim())
  if (wizardStep.value === 2) {
    if (!form.auth_type) return false
    if (authRequiresSecret.value && !editing.value && !form.secret.trim()) return false
    return adapterConfigFields.value.every((field) => !field.required || Boolean(form.adapter_config[field.key]?.trim()))
  }
  return true
}
function nextStep() { if (canContinue()) wizardStep.value = Math.min(4, wizardStep.value + 1) }
function previousStep() { wizardStep.value = Math.max(1, wizardStep.value - 1) }

async function load() {
  loading.value = true; error.value = ''
  try {
    const [groupData, providerData, accountData, healthData] = await Promise.all([getRoutingGroups(), getProviders(), getProviderAccounts(), getProviderAccountHealthChecks()])
    groups.value = groupData; providers.value = providerData; accounts.value = accountData
    healthChecks.value = Object.fromEntries(healthData.map((item) => [item.account_id, item]))
  } catch (err) { error.value = err instanceof Error ? err.message : t('common.failed') }
  finally { loading.value = false }
}

async function save() {
  saving.value = true; error.value = ''; message.value = ''
  try {
    const payload = { ...form, adapter_config: { ...form.adapter_config }, load_factor: form.load_factor ? Number(form.load_factor) : null }
    if (editing.value) {
      await updateProviderAccount(editing.value.id, payload); message.value = t('providerAccounts.updated'); closeModal(); await load()
    } else {
      const created = await createProviderAccount(payload); editing.value = created; Object.assign(form, accountToRequest(created)); accounts.value = [...accounts.value, created]
      message.value = t('providerAccounts.created'); await nextTick()
      if (modelDiscoveryEnabled.value) await modelEditor.value?.discover()
      else { closeModal(); await load() }
    }
  } catch (err) { error.value = err instanceof Error ? err.message : t('common.failed') }
  finally { saving.value = false }
}

async function runAccountAction(account: ProviderAccount, action: 'check' | 'toggle' | 'cooldown') {
  actionID.value = `${action}:${account.id}`; error.value = ''; message.value = ''
  try {
    if (action === 'check') { const result = await checkProviderAccount(account.id); healthChecks.value = { ...healthChecks.value, [account.id]: result }; message.value = result.message }
    if (action === 'toggle') { const status = account.status === 'disabled' ? 'active' : 'disabled'; await updateProviderAccount(account.id, accountToRequest(account, status)); message.value = status }
    if (action === 'cooldown') { await clearProviderAccountCooldown(account.id); message.value = t('providerAccounts.cooldownCleared') }
    await load()
  } catch (err) { error.value = err instanceof Error ? err.message : t('common.failed') }
  finally { actionID.value = '' }
}

function handleModelsSynced(account: ProviderAccount) { editing.value = account; Object.assign(form, accountToRequest(account)); accounts.value = accounts.value.map((item) => item.id === account.id ? account : item) }
function statusClass(status: string) { return status === 'active' || status === 'ok' ? 'status-success' : status === 'error' ? 'status-warning' : 'status-danger' }
function activeCooldownUntil(account: ProviderAccount) { if (!account.cooldown_until) return ''; const until = new Date(account.cooldown_until); return until.getTime() > Date.now() ? until.toLocaleTimeString() : '' }
function accountReady(account: ProviderAccount) { return account.status === 'active' && account.schedulable && (!['api_key', 'bearer', 'aws_access_key', 'gcp_service_account'].includes(account.auth_type) || account.secret_configured) }

onMounted(load)
</script>

<template>
  <main class="content crud-page account-workbench">
    <section class="page-header"><div><h1>{{ t('admin.providerAccounts') }}</h1><p>{{ t('providerAccounts.subtitle') }}</p></div><button class="button" type="button" @click="openCreate"><Plus :size="17" />{{ t('providerAccounts.newAccount') }}</button></section>

    <div class="crud-summary">
      <span><strong>{{ summary.total }}</strong>{{ t('providerAccounts.accounts') }}</span>
      <span><strong>{{ summary.schedulable }}</strong>{{ t('providerAccounts.schedulable') }}</span>
      <span><strong>{{ summary.healthy }}</strong>{{ t('providerAccounts.health') }}</span>
      <span><strong>{{ summary.attention }}</strong>{{ t('providerAccounts.error') }}</span>
    </div>

    <section class="table-toolbar">
      <label class="search-box"><Search :size="17" /><input v-model="query" :placeholder="t('providerAccounts.searchPlaceholder')" /></label>
      <select v-model="platformFilter"><option value="">{{ t('routingGroups.allPlatforms') }}</option><option v-for="platform in platforms" :key="platform" :value="platform">{{ platform }}</option></select>
      <select v-model="statusFilter"><option value="">{{ t('providers.allStatuses') }}</option><option value="active">active</option><option value="error">error</option><option value="disabled">disabled</option></select>
      <button class="icon-button" type="button" :disabled="loading" :aria-label="t('common.refresh')" :title="t('common.refresh')" @click="load"><RefreshCw :size="17" /></button>
    </section>
    <div v-if="message" class="notice success">{{ message }}</div><div v-if="error" class="notice">{{ error }}</div>

    <section class="panel table-panel"><div class="panel-body table-scroll"><table class="data-table crud-table">
      <thead><tr><th>{{ t('providerAccounts.name') }}</th><th>{{ t('providerAccounts.provider') }}</th><th>{{ t('providers.status') }}</th><th>{{ t('providers.models') }}</th><th>{{ t('providerAccounts.capacity') }}</th><th>{{ t('providerAccounts.health') }}</th><th>{{ t('common.actions') }}</th></tr></thead>
      <tbody>
        <tr v-for="account in filteredAccounts" :key="account.id">
          <td><strong>{{ account.name }}</strong><span>{{ account.auth_type }}</span></td>
          <td><strong>{{ providerByID.get(account.provider_id)?.name || '-' }}</strong><span>{{ account.platform }}</span></td>
          <td><span class="pill" :class="accountReady(account) ? 'status-success' : statusClass(account.status)">{{ accountReady(account) ? 'ready' : account.status }}</span><span v-if="activeCooldownUntil(account)" class="pill status-warning">cooldown · {{ activeCooldownUntil(account) }}</span></td>
          <td><strong>{{ account.models.length }}</strong><span>{{ account.models.slice(0, 2).join(' · ') || '-' }}</span></td>
          <td><strong>{{ account.concurrency }} concurrent</strong><span>RPM {{ account.rpm_limit || '∞' }} · TPM {{ account.tpm_limit || '∞' }}</span><span>P{{ account.priority }} · W{{ account.weight }}</span></td>
          <td><template v-if="healthChecks[account.id]"><span class="pill" :class="statusClass(healthChecks[account.id].status)">{{ healthChecks[account.id].status }} · {{ healthChecks[account.id].latency_ms }}ms</span><span>{{ healthChecks[account.id].message }}</span></template><span v-else class="hint">{{ t('providers.notChecked') }}</span></td>
          <td><div class="row-actions">
            <button class="icon-button" type="button" :disabled="actionID === `check:${account.id}`" :aria-label="t('providers.check')" :title="t('providers.check')" @click="runAccountAction(account, 'check')"><Activity :size="16" /></button>
            <button class="icon-button" type="button" :aria-label="t('common.edit')" :title="t('common.edit')" @click="openEdit(account)"><Edit3 :size="16" /></button>
            <button class="icon-button" type="button" :disabled="actionID === `toggle:${account.id}`" :aria-label="account.status === 'disabled' ? t('providerAccounts.enable') : t('providerAccounts.disable')" :title="account.status === 'disabled' ? t('providerAccounts.enable') : t('providerAccounts.disable')" @click="runAccountAction(account, 'toggle')"><ShieldCheck v-if="account.status === 'disabled'" :size="16" /><ShieldOff v-else :size="16" /></button>
            <button v-if="activeCooldownUntil(account)" class="icon-button" type="button" :disabled="actionID === `cooldown:${account.id}`" :aria-label="t('providerAccounts.clearCooldown')" :title="t('providerAccounts.clearCooldown')" @click="runAccountAction(account, 'cooldown')"><CircleCheck :size="16" /></button>
          </div></td>
        </tr>
        <tr v-if="!filteredAccounts.length"><td colspan="7" class="empty-cell">{{ loading ? t('common.loading') : t('providerAccounts.empty') }}</td></tr>
      </tbody>
    </table></div></section>

    <div v-if="modalOpen" class="modal-backdrop" @click.self="closeModal">
      <section class="modal-card modal-card-wide account-wizard" role="dialog" aria-modal="true" :aria-label="editing ? t('providerAccounts.editAccount') : t('providerAccounts.newAccount')">
        <header class="modal-header"><div><h2>{{ editing ? t('providerAccounts.editAccount') : t('providerAccounts.newAccount') }}</h2><p>{{ selectedProvider?.name || t('providerAccounts.selectProvider') }}</p></div><button class="icon-button" type="button" :aria-label="t('common.close')" @click="closeModal"><X :size="19" /></button></header>
        <nav class="wizard-steps" :aria-label="t('providerAccounts.newAccount')"><button v-for="step in wizardSteps" :key="step.id" type="button" :class="{ active: wizardStep === step.id, complete: wizardStep > step.id }" @click="wizardStep = step.id"><CircleCheck v-if="wizardStep > step.id" :size="15" /><span v-else>{{ step.id }}</span>{{ step.label }}</button></nav>

        <div class="modal-body">
          <section v-if="wizardStep === 1" class="form-grid wizard-panel">
            <div class="field form-span-2"><label for="account-provider">{{ t('providerAccounts.provider') }}</label><select id="account-provider" v-model="form.provider_id" required @change="syncProvider"><option value="" disabled>{{ t('providerAccounts.selectProvider') }}</option><option v-for="provider in providers" :key="provider.id" :value="provider.id">{{ provider.name }} · {{ provider.type }}</option></select></div>
            <div class="field"><label for="account-name">{{ t('providerAccounts.name') }}</label><input id="account-name" v-model="form.name" required /></div>
            <div class="field"><label for="account-status">{{ t('providers.status') }}</label><select id="account-status" v-model="form.status"><option value="active">active</option><option value="disabled">disabled</option></select></div>
            <div class="field form-span-2"><label class="checkbox-line"><input v-model="form.schedulable" type="checkbox" /><span>{{ t('providerAccounts.schedulableHelp') }}</span></label></div>
          </section>

          <section v-if="wizardStep === 2" class="form-grid wizard-panel">
            <div class="field"><label for="account-auth">{{ t('providerAccounts.authType') }}</label><select id="account-auth" v-model="form.auth_type"><option v-for="auth in authOptions" :key="auth" :value="auth">{{ auth }}</option></select></div>
            <div class="field"><label for="account-expiry">{{ t('apiKeys.expiresAt') }}</label><input id="account-expiry" v-model="form.expires_at" type="date" /></div>
            <div v-for="field in adapterConfigFields" :key="field.key" class="field"><label :for="`adapter-${field.key}`">{{ t(`providerAccounts.adapterConfig.${field.key}`) }}</label><input :id="`adapter-${field.key}`" v-model="form.adapter_config[field.key]" :required="field.required" :placeholder="field.placeholder" /></div>
            <div v-if="authRequiresSecret" class="field form-span-2"><label for="account-secret">{{ t('providerAccounts.secret') }}</label><textarea id="account-secret" v-model="form.secret" rows="4" :required="!editing" autocomplete="new-password" class="provider-mono-input" :placeholder="secretPlaceholder" /></div>
            <div v-else class="wizard-state form-span-2"><CircleCheck :size="20" /><strong>{{ form.auth_type }}</strong></div>
          </section>

          <section v-if="wizardStep === 3" class="form-grid wizard-panel">
            <div class="field"><label>{{ t('providerAccounts.concurrency') }}</label><input v-model.number="form.concurrency" type="number" min="0" /></div>
            <div class="field"><label>{{ t('providerAccounts.loadFactor') }}</label><input v-model.number="form.load_factor" type="number" min="0" /></div>
            <div class="field"><label>{{ t('providerAccounts.rpmLimit') }}</label><input v-model.number="form.rpm_limit" type="number" min="0" /></div>
            <div class="field"><label>{{ t('providerAccounts.tpmLimit') }}</label><input v-model.number="form.tpm_limit" type="number" min="0" /></div>
            <div class="field"><label>{{ t('providers.priority') }}</label><input v-model.number="form.priority" type="number" min="0" /></div>
            <div class="field"><label>{{ t('providerAccounts.weight') }}</label><input v-model.number="form.weight" type="number" min="1" max="10000" /></div>
            <div class="field"><label>{{ t('providerAccounts.circuitFailureThreshold') }}</label><input v-model.number="form.circuit_failure_threshold" type="number" min="1" max="100" /></div>
            <div class="field"><label>{{ t('providerAccounts.circuitOpenSeconds') }}</label><input v-model.number="form.circuit_open_seconds" type="number" min="1" max="86400" /></div>
            <div class="field form-span-2"><label>{{ t('providerAccounts.groups') }}</label><div class="check-list"><label v-for="group in groups" :key="group.id" class="checkbox-line"><input type="checkbox" :checked="form.group_ids.includes(group.id)" @change="toggleGroup(group.id)" /><span>{{ group.name }}</span></label></div></div>
            <div class="field form-span-2"><label>{{ t('providerAccounts.tempUnschedulableRules') }}</label><div v-for="(rule, index) in form.temp_unschedulable_rules" :key="index" class="rule-row"><input v-model.number="rule.status_code" type="number" min="100" max="599" /><input :value="rule.keywords.join(', ')" @input="setRuleKeywords(rule, ($event.target as HTMLInputElement).value)" /><input v-model.number="rule.duration_minutes" type="number" min="1" /><button class="icon-button" type="button" :aria-label="t('common.delete')" @click="removeRule(index)"><Trash2 :size="15" /></button></div><button class="button secondary" type="button" @click="addRule"><Plus :size="15" />{{ t('providerAccounts.addRule') }}</button></div>
          </section>

          <section v-if="wizardStep === 4" class="wizard-panel"><ProviderAccountModelEditor ref="modelEditor" v-model="form.models" v-model:auto-enable-new-models="form.auto_enable_new_models" :account-id="editing?.id" :discovery-enabled="modelDiscoveryEnabled" @synced="handleModelsSynced" /><div class="wizard-checklist"><span :class="{ complete: Boolean(form.provider_id) }"><CircleCheck :size="16" />{{ t('providerAccounts.provider') }}</span><span :class="{ complete: !authRequiresSecret || Boolean(editing?.secret_configured || form.secret) }"><CircleCheck :size="16" />{{ t('providerAccounts.secret') }}</span><span :class="{ complete: form.models.length > 0 }"><CircleCheck :size="16" />{{ t('providers.models') }} · {{ form.models.length }}</span><span :class="{ complete: form.schedulable }"><CircleCheck :size="16" />{{ t('providerAccounts.schedulable') }}</span></div></section>
        </div>

        <footer class="modal-footer"><button class="button secondary" type="button" @click="closeModal">{{ t('common.cancel') }}</button><button v-if="wizardStep > 1" class="button secondary" type="button" @click="previousStep"><ChevronLeft :size="17" />{{ t('common.previous') }}</button><button v-if="wizardStep < 4" class="button" type="button" :disabled="!canContinue()" @click="nextStep">{{ t('common.next') }}<ChevronRight :size="17" /></button><button v-else class="button" type="button" :disabled="saving || !canContinue()" @click="save"><Save :size="17" />{{ saving ? t('common.saving') : t('common.save') }}</button></footer>
      </section>
    </div>
  </main>
</template>

<style scoped>
.wizard-steps { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); border-bottom: 1px solid var(--border-color); }
.wizard-steps button { min-height: 48px; border: 0; border-right: 1px solid var(--border-color); background: transparent; color: var(--text-muted); display: flex; align-items: center; justify-content: center; gap: 8px; }
.wizard-steps button:last-child { border-right: 0; }
.wizard-steps button.active { color: var(--text-primary); box-shadow: inset 0 -2px 0 var(--accent-color); }
.wizard-steps button.complete { color: var(--success-color); }
.wizard-panel { min-height: 360px; }
.wizard-state { min-height: 64px; display: flex; align-items: center; gap: 10px; color: var(--success-color); border: 1px solid var(--border-color); padding: 16px; }
.wizard-checklist { margin-top: 20px; display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); border: 1px solid var(--border-color); }
.wizard-checklist span { min-height: 48px; display: flex; align-items: center; gap: 7px; padding: 10px; border-right: 1px solid var(--border-color); color: var(--text-muted); }
.wizard-checklist span:last-child { border-right: 0; }
.wizard-checklist span.complete { color: var(--success-color); }
.form-span-2 { grid-column: 1 / -1; }
@media (max-width: 760px) { .wizard-steps button { font-size: 0; } .wizard-steps button span, .wizard-steps button svg { font-size: 13px; } .wizard-checklist { grid-template-columns: 1fr 1fr; } .wizard-checklist span { border-bottom: 1px solid var(--border-color); } .form-span-2 { grid-column: auto; } }
</style>
