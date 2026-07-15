import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as control from '@/api/control'
import type { AIJobAdminDetail, AIJobAdminRecord } from '@/types'
import AdminAIJobsView from './AdminAIJobsView.vue'

vi.mock('@/api/control', () => ({
  cancelAIJob: vi.fn(),
  getAIJob: vi.fn(),
  getAIJobRuntime: vi.fn(),
  getAIJobSummary: vi.fn(),
  getAIJobs: vi.fn(),
  scheduleAIJobAttemptReconciliation: vi.fn()
}))

const job: AIJobAdminRecord = {
  id: 'job-1', operation_id: 'operation-1', profile_scope: 'platform', tenant_id: 'tenant-1', protocol: 'aster_jobs',
  operation: 'image_generation', modality: 'image', model: 'image-model', artifact_policy: 'managed', status: 'queued',
  status_version: 1, priority: 10, next_eligible_at: '2026-07-15T10:00:00Z', created_at: '2026-07-15T10:00:00Z',
  updated_at: '2026-07-15T10:00:00Z', expires_at: '2026-07-16T10:00:00Z'
}

const detail: AIJobAdminDetail = {
  job,
  attempts: [{
    id: 'attempt-1', attempt_number: 1, provider_id: 'provider-1', provider_account_id: 'account-1', provider_adapter_id: 'adapter-1', route_id: 'route-1',
    upstream_model: 'upstream-image', status: 'running', dispatch_state: 'unknown', dispatch_version: 2, provider_task_id: 'task-1', provider_task_status: 'running',
    created_at: job.created_at, updated_at: job.updated_at
  }],
  events: [{ id: 'event-1', job_id: job.id, version: 1, event_type: 'job.queued', to_status: 'queued', created_at: job.created_at }],
  artifacts: []
}

describe('AdminAIJobsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('en-US')
    vi.mocked(control.getAIJobs).mockResolvedValue([job])
    vi.mocked(control.getAIJobSummary).mockResolvedValue({ total: 1, by_status: { queued: 1 } })
    vi.mocked(control.getAIJobRuntime).mockResolvedValue({
      running: true, queue_driver: 'memory', worker_id: 'worker-1',
      scheduler: { runs: 3, errors: 0 }, delivery: { runs: 3, errors: 0 }, reconciler: { runs: 2, errors: 0 }, rebuilder: { runs: 1, errors: 0 }
    })
    vi.mocked(control.getAIJob).mockResolvedValue(detail)
    vi.mocked(control.cancelAIJob).mockResolvedValue({ job_id: job.id, status: 'canceled', changed: true, updated_at: job.updated_at })
    vi.mocked(control.scheduleAIJobAttemptReconciliation).mockResolvedValue({ job_id: job.id, attempt_id: 'attempt-1', status: 'scheduled', scheduled_at: job.updated_at })
  })

  it('renders runtime and schedules safe job actions', async () => {
    const confirm = vi.fn(() => true)
    vi.stubGlobal('confirm', confirm)
    const wrapper = mount(AdminAIJobsView, { global: { plugins: [i18n] } })
    await flushPromises()

    expect(wrapper.get('.crud-summary').text()).toContain('Total jobs')
    expect(wrapper.get('.ai-runtime-strip').text()).toContain('memory')
    expect(wrapper.get('tbody').text()).toContain('image-model')

    await wrapper.get('button[aria-label="Details"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('.ai-job-detail').text()).toContain('Provider attempts')
    const reconcile = wrapper.get('.ai-job-detail button[aria-label="Reconcile now"]')
    await reconcile.trigger('click')
    await flushPromises()
    expect(control.scheduleAIJobAttemptReconciliation).toHaveBeenCalledWith(job.id, 'attempt-1')
    expect(wrapper.text()).toContain('Provider attempt scheduled')

    const cancel = wrapper.findAll('.ai-job-detail button').find((button) => button.text().includes('Cancel job'))
    expect(cancel).toBeTruthy()
    await cancel!.trigger('click')
    await flushPromises()
    expect(control.cancelAIJob).toHaveBeenCalledWith(job.id)
    expect(confirm).toHaveBeenCalledTimes(2)
    vi.unstubAllGlobals()
    wrapper.unmount()
  })
})
