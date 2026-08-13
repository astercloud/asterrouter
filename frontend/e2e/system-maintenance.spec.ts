import { gunzipSync } from 'node:zlib'
import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import { expect, test } from '@playwright/test'
import { captureBrowserErrors, envelope, loginDemo, loginTestPrincipal } from './fixtures'

type SystemArchiveInfo = {
  id: string
  path: string
  size_bytes: number
  created_at: string
}

type Diagnostic = {
  schema_version: string
  created_at: string
  version: string
  build_type: string
  platform: string
  database_configured: boolean
  details: {
    settings_health: string
    control_plane_health: string
    settings: {
      default_locale: string
      enabled_locales: string[]
      service_center_mode: string
      storage_mode: string
      demo_mode: boolean
    }
  }
}

type AuditEvent = { action: string; resource_type: string; resource_id: string }
type S3Request = { method: string; path: string; sigv4_valid: boolean; sigv4_errors: string[]; outcome: string }

type Application = { id: string; name: string; slug: string }

type SystemApplyResult = {
  message: string
  operation_id: string
  need_restart: boolean
  current_version: string
  latest_version: string
}

function sha256(value: Buffer): string {
  return createHash('sha256').update(value).digest('hex')
}

async function waitForSystemVersion(
  page: import('@playwright/test').Page,
  headers: Record<string, string>,
  version: string
): Promise<void> {
  await expect.poll(async () => {
    try {
      const response = await page.request.get('/api/v1/console/system/version', { headers, timeout: 2_000 })
      if (response.status() !== 200) return ''
      return ((await response.json()).data || {}).version || ''
    } catch {
      return ''
    }
  }, { timeout: 30_000, intervals: [100, 200, 500, 1_000] }).toBe(version)
}

async function waitForRuntimeGeneration(path: string, count: number): Promise<void> {
  await expect.poll(async () => {
    try {
      return (await readFile(path, 'utf8')).split('\n').filter((line) => line.startsWith('start ')).length
    } catch {
      return 0
    }
  }, { timeout: 30_000, intervals: [100, 200, 500, 1_000] }).toBeGreaterThanOrEqual(count)
}

async function createApplicationMarker(page: import('@playwright/test').Page, headers: Record<string, string>, kind: string): Promise<Application> {
  const timestamp = Date.now()
  return envelope<Application>(await page.request.post('/api/v1/applications', {
    headers,
    data: {
      name: `${kind} restore marker ${timestamp}`,
      slug: `${kind.toLowerCase()}-restore-marker-${timestamp}`,
      entitlement_reference: '',
      concurrency_limit: 1,
      status: 'active'
    }
  }))
}

async function applicationExists(page: import('@playwright/test').Page, headers: Record<string, string>, id: string): Promise<boolean> {
  const applications = await envelope<Application[]>(await page.request.get('/api/v1/applications', { headers }))
  return applications.some((item) => item.id === id)
}

function readTarFile(archive: Buffer, filename: string): Buffer {
  const tar = gunzipSync(archive)
  for (let offset = 0; offset + 512 <= tar.length;) {
    const header = tar.subarray(offset, offset + 512)
    if (header.every((byte) => byte === 0)) break
    const name = header.subarray(0, 100).toString('utf8').replace(/\0.*$/, '')
    const rawSize = header.subarray(124, 136).toString('ascii').replace(/\0.*$/, '').trim()
    const size = Number.parseInt(rawSize || '0', 8)
    expect(Number.isSafeInteger(size), `invalid tar size for ${name}`).toBe(true)
    const bodyStart = offset + 512
    if (name === filename) return tar.subarray(bodyStart, bodyStart + size)
    offset = bodyStart + Math.ceil(size / 512) * 512
  }
  throw new Error(`${filename} is missing from diagnostic archive`)
}

test('@e2e-system-diagnostic-001 diagnostic bundle is created, downloaded, redacted, and audited', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The stateful archive workflow runs once.')
  test.setTimeout(60_000)

  const errors = captureBrowserErrors(page)
  await loginDemo(page)
  const token = await loginTestPrincipal(page)
  await page.goto('/console/system')
  await page.getByRole('tab', { name: 'Data backup' }).click()

  const panel = page.locator('.panel').filter({ has: page.getByRole('heading', { name: 'Backup & diagnostics' }) })
  const createResponse = page.waitForResponse((response) =>
    response.request().method() === 'POST' && response.url().endsWith('/api/v1/console/system/diagnostics')
  )
  const downloadResponse = page.waitForResponse((response) =>
    response.request().method() === 'GET' && /\/api\/v1\/console\/system\/diagnostics\/[^/]+\/download$/.test(response.url())
  )
  const downloadEvent = page.waitForEvent('download')
  await panel.getByRole('button', { name: 'Create diagnostic bundle' }).click()

  const bundle = await envelope<SystemArchiveInfo>(await createResponse)
  expect(bundle.id).toMatch(/^asterrouter-diagnostic-[A-Za-z0-9_-]+$/)
  expect(bundle.path).toBe(`${bundle.id}.tar.gz`)
  expect(bundle.size_bytes).toBeGreaterThan(0)
  expect(Date.parse(bundle.created_at)).not.toBeNaN()

  const downloaded = await downloadEvent
  expect((await downloadResponse).status()).toBe(200)
  expect(downloaded.suggestedFilename()).toBe(bundle.path)
  const downloadPath = testInfo.outputPath(bundle.path)
  await downloaded.saveAs(downloadPath)
  const archive = await readFile(downloadPath)
  expect(archive.length).toBe(bundle.size_bytes)

  const diagnosticBytes = readTarFile(archive, 'diagnostic.json')
  const diagnostic = JSON.parse(diagnosticBytes.toString('utf8')) as Diagnostic
  expect(diagnostic).toEqual(expect.objectContaining({
    schema_version: 'asterrouter.diagnostic.v1',
    version: expect.any(String),
    build_type: expect.any(String),
    platform: expect.stringMatching(/^[^/]+\/[^/]+$/),
    database_configured: expect.any(Boolean),
    details: expect.objectContaining({
      settings_health: 'ok',
      control_plane_health: 'ok',
      settings: expect.objectContaining({
        default_locale: expect.any(String),
        enabled_locales: expect.any(Array),
        service_center_mode: expect.any(String),
        storage_mode: expect.any(String),
        demo_mode: expect.any(Boolean)
      })
    })
  }))
  expect(Date.parse(diagnostic.created_at)).not.toBeNaN()
  const diagnosticText = diagnosticBytes.toString('utf8')
  expect(diagnosticText).not.toContain('postgres://')
  expect(diagnosticText).not.toContain('asterrouter-e2e-test-secret')

  await expect(page.getByText('Diagnostic bundle created')).toBeVisible()
  const audit = await envelope<AuditEvent[]>(await page.request.get('/api/v1/console/audit-logs?action=diagnostic&resource_type=system&limit=200', {
    headers: { Authorization: `Bearer ${token}` }
  }))
  expect(audit).toContainEqual(expect.objectContaining({
    action: 'diagnostic',
    resource_type: 'system',
    resource_id: bundle.id
  }))
  expect(errors).toEqual([])
})

test('@e2e-system-update-001 source build maintenance actions fail closed with audit evidence', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The maintenance command contract runs once on desktop.')
  test.setTimeout(60_000)

  const errors = captureBrowserErrors(page)
  await loginDemo(page)
  const token = await loginTestPrincipal(page)
  await page.goto('/console/system')
  await page.getByRole('tab', { name: 'Data backup' }).click()

  const updatePanel = page.locator('.panel').filter({ has: page.getByRole('heading', { name: 'System Update' }) })
  const checkResponse = page.waitForResponse((response) =>
    response.request().method() === 'GET' && response.url().includes('/api/v1/console/system/check-updates')
  )
  await updatePanel.getByRole('button', { name: 'Check updates', exact: true }).click()
  expect((await checkResponse).status()).toBe(200)
  await expect(page.getByText('Update check completed', { exact: true })).toBeVisible()
  await expect(updatePanel.getByText('Update available', { exact: true })).toBeVisible()
  await expect(updatePanel.getByText('Signed catalog', { exact: true })).toBeVisible()
  await expect(updatePanel.getByText('Signed metadata', { exact: true })).toBeVisible()
  await expect(updatePanel.getByLabel('Latest version')).toHaveValue('0.99.0')
  await expect(updatePanel.getByText(/not produced as a release artifact.*manual update/i)).toBeVisible()

  for (const action of [
    { button: 'One-click update', endpoint: '/api/v1/console/system/update', status: 409, message: /one-click update is not supported.*download the matching release artifact/i },
    { button: 'Rollback', endpoint: '/api/v1/console/system/rollback', status: 500, message: /no rollback backup found/i },
    { button: 'Restart', endpoint: '/api/v1/console/system/restart', status: 409, message: /service restart is not enabled.*restart the service manually/i }
  ]) {
    const responsePromise = page.waitForResponse((response) =>
      response.request().method() === 'POST' && response.url().endsWith(action.endpoint)
    )
    await updatePanel.getByRole('button', { name: action.button, exact: true }).click()
    expect((await responsePromise).status()).toBe(action.status)
    await expect(page.getByText(action.message)).toBeVisible()
  }

  const audit = await envelope<AuditEvent[]>(await page.request.get('/api/v1/console/audit-logs?resource_type=system&limit=200', {
    headers: { Authorization: `Bearer ${token}` }
  }))
  expect(audit).toEqual(expect.arrayContaining([
    expect.objectContaining({ action: 'check_update', resource_type: 'system' }),
    expect.objectContaining({ action: 'update_failed', resource_type: 'system' }),
    expect.objectContaining({ action: 'rollback_failed', resource_type: 'system' }),
    expect.objectContaining({ action: 'restart_rejected', resource_type: 'system' })
  ]))
  expect(errors).toEqual([
    'console: Failed to load resource: the server responded with a status of 409 (Conflict)',
    'console: Failed to load resource: the server responded with a status of 500 (Internal Server Error)',
    'console: Failed to load resource: the server responded with a status of 409 (Conflict)'
  ])
})

test('@e2e-system-update-lifecycle-001 release binary updates, restarts, rolls back, and restarts through the browser', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The managed release lifecycle runs once on desktop.')
  test.skip(process.env.ASTER_E2E_SYSTEM_UPDATE_LIFECYCLE !== '1', 'The lifecycle requires dedicated release binaries, PostgreSQL, and a supervisor.')
  test.setTimeout(120_000)

  const runtimeBinary = process.env.ASTER_E2E_SYSTEM_UPDATE_RUNTIME_BINARY || ''
  const generationFile = process.env.ASTER_E2E_SYSTEM_UPDATE_GENERATION_FILE || ''
  const oldSHA = process.env.ASTER_E2E_SYSTEM_UPDATE_OLD_SHA256 || ''
  const newSHA = process.env.ASTER_E2E_SYSTEM_UPDATE_NEW_SHA256 || ''
  const oldVersion = process.env.ASTER_E2E_SYSTEM_UPDATE_OLD_VERSION || '0.24.0'
  const newVersion = process.env.ASTER_E2E_SYSTEM_UPDATE_NEW_VERSION || '0.99.0'
  const officialURL = process.env.ASTER_E2E_OFFICIAL_URL || ''
  expect(runtimeBinary).not.toBe('')
  expect(generationFile).not.toBe('')
  expect(oldSHA).toMatch(/^[a-f0-9]{64}$/)
  expect(newSHA).toMatch(/^[a-f0-9]{64}$/)

  const errors = captureBrowserErrors(page)
  await loginDemo(page)
  const token = await loginTestPrincipal(page)
  const headers = { Authorization: `Bearer ${token}` }
  await waitForSystemVersion(page, headers, oldVersion)
  expect(sha256(await readFile(runtimeBinary))).toBe(oldSHA)
  await waitForRuntimeGeneration(generationFile, 1)

  await page.goto('/console/system')
  await page.getByRole('tab', { name: 'Data backup' }).click()
  let updatePanel = page.locator('.panel').filter({ has: page.getByRole('heading', { name: 'System Update' }) })
  const checkResponse = page.waitForResponse((response) =>
    response.request().method() === 'GET' && response.url().includes('/api/v1/console/system/check-updates')
  )
  await updatePanel.getByRole('button', { name: 'Check updates', exact: true }).click()
  expect((await checkResponse).status()).toBe(200)
  await expect(updatePanel.getByText('Update available', { exact: true })).toBeVisible()
  await expect(updatePanel.getByText('Signed catalog', { exact: true })).toBeVisible()
  await expect(updatePanel.getByText('Signed metadata', { exact: true })).toBeVisible()
  await expect(updatePanel.getByLabel('Latest version')).toHaveValue(newVersion)

  const updateResponse = page.waitForResponse((response) =>
    response.request().method() === 'POST' && response.url().endsWith('/api/v1/console/system/update')
  )
  await updatePanel.getByRole('button', { name: 'One-click update', exact: true }).click()
  const updateResult = await envelope<SystemApplyResult>(await updateResponse)
  expect(updateResult).toMatchObject({ need_restart: true, current_version: oldVersion, latest_version: newVersion })
  await expect(page.getByText('Update completed. Restart the service to run the new version.', { exact: true })).toBeVisible()
  expect(sha256(await readFile(runtimeBinary))).toBe(newSHA)
  expect(sha256(await readFile(`${runtimeBinary}.backup`))).toBe(oldSHA)

  const firstRestartResponse = page.waitForResponse((response) =>
    response.request().method() === 'POST' && response.url().endsWith('/api/v1/console/system/restart')
  )
  await updatePanel.getByRole('button', { name: 'Restart', exact: true }).click()
  const firstRestart = await envelope<SystemApplyResult>(await firstRestartResponse)
  expect(firstRestart.message).toBe('Service restart initiated.')
  await waitForRuntimeGeneration(generationFile, 2)
  await waitForSystemVersion(page, headers, newVersion)

  await page.goto('/console/system')
  await page.getByRole('tab', { name: 'Data backup' }).click()
  updatePanel = page.locator('.panel').filter({ has: page.getByRole('heading', { name: 'System Update' }) })
  const rollbackResponse = page.waitForResponse((response) =>
    response.request().method() === 'POST' && response.url().endsWith('/api/v1/console/system/rollback')
  )
  await updatePanel.getByRole('button', { name: 'Rollback', exact: true }).click()
  const rollbackResult = await envelope<SystemApplyResult>(await rollbackResponse)
  expect(rollbackResult.need_restart).toBe(true)
  await expect(page.getByText('Rollback completed. Restart the service to run the restored version.', { exact: true })).toBeVisible()
  expect(sha256(await readFile(runtimeBinary))).toBe(oldSHA)
  await expect(readFile(`${runtimeBinary}.backup`)).rejects.toMatchObject({ code: 'ENOENT' })

  const secondRestartResponse = page.waitForResponse((response) =>
    response.request().method() === 'POST' && response.url().endsWith('/api/v1/console/system/restart')
  )
  await updatePanel.getByRole('button', { name: 'Restart', exact: true }).click()
  const secondRestart = await envelope<SystemApplyResult>(await secondRestartResponse)
  expect(secondRestart.message).toBe('Service restart initiated.')
  await waitForRuntimeGeneration(generationFile, 3)
  await waitForSystemVersion(page, headers, oldVersion)

  const audit = await envelope<AuditEvent[]>(await page.request.get('/api/v1/console/audit-logs?resource_type=system&limit=200', { headers }))
  expect(audit).toEqual(expect.arrayContaining([
    expect.objectContaining({ action: 'check_update', resource_type: 'system' }),
    expect.objectContaining({ action: 'update', resource_type: 'system' }),
    expect.objectContaining({ action: 'rollback', resource_type: 'system' })
  ]))
  expect(audit.filter((event) => event.action === 'restart' && event.resource_type === 'system')).toHaveLength(2)

  const officialRequests = ((await (await page.request.get(`${officialURL}/e2e/requests`)).json()).requests || []) as Array<{ kind: string; valid: boolean }>
  expect(officialRequests).toEqual(expect.arrayContaining([
    expect.objectContaining({ kind: 'catalog', valid: true }),
    expect.objectContaining({ kind: 'core_release_object', valid: true })
  ]))
  expect(errors).toEqual([])
})

test('@e2e-system-backup-001 PostgreSQL backup survives local and S3 download and restores database state', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The destructive backup lifecycle runs once against its dedicated PostgreSQL database.')
  test.skip(process.env.ASTER_E2E_POSTGRES_AVAILABLE !== '1', 'Real backup and restore require the dedicated PostgreSQL E2E runtime.')
  test.skip(process.env.ASTER_E2E_ALLOW_DESTRUCTIVE_RESTORE !== '1', 'Destructive restore requires explicit ASTER_E2E_ALLOW_DESTRUCTIVE_RESTORE=1 opt-in.')
  const databaseName = process.env.ASTER_E2E_DATABASE_NAME || ''
  test.skip(!databaseName.split(/[^a-z0-9]+/i).some((token) => token === 'e2e' || token === 'test'), 'Destructive restore requires a database name with an isolated e2e or test token.')
  test.setTimeout(120_000)

  const errors = captureBrowserErrors(page)
  await loginDemo(page)
  const token = await loginTestPrincipal(page)
  const headers = { Authorization: `Bearer ${token}` }
  const settings = await envelope<Record<string, unknown>>(await page.request.get('/api/v1/console/settings', { headers }))
  const s3Port = process.env.ASTER_E2E_S3_PORT || '29003'
  const s3API = process.env.ASTER_E2E_S3_API_URL || 'http://127.0.0.1:29004'
  await envelope(await page.request.put('/api/v1/console/settings', {
    headers,
    data: {
      ...settings,
      backup_s3_enabled: true,
      backup_s3_endpoint: `https://127.0.0.1:${s3Port}`,
      backup_s3_region: 'auto',
      backup_s3_bucket: 'e2e-system-backups',
      backup_s3_prefix: 'system-lifecycle',
      backup_s3_access_key: 'e2e-access-key',
      backup_s3_secret_key: 'e2e-secret-key',
      backup_s3_path_style: true,
      backup_retention_days: 30,
      backup_max_retained: 10
    }
  }))

  await page.goto('/console/system')
  await page.getByRole('tab', { name: 'Data backup' }).click()
  const s3Panel = page.locator('.panel').filter({ has: page.getByRole('heading', { name: 'S3 / R2 对象存储' }) })
  const connectionResponse = page.waitForResponse((response) => response.url().endsWith('/api/v1/console/system/backups/s3/test') && response.request().method() === 'POST')
  await s3Panel.getByRole('button', { name: '测试连接' }).click()
  expect((await connectionResponse).status()).toBe(200)
  await expect(page.getByText('S3 / R2 连接成功')).toBeVisible()

  const backupPanel = page.locator('.panel').filter({ has: page.getByRole('heading', { name: 'Backup & diagnostics' }) })
  const createResponse = page.waitForResponse((response) => response.url().endsWith('/api/v1/console/system/backups') && response.request().method() === 'POST')
  await backupPanel.getByRole('button', { name: 'Create backup', exact: true }).click()
  const backup = await envelope<SystemArchiveInfo>(await createResponse)
  expect(backup.id).toMatch(/^asterrouter-backup-/)
  expect(backup.size_bytes).toBeGreaterThan(0)
  await expect(backupPanel.getByRole('row').filter({ hasText: backup.id })).toBeVisible()

  const localDownload = page.waitForEvent('download')
  await backupPanel.getByRole('row').filter({ hasText: backup.id }).getByTitle('Download').click()
  const localFile = await localDownload
  const localPath = testInfo.outputPath(`local-${backup.path}`)
  await localFile.saveAs(localPath)
  const localBytes = await readFile(localPath)
  expect(localBytes.length).toBe(backup.size_bytes)

  await page.reload()
  await page.getByRole('tab', { name: 'Data backup' }).click()
  const remotePanel = page.locator('.panel').filter({ has: page.getByRole('heading', { name: '远端备份记录' }) })
  const remoteRow = remotePanel.getByRole('row').filter({ hasText: backup.id })
  await expect(remoteRow).toContainText(`system-lifecycle/${backup.id}.tar.gz`)
  const remoteDownload = page.waitForEvent('download')
  await remoteRow.getByRole('button', { name: '下载', exact: true }).click()
  const remoteFile = await remoteDownload
  const remotePath = testInfo.outputPath(`remote-${backup.path}`)
  await remoteFile.saveAs(remotePath)
  expect(await readFile(remotePath)).toEqual(localBytes)

  const backupAudit = await envelope<AuditEvent[]>(await page.request.get(`/api/v1/console/audit-logs?action=backup&resource_type=system&limit=200`, { headers }))
  expect(backupAudit).toContainEqual(expect.objectContaining({ action: 'backup', resource_type: 'system', resource_id: backup.id }))

  const localMarker = await createApplicationMarker(page, headers, 'Local')
  expect(await applicationExists(page, headers, localMarker.id)).toBe(true)

  page.once('dialog', (dialog) => dialog.accept())
  const localRestoreResponse = page.waitForResponse((response) => response.url().endsWith('/api/v1/console/system/backups/restore') && response.request().method() === 'POST')
  await backupPanel.getByRole('row').filter({ hasText: backup.id }).getByTitle('Restore').click()
  const localRestore = await envelope<{ backup_id: string; need_restart: boolean }>(await localRestoreResponse)
  expect(localRestore).toMatchObject({ backup_id: backup.id, need_restart: true })
  expect(await applicationExists(page, headers, localMarker.id)).toBe(false)
  const restoreAudit = await envelope<AuditEvent[]>(await page.request.get('/api/v1/console/audit-logs?action=restore&resource_type=system&limit=200', { headers }))
  expect(restoreAudit).toContainEqual(expect.objectContaining({ action: 'restore', resource_type: 'system', resource_id: backup.id }))

  const remoteMarker = await createApplicationMarker(page, headers, 'S3')
  expect(await applicationExists(page, headers, remoteMarker.id)).toBe(true)

  page.once('dialog', (dialog) => dialog.accept())
  const remoteRestoreResponse = page.waitForResponse((response) => response.url().endsWith('/api/v1/console/system/backups/s3/restore') && response.request().method() === 'POST')
  await remoteRow.getByRole('button', { name: '恢复', exact: true }).click()
  const remoteRestore = await envelope<{ backup_id: string; need_restart: boolean }>(await remoteRestoreResponse)
  expect(remoteRestore).toMatchObject({ backup_id: backup.id, need_restart: true })
  expect(await applicationExists(page, headers, remoteMarker.id)).toBe(false)
  const remoteRestoreAudit = await envelope<AuditEvent[]>(await page.request.get('/api/v1/console/audit-logs?action=restore_s3&resource_type=system&limit=200', { headers }))
  expect(remoteRestoreAudit).toContainEqual(expect.objectContaining({ action: 'restore_s3', resource_type: 'system', resource_id: backup.id }))
  const s3RequestLog = await page.request.get(`${s3API}/__test/requests`)
  expect(s3RequestLog.status()).toBe(200)
  const s3Requests = ((await s3RequestLog.json()).requests || []) as S3Request[]
  expect(s3Requests.length).toBeGreaterThan(0)
  expect(s3Requests.every((request) => request.sigv4_valid && request.sigv4_errors.length === 0)).toBe(true)
  expect(s3Requests).toEqual(expect.arrayContaining([
    expect.objectContaining({ method: 'PUT', outcome: 'stored' }),
    expect.objectContaining({ method: 'GET', outcome: 'downloaded' })
  ]))
  expect(errors).toEqual([])
})
