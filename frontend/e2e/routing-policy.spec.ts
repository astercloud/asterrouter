import { expect, test } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
import { adminPost, captureBrowserErrors, expectNoHorizontalOverflow, loginDemo, loginTestPrincipal } from './fixtures'

async function createRoutingPolicyFixture(page: Parameters<typeof loginDemo>[0], token: string, runID: string) {
  const upstreamPort = process.env.ASTER_E2E_UPSTREAM_PORT || '19000'
  const routeGroup = `policy-${runID}`
  const publicModel = `policy-model-${runID}`
  const createResource = async (label: string, priority: number, rate: number) => {
    const provider = await adminPost<{ id: string }>(page, token, '/providers', {
      name: `${label} policy provider ${runID}`, type: 'openai_compatible',
      base_url: `http://127.0.0.1:${upstreamPort}/v1`, status: 'active', priority
    })
    const account = await adminPost<{ id: string; secret_configured: boolean }>(page, token, '/provider-accounts', {
      provider_id: provider.id, name: `${label} policy account ${runID}`, platform: 'openai_compatible',
      auth_type: 'api_key', status: 'active', schedulable: true, priority, concurrency: 4,
      rate_multiplier: rate, models: [`${label.toLowerCase()}-upstream`], group_ids: [], secret: `${label}-synthetic-secret`
    })
    expect(account.secret_configured).toBe(true)
    return { provider, account, upstreamModel: `${label.toLowerCase()}-upstream`, priority }
  }
  const expensive = await createResource('Priority', 10, 2)
  const cheap = await createResource('Cheap', 20, 0.5)
  const model = await adminPost<{ id: string }>(page, token, '/gateway-models', {
    model_id: publicModel, name: `Policy model ${runID}`, description: 'Routing policy browser evidence',
    modality: 'chat', default_route_group: routeGroup, status: 'active'
  })
  for (const resource of [expensive, cheap]) {
    await adminPost(page, token, '/model-routes', {
      gateway_model_id: model.id, route_group: routeGroup, provider_account_id: resource.account.id,
      upstream_model: resource.upstreamModel, upstream_format: 'openai_chat', priority: resource.priority, weight: 100, status: 'active'
    })
  }
  const createPrice = (resource: typeof cheap, input: number, output: number) => adminPost(page, token, '/procurement-prices', {
    provider_id: resource.provider.id, provider_account_id: resource.account.id, upstream_model: resource.upstreamModel,
    protocol: 'openai_chat_completions', currency: 'USD', uncached_input_micros_per_1m_tokens: input,
    cache_read_micros_per_1m_tokens: 0, cache_write_5m_micros_per_1m_tokens: input,
    cache_write_1h_micros_per_1m_tokens: input, output_micros_per_1m_tokens: output, request_micros: 0,
    reference_input_micros_per_1m_tokens: input, reference_output_micros_per_1m_tokens: output,
    quoted_multiplier: 1, recharge_multiplier: 1, source_kind: 'synthetic_e2e', confidence: 'exact', status: 'active'
  })
  await createPrice(expensive, 500_000, 500_000)
  await createPrice(cheap, 100_000, 100_000)
  return { routeGroup, publicModel, expensive, cheap }
}

test('@e2e-routing-policy-001 enterprise routing policy workbench persists and remains responsive', async ({ page }, testInfo) => {
  const errors = captureBrowserErrors(page)
  await loginDemo(page)
  const token = await loginTestPrincipal(page)
  const runID = `${testInfo.project.name}-${Date.now()}`.replace(/[^a-z0-9-]+/gi, '-').toLowerCase()
  const fixture = await createRoutingPolicyFixture(page, token, runID)
  await page.goto('/console/policies/routing')

  await expect(page.getByRole('heading', { level: 1, name: 'Routing Policies' })).toBeVisible()
  await expect(page.getByRole('heading', { level: 2, name: 'Routing policy list' })).toBeVisible()
  await expect(page.getByRole('heading', { level: 2, name: 'Choose a decision preference' })).toBeVisible()
  await expect(page.getByRole('heading', { level: 2, name: 'How one request is decided' })).toBeVisible()
  await expectNoHorizontalOverflow(page)

  const policyName = `Enterprise production routing ${runID}`
  const updatedDescription = `Cost-optimized production routing ${runID}`
  await page.getByRole('button', { name: 'New policy' }).click()
  await page.getByLabel('Policy name').fill(policyName)
  await page.getByLabel('Route group').fill(fixture.routeGroup)
  await page.getByRole('radio', { name: /Stability first/ }).click()
  const createResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/v1/console/routing-policies'
  )
  await page.getByRole('button', { name: 'Save policy' }).click()
  const createResponse = await createResponsePromise
  expect(createResponse.status()).toBe(200)
  const created = await createResponse.json() as { data: { id: string; version: number; description: string; strategy: { preset: string } } }
  expect(created.data.id).not.toBe('')
  expect(created.data.version).toBe(1)
  expect(created.data.strategy.preset).toBe('stability')
  await expect(page.getByText('Routing policy created')).toBeVisible()

  await page.reload()
  await expect(page.getByText(policyName, { exact: true }).first()).toBeVisible()
  await expect(page.getByText(fixture.routeGroup, { exact: true }).first()).toBeVisible()
  await expect(page.getByText('v1 ·', { exact: false }).filter({ hasText: created.data.description }).first()).toBeVisible()

  await page.getByLabel('Description').fill(updatedDescription)
  await page.getByRole('radio', { name: /Cost first/ }).click()
  const updateResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'PUT' && new URL(response.url()).pathname === `/api/v1/console/routing-policies/${created.data.id}`
  )
  await page.getByRole('button', { name: 'Save policy' }).click()
  const updateResponse = await updateResponsePromise
  expect(updateResponse.status()).toBe(200)
  const updated = await updateResponse.json() as { data: { id: string; version: number; description: string; strategy: { preset: string } } }
  expect(updated.data).toMatchObject({
    id: created.data.id,
    version: 2,
    description: updatedDescription,
    strategy: { preset: 'cost' }
  })
  await expect(page.getByText('Routing policy updated')).toBeVisible()

  await page.reload()
  const policyRow = page.getByRole('row').filter({ hasText: policyName })
  await expect(policyRow).toContainText(`v2 · ${updatedDescription}`)
  await expect(policyRow).toContainText('Cost first')
  await expect(page.getByLabel('Description')).toHaveValue(updatedDescription)
  await expect(page.getByRole('radio', { name: /Cost first/ })).toHaveAttribute('aria-checked', 'true')
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('routing-policy-light-en.png'), fullPage: true })

  await page.goto('/console/model-services/simulator')
  await expect(page.getByLabel('Client protocol').locator('option')).toHaveCount(13)
  await page.getByLabel('Requested model').selectOption(fixture.publicModel)
  const simulationResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/v1/console/gateway-simulator'
  )
  await page.getByRole('button', { name: 'Run simulation' }).click()
  const simulationResponse = await simulationResponsePromise
  expect(simulationResponse.status()).toBe(200)
  const simulation = await simulationResponse.json() as { data: { routing_policy_id: string; routing_policy_version: number; routing_policy_preset: string; candidates: Array<{ provider_account_id: string; eligible: boolean; reason: string }> } }
  expect(simulation.data).toMatchObject({
    routing_policy_id: created.data.id,
    routing_policy_version: 2,
    routing_policy_preset: 'cost'
  })
  expect(simulation.data.candidates).toEqual(expect.arrayContaining([
    expect.objectContaining({ provider_account_id: fixture.cheap.account.id, eligible: true, reason: '' }),
    expect.objectContaining({ provider_account_id: fixture.expensive.account.id, eligible: false, reason: 'routing_policy_relative_price_exceeded' })
  ]))
  await expect(page.locator('.simulation-flow')).toContainText('Cost first · v2')
  await expect(page.getByRole('table')).toContainText(fixture.cheap.upstreamModel)
  await expect(page.getByRole('table')).toContainText('Cheapest-price multiple exceeded')
  await page.reload()
  await page.getByLabel('Requested model').selectOption(fixture.publicModel)
  await page.getByRole('button', { name: 'Run simulation' }).click()
  await expect(page.locator('.simulation-flow')).toContainText('Cost first · v2')
  await expectNoHorizontalOverflow(page)

  await page.getByLabel('Language').selectOption('zh-CN')
  if ((page.viewportSize()?.width || 0) <= 640) {
    await page.getByRole('button', { name: '打开导航' }).click()
  }
  await page.getByRole('button', { name: '深色模式' }).click()
  if ((page.viewportSize()?.width || 0) <= 640) {
    await page.locator('.sidebar-mobile-close').click()
  }
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await expect(page.getByRole('heading', { level: 1, name: '路由模拟器' })).toBeVisible()
  await expect(page.locator('.simulation-flow')).toContainText('成本优先 · v2')
  await expect(page.getByRole('table')).toContainText('超过相对最低价上限')
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('routing-policy-dark-zh.png'), fullPage: true })
  if (testInfo.project.name === 'chromium-desktop') {
    const results = await new AxeBuilder({ page }).analyze()
    const blocking = results.violations.filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
    expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([])
  }
  expect(errors).toEqual([])
})
