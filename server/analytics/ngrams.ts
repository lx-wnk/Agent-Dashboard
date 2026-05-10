import type { Database } from '../db/client.js'
import { readdir, readFile } from 'node:fs/promises'
import { join } from 'node:path'
import { parseJsonlLines } from '../jsonlParser.js'
import { CLAUDE_PROJECTS_DIR } from '../paths.js'

const N = 3

export function extractNgrams(toolSequence: string[]): Map<string, number> {
  const counts = new Map<string, number>()
  for (let i = 0; i <= toolSequence.length - N; i++) {
    const gram = toolSequence.slice(i, i + N).join(' → ')
    counts.set(gram, (counts.get(gram) ?? 0) + 1)
  }
  return counts
}

export async function discoverPatterns(db: Database): Promise<void> {
  const allCounts = new Map<string, number>()

  const projects = await readdir(CLAUDE_PROJECTS_DIR).catch(() => [])
  for (const project of projects) {
    const projectDir = join(CLAUDE_PROJECTS_DIR, project)
    const entries = await readdir(projectDir).catch(() => [])
    for (const entry of entries) {
      if (!entry.endsWith('.jsonl'))
        continue
      try {
        const raw = await readFile(join(projectDir, entry), 'utf8')
        const entries = parseJsonlLines(raw)
        const tools: string[] = []
        for (const e of entries) {
          if (e?.type === 'assistant' && Array.isArray(e?.message?.content)) {
            for (const block of e.message.content) {
              if (block?.type === 'tool_use' && typeof block?.name === 'string')
                tools.push(block.name)
            }
          }
        }
        const grams = extractNgrams(tools)
        for (const [gram, count] of grams)
          allCounts.set(gram, (allCounts.get(gram) ?? 0) + count)
      }
      catch {
        // tolerate partial/unreadable session files
      }
    }
  }

  const top20 = [...allCounts.entries()]
    .sort((a, b) => b[1] - a[1])
    .slice(0, 20)

  const deleteAll = db.prepare('DELETE FROM workflow_patterns')
  const insert = db.prepare('INSERT INTO workflow_patterns (tools, frequency, last_seen_at) VALUES (?, ?, ?)')
  const now = new Date().toISOString()
  db.transaction(() => {
    deleteAll.run()
    for (const [gram, freq] of top20)
      insert.run(gram, freq, now)
  })()
}
