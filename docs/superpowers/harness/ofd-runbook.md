# OFD Runbook (main thread)

Preconditions: an approved spec and an approved plan exist under `docs/superpowers/`. The plan records review cadence, parallelism, role overrides, and iteration cap.

## 1. (First session only) Confirm nesting works
Spawn a throwaway general-purpose subagent instructed to spawn its own subagent returning `PONG`. Expect `NESTING_OK`. If `NESTING_BLOCKED`: stop and switch the orchestrator to a `Workflow` script (out of scope for this runbook). Verified working 2026-06-23.

## 2. Create the worktree (off main)
```bash
git worktree add -b feat/<feature> ../dashboard-wt-<feature> main
cd ../dashboard-wt-<feature> && pnpm i
```
Branch off `main` (not `origin/main`) to keep local commits. `pnpm i` because worktrees have no `node_modules`. Go needs no per-worktree install.

## 3. Launch the orchestrator
Fill `docs/superpowers/harness/ofd-orchestrator-prompt.md` placeholders and dispatch ONE `Agent` call: `subagent_type: general-purpose`, NO `isolation` flag, `run_in_background: true` for long runs. Pass the absolute worktree path.

Commit trailers every commit must carry:
```
Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: <this session URL>
```
All commits use `--no-gpg-sign` (signing hangs in this setup).

## 4. Re-verify the orchestrator's report (never trust prose alone)
```bash
git -C ../dashboard-wt-<feature> log --oneline main..HEAD
cd ../dashboard-wt-<feature> && pnpm lint && pnpm typecheck && pnpm test && go build ./... && go test ./...
```
Subagent reports can truncate mid-sentence while work still lands — confirm via git log + CI, not the report text.

## 5. Review the draft PR
Open the PR. If `[BLOCKED]`, read the failing output and decide: re-launch with a tightened plan, or fix inline. On approval, merge per the project's `main_merge_mechanics` (squash, `--admin`, never `--delete-branch` — worktrees live).

## 6. Cleanup (after merge only)
```bash
git worktree remove ../dashboard-wt-<feature>
```

## Smoke validation
- 2026-06-24 — chain verified end-to-end: nesting (orchestrator spawned implementer + verifier) + worktree boundary + verifier + draft PR (#218, throwaway, closed). Self-healed one false VERIFY_RED within the iteration cap.
- Gotcha learned: the verifier's `pnpm lint` (antfu eslint) lints **untracked** files in the worktree too — a throwaway/non-deliverable file (e.g. a loose plan or scratch md) causes a false RED. Keep only deliverables in the worktree; the approved plan should live under tracked `docs/superpowers/` (lint-clean), not loose in the worktree root.
