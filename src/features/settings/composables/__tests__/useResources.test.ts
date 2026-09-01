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
  const wrapper = mount(Wrapper)
  return { result, wrapper }
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
    const callsAfterMount = vi.mocked(globalThis.fetch).mock.calls.length

    await result.fetchResources({ scopeKind: 'project' })

    expect(result.held.value).toBe(true)
    expect(result.resources.value).toEqual([])
    expect(vi.mocked(globalThis.fetch).mock.calls.length).toBe(callsAfterMount)
  })

  it('fires the request once a ref is supplied for a non-global scope', async () => {
    const { result } = withSetup(() => useResources())
    await vi.waitUntil(() => !result.loading.value)
    const callsAfterMount = vi.mocked(globalThis.fetch).mock.calls.length

    await result.fetchResources({ scopeKind: 'project', scopeRef: '/tmp/demo' })

    expect(result.held.value).toBe(false)
    expect(vi.mocked(globalThis.fetch).mock.calls.length).toBe(callsAfterMount + 1)
  })
})
