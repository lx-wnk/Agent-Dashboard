import { describe, expect, it } from 'vitest'
import { estimateCost, MODEL_PRICING } from './pricing'

describe('mODEL_PRICING', () => {
  it('defines entries for all expected models', () => {
    const expected = [
      'claude-opus-4-6',
      'claude-opus-4-0',
      'claude-sonnet-4-6',
      'claude-sonnet-4-5',
      'claude-haiku-4-5',
    ]
    for (const model of expected) {
      expect(MODEL_PRICING).toHaveProperty(model)
    }
  })

  it('every entry has strictly positive input and output prices', () => {
    for (const [model, pricing] of Object.entries(MODEL_PRICING)) {
      expect(pricing.input, `${model} input price`).toBeGreaterThan(0)
      expect(pricing.output, `${model} output price`).toBeGreaterThan(0)
    }
  })

  it('every entry has positive cacheRead and cacheCreate prices', () => {
    for (const [model, pricing] of Object.entries(MODEL_PRICING)) {
      expect(pricing.cacheRead, `${model} cacheRead price`).toBeGreaterThan(0)
      expect(pricing.cacheCreate, `${model} cacheCreate price`).toBeGreaterThan(0)
    }
  })

  it('opus tiers cost more than sonnet, which costs more than haiku', () => {
    expect(MODEL_PRICING['claude-opus-4-6'].input).toBeGreaterThan(MODEL_PRICING['claude-sonnet-4-6'].input)
    expect(MODEL_PRICING['claude-sonnet-4-6'].input).toBeGreaterThan(MODEL_PRICING['claude-haiku-4-5'].input)
    expect(MODEL_PRICING['claude-opus-4-6'].output).toBeGreaterThan(MODEL_PRICING['claude-sonnet-4-6'].output)
    expect(MODEL_PRICING['claude-sonnet-4-6'].output).toBeGreaterThan(MODEL_PRICING['claude-haiku-4-5'].output)
  })
})

describe('estimateCost', () => {
  const SONNET_PRICING = MODEL_PRICING['claude-sonnet-4-6']

  it('calculates cost correctly for a known model', () => {
    const usage = { inputTokens: 1_000_000, outputTokens: 1_000_000 }
    const expected = SONNET_PRICING.input + SONNET_PRICING.output
    expect(estimateCost(usage, 'claude-sonnet-4-6')).toBeCloseTo(expected, 6)
  })

  it('uses haiku pricing when haiku model is specified', () => {
    const usage = { inputTokens: 1_000_000, outputTokens: 0 }
    const expected = MODEL_PRICING['claude-haiku-4-5'].input
    expect(estimateCost(usage, 'claude-haiku-4-5')).toBeCloseTo(expected, 6)
  })

  it('falls back to sonnet pricing for an unknown model string', () => {
    const usage = { inputTokens: 1_000_000, outputTokens: 1_000_000 }
    const expectedWithSonnet = estimateCost(usage, 'claude-sonnet-4-6')
    expect(estimateCost(usage, 'claude-unknown-99')).toBeCloseTo(expectedWithSonnet, 6)
  })

  it('falls back to sonnet pricing when model is null', () => {
    const usage = { inputTokens: 1_000_000, outputTokens: 1_000_000 }
    const expectedWithSonnet = estimateCost(usage, 'claude-sonnet-4-6')
    expect(estimateCost(usage, null)).toBeCloseTo(expectedWithSonnet, 6)
  })

  it('returns 0 when all token counts are zero', () => {
    expect(estimateCost({ inputTokens: 0, outputTokens: 0 }, 'claude-sonnet-4-6')).toBe(0)
    expect(estimateCost({ inputTokens: 0, outputTokens: 0 }, null)).toBe(0)
  })

  it('includes cacheReadTokens in the cost calculation', () => {
    const usageWithCache = {
      inputTokens: 0,
      outputTokens: 0,
      cacheReadTokens: 1_000_000,
    }
    const expected = SONNET_PRICING.cacheRead
    expect(estimateCost(usageWithCache, 'claude-sonnet-4-6')).toBeCloseTo(expected, 6)
  })

  it('includes cacheCreationTokens in the cost calculation', () => {
    const usageWithCache = {
      inputTokens: 0,
      outputTokens: 0,
      cacheCreationTokens: 1_000_000,
    }
    const expected = SONNET_PRICING.cacheCreate
    expect(estimateCost(usageWithCache, 'claude-sonnet-4-6')).toBeCloseTo(expected, 6)
  })

  it('treats absent cache fields as zero', () => {
    const withoutCache = { inputTokens: 500, outputTokens: 500 }
    const withExplicitZeros = {
      inputTokens: 500,
      outputTokens: 500,
      cacheReadTokens: 0,
      cacheCreationTokens: 0,
    }
    expect(estimateCost(withoutCache, 'claude-sonnet-4-6'))
      .toBeCloseTo(estimateCost(withExplicitZeros, 'claude-sonnet-4-6'), 10)
  })

  it('calculates combined input+output+cache cost correctly', () => {
    const usage = {
      inputTokens: 1_000_000,
      outputTokens: 1_000_000,
      cacheReadTokens: 1_000_000,
      cacheCreationTokens: 1_000_000,
    }
    const expected
      = SONNET_PRICING.input
        + SONNET_PRICING.output
        + SONNET_PRICING.cacheRead
        + SONNET_PRICING.cacheCreate
    expect(estimateCost(usage, 'claude-sonnet-4-6')).toBeCloseTo(expected, 6)
  })

  it('returns a positive number for any non-zero input', () => {
    expect(estimateCost({ inputTokens: 1, outputTokens: 0 }, 'claude-opus-4-6')).toBeGreaterThan(0)
    expect(estimateCost({ inputTokens: 0, outputTokens: 1 }, 'claude-haiku-4-5')).toBeGreaterThan(0)
  })
})
