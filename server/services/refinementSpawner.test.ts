import { describe, it, expect } from 'vitest'
import { serializeHistory, REFINEMENT_SYSTEM_PROMPT } from './refinementSpawner'
import type { RefinementTurn } from '../db/refinementTurnsRepo'

describe('serializeHistory', () => {
  it('returns empty string for no history', () => {
    expect(serializeHistory([])).toBe('')
  })

  it('serializes turns with correct prefixes', () => {
    const turns: RefinementTurn[] = [
      { id: '1', taskId: 't', role: 'user', content: 'hello', phase: null, createdAt: '' },
      { id: '2', taskId: 't', role: 'assistant', content: 'hi there', phase: null, createdAt: '' },
    ]
    const result = serializeHistory(turns)
    expect(result).toContain('Human: hello')
    expect(result).toContain('Assistant: hi there')
  })
})

describe('REFINEMENT_SYSTEM_PROMPT', () => {
  it('mentions all four phases', () => {
    expect(REFINEMENT_SYSTEM_PROMPT).toContain('ANALYSE')
    expect(REFINEMENT_SYSTEM_PROMPT).toContain('SPEC')
    expect(REFINEMENT_SYSTEM_PROMPT).toContain('UMSETZUNGSKONZEPT')
    expect(REFINEMENT_SYSTEM_PROMPT).toContain('APPROVAL')
  })

  it('includes __phase_done signal instructions', () => {
    expect(REFINEMENT_SYSTEM_PROMPT).toContain('__phase_done:')
  })
})
