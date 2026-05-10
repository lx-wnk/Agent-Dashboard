<script setup lang="ts">
import { onMounted, ref } from 'vue'

const DOW_LABELS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
const HOUR_LABELS = Array.from({ length: 24 }, (_, i) => `${i.toString().padStart(2, '0')}:00`)

const grid = ref<number[][]>(Array.from({ length: 7 }, () => Array.from<number>({ length: 24 }).fill(0)))
const loading = ref(false)
const error = ref<string | null>(null)

async function fetchHeatmap() {
  loading.value = true
  error.value = null
  try {
    const res = await fetch('/api/analytics/heatmap')
    if (!res.ok)
      throw new Error(await res.text())
    const data = await res.json() as { grid: number[][] }
    grid.value = data.grid
  }
  catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Failed to load heatmap'
  }
  finally {
    loading.value = false
  }
}

function maxCost(): number {
  return Math.max(1, ...grid.value.flatMap(row => row))
}

function cellOpacity(cost: number): number {
  return cost === 0 ? 0 : 0.1 + 0.9 * (cost / maxCost())
}

onMounted(fetchHeatmap)
</script>

<template>
  <div class="cost-heatmap p-4">
    <h3 class="text-sm font-semibold mb-3 text-slate-700 dark:text-slate-300">
      Cost by Day & Hour
    </h3>
    <div v-if="loading" class="text-sm text-slate-500">
      Loading heatmap…
    </div>
    <div v-else-if="error" class="text-sm text-red-500">
      {{ error }}
    </div>
    <div v-else class="overflow-x-auto">
      <div class="flex">
        <div class="w-10 flex-shrink-0" />
        <div style="display: grid; grid-template-columns: repeat(24, minmax(28px, 1fr));">
          <span
            v-for="h in HOUR_LABELS"
            :key="h"
            class="text-[9px] text-slate-400 truncate text-center"
          >{{ h.slice(0, 2) }}</span>
        </div>
      </div>
      <div v-for="(dow, dowIdx) in DOW_LABELS" :key="dow" class="flex items-center mb-0.5">
        <div class="w-10 text-[11px] text-slate-500 flex-shrink-0 text-right pr-2">
          {{ dow }}
        </div>
        <div style="display: grid; grid-template-columns: repeat(24, minmax(28px, 1fr)); gap: 2px; flex: 1;">
          <div
            v-for="(_, hourIdx) in 24"
            :key="hourIdx"
            class="h-5 rounded-sm"
            :style="{ backgroundColor: `rgba(59, 130, 246, ${cellOpacity(grid[dowIdx][hourIdx])})` }"
            :title="`${dow} ${HOUR_LABELS[hourIdx]}: $${grid[dowIdx][hourIdx].toFixed(4)}`"
          />
        </div>
      </div>
      <p class="text-[10px] text-slate-400 mt-2">
        Cell intensity proportional to total cost. Hover for exact value.
      </p>
    </div>
  </div>
</template>
