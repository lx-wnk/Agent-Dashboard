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
  it('emits select when the output body is clicked', async () => {
    const w = mount(AgentCard, { props: { agent: makeAgent() }, global: { stubs } })
    await w.get('[data-testid="agent-card-body"]').trigger('click')
    expect(w.emitted('select')).toBeTruthy()
  })
  // Regression for the dead-zone bug: the subagent list used to carry its own
  // @click.stop, so clicking anywhere in that panel (which grows large with the
  // prompt panel hover-expanded below it) silently swallowed the open click.
  it('emits select when the active-subagents panel is clicked, with the prompt panel present', async () => {
    const agent = makeAgent({
      subagents: [{
        id: 'sa-1',
        type: 'researcher',
        status: 'active',
        currentAction: 'Read',
        sessionFile: '/tmp/sa-1.jsonl',
        tokensUsed: 5000,
        durationSeconds: 90,
        latestOutput: 'Analyzing the codebase',
      }],
    })
    const w = mount(AgentCard, { props: { agent }, global: { stubs } })
    await w.get('[data-testid="active-subagents-block"]').trigger('click')
    expect(w.emitted('select')).toBeTruthy()
  })
  it('keeps an expanded subagent output expanded after a fresh SSE-style prop object arrives', async () => {
    const agent = makeAgent({
      subagents: [{
        id: 'sa-1',
        type: 'researcher',
        status: 'active',
        currentAction: 'Read',
        sessionFile: '/tmp/sa-1.jsonl',
        tokensUsed: 5000,
        durationSeconds: 90,
        latestOutput: 'Analyzing the codebase for relevant patterns',
      }],
    })
    const w = mount(AgentCard, { props: { agent }, global: { stubs } })
    await w.get('[data-testid="subagent-expand-toggle"]').trigger('click')
    expect(w.get('[data-testid="subagent-latest-output"]').classes()).toContain('whitespace-pre-wrap')

    // Simulate an SSE frame: a structurally-equal but referentially-fresh agent object.
    const freshAgent = JSON.parse(JSON.stringify(agent))
    await w.setProps({ agent: freshAgent })

    expect(w.get('[data-testid="subagent-latest-output"]').classes()).toContain('whitespace-pre-wrap')
  })
  it('does not emit select when the dismiss button is clicked', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({})))
    const w = mount(AgentCard, { props: { agent: makeAgent({ status: 'finished' }) }, global: { stubs } })
    await w.get('[data-testid="agent-card-dismiss"]').trigger('click')
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
