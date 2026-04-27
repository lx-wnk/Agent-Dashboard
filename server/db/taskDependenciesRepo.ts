import type { Database } from './client.js'
import type { TaskDependency } from '../../src/types.js'
import type { TaskDependencyRow } from './rowMappers.js'
import { randomUUID } from 'node:crypto'
import { getDb } from './client.js'
import { rowToTaskDependency } from './rowMappers.js'

function nowIso(): string {
  return new Date().toISOString()
}

const DEPENDENCY_SELECT = `
  SELECT
    td.id, td.task_id,
    t1.title AS task_title,
    td.depends_on_id,
    t2.title AS depends_on_title,
    t2.current_stage AS depends_on_stage,
    td.required_stage, td.on_cancel_action, td.created_at
  FROM task_dependencies td
  JOIN tasks t1 ON t1.id = td.task_id
  JOIN tasks t2 ON t2.id = td.depends_on_id
`

/**
 * Add a dependency: task_id must wait for depends_on_id to reach required_stage.
 * Throws if the dependency would create a cycle (including self-dependency).
 */
export function addDependency(
  taskId: string,
  dependsOnId: string,
  requiredStage: 'done' | 'cancelled' = 'done',
  onCancelAction: 'cancel' | 'start' | 'on_hold' = 'on_hold',
  db: Database = getDb(),
): TaskDependency {
  if (wouldCreateCycle(taskId, dependsOnId, db))
    throw new Error(`Dependency would create a cycle between ${taskId} and ${dependsOnId}`)
  const id = randomUUID()
  db.prepare(`
    INSERT INTO task_dependencies (id, task_id, depends_on_id, required_stage, on_cancel_action, created_at)
    VALUES (?, ?, ?, ?, ?, ?)
  `).run(id, taskId, dependsOnId, requiredStage, onCancelAction, nowIso())
  return getDependencyById(id, db)!
}

export function removeDependency(taskId: string, dependsOnId: string, db: Database = getDb()): boolean {
  const result = db
    .prepare('DELETE FROM task_dependencies WHERE task_id = ? AND depends_on_id = ?')
    .run(taskId, dependsOnId)
  return result.changes > 0
}

/** Remove a dependency by its row ID (used by the DELETE /dependencies/:depId route). */
export function removeDependencyById(id: string, taskId: string, db: Database = getDb()): boolean {
  const result = db
    .prepare('DELETE FROM task_dependencies WHERE id = ? AND task_id = ?')
    .run(id, taskId)
  return result.changes > 0
}

/** All prerequisites for a task (what this task is waiting for). */
export function getDependenciesFor(taskId: string, db: Database = getDb()): TaskDependency[] {
  const rows = db
    .prepare(`${DEPENDENCY_SELECT} WHERE td.task_id = ?`)
    .all(taskId) as TaskDependencyRow[]
  return rows.map(rowToTaskDependency)
}

/** All tasks that are waiting on this task. */
export function getDependentsOf(taskId: string, db: Database = getDb()): TaskDependency[] {
  const rows = db
    .prepare(`${DEPENDENCY_SELECT} WHERE td.depends_on_id = ?`)
    .all(taskId) as TaskDependencyRow[]
  return rows.map(rowToTaskDependency)
}

/**
 * Returns true if task_id has at least one prerequisite that has not yet
 * reached its required_stage.
 */
export function isBlocked(taskId: string, db: Database = getDb()): boolean {
  const row = db
    .prepare(`
      SELECT 1 FROM task_dependencies td
      JOIN tasks t2 ON t2.id = td.depends_on_id
      WHERE td.task_id = ?
        AND t2.current_stage != td.required_stage
      LIMIT 1
    `)
    .get(taskId)
  return row !== undefined
}

/**
 * Returns true if taskId has any prerequisite that has not yet reached a terminal
 * stage (done or cancelled), excluding the given excludeDependsOnId.
 * Used by the orchestrator cascade to decide whether to apply on_cancel_action.
 */
export function hasOtherBlockingDeps(
  taskId: string,
  excludeDependsOnId: string,
  db: Database = getDb(),
): boolean {
  const row = db
    .prepare(`
      SELECT 1 FROM task_dependencies td
      JOIN tasks t2 ON t2.id = td.depends_on_id
      WHERE td.task_id = ?
        AND td.depends_on_id != ?
        AND t2.current_stage NOT IN ('done', 'cancelled')
      LIMIT 1
    `)
    .get(taskId, excludeDependsOnId)
  return row !== undefined
}

function getDependencyById(id: string, db: Database): TaskDependency | null {
  const row = db
    .prepare(`${DEPENDENCY_SELECT} WHERE td.id = ?`)
    .get(id) as TaskDependencyRow | undefined
  return row ? rowToTaskDependency(row) : null
}

/**
 * BFS from dependsOnId following existing task_id → depends_on_id edges.
 * If the walk reaches taskId, the proposed insert would create a cycle.
 * Also catches self-dependency (taskId === dependsOnId).
 */
function wouldCreateCycle(taskId: string, dependsOnId: string, db: Database): boolean {
  if (taskId === dependsOnId)
    return true
  const visited = new Set<string>()
  const queue = [dependsOnId]
  while (queue.length > 0) {
    const current = queue.shift()!
    if (visited.has(current))
      continue
    visited.add(current)
    const predecessors = db
      .prepare('SELECT depends_on_id FROM task_dependencies WHERE task_id = ?')
      .all(current) as Array<{ depends_on_id: string }>
    for (const row of predecessors) {
      if (row.depends_on_id === taskId)
        return true
      queue.push(row.depends_on_id)
    }
  }
  return false
}
