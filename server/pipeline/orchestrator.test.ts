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
import { addDependency, isBlocked } from '../db/taskDependenciesRepo.js'
import { createTask, getTaskById, updateTask } from '../db/tasksRepo.js'
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
  it('transitions from backlog → umsetzung and writes audit entries', async () => {
    const task = createTask({ slug: 'next', title: 'N', cwd: '/n' })

    await orchestrator.progressTask(task.id)

    const updated = getTaskById(task.id)
    expect(updated?.currentStage).toBe('umsetzung')

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

    // `failed` is a stage_run status now, not a task stage. The task stays
    // on its original stage so the frontend can surface it in "Needs You"
    // with Retry + Analyze actions.
    const t = getTaskById(task.id)
    expect(t?.currentStage).toBe('backlog')
    const run = getLatestStageRun(task.id, 'backlog')
    expect(run?.status).toBe('failed')
    const output = run?.output as Record<string, unknown> | null
    expect(output?.error).toBe('boom')
  })

  it('fires onStageFailed callback when a stage_run flips to failed', async () => {
    const events: Array<{ taskId: string, stage: string, iteration: number, error: string }> = []
    const orch = new PipelineOrchestrator({
      onStageFailed: (taskId, info) => {
        events.push({ taskId, stage: info.stage, iteration: info.iteration, error: info.error })
      },
    })
    const task = createTask({ slug: 'cb', title: 'CB', cwd: '/cb' })
    orch.setHandler('backlog', {
      stage: 'backlog',
      requiresAgent: false,
      async execute() {
        throw new Error('boom')
      },
    })

    await orch.progressTask(task.id)

    expect(events).toHaveLength(1)
    expect(events[0].taskId).toBe(task.id)
    expect(events[0].stage).toBe('backlog')
    expect(events[0].iteration).toBe(0)
    expect(events[0].error).toBe('boom')
  })

  it('keeps failed tasks out of the auto-picker (require explicit retry)', async () => {
    const orch = new PipelineOrchestrator()
    const task = createTask({ slug: 'np', title: 'NP', cwd: '/np' })
    orch.setHandler('backlog', {
      stage: 'backlog',
      requiresAgent: false,
      async execute() {
        throw new Error('boom')
      },
    })
    await orch.progressTask(task.id)

    // Task stays on backlog with a failed run. A fresh tick must NOT
    // auto-retry it — the runner picker excludes tasks whose latest
    // stage_run is 'failed'. We verify this by counting backlog runs
    // after a tick: still exactly 1 (the failed one).
    await orch.tick()

    const runs = listStageRunsForTask(task.id).filter(r => r.stage === 'backlog')
    expect(runs).toHaveLength(1)
    expect(runs[0].status).toBe('failed')
  })

  it('iterates up to max_iterations then marks the latest run failed (task stays on stage)', async () => {
    const task = createTask({ slug: 'it', title: 'IT', cwd: '/it', maxIterations: 3 })
    const { updateTask } = await import('../db/tasksRepo.js')
    updateTask(task.id, { currentStage: 'umsetzung' })

    orchestrator.setHandler(
      'umsetzung',
      makeStubHandler('umsetzung', { kind: 'iterate' }),
    )

    for (let i = 0; i < 3; i++)
      await orchestrator.progressTask(task.id)

    // Task does NOT flip to `failed`. Instead the latest stage_run is
    // marked failed so the UI can offer Retry/Analyze without a schema
    // migration or a terminal "failed" pseudo-stage.
    const updated = getTaskById(task.id)
    expect(updated?.currentStage).toBe('umsetzung')

    const runs = listStageRunsForTask(task.id).filter(r => r.stage === 'umsetzung')
    expect(runs.length).toBeGreaterThanOrEqual(3)
    const latest = runs[runs.length - 1]
    expect(latest.status).toBe('failed')
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

  it('respects maxParallelOrchestrators cap for any agent stage', async () => {
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
        return { kind: 'next', toStage: 'umsetzung' }
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

describe('pipelineOrchestrator.tick - driver loop', () => {
  // All real agent-stages would otherwise spawn `claude` via the driver
  // loop's eager pickup. Park every agent stage on wait_user so tests can
  // exercise the finalizer branches without ENOENTing on child_process.
  function parkAllAgentStages(o: PipelineOrchestrator): void {
    for (const stage of ['umsetzung', 'selbstreview', 'finalisierung'] as const) {
      o.setHandler(stage, {
        stage,
        requiresAgent: true,
        async execute() {
          return { kind: 'wait_user', reason: `test park ${stage}` }
        },
      })
    }
  }

  it('auto-promotes a backlog task to umsetzung on tick', async () => {
    const task = createTask({ slug: 'bp', title: 'BP', cwd: '/bp' })
    parkAllAgentStages(orchestrator)

    await orchestrator.tick()
    // Allow the fire-and-forget progressTask chain a microtask to flush.
    await new Promise(r => setImmediate(r))

    expect(getTaskById(task.id)?.currentStage).toBe('umsetzung')
  })

  it('finalizes a completed async stage_run and advances to the next stage', async () => {
    const task = createTask({ slug: 'ok', title: 'OK', cwd: '/ok' })
    const { updateTask } = await import('../db/tasksRepo.js')
    updateTask(task.id, { currentStage: 'umsetzung' })
    const run = createStageRun({ taskId: task.id, stage: 'umsetzung' })
    updateStageRun(run.id, { status: 'running', pid: 9999 })

    parkAllAgentStages(orchestrator)
    // Park umsetzung but override its completion: the completion detector
    // returns 'completed', so the orchestrator will advance to the next
    // stage (selbstreview) regardless of the parked handler.
    orchestrator.setCompletionDetector(async () => ({
      kind: 'completed',
      output: { changed: ['file.ts'] },
    }))

    await orchestrator.tick()
    await new Promise(r => setImmediate(r))

    expect(getTaskById(task.id)?.currentStage).toBe('selbstreview')
    const updatedRun = getLatestStageRun(task.id, 'umsetzung')
    expect(updatedRun?.status).toBe('done')
  })

  it('iterates with validation feedback on the first schema rejection', async () => {
    const task = createTask({ slug: 'vr', title: 'VR', cwd: '/vr' })
    const { updateTask } = await import('../db/tasksRepo.js')
    updateTask(task.id, { currentStage: 'selbstreview' })
    const run = createStageRun({ taskId: task.id, stage: 'selbstreview' })
    updateStageRun(run.id, { status: 'running', pid: 9999 })

    parkAllAgentStages(orchestrator)
    orchestrator.setCompletionDetector(async () => ({
      kind: 'failed',
      error: 'missing required field: summary',
      output: { passed: true, findings: [] },
      retryable: true,
    }))

    await orchestrator.tick()
    await new Promise(r => setImmediate(r))

    // Task stays on selbstreview; old run is done with validation_error payload;
    // a new iteration row has been inserted.
    expect(getTaskById(task.id)?.currentStage).toBe('selbstreview')
    const runs = listStageRunsForTask(task.id).filter(r => r.stage === 'selbstreview')
    expect(runs.length).toBe(2)
    const oldRun = runs.find(r => r.iteration === 0)
    expect(oldRun?.status).toBe('done')
    const oldOutput = oldRun?.output as Record<string, unknown> | null
    expect(oldOutput?.validation_error).toContain('summary')
  })

  it('escalates to awaiting_user on the second schema rejection', async () => {
    const task = createTask({ slug: 'es', title: 'ES', cwd: '/es' })
    const { updateTask } = await import('../db/tasksRepo.js')
    updateTask(task.id, { currentStage: 'selbstreview' })
    const run = createStageRun({ taskId: task.id, stage: 'selbstreview', iteration: 1 })
    updateStageRun(run.id, { status: 'running', pid: 9999 })

    parkAllAgentStages(orchestrator)
    orchestrator.setCompletionDetector(async () => ({
      kind: 'failed',
      error: 'missing required field: summary',
      output: { passed: true, findings: [] },
      retryable: true,
    }))

    await orchestrator.tick()
    await new Promise(r => setImmediate(r))

    const updatedRun = getLatestStageRun(task.id, 'selbstreview')
    expect(updatedRun?.status).toBe('awaiting_user')
    // Task must NOT move to 'failed' — this is a pause, not a hard fail.
    expect(getTaskById(task.id)?.currentStage).toBe('selbstreview')
  })

  it('loops selbstreview back to umsetzung with review_feedback on passed=false', async () => {
    const task = createTask({ slug: 'sr', title: 'SR', cwd: '/sr' })
    const { updateTask } = await import('../db/tasksRepo.js')
    updateTask(task.id, { currentStage: 'selbstreview' })
    const run = createStageRun({ taskId: task.id, stage: 'selbstreview' })
    updateStageRun(run.id, { status: 'running', pid: 9999 })

    parkAllAgentStages(orchestrator)
    orchestrator.setCompletionDetector(async () => ({
      kind: 'completed',
      output: {
        passed: false,
        findings: [{ severity: 'high', description: 'SQL injection in login', file: 'login.ts' }],
        summary: 'auth module needs fixes',
      },
    }))

    await orchestrator.tick()
    await new Promise(r => setImmediate(r))

    expect(getTaskById(task.id)?.currentStage).toBe('umsetzung')
    const reloaded = getTaskById(task.id)
    expect(reloaded?.metadata?.review_feedback).toContain('SQL injection')
  })

  it('transitions selbstreview → finalisierung on passed=true and clears stale feedback', async () => {
    const task = createTask({ slug: 'sp', title: 'SP', cwd: '/sp' })
    const { updateTask } = await import('../db/tasksRepo.js')
    updateTask(task.id, {
      currentStage: 'selbstreview',
      metadata: { review_feedback: 'old notes' },
    })
    const run = createStageRun({ taskId: task.id, stage: 'selbstreview' })
    updateStageRun(run.id, { status: 'running', pid: 9999 })

    parkAllAgentStages(orchestrator)
    orchestrator.setCompletionDetector(async () => ({
      kind: 'completed',
      output: { passed: true, findings: [], summary: 'ok' },
    }))

    await orchestrator.tick()
    await new Promise(r => setImmediate(r))

    expect(getTaskById(task.id)?.currentStage).toBe('finalisierung')
    const reloaded = getTaskById(task.id)
    expect(reloaded?.metadata?.review_feedback).toBeUndefined()
  })

  it('prefers silver-bullet tasks over newer high-priority tasks', async () => {
    setPipelineConfig('maxParallelOrchestrators', '1')
    const older = createTask({ slug: 'o', title: 'O', cwd: '/o', priority: 'low' })
    await new Promise(r => setImmediate(r))
    const silver = createTask({ slug: 's', title: 'S', cwd: '/s', silverBullet: true, priority: 'low' })
    await new Promise(r => setImmediate(r))
    const highPrio = createTask({ slug: 'h', title: 'H', cwd: '/h', priority: 'high' })

    parkAllAgentStages(orchestrator)
    await orchestrator.tick()
    await new Promise(r => setImmediate(r))

    // Silver bullet must win despite being neither oldest nor highest priority.
    // The silver-bullet task advances from backlog → umsetzung on tick.
    expect(getTaskById(silver.id)?.currentStage).toBe('umsetzung')
    expect(getTaskById(older.id)?.currentStage).toBe('backlog')
    expect(getTaskById(highPrio.id)?.currentStage).toBe('backlog')
  })

  it('prefers a task further along the pipeline over a fresh backlog task', async () => {
    setPipelineConfig('maxParallelOrchestrators', '1')
    const { updateTask } = await import('../db/tasksRepo.js')
    const ahead = createTask({ slug: 'a', title: 'A', cwd: '/a' })
    const fresh = createTask({ slug: 'f', title: 'F', cwd: '/f' })
    updateTask(ahead.id, { currentStage: 'selbstreview' })

    parkAllAgentStages(orchestrator)
    await orchestrator.tick()
    await new Promise(r => setImmediate(r))

    // Ahead task gets the single slot; fresh stays in backlog.
    const aheadAfter = getTaskById(ahead.id)
    expect(aheadAfter?.currentStage).toBe('selbstreview')
    // parkAllAgentStages makes selbstreview return wait_user, which leaves
    // currentStage unchanged and marks the run awaiting_user.
    const aheadRun = getLatestStageRun(ahead.id, 'selbstreview')
    expect(aheadRun?.status).toBe('awaiting_user')
    expect(getTaskById(fresh.id)?.currentStage).toBe('backlog')
  })

  it('hard-fails when the agent produced no parseable output', async () => {
    const task = createTask({ slug: 'hf', title: 'HF', cwd: '/hf' })
    const { updateTask } = await import('../db/tasksRepo.js')
    updateTask(task.id, { currentStage: 'umsetzung' })
    const run = createStageRun({ taskId: task.id, stage: 'umsetzung' })
    updateStageRun(run.id, { status: 'running', pid: 9999 })

    parkAllAgentStages(orchestrator)
    orchestrator.setCompletionDetector(async () => ({
      kind: 'failed',
      error: 'no parseable json output in session tail',
    }))

    await orchestrator.tick()
    await new Promise(r => setImmediate(r))

    // Task stays on `umsetzung`; the stage_run carries the failure.
    expect(getTaskById(task.id)?.currentStage).toBe('umsetzung')
    const updatedRun = getLatestStageRun(task.id, 'umsetzung')
    expect(updatedRun?.status).toBe('failed')
  })

  it('kills and fails a stage_run that exceeds the configured timeout', async () => {
    const task = createTask({ slug: 'to', title: 'TO', cwd: '/to' })
    updateTask(task.id, { currentStage: 'umsetzung' })
    const run = createStageRun({ taskId: task.id, stage: 'umsetzung' })
    const twoHoursAgo = new Date(Date.now() - 2 * 60 * 60 * 1000).toISOString()
    updateStageRun(run.id, { status: 'running', pid: 9999, startedAt: twoHoursAgo })

    setPipelineConfig('stageTimeoutSeconds', '1')
    parkAllAgentStages(orchestrator)
    orchestrator.setCompletionDetector(async () => ({ kind: 'still_running' }))

    await orchestrator.tick()
    await new Promise(r => setImmediate(r))

    const updatedRun = getLatestStageRun(task.id, 'umsetzung')
    expect(updatedRun?.status).toBe('failed')
    const output = updatedRun?.output as Record<string, unknown> | null
    expect(typeof output?.error).toBe('string')
    expect(output?.error as string).toContain('stage timeout')
  })

  it('does not kill a stage_run within the configured timeout', async () => {
    const task = createTask({ slug: 'nto', title: 'NTO', cwd: '/nto' })
    updateTask(task.id, { currentStage: 'umsetzung' })
    const run = createStageRun({ taskId: task.id, stage: 'umsetzung' })
    updateStageRun(run.id, { status: 'running', pid: 9999 })

    setPipelineConfig('stageTimeoutSeconds', '3600')
    parkAllAgentStages(orchestrator)
    orchestrator.setCompletionDetector(async () => ({ kind: 'still_running' }))

    await orchestrator.tick()
    await new Promise(r => setImmediate(r))

    const updatedRun = getLatestStageRun(task.id, 'umsetzung')
    expect(updatedRun?.status).toBe('running')
  })
})

describe('handleDependentTasks (dependency cascade)', () => {
  let tmpDir: string

  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), 'orch-dep-test-'))
    process.env.DASHBOARD_DB_PATH = join(tmpDir, 'test.db')
    getDb()
  })

  afterEach(() => {
    closeDb()
    rmSync(tmpDir, { recursive: true, force: true })
    delete process.env.DASHBOARD_DB_PATH
  })

  it('when prerequisite reaches done: dependent becomes pickable (no cascade action)', () => {
    const a = createTask({ slug: 'a', title: 'A', cwd: '/a' })
    const b = createTask({ slug: 'b', title: 'B', cwd: '/b' })
    addDependency(b.id, a.id, 'done', 'cancel')
    expect(isBlocked(b.id)).toBe(true)

    const notified: string[] = []
    const orch = new PipelineOrchestrator({
      onTaskChanged: (taskId) => { notified.push(taskId) },
    })
    updateTask(a.id, { currentStage: 'done' }) // simulate the actual stage change
    orch.notifyTaskTerminated(a.id, 'done')

    expect(isBlocked(b.id)).toBe(false)
    expect(getTaskById(b.id)?.currentStage).toBe('backlog')
  })

  it('when prerequisite cancelled + on_cancel_action=cancel: dependent moves to cancelled', () => {
    const a = createTask({ slug: 'ca', title: 'CA', cwd: '/ca' })
    const b = createTask({ slug: 'cb', title: 'CB', cwd: '/cb' })
    addDependency(b.id, a.id, 'done', 'cancel')
    updateTask(a.id, { currentStage: 'cancelled' })

    const notified: string[] = []
    const orch = new PipelineOrchestrator({
      onTaskChanged: (taskId) => { notified.push(taskId) },
    })
    orch.notifyTaskTerminated(a.id, 'cancelled')

    expect(getTaskById(b.id)?.currentStage).toBe('cancelled')
    expect(notified).toContain(b.id)
  })

  it('when prerequisite cancelled + on_cancel_action=on_hold: dependent moves to on_hold', () => {
    const a = createTask({ slug: 'ha', title: 'HA', cwd: '/ha' })
    const b = createTask({ slug: 'hb', title: 'HB', cwd: '/hb' })
    addDependency(b.id, a.id, 'done', 'on_hold')
    updateTask(a.id, { currentStage: 'cancelled' })

    const orch = new PipelineOrchestrator({})
    orch.notifyTaskTerminated(a.id, 'cancelled')

    expect(getTaskById(b.id)?.currentStage).toBe('on_hold')
  })

  it('when prerequisite cancelled + on_cancel_action=start: dependent stays pickable (no stage change)', () => {
    const a = createTask({ slug: 'sa', title: 'SA', cwd: '/sa' })
    const b = createTask({ slug: 'sb', title: 'SB', cwd: '/sb' })
    addDependency(b.id, a.id, 'done', 'start')
    updateTask(a.id, { currentStage: 'cancelled' })

    const orch = new PipelineOrchestrator({})
    orch.notifyTaskTerminated(a.id, 'cancelled')

    expect(getTaskById(b.id)?.currentStage).toBe('backlog')
  })

  it('when prerequisite cancelled but dependent still has other unmet deps: no cascade', () => {
    const a = createTask({ slug: 'ma', title: 'MA', cwd: '/ma' })
    const b = createTask({ slug: 'mb', title: 'MB', cwd: '/mb' })
    const c = createTask({ slug: 'mc', title: 'MC', cwd: '/mc' })
    addDependency(c.id, a.id, 'done', 'cancel')
    addDependency(c.id, b.id, 'done', 'cancel')
    updateTask(a.id, { currentStage: 'cancelled' })

    const orch = new PipelineOrchestrator({})
    orch.notifyTaskTerminated(a.id, 'cancelled')

    // c still blocked by b (not done/cancelled), so no cascade
    expect(getTaskById(c.id)?.currentStage).toBe('backlog')
  })

  it('cancel cascade recurses: A cancelled propagates to B then to C', () => {
    const a = createTask({ slug: 'rc-a', title: 'A', cwd: '/a' })
    const b = createTask({ slug: 'rc-b', title: 'B', cwd: '/b' })
    const c = createTask({ slug: 'rc-c', title: 'C', cwd: '/c' })
    addDependency(b.id, a.id, 'done', 'cancel')
    addDependency(c.id, b.id, 'done', 'cancel')
    updateTask(a.id, { currentStage: 'cancelled' })

    const orch = new PipelineOrchestrator({})
    orch.notifyTaskTerminated(a.id, 'cancelled')

    expect(getTaskById(b.id)?.currentStage).toBe('cancelled')
    expect(getTaskById(c.id)?.currentStage).toBe('cancelled')
  })

  it('diamond graph: C depends on A and B, B depends on A — C is only cancelled once', () => {
    const a = createTask({ slug: 'dia-a', title: 'A', cwd: '/a' })
    const b = createTask({ slug: 'dia-b', title: 'B', cwd: '/b' })
    const c = createTask({ slug: 'dia-c', title: 'C', cwd: '/c' })
    addDependency(b.id, a.id, 'done', 'cancel')
    addDependency(c.id, a.id, 'done', 'cancel')
    addDependency(c.id, b.id, 'done', 'cancel')
    updateTask(a.id, { currentStage: 'cancelled' })

    const notified: string[] = []
    const orch = new PipelineOrchestrator({
      onTaskChanged: taskId => notified.push(taskId),
    })
    orch.notifyTaskTerminated(a.id, 'cancelled')

    expect(getTaskById(b.id)?.currentStage).toBe('cancelled')
    expect(getTaskById(c.id)?.currentStage).toBe('cancelled')
    // C must appear exactly once — no duplicate cascade from the diamond
    expect(notified.filter(id => id === c.id)).toHaveLength(1)
  })
})

describe('pipelineOrchestrator concurrency', () => {
  it('serializes concurrent progressTask calls even when they all hit the same handler', async () => {
    const task = createTask({ slug: 'cc', title: 'CC', cwd: '/cc' })
    const { updateTask } = await import('../db/tasksRepo.js')
    updateTask(task.id, { currentStage: 'umsetzung' })

    let activeHandlers = 0
    let peak = 0
    let totalInvocations = 0
    // Handler stays on 'umsetzung' by returning wait_user so the task
    // doesn't advance between calls — forcing the lock to be the only
    // thing preventing parallel execution.
    orchestrator.setHandler('umsetzung', {
      stage: 'umsetzung',
      requiresAgent: true,
      async execute() {
        activeHandlers++
        peak = Math.max(peak, activeHandlers)
        totalInvocations++
        await new Promise(r => setTimeout(r, 15))
        activeHandlers--
        return { kind: 'wait_user', reason: 'test' }
      },
    })

    // Fire five concurrent calls — if the lock is broken, peak will jump.
    await Promise.all([
      orchestrator.progressTask(task.id),
      orchestrator.progressTask(task.id),
      orchestrator.progressTask(task.id),
      orchestrator.progressTask(task.id),
      orchestrator.progressTask(task.id),
    ])

    expect(peak).toBe(1) // handler never ran in parallel
    expect(totalInvocations).toBe(5) // every call actually ran
  })
})
