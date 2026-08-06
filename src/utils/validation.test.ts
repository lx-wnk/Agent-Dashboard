import { describe, expect, it } from 'vitest'
import { MAX_DESCRIPTION_CHARS, SLUG_PATTERN_MESSAGE, SLUG_RE, slugFollowingName, slugify } from './validation'

describe('sLUG_RE', () => {
  it('accepts valid lowercase slugs', () => {
    expect(SLUG_RE.test('my-slug')).toBe(true)
    expect(SLUG_RE.test('a')).toBe(true)
    expect(SLUG_RE.test('abc123')).toBe(true)
    expect(SLUG_RE.test('fix-login-bug')).toBe(true)
  })

  it('rejects slugs that start with a hyphen', () => {
    expect(SLUG_RE.test('-slug')).toBe(false)
  })

  it('rejects empty string', () => {
    expect(SLUG_RE.test('')).toBe(false)
  })

  it('rejects uppercase letters', () => {
    expect(SLUG_RE.test('MySlug')).toBe(false)
    expect(SLUG_RE.test('MY-SLUG')).toBe(false)
  })

  it('rejects slugs with spaces', () => {
    expect(SLUG_RE.test('my slug')).toBe(false)
  })

  it('rejects slugs with special characters', () => {
    expect(SLUG_RE.test('my_slug')).toBe(false)
    expect(SLUG_RE.test('my.slug')).toBe(false)
    expect(SLUG_RE.test('my/slug')).toBe(false)
  })

  it('rejects slugs longer than 64 characters', () => {
    // Pattern: ^[a-z0-9][a-z0-9-]{0,63}$ — total max length is 64 chars.
    const maxValid = `a${'b'.repeat(63)}` // 64 chars
    expect(SLUG_RE.test(maxValid)).toBe(true)
    const tooLong = `a${'b'.repeat(64)}` // 65 chars
    expect(SLUG_RE.test(tooLong)).toBe(false)
  })

  it('accepts hyphens in the middle and at the end', () => {
    expect(SLUG_RE.test('a-')).toBe(true)
    expect(SLUG_RE.test('a-b-c-')).toBe(true)
  })
})

describe('sLUG_PATTERN_MESSAGE', () => {
  it('is a non-empty string', () => {
    expect(typeof SLUG_PATTERN_MESSAGE).toBe('string')
    expect(SLUG_PATTERN_MESSAGE.length).toBeGreaterThan(0)
  })
})

describe('slugFollowingName', () => {
  it('derives the slug while it is still empty', () => {
    expect(slugFollowingName('', 'DIW-ReviewApps', '')).toBe('diw-reviewapps')
  })

  it('keeps following the name once derived', () => {
    expect(slugFollowingName('DIW', 'DIW-ReviewApps', 'diw')).toBe('diw-reviewapps')
  })

  it('leaves a hand-edited slug alone', () => {
    expect(slugFollowingName('DIW', 'DIW-ReviewApps', 'my-own-slug')).toBe('my-own-slug')
  })

  it('produces a slug that satisfies the shared pattern', () => {
    expect(slugFollowingName('', 'DIW-ReviewApps', '')).toMatch(SLUG_RE)
    expect(slugFollowingName('', '  Ünicode & Symbols!  ', '')).toMatch(SLUG_RE)
  })

  it('produces a slug that satisfies the shared pattern for a name past the length cap', () => {
    const longName = 'Dashboard Rewrite Realtime Agent Telemetry And Cost Attribution Across Providers'
    expect(longName.length).toBeGreaterThan(64)
    expect(slugFollowingName('', longName, '')).toMatch(SLUG_RE)
  })

  it('empties the slug when the name is cleared, so it can pick up the next name', () => {
    expect(slugFollowingName('Web', '', 'web')).toBe('')
    expect(slugFollowingName('', 'API', '')).toBe('api')
  })
})

describe('slugify', () => {
  it('lowercases the input', () => {
    expect(slugify('MyProject')).toBe('myproject')
  })

  it('replaces spaces with hyphens', () => {
    expect(slugify('my project name')).toBe('my-project-name')
  })

  it('replaces underscores and dots with hyphens', () => {
    expect(slugify('my_project.name')).toBe('my-project-name')
  })

  it('collapses consecutive non-alphanumeric chars into a single hyphen', () => {
    expect(slugify('hello   world')).toBe('hello-world')
    expect(slugify('a--b')).toBe('a-b')
  })

  it('strips leading and trailing whitespace/hyphens', () => {
    expect(slugify(' my project ')).toBe('my-project')
    expect(slugify('  leading')).toBe('leading')
  })

  it('handles empty string', () => {
    expect(slugify('')).toBe('')
  })

  it('handles string with only special chars', () => {
    expect(slugify('---')).toBe('')
    expect(slugify('...')).toBe('')
  })

  it('caps the slug at the 64 characters SLUG_RE allows', () => {
    const slug = slugify('a'.repeat(200))
    expect(slug).toHaveLength(64)
    expect(slug).toMatch(SLUG_RE)
  })

  it('never leaves a trailing hyphen when the cap lands on a separator', () => {
    // 63 chars, then a space: the cut falls exactly on the derived hyphen.
    const slug = slugify(`${'a'.repeat(63)} tail`)
    expect(slug).toBe('a'.repeat(63))
    expect(slug).toMatch(SLUG_RE)
  })

  it('produces a slug that satisfies SLUG_RE for common inputs', () => {
    const inputs = ['My Project', 'Fix Login Bug', 'test_suite 2.0']
    inputs.forEach((input) => {
      const slug = slugify(input)
      if (slug.length > 0) {
        expect(SLUG_RE.test(slug)).toBe(true)
      }
    })
  })
})

describe('mAX_DESCRIPTION_CHARS', () => {
  it('is a positive integer', () => {
    expect(MAX_DESCRIPTION_CHARS).toBeGreaterThan(0)
    expect(Number.isInteger(MAX_DESCRIPTION_CHARS)).toBe(true)
  })

  it('is 10000', () => {
    expect(MAX_DESCRIPTION_CHARS).toBe(10_000)
  })
})
