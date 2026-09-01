import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'

let useMemory: typeof import('@/features/settings/composables/useMemory').useMemory
let MemoryWriteDeniedError: typeof import('@/features/settings/composables/useMemory').MemoryWriteDeniedError

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

// A 204 answers with no body at all; json() here fails the test rather than
// resolving, so a write that parses the response is caught instead of quietly
// working against a mock more generous than the server.
function noContentResponse(): Response {
  return {
    ok: true,
    status: 204,
    json: () => Promise.reject(new Error('read the body of a 204')),
  } as unknown as Response
}

function urlOf(call: number): string {
  return String(vi.mocked(globalThis.fetch).mock.calls[call][0])
}

function initOf(call: number): RequestInit {
  return vi.mocked(globalThis.fetch).mock.calls[call][1] ?? {}
}

function bodyOf(call: number): unknown {
  return JSON.parse(String(initOf(call).body))
}

beforeEach(async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse(200, [])))
  vi.resetModules()
  const mod = await import('@/features/settings/composables/useMemory')
  useMemory = mod.useMemory
  MemoryWriteDeniedError = mod.MemoryWriteDeniedError
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
  // mem.ParseScope turns a missing `kind` into global with no error, so a
  // write body without a scope is authorized against — and lands in — the
  // global scope whatever the panel is showing, with no error anywhere.
  it('sends the current scope with a space create and refetches the list from the server', async () => {
    const { result } = withSetup(() => useMemory())
    await vi.waitUntil(() => !result.loading.value)
    vi.mocked(globalThis.fetch).mockClear()

    await result.createSpace({ slug: 'new-space', name: 'New space' })

    expect(urlOf(0)).toBe('/api/memory/spaces')
    expect(initOf(0).method).toBe('POST')
    expect(bodyOf(0)).toEqual({ slug: 'new-space', name: 'New space', scope: 'global', scopeRef: '' })
    // The row the panel shows must come back from ListSpaces, not from
    // splicing the response into the local array.
    expect(urlOf(1)).toContain('/api/memory/spaces?')
  })

  // The scope is read from the one `scope` ref, not hard-coded: with
  // `scope: 'global'` baked in, every other write assertion in this file
  // still passes.
  it('threads the selected scope into a write instead of hard-coding global', async () => {
    const { result } = withSetup(() => useMemory())
    await vi.waitUntil(() => !result.loading.value)
    await result.setScope({ scopeKind: 'project', scopeRef: '/x' })
    vi.mocked(globalThis.fetch).mockClear()

    await result.createSpace({ slug: 'new-space', name: 'New space' })

    expect(bodyOf(0)).toEqual({ slug: 'new-space', name: 'New space', scope: 'project', scopeRef: '/x' })
  })

  it('sends the entry body with its kind, source and current scope, then re-runs the search', async () => {
    const { result } = withSetup(() => useMemory())
    await vi.waitUntil(() => !result.loading.value)
    vi.mocked(globalThis.fetch).mockClear()

    await result.createEntry({
      spaceSlug: 'project-notes',
      summary: 'Binds to loopback',
      content: 'The server binds 127.0.0.1 only.',
      kind: 'fact',
      sourceKind: 'user',
      sourceRef: '',
      confidence: 1,
    })

    expect(urlOf(0)).toBe('/api/memory/entries')
    expect(initOf(0).method).toBe('POST')
    expect(bodyOf(0)).toEqual({
      spaceSlug: 'project-notes',
      summary: 'Binds to loopback',
      content: 'The server binds 127.0.0.1 only.',
      kind: 'fact',
      sourceKind: 'user',
      sourceRef: '',
      confidence: 1,
      scope: 'global',
      scopeRef: '',
    })
    expect(urlOf(1)).toContain('/api/memory/entries?')
  })

  // No scope on this body on purpose: the path carries only an entry id and
  // the server resolves the entry's own space before authorizing.
  it('supersedes by patching the entry id with its replacement, then re-runs the search', async () => {
    const { result } = withSetup(() => useMemory())
    await vi.waitUntil(() => !result.loading.value)
    vi.mocked(globalThis.fetch).mockClear()

    await result.supersedeEntry('e 1', 'e2')

    expect(urlOf(0)).toBe('/api/memory/entries/e%201')
    expect(initOf(0).method).toBe('PATCH')
    expect(bodyOf(0)).toEqual({ supersededBy: 'e2' })
    expect(urlOf(1)).toContain('/api/memory/entries?')
  })

  it('expires an entry with DELETE, never parsing the 204 body, then re-runs the search', async () => {
    const { result } = withSetup(() => useMemory())
    await vi.waitUntil(() => !result.loading.value)
    vi.mocked(globalThis.fetch).mockClear()
    vi.mocked(globalThis.fetch).mockResolvedValueOnce(noContentResponse())

    await result.expireEntry('e1')

    expect(urlOf(0)).toBe('/api/memory/entries/e1')
    expect(initOf(0).method).toBe('DELETE')
    expect(initOf(0).body).toBeUndefined()
    expect(urlOf(1)).toContain('/api/memory/entries?')
  })

  // memory.write is a different grant from memory.read, so a 403 here is a
  // configuration state with a known fix — typed apart from a transport
  // failure so the panel can say which capability is missing.
  it('rejects a write 403 as a capability denial, distinct from a transport failure', async () => {
    const { result } = withSetup(() => useMemory())
    await vi.waitUntil(() => !result.loading.value)

    vi.mocked(globalThis.fetch).mockResolvedValue(jsonResponse(403, { error: 'capability memory.write denied in scope global' }))
    await expect(result.createSpace({ slug: 'a', name: 'A' })).rejects.toThrow(MemoryWriteDeniedError)
    await expect(result.createSpace({ slug: 'a', name: 'A' })).rejects.toThrow('capability memory.write denied in scope global')

    vi.mocked(globalThis.fetch).mockResolvedValue(jsonResponse(500, { error: 'boom' }))
    const transport = await result.createSpace({ slug: 'a', name: 'A' }).catch((e: unknown) => e)
    expect(transport).toBeInstanceOf(Error)
    expect(transport).not.toBeInstanceOf(MemoryWriteDeniedError)
    expect((transport as Error).message).toBe('boom')
  })

  // A write failure must not blank the read side: `error` and `denied` are
  // the spaces fetch's own state, and a rejected write leaves them alone.
  it('leaves the read-side error and denied refs untouched when a write fails', async () => {
    const { result } = withSetup(() => useMemory())
    await vi.waitUntil(() => !result.loading.value)
    vi.mocked(globalThis.fetch).mockResolvedValue(jsonResponse(403, { error: 'capability memory.write denied in scope global' }))

    await result.createEntry({
      spaceSlug: 's',
      summary: 'x',
      content: 'y',
      kind: 'fact',
      sourceKind: 'user',
      sourceRef: '',
      confidence: 1,
    }).catch(() => {})

    expect(result.denied.value).toBeNull()
    expect(result.error.value).toBeNull()
  })
  // Same guard the reads use, on the same computed: a write in a non-global
  // scope with a blank ref would be refused by mem.ParseScope, so it is held
  // rather than fired for the server to reject.
  it('holds a write in a scope with no ref instead of firing a known-400', async () => {
    const { result } = withSetup(() => useMemory())
    await vi.waitUntil(() => !result.loading.value)
    await result.setScope({ scopeKind: 'project', scopeRef: '' })
    vi.mocked(globalThis.fetch).mockClear()

    await expect(result.createSpace({ slug: 'a', name: 'A' })).rejects.toThrow('scope ref')
    await expect(result.createEntry({
      spaceSlug: 's',
      summary: 'x',
      content: 'y',
      kind: 'fact',
      sourceKind: 'user',
      sourceRef: '',
      confidence: 1,
    })).rejects.toThrow('scope ref')
    expect(vi.mocked(globalThis.fetch)).not.toHaveBeenCalled()
  })
})
