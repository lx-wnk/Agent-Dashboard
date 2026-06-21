import type { Agent } from '../types'
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it } from 'vitest'
import { defineComponent } from 'vue'
import { useAgents } from './useAgents'

function makeAgent(pid: number): Agent {
  return {
    pid,
    sessionId: `s${pid}`,
  } as unknown as Agent
}

let store: ReturnType<typeof useAgents>

function mountStore() {
  const Harness = defineComponent({
    setup() {
      store = useAgents({ autoStart: false })
      return () => null
    },
  })
  return mount(Harness)
}

describe('useAgents.dismissAgent', () => {
  beforeEach(() => {
    mountStore()
    store.agents.value = []
  })

  it('removes the agent with the given pid, leaving others', () => {
    store.agents.value = [makeAgent(1), makeAgent(2), makeAgent(3)]
    store.dismissAgent(2)
    expect(store.agents.value.map(a => a.pid)).toEqual([1, 3])
  })

  it('is a no-op when the pid is not present', () => {
    store.agents.value = [makeAgent(1)]
    store.dismissAgent(999)
    expect(store.agents.value.map(a => a.pid)).toEqual([1])
  })
})
