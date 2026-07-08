import { describe, expect, it } from 'vitest'

import { detectQuestion } from '../askQuestionScreen'
import multi from './fixtures/askq-multi.txt?raw'
import nonModal from './fixtures/askq-nonmodal.txt?raw'
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

  it('returns null for an ordinary numbered list with a "Type something" line but no "Chat about this" row', () => {
    const raw = [
      'To add an icon:',
      '1. Click icon',
      '2. Type something',
      '3. Press enter',
    ]
    expect(detectQuestion(raw)).toBeNull()
  })

  it('keeps multiSelect false when question/description contain toggle wording but options have no checkboxes', () => {
    const raw = [
      'Would you like to toggle X?',
      '',
      '1. Yes',
      '   press space to confirm later',
      '2. No',
      '3. Type something',
      '4. Chat about this',
    ]
    const q = detectQuestion(raw)!
    expect(q).not.toBeNull()
    expect(q.multiSelect).toBe(false)
  })

  it('holds the index invariant for a 1-option modal', () => {
    const raw = [
      'Confirm',
      'Proceed?',
      '1. Yes',
      '2. Type something',
      '3. Chat about this',
    ]
    const q = detectQuestion(raw)!
    expect(q).not.toBeNull()
    expect(q.options.length).toBe(1)
    expect(q.typeSomethingIndex).toBe(q.options.length + 1)
    expect(q.chatAboutIndex).toBe(q.options.length + 2)
  })

  it('holds the index invariant for a 5-option modal', () => {
    const raw = [
      'Pick one',
      'Which number?',
      '1. One',
      '2. Two',
      '3. Three',
      '4. Four',
      '5. Five',
      '6. Type something',
      '7. Chat about this',
    ]
    const q = detectQuestion(raw)!
    expect(q).not.toBeNull()
    expect(q.options.length).toBe(5)
    expect(q.typeSomethingIndex).toBe(6)
    expect(q.chatAboutIndex).toBe(7)
    expect(q.typeSomethingIndex).toBe(q.options.length + 1)
    expect(q.chatAboutIndex).toBe(q.options.length + 2)
  })

  it('rejects a frame whose description line is numbered (index desync)', () => {
    const raw = [
      'Pick a colour',
      'What is your favourite colour?',
      '1. Red',
      '   2. items match',
      '2. Green',
      '3. Type something',
      '4. Chat about this',
    ]
    expect(detectQuestion(raw)).toBeNull()
  })

  it('returns null for a realistic non-modal terminal buffer', () => {
    expect(detectQuestion(nonModal.split('\n'))).toBeNull()
  })

  it('detects multi-select from checkboxes alone with no footer toggle hint', () => {
    const raw = [
      'Pick fruits',
      'Which fruits?',
      '1. [ ] Apples',
      '2. [✔] Bananas',
      '3. Type something',
      '4. Chat about this',
    ]
    const q = detectQuestion(raw)!
    expect(q).not.toBeNull()
    expect(q.multiSelect).toBe(true)
  })

  it('falls back to footer toggle hint when no options carry checkboxes', () => {
    const raw = [
      'Pick fruits',
      'Which fruits?',
      '1. Apples',
      '2. Bananas',
      '3. Type something',
      '4. Chat about this',
      'Space to toggle · Enter to confirm',
    ]
    const q = detectQuestion(raw)!
    expect(q).not.toBeNull()
    expect(q.multiSelect).toBe(true)
  })

  it('does not flip multiSelect when only some options carry checkboxes', () => {
    const raw = [
      'Pick fruits',
      'Which fruits?',
      '1. [ ] Apples',
      '2. Bananas',
      '3. Type something',
      '4. Chat about this',
    ]
    const q = detectQuestion(raw)!
    expect(q).not.toBeNull()
    expect(q.multiSelect).toBe(false)
  })
})
