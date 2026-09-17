# Routines and Applications in the App — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Routines run their work unattended, human waits never expire, every permission decision continues the run, and MCP applications are added, set up and governed entirely in the dashboard.

**Architecture:** Four PRs in order. PR 1 fixes waiting and decisions in the existing pipeline. PR 2 adds the `job` task kind and a routine run mode on top. PR 3 moves MCP server definitions into the database with an optional export to Claude Code. PR 4 launches a server's own setup UI from the app and replaces the confirmed preset with grants learned on use plus default denies.

**Tech Stack:** Go 1.26 (chi, ent + SQLite, MCP Go SDK), Vue 3 + TypeScript (Vitest, Playwright).

**Specs:** `docs/superpowers/specs/2026-09-17-autonomous-routines-design.md` (routines), `docs/superpowers/specs/2026-09-17-applications-in-the-app-design.md` (applications). Read both before any task.

**Planning mode:** PR 1 is written out in full below. PRs 2–4 are listed as tasks with their spec sections; each is written out in full in this file, against the then-current `main`, before that PR starts. Later PRs build on merged code, so detailing them now would describe code that will have moved.

## Global Constraints

- The server binds to `127.0.0.1`, never `0.0.0.0`.
- No dependency file changes unless a task says so: `server/go.mod`, `server/go.sum`, `go.work.sum`, `pnpm-lock.yaml` stay byte-identical to `main`.
- ent regeneration only via `cd server && go generate ./internal/db/ent/`; afterwards `grep -rl "OnConflict" server/internal/db/ent/ | head` must print files. The generator runs with `-mod=mod` and adds its own dependencies to `server/go.sum`: restore it with `git checkout HEAD -- server/go.sum` before committing.
- While implementing, run package-scoped tests (`go test ./internal/<pkg>/...`). `go test ./...` and `task test` regenerate `server/internal/db/ent/`; run them once per PR at the end and restore `ent/` if it drifted.
- Before every commit: `gofmt -l` on touched packages prints nothing; `go vet ./...` from `server/` passes; `GOTOOLCHAIN=go1.26.6 golangci-lint run` on touched packages reports 0 issues.
- Frontend gate: `pnpm lint && pnpm typecheck && pnpm test`; every component test that mounts calls `wrapper.unmount()`.
- Every new guard gets a test that goes red when the guard is removed. Demonstrate the removal (compiling mutant first), paste the red output, restore from a copy taken before mutating — never with `git checkout`.
- Never pin a Claude model ID in production code; derive it with `claudemodel.Latest` (Go) or `latestModel` (TS). `TestNoPinnedModelIDsOutsideTheCatalog` enforces this.
- Nothing the operator must do on the console: every new capability has its UI path.
- All code, comments, commit messages, PR text in English. Conventional Commits; messages describe behaviour, never task numbers.
- Comments only for a non-obvious contract, timing, edge case, or case-to-effect mapping.
- Docs change with the code: `CHANGELOG.md` (`### Fixed` / `### Added` under `## [Unreleased]`), and the affected guide in `docs/guides/`.
- `gh pr merge --squash --admin`, never `--delete-branch`.

---

## PR 1 — Waiting and decisions

Branch `feat/routine-waits-and-decisions`, worktree `/Users/alexanderwink/dashboard-worktrees/routine-waits`, from `main` at `e54eda3c`. Spec: routines §2.3 (without the routine-context decisions), R4, R5, R6, R8 — plus one gap found while planning (A-1 below).

**A-1 (found 2026-09-17, `VERIFIED`):** `createPermissionRequest` auto-approves every request of an allow-all task (`server/internal/api/tasks/permission_request_routes.go:116`, bulk path `:369`; `taskcontrol.IsAllowAll` is true for `spec_gated` and `full`, and `spec_gated` is the default). That includes MCP application tools, so an agent can grant itself any application tool that is not denied by a grant. Application tools must always wait for a human.

### Task 1: A run waiting for a human has no time limit

**Files:**
- Modify: `server/internal/pipeline/sweeps.go:42` (the wallclock branch of `sweepAwaitingUserRuns`)
- Modify: `server/internal/pipeline/export_test.go` (add one export)
- Create: `server/internal/pipeline/sweeps_awaiting_test.go`

**Interfaces:**
- Produces: `func (o *PipelineOrchestrator) SweepAwaitingUserRunsForTest(ctx context.Context, runs []*ent.StageRun) error` (test export).

- [ ] **Step 1: Add the test export** — append to `server/internal/pipeline/export_test.go`:

```go
// SweepAwaitingUserRunsForTest exposes sweepAwaitingUserRuns for testing.
func (o *PipelineOrchestrator) SweepAwaitingUserRunsForTest(ctx context.Context, runs []*ent.StageRun) error {
	return o.sweepAwaitingUserRuns(ctx, runs)
}
```

- [ ] **Step 2: Write the failing tests** — `server/internal/pipeline/sweeps_awaiting_test.go`:

```go
package pipeline_test

import (
	"context"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
)

func makeAwaitingSweepOrchestrator(t *testing.T) (*pipeline.PipelineOrchestrator, repo.TaskRepo, repo.StageRunRepo, repo.PipelineConfigRepo) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })

	taskRepo := repo.NewTaskRepo(bundle.Client)
	srRepo := repo.NewStageRunRepo(bundle.Client)
	cfgRepo := repo.NewPipelineConfigRepo(bundle.Client)
	orch, err := pipeline.NewOrchestrator(pipeline.OrchestratorOptions{
		TaskRepo:       taskRepo,
		StageRunRepo:   srRepo,
		PermissionRepo: repo.NewPermissionRepo(bundle.Client),
		AuditRepo:      repo.NewAuditEventRepo(bundle.Client),
		ConfigRepo:     cfgRepo,
	})
	require.NoError(t, err)
	return orch, taskRepo, srRepo, cfgRepo
}

func makeAwaitingRun(t *testing.T, ctx context.Context, taskRepo repo.TaskRepo, srRepo repo.StageRunRepo, slug string, pid *int, startedAt time.Time) string {
	t.Helper()
	task, err := taskRepo.Create(ctx, repo.CreateTaskInput{
		Slug: slug, Title: slug, Cwd: "/tmp", CurrentStage: "plan_review",
		Priority: "medium", MaxIterations: 3, StageTimeoutSeconds: 1800,
	})
	require.NoError(t, err)
	sr, err := srRepo.Create(ctx, repo.CreateStageRunInput{TaskID: task.ID, Stage: "plan_review", SessionName: slug + "-0"})
	require.NoError(t, err)
	_, err = srRepo.Update(ctx, sr.ID, repo.UpdateStageRunInput{
		Status:    strPtr("awaiting_user"),
		PID:       pid,
		StartedAt: &startedAt,
		Output:    map[string]any{"plan": "the plan under review"},
	})
	require.NoError(t, err)
	return sr.ID
}

func TestSweepAwaitingUserRuns_NoAgentProcess_NeverTimedOut(t *testing.T) {
	ctx := context.Background()
	orch, taskRepo, srRepo, cfgRepo := makeAwaitingSweepOrchestrator(t)
	require.NoError(t, cfgRepo.Set(ctx, "awaitingUserTimeoutSeconds", "60"))
	runID := makeAwaitingRun(t, ctx, taskRepo, srRepo, "wait-without-agent", nil, time.Now().Add(-2*time.Hour))

	runs, err := srRepo.ListByStatus(ctx, "awaiting_user")
	require.NoError(t, err)
	require.NoError(t, orch.SweepAwaitingUserRunsForTest(ctx, runs))

	got, err := srRepo.GetByID(ctx, runID)
	require.NoError(t, err)
	require.Equal(t, "awaiting_user", got.Status, "a run waiting for a human without an agent process must never be timed out")
}

func TestSweepAwaitingUserRuns_LiveAgentPastLimit_Failed(t *testing.T) {
	ctx := context.Background()
	orch, taskRepo, srRepo, cfgRepo := makeAwaitingSweepOrchestrator(t)
	require.NoError(t, cfgRepo.Set(ctx, "awaitingUserTimeoutSeconds", "60"))

	// The sweep signals the PID's process group, so the live agent is a child
	// in its own group — never the test process.
	cmd := exec.Command("sleep", "60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	pid := cmd.Process.Pid

	runID := makeAwaitingRun(t, ctx, taskRepo, srRepo, "wait-live-agent", &pid, time.Now().Add(-2*time.Hour))
	runs, err := srRepo.ListByStatus(ctx, "awaiting_user")
	require.NoError(t, err)
	require.NoError(t, orch.SweepAwaitingUserRunsForTest(ctx, runs))

	got, err := srRepo.GetByID(ctx, runID)
	require.NoError(t, err)
	require.Equal(t, "failed", got.Status, "a live agent past the limit is still reaped")
}
```

- [ ] **Step 3: Run, expect the first test to fail**

Run: `cd server && go test ./internal/pipeline/ -run 'TestSweepAwaitingUserRuns' -count=1`
Expected: `TestSweepAwaitingUserRuns_NoAgentProcess_NeverTimedOut` FAILS (`"failed"` instead of `"awaiting_user"`); the live-agent test passes.

- [ ] **Step 4: Implement** — in `server/internal/pipeline/sweeps.go` change the wallclock branch condition:

```go
		// Only a live agent can busy-wait. A run whose agent already exited is
		// waiting for a human, and a human wait has no limit.
		if timeoutSec > 0 && run.Pid != nil {
```

(Replaces `if timeoutSec > 0 {` at line 42; a dead PID was already reaped by the branch above, so `run.Pid != nil` here means a live agent.)

- [ ] **Step 5: Run, expect PASS**; then the mutation: temporarily revert the condition to `if timeoutSec > 0 {`, confirm the build, confirm `TestSweepAwaitingUserRuns_NoAgentProcess_NeverTimedOut` fails, restore from the copy.

- [ ] **Step 6: Commit** — `fix(pipeline): never time out a run that is waiting for a human`

### Task 2: A failed run keeps its output

**Files:**
- Modify: `server/internal/pipeline/transitions.go:135-142` (`case FailTransition`)
- Create: `server/internal/pipeline/transitions_fail_output_test.go`

- [ ] **Step 1: Write the failing test**

```go
package pipeline_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
)

func TestFailTransition_KeepsTheRunsExistingOutput(t *testing.T) {
	ctx := context.Background()
	orch, taskRepo, srRepo := makeOrchestratorFull(t)

	task, err := taskRepo.Create(ctx, repo.CreateTaskInput{
		Slug: "fail-keeps-output", Title: "t", Cwd: "/tmp", CurrentStage: "plan_review",
		Priority: "medium", MaxIterations: 3, StageTimeoutSeconds: 1800,
	})
	require.NoError(t, err)
	sr, err := srRepo.Create(ctx, repo.CreateStageRunInput{TaskID: task.ID, Stage: "plan_review", SessionName: "fko-0"})
	require.NoError(t, err)
	sr, err = srRepo.Update(ctx, sr.ID, repo.UpdateStageRunInput{
		Status: strPtr("awaiting_user"),
		Output: map[string]any{"plan": "the plan under review"},
	})
	require.NoError(t, err)

	_, err = orch.ApplyTransitionForTest(ctx, task, sr, pipeline.FailTransition{Reason: "boom"})
	require.NoError(t, err)

	got, err := srRepo.GetByID(ctx, sr.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", got.Status)
	require.Equal(t, "the plan under review", got.Output["plan"], "failing a run must not erase what it produced")
	require.Equal(t, "boom", got.Output["error"])
}
```

- [ ] **Step 2: Run, expect FAIL** — `cd server && go test ./internal/pipeline/ -run TestFailTransition_KeepsTheRunsExistingOutput -count=1` → `plan` is nil.

- [ ] **Step 3: Implement** — replace the output construction in `case FailTransition:`:

```go
	case FailTransition:
		output := make(map[string]any, len(sr.Output)+len(tr.Output)+1)
		for k, v := range sr.Output {
			output[k] = v
		}
		for k, v := range tr.Output {
			output[k] = v
		}
		output["error"] = tr.Reason
```

- [ ] **Step 4: Run, expect PASS**; run `go test ./internal/pipeline/ -count=1` for regressions. Mutation: restore the old `output := tr.Output; if output == nil { output = map[string]any{} }` in a copy-backed edit, confirm red, restore.

- [ ] **Step 5: Commit** — `fix(pipeline): keep a failed run's output and add the error to it`

### Task 3: A refusal continues the run

**Files:**
- Modify: `server/internal/api/tasks/handler.go:1118-1127` (`resolvePermissionRequest`)
- Modify: `server/internal/api/tasks/permission_request_routes.go` (`bulkResolvePermissionRequests`, the `outcome == repo.OutcomeGranted` block; add `deniedResumePrompt`)
- Create: `server/internal/api/tasks/permission_denied_resume_test.go`

**Interfaces:**
- Produces: `func deniedResumePrompt(tools []string) string` (unexported, package `tasks`).

- [ ] **Step 1: Write the failing tests**

```go
package tasks_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
)

// resumeRecorder records ResumeFromUser; every other orchestrator call is a no-op.
type resumeRecorder struct {
	noopOrchestrator
	resumed bool
	prompt  string
}

func (r *resumeRecorder) ResumeFromUser(_ context.Context, _ string, userPrompt string) (*ent.StageRun, error) {
	r.resumed = true
	r.prompt = userPrompt
	return &ent.StageRun{ID: "resumed"}, nil
}

func TestSingleResolve_Denied_ResumesWithTheRefusal(t *testing.T) {
	rec := &resumeRecorder{}
	client, r := newRetryHandler(t, rec)
	taskID, _, reqID := seedPendingPermissionWithPattern(t, client, "deny-resumes", "WebFetch", "domain:example.com")

	url := "/api/tasks/" + taskID + "/permission-requests/" + reqID + "/resolve"
	req := withAuth(t, httptest.NewRequest(http.MethodPost, url, strings.NewReader(`{"outcome":"denied"}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !rec.resumed {
		t.Fatal("a refused request must resume the run; otherwise it waits forever")
	}
	if !strings.Contains(rec.prompt, "WebFetch") {
		t.Errorf("resume prompt must name the refused tool, got %q", rec.prompt)
	}
}

func TestBulkResolve_Denied_ResumesWithTheRefusal(t *testing.T) {
	rec := &resumeRecorder{}
	client, r := newRetryHandler(t, rec)
	taskID, _, _ := seedPendingPermissionWithPattern(t, client, "bulk-deny-resumes", "WebFetch", "domain:example.com")

	body := `{"taskId":"` + taskID + `","outcome":"denied","all":true}`
	req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/permission-requests/bulk-resolve", strings.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !rec.resumed || !strings.Contains(rec.prompt, "WebFetch") {
		t.Fatalf("bulk refusal must resume and name the tool; resumed=%v prompt=%q", rec.resumed, rec.prompt)
	}
}
```

(If `noopOrchestrator` cannot be embedded because of pointer receivers, embed `*noopOrchestrator` initialised as `&resumeRecorder{noopOrchestrator: &noopOrchestrator{}}`; keep the assertions.)

- [ ] **Step 2: Run, expect FAIL** — `cd server && go test ./internal/api/tasks/ -run 'Resolve_Denied_Resumes' -count=1`.

- [ ] **Step 3: Implement** — in `permission_request_routes.go` add:

```go
// deniedResumePrompt is what a resumed agent is told after a human refused tools.
func deniedResumePrompt(tools []string) string {
	return "A human refused permission to use: " + strings.Join(tools, ", ") +
		". Continue without these tools and state in your output what you could not do because of that."
}
```

In `resolvePermissionRequest` (`handler.go`), after the existing `if body.Outcome == repo.OutcomeGranted { … }` block add:

```go
	if body.Outcome == repo.OutcomeDenied {
		if _, err := h.orchestrator.ResumeFromUser(r.Context(), id, deniedResumePrompt([]string{pr.Tool})); err != nil {
			slog.Warn("resolvePermissionRequest: ResumeFromUser after refusal failed", "taskID", id, "err", err)
		}
	}
```

In `bulkResolvePermissionRequests`, after the `if outcome == repo.OutcomeGranted && len(idsToResolve) > 0 { … }` block add:

```go
	if outcome == repo.OutcomeDenied && len(idsToResolve) > 0 {
		resolveSet := make(map[string]bool, len(idsToResolve))
		for _, id := range idsToResolve {
			resolveSet[id] = true
		}
		var tools []string
		for _, req := range pending {
			if resolveSet[req.ID] {
				tools = append(tools, req.Tool)
			}
		}
		if _, err := h.orchestrator.ResumeFromUser(r.Context(), body.TaskID, deniedResumePrompt(tools)); err != nil {
			slog.Warn("bulk_resolve: ResumeFromUser after refusal failed", "taskID", body.TaskID, "err", err)
		}
	}
```

- [ ] **Step 4: Run, expect PASS**; run `go test ./internal/api/tasks/ -count=1`. Mutation per path: drop each new `ResumeFromUser` call (copy-backed), confirm the matching test red, restore.

- [ ] **Step 5: Commit** — `fix(tasks): resume a run after its permission request is refused`

### Task 4: Application tools always wait for a human

**Files:**
- Modify: `server/internal/mcpapps/catalogue.go` (add `IsApplicationTool` next to `CapabilityName`, line 22)
- Create: `server/internal/mcpapps/application_tool_test.go`
- Modify: `server/internal/api/tasks/permission_request_routes.go:116` and `:350-369` (auto-approval)
- Create: `server/internal/api/tasks/permission_application_tool_test.go`

**Interfaces:**
- Produces: `func IsApplicationTool(tool string) bool` in package `mcpapps` — true for `mcp__<server>__<tool>` where `<server>` is not reserved by `channelconfig.IsReservedServerName`.

- [ ] **Step 1: Write the failing tests** — `server/internal/mcpapps/application_tool_test.go`:

```go
package mcpapps_test

import (
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/mcpapps"
)

func TestIsApplicationTool(t *testing.T) {
	cases := map[string]bool{
		"mcp__mail__imap_search_emails":           true,
		"mcp__obsidian__obsidian_read_note":       true,
		"mcp__dashboard-channel__dashboard_reply": false,
		"mcp__dashboard-tasks__list_tasks":        false,
		"Bash":                                    false,
		"mcp__":                                   false,
		"mcp__mail":                               false,
	}
	for tool, want := range cases {
		if got := mcpapps.IsApplicationTool(tool); got != want {
			t.Errorf("IsApplicationTool(%q) = %v, want %v", tool, got, want)
		}
	}
}
```

`server/internal/api/tasks/permission_application_tool_test.go` (mirrors `TestCreatePermissionRequest_AllowAll_AutoApproved` in `autonomy_test.go`):

```go
package tasks_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func TestCreatePermissionRequest_AllowAll_ApplicationToolStaysPending(t *testing.T) {
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	client := bundle.Client
	t.Cleanup(func() { _ = client.Close() })
	_, r := newTestHandlerWithBroadcaster(t, client)

	task, err := repo.NewTaskRepo(client).Create(testCtx(t), repo.CreateTaskInput{
		Slug: "app-tool-pending", Title: "App tool", Cwd: "/tmp/at", CurrentStage: "implementation", Priority: "medium",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err = client.Task.UpdateOneID(task.ID).SetAutonomy("full").Save(testCtx(t)); err != nil {
		t.Fatalf("set autonomy: %v", err)
	}
	sr, err := repo.NewStageRunRepo(client).Create(testCtx(t), repo.CreateStageRunInput{TaskID: task.ID, Stage: "implementation", Iteration: 1})
	if err != nil {
		t.Fatalf("create stage run: %v", err)
	}

	b, _ := json.Marshal(map[string]any{"stageRunId": sr.ID, "tool": "mcp__mail__imap_move_email"})
	req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/permission-requests", bytes.NewReader(b)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var result map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &result)
	if result["outcome"] == "granted" {
		t.Fatal("an application tool must never be auto-approved, whatever the task's autonomy")
	}
}
```

Add the bulk twin `TestBulkCreatePermissionRequests_AllowAll_ApplicationToolStaysPending` posting to the bulk-create route used by `bulkCreatePermissionRequests` (find the route in `Mount` of `handler.go`; body shape: `{"stageRunId": …, "permissions": [{"tool": "mcp__mail__imap_move_email"}, {"tool": "Read"}]}` — confirm the field names in the handler's body struct) and assert: the mail tool's request is not granted, `Read` still is.

- [ ] **Step 2: Run, expect FAIL** — `cd server && go test ./internal/mcpapps/ -run TestIsApplicationTool -count=1` (undefined) and `go test ./internal/api/tasks/ -run 'ApplicationToolStaysPending' -count=1` (granted).

- [ ] **Step 3: Implement** — in `server/internal/mcpapps/catalogue.go`:

```go
// IsApplicationTool reports whether tool names a tool of an attached MCP
// application — mcp__<server>__<tool> with a server the dashboard does not
// reserve for itself.
func IsApplicationTool(tool string) bool {
	rest, ok := strings.CutPrefix(tool, "mcp__")
	if !ok {
		return false
	}
	server, name, ok := strings.Cut(rest, "__")
	if !ok || server == "" || name == "" {
		return false
	}
	return !channelconfig.IsReservedServerName(server)
}
```

(Add the `strings` and `channelconfig` imports if the file lacks them.)

In `permission_request_routes.go`, single path (`:116`):

```go
		if taskErr == nil && taskcontrol.IsAllowAll(task.Autonomy) && !mcpapps.IsApplicationTool(body.Tool) {
```

Bulk path: where each request is auto-approved under `taskIsAllowAll` (around `:369`), skip application tools so they stay pending and flip the run to `awaiting_user` like a gated request:

```go
		if taskIsAllowAll && !mcpapps.IsApplicationTool(p.Tool) {
```

(Use the loop variable's actual name; make sure `hasNewRequests` becomes true when an application tool stays pending, so the run is flipped and the event broadcast exactly as for a gated task.)

- [ ] **Step 4: Run, expect PASS**; `go test ./internal/mcpapps/ ./internal/api/tasks/ -count=1`. Mutations: drop `!mcpapps.IsApplicationTool(...)` on each path (copy-backed), confirm red, restore; make `IsApplicationTool` ignore reserved names, confirm the reserved cases red, restore.

- [ ] **Step 5: Commit** — `fix(tasks): never auto-approve an MCP application tool`

### Task 5: Push notification when a run waits for a decision

**Files:**
- Modify: `server/internal/api/tasks/handler.go` (`Deps.Notifier`, `Handler.notifier`, interface `PermissionNotifier`)
- Modify: `server/internal/api/tasks/permission_request_routes.go` (call the notifier for requests that stay pending)
- Create: `server/internal/api/tasks/permission_notify_test.go`
- Modify: `server/serverapp/di.go:188-194` (hoist `wpSvc`), `server/serverapp/di.go:660` and `server/serverapp/di_tasks.go:16-47` (pass the notifier)
- Create: `server/serverapp/push_notifier.go` and `server/serverapp/push_notifier_test.go`
- Modify: `src/sw.ts` (push and notificationclick listeners); Create: `src/utils/pushPayload.ts`, `src/utils/pushPayload.test.ts`; Modify: `src/sw.test.ts`

**Interfaces:**
- Produces (package `tasks`):

```go
// PermissionNotifier tells the operator that a run waits for a permission decision.
type PermissionNotifier interface {
	PermissionRequested(ctx context.Context, taskID, taskTitle, tool string)
}
```

- Produces (package `serverapp`): `func newPushPermissionNotifier(svc *wpservice.Service) tasks.PermissionNotifier` — returns nil when `svc` is nil.
- Produces (TS): `export interface PushNotice { title: string, body: string, url: string, tag: string }` and `export function parsePushNotice(data: unknown): PushNotice | null` in `src/utils/pushPayload.ts`.

- [ ] **Step 1: Failing Go test for the handler** — `permission_notify_test.go`: a `notifierRecorder` with a `calls []string` slice; construct the handler through `tasks.NewHandler(tasks.Deps{…, Notifier: rec})` (copy the dependency set of `newTestHandlerWithBroadcaster`); post one request for a `manual` task (`Bash`, pattern `make build`) → exactly one call naming task id and `Bash`; post one for a `full` task with `Read` (auto-approved) → no call; post one for a `full` task with `mcp__mail__imap_move_email` → one call.

- [ ] **Step 2: Failing Go test for the payload** — `server/serverapp/push_notifier_test.go`: `pushPermissionPayload("t1", "Triage inbox", "mcp__mail__imap_move_email")` returns JSON that unmarshals to `title == "Approval needed"`, `body == "Triage inbox: mcp__mail__imap_move_email"`, `url == "/"`, `tag == "permission-t1"`; `newPushPermissionNotifier(nil) == nil`.

- [ ] **Step 3: Implement the Go side**

`handler.go`: add `Notifier PermissionNotifier` to `Deps`, `notifier PermissionNotifier` to `Handler`, assign in `NewHandler`, and the interface above.

`permission_request_routes.go`, single path — after the auto-approve block, when the request is still pending:

```go
	if req.Outcome == nil && h.notifier != nil && srErr == nil {
		title := ""
		if task, err := h.taskRepo.GetByID(r.Context(), sr.TaskID); err == nil {
			title = task.Title
		}
		h.notifier.PermissionRequested(r.Context(), sr.TaskID, title, body.Tool)
	}
```

(Check the pending representation on `ent.PermissionRequest` — an unset outcome or a status field — and use it; the test from Step 1 decides.) Bulk path: one call per request that stays pending.

`server/serverapp/push_notifier.go`:

```go
package serverapp

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/lx-wnk/agent-dashboard/server/internal/api/tasks"
	wpservice "github.com/lx-wnk/agent-dashboard/server/internal/webpush"
)

type pushPermissionNotifier struct{ svc *wpservice.Service }

func newPushPermissionNotifier(svc *wpservice.Service) tasks.PermissionNotifier {
	if svc == nil {
		return nil
	}
	return pushPermissionNotifier{svc: svc}
}

func pushPermissionPayload(taskID, taskTitle, tool string) []byte {
	b, _ := json.Marshal(map[string]string{
		"title": "Approval needed",
		"body":  taskTitle + ": " + tool,
		"url":   "/",
		"tag":   "permission-" + taskID,
	})
	return b
}

func (n pushPermissionNotifier) PermissionRequested(ctx context.Context, taskID, taskTitle, tool string) {
	if _, err := n.svc.SendToAll(ctx, pushPermissionPayload(taskID, taskTitle, tool)); err != nil {
		slog.Warn("push: permission request notification failed", "taskID", taskID, "tool", tool, "err", err)
	}
}
```

`di.go`: declare `var wpSvc *wpservice.Service` before the `if bundle != nil` block at line 188 and assign inside it (`wpSvc = wpservice.NewService(...)`). `di_tasks.go`: add a `notifier tasks.PermissionNotifier` parameter to `provideTaskHandler` and set `Notifier: notifier`. `di.go:660`: pass `newPushPermissionNotifier(wpSvc)`. Returning a nil interface (not a typed nil) keeps `h.notifier != nil` meaningful.

- [ ] **Step 4: Failing TS tests** — `src/utils/pushPayload.test.ts`: `parsePushNotice({title:'Approval needed', body:'x: Bash', url:'/', tag:'permission-t1'})` returns the object; missing `title` or non-object input returns `null`. `src/sw.test.ts`: register listeners as the existing test does; dispatch `push` with `data.json()` returning a valid notice and assert `self.registration.showNotification('Approval needed', expect.objectContaining({ body: 'x: Bash', tag: 'permission-t1' }))` inside `waitUntil`; dispatch `notificationclick` and assert it closes the notification and focuses or opens `/`.

- [ ] **Step 5: Implement the TS side** — `src/utils/pushPayload.ts`:

```ts
export interface PushNotice {
  title: string
  body: string
  url: string
  tag: string
}

export function parsePushNotice(data: unknown): PushNotice | null {
  if (typeof data !== 'object' || data === null)
    return null
  const d = data as Record<string, unknown>
  if (typeof d.title !== 'string' || d.title === '')
    return null
  return {
    title: d.title,
    body: typeof d.body === 'string' ? d.body : '',
    url: typeof d.url === 'string' ? d.url : '/',
    tag: typeof d.tag === 'string' ? d.tag : '',
  }
}
```

`src/sw.ts` — the service worker cannot import at runtime except through the build, which already bundles `./utils/...` imports (see the existing `swConstants` import), so import `parsePushNotice` and add:

```ts
self.addEventListener('push', (event) => {
  let raw: unknown = null
  try {
    raw = event.data?.json()
  }
  catch {
    raw = null
  }
  const notice = parsePushNotice(raw)
  if (!notice)
    return
  event.waitUntil(self.registration.showNotification(notice.title, {
    body: notice.body,
    tag: notice.tag,
    data: { url: notice.url },
  }))
})

self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  const url = (event.notification.data as { url?: string } | undefined)?.url ?? '/'
  event.waitUntil((async () => {
    const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
    const open = windows.find(w => 'focus' in w)
    if (open)
      return (open as WindowClient).focus()
    return self.clients.openWindow(url)
  })())
})
```

- [ ] **Step 6: Run all tests, expect PASS** — `cd server && go test ./internal/api/tasks/ ./serverapp/ -count=1` and `pnpm exec vitest run src/utils/pushPayload.test.ts src/sw.test.ts`. Mutation: remove the notifier call on the single path (copy-backed) → Step 1 test red; restore.

- [ ] **Step 7: Commit** — `feat: push a notification when a run waits for a permission decision`

### Task 5c: Push only when the operator asked for it

Found while documenting Task 5 (`VERIFIED`): Settings → Notifications stores per-event preferences in `pipeline_configs` under `notif:pref:<eventType>` as JSON `{"channels": [...], "enabled": bool}` (`server/internal/api/tasks/notification_routes.go:80-87`), and nothing on the server reads them. The approval push must honour the `approval_needed` preference: send only when it exists, is `enabled`, and lists the `browser` channel. No stored preference means no push, matching what the settings panel shows for an unset event (disabled).

**Files:**
- Modify: `server/internal/api/tasks/notification_routes.go` (add `func (h *Handler) approvalPushWanted(ctx context.Context) bool`)
- Modify: `server/internal/api/tasks/permission_request_routes.go` (both notifier call sites also require `h.approvalPushWanted(r.Context())`)
- Modify: `server/internal/api/tasks/permission_notify_test.go`

- [ ] **Step 1: Failing tests** — in `permission_notify_test.go` add a helper that writes the preference through the existing route `PUT /api/notifications/preferences/approval_needed` with body `{"eventType":"approval_needed","channels":["browser"],"enabled":true}` (check the handler's accepted body in `putNotificationPreference`), call it in the three tests that expect a notification, and add: `TestPermissionNotifier_NoPreference_NoPush` (manual task, gated request, no preference → 0 calls) and `TestPermissionNotifier_PreferenceWithoutBrowserChannel_NoPush` (enabled with `["webhook"]` → 0 calls).
- [ ] **Step 2: Run, expect the two new tests to FAIL.**
- [ ] **Step 3: Implement**

```go
// approvalPushWanted reports whether the operator enabled browser notifications
// for approval_needed in Settings → Notifications.
func (h *Handler) approvalPushWanted(ctx context.Context) bool {
	raw := h.cfgRepo.GetString(ctx, notifPrefPrefix+"approval_needed", "")
	if raw == "" {
		return false
	}
	var p notifPref
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return false
	}
	return p.Enabled && slices.Contains(p.Channels, "browser")
}
```

Both call sites: `if req.Outcome == nil && h.notifier != nil && h.approvalPushWanted(r.Context()) {` and, in the bulk loop, `if h.notifier != nil && h.approvalPushWanted(r.Context()) {` (read the preference once before the loop into a local `pushWanted`).
- [ ] **Step 4: Run, expect PASS.** Mutations: `return p.Enabled` without the channel check → the webhook-only test red; `raw == ""` returning true → no-preference test red. Restore from copies.
- [ ] **Step 5: Commit** — `fix(tasks): send the approval push only when browser notifications are enabled for it`

### Task 5d: Subscribe this browser to push notifications

Found while documenting Task 5 (`VERIFIED`): the SPA never calls `pushManager.subscribe`, never fetches the VAPID key and never posts to `POST /api/settings/webpush/subscribe` (`server/internal/api/wphandler/handler.go:27-29`). Without a subscription no push reaches any browser.

**Files:**
- Create: `src/features/settings/composables/usePushSubscription.ts`, `src/features/settings/composables/usePushSubscription.test.ts`
- Modify: `src/features/settings/components/NotificationSettings.vue` (a "Push on this device" block above the event table)
- Modify or create the component test next to `NotificationSettings.vue` (follow the existing test file if there is one)

**Interfaces:**
- Produces (TS): `usePushSubscription(): { supported: ComputedRef<boolean>, state: Ref<'unknown' | 'subscribed' | 'not-subscribed' | 'denied' | 'error'>, error: Ref<string | null>, refresh(): Promise<void>, enable(): Promise<void> }`

- [ ] **Step 1: Failing composable tests** (mock `navigator.serviceWorker.ready`, `Notification.requestPermission`, `PushManager`, `fetch`):
  - `supported` is false when `serviceWorker` or `PushManager` is missing.
  - `refresh()` sets `subscribed` when `pushManager.getSubscription()` returns a subscription, `not-subscribed` otherwise.
  - `enable()`: permission `denied` → state `denied`, no fetch.
  - `enable()`: permission `granted`, `GET /api/settings/webpush/vapid` 404 → `POST /api/settings/webpush/vapid` → key; `pushManager.subscribe({ userVisibleOnly: true, applicationServerKey })` with the key decoded from base64url to a `Uint8Array`; `POST /api/settings/webpush/subscribe` with `subscription.toJSON()` (`endpoint`, `keys.p256dh`, `keys.auth`) → state `subscribed`.
  - A failing subscribe POST → state `error` with the server message.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement** the composable (mutating requests send `Content-Type: application/json`; reuse `errorMessage` from `@/utils/errorMessage`; base64url decode inline with `atob` after replacing `-`/`_` and padding) and the block in `NotificationSettings.vue`: status line ("Push is on for this device" / "Push is off" / "Blocked in browser settings" / error text) and an "Enable push on this device" `AppButton` when not subscribed and supported; hint that "Approval Needed" needs the Browser channel ticked.
- [ ] **Step 4: Component test**: mounting shows the button when not subscribed and calls `enable` on click; unmount at the end.
- [ ] **Step 5: Run, expect PASS**; mutation: skip the `POST …/subscribe` call → the enable test red. Restore.
- [ ] **Step 6: Commit** — `feat(settings): subscribe this browser to push notifications`

### Task 6: Docs, full gates, PR

- [ ] `CHANGELOG.md` `### Fixed`: human waits never time out (plan review measured failing after the limit and losing its plan), failed runs keep output, refusals resume the run, application tools never auto-approved under allow-all autonomy (with the `permission_request_routes.go` evidence). `### Added`: push notification for pending permission requests (needs notifications enabled in the browser; the desktop webview is not covered).
- [ ] `docs/guides/security.md`: application tools always wait for a human regardless of autonomy.
- [ ] `docs/guides/configuration.md` (or the guide that documents `awaitingUserTimeoutSeconds` — `grep -rn awaitingUserTimeoutSeconds docs/`): the limit applies only while an agent process is alive.
- [ ] Full gates, raw output pasted: `cd server && go vet ./... && go test -race ./...` (restore `ent/` if it drifted), `cd sdk && go vet ./...`, `GOTOOLCHAIN=go1.26.6 golangci-lint run ./...` in `server`, `pnpm lint && pnpm typecheck && pnpm test`, `pnpm test:e2e` (restore `server/frontend/dist/.gitkeep`).
- [ ] Push, open the PR with evidence (gates, mutations), wait for CI, merge with `gh pr merge --squash --admin` when all checks are green, confirm `main` CI after the merge.

---

## PR 2 — Job kind and run mode

Branch `feat/routine-jobs`, worktree `/Users/alexanderwink/dashboard-worktrees/routine-jobs`, from `main` at `c1f9d106` (PR 1 merged). Spec: routines §2.1, §2.2, §2.4, and the routine-context decisions of §2.3. Every anchor below was re-read on `c1f9d106`.

**Findings while detailing (2026-09-17, `VERIFIED` unless marked):**

- **B-1 — a human's "allow" for an application tool does nothing on an allow-all task.** `BuildAllowList` returns the permissive list and never reads `task_permissions` when autonomy is `spec_gated` or `full` (`server/internal/pipeline/spawner.go:180-182`). Application tools reach `--allowedTools` only through `mcpapps.Resolver.ResolveRun`, which reads the `grants` table (`server/internal/mcpapps/resolve.go:93-104`) and never `task_permissions`. PR 1 made application-tool requests always wait for a human, so on the default autonomy a grant is stored, the run resumes, and the tool is still missing — the agent asks again. `ResolveRun` already decides in the task context (`resolve.go:128-129`, `{Kind: task, Ref: task.ID}`), so the fix is a grant row in that context. Routine tasks run with autonomy `full` after this PR, so every routine hits this. Fixed in Task 2.13.
- **B-2 — routine grants for non-application tools would be inert.** For the same reason (`spawner.go:180-182`), a routine-context grant for `Bash(pattern)` or any built-in tool never reaches a run's flags. `allow_routine` / `deny_routine` are therefore accepted for application tools only (400 otherwise), instead of the spec's "and, for Bash, its pattern". Routine tasks run with `full` autonomy, whose permissive list already allows the built-in tools, so no routine decision is lost.
- **B-3 — dropping `task_schedules.current_stage` breaks every fresh database** unless the stage-rename migration stops naming it: `migrateRenameStages` counts rows in `task_schedules.current_stage` on a database without its marker (`server/internal/db/client.go:443-444`, query at `:449`). An existing database keeps the column — ent auto-migrate never drops columns (`client.go:151` passes no `WithDropColumn`; precedent `client_test.go:661-668`) — and inserts still succeed because it is `NOT NULL DEFAULT 'backlog'`.
- **B-4 — existing routines need a marker migration to `pipeline`.** A new column declared `.Default("job")` gives existing rows `job`, and the data cannot tell an existing row from a new one afterwards; same reasoning as `client.go:418-420`.
- **B-5 — the board and the needs-you band share one task store.** `App.vue:82-83` feeds `usePendingPermissions(tasks)` from `useTasks`, which fetches `/api/tasks` (`src/features/pipeline/composables/useTasks.ts:95`). If the server's default excludes jobs, a job's permission request never reaches the band. The store therefore fetches `?kind=all` and the board filters jobs out itself.
- **B-6 — run history needs cost and summary**, which live on `stage_runs` (`cost_cents`, `output`; `server/internal/db/ent/schema/stage_run.go:25-27`) and not on the task list payload. Run history gets its own endpoint `GET /api/schedules/{id}/runs` rather than widening every task list response.
- **Out of this PR:** decision buttons for default-denied tools (sibling spec §2.4 ships default denies in PR 4); a per-stage model setting for `job` in Settings → Pipeline (the coded default is the newest Sonnet); the SPA's slash command posting to a non-existent `/api/permission-requests/{id}/resolve` (`src/composables/useSlashCommands.ts:127`) — recorded as a follow-up.

**Stage and kind names used below:** task kinds `pipeline` and `job`; stage `job`; run modes `job` and `pipeline`. Stages are string literals throughout the pipeline today (`IsTerminalStage`, `types.go:231-234`); this PR adds constants only for the new names, in `server/internal/pipeline/types.go`:

```go
const (
	TaskKindPipeline = "pipeline"
	TaskKindJob      = "job"
	StageJob         = "job"
)
```

### Task 2.1: Tasks carry a kind

**Files:**
- Modify: `server/internal/db/ent/schema/task.go:49` (after `routine_id`)
- Modify: `server/internal/db/defaults.go:8-18`
- Modify: `server/internal/db/repo/task_repo.go:59` (`CreateTaskInput`), `:145` (create builder)
- Regenerate: `server/internal/db/ent/`
- Test: `server/internal/db/repo/task_repo_kind_test.go` (new)

**Interfaces:**
- Produces: ent field `Task.Kind string` (`json:"kind"`), `db.DefaultKind = "pipeline"`, `repo.CreateTaskInput.Kind string` (empty = default).

- [ ] **Step 1: Write the failing test** — `task_repo_kind_test.go`, package `repo_test`, `db.Open(":memory:")`:
  - `TestCreateTask_DefaultsToPipelineKind`: create with `Kind: ""` → `got.Kind == "pipeline"`.
  - `TestCreateTask_StoresJobKind`: create with `Kind: "job"` → `got.Kind == "job"`, re-read via `GetByID` → `"job"`.
- [ ] **Step 2: Run** `cd server && go test ./internal/db/repo/ -run 'TestCreateTask_(DefaultsToPipelineKind|StoresJobKind)'` — expected: compile error `unknown field Kind`.
- [ ] **Step 3: Implement.** Schema, next to the other defaults that mirror `db.Default*` (the comment at `task.go:25-26` applies):

```go
		// kind is "pipeline" (runs the stage order) or "job" (one agent run,
		// no worktree, never on the board). Set by the scheduler only.
		field.String("kind").Default("pipeline"),
```

  `defaults.go`: add `DefaultKind = "pipeline"` to the const block. `task_repo.go`: `Kind string` in `CreateTaskInput`; in `Create`, `if in.Kind != "" { q = q.SetKind(in.Kind) }` next to `SetNillableRoutineID`.
- [ ] **Step 4: Regenerate** `cd server && go generate ./internal/db/ent/` then `grep -rl "OnConflict" internal/db/ent/ | head -3` prints files.
- [ ] **Step 5: Run** the two tests — PASS; `go test ./internal/db/...` PASS.
- [ ] **Step 6: Commit** `feat(tasks): store a task kind, pipeline by default`.

### Task 2.2: A job never enters a pipeline stage, a pipeline task never enters the job stage

**Files:**
- Modify: `server/internal/pipeline/types.go` (constants above; new `stageKindViolation`)
- Modify: `server/internal/pipeline/transitions.go:15` (`applyTransition`, first statement)
- Test: `server/internal/pipeline/transitions_kind_test.go` (new)

**Interfaces:**
- Consumes: `Task.Kind` (Task 2.1).
- Produces: `TaskKindPipeline`, `TaskKindJob`, `StageJob`; `func stageKindViolation(kind, stage string) string` (empty = allowed).

- [ ] **Step 1: Write the failing test** — package `pipeline_test`, build with `makeTestOrchestratorWithRepos(t)` (`testhelpers_test.go:30`) and export a thin `ApplyTransitionForTest(ctx, task, sr, t)` in `export_test.go` if none exists. Cases:
  - job task (`Kind: "job"`, `CurrentStage: "job"`) + running stage run, apply `NextTransition{Stage: "implementation"}` → task not in `implementation`; stage run `failed`; output `error` contains `job tasks run only the job stage`.
  - pipeline task in `ready`, apply `NextTransition{Stage: "job"}` → stage run `failed`, error contains `only job tasks run the job stage`.
  - pipeline task in `ready`, `NextTransition{Stage: "implementation"}` → task in `implementation` (unchanged behaviour).
- [ ] **Step 2: Run** `go test ./internal/pipeline/ -run TestApplyTransition_Kind` — FAIL (the job task moves to `implementation`).
- [ ] **Step 3: Implement.** `types.go`:

```go
// stageKindViolation reports why a task of kind may not enter stage, or "".
func stageKindViolation(kind, stage string) string {
	if IsTerminalStage(stage) {
		return ""
	}
	if kind == TaskKindJob && stage != StageJob {
		return "job tasks run only the job stage, refused move to " + stage
	}
	if kind != TaskKindJob && stage == StageJob {
		return "only job tasks run the job stage"
	}
	return ""
}
```

  `transitions.go`, first statement of `applyTransition`:

```go
	if nt, ok := t.(NextTransition); ok {
		if reason := stageKindViolation(task.Kind, nt.Stage); reason != "" {
			t = FailTransition{Reason: reason}
		}
	}
```

- [ ] **Step 4: Run** — PASS. Package tests `go test ./internal/pipeline/...` PASS.
- [ ] **Step 5: Mutation** — copy `transitions.go` aside, replace the condition with `if reason := stageKindViolation(task.Kind, nt.Stage); false && reason != ""`, build, run the test: both refusal cases red. Restore with `cp`, `cmp` identical. Paste the red output.
- [ ] **Step 6: Commit** `fix(pipeline): refuse stage moves that do not match the task kind`.

### Task 2.3: The job stage — prompt, handler, output contract

**Files:**
- Modify: `server/internal/pipeline/stage_prompts.go` (new `JobPrompt` after `FinalizationPrompt`, `:95`)
- Modify: `server/internal/pipeline/stage_handlers.go:404-447` (`jobBuilder`, registry entry)
- Modify: `server/internal/pipeline/completion_detector.go:30-72` (`validateJob`)
- Test: `server/internal/pipeline/job_stage_test.go` (new)

**Interfaces:**
- Consumes: `StageJob` (Task 2.2).
- Produces: `func JobPrompt(t *ent.Task) PromptBundle`; handler registered under `"job"`; `ValidateStageOutput("job", out)` requires `summary` (string, non-empty) and `result` (string).

- [ ] **Step 1: Write the failing tests:**
  - `TestJobPrompt_CarriesTitleDescriptionAndContract`: `JobPrompt(&ent.Task{Title: "Triage inbox", Description: ptr("Sort new mail")})` → `UserPrompt` contains `Triage inbox`, `Sort new mail`, `set_stage_output`, `"summary"`, `"result"`; `SystemPrompt` starts with `sharedContext` and does NOT contain `git`, `commit`, `worktree`, `self_review`, `finalization` (case-insensitive; `review` alone is not forbidden because `sharedContext` says a human will review the output).
  - `TestValidateStageOutput_Job`: `{"summary":"done","result":"3 mails"}` OK; missing `summary` → not OK, message names `summary`; `summary` empty string → not OK; missing `result` → not OK; `result` not a string → not OK.
  - `TestStageHandlers_RegistersJob`: `pipeline.HandlersByStage["job"]` non-nil and `RequiresAgent()` true.
- [ ] **Step 2: Run** `go test ./internal/pipeline/ -run 'TestJobPrompt|TestValidateStageOutput_Job|TestStageHandlers_RegistersJob'` — compile error / FAIL.
- [ ] **Step 3: Implement.** `stage_prompts.go`:

```go
// JobPrompt builds the prompt for a job: one agent run for a routine's
// instruction, with no repository, review, or finalization around it.
func JobPrompt(t *ent.Task) PromptBundle {
	systemPrompt := sharedContext + "\n\nYou run one unattended job for a routine. Work in the current directory. " +
		"If you need a tool you do not have, call `request_permission` once with every tool you need; " +
		"a human decides and the job continues afterwards.\n\n" + jobPermissionsDirective
	userPrompt := fmt.Sprintf(`## Job: %s

%s

When finished, submit your result as your FINAL action by calling the `+"`set_stage_output`"+` MCP tool with an `+"`output`"+` object of exactly this shape:
{"summary": string, "result": string}
"summary" says in one or two sentences what you did; "result" holds what the routine produced.
If `+"`set_stage_output`"+` is unavailable, instead emit the same object as a `+"```json```"+` block.`,
		t.Title, strOrEmpty(t.Description))
	return PromptBundle{SystemPrompt: systemPrompt, UserPrompt: userPrompt}
}
```

  `upfrontPermissionsDirective` names `git commit*` as an example, so the job prompt uses its own `jobPermissionsDirective` (same instruction, no git examples) instead of relaxing the test.

  `stage_handlers.go`: `func jobBuilder(ctx *StageContext) PromptBundle { return JobPrompt(ctx.Task) }` and registry entry `StageJob: createAgentStage(StageJob, jobBuilder, spawnFn),`.

  `completion_detector.go`: `case StageJob: return validateJob(output)`, with `validateJob` following `validateFinalization`'s style (`:58-72`): `summary` must be a non-empty string, `result` a string.
- [ ] **Step 4: Run** — PASS; `go test ./internal/pipeline/...` PASS.
- [ ] **Step 5: Mutation** — drop the `summary` check in `validateJob` → the missing-summary case red; restore identical.
- [ ] **Step 6: Commit** `feat(pipeline): a job stage with its own prompt and output contract`.

### Task 2.4: A job runs `job → done` in the task's directory, without a worktree

**Files:**
- Modify: `server/internal/pipeline/transitions.go:335` (`decideCompletedTransition`, first check)
- Modify: `server/internal/pipeline/progress_guards.go:65-67` (`needsWorktree`)
- Modify: `server/internal/pipeline/model_resolver.go:15-21, 42-51` (`defaultModelJob`, `case StageJob`)
- Test: `server/internal/pipeline/job_run_test.go` (new), `server/internal/pipeline/stage_model_test.go` (one table row)

**Interfaces:**
- Consumes: Tasks 2.1–2.3.
- Produces: a completed `job` stage run decides `DoneTransition`; `needsWorktree` false for jobs; the coded model default for stage `job` is `claudemodel.Latest(claudemodel.Sonnet)`.

- [ ] **Step 1: Write the failing tests** in `job_run_test.go` (package `pipeline_test`):
  - `TestJob_NeverCreatesWorktreeEvenWhenForced`: build the orchestrator like `makeOrchWithCaptureSpawn` (`requeue_for_user_test.go:289-310`) plus `ForceWorktrees: true` and an `EnsureWorktreeFn` stub counting calls; create a task `Kind: "job"`, `CurrentStage: "job"`, `Cwd: t.TempDir()`; `orch.ProgressTask(ctx, task.ID, nil)` → stub calls == 0, the captured spawn options' `Task.Cwd` is the temp dir and `Task.WorktreePath` is nil or empty.
  - `TestPipeline_StillCreatesWorktreeWhenForced`: same orchestrator, pipeline task in `implementation` → stub calls == 1 (unchanged behaviour).
  - `TestJob_CompletedRunFinishesTheTask`: `orch.DecideCompletedTransitionForTest(ctx, task, &ent.StageRun{Stage: "job"}, map[string]any{"summary": "s", "result": "r"})` returns a `pipeline.DoneTransition` carrying that output. Without the explicit branch the fallthrough `NextStage("job")` yields `NextTransition{Stage: "done"}`, which skips the dependency handling and lock release of `DoneTransition` (`transitions.go:115-133`).
  - `stage_model_test.go`, `TestStageModelDefault_BalancedDefaults` table: add `{"job", claudemodel.Latest(claudemodel.Sonnet)}` (create that case's task with `Kind: "job"`).

  The pipeline's pick loop (`scheduler.go:54-113`) has no stage filter; `job` is not in `StageOrder`, so `sortByStageIndex` ranks it -1 behind every pipeline stage (`scheduler.go:129-136`) — a job is picked whenever a slot is free and no pipeline task is ahead of it. Accepted; no test, `Pick` has no test seam.
- [ ] **Step 2: Run** `go test ./internal/pipeline/ -run 'TestJob_|TestPipeline_StillCreatesWorktreeWhenForced|TestStageModelDefault_BalancedDefaults'` — FAIL (stub called once for the job; `NextTransition` instead of `DoneTransition`; model empty for `job`).
- [ ] **Step 3: Implement.** `decideCompletedTransition`, before the `finalization` check:

```go
	if run.Stage == StageJob {
		return DoneTransition{Output: output}
	}
```

  `progress_guards.go`:

```go
	needsWorktree := handler.RequiresAgent() && task.Kind != TaskKindJob &&
		(task.WorktreePath == nil || *task.WorktreePath == "") &&
		(o.opts.ForceWorktrees || (task.SourceBranch != nil && *task.SourceBranch != ""))
```

  `model_resolver.go`: `defaultModelJob = claudemodel.Latest(claudemodel.Sonnet)` in the var block and `case StageJob: coded = defaultModelJob`.
- [ ] **Step 4: Run** — PASS; `go test ./internal/pipeline/...` PASS; `TestNoPinnedModelIDsOutsideTheCatalog` PASS.
- [ ] **Step 5: Mutation** — remove `task.Kind != TaskKindJob &&` → `TestJob_NeverCreatesWorktreeEvenWhenForced` red; remove the `StageJob` branch in `decideCompletedTransition` → `TestJob_CompletedRunFinishesTheTask` red. Restore identical each time; paste both red outputs.
- [ ] **Step 6: Commit** `feat(pipeline): run a job in its own directory and finish it after one stage`.

### Task 2.5: Routines store a run mode and skipped fires; the stage column goes

**Files:**
- Modify: `server/internal/db/ent/schema/task_schedule.go:39` (remove `current_stage`; add `run_mode`, `last_skipped_at`, `skipped_count`)
- Modify: `server/internal/db/client.go:443-444` (remove the two `task_schedules` renames), new `migrateRoutineRunModes` after `migrateRenameStages` (`:192-195`)
- Modify: `server/internal/db/repo/task_schedule_repo.go:44, 134-136` (remove `CreateTaskScheduleInput.CurrentStage` and its setter — the generated `SetCurrentStage` no longer exists, so this cannot wait for Task 2.6)
- Regenerate: `server/internal/db/ent/`
- Test: `server/internal/db/client_test.go` (next to `TestOpen_RenameStagesRunsOnlyOnce`, `:775`)

**Interfaces:**
- Produces: ent fields `TaskSchedule.RunMode string` (default `"job"`), `LastSkippedAt *time.Time`, `SkippedCount int` (default 0); migration marker `routine-run-mode-pipeline`.

- [ ] **Step 1: Write the failing tests:**
  - `TestOpen_FreshDatabaseOpensWithoutScheduleStageColumn`: `db.Open(filepath.Join(t.TempDir(), "x.db"))` succeeds and `PRAGMA table_info(task_schedules)` lists `run_mode`, `skipped_count`, `last_skipped_at` and no `current_stage`.
  - `TestOpen_ExistingRoutinesBecomePipelineOnce`: open a file DB, insert a schedule, delete the marker row `routine-run-mode-pipeline` and set `run_mode = 'job'` on that row (simulates a pre-upgrade row that received the column default), reopen → row `run_mode = 'pipeline'`; then create a new schedule through the repo (default `job`), reopen again → the new row is still `job`.
- [ ] **Step 2: Run** `go test ./internal/db/ -run 'TestOpen_FreshDatabaseOpens|TestOpen_ExistingRoutinesBecomePipelineOnce'` — FAIL.
- [ ] **Step 3: Implement.** Schema:

```go
		// run_mode "job" fires a job task; "pipeline" fires a pipeline task
		// that starts in ready. Existing rows were migrated to "pipeline".
		field.String("run_mode").Default("job"),
		...
		// Fires refused because the previous run was still in flight.
		field.Time("last_skipped_at").Optional().Nillable(),
		field.Int("skipped_count").Default(0),
```

  `client.go`: delete the two `task_schedules` entries from `renames` (`:443-444`); the column they named is gone from the schema and nothing ever read it. New function, same shape as `migrateRenameStages` (`:421-469`) and reusing its `applied_migrations` table:

```go
// migrateRoutineRunModes sets every routine that existed before run modes to
// "pipeline", which is what those routines always fired. It runs once: the
// column default is "job", so after the upgrade an existing row and a newly
// created one look the same, and a second pass would turn new job routines
// into pipeline routines.
func migrateRoutineRunModes(db *sql.DB) error {
	const marker = "routine-run-mode-pipeline"
	// same applied_migrations create + marker check as migrateRenameStages
	// UPDATE task_schedules SET run_mode = 'pipeline'
	// INSERT the marker
}
```

  Extract the marker check/insert shared by both functions into `runOnce(db *sql.DB, marker string, fn func() error) error` only if it keeps both readable; otherwise duplicate the ten lines (two occurrences). Call it in `Open` right after the `migrateRenameStages` block with the same error handling.
- [ ] **Step 4: Regenerate ent**, OnConflict grep prints files; run the tests — PASS; `go test ./internal/db/...` PASS.
- [ ] **Step 5: Mutation** — skip the marker check (always run the UPDATE) → the "new row is still job" assertion red; restore identical.
- [ ] **Step 6: Commit** `feat(routines): store a run mode and skipped fires, drop the unused stage column`.

### Task 2.6: The schedule repo writes run modes and records skips

**Files:**
- Modify: `server/internal/db/repo/task_schedule_repo.go:15-27` (interface), `:30-57` (`CreateTaskScheduleInput`: remove `CurrentStage`, add `RunMode`), `:61-86` (`UpdateTaskScheduleInput.RunMode *string`), `:110-164` (create), `:174-244` (update), new `RecordSkip`
- Test: `server/internal/db/repo/task_schedule_run_mode_test.go` (new)

**Interfaces:**
- Consumes: ent fields from Task 2.5.
- Produces: `repo.RunModeJob = "job"`, `repo.RunModePipeline = "pipeline"`, `func IsValidRunMode(string) bool`; `CreateTaskScheduleInput.RunMode string` (empty = `job`); `UpdateTaskScheduleInput.RunMode *string`; `RecordSkip(ctx context.Context, id string, at time.Time) (*ent.TaskSchedule, error)` — increments `skipped_count`, sets `last_skipped_at`.

- [ ] **Step 1: Write the failing tests:** create without `RunMode` → `job`; create with `pipeline` → `pipeline`; update `RunMode: ptr("pipeline")` persists; `RecordSkip` twice → `SkippedCount == 2`, `LastSkippedAt` equals the second `at` (truncate to second); `RecordSkip` on an unknown id → `ent.IsNotFound(err)`; create with `RunMode: "cron"` → error containing `run mode`.
- [ ] **Step 2: Run** `go test ./internal/db/repo/ -run 'TestTaskSchedule_RunMode|TestTaskSchedule_RecordSkip'` — compile error.
- [ ] **Step 3: Implement.** Constants and `IsValidRunMode` at the top of the file. `Create`: `if in.RunMode != "" { if !IsValidRunMode(in.RunMode) { return nil, fmt.Errorf("task_schedule.create: invalid run mode %q", in.RunMode) }; q = q.SetRunMode(in.RunMode) }` — `Update`: same validation for `in.RunMode != nil`, `SetRunMode`. `RecordSkip`:

```go
func (r *entTaskScheduleRepo) RecordSkip(ctx context.Context, id string, at time.Time) (*ent.TaskSchedule, error) {
	return r.client.TaskSchedule.UpdateOneID(id).
		AddSkippedCount(1).
		SetLastSkippedAt(at).
		SetUpdatedAt(time.Now()).
		Save(ctx)
}
```

  (`CreateTaskScheduleInput.CurrentStage` was already removed in Task 2.5.)
- [ ] **Step 4: Run** — PASS; `go vet ./...` from `server/` PASS.
- [ ] **Step 5: Mutation** — drop `AddSkippedCount(1)` → count test red; restore identical.
- [ ] **Step 6: Commit** `feat(routines): persist run modes and count skipped fires`.

### Task 2.7: A fire creates the task its run mode asks for

**Files:**
- Modify: `server/internal/scheduler/materializer.go:16-34` (`NewTaskSpec`: `Kind`, `Stage`, `Autonomy`), `:74-92`
- Modify: `server/serverapp/di_scheduler.go:28-52` (pass `Kind`, `Stage`, `Autonomy`)
- Modify: `server/internal/api/tasks/handler.go:334-362` (`CreateTaskParams.Kind`), `:432-472` (pass `Kind` to `repo.CreateTaskInput`)
- Test: `server/internal/scheduler/materializer_run_mode_test.go` (new), `server/internal/api/tasks/routine_id_test.go` (extend)

**Interfaces:**
- Consumes: `RunMode*` (Task 2.6), `CreateTaskInput.Kind` (Task 2.1), `pipeline.StageJob`/`TaskKind*` (Task 2.2).
- Produces: `NewTaskSpec.Kind`, `.Stage`, `.Autonomy *string`; `CreateTaskParams.Kind string` — set by the scheduler only; the HTTP create body has no `kind` field, like `routineId` (`schema/task.go:45-48`).

- [ ] **Step 1: Write the failing tests** (pattern `materializer_routine_test.go:18-33`):
  - `TestMaterialize_JobRunMode`: schedule `RunMode: "job"` → spec `Kind "job"`, `Stage "job"`, `*Autonomy "full"`.
  - `TestMaterialize_PipelineRunMode`: `RunMode: "pipeline"` → `Kind "pipeline"`, `Stage "ready"`, `*Autonomy "full"`.
  - `routine_id_test.go`: `CreateTaskFromInput(ctx, CreateTaskParams{..., Kind: "job", Stage: "job"})` stores `kind = job`; a `POST /api/tasks` body containing `"kind":"job"` still creates `kind = pipeline`.
- [ ] **Step 2: Run** `go test ./internal/scheduler/ ./internal/api/tasks/ -run 'TestMaterialize_(Job|Pipeline)RunMode|Kind'` — compile error.
- [ ] **Step 3: Implement.** The scheduler must not import `pipeline` if it does not already (check `go list -deps ./internal/scheduler | grep internal/pipeline`); if it would create a cycle or a new edge, use the string literals `"job"`, `"ready"`, `"full"` with the `repo.RunMode*` constants for the mode. Materializer:

```go
	kind, stage := "pipeline", "ready"
	if s.RunMode == repo.RunModeJob {
		kind, stage = "job", "job"
	}
	full := "full"
	spec := NewTaskSpec{
		...
		Kind:     kind,
		Stage:    stage,
		Autonomy: &full,
	}
```

  `di_scheduler.go`: `Kind: spec.Kind, Stage: spec.Stage, Autonomy: spec.Autonomy,`. `handler.go`: `Kind string` in `CreateTaskParams` with the same "set by the scheduler only" comment style as `RoutineID` (`:349-352`); `Kind: p.Kind` into `repo.CreateTaskInput`.
- [ ] **Step 4: Run** — PASS; `go test ./internal/scheduler/... ./internal/api/tasks/... ./serverapp/...` PASS.
- [ ] **Step 5: Mutation** — force `kind, stage = "pipeline", "ready"` for every mode → `TestMaterialize_JobRunMode` red; restore identical.
- [ ] **Step 6: Commit** `feat(routines): fire a job or a ready pipeline task with full autonomy`.

**Found while dispatching (2026-09-17, `VERIFIED`):** two more ways a task can reach a stage that does not match its kind without a transition, both in `server/internal/api/tasks/handler.go`: the create path takes a caller's `stage` (`:531`, `Stage: body.Stage`), so `POST /api/tasks` with `"stage":"job"` stores a pipeline task in the job stage; and resuming a task from `on_hold` always moves it to `implementation` (`:863-867`), which would put a held job into a pipeline stage. Both are closed in this task:
- Rename `stageKindViolation` to exported `StageKindViolation` in `server/internal/pipeline/types.go` (update its one caller in `transitions.go`). `CreateTaskFromInput` (`api/tasks` already imports `pipeline`) refuses with `apierr.NewAppError(http.StatusBadRequest, reason)` when `pipeline.StageKindViolation(kind, stage)` is non-empty, `kind` defaulting to `pipeline`. Test: `POST /api/tasks` with `"stage":"job"` → 400 containing `only job tasks run the job stage`; `CreateTaskFromInput` with `Kind: "job", Stage: "implementation"` → error.
- The resume handler moves an `on_hold` task to `pipeline.StageJob` when `t.Kind == pipeline.TaskKindJob`, otherwise to `implementation` as today. Test: a held job resumes into `job`; a held pipeline task still resumes into `implementation`.
- Mutations: drop the create check → the HTTP case red; always resume into `implementation` → the held-job case red.

### Task 2.8: A refused fire is counted

**Files:**
- Modify: `server/internal/scheduler/scheduler.go:151-162` (`fireOne`, overlap branch)
- Test: `server/internal/scheduler/scheduler_test.go` (extend `TestTick_SkipOnOverlap`, `:99`; add a waiting-run case)

**Interfaces:**
- Consumes: `TaskScheduleRepo.RecordSkip` (Task 2.6). `Options.Schedules` is already a `repo.TaskScheduleRepo`.

- [ ] **Step 1: Write the failing tests:** in `TestTick_SkipOnOverlap`, after the tick, reload the schedule → `SkippedCount == 1`, `LastSkippedAt` equals the harness `now`. New `TestTick_SkipWhileRunWaitsForHuman`: prior task in `implementation` with a stage run `awaiting_user` and no PID → no task created, `SkippedCount == 1`.
- [ ] **Step 2: Run** `go test ./internal/scheduler/ -run 'TestTick_Skip'` — FAIL (`SkippedCount == 0`).
- [ ] **Step 3: Implement** in the overlap branch, before `s.advance`:

```go
		if _, err := s.schedules.RecordSkip(ctx, sched.ID, now); err != nil {
			slog.Warn("scheduler: record skipped fire", "schedule", sched.ID, "err", err)
		}
```

  (Use the field name the `Scheduler` struct actually has for the schedule repo and the local `now` of `fireOne`.)
- [ ] **Step 4: Run** — PASS; package tests PASS.
- [ ] **Step 5: Mutation** — remove the `RecordSkip` call → both tests red; restore identical.
- [ ] **Step 6: Commit** `feat(routines): count fires skipped while the previous run is in flight`.

### Task 2.9: The schedules API takes a run mode and refuses a pipeline routine outside git

**Files:**
- Modify: `server/internal/api/schedules/view.go:12-35` (`scheduleBody.RunMode string`), `:38-63` (`scheduleView`: `RunMode`, `LastSkippedAt *string`, `SkippedCount int`), `:65-99` (`toView`)
- Modify: `server/internal/api/schedules/handler.go:111-253` (create, update validation)
- Modify: `server/internal/worktree/worktree.go` (new exported `IsGitWorkTree`)
- Create: `server/internal/scheduler/run_mode.go` (`CheckRunMode`, the one rule the REST handler and the MCP tool share — both already import `scheduler`, `worktree` imports nothing from the project)
- Test: `server/internal/api/schedules/handler_test.go` (extend), `server/internal/worktree/worktree_git_check_test.go` (new), `server/internal/scheduler/run_mode_test.go` (new)

**Interfaces:**
- Consumes: `repo.IsValidRunMode`, `RunMode` inputs (Task 2.6).
- Produces: JSON `runMode`, `lastSkippedAt` (RFC 3339, omitted when nil), `skippedCount`; `func IsGitWorkTree(ctx context.Context, dir string) bool`.

- [ ] **Step 1: Write the failing tests:**
  - `worktree_git_check_test.go`: `t.TempDir()` → false; a temp dir after `git init` (skip with `t.Skip` when `exec.LookPath("git")` fails) → true; a nonexistent path → false.
  - `handler_test.go`: create without `runMode` → 201, `runMode == "job"`, `skippedCount == 0`; create `runMode: "pipeline"` with `cwd: t.TempDir()` → 400 and body contains `working directory is not a git repository`; same with a `git init` dir → 201; `runMode: "cron"` → 400 `runMode must be job or pipeline`; PATCH an existing job routine (cwd non-git) to `runMode: "pipeline"` → 400; PATCH `cwd` of a pipeline routine (git dir) to a non-git dir → 400; the view never contains `currentStage`.
- [ ] **Step 2: Run** `go test ./internal/worktree/ ./internal/api/schedules/` — FAIL.
- [ ] **Step 3: Implement.** `worktree.go`:

```go
// IsGitWorkTree reports whether dir lies inside a git working tree.
func IsGitWorkTree(ctx context.Context, dir string) bool {
	out, err := NewRunner().Output(ctx, dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}
```

  `scheduler/run_mode.go`:

```go
// CheckRunMode reports why a routine with runMode and cwd cannot be saved.
func CheckRunMode(ctx context.Context, runMode, cwd string) error {
	if !repo.IsValidRunMode(runMode) {
		return errors.New("runMode must be job or pipeline")
	}
	if runMode == repo.RunModePipeline && !worktree.IsGitWorkTree(ctx, cwd) {
		return errors.New("working directory is not a git repository")
	}
	return nil
}
```

  `handler.go` wraps it: `if err := scheduler.CheckRunMode(ctx, mode, cwd); err != nil { return apierr.NewAppError(http.StatusBadRequest, err.Error()) }`.

  Create: `mode := body.RunMode; if mode == "" { mode = repo.RunModeJob }`, validate with `body.Cwd`, pass `RunMode: mode`. Update: load the schedule, compute the effective mode (`body.RunMode` or stored) and effective cwd (`body.Cwd` or stored), validate only when either changes, set `in.RunMode`. `toView`: map the three fields.
- [ ] **Step 4: Run** — PASS; `go test ./internal/api/... ./internal/worktree/...` PASS.
- [ ] **Step 5: Mutation** — remove the git check in `CheckRunMode` → the non-git pipeline cases red; restore identical.
- [ ] **Step 6: Commit** `feat(routines): choose a run mode in the schedules API`.

### Task 2.10: The MCP schedule tool knows run modes

**Files:**
- Modify: `server/internal/mcp/tools/schedules.go:78-102` (schema property), `:128-186` (create), `:188-238` (update)
- Test: `server/internal/mcp/tools/schedules_test.go` (extend)

**Interfaces:**
- Consumes: `scheduler.CheckRunMode` (Task 2.9) — `mcp.Fail(err.Error())` on refusal.

- [ ] **Step 1: Write the failing tests:** `manage_schedule` create without `runMode` → stored `job`; create `runMode: "pipeline"` with `cwd: t.TempDir()` → tool error containing `working directory is not a git repository`; update `runMode: "cron"` → tool error containing `runMode must be job or pipeline`.
- [ ] **Step 2: Run** `go test ./internal/mcp/tools/ -run TestManageSchedule` — FAIL.
- [ ] **Step 3: Implement.** Schema: `"runMode": {"type": "string", "enum": ["job", "pipeline"], "description": "job: one agent run in cwd, no worktree (default). pipeline: a pipeline task that starts in ready; cwd must be a git repository."}`. Create/update read it like `cwd` and apply the same two checks.
- [ ] **Step 4: Run** — PASS.
- [ ] **Step 5: Mutation** — skip the git check in the tool → the pipeline case red; restore identical.
- [ ] **Step 6: Commit** `feat(mcp): set a routine's run mode through manage_schedule`.

### Task 2.11: The task list filters by kind and routine

**Files:**
- Modify: `server/internal/api/tasks/handler.go:278-302` (`list`)
- Modify: `server/internal/api/tasks/enrich.go:23-49` (`TaskResponse.Kind`, `.RoutineID`), `:53-80` (`ToTaskResponse`)
- Test: `server/internal/api/tasks/list_kind_filter_test.go` (new)

**Interfaces:**
- Consumes: `Task.Kind` (Task 2.1).
- Produces: `GET /api/tasks` → `pipeline` tasks only; `?kind=job` → jobs; `?kind=all` → both; any other value → 400 `kind must be pipeline, job or all`; `?routineId=<id>` narrows to that routine. JSON `kind`, `routineId` on every task payload.

- [ ] **Step 1: Write the failing test:** seed one pipeline task, one job with `RoutineID: "r1"`, one job with `"r2"`; assert: default → only the pipeline task; `kind=job` → two jobs; `kind=job&routineId=r1` → one; `kind=all` → three; `kind=weird` → 400; each item carries `kind` and `routineId` (null for the pipeline task).
- [ ] **Step 2: Run** `go test ./internal/api/tasks/ -run TestListTasks_Kind` — FAIL.
- [ ] **Step 3: Implement** next to the existing `stage` filter (keep the in-memory filtering style of `:284-293`):

```go
	q := r.URL.Query()
	kind := q.Get("kind")
	if kind == "" {
		kind = pipeline.TaskKindPipeline
	}
	if kind != "all" && kind != pipeline.TaskKindPipeline && kind != pipeline.TaskKindJob {
		return apierr.NewAppError(http.StatusBadRequest, "kind must be pipeline, job or all")
	}
	routineID := q.Get("routineId")
	tasks = slices.DeleteFunc(tasks, func(t *ent.Task) bool {
		if kind != "all" && t.Kind != kind {
			return true
		}
		return routineID != "" && (t.RoutineID == nil || *t.RoutineID != routineID)
	})
```

  (Use the string literals if `api/tasks` does not already import `pipeline`.) `TaskResponse`: `Kind string \`json:"kind"\``, `RoutineID *string \`json:"routineId"\``, mapped in `ToTaskResponse`. Check `src/features/**/__tests__/*wire-parity*` for a task payload parity test and extend its expectation if one exists.
- [ ] **Step 4: Run** — PASS; `go test ./internal/api/tasks/...` PASS.
- [ ] **Step 5: Mutation** — default `kind` to `"all"` → the default case red; restore identical.
- [ ] **Step 6: Commit** `feat(tasks): list tasks by kind and routine, pipeline tasks by default`.

### Task 2.12: A routine's runs, with summary and cost

**Files:**
- Create: `server/internal/api/schedules/runs.go` (handler + view)
- Modify: `server/internal/db/repo/task_repo.go` (`ListByRoutine(ctx, routineID string, limit int) ([]*ent.Task, error)`, newest first)
- Modify: `server/serverapp/di_scheduler.go:69` (mount the runs handler next to the schedules handler)
- Test: `server/internal/api/schedules/runs_test.go` (new)

**Interfaces:**
- Consumes: `Task.Kind`, `Task.RoutineID`, stage runs (`cost_cents`, `output`, `started_at`, `ended_at`).
- Produces: `GET /api/schedules/{id}/runs` → `200 [{taskId, title, kind, stage, status, summary, costCents, startedAt, endedAt}]`, newest first, at most 50; 404 when the schedule does not exist.

- [ ] **Step 1: Write the failing test:** schedule `s1`; task A (`job`, `done`) with one stage run `output {"summary":"sorted 3 mails","result":"…"}`, `cost_cents 12`; task B (`job`, `job` stage) with two stage runs (`awaiting_user` then `running`, cost 5 + 7); task C of another routine. Expect two items, B first; B `costCents 12`, `status "running"` (latest run), `summary ""`; A `summary "sorted 3 mails"`, `status "done"`; unknown schedule id → 404.
- [ ] **Step 2: Run** `go test ./internal/api/schedules/ -run TestRoutineRuns` — FAIL.
- [ ] **Step 3: Implement.** `runs.go`:

```go
// RunsHandler serves GET /api/schedules/{id}/runs.
type RunsHandler struct {
	schedules repo.TaskScheduleRepo
	tasks     repo.TaskRepo
	runs      repo.StageRunRepo
}

func NewRunsHandler(s repo.TaskScheduleRepo, t repo.TaskRepo, r repo.StageRunRepo) *RunsHandler {
	return &RunsHandler{schedules: s, tasks: t, runs: r}
}

func (h *RunsHandler) Mount(r chi.Router) {
	r.Get("/api/schedules/{id}/runs", apierr.ErrorMiddleware(h.list))
}
```

  `list`: `GetByID` (404 on not found), `tasks.ListByRoutine(ctx, id, 50)`, then for each task the stage runs through the stage-run list method `api/tasks/handler.go:934` (`listStageRuns`) uses; `costCents` = sum, `status` = the latest run's status (or the task stage when it has none), `summary` = latest run's `output["summary"]` when it is a string, `startedAt` = earliest `started_at`, `endedAt` = latest `ended_at` when the task is terminal. `// ponytail: one stage-run query per task, 50 tasks max; batch by task ids if routine pages get slow.` Mount it in `di_scheduler.go` wherever `handler.Mount` is called for the schedules handler, inside the same authenticated group.
- [ ] **Step 4: Run** — PASS; `go test ./internal/api/schedules/... ./serverapp/...` PASS.
- [ ] **Step 5: Mutation** — sum only the latest run's cost → B's `costCents` assertion red; restore identical.
- [ ] **Step 6: Commit** `feat(routines): list a routine's runs with summary and cost`.

### Task 2.13: Permission decisions, and "allow" that reaches an allow-all run

**Files:**
- Create: `server/internal/api/tasks/permission_decision.go` (`decision` parsing, grant writers, resume prompts)
- Modify: `server/internal/mcpapps/preset.go:60-86` (extract the dedupe-then-create closure into exported `EnsureGrant`, used by the preset and by decisions)
- Modify: `server/internal/api/tasks/handler.go:83-116` (`Deps.GrantRepo repo.GrantRepo`), `:1096-1144` (single resolve)
- Modify: `server/serverapp/di_tasks.go:27` (wire `GrantRepo: repo.NewGrantRepo(client)` — use the constructor name the repo package exports)
- Test: `server/internal/api/tasks/permission_decision_test.go` (new)

**Interfaces:**
- Consumes: `mcpapps.IsApplicationTool` (PR 1), `repo.GrantContextTask`/`GrantContextRoutine`, `ResumeFromUser`, `deniedResumePrompt` (`permission_request_routes.go:420-424`).
- Produces:

```go
const (
	DecisionAllowOnce    = "allow_once"
	DecisionAllowRoutine = "allow_routine"
	DecisionDenyRoutine  = "deny_routine"
	DecisionDenyOnce     = "deny_once"
)

// parseDecision accepts decision, or the legacy outcome as an alias.
func parseDecision(decision, outcome string) (string, error)

// mcpapps.EnsureGrant creates the grant unless an equivalent live one exists.
func EnsureGrant(ctx context.Context, grants repo.GrantRepo, in repo.CreateGrantInput) (created bool, err error)
```

  Request body of the single resolve: `{"decision": "allow_once|allow_routine|deny_routine|deny_once"}`; `{"outcome": "granted|denied"}` still works (`granted` → `allow_once`, `denied` → `deny_once`); both present and disagreeing → 400.

| Decision | Tool | Writes | Resume prompt |
| --- | --- | --- | --- |
| `allow_once` | any | today's `task_permissions` grant (`grantValidatedEntries`); for an application tool additionally a grant `allow`, context `task=<taskID>` (B-1) | `""` |
| `allow_routine` | application tool, task has `routineId` | grant `allow`, context `routine=<routineID>` | `""` |
| `deny_routine` | application tool, task has `routineId` | grant `deny`, context `routine=<routineID>` | `routineDeniedResumePrompt(tools)` |
| `deny_once` | any | nothing | `deniedResumePrompt(tools)` |

  Refusals (400, request stays pending): unknown decision → `decision must be allow_once, allow_routine, deny_routine or deny_once`; routine decision on a task without `routineId` → `routine decisions need a task started by a routine`; routine decision for a non-application tool → `routine decisions apply to application tools only`. Validate before `ResolvePermissionRequest`, so a refused call changes nothing.

- [ ] **Step 1: Write the failing tests** (reuse the fixtures of `permission_denied_resume_test.go` / `permission_application_tool_test.go`: in-memory DB, `resumeRecorder` embedding `*noopOrchestrator`; seed a task with `Autonomy: "full"`, `RoutineID: "r1"`, a stage run, and a pending request for `mcp__mail__move_message`):
  - `allow_once` → resolved `granted`; a `task_permissions` row; a grant row `{capability mcp__mail__move_message, context task/<taskID>, mode allow}`; resume with `""`.
  - `allow_routine` → grant `{routine/r1, allow}`; resume `""`; no `task`-context grant.
  - `deny_routine` → grant `{routine/r1, deny}`; resume prompt contains `mcp__mail__move_message` and `for this routine`.
  - `deny_once` → no grant row, no task permission; resume prompt equals `deniedResumePrompt([]string{"mcp__mail__move_message"})`.
  - `{"outcome":"granted"}` behaves as `allow_once`; `{"outcome":"denied","decision":"allow_once"}` → 400.
  - `allow_routine` on a task with `RoutineID: nil` → 400 and the request is still `pending`; `allow_routine` for `Bash` with pattern `ls` → 400 `application tools only`.
  - `allow_routine` twice for the same tool (two requests) → exactly one live grant row (`EnsureGrant` dedupe).
  - Existing `mcpapps` preset tests still pass unchanged.
- [ ] **Step 2: Run** `go test ./internal/api/tasks/ -run TestResolveDecision` — compile error.
- [ ] **Step 3: Implement.** `EnsureGrant` is the body of `preset.go`'s `apply` closure from `ListForCapability` to `Create`, with the equivalence check unchanged (`RevokedAt == nil && ExpiresAt == nil && Pattern == "" && LimitCount == 0 && Mode && ContextKind && ContextRef`); the closure keeps its `known`/`Skipped`/`Existing`/`Created` bookkeeping around it. In `permission_decision.go`:

```go
func routineDeniedResumePrompt(tools []string) string {
	return "A human denied these tools for this routine, permanently: " + strings.Join(tools, ", ") +
		". Continue without them and state in your output what you could not do because of that."
}
```

  plus `decisionGrant(decision string, task *ent.Task, tool string) (repo.CreateGrantInput, bool)` returning the grant row for the table above (`Reason: "permission decision " + decision`, `GrantedBy: decidedByFromRequest(r)`), and `validateDecision(decision string, task *ent.Task, tools []string) error` for the three refusals. The single resolve handler: decode `decision` and `outcome`, `parseDecision`, load the task, `validateDecision`, then resolve with `repo.OutcomeGranted` for `allow_*` and `repo.OutcomeDenied` for `deny_*`, write the grant(s), resume with the prompt from the table. `Deps.GrantRepo` nil → routine decisions answer 503 `grants unavailable`; `allow_once` then keeps today's behaviour without the task-context grant.
- [ ] **Step 4: Run** — PASS; `go test ./internal/api/tasks/... ./internal/mcpapps/... ./serverapp/...` PASS.
- [ ] **Step 5: Mutation** (each on a copy, restore identical, paste red): drop the task-context grant for `allow_once` → the B-1 assertion red; drop the routine-id check in `validateDecision` → the no-routine case red; drop the application-tool check → the Bash case red.
- [ ] **Step 6: Commit** `feat(tasks): decide a permission request once or for the routine`.

### Task 2.14: The bulk resolve takes the same decisions

**Files:**
- Modify: `server/internal/api/tasks/permission_request_routes.go:427-560` (bulk resolve)
- Test: `server/internal/api/tasks/permission_decision_bulk_test.go` (new)

**Interfaces:**
- Consumes: everything `permission_decision.go` produces (Task 2.13).
- Produces: bulk body `{taskId, decision, outcome, permissionIds, all, remember}`; `decision` applies to every resolved request; `validateDecision` runs over all selected tools before anything is resolved (one non-application tool in a routine decision → 400, nothing resolved). `remember` keeps its preset behaviour for `allow_once` only.

- [ ] **Step 1: Write the failing tests:** two pending application-tool requests on a routine task → `allow_routine` creates two routine grants, resumes once with `""`; `deny_routine` creates two deny grants and resumes once with `routineDeniedResumePrompt` naming both tools; a mix of `mcp__mail__read` and `Bash` with `allow_routine` → 400 and both still pending; `outcome: "denied"` alone still resumes with `deniedResumePrompt` (PR 1 behaviour); `allow_once` on an application tool writes the task-context grant.
- [ ] **Step 2: Run** `go test ./internal/api/tasks/ -run TestBulkResolveDecision` — FAIL.
- [ ] **Step 3: Implement** by replacing the `Outcome` validation (`:441`) with `parseDecision`, collecting the selected requests first, running `validateDecision` on their tools, then branching on `allow_*` / `deny_*` where the code today branches on granted / denied (`:493-553`), writing the grants from `decisionGrant` per request and resuming once with the table's prompt.
- [ ] **Step 4: Run** — PASS; `go test ./internal/api/tasks/...` PASS.
- [ ] **Step 5: Mutation** — skip `validateDecision` in the bulk path → the mixed case red; restore identical.
- [ ] **Step 6: Commit** `feat(tasks): bulk permission decisions for a routine`.

### Task 2.15: The routine form chooses a run mode

**Files:**
- Modify: `src/composables/useSchedules.ts:4-29` (`ScheduleView`: `runMode: RunMode`, `lastSkippedAt?: string`, `skippedCount: number`; fix `catchup` to `'none' | 'once'`), `:38-58` (`CreateScheduleBody.runMode?: RunMode`)
- Modify: `src/components/ScheduleForm.vue:115-241` (run mode field above the working directory)
- Test: `src/components/__tests__/ScheduleForm.runMode.test.ts` (new)

**Interfaces:**
- Produces: `export type RunMode = 'job' | 'pipeline'` in `useSchedules.ts`.

- [ ] **Step 1: Write the failing test** (pattern `ScheduleForm.applications.test.ts`; mock `createSchedule`/`updateSchedule`; `wrapper.unmount()` in every test): new routine → the radio **Job** is checked; choosing **Pipeline task** and saving sends `runMode: 'pipeline'`; editing a schedule with `runMode: 'pipeline'` shows **Pipeline task** checked; the helper text under **Pipeline task** says `The working directory must be a git repository.`; a 400 from the server shows its message in the form's existing error slot.
- [ ] **Step 2: Run** `pnpm vitest run src/components/__tests__/ScheduleForm.runMode.test.ts` — FAIL.
- [ ] **Step 3: Implement** two radio inputs in one `<fieldset>` with a `<legend>Run mode</legend>`; the `name` attribute must be unique per form instance (`useId()`), never a hard-coded string (radio groups are document-wide). Labels: **Job** — "One agent run in the working directory. Not shown on the board." **Pipeline task** — "Starts in Ready and runs the full pipeline. The working directory must be a git repository."
- [ ] **Step 4: Run** — PASS; `pnpm lint && pnpm typecheck` clean.
- [ ] **Step 5: Mutation** — send no `runMode` in the body → the save test red; restore identical.
- [ ] **Step 6: Commit** `feat(routines): choose job or pipeline task in the routine form`.

### Task 2.16: The board never shows jobs; the needs-you band still does

**Files:**
- Modify: `src/types.ts:138` (`PipelineTask.kind: 'pipeline' | 'job'`, `routineId: string | null`)
- Modify: `src/features/pipeline/composables/useTasks.ts:93-106` (`fetch('/api/tasks?kind=all')`)
- Modify: `src/features/pipeline/components/PipelineBoard.vue:133-145` and the stage map it reads (exclude `kind === 'job'`)
- Test: `src/features/pipeline/components/__tests__/PipelineBoard.jobs.test.ts` (new), `src/features/pipeline/composables/__tests__/useTasks.test.ts` (extend)

- [ ] **Step 1: Write the failing tests:** `useTasks` fetches `/api/tasks?kind=all`; a board mounted with a pipeline task in `implementation` and a job in `job` with `needsUser: true` renders the pipeline card and no job card in any column, needs-you included; update every existing `PipelineTask` fixture the typecheck flags with `kind: 'pipeline', routineId: null` (fixture completeness — a partial fixture silently breaks components).
- [ ] **Step 2: Run** `pnpm vitest run src/features/pipeline` — FAIL.
- [ ] **Step 3: Implement** the filter once, where the board builds its per-stage map, so every column including needs-you inherits it: `tasks.filter(t => t.kind !== 'job')`. The store keeps every task, because `usePendingPermissions(tasks)` (`App.vue:83`) feeds the needs-you band from it.
- [ ] **Step 4: Run** — PASS; `pnpm lint && pnpm typecheck && pnpm test` PASS.
- [ ] **Step 5: Mutation** — remove the filter → the board test red; restore identical.
- [ ] **Step 6: Commit** `feat(board): keep routine jobs off the board`.

### Task 2.17: The routine page shows runs and skipped fires

**Files:**
- Create: `src/composables/useRoutineRuns.ts` (`fetchRoutineRuns(id)` → `GET /api/schedules/{id}/runs`)
- Create: `src/components/RoutineRuns.vue`
- Modify: `src/components/SchedulesView.vue:99-136` (expandable runs section per routine, skip note)
- Test: `src/components/__tests__/RoutineRuns.test.ts` (new), `src/components/__tests__/SchedulesView.test.ts` (extend)

**Interfaces:**
- Consumes: `GET /api/schedules/{id}/runs` (Task 2.12), `ScheduleView.skippedCount/lastSkippedAt` (Task 2.15), `useTasks().selectTask` (`App.vue:82`) to open a run in the existing task modal.
- Produces: `export interface RoutineRun { taskId: string, title: string, kind: 'pipeline' | 'job', stage: string, status: string, summary: string, costCents: number, startedAt?: string, endedAt?: string }`.

- [ ] **Step 1: Write the failing tests:** `RoutineRuns` with two runs renders status, summary, cost via `formatCost` (`src/utils/format.ts`), duration, and an **Open** button that calls `selectTask` with the store task of that id (when the store has none, it calls `refreshTask(id)` first); empty list → "No runs yet"; fetch error → the message, not an empty list. `SchedulesView`: `skippedCount: 3, lastSkippedAt` → text `Skipped 3 times, last at <formatted time>`; `skippedCount: 0` → no skip text; **Run now** refetches the runs of that routine.
- [ ] **Step 2: Run** `pnpm vitest run src/components/__tests__/RoutineRuns.test.ts src/components/__tests__/SchedulesView.test.ts` — FAIL.
- [ ] **Step 3: Implement** with the loading/error/empty states distinct; the runs section refetches on the `schedule_changed` and `task_updated` stream events the schedules composable already listens to (`useSchedules.ts:101-126`) — add `task_updated` only for tasks whose `routineId` matches.
- [ ] **Step 4: Run** — PASS; lint, typecheck clean.
- [ ] **Step 5: Mutation** — render the skip note for `skippedCount: 0` too → red; restore identical.
- [ ] **Step 6: Commit** `feat(routines): show a routine's runs and skipped fires`.

### Task 2.18: The routine page lists and revokes its grants

**Files:**
- Create: `src/components/RoutineGrants.vue`
- Modify: `src/components/SchedulesView.vue` (render it in the expanded routine)
- Test: `src/components/__tests__/RoutineGrants.test.ts` (new)

**Interfaces:**
- Consumes: `useGrants()` (`src/features/settings/composables/useGrants.ts:81-119`), filtered in the component to `contextKind === 'routine' && contextRef === scheduleId && !revokedAt`.

- [ ] **Step 1: Write the failing test:** three grants (routine r1 allow, routine r1 deny, routine r2 allow) → two rows for r1 with capability and **Allowed** / **Denied**; **Revoke** asks for confirmation, then calls `revokeGrant(id)`; empty → "No decisions saved for this routine"; `wrapper.unmount()`.
- [ ] **Step 2: Run** `pnpm vitest run src/components/__tests__/RoutineGrants.test.ts` — FAIL.
- [ ] **Step 3: Implement**, reusing `GrantSettings.vue`'s confirm pattern (`:299-310`, `data-testid` style).
- [ ] **Step 4: Run** — PASS.
- [ ] **Step 5: Mutation** — drop the `contextRef` filter → r2's grant shows, red; restore identical.
- [ ] **Step 6: Commit** `feat(routines): review and revoke a routine's saved decisions`.

### Task 2.19: The client sends decisions

**Files:**
- Modify: `src/features/pipeline/composables/useTasks.ts:343-376` (`resolvePermissionRequest(taskId, requestId, decision)`, `bulkResolvePermissionRequests(taskId, ids, decision, remember)`)
- Modify: `src/composables/usePendingPermissions.ts:77-87` (`decide(taskId, ids, decision, remember)`, keeping `approve`/`deny` as `allow_once`/`deny_once` wrappers), `src/composables/usePermissionResolve.ts`
- Test: the three composables' existing test files (extend)

**Interfaces:**
- Produces: `export type PermissionDecision = 'allow_once' | 'allow_routine' | 'deny_routine' | 'deny_once'` in `useTasks.ts`; request bodies carry `decision`, never `outcome`.

- [ ] **Step 1: Write the failing tests:** single resolve body `{ decision: 'deny_once' }`; bulk body `{ taskId, decision: 'allow_routine', permissionIds, remember: false }`; `approve` sends `allow_once`, `deny` sends `deny_once`; `TaskPendingRequests.vue`'s Grant/Deny still work through `useTaskActions` (update its mocks, `useTaskActions.test.ts:9-23`).
- [ ] **Step 2: Run** `pnpm vitest run src/composables src/features/pipeline/composables` — FAIL.
- [ ] **Step 3: Implement**; map the old `'granted' | 'denied'` call sites to decisions at the call site, not with a translation layer.
- [ ] **Step 4: Run** — PASS; `pnpm typecheck` clean.
- [ ] **Step 5: Mutation** — send `outcome` instead of `decision` in the bulk call → red; restore identical.
- [ ] **Step 6: Commit** `feat(permissions): send permission decisions instead of outcomes`.

### Task 2.20: Decision buttons in the needs-you band

**Files:**
- Modify: `src/features/agents/components/AgentTriageBand.vue:25-30` (emit `decide: [taskId, ids, decision]`), `:743-751` (buttons)
- Modify: `src/App.vue:315-318` (wire `@decide` to `usePendingPermissions().decide`)
- Test: `src/features/agents/components/AgentTriageBand.test.ts` (extend)

**Interfaces:**
- Consumes: `PermissionDecision`, `decide` (Task 2.19); `PipelineTask.routineId` (Task 2.16); an application tool is a tool name matching `/^mcp__[^_].*__.+$/` whose server segment is not `dashboard-channel` or `dashboard-tasks` — put this in `src/utils/applicationTool.ts` with a test, mirroring `mcpapps.IsApplicationTool` (keep both in sync by hand, name each other in a one-line comment).

- [ ] **Step 1: Write the failing tests:** a routine task whose pending requests are all application tools shows **Allow once**, **Always allow for this routine**, **Always deny for this routine**, **Deny**, each emitting `decide` with the matching decision; a routine task with a `Bash` request shows only **Allow** / **Deny**; a task without `routineId` shows only **Allow** / **Deny**; the existing approve/deny/remember tests keep passing; `isApplicationTool` cases: `mcp__mail__read` true, `mcp__dashboard-tasks__list` false, `mcp____read` false, `Bash` false.
- [ ] **Step 2: Run** `pnpm vitest run src/features/agents src/utils` — FAIL.
- [ ] **Step 3: Implement**; the routine buttons carry `title` text saying what is saved ("Saved for this routine; revoke it on the routine page").
- [ ] **Step 4: Run** — PASS; lint, typecheck, `pnpm test` PASS.
- [ ] **Step 5: Mutation** — show the routine buttons without the `routineId` check → the plain-task case red; restore identical.
- [ ] **Step 6: Commit** `feat(permissions): allow or deny an application tool for a whole routine`.

### Task 2.21: Docs, full gates, isolated run, PR

- [ ] **Docs:** `CHANGELOG.md` — `### Added`: job routines (run mode, no worktree, not on the board, routine page with runs, skipped fires, saved decisions, decision buttons, `manage_schedule` `runMode`, `GET /api/tasks?kind=&routineId=`, `GET /api/schedules/{id}/runs`); `### Changed`: existing routines migrated to run mode `pipeline` and fire a `ready` task with `full` autonomy instead of a `backlog` task with `spec_gated`, `task_schedules.current_stage` removed, `GET /api/tasks` returns pipeline tasks unless `kind` is given, permission resolve takes `decision` (`outcome` still accepted); `### Fixed`: B-1 (allowing an application tool now reaches a run with allow-all autonomy), routines stopping after their first fire. `docs/guides/security.md`: routine decisions write grants in the routine context, apply to application tools only, revocable on the routine page. `docs/guides/mcp.md`: `manage_schedule` `runMode`. `docs/guides/configuration.md:241-248`: run modes next to the scheduler keys. Every claim checked against the code with `file:line`.
- [ ] **Full gates** (paste raw output): `cd server && go vet ./... && go test -race ./...` (restore `internal/db/ent/` if it drifted), `cd sdk && go vet ./...`, `cd server && GOTOOLCHAIN=go1.26.6 golangci-lint run ./...`, `pnpm lint && pnpm typecheck && pnpm test`, `pnpm test:e2e`, `git checkout HEAD -- server/frontend/dist/.gitkeep`.
- [ ] **Isolated run** — never the production database: build the server with the embedded SPA, start it from inside a fresh `mktemp -d` directory (so no `.env` is loaded, `server/internal/config/config.go` loads one from the working directory) with `DASHBOARD_DB_PATH=<tmp>/tasks.db`, `DASHBOARD_PORT=<free port>`, `DASHBOARD_WORKTREE_ROOT=<tmp>/worktrees`, then through the HTTP API (with an `Origin` header matching the host on every write): create a routine `runMode: job`, `cwd: <mktemp -d, not a git repo>`, instruction "Write the word ok into result."; `POST /run-now`; poll `GET /api/schedules/{id}/runs` until the run is `done` (timeout 5 min) and show `summary`, `costCents`; `GET /api/tasks` does not list it, `?kind=job&routineId=` does; the temp directory has no `.git` and no worktree was created under the worktree root; `POST` a `runMode: pipeline` routine for the same cwd → 400 with the git message. Kill the server, print the binary's mtime and hash before the run. Paste every response.
- [ ] **PR:** push `feat/routine-jobs`, `gh pr create` with summary, findings B-1..B-6, mutation evidence per task, gate output, isolated-run evidence; wait for CI on the head commit (`gh run list --commit <sha>`), merge with `gh pr merge --squash --admin` when every check is green, `gh pr view <n> --json state,mergedAt`, then `main` CI on the merge commit green.

## PR 3 — Servers in the database (detailed before start)

Spec: applications §2.1, §2.2.

- Task 3.1: `mcp_application.entry`, `export_to_claude`, `exported_hash` (ent + regeneration).
- Task 3.2: Import marker migration from `CLAUDE_CONFIG_DIR/.claude.json` (reserved names skipped, `export_to_claude = true`).
- Task 3.3: Runs and refresh read `entry` from the database instead of `ReadServers`.
- Task 3.4: Applications API: create, update, delete (409 while attached, naming routines).
- Task 3.5: Export writer through the materializer (only `mcpServers.<name>`, atomic, mode kept, symlink refused, no secrets).
- Task 3.6: `fsnotify` watch: "found" for unknown servers, "changed outside" for exported entries whose hash differs.
- Task 3.7: Settings → Applications UI: add/edit/remove, export switch, found and changed banners.
- Task 3.8: Docs, full gates, PR.

## PR 4 — Setup UI, grants from use, default denies (detailed before start)

Spec: applications §2.3, §2.4.

- Task 4.1: Verify `imap_list_accounts` output against the published server; record the shape.
- Task 4.2: Preset file format: `match`, `denyGlobal`, `setup`, `secretTemplates`; remove `allowForRoutine`, `confirmed`, `ErrPresetUnconfirmed`.
- Task 4.3: Default denies applied on add, import and first recognition; idempotent re-apply endpoint.
- Task 4.4: Setup launcher: free port, readiness, one process per application, stop on done/idle/max/shutdown.
- Task 4.5: Secret names from `imap_list_accounts` with `secretTemplates` and name normalisation.
- Task 4.6: Needs-you: no decision buttons for default-denied tools; per-tool state list in Settings → Applications.
- Task 4.7: Setup panel UI (iframe + new-window fallback, network warning, password step).
- Task 4.8: Docs, full gates, isolated-instance acceptance of the mail flow without console, PR.
