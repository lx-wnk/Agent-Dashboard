<script setup lang="ts">
import type { HubNote } from '../composables/useObsidianGraph'
import type { Camera, HubLevel } from '../hubCamera'
import type { LabelCandidate } from '../hubCanvas'
import { useMutationObserver } from '@vueuse/core'
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { toScreen } from '../hubCamera'
import { cullLabels, isToday, notePriority } from '../hubCanvas'

const props = defineProps<{
  cam: Camera
  size: { width: number, height: number }
  level: HubLevel
  points: ReadonlyArray<[number, number]>
  colours: ReadonlyArray<number>
  notes: ReadonlyArray<HubNote>
  links: ReadonlyArray<[number, number]>
  hubNotes: ReadonlySet<number>
  selected: number | null
}>()

const NOTE_RADIUS_PX: Record<HubLevel, number> = { 0: 2.1, 1: 3.4, 2: 4.6 }
const LINK_ALPHA: Record<HubLevel, number> = { 0: 0.28, 1: 0.55, 2: 0.8 }
const LINK_WIDTH_PX = 0.7
const NOTE_ALPHA = 0.75
const HUB_NOTE_SCALE = 1.9
const HALO_SCALE = 2.6
const HALO_ALPHA = 0.8
const HALO_WIDTH_PX = 1.2
const SELECTION_SCALE = 3.2
const SELECTION_WIDTH_PX = 1.5
const SELECTION_COLOUR = '#fff'
const VIEWPORT_MARGIN_PX = 20
const LABEL_FONT = '11px system-ui, sans-serif'
const HUB_LABEL_FONT = `600 ${LABEL_FONT}`
const LABEL_STROKE_PX = 3
const LABEL_OFFSET_PX = 8.8
const SECTOR_TOKENS = Array.from({ length: 8 }, (_, i) => `--sector-${i}`)
const DAY_MS = 86_400_000
const FULL_CIRCLE = Math.PI * 2

interface Scene {
  ctx: CanvasRenderingContext2D
  screen: Array<[number, number]>
  onStage: boolean[]
  visible: number[]
  radius: number
  now: number
  token: (name: string) => string
}

const canvas = ref<HTMLCanvasElement | null>(null)
let frame: number | null = null

function circle(ctx: CanvasRenderingContext2D, [x, y]: [number, number], r: number) {
  ctx.beginPath()
  ctx.arc(x, y, r, 0, FULL_CIRCLE)
}

function drawLinks({ ctx, screen, onStage, token }: Scene) {
  ctx.globalAlpha = LINK_ALPHA[props.level]
  ctx.strokeStyle = token('--line-strong')
  ctx.lineWidth = LINK_WIDTH_PX
  ctx.beginPath()
  for (const [from, to] of props.links) {
    if (!onStage[from] && !onStage[to])
      continue
    ctx.moveTo(...screen[from])
    ctx.lineTo(...screen[to])
  }
  ctx.stroke()
}

function drawNotes({ ctx, screen, visible, radius, now, token }: Scene) {
  const palette = SECTOR_TOKENS.map(token)
  ctx.strokeStyle = token('--halo')
  ctx.lineWidth = HALO_WIDTH_PX
  for (const i of visible) {
    if (props.level > 0 && isToday(props.notes[i].mtimeMs, now)) {
      ctx.globalAlpha = HALO_ALPHA
      circle(ctx, screen[i], radius * HALO_SCALE)
      ctx.stroke()
    }
    const hub = props.hubNotes.has(i)
    ctx.globalAlpha = hub ? 1 : NOTE_ALPHA
    ctx.fillStyle = palette[props.colours[i]]
    circle(ctx, screen[i], hub ? radius * HUB_NOTE_SCALE : radius)
    ctx.fill()
  }
}

function labelCandidate(i: number, [sx, sy]: [number, number], now: number): LabelCandidate {
  const note = props.notes[i]
  return {
    index: i,
    sx,
    sy,
    text: note.title,
    priority: notePriority({
      hub: props.hubNotes.has(i),
      touched: isToday(note.mtimeMs, now),
      fresh: now - note.mtimeMs < DAY_MS,
      linkCount: note.links.length + note.backlinks.length,
    }),
  }
}

// Topics level labels the hub notes only; the notes level labels whatever cullLabels keeps.
function drawLabels({ ctx, screen, visible, now, token }: Scene) {
  if (props.level === 0)
    return
  const candidates = visible
    .filter(i => props.level === 2 || props.hubNotes.has(i))
    .map(i => labelCandidate(i, screen[i], now))
  const kept = props.level === 2 ? cullLabels(candidates) : null
  ctx.globalAlpha = 1
  ctx.textBaseline = 'middle'
  ctx.lineJoin = 'round'
  ctx.lineWidth = LABEL_STROKE_PX
  ctx.strokeStyle = token('--app')
  ctx.fillStyle = token('--fg-soft')
  for (const c of candidates) {
    if (kept && !kept.has(c.index))
      continue
    ctx.font = props.hubNotes.has(c.index) ? HUB_LABEL_FONT : LABEL_FONT
    ctx.strokeText(c.text, c.sx + LABEL_OFFSET_PX, c.sy)
    ctx.fillText(c.text, c.sx + LABEL_OFFSET_PX, c.sy)
  }
}

function drawSelection({ ctx, screen, onStage, radius }: Scene) {
  const i = props.selected
  if (i === null || !onStage[i])
    return
  ctx.globalAlpha = 1
  ctx.strokeStyle = SELECTION_COLOUR
  ctx.lineWidth = SELECTION_WIDTH_PX
  circle(ctx, screen[i], radius * SELECTION_SCALE)
  ctx.stroke()
}

function draw() {
  frame = null
  const el = canvas.value
  if (!el)
    return
  const ctx = el.getContext('2d')
  if (!ctx)
    throw new Error('HubBrainCanvas needs a 2D canvas context')
  const { width, height } = props.size
  const dpr = window.devicePixelRatio || 1
  el.width = Math.round(width * dpr)
  el.height = Math.round(height * dpr)
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
  ctx.clearRect(0, 0, width, height)
  const style = getComputedStyle(el)
  const screen = props.points.map(([x, y]) => toScreen(props.cam, x, y))
  const onStage = screen.map(([sx, sy]) =>
    sx >= -VIEWPORT_MARGIN_PX && sx <= width + VIEWPORT_MARGIN_PX && sy >= -VIEWPORT_MARGIN_PX && sy <= height + VIEWPORT_MARGIN_PX)
  const scene: Scene = {
    ctx,
    screen,
    onStage,
    visible: onStage.flatMap((on, i) => on ? [i] : []),
    radius: NOTE_RADIUS_PX[props.level],
    now: Date.now(),
    token: name => style.getPropertyValue(name).trim(),
  }
  drawLinks(scene)
  drawNotes(scene)
  drawLabels(scene)
  drawSelection(scene)
}

function schedule() {
  frame ??= requestAnimationFrame(draw)
}

watch(() => Object.values(props), schedule)
// Colours are read from CSS tokens at draw time, so a theme switch (class on <html>) needs a redraw.
useMutationObserver(document.documentElement, schedule, { attributeFilter: ['class'] })
onMounted(schedule)
onUnmounted(() => {
  if (frame !== null)
    cancelAnimationFrame(frame)
})
</script>

<template>
  <canvas
    ref="canvas"
    aria-hidden="true"
    class="pointer-events-none absolute inset-0"
    :style="{ width: `${size.width}px`, height: `${size.height}px` }"
  />
</template>
