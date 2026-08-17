# Unify Task Creation — Design

**Date:** 2026-06-01
**Status:** Approved for planning
**Area:** Pipeline view header (`src/App.vue`), `BacklogForm.vue`, `RefinementChat.vue`

## Problem

The pipeline view exposes two creation buttons with overlapping purpose:

- **+ New Task** → opens `RefinementChat` (AI chat). Standalone-create path creates a
  task at the `concept` stage and drives its refinement stream.
- **+ Backlog** → opens `BacklogForm`, a two-step wizard (ProjectStep → DetailsStep)
  that creates a task at the `backlog` stage.

Both paths converge on the same `createTask()` call and both contain their own
project picker, so the only real differences are (a) the starting pipeline stage and
(b) the UX surface. The two-button split and the Backlog wizard's multi-step feel are
the pain points. The project-picker logic is duplicated three times (BacklogForm's
ProjectStep, DetailsStep's folder hydration, and RefinementChat's inline picker).

## Goal

Collapse to a **single creation entry point** with a single-screen form, an optional
"refine with AI" follow-up, and one canonical starting stage. Remove the resulting
dead code and duplication.

## Decisions

1. **Single button.** Drop `+ Backlog`. Keep one `+ New Task` button that opens the
   unified create modal.
2. **Single-screen form.** Rework `BacklogForm.vue` into one screen — no Next/Back
   wizard. The two-step `ProjectStep`/`DetailsStep` split is removed for this flow.
3. **Single starting stage: `backlog`.** Both submit actions create the task at the
   `backlog` stage. Refinement is a post-create action, not a separate birth path.
4. **Two submit actions:**
   - **Create** → `createTask({ stage: 'backlog' })` → close modal → `selectTask(task)`.
   - **Create & Refine** → `createTask({ stage: 'backlog' })` → close modal → open
     `RefinementChat` bound to the new task (reuses the existing kanban
     `@open-chat`-with-task path). The chat's own confirm/promote logic moves the task
     forward (to `concept`) as it refines.
5. **Remove RefinementChat standalone-create path.** Once `openNewTask` is gone, the
   null-task create branch (inline project picker + `createTask` at concept) is dead.
   Delete it. `RefinementChat` becomes "always opened with an existing task." This
   removes the third duplicate project picker.

## Unified Form Layout

```
Project: [ dropdown ▼ ]  [+ new]    ← reuses QuickCreateProjectPanel
Title:    [____________________]
Slug:     [__________] (auto-derived from title, editable)
Cwd:      [____________________]   ← auto-filled from project folder suggestion
Description (optional, textarea)
Priority · Permission template · Spawner
──────────────────────────────────────
            [ Create ]    [ Create & Refine ]
```

- Project dropdown selection auto-fills `cwd` via the existing `suggestFolders`
  composable (same behavior DetailsStep has today on mount, moved to a project-change
  watcher).
- `+ new` project uses the existing `QuickCreateProjectPanel`.
- Validation unchanged: Title, Slug, Cwd required; both submit buttons disabled until
  valid.
- All existing fields preserved: priority, permission template, spawner, description.

## Components & Data Flow

| Component | Change |
|---|---|
| `src/App.vue` | Remove `+ Backlog` button + `openBacklogForm`. Rename/repoint `openNewTask` to open the unified create modal. Wire **Create** → `selectTask`; **Create & Refine** → set `activeConceptTask = task` + `showRefinementChat = true` (existing refs). Remove the `activeConceptTask = null` assignment from the create entry. |
| `BacklogForm.vue` | Becomes the unified single-screen form. Project dropdown + folder→cwd hydration inline (absorb DetailsStep fields + ProjectStep's project select into one screen). Emits `created` (Create) and `createdAndRefine` (Create & Refine), each carrying the new task. |
| `backlog/DetailsStep.vue` | Deleted (its fields fold into the unified form). |
| `backlog/ProjectStep.vue` | No longer used by BacklogForm. **Keep the file** — still imported by `SpawnDialog.vue`. Only remove the BacklogForm usage. |
| `RefinementChat.vue` | Remove the null-task standalone-create branch (inline project picker, `cwd-project-select`, the `createTask` call, `cwdError`, related project/spawner refs that are only used by that branch). Now always receives a non-null `task`. |

Both submit actions call the same `createTask()`; the only difference is what App.vue
does with the returned task afterward.

## Stage / Refinement Note (verify during implementation)

Today `RefinementChat` refines tasks at the `concept` stage. With this design,
"Create & Refine" opens the chat on a **`backlog`**-stage task. The kanban
`@open-chat` path can already open chat on existing tasks, but the implementer MUST
verify that:

- Opening `RefinementChat` on a `backlog`-stage task renders and streams correctly.
- The chat's confirm/promote flow advances a `backlog` task to `concept` (or the
  appropriate next stage) rather than assuming the task already sits at `concept`.

If the confirm flow assumes `concept`, add a stage promotion (`backlog` → `concept`)
at the point the refinement chat is opened or confirmed. Resolve this in the
implementation plan before coding the Create & Refine wiring.

## Testing

- **`BacklogForm.test.ts`** — rewrite for the single-screen form: project select
  auto-fills cwd, slug auto-derives from title, validation gating, `created` emitted
  with a backlog task.
- **New test** — "Create & Refine emits `createdAndRefine` with the new task" and
  App-level: it opens RefinementChat bound to that task.
- **RefinementChat tests** (`RefinementChat.createflow.test.ts`,
  `RefinementChat.cwd.test.ts`) — these cover the standalone-create path being
  removed. Delete or repurpose them to assert RefinementChat requires a task.
- Run `pnpm test` and `pnpm typecheck`; update E2E selectors if any reference the
  `+ Backlog` button or the two-step wizard (`details-step`, `cwd-project-select`).

## Out of Scope (YAGNI)

- No change to the pipeline state machine beyond the verified backlog→concept
  promotion above.
- No change to `SpawnDialog` beyond leaving `ProjectStep` intact for its use.
- No new project-picker abstraction — reuse what `QuickCreateProjectPanel` /
  `suggestFolders` already provide.

## Risks

- **Refinement on a backlog task** — primary risk; mitigated by the explicit verify
  step above.
- **Test churn** — three test files touch the removed paths; expected and bounded.
