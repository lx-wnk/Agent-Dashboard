# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

A real-time monitoring dashboard for locally running Claude Code agents. It reads Claude Code's internal JSONL session logs and process metadata to display token usage, costs, tool activity, tasks, and subagents across all running agent processes.

## Development

```bash
pnpm dev           # Starts Express (port 13120) + Vite SPA middleware with hot reload
pnpm build         # Production build via Vite
pnpm lint          # ESLint check
pnpm test          # Vitest unit tests (single run)
pnpm test:e2e      # Playwright E2E tests
pnpm typecheck     # vue-tsc type checking
```

**Package manager:** pnpm (workspace setup — `pnpm install` in root installs both root and `channel/` dependencies).

## Architecture

**Backend** (Express + Node CLI tools in `server/`):
- `server/platform.ts` — Shared `IS_LINUX` constant for platform detection
- `server/paths.ts` — Shared path constants (`CLAUDE_PROJECTS_DIR`, `SESSION_META_DIR`, `DISCOVERY_DIR`, `WHITESPACE_RE`)
- `server/index.ts` — Express server with `/api/agents` REST + `/api/agents/stream` SSE endpoint, integrates Vite dev middleware
- `server/processScanner.ts` — Uses `ps` and `lsof` to find running `/claude` processes and their working directories
- `server/jsonlParser.ts` — Tail-reads last 32KB of JSONL session files, extracts tokens, model, tools, tasks
- `server/agentMerger.ts` — Matches PIDs to session data, calculates costs via `estimateCost` (from `pricing.ts`), determines status

**Frontend** (Vue 3 + TypeScript SPA in `src/`):
- `src/composables/useAgents.ts` — SSE-first real-time updates via `/api/agents/stream` with polling fallback; manages view mode (list/cards/kanban) with localStorage persistence and search query state
- `src/composables/useAgentPrompt.ts` — Shared composable for sending prompts to agents (via channel or spawn/resume)
- `src/composables/useTheme.ts` — Dark/light theme composable with OS preference detection and localStorage persistence
- `src/App.vue` — Root: header stats + view toggle (list/cards/kanban) + search bar + agent list or card grid or kanban + modal
- `src/components/AgentRow.vue` / `SubAgentRow.vue` — Table rows for list view (subagents indented under parent)
- `src/components/AgentCard.vue` — Card view tile with status, output preview, and inline prompt input
- `src/components/AgentCardGrid.vue` — Responsive grid wrapper for AgentCard tiles
- `src/components/AgentModal.vue` — Full session modal with output transcript, agent details (ToolTimeline, TaskList, SubAgentList), and prompt input
- `src/components/ToolTimeline.vue` — Recent tool pills; expects `:tools` (string[])
- `src/components/TaskList.vue` — Task list with status icons; expects `:tasks` (TaskInfo[])
- `src/components/SubAgentList.vue` — Subagent cards; expects `:subagents` (SubAgent[])
- `src/components/KanbanBoard.vue` — Kanban board aggregating tasks across agents into status columns (pending/in-progress/completed)
- `src/types.ts` — All shared TypeScript interfaces (`Agent`, `TokenUsage`, `SessionMeta`, etc.)
- `src/utils/format.ts` — Token, cost, uptime, and model name formatting utilities (including `shortModel`)

**Pipeline UI** (`src/components/` + `src/composables/`):
- `src/components/PipelineBoard.vue` — kanban-style task pipeline board
- `src/components/TaskCard.vue` — task card with status chip and run-status badge
- `src/components/TaskModal.vue` — full task detail: stage output, chat stream, feedback, permissions
- `src/components/StageOutputView.vue` — renders per-stage LLM output with expand/collapse
- `src/components/AgentChatStream.vue` — live SSE-streamed agent message view
- `src/components/BacklogForm.vue` — task creation / backlog entry form
- `src/composables/useTasks.ts` — SSE-first task list state with 60s polling fallback
- `src/composables/useRole.ts` — role-based access control composable

**Data flow:** Browser connects to `/api/agents/stream` (SSE) with polling fallback → Express scans processes (`ps`/`lsof`) → matches PIDs to `~/.claude/projects/{encoded_path}/{sessionId}.jsonl` → tail-reads JSONL + reads `~/.claude/usage-data/session-meta/{sessionId}.json` → merges, calculates cost/status → broadcasts `Agent[]` to SSE clients.

**Agent status thresholds:** active < 30s, waiting < 5min, idle > 5min (since last activity).

## Task Pipeline Architecture

The Task Pipeline subsystem (`server/db/`, `server/pipeline/`, `server/routes/taskRoutes.ts`, `server/notifications/`) follows a strict layered architecture with callback-based decoupling. See [ADR-0001](docs/architecture/adr/0001-sqlite-for-task-pipeline.md) for why SQLite was added and [ADR-0002](docs/architecture/adr/0002-runner-slot-priority-model.md) for the runner-slot priority model used by the driver loop.

**Key pipeline modules** (`server/pipeline/`):

- `orchestrator.ts` — state machine, `tick()` driver loop, `progressTask()` per-task lock, completion finalization, priority-based runner-slot picker (`pickNextTasksForFreeSlots`).
- `stageHandlers.ts` — `createAgentStage(stage, buildPrompt)` factory that produces real handlers spawning detached Claude processes. `backlogHandler` and `approvalStage` are the only agent-less handlers.
- `stagePrompts.ts` — per-stage `{ systemPrompt, userPrompt }` builders. Each user prompt ends with a `` ```json `` block defining the output schema contract.
- `agentSpawner.ts` — detached `claude` CLI spawn, writes `.claude/settings.json` with pre-approved tool allow-list, injects dashboard-channel MCP.
- `completionDetector.ts` — converts a dead PID + session JSONL into a next/retry/fail decision. Strict per-stage schema validators (`validateStageOutput`) with injectable deps for tests.
- `sessionOutputReader.ts` — reads the last assistant turn from a session JSONL, extracts the `` ```json `` block, falls back to newest-by-mtime session discovery when the stage_run has no `session_id` yet.
- `sessionManager.ts` — `isPidAlive` (with EPERM handling), recovery decisions on orchestrator restart.
- `analysisSpawner.ts` — spawns detached analysis agents (distinct from stage agents) for pre-pipeline analysis tasks.
- `worktreeManager.ts` — creates and removes per-task git worktrees for isolated stage execution; root path via `DASHBOARD_WORKTREE_ROOT`.
- `resourceRecommender.ts` — recommends parallelism cap based on available CPU/memory; consulted when setting `maxParallelOrchestrators`.
- `types.ts` — `StageTransition` union (incl. `async_running`, `taskMetadataPatch` on `next`), `StageContext` with injected `recordAudit` / `requestPermission` side-effects.

**Notifications** (`server/notifications/`):
- `dispatcher.ts` — event-driven dispatcher; registered callbacks come from `server/index.ts` only (never imported by `pipeline/`).
- `adapters/email.ts`, `adapters/webhook.ts`, `adapters/browser.ts`, `adapters/system.ts` — pluggable adapters, wired exclusively in `server/index.ts`.

**Channel bridge** (`channel/dashboard-channel.ts`) — MCP server injected into every spawned stage agent. Reads `DASHBOARD_STAGE_RUN_ID` + `DASHBOARD_TASK_ID` env vars and forwards permission requests back to the orchestrator's `onPermissionRequest` callback via the dashboard API. Enables the two-way permission gate without any pipeline-to-notification dependency.

**Runner slots:** `maxParallelOrchestrators` (pipeline_config key, default 3) is the **global** cap on concurrently-driven tasks — it applies to every agent-driven stage, not just `umsetzung`. Agent-less stages (backlog, approval1/2) bypass the cap. See ADR-0002 for the pickup priority order (silver-bullet → furthest stage → priority → createdAt) and the sticky-run invariant.

**Schema validation + retry:** every agent-driven stage's output is validated against a strict per-stage schema in `validateStageOutput`. A schema mismatch on the first iteration → `iterate` with the validation error and the rejected payload fed back into the next prompt; a second failure → `wait_user`. Hard failures (no session, no parseable JSON) → `fail` immediately without retry. This strategy is also documented in the private `feedback_llm_output_validation` memory.

**Layer direction (no upward imports):**

```
server/index.ts  ← composition root (only place that constructs concrete instances)
     │
     ├── creates Dispatcher
     ├── creates PipelineOrchestrator (with onPermissionRequest callback)
     └── creates TaskRouter (with TaskRouterDeps)
              │
              ▼
        routes/taskRoutes.ts
              │ (type-only deps)
              ▼
        pipeline/*  ──────────►  db/*
              │
              └─► (NO import of notifications/)
```

**Layering rules:**

1. `db/*` — imports only `node:*`, `better-sqlite3`, sibling db files, and type-only from `src/types.ts`. Never imports pipeline/notifications/routes.
2. `pipeline/*` — imports `db/*` and `src/types.ts` only. Never imports `notifications/` or `routes/`.
3. `notifications/*` — imports `db/notificationConfigRepo` and `src/types.ts` only. Adapters are private to `dispatcher.ts`.
4. `routes/taskRoutes.ts` — receives `PipelineOrchestrator` and `Dispatcher` as type-only dependencies via `TaskRouterDeps`; runtime instances are injected from `server/index.ts`.
5. `server/index.ts` is the **only** file that instantiates concrete services. It is the composition root.

**Why `pipeline/` does not import `notifications/`:**

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

**When modifying the pipeline:**

- New stage transition? Add it to `StageTransition` in `server/pipeline/types.ts`, then handle it in `PipelineOrchestrator.applyTransition`. Wrap DB writes in `db.transaction()`.
- New side effect (metrics, tracing, a new channel)? Add an optional callback to `OrchestratorOptions`, wire it in `server/index.ts`. Do **not** import the side-effect module from inside `pipeline/`.
- New agent-driven stage handler? Use the `createAgentStage(stage, buildPrompt)` factory in `server/pipeline/stageHandlers.ts`. Add a `PromptBuilder` adapter that reads from `StageContext` and delegates to the matching builder in `stagePrompts.ts`. Register it in `handlersByStage`. Spawn, feedback-prefix injection (from `priorIterationOutput`), and `async_running` return are handled by the factory. Then add a per-stage schema validator in `completionDetector.validateStageOutput` so the orchestrator's retry-then-escalate loop recognizes malformed output.
- Custom completion routing (e.g. a stage that loops back to an earlier stage on a specific output flag, like selbstreview → umsetzung on `passed:false`)? Edit `PipelineOrchestrator.decideCompletedTransition` and return a `next` transition. If you need to mutate `task.metadata` as part of the transition, pass it via `taskMetadataPatch` on the `next` variant so the write lands inside `applyTransition`'s SQLite transaction — never perform bare `updateTask` calls in the completion path.
- New runner-picker priority tier? Update `comparePickOrder` in `orchestrator.ts` AND the `project_task_pipeline_runner_model` memory, in that order. The memory captures user intent; code changes must stay consistent with it.
- **SSE event coverage for new transitions / mutation paths:** the kanban relies on `onTaskChanged` (fires after every successful `applyTransition`) plus the route-level `broadcastEnrichedUpdate(taskId)` helper in `server/routes/taskRoutes.ts`. Both must produce **enriched** payloads (via `enrichTask`), otherwise the cards lose `latestStageRunStatus` / `needsUser` and the run-status chip vanishes. When you add a new mutation endpoint, call `broadcastEnrichedUpdate(taskId)` — never raw `broadcastTaskEvent({ payload: getTaskById(...) })`. When you add a new transition kind to `applyTransition`, the existing `onTaskChanged` call covers it automatically. A 60s polling fallback in `useTasks.ts` is a safety net only — don't rely on it for primary updates.

## Key Conventions

- Path alias: `@/*` maps to `./src/*` (configured in tsconfig.json and vite.config.ts)
- Server binds to `127.0.0.1` only — never expose to network (reads sensitive session data). **Multi-machine mode** (`DASHBOARD_REMOTES` env var) requires remote instances to be network-accessible; use a VPN or SSH tunnel — never bind to `0.0.0.0` on an untrusted network.
- **Dual persistence model:** agent monitoring is filesystem-derived (no database), task pipeline uses SQLite at `~/.claude/dashboard-tasks.db` (override via `DASHBOARD_DB_PATH`; see ADR-0001). The scope boundary is strict — do not mix.
- **Pipeline env vars:** `DASHBOARD_DB_PATH` (SQLite path), `DASHBOARD_WORKTREE_ROOT` (per-task git worktree root, default `~/.claude/dashboard-worktrees`), `DASHBOARD_STAGE_RUN_ID` + `DASHBOARD_TASK_ID` (injected into spawned stage agents for the channel bridge).
- Subagents discovered from `~/.claude/projects/{encoded_path}/{sessionId}/subagents/*.jsonl`
- Cost estimation uses `MODEL_PRICING` lookup table in `server/pricing.ts`
- **Platform:** macOS and Linux. `server/systemMonitor.ts` uses `top` on macOS and `/proc/stat` on Linux for CPU; `server/processScanner.ts` uses `lsof` on macOS and `/proc/<pid>/cwd` on Linux. Windows is unsupported.
