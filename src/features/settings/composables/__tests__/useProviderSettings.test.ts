import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'

let useProviderSettings: typeof import('@/features/settings/composables/useProviderSettings').useProviderSettings

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

const defaultProviders = [
  { id: 'claude', displayName: 'Claude', enabled: true, configDirPresent: true },
  { id: 'codex', displayName: 'Codex', enabled: false, configDirPresent: false },
]

beforeEach(async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: true,
    status: 200,
    json: () => Promise.resolve([...defaultProviders]),
  }))
  vi.resetModules()
  const mod = await import('@/features/settings/composables/useProviderSettings')
  useProviderSettings = mod.useProviderSettings
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useProviderSettings', () => {
  it('fetches providers on mount', async () => {
    const { result } = withSetup(() => useProviderSettings())
    await vi.waitUntil(() => !result.loading.value)

    expect(result.providers.value).toHaveLength(2)
    expect(result.providers.value[0].id).toBe('claude')
    expect(result.error.value).toBeNull()
  })

  it('refetch reloads providers', async () => {
    const { result } = withSetup(() => useProviderSettings())
    await vi.waitUntil(() => !result.loading.value)

    vi.mocked(globalThis.fetch).mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: () => Promise.resolve([]),
    } as Response)
    await result.refetch()
    expect(result.providers.value).toHaveLength(0)
  })

  it('toggle patches and replaces the affected provider in place', async () => {
    const { result } = withSetup(() => useProviderSettings())
    await vi.waitUntil(() => !result.loading.value)

    vi.mocked(globalThis.fetch).mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: () => Promise.resolve({ id: 'codex', displayName: 'Codex', enabled: true, configDirPresent: false }),
    } as Response)

    await result.toggle('codex', true)

    expect(globalThis.fetch).toHaveBeenLastCalledWith('/api/providers/codex', expect.objectContaining({
      method: 'PATCH',
      body: JSON.stringify({ enabled: true }),
    }))
    expect(result.providers.value.find(p => p.id === 'codex')?.enabled).toBe(true)
    expect(result.providers.value).toHaveLength(2)
  })

  it('toggle throws on a non-ok response', async () => {
    const { result } = withSetup(() => useProviderSettings())
    await vi.waitUntil(() => !result.loading.value)

    vi.mocked(globalThis.fetch).mockResolvedValueOnce({
      ok: false,
      status: 403,
      json: () => Promise.resolve({}),
    } as Response)

    await expect(result.toggle('codex', true)).rejects.toThrow('HTTP 403')
  })

  it('sets error on fetch failure', async () => {
    vi.mocked(globalThis.fetch).mockResolvedValue({
      ok: false,
      status: 500,
      json: () => Promise.resolve({}),
    } as Response)
    vi.resetModules()
    const mod = await import('@/features/settings/composables/useProviderSettings')
    useProviderSettings = mod.useProviderSettings

    const { result } = withSetup(() => useProviderSettings())
    await vi.waitUntil(() => !result.loading.value)

    expect(result.error.value).toBeTruthy()
    expect(result.providers.value).toHaveLength(0)
  })
})
