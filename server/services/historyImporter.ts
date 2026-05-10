import type { Database } from '../db/client.js'
import { readdir, stat } from 'node:fs/promises'
import { join } from 'node:path'
import { bulkInsertHistoricalCost } from '../db/agentCostTrendRepo.js'
import { extractSessionInfo, parseJsonlLines, tailRead } from '../jsonlParser.js'
import { CLAUDE_PROJECTS_DIR } from '../paths.js'
import { estimateCost } from '../pricing.js'

const FILE_SIZE_LIMIT = 100 * 1024 * 1024 // 100 MB
const BROADCAST_INTERVAL_MS = 100

export interface ImportProgress {
  total: number
  processed: number
  imported: number
  errors: number
  done: boolean
}

export async function runImport(
  db: Database,
  onProgress: (p: ImportProgress) => void,
): Promise<void> {
  const progress: ImportProgress = { total: 0, processed: 0, imported: 0, errors: 0, done: false }

  const files: string[] = []
  try {
    const projects = await readdir(CLAUDE_PROJECTS_DIR)
    for (const project of projects) {
      const projectDir = join(CLAUDE_PROJECTS_DIR, project)
      const entries = await readdir(projectDir).catch(() => [] as string[])
      for (const entry of entries) {
        if (entry.endsWith('.jsonl'))
          files.push(join(projectDir, entry))
      }
    }
  }
  catch {
    // CLAUDE_PROJECTS_DIR does not exist — proceed with empty file list
  }

  progress.total = files.length
  onProgress({ ...progress })

  let lastBroadcast = Date.now()

  const rows: import('../db/agentCostTrendRepo.js').CostRow[] = []

  try {
    for (const file of files) {
      try {
        const fileStat = await stat(file)
        if (fileStat.size > FILE_SIZE_LIMIT) {
          progress.errors++
          continue
        }
        const raw = await tailRead(file)
        const parsed = parseJsonlLines(raw)
        const info = extractSessionInfo(parsed)
        const { inputTokens, outputTokens } = info.tokenUsage ?? { inputTokens: 0, outputTokens: 0 }
        const tokens = inputTokens + outputTokens
        const cost = estimateCost({ inputTokens, outputTokens }, info.model ?? null)
        const t = Math.floor(fileStat.mtimeMs)
        rows.push({ t, cost, tokens })
        progress.imported++
      }
      catch {
        progress.errors++
      }
      finally {
        progress.processed++
        const now = Date.now()
        if (now - lastBroadcast >= BROADCAST_INTERVAL_MS) {
          onProgress({ ...progress })
          lastBroadcast = now
        }
      }
    }

    bulkInsertHistoricalCost(db, rows)
  }
  finally {
    progress.done = true
    onProgress({ ...progress })
  }
}
