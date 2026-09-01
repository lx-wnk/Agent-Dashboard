# Wire-Format DTO Sweep Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every HTTP endpoint that today encodes a raw ent entity answers with a hand-written camelCase DTO instead, so the field names on the wire are the field names `src/types.ts` declares and a `false` or a `0` arrives as `false` and `0` rather than not arriving at all.

**Architecture:** No schema change, no new route, no new dependency. Each affected handler gains a response struct whose json tags are camelCase and carry **no** `omitempty`, plus a `toXResponse` mapper next to it, and its `jsonReply`/`WriteJSON` call is changed to pass the mapped value. This is exactly the shape PR #412 (`508d145d`) already established for task permissions and audit events in `server/internal/api/tasks/handler.go`. Two endpoints need more than a rename: the dependency routes must resolve titles and the upstream stage from the referenced tasks, and the JSON export embeds a task DTO rather than the entity.

**Tech Stack:** Go 1.26 (chi, ent ORM), Vue 3 + TypeScript SPA (Vite, pnpm)

**Spec:** none — this plan is driven by the inventory in §Inventory below, produced by a full scan of `server/internal/api/` on 2026-09-01.

## Global Constraints

- **Read PR #412 first.** `git show 508d145d` is the reference implementation. Copy its DTO shape (`taskPermissionResponse`, `toTaskPermissionResponse`, `auditEntryResponse`, `toAuditEntryResponse`, `auditActor` in `server/internal/api/tasks/handler.go:855-985`) and, above all, its **test style** in `server/internal/api/tasks/wire_format_test.go`. That file asserts the RAW key set: it unmarshals into `map[string]any` and checks that every camelCase key is present AND every snake_case key is absent. Unmarshalling into a correctly-tagged struct would pass both before and after the fix and prove nothing. `decodeRows` and `assertKeys` already exist there — reuse them, do not redefine them.
- **`omitempty` is the defect, not just the casing.** Every DTO in this plan is written **without** `omitempty` on any field. That is the whole point: `granted: false`, `priority: 0`, `iteration: 0` and `costCents: 0` must be sent, and a nullable field must be sent as `null`, because `src/types.ts` declares `number` and `T | null`, not `number | undefined`.
- **Never run `go test ./...` or `task test`.** Both regenerate `server/internal/db/ent/`, which then appears as unrelated noise in the diff. Scope every Go test run to the package under change. If the tree is already dirty under `server/internal/db/ent/`, restore it with `git checkout -- server/internal/db/ent/` before committing.
- **`gofmt -l` is mandatory in every Go gate.** CI runs `golangci-lint fmt --diff`, which fails on struct-field and comment alignment that `go build`, `go vet` and `go test` all accept. A green build is not evidence of a green CI. Every DTO in this plan is a struct literal with aligned tags — exactly the construct that trips it.
- **`go vet ./...` runs module-wide on purpose.** A narrow `go test ./internal/api/tasks/...` misses `_test.go` files in sibling packages that reference a changed exported type, and `go build` skips test files entirely. Task 5 changes an exported type (`EnrichedTask`) — module-wide vet is not optional there.
- **No route is added or removed.** `server/internal/api/testdata/routes.golden` must stay untouched; if `TestRouteGolden` fails, a route changed by accident and the change is wrong.
- **One commit per task**, Conventional Commits (`fix:` for every task here — each restores a contract the client already declared), English subject and body, no task or phase numbers in the message.
- **Gate per task.**
  - Go: `cd server && go build ./... && go vet ./... && gofmt -l ./internal/api/... && go test -count=1 ./internal/api/<pkg>/...`
  - Frontend (any task touching `src/`): `pnpm lint && pnpm typecheck && pnpm test`
  - Paste the raw output. A summary is not evidence.

---

## CRITICAL — three exceptions that are NOT defects

Do not "fix" any of these. Each was checked at plan time and each is a deliberate, working contract.

1. **`GET /api/grants`, `POST /api/grants`, `GET /api/capabilities` are deliberately snake_case.** `src/features/settings/composables/useGrants.ts:3-6` says so in a comment above the interfaces: *"Grant and Capability are ent-generated rows … encoded straight from their Go struct tags, which are ent's default snake_case — unlike the rest of this codebase's camelCase DTOs, so the field names here intentionally do not match the sdk.generated.ts convention."* `Capability.enforceable_by`, `Grant.capability_name`, `Grant.granted_by`, `Grant.revoked_at` and the rest are read under those exact names by `GrantSettings.vue`. Converting them breaks that panel. **Leave `server/internal/api/grants/` and `server/internal/api/capabilities/` alone.**
2. **`plan/service.go`'s `PlanStatusResult` ships `gate_state` / `approved_plan` and the client reads exactly those.** `src/features/pipeline/composables/usePlanReview.ts:37` types the `GET /api/plan/{taskId}/status` body as `{ gate_state: string, approved_plan?: Record<string, unknown> | null }` and `:42` reads `data.approved_plan`. Both sides agree. Not a defect. Task 5 touches `POST /api/plan/{taskId}/approve` only — do not touch `status`.
3. **Anything already converting to a DTO is clean.** `EnrichedTask` (`server/internal/api/tasks/enrich.go:19`), `toSpawnerView`, `toProjectView`, `toView` in `prompttemplates`/`schedules`, `eval`'s `toMetricDTO`/`toDriftDTO`, `permissionRequestResponse` (`permission_request_routes.go:23`), `pipelineConfigResponse`, `notifPref` (`notification_routes.go:83` — a local struct with its own camelCase tags, **not** an ent entity, despite what #412's commit body implies). None of these are in scope.

---

## Inventory

Produced by a full scan of `server/internal/api/` on 2026-09-01. Every line number below was re-verified against the tree at `508d145d`.

### BROKEN + CONSUMED (must fix — a component reads a field that is missing today)

| endpoint | handler | ent type | what breaks in the UI |
|---|---|---|---|
| `GET /api/tasks/{id}/stage-runs` | `tasks/handler.go:852` `listStageRuns` | `[]*ent.StageRun` | `TaskStagesTab.vue:16-30` reads `run.sessionName`/`run.startedAt`/`run.endedAt`; `src/utils/taskFormat.ts:38` `activeRuntime` reads `r.startedAt`/`r.endedAt` — all `undefined` |
| `GET /api/tasks/{id}/dependencies` | `tasks/dependency_routes.go:23` | `[]*ent.TaskDependency` | `TaskDependenciesTab.vue:44,49,51` reads `dep.dependsOnTitle`, `dep.dependsOnStage`, `dep.onCancelAction` |
| `GET /api/tasks/{id}/dependents` | `tasks/dependency_routes.go:35` | `[]*ent.TaskDependency` | `TaskDependenciesTab.vue:64` reads `dep.taskTitle` |
| `GET/POST/PUT /api/settings/system-prompts` | `systemprompts/handler.go:42,69,86` | `*ent.SystemPrompt` | names match, but `priority` is `int` with `omitempty` so `priority: 0` is dropped; `SystemPromptSettings.vue:188` displays it and `:64` re-seeds the edit form |

**Corrections to the scan, verified against the code at plan time:**

- `TaskDependenciesTab.vue`'s real line numbers are `:47` (`dep.dependsOnTitle`), `:50` and `:53` (`dep.dependsOnStage`), `:54` (`dep.onCancelAction`), `:66` (`dep.taskTitle || dep.taskId`). The endpoints and the fields are exactly as the scan says; only the line numbers drifted by three.
- `SystemPromptSettings.vue:64` and `:188` are exact. But the component's own `SystemPrompt` interface (`:10-19`) declares `created_at`, `updated_at` and `created_by` in **snake_case**, matching the server today. So on that endpoint the only field the client cannot read is `priority` when it is `0` (and `stage` when null, which `p.stage ?? ''` happens to survive). The casing of the three timestamps is a latent inconsistency, not a live bug — this plan converts them anyway and updates the component interface in the same commit, so the whole API speaks one convention.

### The dependency rows need more than renaming

`taskTitle`, `dependsOnTitle` and `dependsOnStage` **do not exist on `ent.TaskDependency`**. The entity has exactly five columns (`server/internal/db/ent/schema/task_dependency.go:12-19`): `id`, `task_id`, `depends_on_id`, `required_stage`, `on_cancel_action`. There is no `created_at` either.

Two findings settle the design:

- **`dependsOnStage` is NOT `required_stage`.** `TaskDependenciesTab.vue:50` renders the badge green or red on `dep.dependsOnStage === dep.requiredStage`. A comparison of a field with itself would be constant-green, so `dependsOnStage` is the **current stage of the upstream task** and `requiredStage` is the stored column. `DependencyGraph.vue:79-85` confirms it: it feeds `dep.dependsOnStage` into `stageColor(stage)` as the node's stage. So a join is genuinely required; only `requiredStage` maps to the `required_stage` column.
- **`createdAt` has no column and never had one.** `src/types.ts:136` declares it; nothing in `src/` reads it. It is dropped from the TS interface in Task 2 rather than invented on the server.

**Chosen approach: resolve the referenced tasks in the handler through the existing `repo.TaskRepo.ListByIDs`, not through ent eager-loading.** `ListByIDs` (`server/internal/db/repo/task_repo.go:358-367`) already exists and issues one `WHERE id IN (…)` query. The alternative — adding `.WithTask().WithDependsOn()` inside `entDependencyRepo.ListUpstream`/`ListDownstream` (`server/internal/db/repo/task_dependency_repo.go:55-73`) — would add joins to two **non-API hot-path callers** that need none: `server/internal/pipeline/dependency_eval.go:57` and `server/internal/pipeline/orchestrator.go:823`. Adding a `ListUpstreamWithTasks` variant instead would widen the `DependencyRepo` interface and break `fakeDepRepo` in `server/internal/pipeline/dependency_eval_test.go:47-55`. The handler-side lookup costs one extra query on a display-only route, changes no interface, and touches no hot path.

### BROKEN + UNCONSUMED (20 endpoints — the response body is discarded by every caller today)

`PATCH /api/tasks/{id}` (`handler.go:656`), `POST /api/tasks/{id}/cancel` (`:751`) `|hold` (`:814`) `|progress` (`:732`) `|retry` (`:777`) `|resume` (`:843`), `POST /api/tasks/{id}/resume-stage` (`resume_routes.go:31`), `POST /api/tasks/{id}/dependencies` (`dependency_routes.go:69`), `POST /api/permission-requests` (`permission_request_routes.go:132,150`), `POST /api/tasks/{id}/permissions/bulk` (`permission_request_routes.go:279`), `POST /api/tasks/{id}/permission-requests/{reqID}/resolve` (`handler.go:1050`), `GET /api/tasks/export?format=json` (`export_routes.go:59-70`), `POST /api/plan/{taskId}/approve` (`plan/handler.go:54`), `POST /api/refine/{taskId}/confirm` (`refine/handler.go:251`), and the six `/api/memory/*` routes (`memory/handler.go:87,122,150,225,276,318`).

**Correction: `POST /api/plan/{taskId}/approve` is not entirely unconsumed.** `usePlanReview.ts:93` does `return await res.json() as PipelineTask`, and `PlanReviewPanel.vue:50-53` emits it. But the only use is truthiness (`if (updated) emit('approved', updated)`) and `App.vue:448` ignores the payload. So no field is read — the classification holds — but the body **is** parsed and **is** typed as `PipelineTask`, which today it is not. Same story for `POST /api/refine/{taskId}/confirm`, reached through `runAction` (`src/composables/useRunAction.ts:38-52`), which discards the body entirely.

`GET /api/memory/entries` (`memory/handler.go:150`) is a different symptom in the same class: it returns `[]memory.Entry`, a struct with **no json tags at all** (`server/internal/memory/retrieve.go:48-56`), so the wire carries PascalCase Go field names (`ID`, `SpaceID`, `Summary`, `Content`, `Kind`, `Confidence`, `CreatedAt`).

### Ordering requirement

**The `/api/memory/*` routes are converted in THIS plan (Task 4) even though nothing consumes them, and they must land before any memory UI is built.** The sibling plan `docs/superpowers/plans/2026-09-01-agenticos-visibility-wave.md` adds a memory panel and an injections view, and its own Global Constraints already declare those two tasks blocked on this plan's memory task. Building a client against `space_id` / `Summary` / `stage_run_id` and then changing the wire underneath it would mean writing a component twice and shipping a broken intermediate. Task 4 exists to unblock that plan, not because anything is visibly wrong today.

---

### Task 1: `GET /api/tasks/{id}/stage-runs` answers the shape `StageRun` declares

**Problem.** `listStageRuns` (`server/internal/api/tasks/handler.go:846-853`) replies with `[]*ent.StageRun`. `ent.StageRun` (`server/internal/db/ent/stagerun.go:18-59`) is tagged `task_id`, `session_id`, `session_name`, `started_at`, `ended_at`, `tokens_used`, `cost_cents`, `last_grant_at`, all with `omitempty`, plus `Edges StageRunEdges` tagged `json:"edges"` with no omitempty at all. `src/types.ts:224-244` declares `taskId`, `sessionId`, `sessionName`, `startedAt`, `endedAt`, `tokensUsed`, `costCents`, `lastGrantAt`. So `TaskStagesTab.vue:24` (`v-if="run.sessionName"`) is never true, `:28` renders `started {{ formatTaskDate(undefined) }} · ended …`, and `activeRuntime` (`src/utils/taskFormat.ts:38-47`) returns `'—'` for every task because `r.startedAt` is always falsy. On a fresh run `iteration: 0`, `tokensUsed: 0` and `costCents: 0` are dropped entirely on top of that.

**Fields deliberately not on the DTO:** `retry_count`, `next_retry_at`, `pending_user_prompt` and `created_at`. Nothing in `src/` reads them from this endpoint; `retryCount`/`nextRetryAt` already reach the client through `EnrichedTask.autoRetryCount`/`nextRetryAt`, and `pending_user_prompt` is the agent's queued input, not run history. Same reasoning #412 applied to `manual_override`. The test asserts their absence.

### Files

- Modify: `server/internal/api/tasks/handler.go` — add `stageRunResponse`, `toStageRunResponse`, `toStageRunResponses`; change one `jsonReply`
- Modify: `server/internal/api/tasks/wire_format_test.go` — one new test

### Steps

- [ ] **RED — add the failing test.** Append to `server/internal/api/tasks/wire_format_test.go`:

```go
// TestListStageRuns_WireFormat asserts the camelCase keys src/types.ts declares
// for StageRun, and that a zero iteration/cost/token count is sent as 0 rather
// than dropped by the entity's omitempty tags.
func TestListStageRuns_WireFormat(t *testing.T) {
	client, r := newTestHandlerWithRepos(t)
	task, err := repo.NewTaskRepo(client).Create(testCtx(t), repo.CreateTaskInput{
		Slug:          "stage-run-wire-format",
		Title:         "Stage Run Wire Format",
		Cwd:           t.TempDir(),
		MaxIterations: 5,
		Priority:      "normal",
		CurrentStage:  "implementation",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	srRepo := repo.NewStageRunRepo(client)
	finished, err := srRepo.Create(testCtx(t), repo.CreateStageRunInput{
		TaskID:      task.ID,
		Stage:       "implementation",
		Iteration:   1,
		SessionName: "impl-session",
	})
	if err != nil {
		t.Fatalf("create finished run: %v", err)
	}
	started := time.Now().Add(-time.Hour)
	ended := time.Now()
	done := "done"
	sessionID := "sess-1"
	tokens := 1234
	cost := 42
	if _, err = srRepo.Update(testCtx(t), finished.ID, repo.UpdateStageRunInput{
		Status:     &done,
		SessionID:  &sessionID,
		StartedAt:  &started,
		EndedAt:    &ended,
		TokensUsed: &tokens,
		CostCents:  &cost,
	}); err != nil {
		t.Fatalf("update finished run: %v", err)
	}
	// Zero values: iteration, tokensUsed and costCents are all 0 here, which the
	// entity's omitempty tags drop from the payload entirely.
	if _, err = srRepo.Create(testCtx(t), repo.CreateStageRunInput{
		TaskID:    task.ID,
		Stage:     "self_review",
		Iteration: 0,
	}); err != nil {
		t.Fatalf("create pending run: %v", err)
	}

	req := withAuth(t, httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID+"/stage-runs", nil))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	rows := decodeRows(t, w.Body.Bytes())
	if len(rows) != 2 {
		t.Fatalf("expected 2 stage runs, got %d: %s", len(rows), w.Body.String())
	}

	byStage := map[string]map[string]any{}
	want := []string{"id", "taskId", "stage", "sessionId", "sessionName", "pid", "status", "iteration", "output", "tokensUsed", "costCents", "startedAt", "endedAt", "lastGrantAt"}
	forbidden := []string{"task_id", "session_id", "session_name", "started_at", "ended_at", "tokens_used", "cost_cents", "last_grant_at", "created_at", "retry_count", "next_retry_at", "pending_user_prompt", "edges"}
	for _, row := range rows {
		stage, _ := row["stage"].(string)
		byStage[stage] = row
		t.Run(stage, func(t *testing.T) {
			assertKeys(t, row, want, forbidden)
		})
	}

	impl := byStage["implementation"]
	if impl["taskId"] != task.ID {
		t.Errorf("taskId = %v, want %q", impl["taskId"], task.ID)
	}
	if impl["sessionName"] != "impl-session" {
		t.Errorf("sessionName = %v, want %q", impl["sessionName"], "impl-session")
	}
	if impl["startedAt"] == nil {
		t.Error("startedAt = null, want a timestamp for a run that started")
	}
	if impl["endedAt"] == nil {
		t.Error("endedAt = null, want a timestamp for a finished run")
	}
	if impl["tokensUsed"] != float64(1234) {
		t.Errorf("tokensUsed = %v, want 1234", impl["tokensUsed"])
	}

	pending := byStage["self_review"]
	if pending["iteration"] != float64(0) {
		t.Errorf("iteration = %v, want 0 (not omitted)", pending["iteration"])
	}
	if pending["costCents"] != float64(0) {
		t.Errorf("costCents = %v, want 0 (not omitted)", pending["costCents"])
	}
	if pending["startedAt"] != nil {
		t.Errorf("startedAt = %v, want null", pending["startedAt"])
	}
	if pending["output"] != nil {
		t.Errorf("output = %v, want null (not omitted)", pending["output"])
	}
}
```

  Add `"time"` to the import block of `wire_format_test.go` if it is not already there.

- [ ] **Run it and confirm it fails for the right reason.** `cd server && go test -count=1 ./internal/api/tasks/ -run TestListStageRuns_WireFormat` — expect `missing key "taskId"`, `missing key "sessionName"`, `missing key "startedAt"`, `unexpected key "task_id"`, `unexpected key "edges"`. A compile error or a 404 means the fixture is wrong, not the feature.

- [ ] **GREEN — add the DTO.** In `server/internal/api/tasks/handler.go`, immediately **above** `func (h *Handler) listStageRuns`, insert:

```go
// stageRunResponse is the API response shape for one stage run. The ent entity's
// own JSON tags are the storage column names and carry omitempty, which drops a
// zero iteration, token count or cost from the payload instead of sending 0, and
// leaks the empty edges container.
//
// retry_count, next_retry_at, pending_user_prompt and created_at are deliberately
// absent: no client reads them off this route, the first two already reach the
// browser as EnrichedTask.autoRetryCount/nextRetryAt, and the third is the agent's
// queued input rather than run history.
type stageRunResponse struct {
	ID          string         `json:"id"`
	TaskID      string         `json:"taskId"`
	Stage       string         `json:"stage"`
	SessionID   *string        `json:"sessionId"`
	SessionName *string        `json:"sessionName"`
	Pid         *int           `json:"pid"`
	Status      string         `json:"status"`
	Iteration   int            `json:"iteration"`
	Output      map[string]any `json:"output"`
	TokensUsed  int            `json:"tokensUsed"`
	CostCents   int            `json:"costCents"`
	StartedAt   *time.Time     `json:"startedAt"`
	EndedAt     *time.Time     `json:"endedAt"`
	LastGrantAt *time.Time     `json:"lastGrantAt"`
}

func toStageRunResponse(sr *ent.StageRun) stageRunResponse {
	return stageRunResponse{
		ID:          sr.ID,
		TaskID:      sr.TaskID,
		Stage:       sr.Stage,
		SessionID:   sr.SessionID,
		SessionName: sr.SessionName,
		Pid:         sr.Pid,
		Status:      sr.Status,
		Iteration:   sr.Iteration,
		Output:      sr.Output,
		TokensUsed:  sr.TokensUsed,
		CostCents:   sr.CostCents,
		StartedAt:   sr.StartedAt,
		EndedAt:     sr.EndedAt,
		LastGrantAt: sr.LastGrantAt,
	}
}

func toStageRunResponses(runs []*ent.StageRun) []stageRunResponse {
	resp := make([]stageRunResponse, len(runs))
	for i, sr := range runs {
		resp[i] = toStageRunResponse(sr)
	}
	return resp
}
```

  `time` is already imported in this file (added by #412). `ent.StageRun.Output` is declared as `map[string]interface{}`, which is identical to `map[string]any` — no conversion is needed.

- [ ] **GREEN — use it.** In the same file, change the last line of `listStageRuns`:

```go
	return jsonReply(w, http.StatusOK, toStageRunResponses(runs))
```

- [ ] **Run the test and see it green.** `cd server && go test -count=1 ./internal/api/tasks/ -run TestListStageRuns_WireFormat`

- [ ] **Run the full gate and paste the raw output.** `cd server && go build ./... && go vet ./... && gofmt -l ./internal/api/... && go test -count=1 ./internal/api/tasks/...`

- [ ] **Check the ent tree is clean.** `git status --short server/internal/db/ent/` must print nothing.

- [ ] **Commit.** `fix(api): reply with camelCase stage runs`

---

### Task 2: the dependency routes carry the titles and the upstream stage

**Problem.** `listDependencies` and `listDependents` (`server/internal/api/tasks/dependency_routes.go:14-36`) reply with `[]*ent.TaskDependency`, tagged `task_id`, `depends_on_id`, `required_stage`, `on_cancel_action` plus `edges`. `TaskDependenciesTab.vue` reads `dep.dependsOnTitle` (`:47`), `dep.dependsOnStage` (`:50`, `:53`), `dep.onCancelAction` (`:54`) and `dep.taskTitle` (`:66`) — so the "Waiting for:" list renders a nameless row, a badge with no label, and `on cancel:` followed by nothing. The "Needed by:" list falls back to the raw task id for every row.

Three of the five fields the client reads are not columns at all. See §Inventory for why `dependsOnStage` is the upstream task's `current_stage` and not the `required_stage` column, and why the join is done in the handler via `repo.TaskRepo.ListByIDs` rather than by ent eager-loading.

**`newTestHandlerWithRepos` does not wire `DepRepo` today** (`server/internal/api/tasks/handler_test.go:414-426`), so `h.depRepo.ListUpstream` on a handler built by it is a nil-interface call and panics. That one line is part of this task. `newCheckpointHandler` (`checkpoint_routes_test.go:54`) already sets it — copy that line.

### Files

- Modify: `server/internal/api/tasks/dependency_routes.go` — add `taskDependencyResponse`, `toDependencyResponses`; change three replies
- Modify: `server/internal/api/tasks/handler_test.go` — add `DepRepo` to `newTestHandlerWithRepos`
- Modify: `server/internal/api/tasks/wire_format_test.go` — one new test
- Modify: `src/types.ts` — drop `createdAt` from `TaskDependency`
- Modify: `src/features/pipeline/composables/__tests__/useTaskDependencies.test.ts` — drop `createdAt` from the fixture

### Steps

- [ ] **Wire the repo into the shared test harness first.** In `server/internal/api/tasks/handler_test.go`, inside the `tasks.NewHandler(tasks.Deps{…})` literal in `newTestHandlerWithRepos` (line 414), add after the `CfgRepo:` line:

```go
		DepRepo:      repo.NewDependencyRepo(client),
```

  Without this the new test panics with a nil-pointer dereference instead of failing on the key set, and the RED step below would prove nothing.

- [ ] **RED — add the failing test.** Append to `server/internal/api/tasks/wire_format_test.go`:

```go
// TestListDependencies_WireFormat asserts the dependency rows carry the joined
// titles and the upstream task's current stage, which ent.TaskDependency stores
// no column for, under the camelCase names TaskDependenciesTab.vue reads.
func TestListDependencies_WireFormat(t *testing.T) {
	client, r := newTestHandlerWithRepos(t)
	taskRepo := repo.NewTaskRepo(client)
	downstream, err := taskRepo.Create(testCtx(t), repo.CreateTaskInput{
		Slug:          "dep-wire-downstream",
		Title:         "Downstream Task",
		Cwd:           t.TempDir(),
		MaxIterations: 5,
		Priority:      "normal",
		CurrentStage:  "backlog",
	})
	if err != nil {
		t.Fatalf("create downstream: %v", err)
	}
	upstream, err := taskRepo.Create(testCtx(t), repo.CreateTaskInput{
		Slug:          "dep-wire-upstream",
		Title:         "Upstream Task",
		Cwd:           t.TempDir(),
		MaxIterations: 5,
		Priority:      "normal",
		CurrentStage:  "implementation",
	})
	if err != nil {
		t.Fatalf("create upstream: %v", err)
	}
	if _, err = repo.NewDependencyRepo(client).Add(testCtx(t), downstream.ID, upstream.ID, "done", "on_hold"); err != nil {
		t.Fatalf("add dependency: %v", err)
	}

	want := []string{"id", "taskId", "taskTitle", "dependsOnId", "dependsOnTitle", "dependsOnStage", "requiredStage", "onCancelAction"}
	forbidden := []string{"task_id", "depends_on_id", "required_stage", "on_cancel_action", "created_at", "createdAt", "edges"}

	t.Run("upstream", func(t *testing.T) {
		req := withAuth(t, httptest.NewRequest(http.MethodGet, "/api/tasks/"+downstream.ID+"/dependencies", nil))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		rows := decodeRows(t, w.Body.Bytes())
		if len(rows) != 1 {
			t.Fatalf("expected 1 dependency, got %d: %s", len(rows), w.Body.String())
		}
		assertKeys(t, rows[0], want, forbidden)
		if rows[0]["dependsOnTitle"] != "Upstream Task" {
			t.Errorf("dependsOnTitle = %v, want %q", rows[0]["dependsOnTitle"], "Upstream Task")
		}
		if rows[0]["dependsOnStage"] != "implementation" {
			t.Errorf("dependsOnStage = %v, want the upstream task's current stage %q", rows[0]["dependsOnStage"], "implementation")
		}
		if rows[0]["requiredStage"] != "done" {
			t.Errorf("requiredStage = %v, want %q", rows[0]["requiredStage"], "done")
		}
		if rows[0]["onCancelAction"] != "on_hold" {
			t.Errorf("onCancelAction = %v, want %q", rows[0]["onCancelAction"], "on_hold")
		}
	})

	t.Run("downstream", func(t *testing.T) {
		req := withAuth(t, httptest.NewRequest(http.MethodGet, "/api/tasks/"+upstream.ID+"/dependents", nil))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		rows := decodeRows(t, w.Body.Bytes())
		if len(rows) != 1 {
			t.Fatalf("expected 1 dependent, got %d: %s", len(rows), w.Body.String())
		}
		assertKeys(t, rows[0], want, forbidden)
		if rows[0]["taskTitle"] != "Downstream Task" {
			t.Errorf("taskTitle = %v, want %q", rows[0]["taskTitle"], "Downstream Task")
		}
	})

	t.Run("create", func(t *testing.T) {
		third, err := taskRepo.Create(testCtx(t), repo.CreateTaskInput{
			Slug:          "dep-wire-third",
			Title:         "Third Task",
			Cwd:           t.TempDir(),
			MaxIterations: 5,
			Priority:      "normal",
			CurrentStage:  "concept",
		})
		if err != nil {
			t.Fatalf("create third: %v", err)
		}
		body := `{"dependsOnId":"` + third.ID + `","requiredStage":"done","onCancelAction":"cancel"}`
		req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/tasks/"+downstream.ID+"/dependencies", strings.NewReader(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var row map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &row); err != nil {
			t.Fatalf("unmarshal response: %v (body: %s)", err, w.Body.String())
		}
		assertKeys(t, row, want, forbidden)
		if row["dependsOnStage"] != "concept" {
			t.Errorf("dependsOnStage = %v, want %q", row["dependsOnStage"], "concept")
		}
	})
}
```

- [ ] **Run it and confirm it fails for the right reason.** `cd server && go test -count=1 ./internal/api/tasks/ -run TestListDependencies_WireFormat` — expect `missing key "taskTitle"`, `missing key "dependsOnTitle"`, `missing key "dependsOnStage"`, `unexpected key "task_id"`, `unexpected key "edges"` in all three subtests. A nil-pointer panic means the `DepRepo` step above was skipped.

- [ ] **GREEN — add the DTO and the join.** In `server/internal/api/tasks/dependency_routes.go`, insert after the import block:

```go
// taskDependencyResponse is the API response shape for one dependency edge.
// ent.TaskDependency stores only the two task ids and the two settings, so the
// titles and the upstream task's current stage are resolved from the referenced
// tasks — TaskDependenciesTab.vue and DependencyGraph.vue render all three, and
// dependsOnStage is what the required-stage badge is compared against.
type taskDependencyResponse struct {
	ID             string `json:"id"`
	TaskID         string `json:"taskId"`
	TaskTitle      string `json:"taskTitle"`
	DependsOnID    string `json:"dependsOnId"`
	DependsOnTitle string `json:"dependsOnTitle"`
	DependsOnStage string `json:"dependsOnStage"`
	RequiredStage  string `json:"requiredStage"`
	OnCancelAction string `json:"onCancelAction"`
}

// toDependencyResponses resolves both endpoints of every edge in one query via
// TaskRepo.ListByIDs. Eager-loading the edges in DependencyRepo instead would
// add the joins to pipeline.EvaluateTaskDeps and the orchestrator's downstream
// sweep, neither of which reads a title.
//
// A row whose referenced task no longer resolves answers with empty strings
// rather than failing the whole listing: the tab already falls back to the id.
func (h *Handler) toDependencyResponses(ctx context.Context, deps []*ent.TaskDependency) ([]taskDependencyResponse, error) {
	resp := make([]taskDependencyResponse, 0, len(deps))
	if len(deps) == 0 {
		return resp, nil
	}
	ids := make([]string, 0, len(deps)*2)
	for _, d := range deps {
		ids = append(ids, d.TaskID, d.DependsOnID)
	}
	tasks, err := h.taskRepo.ListByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*ent.Task, len(tasks))
	for _, t := range tasks {
		byID[t.ID] = t
	}
	for _, d := range deps {
		row := taskDependencyResponse{
			ID:             d.ID,
			TaskID:         d.TaskID,
			DependsOnID:    d.DependsOnID,
			RequiredStage:  d.RequiredStage,
			OnCancelAction: d.OnCancelAction,
		}
		if t, ok := byID[d.TaskID]; ok {
			row.TaskTitle = t.Title
		}
		if t, ok := byID[d.DependsOnID]; ok {
			row.DependsOnTitle = t.Title
			row.DependsOnStage = t.CurrentStage
		}
		resp = append(resp, row)
	}
	return resp, nil
}
```

  Add `"context"` to the import block of `dependency_routes.go` (it currently imports `encoding/json`, `fmt`, `net/http`, chi, apierr, ent, repo).

- [ ] **GREEN — use it on all three routes.** In the same file, replace the two listings:

```go
func (h *Handler) listDependencies(w http.ResponseWriter, r *http.Request) error {
	taskID := chi.URLParam(r, "id")
	deps, err := h.depRepo.ListUpstream(r.Context(), taskID)
	if err != nil {
		return fmt.Errorf("dependencies.list: %w", err)
	}
	resp, err := h.toDependencyResponses(r.Context(), deps)
	if err != nil {
		return fmt.Errorf("dependencies.list.resolve: %w", err)
	}
	return jsonReply(w, http.StatusOK, resp)
}

func (h *Handler) listDependents(w http.ResponseWriter, r *http.Request) error {
	taskID := chi.URLParam(r, "id")
	deps, err := h.depRepo.ListDownstream(r.Context(), taskID)
	if err != nil {
		return fmt.Errorf("dependents.list: %w", err)
	}
	resp, err := h.toDependencyResponses(r.Context(), deps)
	if err != nil {
		return fmt.Errorf("dependents.list.resolve: %w", err)
	}
	return jsonReply(w, http.StatusOK, resp)
}
```

  The `if deps == nil { deps = []*ent.TaskDependency{} }` guards are dropped because `toDependencyResponses` returns a non-nil empty slice, which encodes as `[]` — the reason those guards existed.

  And in `addDependency`, replace the final `return jsonReply(w, http.StatusCreated, dep)` with:

```go
	resp, err := h.toDependencyResponses(r.Context(), []*ent.TaskDependency{dep})
	if err != nil {
		return fmt.Errorf("dependencies.add.resolve: %w", err)
	}
	return jsonReply(w, http.StatusCreated, resp[0])
```

- [ ] **GREEN — drop the invented `createdAt` from the client type.** In `src/types.ts`, in the `TaskDependency` interface (line 127), delete the line `  createdAt: string`. There is no `created_at` column on `task_dependency` and nothing in `src/` reads the field.

- [ ] **GREEN — drop it from the fixture too.** In `src/features/pipeline/composables/__tests__/useTaskDependencies.test.ts`, in `makeDependency` (line 47), delete the line `    createdAt: '2026-01-01T00:00:00Z',`. Leaving it is a typecheck error (excess property on an object literal).

- [ ] **Run the tests and see them green.** `cd server && go test -count=1 ./internal/api/tasks/ -run TestListDependencies_WireFormat` and `pnpm test src/features/pipeline/composables/__tests__/useTaskDependencies.test.ts`

- [ ] **Run the full gate and paste the raw output.** `cd server && go build ./... && go vet ./... && gofmt -l ./internal/api/... && go test -count=1 ./internal/api/tasks/...` then `pnpm lint && pnpm typecheck && pnpm test`

- [ ] **Check the ent tree is clean.** `git status --short server/internal/db/ent/` must print nothing.

- [ ] **Commit.** `fix(api): resolve dependency titles and upstream stage on the wire`

---

### Task 3: system prompts stop dropping `priority: 0`

**Problem.** All four `/api/settings/system-prompts` handlers (`server/internal/api/systemprompts/handler.go:34-88`) reply with `*ent.SystemPrompt`. Every field on that entity carries `omitempty` (`server/internal/db/ent/systemprompt.go`), so a prompt created with the form's default `priority: 0` goes over the wire without a `priority` key at all. `SystemPromptSettings.vue:188` renders `{{ p.priority }}` — a blank cell — and `:64` seeds the edit form with `priority: p.priority`, i.e. `undefined`, which `JSON.stringify` then drops from the PUT body, so the value silently stays whatever it was.

`ent.SystemPrompt` has **no** `Edges` field, so nothing leaks there. The three timestamp/author fields are already snake_case on both sides (`SystemPromptSettings.vue:16-18`); they are converted here anyway so this endpoint speaks the same convention as the rest of the API, and the component interface is updated in the same commit.

### Files

- Modify: `server/internal/api/systemprompts/handler.go` — add `systemPromptResponse`, `toSystemPromptResponse`, `toSystemPromptResponses`; change three replies
- Modify: `server/internal/api/systemprompts/handler_test.go` — one new test
- Modify: `src/features/settings/components/SystemPromptSettings.vue` — three interface fields

### Steps

- [ ] **RED — add the failing test.** Append to `server/internal/api/systemprompts/handler_test.go`:

```go
// TestSystemPromptsHandler_WireFormat asserts the raw key set: priority 0 must
// be sent as 0 rather than dropped by the entity's omitempty tag, and the
// timestamps must use the camelCase names the rest of the API answers with.
func TestSystemPromptsHandler_WireFormat(t *testing.T) {
	mux := setupHandler(t)

	body, _ := json.Marshal(map[string]any{
		"content":  "Zero priority prompt.",
		"scope":    "global",
		"priority": 0,
	})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/settings/system-prompts", bytes.NewReader(body)))
	require.Equal(t, http.StatusCreated, w.Code)

	var created map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	for _, k := range []string{"id", "scope", "stage", "content", "priority", "createdBy", "createdAt", "updatedAt"} {
		require.Contains(t, created, k, "missing key %q in %v", k, created)
	}
	for _, k := range []string{"created_by", "created_at", "updated_at"} {
		require.NotContains(t, created, k, "unexpected key %q in %v", k, created)
	}
	require.Equal(t, float64(0), created["priority"], "priority 0 must be sent, not omitted")
	require.Nil(t, created["stage"], "an unset stage must be null, not omitted")

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/settings/system-prompts", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list, 1)
	require.Contains(t, list[0], "priority")
	require.Equal(t, float64(0), list[0]["priority"])
	require.NotContains(t, list[0], "created_at")

	put, _ := json.Marshal(map[string]any{"content": "Still zero.", "priority": 0})
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/settings/system-prompts/"+created["id"].(string), bytes.NewReader(put)))
	require.Equal(t, http.StatusOK, w.Code)
	var updated map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &updated))
	require.Equal(t, float64(0), updated["priority"])
	require.Contains(t, updated, "updatedAt")
	require.NotContains(t, updated, "updated_at")
}
```

- [ ] **Run it and confirm it fails for the right reason.** `cd server && go test -count=1 ./internal/api/systemprompts/ -run TestSystemPromptsHandler_WireFormat` — expect `missing key "priority"`, `missing key "createdAt"` and `unexpected key "created_at"`. If it fails on the POST status instead, the seed body is wrong.

- [ ] **GREEN — add the DTO.** In `server/internal/api/systemprompts/handler.go`, insert after `NewHandler`:

```go
// systemPromptResponse is the API response shape for one custom system prompt.
// ent.SystemPrompt's tags carry omitempty, which drops priority 0 — the value
// the create form submits by default — so the settings table rendered a blank
// cell and the edit form re-seeded itself from undefined.
type systemPromptResponse struct {
	ID        string    `json:"id"`
	Scope     string    `json:"scope"`
	Stage     *string   `json:"stage"`
	Content   string    `json:"content"`
	Priority  int       `json:"priority"`
	CreatedBy *string   `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func toSystemPromptResponse(p *ent.SystemPrompt) systemPromptResponse {
	return systemPromptResponse{
		ID:        p.ID,
		Scope:     p.Scope,
		Stage:     p.Stage,
		Content:   p.Content,
		Priority:  p.Priority,
		CreatedBy: p.CreatedBy,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}
}

func toSystemPromptResponses(prompts []*ent.SystemPrompt) []systemPromptResponse {
	resp := make([]systemPromptResponse, len(prompts))
	for i, p := range prompts {
		resp[i] = toSystemPromptResponse(p)
	}
	return resp
}
```

  Add `"time"` to the import block.

- [ ] **GREEN — use it on all three read paths.** In the same file:

  - `list`: drop the `if prompts == nil { prompts = []*ent.SystemPrompt{} }` guard (`toSystemPromptResponses` returns a non-nil slice, which encodes as `[]`) and reply `apierr.WriteJSON(w, http.StatusOK, toSystemPromptResponses(prompts))`.
  - `create`: `apierr.WriteJSON(w, http.StatusCreated, toSystemPromptResponse(prompt))`
  - `update`: `apierr.WriteJSON(w, http.StatusOK, toSystemPromptResponse(prompt))`

  The `ent` import stays needed by `toSystemPromptResponse`'s parameter type.

- [ ] **GREEN — update the component's interface.** In `src/features/settings/components/SystemPromptSettings.vue`, lines 16-18, replace:

```ts
  created_at: string
  updated_at: string
  created_by?: string | null
```

  with:

```ts
  createdAt: string
  updatedAt: string
  createdBy: string | null
```

  No template or script body reads any of the three, so nothing else in the file changes.

- [ ] **Run the tests and see them green.** `cd server && go test -count=1 ./internal/api/systemprompts/...`

- [ ] **Run the full gate and paste the raw output.** `cd server && go build ./... && go vet ./... && gofmt -l ./internal/api/... && go test -count=1 ./internal/api/systemprompts/...` then `pnpm lint && pnpm typecheck && pnpm test`

- [ ] **Commit.** `fix(api): send a zero system-prompt priority instead of dropping it`

---

### Task 4: the six `/api/memory/*` routes answer camelCase

**Why now, even though nothing consumes them.** The sibling plan `docs/superpowers/plans/2026-09-01-agenticos-visibility-wave.md` builds a memory panel and an injections view, and its Global Constraints already name this task as a hard dependency for its Tasks 4 and 5. If the panel were written first it would be written against `space_id`, `stage_run_id`, `char_budget` and PascalCase `Summary`, and this sweep would then break it — the component would be written twice and the intermediate state would ship broken. Converting first costs nothing because there is no client to migrate.

**Problem.** All six handlers encode entities directly (`server/internal/api/memory/handler.go:87,122,150,225,276,318`):

- `*ent.Resource` → `scope_kind`, `scope_ref`, `node_id`, `origin_ref`, `created_at`, `updated_at`
- `*ent.MemoryEntry` → `space_id`, `source_kind`, `source_ref`, `valid_from`, `valid_until`, `superseded_by`, `user_id`, `created_at`, `updated_at`
- `*ent.MemoryInjection` → `stage_run_id`, `entry_ids`, `char_budget`, `chars_used`, `candidate_count`
- `[]memory.Entry` → **PascalCase**: `ID`, `SpaceID`, `Summary`, `Content`, `Kind`, `Confidence`, `CreatedAt`, because `memory.Entry` (`server/internal/memory/retrieve.go:48-56`) carries no json tags at all.

Search results get their own DTO rather than reusing the entry DTO: `mem.Entry` is a seven-field projection, and padding it out to the full row would invent `sourceKind`, `validUntil` and `supersededBy` values the retriever never loaded.

**Two existing tests assert the old keys and must be updated in this commit:** `server/internal/api/memory/handler_test.go:272` asserts `entries[0]["Summary"]` and `:354` asserts `list[0]["stage_run_id"]`.

**Out of scope: the MCP memory tools.** `server/internal/mcp/tools/memory_test.go:221` asserts `out["space_id"]` on the `memory_write` tool. That is a separate serialization path with its own agent-facing contract; this plan touches `server/internal/api/memory/` only, and that test must stay green untouched.

### Files

- Modify: `server/internal/api/memory/handler.go` — four DTOs, four mappers, six replies
- Modify: `server/internal/api/memory/handler_test.go` — one new test, two existing assertions updated

### Steps

- [ ] **RED — add the failing test.** Append to `server/internal/api/memory/handler_test.go`:

```go
// TestMemoryRoutesWireFormat asserts the raw key set on all four payload shapes
// the memory routes answer with. Decoding into typed structs would pass before
// and after the change, so every assertion here is against map keys.
func TestMemoryRoutesWireFormat(t *testing.T) {
	mux, client, memRepo, grants, capRepo, ctx := newMux(t)
	repo.SeedCapabilities(ctx, capRepo)
	mustAllowGrant(t, grants, ctx, repo.CapabilityMemoryWrite, repo.GrantContextGlobal, "")
	mustAllowGrant(t, grants, ctx, repo.CapabilityMemoryRead, repo.GrantContextGlobal, "")

	requireKeys := func(t *testing.T, row map[string]any, want, forbidden []string) {
		t.Helper()
		for _, k := range want {
			require.Contains(t, row, k, "missing key %q in %v", k, row)
		}
		for _, k := range forbidden {
			require.NotContains(t, row, k, "unexpected key %q in %v", k, row)
		}
	}

	spaceKeys := []string{"id", "kind", "slug", "name", "scopeKind", "scopeRef", "nodeId", "state", "version", "origin", "originRef", "createdAt", "updatedAt"}
	spaceForbidden := []string{"scope_kind", "scope_ref", "node_id", "origin_ref", "created_at", "updated_at"}

	w := doJSON(t, mux, http.MethodPost, "/api/memory/spaces", map[string]any{"slug": "wire", "name": "Wire"})
	require.Equal(t, http.StatusCreated, w.Code)
	requireKeys(t, decodeMap(t, w), spaceKeys, spaceForbidden)

	w = doJSON(t, mux, http.MethodGet, "/api/memory/spaces", nil)
	require.Equal(t, http.StatusOK, w.Code)
	spaces := decodeSlice(t, w)
	require.Len(t, spaces, 1)
	requireKeys(t, spaces[0].(map[string]any), spaceKeys, spaceForbidden)

	entryKeys := []string{"id", "spaceId", "summary", "content", "kind", "sourceKind", "sourceRef", "confidence", "validFrom", "validUntil", "supersededBy", "userId", "createdAt", "updatedAt"}
	entryForbidden := []string{"space_id", "source_kind", "source_ref", "valid_from", "valid_until", "superseded_by", "user_id", "created_at", "updated_at", "ID", "SpaceID", "Summary"}

	w = doJSON(t, mux, http.MethodPost, "/api/memory/entries", map[string]any{
		"spaceSlug": "wire", "summary": "wire format note", "content": "the body",
		"kind": "fact", "sourceKind": "user",
	})
	require.Equal(t, http.StatusCreated, w.Code)
	created := decodeMap(t, w)
	requireKeys(t, created, entryKeys, entryForbidden)

	// The search projection is its own shape: memory.Entry carries no json tags
	// at all today, so the wire spells these ID/SpaceID/Summary.
	w = doJSON(t, mux, http.MethodGet, "/api/memory/entries?q=wire", nil)
	require.Equal(t, http.StatusOK, w.Code)
	hits := decodeSlice(t, w)
	require.Len(t, hits, 1)
	requireKeys(t, hits[0].(map[string]any),
		[]string{"id", "spaceId", "summary", "content", "kind", "confidence", "createdAt"},
		[]string{"ID", "SpaceID", "Summary", "Content", "Kind", "Confidence", "CreatedAt", "space_id", "created_at"})

	w = doJSON(t, mux, http.MethodPatch, "/api/memory/entries/"+created["id"].(string), map[string]any{"supersededBy": "other-entry"})
	require.Equal(t, http.StatusOK, w.Code)
	requireKeys(t, decodeMap(t, w), entryKeys, entryForbidden)

	stageRunID := mustStageRun(t, client)
	_, err := memRepo.RecordInjection(ctx, repo.RecordInjectionInput{
		StageRunID: stageRunID, EntryIDs: []string{created["id"].(string)}, CharBudget: 4000, CharsUsed: 0, CandidateCount: 0,
	})
	require.NoError(t, err)

	w = doJSON(t, mux, http.MethodGet, "/api/memory/injections?stageRun="+stageRunID, nil)
	require.Equal(t, http.StatusOK, w.Code)
	injections := decodeSlice(t, w)
	require.Len(t, injections, 1)
	injection := injections[0].(map[string]any)
	requireKeys(t, injection,
		[]string{"id", "stageRunId", "entryIds", "charBudget", "charsUsed", "candidateCount", "createdAt", "updatedAt"},
		[]string{"stage_run_id", "entry_ids", "char_budget", "chars_used", "candidate_count", "created_at", "updated_at"})
	require.Equal(t, float64(0), injection["charsUsed"], "a zero char count must be sent, not omitted")
	require.Equal(t, float64(0), injection["candidateCount"], "a zero candidate count must be sent, not omitted")
}
```

- [ ] **Run it and confirm it fails for the right reason.** `cd server && go test -count=1 ./internal/api/memory/ -run TestMemoryRoutesWireFormat` — expect `missing key "scopeKind"`, `missing key "spaceId"`, `missing key "stageRunId"`, `unexpected key "Summary"`, `unexpected key "stage_run_id"`.

- [ ] **GREEN — add the four DTOs.** In `server/internal/api/memory/handler.go`, insert after `NewHandler` (before `Mount`):

```go
// The four response shapes below exist because every payload on these routes is
// an ent entity or an untagged Go struct: ent tags the storage column names in
// snake_case with omitempty, and memory.Entry carries no json tags at all, so
// today the search route answers PascalCase Go field names.

// memorySpaceResponse is a memory space — a resource-registry row of kind
// memory_space.
type memorySpaceResponse struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	ScopeKind string    `json:"scopeKind"`
	ScopeRef  string    `json:"scopeRef"`
	NodeID    string    `json:"nodeId"`
	State     string    `json:"state"`
	Version   string    `json:"version"`
	Origin    string    `json:"origin"`
	OriginRef string    `json:"originRef"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func toMemorySpaceResponse(r *ent.Resource) memorySpaceResponse {
	return memorySpaceResponse{
		ID:        r.ID,
		Kind:      r.Kind,
		Slug:      r.Slug,
		Name:      r.Name,
		ScopeKind: r.ScopeKind,
		ScopeRef:  r.ScopeRef,
		NodeID:    r.NodeID,
		State:     r.State,
		Version:   r.Version,
		Origin:    r.Origin,
		OriginRef: r.OriginRef,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}

// memoryEntryResponse is a stored memory entry, the full row.
type memoryEntryResponse struct {
	ID           string     `json:"id"`
	SpaceID      string     `json:"spaceId"`
	Summary      string     `json:"summary"`
	Content      string     `json:"content"`
	Kind         string     `json:"kind"`
	SourceKind   string     `json:"sourceKind"`
	SourceRef    *string    `json:"sourceRef"`
	Confidence   float64    `json:"confidence"`
	ValidFrom    time.Time  `json:"validFrom"`
	ValidUntil   *time.Time `json:"validUntil"`
	SupersededBy *string    `json:"supersededBy"`
	UserID       *string    `json:"userId"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

func toMemoryEntryResponse(e *ent.MemoryEntry) memoryEntryResponse {
	return memoryEntryResponse{
		ID:           e.ID,
		SpaceID:      e.SpaceID,
		Summary:      e.Summary,
		Content:      e.Content,
		Kind:         e.Kind,
		SourceKind:   e.SourceKind,
		SourceRef:    e.SourceRef,
		Confidence:   e.Confidence,
		ValidFrom:    e.ValidFrom,
		ValidUntil:   e.ValidUntil,
		SupersededBy: e.SupersededBy,
		UserID:       e.UserID,
		CreatedAt:    e.CreatedAt,
		UpdatedAt:    e.UpdatedAt,
	}
}

// memorySearchHitResponse is a retrieval result. It is a narrower shape than
// memoryEntryResponse on purpose: mem.Entry is the projection the retriever
// resolves, and padding it out would invent values it never loaded.
type memorySearchHitResponse struct {
	ID         string    `json:"id"`
	SpaceID    string    `json:"spaceId"`
	Summary    string    `json:"summary"`
	Content    string    `json:"content"`
	Kind       string    `json:"kind"`
	Confidence float64   `json:"confidence"`
	CreatedAt  time.Time `json:"createdAt"`
}

func toMemorySearchHitResponses(entries []mem.Entry) []memorySearchHitResponse {
	resp := make([]memorySearchHitResponse, len(entries))
	for i, e := range entries {
		resp[i] = memorySearchHitResponse{
			ID:         e.ID,
			SpaceID:    e.SpaceID,
			Summary:    e.Summary,
			Content:    e.Content,
			Kind:       e.Kind,
			Confidence: e.Confidence,
			CreatedAt:  e.CreatedAt,
		}
	}
	return resp
}

// memoryInjectionResponse is the record of what was pushed into one spawn.
type memoryInjectionResponse struct {
	ID             string    `json:"id"`
	StageRunID     string    `json:"stageRunId"`
	EntryIDs       []string  `json:"entryIds"`
	CharBudget     int       `json:"charBudget"`
	CharsUsed      int       `json:"charsUsed"`
	CandidateCount int       `json:"candidateCount"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

func toMemoryInjectionResponses(injections []*ent.MemoryInjection) []memoryInjectionResponse {
	resp := make([]memoryInjectionResponse, len(injections))
	for i, in := range injections {
		resp[i] = memoryInjectionResponse{
			ID:             in.ID,
			StageRunID:     in.StageRunID,
			EntryIDs:       in.EntryIds,
			CharBudget:     in.CharBudget,
			CharsUsed:      in.CharsUsed,
			CandidateCount: in.CandidateCount,
			CreatedAt:      in.CreatedAt,
			UpdatedAt:      in.UpdatedAt,
		}
	}
	return resp
}
```

  `time`, `ent` and the `mem` alias for `server/internal/memory` are all already imported in this file. Note the ent field is spelled `EntryIds`, not `EntryIDs`.

- [ ] **GREEN — use them on all six replies.** In the same file:

  - `listSpaces` (`:80-88`): drop the `if spaces == nil { spaces = []*ent.Resource{} }` guard and reply

```go
	resp := make([]memorySpaceResponse, len(spaces))
	for i, s := range spaces {
		resp[i] = toMemorySpaceResponse(s)
	}
	apierr.WriteJSON(w, http.StatusOK, resp)
```

  - `createSpace` (`:122`): `apierr.WriteJSON(w, http.StatusCreated, toMemorySpaceResponse(space))`
  - `searchEntries` (`:150`): `apierr.WriteJSON(w, http.StatusOK, toMemorySearchHitResponses(entries))`
  - `createEntry` (`:225`): `apierr.WriteJSON(w, http.StatusCreated, toMemoryEntryResponse(entry))`
  - `supersedeEntry` (`:276`): `apierr.WriteJSON(w, http.StatusOK, toMemoryEntryResponse(updated))`
  - `listInjections` (`:311-319`): drop the `if injections == nil { … }` guard and reply `apierr.WriteJSON(w, http.StatusOK, toMemoryInjectionResponses(injections))`

  `make([]T, len(x))` is never nil, so every list route still encodes `[]` when empty — that is what the dropped guards were for. `expireEntry` writes `204` with no body and does not change.

- [ ] **GREEN — update the two existing assertions.** In `server/internal/api/memory/handler_test.go`:

  - line 272: `require.Equal(t, "bravo secret rollout plan", entries[0].(map[string]any)["Summary"])` → `…["summary"])`
  - line 354: `require.Equal(t, stageRunID, list[0].(map[string]any)["stage_run_id"])` → `…["stageRunId"])`

  Both are the same defect being asserted as correct behaviour. `TestCreateSpaceThenListSpaces` asserts `["slug"]`, which is unchanged.

- [ ] **Run the tests and see them green.** `cd server && go test -count=1 ./internal/api/memory/...`

- [ ] **Confirm the MCP path is untouched.** `cd server && go test -count=1 ./internal/mcp/tools/ -run TestMemory` must stay green without any edit — the MCP tools serialize independently and keep their own contract.

- [ ] **Run the full gate and paste the raw output.** `cd server && go build ./... && go vet ./... && gofmt -l ./internal/api/... && go test -count=1 ./internal/api/memory/...`

- [ ] **Commit.** `fix(api): reply with camelCase memory spaces, entries and injections`

---

### Task 5: the task-returning routes answer a task DTO

**Problem.** Five routes reply with a raw `*ent.Task`: `PATCH /api/tasks/{id}` (`handler.go:656`), `POST /api/tasks/{id}/cancel` (`:751`), `POST /api/tasks/{id}/hold` (`:814`), `POST /api/plan/{taskId}/approve` (`plan/handler.go:54`) and `POST /api/refine/{taskId}/confirm` (`refine/handler.go:251`). `ent.Task` is tagged `worktree_path`, `source_branch`, `current_stage`, `max_iterations`, `silver_bullet`, `plan_mode`, `created_at`, … with `omitempty` on all of them and an `edges` container. `usePlanReview.ts:93` types the approve response `as PipelineTask`, which it has never been.

`PATCH` is inconsistent with itself today: the `cwd_not_in_project` warning branch (`handler.go:650`) already answers an `EnrichedTask`, while the normal branch answers the raw entity.

**Design: `EnrichedTask` gains an embedded base.** `EnrichedTask` (`server/internal/api/tasks/enrich.go:19-44`) already declares exactly the 25 plain task columns in camelCase, followed by its computed fields. Rather than writing a second copy of those 25 declarations — which the project's SSOT rule forbids — the plain half is extracted into an exported `TaskResponse` that `EnrichedTask` embeds anonymously. Go flattens an embedded struct's fields into the parent JSON object, so **`EnrichedTask`'s wire format is byte-for-byte unchanged**, and `enriched.ID` and friends keep working through field promotion. Only composite literals need updating, and there are exactly two in the tree (`enrich.go:271` and `refine_status_test.go:56`).

`plan` and `refine` then call `tasks.ToTaskResponse`. Neither package can reach `EnrichTask` — it needs `permRepo` and `srBulkRepo`, which neither `plan.HandlerDeps` nor `refine.Deps` carries — and neither has any computed field to report. There is no import cycle: `api/tasks` imports no `api/*` sibling, and `server/internal/mcp/tools/control.go:6` already imports `api/tasks` as `tasksapi` for the same kind of reuse.

### Files

- Modify: `server/internal/api/tasks/enrich.go` — extract `TaskResponse`, add `ToTaskResponse`, embed it in `EnrichedTask`, rewrite the literal
- Modify: `server/internal/api/tasks/handler.go` — three replies
- Modify: `server/internal/api/tasks/refine_status_test.go` — one literal
- Modify: `server/internal/api/plan/handler.go` — one reply
- Modify: `server/internal/api/refine/handler.go` — one reply
- Modify: `server/internal/api/tasks/wire_format_test.go` — one new test
- Modify: `server/internal/api/refine/handler_test.go` — one new test
- Create: `server/internal/api/plan/handler_test.go` — the package has no HTTP-level test today

### Steps

- [ ] **RED — add the tasks-package test.** Append to `server/internal/api/tasks/wire_format_test.go`:

```go
// TestTaskActions_WireFormat asserts that the three task-returning action routes
// answer camelCase and send false booleans rather than dropping them.
func TestTaskActions_WireFormat(t *testing.T) {
	client, r := newTestHandlerWithRepos(t)
	taskRepo := repo.NewTaskRepo(client)

	want := []string{"id", "slug", "title", "description", "cwd", "worktreePath", "sourceBranch", "targetBranch", "currentStage", "priority", "autonomy", "userId", "parentTaskId", "projectId", "spawnerId", "maxIterations", "tokenBudget", "costBudgetCents", "stageTimeoutSeconds", "silverBullet", "planMode", "rank", "metadata", "createdAt", "updatedAt"}
	forbidden := []string{"worktree_path", "source_branch", "target_branch", "current_stage", "parent_task_id", "project_id", "spawner_id", "max_iterations", "token_budget", "cost_budget_cents", "stage_timeout_seconds", "silver_bullet", "plan_mode", "user_id", "created_at", "updated_at", "edges"}

	newTask := func(t *testing.T, slug string) string {
		t.Helper()
		task, err := taskRepo.Create(testCtx(t), repo.CreateTaskInput{
			Slug:          slug,
			Title:         "Action Wire Format",
			Cwd:           t.TempDir(),
			MaxIterations: 5,
			Priority:      "normal",
			CurrentStage:  "implementation",
		})
		if err != nil {
			t.Fatalf("create task: %v", err)
		}
		return task.ID
	}

	decodeOne := func(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
		t.Helper()
		var row map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &row); err != nil {
			t.Fatalf("unmarshal response: %v (body: %s)", err, w.Body.String())
		}
		return row
	}

	t.Run("patch", func(t *testing.T) {
		id := newTask(t, "action-wire-patch")
		req := withAuth(t, httptest.NewRequest(http.MethodPatch, "/api/tasks/"+id, strings.NewReader(`{"title":"Renamed"}`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		row := decodeOne(t, w)
		assertKeys(t, row, want, forbidden)
		if row["silverBullet"] != false {
			t.Errorf("silverBullet = %v, want false (not omitted)", row["silverBullet"])
		}
		if row["title"] != "Renamed" {
			t.Errorf("title = %v, want %q", row["title"], "Renamed")
		}
	})

	t.Run("cancel", func(t *testing.T) {
		id := newTask(t, "action-wire-cancel")
		req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/tasks/"+id+"/cancel", nil))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		row := decodeOne(t, w)
		assertKeys(t, row, want, forbidden)
		if row["currentStage"] != "cancelled" {
			t.Errorf("currentStage = %v, want %q", row["currentStage"], "cancelled")
		}
	})

	t.Run("hold", func(t *testing.T) {
		id := newTask(t, "action-wire-hold")
		req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/tasks/"+id+"/hold", nil))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		row := decodeOne(t, w)
		assertKeys(t, row, want, forbidden)
		if row["currentStage"] != "on_hold" {
			t.Errorf("currentStage = %v, want %q", row["currentStage"], "on_hold")
		}
	})
}
```

- [ ] **Run it and confirm it fails for the right reason.** `cd server && go test -count=1 ./internal/api/tasks/ -run TestTaskActions_WireFormat` — expect `missing key "worktreePath"`, `missing key "currentStage"`, `missing key "silverBullet"`, `unexpected key "current_stage"`, `unexpected key "edges"` in all three subtests.

- [ ] **GREEN — extract `TaskResponse`.** In `server/internal/api/tasks/enrich.go`, replace the plain-column half of the `EnrichedTask` declaration (lines 19-44, from `type EnrichedTask struct {` down to and including the `UpdatedAt` line) with:

```go
// TaskResponse is the API response shape for a task's own stored columns. It is
// the base of EnrichedTask — embedded, so the enriched payload's wire format is
// unchanged — and the whole answer for routes outside this package that return a
// task and have no repos to compute anything from (plan approve, refine confirm).
//
// The ent entity's own tags are the storage column names and carry omitempty,
// which drops silverBullet: false and planMode: false from the payload instead
// of sending them, and leaks the empty edges container.
type TaskResponse struct {
	ID                  string                 `json:"id"`
	Slug                string                 `json:"slug"`
	Title               string                 `json:"title"`
	Description         *string                `json:"description"`
	Cwd                 string                 `json:"cwd"`
	WorktreePath        *string                `json:"worktreePath"`
	SourceBranch        *string                `json:"sourceBranch"`
	TargetBranch        *string                `json:"targetBranch"`
	CurrentStage        string                 `json:"currentStage"`
	Priority            string                 `json:"priority"`
	Autonomy            string                 `json:"autonomy"`
	UserID              *string                `json:"userId"`
	ParentTaskID        *string                `json:"parentTaskId"`
	ProjectID           *string                `json:"projectId"`
	SpawnerID           *string                `json:"spawnerId"`
	MaxIterations       int                    `json:"maxIterations"`
	TokenBudget         *int                   `json:"tokenBudget"`
	CostBudgetCents     *int                   `json:"costBudgetCents"`
	StageTimeoutSeconds int                    `json:"stageTimeoutSeconds"`
	SilverBullet        bool                   `json:"silverBullet"`
	PlanMode            bool                   `json:"planMode"`
	Rank                *float64               `json:"rank"`
	Metadata            map[string]interface{} `json:"metadata"`
	CreatedAt           time.Time              `json:"createdAt"`
	UpdatedAt           time.Time              `json:"updatedAt"`
}

// ToTaskResponse maps a stored task onto the wire shape src/types.ts declares as
// PipelineTask's non-computed half.
func ToTaskResponse(t *ent.Task) TaskResponse {
	return TaskResponse{
		ID:                  t.ID,
		Slug:                t.Slug,
		Title:               t.Title,
		Description:         t.Description,
		Cwd:                 t.Cwd,
		WorktreePath:        t.WorktreePath,
		SourceBranch:        t.SourceBranch,
		TargetBranch:        t.TargetBranch,
		CurrentStage:        t.CurrentStage,
		Priority:            t.Priority,
		Autonomy:            t.Autonomy,
		UserID:              t.UserID,
		ParentTaskID:        t.ParentTaskID,
		ProjectID:           t.ProjectID,
		SpawnerID:           t.SpawnerID,
		MaxIterations:       t.MaxIterations,
		TokenBudget:         t.TokenBudget,
		CostBudgetCents:     t.CostBudgetCents,
		StageTimeoutSeconds: t.StageTimeoutSeconds,
		SilverBullet:        t.SilverBullet,
		PlanMode:            t.PlanMode,
		Rank:                t.Rank,
		Metadata:            t.Metadata,
		CreatedAt:           t.CreatedAt,
		UpdatedAt:           t.UpdatedAt,
	}
}

// EnrichedTask is a task plus the fields computed at read time. The embedded
// TaskResponse is anonymous, so encoding/json flattens its fields into the same
// object — the payload is identical to the flat struct this replaced.
type EnrichedTask struct {
	TaskResponse
```

  Everything from `// Computed fields — not stored in DB.` down to the closing brace of `EnrichedTask` stays exactly as it is.

- [ ] **GREEN — rewrite the one construction site.** In the same file, at line 271, replace the 25 plain-column assignments in the `e := &EnrichedTask{…}` literal with a single embedded-field assignment, keeping every computed assignment:

```go
	e := &EnrichedTask{
		TaskResponse:                ToTaskResponse(t),
		NeedsUser:                   needsUser,
		LatestStageRunStatus:        latestStatus,
		AutoRetryCount:              autoRetryCount,
		NextRetryAt:                 nextRetryAt,
		CurrentIteration:            currentIteration,
		ActiveSessionID:             activeSessionID,
		ActivePID:                   activePID,
		BlockedByPendingPermissions: blockedByPendingPermissions,
		IsBlocked:                   isBlocked,
		IsUnsatisfiable:             isUnsatisfiable,
		pendingPermsCount:           pendingPermsCount,
	}
```

- [ ] **GREEN — fix the one test literal.** In `server/internal/api/tasks/refine_status_test.go:56`, `CurrentStage` is now a promoted field and cannot be set in the outer literal. Replace:

```go
	e := &EnrichedTask{
		CurrentStage:         "implementation",
		LatestStageRunStatus: &awaiting,
		NeedsUser:            true,
		pendingPermsCount:    3,
	}
```

  with:

```go
	e := &EnrichedTask{
		TaskResponse:         TaskResponse{CurrentStage: "implementation"},
		LatestStageRunStatus: &awaiting,
		NeedsUser:            true,
		pendingPermsCount:    3,
	}
```

  The three `&EnrichedTask{}` empty literals at `:15`, `:27` and `:39` compile unchanged.

- [ ] **GREEN — use it in the three tasks handlers.** In `server/internal/api/tasks/handler.go`:

  - `update` (line 656): `return jsonReply(w, http.StatusOK, ToTaskResponse(updated))`
  - `cancel` (line 751): `return jsonReply(w, http.StatusOK, ToTaskResponse(updated))`
  - `hold` (line 814): `return jsonReply(w, http.StatusOK, ToTaskResponse(updated))`

  The `cwd_not_in_project` warning branch at line 650 keeps answering the enriched task — it is a different, richer payload by design and its `map[string]any` wrapper is already camelCase.

- [ ] **Run the tasks tests and see them green.** `cd server && go test -count=1 ./internal/api/tasks/...` — the whole package, because the embedding change touches every enrichment test.

- [ ] **RED — add the plan HTTP test.** The `plan` package has no handler test today (`service_test.go` only). Create `server/internal/api/plan/handler_test.go`:

```go
package plan_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/api/plan"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// TestApproveRoute_WireFormat asserts the approve route answers the camelCase
// task shape usePlanReview.ts already types the body as, not the raw entity.
func TestApproveRoute_WireFormat(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })

	ctx := context.Background()
	taskRepo := repo.NewTaskRepo(bundle.Client)
	srRepo := repo.NewStageRunRepo(bundle.Client)
	turnsRepo := repo.NewRefinementTurnRepo(bundle.Client)
	taskID, _ := seedPlanReviewTask(t, ctx, taskRepo, srRepo)

	h := plan.NewHandler(plan.HandlerDeps{
		Turns:     turnsRepo,
		Tasks:     taskRepo,
		StageRuns: srRepo,
		Advance:   func(context.Context, string) error { return nil },
	})
	r := chi.NewRouter()
	h.Mount(r)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/plan/"+taskID+"/approve", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var row map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &row))
	for _, k := range []string{"id", "slug", "title", "currentStage", "maxIterations", "silverBullet", "planMode", "createdAt", "updatedAt"} {
		require.Contains(t, row, k, "missing key %q in %v", k, row)
	}
	for _, k := range []string{"current_stage", "max_iterations", "silver_bullet", "plan_mode", "created_at", "updated_at", "edges"} {
		require.NotContains(t, row, k, "unexpected key %q in %v", k, row)
	}
	require.Equal(t, "implementation", row["currentStage"])
}
```

  `seedPlanReviewTask` already exists in `service_test.go` in the same `plan_test` package — do not redefine it.

- [ ] **Run it and confirm it fails for the right reason.** `cd server && go test -count=1 ./internal/api/plan/ -run TestApproveRoute_WireFormat` — expect `missing key "currentStage"` and `unexpected key "current_stage"`.

- [ ] **GREEN — use the DTO in plan.** In `server/internal/api/plan/handler.go`, in `approve`, replace `_ = json.NewEncoder(w).Encode(task)` with `_ = json.NewEncoder(w).Encode(tasks.ToTaskResponse(task))` and add the import `"github.com/lx-wnk/agent-dashboard/server/internal/api/tasks"`. Leave `reject` and `status` alone — `reject` answers `{"status":"ok"}` and `status` answers `PlanStatusResult`, whose snake_case keys the client reads on purpose.

- [ ] **RED — add the refine test.** Append to `server/internal/api/refine/handler_test.go`:

```go
// TestConfirm_WireFormat asserts the confirm route answers the camelCase task
// shape rather than the raw ent entity.
func TestConfirm_WireFormat(t *testing.T) {
	tasks := newFakeTaskRepo(defaultTask(t, "task-1"))
	r := makeRouter(&fakeTurnRepo{}, tasks, noopSpawner)
	req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/refine/task-1/confirm", nil))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var row map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &row); err != nil {
		t.Fatalf("unmarshal response: %v (body: %s)", err, rr.Body.String())
	}
	for _, k := range []string{"id", "title", "currentStage", "maxIterations", "silverBullet", "createdAt"} {
		if _, ok := row[k]; !ok {
			t.Errorf("missing key %q in %v", k, row)
		}
	}
	for _, k := range []string{"current_stage", "max_iterations", "silver_bullet", "created_at", "edges"} {
		if _, ok := row[k]; ok {
			t.Errorf("unexpected key %q in %v", k, row)
		}
	}
}
```

- [ ] **Run it and confirm it fails for the right reason.** `cd server && go test -count=1 ./internal/api/refine/ -run TestConfirm_WireFormat` — expect `missing key "currentStage"` and `unexpected key "edges"`.

- [ ] **GREEN — use the DTO in refine.** In `server/internal/api/refine/handler.go`, in `confirm` (line 251), replace `_ = json.NewEncoder(w).Encode(task)` with `_ = json.NewEncoder(w).Encode(tasks.ToTaskResponse(task))` and add the import `"github.com/lx-wnk/agent-dashboard/server/internal/api/tasks"`. The local variable `tasks` in `TestConfirm_WireFormat` shadows nothing in the handler file — but if the handler already has a local named `tasks`, alias the import as `tasksapi`, the way `mcp/tools/control.go` does.

- [ ] **Run the full gate and paste the raw output.** `cd server && go build ./... && go vet ./... && gofmt -l ./internal/api/... && go test -count=1 ./internal/api/tasks/... ./internal/api/plan/... ./internal/api/refine/...`

  Module-wide `go vet` matters here: `EnrichedTask` is exported and `server/internal/mcp/tools/control.go` imports this package.

- [ ] **Check the ent tree is clean.** `git status --short server/internal/db/ent/` must print nothing.

- [ ] **Commit.** `fix(api): reply with a camelCase task DTO from every task-returning route`

---

### Task 6: the action routes returning stage runs and permission requests

**Problem.** Seven more replies encode entities directly, all reusing DTOs that exist by the time this task runs:

| route | line | today | becomes |
|---|---|---|---|
| `POST /api/tasks/{id}/progress` | `handler.go:732` | `map[string]any{"task": *ent.Task, "stageRun": *ent.StageRun}` | `ToTaskResponse` + `toStageRunResponse` |
| `POST /api/tasks/{id}/retry` | `handler.go:777` | `*ent.StageRun` | `toStageRunResponse` |
| `POST /api/tasks/{id}/resume` | `handler.go:843` | `*ent.StageRun` | `toStageRunResponse` |
| `POST /api/tasks/{id}/resume-stage` | `resume_routes.go:31` | `*ent.StageRun` | `toStageRunResponse` |
| `POST /api/permission-requests` | `permission_request_routes.go:132,150` | `*ent.PermissionRequest` | `toPermissionRequestResponse` |
| `POST /api/tasks/{id}/permission-requests/{reqID}/resolve` | `handler.go:1050` | `*ent.PermissionRequest` | `toPermissionRequestResponse` |
| `POST /api/tasks/{id}/permissions/bulk` | `permission_request_routes.go:279` | `[]*ent.TaskPermission` | `toTaskPermissionResponse` |

`toPermissionRequestResponse` (`permission_request_routes.go:37`) and `toTaskPermissionResponse` (`handler.go:939`, from #412) already exist and already serve the list routes that feed the same TypeScript types — this task only stops the write routes from answering differently from the read routes beside them. #412 named the bulk-grant route explicitly as the same defect left unfixed.

`progress` reads its task with `task, _ := h.taskRepo.GetByID(…)`, discarding the error, so `task` can be nil. `ToTaskResponse(nil)` would panic — the nil case must stay `null` on the wire, as it is today.

### Files

- Modify: `server/internal/api/tasks/handler.go` — three replies
- Modify: `server/internal/api/tasks/resume_routes.go` — one reply
- Modify: `server/internal/api/tasks/permission_request_routes.go` — three replies
- Modify: `server/internal/api/tasks/wire_format_test.go` — one new test

### Steps

- [ ] **Read the bulk-grant request body first.** `bulkGrantPermissions` starts at `permission_request_routes.go:257`; its anonymous body struct is the one field list in this plan that was not transcribed line-by-line at plan time. Read it, then adjust the JSON literal in the subtest below to match before running anything.

- [ ] **RED — add the failing test.** Append to `server/internal/api/tasks/wire_format_test.go`:

```go
// TestPermissionWriteRoutes_WireFormat asserts the write routes answer the same
// shape as the list routes beside them, which feed the same TypeScript types.
func TestPermissionWriteRoutes_WireFormat(t *testing.T) {
	client, r := newTestHandlerWithRepos(t)
	task, err := repo.NewTaskRepo(client).Create(testCtx(t), repo.CreateTaskInput{
		Slug:          "perm-write-wire-format",
		Title:         "Permission Write Wire Format",
		Cwd:           t.TempDir(),
		MaxIterations: 5,
		Priority:      "normal",
		CurrentStage:  "implementation",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	run, err := repo.NewStageRunRepo(client).Create(testCtx(t), repo.CreateStageRunInput{
		TaskID:    task.ID,
		Stage:     "implementation",
		Iteration: 1,
	})
	if err != nil {
		t.Fatalf("create stage run: %v", err)
	}

	t.Run("createPermissionRequest", func(t *testing.T) {
		body := `{"stageRunId":"` + run.ID + `","tool":"Bash","pattern":"ls -la"}`
		req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/permission-requests", strings.NewReader(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var row map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &row); err != nil {
			t.Fatalf("unmarshal response: %v (body: %s)", err, w.Body.String())
		}
		assertKeys(t, row,
			[]string{"id", "stageRunId", "tool", "pattern", "reason", "outcome", "requestedAt", "resolvedAt", "outsideSafeList"},
			[]string{"stage_run_id", "requested_at", "resolved_at", "edges"},
		)
	})

	t.Run("bulkGrantPermissions", func(t *testing.T) {
		body := `{"permissions":[{"tool":"Write"},{"tool":"Read"}]}`
		req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID+"/permissions/bulk", strings.NewReader(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		rows := decodeRows(t, w.Body.Bytes())
		if len(rows) == 0 {
			t.Fatalf("expected granted permissions, got none: %s", w.Body.String())
		}
		for _, row := range rows {
			assertKeys(t, row,
				[]string{"id", "taskId", "tool", "pattern", "granted", "decidedBy", "requestedAt", "decidedAt", "expiresAt"},
				[]string{"task_id", "requested_at", "decided_at", "decided_by", "expires_at", "manual_override", "edges"},
			)
		}
	})
}
```

- [ ] **Run it and confirm it fails for the right reason.** `cd server && go test -count=1 ./internal/api/tasks/ -run TestPermissionWriteRoutes_WireFormat` — expect `missing key "stageRunId"`, `missing key "outsideSafeList"`, `missing key "taskId"`, `unexpected key "stage_run_id"`, `unexpected key "edges"`.

- [ ] **GREEN — map the three stage-run replies.** Pass `toStageRunResponse(sr)` in place of `sr`:

  - `server/internal/api/tasks/handler.go:777` (`retry`) — `return jsonReply(w, http.StatusAccepted, toStageRunResponse(sr))`
  - `server/internal/api/tasks/handler.go:843` (`resume`) — same
  - `server/internal/api/tasks/resume_routes.go:31` (`resumeStage`) — same

- [ ] **GREEN — map the progress reply, keeping the nil task null.** In `handler.go`, replace the last line of `progress` (line 732):

```go
	// GetByID's error is deliberately discarded above, so task can be nil; the
	// payload must then carry null rather than panic in the mapper.
	var taskView any
	if task != nil {
		taskView = ToTaskResponse(task)
	}
	return jsonReply(w, http.StatusOK, map[string]any{"task": taskView, "stageRun": toStageRunResponse(sr)})
```

- [ ] **GREEN — map the permission-request replies.** Replace `req` with `toPermissionRequestResponse(req)` at `permission_request_routes.go:132` and `:150`, and `resolved` with `toPermissionRequestResponse(resolved)` at `handler.go:1050`.

- [ ] **GREEN — map the bulk grant reply.** At `permission_request_routes.go:279`, replace `return jsonReply(w, http.StatusOK, granted)` with:

```go
	resp := make([]taskPermissionResponse, len(granted))
	for i, p := range granted {
		resp[i] = toTaskPermissionResponse(p)
	}
	return jsonReply(w, http.StatusOK, resp)
```

  The `if granted == nil { granted = []*ent.TaskPermission{} }` guard above becomes redundant (`make` is never nil) but is harmless — leave it rather than risk an unused-import cascade.

- [ ] **Run the test and see it green.** `cd server && go test -count=1 ./internal/api/tasks/...`

- [ ] **Run the full gate and paste the raw output.** `cd server && go build ./... && go vet ./... && gofmt -l ./internal/api/... && go test -count=1 ./internal/api/tasks/...`

- [ ] **Commit.** `fix(api): map stage runs and permission requests on the action routes`

---

### Task 7: the JSON export, and the changelog for the sweep

**Problem.** `exportTasks` (`server/internal/api/tasks/export_routes.go:58-70`) builds a local `taskWithRuns` struct that **embeds `*ent.Task`** and adds `StageRuns []*ent.StageRun` tagged `json:"stageRuns"`. Embedding the entity flattens all of its snake_case tags into the row and carries the `edges` object along, and the nested stage runs are raw entities too. So `tasks.json` — a file a user downloads and keeps — is the only artifact in this sweep whose wrong shape outlives the session that produced it. The CSV branch is hand-written with a camelCase header (`:35`) and is already correct.

`GET /api/tasks/export?format=json` is reached from `PipelineBoard.vue:96` via `triggerDownload`, so the browser never parses the body — but the file does not stop being a contract for being read by a human.

### Files

- Modify: `server/internal/api/tasks/export_routes.go` — replace the local row struct
- Modify: `server/internal/api/tasks/wire_format_test.go` — one new test
- Modify: `CHANGELOG.md` — one entry under `## [Unreleased]` → `### Fixed`

### Steps

- [ ] **RED — add the failing test.** Append to `server/internal/api/tasks/wire_format_test.go`:

```go
// TestExportTasksJSON_WireFormat asserts the downloaded JSON export carries the
// same camelCase field names as the API, including inside the nested stage runs.
func TestExportTasksJSON_WireFormat(t *testing.T) {
	client, r := newTestHandlerWithRepos(t)
	task, err := repo.NewTaskRepo(client).Create(testCtx(t), repo.CreateTaskInput{
		Slug:          "export-wire-format",
		Title:         "Export Wire Format",
		Cwd:           t.TempDir(),
		MaxIterations: 5,
		Priority:      "normal",
		CurrentStage:  "implementation",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err = repo.NewStageRunRepo(client).Create(testCtx(t), repo.CreateStageRunInput{
		TaskID:    task.ID,
		Stage:     "implementation",
		Iteration: 1,
	}); err != nil {
		t.Fatalf("create stage run: %v", err)
	}

	req := withAuth(t, httptest.NewRequest(http.MethodGet, "/api/tasks/export?format=json", nil))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	rows := decodeRows(t, w.Body.Bytes())
	if len(rows) != 1 {
		t.Fatalf("expected 1 exported task, got %d: %s", len(rows), w.Body.String())
	}
	assertKeys(t, rows[0],
		[]string{"id", "slug", "title", "currentStage", "maxIterations", "silverBullet", "createdAt", "stageRuns"},
		[]string{"current_stage", "max_iterations", "silver_bullet", "created_at", "edges"},
	)

	runs, ok := rows[0]["stageRuns"].([]any)
	if !ok || len(runs) != 1 {
		t.Fatalf("stageRuns = %v, want one nested run", rows[0]["stageRuns"])
	}
	assertKeys(t, runs[0].(map[string]any),
		[]string{"id", "taskId", "stage", "status", "iteration", "tokensUsed", "costCents", "startedAt", "endedAt"},
		[]string{"task_id", "tokens_used", "cost_cents", "started_at", "ended_at", "edges"},
	)
}
```

  `exportTasks` scopes its listing with `h.taskRepo.ListForUser(ctx, payload.Sub, h.bypassAuth)`. If the row count comes back 0, the seeded task has no `user_id` matching the test JWT's subject — set `UserID` on `CreateTaskInput` to the same subject `withAuth` signs, rather than weakening the assertion.

- [ ] **Run it and confirm it fails for the right reason.** `cd server && go test -count=1 ./internal/api/tasks/ -run TestExportTasksJSON_WireFormat` — expect `missing key "currentStage"`, `unexpected key "current_stage"`, `unexpected key "edges"` on the row, and the same on the nested run.

- [ ] **GREEN — swap the export row.** In `server/internal/api/tasks/export_routes.go`, replace the JSON branch (lines 58-70) with:

```go
	// JSON export with stage runs embedded. TaskResponse is embedded anonymously
	// so its fields flatten into the row — the same shape the API answers with,
	// rather than the entity's storage column names, because this file outlives
	// the session that downloaded it.
	type taskExportRow struct {
		TaskResponse
		StageRuns []stageRunResponse `json:"stageRuns"`
	}
	result := make([]taskExportRow, 0, len(tasks))
	for _, t := range tasks {
		result = append(result, taskExportRow{
			TaskResponse: ToTaskResponse(t),
			StageRuns:    toStageRunResponses(runsByTask[t.ID]),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="tasks.json"`)
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(result)
```

  The `ent` import stays needed by `listFeedback` in the same file (`ent.IsNotFound`).

- [ ] **Run the test and see it green.** `cd server && go test -count=1 ./internal/api/tasks/ -run TestExportTasksJSON_WireFormat`

- [ ] **Document the sweep.** In `CHANGELOG.md`, under `## [Unreleased]` → `### Fixed`, add one entry immediately **above** the existing "The task permission and audit endpoints answer in the shape the client declares" bullet, so the two read in sequence:

```markdown
- Every remaining endpoint that answered with a raw ent entity now answers a DTO in the shape the client declares. #412 fixed the task permission and audit routes; this is the rest of that class. `GET /api/tasks/{id}/stage-runs` sent `session_name`, `started_at` and `ended_at`, so the Stages tab printed *Invalid Date* for both ends of every run and the task modal's active-runtime figure read `—` for every task that has ever run. `GET /api/tasks/{id}/dependencies` and `/dependents` needed more than a rename: `taskTitle`, `dependsOnTitle` and `dependsOnStage` are not columns on `task_dependency` at all, so the dependency list rendered nameless rows under an empty stage badge — they are now resolved from the referenced tasks in one extra query on the display route, deliberately not by eager-loading in the repo, which would have added joins to the orchestrator's dependency sweep for data it never reads. `GET/POST/PUT /api/settings/system-prompts` dropped `priority: 0` — the value the create form submits by default — because of the entity's `omitempty`, so the settings table showed a blank cell and the edit form re-seeded itself from `undefined`. The six `/api/memory/*` routes were converted too, ahead of the memory panel that will consume them: `GET /api/memory/entries` was answering PascalCase Go field names (`ID`, `SpaceID`, `Summary`), because the retriever's own result struct carries no json tags. The task-returning routes (`PATCH /api/tasks/{id}`, cancel, hold, `POST /api/plan/{taskId}/approve`, `POST /api/refine/{taskId}/confirm`) now share one exported `TaskResponse`, which `EnrichedTask` embeds rather than redeclaring — the enriched payload is unchanged byte-for-byte. The downloaded `tasks.json` export was the one artifact whose wrong shape outlived the session that produced it, and it now carries the same names as the API, nested stage runs included. Three endpoints were deliberately left alone: `GET/POST /api/grants` and `GET /api/capabilities` are snake_case on purpose and `useGrants.ts` says so at its interface definitions, and `GET /api/plan/{taskId}/status` ships `gate_state`/`approved_plan`, which `usePlanReview.ts` reads under exactly those names.
```

- [ ] **Run the full gate and paste the raw output.** `cd server && go build ./... && go vet ./... && gofmt -l ./internal/api/... && go test -count=1 ./internal/api/tasks/...` then `pnpm lint && pnpm typecheck && pnpm test`

- [ ] **Check the ent tree is clean.** `git status --short server/internal/db/ent/` must print nothing.

- [ ] **Commit.** `fix(api): export tasks in the API's own field names`

---

## FOLLOW-UPS — found while scanning, deliberately not fixed here

Each of these is outside "convert an entity to a DTO" and none blocks any task above. Raise them separately.

1. **`DependencyGraph.vue` has never rendered.** `src/components/DependencyGraph.vue:53-57` fetches `GET /api/tasks/{id}/dependencies` and decodes it as `{ dependencies: TaskDependency[], dependents: TaskDependency[] }` — an object. The route has always answered a bare **array** (`dependency_routes.go:23`). So `data.dependencies` is `undefined`, `renderGraph(undefined, undefined)` throws on `for (const dep of deps)`, the `catch` fires, and the user gets a *Failed to load graph* toast every time the Dependencies tab opens. Task 2 fixes the field names on that route and does **not** fix this: here the server is right and the client is wrong. The fix is client-side — call `fetchDependencies(id)` and `fetchDependents(id)` from `useTasks.ts` (both already exist and are already used by `useTaskDependencies`) and pass the two arrays to `renderGraph`. `VERIFIED` by reading both sides; the cheapest confirmation is opening the tab and watching for the toast.
2. **`AdvanceResult.Result` carries a raw `*ent.StageRun`.** `server/internal/api/tasks/advance.go:29` types `Result any` and `:83`, `:103`, `:110` assign the entity into it, so `POST /api/tasks/{id}/advance` nests a snake_case run with an `edges` object. Left out of Task 6 on purpose: `Advance` is also called by the MCP `advance_task` tool (`server/internal/mcp/tools/control.go:68-83`, `mcp.OK(res)`), so changing it changes an agent-facing contract this plan never scoped. `toStageRunResponse` from Task 1 is the whole fix, once someone decides the MCP shape may change.
3. **`memory.Entry` still has no json tags.** Task 4 maps around it in the API handler. If a second consumer of `memory.Entry` ever encodes it, the PascalCase leak comes back. Tagging the struct itself is the durable fix, but it lives outside `api/` and its MCP callers would have to be checked first.
4. **The MCP memory tools keep snake_case.** `server/internal/mcp/tools/memory_test.go:221` asserts `out["space_id"]`. After Task 4 the HTTP and MCP surfaces disagree about the same rows. That may well be correct — they are different contracts with different consumers — but it should be a decision on the record rather than an accident of which one got swept.
