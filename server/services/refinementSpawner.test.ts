import type { RefinementTurn } from '../db/refinementTurnsRepo'
import { describe, expect, it } from 'vitest'
import { buildWindowedHistory, REFINEMENT_SYSTEM_PROMPT, serializeHistory } from './refinementSpawner'

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

  it('uses Human/Assistant role prefixes (not lowercase)', () => {
    const turns: RefinementTurn[] = [
      { id: '1', taskId: 'x', role: 'user', content: 'hello', phase: null, createdAt: '1' },
      { id: '2', taskId: 'x', role: 'assistant', content: 'world', phase: null, createdAt: '2' },
    ]
    const result = buildWindowedHistory(turns)
    expect(result).toContain('Human: hello')
    expect(result).toContain('Assistant: world')
    expect(result).not.toMatch(/^user:/m)
    expect(result).not.toMatch(/^assistant:/m)
  })

  it('keeps last 2 regular turns per phase group in candidate', () => {
    const turns: RefinementTurn[] = [
      // phase 1 group (3 regular turns) — last 2 should be kept
      { id: '1', taskId: 'x', role: 'user', content: 'p1-q1', phase: null, createdAt: '1' },
      { id: '2', taskId: 'x', role: 'assistant', content: 'p1-a1', phase: null, createdAt: '2' },
      { id: '3', taskId: 'x', role: 'user', content: 'p1-q2', phase: null, createdAt: '3' },
      // anchor
      { id: '4', taskId: 'x', role: 'assistant', content: 'ok\n__phase_done: analyse', phase: null, createdAt: '4' },
      // phase 2 group (2 regular turns) — both kept
      { id: '5', taskId: 'x', role: 'user', content: 'p2-q1', phase: null, createdAt: '5' },
      { id: '6', taskId: 'x', role: 'assistant', content: 'p2-a1', phase: null, createdAt: '6' },
    ]
    const result = buildWindowedHistory(turns)
    // anchor always present
    expect(result).toContain('__phase_done: analyse')
    // last 2 of phase 1 group: p1-a1, p1-q2 (p1-q1 dropped)
    expect(result).not.toContain('p1-q1')
    expect(result).toContain('p1-a1')
    expect(result).toContain('p1-q2')
    // both turns of phase 2 group kept
    expect(result).toContain('p2-q1')
    expect(result).toContain('p2-a1')
  })

  it('falls back to mustKeep (anchors + global last 2) when candidate exceeds maxChars', () => {
    const longContent = `x`.repeat(10_000)
    const turns: RefinementTurn[] = [
      // phase 1 group: 2 long regular turns. Both will be in candidate
      // (last 2 per phase group), making candidate exceed maxChars.
      { id: '1', taskId: 'x', role: 'user', content: longContent, phase: null, createdAt: '1' },
      { id: '2', taskId: 'x', role: 'assistant', content: longContent, phase: null, createdAt: '2' },
      // anchor (small)
      { id: '3', taskId: 'x', role: 'assistant', content: 'ok\n__phase_done: konzept', phase: null, createdAt: '3' },
      // phase 2 group: 2 long middle turns + 2 short final turns.
      // Per-group last 2 picks the short final turns, but phase 1's two long
      // turns are still in candidate -> candidate too large.
      { id: '4', taskId: 'x', role: 'user', content: longContent, phase: null, createdAt: '4' },
      { id: '5', taskId: 'x', role: 'assistant', content: longContent, phase: null, createdAt: '5' },
      { id: '6', taskId: 'x', role: 'user', content: 'last user', phase: null, createdAt: '6' },
      { id: '7', taskId: 'x', role: 'assistant', content: 'last answer', phase: null, createdAt: '7' },
    ]
    const result = buildWindowedHistory(turns, 5_000)
    // anchor always present
    expect(result).toContain('__phase_done: konzept')
    // global last 2 regular turns kept
    expect(result).toContain('last user')
    expect(result).toContain('last answer')
    // long middle turns must be excluded after mustKeep fallback fires
    expect(result).not.toContain(longContent.slice(0, 100))
    // result must use Human/Assistant prefixes
    expect(result).toContain('Human: last user')
    expect(result).toContain('Assistant: last answer')
    // result fits under the cap
    expect(result.length).toBeLessThanOrEqual(5_000)
  })
})

describe('rEFINEMENT_SYSTEM_PROMPT', () => {
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
