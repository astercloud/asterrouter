import { describe, expect, it } from 'vitest'
import type { RoutingPolicy, RoutingPolicyStrategy } from '@/types'
import {
  accountBatchIndex,
  assignAccountToBatch,
  buildSupplyCatalogRows,
  classifyModelFamily,
  protocolLabelKey,
  togglePreferredAccount
} from './supplyCatalog'

function strategy(overrides: Partial<RoutingPolicyStrategy> = {}): RoutingPolicyStrategy {
  return {
    preset: 'balanced', smart_optimization: true, strict_order: false, failover_before_first_byte: true,
    sticky_routing: true, sticky_ttl_seconds: 900, native_protocol_only: false,
    absolute_max_input_per_1m: 0, absolute_max_output_per_1m: 0, max_price_multiple_of_cheapest: 2,
    low_price_pool_mode: 'auto', low_price_pool_percent: 70, low_price_pool_min_candidates: 2,
    missing_price_action: 'allow', model_price_limits: [], resource_batches: [],
    preferred_provider_account_ids: [], allowed_models: [], denied_models: [], allowed_protocols: [], denied_protocols: [],
    ...overrides
  }
}

function policy(): RoutingPolicy {
  return {
    id: 'policy-1', name: 'Production', description: 'Production routes', route_group: 'stable', status: 'active', is_default: true,
    version: 2, created_at: '2026-08-16T00:00:00Z', updated_at: '2026-08-16T00:00:00Z',
    strategy: strategy({
      resource_batches: [
        { name: 'Primary', provider_account_ids: ['account-a'] },
        { name: 'Fallback', provider_account_ids: ['account-b'] }
      ],
      preferred_provider_account_ids: ['account-a']
    })
  }
}

describe('supply catalog projection', () => {
  it('classifies the supported model families from enterprise model identifiers', () => {
    expect(classifyModelFamily('claude-sonnet-4-6')).toBe('claude')
    expect(classifyModelFamily('codex-auto-review')).toBe('openai')
    expect(classifyModelFamily('gemini-2.5-pro')).toBe('gemini')
    expect(classifyModelFamily('grok-4')).toBe('grok')
    expect(classifyModelFamily('deepseek-v4')).toBe('deepseek')
    expect(classifyModelFamily('qwen3-max')).toBe('qwen')
    expect(classifyModelFamily('glm-5')).toBe('glm')
    expect(classifyModelFamily('enterprise-private-model')).toBe('other')
  })

  it('joins enterprise supply facts and derives comparable route tags', () => {
    const result = buildSupplyCatalogRows({
      models: [{ id: 'model-1', model_id: 'enterprise-chat', name: 'Enterprise Chat', modality: 'chat', default_route_group: 'stable', status: 'active' }] as never,
      providers: [{ id: 'provider-1', name: 'Vendor One', type: 'openai_compatible', status: 'active' }] as never,
      accounts: [
        { id: 'account-a', provider_id: 'provider-1', name: 'Primary account', status: 'active', schedulable: true, circuit_state: 'closed' },
        { id: 'account-b', provider_id: 'provider-1', name: 'Fallback account', status: 'active', schedulable: true, circuit_state: 'closed' }
      ] as never,
      routes: [
        { id: 'route-a', gateway_model_id: 'model-1', provider_account_id: 'account-a', route_group: 'stable', upstream_model: 'upstream-a', upstream_format: 'openai_chat', priority: 10, weight: 100, status: 'active' },
        { id: 'route-b', gateway_model_id: 'model-1', provider_account_id: 'account-b', route_group: 'stable', upstream_model: 'upstream-b', upstream_format: 'openai_chat', priority: 20, weight: 100, status: 'active' }
      ] as never,
      prices: [
        { id: 'wrong-protocol', provider_account_id: 'account-a', upstream_model: 'upstream-a', protocol: 'anthropic_messages', status: 'active', currency: 'USD', uncached_input_micros_per_1m_tokens: 1, output_micros_per_1m_tokens: 1 },
        { id: 'price-a', provider_account_id: 'account-a', upstream_model: 'upstream-a', protocol: 'openai_chat_completions', status: 'active', currency: 'USD', uncached_input_micros_per_1m_tokens: 100_000, output_micros_per_1m_tokens: 200_000 },
        { id: 'price-b', provider_account_id: 'account-b', upstream_model: 'upstream-b', protocol: 'openai_chat_completions', status: 'active', currency: 'USD', uncached_input_micros_per_1m_tokens: 300_000, output_micros_per_1m_tokens: 400_000 }
      ] as never,
      healthChecks: [
        { id: 'old', account_id: 'account-a', status: 'error', latency_ms: 500, checked_at: '2026-08-15T00:00:00Z' },
        { id: 'new', account_id: 'account-a', status: 'ok', latency_ms: 80, checked_at: '2026-08-16T00:00:00Z' },
        { id: 'b', account_id: 'account-b', status: 'warning', latency_ms: 120, checked_at: '2026-08-16T00:00:00Z' }
      ] as never,
      utilization: {
        rows: [
          { dimension: 'provider_account', id: 'account-a', demand: { requests: 100, success_rate: 0.995 } },
          { dimension: 'provider_account', id: 'account-b', demand: { requests: 50, success_rate: 0.96 } }
        ]
      } as never
    })

    expect(result).toHaveLength(2)
    expect(result[0]).toMatchObject({
      id: 'route-a', modelID: 'enterprise-chat', providerName: 'Vendor One', accountName: 'Primary account', available: true,
      price: { id: 'price-a' }, health: { id: 'new' }, tags: ['healthy', 'low_cost', 'low_latency']
    })
    expect(result[1].tags).toEqual([])
  })

  it('marks unavailable and unpriced routes instead of inventing evidence', () => {
    const [row] = buildSupplyCatalogRows({
      models: [{ id: 'model-1', model_id: 'enterprise-chat', name: 'Enterprise Chat', modality: 'chat', status: 'active' }] as never,
      providers: [{ id: 'provider-1', name: 'Vendor One', type: 'openai_compatible', status: 'active' }] as never,
      accounts: [{ id: 'account-a', provider_id: 'provider-1', name: 'Paused account', status: 'active', schedulable: false, circuit_state: 'closed' }] as never,
      routes: [{ id: 'route-a', gateway_model_id: 'model-1', provider_account_id: 'account-a', route_group: 'stable', upstream_model: 'upstream-a', upstream_format: 'openai_chat', priority: 10, weight: 100, status: 'active' }] as never,
      prices: [], healthChecks: []
    })
    expect(row).toMatchObject({ available: false, price: null, health: null, utilization: null })
    expect(row.tags).toEqual(['unavailable', 'unpriced'])
  })

  it('uses the latest currently effective compatible protocol price', () => {
    const [row] = buildSupplyCatalogRows({
      models: [{ id: 'model-1', model_id: 'enterprise-chat', name: 'Enterprise Chat', modality: 'chat', status: 'active' }] as never,
      providers: [{ id: 'provider-1', name: 'Vendor One', type: 'openai_compatible', status: 'active' }] as never,
      accounts: [{ id: 'account-a', provider_id: 'provider-1', name: 'Primary account', status: 'active', schedulable: true, circuit_state: 'closed' }] as never,
      routes: [{ id: 'route-a', gateway_model_id: 'model-1', provider_account_id: 'account-a', route_group: 'stable', upstream_model: 'upstream-a', upstream_format: 'openai_chat', priority: 10, weight: 100, status: 'active' }] as never,
      prices: [
        { id: 'wrong-protocol', provider_account_id: 'account-a', upstream_model: 'upstream-a', protocol: 'anthropic_messages', status: 'active', currency: 'USD', effective_from: '2026-08-01T00:00:00Z' },
        { id: 'expired', provider_account_id: 'account-a', upstream_model: 'upstream-a', protocol: 'openai_chat_completions', status: 'active', currency: 'USD', effective_from: '2026-08-01T00:00:00Z', expires_at: '2026-08-10T00:00:00Z' },
        { id: 'older', provider_account_id: 'account-a', upstream_model: 'upstream-a', protocol: 'openai_chat_completions', status: 'active', currency: 'usd', effective_from: '2026-08-11T00:00:00Z' },
        { id: 'current', provider_account_id: 'account-a', upstream_model: 'upstream-a', protocol: 'openai_chat_completions', status: 'active', currency: 'USD', effective_from: '2026-08-15T00:00:00Z' },
        { id: 'future', provider_account_id: 'account-a', upstream_model: 'upstream-a', protocol: 'openai_chat_completions', status: 'active', currency: 'USD', effective_from: '2026-08-20T00:00:00Z' }
      ] as never,
      healthChecks: [],
      now: new Date('2026-08-17T00:00:00Z')
    })

    expect(row.price?.id).toBe('current')
    expect(protocolLabelKey('openai_chat')).toBe('openai_chat_completions')
    expect(protocolLabelKey('aster_jobs')).toBe('aster_jobs')
  })

  it('does not project an image price onto an audio native-media route', () => {
    const [row] = buildSupplyCatalogRows({
      models: [{ id: 'model-audio', model_id: 'enterprise-audio', name: 'Enterprise Audio', modality: 'audio', status: 'active' }] as never,
      providers: [{ id: 'provider-1', name: 'Provider', type: 'openai_compatible', status: 'active' }] as never,
      accounts: [{ id: 'account-a', provider_id: 'provider-1', name: 'Audio account', status: 'active', schedulable: true, circuit_state: 'closed' }] as never,
      routes: [{ id: 'route-audio', gateway_model_id: 'model-audio', provider_account_id: 'account-a', route_group: 'stable', upstream_model: 'omni-media', upstream_format: 'native_media', priority: 10, weight: 100, status: 'active' }] as never,
      prices: [
        { id: 'image-price', provider_account_id: 'account-a', upstream_model: 'omni-media', protocol: 'openai_images_generations', status: 'active', currency: 'USD', effective_from: '2026-08-15T00:00:00Z' },
        { id: 'speech-price', provider_account_id: 'account-a', upstream_model: 'omni-media', protocol: 'openai_audio_speech', status: 'active', currency: 'USD', effective_from: '2026-08-16T00:00:00Z' }
      ] as never,
      healthChecks: [],
      now: new Date('2026-08-17T00:00:00Z')
    })

    expect(row.price?.id).toBe('speech-price')
  })
})

describe('supply catalog policy actions', () => {
  it('toggles preference without mutating the current policy', () => {
    const current = policy()
    const added = togglePreferredAccount(current, 'account-b')
    const removed = togglePreferredAccount(current, 'account-a')

    expect(added.strategy.preferred_provider_account_ids).toEqual(['account-a', 'account-b'])
    expect(removed.strategy.preferred_provider_account_ids).toEqual([])
    expect(current.strategy.preferred_provider_account_ids).toEqual(['account-a'])
    expect(added.strategy.resource_batches).toEqual(current.strategy.resource_batches)
  })

  it('moves an account across batches and removes the empty source batch', () => {
    const current = policy()
    const request = assignAccountToBatch(current, 'account-b', 0, 'Primary routes')

    expect(request.strategy.resource_batches).toEqual([
      { name: 'Primary', provider_account_ids: ['account-a', 'account-b'] }
    ])
    expect(accountBatchIndex(current, 'account-b')).toBe(1)
    expect(current.strategy.resource_batches).toHaveLength(2)
  })

  it('creates the first explicit batch and can restore dynamic candidates', () => {
    const dynamic = { ...policy(), strategy: strategy() }
    const created = assignAccountToBatch(dynamic, 'account-a', 'new', 'Primary routes')
    expect(created.strategy.resource_batches).toEqual([{ name: 'Primary routes', provider_account_ids: ['account-a'] }])

    const explicit = { ...dynamic, strategy: { ...dynamic.strategy, resource_batches: created.strategy.resource_batches } }
    const removed = assignAccountToBatch(explicit, 'account-a', null, 'Primary routes')
    expect(removed.strategy.resource_batches).toEqual([])
  })
})
