import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as control from '@/api/control'
import type { AlertEvent, AlertSummary } from '@/types'
import AdminAlertsView from './AdminAlertsView.vue'

vi.mock('@/api/control', () => ({
  acknowledgeAlert: vi.fn(),
  getAlertSummary: vi.fn(),
  getAlerts: vi.fn(),
  resolveAlert: vi.fn()
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolver) => {
    resolve = resolver
  })
  return { promise, resolve }
}

const resolvedAlert: AlertEvent = {
  id: 'alert-resolved',
  type: 'api_key_quota',
  severity: 'critical',
  status: 'resolved',
  title: 'API key monthly token quota exhausted',
  summary: 'The workspace key reached its monthly token quota.',
  resource_type: 'api_key',
  resource_id: 'key-race-regression',
  dedupe_key: 'api_key_quota:key-race-regression:2026-08',
  metadata: {},
  first_seen_at: '2026-08-13T01:00:00Z',
  last_seen_at: '2026-08-13T01:01:00Z',
  acknowledged_by: 'admin',
  resolved_at: '2026-08-13T01:02:00Z',
  resolved_by: 'admin'
}

const resolvedSummary: AlertSummary = {
  total: 1,
  active: 0,
  acknowledged: 0,
  resolved: 1,
  warning: 0,
  critical: 1
}

describe('AdminAlertsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('en-US')
  })

  it('ignores an older list response that finishes after the latest filter request', async () => {
    const staleAlerts = deferred<AlertEvent[]>()
    const staleSummary = deferred<AlertSummary>()
    vi.mocked(control.getAlerts).mockImplementation((params) => (
      params?.status === 'resolved' ? Promise.resolve([resolvedAlert]) : staleAlerts.promise
    ))
    vi.mocked(control.getAlertSummary).mockImplementation((params) => (
      params?.status === 'resolved' ? Promise.resolve(resolvedSummary) : staleSummary.promise
    ))

    const wrapper = mount(AdminAlertsView, { global: { plugins: [i18n] } })
    const statusFilter = wrapper.findAll('.table-toolbar select')[2]
    await statusFilter.setValue('resolved')
    await flushPromises()

    expect(wrapper.text()).toContain('key-race-regression')
    expect(wrapper.text()).toContain('Resolved')

    staleAlerts.resolve([])
    staleSummary.resolve({ total: 0, active: 0, acknowledged: 0, resolved: 0, warning: 0, critical: 0 })
    await flushPromises()

    expect(wrapper.text()).toContain('key-race-regression')
    expect(wrapper.text()).toContain('Resolved')
    wrapper.unmount()
  })
})
