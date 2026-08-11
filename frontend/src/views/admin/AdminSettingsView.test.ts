import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as settings from '@/api/settings'
import * as system from '@/api/system'
import AdminSettingsView from './AdminSettingsView.vue'

const loadPublicSettingsMock = vi.fn()

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ loadPublicSettings: loadPublicSettingsMock })
}))

vi.mock('@/api/settings', () => ({
  getAdminSettings: vi.fn(),
  getDefaultEmailTemplates: vi.fn(),
  getEmailTemplate: vi.fn(),
  getEmailTemplateCatalog: vi.fn(),
  previewEmailTemplate: vi.fn(),
  restoreEmailTemplate: vi.fn(),
  runRetentionCleanup: vi.fn(),
  testEmailTemplate: vi.fn(),
  testSMTP: vi.fn(),
  testSMTPConnection: vi.fn(),
  updateEmailTemplate: vi.fn(),
  updateAdminSettings: vi.fn()
}))

vi.mock('@/api/system', () => ({
  checkSystemUpdates: vi.fn(),
  createDiagnosticBundle: vi.fn(),
  createSystemBackup: vi.fn(),
  downloadDiagnosticBundle: vi.fn(),
  downloadS3Backup: vi.fn(),
  downloadSystemBackup: vi.fn(),
  listS3Backups: vi.fn(),
  listSystemBackups: vi.fn(),
  performSystemUpdate: vi.fn(),
  restartSystem: vi.fn(),
  restoreS3Backup: vi.fn(),
  restoreSystemBackup: vi.fn(),
  rollbackSystemUpdate: vi.fn(),
  testBackupS3: vi.fn(),
}))

const loadedSettings = {
  version: '0.9.0-test',
  storage_mode: 'memory',
  public_base_url: 'https://router.example.test',
  gateway_base_path: '/v1',
  demo_mode: false,
  email_templates: [],
  runtime_restart_required: false,
  runtime_restart_reasons: [],
  auth_source_defaults: {},
  registration_enabled: true,
  email_verify_enabled: true,
  password_reset_enabled: true,
  invitation_required: true,
  allowed_email_domains: ['example.com'],
  invitation_codes: ['INVITE-ONE'],
  turnstile_enabled: true,
  turnstile_site_key: 'site-key',
  turnstile_secret_key: '',
  turnstile_configured: true,
  trusted_proxy_headers: true,
  trusted_proxy_cidrs: ['10.0.0.0/8'],
  smtp_host: 'smtp.example.com',
  smtp_port: 465,
  smtp_username: 'mailer',
  smtp_password: '',
  smtp_from: 'noreply@example.com',
  smtp_from_name: 'AsterRouter',
  smtp_use_tls: true,
  smtp_configured: true
}

describe('AdminSettingsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('en-US')
    vi.mocked(settings.getAdminSettings).mockResolvedValue(structuredClone(loadedSettings) as never)
    vi.mocked(settings.getDefaultEmailTemplates).mockResolvedValue([])
    vi.mocked(settings.getEmailTemplateCatalog).mockResolvedValue({
      events: [{ event: 'email_verification', placeholders: ['{{.SiteName}}'] }],
      locales: ['en-US'], templates: [{ event: 'email_verification', locale: 'en-US', customized: false }], placeholders: ['{{.SiteName}}']
    })
    vi.mocked(settings.getEmailTemplate).mockResolvedValue({
      event: 'email_verification', locale: 'en-US', subject: 'Verify {{.SiteName}}', html: '<p>Verify</p>', customized: false, placeholders: ['{{.SiteName}}']
    })
    vi.mocked(settings.previewEmailTemplate).mockResolvedValue({ subject: 'Verify AsterRouter', html: '<p>Verify</p>' })
    vi.mocked(settings.updateAdminSettings).mockResolvedValue(structuredClone(loadedSettings) as never)
    vi.mocked(system.checkSystemUpdates).mockResolvedValue({ has_update: false, source: 'none' } as never)
    vi.mocked(system.listSystemBackups).mockResolvedValue([])
    vi.mocked(system.listS3Backups).mockResolvedValue([])
    window.history.replaceState({}, '', '/console/system')
  })

  it('opens on general settings and supports keyboard tab navigation', async () => {
    const wrapper = mount(AdminSettingsView, { global: { plugins: [i18n] } })
    await flushPromises()

    const tabs = wrapper.findAll('[role="tab"]')
    expect(tabs).toHaveLength(8)
    expect(tabs[0]?.attributes('aria-selected')).toBe('true')
    expect(wrapper.get('h1').text()).toBe('System Settings')

    await tabs[0]?.trigger('keydown', { key: 'ArrowRight' })
    await flushPromises()
    expect(wrapper.get('#settings-tab-terms').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[role="tabpanel"]').attributes('aria-labelledby')).toBe('settings-tab-terms')

    wrapper.unmount()
  })

  it('keeps gateway settings and a save action without deployment mode switching', async () => {
    const wrapper = mount(AdminSettingsView, { global: { plugins: [i18n] } })
    await flushPromises()

    await wrapper.get('#settings-tab-gateway').trigger('click')
    expect(wrapper.findAll('input[type="radio"], input[name*="profile" i], input[name*="deployment" i]')).toHaveLength(0)
    const saveBar = wrapper.get('[data-section="settings-save-bar"]')
    expect(saveBar.text()).toContain('Deployment & gateway')
    expect(saveBar.text()).toContain('Save settings')

    await saveBar.get('button').trigger('click')
    await flushPromises()
    expect(settings.updateAdminSettings).toHaveBeenCalledTimes(1)

    wrapper.unmount()
  })

  it('loads and saves registration, verification, proxy, Turnstile, and SMTP settings', async () => {
    const wrapper = mount(AdminSettingsView, { global: { plugins: [i18n] } })
    await flushPromises()

    const fieldControl = (label: string, control: 'input' | 'textarea') => {
      const field = wrapper.findAll('.field').find((item) => item.find('label').text() === label)
      expect(field, `missing settings field: ${label}`).toBeDefined()
      return field!.get(control).element as HTMLInputElement | HTMLTextAreaElement
    }

    await wrapper.get('#settings-tab-features').trigger('click')
    expect(wrapper.text()).toContain('Password recovery')
    expect(fieldControl('Allowed email domains', 'textarea').value).toBe('example.com')
    expect(fieldControl('Invitation codes', 'textarea').value).toBe('INVITE-ONE')

    await wrapper.get('#settings-tab-security').trigger('click')
    expect(fieldControl('Site Key', 'input').value).toBe('site-key')
    expect(fieldControl('Trusted proxy CIDRs', 'textarea').value).toBe('10.0.0.0/8')

    await wrapper.get('#settings-tab-email').trigger('click')
    expect(fieldControl('SMTP Host', 'input').value).toBe('smtp.example.com')
    expect(fieldControl('Sender name', 'input').value).toBe('AsterRouter')
    expect(wrapper.get('input[name="smtp-username"]').attributes('autocomplete')).toBe('off')
    expect(wrapper.get('input[name="smtp-password"]').attributes('autocomplete')).toBe('new-password')

    await wrapper.get('[data-section="settings-save-bar"] button').trigger('click')
    await flushPromises()

    expect(settings.updateAdminSettings).toHaveBeenCalledWith(expect.objectContaining({
      password_reset_enabled: true,
      allowed_email_domains: ['example.com'],
      invitation_codes: ['INVITE-ONE'],
      turnstile_enabled: true,
      turnstile_site_key: 'site-key',
      trusted_proxy_headers: true,
      trusted_proxy_cidrs: ['10.0.0.0/8'],
      smtp_from_name: 'AsterRouter',
      smtp_use_tls: true
    }))

    wrapper.unmount()
  })

  it('tests the unsaved SMTP form values without exposing the stored password', async () => {
    const wrapper = mount(AdminSettingsView, { global: { plugins: [i18n] } })
    await flushPromises()
    await wrapper.get('#settings-tab-email').trigger('click')
    await flushPromises()

    await wrapper.get('input[name="smtp-host"]').setValue('smtp.unsaved.example')
    await wrapper.get('input[name="smtp-username"]').setValue('unsaved-user')
    await wrapper.get('input[name="smtp-password"]').setValue('new-secret')
    await wrapper.findAll('.smtp-test-controls button')[0]!.trigger('click')
    await flushPromises()
    expect(settings.testSMTPConnection).toHaveBeenCalledWith(expect.objectContaining({
      smtp_host: 'smtp.unsaved.example', smtp_username: 'unsaved-user', smtp_password: 'new-secret', smtp_use_tls: true
    }))

    await wrapper.get('.smtp-test-controls input[type="email"]').setValue('recipient@example.com')
    await wrapper.findAll('.smtp-test-controls button')[1]!.trigger('click')
    await flushPromises()
    expect(settings.testSMTP).toHaveBeenCalledWith('recipient@example.com', expect.objectContaining({ smtp_host: 'smtp.unsaved.example' }))
    wrapper.unmount()
  })
})
