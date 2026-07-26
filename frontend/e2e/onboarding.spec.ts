import { expect, test, type Page, type TestInfo } from '@playwright/test'
import { captureBrowserErrors, expectNoHorizontalOverflow, loginDemo } from './fixtures'

type ClientCase = {
  id: 'codex' | 'claude_code' | 'openai_sdk' | 'anthropic_sdk'
  label: string
  configMarker: string
  versions: string[]
}

const clients: ClientCase[] = [
  { id: 'codex', label: 'Codex', configMarker: 'wire_api = "responses"', versions: ['0.145.0', '0.144.6'] },
  { id: 'claude_code', label: 'Claude Code', configMarker: 'ANTHROPIC_BASE_URL', versions: ['2.1.220', '1.0.128'] },
  { id: 'openai_sdk', label: 'OpenAI SDK', configMarker: 'OPENAI_BASE_URL', versions: ['6.49.0', '5.23.2', '2.48.0', '1.109.1'] },
  { id: 'anthropic_sdk', label: 'Anthropic SDK', configMarker: 'ANTHROPIC_BASE_URL', versions: ['0.115.0', '0.114.0', '0.120.0', '0.119.0'] }
]

async function expectCompatibilityEvidence(page: Page, client: ClientCase) {
  const evidence = page.locator('.onboarding-compatibility-band')
  await expect(evidence.getByText('Compatibility evidence')).toBeVisible()
  await expect(evidence).toContainText('Protocol verified')
  await expect(evidence).toContainText('Official client runtime was not executed')
  await expect(evidence).not.toContainText('Client runtime verified')
  for (const version of client.versions) {
    await expect(evidence).toContainText(`v${version}`)
  }
}

async function createApplicationThroughUI(page: Page, testInfo: TestInfo, client: ClientCase) {
  const upstreamPort = process.env.ASTER_E2E_UPSTREAM_PORT || '19000'
  const runID = `${client.id}-${testInfo.project.name}-${Date.now()}`.replace(/[^a-z0-9_-]/gi, '-')
  const publicModel = `first-access-${runID}`

  await page.goto('/admin/onboarding')
  await expect(page.getByRole('heading', { level: 1, name: 'First Access' })).toBeVisible()
  await page.getByLabel('Base URL').fill(`http://127.0.0.1:${upstreamPort}/v1`)
  await page.getByLabel('Upstream model').fill('upstream-model')
  await page.getByLabel('Access credential').fill('synthetic-onboarding-secret')
  await page.getByRole('button', { name: 'Connect and check' }).click()

  await expect(page.getByRole('heading', { level: 2, name: 'Publish team model' })).toBeVisible()
  await page.getByLabel('Model identifier').fill(publicModel)
  await page.getByLabel('Display name').fill(`First access ${client.label}`)
  await page.getByRole('button', { name: 'Publish model' }).click()

  await expect(page.getByRole('heading', { level: 2, name: 'Create application' })).toBeVisible()
  await page.getByLabel('Application name').fill(`Application ${runID}`)
  await page.getByRole('button', { name: 'Create application and credential' }).click()

  await expect(page.getByRole('heading', { level: 2, name: 'Configure and verify client' })).toBeVisible()
  const credentialInput = page.getByRole('textbox', { name: 'Application credential', exact: true })
  const credential = await credentialInput.inputValue()
  expect(credential).toMatch(/^ar_/)
  expect(await page.evaluate(() => Object.values(localStorage))).not.toContain(credential)
  expect(await page.evaluate(() => Object.values(localStorage))).not.toContain('synthetic-onboarding-secret')

  await page.reload()
  await expect(page.getByRole('heading', { level: 2, name: 'Configure and verify client' })).toBeVisible()
  await expect(credentialInput).toHaveValue(credential)
  await page.getByRole('button', { name: client.label, exact: true }).click()
  await expect(page.locator('.onboarding-config-band .code-block')).toContainText(client.configMarker)
  await expectCompatibilityEvidence(page, client)
  return { credential, publicModel }
}

for (const client of clients) {
  test(`@j01 first-access browser journey verifies ${client.label}`, async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'chromium-desktop', 'Each client workflow runs once on desktop; responsive coverage is separate.')
    const browserErrors = captureBrowserErrors(page)
    await loginDemo(page)
    await createApplicationThroughUI(page, testInfo, client)

    await page.getByRole('button', { name: 'Run real verification' }).click()
    await expect(page.getByText('Verification succeeded')).toBeVisible()
    await expect(page.getByText(/Operation: aio_/)).toBeVisible()
    const traceLink = page.getByRole('link', { name: 'Open Trace' })
    await expect(traceLink).toBeVisible()
    await traceLink.click()
    await expect(page).toHaveURL(/\/admin\/traces\?q=trace_/)
    await expect(page.getByRole('heading', { level: 1, name: 'Gateway Trace' })).toBeVisible()
    expect(browserErrors).toEqual([])
  })
}

test('@j01 first-access layout remains usable across locale, theme, and viewport', async ({ page }, testInfo) => {
  const browserErrors = captureBrowserErrors(page)
  await loginDemo(page)
  await page.goto('/admin/onboarding')
  await expect(page.getByRole('heading', { level: 1, name: 'First Access' })).toBeVisible()
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('first-access-light-en.png'), fullPage: true })

  await createApplicationThroughUI(page, testInfo, clients[0])
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('compatibility-light-en.png'), fullPage: true })

  await page.getByLabel('Language').selectOption('zh-CN')
  await expect(page.getByRole('heading', { level: 1, name: '首次接入' })).toBeVisible()
  const isMobile = (page.viewportSize()?.width || 0) <= 640
  if (isMobile) {
    await page.getByRole('button', { name: '打开导航' }).click()
  }
  await page.getByRole('button', { name: '深色模式' }).click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  if (isMobile) {
    await page.locator('.sidebar-mobile-close').click()
    await expect.poll(async () => page.locator('.admin-sidebar').evaluate((element) => element.getBoundingClientRect().right)).toBeLessThanOrEqual(0)
  }
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('first-access-dark-zh.png'), fullPage: true })
  expect(browserErrors).toEqual([])
})
