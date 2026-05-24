import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'

let useMemory: typeof import('../useMemory').useMemory

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

const defaultFiles = [
  { path: 'memory/lessons.md', name: 'lessons.md' },
]

beforeEach(async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: true,
    json: () => Promise.resolve({ files: defaultFiles }),
    status: 200,
  }))
  vi.resetModules()
  const mod = await import('../useMemory')
  useMemory = mod.useMemory
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useMemory', () => {
  it('fetches files on mount', async () => {
    const { result } = withSetup(() => useMemory())
    await vi.waitUntil(() => !result.loading.value)

    expect(result.files.value).toHaveLength(1)
    expect(result.files.value[0].name).toBe('lessons.md')
  })

  it('refetch re-fetches files', async () => {
    const { result } = withSetup(() => useMemory())
    await vi.waitUntil(() => !result.loading.value)

    vi.mocked(globalThis.fetch).mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve({ files: [{ path: 'a.md', name: 'a.md' }, { path: 'b.md', name: 'b.md' }] }),
      status: 200,
    } as Response)
    await result.refetch()
    expect(result.files.value).toHaveLength(2)
  })

  it('sets error when fetch fails', async () => {
    vi.mocked(globalThis.fetch).mockResolvedValue({
      ok: false,
      status: 500,
      json: () => Promise.resolve({}),
    } as Response)
    vi.resetModules()
    const mod = await import('../useMemory')
    useMemory = mod.useMemory

    const { result } = withSetup(() => useMemory())
    await vi.waitUntil(() => result.error.value !== null || !result.loading.value)

    expect(result.error.value).toBeTruthy()
  })

  it('fetchFileContent returns content on success', async () => {
    const { result } = withSetup(() => useMemory())
    vi.mocked(globalThis.fetch).mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve({ content: '# Notes' }),
      status: 200,
    } as Response)
    const content = await result.fetchFileContent('memory/lessons.md')
    expect(content).toBe('# Notes')
  })

  it('fetchFileContent returns null and sets error on failure', async () => {
    const { result } = withSetup(() => useMemory())
    vi.mocked(globalThis.fetch).mockResolvedValueOnce({
      ok: false,
      status: 404,
      json: () => Promise.resolve({}),
    } as Response)
    const content = await result.fetchFileContent('missing.md')
    expect(content).toBeNull()
    expect(result.error.value).toBeTruthy()
  })
})
