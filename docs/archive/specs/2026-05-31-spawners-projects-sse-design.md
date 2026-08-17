# SSE streams for spawners and projects

**Date:** 2026-05-31
**Branch:** `feat/layout-redesign`
**Status:** Approved (design) — pending implementation plan

## Problem

`src/composables/useSpawners.ts` and `useProjects.ts` open an `EventSource` on
`/api/spawners/stream` and `/api/projects/stream`, but those routes are not
registered on the backend (only `/api/agents/stream` and `/api/tasks/stream`
exist). Each open returns **404**, the composable falls back to polling, then
re-opens the dead EventSource after `SSE_RETRY_DELAY_MS` — so the app perpetually
flaps between polling and a 404 SSE attempt, spamming the network tab. The code
comments call the SSE endpoint "pending — polling fallback active by design".

Both composables already define the event contracts (`SpawnerEvent`,
`ProjectEvent`) and an `applyEvent` reducer, so the frontend is ready; only the
backend endpoints + event emission are missing.

## Goal

Register real SSE endpoints for spawners and projects that push create/update/
delete events, so the existing frontend EventSource connects (200) and updates
live — eliminating the 404 flapping. Mirror the established
`/api/tasks/stream` + `sse.Broadcaster` pattern exactly.

## Non-goals

- No frontend changes: `useSpawners`/`useProjects` already open the EventSource
  and consume the events via `applyEvent`. Polling remains as the existing
  fallback.
- No folder-specific event types: a project folder change emits a
  `project_updated` carrying the re-loaded parent project.
- No change to the generic `sse.Broadcaster` or the agents/tasks streams.

## Event contracts (must match the frontend exactly)

From the existing composables:

```ts
interface SpawnerEvent { type: 'spawner_created' | 'spawner_updated' | 'spawner_deleted'; spawnerId: string; payload?: unknown }
interface ProjectEvent { type: 'project_created' | 'project_updated' | 'project_deleted'; projectId: string; payload?: unknown }
```

- `*_created` / `*_updated`: `payload` is the full object (a `Spawner` / a
  `Project` with its folders), `spawnerId`/`projectId` set to its id.
- `*_deleted`: `payload` omitted; only `spawnerId`/`projectId` set.

The JSON keys the backend emits MUST be `type`, `spawnerId`/`projectId`,
`payload`.

## Architecture

Mirror `sse.TaskBroadcaster` (which wraps `*sse.Broadcaster`, marshals a typed
event struct to JSON, and broadcasts it as an SSE frame; the stream handler
writes the raw frames to subscribers).

### 1. `sse` package — typed broadcasters

Add to `server/internal/sse/`:

- `SpawnerEvent{ Type string; SpawnerID string; Payload any }` with JSON tags
  `type`, `spawnerId`, `payload,omitempty`.
- `ProjectEvent{ Type string; ProjectID string; Payload any }` with JSON tags
  `type`, `projectId`, `payload,omitempty`.
- `SpawnerBroadcaster` / `ProjectBroadcaster` thin wrappers over `*Broadcaster`,
  each with `NewXBroadcaster(*Broadcaster)`, `Broadcast(event)`,
  `Subscribe() chan []byte`, `Unsubscribe(chan []byte)` — identical shape to
  `TaskBroadcaster`.

### 2. Handlers — broadcaster dep + Stream method + emit on CRUD

`server/internal/api/spawners/handler.go`:
- Add a `*sse.SpawnerBroadcaster` field to the handler (injected; nil-safe — emit
  helpers no-op when nil so existing tests that build the handler without a
  broadcaster keep working).
- Add `Stream(w, r)` — a copy of the tasks `stream` handler (flusher check,
  `sse.WriteHeaders`, subscribe, loop writing raw frames, return on
  `r.Context().Done()`).
- After a successful `Create`: broadcast `spawner_created` with the created
  spawner as payload. After `Update`: `spawner_updated` with the updated spawner.
  After `Delete`: `spawner_deleted` with the id (no payload).

`server/internal/api/projects/handler.go`:
- Same broadcaster dep + `Stream(w, r)`.
- `Create` → `project_created` (payload = created project), `Update` →
  `project_updated`, `Delete` → `project_deleted` (id only).
- `CreateFolder` / `UpdateFolder` / `DeleteFolder` → `project_updated` carrying
  the re-loaded parent project (so the frontend sees the new folder set). Use the
  existing project-fetch path the handler already has (the same data `Get`/`List`
  return, including folders).

A tiny private emit helper per handler keeps the call sites one-liners and
centralizes the nil-broadcaster guard.

### 3. Router — register the stream routes

In `server/internal/api/router.go`, inside the protected JWT group, register:
- `r.Get("/api/spawners/stream", spawnersHandler.Stream)` — note the spawner CRUD
  group is wrapped in `RequireAdminOrBypass`; the **stream is read-only** and
  should be JWT-only (not admin-gated), so register it on the protected group
  directly, NOT inside the admin sub-group.
- `r.Get("/api/projects/stream", projectsHandler.Stream)` alongside the existing
  projects mount.

### 4. Composition root — construct + inject broadcasters

In `cmd/serve/di.go`, build a `*sse.Broadcaster` for each domain, wrap them
(`sse.NewSpawnerBroadcaster(...)`, `sse.NewProjectBroadcaster(...)`), and pass
them into the spawners/projects handler constructors. Start any required pruning
goroutine only if the generic broadcaster needs it (it does not — agents/tasks
broadcasters are constructed plainly).

## Data flow

```
POST /api/spawners → repo.Create OK → SpawnerBroadcaster.Broadcast({type:"spawner_created", spawnerId:id, payload:spawner})
  → all /api/spawners/stream subscribers receive the frame → useSpawners.applyEvent prepends it
PATCH/DELETE → spawner_updated / spawner_deleted likewise
projects + folder CRUD → project_* events (folder change → project_updated with reloaded project)
```

The frontend EventSource now connects (200), so `startSSE` succeeds and the
retry-to-polling flap stops.

## Error handling

- Emit only after the repo mutation succeeds.
- Marshal/write errors inside the broadcaster are dropped silently (matching
  `TaskBroadcaster`); the client's polling fallback / reconnect recovers state.
- A nil broadcaster (tests, or a handler built without one) makes the emit
  helper a no-op.

## Testing

### Go
- `sse`: `SpawnerBroadcaster.Broadcast` / `ProjectBroadcaster.Broadcast` produce a
  frame a subscriber receives whose JSON has `type` + `spawnerId`/`projectId`
  (+ `payload` for created/updated). Mirror `broadcaster_test.go`.
- spawners handler: a subscriber to the broadcaster receives `spawner_created`
  after `Create`, `spawner_updated` after `Update`, `spawner_deleted` (id, no
  payload) after `Delete`. Build the handler WITH a broadcaster in the test.
- projects handler: `project_created/updated/deleted` on CRUD; `project_updated`
  on folder create/update/delete.
- Bypass-auth smoke test (`server/internal/api/bypass_auth_smoke_test.go`):
  ADD `/api/spawners/stream` and `/api/projects/stream` to `bypassSkip`
  (long-lived SSE, exactly like the existing `/api/agents/stream` +
  `/api/tasks/stream` skips). Without this the walk would hold each open for the
  3s timeout.

### Frontend
- No change. Optionally: confirm the existing `useSpawners`/`useProjects` SSE
  tests still pass (they assert the EventSource opens on the `/stream` URL — that
  remains true).

## Implementation phasing (single spec)

- **A:** `sse` typed broadcasters (`SpawnerEvent`/`ProjectEvent` +
  `SpawnerBroadcaster`/`ProjectBroadcaster`) + their tests.
- **B:** spawners handler — broadcaster dep, `Stream`, emit on CRUD; route; DI.
- **C:** projects handler — broadcaster dep, `Stream`, emit on CRUD + folder CRUD;
  route; DI. Add the two routes to the bypass smoke `bypassSkip`.
