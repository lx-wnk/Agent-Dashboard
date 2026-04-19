import { describe, expect, it } from 'vitest'
import { recommendParallelism } from './resourceRecommender.js'

describe('recommendParallelism', () => {
  it('returns a sane recommendation for the current host', () => {
    const rec = recommendParallelism()
    expect(rec.recommended).toBeGreaterThanOrEqual(1)
    expect(rec.recommended).toBeLessThanOrEqual(5)
    expect(rec.reason).toBeTruthy()
    expect(rec.details.cpuCount).toBeGreaterThan(0)
    expect(rec.details.freeRamGb).toBeGreaterThanOrEqual(0)
  })

  it('has a coherent reason string matching the limiting factor', () => {
    const rec = recommendParallelism()
    const { ramRecommended, cpuRecommended } = rec.details
    if (ramRecommended < cpuRecommended && ramRecommended < 5)
      expect(rec.reason).toContain('RAM-limited')
    else if (cpuRecommended < 5)
      expect(rec.reason).toContain('CPU-limited')
    else
      expect(rec.reason).toContain('Capped')
  })
})
