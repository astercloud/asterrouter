import { expect, test } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
import { captureBrowserErrors, expectNoHorizontalOverflow } from './fixtures'

test('@e2e-marketing-001 official website is public, localized, and responsive', async ({ page }, testInfo) => {
  const errors = captureBrowserErrors(page)
  const captureDesignEvidence = !process.env.ASTER_E2E_EXTERNAL_URL
  await page.goto('/', { waitUntil: 'commit' })

  await expect(page).toHaveURL('/')
  await expect(page.getByRole('heading', { level: 1, name: 'AsterRouter' })).toBeVisible()
  await expect(page.locator('.hero-category')).toHaveText('Enterprise AI access and routing infrastructure')
  await expect(page.getByRole('heading', { level: 2, name: 'Every model request follows the same enterprise decision chain' })).toBeVisible()
  await expect(page.getByRole('region', { name: 'AsterRouter live routing decision preview' })).toBeVisible()
  await expect(page.getByText('Request entered the preferred route')).toBeVisible()
  const nextSectionSignal = await page.locator('.decision-section .section-heading > span').boundingBox()
  expect(nextSectionSignal?.y).toBeLessThan(page.viewportSize()?.height || 0)
  const productImage = page.getByRole('img', { name: 'Actual AsterRouter routing policy workbench' })
  await expect(productImage).toBeVisible()
  await expect.poll(() => productImage.evaluate((image: HTMLImageElement) => image.complete && image.naturalWidth > 0)).toBe(true)
  await expectNoHorizontalOverflow(page)
  if (captureDesignEvidence) {
    await page.screenshot({ path: testInfo.outputPath('marketing-home-en.png'), fullPage: true, animations: 'disabled' })
  }

  if ((page.viewportSize()?.width || 0) <= 1080) {
    await page.getByRole('button', { name: 'Open website navigation' }).click()
    const mobileNav = page.getByRole('navigation', { name: 'Mobile website navigation' })
    await mobileNav.getByLabel('Language').selectOption('zh-CN')
  } else {
    await page.locator('.marketing-locale select').selectOption('zh-CN')
  }
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')
  await expect(page.locator('.hero-category')).toHaveText('企业 AI 访问与路由基础设施')
  await expect(page.getByRole('heading', { level: 2, name: '策略不是一个权重，而是一份完整路由合同' })).toBeVisible()
  await expect(page.getByRole('region', { name: 'AsterRouter 实时路由决策预览' })).toBeVisible()
  await expectNoHorizontalOverflow(page)
  if (captureDesignEvidence) {
    await page.screenshot({ path: testInfo.outputPath('marketing-home-zh.png'), fullPage: true, animations: 'disabled' })
  }

  if (testInfo.project.name === 'chromium-desktop') {
    const results = await new AxeBuilder({ page }).analyze()
    const blocking = results.violations.filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
    expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([])
  }
  expect(errors).toEqual([])
})
