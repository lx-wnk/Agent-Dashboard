import { describe, expect, it } from 'vitest'
import { decodeProjectDir } from './sessionScanner'

// decodeProjectDir logic (from source):
//   If the encoded string starts with '-', restore the leading '/' by
//   taking the slice from index 1 and replacing every remaining '-' with '/'.
//   Otherwise return the encoded string unchanged.
//
// Note: this is a lossy reverse — original '-' chars in dir names and
// underscores (encoded as '-' by encodePath) are all collapsed into '/'.

describe('decodeProjectDir', () => {
  it('restores a leading slash and converts dashes to slashes', () => {
    expect(decodeProjectDir('-Users-alex-project')).toBe('/Users/alex/project')
  })

  it('handles a single dash (encoded root "/")', () => {
    expect(decodeProjectDir('-')).toBe('/')
  })

  it('handles a deep nested path', () => {
    expect(decodeProjectDir('-home-user-code-privat-my-repo')).toBe('/home/user/code/privat/my/repo')
  })

  it('returns the input unchanged when it does not start with a dash', () => {
    expect(decodeProjectDir('Users-alex-project')).toBe('Users-alex-project')
  })

  it('returns an empty string unchanged', () => {
    expect(decodeProjectDir('')).toBe('')
  })

  it('handles encoded path with username containing underscores (encoded as dashes)', () => {
    // /Users/alex_smith/project → encodePath → -Users-alex-smith-project
    // decode: -Users-alex-smith-project → /Users/alex/smith/project
    // (lossiness acknowledged — dashes in original names become slashes)
    expect(decodeProjectDir('-Users-alex-smith-project')).toBe('/Users/alex/smith/project')
  })

  it('handles a two-segment path', () => {
    expect(decodeProjectDir('-code-repo')).toBe('/code/repo')
  })
})
