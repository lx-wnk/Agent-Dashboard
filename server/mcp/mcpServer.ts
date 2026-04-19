import type { McpScope, PipelineStage } from '../../src/types.js'
import { VALID_STAGES } from '../../src/types.js'
import { ALLOWED_TOOLS, bulkGrantKonzeptPermissions } from '../services/approvalUtils.js'
import type { PipelineOrchestrator } from '../pipeline/orchestrator.js'
import { createHash, randomBytes } from 'node:crypto'
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { z } from 'zod'
import { createApiKey, getApiKeyById, listApiKeys, revokeApiKey } from '../db/apiKeysRepo.js'
import { appendAudit, listAuditForTask } from '../db/auditRepo.js'
import { createFeedback } from '../db/feedbackRepo.js'
import {
  createTaskPermission,
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
  createTask,
  deleteTask,
  getTaskById,
  getTaskBySlug,
  listTasks,
  listTasksByStage,
  updateTask,
} from '../db/tasksRepo.js'

function mcpError(message: string): never {
  const err = new Error(message) as Error & { code: number }
  err.code = -32003
  throw err
}

function requireScope(scopes: Set<McpScope>, needed: McpScope): void {
  if (!scopes.has(needed))
    mcpError(`Insufficient scope: requires ${needed}`)
}


const SLUG_RE = /^[a-z0-9][a-z0-9-]{0,63}$/

export function buildMcpServer(
  orchestrator: PipelineOrchestrator,
  scopes: Set<McpScope>,
  broadcast: (taskId: string) => void,
  broadcastDeleted: (taskId: string) => void,
): McpServer {
  const server = new McpServer({ name: 'dashboard-tasks', version: '1.0.0' })

  // ─── tasks:read ───────────────────────────────────────────────

  server.tool(
    'list_tasks',
    'List all pipeline tasks, optionally filtered by stage',
    { stage: z.string().optional().describe('Filter by pipeline stage') },
    async ({ stage }) => {
      requireScope(scopes, 'tasks:read')
      if (stage && !VALID_STAGES.has(stage as PipelineStage))
        mcpError(`Invalid stage: ${stage}`)
      const tasks = stage ? listTasksByStage(stage as PipelineStage) : listTasks()
      return { content: [{ type: 'text' as const, text: JSON.stringify(tasks) }] }
    },
  )

  server.tool(
    'get_task',
    'Get a single task by ID or slug',
    { id_or_slug: z.string().describe('Task UUID or slug') },
    async ({ id_or_slug }) => {
      requireScope(scopes, 'tasks:read')
      const task = getTaskById(id_or_slug) ?? getTaskBySlug(id_or_slug)
      if (!task)
        mcpError(`Task not found: ${id_or_slug}`)
      return { content: [{ type: 'text' as const, text: JSON.stringify(task) }] }
    },
  )

  server.tool(
    'list_stage_runs',
    'List all stage runs for a task',
    { task_id: z.string() },
    async ({ task_id }) => {
      requireScope(scopes, 'tasks:read')
      if (!getTaskById(task_id))
        mcpError(`Task not found: ${task_id}`)
      return { content: [{ type: 'text' as const, text: JSON.stringify(listStageRunsForTask(task_id)) }] }
    },
  )

  server.tool(
    'list_audit',
    'List audit log entries for a task',
    { task_id: z.string() },
    async ({ task_id }) => {
      requireScope(scopes, 'tasks:read')
      if (!getTaskById(task_id))
        mcpError(`Task not found: ${task_id}`)
      return { content: [{ type: 'text' as const, text: JSON.stringify(listAuditForTask(task_id)) }] }
    },
  )

  server.tool(
    'list_permission_requests',
    'List pending permission requests for a task across all stage runs',
    { task_id: z.string() },
    async ({ task_id }) => {
      requireScope(scopes, 'tasks:read')
      if (!getTaskById(task_id))
        mcpError(`Task not found: ${task_id}`)
      const runs = listStageRunsForTask(task_id)
      const requests = runs.flatMap(r => listPendingPermissionRequests(r.id))
      return { content: [{ type: 'text' as const, text: JSON.stringify(requests) }] }
    },
  )

  // ─── tasks:write ───────────────────────────────────────────────

  server.tool(
    'create_task',
    'Create a new pipeline task in the backlog',
    {
      slug: z.string().describe('Unique slug matching [a-z0-9][a-z0-9-]{0,63}'),
      title: z.string(),
      cwd: z.string().describe('Absolute working directory path'),
      description: z.string().optional(),
      priority: z.enum(['high', 'medium', 'low']).optional(),
      silverBullet: z.boolean().optional().describe('Jump-queue flag'),
      metadata: z.record(z.string(), z.unknown()).optional(),
      sourceBranch: z.string().optional(),
      targetBranch: z.string().optional(),
      maxIterations: z.number().int().positive().optional(),
      tokenBudget: z.number().int().positive().optional(),
      costBudgetCents: z.number().int().positive().optional(),
    },
    async (args) => {
      requireScope(scopes, 'tasks:write')
      if (!SLUG_RE.test(args.slug))
        mcpError('slug must match [a-z0-9][a-z0-9-]{0,63}')
      if (getTaskBySlug(args.slug))
        mcpError(`slug already exists: ${args.slug}`)
      const task = createTask({
        slug: args.slug,
        title: args.title,
        description: args.description ?? null,
        cwd: args.cwd,
        sourceBranch: args.sourceBranch ?? null,
        targetBranch: args.targetBranch ?? null,
        metadata: args.metadata ?? null,
        silverBullet: args.silverBullet ?? false,
        priority: args.priority,
        maxIterations: args.maxIterations,
        tokenBudget: args.tokenBudget ?? null,
        costBudgetCents: args.costBudgetCents ?? null,
      })
      broadcast(task.id)
      return { content: [{ type: 'text' as const, text: JSON.stringify(task) }] }
    },
  )

  server.tool(
    'update_task',
    'Update whitelisted fields of an existing task (cannot change currentStage directly)',
    {
      id: z.string(),
      title: z.string().optional(),
      description: z.string().nullable().optional(),
      priority: z.enum(['high', 'medium', 'low']).optional(),
      silverBullet: z.boolean().optional(),
      maxIterations: z.number().int().positive().optional(),
      tokenBudget: z.number().int().positive().nullable().optional(),
      costBudgetCents: z.number().int().positive().nullable().optional(),
      metadata: z.record(z.string(), z.unknown()).nullable().optional(),
    },
    async ({ id, ...fields }) => {
      requireScope(scopes, 'tasks:write')
      const task = updateTask(id, fields)
      if (!task)
        mcpError(`Task not found: ${id}`)
      broadcast(id)
      return { content: [{ type: 'text' as const, text: JSON.stringify(task) }] }
    },
  )

  server.tool(
    'delete_task',
    'Delete a task and its associated data',
    { id: z.string() },
    async ({ id }) => {
      requireScope(scopes, 'tasks:write')
      const ok = deleteTask(id)
      if (!ok)
        mcpError(`Task not found: ${id}`)
      broadcastDeleted(id)
      return { content: [{ type: 'text' as const, text: JSON.stringify({ success: true }) }] }
    },
  )

  // ─── pipeline:control ─────────────────────────────────────────

  server.tool(
    'progress_task',
    'Advance a task to its next pipeline stage',
    { id: z.string() },
    async ({ id }) => {
      requireScope(scopes, 'pipeline:control')
      const stageRun = await orchestrator.progressTask(id)
      if (!stageRun)
        mcpError('Task cannot progress (terminal, not found, or no free runner slot)')
      broadcast(id)
      const task = getTaskById(id)
      return { content: [{ type: 'text' as const, text: JSON.stringify({ task, stageRun }) }] }
    },
  )

  server.tool(
    'approve_task',
    'Approve a task at an approval gate (approval1 or approval2)',
    { id: z.string() },
    async ({ id }) => {
      requireScope(scopes, 'pipeline:control')
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

      // approval2: bulk-grant tool permissions declared in umsetzungskonzept output
      if (task.currentStage === 'approval2')
        bulkGrantKonzeptPermissions(task.id)

      updateTask(id, { currentStage: next })
      appendAudit({ taskId: id, actor: 'user', action: 'approved', details: { from: task.currentStage, to: next } })
      broadcast(id)
      return { content: [{ type: 'text' as const, text: JSON.stringify({ task: getTaskById(id) }) }] }
    },
  )

  server.tool(
    'request_changes',
    'Reject an approval artifact and regress the task with user feedback',
    { id: z.string(), feedback: z.string().min(1).max(4000) },
    async ({ id, feedback }) => {
      requireScope(scopes, 'pipeline:control')
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
      updateTask(id, { currentStage: regressionStage })
      appendAudit({
        taskId: task.id,
        actor: 'user',
        action: 'request_changes',
        details: { fromStage: task.currentStage, toStage: regressionStage, feedbackId: feedbackRow.id, iteration: feedbackRow.iteration },
      })
      broadcast(id)
      return { content: [{ type: 'text' as const, text: JSON.stringify({ task: getTaskById(id) }) }] }
    },
  )

  server.tool(
    'cancel_task',
    'Cancel a task',
    { id: z.string() },
    async ({ id }) => {
      requireScope(scopes, 'pipeline:control')
      const task = getTaskById(id)
      if (!task)
        mcpError(`Task not found: ${id}`)
      if (task.currentStage === 'done' || task.currentStage === 'cancelled')
        mcpError(`Task is already ${task.currentStage}`)
      updateTask(id, { currentStage: 'cancelled' })
      appendAudit({ taskId: id, actor: 'user', action: 'cancelled' })
      broadcast(id)
      return { content: [{ type: 'text' as const, text: JSON.stringify({ task: getTaskById(id) }) }] }
    },
  )

  server.tool(
    'retry_task',
    'Retry a task whose latest stage run failed',
    { id: z.string() },
    async ({ id }) => {
      requireScope(scopes, 'pipeline:control')
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
      return { content: [{ type: 'text' as const, text: JSON.stringify({ task: getTaskById(id), stageRun }) }] }
    },
  )

  server.tool(
    'grant_permission',
    'Pre-approve a tool for a task (added to allow-list for future stage runs)',
    {
      task_id: z.string(),
      tool: z.string().refine(t => ALLOWED_TOOLS.has(t), {
        message: `tool must be one of: ${[...ALLOWED_TOOLS].sort().join(', ')}`,
      }),
      pattern: z.string().max(256).optional(),
    },
    async ({ task_id, tool, pattern }) => {
      requireScope(scopes, 'pipeline:control')
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
      return { content: [{ type: 'text' as const, text: JSON.stringify(perm) }] }
    },
  )

  server.tool(
    'resolve_permission_request',
    'Grant or deny a runtime permission request from a stage agent',
    { request_id: z.string(), outcome: z.enum(['granted', 'denied']) },
    async ({ request_id, outcome }) => {
      requireScope(scopes, 'pipeline:control')
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
      }
      return { content: [{ type: 'text' as const, text: JSON.stringify(resolved) }] }
    },
  )

  // ─── keys:manage ─────────────────────────────────────────────

  server.tool(
    'list_api_keys',
    'List all MCP API keys',
    { include_revoked: z.boolean().optional() },
    async ({ include_revoked }) => {
      requireScope(scopes, 'keys:manage')
      return { content: [{ type: 'text' as const, text: JSON.stringify(listApiKeys({ includeRevoked: include_revoked })) }] }
    },
  )

  server.tool(
    'create_api_key',
    'Create a new MCP API key — the token is returned once only and never stored',
    {
      name: z.string().describe('Unique human-readable name for this key'),
      scopes: z.array(z.enum(['tasks:read', 'tasks:write', 'pipeline:control', 'keys:manage'])),
    },
    async (args) => {
      requireScope(scopes, 'keys:manage')
      const token = `mcp_${randomBytes(16).toString('hex')}`
      const keyHash = createHash('sha256').update(token).digest('hex')
      const key = createApiKey({ name: args.name, keyHash, scopes: args.scopes as McpScope[] })
      return { content: [{ type: 'text' as const, text: JSON.stringify({ key, token }) }] }
    },
  )

  server.tool(
    'revoke_api_key',
    'Revoke an MCP API key by ID (soft delete)',
    { id: z.string() },
    async ({ id }) => {
      requireScope(scopes, 'keys:manage')
      if (!getApiKeyById(id))
        mcpError(`API key not found: ${id}`)
      revokeApiKey(id)
      return { content: [{ type: 'text' as const, text: JSON.stringify({ success: true }) }] }
    },
  )

  return server
}
