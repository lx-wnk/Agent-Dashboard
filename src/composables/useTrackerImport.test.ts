import { afterEach, describe, expect, it, vi } from 'vitest'
import { useTrackerImport } from './useTrackerImport'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

const mockIssue = {
  tracker: 'github',
  key: '#1',
  title: 'Fix the bug',
  body: 'Details here',
  url: 'https://github.com/owner/repo/issues/1',
  labels: ['bug'],
}

describe('useTrackerImport', () => {
  it('posts ref with Origin header and returns issue', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => mockIssue }))
    const { fetchIssue } = useTrackerImport()
    const iss = await fetchIssue('https://github.com/owner/repo/issues/1')
    expect(iss).toEqual(mockIssue)
    expect(fetch).toHaveBeenCalledWith('/api/tracker/fetch', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ ref: 'https://github.com/owner/repo/issues/1' }),
    }))
    const call = (fetch as ReturnType<typeof vi.fn>).mock.calls[0][1] as RequestInit
    expect((call.headers as Record<string, string>).Origin).toBe(globalThis.location.origin)
  })

  it('throws with error message on http error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      json: async () => ({ error: 'issue not found' }),
    }))
    const { fetchIssue } = useTrackerImport()
    await expect(fetchIssue('#999')).rejects.toThrow('issue not found')
  })

  it('throws with fallback message when no json body', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 502,
      json: async () => { throw new Error('parse error') },
    }))
    const { fetchIssue } = useTrackerImport()
    await expect(fetchIssue('X')).rejects.toThrow('HTTP')
  })
})
