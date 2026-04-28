import { randomUUID } from 'node:crypto'
import { getDb } from './client.js'

export interface RemoteRegistration {
  id: string
  userId: string
  url: string
  name: string | null
  bearerKey: string | null
  createdAt: string
}

export interface CreateRemoteInput {
  userId: string
  url: string
  name: string | null
  bearerKey: string | null
}

function rowToReg(row: Record<string, unknown>): RemoteRegistration {
  return {
    id: row.id as string,
    userId: row.user_id as string,
    url: row.url as string,
    name: (row.name as string | null) ?? null,
    bearerKey: (row.bearer_key as string | null) ?? null,
    createdAt: row.created_at as string,
  }
}

export function createRemoteRegistration(input: CreateRemoteInput): RemoteRegistration {
  const db = getDb()
  const id = randomUUID()
  const now = new Date().toISOString()
  db.prepare(`
    INSERT INTO remote_registrations (id, user_id, url, name, bearer_key, created_at)
    VALUES (?, ?, ?, ?, ?, ?)
  `).run(id, input.userId, input.url, input.name, input.bearerKey, now)
  return rowToReg(db.prepare('SELECT * FROM remote_registrations WHERE id = ?').get(id) as Record<string, unknown>)
}

/** Always filters by userId — no admin override. */
export function listRemoteRegistrationsForUser(userId: string): RemoteRegistration[] {
  const rows = getDb()
    .prepare('SELECT * FROM remote_registrations WHERE user_id = ? ORDER BY created_at ASC')
    .all(userId) as Record<string, unknown>[]
  return rows.map(rowToReg)
}

/** Returns true only when the registration belonged to userId. */
export function deleteRemoteRegistration(id: string, userId: string): boolean {
  const result = getDb()
    .prepare('DELETE FROM remote_registrations WHERE id = ? AND user_id = ?')
    .run(id, userId)
  return result.changes > 0
}
