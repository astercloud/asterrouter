import { expect, test } from '@playwright/test'
import { adminPost, captureBrowserErrors, controlAPI, envelope, expectNoHorizontalOverflow, loginDemo } from './fixtures'

function modelPaths(): { providers: string; accounts: string; routes: string } {
  switch (process.env.ASTER_E2E_EXPECT_PROFILE) {
    case 'enterprise':
      return { providers: '/admin/providers', accounts: '/admin/provider-accounts', routes: '/admin/model-routes' }
    case 'relay_operator':
      return { providers: '/operator/providers', accounts: '/operator/resources', routes: '/operator/model-routes' }
    default:
      return { providers: '/console/providers', accounts: '/console/resources', routes: '/console/model-routes' }
  }
}

test('new provider account persists empty before automatic discovery and explicit apply', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'Lifecycle is covered once; the responsive inventory flow is covered separately.')

  const browserErrors = captureBrowserErrors(page)
  await loginDemo(page)
  const token = await page.evaluate(() => localStorage.getItem('asterrouter_admin_token') || '')
  const runID = `${testInfo.project.name}-${Date.now()}`
  const upstreamPort = process.env.ASTER_E2E_UPSTREAM_PORT || '19000'
  const provider = await adminPost<{ id: string }>(page, token, '/providers', {
    name: `Empty inventory provider ${runID}`,
    type: 'openai_compatible',
    base_url: `http://127.0.0.1:${upstreamPort}/v1`,
    status: 'active',
    priority: 10,
    api_key: 'synthetic-provider-secret'
  })

  await page.goto(modelPaths().accounts)
  await page.getByRole('button', { name: 'New route resource' }).click()
  const createDialog = page.getByRole('dialog', { name: 'New route resource' })
  await createDialog.locator('.field').filter({ hasText: 'Provider connection' }).getByRole('combobox').selectOption(provider.id)
  await createDialog.locator('.field').filter({ hasText: 'Resource name' }).getByRole('textbox').fill(`Empty inventory account ${runID}`)
  await createDialog.locator('.field').filter({ hasText: 'Resource credential' }).getByRole('textbox').fill('synthetic-account-secret')
  await createDialog.getByRole('button', { name: 'Save' }).click()

  const editDialog = page.getByRole('dialog', { name: 'Edit route resource' })
  await expect(editDialog).toBeVisible()
  await expect(editDialog.getByText(/Discovery complete; the upstream currently reports 1 available models/)).toBeVisible()
  await expect(editDialog.getByText('upstream-model', { exact: true })).toBeVisible()

  const headers = { Authorization: `Bearer ${token}` }
  const accountsBeforeApply = await envelope<Array<{ id: string; name: string; models: string[] }>>(
    await page.request.get(controlAPI('/provider-accounts'), { headers })
  )
  const created = accountsBeforeApply.find((account) => account.name === `Empty inventory account ${runID}`)
  expect(created).toBeDefined()
  expect(created?.models).toEqual([])

  await editDialog.getByLabel('Toggle model upstream-model').check()
  await editDialog.getByRole('button', { name: 'Discover and apply' }).click()
  await expect(editDialog.getByText('Synchronized 1 enabled upstream models')).toBeVisible()

  const accountsAfterApply = await envelope<Array<{ id: string; name: string; models: string[] }>>(
    await page.request.get(controlAPI('/provider-accounts'), { headers })
  )
  expect(accountsAfterApply.find((account) => account.id === created?.id)?.models).toEqual(['upstream-model'])
  expect(browserErrors).toEqual([])
})

test('model inventory and bulk routes stay auditable across responsive surfaces', async ({ page }, testInfo) => {
  const browserErrors = captureBrowserErrors(page)
  await loginDemo(page)
  const paths = modelPaths()
  await page.goto(paths.providers)
  await page.getByRole('button', { name: 'New provider' }).click()
  const providerDialog = page.getByRole('dialog', { name: 'New provider connection' })
  await expect(providerDialog).toBeVisible()
  await expect(providerDialog.getByText('Recommended models')).toHaveCount(0)
  await expect(providerDialog.locator('.provider-model-section')).toHaveCount(0)
  await expectNoHorizontalOverflow(page)
  await providerDialog.getByRole('button', { name: 'Close' }).click()

  const token = await page.evaluate(() => localStorage.getItem('asterrouter_admin_token') || '')
  const runID = `${testInfo.project.name}-${Date.now()}`
  const upstreamPort = process.env.ASTER_E2E_UPSTREAM_PORT || '19000'
  const exactUpstream = `exact-upstream-${runID}`
  const manualUpstream = `manual-upstream-${runID}`

  const provider = await adminPost<{ id: string }>(page, token, '/providers', {
    name: `Model inventory provider ${runID}`,
    type: 'openai_compatible',
    base_url: `http://127.0.0.1:${upstreamPort}/v1`,
    status: 'active',
    models: [exactUpstream, manualUpstream],
    priority: 10,
    api_key: 'synthetic-provider-secret'
  })
  const account = await adminPost<{ id: string }>(page, token, '/provider-accounts', {
    provider_id: provider.id,
    name: `Model inventory account ${runID}`,
    platform: 'openai_compatible',
    auth_type: 'api_key',
    status: 'active',
    schedulable: true,
    priority: 10,
    weight: 100,
    concurrency: 2,
    rpm_limit: 0,
    tpm_limit: 0,
    rate_multiplier: 1,
    models: [exactUpstream, manualUpstream],
    auto_enable_new_models: false,
    group_ids: [],
    secret: 'synthetic-account-secret'
  })
  const exactModel = await adminPost<{ id: string }>(page, token, '/gateway-models', {
    model_id: exactUpstream,
    name: `Exact model ${runID}`,
    description: 'Exact model inventory E2E match',
    modality: 'chat',
    default_route_group: 'default',
    status: 'active'
  })
  const manualModel = await adminPost<{ id: string }>(page, token, '/gateway-models', {
    model_id: `manual-public-${runID}`,
    name: `Manual model ${runID}`,
    description: 'Manual model inventory E2E match',
    modality: 'chat',
    default_route_group: 'default',
    status: 'active'
  })
  await adminPost(page, token, '/model-routes', {
    gateway_model_id: exactModel.id,
    route_group: 'default',
    provider_account_id: account.id,
    upstream_model: exactUpstream,
    priority: 10,
    weight: 100,
    status: 'active'
  })

  await page.goto(paths.accounts)
  const accountRow = page.getByRole('row').filter({ hasText: `Model inventory account ${runID}` })
  await expect(accountRow).toBeVisible()
  await accountRow.getByRole('button', { name: 'Edit' }).click()
  const accountDialog = page.getByRole('dialog', { name: 'Edit route resource' })
  await expect(accountDialog).toBeVisible()
  await expect(accountDialog.getByText('Upstream model inventory')).toBeVisible()
  await accountDialog.getByRole('button', { name: 'Discover models' }).click()
  await expect(accountDialog.getByText(/Discovery complete/)).toBeVisible()
  await accountDialog.getByPlaceholder('Search upstream models').fill(manualUpstream)
  await expect(accountDialog.getByText(manualUpstream, { exact: true })).toBeVisible()

  const dialogBox = await accountDialog.boundingBox()
  const viewport = page.viewportSize()
  expect(dialogBox).not.toBeNull()
  expect(viewport).not.toBeNull()
  expect(dialogBox!.x).toBeGreaterThanOrEqual(0)
  expect(dialogBox!.x + dialogBox!.width).toBeLessThanOrEqual(viewport!.width + 1)
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('model-inventory-light-en.png'), fullPage: true })

  await page.getByLabel('Language').selectOption('zh-CN')
  const zhAccountDialog = page.getByRole('dialog', { name: '编辑路由资源' })
  await expect(zhAccountDialog.getByText('上游模型库存')).toBeVisible()
  await zhAccountDialog.getByRole('button', { name: '关闭' }).click()
  if (viewport!.width <= 920) {
    await page.evaluate(() => {
      document.documentElement.dataset.theme = 'dark'
      localStorage.setItem('asterrouter_theme', 'dark')
    })
  } else {
    await page.getByRole('button', { name: '深色模式' }).click()
  }
  expect(await page.locator('html').getAttribute('data-theme')).toBe('dark')
  const zhAccountRow = page.getByRole('row').filter({ hasText: `Model inventory account ${runID}` })
  await zhAccountRow.getByRole('button', { name: '编辑' }).click()
  const darkAccountDialog = page.getByRole('dialog', { name: '编辑路由资源' })
  await expect(darkAccountDialog.getByText('上游模型库存')).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('model-inventory-dark-zh.png'), fullPage: true })
  await darkAccountDialog.getByRole('button', { name: '关闭' }).click()

  await page.getByLabel('语言').selectOption('en-US')
  await page.goto(paths.routes)
  await page.getByRole('button', { name: 'Bulk match models' }).click()
  const routeDialog = page.getByRole('dialog', { name: 'Bulk match models' })
  await routeDialog.getByLabel('Provider account').selectOption(account.id)
  await expect(routeDialog.getByText('Route exists')).toBeVisible()
  const manualMapping = routeDialog.getByLabel(`Gateway model for upstream model ${manualUpstream}`)
  await manualMapping.selectOption(manualModel.id)
  await expect(routeDialog.getByRole('button', { name: 'Create 1 routes' })).toBeEnabled()
  await routeDialog.locator('.bulk-route-table-wrap').evaluate((element) => {
    element.scrollTop = 0
    element.scrollLeft = 0
  })
  await expect(routeDialog.getByText(exactUpstream, { exact: true })).toBeVisible()
  await expect(routeDialog.getByText(manualUpstream, { exact: true })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('bulk-model-routes.png'), fullPage: true })
  await routeDialog.getByRole('button', { name: 'Create 1 routes' }).click()
  await expect(page.getByText('Created 1 model routes')).toBeVisible()
  await expectNoHorizontalOverflow(page)
  expect(browserErrors).toEqual([])
})
