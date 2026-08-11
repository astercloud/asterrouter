<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import { Edit3, Plus, RefreshCw, Save, Search, ShieldCheck, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import GatewayModelPicker from '@/components/model/GatewayModelPicker.vue'
import { createGovernancePolicy, getGatewayModels, getGovernancePolicies, updateGovernancePolicy } from '@/api/control'
import type { GatewayModel, GovernancePolicy, GovernancePolicyRequest } from '@/types'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const message = ref('')
const query = ref('')
const statusFilter = ref('')
const scopeFilter = ref('')
const modalOpen = ref(false)
const editing = ref<GovernancePolicy | null>(null)
const policies = ref<GovernancePolicy[]>([])
const gatewayModels = ref<GatewayModel[]>([])
const policyNameInput = ref<HTMLInputElement | null>(null)

const form = reactive<GovernancePolicyRequest>({
  name: '',
  description: '',
  scope_type: 'global',
  scope_id: '',
  model_allowlist: [],
  model_denylist: [],
  qps_limit: 0,
  monthly_token_limit: 0,
  monthly_budget_micros: 0,
  overage_action: 'block',
  prompt_logging_mode: 'metadata_only',
  retention_days: 30,
  tool_call_allowed: true,
  image_input_allowed: true,
  web_access_allowed: false,
  status: 'active'
})

const filteredPolicies = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return policies.value.filter((policy) => {
    if (statusFilter.value && policy.status !== statusFilter.value) return false
    if (scopeFilter.value && policy.scope_type !== scopeFilter.value) return false
    if (!keyword) return true
    return [policy.name, policy.description, policy.scope_type, policy.scope_id, policy.model_allowlist.join(' '), policy.model_denylist.join(' ')].some((value) =>
      value.toLowerCase().includes(keyword)
    )
  })
})

const summary = computed(() => ({
  total: policies.value.length,
  active: policies.value.filter((item) => item.status === 'active').length,
  disabled: policies.value.filter((item) => item.status === 'disabled').length,
  scoped: policies.value.filter((item) => item.scope_type !== 'global').length
}))

function resetForm() {
  Object.assign(form, {
    name: '',
    description: '',
    scope_type: 'global',
    scope_id: '',
    model_allowlist: [],
    model_denylist: [],
    qps_limit: 0,
    monthly_token_limit: 0,
    monthly_budget_micros: 0,
    overage_action: 'block',
    prompt_logging_mode: 'metadata_only',
    retention_days: 30,
    tool_call_allowed: true,
    image_input_allowed: true,
    web_access_allowed: false,
    status: 'active'
  })
}

function openCreate() {
  editing.value = null
  resetForm()
  modalOpen.value = true
  void focusPolicyName()
}

function openEdit(policy: GovernancePolicy) {
  editing.value = policy
  Object.assign(form, {
    name: policy.name,
    description: policy.description,
    scope_type: policy.scope_type,
    scope_id: policy.scope_id,
    model_allowlist: [...policy.model_allowlist],
    model_denylist: [...policy.model_denylist],
    qps_limit: policy.qps_limit,
    monthly_token_limit: policy.monthly_token_limit,
    monthly_budget_micros: policy.monthly_budget_micros,
    overage_action: policy.overage_action,
    prompt_logging_mode: policy.prompt_logging_mode,
    retention_days: policy.retention_days,
    tool_call_allowed: policy.tool_call_allowed,
    image_input_allowed: policy.image_input_allowed,
    web_access_allowed: policy.web_access_allowed,
    status: policy.status
  })
  modalOpen.value = true
  void focusPolicyName()
}

async function focusPolicyName() {
  await nextTick()
  policyNameInput.value?.focus()
}

function closeModal() {
  modalOpen.value = false
  editing.value = null
}

function formatBudget(micros: number): string {
  return micros
    ? new Intl.NumberFormat(undefined, { style: 'currency', currency: 'USD', minimumFractionDigits: 2, maximumFractionDigits: 6 }).format(micros / 1_000_000)
    : t('apiKeys.unlimited')
}

function statusClass(status: string): string {
  return status === 'active' ? 'status-success' : 'status-danger'
}

function formatDate(value: string): string {
  return value ? new Date(value).toLocaleString() : '-'
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [policyList, modelList] = await Promise.all([getGovernancePolicies(), getGatewayModels()])
    policies.value = policyList
    gatewayModels.value = modelList
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
    const payload: GovernancePolicyRequest = {
      ...form,
      scope_id: form.scope_type === 'global' ? '' : form.scope_id.trim(),
      model_allowlist: [...form.model_allowlist],
      model_denylist: [...form.model_denylist]
    }
    if (editing.value) {
      await updateGovernancePolicy(editing.value.id, payload)
      message.value = t('policies.updated')
    } else {
      await createGovernancePolicy(payload)
      message.value = t('policies.created')
    }
    closeModal()
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
  <main class="content crud-page policy-page">
    <section class="page-header">
      <div>
        <h1>{{ t('admin.policies') }}</h1>
        <p>{{ t('policies.subtitle') }}</p>
      </div>
      <button class="button" type="button" @click="openCreate">
        <Plus :size="17" />
        {{ t('policies.newPolicy') }}
      </button>
    </section>

    <div class="crud-summary">
      <span><strong>{{ summary.total }}</strong>{{ t('policies.total') }}</span>
      <span><strong>{{ summary.active }}</strong>{{ t('dashboard.active') }}</span>
      <span><strong>{{ summary.disabled }}</strong>{{ t('providers.disabled') }}</span>
      <span><strong>{{ summary.scoped }}</strong>{{ t('policies.scoped') }}</span>
    </div>

    <section class="table-toolbar">
      <label class="search-box">
        <Search :size="17" />
        <input v-model="query" :placeholder="t('policies.searchPlaceholder')" />
      </label>
      <select v-model="scopeFilter" :aria-label="t('policies.scopeType')">
        <option value="">{{ t('policies.allScopes') }}</option>
        <option value="global">{{ t('policies.scopes.global') }}</option>
        <option value="api_key">{{ t('policies.scopes.api_key') }}</option>
      </select>
      <select v-model="statusFilter" :aria-label="t('policies.status')">
        <option value="">{{ t('providers.allStatuses') }}</option>
        <option value="active">{{ t('policies.statuses.active') }}</option>
        <option value="disabled">{{ t('policies.statuses.disabled') }}</option>
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
              <th>{{ t('policies.policy') }}</th>
              <th>{{ t('policies.scope') }}</th>
              <th>{{ t('policies.limits') }}</th>
              <th>{{ t('policies.modelRules') }}</th>
              <th>{{ t('common.version') }}</th>
              <th>{{ t('providers.status') }}</th>
              <th>{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="policy in filteredPolicies" :key="policy.id">
              <td>
                <strong>{{ policy.name }}</strong>
                <span>{{ policy.description || policy.id }}</span>
              </td>
              <td>
                <strong>{{ t(`policies.scopes.${policy.scope_type}`) }}</strong>
                <span>{{ policy.scope_id || '-' }}</span>
              </td>
              <td>
                <strong>{{ formatBudget(policy.monthly_budget_micros) }}</strong>
                <span>{{ policy.qps_limit || '-' }} QPS · {{ policy.monthly_token_limit || '-' }} {{ t('policies.tokens') }}</span>
              </td>
              <td>
                <strong>{{ t(`policies.overageActions.${policy.overage_action}`) }} · {{ t(`policies.promptModes.${policy.prompt_logging_mode}`) }}</strong>
                <span>{{ t('policies.modelRuleCount', { allow: policy.model_allowlist.length, deny: policy.model_denylist.length }) }}</span>
              </td>
              <td>
                <strong>v{{ policy.version || 1 }}</strong>
                <span>{{ policy.last_updated_by || '-' }} · {{ formatDate(policy.updated_at) }}</span>
              </td>
              <td><span class="pill" :class="statusClass(policy.status)">{{ t(`policies.statuses.${policy.status}`) }}</span></td>
              <td>
                <button class="button secondary" type="button" @click="openEdit(policy)">
                  <Edit3 :size="15" />
                  {{ t('common.edit') }}
                </button>
              </td>
            </tr>
            <tr v-if="!filteredPolicies.length">
              <td colspan="7" class="empty-cell">{{ loading ? t('common.loading') : t('policies.empty') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <div v-if="modalOpen" class="modal-backdrop" @click.self="closeModal" @keydown.esc="closeModal">
      <form class="modal-card modal-card-wide policy-modal" role="dialog" aria-modal="true" :aria-label="editing ? t('policies.editPolicy') : t('policies.newPolicy')" @submit.prevent="save">
        <header class="modal-header">
          <div>
            <h2>{{ editing ? t('policies.editPolicy') : t('policies.newPolicy') }}</h2>
            <p>{{ t('policies.modalSubtitle') }}</p>
          </div>
          <button class="icon-button" type="button" :aria-label="t('common.close')" :title="t('common.close')" @click="closeModal">
            <X :size="18" />
          </button>
        </header>

        <div class="modal-body policy-form-body">
          <section class="policy-form-section">
            <h3>{{ t('policies.basicInformation') }}</h3>
            <div class="form-grid policy-form-grid">
              <div class="field">
                <label for="policy-name">{{ t('policies.name') }}</label>
                <input id="policy-name" ref="policyNameInput" v-model.trim="form.name" required maxlength="120" autocomplete="off" />
              </div>
              <div class="field">
                <label for="policy-status">{{ t('policies.status') }}</label>
                <select id="policy-status" v-model="form.status">
                  <option value="active">{{ t('policies.statuses.active') }}</option>
                  <option value="disabled">{{ t('policies.statuses.disabled') }}</option>
                </select>
              </div>
              <div class="field form-span-2">
                <label for="policy-description">{{ t('policies.description') }}</label>
                <input id="policy-description" v-model.trim="form.description" autocomplete="off" />
              </div>
              <div class="field">
                <label for="policy-scope-type">{{ t('policies.scopeType') }}</label>
                <select id="policy-scope-type" v-model="form.scope_type">
                  <option value="global">{{ t('policies.scopes.global') }}</option>
                  <option value="api_key">{{ t('policies.scopes.api_key') }}</option>
                </select>
              </div>
              <div class="field">
                <label for="policy-scope-id">{{ t('policies.scopeId') }}</label>
                <input id="policy-scope-id" v-model.trim="form.scope_id" :disabled="form.scope_type === 'global'" :required="form.scope_type !== 'global'" autocomplete="off" />
              </div>
            </div>
          </section>

          <section class="policy-form-section">
            <h3>{{ t('policies.quotaAndData') }}</h3>
            <div class="form-grid policy-form-grid">
              <div class="field">
                <label for="policy-qps">{{ t('policies.qpsLimit') }}</label>
                <input id="policy-qps" v-model.number="form.qps_limit" type="number" min="0" />
              </div>
              <div class="field">
                <label for="policy-monthly-tokens">{{ t('policies.monthlyTokens') }}</label>
                <input id="policy-monthly-tokens" v-model.number="form.monthly_token_limit" type="number" min="0" />
              </div>
              <div class="field">
                <label for="policy-monthly-budget">{{ t('policies.monthlyBudget') }}</label>
                <input id="policy-monthly-budget" v-model.number="form.monthly_budget_micros" type="number" min="0" />
              </div>
              <div class="field">
                <label for="policy-retention-days">{{ t('policies.retentionDays') }}</label>
                <input id="policy-retention-days" v-model.number="form.retention_days" type="number" min="0" />
              </div>
              <div class="field">
                <label for="policy-overage-action">{{ t('policies.overageAction') }}</label>
                <select id="policy-overage-action" v-model="form.overage_action">
                  <option value="block">{{ t('policies.overageActions.block') }}</option>
                  <option value="warn">{{ t('policies.overageActions.warn') }}</option>
                  <option value="fallback">{{ t('policies.overageActions.fallback') }}</option>
                </select>
              </div>
              <div class="field">
                <label for="policy-prompt-mode">{{ t('policies.promptLoggingMode') }}</label>
                <select id="policy-prompt-mode" v-model="form.prompt_logging_mode">
                  <option value="disabled">{{ t('policies.promptModes.disabled') }}</option>
                  <option value="metadata_only">{{ t('policies.promptModes.metadata_only') }}</option>
                  <option value="redacted">{{ t('policies.promptModes.redacted') }}</option>
                </select>
              </div>
            </div>
          </section>

          <section class="policy-form-section">
            <h3>{{ t('policies.modelAccess') }}</h3>
            <div class="policy-model-grid">
              <div class="field">
                <span class="policy-field-label">{{ t('policies.allowlist') }}</span>
                <GatewayModelPicker v-model="form.model_allowlist" :models="gatewayModels" :disabled="saving" :aria-label="t('policies.allowlist')" />
              </div>
              <div class="field">
                <span class="policy-field-label">{{ t('policies.denylist') }}</span>
                <GatewayModelPicker v-model="form.model_denylist" :models="gatewayModels" :disabled="saving" :aria-label="t('policies.denylist')" />
              </div>
            </div>
          </section>

          <section class="policy-form-section">
            <h3>{{ t('policies.capabilities') }}</h3>
            <div class="policy-capability-grid">
              <label class="checkbox-label">
                <input v-model="form.tool_call_allowed" type="checkbox" />
                <span>{{ t('policies.toolCallAllowed') }}</span>
              </label>
              <label class="checkbox-label">
                <input v-model="form.image_input_allowed" type="checkbox" />
                <span>{{ t('policies.imageInputAllowed') }}</span>
              </label>
              <label class="checkbox-label">
                <input v-model="form.web_access_allowed" type="checkbox" />
                <span>{{ t('policies.webAccessAllowed') }}</span>
              </label>
            </div>
          </section>
        </div>

        <footer class="modal-footer">
          <button class="button secondary" type="button" @click="closeModal">{{ t('common.cancel') }}</button>
          <button class="button" type="submit" :disabled="saving">
            <Save :size="16" />
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </footer>
      </form>
    </div>
  </main>
</template>

<style scoped>
.policy-page {
  width: min(1440px, 100%);
  margin-inline: auto;
}

.policy-modal {
  width: min(820px, 100%);
}

.policy-form-body {
  display: grid;
  gap: 22px;
}

.policy-form-section {
  display: grid;
  gap: 14px;
}

.policy-form-section + .policy-form-section {
  padding-top: 20px;
  border-top: 1px solid var(--border);
}

.policy-form-section h3 {
  margin: 0;
  color: var(--text);
  font-size: 13px;
  font-weight: 750;
}

.policy-form-grid {
  gap: 14px 16px;
}

.policy-model-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.policy-field-label {
  color: var(--text-secondary);
  font-size: 12px;
  font-weight: 650;
}

.policy-capability-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}

.policy-capability-grid .checkbox-label {
  min-height: 42px;
  padding: 0 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--surface-subtle);
}

@media (max-width: 640px) {
  .policy-form-grid,
  .policy-model-grid,
  .policy-capability-grid {
    grid-template-columns: 1fr;
  }

  .policy-form-grid .form-span-2 {
    grid-column: auto;
  }
}
</style>
