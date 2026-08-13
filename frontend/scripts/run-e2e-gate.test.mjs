import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import test from 'node:test'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const scriptDirectory = dirname(fileURLToPath(import.meta.url))
const gateScript = resolve(scriptDirectory, 'run-e2e-gate.mjs')

function runGate(...args) {
  return spawnSync(process.execPath, [gateScript, ...args], { encoding: 'utf8' })
}

test('exclude-id removes a dedicated scenario from both release outputs', () => {
  const options = ['release', '--exclude-kind', 'setup', '--exclude-id', '@e2e-system-update-001', '--exclude-id', '@e2e-system-update-lifecycle-001']
  const ids = runGate(...options, '--print-ids')
  const pattern = runGate(...options, '--print-pattern')

  assert.equal(ids.status, 0, ids.stderr)
  assert.equal(pattern.status, 0, pattern.stderr)
  assert.doesNotMatch(ids.stdout, /@e2e-system-update-001/)
  assert.doesNotMatch(ids.stdout, /@e2e-system-update-lifecycle-001/)
  assert.doesNotMatch(pattern.stdout, /@e2e-system-update-001/)
  assert.doesNotMatch(pattern.stdout, /@e2e-system-update-lifecycle-001/)
})

test('exclude-id fails closed for a scenario outside the selected gate', () => {
  const result = runGate('pr', '--exclude-id', '@e2e-system-update-lifecycle-001', '--print-ids')

  assert.equal(result.status, 2)
  assert.match(result.stderr, /not registered for the pr gate/)
})

test('exclude-id requires a value', () => {
  const result = runGate('release', '--exclude-id')

  assert.equal(result.status, 2)
  assert.match(result.stderr, /--exclude-id requires a value/)
})
