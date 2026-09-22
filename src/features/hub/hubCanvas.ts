import type { HubNote } from './composables/useObsidianGraph'
import type { Camera } from './hubCamera'
import { toScreen } from './hubCamera'

export interface LabelCandidate { index: number, sx: number, sy: number, text: string, priority: number }

export interface LabelBox { x: number, y: number, w: number, h: number }

// A note's label sits to the right of its point (HubBrainCanvas.vue's drawLabels offset).
function noteLabelBox(c: LabelCandidate): LabelBox {
  return { x: c.sx + 6, y: c.sy - 8, w: c.text.length * 6.3 + 10, h: 16 }
}

export function boxesOverlap(a: LabelBox, b: LabelBox): boolean {
  return a.x < b.x + b.w && a.x + a.w > b.x && a.y < b.y + b.h && a.y + a.h > b.y
}

export function notePriority(n: { hub: boolean, touched: boolean, fresh: boolean, linkCount: number }): number {
  return (n.hub ? 100 : 0) + (n.touched ? 80 : 0) + (n.fresh ? 40 : 0) + n.linkCount
}

// A permanently-occupied area a label must not land on, e.g. another agent's dot or a sector name.
// ownerIndex exempts one candidate's own obstacle (its own dot) from blocking its own label.
export interface LabelObstacle { box: LabelBox, ownerIndex?: number }

// Greedy placement: highest priority first, skip a candidate whose box overlaps one already placed
// or a pre-seeded obstacle (own obstacle, if any, excluded).
export function cullLabels(candidates: LabelCandidate[], boxOf: (c: LabelCandidate) => LabelBox = noteLabelBox, obstacles: readonly LabelObstacle[] = []): Set<number> {
  const kept = new Set<number>()
  const placed: LabelBox[] = []
  for (const c of [...candidates].sort((a, b) => b.priority - a.priority)) {
    const box = boxOf(c)
    const blocked = placed.some(p => boxesOverlap(box, p))
      || obstacles.some(o => o.ownerIndex !== c.index && boxesOverlap(box, o.box))
    if (blocked)
      continue
    placed.push(box)
    kept.add(c.index)
  }
  return kept
}

// An agent's label sits centred under its dot (HubOrbit.vue's button layout) and renders the name
// plus its status word side by side, separated by a gap — c.text is expected to carry both, joined
// by a space, so the estimate isn't 25-45px narrower than the real `flex gap-1 px-1.5` box.
export function agentLabelBox(c: LabelCandidate): LabelBox {
  const w = c.text.length * 6.3 + 24
  return { x: c.sx - w / 2, y: c.sy + 20, w, h: 16 }
}

const AGENT_DOT_PX = 18 // HubOrbit.vue's `size-[18px]` dot, centred on the agent's screen point.

// An agent's own dot: an obstacle a neighbour's label must not cover, or that agent's dot becomes
// invisible (its label sits on a `bg-card/85` background) and unclickable underneath it.
export function agentDotBox(sx: number, sy: number): LabelBox {
  const r = AGENT_DOT_PX / 2
  return { x: sx - r, y: sy - r, w: AGENT_DOT_PX, h: AGENT_DOT_PX }
}

// A sector name centres on its point (HubOrbit.vue's `-translate-1/2`), reads UPPERCASE with wide
// tracking and carries a note-count badge — all three widen it past a plain label's estimate. It is
// the map's legend and is never culled itself, only ever an obstacle for an agent label.
export function sectorLabelBox(sx: number, sy: number, label: string, weight: number): LabelBox {
  const w = (label.length + String(weight).length + 1) * 7.6 + 12
  return { x: sx - w / 2, y: sy - 9, w, h: 18 }
}

// needs-the-operator outranks working, which outranks everything else (Ruling R23).
export function agentPriority(needsOperator: boolean, working: boolean): number {
  return (needsOperator ? 2 : 0) + (working ? 1 : 0)
}

export function hitNote(points: ReadonlyArray<[number, number]>, cam: Camera, sx: number, sy: number, maxPx = 8): number {
  let best = -1
  let bestDist = maxPx
  points.forEach(([x, y], i) => {
    const [px, py] = toScreen(cam, x, y)
    const d = Math.hypot(px - sx, py - sy)
    if (d <= bestDist) {
      bestDist = d
      best = i
    }
  })
  return best
}

export function isToday(mtimeMs: number, nowMs: number): boolean {
  const a = new Date(mtimeMs)
  const b = new Date(nowMs)
  return a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate()
}

export function hubNoteSet(notes: ReadonlyArray<HubNote>, sectorOf: (n: HubNote) => string): Set<number> {
  const bySector = new Map<string, HubNote[]>()
  for (const n of notes) {
    if (n.backlinks.length < 2)
      continue
    const key = sectorOf(n)
    const list = bySector.get(key)
    if (list)
      list.push(n)
    else
      bySector.set(key, [n])
  }
  const result = new Set<number>()
  for (const list of bySector.values()) {
    list.sort((a, b) => b.backlinks.length - a.backlinks.length)
    for (const n of list.slice(0, 3))
      result.add(n.index)
  }
  return result
}
