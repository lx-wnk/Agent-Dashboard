import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'

let useSessions: typeof import('../useSessions').useSessions

function withSetup<T>(composable: () => T) {
  let result!: T
  const Wrapper = defineComponent({
    setup() {
      result = composable()
      return {}
    },
    template: '<div />',
  })
  mount(Wrapper)
  return { result }
}

const defaultSessions = [
  {
    sessionId: 'abc123',
    projectPath: '/home/user/project',
    projectName: 'project',
    lastModified: new Date().toISOString(),
    model: 'claude-3-5-sonnet-20241022',
    firstPrompt: 'Fix the bug',
    lastResponse: 'Done',
    totalInputTokens: 100,
    totalOutputTokens: 50,
    costEstimate: 0.01,
    isRunning: false,
  },
]

beforeEach(async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: true,
    json: () => Promise.resolve(defaultSessions),
    status: 200,
  }))
  vi.resetModules()
  const mod = await import('../useSessions')
  useSessions = mod.useSessions
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useSessions', () => {
  it('exposes sessions ref and refetch', () => {
    const { result } = withSetup(() => useSessions())
    expect(result.sessions).toBeDefined()
    expect(result.refetch).toBeTypeOf('function')
  })

  it('refetch populates sessions', async () => {
    const { result } = withSetup(() => useSessions())
    await result.refetch()
    await vi.waitUntil(() => !result.loading.value)

    expect(result.sessions.value).toHaveLength(1)
    expect(result.sessions.value[0].sessionId).toBe('abc123')
  })

  it('sets error on fetch failure', async () => {
    vi.mocked(globalThis.fetch).mockResolvedValue({
      ok: false,
      status: 500,
      json: () => Promise.resolve({}),
    } as Response)
    vi.resetModules()
    const mod = await import('../useSessions')
    useSessions = mod.useSessions

    const { result } = withSetup(() => useSessions())
    await result.refetch()

    expect(result.error.value).toBeTruthy()
  })
})
