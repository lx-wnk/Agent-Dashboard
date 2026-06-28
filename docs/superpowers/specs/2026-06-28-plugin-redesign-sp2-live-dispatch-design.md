# Plugin Redesign SP2 — Live Backend Dispatch + Process Management — Design Spec

> Date: 2026-06-28 · Status: Draft for review · Branch: `feat/plugin-sp2-live-dispatch`
> Prerequisite: SP1 (PR #231) — lifecycle foundation (`plugin` table, lifecycle engine + `HookCaller`/`StateRepo` seams, settings/secrets, discovery, lifecycle API). This branch is based on `feat/plugin-sp1-lifecycle` and its PR targets that branch (stacked on #231); both merge to `main` in order #231 → SP2.
> Parent spec: `docs/superpowers/specs/2026-06-28-plugin-system-redesign-design.md` (SP2 row).

## Why

SP1 persists plugin lifecycle state and calls HTTP hooks, but enable/disable still requires a server restart: chi cannot mutate its route tree after `ListenAndServe` (chi #480), so per-plugin reverse proxies are `r.Mount`ed once at boot. PR #230's interim made enablement restart-to-apply to kill an orphan-restart bug. SP2 delivers genuinely **live** `route_extension` enable/disable: a catch-all dispatcher decouples the URL→plugin mapping from chi's frozen route tree, and the lifecycle transitions drive real process start/stop. It also lands the real orphan-restart fix (suppress-restart-on-intentional-stop), process-group hygiene (no orphaned children), and observable supervision (unhealthy plugins surface in the DB instead of vanishing).

## Scope

In: catch-all dispatcher + atomic registry lookup; live `Activate`/`Deactivate` (start/stop, zero restart); `ProcessManager` seam on the lifecycle engine; transient start-run-stop for lifecycle hooks; `Setpgid` process groups + group-kill; suppress-restart-on-intentional-stop; mark-unhealthy-in-DB supervision; removal of the interim enablement path + boot per-plugin mount.

Out (later SPs): web-triggered supervised restart (SP3); frontend dynamic UI slots + per-plugin settings UI (SP4); plugin SDK + author docs (SP5). `auth_provider` and bind-port plugins stay boot-wired / restart-only (SP3).

## Decisions (resolved in brainstorming)

| # | Decision | Rationale |
|---|---|---|
| D1 | **Full replace** of the interim enablement | `activate`/`deactivate` become the only enable path for `route_extension`. Remove `PATCH /api/settings/plugins-enabled/{id}` and the boot-time per-plugin `r.Mount` loop. No dead paths / two mechanisms. |
| D2 | Dispatcher mounts **inside the authed `/api` umbrella** at `/api/plugins/{id}/proxy/*` | Inherits the dashboard's auth + Origin/CORS guards; no new unauthenticated surface on a server that reads sensitive Claude data. Keeps existing Cookie/Authorization stripping toward the plugin. |
| D3 | Engine gains a **`ProcessManager` seam**; the registry implements it | Engine stays process-agnostic (SP1 tests with a no-op); DI injects the real registry. Engine sequences start→hook→`SetActive` so a failing activate hook rolls back the process start. |
| D4 | **Transient start lives in the registry** (`WithTransient`), engine calls it | Process ownership stays in the registry (single source of process state); `HookCaller` stays pure HTTP; strategy (persistent vs transient) lives in the engine per transition. |
| D5 | Crash supervision = **mark unhealthy in DB**, keep the row | On exhausted backoff, fire `OnUnhealthy(id)` → DI sets the plugin row inactive/unhealthy. UI can surface it; dispatcher serves `503`; recoverable via re-activate. Replaces silent in-memory remove. |
| D6 | **Process groups + group-kill** | `Setpgid: true` on spawn; `gracefulStop` signals the group (`-pgid`) so plugin children die with the parent — no orphans. Unix-only (project is macOS/Linux). |

## Architecture

### 1. Catch-all dispatcher (`internal/plugin/dispatcher.go`)

- One static route registered at boot in `router.go`: `r.Handle("/api/plugins/{id}/proxy/*", dispatcher)`, inside the authed `/api` group.
- Per request: extract `{id}` (chi URL param) → `registry.Lookup(id)` → branch:
  - unknown id → `404`
  - found but not running / unhealthy / not active → `503`
  - running + healthy → reverse-proxy via existing `NewReverseProxy(entry, basePrefix)` (Cookie/Authorization stripping + path-prefix rewrite preserved). Base prefix is `/api/plugins/{id}/proxy`.
- Replaces the boot-time per-plugin `r.Mount` loop (`router.go:443-452`) entirely.

### 2. Atomic registry lookup (`internal/plugin/registry.go`)

- Add an `index map[string]*Entry` maintained alongside the `plugins` slice, under the existing `sync.RWMutex`. Hot-path `Lookup(id) (*Entry, bool)` takes `RLock` → O(1).
- `StartOne`/`StopOne`/crash-remove keep the index in sync under `Lock`.
- `Entry` gains: `healthy bool` (set after `waitHealthy`, cleared on crash), `intentionalStop bool` (see §4). Exposed via accessors, not raw fields, to keep mutation under the lock.

### 3. `ProcessManager` seam + live transitions (`internal/pluginlifecycle/engine.go`)

```go
type ProcessManager interface {
    Start(ctx context.Context, id string) error                          // persistent, idempotent
    Stop(ctx context.Context, id string) error                           // intentional stop
    WithTransient(ctx context.Context, id string, fn func() error) error // start-run-stop if not already up
}
```

- `Engine` gains a `proc ProcessManager` field (nil-safe / no-op default for SP1-isolated tests).
- Transition strategies:
  - `Activate`: requires installed → `proc.Start` (persistent) → activate hook → `SetActive(true)`. Hook error → `proc.Stop` + abort (no half-activated state).
  - `Deactivate`: deactivate hook (plugin still up) → `SetActive(false)` → `proc.Stop`.
  - `Install`: `proc.WithTransient` wrapping install + postInstall hooks; then `SetInstalledAt`.
  - `Update`: `proc.WithTransient` wrapping update hook; bump version.
  - `Uninstall`: if active → `proc.Stop` first; `proc.WithTransient` wrapping uninstall hook; clear settings + `installed_at`.
- `HookCaller` stays pure HTTP (unchanged from SP1). The registry implements `ProcessManager`; `WithTransient` starts the process if `Lookup` shows it down, runs `fn`, stops it iff it started it (idempotent / re-entrant safe).

### 4. Suppress-restart-on-intentional-stop (`registry.go`)

- `Stop`/`StopOne`/`Deactivate`-driven stop sets `entry.intentionalStop = true` before signalling.
- `watchPlugin` checks `intentionalStop` before respawning: intentional exit ⇒ no restart, clean deregister. Crash (flag false) ⇒ existing backoff path.
- This is the real orphan-restart fix that #230's restart-to-apply only worked around.

### 5. Process groups + group-kill (`registry.go`, Unix)

- `startEntry`: `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`.
- `gracefulStop`: `syscall.Kill(-pgid, syscall.SIGTERM)` → after the existing 5s timeout `syscall.Kill(-pgid, syscall.SIGKILL)`. Kills the whole group so plugin children die too.
- Lives in a Unix-tagged file consistent with `internal/platform` conventions (project supports macOS + Linux only).

### 6. Mark-unhealthy supervision (`registry.go` + DI)

- Add `Hooks.OnUnhealthy func(id string)` to the registry `Hooks` struct.
- `watchPlugin` after exhausted backoff (existing 1s→5s→30s, max 3): clear `healthy`, call `OnUnhealthy(id)` instead of silent in-memory remove. Keep the entry so the dispatcher can answer `503` knowingly.
- DI wires `OnUnhealthy` → set the plugin row `active=false` via the plugin repo (no new column; in-memory `healthy=false` distinguishes "crashed" from "user-deactivated" while the process is down).

### 7. Removal / migration

- Delete the interim `PATCH /api/settings/plugins-enabled/{id}` handler (`internal/api/plugins/handler.go`) and its route.
- Delete the boot per-plugin `r.Mount` loop (`router.go:443-452`); replace with the single catch-all registration.
- Docs: README + CHANGELOG + plugin docs note the URL-contract change — plugin route extensions are now served under `/api/plugins/{id}/proxy/*` (was `/api/settings/plugins/{id}`), and enable/disable is live via `activate`/`deactivate` (no restart).

### 8. DI wiring (`cmd/serve/di.go`)

- Inject the registry as the lifecycle engine's `ProcessManager`.
- Register the catch-all dispatcher route in `router.go` via `RouterDeps`.
- Wire `Hooks.OnUnhealthy` → plugin repo state update.
- Boot still reads `plugin.active` to decide which plugins `Load` starts (unchanged from SP1).

## Data flow (live activate)

```
POST /api/plugins/{id}/activate
  → controller → Engine.Activate(id)
      → proc.Start(id)            // registry: spawn (Setpgid), waitHealthy, set healthy, index++
      → HookCaller.Call(activate) // HTTP POST to plugin addr
      → StateRepo.SetActive(true)
  (hook fails → proc.Stop(id) + abort, active stays false)

GET /api/plugins/{id}/proxy/foo  (immediately, no restart)
  → dispatcher → registry.Lookup(id) → healthy → reverse-proxy → plugin
```

## Error handling

- Activate hook non-2xx / transport error → `proc.Stop`, transition aborts, surface hook error (4xx/5xx per #230 error-class pattern), `active` unchanged.
- Dispatcher: unknown→404, down/unhealthy/inactive→503, proxy transport error→502 (existing reverse-proxy error handler).
- `WithTransient`: start failure → abort hook, surface error; ensure stop on the way out even if `fn` panics/errors (defer).
- Group-kill: SIGKILL escalation guarded by the existing timeout; never block shutdown indefinitely.

## Testing

- **Dispatcher:** unknown id→404; inactive/unhealthy→503; active+healthy→proxied, Cookie/Authorization stripped, path rewritten; concurrent lookup under RWMutex (race detector).
- **Live transitions:** activate→process started + route reachable, zero restart; deactivate→stopped + route 503, zero restart; activate hook failure→process stopped, `active` false.
- **Transient:** installed-but-stopped plugin → `WithTransient` starts, runs hook, stops; already-running plugin → hook runs, no stop; `fn` error still stops.
- **Process groups:** spawn a plugin that forks a child; group-kill leaves no orphan (assert child pid gone).
- **Suppress-restart:** intentional stop ⇒ no respawn; simulated crash ⇒ respawn via backoff.
- **Supervision:** exhausted backoff ⇒ `OnUnhealthy` fired, entry retained, dispatcher 503; DB row marked.
- **Removal:** interim PATCH route gone (404); boot no longer mounts per-plugin paths; catch-all serves them.
- **Engine isolation:** SP2 engine tests with a fake `ProcessManager` assert call order (start before hook, stop on hook failure, transient wrap).

## Open risks

- **ent regen** not required (no schema change) unless the unhealthy marker becomes a new column. Default: reuse `active=false`; if an explicit `unhealthy`/health column is wanted, it's a schema add → run generate flow, revert `runtime.go`/`go.sum` drift, restore `server/internal/db/ent/` after full test runs. **Decision: reuse `active=false` + an in-memory healthy flag for SP2; no new column.**
- **URL contract change** for any existing built plugin (route path moves). Document in CHANGELOG; sample/dev plugins updated.
- **Setpgid portability:** Unix-only; guarded by build tags consistent with `internal/platform`. Windows already unsupported.
- **Race safety:** dispatcher reads while transitions mutate — all registry mutation under the existing `sync.RWMutex`; verify with `-race`.
