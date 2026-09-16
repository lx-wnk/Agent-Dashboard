import { describe, expect, it } from 'vitest'
import { AVAILABLE_MODELS, compareModelVersions, latestModel } from './models'

describe('aVAILABLE_MODELS', () => {
  it('is non-empty', () => {
    expect(AVAILABLE_MODELS.length).toBeGreaterThan(0)
  })

  it('contains claude-sonnet-4-6', () => {
    expect(AVAILABLE_MODELS).toContain('claude-sonnet-4-6')
  })

  it('contains claude-opus-4-6', () => {
    expect(AVAILABLE_MODELS).toContain('claude-opus-4-6')
  })

  it('contains claude-haiku-4-5', () => {
    expect(AVAILABLE_MODELS).toContain('claude-haiku-4-5')
  })

  it('all entries start with claude-', () => {
    AVAILABLE_MODELS.forEach((model) => {
      expect(model).toMatch(/^claude-/)
    })
  })

  it('contains no duplicate entries', () => {
    const unique = new Set(AVAILABLE_MODELS)
    expect(unique.size).toBe(AVAILABLE_MODELS.length)
  })

  it('all entries are non-empty strings', () => {
    AVAILABLE_MODELS.forEach((model) => {
      expect(typeof model).toBe('string')
      expect(model.length).toBeGreaterThan(0)
    })
  })
})

describe('compareModelVersions', () => {
  it('compares version parts as numbers, not text', () => {
    expect(compareModelVersions('4-10', '4-8')).toBeGreaterThan(0)
    expect(compareModelVersions('5', '4-8')).toBeGreaterThan(0)
  })

  it('sorts a version before its own longer successor', () => {
    expect(compareModelVersions('5', '5-1')).toBeLessThan(0)
  })
})

describe('latestModel', () => {
  it.each(['opus', 'sonnet', 'haiku', 'fable'] as const)('returns the newest %s model in the list', (series) => {
    const prefix = `claude-${series}-`
    const latest = latestModel(series)
    expect(latest.startsWith(prefix)).toBe(true)
    for (const id of AVAILABLE_MODELS.filter(m => m.startsWith(prefix)))
      expect(compareModelVersions(id.slice(prefix.length), latest.slice(prefix.length))).toBeLessThanOrEqual(0)
  })
})
