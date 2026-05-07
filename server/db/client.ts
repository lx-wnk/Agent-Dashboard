import { existsSync, mkdirSync, unlinkSync } from 'node:fs'
import { homedir } from 'node:os'
import { dirname, join } from 'node:path'
import process from 'node:process'
import { Database as BunDatabase } from 'bun:sqlite'
import schemaSql from './schema.sql' with { type: 'text' }

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
  db.exec(schemaSql)

  const taskCols = db.prepare('PRAGMA table_info(tasks)').all() as Array<{ name: string }>
  const hasTaskCol = (name: string) => taskCols.some(c => c.name === name)

  if (!hasTaskCol('silver_bullet'))
    db.run('ALTER TABLE tasks ADD COLUMN silver_bullet INTEGER NOT NULL DEFAULT 0')
  if (!hasTaskCol('priority'))
    db.run(`ALTER TABLE tasks ADD COLUMN priority TEXT NOT NULL DEFAULT 'medium'`)

  db.run('CREATE INDEX IF NOT EXISTS idx_tasks_picker ON tasks(silver_bullet DESC, priority, created_at)')
  db.run('DROP INDEX IF EXISTS idx_stage_runs_session')
  db.run('CREATE UNIQUE INDEX IF NOT EXISTS idx_stage_runs_session ON stage_runs(session_id) WHERE session_id IS NOT NULL')

  // Runtime migration: older DBs have a CHECK constraint on tasks.current_stage
  // and stage_runs.stage that does not include 'concept'. SQLite can't ALTER a
  // CHECK constraint in place, so probe with a disposable INSERT/ROLLBACK and,
  // if rejected, rebuild both tables preserving rows and indexes.
  migrateKonzeptCheckConstraint(db)

  // Runtime migration: create task_dependencies for DBs created before this feature.
  // schema.sql uses CREATE TABLE IF NOT EXISTS which is idempotent for new DBs.
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

  // permission_presets was added after some DBs were initialised. The inline
  // UNIQUE(COALESCE(...)) in schema.sql caused db.exec(schemaSql) to throw,
  // leaving this table absent. Create it explicitly here as a safe catch-up.
  db.exec(`
    CREATE TABLE IF NOT EXISTS permission_presets (
      id          TEXT PRIMARY KEY,
      user_id     TEXT,
      project_cwd TEXT NOT NULL,
      tool        TEXT NOT NULL,
      pattern     TEXT,
      created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
    )
  `)
  db.exec(`
    CREATE UNIQUE INDEX IF NOT EXISTS idx_permission_presets_unique
      ON permission_presets(COALESCE(user_id, ''), project_cwd, tool, COALESCE(pattern, ''))
  `)
  db.exec('CREATE INDEX IF NOT EXISTS idx_permission_presets_lookup ON permission_presets(user_id, project_cwd)')
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

/**
 * Adds optional `expires_at` to task_permissions so grants can be time-bound.
 * Indexed alongside task_id so listEffectiveTaskPermissions stays cheap when
 * the table grows. NULL = never expires (existing rows behave unchanged).
 */
function migrateV3PermissionExpiry(db: Database): void {
  const cols = db.prepare('PRAGMA table_info(task_permissions)').all() as Array<{ name: string }>
  if (!cols.some(c => c.name === 'expires_at'))
    db.run('ALTER TABLE task_permissions ADD COLUMN expires_at TEXT')
  db.run('CREATE INDEX IF NOT EXISTS idx_task_permissions_effective ON task_permissions(task_id, granted, expires_at)')
}

/**
 * Translate the four German stage tokens (and the two refinement-phase
 * tokens) to their English equivalents in every place they live:
 *
 *   - tasks.current_stage     ('konzept'/'umsetzung'/'selbstreview'/'finalisierung' → English)
 *   - stage_runs.stage        (same)
 *   - refinement_turns.phase  ('analyse' → 'analysis', 'umsetzungskonzept' → 'implementation_plan')
 *   - task_feedback.stage     ('umsetzungskonzept' → 'implementation_plan')
 *   - task.metadata JSON: legacy `konzeptOutput` key → canonical `conceptOutput`
 *
 * The CHECK constraint on tasks/stage_runs already accepts both old and
 * new tokens (see schema.sql comment), so the UPDATEs below succeed
 * without rebuilding tables. After translation, `VALID_STAGES` and the
 * `PipelineStage` union narrow runtime/typecheck enforcement to the new
 * names; legacy rows that survived earlier migrations are repaired here.
 *
 * Idempotent: each UPDATE is a no-op once a DB has already been
 * translated (the WHEN clauses match nothing).
 */
function migrateV4StageRename(db: Database): void {
  // Use string concatenation so a future codebase-wide rename pass that
  // rewrites bare German tokens to English doesn't accidentally clobber
  // these legacy literals — they MUST stay German, that's the whole point.
  const LEGACY_KONZEPT = `kon${'zept'}`
  const LEGACY_UMSETZUNG = `um${'setzung'}`
  const LEGACY_SELBSTREVIEW = `selbst${'review'}`
  const LEGACY_FINALISIERUNG = `finali${'sierung'}`
  const LEGACY_ANALYSE = `ana${'lyse'}`
  const LEGACY_UMSETZUNGSKONZEPT = `umsetzungs${'konzept'}`
  const LEGACY_KONZEPT_OUTPUT_KEY = `kon${'zeptOutput'}`

  db.exec(`
    UPDATE tasks SET current_stage = CASE current_stage
      WHEN '${LEGACY_KONZEPT}' THEN 'concept'
      WHEN '${LEGACY_UMSETZUNG}' THEN 'implementation'
      WHEN '${LEGACY_SELBSTREVIEW}' THEN 'self_review'
      WHEN '${LEGACY_FINALISIERUNG}' THEN 'finalization'
      ELSE current_stage
    END
    WHERE current_stage IN ('${LEGACY_KONZEPT}','${LEGACY_UMSETZUNG}','${LEGACY_SELBSTREVIEW}','${LEGACY_FINALISIERUNG}')
  `)
  db.exec(`
    UPDATE stage_runs SET stage = CASE stage
      WHEN '${LEGACY_KONZEPT}' THEN 'concept'
      WHEN '${LEGACY_UMSETZUNG}' THEN 'implementation'
      WHEN '${LEGACY_SELBSTREVIEW}' THEN 'self_review'
      WHEN '${LEGACY_FINALISIERUNG}' THEN 'finalization'
      ELSE stage
    END
    WHERE stage IN ('${LEGACY_KONZEPT}','${LEGACY_UMSETZUNG}','${LEGACY_SELBSTREVIEW}','${LEGACY_FINALISIERUNG}')
  `)
  db.exec(`
    UPDATE refinement_turns SET phase = CASE phase
      WHEN '${LEGACY_ANALYSE}' THEN 'analysis'
      WHEN '${LEGACY_UMSETZUNGSKONZEPT}' THEN 'implementation_plan'
      ELSE phase
    END
    WHERE phase IN ('${LEGACY_ANALYSE}','${LEGACY_UMSETZUNGSKONZEPT}')
  `)
  // task_feedback only exists on DBs that ran the relevant migration; guard.
  const feedbackTbl = db.prepare(
    `SELECT name FROM sqlite_master WHERE type='table' AND name='task_feedback'`,
  ).get() as { name: string } | undefined
  if (feedbackTbl !== undefined) {
    db.exec(`
      UPDATE task_feedback SET stage = 'implementation_plan'
      WHERE stage = '${LEGACY_UMSETZUNGSKONZEPT}'
    `)
  }
  // task.metadata JSON key migration — legacy konzeptOutput → conceptOutput.
  // SQLite json_set/json_remove require the metadata column to be valid
  // JSON; rows with NULL or non-JSON metadata are skipped via the
  // json_valid + json_extract guard so a corrupt blob doesn't abort the
  // whole migration.
  db.exec(`
    UPDATE tasks
    SET metadata = json_remove(
      json_set(metadata, '$.conceptOutput', json_extract(metadata, '$.${LEGACY_KONZEPT_OUTPUT_KEY}')),
      '$.${LEGACY_KONZEPT_OUTPUT_KEY}'
    )
    WHERE metadata IS NOT NULL
      AND json_valid(metadata)
      AND json_extract(metadata, '$.${LEGACY_KONZEPT_OUTPUT_KEY}') IS NOT NULL
  `)
}

/**
 * Narrow the CHECK constraints on `tasks.current_stage`, `stage_runs.stage`,
 * and `task_feedback.stage` to the canonical English token list. Closes the
 * V4 transition window that intentionally widened the CHECK to accept BOTH
 * old and new tokens so legacy rows survived the rename UPDATEs.
 *
 * The rename UPDATEs in `migrateV4StageRename` translate every legacy row
 * before this migration runs, so post-V5 the CHECK can safely reject any
 * remaining German literal — none should exist.
 *
 * Cosmetically batch-renames legacy German action names and `details` JSON
 * tokens in `audit_log` so the audit-log UI no longer shows mixed-language
 * entries from pre-V4 sessions. This is purely visual: audit rows are not
 * queried by action name and the rewrites are no-ops on rows that already
 * use the new English names.
 *
 * Idempotent: probes whether `tasks.current_stage` still accepts a legacy
 * German token; if not, returns early because the rebuild has already run.
 */
function migrateV5NarrowStageCheck(db: Database): void {
  const tasksAcceptsLegacyKonzept = (): boolean => {
    try {
      db.exec('SAVEPOINT probe_v5_narrow')
      db
        .prepare(
          `INSERT INTO tasks (id, slug, title, cwd, current_stage, max_iterations, stage_timeout_seconds, priority, created_at, updated_at)
           VALUES ('__probe_v5__', '__probe_v5__', 'p', '/', 'kon' || 'zept', 1, 1, 'medium', '', '')`,
        )
        .run()
      db.exec('ROLLBACK TO probe_v5_narrow')
      db.exec('RELEASE probe_v5_narrow')
      return true
    }
    catch {
      try {
        db.exec('ROLLBACK TO probe_v5_narrow')
        db.exec('RELEASE probe_v5_narrow')
      }
      catch {}
      return false
    }
  }

  if (!tasksAcceptsLegacyKonzept())
    return

  const taskCols = db.prepare('PRAGMA table_info(tasks)').all() as Array<{ name: string }>
  const hasUserId = taskCols.some(c => c.name === 'user_id')

  // Standard SQLite table-rebuild pattern: disable FK enforcement during the
  // transaction so cascading references on referenced tables (`stage_runs.task_id`
  // → `tasks.id` ON DELETE CASCADE) do not wipe child rows when the parent
  // table is dropped mid-rebuild. Re-enabled in the finally block.
  db.exec('PRAGMA foreign_keys = OFF')
  db.exec('BEGIN')
  try {
    db.exec(`
      CREATE TABLE tasks_v5_new (
        id TEXT PRIMARY KEY,
        slug TEXT UNIQUE NOT NULL,
        title TEXT NOT NULL,
        description TEXT,
        cwd TEXT NOT NULL,
        worktree_path TEXT,
        source_branch TEXT,
        target_branch TEXT,
        current_stage TEXT NOT NULL CHECK (current_stage IN (
          'concept','backlog','implementation','self_review','finalization',
          'done','on_hold','cancelled'
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
        priority TEXT NOT NULL DEFAULT 'medium' CHECK (priority IN ('high','medium','low'))${hasUserId
          ? `,
        user_id TEXT REFERENCES users(id) ON DELETE SET NULL`
          : ''}
      )
    `)
    db.exec(`
      INSERT INTO tasks_v5_new
      SELECT id, slug, title, description, cwd, worktree_path, source_branch, target_branch,
             current_stage, parent_task_id, max_iterations, token_budget, cost_budget_cents,
             stage_timeout_seconds, created_at, updated_at, metadata, silver_bullet, priority${hasUserId ? ', user_id' : ''}
      FROM tasks
    `)
    db.exec('DROP TABLE tasks')
    db.exec('ALTER TABLE tasks_v5_new RENAME TO tasks')
    db.exec('CREATE INDEX IF NOT EXISTS idx_tasks_stage ON tasks(current_stage)')
    db.exec('CREATE INDEX IF NOT EXISTS idx_tasks_parent ON tasks(parent_task_id)')
    db.exec('CREATE INDEX IF NOT EXISTS idx_tasks_picker ON tasks(silver_bullet DESC, priority, created_at)')
    if (hasUserId)
      db.exec('CREATE INDEX IF NOT EXISTS idx_tasks_user ON tasks(user_id)')

    db.exec(`
      CREATE TABLE stage_runs_v5_new (
        id TEXT PRIMARY KEY,
        task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
        stage TEXT NOT NULL CHECK (stage IN (
          'concept','backlog','implementation','self_review','finalization',
          'done','on_hold','cancelled'
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
    db.exec(`
      INSERT INTO stage_runs_v5_new
      SELECT id, task_id, stage, session_id, session_name, pid, status, started_at, ended_at,
             iteration, output, tokens_used, cost_cents
      FROM stage_runs
    `)
    db.exec('DROP TABLE stage_runs')
    db.exec('ALTER TABLE stage_runs_v5_new RENAME TO stage_runs')
    db.exec('CREATE INDEX IF NOT EXISTS idx_stage_runs_task ON stage_runs(task_id)')
    db.exec('CREATE INDEX IF NOT EXISTS idx_stage_runs_status ON stage_runs(status)')
    db.exec('CREATE UNIQUE INDEX IF NOT EXISTS idx_stage_runs_session ON stage_runs(session_id) WHERE session_id IS NOT NULL')
    db.exec('CREATE INDEX IF NOT EXISTS idx_stage_runs_latest ON stage_runs(task_id, stage, iteration DESC)')

    // task_feedback only exists on DBs that ran the relevant migration; guard.
    const feedbackTbl = db.prepare(
      `SELECT name FROM sqlite_master WHERE type='table' AND name='task_feedback'`,
    ).get() as { name: string } | undefined
    if (feedbackTbl !== undefined) {
      db.exec(`
        CREATE TABLE task_feedback_v5_new (
          id TEXT PRIMARY KEY,
          task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
          stage TEXT NOT NULL CHECK (stage IN ('planning','implementation_plan')),
          stage_run_id TEXT REFERENCES stage_runs(id) ON DELETE SET NULL,
          iteration INTEGER NOT NULL,
          feedback TEXT NOT NULL,
          created_at TEXT NOT NULL,
          resolved_at TEXT,
          resolved_by_stage_run_id TEXT REFERENCES stage_runs(id) ON DELETE SET NULL
        )
      `)
      db.exec(`
        INSERT INTO task_feedback_v5_new
        SELECT id, task_id, stage, stage_run_id, iteration, feedback, created_at,
               resolved_at, resolved_by_stage_run_id
        FROM task_feedback
      `)
      db.exec('DROP TABLE task_feedback')
      db.exec('ALTER TABLE task_feedback_v5_new RENAME TO task_feedback')
      db.exec('CREATE INDEX IF NOT EXISTS idx_task_feedback_task_stage ON task_feedback(task_id, stage)')
      db.exec('CREATE INDEX IF NOT EXISTS idx_task_feedback_unresolved ON task_feedback(task_id, stage, resolved_at)')
    }

    // F6: cosmetic batch-rename of legacy German action names and `details`
    // JSON tokens in audit_log. Order matters — replace the longest token
    // first so 'umsetzungskonzept' is not corrupted to 'umsetzungconcept'
    // by the shorter 'konzept' rule. REPLACE is a no-op for rows that
    // already use the new English forms.
    const ANALYSE = `ana${'lyse'}`
    const KONZEPT = `kon${'zept'}`
    const UMSETZUNG = `um${'setzung'}`
    const SELBSTREVIEW = `selbst${'review'}`
    const FINALISIERUNG = `finali${'sierung'}`
    const UMSETZUNGSKONZEPT = `umsetzungs${'konzept'}`

    const renamePairs: Array<[string, string]> = [
      [UMSETZUNGSKONZEPT, 'implementation_plan'],
      [UMSETZUNG, 'implementation'],
      [SELBSTREVIEW, 'self_review'],
      [FINALISIERUNG, 'finalization'],
      [ANALYSE, 'analysis'],
      [KONZEPT, 'concept'],
    ]
    for (const [oldTok, newTok] of renamePairs) {
      db.prepare(`UPDATE audit_log SET action = REPLACE(action, @old, @new) WHERE action LIKE '%' || @old || '%'`)
        .run({ old: oldTok, new: newTok })
      db.prepare(`UPDATE audit_log SET details = REPLACE(details, @old, @new) WHERE details LIKE '%' || @old || '%'`)
        .run({ old: oldTok, new: newTok })
    }

    db.exec('COMMIT')
  }
  catch (err) {
    db.exec('ROLLBACK')
    throw err
  }
  finally {
    db.exec('PRAGMA foreign_keys = ON')
  }
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

  migrateV3PermissionExpiry(db)

  if ((version.v ?? 0) < 3) {
    db.prepare('INSERT INTO schema_version (version, applied_at) VALUES (?, ?)')
      .run(3, new Date().toISOString())
  }

  migrateV4StageRename(db)

  if ((version.v ?? 0) < 4) {
    db.prepare('INSERT INTO schema_version (version, applied_at) VALUES (?, ?)')
      .run(4, new Date().toISOString())
  }

  migrateV5NarrowStageCheck(db)

  if ((version.v ?? 0) < 5) {
    db.prepare('INSERT INTO schema_version (version, applied_at) VALUES (?, ?)')
      .run(5, new Date().toISOString())
  }
}

/**
 * Probe-and-rebuild: does the current CHECK constraint on `tasks.current_stage`
 * accept 'concept'? If not, drop-and-recreate the table with the updated CHECK,
 * preserving all rows. Same for `stage_runs.stage`.
 *
 * CREATE TABLE IF NOT EXISTS is a no-op on an existing table, so CHECK changes
 * in schema.sql never reach legacy DBs without this migration.
 */
function migrateKonzeptCheckConstraint(connection: Database): void {
  // Probe the `tasks` CHECK constraint only — both tables were created
  // together, so if `tasks` rejects 'concept' the stage_runs CHECK is
  // equally stale and both get rebuilt below in the same transaction.
  const tasksAcceptsKonzept = (): boolean => {
    // Use savepoint so a failed INSERT doesn't poison any outer transaction.
    try {
      connection.exec('SAVEPOINT probe_konzept')
      connection
        .prepare(
          `INSERT INTO tasks (id, slug, title, cwd, current_stage, max_iterations, stage_timeout_seconds, priority, created_at, updated_at)
           VALUES ('__probe__', '__probe__', 'p', '/', 'concept', 1, 1, 'medium', '', '')`,
        )
        .run()
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

  if (tasksAcceptsKonzept())
    return

  // Must run outside any outer transaction — the caller (getDb → runMigrations)
  // only runs DDL before this point, so BEGIN here is always the top-level tx.
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
          'concept','implementation','self_review','finalization',
          'konzept','umsetzung','selbstreview','finalisierung',
          'backlog','pruefung','refinement','planning','approval1',
          'umsetzungskonzept','implementation_plan','approval2','done','on_hold','cancelled'
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
          'concept','implementation','self_review','finalization',
          'konzept','umsetzung','selbstreview','finalisierung',
          'backlog','pruefung','refinement','planning','approval1',
          'umsetzungskonzept','implementation_plan','approval2','done','on_hold','cancelled'
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
