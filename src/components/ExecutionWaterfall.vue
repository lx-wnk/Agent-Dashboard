<script setup lang="ts">
import type { OutputMessage } from '../types'
import * as d3 from 'd3'
import { onMounted, ref, useId, watch } from 'vue'

const props = defineProps<{ sessionId: string }>()

const svgRef = ref<SVGSVGElement | null>(null)
const loading = ref(true)
const error = ref<string | null>(null)

const titleId = useId()
const descId = useId()

interface ToolEvent {
  toolName: string
  start: Date
  end: Date
  durationMs: number
}

async function fetchAndRender() {
  if (!props.sessionId)
    return
  loading.value = true
  error.value = null
  try {
    const res = await fetch(`/api/sessions/${props.sessionId}/timeline`)
    if (!res.ok)
      throw new Error(await res.text())
    const { toolCalls } = await res.json() as { toolCalls: OutputMessage[] }
    renderGantt(toolCalls)
  }
  catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Failed to load timeline'
  }
  finally {
    loading.value = false
  }
}

function renderGantt(messages: OutputMessage[]) {
  const svg = d3.select(svgRef.value!)
  svg.selectAll(':not(title):not(desc)').remove()

  if (messages.length === 0) {
    svg.append('text').attr('x', 20).attr('y', 30).text('No tool calls recorded for this session.')
    return
  }

  const events: ToolEvent[] = messages.map((m, i) => {
    const start = new Date(m.timestamp!)
    const next = messages[i + 1]
    const end = next ? new Date(next.timestamp!) : new Date(start.getTime() + 500)
    return {
      toolName: m.toolName ?? 'unknown',
      start,
      end,
      durationMs: end.getTime() - start.getTime(),
    }
  })

  const margin = { top: 20, right: 20, bottom: 30, left: 140 }
  const width = (svgRef.value!.clientWidth || 800) - margin.left - margin.right
  const rowH = 24
  const height = events.length * rowH

  svg.attr('height', height + margin.top + margin.bottom)
  const g = svg.append('g').attr('transform', `translate(${margin.left},${margin.top})`)

  const xMin = d3.min(events, e => e.start)!
  const xMax = d3.max(events, e => e.end)!
  const x = d3.scaleTime().domain([xMin, xMax]).range([0, width])

  const toolNames = [...new Set(events.map(e => e.toolName))]
  const color = d3.scaleOrdinal(d3.schemeTableau10).domain(toolNames)

  g.selectAll<SVGRectElement, ToolEvent>('rect.bar')
    .data(events)
    .join('rect')
    .attr('class', 'bar')
    .attr('x', d => x(d.start))
    .attr('y', (_d, i) => i * rowH + 2)
    .attr('width', d => Math.max(2, x(d.end) - x(d.start)))
    .attr('height', rowH - 4)
    .attr('rx', 3)
    .attr('fill', d => color(d.toolName))
    .append('title')
    .text(d => `${d.toolName} — ${d.durationMs}ms`)

  g.selectAll<SVGTextElement, ToolEvent>('text.label')
    .data(events)
    .join('text')
    .attr('class', 'label')
    .attr('x', -8)
    .attr('y', (_d, i) => i * rowH + rowH / 2 + 4)
    .attr('text-anchor', 'end')
    .attr('font-size', '11px')
    .attr('fill', 'currentColor')
    .text(d => d.toolName)

  const xAxis = d3.axisBottom(x).ticks(5).tickFormat((d) => {
    const ms = (d as Date).getTime() - xMin.getTime()
    return `${(ms / 1000).toFixed(1)}s`
  })
  g.append('g')
    .attr('transform', `translate(0,${height})`)
    .call(xAxis)
    .selectAll('text')
    .attr('font-size', '10px')
}

onMounted(fetchAndRender)
watch(() => props.sessionId, fetchAndRender)
</script>

<template>
  <div class="execution-waterfall">
    <div v-if="loading" class="text-sm text-slate-500 p-4">
      Loading timeline...
    </div>
    <div v-else-if="error" class="text-sm text-red-500 p-4">
      {{ error }}
    </div>
    <div v-else class="overflow-x-auto">
      <svg
        ref="svgRef"
        class="w-full text-slate-800 dark:text-slate-200"
        style="min-height: 60px;"
        role="img"
        :aria-labelledby="titleId"
        :aria-describedby="descId"
      >
        <title :id="titleId">Execution Waterfall</title>
        <desc :id="descId">Gantt chart showing tool call durations over time for this session</desc>
      </svg>
    </div>
  </div>
</template>
