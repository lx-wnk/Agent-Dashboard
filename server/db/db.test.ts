import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
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
