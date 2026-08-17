# Task Creation Wizard + Refinement Spawner Resolution

**Date:** 2026-05-22
**Status:** Approved (brainstorm)

## Problem

1. Task creation form lists project as a mid-form dropdown with no inline "create new" option. Users want project context to be the first decision — including the ability to create the project on the fly.
2. The refinement (concept-stage) chat hardcodes `exec.CommandContext(ctx, "claude", "-p", prompt)` in `server/internal/refine/spawner.go`. It ignores the resolved spawner row, so projects/tasks configured to use a non-default spawner (custom Claude wrapper, Ollama, OpenAI, custom command) still get vanilla `claude` during refinement. Pipeline stage handlers already honor `task.spawner_id ?? project.default_spawner_id ?? claude-default` via `pipeline.SpawnerResolver`; refinement does not.

## Goals

- Foreground project selection in task creation: a two-step wizard with optional skip.
- Allow inline project creation in Step 1 (reuse `QuickCreateProjectPanel`).
- Refinement turns resolve their spawner per turn via the same precedence chain as stage handlers.
- All adapter types (`claude`, `ollama`, `openai`, `custom`) gain a streaming variant so SSE refinement output is uniform across spawners.

## Non-Goals

- No project-required migration. Step 1 keeps a "Skip" path; tasks without a project resolve to `claude-default`.
- No snapshot of resolved spawner onto `task.spawner_id` at creation. Resolution is live per turn so spawner edits on the project propagate to in-flight refinements.
- No new "Step 0 — pick template" screen.
- No changes to pipeline stage handlers — they already resolve correctly. Streaming is only required for refinement; stage handlers stay one-shot.
- No model-picker UI inside refinement; the spawner's `default_model` (adapter config) drives the model.

## Decisions

| # | Question | Choice |
|---|---|---|
| 1 | UI shape for "Step 1" | Two-step wizard |
| 2 | Project required? | Optional with skip |
| 3 | How refine picks up spawner | Resolve per turn from DB |
| 4 | Adapter scope in v1 | Full streaming for all adapters |

## Section A — Two-Step Task Creation Wizard

Replace `BacklogForm.vue` body with two-step inner flow. Parent emit contract (`created` event with the created `PipelineTask`) stays unchanged so the Pipeline view is untouched.

### Step 1 — Project

- List existing projects as radio cards (name, path, spawner badge).
- `+ Create new project` action expands `QuickCreateProjectPanel` inline (reused as-is). On `created`, the new project becomes the selected radio.
- `Skip — no project` button as an explicit escape hatch.
- `Next` advances to Step 2; disabled until either a project is selected or skip is chosen.

### Step 2 — Task Details

- Existing fields minus the project dropdown: title, slug, description, cwd combobox, priority, spawner override, permission template.
- When a project was chosen in Step 1:
  - `cwd` pre-fills from the project's default folder (or first folder if no default).
  - Spawner override defaults to "Project default" (empty value).
- When skip was chosen:
  - `cwd` is a plain text input (no datalist).
  - Spawner override defaults to "Claude default".
- `Back` returns to Step 1, retaining Step 2 field state.
- `Create Task` submits — payload is identical to today: `{ slug, title, description?, cwd, priority, template?, projectId?, spawnerId? }`.

### Files Touched

- `src/components/BacklogForm.vue` — refactor into a wizard shell that mounts the two steps and holds shared state.
- New: `src/components/backlog/ProjectStep.vue` — Step 1 UI.
- New: `src/components/backlog/DetailsStep.vue` — Step 2 UI.
- `src/components/QuickCreateProjectPanel.vue` — reused unchanged.
- Tests: extend or add `src/components/BacklogForm.test.ts` covering: select-existing → Step 2 prefilled cwd; create-new → project shows up → Step 2; skip → Step 2 with empty cwd.

## Section B — Refinement Spawner Resolution

### 1. Extend `LLMSpawner` Contract

In `server/internal/pipeline/llm_spawner.go`:

```go
type StreamingLLMSpawner interface {
    LLMSpawner
    SpawnStream(ctx context.Context, args LLMSpawnArgs) (<-chan string, error)
}
```

Adapters implement `StreamingLLMSpawner` where feasible. Callers use a type assertion to discover the capability.

### 2. Per-Adapter Streaming Implementations

- `OllamaSpawner.SpawnStream` — POST `/api/chat` with `stream: true`, scan NDJSON, emit each `message.content` chunk on the channel.
- `OpenAISpawner.SpawnStream` — POST `/v1/chat/completions` with `stream: true`, parse `data: ` SSE chunks, emit each delta's `content`.
- `CustomCommandSpawner.SpawnStream` — exec with stdin = `LLMSpawnArgs` JSON, scan stdout line-by-line. Same contract as the existing pipeline custom adapter, only the consumer differs.
- Claude family (`AdapterType ∈ {"", "claude"}`) — keep `exec` path, but honor the resolved row's `command` (default `"claude"`), `args` (prepended to `-p <prompt>`), and `env` (merged per the standard precedence). Streaming is the current line-scan loop.

### 3. Resolver Injection in Refine Handler

In `server/internal/api/refine/handler.go`:

```go
type Deps struct {
    // ... existing fields ...
    ResolveSpawner pipeline.SpawnerResolverFunc
}
```

Per turn (`submitTurn`):

1. `sp, err := deps.ResolveSpawner(ctx, taskID)` — on error, emit `data: [ERROR] spawner resolution failed: ...` and close.
2. Branch on `sp.AdapterType`:
   - `"" | "claude"` → build `*exec.Cmd` from `sp.Command || "claude"`, append `sp.Args` (validated against allow-list at create time), apply env merge, run current line-scan loop.
   - else → `adapter, err := pipeline.NewLLMSpawnerFromSpawner(sp)`; type-assert `StreamingLLMSpawner`; call `adapter.SpawnStream(ctx, args)`.
3. Forward each chunk as an SSE `data:` frame, flush, persist full assistant turn on close — unchanged from today.

The existing `Deps.Spawner` function pointer stays as the test seam; the default impl now accepts the resolved row.

### 4. DI Wire-Up

In `server/cmd/serve/di.go` (or Wire set), pass `pipeline.SpawnerResolver.Resolve` into the refine handler `Deps`. The resolver is already constructed for stage handlers; refine just borrows the same instance.

### Env-Merge Rules

Identical to stage handlers (`server/internal/pipeline/spawner.go`):

1. Custom spawner `env` (from `spawners.env`) is applied first.
2. Dashboard-controlled vars (`DASHBOARD_*`, `CLAUDE_*`) overlay and always win.
3. `DASHBOARD_JWT_SECRET` and `DASHBOARD_HOOKS_SECRET` are never forwarded.

Refinement does not inject `DASHBOARD_STAGE_RUN_ID` / `DASHBOARD_TASK_ID` — those are pipeline-specific. Refinement only needs `DASHBOARD_MCP_TOKEN` / `DASHBOARD_MCP_URL` if the spawned process is expected to call back (currently it does not).

### Files Touched

- `server/internal/pipeline/llm_spawner.go` — add `StreamingLLMSpawner` interface.
- `server/internal/pipeline/ollama_spawner.go` — add `SpawnStream`.
- `server/internal/pipeline/openai_spawner.go` — add `SpawnStream`.
- `server/internal/pipeline/custom_command_spawner.go` — add `SpawnStream`.
- `server/internal/refine/spawner.go` — accept `*ent.Spawner` (resolved row); branch on `AdapterType`.
- `server/internal/api/refine/handler.go` — add `Deps.ResolveSpawner`; thread it through `submitTurn`.
- `server/cmd/serve/di.go` (or Wire set) — inject the resolver.
- Tests:
  - Per adapter unit test for `SpawnStream` (table-driven, mocked transport / mocked exec).
  - `refine.RunRefinementTurn` table test: resolved row variants → exec vs adapter branch.
  - Handler test: stub `ResolveSpawner`, assert correct branch + SSE frames per adapter type.
  - Integration: claude-default row → exec path still streams (regression guard).

## Section C — Data Flow + Error Handling

### Create-Task Flow (UI)

```
BacklogForm open
  → Step 1: existing project | create new (QuickCreateProjectPanel) | skip
  → Step 2: title/slug/desc/cwd/priority/spawner/permissions  (cwd prefilled from project default folder)
  → POST /api/tasks { projectId?, spawnerId?, ... }
  → backend persists task (task.spawner_id stays nullable)
  → task lands in concept stage
```

### Refinement Turn Flow (Backend)

```
POST /api/refine/{taskId}/turn
  → fetch task
  → ResolveSpawner(ctx, taskID)            // task.spawner_id ?? project.default_spawner_id ?? claude-default
  → branch on AdapterType:
       "" | "claude" → exec path  (sp.Command || "claude")
       other         → StreamingLLMSpawner.SpawnStream
  → stream chunks → SSE `data:` frames → flush
  → on stream close: persist full assistant turn
```

### Error Handling

| Scenario | Behavior |
|---|---|
| `ResolveSpawner` returns error | SSE `data: [ERROR] spawner resolution failed: ...`, close. No fallback to default — surface misconfig loudly. |
| Adapter factory returns `nil` for a non-claude type | SSE `data: [ERROR] adapter %q has no streaming impl`, close. |
| Chunk error mid-stream | Emit `[ERROR] ...` chunk; still persist partial response (preserves current behavior). |
| `sp.Command` not in allow-list | Already validated at spawner create/update time — no re-check at refine time. |
| Step 1 skip | `projectId` omitted; resolver returns `claude-default` row — no special-case code needed. |

## Section D — Testing

### Frontend

- Vitest `BacklogForm.test.ts`:
  - Step 1 select-existing → Step 2 with prefilled cwd from project default folder.
  - Step 1 create-new (mock `createProject`) → new project appears in radio list → Step 2.
  - Step 1 skip → Step 2 with empty cwd and "Claude default" spawner override.
  - Back from Step 2 preserves Step 2 field state.
- Playwright: extend existing backlog-creation E2E with a project pick before fields.

### Backend

- Unit per adapter: `Ollama.SpawnStream` against an `httptest.Server` returning NDJSON; same for OpenAI; `CustomCommandSpawner.SpawnStream` against a fake binary.
- `refine.RunRefinementTurn` table test: resolved spawner row variants → exec or adapter branch.
- Handler test: stub `ResolveSpawner`, assert correct adapter selected per row, assert SSE frames.
- Integration: real `claude-default` row → exec path still streams as before (regression guard).

## Section E — Out of Scope

- Live resolution semantics for `task.spawner_id` snapshot (we explicitly do not snapshot).
- "Step 0 — pick template" screen.
- Project-required migration.
- Model-picker inside refinement (spawner adapter config drives the model).
- LLMSpawner streaming for pipeline stage handlers — stage handlers stay one-shot, only refinement gains streaming.

## Open Items

None.
