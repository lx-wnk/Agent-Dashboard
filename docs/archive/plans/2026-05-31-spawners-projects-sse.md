# Spawners & Projects SSE Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add real `/api/spawners/stream` and `/api/projects/stream` SSE endpoints that push create/update/delete events, so the existing frontend EventSources connect (200) and stop the 404 polling-flap.

**Architecture:** Mirror the established `sse.TaskBroadcaster` + `/api/tasks/stream` pattern. Add typed `SpawnerBroadcaster`/`ProjectBroadcaster` over the generic `sse.Broadcaster`; give each handler a (nil-safe) broadcaster dep, a `Stream` handler, and emit calls after each successful CRUD mutation; register the routes and construct the broadcasters in the composition root.

**Tech Stack:** Go 1.26 (chi, ent, testing), in-memory `sse.Broadcaster`.

**Spec:** `docs/superpowers/specs/2026-05-31-spawners-projects-sse-design.md`

---

## File Structure

- Create: `server/internal/sse/spawner_broadcaster.go` — `SpawnerEvent` + `SpawnerBroadcaster`.
- Create: `server/internal/sse/project_broadcaster.go` — `ProjectEvent` + `ProjectBroadcaster`.
- Create: `server/internal/sse/domain_broadcaster_test.go` — broadcaster frame tests.
- Modify: `server/internal/api/spawners/handler.go` — broadcaster dep, `Stream`, emit on CRUD.
- Modify: `server/internal/api/spawners/handler_test.go` (create if absent) — emit assertions.
- Modify: `server/internal/api/projects/handler.go` — broadcaster dep, `Stream`, emit on CRUD + folder CRUD.
- Modify: `server/internal/api/projects/handler_test.go` (create if absent) — emit assertions.
- Modify: `server/internal/api/router.go` — register the two stream routes.
- Modify: `server/cmd/serve/di.go` — construct + inject the broadcasters.
- Modify: `server/internal/api/bypass_auth_smoke_test.go` — add the two stream routes to `bypassSkip`.

The reference pattern (`server/internal/sse/task_broadcaster.go`) is:

```go
type TaskEvent struct { Type string `json:"type"`; TaskID string `json:"taskId"`; Payload any `json:"payload,omitempty"` }
type TaskBroadcaster struct { b *Broadcaster }
func NewTaskBroadcaster(b *Broadcaster) *TaskBroadcaster { return &TaskBroadcaster{b: b} }
func (t *TaskBroadcaster) Broadcast(event TaskEvent) { data, err := json.Marshal(event); if err != nil { return }; t.b.Broadcast(data) }
func (t *TaskBroadcaster) Subscribe() chan []byte { return t.b.Subscribe() }
func (t *TaskBroadcaster) Unsubscribe(ch chan []byte) { t.b.Unsubscribe(ch) }
```

The tasks `stream` handler to mirror:

```go
func (h *Handler) stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok { http.Error(w, "streaming not supported", http.StatusInternalServerError); return }
	sse.WriteHeaders(w)
	flusher.Flush()
	sub := h.broadcaster.Subscribe()
	defer h.broadcaster.Unsubscribe(sub)
	for {
		select {
		case data, ok := <-sub:
			if !ok { return }
			w.Write(data) //nolint:errcheck
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
```

---

## PHASE A — sse typed broadcasters

### Task A1: `SpawnerBroadcaster` + `ProjectBroadcaster`

**Files:**
- Create: `server/internal/sse/spawner_broadcaster.go`
- Create: `server/internal/sse/project_broadcaster.go`
- Test: `server/internal/sse/domain_broadcaster_test.go`

- [ ] **Step 1: Write the failing tests**

Create `server/internal/sse/domain_broadcaster_test.go`:

```go
package sse_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

func readFrame(t *testing.T, ch chan []byte) []byte {
	t.Helper()
	select {
	case data := <-ch:
		return data
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SSE frame")
		return nil
	}
}

func TestSpawnerBroadcaster_EmitsTypedFrame(t *testing.T) {
	b := sse.NewSpawnerBroadcaster(sse.NewBroadcaster())
	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	b.Broadcast(sse.SpawnerEvent{Type: "spawner_created", SpawnerID: "s1", Payload: map[string]string{"id": "s1"}})

	var got map[string]any
	if err := json.Unmarshal(readFrame(t, ch), &got); err != nil {
		t.Fatalf("frame not JSON: %v", err)
	}
	if got["type"] != "spawner_created" || got["spawnerId"] != "s1" {
		t.Errorf("frame: got %v", got)
	}
	if got["payload"] == nil {
		t.Error("created event must carry payload")
	}
}

func TestSpawnerBroadcaster_DeletedHasNoPayload(t *testing.T) {
	b := sse.NewSpawnerBroadcaster(sse.NewBroadcaster())
	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	b.Broadcast(sse.SpawnerEvent{Type: "spawner_deleted", SpawnerID: "s1"})

	var got map[string]any
	_ = json.Unmarshal(readFrame(t, ch), &got)
	if _, ok := got["payload"]; ok {
		t.Errorf("deleted event must omit payload, got %v", got)
	}
}

func TestProjectBroadcaster_EmitsTypedFrame(t *testing.T) {
	b := sse.NewProjectBroadcaster(sse.NewBroadcaster())
	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	b.Broadcast(sse.ProjectEvent{Type: "project_updated", ProjectID: "p1", Payload: map[string]string{"id": "p1"}})

	var got map[string]any
	if err := json.Unmarshal(readFrame(t, ch), &got); err != nil {
		t.Fatalf("frame not JSON: %v", err)
	}
	if got["type"] != "project_updated" || got["projectId"] != "p1" {
		t.Errorf("frame: got %v", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/sse/ -run 'TestSpawnerBroadcaster|TestProjectBroadcaster' -v`
Expected: FAIL — `NewSpawnerBroadcaster` / `SpawnerEvent` undefined.

- [ ] **Step 3: Implement the two broadcasters**

Create `server/internal/sse/spawner_broadcaster.go`:

```go
package sse

import "encoding/json"

// SpawnerEvent is a server-sent event for spawner CRUD. JSON keys MUST match the
// frontend SpawnerEvent contract in src/composables/useSpawners.ts.
type SpawnerEvent struct {
	Type      string `json:"type"`
	SpawnerID string `json:"spawnerId"`
	Payload   any    `json:"payload,omitempty"`
}

// SpawnerBroadcaster wraps Broadcaster with typed spawner-event publishing.
type SpawnerBroadcaster struct {
	b *Broadcaster
}

func NewSpawnerBroadcaster(b *Broadcaster) *SpawnerBroadcaster { return &SpawnerBroadcaster{b: b} }

// Broadcast serializes the event and sends it to all SSE subscribers.
// Marshalling errors are dropped — the next reconnect/poll delivers fresh state.
func (s *SpawnerBroadcaster) Broadcast(event SpawnerEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	s.b.Broadcast(data)
}

func (s *SpawnerBroadcaster) Subscribe() chan []byte    { return s.b.Subscribe() }
func (s *SpawnerBroadcaster) Unsubscribe(ch chan []byte) { s.b.Unsubscribe(ch) }
```

Create `server/internal/sse/project_broadcaster.go`:

```go
package sse

import "encoding/json"

// ProjectEvent is a server-sent event for project CRUD. JSON keys MUST match the
// frontend ProjectEvent contract in src/composables/useProjects.ts.
type ProjectEvent struct {
	Type      string `json:"type"`
	ProjectID string `json:"projectId"`
	Payload   any    `json:"payload,omitempty"`
}

// ProjectBroadcaster wraps Broadcaster with typed project-event publishing.
type ProjectBroadcaster struct {
	b *Broadcaster
}

func NewProjectBroadcaster(b *Broadcaster) *ProjectBroadcaster { return &ProjectBroadcaster{b: b} }

func (p *ProjectBroadcaster) Broadcast(event ProjectEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	p.b.Broadcast(data)
}

func (p *ProjectBroadcaster) Subscribe() chan []byte    { return p.b.Subscribe() }
func (p *ProjectBroadcaster) Unsubscribe(ch chan []byte) { p.b.Unsubscribe(ch) }
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd server && go test ./internal/sse/ -v`
Expected: PASS (new + existing broadcaster tests).

- [ ] **Step 5: Commit**

```bash
git add server/internal/sse/spawner_broadcaster.go server/internal/sse/project_broadcaster.go server/internal/sse/domain_broadcaster_test.go
git commit -m "feat(sse): typed Spawner/Project broadcasters"
```

---

## PHASE B — Spawners stream + emit

### Task B1: Spawners handler broadcaster, Stream, CRUD emit

**Files:**
- Modify: `server/internal/api/spawners/handler.go`
- Test: `server/internal/api/spawners/handler_test.go` (create if it does not exist)

- [ ] **Step 1: Write the failing test**

Read whether `server/internal/api/spawners/handler_test.go` exists and how it builds a handler (there may be a fake `repo.SpawnerRepo`). If no test file exists, create one with a minimal in-memory fake repo. The test asserts the broadcaster receives a `spawner_created` frame after `Create`:

```go
package spawners

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

func TestCreate_BroadcastsSpawnerCreated(t *testing.T) {
	// Build the handler with a real ent-backed repo via the package's existing
	// test helper if present; otherwise use the in-memory fake the test file
	// already defines. Attach a SpawnerBroadcaster and subscribe.
	bc := sse.NewSpawnerBroadcaster(sse.NewBroadcaster())
	h := newTestHandler(t) // existing/local helper returning *Handler
	h.broadcaster = bc
	ch := bc.Subscribe()
	defer bc.Unsubscribe(ch)

	r := chi.NewRouter()
	h.Mount(r)
	body := `{"name":"My Spawner","slug":"my-spawner","command":"claude"}`
	req := httptest.NewRequest("POST", "/api/spawners", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != 201 {
		t.Fatalf("create: got %d, want 201; body=%s", rr.Code, rr.Body.String())
	}

	select {
	case data := <-ch:
		var ev map[string]any
		_ = json.Unmarshal(data, &ev)
		if ev["type"] != "spawner_created" {
			t.Errorf("event type: got %v, want spawner_created", ev["type"])
		}
		if ev["payload"] == nil {
			t.Error("spawner_created must carry the spawner payload")
		}
	case <-time.After(time.Second):
		t.Fatal("no spawner_created event broadcast")
	}
	_ = context.Background()
}
```

NOTE: adapt `newTestHandler` + the create body to the package's real repo and validation (the slug must match `^[a-z0-9][a-z0-9-]{0,63}$`, command must be in the allow-list — `claude` is allowed). If the package has no existing in-memory repo fake, build the handler against an ent `:memory:` client via `db.Open(":memory:")` + `repo.NewSpawnerRepo(client)` (see `server/internal/db/repo/helpers_test.go` for `openDB`).

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/api/spawners/ -run TestCreate_Broadcasts -v`
Expected: FAIL — `h.broadcaster` field undefined.

- [ ] **Step 3: Add the broadcaster dep, Stream, and emit helper**

In `server/internal/api/spawners/handler.go`:

Add `"net/http"` is already imported; add `"github.com/lx-wnk/agent-dashboard/server/internal/sse"` to imports.

Change the struct + constructor:

```go
type Handler struct {
	repo        repo.SpawnerRepo
	broadcaster *sse.SpawnerBroadcaster
}

// NewHandler returns a Handler backed by the given repo. broadcaster may be nil
// (e.g. in tests); emit becomes a no-op then.
func NewHandler(r repo.SpawnerRepo, broadcaster *sse.SpawnerBroadcaster) *Handler {
	return &Handler{repo: r, broadcaster: broadcaster}
}
```

Add the emit helper + Stream handler:

```go
func (h *Handler) emit(eventType, id string, payload any) {
	if h.broadcaster == nil {
		return
	}
	h.broadcaster.Broadcast(sse.SpawnerEvent{Type: eventType, SpawnerID: id, Payload: payload})
}

// Stream serves GET /api/spawners/stream — live spawner CRUD events.
func (h *Handler) Stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	sse.WriteHeaders(w)
	flusher.Flush()
	sub := h.broadcaster.Subscribe()
	defer h.broadcaster.Unsubscribe(sub)
	for {
		select {
		case data, ok := <-sub:
			if !ok {
				return
			}
			w.Write(data) //nolint:errcheck
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
```

Add emit calls right before the success return of each mutation:
- In `Create`, change the tail to:

```go
	v := toSpawnerView(s)
	h.emit("spawner_created", s.ID, v)
	apierr.WriteJSON(w, http.StatusCreated, v)
	return nil
```

- In `Update`, before `WriteJSON(... toSpawnerView(s))`, add `h.emit("spawner_updated", s.ID, toSpawnerView(s))` (mirror the Create change: build the view once, emit, write).
- In `Delete`, before `w.WriteHeader(http.StatusNoContent)`, add `h.emit("spawner_deleted", id, nil)`.

NOTE: `Stream` dereferences `h.broadcaster` unconditionally — that is fine because the route is only mounted in DI where a broadcaster is always provided; the nil-guard lives in `emit` for the CRUD paths used by tests.

- [ ] **Step 3b: Keep the build green — add the RouterDeps field + update the one caller**

Changing `NewHandler`'s signature breaks its only caller, `server/internal/api/router.go`. In the SAME commit: add `SpawnerBroadcaster *sse.SpawnerBroadcaster` to the `RouterDeps` struct (import `sse` in router.go is already present), and update the existing spawner-handler construction to pass it:

```go
			spawnersHandler := spawners.NewHandler(deps.SpawnerRepo, deps.SpawnerBroadcaster)
```

`deps.SpawnerBroadcaster` is nil until DI sets it (Task C2) — harmless, `emit` is nil-safe and the stream route is not mounted yet.

- [ ] **Step 4: Run to verify it passes + build**

Run: `cd server && go test ./internal/api/spawners/ -v && go build ./internal/...`
Expected: PASS; `./internal/...` builds clean (the `RouterDeps` field + updated caller keep router.go compiling).

- [ ] **Step 5: Commit**

```bash
git add server/internal/api/spawners/handler.go server/internal/api/spawners/handler_test.go server/internal/api/router.go
git commit -m "feat(spawners): SSE Stream endpoint + emit on CRUD"
```

### Task B2: Register the spawners stream route

**Files:**
- Modify: `server/internal/api/router.go`
- Modify: `server/internal/api/bypass_auth_smoke_test.go`

- [ ] **Step 1: Register the route (read-only → JWT group, NOT admin-gated)**

In `server/internal/api/router.go`, the spawner CRUD handler is mounted inside an `RequireAdminOrBypass` sub-group. The stream is read-only and must be reachable by any authenticated user, so register it on the protected group OUTSIDE the admin sub-group. Where `deps.SpawnerRepo != nil` builds `spawnersHandler`, add a sibling registration on the outer protected `r`:

```go
		if deps.SpawnerRepo != nil {
			r.Group(func(r chi.Router) {
				r.Use(authpkg.RequireAdminOrBypass(deps.Config.BypassAuth))
				spawnersHandler := spawners.NewHandler(deps.SpawnerRepo, deps.SpawnerBroadcaster)
				spawnersHandler.Mount(r)
			})
			// Read-only live stream — JWT-protected but not admin-gated.
			streamHandler := spawners.NewHandler(deps.SpawnerRepo, deps.SpawnerBroadcaster)
			r.Get("/api/spawners/stream", streamHandler.Stream)
		}
```

(`deps.SpawnerBroadcaster` was added to `RouterDeps` in Task B1. Two handler instances share the same broadcaster, which is fine — the broadcaster is the shared fan-out.)

- [ ] **Step 2: Add the stream route to the bypass smoke allow-list**

In `server/internal/api/bypass_auth_smoke_test.go`, in `bypassSkip`, add to the long-lived-SSE case:

```go
	case pattern == "/api/agents/stream", pattern == "/api/tasks/stream",
		pattern == "/api/spawners/stream", pattern == "/api/projects/stream": // long-lived SSE
		return true
```

- [ ] **Step 3: Build + verify**

`deps.SpawnerBroadcaster` already exists on `RouterDeps` (added in B1), so this builds.
Run: `cd server && go build ./internal/... && go test ./internal/api/ -run TestBypassAuth -v`
Expected: build clean; bypass smoke PASS — `/api/spawners/stream` is now registered and skipped by `bypassSkip` (no 401/403).

- [ ] **Step 4: Commit**

```bash
git add server/internal/api/router.go server/internal/api/bypass_auth_smoke_test.go
git commit -m "feat(router): mount /api/spawners/stream (read-only); bypass-skip the SSE routes"
```

---

## PHASE C — Projects stream + emit + DI wiring

### Task C1: Projects handler broadcaster, Stream, CRUD + folder emit

**Files:**
- Modify: `server/internal/api/projects/handler.go`
- Test: `server/internal/api/projects/handler_test.go` (create if absent)

- [ ] **Step 1: Write the failing test**

Mirror the spawners test. Read how the projects package builds a handler in any existing test; otherwise build against `db.Open(":memory:")` + `repo.NewProjectRepo` / `repo.NewProjectFolderRepo`. Assert a `project_created` frame after `Create`:

```go
func TestCreate_BroadcastsProjectCreated(t *testing.T) {
	bc := sse.NewProjectBroadcaster(sse.NewBroadcaster())
	h := newTestHandler(t) // local helper returning *Handler
	h.broadcaster = bc
	ch := bc.Subscribe()
	defer bc.Unsubscribe(ch)

	r := chi.NewRouter()
	h.Mount(r)
	req := httptest.NewRequest("POST", "/api/projects", bytes.NewBufferString(`{"name":"Proj","slug":"proj"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != 201 {
		t.Fatalf("create: got %d, body=%s", rr.Code, rr.Body.String())
	}
	select {
	case data := <-ch:
		var ev map[string]any
		_ = json.Unmarshal(data, &ev)
		if ev["type"] != "project_created" {
			t.Errorf("type: got %v", ev["type"])
		}
	case <-time.After(time.Second):
		t.Fatal("no project_created event")
	}
}
```

(Adapt the create body + validation to the real projects `Create` — check its required fields and slug rule by reading the handler.)

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/api/projects/ -run TestCreate_Broadcasts -v`
Expected: FAIL — `h.broadcaster` undefined.

- [ ] **Step 3: Add broadcaster dep, Stream, emit (incl. folder → project_updated)**

In `server/internal/api/projects/handler.go`:

Add `"github.com/lx-wnk/agent-dashboard/server/internal/sse"` import. Add `broadcaster *sse.ProjectBroadcaster` to the `Handler` struct and a parameter to `NewHandler`:

```go
func NewHandler(p repo.ProjectRepo, f repo.ProjectFolderRepo, tasks TaskProjectOps, broadcaster *sse.ProjectBroadcaster) *Handler {
	return &Handler{projects: p, folders: f, tasks: tasks, broadcaster: broadcaster}
}
```

(Match the real field names in the struct — read it; the constructor currently assigns `p`, `f`, `tasks` to fields.)

Add the emit helper + `Stream` (identical shape to the spawners one, using `sse.ProjectEvent` and `ProjectID`):

```go
func (h *Handler) emit(eventType, id string, payload any) {
	if h.broadcaster == nil {
		return
	}
	h.broadcaster.Broadcast(sse.ProjectEvent{Type: eventType, ProjectID: id, Payload: payload})
}

func (h *Handler) Stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	sse.WriteHeaders(w)
	flusher.Flush()
	sub := h.broadcaster.Subscribe()
	defer h.broadcaster.Unsubscribe(sub)
	for {
		select {
		case data, ok := <-sub:
			if !ok {
				return
			}
			w.Write(data) //nolint:errcheck
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
```

Emit calls:
- `Create`: build the `projectView` it already returns once, `h.emit("project_created", <project id>, view)`, then write it.
- `Update`: `h.emit("project_updated", <id>, view)` before writing.
- `Delete`: `h.emit("project_deleted", <id>, nil)` before `w.WriteHeader(http.StatusNoContent)`.
- `CreateFolder` / `UpdateFolder` / `DeleteFolder`: after the folder mutation succeeds, RELOAD the parent project and its folders using the SAME path `Get` uses to build a `projectView` (fetch project by id + its folders + count → `toProjectView(...)`), then `h.emit("project_updated", <projectID>, reloadedView)`. Reuse the existing fetch/build code from `Get` — do not invent a new query.

- [ ] **Step 3b: Keep the build green — add the RouterDeps field + update the one caller**

Changing `projects.NewHandler`'s signature breaks its only caller, `server/internal/api/router.go`. In the SAME commit: add `ProjectBroadcaster *sse.ProjectBroadcaster` to the `RouterDeps` struct, and update the existing projects-handler construction to pass it:

```go
		projectsHandler := projects.NewHandler(deps.ProjectRepo, deps.ProjectFolderRepo, deps.TaskProjectOps, deps.ProjectBroadcaster)
		projectsHandler.Mount(r)
```

`deps.ProjectBroadcaster` is nil until DI sets it (Task C2) — harmless (`emit` is nil-safe; the stream route is added in C2).

- [ ] **Step 4: Run to verify it passes + build internal**

Run: `cd server && go test ./internal/api/projects/ -v && go build ./internal/...`
Expected: PASS; internal build clean (the `RouterDeps` field + updated caller keep router.go compiling).

- [ ] **Step 5: Commit**

```bash
git add server/internal/api/projects/handler.go server/internal/api/projects/handler_test.go server/internal/api/router.go
git commit -m "feat(projects): SSE Stream endpoint + emit on CRUD and folder changes"
```

### Task C2: Register projects stream route + DI wiring

**Files:**
- Modify: `server/internal/api/router.go`
- Modify: `server/cmd/serve/di.go`

- [ ] **Step 1: Register the projects stream route**

Both `RouterDeps` broadcaster fields already exist (`SpawnerBroadcaster` added in B1, `ProjectBroadcaster` in C1) and the `projects.NewHandler(...)` call already passes `deps.ProjectBroadcaster` (C1). In `server/internal/api/router.go`, in the projects mount block, add the stream route after `projectsHandler.Mount(r)`:

```go
		r.Get("/api/projects/stream", projectsHandler.Stream)
```

- [ ] **Step 2: Construct + inject the broadcasters in DI**

In `server/cmd/serve/di.go`, near where `broadcaster`/`taskBroadcaster` are built:

```go
	spawnerBroadcaster := sse.NewSpawnerBroadcaster(sse.NewBroadcaster())
	projectBroadcaster := sse.NewProjectBroadcaster(sse.NewBroadcaster())
```

Add to the `api.RouterDeps{...}` literal:

```go
		SpawnerBroadcaster: spawnerBroadcaster,
		ProjectBroadcaster: projectBroadcaster,
```

If `cmd/serve` constructs the spawners/projects handlers anywhere itself (it does not — the router builds them from deps/repos), no other change is needed. The router's `spawners.NewHandler(deps.SpawnerRepo, deps.SpawnerBroadcaster)` and `projects.NewHandler(..., deps.ProjectBroadcaster)` now receive the shared broadcasters.

- [ ] **Step 3: Build + vet + smoke + full suite**

Run:
```bash
cd server && go build ./... && go vet ./internal/... ./cmd/...
go test ./internal/api/ -run TestBypassAuth -v
go test ./...
```
Expected: build + vet clean. Bypass smoke PASS (route count up by 2 vs prior baseline, the two new `/stream` routes are skipped by `bypassSkip`, none return 401/403). Full suite green (pre-existing worktree git-signing tests need the SSH key unlocked; the unrelated `useRemotes` frontend test is not part of the Go suite).

- [ ] **Step 4: Commit**

```bash
git add server/internal/api/router.go server/cmd/serve/di.go
git commit -m "feat(router): mount /api/projects/stream; wire domain broadcasters in DI"
```

---

## Final verification

- [ ] **Go suite:** `cd server && go test ./...` → all PASS.
- [ ] **Routes live:** `curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:13120/api/spawners/stream` after rebuild → 200 (was 404). Same for `/api/projects/stream`.
- [ ] **Frontend suite unchanged:** `pnpm test` → the existing `useSpawners`/`useProjects` SSE tests (which assert the EventSource opens on `/stream`) still pass; the pre-existing unrelated `useRemotes` failure is not introduced by this work.
- [ ] **Manual smoke (task dev):** with the dashboard open, create/rename/delete a spawner (Settings → Spawners) and a project in one tab → the list updates live in another tab without a manual refresh; the network tab shows a single open `spawners/stream` / `projects/stream` connection instead of repeating 404s.

## Notes for the implementer

- **Payload = the VIEW, not the ent row:** spawners return `toSpawnerView(s)` and projects return `toProjectView(...)`. Emit the SAME view object so the SSE payload matches what `GET /api/spawners` / `/api/projects` return (the frontend's `Spawner`/`Project` types).
- **Two handler instances sharing one broadcaster** (spawners: one admin-gated for CRUD, one for the read-only stream) is fine — the broadcaster is the shared fan-out; both `NewHandler` calls receive `deps.SpawnerBroadcaster`.
- **Folder changes emit `project_updated`** with the reloaded parent project — there are no folder-specific event types (the frontend has none).
- **`emit` is nil-safe** so handler unit tests can run without a broadcaster; `Stream` assumes a non-nil broadcaster because it is only ever mounted via DI where one is provided.
