import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const scriptDirectory = dirname(fileURLToPath(import.meta.url))
const repositoryRoot = resolve(scriptDirectory, '../..')
const lifecycleID = '@e2e-system-update-lifecycle-001'
const failures = []

function read(path) {
  return readFileSync(resolve(repositoryRoot, path), 'utf8')
}

function requireText(content, expected, location) {
  if (!content.includes(expected)) failures.push(`${location}: missing ${JSON.stringify(expected)}`)
}

const packagePath = 'frontend/package.json'
const packageJSON = JSON.parse(read(packagePath))
for (const name of ['test:e2e:nightly', 'test:e2e:release']) {
  requireText(packageJSON.scripts[name] || '', `--exclude-id ${lifecycleID}`, `${packagePath}#scripts.${name}`)
}

const playwrightConfigPath = 'frontend/playwright.config.ts'
const playwrightConfig = read(playwrightConfigPath)
for (const expected of [
  "const oidcEnabled = isolatedOIDC || process.env.ASTER_E2E_OIDC_ENABLED === '1'",
  'oidcEnabled ? `https://127.0.0.1:${oidcProxyPort}`',
  "process.env.ASTER_E2E_OIDC_AVAILABLE = oidcEnabled ? '1' : '0'",
  'ignoreHTTPSErrors: oidcEnabled',
  '`ASTER_E2E_OIDC_ENABLED=${oidcEnabled ? \'1\' : \'0\'}`'
]) {
  requireText(playwrightConfig, expected, playwrightConfigPath)
}

const runtimePath = 'backend/internal/appcmd/server/runtime.go'
const runtime = read(runtimePath)
for (const expected of [
  'ASTER_E2E_POSTGRES_AVAILABLE',
  'parsed.Scheme != "postgres" && parsed.Scheme != "postgresql"',
  'token == "e2e" || token == "test"'
]) {
  requireText(runtime, expected, runtimePath)
}

const releaseJourneyPath = 'scripts/test-release-browser-journeys.sh'
const releaseJourney = read(releaseJourneyPath)
for (const output of ['--print-pattern', '--print-ids']) {
  requireText(releaseJourney, `--exclude-id ${lifecycleID} ${output}`, releaseJourneyPath)
}

const lifecyclePath = 'scripts/test-system-update-lifecycle.sh'
const lifecycle = read(lifecyclePath)
for (const expected of [
  'ASTER_E2E_OFFICIAL_CORE_RELEASE_VERSION="${NEW_VERSION}"',
  `--grep ${lifecycleID}`,
  'if [ "${START_COUNT}" != "3" ]',
  'if [ "${START_SEQUENCE}" != "${OLD_VERSION},${NEW_VERSION},${OLD_VERSION}" ]',
  'if [ "${EXIT_FAILURES}" != "0" ]',
  "echo 'system_update_lifecycle=passed'",
  "echo 'database_class=dedicated_postgresql'",
  "echo 'execution=dedicated_release_binary_supervisor'"
]) {
  requireText(lifecycle, expected, lifecyclePath)
}

for (const workflowPath of ['.github/workflows/nightly.yml', '.github/workflows/build.yml', '.github/workflows/release.yml']) {
  const workflow = read(workflowPath)
  for (const expected of [
    'ASTER_SYSTEM_UPDATE_E2E_DIR: ${{ runner.temp }}/asterrouter-system-update-lifecycle',
    'bash scripts/test-system-update-lifecycle.sh',
    "grep -Fxq 'system_update_lifecycle=passed'",
    'asterrouter-system-update-lifecycle/report.txt',
    'asterrouter-system-update-lifecycle/generations.log',
    'asterrouter-system-update-lifecycle/supervisor.log',
    'asterrouter-system-update-lifecycle/runtime.log',
    'asterrouter-system-update-lifecycle/postgres.log',
    'asterrouter-system-update-lifecycle/official.log',
    'asterrouter-system-update-lifecycle/playwright'
  ]) {
    requireText(workflow, expected, workflowPath)
  }
}

const nightlyWorkflowPath = '.github/workflows/nightly.yml'
const nightlyWorkflow = read(nightlyWorkflowPath)
const nightlyBrowserStep = nightlyWorkflow.match(/- name: Nightly browser gate[\s\S]*?run: npm run test:e2e:nightly/)?.[0] || ''
requireText(nightlyBrowserStep, "ASTER_E2E_OIDC_ENABLED: '1'", `${nightlyWorkflowPath}#Nightly browser gate`)

if (failures.length > 0) {
  process.stderr.write(`E2E lifecycle contract check failed:\n${failures.join('\n')}\n`)
  process.exit(1)
}

process.stdout.write('Dedicated system update lifecycle gate and evidence contract check passed.\n')
