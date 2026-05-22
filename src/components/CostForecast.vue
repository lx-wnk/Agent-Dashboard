<script setup lang="ts">
import * as d3 from 'd3'
import { onMounted, ref } from 'vue'

interface TrendPoint { t: number, y: number }
interface ForecastPoint { t: number, projectedCost: number }
interface Alert { level: 'warn' | 'critical', message: string }

const svgRef = ref<SVGSVGElement | null>(null)
const alerts = ref<Alert[]>([])
const loading = ref(false)
const error = ref<string | null>(null)

async function fetchAndRender() {
  loading.value = true
  error.value = null
  try {
    const res = await fetch('/api/analytics/cost-forecast')
    if (!res.ok)
      throw new Error(await res.text())
    const data = await res.json() as { trend: TrendPoint[], forecast: ForecastPoint[], alerts: Alert[] }
    alerts.value = data.alerts
    renderChart(data.trend, data.forecast)
  }
  catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Failed to load forecast'
  }
  finally {
    loading.value = false
  }
}

function renderChart(trend: TrendPoint[], forecast: ForecastPoint[]) {
  if (!svgRef.value)
    return

  const svg = d3.select(svgRef.value)
  svg.selectAll('*').remove()

  const margin = { top: 20, right: 20, bottom: 40, left: 60 }
  const W = (svgRef.value.clientWidth || 700) - margin.left - margin.right
  const H = 220

  svg.attr('height', H + margin.top + margin.bottom)
  const g = svg.append('g').attr('transform', `translate(${margin.left},${margin.top})`)

  const allT = [...trend.map(p => p.t), ...forecast.map(p => p.t)]
  const allY = [...trend.map(p => p.y), ...forecast.map(p => p.projectedCost)]
  const x = d3.scaleTime()
    .domain([new Date(Math.min(...allT)), new Date(Math.max(...allT))])
    .range([0, W])
  const y = d3.scaleLinear()
    .domain([0, Math.max(0.01, ...allY) * 1.1])
    .range([H, 0])

  const trendLine = d3.line<TrendPoint>().x(d => x(new Date(d.t))).y(d => y(d.y))
  g.append('path')
    .datum(trend)
    .attr('fill', 'none')
    .attr('stroke', '#3b82f6')
    .attr('stroke-width', 2)
    .attr('d', trendLine)

  const forecastLine = d3.line<ForecastPoint>()
    .x(d => x(new Date(d.t)))
    .y(d => y(d.projectedCost))
  const bridge: ForecastPoint[] = trend.length > 0 && forecast.length > 0
    ? [{ t: trend[trend.length - 1].t, projectedCost: trend[trend.length - 1].y }, ...forecast]
    : forecast
  g.append('path')
    .datum(bridge)
    .attr('fill', 'none')
    .attr('stroke', '#f59e0b')
    .attr('stroke-width', 2)
    .attr('stroke-dasharray', '6 3')
    .attr('d', forecastLine)

  g.append('g')
    .attr('transform', `translate(0,${H})`)
    .call(d3.axisBottom(x).ticks(6))
    .selectAll('text')
    .attr('font-size', '10px')

  g.append('g')
    .call(d3.axisLeft(y).ticks(5).tickFormat(d => `$${Number(d).toFixed(2)}`))
    .selectAll('text')
    .attr('font-size', '10px')
}

onMounted(fetchAndRender)
</script>

<template>
  <div class="cost-forecast p-4">
    <h3 class="text-sm font-semibold mb-2 text-fg-soft">
      30-Day Cost Trend + 7-Day Forecast
    </h3>
    <div v-if="loading" class="text-sm text-slate-500">
      Loading forecast…
    </div>
    <div v-else-if="error" class="text-sm text-red-500">
      {{ error }}
    </div>
    <template v-else>
      <div
        v-for="alert in alerts"
        :key="alert.message"
        class="text-xs px-3 py-1.5 rounded border mb-2"
        :class="alert.level === 'critical'
          ? 'bg-red-100 text-red-700 border-red-300 dark:bg-red-900/30 dark:text-red-400 dark:border-red-800'
          : 'bg-yellow-100 text-yellow-700 border-yellow-300 dark:bg-yellow-900/30 dark:text-yellow-400 dark:border-yellow-800'"
      >
        {{ alert.message }}
      </div>
      <svg ref="svgRef" class="w-full text-fg" style="min-height: 120px;" />
      <p class="text-[10px] text-slate-400 mt-1">
        Blue = actual cumulative cost. Orange dashed = linear regression forecast.
      </p>
    </template>
  </div>
</template>
