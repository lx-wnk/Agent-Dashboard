<script setup lang="ts">
import type { SankeyLink } from 'd3-sankey'
import type { SankeyData } from '../../sdk.generated'
import * as d3 from 'd3'
import { sankey as d3Sankey, sankeyLinkHorizontal } from 'd3-sankey'
import { computed, onUnmounted, ref, watch } from 'vue'
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
// Surfaces a d3-sankey layout failure (e.g. "circular link" if a cyclic
// graph ever reaches the layout) as a visible message instead of an
// uncaught promise rejection bubbling out of the watcher callback.
const renderError = ref<string | null>(null)

const isEmpty = computed(() => !props.data || props.data.nodes.length === 0)

function render() {
  renderError.value = null
  if (!svgRef.value || !props.data)
    return
  const svg = d3.select(svgRef.value)
  svg.selectAll('*').remove()

  if (props.data.nodes.length === 0)
    return

  try {
    drawSankey(svg)
  }
  catch (err) {
    svg.selectAll('*').remove()
    renderError.value = `Could not lay out sankey: ${errorMessage(err)}`
  }
}

function drawSankey(svg: d3.Selection<SVGSVGElement, unknown, null, undefined>) {
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

  const color = d3.scaleOrdinal<string>(d3.schemeTableau10).domain(props.data.nodes.map(n => n.name))

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
    .attr('stroke', '#94a3b8')
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

onUnmounted(() => {
  if (svgRef.value)
    d3.select(svgRef.value).selectAll('*').remove()
})
</script>

<template>
  <div class="sankey-chart">
    <div v-if="loading" class="text-sm text-fg-mute p-4">
      Loading sankey…
    </div>
    <div v-else-if="error" class="text-sm text-red-500 dark:text-red-400 p-4">
      {{ error }}
    </div>
    <div v-else-if="isEmpty" class="text-sm text-fg-mute p-4">
      No tool calls found in this window.
    </div>
    <!-- The <svg> stays mounted in this branch (not gated behind renderError)
         so svgRef survives a recoverable layout error — otherwise re-rendering
         after the data changes would hit the same null-ref mount race. -->
    <template v-else>
      <div v-if="renderError" class="text-sm text-red-500 dark:text-red-400 p-4">
        {{ renderError }}
      </div>
      <svg ref="svgRef" class="w-full" style="min-height: 480px;" aria-label="Tool-call sankey diagram" role="img" />
    </template>
  </div>
</template>
