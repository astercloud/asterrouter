import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import {
  extractProductAPIOperations,
  extractPlaywrightTests,
  extractRouterPaths,
  frontendRoot,
  grepPattern,
  listE2ESpecPaths,
  loadRegistry
} from './e2e-registry.mjs'

const registry = loadRegistry()
const failures = []
const validKinds = new Set(['surface', 'journey', 'setup'])
const validProofLevels = new Set(['gate-a', 'gate-b'])
const validGates = new Set(['pr', 'nightly', 'release'])
const scenarios = new Map()
const sourceOperations = new Set(extractProductAPIOperations().map((operation) => operation.id))
const nonEmptyString = (value) => typeof value === 'string' && value.trim().length > 0
const hasDuplicates = (values) => Array.isArray(values) && new Set(values).size !== values.length

const playwrightConfig = readFileSync(resolve(frontendRoot, 'playwright.config.ts'), 'utf8')
if (!playwrightConfig.includes('const storageDatabaseURL = process.env.ASTERROUTER_SERVER_STORAGE_DATABASE_URL')) {
  failures.push('playwright.config.ts: storage backend selection must read ASTERROUTER_SERVER_STORAGE_DATABASE_URL')
}
if (!playwrightConfig.includes("`ASTER_DEV_ISOLATED_MEMORY=${storageDatabaseURL ? '0' : '1'}`")) {
  failures.push('playwright.config.ts: an explicit database URL must disable isolated-memory mode')
}

if (registry.schemaVersion !== 1) failures.push('scenario-registry.json: schemaVersion must equal 1')
if (!Array.isArray(registry.scenarios) || !Array.isArray(registry.routes)) {
  failures.push('scenario-registry.json: scenarios and routes must be arrays')
}

for (const scenario of registry.scenarios || []) {
  const location = `scenario ${JSON.stringify(scenario.id)}`
  if (!/^@e2e-[a-z0-9-]+$/.test(scenario.id || '')) failures.push(`${location}: invalid stable id`)
  if (scenarios.has(scenario.id)) failures.push(`${location}: duplicate id`)
  scenarios.set(scenario.id, scenario)
  if (!validKinds.has(scenario.kind)) failures.push(`${location}: invalid kind ${JSON.stringify(scenario.kind)}`)
  if (!nonEmptyString(scenario.title)) failures.push(`${location}: title is required`)
  if (!nonEmptyString(scenario.owner)) failures.push(`${location}: owner is required`)
  if (!nonEmptyString(scenario.fixture)) failures.push(`${location}: fixture is required`)
  if (!nonEmptyString(scenario.claim)) failures.push(`${location}: claim is required`)
  if (!Array.isArray(scenario.routes) || scenario.routes.length === 0) failures.push(`${location}: routes are required`)
  if (hasDuplicates(scenario.routes)) failures.push(`${location}: routes must be unique`)
  if (scenario.kind === 'surface') {
    if (scenario.operations !== undefined) failures.push(`${location}: surface scenarios must not claim product operations`)
  } else {
    if (!Array.isArray(scenario.operations)) failures.push(`${location}: operations must be an array`)
    else {
      if (hasDuplicates(scenario.operations)) failures.push(`${location}: operations must be unique`)
      for (const operation of scenario.operations) {
        if (!sourceOperations.has(operation)) failures.push(`${location}: unknown product operation ${JSON.stringify(operation)}`)
      }
    }
  }
  if (!Array.isArray(scenario.proofLevels) || scenario.proofLevels.length === 0 || scenario.proofLevels.some((level) => !validProofLevels.has(level))) {
    failures.push(`${location}: proofLevels must contain only gate-a/gate-b`)
  }
  if (hasDuplicates(scenario.proofLevels)) failures.push(`${location}: proofLevels must be unique`)
  if (!Array.isArray(scenario.gates) || scenario.gates.length === 0 || scenario.gates.some((gate) => !validGates.has(gate))) {
    failures.push(`${location}: gates must contain only pr/nightly/release`)
  }
  if (hasDuplicates(scenario.gates)) failures.push(`${location}: gates must be unique`)
  if (scenario.gates?.includes('release') && !scenario.proofLevels?.includes('gate-b')) failures.push(`${location}: release scenarios must prove gate-b`)
  if (!nonEmptyString(scenario.spec) || !existsSync(resolve(frontendRoot, scenario.spec))) failures.push(`${location}: spec does not exist: ${scenario.spec}`)
}

const sourceRoutes = extractRouterPaths()
const registryRoutes = registry.routes || []
const registryRoutePaths = registryRoutes.map((route) => route.path).sort()
const routesByPath = new Map(registryRoutes.map((route) => [route.path, route]))
for (const path of sourceRoutes.filter((path) => !registryRoutePaths.includes(path))) failures.push(`router: missing route contract for ${path}`)
for (const path of registryRoutePaths.filter((path) => !sourceRoutes.includes(path))) failures.push(`registry: stale route contract for ${path}`)
if (new Set(registryRoutePaths).size !== registryRoutePaths.length) failures.push('registry: route paths must be unique')

for (const route of registryRoutes) {
  const location = `route ${route.path}`
  const surface = scenarios.get(route.surface)
  if (!surface) failures.push(`${location}: unknown surface scenario ${JSON.stringify(route.surface)}`)
  else if (!['surface', 'setup'].includes(surface.kind)) failures.push(`${location}: surface must reference a surface/setup scenario`)
  else if (!Array.isArray(surface.routes) || !surface.routes.includes(route.path)) failures.push(`${location}: surface scenario does not declare this route`)
  if (!Array.isArray(route.journeys) || route.journeys.length === 0) failures.push(`${location}: at least one vertical journey is required`)
  if (hasDuplicates(route.journeys)) failures.push(`${location}: journeys must be unique`)
  for (const id of route.journeys || []) {
    const journey = scenarios.get(id)
    if (!journey) failures.push(`${location}: unknown journey ${JSON.stringify(id)}`)
    else if (!['journey', 'setup'].includes(journey.kind)) failures.push(`${location}: ${id} is not a journey/setup scenario`)
    else if (!Array.isArray(journey.routes) || !journey.routes.includes(route.path)) failures.push(`${location}: ${id} does not declare this route`)
  }
}

for (const [id, scenario] of scenarios) {
  for (const path of Array.isArray(scenario.routes) ? scenario.routes : []) {
    const route = routesByPath.get(path)
    if (!route) {
      failures.push(`scenario ${id}: declares unknown route ${path}`)
      continue
    }
    if (['surface', 'setup'].includes(scenario.kind) && route.surface !== id) failures.push(`scenario ${id}: route ${path} does not reference it as surface`)
    if (['journey', 'setup'].includes(scenario.kind) && !route.journeys?.includes(id)) failures.push(`scenario ${id}: route ${path} does not reference it as journey`)
  }
}

const actualTags = new Map()
for (const spec of listE2ESpecPaths()) {
  const tests = extractPlaywrightTests(spec)
  if (tests.length === 0) failures.push(`${spec}: spec contains no Playwright tests`)
  for (const test of tests) {
    const location = `${spec}: ${JSON.stringify(test.title)}`
    if (!test.title) failures.push(`${location}: test title must be a string literal`)
    if (test.modifier !== 'test') failures.push(`${location}: permanent test.${test.modifier} is not allowed`)
    if (test.tags.length !== 1) failures.push(`${location}: exactly one @e2e-* scenario id is required`)
    for (const tag of test.tags) {
      const locations = actualTags.get(tag) || []
      locations.push(spec)
      actualTags.set(tag, locations)
    }
  }
}
for (const [id, scenario] of scenarios) {
  const locations = actualTags.get(id) || []
  if (locations.length === 0) failures.push(`scenario ${id}: test title is missing from ${scenario.spec}`)
  if (locations.length > 1) failures.push(`scenario ${id}: tag occurs ${locations.length} times (${locations.join(', ')})`)
  if (locations.length === 1 && locations[0] !== scenario.spec) failures.push(`scenario ${id}: tag occurs in ${locations[0]}, expected ${scenario.spec}`)
}
for (const [tag, locations] of actualTags) {
  if (!scenarios.has(tag)) failures.push(`test tag ${tag}: not registered (${locations.join(', ')})`)
}
for (const gate of validGates) {
  const selected = [...scenarios.values()].filter((scenario) => scenario.gates.includes(gate))
  if (!grepPattern(selected)) failures.push(`gate ${gate}: no scenarios selected`)
}

if (failures.length > 0) {
  process.stderr.write(`E2E coverage contract failed:\n${failures.map((failure) => `- ${failure}`).join('\n')}\n`)
  process.exit(1)
}

process.stdout.write(`E2E coverage contract passed: ${sourceRoutes.length} product routes, ${scenarios.size} scenarios, 3 delivery gates.\n`)
