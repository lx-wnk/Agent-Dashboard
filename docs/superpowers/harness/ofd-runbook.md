# OFD Runbook (main thread)

Preconditions: an approved spec and an approved plan exist under `docs/superpowers/`. The plan records review cadence, parallelism, role overrides, and iteration cap.

## Execution model — main-thread orchestrated (default)

By default the **main thread is the orchestrator**. There is NO separate spawned orchestrator subagent. The main thread reads the plan and dispatches the sub-subagents (implementer / reviewer / verifier) itself, one task at a time, using `superpowers:subagent-driven-development`. The orchestrator role checklist lives in `ofd-orchestrator-prompt.md` — the main thread follows it directly.

**Why default:** a *spawned* orchestrator subagent is not reliably re-woken after it dispatches a child, so it stalls mid-run (observed on the first real run, plan-mode). A synchronous `Agent` call from the main thread always returns to the main thread — so main-thread orchestration cannot stall. Reliability beats the background/context-isolation the nested model offered.

> Optional background mode (advanced, may stall): you *can* paste `ofd-orchestrator-prompt.md` into a spawned `general-purpose` subagent (`run_in_background: true`) for fire-and-forget runs. It keeps the main context lean but WILL likely stall — the model tends to background long child tasks despite instructions, and then is not re-woken. If you use it, the main thread must babysit: verify state via `git log`/`gh` and `SendMessage`-resume, or take over as orchestrator. Not recommended for multi-task plans.

## 1. (First session only) Confirm nesting works
Only relevant for the optional background mode. Spawn a throwaway general-purpose subagent instructed to spawn its own subagent returning `PONG`. Expect `NESTING_OK`. Verified working 2026-06-23. (Main-thread mode does not need this — the main thread always spawns its own workers directly.)

## 2. Create the worktree (off main)
```bash
git worktree add -b feat/<feature> ../dashboard-wt-<feature> main
cd ../dashboard-wt-<feature> && pnpm i
```
Branch off `main` (not `origin/main`) to keep local commits. `pnpm i` because worktrees have no `node_modules`. Go needs no per-worktree install.

## 3. Orchestrate the plan (main thread)
Follow `ofd-orchestrator-prompt.md` as your own role. For each plan task, in order:
1. Dispatch an implementer sub-subagent (foreground `Agent` call — it returns to you) with the task text verbatim, the worktree path, "work only in this worktree", and TDD.
2. Per the review cadence, dispatch a reviewer (`agents:review`) on the task diff; fix-loop until clean (cap = iterationCap).
3. Commit the task (`--no-gpg-sign`, English message, no phase labels, trailers below).
4. Next task.
Then dispatch the verifier (full CI). Every `Agent` call is foreground/synchronous — never `run_in_background`, never `Monitor`.

Commit trailers every commit must carry:
```
Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: <this session URL>
```
All commits use `--no-gpg-sign` (signing hangs in this setup).

## 4. Verify before the PR (never trust a subagent report's prose)
```bash
git -C ../dashboard-wt-<feature> log --oneline main..HEAD
cd ../dashboard-wt-<feature> && pnpm lint && pnpm typecheck && pnpm test && go build ./... && go test ./...
```
Subagent reports can truncate mid-sentence while work still lands — confirm via git log + CI, not the report text.

## 5. Open + review the PR
Completed + self-reviewed work → normal PR (draft only if genuinely incomplete). If `[BLOCKED]`, read the failing output and decide. On approval, merge per `main_merge_mechanics` (squash, `--admin`, never `--delete-branch` — worktrees live).

## 6. Cleanup (after merge only)
```bash
git worktree remove ../dashboard-wt-<feature>
```

## Validated lessons (first runs)
- **Verifier lints untracked files:** the verifier's `pnpm lint` (antfu eslint) lints **untracked** files in the worktree too — a loose scratch/plan md causes a false RED. Keep only deliverables in the worktree; the approved plan lives under tracked `docs/superpowers/`.
- **Stall on background dispatch (why main-thread is default):** on the first real run a spawned orchestrator that backgrounded children + waited via `Monitor` came to rest mid-task and stalled across multiple resumes. Main-thread orchestration removes the failure mode.
- **Take-over hazard:** a "stalled" background orchestrator may still be running silently and will edit the worktree. If you take over concurrently, both edit the same tree. `TaskStop` the orchestrator BEFORE taking over.
