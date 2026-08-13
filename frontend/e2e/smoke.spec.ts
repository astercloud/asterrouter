import { expect, test } from '@playwright/test'
import { captureBrowserErrors, expectNoHorizontalOverflow, loginDemo } from './fixtures'

// Candidate-package journeys use one external origin instead of the local dev
// server. Keep health assertions on that same origin.
const backendURL = process.env.ASTER_E2E_EXTERNAL_URL || `http://127.0.0.1:${process.env.ASTER_E2E_BACKEND_PORT || '18080'}`
const expectedDemoMode = process.env.ASTER_E2E_EXPECT_DEMO_MODE === undefined
  ? true
  : process.env.ASTER_E2E_EXPECT_DEMO_MODE === 'true'
const managementEntry = { path: '/console/workbench', heading: 'Overview' }

test('@e2e-platform-001 backend health and public settings are ready', async ({ request }) => {
  const health = await request.get(`${backendURL}/health`)
  expect(health.status()).toBe(200)
  await expect(health.json()).resolves.toMatchObject({ data: { status: 'ok' } })

  const ready = await request.get(`${backendURL}/ready`)
  expect(ready.status()).toBe(200)
  await expect(ready.json()).resolves.toMatchObject({ data: { status: 'ready' } })

  const settings = await request.get(`${backendURL}/api/v1/settings/public`)
  expect(settings.status()).toBe(200)
  const settingsBody = await settings.json()
  expect(settingsBody).toMatchObject({ data: { demo_mode: expectedDemoMode, setup_completed: true } })
})

test('@e2e-authz-001 anonymous protected navigation redirects to login', async ({ page }) => {
  const errors = captureBrowserErrors(page)
  const protectedPath = `${managementEntry.path}?status=active`
  await page.goto(protectedPath)

  await expect(page).toHaveURL(/\/login\?redirect=/)
  const loginURL = new URL(page.url())
  expect(loginURL.searchParams.get('redirect')).toBe(protectedPath)
  await expect(page.getByRole('heading', { level: 2, name: 'Welcome back' })).toBeVisible()
  if (expectedDemoMode) {
    await expect(page.getByText('Experience AsterRouter now')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Try the demo' })).toBeVisible()
  }
  await expect(page.getByLabel('Username')).toHaveValue('admin')
  await expect(page.locator('input#password')).toHaveAttribute('type', 'password')
  expect(errors).toEqual([])
})

test('@e2e-login-001 login persists and opens the enterprise management console', async ({ page }) => {
  const errors = captureBrowserErrors(page)
  await loginDemo(page)

  await page.reload()
  await expect(page).toHaveURL(new RegExp(`${managementEntry.path}$`))
  await expect(page.getByRole('heading', { level: 1, name: managementEntry.heading })).toBeVisible()
  expect(errors).toEqual([])
})

test('@e2e-credential-boundary-001 application credential editor exposes enterprise ownership only', async ({ page }) => {
  const errors = captureBrowserErrors(page)
  await loginDemo(page)
  await page.goto('/console/applications/credentials')

  await page.getByRole('button', { name: 'New workspace key' }).click()
  const keyTypeField = page.locator('.field').filter({ hasText: 'Key type' }).locator('select')
  await expect(keyTypeField.locator('option')).toHaveText(['workspace', 'user', 'service'])
  await expect(page.getByText('Customer ID')).toHaveCount(0)
  expect(errors).toEqual([])
})

test('@e2e-list-contract-001 application and policy lists remain usable', async ({ page }) => {
  const errors = captureBrowserErrors(page)
  await loginDemo(page)

  await page.goto('/console/applications')
  await expect(page.getByRole('heading', { level: 1, name: 'Applications' })).toBeVisible()
  await expect(page.locator('.application-list-panel')).toBeVisible()
  await expectNoHorizontalOverflow(page)

  await page.goto('/console/policies/access')
  await expect(page.getByRole('heading', { level: 1, name: 'Policies' })).toBeVisible()
  await expect(page.locator('.policy-page .table-panel')).toBeVisible()
  await expectNoHorizontalOverflow(page)
  expect(errors).toEqual([])
})

test('@e2e-preferences-001 locale, theme, and responsive layout remain usable', async ({ page }) => {
  const errors = captureBrowserErrors(page)
  await loginDemo(page)

  const language = page.getByLabel('Language')
  await language.selectOption('zh-CN')
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')
  await expect(page.locator('h1')).toBeVisible()

  if ((page.viewportSize()?.width || 0) <= 640) {
    await page.getByRole('button', { name: '打开导航' }).click()
  }
  const themeButton = page.getByRole('button', { name: /深色模式|浅色模式/ })
  await themeButton.click()
  const theme = await page.locator('html').getAttribute('data-theme')
  expect(['dark', 'light']).toContain(theme)

  await page.reload()
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme || '')
  await expectNoHorizontalOverflow(page)
  expect(errors).toEqual([])
})
