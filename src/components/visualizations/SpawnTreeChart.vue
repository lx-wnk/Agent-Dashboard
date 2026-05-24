<script setup lang="ts">
import type { SpawnTreeData, SpawnTreeNode } from '../../sdk.generated'
import * as d3 from 'd3'
import { computed, onUnmounted, ref, watch } from 'vue'

const props = defineProps<{
  data: SpawnTreeData | null
  loading: boolean
  error: string | null
}>()

const emit = defineEmits<{ navigate: [sessionId: string] }>()

const svgRef = ref<SVGSVGElement | null>(null)

const isEmpty = computed(() => !props.data || props.data.nodes.length === 0)

interface TreeDatum {
  id: string
  label: string
  toolCount: number
  children?: TreeDatum[]
}

// buildHierarchy converts the flat node + link payload into a nested
// tree rooted at `props.data.roots[0]`. Multiple roots are stitched into
// a single synthetic root so d3.tree can lay everything out at once.
function buildHierarchy(): TreeDatum | null {
  if (!props.data || props.data.nodes.length === 0)
    return null
  const childrenByParent = new Map<string, string[]>()
  for (const l of props.data.links) {
    if (!childrenByParent.has(l.source))
      childrenByParent.set(l.source, [])
    childrenByParent.get(l.source)!.push(l.target)
  }
  const byID = new Map<string, SpawnTreeNode>(props.data.nodes.map(n => [n.id, n]))
  function walk(id: string): TreeDatum {
    const n = byID.get(id)!
    const kids = childrenByParent.get(id) ?? []
    return {
      id,
      label: n.label,
      toolCount: n.toolCount,
      children: kids.length > 0 ? kids.map(walk) : undefined,
    }
  }
  if (props.data.roots.length === 1)
    return walk(props.data.roots[0])
  return {
    id: '__synth_root__',
    label: 'all roots',
    toolCount: 0,
    children: props.data.roots.map(walk),
  }
}

function render() {
  if (!svgRef.value || !props.data)
    return
  const svg = d3.select(svgRef.value)
  svg.selectAll('*').remove()
  const tree = buildHierarchy()
  if (!tree)
    return

  const width = svgRef.value.clientWidth || 720
  const height = 480
  svg.attr('viewBox', `0 0 ${width} ${height}`)

  const root = d3.hierarchy<TreeDatum>(tree)
  const layout = d3.tree<TreeDatum>().size([width - 80, height - 80])
  layout(root)

  const g = svg.append('g').attr('transform', 'translate(40,40)')

  g.append('g')
    .attr('fill', 'none')
    .attr('stroke', '#94a3b8')
    .attr('stroke-width', 1.2)
    .selectAll('path')
    .data(root.links())
    .join('path')
    .attr('d', d3.linkVertical<d3.HierarchyPointLink<TreeDatum>, d3.HierarchyPointNode<TreeDatum>>()
      .x(d => d.x ?? 0)
      .y(d => d.y ?? 0) as any)

  const node = g.append('g')
    .selectAll<SVGGElement, d3.HierarchyPointNode<TreeDatum>>('g')
    .data(root.descendants())
    .join('g')
    .attr('cursor', 'pointer')
    .attr('transform', d => `translate(${d.x},${d.y})`)
    .on('click', (_e, d) => {
      if (d.data.id !== '__synth_root__')
        emit('navigate', d.data.id)
    })

  node.append('circle')
    .attr('r', d => Math.max(4, Math.min(18, 4 + Math.sqrt(d.data.toolCount))))
    .attr('fill', d => d.data.id === '__synth_root__' ? 'transparent' : '#6366f1')
    .attr('stroke', '#0f172a')

  node.append('text')
    .attr('dy', '-1em')
    .attr('text-anchor', 'middle')
    .attr('font-size', '10px')
    .attr('fill', 'currentColor')
    .text(d => d.data.label)
    .append('title')
    .text(d => `${d.data.id}\ntoolCount=${d.data.toolCount}`)
}

watch(() => props.data, render, { immediate: true })

onUnmounted(() => {
  if (svgRef.value)
    d3.select(svgRef.value).selectAll('*').remove()
})
</script>

<template>
  <div class="spawn-tree-chart">
    <div v-if="loading" class="text-sm text-fg-mute p-4">
      Loading spawn tree…
    </div>
    <div v-else-if="error" class="text-sm text-red-500 dark:text-red-400 p-4">
      {{ error }}
    </div>
    <div v-else-if="isEmpty" class="text-sm text-fg-mute p-4">
      No sessions found in this window.
    </div>
    <svg v-else ref="svgRef" class="w-full" style="min-height: 480px;" aria-label="Sub-agent spawn tree" role="img" />
  </div>
</template>
