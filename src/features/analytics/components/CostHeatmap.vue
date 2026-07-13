<script setup lang="ts">
import { computed, watch } from 'vue'
import { useTheme } from '@/composables/useTheme'
import { toast } from '@/composables/useToast'
import { useCostHeatmap } from '@/features/analytics/composables/useCostHeatmap'
import { chartColors } from '@/utils/chartColors'

const RGB_CHANNELS_RE = /^rgba?\(([^)]+?)(?:,\s*[\d.]+)?\)$/

const DOW_LABELS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
const HOUR_LABELS = Array.from({ length: 24 }, (_, i) => `${i.toString().padStart(2, '0')}:00`)

const { grid, loading, error } = useCostHeatmap()

// Surface async load failures as toasts; the view keeps its empty/loading state.
watch(error, (msg) => {
  if (msg)
    toast.error(msg)
})

const { theme } = useTheme()

// Resolve the token channels once per render (re-evaluates on theme toggle), then
// splice the per-cell alpha — avoids a chartColors() DOM probe for every cell.
const heatmapChannels = computed(() => {
  void theme.value // re-evaluate when the theme toggles
  const m = chartColors().info.match(RGB_CHANNELS_RE)
  return m ? m[1] : '59, 130, 246'
})

function heatmapColor(opacity: number): string {
  return `rgba(${heatmapChannels.value}, ${opacity})`
}

function maxCost(): number {
  return Math.max(1, ...grid.value.flatMap(row => row))
}

function cellOpacity(cost: number): number {
  return cost === 0 ? 0 : 0.1 + 0.9 * (cost / maxCost())
}

function cellLabel(dow: string, hourIdx: number, cost: number): string {
  return `${dow} ${HOUR_LABELS[hourIdx]}: $${cost.toFixed(4)}`
}
</script>

<template>
  <div class="cost-heatmap p-4">
    <h3 class="text-sm font-semibold mb-3 text-fg-soft">
      Cost by Day & Hour
    </h3>
    <div v-if="loading" class="text-sm text-slate-500">
      Loading heatmap…
    </div>
    <div v-else class="overflow-x-auto">
      <table class="heatmap-table border-collapse">
        <caption class="sr-only">
          Cost by hour of day and day of week
        </caption>
        <thead>
          <tr>
            <!-- empty corner cell aligns with row-header column -->
            <td />
            <th
              v-for="h in HOUR_LABELS"
              :key="h"
              scope="col"
              class="text-[9px] text-slate-400 text-center font-normal px-0"
              style="width: 28px; min-width: 28px;"
            >
              {{ h.slice(0, 2) }}
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(dow, dowIdx) in DOW_LABELS" :key="dow" class="mb-0.5">
            <th
              scope="row"
              class="text-[11px] text-slate-500 text-right pr-2 font-normal"
              style="width: 40px; min-width: 40px;"
            >
              {{ dow }}
            </th>
            <td
              v-for="hourIdx in 24"
              :key="hourIdx - 1"
              class="p-0"
              style="width: 28px; min-width: 28px; height: 20px;"
              :aria-label="cellLabel(dow, hourIdx - 1, grid[dowIdx][hourIdx - 1])"
            >
              <!-- decorative color fill; data is in aria-label above -->
              <div
                class="h-5 w-full rounded-sm"
                aria-hidden="true"
                :style="{ backgroundColor: heatmapColor(cellOpacity(grid[dowIdx][hourIdx - 1])) }"
              />
            </td>
          </tr>
        </tbody>
      </table>
      <p class="text-[10px] text-slate-400 mt-2">
        Cell intensity proportional to total cost.
      </p>
    </div>
  </div>
</template>

<style scoped>
.heatmap-table {
  border-collapse: collapse;
  table-layout: fixed;
}

.heatmap-table td,
.heatmap-table th {
  padding: 0;
  vertical-align: middle;
}

.heatmap-table tbody tr + tr {
  margin-top: 2px;
}
</style>
