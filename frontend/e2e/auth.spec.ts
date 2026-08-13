import { createHmac } from 'node:crypto'
import { expect, request as playwrightRequest, test, type Page } from '@playwright/test'
import { captureBrowserErrors, envelope, expectNoHorizontalOverflow, loginTestPrincipal } from './fixtures'

function decodeBase32(value: string): Buffer {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  let bits = ''
  for (const character of value.toUpperCase().replace(/=+$/, '')) {
    const index = alphabet.indexOf(character)
    if (index < 0) throw new Error('invalid base32 secret')
    bits += index.toString(2).padStart(5, '0')
  }
  const bytes: number[] = []
  for (let offset = 0; offset + 8 <= bits.length; offset += 8) {
    bytes.push(Number.parseInt(bits.slice(offset, offset + 8), 2))
  }
  return Buffer.from(bytes)
}

function currentTOTP(secret: string): string {
  const counter = Buffer.alloc(8)
  counter.writeBigUInt64BE(BigInt(Math.floor(Date.now() / 30_000)))
  const digest = createHmac('sha1', decodeBase32(secret)).update(counter).digest()
  const offset = digest[digest.length - 1] & 0x0f
  const value = (digest.readUInt32BE(offset) & 0x7fffffff) % 1_000_000
  return value.toString().padStart(6, '0')
}

async function signOut(page: Page) {
  await page.getByRole('button', { name: 'Account menu' }).click()
  await page.getByRole('button', { name: 'Sign out', exact: true }).click()
  await expect(page).toHaveURL(/\/login$/)
}

async function startPasswordLogin(page: Page, email: string, password: string) {
  await page.goto('/login')
  await page.getByLabel('Username').fill(email)
  await page.locator('#password').fill(password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
}

async function verificationToken(page: Page, email: string, responseToken?: string): Promise<string> {
  if (responseToken) return responseToken
  const mailAPI = process.env.ASTER_E2E_MAIL_API_URL
  expect(mailAPI, 'Gate B authentication requires the isolated fake SMTP API').toBeTruthy()
  let token = ''
  await expect.poll(async () => {
    const response = await page.request.get(`${mailAPI}/__test/messages?recipient=${encodeURIComponent(email)}`)
    expect(response.status()).toBe(200)
    const body = await response.json() as { messages: Array<{ body: string }> }
    const match = body.messages.at(-1)?.body.match(/\/verify-email\?token=([^"'&<\s]+)/)
    token = match?.[1] ? decodeURIComponent(match[1]) : ''
    return token
  }).not.toBe('')
  return token
}

async function accountProfileStatus(page: Page, token: string): Promise<number> {
  const request = await playwrightRequest.newContext({
    baseURL: new URL(page.url()).origin,
    extraHTTPHeaders: { Authorization: `Bearer ${token}` }
  })
  try {
    return (await request.get('/api/v1/account/profile')).status()
  } finally {
    await request.dispose()
  }
}

async function createTOTPAuthenticatedSession(page: Page, email: string, password: string, code: string): Promise<string> {
  const request = await playwrightRequest.newContext({ baseURL: new URL(page.url()).origin })
  try {
    const login = await envelope<{ mfa_required: boolean; challenge: string }>(await request.post('/api/v1/auth/login', {
      data: { username: email, password, agreement_accepted: true }
    }))
    expect(login.mfa_required).toBe(true)
    const session = await envelope<{ access_token: string }>(await request.post('/api/v1/auth/totp/login', {
      data: { challenge: login.challenge, code }
    }))
    return session.access_token
  } finally {
    await request.dispose()
  }
}

test('@e2e-auth-001 registration, email verification, TOTP, and recovery-code sign-in work end to end', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The stateful authentication journey runs once; public layouts run in every viewport.')
  test.setTimeout(90_000)

  const browserErrors = captureBrowserErrors(page)
  const adminToken = await loginTestPrincipal(page)
  const adminHeaders = { Authorization: `Bearer ${adminToken}` }
  const settings = await envelope<Record<string, unknown>>(await page.request.get('/api/v1/console/settings', { headers: adminHeaders }))
  const email = `e2e-auth-${Date.now()}@example.test`
  const password = 'synthetic-password-123'
  const updatedPassword = 'synthetic-password-456'
  const updatedDisplayName = 'E2E Authentication User Updated'

  try {
    await envelope(await page.request.put('/api/v1/console/settings', {
      headers: adminHeaders,
      data: {
        ...settings,
        registration_enabled: true,
        email_verify_enabled: true,
        password_reset_enabled: true,
        totp_enabled: true,
        turnstile_enabled: false,
        public_base_url: 'https://router.example.test',
        smtp_host: process.env.ASTER_E2E_SMTP_PORT ? '127.0.0.1' : 'smtp.example.test',
        smtp_port: Number(process.env.ASTER_E2E_SMTP_PORT || 587),
        smtp_from: 'noreply@example.test',
        smtp_use_tls: false
      }
    }))

    await page.goto('/register')
    await expect(page.getByRole('heading', { level: 2, name: 'Create account' })).toBeVisible()
    await page.getByLabel('Email').fill(email)
    await page.getByLabel('Display name').fill('E2E Authentication User')
    await page.locator('#new-password').fill(password)
    await page.getByLabel('Confirm password').fill(password)
    const registrationResponse = page.waitForResponse((response) => response.url().endsWith('/api/v1/auth/register') && response.request().method() === 'POST')
    await page.getByRole('button', { name: 'Create account', exact: true }).click()
    const registered = await registrationResponse
    expect(registered.status()).toBe(200)
    const registrationBody = await registered.json() as { data: { verification_token?: string; verification_required: boolean; email_delivery_failed: boolean } }
    expect(registrationBody.data.verification_required).toBe(true)
    expect(registrationBody.data.email_delivery_failed).toBe(false)
    const token = await verificationToken(page, email, registrationBody.data.verification_token)
    await expect(page.getByText('Your account has been created. Check your email to verify it before signing in.')).toBeVisible()

    const verificationResponse = page.waitForResponse((response) => response.url().endsWith('/api/v1/auth/verify-email'))
    await page.goto(`/verify-email?token=${encodeURIComponent(token)}`)
    expect((await verificationResponse).status()).toBe(200)
    await expect(page.getByText('Email verified. You can now sign in.')).toBeVisible()
    await page.getByRole('button', { name: 'Back to sign in' }).click()

    await page.goto('/forgot-password')
    await page.getByLabel('Email').fill(email)
    const forgotPasswordResponse = page.waitForResponse((response) => response.url().endsWith('/api/v1/auth/forgot-password'))
    await page.getByRole('button', { name: 'Send reset email', exact: true }).click()
    expect((await forgotPasswordResponse).status()).toBe(200)
    await expect(page.getByText('If the account exists, a reset email has been sent.')).toBeVisible()

    await page.goto('/resend-verification')
    await page.getByLabel('Email').fill(email)
    const resendResponse = page.waitForResponse((response) => response.url().endsWith('/api/v1/auth/resend-verification'))
    await page.getByRole('button', { name: 'Resend verification email', exact: true }).click()
    expect((await resendResponse).status()).toBe(200)
    await expect(page.getByText('If the account is awaiting verification, another email has been sent.')).toBeVisible()

    await page.goto('/login')

    await page.getByLabel('Username').fill(email)
    await page.locator('#password').fill(password)
    await page.getByRole('button', { name: 'Sign in', exact: true }).click()
    await expect(page).not.toHaveURL(/\/login/)
    const entryPath = new URL(page.url()).pathname
    const surface = entryPath.split('/').filter(Boolean)[0]
    expect(surface).toBeTruthy()

    await page.goto(`/${surface}/account`)
    await page.getByLabel('Display name').fill(updatedDisplayName)
    await page.getByRole('button', { name: 'Save profile' }).click()
    await expect(page.getByText('Profile updated')).toBeVisible()
    await page.reload()
    await expect(page.getByLabel('Display name')).toHaveValue(updatedDisplayName)

    await page.getByRole('tab', { name: 'Security' }).click()
    const prePasswordChangeToken = await page.evaluate(() => localStorage.getItem('asterrouter_admin_token'))
    expect(prePasswordChangeToken).toBeTruthy()
    await page.locator('#account-current-password').fill(password)
    await page.locator('#account-new-password').fill(updatedPassword)
    await page.locator('#account-confirm-password').fill(updatedPassword)
    await page.getByRole('button', { name: 'Change password', exact: true }).click()
    await expect(page.getByText('Password changed')).toBeVisible()
    expect(await accountProfileStatus(page, prePasswordChangeToken!)).toBe(401)

    await page.locator('#account-totp-current-password').fill(updatedPassword)
    await page.getByRole('button', { name: 'Set up authenticator' }).click()
    const secret = (await page.locator('.totp-setup-copy > code').first().textContent())?.trim() || ''
    expect(secret).toMatch(/^[A-Z2-7]+$/)
    await page.locator('#account-totp-code').fill(currentTOTP(secret))
    await page.getByRole('button', { name: 'Confirm and enable' }).click()
    await expect(page.getByText('Two-factor authentication enabled. Save your recovery codes now.')).toBeVisible()
    const recoveryCode = (await page.locator('.recovery-grid code').first().textContent())?.trim() || ''
    expect(recoveryCode).toMatch(/^[A-Z2-7]{6}-[A-Z2-7]{6}$/)
    await page.screenshot({ path: testInfo.outputPath('totp-enabled.png'), fullPage: true })

    await page.locator('#account-recovery-totp-code').fill(recoveryCode)
    const recoveryResponse = page.waitForResponse((response) =>
      response.url().endsWith('/api/v1/account/totp/recovery-codes') && response.request().method() === 'POST'
    )
    await page.getByRole('button', { name: 'Regenerate recovery codes' }).click()
    expect((await recoveryResponse).status()).toBe(200)
    await expect(page.locator('.recovery-grid code').first()).not.toHaveText(recoveryCode)
    const replacementRecoveryCode = (await page.locator('.recovery-grid code').first().textContent())?.trim() || ''
    expect(replacementRecoveryCode).toMatch(/^[A-Z2-7]{6}-[A-Z2-7]{6}$/)
    expect(replacementRecoveryCode).not.toBe(recoveryCode)

    await signOut(page)
    await startPasswordLogin(page, email, updatedPassword)
    await expect(page.getByRole('heading', { level: 2, name: 'Two-factor verification' })).toBeVisible()
    await page.locator('#mfa-code').fill(currentTOTP(secret))
    await page.getByRole('button', { name: 'Verify and sign in' }).click()
    await expect(page).toHaveURL(new RegExp(`${entryPath}$`))

    await signOut(page)
    await startPasswordLogin(page, email, updatedPassword)
    await expect(page.locator('#mfa-code')).toHaveAttribute('maxlength', '13')
    await page.locator('#mfa-code').fill(replacementRecoveryCode)
    await page.getByRole('button', { name: 'Verify and sign in' }).click()
    await expect(page).toHaveURL(new RegExp(`${entryPath}$`))

    const secondarySessionToken = await createTOTPAuthenticatedSession(page, email, updatedPassword, currentTOTP(secret))
    expect(secondarySessionToken).toBeTruthy()

    await page.goto(`/${surface}/account`)
    await page.getByRole('tab', { name: 'Security' }).click()
    page.once('dialog', (dialog) => dialog.accept())
    await page.getByRole('button', { name: 'Sign out other devices' }).click()
    await expect(page.getByText('Other device sessions have been revoked')).toBeVisible()
    expect(await accountProfileStatus(page, secondarySessionToken)).toBe(401)

    await page.locator('#account-disable-totp-code').fill(currentTOTP(secret))
    await page.getByRole('button', { name: 'Disable two-factor authentication' }).click()
    await expect(page.getByText('Two-factor authentication disabled')).toBeVisible()
    await signOut(page)
    await startPasswordLogin(page, email, updatedPassword)
    await expect(page).toHaveURL(new RegExp(`${entryPath}$`))
    await expect(page.getByRole('heading', { level: 2, name: 'Two-factor verification' })).toHaveCount(0)
    await page.goto(`/${surface}/account`)
    await expect(page.getByLabel('Display name')).toHaveValue(updatedDisplayName)
    expect(browserErrors).toEqual([])
  } finally {
    await envelope(await page.request.put('/api/v1/console/settings', { headers: adminHeaders, data: settings }))
  }
})

test('@e2e-auth-002 public authentication pages remain usable and reject invalid links', async ({ page }, testInfo) => {
  const browserErrors = captureBrowserErrors(page)

  await page.goto('/register')
  await expect(page.getByRole('heading', { level: 2, name: 'Create account' })).toBeVisible()
  await expectNoHorizontalOverflow(page)

  await page.goto('/forgot-password')
  await expect(page.getByRole('heading', { level: 2, name: 'Recover your account' })).toBeVisible()
  await expectNoHorizontalOverflow(page)

  await page.goto('/resend-verification')
  await expect(page.getByRole('heading', { level: 2, name: 'Request another verification email' })).toBeVisible()
  await expectNoHorizontalOverflow(page)

  await page.goto('/verify-email')
  await expect(page.getByText('The email verification link is invalid or expired.')).toBeVisible()

  await page.goto('/reset-password?token=invalid-token')
  await page.locator('#new-password').fill('synthetic-password-123')
  await page.getByLabel('Confirm password').fill('synthetic-password-123')
  await page.getByRole('button', { name: 'Reset password', exact: true }).click()
  await expect(page.getByText('password reset link is invalid or expired')).toBeVisible()
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('public-auth-invalid-reset.png'), fullPage: true })
  expect(browserErrors.filter((error) => error !== 'console: Failed to load resource: the server responded with a status of 400 (Bad Request)')).toEqual([])
})
