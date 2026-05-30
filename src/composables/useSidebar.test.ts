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
  return import('./useSidebar')
}

describe('useSidebar', () => {
  beforeEach(() => localStorage.clear())

  it('defaults to collapsed (not pinned, not expanded)', async () => {
    const { useSidebar } = await freshModule()
    const s = useSidebar()
    expect(s.pinned.value).toBe(false)
    expect(s.expanded.value).toBe(false)
  })

  it('restores pinned=true from localStorage', async () => {
    localStorage.setItem('agent-sidebar-pinned', 'true')
    const { useSidebar } = await freshModule()
    expect(useSidebar().expanded.value).toBe(true)
  })

  it('togglePinned flips and persists', async () => {
    const { useSidebar } = await freshModule()
    const s = useSidebar()
    s.togglePinned()
    expect(s.pinned.value).toBe(true)
    expect(localStorage.getItem('agent-sidebar-pinned')).toBe('true')
  })

  it('hovering expands when not pinned', async () => {
    const { useSidebar } = await freshModule()
    const s = useSidebar()
    s.setHovering(true)
    expect(s.expanded.value).toBe(true)
    s.setHovering(false)
    expect(s.expanded.value).toBe(false)
  })

  it('handleShortcut toggles pin on ctrl+b', async () => {
    const { useSidebar } = await freshModule()
    const s = useSidebar()
    const e = new KeyboardEvent('keydown', { key: 'b', ctrlKey: true })
    const prevented = vi.spyOn(e, 'preventDefault')
    s.handleShortcut(e)
    expect(s.pinned.value).toBe(true)
    expect(prevented).toHaveBeenCalled()
  })

  it('handleShortcut ignores plain b', async () => {
    const { useSidebar } = await freshModule()
    const s = useSidebar()
    s.handleShortcut(new KeyboardEvent('keydown', { key: 'b' }))
    expect(s.pinned.value).toBe(false)
  })
})
