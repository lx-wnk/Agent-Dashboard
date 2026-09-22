import { describe, expect, it } from 'vitest'
import {
  AGENT_FLOOR_PX,
  AGENT_SECTOR_STAGGER_PX,
  AGENT_SPACING_PX,
  AGENT_STAGE_MARGIN_PX,
  AGENT_WAITING_FLOOR_PX,
  agentAngles,
  agentRadius,
  agentRingPx,
  agentSectorRingPx,
  buildSectors,
  hash01,
  MAX_AGE_DAYS,
  notePoint,
  OTHER_SECTOR_KEY,
  planSectors,
  R0,
  R_MAX,
  radiusForAge,
  RINGS,
  SECTOR_FLOOR_DEG,
  SECTOR_LABEL_AGENT_CLEARANCE_PX,
  SECTOR_LABEL_RADIUS,
  sectorKeyFor,
  sectorLabelRadius,
  sectorTiers,
  visibleRingLabels,
} from './hubGeometry'

const span = (s: { start: number, end: number }) => s.end - s.start

describe('radiusForAge', () => {
  it('starts at r0, grows with age and stops at rMax after two years', () => {
    expect(radiusForAge(0)).toBe(R0)
    expect(radiusForAge(1)).toBeGreaterThan(radiusForAge(0.5))
    expect(radiusForAge(365)).toBeGreaterThan(radiusForAge(30))
    expect(radiusForAge(MAX_AGE_DAYS)).toBeCloseTo(R_MAX)
    expect(radiusForAge(5000)).toBeCloseTo(R_MAX)
    expect(radiusForAge(-3)).toBe(R0)
  })
})

describe('hash01', () => {
  it('is stable and inside [0, 1)', () => {
    expect(hash01('a/b.md')).toBe(hash01('a/b.md'))
    expect(hash01('a/b.md')).not.toBe(hash01('a/c.md'))
    for (const p of ['', 'x', 'Privat/Reise Lissabon.md']) {
      expect(hash01(p)).toBeGreaterThanOrEqual(0)
      expect(hash01(p)).toBeLessThan(1)
    }
  })
})

describe('buildSectors', () => {
  it('sums to 360 degrees and starts at north', () => {
    const s = buildSectors([{ key: 'a', label: 'a', weight: 100 }, { key: 'b', label: 'b', weight: 9 }, { key: 'c', label: 'c', weight: 1 }])
    expect(s.reduce((sum, x) => sum + span(x), 0)).toBeCloseTo(360)
    expect(s[0].start).toBe(-90)
    for (let i = 1; i < s.length; i++) expect(s[i].start).toBeCloseTo(s[i - 1].end)
  })
  it('gives every sector at least the floor', () => {
    const s = buildSectors([{ key: 'privat', label: 'Privat', weight: 5371 }, { key: 'cm', label: 'claude-memory', weight: 306 }, { key: 'x', label: 'x', weight: 1 }, { key: 'y', label: 'y', weight: 2 }])
    for (const x of s) expect(span(x)).toBeGreaterThanOrEqual(SECTOR_FLOOR_DEG - 1e-9)
    expect(span(s.find(x => x.key === 'privat')!)).toBeGreaterThan(span(s.find(x => x.key === 'cm')!))
  })
  it('scales with the square root of the weight above the floor', () => {
    const s = buildSectors([{ key: 'a', label: 'a', weight: 400 }, { key: 'b', label: 'b', weight: 100 }])
    expect(span(s[0]) / span(s[1])).toBeCloseTo(2)
  })
  it('splits evenly when the floor cannot hold', () => {
    const s = buildSectors(Array.from({ length: 20 }, (_, i) => ({ key: `k${i}`, label: `k${i}`, weight: i + 1 })))
    for (const x of s) expect(span(x)).toBeCloseTo(18)
  })
  it('orders sectors by key, so a weight change never reorders them', () => {
    expect(buildSectors([{ key: 'b', label: 'b', weight: 1 }, { key: 'a', label: 'a', weight: 50 }]).map(x => x.key)).toEqual(['a', 'b'])
  })
  it('returns nothing for no input', () => {
    expect(buildSectors([])).toEqual([])
  })
})

describe('buildSectors with agents', () => {
  // The real vault: six folders and a catch-all, twelve agents, seven of them in the catch-all.
  const vault = [
    { key: 'a', label: 'a', weight: 1200, agents: 1 },
    { key: 'b', label: 'b', weight: 900, agents: 1 },
    { key: 'c', label: 'c', weight: 700, agents: 1 },
    { key: 'd', label: 'd', weight: 400, agents: 1 },
    { key: 'e', label: 'e', weight: 300, agents: 1 },
    { key: 'quiet', label: 'quiet', weight: 1, agents: 0 },
    { key: 'other', label: 'Other', weight: 1, agents: 7 },
  ]
  const AGENT_LABEL_PX = 110
  const heavy = [
    { key: 'privat', label: 'Privat', weight: 5371 },
    { key: 'cm', label: 'claude-memory', weight: 306 },
    { key: 'x', label: 'x', weight: 1 },
    { key: 'y', label: 'y', weight: 2 },
  ]

  it('gives a sector of one note and seven agents the arc those seven labels need', () => {
    const sectors = buildSectors(vault)
    const crowded = sectors.find(s => s.key === 'other')!
    const quiet = sectors.find(s => s.key === 'quiet')!
    expect(span(crowded)).toBeGreaterThan(3 * span(quiet))
    // Same-tier neighbours sit `tiers` angular steps apart on the base ring; that gap carries the label.
    const gapDeg = sectorTiers(7) * span(crowded) / (7 + 1)
    const gapPx = gapDeg * Math.PI / 180 * agentRingPx(12)
    expect(gapPx).toBeGreaterThanOrEqual(AGENT_LABEL_PX)
  })

  it('still spends the whole circle once a sector claims room for its agents', () => {
    expect(buildSectors(vault).reduce((sum, s) => sum + span(s), 0)).toBeCloseTo(360)
  })

  it('leaves a thousands-of-notes folder with no agents the arc the note weighting gives it', () => {
    const privat = buildSectors(heavy).find(s => s.key === 'privat')!
    const free = 360 - 2 * SECTOR_FLOOR_DEG
    expect(span(privat)).toBeCloseTo(free * Math.sqrt(5371) / (Math.sqrt(5371) + Math.sqrt(306)), 6)
  })

  it('keeps that folder the widest sector when a tiny sibling fills with agents', () => {
    const sectors = buildSectors(heavy.map(s => s.key === 'x' ? { ...s, agents: 7 } : s))
    expect([...sectors].sort((a, b) => span(b) - span(a))[0].key).toBe('privat')
  })

  it('leaves the common case — three agents over six well-populated folders — where the notes put it', () => {
    const folders = [1200, 900, 800, 700, 400, 300].map((weight, i) => ({ key: `f${i}`, label: `f${i}`, weight }))
    const withAgents = buildSectors(folders.map((f, i) => i < 3 ? { ...f, agents: 1 } : f))
    const notesOnly = buildSectors(folders)
    for (let i = 0; i < folders.length; i++) {
      expect(span(withAgents[i])).toBeCloseTo(span(notesOnly[i]), 1)
      expect(span(withAgents[i])).toBeGreaterThan(SECTOR_FLOOR_DEG)
    }
  })
})

describe('notePoint', () => {
  const [sector] = buildSectors([{ key: 'a', label: 'a', weight: 1 }, { key: 'b', label: 'b', weight: 1 }])
  it('is identical across two builds', () => {
    expect(notePoint('a/x.md', sector, 3)).toEqual(notePoint('a/x.md', sector, 3))
  })
  it('stays inside its sector and sits on its freshness radius', () => {
    const [x, y] = notePoint('a/x.md', sector, 30)
    const deg = Math.atan2(y, x) * 180 / Math.PI
    const norm = (d: number) => ((d - sector.start) % 360 + 360) % 360
    expect(norm(deg)).toBeLessThanOrEqual(sector.end - sector.start)
    expect(Math.hypot(x, y)).toBeCloseTo(radiusForAge(30))
  })
})

describe('agents', () => {
  it('spreads agents evenly inside their sector', () => {
    const [s] = buildSectors([{ key: 'a', label: 'a', weight: 1 }, { key: 'b', label: 'b', weight: 1 }])
    expect(agentAngles(1, s)).toEqual([(s.start + s.end) / 2])
    const two = agentAngles(2, s)
    expect(two[0]).toBeGreaterThan(s.start)
    expect(two[1]).toBeLessThan(s.end)
  })
  it('never comes closer to the core than the on-screen floor, at any zoom', () => {
    for (const k of [0.05, 0.3, 1, 2.5, 14]) {
      expect(agentRadius(k, false) * k).toBeGreaterThanOrEqual(AGENT_FLOOR_PX - 1e-9)
      expect(agentRadius(k, true) * k).toBeGreaterThanOrEqual(AGENT_WAITING_FLOOR_PX - 1e-9)
      expect(agentRadius(k, true)).toBeLessThanOrEqual(agentRadius(k, false))
    }
  })
  it('widens the on-screen ring with the number of agents so neighbours get room, never below the floor', () => {
    for (const count of [0, 1, 6, 11, 40])
      expect(agentRingPx(count)).toBeCloseTo(Math.max(AGENT_FLOOR_PX, count * AGENT_SPACING_PX / (2 * Math.PI)))
    expect(agentRingPx(11)).toBeGreaterThan(agentRingPx(4))
    expect(agentRingPx(0)).toBe(AGENT_FLOOR_PX)
  })
  it('caps the ring growth by the stage, but the floor wins over the cap', () => {
    expect(agentRingPx(40, 600)).toBeCloseTo(600 / 2 - AGENT_STAGE_MARGIN_PX)
    expect(agentRingPx(11, 600)).toBeCloseTo(11 * AGENT_SPACING_PX / (2 * Math.PI))
    expect(agentRingPx(40, 200)).toBe(AGENT_FLOOR_PX)
    expect(agentRingPx(40, 0)).toBe(AGENT_FLOOR_PX)
  })
  it('pulls eleven agents in on the 544 px stacked stage so side labels clear the dock and controls columns', () => {
    expect(agentRingPx(11, 544)).toBe(182)
  })
  it('places agents on the given ring, a waiting agent the floor gap further in', () => {
    const k = 0.6
    for (const ring of [AGENT_FLOOR_PX, 196, 300]) {
      expect(agentRadius(k, false, ring) * k).toBeCloseTo(ring)
      expect(agentRadius(k, true, ring) * k).toBeCloseTo(ring - (AGENT_FLOOR_PX - AGENT_WAITING_FLOOR_PX))
    }
    expect(agentRadius(k, true, AGENT_FLOOR_PX) * k).toBeCloseTo(AGENT_WAITING_FLOOR_PX)
    expect(agentRadius(k, false, AGENT_FLOOR_PX)).toBe(agentRadius(k, false))
  })
})

describe('agentSectorRingPx', () => {
  it('leaves the first agent in a sector on the base ring', () => {
    expect(agentSectorRingPx(196, 0, 2)).toBe(196)
    expect(agentSectorRingPx(196, 2, 2)).toBe(196)
  })
  it('staggers the second agent sharing a sector out onto a different radius', () => {
    expect(agentSectorRingPx(196, 1, 2)).toBe(196 + AGENT_SECTOR_STAGGER_PX)
    expect(agentSectorRingPx(196, 1, 2)).not.toBe(agentSectorRingPx(196, 0, 2))
    expect(agentSectorRingPx(196, 3, 2)).toBe(agentSectorRingPx(196, 1, 2))
  })
  it('spreads seven agents sharing a sector over more than two radii', () => {
    const rings = Array.from({ length: 7 }, (_, i) => agentSectorRingPx(196, i, 7))
    expect(new Set(rings).size).toBeGreaterThan(2)
    expect(sectorTiers(7)).toBeGreaterThan(sectorTiers(2))
  })
  it('keeps every one of those seven on the stage and clear of the core', () => {
    const stagePx = 900
    const cap = stagePx / 2 - AGENT_STAGE_MARGIN_PX
    const base = agentRingPx(12, stagePx)
    for (let i = 0; i < 7; i++) {
      const ring = agentSectorRingPx(base, i, 7, stagePx)
      expect(ring).toBeGreaterThanOrEqual(AGENT_FLOOR_PX)
      expect(ring).toBeLessThanOrEqual(cap)
      expect(agentRadius(1, true, ring)).toBeGreaterThanOrEqual(AGENT_WAITING_FLOOR_PX)
    }
  })
  it('never staggers below the on-screen floor, at any base ring or index', () => {
    for (const ring of [0, AGENT_FLOOR_PX, 60]) {
      for (const i of [0, 1, 2, 3])
        expect(agentSectorRingPx(ring, i, 7)).toBeGreaterThanOrEqual(AGENT_FLOOR_PX)
    }
  })
  it('never staggers past the stage cap', () => {
    const stagePx = 600
    const cap = stagePx / 2 - AGENT_STAGE_MARGIN_PX
    expect(agentSectorRingPx(cap, 1, 2, stagePx)).toBe(cap)
    expect(agentSectorRingPx(cap - 1, 1, 7, stagePx)).toBeLessThanOrEqual(cap)
  })
})

describe('sectorLabelRadius', () => {
  it('keeps sector names at their world radius while that clears the agent ring', () => {
    expect(sectorLabelRadius(3, AGENT_FLOOR_PX)).toBe(SECTOR_LABEL_RADIUS)
  })
  it('pushes sector names out to clear the agent ring on screen', () => {
    const k = 0.57
    expect(sectorLabelRadius(k, 196) * k).toBeCloseTo(196 + SECTOR_LABEL_AGENT_CLEARANCE_PX)
    for (const [scale, ring] of [[0.3, 116], [0.57, 196], [1, 116], [2, 300]]) {
      expect(sectorLabelRadius(scale, ring) * scale).toBeGreaterThanOrEqual(ring + SECTOR_LABEL_AGENT_CLEARANCE_PX - 1e-9)
      expect(sectorLabelRadius(scale, ring)).toBeGreaterThanOrEqual(SECTOR_LABEL_RADIUS)
    }
  })
})

describe('visibleRingLabels', () => {
  const px = (label: string, k: number) => radiusForAge(RINGS.find(r => r.label === label)!.days) * k

  it('draws every ring label when the rings are far apart and outside the agent ring', () => {
    expect(visibleRingLabels(3, 116).map(r => r.label)).toEqual(['today', 'week', 'month', 'year'])
  })

  it('hides ring labels inside the agent ring plus its clearance', () => {
    const k = 3
    const labels = visibleRingLabels(k, px('month', k) - 11).map(r => r.label)
    expect(labels).toEqual(['year'])
  })

  it('skips a ring label closer than 16 px to the last drawn one', () => {
    const k = 0.57
    const drawn = visibleRingLabels(k, 0).map(r => px(r.label, k))
    for (let i = 1; i < drawn.length; i++) expect(drawn[i] - drawn[i - 1]).toBeGreaterThanOrEqual(16)
    expect(drawn.length).toBeLessThan(RINGS.length)
    expect(drawn[0]).toBe(px('today', k))
  })
})

describe('sectorKeyFor', () => {
  it('uses the top-level folder', () => {
    expect(sectorKeyFor('Privat/Reise.md', [])).toEqual({ key: 'Privat', label: 'Privat' })
  })
  it('gives a nested folder named like an agent project its own sector', () => {
    expect(sectorKeyFor('claude-memory/private/agent-dashboard/sessions/x.md', ['agent-dashboard']))
      .toEqual({ key: 'claude-memory/private/agent-dashboard', label: 'agent-dashboard' })
  })
  it('matches the project name case-insensitively', () => {
    expect(sectorKeyFor('work/Agent-Context/a.md', ['agent-context']).key).toBe('work/Agent-Context')
  })
  it('puts notes at the root into one root sector', () => {
    expect(sectorKeyFor('Inbox.md', [])).toEqual({ key: '', label: 'Notes' })
  })
})

describe('planSectors', () => {
  it('without notes, gives every agent project its own sector', () => {
    const p = planSectors([], ['kontor', 'shop', 'kontor'])
    expect(p.sectors.map(s => s.key)).toEqual(['kontor', 'shop'])
    expect(p.sectorOfProject.get('shop')).toBe('shop')
  })
  it('with notes, maps agents to a matching sector or to Other', () => {
    const p = planSectors(['Privat/a.md', 'Privat/b.md', 'claude-memory/x/kontor/c.md'], ['kontor', 'shop'])
    expect(p.sectorOfNote.get('Privat/a.md')).toBe('Privat')
    expect(p.sectorOfProject.get('kontor')).toBe('claude-memory/x/kontor')
    expect(p.sectorOfProject.get('shop')).toBe(OTHER_SECTOR_KEY)
    expect(p.sectors.map(s => s.key)).toContain(OTHER_SECTOR_KEY)
  })
  it('adds no Other sector when every agent has one', () => {
    expect(planSectors(['kontor/a.md'], ['kontor']).sectors.map(s => s.key)).toEqual(['kontor'])
  })
  it('counts one agent per repeated project name, so a crowded sector outgrows a quiet one', () => {
    const p = planSectors(['Privat/a.md'], Array.from({ length: 7 }).fill('shop') as string[])
    expect(span(p.sectors.find(s => s.key === OTHER_SECTOR_KEY)!)).toBeGreaterThan(span(p.sectors.find(s => s.key === 'Privat')!))
  })
})
