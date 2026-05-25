# TODO: db/repo → db/rawrepo layer violation

Branch: perf/db-repo (PR #88)
User pick: 6.A (DI inject StageRunBulkRepo as separate interface) — 2026-05-25
Status: Deferred to follow-up PR after 2 failed implementation attempts.

## Plan

1. Create `server/internal/services/stage_run_bulk.go` with interface `StageRunBulkRepo` exposing the methods currently delegated to rawrepo (likely `ListStageRunsByTaskIDs`).
2. Remove `rawrepo` import from `server/internal/db/repo/stage_run_repo.go`. Remove the field/method that delegates to rawrepo from `StageRunRepo`.
3. In `server/cmd/serve/di.go` (or `di_tasks.go`), wire `rawrepo.NewStageRunBulkRepo(client)` to the new interface. Inject into `tasks.Handler` as a separate field.
4. Update `server/internal/api/tasks/export_routes.go` and `server/internal/api/tasks/enrich.go` (and any other consumer found via `grep -rn rawrepo server/`) to take the bulk repo from `h.bulkRepo` instead of through `srRepo`.
5. `grep -rn "rawrepo" server/internal/db/repo/` must return zero.
6. `cd server && go build ./... && go test ./internal/...`.

## Why deferred

- DI signature change ripples through `tasks/{handler.go, export_routes.go, enrich.go, ...}`.
- Two agent attempts crashed on cross-file scope rebuilding `err` variable scope in `enrich.go` after the StageRunRepo method removal.
- Should be its own PR with a clean diff so review can verify layer boundary is truly closed.
