import type { StageHandler, StageTransition } from './types.js'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { listAuditForTask } from '../db/auditRepo.js'
import { closeDb, getDb } from '../db/client.js'
import { setPipelineConfig } from '../db/notificationConfigRepo.js'
import { createTaskPermission } from '../db/permissionsRepo.js'
import { createStageRun, getLatestStageRun, listStageRunsForTask, updateStageRun } from '../db/stageRunsRepo.js'
import { createTask, getTaskById } from '../db/tasksRepo.js'
import { PipelineOrchestrator } from './orchestrator.js'

let tmpDir: string
let orchestrator: PipelineOrchestrator

function makeStubHandler(
  stage: StageHandler['stage'],
  transition: StageTransition,
): StageHandler {
  return {
    stage,
    requiresAgent: false,
    async execute() {
      return transition
    },
  }
}

beforeEach(() => {
  tmpDir = mkdtempSync(join(tmpdir(), 'pipeline-orch-test-'))
  process.env.DASHBOARD_DB_PATH = join(tmpDir, 'test.db')
  getDb()
  orchestrator = new PipelineOrchestrator()
})

afterEach(() => {
  orchestrator.stop()
  closeDb()
  rmSync(tmpDir, { recursive: true, force: true })
  delete process.env.DASHBOARD_DB_PATH
})

describe('pipelineOrchestrator.progressTask', () => {
  it('transitions from backlog → pruefung and writes audit entries', async () => {
    const task = createTask({ slug: 'next', title: 'N', cwd: '/n' })

    await orchestrator.progressTask(task.id)

    const updated = getTaskById(task.id)
    expect(updated?.currentStage).toBe('pruefung')

    const audit = listAuditForTask(task.id)
    const actions = audit.map(a => a.action)
    expect(actions).toContain('stage_transition')
  })

  it('writes a done stage_run for the completed stage', async () => {
    const task = createTask({ slug: 'sr', title: 'SR', cwd: '/sr' })
    await orchestrator.progressTask(task.id)

    const backlogRun = getLatestStageRun(task.id, 'backlog')
    expect(backlogRun?.status).toBe('done')
    expect(backlogRun?.endedAt).toBeTruthy()
  })

  it('respects wait_user transition from approval stage', async () => {
    const task = createTask({ slug: 'ap', title: 'AP', cwd: '/ap' })
    // Move task to approval1 manually
    const { updateTask } = await import('../db/tasksRepo.js')
    updateTask(task.id, { currentStage: 'approval1' })

    const run = await orchestrator.progressTask(task.id)
    expect(run?.status).toBe('awaiting_user')

    // Task stage should NOT advance until user action
    expect(getTaskById(task.id)?.currentStage).toBe('approval1')
  })

  it('applies fail transition on handler errors', async () => {
    const task = createTask({ slug: 'err', title: 'ERR', cwd: '/err' })
    orchestrator.setHandler('backlog', {
      stage: 'backlog',
      requiresAgent: false,
      async execute() {
        throw new Error('boom')
      },
    })

    await orchestrator.progressTask(task.id)

    const t = getTaskById(task.id)
    expect(t?.currentStage).toBe('failed')
    const run = getLatestStageRun(task.id, 'backlog')
    expect(run?.status).toBe('failed')
    const output = run?.output as Record<string, unknown> | null
    expect(output?.error).toBe('boom')
  })

  it('iterates up to max_iterations then fails', async () => {
    const task = createTask({ slug: 'it', title: 'IT', cwd: '/it', maxIterations: 3 })
    const { updateTask } = await import('../db/tasksRepo.js')
    updateTask(task.id, { currentStage: 'umsetzung' })

    orchestrator.setHandler(
      'umsetzung',
      makeStubHandler('umsetzung', { kind: 'iterate' }),
    )

    for (let i = 0; i < 3; i++)
      await orchestrator.progressTask(task.id)

    const updated = getTaskById(task.id)
    expect(updated?.currentStage).toBe('failed')

    const runs = listStageRunsForTask(task.id).filter(r => r.stage === 'umsetzung')
    expect(runs.length).toBeGreaterThanOrEqual(3)
  })

  it('moves to on_hold and sets task stage to on_hold', async () => {
    const task = createTask({ slug: 'oh', title: 'OH', cwd: '/oh' })
    const { updateTask } = await import('../db/tasksRepo.js')
    updateTask(task.id, { currentStage: 'umsetzung' })

    orchestrator.setHandler(
      'umsetzung',
      makeStubHandler('umsetzung', {
        kind: 'on_hold',
        permissionRequestId: 'fake-id',
      }),
    )

    await orchestrator.progressTask(task.id)

    expect(getTaskById(task.id)?.currentStage).toBe('on_hold')
    const run = getLatestStageRun(task.id, 'umsetzung')
    expect(run?.status).toBe('on_hold')
  })

  it('respects maxParallelOrchestrators cap for umsetzung', async () => {
    setPipelineConfig('maxParallelOrchestrators', '1')

    const t1 = createTask({ slug: 't1', title: 'T1', cwd: '/t1' })
    const t2 = createTask({ slug: 't2', title: 'T2', cwd: '/t2' })
    const { updateTask } = await import('../db/tasksRepo.js')
    updateTask(t1.id, { currentStage: 'umsetzung' })
    updateTask(t2.id, { currentStage: 'umsetzung' })

    // Mark one umsetzung run as running to fill the slot
    const run1 = createStageRun({ taskId: t1.id, stage: 'umsetzung' })
    updateStageRun(run1.id, { status: 'running' })

    // Handler should not even be invoked for t2 since slot is full
    let t2Invoked = false
    orchestrator.setHandler('umsetzung', {
      stage: 'umsetzung',
      requiresAgent: true,
      async execute(ctx) {
        if (ctx.task.id === t2.id)
          t2Invoked = true
        return { kind: 'done' }
      },
    })

    const result = await orchestrator.progressTask(t2.id)
    expect(result).toBeNull()
    expect(t2Invoked).toBe(false)
  })

  it('does not progress terminal tasks (done/failed/cancelled)', async () => {
    const task = createTask({ slug: 'tm', title: 'TM', cwd: '/tm' })
    const { updateTask } = await import('../db/tasksRepo.js')
    updateTask(task.id, { currentStage: 'done' })

    const result = await orchestrator.progressTask(task.id)
    expect(result).toBeNull()
  })

  it('passes pre-approved permissions to the handler context', async () => {
    const task = createTask({ slug: 'pp', title: 'PP', cwd: '/pp' })
    createTaskPermission({
      taskId: task.id,
      tool: 'Bash',
      pattern: 'npm *',
      granted: true,
      preApproved: true,
    })

    let receivedPermissions: unknown
    orchestrator.setHandler('backlog', {
      stage: 'backlog',
      requiresAgent: false,
      async execute(ctx) {
        receivedPermissions = ctx.permissions
        return { kind: 'next', toStage: 'pruefung' }
      },
    })

    await orchestrator.progressTask(task.id)
    expect(Array.isArray(receivedPermissions)).toBe(true)
    expect((receivedPermissions as unknown[]).length).toBe(1)
  })
})

describe('pipelineOrchestrator.start recovery', () => {
  it('logs a recovery decision and flips dead running runs to pending/failed', async () => {
    const task = createTask({ slug: 'rec', title: 'REC', cwd: '/rec' })

    // Dead PID + session_id → should become 'pending' (resume path)
    const run1 = createStageRun({ taskId: task.id, stage: 'umsetzung' })
    updateStageRun(run1.id, { status: 'running', sessionId: 'sess-1', pid: 2147483647 })

    // Dead PID + no session → should become 'failed'
    const run2 = createStageRun({ taskId: task.id, stage: 'selbstreview' })
    updateStageRun(run2.id, { status: 'running', pid: 2147483647 })

    const o = new PipelineOrchestrator(10000)
    o.start()

    const refreshedRun1 = getLatestStageRun(task.id, 'umsetzung')
    const refreshedRun2 = getLatestStageRun(task.id, 'selbstreview')
    expect(refreshedRun1?.status).toBe('pending')
    expect(refreshedRun2?.status).toBe('failed')

    const audit = listAuditForTask(task.id)
    expect(audit.filter(a => a.action === 'recovery_decision')).toHaveLength(2)
    o.stop()
  })
})

describe('pipelineOrchestrator concurrency', () => {
  it('serializes concurrent progressTask calls for the same task', async () => {
    const task = createTask({ slug: 'cc', title: 'CC', cwd: '/cc' })

    let activeHandlers = 0
    let peak = 0
    let totalInvocations = 0
    orchestrator.setHandler('backlog', {
      stage: 'backlog',
      requiresAgent: false,
      async execute() {
        activeHandlers++
        peak = Math.max(peak, activeHandlers)
        totalInvocations++
        await new Promise(r => setTimeout(r, 10))
        activeHandlers--
        return { kind: 'next', toStage: 'pruefung' }
      },
    })

    // Fire three concurrent calls.
    const results = await Promise.all([
      orchestrator.progressTask(task.id),
      orchestrator.progressTask(task.id),
      orchestrator.progressTask(task.id),
    ])

    expect(peak).toBe(1) // handler never ran in parallel
    expect(totalInvocations).toBeGreaterThanOrEqual(1)
    // Task has advanced past backlog (stub handlers will carry it forward
    // but the key guarantee is the serialization, not the final stage)
    expect(getTaskById(task.id)?.currentStage).not.toBe('backlog')
    expect(results.filter(r => r !== null).length).toBeGreaterThanOrEqual(1)
  })
})
