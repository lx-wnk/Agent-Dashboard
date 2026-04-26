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

  // Runtime ALTER for DBs created before silver_bullet/priority landed.
  // schema.sql uses CREATE TABLE IF NOT EXISTS which does not add new
  // columns to an existing table, so we probe pragma and ALTER on the fly.
  const taskCols = connection.prepare('PRAGMA table_info(tasks)').all() as Array<{ name: string }>
  const hasCol = (name: string) => taskCols.some(c => c.name === name)
  if (!hasCol('silver_bullet'))
    connection.prepare('ALTER TABLE tasks ADD COLUMN silver_bullet INTEGER NOT NULL DEFAULT 0').run()
  if (!hasCol('priority'))
    connection.prepare(`ALTER TABLE tasks ADD COLUMN priority TEXT NOT NULL DEFAULT 'medium'`).run()
  // Picker index (idempotent).
  connection.prepare('CREATE INDEX IF NOT EXISTS idx_tasks_picker ON tasks(silver_bullet DESC, priority, created_at)').run()
  // Upgrade stage_runs.session_id index to UNIQUE (idempotent: drops old non-unique index first).
  connection.prepare('DROP INDEX IF EXISTS idx_stage_runs_session').run()
  connection.prepare('CREATE UNIQUE INDEX IF NOT EXISTS idx_stage_runs_session ON stage_runs(session_id) WHERE session_id IS NOT NULL').run()

  // Runtime migration: create task_dependencies for DBs created before this feature.
  // schema.sql uses CREATE TABLE IF NOT EXISTS which is idempotent for new DBs.
  connection.exec(`
    CREATE TABLE IF NOT EXISTS task_dependencies (
      id               TEXT PRIMARY KEY,
      task_id          TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
      depends_on_id    TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
      required_stage   TEXT NOT NULL DEFAULT 'done'
                       CHECK (required_stage IN ('done', 'cancelled')),
      on_cancel_action TEXT NOT NULL DEFAULT 'on_hold'
                       CHECK (on_cancel_action IN ('cancel', 'start', 'on_hold')),
      created_at       TEXT NOT NULL,
      UNIQUE(task_id, depends_on_id)
    )
  `)
  connection.exec(`CREATE INDEX IF NOT EXISTS idx_task_dependencies_task ON task_dependencies(task_id)`)
  connection.exec(`CREATE INDEX IF NOT EXISTS idx_task_dependencies_depends_on ON task_dependencies(depends_on_id)`)

  // Agent monitoring telemetry — deliberately separate from pipeline_config.
  // t is unique: SSE fires at a fixed interval so two points at the same ms are impossible.
  connection.exec(`
    CREATE TABLE IF NOT EXISTS agent_cost_trend (
      t      INTEGER NOT NULL UNIQUE,
      cost   REAL    NOT NULL,
      tokens INTEGER NOT NULL
    )
  `)
  connection.exec(`CREATE INDEX IF NOT EXISTS idx_agent_cost_trend_t ON agent_cost_trend(t)`)

  const version = connection
    .prepare('SELECT MAX(version) as v FROM schema_version')
    .get() as { v: number | null }

  if (version.v === null) {
    connection
      .prepare('INSERT INTO schema_version (version, applied_at) VALUES (?, ?)')
      .run(1, new Date().toISOString())
  }

  // Runtime migration: add user_id to tasks and api_keys for multi-user support.
  const apiKeyCols = connection.prepare('PRAGMA table_info(api_keys)').all() as Array<{ name: string }>
  if (!apiKeyCols.some(c => c.name === 'user_id'))
    connection.prepare('ALTER TABLE api_keys ADD COLUMN user_id TEXT REFERENCES users(id) ON DELETE SET NULL').run()

  if (!hasCol('user_id'))
    connection.prepare('ALTER TABLE tasks ADD COLUMN user_id TEXT REFERENCES users(id) ON DELETE SET NULL').run()
}

export function resetDb(): void {
  closeDb()
  const path = getDbPath()
  if (existsSync(path))
    unlinkSync(path)
}
