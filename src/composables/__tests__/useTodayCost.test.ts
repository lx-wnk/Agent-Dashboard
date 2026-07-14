import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { useTodayCost } from '../useTodayCost'

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

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('useTodayCost', () => {
  it('fetches today\'s cost summary on start', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve({ totalUsd: 4.2 }) })
    vi.stubGlobal('fetch', fetchMock)
    const today = new Date().toISOString().slice(0, 10)

    const { result, wrapper } = withSetup(() => useTodayCost())
    result.start()
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith(
      `/api/cost/summary?from=${today}&to=${today}`,
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    )
    expect(result.todayUsd.value).toBe(4.2)
    wrapper.unmount()
  })

  it('defaults to 0 when totalUsd is absent from the response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve({}) }))
    const { result, wrapper } = withSetup(() => useTodayCost())
    result.start()
    await flushPromises()

    expect(result.todayUsd.value).toBe(0)
    wrapper.unmount()
  })

  it('leaves the last known value in place on a non-ok response', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve({ totalUsd: 1.5 }) })
    vi.stubGlobal('fetch', fetchMock)
    const { result, wrapper } = withSetup(() => useTodayCost())
    result.start()
    await flushPromises()
    expect(result.todayUsd.value).toBe(1.5)

    fetchMock.mockResolvedValueOnce({ ok: false, status: 500 })
    await result.refresh()

    expect(result.todayUsd.value).toBe(1.5)
    wrapper.unmount()
  })

  it('leaves the last known value in place when the request throws', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve({ totalUsd: 2 }) })
    vi.stubGlobal('fetch', fetchMock)
    const { result, wrapper } = withSetup(() => useTodayCost())
    result.start()
    await flushPromises()

    fetchMock.mockRejectedValueOnce(new Error('network down'))
    await result.refresh()

    expect(result.todayUsd.value).toBe(2)
    wrapper.unmount()
  })

  it('polls every 5 minutes while started', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve({ totalUsd: 1 }) })
    vi.stubGlobal('fetch', fetchMock)
    const { result, wrapper } = withSetup(() => useTodayCost())
    result.start()
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(5 * 60 * 1000)
    expect(fetchMock).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('start() is idempotent — a second call does not create a second interval', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve({ totalUsd: 1 }) })
    vi.stubGlobal('fetch', fetchMock)
    const { result, wrapper } = withSetup(() => useTodayCost())
    result.start()
    result.start()
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(5 * 60 * 1000)
    expect(fetchMock).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('aborts an in-flight request when stop() is called', async () => {
    const abortSpy = vi.spyOn(AbortController.prototype, 'abort')
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})))
    const { result, wrapper } = withSetup(() => useTodayCost())

    result.start()
    result.stop()

    expect(abortSpy).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('stop() clears the polling interval', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve({ totalUsd: 1 }) })
    vi.stubGlobal('fetch', fetchMock)
    const { result, wrapper } = withSetup(() => useTodayCost())
    result.start()
    await flushPromises()

    result.stop()
    await vi.advanceTimersByTimeAsync(10 * 60 * 1000)

    expect(fetchMock).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('stops polling on unmount', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve({ totalUsd: 1 }) })
    vi.stubGlobal('fetch', fetchMock)
    const { result, wrapper } = withSetup(() => useTodayCost())
    result.start()
    await flushPromises()

    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(10 * 60 * 1000)

    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})
