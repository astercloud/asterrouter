import { expect, test } from '@playwright/test'
import { captureBrowserErrors, envelope, expectNoHorizontalOverflow, loginDemo } from './fixtures'

test('@artifact-sink manages customer-owned object storage without exposing credentials', async ({ page }, testInfo) => {
  const errors = captureBrowserErrors(page)
  await loginDemo(page)
  const pluginID = 'com.asterrouter.artifact.s3-compatible-sink'
  const token = await page.evaluate(() => localStorage.getItem('asterrouter_admin_token') || '')
  const artifactSinkURL = `/api/v1/console/plugins/${encodeURIComponent(pluginID)}/artifact-sinks`
  const existing = await envelope<Array<{ id: string }>>(await page.request.get(artifactSinkURL, {
    headers: { Authorization: `Bearer ${token}` }
  }))
  for (const destination of existing.filter((item) => item.id.startsWith('browser-'))) {
    await envelope(await page.request.delete(`${artifactSinkURL}/${encodeURIComponent(destination.id)}`, {
      headers: { Authorization: `Bearer ${token}` }
    }))
  }
  await page.goto('/console/plugins')
  const mobileViewport = (page.viewportSize()?.width || 0) <= 640
  await page.getByRole('button', { name: 'Plugin registry', exact: true }).click()
  await page.getByRole('button', { name: /S3-compatible Artifact Delivery/ }).click()
  await expect(page.getByRole('heading', { level: 2, name: 'S3-compatible Artifact Delivery' })).toBeVisible()
  await expect(page.getByText('Delivery destinations', { exact: true })).toBeVisible()

  const suffix = `${testInfo.project.name}-${Date.now()}`.replace(/[^A-Za-z0-9._:-]/g, '-')
  const sinkID = `browser-${suffix}`
  const sinkName = `Browser media ${suffix}`
  const accessKey = `synthetic-access-${suffix}`
  const secretKey = `synthetic-secret-${suffix}`
  const sessionToken = `synthetic-session-${suffix}`

  await page.getByRole('button', { name: 'Add destination' }).click()
  await page.getByLabel('Destination ID').fill(sinkID)
  await page.getByLabel('Display name').fill(sinkName)
  await page.getByLabel('Provider').selectOption('r2')
  await page.getByLabel('Region').fill('auto')
  await page.getByLabel('HTTPS endpoint').fill('https://account.r2.cloudflarestorage.com')
  await page.getByLabel('Bucket').fill('browser-media')
  await page.getByLabel('Object prefix').fill('generated')
  await page.getByLabel('Public reference base URL').fill('https://media.example/generated')
  await page.getByLabel('Profile scope').selectOption('personal')
  await page.getByLabel('Access key').fill(accessKey)
  await page.getByLabel('Secret key').fill(secretKey)
  await page.getByLabel('Session token').fill(sessionToken)
  await page.getByRole('button', { name: 'Save' }).click()

  await expect(page.getByText('Delivery destination saved')).toBeVisible()
  await expect(page.getByText(sinkName, { exact: true })).toBeVisible()
  await expect(page.getByText(accessKey, { exact: true })).toHaveCount(0)
  await expect(page.getByText(secretKey, { exact: true })).toHaveCount(0)
  await expect(page.getByText(sessionToken, { exact: true })).toHaveCount(0)

  await page.getByRole('button', { name: `Edit ${sinkName}` }).click()
  await expect(page.getByLabel('Access key')).toHaveValue('')
  await expect(page.getByLabel('Secret key')).toHaveValue('')
  await expect(page.getByLabel('Remove the stored session token')).toBeVisible()
  await page.getByLabel('Remove the stored session token').check()
  await page.getByRole('button', { name: 'Save' }).click()
  await expect(page.getByText('Delivery destination saved')).toBeVisible()

  await page.getByRole('button', { name: `Edit ${sinkName}` }).click()
  await expect(page.getByLabel('Remove the stored session token')).toHaveCount(0)
  await page.getByRole('button', { name: 'Close', exact: true }).click()

  await page.reload()
  await page.getByRole('button', { name: 'Plugin registry', exact: true }).click()
  await page.getByRole('button', { name: /S3-compatible Artifact Delivery/ }).click()
  await expect(page.getByText(sinkName, { exact: true })).toBeVisible()
  await expectNoHorizontalOverflow(page)
  if (mobileViewport) {
    await page.locator('[data-artifact-sinks]').screenshot({ path: testInfo.outputPath('artifact-sink.png') })
  } else {
    await page.screenshot({ path: testInfo.outputPath('artifact-sink.png'), fullPage: true })
  }

  if (mobileViewport) {
    await page.getByRole('button', { name: 'Open navigation' }).click()
  }
  await page.getByRole('button', { name: 'Dark mode' }).click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  if (mobileViewport) {
    await page.locator('.sidebar-mobile-close').click()
    const sidebar = page.locator('.admin-sidebar')
    await expect(sidebar).not.toHaveClass(/mobile-open/)
    await expect(page.locator('.sidebar-overlay')).toHaveCount(0)
    await expect.poll(() => sidebar.evaluate((element) => getComputedStyle(element).transform)).toBe('matrix(1, 0, 0, 1, -256, 0)')
  }
  await page.getByLabel('Language').selectOption('zh-CN')
  await expect(page.getByRole('heading', { level: 1, name: '插件中心' })).toBeVisible()
  await expect(page.getByText('交付目标', { exact: true })).toBeVisible()
  await expectNoHorizontalOverflow(page)
  if (mobileViewport) {
    await page.locator('[data-artifact-sinks]').screenshot({ path: testInfo.outputPath('artifact-sink-zh-dark.png') })
  } else {
    await page.screenshot({ path: testInfo.outputPath('artifact-sink-zh-dark.png'), fullPage: true })
  }
  await page.getByLabel('语言').selectOption('en-US')

  page.once('dialog', (dialog) => dialog.accept())
  await page.getByRole('button', { name: `Delete ${sinkName}` }).click()
  await expect(page.getByText('Delivery destination deleted')).toBeVisible()
  await expect(page.getByText(sinkName, { exact: true })).toHaveCount(0)
  expect(errors).toEqual([])
})
