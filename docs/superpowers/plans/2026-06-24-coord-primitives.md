# Coordination Primitives (Plane B / L0) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add two coordination primitives — scratchpads (shared KV state) and lease-based locks — as ent tables + repos + five `agent:coord`-scoped MCP tools + read-only REST/UI, so dashboard agents can share contracts/handoff state and mutually exclude on shared resources.

**Architecture:** Two additive ent tables (`scratchpad`, `coord_lock`) follow the `pipeline_config` template (free-string `namespace`, unique `(namespace,key)` index). Scratchpad write is an unconditional `OnConflictColumns().UpdateNewValues()` upsert; lock acquire is a transactional read-then-conditional-write (atomic via SQLite single-writer + the unique index). Five MCP tools under a new `agent:coord` scope; owner identity from `AuthFromContext(ctx).KeyID`. A read-only `Coordination` task-modal tab visualizes state.

**Tech Stack:** Go (ent ORM `--feature sql/upsert`, chi, modernc/sqlite), Vue 3 + TS (Vitest). Spec: `docs/superpowers/specs/2026-06-24-coord-primitives-design.md`.

**Worktree:** all work in `/Users/alexanderwink/code/_privat/projects/dashboard-wt-coord-primitives` (branch `feat/coord-primitives`). Commit `--no-gpg-sign`. `task test` regenerates ent (keep `--feature sql/upsert` in `server/internal/db/ent/generate.go`). Verify with `pnpm lint && pnpm typecheck && pnpm test` and `cd server && go build ./... && go test ./...`.

**Key existing patterns (from codebase extraction):**
- ent schema template: `server/internal/db/ent/schema/pipeline_config.go` (Fields + `index.Fields(...).Unique()`, no Edges/Annotations); `field.Time("x").Default(time.Now)`, `.Optional().Nillable()`.
- Upsert: `r.client.X.Create().Set...().OnConflictColumns(xpkg.FieldA, xpkg.FieldB).UpdateNewValues().Exec(ctx)` (`pipeline_config_repo.go:126`).
- codegen: `server/internal/db/ent/generate.go` `//go:generate ... --feature sql/upsert ...`.
- scope: `ToolScopeMap` + `scopeImplies` + `ResolveScopes` in `server/internal/mcp/auth.go:17-62`; `Register` panics on missing scope entry.
- tool: `registerAddDependency` shape in `server/internal/mcp/tools/write.go:783`; `mcp.StringArg/OptionalString/OK/Fail`; deps struct `WriteDeps`; group `RegisterWriteTools`; DI in `server/cmd/serve/di_mcp.go:54`.
- owner identity: `mcp.AuthFromContext(ctx).KeyID` (`auth.go:79-97`, nil-check).
- REST: domain handler `Mount(r chi.Router)` called from `server/internal/api/router.go:~303`; handlers return `error`, wrapped by `apierr.ErrorMiddleware`; `jsonReply(w, status, v)`.
- UI tab: `src/components/TaskModal.vue` `TABS` tuple + `TAB_LABELS` + `v-else-if` render; tabs use `useInjectedTask()` + a `useXxx(task)` composable; template `src/components/task/TaskCostTab.vue`.

---

## Task 1: `scratchpad` schema + repo + ent regen

**Files:**
- Create: `server/internal/db/ent/schema/scratchpad.go`
- Create: `server/internal/db/repo/scratchpad_repo.go`
- Test: `server/internal/db/repo/scratchpad_repo_test.go`

- [ ] **Step 1: Write the failing repo test** mirroring an existing repo test's client setup (`grep -l "enttest" server/internal/db/repo/*_test.go` for the harness):
```go
func TestScratchpad_WriteReadList(t *testing.T) {
	client := newTestClient(t) // use the project's existing test-client helper
	r := NewScratchpadRepo(client)
	ctx := context.Background()
	if err := r.Write(ctx, "ns1", "k1", "v1", "task-A"); err != nil { t.Fatal(err) }
	got, err := r.Read(ctx, "ns1", "k1")
	if err != nil || got == nil || got.Value != "v1" { t.Fatalf("read: %v %+v", err, got) }
	// upsert overwrites
	if err := r.Write(ctx, "ns1", "k1", "v2", "task-B"); err != nil { t.Fatal(err) }
	got, _ = r.Read(ctx, "ns1", "k1")
	if got.Value != "v2" || got.UpdatedByTaskID != "task-B" { t.Fatalf("overwrite failed: %+v", got) }
	// list
	_ = r.Write(ctx, "ns1", "k2", "x", "task-A")
	all, _ := r.List(ctx, "ns1")
	if len(all) != 2 { t.Fatalf("list want 2 got %d", len(all)) }
	// missing read → nil
	miss, _ := r.Read(ctx, "ns1", "nope")
	if miss != nil { t.Fatalf("missing read should be nil") }
}
```

- [ ] **Step 2: Run, expect FAIL** — `cd server && go test ./internal/db/repo/ -run Scratchpad` → FAIL (undefined NewScratchpadRepo + no ent type).

- [ ] **Step 3: Write the schema** (`scratchpad.go`, template = pipeline_config.go):
```go
package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Scratchpad is shared key/value coordination state, keyed by a free-string namespace.
type Scratchpad struct{ ent.Schema }

func (Scratchpad) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("namespace"),
		field.String("key"),
		field.Text("value"),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
		field.String("updated_by_task_id"),
	}
}

func (Scratchpad) Indexes() []ent.Index {
	return []ent.Index{index.Fields("namespace", "key").Unique()}
}
```

- [ ] **Step 4: Regenerate ent** — `cd server && go generate ./internal/db/ent` (or `task generate`). Confirm `--feature sql/upsert` is still in `generate.go` and `ent/scratchpad.go` + `ent/scratchpad/` package now exist.

- [ ] **Step 5: Write the repo** (`scratchpad_repo.go`, upsert template = pipeline_config_repo.go):
```go
package repo

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/scratchpad"
)

type ScratchpadRepo interface {
	Write(ctx context.Context, namespace, key, value, updatedByTaskID string) error
	Read(ctx context.Context, namespace, key string) (*ent.Scratchpad, error)
	List(ctx context.Context, namespace string) ([]*ent.Scratchpad, error)
}

type entScratchpadRepo struct{ client *ent.Client }

func NewScratchpadRepo(client *ent.Client) ScratchpadRepo { return &entScratchpadRepo{client: client} }

func (r *entScratchpadRepo) Write(ctx context.Context, namespace, key, value, updatedBy string) error {
	err := r.client.Scratchpad.Create().
		SetID(uuid.New().String()).
		SetNamespace(namespace).SetKey(key).SetValue(value).SetUpdatedByTaskID(updatedBy).
		OnConflictColumns(scratchpad.FieldNamespace, scratchpad.FieldKey).
		UpdateNewValues().
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("scratchpad.Write: %w", err)
	}
	return nil
}

func (r *entScratchpadRepo) Read(ctx context.Context, namespace, key string) (*ent.Scratchpad, error) {
	row, err := r.client.Scratchpad.Query().
		Where(scratchpad.Namespace(namespace), scratchpad.Key(key)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	return row, err
}

func (r *entScratchpadRepo) List(ctx context.Context, namespace string) ([]*ent.Scratchpad, error) {
	return r.client.Scratchpad.Query().Where(scratchpad.Namespace(namespace)).
		Order(ent.Asc(scratchpad.FieldKey)).All(ctx)
}
```

- [ ] **Step 6: Run, expect PASS** — `cd server && go test ./internal/db/repo/ -run Scratchpad` → PASS.

- [ ] **Step 7: Commit** — `git add -A && git commit --no-gpg-sign -m "feat: scratchpad coordination table and repo"`

---

## Task 2: `coord_lock` schema + repo (transactional acquire)

**Files:**
- Create: `server/internal/db/ent/schema/coord_lock.go`
- Create: `server/internal/db/repo/coord_lock_repo.go`
- Test: `server/internal/db/repo/coord_lock_repo_test.go`

- [ ] **Step 1: Write the failing test** covering all lock semantics + a race:
```go
func TestCoordLock_Semantics(t *testing.T) {
	client := newTestClient(t)
	r := NewCoordLockRepo(client)
	ctx := context.Background()
	// acquire when free
	ok, owner, _, err := r.Acquire(ctx, "ns", "res", "A", time.Minute)
	if err != nil || !ok || owner != "A" { t.Fatalf("free acquire: %v ok=%v owner=%s", err, ok, owner) }
	// acquire when held by other → false, reports current owner
	ok, owner, _, _ = r.Acquire(ctx, "ns", "res", "B", time.Minute)
	if ok || owner != "A" { t.Fatalf("held acquire should fail, owner A; got ok=%v owner=%s", ok, owner) }
	// re-entrant (same owner) → true
	ok, _, _, _ = r.Acquire(ctx, "ns", "res", "A", time.Minute)
	if !ok { t.Fatalf("re-entrant should succeed") }
	// release by non-owner → rejected
	if err := r.Release(ctx, "ns", "res", "B"); err == nil { t.Fatalf("non-owner release should error") }
	// release by owner → gone, next acquire succeeds for anyone
	if err := r.Release(ctx, "ns", "res", "A"); err != nil { t.Fatal(err) }
	ok, owner, _, _ = r.Acquire(ctx, "ns", "res", "B", time.Minute)
	if !ok || owner != "B" { t.Fatalf("post-release acquire: ok=%v owner=%s", ok, owner) }
}

func TestCoordLock_AcquireAfterExpiry(t *testing.T) {
	client := newTestClient(t)
	r := NewCoordLockRepo(client)
	ctx := context.Background()
	r.Acquire(ctx, "ns", "res", "A", -time.Second) // already expired
	ok, owner, _, _ := r.Acquire(ctx, "ns", "res", "B", time.Minute)
	if !ok || owner != "B" { t.Fatalf("expired takeover failed: ok=%v owner=%s", ok, owner) }
}

func TestCoordLock_FreeRace(t *testing.T) {
	client := newTestClient(t)
	r := NewCoordLockRepo(client)
	ctx := context.Background()
	var wg sync.WaitGroup
	var wins int32
	for i := 0; i < 8; i++ {
		owner := fmt.Sprintf("W%d", i)
		wg.Add(1)
		go func() { defer wg.Done(); if ok, _, _, _ := r.Acquire(ctx, "ns", "race", owner, time.Minute); ok { atomic.AddInt32(&wins, 1) } }()
	}
	wg.Wait()
	if wins != 1 { t.Fatalf("exactly one acquirer must win, got %d", wins) }
}
```

- [ ] **Step 2: Run, expect FAIL** — `cd server && go test ./internal/db/repo/ -run CoordLock` → FAIL.

- [ ] **Step 3: Write the schema** (`coord_lock.go`):
```go
package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// CoordLock is a lease-based mutual-exclusion lock keyed by a free-string namespace.
type CoordLock struct{ ent.Schema }

func (CoordLock) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("namespace"),
		field.String("key"),
		field.String("owner_task_id"),
		field.Time("acquired_at").Default(time.Now),
		field.Time("expires_at"),
	}
}

func (CoordLock) Indexes() []ent.Index {
	return []ent.Index{index.Fields("namespace", "key").Unique()}
}
```

- [ ] **Step 4: Regenerate ent** — `cd server && go generate ./internal/db/ent`. Confirm `ent/coordlock/` package exists.

- [ ] **Step 5: Write the repo** (`coord_lock_repo.go`) — transactional acquire (atomic via SQLite single-writer + unique index; insert-race resolved by `ent.IsConstraintError`):
```go
package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/coordlock"
)

type CoordLockRepo interface {
	Acquire(ctx context.Context, namespace, key, owner string, ttl time.Duration) (acquired bool, curOwner string, expiresAt time.Time, err error)
	Release(ctx context.Context, namespace, key, owner string) error
	ListActive(ctx context.Context, namespace string) ([]*ent.CoordLock, error)
}

type entCoordLockRepo struct{ client *ent.Client }

func NewCoordLockRepo(client *ent.Client) CoordLockRepo { return &entCoordLockRepo{client: client} }

func (r *entCoordLockRepo) Acquire(ctx context.Context, namespace, key, owner string, ttl time.Duration) (bool, string, time.Time, error) {
	now := time.Now()
	exp := now.Add(ttl)
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return false, "", time.Time{}, fmt.Errorf("coordlock.Acquire tx: %w", err)
	}
	existing, err := tx.CoordLock.Query().Where(coordlock.Namespace(namespace), coordlock.Key(key)).Only(ctx)
	switch {
	case ent.IsNotFound(err):
		_, cerr := tx.CoordLock.Create().SetID(uuid.New().String()).
			SetNamespace(namespace).SetKey(key).SetOwnerTaskID(owner).
			SetAcquiredAt(now).SetExpiresAt(exp).Save(ctx)
		if cerr != nil {
			_ = tx.Rollback()
			if ent.IsConstraintError(cerr) {
				// lost the insert race — someone else holds it now
				cur, _, e2 := r.read(ctx, namespace, key)
				return false, cur, time.Time{}, e2
			}
			return false, "", time.Time{}, fmt.Errorf("coordlock.Acquire insert: %w", cerr)
		}
		return true, owner, exp, tx.Commit()
	case err != nil:
		_ = tx.Rollback()
		return false, "", time.Time{}, fmt.Errorf("coordlock.Acquire query: %w", err)
	default:
		if existing.ExpiresAt.Before(now) || existing.OwnerTaskID == owner {
			_, uerr := tx.CoordLock.UpdateOne(existing).SetOwnerTaskID(owner).SetAcquiredAt(now).SetExpiresAt(exp).Save(ctx)
			if uerr != nil {
				_ = tx.Rollback()
				return false, "", time.Time{}, fmt.Errorf("coordlock.Acquire update: %w", uerr)
			}
			return true, owner, exp, tx.Commit()
		}
		_ = tx.Rollback()
		return false, existing.OwnerTaskID, existing.ExpiresAt, nil
	}
}

func (r *entCoordLockRepo) read(ctx context.Context, namespace, key string) (string, time.Time, error) {
	row, err := r.client.CoordLock.Query().Where(coordlock.Namespace(namespace), coordlock.Key(key)).Only(ctx)
	if err != nil {
		return "", time.Time{}, nil
	}
	return row.OwnerTaskID, row.ExpiresAt, nil
}

func (r *entCoordLockRepo) Release(ctx context.Context, namespace, key, owner string) error {
	n, err := r.client.CoordLock.Delete().
		Where(coordlock.Namespace(namespace), coordlock.Key(key), coordlock.OwnerTaskID(owner)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("coordlock.Release: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("coordlock.Release: not held by %s", owner)
	}
	return nil
}

func (r *entCoordLockRepo) ListActive(ctx context.Context, namespace string) ([]*ent.CoordLock, error) {
	return r.client.CoordLock.Query().
		Where(coordlock.Namespace(namespace), coordlock.ExpiresAtGT(time.Now())).
		Order(ent.Asc(coordlock.FieldKey)).All(ctx)
}
```

- [ ] **Step 6: Run, expect PASS** — `cd server && go test ./internal/db/repo/ -run CoordLock` → PASS (incl. FreeRace).

- [ ] **Step 7: Commit** — `git add -A && git commit --no-gpg-sign -m "feat: coord_lock lease-based lock table and repo"`

---

## Task 3: `agent:coord` scope

**Files:**
- Modify: `server/internal/mcp/auth.go`
- Test: `server/internal/mcp/auth_test.go` (or nearest scope test)

- [ ] **Step 1: Write the failing test**:
```go
func TestAgentCoordScope(t *testing.T) {
	for _, name := range []string{"write_scratchpad", "read_scratchpad", "list_scratchpad", "acquire_lock", "release_lock"} {
		if ToolScopeMap[name] != "agent:coord" { t.Fatalf("%s scope = %q want agent:coord", name, ToolScopeMap[name]) }
	}
	resolved := ResolveScopes([]string{"pipeline:control"})
	if !resolved["agent:coord"] { t.Fatalf("pipeline:control must imply agent:coord") }
}
```

- [ ] **Step 2: Run, expect FAIL** — `cd server && go test ./internal/mcp/ -run AgentCoordScope` → FAIL.

- [ ] **Step 3: Implement** — in `auth.go`, add the five tools to `ToolScopeMap` (a new `// agent:coord` block):
```go
	// agent:coord
	"write_scratchpad": "agent:coord", "read_scratchpad": "agent:coord",
	"list_scratchpad":  "agent:coord",
	"acquire_lock":     "agent:coord", "release_lock": "agent:coord",
```
And update `scopeImplies`:
```go
var scopeImplies = map[string][]string{
	"tasks:read":       {},
	"tasks:write":      {"tasks:read"},
	"agent:coord":      {},
	"pipeline:control": {"tasks:read", "agent:coord"},
	"keys:manage":      {"tasks:read", "tasks:write", "pipeline:control", "agent:coord"},
}
```

- [ ] **Step 4: Run, expect PASS** — `cd server && go test ./internal/mcp/ -run AgentCoordScope` → PASS.

- [ ] **Step 5: Commit** — `git add -A && git commit --no-gpg-sign -m "feat: agent:coord MCP scope"`

---

## Task 4: MCP coord tools

**Files:**
- Create: `server/internal/mcp/tools/coord.go`
- Modify: `server/cmd/serve/di_mcp.go`
- Test: `server/internal/mcp/tools/coord_test.go`

- [ ] **Step 1: Write failing tests** for the five handlers (mirror an existing tool test's registry+ctx setup; owner via `mcp.ContextWithAuth`):
```go
func TestCoordTools(t *testing.T) {
	client := newTestClient(t)
	reg := mcp.ToolRegistry{}
	RegisterCoordTools(reg, CoordDeps{Scratch: repo.NewScratchpadRepo(client), Locks: repo.NewCoordLockRepo(client)})
	ctx := mcp.ContextWithAuth(context.Background(), &mcp.MCPAuthInfo{KeyID: "task-A"})

	// write + read scratchpad
	call(t, reg, ctx, "write_scratchpad", map[string]any{"namespace": "ns", "key": "k", "value": "v"})
	res := call(t, reg, ctx, "read_scratchpad", map[string]any{"namespace": "ns", "key": "k"})
	// assert res contains value "v" (shape per mcp.OK)

	// acquire lock as task-A → acquired true
	res = call(t, reg, ctx, "acquire_lock", map[string]any{"namespace": "ns", "key": "r", "ttlSeconds": float64(60)})
	// assert acquired == true, owner == "task-A"

	// acquire as task-B (different KeyID ctx) → acquired false
	ctxB := mcp.ContextWithAuth(context.Background(), &mcp.MCPAuthInfo{KeyID: "task-B"})
	res = call(t, reg, ctxB, "acquire_lock", map[string]any{"namespace": "ns", "key": "r", "ttlSeconds": float64(60)})
	// assert acquired == false
}
```
(`call` is a small helper that looks up the ToolDef in `reg` and invokes its Handler with ctx+args; define it in the test file.)

- [ ] **Step 2: Run, expect FAIL** — `cd server && go test ./internal/mcp/tools/ -run CoordTools` → FAIL.

- [ ] **Step 3: Implement `coord.go`** — `CoordDeps{Scratch ScratchpadRepo; Locks CoordLockRepo}`, `RegisterCoordTools`, and five `register*` funcs mirroring `registerAddDependency`. Resolve owner from `mcp.AuthFromContext(ctx)`; require an explicit `ownerTaskId` arg only if the key is not bound (`KeyID == ""`). Key handler bodies:
  - `write_scratchpad`: args namespace/key/value (required) → `d.Scratch.Write(ctx, ns, key, value, owner)` → `mcp.OK(map{"ok":true})`.
  - `read_scratchpad`: → `d.Scratch.Read(...)` → `mcp.OK(map{"entry": row})` (row may be nil).
  - `list_scratchpad`: → `d.Scratch.List(...)` → `mcp.OK(map{"entries": rows})`.
  - `acquire_lock`: args namespace/key (required), `ttlSeconds` (optional, default 300) → `ok, curOwner, exp, err := d.Locks.Acquire(ctx, ns, key, owner, time.Duration(ttl)*time.Second)` → `mcp.OK(map{"acquired": ok, "owner": curOwner, "expiresAt": exp})`.
  - `release_lock`: → `d.Locks.Release(ctx, ns, key, owner)`; on error `mcp.Fail(err.Error())` else `mcp.OK(map{"released": true})`.
  Owner helper:
```go
func ownerFromCtx(ctx context.Context, args map[string]any) string {
	if info := mcp.AuthFromContext(ctx); info != nil && info.KeyID != "" {
		return info.KeyID
	}
	return mcp.OptionalString(args, "ownerTaskId")
}
```

- [ ] **Step 4: Wire DI** — in `di_mcp.go`, build `scratchRepo := repo.NewScratchpadRepo(client)` and `lockRepo := repo.NewCoordLockRepo(client)` next to the other repos (~lines 28-36), and add `mcptools.RegisterCoordTools(registry, mcptools.CoordDeps{Scratch: scratchRepo, Locks: lockRepo})` next to `RegisterWriteTools` (~line 54).

- [ ] **Step 5: Run, expect PASS** — `cd server && go test ./internal/mcp/... -run Coord` → PASS. Also `go build ./...` clean (Register would panic at startup if any tool lacks a ToolScopeMap entry — Task 3 added them).

- [ ] **Step 6: Commit** — `git add -A && git commit --no-gpg-sign -m "feat: scratchpad and lock MCP tools (agent:coord)"`

---

## Task 5: REST read routes

**Files:**
- Create: `server/internal/api/coord/handler.go`
- Modify: `server/internal/api/router.go` + `server/cmd/serve/di.go` (construct + pass handler)
- Test: `server/internal/api/coord/handler_test.go`

- [ ] **Step 1: Write failing test** — `GET /api/coord/{namespace}/scratchpads` returns written entries; `GET /api/coord/{namespace}/locks` returns active locks. Mirror an existing api handler test (httptest + the handler's `Mount`).

- [ ] **Step 2: Run, expect FAIL** — `cd server && go test ./internal/api/coord/...` → FAIL.

- [ ] **Step 3: Implement the handler** (`coord/handler.go`) with `New(scratch repo.ScratchpadRepo, locks repo.CoordLockRepo) *Handler`, a `Mount(r chi.Router)`, and two handlers returning `jsonReply`:
```go
func (h *Handler) Mount(r chi.Router) {
	r.Get("/api/coord/{namespace}/scratchpads", apierr.ErrorMiddleware(h.listScratchpads))
	r.Get("/api/coord/{namespace}/locks", apierr.ErrorMiddleware(h.listLocks))
}
func (h *Handler) listScratchpads(w http.ResponseWriter, r *http.Request) error {
	ns := chi.URLParam(r, "namespace")
	rows, err := h.scratch.List(r.Context(), ns)
	if err != nil { return fmt.Errorf("coord.scratchpads: %w", err) }
	return jsonReply(w, http.StatusOK, map[string]any{"entries": rows})
}
func (h *Handler) listLocks(w http.ResponseWriter, r *http.Request) error {
	ns := chi.URLParam(r, "namespace")
	rows, err := h.locks.ListActive(r.Context(), ns)
	if err != nil { return fmt.Errorf("coord.locks: %w", err) }
	return jsonReply(w, http.StatusOK, map[string]any{"locks": rows})
}
```
(Add a local `jsonReply` matching the tasks-handler helper, or reuse the shared one if exported.)

- [ ] **Step 4: Wire** — in `di.go` construct `coordHandler := coordapi.New(scratchRepo, lockRepo)` and add it to `routerDeps`; in `router.go` call `deps.CoordHandler.Mount(r)` next to `deps.TaskHandler.Mount(r)` (~line 303, inside the same auth/same-origin group).

- [ ] **Step 5: Run, expect PASS** — `cd server && go test ./internal/api/coord/...` → PASS; `go build ./...` clean.

- [ ] **Step 6: Commit** — `git add -A && git commit --no-gpg-sign -m "feat: read-only coordination REST routes"`

---

## Task 6: Coordination UI tab (read-only)

**Files:**
- Create: `src/components/task/CoordinationTab.vue`
- Create: `src/composables/useTaskCoordination.ts`
- Modify: `src/components/TaskModal.vue` (TABS, label, import, render line)
- Test: `src/components/task/__tests__/CoordinationTab.test.ts`

- [ ] **Step 1: Write failing Vitest** — `CoordinationTab` renders scratchpad entries + active locks fetched from the composable, and has no write controls (read-only). Mirror an existing task-tab test.

- [ ] **Step 2: Run, expect FAIL** — `pnpm test CoordinationTab` → FAIL.

- [ ] **Step 3: Composable** (`useTaskCoordination.ts`) — takes the injected task ref; namespace = `task.value.id` (or `task.value.parentTaskId` when set); fetches `GET /api/coord/${ns}/scratchpads` + `/locks`; returns `{ scratchpads, locks, loading, error }` refs (mirror `useTaskCostBreakdown`).

- [ ] **Step 4: Tab component** (`CoordinationTab.vue`, template = TaskCostTab.vue) — `useInjectedTask()` + `useTaskCoordination(task)`; renders a scratchpads list (key → value, updated_by) and an active-locks list (key → owner, expires). Read-only, no inputs/buttons.

- [ ] **Step 5: Wire into TaskModal.vue** — add `'coordination'` to `TABS`; add `coordination: 'Coordination'` to `TAB_LABELS`; add `import CoordinationTab from './task/CoordinationTab.vue'`; add `<CoordinationTab v-else-if="activeTab === 'coordination'" />` in the render block.

- [ ] **Step 6: Run, expect PASS** — `pnpm test CoordinationTab && pnpm typecheck && pnpm lint` → green.

- [ ] **Step 7: Commit** — `git add -A && git commit --no-gpg-sign -m "feat: read-only coordination task-modal tab"`

---

## Task 7: Docs

**Files:**
- Modify: `README.md`, `CHANGELOG.md`, `.agent-context/mcp.md`

- [ ] **Step 1: README** — in "Build & control", add one sentence: agents can coordinate via shared scratchpads and lease-based locks (`agent:coord` MCP scope).

- [ ] **Step 2: CHANGELOG** — `[Unreleased]` → `### Added`: "Coordination primitives: shared scratchpads and lease-based locks via the new `agent:coord` MCP scope (`write_scratchpad`/`read_scratchpad`/`list_scratchpad`/`acquire_lock`/`release_lock`), with a read-only Coordination tab."

- [ ] **Step 3: `.agent-context/mcp.md`** — document the new `agent:coord` scope (implied by `pipeline:control`) and the five tools.

- [ ] **Step 4: Commit** — `git add -A && git commit --no-gpg-sign -m "docs: document coordination primitives and agent:coord scope"`

---

## Self-Review

**Spec coverage:**
- §2 data model (2 tables, unique index) → Tasks 1, 2. ✓
- §3 lock semantics (lease, lazy expiry, atomic, re-entrant, owner release) → Task 2 + tests. ✓
- §4 scratchpad semantics → Task 1. ✓
- §5 MCP surface (5 tools, agent:coord, owner from auth ctx) → Tasks 3, 4. ✓
- §6 REST + UI (read-only) → Tasks 5, 6. ✓
- §7 testing (incl. race) → tests in Tasks 1-6. ✓
- §8 docs → Task 7. ✓
- §9 risks (race test, FeatureUpsert kept, scope-map panic guard, additive tables) → Tasks 2,3,4 + ent regen note. ✓

**Placeholder scan:** No TBD/TODO. Concrete code for schemas, upsert, transactional acquire, scope edits, tool/owner resolution, REST, UI wiring; concrete test assertions + commands. ✓

**Type/name consistency:** `ScratchpadRepo`/`CoordLockRepo`, `NewScratchpadRepo`/`NewCoordLockRepo`, `CoordDeps{Scratch,Locks}`, `RegisterCoordTools`, the five tool names, `agent:coord`, and `ownerFromCtx` are used identically across Tasks 1-7. ✓
