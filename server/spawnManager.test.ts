import { beforeEach, describe, expect, it } from 'bun:test'
import { SpawnManager } from './spawnManager.js'

// The module reads DASHBOARD_SPAWN_RATE_LIMIT and DASHBOARD_SPAWN_RATE_WINDOW_MS via IIFEs
// at import time — so defaults (5 max, 60 000 ms) are in effect for all tests.
// Each test creates a fresh SpawnManager instance to reset the sliding window.

const NON_EXISTENT_CWD = '/tmp/nonexistent-path-xyz-abc-123'

describe('SpawnManager.isSpawnAllowed', () => {
  it('returns true when no spawns have occurred yet', () => {
    const sm = new SpawnManager()
    expect(sm.isSpawnAllowed()).toBe(true)
  })

  it('returns true when spawn count is below the limit', () => {
    const sm = new SpawnManager()
    const now = Date.now()
    // Simulate 4 prior spawns inside the window (limit is 5)
    for (let i = 0; i < 4; i++) {
      sm.isSpawnAllowed(now) // prune pass
      // Manually consume a slot by spawning (validation fails fast, no real spawn)
      sm.spawnAgent({ prompt: 'x', cwd: NON_EXISTENT_CWD })
    }
    expect(sm.isSpawnAllowed(now)).toBe(true)
  })

  it('returns false after the rate limit is reached', () => {
    const sm = new SpawnManager()
    const now = Date.now()
    // Exhaust all 5 slots
    for (let i = 0; i < 5; i++) {
      sm.spawnAgent({ prompt: 'x', cwd: NON_EXISTENT_CWD })
    }
    expect(sm.isSpawnAllowed(now)).toBe(false)
  })

  it('returns true again after the window passes', () => {
    const sm = new SpawnManager()
    const now = Date.now()
    // Exhaust the limit at time `now`
    for (let i = 0; i < 5; i++) {
      sm.spawnAgent({ prompt: 'x', cwd: NON_EXISTENT_CWD })
    }
    expect(sm.isSpawnAllowed(now)).toBe(false)

    // Advance 60 001 ms beyond `now` — all window entries expire
    const future = now + 60_001
    expect(sm.isSpawnAllowed(future)).toBe(true)
  })
})

describe('SpawnManager.getRateLimitConfig', () => {
  it('returns the default window and max values', () => {
    const sm = new SpawnManager()
    const config = sm.getRateLimitConfig()
    expect(config.windowMs).toBe(60_000)
    expect(config.max).toBe(5)
  })
})

describe('SpawnManager.spawnAgent — validation errors', () => {
  let sm: SpawnManager

  beforeEach(() => {
    sm = new SpawnManager()
  })

  it('returns 400 for missing prompt', () => {
    const result = sm.spawnAgent({ cwd: '/tmp' })
    expect(result.ok).toBe(false)
    if (!result.ok) {
      expect(result.status).toBe(400)
      expect(result.error).toMatch(/prompt/i)
    }
  })

  it('returns 400 for non-string prompt', () => {
    const result = sm.spawnAgent({ prompt: 42 as unknown as string, cwd: '/tmp' })
    expect(result.ok).toBe(false)
    if (!result.ok) {
      expect(result.status).toBe(400)
    }
  })

  it('returns 400 for missing cwd', () => {
    const result = sm.spawnAgent({ prompt: 'hello' })
    expect(result.ok).toBe(false)
    if (!result.ok) {
      expect(result.status).toBe(400)
      expect(result.error).toMatch(/cwd/i)
    }
  })

  it('returns 400 for non-existent cwd directory', () => {
    const result = sm.spawnAgent({ prompt: 'hello', cwd: NON_EXISTENT_CWD })
    expect(result.ok).toBe(false)
    if (!result.ok) {
      expect(result.status).toBe(400)
      expect(result.error).toMatch(/does not exist/i)
    }
  })

  it('returns 400 for invalid model string', () => {
    const result = sm.spawnAgent({ prompt: 'hello', cwd: NON_EXISTENT_CWD, model: 'gpt-4' })
    expect(result.ok).toBe(false)
    if (!result.ok) {
      // cwd check fires first — still 400
      expect(result.status).toBe(400)
    }
  })

  it('returns 400 for invalid model after a valid cwd path', () => {
    // Use /tmp which exists so cwd passes, then model validation fails
    const result = sm.spawnAgent({ prompt: 'hello', cwd: '/tmp', model: 'not-a-real-model' })
    expect(result.ok).toBe(false)
    if (!result.ok) {
      expect(result.status).toBe(400)
      expect(result.error).toMatch(/model/i)
    }
  })

  it('returns 400 for invalid resumeSessionId (not a UUID)', () => {
    const result = sm.spawnAgent({
      prompt: 'hello',
      cwd: '/tmp',
      resumeSessionId: 'not-a-uuid',
    })
    expect(result.ok).toBe(false)
    if (!result.ok) {
      expect(result.status).toBe(400)
      expect(result.error).toMatch(/sessionId/i)
    }
  })

  it('returns 400 for resumeSessionId with wrong UUID format', () => {
    const result = sm.spawnAgent({
      prompt: 'hello',
      cwd: '/tmp',
      resumeSessionId: '12345678-1234-1234-1234-1234567890ZZ', // invalid hex
    })
    expect(result.ok).toBe(false)
    if (!result.ok) {
      expect(result.status).toBe(400)
    }
  })
})

describe('SpawnManager.storeReply + getReplies', () => {
  let sm: SpawnManager

  beforeEach(() => {
    sm = new SpawnManager()
  })

  it('round-trip: stores and retrieves 3 replies', () => {
    sm.storeReply(1, 'msg-1', '2024-01-01T00:00:01.000Z')
    sm.storeReply(1, 'msg-2', '2024-01-01T00:00:02.000Z')
    sm.storeReply(1, 'msg-3', '2024-01-01T00:00:03.000Z')

    const replies = sm.getReplies(1)
    expect(replies).toHaveLength(3)
    expect(replies[0].message).toBe('msg-1')
    expect(replies[1].message).toBe('msg-2')
    expect(replies[2].message).toBe('msg-3')
  })

  it('filters by `since` timestamp (exclusive)', () => {
    sm.storeReply(2, 'old', '2024-01-01T00:00:01.000Z')
    sm.storeReply(2, 'exact', '2024-01-01T00:00:02.000Z')
    sm.storeReply(2, 'new', '2024-01-01T00:00:03.000Z')

    // since='00:02' → should exclude 'old' and 'exact', return only 'new'
    const replies = sm.getReplies(2, '2024-01-01T00:00:02.000Z')
    expect(replies).toHaveLength(1)
    expect(replies[0].message).toBe('new')
  })

  it('returns empty array for unknown pid', () => {
    expect(sm.getReplies(9999)).toHaveLength(0)
  })

  it('returns all replies when since is not provided', () => {
    sm.storeReply(3, 'a', '2024-01-01T00:00:01.000Z')
    sm.storeReply(3, 'b', '2024-01-01T00:00:02.000Z')
    const all = sm.getReplies(3)
    expect(all).toHaveLength(2)
  })
})

describe('SpawnManager — ring-buffer cap (MAX_REPLIES_PER_PID = 50)', () => {
  it('evicts the oldest entry when more than 50 replies are stored', () => {
    const sm = new SpawnManager()
    const pid = 42

    // Store 51 replies; first message is 'reply-0'
    for (let i = 0; i <= 50; i++) {
      sm.storeReply(pid, `reply-${i}`, new Date(1_000_000 + i * 1000).toISOString())
    }

    const replies = sm.getReplies(pid)
    expect(replies).toHaveLength(50) // capped at 50

    // Oldest ('reply-0') must have been evicted
    expect(replies[0].message).toBe('reply-1')
    // Newest must be present
    expect(replies[replies.length - 1].message).toBe('reply-50')
  })
})
