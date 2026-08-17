import type {
  GatewayModel,
  ModelRoute,
  ProcurementPrice,
  ProviderAccount,
  ProviderAccountHealthCheck,
  ProviderConnection,
  RoutingPolicy,
  RoutingPolicyRequest,
  SupplyUtilizationReport,
  SupplyUtilizationRow
} from '@/types'

export type SupplyCatalogTag = 'healthy' | 'low_cost' | 'low_latency' | 'unpriced' | 'unavailable'
export type ModelFamily = 'claude' | 'openai' | 'gemini' | 'grok' | 'deepseek' | 'qwen' | 'glm' | 'other'

export interface SupplyCatalogRow {
  id: string
  gatewayModelID: string
  modelID: string
  modelName: string
  modelFamily: ModelFamily
  modality: string
  routeGroup: string
  providerID: string
  providerName: string
  providerType: string
  accountID: string
  accountName: string
  upstreamModel: string
  upstreamFormat: string
  routeStatus: string
  accountStatus: string
  schedulable: boolean
  circuitState: string
  priority: number
  weight: number
  price: ProcurementPrice | null
  health: ProviderAccountHealthCheck | null
  utilization: SupplyUtilizationRow | null
  available: boolean
  tags: SupplyCatalogTag[]
}

interface SupplyCatalogSources {
  models: GatewayModel[]
  routes: ModelRoute[]
  providers: ProviderConnection[]
  accounts: ProviderAccount[]
  prices: ProcurementPrice[]
  healthChecks: ProviderAccountHealthCheck[]
  utilization?: SupplyUtilizationReport | null
  now?: Date
}

export function protocolLabelKey(upstreamFormat: string): string {
  const aliases: Record<string, string> = {
    openai_chat: 'openai_chat_completions',
    anthropic: 'anthropic_messages',
    gemini: 'gemini_generate_content'
  }
  return aliases[upstreamFormat] || upstreamFormat
}

export function classifyModelFamily(...values: Array<string | undefined>): ModelFamily {
  const value = values.filter(Boolean).join(' ').toLowerCase()
  if (/claude|anthropic/.test(value)) return 'claude'
  if (/gemini/.test(value)) return 'gemini'
  if (/grok/.test(value)) return 'grok'
  if (/deepseek/.test(value)) return 'deepseek'
  if (/qwen|qwq/.test(value)) return 'qwen'
  if (/chatglm|\bglm[-_ ]?\d/.test(value)) return 'glm'
  if (/openai|codex|\bgpt[-_ ]|\bo[134][-_ ]/.test(value)) return 'openai'
  return 'other'
}

const routeProtocolAliases: Record<string, string[]> = {
  openai_chat: ['openai_chat_completions'],
  openai_responses: ['openai_responses'],
  openai_embeddings: ['openai_embeddings'],
  anthropic: ['anthropic_messages'],
  anthropic_messages: ['anthropic_messages'],
  gemini: ['gemini_generate_content'],
  gemini_generate_content: ['gemini_generate_content'],
  native_media: ['openai_images_generations', 'openai_media_generations', 'openai_audio_transcriptions', 'openai_audio_translations', 'openai_audio_speech']
}

function routePriceProtocols(route: ModelRoute, modality: string): string[] {
  if (route.upstream_format !== 'native_media') return routeProtocolAliases[route.upstream_format] || [route.upstream_format]
  if (modality === 'image') return ['openai_images_generations', 'openai_media_generations']
  if (modality === 'video') return ['openai_media_generations']
  if (modality === 'audio') return ['openai_audio_transcriptions', 'openai_audio_translations', 'openai_audio_speech']
  return routeProtocolAliases.native_media
}

function priceProtocolRank(route: ModelRoute, modality: string, price: ProcurementPrice): number {
  const aliases = routePriceProtocols(route, modality)
  return aliases.indexOf(price.protocol)
}

function isPriceEffective(price: ProcurementPrice, now: Date): boolean {
  const effectiveFrom = Date.parse(price.effective_from)
  if (Number.isFinite(effectiveFrom) && effectiveFrom > now.getTime()) return false
  const expiresAt = Date.parse(price.expires_at || '')
  return !Number.isFinite(expiresAt) || expiresAt > now.getTime()
}

function priceEffectiveFrom(price: ProcurementPrice): number {
  const effectiveFrom = Date.parse(price.effective_from)
  return Number.isFinite(effectiveFrom) ? effectiveFrom : 0
}

function selectPrice(route: ModelRoute, modality: string, prices: ProcurementPrice[], now: Date): ProcurementPrice | null {
  return prices
    .filter((price) =>
      price.status === 'active' &&
      price.currency.toUpperCase() === 'USD' &&
      price.provider_account_id === route.provider_account_id &&
      price.upstream_model === route.upstream_model &&
      priceProtocolRank(route, modality, price) >= 0 &&
      isPriceEffective(price, now)
    )
    .sort((left, right) =>
      priceProtocolRank(route, modality, left) - priceProtocolRank(route, modality, right) ||
      priceEffectiveFrom(right) - priceEffectiveFrom(left)
    )[0] || null
}

function latestHealthByAccount(checks: ProviderAccountHealthCheck[]): Map<string, ProviderAccountHealthCheck> {
  const result = new Map<string, ProviderAccountHealthCheck>()
  for (const check of checks) {
    const previous = result.get(check.account_id)
    if (!previous || Date.parse(check.checked_at) > Date.parse(previous.checked_at)) result.set(check.account_id, check)
  }
  return result
}

function utilizationByAccount(report?: SupplyUtilizationReport | null): Map<string, SupplyUtilizationRow> {
  return new Map((report?.rows || [])
    .filter((row) => row.dimension === 'provider_account')
    .map((row) => [row.id, row]))
}

function hasHealthyEvidence(row: SupplyCatalogRow): boolean {
  return row.health?.status === 'ok' || (row.utilization?.demand.requests || 0) > 0 && (row.utilization?.demand.success_rate || 0) >= 0.99
}

export function buildSupplyCatalogRows(sources: SupplyCatalogSources): SupplyCatalogRow[] {
  const models = new Map(sources.models.map((model) => [model.id, model]))
  const providers = new Map(sources.providers.map((provider) => [provider.id, provider]))
  const accounts = new Map(sources.accounts.map((account) => [account.id, account]))
  const health = latestHealthByAccount(sources.healthChecks)
  const utilization = utilizationByAccount(sources.utilization)
  const now = sources.now || new Date()

  const rows = sources.routes.flatMap((route): SupplyCatalogRow[] => {
    const model = models.get(route.gateway_model_id)
    const account = accounts.get(route.provider_account_id)
    const provider = account ? providers.get(account.provider_id) : undefined
    if (!model || !account || !provider) return []
    const row: SupplyCatalogRow = {
      id: route.id,
      gatewayModelID: model.id,
      modelID: model.model_id,
      modelName: model.name,
      modelFamily: classifyModelFamily(model.model_id, model.name, route.upstream_model),
      modality: model.modality,
      routeGroup: route.route_group,
      providerID: provider.id,
      providerName: provider.name,
      providerType: provider.type,
      accountID: account.id,
      accountName: account.name,
      upstreamModel: route.upstream_model,
      upstreamFormat: route.upstream_format,
      routeStatus: route.status,
      accountStatus: account.status,
      schedulable: account.schedulable,
      circuitState: account.circuit_state,
      priority: route.priority,
      weight: route.weight,
      price: selectPrice(route, model.modality, sources.prices, now),
      health: health.get(account.id) || null,
      utilization: utilization.get(account.id) || null,
      available: route.status === 'active' && model.status === 'active' && provider.status === 'active' && account.status === 'active' && account.schedulable && account.circuit_state !== 'open',
      tags: []
    }
    return [row]
  })

  const cheapestByModel = new Map<string, number>()
  const fastestByModel = new Map<string, number>()
  for (const row of rows) {
    const input = row.price?.uncached_input_micros_per_1m_tokens
    if (input != null) cheapestByModel.set(row.modelID, Math.min(cheapestByModel.get(row.modelID) ?? input, input))
    const latency = row.health?.latency_ms
    if (latency != null && latency > 0) fastestByModel.set(row.modelID, Math.min(fastestByModel.get(row.modelID) ?? latency, latency))
  }

  for (const row of rows) {
    if (!row.available) row.tags.push('unavailable')
    if (!row.price) row.tags.push('unpriced')
    if (hasHealthyEvidence(row)) row.tags.push('healthy')
    if (row.price && row.price.uncached_input_micros_per_1m_tokens === cheapestByModel.get(row.modelID)) row.tags.push('low_cost')
    if (row.health?.latency_ms && row.health.latency_ms === fastestByModel.get(row.modelID)) row.tags.push('low_latency')
  }
  return rows
}

export function routingPolicyRequest(policy: RoutingPolicy): RoutingPolicyRequest {
  return {
    name: policy.name,
    description: policy.description,
    route_group: policy.route_group,
    status: policy.status,
    is_default: policy.is_default,
    strategy: JSON.parse(JSON.stringify(policy.strategy)) as RoutingPolicy['strategy']
  }
}

export function togglePreferredAccount(policy: RoutingPolicy, accountID: string): RoutingPolicyRequest {
  const request = routingPolicyRequest(policy)
  const preferred = new Set(request.strategy.preferred_provider_account_ids)
  if (preferred.has(accountID)) preferred.delete(accountID)
  else preferred.add(accountID)
  request.strategy.preferred_provider_account_ids = Array.from(preferred)
  return request
}

export function accountBatchIndex(policy: RoutingPolicy, accountID: string): number | null {
  const index = policy.strategy.resource_batches.findIndex((batch) => batch.provider_account_ids.includes(accountID))
  return index < 0 ? null : index
}

export function assignAccountToBatch(
  policy: RoutingPolicy,
  accountID: string,
  target: number | 'new' | null,
  defaultBatchName: string
): RoutingPolicyRequest {
  const request = routingPolicyRequest(policy)
  const batches = request.strategy.resource_batches.map((batch) => ({
    name: batch.name,
    provider_account_ids: batch.provider_account_ids.filter((id) => id !== accountID)
  }))
  if (typeof target === 'number' && batches[target]) batches[target].provider_account_ids.push(accountID)
  if (target === 'new') batches.push({ name: defaultBatchName, provider_account_ids: [accountID] })
  request.strategy.resource_batches = batches.filter((batch) => batch.provider_account_ids.length > 0)
  return request
}
