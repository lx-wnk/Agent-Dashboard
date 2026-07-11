import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

let useOnboardingMod: typeof import('./useOnboarding')

beforeEach(async () => {
  vi.resetModules()
  useOnboardingMod = await import('./useOnboarding')
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useOnboarding', () => {
  it('fetchStatus loads /api/onboarding/status into status', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: () => Promise.resolve({ completed: false, cliInstalled: true, cliVersion: '1.2.3', mcpRegistered: false }),
    }))

    const { status, fetchStatus } = useOnboardingMod.useOnboarding()
    await fetchStatus()

    expect(fetch).toHaveBeenCalledWith('/api/onboarding/status')
    expect(status.value).toEqual({ completed: false, cliInstalled: true, cliVersion: '1.2.3', mcpRegistered: false })
  })

  it('fetchStatus surfaces an error message on a non-ok response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 500 }))

    const { status, error, fetchStatus } = useOnboardingMod.useOnboarding()
    await fetchStatus()

    expect(status.value).toBeNull()
    expect(error.value).toMatch(/500/)
  })

  it('registerMcp POSTs to /api/onboarding/register-mcp and returns the result', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ ok: true, command: 'claude mcp add --scope user --transport http dashboard http://localhost:1234/mcp --header "Authorization: Bearer tok"' }),
    }))

    const { registerMcp } = useOnboardingMod.useOnboarding()
    const result = await registerMcp()

    expect(fetch).toHaveBeenCalledWith('/api/onboarding/register-mcp', { method: 'POST' })
    expect(result?.ok).toBe(true)
    expect(result?.command).toContain('claude mcp add')
  })

  it('registerMcp merges mcpRegistered into the existing status', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ completed: false, cliInstalled: true, cliVersion: '1.2.3', mcpRegistered: false }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ ok: true, command: 'claude mcp add ...' }),
      })
    vi.stubGlobal('fetch', fetchMock)

    const { status, fetchStatus, registerMcp } = useOnboardingMod.useOnboarding()
    await fetchStatus()
    await registerMcp()

    expect(status.value?.mcpRegistered).toBe(true)
    expect(status.value?.cliInstalled).toBe(true)
  })

  it('registerMcp surfaces the server error message on a non-ok response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      json: () => Promise.resolve({ error: 'claude CLI not found' }),
    }))

    const { registerMcp, error } = useOnboardingMod.useOnboarding()
    const result = await registerMcp()

    expect(result).toBeNull()
    expect(error.value).toBe('claude CLI not found')
  })

  it('complete PATCHes /api/onboarding/status with completed:true', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve({}) }))

    const { complete } = useOnboardingMod.useOnboarding()
    const ok = await complete()

    expect(fetch).toHaveBeenCalledWith('/api/onboarding/status', {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ completed: true }),
    })
    expect(ok).toBe(true)
  })

  it('complete marks the existing status as completed', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ completed: false, cliInstalled: true, cliVersion: '1.2.3', mcpRegistered: true }),
      })
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) })
    vi.stubGlobal('fetch', fetchMock)

    const { status, fetchStatus, complete } = useOnboardingMod.useOnboarding()
    await fetchStatus()
    await complete()

    expect(status.value?.completed).toBe(true)
  })
})
