import type { Agent } from '../types'
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AgentTriageBand from './AgentTriageBand.vue'

vi.mock('../composables/useAgentIdentity', () => ({
  useAgentIdentity: () => ({
    getIdentity: () => ({ emoji: '🤖' }),
  }),
}))

vi.mock('../composables/useNow', () => ({
  useNow: () => ({ nowMs: { value: Date.now() } }),
}))

vi.mock('../composables/usePermissionResolve', () => ({
  usePermissionResolve: () => ({
    resolveAgent: vi.fn(),
    resolving: { value: {} },
  }),
}))

vi.mock('../composables/useToast', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}))

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    pid: 1,
    sessionId: 'sess-1',
    provider: 'claude',
    projectName: 'my-project',
    projectPath: '/my/project',
    cwd: '/my/project',
    entrypoint: 'cli',
    status: 'active',
    uptime: 60,
    lastActivity: new Date().toISOString(),
    currentAction: null,
    lastTools: [],
    tasks: [],
    subagents: [],
    tokenUsage: { inputTokens: 0, outputTokens: 0, cacheCreationTokens: 0, cacheReadTokens: 0 },
    costEstimate: 0,
    cacheCreationCostEstimate: 0,
    cacheReadCostEstimate: 0,
    healthScore: 100,
    conversationTurns: 0,
    toolCounts: {},
    channelAvailable: false,
    working: false,
    convergenceAlert: false,
    meta: null,
    costUnknown: false,
    ...overrides,
  } as unknown as Agent
}

const pendingQuestion = {
  toolUseID: 'tu_q1',
  questions: [
    {
      header: 'Deploy target',
      question: 'Which environment should be used?',
      multiSelect: false,
      options: [
        { label: 'staging', description: 'Use the staging environment' },
        { label: 'prod', description: 'Use production' },
      ],
    },
  ],
}

const stubs = {
  AppButton: { template: '<button v-bind="$attrs"><slot /></button>' },
}

describe('agentTriageBand — pendingQuestion', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve([]) })))
  })

  it('renders question header and option labels', () => {
    const agent = makeAgent({ pendingQuestion })
    const w = mount(AgentTriageBand, {
      props: { agents: [agent], permissionItems: [] },
      global: { stubs },
    })
    expect(w.text()).toContain('Deploy target')
    expect(w.text()).toContain('Which environment should be used?')
    expect(w.text()).toContain('staging')
    expect(w.text()).toContain('Use the staging environment')
    expect(w.text()).toContain('prod')
  })

  it('does NOT render answer buttons (no clickable option buttons)', () => {
    const agent = makeAgent({ pendingQuestion })
    const w = mount(AgentTriageBand, {
      props: { agents: [agent], permissionItems: [] },
      global: { stubs },
    })
    // The only buttons in a question card are the "Open ↗" detail link
    const buttons = w.findAll('button')
    const buttonTexts = buttons.map(b => b.text())
    expect(buttonTexts.some(t => t === 'staging' || t === 'prod')).toBe(false)
  })

  it('shows terminal hint when liveInjectable is false', () => {
    const agent = makeAgent({ pendingQuestion, liveInjectable: false })
    const w = mount(AgentTriageBand, {
      props: { agents: [agent], permissionItems: [] },
      global: { stubs },
    })
    expect(w.text()).toContain('answer in your terminal')
  })

  it('hides terminal hint when liveInjectable is true', () => {
    const agent = makeAgent({ pendingQuestion, liveInjectable: true })
    const w = mount(AgentTriageBand, {
      props: { agents: [agent], permissionItems: [] },
      global: { stubs },
    })
    expect(w.text()).not.toContain('answer in your terminal')
  })

  it('sets aria-label on the card for accessibility', () => {
    const agent = makeAgent({ pendingQuestion })
    const w = mount(AgentTriageBand, {
      props: { agents: [agent], permissionItems: [] },
      global: { stubs },
    })
    const card = w.find('[aria-label*="Question from"]')
    expect(card.exists()).toBe(true)
    expect(card.attributes('aria-label')).toContain('My Project')
  })
})
