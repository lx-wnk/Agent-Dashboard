// Verifies each host mounts <PluginSlot> with the correct slot name and ctx.
// Network/composable side-effects are stubbed so the heavy hosts mount in jsdom.
import { shallowMount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AgentModal from '@/features/agents/components/AgentModal.vue'
import TaskCard from '@/features/pipeline/components/TaskCard.vue'
import TaskModal from '@/features/pipeline/components/TaskModal.vue'
import PluginSettings from './PluginSettings.vue'

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => [], body: null }))
  class FakeEventSource {
    close = vi.fn()
    addEventListener = vi.fn()
    removeEventListener = vi.fn()
  }
  vi.stubGlobal('EventSource', FakeEventSource)
  vi.stubGlobal('localStorage', { getItem: () => null, setItem: vi.fn(), removeItem: vi.fn() })
  // useAgentIdentity persists via requestIdleCallback; a noop avoids a post-teardown
  // setTimeout that would touch localStorage after globals are unstubbed.
  vi.stubGlobal('requestIdleCallback', () => 0)
})
afterEach(() => vi.unstubAllGlobals())

const task = { id: 't1', slug: 's', title: 'T', currentStage: 'concept', createdAt: '2026-06-12T00:00:00Z', maxIterations: 3 } as any
const agent = {
  pid: 1,
  projectName: 'P',
  projectPath: '/p',
  status: 'active',
  model: 'sonnet',
  costEstimate: 0,
  uptime: 0,
  tokenUsage: { inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheCreationTokens: 0 },
  tasks: [],
  subagents: [],
  lastTools: [],
} as any

// Generic SFCs can't be passed to findAllComponents as a value; match by name.
function slotNamed(wrapper: any, name: string): any {
  return wrapper.findAllComponents({ name: 'PluginSlot' }).find((c: any) => c.props('name') === name)
}

// shallowMount stubs AppModal and would swallow its default slot (where the
// footer PluginSlot lives), so render the slot through a passthrough stub.
const appModalPassthrough = { AppModal: { template: '<div><slot /></div>' } }

// shallowMount stubs AppCard and swallows its slot; pass through so inner
// PluginSlot components remain discoverable.
const appCardPassthrough = { AppCard: { template: '<div><slot /></div>' } }

describe('plugin slot host wiring', () => {
  it('taskCard mounts the kanban-card-badge slot with the task ctx', () => {
    const wrapper = shallowMount(TaskCard, { props: { task }, global: { stubs: appCardPassthrough } })
    const slot = slotNamed(wrapper, 'kanban-card-badge')
    expect(slot).toBeDefined()
    expect((slot!.props('ctx') as any).task.id).toBe('t1')
  })

  it('agentModal mounts the agent-modal-footer slot with the agent ctx', () => {
    const wrapper = shallowMount(AgentModal, { props: { agent }, global: { stubs: appModalPassthrough } })
    const slot = slotNamed(wrapper, 'agent-modal-footer')
    expect(slot).toBeDefined()
    expect((slot!.props('ctx') as any).agent.pid).toBe(1)
  })

  it('taskModal mounts the task-modal-footer slot with the task ctx', () => {
    const wrapper = shallowMount(TaskModal, { props: { task }, global: { stubs: appModalPassthrough } })
    const slot = slotNamed(wrapper, 'task-modal-footer')
    expect(slot).toBeDefined()
    expect((slot!.props('ctx') as any).task.id).toBe('t1')
  })

  it('pluginSettings mounts the settings-panel slot', () => {
    const wrapper = shallowMount(PluginSettings)
    expect(slotNamed(wrapper, 'settings-panel')).toBeDefined()
  })
})
