import { createServer } from 'node:http'

const port = Number(process.env.ASTER_E2E_UPSTREAM_PORT || 29000)
let mode = 'normal'
let imageRequests = 0
let cacheProbeRequests = 0
let completionRequests = 0

const syntheticPNG = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII='

function json(response, status, body) {
  response.writeHead(status, { 'Content-Type': 'application/json' })
  response.end(JSON.stringify(body))
}

function syntheticUsage(value) {
  const marker = typeof value === 'string' ? value.match(/synthetic\s+(\d+)-token\s+policy\s+request/i) : null
  const promptTokens = marker ? Number(marker[1]) : Number(value?.prompt_tokens)
  const completionTokens = marker ? 0 : Number(value?.completion_tokens)
  if (!Number.isSafeInteger(promptTokens) || promptTokens < 0 || !Number.isSafeInteger(completionTokens) || completionTokens < 0) {
    return { prompt_tokens: 7, completion_tokens: 11 }
  }
  return { prompt_tokens: promptTokens, completion_tokens: completionTokens }
}

async function readJSON(request) {
  const chunks = []
  for await (const chunk of request) chunks.push(chunk)
  return JSON.parse(Buffer.concat(chunks).toString('utf8') || '{}')
}

function completeImageResponse(response) {
  response.setHeader('X-Request-ID', `e2e-image-${imageRequests}`)
  json(response, 200, { created: 1_700_000_000, data: [{ b64_json: syntheticPNG }] })
}

const server = createServer(async (request, response) => {
  try {
    if (request.method === 'GET' && request.url === '/health') {
      json(response, 200, { status: 'ok', mode })
      return
    }
    if (request.method === 'POST' && request.url === '/__test/mode') {
      const payload = await readJSON(request)
      mode = String(payload.mode || 'normal')
      json(response, 200, { mode })
      return
    }
    if (request.method === 'GET' && request.url === '/v1/models') {
      json(response, 200, { object: 'list', data: [{ id: 'upstream-model', object: 'model' }] })
      return
    }
    if (request.method === 'POST' && request.url === '/v1/chat/completions') {
      const payload = await readJSON(request)
      const probePhase = payload.messages?.find((message) => message?.role === 'user')?.content
      if (['warm cache seed', 'reuse cache verification', 'negative control'].includes(probePhase)) {
        cacheProbeRequests += 1
        const cachedTokens = probePhase === 'reuse cache verification' ? 240 : 0
        response.setHeader('X-Request-ID', `e2e-cache-probe-${cacheProbeRequests}`)
        json(response, 200, {
          id: `e2e-cache-probe-${cacheProbeRequests}`,
          object: 'chat.completion',
          choices: [{ index: 0, message: { role: 'assistant', content: 'ok' }, finish_reason: 'stop' }],
          usage: {
            prompt_tokens: 256,
            completion_tokens: 1,
            total_tokens: 257,
            prompt_tokens_details: { cached_tokens: cachedTokens }
          }
        })
        return
      }
      if (payload.model === 'fail-model') {
        json(response, 500, { error: { type: 'upstream_error', message: 'synthetic route failure' } })
        return
      }
      if (mode === '429') {
        json(response, 429, { error: { type: 'rate_limit_error', message: 'synthetic rate limit' } })
        return
      }
      if (mode === '500') {
        json(response, 500, { error: { type: 'upstream_error', message: 'synthetic upstream failure' } })
        return
      }
      if (payload.stream) {
        response.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' })
        response.write('data: {"id":"e2e-stream","choices":[{"delta":{"content":"hello"}}]}\n\n')
        response.write('data: {"id":"e2e-stream","choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":11,"total_tokens":18}}\n\n')
        response.end('data: [DONE]\n\n')
        return
      }
      completionRequests += 1
      response.setHeader('X-Request-ID', `e2e-completion-${completionRequests}`)
      json(response, 200, {
        id: 'e2e-completion',
        object: 'chat.completion',
        choices: [{ index: 0, message: { role: 'assistant', content: 'e2e-ok' }, finish_reason: 'stop' }],
        usage: syntheticUsage(payload.messages?.[0]?.content)
      })
      return
    }
    if (request.method === 'POST' && request.url === '/v1/responses') {
      const payload = await readJSON(request)
      if (mode === '429') {
        json(response, 429, { error: { type: 'rate_limit_error', message: 'synthetic rate limit' } })
        return
      }
      if (mode === '500') {
        json(response, 500, { error: { type: 'upstream_error', message: 'synthetic upstream failure' } })
        return
      }
      json(response, 200, {
        id: 'e2e-response',
        object: 'response',
        status: 'completed',
        model: payload.model || 'upstream-model',
        output: [{ type: 'message', role: 'assistant', content: [{ type: 'output_text', text: 'e2e-ok' }] }],
        usage: { input_tokens: 7, output_tokens: 11, total_tokens: 18 }
      })
      return
    }
    if (request.method === 'POST' && request.url === '/v1/images/generations') {
      const payload = await readJSON(request)
      imageRequests += 1
      if (!String(payload.prompt || '').trim() || payload.model !== 'upstream-image-model') {
        json(response, 400, { error: { type: 'invalid_request', message: 'invalid synthetic image payload' } })
        return
      }
      if (mode === '500') {
        json(response, 500, { error: { type: 'upstream_error', message: 'synthetic image failure' } })
        return
      }
      completeImageResponse(response)
      return
    }
    json(response, 404, { error: { type: 'not_found', message: 'not found' } })
  } catch (error) {
    json(response, 400, { error: { type: 'invalid_request', message: error instanceof Error ? error.message : 'invalid request' } })
  }
})

server.listen(port, '127.0.0.1', () => {
  console.log(`Fake OpenAI: http://127.0.0.1:${port}/v1`)
})

for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => server.close(() => process.exit(0)))
}
