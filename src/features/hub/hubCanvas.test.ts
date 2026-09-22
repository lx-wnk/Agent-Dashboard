import type { HubNote } from './composables/useObsidianGraph'
import type { LabelCandidate } from './hubCanvas'
import { describe, expect, it } from 'vitest'
import { labelSize } from './__tests__/labelMeasurement'
import { agentDotBox, agentLabelBox, agentLabelKey, agentPriority, boxesOverlap, cullLabels, hitNote, hubNoteSet, isToday, notePriority, sectorLabelBox, sectorLabelKey } from './hubCanvas'

const measured = (c: LabelCandidate) => agentLabelBox(c, labelSize(c.text))

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

  it('places boxes with a caller-supplied shape instead of the note default', () => {
    const kept = cullLabels([
      { index: 0, sx: 100, sy: 100, text: 'a', priority: 10 },
      { index: 1, sx: 100, sy: 500, text: 'b', priority: 20 },
    ], measured)
    expect(kept).toEqual(new Set([0, 1]))
  })

  it('culls from the measured width where a character-count estimate would have kept both', () => {
    const text = 'Kontor Hub Working'
    const estimated = text.length * 6.3 + 24 // the estimate this unit replaced
    const apart = estimated + 10 // clear under the estimate, overlapping once really measured
    const kept = cullLabels([
      { index: 1, sx: 0, sy: 0, text, priority: 1 },
      { index: 2, sx: apart, sy: 0, text, priority: 0 },
    ], c => agentLabelBox(c, { w: estimated + 40, h: 16 }))
    expect(kept).toEqual(new Set([1]))
  })

  it('draws a label whose size is not measured yet and lets it block nothing', () => {
    const unmeasured = { index: 1, sx: 100, sy: 100, text: 'Kontor Hub Working', priority: 10 }
    const neighbour = { index: 2, sx: 104, sy: 100, text: 'Web App Idle', priority: 0 }
    const boxOf = (c: LabelCandidate) => c.index === 1 ? agentLabelBox(c) : measured(c)
    expect(cullLabels([unmeasured, neighbour], boxOf)).toEqual(new Set([1, 2]))
    expect(cullLabels([unmeasured], boxOf, [{ box: agentDotBox(100, 112) }])).toEqual(new Set([1]))
  })
})

describe('agentPriority', () => {
  it('ranks needs-operator above working above the rest', () => {
    const needsOperator = agentPriority(true, false)
    const working = agentPriority(false, true)
    const rest = agentPriority(false, false)
    expect(needsOperator).toBeGreaterThan(working)
    expect(working).toBeGreaterThan(rest)
  })
})

describe('agentLabelBox', () => {
  it('centres the measured box under the dot', () => {
    const box = agentLabelBox({ index: 0, sx: 100, sy: 100, text: 'Kontor Hub', priority: 0 }, { w: 80, h: 16 })
    expect(box.x + box.w / 2).toBeCloseTo(100)
    expect(box.y).toBeGreaterThan(100)
    expect(box.w).toBe(80)
    expect(box.h).toBe(16)
  })

  it('has no box at all while its size is unmeasured', () => {
    const box = agentLabelBox({ index: 0, sx: 100, sy: 100, text: 'Kontor Hub', priority: 0 })
    expect(boxesOverlap(box, agentDotBox(100, 112))).toBe(false)
  })
})

describe('cullLabels with agent boxes', () => {
  it('keeps only the highest-priority label of three agents whose boxes would collide', () => {
    const kept = cullLabels([
      { index: 1, sx: 100, sy: 100, text: 'Kontor Hub', priority: 0 },
      { index: 2, sx: 106, sy: 100, text: 'Web App', priority: 2 },
      { index: 3, sx: 112, sy: 100, text: 'Api Server', priority: 1 },
    ], measured)
    expect(kept).toEqual(new Set([2]))
  })

  it('always keeps a needs-operator agent even when it collides with others', () => {
    const kept = cullLabels([
      { index: 1, sx: 100, sy: 100, text: 'A', priority: agentPriority(true, false) },
      { index: 2, sx: 101, sy: 100, text: 'B', priority: agentPriority(false, true) },
      { index: 3, sx: 102, sy: 100, text: 'C', priority: agentPriority(false, false) },
    ], measured)
    expect(kept).toEqual(new Set([1]))
  })

  it('culls a label that would land on another agent\'s dot (defect 2)', () => {
    // Agent 2's dot sits right where agent 1's label would be drawn.
    const candidate = { index: 1, sx: 100, sy: 100, text: 'Kontor Hub', priority: 0 }
    const dotBox = agentDotBox(100, measured(candidate).y + 8)
    const kept = cullLabels([candidate], measured, [{ box: dotBox, ownerIndex: 2 }])
    expect(kept).toEqual(new Set())
  })

  it('never blocks a label with its own dot', () => {
    const candidate = { index: 1, sx: 100, sy: 100, text: 'Kontor Hub', priority: 0 }
    const ownDot = agentDotBox(candidate.sx, candidate.sy + 15) // close enough to the label to collide if not excluded
    const kept = cullLabels([candidate], measured, [{ box: ownDot, ownerIndex: 1 }])
    expect(kept).toEqual(new Set([1]))
  })

  it('culls a label that would land on a sector name (defect 3)', () => {
    const sector = sectorLabelBox(100, 116, labelSize(sectorLabelKey('Other', 4)))
    const kept = cullLabels([{ index: 1, sx: 100, sy: 100, text: 'Kontor Hub', priority: 0 }], measured, [{ box: sector }])
    expect(kept).toEqual(new Set())
  })
})

describe('agentDotBox', () => {
  it('centres an 18px box on the screen point', () => {
    const box = agentDotBox(100, 100)
    expect(box).toEqual({ x: 91, y: 91, w: 18, h: 18 })
  })
})

describe('sectorLabelBox', () => {
  it('centres the measured box on the screen point', () => {
    const box = sectorLabelBox(100, 100, { w: 84, h: 18 })
    expect(box.x + box.w / 2).toBeCloseTo(100)
    expect(box.y + box.h / 2).toBeCloseTo(100)
    expect(box).toEqual({ x: 58, y: 91, w: 84, h: 18 })
  })
})

describe('label keys', () => {
  it('carries the status word an agent label renders next to its name', () => {
    expect(agentLabelKey('kontor-hub', 'working')).toBe('Kontor Hub Working')
  })

  it('carries the weight badge a sector name renders', () => {
    expect(sectorLabelKey('Other', 4)).toBe('Other 4')
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
