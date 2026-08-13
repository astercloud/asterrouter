import { expect, test, type Locator, type Page } from '@playwright/test'
import { readFile } from 'node:fs/promises'
import {
  adminPost,
  captureBrowserErrors,
  createDurableImageGatewayFixture,
  createGatewayFixture,
  envelope,
  expectNoHorizontalOverflow,
  loginDemo,
  loginTestPrincipal,
  registerUsers
} from './fixtures'

function uniqueID(testName: string, projectName: string): string {
  return `${testName}-${projectName}-${Date.now()}`.replace(/[^a-z0-9-]+/gi, '-').toLowerCase()
}

async function loginThroughPage(page: Page, email: string, password: string): Promise<void> {
  await page.goto('/login')
  await page.getByLabel('Username').fill(email)
  await page.locator('#password').fill(password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).toHaveURL(/\/portal\/overview$/)
}

function rowFor(table: Locator, text: string): Locator {
  return table.getByRole('row').filter({ hasText: text })
}

function fieldControl(container: Locator, label: string, control = 'input'): Locator {
  return container.locator('.field').filter({ hasText: label }).locator(control).first()
}

test('@e2e-application-001 application and workspace key lifecycle is auditable', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The stateful lifecycle runs once; route surfaces cover every supported viewport.')
  test.setTimeout(90_000)

  const errors = captureBrowserErrors(page)
  const runID = uniqueID('application', testInfo.project.name)
  const applicationName = `Browser Application ${runID}`
  const updatedName = `${applicationName} Updated`
  const keyName = `Browser Key ${runID}`
  const updatedKeyName = `${keyName} Updated`
  const policyName = `Browser Access Policy ${runID}`
  const publicModel = `browser-app-model-${runID}`
  await loginDemo(page)
  const token = await loginTestPrincipal(page)
  await createGatewayFixture(page, token, runID, publicModel)

  await page.goto('/console/applications')
  await page.getByRole('button', { name: 'New application' }).click()
  const applicationDialog = page.getByRole('dialog', { name: 'New application' })
  await applicationDialog.getByLabel('Application name').fill(applicationName)
  await applicationDialog.getByLabel('Application identifier').fill(`browser-app-${runID}`)
  await applicationDialog.getByLabel('Concurrency limit').fill('7')
  await applicationDialog.getByLabel('Entitlement reference').fill(`contract-${runID}`)
  await applicationDialog.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByText('Application created.')).toBeVisible()
  const applicationRow = rowFor(page.getByRole('table'), applicationName)
  await expect(applicationRow).toContainText('7')
  await applicationRow.getByRole('button', { name: `Edit application ${applicationName}` }).click()
  const editDialog = page.getByRole('dialog', { name: 'Edit application' })
  await editDialog.getByLabel('Application name').fill(updatedName)
  await editDialog.getByRole('button', { name: 'Save', exact: true }).click()
  await page.reload()
  await expect(rowFor(page.getByRole('table'), updatedName)).toContainText(`contract-${runID}`)

  await page.goto('/console/applications/credentials')
  await page.getByRole('button', { name: 'New workspace key' }).click()
  const keyDialog = page.locator('.api-key-modal')
  await fieldControl(keyDialog, 'Name').fill(keyName)
  const targetModel = keyDialog.getByRole('button', { name: publicModel, exact: true })
  await expect(targetModel).toBeVisible()
  if (await targetModel.getAttribute('aria-pressed') !== 'true') await targetModel.click()
  await expect(targetModel).toHaveAttribute('aria-pressed', 'true')
  const createKeyResponse = page.waitForResponse((response) =>
    response.request().method() === 'POST' && response.url().endsWith('/api/v1/console/api-keys')
  )
  await keyDialog.getByRole('button', { name: 'Save', exact: true }).click()
  const createdKey = await envelope<{ key: string; record: { id: string } }>(await createKeyResponse)
  await expect(page.getByText('API key created')).toBeVisible()
  const firstSecret = await page.locator('.notice.success input[readonly]').inputValue()
  expect(firstSecret).toBe(createdKey.key)
  let keyRow = rowFor(page.getByRole('table'), keyName)
  await expect(keyRow).toContainText(publicModel)

  await page.reload()
  await expect(page.getByText(firstSecret, { exact: true })).toHaveCount(0)

  await page.goto('/console/policies/access')
  const createPolicyResponse = page.waitForResponse((response) =>
    response.request().method() === 'POST' && response.url().endsWith('/api/v1/console/policies')
  )
  await page.getByRole('button', { name: 'New policy' }).click()
  let policyDialog = page.getByRole('dialog', { name: 'New policy' })
  await policyDialog.getByLabel('Name').fill(policyName)
  await policyDialog.getByLabel('Description').fill('Synthetic application access policy')
  await policyDialog.getByLabel('Scope type').selectOption('api_key')
  await policyDialog.getByLabel('Scope ID').fill(createdKey.record.id)
  await policyDialog.getByLabel('Requests per second (QPS)').fill('8')
  await policyDialog.getByLabel('Monthly token limit').fill('75000')
  await policyDialog.getByLabel('Monthly budget (micros)').fill('2500000')
  await policyDialog.getByRole('button', { name: 'Save', exact: true }).click()
  const policy = await envelope<{ id: string; version: number; scope_type: string; scope_id: string }>(await createPolicyResponse)
  expect(policy).toMatchObject({ version: 1, scope_type: 'api_key', scope_id: createdKey.record.id })
  await expect(page.getByText('Policy created')).toBeVisible()
  let policyRow = rowFor(page.getByRole('table'), policyName)
  await expect(policyRow).toContainText('8 QPS')
  await policyRow.getByRole('button', { name: 'Edit' }).click()
  policyDialog = page.getByRole('dialog', { name: 'Edit policy' })
  await policyDialog.getByLabel('Description').fill('Updated synthetic application access policy')
  await policyDialog.getByLabel('Monthly budget (micros)').fill('3000000')
  await policyDialog.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByText('Policy updated')).toBeVisible()
  await page.reload()
  policyRow = rowFor(page.getByRole('table'), policyName)
  await expect(policyRow).toContainText('Updated synthetic application access policy')
  await expect(policyRow).toContainText('v2')

  await page.goto('/console/applications/credentials')
  keyRow = rowFor(page.getByRole('table'), keyName)
  await keyRow.getByRole('button', { name: 'Edit', exact: true }).click()
  const editKeyDialog = page.locator('.api-key-modal')
  await fieldControl(editKeyDialog, 'Name').fill(updatedKeyName)
  await fieldControl(editKeyDialog, 'Policy', 'select').selectOption(policy.id)
  await fieldControl(editKeyDialog, 'QPS limit').fill('7')
  await fieldControl(editKeyDialog, 'Monthly token limit').fill('54321')
  await editKeyDialog.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByText('API key updated')).toBeVisible()
  await page.reload()
  keyRow = rowFor(page.getByRole('table'), updatedKeyName)
  await expect(keyRow).toContainText(policyName)
  await expect(keyRow).toContainText('54,321')

  const explanationResponse = page.waitForResponse((response) =>
    response.request().method() === 'GET' && response.url().endsWith(`/api/v1/console/api-keys/${createdKey.record.id}/policy-explanation`)
  )
  await keyRow.getByRole('button', { name: 'Details', exact: true }).click()
  const explanation = await envelope<{
    api_key_id: string
    selected_policy_id: string
    selected_policy_name: string
    selected_policy_version: number
    selected_source: string
    candidates: Array<{ policy_id: string; source: string; selected: boolean; reason: string }>
  }>(await explanationResponse)
  expect(explanation).toEqual(expect.objectContaining({
    api_key_id: createdKey.record.id,
    selected_policy_id: policy.id,
    selected_policy_name: policyName,
    selected_policy_version: 2,
    selected_source: 'api_key_explicit'
  }))
  expect(explanation.candidates).toContainEqual(expect.objectContaining({
    policy_id: policy.id,
    source: 'api_key_explicit',
    selected: true
  }))
  const keyDetails = page.locator('.modal-card').filter({ has: page.getByRole('heading', { name: updatedKeyName }) })
  await expect(keyDetails).toContainText(policyName)
  await expect(keyDetails).toContainText('api key explicit')
  await keyDetails.locator('.modal-header .icon-button').click()

  keyRow = rowFor(page.getByRole('table'), updatedKeyName)
  await keyRow.getByRole('button', { name: 'Rotate', exact: true }).click()
  const rotationDialog = page.getByRole('dialog', { name: 'Rotate API key' })
  await rotationDialog.locator('#api-key-rotation-grace').selectOption('0')
  await rotationDialog.getByRole('button', { name: 'Rotate key' }).click()
  await expect(page.getByText('API key rotated. Copy the new key now.')).toBeVisible()
  const rotatedSecret = await page.locator('.notice.success input[readonly]').inputValue()
  expect(rotatedSecret).toMatch(/^ar_/)
  expect(rotatedSecret).not.toBe(firstSecret)

  keyRow = rowFor(page.getByRole('table'), updatedKeyName)
  await keyRow.getByRole('button', { name: 'Disable', exact: true }).click()
  await expect(page.getByText('API key disabled')).toBeVisible()
  await expect(rowFor(page.getByRole('table'), updatedKeyName).filter({ hasText: 'Disabled' }).first()).toBeVisible()

  const audit = await envelope<Array<Record<string, unknown>>>(await page.request.get('/api/v1/console/audit-logs?limit=200', {
    headers: { Authorization: `Bearer ${token}` }
  }))
  expect(audit).toEqual(expect.arrayContaining([
    expect.objectContaining({ action: 'create', resource_type: 'application' }),
    expect.objectContaining({ action: 'create', resource_type: 'api_key' }),
    expect.objectContaining({ action: 'update', resource_type: 'api_key' }),
    expect.objectContaining({ action: 'create', resource_type: 'governance_policy' }),
    expect.objectContaining({ action: 'update', resource_type: 'governance_policy' }),
    expect.objectContaining({ action: 'rotate', resource_type: 'api_key' }),
    expect.objectContaining({ action: 'disable', resource_type: 'api_key' })
  ]))
  await page.goto('/console/system/audit')
  await expect(page.getByRole('heading', { level: 1, name: 'Audit Logs' })).toBeVisible()
  await expectNoHorizontalOverflow(page)
  expect(errors).toEqual([])
})

test('@e2e-identity-001 enterprise identity and organization lifecycle remains scoped', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The stateful lifecycle runs once; route surfaces cover every supported viewport.')
  test.setTimeout(90_000)

  const errors = captureBrowserErrors(page)
  const runID = uniqueID('identity', testInfo.project.name)
  const departmentName = `Finance ${runID}`
  const updatedDepartmentName = `${departmentName} Operations`
  const email = `member-${runID}@example.test`
  const displayName = `Finance Member ${runID}`
  const groupName = `Budget Owners ${runID}`
  const updatedGroupName = `${groupName} Updated`
  await loginDemo(page)

  await page.goto('/console/organization/departments')
  await page.getByRole('button', { name: 'New department' }).click()
  let modal = page.locator('.modal-card')
  await fieldControl(modal, 'Name').fill(departmentName)
  await fieldControl(modal, 'Code').fill(`FIN-${runID.slice(-8)}`.toUpperCase())
  await fieldControl(modal, 'Cost center').fill(`CC-${runID.slice(-8)}`.toUpperCase())
  await fieldControl(modal, 'Monthly budget').fill('25000000')
  await modal.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByText('Department created')).toBeVisible()
  await page.reload()
  await expect(rowFor(page.getByRole('table'), departmentName)).toContainText('25.00')
  await rowFor(page.getByRole('table'), departmentName).getByRole('button', { name: 'Edit' }).click()
  modal = page.locator('.modal-card')
  await fieldControl(modal, 'Name').fill(updatedDepartmentName)
  await fieldControl(modal, 'Cost center').fill(`CC-UPDATED-${runID.slice(-6)}`.toUpperCase())
  await fieldControl(modal, 'Monthly budget').fill('30000000')
  await modal.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByText('Department updated')).toBeVisible()
  await page.reload()
  await expect(rowFor(page.getByRole('table'), updatedDepartmentName)).toContainText('30.00')

  await page.goto('/console/organization')
  await page.getByRole('button', { name: 'New user' }).click()
  modal = page.getByRole('dialog', { name: 'New user' })
  await modal.getByLabel('Email').fill(email)
  await modal.getByLabel('Display name').fill(displayName)
  await modal.getByLabel('Default role').selectOption('developer')
  await modal.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByText('Workspace user created')).toBeVisible()
  await expect(rowFor(page.getByRole('table'), email)).toContainText('Developer')

  await page.goto('/console/organization/groups')
  await page.getByRole('button', { name: 'New organization group' }).click()
  modal = page.locator('.modal-card')
  await fieldControl(modal, 'Group name').fill(groupName)
  await fieldControl(modal, 'Description', 'textarea').fill('Synthetic cross-department budget ownership')
  await modal.locator('label').filter({ hasText: email }).getByRole('checkbox').check()
  await modal.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(rowFor(page.getByRole('table'), groupName)).toContainText('1')
  await page.reload()
  await expect(rowFor(page.getByRole('table'), groupName)).toContainText(displayName)
  await rowFor(page.getByRole('table'), groupName).getByTitle('Edit').click()
  modal = page.locator('.modal-card')
  await fieldControl(modal, 'Group name').fill(updatedGroupName)
  await fieldControl(modal, 'Description', 'textarea').fill('Updated synthetic budget ownership')
  await modal.getByRole('button', { name: 'Save', exact: true }).click()
  await page.reload()
  await expect(rowFor(page.getByRole('table'), updatedGroupName)).toContainText('Updated synthetic budget ownership')

  await page.goto('/console/organization')
  const userRow = rowFor(page.getByRole('table'), email)
  await userRow.getByRole('button', { name: 'Grant role' }).click()
  const bindingDialog = page.getByRole('dialog', { name: 'Grant role' })
  await bindingDialog.getByLabel('Role').selectOption('key_manager')
  await bindingDialog.getByLabel('Scope').selectOption('department')
  const departmentID = await bindingDialog.getByLabel('Scope target').locator('option').filter({ hasText: updatedDepartmentName }).getAttribute('value')
  expect(departmentID).toBeTruthy()
  await bindingDialog.getByLabel('Scope target').selectOption(departmentID!)
  await bindingDialog.getByRole('button', { name: 'Grant role', exact: true }).click()
  await expect(page.getByText('Role binding created')).toBeVisible()
  await page.getByRole('button', { name: 'Role assignments' }).click()
  const bindingRow = rowFor(page.getByRole('table'), email).filter({ hasText: updatedDepartmentName })
  await expect(bindingRow).toContainText('Key manager')
  page.once('dialog', (dialog) => dialog.accept())
  await bindingRow.getByRole('button', { name: 'Revoke' }).click()
  await expect(page.getByText('Role binding revoked')).toBeVisible()

  await page.goto('/console/organization/groups')
  const groupRow = rowFor(page.getByRole('table'), updatedGroupName)
  page.once('dialog', (dialog) => dialog.accept())
  await groupRow.getByTitle('Delete').click()
  await page.reload()
  await expect(rowFor(page.getByRole('table'), updatedGroupName)).toHaveCount(0)

  await page.goto('/console/system/audit')
  await expect(page.getByRole('table')).toContainText('department')
  await expect(page.getByRole('table')).toContainText('organization_group')
  await expectNoHorizontalOverflow(page)
  expect(errors).toEqual([])
})

test('@e2e-routing-resources-001 route groups and simulator use the published routing contract', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The stateful lifecycle runs once; route surfaces cover every supported viewport.')
  test.setTimeout(60_000)

  const errors = captureBrowserErrors(page)
  const runID = uniqueID('routing', testInfo.project.name)
  const groupName = `Browser Route Group ${runID}`
  const updatedDescription = `Updated routing resource contract ${runID}`
  const publicModel = `browser-route-model-${runID}`
  await loginDemo(page)
  const token = await loginTestPrincipal(page)
  await createGatewayFixture(page, token, runID, publicModel)

  await page.goto('/console/model-services/route-groups')
  await page.getByRole('button', { name: 'New policy group' }).click()
  const modal = page.locator('.modal-card')
  await fieldControl(modal, 'Policy group name').fill(groupName)
  await fieldControl(modal, 'Platform').fill('openai_compatible')
  await fieldControl(modal, 'Description').fill('Synthetic routing resource contract')
  await modal.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByText('Routing policy group created')).toBeVisible()
  await page.reload()
  await expect(rowFor(page.getByRole('table'), groupName)).toContainText('openai_compatible')
  const groupRow = rowFor(page.getByRole('table'), groupName)
  await groupRow.getByRole('button', { name: 'Edit' }).click()
  const editGroupDialog = page.locator('.modal-card')
  await fieldControl(editGroupDialog, 'Description').fill(updatedDescription)
  await fieldControl(editGroupDialog, 'Cost weight').fill('1.25')
  await fieldControl(editGroupDialog, 'RPM limit').fill('321')
  await editGroupDialog.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByText('Routing policy group updated')).toBeVisible()
  await page.reload()
  await expect(rowFor(page.getByRole('table'), groupName)).toContainText(updatedDescription)
  await expect(rowFor(page.getByRole('table'), groupName)).toContainText('1.25x')
  await expect(rowFor(page.getByRole('table'), groupName)).toContainText('RPM limit 321')

  await page.goto('/console/model-services/simulator')
  await page.getByLabel('Requested model').selectOption(publicModel)
  await page.getByRole('button', { name: 'Run simulation' }).click()
  await expect(page.locator('.simulation-flow')).toContainText(publicModel)
  await expect(page.getByRole('table')).toContainText('upstream-model')
  await expect(page.locator('.crud-summary')).toContainText('1 / 1')
  await expect(page.getByRole('table')).toContainText('eligible')
  await page.getByRole('checkbox', { name: 'tools' }).check()
  await page.getByRole('button', { name: 'Run simulation' }).click()
  await expect(page.locator('.crud-summary')).toContainText('candidates')
  await expectNoHorizontalOverflow(page)
  expect(errors).toEqual([])
})

test('@e2e-operations-001 gateway evidence reaches enterprise operations views', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The evidence workflow runs once; route surfaces cover every supported viewport.')
  test.setTimeout(90_000)

  const errors = captureBrowserErrors(page)
  const runID = uniqueID('operations', testInfo.project.name)
  const publicModel = `browser-ops-model-${runID}`
  const imageModel = `browser-image-model-${runID}`
  await loginDemo(page)
  const token = await loginTestPrincipal(page)
  await createGatewayFixture(page, token, runID, publicModel)
  const imageFixture = await createDurableImageGatewayFixture(page, token, runID, imageModel)
  const workspaceKey = await adminPost<{ key: string; record: { id: string } }>(page, token, '/api-keys', {
    name: `Operations Key ${runID}`,
    model_allowlist: [publicModel],
    qps_limit: 10,
    monthly_token_limit: 18
  })
  const completion = await page.request.post('/v1/chat/completions', {
    data: { model: publicModel, messages: [{ role: 'user', content: 'synthetic operations evidence request' }] },
    headers: { Authorization: `Bearer ${workspaceKey.key}` }
  })
  expect(completion.status()).toBe(200)

  const imageKey = await adminPost<{ key: string; record: { id: string } }>(page, token, '/api-keys', {
    name: `Operations Image Key ${runID}`,
    scopes: ['gateway:invoke', 'jobs:read', 'jobs:cancel'],
    model_allowlist: [imageModel],
    allowed_modalities: ['image'],
    allowed_operations: ['image_generation'],
    lane_policy: 'direct_and_durable',
    artifact_policy: 'temporary'
  })
  const imageHeaders = { Authorization: `Bearer ${imageKey.key}` }
  const directImageResponse = await page.request.post('/v1/images/generations', {
    headers: { ...imageHeaders, 'Idempotency-Key': `image-direct-${runID}` },
    data: { model: imageModel, prompt: 'synthetic direct image', delivery_mode: 'inline' }
  })
  expect(directImageResponse.status()).toBe(200)
  const queuedJobResponse = await page.request.post('/v1/jobs', {
    headers: { ...imageHeaders, 'Idempotency-Key': `image-queued-${runID}` },
    data: { model: imageModel, operation: 'image_generation', modality: 'image', input: { prompt: 'synthetic queued image', count: 1 } }
  })
  expect(queuedJobResponse.status()).toBe(202)
  const queuedJob = await queuedJobResponse.json() as { id: string }
  await expect.poll(async () => {
    const response = await page.request.get(`/v1/jobs/${queuedJob.id}`, { headers: imageHeaders })
    return (await response.json() as { status: string }).status
  }).toBe('queued')

  for (const route of ['/console/usage', '/console/usage/traces']) {
    await page.goto(route)
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible()
    await expect(page.getByRole('main')).toContainText(publicModel)
  }

  await page.goto('/console/usage/cost-allocation')
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible()
  await page.getByRole('button', { name: 'By model' }).click()
  await expect(page.getByRole('main')).toContainText(publicModel)

  await page.goto('/console/usage/jobs')
  await expect(page.locator('.runtime-heading')).toContainText('Online')
  await expect(rowFor(page.getByRole('table'), queuedJob.id)).toContainText('queued')
  await rowFor(page.getByRole('table'), queuedJob.id).getByRole('button', { name: 'Details' }).click()
  const queuedJobDialog = page.getByRole('dialog')
  await expect(queuedJobDialog).toContainText(queuedJob.id)
  page.once('dialog', (dialog) => dialog.accept())
  await queuedJobDialog.getByRole('button', { name: 'Cancel job' }).click()
  await expect(page.getByText('Job cancellation requested.')).toBeVisible()
  await expect(queuedJobDialog).toContainText('canceled')
  await queuedJobDialog.locator('.modal-footer').getByRole('button', { name: 'Close', exact: true }).click()
  await page.reload()
  await expect(rowFor(page.getByRole('table'), queuedJob.id)).toContainText('canceled')

  await envelope(await page.request.put(`/api/v1/console/provider-accounts/${imageFixture.account.id}`, {
    headers: { Authorization: `Bearer ${token}` },
    data: {
      provider_id: imageFixture.account.provider_id,
      name: imageFixture.account.name,
      platform: 'openai_compatible',
      auth_type: 'api_key',
      status: 'active',
      schedulable: true,
      priority: 10,
      weight: 100,
      concurrency: 1,
      rpm_limit: 0,
      tpm_limit: 0,
      rate_multiplier: 1,
      models: ['upstream-image-model'],
      auto_enable_new_models: false,
      group_ids: [],
      secret: ''
    }
  }))
  const imageJobResponse = await page.request.post('/v1/jobs', {
    headers: { ...imageHeaders, 'Idempotency-Key': `image-ready-${runID}` },
    data: { model: imageModel, operation: 'image_generation', modality: 'image', input: { prompt: 'synthetic ready image', count: 1 } }
  })
  expect(imageJobResponse.status()).toBe(202)
  const imageJob = await imageJobResponse.json() as { id: string }
  await expect.poll(async () => {
    const response = await page.request.get(`/v1/jobs/${imageJob.id}`, { headers: imageHeaders })
    return (await response.json() as { status: string }).status
  }, { timeout: 20_000 }).toBe('succeeded')
  const completedJob = await envelope<{ artifacts: Array<{ id: string }> }>(
    await page.request.get(`/api/v1/console/ai-jobs/${imageJob.id}`, { headers: { Authorization: `Bearer ${token}` } })
  )
  expect(completedJob.artifacts).toHaveLength(1)
  const artifactID = completedJob.artifacts[0].id

  await page.goto('/console/usage/jobs')
  const completedJobRow = rowFor(page.getByRole('table'), imageJob.id)
  await expect(completedJobRow).toContainText('succeeded')
  await completedJobRow.getByRole('button', { name: 'Details' }).click()
  const completedJobDialog = page.getByRole('dialog')
  await expect(completedJobDialog).toContainText('accepted')
  await expect(completedJobDialog).toContainText('image/png')
  await completedJobDialog.locator('.modal-footer').getByRole('button', { name: 'Close', exact: true }).click()

  for (const route of ['/console/usage/supply']) {
    await page.goto(route)
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible()
    await expectNoHorizontalOverflow(page)
  }

  await page.goto('/console/usage/artifacts')
  const artifactRow = rowFor(page.getByRole('table'), artifactID)
  await expect(artifactRow).toContainText('ready')
  await artifactRow.getByRole('button', { name: 'Details' }).click()
  const artifactDialog = page.getByRole('dialog')
  const preview = artifactDialog.getByRole('img', { name: /Preview of/ })
  await expect(preview).toBeVisible()
  await expect.poll(() => preview.evaluate((image: HTMLImageElement) => image.naturalWidth)).toBeGreaterThan(0)
  const artifactDownloadStarted = page.waitForEvent('download')
  await artifactDialog.getByRole('button', { name: 'Download' }).click()
  const artifactDownload = await artifactDownloadStarted
  expect(artifactDownload.suggestedFilename()).toMatch(/^artifact_.+\.png$/)
  const artifactDownloadPath = await artifactDownload.path()
  expect(artifactDownloadPath).toBeTruthy()
  const artifactBytes = await readFile(artifactDownloadPath!)
  expect(artifactBytes.subarray(1, 4).toString('ascii')).toBe('PNG')
  await artifactDialog.locator('.modal-footer').getByRole('button', { name: 'Close', exact: true }).click()
  await page.reload()
  await expect(rowFor(page.getByRole('table'), artifactID)).toContainText('ready')

  await page.goto('/console/usage/alerts')
  const alertTable = page.getByRole('table')
  let alertRow = rowFor(alertTable, workspaceKey.record.id)
  await expect(alertRow).toContainText('Critical')
  await alertRow.getByRole('button', { name: 'Acknowledge' }).click()
  await page.locator('.table-toolbar select').nth(2).selectOption('acknowledged')
  alertRow = rowFor(alertTable, workspaceKey.record.id)
  await expect(alertRow).toContainText('Acknowledged')
  await alertRow.getByRole('button', { name: 'Resolve' }).click()
  await page.locator('.table-toolbar select').nth(2).selectOption('resolved')
  alertRow = rowFor(alertTable, workspaceKey.record.id)
  await expect(alertRow).toContainText('Resolved')
  await expect(alertRow.getByText('Closed')).toBeVisible()
  await page.reload()
  await page.locator('.table-toolbar select').nth(2).selectOption('resolved')
  await expect(rowFor(page.getByRole('table'), workspaceKey.record.id)).toContainText('Resolved')
  await expectNoHorizontalOverflow(page)

  await page.goto('/console/usage/exports')
  await page.getByRole('button', { name: 'Create export job' }).click()
  const exportDialog = page.locator('.modal-card')
  await exportDialog.getByLabel('Data type').selectOption('usage')
  await exportDialog.getByLabel('Model').fill(publicModel)
  await exportDialog.getByRole('button', { name: 'Create export job' }).click()
  await expect(page.getByText('Export job created')).toBeVisible()
  const exportRow = rowFor(page.getByRole('table'), publicModel).first()
  await expect(exportRow).toContainText('Succeeded', { timeout: 15_000 })
  await expect(exportRow).toContainText('1 rows')
  const downloadStarted = page.waitForEvent('download')
  await exportRow.getByRole('button', { name: 'Download' }).click()
  const download = await downloadStarted
  expect(download.suggestedFilename()).toBe('usage-records.csv')
  const downloadPath = await download.path()
  expect(downloadPath).toBeTruthy()
  const csv = await readFile(downloadPath!, 'utf8')
  expect(csv).toContain(publicModel)
  expect(csv).toContain(workspaceKey.record.id)

  await page.goto('/console/system/audit')
  await expect(page.getByRole('table')).toContainText('gateway_call')
  const audit = await envelope<Array<{ action: string; resource_type: string; resource_id: string }>>(
    await page.request.get('/api/v1/console/audit-logs?limit=200', { headers: { Authorization: `Bearer ${token}` } })
  )
  expect(audit).toContainEqual(expect.objectContaining({ action: 'acknowledge', resource_type: 'alert_event' }))
  expect(audit).toContainEqual(expect.objectContaining({ action: 'resolve', resource_type: 'alert_event' }))
  expect(audit).toContainEqual(expect.objectContaining({ action: 'create', resource_type: 'export_job' }))
  expect(audit).toContainEqual(expect.objectContaining({ action: 'download', resource_type: 'export_job' }))
  expect(audit).toContainEqual(expect.objectContaining({ action: 'cancel', resource_type: 'ai_job', resource_id: queuedJob.id }))
  await expectNoHorizontalOverflow(page)
  expect(errors).toEqual([])
})

test('@e2e-portal-001 developer portal key and usage projection is isolated', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The stateful lifecycle runs once; portal surfaces cover every supported viewport.')
  test.setTimeout(90_000)

  const errors = captureBrowserErrors(page)
  const runID = uniqueID('portal', testInfo.project.name)
  const publicModel = `browser-portal-model-${runID}`
  const password = 'synthetic-password-123'
  await loginDemo(page)
  const token = await loginTestPrincipal(page)
  await createGatewayFixture(page, token, runID, publicModel)
  const [developer] = await registerUsers(page, token, [{
    email: `portal-${runID}@example.test`,
    password,
    displayName: 'Portal Lifecycle User'
  }])

  await page.context().clearCookies()
  await page.evaluate(() => localStorage.clear())
  await loginThroughPage(page, developer.email, password)
  await page.goto('/portal/applications')
  await page.locator('.portal-keys-heading').getByRole('button', { name: 'Create new Key' }).click()
  const createPanel = page.locator('.portal-create-panel')
  const keyName = `Portal Key ${runID}`
  await createPanel.getByLabel('Name').fill(keyName)
  const selectedModels = await createPanel.locator('.chip-list .pill.status-success').allTextContents()
  for (const model of selectedModels.filter((model) => model !== publicModel)) {
    await createPanel.getByRole('button', { name: model, exact: true }).click()
  }
  const publicModelButton = createPanel.getByRole('button', { name: publicModel, exact: true })
  if (!selectedModels.includes(publicModel)) await publicModelButton.click()
  await expect(createPanel.locator('.chip-list .pill.status-success')).toHaveCount(1)
  await expect(publicModelButton).toHaveClass(/status-success/)
  await createPanel.getByRole('button', { name: 'Create new Key' }).click()
  await expect(page.locator('.notice.success')).toContainText('API key created. The full secret is shown once')
  const firstSecret = await page.locator('.global-key-line code').textContent()
  expect(firstSecret).toMatch(/^ar_/)
  let keyRow = rowFor(page.getByRole('table'), keyName)
  await expect(keyRow).toContainText(publicModel)

  page.once('dialog', (dialog) => dialog.accept())
  await keyRow.getByTitle('Rotate key').click()
  await expect(page.getByText('API key rotated. The old secret is invalid; copy the new one now.')).toBeVisible()
  const rotatedSecret = await page.locator('.global-key-line code').textContent()
  expect(rotatedSecret).toMatch(/^ar_/)
  expect(rotatedSecret).not.toBe(firstSecret)

  await page.goto('/portal/access')
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible()
  await expect(page.getByRole('option', { name: publicModel })).toHaveCount(1)
  await page.goto('/portal/usage')
  await expect(page.getByRole('heading', { level: 1, name: 'Employee Portal' })).toBeVisible()
  await page.goto('/portal/account')
  await expect(page.getByLabel('Email')).toHaveValue(developer.email)

  await page.goto('/console/workbench')
  await expect(page).toHaveURL(/\/portal\/overview$/)
  await page.goto('/portal/applications')
  keyRow = rowFor(page.getByRole('table'), keyName).filter({ hasText: 'Active' })
  page.once('dialog', (dialog) => dialog.accept())
  await keyRow.getByTitle('Disable key').click()
  await expect(page.getByText('API key disabled.')).toBeVisible()
  await expect(rowFor(page.getByRole('table'), keyName).filter({ hasText: 'Disabled' }).first()).toBeVisible()
  expect(errors).toEqual([])
})
