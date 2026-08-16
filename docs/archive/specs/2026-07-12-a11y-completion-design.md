# A11y Completion — Design Spec

> Date: 2026-07-12 · Status: Approved · Branch: `docs/audit-spec-roadmap` (off `upcoming`)
> Closes WCAG 2.1 AA gaps A11Y-2/3/4/8/9/10 from `outputs/Findings-full-project-2026-07-12.md`. Ships now, 1-2 PRs, independent of the frontend restructure.

## Why

Six WCAG gaps block keyboard-only and low-vision users today: drag-only task reordering, mouse-only chart data, a hand-rolled onboarding dialog without a focus trap, invisible sidebar labels for keyboard users, low-contrast text, and sub-24px touch targets. All six are small and self-contained — most are "adopt a primitive the app already has" rather than new design work (`AppModal.vue` already implements focus-trap/scroll-lock/restore-focus/Escape; `SpawnTreeChart.vue` already proves the accessible-disclosure-list interaction model). None require the `src/features/` restructure and should not wait for it.

## Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | A11Y-2: keyboard reorder via **Move up / Move down buttons** on `TaskCard.vue`, not a full APG reorderable-listbox re-architecture | `SortableTaskList.vue` already calls `reorderTask(id, beforeId, afterId)` on drag drop — buttons call the same function with adjacent-item ids, zero new server surface. Full listbox roving-tabindex pattern is more code for the same outcome. |
| D2 | A11Y-3: build one shared `<ChartDataTable>` component (visually-hidden by default, toggle to reveal) fed by each chart's existing computed rows, instead of per-chart tables or copying SpawnTreeChart's disclosure-list markup into 3 D3 charts | SpawnTreeChart's pattern is bespoke to session-tree data; Sankey/DAG/CoOccurrence have different row shapes (link/matrix/node). A table primitive normalizes any tabular projection of chart data without re-deriving three different disclosure-list layouts. |
| D3 | A11Y-4: migrate `OnboardingFlow.vue` to `<AppModal size="auto">` in **one atomic PR** — delete the hand-rolled `role="dialog"` div, the manual `window.addEventListener('keydown', onKeydown)` Escape handler, and `previouslyFocused`-equivalent logic entirely; keep the custom header/3-section-body/footer markup verbatim inside the single default slot | A prior attempt was reverted: a partial edit left an unused import and stripped the focus lifecycle without replacing it. `size="auto"` is a transparent passthrough (no forced chrome), so the existing header/footer markup needs zero visual change — only the outer wrapper changes. Doing it as one PR removes the failure mode (no half-migrated state possible: either the file imports and uses `AppModal` and the old dialog div is gone, or it doesn't touch it at all). |
| D4 | A11Y-8: CSS-only `:focus-visible` tooltip on `NavItem.vue`, no JS | Matches existing hover-tooltip pattern already used for collapsed sidebar icons; keyboard users get the same disclosure sighted mouse users get on hover, for free. |
| D5 | A11Y-9: bump `PipelineBoard.vue`'s needs-you column text/background pair to meet 4.5:1 (reuse an existing `warning`/`danger` design-token pair, not a new color) | Design tokens are the canonical color source (layer2 SSOT rule) — no new hex values. |
| D6 | A11Y-10: bump icon-only control hit areas to ≥24×24 via padding, not icon size | Keeps visual icon size in the design system; only the clickable/tappable box grows (WCAG 2.2 AA target-size, advisory today but cheap to fix now). |

## Scope

**In:** keyboard reorder on `TaskCard.vue`/`SortableTaskList.vue`; new `src/components/charts/ChartDataTable.vue` wired into `SankeyChart.vue`, `SessionDagChart.vue`, `CoOccurrenceMatrix.vue`; `OnboardingFlow.vue` → `AppModal` migration; `NavItem.vue` focus-visible tooltip; `PipelineBoard.vue` needs-you contrast fix; icon-only control padding audit across `src/components/`; axe-core + vitest-axe test harness (net-new — none exists today).

**Out:** ARCH-P3-4 restructure, CQ-35, CQ-42 (separate spec — `2026-07-12-frontend-restructure-design.md`); full APG reorderable-listbox pattern (D1); a general-purpose chart-interaction redesign beyond data-table access; automated axe CI gate on the whole app (start with targeted component assertions, expand later).

## Architecture

### A11Y-2 — keyboard reorder (`src/components/TaskCard.vue`, `src/components/SortableTaskList.vue`)
- `TaskCard.vue` gains two icon buttons (`aria-label="Move task up"` / `"Move task down"`), rendered next to the existing `.task-drag-handle`, disabled at list boundaries (first item has no "up", last has no "down" — computed from a new `isFirst`/`isLast` prop passed down from `SortableTaskList`).
- `SortableTaskList.vue` passes `isFirst`/`isLast` per row (index-derived from `list.value`) and adds `moveTask(taskId, direction)`, which computes the same `beforeId`/`afterId` pair the drag-drop `onEnd` handler already computes for the adjacent swap, then calls the existing `reorderTask(id, beforeId, afterId)` from `useTasks`. No new composable, no new API — reuses the drop handler's id-resolution logic (extract into a small shared helper local to the file to avoid the two call sites drifting).

### A11Y-3 — `ChartDataTable.vue` (`src/components/charts/ChartDataTable.vue`, new)
- Props: `caption: string`, `columns: { key: string, label: string }[]`, `rows: Record<string, string | number>[]`.
- Renders a real `<table>` with `<caption>`, `<th scope="col">` per column, one `<tr>` per row — visually hidden by default (`sr-only` utility, consistent with existing visually-hidden patterns in the codebase) with a visible "Show data table" toggle `<button>` per chart that un-hides it (matches the disclosure-affordance precedent set by `SpawnTreeChart`'s expand/collapse, without copying its markup).
- Each chart derives its own `columns`/`rows` from data it already computes for D3 rendering (e.g. `SankeyChart.vue` already builds node/link arrays for the D3 layout — project the same arrays into table rows, no new data fetch):
  - `SankeyChart.vue`: columns `[source, target, value]`, one row per link.
  - `SessionDagChart.vue`: columns `[node, parent, tool count, cost]`, one row per node.
  - `CoOccurrenceMatrix.vue`: columns `[tool A, tool B, co-occurrence count]`, one row per non-zero cell.
- Each chart renders `<ChartDataTable>` once, positioned adjacent to (not replacing) the D3 canvas.

### A11Y-4 — `OnboardingFlow.vue` → `AppModal`
- Replace the outer `<div v-if="open" role="dialog" aria-modal="true" ...>` wrapper with `<AppModal :open="open" size="auto" labelled-by="onboarding-title" @close="skip">`.
- Delete: the manual `onKeydown`/`onMounted`/`onUnmounted` Escape-listener trio (lines 133-138 in the current file) — `AppModal` already binds `@keydown.escape="emit('close')"` on its own root.
- Delete: the outer `bg-black/60 ... backdrop-blur-sm` overlay div and its `@click.self="skip"` — `AppModal`'s `Teleport`-rendered root already provides the overlay, backdrop, `@click.self="emit('close')"`, focus-trap (`trapFocus`), scroll-lock, and focus-restore.
- Keep verbatim, unindented change only for the wrapper: the inner `bg-card border ... rounded-2xl` panel div, `<header>`, the three `<section>` steps, and `<footer>` — these become the single slotted content of `AppModal`. `size="auto"` means `AppModal` adds no forced chrome/width, so the existing `max-width: 640px` inline style on the panel div is preserved as-is.
- `emit('close')` already matches `AppModal`'s `close` event name — no event renaming needed at the call sites (`skip`, `finish`) already used.

### A11Y-8 — `NavItem.vue`
- Existing hover-only tooltip (collapsed-sidebar label, shown via `:hover`) gets a duplicate CSS trigger: `.nav-item:focus-visible .nav-tooltip { opacity: 1; visibility: visible; }` alongside the existing `.nav-item:hover .nav-tooltip`. No template/script change — pure CSS addition.

### A11Y-9 — `PipelineBoard.vue`
- Needs-you column's text/background utility classes swapped for the existing `warning-text`/`bg-warning`-family tokens already used elsewhere (e.g. the same tokens `TaskCard.vue`/`AgentModal.vue` use for warning states) — verify computed contrast ≥4.5:1 with the actual token hex values before picking the pair; if none of the existing warning tokens clear 4.5:1 against each other, escalate to design-token definition (not a new one-off hex).

### A11Y-10 — icon-only controls
- Audit pass over icon-only `<button>` elements below 24×24 (the OnboardingFlow copy-icon buttons at `p-1.5` are one instance already in scope from D3's file); apply a shared min `padding`/`min-width`/`min-height` utility class rather than per-button one-offs, so the fix is grep-able and consistent.

## Interaction flow

```
Keyboard reorder:
  TaskCard "Move up" click → SortableTaskList.moveTask(id, 'up')
    → resolve adjacent beforeId/afterId → reorderTask(id, beforeId, afterId)
    → optimistic rank update (existing composable behavior) → list re-renders

Chart data table:
  Chart mounts → derives rows/columns from existing D3 data → renders <ChartDataTable> (sr-only)
  User activates "Show data table" → table becomes visible → screen reader / sighted user reads rows directly

OnboardingFlow open:
  props.open → true → AppModal watch(open) fires
    → body scroll lock + capture previously-focused element + focus modal panel
  User presses Tab repeatedly → AppModal.trapFocus cycles focus within the slotted content
  User presses Escape / clicks backdrop → AppModal emits 'close' → OnboardingFlow's existing skip() handler runs (unchanged)
  AppModal watch(open) fires false → scroll unlock + focus restored to pre-open element
```

## Error handling

- A11Y-2: `moveTask` at a list boundary is prevented at the UI layer (button `disabled`, not a runtime guard) — no boundary error surfaces to `reorderTask`.
- A11Y-2: if `reorderTask` rejects (network/API error), it already surfaces via the existing toast path used by drag-drop today — keyboard path reuses the identical call, so error handling is inherited, not reimplemented.
- A11Y-3: charts with zero rows render `<ChartDataTable>` with an empty-state row ("No data") rather than omitting the table, so the toggle button never opens to a blank panel.
- A11Y-4: if `AppModal`'s `trapFocus` finds zero focusable elements (e.g. transient loading state inside a section), it no-ops safely (existing `AppModal` behavior, unchanged) — no dead-focus lock.

## Testing

- **New harness (PR 1):** add `axe-core` + `vitest-axe` (or `jest-axe` under Vitest, whichever resolves cleanly against the current Vitest/jsdom version — verify in a spike commit before wiring broadly) as dev dependencies; add a `expect(await axe(container)).toHaveNoViolations()` helper used per-component, not a blanket app-wide scan (avoids false positives from partially-mocked global chrome).
- **Assert axe-clean on:** `OnboardingFlow.vue` (open state), `AppModal.vue` (open state, standard and auto size), `SortableTaskList.vue` (with the new move buttons), `ChartDataTable.vue` (with sample rows, both hidden and revealed states), `NavItem.vue` (collapsed state).
- **A11Y-2:** component spec — clicking "Move up"/"Move down" calls `reorderTask` with the expected adjacent ids; boundary buttons render `disabled`; keyboard `Enter`/`Space` on the buttons behaves identically to click (native `<button>` semantics, assert not reimplemented).
- **A11Y-3:** `ChartDataTable` unit spec — renders one `<tr>` per row, `<th scope="col">` per column, caption present; each chart's existing spec (if any) gets an added assertion that the table receives the same row count as the chart's computed data.
- **A11Y-4:** existing `OnboardingFlow` test coverage (if a spec file exists, extend it; if not, add one) asserting: focus moves into the panel on open, Tab wraps at the panel boundary (not the whole document), Escape closes via `skip()`, and no dangling unused imports remain (covered by lint, not a runtime test — call out explicitly since that's exactly what the reverted attempt missed).
- **A11Y-9:** a computed-contrast unit check (plain JS, no DOM) against the actual token hex values, asserting ≥4.5:1 — cheap regression guard against a future token rename silently breaking contrast.

## Risks

- `vitest-axe`/`jest-axe` version compatibility with the project's current Vitest major version is unverified — spike this first before writing all component assertions; if incompatible, fall back to a manual axe-core `run()` call wrapped in a small local helper instead of the wrapper library.
- A11Y-4 is the highest-care item given the prior partial revert: the fix must land as one PR with the old dialog markup and its Escape/focus-restore logic *fully deleted*, not left dormant beside the new `AppModal` usage. Lint (`no-unused-vars`) plus the extended test spec (asserting `AppModal`-sourced focus-trap behavior, not the old listener) are the two guardrails against a repeat.
- A11Y-9's contrast fix depends on the existing warning/danger token pair actually clearing 4.5:1 — if it doesn't, this item grows from a CSS class swap into a token-definition change, which should be flagged back before merge rather than shipped non-compliant.
- ChartDataTable's row projection must stay in sync with each chart's D3 data shape by construction (same computed source) — if a future chart data-shape change forgets to update its table columns, that's a silent a11y regression with no compile-time guard; the axe assertion catches missing/malformed tables but not stale columns, so a table row-count assertion per chart (as noted under Testing) is the practical backstop.
