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

  // Runtime migration: older DBs have a CHECK constraint on tasks.current_stage
  // and stage_runs.stage that does not include 'konzept'. SQLite can't ALTER a
  // CHECK constraint in place, so probe with a disposable INSERT/ROLLBACK and,
  // if rejected, rebuild both tables preserving rows and indexes.
  migrateKonzeptCheckConstraint(connection)

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

  const version = connection
    .prepare('SELECT MAX(version) as v FROM schema_version')
    .get() as { v: number | null }

  if (version.v === null) {
    connection
      .prepare('INSERT INTO schema_version (version, applied_at) VALUES (?, ?)')
      .run(1, new Date().toISOString())
  }
}

/**
 * Probe-and-rebuild: does the current CHECK constraint on `tasks.current_stage`
 * accept 'konzept'? If not, drop-and-recreate the table with the updated CHECK,
 * preserving all rows. Same for `stage_runs.stage`.
 *
 * CREATE TABLE IF NOT EXISTS is a no-op on an existing table, so CHECK changes
 * in schema.sql never reach legacy DBs without this migration.
 */
function migrateKonzeptCheckConstraint(connection: DatabaseType): void {
  const acceptsKonzept = (table: 'tasks' | 'stage_runs'): boolean => {
    // Use savepoint so a failed INSERT doesn't poison any outer transaction.
    try {
      connection.exec('SAVEPOINT probe_konzept')
      if (table === 'tasks') {
        connection
          .prepare(
            `INSERT INTO tasks (id, slug, title, cwd, current_stage, max_iterations, stage_timeout_seconds, priority, created_at, updated_at)
             VALUES ('__probe__', '__probe__', 'p', '/', 'konzept', 1, 1, 'medium', '', '')`,
          )
          .run()
      }
      else {
        // Need a referenced task for FK; use the probe row we just created —
        // if the tasks table already accepts konzept we bail via the outer
        // short-circuit before ever reaching here.
        connection
          .prepare(
            `INSERT INTO stage_runs (id, task_id, stage, status, iteration)
             VALUES ('__probe__', '__probe__', 'konzept', 'pending', 0)`,
          )
          .run()
      }
      connection.exec('ROLLBACK TO probe_konzept')
      connection.exec('RELEASE probe_konzept')
      return true
    }
    catch {
      try {
        connection.exec('ROLLBACK TO probe_konzept')
        connection.exec('RELEASE probe_konzept')
      }
      catch {}
      return false
    }
  }

  const tasksOk = acceptsKonzept('tasks')
  if (tasksOk)
    return // both tables were created together; if tasks accepts konzept, stage_runs was rebuilt with it too.

  connection.exec('BEGIN')
  try {
    // Rebuild tasks.
    connection.exec(`
      CREATE TABLE tasks_new (
        id TEXT PRIMARY KEY,
        slug TEXT UNIQUE NOT NULL,
        title TEXT NOT NULL,
        description TEXT,
        cwd TEXT NOT NULL,
        worktree_path TEXT,
        source_branch TEXT,
        target_branch TEXT,
        current_stage TEXT NOT NULL CHECK (current_stage IN (
          'konzept','backlog','pruefung','refinement','planning','approval1',
          'umsetzungskonzept','approval2','umsetzung','selbstreview',
          'finalisierung','done','on_hold','cancelled'
        )),
        parent_task_id TEXT REFERENCES tasks(id) ON DELETE SET NULL,
        max_iterations INTEGER NOT NULL DEFAULT 20,
        token_budget INTEGER,
        cost_budget_cents INTEGER,
        stage_timeout_seconds INTEGER NOT NULL DEFAULT 1800,
        created_at TEXT NOT NULL,
        updated_at TEXT NOT NULL,
        metadata TEXT,
        silver_bullet INTEGER NOT NULL DEFAULT 0,
        priority TEXT NOT NULL DEFAULT 'medium' CHECK (priority IN ('high','medium','low'))
      )
    `)
    connection.exec(`
      INSERT INTO tasks_new
      SELECT id, slug, title, description, cwd, worktree_path, source_branch, target_branch,
             current_stage, parent_task_id, max_iterations, token_budget, cost_budget_cents,
             stage_timeout_seconds, created_at, updated_at, metadata, silver_bullet, priority
      FROM tasks
    `)
    connection.exec('DROP TABLE tasks')
    connection.exec('ALTER TABLE tasks_new RENAME TO tasks')
    connection.exec('CREATE INDEX IF NOT EXISTS idx_tasks_stage ON tasks(current_stage)')
    connection.exec('CREATE INDEX IF NOT EXISTS idx_tasks_parent ON tasks(parent_task_id)')
    connection.exec('CREATE INDEX IF NOT EXISTS idx_tasks_picker ON tasks(silver_bullet DESC, priority, created_at)')

    // Rebuild stage_runs.
    connection.exec(`
      CREATE TABLE stage_runs_new (
        id TEXT PRIMARY KEY,
        task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
        stage TEXT NOT NULL CHECK (stage IN (
          'konzept','backlog','pruefung','refinement','planning','approval1',
          'umsetzungskonzept','approval2','umsetzung','selbstreview',
          'finalisierung','done','on_hold','cancelled'
        )),
        session_id TEXT,
        session_name TEXT,
        pid INTEGER,
        status TEXT NOT NULL CHECK (status IN (
          'pending','running','awaiting_user','on_hold','done','failed'
        )),
        started_at TEXT,
        ended_at TEXT,
        iteration INTEGER NOT NULL DEFAULT 0,
        output TEXT,
        tokens_used INTEGER NOT NULL DEFAULT 0,
        cost_cents INTEGER NOT NULL DEFAULT 0
      )
    `)
    connection.exec(`
      INSERT INTO stage_runs_new
      SELECT id, task_id, stage, session_id, session_name, pid, status, started_at, ended_at,
             iteration, output, tokens_used, cost_cents
      FROM stage_runs
    `)
    connection.exec('DROP TABLE stage_runs')
    connection.exec('ALTER TABLE stage_runs_new RENAME TO stage_runs')
    connection.exec('CREATE INDEX IF NOT EXISTS idx_stage_runs_task ON stage_runs(task_id)')
    connection.exec('CREATE INDEX IF NOT EXISTS idx_stage_runs_status ON stage_runs(status)')
    connection.exec('CREATE UNIQUE INDEX IF NOT EXISTS idx_stage_runs_session ON stage_runs(session_id) WHERE session_id IS NOT NULL')
    connection.exec('CREATE INDEX IF NOT EXISTS idx_stage_runs_latest ON stage_runs(task_id, stage, iteration DESC)')

    connection.exec('COMMIT')
  }
  catch (err) {
    connection.exec('ROLLBACK')
    throw err
  }
}

export function resetDb(): void {
  closeDb()
  const path = getDbPath()
  if (existsSync(path))
    unlinkSync(path)
}
