import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useRefinementChat } from '@/features/pipeline/composables/useRefinementChat'

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

  it('reloads turns when the chat is reopened for the same task', async () => {
    mockFetchOnce([{ role: 'user', content: 'hi', phase: null }])
    const chat = useRefinementChat(() => 'task-1')
    await chat.loadHistory()
    expect(chat.messages.value).toHaveLength(1)
    chat.messages.value = []
    mockFetchOnce([{ role: 'user', content: 'hi', phase: null }, { role: 'assistant', content: 'yo', phase: null }])
    await chat.loadHistory()
    expect(chat.messages.value).toHaveLength(2)
  })
})

describe('useRefinementChat.syncStatus (detached-run reconnect)', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows the working indicator and reloads turns when a detached run finishes', async () => {
    vi.useFakeTimers()
    // /status returns running first, then done; /turns returns the persisted
    // assistant turn after completion.
    let statusCalls = 0
    vi.stubGlobal('fetch', vi.fn((url: string) => {
      if (url.endsWith('/status')) {
        statusCalls++
        const status = statusCalls === 1 ? 'refining' : 'draft_ready'
        return Promise.resolve({ ok: true, status: 200, json: async () => ({ status }) })
      }
      // /turns
      return Promise.resolve({
        ok: true,
        status: 200,
        json: async () => [
          { role: 'user', content: 'build X', phase: null },
          { role: 'assistant', content: 'analysis…', phase: 'analysis' },
        ],
      })
    }))

    const chat = useRefinementChat(() => 'task-1')
    await chat.syncStatus()

    // running → indicator on, status surfaced
    expect(chat.isStreaming.value).toBe(true)
    expect(chat.runStatus.value).toBe('refining')

    // advance the poll loop → /status now done → reload persisted turns
    await vi.advanceTimersByTimeAsync(2000)

    expect(chat.runStatus.value).toBe('draft_ready')
    expect(chat.isStreaming.value).toBe(false)
    expect(chat.messages.value).toHaveLength(2)
    expect(chat.messages.value[1]).toMatchObject({ role: 'assistant', content: 'analysis…' })
  })

  it('stop() halts polling and aborts an in-flight stream', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('fetch', vi.fn((url: string) => {
      if (url.endsWith('/status'))
        return Promise.resolve({ ok: true, status: 200, json: async () => ({ status: 'refining' }) })
      return Promise.resolve({ ok: true, status: 200, json: async () => [] })
    }))

    const chat = useRefinementChat(() => 'task-1')
    await chat.syncStatus()
    expect(chat.isStreaming.value).toBe(true)

    chat.stop()
    expect(chat.isStreaming.value).toBe(false)

    // No further status polling happens after stop().
    const before = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls.length
    await vi.advanceTimersByTimeAsync(5000)
    expect((globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls.length).toBe(before)
  })
})

describe('useRefinementChat.sendMessage — line-break reconstruction', () => {
  function streamReader(frames: string) {
    const chunks = [new TextEncoder().encode(frames)]
    let i = 0
    return {
      read: async () => i < chunks.length
        ? { done: false, value: chunks[i++] }
        : { done: true, value: undefined },
    }
  }

  it('preserves newlines between data frames so markdown structure survives', async () => {
    // Backend emits ONE SSE frame per source line (the trailing \n is stripped by
    // bufio.Scanner). The client must re-insert the line breaks between frames,
    // otherwise headings/tables/lists collapse into a single line.
    const frames
      = 'data: ## Analysis\n\n'
        + 'data: S-01 resolved.\n\n'
        + 'data: \n\n' // blank source line → paragraph break
        + 'data: | ID | Fix |\n\n'
        + 'data: |----|-----|\n\n'
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      body: { getReader: () => streamReader(frames) },
    }))

    const chat = useRefinementChat(() => 'task-1')
    await chat.sendMessage('go')

    const content = chat.messages.value.at(-1)?.content ?? ''
    // Heading on its own line (renders as <h2>, not literal "##" mid-paragraph).
    expect(content).toContain('## Analysis\nS-01 resolved.')
    // Table rows on separate lines (renders as a table).
    expect(content).toContain('| ID | Fix |\n|----|-----|')
    // Blank source line preserved as a paragraph break.
    expect(content).toContain('S-01 resolved.\n\n| ID | Fix |')
  })

  it('reconstructs multiple data lines packed into a single frame', async () => {
    // The handler splits an embedded \n into several `data:` lines within ONE
    // frame; all of them must be read (not just the first) and rejoined.
    const frames = 'data: line one\ndata: line two\ndata: line three\n\n'
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      body: { getReader: () => streamReader(frames) },
    }))

    const chat = useRefinementChat(() => 'task-1')
    await chat.sendMessage('go')

    expect(chat.messages.value.at(-1)?.content).toBe('line one\nline two\nline three')
  })
})

it('parses a streamed __phase_done marker into completedPhases + approvalReady, hidden from content', async () => {
  const frames = 'data: Looks ready.\n\ndata: __phase_done: approval\n\n'
  const chunks = [new TextEncoder().encode(frames)]
  let i = 0
  const reader = {
    read: async () => i < chunks.length
      ? { done: false, value: chunks[i++] }
      : { done: true, value: undefined },
  }
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200, body: { getReader: () => reader } }))

  const chat = useRefinementChat(() => 'task-1')
  await chat.sendMessage('is it ready?')

  expect(chat.approvalReady.value).toBe(true)
  expect(chat.completedPhases.value.has('approval')).toBe(true)
  const assistant = chat.messages.value.at(-1)
  expect(assistant?.content).not.toContain('__phase_done')
  expect(assistant?.content).toContain('Looks ready.')
})
