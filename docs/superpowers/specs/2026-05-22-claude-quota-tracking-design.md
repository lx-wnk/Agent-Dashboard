# Claude Pro/Max Quota Tracking (CI-8)

**Date:** 2026-05-22
**Status:** SUPERSEDED — feature already shipped via simpler impl: `server/internal/api/system/quota.go` reads `~/.claude/usage-data/*.json` directly (relies on Claude CLI writing the quota file) + `GET /api/quota` + `App.vue` header chip with severity tiers. Spec proposes a heavier alternative (per-plan table, session-meta aggregation, cache-token weighting) — keep here as alt-impl reference if the simpler version proves insufficient.
**Roadmap ref:** `2026-05-09-agent-dashboard-unified-roadmap-design.md` → CI-8 (P3)

## Problem

Claude Pro/Max subscribers run into rolling token quotas. The dashboard currently shows per-session cost but no aggregate quota progress against the active subscription window. Users only learn they hit the cap when an agent fails mid-flow with a quota error (already caught by HD-1's `errorState: 'quota_exhausted'`, but only *after* failure).

`~/.claude/usage-data/session-meta/{sessionId}.json` files contain per-session token totals and timestamps. Aggregating those across the current subscription window produces a quota progress bar with no cooperation from the Claude CLI itself.

## Goals

- `GET /api/quota` returns `{ window: {start, end, source}, usage: {input, output, cache, totalEffective}, limit?, percent? }`.
- Header bar in dashboard shows a horizontal progress bar with current/limit and a tooltip breakdown.
- Graceful no-op when `~/.claude/usage-data/` is absent or empty (header chip hidden).
- User-overridable plan via `DASHBOARD_CLAUDE_PLAN` env var: `none|pro|max5|max20` setting the limit baseline.

## Non-Goals

- No write/refund operations. Read-only.
- No daily/hourly subdivisions in v1 — CI-5 (heatmap) and CI-6 (forecast) already cover that ground.
- No notifications when approaching cap. Browser-push warning is a follow-up.
- No multi-machine federation. Local files only.
- No backfill from JSONL session transcripts when session-meta files are missing — those events stay invisible to quota.

## Decisions

| # | Question | Choice |
|---|---|---|
| 1 | Window length | 5 hours rolling (Claude Pro standard). Configurable via env. |
| 2 | Limit source | Hardcoded plan table + env override |
| 3 | Refresh cadence | Endpoint computes on demand; frontend polls every 60s |
| 4 | UI location | Header chip — left of theme toggle in `App.vue` |
| 5 | Missing data behavior | Endpoint returns `200 { available: false }`; frontend hides chip |
| 6 | Cache token weighting | Use `cache_creation_input_tokens` at 1.25× rate per Anthropic docs; `cache_read_input_tokens` at 0.1× |

## Section A — Backend Surface

### A.1 Plan Table

`server/internal/quota/plans.go`:

```go
type Plan struct {
    Name      string
    WindowDur time.Duration
    Limit     int  // effective tokens
}

var Plans = map[string]Plan{
    "none":  {Name: "none",  WindowDur: 5 * time.Hour, Limit: 0},        // → no limit shown
    "pro":   {Name: "pro",   WindowDur: 5 * time.Hour, Limit: 45_000},
    "max5":  {Name: "max5",  WindowDur: 5 * time.Hour, Limit: 225_000},
    "max20": {Name: "max20", WindowDur: 5 * time.Hour, Limit: 900_000},
}
```

(Exact numbers are best-known approximations and stay in the file as the user-tunable knob — env var `DASHBOARD_CLAUDE_PLAN_LIMIT` overrides regardless.)

### A.2 Session-Meta Aggregator

`server/internal/quota/aggregator.go`:

```go
type Snapshot struct {
    Available bool        `json:"available"`
    Plan      string      `json:"plan,omitempty"`
    Window    Window      `json:"window,omitempty"`
    Usage     UsageTotals `json:"usage,omitempty"`
    Limit     int         `json:"limit,omitempty"`     // 0 = unknown
    Percent   float64     `json:"percent,omitempty"`   // 0..1; 0 if limit unknown
}

func ComputeSnapshot(ctx context.Context, now time.Time, plan Plan) (Snapshot, error)
```

Walks all `~/.claude/usage-data/session-meta/*.json` files across `parser.AllClaudeConfigDirs()`. Filters by mtime ≥ `now - plan.WindowDur`. Sums tokens with the cache-weighted formula. Returns `Available: false` if zero files matched or directory missing.

### A.3 HTTP Handler

`server/internal/api/quota/handler.go`:

`GET /api/quota` — uses request context, calls `ComputeSnapshot(ctx, time.Now(), planFromEnv())`. Cached in-memory for 30 seconds (sync.Map keyed by `planName`).

## Section B — Frontend Surface

### B.1 Composable

`src/composables/useQuota.ts` — polls `/api/quota` every 60s with `setInterval`, exposes `Ref<Snapshot | null>`. Suspends polling when `document.hidden` (mirrors `useAgents.ts` pattern).

### B.2 Component

`src/components/QuotaChip.vue` — header chip. Hidden when `snapshot?.available !== true`. When available, renders:

- Compact: emoji icon + `{percent%}` text + 80px horizontal progress bar
- Color tiers: <60% green, 60–85% amber, >85% red
- Hover tooltip: input/output/cache breakdown + window start/end + plan name

### B.3 Wire into header

`App.vue` adds `<QuotaChip />` to the existing header bar, positioned left of the theme toggle. No layout shift on missing data (component renders nothing when hidden).

## Section C — Env Vars

| Var | Purpose |
|---|---|
| `DASHBOARD_CLAUDE_PLAN` | `none|pro|max5|max20`, default `none`. Disables chip when `none`. |
| `DASHBOARD_CLAUDE_PLAN_LIMIT` | Integer override of the plan's `Limit` field. Useful when Anthropic adjusts caps. |
| `DASHBOARD_CLAUDE_PLAN_WINDOW_HOURS` | Float, default `5`. Overrides window length. |

All three documented in `.agent-context/conventions.md`.

## Section D — Layering

| Package | Allowed deps |
|---|---|
| `server/internal/quota` | `parser`, `sdk`, `node:*` equivalents — no DB, no pipeline, no api |
| `server/internal/api/quota` | `internal/quota`, `auth` |

Composition root (`server/cmd/serve/di.go`) wires the handler into the existing protected router group.

## Section E — Testing

- `server/internal/quota/aggregator_test.go` — table-driven against fixture session-meta JSON files in `testdata/`. Cover: empty dir, single file inside window, file outside window, multiple files cache-weighted sum.
- `server/internal/api/quota/handler_test.go` — request shape, default plan, env override, cache TTL behaviour.
- `src/composables/useQuota.test.ts` — polling start/stop, visibility pause, fetch error → snapshot stays null.
- `src/components/QuotaChip.test.ts` — hidden when unavailable, tier colors, tooltip content.

## Acceptance

User sets `DASHBOARD_CLAUDE_PLAN=pro`, restarts server. Opens dashboard. Header shows chip "🪙 23% · 10,350/45,000". Tooltip shows input/output/cache split + window times. Run an agent that consumes more tokens. Within 60s, chip updates. Set `DASHBOARD_CLAUDE_PLAN=none`. Chip disappears.
