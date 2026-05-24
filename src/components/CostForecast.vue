<script setup lang="ts">
import * as d3 from 'd3'
import { ref, watch } from 'vue'
import { useCostForecast } from '../composables/useCostForecast'
import type { ForecastTrendPoint, ForecastPoint } from '../composables/useCostForecast'

const svgRef = ref<SVGSVGElement | null>(null)
const { trend, forecast, alerts, loading, error } = useCostForecast()

function renderChart(trendData: ForecastTrendPoint[], forecastData: ForecastPoint[]) {
  if (!svgRef.value)
    return

  const svg = d3.select(svgRef.value)
  svg.selectAll('*').remove()

  const margin = { top: 20, right: 20, bottom: 40, left: 60 }
  const W = (svgRef.value.clientWidth || 700) - margin.left - margin.right
  const H = 220

  svg.attr('height', H + margin.top + margin.bottom)
  const g = svg.append('g').attr('transform', `translate(${margin.left},${margin.top})`)

  const allT = [...trendData.map(p => p.t), ...forecastData.map(p => p.t)]
  const allY = [...trendData.map(p => p.y), ...forecastData.map(p => p.projectedCost)]
  const x = d3.scaleTime()
    .domain([new Date(Math.min(...allT)), new Date(Math.max(...allT))])
    .range([0, W])
  const y = d3.scaleLinear()
    .domain([0, Math.max(0.01, ...allY) * 1.1])
    .range([H, 0])

  const trendLine = d3.line<ForecastTrendPoint>().x(d => x(new Date(d.t))).y(d => y(d.y))
  g.append('path')
    .datum(trendData)
    .attr('fill', 'none')
    .attr('stroke', '#3b82f6')
    .attr('stroke-width', 2)
    .attr('d', trendLine)

  const forecastLine = d3.line<ForecastPoint>()
    .x(d => x(new Date(d.t)))
    .y(d => y(d.projectedCost))
  const bridge: ForecastPoint[] = trendData.length > 0 && forecastData.length > 0
    ? [{ t: trendData[trendData.length - 1].t, projectedCost: trendData[trendData.length - 1].y }, ...forecastData]
    : forecastData
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

// Re-render chart whenever trend/forecast data changes after fetch completes
watch([trend, forecast], ([t, f]) => {
  if (t.length > 0 || f.length > 0)
    renderChart(t, f)
})
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
