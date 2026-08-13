import { existsSync, readFileSync, writeFileSync } from 'node:fs'
import {
  capabilityRegistryPath,
  extractProductAPIOperations,
  indexOwnerEvidence,
  indexScenarioOperations,
  loadOwnerEvidence,
  loadRegistry
} from './e2e-registry.mjs'

const existing = existsSync(capabilityRegistryPath)
  ? JSON.parse(readFileSync(capabilityRegistryPath, 'utf8'))
  : { capabilities: [] }
const byID = new Map((existing.capabilities || []).map((capability) => [capability.id, capability]))
const browserEvidence = indexScenarioOperations(loadRegistry())
const ownerEvidence = indexOwnerEvidence(loadOwnerEvidence())

function ownerFor(operation) {
  if (operation.module !== 'control') return operation.module
  const symbol = operation.symbol.toLowerCase()
  if (symbol.includes('provider') || symbol.includes('gatewaymodel') || symbol.includes('modelroute')) return 'gateway/supply'
  if (symbol.includes('pricing') || symbol.includes('procurement') || symbol.includes('billing')) return 'billing/pricing'
  if (symbol.includes('apikey') || symbol.includes('application')) return 'applications/credentials'
  if (symbol.includes('department') || symbol.includes('workspaceuser') || symbol.includes('rolebinding') || symbol.includes('organizationgroup')) return 'identity/rbac'
  if (symbol.includes('routing')) return 'gateway/routing'
  if (symbol.includes('artifact')) return 'artifacts'
  if (symbol.includes('aijob')) return 'jobs'
  if (symbol.includes('alert')) return 'alerts'
  if (symbol.includes('export')) return 'exports'
  if (symbol.includes('usage') || symbol.includes('trace') || symbol.includes('audit') || symbol.includes('supply') || symbol.includes('capacity')) return 'operations/observability'
  return 'controlplane'
}

function riskFor(operation) {
  const id = operation.id.toLowerCase()
  if (/auth|account|apikey|rolebinding|system|backup|restore|restart|update|rollback|license|plugin|pricing|billing|provider|modelroute|gatewaymodel/.test(id)) return 'P0'
  if (/delete|disable|revoke|cancel|artifact|job|alert|export|department|organization|routing/.test(id)) return 'P1'
  return 'P2'
}

const capabilities = extractProductAPIOperations().map((operation) => {
  const previous = byID.get(operation.id) || {}
  const browser = browserEvidence.get(operation.id) || []
  const owner = ownerEvidence.get(operation.id) || { success: [], negative: [], boundary: [] }
  return {
    id: operation.id,
    owner: previous.owner || ownerFor(operation),
    risk: previous.risk || riskFor(operation),
    interaction: operation.interaction,
    methods: operation.methods,
    views: operation.views,
    evidence: {
      success: owner.success,
      negative: owner.negative,
      boundary: owner.boundary,
      browser
    },
    notes: previous.notes || ''
  }
})

const registry = {
  schemaVersion: 1,
  generatedFrom: 'frontend Vue @/api imports and exported apiClient call graph',
  coveragePolicy: {
    success: 'Required for every product HTTP operation.',
    negative: 'Required for every command and every P0 query.',
    boundary: 'Required for every command and every P0 query.',
    browser: 'Required for every command and every P0 or P1 query.'
  },
  proofContract: {
    success: 'The owner or product chain returns the intended result and persists applicable state.',
    negative: 'Invalid identity, authorization, dependency, or state is rejected with the public error contract.',
    boundary: 'At least one material edge such as empty, limit, idempotency, concurrency, pagination, or restart is verified.',
    browser: 'A visible product interaction or projection reaches the public HTTP operation in a registered Playwright journey.'
  },
  capabilities
}

writeFileSync(capabilityRegistryPath, `${JSON.stringify(registry, null, 2)}\n`)
process.stdout.write(`Capability registry synchronized: ${capabilities.length} product HTTP operations.\n`)
