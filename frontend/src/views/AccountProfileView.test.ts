import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as account from '@/api/account'
import type { AccountProfile } from '@/types'
import AccountProfileView from './AccountProfileView.vue'

vi.mock('qrcode', () => ({
  default: { toDataURL: vi.fn().mockResolvedValue('data:image/png;base64,dG90cA==') }
}))

vi.mock('@/api/account', () => ({
  beginAccountIdentityBinding: vi.fn(),
  beginTOTPSetup: vi.fn(),
  changeAccountPassword: vi.fn(),
  confirmTOTP: vi.fn(),
  disableTOTP: vi.fn(),
  generateTOTPRecoveryCodes: vi.fn(),
  getAccountProfile: vi.fn(),
  revokeOtherAccountSessions: vi.fn(),
  unbindAccountIdentity: vi.fn(),
  updateAccountProfile: vi.fn()
}))

const profile: AccountProfile = {
  id: 'user-1',
  email: 'user@example.test',
  display_name: 'Account User',
  avatar_data_url: '',
  status: 'active',
  role: 'developer',
  balance_micros: 1200,
  concurrency_limit: 5,
  rpm_limit: 60,
  auth_identities: [],
  email_verified: true,
  password_enabled: true,
  totp_enabled: false,
  totp_available: false,
  managed_by_config: false,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
  login_methods: [
    { id: 'email', label: 'Email', available: true, bound: true, detail: 'user@example.test' },
    { id: 'oidc', label: 'Enterprise OIDC', available: false, bound: false },
    { id: 'github', label: 'GitHub', available: true, bound: false },
    { id: 'google', label: 'Google', available: false, bound: false },
    { id: 'feishu', label: 'Feishu', available: false, bound: false },
    { id: 'dingtalk', label: 'DingTalk', available: false, bound: false }
  ]
}

describe('AccountProfileView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    setLocale('en-US')
    const pinia = createPinia()
    setActivePinia(pinia)
    vi.mocked(account.getAccountProfile).mockResolvedValue(structuredClone(profile))
    vi.mocked(account.changeAccountPassword).mockResolvedValue({
      access_token: 'replacement-token',
      expires_at: '2099-01-01T00:00:00Z',
      changed: true
    })
  })

  it('separates profile, sign-in methods, and security into focused views', async () => {
    const wrapper = mount(AccountProfileView, { global: { plugins: [i18n] } })
    await flushPromises()

    expect(wrapper.findAll('.account-tabs button')).toHaveLength(3)
    expect(wrapper.get('[data-section="account-profile"]').isVisible()).toBe(true)

    await wrapper.get('[data-tab="profile"]').trigger('keydown', { key: 'End' })
    await flushPromises()
    expect(wrapper.get('[data-section="account-security"]').isVisible()).toBe(true)

    await wrapper.get('[data-tab="login"]').trigger('click')
    const methods = wrapper.get('[data-section="account-login-methods"]')
    expect(methods.text()).toContain('GitHub')
    expect(methods.text()).not.toContain('Google')
    expect(methods.text()).not.toContain('Enterprise OIDC')

    await wrapper.get('[data-tab="security"]').trigger('click')
    expect(wrapper.get('[data-section="account-security"]').text()).toContain('Account security overview')

    wrapper.unmount()
  })

  it('persists the replacement token returned after a password change', async () => {
    localStorage.setItem('asterrouter_admin_token', 'old-token')
    const wrapper = mount(AccountProfileView, { global: { plugins: [i18n] } })
    await flushPromises()
    await wrapper.get('[data-tab="security"]').trigger('click')
    await wrapper.get('#account-current-password').setValue('current-password')
    await wrapper.get('#account-new-password').setValue('updated-password')
    await wrapper.get('#account-confirm-password').setValue('updated-password')
    await wrapper.get('[data-form="account-password"]').trigger('submit')
    await flushPromises()

    expect(account.changeAccountPassword).toHaveBeenCalledWith('current-password', 'updated-password')
    expect(localStorage.getItem('asterrouter_admin_token')).toBe('replacement-token')
    expect(wrapper.text()).toContain('Password changed')

    wrapper.unmount()
  })

  it('routes passwordless accounts through verified email recovery', async () => {
    vi.mocked(account.getAccountProfile).mockResolvedValue({ ...structuredClone(profile), password_enabled: false })
    const wrapper = mount(AccountProfileView, { global: { plugins: [i18n] } })
    await flushPromises()
    await wrapper.get('[data-tab="security"]').trigger('click')

    expect(wrapper.find('[data-form="account-password"]').exists()).toBe(false)
    expect(wrapper.get('a[href="/forgot-password?email=user%40example.test"]').text()).toContain('Send setup email')
    wrapper.unmount()
  })

  it('requires the current password before exposing a TOTP enrollment secret', async () => {
    vi.mocked(account.getAccountProfile).mockResolvedValue({ ...structuredClone(profile), totp_available: true })
    vi.mocked(account.beginTOTPSetup).mockResolvedValue({
      secret: 'JBSWY3DPEHPK3PXP',
      provisioning_uri: 'otpauth://totp/AsterRouter:user@example.test?secret=JBSWY3DPEHPK3PXP',
      expires_at: '2099-01-01T00:10:00Z'
    })
    const wrapper = mount(AccountProfileView, { global: { plugins: [i18n] } })
    await flushPromises()
    await wrapper.get('[data-tab="security"]').trigger('click')

    const setupButton = wrapper.findAll('button').find((button) => button.text().includes('Set up authenticator'))
    expect(setupButton?.attributes('disabled')).toBeDefined()
    await wrapper.get('#account-totp-current-password').setValue('current-password')
    await setupButton!.trigger('click')
    await flushPromises()

    expect(account.beginTOTPSetup).toHaveBeenCalledWith('current-password')
    expect(wrapper.text()).toContain('JBSWY3DPEHPK3PXP')
    wrapper.unmount()
  })

  it('requires an existing factor before replacing recovery codes', async () => {
    vi.mocked(account.getAccountProfile).mockResolvedValue({ ...structuredClone(profile), totp_enabled: true, totp_available: true })
    vi.mocked(account.generateTOTPRecoveryCodes).mockResolvedValue({
      access_token: 'totp-replacement-token',
      expires_at: '2099-01-01T00:00:00Z',
      codes: ['RECOV1-RECOV2']
    })
    const wrapper = mount(AccountProfileView, { global: { plugins: [i18n] } })
    await flushPromises()
    await wrapper.get('[data-tab="security"]').trigger('click')

    const regenerateButton = wrapper.findAll('button').find((button) => button.text().includes('Regenerate recovery codes'))
    expect(regenerateButton?.attributes('disabled')).toBeDefined()
    await wrapper.get('#account-recovery-totp-code').setValue('123456')
    await regenerateButton!.trigger('click')
    await flushPromises()

    expect(account.generateTOTPRecoveryCodes).toHaveBeenCalledWith('123456')
    expect(wrapper.text()).toContain('RECOV1-RECOV2')
    expect(localStorage.getItem('asterrouter_admin_token')).toBe('totp-replacement-token')
    wrapper.unmount()
  })
})
