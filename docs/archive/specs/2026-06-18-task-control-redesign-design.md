# Task Refinement & Control Redesign — Design

> Status: approved design (brainstorming). Next: implementation plans per sub-project.
> Date: 2026-06-18. Scope: make the task refinement/control process simpler, intuitive, and fully MCP/API-controllable.

## Problem

Driving a task through the pipeline by an agent (or a human) is not "round". Friction observed in a real fleet-run session (queuing 5 tasks → PRs):

1. **Two parallel notions of "refined"** — free-text `task.description` vs the structured concept stored in `refinement_turns`/`task.Metadata`. A hand-written spec in `description` left the implementation stage seeing an empty `{}` concept block.
2. **Refinement is REST-only** — the entire `/api/refine/*` chat (turn/confirm/status) has **no MCP tools**, so an MCP-driven agent cannot refine or approve a task.
3. **Lifecycle control is a scavenger hunt** — `confirm` vs `progress` vs `retry` vs `resume` vs `grant`, each valid only in a hidden state; callers (especially agents) cannot tell which applies. The concept stage silently parks on `awaiting_user` ("Refinement chat in progress") until an undocumented `/confirm`.
4. **Permission stalls** — fresh tasks die on `awaiting_user reaper: stage agent exited while permissions pending`. Rescue required 11 rate-limited grant calls per task, and broad grants trip the auto-mode safety classifier.

## Decisions (locked)

1. **Unified state machine; UI and MCP are thin clients** over one operation set.
2. **Refinement = hybrid draft + amend**; the structured concept is canonical; `description` is the seed/intent.
3. **Lifecycle control = `availableActions[]` on every read + an `advance` convenience verb.**
4. **Permissions controllable over MCP**, including one-call "allow everything" and runtime rescue.
5. **Single `autonomy` knob** with three levels presetting permission behavior + stage gates.

## Section A — Unified lifecycle + self-describing control (spine)

Task state = derived tuple: `stage` (backlog→concept→implementation→self_review→finalization→done, + cancelled/on_hold) × `runStatus` (idle/running/failed/awaiting_user/requeued) × `autonomy` × `blockedBy` (pending perms / pending approval).

- **`availableActions[]`** on every `get_task`/`list_tasks` and the SSE payload. Each entry `{ action, enabled, reason, primary }`; disabled actions carry a human reason. Computed by ONE server-side `computeActions(task)` — UI renders exactly these buttons; an MCP agent reads the same array. No guessing.
- **`advance(task)`** executes the current `primary` action whatever the state (draft spec → approve → run → review → PR). Symmetric `retry` (re-run current stage) and `back` (return a stage with feedback).
- **Verb set, all REST + MCP:** `refine` · `approve_spec` · `advance` · `retry` · `back` · `cancel` · `hold`/`resume` · `approve_all_pending` · `open_pr`.
- `confirm`/`progress`/`resume` collapse into internal transitions behind `advance`/`approve_spec`/`retry`.

## Section B — Refinement: draft + amend, one canonical concept

- **Canonical artifact `task.concept`** = `{ spec, plan, toolRequests, refinedTitle, sourceBranch, targetBranch }`. `description` is the seed the refiner reads. Implementation/review/finalization prompts always read `task.concept` (with `description` as context).
- **Flow:** `refine(task)` → refiner agent reads code + description → emits a draft `task.concept` in one pass (`refineStatus: draft_ready`, no chat). Optional `refine(task, amend:"…")` revises in place (turns kept as history; concept is source of truth). `approve_spec(task)` freezes `concept` and advances to implementation (today's `confirm`, renamed, made the only approval verb).
- **Honest `refineStatus`:** `none → refining → draft_ready → approved` (or `failed` + error) — tracks the concept's real lifecycle (today the flag is decoupled from content).
- **Human/agent-authored escape hatch:** `refine(task, concept:{…})` injects a finished concept directly → `draft_ready`, skipping the agent. First-class path (replaces the description-field hack).
- **MCP parity:** `refine`, `approve_spec`, `get_refine_status` become MCP tools (today: none).

## Section C — Autonomy: one knob, three levels

Single `task.autonomy`, set at `create_task`/`update_task` (REST + MCP). Presets permission behavior AND stage gates.

| Level | Permissions | Approval gates | Use |
|---|---|---|---|
| `manual` | gated — every request prompts | approve every stage transition | hands-on, untrusted |
| `spec-gated` (default) | allow-all (seeded at create) | approve the spec once → run to PR | common case |
| `full` | allow-all | none — agent self-approves spec, runs to PR | autonomous fleet |

- **allow-all** is seeded atomically at create (or on level change) as a task permission with `mode=allow-all` — one DB write, not N rate-limited calls. The permission gate checks `autonomy==allow-all` → auto-approves every `request_permission`. The `awaiting_user reaper` stall cannot happen at `spec-gated`/`full`.
- **Runtime rescue:** `approve_all_pending(task)` — one MCP/REST call clears pending requests / bumps a stuck `manual` task. No grant loop.
- **Safety:** allow-all/full is explicit per-task opt-in; every auto-approve is audit-logged; `git push` stays scoped to the task worktree branch. Default `spec-gated` keeps a human at the spec gate. (allow-all over API is the elevation the safety classifier flags — owned by the operator on a local tool.)
- `availableActions[]` reflects the level: `manual` shows `approve_stage`; `spec-gated` shows only `approve_spec`; `full` shows none.

## Section D — MCP/REST surface (parity over one service layer)

Every lifecycle op is one service-layer function; REST route and MCP tool are thin adapters over it (no logic in handlers → no drift). `computeActions` lives here.

- **Reads** (✏️ enriched with `availableActions[]`, `concept`, honest `refineStatus`): `list_tasks` `get_task` `list_stage_runs` `list_audit` `list_permission_requests` `list_projects` `list_spawners`.
- **Create/edit:** ✏️ `create_task` (+`autonomy` default `spec-gated`, optional `concept` inject) · ✅ `update_task` `delete_task` `add_dependency` `remove_dependency`.
- **Refinement (new over MCP):** ➕ `refine_task` (draft / `amend` / `concept` inject) · ➕ `approve_spec` · ➕ `get_refine_status`.
- **Control:** ➕ `advance_task` · ➕ `back_task` · ➕ `hold_task`/`resume_task` · ✅ `retry_task` `cancel_task` · ✏️ `progress_task` → deprecated alias of `advance_task` (removed next major).
- **Permissions:** ✅ `grant_permission` `resolve_permission_request` · ➕ `approve_all_pending` · autonomy=allow-all via `create/update_task`.
- **`manage_task`** (existing umbrella) becomes the batch front-end: create+set-autonomy+inject-concept+approve in one call for fleet spawns.

Agent loop becomes: `get_task` → read `availableActions` → call `primary` (or `advance_task`), repeat.

## Section E — Rollout, compat, testing

Five sub-projects, each its own spec→plan→PR, in build order:

1. **Service layer + `availableActions`** (foundation) — extract control logic into one service package, add `computeActions`, surface `availableActions[]` in reads/SSE. Additive, no behavior change.
2. **Autonomy + allow-all gate** — `autonomy` field, atomic allow-all seed, gate honors it, `approve_all_pending`. Highest immediate value (kills stall pain). Independent of 3–5.
3. **Refine redesign** — canonical `task.concept`, `refine_task`/`approve_spec`, honest `refineStatus`, concept inject. Depends on 1.
4. **Verb set + MCP parity** — `advance`/`back`/`hold`/`resume`, `progress_task` alias, all refine+control verbs as MCP tools over the shared service. Depends on 1+3.
5. **UI thin-client** — render `availableActions`, autonomy selector, concept editor/diff. Depends on 1–4.

**Compat/migration:** `description` stays (seed) → no data loss; `concept` backfills lazily; `progress_task` deprecated alias; `refineStatus` enum migrated (`idle`→`none`, `failed` preserved); pipeline stage names unchanged.

**Testing:** `computeActions` table tests (state-tuple → actions incl. blocked+reasons); autonomy gate (allow-all auto-approves; manual stalls→`approve_all_pending` clears); refine draft/amend/inject→`approve_spec`; MCP↔REST parity golden test per verb; E2E `create(autonomy=spec-gated)` via MCP → refine → approve → unattended to PR.
