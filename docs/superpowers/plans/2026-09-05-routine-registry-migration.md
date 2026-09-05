# Routine Registry Migration — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every `task_schedule` row a persistent `resources` identity so that routines are real registry entries — visible to `ListForKind`, grantable by resource reference, and indistinguishable from other resource kinds at the API layer.

**Architecture:** A boot-time reconciler (`ReconcileScheduleResources`) backfills existing schedules with `resources` rows, mirroring the plugin pattern in `plugin_resource.go`. CRUD operations on `task_schedule` propagate state to the linked resource row inline. The read-time projection in `api/resources/handler.go` is deleted — routines flow through `ListMerged` like every other kind.

**Tech Stack:** Go 1.26 (chi, ent ORM, modernc/sqlite, cobra), Vue 3 + TypeScript SPA (Vite, pnpm, Vitest). This plan is Go-only on the write side; the handler cleanup removes a Go special case and requires no frontend changes (the frontend already consumes the generic resource list).

**Spec:** `docs/superpowers/specs/2026-09-05-routine-registry-migration-design.md`

## Global Constraints

- Server MUST bind to `127.0.0.1`. Never `0.0.0.0`.
- Never run `go test ./...` or `task test` while implementing — both regenerate `server/internal/db/ent/`. Use package-scoped test paths. Task 1 regenerates ent deliberately; that is the only task where a changed `server/internal/db/ent/` belongs in the commit.
- ent regeneration MUST use the project's own path: `cd server && go generate ./internal/db/ent/` (it carries `--feature sql/upsert`). Verify after regen: `grep -rl "OnConflict" server/internal/db/ent/ | head` must print files. Then restore `server/go.sum` from HEAD — `go generate` pulls codegen-only dependencies into it that `go build` does not need.
- After regen, restore `server/internal/db/ent/runtime/runtime.go` from HEAD if it lost its `Version`/`Sum` constants. A local ent version differing from the committed one strips them; that diff does not belong in the commit.
- `gofmt -l <pkg>` is mandatory before every commit. CI runs `golangci-lint fmt --diff`, which fails on struct-literal alignment that `go build`, `go vet` and `go test` all pass.
- Run `go vet ./...` module-wide (from `server/`) before every commit — a package-scoped `go test` misses `_test.go` files in sibling packages that reference a changed exported type.
- ent auto-migrate is non-destructive and this project deliberately does not enable `WithDropColumn`. Added columns must be additive-safe with defaults.
- All code, comments, commit messages, PR titles and bodies in English. Conventional Commits.

---

## Verified against the code before planning

| Spec claim | Verified at | Note |
|---|---|---|
| Plugin reconciler is the pattern to mirror | `server/internal/db/repo/plugin_resource.go:24-66` | Exact. Idempotent, queries `resource_id == ""`, upserts, backlinks. |
| `task_schedule` has no `resource_id` | `server/internal/db/ent/schema/task_schedule.go:17-58` | Exact. No `resource_id` field. |
| `slug_prefix` has no unique index | `server/internal/db/ent/schema/task_schedule.go:60-65` | Exact. Only `enabled` and `next_run_at` indexed. |
| `slug_prefix` has no format validation | `server/internal/db/repo/task_schedule_repo.go:99-146` | Exact. `SetSlugPrefix(in.SlugPrefix)` with no regex check. |
| Projection in handler.go writes nothing | `server/internal/api/resources/handler.go:120-152` | Exact. `routineViews()` reads `task_schedule` rows, returns `resourceView` structs, persists nothing. |
| Special-case routing for routines | `server/internal/api/resources/handler.go:204-210` | Exact. `if kind == repo.ResourceKindRoutine` short-circuits before `ListMerged`. |
| `ReconcilePluginResources` called at boot | `server/serverapp/di.go:333` | Exact. |
| `schedule_repo.Delete` is a bare delete | `server/internal/db/repo/task_schedule_repo.go:216-220` | Exact. `DeleteOneID(id).Exec(ctx)`, no cascade logic. |
| Grant context for routines uses schedule ID | `server/internal/memory/authorize.go:75`, `server/internal/db/repo/grant_repo.go:28` | Exact. `GrantContextRoutine = "routine"`, ref is the schedule UUID. |
| Resources unique index is `(kind, scope_kind, scope_ref, slug)` | `server/internal/db/ent/schema/resource.go` | Exact. |
| `ResourceKindRoutine` has no production writer | `server/internal/api/resources/handler.go:140` | Exact. Only set in the read-time projection, never persisted. |

---

## Decisions this plan makes because the spec is silent

| Question | Decision | Why |
|---|---|---|
| Resource slug value | Schedule UUID (`task_schedule.id`) | `slug_prefix` is neither unique nor validated; UUID is guaranteed unique and safe. See spec §4.2. |
| Where lifecycle sync lives | Inline in `schedule_resource.go` as exported helpers called from `schedule_repo.go` methods | Keeps the resource-sync concern in one file, avoids scattering upsert calls across repo methods. The schedule repo calls into the helper; the helper calls into `ResourceRepo`. |
| State mapping | `enabled → "enabled"`, `!enabled → "disabled"`, `deleted → "archived"` | Matches plugin's `Active → enabled` convention. No `"discovered"` state — schedules are always explicitly created, never scanned. |
| Projection test cleanup | Delete `routine_projection_test.go` | It tests behavior that no longer exists after Task 4. |
| Transaction boundary | Resource upsert runs in the same database transaction as the schedule mutation | SQLite serializes writes; wrapping in `WithTx` ensures the schedule and resource are consistent. If resource upsert fails, the schedule mutation rolls back. |

---

## File Structure

| File | Responsibility |
|---|---|
| `server/internal/db/ent/schema/task_schedule.go` | Add `resource_id` field (optional string, default `""`) |
| `server/internal/db/ent/` | Regenerated (Task 1 only) |
| `server/internal/db/repo/schedule_resource.go` | `ReconcileScheduleResources` + lifecycle helpers (`UpsertScheduleResource`, `ArchiveScheduleResource`) |
| `server/internal/db/repo/schedule_resource_test.go` | Tests for reconciler and lifecycle helpers |
| `server/internal/db/repo/task_schedule_repo.go` | Modified: Create/Delete call lifecycle helpers |
| `server/internal/db/repo/task_schedule_repo_test.go` | Extended: verify resource rows are created/archived on schedule CRUD |
| `server/serverapp/di.go` | Add `ReconcileScheduleResources` call after plugin reconciliation |
| `server/internal/api/resources/handler.go` | Remove `routineViews()`, remove special-case `if` branch |
| `server/internal/api/resources/routine_projection_test.go` | Deleted |

---

### Task 1: Schema — add `resource_id` to `task_schedule`

**Files:**
- Modify: `server/internal/db/ent/schema/task_schedule.go`
- Regenerate: `server/internal/db/ent/` (deliberate, belongs in this commit)

**Interfaces:**
- Produces: `task_schedule.resource_id` column (optional string, default `""`, immutable after set)

**Design note:** The field is `Optional().Default("")` — not `Nillable()` — matching `plugin.resource_id`'s convention. Default `""` means "not yet reconciled." The reconciler and CRUD helpers query for `resource_id == ""` to find unlinked rows, exactly as `ReconcilePluginResources` queries `plugin.ResourceIDEQ("")`. The field is not immutable at the ent level (the reconciler sets it after creation), but application code treats it as write-once.

**Steps:**
- [ ] Add `resource_id` field to `task_schedule.go` schema: `field.String("resource_id").Optional().Default("")`
- [ ] Run ent codegen: `cd server && go generate ./internal/db/ent/`
- [ ] Verify OnConflict survived: `grep -rl "OnConflict" server/internal/db/ent/ | head`
- [ ] Restore `server/go.sum` from HEAD: `git checkout HEAD -- server/go.sum`
- [ ] Restore `server/internal/db/ent/runtime/runtime.go` from HEAD if `Version`/`Sum` constants were stripped
- [ ] Verify build: `cd server && go build ./...`
- [ ] Verify vet: `cd server && go vet ./...`
- [ ] Verify fmt: `gofmt -l internal/db/ent/schema/task_schedule.go`
- [ ] Commit: `feat: add resource_id field to task_schedule schema`

---

### Task 2: Reconciler and lifecycle helpers

**Files:**
- Create: `server/internal/db/repo/schedule_resource.go`
- Create: `server/internal/db/repo/schedule_resource_test.go`

**Interfaces:**
- Consumes: `repo.ResourceRepo` (existing), `*ent.Client` (existing), `repo.TaskScheduleRepo` (existing)
- Produces:
  - `repo.ReconcileScheduleResources(ctx context.Context, resources ResourceRepo, client *ent.Client) (int, error)` — boot-time backfill
  - `repo.UpsertScheduleResource(ctx context.Context, resources ResourceRepo, client *ent.Client, s *ent.TaskSchedule) (string, error)` — returns the `resources.id`; called from Create and reconciler
  - `repo.ArchiveScheduleResource(ctx context.Context, resources ResourceRepo, resourceID string) error` — sets state to `"archived"`; called from Delete

**Design note — `ReconcileScheduleResources`:**
Mirrors `ReconcilePluginResources` line for line:
1. `client.TaskSchedule.Query().Where(taskschedule.ResourceIDEQ("")).All(ctx)`
2. For each schedule: derive state (`Enabled → "enabled"`, else `"disabled"`), call `resources.Upsert` with `Kind: ResourceKindRoutine`, `Slug: s.ID` (the schedule UUID), `Name: s.Name`, `Scope: GlobalScope()`, `Origin: ResourceOriginLocal`, `OriginRef: s.ID`.
3. Backlink: `client.TaskSchedule.UpdateOneID(s.ID).SetResourceID(res.ID).Exec(ctx)`.
4. Log and skip on error (one bad row must not block the rest).

**Design note — `UpsertScheduleResource`:**
Extracted so that both the reconciler and `Create` share one code path. Accepts the schedule, calls `resources.Upsert`, backlinks, returns the resource ID.

**Design note — `ArchiveScheduleResource`:**
Thin wrapper: `resources.SetState(ctx, resourceID, ResourceStateArchived)`. Extracted for naming clarity and to centralize the archived-state constant. `ResourceStateArchived` is not yet defined — this task adds it alongside `ResourceStateOrphaned` in `resource_repo.go` (currently at line 31). **Wait — `ResourceStateOrphaned` already exists at line 31.** Check whether an `"archived"` constant already exists. If not, add `ResourceStateArchived = "archived"` to the const block.

**Steps:**
- [ ] Check if `ResourceStateArchived` exists in `resource_repo.go`. If not, add it to the state const block.
- [ ] Write `schedule_resource_test.go`:
  - Test: reconciler links all unlinked schedules and is idempotent (second run returns 0)
  - Test: `UpsertScheduleResource` creates a resource row with correct kind/slug/name/state
  - Test: `UpsertScheduleResource` for an enabled schedule sets state `"enabled"`
  - Test: `UpsertScheduleResource` for a disabled schedule sets state `"disabled"`
  - Test: `ArchiveScheduleResource` sets state to `"archived"`
  - Test: reconciler skips schedules that already have a `resource_id`
- [ ] Run tests to see them fail: `cd server && go test ./internal/db/repo/ -run TestReconcileSchedule -count=1`
- [ ] Implement `schedule_resource.go`
- [ ] Run tests to see them pass
- [ ] Verify vet: `cd server && go vet ./...`
- [ ] Verify fmt: `gofmt -l internal/db/repo/schedule_resource.go`
- [ ] Commit: `feat: add schedule resource reconciler and lifecycle helpers`

---

### Task 3: CRUD integration — wire schedule mutations to resource rows

**Files:**
- Modify: `server/internal/db/repo/task_schedule_repo.go` (Create, Delete, SetEnabled)
- Modify: `server/serverapp/di.go` (boot reconciliation call)
- Extend: `server/internal/db/repo/task_schedule_repo_test.go` (verify resource side-effects)

**Interfaces:**
- Consumes: `repo.UpsertScheduleResource`, `repo.ArchiveScheduleResource`, `repo.ResourceRepo.SetState` (all from Task 2)
- Modifies:
  - `TaskScheduleRepo` interface — the implementation needs access to `ResourceRepo`. Two options: (a) inject `ResourceRepo` into the repo struct, or (b) call the helpers from the handler/service layer. Option (a) is cleaner — the schedule repo already receives `*ent.Client`, adding `ResourceRepo` is one more constructor parameter. **Decision: inject `ResourceRepo` into `entTaskScheduleRepo`.**
  - `NewTaskScheduleRepo(client *ent.Client, resources ResourceRepo) TaskScheduleRepo` — signature change. All call sites in `di.go` and tests must pass the new parameter.

**Design note — Create:**
After `client.TaskSchedule.Create().Save(ctx)`, call `UpsertScheduleResource(ctx, resources, client, schedule)`. If the resource upsert fails, the schedule was already saved — log a warning and continue (matching the reconciler's skip-on-error behavior). The reconciler will catch it on next boot.

**Design note — Delete:**
Before `client.TaskSchedule.DeleteOneID(id).Exec(ctx)`, read the schedule's `resource_id`. If non-empty, call `ArchiveScheduleResource(ctx, resources, resourceID)` after the delete succeeds. If archival fails, log and continue — the resource row persists with its last state, which is acceptable.

**Design note — SetEnabled:**
After updating `enabled` on the schedule, call `resources.SetState(ctx, s.ResourceID, state)` where state is `"enabled"` or `"disabled"`. If `resource_id` is empty (schedule predates reconciliation and server hasn't rebooted), skip — the reconciler will handle it.

**Design note — Update (name change):**
When `name` changes, call `resources.Upsert` to refresh the name on the resource row. The upsert's on-conflict clause updates `name` (see `resource_repo.go:85-89`).

**Steps:**
- [ ] Modify `NewTaskScheduleRepo` signature to accept `ResourceRepo`
- [ ] Update `di.go`: pass `ResourceRepo` to `NewTaskScheduleRepo`
- [ ] Add `ReconcileScheduleResources` call to `di.go` after `ReconcilePluginResources`
- [ ] Write tests in `task_schedule_repo_test.go`:
  - Test: creating a schedule also creates a resource row
  - Test: deleting a schedule archives the resource row
  - Test: `SetEnabled(false)` sets resource state to `"disabled"`
  - Test: `SetEnabled(true)` sets resource state to `"enabled"`
  - Test: updating schedule name refreshes resource name
- [ ] Run tests to see them fail
- [ ] Implement Create/Delete/SetEnabled/Update changes in `task_schedule_repo.go`
- [ ] Run tests to see them pass
- [ ] Verify full build: `cd server && go build ./...`
- [ ] Verify vet: `cd server && go vet ./...`
- [ ] Fix any test call sites that break due to `NewTaskScheduleRepo` signature change
- [ ] Commit: `feat: sync schedule CRUD operations to resource registry`

---

### Task 4: Handler cleanup — remove projection, unify read path

**Files:**
- Modify: `server/internal/api/resources/handler.go` (remove `routineViews()`, remove `if kind == ResourceKindRoutine` branch)
- Delete: `server/internal/api/resources/routine_projection_test.go`
- Modify: handler constructor if `schedules` field becomes unused

**Interfaces:**
- Consumes: `repo.ResourceRepo.ListMerged` (existing) — routines now flow through this path
- Removes: `routineViews()` function, `schedules` field on handler struct (if no other method uses it)

**Design note — handler struct cleanup:**
The `schedules` field (`h.schedules`) was injected solely for the projection. If no other handler method references it after removing `routineViews()`, remove the field from the struct and its injection in `di.go`. Grep for `h.schedules` to verify.

**Design note — why this is safe:**
After Task 3, every schedule has a resource row (boot reconciliation guarantees it). The `ListMerged` path returns these rows. The projection would return duplicates — its removal is not optional.

**Design note — API contract:**
The response shape changes minimally:
- `id` was the schedule UUID; now it is the resource UUID. Consumers that used `id` as a schedule identifier must use `origin_ref` instead (which holds the schedule UUID, per the reconciler's `OriginRef: s.ID`).
- `slug` was `slug_prefix`; now it is the schedule UUID. This is a breaking change for any consumer that parsed `slug` as a human-readable prefix. Since no row existed before, no consumer could have persisted these values.
- `version` was absent (empty string in the projection); now it is `""` from the resource row. No change.

**Steps:**
- [ ] Grep `h.schedules` in handler.go to identify all uses
- [ ] Remove `routineViews()` function
- [ ] Remove the `if kind == repo.ResourceKindRoutine` special-case branch in the list handler
- [ ] If `h.schedules` is unused, remove the field from the handler struct and its injection
- [ ] Delete `routine_projection_test.go`
- [ ] Write a replacement test: `GET /api/resources?kind=routine` returns rows from the resources table with correct kind, slug (schedule UUID), and state
- [ ] Run tests: `cd server && go test ./internal/api/resources/ -count=1`
- [ ] Verify full build: `cd server && go build ./...`
- [ ] Verify vet: `cd server && go vet ./...`
- [ ] Commit: `refactor: remove routine projection, unify resource read path`
