<script setup lang="ts">
import type { CoOccurrenceData } from '../../sdk.generated'
import * as d3 from 'd3'
import { computed, onUnmounted, ref, watch } from 'vue'

const props = defineProps<{
  data: CoOccurrenceData | null
  loading: boolean
  error: string | null
}>()

const svgRef = ref<SVGSVGElement | null>(null)

const isEmpty = computed(() => !props.data || props.data.tools.length === 0)
const truncated = computed(() => props.data?.meta.truncated ?? false)

function render() {
  if (!svgRef.value || !props.data)
    return
  const svg = d3.select(svgRef.value)
  svg.selectAll('*').remove()
  if (props.data.tools.length === 0)
    return

  const { tools, matrix } = props.data
  const n = tools.length
  const cell = Math.max(16, Math.min(36, Math.floor(560 / Math.max(n, 1))))
  const margin = { top: 110, left: 110, right: 20, bottom: 20 }
  const width = margin.left + cell * n + margin.right
  const height = margin.top + cell * n + margin.bottom

  svg.attr('viewBox', `0 0 ${width} ${height}`)

  let max = 0
  for (const row of matrix) {
    for (const v of row) {
      if (v > max)
        max = v
    }
  }
  const color = d3.scaleSequential(d3.interpolateBlues).domain([0, max || 1])

  const g = svg.append('g').attr('transform', `translate(${margin.left},${margin.top})`)

  for (let i = 0; i < n; i++) {
    for (let j = 0; j < n; j++) {
      g.append('rect')
        .attr('x', j * cell)
        .attr('y', i * cell)
        .attr('width', cell - 1)
        .attr('height', cell - 1)
        .attr('fill', matrix[i][j] === 0 ? 'transparent' : color(matrix[i][j]))
        .attr('stroke', '#e2e8f0')
        .attr('stroke-width', 0.5)
        .append('title')
        .text(`${tools[i]} × ${tools[j]} = ${matrix[i][j]}`)
    }
  }

  // Column labels (rotated 45 degrees).
  g.append('g')
    .selectAll('text')
    .data(tools)
    .join('text')
    .attr('x', (_, i) => i * cell + cell / 2)
    .attr('y', -8)
    .attr('text-anchor', 'start')
    .attr('font-size', '11px')
    .attr('fill', 'currentColor')
    .attr('transform', (_, i) => `rotate(-45,${i * cell + cell / 2},-8)`)
    .text(d => d)

  // Row labels.
  g.append('g')
    .selectAll('text')
    .data(tools)
    .join('text')
    .attr('x', -8)
    .attr('y', (_, i) => i * cell + cell / 2 + 4)
    .attr('text-anchor', 'end')
    .attr('font-size', '11px')
    .attr('fill', 'currentColor')
    .text(d => d)
}

watch(() => props.data, render, { immediate: true })

onUnmounted(() => {
  if (svgRef.value)
    d3.select(svgRef.value).selectAll('*').remove()
})
</script>

<template>
  <div class="co-occurrence-matrix">
    <div v-if="loading" class="text-sm text-fg-mute p-4">
      Loading co-occurrence matrix…
    </div>
    <div v-else-if="error" class="text-sm text-red-500 dark:text-red-400 p-4">
      {{ error }}
    </div>
    <div v-else-if="isEmpty" class="text-sm text-fg-mute p-4">
      No tool calls found in this window.
    </div>
    <div v-else>
      <p v-if="truncated" class="text-[11px] text-amber-500 mb-2">
        Showing the 50 most-active tools; rare tools omitted.
      </p>
      <svg ref="svgRef" class="w-full" style="min-height: 480px;" aria-label="Tool co-occurrence matrix" role="img" />
    </div>
  </div>
</template>
