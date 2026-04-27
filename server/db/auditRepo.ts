import type { Database } from './client.js'
import type { AuditEntry } from '../../src/types.js'
import type { AuditRow } from './rowMappers.js'
import { randomUUID } from 'node:crypto'
import { getDb } from './client.js'
import { rowToAuditEntry } from './rowMappers.js'

export interface CreateAuditInput {
  taskId: string
  actor: AuditEntry['actor']
  action: string
  details?: Record<string, unknown> | null
}

export function appendAudit(input: CreateAuditInput, db: Database = getDb()): AuditEntry {
  const id = randomUUID()
  db.prepare(`
    INSERT INTO audit_log (id, task_id, actor, action, timestamp, details)
    VALUES (@id, @task_id, @actor, @action, @timestamp, @details)
  `).run({
    id,
    task_id: input.taskId,
    actor: input.actor,
    action: input.action,
    timestamp: new Date().toISOString(),
    details: input.details ? JSON.stringify(input.details) : null,
  })
  const row = db.prepare('SELECT * FROM audit_log WHERE id = ?').get(id) as AuditRow
  return rowToAuditEntry(row)
}

export function listAuditForTask(taskId: string, db: Database = getDb()): AuditEntry[] {
  const rows = db
    .prepare('SELECT * FROM audit_log WHERE task_id = ? ORDER BY timestamp')
    .all(taskId) as AuditRow[]
  return rows.map(rowToAuditEntry)
}
