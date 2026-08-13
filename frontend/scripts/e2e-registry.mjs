import { existsSync, readFileSync, readdirSync } from 'node:fs'
import { dirname, relative, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'
import ts from 'typescript'

const scriptDirectory = dirname(fileURLToPath(import.meta.url))
export const frontendRoot = resolve(scriptDirectory, '..')
export const repositoryRoot = resolve(frontendRoot, '..')
export const registryPath = resolve(repositoryRoot, 'docs/test/v1/scenario-registry.json')
export const capabilityRegistryPath = resolve(repositoryRoot, 'docs/test/v1/capability-registry.json')
export const ownerEvidencePath = resolve(repositoryRoot, 'docs/test/v1/owner-evidence.json')

export function loadRegistry() {
  return JSON.parse(readFileSync(registryPath, 'utf8'))
}

export function loadCapabilityRegistry() {
  return JSON.parse(readFileSync(capabilityRegistryPath, 'utf8'))
}

export function loadOwnerEvidence() {
  return JSON.parse(readFileSync(ownerEvidencePath, 'utf8'))
}

function property(object, name) {
  return object.properties.find((candidate) => {
    if (!ts.isPropertyAssignment(candidate)) return false
    return (ts.isIdentifier(candidate.name) || ts.isStringLiteral(candidate.name)) && candidate.name.text === name
  })
}

function stringValue(node) {
  return ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node) ? node.text : null
}

function joinRoute(parent, child) {
  if (child.startsWith('/')) return child
  if (!parent || parent === '/') return `/${child}`
  return `${parent.replace(/\/$/, '')}/${child}`
}

function collectRouteObjects(array, parent, paths) {
  for (const element of array.elements) {
    if (!ts.isObjectLiteralExpression(element)) continue
    const pathProperty = property(element, 'path')
    if (!pathProperty) continue
    const segment = stringValue(pathProperty.initializer)
    if (segment === null || segment.includes(':pathMatch')) continue
    const path = joinRoute(parent, segment)
    const childrenProperty = property(element, 'children')
    if (childrenProperty && ts.isArrayLiteralExpression(childrenProperty.initializer)) {
      collectRouteObjects(childrenProperty.initializer, path, paths)
      continue
    }
    if (property(element, 'component') && !property(element, 'redirect')) paths.add(path)
  }
}

export function extractRouterPaths() {
  const routerFile = resolve(frontendRoot, 'src/router/index.ts')
  const source = ts.createSourceFile(
    routerFile,
    readFileSync(routerFile, 'utf8'),
    ts.ScriptTarget.Latest,
    true,
    ts.ScriptKind.TS
  )
  let routesArray = null
  function visit(node) {
    if (routesArray) return
    if (ts.isCallExpression(node) && ts.isIdentifier(node.expression) && node.expression.text === 'createRouter') {
      const options = node.arguments[0]
      if (options && ts.isObjectLiteralExpression(options)) {
        const routes = property(options, 'routes')
        if (routes && ts.isArrayLiteralExpression(routes.initializer)) routesArray = routes.initializer
      }
    }
    ts.forEachChild(node, visit)
  }
  visit(source)
  if (!routesArray) throw new Error(`Unable to find createRouter routes in ${routerFile}`)
  const paths = new Set()
  collectRouteObjects(routesArray, '', paths)
  return [...paths].sort()
}

function collectSpecPaths(directory, paths) {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = resolve(directory, entry.name)
    if (entry.isDirectory()) collectSpecPaths(path, paths)
    else if (entry.isFile() && entry.name.endsWith('.spec.ts')) paths.push(relative(frontendRoot, path).split(sep).join('/'))
  }
}

export function listE2ESpecPaths() {
  const paths = []
  collectSpecPaths(resolve(frontendRoot, 'e2e'), paths)
  return paths.sort()
}

function collectVuePaths(directory, paths) {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = resolve(directory, entry.name)
    if (entry.isDirectory()) collectVuePaths(path, paths)
    else if (entry.isFile() && entry.name.endsWith('.vue')) paths.push(path)
  }
}

const httpMethods = new Set(['get', 'post', 'put', 'patch', 'delete'])

function exportedAPIFunctions(moduleName) {
  const apiPath = resolve(frontendRoot, `src/api/${moduleName}.ts`)
  if (!existsSync(apiPath)) return new Map()
  const source = ts.createSourceFile(apiPath, readFileSync(apiPath, 'utf8'), ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
  const functions = new Map()
  const exported = new Set()

  for (const statement of source.statements) {
    if (!ts.isFunctionDeclaration(statement) || !statement.name || !statement.body) continue
    functions.set(statement.name.text, statement)
    if (statement.modifiers?.some((modifier) => modifier.kind === ts.SyntaxKind.ExportKeyword)) exported.add(statement.name.text)
  }

  function methodsFor(name, visiting = new Set()) {
    if (visiting.has(name)) return new Set()
    const declaration = functions.get(name)
    if (!declaration) return new Set()
    visiting.add(name)
    const methods = new Set()
    function visit(node) {
      if (ts.isCallExpression(node)) {
        const expression = node.expression
        if (
          ts.isPropertyAccessExpression(expression) &&
          ts.isIdentifier(expression.expression) &&
          expression.expression.text === 'apiClient' &&
          httpMethods.has(expression.name.text)
        ) {
          methods.add(expression.name.text)
        } else if (ts.isIdentifier(expression) && functions.has(expression.text)) {
          for (const method of methodsFor(expression.text, new Set(visiting))) methods.add(method)
        }
      }
      ts.forEachChild(node, visit)
    }
    visit(declaration.body)
    return methods
  }

  return new Map([...exported]
    .map((name) => [name, [...methodsFor(name)].sort()])
    .filter(([, methods]) => methods.length > 0))
}

export function extractProductAPIOperations() {
  const vuePaths = []
  collectVuePaths(resolve(frontendRoot, 'src'), vuePaths)
  const operations = new Map()

  for (const vuePath of vuePaths) {
    const body = readFileSync(vuePath, 'utf8')
    const script = body.match(/<script\s+setup(?:\s+lang=(?:"ts"|'ts'))?[^>]*>([\s\S]*?)<\/script>/)?.[1]
    if (!script) continue
    const source = ts.createSourceFile(`${vuePath}.ts`, script, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
    for (const statement of source.statements) {
      if (!ts.isImportDeclaration(statement) || !ts.isStringLiteral(statement.moduleSpecifier)) continue
      const specifier = statement.moduleSpecifier.text
      if (!specifier.startsWith('@/api/')) continue
      const moduleName = specifier.slice('@/api/'.length)
      const apiFunctions = exportedAPIFunctions(moduleName)
      const bindings = statement.importClause?.namedBindings
      if (!bindings || !ts.isNamedImports(bindings)) continue
      for (const element of bindings.elements) {
        const symbol = (element.propertyName || element.name).text
        const methods = apiFunctions.get(symbol)
        if (!methods) continue
        const id = `${moduleName}:${symbol}`
        const interaction = /^(get|list)/.test(symbol) || symbol === 'checkSystemUpdates' ? 'query' : 'command'
        const current = operations.get(id) || {
          id,
          module: moduleName,
          symbol,
          methods,
          interaction,
          views: []
        }
        current.views.push(relative(repositoryRoot, vuePath).split(sep).join('/'))
        operations.set(id, current)
      }
    }
  }

  return [...operations.values()]
    .map((operation) => ({ ...operation, views: [...new Set(operation.views)].sort() }))
    .sort((left, right) => left.id.localeCompare(right.id))
}

export function requiredCapabilityProofs(capability) {
  const proofs = new Set(['success'])
  const isCommand = capability.interaction === 'command'
  if (isCommand || capability.risk === 'P0') {
    proofs.add('negative')
    proofs.add('boundary')
  }
  if (isCommand || capability.risk === 'P0' || capability.risk === 'P1') proofs.add('browser')
  return [...proofs]
}

export function indexScenarioOperations(registry) {
  const evidence = new Map()
  for (const scenario of registry.scenarios || []) {
    if (!['journey', 'setup'].includes(scenario.kind)) continue
    for (const operation of scenario.operations || []) {
      const scenarios = evidence.get(operation) || []
      scenarios.push(scenario.id)
      evidence.set(operation, scenarios)
    }
  }
  return new Map([...evidence].map(([operation, scenarios]) => [operation, [...new Set(scenarios)].sort()]))
}

export function indexOwnerEvidence(registry) {
  const evidence = new Map()
  for (const entry of registry.evidence || []) {
    for (const operation of entry.operations || []) {
      const proofs = evidence.get(operation) || { success: [], negative: [], boundary: [] }
      for (const proof of entry.proofs || []) proofs[proof]?.push(entry.reference)
      evidence.set(operation, proofs)
    }
  }
  return new Map([...evidence].map(([operation, proofs]) => [operation, Object.fromEntries(
    Object.entries(proofs).map(([proof, references]) => [proof, [...new Set(references)].sort()])
  )]))
}

function testCallModifier(expression) {
  if (ts.isIdentifier(expression) && expression.text === 'test') return 'test'
  if (!ts.isPropertyAccessExpression(expression) || !ts.isIdentifier(expression.expression) || expression.expression.text !== 'test') return null
  return ['only', 'skip', 'fixme', 'fail'].includes(expression.name.text) ? expression.name.text : null
}

export function extractPlaywrightTests(specRelativePath) {
  const specPath = resolve(frontendRoot, specRelativePath)
  const source = ts.createSourceFile(
    specPath,
    readFileSync(specPath, 'utf8'),
    ts.ScriptTarget.Latest,
    true,
    ts.ScriptKind.TS
  )
  const tests = []
  function visit(node) {
    if (ts.isCallExpression(node)) {
      const modifier = testCallModifier(node.expression)
      const title = node.arguments[0] && stringValue(node.arguments[0])
      if (modifier && (modifier === 'test' || title)) {
        tests.push({ modifier, title, tags: title?.match(/@e2e-[a-z0-9-]+/g) || [] })
      }
    }
    ts.forEachChild(node, visit)
  }
  visit(source)
  return tests
}

export function grepPattern(scenarios) {
  return scenarios
    .map((scenario) => `(?:^|\\s)${scenario.id.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}(?=\\s|$)`)
    .join('|')
}
