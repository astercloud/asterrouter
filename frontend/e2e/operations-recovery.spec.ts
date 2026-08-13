import { expect, test, type APIResponse, type Locator, type Page } from '@playwright/test'
import {
  adminPost,
  captureBrowserErrors,
  createDurableImageGatewayFixture,
  envelope,
  loginDemo,
  loginTestPrincipal
} from './fixtures'

type Plugin = { id: string; status: string }
type Attempt = {
  id: string
  status: string
  dispatch_state: string
  dispatch_version: number
  reconcile_after?: string
}
type Artifact = {
  id: string
  status: string
  media_type: string
  size_bytes: number
  sha256: string
}
type AIJobDetail = { job: { id: string; status: string }; attempts: Attempt[]; artifacts: Artifact[] }
type AuditEvent = { action: string; resource_type: string; resource_id: string }
type S3Request = {
  method: string
  path: string
  content_type: string
  content_length: number
  authorization_present: boolean
  sigv4_valid: boolean
  sigv4_errors: string[]
  body_base64: string
  decode_error: string
  outcome: string
}

const artifactSinkPluginID = 'com.asterrouter.artifact.s3-compatible-sink'
const syntheticPNG = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=', 'base64')

function uniqueID(name: string, project: string): string {
  return `${name}-${project}-${Date.now()}`.replace(/[^a-z0-9-]+/gi, '-').toLowerCase()
}

function rowFor(table: Locator, value: string): Locator {
  return table.getByRole('row').filter({ hasText: value })
}

async function responseData<T>(response: APIResponse): Promise<T> {
  const body = await response.json() as { code: number; message: string; data: T }
  expect(response.status(), JSON.stringify(body)).toBe(200)
  expect(body.code, JSON.stringify(body)).toBe(0)
  return body.data
}

async function setFixtureMode(page: Page, baseURL: string, mode: string): Promise<void> {
  const response = await page.request.post(`${baseURL}/__test/mode`, { data: { mode } })
  expect(response.status()).toBe(200)
  expect(await response.json()).toEqual({ mode })
}

async function getJob(page: Page, token: string, jobID: string): Promise<AIJobDetail> {
  return envelope<AIJobDetail>(await page.request.get(`/api/v1/console/ai-jobs/${jobID}`, {
    headers: { Authorization: `Bearer ${token}` }
  }))
}

test('@e2e-ai-job-reconciliation-001 unknown provider attempts can be scheduled for immediate reconciliation', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The stateful recovery action runs once.')
  test.setTimeout(90_000)

  const errors = captureBrowserErrors(page)
  const runID = uniqueID('reconcile', testInfo.project.name)
  const modelID = `browser-reconcile-model-${runID}`
  const upstreamAPI = `http://127.0.0.1:${process.env.ASTER_E2E_UPSTREAM_PORT || '19000'}`
  await loginDemo(page)
  const token = await loginTestPrincipal(page)
  await createDurableImageGatewayFixture(page, token, runID, modelID)
  const key = await adminPost<{ key: string }>(page, token, '/api-keys', {
    name: `Reconciliation Key ${runID}`,
    scopes: ['gateway:invoke', 'jobs:read'],
    model_allowlist: [modelID],
    allowed_modalities: ['image'],
    allowed_operations: ['image_generation'],
    lane_policy: 'durable_only',
    artifact_policy: 'temporary'
  })

  try {
    await setFixtureMode(page, upstreamAPI, '500')
    const response = await page.request.post('/v1/jobs', {
      headers: { Authorization: `Bearer ${key.key}`, 'Idempotency-Key': `reconcile-${runID}` },
      data: { model: modelID, operation: 'image_generation', modality: 'image', input: { prompt: 'synthetic unknown image', count: 1 } }
    })
    expect(response.status()).toBe(202)
    const jobID = (await response.json() as { id: string }).id

    await expect.poll(async () => {
      const detail = await getJob(page, token, jobID)
      return `${detail.job.status}:${detail.attempts[0]?.status}:${detail.attempts[0]?.dispatch_state}`
    }, { timeout: 20_000 }).toBe('unknown:running:unknown')
    const before = await getJob(page, token, jobID)
    const attempt = before.attempts[0]
    expect(attempt).toBeDefined()
    expect(attempt.reconcile_after).toBeTruthy()

    await page.goto(`/console/usage/jobs?q=${encodeURIComponent(jobID)}`)
    const jobRow = rowFor(page.getByRole('table'), jobID)
    await expect(jobRow).toContainText('unknown')
    await jobRow.getByRole('button', { name: 'Details' }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog).toContainText('unknown')
    await expect(dialog.getByRole('button', { name: 'Reconcile now' })).toBeVisible()

    const scheduledResponse = page.waitForResponse((candidate) =>
      candidate.request().method() === 'POST' && candidate.url().includes(`/ai-jobs/${jobID}/attempts/${attempt.id}/reconcile`)
    )
    page.once('dialog', (confirmation) => confirmation.accept())
    await dialog.getByRole('button', { name: 'Reconcile now' }).click()
    const scheduled = await responseData<{ job_id: string; attempt_id: string; status: string; scheduled_at: string }>(await scheduledResponse)
    expect(scheduled).toEqual(expect.objectContaining({ job_id: jobID, attempt_id: attempt.id, status: 'scheduled' }))
    expect(Date.parse(scheduled.scheduled_at)).not.toBeNaN()
    await expect(page.getByText('Provider attempt scheduled for reconciliation.')).toBeVisible()

    await page.reload()
    const persistedRow = rowFor(page.getByRole('table'), jobID)
    await expect(persistedRow).toContainText('unknown')
    await persistedRow.getByRole('button', { name: 'Details' }).click()
    await expect(page.getByRole('dialog').getByRole('button', { name: 'Reconcile now' })).toBeVisible()

    const audit = await envelope<AuditEvent[]>(await page.request.get('/api/v1/console/audit-logs?action=schedule_reconciliation&resource_type=ai_attempt&limit=200', {
      headers: { Authorization: `Bearer ${token}` }
    }))
    expect(audit).toContainEqual(expect.objectContaining({
      action: 'schedule_reconciliation',
      resource_type: 'ai_attempt',
      resource_id: attempt.id
    }))
    expect(errors).toEqual([])
  } finally {
    await setFixtureMode(page, upstreamAPI, 'normal')
  }
})

test('@e2e-artifact-delivery-retry-001 failed customer delivery retries through to stored bytes', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The stateful recovery action runs once.')
  test.setTimeout(120_000)

  const errors = captureBrowserErrors(page)
  const runID = uniqueID('artifact-retry', testInfo.project.name)
  const modelID = `browser-artifact-model-${runID}`
  const sinkID = `browser-retry-${runID}`
  const s3Port = process.env.ASTER_E2E_S3_PORT || '29003'
  const s3API = process.env.ASTER_E2E_S3_API_URL || 'http://127.0.0.1:29004'
  await loginDemo(page)
  const token = await loginTestPrincipal(page)
  const headers = { Authorization: `Bearer ${token}` }
  const catalog = await envelope<{ plugins: Plugin[] }>(await page.request.get('/api/v1/console/plugins', { headers }))
  const initialPlugin = catalog.plugins.find((plugin) => plugin.id === artifactSinkPluginID)
  expect(initialPlugin).toBeDefined()

  let destinationCreated = false
  try {
    await envelope(await page.request.put(
      `/api/v1/console/plugins/${encodeURIComponent(artifactSinkPluginID)}/artifact-sinks/${encodeURIComponent(sinkID)}`,
      {
        headers,
        data: {
          name: `Browser retry sink ${runID}`,
          provider: 's3',
          endpoint: `https://localhost:${s3Port}`,
          region: 'us-east-1',
          bucket: 'browser-media',
          prefix: 'delivery',
          reference_base_url: 'https://media.example.test/delivery',
          allowed_application_id: '',
          path_style: true,
          enabled: true,
          secrets: { access_key: `access-${runID}`, secret_key: `secret-${runID}` },
          clear_session_token: false
        }
      }
    ))
    destinationCreated = true
    if (initialPlugin?.status !== 'enabled') {
      await envelope(await page.request.post(`/api/v1/console/plugins/${encodeURIComponent(artifactSinkPluginID)}/enable`, { headers }))
    }

    await createDurableImageGatewayFixture(page, token, runID, modelID)
    const key = await adminPost<{ key: string }>(page, token, '/api-keys', {
      name: `Artifact Retry Key ${runID}`,
      scopes: ['gateway:invoke', 'jobs:read'],
      model_allowlist: [modelID],
      allowed_modalities: ['image'],
      allowed_operations: ['image_generation'],
      lane_policy: 'durable_only',
      artifact_policy: 'customer_sink',
      artifact_sink_id: sinkID
    })

    await setFixtureMode(page, s3API, 'fail-put')
    const response = await page.request.post('/v1/jobs', {
      headers: { Authorization: `Bearer ${key.key}`, 'Idempotency-Key': `artifact-retry-${runID}` },
      data: { model: modelID, operation: 'image_generation', modality: 'image', input: { prompt: 'synthetic customer delivery', count: 1 } }
    })
    expect(response.status()).toBe(202)
    const jobID = (await response.json() as { id: string }).id

    await expect.poll(async () => {
      const detail = await getJob(page, token, jobID)
      return detail.artifacts[0]?.status || 'missing'
    }, { timeout: 30_000 }).toBe('delivery_failed')
    const failedJob = await getJob(page, token, jobID)
    const artifact = failedJob.artifacts[0]
    expect(artifact).toEqual(expect.objectContaining({ status: 'delivery_failed', media_type: 'image/png' }))
    expect(artifact.size_bytes).toBe(syntheticPNG.length)

    await page.goto(`/console/usage/artifacts?q=${encodeURIComponent(artifact.id)}`)
    const artifactRow = rowFor(page.getByRole('table'), artifact.id)
    await expect(artifactRow).toContainText('delivery failed')
    await expect(artifactRow).toContainText(sinkID)
    await artifactRow.getByRole('button', { name: 'Details' }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByRole('button', { name: 'Retry delivery' })).toBeVisible()

    await setFixtureMode(page, s3API, 'normal')
    const retryResponse = page.waitForResponse((candidate) =>
      candidate.request().method() === 'POST' && candidate.url().includes(`/artifacts/${artifact.id}/retry-delivery`)
    )
    page.once('dialog', (confirmation) => confirmation.accept())
    await dialog.getByRole('button', { name: 'Retry delivery' }).click()
    const retry = await responseData<{ artifact_id: string; attempt_id: string; status: string; scheduled_at: string }>(await retryResponse)
    expect(retry).toEqual(expect.objectContaining({ artifact_id: artifact.id, status: 'scheduled' }))
    expect(Date.parse(retry.scheduled_at)).not.toBeNaN()
    await expect(page.getByText('Delivery retry scheduled.')).toBeVisible()

    await expect.poll(async () => {
      const detail = await envelope<{ artifact: Artifact }>(await page.request.get(`/api/v1/console/artifacts/${artifact.id}`, { headers }))
      return detail.artifact.status
    }, { timeout: 30_000 }).toBe('delivered')

    await page.reload()
    const deliveredRow = rowFor(page.getByRole('table'), artifact.id)
    await expect(deliveredRow).toContainText('delivered')
    await deliveredRow.getByRole('button', { name: 'Details' }).click()
    const deliveredDialog = page.getByRole('dialog')
    await expect(deliveredDialog).toContainText('delivery failed')
    await expect(deliveredDialog).toContainText('delivered')
    await expect(deliveredDialog).toContainText('sink delivery failed')
    await expect(deliveredDialog.getByRole('button', { name: 'Retry delivery' })).toHaveCount(0)

    const requestLog = await page.request.get(`${s3API}/__test/requests?artifact_id=${encodeURIComponent(artifact.id)}`)
    expect(requestLog.status()).toBe(200)
    const s3Requests = (await requestLog.json() as { requests: S3Request[] }).requests
    const putRequests = s3Requests.filter((request) => request.method === 'PUT')
    const stored = putRequests.filter((request) => request.outcome === 'stored')
    expect(putRequests.some((request) => request.outcome === 'rejected')).toBe(true)
    expect(stored).toHaveLength(1)
    expect(stored[0]).toEqual(expect.objectContaining({
      content_type: 'image/png',
      content_length: syntheticPNG.length,
      authorization_present: true,
      sigv4_valid: true,
      sigv4_errors: [],
      body_base64: syntheticPNG.toString('base64'),
      decode_error: ''
    }))
    expect(stored[0].path).toMatch(new RegExp(`^/browser-media/delivery/owners/[a-f0-9]{32}/${artifact.id}`))

    const objectResponse = await page.request.get(`${s3API}/__test/objects`)
    const objects = (await objectResponse.json() as { objects: Array<{ path: string; content_type: string; body_base64: string }> }).objects
    expect(objects).toContainEqual(expect.objectContaining({
      content_type: 'image/png',
      body_base64: syntheticPNG.toString('base64')
    }))

    const audit = await envelope<AuditEvent[]>(await page.request.get('/api/v1/console/audit-logs?action=retry_delivery&resource_type=artifact&limit=200', { headers }))
    expect(audit).toContainEqual(expect.objectContaining({ action: 'retry_delivery', resource_type: 'artifact', resource_id: artifact.id }))
    expect(errors).toEqual([])
  } finally {
    await setFixtureMode(page, s3API, 'normal')
    if (initialPlugin?.status !== 'enabled') {
      await page.request.post(`/api/v1/console/plugins/${encodeURIComponent(artifactSinkPluginID)}/disable`, { headers })
    }
    if (destinationCreated) {
      await page.request.delete(
        `/api/v1/console/plugins/${encodeURIComponent(artifactSinkPluginID)}/artifact-sinks/${encodeURIComponent(sinkID)}`,
        { headers }
      )
    }
  }
})
