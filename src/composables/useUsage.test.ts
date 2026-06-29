import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'

const mockNobudget = {
  windows: [
    { key: '5h', tokens: 2_100_000, costCents: 430, budgetTokens: null, pct: null },
    { key: '7d', tokens: 14_000_000, costCents: 2900, budgetTokens: null, pct: null },
  ],
  accounts: [],
}

const mockWithBudget = {
  windows: [
    { key: '5h', tokens: 1_000_000, costCents: 100, budgetTokens: 10_000_000, pct: 0.1 },
    { key: '7d', tokens: 5_000_000, costCents: 500, budgetTokens: 10_000_000, pct: 0.5 },
  ],
  accounts: [],
}

describe('useUsage', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(mockNobudget),
    }))
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    vi.resetModules()
  })

  it('fetches on start and polls every 5 min', async () => {
    const { useUsage } = await import('./useUsage')
    const Host = defineComponent({
      setup() {
        const u = useUsage()
        u.start()
        return u
      },
      template: '<div />',
    })
    const w = mount(Host)
    await flushPromises()
    expect(fetch).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(5 * 60 * 1000)
    await flushPromises()
    expect(fetch).toHaveBeenCalledTimes(2)

    w.unmount()
  })

  it('clears interval on unmount', async () => {
    const { useUsage } = await import('./useUsage')
    const Host = defineComponent({
      setup() {
        const u = useUsage()
        u.start()
        return u
      },
      template: '<div />',
    })
    const w = mount(Host)
    await flushPromises()
    w.unmount()
    await vi.advanceTimersByTimeAsync(10 * 60 * 1000)
    await flushPromises()
    expect(fetch).toHaveBeenCalledTimes(1) // only the initial fetch
  })

  it('computes worst as nil when no budget', async () => {
    const { useUsage } = await import('./useUsage')
    const Host = defineComponent({
      setup() {
        const u = useUsage()
        u.start()
        return u
      },
      template: '<div />',
    })
    const w = mount(Host)
    await flushPromises()
    expect((w.vm as any).worst).toBeNull()
    w.unmount()
  })

  it('computes worst as the budgeted window with the highest pct', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(mockWithBudget),
    }))
    vi.resetModules()
    const { useUsage } = await import('./useUsage')
    const Host = defineComponent({
      setup() {
        const u = useUsage()
        u.start()
        return u
      },
      template: '<div />',
    })
    const w = mount(Host)
    await flushPromises()
    const worst = (w.vm as any).worst
    expect(worst).not.toBeNull()
    expect(worst.key).toBe('7d') // pct 0.5 > 0.1
    w.unmount()
  })
})
