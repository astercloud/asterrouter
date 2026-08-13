import assert from 'node:assert/strict'
import test from 'node:test'
import { extractProductAPIOperations, indexOwnerEvidence, indexScenarioOperations, requiredCapabilityProofs } from './e2e-registry.mjs'

test('extractProductAPIOperations returns only exported HTTP operations with transitive methods', () => {
  const operations = new Map(extractProductAPIOperations().map((operation) => [operation.id, operation]))

  assert.equal(operations.has('client:ApiClientError'), false)
  assert.equal(operations.has('client:isNotFoundError'), false)
  assert.deepEqual(operations.get('account:beginAccountIdentityBinding')?.methods, ['post'])
  assert.equal(operations.get('account:beginAccountIdentityBinding')?.interaction, 'command')
  assert.deepEqual(operations.get('control:getUsageReport')?.methods, ['get'])
  assert.equal(operations.get('control:getUsageReport')?.interaction, 'query')
  assert.deepEqual(operations.get('system:downloadSystemBackup')?.methods, ['get'])
  assert.equal(operations.get('system:downloadSystemBackup')?.interaction, 'command')
})

test('requiredCapabilityProofs keeps browser and owner evidence proportional to risk', () => {
  assert.deepEqual(requiredCapabilityProofs({ interaction: 'query', risk: 'P2' }), ['success'])
  assert.deepEqual(requiredCapabilityProofs({ interaction: 'query', risk: 'P1' }), ['success', 'browser'])
  assert.deepEqual(requiredCapabilityProofs({ interaction: 'query', risk: 'P0' }), ['success', 'negative', 'boundary', 'browser'])
  assert.deepEqual(requiredCapabilityProofs({ interaction: 'command', risk: 'P2' }), ['success', 'negative', 'boundary', 'browser'])
})

test('indexScenarioOperations derives browser evidence only from vertical journeys', () => {
  const evidence = indexScenarioOperations({
    scenarios: [
      { id: '@e2e-surface-001', kind: 'surface', operations: ['control:getDashboard'] },
      { id: '@e2e-journey-b', kind: 'journey', operations: ['control:getDashboard', 'control:createAPIKey'] },
      { id: '@e2e-journey-a', kind: 'journey', operations: ['control:getDashboard'] },
      { id: '@e2e-setup-001', kind: 'setup', operations: ['settings:completeEnterpriseSetup'] }
    ]
  })

  assert.deepEqual(evidence.get('control:getDashboard'), ['@e2e-journey-a', '@e2e-journey-b'])
  assert.deepEqual(evidence.get('control:createAPIKey'), ['@e2e-journey-b'])
  assert.deepEqual(evidence.get('settings:completeEnterpriseSetup'), ['@e2e-setup-001'])
})

test('indexOwnerEvidence expands grouped owner proofs without duplicates', () => {
  const evidence = indexOwnerEvidence({
    evidence: [
      { reference: 'backend/example_test.go#TestLifecycle', proofs: ['success', 'boundary'], operations: ['control:createAPIKey'] },
      { reference: 'backend/example_test.go#TestLifecycle', proofs: ['success'], operations: ['control:createAPIKey'] },
      { reference: 'backend/example_test.go#TestDenied', proofs: ['negative'], operations: ['control:createAPIKey', 'control:getAPIKeys'] }
    ]
  })

  assert.deepEqual(evidence.get('control:createAPIKey'), {
    success: ['backend/example_test.go#TestLifecycle'],
    negative: ['backend/example_test.go#TestDenied'],
    boundary: ['backend/example_test.go#TestLifecycle']
  })
  assert.deepEqual(evidence.get('control:getAPIKeys')?.negative, ['backend/example_test.go#TestDenied'])
})
