import { describe, expect, it } from 'vitest'
import { computeHealthScore } from './healthScore.js'

const base = {
  completedTasks: 0,
  totalTasks: 0,
  cacheReadTokens: 0,
  inputTokens: 0,
  hasError: false,
  costEstimate: 0,
  recentAvgCost: 0,
}

describe('computeHealthScore', () => {
  it('returns 100 for a perfect agent (all tasks done, full cache hit rate, no error)', () => {
    // inputTokens=0 → cacheHitRate=100%; all tasks done → successRate=100%
    const score = computeHealthScore({
      ...base,
      completedTasks: 5,
      totalTasks: 5,
      cacheReadTokens: 1000,
      inputTokens: 0,
    })
    expect(score).toBe(100)
  })

  it('penalises 50% task failure rate correctly', () => {
    const score = computeHealthScore({ ...base, completedTasks: 5, totalTasks: 10 })
    // successRate=50 → 20, cacheHit=0 → 0, error=100 → 25, spike=100 → 10 = 55
    expect(score).toBe(55)
  })

  it('applies full cache hit weight when all tokens are cache reads', () => {
    const score = computeHealthScore({ ...base, cacheReadTokens: 1000, inputTokens: 0 })
    // successRate=0, cacheHit=100 → 25, error=100 → 25, spike=100 → 10 = 60
    expect(score).toBe(60)
  })

  it('sets error component to 0 when hasError is true', () => {
    const noError = computeHealthScore({ ...base, cacheReadTokens: 500, inputTokens: 500 })
    const withError = computeHealthScore({ ...base, cacheReadTokens: 500, inputTokens: 500, hasError: true })
    expect(noError - withError).toBe(25)
  })

  it('reduces cost spike score to 0 when costEstimate > 3x average', () => {
    const noSpike = computeHealthScore({ ...base, costEstimate: 1, recentAvgCost: 1 })
    const spike = computeHealthScore({ ...base, costEstimate: 4, recentAvgCost: 1 })
    expect(noSpike - spike).toBe(10)
  })

  it('ignores cost spike when recentAvgCost is 0 (no history)', () => {
    const score = computeHealthScore({ ...base, costEstimate: 999, recentAvgCost: 0 })
    // error=100 → 25, spike=100 → 10 = 35
    expect(score).toBe(35)
  })

  it('clamps result between 0 and 100', () => {
    const score = computeHealthScore({ ...base, hasError: true, completedTasks: 0, totalTasks: 100 })
    expect(score).toBeGreaterThanOrEqual(0)
    expect(score).toBeLessThanOrEqual(100)
  })
})
