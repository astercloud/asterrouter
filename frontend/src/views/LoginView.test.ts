import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { defineComponent } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { login as loginRequest, register as registerRequest, verifyEmail as verifyEmailRequest } from '@/api/auth'
import i18n, { setLocale } from '@/i18n'
import { useAppStore } from '@/stores/app'
import { makeAuthUser, makePublicSettings } from '@/test/fixtures'
import LoginView from './LoginView.vue'

vi.mock('@/api/auth', () => ({
  completeTOTPLogin: vi.fn(),
  forgotPassword: vi.fn(),
  getCurrentUser: vi.fn(),
  login: vi.fn(),
	logout: vi.fn(),
  register: vi.fn(),
	resendVerification: vi.fn(),
  resetPassword: vi.fn(),
  verifyEmail: vi.fn()
}))

const loginMock = vi.mocked(loginRequest)
const registerMock = vi.mocked(registerRequest)
const verifyEmailMock = vi.mocked(verifyEmailRequest)
const target = defineComponent({ template: '<main><h1>Personal Console</h1></main>' })

describe('LoginView demo entry', () => {
  beforeEach(() => {
    setLocale('zh-CN')
		loginMock.mockReset()
		registerMock.mockReset()
		verifyEmailMock.mockReset()
    loginMock.mockResolvedValue({
      access_token: 'demo-token',
      token_type: 'Bearer',
      expires_at: '2099-01-01T00:00:00Z',
      user: makeAuthUser({ username: 'demo', role: 'demo_admin' })
    })
  })

	async function mountLogin(demoMode: boolean, path = '/login', settingsOverrides = {}) {
    const pinia = createPinia()
    setActivePinia(pinia)
    const app = useAppStore()
    app.publicSettings = makePublicSettings({
      demo_mode: demoMode,
      default_profile: 'personal',
			enabled_profiles: ['personal'],
			...settingsOverrides
    })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/login', component: LoginView },
				{ path: '/register', component: LoginView },
				{ path: '/forgot-password', component: LoginView },
				{ path: '/resend-verification', component: LoginView },
				{ path: '/reset-password', component: LoginView },
				{ path: '/verify-email', component: LoginView },
        { path: '/console/overview', component: target }
      ]
    })
		await router.push(path)
    await router.isReady()

    const wrapper = mount(LoginView, { global: { plugins: [pinia, router, i18n] } })
    return { router, wrapper }
  }

  it('shows a prominent one-click entry and opens the demo surface', async () => {
    const { router, wrapper } = await mountLogin(true)

    expect(wrapper.get('#demo-experience-title').text()).toBe('立即体验 AsterRouter')
    await wrapper.get('.demo-experience-action').trigger('click')
    await flushPromises()

    expect(loginMock).toHaveBeenCalledWith('demo', 'demo', false, '')
    expect(router.currentRoute.value.fullPath).toBe('/console/overview')
    expect(localStorage.getItem('asterrouter_admin_token')).toBe('demo-token')
    wrapper.unmount()
  })

  it('does not expose demo credentials when demo mode is disabled', async () => {
    const { wrapper } = await mountLogin(false)

    expect(wrapper.find('.demo-experience').exists()).toBe(false)
    expect(wrapper.get('button[type="submit"]').text()).toContain('登录')
    wrapper.unmount()
  })

	it('keeps an MFA challenge out of local session storage', async () => {
		loginMock.mockResolvedValue({ mfa_required: true, challenge: 'mfa-challenge', expires_at: '2099-01-01T00:00:00Z' })
		const { wrapper } = await mountLogin(false)
		await wrapper.get('#password').setValue('long-password')
		await wrapper.get('form').trigger('submit')
		await flushPromises()

		expect(wrapper.find('#mfa-code').exists()).toBe(true)
		expect(wrapper.get('#mfa-code').attributes('maxlength')).toBe('13')
		expect(wrapper.get('#mfa-code').attributes('pattern')).toBeUndefined()
		expect(localStorage.getItem('asterrouter_admin_token')).toBeNull()
		wrapper.unmount()
	})

	it('submits registration fields and exposes verification email recovery', async () => {
		registerMock.mockResolvedValue({ user_id: 'user-1', verification_required: true, email_delivery_failed: true })
		const { wrapper } = await mountLogin(false, '/register', { registration_enabled: true, email_verify_enabled: true, allowed_email_domains: ['example.com'] })
		await wrapper.get('#account-email').setValue('user@example.com')
		await wrapper.get('#new-password').setValue('long-password')
		await wrapper.get('#confirm-password').setValue('long-password')
		await wrapper.get('form').trigger('submit')
		await flushPromises()

		expect(registerMock).toHaveBeenCalledWith('user@example.com', 'long-password', '', '', false, '')
		expect(wrapper.text()).toContain('验证邮件投递失败')
		expect(wrapper.text()).toContain('重新发送验证邮件')
		wrapper.unmount()
	})

		it('verifies direct email links from the first-class route', async () => {
		verifyEmailMock.mockResolvedValue({ verified: true })
		const { wrapper } = await mountLogin(false, '/verify-email?token=verify-token')
		await flushPromises()

		expect(verifyEmailMock).toHaveBeenCalledWith('verify-token')
		expect(wrapper.text()).toContain('邮箱验证成功')
			wrapper.unmount()
		})

		it('prefills email recovery from the account security page', async () => {
			const { wrapper } = await mountLogin(false, '/forgot-password?email=user%40example.test', { password_reset_enabled: true })
			await flushPromises()

			expect((wrapper.get('#account-email').element as HTMLInputElement).value).toBe('user@example.test')
			wrapper.unmount()
		})
	})
