# Pipeline Orchestrator Decomposition — Design Spec

> Date: 2026-07-12 · Status: Approved · Branch: `docs/audit-spec-roadmap` (off `upcoming`)
> Audit items ARCH-P2-1 + CQ-04 + CQ-05 from `outputs/Findings-full-project-2026-07-12.md`.
> Ships AFTER `2026-07-12-pipeline-tx-correctness-design.md` — correctness fixes land first on the current structure, then this refactor runs against a known-good, characterized baseline.

## Why

`server/internal/pipeline/orchestrator.go` is 1021 LOC / 38 methods and owns ≥5 unrelated responsibilities on one type (`PipelineOrchestrator`): the driver loop (`Run` :231, `tick` :305), slot scheduling (`hasFreeRunnerSlot` :343, `ensureStageRun` :355), stage-run persistence (`createNextPendingRun` :379, the finalize sweep :448), a config cache (`getCachedConfigNumber` :168, `getCachedConfigString` :180, `InvalidateConfigCache` :157), model resolution (`EffectiveStageModel` :195, `EffectiveStageModelForProject` :202, `stageModelDefault` :209), and session/token side-effects, worktree cleanup, dependency cascade, user transitions, and sweeps. This is the repo's highest-change-coupling file and its most frequent regression site (project memory: repeated multi-spawn / cascade / permission bugs).

Two of those responsibilities are also locally rotten:
- **CQ-04** — `finalizeCompletedAsyncRuns` (:448) is a ~237-line method mixing budget/timeout enforcement for *live* runs with failure-classification for *exited* runs.
- **CQ-05** — the cost-budget block (:510-530) and token-budget block (:532-552) are near-identical copies differing only in the budget field, the sum call, and the label — a copy-paste SSOT violation on the enforcement hot path.

The `OrchestratorOptions` callback seam already exists (`o.opts.*` throughout), so collaborators can be injected without new DI plumbing.

## Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | **Option C — incremental collaborator extraction, leaf-first**, one PR per collaborator, each behavior-preserving. Reject big-bang split (blast radius) and seam-only (no value). | This is the highest-coupling file in the repo; small reversible steps each re-run the characterization suite. |
| D2 | Extraction order by ascending coupling: **ConfigCache → ModelResolver → StageRunService → Scheduler**, leaving `PipelineOrchestrator` as a transition coordinator. | ConfigCache is pure and zero-coupling (safest first); Scheduler touches the live driver loop (riskiest last). |
| D3 | Land **CQ-05 (`enforceBudget`) then CQ-04 (split `finalizeCompletedAsyncRuns`)** as the *first cut*, before any collaborator extraction. | They shrink the worst method and de-duplicate the hot path with minimal structural risk, producing the seam `StageRunService` later absorbs. |
| D4 | Write **characterization (golden) tests over every `finalizeCompletedAsyncRuns` branch and a transition table BEFORE any refactor commit.** | Behavior-preservation is only verifiable against a locked baseline; the CQ-03 fix must already be in place so the baseline is correct, not bug-compatible. |
| D5 | Collaborators are injected through the existing `OrchestratorOptions`, nil-safe, with the no-DB/test path preserved. No new DI framework, no new package-level singletons. | Keeps the test seam and the mocked-repo path working; matches how `OnStageFailed`/`BuildTaskPayload` are already wired. |

## Scope

In: extraction of `ConfigCache`, `ModelResolver`, `StageRunService`, `Scheduler` collaborators within `server/internal/pipeline/`; `enforceBudget` helper (CQ-05); splitting `finalizeCompletedAsyncRuns` into `enforceBudgetsAndTimeout` + `handleFailedResult` (CQ-04); a characterization test suite locking current behavior.

Out: the CQ-03/CQ-06 correctness fixes (prerequisite spec, already merged); any change to transition semantics, retry/backoff policy, budget values, or config keys; new endpoints or SSE events; splitting the pipeline into multiple Go packages (stays one package — this is intra-package decomposition).

## Architecture (file-anchored)

### Step 0 — Characterization tests (before touching orchestrator.go)
Lock the observable behavior of `finalizeCompletedAsyncRuns` (:448) and `applyTransition` per D4 (see Testing). No production change.

### Step 1 — CQ-05 `enforceBudget` (orchestrator.go)
Collapse the cost block (:510-530) and token block (:532-552) into one helper:
`enforceBudget(ctx, task, run, budgetCents/limit *int, sumFn func(ctx, taskID) (int64, error), label string) (killed bool)`. Both call sites pass their field, their sum call (`SumCompletedCostCents` :511 / `SumCompletedTokens` :533), and a label ("cost"/"token"). Returns whether it killed + failed the run so the caller can `continue`. Stage-timeout enforcement (:554-570) stays a sibling call — same shape, distinct trigger.

### Step 2 — CQ-04 split `finalizeCompletedAsyncRuns` (orchestrator.go:448)
- `enforceBudgetsAndTimeout(ctx, task, run) (handled bool)` — the `still_running` branch (:498-571): the attach-session goroutine (:501-508), the two `enforceBudget` calls (Step 1), and timeout (:554-570). Returns `handled=true` when it killed/continued.
- `handleFailedResult(ctx, task, fresh, result) ` — the four-case failure ladder (:592-683): rate-limited requeue/exhaust (:596-626), infra requeue/exhaust (:628-658), retryable iterate-vs-wait_user (:660-679), hard fail (:681-683).
- `finalizeCompletedAsyncRuns` shrinks to the run-selection loop (:449-497) + `completed` dispatch (:584-590) delegating to the two new methods.

### Step 3 — `ConfigCache` collaborator (leaf, zero-coupling)
Extract `getCachedConfigNumber` (:168), `getCachedConfigString` (:180), `InvalidateConfigCache` (:157) and the backing cache field into a `configCache` type. Inject via `OrchestratorOptions` (nil-safe: nil → construct a default backed by `opts.SettingsRepo`). Orchestrator calls become `o.config.Number(...)` / `.String(...)`.

### Step 4 — `ModelResolver` collaborator
Extract `EffectiveStageModel` (:195), `EffectiveStageModelForProject` (:202), `stageModelDefault` (:209). Depends only on `ConfigCache` (Step 3) — inject that in. Pure resolution logic, no DB writes.

### Step 5 — `StageRunService` collaborator
Absorb stage-run persistence + finalize: `ensureStageRun` (:355), `createNextPendingRun` (:379), `resolveResumeSessionID` (:391), `getPreviousStageOutput` (:412), `getPriorIterationOutput` (:427), and the CQ-04-split finalize methods (Steps 1-2). This is where most churn concentrates — do it after the two smallest collaborators prove the injection pattern.

### Step 6 — `Scheduler` / picker collaborator (riskiest, last)
Extract `hasFreeRunnerSlot` (:343) and the slot-selection logic driving `tick` (:305). Touches the live driver loop; land only once Steps 3-5 are green. `PipelineOrchestrator` is now a transition coordinator: `Run`/`tick`/`ProgressTask` + `applyTransition`, delegating config, models, persistence, and scheduling to injected collaborators.

## Data flow

No runtime behavior change at any step. `Run` (:231) → `tick` (:305) → `finalizeCompletedAsyncRuns` still drives the same sweep; after decomposition it delegates: config lookups → `ConfigCache`, model resolution → `ModelResolver`, stage-run reads/writes + finalize → `StageRunService`, slot decisions → `Scheduler`. All transitions still funnel through the single `applyTransition` / one-tx path from the correctness spec — the decomposition must not add a second writer or a second tx.

## Error handling

- Every step is behavior-preserving: existing error handling (`slog.Error` + `continue` per finalize branch, `fmt.Errorf` wrapping in transitions) is moved verbatim, not rewritten.
- Nil-safe injection (D5): a nil collaborator in `OrchestratorOptions` constructs the current default, so the no-DB/test path and any existing caller keep working without change.
- CQ-05 `enforceBudget` must preserve the existing "sum failed → log warn, skip enforcement this tick" semantics (:512-515, :534-537) — a sum error does **not** kill the agent.

## Testing

`go test ./internal/pipeline/...` green after **every** step (restore ent tree after any `go test ./...`: `git checkout -- server/internal/db/ent/`). Pipeline tests already exist; extend them into a characterization baseline first.

1. **Golden `finalizeCompletedAsyncRuns` branch matrix (Step 0, before any refactor).** One case per branch: completed (→ `decideCompletedTransition`), rate-limited requeue, rate-limited exhausted (:613), infra requeue, infra exhausted (:645), retryable iter0 → iterate (:662), retryable iter≥1 → wait_user (:669), plain hard-fail (:681), cost-budget kill (:516), token-budget kill (:538), stage-timeout kill (:556), external-cancel on terminal stage (:479). Assert the resulting transition + status for each. This matrix must pass identically before and after Steps 1-6.
2. **Transition table test** (shared with the correctness spec's suite): every `StageTransition` type → expected stage-run/task/audit writes + post-commit closures. Locks `applyTransition` while collaborators move around it.
3. **CQ-05:** parametrized test over `enforceBudget` (cost vs token; under/over/at limit; sum-error → no kill).
4. **Per-step regression:** re-run matrix (1) + table (2) after each of Steps 1-6; a diff in outcomes = a behavior regression, stop and fix before continuing.
5. **Nil-injection test:** construct the orchestrator with each collaborator left nil → default is built, behavior identical (guards D5 / the test path).

## Risks

- **Highest-change-coupling file + most frequent regression site in the repo** (project memory: multi-spawn/cascade/permission bugs cluster here). Mitigation: Option C's one-collaborator-per-PR cadence, each gated on the full characterization matrix; never combine two extractions in one PR.
- **One lock surface (SQLite single writer).** All writes must keep funneling through the single-tx `applyTransition`; no extracted collaborator may open its own transaction or issue writes outside that path, or CQ-03-class contention returns. Assert "no second writer" by keeping `StageRunService` write methods delegating to the same tx-bound repos.
- **Async goroutines escape the tx** (`tryAttachSessionID` :788 via the `attachInFlight` dedupe :502-508, `updateTokenUsage` :802 via `go` :576). Inventory these before moving finalize into `StageRunService` so extraction does not accidentally serialize, double-fire, or drop the `attachInFlight` dedupe.
- **Config-cache coherence.** `InvalidateConfigCache` (:157) is called from settings mutations elsewhere; when `ConfigCache` moves out (Step 3), every existing caller must still reach the invalidation entry point. Grep all `InvalidateConfigCache` callers before the move.
- **Bug-compatible baseline.** Characterization tests written before the CQ-03 fix would lock in the pre-commit-side-effect bug. Hence D4's ordering: this spec ships strictly after the correctness spec, and the Step-0 matrix is authored against the corrected code.
- **`go test ./...` regenerates `internal/db/ent/`** — a stray full-suite run can corrupt the generated tree mid-refactor. Scope test runs to `./internal/pipeline/...` and restore the ent tree if the full suite is ever run.
