import { describe, expect, it } from 'vitest'

import { detectConfirmScreen, detectQuestion, screenSignature } from '../askQuestionScreen'
import confirmScreen from './fixtures/askq-confirm.txt?raw'
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

  it('detects the v2.1.205 render whose meta-row reads "Type something." with a trailing period', () => {
    // Captured from a real Claude Code v2.1.205 pty (the meta-row copy gained a
    // trailing period vs v2.1.197), plus per-option descriptions and a border
    // line between the last two meta-rows.
    const raw = [
      ' ☐ Colour ',
      '',
      'Which colour do you prefer?',
      '❯ 1. Red',
      '     Warm, high-energy hue.',
      '  2. Green',
      'Calm, natural hue.',
      ' 3. Blue',
      'Cool, steady hue.',
      '  4. Type something.',
      '────────────────────────────────────',
      '  5. Chat about this',
      'Enter to select · ↑/↓ to navigate · Esc to cancel',
    ]
    const q = detectQuestion(raw)!
    expect(q).not.toBeNull()
    expect(q.multiSelect).toBe(false)
    expect(q.options.map(o => o.label)).toEqual(['Red', 'Green', 'Blue'])
    expect(q.typeSomethingIndex).toBe(4)
    expect(q.chatAboutIndex).toBe(5)
    expect(q.options.find(o => o.label === 'Red')?.description).toBe('Warm, high-energy hue.')
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

describe('detectConfirmScreen', () => {
  it('detects the review/submit screen from a real render', () => {
    const c = detectConfirmScreen(confirmScreen.split('\n'))!
    expect(c).not.toBeNull()
    expect(c.question).toBe('Ready to submit your answers?')
    expect(c.options).toEqual([
      { index: 1, label: 'Submit answers' },
      { index: 2, label: 'Cancel' },
    ])
  })

  // The confirm screen is exactly what detectQuestion is built to reject (no
  // meta-rows); asserting it here pins WHY the second detector has to exist.
  it('is rejected by detectQuestion', () => {
    expect(detectQuestion(confirmScreen.split('\n'))).toBeNull()
  })

  it('tolerates copy drift in the submit label', () => {
    const c = detectConfirmScreen(['Ready to submit your answers?', '❯ 1. Submit', '  2. Cancel'])
    expect(c).not.toBeNull()
  })

  it('rejects a real question modal', () => {
    expect(detectConfirmScreen(single.split('\n'))).toBeNull()
  })

  it('rejects an ordinary two-item numbered list', () => {
    expect(detectConfirmScreen(['Files changed:', '1. server/main.go', '2. README.md'])).toBeNull()
  })

  it('rejects a submit/cancel pair with no preamble', () => {
    expect(detectConfirmScreen(['❯ 1. Submit answers', '  2. Cancel'])).toBeNull()
  })

  it('rejects a two-option modal that still carries meta-rows', () => {
    expect(detectConfirmScreen([
      'Submit?',
      '1. Submit answers',
      '2. Cancel',
      '3. Type something',
      '4. Chat about this',
    ])).toBeNull()
  })
})

describe('screenSignature', () => {
  it('is stable across re-parsing the same screen', () => {
    const a = detectQuestion(single.split('\n'))
    const b = detectQuestion(single.split('\n'))
    expect(a).not.toBe(b)
    expect(screenSignature(a)).toBe(screenSignature(b))
  })

  it('differs between two different screens', () => {
    const singleSig = screenSignature(detectQuestion(single.split('\n')))
    expect(singleSig).not.toBe(screenSignature(detectQuestion(multi.split('\n'))))
  })

  it('never collides a question with a confirm screen', () => {
    const confirmSig = screenSignature(detectConfirmScreen(confirmScreen.split('\n')))
    expect(confirmSig).not.toBe(screenSignature(detectQuestion(single.split('\n'))))
  })

  it('maps null to null', () => {
    expect(screenSignature(null)).toBeNull()
  })
})

describe('border bleed', () => {
  // A borderless v2.1.220 render can bleed the modal's right border into a
  // content line; it must not end up in the question text.
  it('strips a trailing box-drawing run from the question', () => {
    const q = detectQuestion([
      'Welches Tier soll es sein?─────────────────────────────────────────────╯',
      '❯ 1. Katze',
      '  2. Hund',
      '  3. Type something.',
      '  4. Chat about this',
    ])!
    expect(q).not.toBeNull()
    expect(q.question).toBe('Welches Tier soll es sein?')
    expect(q.options[0].label).toBe('Katze')
  })

  it('keeps a label that legitimately ends in an ASCII hyphen', () => {
    const q = detectQuestion([
      'Which flag?',
      '❯ 1. --dry-run',
      '  2. Type something.',
      '  3. Chat about this',
    ])!
    expect(q.options[0].label).toBe('--dry-run')
  })
})

describe('confirm screen tolerance', () => {
  it('ignores unrelated numbered lines above the modal', () => {
    const c = detectConfirmScreen([
      'Files changed:',
      '1. server/main.go',
      '2. README.md',
      'Ready to submit your answers?',
      '❯ 1. Submit answers',
      '  2. Cancel',
    ])!
    expect(c).not.toBeNull()
    expect(c.question).toBe('Ready to submit your answers?')
  })

  it('rejects a non-adjacent submit/cancel pair', () => {
    expect(detectConfirmScreen([
      'Ready to submit your answers?',
      '1. Submit answers',
      'some unrelated line',
      '2. Cancel',
    ])).toBeNull()
  })
})
