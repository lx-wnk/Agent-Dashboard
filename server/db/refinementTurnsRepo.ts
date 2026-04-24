import type { Database } from 'better-sqlite3'
import { randomUUID } from 'node:crypto'
import { getDb } from './client.js'

export interface RefinementTurn {
  id: string
  taskId: string
  role: 'user' | 'assistant'
  content: string
  phase: string | null
  createdAt: string
}

interface RefinementTurnRow {
  id: string
  task_id: string
  role: string
  content: string
  phase: string | null
  created_at: string
}

function rowToTurn(row: RefinementTurnRow): RefinementTurn {
  return {
    id: row.id,
    taskId: row.task_id,
    role: row.role as 'user' | 'assistant',
    content: row.content,
    phase: row.phase,
    createdAt: row.created_at,
  }
}

export function createTurn(
  input: Omit<RefinementTurn, 'id' | 'createdAt'>,
  db: Database = getDb(),
): RefinementTurn {
  const id = randomUUID()
  const createdAt = new Date().toISOString()
  db.prepare(`
    INSERT INTO refinement_turns (id, task_id, role, content, phase, created_at)
    VALUES (@id, @task_id, @role, @content, @phase, @created_at)
  `).run({
    id,
    task_id: input.taskId,
    role: input.role,
    content: input.content,
    phase: input.phase ?? null,
    created_at: createdAt,
  })
  return {
    id,
    taskId: input.taskId,
    role: input.role,
    content: input.content,
    phase: input.phase ?? null,
    createdAt,
  }
}

export function listTurns(
  taskId: string,
  db: Database = getDb(),
): RefinementTurn[] {
  const rows = db
    .prepare(`
      SELECT id, task_id, role, content, phase, created_at
      FROM refinement_turns
      WHERE task_id = ?
      ORDER BY created_at ASC, rowid ASC
    `)
    .all(taskId) as RefinementTurnRow[]
  return rows.map(rowToTurn)
}
