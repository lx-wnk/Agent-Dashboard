import type express from 'express'
import type { NotificationEventType, PipelineStage, PipelineTask } from '../../src/types.js'
import type { AuditRow } from '../db/rowMappers.js'
import type { Dispatcher } from '../notifications/dispatcher.js'
import type { PipelineOrchestrator } from '../pipeline/orchestrator.js'
import { randomBytes } from 'node:crypto'
import process from 'node:process'
import { consola } from 'consola'
import { Router } from 'express'
import { isAuthEnabled } from '../auth/requireAuth.js'
import { DEPENDENCY_CANCEL_ACTIONS, DEPENDENCY_REQUIRED_STAGES, SLUG_PATTERN_MESSAGE, SLUG_RE, UUID_RE, VALID_STAGES } from '../constants.js'
import { appendAudit, listAuditForTask } from '../db/auditRepo.js'
import { getDb } from '../db/client.js'
import {
  listFeedbackForTask,
} from '../db/feedbackRepo.js'
import { getAllConfig, getConfig, getPipelineConfigNumber, listPreferences, setConfig, setPipelineConfig, setPreference } from '../db/notificationConfigRepo.js'
import {
  countPermissionRequestsForStageRun,
  createPermissionRequest,
  createTaskPermission,
  deleteTaskPermission,
  getPermissionRequestById,
  getPermissionReRequestCounts,
  listPendingPermissionRequests,
  listTaskPermissions,
  resolvePermissionRequest,
} from '../db/permissionsRepo.js'
import { rowToAuditEntry } from '../db/rowMappers.js'
import {
  getLatestStageRunForTask,
  getLatestStageRunsForTasks,
  getStageRunById,
  listStageRunsForTask,
  updateStageRun,
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
  listTasksForUser,
  updateTask,
} from '../db/tasksRepo.js'
import { isPidAlive } from '../pipeline/sessionManager.js'
import { findNewestSessionId, readLastStageJsonOutput, resolvedProjectDir } from '../pipeline/sessionOutputReader.js'
import { spawnAnalysisAgent } from '../services/analysisSpawner.js'
import { applyPermissionTemplateByName, bulkGrantPermissions, inheritParentPermissions, validatePermissionEntry } from '../services/approvalUtils.js'
import { getGitStatus, runGitAction } from '../services/gitService.js'
import { listTemplateNames } from '../services/permissionTemplates.js'
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
// Returns false for non-admin users trying to access another user's task.
// Returns 404 (not 403) at call sites to avoid leaking task existence.
export function canAccessTask(task: PipelineTask, user: { id: string, isAdmin: boolean }): boolean {
  return user.isAdmin || task.userId === user.id
}

/**
 * Per-task dedup map for ad-hoc analysis sessions spawned via
 * `POST /tasks/:id/analyze`. See the route handler for full rationale.
 * Module-level so the in-memory state survives across requests within the
 * same dashboard process. Exported for tests; do not write from outside the
 * route handler in production code.
 */
export const activeAnalysisTasks = new Map<string, number>()

/**
 * Builds the resume prompt the orchestrator passes to the re-spawned stage
 * agent after the user grants a permission. The note must:
 *   1. Acknowledge the specific grant so the agent knows the pause is over.
 *   2. Force a forward-scan of remaining work — the agent's previous
 *      `request_permission` call clearly missed something, so we instruct
 *      it to enumerate ALL still-needed tools in a single bulk call before
 *      continuing. This breaks the per-tool kill/restart loop.
 *   3. When this is the 2nd or later cycle on the same stage_run, escalate
 *      the wording so the agent treats forward-scan as mandatory rather
 *      than nice-to-have.
 *
 * Exported for unit-testing the wording — the actual loop check lives in
 * the resolve route.
 */
export function buildPermissionGrantHandoffNote(input: {
  tool: string
  pattern: string | null
  cycleCount: number
}): string {
  const { tool, pattern, cycleCount } = input
  const toolStr = pattern ? `${tool} (${pattern})` : tool
  const ordinal = cycleCount >= 2
    ? `\n\nThis is permission cycle #${cycleCount} on this stage_run — your prior request_permission call did not cover everything you actually needed. STOP and forward-scan the entire remaining plan now.`
    : ''
  return `[PERMISSION GRANTED] You requested permission for "${toolStr}". It has been granted.${ordinal}\n\nBefore your next tool call, scan ALL remaining work in this stage and request_permission ONCE in a single bulk call with every additional tool/pattern you anticipate needing. Pre-granted entries auto-resolve silently; only genuinely new ones surface as ON HOLD. Do not request piecemeal — every missed tool restarts this stage.\n\nThen resume exactly where you left off.`
}

/**
 * Bulk-grant variant of {@link buildPermissionGrantHandoffNote}. The user
 * resolved N pending permission_requests in one click; the resume prompt
 * lists every grant so the agent can attribute the unblock and forward-scan
 * with full awareness of what was just unlocked.
 *
 * Exported for unit-testing the wording.
 */
export function buildBulkPermissionGrantHandoffNote(input: {
  grantedTools: Array<{ tool: string, pattern: string | null }>
  cycleCount: number
}): string {
  const { grantedTools, cycleCount } = input
  const list = grantedTools
    .map(g => g.pattern ? `  - ${g.tool} (${g.pattern})` : `  - ${g.tool}`)
    .join('\n')
  const ordinal = cycleCount >= 2
    ? `\n\nThis is permission cycle #${cycleCount} on this stage_run — your prior request_permission call did not cover everything you actually needed. STOP and forward-scan the entire remaining plan now.`
    : ''
  return `[PERMISSIONS GRANTED — BULK] You requested ${grantedTools.length} permission${grantedTools.length === 1 ? '' : 's'} and the user granted all of them in a single decision:\n${list}${ordinal}\n\nBefore your next tool call, scan ALL remaining work in this stage and request_permission ONCE in a single bulk call with every additional tool/pattern you anticipate needing. Pre-granted entries auto-resolve silently; only genuinely new ones surface as ON HOLD. Do not request piecemeal — every missed tool restarts this stage.\n\nThen resume exactly where you left off.`
}

export function enrichTask(task: PipelineTask): PipelineTask {
  const latest = getLatestStageRunForTask(task.id)
  const latestBelongsToCurrent = latest?.stage === task.currentStage
  const latestStatus = latestBelongsToCurrent ? (latest?.status ?? null) : null
  const currentIteration = latestBelongsToCurrent ? (latest?.iteration ?? 0) : 0
  // Query the pending permission_requests count once and reuse it for both
  // hasPendingPermissions (surfaces a running task as "needs user") and
  // blockedByPendingPermissions (surfaces a terminal/zombie task whose
  // respawn is being held by the orchestrator's lingering-pending gate).
  // The query is mirrored from runProgressTaskLocked so the UI flag tracks
  // the gate's actual block condition exactly.
  const pendingPermsCount
    = latestBelongsToCurrent && latest != null
      ? listPendingPermissionRequests(latest.id).length
      : 0
  const hasPendingPermissions = latestStatus === 'running' && pendingPermsCount > 0
  const isTerminal = latestStatus === 'failed' || latestStatus === 'done'
  const isZombieAwait
    = latestStatus === 'awaiting_user'
      && latest != null
      && (latest.pid === null || !isPidAlive(latest.pid))
  const blockedByPendingPermissions = (isTerminal || isZombieAwait) && pendingPermsCount > 0
  const needsUser
    = USER_WAIT_STAGES.has(task.currentStage)
      || latestStatus === 'awaiting_user'
      || latestStatus === 'on_hold'
      // `failed` is now a stage_run status, not a task lifecycle state:
      // the task stays on its current stage and the UI surfaces it in
      // the Needs You column with Retry + Analyze actions.
      || latestStatus === 'failed'
      || hasPendingPermissions
      || blockedByPendingPermissions
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
    blockedByPendingPermissions,
  }
}

/**
 * Bulk version of enrichTask — fetches all latest stage runs in one DB query.
 * Use this for list endpoints; use enrichTask for single-task responses.
 */
export function enrichTasksBulk(tasks: PipelineTask[]): PipelineTask[] {
  if (tasks.length === 0)
    return []
  const latestRunMap = getLatestStageRunsForTasks(tasks.map(t => t.id))
  return tasks.map((task) => {
    const latest = latestRunMap.get(task.id) ?? null
    const latestBelongsToCurrent = latest?.stage === task.currentStage
    const latestStatus = latestBelongsToCurrent ? (latest?.status ?? null) : null
    const currentIteration = latestBelongsToCurrent ? (latest?.iteration ?? 0) : 0
    const pendingPermsCount
      = latestBelongsToCurrent && latest != null
        ? listPendingPermissionRequests(latest.id).length
        : 0
    const hasPendingPermissions = latestStatus === 'running' && pendingPermsCount > 0
    const isTerminal = latestStatus === 'failed' || latestStatus === 'done'
    const isZombieAwait
      = latestStatus === 'awaiting_user'
        && latest != null
        && (latest.pid === null || !isPidAlive(latest.pid))
    const blockedByPendingPermissions = (isTerminal || isZombieAwait) && pendingPermsCount > 0
    const needsUser
      = USER_WAIT_STAGES.has(task.currentStage)
        || latestStatus === 'awaiting_user'
        || latestStatus === 'on_hold'
        || latestStatus === 'failed'
        || hasPendingPermissions
        || blockedByPendingPermissions
    const activeSessionId = latest?.sessionId ?? null
    const activePid = latest?.status === 'running' ? (latest?.pid ?? null) : null
    return {
      ...task,
      needsUser,
      latestStageRunStatus: latestStatus,
      currentIteration,
      activeSessionId,
      activePid,
      blockedByPendingPermissions,
    }
  })
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

  const uuidParamGuard: express.RequestParamHandler = (_req, res, next, value) => {
    if (!UUID_RE.test(value)) {
      res.status(400).json({ error: 'Invalid task ID format' })
      return
    }
    next()
  }
  router.param('id', uuidParamGuard)
  mutationRouter.param('id', uuidParamGuard)

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
    const user = req.user! // set by requireAuth middleware
    if (stage) {
      if (!VALID_STAGES.has(stage as PipelineStage)) {
        res.status(400).json({ error: 'Invalid stage' })
        return
      }
      const all = listTasksForUser(user.id, user.isAdmin)
      res.json(enrichTasksBulk(all.filter(t => t.currentStage === stage)))
      return
    }
    res.json(enrichTasksBulk(listTasksForUser(user.id, user.isAdmin)))
  })

  router.get('/tasks/:id', (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task || !canAccessTask(task, req.user!)) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    res.json(enrichTask(task))
  })

  mutationRouter.post('/tasks', async (req, res) => {
    const { slug, title, description, cwd, worktreePath, sourceBranch, targetBranch, parentTaskId, maxIterations, tokenBudget, costBudgetCents, stageTimeoutSeconds, metadata, useWorktree, silverBullet, priority, stage, permissions, template, inheritPermissions } = req.body ?? {}

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
    if (typeof title === 'string' && title.length > 200)
      return void res.status(400).json({ error: 'title must be ≤ 200 characters' })
    if (typeof description === 'string' && description.length > 10_000)
      return void res.status(400).json({ error: 'description must be ≤ 10,000 characters' })
    if (typeof cwd === 'string' && cwd.length > 4096)
      return void res.status(400).json({ error: 'cwd must be ≤ 4096 characters' })
    if (maxIterations !== undefined && maxIterations !== null) {
      if (!Number.isInteger(maxIterations) || maxIterations < 1 || maxIterations > 100)
        return void res.status(400).json({ error: 'maxIterations must be an integer between 1 and 100' })
    }
    if (tokenBudget !== undefined && tokenBudget !== null) {
      if (!Number.isFinite(tokenBudget) || tokenBudget < 0)
        return void res.status(400).json({ error: 'tokenBudget must be a non-negative number' })
    }
    if (costBudgetCents !== undefined && costBudgetCents !== null) {
      if (!Number.isFinite(costBudgetCents) || costBudgetCents < 0)
        return void res.status(400).json({ error: 'costBudgetCents must be a non-negative number' })
    }
    if (template !== undefined && template !== null) {
      if (typeof template !== 'string' || !listTemplateNames().includes(template as never)) {
        return void res.status(400).json({
          error: `template must be one of: ${listTemplateNames().join(', ')}`,
        })
      }
    }
    if (permissions !== undefined && permissions !== null) {
      if (!Array.isArray(permissions))
        return void res.status(400).json({ error: 'permissions must be an array' })
      for (const p of permissions) {
        if (typeof p !== 'object' || p === null || typeof (p as { tool?: unknown }).tool !== 'string')
          return void res.status(400).json({ error: 'permissions[i].tool is required (string)' })
        const v = validatePermissionEntry((p as { tool: string }).tool, (p as { pattern?: string | null }).pattern ?? null)
        if (!v.ok)
          return void res.status(400).json({ error: `permission rejected: ${v.reason}` })
      }
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
        consola.error('[task] worktree creation failed:', err)
        res.status(400).json({
          error: 'Failed to create worktree',
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
        currentStage: (typeof stage === 'string' && VALID_STAGES.has(stage as PipelineStage)) ? (stage as PipelineStage) : 'concept',
        userId: isAuthEnabled() ? req.user!.id : null,
      })

      // Permission seeding: template → explicit permissions[] → parent inheritance.
      // Order matters: template first so explicit permissions[] can extend
      // (or in dup case, the existing-grant skip kicks in). Parent inheritance
      // is opt-in via inheritPermissions=true; skipped if explicit perms set.
      const permissionAuditDetail: Record<string, unknown> = {}
      if (typeof template === 'string') {
        const tplResult = applyPermissionTemplateByName(task.id, template)
        if (tplResult)
          permissionAuditDetail.template = { name: template, granted: tplResult.granted.length, skipped: tplResult.skipped.length }
      }
      if (Array.isArray(permissions) && permissions.length > 0) {
        const result = bulkGrantPermissions(
          task.id,
          permissions.map((p: { tool: string, pattern?: string | null, expiresAt?: string | null }) => ({
            tool: p.tool,
            pattern: p.pattern ?? null,
            expiresAt: p.expiresAt ?? null,
          })),
          { source: 'create_task:permissions[]' },
        )
        permissionAuditDetail.explicit = { granted: result.granted.length, skipped: result.skipped.length }
      }
      if (
        inheritPermissions === true
        && (!Array.isArray(permissions) || permissions.length === 0)
        && typeof parentTaskId === 'string'
      ) {
        const inhResult = inheritParentPermissions(task.id, parentTaskId)
        permissionAuditDetail.inherited = { fromParent: parentTaskId, granted: inhResult.granted.length }
      }

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
      consola.error('[task] createTask failed:', err)
      res.status(500).json({ error: 'Internal error' })
    }
  })

  mutationRouter.patch('/tasks/:id', (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task || !canAccessTask(task, req.user!)) {
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
    if (typeof body.title === 'string' && body.title.length > 200)
      return void res.status(400).json({ error: 'title must be ≤ 200 characters' })
    if (typeof body.description === 'string' && body.description.length > 10_000)
      return void res.status(400).json({ error: 'description must be ≤ 10,000 characters' })
    if (body.maxIterations !== undefined && body.maxIterations !== null) {
      if (!Number.isInteger(body.maxIterations) || body.maxIterations < 1 || body.maxIterations > 100)
        return void res.status(400).json({ error: 'maxIterations must be an integer between 1 and 100' })
    }
    if (body.tokenBudget !== undefined && body.tokenBudget !== null) {
      if (!Number.isFinite(body.tokenBudget) || body.tokenBudget < 0)
        return void res.status(400).json({ error: 'tokenBudget must be a non-negative number' })
    }
    if (body.costBudgetCents !== undefined && body.costBudgetCents !== null) {
      if (!Number.isFinite(body.costBudgetCents) || body.costBudgetCents < 0)
        return void res.status(400).json({ error: 'costBudgetCents must be a non-negative number' })
    }
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
    if (!task || !canAccessTask(task, req.user!)) {
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
    deps.broadcastTaskEvent({ type: 'task_deleted', taskId: req.params.id, payload: { userId: task.userId } })
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
    const precheck = getTaskById(req.params.id)
    if (!precheck || !canAccessTask(precheck, req.user!)) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
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
    if (!task || !canAccessTask(task, req.user!)) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    res.json(listFeedbackForTask(req.params.id))
  })

  mutationRouter.post('/tasks/:id/cancel', (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task || !canAccessTask(task, req.user!)) {
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
    if (!task || !canAccessTask(task, req.user!)) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    const latest = getLatestStageRunForTask(req.params.id)
    if (!latest || latest.stage !== task.currentStage || latest.status !== 'failed') {
      res.status(409).json({ error: 'Task has no failed stage run to retry on its current stage' })
      return
    }
    const { additionalPrompt } = req.body ?? {}
    appendAudit({
      taskId: task.id,
      actor: 'user',
      action: 'retry_requested',
      details: { stage: latest.stage, iteration: latest.iteration },
    })
    const run = await deps.orchestrator.progressTask(task.id, {
      userAdditionalPrompt: typeof additionalPrompt === 'string' && additionalPrompt.trim() ? additionalPrompt.trim() : undefined,
    })
    if (!run) {
      res.status(409).json({ error: 'Task could not progress (slot full, no handler, or terminal)' })
      return
    }
    const updated = getTaskById(task.id)
    broadcastEnrichedUpdate(task.id)
    res.json({ task: updated, stageRun: run })
  })

  // ─── Resume the latest failed stage from its prior session ───────────
  //
  // Like /retry, but instead of starting a fresh conversation we pass the
  // failed run's session_id to the orchestrator so the spawned claude
  // process is launched with `--resume <sessionId>`. Useful when the agent
  // got a long way in but hit a permission wall or a transient error —
  // the resumed conversation keeps its tool history intact.
  //
  // Only valid when the latest stage_run on the task's current stage is
  // `failed` AND has a session_id attached.
  mutationRouter.post('/tasks/:id/resume-stage', async (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task || !canAccessTask(task, req.user!)) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    const latest = getLatestStageRunForTask(req.params.id)
    if (!latest || latest.stage !== task.currentStage || latest.status !== 'failed') {
      res.status(409).json({ error: 'Task has no failed stage run to resume on its current stage' })
      return
    }
    if (!latest.sessionId) {
      res.status(409).json({ error: 'Failed stage run has no session_id to resume from' })
      return
    }
    const { additionalPrompt: resumeAdditionalPrompt } = req.body ?? {}
    appendAudit({
      taskId: task.id,
      actor: 'user',
      action: 'resume_stage_requested',
      details: {
        stage: latest.stage,
        iteration: latest.iteration,
        sessionId: latest.sessionId,
      },
    })
    const run = await deps.orchestrator.progressTask(task.id, {
      resumeSessionId: latest.sessionId,
      userAdditionalPrompt: typeof resumeAdditionalPrompt === 'string' && resumeAdditionalPrompt.trim() ? resumeAdditionalPrompt.trim() : undefined,
    })
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
  //
  // Per-task dedup: the route maintains an in-memory `taskId → pid` map so
  // repeated clicks of the dashboard's "Analyze" button do not spawn
  // multiple analysis agents for the same task (each one would burn tokens
  // independently, mirror the same failure context, and confuse the agent-
  // monitoring view's PID→task linking). The map is purged when the
  // tracked PID exits — both via the child's `exit` event and lazily on
  // every new request via `isPidAlive`. State is process-local; a
  // dashboard restart loses the map but each spawn is detached, so the
  // agent itself is unaffected and a one-off duplicate after restart is
  // acceptable.
  mutationRouter.post('/tasks/:id/analyze', async (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task || !canAccessTask(task, req.user!)) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    const latest = getLatestStageRunForTask(req.params.id)
    if (!latest) {
      res.status(409).json({ error: 'Task has no stage runs to analyze' })
      return
    }

    // Lazy purge of dead entries (covers crashes / SIGKILLs that bypass the
    // `exit` listener) and conflict-check against any live analysis agent.
    const existingPid = activeAnalysisTasks.get(task.id)
    if (existingPid !== undefined) {
      if (isPidAlive(existingPid)) {
        res.status(409).json({
          error: 'Analysis session already running for this task',
          pid: existingPid,
        })
        return
      }
      activeAnalysisTasks.delete(task.id)
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
      activeAnalysisTasks.set(task.id, result.pid)
      // Best-effort cleanup so a fast-exiting analysis agent frees its
      // dedup slot without waiting for the next click's lazy purge.
      result.child.once('exit', () => {
        if (activeAnalysisTasks.get(task.id) === result.pid)
          activeAnalysisTasks.delete(task.id)
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
      consola.error('[task] analyze failed:', err)
      res.status(500).json({ error: 'Internal error' })
    }
  })

  // ─── Task stage runs & audit ────────────────

  router.get('/tasks/:id/stage-runs', (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task || !canAccessTask(task, req.user!)) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    res.json(listStageRunsForTask(req.params.id))
  })

  router.get('/tasks/:id/cost-breakdown', (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task || !canAccessTask(task, req.user!)) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    const breakdown = listStageRunsForTask(req.params.id)
      .filter(r => r.status === 'done')
      .map(r => ({
        stage: r.stage,
        iteration: r.iteration,
        costCents: r.costCents,
        tokensUsed: r.tokensUsed,
        startedAt: r.startedAt,
        endedAt: r.endedAt,
      }))
    res.json(breakdown)
  })

  router.get('/tasks/:id/stage-runs/:runId/agent-output', async (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task || !canAccessTask(task, req.user!)) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    const run = getStageRunById(req.params.runId)
    if (!run || run.taskId !== req.params.id) {
      res.status(404).json({ error: 'Stage run not found' })
      return
    }
    const cwd = task.worktreePath || task.cwd
    const sessionId = run.sessionId ?? await findNewestSessionId(cwd, run.startedAt)
    if (!sessionId) {
      res.json({ text: null })
      return
    }
    const { rawText } = await readLastStageJsonOutput(cwd, sessionId)
    res.json({ text: rawText ?? null })
  })

  router.get('/tasks/:id/audit', (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task || !canAccessTask(task, req.user!)) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    res.json(listAuditForTask(req.params.id))
  })

  // ─── Permissions ────────────────

  router.get('/tasks/:id/permissions', (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task || !canAccessTask(task, req.user!)) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
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
    const task = getTaskById(req.params.id)
    if (!task || !canAccessTask(task, req.user!)) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    const runs = listStageRunsForTask(req.params.id)
    const pendingRequests = runs.flatMap(r => listPendingPermissionRequests(r.id))
    const reRequestCounts = getPermissionReRequestCounts(task.id)
    const enrichedRequests = pendingRequests.map(pr => ({
      ...pr,
      reRequestCount: reRequestCounts.get(`${pr.tool}:${pr.pattern ?? '*'}`) ?? 1,
    }))
    res.json(enrichedRequests)
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
    const task = getTaskById(run.taskId)
    if (!task || !canAccessTask(task, req.user!)) {
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

    // Flip the stage_run to awaiting_user so the orchestrator's tick loop
    // stops counting elapsed time toward the stage timeout (see
    // hasPendingPerms guard in orchestrator.finalizeCompletedAsyncRuns).
    // The agent process stays alive and idle on the channel waiting for
    // the user's decision; without this, a slow user response would race
    // the timeout and the agent would be SIGTERM'd mid-question.
    if (run.status === 'running')
      updateStageRun(run.id, { status: 'awaiting_user' })
    broadcastEnrichedUpdate(run.taskId)

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

  /**
   * Bulk-grant permissions to a task post-creation. Mirrors what create_task
   * accepts: explicit `permissions[]` and/or `template`. Both can be combined.
   * Available retroactively for tasks already in flight.
   */
  mutationRouter.post('/tasks/:id/permissions/bulk', (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task || !canAccessTask(task, req.user!)) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    const { permissions, template } = req.body ?? {}
    if (template !== undefined && template !== null && (typeof template !== 'string' || !listTemplateNames().includes(template as never))) {
      res.status(400).json({ error: `template must be one of: ${listTemplateNames().join(', ')}` })
      return
    }
    if (permissions !== undefined && permissions !== null && !Array.isArray(permissions)) {
      res.status(400).json({ error: 'permissions must be an array' })
      return
    }

    const result: { templateGranted?: number, templateSkipped?: number, explicitGranted?: number, explicitSkipped?: number } = {}

    if (typeof template === 'string') {
      const tplRes = applyPermissionTemplateByName(task.id, template)
      if (tplRes) {
        result.templateGranted = tplRes.granted.length
        result.templateSkipped = tplRes.skipped.length
      }
    }
    if (Array.isArray(permissions) && permissions.length > 0) {
      for (const p of permissions) {
        if (typeof p !== 'object' || p === null || typeof (p as { tool?: unknown }).tool !== 'string') {
          res.status(400).json({ error: 'permissions[i].tool is required (string)' })
          return
        }
      }
      const explicitRes = bulkGrantPermissions(
        task.id,
        permissions.map((p: { tool: string, pattern?: string | null, expiresAt?: string | null }) => ({
          tool: p.tool,
          pattern: p.pattern ?? null,
          expiresAt: p.expiresAt ?? null,
        })),
        { source: 'rest_bulk_grant' },
      )
      result.explicitGranted = explicitRes.granted.length
      result.explicitSkipped = explicitRes.skipped.length
    }

    deps.broadcastTaskEvent({ type: 'task_updated', taskId: task.id, payload: enrichTask(task) })
    res.status(200).json(result)
  })

  /**
   * Bulk variant of POST /permission-requests. Accepts an array of
   * {tool, pattern?, reason?} entries. For each entry:
   *   - If a granted, non-expired task_permission already covers it,
   *     auto-resolve silently (no UI prompt).
   *   - Else, create a permission_request row and surface as ON HOLD.
   *
   * Response shape:
   *   {
   *     autoResolved: Array<{tool, pattern}>,
   *     pending:      Array<{id, tool, pattern}>
   *   }
   *
   * Stage status is flipped to awaiting_user only if at least one entry is
   * pending — if all entries auto-resolve, the agent keeps running.
   */
  mutationRouter.post('/permission-requests/bulk', (req, res) => {
    const { stageRunId, entries } = req.body ?? {}
    if (!stageRunId || typeof stageRunId !== 'string') {
      res.status(400).json({ error: 'stageRunId is required' })
      return
    }
    if (!Array.isArray(entries) || entries.length === 0) {
      res.status(400).json({ error: 'entries must be a non-empty array' })
      return
    }
    const run = getStageRunById(stageRunId)
    if (!run) {
      res.status(404).json({ error: 'stage run not found' })
      return
    }
    const task = getTaskById(run.taskId)
    if (!task || !canAccessTask(task, req.user!)) {
      res.status(404).json({ error: 'stage run not found' })
      return
    }

    const grants = listTaskPermissions(task.id)
    const nowIso = new Date().toISOString()
    const isCovered = (tool: string, pattern: string | null): boolean => {
      return grants.some((p) => {
        if (!p.granted)
          return false
        if (p.expiresAt && p.expiresAt <= nowIso)
          return false
        if (p.tool !== tool)
          return false
        // Coverage rule: a "tool only" grant (pattern === null) covers
        // every pattern of that tool. A specific-pattern grant covers only
        // exact-string-equal pattern matches (no glob expansion — Claude
        // Code semantics handle the matching at runtime).
        if (p.pattern === null)
          return true
        return (p.pattern ?? null) === pattern
      })
    }

    // B3: count permission_requests already on this stage_run BEFORE we add
    // the new ones. This becomes the cycleCount returned to the agent — when
    // > 0 the agent has been here before and is asked to forward-scan
    // everything still missing instead of trickling.
    const cycleCount = countPermissionRequestsForStageRun(stageRunId)

    const autoResolved: Array<{ tool: string, pattern: string | null }> = []
    const pending: Array<{ id: string, tool: string, pattern: string | null }> = []

    for (const raw of entries) {
      if (typeof raw !== 'object' || raw === null)
        continue
      const e = raw as { tool?: unknown, pattern?: unknown, reason?: unknown }
      const tool = typeof e.tool === 'string' ? e.tool.trim() : ''
      const pattern = typeof e.pattern === 'string' && e.pattern.trim().length > 0 ? e.pattern.trim() : null
      const reason = typeof e.reason === 'string' ? e.reason : null
      if (!tool)
        continue

      if (isCovered(tool, pattern)) {
        autoResolved.push({ tool, pattern })
        appendAudit({
          taskId: task.id,
          actor: 'system',
          action: 'permission_auto_resolved',
          details: { tool, pattern, source: 'bulk_request' },
        })
        continue
      }

      const reqRow = createPermissionRequest({
        stageRunId,
        tool,
        pattern,
        reason,
      })
      pending.push({ id: reqRow.id, tool, pattern })
      deps.broadcastTaskEvent({ type: 'permission_request', taskId: run.taskId, payload: reqRow })

      if (deps.dispatcher) {
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

    if (pending.length > 0 && run.status === 'running')
      updateStageRun(run.id, { status: 'awaiting_user' })

    broadcastEnrichedUpdate(run.taskId)

    // B3: when this is the 2nd+ bulk on the same stage_run AND new entries
    // had to be created, emit a forward-scan warning. The MCP channel
    // surfaces this in the agent's tool response so the agent treats the
    // pause as a signal to broaden, not retry-by-trickle.
    const loopWarning = (cycleCount > 0 && pending.length > 0)
      ? `Re-request loop detected: this is permission cycle #${cycleCount + 1} on this stage_run. Forward-scan ALL remaining work and request_permission ONCE with everything still missing — every kill/restart costs the user real time.`
      : null

    if (loopWarning) {
      appendAudit({
        taskId: task.id,
        actor: 'system',
        action: 'permission_loop_detected',
        details: {
          stageRunId,
          cycleCount: cycleCount + 1,
          newPending: pending.length,
        },
      })
    }

    res.status(200).json({ autoResolved, pending, cycleCount, loopWarning })
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
    const run = getStageRunById(existing.stageRunId)
    if (!run) {
      res.status(404).json({ error: 'request not found' })
      return
    }
    const task = getTaskById(run.taskId)
    if (!task || !canAccessTask(task, req.user!)) {
      res.status(404).json({ error: 'request not found' })
      return
    }
    // Kill the idle stage agent BEFORE the DB transaction — process signaling
    // is not a DB operation and must stay outside the atomic write block.
    const shouldRestartRun = outcome === 'granted' && run.status === 'awaiting_user'
    if (shouldRestartRun && run.pid !== null && run.pid > 1) {
      try {
        process.kill(run.pid, 'SIGTERM')
      }
      catch { /* already dead */ }
    }

    let resolved: ReturnType<typeof resolvePermissionRequest> | null = null
    const db = getDb()
    db.transaction(() => {
      resolved = resolvePermissionRequest(req.params.id, outcome)
      // Stamp `last_grant_at` on the run so `sweepAwaitingUserRuns` resets
      // its wallclock budget. Applies to both granted AND denied outcomes
      // because either is user activity that proves the agent is not
      // busy-waiting in a polling loop. Skipped only when the run already
      // moved past awaiting_user (race with kill+restart cascade — the new
      // run will get its own stamp on the next resolution).
      if (run.status === 'awaiting_user') {
        updateStageRun(run.id, { lastGrantAt: new Date().toISOString() })
      }
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

        // Resume-after-grant pattern: when the run was paused at awaiting_user
        // for this permission, the spawned claude process cannot pick the
        // newly-granted permission up mid-conversation — its
        // .claude/settings.json was written at spawn time. The SIGTERM above
        // killed the idle process; here we mark the stage_run failed so the
        // re-spawn (below, after the transaction) can pick up with --resume
        // pointing at the same session and the just-granted permission now
        // in settings.json. If session_id is null (not yet attached), falls
        // back to a fresh spawn.
        if (shouldRestartRun) {
          updateStageRun(run.id, {
            status: 'failed',
            output: { error: 'restarting after permission grant' },
            endedAt: new Date().toISOString(),
          })
          appendAudit({
            taskId: run.taskId,
            actor: 'user',
            action: 'permission_granted_restart',
            details: {
              permissionRequestId: existing.id,
              tool: existing.tool,
              pattern: existing.pattern,
              stageRunId: run.id,
            },
          })
        }
      }
    })()

    if (outcome === 'granted') {
      if (shouldRestartRun) {
        // Count cycles BEFORE building the note so the message references
        // the current attempt number (the resolved row counts toward total).
        const cycleCount = countPermissionRequestsForStageRun(run.id)
        const handoffNote = buildPermissionGrantHandoffNote({
          tool: existing.tool,
          pattern: existing.pattern,
          cycleCount,
        })
        await deps.orchestrator.progressTask(run.taskId, {
          resumeSessionId: run.sessionId ?? undefined,
          userAdditionalPrompt: handoffNote,
        })
      }
      else {
        // Normal resume path — agent never paused (race) or already finished.
        await deps.orchestrator.resumeFromUser(run.taskId)
      }
    }
    else {
      // outcome === 'denied' — leave the run in awaiting_user; the agent
      // will see the denial via channel and either replan or give up.
      await deps.orchestrator.resumeFromUser(run.taskId)
    }
    broadcastEnrichedUpdate(run.taskId)
    res.json(resolved)
  })

  /**
   * Bulk-resolve every pending permission_request on a single stage_run with
   * the same outcome (granted | denied). Collapses the user-grants-many-
   * permissions cascade into ONE transaction, ONE SIGTERM, ONE progressTask
   * invocation:
   *
   *   - all permission_request rows resolved in a single DB transaction
   *   - on grant: each entry persisted as a task_permission row in the same
   *     transaction so the next spawned agent's settings.json picks them up
   *   - the awaiting_user agent is SIGTERMed exactly once (if applicable)
   *   - the run is marked failed exactly once with a "restarting after bulk
   *     permission grant" output stamp
   *   - exactly one progressTask call with a combined handoff note listing
   *     every grant
   *
   * Body: { stageRunId: string, outcome: 'granted' | 'denied' }
   * Response: { resolved: number, granted: number, denied: number,
   *             grantedTools: Array<{tool, pattern}> }
   */
  mutationRouter.post('/permission-requests/bulk-resolve', async (req, res) => {
    const { stageRunId, outcome } = req.body ?? {}
    if (!stageRunId || typeof stageRunId !== 'string') {
      res.status(400).json({ error: 'stageRunId is required' })
      return
    }
    if (outcome !== 'granted' && outcome !== 'denied') {
      res.status(400).json({ error: 'outcome must be granted|denied' })
      return
    }
    const run = getStageRunById(stageRunId)
    if (!run) {
      res.status(404).json({ error: 'stage run not found' })
      return
    }
    const task = getTaskById(run.taskId)
    if (!task || !canAccessTask(task, req.user!)) {
      res.status(404).json({ error: 'stage run not found' })
      return
    }

    const pending = listPendingPermissionRequests(stageRunId)
    if (pending.length === 0) {
      res.json({ resolved: 0, granted: 0, denied: 0, grantedTools: [] })
      return
    }

    // Kill the idle stage agent BEFORE the DB transaction — process signaling
    // is not a DB operation and must stay outside the atomic write block.
    const shouldRestartRun = outcome === 'granted' && run.status === 'awaiting_user'
    if (shouldRestartRun && run.pid !== null && run.pid > 1) {
      try {
        process.kill(run.pid, 'SIGTERM')
      }
      catch { /* already dead */ }
    }

    const grantedTools: Array<{ tool: string, pattern: string | null }> = []
    const nowIso = new Date().toISOString()
    const db = getDb()
    db.transaction(() => {
      for (const r of pending) {
        resolvePermissionRequest(r.id, outcome)
        if (outcome === 'granted') {
          createTaskPermission({
            taskId: run.taskId,
            tool: r.tool,
            pattern: r.pattern,
            granted: true,
            preApproved: false,
            decidedBy: 'user',
          })
          grantedTools.push({ tool: r.tool, pattern: r.pattern })
        }
      }
      // Stamp last_grant_at so sweepAwaitingUserRuns resets its wallclock budget.
      // Same rationale as the single-resolve handler: any user activity is proof
      // the agent is not busy-waiting in a polling loop.
      if (run.status === 'awaiting_user') {
        updateStageRun(run.id, { lastGrantAt: nowIso })
      }
      if (shouldRestartRun) {
        updateStageRun(run.id, {
          status: 'failed',
          output: { error: 'restarting after bulk permission grant' },
          endedAt: nowIso,
        })
        appendAudit({
          taskId: run.taskId,
          actor: 'user',
          action: 'permission_bulk_granted_restart',
          details: {
            stageRunId: run.id,
            count: pending.length,
            tools: grantedTools,
          },
        })
      }
      else {
        appendAudit({
          taskId: run.taskId,
          actor: 'user',
          action: outcome === 'granted' ? 'permission_bulk_granted' : 'permission_bulk_denied',
          details: {
            stageRunId: run.id,
            count: pending.length,
            tools: outcome === 'granted'
              ? grantedTools
              : pending.map(p => ({ tool: p.tool, pattern: p.pattern })),
          },
        })
      }
    })()

    if (outcome === 'granted') {
      if (shouldRestartRun) {
        const cycleCount = countPermissionRequestsForStageRun(run.id)
        const handoffNote = buildBulkPermissionGrantHandoffNote({
          grantedTools,
          cycleCount,
        })
        await deps.orchestrator.progressTask(run.taskId, {
          resumeSessionId: run.sessionId ?? undefined,
          userAdditionalPrompt: handoffNote,
        })
      }
      else {
        await deps.orchestrator.resumeFromUser(run.taskId)
      }
    }
    else {
      // outcome === 'denied' — leave the run in awaiting_user; the agent
      // will see the denials via channel and either replan or give up.
      await deps.orchestrator.resumeFromUser(run.taskId)
    }

    broadcastEnrichedUpdate(run.taskId)
    res.json({
      resolved: pending.length,
      granted: outcome === 'granted' ? pending.length : 0,
      denied: outcome === 'denied' ? pending.length : 0,
      grantedTools,
    })
  })

  // ─── Task dependencies ────────────────

  // GET /tasks/:id/dependencies — list all prerequisites for a task
  router.get('/tasks/:id/dependencies', (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task || !canAccessTask(task, req.user!)) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    res.json(getDependenciesFor(req.params.id))
  })

  // GET /tasks/:id/dependents — list all tasks waiting on this task
  router.get('/tasks/:id/dependents', (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task || !canAccessTask(task, req.user!)) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    res.json(getDependentsOf(req.params.id))
  })

  // POST /tasks/:id/dependencies — add a dependency
  mutationRouter.post('/tasks/:id/dependencies', (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task || !canAccessTask(task, req.user!)) {
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
      consola.error('[taskRoutes] addDependency failed:', err)
      res.status(500).json({ error: 'Internal error' })
    }
  })

  // DELETE /tasks/:id/dependencies/:depId — remove a dependency by its row ID
  mutationRouter.delete('/tasks/:id/dependencies/:depId', (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task || !canAccessTask(task, req.user!)) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
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
      deps.orchestrator.invalidateConfigCache()
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

  // ─── Global Audit ────────────────────────────────────────────────────────────

  router.get('/audit', (req, res) => {
    if (!req.user!.isAdmin) {
      res.status(403).json({ error: 'Forbidden' })
      return
    }
    const limit = Math.min(Number(req.query.limit) || 100, 500)
    const offset = Number(req.query.offset) || 0
    const rows = getDb()
      .prepare('SELECT * FROM audit_log ORDER BY timestamp DESC LIMIT ? OFFSET ?')
      .all(limit, offset) as AuditRow[]
    res.json(rows.map(rowToAuditEntry))
  })

  // ─── Webhook HMAC ────────────────────────────────────────────────────────────

  router.get('/settings/webhook-hmac', (req, res) => {
    if (!req.user!.isAdmin) {
      res.status(403).json({ error: 'Forbidden' })
      return
    }
    const enabled = getConfig('webhook_hmac_enabled') === 'true'
    const hasSecret = !!getConfig('webhook_hmac_secret')
    res.json({ enabled, hasSecret })
  })

  mutationRouter.post('/settings/webhook-hmac', (req, res) => {
    if (!req.user!.isAdmin) {
      res.status(403).json({ error: 'Forbidden' })
      return
    }
    const { enabled, secret } = req.body as { enabled: boolean, secret?: string }
    if (typeof enabled !== 'boolean') {
      res.status(400).json({ error: '`enabled` must be a boolean' })
      return
    }
    setConfig('webhook_hmac_enabled', enabled ? 'true' : 'false')
    if (enabled) {
      const resolvedSecret = secret && secret.length >= 32
        ? secret
        : randomBytes(32).toString('hex')
      setConfig('webhook_hmac_secret', resolvedSecret)
      res.json({ enabled: true, secret: resolvedSecret })
    }
    else {
      res.json({ enabled: false })
    }
  })

  // ─── Git status & actions for task cwd ────────────────

  router.get('/tasks/:id/git-status', async (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task || !canAccessTask(task, req.user!)) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    const cwd = task.worktreePath || task.cwd
    try {
      const status = await getGitStatus(cwd)
      res.json(status)
    }
    catch {
      res.status(500).json({ error: 'Git operation failed' })
    }
  })

  mutationRouter.post('/tasks/:id/git-action', async (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task || !canAccessTask(task, req.user!)) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    const { action } = req.body as { action: unknown }
    if (action !== 'fetch' && action !== 'pull') {
      res.status(400).json({ error: 'Invalid action' })
      return
    }
    if (action === 'pull' && process.env.DASHBOARD_ALLOW_GIT_PULL !== 'true') {
      res.status(403).json({ error: 'pull is disabled. Set DASHBOARD_ALLOW_GIT_PULL=true to enable.' })
      return
    }
    const cwd = task.worktreePath || task.cwd
    try {
      const output = await runGitAction(cwd, action)
      res.json({ output })
    }
    catch (err) {
      res.status(500).json({ error: String(err) })
    }
  })

  // Mount mutation sub-router so its rejectCrossOrigin middleware guards
  // every POST/PUT/PATCH/DELETE route registered above.
  router.use(mutationRouter)

  return router
}
