import { readdirSync, readFileSync, statSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, extname, join, relative, resolve } from 'node:path'

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const repositoryRoot = resolve(frontendRoot, '..')
const scanRoots = ['.github/workflows', 'backend', 'frontend/src', 'frontend/e2e', 'deploy', 'scripts']
const textExtensions = new Set(['.go', '.js', '.mjs', '.sql', '.sh', '.ts', '.vue', '.yaml', '.yml'])
const currentScript = 'frontend/scripts/check-enterprise-surface.mjs'
const generatedDirectories = new Set([
  join(repositoryRoot, 'backend/data'),
  join(repositoryRoot, 'frontend/dist'),
  join(repositoryRoot, 'frontend/node_modules')
])

const legacyRouteGuards = new Set([
  'backend/internal/server/server_test.go',
  'backend/internal/server/rbac_test.go',
  'frontend/src/router/index.test.ts'
])

const legacyPricingGuards = new Set([
  'backend/internal/controlplane/pricing_service_test.go',
  'backend/internal/server/pricing_rule_routes_test.go'
])

const rules = [
  {
    name: 'historical configuration or scope',
    pattern: /enabled_profiles|default_profile|deployment_role|ASTERROUTER_DEPLOYMENT_ROLE|ProfileScope|profile_scope|profileScope|AllowedProfile|allowed_profile_scope|TierProfileBundle|profile_bundle|relay_operator/g
  },
  {
    name: 'removed API route',
    pattern: /\/api\/v1\/(?:admin|operator|customer|platform)(?:\/|["'`])/g,
    allowedFiles: legacyRouteGuards
  },
  {
    name: 'removed product route',
    pattern: /["'`]\/(?:admin|operator|customer|platform)(?:\/|["'`])/g,
    allowedFiles: legacyRouteGuards
  },
  {
    name: 'removed product terminology',
    pattern: /\b(?:Personal|Relay Operator|relay-operator)\b|中转运营|个人模式|个人工作台|个人网关|四种产品形态|四种产品模式/g
  },
  {
    name: 'retired application model naming',
    pattern: /PlatformTenant|platform_tenant|platform_tenants|CreatePlatformTenant|UpdatePlatformTenant|ListPlatformTenants|SavePlatformTenant|EnsurePlatformBootstrap|TenantID|TenantName|tenant_id|tenant_name|\bptn_|\bgpr_/g
  },
  {
    name: 'removed operator or customer schema',
    pattern: /operator_customers|operator_balance_entries|customer_charge|operator_plan/g,
    allowedFiles: legacyPricingGuards
  },
  {
    name: 'removed customer ownership field',
    pattern: /customer_id/g,
    filesUnder: 'backend/internal/controlplane'
  },
  {
    name: 'removed customer ownership field',
    pattern: /customer_id/g,
    filesUnder: 'frontend/src/views'
  },
  {
    name: 'removed surface abstraction',
    pattern: /SurfaceShell|surfaces_json|PluginFrontendContributionSurface|@surface-smoke/g
  },
  {
    name: 'removed plugin contribution contract',
    pattern: /frontend\/contribution|contribution\.json|plugin-frontend-contribution|console\.plugins/g
  },
  {
    name: 'removed i18n namespace',
    pattern: /^\s*(?:customer|platform|operator|operatorCrud|operatorDomain)\s*:\s*\{/gm,
    files: new Set(['frontend/src/i18n/locales/en-US.ts', 'frontend/src/i18n/locales/zh-CN.ts'])
  },
  {
    name: 'relay operations wording',
    pattern: /利润|套利|补池|站点发现|邮箱验证码|代理出口|账号池/g,
    filesUnder: 'frontend/src'
  }
]

function extensionOf(file) {
  return extname(file)
}

function walk(dir) {
  const files = []
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry)
    const stat = statSync(path)
    if (stat.isDirectory()) {
      if (generatedDirectories.has(path)) continue
      files.push(...walk(path))
    } else if (textExtensions.has(extensionOf(path))) {
      files.push(path)
    }
  }
  return files
}

function lineNumberAt(content, offset) {
  return content.slice(0, offset).split('\n').length
}

const files = scanRoots.flatMap((root) => walk(join(repositoryRoot, root)))
const findings = []
for (const file of files) {
  const rel = relative(repositoryRoot, file)
  if (rel === currentScript) continue
  const content = readFileSync(file, 'utf8')
  for (const rule of rules) {
    if (rule.files && !rule.files.has(rel)) continue
    if (rule.filesUnder && !rel.startsWith(`${rule.filesUnder}/`)) continue
    if (rule.allowedFiles?.has(rel)) continue

    rule.pattern.lastIndex = 0
    for (const match of content.matchAll(rule.pattern)) {
      findings.push(`${rel}:${lineNumberAt(content, match.index)}: ${rule.name}: ${JSON.stringify(match[0])}`)
    }
  }
}

if (findings.length > 0) {
  console.error(findings.join('\n'))
  process.exit(1)
}

console.log(`Enterprise product boundary check passed (${files.length} files).`)
