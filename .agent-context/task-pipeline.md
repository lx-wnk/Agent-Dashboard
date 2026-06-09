# Task Pipeline Architecture

The Task Pipeline subsystem (`server/db/`, `server/pipeline/`, `server/routes/taskRoutes.ts`, `server/notifications/`) follows a strict layered architecture with callback-based decoupling. See [ADR-0001](../docs/architecture/adr/0001-sqlite-for-task-pipeline.md) for why SQLite was added and [ADR-0002](../docs/architecture/adr/0002-runner-slot-priority-model.md) for the runner-slot priority model used by the driver loop.

## Key Pipeline Modules (`server/pipeline/`)

- `orchestrator.ts` — state machine, `tick()` driver loop, `progressTask()` per-task lock, completion finalization, priority-based runner-slot picker (`pickNextTasksForFreeSlots`).
- `stageHandlers.ts` — `createAgentStage(stage, buildPrompt)` factory that produces real handlers spawning detached Claude processes. `backlogHandler` and `approvalStage` are the only agent-less handlers.
- `stagePrompts.ts` — per-stage `{ systemPrompt, userPrompt }` builders. Each user prompt ends with a `` ```json `` block defining the output schema contract.
- `agentSpawner.ts` — detached `claude` CLI spawn, writes `.claude/settings.json` with pre-approved tool allow-list, injects dashboard-channel MCP.
- `completionDetector.ts` — converts a dead PID + session JSONL into a next/retry/fail decision. Strict per-stage schema validators (`validateStageOutput`) with injectable deps for tests.
- `sessionOutputReader.ts` — reads the last assistant turn from a session JSONL, extracts the `` ```json `` block, falls back to newest-by-mtime session discovery when the stage_run has no `session_id` yet.
- `sessionManager.ts` — `isPidAlive` (with EPERM handling), recovery decisions on orchestrator restart.
- `types.ts` — `StageTransition` union (incl. `async_running`, `taskMetadataPatch` on `next`), `StageContext` with injected `recordAudit` / `requestPermission` side-effects.

## Services (`server/services/`)

Stateless helpers consumed by `routes/*` and `mcp/*` (and, where appropriate, by `pipeline/*`). They do not drive the state machine and do not depend on the orchestrator at runtime:

- `approvalUtils.ts` — `ALLOWED_TOOLS` allow-list and `bulkGrantConceptStagePermissions(taskId)`: bulk-grant every tool permission declared in the latest `implementation_plan` stage output's `toolRequests` array.
- `analysisSpawner.ts` — `spawnAnalysisAgent` / `buildAnalysisPrompt`: detached Claude CLI session for post-failure investigation. Distinct from `pipeline/agentSpawner.ts`; not part of the state machine (no stage_run, no channel MCP, no allow-list).
- `resourceRecommender.ts` — `recommendParallelism()`: recommends a `maxParallelOrchestrators` value based on available CPU/memory.
- `worktreeManager.ts` — `createWorktree` / `removeWorktree` / `isGitRepo` / `currentBranch` / `resolveWorktreeRoot`: per-task git worktrees under `DASHBOARD_WORKTREE_ROOT` (default `<repo>-worktrees` sibling), with legacy-path adoption.

## Notifications (`server/notifications/`)

- `dispatcher.ts` — event-driven dispatcher; registered callbacks come from `server/index.ts` only (never imported by `pipeline/`).
- `adapters/email.ts`, `adapters/webhook.ts`, `adapters/browser.ts`, `adapters/system.ts` — pluggable adapters, wired exclusively in `server/index.ts`.

## Channel Bridge

`channel/dashboard-channel.ts` — MCP server injected into every spawned stage agent. Reads `DASHBOARD_STAGE_RUN_ID` + `DASHBOARD_TASK_ID` env vars and forwards permission requests back to the orchestrator's `onPermissionRequest` callback via the dashboard API. Enables the two-way permission gate without any pipeline-to-notification dependency.

## Runner Slots

`maxParallelOrchestrators` (pipeline_config key, default 3) is the **global** cap on concurrently-driven tasks — it applies to every agent-driven stage, not just `implementation`. Agent-less stages (`concept`, `backlog`) bypass the cap. See ADR-0002 for the pickup priority order (silver-bullet → furthest stage → priority → createdAt) and the sticky-run invariant.

## Stage Timeout

`stageTimeoutSeconds` (pipeline_config key, default 1800) is enforced by the orchestrator's tick loop. If a stage agent's PID is still alive after this many seconds, the orchestrator sends SIGTERM and applies a `fail` transition, freeing its runner slot. Set to `0` to disable. Written via `UPDATE pipeline_config SET value='3600' WHERE key='stageTimeoutSeconds'` (or INSERT if absent).

## Spawner Resolution

`server/internal/services/spawner_resolver.go` resolves the effective spawner for a stage agent using the following precedence:

```
task.spawner_id ?? task.project.default_spawner_id ?? spawner WHERE is_default ?? slug claude-default
```

Resolution happens once, immediately before `exec`, inside
`server/internal/pipeline/spawner.go`. The deployment-wide default is the row
flagged `is_default` (exactly one at a time, enforced by `SpawnerRepo.SetDefault`
in a clear-all-then-set-one transaction; the `claude-default` slug is the ultimate
backstop if no row is flagged). The same `is_default`-then-slug fallback is mirrored
in `server/internal/cmdscope/request_scope.go` for the config-enumeration scope.

`claude-default` is seeded as the initial default and stays an un-deletable
backstop. **Built-in spawners are editable** (name/command/args/env/model/
description) but their **slug is immutable** and they **cannot be deleted**; the
current default also cannot be deleted (`ErrSpawnerIsDefault`) — pick another
default first. Mark a different spawner default via `POST /api/spawners/{id}/default`.

**Env-merge order:** custom-spawner `env` is applied first; dashboard vars
(`DASHBOARD_*`, `CLAUDE_*`) are overlaid afterward and always win.
`DASHBOARD_JWT_SECRET` and `DASHBOARD_HOOKS_SECRET` are never forwarded to
spawned agents.

**Command allow-list:** the `spawners.command` field is validated against a
conservative allow-list at create/update time. Extend the default list via
`DASHBOARD_SPAWNER_ALLOWED_COMMANDS` (comma-separated). CRUD requires the
`keys:manage` MCP scope.

**Adapter dispatch:** the resolved Spawner row also carries an
`adapter_type` field. `server/internal/pipeline/stage_handlers.go::Execute`
reads it and dispatches accordingly: `claude` (or empty) → native subprocess
via `SpawnStageAgent`; any other value → adapter built via
`pipeline.NewLLMSpawnerFromSpawner(row)` followed by `LLMSpawner.Spawn(...)`.
`custom` rows require a non-empty `command` column.

See [ADR-0003](../docs/architecture/adr/0003-pluggable-spawners.md) for full
rationale, security model, and alternatives considered. The
adapter/spawner merge is described in
[ADR-0003 section A](../docs/architecture/adr/0003-pluggable-spawners.md#a-adapterspawner-merge-2026-05).

## Awaiting-User Sweep

`awaitingUserTimeoutSeconds` (pipeline_config key, default 14400 = 4h) governs `sweepAwaitingUserRuns` in the orchestrator tick. Two zombie modes are reaped: (1) any `awaiting_user` stage_run whose PID is gone is immediately marked `failed` (stage timeout itself only inspects status='running' runs, so without this sweep dead-PID awaits would stay zombie forever); (2) any `awaiting_user` run whose PID is still alive but whose `started_at` is older than this limit is SIGTERMed and failed — defends against agents that busy-wait via shell polling loops (`until [ -e /tmp/x ]; do sleep N; done`) instead of yielding to the `request_permission` MCP gate. Set to `0` to disable the wallclock branch (the dead-PID branch always runs).

## Orphan Sweep

`sweepOrphanRuns` runs every tick after the awaiting-user sweep and reaps three zombie modes that the status-specific sweepers leave behind: (1) any non-terminal stage_run (any of `pending`/`running`/`awaiting_user`/`on_hold`) whose parent task is parked at `done`/`cancelled`/`on_hold` — covers cancel-cascade and on_hold-cascade leaks where the dependency cascade flips `task.currentStage` without touching the in-flight stage_run; SIGTERMs any live PID and marks failed; (2) any `on_hold` stage_run with a dead PID; (3) any `pending` stage_run that has not been promoted to `running` within 5 minutes (orchestrator-crash escape hatch — healthy pendings transition within milliseconds). Combined with the awaiting-user sweep, every non-terminal stage_run status across every agent-driven stage (`implementation`/`self_review`/`finalization`) now has a watchdog branch — the sweep is stage-agnostic by construction (filters on stage_run.status, not stage value).

## Multi-Spawn Re-entry Guard

`runProgressTaskLocked` checks after `ensureStageRun` whether the returned stage_run is already `status='running'` with a live PID, and if so returns it without invoking `handler.execute` again. This blocks the user-grants-many-permissions cascade where the resolve route's `else` branch (shouldRestartRun=false because the resolved run is already failed from a prior grant's kill+restart) calls `resumeFromUser → progressTask` and would otherwise spawn another claude process on top of the running one. The guard also leaves `started_at` untouched on re-entry so the awaiting-user / stage timeout windows continue to count from the original spawn.

## Lingering-Pending Gate

`runProgressTaskLocked` checks BEFORE `ensureStageRun` whether the most recent stage_run on the current stage either (a) is terminal (`done`/`failed`) OR (b) is `awaiting_user` with a dead PID (zombie awaiting that `sweepAwaitingUserRuns` will reap on its next tick), AND still has unresolved `permission_request` rows attached. If so it returns null without spawning. Defense-in-depth against the same cascade as the re-entry guard, but from the OPPOSITE direction: re-entry guard catches a 2nd-spawn-on-top-of-running run; this gate prevents a 2nd spawn after the first run was killed-and-failed while sibling pendings were still waiting on the dead run, AND closes the synchronous-resolve-vs-tick-driven-sweep race for zombie awaiting_user runs. The bulk-resolve endpoint (`POST /api/permission-requests/bulk-resolve`) avoids the cascade by design (one transaction, one SIGTERM, one progressTask call) — the gate is the safety net for legacy one-by-one resolution and any race where new agent-side requests arrive between resolution and respawn. Surfaced to the UI as `task.blockedByPendingPermissions` (computed in `enrichTask`/`enrichTasksBulk`) so the kanban card and task modal show why the task is parked instead of looking generically "stuck".

## DB-Level Invariant: One Running Stage_run per Task (V7)

`migrateV7OneRunningRunPerTask` adds a partial unique index `idx_stage_runs_one_running ON stage_runs(task_id) WHERE status = 'running'`. Defense-in-depth catching multi-spawn cascades at the DB level if a future code path bypasses the runtime re-entry guard or the per-task lock. SQLite throws `SQLITE_CONSTRAINT_UNIQUE` instead of letting two parallel agents burn tokens on the same task. Pre-flight check in the migration aborts loudly (with the offending task IDs in the message) if a legacy DB already contains duplicate running rows. Iterate-flow is safe by construction: OLD → done is committed BEFORE NEW → pending is created, and NEW only flips to running on the next `progressTask` call when OLD is no longer running.

## Schema Validation + Retry

Every agent-driven stage's output is validated against a strict per-stage schema in `validateStageOutput`. A schema mismatch on the first iteration → `iterate` with the validation error and the rejected payload fed back into the next prompt; a second failure → `wait_user`. Hard failures (no session, no parseable JSON) → `fail` immediately without retry. This strategy is also documented in the private `feedback_llm_output_validation` memory.

## Layer Direction (No Upward Imports)

```
server/index.ts  ← composition root (only place that constructs concrete instances)
     │
     ├── creates Dispatcher
     ├── creates PipelineOrchestrator (with onPermissionRequest callback)
     └── creates TaskRouter (with TaskRouterDeps)
              │
              ▼
        routes/taskRoutes.ts ──┐
              │                │
        mcp/mcpServer.ts   ────┤ (both may import from services/*)
              │                │
              │                ▼
              │          services/*  ─────►  db/*
              │ (type-only deps)
              ▼
        pipeline/*  ──────────►  db/*
              │
              └─► (NO import of notifications/ or services/)
```

## Layering Rules

1. `db/*` — imports only `node:*`, `better-sqlite3`, sibling db files, and type-only from `src/types.ts`. Never imports pipeline/notifications/routes/services/mcp.
2. `pipeline/*` — imports `db/*` and `src/types.ts` only. One narrow exception: `server/channelConfig.ts` (pure `node:*` utility, no project-layer dependencies) may also be imported. Never imports `notifications/`, `routes/`, `services/`, or `mcp/`.
3. `services/*` — stateless helpers. Imports only `db/*`, `src/types.ts`, `server/paths.ts`, `server/platform.ts`, and `node:*`. Type-only imports from `pipeline/orchestrator.ts` (e.g. `import type { ... } from '../pipeline/orchestrator'`) are allowed; no runtime dependency on the orchestrator. Never imports `notifications/`, `routes/`, or `mcp/`.
4. `notifications/*` — imports `db/notificationConfigRepo` and `src/types.ts` only. Adapters are private to `dispatcher.ts`.
5. `routes/*` and `mcp/*` — may import from `db/*`, `services/*`, `src/types.ts`, and type-only from `pipeline/orchestrator.ts` and `notifications/dispatcher.ts`. Runtime instances of the orchestrator and dispatcher are injected from `server/index.ts`. Runtime imports from `pipeline/*` are limited to specific named helpers that do not touch the state machine (currently: `resolvedProjectDir` from `pipeline/sessionOutputReader.ts`, used by `routes/taskRoutes.ts`); never import the orchestrator, stage handlers, completion detector, or any other state-machine internals at runtime.
6. `server/index.ts` is the **only** file that instantiates concrete services. It is the composition root.
7. `analytics/*` — stateless analyzers. Imports `db/*`, `paths`, `jsonlParser`, `node:*` only. Never imports `pipeline/`, `notifications/`, `routes/`, or `services/`. Called from `routes/*` and composition root only.

## Why `pipeline/` Does Not Import `notifications/`

The orchestrator must stay agnostic of how users get notified — otherwise unit-testing the state machine would require mocking every adapter, and adding a new notification channel would force a pipeline change. Instead, the orchestrator exposes an `onPermissionRequest` callback on its constructor:

```ts
const orchestrator = new PipelineOrchestrator({
  onPermissionRequest: (taskId, request) => {
    broadcastTaskEvent({ type: 'permission_request', taskId, payload: request })
    void dispatcher.dispatch({ eventType: 'on_hold', /* ...payload */ })
  },
})
```

`server/index.ts` wires this callback to both the SSE broadcast and the notification dispatcher. The orchestrator itself never knows SSE or the dispatcher exists. Tests can pass a spy callback to verify the orchestrator emits the right events without touching the real notification stack.

The same pattern applies inside `StageContext` (`server/pipeline/types.ts`): stage handlers receive `recordAudit` and `requestPermission` as injected functions, not as direct DB or module references.

## Go Layer Direction

The Go backend enforces the same layering intent as the TypeScript rules above, expressed in Go package terms.

### Permitted import graph

```
cmd/serve/main.go + di.go   ← composition root only
        │
        ├── api/           may import: db/repo, db/rawrepo, auth, mcp, pipeline (ProgressOpts + allowlisted helpers — see table below), plugin, sse, merger, config, services
        ├── mcp/tools/     may import: db/repo, pipeline (ProgressOpts + allowlisted helpers), sse, services
        ├── pipeline/      may import: db/repo, db/ent, auth, config, channelconfig, sdk (types only), services
        ├── services/      may import: db/repo, db/ent, auth, config, paths, platform, sdk (types only); never imports pipeline/, api/, mcp/, or plugin/ at runtime
        ├── db/repo        may import: db/ent only
        └── plugin/        may import: auth only
```

`services/` mirrors the TypeScript Rule 3 above: it hosts stateless
helpers (worktree manager, spawner resolver, resource recommender,
approval utils) that may be composed by routes, MCP tools, and the
pipeline itself, but must never reach back into those layers at runtime.
Type-only imports from `pipeline/` are permitted so a service can express
its inputs/outputs in pipeline terms without taking a runtime dependency
on the orchestrator.

### Runtime import whitelist for routes (api/*) and mcp/*

The following `pipeline/` symbols may be imported at runtime from `api/*` and `mcp/*`. All other `pipeline/` imports from these layers must be type-only (`*pipeline.SomeType`):

| Symbol | File in pipeline/ | Consumers |
|---|---|---|
| `ProgressOpts` | `types.go` | `api/tasks/handler.go`, `mcp/tools/control.go` |
| `IsPidAlive` | `session_manager.go` | `api/tasks/enrich.go`, `api/tasks/analyze_routes.go` |
| `ResolvedProjectDir` | `session_reader.go` | `api/tasks/analyze_routes.go` |
| `FindNewestSessionID` | `session_reader.go` | `api/tasks/cost_stage_routes.go` |
| `ReadLastStageJsonOutput` | `session_reader.go` | `api/tasks/cost_stage_routes.go` |
| `ValidateStageOutput` | `completion_detector.go` | `api/agents/channel_stage_output.go` |

These are session-reader and process-monitor helpers — they do not touch the state machine (orchestrator, stage handlers, completion detector). New `pipeline/` imports from `api/*` or `mcp/*` require an explicit justification here before being added.

`ValidateStageOutput` is a pure schema validator with no state-machine touch — used by the channel-stage-output ingress handler (`set_stage_output` MCP tool) to validate agent-submitted output synchronously, so a live agent gets a 422 and can correct without a kill-restart.

Never import from `pipeline/` in `db/` or `plugin/` packages.
<!-- `notifications/` is intentionally absent here: the Go backend has no `notifications/` package
     (this was a TypeScript carry-over). If a Go `notifications/` package is introduced, this
     rule must be updated to explicitly include it. -->

## When Modifying the Pipeline

- New stage transition? Add it to `StageTransition` in `server/pipeline/types.ts`, then handle it in `PipelineOrchestrator.applyTransition`. Wrap DB writes in `db.transaction()`.
- New side effect (metrics, tracing, a new channel)? Add an optional callback to `OrchestratorOptions`, wire it in `server/index.ts`. Do **not** import the side-effect module from inside `pipeline/`.
- New agent-driven stage handler? Use the `createAgentStage(stage, buildPrompt)` factory in `server/pipeline/stageHandlers.ts`. Add a `PromptBuilder` adapter that reads from `StageContext` and delegates to the matching builder in `stagePrompts.ts`. Register it in `handlersByStage`. Spawn, feedback-prefix injection (from `priorIterationOutput`), and `async_running` return are handled by the factory. Then add a per-stage schema validator in `completionDetector.validateStageOutput` so the orchestrator's retry-then-escalate loop recognizes malformed output.
- Custom completion routing (e.g. a stage that loops back to an earlier stage on a specific output flag, like self_review → implementation on `passed:false`)? Edit `PipelineOrchestrator.decideCompletedTransition` and return a `next` transition. If you need to mutate `task.metadata` as part of the transition, pass it via `taskMetadataPatch` on the `next` variant so the write lands inside `applyTransition`'s SQLite transaction — never perform bare `updateTask` calls in the completion path.
- New runner-picker priority tier? Update `comparePickOrder` in `orchestrator.ts` AND the `project_task_pipeline_runner_model` memory, in that order. The memory captures user intent; code changes must stay consistent with it.
- **SSE event coverage for new transitions / mutation paths:** the kanban relies on `onTaskChanged` (fires after every successful `applyTransition`) plus the route-level `broadcastEnrichedUpdate(taskId)` helper in `server/routes/taskRoutes.ts`. Both must produce **enriched** payloads (via `enrichTask`), otherwise the cards lose `latestStageRunStatus` / `needsUser` and the run-status chip vanishes. When you add a new mutation endpoint, call `broadcastEnrichedUpdate(taskId)` — never raw `broadcastTaskEvent({ payload: getTaskById(...) })`. When you add a new transition kind to `applyTransition`, the existing `onTaskChanged` call covers it automatically. A 60s polling fallback in `useTasks.ts` is a safety net only — don't rely on it for primary updates.
