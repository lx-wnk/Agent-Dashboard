<script setup lang="ts">
import type { SankeyLink } from 'd3-sankey'
import type { Selection } from 'd3-selection'
import type { SankeyData } from '../../sdk.generated'
import { sankey as d3Sankey, sankeyLinkHorizontal } from 'd3-sankey'
import { scaleOrdinal } from 'd3-scale'
import { select } from 'd3-selection'
import { computed, onUnmounted, ref, watch } from 'vue'
import { useTheme } from '../../composables/useTheme'
import { toast } from '../../composables/useToast'
import { chartColors, chartPalette } from '../../utils/chartColors'
import { errorMessage } from '../../utils/errorMessage'

// User-defined node/link properties carried through the layout, on top of the
// d3-sankey-computed geometry (x0/y0/width/…).
interface NodeExtra { id: string, name: string }
interface LinkExtra { value: number }
type SLink = SankeyLink<NodeExtra, LinkExtra>

const props = defineProps<{
  data: SankeyData | null
  loading: boolean
  error: string | null
}>()

// A laid-out link's source/target is the resolved node object; narrow the
// `number | string | node` union d3-sankey types it as.
function nodeName(endpoint: SLink['source']): string {
  return typeof endpoint === 'object' ? endpoint.name : String(endpoint)
}

const svgRef = ref<SVGSVGElement | null>(null)
const { theme } = useTheme()

// Surface data-fetch errors (from the parent view) as toasts.
watch(() => props.error, (msg) => {
  if (msg)
    toast.error(msg)
}, { immediate: true })

const isEmpty = computed(() => !props.data || props.data.nodes.length === 0)

function render() {
  if (!svgRef.value || !props.data)
    return
  const svg = select(svgRef.value)
  svg.selectAll('*').remove()

  if (props.data.nodes.length === 0)
    return

  try {
    drawSankey(svg)
  }
  catch (err) {
    // Surfaces a d3-sankey layout failure (e.g. a cyclic graph reaching the
    // layout) as a toast instead of an uncaught rejection from the watcher.
    svg.selectAll('*').remove()
    toast.error(`Could not lay out sankey: ${errorMessage(err)}`)
  }
}

function drawSankey(svg: Selection<SVGSVGElement, unknown, null, undefined>) {
  if (!svgRef.value || !props.data)
    return

  const width = svgRef.value.clientWidth || 720
  const height = 480
  svg.attr('viewBox', `0 0 ${width} ${height}`)
  // SVG <title> provides accessible fallback for browsers that surface it to AT
  svg.append('title').text('Tool-call flow — Sankey diagram')

  const nodes = props.data.nodes.map(n => ({ ...n }))
  const links = props.data.links.map(l => ({ ...l }))

  const layout = d3Sankey<NodeExtra, LinkExtra>()
    .nodeId(d => d.id)
    .nodeWidth(15)
    .nodePadding(12)
    .extent([[1, 1], [width - 1, height - 6]])

  const { nodes: laidOutNodes, links: laidOutLinks } = layout({ nodes, links })

  const colors = chartColors()
  const color = scaleOrdinal<string>(chartPalette()).domain(props.data.nodes.map(n => n.name))

  svg.append('g')
    .selectAll('rect')
    .data(laidOutNodes)
    .join('rect')
    .attr('x', d => d.x0 ?? 0)
    .attr('y', d => d.y0 ?? 0)
    .attr('height', d => (d.y1 ?? 0) - (d.y0 ?? 0))
    .attr('width', d => (d.x1 ?? 0) - (d.x0 ?? 0))
    .attr('fill', d => color(d.name))
    .append('title')
    .text(d => `${d.name}\n${d.value ?? 0}`)

  svg.append('g')
    .attr('fill', 'none')
    .attr('stroke-opacity', 0.4)
    .selectAll('path')
    .data(laidOutLinks)
    .join('path')
    .attr('d', sankeyLinkHorizontal())
    .attr('stroke', colors.line)
    .attr('stroke-width', d => Math.max(1, d.width ?? 1))
    .append('title')
    .text(d => `${nodeName(d.source)} → ${nodeName(d.target)}\n${d.value}`)

  svg.append('g')
    .style('font-size', '11px')
    .style('fill', 'currentColor')
    .selectAll('text')
    .data(laidOutNodes)
    .join('text')
    .attr('x', d => ((d.x0 ?? 0) < width / 2 ? (d.x1 ?? 0) + 6 : (d.x0 ?? 0) - 6))
    .attr('y', d => ((d.y0 ?? 0) + (d.y1 ?? 0)) / 2)
    .attr('dy', '0.35em')
    .attr('text-anchor', d => ((d.x0 ?? 0) < width / 2 ? 'start' : 'end'))
    .text(d => d.name)
}

// flush: 'post' so render runs AFTER the DOM patch — the <svg v-else> only
// mounts once data is non-empty, so a pre-flush watcher would see a null
// svgRef on first data arrival and bail, leaving the chart blank.
watch(() => props.data, render, { immediate: true, flush: 'post' })
watch(theme, render)

onUnmounted(() => {
  if (svgRef.value)
    select(svgRef.value).selectAll('*').remove()
})
</script>

<template>
  <div class="sankey-chart">
    <div v-if="loading" class="text-sm text-fg-mute p-4">
      Loading sankey…
    </div>
    <div v-else-if="isEmpty" class="text-sm text-fg-mute p-4">
      No tool calls found in this window.
    </div>
    <template v-else>
      <svg ref="svgRef" class="w-full" style="min-height: 480px;" aria-label="Tool-call sankey diagram" role="img" />
    </template>
  </div>
</template>
