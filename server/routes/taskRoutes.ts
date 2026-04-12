import type express from 'express'
import type { NotificationEventType, PipelineStage } from '../../src/types.js'
import type { PipelineOrchestrator } from '../pipeline/orchestrator.js'
import { Router } from 'express'
import { listAuditForTask } from '../db/auditRepo.js'
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
import { getStageRunById, listStageRunsForTask } from '../db/stageRunsRepo.js'
import {
  createTask,
  deleteTask,
  getTaskById,
  getTaskBySlug,
  listTasks,
  listTasksByStage,
  updateTask,
} from '../db/tasksRepo.js'

type RejectCrossOrigin = (req: express.Request, res: express.Response) => boolean

export interface TaskRouterDeps {
  rejectCrossOrigin: RejectCrossOrigin
  orchestrator: PipelineOrchestrator
  broadcastTaskEvent: (event: TaskEvent) => void
}

export interface TaskEvent {
  type: 'task_created' | 'task_updated' | 'task_deleted' | 'stage_run_updated' | 'permission_request'
  taskId: string
  payload?: unknown
}

const VALID_STAGES = new Set<PipelineStage>([
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
  'done',
  'on_hold',
  'cancelled',
  'failed',
])

const VALID_EVENT_TYPES = new Set<NotificationEventType>([
  'on_hold',
  'approval_needed',
  'completed',
  'failed',
  'budget_exceeded',
  'iteration_warning',
])

const SLUG_RE = /^[a-z0-9][a-z0-9-]{0,63}$/

export function createTaskRouter(deps: TaskRouterDeps): Router {
  const router = Router()

  // ─── Tasks CRUD ────────────────

  router.get('/tasks', (req, res) => {
    const stage = req.query.stage as string | undefined
    if (stage) {
      if (!VALID_STAGES.has(stage as PipelineStage)) {
        res.status(400).json({ error: 'Invalid stage' })
        return
      }
      res.json(listTasksByStage(stage as PipelineStage))
      return
    }
    res.json(listTasks())
  })

  router.get('/tasks/:id', (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    res.json(task)
  })

  router.post('/tasks', (req, res) => {
    if (deps.rejectCrossOrigin(req, res))
      return

    const { slug, title, description, cwd, worktreePath, sourceBranch, targetBranch, parentTaskId, maxIterations, tokenBudget, costBudgetCents, stageTimeoutSeconds, metadata } = req.body ?? {}

    if (!slug || typeof slug !== 'string' || !SLUG_RE.test(slug)) {
      res.status(400).json({ error: 'slug must match [a-z0-9][a-z0-9-]{0,63}' })
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

    try {
      const task = createTask({
        slug,
        title,
        description: typeof description === 'string' ? description : null,
        cwd,
        worktreePath: typeof worktreePath === 'string' ? worktreePath : null,
        sourceBranch: typeof sourceBranch === 'string' ? sourceBranch : null,
        targetBranch: typeof targetBranch === 'string' ? targetBranch : null,
        parentTaskId: typeof parentTaskId === 'string' ? parentTaskId : null,
        maxIterations: typeof maxIterations === 'number' ? maxIterations : undefined,
        tokenBudget: typeof tokenBudget === 'number' ? tokenBudget : null,
        costBudgetCents: typeof costBudgetCents === 'number' ? costBudgetCents : null,
        stageTimeoutSeconds: typeof stageTimeoutSeconds === 'number' ? stageTimeoutSeconds : undefined,
        metadata: typeof metadata === 'object' && metadata !== null ? metadata : null,
      })
      deps.broadcastTaskEvent({ type: 'task_created', taskId: task.id, payload: task })
      res.status(201).json(task)
    }
    catch (err) {
      res.status(500).json({ error: (err as Error).message })
    }
  })

  router.patch('/tasks/:id', (req, res) => {
    if (deps.rejectCrossOrigin(req, res))
      return
    const task = getTaskById(req.params.id)
    if (!task) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    const updated = updateTask(req.params.id, req.body ?? {})
    deps.broadcastTaskEvent({ type: 'task_updated', taskId: req.params.id, payload: updated })
    res.json(updated)
  })

  router.delete('/tasks/:id', (req, res) => {
    if (deps.rejectCrossOrigin(req, res))
      return
    const ok = deleteTask(req.params.id)
    if (!ok) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    deps.broadcastTaskEvent({ type: 'task_deleted', taskId: req.params.id })
    res.status(204).end()
  })

  // ─── Stage progression & approvals ────────────────

  router.post('/tasks/:id/progress', async (req, res) => {
    if (deps.rejectCrossOrigin(req, res))
      return
    const run = await deps.orchestrator.progressTask(req.params.id)
    if (!run) {
      res.status(409).json({ error: 'Task cannot progress (terminal, missing, or slot full)' })
      return
    }
    const task = getTaskById(req.params.id)
    deps.broadcastTaskEvent({ type: 'task_updated', taskId: req.params.id, payload: task })
    res.json({ task, stageRun: run })
  })

  router.post('/tasks/:id/approve', async (req, res) => {
    if (deps.rejectCrossOrigin(req, res))
      return
    const task = getTaskById(req.params.id)
    if (!task) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    // Approve just advances the task past an awaiting_user state.
    // Determine the next stage from current:
    const nextMap: Record<string, PipelineStage> = {
      approval1: 'umsetzungskonzept',
      approval2: 'umsetzung',
    }
    const next = nextMap[task.currentStage]
    if (!next) {
      res.status(409).json({ error: `Task in stage ${task.currentStage} cannot be approved` })
      return
    }
    updateTask(req.params.id, { currentStage: next })
    const updated = getTaskById(req.params.id)
    deps.broadcastTaskEvent({ type: 'task_updated', taskId: req.params.id, payload: updated })
    res.json(updated)
  })

  router.post('/tasks/:id/cancel', (req, res) => {
    if (deps.rejectCrossOrigin(req, res))
      return
    const task = getTaskById(req.params.id)
    if (!task) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    updateTask(req.params.id, { currentStage: 'cancelled' })
    const updated = getTaskById(req.params.id)
    deps.broadcastTaskEvent({ type: 'task_updated', taskId: req.params.id, payload: updated })
    res.json(updated)
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

  router.post('/tasks/:id/permissions', (req, res) => {
    if (deps.rejectCrossOrigin(req, res))
      return
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

  router.delete('/tasks/:id/permissions/:permId', (req, res) => {
    if (deps.rejectCrossOrigin(req, res))
      return
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

  router.post('/permission-requests', (req, res) => {
    if (deps.rejectCrossOrigin(req, res))
      return
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
    res.status(201).json(reqRow)
  })

  router.post('/permission-requests/:id/resolve', async (req, res) => {
    if (deps.rejectCrossOrigin(req, res))
      return
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
      const task = getTaskById(run.taskId)
      deps.broadcastTaskEvent({ type: 'task_updated', taskId: run.taskId, payload: task })
    }
    res.json(resolved)
  })

  // ─── Pipeline config (maxParallel, etc.) ────────────────

  router.get('/pipeline/config', (_req, res) => {
    res.json({
      maxParallelOrchestrators: getPipelineConfigNumber('maxParallelOrchestrators', 3),
    })
  })

  router.put('/pipeline/config', (req, res) => {
    if (deps.rejectCrossOrigin(req, res))
      return
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

  router.put('/notifications/preferences/:eventType', (req, res) => {
    if (deps.rejectCrossOrigin(req, res))
      return
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

  router.put('/notifications/config', (req, res) => {
    if (deps.rejectCrossOrigin(req, res))
      return
    const updates = req.body ?? {}
    for (const [key, value] of Object.entries(updates)) {
      if (typeof key !== 'string')
        continue
      setConfig(key, typeof value === 'string' ? value : value === null ? null : String(value))
    }
    res.json(getAllConfig())
  })

  return router
}
