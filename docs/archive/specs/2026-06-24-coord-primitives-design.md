# Coordination Primitives (Plane B / L0) — Design

> Date: 2026-06-24
> Status: Approved (design); pending implementation plan
> Source: Plane B (native dashboard multi-agent coordination), Layer 0. Foundational, fully independent of L1 (agent-to-agent channel) and L2 (PM-agent). Maps to B4/B5 (Soloterm-style coordination) from the competitor gap analysis.

## 1. Goal & Non-Goals

**Goal.** Give dashboard agents two shared-state coordination primitives — **scratchpads** (shared key/value handoff + contract state) and **lease-based locks** (mutual exclusion for non-file shared resources) — exposed via a new `agent:coord` MCP scope. These let a future PM-agent (L2) and its subtask team share contracts, hand off results, and avoid collisions on shared resources.

**Non-Goals.**
- No timers (deferred; they would require an orchestrator-tick sweep — locks deliberately use lazy expiry to avoid that).
- Not the agent-to-agent channel (L1) nor the PM-agent role (L2) — separate specs.
- No tree-scoped access control; the local single-user dashboard grants any `agent:coord` key access to any namespace (documented; revisit if multi-user lands).
- No write UI — agents write via MCP only; the UI is read-only visibility.

## 2. Data Model (two additive ent tables)

Both tables are new and additive (no column changes to existing tables → no phantom-column rebuild risk; still add an old-DB regression test per the `ent_phantom_column_rebuild` lesson).

**`scratchpad`** (`server/internal/db/ent/schema/scratchpad.go`):
- `id` (string, immutable), `namespace` (string), `key` (string), `value` (text, JSON-or-plain), `updated_at` (time), `updated_by_task_id` (string).
- Unique index `(namespace, key)`.

**`coord_lock`** (`server/internal/db/ent/schema/coord_lock.go`):
- `id` (string, immutable), `namespace` (string), `key` (string), `owner_task_id` (string), `acquired_at` (time), `expires_at` (time).
- Unique index `(namespace, key)`.

**Namespace** is a free string. Convention: the PM/root task ID, which the PM passes to its subtasks at creation. No parent-chain walking.

## 3. Lock Semantics (lease-based, lazy expiry)

- `acquire(namespace, key, ttlSeconds, ownerTaskId)` succeeds iff: no row exists, OR the existing row is expired (`expires_at < now`), OR it is already owned by `ownerTaskId` (re-entrant — refreshes the lease). Otherwise returns `acquired:false` with the current owner + `expiresAt`.
- TTL prevents deadlock if an owner dies. **Expiry is evaluated lazily at acquire time** — no background sweep (this is why timers are out of scope for L0).
- Acquire is a single atomic upsert: `INSERT … ON CONFLICT(namespace,key) DO UPDATE SET owner_task_id=excluded…, acquired_at=…, expires_at=… WHERE coord_lock.expires_at < now OR coord_lock.owner_task_id = excluded.owner_task_id`, then read back to confirm ownership. The conditional `WHERE` on the upsert makes two concurrent acquires resolve to exactly one winner.
- `release(namespace, key, ownerTaskId)` deletes the row only if `owner_task_id` matches; a non-owner release is a rejected no-op.

`ON CONFLICT` requires `gen.FeatureUpsert` at ent codegen (lesson `ent_codegen_upsert_feature`) — without it the OnConflict builders are stripped and the upsert breaks.

## 4. Scratchpad Semantics

- `write(namespace, key, value, updatedByTaskId)` — upsert on `(namespace, key)`; sets `value`, `updated_at`, `updated_by_task_id`.
- `read(namespace, key)` — returns the row or null.
- `list(namespace)` — all keys+values in the namespace.

## 5. MCP Surface

New file `server/internal/mcp/tools/coord.go`, all under scope `agent:coord`:
- `write_scratchpad(namespace, key, value)`
- `read_scratchpad(namespace, key)`
- `list_scratchpad(namespace)`
- `acquire_lock(namespace, key, ttlSeconds)` → `{acquired, owner, expiresAt}`
- `release_lock(namespace, key)`

Registration wired in `server/cmd/serve/di_mcp.go`. All five added to `ToolScopeMap` in `server/internal/mcp/auth.go` (missing entry panics at registration). `scopeImplies` updated so `pipeline:control` implies `agent:coord` and `keys:manage` implies everything.

**`ownerTaskId` resolution:** taken from the MCP session auth context (the `owner=KeyID` / `ContextWithAuth` seam). When the key is not task-bound, the tool accepts an explicit `ownerTaskId` argument. Release and re-entrancy check against this owner.

**Access control:** any authenticated `agent:coord` key may read/write any namespace. The dashboard is loopback-only and Origin-guarded; this is acceptable for single-user. Documented as an explicit non-goal to restrict.

## 6. REST + UI (read-only visibility)

- `GET /api/coord/{namespace}/scratchpads` → list of scratchpad entries.
- `GET /api/coord/{namespace}/locks` → active (non-expired) locks.
- A minimal **Coordination tab** in the task modal (`src/components/task/`) renders scratchpads + active locks for the task's namespace (default namespace = the task's own ID, or its `parentTaskId` when set). Read-only — writes happen only via MCP. Kanban/cards untouched.

## 7. Testing (TDD)

**Go (no agent spawns needed — pure repo/MCP):**
- Scratchpad: write→read roundtrip; second write overwrites (upsert); list returns all keys in a namespace; read of missing key → null.
- Lock: acquire when free → true; acquire when held (unexpired, other owner) → false with current owner; acquire after expiry → true; re-entrant acquire (same owner) → true and refreshes `expires_at`; release by non-owner → rejected; release by owner → row gone.
- Concurrency: two near-simultaneous `acquire` calls for the same `(namespace,key)` → exactly one returns `acquired:true` (atomic upsert race test).
- Scope: `ToolScopeMap` maps all five tools to `agent:coord`; `scopeImplies(pipeline:control, agent:coord)` is true.
- ent: codegen retains `gen.FeatureUpsert`; new tables are additive — seed an old-shape DB and assert `db.Open` migrates without a rebuild crash.

**Vue (Vitest):** Coordination tab renders scratchpads + active locks; is read-only (no write controls).

## 8. Docs

README (Build & control — coordination primitives), CHANGELOG (`[Unreleased]` → Added), and `.agent-context/mcp.md` (new `agent:coord` scope + the five tools).

## 9. Risks & Mitigations

| Risk | Mitigation |
|---|---|
| Lock race (two acquirers) | Atomic conditional `ON CONFLICT` upsert + read-back; covered by the concurrency test |
| ent codegen strips OnConflict | Keep `gen.FeatureUpsert`; covered by build + upsert test |
| Missing ToolScopeMap entry → panic | All five tools registered in `ToolScopeMap` + a scope test |
| New tables trigger DB rebuild on existing installs | Additive-only tables; old-DB-shape regression test |
| Unbounded namespace access misused | Acceptable for loopback single-user; documented; tree-scoping deferred to a multi-user follow-up |
| Stale expired locks accumulate | Lazy expiry frees them on next acquire; an optional cleanup is a later optimization, not L0 |

## 10. Relationship to Plane B

L0 is the substrate. **L1** (agent-to-agent channel) is independent and can be built in parallel. **L2** (PM-agent) consumes L0 (scratchpads for contracts/handoff, locks for shared resources) and L1 (peer messaging), and additionally requires completing the dependency-cascade stub (`orchestrator.go` `handleDependentTasks`). L0 ships and is useful on its own — any agent with the `agent:coord` scope can coordinate via shared state immediately.
