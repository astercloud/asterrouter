import { expect, test, type APIResponse, type Page } from '@playwright/test'
import { adminPost, createGatewayFixture, envelope, loginDemo, loginUser, registerUsers } from './fixtures'

type WorkspaceUser = { id: string; email: string; department_id?: string }
type APIKeyResult = { key: string; record: { id: string; owner_user_id: string } }

async function adminGet<T>(page: Page, token: string, path: string): Promise<T> {
  return envelope<T>(await page.request.get(`/api/v1/admin${path}`, {
    headers: { Authorization: `Bearer ${token}` }
  }))
}

async function updateUser(page: Page, token: string, user: WorkspaceUser, departmentID: string): Promise<void> {
  await envelope(await page.request.put(`/api/v1/admin/users/${user.id}`, {
    headers: { Authorization: `Bearer ${token}` },
    data: {
      email: user.email,
      display_name: user.email.split('@')[0],
      status: 'active',
      role: 'developer',
      department_id: departmentID
    }
  }))
}

async function invoke(page: Page, key: string, model: string): Promise<void> {
  const response = await page.request.post('/v1/chat/completions', {
    headers: { Authorization: `Bearer ${key}` },
    data: { model, messages: [{ role: 'user', content: `synthetic isolation request for ${model}` }] }
  })
  expect(response.status(), await response.text()).toBe(200)
}

async function expectForbiddenOrHidden(response: APIResponse): Promise<void> {
  expect([403, 404]).toContain(response.status())
}

test('@smoke @j03 department and owner isolation covers reads, writes, and exports', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The isolation workflow is viewport-independent and runs once on desktop.')

  await loginDemo(page)
  const adminToken = await page.evaluate(() => localStorage.getItem('asterrouter_admin_token') || '')
  expect(adminToken).not.toBe('')

  const runID = `${testInfo.project.name}-${Date.now()}`
  const password = 'synthetic-password-123'
  const registered = await registerUsers(page, adminToken, [
    { email: `eng-manager-${runID}@example.test`, password, displayName: 'Engineering Manager' },
    { email: `fin-manager-${runID}@example.test`, password, displayName: 'Finance Manager' },
    { email: `eng-a-${runID}@example.test`, password, displayName: 'Engineer A' },
    { email: `eng-b-${runID}@example.test`, password, displayName: 'Engineer B' },
    { email: `fin-a-${runID}@example.test`, password, displayName: 'Finance A' }
  ])
  const [engManager, finManager, engA, engB, finA] = registered

  const engineering = await adminPost<{ id: string }>(page, adminToken, '/departments', {
    name: `Engineering ${runID}`,
    code: `eng-${runID}`,
    status: 'active'
  })
  const finance = await adminPost<{ id: string }>(page, adminToken, '/departments', {
    name: `Finance ${runID}`,
    code: `fin-${runID}`,
    status: 'active'
  })
  for (const user of [engManager, engA, engB]) await updateUser(page, adminToken, user, engineering.id)
  for (const user of [finManager, finA]) await updateUser(page, adminToken, user, finance.id)
  for (const [user, department] of [[engManager, engineering], [finManager, finance]] as const) {
    await adminPost(page, adminToken, '/role-bindings', {
      user_id: user.id,
      role: 'platform_admin',
      scope_type: 'department',
      scope_id: department.id
    })
  }

  const publicModel = `e2e-isolation-${runID}`
  await createGatewayFixture(page, adminToken, runID, publicModel)
  const createOwnedKey = (user: WorkspaceUser) => adminPost<APIKeyResult>(page, adminToken, '/api-keys', {
    name: `Owned key ${user.email}`,
    key_type: 'user',
    owner_user_id: user.id,
    model_allowlist: [publicModel],
    qps_limit: 10,
    monthly_token_limit: 18
  })
  const [engAKey, engBKey, finAKey] = await Promise.all([createOwnedKey(engA), createOwnedKey(engB), createOwnedKey(finA)])
  await invoke(page, engAKey.key, publicModel)
  await invoke(page, engBKey.key, publicModel)
  await invoke(page, finAKey.key, publicModel)

  const engManagerToken = await loginUser(page, engManager.email, password)
  const finManagerToken = await loginUser(page, finManager.email, password)
  const engAToken = await loginUser(page, engA.email, password)

  const visibleUsers = await adminGet<WorkspaceUser[]>(page, engManagerToken, '/users')
  expect(visibleUsers.map((user) => user.id).sort()).toEqual([engManager.id, engA.id, engB.id].sort())
  expect(visibleUsers).not.toContainEqual(expect.objectContaining({ id: finA.id }))

  const visibleKeys = await adminGet<Array<{ id: string }>>(page, engManagerToken, '/api-keys')
  expect(visibleKeys.map((key) => key.id).sort()).toEqual([engAKey.record.id, engBKey.record.id].sort())
  const usage = await adminGet<{ total_requests: number; recent: Array<{ api_key_id: string }> }>(page, engManagerToken, '/usage?limit=100')
  expect(usage.total_requests).toBe(2)
  expect(usage.recent.map((item) => item.api_key_id).sort()).toEqual([engAKey.record.id, engBKey.record.id].sort())
  const bypassUsage = await adminGet<{ total_requests: number }>(page, engManagerToken, `/usage?api_key_id=${finAKey.record.id}`)
  expect(bypassUsage.total_requests).toBe(0)

  const traces = await adminGet<Array<{ api_key_id: string }>>(page, engManagerToken, '/gateway-traces?limit=100')
  expect(traces.map((trace) => trace.api_key_id).sort()).toEqual([engAKey.record.id, engBKey.record.id].sort())
  const alerts = await adminGet<Array<{ resource_id: string }>>(page, engManagerToken, '/alerts?limit=100')
  expect(alerts.map((alert) => alert.resource_id).sort()).toEqual([engAKey.record.id, engBKey.record.id].sort())
  const costs = await adminGet<{ rows: Array<{ resource_id: string }> }>(page, engManagerToken, '/cost-allocation?dimension=user')
  expect(costs.rows.map((row) => row.resource_id).sort()).toEqual([engA.id, engB.id].sort())
  expect(costs.rows).not.toContainEqual(expect.objectContaining({ resource_id: finA.id }))

  const usageCSV = await page.request.get('/api/v1/admin/usage/export?limit=100', {
    headers: { Authorization: `Bearer ${engManagerToken}` }
  })
  expect(usageCSV.status()).toBe(200)
  const usageCSVBody = await usageCSV.text()
  expect(usageCSVBody).toContain(engAKey.record.id)
  expect(usageCSVBody).toContain(engBKey.record.id)
  expect(usageCSVBody).not.toContain(finAKey.record.id)

  const exportJob = await adminPost<{ id: string }>(page, engManagerToken, '/export-jobs?kind=gateway_traces&limit=100', {})
  await expectForbiddenOrHidden(await page.request.get(`/api/v1/admin/export-jobs/${exportJob.id}`, {
    headers: { Authorization: `Bearer ${finManagerToken}` }
  }))
  await expect.poll(async () => {
    const job = await adminGet<{ status: string }>(page, engManagerToken, `/export-jobs/${exportJob.id}`)
    return job.status
  }).toBe('succeeded')
  const exportDownload = await page.request.get(`/api/v1/admin/export-jobs/${exportJob.id}/download`, {
    headers: { Authorization: `Bearer ${engManagerToken}` }
  })
  expect(exportDownload.status()).toBe(200)
  const exportBody = await exportDownload.text()
  expect(exportBody).toContain(engAKey.record.id)
  expect(exportBody).toContain(engBKey.record.id)
  expect(exportBody).not.toContain(finAKey.record.id)

  await expectForbiddenOrHidden(await page.request.post(`/api/v1/admin/api-keys/${finAKey.record.id}/disable`, {
    headers: { Authorization: `Bearer ${engManagerToken}` }
  }))
  const portal = await envelope<{ api_keys: Array<{ id: string }>; usage: { recent: Array<{ api_key_id: string }> }; recent_traces: Array<{ api_key_id: string }>; alerts: Array<{ resource_id: string }> }>(
    await page.request.get('/api/v1/portal/workspace', { headers: { Authorization: `Bearer ${engAToken}` } })
  )
  expect(portal.api_keys.map((key) => key.id)).toEqual([engAKey.record.id])
  expect(portal.usage.recent.map((item) => item.api_key_id)).toEqual([engAKey.record.id])
  expect(portal.recent_traces.map((trace) => trace.api_key_id)).toEqual([engAKey.record.id])
  expect(portal.alerts.map((alert) => alert.resource_id)).toEqual([engAKey.record.id])
  expect((await page.request.post(`/api/v1/portal/api-keys/${engBKey.record.id}/disable`, {
    headers: { Authorization: `Bearer ${engAToken}` }
  })).status()).toBe(404)
})
