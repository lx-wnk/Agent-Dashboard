import type { Agent } from '../types'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AgentCardGrid from './AgentCardGrid.vue'

vi.mock('../composables/useAgentIdentity', () => ({
  useAgentIdentity: () => ({
    getIdentity: () => ({ emoji: '🤖' }),
  }),
}))

const stubs = {
  MachineBadge: true,
  ProviderBadge: true,
  AppBadge: true,
  PromptInput: true,
}

function finishedAgent(pid = 4242): Agent {
  return {
    pid,
    sessionId: 's1',
    provider: 'claude',
    projectName: 'p',
    projectPath: '/p',
    status: 'finished',
    channelAvailable: true,
    liveInjectable: false,
    uptime: 1,
    lastActivity: new Date().toISOString(),
    tokenUsage: { inputTokens: 0, outputTokens: 0, cacheCreationTokens: 0, cacheReadTokens: 0 },
    costEstimate: 0,
    cacheCreationCostEstimate: 0,
    cacheReadCostEstimate: 0,
    costUnknown: false,
    healthScore: 100,
    model: 'claude-opus-4-8',
    subagents: [],
    tasks: [],
    lastOutput: 'done',
  } as unknown as Agent
}

describe('agentCardGrid dismiss forwarding', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: true, status: 204 })))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('re-emits dismiss with the pid when a finished card is dismissed', async () => {
    const w = mount(AgentCardGrid, {
      props: { agents: [finishedAgent(4242)] },
      global: { stubs },
    })
    await w.find('[data-testid="agent-card-dismiss"]').trigger('click')
    expect(w.emitted('dismiss')?.[0]).toEqual([4242])
  })
})
