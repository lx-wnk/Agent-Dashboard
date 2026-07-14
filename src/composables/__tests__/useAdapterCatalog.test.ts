import type { AdapterMeta } from '@/types'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

let useAdapterCatalogMod: typeof import('../useAdapterCatalog')

function makeAdapter(overrides: Partial<AdapterMeta> = {}): AdapterMeta {
  return { name: 'claude', description: 'Claude Code', configKeys: [], ...overrides }
}

beforeEach(() => {
  vi.resetModules()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useAdapterCatalog', () => {
  it('loads the catalog on first use', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve([makeAdapter()]) }))
    useAdapterCatalogMod = await import('../useAdapterCatalog')

    const { catalog, isLoading, reload } = useAdapterCatalogMod.useAdapterCatalog()
    await reload()

    expect(catalog.value).toEqual([makeAdapter()])
    expect(isLoading.value).toBe(false)
  })

  it('getByType finds an adapter by name', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve([makeAdapter({ name: 'claude' }), makeAdapter({ name: 'ollama' })]),
    }))
    useAdapterCatalogMod = await import('../useAdapterCatalog')

    const { getByType, reload } = useAdapterCatalogMod.useAdapterCatalog()
    await reload()

    expect(getByType('ollama')?.name).toBe('ollama')
  })

  it('getByType returns undefined for a type not in the catalog', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve([makeAdapter({ name: 'claude' })]) }))
    useAdapterCatalogMod = await import('../useAdapterCatalog')

    const { getByType, reload } = useAdapterCatalogMod.useAdapterCatalog()
    await reload()

    expect(getByType('openai')).toBeUndefined()
  })

  it('dedupes concurrent loads behind a single in-flight fetch', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve([makeAdapter()]) })
    vi.stubGlobal('fetch', fetchMock)
    useAdapterCatalogMod = await import('../useAdapterCatalog')

    useAdapterCatalogMod.useAdapterCatalog()
    const { catalog, reload } = useAdapterCatalogMod.useAdapterCatalog()
    await reload()

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(catalog.value).toHaveLength(1)
  })

  it('does not refetch once the catalog has been populated', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve([makeAdapter()]) })
    vi.stubGlobal('fetch', fetchMock)
    useAdapterCatalogMod = await import('../useAdapterCatalog')

    const { reload } = useAdapterCatalogMod.useAdapterCatalog()
    await reload()
    useAdapterCatalogMod.useAdapterCatalog() // second call site, catalog already populated

    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('sets an error on a non-ok response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 500 }))
    useAdapterCatalogMod = await import('../useAdapterCatalog')

    const { error, catalog, reload } = useAdapterCatalogMod.useAdapterCatalog()
    await reload()

    expect(error.value).toBe('HTTP 500')
    expect(catalog.value).toEqual([])
  })

  it('sets an error message when the request throws', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')))
    useAdapterCatalogMod = await import('../useAdapterCatalog')

    const { error, reload } = useAdapterCatalogMod.useAdapterCatalog()
    await reload()

    expect(error.value).toBe('offline')
  })
})
