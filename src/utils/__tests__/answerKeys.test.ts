import { describe, expect, it } from 'vitest'
import { encodeAnswer } from '../answerKeys'

describe('encodeAnswer', () => {
  it('single-select → digit', () => {
    expect(encodeAnswer({ mode: 'single', index: 0 })).toEqual(['1'])
  })

  it('single-select → digit at higher index', () => {
    expect(encodeAnswer({ mode: 'single', index: 4 })).toEqual(['5'])
  })

  it('multi-select → digits + Tab + Enter', () => {
    expect(encodeAnswer({ mode: 'multi', indices: [0, 2] })).toEqual(['1', '3', '\t', '\r'])
  })

  it('multi-select → single index still needs Tab + Enter', () => {
    expect(encodeAnswer({ mode: 'multi', indices: [1] })).toEqual(['2', '\t', '\r'])
  })

  it('custom → digit(len+1) + text + Enter', () => {
    expect(encodeAnswer({ mode: 'custom', optionCount: 2, text: 'Cherry' })).toEqual(['3', 'Cherry', '\r'])
  })

  it('custom → digit shifts with optionCount', () => {
    expect(encodeAnswer({ mode: 'custom', optionCount: 3, text: 'Cherry' })).toEqual(['4', 'Cherry', '\r'])
  })

  it('chat → digit(len+2) + text + Enter', () => {
    expect(encodeAnswer({ mode: 'chat', optionCount: 2, text: 'why?' })).toEqual(['4', 'why?', '\r'])
  })

  it('chat → digit shifts with optionCount', () => {
    expect(encodeAnswer({ mode: 'chat', optionCount: 3, text: 'why?' })).toEqual(['5', 'why?', '\r'])
  })
})
