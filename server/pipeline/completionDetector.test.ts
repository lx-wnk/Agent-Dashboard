import type { StageRun } from '../../src/types.js'
import { describe, expect, it } from 'vitest'
import { detectCompletion, validateStageOutput } from './completionDetector.js'

function makeRun(overrides: Partial<StageRun> = {}): StageRun {
  return {
    id: 'run-1',
    taskId: 'task-1',
    stage: 'pruefung',
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

const validPruefung = {
  wellDefined: true,
  risks: ['r1'],
  complexity: 'M',
  blockers: [],
  recommendation: 'proceed',
}

describe('validateStageOutput - pruefung', () => {
  it('accepts a well-formed payload', () => {
    expect(validateStageOutput('pruefung', validPruefung)).toEqual({ ok: true })
  })

  it('rejects missing recommendation', () => {
    const { recommendation, ...rest } = validPruefung
    const result = validateStageOutput('pruefung', rest)
    expect(result.ok).toBe(false)
    expect(result.error).toContain('recommendation')
  })

  it('rejects an unknown complexity enum value', () => {
    const result = validateStageOutput('pruefung', { ...validPruefung, complexity: 'medium' })
    expect(result.ok).toBe(false)
    expect(result.error).toContain('complexity')
  })

  it('rejects non-boolean wellDefined', () => {
    const result = validateStageOutput('pruefung', { ...validPruefung, wellDefined: 'yes' })
    expect(result.ok).toBe(false)
    expect(result.error).toContain('wellDefined')
  })
})

describe('validateStageOutput - other stages', () => {
  it('checks planning required fields', () => {
    expect(validateStageOutput('planning', { subtasks: [], acceptanceCriteria: [] }).ok).toBe(true)
    expect(validateStageOutput('planning', { subtasks: [] }).ok).toBe(false)
  })

  it('checks selbstreview required fields', () => {
    expect(validateStageOutput('selbstreview', {
      passed: true,
      findings: [],
      summary: 'ok',
    }).ok).toBe(true)
    expect(validateStageOutput('selbstreview', { passed: true, findings: [] }).ok).toBe(false)
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

  it('returns completed with output for a valid pruefung payload', async () => {
    const run = makeRun()
    const result = await detectCompletion(run, '/cwd', {
      isPidAlive: () => false,
      readOutput: async () => validPruefung,
    })
    expect(result.kind).toBe('completed')
    expect(result.output).toEqual(validPruefung)
  })

  it('returns failed WITH output when the payload parses but schema is wrong', async () => {
    const run = makeRun()
    const badOutput = { ...validPruefung, recommendation: 'maybe' }
    const result = await detectCompletion(run, '/cwd', {
      isPidAlive: () => false,
      readOutput: async () => badOutput,
    })
    expect(result.kind).toBe('failed')
    expect(result.output).toEqual(badOutput)
    expect(result.error).toContain('recommendation')
  })

  it('returns failed WITHOUT output when no session could be located', async () => {
    const run = makeRun({ sessionId: null })
    const result = await detectCompletion(run, '/cwd', {
      isPidAlive: () => false,
      findSessionId: async () => null,
    })
    expect(result.kind).toBe('failed')
    expect(result.output).toBeUndefined()
    expect(result.error).toContain('session')
  })

  it('returns failed WITHOUT output when the session exists but has no parseable json', async () => {
    const run = makeRun()
    const result = await detectCompletion(run, '/cwd', {
      isPidAlive: () => false,
      readOutput: async () => null,
    })
    expect(result.kind).toBe('failed')
    expect(result.output).toBeUndefined()
    expect(result.error).toContain('parseable')
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
      readOutput: async () => validPruefung,
    })
    expect(persisted).toEqual({ id: 'run-1', sessionId: 'discovered-sess' })
  })
})
