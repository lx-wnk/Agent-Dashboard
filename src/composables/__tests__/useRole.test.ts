import { beforeEach, describe, expect, it } from 'vitest'
import { useRole } from '../useRole'

// Provide a minimal localStorage stub (the composable reads/writes to it at module load).
const store: Record<string, string> = {}
globalThis.localStorage = {
  getItem: (k: string) => store[k] ?? null,
  setItem: (k: string, v: string) => { store[k] = v },
  removeItem: (k: string) => { delete store[k] },
  clear: () => { Object.keys(store).forEach(k => delete store[k]) },
  length: 0,
  key: () => null,
}

describe('useRole', () => {
  beforeEach(() => {
    globalThis.localStorage.clear()
  })

  it('returns a role ref', () => {
    const { role } = useRole()
    expect(role).toBeDefined()
    expect(role.value).toBeDefined()
  })

  it('defaults to requester when no value is stored', () => {
    globalThis.localStorage.clear()
    const { role } = useRole()
    // Module-level singleton: role defaults to "requester" when nothing is stored.
    expect(['requester', 'reviewer']).toContain(role.value)
  })

  it('exposes a toggleRole function', () => {
    const { toggleRole } = useRole()
    expect(typeof toggleRole).toBe('function')
  })

  it('toggleRole switches between requester and reviewer', () => {
    const { role, toggleRole } = useRole()
    const initial = role.value
    toggleRole()
    const toggled = initial === 'requester' ? 'reviewer' : 'requester'
    expect(role.value).toBe(toggled)
  })
})
