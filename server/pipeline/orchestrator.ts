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
  getStageRunByIteration,
  listRunningStageRuns,
  updateStageRun,
} from '../db/stageRunsRepo.js'
import { getTaskById, listPickableTasks, updateTask } from '../db/tasksRepo.js'
import { detectCompletion } from './completionDetector.js'
import { buildSessionName, decideRecovery } from './sessionManager.js'
import { getHandlerForStage } from './stageHandlers.js'
import { STAGE_ORDER } from './types.js'

const POLL_INTERVAL_MS = 2000
const MAX_PARALLEL_KEY = 'maxParallelOrchestrators'
const DEFAULT_MAX_PARALLEL = 3

/**
 * Callback invoked when a stage handler creates a runtime permission request.
 * Injected by the server so the orchestrator stays decoupled from SSE / the
 * notification dispatcher.
 */
export type PermissionRequestNotifier = (
  taskId: string,
  request: { id: string, tool: string, pattern: string | null, reason: string | null },
) => void

export interface OrchestratorOptions {
  pollIntervalMs?: number
  onPermissionRequest?: PermissionRequestNotifier
}

export class PipelineOrchestrator {
  private timer: ReturnType<typeof setInterval> | null = null
  private processing = false
  private handlerOverrides: Map<PipelineStage, StageHandler> = new Map()
  /**
   * Swappable completion detector — production uses the real one; tests
   *  inject a stub so they can exercise the driver-loop branches without
   *  spawning real processes or touching the filesystem.
   */
  private detectCompletionImpl: typeof detectCompletion = detectCompletion
  // Per-task locks prevent concurrent progressTask calls for the same task.
  private taskLocks: Map<string, Promise<unknown>> = new Map()
  private readonly pollIntervalMs: number
  private readonly onPermissionRequest: PermissionRequestNotifier | null

  constructor(options: OrchestratorOptions | number = {}) {
    // Backwards-compatible: constructor used to accept a plain pollIntervalMs.
    if (typeof options === 'number') {
      this.pollIntervalMs = options
      this.onPermissionRequest = null
    }
    else {
      this.pollIntervalMs = options.pollIntervalMs ?? POLL_INTERVAL_MS
      this.onPermissionRequest = options.onPermissionRequest ?? null
    }
  }

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

  /** Test-only seam: override the completion detector used by the driver loop. */
  setCompletionDetector(fn: typeof detectCompletion): void {
    this.detectCompletionImpl = fn
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
   * via a promise-chain lock: each call atomically reads the current lock
   * and chains the next runProgressTaskLocked onto it, so concurrent
   * callers line up deterministically instead of racing for the lock slot.
   *
   * CORRECTNESS WARNING: the get/chain/set sequence must remain synchronous
   * (no awaits between Map.get and Map.set). If you insert an await here,
   * two concurrent callers can read the same `prev`, each build a new
   * chain head, and parallel execution returns. Keep this block atomic.
   */
  async progressTask(taskId: string): Promise<StageRun | null> {
    const prev = this.taskLocks.get(taskId) ?? Promise.resolve(null)
    const next = prev
      .catch(() => null)
      .then(() => this.runProgressTaskLocked(taskId))
    this.taskLocks.set(taskId, next)
    try {
      return await next
    }
    finally {
      // Only clear the slot if no newer call has already chained on top.
      if (this.taskLocks.get(taskId) === next)
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

    // Global runner-slot cap — applies to every agent-driven stage, not
    // just umsetzung. Protects against spawning more concurrent Claude
    // agents than maxParallelOrchestrators allows.
    if (handler.requiresAgent && !this.hasFreeRunnerSlot(task.id))
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
    const priorIterationOutput = stageRun.iteration > 0
      ? getStageRunByIteration(task.id, stageRun.stage, stageRun.iteration - 1)?.output ?? null
      : null
    return {
      task,
      stageRun,
      permissions,
      previousOutput,
      priorIterationOutput,
      recordAudit: (action, details) => {
        appendAudit({ taskId: task.id, actor: 'orchestrator', action, details: details ?? null })
      },
      requestPermission: (tool, pattern, reason) => {
        const req = createPermissionRequest({ stageRunId: stageRun.id, tool, pattern, reason })
        // Broadcast so the UI / dispatcher sees the pause immediately,
        // matching the behavior of the REST /permission-requests endpoint.
        this.onPermissionRequest?.(task.id, {
          id: req.id,
          tool: req.tool,
          pattern: req.pattern,
          reason: req.reason,
        })
        return req
      },
    }
  }

  private getPreviousStageOutput(task: PipelineTask): Record<string, unknown> | null {
    // Walk back through the canonical order and return the first prior
    // stage that produced a non-null output. Approval1/approval2 stages
    // are agent-less wait_user gates that store null output — skipping
    // them lets agent handlers see the real previous meaningful result.
    const idx = STAGE_ORDER.indexOf(task.currentStage)
    if (idx <= 0)
      return null
    for (let i = idx - 1; i >= 0; i--) {
      const prev = getLatestStageRun(task.id, STAGE_ORDER[i])
      if (prev?.output)
        return prev.output
    }
    return null
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
          // Optional atomic task.metadata patch — e.g. selbstreview loop
          // stashing review_feedback when jumping back to umsetzung. Must
          // land in the same transaction as the stage transition or a
          // crash between the two leaves task state inconsistent.
          const patch: { currentStage: PipelineStage, metadata?: Record<string, unknown> | null } = {
            currentStage: transition.toStage,
          }
          if (transition.taskMetadataPatch !== undefined)
            patch.metadata = transition.taskMetadataPatch
          updateTask(task.id, patch, db)
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

  /**
   * Driver loop body, called once per tick. Two responsibilities:
   *
   * 1. **Finalize async agents**: for every stage_run with status='running'
   *    and a PID, ask the completionDetector whether the agent is done.
   *    On success the per-stage routing decides the next transition:
   *    most stages go to their canonical next stage; selbstreview inspects
   *    `passed` and loops back to umsetzung with review feedback stored
   *    on task.metadata; finalisierung transitions to `done`.
   *    On schema-validation failure → retry once via `iterate` carrying
   *    the error as feedback, then escalate to `wait_user` on the second
   *    failure. Hard failure (no session, no output) → fail fast.
   *
   * 2. **Pick next tasks for free runner slots**: enforces the task
   *    priority order (silver-bullet → furthest stage → priority →
   *    createdAt) against the global maxParallelOrchestrators cap.
   *    See project_task_pipeline_runner_model memory for invariants.
   *
   * Completion finalization runs BEFORE pickup so a freshly finished
   * async run frees its slot before we count free slots for the picker.
   */
  private async progressPendingTasks(): Promise<void> {
    await this.finalizeCompletedAsyncRuns()
    this.pickNextTasksForFreeSlots()
  }

  private async finalizeCompletedAsyncRuns(): Promise<void> {
    const running = listRunningStageRuns().filter(r => r.status === 'running' && r.pid !== null)
    for (const run of running) {
      const task = getTaskById(run.taskId)
      if (!task)
        continue
      const cwd = task.worktreePath || task.cwd
      let result
      try {
        result = await this.detectCompletionImpl(run, cwd)
      }
      catch (err) {
        consola.error('[orchestrator] completion detection failed:', err)
        continue
      }
      if (result.kind === 'still_running')
        continue

      // Re-fetch the stage_run to guard against races where applyTransition
      // on a prior iteration already moved this row to done/failed.
      const fresh = getStageRunById(run.id)
      if (!fresh || fresh.status !== 'running')
        continue

      if (result.kind === 'completed') {
        const transition = this.decideCompletedTransition(task, fresh, result.output ?? {})
        this.applyTransition(task, fresh, transition)
        continue
      }

      // result.kind === 'failed': distinguish schema rejection (retryable)
      // from hard failure (no output). Only the retryable branch carries
      // `result.output` — the parsed-but-invalid payload.
      const isSchemaRejection = result.output !== undefined
      if (!isSchemaRejection) {
        this.applyTransition(task, fresh, { kind: 'fail', error: result.error ?? 'unknown failure' })
        continue
      }

      // Strict + single-retry + escalate strategy:
      // iteration 0 → retry with feedback; iteration ≥ 1 → wait_user.
      if (fresh.iteration === 0) {
        this.applyTransition(task, fresh, {
          kind: 'iterate',
          output: {
            validation_error: result.error,
            rejected_output: result.output,
          },
        })
      }
      else {
        this.applyTransition(task, fresh, {
          kind: 'wait_user',
          reason: `schema validation failed twice at stage ${fresh.stage}: ${result.error}`,
          output: {
            validation_error: result.error,
            rejected_output: result.output,
          },
        })
      }
    }
  }

  /**
   * Decide which transition to apply after an async stage completes.
   * Most stages route to the canonical next stage, but two special cases:
   *
   *   - **selbstreview**: if `passed: false`, loop back to umsetzung with
   *     the review findings stored on task.metadata.review_feedback so
   *     the next umsetzung iteration sees them. If `passed: true`,
   *     advance to finalisierung and clear any stale review_feedback.
   *   - **finalisierung**: the terminal agent stage — always `{done}`.
   *
   * Metadata mutations are returned as part of the `next` transition
   * (`taskMetadataPatch`) so they land in the same SQLite transaction
   * as the stage transition. Do NOT perform bare `updateTask` calls
   * here — a crash between two separate writes leaves the task in an
   * inconsistent state.
   */
  private decideCompletedTransition(
    task: PipelineTask,
    run: StageRun,
    output: Record<string, unknown>,
  ): StageTransition {
    if (run.stage === 'finalisierung')
      return { kind: 'done', output }

    if (run.stage === 'selbstreview') {
      const passed = output.passed === true
      if (!passed) {
        const feedback = summarizeReviewFindings(output)
        const nextMeta = { ...(task.metadata ?? {}), review_feedback: feedback }
        return { kind: 'next', toStage: 'umsetzung', output, taskMetadataPatch: nextMeta }
      }
      // Passed — clear any lingering feedback so the finalisierung
      // handler doesn't read stale review notes from metadata.
      if (task.metadata && typeof task.metadata === 'object' && 'review_feedback' in task.metadata) {
        const { review_feedback: _drop, ...rest } = task.metadata
        const cleared = Object.keys(rest).length > 0 ? rest : null
        return { kind: 'next', toStage: 'finalisierung', output, taskMetadataPatch: cleared }
      }
      return { kind: 'next', toStage: 'finalisierung', output }
    }

    return { kind: 'next', toStage: nextStageOrDone(run.stage), output }
  }

  /**
   * Pick tasks from the pickable pool (non-terminal, non-paused, no
   * running run) and promote them to fill free runner slots. Uses the
   * project_task_pipeline_runner_model priority order.
   */
  private pickNextTasksForFreeSlots(): void {
    const max = getPipelineConfigNumber(MAX_PARALLEL_KEY, DEFAULT_MAX_PARALLEL)
    // Single DB scan, reused for both the busy-count and the per-task
    // "has running run" check below — avoids N×M full-table scans.
    const running = listRunningStageRuns().filter(r => r.status === 'running')
    const busyTaskIds = new Set(running.map(r => r.taskId))
    const freeSlots = max - busyTaskIds.size
    if (freeSlots <= 0)
      return

    // Exclude: tasks already driving an agent (in busyTaskIds) and tasks
    // whose latest stage_run is awaiting_user. The latter is needed
    // because wait_user does NOT change currentStage, so a task in
    // e.g. 'pruefung' with an awaiting_user latest run must be skipped.
    const readyToProgress = listPickableTasks().filter((t) => {
      if (busyTaskIds.has(t.id))
        return false
      const latest = getLatestStageRun(t.id, t.currentStage)
      return latest?.status !== 'awaiting_user'
    })

    readyToProgress.sort(comparePickOrder)
    const picks = readyToProgress.slice(0, freeSlots)

    for (const task of picks) {
      // Fire-and-forget — progressTask serializes per-task internally.
      void this.progressTask(task.id).catch(err =>
        consola.error(`[orchestrator] pickup failed for ${task.id}:`, err),
      )
    }
  }

  /**
   * Returns true if there is at least one free runner slot available
   * for a NEW agent spawn. Excludes the caller's own task from the busy
   * count when `exceptTaskId` is provided, so an already-in-flight task
   * that's about to spawn its next stage doesn't consume a slot twice.
   */
  private hasFreeRunnerSlot(exceptTaskId?: string): boolean {
    const max = getPipelineConfigNumber(MAX_PARALLEL_KEY, DEFAULT_MAX_PARALLEL)
    const busy = countBusyRunners(exceptTaskId)
    return busy < max
  }

  /**
   * After a restart, any stage_run with status='running' whose PID is dead
   * must be flipped to 'pending' (for resume) or 'failed' (no session/PID)
   * to avoid permanently blocking the global runner-slot cap.
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
}

function isTerminal(stage: PipelineStage): boolean {
  return stage === 'done' || stage === 'failed' || stage === 'cancelled'
}

function nextStageOrDone(stage: PipelineStage): PipelineStage {
  const idx = STAGE_ORDER.indexOf(stage)
  return idx >= 0 && idx < STAGE_ORDER.length - 1 ? STAGE_ORDER[idx + 1] : 'done'
}

/**
 * Count tasks currently occupying a runner slot. A task is "busy" iff it
 * has a stage_run with status='running' — awaiting_user, on_hold, and
 * pending do not count (they're idle/paused).
 */
function countBusyRunners(exceptTaskId?: string): number {
  const running = listRunningStageRuns().filter(r => r.status === 'running')
  const byTask = new Set(running.map(r => r.taskId))
  if (exceptTaskId)
    byTask.delete(exceptTaskId)
  return byTask.size
}

const PRIORITY_RANK: Record<string, number> = { high: 3, medium: 2, low: 1 }

/**
 * Sort comparator implementing the pickup order:
 * silver_bullet desc → stage-index desc → priority desc → createdAt asc.
 */
function comparePickOrder(a: PipelineTask, b: PipelineTask): number {
  if (a.silverBullet !== b.silverBullet)
    return a.silverBullet ? -1 : 1
  const stageA = STAGE_ORDER.indexOf(a.currentStage)
  const stageB = STAGE_ORDER.indexOf(b.currentStage)
  if (stageA !== stageB)
    return stageB - stageA
  const prA = PRIORITY_RANK[a.priority] ?? 2
  const prB = PRIORITY_RANK[b.priority] ?? 2
  if (prA !== prB)
    return prB - prA
  return a.createdAt.localeCompare(b.createdAt)
}

/**
 * Extract a short, actionable review-feedback string from a selbstreview
 * output payload for injection into the next umsetzung iteration prompt.
 */
function summarizeReviewFindings(output: Record<string, unknown>): string {
  const findings = Array.isArray(output.findings) ? output.findings : []
  const summary = typeof output.summary === 'string' ? output.summary : ''
  const lines = findings
    .map((f) => {
      if (typeof f !== 'object' || f === null)
        return ''
      const entry = f as Record<string, unknown>
      const severity = typeof entry.severity === 'string' ? entry.severity.toUpperCase() : 'ISSUE'
      const description = typeof entry.description === 'string' ? entry.description : ''
      const file = typeof entry.file === 'string' ? ` (${entry.file})` : ''
      return `- [${severity}] ${description}${file}`
    })
    .filter(Boolean)
  return [summary, ...lines].filter(Boolean).join('\n')
}
