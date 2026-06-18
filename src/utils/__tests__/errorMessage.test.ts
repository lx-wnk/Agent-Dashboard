import { describe, expect, it } from 'vitest'
import { errorMessage } from '../errorMessage'

describe('errorMessage', () => {
  it('returns the message of an Error instance', () => {
    expect(errorMessage(new Error('boom'))).toBe('boom')
  })

  it('returns the default fallback for non-Error values', () => {
    expect(errorMessage('nope')).toBe('Unknown error')
    expect(errorMessage(undefined)).toBe('Unknown error')
    expect(errorMessage({ message: 'fake' })).toBe('Unknown error')
  })

  it('returns a custom fallback when provided', () => {
    expect(errorMessage(42, 'Failed to load')).toBe('Failed to load')
  })

  it('preserves messages of Error subclasses', () => {
    expect(errorMessage(new TypeError('bad type'))).toBe('bad type')
  })
})
