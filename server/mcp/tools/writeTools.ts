import type { makeToolRegistrar } from '../mcpAuth.js'
import { z } from 'zod'
import { SLUG_PATTERN_MESSAGE, SLUG_RE } from '../../constants.js'
import { createTask, deleteTask, getTaskById, getTaskBySlug, updateTask } from '../../db/tasksRepo.js'
import { mcpError, ok } from '../mcpAuth.js'

type ToolFn = ReturnType<typeof makeToolRegistrar>

export function registerWriteTools(
  tool: ToolFn,
  broadcast: (taskId: string) => void,
  broadcastDeleted: (taskId: string) => void,
  callerUserId: string | null,
): void {
  tool(
    'create_task',
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
      if (!SLUG_RE.test(args.slug))
        mcpError(SLUG_PATTERN_MESSAGE)
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
      return ok(task)
    },
  )

  tool(
    'update_task',
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
      const existing = getTaskById(id)
      if (!existing)
        mcpError(`Task not found: ${id}`)
      if (callerUserId !== null && existing.userId !== null && existing.userId !== callerUserId)
        mcpError('Access denied: task belongs to a different user')
      const task = updateTask(id, fields)
      if (!task)
        mcpError(`Task not found: ${id}`)
      broadcast(id)
      return ok(task)
    },
  )

  tool(
    'delete_task',
    { id: z.string() },
    async ({ id }) => {
      const existing = getTaskById(id)
      if (!existing)
        mcpError(`Task not found: ${id}`)
      if (callerUserId !== null && existing.userId !== null && existing.userId !== callerUserId)
        mcpError('Access denied: task belongs to a different user')
      const deleted = deleteTask(id)
      if (!deleted)
        mcpError(`Task not found: ${id}`)
      broadcastDeleted(id)
      return ok({ success: true })
    },
  )
}
