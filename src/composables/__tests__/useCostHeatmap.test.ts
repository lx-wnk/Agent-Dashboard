import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'

let useCostHeatmap: typeof import('../useCostHeatmap').useCostHeatmap

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

const mockGrid = Array.from({ length: 7 }, (_, d) =>
  Array.from({ length: 24 }, (_, h) => d * 24 + h),
)

beforeEach(async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: true,
    json: () => Promise.resolve({ grid: mockGrid }),
    status: 200,
  }))
  vi.resetModules()
  const mod = await import('../useCostHeatmap')
  useCostHeatmap = mod.useCostHeatmap
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useCostHeatmap', () => {
  it('fetches heatmap grid on mount', async () => {
    const { result } = withSetup(() => useCostHeatmap())
    await vi.waitUntil(() => !result.loading.value)

    expect(result.grid.value).toHaveLength(7)
    expect(result.grid.value[0]).toHaveLength(24)
    expect(result.grid.value[1][0]).toBe(24)
  })

  it('refetch re-fetches heatmap', async () => {
    const { result } = withSetup(() => useCostHeatmap())
    await vi.waitUntil(() => !result.loading.value)

    const zeroGrid = Array.from({ length: 7 }, () => new Array(24).fill(0))
    vi.mocked(globalThis.fetch).mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve({ grid: zeroGrid }),
      status: 200,
    } as Response)
    await result.refetch()
    expect(result.grid.value[0][0]).toBe(0)
  })

  it('sets error when fetch fails', async () => {
    vi.mocked(globalThis.fetch).mockResolvedValue({
      ok: false,
      status: 500,
      text: () => Promise.resolve('Internal Server Error'),
      json: () => Promise.resolve({}),
    } as Response)
    vi.resetModules()
    const mod = await import('../useCostHeatmap')
    useCostHeatmap = mod.useCostHeatmap

    const { result } = withSetup(() => useCostHeatmap())
    await vi.waitUntil(() => !result.loading.value)

    expect(result.error.value).toBeTruthy()
  })
})
