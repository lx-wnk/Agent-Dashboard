# Phase 4 — Visualization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make agent behavior and pipeline costs visually legible: Gantt timeline of tool calls, task dependency DAG, hourly cost heatmap, 30-day cost forecast.

**Architecture:** VA-1 and VA-3 use D3.js (already present or add as dependency). CI-5 uses a CSS grid heatmap (no D3 needed). CI-6 uses a simple linear regression over `agent_cost_trend` data.

**Tech Stack:** D3.js v7, Vue 3, Vitest

---

## Task 1: Execution Waterfall Gantt Timeline (VA-1)

**Files:**
- `server/routes/agentRoutes.ts` — new `GET /api/sessions/:sessionId/timeline` endpoint
- `src/components/ExecutionWaterfall.vue` — new D3 Gantt component
- `src/components/AgentModal.vue` — add Waterfall tab

### Steps

- [ ] **1.1** Install D3 if not already present

  ```bash
  pnpm add d3
  pnpm add -D @types/d3
  ```

  Expected output: packages added to `package.json`.

- [ ] **1.2** Add `GET /api/sessions/:sessionId/timeline` to `server/routes/agentRoutes.ts`

  Insert after the existing `/agents/:sessionId/output` handler:

  ```typescript
  router.get('/sessions/:sessionId/timeline', async (req, res) => {
    const { sessionId } = req.params
    if (!UUID_RE.test(sessionId)) {
      res.status(400).json({ error: 'Invalid sessionId format' })
      return
    }
    try {
      const messages = await parseFullSession(sessionId, false)
      const toolCalls = messages.filter(m => m.role === 'tool_call' && m.timestamp)
      res.json({ toolCalls })
    }
    catch {
      res.status(500).json({ error: 'Failed to read session timeline' })
    }
  })
  ```

- [ ] **1.3** Create `src/components/ExecutionWaterfall.vue`

  ```vue
  <script setup lang="ts">
  import type { OutputMessage } from '../types'
  import * as d3 from 'd3'
  import { onMounted, ref, watch } from 'vue'

  const props = defineProps<{ sessionId: string }>()

  const svgRef = ref<SVGSVGElement | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)

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
    svg.selectAll('*').remove()

    if (messages.length === 0) {
      svg.append('text').attr('x', 20).attr('y', 30).text('No tool calls recorded for this session.')
      return
    }

    // Build events: each tool call spans from its timestamp to the next one
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

    svg
      .attr('height', height + margin.top + margin.bottom)
    const g = svg.append('g').attr('transform', `translate(${margin.left},${margin.top})`)

    const xMin = d3.min(events, e => e.start)!
    const xMax = d3.max(events, e => e.end)!
    const x = d3.scaleTime().domain([xMin, xMax]).range([0, width])

    const toolNames = [...new Set(events.map(e => e.toolName))]
    const color = d3.scaleOrdinal(d3.schemeTableau10).domain(toolNames)

    // Bars
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

    // Row labels
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

    // X-axis
    const xAxis = d3.axisBottom(x).ticks(5).tickFormat(d => {
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
      <div v-if="loading" class="text-sm text-slate-500 p-4">Loading timeline…</div>
      <div v-else-if="error" class="text-sm text-red-500 p-4">{{ error }}</div>
      <div v-else class="overflow-x-auto">
        <svg ref="svgRef" class="w-full text-slate-800 dark:text-slate-200" style="min-height: 60px;" />
      </div>
    </div>
  </template>
  ```

- [ ] **1.4** Add "Waterfall" tab to `src/components/AgentModal.vue`

  In the `<script setup>` block, import the component:

  ```typescript
  import ExecutionWaterfall from './ExecutionWaterfall.vue'
  ```

  Add a tab type. Locate the section rendering existing tabs (Tools, Tasks, Subagents) and add:

  ```html
  <!-- Tab buttons -->
  <button
    type="button"
    :class="activeTab === 'waterfall' ? 'border-b-2 border-blue-500 text-blue-600' : 'text-slate-500'"
    class="px-3 py-1.5 text-xs font-medium"
    @click="activeTab = 'waterfall'"
  >
    Waterfall
  </button>

  <!-- Tab panel -->
  <div v-if="activeTab === 'waterfall'">
    <ExecutionWaterfall :session-id="agent.sessionId" />
  </div>
  ```

  Add `'waterfall'` to the `Tab` union type in the modal script.

- [ ] **1.5** Write Vitest unit test `src/components/ExecutionWaterfall.test.ts`

  ```typescript
  import { describe, expect, it, vi } from 'vitest'
  import { mount } from '@vue/test-utils'
  import ExecutionWaterfall from './ExecutionWaterfall.vue'

  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: true,
    json: async () => ({
      toolCalls: [
        { role: 'tool_call', toolName: 'Read', timestamp: '2024-01-01T00:00:00.000Z', content: '' },
        { role: 'tool_call', toolName: 'Write', timestamp: '2024-01-01T00:00:01.000Z', content: '' },
      ],
    }),
  }))

  describe('ExecutionWaterfall', () => {
    it('fetches timeline on mount', async () => {
      const wrapper = mount(ExecutionWaterfall, { props: { sessionId: '00000000-0000-0000-0000-000000000001' } })
      await wrapper.vm.$nextTick()
      expect(fetch).toHaveBeenCalledWith('/api/sessions/00000000-0000-0000-0000-000000000001/timeline')
    })

    it('shows loading state initially', () => {
      const wrapper = mount(ExecutionWaterfall, { props: { sessionId: '00000000-0000-0000-0000-000000000001' } })
      expect(wrapper.text()).toContain('Loading')
    })
  })
  ```

- [ ] **1.6** Run tests and typecheck

  ```bash
  pnpm test --run src/components/ExecutionWaterfall.test.ts
  pnpm typecheck
  ```

  Expected: all tests pass, no type errors.

- [ ] **1.7** Commit

  ```
  feat(viz): add execution waterfall Gantt timeline (VA-1)
  ```

---

## Task 2: Task Dependency Graph (VA-3)

**Files:**
- `src/components/DependencyGraph.vue` — new D3 force-directed DAG
- `src/components/TaskModal.vue` — add Graph tab

### Steps

- [ ] **2.1** Verify `GET /api/tasks/:id/dependencies` exists in `taskRoutes.ts`. It already does (returns `TaskDependency[]`). If missing, add:

  ```typescript
  router.get('/tasks/:id/dependencies', async (req, res) => {
    const { id } = req.params
    if (!UUID_RE.test(id)) {
      res.status(400).json({ error: 'Invalid task id' })
      return
    }
    const deps = getDependenciesFor(id)
    const dependents = getDependentsOf(id)
    res.json({ dependencies: deps, dependents })
  })
  ```

- [ ] **2.2** Create `src/components/DependencyGraph.vue`

  ```vue
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
    source: string
    target: string
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

  const STAGE_COLORS: Record<string, string> = {
    concept: '#6366f1',
    backlog: '#8b5cf6',
    implementation: '#3b82f6',
    self_review: '#0ea5e9',
    finalization: '#10b981',
    done: '#22c55e',
    on_hold: '#f59e0b',
    cancelled: '#ef4444',
  }

  function renderGraph(deps: TaskDependency[], dependents: TaskDependency[]) {
    const svg = d3.select(svgRef.value!)
    svg.selectAll('*').remove()

    const nodeMap = new Map<string, GraphNode>()
    const addNode = (id: string, title: string, stage: string) => {
      if (!nodeMap.has(id))
        nodeMap.set(id, { id, title, stage })
    }

    const links: GraphLink[] = []

    for (const dep of deps) {
      addNode(dep.taskId, dep.taskTitle, 'unknown')
      addNode(dep.dependsOnId, dep.dependsOnTitle, dep.dependsOnStage)
      links.push({ source: dep.dependsOnId, target: dep.taskId })
    }
    for (const dep of dependents) {
      addNode(dep.taskId, dep.taskTitle, 'unknown')
      addNode(dep.dependsOnId, dep.dependsOnTitle, dep.dependsOnStage)
      links.push({ source: dep.dependsOnId, target: dep.taskId })
    }
    // Ensure root node is present
    addNode(props.taskId, 'Current Task', 'unknown')

    const nodes = [...nodeMap.values()]
    const W = svgRef.value!.clientWidth || 600
    const H = 400

    svg.attr('height', H)

    // Arrow marker
    svg.append('defs').append('marker')
      .attr('id', 'arrow')
      .attr('viewBox', '0 -5 10 10')
      .attr('refX', 20)
      .attr('markerWidth', 6)
      .attr('markerHeight', 6)
      .attr('orient', 'auto')
      .append('path')
      .attr('d', 'M0,-5L10,0L0,5')
      .attr('fill', '#94a3b8')

    const simulation = d3.forceSimulation<GraphNode>(nodes)
      .force('link', d3.forceLink<GraphNode, GraphLink>(links).id(d => d.id).distance(100))
      .force('charge', d3.forceManyBody().strength(-300))
      .force('center', d3.forceCenter(W / 2, H / 2))

    const linkSel = svg.append('g')
      .selectAll<SVGLineElement, GraphLink>('line')
      .data(links)
      .join('line')
      .attr('stroke', '#94a3b8')
      .attr('stroke-width', 1.5)
      .attr('marker-end', 'url(#arrow)')

    const nodeSel = svg.append('g')
      .selectAll<SVGGElement, GraphNode>('g.node')
      .data(nodes)
      .join('g')
      .attr('class', 'node')
      .attr('cursor', 'pointer')
      .on('click', (_e, d) => emit('navigate', d.id))
      .call(
        d3.drag<SVGGElement, GraphNode>()
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
          }),
      )

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
      .text(d => d.title.slice(0, 12))
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
      <div v-if="loading" class="text-sm text-slate-500 p-4">Loading dependency graph…</div>
      <div v-else-if="error" class="text-sm text-red-500 p-4">{{ error }}</div>
      <div v-else>
        <p class="text-xs text-slate-400 px-4 pt-2">Click a node to navigate to that task. Drag to reposition.</p>
        <svg ref="svgRef" class="w-full" style="min-height: 400px;" />
      </div>
    </div>
  </template>
  ```

- [ ] **2.3** Add "Graph" tab to `src/components/TaskModal.vue`

  In `<script setup>`:

  ```typescript
  import DependencyGraph from './DependencyGraph.vue'
  ```

  Add `'graph'` to the `Tab` union type. Add tab button and panel alongside existing tabs:

  ```html
  <button
    type="button"
    :class="activeTab === 'graph' ? 'border-b-2 border-blue-500 text-blue-600' : 'text-slate-500'"
    class="px-3 py-1.5 text-xs font-medium"
    @click="activeTab = 'graph'"
  >
    Dependencies
  </button>

  <div v-if="activeTab === 'graph' && task">
    <DependencyGraph :task-id="task.id" @navigate="id => emit('navigate', { id } as any)" />
  </div>
  ```

- [ ] **2.4** Run typecheck

  ```bash
  pnpm typecheck
  ```

  Expected: no errors.

- [ ] **2.5** Commit

  ```
  feat(viz): add D3 force-directed task dependency graph (VA-3)
  ```

---

## Task 3: Hourly Cost Heatmap (CI-5)

**Files:**
- `server/routes/agentRoutes.ts` (or a new `analyticsRoutes.ts`) — `GET /api/analytics/heatmap`
- `src/components/CostHeatmap.vue` — CSS grid heatmap

### Steps

- [ ] **3.1** Add `GET /api/analytics/heatmap` endpoint

  Add to `server/routes/agentRoutes.ts` (or register a new router in `server/index.ts`):

  ```typescript
  router.get('/analytics/heatmap', (_req, res) => {
    try {
      const db = getDb()
      const rows = db.prepare(`
        SELECT
          CAST(strftime('%w', datetime(t/1000, 'unixepoch')) AS INTEGER) AS dow,
          CAST(strftime('%H', datetime(t/1000, 'unixepoch')) AS INTEGER) AS hour,
          SUM(cost) AS total_cost
        FROM agent_cost_trend
        GROUP BY dow, hour
      `).all() as Array<{ dow: number, hour: number, total_cost: number }>

      // Build 7×24 grid, indexed [dow][hour]
      const grid: number[][] = Array.from({ length: 7 }, () => new Array(24).fill(0))
      for (const row of rows)
        grid[row.dow][row.hour] = row.total_cost

      res.json({ grid })
    }
    catch {
      res.status(500).json({ error: 'Failed to compute heatmap' })
    }
  })
  ```

  Note: `getDb` must be imported from `'../db/client.js'` in the route file.

- [ ] **3.2** Create `src/components/CostHeatmap.vue`

  ```vue
  <script setup lang="ts">
  import { onMounted, ref } from 'vue'

  const DOW_LABELS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
  const HOUR_LABELS = Array.from({ length: 24 }, (_, i) => `${i.toString().padStart(2, '0')}:00`)

  // grid[dow][hour] = cost in dollars
  const grid = ref<number[][]>(Array.from({ length: 7 }, () => new Array(24).fill(0)))
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchHeatmap() {
    loading.value = true
    error.value = null
    try {
      const res = await fetch('/api/analytics/heatmap')
      if (!res.ok)
        throw new Error(await res.text())
      const data = await res.json() as { grid: number[][] }
      grid.value = data.grid
    }
    catch (e: unknown) {
      error.value = e instanceof Error ? e.message : 'Failed to load heatmap'
    }
    finally {
      loading.value = false
    }
  }

  function maxCost(): number {
    return Math.max(1, ...grid.value.flatMap(row => row))
  }

  function cellOpacity(cost: number): number {
    return cost === 0 ? 0 : 0.1 + 0.9 * (cost / maxCost())
  }

  onMounted(fetchHeatmap)
  </script>

  <template>
    <div class="cost-heatmap p-4">
      <h3 class="text-sm font-semibold mb-3 text-slate-700 dark:text-slate-300">Cost by Day & Hour</h3>
      <div v-if="loading" class="text-sm text-slate-500">Loading heatmap…</div>
      <div v-else-if="error" class="text-sm text-red-500">{{ error }}</div>
      <div v-else class="overflow-x-auto">
        <!-- Hour axis label row -->
        <div class="flex">
          <div class="w-10 flex-shrink-0" />
          <div
            class="grid text-center"
            style="display: grid; grid-template-columns: repeat(24, minmax(28px, 1fr));"
          >
            <span
              v-for="h in HOUR_LABELS"
              :key="h"
              class="text-[9px] text-slate-400 truncate"
            >{{ h.slice(0, 2) }}</span>
          </div>
        </div>
        <!-- Data rows — one per day of week -->
        <div v-for="(dow, dowIdx) in DOW_LABELS" :key="dow" class="flex items-center mb-0.5">
          <div class="w-10 text-[11px] text-slate-500 flex-shrink-0 text-right pr-2">{{ dow }}</div>
          <div
            style="display: grid; grid-template-columns: repeat(24, minmax(28px, 1fr)); gap: 2px; flex: 1;"
          >
            <div
              v-for="(hour, hourIdx) in 24"
              :key="hourIdx"
              class="h-5 rounded-sm"
              :style="{
                backgroundColor: `rgba(59, 130, 246, ${cellOpacity(grid[dowIdx][hourIdx])})`,
              }"
              :title="`${dow} ${HOUR_LABELS[hourIdx]}: $${grid[dowIdx][hourIdx].toFixed(4)}`"
            />
          </div>
        </div>
        <p class="text-[10px] text-slate-400 mt-2">Cell intensity proportional to total cost. Hover for exact value.</p>
      </div>
    </div>
  </template>
  ```

- [ ] **3.3** Register `CostHeatmap` in the Settings or Analytics page (or add as a section below `CostTrend`). Minimal integration — add to `src/App.vue` or a dedicated settings panel, not required for the component to be functional.

- [ ] **3.4** Write Vitest test `src/components/CostHeatmap.test.ts`

  ```typescript
  import { describe, expect, it, vi } from 'vitest'
  import { mount } from '@vue/test-utils'
  import CostHeatmap from './CostHeatmap.vue'

  const mockGrid = Array.from({ length: 7 }, (_, d) =>
    Array.from({ length: 24 }, (__, h) => (d === 1 && h === 9 ? 0.5 : 0)),
  )

  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: true,
    json: async () => ({ grid: mockGrid }),
  }))

  describe('CostHeatmap', () => {
    it('renders 7 day-rows', async () => {
      const wrapper = mount(CostHeatmap)
      await wrapper.vm.$nextTick()
      await wrapper.vm.$nextTick() // wait for fetch resolution
      const labels = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
      for (const label of labels)
        expect(wrapper.text()).toContain(label)
    })
  })
  ```

- [ ] **3.5** Run

  ```bash
  pnpm test --run src/components/CostHeatmap.test.ts
  pnpm typecheck
  ```

- [ ] **3.6** Commit

  ```
  feat(viz): add hourly cost heatmap with CSS grid (CI-5)
  ```

---

## Task 4: 30-Day Cost Forecast + Budget Alerts (CI-6)

**Files:**
- `server/routes/agentRoutes.ts` (or analytics router) — `GET /api/analytics/cost-forecast`
- `src/components/CostForecast.vue` — D3 line chart with forecast overlay

### Steps

- [ ] **4.1** Add `GET /api/analytics/cost-forecast` endpoint

  ```typescript
  router.get('/analytics/cost-forecast', (_req, res) => {
    try {
      const db = getDb()

      // Last 30 days of hourly samples
      const cutoff = Date.now() - 30 * 24 * 60 * 60 * 1000
      const rows = db.prepare(
        'SELECT t, cost FROM agent_cost_trend WHERE t >= ? ORDER BY t ASC',
      ).all(cutoff) as Array<{ t: number, cost: number }>

      // Linear regression: y = slope * x + intercept
      // where x = t (ms since epoch), y = cumulative cost
      let cumCost = 0
      const points: Array<{ t: number, y: number }> = rows.map((r) => {
        cumCost += r.cost
        return { t: r.t, y: cumCost }
      })

      let slope = 0
      let intercept = 0
      if (points.length >= 2) {
        const n = points.length
        const sumX = points.reduce((s, p) => s + p.t, 0)
        const sumY = points.reduce((s, p) => s + p.y, 0)
        const sumXY = points.reduce((s, p) => s + p.t * p.y, 0)
        const sumXX = points.reduce((s, p) => s + p.t * p.t, 0)
        slope = (n * sumXY - sumX * sumY) / (n * sumXX - sumX * sumX)
        intercept = (sumY - slope * sumX) / n
      }

      // Forecast: next 7 days, one point per day
      const now = Date.now()
      const forecast = Array.from({ length: 7 }, (_, i) => {
        const t = now + (i + 1) * 24 * 60 * 60 * 1000
        return { t, projectedCost: Math.max(0, slope * t + intercept) }
      })

      // Budget alert thresholds from notification_config
      const { getConfig } = await import('../db/notificationConfigRepo.js')
      const warnCents = Number(getConfig('cost_forecast_warn_cents') ?? '1000')
      const critCents = Number(getConfig('cost_forecast_critical_cents') ?? '5000')

      const projectedTotal = forecast[forecast.length - 1]?.projectedCost ?? 0
      const projectedCents = projectedTotal * 100

      const alerts: Array<{ level: 'warn' | 'critical', message: string }> = []
      if (projectedCents >= critCents)
        alerts.push({ level: 'critical', message: `Projected 7-day cost $${(projectedCents / 100).toFixed(2)} exceeds critical threshold $${(critCents / 100).toFixed(2)}` })
      else if (projectedCents >= warnCents)
        alerts.push({ level: 'warn', message: `Projected 7-day cost $${(projectedCents / 100).toFixed(2)} exceeds warning threshold $${(warnCents / 100).toFixed(2)}` })

      res.json({ trend: points, forecast, alerts })
    }
    catch {
      res.status(500).json({ error: 'Failed to compute forecast' })
    }
  })
  ```

  Note: the `await import(...)` above should be a static import at the file top. Move `getConfig` import to the top of the router file alongside other `notificationConfigRepo` imports.

- [ ] **4.2** Create `src/components/CostForecast.vue`

  ```vue
  <script setup lang="ts">
  import * as d3 from 'd3'
  import { onMounted, ref } from 'vue'

  interface TrendPoint { t: number, y: number }
  interface ForecastPoint { t: number, projectedCost: number }
  interface Alert { level: 'warn' | 'critical', message: string }

  const svgRef = ref<SVGSVGElement | null>(null)
  const alerts = ref<Alert[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchAndRender() {
    loading.value = true
    error.value = null
    try {
      const res = await fetch('/api/analytics/cost-forecast')
      if (!res.ok)
        throw new Error(await res.text())
      const data = await res.json() as { trend: TrendPoint[], forecast: ForecastPoint[], alerts: Alert[] }
      alerts.value = data.alerts
      renderChart(data.trend, data.forecast)
    }
    catch (e: unknown) {
      error.value = e instanceof Error ? e.message : 'Failed to load forecast'
    }
    finally {
      loading.value = false
    }
  }

  function renderChart(trend: TrendPoint[], forecast: ForecastPoint[]) {
    const svg = d3.select(svgRef.value!)
    svg.selectAll('*').remove()

    const margin = { top: 20, right: 20, bottom: 40, left: 60 }
    const W = (svgRef.value!.clientWidth || 700) - margin.left - margin.right
    const H = 220

    svg.attr('height', H + margin.top + margin.bottom)
    const g = svg.append('g').attr('transform', `translate(${margin.left},${margin.top})`)

    const allT = [...trend.map(p => p.t), ...forecast.map(p => p.t)]
    const allY = [...trend.map(p => p.y), ...forecast.map(p => p.projectedCost)]
    const x = d3.scaleTime().domain([new Date(Math.min(...allT)), new Date(Math.max(...allT))]).range([0, W])
    const y = d3.scaleLinear().domain([0, Math.max(0.01, ...allY) * 1.1]).range([H, 0])

    // Actual trend line (solid blue)
    const trendLine = d3.line<TrendPoint>().x(d => x(new Date(d.t))).y(d => y(d.y))
    g.append('path').datum(trend).attr('fill', 'none').attr('stroke', '#3b82f6').attr('stroke-width', 2).attr('d', trendLine)

    // Forecast line (dashed orange)
    const forecastLine = d3.line<ForecastPoint>().x(d => x(new Date(d.t))).y(d => y(d.projectedCost))
    // Bridge: draw from last trend point to first forecast point
    const bridge = trend.length > 0 && forecast.length > 0
      ? [{ t: trend[trend.length - 1].t, projectedCost: trend[trend.length - 1].y }, ...forecast]
      : forecast
    g.append('path').datum(bridge).attr('fill', 'none').attr('stroke', '#f59e0b').attr('stroke-width', 2).attr('stroke-dasharray', '6 3').attr('d', forecastLine)

    // Axes
    g.append('g').attr('transform', `translate(0,${H})`).call(d3.axisBottom(x).ticks(6)).selectAll('text').attr('font-size', '10px')
    g.append('g').call(d3.axisLeft(y).ticks(5).tickFormat(d => `$${Number(d).toFixed(2)}`)).selectAll('text').attr('font-size', '10px')
  }

  onMounted(fetchAndRender)
  </script>

  <template>
    <div class="cost-forecast p-4">
      <h3 class="text-sm font-semibold mb-2 text-slate-700 dark:text-slate-300">30-Day Cost Trend + 7-Day Forecast</h3>
      <div v-if="loading" class="text-sm text-slate-500">Loading forecast…</div>
      <div v-else-if="error" class="text-sm text-red-500">{{ error }}</div>
      <template v-else>
        <div v-for="alert in alerts" :key="alert.message"
          :class="alert.level === 'critical' ? 'bg-red-100 text-red-700 border-red-300' : 'bg-yellow-100 text-yellow-700 border-yellow-300'"
          class="text-xs px-3 py-1.5 rounded border mb-2"
        >
          {{ alert.message }}
        </div>
        <svg ref="svgRef" class="w-full" style="min-height: 120px;" />
        <p class="text-[10px] text-slate-400 mt-1">
          Blue = actual cumulative cost. Orange dashed = linear regression forecast.
        </p>
      </template>
    </div>
  </template>
  ```

- [ ] **4.3** Write Vitest test `src/components/CostForecast.test.ts`

  ```typescript
  import { describe, expect, it, vi } from 'vitest'
  import { mount } from '@vue/test-utils'
  import CostForecast from './CostForecast.vue'

  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: true,
    json: async () => ({
      trend: [
        { t: Date.now() - 86400000, y: 0.5 },
        { t: Date.now(), y: 1.0 },
      ],
      forecast: [
        { t: Date.now() + 86400000, projectedCost: 1.5 },
      ],
      alerts: [{ level: 'warn', message: 'Projected cost exceeds warning threshold $10.00' }],
    }),
  }))

  describe('CostForecast', () => {
    it('renders alert messages', async () => {
      const wrapper = mount(CostForecast)
      await wrapper.vm.$nextTick()
      await wrapper.vm.$nextTick()
      expect(wrapper.text()).toContain('exceeds warning threshold')
    })
  })
  ```

- [ ] **4.4** Run

  ```bash
  pnpm test --run src/components/CostForecast.test.ts
  pnpm typecheck
  ```

- [ ] **4.5** Commit

  ```
  feat(viz): add 30-day cost forecast with linear regression and budget alerts (CI-6)
  ```
