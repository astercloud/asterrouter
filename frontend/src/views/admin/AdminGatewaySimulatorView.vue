<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { FlaskConical, Play, Route } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { getGatewayModels, simulateGatewayRouting } from '@/api/control'
import type { GatewayModel, GatewaySimulation } from '@/types'

const { t } = useI18n()
const loading = ref(false)
const error = ref('')
const result = ref<GatewaySimulation | null>(null)
const gatewayModels = ref<GatewayModel[]>([])
const form = reactive({ model: '', estimated_tokens: 1000 })
const activeGatewayModels = computed(() => gatewayModels.value.filter((item) => item.status === 'active'))

async function load() {
  loading.value = true
  error.value = ''
  try {
    gatewayModels.value = await getGatewayModels()
    form.model = activeGatewayModels.value[0]?.model_id || ''
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

async function simulate() {
  loading.value = true
  error.value = ''
  try { result.value = await simulateGatewayRouting(form.model, form.estimated_tokens) }
  catch (err) { error.value = err instanceof Error ? err.message : t('common.failed') }
  finally { loading.value = false }
}

onMounted(load)
</script>

<template>
  <main class="content crud-page">
    <section class="page-header"><div><h1>{{ t('admin.gatewaySimulator') }}</h1><p>{{ t('gatewaySimulator.subtitle') }}</p></div></section>
    <form class="panel" @submit.prevent="simulate"><div class="panel-body form-grid">
      <div class="field"><label>{{ t('gatewaySimulator.model') }}</label><select v-model="form.model" required><option v-if="!activeGatewayModels.length" value="" disabled>{{ t('apiKeys.noActiveModels') }}</option><option v-for="model in activeGatewayModels" :key="model.id" :value="model.model_id">{{ model.model_id }} · {{ model.name }}</option></select></div>
      <div class="field"><label>{{ t('gatewaySimulator.estimatedTokens') }}</label><input v-model.number="form.estimated_tokens" type="number" min="0" /></div>
    </div><footer class="modal-footer"><button class="button" type="submit" :disabled="loading || !form.model"><Play :size="17" />{{ loading ? t('common.loading') : t('gatewaySimulator.run') }}</button></footer></form>
    <div v-if="error" class="notice">{{ error }}</div>
    <template v-if="result">
      <div class="crud-summary"><span><strong>{{ result.status }}</strong>{{ t('gatewaySimulator.status') }}</span><span><strong>{{ result.resolved_model || '-' }}</strong>{{ t('gatewaySimulator.resolvedModel') }}</span><span><strong>{{ result.route_group || '-' }}</strong>{{ t('gatewaySimulator.routeGroup') }}</span><span><strong>{{ result.candidates.length }}</strong>{{ t('gatewaySimulator.candidates') }}</span></div>
      <div class="notice"><FlaskConical :size="16" />{{ result.summary }}</div>
      <section class="panel table-panel"><div class="panel-body table-scroll"><table class="data-table crud-table">
        <thead><tr><th>#</th><th>{{ t('gatewaySimulator.route') }}</th><th>{{ t('gatewaySimulator.account') }}</th><th>{{ t('modelRoutes.upstreamModel') }}</th><th>{{ t('gatewaySimulator.headroom') }}</th><th>{{ t('gatewaySimulator.limits') }}</th><th>{{ t('gatewaySimulator.decision') }}</th></tr></thead>
        <tbody>
          <tr v-for="candidate in result.candidates" :key="candidate.route_id"><td>{{ candidate.rank }}</td><td><strong><Route :size="14" />{{ candidate.route_group }}</strong><span>{{ candidate.route_id }}</span></td><td><strong>{{ candidate.provider_id }}</strong><span>{{ candidate.provider_account_id }}</span></td><td><code>{{ candidate.upstream_model }}</code></td><td>{{ (candidate.headroom * 100).toFixed(1) }}%</td><td><strong>RPM {{ candidate.rpm_limit || '∞' }} · TPM {{ candidate.tpm_limit || '∞' }}</strong><span>{{ t('providerAccounts.concurrency') }} {{ candidate.concurrency }} · {{ candidate.circuit_state }}</span></td><td><span class="pill" :class="candidate.eligible ? 'status-success' : 'status-warning'">{{ candidate.eligible ? t('gatewaySimulator.eligible') : candidate.reason }}</span></td></tr>
          <tr v-if="!result.candidates.length"><td colspan="7" class="empty-cell"></td></tr>
        </tbody>
      </table></div></section>
    </template>
  </main>
</template>
