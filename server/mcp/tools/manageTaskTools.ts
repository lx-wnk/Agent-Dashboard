import type { TaskPriority } from '../../../src/types.js'
import type { makeToolRegistrar } from '../mcpAuth.js'
import { z } from 'zod'
import { appendAudit } from '../../db/auditRepo.js'
import {
  deleteTaskPermission,
  listEffectiveTaskPermissions,
  listTaskPermissions,
} from '../../db/permissionsRepo.js'
import { getTaskById, updateTask } from '../../db/tasksRepo.js'
import {
  applyPermissionTemplateByName,
  bulkGrantPermissions,
  inheritParentPermissions,
  validatePermissionEntry,
} from '../../services/approvalUtils.js'
import { listTemplateNames } from '../../services/permissionTemplates.js'
import { mcpError, ok } from '../mcpAuth.js'

type ToolFn = ReturnType<typeof makeToolRegistrar>

/**
 * Consolidated retroactive task-management tool. One MCP entry point with
 * an `action` discriminator — keeps the tool surface small while exposing
 * every common edit operation an operator (or self-managing agent) needs:
 *
 *   - grant_permissions: bulk-add task permissions by template and/or list
 *   - revoke_permission: remove a single permission row by id
 *   - list_permissions:  inspect current grants (effective or all)
 *   - inherit_from_parent: copy parent's grants onto this task
 *   - set_metadata:      shallow-merge metadata patch (e.g. allowGitPush=true)
 *   - set_priority:      change task priority
 *   - set_budget:        update token / cost / iteration budgets
 *
 * Scope: tasks:write. All actions audit via appendAudit.
 */
export function registerManageTaskTool(
  tool: ToolFn,
  broadcast: (taskId: string) => void,
  callerUserId: string | null,
): void {
  tool(
    'manage_task',
    {
      task_id: z.string().describe('Target task id'),
      action: z.enum([
        'grant_permissions',
        'revoke_permission',
        'list_permissions',
        'inherit_from_parent',
        'set_metadata',
        'set_priority',
        'set_budget',
      ]),
      // grant_permissions
      template: z.enum(listTemplateNames() as [string, ...string[]]).optional(),
      permissions: z.array(z.object({
        tool: z.string(),
        pattern: z.string().nullable().optional(),
        expiresAt: z.string().nullable().optional(),
      })).optional(),
      // revoke_permission
      permission_id: z.string().optional(),
      // list_permissions
      effective_only: z.boolean().optional().describe('Filter expired/denied (default true)'),
      // inherit_from_parent — no extra params; uses task.parentTaskId
      // set_metadata
      metadata_patch: z.record(z.string(), z.unknown()).optional(),
      // set_priority
      priority: z.enum(['high', 'medium', 'low']).optional(),
      silverBullet: z.boolean().optional(),
      // set_budget
      tokenBudget: z.number().int().positive().nullable().optional(),
      costBudgetCents: z.number().int().positive().nullable().optional(),
      maxIterations: z.number().int().positive().optional(),
    },
    async (args) => {
      const task = getTaskById(args.task_id)
      if (!task)
        mcpError(`Task not found: ${args.task_id}`)
      if (callerUserId !== null && task.userId !== null && task.userId !== callerUserId)
        mcpError('Access denied: task belongs to a different user')

      switch (args.action) {
        case 'grant_permissions': {
          if (args.permissions) {
            for (const p of args.permissions) {
              const v = validatePermissionEntry(p.tool, p.pattern ?? null)
              if (!v.ok)
                mcpError(`permission rejected: ${v.reason}`)
            }
          }
          const summary: Record<string, unknown> = {}
          if (args.template) {
            const tplRes = applyPermissionTemplateByName(args.task_id, args.template)
            if (tplRes)
              summary.template = { name: args.template, granted: tplRes.granted.length, skipped: tplRes.skipped.length }
          }
          if (args.permissions && args.permissions.length > 0) {
            const r = bulkGrantPermissions(
              args.task_id,
              args.permissions.map(p => ({
                tool: p.tool,
                pattern: p.pattern ?? null,
                expiresAt: p.expiresAt ?? null,
              })),
              { source: 'mcp_manage_task' },
            )
            summary.explicit = { granted: r.granted.length, skipped: r.skipped.length }
          }
          broadcast(args.task_id)
          return ok({ action: 'grant_permissions', summary })
        }

        case 'revoke_permission': {
          if (!args.permission_id)
            mcpError('permission_id is required for revoke_permission')
          const removed = deleteTaskPermission(args.permission_id!)
          if (!removed)
            mcpError(`permission row not found: ${args.permission_id}`)
          appendAudit({
            taskId: args.task_id,
            actor: callerUserId ? 'user' : 'system',
            action: 'permission_revoked',
            details: { permissionId: args.permission_id, source: 'mcp_manage_task' },
          })
          broadcast(args.task_id)
          return ok({ action: 'revoke_permission', removed: args.permission_id })
        }

        case 'list_permissions': {
          const all = args.effective_only === false
            ? listTaskPermissions(args.task_id)
            : listEffectiveTaskPermissions(args.task_id)
          return ok({ action: 'list_permissions', permissions: all })
        }

        case 'inherit_from_parent': {
          if (!task.parentTaskId)
            mcpError('Task has no parentTaskId — cannot inherit')
          const r = inheritParentPermissions(args.task_id, task.parentTaskId!)
          broadcast(args.task_id)
          return ok({ action: 'inherit_from_parent', from: task.parentTaskId, granted: r.granted.length, skipped: r.skipped.length })
        }

        case 'set_metadata': {
          if (!args.metadata_patch || Object.keys(args.metadata_patch).length === 0)
            mcpError('metadata_patch with at least one key is required')
          const existing = (task.metadata ?? {}) as Record<string, unknown>
          const merged = { ...existing, ...args.metadata_patch }
          const updated = updateTask(args.task_id, { metadata: merged })
          appendAudit({
            taskId: args.task_id,
            actor: callerUserId ? 'user' : 'system',
            action: 'metadata_patched',
            details: { keys: Object.keys(args.metadata_patch), source: 'mcp_manage_task' },
          })
          broadcast(args.task_id)
          return ok({ action: 'set_metadata', task: updated })
        }

        case 'set_priority': {
          if (args.priority === undefined && args.silverBullet === undefined)
            mcpError('priority or silverBullet must be provided')
          const patch: { priority?: TaskPriority, silverBullet?: boolean } = {}
          if (args.priority !== undefined)
            patch.priority = args.priority
          if (args.silverBullet !== undefined)
            patch.silverBullet = args.silverBullet
          const updated = updateTask(args.task_id, patch)
          appendAudit({
            taskId: args.task_id,
            actor: callerUserId ? 'user' : 'system',
            action: 'priority_changed',
            details: { ...patch, source: 'mcp_manage_task' },
          })
          broadcast(args.task_id)
          return ok({ action: 'set_priority', task: updated })
        }

        case 'set_budget': {
          const patch: { tokenBudget?: number | null, costBudgetCents?: number | null, maxIterations?: number } = {}
          if (args.tokenBudget !== undefined)
            patch.tokenBudget = args.tokenBudget
          if (args.costBudgetCents !== undefined)
            patch.costBudgetCents = args.costBudgetCents
          if (args.maxIterations !== undefined)
            patch.maxIterations = args.maxIterations
          if (Object.keys(patch).length === 0)
            mcpError('at least one of tokenBudget, costBudgetCents, maxIterations is required')
          const updated = updateTask(args.task_id, patch)
          appendAudit({
            taskId: args.task_id,
            actor: callerUserId ? 'user' : 'system',
            action: 'budget_changed',
            details: { ...patch, source: 'mcp_manage_task' },
          })
          broadcast(args.task_id)
          return ok({ action: 'set_budget', task: updated })
        }

        default: {
          // Should be unreachable given Zod enum guard.
          const _exhaustive: never = args.action as never
          mcpError(`Unsupported action: ${_exhaustive}`)
        }
      }
    },
  )
}
