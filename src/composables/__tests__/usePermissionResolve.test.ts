import type { Agent, PendingPermission } from '../../types'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const mockBulkResolve = vi.fn()

vi.mock('../useTasks', () => ({
  bulkResolvePermissionRequests: mockBulkResolve,
}))

function makePerm(id: string, tool: string): PendingPermission {
  return { id, tool, requestedAt: new Date().toISOString() }
}

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    sessionId: 'sess-1',
    pid: 1,
    projectPath: '/proj',
    projectName: 'proj',
    status: 'active',
    costEstimate: 0,
    tokenUsage: { inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheCreationTokens: 0 },
    lastActivity: new Date().toISOString(),
    tasks: [],
    subagents: [],
    meta: null,
    pendingPermissions: undefined,
    ...overrides,
  } as Agent
}

describe('usePermissionResolve', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockBulkResolve.mockResolvedValue({ resolved: 1, errors: [] })
  })

  describe('resolveAgent', () => {
    it('calls bulkResolvePermissionRequests with the agent ids and outcome', async () => {
      const { usePermissionResolve } = await import('../usePermissionResolve')
      const { resolveAgent } = usePermissionResolve()
      const agent = makeAgent({
        sessionId: 'sess-a',
        pipelineTaskId: 'task-1',
        pendingPermissions: [makePerm('perm-1', 'Bash'), makePerm('perm-2', 'Read')],
      })

      const err = await resolveAgent(agent, 'granted')

      expect(err).toBeNull()
      expect(mockBulkResolve).toHaveBeenCalledOnce()
      expect(mockBulkResolve).toHaveBeenCalledWith('task-1', ['perm-1', 'perm-2'], 'granted', false)
    })

    it('forwards remember: true to bulkResolvePermissionRequests', async () => {
      const { usePermissionResolve } = await import('../usePermissionResolve')
      const { resolveAgent } = usePermissionResolve()
      const agent = makeAgent({
        sessionId: 'sess-b',
        pipelineTaskId: 'task-2',
        pendingPermissions: [makePerm('perm-3', 'Bash')],
      })

      const err = await resolveAgent(agent, 'granted', true)

      expect(err).toBeNull()
      expect(mockBulkResolve).toHaveBeenCalledWith('task-2', ['perm-3'], 'granted', true)
    })

    it('returns an error string when the API throws', async () => {
      mockBulkResolve.mockRejectedValue(new Error('network error'))
      const { usePermissionResolve } = await import('../usePermissionResolve')
      const { resolveAgent } = usePermissionResolve()
      const agent = makeAgent({
        pipelineTaskId: 'task-1',
        pendingPermissions: [makePerm('perm-1', 'Bash')],
      })

      const err = await resolveAgent(agent, 'denied')

      expect(err).toBe('network error')
    })

    it('does nothing when agent has no pipelineTaskId', async () => {
      const { usePermissionResolve } = await import('../usePermissionResolve')
      const { resolveAgent } = usePermissionResolve()
      const agent = makeAgent({ pipelineTaskId: '' })

      const err = await resolveAgent(agent, 'granted')

      expect(err).toBeNull()
      expect(mockBulkResolve).not.toHaveBeenCalled()
    })
  })

  describe('approveAll', () => {
    it('groups agents by pipelineTaskId into one bulk call each', async () => {
      const { usePermissionResolve } = await import('../usePermissionResolve')
      const { approveAll } = usePermissionResolve()

      const agents = [
        makeAgent({
          sessionId: 'sess-1',
          pipelineTaskId: 'task-1',
          pendingPermissions: [makePerm('p1', 'Bash')],
        }),
        makeAgent({
          sessionId: 'sess-2',
          pipelineTaskId: 'task-1',
          pendingPermissions: [makePerm('p2', 'Read')],
        }),
        makeAgent({
          sessionId: 'sess-3',
          pipelineTaskId: 'task-2',
          pendingPermissions: [makePerm('p3', 'Write')],
        }),
      ]

      const err = await approveAll(agents)

      expect(err).toBeNull()
      expect(mockBulkResolve).toHaveBeenCalledTimes(2)
      expect(mockBulkResolve).toHaveBeenCalledWith('task-1', ['p1', 'p2'], 'granted', false)
      expect(mockBulkResolve).toHaveBeenCalledWith('task-2', ['p3'], 'granted', false)
    })

    it('forwards remember: true to every bulk call', async () => {
      const { usePermissionResolve } = await import('../usePermissionResolve')
      const { approveAll } = usePermissionResolve()

      const agents = [
        makeAgent({
          sessionId: 'sess-1',
          pipelineTaskId: 'task-1',
          pendingPermissions: [makePerm('p1', 'Bash')],
        }),
      ]

      const err = await approveAll(agents, 'granted', true)

      expect(err).toBeNull()
      expect(mockBulkResolve).toHaveBeenCalledWith('task-1', ['p1'], 'granted', true)
    })

    it('returns an error string when any bulk call fails', async () => {
      mockBulkResolve.mockRejectedValue(new Error('task blown up'))
      const { usePermissionResolve } = await import('../usePermissionResolve')
      const { approveAll } = usePermissionResolve()

      const agents = [
        makeAgent({
          sessionId: 'sess-1',
          pipelineTaskId: 'task-1',
          pendingPermissions: [makePerm('p1', 'Bash')],
        }),
      ]

      const err = await approveAll(agents)

      expect(err).toMatch(/Approve all failed/)
      expect(err).toMatch(/task blown up/)
    })
  })
})
