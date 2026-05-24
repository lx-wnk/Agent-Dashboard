<script setup lang="ts">
import type { Agent } from '../types'
import { computed } from 'vue'
import { useAgentIdentity } from '../composables/useAgentIdentity'
import { formatCost, formatTokens, formatUptime, shortModel, totalTokenCount } from '../utils/format'
import MachineBadge from './MachineBadge.vue'
import PromptInput from './PromptInput.vue'
import AppBadge from './ui/AppBadge.vue'

const props = defineProps<{ agent: Agent }>()
defineEmits<{ select: [agent: Agent] }>()

const { getIdentity } = useAgentIdentity()

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
</script>

<template>
  <div
    class="rounded-lg overflow-hidden cursor-pointer border border-line/50 bg-card transition-all hover:border-slate-400 dark:hover:border-slate-600 hover:shadow-md dark:hover:shadow-[0_2px_12px_rgba(0,0,0,0.3)] focus-visible:outline-2 focus-visible:outline-blue-500 focus-visible:outline-offset-[-2px]"
    tabindex="0"
    role="button"
    :aria-label="`Open details for ${agent.projectName}`"
    @click="$emit('select', agent)"
    @keydown.enter="$emit('select', agent)"
    @keydown.space.prevent="$emit('select', agent)"
  >
    <div class="bg-raised px-3 py-2 flex justify-between items-center gap-2">
      <div class="flex items-center gap-2 min-w-0">
        <AppBadge :variant="agent.status" />
        <span class="mr-1" aria-hidden="true">{{ getIdentity(agent.projectPath).emoji }}</span>
        <span class="font-semibold text-[13px] text-fg whitespace-nowrap overflow-hidden text-ellipsis">{{ agent.projectName }}</span>
        <span class="text-[11px] text-fg-mute whitespace-nowrap">{{ shortModel(agent.model ?? null) }} · {{ formatCost(agent.costEstimate) }}</span>
        <span
          class="text-[10px] font-mono px-1.5 py-0.5 rounded"
          :class="healthChipClass"
          :title="`Health score: ${agent.healthScore}/100`"
        >{{ agent.healthScore }}</span>
        <MachineBadge v-if="agent.machine" :machine="agent.machine" />
      </div>
      <div class="flex-shrink-0 flex flex-col items-end">
        <span class="text-[11px] text-fg-mute whitespace-nowrap">{{ formatTokens(totalTokens) }} tok · {{ formatUptime(agent.uptime) }}</span>
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
    <div v-if="agent.lastBtw" class="border-t border-line px-3 py-2 flex flex-col gap-1 text-[12px] font-mono" @click.stop>
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
    <PromptInput v-if="!agent.machine" :agent="agent" variant="compact" @click.stop @keydown.enter.stop @keydown.space.stop />
  </div>
</template>
