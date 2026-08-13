import { expect, test, type Locator, type Page } from '@playwright/test'
import { adminPost, captureBrowserErrors, controlAPI, envelope, expectNoHorizontalOverflow, loginDemo, loginTestPrincipal } from './fixtures'

type SupplierFixture = {
  provider: { id: string }
  account: { id: string; provider_id: string; name: string }
  model: { id: string; model_id: string }
  route: { id: string }
  workspaceKey: { key: string; record: { id: string } }
}

type EffectivePricingReportRow = {
  provider_account_id: string
  request_count: number
  cost_confidence: string
  effective_cost_micros_per_1m: number
  price_id: string
  billing_consistency_rate?: number
}

type UsageEvidence = {
  id: string
  provider_account_id: string
  upstream_request_id: string
  procurement_cost_micros?: number
  procurement_cost_source: string
  procurement_cost_confidence: string
  provider_billing_line_id: string
}

function dialogField(dialog: Locator, label: string, selector = 'input'): Locator {
  return dialog.locator('.field').filter({ hasText: label }).locator(selector).first()
}

async function createSupplierFixture(
  page: Page,
  token: string,
  runID: string,
  label: string,
  publicModel: string,
  upstreamModel: string
): Promise<SupplierFixture> {
  const upstreamPort = process.env.ASTER_E2E_UPSTREAM_PORT || '19000'
  const provider = await adminPost<{ id: string }>(page, token, '/providers', {
    name: `${label} provider ${runID}`,
    type: 'openai_compatible',
    base_url: `http://127.0.0.1:${upstreamPort}/v1`,
    status: 'active',
    priority: 10
  })
  const account = await adminPost<{ id: string; provider_id: string; name: string; secret_configured: boolean }>(page, token, '/provider-accounts', {
    provider_id: provider.id,
    name: `${label} account ${runID}`,
    platform: 'openai_compatible',
    auth_type: 'api_key',
    status: 'active',
    schedulable: true,
    priority: 10,
    concurrency: 4,
    rate_multiplier: 1,
    models: [upstreamModel],
    group_ids: [],
    secret: 'synthetic-effective-pricing-secret'
  })
  expect(account.secret_configured).toBe(true)
  const model = await adminPost<{ id: string; model_id: string }>(page, token, '/gateway-models', {
    model_id: publicModel,
    name: `${label} model ${runID}`,
    description: 'Synthetic effective-pricing E2E route',
    modality: 'chat',
    default_route_group: 'default',
    status: 'active'
  })
  const route = await adminPost<{ id: string }>(page, token, '/model-routes', {
    gateway_model_id: model.id,
    route_group: 'default',
    provider_account_id: account.id,
    upstream_model: upstreamModel,
    upstream_format: 'openai_chat',
    priority: 10,
    weight: 100,
    status: 'active'
  })
  const workspaceKey = await adminPost<{ key: string; record: { id: string } }>(page, token, '/api-keys', {
    name: `${label} effective pricing key ${runID}`,
    model_allowlist: [publicModel],
    qps_limit: 10,
    monthly_token_limit: 100000
  })
  return { provider, account, model, route, workspaceKey }
}

async function addProcurementPrice(
  page: Page,
  supplier: SupplierFixture,
  upstreamModel: string,
  inputRate: number,
  outputRate: number,
  sourceReference: string
) {
  await page.getByRole('button', { name: 'Add procurement price' }).click()
  const dialog = page.getByRole('dialog', { name: 'Add procurement price' })
  await dialogField(dialog, 'Route Resources', 'select').selectOption(supplier.account.id)
  await dialogField(dialog, 'Model', 'select').selectOption(upstreamModel)
  await dialogField(dialog, 'Uncached input price').fill(String(inputRate))
  await dialogField(dialog, 'Cache read price').fill(String(Math.floor(inputRate / 10)))
  await dialogField(dialog, '5-minute cache write price').fill(String(inputRate))
  await dialogField(dialog, '1-hour cache write price').fill(String(inputRate))
  await dialogField(dialog, 'Output price').fill(String(outputRate))
  await dialogField(dialog, 'Per-request fee').fill('0')
  await dialogField(dialog, 'Quoted multiplier').fill(inputRate === 2_000_000 ? '2' : '1')
  await dialogField(dialog, 'Recharge paid multiplier').fill('1')
  await dialogField(dialog, 'Official input baseline').fill('1000000')
  await dialogField(dialog, 'Official output baseline').fill('2000000')
  await dialogField(dialog, 'Confidence', 'select').selectOption('exact')
  await dialogField(dialog, 'Quote source').fill(sourceReference)
  const responsePromise = page.waitForResponse((response) =>
    response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/v1/console/procurement-prices'
  )
  await dialog.getByRole('button', { name: 'Save' }).click()
  const response = await responsePromise
  expect(response.status()).toBe(200)
  const body = await response.json() as { data: Record<string, unknown> }
  expect(body.data).toMatchObject({
    provider_id: supplier.provider.id,
    provider_account_id: supplier.account.id,
    upstream_model: upstreamModel,
    protocol: 'openai_chat_completions',
    confidence: 'exact',
    status: 'active',
    source_reference: sourceReference
  })
  return body.data as { id: string }
}

test('@e2e-effective-pricing-001 effective pricing executes a measured provider switch through canary', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The stateful provider-switch lifecycle runs once; surface coverage verifies responsive layouts.')
  test.setTimeout(120_000)
  const errors = captureBrowserErrors(page)
  await loginDemo(page)
  const token = await loginTestPrincipal(page)
  const runID = `${testInfo.project.name.replace(/[^a-z0-9]+/gi, '-').toLowerCase()}-${Date.now().toString(36)}`
  const upstreamModel = `effective-upstream-${runID}`
  const currentModel = `effective-current-${runID}`
  const candidateModel = `effective-candidate-${runID}`
  const current = await createSupplierFixture(page, token, runID, 'Current expensive', currentModel, upstreamModel)
  const candidate = await createSupplierFixture(page, token, runID, 'Candidate efficient', candidateModel, upstreamModel)

  await page.goto('/console/model-services/effective-pricing')
  await expect(page).toHaveURL(/\/console\/model-services\/effective-pricing$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Effective Pricing & Cache Routing' })).toBeVisible()

  await page.getByRole('button', { name: 'Policy' }).click()
  let dialog = page.getByRole('dialog', { name: 'Effective pricing policy' })
  await dialogField(dialog, 'Mode', 'select').selectOption('cost_first')
  await dialogField(dialog, 'Minimum samples').fill('1')
  await dialogField(dialog, 'Minimum metric coverage').fill('0')
  await dialogField(dialog, 'Minimum billing consistency').fill('0')
  await dialogField(dialog, 'Minimum effective cost improvement').fill('0.1')
  await dialogField(dialog, 'Maximum error-rate regression').fill('1')
  await dialogField(dialog, 'Maximum P95 latency regression').fill('1')
  await dialogField(dialog, 'Canary percent').fill('25')
  await dialogField(dialog, 'Daily probe token budget').fill('100000')
  await dialogField(dialog, 'Daily probe cost budget').fill('100000')
  await dialogField(dialog, 'Per-account probe cooldown').fill('0')
  await dialog.getByLabel('Enable controlled probes').check()
  const policyResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'PUT' && new URL(response.url()).pathname === '/api/v1/console/effective-pricing/policy'
  )
  await dialog.getByRole('button', { name: 'Save' }).click()
  const policyResponse = await policyResponsePromise
  expect(policyResponse.status()).toBe(200)
  await expect(policyResponse.json()).resolves.toMatchObject({
    data: {
      mode: 'cost_first', min_sample_count: 1, min_metrics_coverage: 0, min_billing_consistency: 0,
      min_cost_improvement: 0.1, max_error_rate_regression: 1, max_p95_latency_regression: 1,
      canary_percent: 25, probe_enabled: true, probe_cooldown_seconds: 0
    }
  })

  const currentPrice = await addProcurementPrice(page, current, upstreamModel, 2_000_000, 4_000_000, `current-${runID}`)
  const candidatePrice = await addProcurementPrice(page, candidate, upstreamModel, 1_000_000, 2_000_000, `candidate-${runID}`)

  const usageByAccount = new Map<string, UsageEvidence>()
  for (const supplier of [current, candidate]) {
    const completion = await page.request.post('/v1/chat/completions', {
      headers: { Authorization: `Bearer ${supplier.workspaceKey.key}` },
      data: { model: supplier.model.model_id, messages: [{ role: 'user', content: `effective pricing sample ${runID}` }] }
    })
    expect(completion.status()).toBe(200)
    await expect(completion.json()).resolves.toMatchObject({
      id: 'e2e-completion', usage: { prompt_tokens: 7, completion_tokens: 11 }
    })
    await expect.poll(async () => {
      const usage = await envelope<{ recent: UsageEvidence[] }>(await page.request.get(
        `/api/v1/console/usage?api_key_id=${encodeURIComponent(supplier.workspaceKey.record.id)}&limit=10`,
        { headers: { Authorization: `Bearer ${token}` } }
      ))
      const record = usage.recent.find((item) => item.provider_account_id === supplier.account.id)
      if (record) usageByAccount.set(supplier.account.id, record)
      return record
    }, { message: `persisted Gateway usage for ${supplier.account.name}` }).toMatchObject({
      provider_account_id: supplier.account.id,
      procurement_cost_source: 'procurement_price',
      procurement_cost_confidence: 'exact',
      provider_billing_line_id: ''
    })
    expect(usageByAccount.get(supplier.account.id)?.upstream_request_id).not.toBe('')
  }

  const reportResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'GET' && new URL(response.url()).pathname === '/api/v1/console/effective-pricing/report'
  )
  await page.getByRole('button', { name: 'Refresh' }).click()
  const reportResponse = await reportResponsePromise
  expect(reportResponse.status()).toBe(200)
  const reportBody = await reportResponse.json() as { data: { rows: EffectivePricingReportRow[] } }
  const currentRow = reportBody.data.rows.find((row) => row.provider_account_id === current.account.id)
  const candidateRow = reportBody.data.rows.find((row) => row.provider_account_id === candidate.account.id)
  expect(currentRow).toMatchObject({ request_count: 1, cost_confidence: 'exact', price_id: currentPrice.id })
  expect(candidateRow).toMatchObject({ request_count: 1, cost_confidence: 'exact', price_id: candidatePrice.id })
  expect(currentRow!.effective_cost_micros_per_1m).toBeGreaterThan(candidateRow!.effective_cost_micros_per_1m)
  await expect(page.locator('.ep-table tbody tr').filter({ hasText: current.account.name })).toContainText(/1 requests/i)
  await expect(page.locator('.ep-table tbody tr').filter({ hasText: candidate.account.name })).toContainText(/1 requests/i)

  const currentUsage = usageByAccount.get(current.account.id)!
  const externalLineID = `line-${runID}`
  await page.getByRole('button', { name: 'Import bill' }).click()
  dialog = page.getByRole('dialog', { name: 'Import provider bill' })
  await dialogField(dialog, 'Route Resources', 'select').selectOption(current.account.id)
  await dialogField(dialog, 'Model', 'select').selectOption(upstreamModel)
  await dialogField(dialog, 'External billing line ID').fill(externalLineID)
  await dialogField(dialog, 'Upstream request ID').fill(currentUsage.upstream_request_id)
  await dialogField(dialog, 'Billed amount').fill('99')
  await dialogField(dialog, 'Confidence', 'select').selectOption('exact')
  const billingResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/v1/console/provider-billing-lines'
  )
  await dialog.getByRole('button', { name: 'Save' }).click()
  const billingResponse = await billingResponsePromise
  expect(billingResponse.status()).toBe(200)
  const billingBody = await billingResponse.json() as { data: Record<string, unknown> }
  expect(billingBody.data).toMatchObject({
    provider_id: current.provider.id,
    provider_account_id: current.account.id,
    external_line_id: externalLineID,
    external_request_id: currentUsage.upstream_request_id,
    usage_record_id: currentUsage.id,
    upstream_model: upstreamModel,
    amount_micros: 99,
    confidence: 'exact',
    reconciliation_status: 'matched'
  })
  const billingLineID = String(billingBody.data.id)
  await expect.poll(async () => {
    const usage = await envelope<{ recent: UsageEvidence[] }>(await page.request.get(
      `/api/v1/console/usage?api_key_id=${encodeURIComponent(current.workspaceKey.record.id)}&limit=10`,
      { headers: { Authorization: `Bearer ${token}` } }
    ))
    return usage.recent.find((item) => item.id === currentUsage.id)
  }, { message: 'Gateway usage reconciled to the imported provider bill' }).toMatchObject({
    id: currentUsage.id,
    upstream_request_id: currentUsage.upstream_request_id,
    procurement_cost_micros: 99,
    procurement_cost_source: 'billing',
    procurement_cost_confidence: 'exact',
    provider_billing_line_id: billingLineID
  })
  const reconciledReport = await envelope<{ rows: EffectivePricingReportRow[] }>(await page.request.get(
    `/api/v1/console/effective-pricing/report?model=${encodeURIComponent(upstreamModel)}&protocol=openai_chat_completions&window_hours=24`,
    { headers: { Authorization: `Bearer ${token}` } }
  ))
  expect(reconciledReport.rows.find((row) => row.provider_account_id === current.account.id)).toMatchObject({
    request_count: 1,
    cost_confidence: 'exact',
    billing_consistency_rate: 1
  })

  await page.getByRole('button', { name: 'Cache quality' }).click()
  let candidateCacheRow = page.locator('.cache-row').filter({ hasText: candidate.account.name })
  await candidateCacheRow.getByRole('button', { name: 'Configure capability' }).click()
  dialog = page.getByRole('dialog', { name: 'Configure provider cache capability' })
  await dialogField(dialog, 'Capability status', 'select').selectOption('accepted')
  await dialogField(dialog, 'Affinity transport', 'select').selectOption('header')
  await dialogField(dialog, 'Affinity field').fill('X-E2E-Session')
  await dialogField(dialog, 'Cache control mode', 'select').selectOption('prompt_cache_key')
  const capabilityResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'PUT' && new URL(response.url()).pathname === '/api/v1/console/provider-cache-capabilities'
  )
  await dialog.getByRole('button', { name: 'Save' }).click()
  const capabilityResponse = await capabilityResponsePromise
  expect(capabilityResponse.status()).toBe(200)
  const capabilityBody = await capabilityResponse.json() as { data: Record<string, unknown> }
  expect(capabilityBody.data).toMatchObject({
    provider_account_id: candidate.account.id,
    upstream_model: upstreamModel,
    protocol: 'openai_chat_completions',
    support_status: 'accepted',
    pool_affinity_grade: 'unknown',
    affinity_transport: 'header',
    affinity_field: 'X-E2E-Session',
    cache_control_mode: 'prompt_cache_key'
  })

  await page.reload()
  await page.getByRole('button', { name: 'Cache quality' }).click()
  candidateCacheRow = page.locator('.cache-row').filter({ hasText: candidate.account.name })
  await candidateCacheRow.getByRole('button', { name: 'Configure capability' }).click()
  dialog = page.getByRole('dialog', { name: 'Configure provider cache capability' })
  await expect(dialogField(dialog, 'Capability status', 'select')).toHaveValue('accepted')
  await expect(dialogField(dialog, 'Affinity transport', 'select')).toHaveValue('header')
  await expect(dialogField(dialog, 'Affinity field')).toHaveValue('X-E2E-Session')
  await expect(dialogField(dialog, 'Cache control mode', 'select')).toHaveValue('prompt_cache_key')
  await dialog.getByRole('button', { name: 'Close' }).click()

  await page.getByRole('button', { name: 'Probe records' }).click()
  await page.getByRole('button', { name: 'Run probe' }).click()
  dialog = page.getByRole('dialog', { name: 'Run controlled cache probe' })
  await dialogField(dialog, 'Route Resources', 'select').selectOption(candidate.account.id)
  await dialogField(dialog, 'Model', 'select').selectOption(upstreamModel)
  await dialogField(dialog, 'Synthetic prefix token estimate').fill('256')
  await dialogField(dialog, 'Maximum accepted cost').fill('100000')
  await dialog.getByLabel(/I confirm this run will send three synthetic requests/).check()
  const probeResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/v1/console/provider-cache-probes'
  )
  await dialog.getByRole('button', { name: 'Save' }).click()
  const probeResponse = await probeResponsePromise
  expect(probeResponse.status()).toBe(200)
  const probeBody = await probeResponse.json() as { data: Record<string, unknown> }
  expect(probeBody.data).toMatchObject({
    provider_account_id: candidate.account.id,
    upstream_model: upstreamModel,
    status: 'succeeded',
    warm_cache_read_tokens: 0,
    reuse_cache_read_tokens: 240,
    control_cache_read_tokens: 0,
    cache_fields_present: true
  })
  expect(new Set([
    probeBody.data.warm_upstream_request_id,
    probeBody.data.reuse_upstream_request_id,
    probeBody.data.control_upstream_request_id
  ]).size).toBe(3)
  const probeRow = page.locator('.probe-row').filter({ hasText: candidate.account.name })
  await expect(probeRow).toContainText('240')
  await expect(probeRow).toContainText('succeeded')

  await page.getByRole('button', { name: 'Cache quality' }).click()
  candidateCacheRow = page.locator('.cache-row').filter({ hasText: candidate.account.name })
  await expect(candidateCacheRow).toContainText('observed')
  await expect(candidateCacheRow).toContainText('probable')

  await page.getByRole('button', { name: 'Switch center' }).click()
  await page.getByRole('button', { name: 'New evaluation' }).click()
  dialog = page.getByRole('dialog', { name: 'Evaluate provider switch' })
  await dialogField(dialog, 'Gateway model', 'select').selectOption(currentModel)
  await dialogField(dialog, 'Provider upstream model', 'select').selectOption(upstreamModel)
  await dialogField(dialog, 'Current route', 'select').selectOption(current.account.id)
  await dialogField(dialog, 'Candidate route', 'select').selectOption(candidate.account.id)
  const decisionResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/v1/console/effective-pricing/decisions/evaluate'
  )
  await dialog.getByRole('button', { name: 'Save' }).click()
  const decisionResponse = await decisionResponsePromise
  expect(decisionResponse.status()).toBe(200)
  const decisionBody = await decisionResponse.json() as { data: Record<string, unknown> }
  expect(decisionBody.data).toMatchObject({
    model: currentModel,
    upstream_model: upstreamModel,
    current_provider_account_id: current.account.id,
    candidate_provider_account_id: candidate.account.id,
    status: 'recommended',
    sample_count: 1,
    confidence: 'exact',
    reason_codes: []
  })
  expect(Number(decisionBody.data.current_cost_micros_per_1m)).toBeGreaterThan(Number(decisionBody.data.candidate_cost_micros_per_1m))
  const decisionID = String(decisionBody.data.id)
  let decisionCard = page.locator('.decision-card').filter({ hasText: decisionID })
  await expect(decisionCard).toContainText('recommended')

  const actionResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'POST' && new URL(response.url()).pathname === `/api/v1/console/effective-pricing/decisions/${decisionID}/action`
  )
  await decisionCard.getByRole('button', { name: 'Start canary' }).click()
  const actionResponse = await actionResponsePromise
  expect(actionResponse.status()).toBe(200)
  await expect(actionResponse.json()).resolves.toMatchObject({
    data: { id: decisionID, status: 'canary', canary_percent: 25 }
  })

  await page.reload()
  await page.getByRole('button', { name: 'Policy' }).click()
  dialog = page.getByRole('dialog', { name: 'Effective pricing policy' })
  await expect(dialogField(dialog, 'Mode', 'select')).toHaveValue('cost_first')
  await expect(dialogField(dialog, 'Minimum samples')).toHaveValue('1')
  await expect(dialogField(dialog, 'Canary percent')).toHaveValue('25')
  await expect(dialog.getByLabel('Enable controlled probes')).toBeChecked()
  await dialog.getByRole('button', { name: 'Close' }).click()
  await page.getByRole('button', { name: 'Switch center' }).click()
  decisionCard = page.locator('.decision-card').filter({ hasText: decisionID })
  await expect(decisionCard).toContainText('canary')
  await expect(decisionCard.getByRole('button', { name: 'Activate' })).toBeVisible()
  const evaluationsResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'GET' && new URL(response.url()).pathname === `/api/v1/console/effective-pricing/decisions/${decisionID}/evaluations`
  )
  await decisionCard.getByRole('button', { name: 'Window evidence' }).click()
  const evaluationsResponse = await evaluationsResponsePromise
  expect(evaluationsResponse.status()).toBe(200)
  await expect(evaluationsResponse.json()).resolves.toMatchObject({ data: [] })
  const evidenceDialog = page.getByRole('dialog').filter({ hasText: decisionID })
  await expect(evidenceDialog.getByText('No completed evaluation windows yet.')).toBeVisible()
  await evidenceDialog.getByRole('button', { name: 'Close' }).click()

  const audits = await envelope<Array<{ action: string; resource_type: string; resource_id: string }>>(
    await page.request.get(controlAPI('/audit-logs?limit=200'), { headers: { Authorization: `Bearer ${token}` } })
  )
  expect(audits).toEqual(expect.arrayContaining([
    expect.objectContaining({ action: 'create', resource_type: 'procurement_price', resource_id: currentPrice.id }),
    expect.objectContaining({ action: 'create', resource_type: 'procurement_price', resource_id: candidatePrice.id }),
    expect.objectContaining({ action: 'import', resource_type: 'provider_billing_line', resource_id: billingLineID }),
    expect.objectContaining({ action: 'upsert', resource_type: 'provider_cache_capability', resource_id: capabilityBody.data.id }),
    expect.objectContaining({ action: 'run', resource_type: 'provider_cache_probe', resource_id: probeBody.data.id }),
    expect.objectContaining({ action: 'evaluate', resource_type: 'effective_pricing_decision', resource_id: decisionID }),
    expect.objectContaining({ action: 'approve_canary', resource_type: 'effective_pricing_decision', resource_id: decisionID })
  ]))

  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: testInfo.outputPath(`effective-pricing-canary-${runID}.png`), fullPage: true })
  expect(errors).toEqual([])
})

test('@e2e-effective-pricing-002 billing source inspection keeps aggregate evidence distinct from bill lines', async ({ page }, testInfo) => {
  const errors = captureBrowserErrors(page)
  const source = {
    id: 'source-e2e', provider_id: 'provider-source-e2e', provider_account_id: 'account-source-e2e', adapter_id: 'sub2api_compatible',
    status: 'observe_only', automatic_sync_enabled: true, sync_interval_seconds: 3600,
    capabilities: { usage_cost_lines: false, aggregate_usage: true, balance: true, incremental_sync: false, price_feed: false },
    detection_status: 'schema_match', contract_version: 'sub2api_v1_usage', evidence_hash: '0123456789abcdef0123456789abcdef', warnings: [],
    next_sync_at: '2026-07-15T09:00:00Z', last_sync_started_at: '2026-07-15T08:00:00Z',
    last_sync_completed_at: '2026-07-15T08:00:01Z', last_success_at: '2026-07-15T08:00:01Z',
    consecutive_failures: 0, last_error_code: '', version: 3, created_by: 'admin', updated_by: 'admin',
    created_at: '2026-07-15T07:00:00Z', updated_at: '2026-07-15T08:00:01Z',
    routing_health: { source_status: 'observe_only', status: 'observe_only', hard_blocked: false, economic_switch_eligible: false, reason_codes: ['provider_billing_source_observe_only'], evaluated_at: '2026-07-15T08:00:01Z', evidence_observed_at: '2026-07-15T08:00:01Z', evidence_stale_after_seconds: 21600 }
  }
  const run = {
    id: 'run-e2e', source_id: source.id, provider_id: source.provider_id, provider_account_id: source.provider_account_id,
    trigger: 'scheduled', triggered_by: 'worker', adapter_id: source.adapter_id, status: 'succeeded',
    capabilities: source.capabilities, detection_status: source.detection_status, contract_version: source.contract_version,
    discovered_lines: 0, imported_lines: 0, skipped_lines: 0, evidence_hash: source.evidence_hash,
    warnings: [], error_code: '', started_at: '2026-07-15T08:00:00Z', finished_at: '2026-07-15T08:00:01Z', created_at: '2026-07-15T08:00:00Z'
  }
  const balance = {
    id: 'balance-e2e', source_id: source.id, sync_run_id: run.id, provider_account_id: source.provider_account_id,
    kind: 'api_key_quota_remaining', amount_micros: 7_500_000, unlimited: false, currency: 'USD',
    evidence_hash: source.evidence_hash, observed_at: '2026-07-15T08:00:00Z', created_at: '2026-07-15T08:00:01Z'
  }
  const aggregate = {
    id: 'aggregate-e2e', source_id: source.id, sync_run_id: run.id, provider_account_id: source.provider_account_id,
    scope: 'model_30d', model: 'claude-sonnet', request_count: 7, input_tokens: 350, output_tokens: 60,
    cache_creation_tokens: 100, cache_read_tokens: 180, list_cost_micros: 6_500_000, actual_cost_micros: 3_250_000,
    currency: 'USD', evidence_hash: source.evidence_hash, observed_at: '2026-07-15T08:00:00Z', created_at: '2026-07-15T08:00:01Z'
  }
  let savedPayload: Record<string, unknown> | undefined
  let syncRequests = 0
  await loginDemo(page)
  await page.route('**/api/v1/console/provider-accounts', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        code: 0,
        message: 'success',
        data: [{ id: 'account-source-e2e', provider_id: 'provider-source-e2e', name: 'Synthetic procurement', status: 'active', models: ['synthetic-model'] }]
      })
    })
  })
  await page.route('**/api/v1/console/provider-billing-sources/inspect', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        code: 0,
        message: 'success',
        data: {
          provider_id: 'provider-source-e2e', provider_account_id: 'account-source-e2e',
          provider_name: 'Synthetic channel', provider_account_name: 'Synthetic procurement',
          adapter_id: 'sub2api_compatible', detection_status: 'schema_match', contract_version: 'sub2api_v1_usage',
          currency: 'USD',
          capabilities: { usage_cost_lines: false, aggregate_usage: true, balance: true, incremental_sync: false, price_feed: false },
          balance: { kind: 'api_key_quota_remaining', amount_micros: 7_500_000, unlimited: false, currency: 'USD', observed_at: '2026-07-15T08:00:00Z' },
          usage_aggregates: [{ scope: 'total', request_count: 10, input_tokens: 500, output_tokens: 80, cache_creation_tokens: 120, cache_read_tokens: 200, list_cost_micros: 8_000_000, actual_cost_micros: 4_250_000 }, { scope: 'model_30d', model: 'claude-sonnet', request_count: 7, input_tokens: 350, output_tokens: 60, cache_creation_tokens: 100, cache_read_tokens: 180, list_cost_micros: 6_500_000, actual_cost_micros: 3_250_000 }],
          discovered_lines: 0, evidence_hash: '0123456789abcdef0123456789abcdef',
          warnings: ['usage_cost_lines_unavailable', 'remaining_is_quota_not_wallet_balance', 'aggregate_totals_are_not_billing_lines'],
          observed_at: '2026-07-15T08:00:00Z'
        }
      })
    })
  })
  await page.route('**/api/v1/console/provider-billing-sources', async (route) => {
    if (route.request().method() === 'PUT') {
      savedPayload = route.request().postDataJSON()
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ code: 0, message: 'success', data: { ...source, ...savedPayload, version: 4 } }) })
      return
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ code: 0, message: 'success', data: [source] }) })
  })
  await page.route('**/api/v1/console/provider-billing-sources/source-e2e/evidence**', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ code: 0, message: 'success', data: { source, runs: [run], balances: [balance], aggregates: [aggregate] } }) })
  })
  await page.route('**/api/v1/console/provider-billing-sources/source-e2e/sync', async (route) => {
    syncRequests++
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ code: 0, message: 'success', data: { source: { ...source, version: 5 }, run, balance, aggregates: [aggregate] } }) })
  })

  await page.goto('/console/model-services/effective-pricing')
  await expect(page).toHaveURL(/\/console\/model-services\/effective-pricing$/)
  await page.getByRole('button', { name: 'Billing source' }).click()
  await expect(page.getByRole('heading', { name: 'Third-party billing source inspection' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Sync run history' })).toBeVisible()
  await expect(page.locator('.routing-health-summary')).toContainText('Routing health')
  await expect(page.locator('.routing-health-summary')).toContainText('Automatic economic switch')
  await expect(page.getByText('succeeded', { exact: true })).toBeVisible()
  await expect(page.getByText('$7.50', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Auto-detect' }).click()
  const inspectionResult = page.locator('.billing-source-result')
  await expect(inspectionResult.locator('.source-result-head p').filter({ hasText: 'sub2api_compatible' })).toBeVisible()
  await expect(inspectionResult.getByText('API key quota remaining', { exact: true })).toBeVisible()
  await expect(inspectionResult.getByText('Aggregate totals are evidence only and are not billing lines. They never create a pseudo-precise unit price.')).toBeVisible()
  await expect(inspectionResult.getByRole('cell', { name: '$4.25' })).toBeVisible()
  await expect(inspectionResult.getByText('claude-sonnet · Last 30 days')).toBeVisible()
  await page.getByLabel('Enable automatic sync').uncheck()
  await page.getByRole('button', { name: 'Save' }).click()
  await expect.poll(() => savedPayload).toMatchObject({ provider_account_id: 'account-source-e2e', automatic_sync_enabled: false, sync_interval_seconds: 3600, version: 3 })
  await page.getByRole('button', { name: 'Sync now' }).click()
  await expect.poll(() => syncRequests).toBe(1)
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('effective-pricing-billing-source.png'), fullPage: true })
  expect(errors).toEqual([])
})
