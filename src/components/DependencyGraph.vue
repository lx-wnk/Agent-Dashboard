<script setup lang="ts">
import type { TaskDependency } from '../types'
import * as d3 from 'd3'
import { onMounted, ref, watch } from 'vue'

const props = defineProps<{ taskId: string }>()
const emit = defineEmits<{ navigate: [taskId: string] }>()

const svgRef = ref<SVGSVGElement | null>(null)
const loading = ref(false)
const error = ref<string | null>(null)

interface GraphNode {
  id: string
  title: string
  stage: string
  x?: number
  y?: number
  fx?: number | null
  fy?: number | null
}
interface GraphLink {
  source: string | GraphNode
  target: string | GraphNode
}

const STAGE_COLORS: Record<string, string> = {
  concept: '#6366f1',
  backlog: '#8b5cf6',
  implementation: '#3b82f6',
  self_review: '#0ea5e9',
  finalization: '#10b981',
  done: '#22c55e',
  on_hold: '#f59e0b',
  cancelled: '#ef4444',
  failed: '#f97316',
  unknown: '#64748b',
}

async function fetchAndRender() {
  loading.value = true
  error.value = null
  try {
    const res = await fetch(`/api/tasks/${props.taskId}/dependencies`)
    if (!res.ok)
      throw new Error(await res.text())
    const data = await res.json() as { dependencies: TaskDependency[], dependents: TaskDependency[] }
    renderGraph(data.dependencies, data.dependents)
  }
  catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Failed to load graph'
  }
  finally {
    loading.value = false
  }
}

function renderGraph(deps: TaskDependency[], dependents: TaskDependency[]) {
  const svg = d3.select(svgRef.value!)
  svg.selectAll('*').remove()

  const nodeMap = new Map<string, GraphNode>()

  function addNode(id: string, title: string, stage: string) {
    if (!nodeMap.has(id))
      nodeMap.set(id, { id, title, stage })
  }

  const links: GraphLink[] = []

  for (const dep of deps) {
    addNode(dep.dependsOnId, dep.dependsOnTitle, dep.dependsOnStage)
    addNode(dep.taskId, dep.taskTitle, 'unknown')
    links.push({ source: dep.dependsOnId, target: dep.taskId })
  }
  for (const dep of dependents) {
    addNode(dep.dependsOnId, dep.dependsOnTitle, dep.dependsOnStage)
    addNode(dep.taskId, dep.taskTitle, dep.dependsOnStage)
    links.push({ source: dep.dependsOnId, target: dep.taskId })
  }

  // Ensure current task is always in the graph
  if (!nodeMap.has(props.taskId))
    nodeMap.set(props.taskId, { id: props.taskId, title: 'Current Task', stage: 'unknown' })

  const nodes = [...nodeMap.values()]

  // If only the current node, show a placeholder message instead of an empty graph
  if (nodes.length <= 1 && links.length === 0) {
    svg.append('text')
      .attr('x', '50%')
      .attr('y', '50%')
      .attr('text-anchor', 'middle')
      .attr('dominant-baseline', 'middle')
      .attr('fill', '#64748b')
      .attr('font-size', '13px')
      .text('No dependencies or dependents for this task.')
    return
  }

  const W = svgRef.value!.clientWidth || 600
  const H = 400

  svg.attr('height', H)

  // Arrow marker
  const defs = svg.append('defs')
  const marker = defs
    .append('marker')
    .attr('id', 'dep-arrow')
    .attr('viewBox', '0 -5 10 10')
    .attr('refX', 22)
    .attr('markerWidth', 6)
    .attr('markerHeight', 6)
    .attr('orient', 'auto')
  marker
    .append('path')
    .attr('d', 'M0,-5L10,0L0,5')
    .attr('fill', '#94a3b8')

  const simulation = d3.forceSimulation<GraphNode>(nodes)
    .force('link', d3.forceLink<GraphNode, GraphLink>(links).id(d => d.id).distance(110))
    .force('charge', d3.forceManyBody<GraphNode>().strength(-320))
    .force('center', d3.forceCenter(W / 2, H / 2))
    .force('collision', d3.forceCollide<GraphNode>(26))

  const linkSel = svg.append('g')
    .selectAll<SVGLineElement, GraphLink>('line')
    .data(links)
    .join('line')
    .attr('stroke', '#94a3b8')
    .attr('stroke-width', 1.5)
    .attr('marker-end', 'url(#dep-arrow)')

  const drag = d3.drag<SVGGElement, GraphNode>()
    .on('start', (event, d) => {
      if (!event.active)
        simulation.alphaTarget(0.3).restart()
      d.fx = d.x
      d.fy = d.y
    })
    .on('drag', (event, d) => {
      d.fx = event.x
      d.fy = event.y
    })
    .on('end', (event, d) => {
      if (!event.active)
        simulation.alphaTarget(0)
      d.fx = null
      d.fy = null
    })

  const nodeSel = svg.append('g')
    .selectAll<SVGGElement, GraphNode>('g.node')
    .data(nodes)
    .join('g')
    .attr('class', 'node')
    .attr('cursor', 'pointer')
    .attr('role', 'button')
    .attr('aria-label', d => `Navigate to task: ${d.title}`)
    .on('click', (_e, d) => emit('navigate', d.id))
    .call(drag)

  nodeSel.append('circle')
    .attr('r', 18)
    .attr('fill', d => STAGE_COLORS[d.stage] ?? '#64748b')
    .attr('stroke', d => d.id === props.taskId ? '#f59e0b' : 'transparent')
    .attr('stroke-width', 3)

  nodeSel.append('text')
    .attr('dy', '0.35em')
    .attr('text-anchor', 'middle')
    .attr('font-size', '10px')
    .attr('fill', 'white')
    .attr('pointer-events', 'none')
    .text(d => d.title.length > 12 ? `${d.title.slice(0, 11)}…` : d.title)
    .append('title')
    .text(d => d.title)

  simulation.on('tick', () => {
    linkSel
      .attr('x1', d => (d.source as GraphNode).x!)
      .attr('y1', d => (d.source as GraphNode).y!)
      .attr('x2', d => (d.target as GraphNode).x!)
      .attr('y2', d => (d.target as GraphNode).y!)
    nodeSel.attr('transform', d => `translate(${d.x!},${d.y!})`)
  })
}

onMounted(fetchAndRender)
watch(() => props.taskId, fetchAndRender)
</script>

<template>
  <div class="dependency-graph">
    <div v-if="loading" class="text-sm text-slate-500 dark:text-slate-400 p-4">
      Loading dependency graph…
    </div>
    <div v-else-if="error" class="text-sm text-red-500 dark:text-red-400 p-4">
      {{ error }}
    </div>
    <div v-else>
      <p class="text-xs text-slate-400 dark:text-slate-600 px-4 pt-2">
        Click a node to navigate to that task. Drag to reposition.
      </p>
      <svg ref="svgRef" class="w-full" style="min-height: 400px;" aria-label="Task dependency graph" role="img" />
    </div>
  </div>
</template>
