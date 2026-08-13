import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it } from 'vitest'
import { nextTick } from 'vue'
import i18n, { setLocale } from '@/i18n'
import MarketingHomeView from './MarketingHomeView.vue'

describe('AsterRouter official website', () => {
  beforeEach(() => {
    localStorage.clear()
    setLocale('en-US')
  })

  it('presents the shipped enterprise product and keeps the primary entry on sign-in', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: MarketingHomeView },
        { path: '/login', component: { template: '<div>login</div>' } }
      ]
    })
    await router.push('/')
    await router.isReady()
    const wrapper = mount(MarketingHomeView, { global: { plugins: [router, i18n] } })

    expect(wrapper.get('h1').text()).toBe('AsterRouter')
    expect(wrapper.text()).toContain('Enterprise AI access and routing infrastructure')
    expect(wrapper.text()).toContain('Every model request follows the same enterprise decision chain')
    expect(wrapper.text()).toContain('A policy is not one weight. It is a complete routing contract.')
    expect(wrapper.text()).toContain('Live decision')
    expect(wrapper.text()).toContain('Cost boundary checked')
    expect(wrapper.text()).toContain('Request entered the preferred route')
    expect(wrapper.get('.hero-product-image').attributes('src')).toBe('/images/asterrouter-routing-workbench.webp')
    expect(wrapper.get('.primary-action').attributes('href')).toBe('/login')
    expect(document.title).toContain('AsterRouter')

    setLocale('zh-CN')
    await nextTick()
    expect(wrapper.text()).toContain('企业 AI 访问与路由基础设施')
    expect(wrapper.text()).toContain('策略不是一个权重，而是一份完整路由合同')
    expect(wrapper.text()).toContain('实时决策')
    expect(wrapper.text()).toContain('成本边界已检查')
    wrapper.unmount()
  })
})
