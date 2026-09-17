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
- ent regeneration only via `cd server && go generate ./internal/db/ent/`; afterwards `grep -rl "OnConflict" server/internal/db/ent/ | head` must print files.
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

## PR 2 — Job kind and run mode (detailed before start)

Spec: routines §2.1, §2.2, §2.4, and the routine-context decisions of §2.3 (`allow_routine`, `deny_routine`).

- Task 2.1: `tasks.kind` column (ent schema + regeneration), kind-aware `StageOrder`/`NextStage`, job never enters a pipeline stage.
- Task 2.2: Job stage handler: no worktree (`EnsureWorktreeFn` never called), agent `cwd` = task `cwd`, job system prompt, `set_stage_output` contract `summary`/`result`, `job → done`.
- Task 2.3: `task_schedule.run_mode`, `last_skipped_at`, `skipped_count`; drop `current_stage`; migration sets existing rows to `pipeline`.
- Task 2.4: Materializer and scheduler: kind/stage/autonomy per run mode; skip recording; git check for `pipeline` routines.
- Task 2.5: Tasks API `kind` filter (board excludes jobs by default) and `routineId` filter.
- Task 2.6: Resolve endpoint `decision` (`allow_once`, `allow_routine`, `deny_routine`, `deny_once`; `outcome` accepted as alias for `allow_once`/`deny_once`), grant rows in the routine context.
- Task 2.7: Routine page UI: run mode in the form, run history, skip note, routine grants; needs-you decision buttons per case.
- Task 2.8: Docs, full gates, isolated-instance check that a job routine runs unattended end to end, PR.

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
