import type { Agent } from '../types'
import { ref } from 'vue'
import { bulkResolvePermissionRequests } from './useTasks'

export function usePermissionResolve() {
  // Keyed by sessionId; true while an in-flight resolve is pending for that agent
  const resolving = ref<Record<string, boolean>>({})

  async function resolveAgent(agent: Agent, outcome: 'granted' | 'denied', remember = false): Promise<string | null> {
    if (!agent.pipelineTaskId || !agent.pendingPermissions?.length)
      return null
    resolving.value = { ...resolving.value, [agent.sessionId]: true }
    try {
      await bulkResolvePermissionRequests(
        agent.pipelineTaskId,
        agent.pendingPermissions.map(p => p.id),
        outcome,
        remember,
      )
      return null
    }
    catch (err) {
      return err instanceof Error ? err.message : 'Failed to resolve permissions'
    }
    finally {
      const next = { ...resolving.value }
      delete next[agent.sessionId]
      resolving.value = next
    }
  }

  // Groups selected agents by pipelineTaskId — one bulk call per task
  async function approveAll(agents: Agent[], outcome: 'granted' | 'denied' = 'granted', remember = false): Promise<string | null> {
    const byTask = new Map<string, string[]>()
    for (const agent of agents) {
      if (!agent.pipelineTaskId || !agent.pendingPermissions?.length)
        continue
      const ids = byTask.get(agent.pipelineTaskId) ?? []
      ids.push(...agent.pendingPermissions.map(p => p.id))
      byTask.set(agent.pipelineTaskId, ids)
    }

    const errors: string[] = []
    await Promise.all(
      Array.from(byTask.entries()).map(async ([taskId, ids]) => {
        try {
          await bulkResolvePermissionRequests(taskId, ids, outcome, remember)
        }
        catch (err) {
          errors.push(err instanceof Error ? err.message : 'Unknown error')
        }
      }),
    )

    return errors.length ? `Approve all failed: ${errors.join('; ')}` : null
  }

  return { resolving, resolveAgent, approveAll }
}
