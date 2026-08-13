import assert from 'node:assert/strict'
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { spawnSync } from 'node:child_process'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')

test('dev.sh can skip the repository .env for isolated E2E runtimes', () => {
  const root = mkdtempSync(join(tmpdir(), 'asterrouter-dev-env-test-'))
  const marker = join(root, 'env-loaded')
  const script = join(root, 'scripts', 'dev.sh')

  try {
    mkdirSync(dirname(script), { recursive: true })
    mkdirSync(join(root, 'backend', 'cmd', 'asterrouter'), { recursive: true })
    copyFileSync(join(repositoryRoot, 'scripts', 'dev.sh'), script)
    writeFileSync(join(root, 'backend', 'cmd', 'asterrouter', 'VERSION'), 'test\n')
    writeFileSync(join(root, '.env'), 'printf loaded > "${ASTER_DEV_ENV_TEST_MARKER}"\n')

    const isolated = spawnSync('bash', [script, '--help'], {
      encoding: 'utf8',
      env: {
        ...process.env,
        ASTER_DEV_ENV_TEST_MARKER: marker,
        ASTER_DEV_SKIP_ENV_FILE: '1'
      }
    })
    assert.equal(isolated.status, 0, isolated.stderr)
    assert.equal(existsSync(marker), false)

    const ordinary = spawnSync('bash', [script, '--help'], {
      encoding: 'utf8',
      env: {
        ...process.env,
        ASTER_DEV_ENV_TEST_MARKER: marker,
        ASTER_DEV_SKIP_ENV_FILE: '0'
      }
    })
    assert.equal(ordinary.status, 0, ordinary.stderr)
    assert.equal(existsSync(marker), true)
  } finally {
    rmSync(root, { recursive: true, force: true })
  }
})
