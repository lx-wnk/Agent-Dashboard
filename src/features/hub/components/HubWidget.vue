<script setup lang="ts">
import type { Agent } from '@/types'
import { computed, inject, ref } from 'vue'
import { NEEDS_YOU } from '@/composables/openTask'
import { useAgents } from '@/features/agents'
import { NeedsYouQueue, useKontorAgent, useKontorSession } from '@/features/mission'
import { agentDisplayStatus } from '@/utils/statusColors'
import { useHubCamera } from '../composables/useHubCamera'
import { agentAngles, agentRadius, planSectors, polar, radiusForAge, RINGS, sectorMid, WEDGE_INNER, WEDGE_OUTER } from '../hubGeometry'
import HubOrbit from './HubOrbit.vue'

const needsYou = inject(NEEDS_YOU)
if (!needsYou)
  throw new Error('HubWidget requires NEEDS_YOU from App.vue')

const stage = ref<HTMLElement | null>(null)
const { cam, level, dragging, flyTo } = useHubCamera(stage)
const { agents } = useAgents({ autoStart: false })
const { ask } = useKontorSession()
const kontorAgent = useKontorAgent()

const SECTOR_FLY_RADIUS = 260
const SECTOR_FLY_REL = 2.6
const AGENT_FLY_REL = 3

const live = computed(() => agents.value.filter(a => a.status !== 'finished').sort((a, b) => a.pid - b.pid))
const plan = computed(() => planSectors([], live.value.map(a => a.projectName)))

const placed = computed(() => {
  const k = cam.value.k
  const { sectors, sectorOfProject } = plan.value
  return sectors.flatMap((sector) => {
    const members = live.value.filter(a => sectorOfProject.get(a.projectName) === sector.key)
    const angles = agentAngles(members.length, sector)
    return members.map((agent, i) => {
      const needsOperator = !!(agent.pendingQuestion || agent.pendingConfirm || agent.pendingPermissions?.length)
      const [x, y] = polar(agentRadius(k, needsOperator), angles[i])
      return { agent, x, y, state: agentDisplayStatus(agent), needsOperator }
    })
  })
})

const running = computed(() => placed.value.filter(p => p.state === 'working' || p.state === 'active').length)
const waiting = computed(() => placed.value.filter(p => p.needsOperator).length)
const kontorState = computed(() => kontorAgent.value ? agentDisplayStatus(kontorAgent.value) : 'off')

function arcTo(radius: number, deg: number, sweep: 0 | 1): string {
  const [x, y] = polar(radius, deg)
  return `A${radius},${radius} 0 0 ${sweep} ${x},${y}`
}

// Two half-arcs per edge: a lone 360° sector's single arc would end on its own start and draw nothing.
function wedgePath(start: number, end: number): string {
  const mid = (start + end) / 2
  const [ox, oy] = polar(WEDGE_OUTER, start)
  const [ix, iy] = polar(WEDGE_INNER, end)
  return `M${ox},${oy}${arcTo(WEDGE_OUTER, mid, 1)}${arcTo(WEDGE_OUTER, end, 1)}L${ix},${iy}${arcTo(WEDGE_INNER, mid, 0)}${arcTo(WEDGE_INNER, start, 0)}Z`
}

function flyToAgent(agent: Agent) {
  const hit = placed.value.find(p => p.agent.pid === agent.pid)
  if (hit)
    flyTo(hit.x, hit.y, AGENT_FLY_REL)
}
</script>

<template>
  <section data-testid="hub" aria-label="Zentrale" class="relative h-full min-h-0 overflow-hidden rounded-xl border border-line bg-card">
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
    >
      <svg class="pointer-events-none absolute inset-0 size-full" aria-hidden="true">
        <g :transform="`translate(${cam.tx},${cam.ty}) scale(${cam.k})`">
          <path
            v-for="(sector, i) in plan.sectors"
            :key="sector.key"
            :d="wedgePath(sector.start, sector.end)"
            fill-opacity="0.035"
            :style="{ fill: `var(--sector-${i % 8})` }"
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
      <HubOrbit
        :cam="cam"
        :sectors="plan.sectors"
        :agents="placed"
        :level="level"
        :running="running"
        :waiting="waiting"
        :needs-you="needsYou.length"
        :kontor-state="kontorState"
        @core="ask()"
        @agent="flyToAgent"
        @sector="sector => flyTo(...polar(SECTOR_FLY_RADIUS, sectorMid(sector)), SECTOR_FLY_REL)"
      />
    </div>
    <NeedsYouQueue
      variant="docked"
      data-hub-layer
      class="absolute left-1/2 top-2.5 z-10 max-h-[45%] w-[min(560px,calc(100%-120px))] -translate-x-1/2 overflow-y-auto rounded-[10px] border bg-card/95 px-2.5 py-2 shadow-lg"
      :class="needsYou.length > 0 ? 'border-warning-line' : 'border-line'"
    />
  </section>
</template>
