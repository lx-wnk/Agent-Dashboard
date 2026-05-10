import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import CostHeatmap from './CostHeatmap.vue'

const mockGrid = Array.from(
  { length: 7 },
  (_, d) => Array.from({ length: 24 }, (_h, h) => (d === 1 && h === 9 ? 0.5 : 0)),
)

vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
  ok: true,
  json: async () => ({ grid: mockGrid }),
}))

describe('costHeatmap', () => {
  it('renders 7 day-rows', async () => {
    const wrapper = mount(CostHeatmap)
    await flushPromises()
    for (const label of ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'])
      expect(wrapper.text()).toContain(label)
  })
})
