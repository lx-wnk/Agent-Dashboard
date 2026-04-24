import type { PipelineStage, PipelineTask, StageRun } from '../../src/types.js'
import type { StageContext, StageHandler, StageTransition } from './types.js'
import { consola } from 'consola'
import { revokeApiKeyByName } from '../db/apiKeysRepo.js'
import { appendAudit } from '../db/auditRepo.js'
import { getDb } from '../db/client.js'
import { resolveFeedbackForStage } from '../db/feedbackRepo.js'
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
import { attachSessionId, buildSessionName, decideRecovery } from './sessionManager.js'
import { findNewestSessionId } from './sessionOutputReader.js'
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

/**
 * Callback invoked when a stage run flips to `failed`. Injected by the server
 * so the orchestrator stays decoupled from the SSE broadcast and the
 * notification dispatcher (same decoupling pattern as onPermissionRequest).
 *
 * The stage_run row is passed so callers can include `stage`, `iteration`
 * and `sessionId` in their notification — useful for the "analyze" side
 * session which needs a pointer to the failed JSONL.
 */
export type StageFailedNotifier = (
  taskId: string,
  info: { stageRunId: string, stage: PipelineStage, iteration: number, error: string },
) => void

/**
 * Called after every successful applyTransition. The server wires this
 * to broadcastTaskEvent so the kanban reflects stage advances, iterate
 * spawns, wait_user, on_hold, done — i.e. every healthy state change,
 * not just the failure path covered by onStageFailed.
 */
export type TaskChangedNotifier = (
  taskId: string,
  info: { transitionKind: string },
) => void

export interface OrchestratorOptions {
  pollIntervalMs?: number
  onPermissionRequest?: PermissionRequestNotifier
  onStageFailed?: StageFailedNotifier
  onTaskChanged?: TaskChangedNotifier
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
  private readonly onStageFailed: StageFailedNotifier | null
  private readonly onTaskChanged: TaskChangedNotifier | null

  constructor(options: OrchestratorOptions | number = {}) {
    // Backwards-compatible: constructor used to accept a plain pollIntervalMs.
    if (typeof options === 'number') {
      this.pollIntervalMs = options
      this.onPermissionRequest = null
      this.onStageFailed = null
      this.onTaskChanged = null
    }
    else {
      this.pollIntervalMs = options.pollIntervalMs ?? POLL_INTERVAL_MS
      this.onPermissionRequest = options.onPermissionRequest ?? null
      this.onStageFailed = options.onStageFailed ?? null
      this.onTaskChanged = options.onTaskChanged ?? null
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
      let result: { updatedRunId: string, newRunId: string | null }

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
          // Resolve any user feedback that was pending on this stage —
          // a successful next-transition means the agent has produced a
          // fresh artifact that supersedes the prior reviewed output.
          if (stageRun.stage === 'planning' || stageRun.stage === 'umsetzungskonzept')
            resolveFeedbackForStage(task.id, stageRun.stage, stageRun.id, db)
          appendAudit({
            taskId: task.id,
            actor: 'orchestrator',
            action: 'stage_transition',
            details: { from: task.currentStage, to: transition.toStage },
          }, db)
          result = { updatedRunId: stageRun.id, newRunId: null }
          break
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
          result = { updatedRunId: stageRun.id, newRunId: null }
          break
        }

        case 'iterate': {
          updateStageRun(stageRun.id, {
            status: 'done',
            endedAt: now,
            output: transition.output ?? null,
          }, db)
          const maxIter = task.maxIterations
          if (stageRun.iteration + 1 >= maxIter) {
            // Flip the *stage_run* to failed but leave task.currentStage
            // where it is. The frontend's needsUser predicate surfaces the
            // failed run in the "Needs You" column so the user can retry
            // or launch an analysis session.
            updateStageRun(stageRun.id, {
              status: 'failed',
              output: {
                ...(transition.output ?? {}),
                error: `iteration limit reached (${maxIter})`,
              },
            }, db)
            appendAudit({
              taskId: task.id,
              actor: 'orchestrator',
              action: 'iteration_limit_reached',
              details: { maxIter, lastIteration: stageRun.iteration },
            }, db)
            result = { updatedRunId: stageRun.id, newRunId: null }
          }
          else {
            const newRun = createStageRun({
              taskId: task.id,
              stage: stageRun.stage,
              iteration: stageRun.iteration + 1,
              sessionName: buildSessionName(task, stageRun.stage, stageRun.iteration + 1),
            }, db)
            result = { updatedRunId: stageRun.id, newRunId: newRun.id }
          }
          break
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
          result = { updatedRunId: stageRun.id, newRunId: null }
          break
        }

        case 'done': {
          updateStageRun(stageRun.id, {
            status: 'done',
            endedAt: now,
            output: transition.output ?? null,
          }, db)
          updateTask(task.id, { currentStage: 'done' }, db)
          appendAudit({ taskId: task.id, actor: 'orchestrator', action: 'task_done' }, db)
          result = { updatedRunId: stageRun.id, newRunId: null }
          break
        }

        case 'fail': {
          updateStageRun(stageRun.id, {
            status: 'failed',
            endedAt: now,
            output: { ...(transition.output ?? {}), error: transition.error },
          }, db)
          // Do NOT touch task.currentStage — the task stays on the stage
          // where the run failed. The frontend derives "needs user"
          // from the latest stage_run status, not from the task stage,
          // so this single status flip surfaces the task in the Needs
          // You column and unlocks the retry/analyze buttons.
          appendAudit({
            taskId: task.id,
            actor: 'orchestrator',
            action: 'stage_failed',
            details: { stage: stageRun.stage, iteration: stageRun.iteration, error: transition.error },
          }, db)
          result = { updatedRunId: stageRun.id, newRunId: null }
          break
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
          result = { updatedRunId: stageRun.id, newRunId: null }
          break
        }
      }

      // Revoke the ephemeral MCP token atomically with the state transition.
      // async_running keeps the token alive so the still-running agent can
      // authenticate; all other transitions end the run.
      if (transition.kind !== 'async_running')
        revokeApiKeyByName(`stage-run:${stageRun.id}`, db)

      return result!
    })

    const { updatedRunId, newRunId } = txn() as { updatedRunId: string, newRunId: string | null }
    // Return the run that the caller expects to inspect (the updated one for
    // most transitions, or the new iteration for `iterate`).
    const resultRun = getStageRunById(newRunId ?? updatedRunId)!

    // Fire onTaskChanged for every transition so SSE listeners see the
    // happy path too — `next`, `wait_user`, `iterate`, `on_hold`, `done`,
    // `async_running`, `fail`. Without this callback the kanban only
    // updates on permission requests and failures, missing healthy
    // stage transitions like planning→approval1.
    this.onTaskChanged?.(task.id, { transitionKind: transition.kind })

    // Fire the onStageFailed callback AFTER the transaction commits so
    // listeners see a consistent DB state. Covers both the explicit
    // `fail` transition and the iteration-limit branch of `iterate`.
    if (this.onStageFailed) {
      const becameFailed
        = transition.kind === 'fail'
          || (transition.kind === 'iterate' && stageRun.iteration + 1 >= task.maxIterations)
      if (becameFailed) {
        const errorMsg = transition.kind === 'fail'
          ? transition.error
          : `iteration limit reached (${task.maxIterations})`
        this.onStageFailed(task.id, {
          stageRunId: stageRun.id,
          stage: stageRun.stage,
          iteration: stageRun.iteration,
          error: errorMsg,
        })
      }
    }

    return resultRun
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
      if (result.kind === 'still_running') {
        // Eagerly attach session_id while the agent is still running so the
        // frontend cross-link banner and live session tab work without waiting
        // for the completion detector (which only runs after the PID exits).
        if (!run.sessionId && run.startedAt)
          void this.tryAttachSessionId(run.id, cwd, run.startedAt)
        continue
      }

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
      // from hard failure (prose-only, missing session, etc.). The
      // completionDetector marks retryable schema rejections explicitly;
      // everything else is a hard fail even if `output` carries a prose
      // snippet for display. The hard-fail branch still forwards that
      // snippet so the modal shows what the agent actually said.
      if (!result.retryable) {
        this.applyTransition(task, fresh, {
          kind: 'fail',
          error: result.error ?? 'unknown failure',
          output: result.output,
        })
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

  private async tryAttachSessionId(stageRunId: string, cwd: string, startedAt: string): Promise<void> {
    try {
      const sid = await findNewestSessionId(cwd, startedAt)
      if (sid)
        attachSessionId(stageRunId, sid)
    }
    catch { /* non-critical: completion detector will attach it after the run ends */ }
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
    // whose latest stage_run is in a user-blocking state:
    //   - `awaiting_user` — wait_user does NOT change currentStage, so a
    //     task with an awaiting_user run on its current stage must sit.
    //   - `failed` — the stage ran and died; the user must explicitly
    //     hit Retry or Analyze. Auto-picking would burn tokens in an
    //     infinite loop until iteration budget hits zero.
    const readyToProgress = listPickableTasks().filter((t) => {
      if (busyTaskIds.has(t.id))
        return false
      const latest = getLatestStageRun(t.id, t.currentStage)
      return latest?.status !== 'awaiting_user' && latest?.status !== 'failed'
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
  return stage === 'done' || stage === 'cancelled'
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
