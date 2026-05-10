import type express from 'express'
import { Router } from 'express'
import { getDb } from '../db/client.js'
import { ImportProgress, runImport } from '../services/historyImporter.js'

type RejectCrossOrigin = (req: express.Request, res: express.Response) => boolean

export interface HistoryRouterDeps {
  rejectCrossOrigin: RejectCrossOrigin
}

// Per-user import job state — keyed by userId so concurrent users don't
// stomp on each other's import progress.
const currentJobs = new Map<string, ImportProgress>()

// Per-user SSE client sets, so broadcasts only reach the triggering user.
const jobClients = new Map<string, Set<(p: ImportProgress) => void>>()

function broadcast(userId: string, p: ImportProgress) {
  const clients = jobClients.get(userId)
  if (!clients)
    return
  for (const cb of clients)
    cb(p)
}

export function createHistoryRouter({ rejectCrossOrigin }: HistoryRouterDeps): ReturnType<typeof Router> {
  const router = Router()

  // Mutating — guard with CSRF check
  router.post('/history/import', (req, res, next) => {
    if (rejectCrossOrigin(req, res))
      return
    next()
  }, async (req, res) => {
    const userId = req.user!.id
    const existing = currentJobs.get(userId)
    if (existing && !existing.done) {
      res.status(409).json({ error: 'Import already in progress' })
      return
    }

    const initial: ImportProgress = { total: 0, processed: 0, imported: 0, errors: 0, done: false }
    currentJobs.set(userId, initial)

    runImport(getDb(), (p) => {
      currentJobs.set(userId, p)
      broadcast(userId, p)
    }).catch(console.error).finally(() => {
      // Ensure done is always set even on unexpected throws
      const job = currentJobs.get(userId)
      if (job && !job.done) {
        const final = { ...job, done: true }
        currentJobs.set(userId, final)
        broadcast(userId, final)
      }
    })

    res.json({ ok: true, message: 'Import started — stream progress at GET /api/history/import/status' })
  })

  router.get('/history/import/status', (req, res) => {
    const userId = req.user!.id

    res.setHeader('Content-Type', 'text/event-stream')
    res.setHeader('Cache-Control', 'no-cache')
    res.setHeader('Connection', 'keep-alive')
    res.setHeader('X-Accel-Buffering', 'no')
    res.flushHeaders()

    const current = currentJobs.get(userId)
    if (current) {
      res.write(`data: ${JSON.stringify(current)}\n\n`)
      if (current.done) {
        res.end()
        return
      }
    }

    let clients = jobClients.get(userId)
    if (!clients) {
      clients = new Set()
      jobClients.set(userId, clients)
    }

    const cb = (p: ImportProgress) => {
      res.write(`data: ${JSON.stringify(p)}\n\n`)
      if (p.done) {
        clients!.delete(cb)
        if (clients!.size === 0)
          jobClients.delete(userId)
        res.end()
      }
    }
    clients.add(cb)

    // Re-check: job may have finished between the initial write and registration
    const recheck = currentJobs.get(userId)
    if (recheck?.done) {
      res.write(`data: ${JSON.stringify(recheck)}\n\n`)
      clients.delete(cb)
      if (clients.size === 0)
        jobClients.delete(userId)
      res.end()
      return
    }

    req.on('close', () => {
      const set = jobClients.get(userId)
      if (set) {
        set.delete(cb)
        if (set.size === 0)
          jobClients.delete(userId)
      }
    })
  })

  return router
}
