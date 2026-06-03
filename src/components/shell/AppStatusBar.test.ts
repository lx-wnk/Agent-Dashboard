import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

// Provide a minimal localStorage stub (jsdom not active at this test path level).
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
    info: { value: {
      cpu: { usage: 34, cores: 8, model: 'x' },
      memory: { total: 100, used: 62, available: 38, usagePercent: 62 },
      disk: { total: 100, used: 48, available: 52, usagePercent: 48, mount: '/' },
      loadAvg: [1.2, 1.0, 0.8],
      uptime: 100,
    } },
  }),
}))

async function load() {
  vi.resetModules()
  localStorage.clear()
  return (await import('./AppStatusBar.vue')).default
}

describe('appStatusBar', () => {
  beforeEach(() => localStorage.clear())

  it('renders compact CPU/MEM values in the strip', async () => {
    const StatusBar = await load()
    const w = mount(StatusBar, { props: { costDelta: 0.42, todayCostLabel: '$5.00' } })
    expect(w.text()).toContain('34%')
    expect(w.text()).toContain('62%')
  })

  it('expands the system segment on click (aria-expanded)', async () => {
    const StatusBar = await load()
    const w = mount(StatusBar, { props: { costDelta: 0.42, todayCostLabel: '$5.00' } })
    const seg = w.get('[data-testid="seg-system"]')
    expect(seg.attributes('aria-expanded')).toBe('false')
    await seg.trigger('click')
    expect(seg.attributes('aria-expanded')).toBe('true')
    expect(w.find('[data-testid="panel-system"]').exists()).toBe(true)
  })

  it('collapses to a corner tab', async () => {
    const StatusBar = await load()
    const w = mount(StatusBar, { props: { costDelta: 0.42, todayCostLabel: '$5.00' } })
    await w.get('[data-testid="statusbar-collapse"]').trigger('click')
    expect(w.find('[data-testid="statusbar-tab"]').exists()).toBe(true)
  })

  it('expands the cost segment on click', async () => {
    const StatusBar = await load()
    const w = mount(StatusBar, { props: { costDelta: 0.42, todayCostLabel: '$5.00' } })
    const seg = w.get('[data-testid="seg-cost"]')
    expect(seg.attributes('aria-expanded')).toBe('false')
    await seg.trigger('click')
    expect(seg.attributes('aria-expanded')).toBe('true')
    expect(w.find('[data-testid="panel-cost"]').exists()).toBe(true)
  })

  it('renders an em-dash when costDelta is null', async () => {
    const StatusBar = await load()
    const w = mount(StatusBar, { props: { costDelta: null, todayCostLabel: '$5.00' } })
    expect(w.text()).toContain('—')
  })
})
