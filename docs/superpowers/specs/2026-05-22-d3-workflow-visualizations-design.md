# D3 Workflow Visualizations (VA-2)

**Date:** 2026-05-22
**Status:** Approved (brainstorm)
**Roadmap ref:** `2026-05-09-agent-dashboard-unified-roadmap-design.md` → VA-2 (P2)

## Problem

The roadmap promises four on-demand D3 visualizations to make agent behavior visually legible: tool-call **Sankey**, session **DAG**, **spawn-tree**, and tool **co-occurrence** matrix. Phase 4 implementation plan silently dropped VA-2 — only VA-1 (Waterfall), VA-3 (DependencyGraph), CI-5 (Heatmap) and CI-6 (Forecast) shipped. The four visualization endpoints and the "Workflows" view toggle are still missing.

The competitive analysis (`memory/project_competitive_analysis.md` 2026-05-19) flagged this category as a differentiator — competitors offer flat lists; we already have D3 in the dependency graph and waterfall, so the marginal cost of four more is the data shaping, not the rendering.

The underlying JSONL data is already parsed at scan time (`server/internal/parser/parser.go::ParseSessionFile`) and again for ngram analytics (`server/internal/analytics/ngrams.go`). VA-2 reuses both paths.

## Goals

- Four read-only endpoints under `GET /api/visualizations/*` returning compact JSON for D3 to render.
- One new view-toggle entry "Workflows" in `App.vue` selector — sits next to Agents / Tasks / Memory.
- Four lazy-loaded chart components, each with empty/loading/error states matching `DependencyGraph.vue`.
- Sessions filter (single session) and date range filter applied at endpoint level — no client-side post-filter.
- Endpoints respect existing JWT auth + scoping; data limited to sessions the requesting user can read.

## Non-Goals

- No materialized views or background pre-aggregation. All endpoints are on-demand. (Confirmed by roadmap §5 "Shared Concerns: D3 visualization endpoints are on-demand only".)
- No cross-machine federation in v1. The "remote dashboards" proxy stays out of scope — these are local-only stats.
- No edit/interact capabilities on the charts beyond hover/click navigation. Visualizations are diagnostic, not control planes.
- No write path to a `workflow_patterns` table — the existing ngram analytics (NE-3) already covers persisted pattern discovery. VA-2 stays ephemeral.
- No new chart for sub-agent timelines beyond what spawn-tree provides.
- No PDF/PNG export in v1. SVG download stays a follow-up.

## Decisions

| # | Question | Choice |
|---|---|---|
| 1 | Endpoint shape | `GET /api/visualizations/{type}?session={id}&from={ts}&to={ts}` returning typed JSON per type |
| 2 | Data source | Re-parse JSONL on demand via `parser.ParseSessionFile`; reuse `analytics` package for shared scanning |
| 3 | Frontend view | New "Workflows" entry in the existing view-toggle in `App.vue` — single page with chart-type tabs |
| 4 | D3 library | Reuse existing `d3` dependency (already in `package.json` for VA-3) |
| 5 | Empty-state UX | Show "No tool calls found in selected window" with link to broaden range |
| 6 | Auth scope | Same JWT requirement as `/api/agents` — no new scope tier |
| 7 | Tests | Backend: data-shaping unit tests against fixtures in `server/internal/analytics/testdata/`. Frontend: component mounting + empty-state tests (Vitest), no D3-render snapshot |

## Section A — Endpoint Surface

All four endpoints live under a new sub-package `server/internal/api/visualizations/`. Each accepts:

- `session` (optional) — restrict to a single session ID. Omitted → aggregate across all sessions reachable by `parser.AllClaudeConfigDirs`.
- `from` (optional, RFC3339) — lower-bound timestamp on JSONL message timestamps.
- `to` (optional, RFC3339) — upper-bound.

Default time window when `from`/`to` omitted: last 7 days. Hard cap on aggregate mode: 20 sessions, sorted by recency.

### A.1 `GET /api/visualizations/sankey`

Tool-call Sankey: source→target flow between consecutive `tool_use` entries within a session.

Response:
```ts
{
  nodes: Array<{ id: string, name: string }>,
  links: Array<{ source: string, target: string, value: number }>,
  meta: { sessionCount: number, callCount: number }
}
```

### A.2 `GET /api/visualizations/dag`

Session DAG: nodes = tool calls + assistant turns within one session (session param REQUIRED — endpoint 400s otherwise). Edges = chronological succession + matching `tool_use_id` → `tool_result` pairs.

Response:
```ts
{
  nodes: Array<{ id: string, type: 'tool' | 'assistant' | 'user', label: string, ts: string }>,
  links: Array<{ source: string, target: string, kind: 'chrono' | 'result' }>
}
```

### A.3 `GET /api/visualizations/spawn-tree`

Spawn tree: nodes = sessions, edges = parent→child sub-agent spawns. Built from `~/.claude/projects/{encoded}/{sessionId}/subagents/*.jsonl` files (same source as `merger.SubAgents`).

Response:
```ts
{
  roots: string[],
  nodes: Array<{ id: string, label: string, depth: number, toolCount: number, costCents: number }>,
  links: Array<{ source: string, target: string }>
}
```

### A.4 `GET /api/visualizations/co-occurrence`

Tool co-occurrence matrix: symmetric matrix of how often two tools appear in the same session.

Response:
```ts
{
  tools: string[],
  matrix: number[][],  // tools.length × tools.length, matrix[i][j] = sessions containing both tools[i] and tools[j]
  meta: { sessionCount: number }
}
```

## Section B — Data Shaping (analytics package)

New file `server/internal/analytics/visualizations.go`. Public funcs:

```go
func BuildSankey(ctx context.Context, sessions []string) (SankeyData, error)
func BuildDAG(ctx context.Context, sessionPath string) (DAGData, error)
func BuildSpawnTree(ctx context.Context, sessions []string) (SpawnTreeData, error)
func BuildCoOccurrence(ctx context.Context, sessions []string) (CoOccurrenceData, error)
```

All four reuse `parser.ParseSessionFile` and the existing tool-name regex from `ngrams.go` (`toolNameRE`) — extract that constant to `analytics/common.go` so both `ngrams.go` and `visualizations.go` import it (SSOT per `layer2-project-core.md`).

Shared scanning helper extracted into `analytics/scan.go`:

```go
// scanSessionsForTools walks the configured Claude dirs and returns
// session-keyed tool-call timelines under the supplied bounds. Honors
// maxFileSize cap from ngrams.go and respects ctx cancellation.
func scanSessionsForTools(ctx context.Context, opts ScanOpts) (map[string][]ToolCall, error)
```

`BuildSankey`, `BuildSpawnTree`, `BuildCoOccurrence` consume `scanSessionsForTools` results. `BuildDAG` reads a single file end-to-end (no aggregation) so it bypasses the helper.

## Section C — Frontend

### C.1 New components

- `src/components/WorkflowsView.vue` — top-level: chart-type tabs, session/date filters, lazy mounts the selected chart.
- `src/components/visualizations/SankeyChart.vue` — d3-sankey rendering.
- `src/components/visualizations/SessionDagChart.vue` — d3-force layout.
- `src/components/visualizations/SpawnTreeChart.vue` — d3-hierarchy tree layout.
- `src/components/visualizations/CoOccurrenceMatrix.vue` — d3 matrix heatmap.

Each chart component mirrors the pattern from `DependencyGraph.vue`: `svgRef`, `loading`/`error` refs, `fetchAndRender()` inside `onMounted` + `watch(props)`, and a teardown on unmount.

### C.2 Composable

`src/composables/useWorkflows.ts` — wraps the four fetches behind a single API:

```ts
function useWorkflows(filters: Ref<WorkflowsFilters>) {
  return {
    sankey: AsyncRef<SankeyData>,
    dag: AsyncRef<DagData>,
    spawnTree: AsyncRef<SpawnTreeData>,
    coOccurrence: AsyncRef<CoOccurrenceData>,
  }
}
```

The composable owns the abort controllers — switching tabs cancels in-flight requests. Empty/loading/error state is per-chart.

### C.3 View toggle

`App.vue` adds `'workflows'` to its `viewMode` union. Selector picks up the new entry automatically since it iterates over a list. No router changes — same modal-less SPA pattern as the dependency graph.

### C.4 New TS types

`src/types.ts` adds:

```ts
export interface SankeyData { /* mirrors response above */ }
export interface DagData { /* ... */ }
export interface SpawnTreeData { /* ... */ }
export interface CoOccurrenceData { /* ... */ }
```

Where possible these flow from `sdk/types.go` via tygo (`gen-types`). If a type carries TS-only computed fields (e.g. cached d3 hierarchy node) it stays manual — matches the `Agent` exception documented in `2026-05-19-agent-tygo-migration-design.md`.

## Section D — Routing + Layering

### D.1 Router registration

Add to `server/internal/api/router.go`:

```go
import "github.com/lx-wnk/agent-dashboard/server/internal/api/visualizations"
// ...
r.Get("/api/visualizations/sankey", apierr.ErrorMiddleware(visualizations.Sankey))
r.Get("/api/visualizations/dag", apierr.ErrorMiddleware(visualizations.DAG))
r.Get("/api/visualizations/spawn-tree", apierr.ErrorMiddleware(visualizations.SpawnTree))
r.Get("/api/visualizations/co-occurrence", apierr.ErrorMiddleware(visualizations.CoOccurrence))
```

All four sit inside the existing JWT-protected route group — no new auth surface.

### D.2 Layer compliance

Per `task-pipeline.md` § Go Layer Direction:

- `api/visualizations/*` → may import `analytics`, `parser`, `sdk`. No `pipeline`, no `mcp`, no `db/ent`.
- `analytics/visualizations.go` → may import `parser`, `sdk`. No `api`, no `pipeline`, no `db/ent`.

This matches the existing analytics+ngrams layering — no new violations introduced.

## Section E — Performance Guards

- Hard cap: at most 20 sessions aggregated per request. `from`/`to` collapsing to <1 hour windows is permitted.
- Per-file size guard: `maxFileSize = 10 MB` (reused from `ngrams.go`).
- Endpoint timeout: 5 seconds via `http.TimeoutHandler` wrapper in router.
- Co-occurrence response capped at 50 most-active tools — if more, response includes `tools` truncated + `meta.truncated: true`.

## Section F — Testing

- `server/internal/analytics/visualizations_test.go` — table-driven against `testdata/*.jsonl` fixtures. Cover: empty session, single tool, branching tool sequences, sub-agent spawn fan-out, missing optional fields.
- `server/internal/api/visualizations/handler_test.go` — request/response shape, error cases (400 on DAG without session, 500 on parser error path).
- `src/components/WorkflowsView.test.ts` — tab switching, empty filter result, error propagation.
- `src/composables/useWorkflows.test.ts` — abort behavior on filter change.
- No D3 snapshot tests — render output is implementation-detail. Behavior tests use jsdom mounting only.

## Section G — Open Items (deferred — capture, don't block)

- Permalink to a specific workflow view (would require router changes). Defer.
- WebSocket streaming for live spawn-tree updates. Defer — VA-2 stays static-snapshot.
- Filter chips for "only sessions with errors" / "only sessions with subagents". Worth adding once usage settles.

## Acceptance

A user opens the Workflows view, picks a date range, switches between the four chart types, and each renders within 1 second for a 7-day window on a developer machine with ~20 sessions. Hovering a Sankey link shows source→target call count. Clicking a spawn-tree node opens the corresponding `AgentModal`.
