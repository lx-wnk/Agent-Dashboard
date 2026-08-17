# Plane B — L2: PM-Agent (orchestrator role)

> Status: approved design (2026-06-25). Top layer of Plane B.
> Depends on: L0 (coordination primitives, PR #223, done), L1 (agent channel, spec'd),
> dependency enforcement (PR #225, done).

## Purpose

Give the dashboard a **PM-agent**: an agent that decomposes a goal into a tree of
worker subtasks, wires their dependencies, monitors their results, and iteratively
plans further waves until the goal is synthesized — without a long-lived,
stall-prone coordinator process.

This is the unified **A + B** solution (no MVP-only cut):

- **A (static DAG author):** the PM decomposes the goal into a subtask DAG, then
  exits; the engine runs the workers gated by their dependencies (#225).
- **B (iterative re-woken PM):** the PM can re-plan in waves. Each wave is followed
  by a PM-continuation task that *depends on* that wave's workers, so the dependency
  gate auto-runs the PM again once the wave settles. Adaptive multi-wave planning
  with **no live process** and therefore no spawned-orchestrator stall.

Static-A is simply the degenerate case of B: one wave, where the continuation is the
final synthesis.

## Core principle

> The agent authors a graph; the engine runs the graph; the graph re-invokes the agent.

Coordination lives in the **tested dependency gate (#225)**, never in agent polling.
The **task tree is the state** — each PM run reconstructs progress by reading its
child subtasks and its L1 inbox, so re-entrancy needs no external wave-state store.
This is the deliberate antidote to the OFD finding that a spawned orchestrator
stalls after dispatching background children (it is never reliably re-woken).

## Non-goals

- No long-lived reactive PM (live poll/react loop) — that is the exact pattern that
  stalled 3× in the OFD plan-mode run; it requires a re-wake infra fix L2 does not have.
- No new "subtree complete" rollup UI now (see §8) — follow-up.
- No change to how workers themselves run (they are ordinary pipeline tasks).

## Architecture

### 1. PM realization

A new task field **`pmMode bool`** (default false), mirroring `planMode` precedent:
a defaulted bool, free-string stage swap, **no enum/migration**. When `pmMode` is
set, the task's implementation stage runs the **PM decomposition prompt** instead of
the normal implementation prompt. The first run lives on the **root goal task**;
subsequent runs live on **PM-continuation** child tasks (also `pmMode`).

### 2. A single PM run

1. Read the goal/spec, the current child subtask tree (`list_tasks` filtered by
   `parentTaskId`), each child's state/outputs (`get_task`), and the L1 inbox
   (`read_inbox` — worker reports).
2. Compute the **delta**: what work remains given the current tree state.
3. For each new worker: `create_task(parentTaskId=<goal>)`, wiring inter-worker
   `add_dependency` where ordering matters.
4. Create exactly **one PM-continuation** child that depends on this wave's workers:
   `add_dependency(continuation, worker, required_stage="done", on_cancel_action="start")`
   for each worker.
5. Finish its own stage (`DoneTransition`). The PM is now gone — no live process.

### 3. Re-wake and termination (B)

When the wave settles, the dependency gate (#225) auto-unblocks the PM-continuation
and the picker runs it. The continuation re-reads tree + inbox and either:

- **More work** → spawn the next wave + another PM-continuation depending on it; or
- **Done** → produce the final **synthesis** itself (write it to its stage output
  / the goal), then finish. No separate aggregator task is needed.

### 4. Engine change — terminal-on-exhaustion (makes B react to failures)

**Problem:** the re-woken PM must run when a wave settles, *including on failure*.
Today, on retry-budget exhaustion the orchestrator applies `FailTransition`
(`orchestrator.go` ~613 rate-limit, ~645 infra), which sets the **stage_run**
status to `failed` but does **not** change `task.current_stage`. So an exhausted
worker stays non-terminal (`current_stage="implementation"`) and a dependent gates
forever — the PM never re-wakes to react.

**Change:**

1. On retry-budget exhaustion, additionally transition the task to a **terminal
   `failed` stage** (add `"failed"` to `IsTerminalStage` in `pipeline/types.go`
   alongside `done`/`cancelled`). Non-exhaustion failures are unchanged (so manual
   retry/resume still works on a transient fail).
2. Extend the `on_cancel_action="start"` rescue in `EvaluateDependency`
   (`pipeline/dependency_eval.go`) so it also rescues an upstream that reached the
   terminal `failed` stage — i.e. `start` means "proceed whether the upstream was
   cancelled **or** hard-failed." This keeps the semantics of `cancelled` and
   `failed` distinct (no overloading) while letting a PM-continuation re-wake on
   either terminal outcome.

Result: PM-continuation deps use `required_stage="done", on_cancel_action="start"`,
so the PM re-wakes whether each worker **finished** (done → satisfied),
**was cancelled** (start rescue), or **hard-failed** (start rescue). The PM then
reads the worker's outputs (which carry `auto_retries_exhausted` /
`rate_limit_retries_exhausted` markers) and reacts.

**Manual retry/resume** from the terminal `failed` stage re-queues as today
(re-queue path sets the stage back to a runnable one); verify this in the retry/
resume tests when implementing.

### 5. Result passing

The PM (and its final synthesis run) obtains worker results from **two sources**:

- **Pull (reliable backbone):** `get_task`/`list_tasks` for each worker's
  `current_stage`, latest stage_run output, PR link, and the exhaustion marker.
  Works even if a worker never reports.
- **Reports (narrative):** workers `send_peer_message(<pm task>, summary)` on
  finish (L1); the PM reads them via `read_inbox`.

Correctness requires no worker compliance — reports only enrich.

### 6. Scope

The PM agent's MCP key needs **`tasks:write`** (create_task / add_dependency) and
**`pipeline:control`** (cancel/redirect workers; implies `tasks:read` + `agent:coord`,
so L0 scratchpads/locks and L1 messaging are covered). No new scope is introduced.

### 7. Re-entrancy safety

Every PM run is idempotent with respect to wave creation: before creating workers it
**inspects existing child subtasks** so a re-wake never double-spawns a wave. The
decomposition prompt is framed around "given the current child tree and inbox, emit
only the *delta* of work." A PM-continuation that finds its wave already fully
spawned and unfinished must be a no-op that simply finishes (the gate will re-run a
later continuation). Guard against an accidental infinite continuation chain by
having the PM stop creating continuations once no new workers are produced.

### 8. UX

The root goal task hosts wave-1 and finishes early; overall "goal complete" equals
the terminal PM-continuation finishing. The existing **subtask cards** plus the
`isBlocked` / `isUnsatisfiable` DTO from #225 already visualize the tree and gating
state. Do **not** build a new "subtree complete" rollup indicator now — note it as a
follow-up if the early-finishing root proves confusing in practice.

## Testing strategy

- **Engine (terminal-on-exhaustion):** a worker that exhausts its retry budget lands
  in terminal `failed`; `IsTerminalStage("failed")` true; a dependent with
  `on_cancel_action="start"` becomes satisfied (re-wakes) on an upstream `failed`;
  a dependent with `on_cancel_action != start` reads `unsatisfiable`. Non-exhaustion
  fail leaves the task non-terminal (manual retry still possible). Manual retry/resume
  from terminal `failed` re-queues.
- **Dependency-eval extension:** `EvaluateDependency` table cases for upstream
  `failed` + each `on_cancel_action` (start → satisfied, others → unsatisfiable).
- **PM run (decomposition):** given a goal, the PM creates the expected workers with
  `parentTaskId`, the correct inter-worker deps, and exactly one continuation
  depending on the wave; idempotent re-run creates no duplicate workers.
- **Iteration:** simulate a wave settling → continuation gate opens → second PM run
  spawns the next wave or synthesizes; chain terminates when no new workers.
- **Result passing:** PM reads both pulled worker state and inbox reports.

## Build inventory

- `pmMode bool` task field (defaulted; no migration beyond the bool).
- PM decomposition prompt + stage-prompt swap (mirror `planMode` wiring).
- Engine change: terminal `failed` stage on exhaustion + `IsTerminalStage` +
  `EvaluateDependency` `start`-rescue extension to cover `failed`.
- Worker finish convention: optional `send_peer_message` report (prompt-level).
- Tests above.

Reuses unchanged: `create_task parentTaskId`, `add_dependency`, the dependency gate
and cascade (#225), L1 inbox (`send_peer_message`/`read_inbox`), subtask-card UI,
the dependency DTO.
