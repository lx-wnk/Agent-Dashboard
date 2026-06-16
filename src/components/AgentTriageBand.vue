<script setup lang="ts">
import type { Agent, PendingPermission } from '../types'
import { computed, nextTick, ref, watch } from 'vue'
import { useAgentIdentity } from '../composables/useAgentIdentity'
import { useNow } from '../composables/useNow'
import { usePermissionResolve } from '../composables/usePermissionResolve'
import { attentionFor } from '../utils/attention'
import { formatErrorState, formatRelativeActivity, secondsSince, shortModel } from '../utils/format'

const props = defineProps<{
  agents: Agent[]
  focusedSessionId?: string | null
}>()
const emit = defineEmits<{
  select: [agent: Agent]
  toast: [message: string]
}>()

const { getIdentity } = useAgentIdentity()
const { nowMs } = useNow()
const { resolving, resolveAgent, approveAll } = usePermissionResolve()

// Tone → Tailwind border + text classes
const toneBorderClass: Record<string, string> = {
  warning: 'border-yellow-400 dark:border-yellow-400',
  danger: 'border-red-400 dark:border-red-400',
  info: 'border-blue-400 dark:border-blue-400',
}

const toneLeftClass: Record<string, string> = {
  warning: 'border-l-yellow-400 dark:border-l-yellow-400',
  danger: 'border-l-red-400 dark:border-l-red-400',
  info: 'border-l-blue-400 dark:border-l-blue-400',
}

const toneLabelClass: Record<string, string> = {
  warning: 'text-yellow-700 dark:text-yellow-400',
  danger: 'text-red-600 dark:text-red-400',
  info: 'text-blue-600 dark:text-blue-400',
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

const totalPendingCount = computed(() =>
  resolvableAgents.value.reduce((sum, a) => sum + (a.pendingPermissions?.length ?? 0), 0),
)

// --- Approve-all bar ---
const approveAllOpen = ref(false)
// Selected agent sessionIds for the approve-all action; default all
const selectedSessionIds = ref<Set<string>>(new Set())

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

async function handleResolveAgent(agent: Agent, outcome: 'granted' | 'denied') {
  const err = await resolveAgent(agent, outcome)
  if (err)
    emit('toast', err)
}

async function handleApproveAll() {
  approveAllInFlight.value = true
  const selected = resolvableAgents.value.filter(a => selectedSessionIds.value.has(a.sessionId))
  const err = await approveAll(selected)
  approveAllInFlight.value = false
  if (err)
    emit('toast', err)
}

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
      class="flex items-center gap-2 px-4 py-2.5 rounded-md bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800/50"
    >
      <span aria-hidden="true" class="text-green-600 dark:text-green-400">✓</span>
      <span class="text-[13px] font-medium text-green-700 dark:text-green-400">All clear — no agent is waiting on you.</span>
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
        <!-- Discoverability hint for keyboard shortcuts -->
        <span class="ml-auto text-[10px] text-fg-faint font-mono select-none hidden sm:inline" aria-hidden="true">
          <kbd class="not-italic">n</kbd> next ·
          <kbd class="not-italic">a</kbd> approve ·
          <kbd class="not-italic">d</kbd> deny ·
          <kbd class="not-italic">⇧A</kbd> approve all ·
          <kbd class="not-italic">c</kbd> density
        </span>
      </div>

      <!-- Approve-all bar: shown when 2+ pending permissions are resolvable -->
      <div
        v-if="totalPendingCount >= 2"
        class="mb-3 rounded-lg border border-yellow-300 dark:border-yellow-700 bg-yellow-50 dark:bg-yellow-900/20 overflow-hidden"
      >
        <div class="flex items-center gap-3 px-3 py-2">
          <span class="text-[12px] font-semibold text-yellow-800 dark:text-yellow-300">
            {{ resolvableAgents.length }} {{ resolvableAgents.length === 1 ? 'agent' : 'agents' }} waiting on a permission
          </span>
          <button
            type="button"
            class="ml-auto text-[11px] text-fg-mute underline-offset-2 hover:text-fg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
            :aria-expanded="approveAllOpen"
            @click="approveAllOpen = !approveAllOpen"
          >
            {{ approveAllOpen ? 'Hide review' : 'Review what\'s affected' }}
          </button>
          <button
            type="button"
            :disabled="approveAllInFlight || selectedCount === 0"
            class="inline-flex items-center justify-center font-semibold rounded-md cursor-pointer transition-all text-xs px-2.5 py-1 bg-green-600 text-white hover:brightness-110 disabled:opacity-40 disabled:cursor-not-allowed focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:ring-green-500"
            @click="handleApproveAll"
          >
            Approve all{{ selectedCount < resolvableAgents.length ? ` (${selectedCount})` : '' }}
          </button>
        </div>

        <!-- Collapsible disclosure: per-agent + command list with deselect checkboxes -->
        <div v-if="approveAllOpen" class="border-t border-yellow-200 dark:border-yellow-800 px-3 py-2 flex flex-col gap-2">
          <div
            v-for="agent in resolvableAgents"
            :key="agent.sessionId"
            class="flex items-start gap-2"
          >
            <input
              :id="`approve-all-${agent.sessionId}`"
              type="checkbox"
              class="mt-0.5 accent-green-600"
              :checked="selectedSessionIds.has(agent.sessionId)"
              :aria-label="`Include ${agent.projectName} in approve-all`"
              @change="toggleAgentSelection(agent.sessionId)"
            >
            <div class="flex flex-col gap-0.5 min-w-0">
              <label :for="`approve-all-${agent.sessionId}`" class="text-[12px] font-medium text-fg cursor-pointer">
                {{ agent.projectName }}
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

      <!-- Agent cards -->
      <div class="flex flex-wrap gap-2.5">
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
            <span class="font-semibold text-[13px] text-fg truncate">{{ agent.projectName }}</span>
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
              <button
                type="button"
                :disabled="resolving[agent.sessionId]"
                class="inline-flex items-center justify-center font-semibold rounded-md cursor-pointer transition-all text-xs px-2.5 py-1 bg-green-600 text-white hover:brightness-110 disabled:opacity-40 disabled:cursor-not-allowed focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:ring-green-500"
                :aria-label="`Approve permissions for ${agent.projectName}`"
                @click="handleResolveAgent(agent, 'granted')"
              >
                Approve
              </button>
              <button
                type="button"
                :disabled="resolving[agent.sessionId]"
                class="inline-flex items-center justify-center font-semibold rounded-md cursor-pointer transition-all text-xs px-2.5 py-1 bg-red-600 text-white hover:brightness-110 disabled:opacity-40 disabled:cursor-not-allowed focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:ring-red-500"
                :aria-label="`Deny permissions for ${agent.projectName}`"
                @click="handleResolveAgent(agent, 'denied')"
              >
                Deny
              </button>
            </template>

            <button
              type="button"
              class="ml-auto text-[12px] font-semibold px-3 py-1 rounded-md border border-line bg-raised text-fg-mute hover:text-fg hover:bg-raised/80 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
              :aria-label="`Open details for ${agent.projectName}`"
              @click="emit('select', agent)"
            >
              Open ↗
            </button>
          </div>
        </div>
      </div>
    </template>
  </section>
</template>
