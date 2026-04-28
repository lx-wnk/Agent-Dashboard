import { describe, it, expect } from 'vitest'
import { serializeHistory, buildWindowedHistory, REFINEMENT_SYSTEM_PROMPT } from './refinementSpawner'
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

describe('buildWindowedHistory', () => {
  it('returns empty string for empty turns', () => {
    expect(buildWindowedHistory([])).toBe('')
  })

  it('always includes __phase_done anchor turns', () => {
    const turns: RefinementTurn[] = [
      { id: '1', taskId: 'x', role: 'user', content: 'first user', phase: null, createdAt: '1' },
      { id: '2', taskId: 'x', role: 'assistant', content: 'ok\n__phase_done: konzept', phase: null, createdAt: '2' },
      { id: '3', taskId: 'x', role: 'user', content: 'second user', phase: null, createdAt: '3' },
      { id: '4', taskId: 'x', role: 'assistant', content: 'second answer', phase: null, createdAt: '4' },
    ]
    const result = buildWindowedHistory(turns)
    expect(result).toContain('__phase_done: konzept')
    expect(result).toContain('second user')
    expect(result).toContain('second answer')
  })

  it('truncates to mustKeep when over maxChars', () => {
    const longContent = 'x'.repeat(10_000)
    const turns: RefinementTurn[] = [
      { id: '1', taskId: 'x', role: 'user', content: longContent, phase: null, createdAt: '1' },
      { id: '2', taskId: 'x', role: 'assistant', content: 'ok\n__phase_done: konzept', phase: null, createdAt: '2' },
      { id: '3', taskId: 'x', role: 'user', content: longContent, phase: null, createdAt: '3' },
      { id: '4', taskId: 'x', role: 'assistant', content: longContent, phase: null, createdAt: '4' },
      { id: '5', taskId: 'x', role: 'user', content: 'last user', phase: null, createdAt: '5' },
      { id: '6', taskId: 'x', role: 'assistant', content: 'last answer', phase: null, createdAt: '6' },
    ]
    const result = buildWindowedHistory(turns, 5_000)
    // must always keep the anchor
    expect(result).toContain('__phase_done: konzept')
    // must always keep the last exchange
    expect(result).toContain('last user')
    expect(result).toContain('last answer')
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
