# ADR-0002: Runner-Slot Priority Model for Task Pickup

**Status:** Accepted
**Date:** 2026-04-12

## Context

Before Phase 4 landed, the Task Pipeline had no real scheduler. Tasks
advanced only when someone (the UI, a test, a `curl`) explicitly called
`POST /api/tasks/:id/progress`. `progressPendingTasks()` was a no-op
placeholder, and the only parallelism cap in the system was stage-specific:
`hasUmsetzungSlot()` counted running `umsetzung` stage_runs against
`maxParallelOrchestrators` and refused additional `umsetzung` work. Every
other stage could spawn without bound.

Phase 4 made every agent stage spawn a real detached Claude process, so
"the orchestrator runs tasks autonomously" became a hard requirement.
That forced three entangled questions:

1. **How many tasks can run concurrently?** Previously answered only for
   `umsetzung`. Now every stage spawns a real process that consumes
   API budget, CPU, and a `.claude/settings.json` write — we need a
   global cap, not a per-stage one.
2. **Which task goes next when a slot frees?** With multiple tasks
   potentially sitting in `backlog` or resumable from user-wait states,
   FIFO, priority, and "finish-what-you-started" all conflict in
   realistic workflows (urgent hotfixes vs long-running refactors vs
   routine cleanup).
3. **When does a running task release its slot?** A paused-awaiting-user
   task is idle (no agent), so the slot is free — but should we
   preempt it on resume, or should it reclaim its slot?

## Decision

Adopt a **runner-slot model** with a four-tier priority comparator and a
sticky-run invariant.

### 1. Runner slots

- `maxParallelOrchestrators` (SQLite `pipeline_config` key, default `3`)
  is now the **global** cap on concurrently-driven tasks. A task is
  "busy" iff its latest `stage_run.status = 'running'`.
- `hasUmsetzungSlot()` is **removed**. The global cap subsumes it.
  Stage-specific caps are not reintroduced — if a future concern
  (per-stage API rate limits, etc.) needs one, it gets its own ADR.
- `hasFreeRunnerSlot()` guards every agent-requiring handler execution.
  Agent-less stages (backlog, approval1, approval2) bypass the cap
  because they do not spawn processes.

### 2. Pickup priority

When a free slot exists, the driver loop picks from the pool of
**pickable** tasks and sorts by this comparator (`comparePickOrder` in
`orchestrator.ts`):

1. **Silver bullet** — per-task boolean, set at creation. Silver-bullet
   tasks jump the queue unconditionally. Intended use: urgent hotfixes,
   demo prep, anything blocking other work.
2. **Furthest stage index** — among non-silver tasks, the one whose
   `current_stage` is latest in `STAGE_ORDER` wins. This operationalizes
   "finish what you started": a task in `umsetzung` beats a task in
   `backlog` on the same priority tier.
3. **Priority field** — `high` > `medium` > `low`, set at creation and
   editable later.
4. **Creation time** — older tasks win. Final tiebreaker, stable.

### 3. Pickable pool

A task is pickable iff:

- `current_stage NOT IN ('done','failed','cancelled','on_hold','approval1','approval2')`
- **AND** it has no `stage_run` currently in `status='running'`
- **AND** its latest `stage_run` is not in `status='awaiting_user'`

This means:

- **`on_hold` releases the slot.** Unlike `awaiting_user` (which is a
  short pause waiting for a single click), `on_hold` is an indefinite
  park waiting for a permission grant — holding a slot for it would
  waste capacity.
- **`awaiting_user` also releases the slot**, because the orchestrator
  cannot re-enter it anyway; only an explicit API call
  (`resumeFromUser`) advances the task. The picker must skip it to
  avoid repeatedly trying and failing.
- **`approval1` / `approval2`** are agent-less wait-gates. Their handler
  returns `wait_user` without spawning, so they do not consume a slot
  even while "in progress".

### 4. Sticky-run invariant

Once a task is picked, the orchestrator keeps driving it stage-by-stage
until it hits terminal (`done`/`failed`/`cancelled`) or a paused state
(`awaiting_user`/`approval1`/`approval2`/`on_hold`). Under the cap, a
free slot must always prefer an already-started task over a new backlog
task — which is exactly what tier 2 (furthest stage index) guarantees.

## Consequences

**Positive:**

- Deterministic, inspectable scheduling order — a reviewer can reason
  about "why did task X go before task Y?" from four columns in the
  `tasks` table.
- Emergency-response path via silver-bullet is explicit and UI-visible,
  not a backdoor hack on priority.
- Long-running multi-stage tasks cannot be starved by new backlog
  arrivals, because tier 2 protects them.
- Single cap is easier to tune than per-stage caps. One config key,
  one invariant to check.

**Negative / Trade-offs:**

- No fairness across priority tiers. A stream of high-priority tasks
  will indefinitely delay a medium-priority task. This is intentional
  — fairness would undermine priority — but it means users must
  occasionally silver-bullet a starved task manually.
- The comparator's tier 2 (furthest stage) can feel surprising to new
  users who expect strict priority ordering. Needs documentation in
  the task-list UI (not yet done).
- Stage-specific concerns (e.g. "`umsetzung` costs much more per run
  than `pruefung`") are now unmodeled. Before Phase 4 they were
  modeled by the umsetzung-only cap; now they are flattened into the
  global cap. A future ADR may reintroduce stage weights if this
  becomes a real problem in production use.

**Follow-ups:**

- `comparePickOrder` in `server/pipeline/orchestrator.ts` is the
  canonical implementation. Changing the order **must** update this
  ADR at the same time — the code and the decision are a pair.
- The picker respects task state via `listPickableTasks()` in
  `server/db/tasksRepo.ts`. If a new stage is added to `STAGE_ORDER`,
  the `NOT IN` exclusion list must be updated if the new stage should
  not be pickable.
- A new `idx_tasks_picker` SQLite index on
  `(silver_bullet DESC, priority, created_at)` exists for the sort,
  but the picker filter uses `listPickableTasks()` which does not use
  this index. The index is there for a future "show me the picker's
  next 10 candidates" query surface.

## Alternatives Considered

- **Strict FIFO on `created_at`** — rejected: no emergency response
  path, no way to prefer in-progress work over new backlog.
- **Strict priority (high → medium → low, FIFO within tier)** —
  rejected: lets a fresh high-priority backlog task starve a
  medium-priority task already mid-pipeline. Violates sticky-run
  intent.
- **Pure sticky runs without priority** — rejected: no way to tell the
  scheduler "this hotfix is more urgent than the refactor I started
  yesterday."
- **Separate priority queues per stage** — rejected: too many knobs,
  no clear mental model, doesn't solve the global-cap question.
- **Weighted round-robin across tiers** — rejected for the first
  iteration: complexity isn't justified until we see real fairness
  problems. May revisit if silver-bullet usage ends up starving
  non-urgent work in production.
