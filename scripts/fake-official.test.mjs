import assert from 'node:assert/strict'
import { mkdtempSync, readFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { spawn } from 'node:child_process'
import test from 'node:test'

function freePort() {
  return 31000 + Math.floor(Math.random() * 10000)
}

async function waitUntilReady(url, child) {
  for (let attempt = 0; attempt < 100; attempt += 1) {
    if (child.exitCode !== null) throw new Error(`fake official service exited with ${child.exitCode}`)
    try {
      const response = await fetch(`${url}/health`)
      if (response.ok) return
    } catch {
      // The isolated process is still binding its listener.
    }
    await new Promise((resolve) => setTimeout(resolve, 25))
  }
  throw new Error('fake official service did not become ready')
}

test('fake official service publishes isolated trust and package contracts', async () => {
  const port = freePort()
  const root = mkdtempSync(join(tmpdir(), 'asterrouter-fake-official-'))
  const keyFile = join(root, 'public-key')
  const releaseVersion = '1.2.3'
  const child = spawn(process.execPath, ['scripts/fake-official.mjs'], {
    cwd: new URL('..', import.meta.url),
    env: {
      ...process.env,
      ASTER_E2E_OFFICIAL_PORT: String(port),
      ASTER_E2E_OFFICIAL_KEY_FILE: keyFile,
      ASTER_E2E_OFFICIAL_CORE_RELEASE_VERSION: releaseVersion
    },
    stdio: 'ignore'
  })

  try {
    const baseURL = `http://127.0.0.1:${port}`
    await waitUntilReady(baseURL, child)
    assert.match(readFileSync(keyFile, 'utf8'), /^[A-Za-z0-9+/]+={0,2}$/)

    const catalogResponse = await fetch(`${baseURL}/official/v1/catalog/index`)
    assert.equal(catalogResponse.status, 200)
    const catalog = await catalogResponse.json()
    assert.equal(catalog.data.purpose, 'catalog_index')
    assert.equal(catalog.data.payload.plugins[0].plugin_id, 'com.astercloud.catalog.router-sync')
    assert.equal(catalog.data.payload.plugins[0].versions[0].required_entitlement, true)
    assert.match(catalog.data.payload.plugins[0].versions[0].packages[0].public_id, /^pkg_router_sync_/)
    assert.equal(catalog.data.payload.core_releases[0].version, releaseVersion)
    assert.equal(catalog.data.payload.core_releases[0].signature.purpose, 'core_release')
    assert.equal(catalog.data.payload.core_releases[0].signature.payload.schema_version, 'astercloud.core-release.v1')
    const coreReleaseResponse = await fetch(catalog.data.payload.core_releases[0].signature.payload.uri)
    assert.equal(coreReleaseResponse.status, 200)
    assert.match(await coreReleaseResponse.text(), /^asterrouter 1\.2\.3 synthetic release artifact/)

    const licenseResponse = await fetch(`${baseURL}/official/v1/licenses/activate`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        license_id: 'lic_e2e_browser',
        activation_secret: 'e2e-activation-secret',
        instance_id: 'inst_e2e_browser',
        instance_fingerprint: 'sha256:e2e-browser-fingerprint'
      })
    })
    assert.equal(licenseResponse.status, 200)
    const license = await licenseResponse.json()
    assert.equal(license.data.envelope.purpose, 'license_snapshot')
    assert.equal(license.data.envelope.payload.instance.public_id, 'inst_e2e_browser')

    const deniedRedeem = await fetch(`${baseURL}/official/v1/licenses/redeem`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        code: 'ASTER-E2E-REDEEM',
        instance_id: 'inst_e2e_browser',
        instance_fingerprint: 'sha256:e2e-browser-fingerprint'
      })
    })
    assert.equal(deniedRedeem.status, 400)

    const packageID = catalog.data.payload.plugins[0].versions[0].packages[0].public_id
    const deniedPackage = await fetch(`${baseURL}/official/v1/packages/${packageID}/download`)
    assert.equal(deniedPackage.status, 401)
    const deniedPackageBody = await deniedPackage.json()
    assert.deepEqual(deniedPackageBody.errors.sort(), [
      'x-aster-activation-secret',
      'x-aster-arch',
      'x-aster-core-version',
      'x-aster-instance-id',
      'x-aster-license-id',
      'x-aster-os'
    ])

    const packageGrantResponse = await fetch(`${baseURL}/official/v1/packages/${packageID}/download`, {
      headers: {
        'X-Aster-Core-Version': '0.23.1',
        'X-Aster-OS': process.platform,
        'X-Aster-Arch': process.arch,
        'X-Aster-License-ID': 'lic_e2e_browser',
        'X-Aster-Activation-Secret': 'e2e-activation-secret',
        'X-Aster-Instance-ID': 'inst_e2e_browser'
      }
    })
    assert.equal(packageGrantResponse.status, 200)
    const packageGrant = await packageGrantResponse.json()
    assert.equal(packageGrant.data.package_id, packageID)

    const deniedFeed = await fetch(`${baseURL}/official/v1/services/provider-intelligence/feeds/latest`)
    assert.equal(deniedFeed.status, 401)

    const requestsResponse = await fetch(`${baseURL}/e2e/requests`)
    assert.equal(requestsResponse.status, 200)
    const requests = (await requestsResponse.json()).requests
    assert.ok(requests.some((request) => request.kind === 'package_authorization' && request.valid === false))
    const validPackageRequest = requests.find((request) => request.kind === 'package_authorization' && request.valid === true)
    assert.equal(validPackageRequest.headers['x-aster-activation-secret'], '[REDACTED]')
    assert.equal(JSON.stringify(validPackageRequest).includes('e2e-activation-secret'), false)
    assert.ok(requests.some((request) => request.kind === 'feed_metadata' && request.valid === false))
    assert.ok(requests.some((request) => request.kind === 'license_redeem' && request.valid === false))
    assert.ok(requests.some((request) => request.kind === 'core_release_object' && request.valid === true))
  } finally {
    child.kill('SIGTERM')
    await new Promise((resolve) => child.once('exit', resolve))
  }
})
