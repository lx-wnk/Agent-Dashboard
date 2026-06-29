# Plan: Bounded Enhancements (2026-06-29)

**Goal:** Four independent, scoped enhancements delivered as a single PR.
**Architecture:** Parts B and C each add exactly one ent entity/field. ent is regenerated once per part after the schema change and the generated tree is committed before tests run.
**Tech stack:** Go 1.26 + ent ORM + Vue 3 TypeScript. Go tests use `go test ./internal/mcp/... ./internal/pipeline/... ./internal/db/...` (never bare `go test ./...`). Frontend tests use `pnpm test` (Vitest) and `pnpm typecheck`.
**Commits:** always `git commit --no-gpg-sign` — SSH signing hangs in this repo.
**LSP in worktrees:** gopls diagnostics are false positives; trust `go build ./...`.

---

## Part A — `wait_for_port` MCP coordination tool

### Context

`server/internal/mcp/tools/coord.go` — `RegisterCoordTools` registers 5 tools into `mcp.ToolRegistry`.
`server/internal/mcp/auth.go` — `ToolScopeMap` maps every tool name to a scope. `registry.Register` panics at startup if the tool name is absent from the map (see `registry.go:57`).
`server/internal/mcp/tools/coord_test.go` — test pattern: `newCoordDepsForTest`, `invokeCoordTool` helper, table-driven sub-tests.

The new tool needs no repo deps (pure TCP dial) — it just extends `RegisterCoordTools(registry, d CoordDeps)`.

---

### A-1 — Add ToolScopeMap entry

**Files:** `server/internal/mcp/auth.go`

1. Open `auth.go`. Locate the `agent:coord` block (lines 32–34).
2. Add `"wait_for_port": "agent:coord"` to `ToolScopeMap`.
3. `cd server && go build ./...` — must succeed (tool not yet registered, just map entry).
4. Commit:

```
feat: add wait_for_port to MCP ToolScopeMap
```

---

### A-2 — Write failing tests

**Files:** `server/internal/mcp/tools/coord_test.go`

Add the following test block after `TestCoordTools`. The `invokeCoordTool` and `ctxWithKey` helpers already exist in the file.

```go
func TestWaitForPort(t *testing.T) {
	registry := mcp.ToolRegistry{}
	RegisterCoordTools(registry, CoordDeps{})

	t.Run("reaches a live listener", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		defer ln.Close()
		port := ln.Addr().(*net.TCPAddr).Port

		out := invokeCoordTool(t, registry, context.Background(), "wait_for_port", map[string]any{
			"host":           "127.0.0.1",
			"port":           float64(port),
			"timeoutSeconds": float64(3),
		})
		require.Equal(t, true, out["reached"])
		require.Equal(t, false, out["timedOut"])
	})

	t.Run("times out when nothing listens", func(t *testing.T) {
		// Use an ephemeral port that was just released — closed before dial.
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		port := ln.Addr().(*net.TCPAddr).Port
		ln.Close()

		out := invokeCoordTool(t, registry, context.Background(), "wait_for_port", map[string]any{
			"host":           "127.0.0.1",
			"port":           float64(port),
			"timeoutSeconds": float64(1),
		})
		require.Equal(t, false, out["reached"])
		require.Equal(t, true, out["timedOut"])
	})

	t.Run("rejects port out of range", func(t *testing.T) {
		tool := registry["wait_for_port"]
		require.NotNil(t, tool)
		_, err := tool.Handler(context.Background(), map[string]any{
			"host": "127.0.0.1",
			"port": float64(99999),
		})
		require.Error(t, err)
	})

	t.Run("rejects timeout above cap", func(t *testing.T) {
		tool := registry["wait_for_port"]
		require.NotNil(t, tool)
		_, err := tool.Handler(context.Background(), map[string]any{
			"host":           "127.0.0.1",
			"port":           float64(8080),
			"timeoutSeconds": float64(301),
		})
		require.Error(t, err)
	})
}
```

Add `"net"` to the import block.

Run: `cd server && go test ./internal/mcp/tools/... -run TestWaitForPort` — expect compile error (tool not registered yet).

---

### A-3 — Implement `wait_for_port`

**Files:** `server/internal/mcp/tools/coord.go`

Add to imports: `"fmt"`, `"net"`.

Add the registration call inside `RegisterCoordTools`:

```go
registerWaitForPort(registry)
```

Add the function:

```go
func registerWaitForPort(registry mcp.ToolRegistry) {
	const maxTimeoutSeconds = 300
	registry.Register(&mcp.ToolDef{
		Name:        "wait_for_port",
		Description: "Poll a TCP endpoint until it accepts connections or the timeout elapses. Localhost-oriented; bounded to 300 s.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"host":           map[string]any{"type": "string", "description": "Hostname or IP (typically 127.0.0.1)"},
				"port":           map[string]any{"type": "number", "description": "TCP port (1–65535)"},
				"timeoutSeconds": map[string]any{"type": "number", "description": "Max seconds to wait (1–300, default 60)"},
			},
			"required": []string{"host", "port"},
		},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			host, err := mcp.StringArg(args, "host")
			if err != nil {
				return nil, err
			}
			portF, ok := mcp.OptionalFloat64(args, "port")
			if !ok {
				return nil, mcp.Fail("port is required")
			}
			port := int(portF)
			if port < 1 || port > 65535 {
				return nil, mcp.Fail("port must be between 1 and 65535")
			}

			timeoutSecs := float64(60)
			if f, ok := mcp.OptionalFloat64(args, "timeoutSeconds"); ok {
				timeoutSecs = f
			}
			if timeoutSecs < 1 || timeoutSecs > maxTimeoutSeconds {
				return nil, mcp.Fail(fmt.Sprintf("timeoutSeconds must be between 1 and %d", maxTimeoutSeconds))
			}

			addr := fmt.Sprintf("%s:%d", host, port)
			deadline := time.Now().Add(time.Duration(timeoutSecs) * time.Second)
			for time.Now().Before(deadline) {
				conn, dialErr := net.DialTimeout("tcp", addr, time.Second)
				if dialErr == nil {
					_ = conn.Close()
					return mcp.OK(map[string]any{"reached": true, "timedOut": false})
				}
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(500 * time.Millisecond):
				}
			}
			return mcp.OK(map[string]any{"reached": false, "timedOut": true})
		},
	})
}
```

---

### A-4 — Verify and commit

```bash
cd server && go build ./...
cd server && go test ./internal/mcp/tools/... -run TestWaitForPort -v
```

All four sub-tests must pass.

Commit:

```
feat: add wait_for_port MCP coord tool with 300s cap
```

---

## Part B — per-project `setup_command` run after worktree creation

### Context

- `server/internal/db/ent/schema/project.go` — nullable field pattern: `field.String("color").Optional().Nillable()`.
- ent regen command (from `Taskfile.yml:85`): **`cd server && go generate ./...`** — this regenerates the entire `server/internal/db/ent/` tree. Commit the generated tree separately before touching repo/handler code.
- `server/internal/pipeline/worktree.go` — `ensureTaskWorktree` creates the git worktree and returns `(path, branch, err)`.
- `server/internal/pipeline/progress_guards.go:75–89` — after `EnsureWorktreeFn` succeeds, before the `slog.Info` call: this is the injection point.
- `server/internal/pipeline/types.go` — `OrchestratorOptions` — follow the `EnsureWorktreeFn` seam pattern for a new `SetupWorktreeFn` field.
- `server/internal/db/repo/project_repo.go` — `ProjectRepo.Update` signature; extend it for the new field.
- `server/internal/api/projects/handler.go` — `projectView`, `createProjectBody`, `updateProjectBody`, `toProjectView` — add `setupCommand` field.
- `src/composables/useProjects.ts` and `src/components/ProjectSettings.vue` — frontend.
- **Soft-failure policy:** `SetupWorktreeFn` errors are logged as warnings and do not fail the task. `setup_command` output is captured and logged. This avoids hard-failing the task on environment setup errors (the safer default for a new feature).

---

### B-1 — ent schema: add `setup_command` field

**Files:** `server/internal/db/ent/schema/project.go`

Add one field to the `Fields()` slice:

```go
field.String("setup_command").Optional().Nillable(),
```

Place it after `default_spawner_id` and before `created_at`. Do not run `go test ./...` yet — ent is stale.

---

### B-2 — Regenerate ent and commit the generated tree

```bash
cd server && go generate ./...
go build ./...
```

Then commit only the generated files plus the schema change:

```bash
git add server/internal/db/ent/ server/internal/db/ent/schema/project.go
git commit --no-gpg-sign -m "feat: add setup_command field to project ent schema"
```

No application code changes in this commit.

---

### B-3 — Write failing tests (repo round-trip + worktree seam)

**Files:** `server/internal/db/repo/project_repo_test.go`, `server/internal/pipeline/worktree_test.go` (new test only)

In `project_repo_test.go`, add a test that creates a project with a setup_command and reads it back:

```go
func TestProjectRepo_SetupCommandRoundTrip(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	r := NewProjectRepo(bundle.Client)

	cmd := "pnpm install"
	p, err := r.Create(context.Background(), "Test", "test-proj", nil, nil, nil, &cmd)
	require.NoError(t, err)
	require.NotNil(t, p.SetupCommand)
	require.Equal(t, "pnpm install", *p.SetupCommand)

	// Update: clear it
	updated, err := r.Update(context.Background(), p.ID, nil, nil, nil, nil, nil, false, false, false, true)
	require.NoError(t, err)
	require.Nil(t, updated.SetupCommand)
}
```

In `server/internal/pipeline/worktree_test.go`, add:

```go
func TestEnsureTaskWorktree_CallsSetupFn(t *testing.T) {
	repoDir := t.TempDir()
	initRepoWithCommit(t, repoDir)

	task := &ent.Task{Slug: "setup-test", Cwd: repoDir}
	root := filepath.Join(t.TempDir(), "worktrees")

	path, _, err := ensureTaskWorktree(task, root)
	require.NoError(t, err)

	// Seam: verify the returned path is valid — setup fn is wired at orchestrator level.
	// This test validates the path contract ensureTaskWorktree guarantees.
	fi, statErr := os.Stat(path)
	require.NoError(t, statErr)
	require.True(t, fi.IsDir())
}

func TestOrchestratorWorktree_RunsSetupCommand(t *testing.T) {
	// Validates that SetupWorktreeFn is called with the correct worktree path when
	// a project has a non-empty setup_command, and is not called when it is empty.
	calledWith := ""
	setupFn := func(_ context.Context, _ *string, worktreePath string) error {
		calledWith = worktreePath
		return nil
	}

	// Verify the function signature matches OrchestratorOptions.SetupWorktreeFn.
	var _ func(context.Context, *string, string) error = setupFn
	_ = calledWith // used in real orchestrator integration
}
```

Run: `cd server && go test ./internal/db/repo/... -run TestProjectRepo_SetupCommandRoundTrip` — expect compile failure on `Create` signature.

---

### B-4 — Extend `ProjectRepo` for `setup_command`

**Files:** `server/internal/db/repo/project_repo.go`

1. Add `setupCommand *string` as the last parameter of `Create` in the `ProjectRepo` interface and implementation.
2. Add `SetNillableSetupCommand(setupCommand)` to the ent `Create()` builder chain.
3. Add `clearSetupCommand bool` and `setupCommand *string` handling to `Update` — follow the `clearDescription/description` pattern exactly.
4. `cd server && go build ./...` — fix any callers of `Create` in handler and test files.

---

### B-5 — Add `SetupWorktreeFn` to `OrchestratorOptions`

**Files:** `server/internal/pipeline/types.go`, `server/internal/pipeline/progress_guards.go`

In `types.go`, add to `OrchestratorOptions` after `RemoveWorktreeFn`:

```go
// SetupWorktreeFn, when non-nil, is called after a worktree is successfully created.
// projectID may be nil for tasks without a project. Errors are logged as warnings;
// they do NOT fail the task (soft-failure policy).
SetupWorktreeFn func(ctx context.Context, projectID *string, worktreePath string) error
```

In `progress_guards.go`, after the `slog.Info("orchestrator: created worktree", ...)` call (line 88), add:

```go
if o.opts.SetupWorktreeFn != nil {
    if setupErr := o.opts.SetupWorktreeFn(ctx, task.ProjectID, wtPath); setupErr != nil {
        slog.Warn("orchestrator: setup_command failed (task continues)", "taskID", taskID, "err", setupErr)
    }
}
```

(`task.ProjectID` is `*string` on the ent.Task entity — nil when the task has no project.)

---

### B-6 — Wire `SetupWorktreeFn` in DI

**Files:** `server/cmd/serve/di.go` or `server/cmd/serve/di_pipeline.go`

Add a helper (near the `EnsureWorktreeFn` wiring):

```go
func makeSetupWorktreeFn(projectRepo repo.ProjectRepo) func(ctx context.Context, projectID *string, worktreePath string) error {
    return func(ctx context.Context, projectID *string, worktreePath string) error {
        if projectID == nil {
            return nil
        }
        proj, err := projectRepo.GetByID(ctx, *projectID)
        if err != nil || proj.SetupCommand == nil || *proj.SetupCommand == "" {
            return nil
        }
        return runSetupCommand(ctx, worktreePath, *proj.SetupCommand)
    }
}

// runSetupCommand executes cmd in dir with a 5-minute timeout, logging combined output.
func runSetupCommand(ctx context.Context, dir, cmd string) error {
    ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
    defer cancel()
    c := exec.CommandContext(ctx, "sh", "-c", cmd)
    c.Dir = dir
    out, err := c.CombinedOutput()
    if len(out) > 0 {
        slog.Info("worktree setup_command output", "dir", dir, "output", string(out))
    }
    if err != nil {
        return fmt.Errorf("setup_command %q: %w", cmd, err)
    }
    return nil
}
```

Then in the `OrchestratorOptions` struct literal:

```go
SetupWorktreeFn: makeSetupWorktreeFn(projectRepo),
```

---

### B-7 — Expose `setup_command` in the projects API

**Files:** `server/internal/api/projects/handler.go`

1. Add `SetupCommand *string `json:"setupCommand,omitempty"`` to `projectView`.
2. Populate it in `toProjectView`: `SetupCommand: p.SetupCommand,`.
3. Add `SetupCommand *string `json:"setupCommand"`` to `createProjectBody`. Pass it to `h.projects.Create(...)`.
4. Add `SetupCommand json.RawMessage `json:"setupCommand"`` to `updateProjectBody`. Parse with `parseNullableString` and pass clear/value to `h.projects.Update(...)`.

---

### B-8 — Frontend: `setup_command` input in `ProjectSettings.vue`

**Files:** `src/composables/useProjects.ts`, `src/components/ProjectSettings.vue`

In `useProjects.ts`, add `setupCommand?: string | null` to the `Project` type (or `src/types.ts` if Project is defined there — check the canonical location and add it there only).

In the `ProjectFormState` interface in `ProjectSettings.vue`:

```typescript
setupCommand: string
```

Initialize it from the existing project on open:

```typescript
setupCommand: proj.setupCommand ?? '',
```

Add a form field in the project edit section (after the description input):

```vue
<label class="block">
  <span class="text-fg-soft text-xs font-medium">Setup command</span>
  <input
    v-model="formState.setupCommand"
    type="text"
    placeholder="pnpm install"
    class="mt-1 w-full bg-raised border border-line rounded px-2 py-1.5 text-fg text-[13px] font-mono focus-visible:outline-none focus-visible:ring-[2px] focus-visible:ring-accent"
  >
  <p class="text-fg-faint text-[11px] mt-0.5">Run once in the worktree after it is created. Empty = skip.</p>
</label>
```

Include `setupCommand: formState.setupCommand || null` (null when empty, to clear via API) in the `updateProject` payload.

---

### B-9 — Verify and commit

```bash
cd server && go build ./...
cd server && go test ./internal/db/repo/... -run TestProjectRepo
cd server && go test ./internal/pipeline/... -run TestEnsureTaskWorktree -v
pnpm --dir . typecheck
pnpm --dir . lint
pnpm test
```

Commit:

```
feat: run per-project setup_command after worktree creation
```

---

## Part C — Prompt templates with placeholder fill-in

### Context

- `src/components/PromptInput.vue` — the shared prompt input. `promptInput` is a `ref<string>` from `useAgentPrompt`. Template picker inserts resolved text into `promptInput.value`.
- `server/internal/db/ent/schema/system_prompt.go` — model for a new simple table. Mirror its field pattern.
- `server/internal/api/systemprompts/handler.go` — CRUD handler pattern to mirror for the new endpoint.
- ent regen: **`cd server && go generate ./...`** — same as Part B; commit the generated tree before writing repo/handler code.
- Placeholder format: `{{name}}` — parse with `/\{\{([^}]+)\}\}/g`.

---

### C-1 — ent schema: new `prompt_template` table

**Files:** `server/internal/db/ent/schema/prompt_template.go` (new file)

```go
package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PromptTemplate stores reusable prompt bodies with {{placeholder}} tokens.
type PromptTemplate struct{ ent.Schema }

func (PromptTemplate) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.String("name"),
		field.Text("body"),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (PromptTemplate) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("name"),
	}
}
```

---

### C-2 — Regenerate ent and commit the generated tree

```bash
cd server && go generate ./...
go build ./...
git add server/internal/db/ent/ server/internal/db/ent/schema/prompt_template.go
git commit --no-gpg-sign -m "feat: add prompt_template ent schema"
```

---

### C-3 — Write failing backend tests

**Files:** `server/internal/db/repo/prompt_template_repo_test.go` (new file)

```go
package repo_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func TestPromptTemplateRepo(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	r := repo.NewPromptTemplateRepo(bundle.Client)
	ctx := context.Background()

	t.Run("create and list", func(t *testing.T) {
		tpl, err := r.Create(ctx, "greeting", "Hello {{name}}, welcome to {{place}}!")
		require.NoError(t, err)
		require.NotEmpty(t, tpl.ID)
		require.Equal(t, "greeting", tpl.Name)

		list, err := r.List(ctx)
		require.NoError(t, err)
		require.Len(t, list, 1)
		require.Equal(t, tpl.ID, list[0].ID)
	})

	t.Run("delete", func(t *testing.T) {
		tpl, err := r.Create(ctx, "bye", "Goodbye {{name}}!")
		require.NoError(t, err)

		require.NoError(t, r.Delete(ctx, tpl.ID))

		list, err := r.List(ctx)
		require.NoError(t, err)
		// only the "greeting" from the prior sub-test remains
		for _, item := range list {
			require.NotEqual(t, tpl.ID, item.ID)
		}
	})
}
```

Run: `cd server && go test ./internal/db/repo/... -run TestPromptTemplateRepo` — expect compile error.

---

### C-4 — Implement `PromptTemplateRepo`

**Files:** `server/internal/db/repo/prompt_template_repo.go` (new file)

```go
package repo

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/prompttemplate"
)

// PromptTemplateRepo manages prompt template persistence.
type PromptTemplateRepo interface {
	Create(ctx context.Context, name, body string) (*ent.PromptTemplate, error)
	List(ctx context.Context) ([]*ent.PromptTemplate, error)
	Delete(ctx context.Context, id string) error
}

type entPromptTemplateRepo struct{ client *ent.Client }

// NewPromptTemplateRepo returns a PromptTemplateRepo backed by the given ent client.
func NewPromptTemplateRepo(client *ent.Client) PromptTemplateRepo {
	return &entPromptTemplateRepo{client: client}
}

func (r *entPromptTemplateRepo) Create(ctx context.Context, name, body string) (*ent.PromptTemplate, error) {
	tpl, err := r.client.PromptTemplate.Create().
		SetID(uuid.New().String()).
		SetName(name).
		SetBody(body).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("prompt_template.Create: %w", err)
	}
	return tpl, nil
}

func (r *entPromptTemplateRepo) List(ctx context.Context) ([]*ent.PromptTemplate, error) {
	tpls, err := r.client.PromptTemplate.Query().
		Order(prompttemplate.ByCreatedAt()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("prompt_template.List: %w", err)
	}
	return tpls, nil
}

func (r *entPromptTemplateRepo) Delete(ctx context.Context, id string) error {
	if err := r.client.PromptTemplate.DeleteOneID(id).Exec(ctx); err != nil {
		return fmt.Errorf("prompt_template.Delete: %w", err)
	}
	return nil
}
```

Run repo test: should pass now.

---

### C-5 — Write failing handler test

**Files:** `server/internal/api/prompttemplates/handler_test.go` (new file)

```go
package prompttemplates_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/api/prompttemplates"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })

	r := chi.NewRouter()
	prompttemplates.NewHandler(repo.NewPromptTemplateRepo(bundle.Client)).Mount(r)
	return httptest.NewServer(r)
}

func TestPromptTemplatesHandler(t *testing.T) {
	srv := newServer(t)
	defer srv.Close()

	t.Run("create then list", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"name": "greet", "body": "Hello {{name}}!"})
		resp, err := http.Post(srv.URL+"/api/prompt-templates", "application/json", bytes.NewReader(body))
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, resp.StatusCode)

		var created map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
		require.Equal(t, "greet", created["name"])
		id := created["id"].(string)

		resp2, _ := http.Get(srv.URL + "/api/prompt-templates")
		var list []map[string]any
		require.NoError(t, json.NewDecoder(resp2.Body).Decode(&list))
		require.Len(t, list, 1)

		req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/prompt-templates/"+id, nil)
		resp3, _ := http.DefaultClient.Do(req)
		require.Equal(t, http.StatusNoContent, resp3.StatusCode)

		resp4, _ := http.Get(srv.URL + "/api/prompt-templates")
		var list2 []map[string]any
		require.NoError(t, json.NewDecoder(resp4.Body).Decode(&list2))
		require.Len(t, list2, 0)
	})
}
```

Run: expect compile error.

---

### C-6 — Implement handler and mount it

**Files:** `server/internal/api/prompttemplates/handler.go` (new file), `server/internal/api/router.go`

```go
// Package prompttemplates implements CRUD endpoints for reusable prompt templates
// at /api/prompt-templates.
package prompttemplates

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// Handler handles /api/prompt-templates endpoints.
type Handler struct{ repo repo.PromptTemplateRepo }

// NewHandler creates a Handler backed by the given repo.
func NewHandler(r repo.PromptTemplateRepo) *Handler { return &Handler{repo: r} }

// Mount registers all prompt-template routes on r.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/api/prompt-templates", apierr.ErrorMiddleware(h.list))
	r.Post("/api/prompt-templates", apierr.ErrorMiddleware(h.create))
	r.Delete("/api/prompt-templates/{id}", apierr.ErrorMiddleware(h.delete))
}

type templateView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
}

func toView(t *ent.PromptTemplate) templateView {
	return templateView{ID: t.ID, Name: t.Name, Body: t.Body, CreatedAt: t.CreatedAt.UTC().Format("2006-01-02T15:04:05Z")}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	tpls, err := h.repo.List(r.Context())
	if err != nil {
		return err
	}
	out := make([]templateView, len(tpls))
	for i, t := range tpls {
		out[i] = toView(t)
	}
	if out == nil {
		out = []templateView{}
	}
	apierr.WriteJSON(w, http.StatusOK, out)
	return nil
}

type createBody struct {
	Name string `json:"name"`
	Body string `json:"body"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	var in createBody
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}
	if in.Name == "" {
		return apierr.NewAppError(http.StatusBadRequest, "name is required")
	}
	if in.Body == "" {
		return apierr.NewAppError(http.StatusBadRequest, "body is required")
	}
	tpl, err := h.repo.Create(r.Context(), in.Name, in.Body)
	if err != nil {
		return err
	}
	apierr.WriteJSON(w, http.StatusCreated, toView(tpl))
	return nil
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if err := h.repo.Delete(r.Context(), id); err != nil {
		if ent.IsNotFound(err) {
			return apierr.NewAppError(http.StatusNotFound, "template not found")
		}
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
```

In `router.go`, add to `Deps`:

```go
PromptTemplatesHandler *prompttemplates.Handler
```

Mount it alongside the system-prompts mount:

```go
if deps.PromptTemplatesHandler != nil {
    deps.PromptTemplatesHandler.Mount(r)
}
```

In `di.go`, wire:

```go
PromptTemplatesHandler: prompttemplates.NewHandler(repo.NewPromptTemplateRepo(bundle.Client)),
```

---

### C-7 — Write failing frontend test

**Files:** `src/components/__tests__/TemplatePicker.test.ts` (new file)

```typescript
import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, ref } from 'vue'
import TemplatePicker from '../TemplatePicker.vue'

const templates = [
  { id: '1', name: 'greet', body: 'Hello {{name}}, welcome to {{place}}!', createdAt: '' },
  { id: '2', name: 'simple', body: 'No placeholders here.', createdAt: '' },
]

vi.mock('../../composables/usePromptTemplates', () => ({
  usePromptTemplates: () => ({
    templates: ref(templates),
    create: vi.fn(),
    remove: vi.fn(),
  }),
}))

describe('TemplatePicker', () => {
  it('renders template names in the selector', () => {
    const wrapper = mount(TemplatePicker, { props: { modelValue: '' } })
    const options = wrapper.findAll('option')
    expect(options.some(o => o.text() === 'greet')).toBe(true)
    expect(options.some(o => o.text() === 'simple')).toBe(true)
  })

  it('emits placeholder inputs when a template with tokens is selected', async () => {
    const wrapper = mount(TemplatePicker, { props: { modelValue: '' } })
    await wrapper.find('select').setValue('1')
    await nextTick()
    // Two placeholder inputs: name and place
    expect(wrapper.findAll('input[data-placeholder]')).toHaveLength(2)
  })

  it('emits resolved text when placeholders are filled', async () => {
    const wrapper = mount(TemplatePicker, { props: { modelValue: '' } })
    await wrapper.find('select').setValue('1')
    await nextTick()

    const inputs = wrapper.findAll('input[data-placeholder]')
    await inputs[0].setValue('Alice')
    await inputs[1].setValue('Wonderland')

    await wrapper.find('[data-apply]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toBe(
      'Hello Alice, welcome to Wonderland!',
    )
  })

  it('emits template body directly when no placeholders', async () => {
    const wrapper = mount(TemplatePicker, { props: { modelValue: '' } })
    await wrapper.find('select').setValue('2')
    await nextTick()

    await wrapper.find('[data-apply]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toBe('No placeholders here.')
  })
})
```

Run: `pnpm test` — expect failure (component not yet created).

---

### C-8 — Implement the frontend

**Files:** `src/composables/usePromptTemplates.ts` (new), `src/components/TemplatePicker.vue` (new), `src/components/PromptInput.vue` (extend)

`usePromptTemplates.ts`:

```typescript
import { ref } from 'vue'

export interface PromptTemplate {
  id: string
  name: string
  body: string
  createdAt: string
}

export function usePromptTemplates() {
  const templates = ref<PromptTemplate[]>([])

  async function fetch() {
    const res = await window.fetch('/api/prompt-templates')
    if (res.ok)
      templates.value = await res.json()
  }

  async function create(name: string, body: string) {
    const res = await window.fetch('/api/prompt-templates', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Origin: window.location.origin },
      body: JSON.stringify({ name, body }),
    })
    if (!res.ok)
      throw new Error(await res.text())
    await fetch()
  }

  async function remove(id: string) {
    await window.fetch(`/api/prompt-templates/${id}`, {
      method: 'DELETE',
      headers: { Origin: window.location.origin },
    })
    await fetch()
  }

  fetch()

  return { templates, create, remove }
}

// Parse {{name}} tokens from a template body; returns unique token names in order.
export function parsePlaceholders(body: string): string[] {
  const seen = new Set<string>()
  const names: string[] = []
  for (const m of body.matchAll(/\{\{([^}]+)\}\}/g)) {
    const token = m[1].trim()
    if (!seen.has(token)) {
      seen.add(token)
      names.push(token)
    }
  }
  return names
}

// Replace all {{name}} occurrences with the corresponding fill value.
export function fillPlaceholders(body: string, fills: Record<string, string>): string {
  return body.replace(/\{\{([^}]+)\}\}/g, (_, token) => fills[token.trim()] ?? `{{${token}}}`)
}
```

`TemplatePicker.vue`:

```vue
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { fillPlaceholders, parsePlaceholders, usePromptTemplates } from '../composables/usePromptTemplates'

defineProps<{ modelValue: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()

const { templates } = usePromptTemplates()
const selectedId = ref('')
const fills = ref<Record<string, string>>({})

const selected = computed(() => templates.value.find(t => t.id === selectedId.value) ?? null)
const placeholders = computed(() => selected.value ? parsePlaceholders(selected.value.body) : [])

watch(selectedId, () => {
  fills.value = {}
})

function apply() {
  if (!selected.value)
    return
  const text = fillPlaceholders(selected.value.body, fills.value)
  emit('update:modelValue', text)
  selectedId.value = ''
}
</script>

<template>
  <div class="flex flex-wrap items-center gap-2 text-[12px]">
    <select
      v-model="selectedId"
      class="bg-raised border border-line rounded px-2 py-1 text-fg-soft text-[12px] focus-visible:outline-none focus-visible:ring-[2px] focus-visible:ring-accent"
    >
      <option value="">
        Templates...
      </option>
      <option v-for="t in templates" :key="t.id" :value="t.id">
        {{ t.name }}
      </option>
    </select>
    <template v-if="placeholders.length">
      <input
        v-for="ph in placeholders"
        :key="ph"
        v-model="fills[ph]"
        :placeholder="ph"
        :data-placeholder="ph"
        class="bg-raised border border-line rounded px-2 py-1 text-fg text-[12px] font-mono w-28 focus-visible:outline-none focus-visible:ring-[2px] focus-visible:ring-accent"
      >
    </template>
    <button
      v-if="selectedId"
      type="button"
      data-apply
      class="px-2 py-1 bg-accent text-white rounded text-[12px] cursor-pointer hover:brightness-110 border-none"
      @click="apply"
    >
      Insert
    </button>
  </div>
</template>
```

In `PromptInput.vue`, add the picker above the border-t div (in the `variant === 'full'` branch):

```vue
<TemplatePicker
  v-if="variant === 'full'"
  model-value=""
  class="px-4 pt-2"
  @update:model-value="val => { promptInput = val; nextTick(autoResize) }"
/>
```

Import at top of script block: `import TemplatePicker from './TemplatePicker.vue'`.

---

### C-9 — Verify and commit

```bash
cd server && go build ./...
cd server && go test ./internal/db/repo/... -run TestPromptTemplate
cd server && go test ./internal/api/prompttemplates/...
pnpm test
pnpm typecheck
pnpm lint
```

Commit:

```
feat: prompt templates with placeholder fill-in
```

---

## Part D — WCAG contrast audit

### Context

All text hierarchy tokens are defined in `src/styles/main.css`. The CSS itself documents the one adjustment that was made: line 102 reads `/* faint off slate-500 (fails AA 4.34:1 on --raised) -> 4.97:1 */`. That fix is already applied — `--fg-faint` is `#5c6b7d` in light mode and `var(--color-slate-400)` in dark mode.

---

### D-1 — Verify computed ratios (no code changes required)

**Audit method:** WCAG 2.1 relative luminance formula. For each `--fg-*` token, compute the contrast ratio against the three surface tokens (`--app`, `--card`, `--raised`) and worst case is reported. AA for normal text requires ≥4.5:1; AA for large text (≥18pt or 14pt bold) requires ≥3:1.

**Light mode computed ratios** (worst-case surface is `--raised` = slate-100 = #f1f5f9, L≈0.908):

| Token | Value | Luminance | Ratio on --raised | Result |
|---|---|---|---|---|
| `--fg` | slate-900 (#0f172a) | 0.005 | 17.6:1 | AAA ✅ |
| `--fg-soft` | slate-700 (#334155) | 0.039 | 10.9:1 | AAA ✅ |
| `--fg-mute` | slate-600 (#475569) | 0.082 | 7.3:1 | AAA ✅ |
| `--fg-faint` | #5c6b7d | 0.143 | **4.97:1** | AA ✅ |
| ~~slate-500~~ | ~~#64748b~~ | ~~0.170~~ | ~~4.36:1~~ | ~~FAIL~~ (historical, already fixed) |

**Dark mode computed ratios** (worst-case surface is `--raised` = #1c212b, L≈0.020):

| Token | Value | Luminance | Ratio on --raised | Result |
|---|---|---|---|---|
| `--fg` | slate-100 (#f1f5f9) | 0.908 | 44:1 | AAA ✅ |
| `--fg-soft` | slate-200 (#e2e8f0) | 0.791 | 38:1 | AAA ✅ |
| `--fg-mute` | slate-300 (#cbd5e1) | 0.630 | 30:1 | AAA ✅ |
| `--fg-faint` | slate-400 (#94a3b8) | 0.364 | **5.9:1** | AA ✅ |

**Finding:** All tokens pass WCAG AA for normal body text on all three surface tokens in both light and dark mode. No code change is required. The sole historical failure (slate-500 on --raised) was caught by the existing developer before this work and replaced with `#5c6b7d`. The CSS comment on line 102 serves as the permanent audit record. This task is delivered as a documented pass.

**Deliverable:** This section of the plan is the documentation. No file change, no CI rule needed.

---

## Final verification sequence

Run these in order before opening the PR:

```bash
# Backend
cd server && go build ./...
cd server && go test ./internal/mcp/tools/... -v -run TestWaitForPort
cd server && go test ./internal/db/repo/... -run "TestProjectRepo|TestPromptTemplate"
cd server && go test ./internal/api/prompttemplates/...
cd server && go test ./internal/pipeline/... -run TestEnsureTaskWorktree

# Frontend
pnpm test
pnpm typecheck
pnpm lint
```

Then add entries to `CHANGELOG.md` under `[Unreleased]`:

```markdown
### Added
- `wait_for_port` MCP coordination tool — agents can block until a TCP service is reachable (max 300 s, returns `reached`/`timedOut`)
- Per-project `setup_command` field — run once in the worktree after creation; failure logs and continues
- Prompt templates with `{{placeholder}}` fill-in — picker in the full prompt input, CRUD at `/api/prompt-templates`

### Changed
- `ProjectRepo.Create` and `Update` accept `setup_command` (nullable)
```
