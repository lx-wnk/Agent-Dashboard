import type { Agent } from '@/types'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import AgentTriageBand from './AgentTriageBand.vue'

function makeAgent(overrides: Partial<Agent>): Agent {
  return {
    pid: 4242,
    sessionId: 'sess-1',
    provider: 'claude',
    projectName: 'demo',
    projectPath: '/repo/demo',
    cwd: '/repo/demo',
    status: 'active',
    working: false,
    entrypoint: 'cli',
    lastActivity: new Date().toISOString(),
    uptime: 60,
    liveInjectable: true,
    channelAvailable: true,
    tokenUsage: { inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheCreationTokens: 0 },
    costEstimate: 0,
    cacheCreationCostEstimate: 0,
    cacheReadCostEstimate: 0,
    healthScore: 100,
    conversationTurns: 0,
    toolCounts: {},
    meta: null,
    costUnknown: false,
    currentAction: null,
    lastTools: [],
    tasks: [],
    subagents: [],
    convergenceAlert: false,
    ...overrides,
  } as unknown as Agent
}

function mountBand(agent: Agent) {
  return mount(AgentTriageBand, { props: { agents: [agent], permissionItems: [] } })
}

describe('agentTriageBand pending tool use', () => {
  // An unresolved AskUserQuestion reaches the client through PendingToolUse (the
  // parser reports it so a session whose screen cannot be probed shows
  // something), but it waits for an ANSWER, not a grant — offering to allow it
  // would write a standing rule for a tool nobody has to approve.
  it('offers no permission grant for AskUserQuestion', () => {
    const wrapper = mountBand(makeAgent({
      pendingToolUse: { id: 'tu_q', tool: 'AskUserQuestion', pattern: '' },
    }))
    expect(wrapper.text()).not.toContain('Allow AskUserQuestion')
    expect(wrapper.text()).toContain('Waiting for your answer')
  })

  it('still offers the grant for a genuinely blocked tool', () => {
    const wrapper = mountBand(makeAgent({
      pendingToolUse: { id: 'tu_b', tool: 'Bash', pattern: 'npm publish' },
    }))
    expect(wrapper.text()).toContain('Allow Bash')
  })

  // With the question itself detected, the card renders the answer UI — and the
  // grant must stay away there too.
  it('offers no grant when the question is detected alongside the tool use', () => {
    const wrapper = mountBand(makeAgent({
      pendingToolUse: { id: 'tu_q', tool: 'AskUserQuestion', pattern: '' },
      pendingQuestion: {
        header: 'chrome',
        question: 'Which colour?',
        multiSelect: false,
        options: [{ index: 1, label: 'Red' }, { index: 2, label: 'Green' }],
        typeSomethingIndex: 3,
        chatAboutIndex: 4,
      },
    }))
    expect(wrapper.text()).not.toContain('Allow AskUserQuestion')
    expect(wrapper.text()).toContain('Which colour?')
  })
})
