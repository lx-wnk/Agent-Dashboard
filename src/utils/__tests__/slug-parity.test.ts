import { existsSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { SLUG_PATTERN_MESSAGE, SLUG_RE } from '../validation'

const GO_SLUG_PATH = join(process.cwd(), 'server/internal/validation/slug.go')

function readGoSource(): string {
  if (!existsSync(GO_SLUG_PATH)) {
    throw new Error(
      `slug-parity: Go source not found at ${GO_SLUG_PATH} — file moved? Update GO_SLUG_PATH.`,
    )
  }
  return readFileSync(GO_SLUG_PATH, 'utf-8')
}

function extractGoPattern(source: string): string {
  const match = source.match(/SlugRE = regexp\.MustCompile\(`([^`]+)`\)/)
  if (!match) {
    throw new Error(
      `slug-parity: could not find "SlugRE = regexp.MustCompile(\`...\`)" in ${GO_SLUG_PATH} — pattern declaration changed shape.`,
    )
  }
  return match[1]
}

function extractGoMessage(source: string): string {
  const match = source.match(/SlugPatternMessage = "([^"]+)"/)
  if (!match) {
    throw new Error(
      `slug-parity: could not find "SlugPatternMessage = \"...\"" in ${GO_SLUG_PATH} — declaration changed shape.`,
    )
  }
  return match[1]
}

const SLUG_CASES: Array<[string, boolean]> = [
  ['a', true],
  ['alpha-beta-1', true],
  ['1abc', true],
  ['a'.repeat(64), true],
  ['', false],
  ['-leading', false],
  ['A', false],
  ['with space', false],
  ['a'.repeat(65), false],
  ['trailing-', true],
]

describe('slug pattern parity (Go <-> TS)', () => {
  const goSource = readGoSource()
  const goPattern = extractGoPattern(goSource)
  const goMessage = extractGoMessage(goSource)

  it('go SlugRE literal matches TS SLUG_RE.source', () => {
    expect(goPattern).toBe(SLUG_RE.source)
  })

  it('go SlugPatternMessage matches TS SLUG_PATTERN_MESSAGE', () => {
    expect(goMessage).toBe(SLUG_PATTERN_MESSAGE)
  })

  it.each(SLUG_CASES)('ts SLUG_RE and Go pattern agree on %s -> %s', (input, expected) => {
    const goRegex = new RegExp(goPattern)
    expect(SLUG_RE.test(input)).toBe(expected)
    expect(goRegex.test(input)).toBe(expected)
  })
})
