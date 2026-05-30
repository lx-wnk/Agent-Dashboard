# Refinement Progress & Persistence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make refinement runs server-tracked and detached so progress is visible in the concept step, survives chat close/reload, and runs in the correct working directory.

**Architecture:** A new `refine.Runner` owns refinement runs in a background goroutine (decoupled from the HTTP request), holds an in-memory status registry, persists the assistant turn on completion, and fires an injected `onRunChange` callback that triggers a Task-SSE broadcast. The tasks handler reads run status via an injected `RefineStatusReader` and surfaces it on the enriched task. The frontend shows a collapsed status panel in the concept step, fixes the reopen-empty lifecycle race, and adds a working-directory selector to the New-Ticket flow.

**Tech Stack:** Go 1.26 (chi, ent, testing), Vue 3 + TypeScript (Vitest), in-memory SSE broadcaster.

**Spec:** `docs/superpowers/specs/2026-05-30-refinement-progress-and-persistence-design.md`

---

## File Structure

**Phase A — detached runner + status + reopen fix**
- Create: `server/internal/refine/runner.go` — `Runner` type, in-memory registry, `Start`/`State`/`IsRunning`/`SetOnRunChange`.
- Create: `server/internal/refine/runner_test.go` — runner unit tests.
- Modify: `server/internal/api/refine/handler.go` — `submitTurn` delegates to the runner; add `GET /status` route + handler.
- Modify: `server/internal/api/refine/handler.go` Deps — add `Runner *refine.Runner`.
- Modify: `server/internal/api/tasks/enrich.go` — add `RefineStatus`/`RefineError` fields to `EnrichedTask`.
- Modify: `server/internal/api/tasks/handler.go` — add `RefineStatusReader` dep + `BroadcastTaskUpdate` exported method + apply refine status after enrich.
- Modify: `cmd/serve/di.go` + `cmd/serve/di_tasks.go` — construct `Runner`, wire it into refine handler + tasks handler, late-bind `onRunChange`.
- Modify: `src/composables/useRefinementChat.ts` — drop message-clear race + `isStreaming` reload guard.
- Modify: `src/components/RefinementChat.vue` — single `[open, taskId]` load watch.
- Create: `src/composables/__tests__/useRefinementChat.reopen.test.ts` — reopen regression (or extend existing test file).

**Phase B — working-directory selector (cwd fix)**
- Modify: `src/components/RefinementChat.vue` — cwd selector in the New-Ticket empty state; pass chosen cwd to `createTask`.
- Modify: `src/composables/__tests__/useRefinementChat.test.ts` — assert chosen cwd passed.

**Phase C — collapsed progress panel**
- Create: `src/components/RefineStatusPanel.vue` — collapsed status + last-output panel.
- Modify: `src/components/TaskModal.vue` — render `RefineStatusPanel` in the concept area.
- Modify: `src/types.ts` — add `refineStatus`/`refineError` to `PipelineTask`.
- Create: `src/components/__tests__/RefineStatusPanel.test.ts` — render-per-status tests.

---

## PHASE A — Detached runner, status surfacing, reopen fix

### Task A1: `refine.Runner` — status registry + State/IsRunning

**Files:**
- Create: `server/internal/refine/runner.go`
- Test: `server/internal/refine/runner_test.go`

- [ ] **Step 1: Write the failing test for the status registry**

Create `server/internal/refine/runner_test.go`:

```go
package refine

import (
	"context"
	"testing"
)

func TestRunner_State_DefaultsToIdle(t *testing.T) {
	r := NewRunner(nil, nil)
	status, errMsg := r.State("task-x")
	if status != StatusIdle {
		t.Errorf("default status: got %q, want %q", status, StatusIdle)
	}
	if errMsg != "" {
		t.Errorf("default errMsg: got %q, want empty", errMsg)
	}
}

func TestRunner_IsRunning_FalseWhenAbsent(t *testing.T) {
	r := NewRunner(nil, nil)
	if r.IsRunning("task-x") {
		t.Error("IsRunning should be false for an unknown task")
	}
	_ = context.Background()
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd server && go test ./internal/refine/ -run TestRunner_State -v`
Expected: FAIL — `NewRunner`, `StatusIdle` undefined (build error).

- [ ] **Step 3: Write the minimal Runner skeleton**

Create `server/internal/refine/runner.go`:

```go
package refine

import (
	"context"
	"sync"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// Run status values surfaced to the UI via the enriched task.
const (
	StatusIdle    = "idle"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"
)

// SpawnFunc spawns a refinement turn and returns a line stream. Matches
// RunRefinementTurn so the real spawner is injectable (and stubbable in tests).
type SpawnFunc func(ctx context.Context, cfg SpawnConfig, sp *ent.Spawner) (<-chan string, error)

type runState struct {
	status string
	errMsg string
}

// Runner owns refinement runs: it spawns claude in a background goroutine
// (decoupled from any HTTP request), tracks per-task status in memory, and
// persists the assistant turn on completion.
type Runner struct {
	turns repo.RefinementTurnRepo
	spawn SpawnFunc

	mu    sync.Mutex
	runs  map[string]*runState
	onRunChange func(taskID string)
}

// NewRunner builds a Runner. spawn defaults to RunRefinementTurn when nil.
func NewRunner(turns repo.RefinementTurnRepo, spawn SpawnFunc) *Runner {
	if spawn == nil {
		spawn = RunRefinementTurn
	}
	return &Runner{turns: turns, spawn: spawn, runs: make(map[string]*runState)}
}

// SetOnRunChange late-binds the status-change callback (the composition root
// wires this to a Task-SSE broadcast after the tasks handler is constructed).
func (r *Runner) SetOnRunChange(fn func(taskID string)) { r.onRunChange = fn }

// State returns the current status and last error for a task.
func (r *Runner) State(taskID string) (status, errMsg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.runs[taskID]
	if !ok {
		return StatusIdle, ""
	}
	return s.status, s.errMsg
}

// IsRunning reports whether a run is currently in flight for the task.
func (r *Runner) IsRunning(taskID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.runs[taskID]
	return ok && s.status == StatusRunning
}

func (r *Runner) setState(taskID, status, errMsg string) {
	r.mu.Lock()
	r.runs[taskID] = &runState{status: status, errMsg: errMsg}
	r.mu.Unlock()
	if r.onRunChange != nil {
		r.onRunChange(taskID)
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd server && go test ./internal/refine/ -run 'TestRunner_State|TestRunner_IsRunning' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/refine/runner.go server/internal/refine/runner_test.go
git commit -m "feat(refine): add Runner status registry skeleton"
```

---

### Task A2: `Runner.Start` — detached run that persists the assistant turn

**Files:**
- Modify: `server/internal/refine/runner.go`
- Test: `server/internal/refine/runner_test.go`

- [ ] **Step 1: Write the failing test (run completes → persists assistant turn, status done)**

Add to `server/internal/refine/runner_test.go`:

```go
import (
	"sync"
	"time"
	// (keep existing imports: context, testing, ent, repo)
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// fakeTurns is a minimal in-memory RefinementTurnRepo for runner tests.
type fakeTurns struct {
	mu      sync.Mutex
	created []repo.CreateTurnInput
}

func (f *fakeTurns) Create(_ context.Context, in repo.CreateTurnInput) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, in)
	return "id", nil
}
func (f *fakeTurns) ListForTask(context.Context, string, int) ([]*ent.RefinementTurn, error) {
	return nil, nil
}
func (f *fakeTurns) ListForTaskNewest(context.Context, string, int) ([]*ent.RefinementTurn, error) {
	return nil, nil
}

func (f *fakeTurns) assistantCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.created {
		if c.Role == "assistant" {
			n++
		}
	}
	return n
}

// waitFor polls until cond() or the deadline; fails the test on timeout.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for: %s", msg)
}

func TestRunner_Start_PersistsAssistantTurnAndMarksDone(t *testing.T) {
	turns := &fakeTurns{}
	spawn := func(_ context.Context, _ SpawnConfig, _ *ent.Spawner) (<-chan string, error) {
		ch := make(chan string, 2)
		ch <- "Hello"
		ch <- "World"
		close(ch)
		return ch, nil
	}
	r := NewRunner(turns, spawn)

	out, err := r.Start("task-1", SpawnConfig{}, nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Drain the tee channel like the HTTP handler would.
	for range out { //nolint:revive
	}
	waitFor(t, func() bool {
		s, _ := r.State("task-1")
		return s == StatusDone
	}, "status done")

	if turns.assistantCount() != 1 {
		t.Errorf("assistant turns persisted: got %d, want 1", turns.assistantCount())
	}
}
```

Note: `time.Now()`/`time.Sleep` here are real-test polling helpers, not production code.

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/refine/ -run TestRunner_Start_Persists -v`
Expected: FAIL — `Start` undefined.

- [ ] **Step 3: Implement `Start`**

Add to `server/internal/refine/runner.go`:

```go
import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	// existing: ent, repo
)

// ErrAlreadyRunning is returned by Start when a run is already in flight.
var ErrAlreadyRunning = errors.New("refine: a run is already in progress for this task")

const runTimeout = 5 * time.Minute

// Start spawns a refinement run in a detached background goroutine and returns
// a tee channel of output lines for the caller (HTTP handler) to forward live.
// The run owns persistence: it writes the assistant turn and updates status
// even if the caller stops reading the tee channel (e.g. client disconnect).
func (r *Runner) Start(taskID string, cfg SpawnConfig, sp *ent.Spawner) (<-chan string, error) {
	r.mu.Lock()
	if s, ok := r.runs[taskID]; ok && s.status == StatusRunning {
		r.mu.Unlock()
		return nil, ErrAlreadyRunning
	}
	r.runs[taskID] = &runState{status: StatusRunning}
	r.mu.Unlock()
	if r.onRunChange != nil {
		r.onRunChange(taskID)
	}

	// Background context — NOT tied to the request. Bounded by runTimeout.
	runCtx, cancel := context.WithTimeout(context.Background(), runTimeout)

	stream, err := r.spawn(runCtx, cfg, sp)
	if err != nil {
		cancel()
		r.setState(taskID, StatusFailed, err.Error())
		return nil, err
	}

	out := make(chan string, 64)
	go func() {
		defer cancel()
		defer close(out)

		var sb strings.Builder
		for line := range stream {
			sb.WriteString(line)
			sb.WriteString("\n")
			// Best-effort tee: never block persistence on a slow/absent reader.
			select {
			case out <- line:
			default:
			}
		}

		resp := strings.TrimRight(sb.String(), "\n")
		switch {
		case resp == "":
			r.setState(taskID, StatusFailed, "no output from refinement agent")
		case strings.HasPrefix(resp, "[ERROR]"):
			r.setState(taskID, StatusFailed, resp)
		default:
			_, _ = r.turns.Create(context.Background(), repo.CreateTurnInput{
				TaskID:  taskID,
				Role:    "assistant",
				Content: resp,
			})
			r.setState(taskID, StatusDone, "")
		}
	}()

	return out, nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd server && go test ./internal/refine/ -run TestRunner_Start_Persists -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/refine/runner.go server/internal/refine/runner_test.go
git commit -m "feat(refine): Runner.Start runs detached and persists the assistant turn"
```

---

### Task A3: Runner — failure + re-entrancy paths

**Files:**
- Test: `server/internal/refine/runner_test.go`

- [ ] **Step 1: Write the failing tests (error→failed, empty→failed, second Start rejected)**

Add to `server/internal/refine/runner_test.go`:

```go
func TestRunner_Start_ErrorLineMarksFailed(t *testing.T) {
	turns := &fakeTurns{}
	spawn := func(_ context.Context, _ SpawnConfig, _ *ent.Spawner) (<-chan string, error) {
		ch := make(chan string, 1)
		ch <- "[ERROR] claude exited: boom"
		close(ch)
		return ch, nil
	}
	r := NewRunner(turns, spawn)
	out, _ := r.Start("t", SpawnConfig{}, nil)
	for range out { //nolint:revive
	}
	waitFor(t, func() bool { s, _ := r.State("t"); return s == StatusFailed }, "failed")
	if turns.assistantCount() != 0 {
		t.Errorf("error run must not persist an assistant turn, got %d", turns.assistantCount())
	}
	_, errMsg := r.State("t")
	if errMsg == "" {
		t.Error("failed run should record an error message")
	}
}

func TestRunner_Start_SpawnErrorMarksFailed(t *testing.T) {
	r := NewRunner(&fakeTurns{}, func(context.Context, SpawnConfig, *ent.Spawner) (<-chan string, error) {
		return nil, errors.New("spawn boom")
	})
	if _, err := r.Start("t", SpawnConfig{}, nil); err == nil {
		t.Fatal("Start should return the spawn error")
	}
	s, _ := r.State("t")
	if s != StatusFailed {
		t.Errorf("status after spawn error: got %q, want failed", s)
	}
}

func TestRunner_Start_RejectsConcurrentRun(t *testing.T) {
	release := make(chan struct{})
	spawn := func(_ context.Context, _ SpawnConfig, _ *ent.Spawner) (<-chan string, error) {
		ch := make(chan string)
		go func() { <-release; close(ch) }()
		return ch, nil
	}
	r := NewRunner(&fakeTurns{}, spawn)
	if _, err := r.Start("t", SpawnConfig{}, nil); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if _, err := r.Start("t", SpawnConfig{}, nil); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("second Start: got %v, want ErrAlreadyRunning", err)
	}
	close(release)
}
```

Add `"errors"` to the test imports.

- [ ] **Step 2: Run to verify they fail or pass**

Run: `cd server && go test ./internal/refine/ -run TestRunner_Start -v`
Expected: PASS (logic from A2 already covers these). If any fail, fix `Start` per the assertion.

- [ ] **Step 3: (No new impl expected — A2 covers it.) If a test fails, adjust `Start`.**

- [ ] **Step 4: Run the full runner package**

Run: `cd server && go test ./internal/refine/ -v`
Expected: PASS, all runner tests.

- [ ] **Step 5: Commit**

```bash
git add server/internal/refine/runner_test.go
git commit -m "test(refine): cover Runner failure and re-entrancy paths"
```

---

### Task A4: Refine handler delegates to the Runner + `/status` route

**Files:**
- Modify: `server/internal/api/refine/handler.go`
- Test: `server/internal/api/refine/handler_test.go`

- [ ] **Step 1: Write the failing test for GET /status**

Add to `server/internal/api/refine/handler_test.go` (the existing `makeRouter` builds a handler; extend `Deps` with a Runner — see Step 3). New test:

```go
func TestStatus_ReturnsIdleForUnknownTask(t *testing.T) {
	turns := &fakeTurnRepo{}
	tasks := &fakeTaskRepo{}
	router := makeRouter(turns, tasks, func(context.Context, refine.SpawnConfig, *ent.Spawner) (<-chan string, error) {
		ch := make(chan string)
		close(ch)
		return ch, nil
	})
	req := withAuth(t, httptest.NewRequest(http.MethodGet, "/api/refine/task-1/status", nil))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rr.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["status"] != "idle" {
		t.Errorf("status field: got %v, want idle", body["status"])
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/api/refine/ -run TestStatus_ReturnsIdle -v`
Expected: FAIL — route 404 / `makeRouter` signature mismatch.

- [ ] **Step 3: Add Runner to Deps, mount /status, delegate submitTurn**

In `server/internal/api/refine/handler.go`:

Add the import `refinesvc "github.com/lx-wnk/agent-dashboard/server/internal/refine"` (alias to avoid clashing with the existing `refine` import — the file already imports `internal/refine` as `refine`; reuse that name, no second alias needed). Add to `Deps`:

```go
	// Runner owns detached refinement runs + status. When nil, NewHandler
	// constructs one from Turns + the default spawner.
	Runner *refine.Runner
```

In `NewHandler`, after the spawner default:

```go
	if deps.Runner == nil {
		deps.Runner = refine.NewRunner(deps.Turns, deps.Spawner)
	}
```

In `Mount`, add:

```go
	r.Get("/api/refine/{taskId}/status", h.status)
```

Add the handler:

```go
// GET /api/refine/{taskId}/status — current refine run status for the task.
func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	status, errMsg := h.deps.Runner.State(taskID)
	resp := map[string]string{"status": status}
	if errMsg != "" {
		resp["error"] = errMsg
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
```

Replace the body of `submitTurn` from the `// Set SSE headers` line onward (the part that builds `turnCtx`, resolves the spawner, calls `h.deps.Spawner`, loops, and stores the assistant turn) with delegation to the runner:

```go
	// Reject a second submit while a run is in flight (prevents duplicate turns).
	if h.deps.Runner.IsRunning(taskID) {
		jsonError(w, "a refinement run is already in progress", http.StatusConflict)
		return
	}

	// Store the user turn only once we know we are starting a new run.
	if _, err := h.deps.Turns.Create(r.Context(), repo.CreateTurnInput{
		TaskID:  taskID,
		Role:    "user",
		Content: body.Message,
	}); err != nil {
		jsonError(w, "failed to store user turn", http.StatusInternalServerError)
		return
	}

	var resolvedSpawner *ent.Spawner
	if h.deps.ResolveSpawner != nil {
		sp, _, err := h.deps.ResolveSpawner(r.Context(), taskID)
		if err != nil {
			jsonError(w, "spawner resolution failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		resolvedSpawner = sp
	}

	out, err := h.deps.Runner.Start(taskID, cfg, resolvedSpawner)
	if err != nil {
		jsonError(w, err.Error(), http.StatusConflict)
		return
	}

	sse.WriteHeaders(w)
	flusher, canFlush := w.(http.Flusher)
	for {
		select {
		case line, open := <-out:
			if !open {
				return
			}
			for _, l := range strings.Split(line, "\n") {
				fmt.Fprintf(w, "data: %s\n", l)
			}
			fmt.Fprint(w, "\n")
			if canFlush {
				flusher.Flush()
			}
		case <-r.Context().Done():
			// Client disconnected — the runner keeps going and persists.
			return
		}
	}
```

IMPORTANT: remove the now-duplicated earlier user-turn `Turns.Create` call and the `history`/`turns`/`workDir`/`cfg` construction must remain BEFORE this block (it builds `cfg`). Keep the history fetch + `cfg` assembly above; only the spawn/stream/persist tail is replaced. Delete the old `done:` label block and the trailing assistant `Turns.Create`.

- [ ] **Step 4: Update `makeRouter` in the test to pass a Runner-backed handler**

In `server/internal/api/refine/handler_test.go`, change `makeRouter` to build the runner from the spawner so existing tests keep working:

```go
func makeRouter(turns *fakeTurnRepo, tasks *fakeTaskRepo, spawner func(context.Context, refine.SpawnConfig, *ent.Spawner) (<-chan string, error)) http.Handler {
	h := NewHandler(Deps{
		Turns:   turns,
		Tasks:   tasks,
		Spawner: spawner,
		Runner:  refine.NewRunner(turns, spawner),
	})
	r := chi.NewRouter()
	r.Use(/* existing auth-injecting middleware used by this test file */)
	h.Mount(r)
	return r
}
```

(Keep the existing middleware that `makeRouter` already used — only the `Deps` gains `Runner`.)

- [ ] **Step 5: Run the refine package tests**

Run: `cd server && go test ./internal/api/refine/ -v`
Expected: PASS — existing turn/submit tests + the new `/status` test. The submit test that streams now reads from the runner tee; if a streaming-submit test asserts the assistant turn is persisted synchronously, change it to poll `fakeTurnRepo` (use a `waitFor`-style helper) since persistence is now asynchronous.

- [ ] **Step 6: Commit**

```bash
git add server/internal/api/refine/handler.go server/internal/api/refine/handler_test.go
git commit -m "refactor(refine): delegate submitTurn to detached Runner; add GET /status"
```

---

### Task A5: Surface `refineStatus` on the enriched task

**Files:**
- Modify: `server/internal/api/tasks/enrich.go`
- Modify: `server/internal/api/tasks/handler.go`
- Test: `server/internal/api/tasks/enrich_test.go` (or a new `refine_status_test.go`)

- [ ] **Step 1: Add fields to EnrichedTask**

In `server/internal/api/tasks/enrich.go`, add to the `EnrichedTask` struct (after `LatestStageRunStatus`):

```go
	RefineStatus *string `json:"refineStatus,omitempty"`
	RefineError  *string `json:"refineError,omitempty"`
```

- [ ] **Step 2: Add the reader interface + dep + apply helper**

In `server/internal/api/tasks/handler.go`:

Define the interface (top of file, package level):

```go
// RefineStatusReader exposes refinement run status to task enrichment without a
// compile-time dependency on the refine runner. Implemented by *refine.Runner.
type RefineStatusReader interface {
	State(taskID string) (status, errMsg string)
}
```

Add `RefineReader RefineStatusReader` to `Deps`, store it on `Handler` (`refineReader RefineStatusReader`), and assign in `NewHandler`.

Add an exported broadcast entrypoint for the runner callback + an apply helper:

```go
// BroadcastTaskUpdate is the runner's onRunChange callback target: it re-enriches
// and broadcasts the task so the kanban + open modal reflect the new run status.
func (h *Handler) BroadcastTaskUpdate(taskID string) {
	h.broadcastEnrichedUpdate(context.Background(), taskID)
}

// applyRefineStatus fills RefineStatus/RefineError from the injected reader.
func (h *Handler) applyRefineStatus(e *EnrichedTask, taskID string) {
	if h.refineReader == nil || e == nil {
		return
	}
	status, errMsg := h.refineReader.State(taskID)
	e.RefineStatus = &status
	if errMsg != "" {
		e.RefineError = &errMsg
	}
}
```

In `broadcastEnrichedEvent`, after `EnrichTask(...)` succeeds and before `Broadcast`, call `h.applyRefineStatus(enriched, taskID)`. In `list`/`get` (wherever single/bulk enriched tasks are returned to the client), call `h.applyRefineStatus` per task. For the bulk list path, loop the enriched slice and apply by `task.ID`.

- [ ] **Step 3: Write the failing test**

Create `server/internal/api/tasks/refine_status_test.go`:

```go
package tasks

import (
	"context"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
)

type stubRefineReader struct{ status, errMsg string }

func (s stubRefineReader) State(string) (string, string) { return s.status, s.errMsg }

func TestApplyRefineStatus_SetsFields(t *testing.T) {
	h := &Handler{refineReader: stubRefineReader{status: "running"}}
	e := &EnrichedTask{}
	h.applyRefineStatus(e, "task-1")
	if e.RefineStatus == nil || *e.RefineStatus != "running" {
		t.Fatalf("RefineStatus: got %v, want running", e.RefineStatus)
	}
	if e.RefineError != nil {
		t.Errorf("RefineError should be nil when no error, got %v", *e.RefineError)
	}
	_ = context.Background()
	_ = (*ent.Task)(nil)
}

func TestApplyRefineStatus_NilReaderNoPanic(t *testing.T) {
	h := &Handler{}
	h.applyRefineStatus(&EnrichedTask{}, "task-1") // must not panic
}
```

(If `Handler` fields are unexported and not settable from a test in the same package — they are, the test is in `package tasks` — this compiles. Adjust the struct literal to whatever the real field names are.)

- [ ] **Step 4: Run to verify pass**

Run: `cd server && go test ./internal/api/tasks/ -run TestApplyRefineStatus -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/api/tasks/enrich.go server/internal/api/tasks/handler.go server/internal/api/tasks/refine_status_test.go
git commit -m "feat(tasks): surface refineStatus/refineError on enriched task"
```

---

### Task A6: Wire the Runner in the composition root

**Files:**
- Modify: `cmd/serve/di.go`
- Modify: `cmd/serve/di_tasks.go`

- [ ] **Step 1: Construct the Runner and inject it (no test — DI wiring, covered by build + smoke)**

In `cmd/serve/di.go`, before building `refineHandler`, construct the runner:

```go
	var refineRunner *refine.Runner
	if entClient != nil {
		refineRunner = refine.NewRunner(repo.NewRefinementTurnRepo(entClient), nil)
	}
```

Add `"github.com/lx-wnk/agent-dashboard/server/internal/refine"` to di.go imports.

Pass `Runner: refineRunner` into `refineapi.Deps{...}`.

`provideTaskHandler` must accept the reader. Change its signature in `cmd/serve/di_tasks.go`:

```go
func provideTaskHandler(client *ent.Client, orch *pipeline.PipelineOrchestrator, tb *sse.TaskBroadcaster, refineReader tasks.RefineStatusReader) *tasks.Handler {
	if client == nil || orch == nil {
		return nil
	}
	taskRepo := repo.NewTaskRepo(client)
	return tasks.NewHandler(tasks.Deps{
		// ...existing fields...
		RefineReader: refineReader,
	})
}
```

In `di.go`, pass the runner: `taskHandler := provideTaskHandler(entClient, orch, taskBroadcaster, refineRunner)`.

After `taskHandler` is built, late-bind the callback:

```go
	if refineRunner != nil && taskHandler != nil {
		refineRunner.SetOnRunChange(taskHandler.BroadcastTaskUpdate)
	}
```

`refineRunner` is a `*refine.Runner` which satisfies `tasks.RefineStatusReader` (it has `State`). If Go complains that the nil-typed `*refine.Runner` passed as the interface is non-nil-but-wrapping-nil, guard: pass `nil` when `refineRunner == nil` (use an `if`/temp interface var).

- [ ] **Step 2: Build + vet**

Run: `cd server && go build ./... && go vet ./internal/... ./cmd/...`
Expected: no output (success).

- [ ] **Step 3: Run the bypass-auth smoke test (auto-covers the new /status route)**

Run: `cd server && go test ./internal/api/ -run TestBypassAuth -v`
Expected: PASS — checked-route count increases by 1 (the new `/api/refine/{taskId}/status`), none return 401/403.

- [ ] **Step 4: Run the full server suite**

Run: `cd server && go test ./...`
Expected: all PASS (worktree git-signing tests require the SSH key unlocked).

- [ ] **Step 5: Commit**

```bash
git add cmd/serve/di.go cmd/serve/di_tasks.go
git commit -m "feat(refine): wire detached Runner + status broadcast in composition root"
```

---

### Task A7: Frontend reopen-empty fix

**Files:**
- Modify: `src/composables/useRefinementChat.ts`
- Modify: `src/components/RefinementChat.vue`
- Test: `src/composables/__tests__/useRefinementChat.test.ts`

- [ ] **Step 1: Write the failing reopen test**

Add to `src/composables/__tests__/useRefinementChat.test.ts`:

```ts
it('reloads turns when the chat is reopened for the same task', async () => {
  // First load returns one turn; simulate a reopen by calling loadHistory twice.
  mockFetchOnce([{ role: 'user', content: 'hi', phase: null }])
  const chat = useRefinementChat(() => 'task-1')
  await chat.loadHistory()
  expect(chat.messages.value).toHaveLength(1)

  // Simulate close: composable reset clears messages (no manual clear race).
  chat.messages.value = []
  // Reopen: history must repopulate even if a prior stream left isStreaming truthy.
  mockFetchOnce([{ role: 'user', content: 'hi', phase: null }, { role: 'assistant', content: 'yo', phase: null }])
  await chat.loadHistory()
  expect(chat.messages.value).toHaveLength(2)
})
```

- [ ] **Step 2: Run to verify current behavior**

Run: `pnpm test src/composables/__tests__/useRefinementChat.test.ts`
Expected: PASS already (loadHistory has no blocking guard in the happy path) OR FAIL if the `isStreaming` guard blocks. If it passes, this test still locks the contract — proceed to harden the component watch in Step 3.

- [ ] **Step 3: Remove the message-clear race in the composable**

In `src/composables/useRefinementChat.ts`, in the `watch(taskId, ...)` callback, remove `messages.value = []` (keep `completedPhases`, `isStreaming`, `approvalReady`, `error` resets). In `loadHistory`, remove the `if (isStreaming.value) return` early-return.

- [ ] **Step 4: Single load watch in the component**

In `src/components/RefinementChat.vue`, replace the separate `watch(() => props.open, ...)` + `onMounted(...)` load triggers with one watch:

```ts
watch(
  [() => props.open, () => currentTask.value?.id],
  ([open, id]) => {
    if (open && id)
      loadHistory()
  },
  { immediate: true },
)
```

Remove the now-redundant `onMounted` load block and the standalone `watch(() => props.open, ...)` loader.

- [ ] **Step 5: Run tests + typecheck + lint**

Run: `pnpm test src/composables/__tests__/useRefinementChat.test.ts && pnpm typecheck && pnpm exec eslint src/composables/useRefinementChat.ts src/components/RefinementChat.vue`
Expected: tests PASS, typecheck clean, lint clean.

- [ ] **Step 6: Commit**

```bash
git add src/composables/useRefinementChat.ts src/components/RefinementChat.vue src/composables/__tests__/useRefinementChat.test.ts
git commit -m "fix(refine): reload chat history reliably on reopen"
```

---

## PHASE B — Working-directory selector (cwd fix)

### Task B1: cwd selector in the New-Ticket flow

**Files:**
- Modify: `src/components/RefinementChat.vue`
- Test: `src/composables/__tests__/useRefinementChat.test.ts`

- [ ] **Step 1: Add a cwd ref + selector UI (only shown for a new ticket)**

In `src/components/RefinementChat.vue` `<script setup>`, add:

```ts
import { fetchProjects } from '../composables/useProjects' // adjust to the real export that returns projects with a path

const cwd = ref('')
const projectPaths = ref<{ label: string, path: string }[]>([])

onMounted(async () => {
  try {
    const projects = await fetchProjects() // returns Project[] with .name and a folder path
    projectPaths.value = projects.flatMap(p => (p.path ? [{ label: p.name, path: p.path }] : []))
  }
  catch { /* selector falls back to free-text input */ }
})
```

(Verify the real projects API: read `src/composables/useProjects.ts` and `src/types.ts` `Project` for the exact path field. If projects expose multiple folders, list each folder path. If no projects API returns a usable path, keep only the free-text input.)

In the New-Ticket empty state template (the `v-if="messages.length === 0"` block), add before the suggestion chips:

```html
<div class="flex flex-col gap-1.5 w-full max-w-md mx-auto">
  <label class="text-[11px] text-muted">Working directory</label>
  <input
    v-model="cwd"
    list="refine-cwd-list"
    placeholder="/absolute/path/to/project"
    class="bg-surface border border-line rounded-md px-3 py-2 text-sm"
  >
  <datalist id="refine-cwd-list">
    <option v-for="p in projectPaths" :key="p.path" :value="p.path">{{ p.label }}</option>
  </datalist>
</div>
```

- [ ] **Step 2: Block send without a cwd, and pass it to createTask**

In `handleSend`, before creating the task:

```ts
if (currentTask.value === null) {
  if (!cwd.value.trim()) {
    error.value = 'Please choose a working directory first'
    inputText.value = msg // restore the message the user typed
    return
  }
  const newTask = await createTask({
    slug: `concept-${Date.now()}`,
    title: 'New Task',
    cwd: cwd.value.trim(),
    stage: 'concept',
  })
  currentTask.value = newTask
  emit('taskCreated', newTask)
}
```

(`error` is already exposed by `useRefinementChat`; if it is read-only in the component, surface a local `cwdError` ref instead and render it.)

- [ ] **Step 3: Write the failing test (createTask receives the chosen cwd, not '/')**

This is a component-level behavior; test via a focused unit by extracting nothing — instead assert through the existing composable test that `createTask` is called with the chosen cwd. Since `createTask` lives in `useTasks`, mock it:

Add to `src/composables/__tests__/useRefinementChat.test.ts` a note that cwd wiring is covered by a component test, and create `src/components/__tests__/RefinementChat.cwd.test.ts`:

```ts
import { mount } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import RefinementChat from '../RefinementChat.vue'

vi.mock('../../composables/useTasks', async (orig) => {
  const actual = await orig() as Record<string, unknown>
  return { ...actual, createTask: vi.fn().mockResolvedValue({ id: 't1', currentStage: 'concept' }) }
})

afterEach(() => vi.restoreAllMocks())

it('creates the task with the chosen cwd, not "/"', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => [] }))
  const { createTask } = await import('../../composables/useTasks')
  const wrapper = mount(RefinementChat, { props: { open: true, task: null } })
  await wrapper.find('input[list="refine-cwd-list"]').setValue('/Users/me/proj')
  await wrapper.find('textarea').setValue('do the thing')
  await wrapper.find('form, [data-testid="refine-send"]').trigger('submit') // adjust selector to the real send trigger
  expect(createTask).toHaveBeenCalledWith(expect.objectContaining({ cwd: '/Users/me/proj' }))
})
```

(Adjust selectors to the real markup — the send button/textarea. If `mount` is heavy, an alternative is to extract `handleSend`'s task-creation into a small testable helper and unit-test that helper directly with the chosen cwd.)

- [ ] **Step 4: Run tests + typecheck + lint**

Run: `pnpm test src/components/__tests__/RefinementChat.cwd.test.ts && pnpm typecheck && pnpm exec eslint src/components/RefinementChat.vue`
Expected: PASS / clean.

- [ ] **Step 5: Commit**

```bash
git add src/components/RefinementChat.vue src/components/__tests__/RefinementChat.cwd.test.ts
git commit -m "feat(refine): choose working directory for new tickets instead of '/'"
```

---

## PHASE C — Collapsed progress panel

### Task C1: `refineStatus` on the PipelineTask type

**Files:**
- Modify: `src/types.ts`

- [ ] **Step 1: Add the optional fields**

In `src/types.ts`, in `interface PipelineTask` (after `latestStageRunStatus`):

```ts
  refineStatus?: 'idle' | 'running' | 'done' | 'failed' | null
  refineError?: string | null
```

- [ ] **Step 2: Typecheck**

Run: `pnpm typecheck`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add src/types.ts
git commit -m "feat(types): add refineStatus/refineError to PipelineTask"
```

---

### Task C2: `RefineStatusPanel.vue`

**Files:**
- Create: `src/components/RefineStatusPanel.vue`
- Test: `src/components/__tests__/RefineStatusPanel.test.ts`

- [ ] **Step 1: Write the failing render tests**

Create `src/components/__tests__/RefineStatusPanel.test.ts`:

```ts
import { mount } from '@vue/test-utils'
import { expect, it } from 'vitest'
import RefineStatusPanel from '../RefineStatusPanel.vue'

it('shows a running badge when status is running', () => {
  const w = mount(RefineStatusPanel, { props: { status: 'running', error: null, lastOutput: '' } })
  expect(w.text().toLowerCase()).toContain('running')
})

it('shows the last output when done', () => {
  const w = mount(RefineStatusPanel, { props: { status: 'done', error: null, lastOutput: 'analysis result' } })
  expect(w.text()).toContain('analysis result')
})

it('shows the error when failed', () => {
  const w = mount(RefineStatusPanel, { props: { status: 'failed', error: 'claude exited: boom', lastOutput: '' } })
  expect(w.text()).toContain('claude exited: boom')
})

it('renders nothing for idle with no output', () => {
  const w = mount(RefineStatusPanel, { props: { status: 'idle', error: null, lastOutput: '' } })
  expect(w.text().trim()).toBe('')
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `pnpm test src/components/__tests__/RefineStatusPanel.test.ts`
Expected: FAIL — component does not exist.

- [ ] **Step 3: Implement the panel**

Create `src/components/RefineStatusPanel.vue`:

```vue
<script setup lang="ts">
import { computed, ref } from 'vue'

const props = defineProps<{
  status: 'idle' | 'running' | 'done' | 'failed' | null
  error: string | null
  lastOutput: string
}>()

const expanded = ref(false)
const show = computed(() => props.status === 'running' || props.status === 'done' || props.status === 'failed')
const badge = computed(() => {
  switch (props.status) {
    case 'running': return { text: 'Running…', cls: 'text-blue-600 dark:text-blue-300' }
    case 'done': return { text: 'Done', cls: 'text-green-600 dark:text-green-400' }
    case 'failed': return { text: 'Failed', cls: 'text-red-600 dark:text-red-400' }
    default: return { text: '', cls: '' }
  }
})
</script>

<template>
  <div v-if="show" class="rounded-md border border-line bg-surface px-3.5 py-2.5 text-sm">
    <button type="button" class="flex items-center gap-2 w-full text-left" @click="expanded = !expanded">
      <span v-if="status === 'running'" class="inline-block h-2 w-2 rounded-full bg-blue-500 animate-pulse" />
      <span class="font-semibold" :class="badge.cls">Refinement: {{ badge.text }}</span>
    </button>
    <p v-if="status === 'failed' && error" class="mt-1.5 text-[0.8rem] text-red-500 whitespace-pre-wrap">{{ error }}</p>
    <pre
      v-else-if="lastOutput"
      class="mt-1.5 text-[0.8rem] text-muted whitespace-pre-wrap overflow-hidden"
      :class="expanded ? '' : 'max-h-16'"
    >{{ lastOutput }}</pre>
  </div>
</template>
```

- [ ] **Step 4: Run to verify it passes**

Run: `pnpm test src/components/__tests__/RefineStatusPanel.test.ts`
Expected: PASS (all 4).

- [ ] **Step 5: Commit**

```bash
git add src/components/RefineStatusPanel.vue src/components/__tests__/RefineStatusPanel.test.ts
git commit -m "feat(refine): collapsed refinement status panel component"
```

---

### Task C3: Render the panel in TaskModal concept area

**Files:**
- Modify: `src/components/TaskModal.vue`
- Test: manual (covered by C2 unit + Phase A smoke)

- [ ] **Step 1: Fetch the last assistant turn for the panel**

In `src/components/TaskModal.vue` `<script setup>`, add a ref + fetch for the latest assistant turn when the task is at concept stage:

```ts
import { ref, watch } from 'vue' // ensure ref/watch imported

const lastRefineOutput = ref('')

async function loadLastRefineOutput(taskId: string) {
  try {
    const res = await fetch(`/api/refine/${taskId}/turns`)
    if (!res.ok) return
    const turns = await res.json() as Array<{ role: string, content: string }>
    const lastAssistant = [...turns].reverse().find(t => t.role === 'assistant')
    lastRefineOutput.value = lastAssistant?.content ?? ''
  }
  catch { /* leave empty */ }
}

watch(
  () => [props.task?.id, props.task?.currentStage, props.task?.refineStatus] as const,
  ([id, stage]) => {
    if (id && stage === 'concept')
      loadLastRefineOutput(id)
  },
  { immediate: true },
)
```

(Use the real prop name for the task in TaskModal — it may be `task` or a computed `task`. Match the existing code.)

- [ ] **Step 2: Render the panel in the concept banner block**

In the concept-stage block (around the "waiting for refinement" banner), add the import `import RefineStatusPanel from './RefineStatusPanel.vue'` and render it inside the concept section:

```html
<RefineStatusPanel
  :status="task.refineStatus ?? 'idle'"
  :error="task.refineError ?? null"
  :last-output="lastRefineOutput"
  class="mb-2"
/>
```

- [ ] **Step 3: Typecheck + lint**

Run: `pnpm typecheck && pnpm exec eslint src/components/TaskModal.vue`
Expected: clean.

- [ ] **Step 4: Build the SPA to confirm no template errors**

Run: `pnpm build`
Expected: build succeeds.

- [ ] **Step 5: Commit**

```bash
git add src/components/TaskModal.vue
git commit -m "feat(refine): show collapsed refinement status panel in concept step"
```

---

## Final verification

- [ ] **Run the full Go suite:** `cd server && go test ./...` → all PASS (SSH signing key unlocked for worktree tests).
- [ ] **Run the full frontend suite:** `pnpm test` → all PASS.
- [ ] **Typecheck + scoped lint:** `pnpm typecheck && pnpm exec eslint src/components/RefinementChat.vue src/components/RefineStatusPanel.vue src/components/TaskModal.vue src/composables/useRefinementChat.ts`
- [ ] **Manual smoke (task dev):** New Ticket → choose a real working dir → send a message → see "Running…" in the concept panel (kanban + modal) → reply renders in chat + panel shows result when done → close + reopen chat shows full history → reload page, open the task, panel still shows status/result.

## Notes for the implementer

- **Layering:** `refine.Runner` must NOT import `api/*` or `sse/*`. The status broadcast is delivered only via the injected `onRunChange` callback wired in `cmd/serve/di.go`. The tasks handler depends on the `RefineStatusReader` interface, not on `*refine.Runner` directly (except in DI).
- **Async persistence:** the assistant turn is now written by the runner goroutine, not synchronously in the handler. Any test asserting persistence must poll (see `waitFor` in `runner_test.go`), not read immediately after the request returns.
- **Existing refine tests:** `submitTurn` no longer kills the process on request-context cancel. A test that cancels the request mid-stream should assert the run still persists (poll `fakeTurnRepo`).
- **cwd selector field name:** confirm the real `Project` path field in `src/types.ts` before wiring the datalist; fall back to free-text only if projects expose no usable path.
