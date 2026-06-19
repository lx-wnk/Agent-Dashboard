# Task Refinement & Control Redesign — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.
> Spec: `docs/superpowers/specs/2026-06-18-task-control-redesign-design.md`.

**Goal:** Make task refinement/control simple, self-describing, and fully MCP/REST-controllable over one service layer.

**Architecture:** One `taskcontrol` service package owns every lifecycle transition + `ComputeActions`. REST handlers and MCP tools are thin adapters. Refinement collapses to a canonical `task.concept` (draft+amend). One `autonomy` knob presets permissions + gates.

**Tech Stack:** Go 1.26 (chi, ent, modernc/sqlite), Vue 3 TS. `task test` regenerates ent (keep `gen.FeatureUpsert`). Branch off main, PR into main.

**Global rules:** TDD. Frequent commits. No phase labels in commit msgs (describe behaviour). Edit/Write blocked while primary repo on a protected branch → if blocked, edit via Bash. Commits: `-c commit.gpgsign=false` if signing lock. Run `cd server && go build ./... && go test ./...`; frontend `pnpm test`.

---

## Sub-Project 1 — Service layer + availableActions (FOUNDATION)

**File structure:**
- Create `server/internal/taskcontrol/actions.go` — `Action` type, `ComputeActions(t TaskState) []Action`.
- Create `server/internal/taskcontrol/actions_test.go` — table tests.
- Create `server/internal/taskcontrol/state.go` — `TaskState` projection struct (stage, runStatus, refineStatus, autonomy, pendingPerms int, pendingApproval bool) + `FromTask(*ent.Task, latestRun, perms)`.
- Modify `server/internal/api/tasks/enrich.go` — add `AvailableActions []Action` to `EnrichedTask`; populate in `EnrichTask`/`EnrichTasksBulk`.
- (SSE already broadcasts EnrichedTask → actions flow automatically.)

**Action shape:** `{ Action string; Enabled bool; Reason string; Primary bool }`. Actions: `refine`,`approve_spec`,`advance`,`retry`,`back`,`cancel`,`hold`,`resume`,`approve_all_pending`,`open_pr`.

**Tasks:**
1. [ ] Test+impl `TaskState` projection (`FromTask`). Cases: concept/idle, concept/awaiting_user, implementation/failed, implementation/awaiting_user+pendingPerms>0, self_review/running, done.
2. [ ] Test+impl `ComputeActions`: for each state tuple assert the exact enabled action set + which is `primary` + disabled reasons. (e.g. concept+draft_ready → primary `approve_spec`; implementation+failed → primary `retry`; pendingPerms>0 → `approve_all_pending` enabled + advance disabled reason "blocked: N pending permissions"; done → only no-ops.)
3. [ ] Wire `AvailableActions` into `EnrichedTask` + both enrich paths; test enrich output contains actions. Commit.

**Acceptance:** `get_task`/`list_tasks` JSON includes `availableActions[]`; computed in ONE place; existing enrich tests still pass.

---

## Sub-Project 2 — Autonomy + allow-all gate + approve_all_pending (STALL FIX, highest value)

**File structure:**
- Modify `server/internal/db/ent/schema/task.go` — add field `autonomy` enum {`manual`,`spec_gated`,`full`} default `spec_gated`.
- Modify `server/internal/db/defaults.go` — `DefaultAutonomy = "spec_gated"`.
- Modify task repo `Create`/`UpdateTaskInput` — accept `Autonomy *string`.
- New `server/internal/taskcontrol/autonomy.go` — `IsAllowAll(autonomy string) bool`; `SeedAllowAllGrants(ctx, permRepo, taskID)` (atomic: one task_permission row `tool="*" mode="allow-all"` OR reuse permission_preset "full"). 
- Modify the pipeline permission gate (grep `IsSafeBashPattern`/permission check in `pipeline/` + `services/spawn_policy.go`) — if task autonomy is allow-all → auto-approve every request (write audit `auto_approved`), never set awaiting_user.
- New handler+tool `approve_all_pending`: REST `POST /api/tasks/{id}/approve-all-pending`, MCP `approve_all_pending` → expire pending requests + (optionally) bump autonomy or seed grants; re-queue if stalled.
- Modify `create_task` (REST + MCP `write.go`) to accept `autonomy` and seed grants when allow-all.

**Tasks:**
1. [ ] ent: add `autonomy` field + default; `cd server && go generate ./...` (verify FeatureUpsert intact); build.
2. [ ] repo: thread `Autonomy` through Create + Update; test.
3. [ ] Test+impl `IsAllowAll` + `SeedAllowAllGrants` (idempotent; one write covers all tools). 
4. [ ] Permission gate: test that an allow-all task auto-approves a request (no awaiting_user, audit row written) and a manual task still stalls. Impl.
5. [ ] `approve_all_pending` service fn + REST route + MCP tool: test it clears pending + unblocks; commit.
6. [ ] `create_task` autonomy param (REST + MCP) seeds grants on allow-all; test create→no-stall path.

**Acceptance:** A `spec_gated`/`full` task never hits `awaiting_user reaper: ... permissions pending`. `approve_all_pending` rescues a stalled `manual` task in one call. All over MCP.

---

## Sub-Project 3 — Refine redesign: canonical task.concept (depends on SP1)

**File structure:**
- Modify `server/internal/db/ent/schema/task.go` — ensure `metadata`/concept JSON field holds `concept` (reuse existing Metadata or add `concept` JSON col).
- Modify `server/internal/refine/concept.go` + `runner.go` — `refineStatus` enum `none|refining|draft_ready|approved|failed`; single-pass draft; `amend`; direct `concept` inject.
- Modify `server/internal/api/refine/handler.go` — keep turns history; `confirm`→`approve_spec` (freeze concept, advance); add direct-inject path.
- New MCP tools `refine_task`,`approve_spec`,`get_refine_status` in `mcp/tools/` over the same refine service.
- Implementation/review/finalization prompts (`pipeline/stage_prompts.go`) read `task.concept` (already read Description; add concept block from canonical field not empty metadata).

**Tasks:**
1. [ ] refineStatus enum migration (`idle`→`none`); test status transitions.
2. [ ] Test+impl single-pass `refine` draft → `draft_ready`; `amend` revises; `concept` inject path.
3. [ ] `approve_spec` freezes concept onto task + advances (replaces confirm); back-compat alias `confirm`. Test.
4. [ ] MCP tools `refine_task`/`approve_spec`/`get_refine_status`; parity test vs REST.
5. [ ] stage_prompts read canonical concept; test implementation prompt embeds concept (not `{}`).

**Acceptance:** `refine`→`approve_spec` works over MCP; description-inject path supported; implementation sees real concept.

---

## Sub-Project 4 — Verb set + full MCP parity (depends on SP1+SP3)

**File structure:**
- New `server/internal/taskcontrol/transitions.go` — `Advance`,`Back`,`Hold`,`Resume` calling existing orchestrator transitions; `Advance` runs the `Primary` action from ComputeActions.
- REST routes + MCP tools for `advance_task`,`back_task`,`hold_task`,`resume_task`.
- `progress_task` (REST+MCP) → thin deprecated alias of `advance`.

**Tasks:**
1. [ ] Test+impl `Advance` = execute primary action (dispatch by ComputeActions). Cases per stage.
2. [ ] `Back`/`Hold`/`Resume` service fns + tests.
3. [ ] REST routes + MCP tools for all four; parity golden test per verb.
4. [ ] `progress_task` aliases `advance`; deprecation note; test alias.

**Acceptance:** Agent loop `get_task`→`advance_task` drives a task end-to-end. Every verb REST+MCP, same service fn.

---

## Sub-Project 5 — UI thin-client (depends on SP1–SP4)

**File structure:**
- Modify `src/types.ts` — `AvailableAction`, `Autonomy`, task `concept` types.
- New `src/composables/useTaskActions.ts` — render `availableActions` → action buttons calling the matching endpoint.
- Modify TaskModal/TaskCard — render action buttons from `availableActions` (drop hard-coded confirm/progress/retry buttons); add `autonomy` selector; concept editor/diff view.

**Tasks:**
1. [ ] `useTaskActions` maps action→endpoint; Vitest.
2. [ ] TaskModal renders availableActions buttons (enabled/disabled+reason tooltip); test.
3. [ ] Autonomy selector (create + modal) wired to create/update; test.
4. [ ] Concept viewer/editor (read concept, amend, approve_spec); test.

**Acceptance:** UI shows exactly the valid actions; autonomy selectable; concept viewable/amendable. No hard-coded transition buttons.

---

## Build order & integration
Branch `feat/task-control-redesign` off main. Build SP1→SP2→SP3→SP4→SP5 sequentially (deps). Commit per task. `cd server && go build ./... && go test ./...` green before each commit; `pnpm test` for SP5. Open PR into main at the end (or per-SP if split). Do NOT merge to main unattended.

## Self-review notes
- Spec coverage: A→SP1, B→SP3, C→SP2, D→SP3+SP4 (MCP tools), E→build order. All covered.
- Risk: ent regen must keep FeatureUpsert. Permission-gate location must be confirmed by grep before editing. concept storage: reuse Metadata JSON to avoid a migration if a `concept` key suffices.
