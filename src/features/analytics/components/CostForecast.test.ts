import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import CostForecast from '@/features/analytics/components/CostForecast.vue'

vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
  ok: true,
  json: async () => ({
    trend: [
      { t: Date.now() - 86400000, y: 0.5 },
      { t: Date.now(), y: 1.0 },
    ],
    forecast: [
      { t: Date.now() + 86400000, projectedCost: 1.5 },
    ],
    alerts: [{ level: 'warn', message: 'Projected cost exceeds warning threshold $10.00' }],
  }),
}))

describe('costForecast', () => {
  it('renders alert messages', async () => {
    const wrapper = mount(CostForecast)
    await flushPromises()
    expect(wrapper.text()).toContain('exceeds warning threshold')
  })
})
