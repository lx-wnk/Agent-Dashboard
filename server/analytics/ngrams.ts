import type { Database } from '../db/client.js'
import { readdir } from 'node:fs/promises'
import { join } from 'node:path'
import { parseFullSession } from '../jsonlParser.js'
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
      const sessionId = entry.replace('.jsonl', '')
      try {
        const messages = await parseFullSession(sessionId, false)
        const tools = messages.filter(m => m.role === 'tool_call').map(m => m.toolName ?? 'unknown')
        const grams = extractNgrams(tools)
        for (const [gram, count] of grams)
          allCounts.set(gram, (allCounts.get(gram) ?? 0) + count)
      }
      catch {
        // Skip unreadable sessions
      }
    }
  }

  const top20 = [...allCounts.entries()]
    .sort((a, b) => b[1] - a[1])
    .slice(0, 20)

  const upsert = db.prepare(`
    INSERT INTO workflow_patterns (tools, frequency, last_seen_at)
    VALUES (?, ?, ?)
    ON CONFLICT(tools) DO UPDATE SET
      frequency = excluded.frequency,
      last_seen_at = excluded.last_seen_at
  `)
  const now = new Date().toISOString()
  for (const [gram, freq] of top20)
    upsert.run(gram, freq, now)
}
