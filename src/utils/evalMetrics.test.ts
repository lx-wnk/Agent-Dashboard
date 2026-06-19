import { describe, expect, it } from 'vitest'
import { METRIC_KEYS, METRIC_LABELS, metricLabel } from './evalMetrics'

describe('evalMetrics', () => {
  it('mETRIC_KEYS contains all 9 keys', () => {
    expect(METRIC_KEYS).toHaveLength(9)
  })

  it('mETRIC_LABELS has an entry for every key in METRIC_KEYS', () => {
    for (const key of METRIC_KEYS)
      expect(METRIC_LABELS[key]).toBeTruthy()
  })

  it('metricLabel returns the human label for a known key', () => {
    expect(metricLabel('success_rate')).toBe('Success rate')
    expect(metricLabel('mean_cost_cents')).toBe('Mean cost (¢)')
  })

  it('metricLabel falls back to the raw key for unknown input', () => {
    expect(metricLabel('unknown_metric')).toBe('unknown_metric')
  })

  it('every METRIC_LABELS entry is a non-empty string', () => {
    for (const value of Object.values(METRIC_LABELS))
      expect(typeof value).toBe('string')
  })
})
