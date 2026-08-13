import { readFile } from 'node:fs/promises'
import { expect, test, type Download, type Page } from '@playwright/test'
import {
  adminPost,
  captureBrowserErrors,
  createGatewayFixture,
  loginDemo,
  loginTestPrincipal
} from './fixtures'

function uniqueID(name: string, project: string): string {
  return `${name}-${project}-${Date.now()}`.replace(/[^a-z0-9-]+/gi, '-').toLowerCase()
}

function parseCSV(input: string): string[][] {
  const rows: string[][] = []
  let row: string[] = []
  let field = ''
  let quoted = false
  for (let index = 0; index < input.length; index += 1) {
    const char = input[index]
    if (quoted) {
      if (char === '"' && input[index + 1] === '"') {
        field += '"'
        index += 1
      } else if (char === '"') {
        quoted = false
      } else {
        field += char
      }
      continue
    }
    if (char === '"') {
      quoted = true
    } else if (char === ',') {
      row.push(field)
      field = ''
    } else if (char === '\n') {
      row.push(field.replace(/\r$/, ''))
      rows.push(row)
      row = []
      field = ''
    } else {
      field += char
    }
  }
  if (quoted) throw new Error('unterminated quoted CSV field')
  if (field || row.length) {
    row.push(field.replace(/\r$/, ''))
    rows.push(row)
  }
  return rows
}

function asRecord(rows: string[][]): Record<string, string> {
  expect(rows).toHaveLength(2)
  expect(rows[1]).toHaveLength(rows[0].length)
  return Object.fromEntries(rows[0].map((header, index) => [header, rows[1][index]]))
}

async function exportedCSV(
  page: Page,
  endpoint: string,
  suggestedFilename: RegExp,
  trigger: () => Promise<void>
): Promise<Record<string, string>> {
  const responsePromise = page.waitForResponse((response) =>
    response.request().method() === 'GET' && response.url().includes(endpoint)
  )
  const downloadPromise = page.waitForEvent('download')
  await trigger()
  const [response, download] = await Promise.all([responsePromise, downloadPromise])
  expect(response.status()).toBe(200)
  expect(response.headers()['content-type']).toMatch(/^text\/csv/)
  expect(response.headers()['content-disposition']).toMatch(/^attachment; filename="[^"]+\.csv"$/)
  expect(download.suggestedFilename()).toMatch(suggestedFilename)
  return asRecord(parseCSV(await downloadText(download)))
}

async function downloadText(download: Download): Promise<string> {
  const path = await download.path()
  expect(path).toBeTruthy()
  return readFile(path!, 'utf8')
}

test('@e2e-record-csv-exports-001 filtered operational records export exact CSV rows', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The stateful export workflow runs once.')
  test.setTimeout(90_000)

  const errors = captureBrowserErrors(page)
  const runID = uniqueID('record-export', testInfo.project.name)
  const publicModel = `browser-export-model-${runID}`
  await loginDemo(page)
  const token = await loginTestPrincipal(page)
  const account = await createGatewayFixture(page, token, runID, publicModel)
  const workspaceKey = await adminPost<{ key: string; record: { id: string } }>(page, token, '/api-keys', {
    name: `Record Export Key ${runID}`,
    model_allowlist: [publicModel],
    qps_limit: 10,
    monthly_token_limit: 100000
  })

  const completion = await page.request.post('/v1/chat/completions', {
    headers: { Authorization: `Bearer ${workspaceKey.key}` },
    data: { model: publicModel, messages: [{ role: 'user', content: `record export ${runID}` }] }
  })
  expect(completion.status()).toBe(200)
  await expect(completion.json()).resolves.toMatchObject({
    choices: [{ message: { content: 'e2e-ok' } }],
    usage: { prompt_tokens: 7, completion_tokens: 11 }
  })

  await page.goto('/console/usage')
  await page.getByRole('button', { name: 'Records', exact: true }).click()
  const usageFilters = page.locator('[data-section="usage-filters"]')
  await usageFilters.getByLabel('Model').selectOption(publicModel)
  await expect(page.getByRole('table')).toContainText(publicModel)
  const usage = await exportedCSV(page, '/console/usage/export?', /^usage-\d+\.csv$/, () =>
    page.locator('.page-header').getByRole('button', { name: 'Export' }).click()
  )
  expect(usage).toEqual(expect.objectContaining({
    api_key_id: workspaceKey.record.id,
    model: publicModel,
    upstream_model: 'upstream-model',
    provider_account_id: account.id,
    status: 'forwarded',
    input_tokens: '7',
    output_tokens: '11'
  }))

  await page.goto('/console/usage/cost-allocation')
  await page.getByRole('button', { name: 'By model', exact: true }).click()
  const costToolbar = page.locator('.table-toolbar')
  await costToolbar.getByLabel('Model').fill(publicModel)
  await costToolbar.getByRole('button', { name: 'Apply' }).click()
  await expect(page.getByRole('table')).toContainText(publicModel)
  const cost = await exportedCSV(page, '/console/cost-allocation/export?', /^cost-allocation-\d+\.csv$/, () =>
    page.locator('.page-header').getByRole('button', { name: 'Export' }).click()
  )
  expect(cost).toEqual(expect.objectContaining({
    dimension: 'model',
    resource_id: publicModel,
    resource_name: publicModel,
    model: publicModel,
    requests: '1',
    error_requests: '0',
    total_tokens: '18'
  }))

  await page.goto('/console/usage/traces')
  const traceToolbar = page.locator('.table-toolbar')
  await traceToolbar.locator('select').first().selectOption(publicModel)
  await expect(page.getByRole('table')).toContainText(publicModel)
  const trace = await exportedCSV(page, '/console/gateway-traces/export?', /^gateway-traces-\d+\.csv$/, () =>
    page.locator('.page-header').getByRole('button', { name: 'Export' }).click()
  )
  expect(trace).toEqual(expect.objectContaining({
    api_key_id: workspaceKey.record.id,
    model: publicModel,
    provider_account_id: account.id,
    upstream_model: 'upstream-model',
    status: 'forwarded',
    http_status: '200',
    input_tokens: '7',
    output_tokens: '11'
  }))

  await page.goto('/console/system/audit')
  const auditToolbar = page.locator('.table-toolbar')
  await auditToolbar.locator('input[placeholder="Search actor, action, resource, or summary"]').fill(workspaceKey.record.id)
  await auditToolbar.getByRole('button', { name: 'Apply' }).click()
  await auditToolbar.locator('select').first().selectOption('invoke')
  await expect(page.getByRole('table')).toContainText(workspaceKey.record.id)
  const audit = await exportedCSV(page, '/console/audit-logs/export?', /^audit-\d+\.csv$/, () =>
    page.locator('.page-header').getByRole('button', { name: 'Export' }).click()
  )
  expect(audit).toEqual(expect.objectContaining({
    action: 'invoke',
    resource_type: 'gateway_call'
  }))
  expect(audit.summary).toContain(`workspace_key=${workspaceKey.record.id}`)
  expect(audit.summary).toContain('status=forwarded')
  expect(errors).toEqual([])
})
