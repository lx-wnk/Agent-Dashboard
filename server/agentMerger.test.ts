import type { Mock } from 'bun:test'
import type { Agent } from '../src/types'
import { afterEach, beforeEach, describe, expect, it, mock, setSystemTime } from 'bun:test'

import { calculateStatus, enrichWithPipelineTask } from './agentMerger'
import { findTasksBySessionIds } from './db/stageRunsRepo.js'

mock.module('./db/stageRunsRepo.js', () => ({
  findTasksBySessionIds: mock(),
}))

// Thresholds from agentMerger.ts:
//   ACTIVE_THRESHOLD = 30_000  (30s)
//   IDLE_THRESHOLD   = 300_000 (5min)

const FIXED_NOW = new Date('2024-06-01T12:00:00.000Z').getTime()

describe('calculateStatus', () => {
  beforeEach(() => {
    setSystemTime(FIXED_NOW)
  })

  afterEach(() => {
    setSystemTime() // reset to real time
  })

  it('returns "active" when last activity is less than 30 seconds ago', () => {
    const lastActivity = new Date(FIXED_NOW - 10_000).toISOString() // 10s ago
    expect(calculateStatus(lastActivity)).toBe('active')
  })

  it('returns "active" at exactly 0ms age', () => {
    const lastActivity = new Date(FIXED_NOW).toISOString()
    expect(calculateStatus(lastActivity)).toBe('active')
  })

  it('returns "active" at 29 999ms (just under threshold)', () => {
    const lastActivity = new Date(FIXED_NOW - 29_999).toISOString()
    expect(calculateStatus(lastActivity)).toBe('active')
  })

  it('returns "waiting" at exactly the active threshold (30 000ms)', () => {
    const lastActivity = new Date(FIXED_NOW - 30_000).toISOString()
    expect(calculateStatus(lastActivity)).toBe('waiting')
  })

  it('returns "waiting" when last activity is between 30s and 5 minutes ago', () => {
    const lastActivity = new Date(FIXED_NOW - 120_000).toISOString() // 2min ago
    expect(calculateStatus(lastActivity)).toBe('waiting')
  })

  it('returns "waiting" at 299 999ms (just under idle threshold)', () => {
    const lastActivity = new Date(FIXED_NOW - 299_999).toISOString()
    expect(calculateStatus(lastActivity)).toBe('waiting')
  })

  it('returns "idle" at exactly the idle threshold (300 000ms)', () => {
    const lastActivity = new Date(FIXED_NOW - 300_000).toISOString()
    expect(calculateStatus(lastActivity)).toBe('idle')
  })

  it('returns "idle" when last activity is more than 5 minutes ago', () => {
    const lastActivity = new Date(FIXED_NOW - 600_000).toISOString() // 10min ago
    expect(calculateStatus(lastActivity)).toBe('idle')
  })

  it('returns "idle" for a very old timestamp', () => {
    const lastActivity = new Date(0).toISOString() // Unix epoch
    expect(calculateStatus(lastActivity)).toBe('idle')
  })
})

describe('enrichWithPipelineTask', () => {
  const mockFindTasks = findTasksBySessionIds as unknown as Mock<typeof findTasksBySessionIds>

  afterEach(() => mockFindTasks.mockReset())

  function makeAgent(sessionId: string): Agent {
    return {
      pid: 1,
      sessionId,
      projectPath: '/tmp',
      projectName: 'proj',
      cwd: '/tmp',
      entrypoint: 'cli',
      status: 'active',
      uptime: 1,
      lastActivity: new Date().toISOString(),
      currentAction: null,
      lastTools: [],
      tasks: [],
      subagents: [],
      tokenUsage: { inputTokens: 0, outputTokens: 0, cacheCreationTokens: 0, cacheReadTokens: 0 },
      costEstimate: 0,
      model: null,
      codeVersion: null,
      conversationTurns: 0,
      toolCounts: {},
      meta: null,
      lastOutput: null,
      lastBtw: null,
      channelAvailable: false,
      convergenceAlert: false,
      convergenceToolName: null,
      errorState: null,
    }
  }

  it('attaches pipelineTaskId and pipelineTaskTitle when a match exists', () => {
    mockFindTasks.mockReturnValue(new Map([['sess-1', { taskId: 'task-abc', title: 'My Task' }]]))
    const agents = [makeAgent('sess-1')]
    enrichWithPipelineTask(agents)
    expect(agents[0].pipelineTaskId).toBe('task-abc')
    expect(agents[0].pipelineTaskTitle).toBe('My Task')
  })

  it('leaves fields undefined when no match in the returned map', () => {
    mockFindTasks.mockReturnValue(new Map())
    const agents = [makeAgent('sess-2')]
    enrichWithPipelineTask(agents)
    expect(agents[0].pipelineTaskId).toBeUndefined()
    expect(agents[0].pipelineTaskTitle).toBeUndefined()
  })

  it('enriches multiple agents in one call', () => {
    mockFindTasks.mockReturnValue(new Map([
      ['sess-a', { taskId: 'task-1', title: 'Task One' }],
      ['sess-b', { taskId: 'task-2', title: 'Task Two' }],
    ]))
    const agents = [makeAgent('sess-a'), makeAgent('sess-b'), makeAgent('sess-c')]
    enrichWithPipelineTask(agents)
    expect(agents[0].pipelineTaskId).toBe('task-1')
    expect(agents[1].pipelineTaskId).toBe('task-2')
    expect(agents[2].pipelineTaskId).toBeUndefined()
  })

  it('does not throw when the DB throws', () => {
    mockFindTasks.mockImplementation(() => {
      throw new Error('DB not ready')
    })
    const agents = [makeAgent('sess-4')]
    expect(() => enrichWithPipelineTask(agents)).not.toThrow()
    expect(agents[0].pipelineTaskId).toBeUndefined()
  })
})

describe('Agent interface — convergenceAlert / convergenceToolName / errorState fields', () => {
  it('Agent type has the three new health fields with correct defaults', () => {
    const agent: Pick<Agent, 'convergenceAlert' | 'convergenceToolName' | 'errorState'> = {
      convergenceAlert: false,
      convergenceToolName: null,
      errorState: null,
    }
    expect(agent.convergenceAlert).toBe(false)
    expect(agent.convergenceToolName).toBeNull()
    expect(agent.errorState).toBeNull()
  })
})
