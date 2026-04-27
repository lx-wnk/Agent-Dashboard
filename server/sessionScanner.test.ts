import { describe, expect, it } from 'vitest'
import { decodeProjectDir } from './sessionScanner'

// decodeProjectDir contract:
//   Returns the raw encoded string unchanged. Decoding is lossy because
//   `/`, `.`, and `_` are all encoded as `-`, so any attempt to reverse
//   the encoding fabricates a path that may not exist on disk. The call
//   site already prefers `headInfo.cwd` (the actual filesystem path read
//   from JSONL); this fallback is reached only when the JSONL is
//   unreadable, in which case showing the encoded form is more honest
//   than guessing.

describe('decodeProjectDir', () => {
  it('returns the encoded string unchanged for a leading-dash path', () => {
    expect(decodeProjectDir('-Users-alex-project')).toBe('-Users-alex-project')
  })

  it('returns a single dash unchanged', () => {
    expect(decodeProjectDir('-')).toBe('-')
  })

  it('returns a deep nested encoded path unchanged', () => {
    expect(decodeProjectDir('-home-user-code-privat-my-repo')).toBe('-home-user-code-privat-my-repo')
  })

  it('returns input without leading dash unchanged', () => {
    expect(decodeProjectDir('Users-alex-project')).toBe('Users-alex-project')
  })

  it('returns an empty string unchanged', () => {
    expect(decodeProjectDir('')).toBe('')
  })

  it('does not fabricate slashes for ambiguous segments (e.g. underscores)', () => {
    // Previously this would have produced '/Users/alex/smith/project',
    // which is not the real path /Users/alex_smith/project. The new
    // contract returns the raw encoded form so callers don't act on a
    // wrong path.
    expect(decodeProjectDir('-Users-alex-smith-project')).toBe('-Users-alex-smith-project')
  })

  it('returns a two-segment encoded path unchanged', () => {
    expect(decodeProjectDir('-code-repo')).toBe('-code-repo')
  })
})
