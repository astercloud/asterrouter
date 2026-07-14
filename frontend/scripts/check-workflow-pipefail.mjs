import { readdirSync, readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../..')
const workflowsDirectory = resolve(process.env.ASTER_WORKFLOW_ROOT || repositoryRoot, '.github/workflows')
const teePipeline = /\|&?\s*tee\b/
const enablesPipefail = /(?:^|\n)\s*set\s+(?:-o\s+pipefail|-[a-z]*o\s+pipefail)(?:\s|;|$)/
const failures = []

for (const filename of readdirSync(workflowsDirectory).filter((name) => /\.ya?ml$/.test(name)).sort()) {
  const lines = readFileSync(resolve(workflowsDirectory, filename), 'utf8').split(/\r?\n/)
  for (let index = 0; index < lines.length; index += 1) {
    const match = lines[index].match(/^(\s*)run:\s*(.*)$/)
    if (!match) continue

    const runIndent = match[1].length
    let command = match[2]
    if (/^[|>][+-]?$/.test(command)) {
      const block = []
      for (let cursor = index + 1; cursor < lines.length; cursor += 1) {
        const line = lines[cursor]
        if (line.trim() !== '' && line.match(/^\s*/)[0].length <= runIndent) break
        block.push(line)
      }
      command = block.join('\n')
    }

    if (teePipeline.test(command) && !enablesPipefail.test(command)) {
      failures.push(`${filename}:${index + 1}`)
    }
  }
}

if (failures.length > 0) {
  process.stderr.write(`Workflow commands pipe through tee without enabling pipefail: ${failures.join(', ')}\n`)
  process.exit(1)
}

process.stdout.write('Workflow pipeline failure propagation check passed.\n')
