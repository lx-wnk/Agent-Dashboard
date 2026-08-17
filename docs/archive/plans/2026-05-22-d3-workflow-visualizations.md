# D3 Workflow Visualizations (VA-2) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
> **Source spec:** `docs/superpowers/specs/2026-05-22-d3-workflow-visualizations-design.md`

**Goal:** Ship four on-demand D3 workflow visualizations (Sankey, session DAG, spawn-tree, co-occurrence matrix) behind a new "Workflows" view toggle, reading from existing JSONL parsers without persistence.

**Architecture:** New `server/internal/analytics/visualizations.go` shapes data; new `server/internal/api/visualizations/` package exposes four GET endpoints; new `src/components/WorkflowsView.vue` hosts chart-type tabs that lazy-mount four chart components under `src/components/visualizations/`. No DB writes, no pipeline coupling.

**Tech Stack:** Go 1.26 (chi router + existing JSONL parser), Vue 3 `<script setup>` + TypeScript, `d3` (already a dependency), `d3-sankey` (new transitive), Vitest + `@vue/test-utils`, Playwright.

---

## File Structure

| File | Responsibility |
|---|---|
| `server/internal/analytics/common.go` (create) | Extract `toolNameRE` + shared types — SSOT for analytics scanning |
| `server/internal/analytics/scan.go` (create) | `scanSessionsForTools` shared helper |
| `server/internal/analytics/visualizations.go` (create) | Four `Build*` data-shaping functions |
| `server/internal/analytics/visualizations_test.go` (create) | Table-driven tests against fixtures |
| `server/internal/analytics/testdata/*.jsonl` (create) | Minimal JSONL fixtures |
| `server/internal/api/visualizations/handler.go` (create) | Four chi handlers |
| `server/internal/api/visualizations/handler_test.go` (create) | HTTP-level shape + error tests |
| `server/internal/api/router.go` (modify) | Register four new routes |
| `sdk/types.go` (modify) | Add four response struct types — pickable by tygo |
| `src/sdk.generated.ts` (regen) | Auto-regenerate from `task gen-types` |
| `src/composables/useWorkflows.ts` (create) | Fetch composable with abort handling |
| `src/composables/useWorkflows.test.ts` (create) | Unit tests for abort + filter changes |
| `src/components/WorkflowsView.vue` (create) | Tabs + filters + lazy mount |
| `src/components/visualizations/SankeyChart.vue` (create) | d3-sankey render |
| `src/components/visualizations/SessionDagChart.vue` (create) | d3-force DAG render |
| `src/components/visualizations/SpawnTreeChart.vue` (create) | d3-hierarchy tree render |
| `src/components/visualizations/CoOccurrenceMatrix.vue` (create) | d3 matrix render |
| `src/components/WorkflowsView.test.ts` (create) | Tab switching, empty/error states |
| `src/App.vue` (modify) | Add `'workflows'` to view-mode union + selector entry |
| `package.json` (modify) | Add `d3-sankey` + `@types/d3-sankey` |
| `tests/e2e/workflows.spec.ts` (create) | Playwright happy path |

## Spec Section coverage map

| Spec section | Task |
|---|---|
| Section B — data shaping | Tasks 1, 2, 3 |
| Section A — endpoint surface | Task 4 |
| Section A.2 — DAG REQUIRES session param (400 otherwise) | Task 4 |
| Section D — router + layering | Task 5 |
| Section C.4 — TS types via tygo | Task 6 |
| Section C.2 — composable | Task 7 |
| Section C.1 — chart components | Tasks 8, 9, 10, 11 |
| Section C.3 — view toggle | Task 12 |
| Section E — performance guards | Task 13 |
| Section F — testing | Tasks 1–14 (interleaved) |
| Section G — acceptance E2E | Task 14 |

---

### Task 1: Extract analytics SSOT + fixtures

**Files:**
- Create: `server/internal/analytics/common.go`
- Modify: `server/internal/analytics/ngrams.go` — import `toolNameRE` from `common.go` instead of declaring locally
- Create: `server/internal/analytics/testdata/single-session-linear.jsonl`
- Create: `server/internal/analytics/testdata/two-sessions-branching.jsonl`
- Create: `server/internal/analytics/testdata/spawn-tree-fanout.jsonl`

- [ ] **Step 1: Extract regex constant**

Move `toolNameRE` from `ngrams.go` to `common.go`. Update `ngrams.go` reference. Run `task test -- ./server/internal/analytics/...` — existing ngram tests must stay green (regression gate).

- [ ] **Step 2: Add fixtures**

Three minimal JSONL fixtures covering the chart-type matrix:
- `single-session-linear.jsonl` — assistant → tool_use(Read) → tool_use(Edit) → tool_use(Bash)
- `two-sessions-branching.jsonl` — two sessions, overlapping tool sets for co-occurrence
- `spawn-tree-fanout.jsonl` — parent session + two subagent jsonl fixtures referenced by path

Document each fixture's intended assertions inline as `// fixture-purpose: ...` JSON-line comments where the JSONL spec permits (most lines are pure JSON — put intent in a sibling `.md` next to the fixtures or in test comments).

---

### Task 2: Shared scanning helper

**Files:**
- Create: `server/internal/analytics/scan.go`
- Create: `server/internal/analytics/scan_test.go`

- [ ] **Step 1: Define `ScanOpts` + `ToolCall` types in `common.go`**

```go
type ScanOpts struct {
    Sessions []string          // explicit session IDs; empty → all sessions in window
    From, To time.Time         // zero values = unbounded
    MaxSessions int            // hard cap (default 20)
}

type ToolCall struct {
    SessionID string
    Name      string
    ID        string             // tool_use_id
    Timestamp time.Time
}
```

- [ ] **Step 2: Implement `scanSessionsForTools`**

Walk `parser.AllClaudeConfigDirs()`, locate JSONL files, filter by mtime against `opts.From/To`, sort by recency, take `MaxSessions`. For each, stream-parse and emit `ToolCall` per `tool_use` entry. Honor `opts.Sessions` allow-list. Respect `ctx.Done()`. Skip files exceeding `maxFileSize` with `slog.Warn`.

Reuse `parser.ParseSessionFile` where possible to avoid a second JSONL grammar in this package — if `ParseSessionFile` doesn't already surface tool-use timestamps, extend it with a `ToolUses` slice on its return struct (additive — won't break existing callers).

- [ ] **Step 3: Tests**

`scan_test.go` against the three fixtures. Assert: session count caps respected, time bounds applied, files exceeding cap skipped.

---

### Task 3: `Build*` data-shaping functions

**Files:**
- Create: `server/internal/analytics/visualizations.go`
- Create: `server/internal/analytics/visualizations_test.go`

- [ ] **Step 1: `BuildSankey`**

Per session, emit a link for each consecutive `(prev.Name → curr.Name)` tool pair. Aggregate identical links by `value++`. Nodes are deduped tool names. Return `SankeyData` (defined in `sdk/types.go` — Task 6).

Test against `single-session-linear.jsonl`: 3 links (Read→Edit, Edit→Bash all value=1), 3 nodes.

- [ ] **Step 2: `BuildDAG`**

Parse one session end-to-end (`opts.Sessions` length must be exactly 1; else return `errors.New("dag requires one session")`). Walk JSONL, emit chronological links between consecutive non-system entries; emit `kind: 'result'` links between `tool_use` and matching `tool_result` by ID.

Test against `single-session-linear.jsonl`: 4 nodes (3 tool + 1 assistant turn or however the fixture is shaped — adapt assertion to fixture content), correct edge kinds.

- [ ] **Step 3: `BuildSpawnTree`**

Walk `~/.claude/projects/{encoded}/{sessionId}/subagents/*.jsonl` for each session via the existing path helper used in `merger.SubAgents` (extract if private). Each subagent JSONL gets a node; parent edge derived from directory ownership. Depth computed via BFS from roots.

Test against `spawn-tree-fanout.jsonl`: 1 root, 2 children, depth `{root:0, child:1}`.

- [ ] **Step 4: `BuildCoOccurrence`**

For each session, collect distinct tool names. For each pair `(i, j)` with `i != j` (or `i == j` for diagonal = sessions using tool), increment matrix cell. Cap `tools` at 50 most-active (by session count) — set `meta.truncated=true` when capped.

Test against `two-sessions-branching.jsonl`: assert symmetric matrix, diagonal counts equal session-with-tool counts, cap behavior at 50-tool boundary (use a separate fixture or synthesize tools in test).

---

### Task 4: HTTP handlers

**Files:**
- Create: `server/internal/api/visualizations/handler.go`
- Create: `server/internal/api/visualizations/handler_test.go`

- [ ] **Step 1: Handler struct + four methods**

`Handler` carries no state. Methods `Sankey`, `DAG`, `SpawnTree`, `CoOccurrence` all parse `?session=`, `?from=`, `?to=` query params, build `ScanOpts`, call the matching `analytics.Build*`, JSON-encode response.

`DAG` returns 400 if `session` param is absent or contains commas (DAG is single-session).

Default time window when bounds omitted: `time.Now().Add(-7*24*time.Hour)` → `time.Now()`.

- [ ] **Step 2: Route registration (deferred to Task 5)**

- [ ] **Step 3: Handler tests**

Stand up `httptest.NewRecorder()` per case. Cover:
- 200 happy path on sankey/spawn-tree/co-occurrence with no session param
- 400 on DAG missing session
- 200 on DAG with session
- 422 on malformed `from` timestamp
- response shape matches the `SankeyData` etc. structs defined in `sdk/types.go`

---

### Task 5: Wire routes + verify layering

**Files:**
- Modify: `server/internal/api/router.go`

- [ ] **Step 1: Register routes inside the existing JWT-protected group**

```go
import "github.com/lx-wnk/agent-dashboard/server/internal/api/visualizations"
// inside the protected sub-router:
r.Get("/api/visualizations/sankey", apierr.ErrorMiddleware(deps.VisualizationsHandler.Sankey))
r.Get("/api/visualizations/dag", apierr.ErrorMiddleware(deps.VisualizationsHandler.DAG))
r.Get("/api/visualizations/spawn-tree", apierr.ErrorMiddleware(deps.VisualizationsHandler.SpawnTree))
r.Get("/api/visualizations/co-occurrence", apierr.ErrorMiddleware(deps.VisualizationsHandler.CoOccurrence))
```

Add `VisualizationsHandler` to the relevant `*Deps` struct in `di.go` (or wherever Wire builds the HTTP handler bundle) — match the pattern used by `SearchHandler`.

- [ ] **Step 2: Wire DI**

`task wire` to regenerate. Confirm no manual edits to `wire_gen.go`.

- [ ] **Step 3: Layer-direction lint**

Run `task lint`. The layering rules in `task-pipeline.md` § Go Layer Direction must hold: `api/visualizations/` may import `analytics`, `parser`, `sdk` — but not `pipeline/`, `db/ent`, `mcp/`.

---

### Task 6: SDK types + tygo regen

**Files:**
- Modify: `sdk/types.go`
- Regen: `src/sdk.generated.ts`

- [ ] **Step 1: Add response types to `sdk/types.go`**

Match the JSON contracts in spec §A.1–A.4. Use `json:""` tags on every field — the camelCase mapping must be explicit (per project convention `EnrichedTask explicit camelCase JSON serialization`, commit `0f86ab1`).

- [ ] **Step 2: Regen TS**

`task gen-types` (or whatever the tygo task is — check `Taskfile.yml`). Commit the `src/sdk.generated.ts` diff. Run `pnpm typecheck`.

---

### Task 7: `useWorkflows` composable

**Files:**
- Create: `src/composables/useWorkflows.ts`
- Create: `src/composables/useWorkflows.test.ts`

- [ ] **Step 1: Implement composable with `AbortController`**

```ts
export interface WorkflowsFilters {
  sessionId?: string
  from?: string
  to?: string
}

export function useWorkflows(filters: Ref<WorkflowsFilters>) {
  // Owns four AbortControllers (one per chart type).
  // watch(filters, deep) → abort all in-flight → re-fetch only the active tab.
  // Returns four `AsyncRef<T>` { data, loading, error } objects.
}
```

- [ ] **Step 2: Tests**

`useWorkflows.test.ts` mocks `fetch`, asserts abort fires on filter change, asserts only the active tab refetches.

---

### Tasks 8–11: Chart components

Each chart follows the `DependencyGraph.vue` template: `svgRef`, `loading`/`error` refs, `fetchAndRender()` on mount + `watch(props)`, teardown on unmount.

#### Task 8 — `SankeyChart.vue`

- [ ] Add `d3-sankey` + `@types/d3-sankey` to `package.json`. Run `pnpm install`.
- [ ] Implement Sankey via `d3-sankey` layout. Hover shows source→target→value tooltip.
- [ ] Empty state: render "No tool calls found in this window." when `nodes.length === 0`.

#### Task 9 — `SessionDagChart.vue`

- [ ] Force-directed layout. Color nodes by `type` (`tool|assistant|user`). Edge style differs for `chrono` vs `result`.
- [ ] Tooltip on hover shows full label + timestamp.

#### Task 10 — `SpawnTreeChart.vue`

- [ ] `d3.hierarchy` + `d3.tree` layout. Node radius proportional to `toolCount`. Click emits `navigate` event with session ID.
- [ ] Wire `navigate` upward through `WorkflowsView.vue` to `App.vue` so clicking opens the `AgentModal` for that session.

#### Task 11 — `CoOccurrenceMatrix.vue`

- [ ] Square heatmap. Cell intensity by `matrix[i][j] / max`. Axis labels are tool names, rotated 45°.
- [ ] Show `meta.truncated` banner when truncation flag is set.

---

### Task 12: View toggle + WorkflowsView

**Files:**
- Create: `src/components/WorkflowsView.vue`
- Create: `src/components/WorkflowsView.test.ts`
- Modify: `src/App.vue`

- [ ] **Step 1: Build `WorkflowsView.vue`**

Filters bar (session combobox seeded from `/api/sessions`, date range picker) + tab strip (Sankey / DAG / Spawn Tree / Co-occurrence) + active-chart slot. Lazy-mount the chart component for the active tab; cache mounted instances so switching tabs doesn't re-fetch.

- [ ] **Step 2: Wire into `App.vue`**

Extend `viewMode` union with `'workflows'`. Add selector chip. Mount `WorkflowsView` when active.

- [ ] **Step 3: Tests**

Vitest: mount `WorkflowsView`, switch tabs, assert correct child chart visible. Mock `useWorkflows`; assert charts receive the right data ref.

---

### Task 13: Performance guards

**Files:**
- Modify: `server/internal/api/visualizations/handler.go`
- Modify: `server/internal/api/router.go`

- [ ] **Step 1: Wrap handlers with `http.TimeoutHandler` (5s)**
- [ ] **Step 2: Validate `MaxSessions` cap is enforced at the `ScanOpts` layer (default 20, no override yet)**
- [ ] **Step 3: Re-run all backend tests; add timeout-path test** that triggers a slow `scanSessionsForTools` via a context-aware fake and asserts 503.

---

### Task 14: E2E + manual verify

**Files:**
- Create: `tests/e2e/workflows.spec.ts`

- [ ] **Step 1: Playwright happy path**

Launch dev server, navigate to Workflows view, switch through all four tabs, assert SVG element present + non-empty for each (use a seeded local Claude config dir — extend the existing fixture in `tests/e2e/fixtures/` to include the JSONL fixtures from Task 1).

- [ ] **Step 2: Manual smoke**

`task dev`, open `:5173`, switch to Workflows, verify all four render within ~1s on the developer machine. Confirm acceptance criterion from spec § Acceptance.

- [ ] **Step 3: Update `memory/todo.md`**

Mark VA-2 → done. Re-rank remaining gaps (IP-1, CI-8). Append entry to `memory/log.md`.

---

## Sequencing Notes

- Tasks 1–6 (backend + types) are a self-contained slice. Land first via one PR. Frontend can stub against fake JSON during reviewer turnaround.
- Tasks 7–11 are parallelizable across four contributors; the four charts share no state.
- Task 13 (perf guards) should land in the backend PR — not deferred — to avoid a follow-up.
- Task 14 lands last, after frontend PR merges.

## Risk Register

| Risk | Mitigation |
|---|---|
| `parser.ParseSessionFile` lacks tool-use timestamps | Additive extension (Task 2 Step 2) — backward compatible |
| `d3-sankey` bundle size bloat | Acceptable — already shipping `d3`; sankey adds ~10KB minified |
| Slow scans on dev machines with 500+ sessions | `MaxSessions=20` cap + 5s timeout + `maxFileSize` cap from ngrams.go |
| Multiple Claude config dirs (`DASHBOARD_CLAUDE_CONFIG_DIRS`) breaking aggregation | Walk all dirs via `parser.AllClaudeConfigDirs()` (already used by ngrams) |
| Layer violation creep | Lint runs on PR; spec § D.2 documents the allow-list |
