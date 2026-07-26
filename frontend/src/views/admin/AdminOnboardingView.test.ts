import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as onboarding from '@/api/onboarding'
import type {
  APIKeyClientConfig,
  APIKeyRecord,
  CompatibilityManifest,
  CompatibilityRecord,
  OnboardingAPIKeyResult,
  OnboardingSession
} from '@/types'
import AdminOnboardingView from './AdminOnboardingView.vue'

vi.mock('@/api/onboarding', () => ({
  connectOnboardingModelSource: vi.fn(),
  createOnboardingAPIKey: vi.fn(),
  getAPIKeyClientConfig: vi.fn(),
  getCompatibilityManifest: vi.fn(),
  getOnboardingAPIKey: vi.fn(),
  getOnboardingSession: vi.fn(),
  publishOnboardingModel: vi.fn(),
  startOnboardingSession: vi.fn(),
  verifyOnboardingClient: vi.fn()
}))

function session(overrides: Partial<OnboardingSession> = {}): OnboardingSession {
  return {
    id: 'onb-session-1', actor: 'admin@example.test', status: 'in_progress', current_step: 'started', version: 1,
    created_at: '2026-07-26T08:00:00Z', updated_at: '2026-07-26T08:00:00Z', expires_at: '2099-07-27T08:00:00Z',
    completed_steps: [], pending_steps: ['model_source', 'published_model', 'api_key', 'verification'],
    ...overrides
  }
}

function apiKey(): APIKeyRecord {
  return {
    id: 'key-1', name: 'Engineering', status: 'active', prefix: 'ar_test', fingerprint: 'fingerprint', key_type: 'workspace',
    customer_id: '', owner_user_id: '', profile_scope: '', platform_tenant_id: '', gateway_principal_id: '', tenant_id: '', principal_type: '', principal_reference: '', policy_id: '',
    model_allowlist: ['team-model'], scopes: ['gateway:invoke', 'models:read'], allowed_modalities: ['metadata', 'text'],
    allowed_operations: ['list_models', 'chat_completion', 'count_tokens'], qps_limit: 0, rpm_limit: 0, tpm_limit: 0, concurrency_limit: 3,
    monthly_token_limit: 100000, monthly_budget_micros: 0, monthly_image_limit: 0, monthly_video_seconds_limit: 0, monthly_audio_seconds_limit: 0,
    allowed_cidrs: [], lane_policy: '', artifact_policy: '', rotation_family_id: '', replaces_key_id: '', replaced_by_key_id: '',
    created_at: '2026-07-26T08:03:00Z', updated_at: '2026-07-26T08:03:00Z'
  }
}

function apiKeyResult(current: OnboardingSession, credential = 'ar_one_time_credential'): OnboardingAPIKeyResult {
  return { session: current, api_key: apiKey(), credential }
}

function clientConfig(client: APIKeyClientConfig['client'] = 'codex'): APIKeyClientConfig {
  return {
    api_key_id: 'key-1', client, model: 'team-model', gateway_url: 'https://router.example.test/v1',
    credential_env: 'ASTERROUTER_API_KEY', format: client === 'codex' ? 'toml' : 'shell', file_path: client === 'codex' ? '~/.codex/config.toml' : undefined,
    content: 'model = "team-model"\nenv_key = "ASTERROUTER_API_KEY"', environment: { ASTERROUTER_API_KEY: '<one-time-credential>' },
    verification_path: '/api/v1/admin/api-keys/key-1/client-verifications', recovery_instructions: [], contains_secret: false
  }
}

function compatibilityRecord(overrides: Partial<CompatibilityRecord> = {}): CompatibilityRecord {
  return {
    id: 'codex-current', client: 'codex', language: 'cli', version: '0.145.0', release_line: 'current',
    router_version: '0.18.0-test', protocol_version: 'openai_responses_v1', suite: 'protocol-matrix', result: 'passed',
    verification_status: 'verified', capabilities: ['configuration', 'responses'],
    known_limitations: ['official_client_runtime_not_executed'], evidence_level: 'protocol_mock', evidence_reference: 'test-reference',
    version_source: 'registry', tested_at: '2026-07-26T08:00:00Z', expires_at: '2099-08-25T08:00:00Z',
    ...overrides
  }
}

function compatibility(): CompatibilityManifest {
  return {
    schema_version: 'asterrouter.compatibility.v1', revision: '2026-07-26.1', router_version: '0.18.0-test',
    generated_at: '2026-07-26T08:00:00Z', support_window_days: 30,
    records: [
      compatibilityRecord(),
      compatibilityRecord({ id: 'codex-previous', version: '0.144.6', release_line: 'previous' }),
      compatibilityRecord({ id: 'anthropic-current', client: 'anthropic_sdk', language: 'javascript', version: '0.115.0', protocol_version: 'anthropic_messages_v1' }),
      compatibilityRecord({ id: 'anthropic-previous', client: 'anthropic_sdk', language: 'javascript', version: '0.114.0', release_line: 'previous', protocol_version: 'anthropic_messages_v1' })
    ]
  }
}

function mountView() {
  return mount(AdminOnboardingView, {
    global: {
      plugins: [i18n],
      stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } }
    }
  })
}

function storageValues(): string[] {
  const values: string[] = []
  for (let index = 0; index < localStorage.length; index++) {
    const key = localStorage.key(index)
    if (key) values.push(localStorage.getItem(key) || '')
  }
  return values
}

describe('AdminOnboardingView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    setLocale('en-US')
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: vi.fn().mockResolvedValue(undefined) } })
    vi.mocked(onboarding.startOnboardingSession).mockResolvedValue(session())
    vi.mocked(onboarding.getAPIKeyClientConfig).mockImplementation(async (_id, client) => clientConfig(client))
    vi.mocked(onboarding.getCompatibilityManifest).mockResolvedValue(compatibility())
  })

  it('completes the four-step journey without persisting either credential', async () => {
    const sourceSession = session({ current_step: 'model_source', version: 3, completed_steps: ['model_source'], pending_steps: ['published_model', 'api_key', 'verification'] })
    const modelSession = session({ current_step: 'published_model', version: 5, gateway_model_id: 'gateway-model-1', model_route_id: 'route-1', completed_steps: ['model_source', 'published_model'], pending_steps: ['api_key', 'verification'] })
    const apiKeySession = session({ current_step: 'api_key', version: 6, api_key_id: 'key-1', completed_steps: ['model_source', 'published_model', 'api_key'], pending_steps: ['verification'] })
    const completedSession = session({
      status: 'completed', current_step: 'verification', version: 7, api_key_id: 'key-1',
      verification_client: 'codex', verification_model: 'team-model', verification_http_status: 200, verification_operation_id: 'operation-1', verification_trace_id: 'trace-1',
      completed_steps: ['model_source', 'published_model', 'api_key', 'verification'], pending_steps: []
    })
    vi.mocked(onboarding.connectOnboardingModelSource).mockResolvedValue({ session: sourceSession } as never)
    vi.mocked(onboarding.publishOnboardingModel).mockResolvedValue({ session: modelSession, published_model: { model_id: 'team-model' } } as never)
    vi.mocked(onboarding.createOnboardingAPIKey).mockResolvedValue(apiKeyResult(apiKeySession))
    vi.mocked(onboarding.verifyOnboardingClient).mockResolvedValue({
      session: completedSession,
      verification: { status: 'success', client: 'codex', api_key_id: 'key-1', model: 'team-model', http_status: 200, operation_id: 'operation-1', trace_id: 'trace-1' }
    })

    const wrapper = mountView()
    await flushPromises()
    expect(onboarding.startOnboardingSession).toHaveBeenCalledOnce()

    await wrapper.get('#onboarding-base-url').setValue('https://source.example.test/v1')
    await wrapper.get('#onboarding-upstream-model').setValue('upstream-model')
    await wrapper.get('#onboarding-secret').setValue('source-secret')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(onboarding.connectOnboardingModelSource).toHaveBeenCalledWith('onb-session-1', expect.objectContaining({ secret: 'source-secret', upstream_model: 'upstream-model' }))

    await wrapper.get('#onboarding-model-id').setValue('team-model')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(onboarding.publishOnboardingModel).toHaveBeenCalledWith('onb-session-1', expect.objectContaining({ model_id: 'team-model', upstream_model: 'upstream-model' }))

    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(onboarding.createOnboardingAPIKey).toHaveBeenCalledWith('onb-session-1', expect.objectContaining({ name: 'Engineering', concurrency_limit: 3 }))
    expect(wrapper.text()).toContain('ASTERROUTER_API_KEY')
    expect(wrapper.get('[aria-label="Application credential"]').attributes('type')).toBe('password')

    const runButton = wrapper.findAll('button').find((button) => button.text().includes('Run real verification'))
    await runButton!.trigger('click')
    await flushPromises()
    expect(onboarding.verifyOnboardingClient).toHaveBeenCalledWith('onb-session-1', expect.objectContaining({ client: 'codex', credential: 'ar_one_time_credential' }), expect.stringMatching(/^verify-/))
    expect(wrapper.text()).toContain('Verification succeeded')
		expect(wrapper.text()).toContain('Protocol verified')
		expect(wrapper.text()).toContain('CLI · v0.145.0')
		expect(wrapper.text()).toContain('Previous support line')
		expect(wrapper.text()).toContain('Official client runtime was not executed')
		expect(wrapper.text()).not.toContain('Client runtime verified')
		expect(wrapper.get('a[href="/admin/traces?q=trace-1"]').attributes('href')).toBe('/admin/traces?q=trace-1')
    expect(storageValues().join('|')).not.toContain('source-secret')
    expect(storageValues().join('|')).not.toContain('ar_one_time_credential')
    wrapper.unmount()
  })

	it('reuses a pending session idempotency key after a lost start response', async () => {
		localStorage.setItem('asterrouter_onboarding_idempotency_key', 'session-existing-idempotency')
		const wrapper = mountView()
		await flushPromises()

		expect(onboarding.startOnboardingSession).toHaveBeenCalledWith('session-existing-idempotency')
		expect(localStorage.getItem('asterrouter_onboarding_session_id')).toBe('onb-session-1')
		wrapper.unmount()
	})

  it('resumes a failed verification with the same API key and visible recovery evidence', async () => {
    const failed = session({
      status: 'failed', current_step: 'api_key', failure_stage: 'verification', failure_code: 'upstream_error', recovery_hint: 'check_route_and_provider_health',
      api_key_id: 'key-1', verification_client: 'openai_sdk', verification_model: 'team-model', verification_http_status: 500,
      verification_operation_id: 'operation-failed', verification_trace_id: 'trace-failed', verification_error_code: 'upstream_error', verification_recovery_action: 'check_route_and_provider_health',
      completed_steps: ['model_source', 'published_model', 'api_key'], pending_steps: ['verification']
    })
    localStorage.setItem('asterrouter_onboarding_session_id', failed.id)
    vi.mocked(onboarding.getOnboardingSession).mockResolvedValue(failed)
    vi.mocked(onboarding.createOnboardingAPIKey).mockResolvedValue(apiKeyResult(failed, 'ar_recovered_credential'))

    const wrapper = mountView()
    await flushPromises()

    expect(onboarding.startOnboardingSession).not.toHaveBeenCalled()
    expect(onboarding.createOnboardingAPIKey).toHaveBeenCalledWith(failed.id, { name: '', qps_limit: 0, rpm_limit: 0, tpm_limit: 0, concurrency_limit: 0, monthly_token_limit: 0, monthly_budget_micros: 0 })
		expect(onboarding.getAPIKeyClientConfig).toHaveBeenCalledWith('key-1', 'openai_sdk', 'team-model')
    expect(wrapper.text()).toContain('upstream_error')
    expect(wrapper.text()).toContain('Check route and source health.')
		expect(wrapper.get('a[href="/admin/traces?q=trace-failed"]').attributes('href')).toBe('/admin/traces?q=trace-failed')
    expect(storageValues().join('|')).not.toContain('ar_recovered_credential')
    wrapper.unmount()
  })

  it('loads a completed session without replaying the one-time credential', async () => {
    const completed = session({
      status: 'completed', current_step: 'verification', api_key_id: 'key-1',
      verification_client: 'anthropic_sdk', verification_model: 'team-model', verification_http_status: 200, verification_operation_id: 'operation-complete', verification_trace_id: 'trace-complete',
      completed_steps: ['model_source', 'published_model', 'api_key', 'verification'], pending_steps: []
    })
    localStorage.setItem('asterrouter_onboarding_session_id', completed.id)
    vi.mocked(onboarding.getOnboardingSession).mockResolvedValue(completed)
    vi.mocked(onboarding.getOnboardingAPIKey).mockResolvedValue(apiKey())

    const wrapper = mountView()
    await flushPromises()

    expect(onboarding.getOnboardingAPIKey).toHaveBeenCalledWith('key-1')
    expect(onboarding.createOnboardingAPIKey).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Verification succeeded')
    expect(wrapper.find('[aria-label="Application credential"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('New access session')
    wrapper.unmount()
  })

  it('downgrades expired evidence to pending confirmation', async () => {
    const completed = session({
      status: 'completed', current_step: 'verification', api_key_id: 'key-1', verification_client: 'codex', verification_model: 'team-model',
      verification_http_status: 200, completed_steps: ['model_source', 'published_model', 'api_key', 'verification'], pending_steps: []
    })
    localStorage.setItem('asterrouter_onboarding_session_id', completed.id)
    vi.mocked(onboarding.getOnboardingSession).mockResolvedValue(completed)
    vi.mocked(onboarding.getOnboardingAPIKey).mockResolvedValue(apiKey())
    const expiredManifest = compatibility()
    expiredManifest.records = expiredManifest.records.map((record) => ({ ...record, verification_status: 'pending_confirmation', expires_at: '2026-07-25T08:00:00Z' }))
    vi.mocked(onboarding.getCompatibilityManifest).mockResolvedValue(expiredManifest)

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('Pending confirmation')
    expect(wrapper.text()).toContain('Reconfirmation required before use')
    expect(wrapper.text()).not.toContain('Protocol verified')
    wrapper.unmount()
  })
})
