import { describe, expect, it } from 'vitest'

import { detectQuestion } from '../askQuestionScreen'
import multi from './fixtures/askq-multi.txt?raw'
import single from './fixtures/askq-single.txt?raw'

describe('detectQuestion', () => {
  it('parses single-select', () => {
    const q = detectQuestion(single.split('\n'))!
    expect(q).not.toBeNull()
    expect(q.multiSelect).toBe(false)
    expect(q.options.map(o => o.label)).toEqual(['Red', 'Green', 'Blue'])
    expect(q.typeSomethingIndex).toBe(4)
    expect(q.chatAboutIndex).toBe(5)
    expect(q.options.find(o => o.label === 'Red')?.description).toBe('A warm colour')
    expect(q.options.some(o => o.label === 'Type something')).toBe(false)
    expect(q.options.some(o => o.label === 'Chat about this')).toBe(false)
    expect(q.question).toBe('What is your favourite colour?')
  })

  it('parses multi-select checkboxes', () => {
    const q = detectQuestion(multi.split('\n'))!
    expect(q).not.toBeNull()
    expect(q.multiSelect).toBe(true)
    expect(q.options.length).toBe(3)
    expect(q.options.map(o => o.label)).toEqual(['Apples', 'Bananas', 'Cherries'])
    expect(q.typeSomethingIndex).toBe(4)
    expect(q.chatAboutIndex).toBe(5)
    expect(q.options.find(o => o.label === 'Apples')?.description).toBe('Crisp and sweet')
  })

  it('returns null when no modal is present', () => {
    expect(detectQuestion(['just a prompt', '> '])).toBeNull()
  })

  it('returns null when numbered rows exist but no Type something meta-row is present', () => {
    expect(detectQuestion(['1. Red', '2. Green', '3. Blue'])).toBeNull()
  })

  it('tolerates leading/trailing whitespace and missing box borders', () => {
    const raw = [
      '  Pick a colour  ',
      '',
      '  What is your favourite colour?  ',
      '',
      '  1. Red',
      '  2. Green',
      '  3. Type something',
      '  4. Chat about this',
    ]
    const q = detectQuestion(raw)!
    expect(q).not.toBeNull()
    expect(q.options.map(o => o.label)).toEqual(['Red', 'Green'])
    expect(q.multiSelect).toBe(false)
  })
})
