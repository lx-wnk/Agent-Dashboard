import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import AgentCard from '../AgentCard.vue'

// Avoid the real composable's setTimeout localStorage write firing after teardown.
vi.mock('../../composables/useAgentIdentity', () => ({
  useAgentIdentity: () => ({ getIdentity: () => ({ emoji: '🤖' }) }),
}))

const stubs = {
  PromptInput: { template: '<div data-testid="prompt-input" />' },
  MachineBadge: true,
  ProviderBadge: true,
}

function makeAgent(overrides = {}) {
  return {
    pid: 42,
    sessionId: 's1',
    provider: 'claude',
    projectPath: '/home/u/agent-dashboard',
    projectName: 'agent-dashboard',
    cwd: '/home/u/agent-dashboard',
    status: 'active',
    working: true,
    uptime: 1680,
    lastActivity: new Date().toISOString(),
    currentAction: '',
    lastOutput: '',
    lastTools: [],
    subagents: [],
    tokenUsage: { inputTokens: 100, outputTokens: 50, cacheCreationTokens: 0, cacheReadTokens: 0 },
    costEstimate: 6.09,
    costUnknown: false,
    cacheCreationCostEstimate: 0,
    cacheReadCostEstimate: 0,
    healthScore: 79,
    model: 'claude-opus-4-8',
    ...overrides,
  } as any
}

describe('agentCard output body', () => {
  it('shows lastOutput when present', () => {
    const w = mount(AgentCard, { props: { agent: makeAgent({ lastOutput: 'hello from claude' }) }, global: { stubs } })
    expect(w.text()).toContain('hello from claude')
    expect(w.text()).not.toContain('No output yet')
  })
  it('falls back to currentAction when lastOutput is empty', () => {
    const w = mount(AgentCard, { props: { agent: makeAgent({ lastOutput: '', currentAction: 'Bash' }) }, global: { stubs } })
    expect(w.text()).toContain('Bash')
    expect(w.text()).not.toContain('No output yet')
  })
  it('falls back to last tool when no output or action', () => {
    const w = mount(AgentCard, { props: { agent: makeAgent({ lastOutput: '', currentAction: '', lastTools: ['Read'] }) }, global: { stubs } })
    expect(w.text()).toContain('Read')
    expect(w.text()).not.toContain('No output yet')
  })
  it('shows "No output yet" only when truly empty', () => {
    const w = mount(AgentCard, { props: { agent: makeAgent({ lastOutput: '', currentAction: '', lastTools: [] }) }, global: { stubs } })
    expect(w.text()).toContain('No output yet')
  })
})

describe('agentCard interaction', () => {
  it('emits select when the card open target is clicked', async () => {
    const w = mount(AgentCard, { props: { agent: makeAgent() }, global: { stubs } })
    await w.get('[data-testid="agent-card-open"]').trigger('click')
    expect(w.emitted('select')).toBeTruthy()
  })
  it('does not emit select when the prompt input is clicked', async () => {
    const w = mount(AgentCard, { props: { agent: makeAgent() }, global: { stubs } })
    await w.get('[data-testid="prompt-input"]').trigger('click')
    expect(w.emitted('select')).toBeFalsy()
  })
})

describe('agentCard header', () => {
  it('shows the friendly project name with a title tooltip', () => {
    const w = mount(AgentCard, { props: { agent: makeAgent({ projectName: 'agent-dashboard' }) }, global: { stubs } })
    const name = w.get('[data-testid="agent-card-project"]')
    expect(name.text()).toContain('Agent Dashboard')
    expect(name.attributes('title')).toContain('Agent Dashboard')
  })
  it('reveals the metrics popover on info click', async () => {
    const w = mount(AgentCard, { props: { agent: makeAgent() }, global: { stubs } })
    expect(w.find('[data-testid="metrics-popover"]').exists()).toBe(false)
    await w.get('[data-testid="agent-card-info"]').trigger('click')
    expect(w.find('[data-testid="metrics-popover"]').exists()).toBe(true)
  })
})
