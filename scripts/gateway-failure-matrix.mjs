import { spawnSync } from 'node:child_process'
import { mkdirSync, writeFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const backend = resolve(root, 'backend')
const output = process.env.ASTER_GATEWAY_FAILURE_MATRIX_OUTPUT || resolve(process.env.ASTER_TEST_ARTIFACT_DIR || '/tmp/asterrouter-test-artifacts', 'gateway-failure-matrix.json')
const scenarios = [
  {
    id: 'upstream-status-failover',
    injected_failure: 'primary upstream 401, 429, and 5xx responses',
    expected: 'backup serves the response; trace records failed and selected attempts',
    test: 'TestGatewayChatCompletionFallsBackToNextAccountAfterUpstreamFailure|TestGatewayChatCompletionFallsBackAfterRateLimitAndServerError'
  },
  {
    id: 'timeout-capacity-release',
    injected_failure: 'primary upstream exceeds the client timeout',
    expected: 'backup serves the response and the primary concurrency slot is released',
    test: 'TestGatewayChatCompletionFallsBackAfterPrimaryTimeoutAndReleasesCapacity'
  },
  {
    id: 'stream-interruption',
    injected_failure: 'upstream closes an SSE response after a partial event',
    expected: 'partial response is not replayed through fallback; usage and trace record stream_error',
    test: 'TestGatewayStreamingInterruptionRecordsErrorWithoutUnsafeFailover'
  },
  {
    id: 'concurrency-skip',
    injected_failure: 'primary account has no free concurrency slot',
    expected: 'busy account is not dialed; backup serves the response and trace records skipped',
    test: 'TestGatewayChatCompletionSkipsAccountAtConcurrencyCapacity'
  },
  {
    id: 'circuit-half-open',
    injected_failure: 'configured circuit failure threshold is reached',
    expected: 'circuit opens, permits one half-open probe, and closes after success',
    test: 'TestProviderAccountCircuitOpensAndHalfOpenProbeIsExclusive'
  },
  {
    id: 'cooldown-selection',
    injected_failure: 'a selected provider account fails',
    expected: 'account is excluded during cooldown and backup remains selectable',
    test: 'TestGatewayProviderCandidatesForModelSkipsCooldownAccounts'
  }
]

const testNames = scenarios.flatMap((scenario) => scenario.test.split('|'))
const selected = testNames.join('|')
const command = ['test', '-json', '-count=1', '-run', `^(${selected})$`, './internal/server', './internal/controlplane']
const result = spawnSync('go', command, { cwd: backend, encoding: 'utf8' })
const rawOutput = `${result.stdout || ''}${result.stderr || ''}`
const events = rawOutput.split('\n').flatMap((line) => {
  try {
    return line ? [JSON.parse(line)] : []
  } catch {
    return []
  }
})
const passed = new Set(events.filter((event) => event.Action === 'pass').map((event) => event.Test))
const failed = new Set(events.filter((event) => event.Action === 'fail').map((event) => event.Test))
const report = {
  schema_version: 1,
  journey: 'J04',
  generated_at: new Date().toISOString(),
  command: `cd backend && go ${command.map((value) => JSON.stringify(value)).join(' ')}`,
  exit_code: result.status,
  scenarios: scenarios.map((scenario) => {
    const required = scenario.test.split('|')
    const hasFailure = required.some((test) => failed.has(test))
    const isPassed = required.every((test) => passed.has(test))
    return { ...scenario, status: hasFailure ? 'failed' : isPassed ? 'passed' : 'missing' }
  })
}

mkdirSync(dirname(output), { recursive: true })
writeFileSync(output, `${JSON.stringify(report, null, 2)}\n`)
process.stdout.write(`${JSON.stringify(report)}\n`)
if (result.error || result.status !== 0 || report.scenarios.some((scenario) => scenario.status !== 'passed')) {
  process.stderr.write(rawOutput)
  process.exit(result.status || 1)
}
