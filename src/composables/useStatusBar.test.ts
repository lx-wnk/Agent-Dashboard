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
  return import('./useStatusBar')
}

describe('useStatusBar', () => {
  beforeEach(() => localStorage.clear())

  it('defaults: expanded bar, no open segment', async () => {
    const { useStatusBar } = await freshModule()
    const s = useStatusBar()
    expect(s.collapsed.value).toBe(false)
    expect(s.openSegment.value).toBe(null)
  })

  it('toggleSegment opens then closes the same segment', async () => {
    const { useStatusBar } = await freshModule()
    const s = useStatusBar()
    s.toggleSegment('system')
    expect(s.openSegment.value).toBe('system')
    s.toggleSegment('system')
    expect(s.openSegment.value).toBe(null)
  })

  it('toggleSegment switches between segments', async () => {
    const { useStatusBar } = await freshModule()
    const s = useStatusBar()
    s.toggleSegment('system')
    s.toggleSegment('cost')
    expect(s.openSegment.value).toBe('cost')
  })

  it('toggleCollapsed persists and closes any open segment', async () => {
    const { useStatusBar } = await freshModule()
    const s = useStatusBar()
    s.toggleSegment('system')
    s.toggleCollapsed()
    expect(s.collapsed.value).toBe(true)
    expect(s.openSegment.value).toBe(null)
    expect(localStorage.getItem('agent-statusbar-collapsed')).toBe('true')
  })

  it('restores collapsed=true from localStorage', async () => {
    localStorage.setItem('agent-statusbar-collapsed', 'true')
    expect((await freshModule()).useStatusBar().collapsed.value).toBe(true)
  })
})
