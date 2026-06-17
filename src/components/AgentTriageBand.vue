<script setup lang="ts">
import type { Agent, PendingPermission } from '../types'
import { computed, nextTick, ref, watch } from 'vue'
import { useAgentIdentity } from '../composables/useAgentIdentity'
import { useNow } from '../composables/useNow'
import { usePermissionResolve } from '../composables/usePermissionResolve'
import { attentionFor } from '../utils/attention'
import { formatErrorState, formatRelativeActivity, secondsSince, shortModel } from '../utils/format'
import { friendlyProjectName } from '../utils/friendlyProjectName'
import AppButton from './ui/AppButton.vue'

const props = defineProps<{
  agents: Agent[]
  focusedSessionId?: string | null
}>()
const emit = defineEmits<{
  select: [agent: Agent]
  toast: [message: string]
  remembered: []
}>()

const { getIdentity } = useAgentIdentity()
const { nowMs } = useNow()
const { resolving, resolveAgent, approveAll } = usePermissionResolve()

// Tone → Tailwind border + text classes
const toneBorderClass: Record<string, string> = {
  warning: 'border-warning-dot',
  danger: 'border-danger-dot',
  info: 'border-info-dot',
}

const toneLeftClass: Record<string, string> = {
  warning: 'border-l-warning-dot',
  danger: 'border-l-danger-dot',
  info: 'border-l-info-dot',
}

const toneLabelClass: Record<string, string> = {
  warning: 'text-warning-text',
  danger: 'text-danger-text',
  info: 'text-info-text',
}

// Breakdown string: "2 permissions · 1 failed run · 1 stalled · 1 idle"
const breakdown = computed(() => {
  const counts: Record<string, number> = {}
  for (const agent of props.agents) {
    const att = attentionFor(agent, secondsSince(agent.lastActivity, nowMs.value))
    if (att)
      counts[att.kind] = (counts[att.kind] ?? 0) + 1
  }
  const PARTS: [string, string, string][] = [
    ['permission', 'permission', 'permissions'],
    ['error', 'failed run', 'failed runs'],
    ['stalled', 'stalled', 'stalled'],
    ['idle', 'idle', 'idle'],
  ]
  return PARTS
    .filter(([k]) => counts[k])
    .map(([k, s, p]) => `${counts[k]} ${counts[k] === 1 ? s : p}`)
    .join(' · ')
})

function blockedDetail(agent: Agent): string {
  const secs = secondsSince(agent.lastActivity, nowMs.value)
  const att = attentionFor(agent, secs)
  if (!att)
    return ''
  if (att.kind === 'permission')
    return agent.currentAction || 'Waiting for permission'
  if (att.kind === 'error')
    return agent.errorState ? formatErrorState(agent.errorState) : (agent.currentAction || 'Run failed')
  if (att.kind === 'idle')
    return agent.lastOutput ? agent.lastOutput.slice(0, 110) + (agent.lastOutput.length > 110 ? '…' : '') : 'Awaiting next instruction'
  // stalled
  return `Running but silent — last output ${formatRelativeActivity(secs)}`
}

function permissionLabel(p: PendingPermission): string {
  return p.pattern ? `${p.tool}(${p.pattern})` : p.tool
}

// Agents that have resolvable pending permissions (orchestrated + has pendingPermissions)
const resolvableAgents = computed(() =>
  props.agents.filter(a => a.pipelineTaskId && a.pendingPermissions && a.pendingPermissions.length > 0),
)

// --- Approve-all bar ---
const approveAllOpen = ref(false)
// Selected agent sessionIds for the approve-all action; default all
const selectedSessionIds = ref<Set<string>>(new Set())
// Per-agent "don't ask again" toggle — keyed by sessionId
const rememberPerAgent = ref<Record<string, boolean>>({})
// Approve-all "don't ask again" toggle
const rememberAll = ref(false)

// Resolvable agents seen on a previous tick — lets us default brand-new agents
// to selected while preserving a user's explicit deselection across SSE updates.
const seenSessionIds = new Set<string>()

watch(resolvableAgents, (agents) => {
  const current = new Set(agents.map(a => a.sessionId))
  const next = new Set(selectedSessionIds.value)
  for (const id of current) {
    if (!seenSessionIds.has(id))
      next.add(id)
  }
  for (const id of [...next]) {
    if (!current.has(id))
      next.delete(id)
  }
  seenSessionIds.clear()
  for (const id of current) seenSessionIds.add(id)
  selectedSessionIds.value = next
}, { immediate: true })

const selectedCount = computed(() => selectedSessionIds.value.size)

const approveAllInFlight = ref(false)

function toggleAgentSelection(sessionId: string) {
  const next = new Set(selectedSessionIds.value)
  if (next.has(sessionId))
    next.delete(sessionId)
  else
    next.add(sessionId)
  selectedSessionIds.value = next
}

const distinctProjectCount = computed(() => {
  const selected = resolvableAgents.value.filter(a => selectedSessionIds.value.has(a.sessionId))
  return new Set(selected.map(a => a.projectName)).size
})

async function handleResolveAgent(agent: Agent, outcome: 'granted' | 'denied') {
  const remember = rememberPerAgent.value[agent.sessionId] ?? false
  const err = await resolveAgent(agent, outcome, remember)
  if (err) {
    emit('toast', err)
  }
  else if (remember && outcome === 'granted') {
    emit('remembered')
  }
}

async function handleApproveAll() {
  approveAllInFlight.value = true
  const selected = resolvableAgents.value.filter(a => selectedSessionIds.value.has(a.sessionId))
  const err = await approveAll(selected, 'granted', rememberAll.value)
  approveAllInFlight.value = false
  if (err) {
    emit('toast', err)
  }
  else if (rememberAll.value) {
    emit('remembered')
  }
}

// When ≥2 permission agents are present: default to cards collapsed (hasBulk starts true).
// showCards defaults to false so the bulk bar is primary; the toggle reveals individual cards.
const hasBulk = computed(() => resolvableAgents.value.length >= 2)
const showCards = ref(!hasBulk.value)

// When bulk mode activates or deactivates, sync the toggle default.
watch(hasBulk, (bulk) => {
  if (bulk)
    showCards.value = false
  else
    showCards.value = true
})

// Always show cards when a card is keyboard-focused
const cardsVisible = computed(() => showCards.value || !!props.focusedSessionId)

// Scroll the focused card into view when focusedSessionId changes
const cardRefs = ref<Record<string, HTMLElement | null>>({})

function setCardRef(sessionId: string, el: HTMLElement | null) {
  cardRefs.value[sessionId] = el
}

watch(() => props.focusedSessionId, (id) => {
  if (!id)
    return
  nextTick(() => {
    cardRefs.value[id]?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
  })
})
</script>

<template>
  <section class="mb-4" aria-label="Needs your attention">
    <!-- Empty state -->
    <div
      v-if="agents.length === 0"
      class="flex items-center gap-2 px-4 py-2.5 rounded-md bg-success-soft border border-success-line"
    >
      <span aria-hidden="true" class="text-success-text">✓</span>
      <span class="text-[13px] font-medium text-success-text">All clear — no agent is waiting on you.</span>
    </div>

    <template v-else>
      <!-- Header row -->
      <div class="flex items-center gap-2 px-0.5 pb-2 flex-wrap">
        <h2 class="text-[10px] font-bold uppercase tracking-wider text-fg-mute m-0">
          Needs you
        </h2>
        <span
          class="text-[10px] font-bold font-mono bg-red-500 text-white rounded-full px-1.5 leading-[16px]"
          :aria-label="`${agents.length} agents need attention`"
        >{{ agents.length }}</span>
        <span v-if="breakdown" class="text-[11px] text-fg-faint">{{ breakdown }}</span>
        <!-- Keyboard shortcut legend — includes ⌘K search -->
        <span class="ml-auto text-[10px] text-fg-faint font-mono select-none hidden sm:inline" aria-hidden="true">
          <kbd class="not-italic">n</kbd> next ·
          <kbd class="not-italic">a</kbd> approve ·
          <kbd class="not-italic">d</kbd> deny ·
          <kbd class="not-italic">⇧A</kbd> approve all ·
          <kbd class="not-italic">c</kbd> density ·
          <kbd class="not-italic">⌘K</kbd> search
        </span>
      </div>

      <!-- Approve-all bar: shown when 2+ agents are blocked on a permission -->
      <div
        v-if="resolvableAgents.length >= 2"
        class="mb-3 rounded-lg border border-warning-line bg-warning-soft overflow-hidden"
      >
        <div class="flex items-center gap-3 px-3 py-2">
          <span class="text-[12px] font-semibold text-warning-text">
            {{ resolvableAgents.length }} {{ resolvableAgents.length === 1 ? 'agent' : 'agents' }} waiting on a permission
          </span>
          <label class="ml-auto flex items-center gap-1.5 cursor-pointer select-none text-[11px] text-fg-faint">
            <input
              v-model="rememberAll"
              type="checkbox"
              class="accent-success"
              aria-label="Don't ask again for all selected projects"
            >
            Don't ask again{{ rememberAll && distinctProjectCount > 0 ? ` (${distinctProjectCount} project${distinctProjectCount !== 1 ? 's' : ''})` : '' }}
          </label>
          <button
            type="button"
            class="text-[11px] text-fg-mute underline-offset-2 hover:text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent"
            :aria-expanded="approveAllOpen"
            @click="approveAllOpen = !approveAllOpen"
          >
            {{ approveAllOpen ? 'Hide what\'s affected' : 'Review what\'s affected' }}
          </button>
          <AppButton
            variant="success"
            size="sm"
            :disabled="approveAllInFlight || selectedCount === 0"
            @click="handleApproveAll"
          >
            ✓ Approve all{{ selectedCount < resolvableAgents.length ? ` (${selectedCount})` : '' }}
          </AppButton>
        </div>

        <!-- Collapsible disclosure: per-agent + command list with deselect checkboxes -->
        <div v-if="approveAllOpen" class="border-t border-warning-line px-3 py-2 flex flex-col gap-2">
          <div
            v-for="agent in resolvableAgents"
            :key="agent.sessionId"
            class="flex items-start gap-2"
          >
            <input
              :id="`approve-all-${agent.sessionId}`"
              type="checkbox"
              class="mt-0.5 accent-success"
              :checked="selectedSessionIds.has(agent.sessionId)"
              :aria-label="`Include ${agent.projectName} in approve-all`"
              @change="toggleAgentSelection(agent.sessionId)"
            >
            <div class="flex flex-col gap-0.5 min-w-0">
              <label :for="`approve-all-${agent.sessionId}`" class="text-[12px] font-medium text-fg cursor-pointer">
                {{ friendlyProjectName(agent.projectName) }}
              </label>
              <div
                v-for="p in agent.pendingPermissions"
                :key="p.id"
                class="font-mono text-[11px] text-fg-mute"
              >
                {{ permissionLabel(p) }}
                <span v-if="p.reason" class="font-sans text-fg-faint ml-1">— {{ p.reason }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- Toggle: Review individually / Hide individual requests -->
      <button
        type="button"
        class="inline-flex items-center gap-1.5 mb-2 bg-transparent border-none cursor-pointer text-xs text-fg-mute hover:text-fg font-sans focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent rounded"
        :aria-expanded="cardsVisible"
        @click="showCards = !showCards"
      >
        <span
          aria-hidden="true"
          class="text-[10px] transition-transform duration-150"
          :class="cardsVisible ? 'rotate-90' : ''"
        >▸</span>
        {{ cardsVisible ? 'Hide individual requests' : `Review individually (${agents.length})` }}
      </button>

      <!-- Agent cards -->
      <div v-if="cardsVisible" class="flex flex-wrap gap-2.5">
        <div
          v-for="agent in agents"
          :key="agent.sessionId"
          :ref="(el) => setCardRef(agent.sessionId, el as HTMLElement | null)"
          class="min-w-[280px] flex-1 basis-[280px] max-w-[420px] rounded-lg bg-card border border-l-[3px] p-3 flex flex-col gap-2 transition-shadow"
          :class="[
            toneBorderClass[attentionFor(agent, secondsSince(agent.lastActivity, nowMs))?.tone ?? 'info'],
            toneLeftClass[attentionFor(agent, secondsSince(agent.lastActivity, nowMs))?.tone ?? 'info'],
            agent.sessionId === focusedSessionId ? 'ring-2 ring-accent shadow-md' : '',
          ]"
        >
          <!-- Card header: emoji + name + model + attention label -->
          <div class="flex items-center gap-2 min-w-0">
            <span aria-hidden="true" class="text-[15px] shrink-0">{{ getIdentity(agent.projectPath).emoji }}</span>
            <span class="font-semibold text-[13px] text-fg truncate">{{ friendlyProjectName(agent.projectName) }}</span>
            <span class="font-mono text-[11px] text-fg-faint shrink-0">{{ shortModel(agent.model ?? null) }}</span>
            <span
              class="ml-auto text-[10px] font-bold uppercase tracking-wide shrink-0"
              :class="toneLabelClass[attentionFor(agent, secondsSince(agent.lastActivity, nowMs))?.tone ?? 'info']"
            >{{ attentionFor(agent, secondsSince(agent.lastActivity, nowMs))?.label }}</span>
          </div>

          <!-- Permission list for orchestrated agents with pending requests -->
          <template v-if="agent.pipelineTaskId && agent.pendingPermissions?.length">
            <ul class="m-0 p-0 list-none flex flex-col gap-1" :aria-label="`Pending permissions for ${agent.projectName}`">
              <li
                v-for="p in agent.pendingPermissions"
                :key="p.id"
                class="flex flex-col gap-0.5"
              >
                <span class="font-mono text-[12px] text-fg bg-app border border-line rounded px-2.5 py-1 leading-snug">{{ permissionLabel(p) }}</span>
                <span v-if="p.reason" class="text-[11px] text-fg-faint px-0.5">{{ p.reason }}</span>
              </li>
            </ul>
          </template>

          <!-- Fallback blocked-on detail for non-resolvable permission agents and other kinds -->
          <template v-else>
            <div class="font-mono text-[12px] text-fg-mute bg-app border border-line rounded px-2.5 py-1.5 leading-snug break-words">
              {{ blockedDetail(agent) }}
            </div>
          </template>

          <!-- Action row -->
          <div class="flex items-center gap-2 flex-wrap">
            <span class="text-[11px] text-fg-faint">{{ formatRelativeActivity(secondsSince(agent.lastActivity, nowMs)) }}</span>

            <!-- Approve/Deny buttons only for resolvable agents -->
            <template v-if="agent.pipelineTaskId && agent.pendingPermissions?.length">
              <AppButton
                variant="success"
                size="sm"
                :disabled="resolving[agent.sessionId]"
                :aria-label="`Approve permissions for ${agent.projectName}`"
                @click="handleResolveAgent(agent, 'granted')"
              >
                Approve
              </AppButton>
              <AppButton
                variant="danger"
                size="sm"
                :disabled="resolving[agent.sessionId]"
                :aria-label="`Deny permissions for ${agent.projectName}`"
                @click="handleResolveAgent(agent, 'denied')"
              >
                Deny
              </AppButton>
              <label class="flex items-center gap-1 cursor-pointer select-none text-[11px] text-fg-faint ml-auto">
                <input
                  v-model="rememberPerAgent[agent.sessionId]"
                  type="checkbox"
                  class="accent-success"
                  :aria-label="`Don't ask again for ${agent.projectName}`"
                >
                <span class="font-mono">{{ friendlyProjectName(agent.projectName) }}</span>
              </label>
            </template>

            <AppButton
              variant="outline"
              size="sm"
              :class="!(agent.pipelineTaskId && agent.pendingPermissions?.length) ? 'ml-auto' : ''"
              :aria-label="`Open details for ${agent.projectName}`"
              @click="emit('select', agent)"
            >
              Open ↗
            </AppButton>
          </div>
        </div>
      </div>
    </template>
  </section>
</template>
