import { describe, expect, it } from 'vitest'
import { extractJsonBlock, lastAssistantText } from './sessionOutputReader.js'

describe('extractJsonBlock', () => {
  it('parses a fenced json block', () => {
    const text = 'Some prose.\n\n```json\n{"foo": 1, "bar": "x"}\n```\n\nMore prose.'
    expect(extractJsonBlock(text)).toEqual({ foo: 1, bar: 'x' })
  })

  it('returns null when no json block exists', () => {
    expect(extractJsonBlock('just text')).toBeNull()
  })

  it('returns null when the block contains invalid json', () => {
    const text = '```json\n{not valid}\n```'
    expect(extractJsonBlock(text)).toBeNull()
  })

  it('returns null when the json parses to a non-object (array, number)', () => {
    expect(extractJsonBlock('```json\n[1,2,3]\n```')).toBeNull()
    expect(extractJsonBlock('```json\n42\n```')).toBeNull()
  })

  it('extracts only the first block when multiple are present', () => {
    const text = '```json\n{"a": 1}\n```\n```json\n{"b": 2}\n```'
    expect(extractJsonBlock(text)).toEqual({ a: 1 })
  })
})

describe('lastAssistantText', () => {
  it('concatenates text blocks from the last assistant turn', () => {
    const entries = [
      { type: 'user', message: { role: 'user', content: [{ type: 'text', text: 'q' }] } },
      {
        type: 'assistant',
        message: {
          role: 'assistant',
          content: [
            { type: 'text', text: 'part 1' },
            { type: 'tool_use', text: undefined },
            { type: 'text', text: 'part 2' },
          ],
        },
      },
    ]
    expect(lastAssistantText(entries)).toBe('part 1\npart 2')
  })

  it('walks backward past tool-only assistant turns to find the last text turn', () => {
    const entries = [
      {
        type: 'assistant',
        message: { role: 'assistant', content: [{ type: 'text', text: 'earlier answer' }] },
      },
      {
        type: 'assistant',
        message: { role: 'assistant', content: [{ type: 'tool_use' }] },
      },
    ]
    // lastAssistantText returns the text of the most recent assistant turn
    // WITH text content, which is the second-to-last entry here.
    expect(lastAssistantText(entries)).toBe('earlier answer')
  })

  it('returns null when no assistant turn has text', () => {
    const entries = [
      { type: 'user', message: { role: 'user', content: [{ type: 'text', text: 'q' }] } },
    ]
    expect(lastAssistantText(entries)).toBeNull()
  })
})
