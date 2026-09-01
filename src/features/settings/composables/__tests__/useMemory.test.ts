import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'

let useMemory: typeof import('@/features/settings/composables/useMemory').useMemory

function withSetup<T>(composable: () => T) {
  let result!: T
  const Wrapper = defineComponent({
    setup() {
      result = composable()
      return {}
    },
    template: '<div />',
  })
  const wrapper = mount(Wrapper)
  return { result, wrapper }
}

function jsonResponse(status: number, body: unknown): Response {
  return { ok: status >= 200 && status < 300, status, json: () => Promise.resolve(body) } as Response
}

function fetchUrls(): string[] {
  return vi.mocked(globalThis.fetch).mock.calls.map(c => String(c[0]))
}

beforeEach(async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse(200, [])))
  vi.resetModules()
  const mod = await import('@/features/settings/composables/useMemory')
  useMemory = mod.useMemory
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useMemory', () => {
  // Same regression shape as useResources.held: the fetch guard and the
  // display state must be the one computed, not two conditions that happen
  // to agree today.
  it('holds the request when a non-global scope has a blank ref: held is true and no fetch fires', async () => {
    const { result } = withSetup(() => useMemory())
    await vi.waitUntil(() => !result.loading.value)
    const callsAfterMount = vi.mocked(globalThis.fetch).mock.calls.length

    await result.setScope({ scopeKind: 'project', scopeRef: '' })

    expect(result.held.value).toBe(true)
    expect(result.spaces.value).toEqual([])
    expect(vi.mocked(globalThis.fetch).mock.calls.length).toBe(callsAfterMount)
  })

  // Two requests fire once a ref is supplied: the scoped spaces list itself,
  // plus the global spaces list fetched purely to resolve labels for hits
  // that came from a global space (F7 — the retriever unions those in, but
  // ListSpaces for this exact scope never returns them). Also pins the
  // param names on the wire: mem.ParseScope defaults a missing/renamed
  // `scope` to global silently, so a typo'd param would return global data
  // mislabelled as this scope with every other assertion still green.
  it('fires the scoped-spaces and global-label requests once a ref is supplied for a non-global scope', async () => {
    const { result } = withSetup(() => useMemory())
    await vi.waitUntil(() => !result.loading.value)
    const callsAfterMount = vi.mocked(globalThis.fetch).mock.calls.length

    await result.setScope({ scopeKind: 'project', scopeRef: '/tmp/demo' })

    expect(result.held.value).toBe(false)
    expect(vi.mocked(globalThis.fetch).mock.calls.length).toBe(callsAfterMount + 2)
    const urls = fetchUrls()
    expect(urls.some(u => u.includes('scope=project&scopeRef=%2Ftmp%2Fdemo'))).toBe(true)
    expect(urls.some(u => u.includes('scope=global') && !u.includes('scopeRef'))).toBe(true)
  })

  it('treats a 403 on spaces as a capability denial, not a transport error', async () => {
    vi.mocked(globalThis.fetch).mockResolvedValue(jsonResponse(403, { error: 'capability memory.read denied in scope global' }))
    const { result } = withSetup(() => useMemory())
    await vi.waitUntil(() => !result.loading.value)

    expect(result.denied.value).toContain('memory.read')
    expect(result.error.value).toBeNull()
    expect(result.spaces.value).toEqual([])
  })

  it('treats a non-403 failure on spaces as an error, not a denial', async () => {
    vi.mocked(globalThis.fetch).mockResolvedValue(jsonResponse(500, { error: 'boom' }))
    const { result } = withSetup(() => useMemory())
    await vi.waitUntil(() => !result.loading.value)

    expect(result.error.value).toBe('boom')
    expect(result.denied.value).toBeNull()
  })

  it('treats a 403 on entries as a capability denial', async () => {
    const { result } = withSetup(() => useMemory())
    await vi.waitUntil(() => !result.loading.value)
    vi.mocked(globalThis.fetch).mockResolvedValue(jsonResponse(403, { error: 'capability memory.read denied in scope global' }))

    await result.searchEntries()

    expect(result.denied.value).toContain('memory.read')
    expect(result.entries.value).toEqual([])
  })

  // F1: setScope used to leave `entries` untouched, so the previous scope's
  // hits re-rendered under the new scope's heading.
  it('clears stale entries from the previous scope when the scope changes', async () => {
    const { result } = withSetup(() => useMemory())
    await vi.waitUntil(() => !result.loading.value)
    vi.mocked(globalThis.fetch).mockResolvedValue(jsonResponse(200, [
      { id: 'e1', spaceId: 's1', summary: 'x', content: 'y', kind: 'fact', confidence: 1, createdAt: '2026-01-01T00:00:00Z' },
    ]))
    await result.searchEntries()
    expect(result.entries.value).toHaveLength(1)

    vi.mocked(globalThis.fetch).mockResolvedValue(jsonResponse(200, []))
    await result.setScope({ scopeKind: 'project', scopeRef: '/tmp/demo' })

    expect(result.entries.value).toEqual([])
  })

  // F5.1: searchEntries had no held guard test — deleting the guard left all
  // prior tests green.
  it('holds a search the same way it holds the spaces fetch: held is true and no fetch fires', async () => {
    const { result } = withSetup(() => useMemory())
    await vi.waitUntil(() => !result.loading.value)
    await result.setScope({ scopeKind: 'project', scopeRef: '' })
    const callsAfterSetScope = vi.mocked(globalThis.fetch).mock.calls.length

    await result.searchEntries()

    expect(vi.mocked(globalThis.fetch).mock.calls.length).toBe(callsAfterSetScope)
    expect(result.entries.value).toEqual([])
  })

  // F5.2 for the search request specifically: pins the `q` param on the wire.
  it('sends the query text on a search request', async () => {
    const { result } = withSetup(() => useMemory())
    await vi.waitUntil(() => !result.loading.value)
    result.searchText.value = 'binds'

    await result.searchEntries()

    expect(fetchUrls().some(u => u.includes('/api/memory/entries?') && u.includes('q=binds'))).toBe(true)
  })

  // F3: a search failure must not touch the spaces `error` ref.
  it('writes a search failure to searchError, not the shared spaces error', async () => {
    const { result } = withSetup(() => useMemory())
    await vi.waitUntil(() => !result.loading.value)
    vi.mocked(globalThis.fetch).mockResolvedValue(jsonResponse(500, { error: 'search boom' }))

    await result.searchEntries()

    expect(result.searchError.value).toBe('search boom')
    expect(result.error.value).toBeNull()
  })

  // F7: the global-label fetch is best-effort — a failure must not fail the
  // panel, just leave label resolution falling back to the raw id.
  it('falls back to an empty global-spaces list when the label-lookup request fails', async () => {
    const { result } = withSetup(() => useMemory())
    await vi.waitUntil(() => !result.loading.value)
    // setScope fires the scoped-spaces fetch first, then the global-label
    // fetch — only the second one fails here.
    vi.mocked(globalThis.fetch)
      .mockResolvedValueOnce(jsonResponse(200, []))
      .mockRejectedValueOnce(new Error('network down'))

    await result.setScope({ scopeKind: 'project', scopeRef: '/tmp/demo' })

    expect(result.globalSpaces.value).toEqual([])
    expect(result.error.value).toBeNull()
  })
})
