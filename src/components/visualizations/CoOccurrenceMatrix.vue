<script setup lang="ts">
import type { CoOccurrenceData } from '../../sdk.generated'
import { scaleDiverging } from 'd3-scale'
import { interpolateRdBu } from 'd3-scale-chromatic'
import { select } from 'd3-selection'
import { useTheme } from '../../composables/useTheme'
import { chartColors } from '../../utils/chartColors'
import { computed, onUnmounted, ref, watch } from 'vue'

const props = defineProps<{
  data: CoOccurrenceData | null
  loading: boolean
  error: string | null
}>()

const svgRef = ref<SVGSVGElement | null>(null)
const { theme } = useTheme()

const isEmpty = computed(() => !props.data || props.data.tools.length === 0)
const truncated = computed(() => props.data?.meta.truncated ?? false)
const sessionCount = computed(() => props.data?.meta.sessionCount ?? 0)

function render() {
  if (!svgRef.value || !props.data)
    return
  const svg = select(svgRef.value)
  svg.selectAll('*').remove()
  if (props.data.tools.length === 0)
    return

  const { tools, matrix, lift } = props.data
  const n = tools.length
  const cell = Math.max(16, Math.min(36, Math.floor(560 / Math.max(n, 1))))
  const margin = { top: 110, left: 110, right: 20, bottom: 36 }
  const width = margin.left + cell * n + margin.right
  const height = margin.top + cell * n + margin.bottom

  // Render at natural pixel size — no w-full stretching — so 1 viewBox unit = 1px.
  // max-width/height:auto lets it shrink on narrow screens without ballooning.
  svg
    .attr('viewBox', `0 0 ${width} ${height}`)
    .attr('width', width)
    .attr('height', height)

  // Build lift-based diverging color scale.
  // Blue (cool) = less than chance (lift < 1), neutral near 1, red (warm) > 1.
  // Use RdBu reversed so red = high lift (positive correlation).
  let maxLift = 2
  if (lift) {
    for (let i = 0; i < n; i++) {
      for (let j = 0; j < n; j++) {
        if (i !== j && lift[i][j] > maxLift)
          maxLift = lift[i][j]
      }
    }
  }
  // scaleDiverging: domain [low, mid, high] mapped to [0, 0.5, 1] of interpolator.
  // interpolateRdBu goes red → white → blue; we want blue<1, neutral=1, red>1 → reverse.
  const colorScale = scaleDiverging(interpolateRdBu)
    .domain([maxLift, 1, 0])

  const DIAGONAL_COLOR = chartColors().lineStrong

  const g = svg.append('g').attr('transform', `translate(${margin.left},${margin.top})`)

  for (let i = 0; i < n; i++) {
    for (let j = 0; j < n; j++) {
      const isDiag = i === j
      const liftVal = lift ? lift[i][j] : 0
      let fill: string
      if (isDiag) {
        fill = DIAGONAL_COLOR
      }
      else if (matrix[i][j] === 0) {
        fill = 'transparent'
      }
      else {
        fill = colorScale(liftVal)
      }
      const liftRounded = Math.round(liftVal * 100) / 100
      g.append('rect')
        .attr('x', j * cell)
        .attr('y', i * cell)
        .attr('width', cell - 1)
        .attr('height', cell - 1)
        .attr('fill', fill)
        .attr('stroke', chartColors().line)
        .attr('stroke-width', 0.5)
        .append('title')
        .text(isDiag
          ? `${tools[i]} (sessions using tool: ${matrix[i][i]})`
          : `${tools[i]} × ${tools[j]}  count=${matrix[i][j]}  lift=${liftRounded}`)
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

  // Legend caption at the bottom.
  const legendY = cell * n + 18
  const legendItems: { color: string, label: string }[] = [
    { color: DIAGONAL_COLOR, label: 'diagonal (usage count)' },
    { color: colorScale(0), label: 'lift < 1 (less than chance)' },
    { color: colorScale(1), label: 'lift ≈ 1 (independent)' },
    { color: colorScale(maxLift), label: `lift > 1 (correlated, max ${Math.round(maxLift * 100) / 100})` },
  ]
  const legendG = g.append('g').attr('transform', `translate(0,${legendY})`)
  let xOffset = 0
  for (const item of legendItems) {
    legendG.append('rect')
      .attr('x', xOffset)
      .attr('y', 0)
      .attr('width', 10)
      .attr('height', 10)
      .attr('fill', item.color)
      .attr('stroke', chartColors().line)
      .attr('stroke-width', 0.5)
    legendG.append('text')
      .attr('x', xOffset + 13)
      .attr('y', 9)
      .attr('font-size', '9px')
      .attr('fill', 'currentColor')
      .text(item.label)
    xOffset += 13 + item.label.length * 5.5 + 8
  }
}

// flush: 'post' so render runs AFTER the DOM patch — the <svg> lives inside
// a v-else that only mounts once data is non-empty, so a pre-flush watcher
// would see a null svgRef on first data arrival and bail, leaving it blank.
watch(() => props.data, render, { immediate: true, flush: 'post' })
watch(theme, render)

onUnmounted(() => {
  if (svgRef.value)
    select(svgRef.value).selectAll('*').remove()
})
</script>

<template>
  <div class="co-occurrence-matrix">
    <div v-if="loading" class="text-sm text-fg-mute p-4">
      Loading co-occurrence matrix…
    </div>
    <div v-else-if="error" class="text-sm text-danger-text p-4">
      {{ error }}
    </div>
    <div v-else-if="isEmpty" class="text-sm text-fg-mute p-4">
      No tool calls found in this window.
    </div>
    <div v-else>
      <p class="text-xs text-fg-mute mb-2">
        How often two tools were used together per session, colored by lift — red = used together more than chance, blue = less, neutral ≈ independent. Diagonal = sessions using that tool.
        Across {{ sessionCount }} session{{ sessionCount === 1 ? '' : 's' }}.
      </p>
      <p v-if="truncated" class="text-[11px] text-amber-500 mb-2">
        Showing the 50 most-active tools; rare tools omitted.
      </p>
      <svg ref="svgRef" style="max-width:100%; height:auto;" aria-label="Tool co-occurrence matrix" role="img" />
    </div>
  </div>
</template>
