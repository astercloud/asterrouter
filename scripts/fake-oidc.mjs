import { createHash, generateKeyPairSync, randomBytes, sign } from 'node:crypto'
import { readFileSync, writeFileSync } from 'node:fs'
import { Buffer } from 'node:buffer'
import https from 'node:https'
import http from 'node:http'
import net from 'node:net'
import { URL } from 'node:url'

const port = Number(process.env.ASTER_E2E_OIDC_PORT || 29005)
const frontendPort = Number(process.env.ASTER_DEV_FRONTEND_PORT || 5173)
const keyPath = process.env.ASTER_E2E_OIDC_KEY
const certPath = process.env.ASTER_E2E_OIDC_CERT
const readyFile = process.env.ASTER_E2E_OIDC_READY_FILE
const issuer = `https://127.0.0.1:${port}/oidc`
const clientID = process.env.ASTER_E2E_OIDC_CLIENT_ID || 'asterrouter-e2e'
const clientSecret = process.env.ASTER_E2E_OIDC_CLIENT_SECRET || 'asterrouter-e2e-secret'
const identity = {
  sub: process.env.ASTER_E2E_OIDC_SUBJECT || 'fake-oidc-subject-1',
  email: process.env.ASTER_E2E_OIDC_EMAIL || 'e2e-oidc@example.test',
  name: process.env.ASTER_E2E_OIDC_NAME || 'E2E OIDC User',
  department: 'E2E QA',
  email_verified: true
}

if (!keyPath || !certPath) throw new Error('ASTER_E2E_OIDC_KEY and ASTER_E2E_OIDC_CERT are required')
const { privateKey, publicKey } = generateKeyPairSync('rsa', { modulusLength: 2048 })
const keyID = 'e2e-oidc-key-1'
const publicJWK = publicKey.export({ format: 'jwk' })
const pendingCodes = new Map()
const frontendAgent = new http.Agent({ keepAlive: true })
const hopByHopHeaders = new Set([
  'connection',
  'keep-alive',
  'proxy-authenticate',
  'proxy-authorization',
  'te',
  'trailer',
  'transfer-encoding',
  'upgrade'
])

function proxyHeaders(headers) {
  const connectionTokens = String(headers.connection || '')
    .split(',')
    .map((value) => value.trim().toLowerCase())
    .filter(Boolean)
  const excluded = new Set([...hopByHopHeaders, ...connectionTokens])
  return Object.fromEntries(Object.entries(headers).filter(([name]) => !excluded.has(name.toLowerCase())))
}

function json(res, status, value) {
  const body = JSON.stringify(value)
  res.writeHead(status, { 'content-type': 'application/json', 'cache-control': 'no-store', 'content-length': Buffer.byteLength(body) })
  res.end(body)
}

function html(res, body) {
  res.writeHead(200, { 'content-type': 'text/html; charset=utf-8', 'cache-control': 'no-store' })
  res.end(`<!doctype html><title>Fake OIDC</title><main><h1>Fake OIDC authorization</h1>${body}</main>`)
}

function signedIDToken(nonce) {
  const now = Math.floor(Date.now() / 1000)
  const header = Buffer.from(JSON.stringify({ alg: 'RS256', kid: keyID, typ: 'JWT' })).toString('base64url')
  const payload = Buffer.from(JSON.stringify({ ...identity, iss: issuer, aud: clientID, iat: now, exp: now + 600, nonce })).toString('base64url')
  const input = `${header}.${payload}`
  const signature = sign('RSA-SHA256', Buffer.from(input), privateKey).toString('base64url')
  return `${input}.${signature}`
}

function handleOIDC(req, res) {
  const url = new URL(req.url, issuer)
  if (url.pathname === '/oidc/.well-known/openid-configuration') {
    return json(res, 200, { issuer, authorization_endpoint: `${issuer}/authorize`, token_endpoint: `${issuer}/token`, jwks_uri: `${issuer}/.well-known/jwks.json`, response_types_supported: ['code'], subject_types_supported: ['public'], id_token_signing_alg_values_supported: ['RS256'], scopes_supported: ['openid', 'profile', 'email'], claims_supported: ['sub', 'email', 'email_verified', 'name', 'department'] })
  }
  if (url.pathname === '/oidc/.well-known/jwks.json') {
    return json(res, 200, { keys: [{ ...publicJWK, kid: keyID, use: 'sig', alg: 'RS256' }] })
  }
  if (url.pathname === '/oidc/authorize') {
    const params = Object.fromEntries(url.searchParams)
    if (!params.client_id || params.client_id !== clientID || !params.redirect_uri || !params.state || !params.code_challenge || params.code_challenge_method !== 'S256') return json(res, 400, { error: 'invalid_request' })
    const action = `/oidc/approve?${new URLSearchParams(params).toString()}`
    return html(res, `<p>Test identity: ${identity.email}</p><form method="post" action="${action}"><button type="submit">Continue</button></form>`)
  }
  if (url.pathname === '/oidc/approve' && req.method === 'POST') {
    const params = Object.fromEntries(url.searchParams)
    const code = randomBytes(24).toString('base64url')
    pendingCodes.set(code, { ...params, createdAt: Date.now() })
    const callback = new URL(params.redirect_uri)
    callback.searchParams.set('code', code)
    callback.searchParams.set('state', params.state)
    res.writeHead(302, { location: callback.toString() })
    return res.end()
  }
  if (url.pathname === '/oidc/token' && req.method === 'POST') {
    let raw = ''
    req.setEncoding('utf8')
    req.on('data', (chunk) => { raw += chunk })
    req.on('end', () => {
      const params = new URLSearchParams(raw)
      const entry = pendingCodes.get(params.get('code') || '')
      pendingCodes.delete(params.get('code') || '')
      const basic = (req.headers.authorization || '').startsWith('Basic ')
        ? Buffer.from(req.headers.authorization.slice(6), 'base64').toString('utf8').split(':', 2)
        : []
      const requestClientID = params.get('client_id') || basic[0]
      const requestClientSecret = params.get('client_secret') || basic[1]
      if (!entry || entry.client_id !== requestClientID || requestClientSecret !== clientSecret || params.get('redirect_uri') !== entry.redirect_uri || params.get('grant_type') !== 'authorization_code') return json(res, 400, { error: 'invalid_grant' })
      const verifierChallenge = createHash('sha256').update(params.get('code_verifier') || '').digest('base64url')
      if (verifierChallenge !== entry.code_challenge) return json(res, 400, { error: 'invalid_grant' })
      return json(res, 200, { access_token: randomBytes(24).toString('base64url'), token_type: 'Bearer', expires_in: 600, id_token: signedIDToken(entry.nonce) })
    })
    return
  }
  json(res, 404, { error: 'not_found' })
}

function proxyToFrontend(req, res) {
  const proxy = http.request({
    hostname: '127.0.0.1',
    port: frontendPort,
    method: req.method,
    path: req.url,
    agent: frontendAgent,
    headers: {
      ...proxyHeaders(req.headers),
      'x-forwarded-proto': 'https',
      'x-forwarded-host': req.headers.host || ''
    }
  }, (upstream) => {
    res.writeHead(upstream.statusCode || 502, proxyHeaders(upstream.headers))
    upstream.pipe(res)
  })
  proxy.on('error', (error) => { if (!res.headersSent) res.writeHead(502); res.end(String(error)) })
  req.pipe(proxy)
}

const server = https.createServer({ key: readFileSync(keyPath), cert: readFileSync(certPath) }, (req, res) => {
  if (req.url?.startsWith('/oidc/')) return handleOIDC(req, res)
  proxyToFrontend(req, res)
})

server.on('upgrade', (req, socket, head) => {
  const upstream = net.connect(frontendPort, '127.0.0.1', () => {
    const headers = { ...req.headers, 'x-forwarded-proto': 'https', 'x-forwarded-host': req.headers.host || '' }
    upstream.write(`${req.method} ${req.url} HTTP/${req.httpVersion}\r\n`)
    for (const [name, value] of Object.entries(headers)) upstream.write(`${name}: ${value}\r\n`)
    upstream.write('\r\n')
    if (head.length) upstream.write(head)
    socket.pipe(upstream).pipe(socket)
  })
  socket.on('error', () => upstream.destroy())
  socket.on('close', () => upstream.destroy())
  upstream.on('error', () => socket.destroy())
  upstream.on('close', () => socket.destroy())
})

server.on('close', () => frontendAgent.destroy())

server.listen(port, '127.0.0.1', () => {
  if (readyFile) writeFileSync(readyFile, `${issuer}\n`, { mode: 0o600 })
  console.log(`Fake OIDC HTTPS proxy listening on ${issuer}`)
})
