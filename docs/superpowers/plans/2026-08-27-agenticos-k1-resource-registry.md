# AgenticOS K1 — Resource Registry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every managed ARMS resource one identity, scope, node and lifecycle state in a narrow registry table, and reconcile the existing `plugin` rows into it as the first kind.

**Architecture:** A deliberately narrow `resource` table holds identity only — kind-specific data stays in the kind's own table (`plugin`, `spawner`, `task_schedule`), which gains a `resource_id` link. A `Scope` value object replaces the six different ways this codebase currently expresses "global versus project", following the empty-string sentinel that `pipeline_config` already proved necessary on SQLite. Shared repository helpers replace two competing transaction idioms, and the duplicated plugin-id regex is deleted in favour of the canonical slug rule.

**Tech Stack:** Go 1.26, ent v0.14.6 (`--feature sql/upsert`), modernc SQLite, chi. Frontend untouched by this plan.

**Spec:** `docs/superpowers/specs/2026-08-27-agenticos-resource-registry-design.md`
Umbrella: `docs/superpowers/specs/2026-08-27-agenticos-overview-design.md`
Conventions: `docs/superpowers/specs/2026-08-27-agenticos-conventions-design.md`

## Global Constraints

These apply to **every** task below. They are not repeated per task.

- **Go module layout:** Go workspace with `./sdk` and `./server`. All work here is in `./server`. Run Go commands from inside `server/`.
- **`go test` regenerates the ent tree.** After any test run, `git checkout -- server/internal/db/ent/` before committing, unless the regeneration is the change. Verify with `git status --short server/internal/db/ent/` before every commit.
- **ent index changes rebuild SQLite tables and crash on existing databases.** Any new unique index must be pre-created under ent's exact generated name in a pre-migration before `Schema.Create` runs. Read the generated name from `server/internal/db/ent/migrate/schema.go` — a differently named pre-seeded index does not prevent the rebuild. Reference: `server/internal/db/client.go:429-452`, PR #207.
- **JSON columns added to populated tables need a raw `entsql.Default("{}")`.** Not `"'{}'"` — ent's SQLite dialect wraps and escapes it already, and the escaped form ships literal `''{}''` and crashes every load. Reference: `server/internal/db/ent/schema/spawner.go:25-33`. This plan adds no JSON columns, but the rule holds if one is added.
- **Empty string, not NULL, is the global-scope sentinel.** SQLite treats two NULLs as distinct, so a nullable scope column cannot carry a unique index that prevents duplicate globals. Reference: `server/internal/db/ent/schema/pipeline_config.go:21-23`.
- **Server binds to `127.0.0.1` only.** Nothing in this plan changes binding.
- **Everything that ships is English** — code, identifiers, comments, commit messages, PR text.
- **Conventional Commits.** Never reference task numbers or plan phases in a commit message; describe the behaviour self-contained.
- **Gate commands** (paste raw output, a summary is not evidence):
  - `cd server && go build ./...`
  - `cd server && go vet ./...` (module-wide on purpose — a narrow package scope misses `_test.go` files in sibling packages that reference a changed exported type)
  - `cd server && go test -race -count=1 ./internal/db/... ./internal/plugin/... ./serverapp/...`
  - `task test` before the final commit of the plan
  - `gofmt -l` on every changed file

---

### Task 1: Scope value object

Pure Go, no database. Everything later depends on it, and it can be built and tested in isolation.

**Files:**
- Create: `server/internal/db/repo/scope.go`
- Test: `server/internal/db/repo/scope_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `repo.ScopeKind` (string type), constants `repo.ScopeGlobal`, `repo.ScopeProject`, `repo.ScopeApplication`; type `repo.Scope{Kind ScopeKind; Ref string}`; constructors `repo.GlobalScope() Scope`, `repo.ProjectScope(ref string) Scope`, `repo.ApplicationScope(ref string) Scope`; methods `(Scope) Normalize() Scope`, `(Scope) Validate() error`, `(Scope) IsGlobal() bool`.

- [ ] **Step 1: Write the failing test**

Create `server/internal/db/repo/scope_test.go`:

```go
package repo_test

import (
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func TestScopeNormalize(t *testing.T) {
	tests := []struct {
		name string
		in   repo.Scope
		want repo.Scope
	}{
		{
			name: "global keeps the empty sentinel",
			in:   repo.Scope{Kind: repo.ScopeGlobal, Ref: ""},
			want: repo.Scope{Kind: repo.ScopeGlobal, Ref: ""},
		},
		{
			name: "global discards a stray ref",
			in:   repo.Scope{Kind: repo.ScopeGlobal, Ref: "/some/project"},
			want: repo.Scope{Kind: repo.ScopeGlobal, Ref: ""},
		},
		{
			name: "project keeps its ref",
			in:   repo.Scope{Kind: repo.ScopeProject, Ref: "/some/project"},
			want: repo.Scope{Kind: repo.ScopeProject, Ref: "/some/project"},
		},
		{
			name: "project with an empty ref collapses to global",
			in:   repo.Scope{Kind: repo.ScopeProject, Ref: ""},
			want: repo.Scope{Kind: repo.ScopeGlobal, Ref: ""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.Normalize(); got != tt.want {
				t.Errorf("Normalize() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestScopeValidate(t *testing.T) {
	if err := repo.GlobalScope().Validate(); err != nil {
		t.Errorf("global scope must validate, got %v", err)
	}
	if err := repo.ProjectScope("/x").Validate(); err != nil {
		t.Errorf("project scope must validate, got %v", err)
	}
	if err := (repo.Scope{Kind: "nonsense", Ref: ""}).Validate(); err == nil {
		t.Error("unknown scope kind must not validate")
	}
}

func TestScopeIsGlobal(t *testing.T) {
	if !repo.GlobalScope().IsGlobal() {
		t.Error("GlobalScope must report IsGlobal")
	}
	if repo.ProjectScope("/x").IsGlobal() {
		t.Error("project scope must not report IsGlobal")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/db/repo/ -run 'TestScope' -count=1`
Expected: FAIL — compile error, `undefined: repo.Scope`.

- [ ] **Step 3: Write minimal implementation**

Create `server/internal/db/repo/scope.go`:

```go
package repo

import "fmt"

// ScopeKind names the layer a scoped row applies to.
type ScopeKind string

const (
	ScopeGlobal      ScopeKind = "global"
	ScopeProject     ScopeKind = "project"
	ScopeApplication ScopeKind = "application"
)

// Scope is where a resource applies. Ref is empty for ScopeGlobal, and that
// emptiness is a sentinel rather than an accident: SQLite treats two NULLs as
// distinct, so a nullable ref column could not carry a unique index that
// prevents duplicate global rows. pipeline_config already relies on this.
type Scope struct {
	Kind ScopeKind
	Ref  string
}

// GlobalScope returns the scope every resource falls back to.
func GlobalScope() Scope { return Scope{Kind: ScopeGlobal, Ref: ""} }

// ProjectScope scopes to one project working directory.
func ProjectScope(ref string) Scope { return Scope{Kind: ScopeProject, Ref: ref}.Normalize() }

// ApplicationScope scopes to one application resource id.
func ApplicationScope(ref string) Scope {
	return Scope{Kind: ScopeApplication, Ref: ref}.Normalize()
}

// Normalize collapses the representations that mean the same thing, so two
// equal scopes are always equal as struct values.
func (s Scope) Normalize() Scope {
	if s.Kind == ScopeGlobal || s.Ref == "" {
		return Scope{Kind: ScopeGlobal, Ref: ""}
	}
	return s
}

// IsGlobal reports whether this scope is the global fallback layer.
func (s Scope) IsGlobal() bool { return s.Normalize().Kind == ScopeGlobal }

// Validate rejects a scope kind the registry does not know.
func (s Scope) Validate() error {
	switch s.Kind {
	case ScopeGlobal, ScopeProject, ScopeApplication:
		return nil
	default:
		return fmt.Errorf("scope: unknown kind %q", s.Kind)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/db/repo/ -run 'TestScope' -count=1 -v`
Expected: PASS — all three test functions, all subtests.

- [ ] **Step 5: Commit**

```bash
gofmt -l server/internal/db/repo/scope.go server/internal/db/repo/scope_test.go
git add server/internal/db/repo/scope.go server/internal/db/repo/scope_test.go
git commit -m "feat(db): add a Scope value object with an explicit global sentinel

Six different expressions of 'global versus project' exist in this schema
today: an empty-string sentinel, a nullable column, an enum column, a boolean
singleton, a required tenancy column, and an unvalidated free string. Scope is
the one representation new code uses.

Normalize collapses the equivalent forms so two equal scopes compare equal as
struct values, which is what lets a scope be used as a map key and a query
predicate without a helper at each call site."
```

---

### Task 2: Shared id/timestamp mixin

29 schema files repeat the same three fields by hand, and nine of twelve carry a redundant `.StorageKey("id")` while three do not. ent provides mixins; this codebase uses zero. The new schema in Task 3 uses this mixin; existing schemas are **not** migrated onto it in this plan, because changing them regenerates their ent code for no behavioural gain.

**Files:**
- Create: `server/internal/db/ent/schema/mixins.go`
- Test: covered by Task 3's schema test — a mixin with no consumer cannot be tested meaningfully.

**Interfaces:**
- Consumes: nothing.
- Produces: `schema.IDTimestampsMixin` — supplies fields `id` (string, immutable, storage key `id`), `created_at` (time, default now, immutable), `updated_at` (time, default now, updated on write).

- [ ] **Step 1: Write the implementation**

There is no test-first cycle here: a mixin is a declaration consumed by Task 3, and Task 3's test is what proves it.

Create `server/internal/db/ent/schema/mixins.go`:

```go
package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"
)

// IDTimestampsMixin carries the id/created_at/updated_at triple that every
// managed entity in this schema currently repeats by hand. New schemas embed it
// so the storage key and the update-default cannot drift between tables.
type IDTimestampsMixin struct{ mixin.Schema }

// Fields of the IDTimestampsMixin.
func (IDTimestampsMixin) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd server && go build ./internal/db/ent/schema/`
Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
gofmt -l server/internal/db/ent/schema/mixins.go
git add server/internal/db/ent/schema/mixins.go
git commit -m "refactor(db): add a shared id/timestamp mixin for new schemas

Existing schemas keep their hand-written fields — migrating them regenerates
ent code without changing behaviour. New schemas embed the mixin so the
storage key and the update-default cannot drift between tables."
```

---

### Task 3: Resource schema, codegen, and the index pre-migration

This task carries the migration hazard. Read the whole task before starting.

**Files:**
- Create: `server/internal/db/ent/schema/resource.go`
- Modify: `server/internal/db/client.go` (pre-migration function plus its call site)
- Test: `server/internal/db/client_test.go` (append)
- Regenerated (commit as part of this task): `server/internal/db/ent/**`

**Interfaces:**
- Consumes: `schema.IDTimestampsMixin` from Task 2.
- Produces: ent entity `Resource` with fields `id, kind, slug, name, scope_kind, scope_ref, node_id, state, version, origin, origin_ref, created_at, updated_at`; generated package `server/internal/db/ent/resource` with predicates `resource.KindEQ`, `resource.SlugEQ`, `resource.ScopeKindEQ`, `resource.ScopeRefEQ`, `resource.StateEQ`, and field constants `resource.FieldKind`, `resource.FieldSlug`, `resource.FieldScopeKind`, `resource.FieldScopeRef`.

- [ ] **Step 1: Write the schema**

Create `server/internal/db/ent/schema/resource.go`:

```go
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Resource is the narrow identity row every managed ARMS resource carries.
// It holds identity, scope, node and lifecycle state — nothing about what the
// resource does. Kind-specific data stays in the kind's own table, which links
// back through resource_id. A single polymorphic table holding process
// addresses, cron expressions and file paths would collapse into dozens of
// nullable columns.
type Resource struct{ ent.Schema }

// Mixin of the Resource.
func (Resource) Mixin() []ent.Mixin {
	return []ent.Mixin{IDTimestampsMixin{}}
}

// Fields of the Resource.
func (Resource) Fields() []ent.Field {
	return []ent.Field{
		// kind: "application" | "routine" | "skill" | "memory_space".
		// A free string rather than an enum: adding a kind must not require a
		// schema migration, the same reasoning that keeps task stages free-form.
		field.String("kind").Immutable(),
		field.String("slug"),
		field.String("name").Default(""),
		// scope_kind: "global" | "project" | "application".
		field.String("scope_kind").Default("global"),
		// scope_ref is "" for global scope. Sentinel, not NULL: SQLite treats
		// two NULLs as distinct, so a nullable column could not carry the unique
		// index below.
		field.String("scope_ref").Default(""),
		// node_id is "local" until the node registry lands. The column exists now
		// because adding it later means a migration through every resource table
		// and every scoped query.
		field.String("node_id").Default("local"),
		// state: "discovered" | "installed" | "enabled" | "disabled" | "orphaned".
		field.String("state").Default("discovered"),
		field.String("version").Default(""),
		// origin: "builtin" | "local" | "remote".
		field.String("origin").Default("local"),
		// origin_ref records where the resource came from — a manifest id, a
		// GitHub source. It is what lets an upstream update avoid silently
		// overwriting a local edit.
		field.String("origin_ref").Default(""),
	}
}

// Indexes of the Resource.
func (Resource) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("kind", "scope_kind", "scope_ref", "slug").Unique(),
		index.Fields("kind", "state"),
	}
}
```

- [ ] **Step 2: Regenerate ent**

Run: `cd server && go generate ./internal/db/ent/`

Expected: `server/internal/db/ent/resource*.go`, `server/internal/db/ent/resource/` and updated `migrate/schema.go` appear.

Verify the entity exists: `ls server/internal/db/ent/resource/`
Expected: `resource.go`, `where.go`.

- [ ] **Step 3: Read the generated index names — do not guess them**

Run: `grep -n 'resource_kind\|ResourcesTable\|Indexes:' server/internal/db/ent/migrate/schema.go | head -20`

Write down the exact generated name of the unique index. With ent's default naming it is `resource_kind_scope_kind_scope_ref_slug`, but **use whatever the file says**. A pre-seeded index under any other name does not prevent SQLite's table rebuild.

- [ ] **Step 4: Write the failing migration test**

Append to `server/internal/db/client_test.go`:

```go
// TestOpenTwiceWithResourceTable proves the resource table and its unique index
// survive a second Open on an existing database. An ent index change triggers
// SQLite's 12-step table rebuild, which fails on populated tables with
// "NOT NULL constraint failed" — the pre-migration exists to make ent's diff
// find the index already present so it never rebuilds.
func TestOpenTwiceWithResourceTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "twice.db")

	first, err := db.Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	ctx := context.Background()
	if _, err := first.Client.Resource.Create().
		SetID("res-1").
		SetKind("application").
		SetSlug("example").
		Save(ctx); err != nil {
		t.Fatalf("insert resource: %v", err)
	}
	if err := first.Client.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}

	second, err := db.Open(path)
	if err != nil {
		t.Fatalf("second Open on a populated database: %v", err)
	}
	t.Cleanup(func() { _ = second.Client.Close() })

	n, err := second.Client.Resource.Query().Count(ctx)
	if err != nil {
		t.Fatalf("count after reopen: %v", err)
	}
	if n != 1 {
		t.Errorf("resource count after reopen = %d, want 1", n)
	}
}
```

If `filepath`, `context` or `db` are not already imported in that file, add them.

- [ ] **Step 5: Run the test to see the current state**

Run: `cd server && go test ./internal/db/ -run 'TestOpenTwiceWithResourceTable' -count=1 -v`

Expected: PASS on a fresh table — the rebuild hazard only bites when ent's diff finds a *missing* index on an existing table. The test is the guard for the next person who changes this index, not a red test today. Record the PASS and continue; do not skip the pre-migration in Step 6 because of it.

- [ ] **Step 6: Add the pre-migration**

In `server/internal/db/client.go`, add this function next to the other pre-migrations (near `migrateEnsureStageRunSessionIndex`):

```go
// migrateEnsureResourceUniqueIndex pre-creates the resource unique index under
// the exact name ent generates, before ent auto-migrate runs. Without this, a
// later change to the index would make ent's diff add it via SQLite's 12-step
// table rebuild, which crashes on existing databases with
// "NOT NULL constraint failed: resources.id" (PR #207).
// Idempotent via IF NOT EXISTS; on a fresh database the table does not yet
// exist, so the statement is a no-op and ent creates its own index.
func migrateEnsureResourceUniqueIndex(db *sql.DB) error {
	var exists int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'resources'`,
	).Scan(&exists); err != nil {
		return fmt.Errorf("probe resources table: %w", err)
	}
	if exists == 0 {
		return nil
	}
	if _, err := db.Exec(
		`CREATE UNIQUE INDEX IF NOT EXISTS resource_kind_scope_kind_scope_ref_slug ` +
			`ON resources (kind, scope_kind, scope_ref, slug)`,
	); err != nil {
		return fmt.Errorf("pre-create resource unique index: %w", err)
	}
	return nil
}
```

Replace `resource_kind_scope_kind_scope_ref_slug` with the exact name read in Step 3 if it differs.

Add the call **before** `client.Schema.Create`, alongside the other pre-migrations:

```go
	if err := migrateEnsureResourceUniqueIndex(sqlDB); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("db: ensure resource unique index: %w", err)
	}
```

- [ ] **Step 7: Run the full db test package**

Run: `cd server && go test -race -count=1 ./internal/db/...`
Expected: PASS, including `TestOpenTwiceWithResourceTable`.

- [ ] **Step 8: Commit**

The regenerated ent tree **is** the change here, so it is committed rather than reverted.

```bash
gofmt -l server/internal/db/client.go server/internal/db/ent/schema/resource.go
cd server && go build ./... && go vet ./... && cd ..
git add server/internal/db/ent/ server/internal/db/client.go server/internal/db/client_test.go
git commit -m "feat(db): add the resource identity table

A narrow row per managed resource: kind, slug, scope, node, lifecycle state
and origin. Kind-specific data stays in the kind's own table.

node_id defaults to 'local' and is unused for now. It exists from the first
migration because adding a node dimension later would mean migrating every
resource table and every scoped query — a cost that is cheap exactly once.

The unique index is pre-created under ent's generated name before auto-migrate,
so a later change to it cannot trigger SQLite's table rebuild on a populated
database."
```

---

### Task 4: ResourceRepo — write and read

**Files:**
- Create: `server/internal/db/repo/resource_repo.go`
- Test: `server/internal/db/repo/resource_repo_test.go`

**Interfaces:**
- Consumes: `repo.Scope` (Task 1), generated `ent.Resource` and package `ent/resource` (Task 3).
- Produces:
  - `repo.UpsertResourceInput{Kind, Slug, Name, Scope, State, Version, Origin, OriginRef string-ish}` — exact shape below.
  - `repo.ResourceRepo` interface with `Upsert(ctx, UpsertResourceInput) (*ent.Resource, error)`, `Get(ctx, kind string, scope Scope, slug string) (*ent.Resource, error)`, `ListForKind(ctx, kind string) ([]*ent.Resource, error)`, `ListForScope(ctx, kind string, scope Scope) ([]*ent.Resource, error)`, `SetState(ctx, id, state string) (*ent.Resource, error)`, `Delete(ctx, id string) error`.
  - `repo.NewResourceRepo(client *ent.Client) ResourceRepo`.
  - Constants `repo.ResourceKindApplication`, `ResourceKindRoutine`, `ResourceKindSkill`, `ResourceKindMemorySpace`; `repo.ResourceStateDiscovered`, `ResourceStateInstalled`, `ResourceStateEnabled`, `ResourceStateDisabled`, `ResourceStateOrphaned`; `repo.ResourceOriginBuiltin`, `ResourceOriginLocal`, `ResourceOriginRemote`.

- [ ] **Step 1: Write the failing test**

Create `server/internal/db/repo/resource_repo_test.go`:

```go
package repo_test

import (
	"context"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func newResourceRepo(t *testing.T) (repo.ResourceRepo, context.Context) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Client.Close() })
	return repo.NewResourceRepo(bundle.Client), context.Background()
}

func TestResourceUpsertIsIdempotent(t *testing.T) {
	r, ctx := newResourceRepo(t)
	in := repo.UpsertResourceInput{
		Kind:  repo.ResourceKindApplication,
		Slug:  "obsidian",
		Name:  "Obsidian",
		Scope: repo.GlobalScope(),
		State: repo.ResourceStateInstalled,
	}

	first, err := r.Upsert(ctx, in)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	in.Name = "Obsidian Vault"
	second, err := r.Upsert(ctx, in)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("upsert created a second row: %s != %s", first.ID, second.ID)
	}
	if second.Name != "Obsidian Vault" {
		t.Errorf("name = %q, want the updated value", second.Name)
	}

	all, err := r.ListForKind(ctx, repo.ResourceKindApplication)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("expected exactly 1 row after two upserts, got %d", len(all))
	}
}

func TestResourceSameSlugInDifferentScopes(t *testing.T) {
	r, ctx := newResourceRepo(t)
	base := repo.UpsertResourceInput{
		Kind: repo.ResourceKindSkill,
		Slug: "review",
		Name: "Review",
	}

	base.Scope = repo.GlobalScope()
	if _, err := r.Upsert(ctx, base); err != nil {
		t.Fatalf("global upsert: %v", err)
	}
	base.Scope = repo.ProjectScope("/tmp/project-a")
	if _, err := r.Upsert(ctx, base); err != nil {
		t.Fatalf("project upsert: %v", err)
	}

	all, err := r.ListForKind(ctx, repo.ResourceKindSkill)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("same slug in two scopes must coexist, got %d rows", len(all))
	}
}

func TestResourceGetAndSetState(t *testing.T) {
	r, ctx := newResourceRepo(t)
	created, err := r.Upsert(ctx, repo.UpsertResourceInput{
		Kind:  repo.ResourceKindApplication,
		Slug:  "mail",
		Scope: repo.GlobalScope(),
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if created.State != repo.ResourceStateDiscovered {
		t.Errorf("default state = %q, want %q", created.State, repo.ResourceStateDiscovered)
	}

	if _, err := r.SetState(ctx, created.ID, repo.ResourceStateEnabled); err != nil {
		t.Fatalf("set state: %v", err)
	}
	got, err := r.Get(ctx, repo.ResourceKindApplication, repo.GlobalScope(), "mail")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.State != repo.ResourceStateEnabled {
		t.Errorf("state = %q, want %q", got.State, repo.ResourceStateEnabled)
	}
}

func TestResourceGetNormalizesScope(t *testing.T) {
	r, ctx := newResourceRepo(t)
	if _, err := r.Upsert(ctx, repo.UpsertResourceInput{
		Kind:  repo.ResourceKindRoutine,
		Slug:  "morning",
		Scope: repo.GlobalScope(),
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// A project scope with an empty ref means global; Get must find the row.
	got, err := r.Get(ctx, repo.ResourceKindRoutine, repo.Scope{Kind: repo.ScopeProject, Ref: ""}, "morning")
	if err != nil {
		t.Fatalf("get with a collapsing scope: %v", err)
	}
	if got.Slug != "morning" {
		t.Errorf("slug = %q, want morning", got.Slug)
	}
}

func TestResourceRejectsInvalidSlug(t *testing.T) {
	r, ctx := newResourceRepo(t)
	_, err := r.Upsert(ctx, repo.UpsertResourceInput{
		Kind:  repo.ResourceKindApplication,
		Slug:  "Not A Slug",
		Scope: repo.GlobalScope(),
	})
	if err == nil {
		t.Fatal("an invalid slug must be rejected before it reaches the database")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/db/repo/ -run 'TestResource' -count=1`
Expected: FAIL — compile error, `undefined: repo.ResourceRepo`.

- [ ] **Step 3: Write the implementation**

Create `server/internal/db/repo/resource_repo.go`:

```go
package repo

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/resource"
	"github.com/lx-wnk/agent-dashboard/server/internal/validation"
)

// Resource kinds. Free strings rather than a Go enum so adding a kind is not a
// schema migration.
const (
	ResourceKindApplication = "application"
	ResourceKindRoutine     = "routine"
	ResourceKindSkill       = "skill"
	ResourceKindMemorySpace = "memory_space"
)

// Resource lifecycle states. Generalises the derivation the plugin table
// documents today (no installed_at = discovered, installed but inactive =
// disabled, active = enabled) into an explicit column.
const (
	ResourceStateDiscovered = "discovered"
	ResourceStateInstalled  = "installed"
	ResourceStateEnabled    = "enabled"
	ResourceStateDisabled   = "disabled"
	ResourceStateOrphaned   = "orphaned"
)

// Resource origins.
const (
	ResourceOriginBuiltin = "builtin"
	ResourceOriginLocal   = "local"
	ResourceOriginRemote  = "remote"
)

// defaultNodeID is written into every resource until the node registry lands.
const defaultNodeID = "local"

// UpsertResourceInput is the named input for Upsert. Named rather than
// positional because the call has more than four parameters, which is where
// this codebase's convention switches.
type UpsertResourceInput struct {
	Kind      string
	Slug      string
	Name      string
	Scope     Scope
	State     string
	Version   string
	Origin    string
	OriginRef string
}

// ResourceRepo persists the identity row shared by every managed ARMS resource.
type ResourceRepo interface {
	Upsert(ctx context.Context, in UpsertResourceInput) (*ent.Resource, error)
	Get(ctx context.Context, kind string, scope Scope, slug string) (*ent.Resource, error)
	ListForKind(ctx context.Context, kind string) ([]*ent.Resource, error)
	ListForScope(ctx context.Context, kind string, scope Scope) ([]*ent.Resource, error)
	SetState(ctx context.Context, id, state string) (*ent.Resource, error)
	Delete(ctx context.Context, id string) error
}

type entResourceRepo struct {
	client *ent.Client
}

// NewResourceRepo returns a ResourceRepo backed by the ent client.
func NewResourceRepo(client *ent.Client) ResourceRepo {
	return &entResourceRepo{client: client}
}

func (r *entResourceRepo) Upsert(ctx context.Context, in UpsertResourceInput) (*ent.Resource, error) {
	scope := in.Scope.Normalize()
	if err := scope.Validate(); err != nil {
		return nil, fmt.Errorf("resource.Upsert: %w", err)
	}
	if !validation.IsValidSlug(in.Slug) {
		return nil, fmt.Errorf("resource.Upsert: %s", validation.SlugPatternMessage)
	}
	if in.Kind == "" {
		return nil, fmt.Errorf("resource.Upsert: kind is required")
	}

	state := in.State
	if state == "" {
		state = ResourceStateDiscovered
	}
	origin := in.Origin
	if origin == "" {
		origin = ResourceOriginLocal
	}

	err := r.client.Resource.Create().
		SetID(uuid.New().String()).
		SetKind(in.Kind).
		SetSlug(in.Slug).
		SetName(in.Name).
		SetScopeKind(string(scope.Kind)).
		SetScopeRef(scope.Ref).
		SetNodeID(defaultNodeID).
		SetState(state).
		SetVersion(in.Version).
		SetOrigin(origin).
		SetOriginRef(in.OriginRef).
		OnConflictColumns(
			resource.FieldKind,
			resource.FieldScopeKind,
			resource.FieldScopeRef,
			resource.FieldSlug,
		).
		UpdateNewValues().
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("resource.Upsert: %w", err)
	}
	return r.Get(ctx, in.Kind, scope, in.Slug)
}

func (r *entResourceRepo) Get(ctx context.Context, kind string, scope Scope, slug string) (*ent.Resource, error) {
	s := scope.Normalize()
	row, err := r.client.Resource.Query().
		Where(
			resource.KindEQ(kind),
			resource.ScopeKindEQ(string(s.Kind)),
			resource.ScopeRefEQ(s.Ref),
			resource.SlugEQ(slug),
		).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("resource.Get: %w", err)
	}
	return row, nil
}

func (r *entResourceRepo) ListForKind(ctx context.Context, kind string) ([]*ent.Resource, error) {
	rows, err := r.client.Resource.Query().
		Where(resource.KindEQ(kind)).
		Order(ent.Asc(resource.FieldSlug)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("resource.ListForKind: %w", err)
	}
	return rows, nil
}

func (r *entResourceRepo) ListForScope(ctx context.Context, kind string, scope Scope) ([]*ent.Resource, error) {
	s := scope.Normalize()
	rows, err := r.client.Resource.Query().
		Where(
			resource.KindEQ(kind),
			resource.ScopeKindEQ(string(s.Kind)),
			resource.ScopeRefEQ(s.Ref),
		).
		Order(ent.Asc(resource.FieldSlug)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("resource.ListForScope: %w", err)
	}
	return rows, nil
}

func (r *entResourceRepo) SetState(ctx context.Context, id, state string) (*ent.Resource, error) {
	row, err := r.client.Resource.UpdateOneID(id).SetState(state).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("resource.SetState: %w", err)
	}
	return row, nil
}

func (r *entResourceRepo) Delete(ctx context.Context, id string) error {
	if err := r.client.Resource.DeleteOneID(id).Exec(ctx); err != nil {
		return fmt.Errorf("resource.Delete: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test -race ./internal/db/repo/ -run 'TestResource' -count=1 -v`
Expected: PASS — all five test functions.

- [ ] **Step 5: Reset the regenerated ent tree, then commit**

```bash
git status --short server/internal/db/ent/
git checkout -- server/internal/db/ent/
gofmt -l server/internal/db/repo/resource_repo.go server/internal/db/repo/resource_repo_test.go
git add server/internal/db/repo/resource_repo.go server/internal/db/repo/resource_repo_test.go
git commit -m "feat(db): add ResourceRepo with scope-aware upsert and lookup

Upsert keys on (kind, scope_kind, scope_ref, slug), so re-registering a
resource updates it instead of creating a duplicate, and the same slug in two
scopes coexists.

Slugs are validated against validation.SlugRE before the write rather than
relying on the database, so the error names the rule instead of surfacing a
constraint violation."
```

---

### Task 5: Scoped resolution semantics

The registry spec requires the three lookup semantics `pipeline_config` already
proved, named so the idiom transfers: resolve with fallback, resolve exactly, and
list merged. Task 4 built only the exact form.

**Files:**
- Modify: `server/internal/db/repo/resource_repo.go`
- Test: `server/internal/db/repo/resource_repo_test.go` (append)

**Interfaces:**
- Consumes: `repo.ResourceRepo`, `repo.Scope` (Tasks 1 and 4).
- Produces: `ResourceRepo.Resolve(ctx context.Context, kind string, scope Scope, slug string) (*ent.Resource, error)` — the row for this scope, falling back to the global row; `ResourceRepo.ListMerged(ctx context.Context, kind string, scope Scope) ([]*ent.Resource, error)` — global rows merged with scoped rows, scoped winning on a slug collision. `Get` keeps its exact-match, no-fallback meaning.

- [ ] **Step 1: Write the failing test**

Append to `server/internal/db/repo/resource_repo_test.go`:

```go
func TestResourceResolveFallsBackToGlobal(t *testing.T) {
	r, ctx := newResourceRepo(t)
	if _, err := r.Upsert(ctx, repo.UpsertResourceInput{
		Kind:  repo.ResourceKindSkill,
		Slug:  "review",
		Name:  "Global Review",
		Scope: repo.GlobalScope(),
	}); err != nil {
		t.Fatalf("seed global: %v", err)
	}

	got, err := r.Resolve(ctx, repo.ResourceKindSkill, repo.ProjectScope("/tmp/project-a"), "review")
	if err != nil {
		t.Fatalf("Resolve with no project row must fall back: %v", err)
	}
	if got.Name != "Global Review" {
		t.Errorf("name = %q, want the global row", got.Name)
	}
}

func TestResourceResolvePrefersTheScopedRow(t *testing.T) {
	r, ctx := newResourceRepo(t)
	if _, err := r.Upsert(ctx, repo.UpsertResourceInput{
		Kind: repo.ResourceKindSkill, Slug: "review",
		Name: "Global Review", Scope: repo.GlobalScope(),
	}); err != nil {
		t.Fatalf("seed global: %v", err)
	}
	if _, err := r.Upsert(ctx, repo.UpsertResourceInput{
		Kind: repo.ResourceKindSkill, Slug: "review",
		Name: "Project Review", Scope: repo.ProjectScope("/tmp/project-a"),
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	got, err := r.Resolve(ctx, repo.ResourceKindSkill, repo.ProjectScope("/tmp/project-a"), "review")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Name != "Project Review" {
		t.Errorf("name = %q, want the project row to win", got.Name)
	}
}

func TestResourceGetDoesNotFallBack(t *testing.T) {
	r, ctx := newResourceRepo(t)
	if _, err := r.Upsert(ctx, repo.UpsertResourceInput{
		Kind: repo.ResourceKindSkill, Slug: "review", Scope: repo.GlobalScope(),
	}); err != nil {
		t.Fatalf("seed global: %v", err)
	}

	if _, err := r.Get(ctx, repo.ResourceKindSkill, repo.ProjectScope("/tmp/project-a"), "review"); err == nil {
		t.Error("Get must not fall back to the global row — that is Resolve's job")
	}
}

func TestResourceListMergedScopedWins(t *testing.T) {
	r, ctx := newResourceRepo(t)
	for _, in := range []repo.UpsertResourceInput{
		{Kind: repo.ResourceKindSkill, Slug: "review", Name: "Global Review", Scope: repo.GlobalScope()},
		{Kind: repo.ResourceKindSkill, Slug: "deploy", Name: "Global Deploy", Scope: repo.GlobalScope()},
		{Kind: repo.ResourceKindSkill, Slug: "review", Name: "Project Review", Scope: repo.ProjectScope("/tmp/project-a")},
	} {
		if _, err := r.Upsert(ctx, in); err != nil {
			t.Fatalf("seed %s: %v", in.Slug, err)
		}
	}

	merged, err := r.ListMerged(ctx, repo.ResourceKindSkill, repo.ProjectScope("/tmp/project-a"))
	if err != nil {
		t.Fatalf("ListMerged: %v", err)
	}
	if len(merged) != 2 {
		t.Fatalf("merged length = %d, want 2 (review and deploy)", len(merged))
	}
	byName := map[string]string{}
	for _, row := range merged {
		byName[row.Slug] = row.Name
	}
	if byName["review"] != "Project Review" {
		t.Errorf("review = %q, want the project row to win", byName["review"])
	}
	if byName["deploy"] != "Global Deploy" {
		t.Errorf("deploy = %q, want the global row to survive", byName["deploy"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/db/repo/ -run 'TestResourceResolve|TestResourceListMerged|TestResourceGetDoesNotFallBack' -count=1`
Expected: FAIL — compile error, `r.Resolve undefined`.

- [ ] **Step 3: Extend the interface**

In `server/internal/db/repo/resource_repo.go`, add to the `ResourceRepo` interface, directly under `Get`:

```go
	// Resolve returns the row for scope, falling back to the global row when the
	// scope has none. This is the effective-value lookup.
	Resolve(ctx context.Context, kind string, scope Scope, slug string) (*ent.Resource, error)
	// ListMerged returns global rows merged with scope rows, scope winning on a
	// slug collision.
	ListMerged(ctx context.Context, kind string, scope Scope) ([]*ent.Resource, error)
```

- [ ] **Step 4: Implement both**

Append to `server/internal/db/repo/resource_repo.go`:

```go
func (r *entResourceRepo) Resolve(ctx context.Context, kind string, scope Scope, slug string) (*ent.Resource, error) {
	s := scope.Normalize()
	if !s.IsGlobal() {
		row, err := r.Get(ctx, kind, s, slug)
		if err == nil {
			return row, nil
		}
		if !ent.IsNotFound(err) {
			return nil, err
		}
	}
	row, err := r.Get(ctx, kind, GlobalScope(), slug)
	if err != nil {
		return nil, fmt.Errorf("resource.Resolve: %w", err)
	}
	return row, nil
}

func (r *entResourceRepo) ListMerged(ctx context.Context, kind string, scope Scope) ([]*ent.Resource, error) {
	globals, err := r.ListForScope(ctx, kind, GlobalScope())
	if err != nil {
		return nil, fmt.Errorf("resource.ListMerged: %w", err)
	}
	s := scope.Normalize()
	if s.IsGlobal() {
		return globals, nil
	}
	scoped, err := r.ListForScope(ctx, kind, s)
	if err != nil {
		return nil, fmt.Errorf("resource.ListMerged: %w", err)
	}

	bySlug := make(map[string]*ent.Resource, len(globals)+len(scoped))
	for _, row := range globals {
		bySlug[row.Slug] = row
	}
	// Scoped rows overwrite globals of the same slug — the merge rule.
	for _, row := range scoped {
		bySlug[row.Slug] = row
	}

	out := make([]*ent.Resource, 0, len(bySlug))
	for _, row := range bySlug {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}
```

Add `"sort"` to the imports of `resource_repo.go`. `ent` is already imported.

Note the direct `ent.IsNotFound` rather than the package's own `IsNotFound` wrapper: the wrapper exists so callers *outside* this package need not import `ent`, and inside it the direct call is the honest one.

- [ ] **Step 5: Run test to verify it passes**

Run: `cd server && go test -race ./internal/db/repo/ -run 'TestResource' -count=1 -v`
Expected: PASS — every `TestResource*` function, including the four added here.

- [ ] **Step 6: Reset ent, then commit**

```bash
git checkout -- server/internal/db/ent/
gofmt -l server/internal/db/repo/
git add server/internal/db/repo/
git commit -m "feat(db): add scoped resolution and merged listing to the registry

Three lookup semantics, named so the idiom transfers from pipeline_config,
which needed the same trio: Resolve falls back to global, Get stays exact with
no fallback, ListMerged merges with the scoped row winning.

Keeping Get exact matters — a caller that wants 'is there a row for exactly
this project' must not silently receive the global one."
```

---

### Task 6: Shared repository helpers

Two competing transaction idioms exist, and the only two shared functions in the package live inside unrelated files: `IsNotFound` in `plugin_repo.go:32` and `rollback` in `spawner_repo.go:158-163`.

**Files:**
- Create: `server/internal/db/repo/helpers.go`
- Modify: `server/internal/db/repo/plugin_repo.go` (remove `IsNotFound`)
- Modify: `server/internal/db/repo/spawner_repo.go` (remove `rollback`)
- Test: `server/internal/db/repo/helpers_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `repo.IsNotFound(err error) bool` (moved, same behaviour); `repo.WithTx(ctx context.Context, client *ent.Client, fn func(tx *ent.Tx) error) error`. The unexported `rollback` moves to `helpers.go` unchanged and keeps its existing callers working.

- [ ] **Step 1: Write the failing test**

Create `server/internal/db/repo/helpers_test.go`:

```go
package repo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func TestWithTxCommitsOnSuccess(t *testing.T) {
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Client.Close() })
	ctx := context.Background()

	err = repo.WithTx(ctx, bundle.Client, func(tx *ent.Tx) error {
		_, err := tx.Resource.Create().
			SetID("res-tx-1").
			SetKind("application").
			SetSlug("committed").
			Save(ctx)
		return err
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	n, err := bundle.Client.Resource.Query().Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("row count after commit = %d, want 1", n)
	}
}

func TestWithTxRollsBackOnError(t *testing.T) {
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Client.Close() })
	ctx := context.Background()

	sentinel := errors.New("deliberate failure")
	err = repo.WithTx(ctx, bundle.Client, func(tx *ent.Tx) error {
		if _, err := tx.Resource.Create().
			SetID("res-tx-2").
			SetKind("application").
			SetSlug("rolled-back").
			Save(ctx); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTx error = %v, want it to wrap the sentinel", err)
	}

	n, err := bundle.Client.Resource.Query().Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("row count after rollback = %d, want 0", n)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/db/repo/ -run 'TestWithTx' -count=1`
Expected: FAIL — compile error, `undefined: repo.WithTx`.

- [ ] **Step 3: Write the implementation**

Create `server/internal/db/repo/helpers.go`:

```go
package repo

import (
	"context"
	"fmt"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
)

// IsNotFound reports whether err is ent's not-found error. It exists so callers
// outside this package do not have to import ent just to classify an error.
func IsNotFound(err error) bool { return ent.IsNotFound(err) }

// rollback aborts tx and returns cause, preserving the original failure when the
// rollback itself also fails.
func rollback(tx *ent.Tx, cause error) error {
	if rerr := tx.Rollback(); rerr != nil {
		return fmt.Errorf("%w: rollback: %v", cause, rerr)
	}
	return cause
}

// WithTx runs fn inside a transaction, committing on success and rolling back on
// error or panic. It replaces the two rollback idioms this package grew: a
// deferred blind rollback in some repos and an explicit per-branch rollback in
// others.
func WithTx(ctx context.Context, client *ent.Client, fn func(tx *ent.Tx) error) error {
	tx, err := client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		return rollback(tx, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Remove the old definitions**

In `server/internal/db/repo/plugin_repo.go`, delete the `IsNotFound` function and its doc comment.
In `server/internal/db/repo/spawner_repo.go`, delete the `rollback` function and its doc comment.

Both names stay reachable — they now live in `helpers.go` in the same package.

- [ ] **Step 5: Run the full package**

Run: `cd server && go build ./... && go test -race -count=1 ./internal/db/repo/`
Expected: PASS. A duplicate-definition compile error means one of the old copies was not removed.

- [ ] **Step 6: Reset ent, then commit**

```bash
git checkout -- server/internal/db/ent/
gofmt -l server/internal/db/repo/
git add server/internal/db/repo/
git commit -m "refactor(db): consolidate repository helpers into one file

IsNotFound lived in plugin_repo.go and rollback in spawner_repo.go, so the
package's only shared code was hidden inside two unrelated entity files.

WithTx replaces the two transaction idioms that had grown side by side — a
deferred blind rollback in two repos, explicit per-branch rollback in seven.
It also rolls back on panic, which neither idiom did."
```

---

### Task 7: One slug rule

`pluginIDRe` duplicates `validation.SlugRE` with the same shape but **no length bound**. This task deletes it. That is a deliberate behaviour change: a plugin id longer than 64 characters was accepted and now is not.

**Files:**
- Modify: `server/internal/plugin/registry.go` (delete `pluginIDRe`, update its two uses)
- Modify: `server/internal/plugin/validate.go` (delegate to `validation`)
- Modify: `server/internal/plugin/dispatcher.go:26` (use the shared validator)
- Test: `server/internal/plugin/validate_test.go` (create if absent)

**Interfaces:**
- Consumes: `validation.IsValidSlug`, `validation.SlugPatternMessage` from `server/internal/validation`.
- Produces: `plugin.IsValidID(id string) bool` keeps its name and signature; only its implementation changes.

- [ ] **Step 1: Write the failing test**

Create or extend `server/internal/plugin/validate_test.go`:

```go
package plugin_test

import (
	"strings"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
)

func TestIsValidID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
		why  string
	}{
		{"github-oauth", true, "canonical form"},
		{"a", true, "single character"},
		{"a1-b2", true, "digits and hyphens"},
		{"-leading-hyphen", false, "must start alphanumeric"},
		{"Upper", false, "no uppercase"},
		{"has space", false, "no spaces"},
		{"has_underscore", false, "no underscores"},
		{"", false, "empty"},
		// Behaviour change: pluginIDRe had no length bound, the canonical slug
		// rule caps at 64 characters. A 65-character id was accepted before.
		{strings.Repeat("a", 64), true, "at the cap"},
		{strings.Repeat("a", 65), false, "over the cap — previously accepted"},
	}
	for _, tt := range tests {
		if got := plugin.IsValidID(tt.id); got != tt.want {
			t.Errorf("IsValidID(%q) = %v, want %v (%s)", tt.id, got, tt.want, tt.why)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/plugin/ -run 'TestIsValidID' -count=1 -v`
Expected: FAIL on the 65-character case — `IsValidID("aaa…") = true, want false (over the cap — previously accepted)`.

- [ ] **Step 3: Rewrite the validator**

Replace the body of `IsValidID` in `server/internal/plugin/validate.go`:

```go
package plugin

import "github.com/lx-wnk/agent-dashboard/server/internal/validation"

// IsValidID reports whether id is a well-formed plugin id. Plugin ids are slugs
// and use the canonical rule — validation.SlugRE — rather than a local copy.
// The id becomes a path segment under the plugin directory, so rejecting
// anything outside the pattern is also the traversal guard.
func IsValidID(id string) bool {
	return validation.IsValidSlug(id)
}
```

- [ ] **Step 4: Delete `pluginIDRe` and route its callers**

In `server/internal/plugin/registry.go`:
- Delete the `pluginIDRe` variable and its doc comment (lines around 23-25).
- Replace `!pluginIDRe.MatchString(desc.ID)` with `!IsValidID(desc.ID)`.
- Replace `!pluginIDRe.MatchString(id)` with `!IsValidID(id)`.
- Remove the `regexp` import if nothing else in the file uses it.

In `server/internal/plugin/dispatcher.go`:
- Replace `!pluginIDRe.MatchString(id)` with `!IsValidID(id)`.

- [ ] **Step 5: Run the tests**

Run: `cd server && go build ./... && go test -race -count=1 ./internal/plugin/...`
Expected: PASS, including the pre-existing `lifecycle_discovery_test.go:138` case which asserts an uppercase id is rejected.

- [ ] **Step 6: Commit**

```bash
git checkout -- server/internal/db/ent/
gofmt -l server/internal/plugin/
git add server/internal/plugin/
git commit -m "refactor(plugin): validate plugin ids with the canonical slug rule

pluginIDRe duplicated validation.SlugRE with the same shape but no length
bound, in a package whose canonical source says 'Import this instead of
defining a local copy'.

Behaviour change: a plugin id longer than 64 characters was accepted and is
now rejected. No shipped plugin comes close to the cap."
```

---

### Task 8: Delete protection

Only `spawner` refuses to delete a row that is built in, in use, or the current default. This lifts that pattern into shared sentinels the registry uses, so every kind can refuse for a named reason instead of each inventing its own.

**Files:**
- Modify: `server/internal/db/repo/helpers.go` (add sentinels)
- Modify: `server/internal/db/repo/resource_repo.go` (`Delete` honours them)
- Test: `server/internal/db/repo/resource_repo_test.go` (append)

**Interfaces:**
- Consumes: `repo.ResourceRepo` (Task 4).
- Produces: `repo.ErrResourceBuiltIn`, `repo.ErrResourceReferenced` (both `error` sentinels, comparable with `errors.Is`). `ResourceRepo.Delete` gains the documented refusal behaviour; its signature is unchanged.

- [ ] **Step 1: Write the failing test**

Append to `server/internal/db/repo/resource_repo_test.go`:

```go
func TestResourceDeleteRefusesBuiltin(t *testing.T) {
	r, ctx := newResourceRepo(t)
	created, err := r.Upsert(ctx, repo.UpsertResourceInput{
		Kind:   repo.ResourceKindApplication,
		Slug:   "builtin-app",
		Scope:  repo.GlobalScope(),
		Origin: repo.ResourceOriginBuiltin,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	err = r.Delete(ctx, created.ID)
	if !errors.Is(err, repo.ErrResourceBuiltIn) {
		t.Fatalf("Delete of a builtin resource = %v, want ErrResourceBuiltIn", err)
	}

	if _, err := r.Get(ctx, repo.ResourceKindApplication, repo.GlobalScope(), "builtin-app"); err != nil {
		t.Errorf("refused delete must leave the row in place, got %v", err)
	}
}

func TestResourceDeleteAllowsLocal(t *testing.T) {
	r, ctx := newResourceRepo(t)
	created, err := r.Upsert(ctx, repo.UpsertResourceInput{
		Kind:   repo.ResourceKindApplication,
		Slug:   "local-app",
		Scope:  repo.GlobalScope(),
		Origin: repo.ResourceOriginLocal,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := r.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete of a local resource: %v", err)
	}
	if _, err := r.Get(ctx, repo.ResourceKindApplication, repo.GlobalScope(), "local-app"); err == nil {
		t.Error("row still present after a successful delete")
	}
}
```

Add `"errors"` to the test file's imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/db/repo/ -run 'TestResourceDelete' -count=1`
Expected: FAIL — compile error, `undefined: repo.ErrResourceBuiltIn`.

- [ ] **Step 3: Add the sentinels**

Append to `server/internal/db/repo/helpers.go`:

```go
// Deletion refusals shared by managed resources. Only the spawner repository
// had refusals of this shape; they are shared so every kind can refuse for a
// named reason rather than inventing its own error string.
var (
	// ErrResourceBuiltIn means the resource ships with the dashboard and is not
	// the user's to delete.
	ErrResourceBuiltIn = errors.New("resource is built in and cannot be deleted")
	// ErrResourceReferenced means something else still points at this resource.
	ErrResourceReferenced = errors.New("resource is still referenced")
)
```

Add `"errors"` to the imports of `helpers.go`.

- [ ] **Step 4: Honour them in Delete**

Replace `Delete` in `server/internal/db/repo/resource_repo.go`:

```go
func (r *entResourceRepo) Delete(ctx context.Context, id string) error {
	row, err := r.client.Resource.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("resource.Delete: %w", err)
	}
	if row.Origin == ResourceOriginBuiltin {
		return fmt.Errorf("resource.Delete %s: %w", row.Slug, ErrResourceBuiltIn)
	}
	if err := r.client.Resource.DeleteOneID(id).Exec(ctx); err != nil {
		return fmt.Errorf("resource.Delete: %w", err)
	}
	return nil
}
```

`ErrResourceReferenced` has no producer yet — the first kind that owns references raises it. It is declared now so the vocabulary is in one place rather than being invented twice.

- [ ] **Step 5: Run the tests**

Run: `cd server && go test -race ./internal/db/repo/ -run 'TestResource' -count=1 -v`
Expected: PASS — all seven test functions.

- [ ] **Step 6: Reset ent, then commit**

```bash
git checkout -- server/internal/db/ent/
gofmt -l server/internal/db/repo/
git add server/internal/db/repo/
git commit -m "feat(db): refuse to delete built-in resources

The spawner repository was the only one that refused a delete for a named
reason. The registry uses the same shape, so a refusal says which rule
applied instead of surfacing a bare constraint error."
```

---

### Task 9: Link plugins to the registry

The `plugin` table keeps its schema and gains a `resource_id`. Its primary key is the manifest id — a human-authored key — so the registry row carries that value in `origin_ref` and the generated UUID becomes the stable identity.

**Files:**
- Modify: `server/internal/db/ent/schema/plugin.go` (add `resource_id`)
- Create: `server/internal/db/repo/plugin_resource.go`
- Test: `server/internal/db/repo/plugin_resource_test.go`
- Regenerated (commit as part of this task): `server/internal/db/ent/**`

**Interfaces:**
- Consumes: `repo.ResourceRepo` (Task 4), `repo.ResourceKindApplication`, the state and origin constants.
- Produces: `repo.ReconcilePluginResources(ctx context.Context, resources ResourceRepo, plugins PluginRepo, client *ent.Client) (int, error)` — returns the number of plugin rows linked, and links every plugin that has no `resource_id` yet.

- [ ] **Step 1: Add the column**

In `server/internal/db/ent/schema/plugin.go`, add to `Fields()`:

```go
		// resource_id links this plugin to its registry identity row. Empty on
		// rows written before the registry existed; the boot reconciler fills it.
		field.String("resource_id").Default(""),
```

- [ ] **Step 2: Regenerate ent and verify the column**

Run: `cd server && go generate ./internal/db/ent/`
Run: `grep -n 'resource_id' server/internal/db/ent/migrate/schema.go | head -5`
Expected: the column appears in `PluginsColumns` with a default.

- [ ] **Step 3: Write the failing test**

Create `server/internal/db/repo/plugin_resource_test.go`:

```go
package repo_test

import (
	"context"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func TestReconcilePluginResourcesIsIdempotent(t *testing.T) {
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Client.Close() })
	ctx := context.Background()

	pluginRepo := repo.NewPluginRepo(bundle.Client)
	resourceRepo := repo.NewResourceRepo(bundle.Client)

	if _, err := pluginRepo.Upsert(ctx, repo.UpsertPluginInput{
		ID:      "github-oauth",
		Name:    "GitHub OAuth",
		Version: "1.0.0",
	}); err != nil {
		t.Fatalf("seed plugin: %v", err)
	}

	n, err := repo.ReconcilePluginResources(ctx, resourceRepo, pluginRepo, bundle.Client)
	if err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if n != 1 {
		t.Errorf("first reconcile linked %d plugins, want 1", n)
	}

	again, err := repo.ReconcilePluginResources(ctx, resourceRepo, pluginRepo, bundle.Client)
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if again != 0 {
		t.Errorf("second reconcile linked %d plugins, want 0 — it must be idempotent", again)
	}

	res, err := resourceRepo.Get(ctx, repo.ResourceKindApplication, repo.GlobalScope(), "github-oauth")
	if err != nil {
		t.Fatalf("registry row missing after reconcile: %v", err)
	}
	if res.OriginRef != "github-oauth" {
		t.Errorf("origin_ref = %q, want the manifest id", res.OriginRef)
	}

	rows, err := resourceRepo.ListForKind(ctx, repo.ResourceKindApplication)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("expected exactly 1 application resource, got %d", len(rows))
	}
}
```

Check the exact field names of `repo.UpsertPluginInput` in `server/internal/db/repo/plugin_repo.go:12-14` before running, and adjust the literal if they differ.

- [ ] **Step 4: Run test to verify it fails**

Run: `cd server && go test ./internal/db/repo/ -run 'TestReconcilePluginResources' -count=1`
Expected: FAIL — compile error, `undefined: repo.ReconcilePluginResources`.

- [ ] **Step 5: Write the implementation**

Create `server/internal/db/repo/plugin_resource.go`:

```go
package repo

import (
	"context"
	"fmt"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/plugin"
)

// ReconcilePluginResources gives every plugin row a registry identity. A plugin
// primary key is its manifest id — a human-authored value — so the registry row
// records it in origin_ref and the generated UUID becomes the stable identity
// that survives a manifest rename.
//
// Idempotent: a plugin that already carries a resource_id is skipped, so this
// runs on every boot and returns 0 once the tree is settled.
func ReconcilePluginResources(ctx context.Context, resources ResourceRepo, plugins PluginRepo, client *ent.Client) (int, error) {
	rows, err := client.Plugin.Query().Where(plugin.ResourceIDEQ("")).All(ctx)
	if err != nil {
		return 0, fmt.Errorf("reconcile plugins: query unlinked: %w", err)
	}

	linked := 0
	for _, p := range rows {
		state := ResourceStateDiscovered
		switch {
		case p.Active:
			state = ResourceStateEnabled
		case p.InstalledAt != nil:
			state = ResourceStateDisabled
		}

		res, err := resources.Upsert(ctx, UpsertResourceInput{
			Kind:      ResourceKindApplication,
			Slug:      p.ID,
			Name:      p.Name,
			Scope:     GlobalScope(),
			State:     state,
			Version:   p.Version,
			Origin:    ResourceOriginLocal,
			OriginRef: p.ID,
		})
		if err != nil {
			return linked, fmt.Errorf("reconcile plugin %s: %w", p.ID, err)
		}
		if err := client.Plugin.UpdateOneID(p.ID).SetResourceID(res.ID).Exec(ctx); err != nil {
			return linked, fmt.Errorf("reconcile plugin %s: link: %w", p.ID, err)
		}
		linked++
	}
	return linked, nil
}
```

The state mapping reproduces the derivation documented in `server/internal/db/ent/schema/plugin.go:11-13`: no `installed_at` means discovered, installed but inactive means disabled, active means enabled.

- [ ] **Step 6: Run the tests**

Run: `cd server && go build ./... && go test -race ./internal/db/repo/ -count=1`
Expected: PASS.

- [ ] **Step 7: Commit, including the regenerated ent tree**

```bash
gofmt -l server/internal/db/repo/ server/internal/db/ent/schema/plugin.go
cd server && go vet ./... && cd ..
git add server/internal/db/ent/ server/internal/db/repo/ 
git commit -m "feat(db): link plugin rows to registry identities

A plugin's primary key is its manifest id, which a human writes and can
rename. The registry row records it in origin_ref and a generated UUID becomes
the identity other tables can point at.

The reconciler maps the plugin table's documented state derivation — no
installed_at means discovered, installed but inactive means disabled, active
means enabled — onto the explicit state column, and skips rows that already
carry a link so it is safe on every boot."
```

---

### Task 10: Wire the registry into boot

**Files:**
- Modify: `server/serverapp/di.go` (construct the repo, run the reconciler)
- Test: `server/serverapp/di_registry_test.go`

**Interfaces:**
- Consumes: `repo.NewResourceRepo`, `repo.ReconcilePluginResources` (Tasks 4 and 8).
- Produces: nothing new for later tasks in this plan. Plan 2 (Capability Gate) consumes `repo.ResourceRepo` from the DI container.

- [ ] **Step 1: Write the failing test**

Create `server/serverapp/di_registry_test.go`:

```go
package serverapp_test

import (
	"context"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// TestReconcileOnBootLinksExistingPlugins proves the boot path leaves no plugin
// row without a registry identity. It exercises the repo layer directly rather
// than booting a server, because the assertion is about data, not wiring order.
func TestReconcileOnBootLinksExistingPlugins(t *testing.T) {
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Client.Close() })
	ctx := context.Background()

	plugins := repo.NewPluginRepo(bundle.Client)
	resources := repo.NewResourceRepo(bundle.Client)
	if _, err := plugins.Upsert(ctx, repo.UpsertPluginInput{
		ID:      "voice-whisper",
		Name:    "Whisper Voice",
		Version: "0.2.0",
	}); err != nil {
		t.Fatalf("seed plugin: %v", err)
	}

	if _, err := repo.ReconcilePluginResources(ctx, resources, plugins, bundle.Client); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if _, err := resources.Get(ctx, repo.ResourceKindApplication, repo.GlobalScope(), "voice-whisper"); err != nil {
		t.Errorf("plugin has no registry identity after boot reconcile: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./serverapp/ -run 'TestReconcileOnBoot' -count=1`
Expected: FAIL — it will actually PASS once Tasks 4 and 8 are merged, because it exercises the repo layer directly. Record the PASS; its job is to lock the invariant, and Step 3 makes boot uphold it.

- [ ] **Step 3: Wire it into DI**

In `server/serverapp/di.go`, next to where `pluginRepo` is constructed (around line 231), add:

```go
		resourceRepo := repo.NewResourceRepo(entClient)
		if linked, err := repo.ReconcilePluginResources(ctx, resourceRepo, pluginRepo, entClient); err != nil {
			slog.Warn("registry: plugin reconcile failed", "err", err)
		} else if linked > 0 {
			slog.Info("registry: linked plugins to registry identities", "count", linked)
		}
```

A reconcile failure is logged, not fatal: a plugin without a registry row still works exactly as it does today, and refusing to boot over a bookkeeping step would be worse than the gap it reports.

Confirm `slog` is already imported in `di.go`; add it if not.

- [ ] **Step 4: Verify boot**

Run: `cd server && go build ./... && go vet ./... && go test -race -count=1 ./serverapp/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git checkout -- server/internal/db/ent/
gofmt -l server/serverapp/di.go server/serverapp/di_registry_test.go
git add server/serverapp/
git commit -m "feat(serverapp): reconcile plugins into the registry on boot

Runs on every start and returns immediately once every plugin carries a link.
A failure is logged rather than fatal: a plugin without a registry row behaves
exactly as it does today, so refusing to boot would cost more than the gap it
reports."
```

---

### Task 11: Documentation and the full gate

**Files:**
- Modify: `CHANGELOG.md`
- Test: none — this task's deliverable is the green gate.

**Interfaces:**
- Consumes: everything above.
- Produces: nothing.

- [ ] **Step 1: Add the changelog entry**

Under the `## [Unreleased]` heading in `CHANGELOG.md`, in its `### Added` section (create the section if it is absent, keeping Keep a Changelog headings):

```markdown
- Internal resource registry: every managed resource now has one identity row carrying kind, slug, scope, node and lifecycle state. Existing plugins are reconciled into it on boot. No user-facing behaviour changes yet — this is the foundation the capability gate and the memory store build on.
```

- [ ] **Step 2: Run the full gate**

Run each and paste the raw output — a summary is not evidence:

```bash
cd server && go build ./...
cd server && go vet ./...
cd sdk && go vet ./...
task test
gofmt -l $(git diff --name-only origin/main...HEAD | grep '\.go$')
```

Expected: `go build` and both `go vet` runs silent; `task test` all packages `ok`; `gofmt -l` empty.

- [ ] **Step 3: Verify the ent tree matches the intended change**

Run: `git status --short server/internal/db/ent/`

Expected: empty. `task test` regenerates the tree; Tasks 3 and 8 committed the regeneration deliberately, and anything appearing now is test-run noise. If output appears, run `git checkout -- server/internal/db/ent/` and re-check.

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs: record the resource registry in the changelog"
```

- [ ] **Step 5: Open the pull request**

```bash
git push -u origin <branch-name>
gh pr create --base main --title "feat: resource registry foundation" --body "<summary plus the pasted gate output>"
```

The PR body must include the raw gate output from Step 2. Do not merge on a red or unfinished CI run.

---

## Out of scope for this plan

Recorded so the next plan does not have to rediscover the boundary.

| Item | Where it belongs |
|---|---|
| Capability and grant tables, the decision function, the three enforcers | Plan 2 (K2) |
| Memory store, retrieval, push and pull delivery | Plan 3 (K3) |
| Obsidian application, effort tuning | Plan 4 (A1, S2) |
| Migrating existing schemas onto `IDTimestampsMixin` | Regenerates ent for no behavioural gain; do it when a schema is touched for another reason |
| Converting `SpawnerRepo` and `ProjectRepo` to named input structs | Real Clean Code debt, but it blocks nothing here. Carry it with the next change that touches those repos |
| Migrating `coord_lock` and `scratchpad` namespaces onto `Scope` | They work and nothing in the MVP touches them |
| Marking a registry row `orphaned` when its kind row disappears | The spec's failure table names it, but with one kind reconciled there is no producer yet. The state value exists; the detector lands with the second kind |
| An HTTP endpoint over the registry | Nothing consumes it yet. YAGNI until the shell needs it |
| Versioned migration files | A known gap independent of this work |
| Adopting `WithTx` in the repositories that still hand-roll transactions | `project_folder_repo.go`, `task_repo.go`, `coord_lock_repo.go`, `drift_alert_repo.go`, `eval_metric_repo.go`, `agent_cost_trend_repo.go`, `permission_preset_repo.go`, `user_repo.go` — the spec called for one idiom, this plan only created the helper |
| Routing the hardcoded slug-pattern message strings through `validation.SlugPatternMessage` | `api/projects/handler.go:251,314` and `api/spawners/handler.go:168,253` — not even textually identical to the constant today |
