# Pipeline Transaction Correctness — Design Spec

> Date: 2026-07-12 · Status: Approved · Branch: `docs/audit-spec-roadmap` (off `upcoming`)
> Audit items CQ-03 (data-integrity) + CQ-06 (fail-loud spawn) from `outputs/Findings-full-project-2026-07-12.md`.
> Ships FIRST — independent of and prerequisite to the orchestrator decomposition (`2026-07-12-orchestrator-decomposition-design.md`).

## Why

**CQ-03 is a data-integrity bug, not a cosmetic one.** In `server/internal/pipeline/transitions.go`, `applyTransition` (:15) wraps all stage-transition writes in a single SQLite transaction: it calls `applyTransitionWrites` at :34, then `tx.Commit()` at :38. But the closures collected into `postCommit` inside `applyTransitionWrites` are executed at :261-263 — **inside that function, before it returns**, therefore **before `tx.Commit()` at :38**. The name `postCommit` is a lie: these side effects run pre-commit. Two concrete failure modes:

1. **Side effects fire for rolled-back transitions.** The `DoneTransition` postCommit (:114-117) runs `o.handleDependentTasks(...)` (cascades dependent tasks forward) and `o.taskLocks.Delete(task.ID)`. The `FailTransition` / iteration-limit postCommits (:133-136, :170-173) run `o.opts.OnStageFailed(...)` (notifications). If `tx.Commit()` at :38 then fails, the transaction rolls back — but dependents were already cascaded, the task lock already released, and failure notifications already sent, for a transition that **did not happen**. State diverges permanently.
2. **Pre-commit writer contention.** `handleDependentTasks` (orchestrator.go:733) issues its own DB writes while the transition's `tx` is still open. SQLite is single-writer; a nested/second writer against the same DB while the first tx is uncommitted risks `SQLITE_BUSY` / lock contention on exactly the pipeline's hottest path.

**CQ-06** is an adjacent silent-failure bug in the same package: `SpawnStageAgent` (spawner.go:496) calls `writeSettingsFile` at :502 and, on error, only logs `slog.Warn` (:504) then **continues the spawn**. The agent starts with no pre-approved allow-list. Under restrictive autonomy that is not a benign fallback — the agent has no permissions, blocks on the first tool call, and stalls silently (matches the "awaiting_user reaper: agent exited while permissions pending" class of stalls). It must fail loud.

## Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | Move `postCommit` execution out of `applyTransitionWrites`; return `[]func()` up to `applyTransition` and run them **only after `tx.Commit()` succeeds**. | Restores the invariant the name already promises. Side effects never fire for a rolled-back transition. |
| D2 | In the no-tx branch (`o.opts.Client == nil`, :50-62), run the returned closures **immediately** after `applyTransitionWrites` returns (there is no commit to gate on). | Preserves current test-path behavior; mocked-repo tests have no real transaction to fail. |
| D3 | Keep `afterCommitTerminalCleanup` (:43) and the `OnTaskChanged` broadcast (:45) ordering exactly as-is; the new closures run in the **same post-commit window**, before the broadcast. | Cascade/notify must be visible to any downstream read the broadcast triggers; ordering stays deterministic. |
| D4 | CQ-06: treat `writeSettingsFile` error as fatal to the spawn **unless autonomy is an allow-all level** (`spec_gated`, `full`). Return the error from `SpawnStageAgent`; the orchestrator surfaces it as a `FailTransition`. | Allow-all agents genuinely need no allow-list file, so a write failure there is harmless. Restrictive autonomy without the file = guaranteed silent stall → fail loud instead. |

## Scope

In: `applyTransition` / `applyTransitionWrites` signature + control-flow change (transitions.go); the CQ-06 fail-loud guard in `SpawnStageAgent` (spawner.go); a fault-injecting `Commit`-fails characterization test; a CQ-06 spawn-error test. No behavior change to any individual transition's writes or to the transition decision logic.

Out: the orchestrator decomposition (separate spec, ships after this); any change to `finalizeCompletedAsyncRuns`, budget blocks, or the config cache; any new transition type; retry/backoff semantics.

## Architecture (file-anchored)

### CQ-03 — `server/internal/pipeline/transitions.go`
- **`applyTransitionWrites` (:65)**: change return signature from `(*ent.StageRun, any, error)` to `(*ent.StageRun, any, []func(), error)`. Delete the execution loop at :260-263. Instead, `return result, enrichedPayload, postCommit, nil` at the end (:282). Each transition case keeps appending to `postCommit` exactly as today (:114-117 Done, :133-136 Fail, :170-173 iteration-limit) — only the *execution site* moves.
- **`applyTransition` (:15)**, tx branch: capture the closures —
  `result, enrichedPayload, postCommit, retErr = o.applyTransitionWrites(...)` at :34. After `tx.Commit()` succeeds at :38 and before `afterCommitTerminalCleanup` at :43, run `for _, fn := range postCommit { fn() }`. On the rollback path (`retErr != nil`, deferred at :21-25, or a `tx.Commit()` error at :38) the closures are **never invoked**.
- **`applyTransition` (:15)**, no-tx branch (:50-62): capture the closures the same way and run them immediately after `applyTransitionWrites` returns (there is no `tx.Commit()` to gate on), before `afterCommitTerminalCleanup` at :58.
- The in-tx `enrichedPayload` read (:268-271, uses the tx-bound `srRepo`/`permRepo`) is unaffected — it stays inside `applyTransitionWrites`, still reads uncommitted-but-in-tx state, still broadcast post-commit.

### CQ-06 — `server/internal/pipeline/spawner.go`
- `SpawnStageAgent` (:496), at the `writeSettingsFile` call (:502-505): when `err != nil`, branch on autonomy. Reuse the existing allow-all classifier that `writeSettingsFile`/`BuildDenyList` already rely on (spawner.go:84-95 documents the `spec_gated`/`full` allow-all levels — factor the predicate into a single `isAllowAllAutonomy(autonomy string) bool` if one does not already exist, SSOT). If **not** allow-all → `return SpawnResult{}, fmt.Errorf("writeSettingsFile: %w", err)`. If allow-all → keep the current `slog.Warn` + continue.

## Data flow

Unchanged shape, corrected ordering. Per transition: `finalizeCompletedAsyncRuns` / user action → `applyTransition` → `Tx.Begin` → `applyTransitionWrites` (stage-run + task + audit writes, collect closures) → `tx.Commit()` → **[new gate] run closures (dependent-task cascade, taskLocks.Delete, OnStageFailed)** → `afterCommitTerminalCleanup` (worktree removal) → `OnTaskChanged` broadcast. If commit fails, flow stops at the gate with zero side effects.

CQ-06 spawn: `SpawnStageAgent` → `writeSettingsFile` → error under restrictive autonomy → return error → orchestrator applies `FailTransition{Reason: "spawn failed: allow-list not written"}` instead of launching a permissionless agent.

## Error handling

- `tx.Commit()` failure (:38): existing `fmt.Errorf("applyTransition.commit: %w", ...)` return stands; closures do not run — this is the whole point.
- A closure itself panicking must not corrupt the post-commit window. Closures today swallow their own errors (`handleDependentTasks` logs; `OnStageFailed` is a fire-and-forget callback). Keep that; do **not** wrap the commit in their failure. They run best-effort after the durable write.
- CQ-06: the returned spawn error propagates to `finalizeCompletedAsyncRuns` / the spawn caller, which already routes spawn failures through `FailTransition`. No new error type.

## Testing

Characterization + targeted regression — `go test ./internal/pipeline/...` (note: `go test ./...` regenerates `internal/db/ent/`; restore with `git checkout -- server/internal/db/ent/` after).

1. **CQ-03 commit-fails test (the load-bearing one).** Inject a `Client` whose `Tx(ctx)` returns a tx whose `Commit()` returns an error (fault-injecting wrapper around the ent client). Drive a `DoneTransition` with a dependent task wired up. Assert: (a) `applyTransition` returns the commit error; (b) the dependent task was **NOT** cascaded (`handleDependentTasks` side effect absent); (c) `taskLocks` still holds the lock; (d) `OnStageFailed` / `OnTaskChanged` did **not** fire. This test **fails on today's code** (closures run pre-commit) and passes after D1.
2. **CQ-03 happy-path ordering test.** Commit succeeds → assert closures ran, and ran after the write is durably committed (dependent cascade observed, `OnTaskChanged` fired exactly once, after the cascade).
3. **CQ-03 no-tx branch test.** With `Client == nil` (mocked-repo path), assert closures still run once (D2) — guards against regressing the test harness.
4. **CQ-06 fail-loud test.** Stub `writeSettingsFile` to error (unwritable `cwd`): restrictive autonomy → `SpawnStageAgent` returns error, no process spawned; allow-all autonomy (`full`) → warns and continues (spawns). Table-driven over autonomy levels.

## Risks

- **Highest-change-coupling file in the repo.** Project memory records repeated multi-spawn / cascade / permission regressions landing in exactly this path. Keep the diff surgical: only the closure execution site moves; no transition's writes change. Review the full transitions.go switch to confirm every `postCommit = append(...)` site is covered by the moved execution (Done :114, Fail :133, iteration-limit :170 today — grep for `postCommit` before and after).
- **One lock surface.** The bug is itself SQLite single-writer contention; the fix must not reintroduce it — closures must run strictly after `tx.Commit()`, never inside the tx. Do not let any collaborator open a nested tx.
- **Broadcast ordering.** `OnTaskChanged` fires post-commit today and must stay post-commit *and* after the cascade closures (D3) so a client re-read sees cascaded state. Assert this in test 2.
- **CQ-06 blast radius.** Turning a warn into a hard failure changes spawn behavior for restrictive-autonomy tasks whose worktree is briefly unwritable. Acceptable — a loud fail is strictly better than a silent stall — but flag in the PR body so it is not mistaken for a new spawn regression.
