# Agent Health/Anomaly Score (CI-7)

**Date:** 2026-06-04
**Status:** READY FOR IMPLEMENTATION
**Roadmap ref:** `2026-05-09-agent-dashboard-unified-roadmap-design.md` → CI-7 (P2)

## Problem

The dashboard shows per-agent cost and token counters but gives no composite signal about whether an agent is behaving well. A user monitoring ten parallel agents must read tool counts, error states, and cache ratios independently to detect a struggling session. There is no single "is this agent healthy?" indicator.

The `Agent.HealthScore` field and the `AgentCard.vue` chip that renders it already exist and are wired into the UI. `buildAgent` in `server/internal/merger/merger.go` never assigns the field, so it always transmits as `0` and the chip always shows `0/100` with a red tint.

## Goals

- `buildAgent` assigns a meaningful `HealthScore` (0–100 integer) every scan tick.
- The score is a composite of four measurable session signals available at merge time.
- No new API endpoints, no DB reads, no new frontend work required.
- `AgentCard.vue` chip color tiers use the score correctly (existing CSS already does this).

## Non-Goals

- No historical trending of the health score itself in v1.
- No per-component breakdown exposed in the API (composite integer only).
- No user-configurable weights; the formula is fixed in source.
- No health score for non-Claude providers whose token counts are zero (score returns 50 by convention — see edge cases below).
- No alerts or notifications when the score drops below a threshold (follow-up item).

## Decisions

| # | Question | Choice |
|---|---|---|
| 1 | Computation location | New `ComputeHealthScore` function in `server/internal/merger/` (new file `health.go`) |
| 2 | Call site | `buildAgent` — called with `session *parser.SessionData`, result assigned to `agent.HealthScore` |
| 3 | SDK type change | None — `Agent.HealthScore` is already `int` with `json:"healthScore"` |
| 4 | 7-day cost baseline | In-memory rolling window injected via `GetAgents` option (see Section B) — merger must not import `db/` |
| 5 | Zero-data default | Score = 50 when `ConversationTurns == 0` (no session content yet) |
| 6 | Non-Claude / cost-unknown | Score = 50 when `CostUnknown == true` (no pricing data to derive cost spike) |
| 7 | Rounding | `int(math.Round(score))`, then clamped to `[0, 100]` |
| 8 | Error-state hard penalty | Any non-nil `ErrorState` clamps the score to max 30 regardless of other components |
| 9 | A.3 error component | Tool-error RATE `(1 − toolErrorRate) × 100`, forced to 0 on a qualitative `ErrorState`. Tool errors intentionally feed two components (A.1 successRate AND A.3) so tool-call failures actually drive severity. |

## Section A — Score Formula

The composite score is a weighted sum of four components, each normalized to 0–100 before weighting.

```
HealthScore = clamp(round(
    0.40 × successRate
  + 0.25 × cacheHitPct
  + 0.25 × errorRateComponent
  + 0.10 × costSpikePenalty
), 0, 100)
```

Post-compute: if `agent.ErrorState != nil` → `HealthScore = min(HealthScore, 30)`.

### A.1 Success Rate (weight 0.40)

**Inputs:** `session.ToolCounts` (map[string]int — total call counts per tool name from the session tail-read), `session.Meta.ToolErrors` (int — cumulative error count from session-meta JSON).

**Denominator:** `totalToolCalls = sum of all values in session.ToolCounts`.

**Numerator:** `successfulCalls = totalToolCalls − session.Meta.ToolErrors` (floor 0).

**Formula:**
```
successRate = (successfulCalls / totalToolCalls) × 100
```

**Edge cases:**
- `totalToolCalls == 0` → `successRate = 100` (no calls, no failures — neutral).
- `session.Meta == nil` → treat `ToolErrors = 0` → `successRate = 100`.
- `ToolErrors > totalToolCalls` (corrupt data) → clamp `successfulCalls` to 0, `successRate = 0`.

**Rationale:** `session.ToolCounts` is populated by the parser from every `tool_use` block seen in the JSONL tail. `session.Meta.ToolErrors` comes from the session-meta JSON written by the Claude CLI. The meta count may be stale for very long sessions (written at checkpoints, not per-call), but it is the only structured error counter available without re-parsing the full JSONL for `is_error` flags in `tool_result` entries.

### A.2 Cache Hit Percentage (weight 0.25)

**Inputs:** `session.TokenUsage.CacheReadTokens` (int), `session.TokenUsage.CacheCreationTokens` (int).

**Formula:**
```
cacheTokens = CacheReadTokens + CacheCreationTokens
cacheHitPct = (CacheReadTokens / cacheTokens) × 100   if cacheTokens > 0
            = 50                                        if cacheTokens == 0
```

**Edge cases:**
- `cacheTokens == 0` → neutral value 50 (agent has not produced any cached content yet; not penalized, not rewarded).
- Both values are accumulated across all turns in the session tail; already available as `sdk.TokenUsage` in `session.TokenUsage`.

**Rationale:** A high cache-read ratio indicates the agent is reusing context efficiently. A session that only creates cache and never reads it scores 0 on this component, which is a mild signal of an unusual pattern. The 50-point neutral floor for zero-cache sessions avoids punishing fresh agents unfairly.

### A.3 Error Rate Component (weight 0.25)

**Inputs:** `session.ToolCounts` (map[string]int — same denominator as A.1), `session.Meta.ToolErrors` (int — treat 0 when `session.Meta == nil`), `session.ErrorState` (sdk.ErrorState — one of `quota_exhausted`, `rate_limited`, `auth_failed`, or empty).

**Formula:**
```
totalToolCalls  = sum of all values in session.ToolCounts
toolErrorRate   = clamp(ToolErrors / totalToolCalls, 0, 1)   // 0 when totalToolCalls == 0
errorComponent  = (1 − toolErrorRate) × 100
if session.ErrorState != "" { errorComponent = 0 }           // qualitative error forces this slot to 0
```

This component reflects the tool-call failure RATE, not a binary flag. A session failing most of its tool calls scores low here even when no qualitative `ErrorState` was ever set — the change that lets genuine tool-call failures drag the score into the red tier.

**Dual-signal note:** tool errors intentionally feed TWO components — A.1 (successRate) and A.3 (errorComponent). A struggling session is penalized by both, so tool failures matter more than either signal alone. This double-counting is deliberate.

**Edge cases:**
- `totalToolCalls == 0` → `toolErrorRate = 0` → `errorComponent = 100` (no calls, no failures — neutral), unless `ErrorState` is set → 0.
- `session.Meta == nil` → treat `ToolErrors = 0` → `errorComponent = 100`.
- `ToolErrors > totalToolCalls` (corrupt data) → `toolErrorRate` clamps to 1 → `errorComponent = 0`.
- A qualitative `ErrorState` overrides the rate and forces `errorComponent = 0`. The post-compute hard cap (`min(score, 30)` when `ErrorState != ""`) reinforces this: even with a perfect success rate and cache hit, a quota/rate-limit/auth error caps the overall score at 30.

### A.4 Cost Spike Penalty (weight 0.10)

**Inputs:** `session.CostEstimate` (float64 — current session cost in USD), `baselineDailyCost` (float64 — injected 7-day per-session average, see Section B).

**Formula:**
```
if baselineDailyCost <= 0 or CostUnknown:
    costSpikePenalty = 100   // no penalty when no baseline

ratio = CostEstimate / baselineDailyCost
costSpikePenalty = clamp(100 − (ratio − 1) × 50, 0, 100)
```

Interpretation of the penalty curve:
- `ratio ≤ 1.0` (at or below baseline) → `costSpikePenalty = 100` (no penalty).
- `ratio = 2.0` (2× baseline) → `100 − (1.0 × 50) = 50`.
- `ratio = 3.0` (3× baseline) → `100 − (2.0 × 50) = 0`.
- Above 3× → clamped to 0.

**Edge cases:**
- `baselineDailyCost <= 0` → `costSpikePenalty = 100` (no baseline available; do not penalize).
- `CostEstimate == 0 and CostUnknown == false` (e.g. very new session) → `ratio = 0 → costSpikePenalty = 100`.
- `CostUnknown == true` → `costSpikePenalty = 100` (non-Claude provider; see Decision 6).

## Section B — 7-Day Baseline and Layering

### B.1 Layering Constraint

`merger/` may import `parser/`, `scanner/`, `sdk`, `channelconfig`, and standard library only. It must not import `db/`, `db/rawrepo`, `db/repo`, `api/`, or `pipeline/` (see `.agent-context/task-pipeline.md` "Go Layer Direction"). The `agent_cost_trend` table lives behind `db/rawrepo.AnalyticsRepo`, which is inaccessible from `merger/`.

### B.2 Chosen Approach — Injected Baseline via Options Struct

`GetAgents` is refactored to accept an optional configuration struct:

```go
// GetAgentsOpts carries optional settings for a single GetAgents call.
type GetAgentsOpts struct {
    // BaselineDailyCostUSD is the average per-session cost over the past 7 days,
    // pre-computed by the caller. Zero means "no baseline available".
    BaselineDailyCostUSD float64
}

func GetAgents(ctx context.Context, opts GetAgentsOpts) ([]sdk.Agent, error)
```

The caller (`agentbroadcast/loop.go` and `api/router.go`) computes the baseline separately — once per broadcast tick — by querying `rawrepo.AnalyticsRepo.GetCostSince(ctx, now.Add(-7*24*time.Hour))` and dividing the total by the session count. This query is cheap (one indexed scan on `recorded_at`) and already in the existing analytics path.

`buildAgent` receives the `float64` baseline directly:

```go
func buildAgent(proc scanner.ProcessInfo, session *parser.SessionData, baselineCost float64) sdk.Agent
```

### B.3 Broadcast Loop Change

`agentbroadcast/loop.go` currently calls `merger.GetAgents(ctx)` with no arguments. After this change it calls `merger.GetAgents(ctx, merger.GetAgentsOpts{BaselineDailyCostUSD: baseline})`. The baseline is computed once per tick before the `GetAgents` call and is zero until the analytics repo is wired in. A zero baseline causes the cost component to return 100 (no penalty), so the behavior before wiring is correct.

### B.4 Wiring in Composition Root

`server/internal/api/router.go` and `server/cmd/serve/di.go` are the two places that call `merger.GetAgents` directly (router) or pass `merger.GetAgents` as a function reference (broadcast loop). The `AnalyticsRepo` is already constructed in `di.go` and injected into route handlers. The broadcast loop will need the analytics repo injected as well. This is a small wiring change in `di.go` / `router.go` — it does not break layering because `agentbroadcast/` is a peer of `merger/` and may freely import `db/rawrepo`.

**Open Decision OD-1:** The analytics repo query adds one DB round-trip per SSE tick (default 3s). If profiling shows this is expensive, the baseline can be cached in-memory in `agentbroadcast/loop.go` with a 60s TTL. The spec recommends starting without the cache and adding it only if benchmarks show a problem.

### B.5 Call Sites Not Yet Using AnalyticsRepo

`api/router.go` line 201 passes `merger.GetAgents` as a `GetAgentsFn` function value to `agents.NewHandler`. This indirect call site will also need the opts injected. The cleanest path is to change `GetAgentsFn` to:

```go
type GetAgentsFn func(ctx context.Context, opts merger.GetAgentsOpts) ([]sdk.Agent, error)
```

and update all four call sites (`agents.NewHandler`, `sessions.NewCommandsHandler`, `apiconfig.NewHandler`, `api/search/handler.go`). Each site passes zero-value opts until the baseline is plumbed through individually.

## Section C — SDK Type

No changes required. `sdk.Agent.HealthScore` is already declared as:

```go
HealthScore int `json:"healthScore"`
```

(confirmed in `sdk/types.go` line 220). The field is a value type (not a pointer), so it always serializes to JSON. Its zero value `0` will no longer be emitted once `buildAgent` assigns it.

## Section D — UI (No Changes Required)

`AgentCard.vue` already renders the health score chip and applies severity CSS classes:

```ts
const healthChipClass = computed(() => {
  const s = props.agent.healthScore
  if (s >= 75) return 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400'
  if (s >= 40) return 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400'
  return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400'
})
```

The tiers are: `< 40` → red, `40–74` → amber, `≥ 75` → green. These align naturally with the formula:
- A session with error state hard-capped at 30 → red.
- A session with moderate tool errors or no cache → amber.
- A clean, cache-efficient session → green.

The tooltip already shows `"Health score: {n}/100"`. No UI file changes are needed.

## Section E — Affected Files

| File | Change |
|---|---|
| `server/internal/merger/health.go` | New — `ComputeHealthScore` function and supporting helpers |
| `server/internal/merger/merger.go` | Add `GetAgentsOpts` struct; update `GetAgents` signature; update `buildAgent` to call `ComputeHealthScore` and assign result |
| `server/internal/agentbroadcast/loop.go` | Accept `AnalyticsRepo` dependency; compute baseline before tick; pass via `GetAgentsOpts` |
| `server/internal/api/agents/handler.go` | Update `GetAgentsFn` type signature to include opts |
| `server/internal/api/router.go` | Update four `merger.GetAgents` call sites to pass opts |
| `server/cmd/serve/di.go` | Wire analytics repo into broadcast loop constructor |
| `server/internal/merger/health_test.go` | New — unit tests (see Section F) |

No changes to `sdk/types.go`, `src/components/AgentCard.vue`, or any other frontend file.

## Section F — Testing

### F.1 Unit Tests — `server/internal/merger/health_test.go`

Table-driven tests for `ComputeHealthScore`. Each case builds a `parser.SessionData` and a `float64` baseline and asserts the returned integer.

All expected values are EXACT (asserted with `require.Equal`, no ranges) — a loose span lets a transposed weight pass silently, so the deterministic math is pinned to a single integer per case.

| Test name | Setup | Components (succ / cache / err / cost) | Expected score |
|---|---|---|---|
| `zero_turns_no_data` | `ConversationTurns=0`, no tools, no meta | short-circuit (neutral) | 50 |
| `perfect_session` | 10 calls, 0 errors, 80% cache read ratio, cost at baseline | 100 / 80 / 100 / 100 | 95 |
| `quota_error_hard_cap` | `ErrorState=quota_exhausted`, otherwise perfect | 100 / 80 / 0 / 100 → raw 70, hard-capped | 30 |
| `high_tool_error_rate` | 10 calls, 5 errors, no cache, no error state | 50 / 50 / 50 / 100 | 55 |
| `all_tools_fail` | 10 calls, 10 errors, no cache, no error state, no baseline | 0 / 50 / 0 / 100 → 22.5 | 23 (RED, < 40) |
| `no_cache_usage` | 10 calls, 0 errors, 0 cache tokens | 100 / 50 / 100 / 100 → 87.5 | 88 |
| `cost_spike_2x` | 0 errors, perfect cache, cost = 2× baseline | 100 / 100 / 100 / 50 | 95 |
| `cost_spike_3x_plus` | 0 errors, perfect cache, cost = 3× baseline | 100 / 100 / 100 / 0 | 90 |
| `no_baseline_no_penalty` | perfect session, baseline = 0 | 100 / 80 / 100 / 100 | 95 |
| `cost_unknown` | `CostUnknown=true`, otherwise perfect | 100 / 80 / 100 / 100 | 95 |
| `nil_meta` | `Meta = nil`, 10 tool calls, no cache | 100 / 50 / 100 / 100 → 87.5 | 88 |
| `clamp_above_100` | All components max (cost ratio 0.5 clamps to 100) | 100 / 100 / 100 / 100 | 100 |
| `clamp_below_0` | All components 0, error state set | 0 / 0 / 0 / 0 | 0 |

The `all_tools_fail` case is the headline regression guard for this recalibration: under the old binary error slot it scored ~48 (amber) despite failing every tool call; now it lands at 23, in the RED chip tier (< 40).

### F.2 Integration Guard

`TestGetAgents_DoesNotPanic` in `merger_integration_test.go` already verifies that `GetAgents` runs without panicking. After the signature change it will pass `merger.GetAgentsOpts{}` (zero value) — no other behavioral change needed.

### F.3 What Is Not Tested Here

- The analytics-repo query for the baseline is tested in `server/internal/db/rawrepo/` (existing coverage via `GetCostSince`).
- `AgentCard.vue` chip color rendering is not changed; no new UI test is required.

## Acceptance Criteria

1. An agent with 20 tool calls, 0 errors, `CacheReadTokens > CacheCreationTokens`, and a cost at the 7-day baseline receives a `HealthScore ≥ 75` and the chip renders green.
2. An agent whose session log contains a quota-exhausted message has `HealthScore ≤ 30` and the chip renders red, regardless of tool success rate.
3. An agent with no conversation turns (just spawned) has `HealthScore = 50` — amber chip, not red.
4. A non-Claude agent with `CostUnknown = true` is not penalized for the cost component; its score reflects only tool success rate, cache ratio, and error state.
5. Restarting the server with no `agent_cost_trend` rows (empty DB) produces valid health scores — no panic, no NaN, no division-by-zero.
6. The `GetAgentsFn` type change compiles across all four call sites without behavioral regression.
