# Plan-Mode (B1) — Design

> Date: 2026-06-24
> Status: Approved (design); pending implementation plan
> Feature source: B1 in `docs/local/2026-06-23-competitor-feature-gap-conductor-orca-soloterm.md` (Conductor "Plan Mode").
> First real consumer of the OFD harness (`docs/superpowers/specs/2026-06-23-ofd-harness-design.md`).

## 1. Goal & Non-Goals

**Goal.** Insert a human approval gate on the *implementing agent's own execution plan* before any files are edited, scoped to the pipeline. After the concept is approved, a new `plan_review` stage auto-generates a concrete execution plan, auto-self-reviews it, and surfaces only the vetted plan for the user to approve or reject — before the implementation stage spawns and edits anything.

**Non-goals.**
- Not plan-mode for live interactive (non-pipeline) spawned agents — that is a separate feature (edit-gate surface). This spec covers pipeline tasks only.
- Not checkpoints/revert (B2) — separate feature, separate seam (worktree git snapshots).
- No change to the default pipeline flow. Plan-mode is opt-in; tasks with it off behave exactly as today.

## 2. Pipeline Shape

Today: `concept -> implementation -> self_review -> finalization -> done`.

With plan-mode **on**:
```
concept -> plan_review -> implementation -> self_review -> finalization -> done
```
With plan-mode **off** (default): unchanged — `concept -> implementation -> ...`.

## 3. The `plan_review` Stage (internal flow)

The stage handler runs three internal steps, then gates:

1. **Generate.** A planning agent reads the approved concept (`spec` + rough `plan` already carried by the concept) and produces a concrete execution plan: files to touch, ordered steps, test approach. Separate agent invocation -> own per-stage spawner/model via existing `pipeline_config` (may be a cheaper model).
2. **Auto self-review.** A second agent pass critiques the generated plan (completeness, fidelity to the concept, missing/incorrect files, test gaps) and **rewrites** it into a vetted version. The raw first-pass plan is never surfaced to the user.
3. **Gate.** The task flips to `awaiting_user` (reusing `orchestrator.ResumeFromUser` pause/resume). The user sees only the vetted plan.

**Resolution:**
- **Approve** -> `approve_plan` advances the task to `implementation`. The frozen approved plan is injected into the implementation agent's prompt context.
- **Reject (+ optional feedback)** -> regenerate (steps 1–2 again) incorporating the feedback, capped at `planIterationCap` (default 3, reusing the existing schema-iterate loop pattern). On exhausting the cap, the task stays `awaiting_user` with the latest plan and a note.

## 4. Opt-in & Configuration

- **`planMode` boolean, default `false`.** When `false`, the `plan_review` stage is skipped entirely and the state machine behaves exactly as today.
- Settable per-task at create/update (MCP `create_task`/`update_task` + REST), and as a per-project default via existing `pipeline_config` / `PUT /api/projects/{id}/pipeline-config`.
- Precedence: task-level `planMode` overrides project default; project default overrides the global default (`false`).

## 5. Data Model

- **`stage_run` stage enum** gains the `plan_review` value (ent). Schema change applied via the idempotent pre-migrate hook pattern to avoid the phantom-column rebuild crash on existing DBs (lesson: `ent_phantom_column_rebuild`).
- **Approved plan** stored frozen on the task (mirroring how the concept is frozen on approval).
- **Turn history** for generate/self-review/reject-feedback persisted as `refinementturn`-style rows with a new `phase = "plan_review"`.
- No git artifacts here — plan-mode is pure data. (Git snapshots belong to B2/checkpoints.)

## 6. API & MCP Surface

- **`approve_plan`** — REST endpoint + MCP tool (scope `pipeline:control`), mirroring `approve_spec`/`refineapi.Confirm`: freezes the approved plan and advances `plan_review -> implementation`.
- **Reject** — REST + MCP variant carrying optional feedback text; triggers regenerate (steps 1–2) within the iteration cap.
- **`get_plan_status`** — read endpoint mirroring `get_refine_status` so the UI/agents can poll the current vetted plan and gate state.
- Each new MCP tool registered in `ToolScopeMap` (auth.go) to avoid the missing-scope panic (lesson: `subagent_final_message_truncation`).

## 7. UI

- **Kanban column** for `plan_review` — add to `src/utils/stageLabels.ts` (`STAGE_LABELS`, `STAGE_DESCRIPTIONS`).
- **Plan-review panel** — adapt `RefinementChat.vue` / `RefineStatusPanel.vue`: renders the vetted plan (markdown), with **Approve** and **Reject + feedback** controls wired to `approve_plan` / reject endpoints. Shown only when the task is in `plan_review` + `awaiting_user`.
- **Task-modal stage view** surfaces the `plan_review` stage like the other stages (`StageOutputView.vue`).
- The `plan_review` column/panel only appears for tasks with `planMode` on.

## 8. Testing (TDD per OFD)

**Go (spawner stubbed via `OrchestratorOptions.SpawnFn` DI seam — no real spawns in `go test`, lesson `no_real_agent_tests`):**
- State machine: `concept -> plan_review -> implementation` when `planMode` on; `concept -> implementation` when off; reject -> regenerate loop respects `planIterationCap`.
- Stage handler: generate -> self-review -> gate (`awaiting_user`) sequence; approved plan frozen + injected.
- `approve_plan` / reject / `get_plan_status` endpoints + MCP tools (incl. `ToolScopeMap` entry).
- Idempotent pre-migrate hook: seed old-shape DB, assert no phantom-column rebuild crash.

**Vue (Vitest):**
- Plan-review panel renders the plan, emits approve/reject(+feedback).
- `plan_review` column appears only when `planMode` is on.

**Docs:** README + CHANGELOG updated (user-facing feature, layer2 rule).

## 9. Risks & Mitigations

| Risk | Mitigation |
|---|---|
| New stage breaks existing tasks | Opt-in `planMode` default off; off-path state machine unchanged + explicitly tested |
| ent enum change crashes existing DBs | Idempotent pre-migrate hook + seed-old-shape regression test (`ent_phantom_column_rebuild`) |
| New MCP tool panics on missing scope | Register in `ToolScopeMap` (auth.go) |
| Extra LLM pass (self-review) cost | Per-stage cheaper model via `pipeline_config`; self-review is one bounded pass |
| Reject loop never converges | `planIterationCap` (default 3) -> stays `awaiting_user` with latest plan |

## 10. Reuse Summary

Plan-mode is mostly assembled from existing seams: `awaiting_user` + `ResumeFromUser` (pause/resume), `refinementturn` + `approve_spec`/`Confirm` (turn storage + approval), `pipeline_config` (per-stage model + opt-in), `RefinementChat.vue` (panel), `SpawnFn` DI (tests). The genuinely new parts are: the `plan_review` stage enum + handler (generate->self-review->gate), the `approve_plan`/reject endpoints + MCP tools, and the plan-review UI column/panel.
