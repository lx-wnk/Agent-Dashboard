import { beforeEach, describe, expect, it } from 'vitest'

const store: Record<string, string> = {}
globalThis.localStorage = {
  getItem: (k: string) => store[k] ?? null,
  setItem: (k: string, v: string) => { store[k] = v },
  removeItem: (k: string) => { delete store[k] },
  clear: () => { Object.keys(store).forEach(k => delete store[k]) },
  length: 0,
  key: () => null,
}

describe('useAgentIdentity', () => {
  beforeEach(() => globalThis.localStorage.clear())

  it('assigns deterministic color and emoji', async () => {
    const { useAgentIdentity } = await import('./useAgentIdentity')
    const { getIdentity } = useAgentIdentity()
    const id1 = getIdentity('/projects/foo')
    const id2 = getIdentity('/projects/foo')
    expect(id1.color).toBe(id2.color)
    expect(id1.emoji).toBe(id2.emoji)
  })

  it('assigns string-type color and emoji', async () => {
    const { useAgentIdentity } = await import('./useAgentIdentity')
    const { getIdentity } = useAgentIdentity()
    const a = getIdentity('/projects/alpha')
    expect(typeof a.color).toBe('string')
    expect(typeof a.emoji).toBe('string')
  })
})
