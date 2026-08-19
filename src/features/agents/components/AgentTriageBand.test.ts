import type { PendingToolUse } from '@/sdk.generated'
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

// Typed so a fixture cannot silently drop a field the wire type requires.
// patternDisplay defaults to pattern: these fixtures carry no deceptive runes,
// and the two differing is what the sanitize tests are for.
function toolUse(o: Partial<PendingToolUse> & Pick<PendingToolUse, 'tool'>): PendingToolUse {
  return { id: 'tu_1', pattern: '', patternDisplay: o.pattern ?? '', ...o }
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
      pendingToolUse: toolUse({ id: 'tu_q', tool: 'AskUserQuestion', pattern: '' }),
    }))
    expect(wrapper.text()).not.toContain('Allow AskUserQuestion')
    expect(wrapper.text()).toContain('Waiting for your answer')
  })

  // A bare pendingToolUse on an otherwise finished agent is a "your turn" card,
  // not a blocked one: nothing is waiting for a grant. The exclusion form this
  // replaced only ruled out 'stalled', so it offered the button here too.
  it('offers no grant for a leftover tool use on a finished turn', () => {
    const wrapper = mountBand(makeAgent({
      pendingToolUse: toolUse({ id: 'tu_b', tool: 'Bash', pattern: 'npm publish' }),
    }))
    expect(wrapper.text()).not.toContain('Allow Bash')
    // Positive anchor: without it the assertion above also passes when the card
    // stops rendering altogether.
    expect(wrapper.text()).toContain('Bash')
  })

  // A question is answered, not granted: the prompt on screen is about something
  // other than the pending tool call, so it is not evidence for a standing rule.
  it('offers no grant while a question is the only prompt on screen', () => {
    const wrapper = mountBand(makeAgent({
      pendingToolUse: toolUse({ id: 'tu_b', tool: 'Bash', pattern: 'npm publish' }),
      pendingQuestion: {
        header: 'branch',
        question: 'Which branch?',
        multiSelect: false,
        options: [{ index: 1, label: 'main' }, { index: 2, label: 'dev' }],
        typeSomethingIndex: 3,
        chatAboutIndex: 4,
      },
    }))
    expect(wrapper.text()).not.toContain('Allow Bash')
    expect(wrapper.text()).toContain('Which branch?')
  })

  // The server sets PendingPermissions only together with PipelineTaskID
  // (agentbroadcast/enricher.go), and an orchestrated agent is approved through
  // the pipeline control, not through a standing project-wide rule. A fixture
  // with permissions and no task id describes a payload that cannot be emitted.
  it('routes an orchestrated permission to Approve, not to a standing grant', () => {
    const wrapper = mountBand(makeAgent({
      pipelineTaskId: 'task-1',
      pendingToolUse: toolUse({ id: 'tu_b', tool: 'Bash', pattern: 'npm publish' }),
      pendingPermissions: [{ id: 'p1', tool: 'Bash', pattern: 'npm publish', requestedAt: new Date().toISOString() }],
    }))
    expect(wrapper.text()).not.toContain('Allow Bash')
    expect(wrapper.text()).toContain('Approve')
  })

  // With the question itself detected, the card renders the answer UI — and the
  // grant must stay away there too.
  it('offers no grant when the question is detected alongside the tool use', () => {
    const wrapper = mountBand(makeAgent({
      pendingToolUse: toolUse({ id: 'tu_q', tool: 'AskUserQuestion', pattern: '' }),
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

describe('agentTriageBand stalled card', () => {
  // A tool_use left unresolved for a long time is reclassified 'stalled' (nobody
  // asked for approval, the run is just quiet) — the card must not imply an
  // approval is pending or print the full command that was never submitted for review.
  it('renders no approve control for a stalled agent', () => {
    const wrapper = mountBand(makeAgent({
      pendingToolUse: toolUse({ id: 'tu_s', tool: 'Bash', pattern: 'rm -rf /tmp/build-cache && npm publish --access public' }),
      lastActivity: new Date(Date.now() - 300_000).toISOString(),
    }))
    expect(wrapper.text()).not.toContain('Allow Bash')
    expect(wrapper.text()).not.toContain('rm -rf /tmp/build-cache')
    expect(wrapper.text()).toContain('Bash — running but silent, last output')
  })

  // errorState and pendingToolUse arrive together whenever an API error
  // interrupts a tool call. The card must report the failure, not the command
  // that was interrupted — the badge already says the run failed.
  it('reports the error, not the interrupted command, on a failed run', () => {
    const wrapper = mountBand(makeAgent({
      pendingToolUse: toolUse({ id: 'tu_e', tool: 'Bash', pattern: 'npm publish --access public' }),
      errorState: 'rate_limited',
    }))
    expect(wrapper.text()).not.toContain('npm publish --access public')
    expect(wrapper.text()).toContain('Rate limited')
  })

  it('still offers the approve control for a real pending permission', () => {
    const wrapper = mountBand(makeAgent({
      pipelineTaskId: 'task-1',
      pendingPermissions: [{ id: 'perm_1', tool: 'Bash', pattern: 'npm publish', requestedAt: new Date().toISOString() }],
    }))
    expect(wrapper.text()).toContain('Approve')
  })
})

describe('agentTriageBand all-clear state', () => {
  // "Nothing needs you" is the normal case, so it must not read as loudly as a
  // band full of blocked agents.
  it('renders the all-clear line without a filled banner', () => {
    const w = mount(AgentTriageBand, { props: { agents: [], permissionItems: [] } })
    const line = w.get('[data-testid="triage-all-clear"]')
    expect(line.text()).toContain('All clear')
    expect(line.classes().join(' ')).not.toMatch(/bg-success-soft|border-success-line/)
  })

  it('drops the all-clear line as soon as an agent needs attention', () => {
    const w = mountBand(makeAgent({ pendingToolUse: toolUse({ tool: 'Bash', pattern: 'ls', id: 'tu_1' }) }))
    expect(w.find('[data-testid="triage-all-clear"]').exists()).toBe(false)
  })
})
