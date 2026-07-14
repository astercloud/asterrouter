import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { defineComponent } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { login as loginRequest } from '@/api/auth'
import i18n, { setLocale } from '@/i18n'
import { useAppStore } from '@/stores/app'
import { makeAuthUser, makePublicSettings } from '@/test/fixtures'
import LoginView from './LoginView.vue'

vi.mock('@/api/auth', () => ({
  completeTOTPLogin: vi.fn(),
  forgotPassword: vi.fn(),
  getCurrentUser: vi.fn(),
  login: vi.fn(),
  register: vi.fn(),
  resetPassword: vi.fn(),
  verifyEmail: vi.fn()
}))

const loginMock = vi.mocked(loginRequest)
const target = defineComponent({ template: '<main><h1>Personal Console</h1></main>' })

describe('LoginView demo entry', () => {
  beforeEach(() => {
    setLocale('zh-CN')
    loginMock.mockResolvedValue({
      access_token: 'demo-token',
      token_type: 'Bearer',
      expires_at: '2099-01-01T00:00:00Z',
      user: makeAuthUser({ username: 'demo', role: 'demo_admin' })
    })
  })

  async function mountLogin(demoMode: boolean) {
    const pinia = createPinia()
    setActivePinia(pinia)
    const app = useAppStore()
    app.publicSettings = makePublicSettings({
      demo_mode: demoMode,
      default_profile: 'personal',
      enabled_profiles: ['personal']
    })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/login', component: LoginView },
        { path: '/console/overview', component: target }
      ]
    })
    await router.push('/login')
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
})
