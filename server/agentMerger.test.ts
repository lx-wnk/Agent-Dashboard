import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { calculateStatus } from './agentMerger'

// Thresholds from agentMerger.ts:
//   ACTIVE_THRESHOLD = 30_000  (30s)
//   IDLE_THRESHOLD   = 300_000 (5min)

const FIXED_NOW = new Date('2024-06-01T12:00:00.000Z').getTime()

describe('calculateStatus', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(FIXED_NOW)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('returns "active" when last activity is less than 30 seconds ago', () => {
    const lastActivity = new Date(FIXED_NOW - 10_000).toISOString() // 10s ago
    expect(calculateStatus(lastActivity)).toBe('active')
  })

  it('returns "active" at exactly 0ms age', () => {
    const lastActivity = new Date(FIXED_NOW).toISOString()
    expect(calculateStatus(lastActivity)).toBe('active')
  })

  it('returns "active" at 29 999ms (just under threshold)', () => {
    const lastActivity = new Date(FIXED_NOW - 29_999).toISOString()
    expect(calculateStatus(lastActivity)).toBe('active')
  })

  it('returns "waiting" at exactly the active threshold (30 000ms)', () => {
    const lastActivity = new Date(FIXED_NOW - 30_000).toISOString()
    expect(calculateStatus(lastActivity)).toBe('waiting')
  })

  it('returns "waiting" when last activity is between 30s and 5 minutes ago', () => {
    const lastActivity = new Date(FIXED_NOW - 120_000).toISOString() // 2min ago
    expect(calculateStatus(lastActivity)).toBe('waiting')
  })

  it('returns "waiting" at 299 999ms (just under idle threshold)', () => {
    const lastActivity = new Date(FIXED_NOW - 299_999).toISOString()
    expect(calculateStatus(lastActivity)).toBe('waiting')
  })

  it('returns "idle" at exactly the idle threshold (300 000ms)', () => {
    const lastActivity = new Date(FIXED_NOW - 300_000).toISOString()
    expect(calculateStatus(lastActivity)).toBe('idle')
  })

  it('returns "idle" when last activity is more than 5 minutes ago', () => {
    const lastActivity = new Date(FIXED_NOW - 600_000).toISOString() // 10min ago
    expect(calculateStatus(lastActivity)).toBe('idle')
  })

  it('returns "idle" for a very old timestamp', () => {
    const lastActivity = new Date(0).toISOString() // Unix epoch
    expect(calculateStatus(lastActivity)).toBe('idle')
  })
})
