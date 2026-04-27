import type { Database } from './client.js'
import type { PermissionRequest, TaskPermission } from '../../src/types.js'
import type { PermissionRequestRow, TaskPermissionRow } from './rowMappers.js'
import { randomUUID } from 'node:crypto'
import { getDb } from './client.js'
import { rowToPermissionRequest, rowToTaskPermission } from './rowMappers.js'

export interface CreateTaskPermissionInput {
  taskId: string
  tool: string
  pattern?: string | null
  granted: boolean
  preApproved: boolean
  decidedBy?: 'user' | 'auto'
}

export function createTaskPermission(
  input: CreateTaskPermissionInput,
  db: Database = getDb(),
): TaskPermission {
  const id = randomUUID()
  const now = new Date().toISOString()
  db.prepare(`
    INSERT INTO task_permissions (
      id, task_id, tool, pattern, granted, pre_approved,
      requested_at, decided_at, decided_by
    ) VALUES (
      @id, @task_id, @tool, @pattern, @granted, @pre_approved,
      @requested_at, @decided_at, @decided_by
    )
  `).run({
    id,
    task_id: input.taskId,
    tool: input.tool,
    pattern: input.pattern ?? null,
    granted: input.granted ? 1 : 0,
    pre_approved: input.preApproved ? 1 : 0,
    requested_at: now,
    decided_at: now,
    decided_by: input.decidedBy ?? 'user',
  })
  return getTaskPermissionById(id, db)!
}

export function getTaskPermissionById(id: string, db: Database = getDb()): TaskPermission | null {
  const row = db
    .prepare('SELECT * FROM task_permissions WHERE id = ?')
    .get(id) as TaskPermissionRow | undefined
  return row ? rowToTaskPermission(row) : null
}

export function listTaskPermissions(taskId: string, db: Database = getDb()): TaskPermission[] {
  const rows = db
    .prepare('SELECT * FROM task_permissions WHERE task_id = ? ORDER BY requested_at')
    .all(taskId) as TaskPermissionRow[]
  return rows.map(rowToTaskPermission)
}

export function deleteTaskPermission(id: string, db: Database = getDb()): boolean {
  return db.prepare('DELETE FROM task_permissions WHERE id = ?').run(id).changes > 0
}

// Runtime permission requests (agent pauses mid-stage)

export interface CreatePermissionRequestInput {
  stageRunId: string
  tool: string
  pattern?: string | null
  reason?: string | null
}

export function createPermissionRequest(
  input: CreatePermissionRequestInput,
  db: Database = getDb(),
): PermissionRequest {
  const id = randomUUID()
  const now = new Date().toISOString()
  db.prepare(`
    INSERT INTO permission_requests (id, stage_run_id, tool, pattern, reason, requested_at)
    VALUES (@id, @stage_run_id, @tool, @pattern, @reason, @requested_at)
  `).run({
    id,
    stage_run_id: input.stageRunId,
    tool: input.tool,
    pattern: input.pattern ?? null,
    reason: input.reason ?? null,
    requested_at: now,
  })
  return getPermissionRequestById(id, db)!
}

export function getPermissionRequestById(
  id: string,
  db: Database = getDb(),
): PermissionRequest | null {
  const row = db
    .prepare('SELECT * FROM permission_requests WHERE id = ?')
    .get(id) as PermissionRequestRow | undefined
  return row ? rowToPermissionRequest(row) : null
}

export function listPendingPermissionRequests(
  stageRunId: string,
  db: Database = getDb(),
): PermissionRequest[] {
  const rows = db
    .prepare(`
      SELECT * FROM permission_requests
      WHERE stage_run_id = ? AND outcome IS NULL
      ORDER BY requested_at
    `)
    .all(stageRunId) as PermissionRequestRow[]
  return rows.map(rowToPermissionRequest)
}

export function resolvePermissionRequest(
  id: string,
  outcome: 'granted' | 'denied' | 'timeout',
  db: Database = getDb(),
): PermissionRequest | null {
  db.prepare(`
    UPDATE permission_requests SET outcome = ?, resolved_at = ? WHERE id = ?
  `).run(outcome, new Date().toISOString(), id)
  return getPermissionRequestById(id, db)
}
