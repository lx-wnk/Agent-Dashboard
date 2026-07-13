# Frontend Restructure — Design Spec

> Date: 2026-07-12 · Status: Approved · Branch: `docs/audit-spec-roadmap` (off `upcoming`)
> Closes ARCH-P3-4, CQ-35, CQ-42 from `outputs/Findings-full-project-2026-07-12.md`. Ships AFTER `2026-07-12-a11y-completion-design.md`; own multi-PR effort, no user-facing urgency.

## Why

`src/components/` and `src/composables/` are flat directories holding 109 `.vue` files and 87 composables with no feature grouping — finding "everything that belongs to pipeline" or "everything that belongs to settings" requires a grep, not a directory listing. This is pure maintainability debt (no compliance/user-impact urgency, unlike the a11y spec), but it compounds: `ApiKeySettings.vue` alone is an 1087-line god component with 15 copy-pasted nav blocks (CQ-35), and ~20 composables (`useTaskDependencies`, `useTaskCostBreakdown`, `useTaskCoordination`, `useCostAnalytics`, etc.) have zero test coverage (CQ-42) partly because there's no natural per-feature boundary to backfill specs against.

## Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | ARCH-P3-4: move to `src/features/{agents,pipeline,plugins,settings,analytics}/` **incrementally, one feature per PR** (`agents` → `pipeline` → `plugins` → `settings` → `analytics`), not a single big-bang move | A big-bang PR moving 109+87 files is unreviewable and guarantees merge conflicts with every other in-flight PR for as long as it's open. One-feature-per-PR keeps each diff bounded and reviewable, and only that feature's files are locked during its move window. |
| D2 | Introduce the `@/features/...` **path alias in `vite.config.ts`/`tsconfig.json` before the first move PR**, not as part of it | Decouples the tooling change (alias resolution, IDE config) from the file-move change — if the alias breaks something, that's caught before any files have actually moved, isolating the failure to a config-only PR. |
| D3 | Move files with `git mv` (not delete + recreate), one feature directory at a time | Preserves git blame/history per file — a delete+recreate shows as 100% rewrite in `git log --follow` and loses history; `git mv` is tracked as a rename by git's similarity detection. |
| D4 | Import-boundary lint (feature dirs may not deep-import each other's internals; shared code lives in `src/components/ui/`, `src/utils/`, or a `src/shared/` composables bucket) is added **last**, after all five features have moved | Adding the boundary rule mid-migration would fail lint on the ~4 not-yet-moved features for imports that are about to become legal anyway; adding it last means it only has to be correct once, against the final tree. |
| D5 | CQ-35 (`ApiKeySettings.vue` `SECTIONS` array refactor) ships as its **own standalone PR**, independent of the restructure's sequencing | It's a same-file, same-directory refactor — no dependency on where the file eventually lives, so it doesn't need to wait for or be sequenced with the `settings` feature-move PR. Can land anytime. |
| D6 | CQ-42 composable specs are backfilled **per feature folder**, bundled into each feature's move PR (or an immediate follow-up), not as one separate 20-spec PR | Each move PR already forces a reviewer to look at that feature's composables; adding specs for the composables just relocated in the same PR keeps the "this feature is now owned/tested" unit coherent, rather than a disconnected spec-only PR touching files across all five features at once. |

## Scope

**In:** `@/features/...` path alias (config-only PR, first); five sequential move PRs (`agents`, `pipeline`, `plugins`, `settings`, `analytics`), each `git mv`-ing that feature's components + composables + specs into `src/features/<name>/{components,composables}/` and fixing relative imports to the alias; import-boundary ESLint rule added in a final PR after all five; `ApiKeySettings.vue` `SECTIONS`-array + `v-for` refactor (standalone, D5); ~20 composable specs for `useTaskDependencies`, `useTaskCostBreakdown`, `useTaskCoordination`, `useCostAnalytics`, and the remaining untested composables, backfilled per-feature (D6).

**Out:** A11Y-2/3/4/8/9/10 (separate spec, ships first); any behavior change to moved components/composables (pure relocation — logic changes, if needed, are separate PRs); a shared-code (`src/shared/`) reorganization beyond what's needed to host genuinely cross-feature composables/components; renaming components during the move (file path changes, not identifier changes, to keep diffs reviewable as renames).

## Architecture

### Path alias (`vite.config.ts`, `tsconfig.json` — first PR, config only)
- Add `'@/features': path.resolve(__dirname, 'src/features')` (or a single `'@': path.resolve(__dirname, 'src')` alias if not already present — verify current alias config before adding a narrower one) to both Vite's `resolve.alias` and TS's `compilerOptions.paths`, so `@/features/pipeline/composables/useTasks` resolves identically in the editor and the build. No files move in this PR — it's a no-op until the first feature PR starts using it.

### Feature move PRs (five, sequential)
Each PR, for one feature (e.g. `pipeline`):
- Identify every component/composable that belongs to the feature by current usage (e.g. `pipeline`: `PipelineBoard.vue`, `TaskCard.vue`, `SortableTaskList.vue`, `TaskModal.vue`, `useTasks.ts`, `useTaskDependencies.ts`, `useTaskCostBreakdown.ts`, `useTaskCoordination.ts`, etc. — exact membership determined at PR time by grepping cross-references, not fixed in advance here).
- `git mv src/components/PipelineBoard.vue src/features/pipeline/components/PipelineBoard.vue` (and so on) — preserves history per D3.
- Update every import site across the codebase (both inside and outside the moved feature) from the old relative path to `@/features/pipeline/components/PipelineBoard.vue` / `@/features/pipeline/composables/useTasks`.
- Components/composables genuinely shared across ≥2 features (e.g. `AppModal.vue`, `formatCost` from `src/utils/format.ts`) stay where they are (`src/components/ui/`, `src/utils/`) — the move only relocates feature-owned files, never the SSOT shared utilities already defined in `layer2-project-core.md`.
- Backfill composable specs for that feature's untested composables in the same PR (D6).
- Order (`agents` → `pipeline` → `plugins` → `settings` → `analytics`) chosen so the smallest/least-cross-referenced features move first, proving the pattern before tackling `pipeline` (the most cross-referenced — `PipelineBoard`/`TaskCard`/`TaskModal` are imported from many other components) and `analytics` (owns the D3 chart components already touched by the a11y spec's `ChartDataTable` work, so it should land after that spec merges to avoid rebasing chart files mid-a11y-PR).

### Import-boundary lint (last PR, after all five features moved)
- ESLint rule (e.g. `eslint-plugin-import`'s `no-restricted-paths`, or a custom rule if the project's ESLint config needs a project-specific zones setup) forbidding `src/features/<a>/**` from importing `src/features/<b>/internal-only paths` — components/composables intended for cross-feature use must be re-exported from a feature's `index.ts` or live in `src/components/ui/`/`src/utils/`/`src/shared/`.
- Added last because it can only be written correctly against the final five-feature tree; written earlier it would need constant updates as each feature migration changes what "internal" means.

### CQ-35 — `ApiKeySettings.vue` (standalone PR, any time)
- Extract the 15 copy-pasted nav blocks into a `SECTIONS: { id, label, icon }[]` const plus a `v-for` over it for the nav list.
- Split the corresponding 15 panel bodies into child components (e.g. `ApiKeySettingsPanel<Section>.vue` or a single generic panel component parameterized by section id, whichever keeps the diff smallest — decided at implementation time based on how much markup the panels actually share).
- No dependency on the feature-move PRs — this is a same-directory-today refactor; if it lands before the `settings` move PR, the move PR just relocates the already-split files; if after, the split happens in the new location.

### CQ-42 — composable specs (bundled into each move PR, D6)
- Target list (non-exhaustive, confirmed at PR time via `find src/composables -iname '*.ts' | grep -v test`): `useTaskDependencies`, `useTaskCostBreakdown`, `useTaskCoordination`, `useCostAnalytics`, and the remainder of the ~20 currently-untested composables identified in the audit.
- Each spec follows the project's existing Vitest composable-test conventions (no DOM, mock fetch/SSE inputs, assert computed outputs) — pattern lifted from whatever tested composable in the same feature area already has a `.test.ts` sibling, to stay consistent rather than inventing a new test shape per feature.

## Data flow

```
PR 0 (config only): vite.config.ts + tsconfig.json alias added → no file moves, no behavior change

PR 1 (agents):    git mv agents-owned files → src/features/agents/{components,composables}/
                  → fix imports repo-wide → backfill agents composable specs → merge
PR 2 (pipeline):  same, pipeline-owned files (largest cross-reference surface)
PR 3 (plugins):   same, plugins-owned files
PR 4 (settings):  same, settings-owned files (ApiKeySettings.vue already split if CQ-35 merged first)
PR 5 (analytics): same, analytics/chart-owned files (after a11y spec's ChartDataTable work is merged)

PR 6 (lint):      import-boundary ESLint rule added against the now-final src/features/ tree → merge

CQ-35 (standalone, any point): ApiKeySettings.vue SECTIONS refactor → independent PR
```

## Error handling

- Broken import after a `git mv`: caught immediately by `tsc --noEmit`/build failure in the same PR — not a runtime concern, a CI-gate concern; each move PR must pass typecheck + build before merge.
- Test file left behind (moved component but not its `.test.ts` sibling): caught by `pnpm test` failing to find the component at its old mocked path — move `.test.ts` files alongside their subject in the same `git mv` batch.
- Import-boundary lint false-positive on a legitimately shared file: resolve by promoting that file to `src/components/ui/`/`src/utils/`/`src/shared/` rather than adding a rule exception (keeps the boundary rule simple and exception-free).
- Mid-migration state (some features moved, some not) is expected and safe: unmoved features keep working via their existing relative-import paths; there is no partial-file-move failure mode since each PR is a complete unit (all of one feature's files, or none).

## Testing

- Each move PR: `pnpm typecheck` + `pnpm build` must pass (catches broken imports); `pnpm test` must pass with the same total spec count before/after (catches orphaned/misresolved test files); `pnpm lint` clean.
- CQ-42 specs: new composable specs run under the standard `pnpm test` / `pnpm test:watch` Vitest setup already used project-wide — no new test infra needed (unlike the a11y spec's axe-core addition).
- CQ-35: existing `ApiKeySettings.vue` coverage (if any spec exists) extended to assert the `SECTIONS` array drives both nav and panel rendering (nav item count === `SECTIONS.length`, clicking a nav item shows the matching panel) — regression guard that the extraction didn't drop a section.
- Import-boundary lint PR: a deliberately-added violating import in a throwaway test fixture (removed before merge) can verify the rule actually fires, then removed — standard "prove the linter catches the bad case" verification before shipping the rule.

## Risks

- **Mass `git mv` still churns history and every import path touching it** — even done incrementally, each move PR's diff is large by file-count (every importer of every moved file changes its import line). Mitigated by: one feature at a time (bounds the diff to that feature's blast radius, not all 109+87 files at once), `git mv` for rename-tracking, and CI (typecheck/build/test) as the safety net rather than manual review of every changed import line.
- **Big-bang was rejected specifically because** a single PR moving all files would (a) be unreviewable at that size, (b) block on every other in-flight PR touching any moved file for the PR's entire open duration, and (c) make a bad rename/import mistake far harder to bisect (which of 196 file moves broke the build?) versus a ~20-40-file-scoped PR.
- `pipeline` (PR 2) is the highest-cross-reference feature — expect the largest import-fix diff of the five; sequencing it after the smaller `agents` PR proves the process on a lower-risk feature first.
- `analytics` (PR 5) overlaps with the a11y spec's `ChartDataTable.vue` work on the same three D3 chart files — sequencing `analytics` after the a11y spec merges avoids a rebase conflict; if the a11y spec is delayed past when `analytics` would otherwise be scheduled, re-sequence `analytics` later rather than proceeding out of order.
- Composable specs backfilled per-PR (D6) means CQ-42's full ~20-spec completion is spread across five PRs over the restructure's full timeline, not delivered as one lump — acceptable since there's no single deadline forcing all 20 to land atomically, but worth tracking so a feature's move PR isn't merged without its share of specs "to save time."
