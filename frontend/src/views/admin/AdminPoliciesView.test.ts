import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as control from '@/api/control'
import AdminPoliciesView from './AdminPoliciesView.vue'

vi.mock('@/api/control', () => ({
  createGovernancePolicy: vi.fn(),
  getGatewayModels: vi.fn(),
  getGovernancePolicies: vi.fn(),
  updateGovernancePolicy: vi.fn()
}))

describe('AdminPoliciesView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('en-US')
    vi.mocked(control.getGovernancePolicies).mockResolvedValue([])
    vi.mocked(control.getGatewayModels).mockResolvedValue([
      { id: 'active', model_id: 'gateway-current', name: 'Current', status: 'active' },
      { id: 'disabled', model_id: 'gateway-retired', name: 'Retired', status: 'disabled' }
    ] as never)
    vi.mocked(control.createGovernancePolicy).mockResolvedValue({ id: 'policy-current' } as never)
  })

  it('uses the active gateway catalog for distinct allowlist and denylist controls', async () => {
    const wrapper = mount(AdminPoliciesView, { global: { plugins: [i18n] } })
    await flushPromises()
    await wrapper.findAll('button').find((button) => button.text().includes('New policy'))!.trigger('click')

    await wrapper.get('form.modal-card input').setValue('Dynamic catalog policy')
    const allowlist = wrapper.get('[role="group"][aria-label="Model allowlist"]')
    const denylist = wrapper.get('[role="group"][aria-label="Model denylist"]')
    expect(allowlist.text()).toContain('gateway-current')
    expect(allowlist.text()).not.toContain('gateway-retired')
    expect(denylist.text()).toContain('gateway-current')
    await allowlist.get('[data-model-state="active"]').trigger('click')
    await wrapper.get('form.modal-card').trigger('submit')
    await flushPromises()

    expect(control.createGovernancePolicy).toHaveBeenCalledWith(expect.objectContaining({
      name: 'Dynamic catalog policy',
      model_allowlist: ['gateway-current'],
      model_denylist: []
    }))
    wrapper.unmount()
  })

  it('uses grouped standard fields and localized enum labels in the policy dialog', async () => {
    setLocale('zh-CN')
    const wrapper = mount(AdminPoliciesView, { global: { plugins: [i18n] } })
    await flushPromises()
    expect(wrapper.get('.crud-summary').text()).toContain('0总数')
    await wrapper.findAll('button').find((button) => button.text().includes('新增策略'))!.trigger('click')

    const dialog = wrapper.get('[role="dialog"]')
    expect(dialog.findAll('.policy-form-section')).toHaveLength(4)
    expect(dialog.text()).toContain('基本信息')
    expect(dialog.text()).toContain('限额与数据')
    expect(dialog.text()).toContain('阻断请求')
    expect(dialog.text()).toContain('仅元数据')
    expect(dialog.text()).not.toContain('metadata_only')
    expect(dialog.get('#policy-name').element.parentElement?.classList.contains('field')).toBe(true)
    expect(dialog.get('#policy-status').element.tagName).toBe('SELECT')
    expect(dialog.get('[aria-label="关闭"]').element.tagName).toBe('BUTTON')
    wrapper.unmount()
  })
})
