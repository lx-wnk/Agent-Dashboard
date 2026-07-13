<script setup lang="ts">
import type { Agent } from '@/types'
import { computed } from 'vue'
import { useNow } from '@/composables/useNow'
import { formatBurnRate, formatCost, formatRelativeActivity, formatUptime, secondsSince } from '@/utils/format'

const props = defineProps<{ agent: Agent }>()
const { nowMs } = useNow()

const lastActivity = computed(() => formatRelativeActivity(secondsSince(props.agent.lastActivity, nowMs.value)))
const burn = computed(() => formatBurnRate(props.agent.costEstimate, props.agent.uptime))
const hasCache = computed(() => props.agent.cacheCreationCostEstimate > 0 || props.agent.cacheReadCostEstimate > 0)
</script>

<template>
  <div
    class="absolute right-0 top-full mt-1 z-20 w-44 rounded-md border border-line bg-card shadow-card-hover px-2.5 py-2 text-[11px] font-mono flex flex-col gap-0.5"
    data-testid="metrics-popover"
    role="tooltip"
  >
    <div class="flex justify-between gap-3">
      <span class="text-fg-mute">Uptime</span><span class="text-fg">{{ formatUptime(agent.uptime) }}</span>
    </div>
    <div class="flex justify-between gap-3">
      <span class="text-fg-mute">Last activity</span><span class="text-fg">{{ lastActivity }}</span>
    </div>
    <div v-if="burn !== '—'" class="flex justify-between gap-3">
      <span class="text-fg-mute">Burn rate</span><span class="text-fg">{{ burn }}</span>
    </div>
    <template v-if="hasCache">
      <div class="flex justify-between gap-3">
        <span class="text-fg-mute">Cache write</span><span class="text-fg">{{ formatCost(agent.cacheCreationCostEstimate) }}</span>
      </div>
      <div class="flex justify-between gap-3">
        <span class="text-fg-mute">Cache read</span><span class="text-fg">{{ formatCost(agent.cacheReadCostEstimate) }}</span>
      </div>
    </template>
  </div>
</template>
