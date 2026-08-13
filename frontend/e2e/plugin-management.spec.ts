import { expect, test } from '@playwright/test'
import { captureBrowserErrors, envelope, loginDemo, loginTestPrincipal } from './fixtures'

type Plugin = { id: string; status: string }
type AuditEvent = { action: string; resource_type: string; resource_id: string; summary: string }
type SidecarRuntimeStatus = { plugin_id: string; installed: boolean; enabled: boolean; running: boolean; version: string }
type OfficialRequest = {
  kind: string
  method: string
  path: string
  valid: boolean
  errors: string[]
  headers: Record<string, string>
}

const webhookPluginID = 'com.asterrouter.notification.webhook'
const lockedPluginID = 'com.asterrouter.notification.slack'

test('@e2e-plugin-management-001 plugin management lifecycle is governed and auditable', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The stateful lifecycle runs once; the console surface contract covers every supported viewport.')
  test.setTimeout(90_000)

  const errors = captureBrowserErrors(page)
  await loginDemo(page)
  const adminToken = await loginTestPrincipal(page)
  const headers = { Authorization: `Bearer ${adminToken}` }
  const initialCatalog = await envelope<{ plugins: Plugin[] }>(await page.request.get('/api/v1/console/plugins', { headers }))
  const initialWebhook = initialCatalog.plugins.find((plugin) => plugin.id === webhookPluginID)
  expect(initialWebhook).toBeDefined()

  let issuedSecret = ''
  try {
    await page.goto('/console/plugins')
    await expect(page).toHaveURL(/\/console\/system\/plugins$/)
    await expect(page.getByRole('heading', { level: 1, name: 'Plugin Center' })).toBeVisible()

    await page.getByRole('button', { name: 'Plugin registry', exact: true }).click()
    const search = page.getByPlaceholder('Search plugins, vendors, or categories')
    await search.fill('Slack Notification')
    await page.getByRole('button', { name: /Slack Notification/ }).click()
    await expect(page.getByRole('button', { name: 'Enable', exact: true })).toBeDisabled()
    const lockedResponse = await page.request.post(`/api/v1/console/plugins/${encodeURIComponent(lockedPluginID)}/enable`, { headers })
    expect(lockedResponse.status()).toBe(409)
    expect(await lockedResponse.json()).toEqual(expect.objectContaining({
      code: 1709,
      message: expect.stringMatching(/entitlement.*missing/i)
    }))

    await search.fill('Generic Webhook Notification')
    await page.getByRole('button', { name: /Generic Webhook Notification/ }).click()
    if (initialWebhook?.status !== 'enabled') {
      await page.getByRole('button', { name: 'Enable', exact: true }).click()
      await expect(page.getByText('Plugin enabled', { exact: true })).toBeVisible()
    }
    await expect(page.getByRole('button', { name: 'Disable', exact: true })).toBeEnabled()

    const deliveriesResponse = page.waitForResponse((response) =>
      response.request().method() === 'GET'
      && response.url().includes(`/api/v1/console/plugins/${encodeURIComponent(webhookPluginID)}/deliveries`)
    )
    await page.getByRole('button', { name: 'Deliveries', exact: true }).click()
    const loadedDeliveries = await deliveriesResponse
    expect(loadedDeliveries.status()).toBe(200)
    expect((await loadedDeliveries.json()).data).toEqual([])
    const deliveriesDialog = page.locator('.modal-card').filter({ has: page.getByRole('heading', { name: /Deliveries.*Generic Webhook Notification/ }) })
    await expect(deliveriesDialog.getByText('No delivery attempts match the current filter.')).toBeVisible()
    await deliveriesDialog.getByRole('button', { name: 'Cancel', exact: true }).click()

    await page.getByRole('button', { name: 'Configure', exact: true }).click()
    const configDialog = page.locator('.modal-card').filter({ has: page.getByRole('heading', { name: /Configure.*Generic Webhook Notification/ }) })
    const webhookURL = configDialog.getByLabel('Webhook URL')
    await webhookURL.fill('ftp://invalid.example.test/hook')
    const expectedErrorCount = errors.length
    await configDialog.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(page.getByText(/webhook_url must be an HTTP or HTTPS URL/)).toBeVisible()
    expect(errors.splice(expectedErrorCount)).toEqual([
      'console: Failed to load resource: the server responded with a status of 400 (Bad Request)'
    ])

    const suffix = `${testInfo.project.name}-${Date.now()}`
    const secret = `synthetic-plugin-secret-${suffix}`
    await webhookURL.fill(`https://hooks.example.test/${suffix}`)
    await configDialog.getByLabel('Bearer token').fill(secret)
    await configDialog.getByLabel('Minimum severity').selectOption('critical')
    await configDialog.getByLabel('Alert types').fill('api_key_quota,gateway_error_rate')
    await configDialog.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(page.getByText('Plugin configuration saved', { exact: true })).toBeVisible()
    await expect(configDialog.getByLabel('Webhook URL')).toHaveValue('')
    await expect(configDialog.getByLabel('Bearer token')).toHaveValue('')
    await expect(page.getByText(secret, { exact: true })).toHaveCount(0)
    await configDialog.getByRole('button', { name: 'Cancel', exact: true }).click()

    await page.reload()
    await page.getByRole('button', { name: 'Plugin registry', exact: true }).click()
    await page.getByPlaceholder('Search plugins, vendors, or categories').fill('Generic Webhook Notification')
    await page.getByRole('button', { name: /Generic Webhook Notification/ }).click()
    await expect(page.getByRole('button', { name: 'Disable', exact: true })).toBeEnabled()
    await page.getByRole('button', { name: 'Configure', exact: true }).click()
    const persistedDialog = page.locator('.modal-card').filter({ has: page.getByRole('heading', { name: /Configure.*Generic Webhook Notification/ }) })
    await expect(persistedDialog.getByLabel('Minimum severity')).toHaveValue('critical')
    await expect(persistedDialog.getByLabel('Alert types')).toHaveValue('api_key_quota,gateway_error_rate')
    await expect(persistedDialog.getByLabel('Webhook URL')).toHaveValue('')
    await expect(persistedDialog.getByLabel('Webhook URL')).not.toHaveAttribute('placeholder', '')
    await persistedDialog.getByRole('button', { name: 'Cancel', exact: true }).click()

    await page.getByRole('button', { name: 'Plugin Open API', exact: true }).click()
    await page.getByRole('button', { name: 'Create API token', exact: true }).click()
    const tokenDialog = page.locator('.modal-card').filter({ has: page.getByRole('heading', { name: 'Create API token' }) })
    const tokenName = `Browser plugin token ${suffix}`
    await tokenDialog.getByLabel('Token name').fill(tokenName)
    await tokenDialog.getByLabel('Bound plugin').selectOption(webhookPluginID)
    await tokenDialog.getByLabel('plugin:action').check()
    await tokenDialog.getByRole('button', { name: 'Create API token', exact: true }).click()
    await expect(page.getByText('Plugin API token created', { exact: true })).toBeVisible()
    issuedSecret = await tokenDialog.locator('.token-secret-panel code').innerText()
    expect(issuedSecret).toMatch(/^arpt_/)

    const openCatalog = await page.request.get('/api/v1/open/plugins/catalog', {
      headers: { Authorization: `Bearer ${issuedSecret}` }
    })
    expect(openCatalog.status()).toBe(200)
    await tokenDialog.getByRole('button', { name: 'Cancel', exact: true }).click()
    await page.getByRole('button', { name: 'Refresh', exact: true }).click()
    const tokenRow = page.getByRole('row').filter({ hasText: tokenName })
    await expect(tokenRow).toContainText('active')
    await expect(tokenRow).not.toContainText(issuedSecret)
    page.once('dialog', (dialog) => dialog.accept())
    await tokenRow.getByTitle('Revoke token').click()
    await expect(page.getByText('Plugin API token revoked', { exact: true })).toBeVisible()
    await expect(tokenRow).toContainText('revoked')

    const revokedUse = await page.request.get('/api/v1/open/plugins/catalog', {
      headers: { Authorization: `Bearer ${issuedSecret}` }
    })
    expect(revokedUse.status()).toBe(401)

    const audit = await envelope<AuditEvent[]>(await page.request.get('/api/v1/console/audit-logs?resource_type=plugin&limit=200', { headers }))
    expect(audit).toEqual(expect.arrayContaining([
      expect.objectContaining({ action: 'enable', resource_type: 'plugin', resource_id: webhookPluginID }),
      expect.objectContaining({ action: 'configure', resource_type: 'plugin', resource_id: webhookPluginID }),
      expect.objectContaining({ action: 'api_token_create', resource_type: 'plugin' }),
      expect.objectContaining({ action: 'api_token_revoke', resource_type: 'plugin' })
    ]))
    expect(JSON.stringify(audit)).not.toContain(issuedSecret)
    expect(errors).toEqual([])
  } finally {
    if (initialWebhook?.status === 'enabled') {
      await page.request.post(`/api/v1/console/plugins/${encodeURIComponent(webhookPluginID)}/enable`, { headers })
    } else {
      await page.request.post(`/api/v1/console/plugins/${encodeURIComponent(webhookPluginID)}/disable`, { headers })
    }
  }
})

test('@e2e-plugin-trust-chain-001 signed official plugin trust chain crosses the browser and runtime', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The signed stateful trust chain runs once on desktop.')
  test.setTimeout(120_000)

  const officialURL = process.env.ASTER_E2E_OFFICIAL_URL || 'http://127.0.0.1:29006'
  const pluginID = 'com.astercloud.catalog.router-sync'
  const packageID = `pkg_router_sync_${process.platform}_${process.arch}`
  const importPackageID = `pkg_router_sync_import_${process.platform}_${process.arch}`
  const errors = captureBrowserErrors(page)

  await loginDemo(page)
  const adminToken = await loginTestPrincipal(page)
  const headers = { Authorization: `Bearer ${adminToken}` }
  await page.goto('/console/system/plugins')
  await expect(page.getByRole('heading', { level: 1, name: 'Plugin Center' })).toBeVisible()

  const catalogResponse = page.waitForResponse((response) =>
    response.request().method() === 'POST' && response.url().endsWith('/api/v1/console/plugins/catalog-sync')
  )
  await page.getByRole('button', { name: 'Sync catalog', exact: true }).first().click()
  expect((await catalogResponse).status()).toBe(200)
  await expect(page.getByText('Official catalog synchronized', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: 'Distribution & licensing', exact: true }).click()
  await page.getByRole('button', { name: 'Activate License', exact: true }).click()
  const activateDialog = page.locator('.modal-card').filter({ has: page.getByRole('heading', { name: 'Activate License', exact: true }) })
  await activateDialog.getByLabel('License ID').fill('lic_e2e_browser')
  await activateDialog.getByLabel('Activation secret').fill('e2e-activation-secret')
  await activateDialog.getByLabel('Instance', { exact: true }).fill('inst_e2e_browser')
  await activateDialog.getByLabel('Instance fingerprint').fill('sha256:e2e-browser-fingerprint')
  await activateDialog.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByText('License activated and locally verified', { exact: true })).toBeVisible()
  await expect(page.getByText('lic_e2e_browser', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: 'Redeem code', exact: true }).click()
  const redeemDialog = page.locator('.modal-card').filter({ has: page.getByRole('heading', { name: 'Redeem code', exact: true }) })
  await redeemDialog.getByLabel('Redeem code').fill('ASTER-E2E-REDEEM')
  await redeemDialog.getByLabel('Instance', { exact: true }).fill('inst_e2e_browser')
  await redeemDialog.getByLabel('Instance fingerprint').fill('sha256:e2e-browser-fingerprint')
  await redeemDialog.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByText('Redeem code consumed and License locally verified', { exact: true })).toBeVisible()

  const licenseFixture = await (await page.request.get(`${officialURL}/e2e/license-envelope`)).json()
  await page.getByRole('button', { name: 'Import License', exact: true }).click()
  const importLicenseDialog = page.locator('.modal-card').filter({ has: page.getByRole('heading', { name: 'Import License', exact: true }) })
  await importLicenseDialog.getByLabel('Offline License file').fill(JSON.stringify(licenseFixture))
  await importLicenseDialog.getByLabel('Activation secret (optional)').fill('e2e-activation-secret')
  await importLicenseDialog.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByText('License imported and locally verified', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: 'Refresh', exact: true }).first().click()
  await page.getByRole('button', { name: 'Data services', exact: true }).click()
  await expect(page.getByText('X25519-HKDF-SHA256+A256GCM', { exact: true })).toBeVisible()
  const feedPublicKey = await page.locator('.inline-code-row code').innerText()
  const feedFixture = await (await page.request.post(`${officialURL}/e2e/feed-envelope`, {
    data: { public_key: feedPublicKey }
  })).json()

  await page.getByRole('button', { name: 'Import feed', exact: true }).click()
  const importFeedDialog = page.locator('.modal-card').filter({ has: page.getByRole('heading', { name: 'Import feed', exact: true }) })
  await importFeedDialog.getByLabel('Encrypted Feed envelope JSON').fill(JSON.stringify(feedFixture))
  await importFeedDialog.getByRole('button', { name: 'Import feed', exact: true }).click()
  await expect(page.getByText('Official encrypted Feed imported', { exact: true })).toBeVisible()
  await expect(page.getByText('feed_e2e_import', { exact: true })).toBeVisible()

  await page.getByLabel('Service', { exact: true }).selectOption('provider-intelligence')
  await page.getByRole('button', { name: 'Sync feed', exact: true }).click()
  await expect(page.getByText('provider-intelligence synchronized at 2', { exact: true })).toBeVisible()
  await expect(page.getByText('feed_e2e_sync', { exact: true })).toHaveCount(2)

  await page.getByRole('button', { name: 'Plugin registry', exact: true }).click()
  const search = page.getByPlaceholder('Search plugins, vendors, or categories')
  await search.fill('Signed Router Sync')
  await page.getByRole('button', { name: /Signed Router Sync/ }).click()
  const importRow = page.locator('.package-row').filter({ hasText: importPackageID })
  const packageFixture = await (await page.request.get(`${officialURL}/e2e/package-import`)).json()
  await importRow.getByRole('button', { name: 'Import package', exact: true }).click()
  const importPackageDialog = page.locator('.modal-card').filter({ has: page.getByRole('heading', { name: 'Import package', exact: true }) })
  await importPackageDialog.getByLabel('Offline plugin package file JSON').fill(JSON.stringify(packageFixture))
  await importPackageDialog.getByRole('button', { name: 'Import package', exact: true }).click()
  await expect(page.getByText('Plugin package imported and verified', { exact: true })).toBeVisible()

  const downloadRow = page.locator('.package-row').filter({ hasText: packageID }).filter({ hasNotText: importPackageID })
  await downloadRow.getByRole('button', { name: 'Download package', exact: true }).click()
  await expect(page.getByText('Plugin package downloaded and verified', { exact: true })).toBeVisible()

  const refreshedImportRow = page.locator('.package-row').filter({ hasText: importPackageID })
  await refreshedImportRow.getByRole('button', { name: 'Install', exact: true }).click()
  await expect(page.getByText('Plugin package installed', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Enable', exact: true }).click()
  await expect(page.getByText('Plugin enabled', { exact: true })).toBeVisible()

  await page.reload()
  await expect(page.getByRole('heading', { level: 1, name: 'Plugin Center' })).toBeVisible()

  await page.getByRole('button', { name: 'Distribution & licensing', exact: true }).click()
  await expect(page.getByText('lic_e2e_browser', { exact: true })).toBeVisible()
  await expect(page.getByText('active', { exact: true }).first()).toBeVisible()

  await page.getByRole('button', { name: 'Data services', exact: true }).click()
  await expect(page.getByText('feed_e2e_sync', { exact: true })).toHaveCount(2)
  await expect(page.locator('.inline-code-row code')).toHaveText(feedPublicKey)

  await page.getByRole('button', { name: 'Plugin registry', exact: true }).click()
  await page.getByPlaceholder('Search plugins, vendors, or categories').fill('Signed Router Sync')
  const runtimeResponse = page.waitForResponse((response) =>
    response.request().method() === 'GET'
    && response.url().endsWith(`/api/v1/console/plugins/${encodeURIComponent(pluginID)}/runtime/status`)
  )
  await page.getByRole('button', { name: /Signed Router Sync/ }).click()
  const loadedRuntime = await runtimeResponse
  expect(loadedRuntime.status()).toBe(200)
  const runtime = (await loadedRuntime.json()).data as SidecarRuntimeStatus
  expect(runtime).toEqual(expect.objectContaining({
    plugin_id: pluginID,
    installed: true,
    enabled: true,
    running: false,
    version: '1.0.0'
  }))
  const runtimeSection = page.locator('.plugin-detail-section').filter({ has: page.getByRole('heading', { name: 'Runtime status' }) })
  await expect(runtimeSection.getByText('Installed', { exact: true })).toBeVisible()
  await expect(runtimeSection.getByText('Enabled', { exact: true })).toBeVisible()
  await expect(runtimeSection.getByText('Yes', { exact: true })).toHaveCount(2)
  await expect(page.locator('.package-row').filter({ hasText: importPackageID })).toContainText('installed')

  await page.getByRole('button', { name: 'Workbench', exact: true }).click()
  const launcher = page.locator('.plugin-launcher-item').filter({ hasText: 'Signed Router Sync' })
  const workbenchResponse = page.waitForResponse((response) => response.url().endsWith(
    `/api/v1/console/plugins/${encodeURIComponent(pluginID)}/frontend/workbench`
  ))
  const styleResponse = page.waitForResponse((response) => response.url().endsWith(
    `/api/v1/console/plugins/${encodeURIComponent(pluginID)}/frontend/assets/app.css`
  ))
  const scriptResponse = page.waitForResponse((response) => response.url().endsWith(
    `/api/v1/console/plugins/${encodeURIComponent(pluginID)}/frontend/assets/app.js`
  ))
  await launcher.getByRole('button', { name: 'Open workbench', exact: true }).click()
  const [loadedWorkbench, loadedStyle, loadedScript] = await Promise.all([workbenchResponse, styleResponse, scriptResponse])
  expect(loadedWorkbench.status()).toBe(200)
  expect(loadedWorkbench.headers()['content-type']).toContain('application/json')
  expect(loadedStyle.status()).toBe(200)
  expect(loadedStyle.headers()['content-type']).toContain('text/css')
  expect(loadedScript.status()).toBe(200)
  expect(loadedScript.headers()['content-type']).toMatch(/javascript/)
  await expect(page).toHaveURL(new RegExp(`/console/system/plugins/${pluginID}/workbench$`))
  await expect(page.getByRole('heading', { level: 1, name: 'Signed Router Sync' })).toBeVisible()
  await expect(page.getByRole('heading', { level: 2, name: 'Signed Router Sync Workbench' })).toBeVisible()
  await expect(page.getByText('Catalog, package, and frontend assets verified.', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: '返回插件中心' }).click()
  await page.getByRole('button', { name: 'Plugin registry', exact: true }).click()
  await page.getByPlaceholder('Search plugins, vendors, or categories').fill('Signed Router Sync')
  await page.getByRole('button', { name: /Signed Router Sync/ }).click()
  await page.locator('.package-row').filter({ hasText: importPackageID }).getByRole('button', { name: 'Uninstall', exact: true }).click()
  await expect(page.getByText('Plugin package uninstalled', { exact: true })).toBeVisible()

  const officialRequestsResponse = await page.request.get(`${officialURL}/e2e/requests`)
  expect(officialRequestsResponse.status()).toBe(200)
  const officialRequests = ((await officialRequestsResponse.json()).requests || []) as OfficialRequest[]
  for (const kind of [
    'catalog',
    'license_activate',
    'license_redeem',
    'feed_metadata',
    'feed_download',
    'package_authorization',
    'package_object'
  ]) {
    expect(officialRequests).toEqual(expect.arrayContaining([
      expect.objectContaining({ kind, valid: true, errors: [] })
    ]))
  }
  const packageAuthorization = officialRequests.find((request) => request.kind === 'package_authorization' && request.valid)
  expect(packageAuthorization?.headers).toEqual(expect.objectContaining({
    'x-aster-os': process.platform,
    'x-aster-arch': process.arch,
    'x-aster-license-id': 'lic_e2e_browser',
    'x-aster-activation-secret': '[REDACTED]',
    'x-aster-instance-id': 'inst_e2e_browser'
  }))
  expect(packageAuthorization?.headers['x-aster-core-version']).toBeTruthy()
  for (const kind of ['feed_metadata', 'feed_download']) {
    const request = officialRequests.find((candidate) => candidate.kind === kind && candidate.valid)
    expect(request?.headers).toEqual(expect.objectContaining({
      'x-aster-license-id': 'lic_e2e_browser',
      'x-aster-activation-secret': '[REDACTED]',
      'x-aster-instance-id': 'inst_e2e_browser',
      'x-aster-instance-fingerprint': 'sha256:e2e-browser-fingerprint',
      'x-aster-feed-public-key': '[PRESENT]'
    }))
    expect(request?.headers['x-aster-core-version']).toBeTruthy()
    expect(request?.headers['x-aster-request-id']).toBeTruthy()
  }
  expect(JSON.stringify(officialRequests)).not.toContain('e2e-activation-secret')

  const audit = await envelope<AuditEvent[]>(await page.request.get('/api/v1/console/audit-logs?resource_type=plugin&limit=200', { headers }))
  for (const action of [
    'catalog_sync',
    'license_activate',
    'license_redeem',
    'license_import',
    'feed_import',
    'feed_sync',
    'package_import',
    'package_download',
    'package_install',
    'enable',
    'package_uninstall'
  ]) {
    expect(audit).toEqual(expect.arrayContaining([
      expect.objectContaining({ action, resource_type: 'plugin' })
    ]))
  }
  expect(JSON.stringify(audit)).not.toContain('e2e-activation-secret')
  expect(errors).toEqual([])
})
