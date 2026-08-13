import { expect, type APIResponse, type Page } from '@playwright/test'
import { isNavigationCancellationError } from '../src/testing/browser-errors'

export type Envelope<T> = { code: number; message: string; data: T }

type PricingRuleDetail = {
  rule: { id: string; lock_version: number; active_version_id?: string }
  draft?: { id: string; expression_hash: string }
}

type PricingRuleInput = {
  name: string
  purpose: 'usage_cost'
  scope_type: 'global'
  scope_id: string
  model: string
  expression: string
}

export function controlAPI(path = ''): string {
  return `/api/v1/console${path}`
}

export async function envelope<T>(response: APIResponse, expectedStatus = 200): Promise<T> {
  const body = await response.json() as Envelope<T>
  expect(response.status(), JSON.stringify(body)).toBe(expectedStatus)
  expect(body.code, JSON.stringify(body)).toBe(0)
  return body.data
}

export async function adminPost<T>(page: Page, token: string, path: string, data: unknown): Promise<T> {
  return envelope<T>(await page.request.post(controlAPI(path), {
    data,
    headers: { Authorization: `Bearer ${token}` }
  }))
}

async function pricingData<T>(response: APIResponse): Promise<T> {
  const body = await response.json() as { data?: T; error?: unknown }
  expect(response.status(), JSON.stringify(body)).toBe(200)
  expect(body.data, JSON.stringify(body)).toBeDefined()
  return body.data!
}

export async function createPublishedPricingRule(
  page: Page,
  token: string,
  input: PricingRuleInput
): Promise<PricingRuleDetail> {
  const headers = { Authorization: `Bearer ${token}` }
  const detail = await pricingData<PricingRuleDetail>(await page.request.post('/api/v1/console/pricing-rules', {
    headers,
    data: {
      ...input,
      currency: 'USD',
      authoring_mode: 'raw',
      test_cases: []
    }
  }))
  expect(detail.draft).toBeDefined()
  return pricingData<PricingRuleDetail>(await page.request.post(`/api/v1/console/pricing-rules/${detail.rule.id}/publish`, {
    headers,
    data: {
      draft_version_id: detail.draft!.id,
      expected_lock_version: detail.rule.lock_version,
      expected_active_version_id: detail.rule.active_version_id || '',
      expression_hash: detail.draft!.expression_hash
    }
  }))
}

export async function loginUser(page: Page, email: string, password: string): Promise<string> {
  const result = await envelope<{ access_token: string }>(await page.request.post('/api/v1/auth/login', {
    data: { username: email, password, agreement_accepted: true }
  }))
  return result.access_token
}

export function loginTestPrincipal(page: Page): Promise<string> {
  return loginUser(page, process.env.ASTER_E2E_USERNAME || 'demo', process.env.ASTER_E2E_PASSWORD || 'demo')
}

export async function registerUsers(
  page: Page,
  adminToken: string,
  users: Array<{ email: string; password: string; displayName: string }>
): Promise<Array<{ id: string; email: string }>> {
  const headers = { Authorization: `Bearer ${adminToken}` }
  const settings = await envelope<Record<string, unknown>>(await page.request.get(controlAPI('/settings'), { headers }))
  try {
    const registered: Array<{ id: string; email: string }> = []
    for (const user of users) {
      await envelope(await page.request.put(controlAPI('/settings'), {
        headers,
        data: {
          ...settings,
          registration_enabled: true,
          email_verify_enabled: false
        }
      }))
      const result = await envelope<{ user_id: string }>(await page.request.post('/api/v1/auth/register', {
        data: {
          email: user.email,
          password: user.password,
          display_name: user.displayName,
          agreement_accepted: true
        }
      }))
      registered.push({ id: result.user_id, email: user.email })
    }
    return registered
  } finally {
    await envelope(await page.request.put(controlAPI('/settings'), { headers, data: settings }))
  }
}

export async function createGatewayFixture(page: Page, token: string, runID: string, publicModel: string) {
  const upstreamPort = process.env.ASTER_E2E_UPSTREAM_PORT || '19000'
  const provider = await adminPost<{ id: string }>(page, token, '/providers', {
    name: `E2E Provider ${runID}`,
    type: 'openai_compatible',
    base_url: `http://127.0.0.1:${upstreamPort}/v1`,
    status: 'active',
    priority: 10
  })
  const account = await adminPost<{ id: string; secret_configured: boolean }>(page, token, '/provider-accounts', {
    provider_id: provider.id,
    name: `E2E Account ${runID}`,
    platform: 'openai_compatible',
    auth_type: 'api_key',
    status: 'active',
    schedulable: true,
    priority: 10,
    concurrency: 2,
    rate_multiplier: 1,
    models: ['upstream-model'],
    group_ids: [],
    secret: 'synthetic-account-secret'
  })
  expect(account.secret_configured).toBe(true)

  const model = await adminPost<{ id: string }>(page, token, '/gateway-models', {
    model_id: publicModel,
    name: `E2E Model ${runID}`,
    description: 'Synthetic Playwright gateway contract',
    modality: 'chat',
    default_route_group: 'default',
    status: 'active'
  })
  await adminPost(page, token, '/model-routes', {
    gateway_model_id: model.id,
    route_group: 'default',
    provider_account_id: account.id,
    upstream_model: 'upstream-model',
    upstream_format: 'openai_chat',
    priority: 10,
    weight: 100,
    status: 'active'
  })
  return account
}

export async function createDurableImageGatewayFixture(page: Page, token: string, runID: string, publicModel: string) {
  const upstreamPort = process.env.ASTER_E2E_UPSTREAM_PORT || '19000'
  const provider = await adminPost<{ id: string }>(page, token, '/providers', {
    name: `E2E Image Provider ${runID}`,
    type: 'openai_compatible',
    base_url: `http://127.0.0.1:${upstreamPort}/v1`,
    status: 'active',
    priority: 10
  })
  const account = await adminPost<{ id: string; provider_id: string; name: string; secret_configured: boolean }>(page, token, '/provider-accounts', {
    provider_id: provider.id,
    name: `E2E Image Account ${runID}`,
    platform: 'openai_compatible',
    auth_type: 'api_key',
    status: 'active',
    schedulable: true,
    priority: 10,
    concurrency: 1,
    rpm_limit: 1,
    rate_multiplier: 1,
    models: ['upstream-image-model'],
    group_ids: [],
    secret: 'synthetic-image-account-secret'
  })
  expect(account.secret_configured).toBe(true)

  const model = await adminPost<{ id: string }>(page, token, '/gateway-models', {
    model_id: publicModel,
    name: `E2E Image Model ${runID}`,
    description: 'Synthetic Playwright durable image contract',
    modality: 'image',
    default_route_group: 'default',
    status: 'active'
  })
  const route = await adminPost<{ id: string }>(page, token, '/model-routes', {
    gateway_model_id: model.id,
    route_group: 'default',
    provider_account_id: account.id,
    upstream_model: 'upstream-image-model',
    upstream_format: 'native_media',
    priority: 10,
    weight: 100,
    status: 'active'
  })
  return { account, model, route }
}

export function captureBrowserErrors(page: Page): string[] {
  const errors: string[] = []
  page.on('console', (message) => {
    if (message.type() === 'error') errors.push(`console: ${message.text()}`)
  })
  page.on('pageerror', (error) => errors.push(`pageerror: ${error.message}`))
  page.on('requestfailed', (request) => {
    const failure = request.failure()
    if (isNavigationCancellationError(failure?.errorText)) return
    errors.push(`requestfailed: ${request.method()} ${request.url()} ${failure?.errorText || ''}`.trim())
  })
  return errors
}

export async function loginDemo(page: Page): Promise<void> {
  await page.goto('/login')
  const username = process.env.ASTER_E2E_USERNAME
  const password = process.env.ASTER_E2E_PASSWORD
  if (username && password) {
    await page.getByLabel('Username').fill(username)
    await page.locator('input#password').fill(password)
    await page.getByRole('button', { name: 'Sign in' }).click()
    await expect(page).not.toHaveURL(/\/login/)
    return
  }
  const demoButton = page.getByRole('button', { name: 'Try the demo' })
  await expect(demoButton).toBeVisible()
  await demoButton.click()
  await expect(page).toHaveURL(/\/console\/workbench$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Overview' })).toBeVisible()
}

export async function expectNoHorizontalOverflow(page: Page): Promise<void> {
  const dimensions = await page.evaluate(() => ({
    body: document.body.scrollWidth,
    document: document.documentElement.scrollWidth,
    viewport: document.documentElement.clientWidth
  }))
  expect(Math.max(dimensions.body, dimensions.document)).toBeLessThanOrEqual(dimensions.viewport + 1)
}
