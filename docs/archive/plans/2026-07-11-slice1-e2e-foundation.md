# Slice 1 — E2E Foundation — Implementation Plan

> Spec: `docs/superpowers/specs/2026-07-11-desktop-app-e2e-design.md` (Approved, D1–D6). Slice 1 only.
> Branch: `feat/e2e-foundation` off `upcoming`. TDD-by-nature (specs ARE the tests). PR → `upcoming`, independent of the desktop slices.

## Goal
Make the Playwright suite actually runnable (it times out today), then raise coverage of the highest-risk Vue flows with stubbed-`/api` specs. The same SPA runs in the future wails webview, so this coverage transfers.

## T1 — Harness fix (the unblocker)
- Root cause: `playwright.config` `webServer.command = 'pnpm dev'` starts vite on `:5173`, but `webServer.port = 13120` → the wait never succeeds → every run times out (the #258 quirk).
- Fix (D3, option A): add `scripts/e2e-server.sh` that (a) builds the binary if `bin/agent-dashboard` is missing or older than any tracked source (`task build:all`), then (b) execs `bin/agent-dashboard serve`. Point `webServer.command` at it; set `reuseExistingServer: true` and a realistic `timeout` (~180s to allow a cold build).
- Keep `baseURL`/`port` at `13120`.
- Done when: `npx playwright test` boots the server itself and the EXISTING specs run (no manual pre-start). Fix any now-runnable existing spec that reveals a real staleness (mirror the #258 fix approach — a broken selector is a spec bug, not a harness bug).
- Note in the PR: this is the same server shape the wails desktop app runs at runtime.

## T2..Tn — High-value web specs (prioritized, incremental)
Each spec stubs `/api` at the browser level (`page.route`, mirroring `question-band.spec.ts` / `dashboard.spec.ts`). Reuse the `stubAuthDisabled` + agent-fixture helpers; extract shared helpers into `tests/e2e/helpers.ts` if duplication grows (DRY). One task per spec (or tight group), each verified by running just that spec against the T1 harness.

Priority order (stop Slice 1 at an agreed depth; the rest is a follow-up):

- **T2 — Agent detail modal**: open an agent card → modal opens; tab switching (Details / Waterfall / Terminal); close. Stub `/api/agents` + the per-agent endpoints the tabs call.
- **T3 — Tasks / pipeline board**: the kanban/pipeline view renders stubbed tasks in the right columns; open the task modal; the stage/status render. Stub the tasks API.
- **T4 — Settings**: API-key create flow → the reveal dialog shows the `claude mcp add` CLI-command block + copy; spawner select; pipeline-config read. Stub the settings/keys API.
- **T5 — Spawn flow**: the spawn form (extend/verify the existing `spawn-with-project.spec.ts` under the fixed harness) — pick project + spawner → POST asserted.
- **T6 — Workflows charts**: the four chart tabs render their empty-state/SVG (already partly in `workflows.spec.ts` — verify green under T1, extend if thin).
- **T7 — Refinement chat / cost analytics** (lower priority): basic render + one interaction each.

## Sequencing
- **T1 first and alone-verifiable** — nothing else can run until the harness boots. Land it, confirm the existing suite is green, then add specs.
- Specs T2→T7 are independent; add in priority order. Slice 1 can ship after T1 + an agreed batch (e.g. T2–T5); T6–T7 as a follow-up PR if scope tightens.
- OFD: main-thread-orchestrated, foreground subagents in the worktree, per-spec verify against the T1 harness, `--no-gpg-sign`, `pnpm i` in the worktree, lint 0 / typecheck clean, run the touched spec(s) green before commit. Restore `ent` if any Go build/test ran.

## Out of scope (Slice 1)
The wails shell, onboarding, webview audit, distribution (Slices 2–4). MSW migration + Playwright component testing (deferred cleanups).
