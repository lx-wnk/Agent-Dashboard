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
  addDependency,
  getDependenciesFor,
  getDependentsOf,
  isBlocked,
  removeDependency,
  removeDependencyById,
} from './taskDependenciesRepo.js'
import {
  createTask,
  deleteTask,
  getTaskById,
  getTaskBySlug,
  listPickableTasks,
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
    updateTask(t2.id, { currentStage: 'implementation' })

    const all = listTasks()
    expect(all).toHaveLength(2)

    const backlog = listTasksByStage('backlog')
    expect(backlog.map(t => t.id)).toEqual([t1.id])

    const impl = listTasksByStage('implementation')
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
    createStageRun({ taskId: task.id, stage: 'implementation' })
    expect(listStageRunsForTask(task.id)).toHaveLength(1)

    deleteTask(task.id)
    expect(getTaskById(task.id)).toBeNull()
    expect(listStageRunsForTask(task.id)).toHaveLength(0)
  })

  it('cascades delete to permissions, permission_requests, and audit_log', () => {
    const task = createTask({ slug: 'casc', title: 'Cascade', cwd: '/c' })
    const run = createStageRun({ taskId: task.id, stage: 'implementation' })
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
    const run = createStageRun({ taskId: task.id, stage: 'implementation' })
    expect(() => updateStageRun(run.id, { status: 'bogus' as 'running' })).toThrow()
  })

  it('getTaskById includes isBlocked=true when task has unmet dependency', async () => {
    const { addDependency: addDep } = await import('./taskDependenciesRepo.js')
    const a = createTask({ slug: 'blk-a', title: 'A', cwd: '/a' })
    const b = createTask({ slug: 'blk-b', title: 'B', cwd: '/b' })
    addDep(b.id, a.id)
    expect(getTaskById(b.id)?.isBlocked).toBe(true)
  })

  it('getTaskById includes isBlocked=false when dependency is met', async () => {
    const { addDependency: addDep } = await import('./taskDependenciesRepo.js')
    const a = createTask({ slug: 'blk-c', title: 'C', cwd: '/c' })
    const b = createTask({ slug: 'blk-d', title: 'D', cwd: '/d' })
    addDep(b.id, a.id)
    updateTask(a.id, { currentStage: 'done' })
    expect(getTaskById(b.id)?.isBlocked).toBe(false)
  })

  it('listPickableTasks excludes tasks with unmet dependencies', async () => {
    const { addDependency: addDep } = await import('./taskDependenciesRepo.js')
    const a = createTask({ slug: 'pkb-a', title: 'A', cwd: '/a' })
    const b = createTask({ slug: 'pkb-b', title: 'B', cwd: '/b' })
    addDep(b.id, a.id) // b waits for a (a is still backlog → not done)
    const pickable = listPickableTasks()
    const ids = pickable.map(t => t.id)
    expect(ids).toContain(a.id) // a has no deps, is pickable
    expect(ids).not.toContain(b.id) // b is blocked
  })

  it('listPickableTasks includes a task once all its deps are met', async () => {
    const { addDependency: addDep } = await import('./taskDependenciesRepo.js')
    const a = createTask({ slug: 'pkb-c', title: 'C', cwd: '/c' })
    const b = createTask({ slug: 'pkb-d', title: 'D', cwd: '/d' })
    addDep(b.id, a.id)
    updateTask(a.id, { currentStage: 'done' })
    const pickable = listPickableTasks()
    expect(pickable.map(t => t.id)).toContain(b.id)
  })
})

describe('stageRunsRepo', () => {
  it('creates and updates stage runs', () => {
    const task = createTask({ slug: 'sr', title: 'SR', cwd: '/sr' })
    const run = createStageRun({ taskId: task.id, stage: 'implementation', iteration: 1 })
    expect(run.status).toBe('pending')
    expect(run.iteration).toBe(1)

    const updated = updateStageRun(run.id, {
      sessionId: 'session-abc',
      sessionName: 'sr-implementation-iter-1',
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
    createStageRun({ taskId: task.id, stage: 'implementation', iteration: 0 })
    const r2 = createStageRun({ taskId: task.id, stage: 'implementation', iteration: 1 })
    const r3 = createStageRun({ taskId: task.id, stage: 'implementation', iteration: 2 })
    expect(r2.id).toBeDefined()

    const latest = getLatestStageRun(task.id, 'implementation')
    expect(latest?.id).toBe(r3.id)
  })

  it('finds stage run by session id', () => {
    const task = createTask({ slug: 'sid', title: 'SID', cwd: '/sid' })
    const run = createStageRun({ taskId: task.id, stage: 'implementation' })
    updateStageRun(run.id, { sessionId: 'uuid-123' })
    const found = findStageRunBySessionId('uuid-123')
    expect(found?.id).toBe(run.id)
  })

  it('lists running stage runs for restart recovery', () => {
    const task = createTask({ slug: 'rr', title: 'RR', cwd: '/rr' })
    const r1 = createStageRun({ taskId: task.id, stage: 'implementation' })
    const r2 = createStageRun({ taskId: task.id, stage: 'self_review' })
    updateStageRun(r1.id, { status: 'running' })
    updateStageRun(r2.id, { status: 'on_hold' })
    createStageRun({ taskId: task.id, stage: 'finalization' }) // stays pending

    const running = listRunningStageRuns()
    expect(running).toHaveLength(2)
    expect(running.map(r => r.status).sort()).toEqual(['on_hold', 'running'])
  })

  it('stores output JSON correctly', () => {
    const task = createTask({ slug: 'out', title: 'Out', cwd: '/out' })
    const run = createStageRun({ taskId: task.id, stage: 'self_review' })
    updateStageRun(run.id, { output: { findings: ['a', 'b'], score: 0.9 } })
    const fetched = findStageRunBySessionId(run.sessionId || '') || getLatestStageRun(task.id, 'self_review')
    expect(fetched?.output).toEqual({ findings: ['a', 'b'], score: 0.9 })
  })

  // V7 partial unique index defense-in-depth (one running stage_run per task).
  it('rejects flipping a second stage_run to running for the same task', () => {
    const task = createTask({ slug: 'urun', title: 'URUN', cwd: '/u' })
    const r1 = createStageRun({ taskId: task.id, stage: 'implementation', iteration: 0 })
    const r2 = createStageRun({ taskId: task.id, stage: 'implementation', iteration: 1 })
    updateStageRun(r1.id, { status: 'running' })
    expect(() => updateStageRun(r2.id, { status: 'running' })).toThrow(/UNIQUE|constraint/i)
  })

  it('allows the iterate ordering: old → done BEFORE new → running', () => {
    const task = createTask({ slug: 'iter', title: 'ITER', cwd: '/i' })
    const r1 = createStageRun({ taskId: task.id, stage: 'implementation', iteration: 0 })
    updateStageRun(r1.id, { status: 'running' })
    // iterate flow: flip OLD to done THEN create+flip NEW
    updateStageRun(r1.id, { status: 'done' })
    const r2 = createStageRun({ taskId: task.id, stage: 'implementation', iteration: 1 })
    expect(() => updateStageRun(r2.id, { status: 'running' })).not.toThrow()
  })

  it('allows running runs across DIFFERENT tasks (partial index is task-scoped)', () => {
    const t1 = createTask({ slug: 'urun-a', title: 'A', cwd: '/a' })
    const t2 = createTask({ slug: 'urun-b', title: 'B', cwd: '/b' })
    const r1 = createStageRun({ taskId: t1.id, stage: 'implementation' })
    const r2 = createStageRun({ taskId: t2.id, stage: 'implementation' })
    updateStageRun(r1.id, { status: 'running' })
    expect(() => updateStageRun(r2.id, { status: 'running' })).not.toThrow()
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
    const run = createStageRun({ taskId: task.id, stage: 'implementation' })
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
      details: { from: 'backlog', to: 'implementation' },
    })

    const log = listAuditForTask(task.id)
    expect(log).toHaveLength(2)
    expect(log[0].action).toBe('created')
    expect(log[1].details).toEqual({ from: 'backlog', to: 'implementation' })
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

describe('taskDependenciesRepo', () => {
  it('addDependency creates a row retrievable by getDependenciesFor', () => {
    const a = createTask({ slug: 'dep-a', title: 'A', cwd: '/a' })
    const b = createTask({ slug: 'dep-b', title: 'B', cwd: '/b' })
    addDependency(b.id, a.id, 'done', 'on_hold')
    const deps = getDependenciesFor(b.id)
    expect(deps).toHaveLength(1)
    expect(deps[0].dependsOnId).toBe(a.id)
    expect(deps[0].requiredStage).toBe('done')
    expect(deps[0].onCancelAction).toBe('on_hold')
  })

  it('getDependentsOf returns tasks waiting on a given task', () => {
    const a = createTask({ slug: 'dep-c', title: 'C', cwd: '/c' })
    const b = createTask({ slug: 'dep-d', title: 'D', cwd: '/d' })
    addDependency(b.id, a.id, 'done', 'cancel')
    const dependents = getDependentsOf(a.id)
    expect(dependents).toHaveLength(1)
    expect(dependents[0].taskId).toBe(b.id)
  })

  it('isBlocked returns true when prerequisite is not at required stage', () => {
    const a = createTask({ slug: 'dep-e', title: 'E', cwd: '/e' })
    const b = createTask({ slug: 'dep-f', title: 'F', cwd: '/f' })
    addDependency(b.id, a.id, 'done', 'on_hold')
    expect(isBlocked(b.id)).toBe(true)
  })

  it('isBlocked returns false when prerequisite has reached required stage', () => {
    const a = createTask({ slug: 'dep-g', title: 'G', cwd: '/g' })
    const b = createTask({ slug: 'dep-h', title: 'H', cwd: '/h' })
    addDependency(b.id, a.id, 'done', 'on_hold')
    updateTask(a.id, { currentStage: 'done' })
    expect(isBlocked(b.id)).toBe(false)
  })

  it('isBlocked returns false when task has no dependencies', () => {
    const a = createTask({ slug: 'dep-i', title: 'I', cwd: '/i' })
    expect(isBlocked(a.id)).toBe(false)
  })

  it('isBlocked true when at least one of two deps is unmet', () => {
    const a = createTask({ slug: 'dep-j', title: 'J', cwd: '/j' })
    const b = createTask({ slug: 'dep-k', title: 'K', cwd: '/k' })
    const c = createTask({ slug: 'dep-l', title: 'L', cwd: '/l' })
    addDependency(c.id, a.id, 'done', 'on_hold')
    addDependency(c.id, b.id, 'done', 'on_hold')
    updateTask(a.id, { currentStage: 'done' }) // only one done
    expect(isBlocked(c.id)).toBe(true)
  })

  it('removeDependency removes the row', () => {
    const a = createTask({ slug: 'dep-m', title: 'M', cwd: '/m' })
    const b = createTask({ slug: 'dep-n', title: 'N', cwd: '/n' })
    addDependency(b.id, a.id, 'done', 'on_hold')
    expect(getDependenciesFor(b.id)).toHaveLength(1)
    const removed = removeDependency(b.id, a.id)
    expect(removed).toBe(true)
    expect(getDependenciesFor(b.id)).toHaveLength(0)
  })

  it('addDependency rejects self-dependency', () => {
    const a = createTask({ slug: 'dep-o', title: 'O', cwd: '/o' })
    expect(() => addDependency(a.id, a.id, 'done', 'on_hold')).toThrow('cycle')
  })

  it('addDependency rejects direct cycle A→B then B→A', () => {
    const a = createTask({ slug: 'dep-p', title: 'P', cwd: '/p' })
    const b = createTask({ slug: 'dep-q', title: 'Q', cwd: '/q' })
    addDependency(b.id, a.id, 'done', 'on_hold')
    expect(() => addDependency(a.id, b.id, 'done', 'on_hold')).toThrow('cycle')
  })

  it('addDependency rejects 3-node cycle A→B→C then C→A', () => {
    const a = createTask({ slug: 'dep-r', title: 'R', cwd: '/r' })
    const b = createTask({ slug: 'dep-s', title: 'S', cwd: '/s' })
    const c = createTask({ slug: 'dep-t', title: 'T', cwd: '/t' })
    addDependency(b.id, a.id, 'done', 'on_hold')
    addDependency(c.id, b.id, 'done', 'on_hold')
    expect(() => addDependency(a.id, c.id, 'done', 'on_hold')).toThrow('cycle')
  })

  it('cascade: deleting a prerequisite removes its dependency rows', () => {
    const a = createTask({ slug: 'dep-u', title: 'U', cwd: '/u' })
    const b = createTask({ slug: 'dep-v', title: 'V', cwd: '/v' })
    addDependency(b.id, a.id, 'done', 'on_hold')
    deleteTask(a.id)
    expect(getDependenciesFor(b.id)).toHaveLength(0)
    expect(isBlocked(b.id)).toBe(false)
  })

  it('addDependency uses defaults: required_stage=done, on_cancel_action=on_hold', () => {
    const a = createTask({ slug: 'dep-w', title: 'W', cwd: '/w' })
    const b = createTask({ slug: 'dep-x', title: 'X', cwd: '/x' })
    addDependency(b.id, a.id)
    const deps = getDependenciesFor(b.id)
    expect(deps[0].requiredStage).toBe('done')
    expect(deps[0].onCancelAction).toBe('on_hold')
  })

  it('isBlocked false when prerequisite reaches cancelled and required_stage=cancelled', () => {
    const a = createTask({ slug: 'dep-y', title: 'Y', cwd: '/y' })
    const b = createTask({ slug: 'dep-z', title: 'Z', cwd: '/z' })
    addDependency(b.id, a.id, 'cancelled', 'on_hold')
    expect(isBlocked(b.id)).toBe(true)
    updateTask(a.id, { currentStage: 'cancelled' })
    expect(isBlocked(b.id)).toBe(false)
  })

  it('removeDependencyById removes by row id scoped to taskId', () => {
    const a = createTask({ slug: 'dep-aa', title: 'AA', cwd: '/aa' })
    const b = createTask({ slug: 'dep-ab', title: 'AB', cwd: '/ab' })
    const dep = addDependency(b.id, a.id, 'done', 'on_hold')
    expect(removeDependencyById(dep.id, 'wrong-task')).toBe(false)
    expect(getDependenciesFor(b.id)).toHaveLength(1)
    expect(removeDependencyById(dep.id, b.id)).toBe(true)
    expect(getDependenciesFor(b.id)).toHaveLength(0)
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

    const { Database } = await import('bun:sqlite')
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

  it('v5: narrows CHECK to canonical English tokens, audit_log batch-renames, schema bumps to 5', async () => {
    closeDb()
    legacyDir = mkdtempSync(join(tmpdir(), 'dashboard-db-v5-'))
    const legacyPath = join(legacyDir, 'legacy-v5.db')

    const { Database } = await import('bun:sqlite')
    const legacy = new Database(legacyPath)
    legacy.exec(`
      CREATE TABLE schema_version (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);
      INSERT INTO schema_version (version, applied_at) VALUES (3, '2026-04-01');
      CREATE TABLE tasks (
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
          'umsetzungskonzept','approval2','done','on_hold','cancelled'
        )),
        parent_task_id TEXT,
        max_iterations INTEGER NOT NULL DEFAULT 20,
        token_budget INTEGER,
        cost_budget_cents INTEGER,
        stage_timeout_seconds INTEGER NOT NULL DEFAULT 1800,
        created_at TEXT NOT NULL,
        updated_at TEXT NOT NULL,
        metadata TEXT,
        silver_bullet INTEGER NOT NULL DEFAULT 0,
        priority TEXT NOT NULL DEFAULT 'medium' CHECK (priority IN ('high','medium','low'))
      );
      CREATE TABLE stage_runs (
        id TEXT PRIMARY KEY,
        task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
        stage TEXT NOT NULL CHECK (stage IN (
          'concept','implementation','self_review','finalization',
          'konzept','umsetzung','selbstreview','finalisierung',
          'backlog','pruefung','refinement','planning','approval1',
          'umsetzungskonzept','approval2','done','on_hold','cancelled'
        )),
        session_id TEXT,
        session_name TEXT,
        pid INTEGER,
        status TEXT NOT NULL CHECK (status IN ('pending','running','awaiting_user','on_hold','done','failed')),
        started_at TEXT,
        ended_at TEXT,
        iteration INTEGER NOT NULL DEFAULT 0,
        output TEXT,
        tokens_used INTEGER NOT NULL DEFAULT 0,
        cost_cents INTEGER NOT NULL DEFAULT 0
      );
      CREATE TABLE audit_log (
        id TEXT PRIMARY KEY,
        task_id TEXT NOT NULL,
        actor TEXT NOT NULL,
        action TEXT NOT NULL,
        timestamp TEXT NOT NULL,
        details TEXT
      );
      INSERT INTO tasks (id, slug, title, cwd, current_stage, created_at, updated_at)
        VALUES ('legacy-v5-1', 'legacy-v5', 'Legacy V5', '/x', 'konzept', '2026-04-01', '2026-04-01');
      INSERT INTO stage_runs (id, task_id, stage, status, iteration)
        VALUES ('sr-v5-1', 'legacy-v5-1', 'umsetzung', 'done', 0);
      INSERT INTO audit_log (id, task_id, actor, action, timestamp, details)
        VALUES ('au-v5-1', 'legacy-v5-1', 'orchestrator', 'umsetzung_spawned', '2026-04-01', '{"from":"konzept","to":"umsetzung"}');
    `)
    legacy.close()

    process.env.DASHBOARD_DB_PATH = legacyPath
    const db = getDb()

    // V4 must have translated the legacy row from German → English.
    const taskRow = db.prepare(`SELECT current_stage FROM tasks WHERE id = 'legacy-v5-1'`).get() as { current_stage: string }
    expect(taskRow.current_stage).toBe('concept')
    const srRow = db.prepare(`SELECT stage FROM stage_runs WHERE id = 'sr-v5-1'`).get() as { stage: string }
    expect(srRow.stage).toBe('implementation')

    // V5 must reject any legacy German token going forward — narrow CHECK.
    expect(() => db.prepare(`UPDATE tasks SET current_stage = 'konzept' WHERE id = 'legacy-v5-1'`).run()).toThrow()
    expect(() => db.prepare(`UPDATE stage_runs SET stage = 'umsetzung' WHERE id = 'sr-v5-1'`).run()).toThrow()

    // Audit_log batch-rename: only action is rewritten — `details` is
    // free-form JSON and intentionally left untouched (see V5 migration).
    const audit = db.prepare(`SELECT action, details FROM audit_log WHERE id = 'au-v5-1'`).get() as { action: string, details: string }
    expect(audit.action).toBe('implementation_spawned')
    expect(audit.details).toBe('{"from":"konzept","to":"umsetzung"}')

    // schema_version bumped to current head (V7 = unique-running partial index on stage_runs).
    const version = db.prepare(`SELECT MAX(version) as v FROM schema_version`).get() as { v: number }
    expect(version.v).toBe(7)

    // V6 must have added last_grant_at to stage_runs.
    const srCols = db.prepare(`PRAGMA table_info(stage_runs)`).all() as Array<{ name: string }>
    expect(srCols.some(c => c.name === 'last_grant_at')).toBe(true)

    // Idempotency: re-running migrations on an already-current DB must be a no-op.
    closeDb()
    const reopened = getDb()
    const versionAgain = reopened.prepare(`SELECT MAX(version) as v FROM schema_version`).get() as { v: number }
    expect(versionAgain.v).toBe(7)
  })
})
