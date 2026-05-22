# Spawn Dialog — Project Picker + Quick-Create

**Date:** 2026-05-22
**Status:** Approved (brainstorm)
**Scope:** UI feature on `SpawnDialog.vue` + backend extension of `/api/agents/spawn`

## Problem

The "New Agent" modal (`src/components/SpawnDialog.vue`) requires the user to type a raw absolute path into a "Working Directory" field, then independently pick a model. The project/folder/spawner data model already exists (commit `863d639`), but the spawn dialog ignores it. Result: every user-initiated spawn is a fresh manual entry, with no link to the project's configured default spawner or model.

## Goal

Let the user pick an existing project (or quickly create one) from the spawn dialog. On selection, hydrate `cwd` from the project's default folder (or chosen folder when there are multiple) and hydrate `model` from the project's default spawner's `modelOverride`. The selected spawner is actually used for the spawn — not just hydration of the model field.

## Non-goals

- ollama / openai adapter dispatch for user-initiated spawns. These are pipeline-only LLM adapters that run as headless API calls inside a stage_run; they do not produce an interactive Claude Code session and have no place in the "New Agent" UX. They are explicitly rejected with HTTP 400.
- Folder CRUD inside the spawn dialog. Folder management stays in `/settings/projects`.
- Multi-spawner-per-project. A project still has at most one `defaultSpawnerId`.

## UX

```
┌─ New Agent ─────────────────────────────────────────┐
│ Prompt              [textarea, required]            │
│                                                     │
│ Project             [— None (manual) — ▼]           │   ← NEW
│   ↳ + Create new project…                            │   ← NEW (last option)
│                                                     │
│ Folder              [default ▼]                      │   ← NEW, conditional
│                       (only rendered when           │
│                        selected project has         │
│                        > 1 folder)                  │
│                                                     │
│ Working Directory   [/path/to/project]              │   ← auto-filled, still editable
│ Model               [Auto ▼]                         │   ← auto-prefilled
│ System Prompt       [textarea, optional]             │
│                                                     │
│ ☑ Enable dashboard control channel                  │
│ ☐ Skip permission prompts                           │
└─────────────────────────────────────────────────────┘
```

### Project select

- First entry: `— None (manual) —` (back-compat default; same as today's behaviour).
- Middle entries: every row from `useProjects()`, sorted alphabetically by `name`.
- Last entry: `+ Create new project…` — opens the inline quick-create panel directly below the select (no nested modal).

### Folder select

- Renders only when the selected project has more than one folder. With one folder it is hidden and that folder is used automatically. With zero folders the project cannot be selected for spawn (the picker shows a disabled state with "no folder — add one in /settings/projects").
- Default-flagged folder is pre-selected.

### Inline quick-create panel

Collapsible block below the project select. Shown when `+ Create new project…` is chosen. Required: `Name`, `Path`. Optional: `Slug`, `Spawner`, `Color`, `Description`.

- `Slug` auto-derives from `Name` via the canonical `slugify()` helper (`src/utils/validation.ts`) but stays editable.
- `Spawner` is a dropdown of all rows from `useSpawners()`; defaults to whichever row has slug `claude-default`.
- Submit:
  1. `POST /api/projects` with `{ name, slug, description, color, defaultSpawnerId }`.
  2. On `201`, `POST /api/projects/{id}/folders` with `{ path, isDefault: true }`.
  3. On both successes, the project is selected (triggering the normal selection flow below).
  4. On folder-create failure, the freshly-created project is deleted (`DELETE /api/projects/{id}`) and the error surfaces in the panel.
- Slug conflict (`409` from `POST /api/projects`) surfaces inline in the panel; no rollback needed.

### Project-selection hydration

When the user picks a non-None project:

1. Fetch folders if not embedded in the project payload (`useProjectFolders(id).load()`).
2. Pick the active folder (default or single; if multi, await user choice in the new dropdown).
3. Set `cwd` to the active folder's `path`. The `cwd` input stays editable for one-off overrides.
4. If `project.defaultSpawnerId` is set, look up the spawner in the `useSpawners()` cache:
   - Set `model` to `spawner.modelOverride ?? ''` (empty = "Auto").
   - Remember the `spawnerId` for the spawn payload.
5. The user can still override `cwd`, `model`, and `systemPrompt` manually.

## Spawn payload

`POST /api/agents/spawn` body gains two optional keys:

```jsonc
{
  "prompt": "…",
  "cwd": "/abs/path",
  "model": "claude-opus-4-7",   // optional, falls back to spawner.modelOverride
  "systemPrompt": "…",
  "enableChannel": true,
  "skipPermissions": false,
  "resumeSessionId": "…",       // existing
  "spawnerId": "spwn_…",        // NEW, optional
  "projectId": "prj_…"          // NEW, optional (observability only)
}
```

## Backend: `/api/agents/spawn`

`server/internal/api/agents/spawn.go` is extended. The `SpawnManager` gains a `repo.SpawnerRepo` dependency injected via the constructor and wired in `server/internal/di.go`.

### Resolution rules (in order)

1. Body has no `spawnerId` → existing path. Run `claudeBin` with current args. Back-compat preserved.
2. `repo.GetByID(ctx, spawnerId)` returns `not found` → HTTP 400 `"spawner not found"`.
3. `adapter_type` is `"ollama"` or `"openai"` → HTTP 400 `"adapter {{type}} not supported for user-initiated spawns; use pipeline tasks instead"`.
4. `adapter_type` is `""`, `"claude"`, or `"custom"` → proceed.

### Command resolution

| `adapter_type` | Command |
|---|---|
| `""` / `"claude"` | `claudeBin` (existing resolution via `exec.LookPath("claude")`). `spawner.args` prepended **before** canonical args (`--resume`, `-p`, `--model`, `--system-prompt`, `--mcp-config`) so user-supplied flags still take precedence. |
| `"custom"` | `spawner.command` + `spawner.args`, then canonical args appended. |

`spawner.command` is re-validated against the spawner command allow-list (built-in + `DASHBOARD_SPAWNER_ALLOWED_COMMANDS`) at spawn time, not just create time. Failure → HTTP 400 `"spawner command not permitted"`.

### Env merge

Mirrors the pipeline rule documented in `conventions.md` and [ADR-0003](../architecture/adr/0003-pluggable-spawners.md):

1. Start with `os.Environ()`.
2. Overlay every key in `spawner.env`.
3. Overlay dashboard-controlled vars (`DASHBOARD_MCP_TOKEN`, `DASHBOARD_MCP_URL`, channel MCP path) — these always win.
4. Strip `DASHBOARD_JWT_SECRET` and `DASHBOARD_HOOKS_SECRET`. These are present in `os.Environ()` because the dashboard process itself was started with them; they must never be forwarded to a child agent process. ADR-0003 mandates this for pipeline-spawned agents; the same rule applies here.

### Model hydration

If the request body has no `model` AND the resolved spawner has a non-nil `modelOverride`, the canonical `--model` arg uses `*spawner.modelOverride`. If the request body provides `model` explicitly, the request body wins.

### Channel injection

The existing `enableChannel` flag is honoured unchanged for `claude` adapter. For `custom` adapter:

- If `spawner.adapter_config["channel_arg"]` is set, the `--mcp-config` flag name is taken from there.
- Otherwise the canonical `--mcp-config <path>` is appended (same as the claude path). If the custom command does not accept `--mcp-config`, the user gets the spawn failure visible in the existing `Status` endpoint stderr capture — no extra UI surface needed.

### Observability

`SpawnStatus` gains a `SpawnerID string` field (omit-empty in JSON). Returned by `/api/agents/spawn/{pid}/status`. `projectId` is logged via `slog.Info("spawn", …)` for audit purposes but not persisted (no spawn-log table today).

## Module boundaries

- `SpawnManager` lives in `server/internal/api/agents/`. New `repo.SpawnerRepo` dep injected via constructor.
- **No new `pipeline/` imports.** Adapter dispatch logic is copied locally rather than shared with `pipeline.LLMSpawner`. Justification: the pipeline spawner assumes a stage_run context (`DASHBOARD_STAGE_RUN_ID`, `DASHBOARD_TASK_ID`, MCP-callback channel). Sharing would force fake stage_run IDs and pollute the state machine. The copy is small (~50 lines) and the two paths diverge in scope.
- Wire DI: `server/internal/di.go` adds `repo.SpawnerRepo` to the `SpawnManager` constructor call. No new providers needed; the repo already exists.
- Layering rules (`task-pipeline.md` "Go Layer Direction"): `api/agents` is allowed to import `db/repo`. No rule change required.

## Frontend module touch list

| File | Change |
|---|---|
| `src/components/SpawnDialog.vue` | Add project select, conditional folder select, inline quick-create panel, hydration logic, payload extension. |
| `src/components/SpawnDialog/QuickCreateProjectPanel.vue` (new) | Encapsulates the create-name/path/slug/spawner/color/description form + create-then-create-folder + rollback. Kept as a separate SFC so `SpawnDialog.vue` doesn't balloon past its current ~285 lines. |
| `src/composables/useSpawnDialog.ts` (new) | Hydration logic (project change → folders → cwd/model). Pulled out so it can be unit-tested without rendering Vue. |
| `src/composables/__tests__/useSpawnDialog.test.ts` (new) | Vitest unit tests for hydration. |
| `src/components/__tests__/SpawnDialog.test.ts` (new) | Vitest component test for selection flow + quick-create rollback. |

## Error handling

| Case | Layer | Behaviour |
|---|---|---|
| `POST /api/projects` returns 409 | FE | Surface "slug already exists" inline in quick-create panel. No rollback. |
| `POST /api/projects/{id}/folders` fails after project create | FE | `DELETE /api/projects/{id}` rollback, surface folder error in quick-create panel. |
| Selected project has zero folders | FE | Project select shows it disabled with hint "no folder — add one in /settings/projects". |
| `spawnerId` refers to deleted spawner | BE | HTTP 400 "spawner not found". |
| `spawner.adapter_type` is `ollama` or `openai` | BE | HTTP 400 with explanatory message (above). |
| `spawner.command` no longer in allow-list | BE | HTTP 400 "spawner command not permitted". |
| `cwd` outside `$HOME` | BE (existing) | Unchanged. |
| Spawn rate-limit exceeded | BE (existing) | Unchanged (HTTP 429). |

## Testing

### Frontend (Vitest)

- `useSpawnDialog.test.ts`
  - Selecting project with single folder → `cwd` is the folder path.
  - Selecting project with multiple folders → `cwd` is the default folder's path, folder dropdown is rendered.
  - Selecting project with `defaultSpawnerId` referencing a spawner with `modelOverride` → `model` is hydrated.
  - Switching back to `— None —` → `cwd`, `model`, `spawnerId` reset to empty.
- `SpawnDialog.test.ts`
  - `+ Create new project…` → panel opens.
  - Submit with valid Name + Path → assert `POST /api/projects` and `POST /api/projects/{id}/folders` called in order; new project becomes selected.
  - Folder-create fails → assert `DELETE /api/projects/{id}` is called and error message renders.
  - Slug conflict → assert error renders and no folder request is fired.

### Backend (Go table-driven)

`server/internal/api/agents/spawn_test.go` extended with a `SpawnerRepo` mock and these cases:

| Case | Expected |
|---|---|
| nil `spawnerId` | existing `claudeBin` path, no env injection from spawner |
| `claude` adapter with `modelOverride` and no `model` in body | `--model {{override}}` added |
| `claude` adapter with `modelOverride` and `model` in body | body `model` wins |
| `custom` adapter with allow-listed command | uses spawner's command + merged args + env |
| `custom` adapter with command not in allow-list | HTTP 400 |
| `ollama` adapter | HTTP 400 with adapter-not-supported message |
| `openai` adapter | HTTP 400 with adapter-not-supported message |
| spawner `env` collides with `DASHBOARD_MCP_TOKEN` | dashboard var wins (assert by inspecting `exec.Cmd.Env`) |
| `spawnerId` references deleted row | HTTP 400 "spawner not found" |

### E2E (Playwright)

`tests/e2e/spawn-with-project.spec.ts`

- Pre-seed: one project with default spawner = `claude-default` and one folder.
- Open New Agent dialog → pick project → assert `cwd` field is auto-filled.
- Click `Spawn Agent` → assert HTTP 200 and PID appears in `/api/agents/spawn/{pid}/status`.
- Open New Agent dialog → `+ Create new project…` → fill Name + Path → submit → assert new project appears in project list and is selected → cancel modal (don't actually spawn).

## Data flow

```
Modal open
    │
    ├── useProjects().startStream() ─── GET /api/projects (cached, SSE)
    └── useSpawners().startStream() ─── GET /api/spawners (cached, SSE)

User selects project P
    │
    ├── if !P.folders → GET /api/projects/{P.id}/folders
    ├── F = single folder OR user-chosen folder OR default folder
    ├── cwd ← F.path
    ├── if P.defaultSpawnerId
    │     └── S ← useSpawners() cache[P.defaultSpawnerId]
    │           ├── model ← S.modelOverride ?? ''
    │           └── spawnerId ← S.id
    └── (else) spawnerId ← null

User clicks "+ Create new project…"
    │
    ├── inline panel expands
    ├── slug auto-derives from name
    ├── submit
    │     ├── POST /api/projects {name,slug,description,color,defaultSpawnerId}
    │     ├── if 201 → POST /api/projects/{id}/folders {path,isDefault:true}
    │     │     ├── if ok → projects list refreshes via SSE; project selected
    │     │     └── if fail → DELETE /api/projects/{id}, surface error
    │     └── if 409 → surface "slug already exists"

User clicks "Spawn Agent"
    │
    └── POST /api/agents/spawn { …, spawnerId?, projectId? }
              │
              ▼
       SpawnManager.Spawn:
         1. cwd validation (existing $HOME check)
         2. if spawnerId
              ├── repo.GetByID → 400 if not found
              ├── reject ollama/openai
              ├── re-validate command against allow-list
              ├── command = claudeBin or spawner.command
              ├── args = spawner.args + canonical args (or vice-versa for custom)
              ├── env = os.Environ() + spawner.env + dashboard-vars (last wins)
              └── if no body.model and spawner.modelOverride → use override
         3. else → existing claude path
         4. exec, store SpawnStatus{ …, SpawnerID }
         5. return { ok, pid }
```

## Open considerations

- The current spawn dialog has no concept of "last used project". A future enhancement could persist the last picked project in `localStorage` and pre-select it; out of scope here.
- Project rate limiting: the spawn-rate-limit currently keys per-user. No project-level cap. Out of scope.
- If `useSpawners()` has not finished loading when the user selects a project, the model field is left at "Auto" and the spawn payload omits `spawnerId`. The user can re-trigger hydration by re-selecting the project after spawners load. This is a non-blocking edge case for the first-second-after-open; acceptable.

## Migration / back-compat

- Existing spawn requests (no `spawnerId`, no `projectId`) keep working unchanged. The dialog defaults to `— None (manual) —`.
- No DB schema changes.
- No env-var changes.
- No new MCP scopes (project + spawner CRUD already require `keys:manage`; the spawn dialog uses the dashboard's session-cookie auth, not MCP).
