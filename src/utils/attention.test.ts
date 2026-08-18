import type { Agent } from '../types'
import { describe, expect, it } from 'vitest'
import { attentionFor, needsAttention, sortByTriage } from './attention'
import { STALLED_THRESHOLD_SECONDS } from './format'

function makeAgent(overrides: Partial<Agent>): Agent {
  return {
    sessionId: 'test-session',
    pid: 1234,
    projectName: 'test-project',
    projectPath: '/test/path',
    cwd: '/test/path',
    status: 'active',
    uptime: 0,
    lastActivity: new Date().toISOString(),
    currentAction: null,
    lastTools: [],
    tasks: [],
    subagents: [],
    tokenUsage: { inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheCreationTokens: 0 },
    costEstimate: 0,
    cacheCreationCostEstimate: 0,
    cacheReadCostEstimate: 0,
    healthScore: 100,
    conversationTurns: 0,
    toolCounts: {},
    channelAvailable: false,
    convergenceAlert: false,
    meta: null,
    costUnknown: false,
    provider: 'claude',
    entrypoint: 'cli',
    ...overrides,
  } as Agent
}

const ACTIVE_SECS = 10
const STALLED_SECS = STALLED_THRESHOLD_SECONDS + 1

describe('attentionFor', () => {
  it('returns null for idle status (idle is no longer flagged)', () => {
    const agent = makeAgent({ status: 'idle' })
    expect(attentionFor(agent, ACTIVE_SECS)).toBeNull()
  })

  it('returns question with top priority when pendingQuestion is set', () => {
    const agent = makeAgent({
      status: 'active',
      pendingQuestion: {
        header: 'Choose',
        question: 'Which one?',
        multiSelect: false,
        options: [{ index: 1, label: 'A' }],
        typeSomethingIndex: 2,
        chatAboutIndex: 3,
      },
    })
    const att = attentionFor(agent, ACTIVE_SECS)
    expect(att?.kind).toBe('question')
    expect(att?.weight).toBeLessThan(0)
  })

  it('pendingQuestion takes precedence over pendingPermissions/pendingToolUse', () => {
    const agent = makeAgent({
      status: 'active',
      pendingQuestion: {
        header: 'Choose',
        question: 'Which one?',
        multiSelect: false,
        options: [{ index: 1, label: 'A' }],
        typeSomethingIndex: 2,
        chatAboutIndex: 3,
      },
      pendingPermissions: [{ id: 'r1', tool: 'Bash', pattern: 'ls', requestedAt: new Date().toISOString() }],
      pendingToolUse: { tool: 'Bash', pattern: 'ls', id: 'tu_1' },
    })
    const att = attentionFor(agent, ACTIVE_SECS)
    expect(att?.kind).toBe('question')
  })

  // A session started with --dangerously-skip-permissions never stops for a
  // prompt, so its unresolved tool_use means the tool is running.
  it('does not read a running tool as a permission prompt when permissions are bypassed', () => {
    const agent = makeAgent({
      status: 'active',
      permissionsBypassed: true,
      pendingToolUse: { tool: 'Bash', pattern: 'sleep 60', id: 'tu_1' },
    })
    expect(attentionFor(agent, ACTIVE_SECS)).toBeNull()
  })

  // The symptom report: a still-running tool and a genuinely blocked one are
  // the same JSONL shape, so a card claiming a permission prompt while the
  // agent is simultaneously rendered "Working" elsewhere is always wrong.
  it('a busy agent with an unresolved pendingToolUse produces no permission attention', () => {
    const agent = makeAgent({
      status: 'active',
      working: true,
      permissionsBypassed: false,
      pendingToolUse: { tool: 'WebSearch', pattern: '', id: 'tu_1' },
    })
    const att = attentionFor(agent, ACTIVE_SECS)
    expect(att?.kind).not.toBe('permission')
    expect(att).toBeNull()
  })

  // Task-driven grants are real DB rows, not an inference from the transcript.
  it('still reports task permission requests for a bypassed session', () => {
    const agent = makeAgent({
      status: 'active',
      permissionsBypassed: true,
      pendingPermissions: [{ id: 'p1', tool: 'Bash', pattern: 'ls', requestedAt: new Date().toISOString() }],
    })
    expect(attentionFor(agent, ACTIVE_SECS)?.kind).toBe('permission')
  })

  // A session parked on the review/submit screen is just as blocked as one
  // showing the modal — it waits for a keypress that only a human can give.
  it('returns question with top priority when pendingConfirm is set', () => {
    const agent = makeAgent({
      status: 'active',
      pendingConfirm: {
        question: 'Ready to submit your answers?',
        options: [
          { index: 1, label: 'Submit answers' },
          { index: 2, label: 'Cancel' },
        ],
      },
      pendingPermissions: [{ id: 'r1', tool: 'Bash', pattern: 'ls', requestedAt: new Date().toISOString() }],
    })
    const att = attentionFor(agent, ACTIVE_SECS)
    expect(att?.kind).toBe('question')
    expect(att?.label).toBe('Confirm answers')
    expect(att?.weight).toBeLessThan(0)
    expect(needsAttention(agent, ACTIVE_SECS)).toBe(true)
  })

  // Past the dwell used for a stalled session, a still-unresolved tool_use is
  // worth a nudge — but only the honest one: no tool name, no permission claim.
  it('reports a long-unresolved pendingToolUse as stalled, not permission', () => {
    const agent = makeAgent({
      status: 'active',
      permissionsBypassed: false,
      pendingToolUse: { tool: 'Bash', pattern: 'git push', id: 'tu_1' },
    })
    const att = attentionFor(agent, STALLED_SECS)
    expect(att?.kind).toBe('stalled')
    expect(att?.label).toBe('No activity')
    expect(att?.weight).toBe(2)
  })

  it('returns error when errorState is set', () => {
    const agent = makeAgent({ errorState: 'auth_failed' })
    const att = attentionFor(agent, ACTIVE_SECS)
    expect(att?.kind).toBe('error')
    expect(att?.tone).toBe('danger')
    expect(att?.weight).toBe(1)
  })

  it('returns null for a finished agent even with a reconstructed errorState', () => {
    const agent = makeAgent({ status: 'finished', errorState: 'auth_failed', pendingToolUse: { tool: 'Bash', pattern: 'ls', id: 'tu_3' } })
    expect(attentionFor(agent, ACTIVE_SECS)).toBeNull()
  })

  it('returns stalled for active + long silence', () => {
    const agent = makeAgent({ status: 'active' })
    const att = attentionFor(agent, STALLED_SECS)
    expect(att?.kind).toBe('stalled')
    expect(att?.tone).toBe('warning')
    expect(att?.weight).toBe(2)
  })

  it('returns null for healthy active agent', () => {
    const agent = makeAgent({ status: 'active' })
    expect(attentionFor(agent, ACTIVE_SECS)).toBeNull()
  })

  it('returns null when secondsSince is null for active agent', () => {
    const agent = makeAgent({ status: 'active' })
    expect(attentionFor(agent, null)).toBeNull()
  })

  it('pendingPermissions on active agent classifies as permission', () => {
    const agent = makeAgent({
      status: 'active',
      pendingPermissions: [{ id: 'r1', tool: 'Bash', pattern: 'git push', requestedAt: new Date().toISOString() }],
    })
    const att = attentionFor(agent, ACTIVE_SECS)
    expect(att?.kind).toBe('permission')
    expect(att?.weight).toBe(0)
  })

  it('pendingPermissions takes precedence over pendingToolUse', () => {
    const agent = makeAgent({
      status: 'active',
      pendingPermissions: [{ id: 'r1', tool: 'Bash', pattern: 'ls', requestedAt: new Date().toISOString() }],
      pendingToolUse: { tool: 'WebFetch', pattern: '', id: 'tu_2' },
    })
    const att = attentionFor(agent, ACTIVE_SECS)
    expect(att?.kind).toBe('permission')
  })

  it('errorState takes precedence over stalled', () => {
    const agent = makeAgent({ status: 'active', errorState: 'rate_limited' })
    const att = attentionFor(agent, STALLED_SECS)
    expect(att?.kind).toBe('error')
  })
})

it('returns null for a finished agent even when it would otherwise read as your-turn', () => {
  const agent = makeAgent({ status: 'finished', working: false })
  expect(attentionFor(agent, ACTIVE_SECS)).toBeNull()
})

it('returns yourTurn when the turn is finished and the process is still alive', () => {
  const agent = makeAgent({ status: 'idle', working: false })
  const att = attentionFor(agent, ACTIVE_SECS)
  expect(att?.kind).toBe('yourTurn')
  expect(att?.label).toBe('Your turn')
  expect(att?.tone).toBe('neutral')
  expect(att?.weight).toBeGreaterThan(2)
})

it('a pending question outranks yourTurn when both apply', () => {
  const agent = makeAgent({
    status: 'idle',
    working: false,
    pendingQuestion: {
      header: 'Choose',
      question: 'Which one?',
      multiSelect: false,
      options: [{ index: 1, label: 'A' }],
      typeSomethingIndex: 2,
      chatAboutIndex: 3,
    },
  })
  expect(attentionFor(agent, ACTIVE_SECS)?.kind).toBe('question')
})

describe('needsAttention', () => {
  it('returns true for agent with pendingQuestion', () => {
    const agent = makeAgent({
      pendingQuestion: {
        header: 'Choose',
        question: 'Which one?',
        multiSelect: false,
        options: [{ index: 1, label: 'A' }],
        typeSomethingIndex: 2,
        chatAboutIndex: 3,
      },
    })
    expect(needsAttention(agent, ACTIVE_SECS)).toBe(true)
  })

  it('returns false for a freshly-started pendingToolUse (indistinguishable from a running tool)', () => {
    const agent = makeAgent({
      pendingToolUse: { tool: 'Bash', pattern: 'rm -rf', id: 'tu_3' },
    })
    expect(needsAttention(agent, ACTIVE_SECS)).toBe(false)
  })

  it('returns true for a pendingToolUse that has sat unresolved past the stalled dwell', () => {
    const agent = makeAgent({
      pendingToolUse: { tool: 'Bash', pattern: 'rm -rf', id: 'tu_3' },
    })
    expect(needsAttention(agent, STALLED_SECS)).toBe(true)
  })

  it('returns false for idle agent', () => {
    expect(needsAttention(makeAgent({ status: 'idle' }), ACTIVE_SECS)).toBe(false)
  })

  it('returns false for healthy active agent', () => {
    expect(needsAttention(makeAgent({ status: 'active' }), ACTIVE_SECS)).toBe(false)
  })
})

describe('sortByTriage', () => {
  it('places yourTurn after a pendingQuestion agent and after a pendingPermissions agent', () => {
    const withQuestion = makeAgent({
      status: 'active',
      sessionId: 'question',
      pendingQuestion: {
        header: 'Choose',
        question: 'Which one?',
        multiSelect: false,
        options: [{ index: 1, label: 'A' }],
        typeSomethingIndex: 2,
        chatAboutIndex: 3,
      },
    })
    const withPermission = makeAgent({
      status: 'active',
      sessionId: 'permission',
      pendingPermissions: [{ id: 'r1', tool: 'Bash', pattern: 'ls', requestedAt: new Date().toISOString() }],
    })
    const yourTurn = makeAgent({ status: 'idle', sessionId: 'your-turn', working: false })
    const sorted = sortByTriage([yourTurn, withPermission, withQuestion], () => ACTIVE_SECS)
    expect(sorted.map(a => a.sessionId)).toEqual(['question', 'permission', 'your-turn'])
  })

  it('places attention agents before non-attention agents', () => {
    const withError = makeAgent({ status: 'active', sessionId: 'err', errorState: 'auth_failed' })
    const active = makeAgent({ status: 'active', sessionId: 'active' })
    const sorted = sortByTriage([active, withError], () => ACTIVE_SECS)
    expect(sorted[0].sessionId).toBe('err')
    expect(sorted[1].sessionId).toBe('active')
  })

  it('idle agents are placed after attention agents (not in attention group)', () => {
    const idle = makeAgent({ status: 'idle', sessionId: 'idle' })
    const withError = makeAgent({ status: 'active', sessionId: 'err', errorState: 'quota_exhausted' })
    const sorted = sortByTriage([idle, withError], () => ACTIVE_SECS)
    expect(sorted[0].sessionId).toBe('err')
    expect(sorted[1].sessionId).toBe('idle')
  })

  it('sorts by ascending weight within attention group', () => {
    const withPermission = makeAgent({
      status: 'active',
      sessionId: 'perm',
      pendingPermissions: [{ id: 'r1', tool: 'Bash', pattern: 'x', requestedAt: new Date().toISOString() }],
    })
    const stalled = makeAgent({ status: 'active', sessionId: 'stalled' })
    const error = makeAgent({ status: 'active', sessionId: 'error', errorState: 'quota_exhausted' })
    const sorted = sortByTriage([stalled, error, withPermission], a =>
      a.sessionId === 'stalled' ? STALLED_SECS : ACTIVE_SECS)
    expect(sorted[0].sessionId).toBe('perm')
    expect(sorted[1].sessionId).toBe('error')
    expect(sorted[2].sessionId).toBe('stalled')
  })

  it('within same weight, longer-waiting comes first', () => {
    const stalled1 = makeAgent({ status: 'active', sessionId: 'stalled-short' })
    const stalled2 = makeAgent({ status: 'active', sessionId: 'stalled-long' })
    const secsMap: Record<string, number> = { 'stalled-short': STALLED_SECS, 'stalled-long': STALLED_SECS + 120 }
    const sorted = sortByTriage([stalled1, stalled2], a => secsMap[a.sessionId] ?? 0)
    expect(sorted[0].sessionId).toBe('stalled-long')
  })

  it('preserves original order of non-attention agents', () => {
    const active1 = makeAgent({ status: 'active', sessionId: 'a1' })
    const active2 = makeAgent({ status: 'active', sessionId: 'a2' })
    const sorted = sortByTriage([active1, active2], () => ACTIVE_SECS)
    expect(sorted.map(a => a.sessionId)).toEqual(['a1', 'a2'])
  })
})
