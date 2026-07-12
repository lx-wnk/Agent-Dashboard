# ADR-0009: Extract `internal/proc` Process-Liveness Leaf

**Status:** Proposed
**Date:** 2026-07-12

## Context

Process-liveness detection lives in `server/internal/pipeline/session_manager.go`:

```go
func IsPidAlive(pid int) bool { ... }   // + unexported isPidZombie(pid int) bool
```

These two functions depend only on the standard library (`os`, `os/exec`,
`syscall`, `strings`) and reference nothing in the pipeline state machine
(orchestrator, stage handlers, completion detector, sweeps). They are a
self-contained leaf that happens to be parked in a high-level package.

Because they live in `pipeline/`, three infra/edge consumers are forced to
import the entire orchestration core just to reach the liveness probe:

- `db/rawrepo/stage_run_bulk_repo.go:11,53` imports `pipeline` **solely** for
  `pipeline.IsPidAlive` (the default probe in `NewStageRunBulkRepo`).
- `api/tasks/enrich.go` and `api/tasks/analyze_routes.go` do the same.

`db/rawrepo` is a low-level persistence package and `api/tasks` is an edge
package; both importing `internal/pipeline` inverts the intended layer
direction and seeds a latent `rawrepo ↔ pipeline` import cycle (finding
ARCH-P1-1). This is the same misplacement pattern already resolved for the
adapter code (ADR-0005) and worktree helpers (ADR-0006).

## Decision

Move `IsPidAlive` and `isPidZombie` verbatim into a new stdlib-only leaf
package `server/internal/proc/` (`package proc`). The leaf imports only the
standard library; its public surface is `func IsPidAlive(pid int) bool`.

Repoint every caller — the three infra/edge consumers plus the intra-`pipeline`
callers (`completion_detector.go`, `orchestrator.go`, `spawner.go`,
`progress_guards.go`, `sweeps.go`, and the two test files) — from
`pipeline.IsPidAlive` to `proc.IsPidAlive`. In `stage_run_bulk_repo.go` the
`pipeline` import is removed outright.

No logic, signature, or platform-behaviour change: `isPidZombie` keeps its
existing `ps`-based zombie check byte-for-byte. This is a pure move + re-import.

Enforce the restored boundary with a `golangci-lint` `depguard` rule denying
`internal/pipeline` imports from `internal/db/rawrepo/**` and `internal/api/**`,
making the invariant a compile-time CI check rather than a convention.

## Consequences

- **Cycle risk removed.** `db/rawrepo` and `api/tasks` no longer import
  `internal/pipeline`; the latent `rawrepo ↔ pipeline` cycle is gone.
- **Layer direction restored.** Every liveness caller now reaches *down* to a
  stdlib-only leaf instead of *up* into the orchestration core.
- **CI-enforced.** The `depguard` rule fails the build if the upward import is
  reintroduced — the same follow-up that ADR-0004 deferred, now realized.
- **Wide but mechanical.** 11 files change (incl. 2 test files); no behaviour
  change. `go test ./...` regenerates the ent tree, so restore
  `server/internal/db/ent/` after running the suite.
- **Import graph.** `.agent-context/architecture.md` and `task-pipeline.md` gain
  a `proc` leaf node importable by any layer.

## Alternatives Considered

1. **Whitelist `pipeline.IsPidAlive` in the runtime-import list.** Legalises the
   edge on paper but keeps infra/edge builds pulling in the state machine.
   Rejected — treats the symptom, not the misplacement (same reasoning as
   ADR-0005).
2. **Duplicate the probe in `db/rawrepo`.** Violates SSOT; two liveness
   implementations would drift on platform edge cases. Rejected.
3. **Put it in `services/`.** `services/` may import `pipeline` types, which
   would blur the leaf guarantee. A dedicated stdlib-only leaf keeps the
   dependency floor at the standard library.
