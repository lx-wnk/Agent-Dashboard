# OFD Harness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the reusable Orchestrated Feature Delivery (OFD) harness — prompt templates, a main-thread runbook, and a registered skill — so any approved feature plan can be executed autonomously by a spawned orchestrator subagent that coordinates implementer/reviewer/verifier sub-subagents in one isolated git worktree until a draft PR.

**Architecture:** The harness is process tooling, not application code. It produces four artifacts under `docs/superpowers/harness/` plus one skill stub in `.agent-context/skills/`. Isolation comes from a manually-created git worktree directory (branched off `main`); NO agent ever uses the `Agent` `isolation:'worktree'` flag (that forks from origin/main — wrong base). Validation is a throwaway smoke-run that exercises the full chain end-to-end.

**Tech Stack:** Markdown artifacts; `git worktree`; `gh` CLI for draft PRs; the `Agent` tool (general-purpose orchestrator + specialist sub-subagents `agents:frontend`/`agents:backend`/`agents:review`). Verified 2026-06-23: nested subagent spawning works (depth ≥ 2).

**Spec:** `docs/superpowers/specs/2026-06-23-ofd-harness-design.md`

---

## File Structure

- Create: `docs/superpowers/harness/ofd-orchestrator-prompt.md` — the launch-prompt template handed to the orchestrator subagent (parametrized).
- Create: `docs/superpowers/harness/ofd-roles.md` — implementer / reviewer / verifier sub-subagent prompt contracts.
- Create: `docs/superpowers/harness/ofd-runbook.md` — main-thread step-by-step (worktree setup → launch → verify → PR review) with exact commands.
- Create: `.agent-context/skills/ofd-harness.md` — skill stub with trigger frontmatter pointing at the runbook.
- Modify: `.agent-context/skills/index.md` — register the new skill.

Each file has one responsibility: the orchestrator prompt is *what the coordinator does*, roles are *what each worker does*, the runbook is *what the human-side main thread does*, the skill makes it discoverable.

---

## Task 1: Orchestrator launch-prompt template

**Files:**
- Create: `docs/superpowers/harness/ofd-orchestrator-prompt.md`

- [ ] **Step 1: Write the template file**

Write this exact content:

````markdown
# OFD Orchestrator Launch Prompt (template)

> Fill the `{{...}}` placeholders, paste as the `Agent` prompt with `subagent_type: general-purpose`, NO `isolation` flag.

You are the OFD orchestrator for feature **{{featureName}}**. You coordinate sub-subagents to implement an approved plan to a draft PR. You write no code yourself — you dispatch and review.

## Your worktree (hard boundary)
- Operate ONLY inside `{{worktreePath}}` (branch `{{branch}}`). Never edit any path outside it.
- When you dispatch sub-subagents, pass them this absolute worktree path and instruct them to work only there.
- NEVER pass `isolation:'worktree'` to any Agent call. Isolation already comes from this worktree directory.

## Inputs
- Approved plan: `{{planPath}}` — read it fully before starting.
- Review cadence: `{{reviewCadence}}` (`after-each-task` | `single-final`).
- Parallelism: `{{parallelism}}` (`sequential` | `disjoint-parallel`).
- Iteration cap before BLOCKED: `{{iterationCap}}`.
- Role map: implementer=`{{implementerType}}`, reviewer=`agents:review`, verifier=`general-purpose`.

## Loop
For each task in the plan, in order:
1. Dispatch an **implementer** sub-subagent (`subagent_type {{implementerType}}`) with: the task's steps verbatim, the worktree path, the rule "work only in this worktree", TDD ("write the failing test first, watch it fail, then implement"), and "return: files changed + test command output". See role contract in `ofd-roles.md`.
2. If `reviewCadence == after-each-task`: dispatch a **reviewer** (`subagent_type agents:review`) on the task's diff. If it returns blocking findings, re-dispatch the implementer to fix. Repeat up to `{{iterationCap}}` times. Stop the loop early and go to BLOCKED if not clean after the cap.
3. Commit the task (`git -C {{worktreePath}} commit --no-gpg-sign`, English message, no phase labels, with the trailers from the runbook). Proceed to the next task.

If `parallelism == disjoint-parallel`: tasks the plan marks as file-disjoint may be dispatched concurrently; all others run sequentially. When unsure whether two tasks share files, run them sequentially.

## Final verification (always)
Dispatch a **verifier** sub-subagent that runs, in `{{worktreePath}}`:
```
pnpm lint && pnpm typecheck && pnpm test
go build ./... && go test ./...
```
(Run the Go commands in `./server` and `./sdk` as the touched modules require.)

## Done-definition (PR gate)
Open the PR only when ALL hold: every task complete; all the above checks green; docs updated if the feature is user-facing (README + CHANGELOG at minimum); commits signed-off, English, no phase labels.

- If green: `git -C {{worktreePath}} push -u origin {{branch}}` then open a **draft** PR with `gh pr create --draft --base main`. 
- If still red after `{{iterationCap}}` iterations: open a **draft** PR with `[BLOCKED]` in the title and the verbatim failing output in the body. Do not claim success.

## Return report (your final message)
Return: branch · PR URL · per-task status table · verbatim tail of final CI output · deviations from plan · open questions for the human. Keep it structured; the main thread re-verifies via git log and CI, so be accurate over verbose.
````

- [ ] **Step 2: Verify all launch-payload fields are present**

Run: `grep -oE '\{\{[a-zA-Z]+\}\}' docs/superpowers/harness/ofd-orchestrator-prompt.md | sort -u`
Expected output includes: `{{branch}}`, `{{featureName}}`, `{{implementerType}}`, `{{iterationCap}}`, `{{parallelism}}`, `{{planPath}}`, `{{reviewCadence}}`, `{{worktreePath}}`

- [ ] **Step 3: Commit**

```bash
git add docs/superpowers/harness/ofd-orchestrator-prompt.md
git commit --no-gpg-sign -m "feat: OFD orchestrator launch-prompt template"
```

---

## Task 2: Sub-subagent role contracts

**Files:**
- Create: `docs/superpowers/harness/ofd-roles.md`

- [ ] **Step 1: Write the role-contracts file**

Write this exact content:

````markdown
# OFD Sub-Subagent Role Contracts

All sub-subagents: operate ONLY in the worktree path the orchestrator gives you. Never pass `isolation` to any Agent call. Never edit outside the worktree.

## Implementer
- `subagent_type`: `agents:frontend` (Vue/TS) or `agents:backend` (Go), chosen per task.
- Input: one plan task's steps, the worktree absolute path.
- Method: TDD — write the failing test first, run it to confirm it fails, then write the minimal code to pass, then run tests green. Follow existing codebase patterns. Default to NO comments (project rule).
- Output (return message): list of files changed, the exact test command run, and its pass/fail tail. Do not commit — the orchestrator commits.

## Reviewer
- `subagent_type`: `agents:review` (read-only).
- Input: the task diff (`git -C <worktree> diff <range>`), the task's intent.
- Method: review for correctness bugs, project-convention violations (DRY, single-responsibility, no stray comments, SSOT), and missing tests. Do not edit code.
- Output: severity-tagged findings (`blocking` | `nit`). If no blocking findings, return exactly `REVIEW_CLEAN`.

## Verifier
- `subagent_type`: `general-purpose`.
- Input: the worktree absolute path.
- Method: run `pnpm lint && pnpm typecheck && pnpm test` then `go build ./... && go test ./...` (server + sdk as touched). Trust in-worktree `go build` over any LSP diagnostics (worktrees emit false gopls errors).
- Output: `VERIFY_GREEN` or `VERIFY_RED` followed by the verbatim failing output tail.
````

- [ ] **Step 2: Verify the three roles and their sentinels exist**

Run: `grep -E 'REVIEW_CLEAN|VERIFY_GREEN|VERIFY_RED|## Implementer|## Reviewer|## Verifier' docs/superpowers/harness/ofd-roles.md`
Expected: all six lines present.

- [ ] **Step 3: Commit**

```bash
git add docs/superpowers/harness/ofd-roles.md
git commit --no-gpg-sign -m "feat: OFD sub-subagent role contracts"
```

---

## Task 3: Main-thread runbook

**Files:**
- Create: `docs/superpowers/harness/ofd-runbook.md`

- [ ] **Step 1: Write the runbook file**

Write this exact content:

````markdown
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
````

- [ ] **Step 2: Verify the runbook commands parse**

Run: `grep -E 'git worktree add|pnpm i|gh pr|--no-gpg-sign|git worktree remove' docs/superpowers/harness/ofd-runbook.md`
Expected: worktree-add, pnpm-i, no-gpg-sign, and worktree-remove lines present. (`gh pr` lives in the orchestrator prompt, not the runbook — its absence here is correct.)

- [ ] **Step 3: Commit**

```bash
git add docs/superpowers/harness/ofd-runbook.md
git commit --no-gpg-sign -m "feat: OFD main-thread runbook"
```

---

## Task 4: Register the harness as a discoverable skill

**Files:**
- Create: `.agent-context/skills/ofd-harness.md`
- Modify: `.agent-context/skills/index.md`

- [ ] **Step 1: Inspect the existing skill index format**

Run: `cat .agent-context/skills/index.md`
Expected: a table or list of skills; note the exact column/row format to match it.

- [ ] **Step 2: Write the skill stub**

Write this content to `.agent-context/skills/ofd-harness.md` (a stub that routes to the runbook — heavy reference stays in `docs/superpowers/harness/`):

```markdown
---
name: ofd-harness
trigger: when executing an approved feature plan autonomously via an orchestrator subagent in an isolated worktree; keywords "OFD", "orchestrated delivery", "run the plan with subagents", "spawn orchestrator"
---

# OFD Harness

Reusable process: human-gated spec/plan, then a spawned general-purpose **orchestrator** subagent coordinates implementer/reviewer/verifier **sub-subagents** in ONE git worktree until a draft PR.

- Runbook (main thread): `docs/superpowers/harness/ofd-runbook.md`
- Orchestrator prompt: `docs/superpowers/harness/ofd-orchestrator-prompt.md`
- Role contracts: `docs/superpowers/harness/ofd-roles.md`
- Design: `docs/superpowers/specs/2026-06-23-ofd-harness-design.md`

Hard rule: isolation comes from the manually-created worktree (branched off `main`); never use the Agent `isolation:'worktree'` flag.
```

- [ ] **Step 3: Add the index row**

Add a row/line to `.agent-context/skills/index.md` matching the format observed in Step 1, pointing at `ofd-harness.md` with the one-line purpose "Run an approved feature plan autonomously via an orchestrator subagent in a worktree."

- [ ] **Step 4: Verify discoverability**

Run: `grep -r 'ofd-harness' .agent-context/skills/`
Expected: both the new file and the index entry match.

- [ ] **Step 5: Commit**

```bash
git add .agent-context/skills/ofd-harness.md .agent-context/skills/index.md
git commit --no-gpg-sign -m "feat: register OFD harness skill"
```

---

## Task 5: Smoke-test the harness end-to-end (throwaway)

Goal: prove the orchestrator → sub-subagents → draft PR chain works on a trivial change before risking a real feature (B1). This is the harness's acceptance test. The branch/PR is discarded afterward.

**Files:**
- No repo files created by this task directly; it exercises the harness on a throwaway worktree.

- [ ] **Step 1: Create a throwaway worktree**

```bash
git worktree add -b chore/ofd-smoke ../dashboard-wt-ofd-smoke main
cd ../dashboard-wt-ofd-smoke && pnpm i
```

- [ ] **Step 2: Write a one-line throwaway plan**

Create `../dashboard-wt-ofd-smoke/SMOKE_PLAN.md` with a single trivial task: "Add a line `<!-- ofd-smoke -->` to the end of `CHANGELOG.md`; no test needed; commit." (This deliberately has no code/test to keep the smoke run cheap; the verifier still runs the full CI suite.)

- [ ] **Step 3: Launch the orchestrator on the smoke plan**

Dispatch the orchestrator per `ofd-runbook.md` §3 with `featureName=ofd-smoke`, `worktreePath=<abs path to ../dashboard-wt-ofd-smoke>`, `planPath=.../SMOKE_PLAN.md`, `reviewCadence=single-final`, `parallelism=sequential`, `iterationCap=2`, `implementerType=agents:backend`.

- [ ] **Step 4: Verify the chain produced a draft PR**

Run: `gh pr list --head chore/ofd-smoke --json number,isDraft,title`
Expected: one draft PR. Confirm `git -C ../dashboard-wt-ofd-smoke log --oneline main..HEAD` shows the orchestrator's commit, and that CHANGELOG.md contains the marker.
Expected outcome: this proves nesting, worktree boundary, verifier, and draft-PR opening all work together.

- [ ] **Step 5: Tear down the throwaway**

```bash
gh pr close chore/ofd-smoke --delete-branch 2>/dev/null || true
git worktree remove --force ../dashboard-wt-ofd-smoke
git branch -D chore/ofd-smoke 2>/dev/null || true
```
(`--delete-branch` is acceptable here because this is a throwaway smoke branch with no worktree to preserve after removal — distinct from the `main_merge_mechanics` rule for real feature branches.)

- [ ] **Step 6: Record the smoke result**

Append a one-line note to `docs/superpowers/harness/ofd-runbook.md` under a new `## Smoke validation` heading: the date and "chain verified: nesting + worktree boundary + verifier + draft PR". Commit:

```bash
git add docs/superpowers/harness/ofd-runbook.md
git commit --no-gpg-sign -m "docs: record OFD smoke validation result"
```

---

## Self-Review

**Spec coverage:**
- §2 topology / §3 flow → Tasks 1 (orchestrator) + 3 (runbook). ✓
- §3 step 2 nesting probe → runbook §1 + already verified; Task 5 exercises it. ✓
- §4 done-definition → encoded in Task 1 orchestrator prompt PR gate. ✓
- §5 launch payload → Task 1 placeholders, asserted in Step 2. ✓
- §6 return report + re-verification → Task 1 report section + runbook §4. ✓
- §7 plan-level params → consumed as orchestrator placeholders; chosen during each feature's planning. ✓
- §8 role contracts → Task 2. ✓
- §9 risks → mitigations baked into prompts (no-isolation rule, sequential default, go-build-over-LSP). ✓
- §10 first run (B1) → out of scope here by design; harness validated via throwaway smoke (Task 5), B1 is a separate spec/plan. ✓

**Placeholder scan:** The `{{...}}` tokens are intentional template parameters, not plan placeholders; every artifact's full content is given inline. No "TBD"/"handle edge cases". ✓

**Type consistency:** Sentinels `NESTING_OK`/`REVIEW_CLEAN`/`VERIFY_GREEN`/`VERIFY_RED` and placeholder names are used identically across Tasks 1–3 and the verify steps. ✓
