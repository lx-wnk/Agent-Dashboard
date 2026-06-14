# ADR-0006: Extract `worktree` Leaf Package

**Status:** Accepted
**Date:** 2026-06-12

## Context

The git-shell logic for managing task worktrees was duplicated across two
layers that never shared code:

- `pipeline/worktree.go` (lifecycle) — `ensureTaskWorktree` / `removeTaskWorktree`
  derived the worktree root (`$HOME/dashboard-worktrees` fallback), the path
  (`<root>/<slug>`), and the branch (`feat/<slug>` fallback) inline, then ran
  git via bare `exec.Command` with **no timeout**.
- `services/worktree_manager.go` (inspection) — `WorktreeStatus` ran git via a
  private `runGit` helper that re-implemented git-binary `LookPath`, a 15s
  context timeout, and `cmd.Dir`/`cmd.Output` plumbing.

Both re-derived the same `git` lookup and exec wrapper; the literal
`dashboard-worktrees` root name appeared a third time in `config.go`'s
`Defaults()` — an SSOT violation (finding Arch-P3-3).

Neither file references the pipeline state machine. The shared logic is a
self-contained leaf whose only dependency is the standard library.

## Decision

Create `server/internal/worktree/` (`package worktree`), depending only on the
standard library, exposing:

- `Runner` — `git` `LookPath` (falling back to `"git"`) + 15s per-call context
  timeout. `Output` returns stdout only (inspection); `Combined` returns
  stdout+stderr merged (mutation diagnostics).
- `DefaultRoot(root)` — returns root, or `$HOME/dashboard-worktrees` when empty.
- `PathFor(root, slug)` — `<resolved-root>/<slug>`.
- `CreateBranch(*sourceBranch, slug)` — `*sourceBranch` when set, else `feat/<slug>`.
- consts `DefaultRootDirName` and `BranchPrefix`.

Rewire the consumers:

| Consumer | Change |
|---|---|
| `pipeline/worktree.go` | derives root/path/branch via the leaf and runs git via a package `Runner.Combined`; keeps the `-b`-then-bare-add fallback, dual-error string, `os.Stat` idempotency, `MkdirAll(0o750)`, and `ensureTaskWorktree`'s signature (uses `context.Background()` internally — zero caller churn). **Gains a bounded timeout it lacked** (120s for mutations such as `git worktree add`, vs 15s for inspection reads). |
| `services/worktree_manager.go` | holds `*worktree.Runner`; drops `gitBin`/`timeout`/`runGit`; repoints `currentBranch`/`remoteRefExists`/`revListCount`/`dirtyState` to `runner.Output`. Distinct checkout-branch/base derivation left untouched. |
| `config/config.go` | `Defaults().WorktreeRoot` references `worktree.DefaultRootDirName` instead of the literal. |

One intentional behaviour change: pipeline mutations, previously run with a
bare untimed `exec.Command`, now inherit the leaf's 120s mutation timeout — a
deliberately generous bound that won't fire for a healthy `git worktree add`
but prevents an indefinite hang. Inspection reads keep the 15s bound.
Otherwise behaviour is preserved: `pipeline/worktree_test.go` stays green
unmodified and `WorktreeStatus` output is identical.

## Consequences

**SSOT restored.** The `dashboard-worktrees` name and the git lookup/exec
wrapper each live in exactly one place.

**`pipeline -> worktree` and `services -> worktree` are legal** high-to-low
edges to a stdlib-only leaf; `config -> worktree` is likewise downward with no
import cycle.

**Lifecycle-vs-inspection split preserved.** Mutating operations stay in
`pipeline/`; read-only status stays in `services/`. The leaf only holds the
shared primitives — it never decides *when* to create or remove a worktree.

**Branch-derivation claim rejected.** The consolidated todo flagged
"branch-derivation duplicated," but the two derivations differ semantically:
pipeline derives the **branch to create** (`feat/<slug>` fallback), while
services derives the **base branch to compare against** (`TargetBranch` then
`SourceBranch`). Only the create-branch logic moved to `CreateBranch`; the
base-derivation stays in `WorktreeStatus`.

## Alternatives Considered

1. **Share via `services/`.** `services/` may import `pipeline` types, which
   would blur the leaf guarantee. Rejected — a dedicated leaf keeps the
   dependency floor at the standard library.

2. **Two small private helpers, no shared package.** Keeps the duplication and
   the SSOT violation; does not give `pipeline` the missing timeout. Rejected.
