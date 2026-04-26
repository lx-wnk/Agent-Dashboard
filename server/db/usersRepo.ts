import { getDb } from './client.js'

export interface User {
  id: string
  githubLogin: string
  displayName: string | null
  avatarUrl: string | null
  isAdmin: boolean
  createdAt: string
  lastLoginAt: string | null
}

export interface UpsertUserInput {
  id: string
  githubLogin: string
  displayName: string | null
  avatarUrl: string | null
}

function rowToUser(row: Record<string, unknown>): User {
  return {
    id: row.id as string,
    githubLogin: row.github_login as string,
    displayName: (row.display_name as string | null) ?? null,
    avatarUrl: (row.avatar_url as string | null) ?? null,
    isAdmin: (row.is_admin as number) === 1,
    createdAt: row.created_at as string,
    lastLoginAt: (row.last_login_at as string | null) ?? null,
  }
}

export function upsertUser(input: UpsertUserInput): User {
  const db = getDb()
  const now = new Date().toISOString()
  db.prepare(`
    INSERT INTO users (id, github_login, display_name, avatar_url, created_at, last_login_at)
    VALUES (?, ?, ?, ?, ?, ?)
    ON CONFLICT(id) DO UPDATE SET
      github_login  = excluded.github_login,
      display_name  = excluded.display_name,
      avatar_url    = excluded.avatar_url,
      last_login_at = excluded.last_login_at
  `).run(input.id, input.githubLogin, input.displayName, input.avatarUrl, now, now)

  return rowToUser(
    db.prepare('SELECT * FROM users WHERE id = ?').get(input.id) as Record<string, unknown>,
  )
}

export function findUserById(id: string): User | null {
  const db = getDb()
  const row = db.prepare('SELECT * FROM users WHERE id = ?').get(id) as Record<string, unknown> | undefined
  return row ? rowToUser(row) : null
}

export function setUserAdmin(id: string, isAdmin: boolean): void {
  getDb().prepare('UPDATE users SET is_admin = ? WHERE id = ?').run(isAdmin ? 1 : 0, id)
}
