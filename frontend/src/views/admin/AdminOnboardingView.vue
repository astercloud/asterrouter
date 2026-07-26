<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import {
  Check,
  Clipboard,
  Eye,
  EyeOff,
  KeyRound,
  LoaderCircle,
  Play,
  RefreshCw,
  RotateCcw,
  Server,
	ShieldCheck,
	SquareTerminal
} from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { ApiClientError } from '@/api/client'
import {
  connectOnboardingModelSource,
  createOnboardingAPIKey,
  getAPIKeyClientConfig,
  getCompatibilityManifest,
  getOnboardingAPIKey,
  getOnboardingSession,
  publishOnboardingModel,
  startOnboardingSession,
  verifyOnboardingClient
} from '@/api/onboarding'
import type {
  APIKeyClientConfig,
  APIKeyRecord,
  ClientVerificationResult,
  CompatibilityManifest,
  CompatibilityRecord,
  OnboardingAPIKeyRequest,
  OnboardingClient,
  OnboardingModelSourceRequest,
  OnboardingPublishedModelRequest,
  OnboardingSession,
  OnboardingStep
} from '@/types'

const { t, locale } = useI18n()
const sessionStorageKey = 'asterrouter_onboarding_session_id'
const idempotencyStorageKey = 'asterrouter_onboarding_idempotency_key'

const session = ref<OnboardingSession | null>(null)
const apiKey = ref<APIKeyRecord | null>(null)
const clientConfig = ref<APIKeyClientConfig | null>(null)
const compatibilityManifest = ref<CompatibilityManifest | null>(null)
const verification = ref<ClientVerificationResult | null>(null)
const credential = ref('')
const credentialVisible = ref(false)
const selectedClient = ref<OnboardingClient>('codex')
const loading = ref(true)
const submitting = ref(false)
const configLoading = ref(false)
const compatibilityLoading = ref(true)
const compatibilityUnavailable = ref(false)
const error = ref('')
const copied = ref('')
const verificationIdempotencyKey = ref(newIdempotencyKey('verify'))

const sourceForm = ref<OnboardingModelSourceRequest>({
  provider_name: 'Primary source',
  provider_type: 'openai_compatible',
  base_url: '',
  account_name: 'Primary account',
  auth_type: 'api_key',
  secret: '',
  adapter_config: {},
  upstream_model: '',
  concurrency: 3,
  rpm_limit: 0,
  tpm_limit: 0
})
const modelForm = ref<OnboardingPublishedModelRequest>({
  model_id: '',
  name: '',
  description: '',
  modality: 'chat',
  route_group: '',
  upstream_model: '',
  upstream_format: ''
})
const apiKeyForm = ref<OnboardingAPIKeyRequest>({
  name: 'Engineering',
  qps_limit: 0,
  rpm_limit: 0,
  tpm_limit: 0,
  concurrency_limit: 3,
  monthly_token_limit: 100000,
  monthly_budget_micros: 0
})

const steps = computed(() => [
  { id: 'model_source', label: t('onboarding.steps.source'), icon: Server },
  { id: 'published_model', label: t('onboarding.steps.model'), icon: RefreshCw },
  { id: 'api_key', label: t('onboarding.steps.application'), icon: KeyRound },
	{ id: 'verification', label: t('onboarding.steps.verify'), icon: SquareTerminal }
])
const currentStepIndex = computed(() => stepRank(session.value?.current_step || 'started'))
const expired = computed(() => Boolean(session.value && Date.parse(session.value.expires_at) <= Date.now()))
const publicModel = computed(() => apiKey.value?.model_allowlist[0] || modelForm.value.model_id || session.value?.verification_model || '')
const canStartNew = computed(() => expired.value || session.value?.status === 'completed')
const traceLink = computed(() => verification.value?.trace_id ? `/admin/traces?q=${encodeURIComponent(verification.value.trace_id)}` : '')
const selectedCompatibilityRecords = computed(() => (compatibilityManifest.value?.records || [])
  .filter((record) => record.client === selectedClient.value)
  .sort((left, right) => {
    if (left.release_line !== right.release_line) return left.release_line === 'current' ? -1 : 1
    return left.language.localeCompare(right.language)
  }))

function stepRank(step: OnboardingStep): number {
  if (step === 'model_source') return 1
  if (step === 'published_model') return 2
  if (step === 'api_key') return 3
  if (step === 'verification') return 4
  return 0
}

function stepDone(index: number): boolean {
  return currentStepIndex.value > index || session.value?.status === 'completed'
}

function stepActive(index: number): boolean {
  if (session.value?.status === 'completed') return index === 3
  return Math.min(currentStepIndex.value, 3) === index
}

function newIdempotencyKey(prefix: string): string {
  const suffix = globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random().toString(16).slice(2)}`
  return `${prefix}-${suffix}`
}

function resetTransientState() {
  apiKey.value = null
  clientConfig.value = null
  verification.value = null
  credential.value = ''
  credentialVisible.value = false
  copied.value = ''
  error.value = ''
}

async function beginNewSession(reusePending = false) {
  loading.value = true
  resetTransientState()
	const pendingIdempotencyKey = reusePending ? localStorage.getItem(idempotencyStorageKey) : ''
	const idempotencyKey = pendingIdempotencyKey || newIdempotencyKey('session')
  localStorage.setItem(idempotencyStorageKey, idempotencyKey)
  localStorage.removeItem(sessionStorageKey)
  try {
    const created = await startOnboardingSession(idempotencyKey)
    session.value = created
    localStorage.setItem(sessionStorageKey, created.id)
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    loading.value = false
  }
}

async function loadOrStartSession() {
  loading.value = true
  const storedID = localStorage.getItem(sessionStorageKey)
  if (!storedID) {
		await beginNewSession(true)
    return
  }
  try {
    const stored = await getOnboardingSession(storedID)
    session.value = stored
    await hydrateAPIKey(stored)
  } catch (err) {
    if (err instanceof ApiClientError && (err.status === 404 || err.status === 410)) {
      localStorage.removeItem(sessionStorageKey)
      localStorage.removeItem(idempotencyStorageKey)
      await beginNewSession()
      return
    }
    error.value = errorMessage(err)
  } finally {
    loading.value = false
  }
}

async function loadCompatibility() {
  compatibilityLoading.value = true
  compatibilityUnavailable.value = false
  try {
    compatibilityManifest.value = await getCompatibilityManifest()
  } catch {
    compatibilityUnavailable.value = true
  } finally {
    compatibilityLoading.value = false
  }
}

async function hydrateAPIKey(stored: OnboardingSession) {
  if (!stored.api_key_id) return
	if (stored.verification_client) selectedClient.value = stored.verification_client
  if (stored.status === 'completed') {
    apiKey.value = await getOnboardingAPIKey(stored.api_key_id)
    verification.value = verificationFromSession(stored)
  } else {
    const replay = await createOnboardingAPIKey(stored.id, emptyAPIKeyRequest())
    apiKey.value = replay.api_key
    credential.value = replay.credential
    session.value = replay.session
    if (stored.failure_stage === 'verification') verification.value = verificationFromSession(stored)
  }
  await loadClientConfig()
}

function emptyAPIKeyRequest(): OnboardingAPIKeyRequest {
  return { name: '', qps_limit: 0, rpm_limit: 0, tpm_limit: 0, concurrency_limit: 0, monthly_token_limit: 0, monthly_budget_micros: 0 }
}

function verificationFromSession(stored: OnboardingSession): ClientVerificationResult {
  return {
    status: stored.status === 'completed' ? 'success' : 'failed',
    client: stored.verification_client || 'codex',
    api_key_id: stored.api_key_id || '',
    model: stored.verification_model || '',
    http_status: stored.verification_http_status || 0,
    operation_id: stored.verification_operation_id,
    trace_id: stored.verification_trace_id,
    error_code: stored.verification_error_code,
    recovery_action: stored.verification_recovery_action
  }
}

async function refreshSessionAfterFailure() {
  if (!session.value) return
  try {
    session.value = await getOnboardingSession(session.value.id)
  } catch {
    // Preserve the actionable error from the original request.
  }
}

async function submitSource() {
  if (!session.value || expired.value) return
  submitting.value = true
  error.value = ''
  try {
		const request: OnboardingModelSourceRequest = { ...sourceForm.value, adapter_config: { ...sourceForm.value.adapter_config } }
		const result = await connectOnboardingModelSource(session.value.id, request)
    session.value = result.session
    modelForm.value.upstream_model = sourceForm.value.upstream_model
    sourceForm.value.secret = ''
  } catch (err) {
    sourceForm.value.secret = ''
    error.value = errorMessage(err)
    await refreshSessionAfterFailure()
  } finally {
    submitting.value = false
  }
}

async function submitModel() {
  if (!session.value || expired.value) return
  submitting.value = true
  error.value = ''
  try {
    const result = await publishOnboardingModel(session.value.id, modelForm.value)
    session.value = result.session
    modelForm.value.model_id = result.published_model.model_id
  } catch (err) {
    error.value = errorMessage(err)
    await refreshSessionAfterFailure()
  } finally {
    submitting.value = false
  }
}

async function submitAPIKey() {
  if (!session.value || expired.value) return
  submitting.value = true
  error.value = ''
  try {
    const result = await createOnboardingAPIKey(session.value.id, apiKeyForm.value)
    session.value = result.session
    apiKey.value = result.api_key
    credential.value = result.credential
    await loadClientConfig()
  } catch (err) {
    error.value = errorMessage(err)
    await refreshSessionAfterFailure()
  } finally {
    submitting.value = false
  }
}

async function selectClient(client: OnboardingClient) {
  selectedClient.value = client
  if (apiKey.value) await loadClientConfig()
}

async function loadClientConfig() {
  if (!apiKey.value || !publicModel.value) return
  configLoading.value = true
  error.value = ''
  try {
    clientConfig.value = await getAPIKeyClientConfig(apiKey.value.id, selectedClient.value, publicModel.value)
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    configLoading.value = false
  }
}

async function runVerification() {
  if (!session.value || !apiKey.value || !credential.value || expired.value) return
  submitting.value = true
  error.value = ''
  try {
    const result = await verifyOnboardingClient(session.value.id, {
      client: selectedClient.value,
      model: publicModel.value,
      credential: credential.value
    }, verificationIdempotencyKey.value)
    session.value = result.session
    verification.value = result.verification
    if (result.verification.status === 'failed') verificationIdempotencyKey.value = newIdempotencyKey('verify')
  } catch (err) {
    error.value = errorMessage(err)
    await refreshSessionAfterFailure()
  } finally {
    submitting.value = false
  }
}

async function copyText(value: string, target: string) {
  if (!value) return
  await navigator.clipboard.writeText(value)
  copied.value = target
  window.setTimeout(() => {
    if (copied.value === target) copied.value = ''
  }, 1600)
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : t('common.failed')
}

function recoveryLabel(code?: string): string {
  if (!code) return ''
  const key = `onboarding.recovery.${code}`
  const translated = t(key)
	return translated === key ? code.replace(/_/g, ' ') : translated
}

function compatibilityStatusLabel(record: CompatibilityRecord): string {
  if (record.verification_status !== 'verified') return t('onboarding.compatibility.pending')
  if (record.evidence_level === 'sdk_runtime') return t('onboarding.compatibility.runtimeVerified')
  return t('onboarding.compatibility.protocolVerified')
}

function compatibilityLimitationsLabel(codes: string[]): string {
  return codes.map((code) => {
    const key = `onboarding.compatibility.limitations.${code}`
    const translated = t(key)
    return translated === key ? code.replace(/_/g, ' ') : translated
  }).join(' ')
}

function compatibilityLanguageLabel(language: CompatibilityRecord['language']): string {
  return t(`onboarding.compatibility.languages.${language}`)
}

function formatCompatibilityDate(value?: string): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return new Intl.DateTimeFormat(locale.value, { year: 'numeric', month: 'short', day: 'numeric' }).format(date)
}

onMounted(() => {
	void loadCompatibility()
  void loadOrStartSession()
})
</script>

<template>
  <div class="onboarding-page">
    <header class="page-header">
      <div>
        <h1>{{ t('onboarding.title') }}</h1>
        <p>{{ t('onboarding.subtitle') }}</p>
      </div>
	  <button v-if="canStartNew" class="button secondary" type="button" :disabled="loading || submitting" @click="beginNewSession(false)">
        <RotateCcw :size="16" />
        {{ t('onboarding.newSession') }}
      </button>
    </header>

    <nav class="onboarding-progress" :aria-label="t('onboarding.progress')">
      <template v-for="(step, index) in steps" :key="step.id">
        <div class="onboarding-progress-step" :class="{ active: stepActive(index), done: stepDone(index) }">
          <span class="onboarding-progress-icon">
            <Check v-if="stepDone(index)" :size="15" />
            <component :is="step.icon" v-else :size="16" />
          </span>
          <span>{{ step.label }}</span>
        </div>
        <span v-if="index < steps.length - 1" class="onboarding-progress-line" :class="{ done: stepDone(index) }"></span>
      </template>
    </nav>

    <div v-if="expired" class="notice onboarding-notice" role="alert">
      <strong>{{ t('onboarding.expired') }}</strong>
      <span>{{ t('onboarding.expiredAction') }}</span>
    </div>
    <div v-if="error" class="notice onboarding-notice" role="alert">{{ error }}</div>
    <div v-if="session?.status === 'failed'" class="notice onboarding-notice" role="alert">
      <strong>{{ t('onboarding.failedAt', { step: t(`onboarding.steps.${session.failure_stage === 'model_source' ? 'source' : session.failure_stage === 'published_model' ? 'model' : 'verify'}`) }) }}</strong>
      <span>{{ session.failure_code }}</span>
      <span>{{ recoveryLabel(session.recovery_hint) }}</span>
      <code v-if="session.provider_id">{{ session.provider_id }}</code>
      <code v-if="session.provider_account_id">{{ session.provider_account_id }}</code>
      <code v-if="session.gateway_model_id">{{ session.gateway_model_id }}</code>
    </div>

    <section v-if="loading" class="panel onboarding-panel" aria-live="polite">
      <div class="panel-body onboarding-loading"><LoaderCircle class="spin" :size="20" /> {{ t('common.loading') }}</div>
    </section>

    <section v-else-if="session" class="panel onboarding-panel">
      <form v-if="currentStepIndex === 0" class="panel-body" @submit.prevent="submitSource">
        <div class="onboarding-section-heading">
          <div><h2>{{ t('onboarding.source.title') }}</h2><p>{{ t('onboarding.source.subtitle') }}</p></div>
          <span class="status-badge">{{ t('onboarding.session', { id: session.id.slice(-8) }) }}</span>
        </div>
        <fieldset class="form-fieldset" :disabled="submitting || expired">
          <div class="form-grid">
            <div class="field"><label for="onboarding-provider-type">{{ t('onboarding.source.type') }}</label><select id="onboarding-provider-type" v-model="sourceForm.provider_type"><option value="openai_compatible">OpenAI-compatible</option><option value="anthropic_compatible">Anthropic-compatible</option><option value="gemini_compatible">Gemini-compatible</option></select></div>
            <div class="field"><label for="onboarding-provider-name">{{ t('onboarding.source.name') }}</label><input id="onboarding-provider-name" v-model.trim="sourceForm.provider_name" required /></div>
            <div class="field field-wide"><label for="onboarding-base-url">{{ t('onboarding.source.baseURL') }}</label><input id="onboarding-base-url" v-model.trim="sourceForm.base_url" type="url" placeholder="https://api.example.com/v1" required /></div>
            <div class="field"><label for="onboarding-account-name">{{ t('onboarding.source.account') }}</label><input id="onboarding-account-name" v-model.trim="sourceForm.account_name" required /></div>
            <div class="field"><label for="onboarding-auth-type">{{ t('onboarding.source.authType') }}</label><select id="onboarding-auth-type" v-model="sourceForm.auth_type"><option value="api_key">API Key</option><option value="bearer">Bearer</option></select></div>
            <div class="field"><label for="onboarding-upstream-model">{{ t('onboarding.source.upstreamModel') }}</label><input id="onboarding-upstream-model" v-model.trim="sourceForm.upstream_model" required /></div>
            <div class="field"><label for="onboarding-secret">{{ t('onboarding.source.secret') }}</label><input id="onboarding-secret" v-model="sourceForm.secret" type="password" autocomplete="new-password" required /></div>
            <div class="field"><label for="onboarding-source-concurrency">{{ t('onboarding.source.concurrency') }}</label><input id="onboarding-source-concurrency" v-model.number="sourceForm.concurrency" type="number" min="0" /></div>
            <div class="field"><label for="onboarding-source-rpm">RPM</label><input id="onboarding-source-rpm" v-model.number="sourceForm.rpm_limit" type="number" min="0" /></div>
            <div class="field"><label for="onboarding-source-tpm">TPM</label><input id="onboarding-source-tpm" v-model.number="sourceForm.tpm_limit" type="number" min="0" /></div>
          </div>
          <div class="onboarding-actions"><button class="button" type="submit"><RefreshCw :class="{ spin: submitting }" :size="16" />{{ submitting ? t('onboarding.source.checking') : t('onboarding.source.connect') }}</button></div>
        </fieldset>
      </form>

      <form v-else-if="currentStepIndex === 1" class="panel-body" @submit.prevent="submitModel">
        <div class="onboarding-section-heading"><div><h2>{{ t('onboarding.model.title') }}</h2><p>{{ t('onboarding.model.subtitle') }}</p></div></div>
        <fieldset class="form-fieldset" :disabled="submitting || expired">
          <div class="form-grid">
            <div class="field"><label for="onboarding-model-id">{{ t('onboarding.model.id') }}</label><input id="onboarding-model-id" v-model.trim="modelForm.model_id" required /></div>
            <div class="field"><label for="onboarding-model-name">{{ t('onboarding.model.name') }}</label><input id="onboarding-model-name" v-model.trim="modelForm.name" /></div>
            <div class="field"><label for="onboarding-model-upstream">{{ t('onboarding.source.upstreamModel') }}</label><input id="onboarding-model-upstream" v-model.trim="modelForm.upstream_model" required /></div>
            <div class="field"><label for="onboarding-route-group">{{ t('onboarding.model.routeGroup') }}</label><input id="onboarding-route-group" v-model.trim="modelForm.route_group" :placeholder="t('onboarding.model.defaultRoute')" /></div>
            <div class="field field-wide"><label for="onboarding-model-description">{{ t('onboarding.model.description') }}</label><textarea id="onboarding-model-description" v-model.trim="modelForm.description" rows="3"></textarea></div>
          </div>
          <div class="onboarding-actions"><button class="button" type="submit"><RefreshCw :class="{ spin: submitting }" :size="16" />{{ submitting ? t('common.saving') : t('onboarding.model.publish') }}</button></div>
        </fieldset>
      </form>

      <form v-else-if="currentStepIndex === 2" class="panel-body" @submit.prevent="submitAPIKey">
        <div class="onboarding-section-heading"><div><h2>{{ t('onboarding.application.title') }}</h2><p>{{ t('onboarding.application.subtitle') }}</p></div></div>
        <fieldset class="form-fieldset" :disabled="submitting || expired">
          <div class="form-grid">
            <div class="field field-wide"><label for="onboarding-api-key-name">{{ t('onboarding.application.name') }}</label><input id="onboarding-api-key-name" v-model.trim="apiKeyForm.name" required /></div>
            <div class="field"><label for="onboarding-api-key-concurrency">{{ t('onboarding.application.concurrency') }}</label><input id="onboarding-api-key-concurrency" v-model.number="apiKeyForm.concurrency_limit" type="number" min="0" /></div>
            <div class="field"><label for="onboarding-api-key-monthly">{{ t('onboarding.application.monthlyTokens') }}</label><input id="onboarding-api-key-monthly" v-model.number="apiKeyForm.monthly_token_limit" type="number" min="0" /></div>
            <div class="field"><label for="onboarding-api-key-rpm">RPM</label><input id="onboarding-api-key-rpm" v-model.number="apiKeyForm.rpm_limit" type="number" min="0" /></div>
            <div class="field"><label for="onboarding-api-key-tpm">TPM</label><input id="onboarding-api-key-tpm" v-model.number="apiKeyForm.tpm_limit" type="number" min="0" /></div>
          </div>
          <div class="onboarding-actions"><button class="button" type="submit"><KeyRound :size="16" />{{ submitting ? t('common.saving') : t('onboarding.application.create') }}</button></div>
        </fieldset>
      </form>

      <div v-else class="panel-body onboarding-verification">
        <div class="onboarding-section-heading"><div><h2>{{ t('onboarding.verify.title') }}</h2><p>{{ t('onboarding.verify.subtitle') }}</p></div><span v-if="apiKey" class="status-badge success">{{ apiKey.name }}</span></div>

        <div class="onboarding-client-tabs" role="group" :aria-label="t('onboarding.verify.client')">
          <button v-for="client in (['codex', 'claude_code', 'openai_sdk', 'anthropic_sdk'] as OnboardingClient[])" :key="client" type="button" :class="{ active: selectedClient === client }" :aria-pressed="selectedClient === client" @click="selectClient(client)">{{ t(`onboarding.clients.${client}`) }}</button>
        </div>

        <section class="onboarding-compatibility-band" aria-live="polite">
          <div class="onboarding-compatibility-header">
            <div><ShieldCheck :size="18" /><span><strong>{{ t('onboarding.compatibility.title') }}</strong><small>{{ compatibilityManifest ? t('onboarding.compatibility.router', { version: compatibilityManifest.router_version }) : t('onboarding.compatibility.subtitle') }}</small></span></div>
            <span v-if="compatibilityManifest" class="status-badge">{{ t('onboarding.compatibility.revision', { revision: compatibilityManifest.revision }) }}</span>
          </div>
          <div v-if="compatibilityLoading" class="onboarding-loading"><LoaderCircle class="spin" :size="18" />{{ t('common.loading') }}</div>
          <p v-else-if="compatibilityUnavailable" class="muted-copy">{{ t('onboarding.compatibility.unavailable') }}</p>
          <p v-else-if="!selectedCompatibilityRecords.length" class="muted-copy">{{ t('onboarding.compatibility.empty') }}</p>
          <div v-else class="onboarding-compatibility-records">
            <div v-for="record in selectedCompatibilityRecords" :key="record.id" class="onboarding-compatibility-record">
              <div class="onboarding-compatibility-version">
                <strong>{{ compatibilityLanguageLabel(record.language) }} · v{{ record.version }}</strong>
                <span>{{ t(`onboarding.compatibility.release.${record.release_line}`) }} · {{ record.protocol_version }}</span>
              </div>
              <span class="status-badge" :class="record.verification_status === 'verified' ? 'success' : 'status-warning'">{{ compatibilityStatusLabel(record) }}</span>
              <small>{{ record.verification_status === 'verified' ? t('onboarding.compatibility.validUntil', { date: formatCompatibilityDate(record.expires_at) }) : t('onboarding.compatibility.confirmBeforeUse') }}</small>
              <small v-if="record.known_limitations.length" class="onboarding-compatibility-limitation">{{ compatibilityLimitationsLabel(record.known_limitations) }}</small>
            </div>
          </div>
        </section>

        <section v-if="credential" class="onboarding-secret-band">
          <div><KeyRound :size="18" /><span><strong>{{ t('onboarding.verify.credential') }}</strong><small>{{ t('onboarding.verify.credentialWindow') }}</small></span></div>
          <div class="onboarding-secret-value">
            <input :type="credentialVisible ? 'text' : 'password'" :value="credential" readonly :aria-label="t('onboarding.verify.credential')" />
            <button class="icon-button" type="button" :title="credentialVisible ? t('auth.hidePassword') : t('auth.showPassword')" :aria-label="credentialVisible ? t('auth.hidePassword') : t('auth.showPassword')" @click="credentialVisible = !credentialVisible"><EyeOff v-if="credentialVisible" :size="16" /><Eye v-else :size="16" /></button>
            <button class="icon-button" type="button" :title="t('common.copy')" :aria-label="t('onboarding.verify.copyCredential')" @click="copyText(credential, 'credential')"><Check v-if="copied === 'credential'" :size="16" /><Clipboard v-else :size="16" /></button>
          </div>
        </section>

        <section class="onboarding-config-band" aria-live="polite">
		  <div class="onboarding-config-header"><div><SquareTerminal :size="18" /><span><strong>{{ t('onboarding.verify.configuration') }}</strong><small>{{ clientConfig?.file_path || clientConfig?.format || '' }}</small></span></div><button v-if="clientConfig" class="icon-button" type="button" :title="t('common.copy')" :aria-label="t('onboarding.verify.copyConfig')" @click="copyText(clientConfig.content, 'config')"><Check v-if="copied === 'config'" :size="16" /><Clipboard v-else :size="16" /></button></div>
          <div v-if="configLoading" class="onboarding-loading"><LoaderCircle class="spin" :size="18" />{{ t('common.loading') }}</div>
          <pre v-else-if="clientConfig" class="code-block"><code>{{ clientConfig.content }}</code></pre>
        </section>

        <div v-if="verification" class="onboarding-result" :class="verification.status" role="status">
          <Check v-if="verification.status === 'success'" :size="20" />
          <RefreshCw v-else :size="20" />
          <div><strong>{{ verification.status === 'success' ? t('onboarding.verify.success') : t('onboarding.verify.failed') }}</strong><span v-if="verification.error_code">{{ verification.error_code }} · {{ recoveryLabel(verification.recovery_action) }}</span><span v-if="verification.operation_id">{{ t('onboarding.verify.operation') }}: {{ verification.operation_id }}</span></div>
          <RouterLink v-if="traceLink" class="button secondary" :to="traceLink">{{ t('onboarding.verify.openTrace') }}</RouterLink>
        </div>

        <div v-if="session.status !== 'completed'" class="onboarding-actions">
          <button class="button" type="button" :disabled="submitting || !credential || configLoading || expired" @click="runVerification"><Play :size="16" />{{ submitting ? t('onboarding.verify.running') : t('onboarding.verify.run') }}</button>
        </div>
      </div>
    </section>
  </div>
</template>
