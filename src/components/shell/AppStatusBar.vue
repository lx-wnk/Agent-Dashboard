<script setup lang="ts">
import type { SystemInfo } from '../../composables/useSystemResources'
import { computed } from 'vue'
import { useStatusBar } from '../../composables/useStatusBar'
import { useSystemResources } from '../../composables/useSystemResources'

defineProps<{ costDelta: number | null, todayCostLabel: string }>()

const { collapsed, openSegment, toggleSegment, toggleCollapsed } = useStatusBar()
const resources = useSystemResources()
// Explicitly read .value so this works with both a real Ref<SystemInfo> (production)
// and a plain { value: SystemInfo } object returned by the test mock.
const systemInfo = computed<SystemInfo | null>(() => resources.info.value)

function barColor(pct: number): string {
  return pct > 85 ? 'bg-danger' : pct > 60 ? 'bg-warning' : 'bg-success'
}

function formatDelta(d: number | null): string {
  if (d === null)
    return '—'
  const sign = d < 0 ? '-' : d > 0 ? '+' : ''
  return `${sign}$${Math.abs(d).toFixed(2)}`
}
</script>

<template>
  <div v-if="collapsed" class="shrink-0 flex justify-end border-t border-line bg-card px-2 py-0.5">
    <button
      type="button"
      data-testid="statusbar-tab"
      class="text-[10px] text-fg-faint hover:text-fg px-2 py-0.5 rounded focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-card"
      aria-label="Expand status bar"
      @click="toggleCollapsed"
    >
      ▴ metrics
    </button>
  </div>

  <div v-else class="shrink-0 border-t border-line bg-card">
    <div v-if="openSegment === 'system'" data-testid="panel-system" class="px-4 py-3 border-b border-line text-[12px] text-fg-mute">
      <div v-if="systemInfo" class="grid grid-cols-2 sm:grid-cols-4 gap-3 font-mono">
        <div>CPU {{ Math.round(systemInfo.cpu.usage) }}% · {{ systemInfo.cpu.cores }} cores</div>
        <div>MEM {{ Math.round(systemInfo.memory.usagePercent) }}%</div>
        <div>DISK {{ Math.round(systemInfo.disk.usagePercent) }}%</div>
        <div>LOAD {{ systemInfo.loadAvg.map(l => l.toFixed(2)).join(' ') }}</div>
      </div>
    </div>
    <div v-if="openSegment === 'cost'" data-testid="panel-cost" class="px-4 py-3 border-b border-line text-[12px] text-fg-mute font-mono flex flex-col gap-1">
      <span>Today's spend: {{ todayCostLabel }}</span>
      <span>Burn rate (last 5 min): {{ formatDelta(costDelta) }}</span>
    </div>

    <div class="flex items-center gap-3 px-3 h-7 text-[11px] font-mono text-fg-mute">
      <button
        type="button"
        data-testid="seg-system"
        class="flex items-center gap-3 hover:text-fg rounded px-1 focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:ring-offset-1 focus-visible:ring-offset-card"
        :aria-expanded="openSegment === 'system'"
        aria-label="Toggle system metrics detail"
        @click="toggleSegment('system')"
      >
        <span v-if="systemInfo" class="flex items-center gap-1">CPU
          <span class="inline-block w-10 h-1.5 bg-raised rounded-full overflow-hidden align-middle">
            <span class="block h-full rounded-full" :class="barColor(systemInfo.cpu.usage)" :style="{ width: `${systemInfo.cpu.usage}%` }" /></span>
          {{ Math.round(systemInfo.cpu.usage) }}%</span>
        <span v-if="systemInfo">MEM {{ Math.round(systemInfo.memory.usagePercent) }}%</span>
        <span v-if="systemInfo">DISK {{ Math.round(systemInfo.disk.usagePercent) }}%</span>
      </button>
      <span class="w-px h-3.5 bg-line" aria-hidden="true" />
      <button
        type="button"
        data-testid="seg-cost"
        class="hover:text-fg rounded px-1 focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:ring-offset-1 focus-visible:ring-offset-card"
        :aria-expanded="openSegment === 'cost'"
        aria-label="Toggle cost trend detail"
        @click="toggleSegment('cost')"
      >
        TODAY <span class="text-fg">{{ todayCostLabel }}</span>
        <span class="text-fg-faint">·</span>
        5m <span :class="(costDelta ?? 0) > 0 ? 'text-green-500' : 'text-fg-faint'">{{ formatDelta(costDelta) }}</span>
      </button>
      <button
        type="button"
        data-testid="statusbar-collapse"
        class="ml-auto text-fg-faint hover:text-fg px-1 rounded focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:ring-offset-1 focus-visible:ring-offset-card"
        aria-label="Collapse status bar"
        @click="toggleCollapsed"
      >
        ▾
      </button>
    </div>
  </div>
</template>
