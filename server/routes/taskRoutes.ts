import type express from 'express'
import type { NotificationEventType, PipelineStage, PipelineTask } from '../../src/types.js'
import type { Dispatcher } from '../notifications/dispatcher.js'
import type { PipelineOrchestrator } from '../pipeline/orchestrator.js'
import { consola } from 'consola'
import { Router } from 'express'
import { DEPENDENCY_CANCEL_ACTIONS, DEPENDENCY_REQUIRED_STAGES, SLUG_PATTERN_MESSAGE, SLUG_RE, VALID_STAGES } from '../constants.js'
import { appendAudit, listAuditForTask } from '../db/auditRepo.js'
import {
  listFeedbackForTask,
} from '../db/feedbackRepo.js'
import { getAllConfig, getPipelineConfigNumber, listPreferences, setConfig, setPipelineConfig, setPreference } from '../db/notificationConfigRepo.js'
import {
  createPermissionRequest,
  createTaskPermission,
  deleteTaskPermission,
  getPermissionRequestById,
  listPendingPermissionRequests,
  listTaskPermissions,
  resolvePermissionRequest,
} from '../db/permissionsRepo.js'
import {
  getLatestStageRunForTask,
  getStageRunById,
  listStageRunsForTask,
} from '../db/stageRunsRepo.js'
import {
  addDependency,
  getDependenciesFor,
  getDependentsOf,
  removeDependencyById,
} from '../db/taskDependenciesRepo.js'
import {
  createTask,
  deleteTask,
  getTaskById,
  getTaskBySlug,
  listTasks,
  listTasksByStage,
  updateTask,
} from '../db/tasksRepo.js'
import { resolvedProjectDir } from '../pipeline/sessionOutputReader.js'
import { spawnAnalysisAgent } from '../services/analysisSpawner.js'
import { recommendParallelism } from '../services/resourceRecommender.js'
import { createWorktree, removeWorktree } from '../services/worktreeManager.js'

type RejectCrossOrigin = (req: express.Request, res: express.Response) => boolean

export interface TaskRouterDeps {
  rejectCrossOrigin: RejectCrossOrigin
  orchestrator: PipelineOrchestrator
  broadcastTaskEvent: (event: TaskEvent) => void
  dispatcher?: Dispatcher
}

export interface TaskEvent {
  type: 'task_created' | 'task_updated' | 'task_deleted' | 'stage_run_updated' | 'permission_request'
  taskId: string
  payload?: unknown
}

const VALID_EVENT_TYPES = new Set<NotificationEventType>([
  'on_hold',
  'approval_needed',
  'completed',
  'failed',
  'budget_exceeded',
  'iteration_warning',
])

const USER_WAIT_STAGES = new Set<PipelineStage>(['on_hold'])

/**
 * Decorate a task with live stage_run status so the frontend can show
 * `awaiting_user` / `on_hold` state on the board without refetching per task.
 *
 * IMPORTANT: stage_run status only contributes to `needsUser` when the latest
 * run belongs to the task's CURRENT stage. Otherwise a stale awaiting_user
 * run from a prior stage would incorrectly flag an advanced task.
 */
export function enrichTask(task: PipelineTask): PipelineTask {
  const latest = getLatestStageRunForTask(task.id)
  const latestBelongsToCurrent = latest?.stage === task.currentStage
  const latestStatus = latestBelongsToCurrent ? (latest?.status ?? null) : null
  const currentIteration = latestBelongsToCurrent ? (latest?.iteration ?? 0) : 0
  const needsUser
    = USER_WAIT_STAGES.has(task.currentStage)
      || latestStatus === 'awaiting_user'
      || latestStatus === 'on_hold'
      // `failed` is now a stage_run status, not a task lifecycle state:
      // the task stays on its current stage and the UI surfaces it in
      // the Needs You column with Retry + Analyze actions.
      || latestStatus === 'failed'
  // Live session surfacing: the modal's "follow along" pane needs the
  // session_id of whichever stage_run the user would most like to watch.
  // Prefer a currently-running run on the current stage; otherwise fall
  // back to the most-recent run that has a session_id attached so the
  // pane still shows the last-seen transcript between runs.
  const activeSessionId = latest?.sessionId ?? null
  const activePid = latest?.status === 'running' ? (latest?.pid ?? null) : null
  return {
    ...task,
    needsUser,
    latestStageRunStatus: latestStatus,
    currentIteration,
    activeSessionId,
    activePid,
  }
}

export function createTaskRouter(deps: TaskRouterDeps): Router {
  const router = Router()

  // Mutation sub-router — all POST/PUT/PATCH/DELETE routes register here so
  // `rejectCrossOrigin` runs once as middleware instead of being copy-pasted
  // into every handler (and silently forgotten on new endpoints).
  const mutationRouter = Router()
  mutationRouter.use((req, res, next) => {
    if (deps.rejectCrossOrigin(req, res))
      return
    next()
  })

  // Convenience: broadcast a task_updated with a freshly enriched payload.
  // Plain getTaskById() returns un-enriched rows missing latestStageRunStatus
  // and needsUser — the kanban then drops the run-status chip on every event.
  function broadcastEnrichedUpdate(taskId: string): void {
    const task = getTaskById(taskId)
    if (!task)
      return
    deps.broadcastTaskEvent({ type: 'task_updated', taskId, payload: enrichTask(task) })
  }

  // ─── Tasks CRUD ────────────────

  router.get('/tasks', (req, res) => {
    const stage = req.query.stage as string | undefined
    if (stage) {
      if (!VALID_STAGES.has(stage as PipelineStage)) {
        res.status(400).json({ error: 'Invalid stage' })
        return
      }
      res.json(listTasksByStage(stage as PipelineStage).map(enrichTask))
      return
    }
    res.json(listTasks().map(enrichTask))
  })

  router.get('/tasks/:id', (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    res.json(enrichTask(task))
  })

  mutationRouter.post('/tasks', async (req, res) => {
    const { slug, title, description, cwd, worktreePath, sourceBranch, targetBranch, parentTaskId, maxIterations, tokenBudget, costBudgetCents, stageTimeoutSeconds, metadata, useWorktree, silverBullet, priority, stage } = req.body ?? {}

    if (!slug || typeof slug !== 'string' || !SLUG_RE.test(slug)) {
      res.status(400).json({ error: SLUG_PATTERN_MESSAGE })
      return
    }
    if (!title || typeof title !== 'string') {
      res.status(400).json({ error: 'title is required' })
      return
    }
    if (!cwd || typeof cwd !== 'string') {
      res.status(400).json({ error: 'cwd is required' })
      return
    }
    if (getTaskBySlug(slug)) {
      res.status(409).json({ error: 'slug already exists' })
      return
    }
    if (priority !== undefined && priority !== 'high' && priority !== 'medium' && priority !== 'low') {
      res.status(400).json({ error: 'priority must be one of high|medium|low' })
      return
    }
    if (stage !== undefined && (typeof stage !== 'string' || !VALID_STAGES.has(stage as PipelineStage))) {
      res.status(400).json({ error: 'invalid stage' })
      return
    }

    let initialWorktreePath: string | null = typeof worktreePath === 'string' ? worktreePath : null
    let weCreatedWorktree = false

    // useWorktree=true triggers real git worktree creation. Default is
    // off (caller must opt-in or provide an explicit worktreePath).
    if (useWorktree === true && !initialWorktreePath) {
      try {
        initialWorktreePath = await createWorktree({
          cwd,
          slug,
          branch: typeof sourceBranch === 'string' ? sourceBranch : null,
        })
        weCreatedWorktree = true
      }
      catch (err) {
        res.status(400).json({
          error: `worktree creation failed: ${(err as Error).message}`,
        })
        return
      }
    }

    try {
      const task = createTask({
        slug,
        title,
        description: typeof description === 'string' ? description : null,
        cwd,
        worktreePath: initialWorktreePath,
        sourceBranch: typeof sourceBranch === 'string' ? sourceBranch : null,
        targetBranch: typeof targetBranch === 'string' ? targetBranch : null,
        parentTaskId: typeof parentTaskId === 'string' ? parentTaskId : null,
        maxIterations: typeof maxIterations === 'number' ? maxIterations : undefined,
        tokenBudget: typeof tokenBudget === 'number' ? tokenBudget : null,
        costBudgetCents: typeof costBudgetCents === 'number' ? costBudgetCents : null,
        stageTimeoutSeconds: typeof stageTimeoutSeconds === 'number' ? stageTimeoutSeconds : undefined,
        metadata: typeof metadata === 'object' && metadata !== null ? metadata : null,
        silverBullet: silverBullet === true,
        priority: (priority === 'high' || priority === 'medium' || priority === 'low') ? priority : undefined,
        currentStage: (typeof stage === 'string' && VALID_STAGES.has(stage as PipelineStage)) ? (stage as PipelineStage) : 'konzept',
      })
      deps.broadcastTaskEvent({ type: 'task_created', taskId: task.id, payload: enrichTask(task) })
      res.status(201).json(enrichTask(task))
    }
    catch (err) {
      // Rollback: if we created the worktree but DB insert failed, remove
      // the orphaned worktree so the slug is reusable on retry.
      if (weCreatedWorktree && initialWorktreePath) {
        try {
          await removeWorktree(cwd, initialWorktreePath, { force: true })
        }
        catch (cleanupErr) {
          consola.warn(
            `[taskRoutes] worktree rollback failed for ${initialWorktreePath}:`,
            (cleanupErr as Error).message,
          )
        }
      }
      res.status(500).json({ error: (err as Error).message })
    }
  })

  mutationRouter.patch('/tasks/:id', (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    const body = req.body ?? {}

    // Clients must NOT write currentStage directly — stage transitions go
    // through /progress, /cancel, or /api/refine/:id/confirm so the state
    // machine and notifications stay consistent.
    if (body.currentStage !== undefined) {
      res.status(400).json({
        error: 'currentStage cannot be set via PATCH — use /progress, /cancel, or /api/refine/:id/confirm',
      })
      return
    }

    // Whitelist the fields clients are allowed to update. Anything else
    // (cwd, parentTaskId, worktreePath, etc.) is intentionally off-limits.
    const allowed: Record<string, unknown> = {}
    if (typeof body.title === 'string')
      allowed.title = body.title
    if (body.description === null || typeof body.description === 'string')
      allowed.description = body.description
    if (typeof body.maxIterations === 'number')
      allowed.maxIterations = body.maxIterations
    if (body.tokenBudget === null || typeof body.tokenBudget === 'number')
      allowed.tokenBudget = body.tokenBudget
    if (body.costBudgetCents === null || typeof body.costBudgetCents === 'number')
      allowed.costBudgetCents = body.costBudgetCents
    if (typeof body.stageTimeoutSeconds === 'number')
      allowed.stageTimeoutSeconds = body.stageTimeoutSeconds
    if (
      body.metadata === null
      || (typeof body.metadata === 'object' && body.metadata !== null && !Array.isArray(body.metadata))
    ) {
      allowed.metadata = body.metadata
    }
    if (typeof body.silverBullet === 'boolean')
      allowed.silverBullet = body.silverBullet
    if (body.priority === 'high' || body.priority === 'medium' || body.priority === 'low')
      allowed.priority = body.priority

    const updated = updateTask(req.params.id, allowed)
    broadcastEnrichedUpdate(req.params.id)
    res.json(updated)
  })

  mutationRouter.delete('/tasks/:id', async (req, res) => {
    // Fetch first so we know the worktree path for cleanup.
    const task = getTaskById(req.params.id)
    if (!task) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    const ok = deleteTask(req.params.id)
    if (!ok) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    // Broadcast + respond first so the UI updates immediately. Worktree
    // cleanup happens after — a stale directory is a cleanup concern, not
    // a UX concern, and `git worktree remove` can block on lock files.
    deps.broadcastTaskEvent({ type: 'task_deleted', taskId: req.params.id })
    res.status(204).end()

    if (task.worktreePath) {
      try {
        await removeWorktree(task.cwd, task.worktreePath, { force: true })
      }
      catch (err) {
        consola.warn(
          `[taskRoutes] worktree cleanup on delete failed for ${task.worktreePath}:`,
          (err as Error).message,
        )
      }
    }
  })

  // ─── Stage progression & approvals ────────────────

  mutationRouter.post('/tasks/:id/progress', async (req, res) => {
    const run = await deps.orchestrator.progressTask(req.params.id)
    if (!run) {
      res.status(409).json({ error: 'Task cannot progress (terminal, missing, or slot full)' })
      return
    }
    const task = getTaskById(req.params.id)
    broadcastEnrichedUpdate(req.params.id)
    res.json({ task, stageRun: run })
  })

  router.get('/tasks/:id/feedback', (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    res.json(listFeedbackForTask(req.params.id))
  })

  mutationRouter.post('/tasks/:id/cancel', (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    if (task.currentStage === 'cancelled' || task.currentStage === 'done') {
      res.status(400).json({ error: `Task is already ${task.currentStage}` })
      return
    }
    updateTask(req.params.id, { currentStage: 'cancelled' })
    deps.orchestrator.notifyTaskTerminated(req.params.id, 'cancelled')
    const updated = getTaskById(req.params.id)
    broadcastEnrichedUpdate(req.params.id)
    res.json(updated)
  })

  // ─── Retry a failed stage ────────────────
  //
  // Creates a fresh iteration of the task's current stage. Only valid
  // when the latest stage_run on that stage is `failed`. The orchestrator's
  // `ensureStageRun` already creates a new iteration when the latest run
  // is non-pending/non-running, so this endpoint just validates intent,
  // writes an audit trail, and defers to `progressTask`.
  mutationRouter.post('/tasks/:id/retry', async (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    const latest = getLatestStageRunForTask(req.params.id)
    if (!latest || latest.stage !== task.currentStage || latest.status !== 'failed') {
      res.status(409).json({ error: 'Task has no failed stage run to retry on its current stage' })
      return
    }
    appendAudit({
      taskId: task.id,
      actor: 'user',
      action: 'retry_requested',
      details: { stage: latest.stage, iteration: latest.iteration },
    })
    const run = await deps.orchestrator.progressTask(task.id)
    if (!run) {
      res.status(409).json({ error: 'Task could not progress (slot full, no handler, or terminal)' })
      return
    }
    const updated = getTaskById(task.id)
    broadcastEnrichedUpdate(task.id)
    res.json({ task: updated, stageRun: run })
  })

  // ─── Launch an ad-hoc analysis session ────────────────
  //
  // Spawns an independent Claude CLI session in the task's worktree with
  // a pre-built prompt containing task identity, failure details, and
  // pointers to the last session JSONLs. The session is NOT tracked as a
  // stage_run — it shows up in the dashboard's normal agent-monitoring
  // view via processScanner finding a `claude` PID in the task's cwd.
  mutationRouter.post('/tasks/:id/analyze', async (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    const latest = getLatestStageRunForTask(req.params.id)
    if (!latest) {
      res.status(409).json({ error: 'Task has no stage runs to analyze' })
      return
    }

    // Gather the error summary and pointers to the JSONLs on disk. Both
    // go through resolvedProjectDir so the paths match what Claude CLI
    // actually wrote (realpath-resolved, dot/underscore/slash encoded).
    const cwd = task.worktreePath || task.cwd
    const projectDir = await resolvedProjectDir(cwd)
    const sessionLogPaths: string[] = []
    const runs = listStageRunsForTask(req.params.id)
    for (const r of runs) {
      if (r.sessionId)
        sessionLogPaths.push(`${projectDir}/${r.sessionId}.jsonl`)
    }

    const errorSummary = (() => {
      const out = latest.output as Record<string, unknown> | null
      if (out && typeof out.error === 'string')
        return `latest stage_run (${latest.stage} iter ${latest.iteration}) failed with: ${out.error}`
      return `latest stage_run (${latest.stage} iter ${latest.iteration}) status: ${latest.status}`
    })()

    try {
      const result = spawnAnalysisAgent({
        task,
        failedRun: latest,
        errorSummary,
        sessionLogPaths,
      })
      appendAudit({
        taskId: task.id,
        actor: 'user',
        action: 'analysis_session_spawned',
        details: { pid: result.pid, cwd: result.cwd },
      })
      res.status(202).json({ pid: result.pid, cwd: result.cwd })
    }
    catch (err) {
      consola.error('[taskRoutes] analysis spawn failed:', err)
      res.status(500).json({ error: (err as Error).message })
    }
  })

  // ─── Task stage runs & audit ────────────────

  router.get('/tasks/:id/stage-runs', (req, res) => {
    res.json(listStageRunsForTask(req.params.id))
  })

  router.get('/tasks/:id/audit', (req, res) => {
    res.json(listAuditForTask(req.params.id))
  })

  // ─── Permissions ────────────────

  router.get('/tasks/:id/permissions', (req, res) => {
    res.json(listTaskPermissions(req.params.id))
  })

  mutationRouter.post('/tasks/:id/permissions', (req, res) => {
    const { tool, pattern, granted, preApproved } = req.body ?? {}
    if (!tool || typeof tool !== 'string') {
      res.status(400).json({ error: 'tool is required' })
      return
    }
    const perm = createTaskPermission({
      taskId: req.params.id,
      tool,
      pattern: typeof pattern === 'string' ? pattern : null,
      granted: Boolean(granted),
      preApproved: Boolean(preApproved),
      decidedBy: 'user',
    })
    res.status(201).json(perm)
  })

  mutationRouter.delete('/tasks/:id/permissions/:permId', (req, res) => {
    const ok = deleteTaskPermission(req.params.permId)
    if (!ok) {
      res.status(404).json({ error: 'Permission not found' })
      return
    }
    res.status(204).end()
  })

  // ─── Runtime permission requests (agent asked mid-stage) ────────────────

  router.get('/tasks/:id/permission-requests', (req, res) => {
    const runs = listStageRunsForTask(req.params.id)
    const pending = runs.flatMap(r => listPendingPermissionRequests(r.id))
    res.json(pending)
  })

  mutationRouter.post('/permission-requests', (req, res) => {
    const { stageRunId, tool, pattern, reason } = req.body ?? {}
    if (!stageRunId || typeof stageRunId !== 'string') {
      res.status(400).json({ error: 'stageRunId is required' })
      return
    }
    if (!tool || typeof tool !== 'string') {
      res.status(400).json({ error: 'tool is required' })
      return
    }
    const run = getStageRunById(stageRunId)
    if (!run) {
      res.status(404).json({ error: 'stage run not found' })
      return
    }
    const reqRow = createPermissionRequest({
      stageRunId,
      tool,
      pattern: typeof pattern === 'string' ? pattern : null,
      reason: typeof reason === 'string' ? reason : null,
    })
    deps.broadcastTaskEvent({ type: 'permission_request', taskId: run.taskId, payload: reqRow })

    // Fire-and-forget notification for ON HOLD state — catch rejections so
    // an adapter failure never becomes an unhandled promise rejection.
    if (deps.dispatcher) {
      const task = getTaskById(run.taskId)
      if (task) {
        deps.dispatcher
          .dispatch({
            eventType: 'on_hold',
            title: `Task "${task.title}" needs permission`,
            body: `Agent requests ${tool}${pattern ? ` (${pattern})` : ''}${reason ? `\nReason: ${reason}` : ''}`,
            taskId: task.id,
            taskSlug: task.slug,
            severity: 'warning',
          })
          .catch(err => consola.warn('[notifications] dispatch failed:', (err as Error).message))
      }
    }

    res.status(201).json(reqRow)
  })

  mutationRouter.post('/permission-requests/:id/resolve', async (req, res) => {
    const { outcome } = req.body ?? {}
    if (outcome !== 'granted' && outcome !== 'denied') {
      res.status(400).json({ error: 'outcome must be granted|denied' })
      return
    }
    const existing = getPermissionRequestById(req.params.id)
    if (!existing) {
      res.status(404).json({ error: 'request not found' })
      return
    }
    const resolved = resolvePermissionRequest(req.params.id, outcome)
    const run = getStageRunById(existing.stageRunId)
    if (run) {
      // If granted, persist a permission row so the tool stays unlocked for the task
      if (outcome === 'granted') {
        createTaskPermission({
          taskId: run.taskId,
          tool: existing.tool,
          pattern: existing.pattern,
          granted: true,
          preApproved: false,
          decidedBy: 'user',
        })
      }
      // Try to resume the task
      await deps.orchestrator.resumeFromUser(run.taskId)
      broadcastEnrichedUpdate(run.taskId)
    }
    res.json(resolved)
  })

  // ─── Task dependencies ────────────────

  // GET /tasks/:id/dependencies — list all prerequisites for a task
  router.get('/tasks/:id/dependencies', (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    res.json(getDependenciesFor(req.params.id))
  })

  // GET /tasks/:id/dependents — list all tasks waiting on this task
  router.get('/tasks/:id/dependents', (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    res.json(getDependentsOf(req.params.id))
  })

  // POST /tasks/:id/dependencies — add a dependency
  mutationRouter.post('/tasks/:id/dependencies', (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    const { dependsOnId, requiredStage = 'done', onCancelAction = 'on_hold' } = req.body as {
      dependsOnId?: string
      requiredStage?: 'done' | 'cancelled'
      onCancelAction?: 'cancel' | 'start' | 'on_hold'
    }
    if (!dependsOnId) {
      res.status(400).json({ error: 'dependsOnId is required' })
      return
    }
    if (!getTaskById(dependsOnId)) {
      res.status(404).json({ error: `Prerequisite task not found: ${dependsOnId}` })
      return
    }
    if (!(DEPENDENCY_REQUIRED_STAGES as readonly string[]).includes(requiredStage)) {
      res.status(400).json({ error: 'requiredStage must be done or cancelled' })
      return
    }
    if (!(DEPENDENCY_CANCEL_ACTIONS as readonly string[]).includes(onCancelAction)) {
      res.status(400).json({ error: 'onCancelAction must be cancel, start, or on_hold' })
      return
    }
    try {
      const dep = addDependency(req.params.id, dependsOnId, requiredStage, onCancelAction)
      broadcastEnrichedUpdate(req.params.id)
      res.status(201).json(dep)
    }
    catch (err) {
      const msg = (err as Error).message
      if (msg.includes('cycle')) {
        res.status(400).json({ error: msg })
        return
      }
      if (msg.includes('UNIQUE')) {
        res.status(409).json({ error: 'Dependency already exists' })
        return
      }
      throw err
    }
  })

  // DELETE /tasks/:id/dependencies/:depId — remove a dependency by its row ID
  mutationRouter.delete('/tasks/:id/dependencies/:depId', (req, res) => {
    const removed = removeDependencyById(req.params.depId, req.params.id)
    if (!removed) {
      res.status(404).json({ error: 'Dependency not found' })
      return
    }
    broadcastEnrichedUpdate(req.params.id)
    res.json({ removed })
  })

  // ─── Pipeline config (maxParallel, etc.) ────────────────

  router.get('/pipeline/config', (_req, res) => {
    res.json({
      maxParallelOrchestrators: getPipelineConfigNumber('maxParallelOrchestrators', 3),
    })
  })

  router.get('/pipeline/recommendation', (_req, res) => {
    res.json(recommendParallelism())
  })

  mutationRouter.put('/pipeline/config', (req, res) => {
    const { maxParallelOrchestrators } = req.body ?? {}
    if (maxParallelOrchestrators !== undefined) {
      const n = Number(maxParallelOrchestrators)
      if (!Number.isFinite(n) || n < 1 || n > 50) {
        res.status(400).json({ error: 'maxParallelOrchestrators must be 1..50' })
        return
      }
      setPipelineConfig('maxParallelOrchestrators', String(Math.floor(n)))
    }
    res.json({
      maxParallelOrchestrators: getPipelineConfigNumber('maxParallelOrchestrators', 3),
    })
  })

  // ─── Notification preferences & adapter config ────────────────

  router.get('/notifications/preferences', (_req, res) => {
    res.json(listPreferences())
  })

  mutationRouter.put('/notifications/preferences/:eventType', (req, res) => {
    const eventType = req.params.eventType as NotificationEventType
    if (!VALID_EVENT_TYPES.has(eventType)) {
      res.status(400).json({ error: 'Unknown eventType' })
      return
    }
    const { channels, enabled } = req.body ?? {}
    if (!Array.isArray(channels)) {
      res.status(400).json({ error: 'channels must be array' })
      return
    }
    const pref = setPreference(eventType, channels, enabled !== false)
    res.json(pref)
  })

  router.get('/notifications/config', (_req, res) => {
    res.json(getAllConfig())
  })

  mutationRouter.put('/notifications/config', (req, res) => {
    const updates = req.body ?? {}
    for (const [key, value] of Object.entries(updates)) {
      if (typeof key !== 'string')
        continue
      setConfig(key, typeof value === 'string' ? value : value === null ? null : String(value))
    }
    res.json(getAllConfig())
  })

  // Mount mutation sub-router so its rejectCrossOrigin middleware guards
  // every POST/PUT/PATCH/DELETE route registered above.
  router.use(mutationRouter)

  return router
}
