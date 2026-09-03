# AgenticOS K4 — Skill Materializer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce, on this node, the `SKILL.md` files the agent runtimes read, from skill resources the database owns — writing only files the system itself wrote, refusing every file a human touched, and never writing at all without the node lease.

**Architecture:** A skill resource gains content (`skill`, keyed by `resource_id`) and per-target bookkeeping (`materialization`, keyed by `(resource_id, target_key)`). A `Resolver` turns this node's Claude config dirs and enabled provider config dirs into a target list — the node × config dir × provider cross product — each target naming its format adapter. `RenderClaudeSkill` produces the file bytes with an ownership marker in frontmatter; `SkillPath` builds the only paths that may be created, gated by `validation.IsValidSlug`. `Classify` compares the file on disk against the recorded hash and returns one of five outcomes; only `created` and `repaired` reach `Apply`, and only while the `coord_lock` lease `("materialize", <node_id>)` is held. Everything else — `unchanged`, `conflict`, `foreign`, `unsupported`, `failed` — is recorded and reported, never performed. `POST /api/skills/materialize` is the trigger, dry-run by default.

**Tech Stack:** Go 1.26 (chi, ent ORM, modernc/sqlite, cobra), Vue 3 + TypeScript SPA (Vite, pnpm, Vitest) — this plan is Go-only; it adds no UI.

**Spec:** `docs/superpowers/specs/2026-08-27-agenticos-materializer-design.md` (parent: `2026-08-27-agenticos-overview-design.md`, unit K4)

## Global Constraints

- Server MUST bind to `127.0.0.1`. Never `0.0.0.0`.
- Never run `go test ./...` or `task test` while implementing — both regenerate `server/internal/db/ent/`. Use package-scoped test paths. Task 1 regenerates ent deliberately; that is the only task where a changed `server/internal/db/ent/` belongs in the commit.
- ent regeneration MUST use the project's own path: `cd server && go generate ./internal/db/ent/` (it carries `--feature sql/upsert`). Verify after regen: `grep -rl "OnConflict" server/internal/db/ent/ | head` must print files. Then restore `server/go.sum` from HEAD — `go generate` pulls codegen-only dependencies into it that `go build` does not need.
- After regen, restore `server/internal/db/ent/runtime/runtime.go` from HEAD if it lost its `Version`/`Sum` constants. A local ent version differing from the committed one strips them; that diff does not belong in the commit.
- `gofmt -l <pkg>` is mandatory before every commit. CI runs `golangci-lint fmt --diff`, which fails on struct-literal alignment that `go build`, `go vet` and `go test` all pass.
- Run `go vet ./...` module-wide (from `server/`) before every commit — a package-scoped `go test` misses `_test.go` files in sibling packages that reference a changed exported type.
- ent auto-migrate is non-destructive and this project deliberately does not enable `WithDropColumn`. Added columns must be additive-safe with defaults.
- **No test in this plan may write outside `t.TempDir()`.** Every filesystem seam (`Resolver.ClaudeConfigDirs`, `Resolver.ProviderConfigDirs`) is injected precisely so a test can name its own directories. A test that read the real `~/.claude` would be the exact accident this component exists to prevent.
- All code, comments, commit messages, PR titles and bodies in English. Conventional Commits.

---

## Verified against the code before planning

The spec's §4 and §5 claims were re-read in the tree, not taken from prose. Where the spec's line numbers have drifted, the corrected reference is given — the content is as described in every case.

| Spec claim | Verified at | Note |
|---|---|---|
| Authorization is enumeration | `server/internal/api/config/file.go:151-174` | Exact. "Enumeration IS the allow-list." |
| Enumeration requires existence — no create | `server/internal/api/config/file.go:176-185` | Exact. `filepath.EvalSymlinks` on a non-existent path fails. |
| mtime optimistic concurrency, whole seconds | `server/internal/api/config/file.go:110-115`, `:19,27,33` | Exact. |
| `atomicWrite` without `tmp.Sync()` | `server/internal/api/config/file.go:187-210` | Exact. |
| Refuse rather than overwrite | `server/cmd/serve/hooks.go:195-198` | Exact. |
| Marker is not proof of ownership; the path is | `server/cmd/serve/hooks.go:252-263` | Spec says `:259-263`; the comment block starts at 252. |
| Three-way outcome | `server/cmd/serve/hooks.go:265-271` | Exact. |
| Foreign entry left in place | `server/cmd/serve/hooks.go:428-431` | Exact. |
| Atomic write with `Sync` and explicit mode | `server/internal/hookscript/hookscript.go:44-73` | Exact. |
| Rewriting on every install is deliberate | `server/internal/hookscript/hookscript.go:30-31` | Exact. |
| Config-dir precedence | `server/internal/cmdscope/scope.go:42-51`, `:55-57`, `:62-81` | Exact. |
| Non-Claude adapter short-circuits | `server/internal/cmdscope/scope.go:63-65` | Exact. |
| Parser's four-tier search set incl. `~/.claude-personal` | `server/internal/parser/parser.go:133-162`, exported at `:172-174` | Spec says `:127-162`. Tier 4 additionally requires `<candidate>/projects` to exist (`parser.go:157-158`). |
| `Registry.ConfigDirs()` drops non-existent dirs | `server/internal/provider/registry.go:179-204` (`isDir` at `:199`) | Exact. Also skips disabled and `IsCustom()` descriptors. |
| Claude is not a descriptor | `server/internal/provider/registry.go:131-133` | Exact. |
| Four providers, envs and defaults | `server/internal/provider/providers/{codex,gemini,junie,pi}.yaml` | Exact; all four ship `enabled: false`. |
| `IsEditableSource` | `server/internal/cmdscope/enumerate.go:96-98` | Spec says `:57-62`. |
| `sourceRank`: builtin > project > user > plugin | `server/internal/cmdscope/enumerate.go:147-158` | Spec says `:111-122`. |
| Skill name falls back to the directory name | `server/internal/cmdscope/enumerate.go:286-289` | Spec says `:251-253`. |
| Symlinks and hidden entries rejected | `server/internal/cmdscope/enumerate.go:339-364` (`listCommandFiles`), `:365-390` (`listSkillDirs`) | Spec says `:307-353`. `listSkillDirs` `Lstat`s `SKILL.md` and requires `IsRegular`. |
| `coord_lock` lease, steal-on-expiry, re-entrant | `server/internal/db/repo/coord_lock_repo.go:24-86` (`:73` is the steal/re-entry branch) | Exact. |
| `Release` by a non-holder is an error | `server/internal/db/repo/coord_lock_repo.go:96-107` | Exact. |
| `ListActive` filters `expires_at > now` (lazy expiry) | `server/internal/db/repo/coord_lock_repo.go:109-114` | Exact. |
| `skills-lock.json` is read by nothing | Only references: the file, `.gitignore:17-19`, `docs/guides/agent-skills.md` | Exact. |
| The documented `jq` snippet cannot work | `docs/guides/agent-skills.md:7,12` read `.name` (the name is the map key) and `curl` `.source` (`lx-wnk/skills`, an `owner/repo` slug) | Exact. |
| The doc contradicts the file | `agent-skills.md:21-25` lists five skills; the lock file holds seven different ones | Exact. |

**One thing the spec assumes that does not exist yet.** §5.1 says "the resource records the path it wrote" and §8 says "the registry's `origin` and `origin_ref` columns replace this" — but `ResourceKindSkill` (`server/internal/db/repo/resource_repo.go:19`) has no production writer at all. `server/internal/api/resources/handler.go:196` states it outright: "nothing writes skill yet." A `resource` row is identity only, by design (`server/internal/db/ent/schema/resource.go:8-12`: "Kind-specific data stays in the kind's own table"). **There is therefore no skill content anywhere and nowhere to record a written path.** Task 1 creates both; see its design note.

---

## Decisions this plan makes because the spec is silent

| Question | Decision | Why |
|---|---|---|
| Where skill content lives | New `skill` table keyed by `resource_id` | Mirrors `memory_entry.space_id` (`schema/memory_entry.go:23-28`): a plain id, not an edge, so the generic `resource` table gains no kind-specific reverse reference. |
| Where the written path and hash live | New `materialization` table, unique on `(resource_id, target_key)` | One skill has N targets (§2.3), so the record cannot live on the resource row. §5.1 and §9 both need it persisted. |
| Lease owner identity | A per-process id minted at construction: `"materializer:" + uuid.NewString()` | `coord_lock.owner_task_id` wants an owner string and this project has no instance id anywhere (grepped: none). Per-process is the right granularity — §7's contention case is two dashboard instances on one machine. |
| Trigger | `POST /api/skills/materialize`, session-authenticated, **dry-run by default** | The spec names no trigger. No boot-time run and no scheduler: a component with real destructive potential does not run unattended before a human has read its report. `dryRun` absent means `true`. |
| Capability gate | **None.** The lease and the refusal rules are the gate | The overview's §11 mitigation is "lease-gated; reports conflicts instead of overwriting" and names no capability. A new grant would mean a user cannot write their own skill files until they grant themselves permission, against no attacker the loopback binding does not already exclude. Recorded as a follow-up, not a gap. |
| Provider targets and scope | Every enabled non-Claude provider gets one `unsupported` target per config dir, regardless of the skill's scope | §2.3 requires a visible no-op. §3 lists no provider project-layer path template and §11 forbids emulating a format, so no provider project target is invented. |
| Application-scoped skills | No Claude target at all | §3 has path templates for `user` and `project` only. Fail closed rather than guess a third. |
| Conflict detection | Content hash decides; mtime is **not** stored | See Task 4's design note — this is the plan's one deliberate divergence from §6. |

---

## File Structure

| File | Responsibility |
|---|---|
| `server/internal/db/ent/schema/skill.go` | The body and description a skill resource materializes from |
| `server/internal/db/ent/schema/materialization.go` | What was written where, and the hash that proves we wrote it |
| `server/internal/db/repo/skill_repo.go` | Upsert/read of skill content by resource id |
| `server/internal/db/repo/materialization_repo.go` | Per-target record: get (absent is not an error), record, delete |
| `server/internal/materializer/target.go` | The node × config dir × provider cross product, and each target's stable key |
| `server/internal/materializer/render.go` | The Claude `SKILL.md` bytes, frontmatter marker included |
| `server/internal/materializer/path.go` | The only paths that may be created; the slug and symlink boundaries |
| `server/internal/materializer/classify.go` | On-disk state + record → one of the five outcomes. Reads, never writes |
| `server/internal/materializer/apply.go` | Atomic write with `Sync`, mode, and the symlinked-directory refusal |
| `server/internal/materializer/materializer.go` | The run: lease, iterate, classify, write when owned, report |
| `server/internal/api/skills/handler.go` | `POST /api/skills/materialize`, dry-run by default, single-flight |
| `server/internal/api/router.go` | Mounts the handler in the session-authenticated group |
| `server/serverapp/di.go` | Composition root: repos, resolver, owner id |
| `docs/guides/agent-skills.md`, `skills-lock.json`, `.gitignore`, `CHANGELOG.md`, `README.md`, `docs/README.md` | The lock file and its unusable snippet go, together |

---

### Task 1: Storage — skill content, and the record of what was written

**Files:**
- Create: `server/internal/db/ent/schema/skill.go`
- Create: `server/internal/db/ent/schema/materialization.go`
- Create: `server/internal/db/repo/skill_repo.go`
- Create: `server/internal/db/repo/materialization_repo.go`
- Regenerate: `server/internal/db/ent/` (deliberate, belongs in this commit)
- Test: `server/internal/db/repo/skill_repo_test.go`, `server/internal/db/repo/materialization_repo_test.go`

**Interfaces:**
- Consumes: `repo.ResourceRepo`, `repo.ResourceKindSkill` (`server/internal/db/repo/resource_repo.go:19`) — existing.
- Produces:
  - `repo.UpsertSkillInput{ResourceID, Description, Body string}`
  - `repo.SkillRepo.Upsert(ctx context.Context, in UpsertSkillInput) (*ent.Skill, error)`
  - `repo.SkillRepo.GetByResource(ctx context.Context, resourceID string) (*ent.Skill, error)`
  - `repo.NewSkillRepo(client *ent.Client) SkillRepo`
  - `repo.RecordMaterializationInput{ResourceID, TargetKey, Path, ContentHash, Outcome string}`
  - `repo.MaterializationRepo.Get(ctx context.Context, resourceID, targetKey string) (*ent.Materialization, error)` — returns `(nil, nil)` when absent
  - `repo.MaterializationRepo.Record(ctx context.Context, in RecordMaterializationInput) (*ent.Materialization, error)`
  - `repo.MaterializationRepo.ListForResource(ctx context.Context, resourceID string) ([]*ent.Materialization, error)`
  - `repo.NewMaterializationRepo(client *ent.Client) MaterializationRepo`

**Design note this task settles.** `Get` answers `(nil, nil)` for a target that has never been materialized rather than an `ent.NotFoundError`. "This node has never written this skill here" is the ordinary first state of every row in this table, not a failure, and making the caller distinguish `ent.IsNotFound` from a real storage error at every call site is how a storage outage eventually gets classified as `foreign` — which is the one outcome that silently stops writing forever.

**Design note two.** `content_hash` is the empty string on a `foreign` record. That is the load-bearing sentinel: an empty hash means *this process has never written these bytes*, which is exactly what makes a foreign file stay foreign across runs instead of being read as a conflict against a hash we never recorded. Task 4's `Classify` depends on it.

- [ ] **Step 1: Write the failing tests**

Create `server/internal/db/repo/skill_repo_test.go`:

```go
package repo_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func newSkillRepo(t *testing.T) (repo.SkillRepo, repo.ResourceRepo, context.Context) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	return repo.NewSkillRepo(bundle.Client), repo.NewResourceRepo(bundle.Client), context.Background()
}

func TestSkill_UpsertThenRead(t *testing.T) {
	skills, resources, ctx := newSkillRepo(t)

	res, err := resources.Upsert(ctx, repo.UpsertResourceInput{
		Kind: repo.ResourceKindSkill, Slug: "code-review", Name: "Code Review",
		Scope: repo.GlobalScope(), State: repo.ResourceStateEnabled,
	})
	require.NoError(t, err)

	_, err = skills.Upsert(ctx, repo.UpsertSkillInput{
		ResourceID: res.ID, Description: "Review a diff", Body: "# Code Review\n\nRead the diff.\n",
	})
	require.NoError(t, err)

	got, err := skills.GetByResource(ctx, res.ID)
	require.NoError(t, err)
	require.Equal(t, "Review a diff", got.Description)
	require.Equal(t, "# Code Review\n\nRead the diff.\n", got.Body)
}

func TestSkill_UpsertReplacesBody(t *testing.T) {
	skills, resources, ctx := newSkillRepo(t)

	res, err := resources.Upsert(ctx, repo.UpsertResourceInput{
		Kind: repo.ResourceKindSkill, Slug: "code-review",
		Scope: repo.GlobalScope(), State: repo.ResourceStateEnabled,
	})
	require.NoError(t, err)

	for _, body := range []string{"first", "second"} {
		_, err = skills.Upsert(ctx, repo.UpsertSkillInput{ResourceID: res.ID, Body: body})
		require.NoError(t, err)
	}

	got, err := skills.GetByResource(ctx, res.ID)
	require.NoError(t, err)
	require.Equal(t, "second", got.Body, "a second upsert must replace, never create a second row")
}

func TestSkill_GetByResourceIsNotFoundForUnknownResource(t *testing.T) {
	skills, _, ctx := newSkillRepo(t)
	_, err := skills.GetByResource(ctx, "no-such-resource")
	require.Error(t, err, "a skill resource with no content is a real error: nothing can be materialized from it")
}
```

Create `server/internal/db/repo/materialization_repo_test.go`:

```go
package repo_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func newMaterializationRepo(t *testing.T) (repo.MaterializationRepo, context.Context) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	return repo.NewMaterializationRepo(bundle.Client), context.Background()
}

func TestMaterialization_GetAbsentIsNotAnError(t *testing.T) {
	r, ctx := newMaterializationRepo(t)

	got, err := r.Get(ctx, "res-1", "claude|user|/tmp/cfg")
	require.NoError(t, err, "never materialized here is an ordinary state, not a failure")
	require.Nil(t, got)
}

func TestMaterialization_RecordThenGet(t *testing.T) {
	r, ctx := newMaterializationRepo(t)

	_, err := r.Record(ctx, repo.RecordMaterializationInput{
		ResourceID: "res-1", TargetKey: "claude|user|/tmp/cfg",
		Path: "/tmp/cfg/skills/review/SKILL.md", ContentHash: "abc", Outcome: "created",
	})
	require.NoError(t, err)

	got, err := r.Get(ctx, "res-1", "claude|user|/tmp/cfg")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "/tmp/cfg/skills/review/SKILL.md", got.Path)
	require.Equal(t, "abc", got.ContentHash)
	require.Equal(t, "created", got.Outcome)
}

func TestMaterialization_RecordIsIdempotentPerTarget(t *testing.T) {
	r, ctx := newMaterializationRepo(t)

	for _, hash := range []string{"abc", "def"} {
		_, err := r.Record(ctx, repo.RecordMaterializationInput{
			ResourceID: "res-1", TargetKey: "claude|user|/tmp/cfg",
			Path: "/tmp/cfg/skills/review/SKILL.md", ContentHash: hash, Outcome: "repaired",
		})
		require.NoError(t, err)
	}

	rows, err := r.ListForResource(ctx, "res-1")
	require.NoError(t, err)
	require.Len(t, rows, 1, "one row per (resource, target) — a second row would orphan the first hash")
	require.Equal(t, "def", rows[0].ContentHash)
}

func TestMaterialization_TargetsAreIndependent(t *testing.T) {
	r, ctx := newMaterializationRepo(t)

	for _, key := range []string{"claude|user|/tmp/a", "claude|user|/tmp/b"} {
		_, err := r.Record(ctx, repo.RecordMaterializationInput{
			ResourceID: "res-1", TargetKey: key, Path: key + "/SKILL.md", ContentHash: "h", Outcome: "created",
		})
		require.NoError(t, err)
	}

	rows, err := r.ListForResource(ctx, "res-1")
	require.NoError(t, err)
	require.Len(t, rows, 2, "one skill, two config dirs, two records")
}

func TestMaterialization_ForeignRecordCarriesAnEmptyHash(t *testing.T) {
	r, ctx := newMaterializationRepo(t)

	_, err := r.Record(ctx, repo.RecordMaterializationInput{
		ResourceID: "res-1", TargetKey: "claude|user|/tmp/cfg",
		Path: "/tmp/cfg/skills/review/SKILL.md", Outcome: "foreign",
	})
	require.NoError(t, err)

	got, err := r.Get(ctx, "res-1", "claude|user|/tmp/cfg")
	require.NoError(t, err)
	require.Equal(t, "", got.ContentHash, "an empty hash is what keeps a foreign file foreign on the next run")
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd server && go test ./internal/db/repo/ -run 'TestSkill_|TestMaterialization_' -v`

Expected: compile failure — `repo.SkillRepo`, `repo.NewSkillRepo`, `repo.UpsertSkillInput`, `repo.MaterializationRepo`, `repo.NewMaterializationRepo`, `repo.RecordMaterializationInput` are all undefined.

- [ ] **Step 3: Add the two schemas**

Create `server/internal/db/ent/schema/skill.go`:

```go
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Skill is the content half of a skill resource: what the materializer turns
// into a SKILL.md. The identity half — slug, scope, node, state, origin —
// lives on the resource row this one points at, which is why there is no slug
// column here to disagree with it.
type Skill struct{ ent.Schema }

// Mixin of the Skill.
func (Skill) Mixin() []ent.Mixin {
	return []ent.Mixin{IDTimestampsMixin{}}
}

// Fields of the Skill.
func (Skill) Fields() []ent.Field {
	return []ent.Field{
		// resource_id is the resource row (kind = skill) this content belongs
		// to. A plain id, not an edge, for the reason memory_entry.space_id
		// gives: resource is the shared identity table for every ARMS kind, and
		// an edge back would tie that generic table to one kind's reverse
		// reference.
		field.String("resource_id").Immutable().Unique(),
		// description becomes the SKILL.md frontmatter `description:`, which is
		// what a runtime shows in its skill menu.
		field.String("description").Default(""),
		// body is everything below the frontmatter. The frontmatter itself is
		// rendered, never stored: it carries the ownership marker, and a stored
		// marker could be edited into something the materializer does not own.
		field.Text("body").Default(""),
	}
}

// Indexes of the Skill.
func (Skill) Indexes() []ent.Index {
	return []ent.Index{index.Fields("resource_id").Unique()}
}
```

Create `server/internal/db/ent/schema/materialization.go`:

```go
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Materialization records what one resource produced at one target. It is the
// ownership proof: cmd/serve/hooks.go:252-263 established for settings.json
// that a marker inside a file is not proof of ownership because a copy carries
// it too — the path is. This table holds those paths.
type Materialization struct{ ent.Schema }

// Mixin of the Materialization.
func (Materialization) Mixin() []ent.Mixin {
	return []ent.Mixin{IDTimestampsMixin{}}
}

// Fields of the Materialization.
func (Materialization) Fields() []ent.Field {
	return []ent.Field{
		field.String("resource_id").Immutable(),
		// target_key identifies the target within the node × config dir ×
		// provider cross product: "<provider>|<layer>|<root>". Stable across
		// runs by construction — a changed key orphans this row, and the next
		// run would then report the file it wrote itself as foreign.
		field.String("target_key").Immutable(),
		field.String("path"),
		// content_hash is the SHA-256 of the bytes last written here. The empty
		// string means this node has never written these bytes — which is what
		// a foreign row records, and what keeps a foreign file foreign instead
		// of reading as a conflict against a hash that was never taken.
		field.String("content_hash").Default(""),
		// outcome is the last classification: created | unchanged | repaired |
		// conflict | foreign. Stored so a report can say when a conflict was
		// first seen without re-deriving it from the filesystem.
		field.String("outcome").Default(""),
	}
}

// Indexes of the Materialization.
func (Materialization) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("resource_id", "target_key").Unique(),
		index.Fields("outcome"),
	}
}
```

- [ ] **Step 4: Regenerate ent**

```bash
cd server && go generate ./internal/db/ent/
grep -rl "OnConflict" internal/db/ent/ | head
```

Expected: the `grep` prints files. Then restore `server/go.sum` from HEAD, and restore `internal/db/ent/runtime/runtime.go` from HEAD if it lost its `Version`/`Sum` constants.

- [ ] **Step 5: Write the two repos**

Create `server/internal/db/repo/skill_repo.go`:

```go
package repo

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/skill"
)

// UpsertSkillInput is the named input for SkillRepo.Upsert.
type UpsertSkillInput struct {
	ResourceID  string
	Description string
	Body        string
}

// SkillRepo persists the content half of a skill resource.
type SkillRepo interface {
	Upsert(ctx context.Context, in UpsertSkillInput) (*ent.Skill, error)
	// GetByResource returns an error when the resource has no content row.
	// Unlike MaterializationRepo.Get, absence here is a real fault: a skill
	// resource with nothing to render cannot be materialized at all.
	GetByResource(ctx context.Context, resourceID string) (*ent.Skill, error)
}

type entSkillRepo struct{ client *ent.Client }

// NewSkillRepo returns a SkillRepo backed by the ent client.
func NewSkillRepo(client *ent.Client) SkillRepo { return &entSkillRepo{client: client} }

func (r *entSkillRepo) Upsert(ctx context.Context, in UpsertSkillInput) (*ent.Skill, error) {
	if in.ResourceID == "" {
		return nil, fmt.Errorf("skill.Upsert: resource id is required")
	}
	existing, err := r.client.Skill.Query().Where(skill.ResourceID(in.ResourceID)).Only(ctx)
	switch {
	case err == nil:
		row, uerr := existing.Update().
			SetDescription(in.Description).
			SetBody(in.Body).
			Save(ctx)
		if uerr != nil {
			return nil, fmt.Errorf("skill.Upsert update: %w", uerr)
		}
		return row, nil
	case ent.IsNotFound(err):
		row, cerr := r.client.Skill.Create().
			SetID(uuid.NewString()).
			SetResourceID(in.ResourceID).
			SetDescription(in.Description).
			SetBody(in.Body).
			Save(ctx)
		if cerr != nil {
			return nil, fmt.Errorf("skill.Upsert insert: %w", cerr)
		}
		return row, nil
	default:
		return nil, fmt.Errorf("skill.Upsert query: %w", err)
	}
}

func (r *entSkillRepo) GetByResource(ctx context.Context, resourceID string) (*ent.Skill, error) {
	row, err := r.client.Skill.Query().Where(skill.ResourceID(resourceID)).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("skill.GetByResource %s: %w", resourceID, err)
	}
	return row, nil
}
```

Create `server/internal/db/repo/materialization_repo.go`:

```go
package repo

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/materialization"
)

// RecordMaterializationInput is the named input for MaterializationRepo.Record.
// A ContentHash of "" means the row records something this node did not write.
type RecordMaterializationInput struct {
	ResourceID  string
	TargetKey   string
	Path        string
	ContentHash string
	Outcome     string
}

// MaterializationRepo persists what was produced where.
type MaterializationRepo interface {
	// Get returns (nil, nil) when the target has never been recorded. Absence
	// is the ordinary first state of every target, not a failure — and forcing
	// each call site to tell ent.IsNotFound from a storage fault is how an
	// outage ends up classified as "foreign", the one outcome that stops
	// writing permanently.
	Get(ctx context.Context, resourceID, targetKey string) (*ent.Materialization, error)
	Record(ctx context.Context, in RecordMaterializationInput) (*ent.Materialization, error)
	ListForResource(ctx context.Context, resourceID string) ([]*ent.Materialization, error)
}

type entMaterializationRepo struct{ client *ent.Client }

// NewMaterializationRepo returns a MaterializationRepo backed by the ent client.
func NewMaterializationRepo(client *ent.Client) MaterializationRepo {
	return &entMaterializationRepo{client: client}
}

func (r *entMaterializationRepo) Get(ctx context.Context, resourceID, targetKey string) (*ent.Materialization, error) {
	row, err := r.client.Materialization.Query().
		Where(materialization.ResourceID(resourceID), materialization.TargetKey(targetKey)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("materialization.Get %s/%s: %w", resourceID, targetKey, err)
	}
	return row, nil
}

func (r *entMaterializationRepo) Record(ctx context.Context, in RecordMaterializationInput) (*ent.Materialization, error) {
	if in.ResourceID == "" || in.TargetKey == "" {
		return nil, fmt.Errorf("materialization.Record: resource id and target key are required")
	}
	existing, err := r.Get(ctx, in.ResourceID, in.TargetKey)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		row, uerr := existing.Update().
			SetPath(in.Path).
			SetContentHash(in.ContentHash).
			SetOutcome(in.Outcome).
			Save(ctx)
		if uerr != nil {
			return nil, fmt.Errorf("materialization.Record update: %w", uerr)
		}
		return row, nil
	}
	row, cerr := r.client.Materialization.Create().
		SetID(uuid.NewString()).
		SetResourceID(in.ResourceID).
		SetTargetKey(in.TargetKey).
		SetPath(in.Path).
		SetContentHash(in.ContentHash).
		SetOutcome(in.Outcome).
		Save(ctx)
	if cerr != nil {
		return nil, fmt.Errorf("materialization.Record insert: %w", cerr)
	}
	return row, nil
}

func (r *entMaterializationRepo) ListForResource(ctx context.Context, resourceID string) ([]*ent.Materialization, error) {
	rows, err := r.client.Materialization.Query().
		Where(materialization.ResourceID(resourceID)).
		Order(ent.Asc(materialization.FieldTargetKey)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("materialization.ListForResource %s: %w", resourceID, err)
	}
	return rows, nil
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd server && go test ./internal/db/repo/ -run 'TestSkill_|TestMaterialization_' -v`
Expected: all nine tests pass.

- [ ] **Step 7: Commit**

```bash
cd server && gofmt -l ./internal/db/ && go vet ./...
```
Expected: `gofmt -l` prints nothing; `go vet` is silent.

Commit message: `feat: give a skill resource content and a record of what it wrote`

---

### Task 2: Targets — the cross product, resolved once and named literally

**Files:**
- Create: `server/internal/materializer/target.go`
- Test: `server/internal/materializer/target_test.go`

**Interfaces:**
- Consumes: `parser.ProviderConfigDir{Provider sdk.Provider; Path string}` (`server/internal/parser/parser.go:177-180`), `repo.Scope`, `repo.ScopeGlobal|ScopeProject|ScopeApplication` (`server/internal/db/repo/scope.go:8-21`), `sdk.ProviderClaude` (`sdk/types.go:86`).
- Produces:
  - `materializer.Target{NodeID, Provider, Layer, Root, Adapter string}`
  - `materializer.Target.Key() string`
  - `materializer.LayerUser = "user"`, `LayerProject = "project"`
  - `materializer.AdapterClaude = "claude"`, `AdapterNone = "none"`
  - `materializer.Resolver{NodeID string; ClaudeConfigDirs func() []string; ProviderConfigDirs func() []parser.ProviderConfigDir}`
  - `materializer.Resolver.Targets(scope repo.Scope) []Target`

**Design note this task settles.** `Resolver` takes both directory sources as functions rather than calling `parser.AllClaudeConfigDirs` and `providerRegistry.ConfigDirs` directly. The production wiring in Task 6 passes exactly those two. The seam exists so the test suite of the one component that can overwrite a person's files never touches a real config directory.

**Design note two.** Provider targets are emitted for *every* scope, including project scope. §2.3 requires a visible no-op — "a user who authors a skill and sees 'not materialized for Codex — no skill format' has learned something true. A user who sees nothing has been misled" — and that is as true of a project skill as of a global one. No provider *project* target is invented, because §3 gives no path template for one and §11 forbids emulating a format that does not exist; the provider's single user-layer no-op says the whole truth.

- [ ] **Step 1: Write the failing test**

Create `server/internal/materializer/target_test.go`:

```go
package materializer_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/materializer"
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

// fixture builds two Claude config dirs and four provider config dirs on disk,
// mirroring the cross product spec §10 asks to be written out literally.
func fixture(t *testing.T) (claude []string, providers []parser.ProviderConfigDir, root string) {
	t.Helper()
	root = t.TempDir()
	mk := func(name string) string {
		p := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(p, 0o700))
		return p
	}
	claude = []string{mk(".claude"), mk(".claude-personal")}
	providers = []parser.ProviderConfigDir{
		{Provider: sdk.Provider("codex"), Path: mk(".codex")},
		{Provider: sdk.Provider("gemini"), Path: mk(".gemini")},
		{Provider: sdk.Provider("junie"), Path: mk(".junie")},
		{Provider: sdk.Provider("pi"), Path: mk(".pi")},
	}
	return claude, providers, root
}

func newResolver(t *testing.T) (materializer.Resolver, string) {
	t.Helper()
	claude, providers, root := fixture(t)
	return materializer.Resolver{
		NodeID:             "local",
		ClaudeConfigDirs:   func() []string { return claude },
		ProviderConfigDirs: func() []parser.ProviderConfigDir { return providers },
	}, root
}

func keys(targets []materializer.Target) []string {
	out := make([]string, 0, len(targets))
	for _, t := range targets {
		out = append(out, t.Key())
	}
	return out
}

func TestTargets_GlobalScopeIsTheFullCrossProduct(t *testing.T) {
	r, root := newResolver(t)

	got := r.Targets(repo.GlobalScope())

	require.Equal(t, []string{
		"claude|user|" + filepath.Join(root, ".claude"),
		"claude|user|" + filepath.Join(root, ".claude-personal"),
		"codex|user|" + filepath.Join(root, ".codex"),
		"gemini|user|" + filepath.Join(root, ".gemini"),
		"junie|user|" + filepath.Join(root, ".junie"),
		"pi|user|" + filepath.Join(root, ".pi"),
	}, keys(got))
}

func TestTargets_OnlyClaudeHasASkillFormat(t *testing.T) {
	r, _ := newResolver(t)

	for _, target := range r.Targets(repo.GlobalScope()) {
		if target.Provider == "claude" {
			require.Equal(t, materializer.AdapterClaude, target.Adapter)
			continue
		}
		require.Equal(t, materializer.AdapterNone, target.Adapter,
			"none of the four providers ships a SKILL.md equivalent")
	}
}

func TestTargets_ProjectScopeTargetsTheProjectDirAndStillReportsProviders(t *testing.T) {
	r, root := newResolver(t)
	project := filepath.Join(root, "work", "repo")
	require.NoError(t, os.MkdirAll(project, 0o700))

	got := r.Targets(repo.ProjectScope(project))

	require.Equal(t, []string{
		"claude|project|" + project,
		"codex|user|" + filepath.Join(root, ".codex"),
		"gemini|user|" + filepath.Join(root, ".gemini"),
		"junie|user|" + filepath.Join(root, ".junie"),
		"pi|user|" + filepath.Join(root, ".pi"),
	}, keys(got))
}

func TestTargets_NonExistentDirectoriesAreSkippedNotCreated(t *testing.T) {
	root := t.TempDir()
	present := filepath.Join(root, ".claude")
	require.NoError(t, os.MkdirAll(present, 0o700))

	r := materializer.Resolver{
		NodeID:             "local",
		ClaudeConfigDirs:   func() []string { return []string{present, filepath.Join(root, ".claude-work")} },
		ProviderConfigDirs: func() []parser.ProviderConfigDir { return nil },
	}

	require.Equal(t, []string{"claude|user|" + present}, keys(r.Targets(repo.GlobalScope())),
		"inventing a config directory for a runtime the user has not set up is not this component's business")
}

func TestTargets_ProjectScopeWithAMissingProjectDirYieldsNoClaudeTarget(t *testing.T) {
	r, root := newResolver(t)

	got := r.Targets(repo.ProjectScope(filepath.Join(root, "gone")))

	for _, target := range got {
		require.NotEqual(t, materializer.LayerProject, target.Layer)
	}
}

func TestTargets_ApplicationScopeHasNoPathTemplate(t *testing.T) {
	r, _ := newResolver(t)

	got := r.Targets(repo.ApplicationScope("app-1"))

	for _, target := range got {
		require.Equal(t, materializer.AdapterNone, target.Adapter,
			"spec §3 has templates for user and project only; a third is not guessed")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd server && go test ./internal/materializer/ -v`
Expected: the package does not exist — `no Go files in .../internal/materializer`.

- [ ] **Step 3: Implement the resolver**

Create `server/internal/materializer/target.go`:

```go
// Package materializer produces, on this node, the files agent runtimes read
// from skill resources the database owns.
//
// It is the only component in this system that writes into the user's own
// directories, so almost all of it is about refusing to. A file it did not
// write is never touched; a file it wrote that a human has since edited stops
// the run for that resource; and without the node lease it writes nothing at
// all, whatever the caller asked for.
package materializer

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

// Layers a target can sit in. They are cmdscope's editable sources
// (cmdscope/enumerate.go:96-98); builtin and plugin are never targeted.
const (
	LayerUser    = "user"
	LayerProject = "project"
)

// Format adapters. AdapterNone is a runtime with no skill concept: its target
// is a recorded no-op, never a silent gap, and never a fabricated format.
const (
	AdapterClaude = "claude"
	AdapterNone   = "none"
)

// Target is one place on this node a skill may be materialized into. The
// target set is node × config dir × provider, not "the filesystem": a
// materializer that writes to one config dir writes to the wrong one about
// half the time on a machine that runs ~/.claude-personal.
type Target struct {
	// NodeID is the node the target lives on, repo.DefaultNodeID until the
	// node registry lands.
	NodeID string
	// Provider is "claude" or a provider-registry id ("codex", "gemini", …).
	Provider string
	// Layer is LayerUser or LayerProject.
	Layer string
	// Root is what the path template is anchored at: a config dir for the user
	// layer, a project working directory for the project layer.
	Root string
	// Adapter is AdapterClaude or AdapterNone.
	Adapter string
}

// Key identifies the target in a materialization record. It must stay stable
// across runs: a changed key orphans the record, and the next run would then
// report the file this node wrote itself as foreign.
func (t Target) Key() string { return t.Provider + "|" + t.Layer + "|" + t.Root }

// Resolver turns this node's config directories into the target list.
//
// Both directory sources are injected rather than called directly. Production
// passes parser.AllClaudeConfigDirs and Registry.ConfigDirs; a test passes its
// own temp dirs, which is the only reason the suite of the one component that
// can overwrite a person's skill file never reads a real config directory.
type Resolver struct {
	NodeID string
	// ClaudeConfigDirs returns every Claude config root on this node. Claude is
	// the always-on built-in and deliberately not a provider descriptor
	// (provider/registry.go:131-133), so its dirs come from the parser's own
	// four-tier search set (parser/parser.go:133-162) — the tier that also
	// finds ~/.claude-personal.
	ClaudeConfigDirs func() []string
	// ProviderConfigDirs returns the enabled non-Claude providers' config dirs.
	// Registry.ConfigDirs already drops the ones that do not exist
	// (provider/registry.go:199).
	ProviderConfigDirs func() []parser.ProviderConfigDir
}

// Targets returns every target a resource in scope materializes into, sorted
// by key so a report and a golden test read the same both times.
func (r Resolver) Targets(scope repo.Scope) []Target {
	out := []Target{}

	switch scope.Normalize().Kind {
	case repo.ScopeGlobal:
		for _, dir := range r.ClaudeConfigDirs() {
			if isDir(dir) {
				out = append(out, Target{
					NodeID: r.NodeID, Provider: string(sdk.ProviderClaude),
					Layer: LayerUser, Root: filepath.Clean(dir), Adapter: AdapterClaude,
				})
			}
		}
	case repo.ScopeProject:
		if isDir(scope.Ref) {
			out = append(out, Target{
				NodeID: r.NodeID, Provider: string(sdk.ProviderClaude),
				Layer: LayerProject, Root: filepath.Clean(scope.Ref), Adapter: AdapterClaude,
			})
		}
	}
	// repo.ScopeApplication falls through with no Claude target on purpose:
	// spec §3 lists path templates for the user and project layers only, and
	// guessing a third is how a file lands somewhere nothing reads it.

	// Every enabled non-Claude provider gets one recorded no-op, whatever the
	// scope. None of the four ships a SKILL.md equivalent, and a user who
	// authored a skill and sees nothing at all for Codex has been misled.
	for _, d := range r.ProviderConfigDirs() {
		out = append(out, Target{
			NodeID: r.NodeID, Provider: string(d.Provider),
			Layer: LayerUser, Root: filepath.Clean(d.Path), Adapter: AdapterNone,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out
}

// isDir reports whether path exists and is a directory. A config dir that does
// not exist is skipped, never created.
func isDir(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd server && go test ./internal/materializer/ -v`
Expected: all seven tests pass.

- [ ] **Step 5: Commit**

```bash
cd server && gofmt -l ./internal/materializer/ && go vet ./...
```

Commit message: `feat: resolve the materialization target cross product`

---

### Task 3: The Claude format, and the two path boundaries

**Files:**
- Create: `server/internal/materializer/render.go`
- Create: `server/internal/materializer/path.go`
- Test: `server/internal/materializer/render_test.go`, `server/internal/materializer/path_test.go`

**Interfaces:**
- Consumes: `materializer.Target`, `materializer.AdapterClaude`, `LayerUser`, `LayerProject` (Task 2); `validation.IsValidSlug`, `validation.SlugPatternMessage` (`server/internal/validation/slug.go:13,17`).
- Produces:
  - `materializer.MarkerKey = "x-dashboard-resource"`
  - `materializer.Skill{ResourceID, Slug, Description, Body string}`
  - `materializer.RenderClaudeSkill(s Skill) []byte`
  - `materializer.HashBytes(b []byte) string`
  - `materializer.SkillPath(t Target, slug string) (string, error)`

**Design note this task settles.** The rendered frontmatter writes `name:` explicitly and always equal to the directory name. `cmdscope`'s enumeration falls back to the directory name when frontmatter carries none (`enumerate.go:286-289`); writing both consistently means that fallback never fires for a file this component produced, so the name a user sees in the runtime's menu is the slug the database holds and not an artefact of how the path was built.

- [ ] **Step 1: Write the failing tests**

Create `server/internal/materializer/render_test.go`:

```go
package materializer_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/materializer"
)

func TestRenderClaudeSkill_Golden(t *testing.T) {
	got := string(materializer.RenderClaudeSkill(materializer.Skill{
		ResourceID:  "res-42",
		Slug:        "code-review",
		Description: "Review a diff for correctness",
		Body:        "# Code Review\n\nRead the diff before the ticket.",
	}))

	require.Equal(t, `---
name: "code-review"
description: "Review a diff for correctness"
x-dashboard-resource: "res-42"
---

# Code Review

Read the diff before the ticket.
`, got)
}

func TestRenderClaudeSkill_NameIsAlwaysTheSlug(t *testing.T) {
	got := string(materializer.RenderClaudeSkill(materializer.Skill{
		ResourceID: "res-1", Slug: "deploy", Description: "", Body: "x",
	}))
	require.Contains(t, got, "name: \"deploy\"",
		"the directory-name fallback at cmdscope/enumerate.go:286-289 must never have to fire")
}

func TestRenderClaudeSkill_DescriptionCannotBreakOutOfTheFrontmatter(t *testing.T) {
	got := string(materializer.RenderClaudeSkill(materializer.Skill{
		ResourceID: "res-1", Slug: "x",
		Description: "line one\n---\nname: hijacked",
		Body:        "body",
	}))

	require.Equal(t, 2, strings.Count(got, "---\n"), "exactly one opening and one closing fence")
	require.Contains(t, got, `description: "line one\n---\nname: hijacked"`)
}

func TestRenderClaudeSkill_BodyEndsWithExactlyOneNewline(t *testing.T) {
	for _, body := range []string{"text", "text\n", "text\n\n\n"} {
		got := string(materializer.RenderClaudeSkill(materializer.Skill{
			ResourceID: "r", Slug: "s", Body: body,
		}))
		require.True(t, strings.HasSuffix(got, "text\n"), "body %q", body)
		require.False(t, strings.HasSuffix(got, "text\n\n"), "body %q", body)
	}
}

func TestRenderClaudeSkill_IsDeterministic(t *testing.T) {
	s := materializer.Skill{ResourceID: "r", Slug: "s", Description: "d", Body: "b"}
	require.Equal(t,
		materializer.HashBytes(materializer.RenderClaudeSkill(s)),
		materializer.HashBytes(materializer.RenderClaudeSkill(s)),
		"an unstable render would report repaired on every run and rewrite the file forever")
}
```

Create `server/internal/materializer/path_test.go`:

```go
package materializer_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/materializer"
)

func userTarget(root string) materializer.Target {
	return materializer.Target{
		NodeID: "local", Provider: "claude",
		Layer: materializer.LayerUser, Root: root, Adapter: materializer.AdapterClaude,
	}
}

func TestSkillPath_UserLayer(t *testing.T) {
	got, err := materializer.SkillPath(userTarget("/home/u/.claude"), "code-review")
	require.NoError(t, err)
	require.Equal(t, "/home/u/.claude/skills/code-review/SKILL.md", got)
}

func TestSkillPath_ProjectLayer(t *testing.T) {
	target := materializer.Target{
		NodeID: "local", Provider: "claude",
		Layer: materializer.LayerProject, Root: "/work/repo", Adapter: materializer.AdapterClaude,
	}
	got, err := materializer.SkillPath(target, "deploy")
	require.NoError(t, err)
	require.Equal(t, "/work/repo/.claude/skills/deploy/SKILL.md", got)
}

func TestSkillPath_RefusesTraversalBeforeBuildingAnything(t *testing.T) {
	for _, slug := range []string{
		"../escape",
		"..",
		"a/b",
		"/absolute",
		"Upper",
		"with space",
		"trailing/",
		"",
	} {
		got, err := materializer.SkillPath(userTarget("/home/u/.claude"), slug)
		require.Error(t, err, "slug %q", slug)
		require.Equal(t, "", got, "no path may be returned for a refused slug: %q", slug)
	}
}

func TestSkillPath_RefusesATargetWithNoSkillFormat(t *testing.T) {
	target := materializer.Target{
		NodeID: "local", Provider: "codex",
		Layer: materializer.LayerUser, Root: "/home/u/.codex", Adapter: materializer.AdapterNone,
	}
	_, err := materializer.SkillPath(target, "code-review")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no skill format")
}

func TestSkillPath_RefusesAnUnknownLayer(t *testing.T) {
	target := userTarget("/home/u/.claude")
	target.Layer = "plugin"
	_, err := materializer.SkillPath(target, "code-review")
	require.Error(t, err, "plugin and builtin sources are not editable (cmdscope/enumerate.go:96-98)")
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd server && go test ./internal/materializer/ -run 'TestRender|TestSkillPath' -v`
Expected: compile failure — `materializer.RenderClaudeSkill`, `Skill`, `HashBytes`, `SkillPath` undefined.

- [ ] **Step 3: Implement the renderer**

Create `server/internal/materializer/render.go`:

```go
package materializer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// MarkerKey is the frontmatter key every materialized skill file carries.
//
// It is not by itself proof of ownership — a file copied out of a config dir
// carries it too, and cmd/serve/hooks.go:252-263 learned that the hard way for
// settings.json: treating a marker match as ownership made uninstall delete a
// script it never wrote. The materialization record's path is what decides;
// this key only says which resource produced the bytes.
const MarkerKey = "x-dashboard-resource"

// Skill is what one skill resource contributes to a rendered file: the
// identity half from the resource row, the content half from the skill row.
type Skill struct {
	ResourceID  string
	Slug        string
	Description string
	Body        string
}

// RenderClaudeSkill produces the SKILL.md a Claude runtime reads.
//
// The frontmatter name is always the slug, which is also the directory name
// SkillPath builds — written consistently so cmdscope's directory-name
// fallback (enumerate.go:286-289) never has to fire for a file this component
// produced. The output is byte-stable for a given Skill: an unstable render
// would classify as "repaired" on every run and rewrite the file forever.
func RenderClaudeSkill(s Skill) []byte {
	var b bytes.Buffer
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", yamlString(s.Slug))
	fmt.Fprintf(&b, "description: %s\n", yamlString(s.Description))
	fmt.Fprintf(&b, "%s: %s\n", MarkerKey, yamlString(s.ResourceID))
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimRight(s.Body, "\n"))
	b.WriteString("\n")
	return b.Bytes()
}

// yamlString renders v as a YAML double-quoted scalar. A JSON string literal
// is a valid YAML 1.2 double-quoted scalar, so the stdlib does the escaping —
// which is what stops a description containing a newline and "---" from
// closing the frontmatter block and rewriting the keys above it.
func yamlString(v string) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `""`
	}
	return string(b)
}

// HashBytes is the content hash recorded against a materialized file. It is
// what decides whether a file on disk is still the one this node wrote.
func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
```

- [ ] **Step 4: Implement the path boundary**

Create `server/internal/materializer/path.go`:

```go
package materializer

import (
	"fmt"
	"path/filepath"

	"github.com/lx-wnk/agent-dashboard/server/internal/validation"
)

// SkillPath builds the SKILL.md path for slug in t, following the layout
// cmdscope enumerates (spec §3, read from cmdscope/enumerate.go:255-260):
//
//	user    <ConfigDir>/skills/<slug>/SKILL.md
//	project <ProjectCwd>/.claude/skills/<slug>/SKILL.md
//
// This is the path-traversal boundary. The slug is not a path segment a caller
// chooses; it is a validated identifier, and it is validated before any join
// happens — PUT /api/config/file gets the same guarantee from a different
// direction, by refusing every path that is not already in its enumerated
// allow-list (api/config/file.go:151-174).
func SkillPath(t Target, slug string) (string, error) {
	if t.Adapter != AdapterClaude {
		return "", fmt.Errorf("target %s has no skill format", t.Key())
	}
	if !validation.IsValidSlug(slug) {
		return "", fmt.Errorf("skill slug %q refused: %s", slug, validation.SlugPatternMessage)
	}
	switch t.Layer {
	case LayerUser:
		return filepath.Join(t.Root, "skills", slug, "SKILL.md"), nil
	case LayerProject:
		return filepath.Join(t.Root, ".claude", "skills", slug, "SKILL.md"), nil
	default:
		return "", fmt.Errorf("target %s: layer %q is not writable", t.Key(), t.Layer)
	}
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd server && go test ./internal/materializer/ -v`
Expected: all Task 2 and Task 3 tests pass.

- [ ] **Step 6: Commit**

```bash
cd server && gofmt -l ./internal/materializer/ && go vet ./...
```

Commit message: `feat: render the Claude skill format behind a validated path boundary`

---

### Task 4: The five outcomes — deciding without writing

**Files:**
- Create: `server/internal/materializer/classify.go`
- Test: `server/internal/materializer/classify_test.go`

**Interfaces:**
- Consumes: `materializer.HashBytes`, `materializer.RenderClaudeSkill`, `materializer.Skill` (Task 3).
- Produces:
  - `materializer.Outcome string`
  - `materializer.OutcomeCreated | OutcomeUnchanged | OutcomeRepaired | OutcomeConflict | OutcomeForeign | OutcomeUnsupported | OutcomeFailed`
  - `materializer.Classify(path string, want []byte, recordedHash string) (Outcome, error)`

**Design note this task settles — the plan's one deliberate divergence from the spec.**

§6 names the detector as "the mtime-versus-recorded-write comparison the config API already uses for optimistic concurrency (`api/config/file.go:110-115`), with its known granularity of whole seconds", "mitigated by also comparing a content hash". This implementation keeps the content hash and **does not store or compare an mtime**, for three reasons:

1. The spec's own sentence concedes that mtime misses a same-second edit and that the hash is what covers the gap. Where both are consulted the hash therefore decides every case, and the mtime decides none — it is a stored field nothing reads, which is the second-truth problem the project's SSOT rule exists to prevent.
2. The one case where mtime is *stronger* is a file a human edited and then edited back to exactly our bytes. Overwriting that loses nothing a human wrote: they reverted their own change, and the bytes on disk are ours. It is not a data-loss case, so the extra alarm buys nothing.
3. `api/config/file.go:110-115` solves a genuinely different problem — a *client* held a copy and is writing it back, so "did it change since you loaded it" is the right question and the client supplied the baseline. The materializer holds no client copy; its question is "are these still the bytes I wrote", and a hash answers that exactly.

Recorded here rather than left implicit: a reviewer comparing plan against spec must see this was decided, not forgotten.

**Design note two.** `recordedHash == ""` collapses two situations into the same, correct answer: never seen before, and seen before but foreign. Neither may be overwritten while a file is present, and both may be written once the path is free — a foreign file the user deleted is a path this node is now entitled to.

- [ ] **Step 1: Write the failing tests**

Create `server/internal/materializer/classify_test.go`:

```go
package materializer_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/materializer"
)

func want(t *testing.T, body string) []byte {
	t.Helper()
	return materializer.RenderClaudeSkill(materializer.Skill{
		ResourceID: "res-1", Slug: "code-review", Description: "Review a diff", Body: body,
	})
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, data, 0o600))
}

func TestClassify_AbsentFileIsCreated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skills", "code-review", "SKILL.md")

	got, err := materializer.Classify(path, want(t, "v1"), "")
	require.NoError(t, err)
	require.Equal(t, materializer.OutcomeCreated, got)
}

func TestClassify_AbsentFileWeOnceWroteIsCreatedAgain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skills", "code-review", "SKILL.md")

	got, err := materializer.Classify(path, want(t, "v1"), materializer.HashBytes(want(t, "v1")))
	require.NoError(t, err)
	require.Equal(t, materializer.OutcomeCreated, got, "a file we wrote and the user deleted may be written again")
}

func TestClassify_OurFileMatchingTheResourceIsUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skills", "code-review", "SKILL.md")
	content := want(t, "v1")
	writeFile(t, path, content)

	got, err := materializer.Classify(path, content, materializer.HashBytes(content))
	require.NoError(t, err)
	require.Equal(t, materializer.OutcomeUnchanged, got)
}

func TestClassify_OurFileBehindTheResourceIsRepaired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skills", "code-review", "SKILL.md")
	old := want(t, "v1")
	writeFile(t, path, old)

	got, err := materializer.Classify(path, want(t, "v2"), materializer.HashBytes(old))
	require.NoError(t, err)
	require.Equal(t, materializer.OutcomeRepaired, got, "the database is the truth for a file we own")
}

func TestClassify_OurFileEditedByAHumanIsAConflict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skills", "code-review", "SKILL.md")
	ours := want(t, "v1")
	writeFile(t, path, append(ours, []byte("\nand one line a person added\n")...))

	got, err := materializer.Classify(path, ours, materializer.HashBytes(ours))
	require.NoError(t, err)
	require.Equal(t, materializer.OutcomeConflict, got)
}

func TestClassify_AFileWeNeverWroteIsForeign(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skills", "code-review", "SKILL.md")
	writeFile(t, path, []byte("---\nname: code-review\n---\n\nsomebody's own skill\n"))

	got, err := materializer.Classify(path, want(t, "v1"), "")
	require.NoError(t, err)
	require.Equal(t, materializer.OutcomeForeign, got)
}

func TestClassify_AFileCarryingOurMarkerIsStillForeignWithoutARecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skills", "code-review", "SKILL.md")
	writeFile(t, path, want(t, "v1"))

	got, err := materializer.Classify(path, want(t, "v1"), "")
	require.NoError(t, err)
	require.Equal(t, materializer.OutcomeForeign, got,
		"a marker is not proof of ownership; the record is (cmd/serve/hooks.go:252-263)")
}

func TestClassify_ASymlinkAtTheTargetIsForeign(t *testing.T) {
	root := t.TempDir()
	secret := filepath.Join(root, "elsewhere.md")
	require.NoError(t, os.WriteFile(secret, []byte("do not touch"), 0o600))

	path := filepath.Join(root, "skills", "code-review", "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.Symlink(secret, path))

	got, err := materializer.Classify(path, want(t, "v1"), materializer.HashBytes(want(t, "v1")))
	require.NoError(t, err)
	require.Equal(t, materializer.OutcomeForeign, got,
		"never follow a symlink — the read side refuses the same shape at cmdscope/enumerate.go:378-382")
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd server && go test ./internal/materializer/ -run TestClassify -v`
Expected: compile failure — `materializer.Classify` and the `Outcome*` constants are undefined.

- [ ] **Step 3: Implement the classifier**

Create `server/internal/materializer/classify.go`:

```go
package materializer

import (
	"errors"
	"fmt"
	"os"
)

// Outcome is what happened, or would happen, at one target.
//
// Three of these are cmd/serve/hooks.go's outcome set (hooks.go:265-271),
// which this component adopts rather than reinvents; created, conflict,
// foreign and unsupported are the cases a per-file artefact adds to a
// per-settings-key one.
type Outcome string

const (
	// OutcomeCreated means no file was at the target path.
	OutcomeCreated Outcome = "created"
	// OutcomeUnchanged means our file already holds the resource's content.
	OutcomeUnchanged Outcome = "unchanged"
	// OutcomeRepaired means our file drifted from the resource and the
	// database is the truth for a file we own.
	OutcomeRepaired Outcome = "repaired"
	// OutcomeConflict means a human edited a file we wrote. The run stops for
	// that resource: no merge, no overwrite, and no retry that would overwrite
	// it later.
	OutcomeConflict Outcome = "conflict"
	// OutcomeForeign means the file at the target was not written by this
	// node. It is never touched.
	OutcomeForeign Outcome = "foreign"
	// OutcomeUnsupported means the runtime has no skill format. A recorded
	// no-op, never a silent gap and never a fabricated format.
	OutcomeUnsupported Outcome = "unsupported"
	// OutcomeFailed means this target could not be processed. Other targets
	// still proceed, and the run reports itself as partial.
	OutcomeFailed Outcome = "failed"
)

// Classify decides what would happen at path. It reads the filesystem and
// writes nothing — every write decision in this package flows through it
// first, so a caller that only wants a report calls exactly the same code the
// caller that writes does.
//
// recordedHash is the SHA-256 of the bytes a previous run wrote at this
// target, or "" when this node has never written these bytes. The empty string
// deliberately covers two situations at once — never materialized here, and
// materialized-as-foreign — because the answer is the same for both: a file
// that is present may not be overwritten, and a path that is free may be
// written. A foreign file the user deleted is a path this node is now entitled
// to.
//
// On why the mtime does not appear here, see the plan's Task 4 design note:
// spec §6 concedes that a whole-second mtime misses a same-second edit and
// that the content hash is what covers it, so the hash decides every case and
// a stored mtime would be a field nothing reads.
func Classify(path string, want []byte, recordedHash string) (Outcome, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return OutcomeCreated, nil
	}
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}
	// Lstat, and IsRegular: a symlink or anything else at the target is not
	// something this node wrote and not something it will write through. The
	// read side refuses the same shape when enumerating
	// (cmdscope/enumerate.go:378-382).
	if !info.Mode().IsRegular() {
		return OutcomeForeign, nil
	}
	if recordedHash == "" {
		return OutcomeForeign, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	switch have := HashBytes(data); {
	case have != recordedHash:
		return OutcomeConflict, nil
	case have == HashBytes(want):
		return OutcomeUnchanged, nil
	default:
		return OutcomeRepaired, nil
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd server && go test ./internal/materializer/ -v`
Expected: every test in the package passes.

- [ ] **Step 5: Commit**

```bash
cd server && gofmt -l ./internal/materializer/ && go vet ./...
```

Commit message: `feat: classify a materialization target into one of five outcomes`

---

### Task 5: The run — lease, atomic write, and a report that admits what it did not do

**Files:**
- Create: `server/internal/materializer/apply.go`
- Create: `server/internal/materializer/materializer.go`
- Test: `server/internal/materializer/apply_test.go`, `server/internal/materializer/materializer_test.go`

**Interfaces:**
- Consumes: `repo.ResourceRepo.ListForKind`, `repo.SkillRepo.GetByResource`, `repo.MaterializationRepo.Get|Record` (Task 1), `repo.CoordLockRepo.Acquire|Release` (`server/internal/db/repo/coord_lock_repo.go:14-18`), `materializer.Resolver.Targets`, `SkillPath`, `RenderClaudeSkill`, `HashBytes`, `Classify`.
- Produces:
  - `materializer.LeaseNamespace = "materialize"`, `materializer.DefaultLeaseTTL = 2 * time.Minute`
  - `materializer.Apply(t Target, path string, want []byte) error`
  - `materializer.Materializer{Resources, Skills, Records, Locks, Resolver, NodeID, Owner, LeaseTTL}`
  - `materializer.New(resources repo.ResourceRepo, skills repo.SkillRepo, records repo.MaterializationRepo, locks repo.CoordLockRepo, resolver Resolver) *Materializer`
  - `materializer.Materializer.Run(ctx context.Context, dryRun bool) (Report, error)`
  - `materializer.Report{NodeID, Owner string; Leased bool; LeaseHolder string; DryRun, Partial bool; Targets []string; Entries []ReportEntry}`
  - `materializer.ReportEntry{ResourceID, Slug, Provider, TargetKey, Path string; Outcome Outcome; Detail string}`

**Design note this task settles.** The lease is acquired only on a writing run. A dry run takes no lease at all: it writes nothing, so serialising it would only make two people unable to look at the same report at the same time. Without the lease a writing run degrades to read-only and names the holder, which is §9's first row — reachability is not ownership, and this project already learned that when a desktop instance adopted a foreign server because a health check returned 200.

**Design note two.** `Report.Targets` lists every target key the run considered, even those that produced no entry. A skill that resolves to zero targets — no config dir on this node exists — would otherwise report as an empty success indistinguishable from one that had nothing to do.

- [ ] **Step 1: Write the failing tests**

Create `server/internal/materializer/apply_test.go`:

```go
package materializer_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/materializer"
)

func TestApply_WritesTheFileAndCreatesItsDirectories(t *testing.T) {
	root := t.TempDir()
	target := userTarget(root)
	path, err := materializer.SkillPath(target, "code-review")
	require.NoError(t, err)

	content := want(t, "v1")
	require.NoError(t, materializer.Apply(target, path, content))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, content, got)

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "a skill file sits beside session transcripts")
}

func TestApply_RefusesASymlinkedDirectoryBelowTheRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	require.NoError(t, os.MkdirAll(outside, 0o700))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "skills")))

	target := userTarget(root)
	path, err := materializer.SkillPath(target, "code-review")
	require.NoError(t, err)

	err = materializer.Apply(target, path, want(t, "v1"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "symlink")

	entries, rerr := os.ReadDir(outside)
	require.NoError(t, rerr)
	require.Empty(t, entries, "nothing may be written through a symlink out of the configured root")
}

func TestApply_ARootThatIsItselfASymlinkIsFine(t *testing.T) {
	real := filepath.Join(t.TempDir(), "claude-personal")
	require.NoError(t, os.MkdirAll(real, 0o700))
	link := filepath.Join(t.TempDir(), "claude")
	require.NoError(t, os.Symlink(real, link))

	target := userTarget(link)
	path, err := materializer.SkillPath(target, "code-review")
	require.NoError(t, err)

	require.NoError(t, materializer.Apply(target, path, want(t, "v1")),
		"~/.claude linked into ~/.claude-personal is an ordinary dotfiles layout, not an attack")
}

func TestApply_AFailedRenameLeavesNoPartialFileAndNoTempFile(t *testing.T) {
	root := t.TempDir()
	target := userTarget(root)
	path, err := materializer.SkillPath(target, "code-review")
	require.NoError(t, err)

	// A directory where the file belongs makes the rename fail after the temp
	// file is already written and synced — the §9 "rename fails" row.
	require.NoError(t, os.MkdirAll(path, 0o700))

	require.Error(t, materializer.Apply(target, path, want(t, "v1")))

	entries, rerr := os.ReadDir(filepath.Dir(path))
	require.NoError(t, rerr)
	require.Len(t, entries, 1, "only the pre-existing entry survives; the temp file is cleaned up")
	require.Equal(t, "SKILL.md", entries[0].Name())
	require.True(t, entries[0].IsDir(), "the target was untouched")
}
```

Create `server/internal/materializer/materializer_test.go`:

```go
package materializer_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/materializer"
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

type harness struct {
	m         *materializer.Materializer
	locks     repo.CoordLockRepo
	resources repo.ResourceRepo
	skills    repo.SkillRepo
	configDir string
	ctx       context.Context
}

// newHarness builds a materializer over an in-memory database and one config
// directory under t.TempDir(). No test in this package ever names a real
// config dir; the injected Resolver seams exist for exactly that reason.
func newHarness(t *testing.T) *harness {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })

	configDir := filepath.Join(t.TempDir(), ".claude")
	require.NoError(t, os.MkdirAll(configDir, 0o700))

	resolver := materializer.Resolver{
		NodeID:             repo.DefaultNodeID,
		ClaudeConfigDirs:   func() []string { return []string{configDir} },
		ProviderConfigDirs: func() []parser.ProviderConfigDir { return nil },
	}
	h := &harness{
		locks:     repo.NewCoordLockRepo(bundle.Client),
		resources: repo.NewResourceRepo(bundle.Client),
		skills:    repo.NewSkillRepo(bundle.Client),
		configDir: configDir,
		ctx:       context.Background(),
	}
	h.m = materializer.New(h.resources, h.skills, repo.NewMaterializationRepo(bundle.Client), h.locks, resolver)
	return h
}

func (h *harness) addSkill(t *testing.T, slug, body string) string {
	t.Helper()
	res, err := h.resources.Upsert(h.ctx, repo.UpsertResourceInput{
		Kind: repo.ResourceKindSkill, Slug: slug, Name: slug,
		Scope: repo.GlobalScope(), State: repo.ResourceStateEnabled,
	})
	require.NoError(t, err)
	_, err = h.skills.Upsert(h.ctx, repo.UpsertSkillInput{
		ResourceID: res.ID, Description: "Review a diff", Body: body,
	})
	require.NoError(t, err)
	return res.ID
}

func (h *harness) skillPath(slug string) string {
	return filepath.Join(h.configDir, "skills", slug, "SKILL.md")
}

func onlyEntry(t *testing.T, rep materializer.Report) materializer.ReportEntry {
	t.Helper()
	require.Len(t, rep.Entries, 1)
	return rep.Entries[0]
}

func TestRun_CreatesTheFile(t *testing.T) {
	h := newHarness(t)
	h.addSkill(t, "code-review", "v1")

	rep, err := h.m.Run(h.ctx, false)
	require.NoError(t, err)
	require.True(t, rep.Leased)
	require.Equal(t, materializer.OutcomeCreated, onlyEntry(t, rep).Outcome)

	got, rerr := os.ReadFile(h.skillPath("code-review"))
	require.NoError(t, rerr)
	require.Contains(t, string(got), "name: \"code-review\"")
	require.Contains(t, string(got), "v1")
}

func TestRun_SecondRunIsUnchanged(t *testing.T) {
	h := newHarness(t)
	h.addSkill(t, "code-review", "v1")

	_, err := h.m.Run(h.ctx, false)
	require.NoError(t, err)
	rep, err := h.m.Run(h.ctx, false)
	require.NoError(t, err)
	require.Equal(t, materializer.OutcomeUnchanged, onlyEntry(t, rep).Outcome)
}

func TestRun_RepairsOurOwnDriftedFile(t *testing.T) {
	h := newHarness(t)
	id := h.addSkill(t, "code-review", "v1")
	_, err := h.m.Run(h.ctx, false)
	require.NoError(t, err)

	_, err = h.skills.Upsert(h.ctx, repo.UpsertSkillInput{ResourceID: id, Description: "Review a diff", Body: "v2"})
	require.NoError(t, err)

	rep, err := h.m.Run(h.ctx, false)
	require.NoError(t, err)
	require.Equal(t, materializer.OutcomeRepaired, onlyEntry(t, rep).Outcome)

	got, rerr := os.ReadFile(h.skillPath("code-review"))
	require.NoError(t, rerr)
	require.Contains(t, string(got), "v2")
}

func TestRun_AHandEditIsAConflictAndSurvivesEverySubsequentRun(t *testing.T) {
	h := newHarness(t)
	h.addSkill(t, "code-review", "v1")
	_, err := h.m.Run(h.ctx, false)
	require.NoError(t, err)

	edited := "---\nname: \"code-review\"\n---\n\nwhat a person actually wants here\n"
	require.NoError(t, os.WriteFile(h.skillPath("code-review"), []byte(edited), 0o600))

	for range 2 {
		rep, rerr := h.m.Run(h.ctx, false)
		require.NoError(t, rerr)
		require.Equal(t, materializer.OutcomeConflict, onlyEntry(t, rep).Outcome)

		got, gerr := os.ReadFile(h.skillPath("code-review"))
		require.NoError(t, gerr)
		require.Equal(t, edited, string(got), "no merge, no overwrite, and no retry that overwrites later")
	}
}

func TestRun_AForeignFileIsNeverTouchedAndIsRememberedAfterTheFirstReport(t *testing.T) {
	h := newHarness(t)
	h.addSkill(t, "code-review", "v1")

	foreign := "---\nname: code-review\n---\n\nsomebody's own skill\n"
	require.NoError(t, os.MkdirAll(filepath.Dir(h.skillPath("code-review")), 0o700))
	require.NoError(t, os.WriteFile(h.skillPath("code-review"), []byte(foreign), 0o600))

	first, err := h.m.Run(h.ctx, false)
	require.NoError(t, err)
	require.Equal(t, materializer.OutcomeForeign, onlyEntry(t, first).Outcome)
	require.Contains(t, onlyEntry(t, first).Detail, "first seen")

	second, err := h.m.Run(h.ctx, false)
	require.NoError(t, err)
	require.Equal(t, materializer.OutcomeForeign, onlyEntry(t, second).Outcome)
	require.NotContains(t, onlyEntry(t, second).Detail, "first seen", "reported once, then remembered so it does not nag")

	got, rerr := os.ReadFile(h.skillPath("code-review"))
	require.NoError(t, rerr)
	require.Equal(t, foreign, string(got))
}

func TestRun_WithoutTheLeaseItWritesNothingAndNamesTheHolder(t *testing.T) {
	h := newHarness(t)
	h.addSkill(t, "code-review", "v1")

	acquired, _, _, err := h.locks.Acquire(h.ctx, materializer.LeaseNamespace, repo.DefaultNodeID, "the-other-instance", materializer.DefaultLeaseTTL)
	require.NoError(t, err)
	require.True(t, acquired)

	rep, err := h.m.Run(h.ctx, false)
	require.NoError(t, err)
	require.False(t, rep.Leased)
	require.Equal(t, "the-other-instance", rep.LeaseHolder)
	require.Equal(t, materializer.OutcomeCreated, onlyEntry(t, rep).Outcome, "it still reports what it would write")
	require.Contains(t, onlyEntry(t, rep).Detail, "the-other-instance")

	_, statErr := os.Stat(h.skillPath("code-review"))
	require.True(t, os.IsNotExist(statErr), "the loser of a lease writes nothing")
}

func TestRun_DryRunWritesNothingAndTakesNoLease(t *testing.T) {
	h := newHarness(t)
	h.addSkill(t, "code-review", "v1")

	rep, err := h.m.Run(h.ctx, true)
	require.NoError(t, err)
	require.True(t, rep.DryRun)
	require.False(t, rep.Leased)
	require.Equal(t, materializer.OutcomeCreated, onlyEntry(t, rep).Outcome)
	require.Contains(t, onlyEntry(t, rep).Detail, "dry run")

	_, statErr := os.Stat(h.skillPath("code-review"))
	require.True(t, os.IsNotExist(statErr))

	held, lerr := h.locks.ListActive(h.ctx, materializer.LeaseNamespace)
	require.NoError(t, lerr)
	require.Empty(t, held, "a run that writes nothing has no reason to lock the node")
}

func TestRun_TheLeaseIsReleasedForTheNextRun(t *testing.T) {
	h := newHarness(t)
	h.addSkill(t, "code-review", "v1")

	_, err := h.m.Run(h.ctx, false)
	require.NoError(t, err)

	held, lerr := h.locks.ListActive(h.ctx, materializer.LeaseNamespace)
	require.NoError(t, lerr)
	require.Empty(t, held)
}

func TestRun_AProviderWithNoSkillFormatIsARecordedNoOp(t *testing.T) {
	h := newHarness(t)
	codexDir := filepath.Join(t.TempDir(), ".codex")
	require.NoError(t, os.MkdirAll(codexDir, 0o700))
	h.m.Resolver.ProviderConfigDirs = func() []parser.ProviderConfigDir {
		return []parser.ProviderConfigDir{{Provider: "codex", Path: codexDir}}
	}
	h.addSkill(t, "code-review", "v1")

	rep, err := h.m.Run(h.ctx, false)
	require.NoError(t, err)
	require.Len(t, rep.Entries, 2)

	var codex materializer.ReportEntry
	for _, e := range rep.Entries {
		if e.Provider == "codex" {
			codex = e
		}
	}
	require.Equal(t, materializer.OutcomeUnsupported, codex.Outcome)
	require.Contains(t, codex.Detail, "no skill format")

	entries, rerr := os.ReadDir(codexDir)
	require.NoError(t, rerr)
	require.Empty(t, entries, "a no-op target touches no filesystem")
}

func TestRun_ADisabledResourceIsNotMaterialized(t *testing.T) {
	h := newHarness(t)
	id := h.addSkill(t, "code-review", "v1")
	_, err := h.resources.SetState(h.ctx, id, repo.ResourceStateDisabled)
	require.NoError(t, err)

	rep, err := h.m.Run(h.ctx, false)
	require.NoError(t, err)
	require.Empty(t, rep.Entries)
}

func TestRun_OneFailedTargetDoesNotStopTheOthersAndTheRunReportsPartial(t *testing.T) {
	h := newHarness(t)
	blocked := filepath.Join(t.TempDir(), ".claude-work")
	require.NoError(t, os.MkdirAll(filepath.Join(blocked, "skills"), 0o500))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(blocked, "skills"), 0o700) })
	h.m.Resolver.ClaudeConfigDirs = func() []string { return []string{h.configDir, blocked} }
	h.addSkill(t, "code-review", "v1")

	rep, err := h.m.Run(h.ctx, false)
	require.NoError(t, err)
	require.True(t, rep.Partial, "a partial materialization is reported as partial")
	require.Len(t, rep.Entries, 2)

	got, rerr := os.ReadFile(h.skillPath("code-review"))
	require.NoError(t, rerr)
	require.Contains(t, string(got), "v1", "the writable target proceeded")
}

func TestRun_ReportsEveryTargetItConsidered(t *testing.T) {
	h := newHarness(t)
	h.addSkill(t, "code-review", "v1")

	rep, err := h.m.Run(h.ctx, true)
	require.NoError(t, err)
	require.Equal(t, []string{"claude|user|" + h.configDir}, rep.Targets)
}
```

> **Note for the implementer:** `TestRun_OneFailedTargetDoesNotStopTheOthers…` relies on a mode-0500 directory. If the suite is ever run as root that assertion cannot hold; add `if os.Geteuid() == 0 { t.Skip("root ignores directory permissions") }` at the top of that test if CI is found to run as root. Do not weaken the assertion.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd server && go test ./internal/materializer/ -run 'TestApply|TestRun' -v`
Expected: compile failure — `materializer.Apply`, `New`, `Run`, `Report`, `ReportEntry`, `LeaseNamespace`, `DefaultLeaseTTL` undefined.

- [ ] **Step 3: Implement the writer**

Create `server/internal/materializer/apply.go`:

```go
package materializer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// skillFileMode is owner-only for the same reason cmd/serve/hooks.go writes
// settings.json 0600 and its directory 0700: this is the config directory that
// also holds session transcripts and the hooks secret.
const (
	skillFileMode = 0o600
	skillDirMode  = 0o700
)

// Apply writes want to path, atomically.
//
// t bounds the symlink refusal: every directory component below t.Root must be
// a real directory, while t.Root itself may be a symlink — a ~/.claude linked
// into ~/.claude-personal is an ordinary dotfiles layout, and this project's
// own author runs one.
func Apply(t Target, path string, want []byte) error {
	dir := filepath.Dir(path)
	if err := refuseSymlinkBelow(t.Root, dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, skillDirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	return atomicWrite(path, want, skillFileMode)
}

// refuseSymlinkBelow walks root down to dir and refuses any component that is
// a symlink or not a directory. A symlinked skills/ or skills/<slug>/ would
// redirect the write outside the configured root — the attack the enumeration
// side already refuses on read (cmdscope/enumerate.go:365-390).
func refuseSymlinkBelow(root, dir string) error {
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("%s is not below %s", dir, root)
	}
	cur := root
	if rel == "." {
		return nil
	}
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		cur = filepath.Join(cur, part)
		info, serr := os.Lstat(cur)
		if errors.Is(serr, os.ErrNotExist) {
			// Nothing exists from here down; MkdirAll creates real directories.
			return nil
		}
		if serr != nil {
			return fmt.Errorf("stat %s: %w", cur, serr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symlink", cur)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", cur)
		}
	}
	return nil
}

// atomicWrite replaces path with data: temp file in the target's own
// directory, Sync, Close, Chmod, Rename.
//
// This is hookscript.writeExecutable's shape (hookscript.go:44-73), not
// api/config/file.go's (file.go:187-210) — the latter omits tmp.Sync(), and a
// skill file that survives a rename but not a power loss is not worth the
// saved syscall. The deferred Remove is a no-op once the rename succeeds and
// is what stops a failed rename from leaving a stray temp file behind.
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".materialize-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file next to %s: %w", path, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmp.Name(), err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp.Name(), err)
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return fmt.Errorf("chmod %s: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
```

- [ ] **Step 4: Implement the run**

Create `server/internal/materializer/materializer.go`:

```go
package materializer

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// LeaseNamespace is the coord_lock namespace holding one lease per node. Two
// dashboard instances on one machine would otherwise write the same files, and
// reachability is not ownership — this project already learned that when a
// second desktop instance adopted a foreign server because a health check
// returned 200.
const LeaseNamespace = "materialize"

// DefaultLeaseTTL bounds how long a crashed run keeps the node locked.
// coord_lock expiry is lazy: there is no sweeper and an expired row survives
// until the same key is re-acquired (repo/coord_lock_repo.go:73,109-114).
// Harmless here — the next run is what re-acquires it.
const DefaultLeaseTTL = 2 * time.Minute

// ReportEntry is what happened at one (resource, target) pair.
type ReportEntry struct {
	ResourceID string  `json:"resourceId"`
	Slug       string  `json:"slug"`
	Provider   string  `json:"provider"`
	TargetKey  string  `json:"targetKey"`
	Path       string  `json:"path,omitempty"`
	Outcome    Outcome `json:"outcome"`
	Detail     string  `json:"detail,omitempty"`
}

// Report is one run. It states what it did not do as explicitly as what it
// did: a materializer that reports only its writes is one whose refusals are
// invisible.
type Report struct {
	NodeID      string `json:"nodeId"`
	Owner       string `json:"owner"`
	DryRun      bool   `json:"dryRun"`
	Leased      bool   `json:"leased"`
	LeaseHolder string `json:"leaseHolder,omitempty"`
	// Partial is true when at least one target failed. Other targets still
	// proceeded; the run is neither a success nor a failure.
	Partial bool `json:"partial"`
	// Targets lists every target key considered, so a resource that resolved
	// to none is distinguishable from one that had nothing to do.
	Targets []string      `json:"targets"`
	Entries []ReportEntry `json:"entries"`
}

// Materializer produces the files agent runtimes read from skill resources.
type Materializer struct {
	Resources repo.ResourceRepo
	Skills    repo.SkillRepo
	Records   repo.MaterializationRepo
	Locks     repo.CoordLockRepo
	Resolver  Resolver
	NodeID    string
	// Owner identifies this process to the lease. Per process, not per run:
	// coord_lock.Acquire is re-entrant for the same owner
	// (repo/coord_lock_repo.go:73), so two concurrent runs in one process would
	// both hold it — which is why the HTTP handler single-flights on top.
	Owner    string
	LeaseTTL time.Duration
}

// New builds a Materializer with a fresh per-process lease owner.
func New(
	resources repo.ResourceRepo,
	skills repo.SkillRepo,
	records repo.MaterializationRepo,
	locks repo.CoordLockRepo,
	resolver Resolver,
) *Materializer {
	return &Materializer{
		Resources: resources,
		Skills:    skills,
		Records:   records,
		Locks:     locks,
		Resolver:  resolver,
		NodeID:    resolver.NodeID,
		Owner:     "materializer:" + uuid.NewString(),
		LeaseTTL:  DefaultLeaseTTL,
	}
}

// Run classifies every enabled skill resource on this node against every
// target and writes the ones it owns.
//
// A dry run takes no lease: it writes nothing, so serialising it would only
// stop two people reading the same report at once. A writing run that cannot
// take the lease degrades to read-only and names the holder — it still reports
// exactly what it would have written.
func (m *Materializer) Run(ctx context.Context, dryRun bool) (Report, error) {
	rep := Report{
		NodeID:  m.NodeID,
		Owner:   m.Owner,
		DryRun:  dryRun,
		Targets: []string{},
		Entries: []ReportEntry{},
	}

	readOnly := "dry run"
	if !dryRun {
		ok, holder, _, err := m.Locks.Acquire(ctx, LeaseNamespace, m.NodeID, m.Owner, m.LeaseTTL)
		if err != nil {
			return rep, fmt.Errorf("materialize: acquire node lease: %w", err)
		}
		rep.Leased = ok
		if ok {
			readOnly = ""
			defer func() {
				// WithoutCancel: a client that disconnected mid-run must not
				// leave the node locked until the lease expires.
				if rerr := m.Locks.Release(context.WithoutCancel(ctx), LeaseNamespace, m.NodeID, m.Owner); rerr != nil {
					slog.Warn("materialize: release node lease", "node", m.NodeID, "err", rerr)
				}
			}()
		} else {
			rep.LeaseHolder = holder
			readOnly = "node lease held by " + holder
		}
	}

	resources, err := m.Resources.ListForKind(ctx, repo.ResourceKindSkill)
	if err != nil {
		return rep, fmt.Errorf("materialize: list skill resources: %w", err)
	}

	seen := map[string]bool{}
	for _, res := range resources {
		if res.NodeID != m.NodeID {
			continue // cross-node materialization is V2, with the node registry
		}
		if res.State == repo.ResourceStateDisabled || res.State == repo.ResourceStateOrphaned {
			continue
		}

		targets := m.Resolver.Targets(repo.Scope{Kind: repo.ScopeKind(res.ScopeKind), Ref: res.ScopeRef})
		for _, t := range targets {
			if !seen[t.Key()] {
				seen[t.Key()] = true
				rep.Targets = append(rep.Targets, t.Key())
			}
		}

		content, cerr := m.Skills.GetByResource(ctx, res.ID)
		if cerr != nil {
			rep.Entries = append(rep.Entries, ReportEntry{
				ResourceID: res.ID, Slug: res.Slug,
				Outcome: OutcomeFailed,
				Detail:  "no skill content to materialize: " + cerr.Error(),
			})
			continue
		}

		skill := Skill{
			ResourceID:  res.ID,
			Slug:        res.Slug,
			Description: content.Description,
			Body:        content.Body,
		}
		for _, t := range targets {
			rep.Entries = append(rep.Entries, m.one(ctx, skill, t, readOnly))
		}
	}

	sort.Strings(rep.Targets)
	for _, e := range rep.Entries {
		if e.Outcome == OutcomeFailed {
			rep.Partial = true
			break
		}
	}
	return rep, nil
}

// one materializes s into t. readOnly is empty when writing is permitted and
// otherwise names why it is not, so the report says both what would happen and
// what stopped it.
func (m *Materializer) one(ctx context.Context, s Skill, t Target, readOnly string) ReportEntry {
	e := ReportEntry{ResourceID: s.ResourceID, Slug: s.Slug, Provider: t.Provider, TargetKey: t.Key()}

	if t.Adapter == AdapterNone {
		e.Outcome = OutcomeUnsupported
		e.Detail = t.Provider + " has no skill format — not written, and not faked"
		return e
	}

	path, err := SkillPath(t, s.Slug)
	if err != nil {
		e.Outcome = OutcomeFailed
		e.Detail = err.Error()
		return e
	}
	e.Path = path

	rec, err := m.Records.Get(ctx, s.ResourceID, t.Key())
	if err != nil {
		e.Outcome = OutcomeFailed
		e.Detail = err.Error()
		return e
	}
	recordedHash := ""
	if rec != nil {
		recordedHash = rec.ContentHash
	}

	want := RenderClaudeSkill(s)
	outcome, err := Classify(path, want, recordedHash)
	if err != nil {
		e.Outcome = OutcomeFailed
		e.Detail = err.Error()
		return e
	}
	e.Outcome = outcome

	switch outcome {
	case OutcomeUnchanged:
		return e

	case OutcomeConflict:
		e.Detail = "hand-edited since it was written — no merge, no overwrite, and no retry that would overwrite it later"
		return e

	case OutcomeForeign:
		if rec != nil {
			e.Detail = "not ours — reported " + rec.CreatedAt.Format(time.RFC3339) + ", left untouched"
			return e
		}
		if _, rerr := m.Records.Record(ctx, repo.RecordMaterializationInput{
			ResourceID: s.ResourceID, TargetKey: t.Key(), Path: path,
			ContentHash: "", Outcome: string(OutcomeForeign),
		}); rerr != nil {
			e.Detail = "not ours, and the report could not be remembered: " + rerr.Error()
			return e
		}
		e.Detail = "not ours — first seen, left untouched"
		return e
	}

	// created | repaired — the only two outcomes that write.
	if readOnly != "" {
		e.Detail = "not written (" + readOnly + ")"
		return e
	}
	if aerr := Apply(t, path, want); aerr != nil {
		e.Outcome = OutcomeFailed
		e.Detail = aerr.Error()
		return e
	}
	if _, rerr := m.Records.Record(ctx, repo.RecordMaterializationInput{
		ResourceID: s.ResourceID, TargetKey: t.Key(), Path: path,
		ContentHash: HashBytes(want), Outcome: string(outcome),
	}); rerr != nil {
		// A written file with no record reads as foreign on the next run, and
		// this node then stops maintaining a file it wrote itself. Loud.
		e.Outcome = OutcomeFailed
		e.Detail = "written but not recorded — the next run will treat it as foreign: " + rerr.Error()
	}
	return e
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd server && go test ./internal/materializer/ -v`
Expected: every test in the package passes, including all five ownership outcomes, both conflict runs, and the lease-contention case.

- [ ] **Step 6: Commit**

```bash
cd server && gofmt -l ./internal/materializer/ && go vet ./...
```

Commit message: `feat: materialize owned skills behind a node lease`

---

### Task 6: The trigger, the wiring, and the lock file that nothing ever read

**Files:**
- Create: `server/internal/api/skills/handler.go`
- Test: `server/internal/api/skills/handler_test.go`
- Modify: `server/internal/api/router.go` (`Deps` field + mount block)
- Modify: `server/serverapp/di.go` (construction next to `resourcesHandler`)
- Delete: `skills-lock.json`
- Modify: `docs/guides/agent-skills.md`, `.gitignore`, `CHANGELOG.md`, `README.md`, `docs/README.md`

**Interfaces:**
- Consumes: `materializer.New`, `materializer.Resolver`, `materializer.Materializer.Run`, `materializer.Report` (Tasks 2 and 5); `parser.AllClaudeConfigDirs` (`server/internal/parser/parser.go:172-174`); `provider.Registry.ConfigDirs` (`server/internal/provider/registry.go:179-204`); `repo.DefaultNodeID` (`server/internal/db/repo/resource_repo.go:44`); `apierr.ErrorMiddleware`, `apierr.NewAppError` (`server/internal/apierr/apierr.go:36,46`).
- Produces:
  - `skills.NewHandler(m *materializer.Materializer) *skills.Handler`
  - `skills.Handler.Mount(r chi.Router)` → `POST /api/skills/materialize`
  - `api.Deps.SkillsHandler *skills.Handler`

**Design note this task settles.** `dryRun` absent means `true`. A route that can overwrite a file has the safe default, and the caller opts into writing. §8's removal ships in the same commit as the replacement, for the reason §8 gives: leaving a documented command that cannot work is worse than having no command.

**Design note two.** The handler single-flights on an `atomic.Bool`, the same guard `api/obsidian` uses (`server/internal/api/obsidian/handler.go:26-39`). It is not redundant with the lease: `coord_lock.Acquire` is re-entrant for the same owner (`repo/coord_lock_repo.go:73`), and the lease owner is per process, so two concurrent requests in one server would both hold it and race each other into the same files.

- [ ] **Step 1: Write the failing test**

Create `server/internal/api/skills/handler_test.go`:

```go
package skills_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/api/skills"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/materializer"
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

func newRouter(t *testing.T) (http.Handler, string) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })

	configDir := filepath.Join(t.TempDir(), ".claude")
	require.NoError(t, os.MkdirAll(configDir, 0o700))

	resources := repo.NewResourceRepo(bundle.Client)
	res, err := resources.Upsert(context.Background(), repo.UpsertResourceInput{
		Kind: repo.ResourceKindSkill, Slug: "code-review", Name: "Code Review",
		Scope: repo.GlobalScope(), State: repo.ResourceStateEnabled,
	})
	require.NoError(t, err)
	_, err = repo.NewSkillRepo(bundle.Client).Upsert(context.Background(), repo.UpsertSkillInput{
		ResourceID: res.ID, Description: "Review a diff", Body: "v1",
	})
	require.NoError(t, err)

	m := materializer.New(
		resources,
		repo.NewSkillRepo(bundle.Client),
		repo.NewMaterializationRepo(bundle.Client),
		repo.NewCoordLockRepo(bundle.Client),
		materializer.Resolver{
			NodeID:             repo.DefaultNodeID,
			ClaudeConfigDirs:   func() []string { return []string{configDir} },
			ProviderConfigDirs: func() []parser.ProviderConfigDir { return nil },
		},
	)

	r := chi.NewRouter()
	skills.NewHandler(m).Mount(r)
	return r, configDir
}

func post(t *testing.T, r http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/skills/materialize", reader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestMaterialize_MissingDryRunDefaultsToADryRun(t *testing.T) {
	r, configDir := newRouter(t)

	rec := post(t, r, `{}`)
	require.Equal(t, http.StatusOK, rec.Code)

	var rep materializer.Report
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rep))
	require.True(t, rep.DryRun, "a route that can overwrite a hand-edited file defaults to writing nothing")

	_, err := os.Stat(filepath.Join(configDir, "skills", "code-review", "SKILL.md"))
	require.True(t, os.IsNotExist(err))
}

func TestMaterialize_AnEmptyBodyIsADryRunToo(t *testing.T) {
	r, _ := newRouter(t)

	rec := post(t, r, "")
	require.Equal(t, http.StatusOK, rec.Code)

	var rep materializer.Report
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rep))
	require.True(t, rep.DryRun)
}

func TestMaterialize_DryRunFalseWrites(t *testing.T) {
	r, configDir := newRouter(t)

	rec := post(t, r, `{"dryRun": false}`)
	require.Equal(t, http.StatusOK, rec.Code)

	got, err := os.ReadFile(filepath.Join(configDir, "skills", "code-review", "SKILL.md"))
	require.NoError(t, err)
	require.Contains(t, string(got), "v1")
}

func TestMaterialize_ResponseIsCamelCase(t *testing.T) {
	r, _ := newRouter(t)

	rec := post(t, r, `{}`)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))

	for _, key := range []string{"nodeId", "dryRun", "leased", "partial", "targets", "entries"} {
		require.Contains(t, raw, key)
	}
	entries, ok := raw["entries"].([]any)
	require.True(t, ok)
	require.Len(t, entries, 1)
	first, ok := entries[0].(map[string]any)
	require.True(t, ok)
	for _, key := range []string{"resourceId", "targetKey", "outcome"} {
		require.Contains(t, first, key)
	}
}

func TestMaterialize_RejectsAnInvalidBody(t *testing.T) {
	r, _ := newRouter(t)

	rec := post(t, r, `not json`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd server && go test ./internal/api/skills/ -v`
Expected: the package does not exist.

- [ ] **Step 3: Implement the handler**

Create `server/internal/api/skills/handler.go`:

```go
// Package skills exposes the HTTP trigger for skill materialization: the one
// route in this server that writes into the user's own config directories.
package skills

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"

	"github.com/go-chi/chi/v5"

	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
	"github.com/lx-wnk/agent-dashboard/server/internal/materializer"
)

// maxRequestBytes caps the request body. The body carries one boolean.
const maxRequestBytes = 4096

// Handler serves POST /api/skills/materialize.
type Handler struct {
	m *materializer.Materializer
	// running single-flights the route. This is not redundant with the node
	// lease: coord_lock.Acquire is re-entrant for the same owner
	// (repo/coord_lock_repo.go:73) and the lease owner is per process, so two
	// concurrent requests in one server would both hold it and race each other
	// into the same files. The lease keeps two *instances* apart; this keeps
	// two requests apart. Same guard api/obsidian uses, for the same reason.
	running atomic.Bool
}

// NewHandler creates a Handler.
func NewHandler(m *materializer.Materializer) *Handler { return &Handler{m: m} }

// Mount registers the /api/skills/* routes on r.
func (h *Handler) Mount(r chi.Router) {
	r.Post("/api/skills/materialize", apierr.ErrorMiddleware(h.materialize))
}

type materializeRequest struct {
	// DryRun is a pointer so an absent field is distinguishable from an
	// explicit false. Absent means true: the default of a route that can
	// overwrite a hand-edited file is the one that writes nothing.
	DryRun *bool `json:"dryRun"`
}

func (h *Handler) materialize(w http.ResponseWriter, r *http.Request) error {
	dryRun := true
	var req materializeRequest
	err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBytes)).Decode(&req)
	switch {
	case errors.Is(err, io.EOF):
		// An empty body is a dry run, the same as an absent field.
	case err != nil:
		return apierr.NewAppError(http.StatusBadRequest, "invalid request body")
	case req.DryRun != nil:
		dryRun = *req.DryRun
	}

	if !h.running.CompareAndSwap(false, true) {
		return apierr.NewAppError(http.StatusConflict, "a materialization run is already in progress")
	}
	defer h.running.Store(false)

	rep, err := h.m.Run(r.Context(), dryRun)
	if err != nil {
		return fmt.Errorf("skills.materialize: %w", err)
	}

	// The report types carry their own camelCase json tags. Unlike the
	// registry and memory routes there is no ent entity here to re-encode:
	// Report exists only to be reported, so it has no schema columns to
	// republish by accident.
	apierr.WriteJSON(w, http.StatusOK, rep)
	return nil
}
```

- [ ] **Step 4: Wire it into the router**

In `server/internal/api/router.go`, add the import alias beside the existing ones:

```go
	apiskills "github.com/lx-wnk/agent-dashboard/server/internal/api/skills"
```

Add the field to `Deps`, directly after `ResourcesHandler` (`router.go:177`):

```go
	SkillsHandler          *apiskills.Handler
```

Add the mount directly after the `ObsidianHandler` block (`router.go:437-439`):

```go
		// Skill materialization writes into the user's own config directories,
		// so it belongs in the session-authenticated group with grants and the
		// Obsidian index trigger — never in the hook/MCP bearer-token group.
		if deps.SkillsHandler != nil {
			deps.SkillsHandler.Mount(r)
		}
```

- [ ] **Step 5: Wire it into the composition root**

In `server/serverapp/di.go`, add the import alias, then insert directly after the `resourcesHandler` block (`di.go:659-668`):

```go
	// Skill materialization — POST /api/skills/materialize. Shares the single
	// resourceRepo instance hoisted above, for the same reason resourcesHandler
	// does. The two directory sources are the ones the rest of the server
	// already trusts: the parser's four-tier Claude search set (which is what
	// finds a ~/.claude-personal), and the provider registry's enabled config
	// dirs (which already drops the ones that do not exist).
	var skillsHandler *apiskills.Handler
	if entClient != nil {
		skillsHandler = apiskills.NewHandler(materializer.New(
			resourceRepo,
			repo.NewSkillRepo(entClient),
			repo.NewMaterializationRepo(entClient),
			repo.NewCoordLockRepo(entClient),
			materializer.Resolver{
				NodeID:             repo.DefaultNodeID,
				ClaudeConfigDirs:   parser.AllClaudeConfigDirs,
				ProviderConfigDirs: providerRegistry.ConfigDirs,
			},
		))
	}
```

and add it to the `api.Deps` literal beside `ResourcesHandler`:

```go
		SkillsHandler:          skillsHandler,
```

- [ ] **Step 6: Run the tests to verify they pass**

```bash
cd server && go test ./internal/api/skills/ ./internal/materializer/ ./internal/db/repo/ -v
cd server && go build ./... && go vet ./...
```
Expected: all tests pass; build and vet are silent.

- [ ] **Step 7: Remove `skills-lock.json` and the snippet that could never work**

Delete `skills-lock.json`.

Replace `docs/guides/agent-skills.md` entirely with:

````markdown
# Agent Skills

Skills are owned by the dashboard's resource registry and **materialized** onto disk — the
`SKILL.md` files under a config directory are a derived artifact, not the source of truth.

## Materializing

```bash
# Dry run: reports what would be written, and writes nothing
curl -X POST http://127.0.0.1:13120/api/skills/materialize \
  -H 'Content-Type: application/json' -d '{}'

# Write
curl -X POST http://127.0.0.1:13120/api/skills/materialize \
  -H 'Content-Type: application/json' -d '{"dryRun": false}'
```

Both forms answer a report listing every target considered — every Claude config directory on
this machine, plus every enabled provider — and, per target, one of:

| Outcome | Meaning |
|---|---|
| `created` | No file was there. One was written. |
| `unchanged` | The file already holds the registry's content. |
| `repaired` | The file was one we wrote and had fallen behind the registry. It was rewritten. |
| `conflict` | The file was one we wrote and a person has since edited it. **Nothing was written**, and nothing will be on a later run either. Resolve it by hand. |
| `foreign` | The file was not written by this dashboard. It is never touched. |
| `unsupported` | That runtime has no skill format. Nothing was written, and nothing was faked. |
| `failed` | That target could not be processed. The others still ran; the report is marked `partial`. |

## What it will and will not do

- It writes **only** `<config dir>/skills/<slug>/SKILL.md` and `<project>/.claude/skills/<slug>/SKILL.md`.
  The slug is validated before any path is built.
- It never follows a symlink below the config directory, and never writes over a file it did not
  write itself.
- Two dashboard instances on one machine cannot both write: the run takes a node lease first, and
  the one that does not get it reports what it would have done and names the holder.

## `skills-lock.json` (removed)

Earlier revisions of this guide described a `skills-lock.json` and a `jq` one-liner for installing
from it. Nothing in the codebase ever read that file, and the snippet could not work as written —
it read a `.name` key that did not exist and passed an `owner/repo` slug to `curl` as a URL. Both
are gone. A skill's provenance now lives on its registry row (`origin`, `origin_ref`).
````

In `.gitignore`, replace the comment above the two skill directories (`.gitignore:17`):

```
# Agent skills — materialized from the registry, never committed
```

In `README.md:181` and `docs/README.md:22`, change the guide's one-line description from
"Installing project skills" / "Installing the project's AI agent skills" to
"Registry-owned skills and how they reach disk".

Add to `CHANGELOG.md` under `## [Unreleased]` → `### Added`:

```markdown
- **Skills the database owns now reach the disk that agent runtimes read.** `ResourceKindSkill` existed as a registry kind with no writer and no content — `GET /api/resources?kind=skill` answered `[]` and always would. A skill resource now carries content (`skill`), and a new materializer produces the `SKILL.md` files from it. The target is deliberately **not** "the filesystem": it is node × config dir × provider, so a machine running `~/.claude-personal` alongside `~/.claude` gets both, and every enabled non-Claude provider gets a *visible no-op* rather than a silent gap — none of Codex, Gemini, Junie or pi has a `SKILL.md` equivalent, and a user who authored a skill and saw nothing at all for Codex would have been misled. This is the only component in the dashboard that writes into a user's own directories, so most of it is refusal. A file it did not write is **never touched**, reported once as `foreign` and then remembered so it stops nagging. A file it wrote that a person has since edited is a `conflict`: it stops for that resource, does not merge, does not overwrite, and does not queue a retry that would overwrite it later. Ownership is proved by the recorded path, not by the marker in the file's frontmatter — a copied file carries the marker too, which is the lesson `agent-dashboard hooks install` already learned about `settings.json`. Writes are atomic (temp file, `Sync`, `Chmod`, `Rename`) and refuse any symlinked directory below the config root, while the config root itself may be a symlink because a `~/.claude` linked into `~/.claude-personal` is an ordinary dotfiles layout. Two dashboard instances on one machine cannot fight over the same files: a run takes a `coord_lock` node lease first, and the instance that does not get it degrades to read-only, reports exactly what it would have written, and names the holder — reachability is not ownership. `POST /api/skills/materialize` is the trigger and is **dry-run by default**: `{"dryRun": false}` is the opt-in to writing, and an absent field or an empty body writes nothing. It is session-authenticated, single-flighted, and takes no capability grant — the lease and the refusal rules are what gate it.
- **`skills-lock.json` is gone, together with the install command that could never work.** Nothing in the tree ever read the file — not Go, not TypeScript, not a shell script — and the documented `jq` one-liner in `docs/guides/agent-skills.md` read a `.name` key that does not exist (the name was the map key) and handed `curl` an `owner/repo` slug where it expected a URL. Its `computedHash` values were verified by nothing, and its seven entries contradicted the five the same document listed as "current skills". Provenance now lives on the registry row's `origin`/`origin_ref`, and the guide documents materialization instead. Leaving a documented command that cannot work is worse than having no command.
```

- [ ] **Step 8: Verify the removal is complete**

```bash
grep -rn 'skills-lock' --include='*.go' --include='*.ts' --include='*.md' --include='*.json' --include='*.sh' . | grep -v node_modules | grep -v docs/superpowers
```
Expected: no output except the deliberate mention inside `docs/guides/agent-skills.md`.

- [ ] **Step 9: Commit**

```bash
cd server && gofmt -l ./internal/api/skills/ ./serverapp/ && go vet ./...
```

Commit message: `feat: trigger skill materialization over HTTP, dry-run by default`

---

## Self-Review

### Spec coverage

| Spec section | Lands in | Note |
|---|---|---|
| §1 Purpose — skills first, hooks and settings later | Tasks 1–6 | Hooks and settings are §11-deferred and nothing here touches them. |
| §2.1 Config dirs multiply | Task 2 (`Resolver.ClaudeConfigDirs`), Task 6 (wired to `parser.AllClaudeConfigDirs`) | The four-tier set, `~/.claude-personal` included. |
| §2.2 Providers multiply, most have no skill concept | Task 2 (`Resolver.ProviderConfigDirs`, `AdapterNone`) | Golden test names all four providers literally. |
| §2.3 Target list; `none` is a visible no-op | Task 2 (`Adapter`), Task 5 (`OutcomeUnsupported` + its "touches no filesystem" test) | |
| §3 Exact on-disk layout | Task 3 (`SkillPath`) | Both templates written out literally; unknown layers refused. |
| §3 Only `user` and `project` are writable | Task 3 (`SkillPath` default branch), tested | `builtin` and `plugin` are never constructible as a `Target.Layer`. |
| §3 Name resolution, fallback never fires | Task 3 (`RenderClaudeSkill` writes `name:` = slug = directory) | Its own test. |
| §4 Does not go through `/api/config/file` | Tasks 3, 6 | Own path construction from `cmdscope`'s templates; own route. |
| §4 Only two path shapes, slug validated | Task 3 (`validation.IsValidSlug` before any join), tested with eight refusals | |
| §4 Never follows a symlink | Task 4 (`Lstat` + `IsRegular` → `foreign`), Task 5 (`refuseSymlinkBelow`) | Two different holes, two checks; both tested. |
| §5 Refuse rather than overwrite | Task 4 (`OutcomeConflict`, `OutcomeForeign`) | |
| §5 A marker is not proof; the path is | Task 1 (`materialization` table), Task 4 (`TestClassify_AFileCarryingOurMarkerIsStillForeignWithoutARecord`) | |
| §5 Three-way outcome | Task 4 (`unchanged`/`created`/`repaired` plus the two refusals) | |
| §5 Atomic write with `Sync` and explicit mode | Task 5 (`atomicWrite`) | Explicitly the `hookscript` variant, not `api/config/file.go`'s. |
| §5 Rewriting on every install is deliberate | Task 4 (`OutcomeRepaired`) | Refined — see "deliberate divergences" below. |
| §5.1 The five outcomes table | Task 4 (one test per row), Task 5 (one end-to-end run per row) | |
| §6 Conflicts, not drift; the name is `materialization_conflict` | Task 4 (`OutcomeConflict`), Task 5 (second run does not overwrite either) | The word "drift" appears nowhere in the code this plan adds. |
| §6 Detection mechanism | Task 4 | One named divergence — see below. |
| §7 Node leases, `namespace = "materialize"`, `key = <node_id>` | Task 5 (`LeaseNamespace`, `m.NodeID`) | |
| §7 Without the lease it is read-only and names the holder | Task 5 (`readOnly` reason, `Report.LeaseHolder`), tested | |
| §7 Lazy expiry and reachability-is-not-ownership are accepted | Task 5 (`DefaultLeaseTTL` doc comment) | |
| §8 What replaces `skills-lock.json` | Task 6 | File, snippet, `.gitignore` comment and both doc index rows in one commit. |
| §9 No lease | Task 5 | |
| §9 Target directory unwritable — partial | Task 5 (`Report.Partial`), tested with a 0500 directory | |
| §9 Provider has no skill format | Task 2 + Task 5 | |
| §9 Config dir does not exist — skipped, not created | Task 2 (`isDir` filter), tested | |
| §9 Foreign file — never touched, reported once, then remembered | Task 5 (`foreign` record with an empty hash), tested across two runs | |
| §9 Conflict — stops, no merge, no retry-that-overwrites | Task 5, tested across two runs | |
| §9 Slug fails validation — refused before any path is built | Task 3, tested | |
| §9 Rename fails — temp removed, target untouched | Task 5 (`atomicWrite`'s deferred `Remove`), tested | |
| §10 Target resolution golden test | Task 2 | Two config dirs, four providers, one node, expected list literal. |
| §10 Format adapters, `none` produces a recorded no-op touching no filesystem | Task 3 (golden render), Task 5 (the assertion) | |
| §10 Ownership — five cases incl. a foreign file that survives | Task 4 + Task 5 | |
| §10 Conflict — no write, and no write on the second run | Task 5 | |
| §10 Lease contention — the loser writes nothing and reports the holder | Task 5 | |
| §10 Path safety — traversal and symlink escape refused before any write | Tasks 3, 4, 5 | |
| §10 Atomicity | Task 5 (`TestApply_AFailedRenameLeavesNoPartialFileAndNoTempFile`) | An interrupted process cannot be simulated in-process; a failed rename after a synced temp write is the same boundary. |
| §11 Deferred (hooks/settings, cross-node, two-way sync, emulated formats) | Nothing implements any of the four | `res.NodeID != m.NodeID` skips foreign nodes rather than pretending to reach them. |

### Deliberate divergences from the spec

1. **§6 mtime.** The materialization record stores a content hash and no mtime. Full reasoning in Task 4's design note: the spec's own text concedes the whole-second granularity misses a same-second edit and names the hash as the mitigation, so the hash decides every case and the mtime would be a stored field nothing reads. The one situation where mtime is stronger — a file edited and then reverted to our exact bytes — loses no human work when overwritten.
2. **§5 "rewriting on every install is deliberate."** `hookscript.Install` rewrites unconditionally; the materializer rewrites only when the hash differs. The property that comment protects — a newer version replacing an older artefact — is preserved by `repaired`, which is exactly the hash-differs case. An unconditional rewrite would make every run report a write and touch the mtime of files nobody changed, which is noise in the one component whose reports have to be readable.
3. **Line-number drift in §2.1, §3, §5.** Six of the spec's citations have moved. The corrected references are in the "Verified against the code" table and are what the code comments cite; the described behaviour is unchanged in every case.

### Known gaps this plan does not close

1. **Nothing creates a skill resource.** `ResourceKindSkill` still has no authoring surface — the overview's roadmap puts "L3 skill authoring" explicitly *on top of* K4, and this plan builds the K4 half only. Until L3 or a one-shot importer lands, a skill resource exists only if something writes it through `ResourceRepo` + `SkillRepo` directly, and the materializer's production runs will report zero entries. This is the sequencing the overview asks for, but it means the feature is not demonstrable end-to-end from the UI on the day it merges. Land L3 or an importer next.
2. **The report is not persisted for reading.** `materialization.outcome` holds the last classification per target, but no route reads it back: a conflict is visible only in the response of the run that found it. §6 requires the conflict to be "recorded and surfaced" — it is recorded; "surfaced" is currently the run's own response. A `GET /api/skills/materializations` and a panel are the natural follow-up and were left out because the spec asks for no UI and the overview puts the shell in S1.
3. **No capability.** Deliberate, reasoned in the decisions table, and worth revisiting the moment any surface beyond loopback exists.

### Placeholder scan

None. Every step contains the code it asks for; there is no "add error handling", no "similar to Task N", and no `t.Skip`. One conditional instruction exists and is explicitly conditional, not a placeholder: the root-user note under Task 5 Step 1, which applies only if CI is found to run as root and says not to weaken the assertion instead.

### Type consistency across tasks

- `repo.UpsertSkillInput{ResourceID, Description, Body}` — defined Task 1, constructed with those exact fields in Tasks 5 and 6.
- `repo.RecordMaterializationInput{ResourceID, TargetKey, Path, ContentHash, Outcome}` — defined Task 1, constructed twice in Task 5 with those fields.
- `MaterializationRepo.Get` returns `(nil, nil)` on absence — Task 5 nil-checks before reading `.ContentHash` and `.CreatedAt`; `CreatedAt` comes from `IDTimestampsMixin` (`schema/mixins.go:17-22`), which both new schemas embed.
- `materializer.Target{NodeID, Provider, Layer, Root, Adapter}` — defined Task 2; Task 3 reads `.Adapter`, `.Layer`, `.Root`, `.Key()`; Task 5 reads `.Root`, `.Provider`, `.Adapter`, `.Key()`.
- `Resolver{NodeID, ClaudeConfigDirs, ProviderConfigDirs}` — defined Task 2; Task 5's harness mutates `h.m.Resolver.ProviderConfigDirs` and `.ClaudeConfigDirs`, which requires `Materializer.Resolver` to be an exported field — it is.
- `Skill{ResourceID, Slug, Description, Body}` — defined Task 3; constructed in Task 4's test helper and in Task 5's `Run`. Neither carries `Name`; `Run` uses `res.Slug`, not `res.Name`.
- `SkillPath(t Target, slug string) (string, error)` — defined Task 3; called in Task 5's `one` and in `apply_test.go`.
- `Classify(path string, want []byte, recordedHash string) (Outcome, error)` — defined Task 4; called with three arguments in Task 5.
- `Apply(t Target, path string, want []byte) error` — defined Task 5; called with three arguments in `one` and in `apply_test.go`.
- `materializer.New(resources, skills, records, locks, resolver)` — defined Task 5; called with those five in the same order in Task 6's DI snippet and its handler test.
- `Report`/`ReportEntry` json tags — defined Task 5; Task 6's `TestMaterialize_ResponseIsCamelCase` asserts `nodeId`, `dryRun`, `leased`, `partial`, `targets`, `entries`, `resourceId`, `targetKey`, `outcome`, all of which are present.
- `parser.ProviderConfigDir{Provider sdk.Provider; Path string}` — Task 2's test constructs it with an explicit `sdk.Provider(...)` conversion, Task 5's with an untyped string constant; both compile.

**Task ordering is load-bearing for the tests.** Task 5's `apply_test.go` uses `userTarget` (defined in Task 3's `path_test.go`) and `want`/`writeFile` (defined in Task 4's `classify_test.go`), all in `package materializer_test`. Executed in order the package compiles at every step; executed out of order it will not.

---

## FOLLOW-UPS

Findings outside this plan's scope, recorded rather than fixed:

1. **Three atomic writers now exist** — `api/config/file.go:190` (no `Sync`), `hookscript/hookscript.go:47` (with `Sync`), and this plan's `materializer/apply.go` (with `Sync`). The first is the odd one out and its omission is not documented as deliberate. One exported helper would settle it; cross-package refactoring of two working write paths does not belong in the change that introduces a third.
2. **`GET /api/skills/materializations`** plus a panel, so a conflict found by one run is still visible on the next page load. See gap 2 above.
3. **`parser.AllClaudeConfigDirs`' tier 4 gates on `<candidate>/projects` existing** (`parser.go:157-158`) — a session-history directory. A config dir holding skills but no session history is invisible to the materializer for a reason that has nothing to do with skills. Not changed here: that predicate is shared with JSONL discovery and moving it is a behaviour change to the scanner.
4. **`repo.DefaultNodeID` is hard-coded at the composition root.** Correct until the node registry (V2/C1) lands; it is the single place to change when it does.
