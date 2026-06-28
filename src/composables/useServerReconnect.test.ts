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

  it('does not reload on a 200 before the server has been seen down', async () => {
    const reload = vi.fn()
    vi.stubGlobal('location', { reload } as any)
    const { stalled, beginReconnect } = useServerReconnect()
    // Old server still alive on first poll — must not reload
    ;(fetch as any).mockResolvedValue({ ok: true, status: 200 })
    beginReconnect()
    await vi.advanceTimersByTimeAsync(1_500)
    expect(reload).not.toHaveBeenCalled()
    expect(stalled.value).toBe(false)
  })

  it('reloads only after a failure has been seen (down→up gate)', async () => {
    const reload = vi.fn()
    vi.stubGlobal('location', { reload } as any)
    const { beginReconnect } = useServerReconnect()
    ;(fetch as any).mockRejectedValueOnce(new Error('down')) // first poll: server unreachable
    ;(fetch as any).mockResolvedValueOnce({ ok: true, status: 200 }) // second poll: back up
    beginReconnect()
    await vi.advanceTimersByTimeAsync(1_500) // first poll fails
    await vi.advanceTimersByTimeAsync(1_500) // second poll → reload
    expect(reload).toHaveBeenCalledOnce()
  })

  it('stalls after STALL_THRESHOLD consecutive failures', async () => {
    ;(fetch as any).mockRejectedValue(new Error('down'))
    const { stalled, beginReconnect } = useServerReconnect()
    beginReconnect()
    // 20 polls × 1500ms = 30s
    await vi.advanceTimersByTimeAsync(1_500 * 20)
    expect(stalled.value).toBe(true)
  })

  it('stops polling after stall threshold is reached', async () => {
    ;(fetch as any).mockRejectedValue(new Error('down'))
    const { stalled, beginReconnect } = useServerReconnect()
    beginReconnect()
    await vi.advanceTimersByTimeAsync(1_500 * 20)
    expect(stalled.value).toBe(true)
    const callsBefore = (fetch as any).mock.calls.length
    // No further polls should fire
    await vi.advanceTimersByTimeAsync(1_500 * 5)
    expect((fetch as any).mock.calls.length).toBe(callsBefore)
  })
})
