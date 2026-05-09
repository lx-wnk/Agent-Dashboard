import type { Agent, PipelineTask } from '../../src/types.js'
import { Router } from 'express'
import { getDb } from '../db/client.js'
import { getTaskById } from '../db/tasksRepo.js'

interface SearchDeps {
  getAgents: () => Agent[]
}

const WHITESPACE_RE = /\s+/

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
    const q = (typeof req.query.q === 'string' ? req.query.q : '').trim()
    const type = (typeof req.query.type === 'string' ? req.query.type : 'all')
    const limit = Math.min(50, Math.max(1, Number.parseInt(String(req.query.limit ?? '20'), 10) || 20))

    if (!q) {
      res.json({ tasks: [], agents: [] })
      return
    }

    const db = getDb()
    const user = req.user!

    // FTS5 task search
    const tasks: PipelineTask[] = []
    if (type === 'tasks' || type === 'all') {
      const rows = db.prepare(`
        SELECT task_id FROM task_fts
        WHERE task_fts MATCH ?
        ORDER BY rank
        LIMIT ?
      `).all(sanitizeFtsQuery(q), limit) as Array<{ task_id: string }>
      tasks.push(
        ...rows
          .map(r => getTaskById(r.task_id))
          .filter((t): t is PipelineTask => t !== null),
      )
    }

    const scopedTasks = user.isAdmin
      ? tasks
      : tasks.filter(t => t.userId === null || t.userId === user.id)

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

    res.json({ tasks: scopedTasks, agents })
  })

  return router
}
