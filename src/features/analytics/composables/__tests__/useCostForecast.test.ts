import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'

let useCostForecast: typeof import('@/features/analytics/composables/useCostForecast').useCostForecast

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

const mockForecastData = {
  trend: [
    { t: Date.now() - 86400000, y: 0.5 },
    { t: Date.now(), y: 1.2 },
  ],
  forecast: [
    { t: Date.now() + 86400000, projectedCost: 1.5 },
    { t: Date.now() + 172800000, projectedCost: 1.8 },
  ],
  alerts: [
    { level: 'warn' as const, message: 'Spending trending high' },
  ],
}

beforeEach(async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: true,
    json: () => Promise.resolve(mockForecastData),
    status: 200,
  }))
  vi.resetModules()
  const mod = await import('@/features/analytics/composables/useCostForecast')
  useCostForecast = mod.useCostForecast
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useCostForecast', () => {
  it('fetches trend, forecast and alerts on mount', async () => {
    const { result } = withSetup(() => useCostForecast())
    await vi.waitUntil(() => !result.loading.value)

    expect(result.trend.value).toHaveLength(2)
    expect(result.forecast.value).toHaveLength(2)
    expect(result.alerts.value).toHaveLength(1)
    expect(result.alerts.value[0].level).toBe('warn')
  })

  it('refetch re-fetches data', async () => {
    const { result } = withSetup(() => useCostForecast())
    await vi.waitUntil(() => !result.loading.value)

    vi.mocked(globalThis.fetch).mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve({ trend: [], forecast: [], alerts: [] }),
      status: 200,
    } as Response)
    await result.refetch()
    expect(result.trend.value).toHaveLength(0)
    expect(result.alerts.value).toHaveLength(0)
  })

  it('sets error when fetch fails', async () => {
    vi.mocked(globalThis.fetch).mockResolvedValue({
      ok: false,
      status: 500,
      text: () => Promise.resolve('Server error'),
      json: () => Promise.resolve({}),
    } as Response)
    vi.resetModules()
    const mod = await import('@/features/analytics/composables/useCostForecast')
    useCostForecast = mod.useCostForecast

    const { result } = withSetup(() => useCostForecast())
    await vi.waitUntil(() => !result.loading.value)

    expect(result.error.value).toBeTruthy()
  })
})
