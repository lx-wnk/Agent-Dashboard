<script setup lang="ts">
import type { Camera, HubLevel } from '../hubCamera'
import type { LabelSize } from '../hubCanvas'
import type { Sector } from '../hubGeometry'
import type { Agent } from '@/types'
import type { AgentDisplayStatus, ChipTone } from '@/utils/statusColors'
import { computed, onMounted, ref, watch } from 'vue'
import { friendlyProjectName } from '@/utils/friendlyProjectName'
import { agentStatusTone, statusLabel } from '@/utils/statusColors'
import { toScreen } from '../hubCamera'
import { agentLabelKey, sectorLabelKey } from '../hubCanvas'
import { polar, radiusForAge, SECTOR_PALETTE_SIZE, sectorLabelRadius, sectorMid, visibleRingLabels } from '../hubGeometry'

const props = defineProps<{
  cam: Camera
  sectors: Sector[]
  agents: ReadonlyArray<{ agent: Agent, x: number, y: number, state: AgentDisplayStatus, needsOperator: boolean }>
  level: HubLevel
  running: number
  waiting: number
  needsYou: number
  coreTitle: string
  coreDisabled: boolean
  agentRingPx: number
  showSectorNames: boolean
  // Omitted shows every label (used by callers that don't cull, e.g. tests); the dot is never gated.
  labelledAgents?: ReadonlySet<number>
}>()

const emit = defineEmits<{ core: [], agent: [agent: Agent], sector: [sector: Sector], measure: [sizes: ReadonlyMap<string, LabelSize>] }>()

const RING_LABEL_DEG = -128

const root = ref<HTMLElement | null>(null)
const ringLabels = computed(() => visibleRingLabels(props.cam.k, props.agentRingPx))
const sectorNameRadius = computed(() => sectorLabelRadius(props.cam.k, props.agentRingPx))
const sectorNamesShown = computed(() => props.level < 2 && props.showSectorNames)

// Label sizes come from the DOM, never from a character count: an estimate has twice placed labels
// over what they must clear. A size depends on the text and the font only — the camera scales
// neither — so each distinct text is measured once and reused across every pan and zoom.
const sizes = new Map<string, LabelSize>()

// Joined, so a camera tick — which hands us new objects for the same labels — is not a change.
const labelKeys = computed(() => [
  ...props.agents.map(a => agentLabelKey(a.agent.projectName, a.state)),
  ...(sectorNamesShown.value ? props.sectors.map(s => sectorLabelKey(s.label, s.weight)) : []),
].join('\n'))

function measure() {
  let grew = false
  for (const el of root.value?.querySelectorAll<HTMLElement>('[data-label-key]') ?? []) {
    const key = el.dataset.labelKey!
    if (sizes.has(key))
      continue
    const { width, height } = el.getBoundingClientRect()
    sizes.set(key, { w: width, h: height })
    grew = true
  }
  if (grew)
    emit('measure', new Map(sizes))
}

onMounted(measure)
// Post-flush and not immediate: the labels of a text that has just appeared must be in the DOM first.
watch(labelKeys, measure, { flush: 'post' })

// Tailwind needs the full literal class name, so the tone still maps to a fixed string per component.
const TONE_DOT_CLASS: Partial<Record<ChipTone, string>> = {
  success: 'bg-success-dot',
  info: 'bg-info-dot',
  warning: 'bg-warning-dot',
  neutral: 'bg-fg-faint',
}

function dotClass(state: AgentDisplayStatus): string {
  return TONE_DOT_CLASS[agentStatusTone(state)] ?? 'bg-fg-faint'
}

function at(x: number, y: number) {
  const [sx, sy] = toScreen(props.cam, x, y)
  return { transform: `translate(${sx}px, ${sy}px)` }
}

function atPolar(radius: number, deg: number) {
  return at(...polar(radius, deg))
}

function showsLabel(pid: number): boolean {
  return !props.labelledAgents || props.labelledAgents.has(pid)
}
</script>

<template>
  <div ref="root" class="pointer-events-none absolute inset-0 [&>*]:absolute [&>*]:left-0 [&>*]:top-0">
    <button
      type="button"
      data-testid="hub-core"
      :title="coreTitle"
      :aria-disabled="coreDisabled"
      class="pointer-events-auto flex size-20 -translate-1/2 cursor-pointer flex-col items-center justify-center rounded-full border border-accent bg-card shadow-[0_0_40px_color-mix(in_oklch,var(--accent)_18%,transparent)]"
      :style="at(0, 0)"
      @click="$emit('core')"
    >
      <b class="text-[13px] text-fg">Kontor</b>
      <span class="text-[10px] text-fg-mute">{{ running }} running · {{ waiting }} need you</span>
      <span
        v-if="needsYou > 0"
        data-testid="hub-core-needs-you"
        class="absolute -right-1 -top-1 rounded-full border border-warning-line bg-warning-soft px-1.5 text-[10px] font-semibold text-warning-text"
      >{{ needsYou }}<span class="sr-only"> in the needs-you queue</span></span>
    </button>

    <template v-if="sectorNamesShown">
      <button
        v-for="(sector, i) in sectors"
        :key="sector.key"
        type="button"
        :data-testid="`hub-sector-${i}`"
        :data-label-key="sectorLabelKey(sector.label, sector.weight)"
        class="pointer-events-auto -translate-1/2 cursor-pointer whitespace-nowrap rounded px-1.5 py-0.5 text-[10.5px] font-semibold uppercase tracking-widest hover:bg-fg/5"
        :class="level === 1 && 'opacity-55'"
        :style="{ ...atPolar(sectorNameRadius, sectorMid(sector)), color: `var(--sector-${i % SECTOR_PALETTE_SIZE})` }"
        @click="$emit('sector', sector)"
      >
        {{ sector.label }}<small class="ml-1 font-normal normal-case tracking-normal text-fg-mute">{{ sector.weight }}</small>
      </button>
    </template>
    <template v-if="level < 2">
      <span
        v-for="ring in ringLabels"
        :key="ring.label"
        data-testid="hub-ring-label"
        class="-translate-1/2 text-[9px] text-fg-faint"
        :style="atPolar(radiusForAge(ring.days), RING_LABEL_DEG)"
      >{{ ring.label }}</span>
    </template>

    <!-- After the sector names, so the agent layer paints and takes pointer events above the legend. -->
    <button
      v-for="{ agent, x, y, state, needsOperator } in agents"
      :key="agent.pid"
      type="button"
      :data-testid="`hub-agent-${agent.pid}`"
      :aria-label="`${friendlyProjectName(agent.projectName)}, ${statusLabel(state)}${needsOperator ? ', needs you' : ''}`"
      :title="friendlyProjectName(agent.projectName)"
      class="group pointer-events-auto relative flex -translate-x-1/2 -translate-y-[9px] cursor-pointer"
      :style="at(x, y)"
      @click="$emit('agent', agent)"
    >
      <span
        class="size-[18px] rounded-full border-[3px] border-app"
        :class="[dotClass(state), needsOperator && 'outline-2 outline-warning motion-safe:animate-pulse']"
      />
      <!-- Absolute, so a culled label neither grows the button's hit box nor covers a neighbour's dot. -->
      <span
        data-testid="hub-label"
        :data-label-key="agentLabelKey(agent.projectName, state)"
        class="absolute left-1/2 top-full mt-[3px] flex -translate-x-1/2 gap-1 whitespace-nowrap rounded-md border bg-card/85 px-1.5 text-[10.5px] text-fg"
        :class="[needsOperator ? 'border-warning' : 'border-line', !showsLabel(agent.pid) && 'invisible group-hover:visible group-focus-visible:visible']"
      >
        <span data-testid="hub-label-name" class="max-w-[14ch] truncate">{{ friendlyProjectName(agent.projectName) }}</span>
        <em class="not-italic" :class="needsOperator ? 'text-warning-text' : 'text-fg-mute'">{{ statusLabel(state) }}</em>
      </span>
    </button>
  </div>
</template>
