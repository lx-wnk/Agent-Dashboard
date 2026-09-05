# Routine Registry Migration — Design Spec

> Bring `task_schedule` (routines) under the resource registry so that routines are first-class ARMS resources with persistent identity, not read-time projections.

**Parent:** `2026-08-27-agenticos-overview-design.md` (unit K1 — Resource Registry)

---

## 1. Status Quo

### What works

- Plugins are already migrated. `server/internal/db/repo/plugin_resource.go` runs `ReconcilePluginResources` at startup (`di.go:333`), upserting a `resources` row per plugin and backlinking via `plugin.resource_id`. State is derived from `Active`/`InstalledAt`.

### What does not

- **Routines have no `resources` row.** `server/internal/api/resources/handler.go:120-152` projects a `resourceView` from `task_schedule` at read time. The projection writes nothing. A routine therefore has no `resources.id`.
- **K4's materializer reads `Resources.ListForKind`.** Since no rows exist for `ResourceKindRoutine`, the materializer can never see a routine.
- **Capability grants use a separate context chain.** `server/internal/memory/authorize.go` builds `RoutineContext(scheduleID)` with `context_kind = "routine"` and `context_ref = <schedule_id>` — a caller context, not a resource reference — because there is no resource row to point at.

### Consequence

This is **one migration**, not two. The projection and the absent rows are two symptoms of the same gap: routines were never persisted into the registry. Closing that gap is a single change that touches schema, boot reconciliation, CRUD lifecycle, and the handler's read path.

---

## 2. What Changes

### 2.1 Schema: `resource_id` on `task_schedule`

Add an optional `resource_id` string field (default `""`) to the `task_schedule` ent schema, mirroring `plugin.resource_id`. This backlink lets a schedule locate its registry identity without a query.

### 2.2 Boot reconciliation: `ReconcileScheduleResources`

A new function in `server/internal/db/repo/schedule_resource.go`, identical in shape to `ReconcilePluginResources`:

1. Query all `task_schedule` rows where `resource_id == ""`.
2. For each, upsert a `resources` row with `kind = "routine"`.
3. Backlink the schedule with the returned `resources.id`.

Called from `di.go` at startup, after `ReconcilePluginResources`. Idempotent — settles to 0 work after the first run.

### 2.3 CRUD lifecycle sync

Every schedule mutation that changes registry-relevant state must propagate:

| Schedule operation | Resource effect |
|---|---|
| **Create** | `resources.Upsert` + set `resource_id` on the new schedule |
| **Update name** | `resources.Upsert` (name refresh, state unchanged) |
| **SetEnabled(true)** | `resources.SetState(id, "enabled")` |
| **SetEnabled(false)** | `resources.SetState(id, "disabled")` |
| **Delete** | `resources.SetState(id, "archived")` — see §4.4 |

### 2.4 Handler: remove projection, unify read path

Delete `routineViews()` and the `if kind == repo.ResourceKindRoutine` special case in `handler.go`. Routines flow through `ListMerged` like every other resource kind.

---

## 3. What This Unlocks

- **K4 materializer visibility.** `ListForKind("routine")` returns real rows. Any future materializer or aggregation that reads the registry sees routines.
- **Uniform resource API.** The `/api/resources` endpoint returns routines from the same table as plugins, skills, and memory spaces — no special-case projection.
- **Foundation for grant migration.** Grants currently key on `context_kind = "routine"` with the schedule ID as `context_ref`. Once routines have a stable `resources.id`, a future migration can unify grant context references to resource IDs across all kinds. This spec does not do that — see §5.

---

## 4. Design Decisions

### 4.1 Backfill strategy: startup reconciliation

**Question:** Existing `task_schedule` rows have no `resources` row. Backfill on startup, lazily on first read, or on next write?

**Trade-offs:**

| Strategy | Pro | Con |
|---|---|---|
| **Startup** | Every routine visible immediately after boot; no code path returns a routine without a resource row; mirrors the proven plugin pattern | Adds one query + N upserts on first boot; negligible for realistic schedule counts (<100) |
| **Lazy on read** | Zero boot cost | A routine without a resource row is invisible to `ListForKind` until something reads it through the projection path — the exact inconsistency this migration eliminates |
| **On next write** | Zero boot cost, resource created at a natural mutation point | Existing routines that are never edited stay invisible indefinitely; the gap persists for the most stable (and therefore most important) routines |

**Decision:** Startup reconciliation. The boot cost is trivial, and it is the only strategy that guarantees all routines are visible from the first `ListForKind` call. It mirrors `ReconcilePluginResources` exactly.

### 4.2 Resource slug: schedule ID (UUID)

**Question:** What is the routine's resource slug? `slug_prefix` is not unique by construction — can it collide?

**Analysis:**

- `slug_prefix` has **no unique index** on `task_schedule` (only `enabled` and `next_run_at` are indexed).
- `slug_prefix` has **no format validation** beyond `MaxLen(100)`. It can contain spaces, special characters — anything the user types.
- The `resources` table enforces a unique index on `(kind, scope_kind, scope_ref, slug)`. Two routines with the same `slug_prefix` in global scope would collide on upsert.
- `slug_prefix` is a task-name template (appended with a timestamp suffix to derive each fired task's slug), not an identity.

**Decision:** Use the schedule's UUID (`task_schedule.id`) as the resource slug. Justification:

1. Guaranteed unique — UUIDs cannot collide.
2. No validation gap — UUIDs are safe slug characters.
3. The resource `Name` field carries the human-readable label (`task_schedule.name`).
4. The current projection's `Slug: s.SlugPrefix` had no persistence behind it — no consumer could have built durable references on it since no row existed.
5. Mirrors the plugin pattern conceptually: the plugin's manifest `id` is its stable identity. The schedule's UUID is its stable identity.

**`origin_ref`** is also set to the schedule ID, recording the source record for traceability (same as plugins: `OriginRef: p.ID`).

### 4.3 Projection removal: delete, not fallback

**Question:** What happens to the projection in `api/resources/handler.go` once rows are real — deleted, or kept as a fallback?

**Decision:** Delete the projection (`routineViews()` and its special-case `if` branch). Rationale:

- A projection that survives alongside real rows causes the same routine to appear twice — once from the projection and once from `ListMerged`. This is worse than no migration at all.
- The reconciler is idempotent and runs at boot. After startup, every schedule has a resource row. There is no window where the projection is needed as a fallback.
- Keeping dead code "just in case" violates YAGNI and creates a maintenance trap: a future developer must reason about two paths that should never both execute.

The projection test file (`routine_projection_test.go`) is also deleted — its assertions validate behavior that no longer exists.

### 4.4 Delete behavior: archived state, not row deletion

**Question:** Deleting a `task_schedule` — does the resource row go, or become `state: archived`?

**Trade-offs:**

| Strategy | Pro | Con |
|---|---|---|
| **Hard delete resource** | Clean; no orphan rows | Grants with `context_ref = <schedule_id>` lose their anchor; audit trail lost |
| **Archived state** | Grants remain valid (they reference a resource that exists, just archived); audit trail preserved; reversible | Archived rows accumulate; requires eventual cleanup policy |

**Decision:** Set resource state to `"archived"`. The schedule row itself is hard-deleted (existing behavior in `schedule_repo.Delete`), but the resource row persists with `state: archived`. Grants that reference the schedule continue to resolve — an archived resource is a valid, if inactive, grant context.

Cleanup of archived resources is a follow-up concern, not a blocker. The plugin reconciler has the same implicit contract: if a plugin is uninstalled and its row deleted, the resource row would need the same treatment.

---

## 5. What This Deliberately Does Not Do

### 5.1 Grant context migration

Grants currently use `context_kind = "routine"` with `context_ref = <schedule_id>`. Migrating them to use `context_ref = <resource_id>` is a cross-cutting change that affects the authorization chain in `authorize.go`, the grant creation flow, and the UI's grant-binding logic — for every resource kind, not just routines.

This spec does not migrate grants. The `RoutineContext()` chain in `authorize.go` continues to work with schedule IDs. A future spec can unify grant context references to resource IDs across all kinds once every kind has a resource row.

### 5.2 Slug-prefix validation

`slug_prefix` has no format validation today. Adding a regex or uniqueness constraint is a separate concern that affects the schedule creation UI and API, not the registry migration. The decision to use the schedule UUID as the resource slug sidesteps this gap entirely.

### 5.3 Scope beyond global

All routines are created with `Scope: GlobalScope()`. Per-project routines would require a `project_id`-to-scope mapping. This spec does not introduce that — routines are global today and remain global.

### 5.4 Materializer integration

K4's materializer will see routine resources via `ListForKind` after this migration. Whether the materializer should do anything with routines (it currently materializes skills only) is a separate design question.

---

## 6. Verification

After migration, these invariants hold:

1. `SELECT count(*) FROM task_schedules WHERE resource_id = ''` returns 0 after boot.
2. `SELECT count(*) FROM resources WHERE kind = 'routine'` equals `SELECT count(*) FROM task_schedules`.
3. `GET /api/resources?kind=routine` returns rows from the `resources` table, not projections.
4. Creating a new schedule produces a `resources` row in the same transaction.
5. Deleting a schedule sets the resource row to `state: archived`.
6. `SetEnabled(false)` on a schedule sets the resource to `state: disabled`.
7. No routine appears twice in the resource list.
