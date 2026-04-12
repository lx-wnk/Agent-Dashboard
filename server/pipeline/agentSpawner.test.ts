import type { PipelineTask, StageRun, TaskPermission } from '../../src/types.js'
import type { SpawnAgentOptions } from './agentSpawner.js'
import { describe, expect, it } from 'vitest'
import { buildAllowList, buildSpawnArgs, buildSpawnEnv } from './agentSpawner.js'

function makeTask(overrides: Partial<PipelineTask> = {}): PipelineTask {
  return {
    id: 't-1',
    slug: 'fix-login',
    title: 'Fix login bug',
    description: null,
    cwd: '/tmp/project',
    worktreePath: null,
    sourceBranch: null,
    targetBranch: null,
    currentStage: 'umsetzung',
    parentTaskId: null,
    maxIterations: 20,
    tokenBudget: null,
    costBudgetCents: null,
    stageTimeoutSeconds: 1800,
    createdAt: '2026-04-12T10:00:00Z',
    updatedAt: '2026-04-12T10:00:00Z',
    metadata: null,
    silverBullet: false,
    priority: 'medium',
    ...overrides,
  }
}

function makeStageRun(): StageRun {
  return {
    id: 'sr-1',
    taskId: 't-1',
    stage: 'umsetzung',
    sessionId: null,
    sessionName: 'fix-login-umsetzung-iter-0',
    pid: null,
    status: 'pending',
    startedAt: null,
    endedAt: null,
    iteration: 0,
    output: null,
    tokensUsed: 0,
    costCents: 0,
  }
}

function permission(overrides: Partial<TaskPermission>): TaskPermission {
  return {
    id: 'p-x',
    taskId: 't-1',
    tool: 'Bash',
    pattern: null,
    granted: true,
    preApproved: true,
    requestedAt: '2026-04-12T10:00:00Z',
    decidedAt: '2026-04-12T10:00:00Z',
    decidedBy: 'user',
    ...overrides,
  }
}

const baseOpts: SpawnAgentOptions = {
  task: makeTask(),
  stageRun: makeStageRun(),
  prompt: 'implement the thing',
  permissions: [],
}

describe('buildAllowList', () => {
  it('emits Tool(pattern) for permissions with patterns', () => {
    const list = buildAllowList([
      permission({ tool: 'Bash', pattern: 'npm *' }),
      permission({ tool: 'Bash', pattern: 'git status' }),
    ])
    expect(list).toEqual(['Bash(npm *)', 'Bash(git status)'])
  })

  it('emits bare Tool when pattern is null', () => {
    const list = buildAllowList([
      permission({ tool: 'Read', pattern: null }),
      permission({ tool: 'Grep', pattern: null }),
    ])
    expect(list).toEqual(['Read', 'Grep'])
  })

  it('filters out denied permissions', () => {
    const list = buildAllowList([
      permission({ tool: 'Read', granted: true }),
      permission({ tool: 'WebFetch', granted: false }),
    ])
    expect(list).toEqual(['Read'])
    expect(list).not.toContain('WebFetch')
  })

  it('returns an empty array when all permissions are denied', () => {
    const list = buildAllowList([
      permission({ tool: 'Bash', granted: false }),
      permission({ tool: 'WebFetch', granted: false }),
    ])
    expect(list).toEqual([])
  })
})

describe('buildSpawnArgs', () => {
  it('always includes the -p prompt arg', () => {
    const args = buildSpawnArgs({ ...baseOpts, prompt: 'do x' })
    expect(args).toContain('-p')
    expect(args).toContain('do x')
  })

  it('includes --resume when resumeSessionId is set', () => {
    const args = buildSpawnArgs({ ...baseOpts, resumeSessionId: 'sess-abc' })
    const idx = args.indexOf('--resume')
    expect(idx).toBeGreaterThanOrEqual(0)
    expect(args[idx + 1]).toBe('sess-abc')
  })

  it('omits --resume when resumeSessionId is not set', () => {
    const args = buildSpawnArgs(baseOpts)
    expect(args).not.toContain('--resume')
  })

  it('includes --model when set', () => {
    const args = buildSpawnArgs({ ...baseOpts, model: 'claude-opus-4-6' })
    const idx = args.indexOf('--model')
    expect(args[idx + 1]).toBe('claude-opus-4-6')
  })

  it('includes --system-prompt when set and caps at 10000 chars', () => {
    const longPrompt = 'x'.repeat(12000)
    const args = buildSpawnArgs({ ...baseOpts, systemPrompt: longPrompt })
    const idx = args.indexOf('--system-prompt')
    expect(args[idx + 1]?.length).toBe(10000)
  })

  it('orders args as --resume, -p, --model, --system-prompt', () => {
    const args = buildSpawnArgs({
      ...baseOpts,
      resumeSessionId: 'sess',
      model: 'claude-haiku-4-5',
      systemPrompt: 'you are an agent',
    })
    // --resume comes first, then -p, then --model, then --system-prompt
    expect(args.indexOf('--resume')).toBeLessThan(args.indexOf('-p'))
    expect(args.indexOf('-p')).toBeLessThan(args.indexOf('--model'))
    expect(args.indexOf('--model')).toBeLessThan(args.indexOf('--system-prompt'))
  })
})

describe('buildSpawnEnv', () => {
  it('injects DASHBOARD_STAGE_RUN_ID and DASHBOARD_TASK_ID', () => {
    const env = buildSpawnEnv(baseOpts)
    expect(env.DASHBOARD_STAGE_RUN_ID).toBe('sr-1')
    expect(env.DASHBOARD_TASK_ID).toBe('t-1')
  })

  it('preserves the parent process env', () => {
    const env = buildSpawnEnv(baseOpts)
    // PATH is present on every sane system
    expect(env.PATH).toBeDefined()
  })
})
