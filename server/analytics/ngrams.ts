import type { Database } from '../db/client.js'
import { readdir, stat } from 'node:fs/promises'
import { join } from 'node:path'
import { tailRead } from '../jsonlParser.js'
import { parseJsonlLines } from '../jsonlParser.js'
import { CLAUDE_PROJECTS_DIR } from '../paths.js'

const N = 3

// Valid tool name: starts with a letter, then letters/digits/underscores/hyphens, max 64 chars
const TOOL_NAME_RE = /^[A-Za-z][A-Za-z0-9_-]{0,63}$/

// 10 MB per-file cap for pattern discovery — keeps startup overhead bounded
const MAX_FILE_SIZE = 10 * 1024 * 1024

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

  const projects = await readdir(CLAUDE_PROJECTS_DIR).catch(() => [] as string[])
  for (const project of projects) {
    const projectDir = join(CLAUDE_PROJECTS_DIR, project)
    // Use withFileTypes to skip symlinks — only process regular files
    const dirents = await readdir(projectDir, { withFileTypes: true }).catch(() => [])
    for (const dirent of dirents) {
      if (!dirent.isFile() || !dirent.name.endsWith('.jsonl'))
        continue
      const filePath = join(projectDir, dirent.name)
      try {
        const fileStat = await stat(filePath)
        if (fileStat.size > MAX_FILE_SIZE)
          continue
        const raw = await tailRead(filePath)
        const parsed = parseJsonlLines(raw)
        const tools: string[] = []
        for (const e of parsed) {
          if (e?.type === 'assistant' && Array.isArray(e?.message?.content)) {
            for (const block of e.message.content) {
              if (
                block?.type === 'tool_use'
                && typeof block?.name === 'string'
                && TOOL_NAME_RE.test(block.name)
              ) {
                tools.push(block.name)
              }
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
