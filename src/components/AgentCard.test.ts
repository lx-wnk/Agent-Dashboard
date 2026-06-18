import type { Agent } from '../types'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

vi.mock('../composables/useAgentIdentity', () => ({
  useAgentIdentity: () => ({
    getIdentity: () => ({ emoji: '🤖' }),
  }),
}))

import AgentCard from './AgentCard.vue'

const baseAgent: Agent = {
  pid: 1234,
  sessionId: 'sess-1',
  provider: 'claude',
  projectPath: '/home/user/my-project',
  projectName: 'my-project',
  cwd: '/home/user/my-project',
  entrypoint: 'cli',
  status: 'active',
  uptime: 60,
  lastActivity: '2026-01-01T00:00:00Z',
  lastTools: [],
  tasks: [],
  subagents: [],
  tokenUsage: { inputTokens: 0, outputTokens: 0, cacheCreationTokens: 0, cacheReadTokens: 0 },
  costEstimate: 0,
  cacheCreationCostEstimate: 0,
  cacheReadCostEstimate: 0,
  healthScore: 80,
  conversationTurns: 0,
  toolCounts: {},
  channelAvailable: false,
  convergenceAlert: false,
  meta: null,
}

const stubs = {
  MachineBadge: true,
  ProviderBadge: true,
  AppBadge: true,
  PromptInput: true,
}

describe('AgentCard', () => {
  it('renders a real button with data-testid="agent-card-open"', () => {
    const wrapper = mount(AgentCard, {
      props: { agent: baseAgent },
      global: { stubs },
    })
    expect(wrapper.find('button[data-testid="agent-card-open"]').exists()).toBe(true)
  })

  it('aria-label on the open button contains projectName', () => {
    const wrapper = mount(AgentCard, {
      props: { agent: baseAgent },
      global: { stubs },
    })
    const btn = wrapper.find('button[data-testid="agent-card-open"]')
    expect(btn.attributes('aria-label')).toContain('my-project')
  })

  it('clicking the open button emits select with the agent', async () => {
    const wrapper = mount(AgentCard, {
      props: { agent: baseAgent },
      global: { stubs },
    })
    await wrapper.find('button[data-testid="agent-card-open"]').trigger('click')
    expect(wrapper.emitted('select')).toBeTruthy()
    expect(wrapper.emitted('select')![0]).toEqual([baseAgent])
  })
})
