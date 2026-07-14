import { expect, test } from '@playwright/test'
import { captureBrowserErrors, expectNoHorizontalOverflow, loginDemo } from './fixtures'

// Candidate-package journeys use one external origin instead of the local dev
// server. Keep health assertions on that same origin.
const backendURL = process.env.ASTER_E2E_EXTERNAL_URL || `http://127.0.0.1:${process.env.ASTER_E2E_BACKEND_PORT || '18080'}`
const expectedDemoMode = process.env.ASTER_E2E_EXPECT_DEMO_MODE === undefined
  ? true
  : process.env.ASTER_E2E_EXPECT_DEMO_MODE === 'true'
const expectedProfile = process.env.ASTER_E2E_EXPECT_PROFILE || ''
const profileJourney: Record<string, { path: string; heading: string }> = {
  personal: { path: '/console/overview', heading: 'Personal Console' },
  relay_operator: { path: '/operator/overview', heading: 'Relay Operator Console' },
  enterprise: { path: '/admin/dashboard', heading: 'Overview' },
  platform: { path: '/platform/overview', heading: 'Platform overview' }
}
const activeJourney = profileJourney[expectedProfile] || profileJourney.personal

test('@smoke @surface-smoke backend health and public settings are ready', async ({ request }) => {
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
  if (expectedProfile) {
    expect(settingsBody.data).toMatchObject({ default_profile: expectedProfile, enabled_profiles: [expectedProfile] })
  }
})

test('@smoke @surface-smoke anonymous protected navigation redirects to login', async ({ page }) => {
  const errors = captureBrowserErrors(page)
  const protectedPath = `${activeJourney.path}?status=active`
  await page.goto(protectedPath)

  await expect(page).toHaveURL(/\/login\?redirect=/)
  const loginURL = new URL(page.url())
  expect(loginURL.searchParams.get('redirect')).toBe(protectedPath)
  await expect(page.getByRole('heading', { level: 2, name: 'Welcome back' })).toBeVisible()
  await expect(page.getByLabel('Username')).toHaveValue('admin')
  await expect(page.locator('input#password')).toHaveAttribute('type', 'password')
  expect(errors).toEqual([])
})

test('@smoke @surface-smoke login persists and opens the enabled deployment surface', async ({ page }) => {
  const errors = captureBrowserErrors(page)
  await loginDemo(page)

  await page.reload()
  await page.waitForLoadState('networkidle')
  await expect(page).toHaveURL(new RegExp(`${activeJourney.path}$`))
  await expect(page.getByRole('heading', { level: 1, name: activeJourney.heading })).toBeVisible()

  const additionalSurfaces = expectedProfile
    ? []
    : ['/operator/overview', '/admin/dashboard', '/portal/overview', '/platform/overview']
  for (const path of additionalSurfaces) {
    await page.goto(path)
    await page.waitForLoadState('networkidle')
    await expect(page).toHaveURL(new RegExp(`${path}$`))
    await expect(page.locator('main')).toBeVisible()
  }
  expect(errors).toEqual([])
})

test('@smoke @surface-smoke locale, theme, and responsive layout remain usable', async ({ page }) => {
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
