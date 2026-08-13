import { expect, test } from '@playwright/test'
import { adminPost, captureBrowserErrors, createGatewayFixture, envelope, expectNoHorizontalOverflow, loginDemo, loginTestPrincipal } from './fixtures'

type PricingRuleResponse = {
  data: {
    rule: {
      id: string
      status: 'active' | 'disabled'
      active_version_id?: string
      lock_version: number
    }
    active_version?: { id: string; revision: number; expression: string }
    draft?: { id: string; revision: number; expression: string }
  }
}

test('@e2e-pricing-001 expression pricing lifecycle remains usable across viewports', async ({ page }, testInfo) => {
  const errors = captureBrowserErrors(page)
  await loginDemo(page)
  const token = await loginTestPrincipal(page)
  const suffix = `${testInfo.project.name.replace(/[^a-z0-9]+/gi, '-').toLowerCase()}-${Date.now().toString(36)}`
  const model = `pricing-e2e-${suffix}`
  const ruleName = `Browser cost ${suffix}`
  await createGatewayFixture(page, token, suffix, model)
  const workspaceKey = await adminPost<{ key: string; record: { id: string } }>(page, token, '/api-keys', {
    name: `Pricing E2E Key ${suffix}`,
    model_allowlist: [model],
    qps_limit: 10,
    monthly_token_limit: 100000
  })

  await page.goto('/console/model-services/pricing')
  await expect(page).toHaveURL(/\/console\/model-services\/pricing$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Expression Pricing' })).toBeVisible()
  await page.getByRole('button', { name: 'New rule' }).click()
  const dialog = page.getByRole('dialog', { name: 'New rule' })
  await dialog.getByLabel('Rule name').fill(ruleName)
  await dialog.getByLabel('Model').fill(model)
  await dialog.getByLabel('v1 expression').fill('v1: fixed_line("request", "request", 125)')
  const createResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/v1/console/pricing-rules'
  )
  await dialog.getByRole('button', { name: 'Create rule' }).click()
  const createResponse = await createResponsePromise
  expect(createResponse.status()).toBe(200)
  const created = await createResponse.json() as PricingRuleResponse
  expect(created.data.rule.id).not.toBe('')
  expect(created.data.draft).toMatchObject({ revision: 0, expression: 'v1: fixed_line("request", "request", 125)' })

  await expect(page.getByText('Rule created')).toBeVisible()
  await expect(page.getByRole('heading', { level: 2, name: ruleName })).toBeVisible()
  await page.getByRole('button', { name: 'Validate' }).click()
  await expect(page.getByText('Validation passed').first()).toBeVisible()

  await page.getByRole('button', { name: 'Simulation' }).click()
  await page.getByRole('button', { name: 'Run simulation' }).click()
  await expect(page.getByText('$0.000125').first()).toBeVisible()

  await page.getByRole('button', { name: 'Rule editor' }).click()
  const firstPublishResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'POST' && new URL(response.url()).pathname === `/api/v1/console/pricing-rules/${created.data.rule.id}/publish`
  )
  await page.getByRole('button', { name: 'Publish' }).click()
  const firstPublishResponse = await firstPublishResponsePromise
  expect(firstPublishResponse.status()).toBe(200)
  const firstPublished = await firstPublishResponse.json() as PricingRuleResponse
  expect(firstPublished.data.rule.active_version_id).toBe(firstPublished.data.active_version?.id)
  expect(firstPublished.data.active_version).toMatchObject({ revision: 1, expression: 'v1: fixed_line("request", "request", 125)' })
  await expect(page.getByText('Version published and activated')).toBeVisible()

  const secondExpression = 'v1: fixed_line("request", "request", 250)'
  await page.getByLabel('v1 expression').fill(secondExpression)
  const secondDraftResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'PUT' && new URL(response.url()).pathname === `/api/v1/console/pricing-rules/${created.data.rule.id}/draft`
  )
  await page.getByRole('button', { name: 'Save draft' }).click()
  const secondDraftResponse = await secondDraftResponsePromise
  expect(secondDraftResponse.status()).toBe(200)
  const secondDraft = await secondDraftResponse.json() as PricingRuleResponse
  expect(secondDraft.data.rule.active_version_id).toBe(firstPublished.data.rule.active_version_id)
  expect(secondDraft.data.draft).toMatchObject({ revision: 0, expression: secondExpression })
  expect(secondDraft.data.draft?.id).not.toBe(firstPublished.data.active_version?.id)
  await expect(page.getByText('Draft saved')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Publish' })).toBeEnabled()

  const secondPublishResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'POST' && new URL(response.url()).pathname === `/api/v1/console/pricing-rules/${created.data.rule.id}/publish`
  )
  await page.getByRole('button', { name: 'Publish' }).click()
  const secondPublishResponse = await secondPublishResponsePromise
  expect(secondPublishResponse.status()).toBe(200)
  const secondPublished = await secondPublishResponse.json() as PricingRuleResponse
  expect(secondPublished.data.rule.active_version_id).toBe(secondPublished.data.active_version?.id)
  expect(secondPublished.data.rule.active_version_id).not.toBe(firstPublished.data.rule.active_version_id)
  expect(secondPublished.data.active_version).toMatchObject({ revision: 2, expression: secondExpression })
  await expect(page.getByText('Version published and activated')).toBeVisible()

  await page.getByRole('button', { name: 'Version history' }).click()
  let firstVersionRow = page.getByRole('row').filter({ hasText: '#1' })
  let secondVersionRow = page.getByRole('row').filter({ hasText: '#2' })
  await expect(firstVersionRow.getByRole('button', { name: 'Activate' })).toBeVisible()
  await expect(secondVersionRow.getByText('Active version')).toBeVisible()

  page.once('dialog', (confirmation) => confirmation.accept())
  const activateResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'POST' && new URL(response.url()).pathname === `/api/v1/console/pricing-rules/${created.data.rule.id}/activate/${firstPublished.data.active_version!.id}`
  )
  await firstVersionRow.getByRole('button', { name: 'Activate' }).click()
  const activateResponse = await activateResponsePromise
  expect(activateResponse.status()).toBe(200)
  expect(await activateResponse.json()).toMatchObject({ data: { status: 'active' } })
  await expect(page.getByText('Version activated')).toBeVisible()
  firstVersionRow = page.getByRole('row').filter({ hasText: '#1' })
  secondVersionRow = page.getByRole('row').filter({ hasText: '#2' })
  await expect(firstVersionRow.getByText('Active version')).toBeVisible()
  await expect(secondVersionRow.getByRole('button', { name: 'Activate' })).toBeVisible()

  await page.reload()
  await page.locator('.pricing-rule-list').getByRole('button').filter({ hasText: ruleName }).click()
  await page.getByRole('button', { name: 'Version history' }).click()
  await expect(page.getByRole('row').filter({ hasText: '#1' }).getByText('Active version')).toBeVisible()

  const completion = await page.request.post('/v1/chat/completions', {
    data: { model, messages: [{ role: 'user', content: 'synthetic pricing evaluation request' }] },
    headers: { Authorization: `Bearer ${workspaceKey.key}` }
  })
  expect(completion.status()).toBe(200)
  await expect(completion.json()).resolves.toMatchObject({
    id: 'e2e-completion',
    usage: { prompt_tokens: 7, completion_tokens: 11 }
  })

  let pricingEvaluationID = ''
  await expect.poll(async () => {
    const usage = await envelope<{ recent: Array<Record<string, unknown>> }>(await page.request.get(
      `/api/v1/console/usage?api_key_id=${encodeURIComponent(workspaceKey.record.id)}&model=${encodeURIComponent(model)}&limit=10`,
      { headers: { Authorization: `Bearer ${token}` } }
    ))
    const record = usage.recent.find((item) => item.api_key_id === workspaceKey.record.id && item.model === model && item.status === 'forwarded')
    pricingEvaluationID = String(record?.usage_pricing_evaluation_id || '')
    return record
  }, { message: 'priced usage record for the real gateway request' }).toMatchObject({
    input_tokens: 7,
    output_tokens: 11,
    usage_cost_micros: 125,
    usage_cost_currency: 'USD',
    pricing_status: 'priced'
  })
  expect(pricingEvaluationID).not.toBe('')

  await page.getByRole('button', { name: 'Evaluation evidence' }).click()
  await page.getByLabel('Evaluation ID').fill(pricingEvaluationID)
  const evaluationResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'GET' && new URL(response.url()).pathname === `/api/v1/console/pricing-evaluations/${pricingEvaluationID}`
  )
  await page.getByRole('button', { name: 'Look up' }).click()
  const evaluationResponse = await evaluationResponsePromise
  expect(evaluationResponse.status()).toBe(200)
  const evaluationBody = await evaluationResponse.json() as {
    data: {
      id: string
      phase: string
      pricing_rule_id: string
      pricing_rule_version_id: string
      amount_micros: number
      currency: string
      status: string
      facts: { total_input_tokens: number; output_tokens: number; normalization_status: string }
    }
  }
  expect(evaluationBody.data).toMatchObject({
    id: pricingEvaluationID,
    phase: 'settlement',
    pricing_rule_id: created.data.rule.id,
    pricing_rule_version_id: firstPublished.data.active_version!.id,
    amount_micros: 125,
    currency: 'USD',
    status: 'succeeded',
    facts: { total_input_tokens: 7, output_tokens: 11 }
  })
  const evaluationResult = page.locator('.evaluation-result')
  await expect(evaluationResult.getByText('succeeded', { exact: true })).toBeVisible()
  await expect(evaluationResult.getByText('settlement', { exact: true })).toBeVisible()
  await expect(evaluationResult.getByText('$0.000125', { exact: true })).toBeVisible()
  await expect(evaluationResult.locator('pre')).toContainText('"total_input_tokens": 7')
  await expect(evaluationResult.locator('pre')).toContainText('"output_tokens": 11')

  await page.getByRole('button', { name: 'Rule editor' }).click()
  page.once('dialog', (confirmation) => confirmation.accept())
  const disableResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'POST' && new URL(response.url()).pathname === `/api/v1/console/pricing-rules/${created.data.rule.id}/disable`
  )
  await page.getByRole('button', { name: 'Disable rule' }).click()
  const disableResponse = await disableResponsePromise
  expect(disableResponse.status()).toBe(200)
  expect(await disableResponse.json()).toMatchObject({ data: { status: 'disabled' } })
  await expect(page.getByText('Rule disabled')).toBeVisible()

  await page.reload()
  await page.locator('.pricing-rule-list').getByRole('button').filter({ hasText: ruleName }).click()
  await expect(page.getByRole('heading', { level: 2, name: ruleName })).toBeVisible()
  await expect(page.locator('.pricing-workspace-header').getByText('Disabled', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Disable rule' })).toHaveCount(0)

  await expectNoHorizontalOverflow(page)
  await page.getByLabel('Language').selectOption('zh-CN')
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')
  if ((page.viewportSize()?.width || 0) <= 640) {
    await page.getByRole('button', { name: '打开导航' }).click()
  }
  const themeButton = page.getByRole('button', { name: /深色模式|浅色模式/ })
  await themeButton.click()
  if ((page.viewportSize()?.width || 0) <= 640) {
    await page.getByRole('button', { name: '关闭导航' }).first().click()
    await expect(page.locator('.admin-sidebar')).not.toHaveClass(/mobile-open/)
    await expect(page.locator('.sidebar-overlay')).toHaveCount(0)
  }
  await expectNoHorizontalOverflow(page)
  await page.evaluate(() => window.scrollTo(0, 0))
  await page.screenshot({ path: testInfo.outputPath(`pricing-${suffix}.png`), fullPage: true })
  expect(errors).toEqual([])
})
