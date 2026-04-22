import type { makeToolRegistrar } from '../mcpAuth.js'
import { z } from 'zod'
import {
  addDependency,
  removeDependency,
} from '../../db/taskDependenciesRepo.js'
import { getTaskById } from '../../db/tasksRepo.js'
import { mcpError, ok } from '../mcpAuth.js'

type ToolFn = ReturnType<typeof makeToolRegistrar>

export function registerDependencyTools(tool: ToolFn, broadcast: (taskId: string) => void): void {
  tool(
    'add_dependency',
    {
      task_id: z.string().describe('ID of the dependent task (the one that waits)'),
      depends_on_id: z.string().describe('ID of the prerequisite task'),
      required_stage: z.enum(['done', 'cancelled']).default('done'),
      on_cancel_action: z
        .enum(['cancel', 'start', 'on_hold'])
        .default('on_hold')
        .describe('What to do with this task when the prerequisite is cancelled'),
    },
    async ({ task_id, depends_on_id, required_stage, on_cancel_action }) => {
      if (!getTaskById(task_id))
        mcpError(`Task not found: ${task_id}`)
      if (!getTaskById(depends_on_id))
        mcpError(`Prerequisite task not found: ${depends_on_id}`)
      try {
        const dep = addDependency(task_id, depends_on_id, required_stage, on_cancel_action)
        broadcast(task_id)
        return ok(dep)
      }
      catch (err) {
        mcpError((err as Error).message)
      }
    },
  )

  tool(
    'remove_dependency',
    {
      task_id: z.string().describe('ID of the dependent task'),
      depends_on_id: z.string().describe('ID of the prerequisite task to remove'),
    },
    async ({ task_id, depends_on_id }) => {
      const removed = removeDependency(task_id, depends_on_id)
      if (removed)
        broadcast(task_id)
      return ok({ removed })
    },
  )
}
