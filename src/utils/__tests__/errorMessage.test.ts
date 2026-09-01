import { describe, expect, it } from 'vitest'
import { errorMessage, readErrorMessage } from '../errorMessage'

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

describe('readErrorMessage', () => {
  it('returns the error field the server sent', async () => {
    const res = new Response(JSON.stringify({ error: 'grant g1 allows 1 use per 60s' }), { status: 403 })

    expect(await readErrorMessage(res, 'fallback')).toBe('grant g1 allows 1 use per 60s')
  })

  it('falls back when the body is not JSON', async () => {
    const res = new Response('<html>502</html>', { status: 502 })

    expect(await readErrorMessage(res, 'fallback')).toBe('fallback')
  })

  it('falls back when the body is JSON without an error field', async () => {
    const res = new Response(JSON.stringify({ detail: 'nope' }), { status: 403 })

    expect(await readErrorMessage(res, 'fallback')).toBe('fallback')
  })
})
