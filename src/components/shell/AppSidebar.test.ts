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

async function load() {
  vi.resetModules()
  localStorage.clear()
  const mod = await import('./AppSidebar.vue')
  const { useViewState } = await import('../../composables/useViewState')
  const { useSidebar } = await import('../../composables/useSidebar')
  return { AppSidebar: mod.default, useViewState, useSidebar }
}

const props = {
  agentCount: 12,
  taskCount: 5,
  totalCostLabel: '$2.34',
  totalTokensLabel: '1.2M',
  quotaPct: 73,
  theme: 'dark' as const,
}

describe('appSidebar', () => {
  beforeEach(() => localStorage.clear())

  it('renders group headers when expanded (pinned)', async () => {
    const { AppSidebar, useSidebar } = await load()
    useSidebar().togglePinned()
    const w = mount(AppSidebar, { props })
    expect(w.text()).toContain('Monitor')
    expect(w.text()).toContain('Build')
  })

  it('clicking a nav item sets activeView', async () => {
    const { AppSidebar, useViewState } = await load()
    const w = mount(AppSidebar, { props })
    const pipelineBtn = w.findAll('button').find(b => b.text().includes('Pipeline'))!
    await pipelineBtn.trigger('click')
    expect(useViewState().activeView.value).toBe('pipeline')
  })

  it('pin toggle button flips aria-expanded', async () => {
    const { AppSidebar } = await load()
    const w = mount(AppSidebar, { props })
    const toggle = w.get('[data-testid="sidebar-pin"]')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('true')
  })

  it('shows agent count badge on Dashboard', async () => {
    const { AppSidebar, useSidebar } = await load()
    useSidebar().togglePinned()
    const w = mount(AppSidebar, { props })
    expect(w.text()).toContain('12')
  })
})
