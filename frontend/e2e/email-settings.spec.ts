import { expect, test } from '@playwright/test'
import { captureBrowserErrors, expectNoHorizontalOverflow, loginDemo } from './fixtures'

test('email settings manage SMTP credentials and localized templates', async ({ page }, testInfo) => {
  const errors = captureBrowserErrors(page)
  await loginDemo(page)
  await page.goto('/console/system')
  await page.getByRole('tab', { name: 'Email settings' }).click()

  const editor = page.locator('.email-template-editor')
  await expect(editor.getByRole('heading', { name: 'Email notification templates' })).toBeVisible()
  await expect(editor.getByLabel('Notification event')).toHaveValue('email_verification')
  await expect(editor.getByLabel('Template locale')).toHaveValue('en-US')
  await expect(editor.locator('iframe')).toHaveAttribute('sandbox', '')
  await expect(editor.locator('iframe')).toHaveAttribute('srcdoc', /Verify email/)
  await expect(editor.frameLocator('iframe').getByRole('heading', { name: 'Verify email' })).toBeVisible()
  await expect(page.locator('input[name="smtp-username"]')).toHaveAttribute('autocomplete', 'off')
  await expect(page.locator('input[name="smtp-password"]')).toHaveAttribute('autocomplete', 'new-password')
  await expect(page.locator('input[name="smtp-username"]')).toHaveValue('')
  await expect(page.locator('input[name="smtp-password"]')).toHaveValue('')
  await expectNoHorizontalOverflow(page)

  if (testInfo.project.name === 'chromium-desktop') {
    const subject = editor.getByLabel('Email subject')
    const originalSubject = await subject.inputValue()
    await subject.fill(`${originalSubject} [browser test]`)
    await editor.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(editor.getByText('Customized')).toBeVisible()
    await page.reload()
    await page.getByRole('tab', { name: 'Email settings' }).click()
    await expect(editor.getByLabel('Email subject')).toHaveValue(`${originalSubject} [browser test]`)
    page.once('dialog', (dialog) => dialog.accept())
    await editor.getByRole('button', { name: 'Restore default' }).click()
    await expect(editor.getByLabel('Email subject')).toHaveValue(originalSubject)
    await expect(editor.getByText('Customized')).toHaveCount(0)
  }

  await page.evaluate(() => {
    localStorage.setItem('asterrouter_locale', 'zh-CN')
    localStorage.setItem('asterrouter_theme', 'dark')
  })
  await page.reload()
  await page.getByRole('tab', { name: '邮件设置' }).click()
  await expect(editor.getByRole('heading', { name: '邮件通知模板' })).toBeVisible()
  await expect(editor.getByLabel('通知事件')).toHaveValue('email_verification')
  await expect(editor.frameLocator('iframe').getByRole('heading', { name: '验证邮箱' })).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('email-settings-zh-dark.png'), fullPage: true })
  const unexpectedErrors = errors.filter((message) => !message.includes("Blocked script execution in 'about:srcdoc'") || !message.includes("frame is sandboxed"))
  expect(unexpectedErrors).toEqual([])
})
