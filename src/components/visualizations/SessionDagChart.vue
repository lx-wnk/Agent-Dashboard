<script setup lang="ts">
import type { DAGData } from '../../sdk.generated'
import { scaleLinear } from 'd3-scale'
import { select } from 'd3-selection'
import { useTheme } from '../../composables/useTheme'
import { chartColors } from '../../utils/chartColors'
import { computed, onUnmounted, ref, watch } from 'vue'

const props = defineProps<{
  data: DAGData | null
  loading: boolean
  error: string | null
}>()

const svgRef = ref<SVGSVGElement | null>(null)
const { theme } = useTheme()

const isEmpty = computed(() => !props.data || props.data.nodes.length === 0)

interface DAGNodeFlat {
  id: string
  type: string
  label: string
  ts: string
}
interface DAGLinkFlat {
  source: string
  target: string
  kind: string
}

function nodeColor(type: string): string {
  const c = chartColors()
  const map: Record<string, string> = { tool: c.info, assistant: c.success, user: c.warning }
  return map[type] ?? c.fgMute
}

// Lane Y centres for each node type.
const LANE_Y: Record<string, number> = {
  user: 70,
  assistant: 160,
  tool: 260,
}
const LANE_FALLBACK_Y = 160

const SVG_HEIGHT = 340
const NODE_RADIUS = 7
// Horizontal padding left (for lane labels) and right.
const PAD_LEFT = 80
const PAD_RIGHT = 24
// Minimum px between nodes on X.
const NODE_SPACING = 26

function laneY(type: string): number {
  return LANE_Y[type] ?? LANE_FALLBACK_Y
}

function render() {
  if (!svgRef.value || !props.data) {
    return
  }

  const svg = select(svgRef.value)
  svg.selectAll('*').remove()

  const nodes: DAGNodeFlat[] = props.data.nodes.map(n => ({ ...n }))
  const links: DAGLinkFlat[] = props.data.links.map(l => ({ ...l }))

  if (nodes.length === 0) {
    return
  }

  const containerWidth = svgRef.value.parentElement?.clientWidth ?? 720
  const svgWidth = Math.max(containerWidth, nodes.length * NODE_SPACING + PAD_LEFT + PAD_RIGHT)

  svg
    .attr('width', svgWidth)
    .attr('height', SVG_HEIGHT)
    .attr('viewBox', `0 0 ${svgWidth} ${SVG_HEIGHT}`)

  // Build position map: nodeId → {x, y}
  const drawWidth = svgWidth - PAD_LEFT - PAD_RIGHT
  const xScale = scaleLinear()
    .domain([0, Math.max(nodes.length - 1, 1)])
    .range([PAD_LEFT, PAD_LEFT + drawWidth])

  const posMap = new Map<string, { x: number, y: number }>()
  nodes.forEach((node, i) => {
    posMap.set(node.id, { x: xScale(i), y: laneY(node.type) })
  })

  // ── Lane guide lines + labels ───────────────────────────────────────────
  const laneEntries: Array<{ key: string, label: string }> = [
    { key: 'user', label: 'user' },
    { key: 'assistant', label: 'assistant' },
    { key: 'tool', label: 'tool' },
  ]

  const lanesG = svg.append('g').attr('class', 'lanes')
  for (const lane of laneEntries) {
    const y = LANE_Y[lane.key]
    lanesG.append('line')
      .attr('x1', PAD_LEFT - 8)
      .attr('y1', y)
      .attr('x2', svgWidth - PAD_RIGHT)
      .attr('y2', y)
      .attr('stroke', chartColors().line)
      .attr('stroke-width', 1)
      .attr('stroke-dasharray', '2 4')
      .attr('opacity', 0.5)

    lanesG.append('text')
      .attr('x', 4)
      .attr('y', y + 4)
      .attr('fill', chartColors().fgMute)
      .attr('font-size', 11)
      .attr('font-family', 'sans-serif')
      .text(lane.label)
  }

  // ── Edges ───────────────────────────────────────────────────────────────
  const edgesG = svg.append('g').attr('class', 'edges')
  for (const link of links) {
    const src = posMap.get(link.source)
    const tgt = posMap.get(link.target)
    if (!src || !tgt) {
      continue
    }
    edgesG.append('line')
      .attr('x1', src.x)
      .attr('y1', src.y)
      .attr('x2', tgt.x)
      .attr('y2', tgt.y)
      .attr('stroke', link.kind === 'result' ? chartColors().success : chartColors().fgFaint)
      .attr('stroke-dasharray', link.kind === 'result' ? '4 2' : null)
      .attr('stroke-width', 1.2)
      .attr('opacity', 0.7)
  }

  // ── Nodes ───────────────────────────────────────────────────────────────
  const nodesG = svg.append('g').attr('class', 'nodes')
  for (const node of nodes) {
    const pos = posMap.get(node.id)
    if (!pos) {
      continue
    }
    const circle = nodesG.append('circle')
      .attr('cx', pos.x)
      .attr('cy', pos.y)
      .attr('r', NODE_RADIUS)
      .attr('fill', nodeColor(node.type))
      .attr('stroke', chartColors().line)
      .attr('stroke-width', 1)

    circle.append('title')
      .text(`${node.label} (${node.type})\n${node.ts}`)
  }

  // ── Tool node labels (rotated, below tool lane) ─────────────────────────
  const toolLabelY = LANE_Y.tool + NODE_RADIUS + 6
  const labelsG = svg.append('g').attr('class', 'tool-labels')
  for (const node of nodes) {
    if (node.type !== 'tool') {
      continue
    }
    const pos = posMap.get(node.id)
    if (!pos) {
      continue
    }
    const truncated = node.label.length > 12 ? `${node.label.slice(0, 12)}…` : node.label
    labelsG.append('text')
      .attr('transform', `translate(${pos.x},${toolLabelY}) rotate(-40)`)
      .attr('fill', chartColors().fgFaint)
      .attr('font-size', 9)
      .attr('font-family', 'sans-serif')
      .attr('text-anchor', 'end')
      .text(truncated)
  }

  // ── Legend ───────────────────────────────────────────────────────────────
  const c = chartColors()
  const legendItems: Array<{ color: string, label: string, dashed?: boolean, isLine?: boolean }> = [
    { color: c.warning, label: 'user' },
    { color: c.success, label: 'assistant' },
    { color: c.info, label: 'tool' },
    { color: c.success, label: 'result edge', dashed: true, isLine: true },
  ]

  const legendG = svg.append('g').attr('class', 'legend')
  const legendStartX = svgWidth - PAD_RIGHT - 130
  const legendStartY = 10
  const rowH = 16

  legendItems.forEach((item, i) => {
    const gy = legendStartY + i * rowH
    if (item.isLine) {
      legendG.append('line')
        .attr('x1', legendStartX)
        .attr('y1', gy + 5)
        .attr('x2', legendStartX + 14)
        .attr('y2', gy + 5)
        .attr('stroke', item.color)
        .attr('stroke-width', 1.5)
        .attr('stroke-dasharray', item.dashed ? '4 2' : null)
    }
    else {
      legendG.append('circle')
        .attr('cx', legendStartX + 7)
        .attr('cy', gy + 5)
        .attr('r', 5)
        .attr('fill', item.color)
    }
    legendG.append('text')
      .attr('x', legendStartX + 20)
      .attr('y', gy + 9)
      .attr('fill', chartColors().fgMute)
      .attr('font-size', 10)
      .attr('font-family', 'sans-serif')
      .text(item.label)
  })
}

// flush: 'post' so render runs AFTER the DOM patch — the <svg v-else> only
// mounts once data is non-empty, so a pre-flush watcher would see a null
// svgRef on first data arrival and bail, leaving the chart blank.
watch(() => props.data, render, { immediate: true, flush: 'post' })
watch(theme, render)

onUnmounted(() => {
  if (svgRef.value) {
    select(svgRef.value).selectAll('*').remove()
  }
})
</script>

<template>
  <div class="session-dag-chart">
    <div v-if="loading" class="text-sm text-fg-mute p-4">
      Loading session DAG…
    </div>
    <div v-else-if="error" class="text-sm text-danger-text p-4">
      {{ error }}
    </div>
    <div v-else-if="isEmpty" class="text-sm text-fg-mute p-4">
      Pick a session to view its DAG.
    </div>
    <div v-else style="overflow-x: auto;">
      <svg ref="svgRef" :style="{ minHeight: '340px' }" aria-label="Session DAG" role="img" />
    </div>
  </div>
</template>
