<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import { Activity, Edit3, Plus, RefreshCw, Save, Search, ShieldCheck, ShieldOff, Trash2, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import ProviderAccountModelEditor from '@/components/provider/ProviderAccountModelEditor.vue'
import {
  checkProviderAccount,
  clearProviderAccountCooldown,
  createProviderAccount,
  getProviderAccountHealthChecks,
  getProviderAccounts,
  getProviders,
  getRoutingGroups,
  updateProviderAccount
} from '@/api/control'
import type {
  ProviderAccount,
  ProviderAccountHealthCheck,
  ProviderAccountRequest,
  ProviderAccountTempUnschedulableRule,
  ProviderConnection,
  RoutingGroup
} from '@/types'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const togglingID = ref('')
const checkingID = ref('')
const clearingCooldownID = ref('')
const error = ref('')
const message = ref('')
const accounts = ref<ProviderAccount[]>([])
const groups = ref<RoutingGroup[]>([])
const providers = ref<ProviderConnection[]>([])
const healthChecks = ref<Record<string, ProviderAccountHealthCheck>>({})
const query = ref('')
const statusFilter = ref('')
const platformFilter = ref('')
const groupFilter = ref('')
const modalOpen = ref(false)
const editing = ref<ProviderAccount | null>(null)
const modelEditor = ref<{ discover: () => Promise<void> } | null>(null)

const form = reactive<ProviderAccountRequest>({
  provider_id: '',
  name: '',
  platform: 'openai_compatible',
  auth_type: 'api_key',
  status: 'active',
  schedulable: true,
  priority: 50,
  weight: 100,
  concurrency: 3,
  rpm_limit: 0,
  tpm_limit: 0,
  load_factor: null,
  rate_multiplier: 1,
  models: [],
  auto_enable_new_models: false,
  group_ids: [],
  secret: '',
  expires_at: '',
  circuit_failure_threshold: 5,
  circuit_open_seconds: 60,
  temp_unschedulable_rules: []
})

const groupByID = computed(() => new Map(groups.value.map((item) => [item.id, item])))
const providerByID = computed(() => new Map(providers.value.map((item) => [item.id, item])))
const platforms = computed(() => Array.from(new Set(accounts.value.map((item) => item.platform))).filter(Boolean).sort())

const filteredAccounts = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return accounts.value.filter((account) => {
    if (statusFilter.value && account.status !== statusFilter.value) return false
    if (platformFilter.value && account.platform !== platformFilter.value) return false
    if (groupFilter.value && !account.group_ids.includes(groupFilter.value)) return false
    if (!keyword) return true
    const groupNames = account.group_ids.map((id) => groupByID.value.get(id)?.name || id).join(' ')
    const providerName = providerByID.value.get(account.provider_id)?.name || ''
    return [account.name, providerName, account.platform, account.auth_type, groupNames, account.models.join(' ')].some((value) =>
      value.toLowerCase().includes(keyword)
    )
  })
})

const summary = computed(() => ({
  total: accounts.value.length,
  active: accounts.value.filter((item) => item.status === 'active').length,
  error: accounts.value.filter((item) => item.status === 'error').length,
  disabled: accounts.value.filter((item) => item.status === 'disabled').length,
  schedulable: accounts.value.filter((item) => item.status === 'active' && item.schedulable).length
}))

function splitModels(value: string): string[] {
  return value.split(/\n|,/).map((item) => item.trim()).filter(Boolean)
}

function dateInputValue(value?: string): string {
  return value ? value.slice(0, 10) : ''
}

function cloneRules(rules: ProviderAccountTempUnschedulableRule[]): ProviderAccountTempUnschedulableRule[] {
  return rules.map((rule) => ({ ...rule, keywords: [...rule.keywords] }))
}

function accountToRequest(account: ProviderAccount, status = account.status): ProviderAccountRequest {
  return {
    provider_id: account.provider_id,
    name: account.name,
    platform: account.platform,
    auth_type: account.auth_type,
    status,
    schedulable: account.schedulable,
    priority: account.priority,
    weight: account.weight,
    concurrency: account.concurrency,
    rpm_limit: account.rpm_limit,
    tpm_limit: account.tpm_limit,
    load_factor: account.load_factor ?? null,
    rate_multiplier: account.rate_multiplier,
    models: [...account.models],
    auto_enable_new_models: account.auto_enable_new_models,
    group_ids: [...account.group_ids],
    secret: '',
    expires_at: dateInputValue(account.expires_at),
    circuit_failure_threshold: account.circuit_failure_threshold,
    circuit_open_seconds: account.circuit_open_seconds,
    temp_unschedulable_rules: cloneRules(account.temp_unschedulable_rules)
  }
}

function resetForm() {
  const provider = providers.value[0]
  Object.assign(form, {
    provider_id: provider?.id || '',
    name: '',
    platform: provider?.type || 'openai_compatible',
    auth_type: 'api_key',
    status: 'active',
    schedulable: true,
    priority: 50,
    weight: 100,
    concurrency: 3,
    rpm_limit: 0,
    tpm_limit: 0,
    load_factor: null,
    rate_multiplier: 1,
    models: [],
    auto_enable_new_models: false,
    group_ids: groups.value[0] ? [groups.value[0].id] : [],
    secret: '',
    temp_unschedulable_rules: [],
    expires_at: '',
    circuit_failure_threshold: 5,
    circuit_open_seconds: 60
  })
}

function syncProviderPlatform() {
  const provider = providerByID.value.get(form.provider_id)
  if (provider) {
    form.platform = provider.type
  }
}

function openCreate() {
  editing.value = null
  resetForm()
  modalOpen.value = true
}

function openEdit(account: ProviderAccount) {
  editing.value = account
  Object.assign(form, accountToRequest(account))
  modalOpen.value = true
}

function closeModal() {
  modalOpen.value = false
  editing.value = null
}

function toggleGroup(groupID: string) {
  if (form.group_ids.includes(groupID)) {
    form.group_ids = form.group_ids.filter((id) => id !== groupID)
    return
  }
  form.group_ids = [...form.group_ids, groupID]
}

function handleModelsSynced(account: ProviderAccount) {
  editing.value = account
  Object.assign(form, accountToRequest(account))
  accounts.value = accounts.value.map((item) => item.id === account.id ? account : item)
  message.value = t('providerAccounts.modelsSynced', { count: account.models.length })
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [groupData, providerData, accountData, healthData] = await Promise.all([
      getRoutingGroups(),
      getProviders(),
      getProviderAccounts(),
      getProviderAccountHealthChecks()
    ])
    groups.value = groupData
    providers.value = providerData
    accounts.value = accountData
    healthChecks.value = Object.fromEntries(healthData.map((item) => [item.account_id, item]))
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
    const payload = {
      ...form,
      load_factor: form.load_factor ? Number(form.load_factor) : null
    }
    if (editing.value) {
      await updateProviderAccount(editing.value.id, payload)
      message.value = t('providerAccounts.updated')
      closeModal()
      await load()
    } else {
      const created = await createProviderAccount(payload)
      editing.value = created
      Object.assign(form, accountToRequest(created))
      accounts.value = [...accounts.value, created]
      message.value = t('providerAccounts.created')
      await nextTick()
      await modelEditor.value?.discover()
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

async function toggleStatus(account: ProviderAccount) {
  togglingID.value = account.id
  error.value = ''
  message.value = ''
  try {
    const nextStatus = account.status === 'disabled' ? 'active' : 'disabled'
    await updateProviderAccount(account.id, accountToRequest(account, nextStatus))
    message.value = nextStatus === 'active' ? t('providerAccounts.enabled') : t('providerAccounts.disabled')
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    togglingID.value = ''
  }
}

async function runCheck(account: ProviderAccount) {
  checkingID.value = account.id
  error.value = ''
  message.value = ''
  try {
    const result = await checkProviderAccount(account.id)
    healthChecks.value = { ...healthChecks.value, [account.id]: result }
    message.value = result.message
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    checkingID.value = ''
  }
}

async function clearCooldown(account: ProviderAccount) {
  clearingCooldownID.value = account.id
  error.value = ''
  message.value = ''
  try {
    await clearProviderAccountCooldown(account.id)
    message.value = t('providerAccounts.cooldownCleared')
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    clearingCooldownID.value = ''
  }
}

function addRule() {
  form.temp_unschedulable_rules = [...form.temp_unschedulable_rules, { status_code: 429, keywords: [], duration_minutes: 30 }]
}

function removeRule(index: number) {
  form.temp_unschedulable_rules = form.temp_unschedulable_rules.filter((_, i) => i !== index)
}

function ruleKeywordsText(rule: ProviderAccountTempUnschedulableRule): string {
  return rule.keywords.join(', ')
}

function setRuleKeywords(rule: ProviderAccountTempUnschedulableRule, value: string) {
  rule.keywords = splitModels(value)
}

function statusClass(status: string) {
  if (status === 'active' || status === 'ok') return 'status-success'
  if (status === 'error') return 'status-warning'
  return 'status-danger'
}

function formatHealth(check: ProviderAccountHealthCheck): string {
  const time = new Date(check.checked_at).toLocaleString()
  return `${check.status} / ${check.latency_ms}ms / ${time}`
}

function activeCooldownUntil(account: ProviderAccount): string {
  if (!account.cooldown_until) return ''
  const until = new Date(account.cooldown_until)
  if (until.getTime() <= Date.now()) return ''
  return until.toLocaleTimeString()
}

onMounted(load)
</script>

<template>
  <main class="content crud-page">
    <section class="page-header">
      <div>
        <h1>{{ t('admin.providerAccounts') }}</h1>
        <p>{{ t('providerAccounts.subtitle') }}</p>
      </div>
      <button class="button" type="button" @click="openCreate">
        <Plus :size="17" />
        {{ t('providerAccounts.newAccount') }}
      </button>
    </section>

    <div class="notice">{{ t('providerAccounts.advancedNotice') }}</div>

    <div class="crud-summary">
      <span><strong>{{ summary.total }}</strong>{{ t('providerAccounts.accounts') }}</span>
      <span><strong>{{ summary.active }}</strong>{{ t('dashboard.active') }}</span>
      <span><strong>{{ summary.schedulable }}</strong>{{ t('providerAccounts.schedulable') }}</span>
      <span><strong>{{ summary.error }}</strong>{{ t('providerAccounts.error') }}</span>
      <span><strong>{{ summary.disabled }}</strong>{{ t('providers.disabled') }}</span>
    </div>

    <section class="table-toolbar">
      <label class="search-box">
        <Search :size="17" />
        <input v-model="query" :placeholder="t('providerAccounts.searchPlaceholder')" />
      </label>
      <select v-model="platformFilter">
        <option value="">{{ t('routingGroups.allPlatforms') }}</option>
        <option v-for="platform in platforms" :key="platform" :value="platform">{{ platform }}</option>
      </select>
      <select v-model="groupFilter">
        <option value="">{{ t('providerAccounts.allGroups') }}</option>
        <option v-for="group in groups" :key="group.id" :value="group.id">{{ group.name }}</option>
      </select>
      <select v-model="statusFilter">
        <option value="">{{ t('providers.allStatuses') }}</option>
        <option value="active">active</option>
        <option value="error">error</option>
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
              <th>{{ t('providerAccounts.name') }}</th>
              <th>{{ t('providerAccounts.provider') }}</th>
              <th>{{ t('routingGroups.platform') }}</th>
              <th>{{ t('providers.status') }}</th>
              <th>{{ t('providerAccounts.groups') }}</th>
              <th>{{ t('providers.models') }}</th>
              <th>{{ t('providerAccounts.capacity') }}</th>
              <th>{{ t('providerAccounts.secret') }}</th>
              <th>{{ t('providerAccounts.health') }}</th>
              <th>{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="account in filteredAccounts" :key="account.id">
              <td>
                <strong>{{ account.name }}</strong>
                <span>{{ account.auth_type }}</span>
              </td>
              <td>
                <strong>{{ providerByID.get(account.provider_id)?.name || '-' }}</strong>
                <span>{{ account.provider_id || '-' }}</span>
              </td>
              <td>{{ account.platform }}</td>
              <td>
                <span class="pill" :class="statusClass(account.status)">{{ account.status }}</span>
                <span>{{ account.schedulable ? t('providerAccounts.schedulable') : t('providerAccounts.notSchedulable') }}</span>
              </td>
              <td>
                <div class="chip-list">
                  <span v-for="groupID in account.group_ids.slice(0, 3)" :key="groupID" class="pill">
                    {{ groupByID.get(groupID)?.name || groupID }}
                  </span>
                  <span v-if="account.group_ids.length > 3" class="pill">+{{ account.group_ids.length - 3 }}</span>
                  <span v-if="!account.group_ids.length" class="hint">-</span>
                </div>
              </td>
              <td>
                <div class="chip-list">
                  <span v-for="model in account.models.slice(0, 3)" :key="model" class="pill">{{ model }}</span>
                  <span v-if="account.models.length > 3" class="pill">+{{ account.models.length - 3 }}</span>
                </div>
              </td>
              <td>
                <strong>{{ account.concurrency }} / {{ account.priority }}</strong>
                <span>RPM {{ account.rpm_limit || '∞' }} · TPM {{ account.tpm_limit || '∞' }}</span>
                <span>W{{ account.weight }} · {{ t('providerAccounts.multiplier') }} {{ account.rate_multiplier }}</span>
                <span v-if="account.circuit_state !== 'closed' || account.consecutive_failures" class="pill status-warning">{{ account.circuit_state }} · {{ account.consecutive_failures }}</span>
                <span v-if="activeCooldownUntil(account)" class="pill status-warning">
                  {{ t('providerAccounts.cooldownUntil') }} {{ activeCooldownUntil(account) }}
                </span>
                <span v-if="activeCooldownUntil(account) && account.temp_unschedulable_reason" class="hint">
                  {{ account.temp_unschedulable_reason }}
                </span>
              </td>
              <td>
                <span class="pill" :class="account.secret_configured ? 'status-success' : 'status-warning'">
                  {{ account.secret_configured ? account.secret_hint : t('providers.warning') }}
                </span>
              </td>
              <td>
                <template v-if="healthChecks[account.id]">
                  <span class="pill" :class="statusClass(healthChecks[account.id].status)">
                    {{ formatHealth(healthChecks[account.id]) }}
                  </span>
                  <span>{{ healthChecks[account.id].message }}</span>
                </template>
                <span v-else class="hint">{{ t('providers.notChecked') }}</span>
              </td>
              <td>
                <div class="row-actions">
                  <button class="button secondary" type="button" :disabled="checkingID === account.id" @click="runCheck(account)">
                    <Activity :size="15" />
                    {{ checkingID === account.id ? t('common.loading') : t('providers.check') }}
                  </button>
                  <button class="button secondary" type="button" @click="openEdit(account)">
                    <Edit3 :size="15" />
                    {{ t('common.edit') }}
                  </button>
                  <button class="button secondary" type="button" :disabled="togglingID === account.id" @click="toggleStatus(account)">
                    <ShieldCheck v-if="account.status === 'disabled'" :size="15" />
                    <ShieldOff v-else :size="15" />
                    {{ account.status === 'disabled' ? t('providerAccounts.enable') : t('providerAccounts.disable') }}
                  </button>
                  <button
                    v-if="activeCooldownUntil(account)"
                    class="button secondary"
                    type="button"
                    :disabled="clearingCooldownID === account.id"
                    @click="clearCooldown(account)"
                  >
                    <ShieldCheck :size="15" />
                    {{ clearingCooldownID === account.id ? t('common.loading') : t('providerAccounts.clearCooldown') }}
                  </button>
                </div>
              </td>
            </tr>
            <tr v-if="!filteredAccounts.length">
              <td colspan="10" class="empty-cell">
                {{ loading ? t('common.loading') : t('providerAccounts.empty') }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <div v-if="modalOpen" class="modal-backdrop" @click.self="closeModal">
      <section class="modal-card modal-card-wide" role="dialog" aria-modal="true" :aria-label="editing ? t('providerAccounts.editAccount') : t('providerAccounts.newAccount')">
        <header class="modal-header">
          <div>
            <h2>{{ editing ? t('providerAccounts.editAccount') : t('providerAccounts.newAccount') }}</h2>
            <p>{{ t('providerAccounts.modalSubtitle') }}</p>
          </div>
          <button class="icon-button" type="button" :title="t('common.close')" :aria-label="t('common.close')" @click="closeModal">
            <X :size="19" />
          </button>
        </header>

        <div class="modal-body form-grid">
          <div class="field form-span-2">
            <label>{{ t('providerAccounts.provider') }}</label>
            <select v-model="form.provider_id" @change="syncProviderPlatform">
              <option value="">{{ t('providerAccounts.selectProvider') }}</option>
              <option v-for="provider in providers" :key="provider.id" :value="provider.id">
                {{ provider.name }} / {{ provider.type }}
              </option>
            </select>
          </div>
          <div class="field">
            <label>{{ t('providerAccounts.name') }}</label>
            <input v-model="form.name" placeholder="OpenAI Account A" />
          </div>
          <div class="field">
            <label>{{ t('routingGroups.platform') }}</label>
            <input v-model="form.platform" placeholder="openai_compatible" />
          </div>
          <div class="field">
            <label>{{ t('providerAccounts.authType') }}</label>
            <select v-model="form.auth_type">
              <option value="api_key">api_key</option>
            </select>
          </div>
          <div class="field">
            <label>{{ t('providers.status') }}</label>
            <select v-model="form.status">
              <option value="active">active</option>
              <option value="error">error</option>
              <option value="disabled">disabled</option>
            </select>
          </div>
          <div class="field">
            <label>{{ t('providerAccounts.concurrency') }}</label>
            <input v-model.number="form.concurrency" type="number" min="0" />
          </div>
          <div class="field"><label>{{ t('providerAccounts.rpmLimit') }}</label><input v-model.number="form.rpm_limit" type="number" min="0" /></div>
          <div class="field"><label>{{ t('providerAccounts.tpmLimit') }}</label><input v-model.number="form.tpm_limit" type="number" min="0" /></div>
          <div class="field">
            <label>{{ t('providerAccounts.loadFactor') }}</label>
            <input v-model.number="form.load_factor" type="number" min="0" :placeholder="t('providerAccounts.loadFactorPlaceholder')" />
          </div>
          <div class="field">
            <label>{{ t('providers.priority') }}</label>
            <input v-model.number="form.priority" type="number" min="0" />
          </div>
          <div class="field"><label>{{ t('providerAccounts.weight') }}</label><input v-model.number="form.weight" type="number" min="1" max="10000" /></div>
          <div class="field">
            <label>{{ t('routingGroups.rateMultiplier') }}</label>
            <input v-model.number="form.rate_multiplier" type="number" min="0" step="0.01" />
          </div>
          <div class="field">
            <label>{{ t('apiKeys.expiresAt') }}</label>
            <input v-model="form.expires_at" type="date" />
          </div>
          <div class="field"><label>{{ t('providerAccounts.circuitFailureThreshold') }}</label><input v-model.number="form.circuit_failure_threshold" type="number" min="1" max="100" /></div>
          <div class="field"><label>{{ t('providerAccounts.circuitOpenSeconds') }}</label><input v-model.number="form.circuit_open_seconds" type="number" min="1" max="86400" /></div>
          <label class="field checkbox-line form-span-2">
            <input v-model="form.schedulable" type="checkbox" />
            <span>{{ t('providerAccounts.schedulableHelp') }}</span>
          </label>
          <div class="field form-span-2">
            <label>{{ t('providerAccounts.groups') }}</label>
            <div class="check-list">
              <button
                v-for="group in groups"
                :key="group.id"
                class="pill"
                :class="{ 'status-success': form.group_ids.includes(group.id) }"
                type="button"
                @click="toggleGroup(group.id)"
              >
                {{ group.name }}
              </button>
              <span v-if="!groups.length" class="hint">{{ t('providerAccounts.noGroups') }}</span>
            </div>
          </div>
          <div class="field form-span-2">
            <ProviderAccountModelEditor
              ref="modelEditor"
              v-model="form.models"
              v-model:auto-enable-new-models="form.auto_enable_new_models"
              :account-id="editing?.id"
              @synced="handleModelsSynced"
            />
          </div>
          <div class="field form-span-2">
            <label>{{ t('providerAccounts.tempUnschedulableRules') }}</label>
            <p class="hint">{{ t('providerAccounts.tempUnschedulableRulesHelp') }}</p>
            <div class="rule-row" v-for="(rule, index) in form.temp_unschedulable_rules" :key="index">
              <input
                v-model.number="rule.status_code"
                type="number"
                min="100"
                max="599"
                :placeholder="t('providerAccounts.ruleStatusCode')"
              />
              <input
                :value="ruleKeywordsText(rule)"
                type="text"
                :placeholder="t('providerAccounts.ruleKeywords')"
                @input="setRuleKeywords(rule, ($event.target as HTMLInputElement).value)"
              />
              <input
                v-model.number="rule.duration_minutes"
                type="number"
                min="1"
                :placeholder="t('providerAccounts.ruleDurationMinutes')"
              />
              <button class="icon-button" type="button" @click="removeRule(index)">
                <Trash2 :size="15" />
              </button>
            </div>
            <button class="button secondary" type="button" @click="addRule">
              <Plus :size="15" />
              {{ t('providerAccounts.addRule') }}
            </button>
          </div>
          <div class="field form-span-2">
            <label>{{ t('providerAccounts.secret') }}</label>
            <input v-model="form.secret" :placeholder="editing ? t('providers.keepSecret') : 'sk-...'" />
            <small>{{ t('providers.secretHelp') }}</small>
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
