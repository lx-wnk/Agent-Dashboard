import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'

let usePlugins: typeof import('@/features/plugins/composables/usePlugins').usePlugins

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

const defaultPlugins = [
  { id: 'my-auth-plugin', capabilities: ['auth_provider'] },
]

beforeEach(async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: true,
    json: () => Promise.resolve(defaultPlugins),
    status: 200,
  }))
  vi.resetModules()
  const mod = await import('@/features/plugins/composables/usePlugins')
  usePlugins = mod.usePlugins
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('usePlugins', () => {
  it('fetches plugins on mount', async () => {
    const { result } = withSetup(() => usePlugins())
    await vi.waitUntil(() => !result.loading.value)

    expect(result.plugins.value).toHaveLength(1)
    expect(result.plugins.value[0].id).toBe('my-auth-plugin')
    // Must call the lifecycle list endpoint, not the legacy settings endpoint.
    expect(vi.mocked(globalThis.fetch)).toHaveBeenCalledWith(
      '/api/plugins',
      expect.objectContaining({ credentials: 'same-origin' }),
    )
  })

  it('refetch re-fetches plugins', async () => {
    const { result } = withSetup(() => usePlugins())
    await vi.waitUntil(() => !result.loading.value)

    vi.mocked(globalThis.fetch).mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve([]),
      status: 200,
    } as Response)
    await result.refetch()
    expect(result.plugins.value).toHaveLength(0)
  })

  it('sets error when fetch fails', async () => {
    vi.mocked(globalThis.fetch).mockResolvedValue({
      ok: false,
      status: 500,
      statusText: 'Internal Server Error',
      json: () => Promise.resolve({}),
    } as Response)
    vi.resetModules()
    const mod = await import('@/features/plugins/composables/usePlugins')
    usePlugins = mod.usePlugins

    const { result } = withSetup(() => usePlugins())
    await vi.waitUntil(() => !result.loading.value)

    expect(result.error.value).toBeTruthy()
  })
})
