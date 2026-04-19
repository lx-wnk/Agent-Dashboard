import type { Agent } from '../src/types'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { calculateStatus, enrichWithPipelineTask } from './agentMerger'
import { findStageRunBySessionId } from './db/stageRunsRepo.js'
import { getTaskById } from './db/tasksRepo.js'

vi.mock('./db/stageRunsRepo.js', () => ({
  findStageRunBySessionId: vi.fn(),
}))
vi.mock('./db/tasksRepo.js', () => ({
  getTaskById: vi.fn(),
}))

// Thresholds from agentMerger.ts:
//   ACTIVE_THRESHOLD = 30_000  (30s)
//   IDLE_THRESHOLD   = 300_000 (5min)

const FIXED_NOW = new Date('2024-06-01T12:00:00.000Z').getTime()

describe('calculateStatus', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(FIXED_NOW)
  })

  afterEach(() => {
    vi.useRealTimers()
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
  const mockFindStageRun = vi.mocked(findStageRunBySessionId)
  const mockGetTask = vi.mocked(getTaskById)

  afterEach(() => vi.clearAllMocks())

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
    }
  }

  it('attaches pipelineTaskId and pipelineTaskTitle when stage run and task exist', () => {
    mockFindStageRun.mockReturnValue({
      id: 'run-1',
      taskId: 'task-abc',
      stage: 'umsetzung',
      sessionId: 'sess-1',
      sessionName: null,
      pid: 1,
      status: 'running',
      startedAt: null,
      endedAt: null,
      iteration: 1,
      output: null,
      tokensUsed: 0,
      costCents: 0,
    })
    mockGetTask.mockReturnValue({
      id: 'task-abc',
      slug: 'my-task',
      title: 'My Task',
      description: null,
      cwd: '/tmp',
      worktreePath: null,
      sourceBranch: null,
      targetBranch: null,
      currentStage: 'umsetzung',
      parentTaskId: null,
      maxIterations: 5,
      tokenBudget: null,
      costBudgetCents: null,
      stageTimeoutSeconds: 3600,
      createdAt: '',
      updatedAt: '',
      metadata: null,
      silverBullet: false,
      priority: 'medium',
    })
    const agents = [makeAgent('sess-1')]
    enrichWithPipelineTask(agents)
    expect(agents[0].pipelineTaskId).toBe('task-abc')
    expect(agents[0].pipelineTaskTitle).toBe('My Task')
  })

  it('leaves fields undefined when no stage run matches', () => {
    mockFindStageRun.mockReturnValue(null)
    const agents = [makeAgent('sess-2')]
    enrichWithPipelineTask(agents)
    expect(agents[0].pipelineTaskId).toBeUndefined()
    expect(agents[0].pipelineTaskTitle).toBeUndefined()
  })

  it('leaves fields undefined when stage run has no matching task', () => {
    mockFindStageRun.mockReturnValue({
      id: 'run-2',
      taskId: 'missing-task',
      stage: 'umsetzung',
      sessionId: 'sess-3',
      sessionName: null,
      pid: null,
      status: 'running',
      startedAt: null,
      endedAt: null,
      iteration: 1,
      output: null,
      tokensUsed: 0,
      costCents: 0,
    })
    mockGetTask.mockReturnValue(null)
    const agents = [makeAgent('sess-3')]
    enrichWithPipelineTask(agents)
    expect(agents[0].pipelineTaskId).toBeUndefined()
    expect(agents[0].pipelineTaskTitle).toBeUndefined()
  })

  it('silently skips an agent when the DB throws', () => {
    mockFindStageRun.mockImplementation(() => {
      throw new Error('DB not ready')
    })
    const agents = [makeAgent('sess-4')]
    expect(() => enrichWithPipelineTask(agents)).not.toThrow()
    expect(agents[0].pipelineTaskId).toBeUndefined()
  })
})
