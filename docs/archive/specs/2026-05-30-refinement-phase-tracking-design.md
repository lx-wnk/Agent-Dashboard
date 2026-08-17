# Refinement phase tracking

**Date:** 2026-05-30
**Branch:** `feat/layout-redesign`
**Status:** Approved (design) — pending implementation plan
**Builds on:** `2026-05-30-refinement-progress-and-persistence-design.md` (the `refine.Runner` introduced there owns persistence and is where phase parsing lands).

## Problem

The refinement chat is meant to guide a task through four phases —
`analysis → spec → implementation_plan → approval` — but the backend never
signals phase progression. The frontend has a half-built convention for it:

- `PHASE_DONE_RE = /__phase_done:\s*\w+/g` strips an inline `__phase_done: <phase>`
  marker from displayed content (`cleanContent` in `RefinementChat.vue`).
- `PHASE_LABELS`, `phaseLabel`, `isPhaseMarker`, the `.phase-marker` CSS divider,
  and `approvalReady` are all present.

But nothing ever **parses** the marker into phase state, and the refine prompt
never asks the agent to emit it. So the marker is silently discarded, the
in-chat phase dividers never appear, and the Confirm button's `approvalReady`
gate is never driven by the agent. `submitTurn`/`Runner` persist the assistant
turn with `phase = nil`.

## Goals

- The refine agent declares phase completion; the dashboard shows progress both
  as in-chat "✓ <phase> complete" dividers and as a compact 4-step stepper in
  the concept-step `RefineStatusPanel`.
- Phase state is persisted on the turn, so it survives chat close/reopen and
  page reload (consistent with the just-shipped server-tracked refinement).
- Reaching `approval` enables the existing Confirm-to-backlog flow.

## Non-goals

- No new phase transport: phases ride the existing `__phase_done:` marker inside
  the normal refinement output stream — no separate SSE event type, no schema
  change (the `refinement_turns.phase` column already exists).
- No enforcement that the agent visits phases in strict order or visits all of
  them; the dashboard reflects whatever the agent declares.
- No change to the confirm/advance mechanics beyond what `approvalReady` already
  drives.

## Phase model (SSOT)

Canonical ordered phase keys, defined once on each side:

- Backend: `refine.Phases = []string{"analysis", "spec", "implementation_plan", "approval"}` (in `server/internal/refine/`), plus a `phaseDoneRE` regexp matching `__phase_done:\s*(\w+)`.
- Frontend: the existing `PHASE_LABELS` map keys MUST match these four strings
  exactly. A `PHASE_ORDER` array (the four keys in order) is added for the
  stepper.

## Architecture

The agent emits a line `__phase_done: <phase>` when it finishes a phase. The
marker travels inside the normal refinement output. The `refine.Runner` (the
single owner of persistence) parses markers from the accumulated output: it
records the phase on the persisted assistant turn and strips every marker from
the persisted content. The frontend parses the same marker live while streaming
(for the open chat) and reads `turn.phase` on reload (for reopen). The stepper
is derived from the set of completed phases.

### 1. Prompt template (`server/internal/refine/spawner.go` `promptTmpl`)

Extend the `<system>` block to instruct the assistant: work the task through the
phases analysis → spec → implementation_plan → approval; when a phase is
complete, output a line on its own containing exactly `__phase_done: <phase>`
using one of the four canonical keys; when the task is ready for the user to
confirm, output `__phase_done: approval`. Keep markers terse and machine-readable.

### 2. Runner phase parsing (`server/internal/refine/runner.go`)

In the goroutine's persist step (currently: trim output, classify empty/error/ok,
`Turns.Create(role=assistant, content=resp)`):

- Scan `resp` for `phaseDoneRE` matches. Collect valid phases (those in `Phases`);
  ignore unknown captures.
- `lastPhase` = the last valid match (or none). Strip ALL `__phase_done: …` marker
  occurrences from `resp` before persisting (so stored content is clean; the
  frontend's display-time strip becomes belt-and-suspenders).
- Persist with `repo.CreateTurnInput{ … Content: cleaned, Phase: &lastPhase }`
  when a valid phase was found; otherwise `Phase: nil` as today.
- The failure/empty branches are unchanged (no phase on a failed run).

A new package-level helper `extractPhases(s string) (cleaned string, phases []string)`
keeps the parsing testable in isolation.

### 3. Frontend live parse (`src/composables/useRefinementChat.ts` `sendMessage`)

Today the SSE loop sets phase only on an `event: phase_change` frame (never emitted
by the backend). Add: for each streamed `data:` line, if it matches `__phase_done: <phase>`
(reuse a shared regex) and the phase is one of `PHASE_LABELS`, then
`completedPhases.value.add(phase)`, set the current assistant message's `phase`,
and set `approvalReady` when phase is `approval`. The marker is already stripped
from displayed text via `cleanContent`; ensure the accumulated `assistantContent`
also excludes marker lines (strip before appending, or skip marker-only frames).

### 4. Reopen (already wired, now effective)

`applyTurnsToMessages` already maps `t.phase` onto each message, adds non-null
phases to `completedPhases`, and sets `approvalReady` when `approval` is present.
No change needed — it becomes correct now that the Runner persists `phase`.

### 5. `RefineStatusPanel` stepper (`src/components/RefineStatusPanel.vue`)

Add a compact horizontal 4-step indicator below the status badge: one node per
`PHASE_ORDER` key, each in state done (✓) / current / pending. The panel gains a
prop `completedPhases: string[]`. "Current" = the first phase not in
`completedPhases` (while `status === 'running'`); when `status === 'done'`/`failed`,
no node is highlighted as current. The stepper renders only when at least one
phase is complete OR status is running.

### 6. `TaskModal` wiring (`src/components/TaskModal.vue`)

`TaskModal` already fetches `/api/refine/{id}/turns` for `lastRefineOutput`.
From the same turns, derive `completedPhases` (collect every non-null `phase`)
and pass it to `RefineStatusPanel`. Re-derived on the existing `refineStatus`
watch, so it advances when a run finishes.

## Data flow

```
agent output contains "__phase_done: spec"
  → Runner.persist: strip marker, set turn.phase="spec", persist clean content
  → status broadcast (refineStatus) over task SSE
  → TaskModal re-fetches /turns → derives completedPhases=["analysis","spec"] → stepper advances
Open chat (live): sendMessage sees "__phase_done: spec" frame
  → completedPhases.add("spec"), message.phase="spec" (in-chat "✓ Spec complete" divider), approvalReady on "approval"
Reopen / reload: GET /turns returns turns with phase set → applyTurnsToMessages rebuilds completedPhases + dividers + approvalReady
```

## Error handling

- Unknown phase capture (not in `Phases`/`PHASE_LABELS`) → ignored on both sides.
- Multiple markers in one turn → backend sets `turn.phase` to the LAST valid
  match and strips all; frontend adds all valid captures to `completedPhases`.
- Failed/empty refinement run → no phase persisted (unchanged).
- A marker mid-line vs on its own line: the regex matches the token anywhere;
  stripping removes the matched token text. The prompt asks for own-line markers,
  but parsing tolerates inline.

## Testing

### Backend (`server/internal/refine/runner_test.go`)
- `extractPhases`: `"work…\n__phase_done: spec\nmore"` → cleaned has no marker,
  phases == `["spec"]`. Multiple markers → all captured, all stripped, order
  preserved. Unknown phase (`__phase_done: bogus`) → not captured. No marker →
  empty.
- `Runner.Start` with a stub stream emitting a `__phase_done: spec` line → the
  persisted assistant turn has `Phase == "spec"` and `Content` without the marker.
  (Extend `fakeTurns` to capture the `Phase` pointer.)

### Frontend
- `useRefinementChat`: a streamed `data: __phase_done: approval` frame sets
  `approvalReady` true and adds `approval` to `completedPhases`; the marker text
  does not appear in the rendered assistant message content.
- `RefineStatusPanel`: renders done/current/pending node states for a given
  `completedPhases` + `status` combination.

## Implementation phasing (single spec)

- **Phase A:** backend — `Phases`/`phaseDoneRE` SSOT, `extractPhases`, Runner sets
  `turn.phase` + strips markers; prompt template update.
- **Phase B:** frontend — live marker parse in `sendMessage`; `RefineStatusPanel`
  stepper + `completedPhases` prop; `TaskModal` derives + passes `completedPhases`.
