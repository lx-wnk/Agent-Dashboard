import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useRefinementChat } from '../useRefinementChat'

// Contract guard: GET /api/refine/{taskId}/turns returns a BARE ARRAY of turns.
// A previous frontend revision expected { turns, isProcessing } and threw
// "Failed to load history" on a fresh ticket (data.turns was undefined). These
// tests lock the array contract so the two sides cannot silently diverge again.

function mockFetchOnce(value: unknown, ok = true, status = 200) {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok,
    status,
    json: async () => value,
  }))
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('useRefinementChat.loadHistory', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })

  it('loads a bare array of turns into messages', async () => {
    mockFetchOnce([
      { role: 'user', content: 'build X', phase: null },
      { role: 'assistant', content: 'analysis…', phase: 'analysis' },
    ])

    const chat = useRefinementChat(() => 'task-1')
    await chat.loadHistory()

    expect(chat.error.value).toBeNull()
    expect(chat.messages.value).toHaveLength(2)
    expect(chat.messages.value[0]).toMatchObject({ role: 'user', content: 'build X' })
    expect(chat.messages.value[1]).toMatchObject({ role: 'assistant', phase: 'analysis' })
  })

  it('handles an empty array (fresh ticket) without erroring', async () => {
    mockFetchOnce([])

    const chat = useRefinementChat(() => 'concept-1780160247158')
    await chat.loadHistory()

    expect(chat.error.value).toBeNull()
    expect(chat.messages.value).toHaveLength(0)
  })

  it('surfaces an error when the request is not ok', async () => {
    mockFetchOnce({}, false, 500)

    const chat = useRefinementChat(() => 'task-1')
    await chat.loadHistory()

    expect(chat.error.value).toBe('Failed to load history')
  })

  it('renders a plain-text SSE frame (claude -p default output) as assistant content', async () => {
    // Backend forwards `claude -p` output as `data: <plain text>` frames — NOT JSON.
    // The parser must fall back to treating the raw line as assistant text rather
    // than discarding it on a JSON.parse failure (which left the reply blank).
    const chunks = [new TextEncoder().encode('data: Here is the analysis.\n\n')]
    let i = 0
    const reader = {
      read: async () => i < chunks.length
        ? { done: false, value: chunks[i++] }
        : { done: true, value: undefined },
    }
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      body: { getReader: () => reader },
    }))

    const chat = useRefinementChat(() => 'task-1')
    await chat.sendMessage('hi')

    const assistant = chat.messages.value.at(-1)
    expect(assistant?.role).toBe('assistant')
    expect(assistant?.content).toBe('Here is the analysis.')
    expect(chat.error.value).toBeNull()
  })

  it('no-ops when no task id is set', async () => {
    const fetchSpy = vi.fn()
    vi.stubGlobal('fetch', fetchSpy)

    const chat = useRefinementChat(() => null)
    await chat.loadHistory()

    expect(fetchSpy).not.toHaveBeenCalled()
    expect(chat.error.value).toBeNull()
  })
})
