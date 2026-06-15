# ADR-0006: Passive Drift Detection over `stage_run` (eval leaf)

**Status:** Accepted
**Date:** 2026-06-15

## Context

The pipeline already persists rich per-run signals in `stage_run`: status,
iteration, JSON output, tokens, cost, and start/end timestamps. Nothing
consumed these to answer the operational question "is a given
`(spawner, model, stage)` combination getting *worse* over time?" — silent
quality regressions (falling success rate, climbing iteration counts, rising
cost) were invisible until a human noticed.

Two broad approaches exist:

1. **Active golden-eval** — replay fixed task fixtures through each
   spawner/model and score the output against a rubric. High signal, but it
   spawns real agents (token cost), needs a fixture corpus and a scoring
   harness, and is a project in its own right.
2. **Passive drift detection** — derive quality metrics from the runs that
   *already happened* and compare a recent window against a trailing
   baseline. Zero extra agent spawns, explainable, and shippable now.

## Decision

Ship **passive drift detection only**; defer the active golden-eval harness
to follow-up GAP-09b.

Add a leaf package `server/internal/eval/` modelled on `analytics/` and
`history/`. It imports only `db/repo`, `db/ent`, `sdk`, `config`, and
`parser` — never `pipeline`, `notifications`, `services`, `sse`, or routes.

- **Metrics** (`metrics.go`, SSOT for metric identity): stage success rate,
  mean iterations-to-success, first-iteration validation-fail rate,
  awaiting-user rate, escalation rate, mean duration, mean cost, mean
  tokens, timeout rate. Each metric is classified as a *rate* (0–1) or a
  *continuous* value, with a `higher-is-worse` direction.
- **Collector** (`collector.go`): buckets `stage_run` rows by
  `(spawner_id, model, stage)`, joining `task` for `spawner_id` and a
  best-effort `model` (from `task.metadata`, since `model` is not persisted
  on `stage_run`).
- **Drift algorithm** (`drift.go`, pure): trailing baseline window
  `[now-2W, now-W)` vs recent `[now-W, now)`. Rate metrics fire on a
  `≥ RATE_DROP_PP` percentage-point worsening; continuous metrics fire when
  the recent mean exceeds `baseline_mean + k × baseline_stddev`. A
  minimum-sample guard suppresses thin data; improvements never alert.
- **Persistence**: two ent tables. `eval_metric_snapshot` is both the chart
  history and the source of baseline statistics (mean/stddev over the
  snapshot series). `drift_alert` holds open/acknowledged alerts, deduped by
  a partial-unique index on `(spawner_id, model, stage, metric_key)` where
  `status = 'open'`.
- **Scheduling**: `Service.RunLoop` mirrors the cost-history scheduler —
  boot scan plus ticker, `interval <= 0` = boot-only, single-instance guard.
- **Delivery**: REST under `/api/eval/*` (metrics, drift, ack, scan), JWT /
  same-origin like analytics, plus an `OnDrift` callback wired **only** in
  the composition root so the leaf stays notifications-agnostic (same
  pattern as `OnPermissionRequest`). The callback currently logs; live SSE /
  push delivery is deferred (the frontend reads alerts via the REST
  endpoint).

## Consequences

**No new agent spawns.** Detection reads existing `stage_run` rows, so it
adds no token cost and cannot trip the real-agent-spawn guard.

**Cold start.** Because the baseline is built from prior snapshots, no alert
can fire until roughly `2 × DASHBOARD_EVAL_WINDOW_HOURS` of history has
accumulated. This is expected, not a bug.

**Best-effort model dimension.** `model` is resolved from `task.metadata`
rather than a dedicated `stage_run` column; runs without it bucket under
`"default"`. Adding a persisted per-run model column is a future option.

**Explainable, no ML.** Thresholds are plain config (`DASHBOARD_EVAL_*`), so
every alert is traceable to a baseline value, a recent value, and a fixed
threshold.

## Alternatives Considered

1. **Active golden-eval now.** Rejected for this iteration — it spawns real
   agents and requires a fixture/scoring harness. Tracked as GAP-09b.
2. **Compute the baseline directly from a second collector window instead of
   snapshots.** A single window aggregate yields a point estimate with no
   dispersion, so continuous metrics would have no stddev to threshold on.
   Deriving the baseline from the snapshot series gives real mean *and*
   stddev and reuses the table we already persist for charts.
3. **Put eval logic in `analytics/` or `services/`.** Rejected: `eval/` is a
   cohesive domain (metrics + drift + scheduler) and a dedicated leaf keeps
   its dependency floor at `db/repo` / `db/ent`, matching ADR-0005's
   reasoning for `llmadapter`.
