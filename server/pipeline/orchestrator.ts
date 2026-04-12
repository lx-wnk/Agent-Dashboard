import type { PipelineStage, PipelineTask, StageRun } from '../../src/types.js'
import type { StageContext, StageHandler, StageTransition } from './types.js'
import { consola } from 'consola'
import { appendAudit } from '../db/auditRepo.js'
import { getDb } from '../db/client.js'
import { getPipelineConfigNumber } from '../db/notificationConfigRepo.js'
import { createPermissionRequest, listTaskPermissions } from '../db/permissionsRepo.js'
import {
  createStageRun,
  getLatestStageRun,
  getStageRunById,
  listRunningStageRuns,
  updateStageRun,
} from '../db/stageRunsRepo.js'
import { getTaskById, updateTask } from '../db/tasksRepo.js'
import { buildSessionName, decideRecovery } from './sessionManager.js'
import { getHandlerForStage } from './stageHandlers.js'
import { STAGE_ORDER } from './types.js'

const POLL_INTERVAL_MS = 2000
const MAX_PARALLEL_KEY = 'maxParallelOrchestrators'
const DEFAULT_MAX_PARALLEL = 3

export class PipelineOrchestrator {
  private timer: ReturnType<typeof setInterval> | null = null
  private processing = false
  private handlerOverrides: Map<PipelineStage, StageHandler> = new Map()
  // Per-task locks prevent concurrent progressTask calls for the same task.
  private taskLocks: Map<string, Promise<unknown>> = new Map()

  constructor(private readonly pollIntervalMs = POLL_INTERVAL_MS) {}

  start(): void {
    if (this.timer)
      return
    this.recoverRunningStageRuns()
    this.timer = setInterval(
      () => this.tick().catch(err => consola.error('[orchestrator] tick error:', err)),
      this.pollIntervalMs,
    )
  }

  stop(): void {
    if (this.timer) {
      clearInterval(this.timer)
      this.timer = null
    }
  }

  /**
   * Override a stage handler (used in tests to inject deterministic stubs).
   */
  setHandler(stage: PipelineStage, handler: StageHandler): void {
    this.handlerOverrides.set(stage, handler)
  }

  clearHandlerOverrides(): void {
    this.handlerOverrides.clear()
  }

  private resolveHandler(stage: PipelineStage): StageHandler | null {
    return this.handlerOverrides.get(stage) ?? getHandlerForStage(stage)
  }

  /**
   * One tick of the poller. Processes tasks that are ready for progression:
   *  - in a terminal-auto stage (handler.requiresAgent=false),
   *  - or with a done stage run ready to transition.
   */
  async tick(): Promise<void> {
    if (this.processing)
      return
    this.processing = true
    try {
      await this.progressPendingTasks()
    }
    finally {
      this.processing = false
    }
  }

  /**
   * Public API: advance a task from its current stage. Serialized per task
   * via an in-memory lock to avoid concurrent writes.
   */
  async progressTask(taskId: string): Promise<StageRun | null> {
    const existing = this.taskLocks.get(taskId)
    if (existing)
      await existing.catch(() => { /* ignore previous failure */ })

    const promise = this.runProgressTaskLocked(taskId)
    this.taskLocks.set(taskId, promise)
    try {
      return await promise
    }
    finally {
      if (this.taskLocks.get(taskId) === promise)
        this.taskLocks.delete(taskId)
    }
  }

  private async runProgressTaskLocked(taskId: string): Promise<StageRun | null> {
    const task = getTaskById(taskId)
    if (!task)
      return null
    if (isTerminal(task.currentStage))
      return null

    const handler = this.resolveHandler(task.currentStage)
    if (!handler)
      return null

    // Parallelism cap: only for the expensive umsetzung stage.
    if (task.currentStage === 'umsetzung' && !this.hasUmsetzungSlot())
      return null

    const stageRun = this.ensureStageRun(task)
    updateStageRun(stageRun.id, { status: 'running', startedAt: new Date().toISOString() })

    const ctx = this.buildContext(task, stageRun)
    let transition: StageTransition
    try {
      transition = await handler.execute(ctx)
    }
    catch (err) {
      transition = { kind: 'fail', error: (err as Error).message }
    }

    return this.applyTransition(task, stageRun, transition)
  }

  /**
   * Called when a user resolves an approval or permission request and the
   * orchestrator should try to push the task forward.
   */
  async resumeFromUser(taskId: string): Promise<StageRun | null> {
    const task = getTaskById(taskId)
    if (!task)
      return null
    return this.progressTask(taskId)
  }

  // -------- private helpers --------

  private buildContext(task: PipelineTask, stageRun: StageRun): StageContext {
    const permissions = listTaskPermissions(task.id)
    const previousOutput = this.getPreviousStageOutput(task)
    return {
      task,
      stageRun,
      permissions,
      previousOutput,
      recordAudit: (action, details) => {
        appendAudit({ taskId: task.id, actor: 'orchestrator', action, details: details ?? null })
      },
      requestPermission: (tool, pattern, reason) => {
        return createPermissionRequest({ stageRunId: stageRun.id, tool, pattern, reason })
      },
    }
  }

  private getPreviousStageOutput(task: PipelineTask): Record<string, unknown> | null {
    // Walk back one stage in the canonical order to find the last done run.
    const idx = STAGE_ORDER.indexOf(task.currentStage)
    if (idx <= 0)
      return null
    const prev = getLatestStageRun(task.id, STAGE_ORDER[idx - 1])
    return prev?.output ?? null
  }

  private ensureStageRun(task: PipelineTask): StageRun {
    const existing = getLatestStageRun(task.id, task.currentStage)
    if (existing && (existing.status === 'pending' || existing.status === 'running'))
      return existing

    const iteration = existing ? existing.iteration + 1 : 0
    return createStageRun({
      taskId: task.id,
      stage: task.currentStage,
      iteration,
      sessionName: buildSessionName(task, task.currentStage, iteration),
    })
  }

  /**
   * Apply a stage transition atomically. Wraps all DB writes in a single
   * SQLite transaction so a crash can't leave the task + stage_run in a
   * split-brain state.
   */
  private applyTransition(
    task: PipelineTask,
    stageRun: StageRun,
    transition: StageTransition,
  ): StageRun {
    const db = getDb()
    const now = new Date().toISOString()

    const txn = db.transaction(() => {
      switch (transition.kind) {
        case 'next': {
          updateStageRun(stageRun.id, {
            status: 'done',
            endedAt: now,
            output: transition.output ?? null,
          }, db)
          updateTask(task.id, { currentStage: transition.toStage }, db)
          appendAudit({
            taskId: task.id,
            actor: 'orchestrator',
            action: 'stage_transition',
            details: { from: task.currentStage, to: transition.toStage },
          }, db)
          return { updatedRunId: stageRun.id, newRunId: null }
        }

        case 'wait_user': {
          updateStageRun(stageRun.id, {
            status: 'awaiting_user',
            output: transition.output ?? null,
          }, db)
          appendAudit({
            taskId: task.id,
            actor: 'orchestrator',
            action: 'awaiting_user',
            details: { reason: transition.reason },
          }, db)
          return { updatedRunId: stageRun.id, newRunId: null }
        }

        case 'iterate': {
          updateStageRun(stageRun.id, {
            status: 'done',
            endedAt: now,
            output: transition.output ?? null,
          }, db)
          const maxIter = task.maxIterations
          if (stageRun.iteration + 1 >= maxIter) {
            updateTask(task.id, { currentStage: 'failed' }, db)
            appendAudit({
              taskId: task.id,
              actor: 'orchestrator',
              action: 'iteration_limit_reached',
              details: { maxIter, lastIteration: stageRun.iteration },
            }, db)
            return { updatedRunId: stageRun.id, newRunId: null }
          }
          const newRun = createStageRun({
            taskId: task.id,
            stage: stageRun.stage,
            iteration: stageRun.iteration + 1,
            sessionName: buildSessionName(task, stageRun.stage, stageRun.iteration + 1),
          }, db)
          return { updatedRunId: stageRun.id, newRunId: newRun.id }
        }

        case 'on_hold': {
          updateStageRun(stageRun.id, {
            status: 'on_hold',
            output: transition.output ?? null,
          }, db)
          updateTask(task.id, { currentStage: 'on_hold' }, db)
          appendAudit({
            taskId: task.id,
            actor: 'orchestrator',
            action: 'moved_on_hold',
            details: { permissionRequestId: transition.permissionRequestId },
          }, db)
          return { updatedRunId: stageRun.id, newRunId: null }
        }

        case 'done': {
          updateStageRun(stageRun.id, {
            status: 'done',
            endedAt: now,
            output: transition.output ?? null,
          }, db)
          updateTask(task.id, { currentStage: 'done' }, db)
          appendAudit({ taskId: task.id, actor: 'orchestrator', action: 'task_done' }, db)
          return { updatedRunId: stageRun.id, newRunId: null }
        }

        case 'fail': {
          updateStageRun(stageRun.id, {
            status: 'failed',
            endedAt: now,
            output: { ...(transition.output ?? {}), error: transition.error },
          }, db)
          updateTask(task.id, { currentStage: 'failed' }, db)
          appendAudit({
            taskId: task.id,
            actor: 'orchestrator',
            action: 'task_failed',
            details: { error: transition.error },
          }, db)
          return { updatedRunId: stageRun.id, newRunId: null }
        }

        case 'async_running': {
          // The handler spawned an agent that will report completion
          // asynchronously via the channel or JSONL session. Keep the
          // stage_run in 'running' status with the PID so the poller
          // (or explicit completion callback) can finalize it later.
          updateStageRun(stageRun.id, {
            status: 'running',
            pid: transition.pid,
            output: transition.output ?? null,
          }, db)
          appendAudit({
            taskId: task.id,
            actor: 'orchestrator',
            action: 'agent_spawned',
            details: { pid: transition.pid, stage: stageRun.stage },
          }, db)
          return { updatedRunId: stageRun.id, newRunId: null }
        }
      }
    })

    const { updatedRunId, newRunId } = txn() as { updatedRunId: string, newRunId: string | null }
    // Return the run that the caller expects to inspect (the updated one for
    // most transitions, or the new iteration for `iterate`).
    return getStageRunById(newRunId ?? updatedRunId)!
  }

  private async progressPendingTasks(): Promise<void> {
    // Currently: no-op placeholder. Phase 3+ will list tasks whose current
    // stage handler is agent-less and push them forward. For now, progression
    // is driven from the API (progressTask / resumeFromUser).
  }

  /**
   * After a restart, any stage_run with status='running' whose PID is dead
   * must be flipped to 'pending' (for resume) or 'failed' (no session/PID)
   * to avoid permanently blocking the umsetzung slot cap.
   */
  private recoverRunningStageRuns(): void {
    const running = listRunningStageRuns()
    for (const run of running) {
      const decision = decideRecovery(run)
      appendAudit({
        taskId: run.taskId,
        actor: 'system',
        action: 'recovery_decision',
        details: {
          stage: run.stage,
          iteration: run.iteration,
          decision: decision.kind,
          reason: decision.reason,
        },
      })
      if (decision.kind === 'alive')
        continue
      if (decision.kind === 'resume') {
        updateStageRun(run.id, { status: 'pending', pid: null })
      }
      else {
        // restart: no PID, no session — mark the run as failed so the task
        // can be retried fresh without a zombie stage_run blocking slots.
        updateStageRun(run.id, {
          status: 'failed',
          endedAt: new Date().toISOString(),
          output: { error: 'orchestrator crashed before completion; no session to resume' },
        })
      }
    }
  }

  private hasUmsetzungSlot(): boolean {
    const max = getPipelineConfigNumber(MAX_PARALLEL_KEY, DEFAULT_MAX_PARALLEL)
    const running = listRunningStageRuns().filter(r => r.stage === 'umsetzung' && r.status === 'running')
    return running.length < max
  }
}

function isTerminal(stage: PipelineStage): boolean {
  return stage === 'done' || stage === 'failed' || stage === 'cancelled'
}
