import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'

let useResources: typeof import('@/features/settings/composables/useResources').useResources

function withSetup<T>(composable: () => T) {
  let result!: T
  const Wrapper = defineComponent({
    setup() {
      result = composable()
      return {}
    },
    template: '<div />',
  })
  const wrapper = mount(Wrapper, { attachTo: document.body })
  return { result, wrapper }
}

function okOnce(body: unknown) {
  vi.mocked(globalThis.fetch).mockResolvedValueOnce({
    ok: true,
    status: 200,
    json: () => Promise.resolve(body),
  } as unknown as Response)
}

function failOnce(status: number) {
  vi.mocked(globalThis.fetch).mockResolvedValueOnce({
    ok: false,
    status,
    json: () => Promise.resolve([]),
  } as unknown as Response)
}

function deniedOnce(message?: string) {
  vi.mocked(globalThis.fetch).mockResolvedValueOnce({
    ok: false,
    status: 403,
    json: () => message ? Promise.resolve({ error: message }) : Promise.reject(new Error('no body')),
  } as unknown as Response)
}

function pendingOnce() {
  vi.mocked(globalThis.fetch).mockImplementationOnce(() => new Promise(() => {}))
}

function urls(): string[] {
  return vi.mocked(globalThis.fetch).mock.calls.map(c => String(c[0]))
}

function lastUrl(): string {
  const all = urls()
  return all[all.length - 1]
}

const row = {
  id: 'r1',
  kind: 'application',
  slug: 'obsidian',
  name: 'Obsidian',
  scopeKind: 'global',
  scopeRef: '',
  nodeId: 'local',
  state: 'enabled',
  version: '1.0.0',
  origin: 'builtin',
  originRef: 'builtin:obsidian',
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-02T00:00:00Z',
}

beforeEach(async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: true,
    json: () => Promise.resolve([]),
    status: 200,
  }))
  vi.resetModules()
  const mod = await import('@/features/settings/composables/useResources')
  useResources = mod.useResources
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useResources', () => {
  // Regression guard for the round-2 finding: `held` (the display state) and
  // the fetch guard used to be two copies of the same condition that could
  // silently drift. This asserts the composable's actual network behaviour —
  // not just what the panel renders — so a broken `held` breaks this too.
  it('holds the request when a non-global scope has a blank ref: held is true and no fetch fires', async () => {
    const { result } = withSetup(() => useResources())
    await vi.waitUntil(() => !result.loading.value)
    const callsAfterMount = urls().length

    await result.fetchResources({ scopeKind: 'project' })

    expect(result.held.value).toBe(true)
    expect(result.resources.value).toEqual([])
    expect(urls().length).toBe(callsAfterMount)
  })

  it('holds a whitespace-only ref too — it is not an answer to "which scope"', async () => {
    const { result } = withSetup(() => useResources())
    await vi.waitUntil(() => !result.loading.value)
    const callsAfterMount = urls().length

    await result.fetchResources({ scopeKind: 'project', scopeRef: '   ' })

    expect(result.held.value).toBe(true)
    expect(urls().length).toBe(callsAfterMount)
  })

  it('fires the request once a ref is supplied for a non-global scope', async () => {
    const { result } = withSetup(() => useResources())
    await vi.waitUntil(() => !result.loading.value)
    const callsAfterMount = urls().length

    await result.fetchResources({ scopeKind: 'project', scopeRef: '/tmp/demo' })

    expect(result.held.value).toBe(false)
    expect(urls().length).toBe(callsAfterMount + 1)
  })

  // ── What goes on the wire ──────────────────────────────────────────────────
  // The scope pair decides which rows the server returns. A dropped or renamed
  // param still answers 200, so only the URL itself proves the panel's heading
  // and its data describe the same scope.
  it('names the selected kind and the global scope in the query string', async () => {
    const { result } = withSetup(() => useResources())
    await vi.waitUntil(() => !result.loading.value)

    expect(lastUrl()).toBe('/api/resources?kind=application&scope=global')

    await result.fetchResources({ kind: 'memory_space' })

    expect(lastUrl()).toBe('/api/resources?kind=memory_space&scope=global')
  })

  it('sends the non-global scope with its ref, trimmed', async () => {
    const { result } = withSetup(() => useResources())
    await vi.waitUntil(() => !result.loading.value)

    await result.fetchResources({ scopeKind: 'project', scopeRef: '  /tmp/demo  ' })

    expect(lastUrl()).toBe('/api/resources?kind=application&scope=project&scopeRef=%2Ftmp%2Fdemo')
  })

  it('reads a non-ok response as a failure, never as an empty registry', async () => {
    const { result } = withSetup(() => useResources())
    await vi.waitUntil(() => !result.loading.value)

    failOnce(500)
    await result.fetchResources({ kind: 'routine' })

    expect(result.error.value).toContain('HTTP 500')
    expect(result.error.value).toContain('routine')
    expect(result.resources.value).toEqual([])
  })

  // ── What gets cleared ──────────────────────────────────────────────────────
  it('drops the previous kind\'s rows when the next load fails', async () => {
    okOnce([row])
    const { result } = withSetup(() => useResources())
    await vi.waitUntil(() => !result.loading.value)
    expect(result.resources.value).toHaveLength(1)

    failOnce(500)
    await result.fetchResources({ kind: 'routine' })

    expect(result.resources.value).toEqual([])
  })

  it('clears a stale error before the retry that fixes it', async () => {
    failOnce(500)
    const { result } = withSetup(() => useResources())
    await vi.waitUntil(() => !result.loading.value)
    expect(result.error.value).not.toBeNull()

    okOnce([row])
    await result.fetchResources()

    expect(result.error.value).toBeNull()
    expect(result.resources.value).toHaveLength(1)
  })

  it('flags loading for every request, not just the first', async () => {
    const { result } = withSetup(() => useResources())
    await vi.waitUntil(() => !result.loading.value)

    pendingOnce()
    void result.fetchResources({ kind: 'skill' })

    expect(result.loading.value).toBe(true)
  })

  it('clears rows, a stale error and the loading flag when a query is held', async () => {
    okOnce([row])
    const { result } = withSetup(() => useResources())
    await vi.waitUntil(() => !result.loading.value)
    expect(result.resources.value).toHaveLength(1)

    await result.fetchResources({ scopeKind: 'project' })
    expect(result.resources.value).toEqual([])

    failOnce(500)
    await result.fetchResources({ scopeKind: 'global' })
    expect(result.error.value).not.toBeNull()

    await result.fetchResources({ scopeKind: 'application' })
    expect(result.error.value).toBeNull()

    pendingOnce()
    void result.fetchResources({ scopeKind: 'global' })
    expect(result.loading.value).toBe(true)

    await result.fetchResources({ scopeKind: 'project' })
    expect(result.loading.value).toBe(false)
  })

  // ── A refused read ─────────────────────────────────────────────────────────
  // `kind=memory_space` is gated on memory.read, so on a fresh install a 403 is
  // that kind's ordinary first answer. Folding it into `error` would print a raw
  // HTTP line as the normal state.
  it('routes a 403 to its own state, leaving the error state clean', async () => {
    const { result } = withSetup(() => useResources())
    await vi.waitUntil(() => !result.loading.value)

    deniedOnce('memory.read is not granted for scope global')
    await result.fetchResources({ kind: 'memory_space' })

    expect(result.denied.value).toBe('memory.read is not granted for scope global')
    expect(result.error.value).toBeNull()
    expect(result.resources.value).toEqual([])
  })

  it('falls back to a cause-free sentence when the 403 carries no message', async () => {
    const { result } = withSetup(() => useResources())
    await vi.waitUntil(() => !result.loading.value)

    deniedOnce()
    await result.fetchResources({ kind: 'memory_space' })

    expect(result.denied.value).toContain('HTTP 403')
    expect(result.error.value).toBeNull()
  })

  it('keeps a 500 in the error state — a broken read is not a refused one', async () => {
    const { result } = withSetup(() => useResources())
    await vi.waitUntil(() => !result.loading.value)

    failOnce(500)
    await result.fetchResources({ kind: 'memory_space' })

    expect(result.denied.value).toBeNull()
    expect(result.error.value).toContain('HTTP 500')
  })

  it('clears the denial when a kind that is not gated is selected', async () => {
    const { result } = withSetup(() => useResources())
    await vi.waitUntil(() => !result.loading.value)
    deniedOnce('memory.read is not granted for scope global')
    await result.fetchResources({ kind: 'memory_space' })
    expect(result.denied.value).not.toBeNull()

    okOnce([row])
    await result.fetchResources({ kind: 'application' })

    expect(result.denied.value).toBeNull()
    expect(result.resources.value).toHaveLength(1)
  })

  it('starts in the loading state, so the first paint cannot read as "none yet"', () => {
    let loadingAtFirstPaint: boolean | undefined
    const Wrapper = defineComponent({
      setup() {
        loadingAtFirstPaint = useResources().loading.value
        return {}
      },
      template: '<div />',
    })
    mount(Wrapper, { attachTo: document.body })

    expect(loadingAtFirstPaint).toBe(true)
  })
})
