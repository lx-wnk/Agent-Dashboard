import type { Agent } from '../../src/types.js'
import { Router } from 'express'
import { getDb } from '../db/client.js'
import { getTaskById } from '../db/tasksRepo.js'

interface SearchDeps {
  getAgents: () => Agent[]
}

export function createSearchRouter({ getAgents }: SearchDeps): ReturnType<typeof Router> {
  const router = Router()

  router.get('/search', (req, res) => {
    const q = ((req.query.q as string) ?? '').trim()
    const type = (req.query.type as string) ?? 'all'
    const limit = Math.min(50, Number(req.query.limit ?? 20))

    if (!q) {
      res.json({ tasks: [], agents: [] })
      return
    }

    const db = getDb()

    // FTS5 task search
    let tasks: ReturnType<typeof getTaskById>[] = []
    if (type === 'tasks' || type === 'all') {
      try {
        const rows = db.prepare(`
          SELECT task_id FROM task_fts
          WHERE task_fts MATCH ?
          ORDER BY rank
          LIMIT ?
        `).all(`${q}*`, limit) as Array<{ task_id: string }>
        tasks = rows.map(r => getTaskById(r.task_id)).filter(Boolean)
      }
      catch {
        // FTS match syntax error — return empty rather than 500
      }
    }

    // In-memory agent search
    let agents: Agent[] = []
    if (type === 'agents' || type === 'all') {
      const ql = q.toLowerCase()
      agents = getAgents()
        .filter(a =>
          a.projectName.toLowerCase().includes(ql)
          || (a.currentAction ?? '').toLowerCase().includes(ql)
          || a.cwd.toLowerCase().includes(ql),
        )
        .slice(0, limit)
    }

    res.json({ tasks, agents })
  })

  return router
}
