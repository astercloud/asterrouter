import assert from 'node:assert/strict'
import { spawn, spawnSync } from 'node:child_process'
import { existsSync, mkdtempSync, readFileSync } from 'node:fs'
import http from 'node:http'
import https from 'node:https'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'

function listen(server) {
  return new Promise((resolve, reject) => {
    server.once('error', reject)
    server.listen(0, '127.0.0.1', () => resolve(server.address().port))
  })
}

function close(server) {
  return new Promise((resolve) => server.close(resolve))
}

function request(port, agent) {
  return new Promise((resolve, reject) => {
    const req = https.get({
      hostname: '127.0.0.1',
      port,
      path: '/reload-probe',
      agent
    }, (res) => {
      const chunks = []
      res.on('data', (chunk) => chunks.push(chunk))
      res.on('end', () => resolve({
        status: res.statusCode,
        headers: res.headers,
        body: Buffer.concat(chunks).toString('utf8')
      }))
    })
    req.on('error', reject)
  })
}

async function waitUntilReady(port, child, agent) {
  for (let attempt = 0; attempt < 100; attempt += 1) {
    if (child.exitCode !== null) throw new Error(`fake OIDC proxy exited with ${child.exitCode}`)
    try {
      const response = await request(port, agent)
      if (response.status === 200) return
    } catch {
      // The isolated HTTPS listener is still starting.
    }
    await new Promise((resolve) => setTimeout(resolve, 25))
  }
  throw new Error('fake OIDC proxy did not become ready')
}

async function waitForReadyFile(path, child) {
  for (let attempt = 0; attempt < 100; attempt += 1) {
    if (child.exitCode !== null) throw new Error(`fake OIDC proxy exited with ${child.exitCode}`)
    if (existsSync(path)) return
    await new Promise((resolve) => setTimeout(resolve, 25))
  }
  throw new Error('fake OIDC proxy did not publish its ready file')
}

test('fake OIDC proxy keeps downstream TLS reusable when upstream closes connections', async (t) => {
  const openssl = spawnSync('openssl', ['version'], { stdio: 'ignore' })
  if (openssl.status !== 0) return t.skip('OpenSSL is required for the isolated TLS proxy test')

  const upstream = http.createServer((_req, res) => {
    res.writeHead(200, {
      'content-type': 'text/plain',
      connection: 'close',
      'x-upstream-hop': 'preserved'
    })
    res.end('ready')
  })
  const frontendPort = await listen(upstream)
  const probe = http.createServer()
  const proxyPort = await listen(probe)
  await close(probe)

  const root = mkdtempSync(join(tmpdir(), 'asterrouter-fake-oidc-'))
  const keyPath = join(root, 'key.pem')
  const certPath = join(root, 'cert.pem')
  const readyPath = join(root, 'ready')
  const certificate = spawnSync('openssl', [
    'req', '-x509', '-newkey', 'rsa:2048', '-sha256', '-nodes', '-days', '1',
    '-subj', '/CN=127.0.0.1', '-addext', 'subjectAltName=IP:127.0.0.1',
    '-keyout', keyPath, '-out', certPath
  ], { stdio: 'ignore' })
  assert.equal(certificate.status, 0)

  const child = spawn(process.execPath, ['scripts/fake-oidc.mjs'], {
    cwd: new URL('..', import.meta.url),
    env: {
      ...process.env,
      ASTER_E2E_OIDC_PORT: String(proxyPort),
      ASTER_DEV_FRONTEND_PORT: String(frontendPort),
      ASTER_E2E_OIDC_KEY: keyPath,
      ASTER_E2E_OIDC_CERT: certPath,
      ASTER_E2E_OIDC_READY_FILE: readyPath
    },
    stdio: 'ignore'
  })
  const agent = new https.Agent({ keepAlive: true, maxSockets: 2, ca: readFileSync(certPath) })

  try {
    await waitForReadyFile(readyPath, child)
    assert.equal(readFileSync(readyPath, 'utf8'), `https://127.0.0.1:${proxyPort}/oidc\n`)
    await waitUntilReady(proxyPort, child, agent)
    for (let attempt = 0; attempt < 30; attempt += 1) {
      const response = await request(proxyPort, agent)
      assert.equal(response.status, 200)
      assert.equal(response.body, 'ready')
      assert.equal(response.headers['x-upstream-hop'], 'preserved')
      assert.notEqual(response.headers.connection, 'close')
    }
  } finally {
    agent.destroy()
    if (child.exitCode === null) {
      child.kill('SIGTERM')
      await new Promise((resolve) => child.once('exit', resolve))
    }
    await close(upstream)
  }
})
