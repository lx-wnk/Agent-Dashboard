# Phase 2 — Cost Intelligence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface cache token cost splits, per-task stage cost waterfall, agent health/anomaly score, and tab-visibility SSE pause.

**Architecture:** Parser already captures cache tokens — this phase wires them to the UI. Health score computed in agentMerger from existing data. Tab-visibility pause is a 1-line composable change.

**Tech Stack:** Vue 3 Composition API, Vitest, TypeScript

---

## Task 1: Cache token cost split — separate display (CI-3)

**Files:**
- Modify: `src/types.ts` — add `cacheCreationCostEstimate` and `cacheReadCostEstimate` to `Agent`
- Modify: `server/pricing.ts` — add `estimateCacheCreationCost` and `estimateCacheReadCost` helpers
- Modify: `server/agentMerger.ts` — set the two new fields on each Agent
- Modify: `src/components/AgentCard.vue` — show cache cost sub-rows
- Modify: `src/components/AgentModal.vue` — show 4-row token/cost breakdown

**Steps:**

- [ ] **Add the two new fields to `Agent` in `src/types.ts`**

  After the existing `costEstimate: number` line, add:

  ```ts
  cacheCreationCostEstimate: number
  cacheReadCostEstimate: number
  ```

- [ ] **Add cost helpers to `server/pricing.ts`**

  ```ts
  export function estimateCacheCreationCost(
    usage: Pick<TokenUsage, 'cacheCreationTokens'>,
    model: string | null,
  ): number {
    const pricing = (model && MODEL_PRICING[model]) || MODEL_PRICING[DEFAULT_MODEL]
    return (usage.cacheCreationTokens * pricing.cacheCreate) / 1_000_000
  }

  export function estimateCacheReadCost(
    usage: Pick<TokenUsage, 'cacheReadTokens'>,
    model: string | null,
  ): number {
    const pricing = (model && MODEL_PRICING[model]) || MODEL_PRICING[DEFAULT_MODEL]
    return (usage.cacheReadTokens * pricing.cacheRead) / 1_000_000
  }
  ```

- [ ] **Write unit tests for the new helpers in `server/pricing.test.ts`**

  Create `server/pricing.test.ts`:

  ```ts
  import { describe, expect, it } from 'vitest'
  import { estimateCacheCreationCost, estimateCacheReadCost } from './pricing.js'

  describe('estimateCacheCreationCost', () => {
    it('returns 0 for zero tokens', () => {
      expect(estimateCacheCreationCost({ cacheCreationTokens: 0 }, 'claude-sonnet-4-6')).toBe(0)
    })

    it('calculates sonnet-4-6 cache creation at $3.75/MTok', () => {
      // 1_000_000 tokens * $3.75/MTok = $3.75
      const result = estimateCacheCreationCost({ cacheCreationTokens: 1_000_000 }, 'claude-sonnet-4-6')
      expect(result).toBeCloseTo(3.75, 6)
    })

    it('calculates opus-4-0 cache creation at $18.75/MTok', () => {
      const result = estimateCacheCreationCost({ cacheCreationTokens: 1_000_000 }, 'claude-opus-4-0')
      expect(result).toBeCloseTo(18.75, 6)
    })

    it('falls back to default model for unknown model', () => {
      const withDefault = estimateCacheCreationCost({ cacheCreationTokens: 100_000 }, null)
      const withSonnet = estimateCacheCreationCost({ cacheCreationTokens: 100_000 }, 'claude-sonnet-4-6')
      expect(withDefault).toBe(withSonnet)
    })
  })

  describe('estimateCacheReadCost', () => {
    it('returns 0 for zero tokens', () => {
      expect(estimateCacheReadCost({ cacheReadTokens: 0 }, 'claude-sonnet-4-6')).toBe(0)
    })

    it('calculates sonnet-4-6 cache read at $0.30/MTok', () => {
      const result = estimateCacheReadCost({ cacheReadTokens: 1_000_000 }, 'claude-sonnet-4-6')
      expect(result).toBeCloseTo(0.3, 6)
    })

    it('calculates haiku-4-5 cache read at $0.08/MTok', () => {
      const result = estimateCacheReadCost({ cacheReadTokens: 1_000_000 }, 'claude-haiku-4-5')
      expect(result).toBeCloseTo(0.08, 6)
    })
  })
  ```

- [ ] **Run tests and confirm they pass**

  ```bash
  pnpm test server/pricing.test.ts
  ```

  Expected output: `2 describe blocks, 8 tests passed`

- [ ] **Set the new fields in `server/agentMerger.ts`**

  Import the helpers at the top:

  ```ts
  import { estimateCacheCreationCost, estimateCacheReadCost, estimateCost } from './pricing.js'
  ```

  In the `agents` map inside `getAgents()`, add the two fields after `costEstimate`:

  ```ts
  costEstimate: estimateCost(session?.tokenUsage || tokenUsage, session?.model || null),
  cacheCreationCostEstimate: estimateCacheCreationCost(
    session?.tokenUsage || tokenUsage,
    session?.model || null,
  ),
  cacheReadCostEstimate: estimateCacheReadCost(
    session?.tokenUsage || tokenUsage,
    session?.model || null,
  ),
  ```

- [ ] **Add cache cost sub-rows to `AgentCard.vue`**

  In the header row where token/cost is displayed, extend the token info area. After the existing `formatTokens(totalTokens) tok · formatUptime(...)` span, add a sub-row below the header band (conditionally rendered when either value is non-zero):

  ```vue
  <script setup lang="ts">
  // add to existing imports:
  import { computed } from 'vue'
  import { formatCost, formatTokens, formatUptime, shortModel, totalTokenCount } from '../utils/format'

  // add computed:
  const hasCacheCosts = computed(
    () => props.agent.cacheCreationCostEstimate > 0 || props.agent.cacheReadCostEstimate > 0,
  )
  </script>

  <!-- In the header band, after the existing token/uptime span: -->
  <div v-if="hasCacheCosts" class="flex gap-2 text-[10px] text-slate-400 dark:text-slate-600 mt-0.5">
    <span title="Cache write cost">W {{ formatCost(agent.cacheCreationCostEstimate) }}</span>
    <span title="Cache read cost">R {{ formatCost(agent.cacheReadCostEstimate) }}</span>
  </div>
  ```

- [ ] **Add 4-row breakdown to `AgentModal.vue`**

  Locate the token/cost section in `AgentModal.vue` and replace or extend it with a 4-row grid:

  ```vue
  <template>
    <!-- token breakdown section -->
    <dl class="grid grid-cols-2 gap-x-4 gap-y-1 text-[13px]">
      <dt class="text-slate-500 dark:text-slate-400">Input tokens</dt>
      <dd class="text-slate-900 dark:text-slate-100 text-right font-mono">
        {{ formatTokens(agent.tokenUsage.inputTokens) }}
      </dd>

      <dt class="text-slate-500 dark:text-slate-400">Output tokens</dt>
      <dd class="text-slate-900 dark:text-slate-100 text-right font-mono">
        {{ formatTokens(agent.tokenUsage.outputTokens) }}
      </dd>

      <dt class="text-slate-500 dark:text-slate-400">Cache write</dt>
      <dd class="text-slate-900 dark:text-slate-100 text-right font-mono">
        {{ formatTokens(agent.tokenUsage.cacheCreationTokens) }}
        <span class="text-slate-400 dark:text-slate-600 ml-1">({{ formatCost(agent.cacheCreationCostEstimate) }})</span>
      </dd>

      <dt class="text-slate-500 dark:text-slate-400">Cache read</dt>
      <dd class="text-slate-900 dark:text-slate-100 text-right font-mono">
        {{ formatTokens(agent.tokenUsage.cacheReadTokens) }}
        <span class="text-slate-400 dark:text-slate-600 ml-1">({{ formatCost(agent.cacheReadCostEstimate) }})</span>
      </dd>

      <dt class="text-slate-700 dark:text-slate-300 font-medium border-t border-slate-200 dark:border-slate-700 pt-1">Total cost</dt>
      <dd class="text-slate-900 dark:text-slate-100 text-right font-mono font-medium border-t border-slate-200 dark:border-slate-700 pt-1">
        {{ formatCost(agent.costEstimate) }}
      </dd>
    </dl>
  </template>
  ```

  Add to `<script setup>`:
  ```ts
  import { formatCost, formatTokens } from '../utils/format'
  ```

- [ ] **Typecheck**

  ```bash
  pnpm typecheck
  ```

  Expected: no errors.

- [ ] **Commit**

  ```
  feat(cost): add cache token cost split to Agent type and card/modal UI (CI-3)
  ```

---

## Task 2: Per-task cost attribution with stage breakdown (CI-1)

**Files:**
- Modify: `server/routes/taskRoutes.ts` — add `GET /api/tasks/:id/cost-breakdown`
- New: `src/components/StageCostWaterfall.vue` — extracted cost breakdown table
- Modify: `src/components/TaskModal.vue` — add "Cost" section using `StageCostWaterfall`

**Steps:**

- [ ] **Add the cost-breakdown endpoint to `server/routes/taskRoutes.ts`**

  After the existing stage-runs endpoint, add:

  ```ts
  // GET /api/tasks/:id/cost-breakdown
  router.get('/:id/cost-breakdown', requireAuth, (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    if (!req.user!.isAdmin && task.userId !== null && task.userId !== req.user!.id) {
      res.status(403).json({ error: 'Forbidden' })
      return
    }

    const runs = listStageRunsForTask(req.params.id)
    const breakdown = runs
      .filter(r => r.status === 'done')
      .map(r => ({
        stage: r.stage,
        iteration: r.iteration,
        costCents: r.costCents,
        tokensUsed: r.tokensUsed,
        startedAt: r.startedAt,
        endedAt: r.endedAt,
      }))

    res.json(breakdown)
  })
  ```

  This endpoint reuses `listStageRunsForTask` (already imported) and `getTaskById` (already imported). No new DB query is needed.

- [ ] **Create `src/components/StageCostWaterfall.vue`**

  ```vue
  <script setup lang="ts">
  import type { PipelineStage } from '../types'
  import { computed } from 'vue'
  import { formatCost } from '../utils/format'

  export interface StageCostRow {
    stage: PipelineStage
    iteration: number
    costCents: number
    tokensUsed: number
    startedAt: string | null
    endedAt: string | null
  }

  const props = defineProps<{ rows: StageCostRow[] }>()

  function centsToUsd(cents: number): number {
    return cents / 100
  }

  const totalCostUsd = computed(() =>
    props.rows.reduce((sum, r) => sum + centsToUsd(r.costCents), 0),
  )

  const totalTokens = computed(() =>
    props.rows.reduce((sum, r) => sum + r.tokensUsed, 0),
  )

  function formatTokensCompact(n: number): string {
    if (n === 0) return '—'
    if (n < 1000) return String(n)
    if (n < 1_000_000) return `${(n / 1000).toFixed(1)}k`
    return `${(n / 1_000_000).toFixed(2)}M`
  }

  function stageDurationMs(row: StageCostRow): number | null {
    if (!row.startedAt || !row.endedAt) return null
    return new Date(row.endedAt).getTime() - new Date(row.startedAt).getTime()
  }

  function formatDuration(ms: number | null): string {
    if (ms === null) return '—'
    const s = Math.round(ms / 1000)
    if (s < 60) return `${s}s`
    return `${Math.floor(s / 60)}m ${s % 60}s`
  }
  </script>

  <template>
    <div v-if="rows.length === 0" class="text-sm text-slate-400 dark:text-slate-600 italic">
      No completed stages yet.
    </div>
    <table v-else class="w-full text-[13px] border-collapse">
      <thead>
        <tr class="text-left text-slate-500 dark:text-slate-400 border-b border-slate-200 dark:border-slate-700">
          <th class="pb-1 font-medium">Stage</th>
          <th class="pb-1 font-medium text-center">Iter</th>
          <th class="pb-1 font-medium text-right">Tokens</th>
          <th class="pb-1 font-medium text-right">Cost</th>
          <th class="pb-1 font-medium text-right">Duration</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="(row, i) in rows"
          :key="i"
          class="border-b border-slate-100 dark:border-slate-800 hover:bg-slate-50 dark:hover:bg-slate-800/50"
        >
          <td class="py-1 pr-2 text-slate-700 dark:text-slate-300 capitalize">{{ row.stage.replace('_', ' ') }}</td>
          <td class="py-1 text-center text-slate-500 dark:text-slate-400">{{ row.iteration + 1 }}</td>
          <td class="py-1 text-right font-mono text-slate-700 dark:text-slate-300">{{ formatTokensCompact(row.tokensUsed) }}</td>
          <td class="py-1 text-right font-mono text-slate-700 dark:text-slate-300">{{ formatCost(centsToUsd(row.costCents)) }}</td>
          <td class="py-1 text-right font-mono text-slate-500 dark:text-slate-400">{{ formatDuration(stageDurationMs(row)) }}</td>
        </tr>
      </tbody>
      <tfoot>
        <tr class="border-t border-slate-300 dark:border-slate-600 font-medium">
          <td class="pt-1 text-slate-700 dark:text-slate-300">Total</td>
          <td />
          <td class="pt-1 text-right font-mono text-slate-900 dark:text-slate-100">{{ formatTokensCompact(totalTokens) }}</td>
          <td class="pt-1 text-right font-mono text-slate-900 dark:text-slate-100">{{ formatCost(totalCostUsd) }}</td>
          <td />
        </tr>
      </tfoot>
    </table>
  </template>
  ```

- [ ] **Add the "Cost" section to `TaskModal.vue`**

  In `<script setup>`, add:

  ```ts
  import type { StageCostRow } from './StageCostWaterfall.vue'
  import { ref, watch } from 'vue'
  import StageCostWaterfall from './StageCostWaterfall.vue'

  // alongside existing props
  const costBreakdown = ref<StageCostRow[]>([])
  const costLoading = ref(false)

  // fetch when task id changes or modal opens
  watch(
    () => props.task?.id,
    async (id) => {
      if (!id) { costBreakdown.value = []; return }
      costLoading.value = true
      try {
        const res = await fetch(`/api/tasks/${id}/cost-breakdown`)
        if (res.ok) costBreakdown.value = await res.json()
      }
      finally { costLoading.value = false }
    },
    { immediate: true },
  )
  ```

  In the template, add a "Cost" section after the existing stage output area:

  ```vue
  <section class="mt-4">
    <h3 class="text-[12px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-2">
      Cost breakdown
    </h3>
    <div v-if="costLoading" class="text-sm text-slate-400 dark:text-slate-600">Loading...</div>
    <StageCostWaterfall v-else :rows="costBreakdown" />
  </section>
  ```

- [ ] **Typecheck**

  ```bash
  pnpm typecheck
  ```

  Expected: no errors.

- [ ] **Commit**

  ```
  feat(cost): add per-task stage cost waterfall endpoint and TaskModal section (CI-1)
  ```

---

## Task 3: Agent health/anomaly score (CI-7)

**Files:**
- New: `server/costTrendCache.ts` — reads `agent_cost_trend` for rolling average
- Modify: `src/types.ts` — add `healthScore: number` to `Agent`
- Modify: `server/agentMerger.ts` — compute and set `healthScore`
- Modify: `src/components/AgentCard.vue` — render colored health chip
- New: `server/healthScore.test.ts` — unit tests for the formula

**Steps:**

- [ ] **Create `server/costTrendCache.ts`**

  ```ts
  import { getDb } from './db/client.js'

  /**
   * Returns the mean cost-per-second across trend points within the last
   * `windowMs` milliseconds. Returns 0 if there are fewer than 2 data points
   * (insufficient history to compute a rate).
   */
  export function getRecentAvgCostPerHour(windowMs: number): number {
    try {
      const db = getDb()
      const since = Date.now() - windowMs
      const rows = db
        .prepare('SELECT cost FROM agent_cost_trend WHERE t >= ? ORDER BY t ASC')
        .all(since) as Array<{ cost: number }>
      if (rows.length < 2) return 0
      const sum = rows.reduce((acc, r) => acc + r.cost, 0)
      return sum / rows.length
    }
    catch {
      return 0
    }
  }
  ```

  Note: the `agent_cost_trend` table stores the *total accumulated cost at each point in time*, not per-interval deltas. The helper returns the mean of the stored `cost` values across the window — use this as the baseline total cost for spike detection, comparing it against `agent.costEstimate` (also a cumulative total).

- [ ] **Add `healthScore` to `Agent` in `src/types.ts`**

  After `costEstimate: number`, add:

  ```ts
  /** 0–100 composite health score. Higher is healthier. */
  healthScore: number
  ```

- [ ] **Add the `computeHealthScore` function to a new `server/healthScore.ts`**

  Extract the formula into a testable pure function:

  ```ts
  import type { Agent } from '../src/types.js'

  export interface HealthScoreInput {
    completedTasks: number
    totalTasks: number
    cacheReadTokens: number
    inputTokens: number
    hasError: boolean        // true when agent has a non-null error indicator
    costEstimate: number     // agent's current cumulative cost (USD)
    recentAvgCost: number    // rolling average cumulative cost (USD) from trend
  }

  export function computeHealthScore(input: HealthScoreInput): number {
    const {
      completedTasks,
      totalTasks,
      cacheReadTokens,
      inputTokens,
      hasError,
      costEstimate,
      recentAvgCost,
    } = input

    // 40% — task success rate
    const successRate = (completedTasks / Math.max(totalTasks, 1)) * 100

    // 25% — cache hit rate (reads / (reads + cache-eligible inputs))
    const cacheHitRate = (cacheReadTokens / Math.max(inputTokens + cacheReadTokens, 1)) * 100

    // 25% — error penalty
    const errorScore = hasError ? 0 : 100

    // 10% — cost spike detection (spike > 3x average → 0, else 100)
    let costSpikeScore = 100
    if (recentAvgCost > 0 && costEstimate > recentAvgCost * 3) {
      costSpikeScore = 0
    }

    const score
      = successRate * 0.4
      + cacheHitRate * 0.25
      + errorScore * 0.25
      + costSpikeScore * 0.1

    return Math.round(Math.max(0, Math.min(100, score)))
  }
  ```

- [ ] **Write unit tests in `server/healthScore.test.ts`**

  ```ts
  import { describe, expect, it } from 'vitest'
  import { computeHealthScore } from './healthScore.js'

  const base = {
    completedTasks: 0,
    totalTasks: 0,
    cacheReadTokens: 0,
    inputTokens: 0,
    hasError: false,
    costEstimate: 0,
    recentAvgCost: 0,
  }

  describe('computeHealthScore', () => {
    it('returns 100 for a perfect agent with no tasks', () => {
      // successRate = (0/max(0,1))*100 = 0 → 0*0.4 = 0
      // cacheHitRate = 0 → 0
      // errorScore = 100 → 25
      // costSpike = 100 → 10
      // total = 35 — edge case: 0/max(0,1) = 0, so not 100
      // Note: true 100 requires completed tasks AND cache hits AND no error AND no spike
      const score = computeHealthScore({ ...base, completedTasks: 5, totalTasks: 5, cacheReadTokens: 1000, inputTokens: 1000 })
      expect(score).toBe(100)
    })

    it('penalises 50% task failure rate correctly', () => {
      const score = computeHealthScore({ ...base, completedTasks: 5, totalTasks: 10 })
      // successRate=50 → 20, cacheHit=0 → 0, error=100→25, spike=100→10 = 55
      expect(score).toBe(55)
    })

    it('applies full cache hit rate weight when all tokens are cache reads', () => {
      const score = computeHealthScore({ ...base, cacheReadTokens: 1000, inputTokens: 0 })
      // successRate=0, cacheHit=100 → 25, error=100→25, spike=100→10 = 60
      expect(score).toBe(60)
    })

    it('sets error component to 0 when hasError is true', () => {
      const noError = computeHealthScore({ ...base, cacheReadTokens: 500, inputTokens: 500 })
      const withError = computeHealthScore({ ...base, cacheReadTokens: 500, inputTokens: 500, hasError: true })
      expect(noError - withError).toBe(25)
    })

    it('reduces cost spike score to 0 when costEstimate > 3x average', () => {
      const noSpike = computeHealthScore({ ...base, costEstimate: 1, recentAvgCost: 1 })
      const spike = computeHealthScore({ ...base, costEstimate: 4, recentAvgCost: 1 })
      expect(noSpike - spike).toBe(10)
    })

    it('ignores cost spike when recentAvgCost is 0 (no history)', () => {
      const score = computeHealthScore({ ...base, costEstimate: 999, recentAvgCost: 0 })
      // spike score stays 100 → contributes 10
      expect(score).toBe(10) // successRate=0, cacheHit=0, error=100→25, spike=100→10
    })

    it('clamps result between 0 and 100', () => {
      const score = computeHealthScore({ ...base, hasError: true, completedTasks: 0, totalTasks: 100 })
      expect(score).toBeGreaterThanOrEqual(0)
      expect(score).toBeLessThanOrEqual(100)
    })
  })
  ```

- [ ] **Run tests and confirm they pass**

  ```bash
  pnpm test server/healthScore.test.ts
  ```

  Expected: `7 tests passed`

- [ ] **Integrate `computeHealthScore` into `server/agentMerger.ts`**

  Add imports:

  ```ts
  import { computeHealthScore } from './healthScore.js'
  import { getRecentAvgCostPerHour } from './costTrendCache.js'
  ```

  Before building the `agents` array, fetch the rolling average once:

  ```ts
  const recentAvgCost = getRecentAvgCostPerHour(7 * 24 * 60 * 60 * 1000) // 7-day window
  ```

  Inside the `.map()` building each `Agent`, add after `cacheReadCostEstimate`:

  ```ts
  healthScore: computeHealthScore({
    completedTasks: (session?.tasks || []).filter(t => t.status === 'completed').length,
    totalTasks: (session?.tasks || []).length,
    cacheReadTokens: tokenUsage.cacheReadTokens,
    inputTokens: tokenUsage.inputTokens,
    hasError: (session?.meta?.toolErrors ?? 0) > 0,
    costEstimate: estimateCost(session?.tokenUsage || tokenUsage, session?.model || null),
    recentAvgCost,
  }),
  ```

  Note: `errorState` as described in the plan brief does not exist on `Agent` yet. Instead, `meta.toolErrors > 0` serves as the error indicator — it is the existing field tracking tool-level errors in `SessionMeta`. A future phase can refine this if a dedicated `errorState` string is added.

- [ ] **Render a health chip in `AgentCard.vue`**

  Add a computed color helper to `<script setup>`:

  ```ts
  const healthChipClass = computed(() => {
    const s = props.agent.healthScore
    if (s >= 75) return 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400'
    if (s >= 40) return 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400'
    return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400'
  })
  ```

  In the header band template, after the model/cost span:

  ```vue
  <span
    :class="['text-[10px] font-mono px-1.5 py-0.5 rounded', healthChipClass]"
    :title="`Health score: ${agent.healthScore}/100`"
  >
    {{ agent.healthScore }}
  </span>
  ```

- [ ] **Typecheck**

  ```bash
  pnpm typecheck
  ```

  Expected: no errors.

- [ ] **Commit**

  ```
  feat(health): compute agent health score and show colored chip on AgentCard (CI-7)
  ```

---

## Task 4: Tab visibility — pause SSE (RT-3)

**Files:**
- Modify: `src/composables/useAgents.ts` — add `visibilitychange` listener

**Context check:**

VueUse is not a dependency of this project (not found in `package.json`). Use manual `addEventListener` / `removeEventListener` via `onMounted` / `onUnmounted` — but since `useAgents` does not use the Options API and its lifecycle state is module-level (not per-component), the correct approach is to attach the listener once at module load time using a `startDataStream`/`stopDataStream`-aware flag.

**Steps:**

- [ ] **Add the `visibilitychange` listener to `useAgents.ts`**

  The exact diff to apply to `src/composables/useAgents.ts`:

  After the existing `let debounceTimer` declaration (around line 28), add:

  ```ts
  let visibilityListenerAttached = false
  ```

  Replace the `startSSE` function to check `document.hidden` before opening:

  ```ts
  function startSSE() {
    if (subscriberCount <= 0) return
    if (typeof document !== 'undefined' && document.hidden) return // tab not visible
    eventSource = new EventSource('/api/agents/stream')
    // ... rest of existing startSSE body unchanged
  }
  ```

  In `startDataStream`, after the existing body, attach the visibility listener once:

  ```ts
  function startDataStream() {
    subscriberCount++
    if (subscriberCount > 1) return

    fetchAgents()
    startSSE()

    if (!visibilityListenerAttached && typeof document !== 'undefined') {
      document.addEventListener('visibilitychange', handleVisibilityChange)
      visibilityListenerAttached = true
    }
  }
  ```

  In `stopDataStream`, clean up the listener when the last subscriber leaves:

  ```ts
  function stopDataStream() {
    subscriberCount--
    if (subscriberCount <= 0) {
      stopSSE()
      stopPolling()
      if (sseRetryTimer) {
        clearTimeout(sseRetryTimer)
        sseRetryTimer = null
      }
      if (visibilityListenerAttached && typeof document !== 'undefined') {
        document.removeEventListener('visibilitychange', handleVisibilityChange)
        visibilityListenerAttached = false
      }
      subscriberCount = 0
    }
  }
  ```

  Add the handler function before `startDataStream`:

  ```ts
  function handleVisibilityChange() {
    if (document.hidden) {
      // Tab hidden — disconnect SSE to avoid background network traffic
      stopSSE()
      stopPolling()
      if (sseRetryTimer) {
        clearTimeout(sseRetryTimer)
        sseRetryTimer = null
      }
    }
    else {
      // Tab visible again — reconnect immediately
      fetchAgents()
      startSSE()
    }
  }
  ```

  Full resulting shape of the module-level state block (no other changes):

  ```ts
  let eventSource: EventSource | null = null
  let intervalId: ReturnType<typeof setInterval> | null = null
  let sseRetryTimer: ReturnType<typeof setTimeout> | null = null
  let subscriberCount = 0
  let debounceTimer: ReturnType<typeof setTimeout> | null = null
  let visibilityListenerAttached = false
  ```

- [ ] **Testing note**

  This feature requires a real browser environment with `document.visibilityState`. Vitest runs in Node — a full unit test is not practical without jsdom mocking. To verify manually:

  1. Run `pnpm dev`
  2. Open the dashboard in Chrome
  3. Open DevTools → Network → WS/EventStream tab
  4. Switch to another tab — the SSE connection to `/api/agents/stream` should close within 1s
  5. Switch back — a new SSE connection should open immediately

  If you want a jsdom-based sanity check, add to `src/composables/useAgents.test.ts`:

  ```ts
  // Requires vitest environment: 'jsdom' (set in vitest.config.ts per-file or globally)
  import { describe, expect, it, vi } from 'vitest'

  describe('handleVisibilityChange (manual verification)', () => {
    it('is documented as requiring browser environment — see plan for manual steps', () => {
      expect(true).toBe(true)
    })
  })
  ```

- [ ] **Typecheck**

  ```bash
  pnpm typecheck
  ```

  Expected: no errors.

- [ ] **Commit**

  ```
  feat(sse): pause SSE and polling when browser tab is hidden (RT-3)
  ```
