import type { SpawnResult } from './agentSpawner.js'
import type { StageContext } from './types.js'
import { describe, expect, it } from 'vitest'
import { backlogHandler, createAgentStage, konzeptHandler } from './stageHandlers.js'

function makeContext(overrides: Partial<StageContext> = {}): StageContext {
  const task = {
    id: 'task-1',
    slug: 'fix-bug',
    title: 'Fix bug',
    description: 'something',
    cwd: '/tmp/x',
    worktreePath: null,
    sourceBranch: null,
    targetBranch: null,
    currentStage: 'umsetzung' as const,
    parentTaskId: null,
    maxIterations: 20,
    tokenBudget: null,
    costBudgetCents: null,
    stageTimeoutSeconds: 1800,
    createdAt: '2026-04-12T10:00:00Z',
    updatedAt: '2026-04-12T10:00:00Z',
    metadata: null,
    silverBullet: false,
    priority: 'medium' as const,
  }
  const stageRun = {
    id: 'run-1',
    taskId: 'task-1',
    stage: 'umsetzung' as const,
    sessionId: null,
    sessionName: null,
    pid: null,
    status: 'running' as const,
    startedAt: '2026-04-12T10:00:00Z',
    endedAt: null,
    iteration: 0,
    output: null,
    tokensUsed: 0,
    costCents: 0,
  }
  return {
    task,
    stageRun,
    permissions: [],
    previousOutput: null,
    priorIterationOutput: null,
    recordAudit: () => {},
    requestPermission: () => { throw new Error('not used') },
    ...overrides,
  }
}

function fakeSpawn(pid = 9999) {
  const calls: Array<{ prompt: string, systemPrompt?: string }> = []
  const spawn = ((opts: { prompt: string, systemPrompt?: string }): SpawnResult => {
    calls.push({ prompt: opts.prompt, systemPrompt: opts.systemPrompt })
    return { child: {} as any, pid, cwd: '/tmp/x', settingsPath: null }
  }) as any
  return { spawn, calls }
}

describe('backlogHandler', () => {
  it('transitions immediately to umsetzung without spawning an agent', async () => {
    expect(backlogHandler.requiresAgent).toBe(false)
    const audits: Array<{ action: string }> = []
    const ctx = makeContext({
      recordAudit: action => audits.push({ action }),
    })

    const transition = await backlogHandler.execute(ctx)

    expect(transition).toEqual({ kind: 'next', toStage: 'umsetzung' })
    expect(audits[0].action).toBe('backlog_entered')
  })
})

describe('konzeptHandler', () => {
  it('is agent-less and returns wait_user as a safety net', async () => {
    expect(konzeptHandler.requiresAgent).toBe(false)
    const audits: Array<{ action: string }> = []
    const ctx = makeContext({
      recordAudit: action => audits.push({ action }),
    })

    const transition = await konzeptHandler.execute(ctx)

    expect(transition.kind).toBe('wait_user')
    expect(audits[0].action).toBe('konzept_chat_pending')
  })
})

describe('createAgentStage', () => {
  it('spawns with the provided prompt and returns async_running carrying the PID', async () => {
    const { spawn, calls } = fakeSpawn(4242)
    const handler = createAgentStage(
      'umsetzung',
      () => ({ systemPrompt: 'sys', userPrompt: 'Implement the thing' }),
      spawn,
    )

    const transition = await handler.execute(makeContext())

    expect(transition).toEqual({ kind: 'async_running', pid: 4242 })
    expect(calls).toHaveLength(1)
    expect(calls[0].prompt).toContain('Implement the thing')
  })

  it('omits the correction prefix on iteration 0', async () => {
    const { spawn, calls } = fakeSpawn()
    const handler = createAgentStage(
      'umsetzung',
      () => ({ systemPrompt: 'sys', userPrompt: 'body' }),
      spawn,
    )

    await handler.execute(makeContext())

    expect(calls[0].prompt).not.toContain('CORRECTION REQUIRED')
  })

  it('prepends a correction block when a prior iteration had a validation error', async () => {
    const { spawn, calls } = fakeSpawn()
    const handler = createAgentStage(
      'umsetzung',
      () => ({ systemPrompt: 'sys', userPrompt: 'body' }),
      spawn,
    )

    const ctx = makeContext({
      stageRun: { ...makeContext().stageRun, iteration: 1 },
      priorIterationOutput: {
        validation_error: 'missing required field: summary',
        rejected_output: { steps: [] },
      },
    })
    await handler.execute(ctx)

    expect(calls[0].prompt).toContain('CORRECTION REQUIRED')
    expect(calls[0].prompt).toContain('missing required field: summary')
    expect(calls[0].prompt).toContain('body')
  })

  it('records an audit entry with the spawned pid and iteration', async () => {
    const { spawn } = fakeSpawn(7777)
    const handler = createAgentStage(
      'umsetzung',
      () => ({ systemPrompt: 'sys', userPrompt: 'body' }),
      spawn,
    )
    const audits: Array<{ action: string, details?: Record<string, unknown> }> = []

    const ctx = makeContext({
      recordAudit: (action, details) => audits.push({ action, details }),
    })
    await handler.execute(ctx)

    expect(audits).toHaveLength(1)
    expect(audits[0].action).toBe('umsetzung_spawned')
    expect(audits[0].details).toMatchObject({ pid: 7777, iteration: 0, hasFeedback: false })
  })
})
