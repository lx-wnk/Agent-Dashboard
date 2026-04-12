import type { Database } from 'better-sqlite3'
import type { PipelineStage, PipelineTask } from '../../src/types.js'
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
}

function nowIso(): string {
  return new Date().toISOString()
}

export function createTask(input: CreateTaskInput, db: Database = getDb()): PipelineTask {
  const id = randomUUID()
  const now = nowIso()
  db.prepare(`
    INSERT INTO tasks (
      id, slug, title, description, cwd, worktree_path,
      source_branch, target_branch, current_stage, parent_task_id,
      max_iterations, token_budget, cost_budget_cents, stage_timeout_seconds,
      created_at, updated_at, metadata
    ) VALUES (
      @id, @slug, @title, @description, @cwd, @worktree_path,
      @source_branch, @target_branch, 'backlog', @parent_task_id,
      @max_iterations, @token_budget, @cost_budget_cents, @stage_timeout_seconds,
      @created_at, @updated_at, @metadata
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
  })

  return getTaskById(id, db)!
}

export function getTaskById(id: string, db: Database = getDb()): PipelineTask | null {
  const row = db.prepare('SELECT * FROM tasks WHERE id = ?').get(id) as TaskRow | undefined
  return row ? rowToTask(row) : null
}

export function getTaskBySlug(slug: string, db: Database = getDb()): PipelineTask | null {
  const row = db.prepare('SELECT * FROM tasks WHERE slug = ?').get(slug) as TaskRow | undefined
  return row ? rowToTask(row) : null
}

export function listTasks(db: Database = getDb()): PipelineTask[] {
  const rows = db.prepare('SELECT * FROM tasks ORDER BY created_at DESC').all() as TaskRow[]
  return rows.map(rowToTask)
}

export function listTasksByStage(stage: PipelineStage, db: Database = getDb()): PipelineTask[] {
  const rows = db
    .prepare('SELECT * FROM tasks WHERE current_stage = ? ORDER BY created_at DESC')
    .all(stage) as TaskRow[]
  return rows.map(rowToTask)
}

export function updateTask(id: string, input: UpdateTaskInput, db: Database = getDb()): PipelineTask | null {
  const existing = getTaskById(id, db)
  if (!existing)
    return null

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
