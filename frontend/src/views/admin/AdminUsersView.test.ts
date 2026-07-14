import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as control from '@/api/control'
import type { RoleBinding, WorkspaceUser } from '@/types'
import AdminUsersView from './AdminUsersView.vue'

vi.mock('@/api/control', () => ({
  createRoleBinding: vi.fn(),
  createWorkspaceUser: vi.fn(),
  deleteRoleBinding: vi.fn(),
  getDepartments: vi.fn(),
  getRoleBindings: vi.fn(),
  getWorkspaceUsers: vi.fn(),
  updateWorkspaceUser: vi.fn()
}))

const user: WorkspaceUser = {
  id: 'user-admin',
  email: 'admin@example.test',
  display_name: 'Admin User',
  status: 'active',
  role: 'super_admin',
  created_at: '2026-07-14T00:00:00Z',
  updated_at: '2026-07-14T00:00:00Z'
}

const binding: RoleBinding = {
  id: 'binding-admin',
  user_id: user.id,
  role: 'super_admin',
  scope_type: 'global',
  scope_id: '',
  created_at: '2026-07-14T00:00:00Z',
  updated_at: '2026-07-14T00:00:00Z'
}

describe('AdminUsersView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('zh-CN')
    vi.mocked(control.getWorkspaceUsers).mockResolvedValue([user])
    vi.mocked(control.getRoleBindings).mockResolvedValue([binding])
    vi.mocked(control.getDepartments).mockResolvedValue([])
  })

  it('uses user-management language and separates users from role assignments', async () => {
    const wrapper = mount(AdminUsersView, { global: { plugins: [i18n] } })
    await flushPromises()

    expect(wrapper.get('h1').text()).toBe('用户管理')
    expect(wrapper.text()).not.toContain('RBAC')
    expect(wrapper.findAll('.user-primary-tab')).toHaveLength(2)
    expect(wrapper.get('[data-section="user-directory"]').isVisible()).toBe(true)
    expect(wrapper.find('[data-section="user-access"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('超级管理员')
    expect(wrapper.text()).toContain('已启用')

    await wrapper.get('[data-view="access"]').trigger('click')
    expect(wrapper.get('[data-section="user-access"]').isVisible()).toBe(true)
    expect(wrapper.find('[data-section="user-directory"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('角色授权')
    expect(wrapper.text()).toContain('全局')

    wrapper.unmount()
  })

  it('offers a clear recovery action when filters return no users', async () => {
    const wrapper = mount(AdminUsersView, { global: { plugins: [i18n] } })
    await flushPromises()

    await wrapper.get('.user-toolbar input').setValue('missing-user')
    expect(wrapper.get('.user-empty-state').text()).toContain('没有匹配的用户')
    expect(wrapper.get('.user-empty-state button').text()).toContain('清除筛选')

    await wrapper.get('.user-empty-state button').trigger('click')
    expect(wrapper.find('.user-empty-state').exists()).toBe(false)

    wrapper.unmount()
  })
})
