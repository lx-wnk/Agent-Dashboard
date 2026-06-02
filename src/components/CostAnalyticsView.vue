<script setup lang="ts">
import * as d3 from 'd3'
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useCostAnalytics } from '../composables/useCostAnalytics'
import { formatCost } from '../utils/format'

const { summary, isLoading, error, start, refresh } = useCostAnalytics()

// --- Historical data rescan ---
const importStatus = ref('')
const isImporting = ref(false)
let importEs: EventSource | null = null

onUnmounted(() => {
  importEs?.close()
})

async function startRescan() {
  if (isImporting.value)
    return
  isImporting.value = true
  importStatus.value = 'Starting…'
  const res = await fetch('/api/history/import', { method: 'POST' })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    const errMsg = (body as { error?: string }).error ?? res.statusText
    if (res.status === 409) {
      // Already running — still attach to the stream for live progress
      importStatus.value = `${errMsg} — watching progress…`
    }
    else {
      importStatus.value = `Error: ${errMsg}`
      isImporting.value = false
      return
    }
  }
  else {
    importStatus.value = 'Scanning…'
  }
  importEs = new EventSource('/api/history/import/status')
  importEs.onmessage = (ev) => {
    const p = JSON.parse(ev.data) as { total: number, processed: number, imported: number, errors: number, done: boolean }
    importStatus.value = `Scanning… ${p.processed}/${p.total}`
    if (p.done) {
      importStatus.value = `Imported ${p.imported} sessions`
      importEs?.close()
      importEs = null
      isImporting.value = false
      void refresh()
    }
  }
  importEs.onerror = () => {
    importStatus.value = 'Connection lost — scan may still be running'
    importEs?.close()
    importEs = null
    isImporting.value = false
  }
}

const stackedRef = ref<SVGSVGElement | null>(null)
const trendRef = ref<SVGSVGElement | null>(null)

const hasData = computed(() => summary.value.byDay.length > 0 || summary.value.byWeek.length > 0)
const updatedAtLabel = computed(() => {
  if (!summary.value.updatedAt)
    return ''
  return new Date(summary.value.updatedAt).toLocaleString()
})

/**
 * Reshape the flat byDay rows into one row per day with one column per model
 * — the input shape d3.stack() expects.
 */
function buildDayMatrix(): { rows: Record<string, number | string>[], models: string[] } {
  const byDay = summary.value.byDay
  const dayMap = new Map<string, Record<string, number | string>>()
  const modelSet = new Set<string>()
  for (const p of byDay) {
    modelSet.add(p.model)
    if (!dayMap.has(p.day))
      dayMap.set(p.day, { day: p.day })
    const row = dayMap.get(p.day)!
    row[p.model] = ((row[p.model] as number | undefined) ?? 0) + p.costUsd
  }
  const rows = Array.from(dayMap.values()).sort((a, b) => String(a.day).localeCompare(String(b.day)))
  const models = Array.from(modelSet).sort()
  // Ensure every row has every model key (zero-fill) so d3.stack() yields
  // contiguous segments with no NaN holes.
  for (const row of rows) {
    for (const m of models) {
      if (row[m] === undefined)
        row[m] = 0
    }
  }
  return { rows, models }
}

function renderStackedBar() {
  const svg = stackedRef.value
  if (!svg)
    return

  const sel = d3.select(svg)
  sel.selectAll('*').remove()

  const { rows, models } = buildDayMatrix()
  if (rows.length === 0)
    return

  const margin = { top: 12, right: 16, bottom: 50, left: 56 }
  const W = (svg.clientWidth || 700) - margin.left - margin.right
  const H = 260 - margin.top - margin.bottom

  sel.attr('height', H + margin.top + margin.bottom)
  const g = sel.append('g').attr('transform', `translate(${margin.left},${margin.top})`)

  const x = d3.scaleBand<string>()
    .domain(rows.map(r => String(r.day)))
    .range([0, W])
    .padding(0.15)

  const stack = d3.stack<Record<string, number | string>, string>()
    .keys(models)
    .value((d, key) => (d[key] as number) ?? 0)
  const series = stack(rows)

  const yMax = d3.max(series, layer => d3.max(layer, d => d[1])) ?? 0
  const y = d3.scaleLinear()
    .domain([0, yMax * 1.05 || 1])
    .nice()
    .range([H, 0])

  const color = d3.scaleOrdinal<string>()
    .domain(models)
    .range(d3.schemeTableau10 as readonly string[])

  // bars
  g.append('g')
    .selectAll('g')
    .data(series)
    .join('g')
    .attr('fill', d => color(d.key))
    .selectAll('rect')
    .data(d => d)
    .join('rect')
    .attr('x', d => x(String(d.data.day)) ?? 0)
    .attr('y', d => y(d[1]))
    .attr('height', d => Math.max(0, y(d[0]) - y(d[1])))
    .attr('width', x.bandwidth())
    .append('title')
    .text((d) => {
      const key = (d as unknown as { key?: string }).key
      const val = d[1] - d[0]
      return `${d.data.day}${key ? ` — ${key}` : ''}: ${formatCost(val)}`
    })

  // axes
  const xTickEvery = Math.max(1, Math.ceil(rows.length / 10))
  g.append('g')
    .attr('transform', `translate(0,${H})`)
    .call(d3.axisBottom(x).tickValues(rows.map((r, i) => i % xTickEvery === 0 ? String(r.day) : '').filter(Boolean)).tickFormat(d => String(d).slice(5)))
    .selectAll('text')
    .attr('font-size', '10px')
    .attr('transform', 'rotate(-32)')
    .attr('text-anchor', 'end')

  g.append('g')
    .call(d3.axisLeft(y).ticks(5).tickFormat(d => `$${Number(d).toFixed(2)}`))
    .selectAll('text')
    .attr('font-size', '10px')

  // legend
  const legend = sel.append('g')
    .attr('transform', `translate(${margin.left},${H + margin.top + 36})`)
  let xOffset = 0
  for (const m of models) {
    const group = legend.append('g').attr('transform', `translate(${xOffset},0)`)
    group.append('rect').attr('width', 10).attr('height', 10).attr('fill', color(m)).attr('y', -8)
    group.append('text').attr('x', 14).attr('y', 1).attr('font-size', '10px').attr('fill', 'currentColor').text(m)
    xOffset += 14 + Math.max(60, m.length * 6.2)
  }
}

function renderWeeklyTrend() {
  const svg = trendRef.value
  if (!svg)
    return

  const sel = d3.select(svg)
  sel.selectAll('*').remove()

  const data = summary.value.byWeek
  if (data.length === 0)
    return

  const margin = { top: 12, right: 16, bottom: 36, left: 56 }
  const W = (svg.clientWidth || 700) - margin.left - margin.right
  const H = 220 - margin.top - margin.bottom

  sel.attr('height', H + margin.top + margin.bottom)
  const g = sel.append('g').attr('transform', `translate(${margin.left},${margin.top})`)

  const x = d3.scalePoint<string>()
    .domain(data.map(d => d.week))
    .range([0, W])
    .padding(0.4)

  const yMax = d3.max(data, d => d.costUsd) ?? 0
  const y = d3.scaleLinear().domain([0, yMax * 1.1 || 1]).nice().range([H, 0])

  const line = d3.line<WeekPoint>()
    .x(d => x(d.week) ?? 0)
    .y(d => y(d.costUsd))
    .curve(d3.curveMonotoneX)

  g.append('path')
    .datum(data)
    .attr('fill', 'none')
    .attr('stroke', '#10b981')
    .attr('stroke-width', 2)
    .attr('d', line)

  g.append('g').selectAll('circle')
    .data(data)
    .join('circle')
    .attr('cx', d => x(d.week) ?? 0)
    .attr('cy', d => y(d.costUsd))
    .attr('r', 3)
    .attr('fill', '#10b981')
    .append('title')
    .text(d => `${d.week}: ${formatCost(d.costUsd)}`)

  const tickEvery = Math.max(1, Math.ceil(data.length / 8))
  g.append('g')
    .attr('transform', `translate(0,${H})`)
    .call(d3.axisBottom(x).tickValues(data.filter((_, i) => i % tickEvery === 0).map(d => d.week)))
    .selectAll('text')
    .attr('font-size', '10px')
    .attr('transform', 'rotate(-24)')
    .attr('text-anchor', 'end')

  g.append('g')
    .call(d3.axisLeft(y).ticks(5).tickFormat(d => `$${Number(d).toFixed(2)}`))
    .selectAll('text')
    .attr('font-size', '10px')
}

// Local alias to satisfy TS in the d3.line generic above.
interface WeekPoint { week: string, costUsd: number }

onMounted(() => {
  start()
})

watch(summary, () => {
  // d3 needs the DOM updated; render after Vue applies summary.value swap.
  queueMicrotask(() => {
    renderStackedBar()
    renderWeeklyTrend()
  })
}, { immediate: true })
</script>

<template>
  <div class="cost-analytics-view p-6 flex flex-col gap-6">
    <header class="flex items-baseline justify-between flex-wrap gap-2">
      <h2 class="text-lg font-semibold text-fg">
        Cost Analytics
      </h2>
      <div class="text-xs text-fg-mute flex items-center gap-3">
        <span v-if="summary.totalUsd > 0">Total: <strong class="text-fg">{{ formatCost(summary.totalUsd) }}</strong></span>
        <span v-if="updatedAtLabel">Updated {{ updatedAtLabel }}</span>
        <button
          v-if="hasData"
          type="button"
          :disabled="isImporting"
          class="text-xs px-2.5 py-1 rounded bg-raised border border-line text-fg-mute hover:text-fg hover:bg-raised/70 disabled:opacity-50 transition-colors"
          @click="startRescan"
        >
          {{ isImporting ? 'Scanning…' : 'Rescan now' }}
        </button>
      </div>
    </header>

    <p v-if="isLoading && !hasData" class="text-sm text-fg-mute">
      Loading cost summary…
    </p>
    <p v-else-if="error" class="text-sm text-red-600 dark:text-red-400">
      {{ error }}
    </p>
    <div v-else-if="!hasData" class="flex flex-col gap-2">
      <p class="text-sm text-fg-mute">
        No cost data yet. Costs are imported automatically from your Claude sessions — the first scan may take a moment. You can also trigger a scan now.
      </p>
      <div class="flex items-center gap-3">
        <button
          type="button"
          :disabled="isImporting"
          class="text-sm px-3 py-1.5 rounded bg-blue-600 text-white hover:bg-blue-700 disabled:opacity-50 transition-colors"
          @click="startRescan"
        >
          {{ isImporting ? 'Scanning…' : 'Rescan now' }}
        </button>
        <span v-if="importStatus" class="text-xs text-fg-mute">{{ importStatus }}</span>
      </div>
    </div>
    <p v-if="importStatus && hasData" class="text-xs text-fg-mute">
      {{ importStatus }}
    </p>

    <section v-if="summary.byModel.length > 0" class="bg-card border border-line rounded-md p-4">
      <h3 class="text-sm font-semibold mb-3 text-fg-soft">
        Spend by Model
      </h3>
      <ul class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2 text-xs">
        <li
          v-for="m in summary.byModel"
          :key="m.model"
          class="flex items-center justify-between gap-2 px-3 py-2 bg-raised rounded-md"
        >
          <span class="font-mono truncate text-fg" :title="m.model">{{ m.model }}</span>
          <span class="font-mono text-green-600 dark:text-green-400 whitespace-nowrap">
            {{ formatCost(m.costUsd) }}
          </span>
          <span class="text-fg-mute whitespace-nowrap">{{ m.sessions }} sess.</span>
        </li>
      </ul>
    </section>

    <section v-show="summary.byDay.length > 0" class="bg-card border border-line rounded-md p-4">
      <h3 class="text-sm font-semibold mb-2 text-fg-soft">
        Cost per Day (last 30 days, stacked by model)
      </h3>
      <svg ref="stackedRef" class="w-full text-fg" style="min-height: 220px;" />
    </section>

    <section v-show="summary.byWeek.length > 0" class="bg-card border border-line rounded-md p-4">
      <h3 class="text-sm font-semibold mb-2 text-fg-soft">
        Weekly Trend (last 12 weeks)
      </h3>
      <svg ref="trendRef" class="w-full text-fg" style="min-height: 200px;" />
    </section>
  </div>
</template>
