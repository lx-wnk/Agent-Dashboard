import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

let useUserMod: typeof import('../useUser')

beforeEach(async () => {
  vi.useFakeTimers()
  vi.resetModules()
  useUserMod = await import('../useUser')
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('useUser', () => {
  it('loads the current user on success', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ user: { id: 'u1', login: 'alex', isAdmin: true }, isAdmin: true, authEnabled: true }),
    }))

    const { user, isAdmin, authEnabled, loaded, loadUser } = useUserMod.useUser()
    await loadUser()

    expect(user.value).toEqual({ id: 'u1', login: 'alex', isAdmin: true })
    expect(isAdmin.value).toBe(true)
    expect(authEnabled.value).toBe(true)
    expect(loaded.value).toBe(true)
  })

  it('marks auth as enabled and schedules a retry on a non-ok response', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 503 })
    vi.stubGlobal('fetch', fetchMock)

    const { user, authEnabled, loaded, loadUser } = useUserMod.useUser()
    await loadUser()

    expect(authEnabled.value).toBe(true)
    expect(user.value).toBeNull()
    expect(loaded.value).toBe(true)
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(2000)
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('retries on a network error', async () => {
    const fetchMock = vi.fn().mockRejectedValue(new Error('offline'))
    vi.stubGlobal('fetch', fetchMock)

    const { loaded, loadUser } = useUserMod.useUser()
    await loadUser()

    expect(loaded.value).toBe(true)
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(2000)
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('clears a pending retry once a subsequent load succeeds', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: false, status: 503 })
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ user: null, isAdmin: true, authEnabled: false }) })
    vi.stubGlobal('fetch', fetchMock)

    const { loadUser } = useUserMod.useUser()
    await loadUser() // schedules a retry in 2s
    await loadUser() // manual retry succeeds immediately, clearing the timer

    expect(fetchMock).toHaveBeenCalledTimes(2)

    await vi.advanceTimersByTimeAsync(5000)
    expect(fetchMock).toHaveBeenCalledTimes(2) // no further auto-retry fires
  })
})
