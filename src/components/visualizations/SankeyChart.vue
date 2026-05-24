<script setup lang="ts">
import type { SankeyData } from '../../sdk.generated'
import * as d3 from 'd3'
import { sankey as d3Sankey, sankeyLinkHorizontal } from 'd3-sankey'
import { computed, onUnmounted, ref, watch } from 'vue'

const props = defineProps<{
  data: SankeyData | null
  loading: boolean
  error: string | null
}>()

const svgRef = ref<SVGSVGElement | null>(null)

const isEmpty = computed(() => !props.data || props.data.nodes.length === 0)

function render() {
  if (!svgRef.value || !props.data)
    return
  const svg = d3.select(svgRef.value)
  svg.selectAll('*').remove()

  if (props.data.nodes.length === 0)
    return

  const width = svgRef.value.clientWidth || 720
  const height = 480
  svg.attr('viewBox', `0 0 ${width} ${height}`)

  const nodes = props.data.nodes.map(n => ({ ...n }))
  const links = props.data.links.map(l => ({ ...l }))

  const layout = d3Sankey<{ id: string, name: string }, { value: number }>()
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
    .text(d => `${d.name}\n${(d as any).value ?? 0}`)

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
    .text(d => `${(d.source as any).name} → ${(d.target as any).name}\n${d.value}`)

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

watch(() => props.data, render, { immediate: true })

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
    <svg v-else ref="svgRef" class="w-full" style="min-height: 480px;" aria-label="Tool-call sankey diagram" role="img" />
  </div>
</template>
