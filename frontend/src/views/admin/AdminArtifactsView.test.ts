import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as control from '@/api/control'
import type { ArtifactAdminDetail, ArtifactAdminRecord } from '@/types'
import AdminArtifactsView from './AdminArtifactsView.vue'

vi.mock('@/api/control', () => ({
  getArtifact: vi.fn(),
  getArtifactRuntimes: vi.fn(),
  getArtifacts: vi.fn(),
  getArtifactSummary: vi.fn(),
  retryArtifactDelivery: vi.fn()
}))

const artifact: ArtifactAdminRecord = {
  id: 'artifact-output-1',
  operation_id: 'operation-1',
  job_id: 'job-1',
  attempt_id: 'attempt-1',
  profile_scope: 'platform',
  tenant_id: 'tenant-1',
  role: 'final',
  policy: 'customer_sink',
  status: 'delivery_failed',
  status_version: 4,
  media_type: 'image/png',
  size_bytes: 2048,
  sha256: 'synthetic-sha',
  store_driver: 'none',
  error_type: 'sink_delivery_failed',
  sink_id: 'sink-customer',
  runtime_status: 'registered',
  retain_until: '2026-07-16T10:00:00Z',
  created_at: '2026-07-15T10:00:00Z',
  updated_at: '2026-07-15T10:01:00Z'
}

const detail: ArtifactAdminDetail = {
  artifact,
  events: [
    { id: 'event-1', artifact_id: artifact.id, version: 1, event_type: 'artifact.pending', to_status: 'pending', created_at: artifact.created_at },
    { id: 'event-2', artifact_id: artifact.id, version: 4, event_type: 'artifact.delivery.failed', from_status: 'delivering', to_status: 'delivery_failed', reason: 'sink_delivery_failed', created_at: artifact.updated_at }
  ]
}

describe('AdminArtifactsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('en-US')
    vi.mocked(control.getArtifacts).mockResolvedValue([artifact])
    vi.mocked(control.getArtifactSummary).mockResolvedValue({ total: 1, size_bytes: 2048, by_status: { delivery_failed: 1 } })
    vi.mocked(control.getArtifactRuntimes).mockResolvedValue([{ kind: 'sink', id: 'sink-customer', status: 'registered' }])
    vi.mocked(control.getArtifact).mockResolvedValue(detail)
    vi.mocked(control.retryArtifactDelivery).mockResolvedValue({ artifact_id: artifact.id, attempt_id: 'attempt-1', status: 'scheduled', scheduled_at: artifact.updated_at })
  })

  it('renders delivery evidence and safely schedules a failed delivery retry', async () => {
    const confirm = vi.fn(() => true)
    vi.stubGlobal('confirm', confirm)
    const wrapper = mount(AdminArtifactsView, { global: { plugins: [i18n] } })
    await flushPromises()

    expect(wrapper.get('.crud-summary').text()).toContain('Needs attention')
    expect(wrapper.get('.artifact-runtime-strip').text()).toContain('sink-customer')
    expect(wrapper.get('tbody').text()).toContain('artifact-output-1')
    expect(wrapper.get('tbody').text()).toContain('delivery failed')

    await wrapper.get('button[aria-label="Details"]').trigger('click')
    await flushPromises()
    expect(control.getArtifact).toHaveBeenCalledWith(artifact.id)
    expect(wrapper.get('.artifact-detail').text()).toContain('Lifecycle events')
    expect(wrapper.get('.artifact-detail').text()).toContain('sink delivery failed')

    const retry = wrapper.findAll('.artifact-detail button').find((button) => button.text().includes('Retry delivery'))
    expect(retry).toBeTruthy()
    await retry!.trigger('click')
    await flushPromises()

    expect(confirm).toHaveBeenCalledOnce()
    expect(control.retryArtifactDelivery).toHaveBeenCalledWith(artifact.id)
    expect(wrapper.text()).toContain('Delivery retry scheduled')
    vi.unstubAllGlobals()
    wrapper.unmount()
  })

  it('shows a stable error state when artifact data cannot be loaded', async () => {
    vi.mocked(control.getArtifacts).mockRejectedValueOnce(new Error('artifact service unavailable'))
    const wrapper = mount(AdminArtifactsView, { global: { plugins: [i18n] } })
    await flushPromises()

    expect(wrapper.get('.notice').text()).toContain('artifact service unavailable')
    expect(wrapper.get('.empty-cell').text()).toContain('No artifacts match')
    wrapper.unmount()
  })
})
