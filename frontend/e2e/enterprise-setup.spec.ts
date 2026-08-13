import { expect, test } from '@playwright/test'
import { captureBrowserErrors } from './fixtures'

test('@setup @e2e-setup-001 setup initializes one enterprise instance', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The persistent setup workflow runs once against an isolated empty runtime.')
  const adminPassword = process.env.ASTER_E2E_PASSWORD || 'setup-browser-test-password'
  const errors = captureBrowserErrors(page)
  await page.addInitScript(() => {
    if (sessionStorage.getItem('stale-auth-seeded')) return
    sessionStorage.setItem('stale-auth-seeded', 'true')
    localStorage.setItem('asterrouter_admin_token', 'stale-token-from-another-instance')
    localStorage.setItem('asterrouter_admin_user', JSON.stringify({
      username: 'old-admin',
      role: 'super_admin',
      display_name: 'Old admin',
      email: 'old-admin@example.com',
    }))
  })
  await page.goto('/setup')

  await expect(page.getByRole('heading', { name: 'Initialize enterprise instance' })).toBeVisible()
  await expect(page.getByLabel('Organization name')).toBeVisible()
  await expect(page.locator('input[type="radio"], input[name*="profile" i], input[name*="deployment" i]')).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Create enterprise instance' })).toBeDisabled()

  await page.getByLabel('Organization name').fill('AsterCloud Enterprise')
  await expect(page.getByRole('button', { name: 'Create enterprise instance' })).toBeEnabled()

  await page.getByRole('button', { name: 'Create enterprise instance' }).click()
  await expect(page).toHaveURL(/\/login(?:\?|$)/)
  expect(new URL(page.url()).searchParams.get('redirect')).toBe('/console/workbench')
  await expect.poll(() => page.evaluate(() => ({
    token: localStorage.getItem('asterrouter_admin_token'),
    user: localStorage.getItem('asterrouter_admin_user')
  }))).toEqual({ token: null, user: null })
  const status = await page.request.get('/api/v1/setup/status')
  await expect(status).toBeOK()
  await expect(status.json()).resolves.toMatchObject({
    data: { setup_completed: true }
  })

  await page.locator('input#password').fill(adminPassword)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page).toHaveURL(/\/console\/workbench$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Overview' })).toBeVisible()
  await page.reload()
  await expect(page).toHaveURL(/\/console\/workbench$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Overview' })).toBeVisible()
  expect(errors).toEqual([])
})
