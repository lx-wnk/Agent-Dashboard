import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

let mod: typeof import('../useConfigExplorer')

// fetch stub matched by URL + method (order-independent), since the singleton
// fires the enumeration endpoints on first useConfigExplorer() call. `file`
// overrides the /api/config/file branch per test.
function makeFetch(file: (url: string, init?: RequestInit) => any) {
  return vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    if (url.startsWith('/api/config/file'))
      return file(url, init)
    if (url.startsWith('/api/config/skills'))
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ skills: [] }) })
    if (url.startsWith('/api/config/commands'))
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ commands: [] }) })
    if (url.startsWith('/api/config/memory'))
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ memory: [] }) })
    throw new Error(`unexpected ${init?.method ?? 'GET'} ${url}`)
  })
}

function install(file: (url: string, init?: RequestInit) => any) {
  vi.stubGlobal('fetch', makeFetch(file))
}

beforeEach(async () => {
  install(() => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({}) }))
  vi.resetModules()
  mod = await import('../useConfigExplorer')
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('useConfigExplorer file IO', () => {
  it('loadFile fetches content for a path and scope', async () => {
    install((url) => {
      expect(url).toContain('path=%2Fc%2FCLAUDE.md')
      expect(url).toContain('spawnerId=sp1')
      return Promise.resolve({
        ok: true,
        status: 200,
        json: () => Promise.resolve({ path: '/c/CLAUDE.md', content: 'hi', mtime: 42, editable: true, source: 'user' }),
      })
    })

    const { loadFile } = mod.useConfigExplorer()
    const f = await loadFile('/c/CLAUDE.md', 'sp1')
    expect(f.content).toBe('hi')
    expect(f.mtime).toBe(42)
    expect(f.source).toBe('user')
  })

  it('saveFile PUTs body and returns the new mtime/size', async () => {
    install((url, init) => {
      expect(url).toBe('/api/config/file?spawnerId=sp1')
      expect(init?.method).toBe('PUT')
      expect(JSON.parse(String(init?.body))).toEqual({ path: '/c/CLAUDE.md', content: 'new', baseMtime: 42 })
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ path: '/c/CLAUDE.md', mtime: 99, size: 3 }) })
    })

    const { saveFile } = mod.useConfigExplorer()
    const res = await saveFile('/c/CLAUDE.md', 'new', 42, 'sp1')
    expect(res.mtime).toBe(99)
    expect(res.size).toBe(3)
  })

  it('saveFile throws ConflictError on HTTP 409', async () => {
    install(() => Promise.resolve({ ok: false, status: 409, json: () => Promise.resolve({}) }))
    const { saveFile } = mod.useConfigExplorer()
    await expect(saveFile('/c/CLAUDE.md', 'x', 1)).rejects.toBeInstanceOf(mod.ConflictError)
  })

  it('saveFile throws a generic Error on other failures', async () => {
    install(() => Promise.resolve({ ok: false, status: 500, json: () => Promise.resolve({}) }))
    const { saveFile } = mod.useConfigExplorer()
    await expect(saveFile('/c/CLAUDE.md', 'x', 1)).rejects.toThrow('HTTP 500')
  })
})
