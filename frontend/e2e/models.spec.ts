import { expect, test, type Locator } from '@playwright/test'
import { adminPost, captureBrowserErrors, controlAPI, envelope, expectNoHorizontalOverflow, loginDemo, loginTestPrincipal } from './fixtures'

const modelPaths = {
  providers: '/console/model-services/providers',
  accounts: '/console/model-services/accounts',
  routes: '/console/model-services/routes'
}

function fieldControl(container: Locator, label: string, control = 'input'): Locator {
  return container.locator('.field').filter({ hasText: label }).locator(control).first()
}

test('@e2e-model-account-001 new provider account persists empty before automatic discovery and explicit apply', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'Lifecycle is covered once; the responsive inventory flow is covered separately.')

  const browserErrors = captureBrowserErrors(page)
  await loginDemo(page)
  const token = await loginTestPrincipal(page)
  const runID = `${testInfo.project.name}-${Date.now()}`
  const upstreamPort = process.env.ASTER_E2E_UPSTREAM_PORT || '19000'
  const provider = await adminPost<{ id: string }>(page, token, '/providers', {
    name: `Empty inventory provider ${runID}`,
    type: 'openai_compatible',
    base_url: `http://127.0.0.1:${upstreamPort}/v1`,
    status: 'active',
    priority: 10
  })

  await page.goto(modelPaths.accounts)
  await page.getByRole('button', { name: 'New route resource' }).click()
  const createDialog = page.getByRole('dialog', { name: 'New route resource' })
  await createDialog.locator('.field').filter({ hasText: 'Provider connection' }).getByRole('combobox').selectOption(provider.id)
  await createDialog.locator('.field').filter({ hasText: 'Resource name' }).getByRole('textbox').fill(`Empty inventory account ${runID}`)
  await createDialog.getByRole('textbox', { name: 'API key', exact: true }).fill('synthetic-account-secret')
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

test('@e2e-model-supply-lifecycle-001 provider supply updates, rejects unsafe deletion, and tears down cleanly', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The stateful supply lifecycle runs once; surface tests cover responsive projections.')
  test.setTimeout(120_000)

  const browserErrors = captureBrowserErrors(page)
  const runID = `${testInfo.project.name}-${Date.now()}`
  const upstreamPort = process.env.ASTER_E2E_UPSTREAM_PORT || '19000'
  const providerName = `Browser supply provider ${runID}`
  const updatedProviderName = `${providerName} updated`
  const accountName = `Browser supply account ${runID}`
  const updatedAccountName = `${accountName} updated`
  const publicModel = `browser-supply-${runID}`
  const updatedModelName = `Browser supply model ${runID} updated`

  await loginDemo(page)
  const token = await loginTestPrincipal(page)

  await page.goto(modelPaths.providers)
  await page.getByRole('button', { name: 'New provider' }).click()
  let modal = page.getByRole('dialog', { name: 'New provider connection' })
  await modal.getByLabel('Connection name').fill(providerName)
  await modal.getByLabel('Base URL').fill(`http://127.0.0.1:${upstreamPort}/v1`)
  await modal.getByLabel('Priority').fill('15')
  await modal.getByRole('button', { name: 'Create connection' }).click()
  await expect(page.getByText('Provider created')).toBeVisible()

  let providerRow = page.getByRole('row').filter({ hasText: providerName })
  await providerRow.getByRole('button', { name: 'Edit' }).click()
  modal = page.getByRole('dialog', { name: 'Edit provider connection' })
  await modal.getByLabel('Connection name').fill(updatedProviderName)
  await modal.getByLabel('Priority').fill('12')
  await modal.getByRole('button', { name: 'Update connection' }).click()
  await expect(page.getByText('Provider updated')).toBeVisible()
  await page.reload()
  providerRow = page.getByRole('row').filter({ hasText: updatedProviderName })
  await expect(providerRow).toContainText('12')
  await providerRow.getByRole('button', { name: 'Check' }).click()
  await expect(providerRow).toContainText('Provider endpoint configuration is ready; credentials are validated on provider accounts')
  await expect(providerRow).toContainText('ok')

  await page.goto(modelPaths.accounts)
  await page.getByRole('button', { name: 'New route resource' }).click()
  modal = page.getByRole('dialog', { name: 'New route resource' })
  await modal.getByLabel('Provider').selectOption({ label: `${updatedProviderName} · openai_compatible` })
  await modal.getByLabel('Resource name').fill(accountName)
  await modal.getByLabel('API key', { exact: true }).fill('synthetic-account-secret')
  await modal.getByPlaceholder('Enter an upstream model ID').fill('upstream-model')
  await modal.getByRole('button', { name: 'Add custom model' }).click()
  await modal.getByRole('button', { name: 'Save', exact: true }).click()
  modal = page.getByRole('dialog', { name: 'Edit route resource' })
  await expect(modal).toBeVisible()
  await expect(page.getByText('Route resource created')).toBeVisible()
  await modal.getByLabel('Resource name').fill(updatedAccountName)
  await fieldControl(modal, 'Concurrency').fill('4')
  await modal.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByText('Route resource updated')).toBeVisible()
  await modal.getByRole('button', { name: 'Close' }).click()
  await page.reload()

  let accountRow = page.getByRole('row').filter({ hasText: updatedAccountName })
  await expect(accountRow).toContainText('4')
  await accountRow.getByRole('switch', { name: `Toggle scheduling for ${updatedAccountName}` }).click()
  await expect(accountRow).toContainText('not schedulable')
  accountRow = page.getByRole('row').filter({ hasText: updatedAccountName })
  await accountRow.getByRole('switch', { name: `Toggle scheduling for ${updatedAccountName}` }).click()
  await expect(accountRow).toContainText('schedulable')
  await accountRow.getByRole('button', { name: 'More actions' }).click()
  await accountRow.getByRole('button', { name: 'Check' }).click()
  await expect(page.getByText('Provider account is reachable; discovered 1 models')).toBeVisible()
  const accountID = await accountRow.getAttribute('data-account-id')
  expect(accountID).toBeTruthy()
  const accountHealth = await envelope<Array<{ account_id: string; status: string }>>(
    await page.request.get(controlAPI('/provider-account-health-checks'), { headers: { Authorization: `Bearer ${token}` } })
  )
  expect(accountHealth).toContainEqual(expect.objectContaining({ account_id: accountID, status: 'ok' }))

  await page.goto('/console/model-services')
  await page.getByRole('button', { name: 'New gateway model' }).click()
  modal = page.locator('.modal-card')
  await fieldControl(modal, 'External model ID').fill(publicModel)
  await fieldControl(modal, 'Display name').fill(`Browser supply model ${runID}`)
  await fieldControl(modal, 'Description', 'textarea').fill('Browser-created supply lifecycle model')
  await modal.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByText('Gateway model created')).toBeVisible()
  let modelRow = page.getByRole('row').filter({ hasText: publicModel })
  await modelRow.getByTitle('Edit').click()
  modal = page.locator('.modal-card')
  await fieldControl(modal, 'Display name').fill(updatedModelName)
  await modal.getByLabel('Enable sticky routing for stable session identifiers').check()
  await fieldControl(modal, 'Sticky TTL (seconds)').fill('900')
  await modal.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByText('Gateway model updated')).toBeVisible()
  await page.reload()
  modelRow = page.getByRole('row').filter({ hasText: publicModel })
  await expect(modelRow).toContainText(updatedModelName)

  await page.goto(modelPaths.routes)
  await page.getByRole('button', { name: 'New model route' }).click()
  modal = page.locator('.modal-card')
  await fieldControl(modal, 'Gateway model', 'select').selectOption({ label: publicModel })
  await fieldControl(modal, 'Provider account', 'select').selectOption({ label: updatedAccountName })
  await fieldControl(modal, 'Upstream model', 'select').selectOption('upstream-model')
  await fieldControl(modal, 'Priority').fill('10')
  await fieldControl(modal, 'Weight').fill('100')
  await modal.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByText('Model route created')).toBeVisible()
  let routeRow = page.getByRole('row').filter({ hasText: publicModel }).filter({ hasText: updatedAccountName })
  await routeRow.getByTitle('Edit').click()
  modal = page.locator('.modal-card')
  await fieldControl(modal, 'Priority').fill('20')
  await fieldControl(modal, 'Weight').fill('250')
  await fieldControl(modal, 'Status', 'select').selectOption('disabled')
  await modal.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByText('Model route updated')).toBeVisible()
  await page.reload()
  routeRow = page.getByRole('row').filter({ hasText: publicModel }).filter({ hasText: updatedAccountName })
  await expect(routeRow).toContainText('P20')
  await expect(routeRow).toContainText('W250')
  await expect(routeRow).toContainText('disabled')

  await page.goto(modelPaths.accounts)
  accountRow = page.getByRole('row').filter({ hasText: updatedAccountName })
  const expectedErrorCount = browserErrors.length
  const protectedDeleteResponse = page.waitForResponse((response) =>
    response.url().endsWith(`/api/v1/console/provider-accounts/${accountID}`) &&
    response.request().method() === 'DELETE'
  )
  page.once('dialog', (dialog) => dialog.accept())
  await accountRow.getByRole('button', { name: 'Delete account' }).click()
  const protectedDelete = await protectedDeleteResponse
  expect(protectedDelete.status()).toBe(400)
  expect(await protectedDelete.json()).toEqual(expect.objectContaining({
    code: 1554,
    message: expect.stringContaining('referenced by model route')
  }))
  await expect(page.locator('.notice').filter({ hasText: 'referenced by model route' })).toBeVisible()
  await expect(accountRow).toBeVisible()
  expect(browserErrors.splice(expectedErrorCount)).toEqual([
    'console: Failed to load resource: the server responded with a status of 400 (Bad Request)'
  ])

  await page.goto(modelPaths.routes)
  routeRow = page.getByRole('row').filter({ hasText: publicModel }).filter({ hasText: updatedAccountName })
  page.once('dialog', (dialog) => dialog.accept())
  await routeRow.getByTitle('Delete model route').click()
  await expect(page.getByText('Model route deleted')).toBeVisible()
  await page.reload()
  await expect(page.getByRole('row').filter({ hasText: publicModel }).filter({ hasText: updatedAccountName })).toHaveCount(0)

  await page.goto('/console/model-services')
  modelRow = page.getByRole('row').filter({ hasText: publicModel })
  page.once('dialog', (dialog) => dialog.accept())
  await modelRow.getByTitle('Delete gateway model').click()
  await expect(page.getByText('Gateway model deleted')).toBeVisible()
  await page.reload()
  await expect(page.getByRole('row').filter({ hasText: publicModel })).toHaveCount(0)

  await page.goto(modelPaths.accounts)
  accountRow = page.getByRole('row').filter({ hasText: updatedAccountName })
  page.once('dialog', (dialog) => dialog.accept())
  await accountRow.getByRole('button', { name: 'Delete account' }).click()
  await expect(page.getByText('Route resource deleted')).toBeVisible()
  await page.reload()
  await expect(page.getByRole('row').filter({ hasText: updatedAccountName })).toHaveCount(0)

  const audit = await envelope<Array<Record<string, unknown>>>(await page.request.get(controlAPI('/audit-logs?limit=200'), {
    headers: { Authorization: `Bearer ${token}` }
  }))
  for (const [action, resourceType] of [
    ['create', 'provider'], ['update', 'provider'], ['check', 'provider'],
    ['create', 'provider_account'], ['update', 'provider_account'], ['check', 'provider_account'], ['delete', 'provider_account'],
    ['create', 'gateway_model'], ['update', 'gateway_model'], ['delete', 'gateway_model'],
    ['create', 'model_route'], ['update', 'model_route'], ['delete', 'model_route']
  ]) {
    expect(audit).toContainEqual(expect.objectContaining({ action, resource_type: resourceType }))
  }
  expect(browserErrors).toEqual([])
})

test('@e2e-model-inventory-001 model inventory and bulk routes stay auditable across responsive layouts', async ({ page }, testInfo) => {
  const browserErrors = captureBrowserErrors(page)
  await loginDemo(page)
  await page.goto(modelPaths.providers)
  await page.getByRole('button', { name: 'New provider' }).click()
  const providerDialog = page.getByRole('dialog', { name: 'New provider' })
  await expect(providerDialog).toBeVisible()
  await expect(providerDialog.getByText('Recommended models')).toHaveCount(0)
  await expect(providerDialog.locator('.provider-model-section')).toHaveCount(0)
  await expectNoHorizontalOverflow(page)
  await providerDialog.getByRole('button', { name: 'Close' }).click()

  const token = await loginTestPrincipal(page)
  const runID = `${testInfo.project.name}-${Date.now()}`
  const upstreamPort = process.env.ASTER_E2E_UPSTREAM_PORT || '19000'
  const exactUpstream = `exact-upstream-${runID}`
  const manualUpstream = `manual-upstream-${runID}`

  const provider = await adminPost<{ id: string }>(page, token, '/providers', {
    name: `Model inventory provider ${runID}`,
    type: 'openai_compatible',
    base_url: `http://127.0.0.1:${upstreamPort}/v1`,
    status: 'active',
    priority: 10
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
    upstream_format: 'openai_chat',
    priority: 10,
    weight: 100,
    status: 'active'
  })

  await page.goto(modelPaths.accounts)
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
  await page.goto(modelPaths.routes)
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
