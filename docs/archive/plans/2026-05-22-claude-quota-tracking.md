# Claude Pro/Max Quota Tracking (CI-8) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` or `superpowers:executing-plans`. Steps use checkbox (`- [ ]`) syntax.
> **Source spec:** `docs/superpowers/specs/2026-05-22-claude-quota-tracking-design.md`

**Goal:** Aggregate `~/.claude/usage-data/session-meta/*.json` into a rolling-window quota snapshot exposed at `GET /api/quota`, and render a header chip with progress bar.

**Tech Stack:** Go 1.26 (chi, slog), Vue 3 `<script setup>` + TypeScript, Vitest.

---

## File Structure

| File | Responsibility |
|---|---|
| `server/internal/quota/plans.go` (create) | Plan table + env helpers |
| `server/internal/quota/aggregator.go` (create) | `ComputeSnapshot` |
| `server/internal/quota/aggregator_test.go` (create) | Aggregator tests |
| `server/internal/quota/testdata/*.json` (create) | Session-meta fixtures |
| `server/internal/api/quota/handler.go` (create) | HTTP handler + 30s cache |
| `server/internal/api/quota/handler_test.go` (create) | Handler tests |
| `server/internal/api/router.go` (modify) | Route registration |
| `server/cmd/serve/di.go` (modify) | Wire handler |
| `sdk/types.go` (modify) | Add `QuotaSnapshot` + sub-types |
| `src/sdk.generated.ts` (regen) | tygo regen |
| `src/composables/useQuota.ts` (create) | Polling composable |
| `src/composables/useQuota.test.ts` (create) | Composable tests |
| `src/components/QuotaChip.vue` (create) | Header chip |
| `src/components/QuotaChip.test.ts` (create) | Chip tests |
| `src/App.vue` (modify) | Mount `<QuotaChip />` in header |
| `.agent-context/conventions.md` (modify) | Document three new env vars |

---

### Task 1: Plan table + env helpers

- [ ] **Step 1: Create `server/internal/quota/plans.go`**

Define `Plan` struct + `Plans` map (`none`, `pro`, `max5`, `max20`). Add `PlanFromEnv() Plan` reading `DASHBOARD_CLAUDE_PLAN`, `DASHBOARD_CLAUDE_PLAN_LIMIT`, `DASHBOARD_CLAUDE_PLAN_WINDOW_HOURS`. Default plan is `none`.

- [ ] **Step 2: Unit test env precedence**

`plans_test.go` covers: defaults, env override of name, env override of limit only, env override of window-hours.

---

### Task 2: Aggregator + fixtures

- [ ] **Step 1: Create fixtures `server/internal/quota/testdata/`**

3–4 session-meta JSON files mirroring the real shape (look at `parser.go::loadSessionMeta` for the canonical schema; mirror exactly so the aggregator can decode via the same struct). Vary timestamps: one inside window, one outside, one with high cache tokens.

- [ ] **Step 2: Implement `ComputeSnapshot`**

```go
func ComputeSnapshot(ctx context.Context, now time.Time, plan Plan) (Snapshot, error) {
    dirs := parser.AllClaudeConfigDirs()
    // for each dir: list usage-data/session-meta/*.json, filter mtime ≥ now-WindowDur
    // decode each, sum tokens with weights:
    //   total += input + output + cacheCreation*1.25 + cacheRead*0.1
    // if total == 0 and zero files matched → Available:false
}
```

Sequential walk is fine — directories are tiny. Honour `ctx.Done()` between files. Skip parse errors with `slog.Warn`.

- [ ] **Step 3: Aggregator tests**

Table-driven `aggregator_test.go`. Cases: empty dir → unavailable; one in-window file → expected sum; in+out window mix → only in-window summed; multi-dir aggregation (set `DASHBOARD_CLAUDE_CONFIG_DIRS` via `t.Setenv`).

---

### Task 3: HTTP handler

- [ ] **Step 1: Handler with TTL cache**

`server/internal/api/quota/handler.go`:

```go
type Handler struct {
    mu      sync.Mutex
    cache   map[string]cachedSnapshot
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
    plan := quota.PlanFromEnv()
    if cs, ok := h.lookup(plan.Name); ok && time.Since(cs.at) < 30*time.Second {
        writeJSON(w, cs.snap); return
    }
    snap, err := quota.ComputeSnapshot(r.Context(), time.Now(), plan)
    // ... on err: 500 + slog; cache; respond
}
```

- [ ] **Step 2: Handler tests**

`handler_test.go`: 200 happy path with fake aggregator; cache hit avoids second compute; unavailable response shape.

- [ ] **Step 3: Wire route**

`router.go` adds `r.Get("/api/quota", apierr.ErrorMiddleware(deps.QuotaHandler.Get))` inside the JWT-protected group. `di.go` adds `QuotaHandler` to the bundle. Run `task wire`.

---

### Task 4: SDK types

- [ ] **Step 1: Add types to `sdk/types.go`**

Match Spec §A.2 shape with explicit camelCase JSON tags.

- [ ] **Step 2: Regen TS**

`task gen-types`. Commit `src/sdk.generated.ts` diff. `pnpm typecheck`.

---

### Task 5: Frontend composable

- [ ] **Step 1: `src/composables/useQuota.ts`**

Poll `/api/quota` every 60s via `setInterval`. Pause when `document.hidden` (visibilitychange listener). Resume immediately on focus. Expose `snapshot: Ref<QuotaSnapshot | null>` + `error: Ref<string | null>`.

- [ ] **Step 2: Composable tests**

`useQuota.test.ts`: mock fetch with timers, assert poll cadence, assert pause on `visibilitychange`.

---

### Task 6: Chip component

- [ ] **Step 1: `src/components/QuotaChip.vue`**

Returns nothing when `snapshot?.available !== true`. Otherwise renders the compact bar + percent text + tooltip. Tier color via `:class="{green/amber/red}"` derived from `percent`. Tooltip via `<div role="tooltip">` shown on hover/focus — keyboard accessible.

- [ ] **Step 2: Chip tests**

`QuotaChip.test.ts`: unavailable → renders nothing; available → renders bar + percent; tier classes flip at 60% / 85%.

- [ ] **Step 3: Wire into header**

`App.vue` imports `QuotaChip` and mounts left of the theme toggle. Verify no layout shift on initial null state.

---

### Task 7: Conventions + memory

- [ ] **Step 1: Update `.agent-context/conventions.md`**

Append three rows to the Pipeline Env Vars table (or a new "Quota" section): `DASHBOARD_CLAUDE_PLAN`, `DASHBOARD_CLAUDE_PLAN_LIMIT`, `DASHBOARD_CLAUDE_PLAN_WINDOW_HOURS`.

- [ ] **Step 2: Memory updates**

Mark CI-8 done in `.agent-context/memory/todo.md`. Append entry to `.agent-context/memory/log.md`.

---

### Task 8: Manual verify

- [ ] **Step 1: Smoke with no plan**

`task dev`. Confirm header has no chip (default `none`).

- [ ] **Step 2: Smoke with plan**

`DASHBOARD_CLAUDE_PLAN=pro task dev`. Confirm chip renders with current usage. Hover → tooltip shows input/output/cache breakdown.

- [ ] **Step 3: Smoke missing data**

Rename `~/.claude/usage-data/session-meta/` temporarily; chip disappears within 60s. Restore directory.

---

## Risk Register

| Risk | Mitigation |
|---|---|
| Anthropic changes plan limits | Limits live in `plans.go`; env override exists for hot-fix |
| Session-meta schema drift | Aggregator imports the existing `sdk.SessionMeta` struct via `parser` package — schema changes break compile, not silently |
| Multiple config dirs (DASHBOARD_CLAUDE_CONFIG_DIRS) | Walk `parser.AllClaudeConfigDirs()`; already covered in aggregator |
| Aggregator slow on huge `session-meta` dirs | Walk is one `os.ReadDir` + small JSON parses; profile if dir exceeds 10k files |
| Cache token weighting wrong | Weights live as constants in `aggregator.go`; trivial to adjust |
