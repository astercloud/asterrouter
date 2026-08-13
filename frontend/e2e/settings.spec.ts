import { expect, test } from '@playwright/test'
import { captureBrowserErrors, envelope, expectNoHorizontalOverflow, loginDemo } from './fixtures'

type RetentionCleanupResult = {
  before: string
  usage_records: number
  gateway_traces: number
  alert_events: number
  audit_logs: number
}

test('@e2e-settings-001 admin settings persist and retention cleanup returns deletion evidence', async ({ page }, testInfo) => {
  const errors = captureBrowserErrors(page)
  await loginDemo(page)
  await page.goto('/console/system')
  await expect(page.getByRole('heading', { level: 1, name: 'System Settings' })).toBeVisible()
  await expectNoHorizontalOverflow(page)

  if (testInfo.project.name !== 'chromium-desktop') {
    expect(errors).toEqual([])
    return
  }

  const token = await page.evaluate(() => localStorage.getItem('asterrouter_admin_token'))
  expect(token).toBeTruthy()
  const headers = { Authorization: `Bearer ${token}` }
  const original = await envelope<Record<string, unknown>>(await page.request.get('/api/v1/console/settings', { headers }))
  const originalRetentionDays = Number(original.data_retention_days)
  const originalLoggingMode = String(original.prompt_logging_mode)
  const changedRetentionDays = originalRetentionDays === 31 ? 32 : 31
  const changedLoggingMode = originalLoggingMode === 'metadata_only' ? 'disabled' : 'metadata_only'

  try {
    await page.getByRole('tab', { name: 'Data backup' }).click()
    const governance = page.locator('.panel').filter({ has: page.getByRole('heading', { name: 'Governance' }) })
    const retentionDays = governance.locator('input[type="number"]')
    const loggingMode = governance.locator('select').first()
    await expect(retentionDays).toHaveValue(String(originalRetentionDays))
    await expect(loggingMode).toHaveValue(originalLoggingMode)

    await retentionDays.fill(String(changedRetentionDays))
    await loggingMode.selectOption(changedLoggingMode)
    const savedResponse = page.waitForResponse((response) => response.url().endsWith('/api/v1/console/settings') && response.request().method() === 'PUT')
    await page.getByRole('button', { name: 'Save settings' }).click()
    expect((await savedResponse).status()).toBe(200)
    await expect(page.getByText('Settings saved', { exact: true })).toBeVisible()

    await page.reload()
    await page.getByRole('tab', { name: 'Data backup' }).click()
    await expect(retentionDays).toHaveValue(String(changedRetentionDays))
    await expect(loggingMode).toHaveValue(changedLoggingMode)

    page.once('dialog', (dialog) => dialog.accept())
    const cleanupResponse = page.waitForResponse((response) => response.url().endsWith('/api/v1/console/settings/retention/cleanup') && response.request().method() === 'POST')
    await governance.getByRole('button', { name: 'Run data cleanup now' }).click()
    const cleanup = await envelope<RetentionCleanupResult>(await cleanupResponse)
    expect(Number.isNaN(Date.parse(cleanup.before))).toBe(false)
    const deleted = cleanup.usage_records + cleanup.gateway_traces + cleanup.alert_events + cleanup.audit_logs
    expect(deleted).toBeGreaterThanOrEqual(0)
    await expect(page.getByText(`Data cleanup completed. ${deleted} records were deleted.`)).toBeVisible()

    await retentionDays.fill(String(originalRetentionDays))
    await loggingMode.selectOption(originalLoggingMode)
    const restoredResponse = page.waitForResponse((response) => response.url().endsWith('/api/v1/console/settings') && response.request().method() === 'PUT')
    await page.getByRole('button', { name: 'Save settings' }).click()
    expect((await restoredResponse).status()).toBe(200)
    await page.reload()
    await page.getByRole('tab', { name: 'Data backup' }).click()
    await expect(retentionDays).toHaveValue(String(originalRetentionDays))
    await expect(loggingMode).toHaveValue(originalLoggingMode)
  } finally {
    await page.goto('/console/system')
    await page.getByRole('tab', { name: 'Data backup' }).click()
    const governance = page.locator('.panel').filter({ has: page.getByRole('heading', { name: 'Governance' }) })
    await governance.locator('input[type="number"]').fill(String(originalRetentionDays))
    await governance.locator('select').first().selectOption(originalLoggingMode)
    const restoredResponse = page.waitForResponse((response) => response.url().endsWith('/api/v1/console/settings') && response.request().method() === 'PUT')
    await page.getByRole('button', { name: 'Save settings' }).click()
    expect((await restoredResponse).status()).toBe(200)
  }

  expect(errors).toEqual([])
})
