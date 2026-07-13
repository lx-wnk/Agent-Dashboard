import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { useCostAnalytics } from '@/features/analytics/composables/useCostAnalytics'

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

const summaryPayload = {
  byModel: [],
  byProject: [],
  byDay: [],
  byWeek: [],
  totalUsd: 12.5,
  totalInputTokens: 100,
  totalOutputTokens: 50,
  from: '',
  to: '',
  updatedAt: 1,
}

function okFetch() {
  return vi.fn().mockResolvedValue({
    ok: true,
    status: 200,
    json: () => Promise.resolve({ ...summaryPayload }),
  })
}

beforeEach(() => {
  vi.stubGlobal('fetch', okFetch())
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('useCostAnalytics', () => {
  it('start() fetches the summary and clears loading', async () => {
    const { result } = withSetup(() => useCostAnalytics())
    result.start()
    await vi.waitUntil(() => !result.isLoading.value)

    expect(result.summary.value.totalUsd).toBe(12.5)
    expect(result.error.value).toBeNull()
  })

  it('start() applies a default 30-day range on the request URL', async () => {
    const { result } = withSetup(() => useCostAnalytics())
    result.start()
    await vi.waitUntil(() => !result.isLoading.value)

    const url = vi.mocked(globalThis.fetch).mock.calls[0][0] as string
    expect(url).toContain('/api/cost/summary?')
    expect(url).toContain('from=')
    expect(url).toContain('to=')
    expect(result.from.value).not.toBe('')
    expect(result.to.value).not.toBe('')
  })

  it('setRange updates the range and refetches with the new params', async () => {
    const { result } = withSetup(() => useCostAnalytics())
    result.start()
    await vi.waitUntil(() => !result.isLoading.value)

    result.setRange('2026-01-01', '2026-01-31')
    await vi.waitUntil(() => result.to.value === '2026-01-31')

    const lastUrl = vi.mocked(globalThis.fetch).mock.calls.at(-1)![0] as string
    expect(lastUrl).toContain('from=2026-01-01')
    expect(lastUrl).toContain('to=2026-01-31')
  })

  it('refresh sets error on a non-ok response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      json: () => Promise.resolve({}),
    }))
    const { result } = withSetup(() => useCostAnalytics())
    await result.refresh()

    expect(result.error.value).toBeTruthy()
  })

  it('swallows an AbortError without surfacing an error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(Object.assign(new Error('aborted'), { name: 'AbortError' })))
    const { result } = withSetup(() => useCostAnalytics())
    await result.refresh()

    expect(result.error.value).toBeNull()
  })

  it('stop() halts the 60s poll loop', async () => {
    vi.useFakeTimers()
    const { result } = withSetup(() => useCostAnalytics())
    result.start()
    await vi.waitFor(() => expect(vi.mocked(globalThis.fetch)).toHaveBeenCalledTimes(1))

    result.stop()
    const callsAfterStop = vi.mocked(globalThis.fetch).mock.calls.length
    await vi.advanceTimersByTimeAsync(120_000)
    expect(vi.mocked(globalThis.fetch).mock.calls.length).toBe(callsAfterStop)
  })
})
