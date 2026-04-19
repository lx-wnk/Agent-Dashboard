import { createHash, randomBytes } from 'node:crypto'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import {
  createApiKey,
  generateApiToken,
  getApiKeyByHash,
  getApiKeyById,
  hashApiToken,
  listApiKeys,
  revokeApiKey,
  touchApiKey,
} from './apiKeysRepo.js'
import { appendAudit, listAuditForTask } from './auditRepo.js'
import { closeDb, getDb } from './client.js'
import {
  getAllConfig,
  getConfig,
  getPipelineConfigNumber,
  listPreferences,
  setConfig,
  setPipelineConfig,
  setPreference,
} from './notificationConfigRepo.js'
import {
  createPermissionRequest,
  createTaskPermission,
  listPendingPermissionRequests,
  listTaskPermissions,
  resolvePermissionRequest,
} from './permissionsRepo.js'
import {
  createStageRun,
  findStageRunBySessionId,
  getLatestStageRun,
  listRunningStageRuns,
  listStageRunsForTask,
  updateStageRun,
} from './stageRunsRepo.js'
import {
  createTask,
  deleteTask,
  getTaskById,
  getTaskBySlug,
  listTasks,
  listTasksByStage,
  updateTask,
} from './tasksRepo.js'

let tmpDir: string

beforeEach(() => {
  tmpDir = mkdtempSync(join(tmpdir(), 'dashboard-db-test-'))
  process.env.DASHBOARD_DB_PATH = join(tmpDir, 'test.db')
  getDb() // initialize schema
})

afterEach(() => {
  closeDb()
  rmSync(tmpDir, { recursive: true, force: true })
  delete process.env.DASHBOARD_DB_PATH
})

describe('tasksRepo', () => {
  it('creates and retrieves a task by id and slug', () => {
    const task = createTask({
      slug: 'fix-login-bug',
      title: 'Fix login bug',
      description: 'Users cannot log in',
      cwd: '/tmp/project',
    })
    expect(task.id).toBeTruthy()
    expect(task.slug).toBe('fix-login-bug')
    expect(task.currentStage).toBe('backlog')
    expect(task.maxIterations).toBe(20)

    expect(getTaskById(task.id)).toEqual(task)
    expect(getTaskBySlug('fix-login-bug')).toEqual(task)
  })

  it('enforces slug uniqueness', () => {
    createTask({ slug: 'duplicate', title: 'A', cwd: '/a' })
    expect(() => createTask({ slug: 'duplicate', title: 'B', cwd: '/b' })).toThrow()
  })

  it('lists tasks and filters by stage', () => {
    const t1 = createTask({ slug: 'a', title: 'A', cwd: '/a' })
    const t2 = createTask({ slug: 'b', title: 'B', cwd: '/b' })
    updateTask(t2.id, { currentStage: 'umsetzung' })

    const all = listTasks()
    expect(all).toHaveLength(2)

    const backlog = listTasksByStage('backlog')
    expect(backlog.map(t => t.id)).toEqual([t1.id])

    const impl = listTasksByStage('umsetzung')
    expect(impl.map(t => t.id)).toEqual([t2.id])
  })

  it('updates task fields selectively', () => {
    const task = createTask({ slug: 'x', title: 'Original', cwd: '/x' })
    const updated = updateTask(task.id, {
      title: 'Updated',
      tokenBudget: 50000,
      metadata: { screenshot: 'foo.png' },
    })
    expect(updated?.title).toBe('Updated')
    expect(updated?.tokenBudget).toBe(50000)
    expect(updated?.metadata).toEqual({ screenshot: 'foo.png' })
    expect(updated?.cwd).toBe('/x') // unchanged
  })

  it('stores parent_task_id for follow-up tasks', () => {
    const parent = createTask({ slug: 'parent', title: 'Parent', cwd: '/p' })
    const child = createTask({
      slug: 'child',
      title: 'Child',
      cwd: '/p',
      parentTaskId: parent.id,
    })
    expect(child.parentTaskId).toBe(parent.id)
  })

  it('deletes tasks and cascades to stage_runs', () => {
    const task = createTask({ slug: 'temp', title: 'Temp', cwd: '/t' })
    createStageRun({ taskId: task.id, stage: 'pruefung' })
    expect(listStageRunsForTask(task.id)).toHaveLength(1)

    deleteTask(task.id)
    expect(getTaskById(task.id)).toBeNull()
    expect(listStageRunsForTask(task.id)).toHaveLength(0)
  })

  it('cascades delete to permissions, permission_requests, and audit_log', () => {
    const task = createTask({ slug: 'casc', title: 'Cascade', cwd: '/c' })
    const run = createStageRun({ taskId: task.id, stage: 'umsetzung' })
    createTaskPermission({
      taskId: task.id,
      tool: 'Bash',
      granted: true,
      preApproved: true,
    })
    createPermissionRequest({ stageRunId: run.id, tool: 'WebFetch' })
    appendAudit({ taskId: task.id, actor: 'user', action: 'created' })

    expect(listTaskPermissions(task.id)).toHaveLength(1)
    expect(listPendingPermissionRequests(run.id)).toHaveLength(1)
    expect(listAuditForTask(task.id).length).toBeGreaterThan(0)

    deleteTask(task.id)

    expect(listTaskPermissions(task.id)).toHaveLength(0)
    expect(listPendingPermissionRequests(run.id)).toHaveLength(0)
    expect(listAuditForTask(task.id)).toHaveLength(0)
  })

  it('sets parent_task_id to null when parent is deleted (follow-up tasks)', () => {
    const parent = createTask({ slug: 'par', title: 'P', cwd: '/p' })
    const child = createTask({
      slug: 'chi',
      title: 'C',
      cwd: '/p',
      parentTaskId: parent.id,
    })
    expect(child.parentTaskId).toBe(parent.id)

    deleteTask(parent.id)

    const refreshedChild = getTaskById(child.id)
    expect(refreshedChild).not.toBeNull()
    expect(refreshedChild?.parentTaskId).toBeNull()
  })

  it('rejects invalid current_stage via CHECK constraint', () => {
    const task = createTask({ slug: 'chk', title: 'CHK', cwd: '/chk' })
    expect(() => updateTask(task.id, { currentStage: 'bogus' as 'backlog' })).toThrow()
  })

  it('rejects invalid stage_run status via CHECK constraint', () => {
    const task = createTask({ slug: 'chk2', title: 'CHK2', cwd: '/chk2' })
    const run = createStageRun({ taskId: task.id, stage: 'planning' })
    expect(() => updateStageRun(run.id, { status: 'bogus' as 'running' })).toThrow()
  })
})

describe('stageRunsRepo', () => {
  it('creates and updates stage runs', () => {
    const task = createTask({ slug: 'sr', title: 'SR', cwd: '/sr' })
    const run = createStageRun({ taskId: task.id, stage: 'umsetzung', iteration: 1 })
    expect(run.status).toBe('pending')
    expect(run.iteration).toBe(1)

    const updated = updateStageRun(run.id, {
      sessionId: 'session-abc',
      sessionName: 'sr-umsetzung-iter-1',
      pid: 12345,
      status: 'running',
      startedAt: new Date().toISOString(),
    })
    expect(updated?.sessionId).toBe('session-abc')
    expect(updated?.status).toBe('running')
    expect(updated?.pid).toBe(12345)
  })

  it('finds latest stage run by task and stage', () => {
    const task = createTask({ slug: 'ls', title: 'LS', cwd: '/ls' })
    createStageRun({ taskId: task.id, stage: 'umsetzung', iteration: 0 })
    const r2 = createStageRun({ taskId: task.id, stage: 'umsetzung', iteration: 1 })
    const r3 = createStageRun({ taskId: task.id, stage: 'umsetzung', iteration: 2 })
    expect(r2.id).toBeDefined()

    const latest = getLatestStageRun(task.id, 'umsetzung')
    expect(latest?.id).toBe(r3.id)
  })

  it('finds stage run by session id', () => {
    const task = createTask({ slug: 'sid', title: 'SID', cwd: '/sid' })
    const run = createStageRun({ taskId: task.id, stage: 'planning' })
    updateStageRun(run.id, { sessionId: 'uuid-123' })
    const found = findStageRunBySessionId('uuid-123')
    expect(found?.id).toBe(run.id)
  })

  it('lists running stage runs for restart recovery', () => {
    const task = createTask({ slug: 'rr', title: 'RR', cwd: '/rr' })
    const r1 = createStageRun({ taskId: task.id, stage: 'umsetzung' })
    const r2 = createStageRun({ taskId: task.id, stage: 'selbstreview' })
    updateStageRun(r1.id, { status: 'running' })
    updateStageRun(r2.id, { status: 'on_hold' })
    createStageRun({ taskId: task.id, stage: 'finalisierung' }) // stays pending

    const running = listRunningStageRuns()
    expect(running).toHaveLength(2)
    expect(running.map(r => r.status).sort()).toEqual(['on_hold', 'running'])
  })

  it('stores output JSON correctly', () => {
    const task = createTask({ slug: 'out', title: 'Out', cwd: '/out' })
    const run = createStageRun({ taskId: task.id, stage: 'planning' })
    updateStageRun(run.id, { output: { findings: ['a', 'b'], score: 0.9 } })
    const fetched = findStageRunBySessionId(run.sessionId || '') || getLatestStageRun(task.id, 'planning')
    expect(fetched?.output).toEqual({ findings: ['a', 'b'], score: 0.9 })
  })
})

describe('permissionsRepo', () => {
  it('creates pre-approved task permissions', () => {
    const task = createTask({ slug: 'p1', title: 'P1', cwd: '/p1' })
    const perm = createTaskPermission({
      taskId: task.id,
      tool: 'Bash',
      pattern: 'npm *',
      granted: true,
      preApproved: true,
    })
    expect(perm.granted).toBe(true)
    expect(perm.preApproved).toBe(true)
    expect(perm.pattern).toBe('npm *')

    const list = listTaskPermissions(task.id)
    expect(list).toHaveLength(1)
  })

  it('handles runtime permission requests with resolution', () => {
    const task = createTask({ slug: 'rq', title: 'RQ', cwd: '/rq' })
    const run = createStageRun({ taskId: task.id, stage: 'umsetzung' })
    const req = createPermissionRequest({
      stageRunId: run.id,
      tool: 'WebFetch',
      reason: 'need to download package',
    })
    expect(req.outcome).toBeNull()

    const pending = listPendingPermissionRequests(run.id)
    expect(pending).toHaveLength(1)

    const resolved = resolvePermissionRequest(req.id, 'granted')
    expect(resolved?.outcome).toBe('granted')
    expect(resolved?.resolvedAt).toBeTruthy()

    expect(listPendingPermissionRequests(run.id)).toHaveLength(0)
  })
})

describe('auditRepo', () => {
  it('appends and lists audit entries in order', () => {
    const task = createTask({ slug: 'au', title: 'AU', cwd: '/au' })
    appendAudit({ taskId: task.id, actor: 'user', action: 'created' })
    appendAudit({
      taskId: task.id,
      actor: 'orchestrator',
      action: 'stage_transition',
      details: { from: 'backlog', to: 'pruefung' },
    })

    const log = listAuditForTask(task.id)
    expect(log).toHaveLength(2)
    expect(log[0].action).toBe('created')
    expect(log[1].details).toEqual({ from: 'backlog', to: 'pruefung' })
  })
})

describe('notificationConfigRepo', () => {
  it('upserts notification preferences', () => {
    setPreference('on_hold', ['email', 'browser'], true)
    const pref = listPreferences().find(p => p.eventType === 'on_hold')
    expect(pref?.channels).toEqual(['email', 'browser'])
    expect(pref?.enabled).toBe(true)

    setPreference('on_hold', ['system'], false)
    const updated = listPreferences().find(p => p.eventType === 'on_hold')
    expect(updated?.channels).toEqual(['system'])
    expect(updated?.enabled).toBe(false)
  })

  it('stores and retrieves adapter config', () => {
    setConfig('smtp_host', 'smtp.example.com')
    setConfig('webhook_url', 'https://discord.example/hook')

    expect(getConfig('smtp_host')).toBe('smtp.example.com')
    expect(getAllConfig()).toEqual({
      smtp_host: 'smtp.example.com',
      webhook_url: 'https://discord.example/hook',
    })
  })

  it('stores pipeline config with numeric helper', () => {
    setPipelineConfig('maxParallelOrchestrators', '5')
    expect(getPipelineConfigNumber('maxParallelOrchestrators', 3)).toBe(5)
    expect(getPipelineConfigNumber('missing', 7)).toBe(7)
  })
})

describe('api_keys table', () => {
  it('has the expected columns after migration', () => {
    const db = getDb()
    const cols = db.prepare('PRAGMA table_info(api_keys)').all() as Array<{ name: string }>
    const names = cols.map(c => c.name)
    expect(names).toContain('id')
    expect(names).toContain('name')
    expect(names).toContain('key_hash')
    expect(names).toContain('scopes')
    expect(names).toContain('active')
    expect(names).toContain('created_at')
    expect(names).toContain('last_used_at')
  })
})

describe('apiKeysRepo', () => {
  function makeToken(): string {
    return `mcp_${randomBytes(16).toString('hex')}`
  }
  function hashToken(t: string): string {
    return createHash('sha256').update(t).digest('hex')
  }

  it('creates a key and retrieves it by hash', () => {
    const token = makeToken()
    const key = createApiKey({ name: 'ci-bot', keyHash: hashToken(token), scopes: ['tasks:read', 'pipeline:control'] })
    expect(key.id).toBeTruthy()
    expect(key.name).toBe('ci-bot')
    expect(key.scopes).toEqual(['tasks:read', 'pipeline:control'])
    expect(key.active).toBe(true)
    expect(key.lastUsedAt).toBeNull()

    const found = getApiKeyByHash(hashToken(token))
    expect(found?.id).toBe(key.id)
  })

  it('returns null for unknown hash', () => {
    expect(getApiKeyByHash('not-a-real-hash')).toBeNull()
  })

  it('lists only active keys by default, includeRevoked shows all', () => {
    const t1 = makeToken()
    const t2 = makeToken()
    const k1 = createApiKey({ name: 'a', keyHash: hashToken(t1), scopes: ['tasks:read'] })
    const k2 = createApiKey({ name: 'b', keyHash: hashToken(t2), scopes: ['tasks:write'] })
    revokeApiKey(k2.id)

    const active = listApiKeys()
    expect(active.map(k => k.id)).toContain(k1.id)
    expect(active.map(k => k.id)).not.toContain(k2.id)

    const all = listApiKeys({ includeRevoked: true })
    expect(all).toHaveLength(2)
  })

  it('revokes a key — getApiKeyByHash returns null after revoke', () => {
    const token = makeToken()
    const key = createApiKey({ name: 'c', keyHash: hashToken(token), scopes: ['tasks:read'] })
    revokeApiKey(key.id)
    expect(getApiKeyByHash(hashToken(token))).toBeNull()
  })

  it('touchApiKey updates last_used_at', () => {
    const token = makeToken()
    const key = createApiKey({ name: 'd', keyHash: hashToken(token), scopes: ['tasks:read'] })
    expect(key.lastUsedAt).toBeNull()
    touchApiKey(key.id)
    const refreshed = getApiKeyByHash(hashToken(token))
    expect(refreshed?.lastUsedAt).toBeTruthy()
  })

  it('enforces unique name constraint', () => {
    createApiKey({ name: 'dup', keyHash: hashToken(makeToken()), scopes: ['tasks:read'] })
    expect(() => createApiKey({ name: 'dup', keyHash: hashToken(makeToken()), scopes: ['tasks:read'] })).toThrow()
  })

  it('getApiKeyById retrieves by primary key', () => {
    const token = makeToken()
    const key = createApiKey({ name: 'e', keyHash: hashToken(token), scopes: ['tasks:write'] })
    expect(getApiKeyById(key.id)?.name).toBe('e')
    expect(getApiKeyById('nonexistent')).toBeNull()
  })
})

describe('token helpers', () => {
  it('generateApiToken returns mcp_ prefixed 32-char hex string', () => {
    const token = generateApiToken()
    expect(token).toMatch(/^mcp_[0-9a-f]{32}$/)
  })

  it('hashApiToken produces stable sha256 hex', () => {
    const hash1 = hashApiToken('mcp_abc')
    const hash2 = hashApiToken('mcp_abc')
    expect(hash1).toBe(hash2)
    expect(hash1).toMatch(/^[0-9a-f]{64}$/)
  })

  it('two generateApiToken calls produce different tokens', () => {
    expect(generateApiToken()).not.toBe(generateApiToken())
  })
})

describe('legacy DB migration', () => {
  // This suite intentionally sidesteps the global beforeEach (which
  // initializes a fresh schema). It simulates a pre-existing dashboard
  // database that was created before silver_bullet/priority landed, and
  // verifies the runtime migration in client.ts can open it without
  // blowing up on the picker-index creation.

  // Track the legacy dir across the test body and teardown so that even
  // if an assertion throws mid-test, the env var is reset and the temp
  // directory is cleaned — otherwise the outer afterEach's `getDb()`
  // call on the NEXT test would open a stale legacy path and poison
  // downstream test files with a schemaless database.
  let legacyDir: string | null = null

  afterEach(() => {
    closeDb()
    delete process.env.DASHBOARD_DB_PATH
    if (legacyDir) {
      rmSync(legacyDir, { recursive: true, force: true })
      legacyDir = null
    }
  })

  it('migrates a legacy tasks table without silver_bullet/priority columns', async () => {
    closeDb()
    legacyDir = mkdtempSync(join(tmpdir(), 'dashboard-db-legacy-'))
    const legacyPath = join(legacyDir, 'legacy.db')

    const Database = (await import('better-sqlite3')).default
    const legacy = new Database(legacyPath)
    // Seed a minimal pre-migration tasks schema — the columns that
    // existed BEFORE this branch landed. CHECK constraints intentionally
    // omitted to match older schema variants.
    legacy.prepare(`
      CREATE TABLE tasks (
        id TEXT PRIMARY KEY,
        slug TEXT UNIQUE NOT NULL,
        title TEXT NOT NULL,
        description TEXT,
        cwd TEXT NOT NULL,
        worktree_path TEXT,
        source_branch TEXT,
        target_branch TEXT,
        current_stage TEXT NOT NULL,
        parent_task_id TEXT,
        max_iterations INTEGER NOT NULL DEFAULT 20,
        token_budget INTEGER,
        cost_budget_cents INTEGER,
        stage_timeout_seconds INTEGER NOT NULL DEFAULT 1800,
        created_at TEXT NOT NULL,
        updated_at TEXT NOT NULL,
        metadata TEXT
      )
    `).run()
    legacy.prepare(`
      INSERT INTO tasks (id, slug, title, cwd, current_stage, created_at, updated_at)
      VALUES ('legacy-1', 'legacy', 'Legacy task', '/x', 'backlog', '2026-01-01', '2026-01-01')
    `).run()
    legacy.close()

    process.env.DASHBOARD_DB_PATH = legacyPath
    // Opening the db must not throw — this is the exact failure path
    // that broke the live dashboard when the picker index was defined
    // in schema.sql (before the ALTER TABLE ran).
    const db = getDb()

    // The legacy row is still there with default values for the new cols.
    const row = db.prepare(`SELECT silver_bullet, priority FROM tasks WHERE id = 'legacy-1'`).get() as {
      silver_bullet: number
      priority: string
    }
    expect(row.silver_bullet).toBe(0)
    expect(row.priority).toBe('medium')

    // The picker index must exist now — created by the runtime migration
    // after the ALTER TABLE added the referenced columns.
    const indexes = db.prepare(`SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='tasks'`).all() as Array<{ name: string }>
    expect(indexes.some(i => i.name === 'idx_tasks_picker')).toBe(true)
    // Cleanup handled by the describe-local afterEach — do NOT close
    // or rm here, or a thrown assertion above would bypass teardown.
  })
})
