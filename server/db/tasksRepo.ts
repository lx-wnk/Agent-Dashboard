import type { Database } from 'better-sqlite3'
import type { PipelineStage, PipelineTask, TaskPriority } from '../../src/types.js'
import type { TaskRow } from './rowMappers.js'
import { randomUUID } from 'node:crypto'
import { getDb } from './client.js'
import { rowToTask } from './rowMappers.js'

export interface CreateTaskInput {
  slug: string
  title: string
  description?: string | null
  cwd: string
  worktreePath?: string | null
  sourceBranch?: string | null
  targetBranch?: string | null
  parentTaskId?: string | null
  maxIterations?: number
  tokenBudget?: number | null
  costBudgetCents?: number | null
  stageTimeoutSeconds?: number
  metadata?: Record<string, unknown> | null
  silverBullet?: boolean
  priority?: TaskPriority
}

export interface UpdateTaskInput {
  title?: string
  description?: string | null
  worktreePath?: string | null
  currentStage?: PipelineStage
  maxIterations?: number
  tokenBudget?: number | null
  costBudgetCents?: number | null
  stageTimeoutSeconds?: number
  metadata?: Record<string, unknown> | null
  silverBullet?: boolean
  priority?: TaskPriority
}

function nowIso(): string {
  return new Date().toISOString()
}

/**
 * Computed column expression that evaluates to 1 when the task has at least
 * one unmet dependency (a prerequisite task whose current_stage doesn't match
 * the dependency's required_stage), else 0. Alias the `tasks` table when
 * using this in a SELECT since the subquery references the same table name.
 */
const IS_BLOCKED_EXPR = `
  CASE WHEN EXISTS (
    SELECT 1 FROM task_dependencies td
    JOIN tasks t2 ON t2.id = td.depends_on_id
    WHERE td.task_id = tasks.id
      AND t2.current_stage != td.required_stage
  ) THEN 1 ELSE 0 END AS is_blocked
`

const IS_UNSATISFIABLE_EXPR = `
  CASE WHEN EXISTS (
    SELECT 1 FROM task_dependencies td
    JOIN tasks t2 ON t2.id = td.depends_on_id
    WHERE td.task_id = tasks.id
      AND t2.current_stage != td.required_stage
      AND t2.current_stage IN ('done', 'cancelled')
  ) THEN 1 ELSE 0 END AS is_unsatisfiable
`

const VALID_PRIORITIES: readonly TaskPriority[] = ['high', 'medium', 'low']

/**
 * Application-layer enum guard for the `priority` column. SQLite ALTER
 * TABLE ADD COLUMN cannot attach a CHECK constraint, so existing DBs
 * migrated via client.ts runtime migration have no DB-level enforcement.
 * This guard keeps behavior consistent regardless of DB vintage.
 */
function assertValidPriority(p: TaskPriority | undefined): void {
  if (p !== undefined && !VALID_PRIORITIES.includes(p))
    throw new Error(`invalid priority: ${String(p)} (expected one of ${VALID_PRIORITIES.join('|')})`)
}

export function createTask(input: CreateTaskInput, db: Database = getDb()): PipelineTask {
  assertValidPriority(input.priority)
  const id = randomUUID()
  const now = nowIso()
  db.prepare(`
    INSERT INTO tasks (
      id, slug, title, description, cwd, worktree_path,
      source_branch, target_branch, current_stage, parent_task_id,
      max_iterations, token_budget, cost_budget_cents, stage_timeout_seconds,
      created_at, updated_at, metadata, silver_bullet, priority
    ) VALUES (
      @id, @slug, @title, @description, @cwd, @worktree_path,
      @source_branch, @target_branch, 'backlog', @parent_task_id,
      @max_iterations, @token_budget, @cost_budget_cents, @stage_timeout_seconds,
      @created_at, @updated_at, @metadata, @silver_bullet, @priority
    )
  `).run({
    id,
    slug: input.slug,
    title: input.title,
    description: input.description ?? null,
    cwd: input.cwd,
    worktree_path: input.worktreePath ?? null,
    source_branch: input.sourceBranch ?? null,
    target_branch: input.targetBranch ?? null,
    parent_task_id: input.parentTaskId ?? null,
    max_iterations: input.maxIterations ?? 20,
    token_budget: input.tokenBudget ?? null,
    cost_budget_cents: input.costBudgetCents ?? null,
    stage_timeout_seconds: input.stageTimeoutSeconds ?? 1800,
    created_at: now,
    updated_at: now,
    metadata: input.metadata ? JSON.stringify(input.metadata) : null,
    silver_bullet: input.silverBullet ? 1 : 0,
    priority: input.priority ?? 'medium',
  })

  return getTaskById(id, db)!
}

export function getTaskById(id: string, db: Database = getDb()): PipelineTask | null {
  const row = db
    .prepare(`SELECT tasks.*, ${IS_BLOCKED_EXPR}, ${IS_UNSATISFIABLE_EXPR} FROM tasks WHERE tasks.id = ?`)
    .get(id) as TaskRow | undefined
  return row ? rowToTask(row) : null
}

export function getTaskBySlug(slug: string, db: Database = getDb()): PipelineTask | null {
  const row = db
    .prepare(`SELECT tasks.*, ${IS_BLOCKED_EXPR}, ${IS_UNSATISFIABLE_EXPR} FROM tasks WHERE tasks.slug = ?`)
    .get(slug) as TaskRow | undefined
  return row ? rowToTask(row) : null
}

export function listTasks(db: Database = getDb()): PipelineTask[] {
  const rows = db
    .prepare(`SELECT tasks.*, ${IS_BLOCKED_EXPR}, ${IS_UNSATISFIABLE_EXPR} FROM tasks ORDER BY tasks.created_at DESC`)
    .all() as TaskRow[]
  return rows.map(rowToTask)
}

export function listTasksByStage(stage: PipelineStage, db: Database = getDb()): PipelineTask[] {
  const rows = db
    .prepare(`SELECT tasks.*, ${IS_BLOCKED_EXPR}, ${IS_UNSATISFIABLE_EXPR} FROM tasks WHERE tasks.current_stage = ? ORDER BY tasks.created_at DESC`)
    .all(stage) as TaskRow[]
  return rows.map(rowToTask)
}

/**
 * List tasks eligible for runner pickup: excludes terminal (done/cancelled)
 * and orchestrator-paused (on_hold, approval1, approval2) stages, AND tasks
 * with at least one unmet dependency. Tasks with a failed latest stage_run
 * are filtered separately by the orchestrator (pickNextTasksForFreeSlots) —
 * they stay on their stage but require explicit user-triggered retry via
 * POST /tasks/:id/retry.
 */
export function listPickableTasks(db: Database = getDb()): PipelineTask[] {
  const rows = db
    .prepare(`
      SELECT tasks.*, ${IS_BLOCKED_EXPR}, ${IS_UNSATISFIABLE_EXPR} FROM tasks
      WHERE tasks.current_stage NOT IN ('done','cancelled','on_hold','approval1','approval2')
        AND NOT EXISTS (
          SELECT 1 FROM task_dependencies td
          JOIN tasks t2 ON t2.id = td.depends_on_id
          WHERE td.task_id = tasks.id
            AND t2.current_stage != td.required_stage
        )
    `)
    .all() as TaskRow[]
  return rows.map(rowToTask)
}

export function updateTask(id: string, input: UpdateTaskInput, db: Database = getDb()): PipelineTask | null {
  const existing = getTaskById(id, db)
  if (!existing)
    return null
  // Validate AFTER the existence check so that an invalid-priority call
  // against a non-existent task returns null (→ route layer 404), not
  // throws (→ route layer 500). Route handlers validate priority before
  // reaching here; this guard is a defensive backstop for internal callers.
  assertValidPriority(input.priority)

  const updates: string[] = []
  const params: Record<string, unknown> = { id, updated_at: nowIso() }

  if (input.title !== undefined) {
    updates.push('title = @title')
    params.title = input.title
  }
  if (input.description !== undefined) {
    updates.push('description = @description')
    params.description = input.description
  }
  if (input.worktreePath !== undefined) {
    updates.push('worktree_path = @worktree_path')
    params.worktree_path = input.worktreePath
  }
  if (input.currentStage !== undefined) {
    updates.push('current_stage = @current_stage')
    params.current_stage = input.currentStage
  }
  if (input.maxIterations !== undefined) {
    updates.push('max_iterations = @max_iterations')
    params.max_iterations = input.maxIterations
  }
  if (input.tokenBudget !== undefined) {
    updates.push('token_budget = @token_budget')
    params.token_budget = input.tokenBudget
  }
  if (input.costBudgetCents !== undefined) {
    updates.push('cost_budget_cents = @cost_budget_cents')
    params.cost_budget_cents = input.costBudgetCents
  }
  if (input.stageTimeoutSeconds !== undefined) {
    updates.push('stage_timeout_seconds = @stage_timeout_seconds')
    params.stage_timeout_seconds = input.stageTimeoutSeconds
  }
  if (input.metadata !== undefined) {
    updates.push('metadata = @metadata')
    params.metadata = input.metadata ? JSON.stringify(input.metadata) : null
  }
  if (input.silverBullet !== undefined) {
    updates.push('silver_bullet = @silver_bullet')
    params.silver_bullet = input.silverBullet ? 1 : 0
  }
  if (input.priority !== undefined) {
    updates.push('priority = @priority')
    params.priority = input.priority
  }

  if (updates.length === 0)
    return existing

  updates.push('updated_at = @updated_at')
  db.prepare(`UPDATE tasks SET ${updates.join(', ')} WHERE id = @id`).run(params)
  return getTaskById(id, db)
}

export function deleteTask(id: string, db: Database = getDb()): boolean {
  const result = db.prepare('DELETE FROM tasks WHERE id = ?').run(id)
  return result.changes > 0
}
