import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import MetricsPopover from '@/features/agents/components/MetricsPopover.vue'

function makeAgent(overrides = {}) {
  return {
    pid: 1,
    uptime: 1680,
    lastActivity: new Date(Date.now() - 27 * 60 * 1000).toISOString(),
    costEstimate: 6.09,
    cacheCreationCostEstimate: 4.64,
    cacheReadCostEstimate: 0.08,
    ...overrides,
  } as any
}

describe('metricsPopover', () => {
  it('renders labeled uptime, last activity, burn, and cache rows', () => {
    const wrapper = mount(MetricsPopover, { props: { agent: makeAgent() } })
    const text = wrapper.get('[data-testid="metrics-popover"]').text()
    expect(text).toContain('Uptime')
    expect(text).toContain('Last activity')
    expect(text).toContain('Burn rate')
    expect(text).toContain('Cache write')
    expect(text).toContain('Cache read')
  })

  it('hides the cache rows when there are no cache costs', () => {
    const wrapper = mount(MetricsPopover, { props: { agent: makeAgent({ cacheCreationCostEstimate: 0, cacheReadCostEstimate: 0 }) } })
    expect(wrapper.get('[data-testid="metrics-popover"]').text()).not.toContain('Cache write')
  })
})
