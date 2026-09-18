import { afterEach, describe, expect, it, vi } from 'vitest'

afterEach(() => {
  vi.resetModules()
  vi.unstubAllGlobals()
})

async function loadWith(fetchImpl: typeof fetch) {
  vi.stubGlobal('fetch', fetchImpl)
  const { useBuildVersion } = await import('../useBuildVersion')
  return useBuildVersion()
}

describe('useBuildVersion', () => {
  it('reads the version the server reports', async () => {
    const { version } = await loadWith(vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ status: 'ok', version: 'v1.4.0' }),
    }) as unknown as typeof fetch)

    await vi.waitFor(() => expect(version.value).toBe('v1.4.0'))
  })

  // Showing nothing beats showing a version that is not the running one.
  it('stays empty when the server answers without a version', async () => {
    const { version } = await loadWith(vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ status: 'ok' }),
    }) as unknown as typeof fetch)

    await new Promise(r => setTimeout(r, 0))
    expect(version.value).toBeNull()
  })

  it('stays empty when the request fails', async () => {
    const { version } = await loadWith(vi.fn().mockRejectedValue(new Error('offline')) as unknown as typeof fetch)

    await new Promise(r => setTimeout(r, 0))
    expect(version.value).toBeNull()
  })

  it('fetches once no matter how many components ask', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ version: 'v1' }) })
    vi.stubGlobal('fetch', fetchMock as unknown as typeof fetch)
    const { useBuildVersion } = await import('../useBuildVersion')

    useBuildVersion()
    useBuildVersion()
    useBuildVersion()

    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('surfaces sourceVersion and stale when the server reports them', async () => {
    const { sourceVersion, stale } = await loadWith(vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ status: 'ok', version: 'v1.4.0', sourceVersion: 'a1b2c3d4', stale: true }),
    }) as unknown as typeof fetch)

    await vi.waitFor(() => expect(stale.value).toBe(true))
    expect(sourceVersion.value).toBe('a1b2c3d4')
  })

  it('reports not stale when the server says so', async () => {
    const { stale } = await loadWith(vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ status: 'ok', version: 'v1.4.0', sourceVersion: 'v1.4.0', stale: false }),
    }) as unknown as typeof fetch)

    await new Promise(r => setTimeout(r, 0))
    expect(stale.value).toBe(false)
  })

  // An older server that has never heard of these fields must render exactly
  // like today: no source version, not stale.
  it('does not throw and defaults to not-stale when the server omits both fields', async () => {
    const { sourceVersion, stale } = await loadWith(vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ status: 'ok', version: 'v1.4.0' }),
    }) as unknown as typeof fetch)

    await new Promise(r => setTimeout(r, 0))
    expect(sourceVersion.value).toBeNull()
    expect(stale.value).toBe(false)
  })
})
