import type { SpawnResult } from './agentSpawner.js'
import type { StageContext } from './types.js'
import { describe, expect, it } from 'vitest'
import { createPruefungHandler } from './stageHandlers.js'

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
    currentStage: 'pruefung' as const,
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
    userId: null,
  }
  const stageRun = {
    id: 'run-1',
    taskId: 'task-1',
    stage: 'pruefung' as const,
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

describe('createPruefungHandler', () => {
  it('spawns with the pruefung prompt and returns async_running carrying the PID', async () => {
    const { spawn, calls } = fakeSpawn(4242)
    const handler = createPruefungHandler(spawn)

    const transition = await handler.execute(makeContext())

    expect(transition).toEqual({ kind: 'async_running', pid: 4242 })
    expect(calls).toHaveLength(1)
    expect(calls[0].prompt).toContain('Feasibility Check')
  })

  it('omits the correction prefix on iteration 0', async () => {
    const { spawn, calls } = fakeSpawn()
    const handler = createPruefungHandler(spawn)

    await handler.execute(makeContext())

    expect(calls[0].prompt).not.toContain('CORRECTION REQUIRED')
  })

  it('prepends a correction block when a prior iteration had a validation error', async () => {
    const { spawn, calls } = fakeSpawn()
    const handler = createPruefungHandler(spawn)

    const ctx = makeContext({
      stageRun: { ...makeContext().stageRun, iteration: 1 },
      priorIterationOutput: {
        validation_error: 'missing required field: recommendation',
        rejected_output: { wellDefined: true },
      },
    })
    await handler.execute(ctx)

    expect(calls[0].prompt).toContain('CORRECTION REQUIRED')
    expect(calls[0].prompt).toContain('missing required field: recommendation')
    // Original prompt still follows the correction block.
    expect(calls[0].prompt).toContain('Feasibility Check')
  })

  it('records an audit entry with the spawned pid and iteration', async () => {
    const { spawn } = fakeSpawn(7777)
    const handler = createPruefungHandler(spawn)
    const audits: Array<{ action: string, details?: Record<string, unknown> }> = []

    const ctx = makeContext({
      recordAudit: (action, details) => audits.push({ action, details }),
    })
    await handler.execute(ctx)

    expect(audits).toHaveLength(1)
    expect(audits[0].action).toBe('pruefung_spawned')
    expect(audits[0].details).toMatchObject({ pid: 7777, iteration: 0, hasFeedback: false })
  })
})
