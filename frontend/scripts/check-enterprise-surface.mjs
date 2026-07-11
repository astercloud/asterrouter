import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'

const root = new URL('..', import.meta.url).pathname
const srcRoot = join(root, 'src')
const blockedTerms = ['利润', '套利', '倍率', '补池', '采集', '站点发现', '邮箱验证码', '代理出口', '账号池']
const textExtensions = new Set(['.vue', '.ts'])
const allowedFiles = new Set([
  'types.ts'
])

function extensionOf(file) {
  const index = file.lastIndexOf('.')
  return index >= 0 ? file.slice(index) : ''
}

function walk(dir) {
  const files = []
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry)
    const stat = statSync(path)
    if (stat.isDirectory()) {
      files.push(...walk(path))
    } else if (textExtensions.has(extensionOf(path))) {
      files.push(path)
    }
  }
  return files
}

const findings = []
for (const file of walk(srcRoot)) {
  const rel = relative(srcRoot, file)
  if (allowedFiles.has(rel)) continue
  const lines = readFileSync(file, 'utf8').split(/\r?\n/)
  lines.forEach((line, index) => {
    for (const term of blockedTerms) {
      if (line.includes(term)) {
        findings.push(`${rel}:${index + 1}: contains forbidden Enterprise surface term "${term}"`)
      }
    }
  })
}

if (findings.length > 0) {
  console.error(findings.join('\n'))
  process.exit(1)
}

console.log('Enterprise surface wording check passed.')
