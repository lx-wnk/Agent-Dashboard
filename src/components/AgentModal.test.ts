import type { Agent } from '../types'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import AgentModal from './AgentModal.vue'

vi.mock('../composables/useAgentIdentity', () => ({
  useAgentIdentity: () => ({ getIdentity: () => ({ emoji: '🤖' }) }),
}))

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
  lastTools: ['Read'],
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
  working: false,
  convergenceAlert: false,
  meta: null,
}

const stubs = {
  AppModal: { template: '<div><slot /></div>' },
  AgentChatStream: true,
  CrossLinkBanner: true,
  MachineBadge: true,
  PromptInput: true,
  SubAgentList: true,
  TaskList: true,
  ToolTimeline: true,
  ExecutionWaterfall: true,
  AppBadge: true,
}

function mountModal() {
  return mount(AgentModal, { props: { agent: baseAgent }, global: { stubs } })
}

describe('agentModal details tablist a11y', () => {
  it('renders a role=tablist with two role=tab buttons', () => {
    const wrapper = mountModal()
    expect(wrapper.find('[role="tablist"]').exists()).toBe(true)
    expect(wrapper.findAll('[role="tab"]').length).toBe(2)
  })

  it('marks the active tab with aria-selected and roving tabindex', () => {
    const wrapper = mountModal()
    const tabs = wrapper.findAll('[role="tab"]')
    expect(tabs[0].attributes('aria-selected')).toBe('true')
    expect(tabs[0].attributes('tabindex')).toBe('0')
    expect(tabs[1].attributes('aria-selected')).toBe('false')
    expect(tabs[1].attributes('tabindex')).toBe('-1')
  })

  it('clicking the Waterfall tab activates its panel', async () => {
    const wrapper = mountModal()
    const tabs = wrapper.findAll('[role="tab"]')
    await tabs[1].trigger('click')
    expect(tabs[1].attributes('aria-selected')).toBe('true')
    const panel = wrapper.find('[role="tabpanel"]')
    expect(panel.attributes('aria-labelledby')).toBe(tabs[1].attributes('id'))
  })

  it('arrowRight on the tablist moves selection to the next tab', async () => {
    const wrapper = mountModal()
    await wrapper.find('[role="tablist"]').trigger('keydown', { key: 'ArrowRight' })
    const tabs = wrapper.findAll('[role="tab"]')
    expect(tabs[1].attributes('aria-selected')).toBe('true')
  })
})
