import type { Database } from 'better-sqlite3'
import type { PipelineStage, StageRun, StageRunStatus } from '../../src/types.js'
import type { StageRunRow } from './rowMappers.js'
import { randomUUID } from 'node:crypto'
import { getDb } from './client.js'
import { rowToStageRun } from './rowMappers.js'

export interface CreateStageRunInput {
  taskId: string
  stage: PipelineStage
  iteration?: number
  sessionName?: string | null
}

export interface UpdateStageRunInput {
  sessionId?: string | null
  sessionName?: string | null
  pid?: number | null
  status?: StageRunStatus
  startedAt?: string | null
  endedAt?: string | null
  output?: Record<string, unknown> | null
  tokensUsed?: number
  costCents?: number
}

export function createStageRun(input: CreateStageRunInput, db: Database = getDb()): StageRun {
  const id = randomUUID()
  db.prepare(`
    INSERT INTO stage_runs (id, task_id, stage, session_name, status, iteration)
    VALUES (@id, @task_id, @stage, @session_name, 'pending', @iteration)
  `).run({
    id,
    task_id: input.taskId,
    stage: input.stage,
    session_name: input.sessionName ?? null,
    iteration: input.iteration ?? 0,
  })
  return getStageRunById(id, db)!
}

export function getStageRunById(id: string, db: Database = getDb()): StageRun | null {
  const row = db.prepare('SELECT * FROM stage_runs WHERE id = ?').get(id) as StageRunRow | undefined
  return row ? rowToStageRun(row) : null
}

export function listStageRunsForTask(taskId: string, db: Database = getDb()): StageRun[] {
  const rows = db
    .prepare('SELECT * FROM stage_runs WHERE task_id = ? ORDER BY started_at, iteration')
    .all(taskId) as StageRunRow[]
  return rows.map(rowToStageRun)
}

export function getLatestStageRun(
  taskId: string,
  stage: PipelineStage,
  db: Database = getDb(),
): StageRun | null {
  const row = db
    .prepare(`
      SELECT * FROM stage_runs
      WHERE task_id = ? AND stage = ?
      ORDER BY iteration DESC LIMIT 1
    `)
    .get(taskId, stage) as StageRunRow | undefined
  return row ? rowToStageRun(row) : null
}

/**
 * Get the most recently created stage_run for a task, regardless of stage.
 * Used for board enrichment to reflect awaiting_user/on_hold status.
 *
 * Ordering priority (highest iteration first, then most recently started):
 * this matters when an `iterate` transition has just inserted a new pending
 * row — the new iter N+1 has `started_at = NULL` but should still outrank
 * the old iter N row whose started_at is set.
 *
 * Uses `(started_at IS NULL)` instead of `NULLS LAST` for portability
 * across all SQLite versions (NULLS LAST requires ≥3.30).
 */
export function getLatestStageRunForTask(taskId: string, db: Database = getDb()): StageRun | null {
  const row = db
    .prepare(`
      SELECT * FROM stage_runs
      WHERE task_id = ?
      ORDER BY iteration DESC,
               (started_at IS NULL),
               started_at DESC,
               rowid DESC
      LIMIT 1
    `)
    .get(taskId) as StageRunRow | undefined
  return row ? rowToStageRun(row) : null
}

export function findStageRunBySessionId(sessionId: string, db: Database = getDb()): StageRun | null {
  const row = db
    .prepare('SELECT * FROM stage_runs WHERE session_id = ? LIMIT 1')
    .get(sessionId) as StageRunRow | undefined
  return row ? rowToStageRun(row) : null
}

export function listRunningStageRuns(db: Database = getDb()): StageRun[] {
  const rows = db
    .prepare(`SELECT * FROM stage_runs WHERE status IN ('running', 'awaiting_user', 'on_hold')`)
    .all() as StageRunRow[]
  return rows.map(rowToStageRun)
}

export function updateStageRun(
  id: string,
  input: UpdateStageRunInput,
  db: Database = getDb(),
): StageRun | null {
  const existing = getStageRunById(id, db)
  if (!existing)
    return null

  const updates: string[] = []
  const params: Record<string, unknown> = { id }

  if (input.sessionId !== undefined) {
    updates.push('session_id = @session_id')
    params.session_id = input.sessionId
  }
  if (input.sessionName !== undefined) {
    updates.push('session_name = @session_name')
    params.session_name = input.sessionName
  }
  if (input.pid !== undefined) {
    updates.push('pid = @pid')
    params.pid = input.pid
  }
  if (input.status !== undefined) {
    updates.push('status = @status')
    params.status = input.status
  }
  if (input.startedAt !== undefined) {
    updates.push('started_at = @started_at')
    params.started_at = input.startedAt
  }
  if (input.endedAt !== undefined) {
    updates.push('ended_at = @ended_at')
    params.ended_at = input.endedAt
  }
  if (input.output !== undefined) {
    updates.push('output = @output')
    params.output = input.output ? JSON.stringify(input.output) : null
  }
  if (input.tokensUsed !== undefined) {
    updates.push('tokens_used = @tokens_used')
    params.tokens_used = input.tokensUsed
  }
  if (input.costCents !== undefined) {
    updates.push('cost_cents = @cost_cents')
    params.cost_cents = input.costCents
  }

  if (updates.length === 0)
    return existing

  db.prepare(`UPDATE stage_runs SET ${updates.join(', ')} WHERE id = @id`).run(params)
  return getStageRunById(id, db)
}
