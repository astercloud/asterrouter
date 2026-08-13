import { spawnSync } from 'node:child_process'
import { grepPattern, loadRegistry } from './e2e-registry.mjs'

const usage = 'Usage: node scripts/run-e2e-gate.mjs <pr|nightly|release> [--print-pattern|--print-ids] [--exclude-kind <kind>] [--exclude-id <scenario-id>] [-- <playwright args>]\n'

function fail(message) {
  process.stderr.write(`${message}\n${usage}`)
  process.exit(2)
}

const gate = process.argv[2]
const validGates = new Set(['pr', 'nightly', 'release'])
if (!validGates.has(gate)) {
  fail(`Unknown E2E gate: ${gate || '(missing)'}`)
}

const args = process.argv.slice(3)
const separator = args.indexOf('--')
const gateArgs = separator >= 0 ? args.slice(0, separator) : args
const playwrightArgs = separator >= 0 ? args.slice(separator + 1) : []
const excludedKinds = new Set()
const excludedIDs = new Set()
let output = ''

for (let index = 0; index < gateArgs.length; index += 1) {
  const argument = gateArgs[index]
  if (argument === '--print-pattern' || argument === '--print-ids') {
    if (output && output !== argument) fail('Only one print mode may be selected.')
    output = argument
    continue
  }
  if (argument === '--exclude-kind' || argument === '--exclude-id') {
    const value = gateArgs[index + 1]
    if (!value || value.startsWith('--')) fail(`${argument} requires a value.`)
    if (argument === '--exclude-kind') excludedKinds.add(value)
    else excludedIDs.add(value)
    index += 1
    continue
  }
  fail(`Unknown E2E gate option: ${argument}`)
}

const registry = loadRegistry()
const gateScenarios = registry.scenarios.filter((scenario) => scenario.gates.includes(gate))
const gateKinds = new Set(gateScenarios.map((scenario) => scenario.kind))
const gateIDs = new Set(gateScenarios.map((scenario) => scenario.id))
for (const kind of excludedKinds) {
  if (!gateKinds.has(kind)) fail(`Cannot exclude unknown ${gate} gate scenario kind: ${kind}`)
}
for (const id of excludedIDs) {
  if (!gateIDs.has(id)) fail(`Cannot exclude scenario not registered for the ${gate} gate: ${id}`)
}

const scenarios = gateScenarios.filter((scenario) => !excludedKinds.has(scenario.kind) && !excludedIDs.has(scenario.id))
const pattern = grepPattern(scenarios)
if (!pattern) {
  process.stderr.write(`No E2E scenarios are registered for the ${gate} gate.\n`)
  process.exit(1)
}

if (output === '--print-pattern') {
  process.stdout.write(`${pattern}\n`)
  process.exit(0)
}
if (output === '--print-ids') {
  process.stdout.write(`${scenarios.map((scenario) => scenario.id).join(',')}\n`)
  process.exit(0)
}

const executable = process.platform === 'win32' ? 'npx.cmd' : 'npx'
const result = spawnSync(executable, ['playwright', 'test', '--grep', pattern, ...playwrightArgs], {
  cwd: process.cwd(),
  env: process.env,
  stdio: 'inherit'
})
if (result.error) throw result.error
process.exit(result.status ?? 1)
