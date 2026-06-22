import type { Agent } from '../types'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, nextTick } from 'vue'
import { useAgents } from './useAgents'

function makeAgent(pid: number): Agent {
  return { pid, sessionId: `s-${pid}` } as Agent
}

// useAgents is a module singleton; mount a harness so watchers/onUnmounted run
// inside an active effect scope. autoStart:false keeps the SSE stream closed.
function mountHarness() {
  let api!: ReturnType<typeof useAgents>
  const wrapper = mount(defineComponent({
    setup() {
      api = useAgents({ autoStart: false })
      return () => null
    },
  }))
  return { wrapper, api }
}

describe('useAgents.dismissAgent', () => {
  it('removes the agent with the given pid, leaving others', () => {
    const { api, wrapper } = mountHarness()
    api.agents.value = [makeAgent(1), makeAgent(2), makeAgent(3)]
    api.dismissAgent(2)
    expect(api.agents.value.map(a => a.pid)).toEqual([1, 3])
    wrapper.unmount()
  })

  it('is a no-op when the pid is not present', () => {
    const { api, wrapper } = mountHarness()
    api.agents.value = [makeAgent(1)]
    api.dismissAgent(999)
    expect(api.agents.value.map(a => a.pid)).toEqual([1])
    wrapper.unmount()
  })
})

describe('useAgents selectAgentWhenAvailable', () => {
  beforeEach(() => {
    const { api, wrapper } = mountHarness()
    api.agents.value = []
    api.selectAgent(null)
    wrapper.unmount()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('selects immediately when the agent is already present', () => {
    const { api, wrapper } = mountHarness()
    const agent = makeAgent(1234)
    api.agents.value = [agent]

    api.selectAgentWhenAvailable(1234)

    expect(api.selectedAgent.value).toStrictEqual(agent)
    wrapper.unmount()
  })

  it('selects once the matching agent appears later', async () => {
    const { api, wrapper } = mountHarness()

    api.selectAgentWhenAvailable(4242)
    expect(api.selectedAgent.value).toBeNull()

    api.agents.value = [makeAgent(999)]
    await nextTick()
    expect(api.selectedAgent.value).toBeNull()

    const target = makeAgent(4242)
    api.agents.value = [makeAgent(999), target]
    await nextTick()

    expect(api.selectedAgent.value).toStrictEqual(target)
    wrapper.unmount()
  })

  it('does not select the wrong agent on pid mismatch', async () => {
    const { api, wrapper } = mountHarness()

    api.selectAgentWhenAvailable(1)
    api.agents.value = [makeAgent(2), makeAgent(3)]
    await nextTick()

    expect(api.selectedAgent.value).toBeNull()
    wrapper.unmount()
  })

  it('gives up silently when the pid never appears before the timeout', async () => {
    vi.useFakeTimers()
    const { api, wrapper } = mountHarness()

    api.selectAgentWhenAvailable(7, 30000)
    vi.advanceTimersByTime(30001)

    // Watcher was torn down by the timeout: a late matching frame is ignored.
    api.agents.value = [makeAgent(7)]
    await nextTick()

    expect(api.selectedAgent.value).toBeNull()
    wrapper.unmount()
  })
})
