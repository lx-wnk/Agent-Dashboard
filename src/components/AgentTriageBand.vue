<script setup lang="ts">
import type { Agent } from '../types'
import { computed } from 'vue'
import { useAgentIdentity } from '../composables/useAgentIdentity'
import { useNow } from '../composables/useNow'
import { attentionFor } from '../utils/attention'
import { formatErrorState, formatRelativeActivity, secondsSince, shortModel } from '../utils/format'

const props = defineProps<{ agents: Agent[] }>()
const emit = defineEmits<{ select: [agent: Agent] }>()

const { getIdentity } = useAgentIdentity()
const { nowMs } = useNow()

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
      <div class="flex items-center gap-2 px-0.5 pb-2">
        <h2 class="text-[10px] font-bold uppercase tracking-wider text-fg-mute m-0">
          Needs you
        </h2>
        <span
          class="text-[10px] font-bold font-mono bg-red-500 text-white rounded-full px-1.5 leading-[16px]"
          :aria-label="`${agents.length} agents need attention`"
        >{{ agents.length }}</span>
        <span v-if="breakdown" class="text-[11px] text-fg-faint">{{ breakdown }}</span>
      </div>

      <!-- Agent cards -->
      <div class="flex flex-wrap gap-2.5">
        <div
          v-for="agent in agents"
          :key="agent.sessionId"
          class="min-w-[280px] flex-1 basis-[280px] max-w-[420px] rounded-lg bg-card border border-l-[3px] p-3 flex flex-col gap-2"
          :class="[
            toneBorderClass[attentionFor(agent, secondsSince(agent.lastActivity, nowMs))?.tone ?? 'info'],
            toneLeftClass[attentionFor(agent, secondsSince(agent.lastActivity, nowMs))?.tone ?? 'info'],
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

          <!-- Blocked-on detail -->
          <div class="font-mono text-[12px] text-fg-mute bg-app border border-line rounded px-2.5 py-1.5 leading-snug break-words">
            {{ blockedDetail(agent) }}
          </div>

          <!-- Action row -->
          <div class="flex items-center gap-2">
            <span class="text-[11px] text-fg-faint">{{ formatRelativeActivity(secondsSince(agent.lastActivity, nowMs)) }}</span>
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
