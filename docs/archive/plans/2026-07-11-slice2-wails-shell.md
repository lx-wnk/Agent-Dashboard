# Slice 2 — wails Desktop Shell — Implementation Plan

> Spec: `docs/superpowers/specs/2026-07-11-desktop-app-e2e-design.md` (Approved, D1–D6), Slice 2.
> Branch: `feat/desktop-slice2` off `upcoming`. PR → `upcoming`.
> Decision (2026-07-11): desktop = its own module in `go.work`, `//go:build darwin`-gated, build-only CI check; THIRD_PARTY_LICENSES regenerated (splice workaround for the go-licenses/Go-1.26 bug).

## Goal
A macOS wails app that starts the existing server **in-process** (goroutine) on `127.0.0.1:13120` and opens a webview to it — one binary that is both server and shell. Native menu, graceful shutdown on quit. The same SPA the browser + E2E run.

## Risk-first decomposition

### 2a — Exported serve bootstrap + in-process proof (headless-verifiable, decision-independent)
The serve run-loop lives in `package main` (`server/cmd/serve/main.go:40-113` + `initializeServer` in `di.go` + the `di_*.go` DI files) → not importable. Extract it so both the CLI and the desktop shell can start the server.

- Move the DI wiring + run-loop into an **exported, non-internal** package `server/serverapp` (must be non-internal so the separate `desktop/` module can import it across the module boundary). Files to relocate (rename `package main` → `package serverapp`): `di.go`, `di_db.go`, `di_mcp.go`, `di_pipeline.go`, `di_router.go`, `di_scheduler.go`, `di_tasks.go`, `di_seed.go`, `plugin_adapters.go`, `plugin_migrate.go`, `task_project_ops.go` (+ their `_test.go`). `serverapp` may import `server/internal/*` freely (same module).
- Expose `func Run(ctx context.Context, cfg config.Config, cfgFile string, restartCtl *restart.Controller) error` — the current serve RunE body (errgroup of agentbroadcast/srv/orch/sched/histImporter/evalService + restart watcher + `cleanup()`). Blocks until `ctx` is cancelled; runs `cleanup()`; honours restart. The caller owns the ctx: the CLI cancels via `signal.NotifyContext`; the desktop shell cancels on window close.
- `server/cmd/serve/main.go` stays `package main` (cobra CLI + `newLiveCmd`/`newPtyHostCmd`/channel/ptyhost subcommands — those don't touch DI). The serve RunE shrinks to: load config → build `restartCtl` → `serverapp.Run(ctx, cfg, cfgFile, restartCtl)`.
- **In-process proof (the headless de-risk):** a test in `serverapp` that starts `Run` in a goroutine (temp config, isolated DB, ephemeral or 13120 port), polls `/api/health` until up, then `POST`s a mutation with `Origin: http://<host>` matching `Host` → asserts **200** (proves the same-origin guard at `middleware.go:87-91` passes for a loopback-http origin — exactly what the webview will send), then cancels ctx → asserts `Run` returns and cleanup ran. This validates the entire in-process + graceful-shutdown mechanic without wails.
- Verify: `go build ./...`, `go test ./...` (restore `server/internal/db/ent/` after — `go test` regenerates it), `golangci-lint` 0. This is a pure server refactor: it ships in the main module/CI even before any wails code.

### 2b — wails shell (GUI, user-smoke)
- New module `desktop/` (own `go.mod`, `replace github.com/lx-wnk/agent-dashboard/server => ../server`), added to `go.work`. `require github.com/wailsapp/wails/v2`. All Go files `//go:build darwin`.
- `desktop/main.go`: create a cancellable ctx; `go serverapp.Run(ctx, cfg, "", restart.NewController(...))`; poll `http://127.0.0.1:13120/api/health` until 200; `wails.Run` opening a webview at `http://127.0.0.1:13120` (NOT wails embedded-asset serving — loading the http URL keeps `Origin` = the loopback host so mutations pass). Native app menu (Quit, Reload, About). On window close / app quit: cancel ctx, wait for `Run` to drain (reuse the existing graceful `Shutdown`).
- **Origin resolution:** confirmed by 2a's proof that loopback-http origin passes. Belt-and-suspenders only if the runtime smoke shows a `wails://` origin: add the webview origin to an allow-list in `RequireSameOriginForMutations` (extend the middleware with a configurable allowed-origin, mirroring `RequireLoopbackHostConfig.ExtraAllowedHosts`). Decide after the smoke — do NOT pre-add without evidence.
- **CI:** a `darwin`-only build-only job (`GOOS=darwin go build ./desktop/...`) so the shell compiles; no runtime GUI test in CI.
- **Runtime smoke (USER — not headless-automatable):** run the app on a real Mac, confirm the window loads the dashboard, basic nav works, and a **mutation** (spawn / create-task / answer-question) succeeds with no 403. This is the only leg that proves the webview Origin end-to-end.
- THIRD_PARTY_LICENSES regen for the wails dep tree (splice workaround, `LC_ALL=C`, per the go-licenses/Go-1.26 lesson). Docs: README/CHANGELOG (desktop build + run), CONTRIBUTING (wails toolchain).

## Sequencing
2a first, fully verified headlessly, its own reviewable unit. 2b stacks on top; its runtime leg is handed to the user. Ship Slice 2 as one PR (2a + 2b) or split 2a as a standalone server-refactor PR if 2b's toolchain/licensing drags.

## Out of scope (Slice 2)
First-run onboarding (Slice 3), webview-safety audit + fallbacks + signed/notarized DMG (Slice 4), Windows/Linux shells.
