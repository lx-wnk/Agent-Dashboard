import type { PipelineStage } from '../../../src/types.js'
import type { PipelineOrchestrator } from '../../pipeline/orchestrator.js'
import type { makeToolRegistrar } from '../mcpAuth.js'
import { z } from 'zod'
import { appendAudit } from '../../db/auditRepo.js'
import { getDb } from '../../db/client.js'
import { createFeedback } from '../../db/feedbackRepo.js'
import { createTaskPermission, getPermissionRequestById, resolvePermissionRequest } from '../../db/permissionsRepo.js'
import { getLatestStageRunForTask, getStageRunById, listStageRunsForTask } from '../../db/stageRunsRepo.js'
import { getTaskById, updateTask } from '../../db/tasksRepo.js'
import { ALLOWED_TOOLS, bulkGrantKonzeptPermissions } from '../../services/approvalUtils.js'
import { mcpError, ok } from '../mcpAuth.js'

type ToolFn = ReturnType<typeof makeToolRegistrar>

export function registerControlTools(
  tool: ToolFn,
  orchestrator: PipelineOrchestrator,
  broadcast: (taskId: string) => void,
): void {
  tool(
    'progress_task',
    { id: z.string() },
    async ({ id }) => {
      const stageRun = await orchestrator.progressTask(id)
      if (!stageRun)
        mcpError('Task cannot progress (terminal, not found, or no free runner slot)')
      broadcast(id)
      const task = getTaskById(id)
      return ok({ task, stageRun })
    },
  )

  tool(
    'approve_task',
    { id: z.string() },
    async ({ id }) => {
      const task = getTaskById(id)
      if (!task)
        mcpError(`Task not found: ${id}`)
      const nextMap: Partial<Record<PipelineStage, PipelineStage>> = {
        approval1: 'umsetzungskonzept',
        approval2: 'umsetzung',
      }
      const next = nextMap[task.currentStage]
      if (!next)
        mcpError(`Task in stage ${task.currentStage} cannot be approved`)

      getDb().transaction(() => {
        // approval2: bulk-grant tool permissions declared in umsetzungskonzept output
        if (task.currentStage === 'approval2')
          bulkGrantKonzeptPermissions(task.id)
        updateTask(id, { currentStage: next })
        appendAudit({ taskId: id, actor: 'user', action: 'approved', details: { from: task.currentStage, to: next } })
      })()
      broadcast(id)
      // Returns plain task row; SSE broadcast is separately enriched via index.ts
      return ok({ task: getTaskById(id) })
    },
  )

  tool(
    'request_changes',
    { id: z.string(), feedback: z.string().min(1).max(4000) },
    async ({ id, feedback }) => {
      const task = getTaskById(id)
      if (!task)
        mcpError(`Task not found: ${id}`)
      const stageMap: Partial<Record<PipelineStage, 'planning' | 'umsetzungskonzept'>> = {
        approval1: 'planning',
        approval2: 'umsetzungskonzept',
      }
      const regressionStage = stageMap[task.currentStage]
      if (!regressionStage)
        mcpError(`Task in stage ${task.currentStage} cannot receive change requests`)

      const priorRun = listStageRunsForTask(task.id)
        .filter(r => r.stage === regressionStage && r.status === 'done')
        .at(-1) ?? null

      const feedbackRow = createFeedback({
        taskId: task.id,
        stage: regressionStage,
        stageRunId: priorRun?.id ?? null,
        feedback,
      })
      getDb().transaction(() => {
        updateTask(id, { currentStage: regressionStage })
        appendAudit({
          taskId: task.id,
          actor: 'user',
          action: 'request_changes',
          details: { fromStage: task.currentStage, toStage: regressionStage, feedbackId: feedbackRow.id, iteration: feedbackRow.iteration },
        })
      })()
      broadcast(id)
      return ok({ task: getTaskById(id) })
    },
  )

  tool(
    'cancel_task',
    { id: z.string() },
    async ({ id }) => {
      const task = getTaskById(id)
      if (!task)
        mcpError(`Task not found: ${id}`)
      if (task.currentStage === 'done' || task.currentStage === 'cancelled')
        mcpError(`Task is already ${task.currentStage}`)
      getDb().transaction(() => {
        updateTask(id, { currentStage: 'cancelled' })
        appendAudit({ taskId: id, actor: 'user', action: 'cancelled' })
      })()
      broadcast(id)
      // Returns plain task row; SSE broadcast is separately enriched via index.ts
      return ok({ task: getTaskById(id) })
    },
  )

  tool(
    'retry_task',
    { id: z.string() },
    async ({ id }) => {
      const task = getTaskById(id)
      if (!task)
        mcpError(`Task not found: ${id}`)
      const latest = getLatestStageRunForTask(id)
      if (!latest || latest.stage !== task.currentStage || latest.status !== 'failed')
        mcpError('Task has no failed stage run to retry on its current stage')
      appendAudit({
        taskId: id,
        actor: 'user',
        action: 'retry_requested',
        details: { stage: latest.stage, iteration: latest.iteration },
      })
      const stageRun = await orchestrator.progressTask(id)
      if (!stageRun)
        mcpError('Task could not progress (slot full, no handler, or terminal)')
      broadcast(id)
      return ok({ task: getTaskById(id), stageRun })
    },
  )

  tool(
    'grant_permission',
    {
      task_id: z.string(),
      tool: z.string().refine(t => ALLOWED_TOOLS.has(t), {
        message: `tool must be one of: ${[...ALLOWED_TOOLS].sort().join(', ')}`,
      }),
      pattern: z.string().max(256).optional(),
    },
    async ({ task_id, tool, pattern }) => {
      if (!getTaskById(task_id))
        mcpError(`Task not found: ${task_id}`)
      const perm = createTaskPermission({
        taskId: task_id,
        tool,
        pattern: pattern ?? null,
        granted: true,
        preApproved: true,
        decidedBy: 'user',
      })
      return ok(perm)
    },
  )

  tool(
    'resolve_permission_request',
    { request_id: z.string(), outcome: z.enum(['granted', 'denied']) },
    async ({ request_id, outcome }) => {
      const req = getPermissionRequestById(request_id)
      if (!req)
        mcpError(`Permission request not found: ${request_id}`)
      const resolved = resolvePermissionRequest(request_id, outcome)
      const run = getStageRunById(req.stageRunId)
      if (run) {
        if (outcome === 'granted') {
          createTaskPermission({
            taskId: run.taskId,
            tool: req.tool,
            pattern: req.pattern ?? null,
            granted: true,
            preApproved: false,
          })
        }
        await orchestrator.resumeFromUser(run.taskId)
        broadcast(run.taskId)
        return ok({ ...resolved, resumed: true })
      }
      // Stage run not found — permission recorded but agent cannot be signalled
      if (outcome === 'granted') {
        console.warn(`[controlTools] resolve_permission_request: stage run ${req.stageRunId} not found; agent not resumed`)
      }
      return ok({ ...resolved, resumed: false, warning: 'Stage run not found; agent not signalled' })
    },
  )
}
