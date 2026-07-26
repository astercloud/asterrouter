import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  connectOnboardingModelSource,
  createOnboardingAPIKey,
  getAPIKeyClientConfig,
  getCompatibilityManifest,
  getOnboardingAPIKey,
  getOnboardingSession,
  publishOnboardingModel,
  startOnboardingSession,
  verifyAPIKeyClient,
  verifyOnboardingClient
} from './onboarding'

const client = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: client }))

describe('onboarding API contracts', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    client.get.mockResolvedValue({ data: {} })
    client.post.mockResolvedValue({ data: {} })
  })

  it('uses session endpoints and preserves idempotency headers', async () => {
    await getCompatibilityManifest()
    expect(client.get).toHaveBeenLastCalledWith('/onboarding/compatibility-records')

    await startOnboardingSession('session-idempotency')
    expect(client.post).toHaveBeenLastCalledWith('/onboarding/sessions', null, { headers: { 'Idempotency-Key': 'session-idempotency' } })

    await getOnboardingSession('session / 1')
    expect(client.get).toHaveBeenLastCalledWith('/onboarding/sessions/session%20%2F%201')

    const source = { provider_name: 'Source', provider_type: 'openai_compatible', base_url: 'https://example.test/v1', account_name: 'Account', auth_type: 'api_key', secret: 'secret', adapter_config: {}, upstream_model: 'upstream', concurrency: 3, rpm_limit: 0, tpm_limit: 0 } as const
    await connectOnboardingModelSource('session / 1', source)
    expect(client.post).toHaveBeenLastCalledWith('/onboarding/sessions/session%20%2F%201/model-source', source)

    const published = { model_id: 'public', name: 'Public', description: '', modality: 'chat', route_group: '', upstream_model: 'upstream', upstream_format: '' } as const
    await publishOnboardingModel('session / 1', published)
    expect(client.post).toHaveBeenLastCalledWith('/onboarding/sessions/session%20%2F%201/published-model', published)
  })

  it('uses API key, configuration, and verification endpoints', async () => {
    const apiKey = { name: 'Engineering', qps_limit: 0, rpm_limit: 0, tpm_limit: 0, concurrency_limit: 3, monthly_token_limit: 10000, monthly_budget_micros: 0 }
    await createOnboardingAPIKey('session-1', apiKey)
    expect(client.post).toHaveBeenLastCalledWith('/onboarding/sessions/session-1/api-key', apiKey)

    await getOnboardingAPIKey('key / 1')
    expect(client.get).toHaveBeenLastCalledWith('/admin/api-keys/key%20%2F%201')

    await getAPIKeyClientConfig('key / 1', 'codex', 'public-model')
    expect(client.get).toHaveBeenLastCalledWith('/admin/api-keys/key%20%2F%201/client-config', { params: { client: 'codex', model: 'public-model' } })

    const verification = { client: 'codex', model: 'public-model', credential: 'credential' } as const
    await verifyOnboardingClient('session / 1', verification, 'verify-idempotency')
    expect(client.post).toHaveBeenLastCalledWith('/onboarding/sessions/session%20%2F%201/verification', verification, { headers: { 'Idempotency-Key': 'verify-idempotency' } })

    await verifyAPIKeyClient('key / 1', verification, 'standalone-idempotency')
    expect(client.post).toHaveBeenLastCalledWith('/admin/api-keys/key%20%2F%201/client-verifications', verification, { headers: { 'Idempotency-Key': 'standalone-idempotency' } })
  })
})
