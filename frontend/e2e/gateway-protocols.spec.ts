import { expect, test, type APIRequestContext, type APIResponse } from '@playwright/test'
import { adminPost, createGatewayFixture, envelope, loginDemo, loginTestPrincipal } from './fixtures'

type ProtocolCase = {
  name: string
  protocol: string
  path: (model: string, stream: boolean) => string
  headers: (key?: string) => Record<string, string>
  body: (model: string, stream: boolean) => Record<string, unknown>
  assertJSON: (body: Record<string, any>) => void
  streamMarker: string
  unauthorizedMarker: string
  forbiddenMarker: string
  rateLimitMarker: string
}

const protocols: ProtocolCase[] = [
  {
    name: 'OpenAI Responses',
    protocol: 'openai_responses',
    path: () => '/v1/responses',
    headers: (key) => key ? { Authorization: `Bearer ${key}` } : {},
    body: (model, stream) => ({ model, input: 'synthetic protocol request', stream }),
    assertJSON: (body) => {
      expect(body).toMatchObject({ object: 'response', status: 'completed', usage: { input_tokens: 7, output_tokens: 11 } })
      expect(body.output?.[0]?.content?.[0]?.text).toBe('e2e-ok')
    },
    streamMarker: 'event: response.output_text.delta',
    unauthorizedMarker: 'invalid_api_key',
    forbiddenMarker: 'model_not_allowed',
    rateLimitMarker: 'rate_limit_error'
  },
  {
    name: 'Anthropic Messages',
    protocol: 'anthropic_messages',
    path: () => '/v1/messages',
    headers: (key) => key ? { 'X-API-Key': key } : {},
    body: (model, stream) => ({ model, max_tokens: 32, messages: [{ role: 'user', content: 'synthetic protocol request' }], stream }),
    assertJSON: (body) => {
      expect(body).toMatchObject({ type: 'message', usage: { input_tokens: 7, output_tokens: 11 } })
      expect(body.content?.[0]?.text).toBe('e2e-ok')
    },
    streamMarker: 'event: content_block_delta',
    unauthorizedMarker: 'authentication_error',
    forbiddenMarker: 'permission_error',
    rateLimitMarker: 'rate_limit_error'
  },
  {
    name: 'Gemini GenerateContent',
    protocol: 'gemini_generate_content',
    path: (model, stream) => `/v1beta/models/${model}:${stream ? 'streamGenerateContent' : 'generateContent'}`,
    headers: (key) => key ? { 'X-Goog-API-Key': key } : {},
    body: () => ({ contents: [{ role: 'user', parts: [{ text: 'synthetic protocol request' }] }] }),
    assertJSON: (body) => {
      expect(body.usageMetadata).toMatchObject({ promptTokenCount: 7, candidatesTokenCount: 11 })
      expect(body.candidates?.[0]?.content?.parts?.[0]?.text).toBe('e2e-ok')
    },
    streamMarker: '"candidates"',
    unauthorizedMarker: 'UNAUTHENTICATED',
    forbiddenMarker: 'PERMISSION_DENIED',
    rateLimitMarker: 'RESOURCE_EXHAUSTED'
  }
]

async function invoke(
  request: APIRequestContext,
  protocol: ProtocolCase,
  model: string,
  key: string | undefined,
  stream: boolean,
  idempotencyKey: string
): Promise<APIResponse> {
  return request.post(protocol.path(model, stream), {
    data: protocol.body(model, stream),
    headers: { ...protocol.headers(key), 'Idempotency-Key': idempotencyKey }
  })
}

async function setUpstreamMode(request: APIRequestContext, mode: string): Promise<void> {
  const upstreamPort = process.env.ASTER_E2E_UPSTREAM_PORT || '19000'
  const response = await request.post(`http://127.0.0.1:${upstreamPort}/__test/mode`, { data: { mode } })
  expect(response.status()).toBe(200)
}

test('@e2e-gateway-protocols-001 native text protocols cross auth routing translation usage and error boundaries', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The API protocol matrix is viewport-independent and runs once on desktop.')

  await loginDemo(page)
  const token = await loginTestPrincipal(page)
  const runID = `${testInfo.project.name}-${Date.now()}`
  const fixtures = new Map<string, { model: string; accountID: string }>()
  for (const protocol of protocols) {
    const model = `e2e-${protocol.protocol.replaceAll('_', '-')}-${runID}`
    const account = await createGatewayFixture(page, token, `${runID}-${protocol.protocol}`, model)
    fixtures.set(protocol.protocol, { model, accountID: account.id })
  }
  const workspaceKey = await adminPost<{ key: string; record: { id: string } }>(page, token, '/api-keys', {
    name: `E2E Protocol Key ${runID}`,
    model_allowlist: [...fixtures.values()].map((fixture) => fixture.model),
    qps_limit: 100,
    monthly_token_limit: 100000
  })

  await setUpstreamMode(page.request, 'normal')
  try {
    for (const protocol of protocols) {
      const fixture = fixtures.get(protocol.protocol)!
      const jsonResponse = await invoke(page.request, protocol, fixture.model, workspaceKey.key, false, `${runID}-${protocol.protocol}-json`)
      expect(jsonResponse.status(), `${protocol.name} JSON: ${await jsonResponse.text()}`).toBe(200)
      protocol.assertJSON(await jsonResponse.json())

      const streamResponse = await invoke(page.request, protocol, fixture.model, workspaceKey.key, true, `${runID}-${protocol.protocol}-stream`)
      expect(streamResponse.status(), `${protocol.name} stream: ${await streamResponse.text()}`).toBe(200)
      expect(streamResponse.headers()['content-type']).toContain('text/event-stream')
      const streamBody = await streamResponse.text()
      expect(streamBody).toContain(protocol.streamMarker)
      expect(streamBody).toContain('hello')

      const unauthorized = await invoke(page.request, protocol, fixture.model, undefined, false, `${runID}-${protocol.protocol}-unauthorized`)
      expect(unauthorized.status()).toBe(401)
      expect(await unauthorized.text()).toContain(protocol.unauthorizedMarker)

      const forbidden = await invoke(page.request, protocol, `${fixture.model}-denied`, workspaceKey.key, false, `${runID}-${protocol.protocol}-forbidden`)
      expect(forbidden.status()).toBe(403)
      expect(await forbidden.text()).toContain(protocol.forbiddenMarker)
    }

    await setUpstreamMode(page.request, '429')
    for (const protocol of protocols) {
      const fixture = fixtures.get(protocol.protocol)!
      const limited = await invoke(page.request, protocol, fixture.model, workspaceKey.key, false, `${runID}-${protocol.protocol}-limited`)
      expect(limited.status()).toBe(429)
      const body = await limited.text()
      expect(body).toContain(protocol.rateLimitMarker)
      expect(body).toContain('synthetic rate limit')
    }
  } finally {
    await setUpstreamMode(page.request, 'normal')
  }

  const usage = await envelope<{ recent: Array<Record<string, unknown>> }>(await page.request.get('/api/v1/console/usage?limit=100', {
    headers: { Authorization: `Bearer ${token}` }
  }))
  for (const protocol of protocols) {
    const fixture = fixtures.get(protocol.protocol)!
    const records = usage.recent.filter((item) =>
      item.api_key_id === workspaceKey.record.id &&
      item.model === fixture.model &&
      item.protocol === protocol.protocol
    )
    expect(records.filter((item) => item.status === 'forwarded'), `${protocol.name} successful usage`).toHaveLength(2)
    expect(records).toContainEqual(expect.objectContaining({
      protocol: protocol.protocol,
      provider_account_id: fixture.accountID,
      upstream_model: 'upstream-model',
      status: 'upstream_error',
      error_type: 'upstream_status'
    }))
  }

  const traces = await envelope<Array<Record<string, unknown>>>(await page.request.get('/api/v1/console/gateway-traces?limit=100', {
    headers: { Authorization: `Bearer ${token}` }
  }))
  const protocolTraces = traces.filter((item) => item.api_key_id === workspaceKey.record.id)
  expect(protocolTraces.filter((item) => item.status === 'forwarded' && item.http_status === 200)).toHaveLength(6)
  expect(protocolTraces.filter((item) => item.status === 'upstream_error' && item.http_status === 429 && item.error_type === 'upstream_status')).toHaveLength(3)

  const audit = await envelope<Array<Record<string, unknown>>>(await page.request.get('/api/v1/console/audit-logs?limit=100', {
    headers: { Authorization: `Bearer ${token}` }
  }))
  expect(audit).toContainEqual(expect.objectContaining({ action: 'invoke', resource_type: 'gateway_call' }))
})
