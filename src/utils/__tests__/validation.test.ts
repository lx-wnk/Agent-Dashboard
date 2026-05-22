import { describe, expect, it } from 'vitest'
import {
  isAbsolutePath,
  isAllowedSpawnerCommand,
  SLUG_PATTERN_MESSAGE,
  SLUG_RE,
  slugify,
} from '../validation'

describe('SLUG_RE', () => {
  it('accepts valid slugs', () => {
    expect(SLUG_RE.test('a')).toBe(true)
    expect(SLUG_RE.test('alpha-beta-1')).toBe(true)
    expect(SLUG_RE.test('1abc')).toBe(true)
  })

  it('rejects invalid slugs', () => {
    expect(SLUG_RE.test('')).toBe(false)
    expect(SLUG_RE.test('-leading')).toBe(false)
    expect(SLUG_RE.test('A')).toBe(false)
    expect(SLUG_RE.test('with space')).toBe(false)
  })

  it('exports a human-readable message', () => {
    expect(SLUG_PATTERN_MESSAGE).toContain('slug')
  })
})

describe('slugify', () => {
  it('lowercases, trims, and collapses non-alphanumeric runs', () => {
    expect(slugify('  Hello World  ')).toBe('hello-world')
    expect(slugify('A__B--C')).toBe('a-b-c')
    expect(slugify('--leading and trailing--')).toBe('leading-and-trailing')
  })
})

describe('isAbsolutePath', () => {
  it('accepts absolute paths without traversal', () => {
    expect(isAbsolutePath('/foo/bar')).toBe(true)
    expect(isAbsolutePath('/')).toBe(true)
    expect(isAbsolutePath('/a/b/c.txt')).toBe(true)
  })

  it('rejects relative paths', () => {
    expect(isAbsolutePath('foo')).toBe(false)
    expect(isAbsolutePath('foo/bar')).toBe(false)
    expect(isAbsolutePath('./foo')).toBe(false)
  })

  it('rejects paths containing .. segments', () => {
    expect(isAbsolutePath('/foo/../bar')).toBe(false)
    expect(isAbsolutePath('/../etc')).toBe(false)
    expect(isAbsolutePath('/a/b/..')).toBe(false)
  })

  it('rejects empty input', () => {
    expect(isAbsolutePath('')).toBe(false)
  })
})

describe('isAllowedSpawnerCommand', () => {
  it('accepts bare allowed names', () => {
    expect(isAllowedSpawnerCommand('claude')).toBe(true)
    expect(isAllowedSpawnerCommand('claude-code')).toBe(true)
    expect(isAllowedSpawnerCommand('npx')).toBe(true)
  })

  it('accepts absolute paths outside tmp', () => {
    expect(isAllowedSpawnerCommand('/usr/local/bin/x')).toBe(true)
    expect(isAllowedSpawnerCommand('/opt/company/bin/runner')).toBe(true)
  })

  it('rejects absolute paths in /tmp or /var/tmp', () => {
    expect(isAllowedSpawnerCommand('/tmp/evil')).toBe(false)
    expect(isAllowedSpawnerCommand('/tmp')).toBe(false)
    expect(isAllowedSpawnerCommand('/var/tmp/x')).toBe(false)
    expect(isAllowedSpawnerCommand('/var/tmp')).toBe(false)
  })

  it('rejects unknown bare names', () => {
    expect(isAllowedSpawnerCommand('random')).toBe(false)
    expect(isAllowedSpawnerCommand('curl')).toBe(false)
  })

  it('rejects empty / whitespace input', () => {
    expect(isAllowedSpawnerCommand('')).toBe(false)
    expect(isAllowedSpawnerCommand('   ')).toBe(false)
  })
})
