import { createCipheriv, createHash, createPrivateKey, createPublicKey, diffieHellman, generateKeyPairSync, hkdfSync, randomBytes, sign } from 'node:crypto'
import { readFileSync, writeFileSync } from 'node:fs'
import http from 'node:http'
import { gzipSync } from 'node:zlib'

const port = Number(process.env.ASTER_E2E_OFFICIAL_PORT || '29006')
const keyFile = process.env.ASTER_E2E_OFFICIAL_KEY_FILE
const keyID = 'aster-e2e-key-v1'
const pluginID = 'com.astercloud.catalog.router-sync'
const pluginSlug = 'router-sync'
const packageID = `pkg_router_sync_${process.platform}_${process.arch}`
const importPackageID = `pkg_router_sync_import_${process.platform}_${process.arch}`
const instanceID = 'inst_e2e_browser'
const fingerprint = 'sha256:e2e-browser-fingerprint'
const licenseID = 'lic_e2e_browser'
const serviceKey = 'provider-intelligence'
const coreReleaseVersion = process.env.ASTER_E2E_OFFICIAL_CORE_RELEASE_VERSION || '0.99.0'
if (!/^[0-9]+\.[0-9]+\.[0-9]+(?:[-.+][0-9A-Za-z.-]+)?$/.test(coreReleaseVersion)) {
  throw new Error('ASTER_E2E_OFFICIAL_CORE_RELEASE_VERSION must be a semantic version')
}
const coreRelease = process.env.ASTER_E2E_OFFICIAL_CORE_RELEASE_FILE
  ? readFileSync(process.env.ASTER_E2E_OFFICIAL_CORE_RELEASE_FILE)
  : Buffer.from(`asterrouter ${coreReleaseVersion} synthetic release artifact\n`)
const coreReleaseSHA = createHash('sha256').update(coreRelease).digest('hex')
const { publicKey, privateKey } = generateKeyPairSync('ed25519')
const publicKeyRaw = publicKey.export({ format: 'der', type: 'spki' }).subarray(-32)
const officialRequests = []

if (!keyFile) throw new Error('ASTER_E2E_OFFICIAL_KEY_FILE is required')
writeFileSync(keyFile, publicKeyRaw.toString('base64'), { mode: 0o600 })

function canonicalize(value) {
  if (value === null || typeof value !== 'object') return JSON.stringify(value)
  if (Array.isArray(value)) return `[${value.map(canonicalize).join(',')}]`
  return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${canonicalize(value[key])}`).join(',')}}`
}

function signedEnvelope(purpose, payload, lifetimeMs = 60 * 60 * 1000) {
  const issuedAt = new Date()
  const envelope = {
    schema_version: 'astercloud.signed-envelope.v1',
    purpose,
    key_id: keyID,
    algorithm: 'Ed25519',
    issued_at: issuedAt.toISOString(),
    expires_at: new Date(issuedAt.getTime() + lifetimeMs).toISOString(),
    payload
  }
  return { ...envelope, signature: sign(null, Buffer.from(canonicalize(envelope)), privateKey).toString('base64url') }
}

function octal(value, width) {
  return `${value.toString(8).padStart(width - 1, '0')}\0`
}

function tarFile(name, content, mode = 0o600) {
  const body = Buffer.from(content)
  const header = Buffer.alloc(512)
  header.write(name, 0, 100, 'utf8')
  header.write(octal(mode, 8), 100, 8, 'ascii')
  header.write(octal(0, 8), 108, 8, 'ascii')
  header.write(octal(0, 8), 116, 8, 'ascii')
  header.write(octal(body.length, 12), 124, 12, 'ascii')
  header.write(octal(Math.floor(Date.now() / 1000), 12), 136, 12, 'ascii')
  header.fill(0x20, 148, 156)
  header.write('0', 156, 1, 'ascii')
  header.write('ustar\0', 257, 6, 'ascii')
  header.write('00', 263, 2, 'ascii')
  const checksum = header.reduce((total, byte) => total + byte, 0)
  header.write(`${checksum.toString(8).padStart(6, '0')}\0 `, 148, 8, 'ascii')
  const padding = Buffer.alloc((512 - (body.length % 512)) % 512)
  return Buffer.concat([header, body, padding])
}

const workbenchManifest = JSON.stringify({
  schema_version: 'asterrouter.plugin-frontend.v1',
  plugin_id: pluginID,
  workbench: { title: 'Signed Router Sync', asset: 'app.js', style: 'app.css' }
})
const workbenchScript = `document.getElementById('aster-plugin-root').innerHTML = '<section class="signed-router-sync"><h2>Signed Router Sync Workbench</h2><p>Catalog, package, and frontend assets verified.</p></section>';\n`
const workbenchStyle = '.signed-router-sync{padding:24px;border:1px solid #16a34a;background:#f0fdf4;color:#14532d}.signed-router-sync h2{margin:0 0 8px}\n'
const pluginPackage = gzipSync(Buffer.concat([
  tarFile('plugin.json', JSON.stringify({ id: pluginID, version: '1.0.0', runtime: 'frontend', entrypoint: {} })),
  tarFile('frontend/workbench.json', workbenchManifest),
  tarFile('frontend/app.js', workbenchScript),
  tarFile('frontend/app.css', workbenchStyle),
  Buffer.alloc(1024)
]))
const packageSHA = createHash('sha256').update(pluginPackage).digest('hex')
const packageSignature = signedEnvelope('plugin_package', {
  schema_version: 'astercloud.plugin-package.v1',
  plugin: pluginSlug,
  version: '1.0.0',
  os: process.platform,
  arch: process.arch,
  sha256: packageSHA,
  size_bytes: pluginPackage.length,
  uri: 'object://router-sync/1.0.0/package.tar.gz'
})

function licenseEnvelope(snapshotVersion = 1) {
  const now = new Date()
  const expiresAt = new Date(now.getTime() + 24 * 60 * 60 * 1000)
  return signedEnvelope('license_snapshot', {
    schema_version: 'astercloud.license-snapshot.v1',
    snapshot_id: `lss_e2e_browser_${snapshotVersion}`,
    snapshot_version: snapshotVersion,
    license: {
      public_id: licenseID,
      edition: 'enterprise',
      status: 'active',
      seats: 50,
      starts_at: new Date(now.getTime() - 60 * 60 * 1000).toISOString(),
      expires_at: expiresAt.toISOString()
    },
    customer: { public_id: 'cus_e2e_browser' },
    sku: { public_id: 'sku_e2e_enterprise', code: 'ASTER-E2E', features: {}, limits: {} },
    instance: {
      public_id: instanceID,
      fingerprint,
      display_name: 'E2E Browser Router',
      first_activated_at: now.toISOString()
    },
    entitlements: [
      { public_id: 'ent_e2e_plugin', type: 'plugin', resource_key: pluginID, status: 'active', starts_at: new Date(now.getTime() - 60 * 60 * 1000).toISOString(), expires_at: expiresAt.toISOString() },
      { public_id: 'ent_e2e_feed', type: 'data_feed', resource_key: serviceKey, status: 'active', starts_at: new Date(now.getTime() - 60 * 60 * 1000).toISOString(), expires_at: expiresAt.toISOString() }
    ],
    issued_at: now.toISOString(),
    expires_at: expiresAt.toISOString()
  }, 24 * 60 * 60 * 1000)
}

function sealAESGCM(key, nonce, plaintext, additionalData) {
  const cipher = createCipheriv('aes-256-gcm', key, nonce)
  cipher.setAAD(Buffer.from(additionalData))
  return Buffer.concat([cipher.update(plaintext), cipher.final(), cipher.getAuthTag()])
}

function feedEnvelope(clientPublicKey, feedID, feedVersion) {
  const rawClientKey = Buffer.from(clientPublicKey, 'base64url')
  const clientKey = createPublicKey({
    key: Buffer.concat([Buffer.from('302a300506032b656e032100', 'hex'), rawClientKey]),
    format: 'der',
    type: 'spki'
  })
  const ephemeral = generateKeyPairSync('x25519')
  const sharedSecret = diffieHellman({ privateKey: ephemeral.privateKey, publicKey: clientKey })
  const keyEncryptionKey = Buffer.from(hkdfSync('sha256', sharedSecret, Buffer.from(feedID), Buffer.from('astercloud:official-data-feed:key-wrap:v1'), 32))
  const dataKey = randomBytes(32)
  const keyNonce = randomBytes(12)
  const payloadNonce = randomBytes(12)
  const plaintext = Buffer.from(JSON.stringify({ provider: 'synthetic-e2e', trusted: true, version: feedVersion }))
  const wrappedKey = sealAESGCM(keyEncryptionKey, keyNonce, dataKey, `${feedID}|${serviceKey}`)
  const ciphertext = sealAESGCM(dataKey, payloadNonce, plaintext, `${serviceKey}|${feedID}|provider-intelligence.feed.v1`)
  const now = new Date()
  const expiresAt = new Date(now.getTime() + 60 * 60 * 1000)
  const ephemeralRaw = ephemeral.publicKey.export({ format: 'der', type: 'spki' }).subarray(-32)
  return signedEnvelope('official_data_feed', {
    schema_version: 'astercloud.encrypted-feed-package.v1',
    service_key: serviceKey,
    feed_id: feedID,
    feed_version: feedVersion,
    data_schema_version: 'provider-intelligence.feed.v1',
    license_id: licenseID,
    instance_id: instanceID,
    issued_at: now.toISOString(),
    expires_at: expiresAt.toISOString(),
    payload: {
      cipher: 'AES-256-GCM',
      key_wrap: 'X25519-HKDF-SHA256+A256GCM',
      ephemeral_public_key: ephemeralRaw.toString('base64url'),
      encrypted_data_key_nonce: keyNonce.toString('base64url'),
      encrypted_data_key: wrappedKey.toString('base64url'),
      nonce: payloadNonce.toString('base64url'),
      ciphertext: ciphertext.toString('base64url'),
      sha256: createHash('sha256').update(plaintext).digest('hex'),
      size_bytes: plaintext.length
    },
    revocations: []
  })
}

function catalogEnvelope() {
  const now = new Date()
  const coreReleasePayload = {
    schema_version: 'astercloud.core-release.v1',
    version: coreReleaseVersion,
    channel: 'stable',
    sha256: coreReleaseSHA,
    size_bytes: coreRelease.length,
    uri: `http://127.0.0.1:${port}/objects/asterrouter-${coreReleaseVersion}`,
    min_supported_version: '0.1.0'
  }
  return signedEnvelope('catalog_index', {
    schema_version: 'astercloud.catalog-index.v1',
    catalog_version: 1,
    generated_at: now.toISOString(),
    core_releases: [{
      public_id: `core_e2e_${coreReleaseVersion.replace(/[^0-9A-Za-z]+/g, '_')}`,
      version: coreReleasePayload.version,
      channel: coreReleasePayload.channel,
      sha256: coreReleasePayload.sha256,
      size_bytes: coreReleasePayload.size_bytes,
      min_supported_version: coreReleasePayload.min_supported_version,
      published_at: now.toISOString(),
      signature: signedEnvelope('core_release', coreReleasePayload)
    }],
    plugins: [{
      public_id: 'plg_e2e_router_sync',
      plugin_id: pluginID,
      slug: pluginSlug,
      name: 'Signed Router Sync',
      summary: 'Synthetic signed frontend plugin for the isolated browser trust-chain journey.',
      category: 'official',
      vendor_name: 'AsterCloud',
      visibility: 'public',
      tier: 'free',
      versions: [{
        public_id: 'plgv_e2e_router_sync_1',
        version: '1.0.0',
        channel: 'stable',
        status: 'published',
        min_core_version: '0.1.0',
        required_entitlement: true,
        compatibility: [{ core_version_range: '>=0.1.0 <1.0.0', os: process.platform, arch: process.arch, result: 'compatible' }],
        packages: [packageID, importPackageID].map((publicID) => ({
          public_id: publicID,
          os: process.platform,
          arch: process.arch,
          sha256: packageSHA,
          size_bytes: pluginPackage.length,
          signature: packageSignature,
          revoked: false
        }))
      }]
    }],
    security_advisories: []
  })
}

function json(response, value, status = 200) {
  response.writeHead(status, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' })
  response.end(JSON.stringify(value))
}

async function bodyJSON(request) {
  const chunks = []
  for await (const chunk of request) chunks.push(chunk)
  return JSON.parse(Buffer.concat(chunks).toString('utf8') || '{}')
}

function validateHeaders(request, expected, required = []) {
  const errors = []
  for (const [name, value] of Object.entries(expected)) {
    if (request.headers[name] !== value) errors.push(name)
  }
  for (const name of required) {
    if (!String(request.headers[name] || '').trim()) errors.push(name)
  }
  return errors
}

function safeOfficialHeaders(request) {
  const headers = {}
  for (const name of [
    'x-aster-core-version',
    'x-aster-os',
    'x-aster-arch',
    'x-aster-license-id',
    'x-aster-instance-id',
    'x-aster-instance-fingerprint',
    'x-aster-request-id',
    'x-aster-idempotency-key'
  ]) {
    if (request.headers[name]) headers[name] = request.headers[name]
  }
  if (request.headers['x-aster-activation-secret']) headers['x-aster-activation-secret'] = '[REDACTED]'
  if (request.headers['x-aster-feed-public-key']) headers['x-aster-feed-public-key'] = '[PRESENT]'
  return headers
}

function recordOfficialRequest(kind, request, valid, errors = []) {
  officialRequests.push({
    kind,
    method: request.method,
    path: new URL(request.url || '/', `http://127.0.0.1:${port}`).pathname,
    valid,
    errors,
    headers: safeOfficialHeaders(request)
  })
}

function rejectInvalidHeaders(response, errors) {
  return json(response, { message: 'invalid official request headers', errors }, 401)
}

const server = http.createServer(async (request, response) => {
  const url = new URL(request.url || '/', `http://127.0.0.1:${port}`)
  try {
    if (url.pathname === '/official/v1/catalog/index') {
      recordOfficialRequest('catalog', request, true)
      return json(response, { data: catalogEnvelope() })
    }
    if (url.pathname === `/official/v1/packages/${packageID}/download` || url.pathname === `/official/v1/packages/${importPackageID}/download`) {
      const errors = validateHeaders(request, {
        'x-aster-os': process.platform,
        'x-aster-arch': process.arch,
        'x-aster-license-id': licenseID,
        'x-aster-activation-secret': 'e2e-activation-secret',
        'x-aster-instance-id': instanceID
      }, ['x-aster-core-version'])
      recordOfficialRequest('package_authorization', request, errors.length === 0, errors)
      if (errors.length > 0) return rejectInvalidHeaders(response, errors)
      const requestedPackageID = url.pathname.includes(importPackageID) ? importPackageID : packageID
      return json(response, { data: {
        id: 'dgr_e2e_browser',
        public_id: 'dgr_e2e_browser',
        package_id: requestedPackageID,
        package_public_id: requestedPackageID,
        download_url: `http://127.0.0.1:${port}/objects/router-sync.tar.gz`,
        headers: { 'X-E2E-Package': 'signed' },
        sha256: packageSHA,
        signature: packageSignature,
        expires_at: new Date(Date.now() + 10 * 60 * 1000).toISOString(),
        created_at: new Date().toISOString()
      } })
    }
    if (url.pathname === '/e2e/package-import') {
      return json(response, {
        package_id: importPackageID,
        content_base64: pluginPackage.toString('base64'),
        sha256: packageSHA,
        size_bytes: pluginPackage.length
      })
    }
    if (url.pathname === '/objects/router-sync.tar.gz') {
      const valid = request.headers['x-e2e-package'] === 'signed'
      recordOfficialRequest('package_object', request, valid, valid ? [] : ['x-e2e-package'])
      if (!valid) return response.writeHead(403).end()
      response.writeHead(200, { 'Content-Type': 'application/gzip', 'Content-Length': pluginPackage.length })
      return response.end(pluginPackage)
    }
    if (url.pathname === `/objects/asterrouter-${coreReleaseVersion}`) {
      recordOfficialRequest('core_release_object', request, true)
      response.writeHead(200, { 'Content-Type': 'application/octet-stream', 'Content-Length': String(coreRelease.length) })
      return response.end(coreRelease)
    }
    if (url.pathname === '/official/v1/licenses/activate' && request.method === 'POST') {
      const body = await bodyJSON(request)
      const valid = body.license_id === licenseID && body.activation_secret === 'e2e-activation-secret' && body.instance_id === instanceID && body.instance_fingerprint === fingerprint
      recordOfficialRequest('license_activate', request, valid, valid ? [] : ['activation_binding'])
      if (!valid) {
        return json(response, { message: 'invalid activation binding' }, 400)
      }
      return json(response, { data: { envelope: licenseEnvelope(1) } })
    }
    if (url.pathname === '/official/v1/licenses/redeem' && request.method === 'POST') {
      const body = await bodyJSON(request)
      const headerErrors = validateHeaders(request, {}, ['x-aster-core-version', 'x-aster-idempotency-key'])
      const valid = headerErrors.length === 0 && body.code === 'ASTER-E2E-REDEEM' && body.instance_id === instanceID && body.instance_fingerprint === fingerprint
      recordOfficialRequest('license_redeem', request, valid, valid ? [] : [...headerErrors, 'redemption_binding'])
      if (!valid) {
        return json(response, { message: 'invalid redemption binding' }, 400)
      }
      return json(response, { data: { envelope: licenseEnvelope(2) } })
    }
    if (url.pathname === '/e2e/license-envelope') return json(response, { envelope: licenseEnvelope(3) })
    if (url.pathname === '/e2e/feed-envelope' && request.method === 'POST') {
      const body = await bodyJSON(request)
      return json(response, { envelope: feedEnvelope(body.public_key, 'feed_e2e_import', '1') })
    }
    if (url.pathname === `/official/v1/services/${serviceKey}/feeds/latest`) {
      const errors = validateHeaders(request, {
        'x-aster-license-id': licenseID,
        'x-aster-activation-secret': 'e2e-activation-secret',
        'x-aster-instance-id': instanceID,
        'x-aster-instance-fingerprint': fingerprint
      }, ['x-aster-core-version', 'x-aster-feed-public-key', 'x-aster-request-id'])
      recordOfficialRequest('feed_metadata', request, errors.length === 0, errors)
      if (errors.length > 0) return rejectInvalidHeaders(response, errors)
      const clientPublicKey = String(request.headers['x-aster-feed-public-key'] || '')
      const metadata = signedEnvelope('official_data_feed_metadata', {
        service_key: serviceKey,
        feed_id: 'feed_e2e_sync',
        feed_version: '2',
        data_schema_version: 'provider-intelligence.feed.v1',
        request_id: String(request.headers['x-aster-request-id'] || 'feedreq_e2e'),
        expires_at: new Date(Date.now() + 10 * 60 * 1000).toISOString(),
        revocations: []
      })
      return json(response, metadata)
    }
    if (url.pathname === `/official/v1/services/${serviceKey}/feeds/feed_e2e_sync/download`) {
      const errors = validateHeaders(request, {
        'x-aster-license-id': licenseID,
        'x-aster-activation-secret': 'e2e-activation-secret',
        'x-aster-instance-id': instanceID,
        'x-aster-instance-fingerprint': fingerprint
      }, ['x-aster-core-version', 'x-aster-feed-public-key', 'x-aster-request-id'])
      recordOfficialRequest('feed_download', request, errors.length === 0, errors)
      if (errors.length > 0) return rejectInvalidHeaders(response, errors)
      const clientPublicKey = String(request.headers['x-aster-feed-public-key'] || '')
      return json(response, feedEnvelope(clientPublicKey, 'feed_e2e_sync', '2'))
    }
    if (url.pathname === '/e2e/requests') return json(response, { requests: officialRequests })
    if (url.pathname === '/health') return json(response, { ok: true, package_id: packageID })
    response.writeHead(404).end()
  } catch {
    json(response, { message: 'synthetic official service error' }, 500)
  }
})

server.listen(port, '127.0.0.1', () => {
  process.stdout.write(`Fake official services listening on http://127.0.0.1:${port}\n`)
})

for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => server.close(() => process.exit(0)))
}
