# ADR-0001: SQLite for Task Pipeline Persistence

**Status:** Accepted
**Date:** 2026-04-12

## Context

The Claude Agent Overview dashboard was deliberately designed without a
database. Agent monitoring derives all state from Claude Code's own
filesystem: JSONL session logs under `~/.claude/projects/`, process tables
via `ps`/`lsof`, and discovery files under `~/.claude/dashboard-channel/`.
Claude Code is the source of truth; the dashboard is a read-only observer
with in-memory caches.

The Task Pipeline feature (see `~/.claude/plans/distributed-hatching-waffle.md`)
breaks this model. Pipeline tasks are first-class domain objects with no
representation in Claude Code: they carry titles, slugs, worktree paths,
max-iteration budgets, approval gates, stage-run history, user-granted
permissions, and an audit log. None of this can be reconstructed from the
filesystem after a restart.

## Decision

Introduce `better-sqlite3` as a single-file embedded database at
`~/.claude/dashboard-tasks.db`, scoped strictly to the Task Pipeline
subsystem.

**Scope boundary (strict):**

SQLite **owns**:

- `tasks`
- `stage_runs`
- `task_permissions`
- `permission_requests`
- `audit_log`
- `notification_preferences`
- `notification_config`
- `pipeline_config`
- `schema_version` (migration tracking)

SQLite **does NOT own**: agent status, process metadata, session JSONL
content, cost trends, channel discovery, system resource readings. These
remain filesystem-derived as before.

Schema migrations are idempotent (`CREATE TABLE IF NOT EXISTS`) and run
at first `getDb()` call — no separate migration command.

## Consequences

**Positive:**

- Pipeline tasks survive dashboard restarts
- ACID transactions for multi-step stage transitions
  (see `PipelineOrchestrator.applyTransition` — wraps every branch in
  `db.transaction()`)
- Single-file DB, zero operational overhead
- Synchronous `better-sqlite3` API keeps repo code simple and testable
- Query support for board enrichment (`needsUser` computed via join)

**Negative / Trade-offs:**

- Adds a native compile step to `pnpm install`
  (mitigated by `onlyBuiltDependencies` in `pnpm-workspace.yaml`)
- Two persistence models (filesystem + SQLite) to reason about
- Risk of scope creep: contributors may be tempted to store
  agent-monitoring data in SQLite for convenience, or pipeline data on
  the filesystem. The scope boundary above is non-negotiable.

**Follow-ups:**

- This decision is documented in `CLAUDE.md` → Architecture section
- Schema version tracking table exists (`schema_version`) for future migrations
- Backup strategy deliberately not defined: single-user, single-machine,
  user can copy the file manually

## Alternatives Considered

- **JSON file per task** under `~/.claude/dashboard-tasks/` — rejected
  because 100+ tasks become unhandy, no transactions, no efficient queries,
  no referential integrity.
- **Extend the existing discovery-file pattern** — rejected: no query
  support, no iteration safety, mixes task state with agent state.
- **Embedded key-value store** (LMDB, leveldb) — rejected: more complex
  native dependency than SQLite, worse ergonomics for relational data.
