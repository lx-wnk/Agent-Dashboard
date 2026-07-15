import type { UsageData } from '../../composables/useUsage'
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const store: Record<string, string> = {}
globalThis.localStorage = {
  getItem: (k: string) => store[k] ?? null,
  setItem: (k: string, v: string) => { store[k] = v },
  removeItem: (k: string) => { delete store[k] },
  clear: () => { Object.keys(store).forEach(k => delete store[k]) },
  length: 0,
  key: () => null,
}

vi.mock('../../composables/useSystemResources', () => ({
  useSystemResources: () => ({
    info: {
      value: {
        cpu: { usage: 34, cores: 8, model: 'x' },
        memory: { total: 100, used: 62, available: 38, usagePercent: 62 },
        disk: { total: 100, used: 48, available: 52, usagePercent: 48, mount: '/' },
        loadAvg: [1.2, 1.0, 0.8],
        uptime: 100,
      },
    },
  }),
}))

async function load() {
  vi.resetModules()
  localStorage.clear()
  return (await import('./AppStatusBar.vue')).default
}

const noBudgetUsage: UsageData = {
  windows: [
    { key: '5h', tokens: 2_100_000, costCents: 430, budgetTokens: null, pct: null },
    { key: '7d', tokens: 14_000_000, costCents: 2900, budgetTokens: null, pct: null },
  ],
  accounts: [],
}

const budgetUsage: UsageData = {
  windows: [
    { key: '5h', tokens: 1_000_000, costCents: 100, budgetTokens: 10_000_000, pct: 0.1 },
    { key: '7d', tokens: 5_000_000, costCents: 500, budgetTokens: 10_000_000, pct: 0.5 },
  ],
  accounts: [
    { label: '.claude', w5h: { tokens: 600_000, costCents: 60 }, w7d: { tokens: 3_000_000, costCents: 300 } },
    { label: '.claude-work', w5h: { tokens: 400_000, costCents: 40 }, w7d: { tokens: 2_000_000, costCents: 200 } },
  ],
}

describe('appStatusBar', () => {
  beforeEach(() => localStorage.clear())

  it('renders compact CPU/MEM values in the strip', async () => {
    const Bar = await load()
    const w = mount(Bar, { props: { costDelta: 0.42, todayCostLabel: '$5.00', usageData: null } })
    expect(w.text()).toContain('34%')
    expect(w.text()).toContain('62%')
  })

  it('shows consumption text when no budget is set', async () => {
    const Bar = await load()
    const w = mount(Bar, { props: { costDelta: 0, todayCostLabel: '$0.00', usageData: noBudgetUsage } })
    const seg = w.get('[data-testid="seg-usage"]')
    expect(seg.text()).toContain('2.1M')
    expect(seg.text()).toContain('14.0M')
  })

  it('shows worst-case % bar with SESSION label when 5h is worst', async () => {
    const sessWorst: UsageData = {
      windows: [
        { key: '5h', tokens: 9_000_000, costCents: 900, budgetTokens: 10_000_000, pct: 0.9 },
        { key: '7d', tokens: 1_000_000, costCents: 100, budgetTokens: 10_000_000, pct: 0.1 },
      ],
      accounts: [],
    }
    const Bar = await load()
    const w = mount(Bar, { props: { costDelta: 0, todayCostLabel: '$0.00', usageData: sessWorst } })
    const seg = w.get('[data-testid="seg-usage"]')
    expect(seg.text()).toContain('SESSION')
    expect(seg.text()).toContain('90%')
  })

  it('shows worst-case % bar with WEEKLY label when 7d is worst', async () => {
    const Bar = await load()
    const w = mount(Bar, { props: { costDelta: 0, todayCostLabel: '$0.00', usageData: budgetUsage } })
    const seg = w.get('[data-testid="seg-usage"]')
    expect(seg.text()).toContain('WEEKLY')
    expect(seg.text()).toContain('50%')
  })

  it('opens usage popover on click showing both windows', async () => {
    const Bar = await load()
    const w = mount(Bar, { props: { costDelta: 0, todayCostLabel: '$0.00', usageData: noBudgetUsage } })
    await w.get('[data-testid="seg-usage"]').trigger('click')
    const panel = w.get('[data-testid="panel-usage"]')
    expect(panel.text()).toContain('5h')
    expect(panel.text()).toContain('7d')
  })

  it('opens usage popover without throwing when accounts key is absent', async () => {
    // Single-account server response omits accounts entirely.
    const noAccounts = { windows: noBudgetUsage.windows } as unknown as UsageData
    const Bar = await load()
    const w = mount(Bar, { props: { costDelta: 0, todayCostLabel: '$0.00', usageData: noAccounts } })
    await w.get('[data-testid="seg-usage"]').trigger('click')
    const panel = w.get('[data-testid="panel-usage"]')
    expect(panel.text()).toContain('5h')
    expect(panel.text()).toContain('7d')
  })

  it('shows per-account breakdown in popover when >1 account', async () => {
    const Bar = await load()
    const w = mount(Bar, { props: { costDelta: 0, todayCostLabel: '$0.00', usageData: budgetUsage } })
    await w.get('[data-testid="seg-usage"]').trigger('click')
    const panel = w.get('[data-testid="panel-usage"]')
    expect(panel.text()).toContain('.claude')
    expect(panel.text()).toContain('.claude-work')
  })

  it('collapses to corner tab', async () => {
    const Bar = await load()
    const w = mount(Bar, { props: { costDelta: 0.42, todayCostLabel: '$5.00', usageData: null } })
    await w.get('[data-testid="statusbar-collapse"]').trigger('click')
    expect(w.find('[data-testid="statusbar-tab"]').exists()).toBe(true)
  })

  it('expands cost segment on click', async () => {
    const Bar = await load()
    const w = mount(Bar, { props: { costDelta: 0.42, todayCostLabel: '$5.00', usageData: null } })
    await w.get('[data-testid="seg-cost"]').trigger('click')
    expect(w.find('[data-testid="panel-cost"]').exists()).toBe(true)
  })

  it('renders em-dash when costDelta is null', async () => {
    const Bar = await load()
    const w = mount(Bar, { props: { costDelta: null, todayCostLabel: '$0.00', usageData: null } })
    expect(w.text()).toContain('—')
  })

  it('does not color MEM below the warning threshold', async () => {
    const Bar = await load()
    const w = mount(Bar, { props: { costDelta: 0, todayCostLabel: '$0.00', usageData: null } })
    const mem = w.get('[data-testid="mem-pct-strip"]')
    expect(mem.classes()).not.toContain('text-warning-text')
    expect(mem.classes()).not.toContain('text-danger-text')
  })

  it('colors MEM/CPU/DISK warning at >=75% usage', async () => {
    vi.doMock('../../composables/useSystemResources', () => ({
      useSystemResources: () => ({
        info: {
          value: {
            cpu: { usage: 80, cores: 8, model: 'x' },
            memory: { total: 100, used: 80, available: 20, usagePercent: 80 },
            disk: { total: 100, used: 78, available: 22, usagePercent: 78, mount: '/' },
            loadAvg: [1.2, 1.0, 0.8],
            uptime: 100,
          },
        },
      }),
    }))
    const Bar = await load()
    const w = mount(Bar, { props: { costDelta: 0, todayCostLabel: '$0.00', usageData: null } })
    const mem = w.get('[data-testid="mem-pct-strip"]')
    expect(mem.classes()).toContain('text-warning-text')
  })

  it('colors MEM danger at >=90% usage', async () => {
    vi.doMock('../../composables/useSystemResources', () => ({
      useSystemResources: () => ({
        info: {
          value: {
            cpu: { usage: 95, cores: 8, model: 'x' },
            memory: { total: 100, used: 95, available: 5, usagePercent: 95 },
            disk: { total: 100, used: 91, available: 9, usagePercent: 91, mount: '/' },
            loadAvg: [1.2, 1.0, 0.8],
            uptime: 100,
          },
        },
      }),
    }))
    const Bar = await load()
    const w = mount(Bar, { props: { costDelta: 0, todayCostLabel: '$0.00', usageData: null } })
    const mem = w.get('[data-testid="mem-pct-strip"]')
    expect(mem.classes()).toContain('text-danger-text')
  })
})
