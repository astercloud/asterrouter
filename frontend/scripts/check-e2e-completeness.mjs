import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import {
  extractProductAPIOperations,
  frontendRoot,
  indexOwnerEvidence,
  loadCapabilityRegistry,
  loadOwnerEvidence,
  loadRegistry,
  requiredCapabilityProofs,
  repositoryRoot
} from './e2e-registry.mjs'

const strict = process.argv.includes('--strict')
const failures = []
const gaps = []
const scenarioRegistry = loadRegistry()
const capabilityRegistry = loadCapabilityRegistry()
const ownerEvidenceRegistry = loadOwnerEvidence()
const scenarios = new Map(scenarioRegistry.scenarios.map((scenario) => [scenario.id, scenario]))
const sourceOperations = new Map(extractProductAPIOperations().map((operation) => [operation.id, operation]))
const capabilities = new Map()
const proofKinds = ['success', 'negative', 'boundary', 'browser']
const ownerProofKinds = ['success', 'negative', 'boundary']
const expectedOwnerEvidence = indexOwnerEvidence(ownerEvidenceRegistry)
const ownerClaims = new Set()

if (capabilityRegistry.schemaVersion !== 1) failures.push('capability-registry.json: schemaVersion must equal 1')
if (!Array.isArray(capabilityRegistry.capabilities)) failures.push('capability-registry.json: capabilities must be an array')
if (ownerEvidenceRegistry.schemaVersion !== 1) failures.push('owner-evidence.json: schemaVersion must equal 1')
if (!Array.isArray(ownerEvidenceRegistry.evidence)) failures.push('owner-evidence.json: evidence must be an array')

for (const [index, entry] of (ownerEvidenceRegistry.evidence || []).entries()) {
  const location = `owner-evidence.json evidence[${index}]`
  validateReference(entry.reference, location, 'owner')
  if (!Array.isArray(entry.proofs) || entry.proofs.length === 0 || entry.proofs.some((proof) => !ownerProofKinds.includes(proof))) {
    failures.push(`${location}: proofs must contain only success/negative/boundary`)
  }
  if (new Set(entry.proofs || []).size !== (entry.proofs || []).length) failures.push(`${location}: proofs must be unique`)
  if (!Array.isArray(entry.operations) || entry.operations.length === 0) failures.push(`${location}: operations are required`)
  if (new Set(entry.operations || []).size !== (entry.operations || []).length) failures.push(`${location}: operations must be unique`)
  for (const operation of entry.operations || []) {
    if (!sourceOperations.has(operation)) failures.push(`${location}: unknown product operation ${operation}`)
    for (const proof of entry.proofs || []) {
      const claim = `${entry.reference}\u0000${proof}\u0000${operation}`
      if (ownerClaims.has(claim)) failures.push(`${location}: duplicate ${proof} claim for ${operation} and ${entry.reference}`)
      ownerClaims.add(claim)
    }
  }
}

function sameStrings(left, right) {
  return Array.isArray(left) && Array.isArray(right) && left.length === right.length && left.every((value, index) => value === right[index])
}

function validateReference(reference, location, proofType) {
  if (typeof reference !== 'string' || !reference.trim()) {
    failures.push(`${location}: evidence reference must be a non-empty string`)
    return
  }
  if (reference.startsWith('@e2e-')) {
    if (proofType === 'owner') {
      failures.push(`${location}: owner evidence must reference an owner test, not a browser scenario`)
      return
    }
    const scenario = scenarios.get(reference)
    if (!scenario) failures.push(`${location}: unknown scenario ${reference}`)
    else if (proofType === 'browser' && !['journey', 'setup'].includes(scenario.kind)) failures.push(`${location}: browser evidence must reference a journey/setup, not ${scenario.kind}`)
    return
  }
  if (proofType === 'browser') {
    failures.push(`${location}: browser evidence must reference a registered @e2e-* journey/setup scenario`)
    return
  }
  const [relativePath, testName] = reference.split('#', 2)
  const path = resolve(repositoryRoot, relativePath)
  if (!existsSync(path)) {
    failures.push(`${location}: owner test file does not exist: ${relativePath}`)
    return
  }
  if (!testName) {
    failures.push(`${location}: owner test reference must include #TestName: ${reference}`)
    return
  }
  const escapedTestName = testName.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  if (!new RegExp(`^func\\s+${escapedTestName}\\s*\\(`, 'm').test(readFileSync(path, 'utf8'))) {
    failures.push(`${location}: exact owner test function is absent: ${reference}`)
  }
}

for (const capability of capabilityRegistry.capabilities || []) {
  const location = `capability ${JSON.stringify(capability.id)}`
  if (capabilities.has(capability.id)) failures.push(`${location}: duplicate id`)
  capabilities.set(capability.id, capability)
  const source = sourceOperations.get(capability.id)
  if (!source) {
    failures.push(`${location}: stale operation is not imported by a Vue product surface`)
    continue
  }
  if (!['P0', 'P1', 'P2'].includes(capability.risk)) failures.push(`${location}: risk must be P0, P1, or P2`)
  if (typeof capability.owner !== 'string' || !capability.owner.trim()) failures.push(`${location}: owner is required`)
  if (capability.interaction !== source.interaction) failures.push(`${location}: interaction drift; expected ${source.interaction}`)
  if (!sameStrings(capability.methods, source.methods)) failures.push(`${location}: HTTP methods drift; regenerate the capability registry`)
  if (!sameStrings(capability.views, source.views)) failures.push(`${location}: view imports drift; regenerate the capability registry`)
  if (!capability.evidence || typeof capability.evidence !== 'object') {
    failures.push(`${location}: evidence object is required`)
    continue
  }
  for (const proof of proofKinds) {
    const references = capability.evidence[proof]
    if (!Array.isArray(references)) {
      failures.push(`${location}: evidence.${proof} must be an array`)
      continue
    }
    if (new Set(references).size !== references.length) failures.push(`${location}: evidence.${proof} must be unique`)
    for (const reference of references) {
      validateReference(reference, `${location} evidence.${proof}`, proof === 'browser' ? 'browser' : 'owner')
      if (proof === 'browser' && reference.startsWith('@e2e-')) {
        const scenario = scenarios.get(reference)
        if (scenario && !scenario.operations?.includes(capability.id)) {
          failures.push(`${location} evidence.browser: ${reference} does not declare operation ${capability.id}`)
        }
      }
    }
    if (references.length === 0 && requiredCapabilityProofs(capability).includes(proof)) {
      gaps.push({ id: capability.id, proof, risk: capability.risk, interaction: capability.interaction })
    }
    if (ownerProofKinds.includes(proof)) {
      const expected = expectedOwnerEvidence.get(capability.id)?.[proof] || []
      if (!sameStrings(references.filter((reference) => !reference.startsWith('@e2e-')), expected)) {
        failures.push(`${location} evidence.${proof}: owner evidence drift; regenerate the capability registry`)
      }
    }
  }
}

for (const id of sourceOperations.keys()) {
  if (!capabilities.has(id)) failures.push(`source operation ${id}: missing capability registry entry`)
}

for (const scenario of scenarioRegistry.scenarios || []) {
  if (!['journey', 'setup'].includes(scenario.kind)) continue
  for (const operation of scenario.operations || []) {
    const capability = capabilities.get(operation)
    if (!capability) continue
    if (!capability.evidence.browser.includes(scenario.id)) {
      failures.push(`scenario ${scenario.id}: operation ${operation} is missing reciprocal capability browser evidence`)
    }
  }
}

if (failures.length > 0) {
  process.stderr.write(`Capability coverage contract failed:\n${failures.map((failure) => `- ${failure}`).join('\n')}\n`)
  process.exit(1)
}

const totals = Object.fromEntries(proofKinds.map((proof) => [proof, gaps.filter((gap) => gap.proof === proof).length]))
const commands = [...capabilities.values()].filter((capability) => capability.interaction === 'command').length
const requiredTotals = Object.fromEntries(proofKinds.map((proof) => [
  proof,
  [...capabilities.values()].filter((capability) => requiredCapabilityProofs(capability).includes(proof)).length
]))
const summary = `${capabilities.size} operations (${commands} commands), required evidence: success=${requiredTotals.success}, negative=${requiredTotals.negative}, boundary=${requiredTotals.boundary}, browser=${requiredTotals.browser}; gaps: success=${totals.success}, negative=${totals.negative}, boundary=${totals.boundary}, browser=${totals.browser}`
if (gaps.length > 0 && strict) {
  process.stderr.write(`E2E completeness gate failed: ${summary}.\n`)
  for (const gap of gaps) process.stderr.write(`- ${gap.risk} ${gap.interaction} ${gap.id}: missing ${gap.proof}\n`)
  process.exit(1)
}
if (gaps.length > 0) {
  process.stdout.write(`Capability registry valid but INCOMPLETE: ${summary}. Run npm run check:e2e-completeness for the fail-closed gap list.\n`)
} else {
  process.stdout.write(`E2E completeness gate passed: ${summary}.\n`)
}
