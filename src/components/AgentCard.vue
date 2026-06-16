<script setup lang="ts">
import type { Agent } from '../types'
import { computed } from 'vue'
import { useAgentIdentity } from '../composables/useAgentIdentity'
import { useNow } from '../composables/useNow'
import { formatBurnRate, formatCost, formatRelativeActivity, formatTokens, formatUptime, isStalled, secondsSince, shortModel, totalTokenCount } from '../utils/format'
import MachineBadge from './MachineBadge.vue'
import PromptInput from './PromptInput.vue'
import ProviderBadge from './ProviderBadge.vue'
import AppBadge from './ui/AppBadge.vue'
import AppCard from './ui/AppCard.vue'

const props = defineProps<{ agent: Agent }>()
defineEmits<{ select: [agent: Agent] }>()

const { getIdentity } = useAgentIdentity()
const { nowMs } = useNow()

const totalTokens = computed(() => totalTokenCount(props.agent.tokenUsage))

const hasCacheCosts = computed(
  () => props.agent.cacheCreationCostEstimate > 0 || props.agent.cacheReadCostEstimate > 0,
)

const healthChipClass = computed(() => {
  const s = props.agent.healthScore
  if (s >= 75)
    return 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400'
  if (s >= 40)
    return 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400'
  return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400'
})

const secSince = computed(() => secondsSince(props.agent.lastActivity, nowMs.value))
const relActivity = computed(() => formatRelativeActivity(secSince.value))
const stalled = computed(() => isStalled(props.agent.status, secSince.value))
const burnRate = computed(() => formatBurnRate(props.agent.costEstimate, props.agent.uptime))
</script>

<template>
  <AppCard surface="card" radius="lg" interactive class="relative overflow-hidden cursor-pointer">
    <button
      type="button"
      class="absolute inset-0 w-full h-full focus-visible:outline-2 focus-visible:outline-blue-500 focus-visible:outline-offset-[-2px]"
      :aria-label="`Open details for ${agent.projectName}`"
      data-testid="agent-card-open"
      @click="$emit('select', agent)"
    />
    <div class="bg-raised px-3 py-2 flex justify-between items-center gap-2">
      <div class="flex items-center gap-2 min-w-0">
        <AppBadge :variant="agent.status" />
        <span
          v-if="stalled"
          class="text-[10px] font-medium px-1 py-0.5 rounded bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400 whitespace-nowrap"
          title="Agent is active but has produced no output for 3+ minutes"
        >stalled</span>
        <span class="mr-1" aria-hidden="true">{{ getIdentity(agent.projectPath).emoji }}</span>
        <span class="font-semibold text-[13px] text-fg whitespace-nowrap overflow-hidden text-ellipsis">{{ agent.projectName }}</span>
        <ProviderBadge :provider="agent.provider" />
        <span class="text-[11px] text-fg-mute whitespace-nowrap">
          {{ shortModel(agent.model ?? null) }} ·
          <span v-if="agent.costUnknown" title="Cost unknown — no pricing data for this provider/model">?</span>
          <template v-else>{{ formatCost(agent.costEstimate) }}</template>
        </span>
        <span
          class="text-[10px] font-mono px-1.5 py-0.5 rounded"
          :class="healthChipClass"
          :title="`Health score: ${agent.healthScore}/100`"
        >{{ agent.healthScore }}</span>
        <MachineBadge v-if="agent.machine" :machine="agent.machine" />
      </div>
      <div class="flex-shrink-0 flex flex-col items-end gap-0.5">
        <span class="text-[11px] text-fg-mute whitespace-nowrap">{{ formatTokens(totalTokens) }} tok · {{ formatUptime(agent.uptime) }}</span>
        <span class="text-[10px] font-mono text-fg-mute whitespace-nowrap">{{ relActivity }}</span>
        <span v-if="burnRate !== '—'" class="text-[10px] font-mono text-fg-mute whitespace-nowrap">{{ burnRate }}</span>
        <div v-if="hasCacheCosts" class="flex gap-2 text-[10px] text-fg-mute">
          <span title="Cache write cost">W {{ formatCost(agent.cacheCreationCostEstimate) }}</span>
          <span title="Cache read cost">R {{ formatCost(agent.cacheReadCostEstimate) }}</span>
        </div>
      </div>
    </div>
    <div class="relative px-3 py-3 h-[150px] overflow-hidden text-[13px] leading-relaxed text-fg-mute font-mono">
      <template v-if="agent.lastOutput">
        {{ agent.lastOutput }}
      </template>
      <span v-else class="text-fg-mute italic">No output yet</span>
      <div class="absolute bottom-0 left-0 right-0 h-8 bg-gradient-to-t from-white dark:from-slate-900 to-transparent pointer-events-none" />
    </div>
    <div v-if="agent.lastBtw" class="relative z-10 border-t border-line px-3 py-2 flex flex-col gap-1 text-[12px] font-mono" @click.stop>
      <div class="text-fg-mute border-l-2 border-yellow-400/60 pl-2 whitespace-nowrap overflow-hidden text-ellipsis">
        {{ agent.lastBtw.message }}
      </div>
      <div v-if="agent.lastBtw.response" class="text-fg-mute border-l-2 border-yellow-400/60 pl-2 whitespace-nowrap overflow-hidden text-ellipsis">
        {{ agent.lastBtw.response }}
      </div>
      <div v-else class="text-fg-mute pl-2.5" style="animation: pulse 2s ease-in-out infinite;">
        ...
      </div>
    </div>
    <PromptInput v-if="!agent.machine" :agent="agent" variant="compact" class="relative z-10" @click.stop @keydown.enter.stop @keydown.space.stop />
  </AppCard>
</template>
