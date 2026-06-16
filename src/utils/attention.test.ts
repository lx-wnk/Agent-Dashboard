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
  it('returns permission for waiting status', () => {
    const agent = makeAgent({ status: 'waiting' })
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

  it('returns stalled for active + long silence', () => {
    const agent = makeAgent({ status: 'active' })
    const att = attentionFor(agent, STALLED_SECS)
    expect(att?.kind).toBe('stalled')
    expect(att?.tone).toBe('warning')
    expect(att?.weight).toBe(2)
  })

  it('returns idle for idle status', () => {
    const agent = makeAgent({ status: 'idle' })
    const att = attentionFor(agent, ACTIVE_SECS)
    expect(att?.kind).toBe('idle')
    expect(att?.tone).toBe('info')
    expect(att?.weight).toBe(3)
  })

  it('returns null for healthy active agent', () => {
    const agent = makeAgent({ status: 'active' })
    expect(attentionFor(agent, ACTIVE_SECS)).toBeNull()
  })

  it('returns null when secondsSince is null for active agent', () => {
    const agent = makeAgent({ status: 'active' })
    expect(attentionFor(agent, null)).toBeNull()
  })

  it('waiting takes precedence over stalled seconds', () => {
    // a waiting agent with many silent seconds should classify as permission, not stalled
    const agent = makeAgent({ status: 'waiting' })
    const att = attentionFor(agent, STALLED_SECS)
    expect(att?.kind).toBe('permission')
  })

  it('errorState takes precedence over stalled', () => {
    // an active agent with errorState + long silence should classify as error, not stalled
    const agent = makeAgent({ status: 'active', errorState: 'rate_limited' })
    const att = attentionFor(agent, STALLED_SECS)
    expect(att?.kind).toBe('error')
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

  it('waiting agent with no pendingPermissions still classifies as permission', () => {
    const agent = makeAgent({ status: 'waiting', pendingPermissions: [] })
    const att = attentionFor(agent, ACTIVE_SECS)
    expect(att?.kind).toBe('permission')
  })
})

describe('needsAttention', () => {
  it('returns true for waiting agent', () => {
    expect(needsAttention(makeAgent({ status: 'waiting' }), ACTIVE_SECS)).toBe(true)
  })

  it('returns false for healthy active agent', () => {
    expect(needsAttention(makeAgent({ status: 'active' }), ACTIVE_SECS)).toBe(false)
  })
})

describe('sortByTriage', () => {
  it('places attention agents before non-attention agents', () => {
    const idle = makeAgent({ status: 'idle', sessionId: 'idle' })
    const active = makeAgent({ status: 'active', sessionId: 'active' })
    const sorted = sortByTriage([active, idle], () => ACTIVE_SECS)
    expect(sorted[0].sessionId).toBe('idle')
    expect(sorted[1].sessionId).toBe('active')
  })

  it('sorts by ascending weight within attention group', () => {
    const waiting = makeAgent({ status: 'waiting', sessionId: 'waiting' })
    const idle = makeAgent({ status: 'idle', sessionId: 'idle' })
    const stalled = makeAgent({ status: 'active', sessionId: 'stalled' })
    const error = makeAgent({ status: 'active', sessionId: 'error', errorState: 'quota_exhausted' })
    const sorted = sortByTriage([idle, stalled, error, waiting], a =>
      a.sessionId === 'stalled' ? STALLED_SECS : ACTIVE_SECS)
    expect(sorted[0].sessionId).toBe('waiting')
    expect(sorted[1].sessionId).toBe('error')
    expect(sorted[2].sessionId).toBe('stalled')
    expect(sorted[3].sessionId).toBe('idle')
  })

  it('within same weight, longer-waiting comes first', () => {
    const idle1 = makeAgent({ status: 'idle', sessionId: 'idle-short' })
    const idle2 = makeAgent({ status: 'idle', sessionId: 'idle-long' })
    const secsMap: Record<string, number> = { 'idle-short': 60, 'idle-long': 300 }
    const sorted = sortByTriage([idle1, idle2], a => secsMap[a.sessionId] ?? 0)
    expect(sorted[0].sessionId).toBe('idle-long')
  })

  it('preserves original order of non-attention agents', () => {
    const active1 = makeAgent({ status: 'active', sessionId: 'a1' })
    const active2 = makeAgent({ status: 'active', sessionId: 'a2' })
    const sorted = sortByTriage([active1, active2], () => ACTIVE_SECS)
    expect(sorted.map(a => a.sessionId)).toEqual(['a1', 'a2'])
  })
})
