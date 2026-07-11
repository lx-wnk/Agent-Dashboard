# Slice 3 — First-Run Onboarding — Implementation Plan

> Spec: `docs/superpowers/specs/2026-07-11-desktop-app-e2e-design.md` (Approved, D4), Slice 3.
> Branch: `feat/onboarding-slice3` off `upcoming`. PR → `upcoming`. SPA-level (works in browser AND desktop webview).
> Decision (2026-07-11): step (b) MCP registration = **auto (`claude mcp add` server-side) + copy-command fallback**, both from the shared `buildMcpAddCommand` foundation.

## Goal
A guided first-run screen (the Sightdeck counter): one flow that (a) verifies the Claude CLI, (b) connects the dashboard to Claude via MCP with one click, (c) discovers existing sessions and makes one controllable — landing the user on our differentiator (a session they can answer from the dashboard). SSOT via server API: the SPA renders the flow; OS actions (detect, register, connect) run through server endpoints.

## What already exists (survey) — reuse, don't rebuild
- CLI probe: `cmdscope.ProbeEngineVersion("claude")` (`server/internal/cmdscope/version.go:58`) + `exec.LookPath` patterns. No standalone endpoint.
- MCP one-liner: `buildMcpAddCommand`/`buildMcpJsonConfig` (`src/utils/mcpCommand.ts`) + `/api/config` fields (`mcpServerName`, `mcpEndpoint`) via `useServerConfig`. Buried in `ApiKeySettings.vue` behind key creation.
- API-key mint: `POST /api/settings/api-keys` → `{key, token}`.
- Session discovery: `GET /api/sessions`. Live-connect mechanism: `POST /api/agents/spawn {resumeSessionId, cwd, prompt}` respawns with `claude --resume <id> --mcp-config …` → the new PID is `liveInjectable` (can't attach to an already-running foreign process — respawn is the only lever; used today in `SessionDetailModal.vue:121`).
- Absent: any onboarding UI, and any "setup complete" state.

## API contract (new server work)
1. `GET /api/onboarding/status` → `{ completed: bool, cliInstalled: bool, cliVersion: string, mcpRegistered: bool }`.
   - `completed` from a new app_setting `onboarding.completed` (`TypeBool`, default false) in `settings/registry.go`.
   - `cliInstalled`/`cliVersion` from `cmdscope.ProbeEngineVersion("claude")` (+ `exec.LookPath`).
   - `mcpRegistered`: best-effort — check whether the dashboard MCP server is present in the user's `~/.claude.json` (parse it) OR omit in v1 and derive client-side. Keep simple: parse `~/.claude.json` for the `mcpServerName`; absent → false.
2. `POST /api/onboarding/register-mcp` → mints an API key (reuse the api-keys service internally, name e.g. `"onboarding"`, minimal scopes) and runs `claude mcp add <mcpServerName> --transport http <origin><mcpEndpoint> --header "Authorization: Bearer <token>"` via `exec.CommandContext`. Params are server-generated (no user-string interpolation → no injection). Returns `{ ok: bool, command: string }` (the equivalent copy-command for display/fallback). Same-origin + loopback guarded (it's a mutation).
3. `PATCH /api/onboarding/status` (or reuse the settings API) → set `onboarding.completed=true` (Done/Skip).
   - Reuse the existing settings write path if it cleanly sets an app_setting; else a tiny dedicated handler.

## Client work
- `src/composables/useOnboarding.ts` — fetch `/api/onboarding/status`, `registerMcp()`, `complete()`; reuse `useServerConfig` + `useSessions`.
- `src/components/onboarding/OnboardingFlow.vue` — a 3-step guided panel (AppModal or full-screen overlay), shown from `App.vue` when `status.completed === false`:
  - Step 1 CLI: show detected version (green) or "not found" + the vendor install command (copy) + a re-check button.
  - Step 2 Connect: `[Connect the dashboard]` button → `registerMcp()`; on success show ✓; always show the `buildMcpAddCommand` copy-block as the manual fallback ("or run manually").
  - Step 3 Session: list `GET /api/sessions`; each row a `[Make controllable]` button → `POST /api/agents/spawn {resumeSessionId, cwd, prompt:"continue"}` (default prompt = one-click) → on success, close onboarding + focus the new agent. If no sessions, a "spawn a new one" affordance (reuse the spawn dialog) + copy the `agent-dashboard live` on-ramp (`ChannelScriptCallout`).
  - Footer: Skip / Done → `complete()`.
- Wire into `App.vue`; add a "Re-run setup" entry (Settings or help menu) so it's re-openable.

## Tasks (OFD, main-thread orchestrated, worktree)
- T1 (backend): app_setting `onboarding.completed` + the 3 endpoints (`status`, `register-mcp`, complete), wired in the router + DI, with unit tests. Mock the `exec` seam for `register-mcp` so tests don't shell out (inject a runner func, default real).
- T2 (frontend): `useOnboarding` + `OnboardingFlow.vue` (3 steps) + `App.vue` wiring + re-run entry. Reuse mcpCommand/useServerConfig/useSessions/spawn. antfu lint 0, typecheck clean, vitest for the composable.
- T3 (e2e): `tests/e2e/onboarding-flow.spec.ts` against the Slice 1 harness — stub `/api/onboarding/status` (completed:false, cli installed), `/api/onboarding/register-mcp`, `/api/sessions`, `/api/agents/spawn`; assert the 3 steps render, the connect button posts, a session connect posts `resumeSessionId`, Done sets completed and the flow closes.
- T4 (docs): README (first-run onboarding), CHANGELOG, CONTRIBUTING if a workflow changed.

## Sequencing
T1 defines the contract → T2/T3 build against it (T3 stubs the endpoints, so it can proceed in parallel once the contract is fixed). Verify: server `go build`/`go test` (+ ent restore), frontend lint/typecheck/vitest, the onboarding E2E green against the harness. One PR → upcoming.

## Out of scope
Actually installing the CLI on the user's behalf (dashboard only prints the vendor command); Windows/Linux specifics; the webview-safety audit + distribution (Slice 4).
