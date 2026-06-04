import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'

let useRemotes: typeof import('../useRemotes').useRemotes

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

const defaultRemotes = [
  { id: 'r1', url: 'http://192.168.1.5:13120', name: 'Home MacBook', createdAt: new Date().toISOString(), connectionOk: true },
]

beforeEach(async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: true,
    json: () => Promise.resolve([...defaultRemotes]),
    status: 200,
  }))
  vi.resetModules()
  const mod = await import('../useRemotes')
  useRemotes = mod.useRemotes
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useRemotes', () => {
  it('fetches remotes on mount', async () => {
    const { result } = withSetup(() => useRemotes())
    await vi.waitUntil(() => !result.loading.value)

    expect(result.remotes.value).toHaveLength(1)
    expect(result.remotes.value[0].id).toBe('r1')
  })

  it('refetch re-fetches remotes', async () => {
    const { result } = withSetup(() => useRemotes())
    await vi.waitUntil(() => !result.loading.value)

    vi.mocked(globalThis.fetch).mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve([]),
      status: 200,
    } as Response)
    await result.refetch()
    expect(result.remotes.value).toHaveLength(0)
  })

  it('addRemote pushes new remote to list', async () => {
    const newRemote = { id: 'r2', url: 'http://10.0.0.5:13120', name: 'Work', createdAt: new Date().toISOString() }
    vi.mocked(globalThis.fetch)
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve([...defaultRemotes]),
        status: 200,
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve(newRemote),
        status: 200,
      } as Response)

    vi.resetModules()
    const mod = await import('../useRemotes')
    useRemotes = mod.useRemotes

    const { result } = withSetup(() => useRemotes())
    await vi.waitUntil(() => !result.loading.value)

    await result.addRemote('http://10.0.0.5:13120', 'Work', null)
    expect(result.remotes.value).toHaveLength(2)
    expect(result.remotes.value[1].id).toBe('r2')
  })

  it('removeRemote removes remote from list', async () => {
    vi.mocked(globalThis.fetch)
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve([...defaultRemotes]),
        status: 200,
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({}),
        status: 200,
      } as Response)

    vi.resetModules()
    const mod = await import('../useRemotes')
    useRemotes = mod.useRemotes

    const { result } = withSetup(() => useRemotes())
    await vi.waitUntil(() => !result.loading.value)

    await result.removeRemote('r1')
    expect(result.remotes.value).toHaveLength(0)
  })

  it('sets error on fetch failure', async () => {
    vi.mocked(globalThis.fetch).mockResolvedValue({
      ok: false,
      status: 500,
      json: () => Promise.resolve({}),
    } as Response)
    vi.resetModules()
    const mod = await import('../useRemotes')
    useRemotes = mod.useRemotes

    const { result } = withSetup(() => useRemotes())
    await vi.waitUntil(() => result.error.value !== null || !result.loading.value)

    expect(result.error.value).toBeTruthy()
  })
})
