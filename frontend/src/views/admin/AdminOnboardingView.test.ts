import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as control from '@/api/control'
import type { Application } from '@/types'
import AdminOnboardingView from './AdminOnboardingView.vue'

vi.mock('@/api/control', () => ({
  createApplication: vi.fn(),
  getApplications: vi.fn(),
  updateApplication: vi.fn()
}))

const applications: Application[] = [
  {
    id: 'app-customer-service',
    name: 'Customer Service',
    slug: 'customer-service',
    entitlement_reference: 'crm-product-1024',
    concurrency_limit: 12,
    status: 'active',
    created_at: '2026-08-10T08:00:00Z',
    updated_at: '2026-08-11T08:30:00Z'
  },
  {
    id: 'app-knowledge-base',
    name: 'Knowledge Base',
    slug: 'knowledge-base',
    entitlement_reference: '',
    concurrency_limit: 0,
    status: 'disabled',
    created_at: '2026-08-09T08:00:00Z',
    updated_at: '2026-08-10T08:30:00Z'
  }
]

function mountView() {
  return mount(AdminOnboardingView, {
    global: {
      plugins: [i18n],
      stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } }
    }
  })
}

describe('AdminOnboardingView application list', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('en-US')
    vi.mocked(control.getApplications).mockResolvedValue(applications)
    vi.mocked(control.createApplication).mockResolvedValue(applications[0])
    vi.mocked(control.updateApplication).mockResolvedValue(applications[0])
  })

  it('uses the application inventory as the primary page instead of an onboarding wizard', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(control.getApplications).toHaveBeenCalledOnce()
    expect(wrapper.get('h1').text()).toBe('Applications')
    expect(wrapper.get('table').text()).toContain('Customer Service')
    expect(wrapper.get('table').text()).toContain('Knowledge Base')
    expect(wrapper.get('a[href="/console/applications/credentials"]').text()).toContain('Credentials')
    expect(wrapper.find('.onboarding-progress').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Connect model source')
    wrapper.unmount()
  })

  it('filters the list by keyword and status', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('.search-box input').setValue('knowledge')
    expect(wrapper.get('tbody').text()).not.toContain('Customer Service')
    expect(wrapper.get('tbody').text()).toContain('Knowledge Base')

    await wrapper.get('.application-toolbar select').setValue('active')
    expect(wrapper.get('tbody').text()).toContain('No applications')
    wrapper.unmount()
  })

  it('creates an application from the compact dialog', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('.page-header .button:not(.secondary)').trigger('click')
    const dialog = wrapper.get('[role="dialog"]')
    expect(dialog.text()).toContain('New application')

    await dialog.get('#application-name').setValue('Support Copilot')
    await dialog.get('#application-slug').setValue('support-copilot')
    await dialog.get('#application-concurrency').setValue('24')
    await dialog.get('#application-entitlement').setValue('crm-plan-enterprise')
    await dialog.get('form').trigger('submit')
    await flushPromises()

    expect(control.createApplication).toHaveBeenCalledWith({
      name: 'Support Copilot',
      slug: 'support-copilot',
      entitlement_reference: 'crm-plan-enterprise',
      concurrency_limit: 24,
      status: 'active'
    })
    expect(control.getApplications).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('Application created.')
    wrapper.unmount()
  })

  it('edits an existing application without changing its identity contract', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[aria-label="Edit application Customer Service"]').trigger('click')
    const dialog = wrapper.get('[role="dialog"]')
    expect((dialog.get('#application-name').element as HTMLInputElement).value).toBe('Customer Service')
    expect((dialog.get('#application-slug').element as HTMLInputElement).value).toBe('customer-service')

    await dialog.get('#application-status').setValue('disabled')
    await dialog.get('form').trigger('submit')
    await flushPromises()

    expect(control.updateApplication).toHaveBeenCalledWith('app-customer-service', {
      name: 'Customer Service',
      slug: 'customer-service',
      entitlement_reference: 'crm-product-1024',
      concurrency_limit: 12,
      status: 'disabled'
    })
    expect(wrapper.text()).toContain('Application updated.')
    wrapper.unmount()
  })
})
