import { expect, test } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
import { captureBrowserErrors, expectNoHorizontalOverflow, loginDemo } from './fixtures'

test('@routing-policy enterprise routing policy workbench persists and remains responsive', async ({ page }, testInfo) => {
  const errors = captureBrowserErrors(page)
  await loginDemo(page)
  await page.goto('/console/policies/routing')

  await expect(page.getByRole('heading', { level: 1, name: 'Routing Policies' })).toBeVisible()
  await expect(page.getByRole('heading', { level: 2, name: 'Routing policy list' })).toBeVisible()
  await expect(page.getByRole('heading', { level: 2, name: 'Choose a decision preference' })).toBeVisible()
  await expect(page.getByRole('heading', { level: 2, name: 'How one request is decided' })).toBeVisible()
  await expectNoHorizontalOverflow(page)

  const runID = testInfo.project.name
  await page.getByRole('button', { name: 'New policy' }).click()
  await page.getByLabel('Policy name').fill('Enterprise production routing')
  await page.getByLabel('Route group').fill(`production-${runID}`)
  await page.getByRole('radio', { name: /Stability first/ }).click()
  await page.getByRole('button', { name: 'Save policy' }).click()
  await expect(page.getByText('Routing policy created')).toBeVisible()

  await page.reload()
  await expect(page.getByText('Enterprise production routing', { exact: true }).first()).toBeVisible()
  await expect(page.getByText(`production-${runID}`, { exact: true }).first()).toBeVisible()
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('routing-policy-light-en.png'), fullPage: true })

  await page.getByLabel('Language').selectOption('zh-CN')
  if ((page.viewportSize()?.width || 0) <= 640) {
    await page.getByRole('button', { name: '打开导航' }).click()
  }
  await page.getByRole('button', { name: '深色模式' }).click()
  if ((page.viewportSize()?.width || 0) <= 640) {
    await page.locator('.sidebar-mobile-close').click()
  }
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await expect(page.getByRole('heading', { level: 1, name: '路由策略' })).toBeVisible()
  await expect(page.getByRole('heading', { level: 2, name: '一次请求如何决策' })).toBeVisible()
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('routing-policy-dark-zh.png'), fullPage: true })
  if (testInfo.project.name === 'chromium-desktop') {
    const results = await new AxeBuilder({ page }).analyze()
    const blocking = results.violations.filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
    expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([])
  }
  expect(errors).toEqual([])
})
