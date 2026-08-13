import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const scriptDirectory = dirname(fileURLToPath(import.meta.url))
const repositoryRoot = process.env.ASTER_RELEASE_CONTRACT_ROOT
  ? resolve(process.env.ASTER_RELEASE_CONTRACT_ROOT)
  : resolve(scriptDirectory, '../..')
const failures = []

function read(path) {
  return readFileSync(resolve(repositoryRoot, path), 'utf8')
}

function requireText(content, expected, location) {
  if (!content.includes(expected)) failures.push(`${location}: missing ${JSON.stringify(expected)}`)
}

function forbidText(content, forbidden, location) {
  if (content.includes(forbidden)) failures.push(`${location}: contains retired ${JSON.stringify(forbidden)}`)
}

const setupJourneyPath = 'scripts/test-setup-browser-journey.sh'
const setupJourney = read(setupJourneyPath)
requireText(
  setupJourney,
  'ASTER_SETUP_JOURNEY_ADMIN_PASSWORD:-setup-browser-test-password',
  setupJourneyPath
)
requireText(setupJourney, 'ASTER_E2E_PASSWORD="${_admin_password}"', setupJourneyPath)

const setupSpecPath = 'frontend/e2e/enterprise-setup.spec.ts'
const setupSpec = read(setupSpecPath)
requireText(setupSpec, "process.env.ASTER_E2E_PASSWORD || 'setup-browser-test-password'", setupSpecPath)

const releaseJourneyPath = 'scripts/test-release-browser-journeys.sh'
const releaseJourney = read(releaseJourneyPath)
for (const expected of [
  'ENTERPRISE_DATABASE_URL="$(database_url_for enterprise)"',
  'ASTER_SETUP_JOURNEY_DATABASE_URL="${ENTERPRISE_DATABASE_URL}"',
  'start_runtime "${port}" "${ENTERPRISE_DATABASE_URL}" "${journey_dir}"',
  'ASTER_SETUP_JOURNEY_ADMIN_PASSWORD="${ADMIN_PASSWORD}"',
  'ASTER_E2E_PASSWORD="${ADMIN_PASSWORD}"',
  'ASTER_E2E_SMTP_PORT="${SMTP_PORT}"',
  'ASTER_E2E_MAIL_API_URL="http://127.0.0.1:${MAIL_API_PORT}"',
  'node "scripts/fake-smtp.mjs"',
  '"SSL_CERT_FILE=${SMTP_CERT}"',
  'check-e2e-coverage.mjs',
  'run-e2e-gate.mjs" release --exclude-kind setup --exclude-id @e2e-system-update-lifecycle-001 --print-pattern',
  'run-e2e-gate.mjs" release --exclude-kind setup --exclude-id @e2e-system-update-lifecycle-001 --print-ids',
  'run_enterprise_journey "${RELEASE_GREP_PATTERN}"',
  '"setup_completed":true',
  'echo "commit=${COMMIT}"',
  'echo "candidate=${ARCHIVE}"',
  'echo "tested_url=http://127.0.0.1:$((BACKEND_PORT + 1))"',
  "echo 'database_class=dedicated_postgresql'"
]) {
  requireText(releaseJourney, expected, releaseJourneyPath)
}
forbidText(releaseJourney, 'enterprise_setup', releaseJourneyPath)
forbidText(releaseJourney, '@j01', releaseJourneyPath)

for (const workflowPath of ['.github/workflows/build.yml', '.github/workflows/release.yml']) {
  const workflow = read(workflowPath)
  for (const expected of [
    '--command="CREATE DATABASE asterrouter_release_test_journey_enterprise"',
    'asterrouter-release-journeys/enterprise/runtime.log',
    'asterrouter-release-journeys/enterprise/playwright'
  ]) {
    requireText(workflow, expected, workflowPath)
  }
  for (const retired of [
    'enterprise_setup',
    'asterrouter-release-journeys/profiles',
    'asterrouter-release-journeys/runtime.log'
  ]) {
    forbidText(workflow, retired, workflowPath)
  }
}

if (failures.length > 0) {
  process.stderr.write(`Release browser contract check failed:\n${failures.join('\n')}\n`)
  process.exit(1)
}

process.stdout.write('Release browser database continuity and evidence contract check passed.\n')
