# Refinement: server-tracked runs, progress visibility, and working-directory fix

**Date:** 2026-05-30
**Branch:** `feat/layout-redesign`
**Status:** Approved (design) — pending implementation plan

## Problem

The New-Ticket refinement chat has three coupled defects, all rooted in the
fact that a refinement turn is an **ephemeral `claude -p` process bound to the
HTTP request** (`turnCtx := context.WithTimeout(r.Context(), 5*time.Minute)` in
`server/internal/api/refine/handler.go`; the process is killed on
`r.Context().Done()`):

1. **No progress is visible.** There is no server-side notion of "a refinement
   is in progress", so nothing can be shown in the pipeline UI while claude runs.
2. **A hung/long run is invisible and dies silently.** If the run takes minutes
   (or hangs), the user sees only a brief "working" bubble; closing the chat or
   reloading kills/abandons it with no trace.
3. **Reopening the chat shows nothing.** A frontend lifecycle race between the
   `props.task` clear-watch and the `props.open` load-watch, plus an
   `isStreaming` early-return in `loadHistory`, leaves the reopened modal empty
   even though the backend `GET /api/refine/{id}/turns` returns the turns.

A fourth, contributing bug: the web New-Ticket flow hardcodes `cwd: '/'`
(`src/components/RefinementChat.vue:166`), so every web-created concept task
runs claude in `/` instead of a real project directory — refinement cannot
analyze the intended codebase ("Löse die Findings aus `/Users/.../EmpiriconSearch/`"
ran in `/`).

## Goals

- A refinement run survives browser close/reopen and reload, and runs to
  completion server-side regardless of the originating request's lifetime.
- The pipeline UI shows refinement progress **collapsed in the concept step**
  (status + last result), not as chat messages.
- New-Ticket tasks run in a real, user-chosen working directory.
- Reopening the chat reliably shows the conversation.

## Non-goals

- Token-by-token live streaming in the collapsed panel (chat keeps token-level
  streaming via the existing `POST /turn` SSE; the panel is status + result).
- Surviving a **server restart** (in-memory run state is acceptable; a restart
  clears it, matching the loss of any detached process).
- Refine phase tracking (analysis/spec/plan/approval stepper) — the backend does
  not emit `phase_change` events; out of scope, tracked separately.
- Fixing the task-creation path for non-web callers (MCP/REST already require a
  real `cwd`); only the web New-Ticket `cwd: '/'` is in scope.

## Architecture

### Shift: ephemeral request-bound spawn → detached server-tracked run

Introduce a **`RefineRunner`** service (in `server/internal/refine` or a small
new package, injected into the refine handler from the composition root). It:

- Holds an in-memory registry `map[taskID]→runState{ status, startedAt, error }`
  guarded by a mutex. `status ∈ {running, done, failed}`; absence ⇒ `idle`.
- `Start(taskID string, cfg refine.SpawnConfig) (<-chan string, error)`:
  - Marks `running`, fires the start-side effect (broadcast — see below).
  - Spawns claude via the existing `RunRefinementTurn` but with a **background
    context** (`context.WithTimeout(context.Background(), 5*time.Minute)`), NOT
    the request context. The run is owned by a background goroutine.
  - The background goroutine accumulates output, and on stream end **persists the
    assistant turn** (`Turns.Create`), sets `done` (or `failed` on error/timeout),
    and fires the end-side effect.
  - Returns a channel the HTTP handler can read to tee live lines to the open
    chat. A fan-out (or a per-run subscriber slot) lets the handler consume lines
    without owning the run: when the client disconnects, the handler stops
    reading and returns, but the goroutine keeps draining + persisting.

Re-entrancy: `Start` is a no-op (returns the existing run) if a run is already
`running` for that task — prevents the double-submit that produced two
user-only turns in the broken era.

### `submitTurn` handler refactor

`submitTurn` becomes a thin adapter:
1. Validate task, store the **user** turn (as today).
2. Call `RefineRunner.Start(taskID, cfg)`.
3. Stream returned lines to the SSE response while `r.Context()` is live; return
   on client disconnect or channel close. The handler no longer owns process
   lifetime or the assistant-turn write — the runner does.

### Status surfacing (Task-SSE, no new channel)

- `enrichTask` (`server/internal/api/tasks/enrich.go`) adds
  `refineStatus: 'idle'|'running'|'done'|'failed'` and optional `refineError`,
  read from the `RefineRunner` registry (injected as a small read-only
  interface so `api/tasks` does not depend on the refine handler).
- `RefineRunner`'s start/end side-effects call the existing
  `broadcastEnrichedUpdate(taskID)` wiring (injected callback from the
  composition root, mirroring the orchestrator's `onTaskChanged` pattern — the
  runner does not import `api/tasks` or `sse` directly).
- New `GET /api/refine/{id}/status` → `{ status, error? }` for a one-shot read
  on open (so the panel reflects state immediately, before the next SSE tick).

### Layering

`RefineRunner` lives at the same layer as the refine handler's deps. It depends
only on `repo` (turns) and the injected broadcast callback — never on `api/` or
`sse/` at compile time. The composition root (`cmd/serve/di.go`) constructs it,
wires the broadcast callback to `broadcastEnrichedUpdate`, and passes the
read-only status interface into both the refine handler and the tasks handler's
`enrichTask`.

## Frontend

### Collapsed progress panel (the Main-Step)

`TaskModal` concept-stage area (currently the "waiting for refinement" banner)
gains a collapsed panel modeled on `StageOutputView`:
- Badge from `task.refineStatus`: `running…` (spinner) / `done` / `failed`.
- Collapsed body: the **last assistant turn** (from `GET /turns`), expandable.
  On `failed`, shows `refineError`.
- It is a status/output summary, not chat messages. The "open chat" button
  remains for the conversation.

### New-Ticket working-directory selector (cwd fix)

The New-Ticket empty state (in `RefinementChat.vue`) gains a **working-directory
selector**, required before the first message:
- A dropdown of registered project folders (from `/api/projects` / project
  folders) plus a free-text path input.
- `handleSend` passes the chosen path as `cwd` to `createTask` instead of `'/'`.
- If no directory is chosen, the send is blocked with an inline hint.

### Reopen-empty fix (lifecycle)

In `RefinementChat.vue`, replace the two racing watches (`props.task`
clear-watch + `props.open` load-watch) with a **single**
`watch([() => props.open, () => currentTask.value?.id], …)` that calls
`loadHistory()` when `open && id` and resets state otherwise. In
`useRefinementChat`:
- Remove the message-clear from the `taskId` watch (keep the `isStreaming` /
  `approvalReady` / `error` resets); `loadHistory` repopulates `messages`
  authoritatively, eliminating the clear-vs-load race.
- Remove the `if (isStreaming.value) return` early-return from `loadHistory`
  (or scope it so it never blocks a fresh open), so reopening always reloads.

## Data flow

```
New ticket → pick cwd → createTask({cwd}) → first message
  → POST /api/refine/{id}/turn
      → store user turn
      → RefineRunner.Start: status=running, broadcastEnrichedUpdate (kanban + modal show "running")
          → background goroutine runs claude (background ctx, 5-min timeout)
      → handler tees lines to chat SSE while connected
  → claude finishes
      → goroutine stores assistant turn; status=done; broadcastEnrichedUpdate (panel shows result)
  → client disconnect / close / reload at any point
      → run continues + persists; panel reflects status on next open via /status + /turns
```

## Error handling

- claude non-zero exit / spawn error / 5-min timeout → `status=failed`, error
  text captured in the registry and surfaced as `refineError`; no assistant turn
  written for an empty/error run (an `[ERROR] …` line is treated as failure, not
  as assistant content).
- `RefineRunner.Start` on an already-`running` task returns the live run (no
  second process, no duplicate user turn — the handler stores the user turn only
  when it actually starts a new run).

## Testing

### Go
- `RefineRunner`: `running → done` persists exactly one assistant turn; a
  client-disconnect mid-run (cancel the handler ctx, not the run ctx) still
  persists and reaches `done`; claude error → `failed` + error captured;
  timeout → `failed`; concurrent `Start` for the same task is a no-op.
- `enrichTask` exposes `refineStatus`/`refineError` from a stubbed registry.
- `GET /api/refine/{id}/status` returns the registry state.
- Bypass-auth smoke test (`bypass_auth_smoke_test.go`) auto-covers the new
  `/status` route (no allow-list change needed — it is session-authed).

### Frontend (vitest)
- `useRefinementChat`: reopen (toggle `open` false→true with a stable task id)
  reloads turns into `messages` — the regression for bug #3.
- New-Ticket `handleSend` passes the selected cwd (not `'/'`) to `createTask`.
- TaskModal concept panel renders each `refineStatus` (running spinner / done +
  last turn / failed + error).

## Implementation phasing (single spec, staged delivery)

- **Phase A:** detached `RefineRunner` + `submitTurn` refactor + `/status` +
  `refineStatus` in `enrichTask` + broadcast wiring + reopen-empty fix.
- **Phase B:** New-Ticket working-directory selector (cwd fix).
- **Phase C:** collapsed progress panel in `TaskModal`.

## Out-of-band cleanup (not part of this spec)

Two stale user-only `refinement_turns` for task `8c33989b-…` (and the `ping`/`PONG`
test turns this investigation added) remain in the DB from the broken era. Offer
to purge them after implementation; not a code change.
