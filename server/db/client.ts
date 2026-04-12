import type { Database as DatabaseType } from 'better-sqlite3'
import { existsSync, mkdirSync, readFileSync, unlinkSync } from 'node:fs'
import { homedir } from 'node:os'
import { dirname, join } from 'node:path'
import process from 'node:process'
import { fileURLToPath } from 'node:url'
import Database from 'better-sqlite3'

const DEFAULT_DB_PATH = join(homedir(), '.claude', 'dashboard-tasks.db')

let db: DatabaseType | null = null

export function getDbPath(): string {
  return process.env.DASHBOARD_DB_PATH || DEFAULT_DB_PATH
}

export function getDb(): DatabaseType {
  if (db)
    return db
  const path = getDbPath()
  const dir = dirname(path)
  if (!existsSync(dir))
    mkdirSync(dir, { recursive: true })
  db = new Database(path)
  db.pragma('journal_mode = WAL')
  db.pragma('foreign_keys = ON')
  runMigrations(db)
  return db
}

export function closeDb(): void {
  if (db) {
    db.close()
    db = null
  }
}

function runMigrations(connection: DatabaseType): void {
  const schemaPath = join(dirname(fileURLToPath(import.meta.url)), 'schema.sql')
  const schema = readFileSync(schemaPath, 'utf-8')
  connection.exec(schema)

  const version = connection
    .prepare('SELECT MAX(version) as v FROM schema_version')
    .get() as { v: number | null }

  if (version.v === null) {
    connection
      .prepare('INSERT INTO schema_version (version, applied_at) VALUES (?, ?)')
      .run(1, new Date().toISOString())
  }
}

export function resetDb(): void {
  closeDb()
  const path = getDbPath()
  if (existsSync(path))
    unlinkSync(path)
}
