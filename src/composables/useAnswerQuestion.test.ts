import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useAnswerQuestion } from './useAnswerQuestion'

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('useAnswerQuestion', () => {
  it('pOSTs to the correct URL with the right body shape', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ ok: true }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { submit } = useAnswerQuestion()
    const result = await submit(42, 'tu-1', [{ header: 'Choose', selected: ['Option A'] }])

    expect(result).toBe(true)
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/agents/42/answer-question',
      expect.objectContaining({
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          toolUseId: 'tu-1',
          answers: [{ header: 'Choose', selected: ['Option A'] }],
        }),
      }),
    )
  })

  it('returns true and sets sendStatus to sent on success', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ ok: true }),
    }))

    const { submit, sendStatus } = useAnswerQuestion()
    const result = await submit(1, 'tu-ok', [])

    expect(result).toBe(true)
    expect(sendStatus.value).toBe('sent')
  })

  it('returns false and sets sendError from response body on non-ok', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 409,
      json: async () => ({ error: 'stale question' }),
    }))

    const { submit, sendStatus, sendError } = useAnswerQuestion()
    const result = await submit(1, 'tu-stale', [])

    expect(result).toBe(false)
    expect(sendStatus.value).toBe('error')
    expect(sendError.value).toBe('stale question')
  })

  it('returns false and sets sendError on network failure', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')))

    const { submit, sendStatus, sendError } = useAnswerQuestion()
    const result = await submit(1, 'tu-net', [])

    expect(result).toBe(false)
    expect(sendStatus.value).toBe('error')
    expect(sendError.value).toBe('Failed to fetch')
  })
})
