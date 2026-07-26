import { createHmac } from 'node:crypto'
import { expect, test, type Page } from '@playwright/test'
import { captureBrowserErrors, envelope, expectNoHorizontalOverflow } from './fixtures'

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
  await page.getByRole('button', { name: 'Sign out' }).click()
  await expect(page).toHaveURL(/\/login$/)
}

async function startPasswordLogin(page: Page, email: string, password: string) {
  await page.goto('/login')
  await page.getByLabel('Username').fill(email)
  await page.locator('#password').fill(password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
}

test('@auth registration, email verification, TOTP, and recovery-code sign-in work end to end', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The stateful authentication journey runs once; public layouts run in every viewport.')
  test.setTimeout(90_000)

  const browserErrors = captureBrowserErrors(page)
  const adminLogin = await envelope<{ access_token: string }>(await page.request.post('/api/v1/auth/login', {
    data: { username: 'demo', password: 'demo', agreement_accepted: true }
  }))
  const adminHeaders = { Authorization: `Bearer ${adminLogin.access_token}` }
  const settings = await envelope<Record<string, unknown>>(await page.request.get('/api/v1/admin/settings', { headers: adminHeaders }))
  const email = `e2e-auth-${Date.now()}@example.test`
  const password = 'synthetic-password-123'

  try {
    await envelope(await page.request.put('/api/v1/admin/settings', {
      headers: adminHeaders,
      data: {
        ...settings,
        registration_enabled: true,
        email_verify_enabled: true,
        password_reset_enabled: true,
        totp_enabled: true,
        turnstile_enabled: false,
        public_base_url: 'https://router.example.test',
        smtp_host: 'smtp.example.test',
        smtp_port: 587,
        smtp_from: 'noreply@example.test'
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
    const registrationBody = await registered.json() as { data: { verification_token?: string; verification_required: boolean } }
    expect(registrationBody.data.verification_required).toBe(true)
    expect(registrationBody.data.verification_token).toBeTruthy()
    await expect(page.getByText('Your account has been created. Check your email to verify it before signing in.')).toBeVisible()

    const verificationResponse = page.waitForResponse((response) => response.url().endsWith('/api/v1/auth/verify-email'))
    await page.goto(`/verify-email?token=${encodeURIComponent(registrationBody.data.verification_token || '')}`)
    expect((await verificationResponse).status()).toBe(200)
    await expect(page.getByText('Email verified. You can now sign in.')).toBeVisible()
    await page.getByRole('button', { name: 'Back to sign in' }).click()

    await page.getByLabel('Username').fill(email)
    await page.locator('#password').fill(password)
    await page.getByRole('button', { name: 'Sign in', exact: true }).click()
    await expect(page).not.toHaveURL(/\/login/)
    const entryPath = new URL(page.url()).pathname
    const surface = entryPath.split('/').filter(Boolean)[0]
    expect(surface).toBeTruthy()

    await page.goto(`/${surface}/account`)
    await page.getByRole('tab', { name: 'Security' }).click()
    await page.locator('#account-totp-current-password').fill(password)
    await page.getByRole('button', { name: 'Set up authenticator' }).click()
    const secret = (await page.locator('.totp-setup-copy > code').first().textContent())?.trim() || ''
    expect(secret).toMatch(/^[A-Z2-7]+$/)
    await page.locator('#account-totp-code').fill(currentTOTP(secret))
    await page.getByRole('button', { name: 'Confirm and enable' }).click()
    await expect(page.getByText('Two-factor authentication enabled. Save your recovery codes now.')).toBeVisible()
    const recoveryCode = (await page.locator('.recovery-grid code').first().textContent())?.trim() || ''
    expect(recoveryCode).toMatch(/^[A-Z2-7]{6}-[A-Z2-7]{6}$/)
    await page.screenshot({ path: testInfo.outputPath('totp-enabled.png'), fullPage: true })

    await signOut(page)
    await startPasswordLogin(page, email, password)
    await expect(page.getByRole('heading', { level: 2, name: 'Two-factor verification' })).toBeVisible()
    await page.locator('#mfa-code').fill(currentTOTP(secret))
    await page.getByRole('button', { name: 'Verify and sign in' }).click()
    await expect(page).toHaveURL(new RegExp(`${entryPath}$`))

    await signOut(page)
    await startPasswordLogin(page, email, password)
    await expect(page.locator('#mfa-code')).toHaveAttribute('maxlength', '13')
    await page.locator('#mfa-code').fill(recoveryCode)
    await page.getByRole('button', { name: 'Verify and sign in' }).click()
    await expect(page).toHaveURL(new RegExp(`${entryPath}$`))
    expect(browserErrors).toEqual([])
  } finally {
    await envelope(await page.request.put('/api/v1/admin/settings', { headers: adminHeaders, data: settings }))
  }
})

test('@auth public authentication pages remain usable and reject invalid links', async ({ page }, testInfo) => {
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
