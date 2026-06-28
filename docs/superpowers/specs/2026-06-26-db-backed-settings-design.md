# DB-backed Settings — Design Spec

> Date: 2026-06-26 · Status: Draft for review · Branch: `feat/db-backed-settings`

## Goal

Make all non-bootstrap configuration live in **one place**: a database-backed settings
store, editable from the **UI** and a new **direct-DB CLI**. Today configuration is
env-only (koanf), so the UI cannot change it and a `.env` value silently wins. This
splits configuration cleanly:

- **Bootstrap + secrets** → stay in env/flags (needed before the DB is open, or must not
  sit in a readable table).
- **Everything else** → DB-only. Removed from koanf. The UI and CLI are the only ways to
  set it.

The original trigger: plugin enablement must be controllable in the UI (default all-off),
and an unbuilt `auth_provider` plugin must not block boot.

## Non-Goals

- Migrating bootstrap/secret settings to the DB (impossible / unsafe — see boundary).
- Per-project scoping of these settings (they are global; `pipeline_config` already
  handles per-project pipeline overrides separately).
- Hot-reloading every setting. Only enablement settings apply live; the rest are
  restart-to-apply with a clear UI badge.
- A general migration of existing env values into the DB (env values for moved keys are
  **ignored** after upgrade; the user re-sets them via UI/CLI).

## Core Decisions

| # | Decision | Rationale | Rejected alternative |
|---|---|---|---|
| D1 | Single generic `app_setting` KV table (`key`, `value` TEXT, `updated_at`) | One migration, extensible; values validated by a Go registry | 20 typed columns / per-domain tables (migration churn) |
| D2 | A Go **setting registry** is the SSOT: per key → type, default, apply-semantics, validation | Replaces koanf defaults; one place defines every setting | Scattering defaults across consumers |
| D3 | Env keeps **only** bootstrap+secrets; all other `DASHBOARD_*` dropped from koanf | User requirement: no env/DB mixing | Env-overrides-DB precedence (the "mix" the user rejected) |
| D4 | CLI mutates the DB **directly** (opens `cfg.DBPath`), never via HTTP | Must work while the server is down / when auth locks out the API | HTTP CLI (locked out by the same auth gate) |
| D5 | Enablement settings apply **live**; all others **restart-to-apply** | Live process start/stop is feasible only for plugins/providers; boot-read settings can't change in place safely | Pretending everything is live |
| D6 | Plugin enablement default **all-off**; `Load` skips disabled plugins entirely | Matches user ask; a disabled `auth_provider` plugin then never trips the boot guard | Default-on (today's behavior) |
| D7 | No env→DB import on upgrade; log a warning for any moved key still set in env | Keeps "single source" honest | Silent import (reintroduces the mix) |

## The env / DB boundary

**Stays in env/flags** (koanf keeps reading these):

`DB_PATH`, `HOST`, `PORT`, `JWT_SECRET`, `HOOKS_SECRET`, `MCP_TOKEN`,
`AUTH_PLUGIN_SECRET`, `PLUGIN_DIR`, `PROVIDER_DIR`, `WORKTREE_ROOT`, `REMOTES_ENABLED`,
`--config` flag.

Reasoning: `DB_PATH` can't be read from the DB (chicken-egg); `HOST`/`PORT`/`REMOTES_ENABLED`
are needed to bind before the DB matters and are security-bootstrap; `*_SECRET`/`MCP_TOKEN`
must not sit in a readable settings table; `PLUGIN_DIR`/`PROVIDER_DIR`/`WORKTREE_ROOT` are
filesystem paths kept with `DB_PATH`.

**Moves to DB-only** (dropped from koanf — see inventory):

`auth`, `providers_enabled`, plugin enablement (new), `allow_git_push`, `force_worktrees`,
`sse_interval_ms`, `shutdown_timeout_seconds`, `hooks_debounce_ms`, `hook_events_per_session`,
`spawn_rate_limit`, `spawn_rate_window_ms`, `inject_rate_limit`, `inject_rate_window_ms`,
`cost_scan_interval_ms`, `eval_scan_interval_ms`, `eval_window_hours`, `eval_min_samples`,
`eval_rate_drop_pp`, `eval_stddev_k`.

**Explicitly out of scope** (not migrated): `adapters` / `AdapterConfig` and
`DASHBOARD_SPAWN_COMMAND` are deprecated boot-migration shims — runtime spawner/adapter
config already lives in the `spawners` table (edited via `/api/spawners`). Pulling them into
`app_setting` would duplicate DB-backed config. They are left untouched.

## Settings Inventory

`key` is the `app_setting` row key; `apply` is `live` or `restart`.

| key | type | default | apply | consumer |
|---|---|---|---|---|
| `auth.mode` | enum(none,plugin) | `none` | restart | router wiring (boot) |
| `providers.enabled` | json([]string) | `[]` | live | provider registry scan |
| `plugins.enabled` | json([]string) | `[]` | live | plugin registry |
| `git.allowPush` | bool | `false` | restart | git-action/spawn |
| `worktree.force` | bool | `false` | restart | pipeline pickup |
| `sse.intervalMs` | int | `3000` | restart | broadcaster |
| `shutdown.timeoutSeconds` | int | `10` | restart | shutdown |
| `hooks.debounceMs` | int | `100` | restart | hooks receiver |
| `hooks.eventsPerSession` | int(>0) | `50` | restart | hooks receiver |
| `spawn.rateLimit` | int | `5` | restart | spawn middleware |
| `spawn.rateWindowMs` | int | `60000` | restart | spawn middleware |
| `inject.rateLimit` | int | `30` | restart | inject middleware |
| `inject.rateWindowMs` | int | `60000` | restart | inject middleware |
| `cost.scanIntervalMs` | int | `300000` | restart | cost scanner ticker |
| `eval.scanIntervalMs` | int | `3600000` | restart | eval ticker |
| `eval.windowHours` | int(>0) | `168` | restart | eval service |
| `eval.minSamples` | int(>=0) | `20` | restart | eval service |
| `eval.rateDropPP` | float(>=0) | `15` | restart | eval service |
| `eval.stddevK` | float(>=0) | `3` | restart | eval service |

Validation rules (e.g. `eval.windowHours > 0`, `hooks.eventsPerSession > 0`) move from
`config.Load` into the registry's per-key validators and run on every `Set`.

## Architecture

```
                ┌──────────────┐         ┌──────────────────┐
   UI  ───────► │ /api/settings│ ──────► │ settings.Service │ ──► app_setting (DB)
                └──────────────┘         │  (DB-first,      │
   CLI ───────────────────────────────► │   registry       │ ◄── reads same DB file
   (direct DB, no HTTP)                  │   default        │
                                         │   fallback)      │
                                         └──────────────────┘
                                                  ▲
   serve boot ────────────────────────────────────┘ (reads each key once;
                                                      live keys also re-read on Set)
```

### Components

1. **`app_setting` ent schema + repo** — `key` (unique), `value` (text), `updated_at`.
   Repo: `Get(key)`, `List()`, `Upsert(key, value)`, `Delete(key)`.

2. **Setting registry** (`internal/settings/registry.go`) — a static map of
   `Definition{ Key, Type, Default, Apply, Validate, Category }` for every DB-backed key.
   The single source for defaults + validation. New settings are added here.

3. **`settings.Service`** (`internal/settings/service.go`) — generalizes
   `providersettings.Service`. `Get[T]`-style typed accessors (`Bool`, `Int`, `Float`,
   `String`, `StringSlice`, `JSON`) returning DB value or registry default. `Set(key, raw)`
   validates against the registry, upserts, updates an in-memory snapshot, and for `live`
   keys notifies the relevant subsystem. Loaded once at boot into the snapshot.

4. **Plugin registry changes** —
   - `Load` consults `settings.Service` (`plugins.enabled`) and **skips** disabled plugins:
     their `plugin.json` is not read into `attemptedCapabilities`, so a disabled
     `auth_provider` plugin no longer trips the di.go boot guard.
   - New `StartOne(ctx, id) error` / `StopOne(id) error` reusing the existing spawn +
     `waitHealthy` + `watchPlugin` machinery for live enable/disable of non-auth plugins.
   - `auth_provider` plugins: toggling persists to DB but returns `applyPending=restart`
     (router wiring is boot-time); the API surfaces this and the UI shows a warning.

5. **Auth wiring** — `di_router.go` reads `auth.mode` from `settings.Service` instead of
   `cfg.Auth`. Still boot-time → changing it is restart-to-apply. The `PATCH` response
   carries `applied:"restart"`; the UI must raise a **warning toast** ("Auth mode change
   takes effect after a server restart — and `plugin` will require login") whenever
   `auth.mode` is changed.

6. **API** (`internal/api/settings`) — mirrors the `systemprompts`/`providers` pattern:
   - `GET /api/settings` → all definitions with current value + default + apply-semantics.
   - `PATCH /api/settings/{key}` `{value}` → validate + persist; response includes
     `{applied: "live"|"restart"}`.
   - `GET /api/plugins` (list: id, capabilities, health, enabled) + `PATCH /api/plugins/{id}`
     `{enabled}` (live start/stop, or restart-pending for auth plugins).

7. **CLI** (`cmd/cli`) — new subcommands that **open the DB directly** via a shared helper
   (`openDB(cfg.DBPath)` with SQLite busy-timeout), independent of the HTTP client:
   - `dashboard config list|get <key>|set <key> <value>`
   - `dashboard plugins list|enable <id>|disable <id>`
   - `dashboard auth set none|plugin`
   These write `app_setting` rows; the running server picks live keys up on next scan and
   all keys on next restart. Primary purpose: scripting + **lockout recovery**.

8. **UI** — extend `ApiKeySettings.vue`. `PluginSettings.vue` goes from read-only to a
   toggle list (live). A new generic settings panel renders the registry grouped by
   category, each control tagged `live`/`restart-required`. Any change to a `restart`-apply
   setting raises a **warning toast** ("Applies after a server restart"); the auth-mode
   selector additionally warns that `plugin` enables login. Live changes show a success
   toast / immediate state flip.

### Data flow — enabling a plugin (live)

1. UI `PATCH /api/plugins/voice-whisper {enabled:true}`.
2. Handler → `settings.Service.Set("plugins.enabled", [...,"voice-whisper"])` (validated).
3. Service appends to snapshot, calls `pluginRegistry.StartOne(ctx, "voice-whisper")`.
4. Registry spawns the process, `waitHealthy`, registers, starts `watchPlugin`.
5. Response `{applied:"live"}`; UI flips the toggle, shows health.

### Data flow — lockout recovery (CLI, server down)

1. User enabled `auth.mode=plugin`, restarted, OAuth plugin broken → can't log in.
2. `dashboard auth set none` opens `cfg.DBPath`, upserts `auth.mode=none`.
3. Restart server → `di_router` reads `none` → bypass-auth → back in.

## Env removal & upgrade behavior

- koanf's env provider is restricted to the bootstrap allowlist; `Config` shrinks to the
  bootstrap+secret fields. The big `defaults` map and the moved validators leave `config.Load`.
- On boot, for each moved key, if `DASHBOARD_<KEY>` is still set in the environment, log
  `WARN "DASHBOARD_X is no longer read from env — set it via the Settings UI or 'dashboard config set'"`.
- First boot after upgrade: `app_setting` is empty → every key resolves to its registry
  default. Default auth is `none` and plugins all-off, so the server boots cleanly even with
  the broken OAuth plugins present.

## Relationship to PR #229 (.env auto-loading)

#229 makes a `.env` file load into the process env. After this change, `.env` is only
consulted for the bootstrap+secret allowlist. The two are compatible; #229 should merge
first so bootstrap settings remain `.env`-settable.

## Lockout-safety summary

- Auth default `none`; enabling `plugin` is restart-to-apply with an explicit UI warning.
- The CLI is a **direct-DB** escape hatch usable while the server is down.
- A disabled (or unbuilt) `auth_provider` plugin never blocks boot (D6).

## Testing

- **Registry/Service:** default fallback, `Set` validation (reject bad enum/negative/zero),
  typed accessors, snapshot update.
- **app_setting repo:** upsert/get/list/delete (in-memory sqlite).
- **Plugin registry:** `Load` skips disabled (synthetic plugin); `StartOne`/`StopOne`
  lifecycle; disabled `auth_provider` does not set `HasAttemptedCapability`.
- **di boot guard:** present-but-disabled auth plugin → boots; enabled-but-unhealthy → refuses.
- **API:** GET shape, PATCH validation + applied-semantics, plugin enable/disable.
- **CLI:** direct-DB set/get against a temp DB; `auth set none` recovers a locked config.
- **Frontend:** PluginSettings toggle calls PATCH and reflects health; settings panel
  renders live/restart badges.
- **Regression:** existing `config_test.go` trimmed to the bootstrap set; add a test that a
  moved key set in env is ignored + warned.

## Open Dependencies / Risks

- **ent regen** required (new schema) — run via the project's generate flow; watch the
  known `runtime.go`/`go.sum` drift (revert non-schema drift).
- **CLI vs server DB concurrency** — direct-DB writes use SQLite busy-timeout; live keys
  changed via CLI while the server runs are not seen until restart (documented; UI is the
  live path).
- **Toast UX** — relies on an existing toast/notification primitive in the frontend; the
  plan must confirm one exists (or add a minimal one) for the restart/auth warnings.

## Phasing (one PR, staged commits)

1. Foundation: `app_setting` schema+repo, registry, `settings.Service`, generic
   `/api/settings`, direct-DB CLI core, UI shell.
2. Live domain: plugin enablement (StartOne/StopOne + guard-skips-disabled), auth-mode,
   provider enablement folded onto the foundation. (Unblocks boot + original ask.)
3. Operational migration: remaining keys → DB, dropped from koanf, env-ignored warnings,
   `config.Load` trimmed.
