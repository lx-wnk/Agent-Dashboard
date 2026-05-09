import { readdir, stat } from 'node:fs/promises'
import { join } from 'node:path'
import { Router } from 'express'
import { getDb } from '../db/client.js'
import { parseFullSession } from '../jsonlParser.js'
import { CLAUDE_PROJECTS_DIR } from '../paths.js'

interface ImportProgress {
  total: number
  processed: number
  imported: number
  errors: number
  done: boolean
}

let currentJob: ImportProgress | null = null
const jobClients = new Set<(p: ImportProgress) => void>()

function broadcast(p: ImportProgress) {
  for (const cb of jobClients)
    cb(p)
}

async function runImport() {
  const db = getDb()
  const insert = db.prepare(
    'INSERT OR IGNORE INTO agent_cost_trend (t, cost, tokens) VALUES (?, ?, ?)',
  )

  const files: string[] = []
  try {
    const projects = await readdir(CLAUDE_PROJECTS_DIR)
    for (const project of projects) {
      const projectDir = join(CLAUDE_PROJECTS_DIR, project)
      const entries = await readdir(projectDir).catch(() => [])
      for (const entry of entries) {
        if (entry.endsWith('.jsonl'))
          files.push(join(projectDir, entry))
      }
    }
  }
  catch {
    // Projects dir may not exist
  }

  currentJob = { total: files.length, processed: 0, imported: 0, errors: 0, done: false }
  broadcast({ ...currentJob })

  for (const file of files) {
    const sessionId = file.split('/').pop()?.replace('.jsonl', '') ?? ''
    try {
      const fileStat = await stat(file)
      const messages = await parseFullSession(sessionId, false)
      const t = fileStat.mtimeMs
      const tokens = messages.length
      insert.run(Math.floor(t), 0, tokens)
      currentJob!.imported++
    }
    catch {
      currentJob!.errors++
    }
    finally {
      currentJob!.processed++
      broadcast({ ...currentJob! })
    }
  }
  currentJob!.done = true
  broadcast({ ...currentJob! })
}

export function createHistoryRouter(): ReturnType<typeof Router> {
  const router = Router()

  router.post('/history/import', async (_req, res) => {
    if (currentJob && !currentJob.done) {
      res.status(409).json({ error: 'Import already in progress' })
      return
    }
    runImport().catch(console.error)
    res.json({ ok: true, message: 'Import started — stream progress at GET /api/history/import/status' })
  })

  router.get('/history/import/status', (req, res) => {
    res.setHeader('Content-Type', 'text/event-stream')
    res.setHeader('Cache-Control', 'no-cache')
    res.setHeader('Connection', 'keep-alive')
    res.flushHeaders()

    if (currentJob) {
      res.write(`data: ${JSON.stringify(currentJob)}\n\n`)
      if (currentJob.done) {
        res.end()
        return
      }
    }

    const cb = (p: ImportProgress) => {
      res.write(`data: ${JSON.stringify(p)}\n\n`)
      if (p.done) {
        jobClients.delete(cb)
        res.end()
      }
    }
    jobClients.add(cb)
    req.on('close', () => jobClients.delete(cb))
  })

  return router
}
