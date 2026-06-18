import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, nextTick } from 'vue'
import { mount } from '@vue/test-utils'

// Minimal EventSource stub to prevent actual network calls.
class MockEventSource {
  static instances: MockEventSource[] = []
  onmessage: ((e: MessageEvent) => void) | null = null
  onerror: ((e: Event) => void) | null = null
  readyState = 0

  constructor(public url: string) {
    MockEventSource.instances.push(this)
  }

  close() {
    this.readyState = 2
  }
}

// Provide a minimal localStorage stub.
const store: Record<string, string> = {}
globalThis.localStorage = {
  getItem: (k: string) => store[k] ?? null,
  setItem: (k: string, v: string) => { store[k] = v },
  removeItem: (k: string) => { delete store[k] },
  clear: () => { Object.keys(store).forEach(k => delete store[k]) },
  length: 0,
  key: () => null,
}

let useAgents: typeof import('../useAgents')

beforeEach(async () => {
  MockEventSource.instances = []
  vi.stubGlobal('EventSource', MockEventSource)
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: true,
    json: () => Promise.resolve([]),
  }))
  vi.resetModules()
  useAgents = await import('../useAgents')
})

afterEach(() => {
  vi.unstubAllGlobals()
})

/**
 * Helper: mount a wrapper component to provide the Vue lifecycle context
 * that composables using onUnmounted require.
 */
function withSetup<T>(composable: () => T) {
  let result!: T
  const Wrapper = defineComponent({
    setup() {
      result = composable()
      return {}
    },
    template: '<div />',
  })
  const wrapper = mount(Wrapper, { attachTo: document.body })
  return { result, wrapper }
}

describe('useAgents', () => {
  it('initialises with an agents ref', async () => {
    const { result, wrapper } = withSetup(() => useAgents.useAgents({ autoStart: false }))
    expect(result.agents).toBeDefined()
    expect(Array.isArray(result.agents.value)).toBe(true)
    wrapper.unmount()
  })

  it('exposes filteredAgents and searchQuery refs', async () => {
    const { result, wrapper } = withSetup(() => useAgents.useAgents({ autoStart: false }))
    expect(result.filteredAgents).toBeDefined()
    expect(result.searchQuery).toBeDefined()
    wrapper.unmount()
  })

  it('creates an EventSource connection when autoStart is true', async () => {
    const { wrapper } = withSetup(() => useAgents.useAgents({ autoStart: true }))
    await nextTick()
    // SSE connection must be established after the first tick.
    expect(MockEventSource.instances.length).toBeGreaterThan(0)
    wrapper.unmount()
  })

  it('selectAgent updates selectedAgent', async () => {
    const { result, wrapper } = withSetup(() => useAgents.useAgents({ autoStart: false }))
    // Set a non-null value first, then clear it to verify null assignment works.
    // Note: Vue wraps objects in a Proxy, so use toStrictEqual rather than toBe.
    const fakeAgent = { sessionId: 'test' } as any
    result.selectAgent(fakeAgent)
    expect(result.selectedAgent.value).toStrictEqual(fakeAgent)
    result.selectAgent(null)
    expect(result.selectedAgent.value).toBeNull()
    wrapper.unmount()
  })
})
