import { expect, test, type Page } from '@playwright/test'
import { captureBrowserErrors, expectNoHorizontalOverflow, loginDemo } from './fixtures'

type MailboxMessage = {
  from: string
  to: string
  subject: string
  body: string
}

async function latestMessage(page: Page, recipient: string): Promise<MailboxMessage> {
  const mailAPI = process.env.ASTER_E2E_MAIL_API_URL || 'http://127.0.0.1:29002'
  let message: MailboxMessage | undefined
  await expect.poll(async () => {
    const response = await page.request.get(`${mailAPI}/__test/messages?recipient=${encodeURIComponent(recipient)}`)
    expect(response.status()).toBe(200)
    const body = await response.json() as { messages: MailboxMessage[] }
    message = body.messages.at(-1)
    return message?.to || ''
  }).toBe(recipient)
  return message!
}

test('@e2e-email-001 email settings manage SMTP credentials and localized templates', async ({ page }, testInfo) => {
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
    const html = editor.getByLabel('HTML template')
    const originalSubject = await subject.inputValue()
    const originalHTML = await html.inputValue()
    const originalSMTP = {
      host: await page.locator('input[name="smtp-host"]').inputValue(),
      port: await page.locator('input[name="smtp-port"]').inputValue(),
      from: await page.locator('input[name="smtp-from"]').inputValue(),
      fromName: await page.locator('input[name="smtp-from-name"]').inputValue()
    }
    const runID = Date.now()
    const smtpPort = process.env.ASTER_E2E_SMTP_PORT || '29001'
    const sender = `e2e-sender-${runID}@example.test`
    const smtpRecipient = `e2e-smtp-${runID}@example.test`
    const templateRecipient = `e2e-template-${runID}@example.test`
    const smtpPanel = page.locator('.panel').filter({ has: page.getByRole('heading', { name: 'Email settings' }) })

    await page.locator('input[name="smtp-host"]').fill('127.0.0.1')
    await page.locator('input[name="smtp-port"]').fill(smtpPort)
    await page.locator('input[name="smtp-from"]').fill(sender)
    await page.locator('input[name="smtp-from-name"]').fill('AsterRouter E2E')
    await smtpPanel.locator('.auth-provider-header input[type="checkbox"]').uncheck()
    await smtpPanel.getByRole('button', { name: 'Test connection' }).click()
    await expect(page.getByText('SMTP connection succeeded')).toBeVisible()

    await smtpPanel.locator('.smtp-test-controls input[type="email"]').fill(smtpRecipient)
    await smtpPanel.getByRole('button', { name: 'Send test email' }).click()
    await expect(page.getByText('SMTP test email sent')).toBeVisible()
    expect(await latestMessage(page, smtpRecipient)).toMatchObject({
      from: sender,
      to: smtpRecipient,
      subject: 'AsterRouter SMTP test',
      body: 'SMTP configuration is working.'
    })

    const templateSubject = `E2E ${runID} for {{.UserName}}`
    const templateHTML = `<h2>E2E ${runID}</h2><p>{{.SiteName}} / {{.UserName}} / {{.ActionURL}}</p>`
    await subject.fill(templateSubject)
    await html.fill(templateHTML)
    await editor.getByLabel('Test recipient').fill(templateRecipient)
    await editor.getByRole('button', { name: 'Send test', exact: true }).click()
    await expect(editor.getByText('Template test email sent')).toBeVisible()
    expect(await latestMessage(page, templateRecipient)).toMatchObject({
      from: sender,
      to: templateRecipient,
      subject: `E2E ${runID} for Enterprise User`,
      body: `<h2>E2E ${runID}</h2><p>AsterRouter / Enterprise User / https://example.test/action</p>`
    })

    await subject.fill(`${originalSubject} [browser test]`)
    await html.fill(originalHTML)
    await editor.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(editor.getByText('Customized')).toBeVisible()
    await page.reload()
    await page.getByRole('tab', { name: 'Email settings' }).click()
    await expect(editor.getByLabel('Email subject')).toHaveValue(`${originalSubject} [browser test]`)
    await expect(page.locator('input[name="smtp-host"]')).toHaveValue(originalSMTP.host)
    await expect(page.locator('input[name="smtp-port"]')).toHaveValue(originalSMTP.port)
    await expect(page.locator('input[name="smtp-from"]')).toHaveValue(originalSMTP.from)
    await expect(page.locator('input[name="smtp-from-name"]')).toHaveValue(originalSMTP.fromName)
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
