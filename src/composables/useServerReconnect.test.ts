import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { _resetForTesting, useServerReconnect } from './useServerReconnect'

describe('useServerReconnect', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.stubGlobal('fetch', vi.fn())
  })
  afterEach(() => {
    _resetForTesting()
    vi.useRealTimers()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('triggerRestart posts and starts reconnecting on 202', async () => {
    ;(fetch as any).mockResolvedValueOnce({ ok: true, status: 202, json: async () => ({ status: 'restarting' }) })
    const { isReconnecting, triggerRestart } = useServerReconnect()
    await triggerRestart()
    expect(fetch).toHaveBeenCalledWith('/api/admin/restart', expect.objectContaining({ method: 'POST' }))
    expect(isReconnecting.value).toBe(true)
  })

  it('triggerRestart on non-2xx does not start reconnecting and throws', async () => {
    ;(fetch as any).mockResolvedValueOnce({ ok: false, status: 409, json: async () => ({ error: 'lockout' }) })
    const { isReconnecting, triggerRestart } = useServerReconnect()
    await expect(triggerRestart()).rejects.toThrow()
    expect(isReconnecting.value).toBe(false)
  })

  it('polls health and reloads on first 200', async () => {
    const reload = vi.fn()
    vi.stubGlobal('location', { reload } as any)
    const { isReconnecting, beginReconnect } = useServerReconnect()
    ;(fetch as any).mockRejectedValueOnce(new Error('down')) // first poll: server still down
    ;(fetch as any).mockResolvedValueOnce({ ok: true, status: 200 }) // second poll: back
    beginReconnect()
    expect(isReconnecting.value).toBe(true)
    await vi.advanceTimersByTimeAsync(1_500) // first poll fails
    await vi.advanceTimersByTimeAsync(1_500) // second poll → reload
    expect(reload).toHaveBeenCalledOnce()
  })
})
