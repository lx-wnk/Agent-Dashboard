import type { PipelineStage, PipelineTask, StageRun } from '../../src/types.js'
import type { StageContext, StageHandler, StageTransition } from './types.js'
import { appendAudit } from '../db/auditRepo.js'
import { getPipelineConfigNumber } from '../db/notificationConfigRepo.js'
import { createPermissionRequest, listTaskPermissions } from '../db/permissionsRepo.js'
import {
  createStageRun,
  getLatestStageRun,
  listRunningStageRuns,
  updateStageRun,
} from '../db/stageRunsRepo.js'
import { getTaskById, updateTask } from '../db/tasksRepo.js'
import { buildSessionName, decideRecovery } from './sessionManager.js'
import { getHandlerForStage } from './stageHandlers.js'

const POLL_INTERVAL_MS = 2000
const MAX_PARALLEL_KEY = 'maxParallelOrchestrators'
const DEFAULT_MAX_PARALLEL = 3

export class PipelineOrchestrator {
  private timer: ReturnType<typeof setInterval> | null = null
  private processing = false
  private handlerOverrides: Map<PipelineStage, StageHandler> = new Map()

  constructor(private readonly pollIntervalMs = POLL_INTERVAL_MS) {}

  start(): void {
    if (this.timer)
      return
    this.recoverRunningStageRuns()
    this.timer = setInterval(() => this.tick().catch(() => { /* swallow */ }), this.pollIntervalMs)
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
   * Public API: advance a task from its current stage. Creates a new
   * stage_run and invokes the handler. Honors the parallel-orchestrator cap
   * when entering the `umsetzung` stage.
   */
  async progressTask(taskId: string): Promise<StageRun | null> {
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
    const order = [
      'backlog',
      'pruefung',
      'refinement',
      'planning',
      'approval1',
      'umsetzungskonzept',
      'approval2',
      'umsetzung',
      'selbstreview',
      'finalisierung',
    ] as const
    const idx = order.indexOf(task.currentStage as typeof order[number])
    if (idx <= 0)
      return null
    const prev = getLatestStageRun(task.id, order[idx - 1])
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

  private applyTransition(
    task: PipelineTask,
    stageRun: StageRun,
    transition: StageTransition,
  ): StageRun {
    const now = new Date().toISOString()
    switch (transition.kind) {
      case 'next': {
        updateStageRun(stageRun.id, { status: 'done', endedAt: now, output: transition.output ?? null })
        updateTask(task.id, { currentStage: transition.toStage })
        appendAudit({
          taskId: task.id,
          actor: 'orchestrator',
          action: 'stage_transition',
          details: { from: task.currentStage, to: transition.toStage },
        })
        return getLatestStageRun(task.id, stageRun.stage)!
      }

      case 'wait_user': {
        updateStageRun(stageRun.id, {
          status: 'awaiting_user',
          output: transition.output ?? null,
        })
        appendAudit({
          taskId: task.id,
          actor: 'orchestrator',
          action: 'awaiting_user',
          details: { reason: transition.reason },
        })
        return getLatestStageRun(task.id, stageRun.stage)!
      }

      case 'iterate': {
        updateStageRun(stageRun.id, { status: 'done', endedAt: now, output: transition.output ?? null })
        const maxIter = task.maxIterations
        if (stageRun.iteration + 1 >= maxIter) {
          updateTask(task.id, { currentStage: 'failed' })
          appendAudit({
            taskId: task.id,
            actor: 'orchestrator',
            action: 'iteration_limit_reached',
            details: { maxIter, lastIteration: stageRun.iteration },
          })
          return getLatestStageRun(task.id, stageRun.stage)!
        }
        // Schedule new iteration in the same stage
        createStageRun({
          taskId: task.id,
          stage: stageRun.stage,
          iteration: stageRun.iteration + 1,
          sessionName: buildSessionName(task, stageRun.stage, stageRun.iteration + 1),
        })
        return getLatestStageRun(task.id, stageRun.stage)!
      }

      case 'on_hold': {
        updateStageRun(stageRun.id, { status: 'on_hold', output: transition.output ?? null })
        updateTask(task.id, { currentStage: 'on_hold' })
        appendAudit({
          taskId: task.id,
          actor: 'orchestrator',
          action: 'moved_on_hold',
          details: { permissionRequestId: transition.permissionRequestId },
        })
        return getLatestStageRun(task.id, stageRun.stage)!
      }

      case 'done': {
        updateStageRun(stageRun.id, { status: 'done', endedAt: now, output: transition.output ?? null })
        updateTask(task.id, { currentStage: 'done' })
        appendAudit({ taskId: task.id, actor: 'orchestrator', action: 'task_done' })
        return getLatestStageRun(task.id, stageRun.stage)!
      }

      case 'fail': {
        updateStageRun(stageRun.id, {
          status: 'failed',
          endedAt: now,
          output: { ...(transition.output ?? {}), error: transition.error },
        })
        updateTask(task.id, { currentStage: 'failed' })
        appendAudit({
          taskId: task.id,
          actor: 'orchestrator',
          action: 'task_failed',
          details: { error: transition.error },
        })
        return getLatestStageRun(task.id, stageRun.stage)!
      }
    }
  }

  private async progressPendingTasks(): Promise<void> {
    // Currently: no-op placeholder. Phase 3+ will list tasks whose current
    // stage handler is agent-less and push them forward. For now, progression
    // is driven from the API (progressTask / resumeFromUser).
  }

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
