<script setup lang="ts">
import type { SystemInfo } from '../../composables/useSystemResources'
import type { UsageData, WindowData } from '../../composables/useUsage'
import { computed } from 'vue'
import { useStatusBar } from '../../composables/useStatusBar'
import { useSystemResources } from '../../composables/useSystemResources'

const props = defineProps<{
  costDelta: number | null
  todayCostLabel: string
  usageData: UsageData | null
}>()

const { collapsed, openSegment, toggleSegment, toggleCollapsed } = useStatusBar()
const resources = useSystemResources()
// Explicitly read .value so this works with both a real Ref<SystemInfo> (production)
// and a plain { value: SystemInfo } object returned by the test mock.
const systemInfo = computed<SystemInfo | null>(() => resources.info.value)

// worst is the budgeted window with the highest pct; null when no budgets exist.
const worst = computed<WindowData | null>(() => {
  if (!props.usageData)
    return null
  const budgeted = props.usageData.windows.filter(w => w.pct !== null)
  if (budgeted.length === 0)
    return null
  return budgeted.reduce((a, b) => (b.pct! > a.pct! ? b : a))
})

const usageLabel = computed<string>(() => {
  if (!worst.value)
    return ''
  return worst.value.key === '5h' ? 'SESSION' : 'WEEKLY'
})

const usagePctDisplay = computed<string>(() => {
  if (!worst.value || worst.value.pct === null)
    return ''
  return `${Math.round(worst.value.pct * 100)}%`
})

function formatM(tokens: number): string {
  return `${(tokens / 1_000_000).toFixed(1)}M`
}

const usageConsumptionText = computed<string>(() => {
  if (!props.usageData)
    return '—'
  const w5h = props.usageData.windows.find(w => w.key === '5h')
  const w7d = props.usageData.windows.find(w => w.key === '7d')
  if (!w5h || !w7d)
    return '—'
  return `5h ${formatM(w5h.tokens)} · 7d ${formatM(w7d.tokens)}`
})

// Resource-pressure thresholds in percent. One definition, so the usage bar and
// the numeric readout can never disagree about what counts as warning/danger.
// NOTE: barColor below still uses its own, older pair (85/60) — aligning it is a
// visual change to an existing element and is left as a deliberate decision.
const WARN_PCT = 75
const DANGER_PCT = 90

function barColor(pct: number): string {
  return pct > 85 ? 'bg-danger' : pct > 60 ? 'bg-warning' : 'bg-success'
}

function metricTextClass(pct: number): string {
  if (pct >= DANGER_PCT)
    return 'text-danger-text'
  if (pct >= WARN_PCT)
    return 'text-warning-text'
  return ''
}

function usageBarColor(): string {
  if (!worst.value || worst.value.pct === null)
    return 'bg-raised'
  const p = worst.value.pct * 100
  return p >= DANGER_PCT ? 'bg-danger' : p >= WARN_PCT ? 'bg-warning' : 'bg-success'
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
        <div>CPU <span :class="metricTextClass(systemInfo.cpu.usage)">{{ Math.round(systemInfo.cpu.usage) }}%</span> · {{ systemInfo.cpu.cores }} cores</div>
        <div>MEM <span data-testid="mem-pct" :class="metricTextClass(systemInfo.memory.usagePercent)">{{ Math.round(systemInfo.memory.usagePercent) }}%</span></div>
        <div>DISK <span :class="metricTextClass(systemInfo.disk.usagePercent)">{{ Math.round(systemInfo.disk.usagePercent) }}%</span></div>
        <div>LOAD {{ systemInfo.loadAvg.map(l => l.toFixed(2)).join(' ') }}</div>
      </div>
    </div>
    <div v-if="openSegment === 'cost'" data-testid="panel-cost" class="px-4 py-3 border-b border-line text-[12px] text-fg-mute font-mono flex flex-col gap-1">
      <span>Today's spend: {{ todayCostLabel }}</span>
      <span>Burn rate (last 5 min): {{ formatDelta(costDelta) }}</span>
    </div>
    <div v-if="openSegment === 'usage'" data-testid="panel-usage" class="px-4 py-3 border-b border-line text-[12px] text-fg-mute font-mono">
      <div v-if="usageData" class="flex flex-col gap-1">
        <div v-for="win in usageData.windows" :key="win.key">
          {{ win.key === '5h' ? 'Session (5h)' : 'Weekly (7d)' }}:
          {{ formatM(win.tokens) }} tokens · ${{ (win.costCents / 100).toFixed(2) }}
          <span v-if="win.pct !== null"> · {{ Math.round(win.pct * 100) }}%</span>
        </div>
        <template v-if="(usageData.accounts?.length ?? 0) > 1">
          <div class="mt-1 text-fg-faint">
            Accounts:
          </div>
          <div v-for="acc in usageData.accounts" :key="acc.label" class="pl-2">
            {{ acc.label }}: 5h {{ formatM(acc.w5h.tokens) }} · 7d {{ formatM(acc.w7d.tokens) }}
          </div>
        </template>
      </div>
      <div v-else>
        No data yet
      </div>
    </div>

    <div class="flex items-center gap-3 px-3 h-7 text-[11px] font-mono text-fg-mute">
      <button
        type="button"
        data-testid="seg-usage"
        class="flex items-center gap-1.5 hover:text-fg rounded px-1 focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:ring-offset-1 focus-visible:ring-offset-card"
        :aria-expanded="openSegment === 'usage'"
        aria-label="Toggle usage detail"
        @click="toggleSegment('usage')"
      >
        <template v-if="worst">
          <span class="text-fg-faint">{{ usageLabel }}</span>
          <span class="inline-block w-16 h-1.5 bg-raised rounded-full overflow-hidden align-middle">
            <span
              class="block h-full rounded-full"
              :class="usageBarColor()"
              :style="{ width: `${Math.round((worst.pct ?? 0) * 100)}%` }"
            />
          </span>
          <span class="text-fg">{{ usagePctDisplay }}</span>
        </template>
        <template v-else>
          <span class="text-fg-faint">USAGE</span>
          <span class="text-fg">{{ usageConsumptionText }}</span>
        </template>
      </button>
      <span class="w-px h-3.5 bg-line" aria-hidden="true" />
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
          <span :class="metricTextClass(systemInfo.cpu.usage)">{{ Math.round(systemInfo.cpu.usage) }}%</span></span>
        <span v-if="systemInfo">MEM <span data-testid="mem-pct-strip" :class="metricTextClass(systemInfo.memory.usagePercent)">{{ Math.round(systemInfo.memory.usagePercent) }}%</span></span>
        <span v-if="systemInfo">DISK <span :class="metricTextClass(systemInfo.disk.usagePercent)">{{ Math.round(systemInfo.disk.usagePercent) }}%</span></span>
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
