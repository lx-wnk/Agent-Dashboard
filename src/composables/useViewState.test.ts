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

async function freshModule() {
  vi.resetModules()
  return import('./useViewState')
}

describe('useViewState', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('defaults to dashboard/cards with no stored state', async () => {
    const { useViewState } = await freshModule()
    const { activeView, dashboardLayout } = useViewState()
    expect(activeView.value).toBe('dashboard')
    expect(dashboardLayout.value).toBe('cards')
  })

  it('migrates legacy agent-view-mode="pipeline" to activeView=pipeline', async () => {
    localStorage.setItem('agent-view-mode', 'pipeline')
    const { useViewState } = await freshModule()
    expect(useViewState().activeView.value).toBe('pipeline')
  })

  it('migrates legacy "cost-analytics" to activeView=cost', async () => {
    localStorage.setItem('agent-view-mode', 'cost-analytics')
    expect((await freshModule()).useViewState().activeView.value).toBe('cost')
  })

  it('migrates legacy "list" to dashboard view + list layout', async () => {
    localStorage.setItem('agent-view-mode', 'list')
    const { useViewState } = await freshModule()
    const vs = useViewState()
    expect(vs.activeView.value).toBe('dashboard')
    expect(vs.dashboardLayout.value).toBe('list')
  })

  it('persists activeView changes to localStorage', async () => {
    const { useViewState } = await freshModule()
    useViewState().activeView.value = 'workflows'
    expect(localStorage.getItem('agent-active-view')).toBe('workflows')
  })

  it('persists dashboardLayout changes', async () => {
    const { useViewState } = await freshModule()
    useViewState().dashboardLayout.value = 'list'
    expect(localStorage.getItem('agent-dashboard-layout')).toBe('list')
  })
})
