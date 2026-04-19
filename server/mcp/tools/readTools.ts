import { z } from 'zod'
import type { PipelineStage } from '../../../src/types.js'
import { VALID_STAGES } from '../../constants.js'
import { listAuditForTask } from '../../db/auditRepo.js'
import { listPendingPermissionRequests } from '../../db/permissionsRepo.js'
import { listStageRunsForTask } from '../../db/stageRunsRepo.js'
import { getTaskById, getTaskBySlug, listTasks, listTasksByStage } from '../../db/tasksRepo.js'
import { mcpError, ok } from '../mcpAuth.js'
import type { makeToolRegistrar } from '../mcpAuth.js'

type ToolFn = ReturnType<typeof makeToolRegistrar>

export function registerReadTools(tool: ToolFn): void {
  tool(
    'list_tasks',
    { stage: z.string().optional().describe('Filter by pipeline stage') },
    async ({ stage }) => {
      if (stage && !VALID_STAGES.has(stage as PipelineStage))
        mcpError(`Invalid stage: ${stage}`)
      const tasks = stage ? listTasksByStage(stage as PipelineStage) : listTasks()
      return ok(tasks)
    },
  )

  tool(
    'get_task',
    { id_or_slug: z.string().describe('Task UUID or slug') },
    async ({ id_or_slug }) => {
      const task = getTaskById(id_or_slug) ?? getTaskBySlug(id_or_slug)
      if (!task)
        mcpError(`Task not found: ${id_or_slug}`)
      return ok(task)
    },
  )

  tool(
    'list_stage_runs',
    { task_id: z.string() },
    async ({ task_id }) => {
      if (!getTaskById(task_id))
        mcpError(`Task not found: ${task_id}`)
      return ok(listStageRunsForTask(task_id))
    },
  )

  tool(
    'list_audit',
    { task_id: z.string() },
    async ({ task_id }) => {
      if (!getTaskById(task_id))
        mcpError(`Task not found: ${task_id}`)
      return ok(listAuditForTask(task_id))
    },
  )

  tool(
    'list_permission_requests',
    { task_id: z.string() },
    async ({ task_id }) => {
      if (!getTaskById(task_id))
        mcpError(`Task not found: ${task_id}`)
      const runs = listStageRunsForTask(task_id)
      const requests = runs.flatMap(r => listPendingPermissionRequests(r.id))
      return ok(requests)
    },
  )
}
