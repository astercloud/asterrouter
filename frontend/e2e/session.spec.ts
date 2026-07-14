import { expect, test } from '@playwright/test'
import { envelope as data, loginDemo } from './fixtures'

test('@smoke @j02 logout immediately revokes a dedicated user session', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The session contract is viewport-independent and runs once on desktop.')

  await loginDemo(page)
  const adminToken = await page.evaluate(() => localStorage.getItem('asterrouter_admin_token') || '')
  const headers = { Authorization: `Bearer ${adminToken}` }
  const settings = await data<Record<string, unknown>>(await page.request.get('/api/v1/admin/settings', { headers }))
  const originalRegistration = Boolean(settings.registration_enabled)
  const email = `e2e-session-${Date.now()}@example.test`
  const password = 'synthetic-password-123'

  try {
    await data(await page.request.put('/api/v1/admin/settings', {
      headers,
      data: { ...settings, registration_enabled: true, email_verify_enabled: false }
    }))
    await data(await page.request.post('/api/v1/auth/register', {
      data: { email, password, display_name: 'E2E Session User', agreement_accepted: true }
    }))
  } finally {
    await data(await page.request.put('/api/v1/admin/settings', {
      headers,
      data: { ...settings, registration_enabled: originalRegistration }
    }))
  }

  const login = await data<{ access_token: string }>(await page.request.post('/api/v1/auth/login', {
    data: { username: email, password, agreement_accepted: true }
  }))
  const userHeaders = { Authorization: `Bearer ${login.access_token}` }
  expect((await page.request.get('/api/v1/account/profile', { headers: userHeaders })).status()).toBe(200)
  expect((await page.request.post('/api/v1/auth/logout', { headers: userHeaders })).status()).toBe(200)
  expect((await page.request.get('/api/v1/account/profile', { headers: userHeaders })).status()).toBe(401)

  const relogin = await data<{ access_token: string }>(await page.request.post('/api/v1/auth/login', {
    data: { username: email, password, agreement_accepted: true }
  }))
  expect(relogin.access_token).not.toBe(login.access_token)
  expect((await page.request.get('/api/v1/account/profile', {
    headers: { Authorization: `Bearer ${relogin.access_token}` }
  })).status()).toBe(200)
})

test('@smoke @j02 role changes and disabling immediately revoke existing user sessions', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The session contract is viewport-independent and runs once on desktop.')

  await loginDemo(page)
  const adminToken = await page.evaluate(() => localStorage.getItem('asterrouter_admin_token') || '')
  const headers = { Authorization: `Bearer ${adminToken}` }
  const settings = await data<Record<string, unknown>>(await page.request.get('/api/v1/admin/settings', { headers }))
  const email = `e2e-session-admin-${Date.now()}@example.test`
  const password = 'synthetic-password-123'
  let userID = ''

  try {
    await data(await page.request.put('/api/v1/admin/settings', {
      headers,
      data: { ...settings, registration_enabled: true, email_verify_enabled: false }
    }))
    const registration = await data<{ user_id: string }>(await page.request.post('/api/v1/auth/register', {
      data: { email, password, display_name: 'E2E Revoked User', agreement_accepted: true }
    }))
    userID = registration.user_id
  } finally {
    await data(await page.request.put('/api/v1/admin/settings', { headers, data: settings }))
  }

  const login = async () => data<{ access_token: string }>(await page.request.post('/api/v1/auth/login', {
    data: { username: email, password, agreement_accepted: true }
  }))
  const updateUser = async (status: string, role: string) => data(await page.request.put(`/api/v1/admin/users/${userID}`, {
    headers,
    data: { email, display_name: 'E2E Revoked User', status, role }
  }))
  const profileStatus = (token: string) => page.request.get('/api/v1/account/profile', {
    headers: { Authorization: `Bearer ${token}` }
  }).then((response) => response.status())

  const beforeRoleChange = await login()
  expect(await profileStatus(beforeRoleChange.access_token)).toBe(200)
  await updateUser('active', 'key_manager')
  expect(await profileStatus(beforeRoleChange.access_token)).toBe(401)

  const beforeDisable = await login()
  expect(await profileStatus(beforeDisable.access_token)).toBe(200)
  await updateUser('disabled', 'key_manager')
  expect(await profileStatus(beforeDisable.access_token)).toBe(401)
  expect((await page.request.post('/api/v1/auth/login', {
    data: { username: email, password, agreement_accepted: true }
  })).status()).toBe(401)
})
