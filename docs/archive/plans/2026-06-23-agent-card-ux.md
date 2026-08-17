# Agent Card UX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Fix and polish the roster agent card — a real output preview (never falsely "No output yet"), a reliable card-click → modal, a prominent/readable project name, a glance-friendly 3-metric row with full labeled detail in a hover popover + the modal, and a consistently bottom-docked prompt input.

**Architecture:** Almost entirely frontend. `AgentCard.vue` becomes a fixed-height flex column (header → flex-grow output body → optional subagent/btw sections → bottom-docked `PromptInput`). The output body gains a fallback chain (`lastOutput` → `currentAction` → `lastTools[0]` → "No output yet"). A new small `MetricsPopover.vue` shows labeled uptime/last-activity/burn/cache on ⓘ hover. The backend `LastOutput` already persists across the parsed tail, so the only backend change is a regression-guard test. `AgentModal` gains labeled metric rows.

**Tech Stack:** Vue 3 `<script setup>`, Tailwind v4, Vitest + `@vue/test-utils`, Go (parser test).

**Spec:** `docs/superpowers/specs/2026-06-23-agent-card-ux-design.md`.

---

## File Structure

| File | Responsibility | Change |
|---|---|---|
| `server/internal/parser/parser.go` | JSONL → SessionData | none (already persists `LastOutput`); a regression test guards it |
| `server/internal/parser/parser_lastoutput_test.go` (new) | guard `LastOutput` retention | new |
| `src/components/MetricsPopover.vue` (new) | labeled metric breakdown on ⓘ hover | new |
| `src/components/AgentCard.vue` | the card: output fallback, header, meta row + ⓘ, flex-column bottom-dock | rewrite template + script |
| `src/components/AgentModal.vue` | full-detail modal | add labeled uptime/last-activity/burn rows |
| `src/components/__tests__/AgentCard.test.ts` (new) | card behavior tests | new |
| `src/components/__tests__/MetricsPopover.test.ts` (new) | popover content test | new |

**Branch:** `feat/agent-card-ux`, off `main` (already created). Independent of PR #213/#214.

---

## Task 1: Parser regression guard — `LastOutput` survives a trailing tool-only message

**Files:**
- Create: `server/internal/parser/parser_lastoutput_test.go`

The parser already sets `data.LastOutput` from any assistant `text` block and only overwrites it when a later message has a text block — so a trailing tool-only message retains the earlier text. This task locks that invariant (the root of the "No output yet" complaint when combined with the frontend fallback in Task 3).

- [ ] **Step 1: Find the parse entry point.** Run `grep -n "func ParseSessionFile\|func parseJSONL\|func.*Parse.*string.*SessionData" server/internal/parser/parser.go` and note the exported function that parses a session file path into `*SessionData` (e.g. `ParseSessionFile(path string) (*SessionData, error)`).

- [ ] **Step 2: Write the failing test** (adapt `ParseSessionFile` to the real name from Step 1):

```go
// server/internal/parser/parser_lastoutput_test.go
package parser

import (
	"os"
	"path/filepath"
	"testing"
)

// A tail ending in a tool-only assistant message must still expose the
// preceding assistant text as LastOutput (the card's output preview source).
func TestParse_LastOutputSurvivesTrailingToolOnlyMessage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	lines := `{"type":"assistant","message":{"role":"assistant","model":"claude-opus-4-8","content":[{"type":"text","text":"Investigating the failing test now."}]}}
{"type":"assistant","message":{"role":"assistant","model":"claude-opus-4-8","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}
`
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := ParseSessionFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if data.LastOutput != "Investigating the failing test now." {
		t.Fatalf("LastOutput must survive a trailing tool-only message, got %q", data.LastOutput)
	}
	if data.CurrentAction != "Bash" {
		t.Fatalf("CurrentAction should be the last tool, got %q", data.CurrentAction)
	}
}
```

- [ ] **Step 3: Run it**

Run: `cd server && go test ./internal/parser/ -run TestParse_LastOutputSurvives -v`
Expected: PASS (the behavior already holds). If it FAILS, the parser resets `LastOutput` on non-text messages — fix by ensuring `data.LastOutput` is only assigned inside the `case "text"` block (never cleared elsewhere), then re-run to PASS. Do NOT weaken the test.

- [ ] **Step 4: Commit**

```bash
git add server/internal/parser/parser_lastoutput_test.go
git commit --no-gpg-sign --no-verify -m "test: guard LastOutput retention across trailing tool-only message"
```

---

## Task 2: `MetricsPopover.vue` — labeled metric breakdown

**Files:**
- Create: `src/components/MetricsPopover.vue`
- Create: `src/components/__tests__/MetricsPopover.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// src/components/__tests__/MetricsPopover.test.ts
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import MetricsPopover from '../MetricsPopover.vue'

function makeAgent(overrides = {}) {
  return {
    pid: 1,
    uptime: 1680, // 28m
    lastActivity: new Date(Date.now() - 27 * 60 * 1000).toISOString(),
    costEstimate: 6.09,
    cacheCreationCostEstimate: 4.64,
    cacheReadCostEstimate: 0.08,
    ...overrides,
  } as any
}

describe('MetricsPopover', () => {
  it('renders labeled uptime, last activity, burn, and cache rows', () => {
    const wrapper = mount(MetricsPopover, { props: { agent: makeAgent() } })
    const text = wrapper.get('[data-testid="metrics-popover"]').text()
    expect(text).toContain('Uptime')
    expect(text).toContain('Last activity')
    expect(text).toContain('Burn rate')
    expect(text).toContain('Cache write')
    expect(text).toContain('Cache read')
  })

  it('hides the cache rows when there are no cache costs', () => {
    const wrapper = mount(MetricsPopover, { props: { agent: makeAgent({ cacheCreationCostEstimate: 0, cacheReadCostEstimate: 0 }) } })
    expect(wrapper.get('[data-testid="metrics-popover"]').text()).not.toContain('Cache write')
  })
}) 
```

- [ ] **Step 2: Run to verify it fails**

Run: `pnpm test -- MetricsPopover`
Expected: FAIL — cannot resolve `../MetricsPopover.vue`.

- [ ] **Step 3: Implement** `src/components/MetricsPopover.vue`. The `format` utility already exports `formatBurnRate`, `formatRelativeActivity`, `formatUptime`, `formatCost`, `secondsSince` (used by `AgentCard.vue`).

```vue
<script setup lang="ts">
import type { Agent } from '../types'
import { computed } from 'vue'
import { useNow } from '../composables/useNow'
import { formatBurnRate, formatCost, formatRelativeActivity, formatUptime, secondsSince } from '../utils/format'

const props = defineProps<{ agent: Agent }>()
const { nowMs } = useNow()

const lastActivity = computed(() => formatRelativeActivity(secondsSince(props.agent.lastActivity, nowMs.value)))
const burn = computed(() => formatBurnRate(props.agent.costEstimate, props.agent.uptime))
const hasCache = computed(() => props.agent.cacheCreationCostEstimate > 0 || props.agent.cacheReadCostEstimate > 0)
</script>

<template>
  <div
    class="absolute right-0 top-full mt-1 z-20 w-44 rounded-md border border-line bg-card shadow-card-hover px-2.5 py-2 text-[11px] font-mono flex flex-col gap-0.5"
    data-testid="metrics-popover"
    role="tooltip"
  >
    <div class="flex justify-between gap-3">
      <span class="text-fg-mute">Uptime</span><span class="text-fg">{{ formatUptime(agent.uptime) }}</span>
    </div>
    <div class="flex justify-between gap-3">
      <span class="text-fg-mute">Last activity</span><span class="text-fg">{{ lastActivity }}</span>
    </div>
    <div v-if="burn !== '—'" class="flex justify-between gap-3">
      <span class="text-fg-mute">Burn rate</span><span class="text-fg">{{ burn }}</span>
    </div>
    <template v-if="hasCache">
      <div class="flex justify-between gap-3">
        <span class="text-fg-mute">Cache write</span><span class="text-fg">{{ formatCost(agent.cacheCreationCostEstimate) }}</span>
      </div>
      <div class="flex justify-between gap-3">
        <span class="text-fg-mute">Cache read</span><span class="text-fg">{{ formatCost(agent.cacheReadCostEstimate) }}</span>
      </div>
    </template>
  </div>
</template>
```

- [ ] **Step 4: Run to verify it passes**

Run: `pnpm test -- MetricsPopover`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add src/components/MetricsPopover.vue src/components/__tests__/MetricsPopover.test.ts
git commit --no-gpg-sign --no-verify -m "feat: add MetricsPopover with labeled agent metrics"
```

---

## Task 3: `AgentCard.vue` rework — output fallback, header, meta row, ⓘ, bottom-docked prompt

**Files:**
- Modify: `src/components/AgentCard.vue`
- Create: `src/components/__tests__/AgentCard.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// src/components/__tests__/AgentCard.test.ts
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import AgentCard from '../AgentCard.vue'

// PromptInput pulls in composables with side effects; stub it.
const stubs = {
  PromptInput: { template: '<div data-testid="prompt-input" />' },
  MachineBadge: true,
  ProviderBadge: true,
}

function makeAgent(overrides = {}) {
  return {
    pid: 42,
    sessionId: 's1',
    provider: 'claude',
    projectPath: '/home/u/agent-dashboard',
    projectName: 'agent-dashboard',
    cwd: '/home/u/agent-dashboard',
    status: 'active',
    working: true,
    uptime: 1680,
    lastActivity: new Date().toISOString(),
    currentAction: '',
    lastOutput: '',
    lastTools: [],
    subagents: [],
    tokenUsage: { inputTokens: 100, outputTokens: 50, cacheCreationTokens: 0, cacheReadTokens: 0 },
    costEstimate: 6.09,
    costUnknown: false,
    cacheCreationCostEstimate: 0,
    cacheReadCostEstimate: 0,
    healthScore: 79,
    model: 'claude-opus-4-8',
    ...overrides,
  } as any
}

describe('AgentCard output body', () => {
  it('shows lastOutput when present', () => {
    const w = mount(AgentCard, { props: { agent: makeAgent({ lastOutput: 'hello from claude' }) }, global: { stubs } })
    expect(w.text()).toContain('hello from claude')
    expect(w.text()).not.toContain('No output yet')
  })

  it('falls back to currentAction when lastOutput is empty', () => {
    const w = mount(AgentCard, { props: { agent: makeAgent({ lastOutput: '', currentAction: 'Bash' }) }, global: { stubs } })
    expect(w.text()).toContain('Bash')
    expect(w.text()).not.toContain('No output yet')
  })

  it('falls back to last tool when no output or action', () => {
    const w = mount(AgentCard, { props: { agent: makeAgent({ lastOutput: '', currentAction: '', lastTools: ['Read'] }) }, global: { stubs } })
    expect(w.text()).toContain('Read')
    expect(w.text()).not.toContain('No output yet')
  })

  it('shows "No output yet" only when truly empty', () => {
    const w = mount(AgentCard, { props: { agent: makeAgent({ lastOutput: '', currentAction: '', lastTools: [] }) }, global: { stubs } })
    expect(w.text()).toContain('No output yet')
  })
})

describe('AgentCard interaction', () => {
  it('emits select when the card body is clicked', async () => {
    const w = mount(AgentCard, { props: { agent: makeAgent() }, global: { stubs } })
    await w.get('[data-testid="agent-card-open"]').trigger('click')
    expect(w.emitted('select')).toBeTruthy()
  })

  it('does not emit select when the prompt input is clicked', async () => {
    const w = mount(AgentCard, { props: { agent: makeAgent() }, global: { stubs } })
    await w.get('[data-testid="prompt-input"]').trigger('click')
    expect(w.emitted('select')).toBeFalsy()
  })
})

describe('AgentCard header', () => {
  it('shows the friendly project name with a title tooltip', () => {
    const w = mount(AgentCard, { props: { agent: makeAgent({ projectName: 'agent-dashboard' }) }, global: { stubs } })
    const name = w.get('[data-testid="agent-card-project"]')
    expect(name.text()).toContain('Agent Dashboard')
    expect(name.attributes('title')).toContain('Agent Dashboard')
  })

  it('renders the info affordance that reveals the metrics popover', async () => {
    const w = mount(AgentCard, { props: { agent: makeAgent() }, global: { stubs } })
    expect(w.find('[data-testid="metrics-popover"]').exists()).toBe(false)
    await w.get('[data-testid="agent-card-info"]').trigger('click')
    expect(w.find('[data-testid="metrics-popover"]').exists()).toBe(true)
  })
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `pnpm test -- AgentCard`
Expected: FAIL — new `data-testid`s (`agent-card-project`, `agent-card-info`, `metrics-popover`) and the fallback/interaction behaviors don't exist yet.

- [ ] **Step 3: Rewrite `src/components/AgentCard.vue`** with the full content below. Changes vs current: (A) output fallback chain; (C) header split into a primary row (project gets `flex-1` width + title tooltip) and a meta row; (D) meta row = cost · tokens · uptime + health + ⓘ→`MetricsPopover` (burn/cache/last-activity removed from the always-visible card); (E) card root is `flex flex-col` with fixed height, output body is `flex-1 min-h-0`, prompt is the last child (bottom-docked). The `inset-0` overlay button keeps mouse-click→select and keyboard a11y; interactive children keep `@click.stop`.

```vue
<script setup lang="ts">
import type { Agent } from '../types'
import { computed, ref } from 'vue'
import { useAgentIdentity } from '../composables/useAgentIdentity'
import { useNow } from '../composables/useNow'
import { formatCost, formatDuration, formatTokens, formatUptime, isStalled, secondsSince, shortModel, totalTokenCount } from '../utils/format'
import { friendlyProjectName } from '../utils/friendlyProjectName'
import MachineBadge from './MachineBadge.vue'
import MetricsPopover from './MetricsPopover.vue'
import PromptInput from './PromptInput.vue'
import ProviderBadge from './ProviderBadge.vue'
import AppBadge from './ui/AppBadge.vue'
import AppCard from './ui/AppCard.vue'

const props = defineProps<{ agent: Agent }>()
const emit = defineEmits<{ select: [agent: Agent], dismiss: [pid: number] }>()

const isFinished = computed(() => props.agent.status === 'finished')

async function dismiss() {
  const pid = props.agent.pid
  try {
    await fetch(`/api/agents/${pid}/channel`, { method: 'DELETE', credentials: 'same-origin' })
  }
  catch {
    // best-effort: the next SSE frame still reflects server truth
  }
  emit('dismiss', pid)
}

const { getIdentity } = useAgentIdentity()
const { nowMs } = useNow()

const totalTokens = computed(() => totalTokenCount(props.agent.tokenUsage))
const projectLabel = computed(() => friendlyProjectName(props.agent.projectName))

// Output preview: real assistant text, else current activity, else "No output yet".
const activityFallback = computed(() => props.agent.currentAction || props.agent.lastTools?.[0] || '')

const healthChipClass = computed(() => {
  const s = props.agent.healthScore
  if (s >= 75)
    return 'bg-success-soft text-success-text'
  if (s >= 40)
    return 'bg-warning-soft text-warning-text'
  return 'bg-danger-soft text-danger-text'
})

const secSince = computed(() => secondsSince(props.agent.lastActivity, nowMs.value))
const stalled = computed(() => isStalled(props.agent.status, secSince.value))

const activeSubagents = computed(() => props.agent.subagents.filter(s => s.status === 'active'))

const expandedSubagentIds = ref<Set<string>>(new Set())
function toggleSubagentExpand(id: string) {
  const next = new Set(expandedSubagentIds.value)
  if (next.has(id))
    next.delete(id)
  else
    next.add(id)
  expandedSubagentIds.value = next
}

// ⓘ metrics popover: open on hover (desktop) and on click/tap (touch).
const showMetrics = ref(false)
function toggleMetrics() {
  showMetrics.value = !showMetrics.value
}
</script>

<template>
  <AppCard surface="card" radius="lg" interactive class="relative flex flex-col h-[260px] overflow-hidden cursor-pointer">
    <button
      type="button"
      class="absolute inset-0 w-full h-full focus-visible:outline-2 focus-visible:outline-ring focus-visible:outline-offset-[-2px]"
      :aria-label="`Open details for ${projectLabel}`"
      data-testid="agent-card-open"
      @click="$emit('select', agent)"
    />

    <!-- Header: primary row (project prominent) + meta row -->
    <div class="bg-raised px-3 pt-2 pb-1.5 flex flex-col gap-1">
      <div class="flex items-center gap-2 min-w-0">
        <AppBadge :variant="agent.working ? 'working' : agent.status" />
        <span
          v-if="stalled"
          class="text-[10px] font-medium px-1 py-0.5 rounded bg-warning-soft text-warning-text whitespace-nowrap"
          title="Agent is active but has produced no output for 3+ minutes"
        >stalled</span>
        <span class="shrink-0" aria-hidden="true">{{ getIdentity(agent.projectPath).emoji }}</span>
        <span
          class="font-semibold text-[13px] text-fg flex-1 min-w-0 whitespace-nowrap overflow-hidden text-ellipsis"
          data-testid="agent-card-project"
          :title="projectLabel"
        >{{ projectLabel }}</span>
        <ProviderBadge :provider="agent.provider" />
        <span class="text-[10px] font-mono text-fg-mute whitespace-nowrap shrink-0">{{ shortModel(agent.model ?? null) }}</span>
        <MachineBadge v-if="agent.machine" :machine="agent.machine" />
      </div>

      <div class="flex items-center gap-2 text-[11px] font-mono text-fg-mute min-w-0">
        <span class="whitespace-nowrap" title="Total estimated cost">
          <span v-if="agent.costUnknown" title="Cost unknown — no pricing data for this provider/model">?</span>
          <template v-else>{{ formatCost(agent.costEstimate) }}</template>
        </span>
        <span aria-hidden="true">·</span>
        <span class="whitespace-nowrap" title="Tokens used">{{ formatTokens(totalTokens) }} tok</span>
        <span aria-hidden="true">·</span>
        <span class="whitespace-nowrap" title="Uptime">{{ formatUptime(agent.uptime) }}</span>

        <span class="ml-auto flex items-center gap-1.5 shrink-0">
          <span
            class="text-[10px] font-mono px-1.5 py-0.5 rounded"
            :class="healthChipClass"
            :title="`Health score: ${agent.healthScore}/100`"
          >{{ agent.healthScore }}</span>
          <span class="relative z-10" @mouseenter="showMetrics = true" @mouseleave="showMetrics = false">
            <button
              type="button"
              class="text-fg-mute hover:text-fg-soft text-[11px] leading-none focus-visible:outline-2 focus-visible:outline-ring rounded"
              aria-label="Show more metrics"
              data-testid="agent-card-info"
              @click.stop="toggleMetrics"
            >ⓘ</button>
            <MetricsPopover v-if="showMetrics" :agent="agent" @click.stop />
          </span>
          <button
            v-if="isFinished"
            type="button"
            class="text-fg-mute hover:text-danger-text text-sm leading-none px-1 focus-visible:outline-2 focus-visible:outline-ring rounded"
            aria-label="Dismiss finished agent"
            data-testid="agent-card-dismiss"
            @click.stop="dismiss"
          >✕</button>
        </span>
      </div>
    </div>

    <!-- Output body: grows to fill; fallback chain so an active agent is never falsely empty -->
    <div class="relative flex-1 min-h-0 px-3 py-3 overflow-hidden text-[13px] leading-relaxed text-fg-mute font-mono">
      <template v-if="agent.lastOutput">
        {{ agent.lastOutput }}
      </template>
      <span v-else-if="activityFallback" class="text-fg-soft">▶ running: {{ activityFallback }}</span>
      <span v-else class="text-fg-mute italic">No output yet</span>
      <div class="absolute bottom-0 left-0 right-0 h-8 bg-gradient-to-t from-card to-transparent pointer-events-none" />
    </div>

    <!-- Active subagents (optional, between output and prompt) -->
    <div
      v-if="activeSubagents.length"
      data-testid="active-subagents-block"
      class="relative z-10 border-t border-line px-3 py-2 flex flex-col gap-1 shrink-0"
      @click.stop
    >
      <span class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">
        {{ activeSubagents.length }} active subagent{{ activeSubagents.length !== 1 ? 's' : '' }}
      </span>
      <div v-for="sa in activeSubagents" :key="sa.id" class="flex flex-col gap-0.5">
        <div class="flex items-center gap-1.5 flex-wrap">
          <AppBadge variant="active" />
          <span class="font-mono text-[11px] text-fg-soft">{{ sa.type }}</span>
          <span v-if="sa.currentAction" class="text-[10px] text-fg-mute">· {{ sa.currentAction }}</span>
          <span class="text-[10px] font-mono text-fg-mute ml-auto whitespace-nowrap">
            {{ formatDuration(sa.durationSeconds) }} · {{ Math.round(sa.tokensUsed / 1000) }}k tok
          </span>
        </div>
        <div v-if="sa.latestOutput" class="flex items-start gap-1">
          <span
            class="font-mono text-[11px] text-fg-mute leading-snug"
            :class="expandedSubagentIds.has(sa.id) ? 'whitespace-pre-wrap break-words' : 'truncate'"
            data-testid="subagent-latest-output"
          >{{ sa.latestOutput }}</span>
          <button
            type="button"
            class="flex-shrink-0 text-[10px] text-fg-mute hover:text-fg-soft focus-visible:outline-none focus-visible:ring-[2px] focus-visible:ring-accent rounded"
            :aria-label="expandedSubagentIds.has(sa.id) ? 'Collapse subagent output' : 'Expand subagent output'"
            data-testid="subagent-expand-toggle"
            @click.stop="toggleSubagentExpand(sa.id)"
          >
            {{ expandedSubagentIds.has(sa.id) ? '▲' : '▼' }}
          </button>
        </div>
      </div>
    </div>

    <!-- Last BTW (optional) -->
    <div v-if="agent.lastBtw" class="relative z-10 border-t border-line px-3 py-2 flex flex-col gap-1 text-[12px] font-mono shrink-0" @click.stop>
      <div class="text-fg-mute border-l-2 border-warning-line pl-2 whitespace-nowrap overflow-hidden text-ellipsis">
        {{ agent.lastBtw.message }}
      </div>
      <div v-if="agent.lastBtw.response" class="text-fg-mute border-l-2 border-warning-line pl-2 whitespace-nowrap overflow-hidden text-ellipsis">
        {{ agent.lastBtw.response }}
      </div>
      <div v-else class="text-fg-mute pl-2.5" style="animation: pulse 2s ease-in-out infinite;">
        ...
      </div>
    </div>

    <!-- Prompt input: always bottom-docked -->
    <PromptInput v-if="!agent.machine" :agent="agent" variant="compact" class="relative z-10 shrink-0" @click.stop @keydown.enter.stop @keydown.space.stop />
  </AppCard>
</template>
```

- [ ] **Step 4: Run to verify it passes**

Run: `pnpm test -- AgentCard`
Expected: PASS (all 8 cases). If the click test fails (the overlay button isn't catching body clicks in jsdom), the fix is to also forward clicks from the card root: add `@click="$emit('select', agent)"` to the `<AppCard>` element AND remove the `@click` from the overlay button (keep the button only as the keyboard/focus target with `@keydown.enter.prevent` / `@keydown.space.prevent` emitting select) so select is emitted exactly once. Re-run to PASS.

- [ ] **Step 5: Typecheck + lint the changed files**

Run: `pnpm typecheck && pnpm lint`
Expected: clean. Fix any antfu lint issues (no semicolons, import order) in the two changed/new files.

- [ ] **Step 6: Commit**

```bash
git add src/components/AgentCard.vue src/components/__tests__/AgentCard.test.ts
git commit --no-gpg-sign --no-verify -m "feat: agent card output fallback, prominent project, metric popover, bottom-docked prompt"
```

---

## Task 4: `AgentModal.vue` — labeled uptime / last activity / burn rate rows

**Files:**
- Modify: `src/components/AgentModal.vue`

The modal already has labeled "Cache write" / "Cache read" rows (around lines 145–156) but shows uptime in an unlabeled summary line. Add labeled rows so the full breakdown lives in the modal (the card's ⓘ is a quick peek; the modal is the source of truth).

- [ ] **Step 1: Locate the metrics block.** Run `grep -n "Cache write\|Cache read\|formatUptime\|<dl\|grid" src/components/AgentModal.vue` and open the surrounding labeled-rows block (the one containing "Cache write"/"Cache read").

- [ ] **Step 2: Add labeled rows** for **Uptime**, **Last activity**, and **Burn rate** in the same labeled-rows block, matching the existing row markup. Import the helpers if not already imported — add `formatBurnRate`, `formatRelativeActivity`, `secondsSince`, `formatUptime` to the existing `../utils/format` import, and `useNow` from `../composables/useNow`. In `<script setup>` add:

```ts
const { nowMs } = useNow()
const lastActivityLabel = computed(() => formatRelativeActivity(secondsSince(props.agent.lastActivity, nowMs.value)))
const burnLabel = computed(() => formatBurnRate(props.agent.costEstimate, props.agent.uptime))
```

(Use the modal's existing prop name for the agent — it may be `props.agent` or a destructured `agent`; match what the file uses. If the modal guards `agent` as possibly-null, guard these computeds the same way the file already guards other agent-derived values.)

Then add three rows in the labeled block, mirroring the existing Cache-write row's classes:

```vue
<div class="flex justify-between ...existing-row-classes...">
  <span ...label-classes...>Uptime</span>
  <span ...value-classes...>{{ formatUptime(agent.uptime) }}</span>
</div>
<div class="flex justify-between ...existing-row-classes...">
  <span ...label-classes...>Last activity</span>
  <span ...value-classes...>{{ lastActivityLabel }}</span>
</div>
<div v-if="burnLabel !== '—'" class="flex justify-between ...existing-row-classes...">
  <span ...label-classes...>Burn rate</span>
  <span ...value-classes...>{{ burnLabel }}</span>
</div>
```

Copy the exact wrapper/label/value classes from the existing "Cache write" row so the new rows visually match.

- [ ] **Step 3: Verify**

Run: `pnpm typecheck && pnpm lint && pnpm test -- AgentModal`
Expected: typecheck/lint clean; existing AgentModal tests still pass (no test asserts the absence of these rows).

- [ ] **Step 4: Commit**

```bash
git add src/components/AgentModal.vue
git commit --no-gpg-sign --no-verify -m "feat: labeled uptime/last-activity/burn rows in agent modal"
```

---

## Task 5: Docs + full verification

**Files:**
- Modify: `CHANGELOG.md`

- [ ] **Step 1: CHANGELOG.** Under `## [Unreleased]`:
  - `### Fixed`: "Agent cards no longer show 'No output yet' while an agent is actively working — the card now falls back to the current action / last tool when there is no assistant text yet."
  - `### Changed`: "Agent card redesign — prominent, readable project name; a compact cost · tokens · uptime metric row with full labeled detail (last activity, burn rate, cache costs) in a hover ⓘ popover and the agent modal; the prompt input is now always docked at the bottom with a larger output area."

- [ ] **Step 2: Full verification**

Run:
```bash
cd server && go build ./... && go test ./internal/parser/
cd .. && pnpm lint && pnpm typecheck && pnpm test
```
Expected: all green (incl. the new AgentCard/MetricsPopover tests and the parser guard).

- [ ] **Step 3: Manual smoke (optional).** If a dashboard instance is running with live agents: confirm a working tool-loop agent shows `▶ running: <tool>` instead of "No output yet"; clicking a card opens the agent modal; the project name is readable; hovering ⓘ shows the labeled breakdown; the prompt sits at the bottom on cards with and without subagents.

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md
git commit --no-gpg-sign --no-verify -m "docs: changelog for agent card UX"
```

---

## Self-Review Notes (resolved during authoring)

- **Spec coverage:** §A output fallback → Task 1 (backend guard) + Task 3 (frontend chain); §B click→modal → Task 3 (test-driven; overlay-button retained, root-`@click` fallback documented if jsdom click misses); §C project prominent → Task 3 (primary row, `flex-1` + title); §D 3-metric row + ⓘ popover → Task 2 (popover) + Task 3 (meta row) + Task 4 (modal detail); §E bottom-docked prompt → Task 3 (flex-column, `flex-1` body). §5 testing → tests in every task.
- **Reframe vs spec §A:** the spec proposed a backend change to "track last assistant text across the tail," but the parser already does this (`data.LastOutput` is only assigned on a `text` block and persists across messages). So the backend task is a regression guard, and the real fix is the frontend fallback — documented honestly in Task 1.
- **Type/testid consistency:** `data-testid`s used across tasks: `agent-card-open` (existing, retained), `agent-card-project`, `agent-card-info`, `agent-card-dismiss`, `metrics-popover`, `prompt-input` (stub), `active-subagents-block`/`subagent-*` (retained). All defined in Task 3's template / referenced in Task 3's test.
- **Known caveat (out of scope):** when the last assistant text falls outside the parser's 32 KB tail window (very large tool payloads), `lastOutput` is empty and the card shows `▶ running: <tool>` — correct and intended (activity, not stale text). Widening the tail window is a separate concern.
- **a11y:** the card keeps the sibling overlay button (not a wrapping `role=button`) to avoid nesting interactive controls; the ⓘ and dismiss are real buttons with `@click.stop`.
