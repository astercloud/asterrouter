import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as settings from '@/api/settings'
import EmailTemplateEditor from './EmailTemplateEditor.vue'

vi.mock('@/api/settings', () => ({
  getEmailTemplate: vi.fn(),
  getEmailTemplateCatalog: vi.fn(),
  previewEmailTemplate: vi.fn(),
  restoreEmailTemplate: vi.fn(),
  testEmailTemplate: vi.fn(),
  updateEmailTemplate: vi.fn()
}))

const smtpConfig = {
  smtp_host: 'smtp.example.com', smtp_port: 465, smtp_username: 'mailer', smtp_password: '',
  smtp_from: 'sender@example.com', smtp_from_name: 'AsterRouter', smtp_use_tls: true
}

const verification = {
  event: 'email_verification', locale: 'en-US' as const, subject: 'Verify {{.SiteName}}', html: '<p>{{.ActionURL}}</p>',
  customized: false, placeholders: ['{{.SiteName}}', '{{.ActionURL}}']
}

describe('EmailTemplateEditor', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('en-US')
    vi.mocked(settings.getEmailTemplateCatalog).mockResolvedValue({
      events: [
        { event: 'email_verification', placeholders: verification.placeholders },
        { event: 'password_reset', placeholders: ['{{.ActionURL}}'] }
      ],
      locales: ['en-US', 'zh-CN'],
      templates: [],
      placeholders: verification.placeholders
    })
    vi.mocked(settings.getEmailTemplate).mockResolvedValue(verification)
    vi.mocked(settings.previewEmailTemplate).mockResolvedValue({ subject: 'Verify AsterRouter', html: '<p>https://example.test/action</p>' })
    vi.mocked(settings.updateEmailTemplate).mockResolvedValue({ ...verification, subject: 'Custom subject', customized: true })
    vi.mocked(settings.restoreEmailTemplate).mockResolvedValue(verification)
    Object.defineProperty(window, 'confirm', { configurable: true, value: vi.fn(() => true) })
  })

  it('initializes with a real event and locale and renders a safe preview', async () => {
    const wrapper = mount(EmailTemplateEditor, { props: { smtpConfig }, global: { plugins: [i18n] } })
    await flushPromises()

    expect(settings.getEmailTemplate).toHaveBeenCalledWith('email_verification', 'en-US')
    expect((wrapper.get('#email-template-event').element as HTMLSelectElement).value).toBe('email_verification')
    expect((wrapper.get('#email-template-locale').element as HTMLSelectElement).value).toBe('en-US')
    expect(wrapper.text()).not.toContain(' / ')
    expect(wrapper.get('iframe').attributes('sandbox')).toBe('')
    expect(wrapper.get('iframe').attributes('srcdoc')).toContain('example.test')
  })

  it('switches, saves, restores, and sends a test with the current SMTP form', async () => {
    const wrapper = mount(EmailTemplateEditor, { props: { smtpConfig }, global: { plugins: [i18n] } })
    await flushPromises()

    await wrapper.get('#email-template-subject').setValue('Custom subject')
    const saveButton = wrapper.findAll('.email-template-actions button').find((item) => item.text().includes('Save'))!
    await saveButton.trigger('click')
    await flushPromises()
    expect(settings.updateEmailTemplate).toHaveBeenCalledWith('email_verification', 'en-US', 'Custom subject', verification.html)
    expect(wrapper.text()).toContain('Customized')

    const restoreButton = wrapper.findAll('.email-template-actions button').find((item) => item.text().includes('Restore default'))!
    await restoreButton.trigger('click')
    await flushPromises()
    expect(settings.restoreEmailTemplate).toHaveBeenCalledWith('email_verification', 'en-US')

    await wrapper.get('#email-template-recipient').setValue('recipient@example.com')
    await wrapper.get('.email-template-test button').trigger('click')
    await flushPromises()
    expect(settings.testEmailTemplate).toHaveBeenCalledWith('recipient@example.com', verification.subject, verification.html, smtpConfig)
  })
})
