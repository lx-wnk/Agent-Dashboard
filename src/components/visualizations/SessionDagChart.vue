<script setup lang="ts">
import type { DAGData } from '../../sdk.generated'
import * as d3 from 'd3'
import { computed, onUnmounted, ref, watch } from 'vue'

const props = defineProps<{
  data: DAGData | null
  loading: boolean
  error: string | null
}>()

const svgRef = ref<SVGSVGElement | null>(null)

const isEmpty = computed(() => !props.data || props.data.nodes.length === 0)

// Holds the active force simulation so render() can stop the previous one
// before starting a fresh tick loop, and so onUnmounted can halt any in-
// flight alpha decay when the component is torn down.
let activeSim: d3.Simulation<DAGNodeFlat, DAGLinkFlat> | null = null

interface DAGNodeFlat {
  id: string
  type: string
  label: string
  ts: string
  x?: number
  y?: number
  fx?: number | null
  fy?: number | null
}
interface DAGLinkFlat {
  source: string | DAGNodeFlat
  target: string | DAGNodeFlat
  kind: string
}

const NODE_COLORS: Record<string, string> = {
  tool: '#3b82f6',
  assistant: '#22c55e',
  user: '#f59e0b',
}

function render() {
  if (!svgRef.value || !props.data)
    return
  // Halt any prior simulation before drawing — d3 otherwise keeps the
  // alpha timer alive and accumulates tick handlers each time props.data
  // changes.
  activeSim?.stop()
  activeSim = null
  const svg = d3.select(svgRef.value)
  svg.selectAll('*').remove()
  if (props.data.nodes.length === 0)
    return

  const width = svgRef.value.clientWidth || 720
  const height = 480
  svg.attr('viewBox', `0 0 ${width} ${height}`)

  const nodes: DAGNodeFlat[] = props.data.nodes.map(n => ({ ...n }))
  const links: DAGLinkFlat[] = props.data.links.map(l => ({ ...l }))

  const sim = d3.forceSimulation<DAGNodeFlat>(nodes)
    .force('link', d3.forceLink<DAGNodeFlat, DAGLinkFlat>(links).id(d => d.id).distance(70))
    .force('charge', d3.forceManyBody<DAGNodeFlat>().strength(-220))
    .force('center', d3.forceCenter(width / 2, height / 2))
    .force('collision', d3.forceCollide<DAGNodeFlat>(18))
  activeSim = sim

  const linkSel = svg.append('g')
    .selectAll('line')
    .data(links)
    .join('line')
    .attr('stroke', d => d.kind === 'result' ? '#22c55e' : '#94a3b8')
    .attr('stroke-dasharray', d => d.kind === 'result' ? '4 2' : null)
    .attr('stroke-width', 1.2)

  const nodeSel = svg.append('g')
    .selectAll('circle')
    .data(nodes)
    .join('circle')
    .attr('r', 9)
    .attr('fill', d => NODE_COLORS[d.type] ?? '#64748b')
    .attr('stroke', '#0f172a')
    .attr('stroke-width', 1)
    .append('title')
    .text(d => `${d.label} (${d.type})\n${d.ts}`)

  sim.on('tick', () => {
    linkSel
      .attr('x1', d => (d.source as DAGNodeFlat).x ?? 0)
      .attr('y1', d => (d.source as DAGNodeFlat).y ?? 0)
      .attr('x2', d => (d.target as DAGNodeFlat).x ?? 0)
      .attr('y2', d => (d.target as DAGNodeFlat).y ?? 0)
    svg.selectAll<SVGCircleElement, DAGNodeFlat>('circle')
      .attr('cx', d => d.x ?? 0)
      .attr('cy', d => d.y ?? 0)
  })
  // Keep reference to avoid unused-var lint.
  void nodeSel
}

watch(() => props.data, render, { immediate: true })

onUnmounted(() => {
  activeSim?.stop()
  activeSim = null
  if (svgRef.value)
    d3.select(svgRef.value).selectAll('*').remove()
})
</script>

<template>
  <div class="session-dag-chart">
    <div v-if="loading" class="text-sm text-fg-mute p-4">
      Loading session DAG…
    </div>
    <div v-else-if="error" class="text-sm text-red-500 dark:text-red-400 p-4">
      {{ error }}
    </div>
    <div v-else-if="isEmpty" class="text-sm text-fg-mute p-4">
      Pick a session to view its DAG.
    </div>
    <svg v-else ref="svgRef" class="w-full" style="min-height: 480px;" aria-label="Session DAG" role="img" />
  </div>
</template>
