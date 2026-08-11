import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as control from '@/api/control'
import type { APIKeyRecord, PortalWorkspace } from '@/types'
import AccountAccessConfig from './AccountAccessConfig.vue'

vi.mock('@/api/control', () => ({
  createPortalAPIKey: vi.fn(),
  getPortalWorkspace: vi.fn(),
  rotatePortalAPIKey: vi.fn()
}))

const key = {
  id: 'key-1', name: 'Developer key', fingerprint: 'fingerprint', prefix: 'sk-test', status: 'active',
	key_type: 'workspace', owner_user_id: '', application_id: 'app_default', gateway_principal_id: '',
	principal_type: 'workspace', principal_reference: 'key-1',
  policy_id: '', scopes: ['gateway:invoke'], model_allowlist: ['published-model'], allowed_modalities: ['text'],
  allowed_operations: ['chat_completion', 'count_tokens'], qps_limit: 0, rpm_limit: 0, tpm_limit: 0,
  concurrency_limit: 0, monthly_token_limit: 0, monthly_budget_micros: 0, monthly_image_limit: 0,
  monthly_video_seconds_limit: 0, monthly_audio_seconds_limit: 0, allowed_cidrs: [], lane_policy: 'direct_only',
  artifact_policy: 'proxy_only', rotation_family_id: 'family-1', replaces_key_id: '', replaced_by_key_id: '',
  created_at: '2026-07-26T00:00:00Z', updated_at: '2026-07-26T00:00:00Z'
} satisfies APIKeyRecord

const workspace = {
  api_keys: [key],
  usage: {
    total_requests: 0, error_requests: 0, total_tokens: 0, total_output_images: 0,
    total_video_milliseconds: 0, total_audio_milliseconds: 0, total_usage_cost_micros: 0,
    priced_requests: 0, unpriced_requests: 0, disputed_requests: 0, cost_available: false,
    avg_latency_ms: 0, by_model: [], recent: []
  },
  recent_traces: [], alerts: [], models: ['published-model'], gateway_path: '/v1',
  can_manage_keys: true, principal: 'developer'
} as PortalWorkspace

describe('AccountAccessConfig', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('en-US')
    const pinia = createPinia()
    setActivePinia(pinia)
    vi.mocked(control.getPortalWorkspace).mockResolvedValue(structuredClone(workspace))
  })

  it('generates configurations from the protocols exposed by the gateway', async () => {
    const wrapper = mount(AccountAccessConfig, { global: { plugins: [i18n, createPinia()] } })
    await flushPromises()

    expect(wrapper.get('.generated-config').text()).toContain('wire_api = "responses"')
    expect(wrapper.get('.protocol-supported').text()).toContain('available Responses API')
    expect(wrapper.text()).not.toContain('Confirm that protocol support is enabled')

    const target = wrapper.findAll('.config-tabs button').find((button) => button.text() === 'Claude Code')
    expect(target).toBeDefined()
    await target!.trigger('click')

    expect(wrapper.get('.generated-config').text()).toContain('ANTHROPIC_BASE_URL')
    expect(wrapper.get('.generated-config').text()).toContain('published-model')
    expect(wrapper.get('.protocol-supported').text()).toContain('exact token-counting endpoints')
  })
})
