<script setup lang="ts">
import type { TrendPoint } from '@/features/agents/composables/useAgents'
import { computed } from 'vue'
import { formatTokens } from '@/utils/format'

const props = defineProps<{ trend: TrendPoint[] }>()

// Take last 60 points (3 minutes of data) for the sparkline
const sparkData = computed(() => {
  const points = props.trend.slice(-60)
  if (points.length < 2)
    return []
  return points
})

const maxCost = computed(() => {
  const costs = sparkData.value.map(p => p.cost)
  return Math.max(...costs, 0.01) // min 0.01 to avoid division by zero
})

const costDelta = computed(() => {
  const pts = props.trend
  if (pts.length < 2)
    return null
  const recent = pts[pts.length - 1].cost
  const older = pts[Math.max(0, pts.length - 61)].cost
  return recent - older
})

const tokenDelta = computed(() => {
  const pts = props.trend
  if (pts.length < 2)
    return null
  const recent = pts[pts.length - 1].tokens
  const older = pts[Math.max(0, pts.length - 61)].tokens
  return recent - older
})
</script>

<template>
  <div v-if="sparkData.length >= 2" class="flex flex-col gap-1 px-6 py-1.5 bg-card border-b border-line">
    <div class="flex items-center gap-2">
      <span class="text-[11px] text-fg-mute">Cost trend (3min)</span>
      <span v-if="costDelta !== null" class="text-[11px] font-mono" :class="costDelta > 0 ? 'text-danger-text' : 'text-fg-mute'">
        {{ costDelta > 0 ? '+' : '' }}${{ costDelta.toFixed(2) }}
      </span>
      <span v-if="tokenDelta !== null && tokenDelta > 0" class="text-[11px] font-mono text-info-text">
        +{{ formatTokens(tokenDelta) }} tok
      </span>
    </div>
    <div class="flex items-end gap-px h-6">
      <div
        v-for="(point, i) in sparkData"
        :key="i"
        class="flex-1 min-w-0.5 bg-success rounded-t-px transition-opacity"
        :style="{ height: `${Math.max(2, (point.cost / maxCost) * 100)}%` }"
        :title="`$${point.cost.toFixed(2)}`"
        :class="i === sparkData.length - 1 ? 'opacity-100' : 'opacity-70 hover:opacity-100'"
      />
    </div>
  </div>
</template>
