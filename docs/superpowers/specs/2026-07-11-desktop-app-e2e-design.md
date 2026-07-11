# Desktop App + E2E Coverage — Design Spec

> Date: 2026-07-11 · Status: Draft (awaiting user review) · Branch: `feat/desktop-app` (off `upcoming`)
> Initiative: neutralize the one axis a closed-source competitor (Sightdeck) wins on — install friction — by shipping a native macOS desktop app with one-click first-run onboarding, while raising Playwright E2E coverage of the Vue app. Lead with the capability we already have that the competitor lacks: answering/steering a live agent from the dashboard.

## Why

Competitive context (research 2026-07-11, `docs/local` brief): **Sightdeck** — closed-source, macOS-only, €49/yr — is a task-backlog layer on Claude Code. It monitors sessions and *visually flags* a waiting one, but **cannot answer the question or inject a prompt from the app** (user must tab back to the terminal). Its only real edge over us is onboarding: a signed `.dmg`, no account, a one-click "Setup" that installs the Claude CLI + registers a skill. We already win on capability (universal session control just shipped in #261 — the exact gap; plus MCP control plane, state-machine pipeline, plugins), cross-platform (macOS+Linux), open-source/auditable, and price (free). We lose only on packaging: install today needs a binary/brew/docker, not a double-click.

Two workstreams close this, and they reinforce each other:
1. **Desktop app + first-run onboarding** — match the install ease, then out-compete on everything else.
2. **E2E coverage** — the same Vue SPA runs in the desktop webview, so Playwright specs written against the SPA cover the desktop UI too; the desktop shell needs only a thin native smoke on top.

## Key architectural insight (why wails)

The backend is already Go (`agent-dashboard serve` binds `127.0.0.1:13120`, serves the embedded Vue SPA + REST/SSE/MCP). **wails is a Go desktop framework**, so the desktop app can start the server **in-process (a goroutine calling the existing serve bootstrap)** and open a webview at `http://127.0.0.1:13120` — one Go binary that is *both* the server and the shell. No sidecar process, no IPC bridge, no lifecycle juggling. Tauri (Rust) or Electron (Node) would each run the Go server as a separate child process. That single-process property is the decisive reason to pick wails here.

## Decisions (proposed — for review)

| # | Decision | Rationale |
|---|---|---|
| D1 | **wails** for the macOS shell | Go-native → server runs in-process, single signed binary; matches the existing Go architecture. (Tauri/Electron = Go-as-sidecar.) |
| D2 | The desktop binary **reuses the `serve` bootstrap in-process** (goroutine on `127.0.0.1:13120`), webview loads that URL; graceful server shutdown on window close | No duplicate server logic; the web app IS the desktop app. |
| D3 | **E2E harness fix first** — `webServer` boots the built Go binary on `13120` (not `pnpm dev`, which serves vite on `5173` and never satisfies the `:13120` wait) | Unblocks the whole suite; the current config times out every run (the #258 quirk). |
| D4 | **First-run onboarding is in-scope for round 1**, not deferred | The onboarding — not the window — is the actual Sightdeck counter; a bare shell doesn't move the needle. |
| D5 | macOS **signing/notarization + DMG** is an operator/paid step (Apple Developer ID) — wire the wails build into release tooling, but cutting/notarizing a signed release stays user-triggered | Same posture as the release tag (#253): outward-facing, credential-bound. |

## Slices

### Slice 1 — E2E foundation (harness fix + high-value web specs)
- **Harness fix:** change `playwright.config` `webServer` to build + run the Go binary on `13120` (or a script that does `task build:all` once then `bin/agent-dashboard serve`), with `reuseExistingServer` + a realistic timeout. Verify the existing specs (some fully stub `/api`, two need a real backend) run green against it.
- **High-value web specs (Plan B):** cover the riskiest flows with stubbed `/api` where possible: dashboard triage band (question-band already done in #261), tasks/pipeline board + task modal, settings (API keys / spawner / pipeline config), spawn flow, workflows charts, refinement chat. Prioritize by user-facing risk; this is incremental, not exhaustive.
- These specs are the transferable asset: they validate the SPA that the desktop webview also renders.

### Slice 2 — wails desktop shell
- Add a wails app (its own module/dir, e.g. `desktop/`) whose `main` starts the server bootstrap in-process on `127.0.0.1:13120` and opens a webview to it. Native app menu (Quit, Reload, About), optional tray. Window lifecycle: on quit, gracefully shut the server (reuse the existing `Shutdown` drain).
- **Webview origin risk:** the same-origin mutation middleware (`RequireSameOriginForMutations`) compares `Origin` vs `Host`. A wails webview's `Origin` may be a custom scheme (`wails://…`) rather than `http://127.0.0.1:13120` → mutations could 403 (the same class of issue the vite dev-proxy solves by rewriting `Origin`). Resolve: load the SPA over `http://127.0.0.1:13120` (so Origin is the loopback host), or add the webview origin to the allow-list. Decide during build; cover with a smoke.
- Native smoke test (wails' test tooling / a headless launch): app boots, server is up, the window loads the dashboard, basic nav works. No duplication of SPA coverage (Slice 1 owns that).

### Slice 3 — First-run onboarding (the Sightdeck counter)
- A guided first-run screen in the SPA (shown when no config/first launch): (a) detect the Claude Code CLI, offer a one-click install/verify; (b) register the dashboard skill/MCP connection (reuse the `claude mcp add` one-liner from #253 + the `agent-dashboard live` on-ramp from #261) without making the user hunt docs; (c) discover existing Claude sessions and offer one-click `live`-connect to make one controllable.
- Mirror Sightdeck's best UX (one "Setup" button that does the CLI + skill install) but land the user on our differentiator immediately: a controllable session they can answer from the dashboard.
- The onboarding is SPA-level (works in browser AND desktop), so Slice 1's E2E covers it; add an onboarding-flow spec.

### Slice 4 — Webview-safety audit + distribution (partly deferred)
- **Webview-safety:** audit browser-only assumptions that differ in a webview — VitePWA/service worker (`sw.js`), notifications, clipboard, install-prompt. Add feature-detection/fallbacks so the SPA degrades cleanly in the wails webview.
- **Distribution:** wire the wails macOS build into the release tooling (goreleaser/CI) producing an unsigned `.app`/`.dmg` artifact; document the signing/notarization steps. **Do not** cut/notarize a signed public release (operator/paid — D5). Homebrew cask already exists (#253) and can point at the `.app` later.

## Scope

**In:** E2E harness fix + a prioritized set of web E2E specs; a wails macOS shell running the server in-process; first-run onboarding (CLI/skill/MCP setup + session discovery + one-click `live`-connect); webview-safety audit + fallbacks; unsigned wails build artifact + release-wiring + signing docs.

**Out:** cutting/notarizing a signed public DMG (operator/paid); Windows/Linux desktop shells (macOS first — matches where the competitor is; Linux users already have the binary); exhaustive "every function" E2E (incremental after the high-value set); any change to the server's wire protocol.

## Testing

- Slice 1: the harness runs the real binary; web specs (stubbed `/api`) for the prioritized flows; existing specs green.
- Slice 2: native smoke (app boots, server up, window loads, nav) + the webview-origin mutation check.
- Slice 3: onboarding-flow web spec (first-run detection → setup → connect), stubbed.
- Slice 4: feature-detection unit tests for the webview fallbacks; a build-only check that the wails artifact assembles.

## Open questions (for review)

1. **wails vs Tauri** — recommend wails (D1, in-process server). Confirm, or prefer Tauri (Go-as-sidecar) for its more mature updater/notarization ecosystem?
2. **Onboarding depth round 1** — full counter (CLI check + skill/MCP register + session discovery + one-click connect), or a leaner first cut (just the connect one-liner surfaced prominently) with discovery later?
3. **E2E harness** — build the binary inside `webServer` each run (slower, hermetic) vs. expect a pre-built binary / `reuseExistingServer` (faster, needs a step)? Recommend a `webServer` command that builds-if-stale then serves.
4. **Slice order / independence** — Slice 1 (E2E) is independent and low-risk; Slices 2–4 (desktop) stack. Ship Slice 1 first as its own PR, then the desktop slices? Or bundle?
