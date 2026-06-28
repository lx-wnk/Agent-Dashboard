# Plugin Redesign SP3c — Anti-Lockout Plugin CLI — Design Spec

> Date: 2026-06-28 · Status: Draft for review · Branch: `feat/plugin-sp3c-cli` (off `feat/plugin-sp2-live-dispatch` / #232)
> Parent: `docs/superpowers/specs/2026-06-28-plugin-system-redesign-design.md` (SP3 row). Independent slice; the offline safety net for SP3a's restart.

## Why

Activating an `auth_provider` plugin and restarting (SP3a) can brick boot: the fatal-safety check refuses to start when a configured `auth_provider` is unhealthy, and the dashboard's HTTP control plane sits behind that same auth — so the user cannot fix it through the UI. The existing direct-DB CLI already provides an auth hatch (`dashboard settings set auth.mode none`, #230). SP3c extends that pattern to the plugin table so a bad plugin can be disabled offline, after which the server boots clean.

## Scope

In: a `dashboard plugins` CLI command group operating **directly on the SQLite DB** (no HTTP, bypassing the auth gate): `list`, `disable <id>`, `enable <id>`. Built on the existing `cmd/cli` cobra + ent pattern (`cmd_settings.go`).

Out: anything HTTP; the restart endpoint (SP3a); manifest/discovery changes. `enable` only flips `active` — it does not run lifecycle hooks (offline; hooks need a live server). Document that hooks are skipped for the CLI path.

## Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | Mirror the **existing direct-DB CLI pattern** (`cmd_settings.go`) — open the ent client against the resolved DB path, mutate, close | Consistent with the established lockout hatch; same DB-resolution (`--db` → `DASHBOARD_DB_PATH` → `~/.claude/dashboard-tasks.db`). |
| D2 | `disable`/`enable` set only `plugin.active` | The minimal, safe offline operation. Lifecycle hooks require a running server; the CLI deliberately skips them (documented). On next boot the enablement predicate reads `active`. |
| D3 | Reuse `repo.PluginRepo` (`SetActive`, `List`, `Get`) | The SP1 repo already has these; no new DB code. The CLI is a thin cobra wrapper over the repo. |
| D4 | `disable` of an unknown id → clear error, non-zero exit | Predictable scripting; matches `cmd_settings` error style. |

## Architecture

### `cmd/cli/cmd_plugins.go`
- A `plugins` cobra command (added to the `dashboard` root in `cmd/cli/main.go`) with subcommands:
  - `list` — print each plugin row: id, active, installed (from `plugin` table via `PluginRepo.List`).
  - `disable <id>` — `PluginRepo.Get` (404 → error) then `SetActive(id,false)`; print confirmation.
  - `enable <id>` — ensure the row exists then `SetActive(id,true)`; print confirmation + a note that lifecycle hooks are skipped (CLI is offline) and take effect on next boot.
- DB open/close + path resolution copied from `cmd_settings.go`'s helper (factor a shared `openDB`-style helper if `cmd_settings` already has one; otherwise follow its inline pattern — do not duplicate ent-open logic if a helper exists).

### Behaviour
- Pure DB mutation; no server contact. Intended to be run while the server is stopped (or even running — next boot/restart applies it). The output reminds the user to restart for the change to take effect on a running server.

## Data flow
```
dashboard plugins disable github-oauth --db <path>
  → open ent client (resolved DB path)
  → PluginRepo.Get(github-oauth)   # unknown → error, exit 1
  → PluginRepo.SetActive(github-oauth, false)
  → print "disabled github-oauth — restart the server to apply"
  → close DB
(next boot: enablement predicate reads active=false → plugin not started → auth_provider fatal-check no longer trips → server boots)
```

## Error handling
- Unknown id → `error: unknown plugin "<id>"`, exit non-zero.
- DB open failure (path missing / locked) → clear error, exit non-zero.
- `enable` on a never-discovered id → either upsert a minimal row (consistent with `pluginsctl.persist`'s prior behaviour) or error "run discovery first"; **decision: error** — enabling a plugin that was never discovered is almost always a typo, and the row lacks path/manifest. Document.

## Testing
- CLI unit tests (follow `cmd_settings_test.go`): against a temp ent DB seeded with a plugin row — `disable` sets active=false; `enable` sets active=true; `list` prints rows; unknown id → error + non-zero exit.
- No HTTP, no server spin-up.

## Risks / notes
- No ent schema change (reuses SP1's `plugin` table + repo).
- Offline `enable` skipping hooks is intentional and documented — the CLI is a recovery tool, not the normal activation path (that's `/api/plugins/{id}/activate`).
- Keep DB-open logic DRY with `cmd_settings.go` (shared helper if one exists).
