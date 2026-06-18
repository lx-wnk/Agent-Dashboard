import { describe, expect, it } from 'vitest'
import { AVAILABLE_MODELS } from './models'

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
