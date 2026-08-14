<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import {
  Activity,
  ArrowDown,
  ArrowUp,
  Check,
  CircleAlert,
  CheckCircle2,
  DollarSign,
  FlaskConical,
  Gauge,
  Layers3,
  ListOrdered,
  Plus,
  Play,
  RefreshCw,
  Route,
  Save,
  Settings2,
  ShieldCheck,
  Star,
  Trash2,
  XCircle,
  Workflow
} from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { createRoutingPolicy, getGatewayModels, getProcurementPrices, getProviderAccounts, getRoutingPolicies, simulateGatewayRouting, updateRoutingPolicy } from '@/api/control'
import type {
  GatewayModel,
  GatewaySimulation,
  ProviderAccount,
  ProcurementPrice,
  RoutingPolicy,
  RoutingPolicyBatch,
  RoutingPolicyPreset,
  RoutingPolicyRequest,
  RoutingPolicyStrategy
} from '@/types'

const { t, locale } = useI18n()
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const message = ref('')
const policies = ref<RoutingPolicy[]>([])
const accounts = ref<ProviderAccount[]>([])
const prices = ref<ProcurementPrice[]>([])
const gatewayModels = ref<GatewayModel[]>([])
const simulation = ref<GatewaySimulation | null>(null)
const simulating = ref(false)
const simulationError = ref('')
const simulationForm = reactive({ model: '', estimated_tokens: 1000, protocol: 'openai_chat_completions', required_features: ['text'] as string[] })
const selectedID = ref('')
const creating = ref(false)

const form = reactive<RoutingPolicyRequest>(defaultRequest())

const presetOptions: Array<{ id: RoutingPolicyPreset; icon: typeof DollarSign }> = [
  { id: 'cost', icon: DollarSign },
  { id: 'speed', icon: Gauge },
  { id: 'stability', icon: ShieldCheck },
  { id: 'balanced', icon: Workflow }
]
const protocolOptions = [
  'openai_chat_completions',
  'openai_responses',
  'openai_embeddings',
  'anthropic_messages',
  'anthropic_count_tokens',
  'gemini_generate_content',
  'openai_images_generations',
  'openai_media_generations',
  'openai_audio_transcriptions',
  'openai_audio_translations',
  'openai_audio_speech',
  'realtime',
  'aster_jobs'
] as const
const featureOptions = ['tools', 'stream', 'response_format', 'top_k']

const activeAccounts = computed(() => accounts.value.filter((account) => account.status === 'active'))
const activeGatewayModels = computed(() => gatewayModels.value.filter((model) => model.status === 'active'))
const selectedPolicy = computed(() => policies.value.find((policy) => policy.id === selectedID.value) || null)
const simulationModels = computed(() => {
  const routeGroup = selectedPolicy.value?.route_group
  return routeGroup ? activeGatewayModels.value.filter((model) => model.default_route_group === routeGroup) : activeGatewayModels.value
})
const preferredAccounts = computed(() => {
  if (!form.strategy.resource_batches.length) return activeAccounts.value
  const configured = new Set(form.strategy.resource_batches.flatMap((batch) => batch.provider_account_ids))
  return activeAccounts.value.filter((account) => configured.has(account.id))
})
const simulationGroups = computed(() => {
  const groups = new Map<string, { key: string; name: string; order: number; candidates: GatewaySimulation['candidates'] }>()
  for (const candidate of simulation.value?.candidates || []) {
    const key = `${candidate.policy_batch_order}:${candidate.policy_batch_name || 'dynamic'}`
    if (!groups.has(key)) {
      groups.set(key, {
        key,
        name: candidate.policy_batch_name || t('routingPolicy.simulation.dynamicBatch'),
        order: candidate.policy_batch_order,
        candidates: []
      })
    }
    groups.get(key)?.candidates.push(candidate)
  }
  return Array.from(groups.values()).sort((left, right) => left.order - right.order)
})
const allowedModelsText = computed({
  get: () => form.strategy.allowed_models.join('\n'),
  set: (value: string) => { form.strategy.allowed_models = splitLines(value) }
})
const deniedModelsText = computed({
  get: () => form.strategy.denied_models.join('\n'),
  set: (value: string) => { form.strategy.denied_models = splitLines(value) }
})
const hasCostGuardrail = computed(() => Boolean(
  form.strategy.absolute_max_input_per_1m ||
  form.strategy.absolute_max_output_per_1m ||
  form.strategy.max_price_multiple_of_cheapest ||
  form.strategy.model_price_limits.length ||
  form.strategy.missing_price_action === 'block' ||
  form.strategy.low_price_pool_mode !== 'none'
))
const activePriceCount = computed(() => prices.value.filter((price) => price.status === 'active' && price.currency === 'USD').length)
const configuredAccountCount = computed(() => new Set(
  form.strategy.resource_batches.flatMap((batch) => batch.provider_account_ids)
).size)
const decisionStages = computed(() => [
  { icon: Layers3, title: t('routingPolicy.flow.scope'), value: scopeSummary.value },
  { icon: ShieldCheck, title: t('routingPolicy.flow.hardRules'), value: hardRuleSummary.value },
  { icon: Gauge, title: t('routingPolicy.flow.preference'), value: t(`routingPolicy.presets.${form.strategy.preset}.name`) },
  { icon: ListOrdered, title: t('routingPolicy.flow.fallback'), value: batchSummary.value },
  { icon: Route, title: t('routingPolicy.flow.failover'), value: form.strategy.failover_before_first_byte ? t('routingPolicy.enabled') : t('routingPolicy.disabled') }
])
const scopeSummary = computed(() => form.strategy.allowed_models.length
  ? t('routingPolicy.preview.modelCount', { count: form.strategy.allowed_models.length })
  : t('routingPolicy.preview.allModels'))
const hardRuleSummary = computed(() => {
  const count = [
    form.strategy.native_protocol_only,
    hasCostGuardrail.value,
    form.strategy.denied_models.length > 0,
    form.strategy.allowed_protocols.length > 0 || form.strategy.denied_protocols.length > 0
  ].filter(Boolean).length
  return count ? t('routingPolicy.preview.ruleCount', { count }) : t('routingPolicy.preview.noHardRules')
})
const batchSummary = computed(() => form.strategy.resource_batches.length
  ? t('routingPolicy.preview.batchCount', { count: form.strategy.resource_batches.length })
  : t('routingPolicy.preview.dynamicFallback'))

function defaultStrategy(): RoutingPolicyStrategy {
  return {
    preset: 'balanced',
    smart_optimization: true,
    strict_order: false,
    failover_before_first_byte: true,
    sticky_routing: true,
    sticky_ttl_seconds: 900,
    native_protocol_only: false,
    absolute_max_input_per_1m: 0,
    absolute_max_output_per_1m: 0,
    max_price_multiple_of_cheapest: 2,
    low_price_pool_mode: 'auto',
    low_price_pool_percent: 30,
    low_price_pool_min_candidates: 2,
    missing_price_action: 'allow',
    model_price_limits: [],
    resource_batches: [],
    preferred_provider_account_ids: [],
    allowed_models: [],
    denied_models: [],
    allowed_protocols: [],
    denied_protocols: []
  }
}

function defaultRequest(): RoutingPolicyRequest {
  return {
    name: t('routingPolicy.defaultName'),
    description: t('routingPolicy.defaultDescription'),
    route_group: 'default',
    status: 'active',
    is_default: false,
    strategy: defaultStrategy()
  }
}

function splitLines(value: string): string[] {
  return Array.from(new Set(value.split(/[\n,]/).map((item) => item.trim()).filter(Boolean)))
}

function cloneRequest(policy: RoutingPolicy): RoutingPolicyRequest {
  const strategy = policy.strategy || defaultStrategy()
  return JSON.parse(JSON.stringify({
    name: policy.name,
    description: policy.description,
    route_group: policy.route_group,
    status: policy.status,
    is_default: policy.is_default,
    strategy: {
      ...defaultStrategy(),
      ...strategy,
      resource_batches: strategy.resource_batches || [],
      model_price_limits: strategy.model_price_limits || [],
      preferred_provider_account_ids: strategy.preferred_provider_account_ids || [],
      allowed_models: strategy.allowed_models || [],
      denied_models: strategy.denied_models || [],
      allowed_protocols: strategy.allowed_protocols || [],
      denied_protocols: strategy.denied_protocols || []
    }
  })) as RoutingPolicyRequest
}

function applyRequest(next: RoutingPolicyRequest) {
  Object.assign(form, next)
  form.strategy = next.strategy
}

function selectPolicy(policy: RoutingPolicy) {
  selectedID.value = policy.id
  creating.value = false
  error.value = ''
  message.value = ''
  simulation.value = null
  simulationError.value = ''
  applyRequest(cloneRequest(policy))
  const compatibleModels = gatewayModels.value.filter((model) => model.status === 'active' && model.default_route_group === policy.route_group)
  if (!compatibleModels.some((model) => model.model_id === simulationForm.model)) {
    simulationForm.model = compatibleModels[0]?.model_id || ''
  }
}

function createNew() {
  selectedID.value = ''
  creating.value = true
  error.value = ''
  message.value = ''
  simulation.value = null
  simulationError.value = ''
  applyRequest(defaultRequest())
}

async function load(preferredID = selectedID.value) {
  loading.value = true
  error.value = ''
  try {
    const [policyData, accountData, priceData, modelData] = await Promise.all([getRoutingPolicies(), getProviderAccounts(), getProcurementPrices(), getGatewayModels()])
    policies.value = policyData
    accounts.value = accountData
    prices.value = priceData
    gatewayModels.value = modelData
    if (!simulationForm.model) simulationForm.model = modelData.find((model) => model.status === 'active')?.model_id || ''
    const next = policyData.find((policy) => policy.id === preferredID) || policyData[0]
    if (next) selectPolicy(next)
    else createNew()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

async function save() {
  error.value = ''
  message.value = ''
  if (!form.name.trim() || !form.route_group.trim()) {
    error.value = t('routingPolicy.validationRequired')
    return
  }
  saving.value = true
  try {
    const payload = JSON.parse(JSON.stringify(form)) as RoutingPolicyRequest
    const result = selectedPolicy.value
      ? await updateRoutingPolicy(selectedPolicy.value.id, payload)
      : await createRoutingPolicy(payload)
    const successMessage = selectedPolicy.value ? t('routingPolicy.updated') : t('routingPolicy.created')
    creating.value = false
    await load(result.id)
    message.value = successMessage
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

function addBatch() {
  form.strategy.resource_batches.push({
    name: t('routingPolicy.batchName', { index: form.strategy.resource_batches.length + 1 }),
    provider_account_ids: []
  })
}

function removeBatch(index: number) {
  const [removed] = form.strategy.resource_batches.splice(index, 1)
  if (!removed || !form.strategy.resource_batches.length) return
  const configured = new Set(form.strategy.resource_batches.flatMap((batch) => batch.provider_account_ids))
  form.strategy.preferred_provider_account_ids = form.strategy.preferred_provider_account_ids.filter((id) => configured.has(id))
}

function addModelPriceLimit() {
  const model = activeGatewayModels.value.find((item) => !form.strategy.model_price_limits.some((limit) => limit.model === item.model_id))?.model_id || ''
  form.strategy.model_price_limits.push({ model, absolute_max_input_per_1m: 0, absolute_max_output_per_1m: 0 })
}

function removeModelPriceLimit(index: number) {
  form.strategy.model_price_limits.splice(index, 1)
}

function togglePreferredAccount(accountID: string) {
  const current = form.strategy.preferred_provider_account_ids
  form.strategy.preferred_provider_account_ids = current.includes(accountID)
    ? current.filter((id) => id !== accountID)
    : [...current, accountID]
}

function toggleSimulationFeature(feature: string) {
  simulationForm.required_features = simulationForm.required_features.includes(feature)
    ? simulationForm.required_features.filter((item) => item !== feature)
    : [...simulationForm.required_features, feature]
}

async function runSimulation() {
  if (!simulationForm.model) return
  simulating.value = true
  simulationError.value = ''
  try {
    simulation.value = await simulateGatewayRouting(
      simulationForm.model,
      simulationForm.estimated_tokens,
      simulationForm.protocol,
      simulationForm.required_features,
      selectedPolicy.value?.id || ''
    )
  } catch (err) {
    simulationError.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    simulating.value = false
  }
}

function decisionLabel(reason: string): string {
  if (!reason) return t('gatewaySimulator.eligible')
  const key = `gatewaySimulator.reasons.${reason.replace(/:/g, '_')}`
  const translated = t(key)
  return translated === key ? reason : translated
}

function formatMicrosPerMillion(value: number): string {
  return value > 0 ? `$${(value / 1_000_000).toFixed(4)}` : '-'
}

function formatSuccessRate(value: number, samples: number): string {
  return samples > 0 ? `${(value * 100).toFixed(1)}%` : '-'
}

function moveBatch(index: number, offset: number) {
  const target = index + offset
  if (target < 0 || target >= form.strategy.resource_batches.length) return
  const [batch] = form.strategy.resource_batches.splice(index, 1)
  form.strategy.resource_batches.splice(target, 0, batch)
}

function moveAccount(batch: RoutingPolicyBatch, index: number, offset: number) {
  const target = index + offset
  if (target < 0 || target >= batch.provider_account_ids.length) return
  const [accountID] = batch.provider_account_ids.splice(index, 1)
  batch.provider_account_ids.splice(target, 0, accountID)
}

function removeAccountFromBatch(batch: RoutingPolicyBatch, accountID: string) {
  batch.provider_account_ids = batch.provider_account_ids.filter((id) => id !== accountID)
  form.strategy.preferred_provider_account_ids = form.strategy.preferred_provider_account_ids.filter((id) => id !== accountID)
}

function accountName(accountID: string): string {
  return accounts.value.find((account) => account.id === accountID)?.name || accountID
}

function accountSelected(batch: RoutingPolicyBatch, accountID: string): boolean {
  return batch.provider_account_ids.includes(accountID)
}

function toggleAccount(batch: RoutingPolicyBatch, accountID: string) {
  if (accountSelected(batch, accountID)) {
    removeAccountFromBatch(batch, accountID)
  } else {
    for (const resourceBatch of form.strategy.resource_batches) {
      resourceBatch.provider_account_ids = resourceBatch.provider_account_ids.filter((id) => id !== accountID)
    }
    batch.provider_account_ids.push(accountID)
  }
}

function protocolRule(protocol: string): 'neutral' | 'allow' | 'deny' {
  if (form.strategy.denied_protocols.includes(protocol)) return 'deny'
  if (form.strategy.allowed_protocols.includes(protocol)) return 'allow'
  return 'neutral'
}

function setProtocolRule(protocol: string, rule: string) {
  form.strategy.allowed_protocols = form.strategy.allowed_protocols.filter((item) => item !== protocol)
  form.strategy.denied_protocols = form.strategy.denied_protocols.filter((item) => item !== protocol)
  if (rule === 'allow') form.strategy.allowed_protocols.push(protocol)
  if (rule === 'deny') form.strategy.denied_protocols.push(protocol)
}

function formatDate(value: string): string {
  if (!value) return '-'
  return new Intl.DateTimeFormat(locale.value, { month: 'short', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(new Date(value))
}

onMounted(() => load())
</script>

<template>
  <main class="content routing-policy-page">
    <section class="page-header policy-page-header">
      <div>
        <h1>{{ t('routingPolicy.title') }}</h1>
        <p>{{ t('routingPolicy.subtitle') }}</p>
      </div>
      <div class="header-actions">
        <button class="button secondary" type="button" :disabled="loading" @click="load()">
          <RefreshCw :size="17" />
          {{ t('common.refresh') }}
        </button>
        <button class="button" type="button" @click="createNew">
          <Plus :size="17" />
          {{ t('routingPolicy.newPolicy') }}
        </button>
      </div>
    </section>

    <div v-if="message" class="notice success">{{ message }}</div>
    <div v-if="error" class="notice">{{ error }}</div>

    <section class="policy-list-section" aria-labelledby="policy-list-title">
      <div class="section-heading list-heading">
        <div>
          <span class="section-kicker">{{ t('routingPolicy.policyLibrary') }}</span>
          <h2 id="policy-list-title">{{ t('routingPolicy.policyList') }}</h2>
        </div>
        <span class="policy-count">{{ policies.length }}</span>
      </div>
      <div class="policy-table-wrap">
        <table class="policy-table">
          <thead>
            <tr>
              <th>{{ t('routingPolicy.name') }}</th>
              <th>{{ t('routingPolicy.routeGroup') }}</th>
              <th>{{ t('routingPolicy.preset') }}</th>
              <th>{{ t('routingPolicy.guardrails') }}</th>
              <th>{{ t('routingPolicy.status') }}</th>
              <th>{{ t('routingPolicy.updatedAt') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="policy in policies"
              :key="policy.id"
              :class="{ selected: policy.id === selectedID }"
              tabindex="0"
              @click="selectPolicy(policy)"
              @keydown.enter="selectPolicy(policy)"
            >
              <td>
                <strong>{{ policy.name }}</strong>
                <small>v{{ policy.version }} · {{ policy.description || '-' }}</small>
                <span v-if="policy.is_default" class="mini-tag">{{ t('routingPolicy.defaultPolicy') }}</span>
              </td>
              <td><code>{{ policy.route_group }}</code></td>
              <td>{{ t(`routingPolicy.presets.${policy.strategy.preset || 'balanced'}.name`) }}</td>
              <td>
                <span v-if="policy.strategy.native_protocol_only" class="mini-tag">{{ t('routingPolicy.nativeProtocolShort') }}</span>
                <span v-if="policy.strategy.max_price_multiple_of_cheapest" class="mini-tag">{{ policy.strategy.max_price_multiple_of_cheapest }}x</span>
                <span v-if="policy.strategy.failover_before_first_byte" class="mini-tag">{{ t('routingPolicy.failoverShort') }}</span>
              </td>
              <td><span class="status-dot" :class="policy.status">{{ policy.status === 'active' ? t('routingPolicy.active') : t('routingPolicy.disabled') }}</span></td>
              <td>{{ formatDate(policy.updated_at) }}</td>
            </tr>
            <tr v-if="!policies.length && !loading">
              <td colspan="6" class="empty-policy-cell">{{ t('routingPolicy.empty') }}</td>
            </tr>
            <tr v-if="loading">
              <td colspan="6" class="empty-policy-cell">{{ t('common.loading') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section class="policy-workbench">
      <div class="workbench-main">
        <section class="config-section identity-section">
          <div class="section-heading">
            <div>
              <span class="section-kicker">01</span>
              <h2>{{ creating ? t('routingPolicy.createTitle') : t('routingPolicy.editTitle') }}</h2>
              <p>{{ t('routingPolicy.identityHelp') }}</p>
            </div>
            <span v-if="selectedPolicy" class="version-badge">v{{ selectedPolicy.version }}</span>
          </div>
          <div class="form-grid three-columns">
            <label class="field">
              <span>{{ t('routingPolicy.name') }}</span>
              <input v-model="form.name" />
            </label>
            <label class="field">
              <span>{{ t('routingPolicy.routeGroup') }}</span>
              <input v-model="form.route_group" class="code-input" placeholder="default" />
              <small>{{ t('routingPolicy.routeGroupHelp') }}</small>
            </label>
            <label class="field">
              <span>{{ t('routingPolicy.status') }}</span>
              <select v-model="form.status">
                <option value="active">{{ t('routingPolicy.active') }}</option>
                <option value="disabled">{{ t('routingPolicy.disabled') }}</option>
              </select>
            </label>
            <label class="field default-policy-field">
              <span>{{ t('routingPolicy.defaultPolicy') }}</span>
              <span class="checkbox-control"><input v-model="form.is_default" type="checkbox" />{{ t('routingPolicy.defaultPolicyHelp') }}</span>
            </label>
            <label class="field full-span">
              <span>{{ t('routingPolicy.description') }}</span>
              <input v-model="form.description" />
            </label>
          </div>
        </section>

        <section class="config-section">
          <div class="section-heading">
            <div>
              <span class="section-kicker">02</span>
              <h2>{{ t('routingPolicy.presetTitle') }}</h2>
              <p>{{ t('routingPolicy.presetHelp') }}</p>
            </div>
          </div>
          <div class="preset-grid" role="radiogroup" :aria-label="t('routingPolicy.presetTitle')">
            <button
              v-for="option in presetOptions"
              :key="option.id"
              type="button"
              class="preset-option"
              :class="{ active: form.strategy.preset === option.id }"
              role="radio"
              :aria-checked="form.strategy.preset === option.id"
              @click="form.strategy.preset = option.id"
            >
              <span class="preset-icon"><component :is="option.icon" :size="18" /></span>
              <strong>{{ t(`routingPolicy.presets.${option.id}.name`) }}</strong>
              <small>{{ t(`routingPolicy.presets.${option.id}.help`) }}</small>
              <Check v-if="form.strategy.preset === option.id" class="preset-check" :size="17" />
            </button>
          </div>
        </section>

        <section class="config-section">
          <div class="section-heading">
            <div>
              <span class="section-kicker">03</span>
              <h2>{{ t('routingPolicy.flowTitle') }}</h2>
              <p>{{ t('routingPolicy.flowHelp') }}</p>
            </div>
          </div>
          <ol class="decision-flow">
            <li v-for="(stage, index) in decisionStages" :key="stage.title">
              <span class="flow-index">{{ index + 1 }}</span>
              <component :is="stage.icon" :size="18" />
              <div><strong>{{ stage.title }}</strong><small>{{ stage.value }}</small></div>
            </li>
          </ol>
        </section>

        <section class="config-section">
          <div class="section-heading">
            <div>
              <span class="section-kicker">04</span>
              <h2>{{ t('routingPolicy.hardRulesTitle') }}</h2>
              <p>{{ t('routingPolicy.hardRulesHelp') }}</p>
            </div>
            <ShieldCheck :size="20" />
          </div>
          <div class="settings-list">
            <div class="setting-row">
              <div><strong>{{ t('routingPolicy.nativeProtocolOnly') }}</strong><small>{{ t('routingPolicy.nativeProtocolOnlyHelp') }}</small></div>
              <label class="switch"><input v-model="form.strategy.native_protocol_only" type="checkbox" :aria-label="t('routingPolicy.nativeProtocolOnly')" /><span /></label>
            </div>
          </div>
          <div class="form-grid three-columns rule-inputs">
            <label class="field">
              <span>{{ t('routingPolicy.inputPriceCap') }}</span>
              <input v-model.number="form.strategy.absolute_max_input_per_1m" type="number" min="0" step="0.01" />
              <small>{{ t('routingPolicy.usdPerMillion') }}</small>
            </label>
            <label class="field">
              <span>{{ t('routingPolicy.outputPriceCap') }}</span>
              <input v-model.number="form.strategy.absolute_max_output_per_1m" type="number" min="0" step="0.01" />
              <small>{{ t('routingPolicy.usdPerMillion') }}</small>
            </label>
            <label class="field">
              <span>{{ t('routingPolicy.relativePriceCap') }}</span>
              <input v-model.number="form.strategy.max_price_multiple_of_cheapest" type="number" min="0" step="0.1" />
              <small>{{ t('routingPolicy.relativePriceCapHelp') }}</small>
            </label>
          </div>
          <div class="form-grid price-fact-policy">
            <label class="field">
              <span>{{ t('routingPolicy.missingPriceAction') }}</span>
              <select v-model="form.strategy.missing_price_action">
                <option value="allow">{{ t('routingPolicy.missingPriceActions.allow') }}</option>
                <option value="block">{{ t('routingPolicy.missingPriceActions.block') }}</option>
              </select>
              <small>{{ t('routingPolicy.missingPriceActionHelp') }}</small>
            </label>
          </div>
          <div class="model-price-limits">
            <div class="subsection-header">
              <div><strong>{{ t('routingPolicy.modelPriceLimits') }}</strong><span>{{ t('routingPolicy.modelPriceLimitsHelp') }}</span></div>
              <button class="button secondary" type="button" @click="addModelPriceLimit"><Plus :size="15" />{{ t('routingPolicy.addModelPriceLimit') }}</button>
            </div>
            <div v-if="form.strategy.model_price_limits.length" class="model-price-limit-list">
              <div v-for="(limit, index) in form.strategy.model_price_limits" :key="index" class="model-price-limit-row">
                <label class="field"><span>{{ t('gatewaySimulator.model') }}</span><select v-model="limit.model"><option value="" disabled>{{ t('routingPolicy.selectModel') }}</option><option v-for="model in activeGatewayModels" :key="model.id" :value="model.model_id">{{ model.model_id }}</option></select></label>
                <label class="field"><span>{{ t('routingPolicy.inputPriceCap') }}</span><input v-model.number="limit.absolute_max_input_per_1m" type="number" min="0" step="0.01" /></label>
                <label class="field"><span>{{ t('routingPolicy.outputPriceCap') }}</span><input v-model.number="limit.absolute_max_output_per_1m" type="number" min="0" step="0.01" /></label>
                <button class="icon-action danger" type="button" :aria-label="t('routingPolicy.removeModelPriceLimit')" @click="removeModelPriceLimit(index)"><Trash2 :size="16" /></button>
              </div>
            </div>
            <p v-else class="empty-inline">{{ t('routingPolicy.noModelPriceLimits') }}</p>
          </div>
          <div class="protocol-rule-list">
            <div class="protocol-rule-header">
              <strong>{{ t('routingPolicy.protocolRules') }}</strong>
              <span>{{ t('routingPolicy.protocolRulesHelp') }}</span>
            </div>
            <label v-for="protocol in protocolOptions" :key="protocol" class="protocol-rule-row">
              <code>{{ t(`routingPolicy.protocols.${protocol}`) }}</code>
              <select :value="protocolRule(protocol)" @change="setProtocolRule(protocol, ($event.target as HTMLSelectElement).value)">
                <option value="neutral">{{ t('routingPolicy.protocolNeutral') }}</option>
                <option value="allow">{{ t('routingPolicy.protocolAllow') }}</option>
                <option value="deny">{{ t('routingPolicy.protocolDeny') }}</option>
              </select>
            </label>
          </div>
          <div v-if="hasCostGuardrail && activePriceCount === 0" class="price-fact-warning">
            <CircleAlert :size="17" />
            <span>{{ t('routingPolicy.priceFactsMissing') }}</span>
          </div>
        </section>

        <section class="config-section split-config">
          <div>
            <div class="section-heading compact-heading">
              <div>
                <span class="section-kicker">05</span>
                <h2>{{ t('routingPolicy.lowPricePoolTitle') }}</h2>
                <p>{{ t('routingPolicy.lowPricePoolHelp') }}</p>
              </div>
            </div>
            <div class="form-grid">
              <label class="field full-span">
                <span>{{ t('routingPolicy.poolMode') }}</span>
                <select v-model="form.strategy.low_price_pool_mode">
                  <option value="auto">{{ t('routingPolicy.poolModes.auto') }}</option>
                  <option value="strict">{{ t('routingPolicy.poolModes.strict') }}</option>
                  <option value="percentile">{{ t('routingPolicy.poolModes.percentile') }}</option>
                  <option value="none">{{ t('routingPolicy.poolModes.none') }}</option>
                </select>
              </label>
              <label v-if="form.strategy.low_price_pool_mode === 'percentile'" class="field">
                <span>{{ t('routingPolicy.poolPercent') }}</span>
                <input v-model.number="form.strategy.low_price_pool_percent" type="number" min="1" max="100" />
              </label>
              <label v-if="form.strategy.low_price_pool_mode === 'auto' || form.strategy.low_price_pool_mode === 'percentile'" class="field">
                <span>{{ t('routingPolicy.minCandidates') }}</span>
                <input v-model.number="form.strategy.low_price_pool_min_candidates" type="number" min="0" max="20" />
              </label>
            </div>
          </div>
          <div>
            <div class="section-heading compact-heading">
              <div>
                <span class="section-kicker">06</span>
                <h2>{{ t('routingPolicy.modelScopeTitle') }}</h2>
                <p>{{ t('routingPolicy.modelScopeHelp') }}</p>
              </div>
            </div>
            <div class="form-grid">
              <label class="field">
                <span>{{ t('routingPolicy.allowedModels') }}</span>
                <textarea v-model="allowedModelsText" rows="4" :placeholder="t('routingPolicy.onePerLine')" />
              </label>
              <label class="field">
                <span>{{ t('routingPolicy.deniedModels') }}</span>
                <textarea v-model="deniedModelsText" rows="4" :placeholder="t('routingPolicy.onePerLine')" />
              </label>
            </div>
          </div>
        </section>

        <section class="config-section">
          <div class="section-heading">
            <div>
              <span class="section-kicker">07</span>
              <h2>{{ t('routingPolicy.batchesTitle') }}</h2>
              <p>{{ t('routingPolicy.batchesHelp') }}</p>
            </div>
            <button class="button secondary" type="button" @click="addBatch">
              <Plus :size="16" />{{ t('routingPolicy.addBatch') }}
            </button>
          </div>
          <div v-if="form.strategy.resource_batches.length" class="batch-list">
            <article v-for="(batch, index) in form.strategy.resource_batches" :key="index" class="batch-row">
              <div class="batch-order">
                <span>{{ index + 1 }}</span>
                <div>
                  <button type="button" :aria-label="t('routingPolicy.moveUp')" :disabled="index === 0" @click="moveBatch(index, -1)"><ArrowUp :size="15" /></button>
                  <button type="button" :aria-label="t('routingPolicy.moveDown')" :disabled="index === form.strategy.resource_batches.length - 1" @click="moveBatch(index, 1)"><ArrowDown :size="15" /></button>
                </div>
              </div>
              <div class="batch-content">
                <input v-model="batch.name" class="batch-name" :aria-label="t('routingPolicy.batchLabel')" />
                <div v-if="batch.provider_account_ids.length" class="batch-resource-order">
                  <strong>{{ t('routingPolicy.declaredResourceOrder') }}</strong>
                  <ol>
                    <li v-for="(accountID, accountIndex) in batch.provider_account_ids" :key="accountID">
                      <span class="resource-position">{{ accountIndex + 1 }}</span>
                      <div><strong>{{ accountName(accountID) }}</strong><code>{{ accountID }}</code></div>
                      <span class="resource-order-actions">
                        <button type="button" :aria-label="t('routingPolicy.moveResourceUp')" :disabled="accountIndex === 0" @click="moveAccount(batch, accountIndex, -1)"><ArrowUp :size="14" /></button>
                        <button type="button" :aria-label="t('routingPolicy.moveResourceDown')" :disabled="accountIndex === batch.provider_account_ids.length - 1" @click="moveAccount(batch, accountIndex, 1)"><ArrowDown :size="14" /></button>
                        <button type="button" class="danger" :aria-label="t('routingPolicy.removeResource')" @click="removeAccountFromBatch(batch, accountID)"><Trash2 :size="14" /></button>
                      </span>
                    </li>
                  </ol>
                </div>
                <div class="account-picker">
                  <button
                    v-for="account in activeAccounts"
                    :key="account.id"
                    type="button"
                    class="account-chip"
                    :class="{ selected: accountSelected(batch, account.id) }"
                    @click="toggleAccount(batch, account.id)"
                  >
                    <Check v-if="accountSelected(batch, account.id)" :size="13" />
                    {{ account.name }}
                  </button>
                  <span v-if="!activeAccounts.length" class="no-accounts">{{ t('routingPolicy.noAccounts') }}</span>
                </div>
              </div>
              <button class="icon-action danger" type="button" :aria-label="t('routingPolicy.removeBatch')" @click="removeBatch(index)">
                <Trash2 :size="16" />
              </button>
            </article>
          </div>
          <button v-else class="empty-batches" type="button" @click="addBatch">
            <ListOrdered :size="22" />
            <strong>{{ t('routingPolicy.emptyBatches') }}</strong>
            <span>{{ t('routingPolicy.emptyBatchesHelp') }}</span>
          </button>
          <div class="preferred-resources">
            <div class="subsection-header"><div><strong>{{ t('routingPolicy.preferredResources') }}</strong><span>{{ t('routingPolicy.preferredResourcesHelp') }}</span></div></div>
            <div class="account-picker preferred-picker">
              <button v-for="account in preferredAccounts" :key="account.id" type="button" class="account-chip" :class="{ selected: form.strategy.preferred_provider_account_ids.includes(account.id) }" @click="togglePreferredAccount(account.id)">
                <Star :size="13" :fill="form.strategy.preferred_provider_account_ids.includes(account.id) ? 'currentColor' : 'none'" />{{ account.name }}
              </button>
            </div>
          </div>
        </section>

        <section class="config-section">
          <div class="section-heading">
            <div>
              <span class="section-kicker">08</span>
              <h2>{{ t('routingPolicy.resilienceTitle') }}</h2>
              <p>{{ t('routingPolicy.resilienceHelp') }}</p>
            </div>
            <Settings2 :size="20" />
          </div>
          <div class="settings-list">
            <div class="setting-row emphasized">
              <div><strong>{{ t('routingPolicy.failoverBeforeFirstByte') }}</strong><small>{{ t('routingPolicy.failoverBeforeFirstByteHelp') }}</small></div>
              <label class="switch"><input v-model="form.strategy.failover_before_first_byte" type="checkbox" :aria-label="t('routingPolicy.failoverBeforeFirstByte')" /><span /></label>
            </div>
            <div class="setting-row">
              <div><strong>{{ t('routingPolicy.stickyRouting') }}</strong><small>{{ t('routingPolicy.stickyRoutingHelp') }}</small></div>
              <label class="switch"><input v-model="form.strategy.sticky_routing" type="checkbox" :aria-label="t('routingPolicy.stickyRouting')" /><span /></label>
            </div>
            <label v-if="form.strategy.sticky_routing" class="inline-setting">
              <span>{{ t('routingPolicy.stickyTTL') }}</span>
              <input v-model.number="form.strategy.sticky_ttl_seconds" type="number" min="60" max="86400" step="60" />
              <small>{{ t('routingPolicy.seconds') }}</small>
            </label>
            <div class="setting-row">
              <div><strong>{{ t('routingPolicy.smartOptimization') }}</strong><small>{{ t('routingPolicy.smartOptimizationHelp') }}</small></div>
              <label class="switch"><input v-model="form.strategy.smart_optimization" type="checkbox" :aria-label="t('routingPolicy.smartOptimization')" /><span /></label>
            </div>
            <div class="setting-row">
              <div><strong>{{ t('routingPolicy.strictOrder') }}</strong><small>{{ t('routingPolicy.strictOrderHelp') }}</small></div>
              <label class="switch"><input v-model="form.strategy.strict_order" type="checkbox" :aria-label="t('routingPolicy.strictOrder')" /><span /></label>
            </div>
          </div>
        </section>

        <section class="config-section simulation-section">
          <div class="section-heading">
            <div><span class="section-kicker">09</span><h2>{{ t('routingPolicy.simulation.title') }}</h2><p>{{ t('routingPolicy.simulation.help') }}</p></div>
            <FlaskConical :size="20" />
          </div>
          <form class="simulation-controls" @submit.prevent="runSimulation">
            <label class="field"><span>{{ t('gatewaySimulator.model') }}</span><select v-model="simulationForm.model" required><option value="" disabled>{{ t('routingPolicy.selectModel') }}</option><option v-for="model in simulationModels" :key="model.id" :value="model.model_id">{{ model.model_id }} · {{ model.name }}</option></select></label>
            <label class="field"><span>{{ t('gatewaySimulator.clientProtocol') }}</span><select v-model="simulationForm.protocol"><option v-for="protocol in protocolOptions" :key="protocol" :value="protocol">{{ t(`routingPolicy.protocols.${protocol}`) }}</option></select></label>
            <label class="field"><span>{{ t('gatewaySimulator.estimatedTokens') }}</span><input v-model.number="simulationForm.estimated_tokens" type="number" min="0" /></label>
            <div class="field simulation-features"><span>{{ t('gatewaySimulator.requiredFeatures') }}</span><div><label v-for="feature in featureOptions" :key="feature"><input type="checkbox" :checked="simulationForm.required_features.includes(feature)" @change="toggleSimulationFeature(feature)" />{{ feature }}</label></div></div>
            <button class="button" type="submit" :disabled="simulating || !selectedPolicy || selectedPolicy.status !== 'active' || !simulationForm.model"><Play :size="16" />{{ simulating ? t('common.loading') : t('routingPolicy.simulation.run') }}</button>
          </form>
          <p v-if="!selectedPolicy" class="simulation-note">{{ t('routingPolicy.simulation.saveFirst') }}</p>
          <div v-if="simulationError" class="notice">{{ simulationError }}</div>
          <div v-if="simulation" class="simulation-results">
            <div class="simulation-summary"><strong>{{ simulation.status }}</strong><span>{{ simulation.resolved_model }} · {{ simulation.route_group }} · {{ simulation.candidates.filter((candidate) => candidate.eligible).length }}/{{ simulation.candidates.length }}</span></div>
            <section v-for="group in simulationGroups" :key="group.key" class="simulation-batch">
              <header><strong>{{ t('routingPolicy.simulation.batch', { index: group.order + 1, name: group.name }) }}</strong><span>{{ group.candidates.length }}</span></header>
              <div class="policy-table-wrap"><table class="policy-table simulation-table"><thead><tr><th>#</th><th>{{ t('gatewaySimulator.account') }}</th><th>{{ t('routingPolicy.simulation.price') }}</th><th>{{ t('routingPolicy.simulation.quality') }}</th><th>{{ t('routingPolicy.simulation.capacity') }}</th><th>{{ t('gatewaySimulator.decision') }}</th></tr></thead><tbody>
                <tr v-for="candidate in group.candidates" :key="`${candidate.route_id}-${candidate.rank}`"><td>{{ candidate.rank }}</td><td><strong>{{ accounts.find((account) => account.id === candidate.provider_account_id)?.name || candidate.provider_account_id || '-' }}</strong><small>{{ candidate.upstream_model }}</small></td><td><strong>{{ formatMicrosPerMillion(candidate.estimated_input_micros_per_1m) }} / {{ formatMicrosPerMillion(candidate.estimated_output_micros_per_1m) }}</strong><small>{{ candidate.price_fact_present ? t('routingPolicy.simulation.priceKnown') : t('routingPolicy.simulation.priceUnknown') }}</small></td><td><strong>{{ formatSuccessRate(candidate.observed_success_rate, candidate.observed_sample_count) }}</strong><small>{{ candidate.observed_avg_latency_ms || '-' }} ms · n={{ candidate.observed_sample_count }}</small></td><td><strong>{{ Math.round(candidate.headroom * 100) }}%</strong><small>{{ candidate.circuit_state }}</small></td><td><span class="decision-state" :class="candidate.eligible ? 'eligible' : 'excluded'"><CheckCircle2 v-if="candidate.eligible" :size="14" /><XCircle v-else :size="14" />{{ decisionLabel(candidate.reason) }}</span><small :title="candidate.selection_reason">{{ candidate.selection_reason || '-' }}</small></td></tr>
              </tbody></table></div>
            </section>
          </div>
        </section>
      </div>

      <aside class="decision-preview">
        <div class="preview-header">
          <span><Activity :size="17" />{{ t('routingPolicy.preview.title') }}</span>
          <span class="live-indicator">{{ t('routingPolicy.preview.live') }}</span>
        </div>
        <div class="preview-policy-name">
          <small>{{ t('routingPolicy.preview.currentPolicy') }}</small>
          <strong>{{ form.name || t('routingPolicy.unnamed') }}</strong>
          <code>{{ form.route_group || 'default' }}</code>
        </div>
        <dl class="preview-metrics">
          <div><dt>{{ t('routingPolicy.preview.preset') }}</dt><dd>{{ t(`routingPolicy.presets.${form.strategy.preset}.name`) }}</dd></div>
          <div><dt>{{ t('routingPolicy.preview.models') }}</dt><dd>{{ scopeSummary }}</dd></div>
          <div><dt>{{ t('routingPolicy.preview.hardRules') }}</dt><dd>{{ hardRuleSummary }}</dd></div>
          <div><dt>{{ t('routingPolicy.preview.batches') }}</dt><dd>{{ batchSummary }}</dd></div>
          <div><dt>{{ t('routingPolicy.preview.resources') }}</dt><dd>{{ configuredAccountCount }}</dd></div>
        </dl>
        <div class="preview-chain">
          <div v-for="(stage, index) in decisionStages" :key="stage.title">
            <span>{{ index + 1 }}</span>
            <p><strong>{{ stage.title }}</strong><small>{{ stage.value }}</small></p>
          </div>
        </div>
        <div class="safety-note">
          <CircleAlert :size="17" />
          <p><strong>{{ t('routingPolicy.preview.safetyTitle') }}</strong><span>{{ t('routingPolicy.preview.safetyHelp') }}</span></p>
        </div>
      </aside>
    </section>

    <footer class="save-bar">
      <div>
        <strong>{{ creating ? t('routingPolicy.unsavedPolicy') : form.name }}</strong>
        <span>{{ t('routingPolicy.saveHint') }}</span>
      </div>
      <button class="button" type="button" :disabled="saving" @click="save">
        <Save :size="17" />
        {{ saving ? t('common.saving') : t('routingPolicy.savePolicy') }}
      </button>
    </footer>
  </main>
</template>

<style scoped>
.routing-policy-page { display: grid; gap: 20px; padding-bottom: 24px; }
.policy-page-header { align-items: flex-start; }
.header-actions { display: flex; flex-wrap: wrap; gap: 8px; }
.policy-list-section, .config-section, .decision-preview { border: 1px solid var(--border); background: var(--surface); }
.policy-list-section { overflow: hidden; }
.section-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; padding: 20px 22px; border-bottom: 1px solid var(--border); }
.section-heading h2 { margin: 2px 0 4px; font-size: 17px; letter-spacing: 0; }
.section-heading p { max-width: 720px; margin: 0; color: var(--text-muted); font-size: 13px; }
.section-kicker { color: var(--policy-accent); font-size: 11px; font-weight: 800; text-transform: uppercase; }
.list-heading { align-items: center; padding-block: 15px; }
.list-heading > div { display: grid; gap: 2px; }
.list-heading h2 { display: inline; margin-right: 8px; }
.policy-count, .version-badge { display: inline-flex; min-width: 28px; height: 25px; align-items: center; justify-content: center; padding: 0 8px; border: 1px solid var(--border); border-radius: 999px; background: var(--surface-subtle); color: var(--text-secondary); font-size: 12px; font-weight: 700; }
.policy-table-wrap { overflow-x: auto; }
.policy-table { width: 100%; min-width: 880px; border-collapse: collapse; }
.policy-table th { padding: 10px 16px; border-bottom: 1px solid var(--border); background: var(--surface-subtle); color: var(--text-muted); font-size: 11px; text-align: left; }
.policy-table td { padding: 13px 16px; border-bottom: 1px solid var(--border); color: var(--text-secondary); font-size: 13px; vertical-align: middle; }
.policy-table tbody tr { cursor: pointer; transition: background 140ms ease; }
.policy-table tbody tr:hover, .policy-table tbody tr.selected { background: var(--surface-hover); }
.policy-table tbody tr.selected td:first-child { box-shadow: inset 3px 0 var(--primary-500); }
.policy-table td strong, .policy-table td small { display: block; }
.policy-table td strong { color: var(--text); }
.policy-table td small { max-width: 360px; overflow: hidden; color: var(--text-muted); text-overflow: ellipsis; white-space: nowrap; }
.policy-table tbody tr.selected td small { color: var(--text-secondary); }
.policy-table code, .preview-policy-name code { color: var(--policy-accent); font-size: 12px; }
.mini-tag { display: inline-flex; margin: 2px 4px 2px 0; padding: 3px 7px; border: 1px solid var(--border); border-radius: 999px; background: var(--surface-subtle); color: var(--text-secondary); font-size: 11px; }
.status-dot { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; font-weight: 700; }
.status-dot::before { width: 7px; height: 7px; border-radius: 50%; background: var(--text-muted); content: ''; }
.status-dot.active { color: var(--success); }
.status-dot.active::before { background: var(--success); }
.empty-policy-cell { height: 88px; color: var(--text-muted); text-align: center !important; }
.policy-workbench { display: grid; grid-template-columns: minmax(0, 1fr) 310px; gap: 20px; align-items: start; }
.workbench-main { display: grid; gap: 16px; min-width: 0; }
.config-section { min-width: 0; }
.form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; padding: 20px 22px; }
.three-columns { grid-template-columns: repeat(3, minmax(0, 1fr)); }
.full-span { grid-column: 1 / -1; }
.field { display: grid; align-content: start; gap: 6px; min-width: 0; color: var(--text-secondary); font-size: 12px; font-weight: 700; }
.field input, .field select, .field textarea, .batch-name, .inline-setting input { width: 100%; min-height: 40px; border: 1px solid var(--border); border-radius: 6px; background: var(--surface); color: var(--text); padding: 9px 11px; }
.field textarea { resize: vertical; }
.field small, .inline-setting small { color: var(--text-muted); font-size: 11px; font-weight: 400; }
.checkbox-control { display: flex; min-height: 40px; align-items: center; gap: 8px; color: var(--text-secondary); font-weight: 500; }
.checkbox-control input { width: 16px; min-height: 16px; }
.preset-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 10px; padding: 20px 22px; }
.preset-option { position: relative; display: grid; min-height: 142px; align-content: start; gap: 8px; padding: 16px; border: 1px solid var(--border); border-radius: 7px; background: var(--surface); color: var(--text); text-align: left; cursor: pointer; }
.preset-option:hover { border-color: var(--border-strong); background: var(--surface-subtle); }
.preset-option.active { border-color: var(--primary-500); background: var(--surface-hover); box-shadow: inset 0 0 0 1px var(--primary-500); }
.preset-option small { color: var(--text-muted); font-size: 12px; line-height: 1.5; }
.preset-option.active small { color: var(--text-secondary); }
.preset-icon { display: grid; width: 34px; height: 34px; place-items: center; border: 1px solid var(--border); border-radius: 7px; color: var(--primary-700); background: var(--surface); }
.preset-check { position: absolute; top: 14px; right: 14px; color: var(--primary-600); }
.decision-flow { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); margin: 0; padding: 20px 22px; list-style: none; }
.decision-flow li { position: relative; display: grid; grid-template-columns: 24px 20px minmax(0, 1fr); gap: 8px; align-items: start; padding-right: 20px; }
.decision-flow li:not(:last-child)::after { position: absolute; top: 12px; right: 7px; width: 12px; border-top: 1px solid var(--border-strong); content: ''; }
.flow-index { display: grid; width: 22px; height: 22px; place-items: center; border-radius: 50%; background: var(--accent-900); color: white; font-size: 11px; font-weight: 800; }
.decision-flow svg { margin-top: 2px; color: var(--primary-600); }
.decision-flow strong, .decision-flow small { display: block; }
.decision-flow strong { font-size: 12px; }
.decision-flow small { margin-top: 3px; color: var(--text-muted); font-size: 11px; line-height: 1.4; }
.settings-list { display: grid; }
.setting-row { display: flex; min-height: 70px; align-items: center; justify-content: space-between; gap: 24px; padding: 14px 22px; border-bottom: 1px solid var(--border); }
.setting-row strong, .setting-row small { display: block; }
.setting-row strong { font-size: 13px; }
.setting-row small { margin-top: 3px; color: var(--text-muted); font-size: 12px; }
.setting-row.emphasized { background: color-mix(in srgb, var(--info-bg) 58%, var(--surface)); }
.rule-inputs { padding-top: 16px; }
.price-fact-warning { display: flex; align-items: center; gap: 8px; margin: 0 22px 20px; padding: 10px 12px; border: 1px solid color-mix(in srgb, var(--warning) 30%, var(--border)); background: var(--warning-bg); color: var(--warning); font-size: 12px; }
.price-fact-policy { grid-template-columns: minmax(240px, 420px); padding-top: 0; }
.model-price-limits, .preferred-resources { border-top: 1px solid var(--border); }
.subsection-header { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 14px 22px; }
.subsection-header > div { display: grid; gap: 3px; }
.subsection-header strong { color: var(--text); font-size: 13px; }
.subsection-header span { color: var(--text-muted); font-size: 12px; }
.model-price-limit-list { border-top: 1px solid var(--border); }
.model-price-limit-row { display: grid; grid-template-columns: minmax(180px, 1.3fr) repeat(2, minmax(140px, 1fr)) 32px; gap: 12px; align-items: end; padding: 14px 22px; border-bottom: 1px solid var(--border); }
.empty-inline, .simulation-note { margin: 0; padding: 14px 22px 18px; color: var(--text-muted); font-size: 12px; }
.protocol-rule-list { display: grid; margin: 0 22px 20px; border: 1px solid var(--border); }
.protocol-rule-header { display: grid; gap: 3px; padding: 11px 12px; border-bottom: 1px solid var(--border); background: var(--surface-subtle); }
.protocol-rule-header strong { font-size: 12px; }
.protocol-rule-header span { color: var(--text-muted); font-size: 11px; }
.protocol-rule-row { display: grid; grid-template-columns: minmax(0, 1fr) 150px; gap: 14px; align-items: center; min-height: 46px; padding: 7px 12px; border-bottom: 1px solid var(--border); }
.protocol-rule-row:last-child { border-bottom: 0; }
.protocol-rule-row code { overflow-wrap: anywhere; color: var(--text-secondary); font-size: 11px; }
.protocol-rule-row select { min-height: 32px; border: 1px solid var(--border); border-radius: 5px; background: var(--surface); color: var(--text); padding: 5px 8px; font-size: 11px; }
.split-config { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }
.split-config > div + div { border-left: 1px solid var(--border); }
.compact-heading { min-height: 114px; }
.batch-list { display: grid; }
.batch-row { display: grid; grid-template-columns: 56px minmax(0, 1fr) 36px; gap: 14px; align-items: start; padding: 16px 22px; border-bottom: 1px solid var(--border); }
.batch-row:last-child { border-bottom: 0; }
.batch-order { display: flex; align-items: center; gap: 8px; }
.batch-order > span { display: grid; width: 25px; height: 25px; place-items: center; border-radius: 5px; background: var(--accent-900); color: white; font-size: 12px; font-weight: 800; }
.batch-order > div { display: grid; }
.batch-order button, .icon-action { display: grid; width: 26px; height: 24px; place-items: center; background: transparent; color: var(--text-muted); cursor: pointer; }
.batch-order button:disabled { opacity: .25; cursor: default; }
.batch-content { display: grid; gap: 10px; }
.batch-name { max-width: 340px; font-weight: 700; }
.batch-resource-order { display: grid; gap: 7px; }
.batch-resource-order > strong { color: var(--text-muted); font-size: 11px; }
.batch-resource-order ol { display: grid; margin: 0; padding: 0; border: 1px solid var(--border); list-style: none; }
.batch-resource-order li { display: grid; grid-template-columns: 26px minmax(0, 1fr) auto; gap: 9px; align-items: center; min-height: 44px; padding: 6px 8px; border-bottom: 1px solid var(--border); }
.batch-resource-order li:last-child { border-bottom: 0; }
.batch-resource-order li > div { display: grid; min-width: 0; }
.batch-resource-order li strong { overflow: hidden; color: var(--text); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.batch-resource-order li code { overflow: hidden; color: var(--text-muted); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.resource-position { display: grid; width: 23px; height: 23px; place-items: center; background: var(--surface-subtle); color: var(--text-secondary); font-size: 11px; font-weight: 800; }
.resource-order-actions { display: flex; gap: 2px; }
.resource-order-actions button { display: grid; width: 27px; height: 27px; place-items: center; background: transparent; color: var(--text-muted); cursor: pointer; }
.resource-order-actions button:disabled { opacity: .25; cursor: default; }
.resource-order-actions button.danger:hover { color: var(--danger); }
.account-picker { display: flex; flex-wrap: wrap; gap: 6px; }
.account-chip { display: inline-flex; align-items: center; gap: 5px; min-height: 30px; padding: 5px 9px; border: 1px solid var(--border); border-radius: 999px; background: var(--surface-subtle); color: var(--text-secondary); font-size: 11px; cursor: pointer; }
.account-chip.selected { border-color: var(--primary-500); background: var(--primary-50); color: var(--primary-800); }
.preferred-picker { padding: 0 22px 18px; }
.no-accounts { color: var(--text-muted); font-size: 12px; }
.icon-action.danger:hover { color: var(--danger); }
.empty-batches { display: grid; width: calc(100% - 44px); min-height: 130px; place-items: center; gap: 5px; margin: 20px 22px; border: 1px dashed var(--border-strong); border-radius: 7px; background: var(--surface-subtle); color: var(--text-secondary); cursor: pointer; }
.empty-batches span { color: var(--text-muted); font-size: 12px; }
.inline-setting { display: grid; grid-template-columns: minmax(120px, 1fr) 130px auto; gap: 10px; align-items: center; padding: 12px 22px; border-bottom: 1px solid var(--border); color: var(--text-secondary); font-size: 12px; font-weight: 700; }
.simulation-controls { display: grid; grid-template-columns: minmax(160px, 1.2fr) minmax(160px, 1fr) minmax(120px, .7fr); gap: 12px; align-items: end; padding: 18px 22px; }
.simulation-controls > .button { width: 100%; justify-content: center; }
.simulation-features { grid-column: 1 / 3; }
.simulation-features > div { display: flex; min-height: 40px; align-items: center; flex-wrap: wrap; gap: 6px 12px; }
.simulation-features label { display: inline-flex; align-items: center; gap: 5px; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 11px; font-weight: 500; }
.simulation-results { border-top: 1px solid var(--border); }
.simulation-summary { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 14px 22px; background: var(--surface-subtle); }
.simulation-summary strong { color: var(--text); text-transform: uppercase; }
.simulation-summary span { color: var(--text-muted); font-size: 12px; overflow-wrap: anywhere; }
.simulation-batch { border-top: 1px solid var(--border); }
.simulation-batch > header { display: flex; align-items: center; justify-content: space-between; padding: 11px 22px; }
.simulation-batch > header strong { color: var(--text); font-size: 13px; }
.simulation-batch > header span { color: var(--text-muted); font-size: 12px; }
.simulation-table { min-width: 920px; }
.decision-state { display: inline-flex; align-items: center; gap: 5px; font-weight: 700; }
.decision-state.eligible { color: var(--success); }
.decision-state.excluded { color: var(--danger); }
.decision-preview { position: sticky; top: 18px; overflow: hidden; }
.preview-header { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 15px 16px; border-bottom: 1px solid var(--border); background: var(--surface-subtle); }
.preview-header > span:first-child { display: flex; align-items: center; gap: 7px; font-size: 13px; font-weight: 800; }
.live-indicator { color: var(--success); font-size: 10px; font-weight: 800; text-transform: uppercase; }
.preview-policy-name { display: grid; gap: 4px; padding: 18px 16px; border-bottom: 1px solid var(--border); }
.preview-policy-name small { color: var(--text-muted); font-size: 10px; text-transform: uppercase; }
.preview-policy-name strong { overflow-wrap: anywhere; }
.preview-policy-name code { width: fit-content; padding: 2px 5px; background: var(--surface-subtle); }
.preview-metrics { display: grid; margin: 0; padding: 8px 16px; }
.preview-metrics div { display: flex; justify-content: space-between; gap: 12px; padding: 9px 0; border-bottom: 1px solid var(--border); font-size: 11px; }
.preview-metrics dt { color: var(--text-muted); }
.preview-metrics dd { margin: 0; color: var(--text); font-weight: 700; text-align: right; }
.preview-chain { display: grid; padding: 12px 16px; }
.preview-chain > div { position: relative; display: grid; grid-template-columns: 24px minmax(0, 1fr); gap: 8px; padding-bottom: 12px; }
.preview-chain > div:not(:last-child)::after { position: absolute; top: 22px; bottom: 1px; left: 10px; border-left: 1px solid var(--border-strong); content: ''; }
.preview-chain > div > span { z-index: 1; display: grid; width: 21px; height: 21px; place-items: center; border: 1px solid var(--border-strong); border-radius: 50%; background: var(--surface); color: var(--text-secondary); font-size: 10px; font-weight: 800; }
.preview-chain p { margin: 0; }
.preview-chain strong, .preview-chain small { display: block; font-size: 11px; }
.preview-chain small { margin-top: 2px; color: var(--text-muted); }
.safety-note { display: flex; gap: 9px; margin: 0 16px 16px; padding: 11px; border: 1px solid color-mix(in srgb, var(--warning) 30%, var(--border)); background: var(--warning-bg); color: var(--warning); }
.safety-note p { margin: 0; }
.safety-note strong, .safety-note span { display: block; font-size: 11px; }
.safety-note span { margin-top: 2px; line-height: 1.45; }
.save-bar { position: sticky; z-index: 20; bottom: 18px; display: flex; align-items: center; justify-content: space-between; gap: 20px; padding: 12px 16px; border: 1px solid var(--border-strong); background: color-mix(in srgb, var(--surface) 92%, transparent); box-shadow: var(--shadow-md); backdrop-filter: blur(12px); }
.save-bar div { display: grid; }
.save-bar strong { font-size: 13px; }
.save-bar span { color: var(--text-muted); font-size: 11px; }

@media (max-width: 1180px) {
  .policy-workbench { grid-template-columns: 1fr; }
  .decision-preview { position: static; }
  .decision-flow { grid-template-columns: 1fr; gap: 10px; }
  .decision-flow li { padding: 0; }
  .decision-flow li:not(:last-child)::after { top: 22px; right: auto; bottom: -10px; left: 10px; width: 0; border-top: 0; border-left: 1px solid var(--border-strong); }
}

@media (max-width: 900px) {
  .preset-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .three-columns { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .split-config { grid-template-columns: 1fr; }
  .split-config > div + div { border-top: 1px solid var(--border); border-left: 0; }
}

@media (max-width: 640px) {
  .routing-policy-page { gap: 14px; padding-bottom: 24px; }
  .policy-page-header, .section-heading { align-items: stretch; }
  .header-actions, .header-actions .button, .section-heading .button { width: 100%; }
  .header-actions .button { justify-content: center; }
  .section-heading { flex-direction: column; padding: 16px; }
  .policy-table { min-width: 720px; }
  .preset-grid, .form-grid, .three-columns { grid-template-columns: 1fr; padding: 16px; }
  .full-span { grid-column: auto; }
  .preset-option { min-height: 118px; }
  .decision-flow { padding: 16px; }
  .setting-row { padding: 14px 16px; }
  .batch-row { grid-template-columns: 40px minmax(0, 1fr) 28px; gap: 8px; padding: 14px 16px; }
  .batch-order { display: grid; }
  .batch-name { max-width: none; }
  .empty-batches { width: calc(100% - 32px); margin: 16px; }
  .inline-setting { grid-template-columns: 1fr 100px auto; padding-inline: 16px; }
  .protocol-rule-list { margin-inline: 16px; }
  .protocol-rule-row { grid-template-columns: minmax(0, 1fr) 108px; }
  .model-price-limit-row { grid-template-columns: 1fr 1fr; padding-inline: 16px; }
  .model-price-limit-row .field:first-child { grid-column: 1 / -1; }
  .simulation-controls { grid-template-columns: 1fr; padding-inline: 16px; }
  .simulation-features { grid-column: auto; }
  .simulation-summary { align-items: flex-start; flex-direction: column; padding-inline: 16px; }
  .save-bar { bottom: 10px; }
  .save-bar div span { display: none; }
}

:global(html[data-theme="dark"]) .flow-index,
:global(html[data-theme="dark"]) .batch-order > span { background: var(--primary-700); }
:global(html[data-theme="dark"]) .account-chip.selected { background: color-mix(in srgb, var(--primary-950) 70%, var(--surface)); color: var(--primary-200); }
</style>
