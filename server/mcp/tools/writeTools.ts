import type { makeToolRegistrar } from '../mcpAuth.js'
import { z } from 'zod'
import { SLUG_PATTERN_MESSAGE, SLUG_RE } from '../../constants.js'
import { createTask, deleteTask, getTaskById, getTaskBySlug, updateTask } from '../../db/tasksRepo.js'
import {
  applyPermissionTemplateByName,
  bulkGrantPermissions,
  inheritParentPermissions,
  validatePermissionEntry,
} from '../../services/approvalUtils.js'
import { listTemplateNames } from '../../services/permissionTemplates.js'
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
      metadata: z.record(z.string(), z.unknown()).optional().describe(
        'Free-form task metadata. Recognised keys: `allowGitPush: boolean` to opt this task out of the global git-push hard-block (default false).',
      ),
      sourceBranch: z.string().optional(),
      targetBranch: z.string().optional(),
      parentTaskId: z.string().optional().describe('If this task is spawned by another task, set parent ID to enable inheritPermissions=true.'),
      maxIterations: z.number().int().positive().optional(),
      tokenBudget: z.number().int().positive().optional(),
      costBudgetCents: z.number().int().positive().optional(),
      template: z.enum(listTemplateNames() as [string, ...string[]]).optional().describe(
        'Predefined permission set (e.g. feature_implementation). Applied before explicit permissions[].',
      ),
      permissions: z.array(z.object({
        tool: z.string(),
        pattern: z.string().nullable().optional(),
        expiresAt: z.string().nullable().optional().describe('ISO timestamp; null = never expires.'),
      })).optional().describe(
        'Explicit permission grants applied at task creation. STRONGLY RECOMMENDED for self-spawning agents: declare every tool you anticipate needing here so no mid-run permission prompts pause your work. Combine with `template` for ergonomic baseline + extensions.',
      ),
      inheritPermissions: z.boolean().optional().describe(
        'When true and `parentTaskId` set and no explicit permissions[] given: copy the parent task\'s effective permissions to this child. Default false.',
      ),
    },
    async (args) => {
      if (!SLUG_RE.test(args.slug))
        mcpError(SLUG_PATTERN_MESSAGE)
      if (getTaskBySlug(args.slug))
        mcpError(`slug already exists: ${args.slug}`)

      // Pre-validate permissions[] before any DB writes; reject early.
      if (args.permissions) {
        for (const p of args.permissions) {
          const v = validatePermissionEntry(p.tool, p.pattern ?? null)
          if (!v.ok)
            mcpError(`permission rejected: ${v.reason}`)
        }
      }

      const task = createTask({
        slug: args.slug,
        title: args.title,
        description: args.description ?? null,
        cwd: args.cwd,
        sourceBranch: args.sourceBranch ?? null,
        targetBranch: args.targetBranch ?? null,
        parentTaskId: args.parentTaskId ?? null,
        metadata: args.metadata ?? null,
        silverBullet: args.silverBullet ?? false,
        priority: args.priority,
        maxIterations: args.maxIterations,
        tokenBudget: args.tokenBudget ?? null,
        costBudgetCents: args.costBudgetCents ?? null,
      })

      const seedSummary: Record<string, unknown> = {}

      if (args.template) {
        const tplRes = applyPermissionTemplateByName(task.id, args.template)
        if (tplRes)
          seedSummary.template = { name: args.template, granted: tplRes.granted.length, skipped: tplRes.skipped.length }
      }

      if (args.permissions && args.permissions.length > 0) {
        const explicitRes = bulkGrantPermissions(
          task.id,
          args.permissions.map(p => ({
            tool: p.tool,
            pattern: p.pattern ?? null,
            expiresAt: p.expiresAt ?? null,
          })),
          { source: 'mcp_create_task' },
        )
        seedSummary.explicit = { granted: explicitRes.granted.length, skipped: explicitRes.skipped.length }
      }

      if (
        args.inheritPermissions === true
        && (!args.permissions || args.permissions.length === 0)
        && args.parentTaskId
      ) {
        const inhRes = inheritParentPermissions(task.id, args.parentTaskId)
        seedSummary.inherited = { fromParent: args.parentTaskId, granted: inhRes.granted.length }
      }

      broadcast(task.id)
      return ok({ task, permissions: seedSummary })
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
