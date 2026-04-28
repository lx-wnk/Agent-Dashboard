import type { FeedbackStage, TaskFeedback } from '../../src/types.js'
import type { Database } from './client.js'
import { randomUUID } from 'node:crypto'
import { getDb } from './client.js'

interface TaskFeedbackRow {
  id: string
  task_id: string
  stage: FeedbackStage
  stage_run_id: string | null
  iteration: number
  feedback: string
  created_at: string
  resolved_at: string | null
  resolved_by_stage_run_id: string | null
}

function rowToFeedback(row: TaskFeedbackRow): TaskFeedback {
  return {
    id: row.id,
    taskId: row.task_id,
    stage: row.stage,
    stageRunId: row.stage_run_id,
    iteration: row.iteration,
    feedback: row.feedback,
    createdAt: row.created_at,
    resolvedAt: row.resolved_at,
    resolvedByStageRunId: row.resolved_by_stage_run_id,
  }
}

export interface CreateFeedbackInput {
  taskId: string
  stage: FeedbackStage
  stageRunId: string | null
  feedback: string
}

export function createFeedback(
  input: CreateFeedbackInput,
  db: Database = getDb(),
): TaskFeedback {
  const id = randomUUID()
  const now = new Date().toISOString()
  // Derive next iteration: 1-based count of prior feedback entries on
  // the same (task, stage). Separate from stage_run.iteration by design.
  const count = (db
    .prepare('SELECT COUNT(*) as c FROM task_feedback WHERE task_id = ? AND stage = ?')
    .get(input.taskId, input.stage) as { c: number }).c
  const iteration = count + 1
  db.prepare(`
    INSERT INTO task_feedback (
      id, task_id, stage, stage_run_id, iteration, feedback, created_at
    ) VALUES (@id, @task_id, @stage, @stage_run_id, @iteration, @feedback, @created_at)
  `).run({
    id,
    task_id: input.taskId,
    stage: input.stage,
    stage_run_id: input.stageRunId,
    iteration,
    feedback: input.feedback,
    created_at: now,
  })
  return {
    id,
    taskId: input.taskId,
    stage: input.stage,
    stageRunId: input.stageRunId,
    iteration,
    feedback: input.feedback,
    createdAt: now,
    resolvedAt: null,
    resolvedByStageRunId: null,
  }
}

export function listFeedbackForTask(
  taskId: string,
  db: Database = getDb(),
): TaskFeedback[] {
  const rows = db
    .prepare('SELECT * FROM task_feedback WHERE task_id = ? ORDER BY created_at ASC')
    .all(taskId) as TaskFeedbackRow[]
  return rows.map(rowToFeedback)
}

export function listUnresolvedFeedbackForStage(
  taskId: string,
  stage: FeedbackStage,
  db: Database = getDb(),
): TaskFeedback[] {
  const rows = db
    .prepare(
      'SELECT * FROM task_feedback WHERE task_id = ? AND stage = ? AND resolved_at IS NULL ORDER BY iteration ASC',
    )
    .all(taskId, stage) as TaskFeedbackRow[]
  return rows.map(rowToFeedback)
}

export function resolveFeedbackForStage(
  taskId: string,
  stage: FeedbackStage,
  resolvedByStageRunId: string,
  db: Database = getDb(),
): number {
  const now = new Date().toISOString()
  const result = db
    .prepare(
      'UPDATE task_feedback SET resolved_at = ?, resolved_by_stage_run_id = ? WHERE task_id = ? AND stage = ? AND resolved_at IS NULL',
    )
    .run(now, resolvedByStageRunId, taskId, stage)
  return result.changes
}
