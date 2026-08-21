import { expect, test, type Page } from '@playwright/test'
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
  const unknown = await createResource('Unknown', 30, 1)
  const model = await adminPost<{ id: string }>(page, token, '/gateway-models', {
    model_id: publicModel, name: `Policy model ${runID}`, description: 'Routing policy browser evidence',
    modality: 'chat', default_route_group: routeGroup, status: 'active'
  })
  for (const resource of [expensive, cheap, unknown]) {
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
  return { routeGroup, publicModel, expensive, cheap, unknown }
}

type SavedRoutingPolicy = {
  id: string
  version: number
  status: string
  is_default: boolean
  strategy: {
    preset: string
    native_protocol_only: boolean
    absolute_max_input_per_1m: number
    absolute_max_output_per_1m: number
    max_price_multiple_of_cheapest: number
    low_price_pool_mode: string
    low_price_pool_percent: number
    low_price_pool_min_candidates: number
    model_price_limits: Array<{ model: string }>
    resource_batches: Array<{ name: string; provider_account_ids: string[] }>
    preferred_provider_account_ids: string[]
    allowed_models: string[]
    denied_models: string[]
    allowed_protocols: string[]
    denied_protocols: string[]
    failover_before_first_byte: boolean
    sticky_routing: boolean
    sticky_ttl_seconds: number
    smart_optimization: boolean
    strict_order: boolean
  }
}

async function savePolicyThroughUI(page: Page, method: 'POST' | 'PUT', id = ''): Promise<SavedRoutingPolicy> {
  const path = id ? `/api/v1/console/routing-policies/${id}` : '/api/v1/console/routing-policies'
  const responsePromise = page.waitForResponse((response) =>
    response.request().method() === method && new URL(response.url()).pathname === path
  )
  await page.getByRole('button', { name: 'Save policy' }).click()
  const response = await responsePromise
  expect(response.status(), await response.text()).toBe(200)
  const body = await response.json() as { data: SavedRoutingPolicy }
  return body.data
}

function routingPolicySection(page: Page, heading: string) {
  return page.locator('.config-section').filter({ has: page.getByRole('heading', { name: heading, exact: true }) })
}

test('@e2e-routing-policy-001 enterprise routing policy workbench persists and remains responsive', async ({ page }, testInfo) => {
  test.setTimeout(90_000)
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
  await page.getByRole('radio', { name: /Cost first/ }).click()
  await page.getByLabel('Maximum cheapest-price multiple').fill('0')
  await page.getByLabel('When price facts are missing').selectOption('block')
  await page.getByLabel('Pool mode').selectOption('none')
  await page.getByRole('button', { name: 'Add model limit' }).click()
  const modelLimit = page.locator('.model-price-limit-row')
  await modelLimit.getByRole('combobox').selectOption(fixture.publicModel)
  await modelLimit.getByRole('spinbutton').nth(0).fill('0.3')
  await modelLimit.getByRole('spinbutton').nth(1).fill('0.3')
  await page.getByRole('button', { name: 'Add batch' }).click()
  const resourceBatch = page.locator('.batch-row')
  await resourceBatch.getByLabel('Batch name').fill('Production')
  await resourceBatch.getByRole('button', { name: new RegExp(`Cheap policy account ${runID}`) }).click()
  await resourceBatch.getByRole('button', { name: new RegExp(`Priority policy account ${runID}`) }).click()
  await resourceBatch.getByRole('button', { name: new RegExp(`Unknown policy account ${runID}`) }).click()
  await page.locator('.preferred-picker').getByRole('button', { name: new RegExp(`Priority policy account ${runID}`) }).click()
  await page.getByLabel('Strict declared order').check()
  await page.getByLabel('Allowed models').fill(fixture.publicModel)
  await page.locator('.protocol-rule-row').filter({ hasText: 'OpenAI Chat Completions' }).getByRole('combobox').selectOption('allow')
  const createResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/v1/console/routing-policies'
  )
  await page.getByRole('button', { name: 'Save policy' }).click()
  const createResponse = await createResponsePromise
  expect(createResponse.status()).toBe(200)
  const created = await createResponse.json() as { data: { id: string; version: number; description: string; strategy: { preset: string; strict_order: boolean; missing_price_action: string; model_price_limits: Array<{ model: string }>; resource_batches: Array<{ name: string; provider_account_ids: string[] }>; preferred_provider_account_ids: string[] } } }
  expect(created.data.id).not.toBe('')
  expect(created.data.version).toBe(1)
  expect(created.data.strategy).toMatchObject({
    preset: 'cost',
    strict_order: true,
    missing_price_action: 'block',
    model_price_limits: [{ model: fixture.publicModel }],
    resource_batches: [{
      name: 'Production',
      provider_account_ids: [fixture.cheap.account.id, fixture.expensive.account.id, fixture.unknown.account.id]
    }],
    preferred_provider_account_ids: [fixture.expensive.account.id]
  })
  await expect(page.getByText('Routing policy created')).toBeVisible()

  await page.reload()
  await expect(page.getByText(policyName, { exact: true }).first()).toBeVisible()
  await expect(page.getByText(fixture.routeGroup, { exact: true }).first()).toBeVisible()
  await expect(page.getByText('v1 ·', { exact: false }).filter({ hasText: created.data.description }).first()).toBeVisible()
  await expect(page.getByLabel('When price facts are missing')).toHaveValue('block')
  await expect(page.getByLabel('Strict declared order')).toBeChecked()
  await expect(page.locator('.batch-resource-order li')).toHaveCount(3)
  await expect(page.locator('.batch-resource-order li').nth(0)).toContainText(`Cheap policy account ${runID}`)
  await expect(page.locator('.batch-resource-order li').nth(1)).toContainText(`Priority policy account ${runID}`)
  await expect(page.locator('.batch-resource-order li').nth(2)).toContainText(`Unknown policy account ${runID}`)

  const v1SimulationResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/v1/console/gateway-simulator'
  )
  await page.locator('.simulation-controls').getByRole('button', { name: 'Run simulation' }).click()
  const v1SimulationResponse = await v1SimulationResponsePromise
  expect(v1SimulationResponse.status()).toBe(200)
  const v1Simulation = await v1SimulationResponse.json() as { data: { routing_policy_id: string; routing_policy_version: number; candidates: Array<{ provider_account_id: string; eligible: boolean; reason: string; policy_batch_name: string; selection_reason: string }> } }
  expect(v1Simulation.data.routing_policy_id).toBe(created.data.id)
  expect(v1Simulation.data.routing_policy_version).toBe(1)
  expect(v1Simulation.data.candidates).toEqual(expect.arrayContaining([
    expect.objectContaining({ provider_account_id: fixture.cheap.account.id, eligible: true, reason: '', policy_batch_name: 'Production' }),
    expect.objectContaining({ provider_account_id: fixture.expensive.account.id, eligible: false, reason: 'routing_policy_input_price_exceeded' }),
    expect.objectContaining({ provider_account_id: fixture.unknown.account.id, eligible: false, reason: 'routing_policy_price_fact_missing' })
  ]))
  expect(v1Simulation.data.candidates.find((candidate) => candidate.provider_account_id === fixture.cheap.account.id)?.selection_reason).not.toBe('')
  await expect(page.locator('.simulation-results')).toContainText('Input price cap exceeded')
  await expect(page.locator('.simulation-results')).toContainText('Comparable price fact missing')

  await page.getByLabel('Description').fill(updatedDescription)
  await page.getByRole('button', { name: 'Remove model limit' }).click()
  await page.getByLabel('When price facts are missing').selectOption('allow')
  const updateResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'PUT' && new URL(response.url()).pathname === `/api/v1/console/routing-policies/${created.data.id}`
  )
  await page.getByRole('button', { name: 'Save policy' }).click()
  const updateResponse = await updateResponsePromise
  expect(updateResponse.status()).toBe(200)
  const updated = await updateResponse.json() as { data: { id: string; version: number; description: string; strategy: { preset: string; strict_order: boolean; missing_price_action: string; model_price_limits: unknown[] } } }
  expect(updated.data).toMatchObject({
    id: created.data.id,
    version: 2,
    description: updatedDescription,
    strategy: { preset: 'cost', strict_order: true, missing_price_action: 'allow', model_price_limits: [] }
  })
  await expect(page.getByText('Routing policy updated')).toBeVisible()

  await page.reload()
  const policyRow = page.getByRole('row').filter({ hasText: policyName })
  await expect(policyRow).toContainText(`v2 · ${updatedDescription}`)
  await expect(policyRow).toContainText('Cost first')
  await expect(page.getByLabel('Description')).toHaveValue(updatedDescription)
  await expect(page.getByRole('radio', { name: /Cost first/ })).toHaveAttribute('aria-checked', 'true')
  await expect(page.getByLabel('When price facts are missing')).toHaveValue('allow')
  await expect(page.getByText('No per-model price limits configured.')).toBeVisible()

  const v2SimulationResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/v1/console/gateway-simulator'
  )
  await page.locator('.simulation-controls').getByRole('button', { name: 'Run simulation' }).click()
  const v2Simulation = await v2SimulationResponsePromise.then((response) => response.json()) as { data: { routing_policy_version: number; candidates: Array<{ provider_account_id: string; eligible: boolean; reason: string }> } }
  expect(v2Simulation.data.routing_policy_version).toBe(2)
  expect(v2Simulation.data.candidates.map((candidate) => candidate.provider_account_id)).toEqual([
    fixture.cheap.account.id, fixture.expensive.account.id, fixture.unknown.account.id
  ])
  expect(v2Simulation.data.candidates.every((candidate) => candidate.eligible && candidate.reason === '')).toBe(true)

  await page.getByLabel('Strict declared order').uncheck()
  const preferredUpdatePromise = page.waitForResponse((response) =>
    response.request().method() === 'PUT' && new URL(response.url()).pathname === `/api/v1/console/routing-policies/${created.data.id}`
  )
  await page.getByRole('button', { name: 'Save policy' }).click()
  const preferredUpdate = await preferredUpdatePromise
  expect(preferredUpdate.status()).toBe(200)
  const preferredPolicy = await preferredUpdate.json() as { data: { version: number; strategy: { strict_order: boolean; preferred_provider_account_ids: string[] } } }
  expect(preferredPolicy.data).toMatchObject({ version: 3, strategy: { strict_order: false, preferred_provider_account_ids: [fixture.expensive.account.id] } })

  const v3SimulationResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/v1/console/gateway-simulator'
  )
  await page.locator('.simulation-controls').getByRole('button', { name: 'Run simulation' }).click()
  const v3Simulation = await v3SimulationResponsePromise.then((response) => response.json()) as { data: { routing_policy_version: number; candidates: Array<{ provider_account_id: string }> } }
  expect(v3Simulation.data.routing_policy_version).toBe(3)
  expect(v3Simulation.data.candidates.map((candidate) => candidate.provider_account_id)).toEqual([
    fixture.expensive.account.id, fixture.cheap.account.id, fixture.unknown.account.id
  ])
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('routing-policy-light-en.png'), fullPage: true })

  const keyName = `Policy-bound workspace key ${runID}`
  await page.goto('/console/applications/credentials')
  await page.getByRole('button', { name: 'New workspace key' }).click()
  const keyDialog = page.locator('.api-key-modal')
  await keyDialog.locator('.field').filter({ hasText: 'Name' }).locator('input').first().fill(keyName)
  const modelPicker = keyDialog.getByRole('group', { name: 'Model allowlist' })
  await expect(modelPicker.getByRole('button', { name: fixture.publicModel, exact: true })).toBeVisible()
  const modelOptions = modelPicker.getByRole('button')
  for (let index = 0; index < await modelOptions.count(); index += 1) {
    const option = modelOptions.nth(index)
    const modelID = (await option.textContent())?.trim() || ''
    const shouldSelect = modelID === fixture.publicModel
    const isSelected = await option.getAttribute('aria-pressed') === 'true'
    if (shouldSelect !== isSelected) await option.click()
  }
  const routingPolicySelect = keyDialog.locator('.field').filter({ hasText: 'Routing policy' }).locator('select')
  await expect(routingPolicySelect.locator('option')).toContainText([policyName])
  await routingPolicySelect.selectOption(created.data.id)
  const createKeyResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/v1/console/api-keys'
  )
  await keyDialog.getByRole('button', { name: 'Save', exact: true }).click()
  const createKeyResponse = await createKeyResponsePromise
  expect(createKeyResponse.status()).toBe(200)
  const createdKey = await createKeyResponse.json() as { data: { record: { id: string; routing_policy_id: string; model_allowlist: string[] } } }
  expect(createdKey.data.record).toMatchObject({
    routing_policy_id: created.data.id,
    model_allowlist: [fixture.publicModel]
  })
  await expect(page.getByText('API key created')).toBeVisible()
  let keyRow = page.getByRole('row').filter({ hasText: keyName })
  await expect(keyRow).toContainText(policyName)
  await expect(keyRow).toContainText(fixture.publicModel)
  await page.reload()
  keyRow = page.getByRole('row').filter({ hasText: keyName })
  await expect(keyRow).toContainText(policyName)
  await keyRow.getByRole('button', { name: 'Edit', exact: true }).click()
  await expect(page.locator('.api-key-modal').locator('.field').filter({ hasText: 'Routing policy' }).locator('select')).toHaveValue(created.data.id)

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
    routing_policy_version: 3,
    routing_policy_preset: 'cost'
  })
  expect(simulation.data.candidates).toEqual(expect.arrayContaining([
    expect.objectContaining({ provider_account_id: fixture.cheap.account.id, eligible: true, reason: '' }),
    expect.objectContaining({ provider_account_id: fixture.expensive.account.id, eligible: true, reason: '' }),
    expect.objectContaining({ provider_account_id: fixture.unknown.account.id, eligible: true, reason: '' })
  ]))
  await expect(page.locator('.simulation-flow')).toContainText('Cost first · v3')
  await expect(page.getByRole('table')).toContainText(fixture.cheap.upstreamModel)
  await expect(page.getByRole('table')).toContainText(fixture.unknown.upstreamModel)
  await page.reload()
  await page.getByLabel('Requested model').selectOption(fixture.publicModel)
  await page.getByRole('button', { name: 'Run simulation' }).click()
  await expect(page.locator('.simulation-flow')).toContainText('Cost first · v3')
  await expectNoHorizontalOverflow(page)

  await page.getByLabel('Language').selectOption('zh-CN')
  const themeButton = page.locator('.global-theme-toggle')
  await expect(themeButton).toBeVisible()
  await themeButton.click()
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await expect(page.getByRole('heading', { level: 1, name: '路由模拟器' })).toBeVisible()
  await expect(page.locator('.simulation-flow')).toContainText('成本优先 · v3')
  await expect(page.getByRole('table')).toContainText('可调度')
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('routing-policy-dark-zh.png'), fullPage: true })
  if (testInfo.project.name === 'chromium-desktop') {
    const results = await new AxeBuilder({ page }).analyze()
    const blocking = results.violations.filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
    expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([])
  }
  expect(errors).toEqual([])
})

test('@e2e-routing-policy-002 preferences defaults lifecycle and rejected conflicts stay visible', async ({ page }, testInfo) => {
  const errors = captureBrowserErrors(page)
  await loginDemo(page)
  const token = await loginTestPrincipal(page)
  const runID = `${testInfo.project.name}-preferences-${Date.now()}`.replace(/[^a-z0-9-]+/gi, '-').toLowerCase()
  const fixture = await createRoutingPolicyFixture(page, token, runID)
  await page.goto('/console/policies/routing')

  await page.getByRole('button', { name: 'New policy' }).click()
  await page.getByLabel('Policy name').fill('')
  await page.getByLabel('Route group').fill('')
  await page.getByRole('button', { name: 'Save policy' }).click()
  await expect(page.getByText('Enter a policy name and route group.')).toBeVisible()

  const policyName = `Default interactive routing ${runID}`
  await page.getByLabel('Policy name').fill(policyName)
  await page.getByLabel('Route group').fill(fixture.routeGroup)
  await page.locator('.default-policy-field input').check()
  await page.getByRole('radio', { name: /Speed first/ }).click()
  await expect(page.locator('.decision-preview')).toContainText('Speed first')
  const created = await savePolicyThroughUI(page, 'POST')
  expect(created).toMatchObject({
    version: 1,
    status: 'active',
    is_default: true,
    strategy: { preset: 'speed' }
  })

  await page.reload()
  let policyRow = page.getByRole('row').filter({ hasText: policyName })
  await expect(policyRow).toContainText('Speed first')
  await expect(policyRow).toContainText('Route-group default')
  await page.getByRole('radio', { name: /Stability first/ }).click()
  await expect(page.locator('.decision-preview')).toContainText('Stability first')
  const stability = await savePolicyThroughUI(page, 'PUT', created.id)
  expect(stability).toMatchObject({ version: 2, is_default: true, strategy: { preset: 'stability' } })

  await page.getByRole('radio', { name: /Balanced/ }).click()
  await page.getByLabel('Status').selectOption('disabled')
  const disabled = await savePolicyThroughUI(page, 'PUT', created.id)
  expect(disabled).toMatchObject({ version: 3, status: 'disabled', is_default: true, strategy: { preset: 'balanced' } })

  await page.reload()
  policyRow = page.getByRole('row').filter({ hasText: policyName })
  await expect(policyRow).toContainText('Balanced')
  await expect(policyRow).toContainText('Disabled')
  await expect(page.locator('.simulation-controls').getByRole('button', { name: 'Run simulation' })).toBeDisabled()
  await policyRow.focus()
  await policyRow.press('Enter')
  await expect(page.getByLabel('Policy name')).toHaveValue(policyName)

  await page.getByLabel('Status').selectOption('active')
  const reenabled = await savePolicyThroughUI(page, 'PUT', created.id)
  expect(reenabled).toMatchObject({ version: 4, status: 'active', is_default: true, strategy: { preset: 'balanced' } })

  const duplicateName = `Conflicting default routing ${runID}`
  await page.getByRole('button', { name: 'New policy' }).click()
  await page.getByLabel('Policy name').fill(duplicateName)
  await page.getByLabel('Route group').fill(fixture.routeGroup)
  await page.locator('.default-policy-field input').check()
  const conflictResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/v1/console/routing-policies'
  )
  await page.getByRole('button', { name: 'Save policy' }).click()
  const conflictResponse = await conflictResponsePromise
  expect(conflictResponse.status()).toBe(400)
  await expect(page.locator('.notice').filter({ hasText: 'already has a default routing policy' })).toBeVisible()
  await expect(page.getByLabel('Policy name')).toHaveValue(duplicateName)

  await page.getByRole('button', { name: 'Refresh' }).click()
  policyRow = page.getByRole('row').filter({ hasText: policyName })
  await expect(policyRow).toContainText('Balanced')
  if ((page.viewportSize()?.width || 0) <= 640) {
    await policyRow.locator('td').first().screenshot({ path: testInfo.outputPath('routing-policy-list-item-en.png') })
    await page.getByRole('radio', { name: /Balanced/ }).screenshot({ path: testInfo.outputPath('routing-policy-preference-balanced-en.png') })
  } else {
    await page.locator('.policy-list-section').screenshot({ path: testInfo.outputPath('routing-policy-list-en.png') })
    await routingPolicySection(page, 'Choose a decision preference').screenshot({ path: testInfo.outputPath('routing-policy-preferences-en.png') })
  }
  await expectNoHorizontalOverflow(page)
  if (testInfo.project.name === 'chromium-desktop') {
    const results = await new AxeBuilder({ page }).analyze()
    const blocking = results.violations.filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
    expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([])
  }
  expect(errors.some((entry) => entry.includes('status of 400 (Bad Request)'))).toBe(true)
  expect(errors.filter((entry) => !entry.includes('status of 400 (Bad Request)'))).toEqual([])
})

test('@e2e-routing-policy-003 hard constraints ordered batches and resilience controls round-trip', async ({ page }, testInfo) => {
	test.setTimeout(60_000)
	const errors = captureBrowserErrors(page)
  await loginDemo(page)
  const token = await loginTestPrincipal(page)
  const runID = `${testInfo.project.name}-guardrails-${Date.now()}`.replace(/[^a-z0-9-]+/gi, '-').toLowerCase()
  const fixture = await createRoutingPolicyFixture(page, token, runID)
  await page.goto('/console/policies/routing')

  const policyName = `Governed fallback routing ${runID}`
  await page.getByRole('button', { name: 'New policy' }).click()
  await page.getByLabel('Policy name').fill(policyName)
  await page.getByLabel('Route group').fill(fixture.routeGroup)
  await page.getByLabel('Native protocol only').check()
  await page.getByLabel('Maximum input price').fill('0.4')
  await page.getByLabel('Maximum output price').fill('0.6')
  await page.getByLabel('Maximum cheapest-price multiple').fill('1.5')
  await page.getByLabel('When price facts are missing').selectOption('block')
  await page.getByLabel('Pool mode').selectOption('auto')
  await page.getByLabel('Minimum candidates').fill('3')
  await page.getByLabel('Allowed models').fill(`${fixture.publicModel}\n${fixture.publicModel}`)
  await page.getByLabel('Denied models').fill('blocked-enterprise-model')
  await page.locator('.protocol-rule-row').filter({ hasText: 'OpenAI Chat Completions' }).getByRole('combobox').selectOption('deny')
  await page.locator('.protocol-rule-row').filter({ hasText: 'OpenAI Responses' }).getByRole('combobox').selectOption('allow')

  await page.getByRole('button', { name: 'Add batch' }).click()
  await page.getByRole('button', { name: 'Add batch' }).click()
  let batches = page.locator('.batch-row')
  await batches.nth(0).getByLabel('Batch name').fill('Primary production')
  await batches.nth(1).getByLabel('Batch name').fill('Continuity fallback')
  await batches.nth(0).getByRole('button', { name: new RegExp(`Cheap policy account ${runID}`) }).click()
  await batches.nth(0).getByRole('button', { name: new RegExp(`Priority policy account ${runID}`) }).click()
  await batches.nth(1).getByRole('button', { name: new RegExp(`Unknown policy account ${runID}`) }).click()
  await batches.nth(0).getByRole('button', { name: 'Move resource up' }).last().click()
  await page.locator('.preferred-picker').getByRole('button', { name: new RegExp(`Priority policy account ${runID}`) }).click()
  await batches.nth(0).getByRole('button', { name: 'Move batch down' }).click()

  await page.getByLabel('Enable pre-first-byte failover').uncheck()
  await page.getByLabel('Sticky TTL').fill('1800')
  await page.getByLabel('Effective-cost optimization').uncheck()
  await page.getByLabel('Strict declared order').check()
  const created = await savePolicyThroughUI(page, 'POST')
  expect(created).toMatchObject({
    version: 1,
    strategy: {
      preset: 'balanced',
      native_protocol_only: true,
      absolute_max_input_per_1m: 0.4,
      absolute_max_output_per_1m: 0.6,
      max_price_multiple_of_cheapest: 1.5,
      low_price_pool_mode: 'auto',
      low_price_pool_min_candidates: 3,
      allowed_models: [fixture.publicModel],
      denied_models: ['blocked-enterprise-model'],
      allowed_protocols: ['openai_responses'],
      denied_protocols: ['openai_chat_completions'],
      resource_batches: [
        { name: 'Continuity fallback', provider_account_ids: [fixture.unknown.account.id] },
        { name: 'Primary production', provider_account_ids: [fixture.expensive.account.id, fixture.cheap.account.id] }
      ],
      preferred_provider_account_ids: [fixture.expensive.account.id],
      failover_before_first_byte: false,
      sticky_routing: true,
      sticky_ttl_seconds: 1800,
      smart_optimization: false,
      strict_order: true
    }
  })

  await page.reload()
  await expect(page.getByLabel('Native protocol only')).toBeChecked()
  await expect(page.getByLabel('Pool mode')).toHaveValue('auto')
  await expect(page.getByLabel('Minimum candidates')).toHaveValue('3')
  await expect(page.getByLabel('Sticky TTL')).toHaveValue('1800')
  batches = page.locator('.batch-row')
  await expect(batches.nth(0).getByLabel('Batch name')).toHaveValue('Continuity fallback')
  await expect(batches.nth(1).locator('.batch-resource-order li').nth(0)).toContainText(`Priority policy account ${runID}`)
  await batches.nth(1).locator('.batch-resource-order').screenshot({ path: testInfo.outputPath('routing-policy-batch-order-en.png') })

  await page.getByLabel('Pool mode').selectOption('percentile')
  await page.getByLabel('Price percentile retained (%)').fill('40')
  await page.getByLabel('Minimum candidates').fill('1')
  await batches.nth(1).getByRole('button', { name: 'Move resource down' }).first().click()
  await batches.nth(1).getByRole('button', { name: 'Remove resource' }).last().click()
  await expect(page.locator('.preferred-picker').getByRole('button', { name: new RegExp(`Priority policy account ${runID}`) })).toHaveCount(0)
  const percentile = await savePolicyThroughUI(page, 'PUT', created.id)
  expect(percentile).toMatchObject({
    version: 2,
    strategy: {
      low_price_pool_mode: 'percentile',
      low_price_pool_percent: 40,
      low_price_pool_min_candidates: 1,
      resource_batches: [
        { name: 'Continuity fallback', provider_account_ids: [fixture.unknown.account.id] },
        { name: 'Primary production', provider_account_ids: [fixture.cheap.account.id] }
      ],
      preferred_provider_account_ids: []
    }
  })

  await page.reload()
  await expect(page.getByLabel('Pool mode')).toHaveValue('percentile')
  await expect(page.getByLabel('Price percentile retained (%)')).toHaveValue('40')
  batches = page.locator('.batch-row')
  await batches.nth(0).getByRole('button', { name: 'Remove batch' }).click()
  await page.getByLabel('Pool mode').selectOption('strict')
  const strict = await savePolicyThroughUI(page, 'PUT', created.id)
  expect(strict).toMatchObject({
    version: 3,
    strategy: {
      low_price_pool_mode: 'strict',
      low_price_pool_percent: 30,
      low_price_pool_min_candidates: 2,
      resource_batches: [{ name: 'Primary production', provider_account_ids: [fixture.cheap.account.id] }]
    }
  })

  await page.reload()
  await expect(page.getByLabel('Pool mode')).toHaveValue('strict')
  await expect(page.locator('.batch-row')).toHaveCount(1)
  await expect(page.getByLabel('Enable pre-first-byte failover')).not.toBeChecked()
  await expect(page.getByLabel('Effective-cost optimization')).not.toBeChecked()
  await expect(page.getByLabel('Strict declared order')).toBeChecked()
  await expectNoHorizontalOverflow(page)

  await page.getByLabel('Language').selectOption('zh-CN')
  const themeButton = page.locator('.global-theme-toggle')
  await expect(themeButton).toBeVisible()
  await themeButton.click()
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await expect(page.getByRole('heading', { level: 1, name: '路由策略' })).toBeVisible()
  await page.locator('.rule-inputs').screenshot({ path: testInfo.outputPath('routing-policy-price-guardrails-dark-zh.png') })
  await page.locator('.protocol-rule-row').filter({ hasText: 'OpenAI Chat Completions' }).screenshot({ path: testInfo.outputPath('routing-policy-protocol-deny-dark-zh.png') })
  await page.locator('.protocol-rule-row').filter({ hasText: 'OpenAI Responses' }).screenshot({ path: testInfo.outputPath('routing-policy-protocol-allow-dark-zh.png') })
  await expectNoHorizontalOverflow(page)
  expect(errors).toEqual([])
})
