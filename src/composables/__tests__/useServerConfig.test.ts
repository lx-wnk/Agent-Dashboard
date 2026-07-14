import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

let useServerConfigMod: typeof import('../useServerConfig')

beforeEach(async () => {
  vi.resetModules()
  useServerConfigMod = await import('../useServerConfig')
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useServerConfig', () => {
  it('loads config from /api/config and marks loaded', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        mcpServerName: 'agent-dashboard',
        mcpEndpoint: '/api/mcp',
        homedir: '/home/me',
        scriptPath: '/opt/script.sh',
      }),
    }))

    const { mcpServerName, mcpEndpoint, homedir, scriptPath, loaded, loadServerConfig } = useServerConfigMod.useServerConfig()
    await loadServerConfig()

    expect(fetch).toHaveBeenCalledWith('/api/config')
    expect(mcpServerName.value).toBe('agent-dashboard')
    expect(mcpEndpoint.value).toBe('/api/mcp')
    expect(homedir.value).toBe('/home/me')
    expect(scriptPath.value).toBe('/opt/script.sh')
    expect(loaded.value).toBe(true)
  })

  it('defaults missing fields to empty strings', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve({}) }))

    const { mcpServerName, loaded, loadServerConfig } = useServerConfigMod.useServerConfig()
    await loadServerConfig()

    expect(mcpServerName.value).toBe('')
    expect(loaded.value).toBe(true)
  })

  it('leaves state unloaded on a non-ok response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 500 }))

    const { loaded, loadServerConfig } = useServerConfigMod.useServerConfig()
    await loadServerConfig()

    expect(loaded.value).toBe(false)
  })

  it('swallows network errors and leaves state unloaded', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')))

    const { loaded, loadServerConfig } = useServerConfigMod.useServerConfig()
    await loadServerConfig()

    expect(loaded.value).toBe(false)
  })

  it('does not refetch once already loaded', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve({ mcpServerName: 'x' }) })
    vi.stubGlobal('fetch', fetchMock)

    const { loadServerConfig } = useServerConfigMod.useServerConfig()
    await loadServerConfig()
    await loadServerConfig()

    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('dedupes concurrent calls into a single in-flight request', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve({ mcpServerName: 'x' }) })
    vi.stubGlobal('fetch', fetchMock)

    const { loadServerConfig } = useServerConfigMod.useServerConfig()
    await Promise.all([loadServerConfig(), loadServerConfig()])

    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})
