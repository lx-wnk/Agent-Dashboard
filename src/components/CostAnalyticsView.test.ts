import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import CostAnalyticsView from './CostAnalyticsView.vue'

describe('costAnalyticsView', () => {
  it('renders empty-state message when API returns no rows', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        byModel: [],
        byDay: [],
        byWeek: [],
        totalUsd: 0,
        updatedAt: 0,
      }),
    }))
    const wrapper = mount(CostAnalyticsView)
    await flushPromises()
    expect(wrapper.text()).toContain('Cost Analytics')
    expect(wrapper.text()).toContain('No cost data yet')
  })

  it('renders model breakdown and total cost when API returns data', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        byModel: [
          { model: 'claude-opus-4', costUsd: 1.5, sessions: 2 },
          { model: 'claude-sonnet-4', costUsd: 0.3, sessions: 1 },
        ],
        byDay: [
          { day: '2026-05-22', model: 'claude-opus-4', costUsd: 1.5 },
          { day: '2026-05-23', model: 'claude-sonnet-4', costUsd: 0.3 },
        ],
        byWeek: [
          { week: '2026-W21', costUsd: 1.8 },
        ],
        totalUsd: 1.8,
        updatedAt: Date.now(),
      }),
    }))
    const wrapper = mount(CostAnalyticsView)
    await flushPromises()
    const text = wrapper.text()
    expect(text).toContain('Spend by Model')
    expect(text).toContain('claude-opus-4')
    expect(text).toContain('claude-sonnet-4')
    // formatCost output should contain the dollar amount
    expect(text).toMatch(/\$1\.80|\$1\.8/)
  })

  it('renders error state when fetch fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      text: async () => 'boom',
    }))
    const wrapper = mount(CostAnalyticsView)
    await flushPromises()
    expect(wrapper.text()).toContain('HTTP 500')
  })
})
