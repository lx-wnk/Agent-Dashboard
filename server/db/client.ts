import { existsSync, mkdirSync, readFileSync, unlinkSync } from 'node:fs'
import { homedir } from 'node:os'
import { dirname, join } from 'node:path'
import process from 'node:process'
import { fileURLToPath } from 'node:url'
import { Database as BunDatabase } from 'bun:sqlite'

const DEFAULT_DB_PATH = join(homedir(), '.claude', 'dashboard-tasks.db')

// Re-export the bun:sqlite Database type for the rest of the codebase.
// Repo files import { Database } from './client' instead of 'bun:sqlite'.
export type Database = BunDatabase

let db: Database | null = null

export function getDbPath(): string {
  return process.env.DASHBOARD_DB_PATH || DEFAULT_DB_PATH
}

/**
 * Compatibility shims layered onto bun:sqlite to keep the repo's call
 * sites — written against better-sqlite3 — working unchanged.
 *
 * Two shims are installed via `installPrepareShim`, which wraps every
 * Statement returned by `db.prepare(...)`:
 *
 *   1. `bindArgs` (run/get/all) — better-sqlite3 accepts a plain object
 *      like `{ slug: 'x' }` for `@slug` placeholders. bun:sqlite requires
 *      the sigil character to be present in the object key (e.g.
 *      `{ '@slug': 'x' }`). We rewrite bare keys to `@`-prefixed keys.
 *      Array bindings and already-prefixed keys (`@`, `$`, `:`) pass
 *      through untouched.
 *
 *   2. `null → undefined` (get only) — bun:sqlite's `.get()` returns
 *      `null` for "no row found"; better-sqlite3 returns `undefined`.
 *      We normalize to `undefined` so call sites can keep using
 *      `row !== undefined` / `row ? ... : null` patterns.
 *
 * The shim is wired only to `prepare()` — `db.exec(...)` and `db.run(...)`
 * intentionally bypass it. Those are used exclusively for DDL / parameterless
 * pragmas and migrations in this codebase, so no binding rewrite is needed.
 *
 * Convention: every parameterized SQL string in `server/db/*.ts` uses the
 * `@name` placeholder sigil (never `$name` or `:name`). The shim only emits
 * `@`-prefixed keys, so introducing `$` or `:` placeholders without also
 * teaching the shim about them would silently break bindings.
 */
function bindArgs(args: unknown[]): unknown[] {
  if (args.length !== 1)
    return args
  const arg = args[0]
  if (arg === null || arg === undefined || typeof arg !== 'object' || Array.isArray(arg))
    return args
  const src = arg as Record<string, unknown>
  // If any key already carries a sigil prefix, assume the caller knows what
  // they're doing and pass through untouched.
  for (const k of Object.keys(src)) {
    if (k.startsWith('@') || k.startsWith('$') || k.startsWith(':'))
      return args
  }
  const out: Record<string, unknown> = {}
  for (const k of Object.keys(src))
    out[`@${k}`] = src[k]
  return [out]
}

function wrapStatement<T extends { run: (...a: unknown[]) => unknown, get: (...a: unknown[]) => unknown, all: (...a: unknown[]) => unknown }>(stmt: T): T {
  const origRun = stmt.run.bind(stmt)
  const origGet = stmt.get.bind(stmt)
  const origAll = stmt.all.bind(stmt)
  stmt.run = (...args: unknown[]) => origRun(...bindArgs(args))
  // bun:sqlite's .get() returns `null` for "no row", but the repo code base
  // was written against better-sqlite3 which returns `undefined`. Normalize
  // here so call sites can keep using `row !== undefined` / `row ? ... : null`
  // without surprises.
  stmt.get = (...args: unknown[]) => {
    const result = origGet(...bindArgs(args))
    return result === null ? undefined : result
  }
  stmt.all = (...args: unknown[]) => origAll(...bindArgs(args))
  return stmt
}

function installPrepareShim(database: BunDatabase): void {
  const origPrepare = database.prepare.bind(database)
  // Re-assign `prepare` so every Statement returned by the rest of the codebase
  // is wrapped by the compat shim (named-param prefix + null→undefined). We
  // cast through `unknown` because bun:sqlite's `prepare` is a generic method
  // and reassigning it with our simpler signature is the whole point of the
  // shim. Keeping the cast pinpointed here means the rest of the file stays
  // strictly typed against `BunDatabase`.
  ;(database as unknown as { prepare: (sql: string) => unknown }).prepare
    = (sql: string) => wrapStatement(origPrepare(sql) as any)
}

export function getDb(): Database {
  if (db)
    return db
  const path = getDbPath()
  const dir = dirname(path)
  if (!existsSync(dir))
    mkdirSync(dir, { recursive: true })
  db = new BunDatabase(path)
  installPrepareShim(db)
  db.run('PRAGMA journal_mode = WAL')
  db.run('PRAGMA foreign_keys = ON')
  runMigrations(db)
  return db
}

export function closeDb(): void {
  if (db) {
    db.close()
    db = null
  }
}

function migrateV1BaseSchema(db: Database): void {
  const schemaPath = join(dirname(fileURLToPath(import.meta.url)), 'schema.sql')
  db.exec(readFileSync(schemaPath, 'utf-8'))

  const taskCols = db.prepare('PRAGMA table_info(tasks)').all() as Array<{ name: string }>
  const hasTaskCol = (name: string) => taskCols.some(c => c.name === name)

  if (!hasTaskCol('silver_bullet'))
    db.run('ALTER TABLE tasks ADD COLUMN silver_bullet INTEGER NOT NULL DEFAULT 0')
  if (!hasTaskCol('priority'))
    db.run(`ALTER TABLE tasks ADD COLUMN priority TEXT NOT NULL DEFAULT 'medium'`)

  db.run('CREATE INDEX IF NOT EXISTS idx_tasks_picker ON tasks(silver_bullet DESC, priority, created_at)')
  db.run('DROP INDEX IF EXISTS idx_stage_runs_session')
  db.run('CREATE UNIQUE INDEX IF NOT EXISTS idx_stage_runs_session ON stage_runs(session_id) WHERE session_id IS NOT NULL')

  db.exec(`
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
  db.exec('CREATE INDEX IF NOT EXISTS idx_task_dependencies_task ON task_dependencies(task_id)')
  db.exec('CREATE INDEX IF NOT EXISTS idx_task_dependencies_depends_on ON task_dependencies(depends_on_id)')

  db.exec(`
    CREATE TABLE IF NOT EXISTS agent_cost_trend (
      t      INTEGER NOT NULL UNIQUE,
      cost   REAL    NOT NULL,
      tokens INTEGER NOT NULL
    )
  `)
  db.exec('CREATE INDEX IF NOT EXISTS idx_agent_cost_trend_t ON agent_cost_trend(t)')
}

function migrateV2MultiUser(db: Database): void {
  const taskCols = db.prepare('PRAGMA table_info(tasks)').all() as Array<{ name: string }>
  const hasTaskCol = (name: string) => taskCols.some(c => c.name === name)
  const apiKeyCols = db.prepare('PRAGMA table_info(api_keys)').all() as Array<{ name: string }>

  if (!apiKeyCols.some(c => c.name === 'user_id'))
    db.run('ALTER TABLE api_keys ADD COLUMN user_id TEXT REFERENCES users(id) ON DELETE SET NULL')
  if (!hasTaskCol('user_id'))
    db.run('ALTER TABLE tasks ADD COLUMN user_id TEXT REFERENCES users(id) ON DELETE SET NULL')

  db.run('CREATE INDEX IF NOT EXISTS idx_tasks_user ON tasks(user_id)')
  db.run('CREATE INDEX IF NOT EXISTS idx_api_keys_user ON api_keys(user_id)')
}

function runMigrations(db: Database): void {
  migrateV1BaseSchema(db)

  const version = db.prepare('SELECT MAX(version) as v FROM schema_version')
    .get() as { v: number | null }

  if (version.v === null) {
    db.prepare('INSERT INTO schema_version (version, applied_at) VALUES (?, ?)')
      .run(1, new Date().toISOString())
  }

  migrateV2MultiUser(db)

  if ((version.v ?? 0) < 2) {
    db.prepare('INSERT INTO schema_version (version, applied_at) VALUES (?, ?)')
      .run(2, new Date().toISOString())
  }
}

export function resetDb(): void {
  closeDb()
  const path = getDbPath()
  if (existsSync(path))
    unlinkSync(path)
}
