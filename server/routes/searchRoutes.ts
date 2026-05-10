import type { Agent, PipelineTask } from '../../src/types.js'
import { Router } from 'express'
import { getDb } from '../db/client.js'
import type { TaskRow } from '../db/rowMappers.js'
import { rowToTask } from '../db/rowMappers.js'
import { WHITESPACE_RE } from '../paths.js'

interface SearchDeps {
  getAgents: () => Agent[]
}

function sanitizeFtsQuery(raw: string): string {
  return raw
    .trim()
    .split(WHITESPACE_RE)
    .filter(Boolean)
    .map(token => `"${token.replace(/"/g, '""')}"*`)
    .join(' ')
}

export function createSearchRouter({ getAgents }: SearchDeps): ReturnType<typeof Router> {
  const router = Router()

  router.get('/search', (req, res) => {
    if (!req.user) {
      res.status(401).json({ error: 'Unauthorized' })
      return
    }

    const q = (typeof req.query.q === 'string' ? req.query.q : '').slice(0, 200).trim()
    const type = (typeof req.query.type === 'string' ? req.query.type : 'all')
    const limit = Math.min(50, Math.max(1, Number.parseInt(String(req.query.limit ?? '20'), 10) || 20))

    if (!q) {
      res.json({ tasks: [], agents: [] })
      return
    }

    const db = getDb()
    const user = req.user

    // FTS5 task search — subquery approach avoids rank-column ambiguity that
    // occurs when task_fts is JOINed with tasks. The FTS subquery resolves
    // `rank` in its own scope; the outer query applies user-scoping.
    const tasks: PipelineTask[] = []
    if (type === 'tasks' || type === 'all') {
      try {
        const rows = db.prepare(`
          SELECT tasks.*
          FROM tasks
          WHERE id IN (
            SELECT task_id FROM task_fts
            WHERE task_fts MATCH @query
            ORDER BY rank
            LIMIT @ftsLimit
          )
            AND (user_id IS NULL OR user_id = @userId OR @isAdmin = 1)
          LIMIT @limit
        `).all({
          query: sanitizeFtsQuery(q),
          ftsLimit: limit * 3,
          userId: user.id,
          isAdmin: user.isAdmin ? 1 : 0,
          limit,
        }) as TaskRow[]
        tasks.push(...rows.map(rowToTask))
      }
      catch {
        // FTS5 query parse errors (e.g. invalid syntax) — return empty results
      }
    }

    // In-memory agent search — agents are local/shared; only admins see them
    // in multi-user setups to avoid leaking other users' agent activity.
    let agents: Agent[] = []
    if ((type === 'agents' || type === 'all') && user.isAdmin) {
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
