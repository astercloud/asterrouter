import { expect, test } from '@playwright/test'
import { captureBrowserErrors, envelope, loginTestPrincipal, loginUser, registerUsers } from './fixtures'

type AccountProfile = {
  auth_identities: Array<{ issuer: string; subject: string; email: string }>
  login_methods: Array<{ id: string; label: string; available: boolean; bound: boolean }>
}

test('@e2e-account-identity-001 OIDC identity binding and unbinding complete the real callback lifecycle', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The stateful identity lifecycle runs once on desktop.')
  test.skip(process.env.ASTER_E2E_OIDC_AVAILABLE !== '1', 'This journey requires the isolated fake OIDC runtime.')
  test.setTimeout(60_000)

  const browserErrors = captureBrowserErrors(page)
  const adminToken = await loginTestPrincipal(page)
  const adminHeaders = { Authorization: `Bearer ${adminToken}` }
  const password = 'e2e-identity-password-1'
  const email = `e2e-identity-${Date.now()}@example.test`
  const [{ id: userID }] = await registerUsers(page, adminToken, [{ email, password, displayName: 'E2E Identity User' }])
  const token = await loginUser(page, email, password)
  const headers = { Authorization: `Bearer ${token}` }
  await page.goto('/login')
  await page.getByLabel('Username').fill(email)
  await page.locator('#password').fill(password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).toHaveURL(/\/portal\/overview$/)

  await page.goto('/portal/account')
  await page.getByRole('tab', { name: 'Sign-in methods' }).click()
  const oidcMethod = page.locator('.login-method-row').filter({ hasText: 'Fake OIDC' })
  await expect(oidcMethod).toContainText('Available')

  const bindingResponse = page.waitForResponse((response) => response.url().includes('/api/v1/account/identities/oidc/bind') && response.request().method() === 'POST')
  await oidcMethod.getByRole('button', { name: 'Bind', exact: true }).click()
  expect((await bindingResponse).status()).toBe(200)
  await expect(page).toHaveURL(/\/oidc\/authorize\?/)
  await expect(page.getByRole('heading', { name: 'Fake OIDC authorization' })).toBeVisible()
  await page.getByRole('button', { name: 'Continue' }).click()

  await expect(page).toHaveURL(/\/portal\/account$/)
  await expect(page.getByText('Sign-in method bound successfully')).toBeVisible()
  await expect(oidcMethod).toContainText('Bound')
  const bound = await envelope<AccountProfile>(await page.request.get('/api/v1/account/profile', { headers }))
  expect(bound.auth_identities).toContainEqual(expect.objectContaining({
    issuer: expect.stringMatching(/\/oidc$/),
    subject: 'fake-oidc-subject-1',
    email: 'e2e-oidc@example.test'
  }))
  expect(bound.login_methods).toContainEqual(expect.objectContaining({ id: 'oidc', available: true, bound: true }))

  await page.reload()
  await page.getByRole('tab', { name: 'Sign-in methods' }).click()
  await expect(oidcMethod).toContainText('Bound')

  page.once('dialog', (dialog) => dialog.accept())
  const unbindingResponse = page.waitForResponse((response) => response.url().includes('/api/v1/account/identities/oidc') && response.request().method() === 'DELETE')
  await oidcMethod.getByRole('button', { name: 'Unbind', exact: true }).click()
  expect((await unbindingResponse).status()).toBe(200)
  await expect(page.getByText('Fake OIDC has been unbound')).toBeVisible()
  await expect(oidcMethod).toContainText('Available')

  await page.reload()
  await page.getByRole('tab', { name: 'Sign-in methods' }).click()
  await expect(oidcMethod).toContainText('Available')
  const unbound = await envelope<AccountProfile>(await page.request.get('/api/v1/account/profile', { headers }))
  expect(unbound.auth_identities).not.toContainEqual(expect.objectContaining({ subject: 'fake-oidc-subject-1' }))
  expect(unbound.login_methods).toContainEqual(expect.objectContaining({ id: 'oidc', available: true, bound: false }))

  const audit = await envelope<Array<Record<string, unknown>>>(await page.request.get('/api/v1/console/audit-logs?limit=100', { headers: adminHeaders }))
  expect(audit).toContainEqual(expect.objectContaining({ action: 'auth_identity_bound', resource_type: 'workspace_user', resource_id: userID }))
  expect(audit).toContainEqual(expect.objectContaining({ action: 'auth_identity_unbound', resource_type: 'workspace_user', resource_id: userID }))
  expect(browserErrors).toEqual([])
})
