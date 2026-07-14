import { existsSync, mkdirSync, readFileSync, readdirSync, statSync, writeFileSync } from 'node:fs'
import { basename, dirname, extname, join, relative, resolve } from 'node:path'

function argumentsFor(name) {
  const values = []
  for (let index = 2; index < process.argv.length; index++) {
    if (process.argv[index] === name && process.argv[index + 1]) values.push(process.argv[++index])
  }
  return values
}

function walk(path) {
  if (!existsSync(path)) return []
  if (!statSync(path).isDirectory()) return [path]
  return readdirSync(path, { withFileTypes: true }).flatMap((entry) => walk(join(path, entry.name)))
}

function attributes(fragment) {
  return Object.fromEntries([...fragment.matchAll(/([\w:-]+)="([^"]*)"/g)].map((match) => [match[1], match[2]]))
}

function normalizeName(value) {
  return value.replace(/\s+/g, ' ').trim()
}

function parseJUnit(source, body) {
  const results = []
  const expanded = body.replace(/<testcase\b([^>]*)\/>/g, '<testcase$1></testcase>')
  const paired = /<testcase\b([^>]*)>([\s\S]*?)<\/testcase>/g
  for (const match of expanded.matchAll(paired)) {
    const attrs = attributes(match[1])
    const name = normalizeName(`${attrs.classname || 'junit'}.${attrs.name || 'unnamed'}`)
    const detail = match[2]
    results.push({ source, name, status: /<(failure|error)\b/.test(detail) ? 'failed' : /<skipped\b/.test(detail) ? 'skipped' : 'passed' })
  }
  return results
}

function parseGoJSON(source, body) {
  const latest = new Map()
  for (const line of body.split(/\r?\n/)) {
    let event
    try {
      event = JSON.parse(line)
    } catch {
      continue
    }
    if (!event.Test || !['pass', 'fail', 'skip'].includes(event.Action)) continue
    latest.set(`${event.Package}.${event.Test}`, event.Action === 'pass' ? 'passed' : event.Action === 'fail' ? 'failed' : 'skipped')
  }
  return [...latest.entries()].map(([name, status]) => ({ source, name, status }))
}

function parseReport(source, parsed) {
  if (parsed.schema_version !== 1 || !Array.isArray(parsed.tests)) return []
  return parsed.tests.flatMap((test) => {
    const result = []
    for (let index = 0; index < Number(test.passed_runs || 0); index++) result.push({ source, name: test.name, status: 'passed' })
    for (let index = 0; index < Number(test.failed_runs || 0); index++) result.push({ source, name: test.name, status: 'failed' })
    for (let index = 0; index < Number(test.skipped_runs || 0); index++) result.push({ source, name: test.name, status: 'skipped' })
    return result
  })
}

function parseFile(path) {
  const body = readFileSync(path, 'utf8')
  const source = relative(process.cwd(), path) || basename(path)
  const trimmed = body.trim()
  if (extname(path) === '.xml' || trimmed.startsWith('<')) return parseJUnit(source, body)
  if (trimmed.startsWith('{')) {
    try {
      const parsed = JSON.parse(trimmed)
      const report = parseReport(source, parsed)
      if (report.length > 0) return report
    } catch {
      // A Go JSON stream starts with an object but is intentionally not one JSON document.
    }
  }
  return parseGoJSON(source, body)
}

const output = resolve(argumentsFor('--output')[0] || process.env.ASTER_FLAKY_REPORT || '')
const inputPaths = [...argumentsFor('--input'), ...argumentsFor('--history')].flatMap((path) => walk(resolve(path)))
const failOnSuspected = process.argv.includes('--fail-on-suspected') || process.env.ASTER_FLAKY_FAIL_ON_SUSPECTED === '1'
if (!output || inputPaths.length === 0) {
  process.stderr.write('Usage: node scripts/flaky-trend.mjs --input <junit.xml|go.json|directory> [--history <report-directory>] --output <report.json>\n')
  process.exit(2)
}

const outcomes = inputPaths.flatMap(parseFile)
const tests = new Map()
for (const outcome of outcomes) {
  const item = tests.get(outcome.name) || { name: outcome.name, passed_runs: 0, failed_runs: 0, skipped_runs: 0, sources: new Set() }
  item[`${outcome.status}_runs`]++
  item.sources.add(outcome.source)
  tests.set(outcome.name, item)
}

const reportTests = [...tests.values()].map((test) => {
  const runCount = test.passed_runs + test.failed_runs + test.skipped_runs
  // A consistently failing test is a regression, not a flaky test. A mixed
  // outcome is the machine-observable part of the policy's flaky definition;
  // repeated failures remain visible for owner/root-cause review.
  const suspectedFlaky = test.failed_runs > 0 && test.passed_runs > 0
  return {
    name: test.name,
    run_count: runCount,
    observation_count: runCount,
    passed_runs: test.passed_runs,
    failed_runs: test.failed_runs,
    skipped_runs: test.skipped_runs,
    // JUnit and Go JSON do not expose an authoritative retry ordinal. Keep
    // cross-run observations separate from in-run retries so the report never
    // manufactures a retry that did not occur.
    retry_count: 0,
    suspected_flaky: suspectedFlaky,
    repeated_failure: test.failed_runs >= 2,
    sources: [...test.sources].sort()
  }
}).sort((left, right) => left.name.localeCompare(right.name))

const report = {
  schema_version: 1,
  generated_at: new Date().toISOString(),
  policy: 'docs/test/v1/FLAKY_TEST_POLICY.md',
  inputs: inputPaths.map((path) => relative(process.cwd(), path) || basename(path)).sort(),
  summary: {
    test_count: reportTests.length,
    passed_runs: reportTests.reduce((total, test) => total + test.passed_runs, 0),
    failed_runs: reportTests.reduce((total, test) => total + test.failed_runs, 0),
    skipped_runs: reportTests.reduce((total, test) => total + test.skipped_runs, 0),
    suspected_flaky_count: reportTests.filter((test) => test.suspected_flaky).length,
    repeated_failure_count: reportTests.filter((test) => test.repeated_failure).length
  },
  suspected_flaky: reportTests.filter((test) => test.suspected_flaky).map((test) => test.name),
  tests: reportTests
}

mkdirSync(dirname(output), { recursive: true })
writeFileSync(output, `${JSON.stringify(report, null, 2)}\n`)
process.stdout.write(`${JSON.stringify(report)}\n`)
if (failOnSuspected && report.summary.suspected_flaky_count > 0) {
  process.stderr.write(`Suspected flaky tests require investigation: ${report.suspected_flaky.join(', ')}\n`)
  process.exit(1)
}
