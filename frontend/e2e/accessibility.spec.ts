import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'
import { captureBrowserErrors, envelope, loginDemo, loginTestPrincipal, loginUser, registerUsers } from './fixtures'

async function loginThroughPage(page: Page, email: string, password: string, redirect: string): Promise<void> {
  await page.goto(`/login?redirect=${encodeURIComponent(redirect)}`)
  await page.getByLabel('Username').fill(email)
  await page.locator('input#password').fill(password)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page).toHaveURL(new RegExp(`${redirect}$`))
}

async function focusWithTab(page: Page, target: ReturnType<Page['getByRole']>): Promise<void> {
  for (let index = 0; index < 80; index++) {
    await page.keyboard.press('Tab')
    if (await target.evaluate((element) => document.activeElement === element)) return
  }
  await expect(target).toBeFocused()
}

test('@e2e-a11y-console-001 console overview has no serious accessibility violations', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The semantic audit runs once; layout coverage runs in every Chromium viewport.')

  await loginDemo(page)
  const results = await new AxeBuilder({ page }).analyze()
  const blocking = results.violations.filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
  expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([])
})

test('@e2e-a11y-session-001 enterprise member sessions are isolated and keyboard-operable', async ({ browser, page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The cross-session workflow is viewport-independent and runs once on desktop.')

  const errors = captureBrowserErrors(page)
  await loginDemo(page)
  const adminToken = await loginTestPrincipal(page)
  const runID = `${testInfo.project.name}-${Date.now()}`
  const password = 'synthetic-password-123'
  const [memberA, memberB] = await registerUsers(page, adminToken, [
    { email: `member-a-${runID}@example.test`, password, displayName: 'Enterprise Member A' },
    { email: `member-b-${runID}@example.test`, password, displayName: 'Enterprise Member B' }
  ])

  await page.context().clearCookies()
  await page.evaluate(() => localStorage.clear())
  await loginThroughPage(page, memberA.email, password, '/portal/overview')
  await expect(page.getByRole('heading', { level: 1, name: 'Employee Portal' })).toBeVisible()
  const memberAToken = await loginUser(page, memberA.email, password)

  const origin = new URL(page.url()).origin
  const otherContext = await browser.newContext()
  const otherPage = await otherContext.newPage()
  try {
    await loginThroughPage(otherPage, memberB.email, password, `${origin}/portal/overview`)
    await expect(otherPage.getByRole('heading', { level: 1, name: 'Employee Portal' })).toBeVisible()
    await otherPage.goto(`${origin}/portal/account`)
    await expect(otherPage.getByLabel('Email')).toHaveValue(memberB.email)
  } finally {
    await otherContext.close()
  }

  await page.goto('/portal/account')
  await expect(page.getByLabel('Email')).toHaveValue(memberA.email)
  expect([403, 404]).toContain((await page.request.get('/api/v1/console/dashboard', { headers: { Authorization: `Bearer ${memberAToken}` } })).status())

  await page.goto('/console/workbench')
  await expect(page).toHaveURL(/\/portal\/overview$/)

  const themeButton = page.getByRole('button', { name: 'Dark mode' })
  await focusWithTab(page, themeButton)
  await page.keyboard.press('Enter')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')

  const usageLink = page.getByRole('main').getByRole('link', { name: 'Usage', exact: true })
  await focusWithTab(page, usageLink)
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/portal\/usage$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Employee Portal' })).toBeVisible()
  expect(errors).toEqual([])
})
