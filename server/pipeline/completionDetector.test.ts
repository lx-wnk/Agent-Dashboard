import type { StageRun } from '../../src/types.js'
import { describe, expect, it } from 'vitest'
import { detectCompletion, validateStageOutput } from './completionDetector.js'

function makeRun(overrides: Partial<StageRun> = {}): StageRun {
  return {
    id: 'run-1',
    taskId: 'task-1',
    stage: 'selbstreview',
    sessionId: 'sess-1',
    sessionName: null,
    pid: 1234,
    status: 'running',
    startedAt: '2026-04-12T10:00:00Z',
    endedAt: null,
    iteration: 0,
    output: null,
    tokensUsed: 0,
    costCents: 0,
    ...overrides,
  }
}

const validSelbstreview = {
  passed: true,
  findings: ['f1'],
  summary: 'looks good',
}

describe('validateStageOutput - selbstreview', () => {
  it('accepts a well-formed payload', () => {
    expect(validateStageOutput('selbstreview', validSelbstreview)).toEqual({ ok: true })
  })

  it('rejects missing summary', () => {
    const { summary, ...rest } = validSelbstreview
    const result = validateStageOutput('selbstreview', rest)
    expect(result.ok).toBe(false)
    expect(result.error).toContain('summary')
  })

  it('rejects non-boolean passed', () => {
    const result = validateStageOutput('selbstreview', { ...validSelbstreview, passed: 'yes' })
    expect(result.ok).toBe(false)
    expect(result.error).toContain('passed')
  })

  it('rejects non-array findings', () => {
    const result = validateStageOutput('selbstreview', { ...validSelbstreview, findings: 'nope' })
    expect(result.ok).toBe(false)
    expect(result.error).toContain('findings')
  })
})

describe('validateStageOutput - other stages', () => {
  it('checks finalisierung required fields', () => {
    expect(validateStageOutput('finalisierung', {
      summary: 'done',
      insights: [],
      openTodos: [],
      testPlan: [],
    }).ok).toBe(true)
    expect(validateStageOutput('finalisierung', { summary: 'done' }).ok).toBe(false)
  })

  it('accepts anything for stages without a structured schema', () => {
    expect(validateStageOutput('backlog', { whatever: 1 }).ok).toBe(true)
    expect(validateStageOutput('umsetzung', {}).ok).toBe(true)
  })
})

describe('detectCompletion', () => {
  it('returns still_running while the pid is alive', async () => {
    const run = makeRun()
    const result = await detectCompletion(run, '/cwd', {
      isPidAlive: () => true,
    })
    expect(result.kind).toBe('still_running')
  })

  it('returns completed with output for a valid selbstreview payload', async () => {
    const run = makeRun()
    const result = await detectCompletion(run, '/cwd', {
      isPidAlive: () => false,
      readOutput: async () => ({ output: validSelbstreview, rawText: '```json\n{}\n```' }),
    })
    expect(result.kind).toBe('completed')
    expect(result.output).toEqual(validSelbstreview)
  })

  it('returns retryable failed WITH output when the payload parses but schema is wrong', async () => {
    const run = makeRun()
    const badOutput = { ...validSelbstreview, passed: 'maybe' }
    const result = await detectCompletion(run, '/cwd', {
      isPidAlive: () => false,
      readOutput: async () => ({ output: badOutput, rawText: '```json\n{}\n```' }),
    })
    expect(result.kind).toBe('failed')
    expect(result.retryable).toBe(true)
    expect(result.output).toEqual(badOutput)
    expect(result.error).toContain('passed')
  })

  it('returns failed WITHOUT output when no session could be located', async () => {
    const run = makeRun({ sessionId: null })
    const result = await detectCompletion(run, '/cwd', {
      isPidAlive: () => false,
      findSessionId: async () => null,
    })
    expect(result.kind).toBe('failed')
    expect(result.retryable).toBeFalsy()
    expect(result.output).toBeUndefined()
    expect(result.error).toContain('session')
  })

  it('returns failed WITHOUT output when the session has neither JSON nor assistant text', async () => {
    const run = makeRun()
    const result = await detectCompletion(run, '/cwd', {
      isPidAlive: () => false,
      readOutput: async () => ({ output: null, rawText: null }),
    })
    expect(result.kind).toBe('failed')
    expect(result.retryable).toBeFalsy()
    expect(result.output).toBeUndefined()
    expect(result.error).toContain('parseable')
  })

  it('returns non-retryable failed WITH agentMessage when the agent wrote prose but no JSON block', async () => {
    const run = makeRun()
    const prose = 'I need write permission to /some/path before I can continue.'
    const result = await detectCompletion(run, '/cwd', {
      isPidAlive: () => false,
      readOutput: async () => ({ output: null, rawText: prose }),
    })
    expect(result.kind).toBe('failed')
    expect(result.retryable).toBeFalsy()
    expect(result.output).toEqual({ agentMessage: prose })
    expect(result.error).toContain('```json')
  })

  it('truncates a very long agentMessage to the last AGENT_MESSAGE_MAX_CHARS', async () => {
    const run = makeRun()
    const longProse = `${'x'.repeat(5000)}END-MARKER`
    const result = await detectCompletion(run, '/cwd', {
      isPidAlive: () => false,
      readOutput: async () => ({ output: null, rawText: longProse }),
    })
    const msg = (result.output as { agentMessage: string } | undefined)?.agentMessage ?? ''
    expect(msg.endsWith('END-MARKER')).toBe(true)
    expect(msg.length).toBeLessThanOrEqual(2000)
  })

  it('persists a newly discovered sessionId via the injected persister', async () => {
    const run = makeRun({ sessionId: null })
    let persisted: { id: string, sessionId: string } | null = null
    await detectCompletion(run, '/cwd', {
      isPidAlive: () => false,
      findSessionId: async () => 'discovered-sess',
      persistSessionId: (id, sessionId) => {
        persisted = { id, sessionId }
      },
      readOutput: async () => ({ output: validSelbstreview, rawText: '```json\n{}\n```' }),
    })
    expect(persisted).toEqual({ id: 'run-1', sessionId: 'discovered-sess' })
  })
})
