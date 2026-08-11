import { expect, test } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
import { captureBrowserErrors, expectNoHorizontalOverflow } from './fixtures'

test('@marketing official website is public, localized, and responsive', async ({ page }, testInfo) => {
  const errors = captureBrowserErrors(page)
  await page.goto('/', { waitUntil: 'commit' })

  await expect(page).toHaveURL('/')
  await expect(page.getByRole('heading', { level: 1, name: 'AsterRouter' })).toBeVisible()
  await expect(page.locator('.hero-category')).toHaveText('Enterprise AI access and routing infrastructure')
  await expect(page.getByRole('heading', { level: 2, name: 'Every model request follows the same enterprise decision chain' })).toBeVisible()
  const productImage = page.getByRole('img', { name: 'Actual AsterRouter routing policy workbench' })
  await expect(productImage).toBeVisible()
  expect(await productImage.evaluate((image: HTMLImageElement) => image.complete && image.naturalWidth > 0)).toBe(true)
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('marketing-home-en.png'), fullPage: true })

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
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('marketing-home-zh.png'), fullPage: true })

  if (testInfo.project.name === 'chromium-desktop') {
    const results = await new AxeBuilder({ page }).analyze()
    const blocking = results.violations.filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
    expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([])
  }
  expect(errors).toEqual([])
})
