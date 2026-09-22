<script setup lang="ts">
import type { HubLevel } from '../hubCamera'
import type { Launcher } from '../hubLaunchers'
import type { WidgetId } from '@/features/workspace'
import type { Agent } from '@/types'
import { useEventListener } from '@vueuse/core'
import { computed, inject, onMounted, ref, watch } from 'vue'
import { NEEDS_YOU, OPEN_SETTINGS } from '@/composables/openTask'
import { useSidebar } from '@/composables/useSidebar'
import { useViewState } from '@/composables/useViewState'
import { useAgents } from '@/features/agents'
import { NeedsYouQueue, useKontorAgent, useKontorSession } from '@/features/mission'
import { pageView, pageWithWidget, useWorkspace, ZENTRALE_PAGE_ID } from '@/features/workspace'
import { attentionFor } from '@/utils/attention'
import { NAV_ITEMS } from '@/utils/navConfig'
import { agentDisplayStatus } from '@/utils/statusColors'
import { useHubCamera } from '../composables/useHubCamera'
import { hubFocusRequest } from '../composables/useHubFocus'
import { useObsidianGraph } from '../composables/useObsidianGraph'
import { launchersDocked, LEVEL_TARGETS } from '../hubCamera'
import { hitNote, hubNoteSet } from '../hubCanvas'
import { agentAngles, agentRadius, agentRingPx, DAY_MS, notePoint, planSectors, polar, radiusForAge, RINGS, SECTOR_PALETTE_SIZE, sectorMid, wedgePath } from '../hubGeometry'
import { GRAPH_NOTICES } from '../hubGraphNotices'
import { launchersFor } from '../hubLaunchers'
import HubAgentCard from './HubAgentCard.vue'
import HubBrainCanvas from './HubBrainCanvas.vue'
import HubControls from './HubControls.vue'
import HubLaunchers from './HubLaunchers.vue'
import HubList from './HubList.vue'
import HubMinimap from './HubMinimap.vue'
import HubNoteCard from './HubNoteCard.vue'
import HubOrbit from './HubOrbit.vue'

const needsYou = inject(NEEDS_YOU)
if (!needsYou)
  throw new Error('HubWidget requires NEEDS_YOU from App.vue')
const openSettings = inject(OPEN_SETTINGS)
if (!openSettings)
  throw new Error('HubWidget requires OPEN_SETTINGS from App.vue')

const hub = ref<HTMLElement | null>(null)
const stage = ref<HTMLElement | null>(null)
const { cam, size, rel, level, dragging, zoomBy, panBy, flyTo, fit, centreWorld } = useHubCamera(stage, { onTap: tapNote })
const { status: graphStatus, message: graphMessage, notes, refresh: refreshGraph, recentNotes, noteByPath } = useObsidianGraph()
const { agents } = useAgents({ autoStart: false })
const { ask, overlayOpen } = useKontorSession()
const kontorAgent = useKontorAgent()
const { activeView } = useViewState()
const { layout, wide } = useWorkspace()
const { requestNewPage } = useSidebar()
const listOpen = ref(false)
const openCard = ref<{ kind: 'agent', pid: number } | { kind: 'note', path: string } | null>(null)

const SECTOR_FLY_RADIUS = 260
const SECTOR_FLY_REL = 2.6
const AGENT_FLY_REL = 3
const NOTE_FLY_REL = 2.6
const CHIP_FLY_MIN_REL = 3
const LIST_NOTE_FLY_REL = 5
const LIST_NOTE_COUNT = 14
const MINIMAP_FLY_MIN_REL = 2
const ZOOM_STEP = 1.4
const PAN_STEP_PX = 60
const SLOT_KEY = /^\d$/
const HUB_WIDGET: WidgetId = 'hub'
const KONTOR_WIDGET: WidgetId = 'kontor'

// Only the blocking kinds: needsAttention() is also true for every non-working agent ('yourTurn').
function blocksOnOperator(agent: Agent): boolean {
  const kind = attentionFor(agent, null)?.kind
  return kind === 'question' || kind === 'permission'
}

const live = computed(() => agents.value.filter(a => a.status !== 'finished').sort((a, b) => a.pid - b.pid))
// A refetch passes through 'loading'; keeping the last notes stops the brain from blanking and the agents from jumping.
const vaultNotes = computed(() => graphStatus.value === 'ready' || graphStatus.value === 'loading' ? notes.value : [])
const plan = computed(() => planSectors(vaultNotes.value.map(n => n.path), live.value.map(a => a.projectName)))
const graphNotice = computed(() => GRAPH_NOTICES[graphStatus.value])

const brain = computed(() => {
  const { sectors, sectorOfNote } = plan.value
  const slotOf = new Map(sectors.map((sector, i) => [sector.key, { sector, colour: i % SECTOR_PALETTE_SIZE }]))
  const now = Date.now()
  const slots = vaultNotes.value.map(n => slotOf.get(sectorOfNote.get(n.path)!)!)
  return {
    points: vaultNotes.value.map((n, i) => notePoint(n.path, slots[i].sector, (now - n.mtimeMs) / DAY_MS)),
    colours: slots.map(s => s.colour),
    links: vaultNotes.value.flatMap(n => n.links.map((to): [number, number] => [n.index, to])),
    hubNotes: hubNoteSet(vaultNotes.value, n => sectorOfNote.get(n.path)!),
  }
})

const cardNote = computed(() => {
  const card = openCard.value
  return card?.kind === 'note' ? vaultNotes.value.find(n => n.path === card.path) ?? null : null
})

function sectorLabel(path: string): string {
  const { sectors, sectorOfNote } = plan.value
  return sectors.find(s => s.key === sectorOfNote.get(path))!.label
}

const listNotes = computed(() => vaultNotes.value.length ? recentNotes(LIST_NOTE_COUNT).map(n => ({ ...n, sector: sectorLabel(n.path) })) : [])

function openNote(index: number) {
  openCard.value = { kind: 'note', path: vaultNotes.value[index].path }
}

function flyToNote(index: number, relTarget: number) {
  openNote(index)
  flyTo(...brain.value.points[index], relTarget)
}

function tapNote(sx: number, sy: number) {
  const hit = hitNote(brain.value.points, cam.value, sx, sy)
  if (hit < 0)
    return
  if (level.value === 0)
    flyTo(...brain.value.points[hit], NOTE_FLY_REL)
  else
    openNote(hit)
}

onMounted(() => refreshGraph())
useEventListener(window, 'focus', () => refreshGraph())
const ringPx = computed(() => agentRingPx(live.value.length, Math.min(size.value.width, size.value.height)))
const ringOnScreenPx = computed(() => agentRadius(cam.value.k, false, ringPx.value) * cam.value.k)
const docked = computed(() => launchersDocked(rel.value, cam.value.k, ringOnScreenPx.value))

const placed = computed(() => {
  const k = cam.value.k
  const { sectors, sectorOfProject } = plan.value
  return sectors.flatMap((sector) => {
    const members = live.value.filter(a => sectorOfProject.get(a.projectName) === sector.key)
    const angles = agentAngles(members.length, sector)
    return members.map((agent, i) => {
      const needsOperator = blocksOnOperator(agent)
      const [x, y] = polar(agentRadius(k, needsOperator, ringPx.value), angles[i])
      return { agent, x, y, state: agentDisplayStatus(agent), needsOperator }
    })
  })
})

const running = computed(() => placed.value.filter(p => p.state === 'working' || p.state === 'active').length)
const waiting = computed(() => placed.value.filter(p => p.needsOperator).length)
const kontorState = computed(() => kontorAgent.value ? agentDisplayStatus(kontorAgent.value) : 'off')
const kontorPage = computed(() => pageWithWidget(layout.value, KONTOR_WIDGET))
const coreTitle = computed(() => kontorPage.value ? `Open Kontor (${kontorState.value})` : 'Add the Kontor tile to a page to open it here')

const cardAgent = computed(() => {
  const card = openCard.value
  return card?.kind === 'agent' ? live.value.find(a => a.pid === card.pid) ?? null : null
})

// A finished agent or a note gone after a refetch closes its card for good.
watch(() => openCard.value !== null && !cardAgent.value && !cardNote.value, (gone) => {
  if (gone)
    openCard.value = null
})

const otherPages = computed(() => layout.value.pages.filter(p => p.id !== ZENTRALE_PAGE_ID))
const launchers = computed(() => launchersFor(NAV_ITEMS, otherPages.value, activeView.value))
const listLaunchers = computed(() => launchersFor(NAV_ITEMS, otherPages.value, activeView.value, Infinity))

function openKontor(prefill?: string) {
  if (!kontorPage.value)
    return
  const current = layout.value.pages.find(p => pageView(p.id) === activeView.value)
  if (!current?.tiles.some(t => t.widget === KONTOR_WIDGET))
    activeView.value = pageView(kontorPage.value.id)
  ask(prefill)
}

function launch(launcher: Launcher) {
  if (launcher.kind === 'new-page')
    requestNewPage()
  else if (launcher.kind === 'more')
    listOpen.value = true
  else if (launcher.view)
    activeView.value = launcher.view
  else
    throw new Error(`Launcher ${launcher.id} has no view`)
}

function toggleWide() {
  wide.value = wide.value === HUB_WIDGET ? null : HUB_WIDGET
}

function toggleList() {
  listOpen.value = !listOpen.value
}

// Post-flush: a closed layer that held focus has unmounted by now and dropped focus to <body>.
watch([listOpen, openCard], () => {
  if (!hub.value?.contains(document.activeElement))
    stage.value?.focus()
}, { flush: 'post' })

function escape() {
  if (openCard.value)
    openCard.value = null
  else if (listOpen.value)
    toggleList()
  else
    fit()
}

function flyToLevel(target: HubLevel) {
  const [x, y] = target === 0 ? [0, 0] : centreWorld()
  flyTo(x, y, LEVEL_TARGETS[target])
}

const zoomIn = () => zoomBy(ZOOM_STEP)
const zoomOut = () => zoomBy(1 / ZOOM_STEP)

const KEY_ACTIONS: Record<string, () => void> = {
  'ArrowLeft': () => panBy(PAN_STEP_PX, 0),
  'ArrowRight': () => panBy(-PAN_STEP_PX, 0),
  'ArrowUp': () => panBy(0, PAN_STEP_PX),
  'ArrowDown': () => panBy(0, -PAN_STEP_PX),
  '+': zoomIn,
  '=': zoomIn,
  '-': zoomOut,
  '0': fit,
  'f': toggleWide,
  'F': toggleWide,
  'l': toggleList,
  'L': toggleList,
}

function isTyping(target: HTMLElement): boolean {
  return target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT' || target.isContentEditable
}

// Shift stays allowed: '+' needs it on most layouts.
function ignored(e: KeyboardEvent): boolean {
  return e.ctrlKey || e.metaKey || e.altKey || isTyping(e.target as HTMLElement)
}

function onKey(e: KeyboardEvent) {
  if (ignored(e))
    return
  const slot = SLOT_KEY.test(e.key) ? launchers.value[Number(e.key) - 1] : undefined
  const action = KEY_ACTIONS[e.key] ?? (slot && (() => launch(slot)))
  if (!action)
    return
  e.preventDefault()
  action()
}

// On the hub root so Escape from the list and card reaches it too; the Kontor overlay above
// the hub collapses on this same Escape from its window listener.
function onEscape(e: KeyboardEvent) {
  if (ignored(e) || overlayOpen.value)
    return
  e.preventDefault()
  escape()
}

function flyToAgent(agent: Agent) {
  openCard.value = { kind: 'agent', pid: agent.pid }
  const hit = placed.value.find(p => p.agent.pid === agent.pid)
  if (hit)
    flyTo(hit.x, hit.y, AGENT_FLY_REL)
}

function pickFromList(agent: Agent) {
  listOpen.value = false
  flyToAgent(agent)
}

function pickNoteFromList(path: string) {
  listOpen.value = false
  flyToNote(noteByPath(path)!.index, LIST_NOTE_FLY_REL)
}

function launchFromList(launcher: Launcher) {
  listOpen.value = false
  launch(launcher)
}

// An unresolved target (deleted note, finished agent) is dropped silently: clearing the request either way stops a stale one from firing on the next mount.
watch(hubFocusRequest, (target) => {
  if (!target)
    return
  if (target.kind === 'note') {
    const note = noteByPath(target.path)
    if (note)
      flyToNote(note.index, LIST_NOTE_FLY_REL)
  }
  else {
    const agent = live.value.find(a => a.pid === target.pid)
    if (agent)
      flyToAgent(agent)
  }
  hubFocusRequest.value = null
}, { immediate: true, flush: 'post' })
</script>

<template>
  <section
    ref="hub"
    data-testid="hub"
    aria-label="Zentrale"
    class="relative h-full min-h-[34rem] overflow-hidden rounded-xl border border-line bg-card md:min-h-0"
    @keydown.escape="onEscape"
  >
    <div
      ref="stage"
      data-testid="hub-stage"
      tabindex="0"
      role="application"
      aria-roledescription="zoomable map"
      aria-label="Zentrale. Arrow keys pan, plus and minus zoom, 0 shows all, F widens, L lists."
      :data-level="level"
      class="absolute inset-0 touch-none overflow-hidden outline-none focus-visible:ring-2 focus-visible:ring-accent"
      :class="dragging ? 'cursor-grabbing' : 'cursor-grab'"
      style="background: var(--hub-bg)"
      @keydown="onKey"
    >
      <svg class="pointer-events-none absolute inset-0 size-full" aria-hidden="true">
        <g :transform="`translate(${cam.tx},${cam.ty}) scale(${cam.k})`">
          <path
            v-for="(sector, i) in plan.sectors"
            :key="sector.key"
            :d="wedgePath(sector.start, sector.end)"
            fill-opacity="0.035"
            :style="{ fill: `var(--sector-${i % SECTOR_PALETTE_SIZE})` }"
          />
          <circle
            v-for="ring in RINGS"
            :key="ring.label"
            :r="radiusForAge(ring.days)"
            fill="none"
            stroke-dasharray="3 5"
            vector-effect="non-scaling-stroke"
            style="stroke: var(--line)"
          />
        </g>
      </svg>
      <HubBrainCanvas
        :cam="cam"
        :size="size"
        :level="level"
        :points="brain.points"
        :colours="brain.colours"
        :notes="vaultNotes"
        :links="brain.links"
        :hub-notes="brain.hubNotes"
        :selected="cardNote?.index ?? null"
      />
      <HubOrbit
        :cam="cam"
        :sectors="plan.sectors"
        :agents="placed"
        :level="level"
        :running="running"
        :waiting="waiting"
        :needs-you="needsYou.length"
        :core-title="coreTitle"
        :agent-ring-px="ringOnScreenPx"
        :show-sector-names="vaultNotes.length > 0"
        @core="openKontor"
        @agent="flyToAgent"
        @sector="sector => flyTo(...polar(SECTOR_FLY_RADIUS, sectorMid(sector)), SECTOR_FLY_REL)"
      />
      <HubLaunchers :launchers="launchers" :cam="cam" :docked="docked" @launch="launch" />
      <HubControls
        :level="level"
        :wide="wide === HUB_WIDGET"
        @zoom-in="zoomIn"
        @zoom-out="zoomOut"
        @fit="fit"
        @wide="toggleWide"
        @list="toggleList"
        @level="flyToLevel"
      />
      <HubMinimap
        :cam="cam"
        :size="size"
        :sectors="plan.sectors"
        :agents="placed"
        @fly="(x, y) => flyTo(x, y, Math.max(rel, MINIMAP_FLY_MIN_REL))"
      />
    </div>
    <div class="pointer-events-none absolute left-1/2 top-2.5 z-10 flex max-h-[45%] w-[min(560px,calc(100%-120px))] -translate-x-1/2 flex-col items-start gap-1.5">
      <NeedsYouQueue
        variant="docked"
        data-hub-layer
        class="pointer-events-auto min-h-0 w-full overflow-y-auto rounded-[10px] border bg-card/95 px-2.5 py-2 shadow-lg"
        :class="needsYou.length > 0 ? 'border-warning-line' : 'border-line'"
      />
      <p
        v-if="graphNotice"
        data-hub-layer
        data-testid="hub-graph-notice"
        :title="graphStatus === 'denied' ? graphMessage : undefined"
        class="pointer-events-auto flex shrink-0 items-center gap-2 rounded-lg border border-line bg-card/95 px-2.5 py-1.5 text-[12px] text-fg-mute shadow"
      >
        {{ graphNotice }}
        <button
          v-if="graphStatus === 'unconfigured'"
          type="button"
          class="cursor-pointer text-accent underline-offset-2 hover:underline"
          @click="openSettings()"
        >
          Open settings
        </button>
      </p>
    </div>
    <HubList
      v-if="listOpen"
      :agents="placed"
      :notes="listNotes"
      :graph-status="graphStatus"
      :graph-message="graphMessage"
      :launchers="listLaunchers"
      @agent="pickFromList"
      @note="pickNoteFromList"
      @launch="launchFromList"
      @close="listOpen = false"
    />
    <HubAgentCard v-if="cardAgent" :agent="cardAgent" @close="openCard = null" />
    <HubNoteCard
      v-if="cardNote"
      :key="cardNote.path"
      :note="cardNote"
      :notes="vaultNotes"
      :sector-label="sectorLabel(cardNote.path)"
      :kontor-reachable="!!kontorPage"
      @fly="index => flyToNote(index, Math.max(rel, CHIP_FLY_MIN_REL))"
      @ask="openKontor"
      @close="openCard = null"
    />
  </section>
</template>
