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

  it('fires the request once a ref is supplied for a non-global scope', async () => {
    const { result } = withSetup(() => useMemory())
    await vi.waitUntil(() => !result.loading.value)
    const callsAfterMount = vi.mocked(globalThis.fetch).mock.calls.length

    await result.setScope({ scopeKind: 'project', scopeRef: '/tmp/demo' })

    expect(result.held.value).toBe(false)
    expect(vi.mocked(globalThis.fetch).mock.calls.length).toBe(callsAfterMount + 1)
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
})
