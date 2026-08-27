# AgenticOS — Resource Registry

**Date:** 2026-08-27
**Status:** Approved design
**Stage:** MVP (unit K1)
**Parent:** `2026-08-27-agenticos-overview-design.md`
**Implements:** decision D1 (ARMS is a real resource model) and D2 (node-ready from day one)

---

## 1. Purpose

Give every managed thing — Applications, Routines, Skills, Memory spaces — one identity, one
scope, one lifecycle state and one place for grants to attach to.

The registry is deliberately **narrow**. It is not a home for what a resource *does*; it is the
row that says a resource exists, where it applies, on which node, and in what state.

---

## 2. What exists today

Ten entities already behave like managed resources. None of them agree on how.

| Entity | Identity | Scope expression | Lifecycle |
|---|---|---|---|
| `plugin` | **manifest id** (caller-supplied) | none | derived from `installed_at` + `active` (`schema/plugin.go:11-13`) |
| `spawner` | UUID | `is_default` boolean singleton | `built_in` flag; delete guarded by three sentinels |
| `task_schedule` | UUID | `project_id` nullable | `enabled` boolean |
| `project` | UUID | — | none |
| `coord_lock` | UUID | free-string `namespace`, unvalidated | lease expiry |
| `scratchpad` | UUID | free-string `namespace`, unvalidated | none |
| `remote_registration` | UUID | `user_id` required | none |
| `app_setting` | UUID | none (global only) | none |
| `provider_setting` | UUID | `provider_id` unique | `enabled` boolean |
| `plugin_setting` | UUID | `(plugin_id, key)` | none |
| `system_prompt` | UUID | `scope` enum — `"task"` declared but **not wired** (`schema/system_prompt.go:17-18`) | `priority` int |
| `pipeline_config` | UUID | `project_id` **empty-string sentinel** | none |

### 2.1 The conventions that are already right

Two of these are correct and become the standard rather than being replaced.

**The scope sentinel.** `pipeline_config` uses `project_id = ""` for global, and the schema
explains exactly why (`schema/pipeline_config.go:21-23`):

```go
// "" = global scope; any non-empty value = project scope.
// Using a sentinel instead of NULL so the unique index (project_id, key) fires correctly on SQLite.
```

Its lookup semantics are already the three that a scoped registry needs, and they are named
distinctly (`repo/pipeline_config_repo.go:14-47`): `GetStringScoped` (project row, falling back to
global), `GetStringForScope` (exactly this scope, **no** fallback), `GetAllScoped` (merged, project
wins). That trio is the model for every scoped read in the registry.

**The slug rule.** `server/internal/validation/slug.go:10` is canonical and says so:

```go
var SlugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
```
with the instruction "Import this instead of defining a local copy."

### 2.2 The conventions that are wrong

These are the "historically grown" parts, and the project's stated principles — DRY, KISS, Clean
Code, SSOT — make fixing them part of this unit rather than a follow-up.

**W1 — Zero shared schema code.** 29 schema files, no ent mixin, no shared field set. `id`,
`created_at` and `updated_at` are hand-repeated in every file, and 9 of 12 carry a redundant
`.StorageKey("id")` while 3 do not.

**W2 — 27 hand-written repositories with identical shape.** 27 `type XRepo interface`, 27
`func NewXRepo(client *ent.Client) XRepo`, 27 `struct{ client *ent.Client }`. The only shared
production code in the whole package is two functions living inside unrelated files:
`IsNotFound` in `repo/plugin_repo.go:32` and `rollback` in `repo/spawner_repo.go:158-163`.

**W3 — Two transaction idioms.** `defer func() { _ = tx.Rollback() }()` in two repos; explicit
per-branch rollback in seven.

**W4 — Positional constructors with boolean clear-flags.** `SpawnerRepo.Create`/`Update` take 11
parameters, `ProjectRepo.Create`/`Update` take 12, with `clearX bool` arguments to distinguish
"unchanged" from "set to NULL" (`repo/project_repo.go:127-146`). Every other repo above four
parameters uses a named input struct. This is the clearest Clean Code violation in the layer.

**W5 — The slug rule is duplicated three ways.** The canonical `SlugRE`; a separate
`pluginIDRe` with the same shape but **unbounded length** (`plugin/registry.go:23-25`); and four
handler sites that hardcode the message string `"slug must match ^[a-z0-9][a-z0-9-]{0,63}$"`
instead of using the exported `SlugPatternMessage`.

**W6 — Six different ways to say "scope".** Empty-string sentinel, nullable column, enum column,
boolean singleton, required tenancy column, and an unvalidated free string. `coord_lock.namespace`
and `scratchpad.namespace` arrive straight from a URL parameter or an MCP string argument with no
validation at all.

**W7 — Delete protection exists for exactly one entity.** `spawner` has `ErrSpawnerBuiltIn`,
`ErrSpawnerInUse`, `ErrSpawnerIsDefault`. `project`, `plugin` and `prompt_template` have none.

---

## 3. Design

### 3.1 The registry table

```
resource: id, kind, slug, name, scope_kind, scope_ref, node_id,
          state, version, origin, origin_ref, created_at, updated_at

kind       ∈ { application, routine, skill, memory_space }
scope_kind ∈ { global, project, application }
state      ∈ { discovered, installed, enabled, disabled, orphaned }
origin     ∈ { builtin, local, remote }
```

Unique index on `(kind, scope_kind, scope_ref, slug)`.

`scope_ref` uses the **empty-string sentinel** for global, following `pipeline_config` and for the
same SQLite reason: two NULLs are distinct, so a nullable column cannot carry a unique index that
prevents duplicate globals.

`node_id` is `local` in the MVP. It exists now because adding it later means a migration through
every resource table and every scoped query — the one cost that is cheap exactly once (overview
D2).

`state` generalises the derivation `plugin` already documents (`schema/plugin.go:11-13`): no
`installed_at` means discovered, installed but inactive means disabled, active means enabled.
Making it an explicit column removes a derivation that today lives in
`pluginmgmt/controller.go:118-126` and is invisible from the row.

`origin` plus `origin_ref` records where a resource came from — a marketplace slug, a GitHub
source, a local authoring action. This is what lets an upstream update avoid silently overwriting
a local edit, which `skills-lock.json` gestures at today without any code reading it.

### 3.2 What the registry does not hold

Kind-specific data stays in the kind's own table. `plugin`, `spawner` and `task_schedule` keep
their schemas and gain a `resource_id`.

This is the direct answer to review finding R2: one polymorphic table holding process addresses,
cron expressions, file paths and knowledge rows collapses into dozens of nullable columns. The
registry references; it does not absorb.

### 3.3 The shared field set

An ent **Mixin** — the mechanism ent provides and this codebase uses zero times — carries `id`,
`created_at`, `updated_at`, and for resource-backed entities also `resource_id`.

```go
type ResourceMixin struct{ mixin.Schema }
```

This closes **W1** and removes the `.StorageKey("id")` inconsistency by having exactly one
declaration of it.

### 3.4 Scope as a value object

One type, used by every scoped read and write:

```go
type Scope struct {
    Kind ScopeKind   // global | project | application
    Ref  string      // "" for global — the sentinel, not NULL
}
```

with the three lookup semantics `pipeline_config` already proved, named identically so the idiom
transfers: `Get` (specific, falling back to global), `GetExact` (no fallback), `GetMerged`
(specific wins).

This closes **W6**. Existing entities migrate to it as they are touched; nothing is rewritten
speculatively. `coord_lock.namespace` and `scratchpad.namespace` gain validation as part of that
move — an unvalidated string arriving from a URL parameter is the kind of thing that only looks
harmless until a path is built from it.

### 3.5 Repository shape

**KISS applies here.** The temptation is a generic `Repository[T]`; the correct scope is smaller:

- One shared `repo` package file holding `IsNotFound`, `rollback` and a single `WithTx` helper,
  replacing the two idioms with one (**W2**, **W3**).
- Every method above four parameters takes a named input struct, and "clear this field" is
  expressed by a `*string` set to a pointer-to-empty rather than a parallel boolean (**W4**).
- Error wrapping keeps the existing uniform `fmt.Errorf("entity.Method: %w", err)`, which is
  already consistent and needs no change.

No base class, no generic repository, no ORM abstraction over the ORM. The duplication that hurts
is the transaction handling and the constructor shape, not the existence of 27 interfaces.

### 3.6 Identity and naming

- Server-generated UUIDv4 for every registry row. The `plugin` manifest id moves to
  `origin_ref`; the plugin's registry `slug` is derived from it. A human-authored primary key is a
  migration hazard the moment the human renames it.
- One slug rule: `validation.SlugRE`. `pluginIDRe` is deleted and its call sites import the
  canonical one; the four hardcoded message strings become `validation.SlugPatternMessage`
  (**W5**). This is exactly the parity case ADR-0011 names.

### 3.7 Lifecycle and deletion

Every registry kind gets the delete protection that only `spawner` has today (**W7**): a resource
that is referenced, built in, or currently the default cannot be deleted, and the refusal names
which of the three applies. The sentinel-error pattern from `repo/spawner_repo.go:18-27` is the
model; it is lifted into the shared file rather than copied.

---

## 4. Migration

1. Add `resource` plus the `ResourceMixin`. Nothing reads them.
2. Backfill one kind at a time, starting with the kind that has the least behaviour attached.
   `plugin` first — it already has a state derivation to replace and an origin to record.
3. Point the consuming service at the registry for identity and scope only; behaviour keeps
   reading the kind table.
4. Consolidate slug validation and delete `pluginIDRe`.
5. Introduce the shared repo helpers and migrate the two transaction idioms to one.
6. Convert `SpawnerRepo` and `ProjectRepo` to input structs.

Steps 1–3 are additive per kind. Steps 4–6 are refactors behind unchanged behaviour and each ships
with its own green gate.

> **Migration hazard, twice over.**
> **Indexes:** pre-create every unique index under ent's exact generated name before auto-migrate,
> read from `server/internal/db/ent/migrate/schema.go`. A differently named pre-seeded index does
> not prevent SQLite's 12-step rebuild, which fails on existing databases
> (`server/internal/db/client.go:429-435`, PR #207).
> **JSON columns:** a JSON column added to a table with existing rows needs an explicit
> `entsql.Default("{}")`, and the raw form matters — the escaped form ships literal `''{}''` and
> crashes every load. This bug shipped once already; its repair migration is still in the tree
> (`serverapp/plugin_migrate.go:21-41`), and the schema comment explaining it is at
> `schema/spawner.go:25-30`.

Also note: `go test ./...` regenerates the ent tree. `git checkout -- server/internal/db/ent/`
before committing unless the regeneration is the change (`AGENTS.md:41`).

---

## 5. Failure modes

| Situation | Behaviour |
|---|---|
| Two resources claim the same slug in the same scope | The unique index refuses. The error names both, because a collision usually means an import went wrong |
| A kind row exists without a registry row | The reconciler adopts it and logs. Deleting user data because a join failed is never correct — the same stance `lifecycle_discovery.go:95-111` already takes when zero manifests are found |
| A registry row exists without its kind row | State becomes `orphaned`, visible in the UI, never auto-deleted |
| A resource is referenced by something else on delete | Refused, naming the referent |
| `node_id` mismatch in the MVP | Impossible by construction; the column is written from a constant. The check exists anyway, because V2 turns that constant into a variable |

---

## 6. Testing

- **Scope resolution** — the three semantics against a fixture set: fallback, exact, merged. The
  empty-string sentinel must behave identically to what `pipeline_config` does today, proven by
  running the same table against both.
- **Uniqueness** — a duplicate slug in the same scope is refused; the same slug in two scopes is
  allowed.
- **Migration parity** — for each backfilled kind, the set of resources visible through the
  registry equals the set visible through the kind table, before and after.
- **Slug consolidation** — the deleted `pluginIDRe` and the canonical `SlugRE` must accept and
  reject identical inputs, except for the length bound that `pluginIDRe` was missing. That
  difference is asserted explicitly, since it is a behaviour change: an over-long plugin id that
  was previously accepted is now rejected.
- **Delete protection** — one test per refusal reason per kind.
- **Database open** — a fresh database and a database seeded in the pre-index shape both open
  twice without error, mirroring `server/internal/db/client_test.go:191-235`.

---

## 7. Deferred

| Item | Why not now |
|---|---|
| Versioned migration files (Atlas) | A known gap independent of this work; auto-migrate plus pre-migrations is the current contract and changing it is its own change |
| A generic repository layer | KISS. The duplication that hurts is transactions and constructors, and both are fixed without one |
| Cross-node uniqueness | V2 |
| Migrating `coord_lock`/`scratchpad` onto `Scope` | They work and nothing in the MVP touches them; they move when something does |
