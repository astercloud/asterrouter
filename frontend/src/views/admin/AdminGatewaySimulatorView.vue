<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { CheckCircle2, FlaskConical, Play, Route, ShieldAlert, XCircle } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { getGatewayModels, simulateGatewayRouting } from '@/api/control'
import type { GatewayModel, GatewaySimulation } from '@/types'

const { t } = useI18n()
const loading = ref(false)
const error = ref('')
const result = ref<GatewaySimulation | null>(null)
const gatewayModels = ref<GatewayModel[]>([])
const form = reactive({ model: '', estimated_tokens: 1000, protocol: 'openai_chat_completions', required_features: ['text'] as string[] })
const activeGatewayModels = computed(() => gatewayModels.value.filter((item) => item.status === 'active'))
const protocols = [
  { value: 'openai_chat_completions', label: 'OpenAI Chat Completions' },
  { value: 'openai_responses', label: 'OpenAI Responses' },
  { value: 'anthropic_messages', label: 'Anthropic Messages' },
  { value: 'gemini_generate_content', label: 'Gemini GenerateContent' }
]
const featureOptions = ['tools', 'stream', 'response_format', 'top_k']
const eligibleCount = computed(() => result.value?.candidates.filter((item) => item.eligible).length || 0)

async function load() {
  loading.value = true; error.value = ''
  try { gatewayModels.value = await getGatewayModels(); form.model = activeGatewayModels.value[0]?.model_id || '' }
  catch (err) { error.value = err instanceof Error ? err.message : t('common.failed') }
  finally { loading.value = false }
}

function toggleFeature(feature: string) {
  form.required_features = form.required_features.includes(feature) ? form.required_features.filter((item) => item !== feature) : [...form.required_features, feature]
}

async function simulate() {
  loading.value = true; error.value = ''
  try { result.value = await simulateGatewayRouting(form.model, form.estimated_tokens, form.protocol, form.required_features) }
  catch (err) { error.value = err instanceof Error ? err.message : t('common.failed') }
  finally { loading.value = false }
}

onMounted(load)
</script>

<template>
  <main class="content crud-page simulator-workbench">
    <section class="page-header"><div><h1>{{ t('admin.gatewaySimulator') }}</h1><p>{{ t('gatewaySimulator.subtitle') }}</p></div></section>

    <form class="simulator-controls" @submit.prevent="simulate">
      <div class="field"><label for="simulator-model">{{ t('gatewaySimulator.model') }}</label><select id="simulator-model" v-model="form.model" required><option v-if="!activeGatewayModels.length" value="" disabled>{{ t('apiKeys.noActiveModels') }}</option><option v-for="model in activeGatewayModels" :key="model.id" :value="model.model_id">{{ model.model_id }} · {{ model.name }}</option></select></div>
      <div class="field"><label for="simulator-protocol">{{ t('gatewaySimulator.clientProtocol') }}</label><select id="simulator-protocol" v-model="form.protocol"><option v-for="protocol in protocols" :key="protocol.value" :value="protocol.value">{{ protocol.label }}</option></select></div>
      <div class="field"><label for="simulator-tokens">{{ t('gatewaySimulator.estimatedTokens') }}</label><input id="simulator-tokens" v-model.number="form.estimated_tokens" type="number" min="0" /></div>
      <div class="field feature-field"><label>{{ t('gatewaySimulator.requiredFeatures') }}</label><div class="feature-checklist"><label v-for="feature in featureOptions" :key="feature"><input type="checkbox" :checked="form.required_features.includes(feature)" @change="toggleFeature(feature)" /><span>{{ feature }}</span></label></div></div>
      <button class="button" type="submit" :disabled="loading || !form.model"><Play :size="17" />{{ loading ? t('common.loading') : t('gatewaySimulator.run') }}</button>
    </form>

    <div v-if="error" class="notice">{{ error }}</div>
    <template v-if="result">
      <div class="crud-summary"><span><strong>{{ result.status }}</strong>{{ t('gatewaySimulator.status') }}</span><span><strong>{{ result.resolved_model || '-' }}</strong>{{ t('gatewaySimulator.resolvedModel') }}</span><span><strong>{{ result.route_group || '-' }}</strong>{{ t('gatewaySimulator.routeGroup') }}</span><span><strong>{{ eligibleCount }} / {{ result.candidates.length }}</strong>{{ t('gatewaySimulator.candidates') }}</span></div>
      <div class="simulation-flow"><span><FlaskConical :size="16" />{{ form.protocol }}</span><Route :size="16" /><span>{{ result.resolved_model }}:{{ result.route_group }}</span><Route :size="16" /><span>{{ t('gatewaySimulator.eligibleCount', { count: eligibleCount }) }}</span></div>
      <section class="panel table-panel"><div class="panel-body table-scroll"><table class="data-table crud-table">
        <thead><tr><th>#</th><th>{{ t('gatewaySimulator.route') }}</th><th>{{ t('gatewaySimulator.account') }}</th><th>{{ t('gatewaySimulator.adapter') }}</th><th>{{ t('modelRoutes.upstreamFormat') }}</th><th>{{ t('modelRoutes.upstreamModel') }}</th><th>{{ t('gatewaySimulator.limits') }}</th><th>{{ t('gatewaySimulator.decision') }}</th></tr></thead>
        <tbody><tr v-for="candidate in result.candidates" :key="candidate.route_id || `${candidate.provider_account_id}-${candidate.rank}`">
          <td><strong>{{ candidate.rank }}</strong></td>
          <td><code>{{ candidate.route_id || '-' }}</code><span>{{ candidate.route_group }}</span></td>
          <td><code>{{ candidate.provider_account_id || '-' }}</code><span>{{ candidate.provider_type }}</span></td>
          <td><strong>{{ candidate.adapter || '-' }}</strong></td>
          <td><code>{{ candidate.upstream_format || '-' }}</code></td>
          <td><code>{{ candidate.upstream_model }}</code></td>
          <td><span>RPM {{ candidate.rpm_limit || '∞' }} · TPM {{ candidate.tpm_limit || '∞' }}</span><span>{{ t('gatewaySimulator.concurrent', { count: candidate.concurrency }) }} · {{ candidate.circuit_state }}</span></td>
          <td><span class="pill" :class="candidate.eligible ? 'status-success' : 'status-danger'"><CheckCircle2 v-if="candidate.eligible" :size="14" /><XCircle v-else :size="14" />{{ candidate.eligible ? t('gatewaySimulator.eligible') : candidate.reason }}</span></td>
        </tr><tr v-if="!result.candidates.length"><td colspan="8" class="empty-cell"><ShieldAlert :size="18" />{{ result.summary }}</td></tr></tbody>
      </table></div></section>
    </template>
  </main>
</template>

<style scoped>
.simulator-controls { display: grid; grid-template-columns: minmax(220px, 1.2fr) minmax(220px, 1fr) 150px minmax(260px, 1.4fr) auto; align-items: end; gap: 12px; padding-block: 16px; border-block: 1px solid var(--border-color); }
.feature-checklist { min-height: 38px; display: flex; align-items: center; flex-wrap: wrap; gap: 6px 14px; }
.feature-checklist label { display: flex; align-items: center; gap: 5px; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; }
.simulation-flow { display: flex; align-items: center; gap: 12px; min-height: 48px; padding: 10px 0; color: var(--text-muted); }
.simulation-flow span { display: inline-flex; align-items: center; gap: 6px; color: var(--text-primary); }
@media (max-width: 1050px) { .simulator-controls { grid-template-columns: 1fr 1fr; } .simulator-controls .button { width: 100%; } }
@media (max-width: 620px) { .simulator-controls { grid-template-columns: 1fr; } .simulation-flow { flex-wrap: wrap; } }
</style>
