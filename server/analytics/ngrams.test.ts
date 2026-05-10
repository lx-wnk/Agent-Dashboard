import { describe, expect, it } from 'bun:test'
import { extractNgrams } from './ngrams.js'

describe('extractNgrams', () => {
  it('extracts trigrams from a sequence', () => {
    const seq = ['Read', 'Grep', 'Write', 'Bash']
    const counts = extractNgrams(seq)
    expect(counts.get('Read → Grep → Write')).toBe(1)
    expect(counts.get('Grep → Write → Bash')).toBe(1)
    expect(counts.size).toBe(2)
  })

  it('counts repeated trigrams', () => {
    const seq = ['Read', 'Write', 'Bash', 'Read', 'Write', 'Bash']
    const counts = extractNgrams(seq)
    expect(counts.get('Read → Write → Bash')).toBe(2)
  })

  it('returns empty map for short sequences', () => {
    expect(extractNgrams(['Read', 'Write']).size).toBe(0)
  })
})
