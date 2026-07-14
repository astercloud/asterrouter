import { mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'

function argument(name, fallback = '') {
  const index = process.argv.indexOf(name)
  return index >= 0 && index + 1 < process.argv.length ? process.argv[index + 1] : fallback
}

function median(values) {
  const sorted = [...values].sort((left, right) => left - right)
  const middle = Math.floor(sorted.length / 2)
  return sorted.length % 2 === 0 ? (sorted[middle - 1] + sorted[middle]) / 2 : sorted[middle]
}

function summarize(values) {
  return {
    samples: values.length,
    median: median(values),
    min: Math.min(...values),
    max: Math.max(...values)
  }
}

const inputValue = argument('--input', process.env.ASTER_BENCHMARK_INPUT || '')
const baselineValue = argument('--baseline', process.env.ASTER_BENCHMARK_BASELINE || 'docs/test/v1/performance-baseline.ubuntu-24.04.json')
const outputValue = argument('--output', process.env.ASTER_BENCHMARK_REPORT || '')
const input = inputValue ? resolve(inputValue) : ''
const baselinePath = resolve(baselineValue)
const output = outputValue ? resolve(outputValue) : ''
const enforce = process.argv.includes('--enforce') || process.env.ASTER_BENCHMARK_ENFORCE === '1'

if (!input || !output) {
  process.stderr.write('Usage: node scripts/benchmark-report.mjs --input <benchmark.txt> --output <report.json> [--baseline <baseline.json>] [--enforce]\n')
  process.exit(2)
}

const samples = new Map()
const pattern = /^(Benchmark\S+?)(?:-\d+)?\s+\d+\s+([\d.]+)\s+ns\/op(?:\s+([\d.]+)\s+B\/op)?(?:\s+([\d.]+)\s+allocs\/op)?\s*$/
for (const line of readFileSync(input, 'utf8').split(/\r?\n/)) {
  const match = line.match(pattern)
  if (!match) continue
  const [, name, nsPerOp, bytesPerOp, allocsPerOp] = match
  const values = samples.get(name) || { ns_per_op: [], bytes_per_op: [], allocs_per_op: [] }
  values.ns_per_op.push(Number(nsPerOp))
  if (bytesPerOp !== undefined) values.bytes_per_op.push(Number(bytesPerOp))
  if (allocsPerOp !== undefined) values.allocs_per_op.push(Number(allocsPerOp))
  samples.set(name, values)
}

if (samples.size === 0) {
  process.stderr.write(`No Go benchmark rows found in ${input}.\n`)
  process.exit(1)
}

const baseline = JSON.parse(readFileSync(baselinePath, 'utf8'))
if (baseline.schema_version !== 1 || !baseline.thresholds || !baseline.benchmarks) {
  process.stderr.write(`Invalid benchmark baseline: ${baselinePath}.\n`)
  process.exit(1)
}

const benchmarks = Object.fromEntries([...samples.entries()].sort(([left], [right]) => left.localeCompare(right)).map(([name, values]) => [name, {
  ns_per_op: summarize(values.ns_per_op),
  ...(values.bytes_per_op.length > 0 ? { bytes_per_op: summarize(values.bytes_per_op) } : {}),
  ...(values.allocs_per_op.length > 0 ? { allocs_per_op: summarize(values.allocs_per_op) } : {})
}]))

const metricThresholds = {
  ns_per_op: baseline.thresholds.ns_per_op_ratio,
  bytes_per_op: baseline.thresholds.bytes_per_op_ratio,
  allocs_per_op: baseline.thresholds.allocs_per_op_ratio
}
const regressions = []
if (baseline.status === 'confirmed') {
  for (const [name, result] of Object.entries(benchmarks)) {
    const expected = baseline.benchmarks[name]
    if (!expected) {
      regressions.push({ benchmark: name, metric: 'benchmark', reason: 'missing_from_confirmed_baseline' })
      continue
    }
    for (const metric of Object.keys(metricThresholds)) {
      if (result[metric] === undefined || expected[metric] === undefined) continue
      const ratio = result[metric].median / expected[metric]
      if (ratio > metricThresholds[metric]) {
        regressions.push({
          benchmark: name,
          metric,
          baseline: expected[metric],
          actual: result[metric].median,
          ratio: Number(ratio.toFixed(4)),
          maximum_ratio: metricThresholds[metric]
        })
      }
    }
  }
}

const report = {
  schema_version: 1,
  generated_at: new Date().toISOString(),
  input,
  baseline: {
    path: baselinePath,
    status: baseline.status,
    environment: baseline.environment,
    source: baseline.source || ''
  },
  thresholds: baseline.thresholds,
  benchmarks,
  regressions,
  status: baseline.status === 'confirmed' ? (regressions.length === 0 ? 'passed' : 'regressed') : 'bootstrap_required'
}

mkdirSync(dirname(output), { recursive: true })
writeFileSync(output, `${JSON.stringify(report, null, 2)}\n`)
process.stdout.write(`${JSON.stringify(report)}\n`)

if (enforce && report.status === 'regressed') process.exit(1)
