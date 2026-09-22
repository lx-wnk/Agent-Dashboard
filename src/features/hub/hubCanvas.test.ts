import type { HubNote } from './composables/useObsidianGraph'
import { describe, expect, it } from 'vitest'
import { cullLabels, hitNote, hubNoteSet, isToday, notePriority } from './hubCanvas'

describe('notePriority', () => {
  it('orders hub > touched > fresh > links', () => {
    const hub = notePriority({ hub: true, touched: false, fresh: false, linkCount: 0 })
    const touched = notePriority({ hub: false, touched: true, fresh: false, linkCount: 0 })
    const fresh = notePriority({ hub: false, touched: false, fresh: true, linkCount: 0 })
    const links = notePriority({ hub: false, touched: false, fresh: false, linkCount: 5 })
    expect(hub).toBeGreaterThan(touched)
    expect(touched).toBeGreaterThan(fresh)
    expect(fresh).toBeGreaterThan(links)
  })
})

describe('cullLabels', () => {
  it('keeps the higher-priority label of two overlapping ones', () => {
    const kept = cullLabels([
      { index: 0, sx: 100, sy: 100, text: 'a', priority: 10 },
      { index: 1, sx: 102, sy: 100, text: 'b', priority: 20 },
    ])
    expect(kept).toEqual(new Set([1]))
  })

  it('keeps both of two labels that are far apart', () => {
    const kept = cullLabels([
      { index: 0, sx: 0, sy: 0, text: 'a', priority: 10 },
      { index: 1, sx: 500, sy: 500, text: 'b', priority: 20 },
    ])
    expect(kept).toEqual(new Set([0, 1]))
  })
})

describe('hitNote', () => {
  const cam = { k: 1, tx: 0, ty: 0 }
  const points: Array<[number, number]> = [[0, 0], [100, 100]]

  it('returns the nearest note within 8px', () => {
    expect(hitNote(points, cam, 3, 4)).toBe(0)
  })

  it('returns -1 beyond 8px', () => {
    expect(hitNote(points, cam, 20, 20)).toBe(-1)
  })
})

describe('isToday', () => {
  it('is false for yesterday 23:59', () => {
    const now = new Date(2026, 8, 22, 0, 1).getTime()
    const yesterday = new Date(2026, 8, 21, 23, 59).getTime()
    expect(isToday(yesterday, now)).toBe(false)
  })

  it('is true for today 00:01', () => {
    const now = new Date(2026, 8, 22, 12, 0).getTime()
    const today = new Date(2026, 8, 22, 0, 1).getTime()
    expect(isToday(today, now)).toBe(true)
  })
})

describe('hubNoteSet', () => {
  function note(index: number, backlinks: number[]): HubNote {
    return { index, path: `n${index}.md`, title: `n${index}`, mtimeMs: 0, links: [], backlinks }
  }

  it('caps at 3 per sector and ignores notes with fewer than 2 backlinks', () => {
    const notes = [
      note(0, [1, 2, 3, 4]),
      note(1, [1, 2, 3]),
      note(2, [1, 2]),
      note(3, [1]),
      note(4, [1, 2, 3, 4, 5]),
    ]
    expect(hubNoteSet(notes, () => 'a')).toEqual(new Set([4, 0, 1]))
  })

  it('caps each sector independently', () => {
    const notes = [
      note(0, [1, 2]),
      note(1, [1, 2]),
      note(2, [1, 2]),
      note(3, [1, 2]),
    ]
    const sectorOf = (n: HubNote) => (n.index < 2 ? 'a' : 'b')
    expect(hubNoteSet(notes, sectorOf)).toEqual(new Set([0, 1, 2, 3]))
  })
})
