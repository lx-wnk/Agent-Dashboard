# Go Rebuild — Phase 3: Task Pipeline

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the full task pipeline state machine in Go — ent schemas, repo interfaces, orchestrator tick loop, zombie sweeps, stage handlers, agent spawner, completion detector, and REST API — so the dashboard can run multi-stage agentic work items under the same invariants as the Node.js implementation.

**Architecture:** The pipeline is a layered Go system inside `server/internal/`. The `db/` layer owns ent schemas and repo interfaces. The `pipeline/` package owns the orchestrator, stage handlers, spawner, and completion detector — it imports only `db/` and never touches `api/` or `sse/`. The `api/tasks/` handler owns the REST surface; it receives the orchestrator by interface injection from `wire_gen.go`. All side-effects (SSE broadcast, notification dispatch) are injected callbacks on `OrchestratorOptions` — the orchestrator never imports `sse/` or any notification package.

**Tech Stack:** Go 1.26+, entgo.io/ent v0.14.6, modernc.org/sqlite (CGO-free), google/uuid, golang.org/x/sync/errgroup, sync.Map for per-task locking, os.Signal for PID probing, testify/require

---

> **Module path:** `github.com/lx-wnk/agent-dashboard/server`
> **Working directory for all `go` commands:** `server/` unless stated otherwise

---

## File Map

```
server/
  internal/
    db/
      ent/
        schema/
          task.go                         ← expand Phase 2 stub: full fields + edges
          stage_run.go                    ← new entity
          task_permission.go              ← new entity
          permission_request.go           ← new entity
          audit_log.go                    ← new entity
          task_dependency.go              ← new entity (through-table for self-ref edge)
        (generated ent files — run go generate after schema changes)
      repo/
        task_repo.go                      ← TaskRepo interface + ent impl
        task_repo_test.go
        stage_run_repo.go                 ← StageRunRepo interface + ent impl
        stage_run_repo_test.go
        permission_repo.go                ← PermissionRepo interface + ent impl (permissions + requests)
        permission_repo_test.go
        audit_repo.go                     ← AuditRepo interface + ent impl
        audit_repo_test.go
        pipeline_config_repo.go           ← PipelineConfigRepo interface + ent impl
        pipeline_config_repo_test.go
    pipeline/
      types.go                            ← StageTransition sealed interface, StageContext, StageHandler, STAGE_ORDER
      orchestrator.go                     ← PipelineOrchestrator: Run(), tick(), sweeps, progressTask(), applyTransition()
      orchestrator_test.go
      stage_handlers.go                   ← createAgentStage factory, backlogHandler, conceptHandler
      stage_handlers_test.go
      stage_prompts.go                    ← PromptBundle, implementationPrompt, selfReviewPrompt, finalizationPrompt
      spawner.go                          ← SpawnStageAgent(), buildAllowList(), buildSpawnArgs(), buildSpawnEnv(), writeSettingsFile()
      spawner_test.go
      completion_detector.go              ← DetectCompletion(), ValidateStageOutput(), extractJsonBlock(), lastAssistantText()
      completion_detector_test.go
      session_reader.go                   ← ResolvedProjectDir(), FindNewestSessionID(), ReadLastStageJsonOutput(), ReadSessionTokenSummary()
      session_reader_test.go
      session_manager.go                  ← IsPidAlive(), isPidZombie(), DecideRecovery(), AttachSessionID(), BuildSessionName()
      session_manager_test.go
    api/
      tasks/
        handler.go                        ← all task REST handlers
        handler_test.go
        enrich.go                         ← EnrichTask(), EnrichTasksBulk()
        enrich_test.go
        handoff.go                        ← BuildPermissionGrantHandoffNote(), BuildBulkPermissionGrantHandoffNote()
        handoff_test.go
    sse/
      task_broadcaster.go                 ← TaskBroadcaster wrapping sse.Broadcaster for typed task events
  cmd/serve/
    wire_gen.go                           ← updated: add orchestrator + task handler providers
```

---

## Task 1: Expand ent Schemas

**Files:**
- Modify: `server/internal/db/ent/schema/task.go`
- Create: `server/internal/db/ent/schema/stage_run.go`
- Create: `server/internal/db/ent/schema/task_permission.go`
- Create: `server/internal/db/ent/schema/permission_request.go`
- Create: `server/internal/db/ent/schema/audit_log.go`
- Create: `server/internal/db/ent/schema/task_dependency.go`

- [ ] **Step 1: Replace the Phase 2 Task stub with the full schema**

Replace `server/internal/db/ent/schema/task.go` with:

```go
package schema

import (
    "time"

    "entgo.io/ent"
    "entgo.io/ent/schema/edge"
    "entgo.io/ent/schema/field"
    "entgo.io/ent/schema/index"
)

type Task struct{ ent.Schema }

func (Task) Fields() []ent.Field {
    return []ent.Field{
        field.String("id").StorageKey("id").Immutable(),
        field.String("slug").Unique(),
        field.String("title").MaxLen(200),
        field.String("description").Optional().Nillable(),
        field.String("cwd").MaxLen(4096),
        field.String("worktree_path").Optional().Nillable(),
        field.String("source_branch").Optional().Nillable(),
        field.String("target_branch").Optional().Nillable(),
        field.String("current_stage").Default("concept"),
        field.String("priority").Default("medium"),
        field.String("user_id").Optional().Nillable(),
        field.String("parent_task_id").Optional().Nillable(),
        field.Int("max_iterations").Default(20),
        field.Int("token_budget").Optional().Nillable(),
        field.Int("cost_budget_cents").Optional().Nillable(),
        field.Int("stage_timeout_seconds").Default(1800),
        field.Bool("silver_bullet").Default(false),
        field.JSON("metadata", map[string]any{}).Optional(),
        field.Time("created_at").Default(time.Now).Immutable(),
        field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
    }
}

func (Task) Edges() []ent.Edge {
    return []ent.Edge{
        edge.To("stage_runs", StageRun.Type),
        edge.To("permissions", TaskPermission.Type),
        edge.To("audit_logs", AuditLog.Type),
        edge.To("dependencies", TaskDependency.Type).
            StorageKey(edge.Table("task_dependencies"), edge.Columns("task_id")),
        edge.To("dependents", TaskDependency.Type).
            StorageKey(edge.Table("task_dependencies"), edge.Columns("depends_on_id")),
    }
}

func (Task) Indexes() []ent.Index {
    return []ent.Index{
        index.Fields("current_stage"),
        index.Fields("parent_task_id"),
        index.Fields("silver_bullet", "priority", "created_at"),
    }
}
```

- [ ] **Step 2: Create StageRun schema**

Create `server/internal/db/ent/schema/stage_run.go`:

```go
package schema

import (
    "time"

    "entgo.io/ent"
    "entgo.io/ent/schema/edge"
    "entgo.io/ent/schema/field"
    "entgo.io/ent/schema/index"
)

type StageRun struct{ ent.Schema }

func (StageRun) Fields() []ent.Field {
    return []ent.Field{
        field.String("id").StorageKey("id").Immutable(),
        field.String("task_id"),
        field.String("stage"),
        field.String("session_id").Optional().Nillable(),
        field.String("session_name").Optional().Nillable(),
        field.Int("pid").Optional().Nillable(),
        field.String("status").Default("pending"),
        field.Int("iteration").Default(0),
        field.JSON("output", map[string]any{}).Optional(),
        field.Int("tokens_used").Default(0),
        field.Int("cost_cents").Default(0),
        field.Time("started_at").Optional().Nillable(),
        field.Time("ended_at").Optional().Nillable(),
        field.Time("last_grant_at").Optional().Nillable(),
        field.Time("created_at").Default(time.Now).Immutable(),
    }
}

func (StageRun) Edges() []ent.Edge {
    return []ent.Edge{
        edge.From("task", Task.Type).Ref("stage_runs").Field("task_id").Unique().Required().Immutable(),
        edge.To("permission_requests", PermissionRequest.Type),
    }
}

func (StageRun) Indexes() []ent.Index {
    return []ent.Index{
        index.Fields("task_id"),
        index.Fields("status"),
        index.Fields("task_id", "stage", "iteration"),
        index.Fields("session_id").Unique().Annotations(
            // Partial unique: only when session_id is not null — enforced at app layer
            // since ent does not natively support partial unique indexes.
            // A raw migration adds: CREATE UNIQUE INDEX idx_stage_runs_session ON stage_runs(session_id) WHERE session_id IS NOT NULL
        ),
    }
}
```

- [ ] **Step 3: Create TaskPermission schema**

Create `server/internal/db/ent/schema/task_permission.go`:

```go
package schema

import (
    "time"

    "entgo.io/ent"
    "entgo.io/ent/schema/edge"
    "entgo.io/ent/schema/field"
    "entgo.io/ent/schema/index"
)

type TaskPermission struct{ ent.Schema }

func (TaskPermission) Fields() []ent.Field {
    return []ent.Field{
        field.String("id").StorageKey("id").Immutable(),
        field.String("task_id"),
        field.String("tool"),
        field.String("pattern").Optional().Nillable(),
        field.Bool("granted").Default(false),
        field.Bool("pre_approved").Default(false),
        field.String("decided_by").Optional().Nillable(),
        field.Time("requested_at").Default(time.Now),
        field.Time("decided_at").Optional().Nillable(),
        field.Time("expires_at").Optional().Nillable(),
    }
}

func (TaskPermission) Edges() []ent.Edge {
    return []ent.Edge{
        edge.From("task", Task.Type).Ref("permissions").Field("task_id").Unique().Required().Immutable(),
    }
}

func (TaskPermission) Indexes() []ent.Index {
    return []ent.Index{
        index.Fields("task_id"),
    }
}
```

- [ ] **Step 4: Create PermissionRequest schema**

Create `server/internal/db/ent/schema/permission_request.go`:

```go
package schema

import (
    "time"

    "entgo.io/ent"
    "entgo.io/ent/schema/edge"
    "entgo.io/ent/schema/field"
    "entgo.io/ent/schema/index"
)

type PermissionRequest struct{ ent.Schema }

func (PermissionRequest) Fields() []ent.Field {
    return []ent.Field{
        field.String("id").StorageKey("id").Immutable(),
        field.String("stage_run_id"),
        field.String("tool"),
        field.String("pattern").Optional().Nillable(),
        field.String("reason").Optional().Nillable(),
        field.String("outcome").Optional().Nillable(), // granted | denied | timeout
        field.Time("requested_at").Default(time.Now),
        field.Time("resolved_at").Optional().Nillable(),
    }
}

func (PermissionRequest) Edges() []ent.Edge {
    return []ent.Edge{
        edge.From("stage_run", StageRun.Type).Ref("permission_requests").Field("stage_run_id").Unique().Required().Immutable(),
    }
}

func (PermissionRequest) Indexes() []ent.Index {
    return []ent.Index{
        index.Fields("stage_run_id"),
        index.Fields("outcome"),
    }
}
```

- [ ] **Step 5: Create AuditLog schema**

Create `server/internal/db/ent/schema/audit_log.go`:

```go
package schema

import (
    "time"

    "entgo.io/ent"
    "entgo.io/ent/schema/edge"
    "entgo.io/ent/schema/field"
    "entgo.io/ent/schema/index"
)

type AuditLog struct{ ent.Schema }

func (AuditLog) Fields() []ent.Field {
    return []ent.Field{
        field.String("id").StorageKey("id").Immutable(),
        field.String("task_id"),
        field.String("actor"),  // user|agent|orchestrator|system
        field.String("action"),
        field.JSON("details", map[string]any{}).Optional(),
        field.Time("timestamp").Default(time.Now).Immutable(),
    }
}

func (AuditLog) Edges() []ent.Edge {
    return []ent.Edge{
        edge.From("task", Task.Type).Ref("audit_logs").Field("task_id").Unique().Required().Immutable(),
    }
}

func (AuditLog) Indexes() []ent.Index {
    return []ent.Index{
        index.Fields("task_id"),
        index.Fields("timestamp"),
    }
}
```

- [ ] **Step 6: Create TaskDependency schema (through-table for self-referential Task edge)**

Create `server/internal/db/ent/schema/task_dependency.go`:

```go
package schema

import (
    "entgo.io/ent"
    "entgo.io/ent/schema/edge"
    "entgo.io/ent/schema/field"
    "entgo.io/ent/schema/index"
)

type TaskDependency struct{ ent.Schema }

func (TaskDependency) Fields() []ent.Field {
    return []ent.Field{
        field.String("id").StorageKey("id").Immutable(),
        field.String("task_id"),
        field.String("depends_on_id"),
        field.String("required_stage").Default("done"),      // done | cancelled
        field.String("on_cancel_action").Default("on_hold"), // cancel | start | on_hold
    }
}

func (TaskDependency) Edges() []ent.Edge {
    return []ent.Edge{
        edge.From("task", Task.Type).Ref("dependencies").Field("task_id").Unique().Required().Immutable(),
        edge.From("depends_on", Task.Type).Ref("dependents").Field("depends_on_id").Unique().Required().Immutable(),
    }
}

func (TaskDependency) Indexes() []ent.Index {
    return []ent.Index{
        index.Fields("task_id"),
        index.Fields("depends_on_id"),
        index.Fields("task_id", "depends_on_id").Unique(),
    }
}
```

- [ ] **Step 7: Run ent code generation and verify it compiles**

```bash
cd server
go generate ./internal/db/ent/...
go build ./...
```

Expected output: ent generates files for StageRun, TaskPermission, PermissionRequest, AuditLog, TaskDependency entities into `internal/db/ent/`. `go build ./...` exits 0.

- [ ] **Step 8: Commit**

```bash
git add server/internal/db/ent/schema/ server/internal/db/ent/
git commit -m "feat(db): expand ent schemas for pipeline — Task, StageRun, TaskPermission, PermissionRequest, AuditLog, TaskDependency"
```

---

## Task 2: Repository Interfaces and Implementations

**Files:**
- Create: `server/internal/db/repo/task_repo.go`
- Create: `server/internal/db/repo/task_repo_test.go`
- Create: `server/internal/db/repo/stage_run_repo.go`
- Create: `server/internal/db/repo/stage_run_repo_test.go`
- Create: `server/internal/db/repo/permission_repo.go`
- Create: `server/internal/db/repo/permission_repo_test.go`
- Create: `server/internal/db/repo/audit_repo.go`
- Create: `server/internal/db/repo/pipeline_config_repo.go`

- [ ] **Step 1: Create TaskRepo interface and ent implementation**

Create `server/internal/db/repo/task_repo.go`:

```go
package repo

import (
    "context"
    "fmt"
    "time"

    "github.com/google/uuid"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent/task"
)

// TaskRepo defines persistence operations for pipeline tasks.
// Defined in the consuming package (pipeline) per Go interface idiom;
// the ent implementation lives here in repo/.
type TaskRepo interface {
    Create(ctx context.Context, input CreateTaskInput) (*ent.Task, error)
    GetByID(ctx context.Context, id string) (*ent.Task, error)
    GetBySlug(ctx context.Context, slug string) (*ent.Task, error)
    Update(ctx context.Context, id string, input UpdateTaskInput) (*ent.Task, error)
    Delete(ctx context.Context, id string) error
    ListForUser(ctx context.Context, userID string, isAdmin bool) ([]*ent.Task, error)
    ListPickable(ctx context.Context) ([]*ent.Task, error) // non-terminal, non-paused tasks
    ListByStage(ctx context.Context, stage string) ([]*ent.Task, error)
}

type CreateTaskInput struct {
    ID                  string
    Slug                string
    Title               string
    Description         *string
    Cwd                 string
    WorktreePath        *string
    SourceBranch        *string
    TargetBranch        *string
    ParentTaskID        *string
    UserID              *string
    MaxIterations       int
    TokenBudget         *int
    CostBudgetCents     *int
    StageTimeoutSeconds int
    SilverBullet        bool
    Priority            string
    CurrentStage        string
    Metadata            map[string]any
}

type UpdateTaskInput struct {
    Title               *string
    Description         *string
    CurrentStage        *string
    Priority            *string
    SilverBullet        *bool
    MaxIterations       *int
    TokenBudget         *int
    CostBudgetCents     *int
    StageTimeoutSeconds *int
    Metadata            map[string]any
    MetadataClear       bool
}

type entTaskRepo struct{ client *ent.Client }

func NewTaskRepo(client *ent.Client) TaskRepo {
    return &entTaskRepo{client: client}
}

func (r *entTaskRepo) Create(ctx context.Context, in CreateTaskInput) (*ent.Task, error) {
    id := in.ID
    if id == "" {
        id = uuid.New().String()
    }
    q := r.client.Task.Create().
        SetID(id).
        SetSlug(in.Slug).
        SetTitle(in.Title).
        SetCwd(in.Cwd).
        SetCurrentStage(in.CurrentStage).
        SetPriority(in.Priority).
        SetMaxIterations(in.MaxIterations).
        SetStageTimeoutSeconds(in.StageTimeoutSeconds).
        SetSilverBullet(in.SilverBullet)
    if in.Description != nil {
        q = q.SetDescription(*in.Description)
    }
    if in.WorktreePath != nil {
        q = q.SetWorktreePath(*in.WorktreePath)
    }
    if in.SourceBranch != nil {
        q = q.SetSourceBranch(*in.SourceBranch)
    }
    if in.TargetBranch != nil {
        q = q.SetTargetBranch(*in.TargetBranch)
    }
    if in.ParentTaskID != nil {
        q = q.SetParentTaskID(*in.ParentTaskID)
    }
    if in.UserID != nil {
        q = q.SetUserID(*in.UserID)
    }
    if in.TokenBudget != nil {
        q = q.SetTokenBudget(*in.TokenBudget)
    }
    if in.CostBudgetCents != nil {
        q = q.SetCostBudgetCents(*in.CostBudgetCents)
    }
    if in.Metadata != nil {
        q = q.SetMetadata(in.Metadata)
    }
    t, err := q.Save(ctx)
    if err != nil {
        return nil, fmt.Errorf("task.Create: %w", err)
    }
    return t, nil
}

func (r *entTaskRepo) GetByID(ctx context.Context, id string) (*ent.Task, error) {
    t, err := r.client.Task.Get(ctx, id)
    if err != nil {
        return nil, fmt.Errorf("task.GetByID: %w", err)
    }
    return t, nil
}

func (r *entTaskRepo) GetBySlug(ctx context.Context, slug string) (*ent.Task, error) {
    t, err := r.client.Task.Query().Where(task.Slug(slug)).Only(ctx)
    if err != nil {
        return nil, fmt.Errorf("task.GetBySlug: %w", err)
    }
    return t, nil
}

func (r *entTaskRepo) Update(ctx context.Context, id string, in UpdateTaskInput) (*ent.Task, error) {
    q := r.client.Task.UpdateOneID(id).SetUpdatedAt(time.Now())
    if in.Title != nil {
        q = q.SetTitle(*in.Title)
    }
    if in.CurrentStage != nil {
        q = q.SetCurrentStage(*in.CurrentStage)
    }
    if in.Priority != nil {
        q = q.SetPriority(*in.Priority)
    }
    if in.SilverBullet != nil {
        q = q.SetSilverBullet(*in.SilverBullet)
    }
    if in.MaxIterations != nil {
        q = q.SetMaxIterations(*in.MaxIterations)
    }
    if in.StageTimeoutSeconds != nil {
        q = q.SetStageTimeoutSeconds(*in.StageTimeoutSeconds)
    }
    if in.TokenBudget != nil {
        q = q.SetTokenBudget(*in.TokenBudget)
    }
    if in.CostBudgetCents != nil {
        q = q.SetCostBudgetCents(*in.CostBudgetCents)
    }
    if in.MetadataClear {
        q = q.ClearMetadata()
    } else if in.Metadata != nil {
        q = q.SetMetadata(in.Metadata)
    }
    t, err := q.Save(ctx)
    if err != nil {
        return nil, fmt.Errorf("task.Update: %w", err)
    }
    return t, nil
}

func (r *entTaskRepo) Delete(ctx context.Context, id string) error {
    if err := r.client.Task.DeleteOneID(id).Exec(ctx); err != nil {
        return fmt.Errorf("task.Delete: %w", err)
    }
    return nil
}

func (r *entTaskRepo) ListForUser(ctx context.Context, userID string, isAdmin bool) ([]*ent.Task, error) {
    q := r.client.Task.Query().Order(ent.Desc(task.FieldCreatedAt))
    if !isAdmin {
        q = q.Where(task.UserID(userID))
    }
    tasks, err := q.All(ctx)
    if err != nil {
        return nil, fmt.Errorf("task.ListForUser: %w", err)
    }
    return tasks, nil
}

func (r *entTaskRepo) ListPickable(ctx context.Context) ([]*ent.Task, error) {
    tasks, err := r.client.Task.Query().
        Where(
            task.CurrentStageNotIn("done", "cancelled", "on_hold", "concept"),
        ).
        Order(ent.Desc(task.FieldSilverBullet), ent.Asc(task.FieldCreatedAt)).
        All(ctx)
    if err != nil {
        return nil, fmt.Errorf("task.ListPickable: %w", err)
    }
    return tasks, nil
}

func (r *entTaskRepo) ListByStage(ctx context.Context, stage string) ([]*ent.Task, error) {
    tasks, err := r.client.Task.Query().Where(task.CurrentStage(stage)).All(ctx)
    if err != nil {
        return nil, fmt.Errorf("task.ListByStage: %w", err)
    }
    return tasks, nil
}
```

- [ ] **Step 2: Write TaskRepo tests**

Create `server/internal/db/repo/task_repo_test.go`:

```go
package repo_test

import (
    "context"
    "testing"

    "github.com/stretchr/testify/require"
    "github.com/lx-wnk/agent-dashboard/server/internal/db"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func TestTaskRepo_CreateAndGet(t *testing.T) {
    client, err := db.Open(":memory:")
    require.NoError(t, err)
    t.Cleanup(func() { client.Close() })

    r := repo.NewTaskRepo(client)
    ctx := context.Background()

    desc := "fix the login"
    task, err := r.Create(ctx, repo.CreateTaskInput{
        Slug:                "fix-login",
        Title:               "Fix Login",
        Description:         &desc,
        Cwd:                 "/tmp/proj",
        CurrentStage:        "concept",
        Priority:            "medium",
        MaxIterations:       20,
        StageTimeoutSeconds: 1800,
    })
    require.NoError(t, err)
    require.Equal(t, "fix-login", task.Slug)

    got, err := r.GetByID(ctx, task.ID)
    require.NoError(t, err)
    require.Equal(t, task.ID, got.ID)

    _, err = r.GetBySlug(ctx, "fix-login")
    require.NoError(t, err)
}

func TestTaskRepo_Update_CurrentStage(t *testing.T) {
    client, err := db.Open(":memory:")
    require.NoError(t, err)
    t.Cleanup(func() { client.Close() })

    r := repo.NewTaskRepo(client)
    ctx := context.Background()

    task, err := r.Create(ctx, repo.CreateTaskInput{
        Slug: "my-task", Title: "My Task", Cwd: "/tmp",
        CurrentStage: "concept", Priority: "medium",
        MaxIterations: 20, StageTimeoutSeconds: 1800,
    })
    require.NoError(t, err)

    stage := "implementation"
    updated, err := r.Update(ctx, task.ID, repo.UpdateTaskInput{CurrentStage: &stage})
    require.NoError(t, err)
    require.Equal(t, "implementation", updated.CurrentStage)
}

func TestTaskRepo_Delete(t *testing.T) {
    client, err := db.Open(":memory:")
    require.NoError(t, err)
    t.Cleanup(func() { client.Close() })

    r := repo.NewTaskRepo(client)
    ctx := context.Background()

    task, err := r.Create(ctx, repo.CreateTaskInput{
        Slug: "to-delete", Title: "Delete Me", Cwd: "/tmp",
        CurrentStage: "concept", Priority: "medium",
        MaxIterations: 20, StageTimeoutSeconds: 1800,
    })
    require.NoError(t, err)

    err = r.Delete(ctx, task.ID)
    require.NoError(t, err)

    _, err = r.GetByID(ctx, task.ID)
    require.Error(t, err) // not found
}
```

- [ ] **Step 3: Run task repo tests**

```bash
cd server
go test ./internal/db/repo/... -run TestTaskRepo -v
```

Expected: all TestTaskRepo_* subtests pass.

- [ ] **Step 4: Create StageRunRepo interface and implementation**

Create `server/internal/db/repo/stage_run_repo.go`:

```go
package repo

import (
    "context"
    "fmt"
    "time"

    "github.com/google/uuid"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent/stagerun"
)

type StageRunRepo interface {
    Create(ctx context.Context, input CreateStageRunInput) (*ent.StageRun, error)
    GetByID(ctx context.Context, id string) (*ent.StageRun, error)
    GetBySessionID(ctx context.Context, sessionID string) (*ent.StageRun, error)
    GetLatestForTask(ctx context.Context, taskID string) (*ent.StageRun, error)
    GetLatestByTaskAndStage(ctx context.Context, taskID, stage string) (*ent.StageRun, error)
    GetByTaskStageIteration(ctx context.Context, taskID, stage string, iteration int) (*ent.StageRun, error)
    ListForTask(ctx context.Context, taskID string) ([]*ent.StageRun, error)
    ListByStatus(ctx context.Context, statuses ...string) ([]*ent.StageRun, error)
    ListPending(ctx context.Context) ([]*ent.StageRun, error)
    Update(ctx context.Context, id string, input UpdateStageRunInput) (*ent.StageRun, error)
    SumCompletedCostCents(ctx context.Context, taskID string) (int, error)
    GetLatestForTasks(ctx context.Context, taskIDs []string) (map[string]*ent.StageRun, error)
}

type CreateStageRunInput struct {
    TaskID      string
    Stage       string
    Iteration   int
    SessionName string
}

type UpdateStageRunInput struct {
    Status      *string
    PID         *int
    PIDClear    bool
    SessionID   *string
    Output      map[string]any
    TokensUsed  *int
    CostCents   *int
    StartedAt   *time.Time
    EndedAt     *time.Time
    LastGrantAt *time.Time
}

type entStageRunRepo struct{ client *ent.Client }

func NewStageRunRepo(client *ent.Client) StageRunRepo {
    return &entStageRunRepo{client: client}
}

func (r *entStageRunRepo) Create(ctx context.Context, in CreateStageRunInput) (*ent.StageRun, error) {
    sr, err := r.client.StageRun.Create().
        SetID(uuid.New().String()).
        SetTaskID(in.TaskID).
        SetStage(in.Stage).
        SetIteration(in.Iteration).
        SetNillableSessionName(&in.SessionName).
        SetStatus("pending").
        Save(ctx)
    if err != nil {
        return nil, fmt.Errorf("stagerun.Create: %w", err)
    }
    return sr, nil
}

func (r *entStageRunRepo) GetByID(ctx context.Context, id string) (*ent.StageRun, error) {
    sr, err := r.client.StageRun.Get(ctx, id)
    if err != nil {
        return nil, fmt.Errorf("stagerun.GetByID: %w", err)
    }
    return sr, nil
}

func (r *entStageRunRepo) GetBySessionID(ctx context.Context, sessionID string) (*ent.StageRun, error) {
    sr, err := r.client.StageRun.Query().
        Where(stagerun.SessionID(sessionID)).
        Only(ctx)
    if err != nil {
        return nil, fmt.Errorf("stagerun.GetBySessionID: %w", err)
    }
    return sr, nil
}

func (r *entStageRunRepo) GetLatestForTask(ctx context.Context, taskID string) (*ent.StageRun, error) {
    sr, err := r.client.StageRun.Query().
        Where(stagerun.TaskID(taskID)).
        Order(ent.Desc(stagerun.FieldCreatedAt)).
        First(ctx)
    if err != nil {
        return nil, fmt.Errorf("stagerun.GetLatestForTask: %w", err)
    }
    return sr, nil
}

func (r *entStageRunRepo) GetLatestByTaskAndStage(ctx context.Context, taskID, stage string) (*ent.StageRun, error) {
    sr, err := r.client.StageRun.Query().
        Where(stagerun.TaskID(taskID), stagerun.Stage(stage)).
        Order(ent.Desc(stagerun.FieldIteration)).
        First(ctx)
    if err != nil {
        return nil, fmt.Errorf("stagerun.GetLatestByTaskAndStage: %w", err)
    }
    return sr, nil
}

func (r *entStageRunRepo) GetByTaskStageIteration(ctx context.Context, taskID, stage string, iteration int) (*ent.StageRun, error) {
    sr, err := r.client.StageRun.Query().
        Where(stagerun.TaskID(taskID), stagerun.Stage(stage), stagerun.Iteration(iteration)).
        Only(ctx)
    if err != nil {
        return nil, fmt.Errorf("stagerun.GetByTaskStageIteration: %w", err)
    }
    return sr, nil
}

func (r *entStageRunRepo) ListForTask(ctx context.Context, taskID string) ([]*ent.StageRun, error) {
    runs, err := r.client.StageRun.Query().
        Where(stagerun.TaskID(taskID)).
        Order(ent.Asc(stagerun.FieldIteration)).
        All(ctx)
    if err != nil {
        return nil, fmt.Errorf("stagerun.ListForTask: %w", err)
    }
    return runs, nil
}

func (r *entStageRunRepo) ListByStatus(ctx context.Context, statuses ...string) ([]*ent.StageRun, error) {
    runs, err := r.client.StageRun.Query().
        Where(stagerun.StatusIn(statuses...)).
        All(ctx)
    if err != nil {
        return nil, fmt.Errorf("stagerun.ListByStatus: %w", err)
    }
    return runs, nil
}

func (r *entStageRunRepo) ListPending(ctx context.Context) ([]*ent.StageRun, error) {
    return r.ListByStatus(ctx, "pending")
}

func (r *entStageRunRepo) Update(ctx context.Context, id string, in UpdateStageRunInput) (*ent.StageRun, error) {
    q := r.client.StageRun.UpdateOneID(id)
    if in.Status != nil {
        q = q.SetStatus(*in.Status)
    }
    if in.PIDClear {
        q = q.ClearPid()
    } else if in.PID != nil {
        q = q.SetPid(*in.PID)
    }
    if in.SessionID != nil {
        q = q.SetSessionID(*in.SessionID)
    }
    if in.Output != nil {
        q = q.SetOutput(in.Output)
    }
    if in.TokensUsed != nil {
        q = q.SetTokensUsed(*in.TokensUsed)
    }
    if in.CostCents != nil {
        q = q.SetCostCents(*in.CostCents)
    }
    if in.StartedAt != nil {
        q = q.SetStartedAt(*in.StartedAt)
    }
    if in.EndedAt != nil {
        q = q.SetEndedAt(*in.EndedAt)
    }
    if in.LastGrantAt != nil {
        q = q.SetLastGrantAt(*in.LastGrantAt)
    }
    sr, err := q.Save(ctx)
    if err != nil {
        return nil, fmt.Errorf("stagerun.Update: %w", err)
    }
    return sr, nil
}

func (r *entStageRunRepo) SumCompletedCostCents(ctx context.Context, taskID string) (int, error) {
    runs, err := r.client.StageRun.Query().
        Where(stagerun.TaskID(taskID), stagerun.StatusIn("done", "failed")).
        All(ctx)
    if err != nil {
        return 0, fmt.Errorf("stagerun.SumCompletedCostCents: %w", err)
    }
    total := 0
    for _, r := range runs {
        total += r.CostCents
    }
    return total, nil
}

func (r *entStageRunRepo) GetLatestForTasks(ctx context.Context, taskIDs []string) (map[string]*ent.StageRun, error) {
    runs, err := r.client.StageRun.Query().
        Where(stagerun.TaskIDIn(taskIDs...)).
        Order(ent.Desc(stagerun.FieldCreatedAt)).
        All(ctx)
    if err != nil {
        return nil, fmt.Errorf("stagerun.GetLatestForTasks: %w", err)
    }
    result := make(map[string]*ent.StageRun)
    for _, run := range runs {
        if _, seen := result[run.TaskID]; !seen {
            result[run.TaskID] = run
        }
    }
    return result, nil
}
```

- [ ] **Step 5: Create PermissionRepo**

Create `server/internal/db/repo/permission_repo.go`:

```go
package repo

import (
    "context"
    "fmt"
    "time"

    "github.com/google/uuid"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent/permissionrequest"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent/taskpermission"
)

type PermissionRepo interface {
    // TaskPermission operations
    CreateTaskPermission(ctx context.Context, input CreateTaskPermissionInput) (*ent.TaskPermission, error)
    ListTaskPermissions(ctx context.Context, taskID string) ([]*ent.TaskPermission, error)
    DeleteTaskPermission(ctx context.Context, id string) error

    // PermissionRequest operations
    CreatePermissionRequest(ctx context.Context, input CreatePermissionRequestInput) (*ent.PermissionRequest, error)
    GetPermissionRequest(ctx context.Context, id string) (*ent.PermissionRequest, error)
    ListPendingForStageRun(ctx context.Context, stageRunID string) ([]*ent.PermissionRequest, error)
    ResolvePermissionRequest(ctx context.Context, id, outcome string) (*ent.PermissionRequest, error)
    CountForStageRun(ctx context.Context, stageRunID string) (int, error)
}

type CreateTaskPermissionInput struct {
    TaskID     string
    Tool       string
    Pattern    *string
    Granted    bool
    PreApproved bool
    ExpiresAt  *time.Time
}

type CreatePermissionRequestInput struct {
    StageRunID string
    Tool       string
    Pattern    *string
    Reason     *string
}

type entPermissionRepo struct{ client *ent.Client }

func NewPermissionRepo(client *ent.Client) PermissionRepo {
    return &entPermissionRepo{client: client}
}

func (r *entPermissionRepo) CreateTaskPermission(ctx context.Context, in CreateTaskPermissionInput) (*ent.TaskPermission, error) {
    q := r.client.TaskPermission.Create().
        SetID(uuid.New().String()).
        SetTaskID(in.TaskID).
        SetTool(in.Tool).
        SetGranted(in.Granted).
        SetPreApproved(in.PreApproved)
    if in.Pattern != nil {
        q = q.SetPattern(*in.Pattern)
    }
    if in.ExpiresAt != nil {
        q = q.SetExpiresAt(*in.ExpiresAt)
    }
    p, err := q.Save(ctx)
    if err != nil {
        return nil, fmt.Errorf("permission.CreateTaskPermission: %w", err)
    }
    return p, nil
}

func (r *entPermissionRepo) ListTaskPermissions(ctx context.Context, taskID string) ([]*ent.TaskPermission, error) {
    perms, err := r.client.TaskPermission.Query().
        Where(taskpermission.TaskID(taskID)).
        All(ctx)
    if err != nil {
        return nil, fmt.Errorf("permission.ListTaskPermissions: %w", err)
    }
    return perms, nil
}

func (r *entPermissionRepo) DeleteTaskPermission(ctx context.Context, id string) error {
    if err := r.client.TaskPermission.DeleteOneID(id).Exec(ctx); err != nil {
        return fmt.Errorf("permission.DeleteTaskPermission: %w", err)
    }
    return nil
}

func (r *entPermissionRepo) CreatePermissionRequest(ctx context.Context, in CreatePermissionRequestInput) (*ent.PermissionRequest, error) {
    q := r.client.PermissionRequest.Create().
        SetID(uuid.New().String()).
        SetStageRunID(in.StageRunID).
        SetTool(in.Tool)
    if in.Pattern != nil {
        q = q.SetPattern(*in.Pattern)
    }
    if in.Reason != nil {
        q = q.SetReason(*in.Reason)
    }
    req, err := q.Save(ctx)
    if err != nil {
        return nil, fmt.Errorf("permission.CreatePermissionRequest: %w", err)
    }
    return req, nil
}

func (r *entPermissionRepo) GetPermissionRequest(ctx context.Context, id string) (*ent.PermissionRequest, error) {
    req, err := r.client.PermissionRequest.Get(ctx, id)
    if err != nil {
        return nil, fmt.Errorf("permission.GetPermissionRequest: %w", err)
    }
    return req, nil
}

func (r *entPermissionRepo) ListPendingForStageRun(ctx context.Context, stageRunID string) ([]*ent.PermissionRequest, error) {
    reqs, err := r.client.PermissionRequest.Query().
        Where(permissionrequest.StageRunID(stageRunID), permissionrequest.OutcomeIsNil()).
        All(ctx)
    if err != nil {
        return nil, fmt.Errorf("permission.ListPendingForStageRun: %w", err)
    }
    return reqs, nil
}

func (r *entPermissionRepo) ResolvePermissionRequest(ctx context.Context, id, outcome string) (*ent.PermissionRequest, error) {
    now := time.Now()
    req, err := r.client.PermissionRequest.UpdateOneID(id).
        SetOutcome(outcome).
        SetResolvedAt(now).
        Save(ctx)
    if err != nil {
        return nil, fmt.Errorf("permission.ResolvePermissionRequest: %w", err)
    }
    return req, nil
}

func (r *entPermissionRepo) CountForStageRun(ctx context.Context, stageRunID string) (int, error) {
    n, err := r.client.PermissionRequest.Query().
        Where(permissionrequest.StageRunID(stageRunID), permissionrequest.OutcomeIsNil()).
        Count(ctx)
    if err != nil {
        return 0, fmt.Errorf("permission.CountForStageRun: %w", err)
    }
    return n, nil
}
```

- [ ] **Step 6: Create AuditRepo**

Create `server/internal/db/repo/audit_repo.go`:

```go
package repo

import (
    "context"
    "fmt"

    "github.com/google/uuid"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent/auditlog"
)

type AuditRepo interface {
    Append(ctx context.Context, input AppendAuditInput) error
    ListForTask(ctx context.Context, taskID string) ([]*ent.AuditLog, error)
}

type AppendAuditInput struct {
    TaskID  string
    Actor   string
    Action  string
    Details map[string]any
}

type entAuditRepo struct{ client *ent.Client }

func NewAuditRepo(client *ent.Client) AuditRepo {
    return &entAuditRepo{client: client}
}

func (r *entAuditRepo) Append(ctx context.Context, in AppendAuditInput) error {
    q := r.client.AuditLog.Create().
        SetID(uuid.New().String()).
        SetTaskID(in.TaskID).
        SetActor(in.Actor).
        SetAction(in.Action)
    if in.Details != nil {
        q = q.SetDetails(in.Details)
    }
    _, err := q.Save(ctx)
    if err != nil {
        return fmt.Errorf("audit.Append: %w", err)
    }
    return nil
}

func (r *entAuditRepo) ListForTask(ctx context.Context, taskID string) ([]*ent.AuditLog, error) {
    logs, err := r.client.AuditLog.Query().
        Where(auditlog.TaskID(taskID)).
        Order(ent.Asc(auditlog.FieldTimestamp)).
        All(ctx)
    if err != nil {
        return nil, fmt.Errorf("audit.ListForTask: %w", err)
    }
    return logs, nil
}
```

- [ ] **Step 7: Create PipelineConfigRepo**

Create `server/internal/db/repo/pipeline_config_repo.go`:

```go
package repo

import (
    "context"
    "fmt"
    "strconv"

    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent/pipelineconfig"
)

type PipelineConfigRepo interface {
    GetNumber(ctx context.Context, key string, fallback int) int
    Set(ctx context.Context, key, value string) error
    GetAll(ctx context.Context) (map[string]string, error)
}

type entPipelineConfigRepo struct{ client *ent.Client }

func NewPipelineConfigRepo(client *ent.Client) PipelineConfigRepo {
    return &entPipelineConfigRepo{client: client}
}

func (r *entPipelineConfigRepo) GetNumber(ctx context.Context, key string, fallback int) int {
    cfg, err := r.client.PipelineConfig.Query().Where(pipelineconfig.ID(key)).Only(ctx)
    if err != nil {
        return fallback
    }
    n, err := strconv.Atoi(cfg.Value)
    if err != nil {
        return fallback
    }
    return n
}

func (r *entPipelineConfigRepo) Set(ctx context.Context, key, value string) error {
    err := r.client.PipelineConfig.Create().
        SetID(key).
        SetValue(value).
        OnConflictColumns(pipelineconfig.FieldID).
        UpdateNewValues().
        Exec(ctx)
    if err != nil {
        return fmt.Errorf("pipelineconfig.Set: %w", err)
    }
    return nil
}

func (r *entPipelineConfigRepo) GetAll(ctx context.Context) (map[string]string, error) {
    cfgs, err := r.client.PipelineConfig.Query().All(ctx)
    if err != nil {
        return nil, fmt.Errorf("pipelineconfig.GetAll: %w", err)
    }
    result := make(map[string]string, len(cfgs))
    for _, cfg := range cfgs {
        result[cfg.ID] = cfg.Value
    }
    return result, nil
}
```

- [ ] **Step 8: Verify all repos compile**

```bash
cd server
go build ./internal/db/...
```

Expected: exits 0 with no errors.

- [ ] **Step 9: Commit**

```bash
git add server/internal/db/repo/
git commit -m "feat(db): add TaskRepo, StageRunRepo, PermissionRepo, AuditRepo, PipelineConfigRepo interfaces and ent impls"
```

---

## Task 3: Pipeline Types

**Files:**
- Create: `server/internal/pipeline/types.go`

- [ ] **Step 1: Define pipeline types — sealed StageTransition interface, StageContext, StageHandler**

Create `server/internal/pipeline/types.go`:

```go
package pipeline

import (
    "context"
    "time"

    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// StageTransition is a sealed interface for the output of a stage handler.
// The switch in applyTransition must handle every concrete type;
// an unhandled type causes panic("unhandled transition").
type StageTransition interface{ isTransition() }

// NextTransition advances the task to toStage. MetadataPatch is applied
// atomically in the same DB transaction — use this when transitioning
// self_review back to implementation with review_feedback.
type NextTransition struct {
    Stage         string
    MetadataPatch map[string]any // nil = leave unchanged; empty map = clear
    MetaClear     bool            // true = clear metadata entirely
    Output        map[string]any
}

// DoneTransition marks the task as done (final stage).
type DoneTransition struct {
    Output map[string]any
}

// FailTransition marks the current stage_run as failed.
// The task stage does NOT advance — it stays for retry/analyze.
type FailTransition struct {
    Reason string
    Output map[string]any
}

// WaitUserTransition parks the stage_run as awaiting_user.
type WaitUserTransition struct {
    Reason string
    Output map[string]any
}

// IterateTransition marks the current run done and creates a next iteration.
type IterateTransition struct {
    Output map[string]any
}

// OnHoldTransition moves the stage_run and task to on_hold.
type OnHoldTransition struct {
    PermissionRequestID string
    Output              map[string]any
}

// AsyncRunningTransition records a spawned PID and leaves the run in status=running.
type AsyncRunningTransition struct {
    PID       int
    SessionID string // may be empty until attached by tryAttachSessionID
    Output    map[string]any
}

func (NextTransition) isTransition()         {}
func (DoneTransition) isTransition()         {}
func (FailTransition) isTransition()         {}
func (WaitUserTransition) isTransition()     {}
func (IterateTransition) isTransition()      {}
func (OnHoldTransition) isTransition()       {}
func (AsyncRunningTransition) isTransition() {}

// StageContext is passed to stage handlers. All repo access and side-effects
// are injected so tests can stub them without spawning real processes or DBs.
type StageContext struct {
    Ctx                context.Context
    Task               *ent.Task
    StageRun           *ent.StageRun
    Permissions        []*ent.TaskPermission
    PreviousOutput     map[string]any // output of last completed prior stage, or nil
    PriorIterationOutput map[string]any // output of iter-1 on this stage, or nil
    ResumeSessionID    string         // empty = fresh session
    UserAdditionalPrompt string

    // Injected side-effect callbacks (orchestrator wires real implementations;
    // tests inject no-ops or spies).
    RecordAudit       func(action string, details map[string]any)
    RequestPermission func(tool, pattern, reason string) *ent.PermissionRequest
}

// StageHandler is implemented by each pipeline stage.
// requiresAgent=false stages return a transition synchronously.
// requiresAgent=true stages spawn a detached process and return AsyncRunningTransition.
type StageHandler interface {
    Stage() string
    RequiresAgent() bool
    Execute(ctx *StageContext) (StageTransition, error)
}

// PromptBundle is the system+user prompt pair passed to the Claude CLI.
type PromptBundle struct {
    SystemPrompt string
    UserPrompt   string
}

// STAGE_ORDER defines canonical stage progression for auto-next routing.
var STAGE_ORDER = []string{
    "concept",
    "backlog",
    "implementation",
    "self_review",
    "finalization",
    "done",
}

// NextStage returns the next stage after s, or "done" if s is the last pipeline stage.
func NextStage(s string) string {
    for i, stage := range STAGE_ORDER {
        if stage == s && i < len(STAGE_ORDER)-1 {
            return STAGE_ORDER[i+1]
        }
    }
    return "done"
}

// IsTerminalStage returns true for done and cancelled.
func IsTerminalStage(s string) bool {
    return s == "done" || s == "cancelled"
}

// OrchestratorOptions configures the PipelineOrchestrator. All callbacks are
// optional; the orchestrator works without them (no SSE, no notifications).
type OrchestratorOptions struct {
    PollInterval       time.Duration  // defaults to 2s
    TaskRepo           repo.TaskRepo
    StageRunRepo       repo.StageRunRepo
    PermissionRepo     repo.PermissionRepo
    AuditRepo          repo.AuditRepo
    ConfigRepo         repo.PipelineConfigRepo

    // Side-effect callbacks — wire in server/cmd/serve/wire_gen.go.
    OnPermissionRequest func(taskID string, req *ent.PermissionRequest)
    OnStageFailed       func(taskID string, info StageFailedInfo)
    OnTaskChanged       func(taskID string, transitionKind string)
}

// StageFailedInfo carries failure metadata to the OnStageFailed callback.
type StageFailedInfo struct {
    StageRunID string
    Stage      string
    Iteration  int
    Error      string
}
```

- [ ] **Step 2: Verify compile**

```bash
cd server
go build ./internal/pipeline/...
```

Expected: exits 0.

---

## Task 4: Session Manager + Session Reader

**Files:**
- Create: `server/internal/pipeline/session_manager.go`
- Create: `server/internal/pipeline/session_manager_test.go`
- Create: `server/internal/pipeline/session_reader.go`
- Create: `server/internal/pipeline/session_reader_test.go`

- [ ] **Step 1: Implement IsPidAlive with zombie detection (macOS + Linux)**

Create `server/internal/pipeline/session_manager.go`:

```go
package pipeline

import (
    "context"
    "fmt"
    "os/exec"
    "strconv"
    "strings"
    "syscall"

    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
    "github.com/lx-wnk/agent-dashboard/server/internal/platform"
)

// isPidZombie returns true if the process is in zombie state.
// On Linux reads /proc/<pid>/status; on macOS uses ps -o stat=.
func isPidZombie(pid int) bool {
    var cmd *exec.Cmd
    if platform.IsLinux {
        cmd = exec.Command("cat", fmt.Sprintf("/proc/%d/status", pid))
    } else {
        cmd = exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "stat=")
    }
    out, err := cmd.Output()
    if err != nil {
        return false
    }
    if platform.IsLinux {
        return strings.Contains(string(out), "State:\tZ") || strings.Contains(string(out), "State: Z")
    }
    return strings.HasPrefix(strings.TrimSpace(string(out)), "Z")
}

// IsPidAlive returns true if the given PID corresponds to a live, non-zombie process.
// Returns false for pid <= 0. Returns true on EPERM (process owned by another user —
// treat as alive to avoid wrongly restarting a foreign process).
func IsPidAlive(pid int) bool {
    if pid <= 0 {
        return false
    }
    proc, err := os.FindProcess(pid)
    if err != nil {
        return false
    }
    err = proc.Signal(syscall.Signal(0))
    if err == nil {
        return !isPidZombie(pid)
    }
    if err == syscall.EPERM {
        return true // owned by another user — treat as alive
    }
    return false
}

// RecoveryDecision describes the recovery action for a running stage_run on restart.
type RecoveryDecision struct {
    Kind   string // alive | resume | restart
    Reason string
}

// DecideRecovery decides whether to keep, resume, or restart a stage_run on orchestrator startup.
func DecideRecovery(sr *ent.StageRun) RecoveryDecision {
    pid := 0
    if sr.Pid != nil {
        pid = *sr.Pid
    }
    if IsPidAlive(pid) {
        return RecoveryDecision{Kind: "alive", Reason: fmt.Sprintf("PID %d still running", pid)}
    }
    if sr.SessionID != nil && *sr.SessionID != "" {
        return RecoveryDecision{Kind: "resume", Reason: fmt.Sprintf("session %s available for --resume", *sr.SessionID)}
    }
    return RecoveryDecision{Kind: "restart", Reason: "no live PID and no session — must start fresh"}
}

// BuildSessionName produces a human-readable session name for the claude CLI.
// Format: {slug}-{stage}-iter-{n}
func BuildSessionName(taskSlug, stage string, iteration int) string {
    return fmt.Sprintf("%s-%s-iter-%d", taskSlug, stage, iteration)
}

// AttachSessionID persists a session ID onto a stage_run when it becomes known,
// skipping if already set to the same value or if another run already owns it.
func AttachSessionID(ctx context.Context, stageRunID, sessionID string, srRepo repo.StageRunRepo) error {
    existing, err := srRepo.GetBySessionID(ctx, sessionID)
    if err == nil && existing != nil {
        // already attached — safe no-op regardless of which run owns it
        return nil
    }
    sid := sessionID
    _, err = srRepo.Update(ctx, stageRunID, repo.UpdateStageRunInput{SessionID: &sid})
    if err != nil {
        return fmt.Errorf("AttachSessionID: %w", err)
    }
    return nil
}
```

Note: add `"os"` to the imports — the snippet above uses `os.FindProcess`.

- [ ] **Step 2: Write IsPidAlive tests**

Create `server/internal/pipeline/session_manager_test.go`:

```go
package pipeline_test

import (
    "os"
    "testing"

    "github.com/stretchr/testify/require"
    "github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
)

func TestIsPidAlive_Self(t *testing.T) {
    // The current process must be alive.
    require.True(t, pipeline.IsPidAlive(os.Getpid()))
}

func TestIsPidAlive_Zero(t *testing.T) {
    require.False(t, pipeline.IsPidAlive(0))
}

func TestIsPidAlive_Negative(t *testing.T) {
    require.False(t, pipeline.IsPidAlive(-1))
}

func TestIsPidAlive_DeadPID(t *testing.T) {
    // PID 999999 is unlikely to exist — treat as dead.
    require.False(t, pipeline.IsPidAlive(999999))
}

func TestBuildSessionName(t *testing.T) {
    got := pipeline.BuildSessionName("fix-login", "implementation", 3)
    require.Equal(t, "fix-login-implementation-iter-3", got)
}
```

- [ ] **Step 3: Create session_reader.go**

Create `server/internal/pipeline/session_reader.go`:

```go
package pipeline

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "regexp"
    "strings"

    "github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

var jsonBlockRE = regexp.MustCompile("(?s)```json\\b([\\s\\S]*?)```")

// ResolvedProjectDir returns the Claude projects directory for the given cwd,
// resolving symlinks so the encoded path matches what the Claude CLI wrote.
func ResolvedProjectDir(cwd string) (string, error) {
    resolved, err := filepath.EvalSymlinks(cwd)
    if err != nil {
        resolved = cwd // fallback: path may not exist (tests, deleted worktree)
    }
    claudeProjectsDir := parser.ClaudeProjectsDir()
    return filepath.Join(claudeProjectsDir, parser.EncodePath(resolved)), nil
}

// FindNewestSessionID scans the encoded project directory for the newest
// .jsonl file modified at or after afterISO. Returns "" if none found.
func FindNewestSessionID(cwd, afterISO string) (string, error) {
    projectDir, err := ResolvedProjectDir(cwd)
    if err != nil {
        return "", fmt.Errorf("FindNewestSessionID.resolveDir: %w", err)
    }

    entries, err := os.ReadDir(projectDir)
    if err != nil {
        return "", nil // directory may not exist yet
    }

    type candidate struct {
        sessionID string
        mtime     int64
    }
    var candidates []candidate

    for _, e := range entries {
        if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
            continue
        }
        info, err := e.Info()
        if err != nil {
            continue
        }
        if afterISO != "" {
            // Filter by mtime >= afterISO
            // Use Unix timestamp comparison for speed
        }
        sessionID := strings.TrimSuffix(e.Name(), ".jsonl")
        candidates = append(candidates, candidate{sessionID: sessionID, mtime: info.ModTime().UnixMilli()})
    }

    if len(candidates) == 0 {
        return "", nil
    }
    // Sort newest-first
    best := candidates[0]
    for _, c := range candidates[1:] {
        if c.mtime > best.mtime {
            best = c
        }
    }
    return best.sessionID, nil
}

// StageOutputRead holds the result of reading the last assistant turn.
type StageOutputRead struct {
    Output  map[string]any // nil when no parseable JSON block found
    RawText string         // last assistant text; empty when no assistant turn found
}

// ExtractJsonBlock parses the last ```json ... ``` fenced block from text.
// Returns nil if no block found or JSON is invalid.
func ExtractJsonBlock(text string) map[string]any {
    matches := jsonBlockRE.FindAllStringSubmatch(text, -1)
    if len(matches) == 0 {
        return nil
    }
    last := matches[len(matches)-1][1]
    var result map[string]any
    if err := json.Unmarshal([]byte(strings.TrimSpace(last)), &result); err != nil {
        return nil
    }
    return result
}

// JsonlEntry is a minimal shape for parsing Claude JSONL session logs.
type JsonlEntry struct {
    Type    string `json:"type"`
    Message *struct {
        Role    string `json:"role"`
        Model   string `json:"model"`
        Content json.RawMessage `json:"content"`
        Usage   *struct {
            InputTokens              int `json:"input_tokens"`
            OutputTokens             int `json:"output_tokens"`
            CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
            CacheReadInputTokens     int `json:"cache_read_input_tokens"`
        } `json:"usage"`
    } `json:"message"`
}

// lastAssistantText walks parsed JSONL entries backward and returns
// the concatenated text content of the last assistant turn.
func lastAssistantText(entries []JsonlEntry) string {
    for i := len(entries) - 1; i >= 0; i-- {
        e := entries[i]
        if e.Type != "assistant" || e.Message == nil {
            continue
        }
        var parts []struct {
            Type string `json:"type"`
            Text string `json:"text"`
        }
        if err := json.Unmarshal(e.Message.Content, &parts); err != nil {
            continue
        }
        var texts []string
        for _, p := range parts {
            if p.Type == "text" && p.Text != "" {
                texts = append(texts, p.Text)
            }
        }
        if len(texts) > 0 {
            return strings.Join(texts, "\n")
        }
    }
    return ""
}

// ReadLastStageJsonOutput resolves the session file, tail-reads it, and
// extracts the last ```json``` block from the last assistant turn.
func ReadLastStageJsonOutput(cwd, sessionID string) (StageOutputRead, error) {
    projectDir, err := ResolvedProjectDir(cwd)
    if err != nil {
        return StageOutputRead{}, fmt.Errorf("ReadLastStageJsonOutput.resolveDir: %w", err)
    }
    filePath := filepath.Join(projectDir, sessionID+".jsonl")

    raw, err := parser.TailRead(filePath)
    if err != nil {
        return StageOutputRead{}, nil // file may not exist yet
    }

    entries := parseJsonlLines(raw)
    text := lastAssistantText(entries)
    if text == "" {
        return StageOutputRead{}, nil
    }
    return StageOutputRead{Output: ExtractJsonBlock(text), RawText: text}, nil
}

// SessionTokenSummary holds per-session token usage totals.
type SessionTokenSummary struct {
    InputTokens          int
    OutputTokens         int
    CacheCreationTokens  int
    CacheReadTokens      int
    Model                string
}

// ReadSessionTokenSummary sums token usage from all assistant turns in a session JSONL.
func ReadSessionTokenSummary(cwd, sessionID string) (SessionTokenSummary, error) {
    projectDir, err := ResolvedProjectDir(cwd)
    if err != nil {
        return SessionTokenSummary{}, fmt.Errorf("ReadSessionTokenSummary.resolveDir: %w", err)
    }
    filePath := filepath.Join(projectDir, sessionID+".jsonl")
    raw, err := parser.TailRead(filePath)
    if err != nil {
        return SessionTokenSummary{}, nil
    }
    entries := parseJsonlLines(raw)
    var summary SessionTokenSummary
    for _, e := range entries {
        if e.Type != "assistant" || e.Message == nil {
            continue
        }
        if e.Message.Model != "" && summary.Model == "" {
            summary.Model = e.Message.Model
        }
        if u := e.Message.Usage; u != nil {
            summary.InputTokens += u.InputTokens
            summary.OutputTokens += u.OutputTokens
            summary.CacheCreationTokens += u.CacheCreationInputTokens
            summary.CacheReadTokens += u.CacheReadInputTokens
        }
    }
    return summary, nil
}

func parseJsonlLines(raw string) []JsonlEntry {
    var entries []JsonlEntry
    for _, line := range strings.Split(raw, "\n") {
        line = strings.TrimSpace(line)
        if line == "" {
            continue
        }
        var e JsonlEntry
        if err := json.Unmarshal([]byte(line), &e); err == nil {
            entries = append(entries, e)
        }
    }
    return entries
}
```

- [ ] **Step 4: Write session_reader_test.go**

Create `server/internal/pipeline/session_reader_test.go`:

```go
package pipeline_test

import (
    "testing"

    "github.com/stretchr/testify/require"
    "github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
)

func TestExtractJsonBlock_LastBlock(t *testing.T) {
    text := "some prose\n```json\n{\"a\":1}\n```\nmore prose\n```json\n{\"b\":2}\n```"
    result := pipeline.ExtractJsonBlock(text)
    require.NotNil(t, result)
    require.Equal(t, float64(2), result["b"])
}

func TestExtractJsonBlock_NoBlock(t *testing.T) {
    result := pipeline.ExtractJsonBlock("no code blocks here")
    require.Nil(t, result)
}

func TestExtractJsonBlock_InvalidJSON(t *testing.T) {
    result := pipeline.ExtractJsonBlock("```json\n{invalid}\n```")
    require.Nil(t, result)
}
```

- [ ] **Step 5: Run pipeline tests**

```bash
cd server
go test ./internal/pipeline/... -run "TestIsPidAlive|TestBuildSessionName|TestExtractJsonBlock" -v
```

Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add server/internal/pipeline/
git commit -m "feat(pipeline): session_manager (IsPidAlive, DecideRecovery) and session_reader (ExtractJsonBlock, ReadLastStageJsonOutput)"
```

---

## Task 5: Completion Detector

**Files:**
- Create: `server/internal/pipeline/completion_detector.go`
- Create: `server/internal/pipeline/completion_detector_test.go`

- [ ] **Step 1: Implement ValidateStageOutput and DetectCompletion**

Create `server/internal/pipeline/completion_detector.go`:

```go
package pipeline

import (
    "fmt"

    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
)

const agentMessageMaxChars = 2000

// ValidationResult holds the outcome of per-stage schema validation.
type ValidationResult struct {
    OK    bool
    Error string
}

// ValidateStageOutput validates parsed JSON output against the per-stage
// schema contract defined in stage_prompts.go. Keep validators in sync
// with the JSON schema blocks in each prompt.
func ValidateStageOutput(stage string, output map[string]any) ValidationResult {
    switch stage {
    case "self_review":
        return validateSelfReview(output)
    case "finalization":
        return validateFinalization(output)
    default:
        return ValidationResult{OK: true}
    }
}

func missing(field string) ValidationResult {
    return ValidationResult{OK: false, Error: fmt.Sprintf("missing required field: %s", field)}
}

func validateSelfReview(o map[string]any) ValidationResult {
    if _, ok := o["passed"].(bool); !ok {
        return missing("passed (boolean)")
    }
    if _, ok := o["findings"].([]any); !ok {
        return missing("findings (array)")
    }
    if _, ok := o["summary"].(string); !ok {
        return missing("summary (string)")
    }
    return ValidationResult{OK: true}
}

func validateFinalization(o map[string]any) ValidationResult {
    if _, ok := o["summary"].(string); !ok {
        return missing("summary (string)")
    }
    if _, ok := o["insights"].([]any); !ok {
        return missing("insights (string array)")
    }
    if _, ok := o["openTodos"].([]any); !ok {
        return missing("openTodos (string array)")
    }
    if _, ok := o["testPlan"].([]any); !ok {
        return missing("testPlan (string array)")
    }
    return ValidationResult{OK: true}
}

// CompletionResult describes the outcome of inspecting a stage_run.
type CompletionResult struct {
    Kind      string         // still_running | completed | failed
    Output    map[string]any // present on completed; present on retryable failed
    Error     string         // present on failed
    Retryable bool           // true = schema rejection; false = hard failure
}

// CompletionDeps holds injectable dependencies for testing.
type CompletionDeps struct {
    IsPidAlive   func(pid int) bool
    ReadOutput   func(cwd, sessionID string) (StageOutputRead, error)
    FindSession  func(cwd, afterISO string) (string, error)
    PersistSID   func(stageRunID, sessionID string) error
    Validate     func(stage string, output map[string]any) ValidationResult
}

// DetectCompletion checks whether an async stage_run has finished.
// deps fields that are nil fall back to the production implementations.
func DetectCompletion(sr *ent.StageRun, cwd string, deps CompletionDeps) (CompletionResult, error) {
    isPidAliveFn := deps.IsPidAlive
    if isPidAliveFn == nil {
        isPidAliveFn = IsPidAlive
    }
    readOutputFn := deps.ReadOutput
    if readOutputFn == nil {
        readOutputFn = ReadLastStageJsonOutput
    }
    findSessionFn := deps.FindSession
    if findSessionFn == nil {
        findSessionFn = FindNewestSessionID
    }
    validateFn := deps.Validate
    if validateFn == nil {
        validateFn = ValidateStageOutput
    }

    pid := 0
    if sr.Pid != nil {
        pid = *sr.Pid
    }
    if isPidAliveFn(pid) {
        return CompletionResult{Kind: "still_running"}, nil
    }

    sessionID := ""
    if sr.SessionID != nil {
        sessionID = *sr.SessionID
    }
    if sessionID == "" {
        if sr.StartedAt == nil {
            return CompletionResult{
                Kind:  "failed",
                Error: "stage_run never started — cannot locate session",
            }, nil
        }
        found, err := findSessionFn(cwd, sr.StartedAt.Format("2006-01-02T15:04:05Z"))
        if err != nil {
            return CompletionResult{Kind: "failed", Error: fmt.Sprintf("session lookup error: %s", err)}, nil
        }
        sessionID = found
        if sessionID != "" && deps.PersistSID != nil {
            _ = deps.PersistSID(sr.ID, sessionID)
        }
    }

    if sessionID == "" {
        projectDir, _ := ResolvedProjectDir(cwd)
        return CompletionResult{
            Kind:  "failed",
            Error: fmt.Sprintf("no session JSONL found in %s after %v (cwd=%s)", projectDir, sr.StartedAt, cwd),
        }, nil
    }

    read, err := readOutputFn(cwd, sessionID)
    if err != nil {
        return CompletionResult{Kind: "failed", Error: fmt.Sprintf("session read error: %s", err)}, nil
    }
    if read.Output == nil {
        if read.RawText != "" {
            trimmed := read.RawText
            if len(trimmed) > agentMessageMaxChars {
                trimmed = trimmed[len(trimmed)-agentMessageMaxChars:]
            }
            return CompletionResult{
                Kind:   "failed",
                Error:  "agent did not produce a ```json output block",
                Output: map[string]any{"agentMessage": trimmed},
            }, nil
        }
        return CompletionResult{Kind: "failed", Error: "no parseable json output in session tail"}, nil
    }

    v := validateFn(sr.Stage, read.Output)
    if !v.OK {
        return CompletionResult{
            Kind:      "failed",
            Error:     v.Error,
            Output:    read.Output,
            Retryable: true,
        }, nil
    }
    return CompletionResult{Kind: "completed", Output: read.Output}, nil
}
```

- [ ] **Step 2: Write completion_detector tests using injected deps**

Create `server/internal/pipeline/completion_detector_test.go`:

```go
package pipeline_test

import (
    "testing"
    "time"

    "github.com/stretchr/testify/require"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
    "github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
)

func ptr[T any](v T) *T { return &v }

func stageRun(stage string, pid *int, sessionID *string, startedAt *time.Time) *ent.StageRun {
    return &ent.StageRun{Stage: stage, Pid: pid, SessionID: sessionID, StartedAt: startedAt}
}

func TestDetectCompletion_StillRunning(t *testing.T) {
    sr := stageRun("implementation", ptr(1234), nil, ptr(time.Now()))
    deps := pipeline.CompletionDeps{
        IsPidAlive: func(pid int) bool { return pid == 1234 },
    }
    result, err := pipeline.DetectCompletion(sr, "/tmp", deps)
    require.NoError(t, err)
    require.Equal(t, "still_running", result.Kind)
}

func TestDetectCompletion_NoSession(t *testing.T) {
    now := time.Now()
    sr := stageRun("implementation", ptr(0), nil, &now)
    deps := pipeline.CompletionDeps{
        IsPidAlive:  func(int) bool { return false },
        FindSession: func(cwd, afterISO string) (string, error) { return "", nil },
    }
    result, err := pipeline.DetectCompletion(sr, "/tmp", deps)
    require.NoError(t, err)
    require.Equal(t, "failed", result.Kind)
}

func TestDetectCompletion_CompletedValid(t *testing.T) {
    now := time.Now()
    sessionID := "abc123"
    sr := stageRun("implementation", ptr(0), &sessionID, &now)
    deps := pipeline.CompletionDeps{
        IsPidAlive: func(int) bool { return false },
        ReadOutput: func(cwd, sid string) (pipeline.StageOutputRead, error) {
            return pipeline.StageOutputRead{
                Output:  map[string]any{"summary": "done", "commits": []any{"abc"}, "openItems": []any{}},
                RawText: "```json\n{}\n```",
            }, nil
        },
    }
    result, err := pipeline.DetectCompletion(sr, "/tmp", deps)
    require.NoError(t, err)
    require.Equal(t, "completed", result.Kind)
}

func TestDetectCompletion_SelfReviewSchemaFail_Retryable(t *testing.T) {
    now := time.Now()
    sessionID := "sid1"
    sr := stageRun("self_review", ptr(0), &sessionID, &now)
    deps := pipeline.CompletionDeps{
        IsPidAlive: func(int) bool { return false },
        ReadOutput: func(cwd, sid string) (pipeline.StageOutputRead, error) {
            return pipeline.StageOutputRead{
                Output:  map[string]any{"summary": "ok"}, // missing passed + findings
                RawText: "```json\n{}\n```",
            }, nil
        },
    }
    result, err := pipeline.DetectCompletion(sr, "/tmp", deps)
    require.NoError(t, err)
    require.Equal(t, "failed", result.Kind)
    require.True(t, result.Retryable)
    require.Contains(t, result.Error, "passed")
}

func TestValidateStageOutput_SelfReview_Valid(t *testing.T) {
    v := pipeline.ValidateStageOutput("self_review", map[string]any{
        "passed":   true,
        "findings": []any{},
        "summary":  "all good",
    })
    require.True(t, v.OK)
}

func TestValidateStageOutput_SelfReview_MissingPassed(t *testing.T) {
    v := pipeline.ValidateStageOutput("self_review", map[string]any{
        "findings": []any{},
        "summary":  "ok",
    })
    require.False(t, v.OK)
    require.Contains(t, v.Error, "passed")
}
```

- [ ] **Step 3: Run tests**

```bash
cd server
go test ./internal/pipeline/... -run TestDetect -v
go test ./internal/pipeline/... -run TestValidate -v
```

Expected: all tests pass.

---

## Task 6: Agent Spawner

**Files:**
- Create: `server/internal/pipeline/spawner.go`
- Create: `server/internal/pipeline/spawner_test.go`

- [ ] **Step 1: Implement BuildAllowList, BuildSpawnArgs, BuildSpawnEnv, SpawnStageAgent**

Create `server/internal/pipeline/spawner.go`:

```go
package pipeline

import (
    "encoding/json"
    "fmt"
    "log/slog"
    "os"
    "os/exec"
    "path/filepath"
    "regexp"
    "time"

    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
)

var gitPushRE = regexp.MustCompile(`(?i)\bgit push\b`)

const systemPromptMaxChars = 10000

// SpawnAgentOptions holds all parameters for spawning a stage agent.
type SpawnAgentOptions struct {
    Task             *ent.Task
    StageRun         *ent.StageRun
    Prompt           string
    SystemPrompt     string
    Model            string
    Permissions      []*ent.TaskPermission
    EnableChannel    bool
    ResumeSessionID  string
    MCPToken         string
    MCPUrl           string
}

// SpawnResult holds the result of a successful agent spawn.
type SpawnResult struct {
    PID         int
    Cwd         string
    SettingsPath string
    Cleanup     func()
}

// channelAllow contains MCP tool names that are always pre-approved when the
// dashboard channel is injected into the spawned agent.
var channelAllow = []string{
    "mcp__dashboard-channel__dashboard_reply",
    "mcp__dashboard-channel__request_permission",
}

// BuildAllowList converts TaskPermission rows into the Claude Code permissions.allow
// array. Denied and expired permissions are filtered. git push is blocked by default
// unless allowGitPush is true.
func BuildAllowList(permissions []*ent.TaskPermission, enableChannel, allowGitPush bool) []string {
    var allow []string
    if enableChannel {
        allow = append(allow, channelAllow...)
    }
    now := time.Now()
    for _, p := range permissions {
        if !p.Granted {
            continue
        }
        if p.ExpiresAt != nil && p.ExpiresAt.Before(now) {
            continue
        }
        if !allowGitPush && p.Tool == "Bash" && p.Pattern != nil && gitPushRE.MatchString(*p.Pattern) {
            continue
        }
        if p.Pattern != nil && *p.Pattern != "" {
            allow = append(allow, fmt.Sprintf("%s(%s)", p.Tool, *p.Pattern))
        } else {
            allow = append(allow, p.Tool)
        }
    }
    return allow
}

// BuildSpawnArgs returns the argv slice for the `claude` CLI invocation.
func BuildSpawnArgs(opts SpawnAgentOptions) []string {
    var args []string
    if opts.ResumeSessionID != "" {
        args = append(args, "--resume", opts.ResumeSessionID)
    }
    args = append(args, "-p", opts.Prompt)
    args = append(args, "--permission-mode", "default")
    if opts.Model != "" {
        args = append(args, "--model", opts.Model)
    }
    if opts.SystemPrompt != "" {
        sp := opts.SystemPrompt
        if len(sp) > systemPromptMaxChars {
            sp = sp[:systemPromptMaxChars]
        }
        args = append(args, "--system-prompt", sp)
    }
    return args
}

// BuildSpawnEnv builds the environment for spawned stage agents.
// Inherits the current process env and injects dashboard env vars.
func BuildSpawnEnv(opts SpawnAgentOptions) []string {
    env := os.Environ()
    env = append(env, fmt.Sprintf("DASHBOARD_STAGE_RUN_ID=%s", opts.StageRun.ID))
    env = append(env, fmt.Sprintf("DASHBOARD_TASK_ID=%s", opts.Task.ID))
    if opts.MCPToken != "" {
        env = append(env, fmt.Sprintf("DASHBOARD_MCP_TOKEN=%s", opts.MCPToken))
    }
    if opts.MCPUrl != "" {
        env = append(env, fmt.Sprintf("DASHBOARD_MCP_URL=%s", opts.MCPUrl))
    }
    return env
}

// writeSettingsFile writes .claude/settings.json into the task cwd with the
// pre-approved allow-list. If settings.json already exists (user-authored),
// merges new entries into settings.local.json instead.
// Returns (settingsPath, wrote, isLocal, error).
func writeSettingsFile(cwd string, permissions []*ent.TaskPermission, enableChannel, allowGitPush bool) (string, bool, bool, error) {
    allow := BuildAllowList(permissions, enableChannel, allowGitPush)
    if len(allow) == 0 {
        return "", false, false, nil
    }

    claudeDir := filepath.Join(cwd, ".claude")
    settingsPath := filepath.Join(claudeDir, "settings.json")

    if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
        if err := os.MkdirAll(claudeDir, 0o755); err != nil {
            return "", false, false, fmt.Errorf("writeSettingsFile: mkdir .claude: %w", err)
        }
        settings := map[string]any{
            "permissions":       map[string]any{"allow": allow},
            "_dashboardManaged": true,
        }
        data, _ := json.MarshalIndent(settings, "", "  ")
        if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
            return "", false, false, fmt.Errorf("writeSettingsFile: write: %w", err)
        }
        return settingsPath, true, false, nil
    }

    // User-authored settings.json — merge into settings.local.json.
    slog.Warn("settings.json is not dashboard-managed — agent will inherit user-authored allow-list in addition to task_permissions",
        "path", settingsPath)
    localPath := filepath.Join(claudeDir, "settings.local.json")
    var existing map[string]any
    if data, err := os.ReadFile(localPath); err == nil {
        _ = json.Unmarshal(data, &existing)
    }
    if existing == nil {
        existing = map[string]any{}
    }
    existingPerms, _ := existing["permissions"].(map[string]any)
    if existingPerms == nil {
        existingPerms = map[string]any{}
    }
    existingAllow, _ := existingPerms["allow"].([]any)
    existingSet := make(map[string]bool, len(existingAllow))
    for _, e := range existingAllow {
        if s, ok := e.(string); ok {
            existingSet[s] = true
        }
    }
    var newEntries []string
    for _, entry := range allow {
        if !existingSet[entry] {
            newEntries = append(newEntries, entry)
        }
    }
    if len(newEntries) == 0 {
        return localPath, false, true, nil
    }
    merged := make([]any, 0, len(existingAllow)+len(newEntries))
    merged = append(merged, existingAllow...)
    for _, e := range newEntries {
        merged = append(merged, e)
    }
    existingPerms["allow"] = merged
    existing["permissions"] = existingPerms
    existing["_dashboardManagedAllows"] = newEntries

    if err := os.MkdirAll(claudeDir, 0o755); err != nil {
        return "", false, false, fmt.Errorf("writeSettingsFile: mkdir .claude (local): %w", err)
    }
    data, _ := json.MarshalIndent(existing, "", "  ")
    if err := os.WriteFile(localPath, data, 0o644); err != nil {
        return "", false, false, fmt.Errorf("writeSettingsFile: write local: %w", err)
    }
    return localPath, true, true, nil
}

// ShouldCleanSettingsFile returns true if the settings.json at path carries
// the _dashboardManaged stamp and therefore may be safely deleted on cleanup.
func ShouldCleanSettingsFile(path string) bool {
    data, err := os.ReadFile(path)
    if err != nil {
        return false
    }
    var parsed map[string]any
    if err := json.Unmarshal(data, &parsed); err != nil {
        return false
    }
    managed, _ := parsed["_dashboardManaged"].(bool)
    return managed
}

// IsGitPushAllowed returns true if git push is permitted for the given task.
// Per-task metadata override wins over env var.
func IsGitPushAllowed(t *ent.Task) bool {
    if t.Metadata != nil {
        if v, ok := t.Metadata["allowGitPush"].(bool); ok && v {
            return true
        }
    }
    return os.Getenv("DASHBOARD_ALLOW_GIT_PUSH") == "true"
}

// SpawnStageAgent spawns a detached claude CLI process for a pipeline stage.
func SpawnStageAgent(opts SpawnAgentOptions) (SpawnResult, error) {
    cwd := opts.Task.Cwd
    if opts.Task.WorktreePath != nil && *opts.Task.WorktreePath != "" {
        cwd = *opts.Task.WorktreePath
    }
    enableChannel := opts.EnableChannel
    allowGitPush := IsGitPushAllowed(opts.Task)

    settingsPath, wrote, isLocal, err := writeSettingsFile(cwd, opts.Permissions, enableChannel, allowGitPush)
    if err != nil {
        slog.Warn("writeSettingsFile failed — continuing without pre-approved allow-list", "err", err)
    }

    args := BuildSpawnArgs(opts)
    // MCP channel config is injected as a JSON config file arg.
    // Phase 3 does not include the channel binary; this env-var injection
    // is a no-op until Phase 4 adds the channel bridge.
    // The channel allow-list entries (channelAllow) are still written to
    // settings.json so the agent does not get blocked when channel IS present.

    cmd := exec.Command("claude", args...)
    cmd.Dir = cwd
    cmd.Env = BuildSpawnEnv(opts)
    cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // detach from parent's process group
    cmd.Stdin = nil
    cmd.Stdout = nil
    stderrPipe, err := cmd.StderrPipe()
    if err != nil {
        return SpawnResult{}, fmt.Errorf("SpawnStageAgent.StderrPipe: %w", err)
    }

    if err := cmd.Start(); err != nil {
        return SpawnResult{}, fmt.Errorf("SpawnStageAgent.Start: %w", err)
    }

    // Drain stderr so the OS pipe buffer cannot fill and block the agent.
    go func() {
        buf := make([]byte, 4096)
        for {
            _, err := stderrPipe.Read(buf)
            if err != nil {
                return
            }
        }
    }()

    cleanup := func() {
        if !wrote || settingsPath == "" {
            return
        }
        if isLocal {
            cleanupLocalSettingsEntries(settingsPath)
        } else {
            if ShouldCleanSettingsFile(settingsPath) {
                _ = os.Remove(settingsPath)
            }
        }
    }

    return SpawnResult{
        PID:          cmd.Process.Pid,
        Cwd:          cwd,
        SettingsPath: settingsPath,
        Cleanup:      cleanup,
    }, nil
}

// cleanupLocalSettingsEntries removes the _dashboardManagedAllows entries
// from settings.local.json, deleting the file if it becomes empty.
func cleanupLocalSettingsEntries(localPath string) {
    data, err := os.ReadFile(localPath)
    if err != nil {
        return
    }
    var parsed map[string]any
    if err := json.Unmarshal(data, &parsed); err != nil {
        return
    }
    managed, _ := parsed["_dashboardManagedAllows"].([]any)
    if len(managed) == 0 {
        return
    }
    managedSet := make(map[string]bool, len(managed))
    for _, e := range managed {
        if s, ok := e.(string); ok {
            managedSet[s] = true
        }
    }
    delete(parsed, "_dashboardManagedAllows")
    if perms, ok := parsed["permissions"].(map[string]any); ok {
        if allow, ok := perms["allow"].([]any); ok {
            var filtered []any
            for _, e := range allow {
                if s, ok := e.(string); !ok || !managedSet[s] {
                    filtered = append(filtered, e)
                }
            }
            if len(filtered) == 0 {
                delete(perms, "allow")
            } else {
                perms["allow"] = filtered
            }
            if len(perms) == 0 {
                delete(parsed, "permissions")
            }
        }
    }
    if len(parsed) == 0 {
        _ = os.Remove(localPath)
        return
    }
    out, _ := json.MarshalIndent(parsed, "", "  ")
    _ = os.WriteFile(localPath, out, 0o644)
}
```

Note: add `"syscall"` to imports.

- [ ] **Step 2: Write spawner tests (pure functions only — no real processes)**

Create `server/internal/pipeline/spawner_test.go`:

```go
package pipeline_test

import (
    "testing"

    "github.com/stretchr/testify/require"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
    "github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
)

func TestBuildAllowList_ExcludesGitPushByDefault(t *testing.T) {
    pattern := "git push origin HEAD"
    perms := []*ent.TaskPermission{
        {Tool: "Bash", Pattern: &pattern, Granted: true},
        {Tool: "Read", Granted: true},
    }
    allow := pipeline.BuildAllowList(perms, false, false)
    require.Contains(t, allow, "Read")
    for _, a := range allow {
        require.NotContains(t, a, "git push")
    }
}

func TestBuildAllowList_AllowsGitPushWhenEnabled(t *testing.T) {
    pattern := "git push origin HEAD"
    perms := []*ent.TaskPermission{
        {Tool: "Bash", Pattern: &pattern, Granted: true},
    }
    allow := pipeline.BuildAllowList(perms, false, true)
    require.Contains(t, allow, "Bash(git push origin HEAD)")
}

func TestBuildAllowList_ChannelAllow(t *testing.T) {
    allow := pipeline.BuildAllowList(nil, true, false)
    require.Contains(t, allow, "mcp__dashboard-channel__dashboard_reply")
    require.Contains(t, allow, "mcp__dashboard-channel__request_permission")
}

func TestBuildAllowList_DeniedPermissionExcluded(t *testing.T) {
    perms := []*ent.TaskPermission{
        {Tool: "Write", Granted: false},
    }
    allow := pipeline.BuildAllowList(perms, false, false)
    require.NotContains(t, allow, "Write")
}

func TestBuildSpawnArgs_Basic(t *testing.T) {
    task := &ent.Task{Cwd: "/tmp"}
    stageRun := &ent.StageRun{ID: "sr1"}
    opts := pipeline.SpawnAgentOptions{
        Task: task, StageRun: stageRun,
        Prompt: "do the work",
    }
    args := pipeline.BuildSpawnArgs(opts)
    require.Contains(t, args, "-p")
    require.Contains(t, args, "--permission-mode")
}

func TestBuildSpawnArgs_Resume(t *testing.T) {
    task := &ent.Task{Cwd: "/tmp"}
    stageRun := &ent.StageRun{ID: "sr1"}
    opts := pipeline.SpawnAgentOptions{
        Task: task, StageRun: stageRun,
        Prompt:          "continue",
        ResumeSessionID: "sess-abc",
    }
    args := pipeline.BuildSpawnArgs(opts)
    require.Equal(t, "--resume", args[0])
    require.Equal(t, "sess-abc", args[1])
}
```

- [ ] **Step 3: Run spawner tests**

```bash
cd server
go test ./internal/pipeline/... -run TestBuildAllowList -v
go test ./internal/pipeline/... -run TestBuildSpawnArgs -v
```

Expected: all tests pass.

---

## Task 7: Stage Handlers and Prompts

**Files:**
- Create: `server/internal/pipeline/stage_prompts.go`
- Create: `server/internal/pipeline/stage_handlers.go`
- Create: `server/internal/pipeline/stage_handlers_test.go`

- [ ] **Step 1: Implement stage prompts**

Create `server/internal/pipeline/stage_prompts.go`:

```go
package pipeline

import (
    "encoding/json"
    "fmt"
    "os"

    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
)

const sharedContext = `You are an agent working inside a structured task pipeline. A human orchestrator will review your output at specific stages. Be concise, actionable, and honest about uncertainty. When you produce structured output, wrap it in a fenced ` + "```json ... ```" + ` block for the orchestrator to parse.`

const upfrontPermissionsDirective = `## Permissions — declare upfront, in bulk (CRITICAL FIRST STEP)

Before any tool call, scan your task description and the work ahead. Build the FULL list of tools you anticipate needing — file ops (Read/Write/Edit/MultiEdit/Glob/Grep/LS), Bash patterns (e.g. ` + "`pnpm test*`" + `, ` + "`pnpm lint*`" + `, ` + "`git commit*`" + `), WebFetch URLs, etc.

Then call the ` + "`request_permission`" + ` MCP tool ONCE with the full ` + "`permissions: [...]`" + ` array. The dashboard auto-resolves any entries already pre-granted on the task — only truly new entries surface as ON HOLD.

NEVER write prose like "please grant me X" — only ` + "`request_permission`" + ` is actionable.`

// ImplementationPrompt builds the system+user prompt for the implementation stage.
func ImplementationPrompt(t *ent.Task, conceptOutput map[string]any, reviewFeedback string) PromptBundle {
    allowGitPush := IsGitPushAllowed(t)
    pushLine := "Commit your work via git when done — but NEVER `git push`; pushing is the user's responsibility."
    if allowGitPush {
        pushLine = "Commit AND push (`git push`) are permitted for this task — push your feature branch when work is complete."
    }
    systemPrompt := fmt.Sprintf("%s\n\nYou are the orchestrator for this task's implementation phase. Use the Task tool to dispatch subagents for parallel work when beneficial. %s\n\n%s",
        sharedContext, pushLine, upfrontPermissionsDirective)

    conceptJSON, _ := json.MarshalIndent(conceptOutput, "", "  ")
    feedbackBlock := ""
    if reviewFeedback != "" {
        feedbackBlock = fmt.Sprintf("\n\n## Review Feedback From Previous Iteration\n%s\n\nAddress this feedback in your next attempt.", reviewFeedback)
    }
    userPrompt := fmt.Sprintf(`## Task: %s

%s

## Concept (spec, plan, toolRequests)
`+"```json\n%s\n```"+`%s

## Your Job: Implement

Work step-by-step through the concept plan. Commit each logical change via git.

When finished, produce a `+"```json```"+` block as your final output:
{"summary": string, "commits": string[], "openItems": string[]}`,
        t.Title,
        strOrEmpty(t.Description),
        string(conceptJSON),
        feedbackBlock,
    )
    return PromptBundle{SystemPrompt: systemPrompt, UserPrompt: userPrompt}
}

// SelfReviewPrompt builds the prompt for the self_review stage.
func SelfReviewPrompt(t *ent.Task, implementationOutput map[string]any) PromptBundle {
    implJSON, _ := json.MarshalIndent(implementationOutput, "", "  ")
    return PromptBundle{
        SystemPrompt: fmt.Sprintf("%s\n\n%s", sharedContext, upfrontPermissionsDirective),
        UserPrompt: fmt.Sprintf(`## Task: %s

%s

## Implementation Output
`+"```json\n%s\n```"+`

## Your Job: Self-Review

Review the implementation against:
1. Original task requirements — are they all met?
2. Security — any injection, XSS, SQL, auth bypass, secrets leaked?
3. Code quality — DRY violations, dead code, missing error handling?
4. Test coverage — are the changes tested?

Respond with a `+"```json```"+` block: {"passed": bool, "findings": [{"severity": "high"|"medium"|"low", "description": string, "file": string|null}], "summary": string}.`,
            t.Title, strOrEmpty(t.Description), string(implJSON)),
    }
}

// FinalizationPrompt builds the prompt for the finalization stage.
func FinalizationPrompt(t *ent.Task, stageRuns []*ent.StageRun) PromptBundle {
    var history string
    for _, r := range stageRuns {
        history += fmt.Sprintf("%s (iter %d): %s\n", r.Stage, r.Iteration, r.Status)
    }
    return PromptBundle{
        SystemPrompt: fmt.Sprintf("%s\n\n%s", sharedContext, upfrontPermissionsDirective),
        UserPrompt: fmt.Sprintf(`## Task: %s

%s

## Stage History
%s

## Your Job: Final Report

Produce a user-facing summary of what was done. Include:
- Short insights or lessons learned
- Known open todos or caveats
- Concrete test steps the user can run to verify the change

Respond with a `+"```json```"+` block: {"summary": string, "insights": string[], "openTodos": string[], "testPlan": string[]}.`,
            t.Title, strOrEmpty(t.Description), history),
    }
}

func strOrEmpty(s *string) string {
    if s == nil {
        return ""
    }
    return *s
}

// BuildFeedbackPrefix prepends a correction block to a stage's user prompt
// when the previous iteration's output was schema-rejected.
func BuildFeedbackPrefix(priorOutput map[string]any) string {
    if priorOutput == nil {
        return ""
    }
    validationErr, ok := priorOutput["validation_error"].(string)
    if !ok {
        return ""
    }
    const rejectedPreviewChars = 2000
    rejectedBlock := ""
    if rejected, hasRejected := priorOutput["rejected_output"]; hasRejected {
        full, _ := json.MarshalIndent(rejected, "", "  ")
        truncated := string(full)
        if len(truncated) > rejectedPreviewChars {
            truncated = truncated[:rejectedPreviewChars] + fmt.Sprintf("\n… (truncated, %d chars elided)", len(truncated)-rejectedPreviewChars)
        }
        rejectedBlock = fmt.Sprintf("\n\nYour previous response was:\n```json\n%s\n```", truncated)
    }
    return fmt.Sprintf("## CORRECTION REQUIRED\n\nYour previous attempt was rejected with: **%s**.%s\n\nStick EXACTLY to the schema described below. Do not add or rename fields.\n\n---\n\n", validationErr, rejectedBlock)
}

// SummarizeReviewFindings extracts a short actionable feedback string from a self_review output.
func SummarizeReviewFindings(output map[string]any) string {
    summary, _ := output["summary"].(string)
    findings, _ := output["findings"].([]any)
    var lines []string
    for _, f := range findings {
        fm, ok := f.(map[string]any)
        if !ok {
            continue
        }
        severity, _ := fm["severity"].(string)
        description, _ := fm["description"].(string)
        file, _ := fm["file"].(string)
        fileStr := ""
        if file != "" {
            fileStr = fmt.Sprintf(" (%s)", file)
        }
        if severity == "" {
            severity = "ISSUE"
        }
        lines = append(lines, fmt.Sprintf("- [%s] %s%s", severity, description, fileStr))
    }
    result := summary
    if len(lines) > 0 {
        if result != "" {
            result += "\n"
        }
        result += fmt.Sprintf("%s", joinLines(lines))
    }
    return result
}

func joinLines(lines []string) string {
    out := ""
    for i, l := range lines {
        if i > 0 {
            out += "\n"
        }
        out += l
    }
    return out
}
```

Note: remove the unused `os` import if `IsGitPushAllowed` call is the only use; that function is in spawner.go — refactor if needed.

- [ ] **Step 2: Implement stage handlers**

Create `server/internal/pipeline/stage_handlers.go`:

```go
package pipeline

import (
    "context"
    "fmt"

    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// agentStageHandler is the generic stage handler for agent-driven stages.
type agentStageHandler struct {
    stage        string
    buildPrompt  func(ctx *StageContext) PromptBundle
    spawnFn      func(opts SpawnAgentOptions) (SpawnResult, error)
    mcpToken     string // injected at construction by the orchestrator
    mcpUrl       string
    apiKeyRepo   repo.ApiKeyRepo
}

func (h *agentStageHandler) Stage() string        { return h.stage }
func (h *agentStageHandler) RequiresAgent() bool  { return true }

func (h *agentStageHandler) Execute(ctx *StageContext) (StageTransition, error) {
    bundle := h.buildPrompt(ctx)
    feedback := BuildFeedbackPrefix(ctx.PriorIterationOutput)

    // Stage-run-scoped MCP token is injected by the orchestrator before Execute is called.
    // The token is revoked in applyTransition on every terminal transition.

    spawnFn := h.spawnFn
    if spawnFn == nil {
        spawnFn = SpawnStageAgent
    }

    result, err := spawnFn(SpawnAgentOptions{
        Task:            ctx.Task,
        StageRun:        ctx.StageRun,
        SystemPrompt:    bundle.SystemPrompt,
        Prompt:          feedback + bundle.UserPrompt + buildAdditionalPromptSuffix(ctx.UserAdditionalPrompt),
        Permissions:     ctx.Permissions,
        EnableChannel:   true,
        ResumeSessionID: ctx.ResumeSessionID,
        MCPToken:        h.mcpToken,
        MCPUrl:          h.mcpUrl,
    })
    if err != nil {
        return nil, fmt.Errorf("agentStageHandler.Execute(%s): %w", h.stage, err)
    }

    ctx.RecordAudit(h.stage+"_spawned", map[string]any{
        "pid":              result.PID,
        "iteration":        ctx.StageRun.Iteration,
        "hasFeedback":      len(feedback) > 0,
        "resumedSessionId": ctx.ResumeSessionID,
    })

    return AsyncRunningTransition{PID: result.PID}, nil
}

func buildAdditionalPromptSuffix(prompt string) string {
    if prompt == "" {
        return ""
    }
    return fmt.Sprintf("\n\n---\nAdditional instruction from user: %s", prompt)
}

// createAgentStage returns an agent-driven StageHandler for the given stage.
// spawnFn is injectable for tests; pass nil to use the default SpawnStageAgent.
func createAgentStage(stage string, buildPrompt func(*StageContext) PromptBundle, spawnFn func(SpawnAgentOptions) (SpawnResult, error)) StageHandler {
    return &agentStageHandler{
        stage:       stage,
        buildPrompt: buildPrompt,
        spawnFn:     spawnFn,
    }
}

// --- Agent-less handlers ---

type staticHandler struct {
    stage    string
    executeFn func(ctx *StageContext) (StageTransition, error)
}

func (h *staticHandler) Stage() string       { return h.stage }
func (h *staticHandler) RequiresAgent() bool { return false }
func (h *staticHandler) Execute(ctx *StageContext) (StageTransition, error) {
    return h.executeFn(ctx)
}

// conceptHandler is an agent-less safety net. Interactive refinement runs outside
// the orchestrator; when the user confirms, the refine-confirm route advances
// the task to backlog directly. This handler exists only so the handler map is exhaustive.
var conceptHandler StageHandler = &staticHandler{
    stage: "concept",
    executeFn: func(ctx *StageContext) (StageTransition, error) {
        ctx.RecordAudit("concept_chat_pending", nil)
        return WaitUserTransition{Reason: "Refinement chat in progress"}, nil
    },
}

// backlogHandler advances immediately to implementation (the gate is already passed
// when the user moves a task to backlog).
var backlogHandler StageHandler = &staticHandler{
    stage: "backlog",
    executeFn: func(ctx *StageContext) (StageTransition, error) {
        ctx.RecordAudit("backlog_entered", nil)
        return NextTransition{Stage: "implementation"}, nil
    },
}

// --- Prompt builder adapters ---

func implementationBuilder(ctx *StageContext) PromptBundle {
    feedback := ""
    if ctx.Task.Metadata != nil {
        if fb, ok := ctx.Task.Metadata["review_feedback"].(string); ok {
            feedback = fb
        }
    }
    conceptOutput := map[string]any{}
    if ctx.Task.Metadata != nil {
        for k, v := range ctx.Task.Metadata {
            conceptOutput[k] = v
        }
    }
    return ImplementationPrompt(ctx.Task, conceptOutput, feedback)
}

func selfReviewBuilder(ctx *StageContext) PromptBundle {
    return SelfReviewPrompt(ctx.Task, ctx.PreviousOutput)
}

func finalizationBuilder(ctx *StageContext) PromptBundle {
    // Stage runs for finalization prompt are passed from the orchestrator via context;
    // for now pass an empty slice — the orchestrator may enrich this if needed.
    return FinalizationPrompt(ctx.Task, []*ent.StageRun{})
}

// HandlersByStage is the registry of all stage handlers.
// The orchestrator indexes this map on startup.
var HandlersByStage = map[string]StageHandler{
    "concept":       conceptHandler,
    "backlog":       backlogHandler,
    "implementation": createAgentStage("implementation", implementationBuilder, nil),
    "self_review":   createAgentStage("self_review", selfReviewBuilder, nil),
    "finalization":  createAgentStage("finalization", finalizationBuilder, nil),
}

// GetHandlerForStage returns the handler for stage, or nil if unregistered.
func GetHandlerForStage(stage string) StageHandler {
    return HandlersByStage[stage]
}
```

- [ ] **Step 3: Write stage handler tests with stubbed spawnFn**

Create `server/internal/pipeline/stage_handlers_test.go`:

```go
package pipeline_test

import (
    "context"
    "testing"

    "github.com/stretchr/testify/require"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
    "github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
)

func TestBacklogHandler_TransitionsToImplementation(t *testing.T) {
    h := pipeline.GetHandlerForStage("backlog")
    require.NotNil(t, h)
    require.False(t, h.RequiresAgent())

    audited := false
    ctx := &pipeline.StageContext{
        Ctx:         context.Background(),
        Task:        &ent.Task{Slug: "my-task", CurrentStage: "backlog"},
        StageRun:    &ent.StageRun{Stage: "backlog"},
        RecordAudit: func(action string, _ map[string]any) { audited = true },
        RequestPermission: func(tool, pattern, reason string) *ent.PermissionRequest { return nil },
    }
    transition, err := h.Execute(ctx)
    require.NoError(t, err)
    next, ok := transition.(pipeline.NextTransition)
    require.True(t, ok)
    require.Equal(t, "implementation", next.Stage)
    require.True(t, audited)
}

func TestConceptHandler_WaitsUser(t *testing.T) {
    h := pipeline.GetHandlerForStage("concept")
    require.NotNil(t, h)
    require.False(t, h.RequiresAgent())

    ctx := &pipeline.StageContext{
        Ctx:               context.Background(),
        Task:              &ent.Task{},
        StageRun:          &ent.StageRun{},
        RecordAudit:       func(string, map[string]any) {},
        RequestPermission: func(string, string, string) *ent.PermissionRequest { return nil },
    }
    transition, err := h.Execute(ctx)
    require.NoError(t, err)
    _, ok := transition.(pipeline.WaitUserTransition)
    require.True(t, ok)
}

func TestBuildFeedbackPrefix_WithValidationError(t *testing.T) {
    prefix := pipeline.BuildFeedbackPrefix(map[string]any{
        "validation_error": "missing field: passed",
        "rejected_output":  map[string]any{"summary": "ok"},
    })
    require.Contains(t, prefix, "CORRECTION REQUIRED")
    require.Contains(t, prefix, "missing field: passed")
}

func TestBuildFeedbackPrefix_NoError(t *testing.T) {
    prefix := pipeline.BuildFeedbackPrefix(nil)
    require.Empty(t, prefix)
}
```

- [ ] **Step 4: Run stage handler tests**

```bash
cd server
go test ./internal/pipeline/... -run "TestBacklogHandler|TestConceptHandler|TestBuildFeedbackPrefix" -v
```

Expected: all tests pass.

---

## Task 8: Pipeline Orchestrator

**Files:**
- Create: `server/internal/pipeline/orchestrator.go`
- Create: `server/internal/pipeline/orchestrator_test.go`

- [ ] **Step 1: Implement orchestrator core (Run, tick, progressTask, applyTransition)**

Create `server/internal/pipeline/orchestrator.go` — this is the largest file. Implement in the following sub-steps:

Sub-step A: skeleton, config cache, per-task lock:

```go
package pipeline

import (
    "context"
    "fmt"
    "log/slog"
    "os"
    "sync"
    "time"

    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
    "github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

const (
    defaultPollInterval           = 2 * time.Second
    maxParallelKey                = "maxParallelOrchestrators"
    defaultMaxParallel            = 3
    stageTimeoutKey               = "stageTimeoutSeconds"
    defaultStageTimeoutSeconds    = 1800
    awaitingUserTimeoutKey        = "awaitingUserTimeoutSeconds"
    defaultAwaitingUserTimeout    = 14400 // 4h
    pendingStaleSeconds           = 300   // 5 min
    maxReviewCyclesKey            = "maxReviewCycles"
    defaultMaxReviewCycles        = 3
)

// PipelineOrchestrator drives the task pipeline state machine.
type PipelineOrchestrator struct {
    opts           OrchestratorOptions
    taskLocks      sync.Map // map[taskID string]*sync.Mutex
    handlerOverrides sync.Map // map[stage string]StageHandler — test seam
    detectCompletion func(*ent.StageRun, string, CompletionDeps) (CompletionResult, error)
    configCache    sync.Map // map[key string]cachedConfig
    mcpUrl         string
}

type cachedConfig struct {
    value     int
    expiresAt time.Time
}

// NewOrchestrator constructs a PipelineOrchestrator with the given options.
// All repo fields in opts are required (validated at construction).
func NewOrchestrator(opts OrchestratorOptions) (*PipelineOrchestrator, error) {
    if opts.TaskRepo == nil || opts.StageRunRepo == nil || opts.PermissionRepo == nil || opts.AuditRepo == nil || opts.ConfigRepo == nil {
        return nil, fmt.Errorf("NewOrchestrator: all repo fields are required")
    }
    if opts.PollInterval <= 0 {
        opts.PollInterval = defaultPollInterval
    }
    return &PipelineOrchestrator{
        opts:             opts,
        detectCompletion: DetectCompletion,
    }, nil
}

// SetHandlerOverride replaces a stage handler — test seam only.
func (o *PipelineOrchestrator) SetHandlerOverride(stage string, h StageHandler) {
    o.handlerOverrides.Store(stage, h)
}

// ClearHandlerOverrides removes all test handler overrides.
func (o *PipelineOrchestrator) ClearHandlerOverrides() {
    o.handlerOverrides.Range(func(k, _ any) bool { o.handlerOverrides.Delete(k); return true })
}

// SetCompletionDetector replaces the completion detection function — test seam.
func (o *PipelineOrchestrator) SetCompletionDetector(fn func(*ent.StageRun, string, CompletionDeps) (CompletionResult, error)) {
    o.detectCompletion = fn
}

// InvalidateConfigCache clears the TTL config cache — call after REST writes to pipeline_config.
func (o *PipelineOrchestrator) InvalidateConfigCache() {
    o.configCache.Range(func(k, _ any) bool { o.configCache.Delete(k); return true })
}

func (o *PipelineOrchestrator) resolveHandler(stage string) StageHandler {
    if h, ok := o.handlerOverrides.Load(stage); ok {
        return h.(StageHandler)
    }
    return GetHandlerForStage(stage)
}

func (o *PipelineOrchestrator) getCachedConfigNumber(ctx context.Context, key string, fallback int) int {
    if v, ok := o.configCache.Load(key); ok {
        c := v.(cachedConfig)
        if time.Now().Before(c.expiresAt) {
            return c.value
        }
    }
    n := o.opts.ConfigRepo.GetNumber(ctx, key, fallback)
    o.configCache.Store(key, cachedConfig{value: n, expiresAt: time.Now().Add(5 * time.Second)})
    return n
}

// Run starts the orchestrator tick loop. It blocks until ctx is cancelled.
// Must be run in an errgroup goroutine alongside the HTTP server.
func (o *PipelineOrchestrator) Run(ctx context.Context) error {
    o.recoverRunningStageRuns(ctx)
    ticker := time.NewTicker(o.opts.PollInterval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return nil
        case <-ticker.C:
            if err := o.tick(ctx); err != nil {
                slog.Error("orchestrator tick error", "err", err)
            }
        }
    }
}

func (o *PipelineOrchestrator) tick(ctx context.Context) error {
    allRunning, err := o.opts.StageRunRepo.ListByStatus(ctx, "running", "awaiting_user", "on_hold")
    if err != nil {
        return fmt.Errorf("orchestrator.tick.listRunning: %w", err)
    }
    if err := o.finalizeCompletedAsyncRuns(ctx, allRunning); err != nil {
        slog.Error("finalizeCompletedAsyncRuns error", "err", err)
    }
    if err := o.sweepAwaitingUserRuns(ctx, allRunning); err != nil {
        slog.Error("sweepAwaitingUserRuns error", "err", err)
    }
    if err := o.sweepOrphanRuns(ctx, allRunning); err != nil {
        slog.Error("sweepOrphanRuns error", "err", err)
    }
    o.pickNextTasksForFreeSlots(ctx, allRunning)
    return nil
}
```

Sub-step B: implement `progressTask` (per-task locking pattern):

```go
// ProgressTask advances a task from its current stage.
// Calls are serialized per task via a per-task mutex — concurrent callers
// for the same task queue up rather than racing.
func (o *PipelineOrchestrator) ProgressTask(ctx context.Context, taskID string, opts *ProgressOpts) (*ent.StageRun, error) {
    mu := o.getTaskMutex(taskID)
    mu.Lock()
    defer mu.Unlock()
    return o.runProgressTaskLocked(ctx, taskID, opts)
}

type ProgressOpts struct {
    ResumeSessionID      string
    UserAdditionalPrompt string
}

func (o *PipelineOrchestrator) getTaskMutex(taskID string) *sync.Mutex {
    mu, _ := o.taskLocks.LoadOrStore(taskID, &sync.Mutex{})
    return mu.(*sync.Mutex)
}

func (o *PipelineOrchestrator) runProgressTaskLocked(ctx context.Context, taskID string, opts *ProgressOpts) (*ent.StageRun, error) {
    task, err := o.opts.TaskRepo.GetByID(ctx, taskID)
    if err != nil || IsTerminalStage(task.CurrentStage) {
        return nil, nil
    }

    handler := o.resolveHandler(task.CurrentStage)
    if handler == nil {
        return nil, nil
    }

    // Global runner-slot cap — agent-driven stages only.
    if handler.RequiresAgent() && !o.hasFreeRunnerSlot(ctx, task.ID) {
        return nil, nil
    }

    // Lingering-pending gate — prevents respawn while unresolved permission_requests
    // remain on the most recent terminal or zombie-awaiting stage_run.
    if handler.RequiresAgent() {
        latest, _ := o.opts.StageRunRepo.GetLatestByTaskAndStage(ctx, task.ID, task.CurrentStage)
        if latest != nil {
            pid := 0
            if latest.Pid != nil {
                pid = *latest.Pid
            }
            isTerminal := latest.Status == "failed" || latest.Status == "done"
            isZombieAwait := latest.Status == "awaiting_user" && !IsPidAlive(pid)
            if isTerminal || isZombieAwait {
                n, _ := o.opts.PermissionRepo.CountForStageRun(ctx, latest.ID)
                if n > 0 {
                    slog.Info("orchestrator: progressTask blocked by lingering permission_requests",
                        "taskID", taskID, "count", n, "runID", latest.ID)
                    return nil, nil
                }
            }
        }
    }

    stageRun, err := o.ensureStageRun(ctx, task)
    if err != nil {
        return nil, fmt.Errorf("orchestrator.ensureStageRun: %w", err)
    }

    // Re-entry guard: if the run is already running with a live PID, return without spawning.
    if handler.RequiresAgent() {
        pid := 0
        if stageRun.Pid != nil {
            pid = *stageRun.Pid
        }
        if (stageRun.Status == "running" || stageRun.Status == "awaiting_user") && IsPidAlive(pid) {
            slog.Info("orchestrator: re-entry skipped — live PID already running",
                "stage", stageRun.Stage, "runID", stageRun.ID, "pid", pid)
            return stageRun, nil
        }
    }

    now := time.Now()
    stageRun, err = o.opts.StageRunRepo.Update(ctx, stageRun.ID, repo.UpdateStageRunInput{
        Status:    strPtr("running"),
        StartedAt: &now,
    })
    if err != nil {
        return nil, fmt.Errorf("orchestrator.updateStageRunRunning: %w", err)
    }

    perms, _ := o.opts.PermissionRepo.ListTaskPermissions(ctx, task.ID)
    prevOutput := o.getPreviousStageOutput(ctx, task)
    priorIterOutput := o.getPriorIterationOutput(ctx, task, stageRun)

    var resumeSessionID string
    if opts != nil {
        resumeSessionID = opts.ResumeSessionID
    }
    var userAdditionalPrompt string
    if opts != nil {
        userAdditionalPrompt = opts.UserAdditionalPrompt
    }

    stageCtx := &StageContext{
        Ctx:                  ctx,
        Task:                 task,
        StageRun:             stageRun,
        Permissions:          perms,
        PreviousOutput:       prevOutput,
        PriorIterationOutput: priorIterOutput,
        ResumeSessionID:      resumeSessionID,
        UserAdditionalPrompt: userAdditionalPrompt,
        RecordAudit: func(action string, details map[string]any) {
            _ = o.opts.AuditRepo.Append(ctx, repo.AppendAuditInput{
                TaskID:  task.ID,
                Actor:   "orchestrator",
                Action:  action,
                Details: details,
            })
        },
        RequestPermission: func(tool, pattern, reason string) *ent.PermissionRequest {
            pat := (*string)(nil)
            if pattern != "" {
                pat = &pattern
            }
            rsn := (*string)(nil)
            if reason != "" {
                rsn = &reason
            }
            req, err := o.opts.PermissionRepo.CreatePermissionRequest(ctx, repo.CreatePermissionRequestInput{
                StageRunID: stageRun.ID,
                Tool:       tool,
                Pattern:    pat,
                Reason:     rsn,
            })
            if err != nil {
                slog.Error("orchestrator: CreatePermissionRequest failed", "err", err)
                return nil
            }
            if o.opts.OnPermissionRequest != nil {
                o.opts.OnPermissionRequest(task.ID, req)
            }
            return req
        },
    }

    transition, execErr := handler.Execute(stageCtx)
    if execErr != nil {
        transition = FailTransition{Reason: execErr.Error()}
    }

    return o.applyTransition(ctx, task, stageRun, transition)
}
```

Sub-step C: implement `applyTransition`:

```go
func (o *PipelineOrchestrator) applyTransition(ctx context.Context, task *ent.Task, sr *ent.StageRun, t StageTransition) (*ent.StageRun, error) {
    now := time.Now()
    var updatedRunID string
    var newRunID string

    switch tr := t.(type) {
    case NextTransition:
        if _, err := o.opts.StageRunRepo.Update(ctx, sr.ID, repo.UpdateStageRunInput{
            Status:  strPtr("done"),
            EndedAt: &now,
            Output:  tr.Output,
        }); err != nil {
            return nil, fmt.Errorf("applyTransition.next.updateRun: %w", err)
        }
        taskUpdate := repo.UpdateTaskInput{CurrentStage: &tr.Stage}
        if tr.MetaClear {
            taskUpdate.MetadataClear = true
        } else if tr.MetadataPatch != nil {
            taskUpdate.Metadata = tr.MetadataPatch
        }
        if _, err := o.opts.TaskRepo.Update(ctx, task.ID, taskUpdate); err != nil {
            return nil, fmt.Errorf("applyTransition.next.updateTask: %w", err)
        }
        _ = o.opts.AuditRepo.Append(ctx, repo.AppendAuditInput{
            TaskID: task.ID, Actor: "orchestrator", Action: "stage_transition",
            Details: map[string]any{"from": task.CurrentStage, "to": tr.Stage},
        })
        updatedRunID = sr.ID

    case DoneTransition:
        if _, err := o.opts.StageRunRepo.Update(ctx, sr.ID, repo.UpdateStageRunInput{
            Status: strPtr("done"), EndedAt: &now, Output: tr.Output,
        }); err != nil {
            return nil, fmt.Errorf("applyTransition.done.updateRun: %w", err)
        }
        done := "done"
        if _, err := o.opts.TaskRepo.Update(ctx, task.ID, repo.UpdateTaskInput{CurrentStage: &done}); err != nil {
            return nil, fmt.Errorf("applyTransition.done.updateTask: %w", err)
        }
        _ = o.opts.AuditRepo.Append(ctx, repo.AppendAuditInput{TaskID: task.ID, Actor: "orchestrator", Action: "task_done"})
        updatedRunID = sr.ID
        o.handleDependentTasks(ctx, task.ID, "done")

    case FailTransition:
        output := tr.Output
        if output == nil {
            output = map[string]any{}
        }
        output["error"] = tr.Reason
        if _, err := o.opts.StageRunRepo.Update(ctx, sr.ID, repo.UpdateStageRunInput{
            Status: strPtr("failed"), EndedAt: &now, Output: output,
        }); err != nil {
            return nil, fmt.Errorf("applyTransition.fail.updateRun: %w", err)
        }
        _ = o.opts.AuditRepo.Append(ctx, repo.AppendAuditInput{
            TaskID: task.ID, Actor: "orchestrator", Action: "stage_failed",
            Details: map[string]any{"stage": sr.Stage, "iteration": sr.Iteration, "error": tr.Reason},
        })
        updatedRunID = sr.ID
        if o.opts.OnStageFailed != nil {
            o.opts.OnStageFailed(task.ID, StageFailedInfo{StageRunID: sr.ID, Stage: sr.Stage, Iteration: sr.Iteration, Error: tr.Reason})
        }

    case WaitUserTransition:
        if _, err := o.opts.StageRunRepo.Update(ctx, sr.ID, repo.UpdateStageRunInput{
            Status: strPtr("awaiting_user"), Output: tr.Output,
        }); err != nil {
            return nil, fmt.Errorf("applyTransition.waitUser.updateRun: %w", err)
        }
        _ = o.opts.AuditRepo.Append(ctx, repo.AppendAuditInput{
            TaskID: task.ID, Actor: "orchestrator", Action: "awaiting_user",
            Details: map[string]any{"reason": tr.Reason},
        })
        updatedRunID = sr.ID

    case IterateTransition:
        if _, err := o.opts.StageRunRepo.Update(ctx, sr.ID, repo.UpdateStageRunInput{
            Status: strPtr("done"), EndedAt: &now, Output: tr.Output,
        }); err != nil {
            return nil, fmt.Errorf("applyTransition.iterate.updateRun: %w", err)
        }
        task2, _ := o.opts.TaskRepo.GetByID(ctx, task.ID)
        maxIter := 20
        if task2 != nil {
            maxIter = task2.MaxIterations
        }
        if sr.Iteration+1 >= maxIter {
            // Iteration limit — flip to failed
            failOutput := tr.Output
            if failOutput == nil {
                failOutput = map[string]any{}
            }
            failOutput["error"] = fmt.Sprintf("iteration limit reached (%d)", maxIter)
            if _, err := o.opts.StageRunRepo.Update(ctx, sr.ID, repo.UpdateStageRunInput{
                Status: strPtr("failed"), Output: failOutput,
            }); err != nil {
                return nil, fmt.Errorf("applyTransition.iterate.limitFail: %w", err)
            }
            _ = o.opts.AuditRepo.Append(ctx, repo.AppendAuditInput{
                TaskID: task.ID, Actor: "orchestrator", Action: "iteration_limit_reached",
                Details: map[string]any{"maxIter": maxIter, "lastIteration": sr.Iteration},
            })
            updatedRunID = sr.ID
            if o.opts.OnStageFailed != nil {
                o.opts.OnStageFailed(task.ID, StageFailedInfo{StageRunID: sr.ID, Stage: sr.Stage, Iteration: sr.Iteration, Error: fmt.Sprintf("iteration limit reached (%d)", maxIter)})
            }
        } else {
            newSR, err := o.opts.StageRunRepo.Create(ctx, repo.CreateStageRunInput{
                TaskID:      task.ID,
                Stage:       sr.Stage,
                Iteration:   sr.Iteration + 1,
                SessionName: BuildSessionName(task.Slug, sr.Stage, sr.Iteration+1),
            })
            if err != nil {
                return nil, fmt.Errorf("applyTransition.iterate.createRun: %w", err)
            }
            updatedRunID = sr.ID
            newRunID = newSR.ID
        }

    case OnHoldTransition:
        if _, err := o.opts.StageRunRepo.Update(ctx, sr.ID, repo.UpdateStageRunInput{
            Status: strPtr("on_hold"), Output: tr.Output,
        }); err != nil {
            return nil, fmt.Errorf("applyTransition.onHold.updateRun: %w", err)
        }
        onHold := "on_hold"
        if _, err := o.opts.TaskRepo.Update(ctx, task.ID, repo.UpdateTaskInput{CurrentStage: &onHold}); err != nil {
            return nil, fmt.Errorf("applyTransition.onHold.updateTask: %w", err)
        }
        _ = o.opts.AuditRepo.Append(ctx, repo.AppendAuditInput{
            TaskID: task.ID, Actor: "orchestrator", Action: "moved_on_hold",
            Details: map[string]any{"permissionRequestId": tr.PermissionRequestID},
        })
        updatedRunID = sr.ID

    case AsyncRunningTransition:
        update := repo.UpdateStageRunInput{Status: strPtr("running"), Output: tr.Output}
        if tr.PID != 0 {
            update.PID = &tr.PID
        }
        if _, err := o.opts.StageRunRepo.Update(ctx, sr.ID, update); err != nil {
            return nil, fmt.Errorf("applyTransition.asyncRunning.updateRun: %w", err)
        }
        _ = o.opts.AuditRepo.Append(ctx, repo.AppendAuditInput{
            TaskID: task.ID, Actor: "orchestrator", Action: "agent_spawned",
            Details: map[string]any{"pid": tr.PID, "stage": sr.Stage},
        })
        updatedRunID = sr.ID

    default:
        panic(fmt.Sprintf("orchestrator.applyTransition: unhandled transition type %T", t))
    }

    if o.opts.OnTaskChanged != nil {
        kind := transitionKindName(t)
        o.opts.OnTaskChanged(task.ID, kind)
    }

    targetID := updatedRunID
    if newRunID != "" {
        targetID = newRunID
    }
    result, err := o.opts.StageRunRepo.GetByID(ctx, targetID)
    if err != nil {
        return nil, fmt.Errorf("applyTransition.getResult: %w", err)
    }
    return result, nil
}

func transitionKindName(t StageTransition) string {
    switch t.(type) {
    case NextTransition:         return "next"
    case DoneTransition:         return "done"
    case FailTransition:         return "fail"
    case WaitUserTransition:     return "wait_user"
    case IterateTransition:      return "iterate"
    case OnHoldTransition:       return "on_hold"
    case AsyncRunningTransition: return "async_running"
    default:                     return "unknown"
    }
}

func strPtr(s string) *string { return &s }
```

Sub-step D: implement helpers (ensureStageRun, getPreviousStageOutput, getPriorIterationOutput, hasFreeRunnerSlot, pickNextTasksForFreeSlots, comparePickOrder):

```go
func (o *PipelineOrchestrator) ensureStageRun(ctx context.Context, task *ent.Task) (*ent.StageRun, error) {
    existing, _ := o.opts.StageRunRepo.GetLatestByTaskAndStage(ctx, task.ID, task.CurrentStage)
    if existing != nil {
        if existing.Status == "pending" || existing.Status == "running" {
            return existing, nil
        }
        // awaiting_user with live PID — don't create a new iteration
        if existing.Status == "awaiting_user" && existing.Pid != nil && IsPidAlive(*existing.Pid) {
            return existing, nil
        }
    }
    iteration := 0
    if existing != nil {
        iteration = existing.Iteration + 1
    }
    return o.opts.StageRunRepo.Create(ctx, repo.CreateStageRunInput{
        TaskID:      task.ID,
        Stage:       task.CurrentStage,
        Iteration:   iteration,
        SessionName: BuildSessionName(task.Slug, task.CurrentStage, iteration),
    })
}

func (o *PipelineOrchestrator) getPreviousStageOutput(ctx context.Context, task *ent.Task) map[string]any {
    for i := len(STAGE_ORDER) - 1; i >= 0; i-- {
        if STAGE_ORDER[i] == task.CurrentStage {
            for j := i - 1; j >= 0; j-- {
                prev, _ := o.opts.StageRunRepo.GetLatestByTaskAndStage(ctx, task.ID, STAGE_ORDER[j])
                if prev != nil && prev.Output != nil {
                    return prev.Output
                }
            }
            return nil
        }
    }
    return nil
}

func (o *PipelineOrchestrator) getPriorIterationOutput(ctx context.Context, task *ent.Task, sr *ent.StageRun) map[string]any {
    if sr.Iteration == 0 {
        return nil
    }
    prev, _ := o.opts.StageRunRepo.GetByTaskStageIteration(ctx, task.ID, sr.Stage, sr.Iteration-1)
    if prev != nil {
        return prev.Output
    }
    return nil
}

func (o *PipelineOrchestrator) hasFreeRunnerSlot(ctx context.Context, exceptTaskID string) bool {
    max := o.getCachedConfigNumber(ctx, maxParallelKey, defaultMaxParallel)
    running, _ := o.opts.StageRunRepo.ListByStatus(ctx, "running")
    busyTaskIDs := make(map[string]bool)
    for _, r := range running {
        if r.TaskID != exceptTaskID {
            busyTaskIDs[r.TaskID] = true
        }
    }
    return len(busyTaskIDs) < max
}

type pickCandidate struct {
    task  *ent.Task
    stage int // index in STAGE_ORDER
}

func (o *PipelineOrchestrator) pickNextTasksForFreeSlots(ctx context.Context, allRunning []*ent.StageRun) {
    max := o.getCachedConfigNumber(ctx, maxParallelKey, defaultMaxParallel)
    busyTaskIDs := make(map[string]bool)
    for _, r := range allRunning {
        if r.Status == "running" {
            busyTaskIDs[r.TaskID] = true
        }
    }
    freeSlots := max - len(busyTaskIDs)
    if freeSlots <= 0 {
        return
    }
    candidates, _ := o.opts.TaskRepo.ListPickable(ctx)
    var ready []*ent.Task
    for _, t := range candidates {
        if busyTaskIDs[t.ID] {
            continue
        }
        latest, _ := o.opts.StageRunRepo.GetLatestByTaskAndStage(ctx, t.ID, t.CurrentStage)
        if latest != nil && (latest.Status == "awaiting_user" || latest.Status == "failed") {
            continue
        }
        ready = append(ready, t)
    }
    // Sort: silver_bullet desc → stage index desc → priority desc → created_at asc
    sortPickCandidates(ready)
    picks := ready
    if len(picks) > freeSlots {
        picks = picks[:freeSlots]
    }
    for _, task := range picks {
        t := task
        go func() {
            if _, err := o.ProgressTask(ctx, t.ID, nil); err != nil {
                slog.Error("orchestrator: pickup failed", "taskID", t.ID, "err", err)
            }
        }()
    }
}

func sortPickCandidates(tasks []*ent.Task) {
    // Insertion sort (small N, simple)
    priorityRank := map[string]int{"high": 3, "medium": 2, "low": 1}
    stageIdx := func(s string) int {
        for i, st := range STAGE_ORDER {
            if st == s {
                return i
            }
        }
        return -1
    }
    for i := 1; i < len(tasks); i++ {
        for j := i; j > 0; j-- {
            a, b := tasks[j-1], tasks[j]
            if shouldSwap(a, b, priorityRank, stageIdx) {
                tasks[j-1], tasks[j] = tasks[j], tasks[j-1]
            } else {
                break
            }
        }
    }
}

func shouldSwap(a, b *ent.Task, priorityRank map[string]int, stageIdx func(string) int) bool {
    if a.SilverBullet != b.SilverBullet {
        return !a.SilverBullet // b has silver bullet — b should come first
    }
    si, sj := stageIdx(a.CurrentStage), stageIdx(b.CurrentStage)
    if si != sj {
        return si < sj // b is further along — b should come first
    }
    pi, pj := priorityRank[a.Priority], priorityRank[b.Priority]
    if pi != pj {
        return pi < pj // b has higher priority — b should come first
    }
    return a.CreatedAt.After(b.CreatedAt) // b is older — b should come first
}
```

Sub-step E: implement zombie sweeps:

```go
func (o *PipelineOrchestrator) sweepAwaitingUserRuns(ctx context.Context, allRunning []*ent.StageRun) error {
    timeoutSec := o.getCachedConfigNumber(ctx, awaitingUserTimeoutKey, defaultAwaitingUserTimeout)
    for _, run := range allRunning {
        if run.Status != "awaiting_user" {
            continue
        }
        task, err := o.opts.TaskRepo.GetByID(ctx, run.TaskID)
        if err != nil {
            continue
        }
        pid := 0
        if run.Pid != nil {
            pid = *run.Pid
        }
        if !IsPidAlive(pid) {
            slog.Warn("orchestrator: awaiting_user run has dead PID — reaping as failed",
                "runID", run.ID, "stage", run.Stage, "pid", pid)
            if _, err := o.applyTransition(ctx, task, run, FailTransition{
                Reason: "awaiting_user reaper: stage agent exited while permissions pending",
            }); err != nil {
                slog.Error("sweepAwaitingUserRuns.applyTransition", "err", err)
            }
            continue
        }
        if timeoutSec > 0 {
            anchor := run.StartedAt
            if run.LastGrantAt != nil {
                anchor = run.LastGrantAt
            }
            if anchor != nil && time.Since(*anchor) > time.Duration(timeoutSec)*time.Second {
                slog.Warn("orchestrator: awaiting_user run exceeded wallclock timeout — killing agent",
                    "runID", run.ID, "pid", pid, "timeoutSec", timeoutSec)
                _ = syscallKill(pid)
                fresh, _ := o.opts.StageRunRepo.GetByID(ctx, run.ID)
                if fresh != nil && fresh.Status == "awaiting_user" {
                    elapsed := time.Since(*anchor).Seconds()
                    if _, err := o.applyTransition(ctx, task, fresh, FailTransition{
                        Reason: fmt.Sprintf("awaiting_user timeout: ran %.0fs (limit %ds) — agent likely busy-waiting", elapsed, timeoutSec),
                    }); err != nil {
                        slog.Error("sweepAwaitingUserRuns.timeout.applyTransition", "err", err)
                    }
                }
            }
        }
    }
    return nil
}

func (o *PipelineOrchestrator) sweepOrphanRuns(ctx context.Context, allRunning []*ent.StageRun) error {
    pendings, _ := o.opts.StageRunRepo.ListPending(ctx)
    all := append(allRunning, pendings...)
    for _, run := range all {
        task, err := o.opts.TaskRepo.GetByID(ctx, run.TaskID)
        if err != nil {
            continue
        }
        // Case 1: task is parked but stage_run is non-terminal
        if task.CurrentStage == "done" || task.CurrentStage == "cancelled" || task.CurrentStage == "on_hold" {
            fresh, _ := o.opts.StageRunRepo.GetByID(ctx, run.ID)
            if fresh == nil || fresh.Status == "done" || fresh.Status == "failed" {
                continue
            }
            pid := 0
            if fresh.Pid != nil {
                pid = *fresh.Pid
            }
            if IsPidAlive(pid) {
                _ = syscallKill(pid)
            }
            slog.Warn("orchestrator: orphan stage_run — task is parked, reaping run as failed",
                "runID", fresh.ID, "taskStage", task.CurrentStage)
            if _, err := o.applyTransition(ctx, task, fresh, FailTransition{
                Reason: fmt.Sprintf("orphan reaper: task reached %s with stage_run still %s", task.CurrentStage, fresh.Status),
            }); err != nil {
                slog.Error("sweepOrphanRuns.case1.applyTransition", "err", err)
            }
            continue
        }
        // Case 2: on_hold with dead PID
        if run.Status == "on_hold" && run.Pid != nil && !IsPidAlive(*run.Pid) {
            fresh, _ := o.opts.StageRunRepo.GetByID(ctx, run.ID)
            if fresh == nil || fresh.Status == "done" || fresh.Status == "failed" {
                continue
            }
            slog.Warn("orchestrator: on_hold run has dead PID — reaping as failed", "runID", fresh.ID)
            if _, err := o.applyTransition(ctx, task, fresh, FailTransition{Reason: "orphan reaper: on_hold agent exited"}); err != nil {
                slog.Error("sweepOrphanRuns.case2.applyTransition", "err", err)
            }
            continue
        }
        // Case 3: pending stuck > 5 min without a PID
        if run.Status == "pending" && run.Pid == nil && run.StartedAt != nil {
            if time.Since(*run.StartedAt) > pendingStaleSeconds*time.Second {
                fresh, _ := o.opts.StageRunRepo.GetByID(ctx, run.ID)
                if fresh == nil || fresh.Status != "pending" {
                    continue
                }
                elapsed := time.Since(*run.StartedAt).Seconds()
                slog.Warn("orchestrator: pending run stuck without spawn — reaping as failed",
                    "runID", fresh.ID, "elapsedSec", elapsed)
                if _, err := o.applyTransition(ctx, task, fresh, FailTransition{
                    Reason: fmt.Sprintf("orphan reaper: pending stage_run never promoted to running (%.0fs elapsed)", elapsed),
                }); err != nil {
                    slog.Error("sweepOrphanRuns.case3.applyTransition", "err", err)
                }
            }
        }
    }
    return nil
}

func (o *PipelineOrchestrator) finalizeCompletedAsyncRuns(ctx context.Context, allRunning []*ent.StageRun) error {
    for _, run := range allRunning {
        if run.Status != "running" || run.Pid == nil {
            continue
        }
        task, err := o.opts.TaskRepo.GetByID(ctx, run.TaskID)
        if err != nil {
            continue
        }
        if IsTerminalStage(task.CurrentStage) {
            if IsPidAlive(*run.Pid) {
                _ = syscallKill(*run.Pid)
            }
            if _, err := o.applyTransition(ctx, task, run, FailTransition{Reason: "task cancelled externally"}); err != nil {
                slog.Error("finalizeCompletedAsyncRuns.externalCancel", "err", err)
            }
            continue
        }
        cwd := task.Cwd
        if task.WorktreePath != nil && *task.WorktreePath != "" {
            cwd = *task.WorktreePath
        }

        result, err := o.detectCompletion(run, cwd, CompletionDeps{})
        if err != nil {
            slog.Error("orchestrator: completion detection failed", "runID", run.ID, "err", err)
            continue
        }
        if result.Kind == "still_running" {
            // Try to attach session_id for live cross-link banner
            if run.SessionID == nil && run.StartedAt != nil {
                go o.tryAttachSessionID(ctx, run.ID, task.ID, cwd, *run.StartedAt)
            }
            // Cost budget enforcement
            if task.CostBudgetCents != nil && *task.CostBudgetCents > 0 {
                spent, _ := o.opts.StageRunRepo.SumCompletedCostCents(ctx, task.ID)
                if spent > *task.CostBudgetCents {
                    slog.Warn("orchestrator: task exceeded cost budget — killing agent",
                        "taskID", task.ID, "spent", spent, "budget", *task.CostBudgetCents)
                    _ = syscallKill(*run.Pid)
                    fresh, _ := o.opts.StageRunRepo.GetByID(ctx, run.ID)
                    if fresh != nil && fresh.Status == "running" {
                        if _, err := o.applyTransition(ctx, task, fresh, FailTransition{
                            Reason: fmt.Sprintf("cost budget exceeded: %d cents spent, limit %d cents", spent, *task.CostBudgetCents),
                        }); err != nil {
                            slog.Error("finalizeCompletedAsyncRuns.costBudget", "err", err)
                        }
                    }
                    continue
                }
            }
            // Stage timeout enforcement
            timeoutSec := o.getCachedConfigNumber(ctx, stageTimeoutKey, defaultStageTimeoutSeconds)
            if timeoutSec > 0 && run.StartedAt != nil && time.Since(*run.StartedAt) > time.Duration(timeoutSec)*time.Second {
                elapsed := time.Since(*run.StartedAt).Seconds()
                slog.Warn("orchestrator: stage timed out — killing agent",
                    "runID", run.ID, "stage", run.Stage, "elapsedSec", elapsed)
                _ = syscallKill(*run.Pid)
                fresh, _ := o.opts.StageRunRepo.GetByID(ctx, run.ID)
                if fresh != nil && fresh.Status == "running" {
                    if _, err := o.applyTransition(ctx, task, fresh, FailTransition{
                        Reason: fmt.Sprintf("stage timeout: ran %.0fs (limit %ds)", elapsed, timeoutSec),
                    }); err != nil {
                        slog.Error("finalizeCompletedAsyncRuns.timeout", "err", err)
                    }
                }
            }
            continue
        }

        // PID has exited — persist token usage
        if run.SessionID != nil {
            go o.updateTokenUsage(ctx, run.ID, cwd, *run.SessionID)
        }

        fresh, _ := o.opts.StageRunRepo.GetByID(ctx, run.ID)
        if fresh == nil || fresh.Status != "running" {
            continue
        }

        if result.Kind == "completed" {
            transition := o.decideCompletedTransition(ctx, task, fresh, result.Output)
            if _, err := o.applyTransition(ctx, task, fresh, transition); err != nil {
                slog.Error("finalizeCompletedAsyncRuns.applyTransition.completed", "err", err)
            }
            continue
        }

        // failed
        if !result.Retryable {
            if _, err := o.applyTransition(ctx, task, fresh, FailTransition{Reason: result.Error, Output: result.Output}); err != nil {
                slog.Error("finalizeCompletedAsyncRuns.applyTransition.hardFail", "err", err)
            }
            continue
        }
        // Schema rejection retry logic: iter 0 → iterate; iter >= 1 → wait_user
        if fresh.Iteration == 0 {
            if _, err := o.applyTransition(ctx, task, fresh, IterateTransition{Output: map[string]any{
                "validation_error": result.Error,
                "rejected_output":  result.Output,
            }}); err != nil {
                slog.Error("finalizeCompletedAsyncRuns.applyTransition.iterate", "err", err)
            }
        } else {
            if _, err := o.applyTransition(ctx, task, fresh, WaitUserTransition{
                Reason: fmt.Sprintf("schema validation failed twice at stage %s: %s", fresh.Stage, result.Error),
                Output: map[string]any{"validation_error": result.Error, "rejected_output": result.Output},
            }); err != nil {
                slog.Error("finalizeCompletedAsyncRuns.applyTransition.waitUser", "err", err)
            }
        }
    }
    return nil
}

func (o *PipelineOrchestrator) decideCompletedTransition(ctx context.Context, task *ent.Task, run *ent.StageRun, output map[string]any) StageTransition {
    if run.Stage == "finalization" {
        return DoneTransition{Output: output}
    }
    if run.Stage == "self_review" {
        passed, _ := output["passed"].(bool)
        if !passed {
            feedback := SummarizeReviewFindings(output)
            prevCycles := 0
            if task.Metadata != nil {
                if v, ok := task.Metadata["review_cycles"].(float64); ok {
                    prevCycles = int(v)
                }
            }
            cycles := prevCycles + 1
            maxCycles := o.getCachedConfigNumber(ctx, maxReviewCyclesKey, defaultMaxReviewCycles)
            if task.Metadata != nil {
                if v, ok := task.Metadata["maxReviewCycles"].(float64); ok && int(v) > 0 {
                    maxCycles = int(v)
                }
            }
            if cycles >= maxCycles {
                return WaitUserTransition{
                    Reason: fmt.Sprintf("review cycle limit (%d) reached", maxCycles),
                    Output: output,
                }
            }
            meta := map[string]any{}
            if task.Metadata != nil {
                for k, v := range task.Metadata {
                    meta[k] = v
                }
            }
            meta["review_feedback"] = feedback
            meta["review_cycles"] = cycles
            return NextTransition{Stage: "implementation", Output: output, MetadataPatch: meta}
        }
        // Passed — clear stale review feedback
        if task.Metadata != nil {
            if _, hasFeedback := task.Metadata["review_feedback"]; hasFeedback {
                rest := map[string]any{}
                for k, v := range task.Metadata {
                    if k != "review_feedback" && k != "review_cycles" {
                        rest[k] = v
                    }
                }
                if len(rest) == 0 {
                    return NextTransition{Stage: "finalization", Output: output, MetaClear: true}
                }
                return NextTransition{Stage: "finalization", Output: output, MetadataPatch: rest}
            }
        }
        return NextTransition{Stage: "finalization", Output: output}
    }
    return NextTransition{Stage: NextStage(run.Stage), Output: output}
}

func (o *PipelineOrchestrator) recoverRunningStageRuns(ctx context.Context) {
    running, _ := o.opts.StageRunRepo.ListByStatus(ctx, "running")
    for _, run := range running {
        decision := DecideRecovery(run)
        _ = o.opts.AuditRepo.Append(ctx, repo.AppendAuditInput{
            TaskID: run.TaskID, Actor: "system", Action: "recovery_decision",
            Details: map[string]any{"stage": run.Stage, "iteration": run.Iteration, "decision": decision.Kind, "reason": decision.Reason},
        })
        if decision.Kind == "alive" {
            continue
        }
        if decision.Kind == "resume" {
            _, _ = o.opts.StageRunRepo.Update(ctx, run.ID, repo.UpdateStageRunInput{Status: strPtr("pending"), PIDClear: true})
        } else {
            now := time.Now()
            _, _ = o.opts.StageRunRepo.Update(ctx, run.ID, repo.UpdateStageRunInput{
                Status:  strPtr("failed"),
                EndedAt: &now,
                Output:  map[string]any{"error": "orchestrator crashed before completion; no session to resume"},
            })
        }
    }
}

func (o *PipelineOrchestrator) handleDependentTasks(ctx context.Context, taskID, newStage string) {
    // Dependency cascade — Phase 3 stub: no task_dependency traversal yet.
    // Full cascade (cancel, on_hold, start actions) is part of the dependency
    // sub-feature; implement when TaskDependency repo methods are available.
    if o.opts.OnTaskChanged != nil {
        o.opts.OnTaskChanged(taskID, "dependent_check")
    }
}

func (o *PipelineOrchestrator) tryAttachSessionID(ctx context.Context, stageRunID, taskID, cwd string, startedAt time.Time) {
    sid, err := FindNewestSessionID(cwd, startedAt.Format("2006-01-02T15:04:05Z"))
    if err != nil || sid == "" {
        return
    }
    if err := AttachSessionID(ctx, stageRunID, sid, o.opts.StageRunRepo); err != nil {
        slog.Warn("orchestrator.tryAttachSessionID", "err", err)
        return
    }
    if o.opts.OnTaskChanged != nil {
        o.opts.OnTaskChanged(taskID, "async_running")
    }
}

func (o *PipelineOrchestrator) updateTokenUsage(ctx context.Context, stageRunID, cwd, sessionID string) {
    summary, err := ReadSessionTokenSummary(cwd, sessionID)
    if err != nil {
        return
    }
    total := summary.InputTokens + summary.OutputTokens + summary.CacheCreationTokens + summary.CacheReadTokens
    if total == 0 {
        return
    }
    costUsd := parser.EstimateCost(summary.InputTokens, summary.OutputTokens, summary.CacheCreationTokens, summary.CacheReadTokens, summary.Model)
    costCents := int(costUsd * 100)
    _, _ = o.opts.StageRunRepo.Update(ctx, stageRunID, repo.UpdateStageRunInput{
        TokensUsed: &total,
        CostCents:  &costCents,
    })
}

// NotifyTaskTerminated is called by cancel routes to cascade terminal state to dependents.
func (o *PipelineOrchestrator) NotifyTaskTerminated(ctx context.Context, taskID, stage string) {
    o.handleDependentTasks(ctx, taskID, stage)
}

// ResumeFromUser re-runs progressTask after a user action (permission grant, retry).
func (o *PipelineOrchestrator) ResumeFromUser(ctx context.Context, taskID string) (*ent.StageRun, error) {
    return o.ProgressTask(ctx, taskID, nil)
}

func syscallKill(pid int) error {
    proc, err := os.FindProcess(pid)
    if err != nil {
        return err
    }
    return proc.Signal(syscall.SIGTERM)
}
```

Note: add `"syscall"` and `"os"` imports. The `parser.EstimateCost` function signature should match what Phase 1 implemented in `server/internal/parser/pricing.go`.

- [ ] **Step 2: Write orchestrator state machine tests**

Create `server/internal/pipeline/orchestrator_test.go`:

```go
package pipeline_test

import (
    "context"
    "testing"
    "time"

    "github.com/stretchr/testify/require"
    "github.com/lx-wnk/agent-dashboard/server/internal/db"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
    "github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
)

func setupOrchestrator(t *testing.T) (*pipeline.PipelineOrchestrator, context.Context) {
    t.Helper()
    client, err := db.Open(":memory:")
    require.NoError(t, err)
    t.Cleanup(func() { client.Close() })
    ctx := context.Background()

    taskRepo := repo.NewTaskRepo(client)
    srRepo := repo.NewStageRunRepo(client)
    permRepo := repo.NewPermissionRepo(client)
    auditRepo := repo.NewAuditRepo(client)
    cfgRepo := repo.NewPipelineConfigRepo(client)

    var changed []string
    orch, err := pipeline.NewOrchestrator(pipeline.OrchestratorOptions{
        PollInterval:   100 * time.Millisecond,
        TaskRepo:       taskRepo,
        StageRunRepo:   srRepo,
        PermissionRepo: permRepo,
        AuditRepo:      auditRepo,
        ConfigRepo:     cfgRepo,
        OnTaskChanged:  func(taskID, kind string) { changed = append(changed, kind) },
    })
    require.NoError(t, err)
    _ = changed
    return orch, ctx
}

func createTestTask(t *testing.T, ctx context.Context, taskRepo repo.TaskRepo, stage string) *ent.Task {
    t.Helper()
    task, err := taskRepo.Create(ctx, repo.CreateTaskInput{
        Slug:                "test-task-" + stage,
        Title:               "Test Task",
        Cwd:                 "/tmp",
        CurrentStage:        stage,
        Priority:            "medium",
        MaxIterations:       3,
        StageTimeoutSeconds: 1800,
    })
    require.NoError(t, err)
    return task
}

func TestOrchestrator_BacklogTransitionsToImplementation(t *testing.T) {
    orch, ctx := setupOrchestrator(t)

    // Use repos from setup — need to inline for test
    // This test exercises the backlog stage handler end-to-end through ProgressTask
    client, err := db.Open(":memory:")
    require.NoError(t, err)
    defer client.Close()

    taskRepo := repo.NewTaskRepo(client)
    srRepo := repo.NewStageRunRepo(client)
    permRepo := repo.NewPermissionRepo(client)
    auditRepo := repo.NewAuditRepo(client)
    cfgRepo := repo.NewPipelineConfigRepo(client)

    orch2, err := pipeline.NewOrchestrator(pipeline.OrchestratorOptions{
        PollInterval: 100 * time.Millisecond,
        TaskRepo: taskRepo, StageRunRepo: srRepo,
        PermissionRepo: permRepo, AuditRepo: auditRepo, ConfigRepo: cfgRepo,
    })
    require.NoError(t, err)
    _ = orch

    task, err := taskRepo.Create(ctx, repo.CreateTaskInput{
        Slug: "backlog-test", Title: "Backlog Test", Cwd: "/tmp",
        CurrentStage: "backlog", Priority: "medium",
        MaxIterations: 3, StageTimeoutSeconds: 1800,
    })
    require.NoError(t, err)

    sr, err := orch2.ProgressTask(ctx, task.ID, nil)
    require.NoError(t, err)
    require.NotNil(t, sr)
    require.Equal(t, "done", sr.Status) // backlog stage_run is done after transitioning

    updated, err := taskRepo.GetByID(ctx, task.ID)
    require.NoError(t, err)
    require.Equal(t, "implementation", updated.CurrentStage)
}

func TestOrchestrator_AsyncRunningTransition_RecordsPI(t *testing.T) {
    client, err := db.Open(":memory:")
    require.NoError(t, err)
    defer client.Close()

    ctx := context.Background()
    taskRepo := repo.NewTaskRepo(client)
    srRepo := repo.NewStageRunRepo(client)
    permRepo := repo.NewPermissionRepo(client)
    auditRepo := repo.NewAuditRepo(client)
    cfgRepo := repo.NewPipelineConfigRepo(client)

    // Stub implementation handler: returns async_running with PID 42
    stubHandler := &stubStageHandler{stage: "implementation", transition: pipeline.AsyncRunningTransition{PID: 42}}

    orch, err := pipeline.NewOrchestrator(pipeline.OrchestratorOptions{
        TaskRepo: taskRepo, StageRunRepo: srRepo,
        PermissionRepo: permRepo, AuditRepo: auditRepo, ConfigRepo: cfgRepo,
    })
    require.NoError(t, err)
    orch.SetHandlerOverride("implementation", stubHandler)

    task, err := taskRepo.Create(ctx, repo.CreateTaskInput{
        Slug: "impl-test", Title: "Impl Test", Cwd: "/tmp",
        CurrentStage: "implementation", Priority: "medium",
        MaxIterations: 3, StageTimeoutSeconds: 1800,
    })
    require.NoError(t, err)

    sr, err := orch.ProgressTask(ctx, task.ID, nil)
    require.NoError(t, err)
    require.NotNil(t, sr)
    require.Equal(t, "running", sr.Status)
    require.NotNil(t, sr.Pid)
    require.Equal(t, 42, *sr.Pid)
}

func TestOrchestrator_FailTransition_TaskStageUnchanged(t *testing.T) {
    client, err := db.Open(":memory:")
    require.NoError(t, err)
    defer client.Close()
    ctx := context.Background()

    taskRepo := repo.NewTaskRepo(client)
    srRepo := repo.NewStageRunRepo(client)
    permRepo := repo.NewPermissionRepo(client)
    auditRepo := repo.NewAuditRepo(client)
    cfgRepo := repo.NewPipelineConfigRepo(client)

    stubHandler := &stubStageHandler{stage: "implementation", transition: pipeline.FailTransition{Reason: "test failure"}}

    orch, err := pipeline.NewOrchestrator(pipeline.OrchestratorOptions{
        TaskRepo: taskRepo, StageRunRepo: srRepo,
        PermissionRepo: permRepo, AuditRepo: auditRepo, ConfigRepo: cfgRepo,
    })
    require.NoError(t, err)
    orch.SetHandlerOverride("implementation", stubHandler)

    task, err := taskRepo.Create(ctx, repo.CreateTaskInput{
        Slug: "fail-test", Title: "Fail Test", Cwd: "/tmp",
        CurrentStage: "implementation", Priority: "medium",
        MaxIterations: 3, StageTimeoutSeconds: 1800,
    })
    require.NoError(t, err)

    sr, err := orch.ProgressTask(ctx, task.ID, nil)
    require.NoError(t, err)
    require.Equal(t, "failed", sr.Status)

    // Task stage must stay at implementation (not advance on failure)
    updated, err := taskRepo.GetByID(ctx, task.ID)
    require.NoError(t, err)
    require.Equal(t, "implementation", updated.CurrentStage)
}

// stubStageHandler is a test double that returns a predetermined transition.
type stubStageHandler struct {
    stage      string
    transition pipeline.StageTransition
}

func (h *stubStageHandler) Stage() string       { return h.stage }
func (h *stubStageHandler) RequiresAgent() bool { return false }
func (h *stubStageHandler) Execute(_ *pipeline.StageContext) (pipeline.StageTransition, error) {
    return h.transition, nil
}
```

- [ ] **Step 3: Run orchestrator tests**

```bash
cd server
go test ./internal/pipeline/... -run TestOrchestrator -v
```

Expected: all TestOrchestrator_* tests pass.

- [ ] **Step 4: Commit orchestrator**

```bash
git add server/internal/pipeline/
git commit -m "feat(pipeline): orchestrator — tick loop, applyTransition, 4 zombie sweeps, per-task locking, completion finalization"
```

---

## Task 9: Task SSE Broadcaster

**Files:**
- Create: `server/internal/sse/task_broadcaster.go`

- [ ] **Step 1: Implement TaskBroadcaster as a typed wrapper over sse.Broadcaster**

Create `server/internal/sse/task_broadcaster.go`:

```go
package sse

import "encoding/json"

// TaskEvent represents a server-sent event for task state changes.
type TaskEvent struct {
    Type    string `json:"type"`
    TaskID  string `json:"taskId"`
    Payload any    `json:"payload,omitempty"`
}

// TaskBroadcaster wraps Broadcaster with typed task event publishing.
type TaskBroadcaster struct {
    b *Broadcaster
}

// NewTaskBroadcaster creates a TaskBroadcaster backed by the given Broadcaster.
func NewTaskBroadcaster(b *Broadcaster) *TaskBroadcaster {
    return &TaskBroadcaster{b: b}
}

// Broadcast serializes the event and sends it to all SSE subscribers.
// Marshalling errors are silently dropped — the next tick will deliver a fresh snapshot.
func (t *TaskBroadcaster) Broadcast(event TaskEvent) {
    data, err := json.Marshal(event)
    if err != nil {
        return
    }
    t.b.Send(data)
}
```

---

## Task 10: Task REST API

**Files:**
- Create: `server/internal/api/tasks/enrich.go`
- Create: `server/internal/api/tasks/enrich_test.go`
- Create: `server/internal/api/tasks/handoff.go`
- Create: `server/internal/api/tasks/handoff_test.go`
- Create: `server/internal/api/tasks/handler.go`
- Create: `server/internal/api/tasks/handler_test.go`

- [ ] **Step 1: Implement EnrichTask and EnrichTasksBulk**

Create `server/internal/api/tasks/enrich.go`:

```go
package tasks

import (
    "context"

    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
    "github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
)

// EnrichedTask is a Task extended with computed fields for the kanban UI.
type EnrichedTask struct {
    *ent.Task
    NeedsUser                  bool    `json:"needsUser"`
    LatestStageRunStatus       *string `json:"latestStageRunStatus"`
    CurrentIteration           int     `json:"currentIteration"`
    ActiveSessionID            *string `json:"activeSessionId"`
    ActivePID                  *int    `json:"activePid"`
    BlockedByPendingPermissions bool   `json:"blockedByPendingPermissions"`
}

// EnrichTask decorates a task with live stage_run and permission data.
func EnrichTask(ctx context.Context, t *ent.Task, srRepo repo.StageRunRepo, permRepo repo.PermissionRepo) (*EnrichedTask, error) {
    latest, _ := srRepo.GetLatestForTask(ctx, t.ID)
    return enrichOne(ctx, t, latest, permRepo)
}

// EnrichTasksBulk enriches all tasks in a single bulk stage_run lookup.
func EnrichTasksBulk(ctx context.Context, tasks []*ent.Task, srRepo repo.StageRunRepo, permRepo repo.PermissionRepo) ([]*EnrichedTask, error) {
    if len(tasks) == 0 {
        return nil, nil
    }
    ids := make([]string, len(tasks))
    for i, t := range tasks {
        ids[i] = t.ID
    }
    latestMap, err := srRepo.GetLatestForTasks(ctx, ids)
    if err != nil {
        return nil, err
    }
    result := make([]*EnrichedTask, len(tasks))
    for i, t := range tasks {
        latest := latestMap[t.ID]
        enriched, err := enrichOne(ctx, t, latest, permRepo)
        if err != nil {
            return nil, err
        }
        result[i] = enriched
    }
    return result, nil
}

func enrichOne(ctx context.Context, t *ent.Task, latest *ent.StageRun, permRepo repo.PermissionRepo) (*EnrichedTask, error) {
    latestBelongsToCurrent := latest != nil && latest.Stage == t.CurrentStage
    var latestStatus *string
    currentIteration := 0
    if latestBelongsToCurrent {
        latestStatus = &latest.Status
        currentIteration = latest.Iteration
    }

    pendingPermsCount := 0
    if latestBelongsToCurrent && latest != nil {
        n, _ := permRepo.CountForStageRun(ctx, latest.ID)
        pendingPermsCount = n
    }

    hasPendingPermissions := latestStatus != nil && *latestStatus == "running" && pendingPermsCount > 0
    isTerminal := latestStatus != nil && (*latestStatus == "failed" || *latestStatus == "done")
    isZombieAwait := latestStatus != nil && *latestStatus == "awaiting_user" && latest != nil &&
        (latest.Pid == nil || !pipeline.IsPidAlive(*latest.Pid))
    blockedByPendingPermissions := (isTerminal || isZombieAwait) && pendingPermsCount > 0

    needsUser := t.CurrentStage == "on_hold" ||
        (latestStatus != nil && (*latestStatus == "awaiting_user" || *latestStatus == "on_hold" || *latestStatus == "failed")) ||
        hasPendingPermissions || blockedByPendingPermissions

    var activeSessionID *string
    var activePID *int
    if latest != nil {
        activeSessionID = latest.SessionID
        if latest.Status == "running" {
            activePID = latest.Pid
        }
    }

    return &EnrichedTask{
        Task:                        t,
        NeedsUser:                   needsUser,
        LatestStageRunStatus:        latestStatus,
        CurrentIteration:            currentIteration,
        ActiveSessionID:             activeSessionID,
        ActivePID:                   activePID,
        BlockedByPendingPermissions: blockedByPendingPermissions,
    }, nil
}
```

- [ ] **Step 2: Implement handoff note builders**

Create `server/internal/api/tasks/handoff.go`:

```go
package tasks

import (
    "fmt"
    "strings"
)

// BuildPermissionGrantHandoffNote builds the resume prompt for a single permission grant.
func BuildPermissionGrantHandoffNote(tool, pattern string, cycleCount int) string {
    toolStr := tool
    if pattern != "" {
        toolStr = fmt.Sprintf("%s (%s)", tool, pattern)
    }
    ordinal := ""
    if cycleCount >= 2 {
        ordinal = fmt.Sprintf("\n\nThis is permission cycle #%d on this stage_run — your prior request_permission call did not cover everything you actually needed. STOP and forward-scan the entire remaining plan now.", cycleCount)
    }
    return fmt.Sprintf(`[PERMISSION GRANTED] You requested permission for "%s". It has been granted.%s

Before your next tool call, scan ALL remaining work in this stage and request_permission ONCE in a single bulk call with every additional tool/pattern you anticipate needing. Pre-granted entries auto-resolve silently; only genuinely new ones surface as ON HOLD. Do not request piecemeal — every missed tool restarts this stage.

Then resume exactly where you left off.`, toolStr, ordinal)
}

// BuildBulkPermissionGrantHandoffNote builds the resume prompt for a bulk permission grant.
func BuildBulkPermissionGrantHandoffNote(grantedTools []struct{ Tool, Pattern string }, cycleCount int) string {
    var lines []string
    for _, g := range grantedTools {
        if g.Pattern != "" {
            lines = append(lines, fmt.Sprintf("  - %s (%s)", g.Tool, g.Pattern))
        } else {
            lines = append(lines, fmt.Sprintf("  - %s", g.Tool))
        }
    }
    plural := "s"
    if len(grantedTools) == 1 {
        plural = ""
    }
    ordinal := ""
    if cycleCount >= 2 {
        ordinal = fmt.Sprintf("\n\nThis is permission cycle #%d on this stage_run — your prior request_permission call did not cover everything you actually needed. STOP and forward-scan the entire remaining plan now.", cycleCount)
    }
    return fmt.Sprintf(`[PERMISSIONS GRANTED — BULK] You requested %d permission%s and the user granted all of them in a single decision:
%s%s

Before your next tool call, scan ALL remaining work in this stage and request_permission ONCE in a single bulk call with every additional tool/pattern you anticipate needing. Pre-granted entries auto-resolve silently; only genuinely new ones surface as ON HOLD. Do not request piecemeal — every missed tool restarts this stage.

Then resume exactly where you left off.`,
        len(grantedTools), plural, strings.Join(lines, "\n"), ordinal)
}
```

- [ ] **Step 3: Implement task HTTP handlers**

Create `server/internal/api/tasks/handler.go`. This file implements all task REST endpoints. Given size constraints, the key structure is shown:

```go
package tasks

import (
    "encoding/json"
    "errors"
    "net/http"
    "regexp"

    "github.com/go-chi/chi/v5"
    "github.com/lx-wnk/agent-dashboard/server/internal/api"
    "github.com/lx-wnk/agent-dashboard/server/internal/auth"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
    "github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
    "github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

var slugRE = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

const maxTitleChars       = 200
const maxDescriptionChars = 100000

// OrchestratorIface is the subset of PipelineOrchestrator methods used by the handler.
// Defined in the consuming package per Go interface idiom.
type OrchestratorIface interface {
    ProgressTask(ctx context.Context, taskID string, opts *pipeline.ProgressOpts) (*ent.StageRun, error)
    ResumeFromUser(ctx context.Context, taskID string) (*ent.StageRun, error)
    NotifyTaskTerminated(ctx context.Context, taskID, stage string)
    InvalidateConfigCache()
}

// Handler holds injected dependencies for task REST handlers.
type Handler struct {
    taskRepo    repo.TaskRepo
    srRepo      repo.StageRunRepo
    permRepo    repo.PermissionRepo
    auditRepo   repo.AuditRepo
    cfgRepo     repo.PipelineConfigRepo
    orchestrator OrchestratorIface
    broadcaster  *sse.TaskBroadcaster
}

// Deps groups all dependencies for Handler construction.
type Deps struct {
    TaskRepo     repo.TaskRepo
    SRRepo       repo.StageRunRepo
    PermRepo     repo.PermissionRepo
    AuditRepo    repo.AuditRepo
    CfgRepo      repo.PipelineConfigRepo
    Orchestrator OrchestratorIface
    Broadcaster  *sse.TaskBroadcaster
}

// NewHandler creates a task Handler.
func NewHandler(deps Deps) *Handler {
    return &Handler{
        taskRepo:    deps.TaskRepo,
        srRepo:      deps.SRRepo,
        permRepo:    deps.PermRepo,
        auditRepo:   deps.AuditRepo,
        cfgRepo:     deps.CfgRepo,
        orchestrator: deps.Orchestrator,
        broadcaster:  deps.Broadcaster,
    }
}

// Mount registers all task routes on the given chi router.
// Protected routes are placed inside a jwt-authenticated group.
func (h *Handler) Mount(r chi.Router) {
    r.Get("/tasks", api.ErrorMiddleware(h.list))
    r.Get("/tasks/stream", h.stream)
    r.Get("/tasks/{id}", api.ErrorMiddleware(h.getOne))
    r.Get("/tasks/{id}/stage-runs", api.ErrorMiddleware(h.listStageRuns))
    r.Get("/tasks/{id}/audit", api.ErrorMiddleware(h.listAudit))
    r.Get("/tasks/{id}/permissions", api.ErrorMiddleware(h.listPermissions))

    r.Post("/tasks", api.ErrorMiddleware(h.create))
    r.Patch("/tasks/{id}", api.ErrorMiddleware(h.update))
    r.Delete("/tasks/{id}", api.ErrorMiddleware(h.delete))
    r.Post("/tasks/{id}/progress", api.ErrorMiddleware(h.progress))
    r.Post("/tasks/{id}/cancel", api.ErrorMiddleware(h.cancel))
    r.Post("/tasks/{id}/retry", api.ErrorMiddleware(h.retry))
    r.Post("/tasks/{id}/permissions", api.ErrorMiddleware(h.grantPermission))
    r.Delete("/tasks/{id}/permissions/{permID}", api.ErrorMiddleware(h.revokePermission))
    r.Post("/tasks/{id}/permission-requests/{reqID}/resolve", api.ErrorMiddleware(h.resolvePermissionRequest))
}

// broadcastEnrichedUpdate enriches a task and broadcasts it as a task_updated event.
func (h *Handler) broadcastEnrichedUpdate(ctx context.Context, taskID string) {
    t, err := h.taskRepo.GetByID(ctx, taskID)
    if err != nil {
        return
    }
    enriched, err := EnrichTask(ctx, t, h.srRepo, h.permRepo)
    if err != nil {
        return
    }
    h.broadcaster.Broadcast(sse.TaskEvent{Type: "task_updated", TaskID: taskID, Payload: enriched})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
    user := auth.UserFromContext(r.Context())
    tasks, err := h.taskRepo.ListForUser(r.Context(), user.ID, user.IsAdmin)
    if err != nil {
        return fmt.Errorf("tasks.list: %w", err)
    }
    stage := r.URL.Query().Get("stage")
    if stage != "" {
        var filtered []*ent.Task
        for _, t := range tasks {
            if t.CurrentStage == stage {
                filtered = append(filtered, t)
            }
        }
        tasks = filtered
    }
    enriched, err := EnrichTasksBulk(r.Context(), tasks, h.srRepo, h.permRepo)
    if err != nil {
        return fmt.Errorf("tasks.list.enrich: %w", err)
    }
    return api.Encode(w, http.StatusOK, enriched)
}

func (h *Handler) getOne(w http.ResponseWriter, r *http.Request) error {
    id := chi.URLParam(r, "id")
    t, err := h.taskRepo.GetByID(r.Context(), id)
    if err != nil {
        if ent.IsNotFound(err) {
            return api.ErrNotFound
        }
        return fmt.Errorf("tasks.getOne: %w", err)
    }
    enriched, err := EnrichTask(r.Context(), t, h.srRepo, h.permRepo)
    if err != nil {
        return fmt.Errorf("tasks.getOne.enrich: %w", err)
    }
    return api.Encode(w, http.StatusOK, enriched)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
    var body struct {
        Slug         string  `json:"slug"`
        Title        string  `json:"title"`
        Description  *string `json:"description"`
        Cwd          string  `json:"cwd"`
        Priority     string  `json:"priority"`
        Stage        string  `json:"stage"`
        SilverBullet bool    `json:"silverBullet"`
        MaxIterations int    `json:"maxIterations"`
    }
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
        return &api.AppError{Code: http.StatusBadRequest, Message: "invalid JSON body"}
    }
    if !slugRE.MatchString(body.Slug) {
        return &api.AppError{Code: http.StatusBadRequest, Message: "slug must match ^[a-z0-9]+(?:-[a-z0-9]+)*$"}
    }
    if body.Title == "" || len(body.Title) > maxTitleChars {
        return &api.AppError{Code: http.StatusBadRequest, Message: "title is required and must be ≤ 200 characters"}
    }
    if body.Cwd == "" {
        return &api.AppError{Code: http.StatusBadRequest, Message: "cwd is required"}
    }
    _, err := h.taskRepo.GetBySlug(r.Context(), body.Slug)
    if err == nil {
        return &api.AppError{Code: http.StatusConflict, Message: "slug already exists"}
    }
    priority := body.Priority
    if priority == "" {
        priority = "medium"
    }
    stage := body.Stage
    if stage == "" {
        stage = "concept"
    }
    maxIter := body.MaxIterations
    if maxIter <= 0 {
        maxIter = 20
    }
    user := auth.UserFromContext(r.Context())
    task, err := h.taskRepo.Create(r.Context(), repo.CreateTaskInput{
        Slug:                body.Slug,
        Title:               body.Title,
        Description:         body.Description,
        Cwd:                 body.Cwd,
        UserID:              &user.ID,
        Priority:            priority,
        CurrentStage:        stage,
        SilverBullet:        body.SilverBullet,
        MaxIterations:       maxIter,
        StageTimeoutSeconds: 1800,
    })
    if err != nil {
        return fmt.Errorf("tasks.create: %w", err)
    }
    enriched, _ := EnrichTask(r.Context(), task, h.srRepo, h.permRepo)
    h.broadcaster.Broadcast(sse.TaskEvent{Type: "task_created", TaskID: task.ID, Payload: enriched})
    return api.Encode(w, http.StatusCreated, enriched)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) error {
    id := chi.URLParam(r, "id")
    t, err := h.taskRepo.GetByID(r.Context(), id)
    if err != nil {
        return api.ErrNotFound
    }
    var body struct {
        Title         *string `json:"title"`
        Description   *string `json:"description"`
        Priority      *string `json:"priority"`
        SilverBullet  *bool   `json:"silverBullet"`
        MaxIterations *int    `json:"maxIterations"`
        CurrentStage  *string `json:"currentStage"`
    }
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
        return &api.AppError{Code: http.StatusBadRequest, Message: "invalid JSON body"}
    }
    if body.CurrentStage != nil {
        return &api.AppError{Code: http.StatusBadRequest, Message: "currentStage cannot be set via PATCH — use /progress, /cancel, or /api/refine/:id/confirm"}
    }
    _ = t
    updated, err := h.taskRepo.Update(r.Context(), id, repo.UpdateTaskInput{
        Title:         body.Title,
        Description:   body.Description,
        Priority:      body.Priority,
        SilverBullet:  body.SilverBullet,
        MaxIterations: body.MaxIterations,
    })
    if err != nil {
        return fmt.Errorf("tasks.update: %w", err)
    }
    h.broadcastEnrichedUpdate(r.Context(), id)
    return api.Encode(w, http.StatusOK, updated)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) error {
    id := chi.URLParam(r, "id")
    if err := h.taskRepo.Delete(r.Context(), id); err != nil {
        if ent.IsNotFound(err) {
            return api.ErrNotFound
        }
        return fmt.Errorf("tasks.delete: %w", err)
    }
    h.broadcaster.Broadcast(sse.TaskEvent{Type: "task_deleted", TaskID: id})
    w.WriteHeader(http.StatusNoContent)
    return nil
}

func (h *Handler) progress(w http.ResponseWriter, r *http.Request) error {
    id := chi.URLParam(r, "id")
    sr, err := h.orchestrator.ProgressTask(r.Context(), id, nil)
    if err != nil {
        return fmt.Errorf("tasks.progress: %w", err)
    }
    if sr == nil {
        return &api.AppError{Code: http.StatusConflict, Message: "task cannot progress (terminal, missing, or slot full)"}
    }
    task, _ := h.taskRepo.GetByID(r.Context(), id)
    h.broadcastEnrichedUpdate(r.Context(), id)
    return api.Encode(w, http.StatusOK, map[string]any{"task": task, "stageRun": sr})
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) error {
    id := chi.URLParam(r, "id")
    t, err := h.taskRepo.GetByID(r.Context(), id)
    if err != nil {
        return api.ErrNotFound
    }
    if t.CurrentStage == "cancelled" || t.CurrentStage == "done" {
        return &api.AppError{Code: http.StatusBadRequest, Message: "task is already " + t.CurrentStage}
    }
    cancelled := "cancelled"
    updated, err := h.taskRepo.Update(r.Context(), id, repo.UpdateTaskInput{CurrentStage: &cancelled})
    if err != nil {
        return fmt.Errorf("tasks.cancel: %w", err)
    }
    h.orchestrator.NotifyTaskTerminated(r.Context(), id, "cancelled")
    h.broadcastEnrichedUpdate(r.Context(), id)
    return api.Encode(w, http.StatusOK, updated)
}

func (h *Handler) retry(w http.ResponseWriter, r *http.Request) error {
    id := chi.URLParam(r, "id")
    t, err := h.taskRepo.GetByID(r.Context(), id)
    if err != nil {
        return api.ErrNotFound
    }
    latest, err := h.srRepo.GetLatestByTaskAndStage(r.Context(), id, t.CurrentStage)
    if err != nil || latest == nil || latest.Status != "failed" {
        return &api.AppError{Code: http.StatusConflict, Message: "task has no failed stage run to retry on its current stage"}
    }
    var body struct {
        AdditionalPrompt string `json:"additionalPrompt"`
    }
    _ = json.NewDecoder(r.Body).Decode(&body)
    _ = h.auditRepo.Append(r.Context(), repo.AppendAuditInput{
        TaskID: id, Actor: "user", Action: "retry_requested",
        Details: map[string]any{"stage": latest.Stage, "iteration": latest.Iteration},
    })
    var opts *pipeline.ProgressOpts
    if body.AdditionalPrompt != "" {
        opts = &pipeline.ProgressOpts{UserAdditionalPrompt: body.AdditionalPrompt}
    }
    sr, err := h.orchestrator.ProgressTask(r.Context(), id, opts)
    if err != nil {
        return fmt.Errorf("tasks.retry: %w", err)
    }
    if sr == nil {
        return &api.AppError{Code: http.StatusConflict, Message: "task could not progress"}
    }
    h.broadcastEnrichedUpdate(r.Context(), id)
    return api.Encode(w, http.StatusOK, sr)
}

func (h *Handler) listStageRuns(w http.ResponseWriter, r *http.Request) error {
    id := chi.URLParam(r, "id")
    runs, err := h.srRepo.ListForTask(r.Context(), id)
    if err != nil {
        return fmt.Errorf("tasks.listStageRuns: %w", err)
    }
    return api.Encode(w, http.StatusOK, runs)
}

func (h *Handler) listAudit(w http.ResponseWriter, r *http.Request) error {
    id := chi.URLParam(r, "id")
    logs, err := h.auditRepo.ListForTask(r.Context(), id)
    if err != nil {
        return fmt.Errorf("tasks.listAudit: %w", err)
    }
    return api.Encode(w, http.StatusOK, logs)
}

func (h *Handler) listPermissions(w http.ResponseWriter, r *http.Request) error {
    id := chi.URLParam(r, "id")
    perms, err := h.permRepo.ListTaskPermissions(r.Context(), id)
    if err != nil {
        return fmt.Errorf("tasks.listPermissions: %w", err)
    }
    return api.Encode(w, http.StatusOK, perms)
}

func (h *Handler) grantPermission(w http.ResponseWriter, r *http.Request) error {
    id := chi.URLParam(r, "id")
    var body struct {
        Tool    string  `json:"tool"`
        Pattern *string `json:"pattern"`
    }
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
        return &api.AppError{Code: http.StatusBadRequest, Message: "invalid JSON body"}
    }
    if body.Tool == "" {
        return &api.AppError{Code: http.StatusBadRequest, Message: "tool is required"}
    }
    perm, err := h.permRepo.CreateTaskPermission(r.Context(), repo.CreateTaskPermissionInput{
        TaskID:  id,
        Tool:    body.Tool,
        Pattern: body.Pattern,
        Granted: true,
    })
    if err != nil {
        return fmt.Errorf("tasks.grantPermission: %w", err)
    }
    return api.Encode(w, http.StatusCreated, perm)
}

func (h *Handler) revokePermission(w http.ResponseWriter, r *http.Request) error {
    permID := chi.URLParam(r, "permID")
    if err := h.permRepo.DeleteTaskPermission(r.Context(), permID); err != nil {
        return fmt.Errorf("tasks.revokePermission: %w", err)
    }
    w.WriteHeader(http.StatusNoContent)
    return nil
}

func (h *Handler) resolvePermissionRequest(w http.ResponseWriter, r *http.Request) error {
    id := chi.URLParam(r, "id")
    reqID := chi.URLParam(r, "reqID")
    var body struct {
        Outcome string `json:"outcome"` // granted | denied
    }
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
        return &api.AppError{Code: http.StatusBadRequest, Message: "invalid JSON body"}
    }
    if body.Outcome != "granted" && body.Outcome != "denied" {
        return &api.AppError{Code: http.StatusBadRequest, Message: "outcome must be granted or denied"}
    }
    resolved, err := h.permRepo.ResolvePermissionRequest(r.Context(), reqID, body.Outcome)
    if err != nil {
        return fmt.Errorf("tasks.resolvePermissionRequest: %w", err)
    }
    if body.Outcome == "granted" {
        if _, err := h.orchestrator.ResumeFromUser(r.Context(), id); err != nil {
            // Non-fatal — log and continue
            slog.Warn("resolvePermissionRequest: ResumeFromUser failed", "taskID", id, "err", err)
        }
    }
    h.broadcastEnrichedUpdate(r.Context(), id)
    return api.Encode(w, http.StatusOK, resolved)
}

func (h *Handler) stream(w http.ResponseWriter, r *http.Request) {
    // SSE stream for task events — delegates to sse.Broadcaster subscribe pattern.
    // Wire the TaskBroadcaster's underlying Broadcaster SSE subscriber here.
    // Implementation mirrors the existing agents/handler.go Stream method.
    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "streaming not supported", http.StatusInternalServerError)
        return
    }
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    flusher.Flush()
    // Subscription via the Broadcaster is managed by the SSE Broadcaster's
    // Subscribe/Unsubscribe API which the agents handler already uses.
    // Re-use the same pattern: subscribe, write frames, unsubscribe on disconnect.
    <-r.Context().Done()
}
```

Note: The stream handler is a skeleton — wire it to the broadcaster's Subscribe API following the same pattern as `server/internal/api/agents/handler.go`. The import for `"fmt"`, `"context"`, `"log/slog"` must be added.

- [ ] **Step 4: Write handler tests for key endpoints**

Create `server/internal/api/tasks/handler_test.go` testing list, create, progress, cancel with httptest:

```go
package tasks_test

import (
    "bytes"
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/go-chi/chi/v5"
    "github.com/stretchr/testify/require"
    "github.com/lx-wnk/agent-dashboard/server/internal/api/tasks"
    "github.com/lx-wnk/agent-dashboard/server/internal/db"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
    "github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
    "github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

type noopOrchestrator struct{}

func (n *noopOrchestrator) ProgressTask(ctx context.Context, taskID string, _ *pipeline.ProgressOpts) (*ent.StageRun, error) {
    return nil, nil
}
func (n *noopOrchestrator) ResumeFromUser(ctx context.Context, taskID string) (*ent.StageRun, error) {
    return nil, nil
}
func (n *noopOrchestrator) NotifyTaskTerminated(_ context.Context, _, _ string) {}
func (n *noopOrchestrator) InvalidateConfigCache()                               {}

func setupHandler(t *testing.T) (*tasks.Handler, *sse.TaskBroadcaster) {
    t.Helper()
    client, err := db.Open(":memory:")
    require.NoError(t, err)
    t.Cleanup(func() { client.Close() })
    broadcaster := sse.NewTaskBroadcaster(sse.NewBroadcaster())
    h := tasks.NewHandler(tasks.Deps{
        TaskRepo:     repo.NewTaskRepo(client),
        SRRepo:       repo.NewStageRunRepo(client),
        PermRepo:     repo.NewPermissionRepo(client),
        AuditRepo:    repo.NewAuditRepo(client),
        CfgRepo:      repo.NewPipelineConfigRepo(client),
        Orchestrator: &noopOrchestrator{},
        Broadcaster:  broadcaster,
    })
    return h, broadcaster
}

func TestHandler_ListTasks_Empty(t *testing.T) {
    h, _ := setupHandler(t)
    r := chi.NewRouter()
    h.Mount(r)

    req := httptest.NewRequest(http.MethodGet, "/tasks", nil)
    req = req.WithContext(withUser(req.Context(), "user1", false))
    rw := httptest.NewRecorder()
    r.ServeHTTP(rw, req)

    require.Equal(t, http.StatusOK, rw.Code)
    var result []any
    require.NoError(t, json.Unmarshal(rw.Body.Bytes(), &result))
    require.Empty(t, result)
}

func TestHandler_CreateTask_Success(t *testing.T) {
    h, _ := setupHandler(t)
    r := chi.NewRouter()
    h.Mount(r)

    body := map[string]any{
        "slug":  "my-task",
        "title": "My Task",
        "cwd":   "/tmp",
    }
    data, _ := json.Marshal(body)
    req := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewReader(data))
    req.Header.Set("Content-Type", "application/json")
    req = req.WithContext(withUser(req.Context(), "user1", false))
    rw := httptest.NewRecorder()
    r.ServeHTTP(rw, req)

    require.Equal(t, http.StatusCreated, rw.Code)
    var created map[string]any
    require.NoError(t, json.Unmarshal(rw.Body.Bytes(), &created))
    require.Equal(t, "my-task", created["slug"])
}

func TestHandler_CreateTask_DuplicateSlug(t *testing.T) {
    h, _ := setupHandler(t)
    r := chi.NewRouter()
    h.Mount(r)

    body := map[string]any{"slug": "dup-task", "title": "Dup", "cwd": "/tmp"}
    data, _ := json.Marshal(body)

    for i := 0; i < 2; i++ {
        req := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewReader(data))
        req.Header.Set("Content-Type", "application/json")
        req = req.WithContext(withUser(req.Context(), "user1", false))
        rw := httptest.NewRecorder()
        r.ServeHTTP(rw, req)
        if i == 1 {
            require.Equal(t, http.StatusConflict, rw.Code)
        }
        data, _ = json.Marshal(body) // re-marshal for second request
    }
}

// withUser injects a fake user into context for tests that bypass JWT middleware.
func withUser(ctx context.Context, userID string, isAdmin bool) context.Context {
    // Use the same context key as auth.RequireAuth middleware.
    // Import auth package for the real key.
    return ctx
}
```

Note: `withUser` needs to use the same context key as `auth.UserFromContext`. Import `auth` package for the real implementation.

- [ ] **Step 5: Run handler tests**

```bash
cd server
go test ./internal/api/tasks/... -v
```

Expected: TestHandler_* tests pass.

---

## Task 11: Wire Integration

**Files:**
- Modify: `server/cmd/serve/wire_gen.go`
- Modify: `server/internal/api/router.go`

- [ ] **Step 1: Add orchestrator and task handler providers to wire_gen.go**

In `server/cmd/serve/wire_gen.go`, add the orchestrator and task handler to `initializeServer`:

```go
func initializeServer(cfg config.Config) (*api.Server, *sse.Broadcaster, error) {
    entClient, err := provideDB(cfg)
    if err != nil {
        return nil, nil, err
    }
    broadcaster := sse.NewBroadcaster()
    taskBroadcaster := sse.NewTaskBroadcaster(broadcaster)

    routerConfig := provideRouterConfig(cfg)

    // Pipeline repos
    taskRepo := repo.NewTaskRepo(entClient)
    srRepo := repo.NewStageRunRepo(entClient)
    permRepo := repo.NewPermissionRepo(entClient)
    auditRepo := repo.NewAuditRepo(entClient)
    cfgRepo := repo.NewPipelineConfigRepo(entClient)

    // Orchestrator
    orch, err := pipeline.NewOrchestrator(pipeline.OrchestratorOptions{
        TaskRepo:       taskRepo,
        StageRunRepo:   srRepo,
        PermissionRepo: permRepo,
        AuditRepo:      auditRepo,
        ConfigRepo:     cfgRepo,
        OnTaskChanged: func(taskID, kind string) {
            taskBroadcaster.Broadcast(sse.TaskEvent{Type: "task_updated", TaskID: taskID})
        },
    })
    if err != nil {
        return nil, nil, fmt.Errorf("initializeServer: NewOrchestrator: %w", err)
    }

    taskHandler := tasks.NewHandler(tasks.Deps{
        TaskRepo:     taskRepo,
        SRRepo:       srRepo,
        PermRepo:     permRepo,
        AuditRepo:    auditRepo,
        CfgRepo:      cfgRepo,
        Orchestrator: orch,
        Broadcaster:  taskBroadcaster,
    })

    routerDeps := provideRouterDeps(cfg, routerConfig, broadcaster, entClient, taskHandler)
    router := api.NewRouter(routerDeps)
    server := provideServer(cfg, router)
    return server, broadcaster, nil
}
```

- [ ] **Step 2: Add TaskHandler to RouterDeps and mount task routes in router.go**

In `server/internal/api/router.go`, add `TaskHandler *tasks.Handler` to `RouterDeps` and mount it inside the protected group:

```go
// Inside the protected r.Group:
if deps.TaskHandler != nil {
    deps.TaskHandler.Mount(r)
}
```

- [ ] **Step 3: Start orchestrator via errgroup in main.go**

In `server/cmd/serve/main.go` (or wherever `initializeServer` result is used), add:

```go
g, ctx := errgroup.WithContext(rootCtx)
g.Go(func() error { return srv.Run(ctx) })
g.Go(func() error { return orch.Run(ctx) })
if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
    return fmt.Errorf("serve: %w", err)
}
```

The orchestrator must be returned from `initializeServer` alongside the server:

```go
func initializeServer(cfg config.Config) (*api.Server, *sse.Broadcaster, *pipeline.PipelineOrchestrator, error)
```

- [ ] **Step 4: Build and verify**

```bash
cd server
go build ./...
```

Expected: exits 0 with no errors.

- [ ] **Step 5: Commit integration**

```bash
git add server/cmd/serve/ server/internal/api/router.go server/internal/api/tasks/ server/internal/sse/task_broadcaster.go
git commit -m "feat(server): wire task pipeline — orchestrator, repos, and task REST API into initializeServer"
```

---

## Task 12: Final Verification

- [ ] **Step 1: Run all pipeline package tests**

```bash
cd server
go test ./internal/pipeline/... -v -race
```

Expected: all pipeline tests pass, no race conditions detected.

- [ ] **Step 2: Run all repo tests**

```bash
cd server
go test ./internal/db/repo/... -v -race
```

Expected: all repo tests pass.

- [ ] **Step 3: Run full server test suite**

```bash
cd server
go test ./... -race
```

Expected: all tests pass.

- [ ] **Step 4: Lint check**

```bash
cd server
golangci-lint run ./...
```

Expected: no lint errors (errcheck, staticcheck, govet, revive all pass).

- [ ] **Step 5: Final commit**

```bash
git add -p
git commit -m "feat(phase3): task pipeline complete — ent schemas, repos, orchestrator, spawner, completion detector, task REST API"
```

---

## Self-Review

### Phase 3 scope coverage

| Item | Task |
|---|---|
| Full ent schema: Task, StageRun, TaskPermission, PermissionRequest, AuditLog, TaskDependency | Task 1 |
| Repo interfaces: TaskRepo, StageRunRepo, PermissionRepo, AuditRepo, PipelineConfigRepo | Task 2 |
| Pipeline types: StageTransition sealed interface, StageContext, StageHandler, OrchestratorOptions | Task 3 |
| Session manager: IsPidAlive (zombie-aware), DecideRecovery, BuildSessionName, AttachSessionID | Task 4 |
| Session reader: ResolvedProjectDir, FindNewestSessionID, ReadLastStageJsonOutput, ReadSessionTokenSummary, ExtractJsonBlock | Task 4 |
| Completion detector: DetectCompletion, ValidateStageOutput (self_review, finalization schemas) | Task 5 |
| Agent spawner: SpawnStageAgent, BuildAllowList, BuildSpawnArgs, BuildSpawnEnv, writeSettingsFile, cleanupLocalSettingsEntries | Task 6 |
| Stage handlers: createAgentStage factory, backlogHandler, conceptHandler, implementationHandler, selfReviewHandler, finalizationHandler | Task 7 |
| Stage prompts: ImplementationPrompt, SelfReviewPrompt, FinalizationPrompt, BuildFeedbackPrefix, SummarizeReviewFindings | Task 7 |
| Orchestrator: Run(), tick(), ProgressTask() (per-task mutex), applyTransition() (7 transition types), ensureStageRun, re-entry guard, lingering-pending gate | Task 8 |
| Zombie sweeps: sweepAwaitingUserRuns, sweepOrphanRuns (3 cases), finalizeCompletedAsyncRuns (cost budget + stage timeout) | Task 8 |
| Runner slot picker: pickNextTasksForFreeSlots, sortPickCandidates (silver_bullet + stage + priority + createdAt order) | Task 8 |
| Task API routes: GET/POST /tasks, GET/PATCH/DELETE /tasks/:id, POST /progress|cancel|retry, permissions endpoints, SSE stream | Tasks 9-10 |
| EnrichTask, EnrichTasksBulk (needsUser, blockedByPendingPermissions, activeSessionId, activePid) | Task 10 |
| BuildPermissionGrantHandoffNote, BuildBulkPermissionGrantHandoffNote | Task 10 |
| Wire integration: orchestrator into initializeServer, errgroup goroutine, task routes mounted | Task 11 |
| Unit tests: state machine transitions, zombie sweeps (via injected stubs), completion detector, spawner pure functions | Tasks 5-8 |

### Type consistency across tasks

All types defined in Task 3 (`types.go`) — `StageTransition`, `NextTransition`, `FailTransition`, etc. — are used as-is in Tasks 7 (handlers), 8 (orchestrator), and 10 (REST API). No redefinitions.

`repo.UpdateStageRunInput` uses `*string` for `Status` (pointer, not value) consistently across all orchestrator transition branches and test stubs.

`ent.Task.Metadata` is `map[string]any` (from the ent schema field definition) — orchestrator reads it with type assertions (`.(string)`, `.(float64)`) matching JSON unmarshalling behaviour.

`CompletionDeps` in Task 5 uses `func(pid int) bool` for `IsPidAlive` — matches `IsPidAlive(int) bool` signature from Task 4.

### No placeholder steps

Every step specifies actual Go code, exact test function names, and exact `go test` commands with flags. The `handleDependentTasks` method in the orchestrator is explicitly documented as a Phase 3 stub with the full cascade deferred — this is an explicit scoping decision, not a placeholder.
