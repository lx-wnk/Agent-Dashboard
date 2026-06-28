# Plugin Redesign SP3a — Web-Triggered Supervised Restart — Design Spec

> Date: 2026-06-28 · Status: Draft for review · Branch: `feat/plugin-sp3a-restart` (off `feat/plugin-sp2-live-dispatch` / #232)
> Parent: `docs/superpowers/specs/2026-06-28-plugin-system-redesign-design.md` (SP3 row). Sibling slices: SP3b (reconnect overlay), SP3c (anti-lockout CLI). Each its own spec -> plan -> OFD -> PR.

## Why

SP2 made `route_extension` plugins live. `auth_provider` and bind-port plugins stay **boot-wired** — auth middleware is assembled once at startup (`di.go`), so applying such a plugin needs a process restart. SP3a delivers a safe, web-triggered restart: a request drains in-flight work, validates that the new boot will not lock the user out, then either re-execs itself (no external supervisor needed) or exits for a supervisor to restart.

## Scope

In: `POST /api/admin/restart` (202 + async restart); **validate-before-restart** lockout guard; graceful drain via the existing signal path; restart mechanic with two modes (`reexec` default, `exit` for supervisors); supervisor setup docs.

Out: frontend reconnect overlay + "restart required" badge (SP3b); anti-lockout CLI (SP3c); config hot-reload without restart (not a goal — auth is boot-bound by design).

## Decisions (resolved in brainstorming)

| # | Decision | Rationale |
|---|---|---|
| D1 | **Self-re-exec is the default; external supervisors also supported** via `DASHBOARD_RESTART_MODE` (`reexec`\|`exit`, default `reexec`) | `reexec` (re-exec same binary via syscall, same PID) works for plain `./bin/agent-dashboard serve` with no external dependency — ideal for local single-user. `exit` lets systemd/launchd/wrapper own the restart. |
| D2 | **Validate before restart** — refuse (409) if the post-restart boot would brick | The boot fatal-safety check refuses to start when a configured `auth_provider` is unhealthy. Validating first turns a hard lockout into a recoverable 409. |
| D3 | Restart is **async after the 202** | The HTTP response must flush before the process tears down; the handler signals a restart channel and returns 202. |
| D4 | Reuse the **existing signal/shutdown path** (`serve/main.go` signal.NotifyContext) | One graceful-shutdown path, not two. The restart trigger feeds the same teardown that SIGTERM does, then branches on mode. |

## Architecture

### Restart trigger + mechanic
- `serve/main.go` already runs the server under `ctx, stop := signal.NotifyContext(ctx, os.Interrupt, SIGTERM)` and shuts down on ctx-cancel. Add a `restart chan struct{}` (buffered, size 1) wired into the run loop: the loop selects on `ctx.Done()` (plain shutdown) **or** `<-restart` (shutdown-then-reexec).
- On restart: graceful `http.Server.Shutdown(drainCtx)` (drain in-flight, bounded timeout, reuse existing shutdown logic + `cleanup()` which stops plugins), then:
  - `reexec` mode -> re-exec the current binary in-place (resolve path via `os.Executable()`, then the syscall that replaces the process image with `os.Args` + `os.Environ()`). Same PID; the OS re-runs the binary. On re-exec error, fall back to a non-zero exit with a logged error.
  - `exit` mode -> exit 0; the supervisor restarts.
- The mode lives in `config.Config` (`RestartMode string` koanf `restart_mode`, env `DASHBOARD_RESTART_MODE`, default `reexec`); validate to the two allowed values at load (unknown -> error or fall back to `reexec` with a warning).

### Endpoint
- `POST /api/admin/restart` mounted in the authed `/api` group (`router.go`, via `RouterDeps`). Returns **202** with `{"status":"restarting","mode":"reexec|exit"}` after passing validation; the actual restart fires asynchronously (handler sends on the restart channel, then returns).
- A new `AdminHandler` (`internal/api/admin`) holds the restart channel + a `Validator` func + the configured mode. Keep it a narrow handler (one route now; SP3 may add more admin verbs later).

### validate-before-restart (the lockout guard)
- Before signalling restart, run a `RestartValidator`: if an `auth_provider` plugin is currently `active` in the plugin table, verify it can serve — i.e. start (if needed) + health-check its `/health` (reuse the registry's `StartOne` + health, or a dedicated probe), mirroring the boot fatal-safety predicate (`di.go:244-250`). If it fails, return **409** `{"error":"restart would lock out auth: <detail>"}` and do NOT restart.
- The validator is injected (interface) so it is unit-testable with a fake; the real impl consults the plugin registry + repo. If no `auth_provider` is active, validation passes trivially.

### Supervisor docs
- README/docs: two run modes. Default (`reexec`) — plain `serve`, restart re-execs itself, no supervisor. Supervised — set `DASHBOARD_RESTART_MODE=exit` and run under systemd (`Restart=always`) / launchd (`KeepAlive`) / a wrapper loop; document a minimal unit + a `while true; do ./bin/agent-dashboard serve; done` wrapper.

## Data flow
```
POST /api/admin/restart  (authed)
  -> AdminHandler.Restart
      -> validator.Validate(ctx)            # active auth_provider can boot?
          fail -> 409, stay running
          ok   -> send on restart chan; respond 202 {status:restarting,mode}
  (async, after response flush)
  main run-loop receives <-restart
      -> server.Shutdown(drainCtx) + cleanup()   # drain + stop plugins
      -> mode reexec: re-exec self  |  mode exit: exit 0
```

## Error handling
- Validation failure -> 409, no restart, server keeps serving.
- Re-exec failure (rare) -> log fatal + non-zero exit (supervisor or user restarts manually); never hang.
- Drain timeout -> bounded `Shutdown` context (reuse existing timeout), then proceed to re-exec/exit regardless.
- Double-trigger -> buffered chan size 1 + non-blocking send; a second restart request while one is pending returns 202 (idempotent).

## Testing
- Endpoint: 202 on success (fake validator ok) with correct body/mode; 409 when validator fails; route is inside the authed group.
- Validator: passes when no active auth_provider; fails when an active auth_provider is unhealthy (fake registry/repo).
- Restart mechanic via a **seam**: the run-loop's "do restart" calls an injected `restarter` interface (`Reexec()` / `Exit()`); tests assert the right method is called per mode without actually re-exec-ing. The real re-exec syscall lives behind the real impl only.
- Config: `DASHBOARD_RESTART_MODE` parsing (default reexec; `exit`; unknown -> reexec+warn).

## Risks / notes
- `reexec` under `air` (dev) or systemd: re-exec keeps the same PID and re-runs the binary — under air this may fight the file-watcher; under systemd it bypasses the supervisor. Document: use `exit` mode when supervised. Default `reexec` targets the plain local `serve`.
- No ent schema change.
- `os.Executable()` may resolve a path that was replaced/deleted (rare during upgrades) — acceptable for local single-user; document.
