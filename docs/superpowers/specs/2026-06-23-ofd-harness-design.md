# Orchestrated Feature Delivery (OFD) Harness — Design

> Date: 2026-06-23
> Status: Approved (design); pending implementation plan
> Purpose: A reusable dev-time process for shipping one feature from an approved plan to a draft PR, executed autonomously by a spawned orchestrator subagent that coordinates its own sub-subagents in an isolated git worktree.
> First consumer: B1 (plan-mode + checkpoints) from `docs/local/2026-06-23-competitor-feature-gap-conductor-orca-soloterm.md`. Reused for B2–B9.

## 1. Goal & Non-Goals

**Goal.** Given a human-approved spec and implementation plan, deliver the feature end-to-end without further human steering until a draft PR is ready for review. The same harness is reused per feature; only the plan changes.

**Non-goals.**
- Not the product's own task pipeline. The product (concept→implementation→review→done) spawns `claude` CLI *OS processes* per stage in worktrees. This harness coordinates *in-conversation subagents* at dev time. They are conceptually parallel but mechanically separate.
- Not a replacement for human spec/plan review. Intent is human-gated; only execution is autonomous.
- No auto-merge. The harness stops at a draft PR.

## 2. Agent Topology

Three tiers:

```
Main thread (you + assistant)     human-gated: brainstorm → spec → plan
        │ launch with approved plan + worktree path + contracts
        ▼
Orchestrator subagent             autonomous coordinator, owns ONE worktree
        │ dispatches via the Agent tool
        ▼
Sub-subagents                     implementer / reviewer / verifier
                                  share the orchestrator's single worktree
```

**Critical worktree rule.** Sub-subagents do **not** each take `isolation:'worktree'`. That forks a fresh worktree from `origin/main` and diverges from the feature branch (lesson: `worktree_isolation_baseref`). All sub-subagents edit the orchestrator's one worktree. Parallelism is therefore only safe on **file-disjoint** tasks; anything touching shared files serializes.

## 3. End-to-End Flow

1. **Spec + plan (human-gated, main thread).** Brainstorm → `spec.md` (user approval) → `writing-plans` → `plan.md` (user approval). The plan records this feature's execution parameters (§7).
2. **Pre-flight nesting probe (once).** A single throwaway call verifying that a spawned subagent can itself spawn a subagent (nesting depth ≥ 2). The general-purpose agent has the `Agent` tool, but harness nesting caps are common and unverified. Pass → proceed. Blocked → fall back: orchestrator becomes a `Workflow` script, or the topology flattens to main-thread-as-orchestrator (superpowers `subagent-driven-development`).
3. **Worktree setup (main thread).** Create `dashboard-worktrees/<feature>` branched off **`main`** (not `origin/main`, to retain local commits) and run `pnpm i` (worktrees lack `node_modules`). Go needs no per-worktree install.
4. **Launch orchestrator (main thread).** Spawn one general-purpose orchestrator subagent with the launch payload (§5).
5. **Orchestration loop (orchestrator).** Iterate plan tasks at the configured cadence: dispatch implementer (TDD — test first) → dispatch reviewer → fix loop until clean → next task. Run the verifier at the end.
6. **Draft PR (orchestrator).** When the done-definition (§4) is met, open a **draft** PR and return the structured report (§6).
7. **Human review (main thread + user).** Review PR; iterate or merge per `main_merge_mechanics`.

## 4. Done-Definition (autonomous exit bar)

The orchestrator may open the PR only when **all** hold:

- (a) Every plan task is complete.
- (b) `pnpm lint` + `pnpm typecheck` + `pnpm test` all green.
- (c) `go build ./...` + `go test ./...` all green (run in both `./sdk` and `./server` workspace modules as relevant).
- (d) If the feature is user-facing, docs are updated in the same change — at minimum `README.md`, `CHANGELOG.md` (Keep a Changelog), and any touched guide (layer2 docs rule).
- (e) Commits are signed-off, English, no phase labels (`feedback_commit_message_style`), with the required co-author/session trailers.

If any check is red after the iteration cap (§7), the orchestrator opens a **draft PR flagged `BLOCKED`** with the exact failure output rather than claiming success (`verify_ci_before_done`). The harness never reports "done" while CI is red.

## 5. Launch Payload (main → orchestrator)

| Field | Meaning |
|---|---|
| `worktreePath` | Absolute path to the prepared worktree |
| `planPath` | Path to the approved `plan.md` |
| `reviewCadence` | `after-each-task` (default) \| `single-final` |
| `parallelism` | `sequential` (default) \| `disjoint-parallel` |
| `roleMap` | role → agent-type (implementer→frontend/backend, reviewer→review, verifier→general) |
| `doneDefinition` | The §4 checklist, inline |
| `iterationCap` | Max fix iterations before BLOCKED |
| `prMode` | `draft-only` (fixed for now) |

## 6. Orchestrator Return Report (orchestrator → main)

Branch name · PR URL · per-task status table · final CI output (verbatim tail) · deviations from plan · open questions for the human.

**Verification note.** Subagent final messages can truncate mid-sentence while work still lands (`subagent_final_message_truncation`). The main thread verifies outcomes via `git log`, the PR, and re-running CI — never by trusting the report prose alone.

## 7. Plan-Level Parameters

Recorded in each feature's `plan.md`, chosen with the user during planning:

- **Review cadence** — default `after-each-task`.
- **Parallelism** — default `sequential`.
- **Role overrides** — e.g. force `agents:backend` for a Go-only feature.
- **Iteration cap before BLOCKED** — default 3.

## 8. Sub-Subagent Role Contracts

- **Implementer** — `agents:frontend` (Vue/TS) or `agents:backend` (Go) per task. TDD: write failing test first, then implement. Edits only the orchestrator's worktree. Returns: files changed, test status.
- **Reviewer** — `agents:review` (read-only) or `caveman:cavecrew-reviewer` for terse diff review. Returns severity-tagged findings; no code edits.
- **Verifier** — runs the full §4 (b)/(c) command set, returns pass/fail with verbatim output.

## 9. Risks & Mitigations

| Risk | Mitigation |
|---|---|
| Nesting depth capped (subagent can't spawn subagent) | §3 step 2 pre-flight probe; fallback to Workflow or flat topology |
| Parallel sub-subagents clobber shared files | Default sequential; parallel only on file-disjoint task sets |
| Worktree forks from wrong base | Branch off `main` explicitly; never rely on `isolation:'worktree'` for sub-subagents |
| Orchestrator reports false success | Done-definition gate + main-thread re-verification via git/CI |
| Worktree deps missing | `pnpm i` at setup; verifier surfaces module errors early |
| Worktree Go LSP false errors | Trust in-worktree `go build`, not LSP diagnostics (`worktree_gopls_false_errors`) |

## 10. First Run

B1 (plan-mode + checkpoints) is the proving pilot. After the harness plan is written, B1 gets its own `spec.md` + `plan.md` and runs through OFD end-to-end. Lessons from the B1 run feed back into this design before B2–B9.
