import { describe, expect, it } from 'vitest'
import { shortId } from '../useCopyId'

describe('shortId', () => {
  it('returns the first 8 characters of a UUID', () => {
    expect(shortId('812f85f4-aaaa-bbbb-cccc-dddddddddddd')).toBe('812f85f4')
  })

  it('returns the whole string when shorter than 8 chars', () => {
    expect(shortId('abc')).toBe('abc')
  })

  it('returns empty string for an empty input', () => {
    expect(shortId('')).toBe('')
  })
})
