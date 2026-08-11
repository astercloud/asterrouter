import { apiClient } from './client'
import {
  listOrEmpty,
  normalizeAIJobAdminDetail,
  normalizeAPIKeyCreateResponse,
  normalizeAPIKeyRecord,
  normalizeArtifactAdminDetail,
  normalizeDashboard,
  normalizeEffectivePricingDecision,
  normalizeEffectivePricingDecisionEvaluation,
  normalizeEffectivePricingReport,
  normalizeGatewayPolicyExplanation,
  normalizeProviderBillingSource,
  normalizeProviderBillingSourceInspection,
  normalizeProviderBillingSyncResult,
  normalizeUsageReport,
  stringListOrEmpty,
  type AIJobAdminDetailPayload,
  type APIKeyCreateResponsePayload,
  type APIKeyRecordPayload,
  type ArtifactAdminDetailPayload,
  type DashboardPayload,
  type EffectivePricingDecisionEvaluationPayload,
  type EffectivePricingDecisionPayload,
  type EffectivePricingReportPayload,
  type ProviderBillingSourceInspectionPayload,
  type ProviderBillingSourcePayload,
  type ProviderBillingSyncResultPayload,
  type UsageReportPayload
} from './normalizers'
import type {
  Application,
  ApplicationRequest,
  APIKeyCreateRequest,
  APIKeyCreateResponse,
  APIKeyRecord,
  APIKeyUpdateRequest,
  AIAttemptReconcileScheduleResult,
  AIJobAdminActionResult,
  AIJobAdminDetail,
  AIJobAdminRecord,
  AIJobListQuery,
  AIJobRuntimeStatus,
  AIJobSummary,
  ArtifactAdminDetail,
  ArtifactAdminRecord,
  ArtifactDeliveryRetryResult,
  ArtifactListQuery,
  ArtifactRuntime,
  ArtifactSummary,
  AlertEvent,
  AlertSummary,
  AuditLog,
  AuditLogSummary,
  CacheProbeRequest,
  CapacityRecommendationReport,
  CostAllocationReport,
  Department,
  DepartmentRequest,
  Dashboard,
  EffectivePricingDecision,
  EffectivePricingDecisionEvaluation,
  EffectivePricingDecisionEvaluationRequest,
  EffectivePricingPolicy,
  EffectivePricingPolicyRequest,
  EffectivePricingReport,
  ExportJob,
  ExportJobKind,
  GatewayPolicyExplanation,
	GatewayModel,
	GatewayModelRequest,
	GatewaySimulation,
  GatewayTrace,
  GatewayTraceSummary,
  GovernancePolicy,
  GovernancePolicyRequest,
	PricingDraftUpdateRequest,
	PricingEvaluation,
	PricingPublishRequest,
	PricingRule,
	PricingRuleAnalysis,
	PricingRuleCreateRequest,
	PricingRuleDetail,
	PricingRuleVersion,
	PricingSimulationRequest,
	PricingSimulationResult,
	PricingValidationResult,
	OrganizationGroup,
	OrganizationGroupRequest,
	ModelRoute,
	ModelRouteBulkCreateRequest,
	ModelRouteBulkCreateResult,
	ModelRouteRequest,
  PortalWorkspace,
  RecordListQuery,
  RoleBinding,
  RoleBindingRequest,
  ProviderAccount,
  ProviderAccountHealthCheck,
  ProviderAccountModelDiscovery,
  ProviderAccountModelInventory,
  ProviderAccountModelSyncRequest,
  ProviderAccountModelSyncResult,
  ProviderAccountRequest,
  ProviderBillingLine,
  ProviderBillingLineRequest,
  ProviderBillingSource,
  ProviderBillingSourceEvidence,
  ProviderBillingSourceInspection,
  ProviderBillingSourceRequest,
  ProviderBillingSyncResult,
  ProviderCacheCapability,
  ProviderCacheCapabilityRequest,
  ProviderCacheProbeRun,
  ProviderHealthCheck,
  ProviderConnection,
  ProviderRequest,
  ProcurementPrice,
  ProcurementPriceRequest,
  RoutingGroup,
  RoutingGroupRequest,
  RoutingPolicy,
  RoutingPolicyRequest,
  SupplyUtilizationReport,
  UsageReport,
  WorkspaceUser,
  WorkspaceUserRequest
} from '@/types'

type ProviderAccountPayload = Omit<ProviderAccount, 'models' | 'group_ids' | 'temp_unschedulable_rules'> & {
  models?: string[] | null
  group_ids?: string[] | null
  temp_unschedulable_rules?: ProviderAccount['temp_unschedulable_rules'] | null
}
type ProviderAccountHealthCheckPayload = Omit<ProviderAccountHealthCheck, 'models'> & { models?: string[] | null }
type OrganizationGroupPayload = Omit<OrganizationGroup, 'member_ids'> & { member_ids?: string[] | null }
type GovernancePolicyPayload = Omit<GovernancePolicy, 'model_allowlist' | 'model_denylist'> & {
  model_allowlist?: string[] | null
  model_denylist?: string[] | null
}
type GatewaySimulationPayload = Omit<GatewaySimulation, 'candidates'> & { candidates?: GatewaySimulation['candidates'] | null }
type CostAllocationReportPayload = Omit<CostAllocationReport, 'rows'> & { rows?: CostAllocationReport['rows'] | null }
type ProviderBillingSourceEvidencePayload = Omit<ProviderBillingSourceEvidence, 'source' | 'runs' | 'balances' | 'aggregates'> & {
  source: ProviderBillingSourcePayload
  runs?: ProviderBillingSourceEvidence['runs'] | null
  balances?: ProviderBillingSourceEvidence['balances'] | null
  aggregates?: ProviderBillingSourceEvidence['aggregates'] | null
}

function normalizeProviderAccount(account: ProviderAccountPayload): ProviderAccount {
  return {
    ...account,
    models: stringListOrEmpty(account.models),
    auto_enable_new_models: account.auto_enable_new_models === true,
    group_ids: stringListOrEmpty(account.group_ids),
    temp_unschedulable_rules: listOrEmpty(account.temp_unschedulable_rules)
  }
}

function normalizeProviderAccountHealthCheck(check: ProviderAccountHealthCheckPayload): ProviderAccountHealthCheck {
  return {
    ...check,
    models: stringListOrEmpty(check.models)
  }
}

function normalizeOrganizationGroup(group: OrganizationGroupPayload): OrganizationGroup {
  return { ...group, member_ids: stringListOrEmpty(group.member_ids) }
}

function normalizeGovernancePolicy(policy: GovernancePolicyPayload): GovernancePolicy {
  return {
    ...policy,
    model_allowlist: stringListOrEmpty(policy.model_allowlist),
    model_denylist: stringListOrEmpty(policy.model_denylist)
  }
}

export async function getDashboard(): Promise<Dashboard> {
  const response = await apiClient.get<DashboardPayload>('/console/dashboard')
  return normalizeDashboard(response.data)
}

export async function getProviders(): Promise<ProviderConnection[]> {
  const response = await apiClient.get<ProviderConnection[] | null>('/console/providers')
  return listOrEmpty(response.data)
}

export async function getProviderHealthChecks(): Promise<ProviderHealthCheck[]> {
  const response = await apiClient.get<ProviderHealthCheck[] | null>('/console/provider-health-checks')
  return listOrEmpty(response.data)
}

export async function createProvider(payload: ProviderRequest): Promise<ProviderConnection> {
  const response = await apiClient.post<ProviderConnection>('/console/providers', payload)
  return response.data
}

export async function updateProvider(id: string, payload: ProviderRequest): Promise<ProviderConnection> {
  const response = await apiClient.put<ProviderConnection>(`/console/providers/${id}`, payload)
  return response.data
}

export async function checkProvider(id: string): Promise<ProviderHealthCheck> {
  const response = await apiClient.post<ProviderHealthCheck>(`/console/providers/${id}/check`)
  return response.data
}

export async function getDepartments(): Promise<Department[]> {
  const response = await apiClient.get<Department[] | null>('/console/departments')
  return listOrEmpty(response.data)
}

export async function getApplications(): Promise<Application[]> {
  const response = await apiClient.get<Application[] | null>('/applications')
  return listOrEmpty(response.data)
}

export async function createApplication(payload: ApplicationRequest): Promise<Application> {
  const response = await apiClient.post<Application>('/applications', payload)
  return response.data
}

export async function updateApplication(id: string, payload: ApplicationRequest): Promise<Application> {
  const response = await apiClient.put<Application>(`/applications/${id}`, payload)
  return response.data
}

export async function getOrganizationGroups(): Promise<OrganizationGroup[]> {
		const response = await apiClient.get<OrganizationGroupPayload[] | null>('/console/organization-groups')
		return listOrEmpty(response.data).map(normalizeOrganizationGroup)
}

export async function createOrganizationGroup(payload: OrganizationGroupRequest): Promise<OrganizationGroup> {
	return normalizeOrganizationGroup((await apiClient.post<OrganizationGroupPayload>('/console/organization-groups', payload)).data)
}

export async function updateOrganizationGroup(id: string, payload: OrganizationGroupRequest): Promise<OrganizationGroup> {
	return normalizeOrganizationGroup((await apiClient.put<OrganizationGroupPayload>(`/console/organization-groups/${id}`, payload)).data)
}

export async function deleteOrganizationGroup(id: string): Promise<void> {
	await apiClient.delete(`/console/organization-groups/${id}`)
}

export async function createDepartment(payload: DepartmentRequest): Promise<Department> {
  const response = await apiClient.post<Department>('/console/departments', payload)
  return response.data
}

export async function updateDepartment(id: string, payload: DepartmentRequest): Promise<Department> {
  const response = await apiClient.put<Department>(`/console/departments/${id}`, payload)
  return response.data
}

export async function getGovernancePolicies(): Promise<GovernancePolicy[]> {
  const response = await apiClient.get<GovernancePolicyPayload[] | null>('/console/policies')
  return listOrEmpty(response.data).map(normalizeGovernancePolicy)
}

export async function createGovernancePolicy(payload: GovernancePolicyRequest): Promise<GovernancePolicy> {
  const response = await apiClient.post<GovernancePolicyPayload>('/console/policies', payload)
  return normalizeGovernancePolicy(response.data)
}

export async function updateGovernancePolicy(id: string, payload: GovernancePolicyRequest): Promise<GovernancePolicy> {
  const response = await apiClient.put<GovernancePolicyPayload>(`/console/policies/${id}`, payload)
  return normalizeGovernancePolicy(response.data)
}

export async function getWorkspaceUsers(): Promise<WorkspaceUser[]> {
  const response = await apiClient.get<WorkspaceUser[] | null>('/console/users')
  return listOrEmpty(response.data)
}

export async function createWorkspaceUser(payload: WorkspaceUserRequest): Promise<WorkspaceUser> {
  const response = await apiClient.post<WorkspaceUser>('/console/users', payload)
  return response.data
}

export async function updateWorkspaceUser(id: string, payload: WorkspaceUserRequest): Promise<WorkspaceUser> {
  const response = await apiClient.put<WorkspaceUser>(`/console/users/${id}`, payload)
  return response.data
}

export async function getRoleBindings(): Promise<RoleBinding[]> {
  const response = await apiClient.get<RoleBinding[] | null>('/console/role-bindings')
  return listOrEmpty(response.data)
}

export async function createRoleBinding(payload: RoleBindingRequest): Promise<RoleBinding> {
  const response = await apiClient.post<RoleBinding>('/console/role-bindings', payload)
  return response.data
}

export async function deleteRoleBinding(id: string): Promise<void> {
  await apiClient.delete(`/console/role-bindings/${id}`)
}

export async function getRoutingGroups(): Promise<RoutingGroup[]> {
  const response = await apiClient.get<RoutingGroup[] | null>('/console/routing-groups')
  return listOrEmpty(response.data)
}

export async function createRoutingGroup(payload: RoutingGroupRequest): Promise<RoutingGroup> {
  const response = await apiClient.post<RoutingGroup>('/console/routing-groups', payload)
  return response.data
}

export async function updateRoutingGroup(id: string, payload: RoutingGroupRequest): Promise<RoutingGroup> {
  const response = await apiClient.put<RoutingGroup>(`/console/routing-groups/${id}`, payload)
  return response.data
}

export async function getRoutingPolicies(): Promise<RoutingPolicy[]> {
  const response = await apiClient.get<RoutingPolicy[] | null>('/console/routing-policies')
  return listOrEmpty(response.data)
}

export async function createRoutingPolicy(payload: RoutingPolicyRequest): Promise<RoutingPolicy> {
  const response = await apiClient.post<RoutingPolicy>('/console/routing-policies', payload)
  return response.data
}

export async function updateRoutingPolicy(id: string, payload: RoutingPolicyRequest): Promise<RoutingPolicy> {
  const response = await apiClient.put<RoutingPolicy>(`/console/routing-policies/${id}`, payload)
  return response.data
}

export async function getProviderAccounts(): Promise<ProviderAccount[]> {
  const response = await apiClient.get<ProviderAccountPayload[] | null>('/console/provider-accounts')
  return listOrEmpty(response.data).map(normalizeProviderAccount)
}

export async function getProviderAccountHealthChecks(): Promise<ProviderAccountHealthCheck[]> {
  const response = await apiClient.get<ProviderAccountHealthCheckPayload[] | null>('/console/provider-account-health-checks')
  return listOrEmpty(response.data).map(normalizeProviderAccountHealthCheck)
}

export async function createProviderAccount(payload: ProviderAccountRequest): Promise<ProviderAccount> {
  const response = await apiClient.post<ProviderAccountPayload>('/console/provider-accounts', payload)
  return normalizeProviderAccount(response.data)
}

export async function updateProviderAccount(id: string, payload: ProviderAccountRequest): Promise<ProviderAccount> {
  const response = await apiClient.put<ProviderAccountPayload>(`/console/provider-accounts/${id}`, payload)
  return normalizeProviderAccount(response.data)
}

export async function deleteProviderAccount(id: string): Promise<void> {
  await apiClient.delete(`/console/provider-accounts/${id}`)
}

export async function checkProviderAccount(id: string): Promise<ProviderAccountHealthCheck> {
  const response = await apiClient.post<ProviderAccountHealthCheckPayload>(`/console/provider-accounts/${id}/check`)
  return normalizeProviderAccountHealthCheck(response.data)
}

export async function getProviderAccountModelInventory(id: string): Promise<ProviderAccountModelInventory> {
  const response = await apiClient.get<ProviderAccountModelInventory>(`/console/provider-accounts/${id}/models`)
  return { ...response.data, models: listOrEmpty(response.data.models) }
}

export async function discoverProviderAccountModels(id: string): Promise<ProviderAccountModelDiscovery> {
  const response = await apiClient.post<ProviderAccountModelDiscovery>(`/console/provider-accounts/${id}/models/discover`)
  return {
    ...response.data,
    models: listOrEmpty(response.data.models),
    added_models: stringListOrEmpty(response.data.added_models),
    missing_models: stringListOrEmpty(response.data.missing_models),
    unchanged_models: stringListOrEmpty(response.data.unchanged_models),
    affected_route_ids: stringListOrEmpty(response.data.affected_route_ids)
  }
}

export async function syncProviderAccountModels(id: string, payload: ProviderAccountModelSyncRequest): Promise<ProviderAccountModelSyncResult> {
  const response = await apiClient.post<ProviderAccountModelSyncResult>(`/console/provider-accounts/${id}/models/sync`, payload)
  return {
    ...response.data,
    account: normalizeProviderAccount(response.data.account),
    inventory: {
      ...response.data.inventory,
      models: listOrEmpty(response.data.inventory.models)
    },
    discovery: {
      ...response.data.discovery,
      models: listOrEmpty(response.data.discovery.models),
      added_models: stringListOrEmpty(response.data.discovery.added_models),
      missing_models: stringListOrEmpty(response.data.discovery.missing_models),
      unchanged_models: stringListOrEmpty(response.data.discovery.unchanged_models),
      affected_route_ids: stringListOrEmpty(response.data.discovery.affected_route_ids)
    }
  }
}

export async function clearProviderAccountCooldown(id: string): Promise<ProviderAccount> {
  const response = await apiClient.post<ProviderAccountPayload>(`/console/provider-accounts/${id}/clear-cooldown`)
  return normalizeProviderAccount(response.data)
}

export async function getGatewayModels(): Promise<GatewayModel[]> {
  const response = await apiClient.get<GatewayModel[] | null>('/console/gateway-models')
  return listOrEmpty(response.data)
}

export async function createGatewayModel(payload: GatewayModelRequest): Promise<GatewayModel> {
  const response = await apiClient.post<GatewayModel>('/console/gateway-models', payload)
  return response.data
}

export async function updateGatewayModel(id: string, payload: GatewayModelRequest): Promise<GatewayModel> {
  const response = await apiClient.put<GatewayModel>(`/console/gateway-models/${id}`, payload)
  return response.data
}

export async function deleteGatewayModel(id: string): Promise<void> {
  await apiClient.delete(`/console/gateway-models/${id}`)
}

export async function getModelRoutes(): Promise<ModelRoute[]> {
  const response = await apiClient.get<ModelRoute[] | null>('/console/model-routes')
  return listOrEmpty(response.data)
}

export async function createModelRoute(payload: ModelRouteRequest): Promise<ModelRoute> {
  const response = await apiClient.post<ModelRoute>('/console/model-routes', payload)
  return response.data
}

export async function bulkCreateModelRoutes(payload: ModelRouteBulkCreateRequest): Promise<ModelRouteBulkCreateResult> {
  const response = await apiClient.post<Omit<ModelRouteBulkCreateResult, 'routes'> & { routes?: ModelRoute[] | null }>('/console/model-routes/bulk', payload)
  return { ...response.data, routes: listOrEmpty(response.data.routes) }
}

export async function updateModelRoute(id: string, payload: ModelRouteRequest): Promise<ModelRoute> {
  const response = await apiClient.put<ModelRoute>(`/console/model-routes/${id}`, payload)
  return response.data
}

export async function deleteModelRoute(id: string): Promise<void> {
  await apiClient.delete(`/console/model-routes/${id}`)
}

export async function simulateGatewayRouting(model: string, estimatedTokens: number, protocol = 'openai_chat_completions', requiredFeatures: string[] = []): Promise<GatewaySimulation> {
  const response = await apiClient.post<GatewaySimulationPayload>('/console/gateway-simulator', {
    model,
    estimated_tokens: estimatedTokens,
    protocol,
    required_features: requiredFeatures
  })
  return { ...response.data, candidates: listOrEmpty(response.data.candidates) }
}

type PricingEnvelope<T> = { data: T }

function pricingData<T>(payload: T | PricingEnvelope<T>): T {
  if (payload && typeof payload === 'object' && 'data' in payload) {
    return (payload as PricingEnvelope<T>).data
  }
  return payload as T
}

function pricingPath(suffix: string): string {
	return `/console${suffix}`
}

function normalizePricingAnalysis(value: PricingRuleAnalysis | null | undefined): PricingRuleAnalysis {
  const payload = value || {} as PricingRuleAnalysis
  return {
    ...payload,
    required_facts: listOrEmpty(payload.required_facts),
    tiers: listOrEmpty(payload.tiers).map((tier) => ({ ...tier, conditions: listOrEmpty(tier.conditions) })),
    line_codes: listOrEmpty(payload.line_codes)
  }
}

function normalizePricingVersion(value: PricingRuleVersion): PricingRuleVersion {
  return {
    ...value,
    analysis: normalizePricingAnalysis(value.analysis),
    test_cases: listOrEmpty(value.test_cases)
  }
}

function normalizePricingDetail(value: PricingRuleDetail): PricingRuleDetail {
  return {
    ...value,
    active_version: value.active_version ? normalizePricingVersion(value.active_version) : undefined,
    draft: value.draft ? normalizePricingVersion(value.draft) : undefined,
    versions: listOrEmpty(value.versions).map(normalizePricingVersion)
  }
}

export async function getPricingRules(params?: Record<string, string>): Promise<PricingRule[]> {
  const response = await apiClient.get<PricingRule[] | PricingEnvelope<PricingRule[] | null> | null>(pricingPath('/pricing-rules'), { params })
  return listOrEmpty(pricingData(response.data))
}

export async function getPricingRule(id: string): Promise<PricingRuleDetail> {
  const response = await apiClient.get<PricingRuleDetail | PricingEnvelope<PricingRuleDetail>>(pricingPath(`/pricing-rules/${id}`))
  return normalizePricingDetail(pricingData(response.data))
}

export async function createPricingRule(payload: PricingRuleCreateRequest): Promise<PricingRuleDetail> {
  const response = await apiClient.post<PricingRuleDetail | PricingEnvelope<PricingRuleDetail>>(pricingPath('/pricing-rules'), payload)
  return normalizePricingDetail(pricingData(response.data))
}

export async function updatePricingRuleDraft(id: string, payload: PricingDraftUpdateRequest): Promise<PricingRuleDetail> {
  const response = await apiClient.put<PricingRuleDetail | PricingEnvelope<PricingRuleDetail>>(pricingPath(`/pricing-rules/${id}/draft`), payload)
  return normalizePricingDetail(pricingData(response.data))
}

export async function validatePricingRule(expression: string, testCases: PricingRuleCreateRequest['test_cases']): Promise<PricingValidationResult> {
  const response = await apiClient.post<PricingValidationResult | PricingEnvelope<PricingValidationResult>>(
    pricingPath('/pricing-rules/validate'),
    { expression, test_cases: testCases },
    { validateStatus: (status) => status === 200 || status === 422 }
  )
  const result = pricingData(response.data)
  return {
    ...result,
    analysis: result.analysis ? normalizePricingAnalysis(result.analysis) : undefined,
    test_results: listOrEmpty(result.test_results).map((test) => ({ ...test, lines: listOrEmpty(test.lines) })),
    errors: listOrEmpty(result.errors)
  }
}

export async function simulatePricingRule(payload: PricingSimulationRequest): Promise<PricingSimulationResult> {
  const response = await apiClient.post<PricingSimulationResult | PricingEnvelope<PricingSimulationResult>>(pricingPath('/pricing-rules/simulate'), payload)
  const result = pricingData(response.data)
  return { ...result, lines: listOrEmpty(result.lines) }
}

export async function publishPricingRule(id: string, payload: PricingPublishRequest): Promise<PricingRuleDetail> {
  const response = await apiClient.post<PricingRuleDetail | PricingEnvelope<PricingRuleDetail>>(pricingPath(`/pricing-rules/${id}/publish`), payload)
  return normalizePricingDetail(pricingData(response.data))
}

export async function activatePricingRuleVersion(id: string, versionID: string, expectedLockVersion: number): Promise<void> {
  await apiClient.post(pricingPath(`/pricing-rules/${id}/activate/${versionID}`), { expected_lock_version: expectedLockVersion })
}

export async function disablePricingRule(id: string, expectedLockVersion: number): Promise<void> {
  await apiClient.post(pricingPath(`/pricing-rules/${id}/disable`), { expected_lock_version: expectedLockVersion })
}

export async function getPricingEvaluation(id: string): Promise<PricingEvaluation> {
  const response = await apiClient.get<PricingEvaluation | PricingEnvelope<PricingEvaluation>>(pricingPath(`/pricing-evaluations/${id}`))
  const result = pricingData(response.data)
  return { ...result, lines: listOrEmpty(result.lines) }
}

export async function getEffectivePricingReport(params?: { model?: string; protocol?: string; window_hours?: number }): Promise<EffectivePricingReport> {
  const response = await apiClient.get<EffectivePricingReportPayload>('/console/effective-pricing/report', { params })
  return normalizeEffectivePricingReport(response.data)
}

export async function getEffectivePricingPolicy(): Promise<EffectivePricingPolicy> {
  const response = await apiClient.get<EffectivePricingPolicy>('/console/effective-pricing/policy')
  return response.data
}

export async function updateEffectivePricingPolicy(payload: EffectivePricingPolicyRequest): Promise<EffectivePricingPolicy> {
  const response = await apiClient.put<EffectivePricingPolicy>('/console/effective-pricing/policy', payload)
  return response.data
}

export async function getProcurementPrices(): Promise<ProcurementPrice[]> {
  const response = await apiClient.get<ProcurementPrice[] | null>('/console/procurement-prices')
  return listOrEmpty(response.data)
}

export async function createProcurementPrice(payload: ProcurementPriceRequest): Promise<ProcurementPrice> {
  const response = await apiClient.post<ProcurementPrice>('/console/procurement-prices', payload)
  return response.data
}

export async function updateProcurementPrice(id: string, payload: ProcurementPriceRequest): Promise<ProcurementPrice> {
  const response = await apiClient.put<ProcurementPrice>(`/console/procurement-prices/${id}`, payload)
  return response.data
}

export async function getProviderBillingLines(): Promise<ProviderBillingLine[]> {
  const response = await apiClient.get<ProviderBillingLine[] | null>('/console/provider-billing-lines')
  return listOrEmpty(response.data)
}

export async function createProviderBillingLine(payload: ProviderBillingLineRequest): Promise<ProviderBillingLine> {
  const response = await apiClient.post<ProviderBillingLine>('/console/provider-billing-lines', payload)
  return response.data
}

export async function inspectProviderBillingSource(providerAccountID: string, adapterID = 'auto'): Promise<ProviderBillingSourceInspection> {
  const response = await apiClient.post<ProviderBillingSourceInspectionPayload>('/console/provider-billing-sources/inspect', {
    provider_account_id: providerAccountID,
    adapter_id: adapterID
  })
  return normalizeProviderBillingSourceInspection(response.data)
}

export async function getProviderBillingSources(): Promise<ProviderBillingSource[]> {
  const response = await apiClient.get<ProviderBillingSourcePayload[] | null>('/console/provider-billing-sources')
  return listOrEmpty(response.data).map(normalizeProviderBillingSource)
}

export async function updateProviderBillingSource(payload: ProviderBillingSourceRequest): Promise<ProviderBillingSource> {
  const response = await apiClient.put<ProviderBillingSourcePayload>('/console/provider-billing-sources', payload)
  return normalizeProviderBillingSource(response.data)
}

export async function syncProviderBillingSource(id: string): Promise<ProviderBillingSyncResult> {
  const response = await apiClient.post<ProviderBillingSyncResultPayload>(`/console/provider-billing-sources/${id}/sync`)
  return normalizeProviderBillingSyncResult(response.data)
}

export async function getProviderBillingSourceEvidence(id: string, limit = 100): Promise<ProviderBillingSourceEvidence> {
  const response = await apiClient.get<ProviderBillingSourceEvidencePayload>(`/console/provider-billing-sources/${id}/evidence`, { params: { limit } })
  return {
    ...response.data,
    source: normalizeProviderBillingSource(response.data.source),
    runs: listOrEmpty(response.data.runs),
    balances: listOrEmpty(response.data.balances),
    aggregates: listOrEmpty(response.data.aggregates)
  }
}

export async function getProviderCacheCapabilities(): Promise<ProviderCacheCapability[]> {
  const response = await apiClient.get<ProviderCacheCapability[] | null>('/console/provider-cache-capabilities')
  return listOrEmpty(response.data)
}

export async function updateProviderCacheCapability(payload: ProviderCacheCapabilityRequest): Promise<ProviderCacheCapability> {
  const response = await apiClient.put<ProviderCacheCapability>('/console/provider-cache-capabilities', payload)
  return response.data
}

export async function getProviderCacheProbeRuns(limit = 100): Promise<ProviderCacheProbeRun[]> {
  const response = await apiClient.get<ProviderCacheProbeRun[] | null>('/console/provider-cache-probes', { params: { limit } })
  return listOrEmpty(response.data)
}

export async function runProviderCacheProbe(payload: CacheProbeRequest): Promise<ProviderCacheProbeRun> {
  const response = await apiClient.post<ProviderCacheProbeRun>('/console/provider-cache-probes', payload)
  return response.data
}

export async function getEffectivePricingDecisions(): Promise<EffectivePricingDecision[]> {
  const response = await apiClient.get<EffectivePricingDecisionPayload[] | null>('/console/effective-pricing/decisions')
  return listOrEmpty(response.data).map(normalizeEffectivePricingDecision)
}

export async function getEffectivePricingDecisionEvaluations(id: string, limit = 100): Promise<EffectivePricingDecisionEvaluation[]> {
  const response = await apiClient.get<EffectivePricingDecisionEvaluationPayload[] | null>(`/console/effective-pricing/decisions/${id}/evaluations`, { params: { limit } })
  return listOrEmpty(response.data).map(normalizeEffectivePricingDecisionEvaluation)
}

export async function evaluateEffectivePricingDecision(payload: EffectivePricingDecisionEvaluationRequest): Promise<EffectivePricingDecision> {
  const response = await apiClient.post<EffectivePricingDecisionPayload>('/console/effective-pricing/decisions/evaluate', payload)
  return normalizeEffectivePricingDecision(response.data)
}

export async function actOnEffectivePricingDecision(id: string, action: string, canaryPercent = 0): Promise<EffectivePricingDecision> {
  const response = await apiClient.post<EffectivePricingDecisionPayload>(`/console/effective-pricing/decisions/${id}/action`, { action, canary_percent: canaryPercent })
  return normalizeEffectivePricingDecision(response.data)
}

export async function getAPIKeys(): Promise<APIKeyRecord[]> {
  const response = await apiClient.get<APIKeyRecord[] | null>('/console/api-keys')
  return listOrEmpty(response.data).map((record) => normalizeAPIKeyRecord(record as APIKeyRecordPayload))
}

export async function getAPIKeyPolicyExplanation(id: string): Promise<GatewayPolicyExplanation> {
  const response = await apiClient.get<GatewayPolicyExplanation>(`/console/api-keys/${id}/policy-explanation`)
  return normalizeGatewayPolicyExplanation(response.data)
}

export async function createAPIKey(payload: APIKeyCreateRequest): Promise<APIKeyCreateResponse> {
  const response = await apiClient.post<APIKeyCreateResponsePayload>('/console/api-keys', payload)
  return normalizeAPIKeyCreateResponse(response.data)
}

export async function updateAPIKey(id: string, payload: APIKeyUpdateRequest): Promise<APIKeyRecord> {
  const response = await apiClient.put<APIKeyRecordPayload>(`/console/api-keys/${id}`, payload)
  return normalizeAPIKeyRecord(response.data)
}

export async function rotateAPIKey(id: string, gracePeriodSeconds = 0): Promise<APIKeyCreateResponse> {
		const response = await apiClient.post<APIKeyCreateResponsePayload>(`/console/api-keys/${id}/rotate`, { grace_period_seconds: gracePeriodSeconds })
  return normalizeAPIKeyCreateResponse(response.data)
}

export async function disableAPIKey(id: string): Promise<void> {
  await apiClient.post(`/console/api-keys/${id}/disable`)
}

export async function getAuditLogs(params?: RecordListQuery): Promise<AuditLog[]> {
  const response = await apiClient.get<AuditLog[] | null>('/console/audit-logs', { params })
  return listOrEmpty(response.data)
}

export async function getAuditLogSummary(params?: RecordListQuery): Promise<AuditLogSummary> {
  const response = await apiClient.get<AuditLogSummary>('/console/audit-logs/summary', { params })
  return response.data
}

export async function getAlerts(params?: RecordListQuery): Promise<AlertEvent[]> {
  const response = await apiClient.get<AlertEvent[] | null>('/console/alerts', { params })
  return listOrEmpty(response.data)
}

export async function getAlertSummary(params?: RecordListQuery): Promise<AlertSummary> {
  const response = await apiClient.get<AlertSummary>('/console/alerts/summary', { params })
  return response.data
}

export async function acknowledgeAlert(id: string): Promise<AlertEvent> {
  const response = await apiClient.post<AlertEvent>(`/console/alerts/${id}/acknowledge`)
  return response.data
}

export async function resolveAlert(id: string): Promise<AlertEvent> {
  const response = await apiClient.post<AlertEvent>(`/console/alerts/${id}/resolve`)
  return response.data
}

export async function exportAuditLogsCSV(params?: RecordListQuery): Promise<void> {
  await downloadCSV('/console/audit-logs/export', `audit-${Date.now()}.csv`, params)
}

export async function getUsageReport(params?: RecordListQuery): Promise<UsageReport> {
  const response = await apiClient.get<UsageReportPayload>('/console/usage', { params })
  return normalizeUsageReport(response.data)
}

export async function exportUsageCSV(params?: RecordListQuery): Promise<void> {
  await downloadCSV('/console/usage/export', `usage-${Date.now()}.csv`, params)
}

export async function getCostAllocationReport(params?: RecordListQuery): Promise<CostAllocationReport> {
  const response = await apiClient.get<CostAllocationReportPayload>('/console/cost-allocation', { params })
  return { ...response.data, rows: listOrEmpty(response.data.rows) }
}

export async function exportCostAllocationCSV(params?: RecordListQuery): Promise<void> {
  await downloadCSV('/console/cost-allocation/export', `cost-allocation-${Date.now()}.csv`, params)
}

export async function getGatewayTraces(params?: RecordListQuery): Promise<GatewayTrace[]> {
  const response = await apiClient.get<GatewayTrace[] | null>('/console/gateway-traces', { params })
  return listOrEmpty(response.data)
}

export async function getGatewayTraceSummary(params?: RecordListQuery): Promise<GatewayTraceSummary> {
  const response = await apiClient.get<GatewayTraceSummary>('/console/gateway-traces/summary', { params })
  return response.data
}

export async function getSupplyUtilization(windowHours = 24): Promise<SupplyUtilizationReport> {
  const response = await apiClient.get<SupplyUtilizationReport>('/supply/utilization', { params: { window_hours: windowHours } })
  return {
    ...response.data,
    rows: listOrEmpty(response.data.rows).map((row) => ({
      ...row,
      stranded_reasons: stringListOrEmpty(row.stranded_reasons),
      costs: listOrEmpty(row.costs),
      evidence: { ...row.evidence, sources: stringListOrEmpty(row.evidence?.sources) }
    })),
    by_dimension: response.data.by_dimension || {}
  }
}

export async function getCapacityRecommendations(windowHours = 24): Promise<CapacityRecommendationReport> {
  const response = await apiClient.get<CapacityRecommendationReport>('/supply/recommendations', { params: { window_hours: windowHours } })
  return {
    ...response.data,
    items: listOrEmpty(response.data.items).map((item) => ({
      ...item,
      reason_codes: stringListOrEmpty(item.reason_codes),
      counter_evidence: stringListOrEmpty(item.counter_evidence),
      missing_evidence: stringListOrEmpty(item.missing_evidence),
      affected_applications: stringListOrEmpty(item.affected_applications),
      affected_models: stringListOrEmpty(item.affected_models),
      affected_route_groups: stringListOrEmpty(item.affected_route_groups)
    }))
  }
}

export async function getArtifacts(params?: ArtifactListQuery): Promise<ArtifactAdminRecord[]> {
  const response = await apiClient.get<ArtifactAdminRecord[] | null>('/console/artifacts', { params })
  return listOrEmpty(response.data)
}

export async function getArtifactSummary(params?: ArtifactListQuery): Promise<ArtifactSummary> {
  const response = await apiClient.get<ArtifactSummary>('/console/artifacts/summary', { params })
  return response.data
}

export async function getArtifact(id: string): Promise<ArtifactAdminDetail> {
  const response = await apiClient.get<ArtifactAdminDetailPayload>(`/console/artifacts/${id}`)
  return normalizeArtifactAdminDetail(response.data)
}

export async function getArtifactContent(id: string): Promise<Blob> {
  const response = await apiClient.get<Blob>(`/console/artifacts/${encodeURIComponent(id)}/content`, { responseType: 'blob' })
  return response.data
}

export async function getArtifactRuntimes(): Promise<ArtifactRuntime[]> {
  const response = await apiClient.get<ArtifactRuntime[] | null>('/console/artifact-runtimes')
  return listOrEmpty(response.data)
}

export async function retryArtifactDelivery(id: string): Promise<ArtifactDeliveryRetryResult> {
  const response = await apiClient.post<ArtifactDeliveryRetryResult>(`/console/artifacts/${id}/retry-delivery`)
  return response.data
}

export async function getAIJobs(params?: AIJobListQuery): Promise<AIJobAdminRecord[]> {
  const response = await apiClient.get<AIJobAdminRecord[] | null>('/console/ai-jobs', { params })
  return listOrEmpty(response.data)
}

export async function getAIJobSummary(params?: AIJobListQuery): Promise<AIJobSummary> {
  const response = await apiClient.get<AIJobSummary>('/console/ai-jobs/summary', { params })
  return response.data
}

export async function getAIJobRuntime(): Promise<AIJobRuntimeStatus> {
  const response = await apiClient.get<AIJobRuntimeStatus>('/console/ai-jobs/runtime')
  return response.data
}

export async function getAIJob(id: string): Promise<AIJobAdminDetail> {
  const response = await apiClient.get<AIJobAdminDetailPayload>(`/console/ai-jobs/${id}`)
  return normalizeAIJobAdminDetail(response.data)
}

export async function cancelAIJob(id: string): Promise<AIJobAdminActionResult> {
  const response = await apiClient.post<AIJobAdminActionResult>(`/console/ai-jobs/${id}/cancel`)
  return response.data
}

export async function scheduleAIJobAttemptReconciliation(jobID: string, attemptID: string): Promise<AIAttemptReconcileScheduleResult> {
  const response = await apiClient.post<AIAttemptReconcileScheduleResult>(`/console/ai-jobs/${jobID}/attempts/${attemptID}/reconcile`)
  return response.data
}

export async function exportGatewayTracesCSV(params?: RecordListQuery): Promise<void> {
  await downloadCSV('/console/gateway-traces/export', `gateway-traces-${Date.now()}.csv`, params)
}

export async function createExportJob(kind: ExportJobKind, params?: RecordListQuery): Promise<ExportJob> {
  const response = await apiClient.post<ExportJob>('/console/export-jobs', null, { params: { ...params, kind } })
  return response.data
}

export async function getExportJobs(limit = 50): Promise<ExportJob[]> {
  const response = await apiClient.get<ExportJob[] | null>('/console/export-jobs', { params: { limit } })
  return listOrEmpty(response.data)
}

export async function getExportJob(id: string): Promise<ExportJob> {
  const response = await apiClient.get<ExportJob>(`/console/export-jobs/${id}`)
  return response.data
}

export async function downloadExportJob(job: ExportJob): Promise<void> {
  await downloadCSV(`/console/export-jobs/${job.id}/download`, job.filename)
}

export async function getPortalWorkspace(): Promise<PortalWorkspace> {
		const response = await apiClient.get<PortalWorkspace>('/portal/workspace')
		const payload = response.data ?? {} as PortalWorkspace
		return {
			...payload,
			api_keys: listOrEmpty(payload.api_keys).map((record) => normalizeAPIKeyRecord(record as APIKeyRecordPayload)),
			usage: normalizeUsageReport(payload.usage as UsageReportPayload),
			recent_traces: listOrEmpty(payload.recent_traces),
			alerts: listOrEmpty(payload.alerts),
			models: stringListOrEmpty(payload.models)
		}
}

export async function createPortalAPIKey(payload: APIKeyCreateRequest): Promise<APIKeyCreateResponse> {
		const response = await apiClient.post<APIKeyCreateResponsePayload>('/portal/api-keys', payload)
		return normalizeAPIKeyCreateResponse(response.data)
}

export async function rotatePortalAPIKey(id: string, gracePeriodSeconds = 0): Promise<APIKeyCreateResponse> {
		const response = await apiClient.post<APIKeyCreateResponsePayload>(`/portal/api-keys/${id}/rotate`, { grace_period_seconds: gracePeriodSeconds })
		return normalizeAPIKeyCreateResponse(response.data)
}

export async function disablePortalAPIKey(id: string): Promise<void> {
	await apiClient.post(`/portal/api-keys/${id}/disable`)
}

async function downloadCSV(path: string, filename: string, params?: RecordListQuery): Promise<void> {
  const response = await apiClient.get<Blob>(path, { params, responseType: 'blob' })
  const blob = new Blob([response.data], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.click()
  URL.revokeObjectURL(url)
}
