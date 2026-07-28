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

  it('returns permission when pendingToolUse is set', () => {
    const agent = makeAgent({
      status: 'active',
      pendingToolUse: { tool: 'Bash', pattern: 'git push', id: 'tu_1' },
    })
    const att = attentionFor(agent, ACTIVE_SECS)
    expect(att?.kind).toBe('permission')
    expect(att?.tone).toBe('warning')
    expect(att?.weight).toBe(0)
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

  it('returns true for agent with pendingToolUse', () => {
    const agent = makeAgent({
      pendingToolUse: { tool: 'Bash', pattern: 'rm -rf', id: 'tu_3' },
    })
    expect(needsAttention(agent, ACTIVE_SECS)).toBe(true)
  })

  it('returns false for idle agent', () => {
    expect(needsAttention(makeAgent({ status: 'idle' }), ACTIVE_SECS)).toBe(false)
  })

  it('returns false for healthy active agent', () => {
    expect(needsAttention(makeAgent({ status: 'active' }), ACTIVE_SECS)).toBe(false)
  })
})

describe('sortByTriage', () => {
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
