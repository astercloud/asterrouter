import { beforeEach, describe, expect, it, vi } from 'vitest'
import * as control from './control'

const client = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  delete: vi.fn()
}))

vi.mock('@/api/client', () => ({ apiClient: client }))

type ClientMethod = keyof typeof client

describe('control API contracts', () => {
  beforeEach(() => {
    for (const method of Object.values(client)) method.mockReset()
    client.get.mockResolvedValue({ data: [] })
    client.post.mockResolvedValue({ data: {} })
    client.put.mockResolvedValue({ data: {} })
    client.delete.mockResolvedValue({ data: {} })
    window.history.replaceState({}, '', '/console/dashboard')
  })

  it('normalizes nullable account inventory and routing collections', async () => {
    client.get.mockResolvedValueOnce({ data: { provider_count: 0, active_provider_count: 0, api_key_count: 0, active_api_key_count: 0, models: null, recent_audit: null } })
    expect(await control.getDashboard()).toMatchObject({ models: [], recent_audit: [] })
    client.get.mockResolvedValueOnce({ data: [{ id: 'provider-1' }] })
    expect(await control.getProviders()).toEqual([{ id: 'provider-1' }])
    client.get.mockResolvedValueOnce({ data: [{ id: 'check-1' }] })
    expect(await control.getProviderHealthChecks()).toEqual([{ id: 'check-1' }])
    client.get.mockResolvedValueOnce({ data: [{ id: 'account-1', models: null, group_ids: null, temp_unschedulable_rules: null }] })
    expect(await control.getProviderAccounts()).toEqual([{ id: 'account-1', models: [], auto_enable_new_models: false, group_ids: [], temp_unschedulable_rules: [] }])
    client.get.mockResolvedValueOnce({ data: [{ id: 'check-2', models: null }] })
    expect(await control.getProviderAccountHealthChecks()).toEqual([{ id: 'check-2', models: [] }])
    client.get.mockResolvedValueOnce({ data: null })
    expect(await control.getRoutingGroups()).toEqual([])
    client.get.mockResolvedValueOnce({ data: undefined })
    expect(await control.getGatewayModels()).toEqual([])
    client.get.mockResolvedValueOnce({ data: null })
    expect(await control.getModelRoutes()).toEqual([])
    client.get.mockResolvedValueOnce({ data: null })
    expect(await control.getAPIKeys()).toEqual([])
    client.get.mockResolvedValueOnce({ data: null })
    expect(await control.getGovernancePolicies()).toEqual([])
  })

  it('normalizes nullable collections used by every admin list page', async () => {
    const loads: Array<() => Promise<unknown[]>> = [
      control.getDepartments,
      control.getApplications,
      control.getOrganizationGroups,
      control.getWorkspaceUsers,
      control.getRoleBindings,
      () => control.getPricingRules(),
      control.getAuditLogs,
      control.getAlerts,
      control.getGatewayTraces,
      control.getExportJobs
    ]

    for (const load of loads) {
      client.get.mockResolvedValueOnce({ data: null })
      expect(await load()).toEqual([])
    }
  })

  it('normalizes nested collections consumed directly by admin and portal views', async () => {
    client.get.mockResolvedValueOnce({ data: [{ id: 'group-1', member_ids: null }] })
    expect(await control.getOrganizationGroups()).toEqual([{ id: 'group-1', member_ids: [] }])

    client.get.mockResolvedValueOnce({ data: [{ id: 'policy-1', model_allowlist: null, model_denylist: null }] })
    expect(await control.getGovernancePolicies()).toEqual([{ id: 'policy-1', model_allowlist: [], model_denylist: [] }])

    client.get.mockResolvedValueOnce({ data: [{ id: 'key-1', scopes: null, model_allowlist: null, allowed_modalities: null, allowed_operations: null, allowed_cidrs: null }] })
    expect(await control.getAPIKeys()).toEqual([{
      id: 'key-1', scopes: [], model_allowlist: [], allowed_modalities: [], allowed_operations: [], allowed_cidrs: []
    }])

    client.get.mockResolvedValueOnce({
      data: {
        api_keys: [{ id: 'key-1', scopes: null, model_allowlist: null, allowed_modalities: null, allowed_operations: null, allowed_cidrs: null }],
        usage: { by_model: null, recent: null },
        recent_traces: null,
        alerts: null,
        models: null
      }
    })
    expect(await control.getPortalWorkspace()).toMatchObject({
      api_keys: [{ model_allowlist: [] }],
      usage: { by_model: [], recent: [] },
      recent_traces: [],
      alerts: [],
      models: []
    })

    client.get.mockResolvedValueOnce({ data: { rows: [{ reason_codes: null, provider_billing_routing_health: { reason_codes: null } }], decisions: [{ reason_codes: null, last_evaluation_reason_codes: null }] } })
    expect(await control.getEffectivePricingReport()).toMatchObject({
      rows: [{ reason_codes: [], provider_billing_routing_health: { reason_codes: [] } }],
      decisions: [{ reason_codes: [], last_evaluation_reason_codes: [] }]
    })

    client.post.mockResolvedValueOnce({ data: { usage_aggregates: null, warnings: null } })
    expect(await control.inspectProviderBillingSource('account-1')).toMatchObject({ usage_aggregates: [], warnings: [] })

    client.get.mockResolvedValueOnce({ data: [{ id: 'source-1', warnings: null, routing_health: { reason_codes: null } }] })
    expect(await control.getProviderBillingSources()).toEqual([{ id: 'source-1', warnings: [], routing_health: { reason_codes: [] } }])

    client.get.mockResolvedValueOnce({ data: { candidates: null } })
    expect(await control.getAPIKeyPolicyExplanation('key-1')).toMatchObject({ candidates: [] })
  })

  it('normalizes nullable provider mutation responses', async () => {
    const provider = { id: 'provider-1' }
    client.post.mockResolvedValueOnce({ data: provider })
    expect(await control.createProvider({} as never)).toEqual({ id: 'provider-1' })
    client.put.mockResolvedValueOnce({ data: provider })
    expect(await control.updateProvider('provider-1', {} as never)).toEqual({ id: 'provider-1' })
    client.post.mockResolvedValueOnce({ data: { id: 'check-1' } })
    expect(await control.checkProvider('provider-1')).toEqual({ id: 'check-1' })

    const account = { id: 'account-1', models: null, group_ids: null, temp_unschedulable_rules: null }
    client.post.mockResolvedValueOnce({ data: account })
    expect(await control.createProviderAccount({} as never)).toMatchObject({ id: 'account-1', models: [], group_ids: [], temp_unschedulable_rules: [] })
    client.put.mockResolvedValueOnce({ data: account })
    expect(await control.updateProviderAccount('account-1', {} as never)).toMatchObject({ id: 'account-1', models: [], group_ids: [], temp_unschedulable_rules: [] })
    client.post.mockResolvedValueOnce({ data: { id: 'check-2', models: null } })
    expect(await control.checkProviderAccount('account-1')).toEqual({ id: 'check-2', models: [] })

    client.post.mockResolvedValueOnce({
      data: {
        account,
        inventory: { account_id: 'account-1', models: null },
        discovery: { account_id: 'account-1', models: null, added_models: null, missing_models: null, unchanged_models: null, affected_route_ids: null }
      }
    })
    expect(await control.syncProviderAccountModels('account-1', { enabled_models: [], auto_enable_new_models: false })).toMatchObject({
      account: { models: [], group_ids: [], temp_unschedulable_rules: [] },
      inventory: { models: [] },
      discovery: { models: [], added_models: [], missing_models: [], unchanged_models: [], affected_route_ids: [] }
    })
  })

  it('uses admin CRUD endpoint contracts', async () => {
    const payload = { synthetic: true } as never
    const cases: Array<{ run: () => Promise<unknown>; method: ClientMethod; args: unknown[] }> = [
      { run: () => control.getDashboard(), method: 'get', args: ['/console/dashboard'] },
      { run: () => control.getProviders(), method: 'get', args: ['/console/providers'] },
      { run: () => control.getProviderHealthChecks(), method: 'get', args: ['/console/provider-health-checks'] },
      { run: () => control.createProvider(payload), method: 'post', args: ['/console/providers', payload] },
      { run: () => control.updateProvider('provider-1', payload), method: 'put', args: ['/console/providers/provider-1', payload] },
      { run: () => control.checkProvider('provider-1'), method: 'post', args: ['/console/providers/provider-1/check'] },
      { run: () => control.getDepartments(), method: 'get', args: ['/console/departments'] },
      { run: () => control.getApplications(), method: 'get', args: ['/applications'] },
      { run: () => control.createApplication(payload), method: 'post', args: ['/applications', payload] },
      { run: () => control.updateApplication('application-1', payload), method: 'put', args: ['/applications/application-1', payload] },
      { run: () => control.createDepartment(payload), method: 'post', args: ['/console/departments', payload] },
      { run: () => control.updateDepartment('department-1', payload), method: 'put', args: ['/console/departments/department-1', payload] },
      { run: () => control.getOrganizationGroups(), method: 'get', args: ['/console/organization-groups'] },
      { run: () => control.createOrganizationGroup(payload), method: 'post', args: ['/console/organization-groups', payload] },
      { run: () => control.updateOrganizationGroup('organization-1', payload), method: 'put', args: ['/console/organization-groups/organization-1', payload] },
      { run: () => control.deleteOrganizationGroup('organization-1'), method: 'delete', args: ['/console/organization-groups/organization-1'] },
      { run: () => control.getGovernancePolicies(), method: 'get', args: ['/console/policies'] },
      { run: () => control.createGovernancePolicy(payload), method: 'post', args: ['/console/policies', payload] },
      { run: () => control.updateGovernancePolicy('policy-1', payload), method: 'put', args: ['/console/policies/policy-1', payload] },
      { run: () => control.getWorkspaceUsers(), method: 'get', args: ['/console/users'] },
      { run: () => control.createWorkspaceUser(payload), method: 'post', args: ['/console/users', payload] },
      { run: () => control.updateWorkspaceUser('user-1', payload), method: 'put', args: ['/console/users/user-1', payload] },
      { run: () => control.getRoleBindings(), method: 'get', args: ['/console/role-bindings'] },
      { run: () => control.createRoleBinding(payload), method: 'post', args: ['/console/role-bindings', payload] },
      { run: () => control.deleteRoleBinding('binding-1'), method: 'delete', args: ['/console/role-bindings/binding-1'] },
      { run: () => control.getRoutingGroups(), method: 'get', args: ['/console/routing-groups'] },
      { run: () => control.createRoutingGroup(payload), method: 'post', args: ['/console/routing-groups', payload] },
      { run: () => control.updateRoutingGroup('group-1', payload), method: 'put', args: ['/console/routing-groups/group-1', payload] },
      { run: () => control.getProviderAccounts(), method: 'get', args: ['/console/provider-accounts'] },
      { run: () => control.getProviderAccountHealthChecks(), method: 'get', args: ['/console/provider-account-health-checks'] },
      { run: () => control.createProviderAccount(payload), method: 'post', args: ['/console/provider-accounts', payload] },
      { run: () => control.updateProviderAccount('account-1', payload), method: 'put', args: ['/console/provider-accounts/account-1', payload] },
      { run: () => control.checkProviderAccount('account-1'), method: 'post', args: ['/console/provider-accounts/account-1/check'] },
      { run: () => control.getProviderAccountModelInventory('account-1'), method: 'get', args: ['/console/provider-accounts/account-1/models'] },
      { run: () => control.discoverProviderAccountModels('account-1'), method: 'post', args: ['/console/provider-accounts/account-1/models/discover'] },
      {
        run: () => {
          client.post.mockResolvedValueOnce({ data: { account: {}, inventory: {}, discovery: {} } })
          return control.syncProviderAccountModels('account-1', { enabled_models: ['model-a'], auto_enable_new_models: false })
        },
        method: 'post',
        args: ['/console/provider-accounts/account-1/models/sync', { enabled_models: ['model-a'], auto_enable_new_models: false }]
      },
      { run: () => control.clearProviderAccountCooldown('account-1'), method: 'post', args: ['/console/provider-accounts/account-1/clear-cooldown'] },
      { run: () => control.getGatewayModels(), method: 'get', args: ['/console/gateway-models'] },
      { run: () => control.createGatewayModel(payload), method: 'post', args: ['/console/gateway-models', payload] },
      { run: () => control.updateGatewayModel('model-1', payload), method: 'put', args: ['/console/gateway-models/model-1', payload] },
      { run: () => control.deleteGatewayModel('model-1'), method: 'delete', args: ['/console/gateway-models/model-1'] },
      { run: () => control.getModelRoutes(), method: 'get', args: ['/console/model-routes'] },
      { run: () => control.createModelRoute(payload), method: 'post', args: ['/console/model-routes', payload] },
      { run: () => control.bulkCreateModelRoutes({ routes: [payload] }), method: 'post', args: ['/console/model-routes/bulk', { routes: [payload] }] },
      { run: () => control.updateModelRoute('route-1', payload), method: 'put', args: ['/console/model-routes/route-1', payload] },
      { run: () => control.deleteModelRoute('route-1'), method: 'delete', args: ['/console/model-routes/route-1'] },
      { run: () => control.simulateGatewayRouting('model-a', 123), method: 'post', args: ['/console/gateway-simulator', { model: 'model-a', estimated_tokens: 123, protocol: 'openai_chat_completions', required_features: [] }] },
      { run: () => control.getPricingRules(), method: 'get', args: ['/console/pricing-rules', { params: undefined }] },
      { run: () => control.getPricingRule('pricing-1'), method: 'get', args: ['/console/pricing-rules/pricing-1'] },
      { run: () => control.createPricingRule(payload), method: 'post', args: ['/console/pricing-rules', payload] },
      { run: () => control.updatePricingRuleDraft('pricing-1', payload), method: 'put', args: ['/console/pricing-rules/pricing-1/draft', payload] },
      { run: () => control.simulatePricingRule(payload), method: 'post', args: ['/console/pricing-rules/simulate', payload] },
      { run: () => control.publishPricingRule('pricing-1', payload), method: 'post', args: ['/console/pricing-rules/pricing-1/publish', payload] },
      { run: () => control.activatePricingRuleVersion('pricing-1', 'version-1', 4), method: 'post', args: ['/console/pricing-rules/pricing-1/activate/version-1', { expected_lock_version: 4 }] },
      { run: () => control.disablePricingRule('pricing-1', 5), method: 'post', args: ['/console/pricing-rules/pricing-1/disable', { expected_lock_version: 5 }] },
      { run: () => control.getPricingEvaluation('evaluation-1'), method: 'get', args: ['/console/pricing-evaluations/evaluation-1'] },
      { run: () => control.getEffectivePricingPolicy(), method: 'get', args: ['/console/effective-pricing/policy'] },
      { run: () => control.updateEffectivePricingPolicy(payload), method: 'put', args: ['/console/effective-pricing/policy', payload] },
      { run: () => control.getProcurementPrices(), method: 'get', args: ['/console/procurement-prices'] },
      { run: () => control.createProcurementPrice(payload), method: 'post', args: ['/console/procurement-prices', payload] },
      { run: () => control.updateProcurementPrice('price-1', payload), method: 'put', args: ['/console/procurement-prices/price-1', payload] },
      { run: () => control.getProviderBillingLines(), method: 'get', args: ['/console/provider-billing-lines'] },
      { run: () => control.createProviderBillingLine(payload), method: 'post', args: ['/console/provider-billing-lines', payload] },
      { run: () => control.inspectProviderBillingSource('account-a'), method: 'post', args: ['/console/provider-billing-sources/inspect', { provider_account_id: 'account-a', adapter_id: 'auto' }] },
      { run: () => control.getProviderBillingSources(), method: 'get', args: ['/console/provider-billing-sources'] },
      { run: () => control.updateProviderBillingSource(payload), method: 'put', args: ['/console/provider-billing-sources', payload] },
      { run: () => control.syncProviderBillingSource('source-a'), method: 'post', args: ['/console/provider-billing-sources/source-a/sync'] },
      { run: () => control.getProviderBillingSourceEvidence('source-a', 25), method: 'get', args: ['/console/provider-billing-sources/source-a/evidence', { params: { limit: 25 } }] },
      { run: () => control.getProviderCacheCapabilities(), method: 'get', args: ['/console/provider-cache-capabilities'] },
      { run: () => control.updateProviderCacheCapability(payload), method: 'put', args: ['/console/provider-cache-capabilities', payload] },
      { run: () => control.getProviderCacheProbeRuns(25), method: 'get', args: ['/console/provider-cache-probes', { params: { limit: 25 } }] },
      { run: () => control.runProviderCacheProbe({ provider_account_id: 'account-1', upstream_model: 'model-a', protocol: 'openai_chat_completions', prefix_tokens: 2048, max_cost_micros: 100000 }), method: 'post', args: ['/console/provider-cache-probes', { provider_account_id: 'account-1', upstream_model: 'model-a', protocol: 'openai_chat_completions', prefix_tokens: 2048, max_cost_micros: 100000 }] },
      { run: () => control.getEffectivePricingDecisions(), method: 'get', args: ['/console/effective-pricing/decisions'] },
      { run: () => control.getEffectivePricingDecisionEvaluations('decision-1', 25), method: 'get', args: ['/console/effective-pricing/decisions/decision-1/evaluations', { params: { limit: 25 } }] },
      { run: () => control.evaluateEffectivePricingDecision(payload), method: 'post', args: ['/console/effective-pricing/decisions/evaluate', payload] },
      { run: () => control.actOnEffectivePricingDecision('decision-1', 'approve_canary', 5), method: 'post', args: ['/console/effective-pricing/decisions/decision-1/action', { action: 'approve_canary', canary_percent: 5 }] },
      { run: () => control.getAPIKeys(), method: 'get', args: ['/console/api-keys'] },
      { run: () => control.getAPIKeyPolicyExplanation('key-1'), method: 'get', args: ['/console/api-keys/key-1/policy-explanation'] },
      { run: () => control.createAPIKey(payload), method: 'post', args: ['/console/api-keys', payload] },
      { run: () => control.updateAPIKey('key-1', payload), method: 'put', args: ['/console/api-keys/key-1', payload] },
      { run: () => control.rotateAPIKey('key-1', 3600), method: 'post', args: ['/console/api-keys/key-1/rotate', { grace_period_seconds: 3600 }] },
      { run: () => control.disableAPIKey('key-1'), method: 'post', args: ['/console/api-keys/key-1/disable'] },
      { run: () => control.getArtifact('artifact-1'), method: 'get', args: ['/console/artifacts/artifact-1'] },
      { run: () => control.getArtifactContent('artifact / 1'), method: 'get', args: ['/console/artifacts/artifact%20%2F%201/content', { responseType: 'blob' }] },
      { run: () => control.getArtifactRuntimes(), method: 'get', args: ['/console/artifact-runtimes'] },
      { run: () => control.retryArtifactDelivery('artifact-1'), method: 'post', args: ['/console/artifacts/artifact-1/retry-delivery'] },
      { run: () => control.getAIJob('job-1'), method: 'get', args: ['/console/ai-jobs/job-1'] },
      { run: () => control.getAIJobRuntime(), method: 'get', args: ['/console/ai-jobs/runtime'] },
      { run: () => control.cancelAIJob('job-1'), method: 'post', args: ['/console/ai-jobs/job-1/cancel'] },
      { run: () => control.scheduleAIJobAttemptReconciliation('job-1', 'attempt-1'), method: 'post', args: ['/console/ai-jobs/job-1/attempts/attempt-1/reconcile'] },
      { run: () => control.acknowledgeAlert('alert-1'), method: 'post', args: ['/console/alerts/alert-1/acknowledge'] },
      { run: () => control.resolveAlert('alert-1'), method: 'post', args: ['/console/alerts/alert-1/resolve'] }
    ]
    for (const testCase of cases) {
      await testCase.run()
      expect(client[testCase.method]).toHaveBeenLastCalledWith(...testCase.args)
    }
  })

  it('uses query, summary, and asynchronous export endpoint contracts', async () => {
    const params = { limit: 10, q: 'synthetic' }
    const cases: Array<{ run: () => Promise<unknown>; method: ClientMethod; args: unknown[] }> = [
      { run: () => control.getAuditLogs(params), method: 'get', args: ['/console/audit-logs', { params }] },
      { run: () => control.getAuditLogSummary(params), method: 'get', args: ['/console/audit-logs/summary', { params }] },
      { run: () => control.getAlerts(params), method: 'get', args: ['/console/alerts', { params }] },
      { run: () => control.getAlertSummary(params), method: 'get', args: ['/console/alerts/summary', { params }] },
      { run: () => control.getUsageReport(params), method: 'get', args: ['/console/usage', { params }] },
      { run: () => control.getEffectivePricingReport({ model: 'model-a', protocol: 'openai_chat_completions', window_hours: 24 }), method: 'get', args: ['/console/effective-pricing/report', { params: { model: 'model-a', protocol: 'openai_chat_completions', window_hours: 24 } }] },
      { run: () => control.getCostAllocationReport(params), method: 'get', args: ['/console/cost-allocation', { params }] },
      { run: () => control.getGatewayTraces(params), method: 'get', args: ['/console/gateway-traces', { params }] },
      { run: () => control.getGatewayTraceSummary(params), method: 'get', args: ['/console/gateway-traces/summary', { params }] },
      { run: () => control.getArtifacts(params), method: 'get', args: ['/console/artifacts', { params }] },
      { run: () => control.getArtifactSummary(params), method: 'get', args: ['/console/artifacts/summary', { params }] },
      { run: () => control.getAIJobs(params), method: 'get', args: ['/console/ai-jobs', { params }] },
      { run: () => control.getAIJobSummary(params), method: 'get', args: ['/console/ai-jobs/summary', { params }] },
      { run: () => control.createExportJob('usage', params), method: 'post', args: ['/console/export-jobs', null, { params: { ...params, kind: 'usage' } }] },
      { run: () => control.getExportJobs(25), method: 'get', args: ['/console/export-jobs', { params: { limit: 25 } }] },
      { run: () => control.getExportJob('job-1'), method: 'get', args: ['/console/export-jobs/job-1'] }
    ]
    for (const testCase of cases) {
      await testCase.run()
      expect(client[testCase.method]).toHaveBeenLastCalledWith(...testCase.args)
    }
  })

  it('uses the enterprise portal self-service endpoint contracts', async () => {
    const payload = { name: 'Self-service key' } as never
    await control.getPortalWorkspace()
    expect(client.get).toHaveBeenLastCalledWith('/portal/workspace')
    await control.createPortalAPIKey(payload)
    expect(client.post).toHaveBeenLastCalledWith('/portal/api-keys', payload)
    await control.rotatePortalAPIKey('key-1', 300)
    expect(client.post).toHaveBeenLastCalledWith('/portal/api-keys/key-1/rotate', { grace_period_seconds: 300 })
    await control.disablePortalAPIKey('key-1')
    expect(client.post).toHaveBeenLastCalledWith('/portal/api-keys/key-1/disable')
  })

  it('downloads synchronous and asynchronous CSV exports', async () => {
    vi.spyOn(Date, 'now').mockReturnValue(123456)
    client.get.mockResolvedValue({ data: new Blob(['id,value\n1,synthetic\n']) })
    const createObjectURL = vi.fn(() => 'blob:test-control-csv')
    const revokeObjectURL = vi.fn()
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: createObjectURL })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: revokeObjectURL })
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
    const params = { limit: 5 }

    await control.exportAuditLogsCSV(params)
    expect(client.get).toHaveBeenLastCalledWith('/console/audit-logs/export', { params, responseType: 'blob' })
    await control.exportUsageCSV(params)
    expect(client.get).toHaveBeenLastCalledWith('/console/usage/export', { params, responseType: 'blob' })
    await control.exportCostAllocationCSV(params)
    expect(client.get).toHaveBeenLastCalledWith('/console/cost-allocation/export', { params, responseType: 'blob' })
    await control.exportGatewayTracesCSV(params)
    expect(client.get).toHaveBeenLastCalledWith('/console/gateway-traces/export', { params, responseType: 'blob' })
    await control.downloadExportJob({ id: 'job-1', filename: 'job.csv' } as never)
    expect(client.get).toHaveBeenLastCalledWith('/console/export-jobs/job-1/download', { params: undefined, responseType: 'blob' })
    expect(createObjectURL).toHaveBeenCalledTimes(5)
    expect(click).toHaveBeenCalledTimes(5)
    expect(revokeObjectURL).toHaveBeenCalledTimes(5)
  })
})
