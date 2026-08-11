import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { defineComponent } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { completeTOTPLogin, getCurrentUser, login as loginRequest, register as registerRequest, resetPassword as resetPasswordRequest, verifyEmail as verifyEmailRequest } from '@/api/auth'
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
const completeTOTPMock = vi.mocked(completeTOTPLogin)
const registerMock = vi.mocked(registerRequest)
const resetPasswordMock = vi.mocked(resetPasswordRequest)
const verifyEmailMock = vi.mocked(verifyEmailRequest)
const currentUserMock = vi.mocked(getCurrentUser)
const target = defineComponent({ template: '<main><h1>Enterprise Console</h1></main>' })

describe('LoginView demo entry', () => {
  beforeEach(() => {
    setLocale('zh-CN')
		loginMock.mockReset()
		completeTOTPMock.mockReset()
		registerMock.mockReset()
		resetPasswordMock.mockReset()
		verifyEmailMock.mockReset()
		currentUserMock.mockReset()
    loginMock.mockResolvedValue({
      access_token: 'oidc-cookie',
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
        { path: '/console/workbench', component: target }
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
    expect(router.currentRoute.value.fullPath).toBe('/console/workbench')
    expect(localStorage.getItem('asterrouter_admin_token')).toBe('oidc-cookie')
    wrapper.unmount()
  })

  it('does not expose demo credentials when demo mode is disabled', async () => {
    const { wrapper } = await mountLogin(false)

    expect(wrapper.find('.demo-experience').exists()).toBe(false)
    expect(wrapper.get('button[type="submit"]').text()).toContain('登录')
    wrapper.unmount()
  })

	it('enters browser MFA without exposing a challenge to JavaScript', async () => {
		loginMock.mockResolvedValue({ mfa_required: true, expires_at: '2099-01-01T00:00:00Z' })
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

	it('completes external MFA without exposing a challenge to JavaScript', async () => {
		completeTOTPMock.mockResolvedValue({
			access_token: 'oidc-cookie',
			token_type: 'Bearer',
			expires_at: '2099-01-01T00:00:00Z',
			user: makeAuthUser({ username: 'external@example.test', role: 'developer' })
		})
		const { router, wrapper } = await mountLogin(false, '/login?mfa=required')

		expect(wrapper.find('#mfa-code').exists()).toBe(true)
		expect(router.currentRoute.value.query).toEqual({ mfa: 'required' })
		await wrapper.get('#mfa-code').setValue('123456')
		await wrapper.get('form').trigger('submit')
		await flushPromises()

		expect(completeTOTPMock).toHaveBeenCalledWith('', '123456')
		expect(router.currentRoute.value.fullPath).toBe('/portal/overview')
		wrapper.unmount()
	})

		it.each([
		['OIDC', '/login?oidc=success'],
		['Feishu', '/login?provider=feishu'],
		['DingTalk', '/login?provider=dingtalk'],
		['GitHub', '/login?oauth=github&status=success'],
		['Google', '/login?oauth=google&status=success']
		])('restores the HttpOnly cookie session after %s login', async (_provider, callbackPath) => {
		const user = makeAuthUser({ username: 'external@example.test', role: 'developer' })
		currentUserMock.mockResolvedValue(user)

		const { router, wrapper } = await mountLogin(false, callbackPath)
		await flushPromises()

		expect(currentUserMock).toHaveBeenCalledOnce()
		expect(router.currentRoute.value.fullPath).toBe('/portal/overview')
		expect(localStorage.getItem('asterrouter_admin_token')).toBe('oidc-cookie')
		expect(JSON.parse(localStorage.getItem('asterrouter_admin_user') || '{}')).toEqual(user)
			wrapper.unmount()
		})

		it('shows a generic error for failed external login callbacks', async () => {
			const { wrapper } = await mountLogin(false, '/login?external=error&provider=oidc')
			await flushPromises()

			expect(currentUserMock).not.toHaveBeenCalled()
			expect(wrapper.text()).toContain('第三方登录未能完成')
			wrapper.unmount()
		})

		it('shows a server revocation warning after a failed sign-out', async () => {
			const { wrapper } = await mountLogin(false, '/login?logout=failed')
			await flushPromises()

			expect(wrapper.text()).toContain('无法确认服务端会话已撤销')
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
		const { router, wrapper } = await mountLogin(false, '/verify-email?token=verify-token')
		await flushPromises()

		expect(verifyEmailMock).toHaveBeenCalledWith('verify-token')
		expect(router.currentRoute.value.fullPath).toBe('/verify-email')
		expect(wrapper.text()).toContain('邮箱验证成功')
			wrapper.unmount()
		})

		it('removes reset tokens from the URL while retaining them for submission', async () => {
			resetPasswordMock.mockResolvedValue({ reset: true })
			const { router, wrapper } = await mountLogin(false, '/reset-password?token=reset-token')
			await flushPromises()

			expect(router.currentRoute.value.fullPath).toBe('/reset-password')
			await wrapper.get('#new-password').setValue('another-long-password')
			await wrapper.get('#confirm-password').setValue('another-long-password')
			await wrapper.get('form').trigger('submit')
			await flushPromises()

			expect(resetPasswordMock).toHaveBeenCalledWith('reset-token', 'another-long-password')
			wrapper.unmount()
		})

		it('prefills email recovery from the account security page', async () => {
			const { wrapper } = await mountLogin(false, '/forgot-password?email=user%40example.test', { password_reset_enabled: true })
			await flushPromises()

			expect((wrapper.get('#account-email').element as HTMLInputElement).value).toBe('user@example.test')
			wrapper.unmount()
		})
	})
