import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, nextTick } from 'vue'

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

  // A caller passing autoStart: false never increments the shared subscriber
  // count, so it must not decrement it on unmount either — only a caller that
  // started the stream may stop it. DashboardView mounts/unmounts on every
  // dashboard<->other-view switch; without this, two switches away from
  // Dashboard silently close the stream for every other consumer (e.g. App.vue).
  it('a caller opted out of owning the stream (autoStart: false) does not tear it down for an owner on repeated mount/unmount', async () => {
    const owner = withSetup(() => useAgents.useAgents({ autoStart: true }))
    await nextTick()
    expect(MockEventSource.instances).toHaveLength(1)
    const es = MockEventSource.instances[0]
    expect(es.readyState).not.toBe(2)

    withSetup(() => useAgents.useAgents({ autoStart: false })).wrapper.unmount()
    withSetup(() => useAgents.useAgents({ autoStart: false })).wrapper.unmount()

    expect(es.readyState).not.toBe(2)
    owner.wrapper.unmount()
  })
})

describe('useAgents pendingCapabilityDecisions', () => {
  it('a poll response (no decisions field) leaves the pending list untouched', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve([]),
    }))
    const { result, wrapper } = withSetup(() => useAgents.useAgents({ autoStart: false }))
    result.pendingCapabilityDecisions.value = [{ id: 'cap-1' } as any]

    result.startStream()
    await flushPromises()

    expect(result.pendingCapabilityDecisions.value).toEqual([{ id: 'cap-1' }])
    wrapper.unmount()
  })

  it('an SSE frame without the field clears the pending list', async () => {
    const { result, wrapper } = withSetup(() => useAgents.useAgents({ autoStart: true }))
    await flushPromises()
    result.pendingCapabilityDecisions.value = [{ id: 'cap-1' } as any]

    const es = MockEventSource.instances[0]
    es.onmessage?.({ data: JSON.stringify({ agents: [], trend: [] }) } as MessageEvent)

    expect(result.pendingCapabilityDecisions.value).toEqual([])
    wrapper.unmount()
  })
})
