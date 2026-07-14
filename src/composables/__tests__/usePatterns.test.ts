import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { usePatterns } from '../usePatterns'

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

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('usePatterns', () => {
  it('loads patterns on mount', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ patterns: [{ tools: 'Read+Edit', frequency: 12 }] }),
    }))
    const { result, wrapper } = withSetup(() => usePatterns())
    expect(result.isLoading.value).toBe(true)

    await vi.waitUntil(() => !result.isLoading.value)

    expect(fetch).toHaveBeenCalledWith('/api/analytics/patterns')
    expect(result.patterns.value).toEqual([{ tools: 'Read+Edit', frequency: 12 }])
    expect(result.error.value).toBeNull()
    wrapper.unmount()
  })

  it('defaults to an empty array when the response omits patterns', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({}),
    }))
    const { result, wrapper } = withSetup(() => usePatterns())
    await vi.waitUntil(() => !result.isLoading.value)

    expect(result.patterns.value).toEqual([])
    wrapper.unmount()
  })

  it('sets an error and clears patterns on a non-ok response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 503 }))
    const { result, wrapper } = withSetup(() => usePatterns())
    await vi.waitUntil(() => !result.isLoading.value)

    expect(result.error.value).toBe('HTTP 503')
    expect(result.patterns.value).toEqual([])
    wrapper.unmount()
  })

  it('falls back to a generic message on a non-Error rejection', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue('offline'))
    const { result, wrapper } = withSetup(() => usePatterns())
    await vi.waitUntil(() => !result.isLoading.value)

    expect(result.error.value).toBe('Failed to load patterns')
    wrapper.unmount()
  })

  it('reload() re-fetches patterns', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ patterns: [] }),
    })
    vi.stubGlobal('fetch', fetchMock)
    const { result, wrapper } = withSetup(() => usePatterns())
    await vi.waitUntil(() => !result.isLoading.value)

    await result.reload()

    expect(fetchMock).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })
})
