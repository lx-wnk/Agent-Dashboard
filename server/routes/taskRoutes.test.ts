import type express from 'express'
import type { AddressInfo } from 'node:net'
import type { TaskEvent } from './taskRoutes.js'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'
import expressLib from 'express'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { closeDb, getDb } from '../db/client.js'
import { addDependency } from '../db/taskDependenciesRepo.js'
import { createTask, updateTask } from '../db/tasksRepo.js'
import { PipelineOrchestrator } from '../pipeline/orchestrator.js'
import { createTaskRouter } from './taskRoutes.js'

/**
 * Test helper — bypasses PATCH's stage-write block which exists for
 * client-facing safety. Tests need to put a task into a specific stage
 * to exercise downstream logic.
 */
function forceStage(id: string, stage: Parameters<typeof updateTask>[1]['currentStage']) {
  updateTask(id, { currentStage: stage })
}

let tmpDir: string
let server: ReturnType<express.Express['listen']>
let baseUrl: string
let events: TaskEvent[]
let orchestrator: PipelineOrchestrator

beforeEach(async () => {
  tmpDir = mkdtempSync(join(tmpdir(), 'task-routes-test-'))
  process.env.DASHBOARD_DB_PATH = join(tmpDir, 'test.db')
  getDb()

  events = []
  orchestrator = new PipelineOrchestrator()
  const app = expressLib()
  app.use(expressLib.json())
  app.use('/api', createTaskRouter({
    rejectCrossOrigin: () => false, // allow everything in tests
    orchestrator,
    broadcastTaskEvent: (e) => { events.push(e) },
  }))

  server = await new Promise<ReturnType<express.Express['listen']>>((resolve) => {
    const s = app.listen(0, '127.0.0.1', () => resolve(s))
  })
  const addr = server.address() as AddressInfo
  baseUrl = `http://127.0.0.1:${addr.port}/api`
})

afterEach(() => {
  orchestrator.stop()
  server?.close()
  closeDb()
  rmSync(tmpDir, { recursive: true, force: true })
  delete process.env.DASHBOARD_DB_PATH
})

async function api<T = unknown>(method: string, path: string, body?: unknown): Promise<{ status: number, data: T }> {
  const res = await fetch(baseUrl + path, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  const text = await res.text()
  const data = text ? JSON.parse(text) as T : ({} as T)
  return { status: res.status, data }
}

describe('pOST /api/tasks', () => {
  it('creates a task with minimum fields', async () => {
    const { status, data } = await api<{ id: string, slug: string }>('POST', '/tasks', {
      slug: 'fix-bug',
      title: 'Fix a bug',
      cwd: '/tmp',
    })
    expect(status).toBe(201)
    expect(data.slug).toBe('fix-bug')
    expect(events.some(e => e.type === 'task_created')).toBe(true)
  })

  it('rejects invalid slug', async () => {
    const { status, data } = await api<{ error: string }>('POST', '/tasks', {
      slug: 'Invalid Slug!',
      title: 'x',
      cwd: '/tmp',
    })
    expect(status).toBe(400)
    expect(data.error).toContain('slug')
  })

  it('rejects duplicate slug', async () => {
    await api('POST', '/tasks', { slug: 'dup', title: 'A', cwd: '/a' })
    const { status } = await api('POST', '/tasks', { slug: 'dup', title: 'B', cwd: '/b' })
    expect(status).toBe(409)
  })

  it('requires title and cwd', async () => {
    const r1 = await api('POST', '/tasks', { slug: 'nx', cwd: '/t' })
    expect(r1.status).toBe(400)
    const r2 = await api('POST', '/tasks', { slug: 'nx', title: 'T' })
    expect(r2.status).toBe(400)
  })
})

describe('gET /api/tasks', () => {
  it('lists tasks and filters by stage', async () => {
    await api('POST', '/tasks', { slug: 'a', title: 'A', cwd: '/a' })
    const { data: b } = await api<{ id: string }>('POST', '/tasks', {
      slug: 'b',
      title: 'B',
      cwd: '/b',
    })
    forceStage(b.id, 'umsetzung')

    const all = await api<unknown[]>('GET', '/tasks')
    expect(all.data).toHaveLength(2)

    const impl = await api<unknown[]>('GET', '/tasks?stage=umsetzung')
    expect(impl.data).toHaveLength(1)

    const invalid = await api('GET', '/tasks?stage=nope')
    expect(invalid.status).toBe(400)
  })
})

describe('pATCH /api/tasks', () => {
  it('rejects currentStage writes — must use /progress or /approve', async () => {
    const { data: t } = await api<{ id: string }>('POST', '/tasks', {
      slug: 'px',
      title: 'PX',
      cwd: '/px',
    })
    const { status, data } = await api<{ error: string }>(
      'PATCH',
      `/tasks/${t.id}`,
      { currentStage: 'done' },
    )
    expect(status).toBe(400)
    expect(data.error).toContain('currentStage cannot be set')
  })

  it('accepts whitelisted field updates', async () => {
    const { data: t } = await api<{ id: string }>('POST', '/tasks', {
      slug: 'pw',
      title: 'PW',
      cwd: '/pw',
    })
    const { status, data } = await api<{ title: string, tokenBudget: number | null }>(
      'PATCH',
      `/tasks/${t.id}`,
      { title: 'New Title', tokenBudget: 100000 },
    )
    expect(status).toBe(200)
    expect(data.title).toBe('New Title')
    expect(data.tokenBudget).toBe(100000)
  })

  it('ignores non-whitelisted fields like cwd', async () => {
    const { data: t } = await api<{ id: string }>('POST', '/tasks', {
      slug: 'pc',
      title: 'PC',
      cwd: '/pc',
    })
    const { data } = await api<{ cwd: string }>(
      'PATCH',
      `/tasks/${t.id}`,
      { cwd: '/hijacked' },
    )
    expect(data.cwd).toBe('/pc')
  })
})

describe('pOST /api/tasks/:id/progress', () => {
  it('advances a task through the backlog → umsetzung transition', async () => {
    const { data: task } = await api<{ id: string }>('POST', '/tasks', {
      slug: 'prog',
      title: 'P',
      cwd: '/p',
    })

    const { status, data } = await api<{ task: { currentStage: string } }>(
      'POST',
      `/tasks/${task.id}/progress`,
    )
    expect(status).toBe(200)
    expect(data.task.currentStage).toBe('umsetzung')
    expect(events.some(e => e.type === 'task_updated')).toBe(true)
  })
})

describe('pOST /api/tasks/:id/cancel', () => {
  it('sets task stage to cancelled', async () => {
    const { data: task } = await api<{ id: string }>('POST', '/tasks', {
      slug: 'cx',
      title: 'C',
      cwd: '/c',
    })
    const { data } = await api<{ currentStage: string }>('POST', `/tasks/${task.id}/cancel`)
    expect(data.currentStage).toBe('cancelled')
  })
})

describe('task permissions endpoints', () => {
  it('creates and lists task permissions', async () => {
    const { data: task } = await api<{ id: string }>('POST', '/tasks', {
      slug: 'pp',
      title: 'PP',
      cwd: '/pp',
    })
    const create = await api('POST', `/tasks/${task.id}/permissions`, {
      tool: 'Bash',
      pattern: 'npm *',
      granted: true,
      preApproved: true,
    })
    expect(create.status).toBe(201)

    const list = await api<unknown[]>('GET', `/tasks/${task.id}/permissions`)
    expect(list.data).toHaveLength(1)
  })

  it('rejects permission creation without tool', async () => {
    const { data: task } = await api<{ id: string }>('POST', '/tasks', {
      slug: 'np',
      title: 'NP',
      cwd: '/np',
    })
    const { status } = await api('POST', `/tasks/${task.id}/permissions`, { granted: true })
    expect(status).toBe(400)
  })
})

describe('permission request resolution', () => {
  it('granting a permission request creates a task permission and resumes', async () => {
    const { data: task } = await api<{ id: string }>('POST', '/tasks', {
      slug: 'pr',
      title: 'PR',
      cwd: '/pr',
    })

    const { createStageRun } = await import('../db/stageRunsRepo.js')
    const run = createStageRun({ taskId: task.id, stage: 'umsetzung' })

    const { data: reqRow } = await api<{ id: string }>('POST', '/permission-requests', {
      stageRunId: run.id,
      tool: 'WebFetch',
      reason: 'need to fetch docs',
    })

    const { status, data } = await api<{ outcome: string }>(
      'POST',
      `/permission-requests/${reqRow.id}/resolve`,
      { outcome: 'granted' },
    )
    expect(status).toBe(200)
    expect(data.outcome).toBe('granted')

    // Task now has a permission entry
    const perms = await api<unknown[]>('GET', `/tasks/${task.id}/permissions`)
    expect(perms.data.length).toBeGreaterThanOrEqual(1)
  })

  it('rejects invalid outcome', async () => {
    const { data: task } = await api<{ id: string }>('POST', '/tasks', {
      slug: 'ix',
      title: 'IX',
      cwd: '/ix',
    })
    const { createStageRun } = await import('../db/stageRunsRepo.js')
    const run = createStageRun({ taskId: task.id, stage: 'umsetzung' })
    const { data: reqRow } = await api<{ id: string }>('POST', '/permission-requests', {
      stageRunId: run.id,
      tool: 'WebFetch',
    })
    const { status } = await api('POST', `/permission-requests/${reqRow.id}/resolve`, {
      outcome: 'maybe',
    })
    expect(status).toBe(400)
  })
})

describe('pipeline config', () => {
  it('gets default and updates maxParallelOrchestrators', async () => {
    const { data: initial } = await api<{ maxParallelOrchestrators: number }>('GET', '/pipeline/config')
    expect(initial.maxParallelOrchestrators).toBe(3)

    const { data: updated } = await api<{ maxParallelOrchestrators: number }>('PUT', '/pipeline/config', {
      maxParallelOrchestrators: 5,
    })
    expect(updated.maxParallelOrchestrators).toBe(5)
  })

  it('rejects out-of-range values', async () => {
    const { status } = await api('PUT', '/pipeline/config', { maxParallelOrchestrators: 0 })
    expect(status).toBe(400)
  })
})

describe('task enrichment (needsUser)', () => {
  it('flags needsUser=true when current stage run is awaiting_user', async () => {
    const { data: t } = await api<{ id: string }>('POST', '/tasks', {
      slug: 'au',
      title: 'AU',
      cwd: '/au',
    })
    forceStage(t.id, 'umsetzung')
    const { createStageRun, updateStageRun } = await import('../db/stageRunsRepo.js')
    const run = createStageRun({ taskId: t.id, stage: 'umsetzung' })
    updateStageRun(run.id, { status: 'awaiting_user', startedAt: new Date().toISOString() })

    const { data } = await api<{ needsUser: boolean }>('GET', `/tasks/${t.id}`)
    expect(data.needsUser).toBe(true)
  })

  it('does NOT flag needsUser when a newer iteration supersedes an awaiting_user run', async () => {
    // Regression: after an iterate transition, the new pending stage_run has
    // null started_at while the old awaiting_user run has it set. Ordering
    // must prioritize iteration DESC so the new run wins.
    const { data: t } = await api<{ id: string }>('POST', '/tasks', {
      slug: 'itr',
      title: 'ITR',
      cwd: '/itr',
    })
    forceStage(t.id, 'umsetzung')
    const { createStageRun, updateStageRun } = await import('../db/stageRunsRepo.js')

    // Old iteration 0 paused at awaiting_user
    const oldRun = createStageRun({ taskId: t.id, stage: 'umsetzung', iteration: 0 })
    updateStageRun(oldRun.id, {
      status: 'awaiting_user',
      startedAt: '2026-04-11T10:00:00Z',
    })

    // New iteration 1 just enqueued — no started_at yet
    createStageRun({ taskId: t.id, stage: 'umsetzung', iteration: 1 })

    const { data } = await api<{ needsUser: boolean, currentIteration: number }>(
      'GET',
      `/tasks/${t.id}`,
    )
    expect(data.needsUser).toBe(false)
    expect(data.currentIteration).toBe(1)
  })

  it('does NOT flag needsUser when stale awaiting_user belongs to a prior stage', async () => {
    const { data: t } = await api<{ id: string }>('POST', '/tasks', {
      slug: 'stale',
      title: 'Stale',
      cwd: '/stale',
    })
    const { createStageRun, updateStageRun } = await import('../db/stageRunsRepo.js')

    // Task was paused at umsetzung with awaiting_user at some past point
    const oldRun = createStageRun({ taskId: t.id, stage: 'umsetzung' })
    updateStageRun(oldRun.id, { status: 'awaiting_user', startedAt: '2026-04-11T10:00:00Z' })

    // Then it advanced to backlog (unusual but this is the stale-run scenario)
    // and the new stage has no stage_run yet
    forceStage(t.id, 'pruefung')

    const { data } = await api<{ needsUser: boolean, latestStageRunStatus: string | null }>(
      'GET',
      `/tasks/${t.id}`,
    )
    expect(data.needsUser).toBe(false)
    expect(data.latestStageRunStatus).toBeNull()
  })
})

describe('notification endpoints', () => {
  it('sets and lists notification preferences', async () => {
    await api('PUT', '/notifications/preferences/on_hold', {
      channels: ['email', 'browser'],
      enabled: true,
    })
    const { data } = await api<Array<{ eventType: string, channels: string[] }>>(
      'GET',
      '/notifications/preferences',
    )
    const onHold = data.find(p => p.eventType === 'on_hold')
    expect(onHold?.channels).toEqual(['email', 'browser'])
  })

  it('rejects unknown event type', async () => {
    const { status } = await api('PUT', '/notifications/preferences/unknown', {
      channels: ['email'],
    })
    expect(status).toBe(400)
  })

  it('stores and retrieves adapter config', async () => {
    await api('PUT', '/notifications/config', {
      smtp_host: 'smtp.example.com',
      webhook_url: 'https://hooks.example.com/abc',
    })
    const { data } = await api<Record<string, string>>('GET', '/notifications/config')
    expect(data.smtp_host).toBe('smtp.example.com')
    expect(data.webhook_url).toBe('https://hooks.example.com/abc')
  })
})

describe('dependency routes', () => {
  it('pOST /tasks/:id/dependencies adds a dependency', async () => {
    const a = createTask({ slug: 'drt-a', title: 'A', cwd: '/a' })
    const b = createTask({ slug: 'drt-b', title: 'B', cwd: '/b' })
    const { status, data } = await api<{ dependsOnId: string }>(
      'POST',
      `/tasks/${b.id}/dependencies`,
      { dependsOnId: a.id, requiredStage: 'done', onCancelAction: 'cancel' },
    )
    expect(status).toBe(201)
    expect(data.dependsOnId).toBe(a.id)
  })

  it('pOST /tasks/:id/dependencies returns 400 on cycle', async () => {
    const a = createTask({ slug: 'drt-c', title: 'C', cwd: '/c' })
    const b = createTask({ slug: 'drt-d', title: 'D', cwd: '/d' })
    await api('POST', `/tasks/${b.id}/dependencies`, { dependsOnId: a.id })
    const { status, data } = await api<{ error: string }>(
      'POST',
      `/tasks/${a.id}/dependencies`,
      { dependsOnId: b.id },
    )
    expect(status).toBe(400)
    expect(data.error).toMatch(/cycle/)
  })

  it('gET /tasks/:id/dependencies lists prerequisites', async () => {
    const a = createTask({ slug: 'drt-e', title: 'E', cwd: '/e' })
    const b = createTask({ slug: 'drt-f', title: 'F', cwd: '/f' })
    addDependency(b.id, a.id)
    const { status, data } = await api<Array<{ dependsOnId: string }>>(
      'GET',
      `/tasks/${b.id}/dependencies`,
    )
    expect(status).toBe(200)
    expect(data).toHaveLength(1)
    expect(data[0].dependsOnId).toBe(a.id)
  })

  it('gET /tasks/:id/dependents lists dependents', async () => {
    const a = createTask({ slug: 'drt-g', title: 'G', cwd: '/g' })
    const b = createTask({ slug: 'drt-h', title: 'H', cwd: '/h' })
    addDependency(b.id, a.id)
    const { status, data } = await api<Array<{ taskId: string }>>(
      'GET',
      `/tasks/${a.id}/dependents`,
    )
    expect(status).toBe(200)
    expect(data).toHaveLength(1)
    expect(data[0].taskId).toBe(b.id)
  })

  it('dELETE /tasks/:id/dependencies/:depId removes a dependency', async () => {
    const a = createTask({ slug: 'drt-i', title: 'I', cwd: '/i' })
    const b = createTask({ slug: 'drt-j', title: 'J', cwd: '/j' })
    const dep = addDependency(b.id, a.id)
    const { status, data } = await api<{ removed: boolean }>(
      'DELETE',
      `/tasks/${b.id}/dependencies/${dep.id}`,
    )
    expect(status).toBe(200)
    expect(data.removed).toBe(true)
  })

  it('pOST /tasks/:id/dependencies returns 404 when task not found', async () => {
    const { status } = await api('POST', '/tasks/nonexistent-id/dependencies', { dependsOnId: 'other-id' })
    expect(status).toBe(404)
  })

  it('pOST /tasks/:id/dependencies returns 409 on duplicate', async () => {
    const a = createTask({ slug: 'drt-dup-a', title: 'A', cwd: '/dup-a' })
    const b = createTask({ slug: 'drt-dup-b', title: 'B', cwd: '/dup-b' })
    await api('POST', `/tasks/${b.id}/dependencies`, { dependsOnId: a.id })
    const { status } = await api('POST', `/tasks/${b.id}/dependencies`, { dependsOnId: a.id })
    expect(status).toBe(409)
  })
})
