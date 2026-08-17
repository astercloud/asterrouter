import { expect, test, type Locator, type Page } from '@playwright/test'
import { adminPost, captureBrowserErrors, expectNoHorizontalOverflow, loginDemo, loginTestPrincipal } from './fixtures'

type CatalogFixture = {
  routeGroup: string
  modelID: string
  policyID: string
  workspaceKeyID: string
  primaryAccount: { id: string; name: string }
  fallbackAccount: { id: string; name: string }
}

async function expectControlWithinViewport(control: Locator): Promise<void> {
  await expect(control).toBeVisible()
  const bounds = await control.evaluate((element) => {
    const rect = element.getBoundingClientRect()
    return { left: rect.left, right: rect.right, viewportWidth: window.innerWidth }
  })
  expect(bounds.left).toBeGreaterThanOrEqual(0)
  expect(bounds.right).toBeLessThanOrEqual(bounds.viewportWidth)
}

async function createCatalogFixture(page: Page, token: string, runID: string): Promise<CatalogFixture> {
  const upstreamPort = process.env.ASTER_E2E_UPSTREAM_PORT || '19000'
  const routeGroup = `catalog-${runID}`
  const modelID = `gpt-catalog-model-${runID}`
  const provider = await adminPost<{ id: string }>(page, token, '/providers', {
    name: `Catalog provider ${runID}`, type: 'openai_compatible', base_url: `http://127.0.0.1:${upstreamPort}/v1`, status: 'active', priority: 10
  })
  const createAccount = (label: string, priority: number) => adminPost<{ id: string; name: string }>(page, token, '/provider-accounts', {
    provider_id: provider.id, name: `${label} catalog account ${runID}`, platform: 'openai_compatible', auth_type: 'api_key',
    status: 'active', schedulable: true, priority, concurrency: 4, rate_multiplier: 1, models: [`${label.toLowerCase()}-catalog-upstream`],
    group_ids: [], secret: `${label}-catalog-synthetic-secret`
  })
  const primaryAccount = await createAccount('Primary', 10)
  const fallbackAccount = await createAccount('Fallback', 20)
  const model = await adminPost<{ id: string }>(page, token, '/gateway-models', {
    model_id: modelID, name: `Catalog model ${runID}`, description: 'Supply catalog browser evidence', modality: 'chat',
    default_route_group: routeGroup, status: 'active'
  })
  const createRoute = (account: { id: string }, upstreamModel: string, priority: number) => adminPost(page, token, '/model-routes', {
    gateway_model_id: model.id, route_group: routeGroup, provider_account_id: account.id, upstream_model: upstreamModel,
    upstream_format: 'openai_chat', priority, weight: 100, status: 'active'
  })
  await createRoute(primaryAccount, 'primary-catalog-upstream', 10)
  await createRoute(fallbackAccount, 'fallback-catalog-upstream', 20)
  const createPrice = (account: { id: string }, upstreamModel: string, input: number, output: number) => adminPost(page, token, '/procurement-prices', {
    provider_id: provider.id, provider_account_id: account.id, upstream_model: upstreamModel, protocol: 'openai_chat_completions', currency: 'USD',
    uncached_input_micros_per_1m_tokens: input, cache_read_micros_per_1m_tokens: Math.floor(input / 10),
    cache_write_5m_micros_per_1m_tokens: input, cache_write_1h_micros_per_1m_tokens: input,
    output_micros_per_1m_tokens: output, request_micros: 0, reference_input_micros_per_1m_tokens: input * 2,
    reference_output_micros_per_1m_tokens: output * 2, quoted_multiplier: 0.5, recharge_multiplier: 1,
    source_kind: 'synthetic_e2e', source_reference: runID, confidence: 'exact', status: 'active'
  })
  await createPrice(primaryAccount, 'primary-catalog-upstream', 100_000, 200_000)
  await createPrice(fallbackAccount, 'fallback-catalog-upstream', 300_000, 400_000)
  const policy = await adminPost<{ id: string }>(page, token, '/routing-policies', {
    name: `Catalog routing ${runID}`, description: 'Supply catalog policy actions', route_group: routeGroup, status: 'active', is_default: true,
    strategy: {
      preset: 'balanced', smart_optimization: true, strict_order: false, failover_before_first_byte: true,
      sticky_routing: true, sticky_ttl_seconds: 900, native_protocol_only: false,
      absolute_max_input_per_1m: 0, absolute_max_output_per_1m: 0, max_price_multiple_of_cheapest: 2,
      low_price_pool_mode: 'auto', low_price_pool_percent: 70, low_price_pool_min_candidates: 2, missing_price_action: 'allow',
      model_price_limits: [], resource_batches: [
        { name: 'Primary', provider_account_ids: [primaryAccount.id] },
        { name: 'Fallback', provider_account_ids: [fallbackAccount.id] }
      ],
      preferred_provider_account_ids: [], allowed_models: [modelID], denied_models: [], allowed_protocols: [], denied_protocols: []
    }
  })
  for (const account of [primaryAccount, fallbackAccount]) {
    const health = await adminPost<{ account_id: string; status: string }>(page, token, `/provider-accounts/${account.id}/check`, {})
    expect(health).toMatchObject({ account_id: account.id, status: 'ok' })
  }
  const workspaceKey = await adminPost<{ key: string; record: { id: string } }>(page, token, '/api-keys', {
    name: `Catalog gateway key ${runID}`, routing_policy_id: policy.id, model_allowlist: [modelID]
  })
  const completion = await page.request.post('/v1/chat/completions', {
    data: { model: modelID, messages: [{ role: 'user', content: 'catalog evidence request' }] },
    headers: { Authorization: `Bearer ${workspaceKey.key}` }
  })
  expect(completion.status()).toBe(200)
  return { routeGroup, modelID, policyID: policy.id, workspaceKeyID: workspaceKey.record.id, primaryAccount, fallbackAccount }
}

test('@e2e-supply-catalog-001 Model Hub compares routes and persists policy actions', async ({ page }, testInfo) => {
  test.setTimeout(60_000)
  const errors = captureBrowserErrors(page)
  await loginDemo(page)
  const token = await loginTestPrincipal(page)
  const runID = `${testInfo.project.name}-${Date.now()}`.replace(/[^a-z0-9-]+/gi, '-').toLowerCase()
  const fixture = await createCatalogFixture(page, token, runID)

  await page.goto('/console/model-services/catalog')
  await expect(page).toHaveURL(/\/console\/model-services\/catalog$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Model Hub' })).toBeVisible()
  await expect(page.getByLabel('Apply to routing policy')).toHaveValue(fixture.policyID)
  const openAIFamily = page.getByRole('tab', { name: 'OpenAI' })
  await expect(openAIFamily).toBeVisible()
  await openAIFamily.click()
  await page.getByLabel('Search Model Hub').fill(fixture.modelID)
  await page.getByLabel('Filter by protocol').selectOption('openai_chat')
  const visibleCatalog = page.locator('.model-catalog-list:visible, .mobile-route-list:visible')
  await expect(visibleCatalog).toContainText(fixture.modelID)
  await expect(visibleCatalog).toContainText('$0.1000')
  await expect(visibleCatalog).toContainText('$0.3000')
  await expect(visibleCatalog).toContainText('Reference $0.2000')
  await expect(visibleCatalog).toContainText('Reference $0.6000')
  await expect(visibleCatalog).toContainText('100.0%')
  await expect(visibleCatalog).toContainText('Operational')
  await expect(visibleCatalog).toContainText('0.50×')
  const modelCatalog = page.locator('.model-catalog-list:visible')
  if (await modelCatalog.isVisible()) {
    await expect(modelCatalog).toContainText('Reference $0.2000')
    await expect(modelCatalog).toContainText('Prefer route')
  }
  await expectControlWithinViewport(page.getByRole('button', { name: `Set ${fixture.primaryAccount.name} as preferred` }))
  await expectControlWithinViewport(page.locator(`select:visible[aria-label="Set ordered batch for ${fixture.primaryAccount.name}"]`))
  await expectNoHorizontalOverflow(page)
  const clippedSections = await page.locator('.policy-scope-bar, .catalog-controls, .model-catalog-list:visible, .mobile-route-list:visible').evaluateAll((elements) => elements
    .map((element) => ({ name: element.className, right: element.getBoundingClientRect().right }))
    .filter(({ right }) => right > window.innerWidth + 1))
  expect(clippedSections).toEqual([])

  const visibleRoute = page.locator('.clickable-row:visible').filter({ hasText: fixture.primaryAccount.name }).first()
  const visibleCard = page.locator('.mobile-route-item:visible').filter({ hasText: fixture.primaryAccount.name }).first()
  if (await visibleRoute.isVisible()) await visibleRoute.click()
  else await visibleCard.click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toContainText('primary-catalog-upstream')
  await expect(dialog).toContainText('synthetic_e2e')
  await expect(dialog).toContainText(fixture.routeGroup)
  await expect(dialog).toContainText('$0.2000')
  await expect(dialog).toContainText('$0.4000')
  await expect(dialog).toContainText('0.50×')
  await dialog.getByRole('button', { name: 'Close' }).click()

  const preferenceResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'PUT' && new URL(response.url()).pathname === `/api/v1/console/routing-policies/${fixture.policyID}`
  )
  await page.getByRole('button', { name: `Set ${fixture.primaryAccount.name} as preferred` }).click()
  const preferenceResponse = await preferenceResponsePromise
  expect(preferenceResponse.status()).toBe(200)
  const preferredPolicy = await preferenceResponse.json() as { data: { version: number; strategy: { preferred_provider_account_ids: string[] } } }
  expect(preferredPolicy.data.version).toBe(2)
  expect(preferredPolicy.data.strategy.preferred_provider_account_ids).toEqual([fixture.primaryAccount.id])
  await expect(page.getByText(`${fixture.primaryAccount.name} is now preferred`)).toBeVisible()

  const batchResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'PUT' && new URL(response.url()).pathname === `/api/v1/console/routing-policies/${fixture.policyID}`
  )
  await page.locator(`select:visible[aria-label="Set ordered batch for ${fixture.fallbackAccount.name}"]`).selectOption('0')
  const batchResponse = await batchResponsePromise
  expect(batchResponse.status()).toBe(200)
  const batchedPolicy = await batchResponse.json() as { data: { version: number; strategy: { preferred_provider_account_ids: string[]; resource_batches: Array<{ name: string; provider_account_ids: string[] }> } } }
  expect(batchedPolicy.data).toMatchObject({
    version: 3,
    strategy: {
      preferred_provider_account_ids: [fixture.primaryAccount.id],
      resource_batches: [{ name: 'Primary', provider_account_ids: [fixture.primaryAccount.id, fixture.fallbackAccount.id] }]
    }
  })

  const traces = await (await page.request.get('/api/v1/console/gateway-traces?limit=100', { headers: { Authorization: `Bearer ${token}` } })).json() as { data: Array<Record<string, unknown>> }
  expect(traces.data).toContainEqual(expect.objectContaining({
    api_key_id: fixture.workspaceKeyID, model: fixture.modelID, provider_account_id: fixture.primaryAccount.id, status: 'forwarded', http_status: 200
  }))
  const utilization = await (await page.request.get('/api/v1/supply/utilization?window_hours=24', { headers: { Authorization: `Bearer ${token}` } })).json() as { data: { rows: Array<{ dimension: string; id: string; demand: { requests: number; success_rate: number; fallback_rate: number }; evidence: { trace_count: number; usage_record_count: number } }> } }
  const accountEvidence = utilization.data.rows.find((row) => row.dimension === 'provider_account' && row.id === fixture.primaryAccount.id)
  expect(accountEvidence).toMatchObject({ demand: { requests: 1, success_rate: 1, fallback_rate: 0 }, evidence: { trace_count: 1, usage_record_count: 1 } })
  const auditBeforeRejectedUpdate = await (await page.request.get('/api/v1/console/audit-logs?action=update&resource_type=routing_policy&limit=100', { headers: { Authorization: `Bearer ${token}` } })).json() as { data: Array<{ action: string; resource_type: string; resource_id: string }> }
  expect(auditBeforeRejectedUpdate.data.filter((event) => event.resource_id === fixture.policyID)).toHaveLength(2)

  await page.reload()
  await page.getByLabel('Search Model Hub').fill(fixture.modelID)
  await expect(page.getByRole('button', { name: `Remove preference for ${fixture.primaryAccount.name}` })).toBeVisible()
  await expect(page.locator(`select:visible[aria-label="Set ordered batch for ${fixture.fallbackAccount.name}"]`)).toHaveValue('0')

  await page.route(`**/api/v1/console/routing-policies/${fixture.policyID}`, async (route) => {
    const payload = JSON.parse(route.request().postData() || '{}') as Record<string, unknown>
    await route.continue({ postData: JSON.stringify({ ...payload, route_group: 'invalid route group' }) })
  })
  await page.getByRole('button', { name: `Remove preference for ${fixture.primaryAccount.name}` }).click()
  await expect(page.getByRole('alert')).toContainText('route_group')
  await page.unroute(`**/api/v1/console/routing-policies/${fixture.policyID}`)
  await page.reload()
  await page.getByLabel('Search Model Hub').fill(fixture.modelID)
  await expect(page.getByRole('button', { name: `Remove preference for ${fixture.primaryAccount.name}` })).toBeVisible()
  await expect(page.locator('.policy-scope-copy')).toContainText('v3')
  const auditAfterRejectedUpdate = await (await page.request.get('/api/v1/console/audit-logs?action=update&resource_type=routing_policy&limit=100', { headers: { Authorization: `Bearer ${token}` } })).json() as { data: Array<{ resource_id: string }> }
  expect(auditAfterRejectedUpdate.data.filter((event) => event.resource_id === fixture.policyID)).toHaveLength(2)

  await page.getByLabel('Language').selectOption('zh-CN')
  await expect(page.getByRole('heading', { level: 1, name: '模型广场' })).toBeVisible()
  await expect(page.getByText('仅看可调度线路')).toBeVisible()
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('supply-catalog-zh.png'), fullPage: true })
  expect(errors.some((entry) => entry.includes('status of 400 (Bad Request)'))).toBe(true)
  expect(errors.filter((entry) => !entry.includes('status of 400 (Bad Request)'))).toEqual([])
})
