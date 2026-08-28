# Configuration

Configuration comes from two places:

1. **Bootstrap configuration** — host, port, secrets, and filesystem paths the server needs before it can read its own database. These live in environment variables (or a JSON config file). A documented template lives in [`.env.dist`](../../.env.dist).
2. **Runtime settings** — operational config (auth mode, rate limits, scan intervals, plugin/provider enablement, …) stored in the database `app_setting` table. Edit these in the **Settings UI** or with the `dashboard settings` CLI. They are **no longer read from the environment** — a still-set env var for a moved key is ignored and logs a warning on boot.

```bash
cp .env.dist .env
```

A `.env` file in the working directory is loaded automatically at startup for both
`task dev` and `./bin/agent-dashboard serve` — no manual sourcing needed. An explicit
shell `export` always wins over a value in `.env`. The file is read from the current
working directory (the repository root in the standard layout); run the binary from
there, or export the variables, if you keep `.env` elsewhere. Only the bootstrap
variables below are read from `.env`/env; runtime settings live in the database.

## Bootstrap configuration (env / flags)

These are the only environment variables still read by the core server. They cannot be set through the Settings UI because they are needed before (or independently of) the database.

| Variable | Default | Description |
|---|---|---|
| `DASHBOARD_HOST` | `127.0.0.1` | Bind address. A non-loopback address fails to boot unless `DASHBOARD_REMOTES_ENABLED=true` |
| `DASHBOARD_PORT` | `13120` | HTTP server port |
| `DASHBOARD_JWT_SECRET` | auto-generated (ephemeral) | Secret for signing JWT session tokens (min 32 chars). Set a stable value to survive restarts |
| `DASHBOARD_DB_PATH` | `~/.claude/dashboard-tasks.db` | SQLite path for the task pipeline and the settings store |
| `DASHBOARD_PLUGIN_DIR` | — | Directory of auth/route-extension plugins to load. Empty disables plugin loading. (Which discovered plugins are *enabled* is a runtime setting — see `plugins.enabled` below) |
| `DASHBOARD_PROVIDER_DIR` | — | Optional directory of user provider descriptors merged over the built-ins |
| `DASHBOARD_WORKTREE_ROOT` | `~/.claude/dashboard-worktrees` | Per-task git worktree root |
| `DASHBOARD_HOOKS_SECRET` | auto-generated & persisted | Shared bearer token for `/api/hooks/*`. Persisted to `~/.claude/dashboard-hooks-secret` if unset |
| `DASHBOARD_SECRET_KEY` | auto-generated & persisted | Master key (64 hex chars / 32 bytes) that encrypts plugin **secret** settings at rest (AES-256-GCM). If unset, a key is generated and persisted to `~/.claude/dashboard-secret.key` (mode `0600`). Set a stable value to share encrypted secrets across machines. **Losing it makes stored plugin secrets unreadable — you must re-enter them** |
| `DASHBOARD_MCP_TOKEN` | — | Bearer token for dashboard MCP access |
| `DASHBOARD_AUTH_PLUGIN_SECRET` | — | Shared secret between the core server and an auth plugin (`POST /api/auth/session`). Required when using an auth plugin. Min 32 chars |
| `DASHBOARD_REMOTES_ENABLED` | `false` | Opt-in to binding on a non-loopback address. The dashboard reads sensitive Claude session data — only enable behind a VPN or SSH tunnel |
| `DASHBOARD_RESTART_MODE` | `reexec` | How `POST /api/admin/restart` relaunches the server: `reexec` replaces the process image in-place (no supervisor needed); `exit` exits 0 so an external supervisor (systemd/launchd) restarts it |

**Flag:** `--config <path>` — load a JSON config file whose keys mirror the variables above (without the `DASHBOARD_` prefix, lowercased). Precedence: defaults → JSON file → environment variables.

### Other env vars (not migrated)

A few operational env vars are read directly by their subsystems and are **not** part of the runtime settings store:

| Variable | Default | Description |
|---|---|---|
| `DASHBOARD_CLAUDE_CONFIG_DIRS` | — | Comma-separated extra Claude config dirs to scan for sessions, e.g. `~/.claude-personal,~/.claude-work` |
| `DASHBOARD_INJECT_TOKEN_ROTATE_MS` | `300000` | Discovery bearer-token rotation interval (ms); `<= 0` disables. Previous token honored one extra interval (grace). Read by the `channel`/`live`/`pty-host` child processes (which have no DB access), so it stays a child-process env var rather than a runtime setting |
| `DASHBOARD_SPAWN_COMMAND` | — | Path to a custom spawner binary for the `custom` LLM adapter |

### Injected automatically (do not set by hand)

The orchestrator injects these into spawned stage agents; you rarely set them yourself:

| Variable | Description |
|---|---|
| `DASHBOARD_MCP_URL` | Dashboard MCP URL injected into stage agents |
| `DASHBOARD_STAGE_RUN_ID` | Stage-run ID injected into stage agents |
| `DASHBOARD_TASK_ID` | Task ID injected into stage agents |

## Runtime settings (Settings UI or `dashboard settings` CLI)

These keys live in the database `app_setting` table — the single source of truth is `server/internal/settings/registry.go`. Edit them in the **Settings** UI (the generic **Server** panel, plus the **Plugins** and **Providers** panels) or with the `dashboard settings` CLI.

> The matching `DASHBOARD_*` environment variables that used to set these are **no longer read**. If one is still set, it is ignored and the server logs a warning on boot.

**Apply** is when a change takes effect: `live` applies without a restart; `restart` requires a server restart. Auth mode is `restart` even though it lives here.

| Key | Type | Default | Apply |
|---|---|---|---|
| `auth.mode` | enum (`none`, `plugin`) | `none` | restart |
| `plugins.enabled` | string list (comma) | — | live (managed) |
| `git.allowPush` | bool | `false` | restart |
| `git.allowPull` | bool | `false` | restart |
| `worktree.force` | bool | `true` | restart |
| `sse.intervalMs` | int | `3000` | restart |
| `shutdown.timeoutSeconds` | int | `10` | restart |
| `hooks.debounceMs` | int | `100` | restart |
| `hooks.eventsPerSession` | int | `50` | restart |
| `spawn.rateLimit` | int | `5` | restart |
| `spawn.allowedCommands` | string list (comma) | — | restart |
| `spawn.rateWindowMs` | int | `60000` | restart |
| `inject.rateLimit` | int | `30` | restart |
| `inject.rateWindowMs` | int | `60000` | restart |
| `cost.scanIntervalMs` | int | `300000` | restart |
| `eval.scanIntervalMs` | int | `3600000` | restart |
| `eval.windowHours` | int | `168` | restart |
| `eval.minSamples` | int | `20` | restart |
| `eval.rateDropPP` | float | `15` | restart |
| `eval.stddevK` | float | `3` | restart |

**Apply semantics:** plugin enablement (`plugins.enabled`) applies **live**. Everything else — including `auth.mode` — needs a **server restart** to take effect. The UI marks restart-only changes with a warning.

**Managed keys:** `plugins.enabled` is **read-only via the generic Server panel** — it is edited through the dedicated **Plugins** panel, which starts/stops the affected plugin processes as part of the change. Provider enablement is not a settings key at all; it lives in its own `provider_setting` table and is edited through the **Providers** panel (`/api/providers`).

### CLI / lockout recovery

The `dashboard settings` CLI edits the SQLite database **directly**, so it works even while the server is down — the recovery path when a setting (e.g. an auth mode that requires a plugin you can no longer load) locks you out of the UI.

```bash
dashboard settings list            # all keys with effective values, type, and apply mode
dashboard settings get <key>       # one value (falls back to the registry default)
dashboard settings set <key> <value>
```

Database resolution order: `--db <path>` flag → `DASHBOARD_DB_PATH` → default `~/.claude/dashboard-tasks.db`.

`auth.mode` is an ordinary settings key, so the CLI doubles as lockout recovery:

```bash
# Locked out of a 'plugin' auth mode whose plugin won't load? Reset to no-auth:
dashboard settings set auth.mode none
# Then restart the server (auth.mode is restart-apply).
```

Provider enablement is **not** a `dashboard settings` key — it lives in the `provider_setting` table and is edited through the Providers panel.

### Grants CLI

The `dashboard grants` CLI also operates directly on the SQLite database — no HTTP, no auth gate, no running server required. It is the only user-facing way to create or revoke a capability grant today — the boot backfill migration also inserts `grants` rows, but it does so in raw SQL (`server/internal/db/client.go`, `granted_by = "migration:legacy"`) and bypasses `GrantRepo.Create`'s validation entirely; there is still no HTTP route and no settings page. See [Security](security.md#creating-and-revoking-grants) for what a grant does, why specificity beats mode, and the `ask`-mode limitation.

```bash
dashboard grants add <capability> --pattern '*' [--scope kind:ref] [--mode allow|deny|ask]  # create a grant
dashboard grants list [--capability <name>] [--json]                                       # list grants, newest first
dashboard grants revoke <id>                                                               # tombstone a grant
dashboard grants capabilities                                                              # list grantable capability names
```

`--pattern` is required on `add`: `'*'` covers every value, `'git status*'` is a prefix pattern. A non-`global` `--scope` must carry a ref (`project:/home/me/app`, not bare `project`), and `--expires-in` must be a positive duration — both are rejected at write time rather than stored as a grant that can never apply. `revoke` refuses an already-revoked grant. `add` still writes the grant when no enforcement point reads that capability, but says so on stderr; `list`'s `ENFORCEMENT` column shows the same per row.

Database resolution uses the same `--db` flag / `DASHBOARD_DB_PATH` / default path as the settings CLI.

### Plugin CLI / lockout recovery

The `dashboard plugins` CLI also operates directly on the SQLite database — no HTTP, no auth gate, no running server required.

```bash
dashboard plugins list              # list all discovered plugins with active state
dashboard plugins disable <id>      # set active=false
dashboard plugins enable <id>       # set active=true
```

Database resolution uses the same `--db` flag / `DASHBOARD_DB_PATH` / default path as the settings CLI.

Use `disable` to recover when a broken `auth_provider` plugin prevents the server from booting:

```bash
dashboard plugins disable my-auth-plugin
# Then restart — the change applies on next boot.
```

Lifecycle hooks (activate/deactivate) are **not** run by the CLI — it is a recovery tool, not the normal activate path. `enable` only flips the DB flag; the plugin's full start sequence runs on the next server start.

## LLM adapters

Spawners can use different `adapter_type` values to route stage-agent calls to different LLM backends:

| `adapter_type` | Description | Required env / config |
|---|---|---|
| `claude` (or empty) | Native Claude Code CLI subprocess (default) | None — uses the installed `claude` binary |
| `openai` | OpenAI-compatible HTTP endpoint | `base_url`, `api_key_env`, `default_model` in `AdapterConfig`; the named env var must hold the key |
| `ollama` | Ollama local server | `host`, `default_model` in `AdapterConfig`; defaults to `http://localhost:11434` |
| `anthropic` | Anthropic Messages API via `anthropic-spawner` binary (see below) | `ANTHROPIC_API_KEY` in server env; binary on `PATH` or `DASHBOARD_ANTHROPIC_SPAWNER_CMD` |
| `custom` | Any binary following the custom-exec contract | `spawner.command` must point to the binary |

### `anthropic` adapter

The `anthropic` adapter runs pipeline stage agents and refinement chat against the Anthropic Messages API. It works through an out-of-process binary (`anthropic-spawner`) so that the `anthropic-sdk-go` dependency never enters the server module.

**Prerequisites:**

1. `ANTHROPIC_API_KEY` set in the server environment (inherited by the spawner binary).
2. The `anthropic-spawner` binary on `PATH`, or its absolute path in `DASHBOARD_ANTHROPIC_SPAWNER_CMD`.

**Default model:** `claude-opus-4-8` (can be overridden per-spawner via the model resolution chain described in the [Pipeline stage configuration](#pipeline-stage-configuration) section).

**Building the binary:**

```bash
cd plugins/anthropic-spawner
GOWORK=off go build -o anthropic-spawner .
# Move to somewhere on PATH, e.g.:
mv anthropic-spawner "$(go env GOPATH)/bin/"
```

### OpenAI-compatible gateways (OpenRouter, Together AI, …)

Any multi-model gateway that speaks the OpenAI **chat completions** protocol works with the
existing `openai` adapter — **no new adapter is needed**. This is how you reach "basically any
model" through one provider: point `base_url` at the gateway, name the env var holding its key,
and set a default model in the gateway's string format. The adapter POSTs to
`<base_url>/chat/completions` with an `Authorization: Bearer <key>` header, so `base_url` must
include the version prefix (usually `/v1`).

| Gateway | `base_url` | `api_key_env` | `default_model` (example) |
|---|---|---|---|
| OpenRouter | `https://openrouter.ai/api/v1` | `OPENROUTER_API_KEY` | `anthropic/claude-opus-4`, `meta-llama/llama-3.3-70b-instruct` |
| Together AI | `https://api.together.xyz/v1` | `TOGETHER_API_KEY` | `meta-llama/Llama-3.3-70B-Instruct-Turbo` |
| Inflection | (Inflection inference host) | `INFLECTION_API_KEY` | `inflection_3_productivity` |

Set these three keys in the spawner's `AdapterConfig` with `adapter_type: openai`, and make sure
the named env var is present in the server's environment. The per-spawner model override and the
per-stage model resolution chain still apply, so a single gateway spawner can serve different
models per stage. Streaming (refinement chat) works too — the `openai` adapter parses the standard
OpenAI SSE stream.

> A native `anthropic` adapter exists separately because the Anthropic Messages API is **not**
> OpenAI-compatible. Aggregators almost always are, so they need no dedicated adapter.

## Pipeline stage configuration

Per-stage engine (spawner) and model are stored in the `pipeline_config` table and
exposed via two endpoints:

| Endpoint | Scope |
|---|---|
| `GET`/`PUT /api/pipeline/config` | Global defaults — `stageModels` and `stageSpawners` maps are included in the response |
| `GET`/`PUT /api/projects/{id}/pipeline-config` | Per-project overrides — same `stageModels` / `stageSpawners` shape; empty string means "inherit global" |

Supported stages for both maps: `implementation`, `self_review`, `finalization`.

**Spawner resolution order** (first match wins):

1. `task.spawner_id` (explicit task override)
2. Project `stageSpawner.<stage>` config row
3. Project `default_spawner_id`
4. Global `stageSpawner.<stage>` config row
5. The `is_default` spawner row, falling back to the seeded `claude-default`

**Model resolution order** (first match wins):

1. Spawner's own `ModelOverride` field
2. Task `metadata.model`
3. Project `stageModel.<stage>` config row
4. Global `stageModel.<stage>` config row
5. Coded default per stage

The UI exposes these pickers in **Settings → Pipeline** (global) and on each project's
settings page (per-project).

## Scheduler (pipeline DB config keys)

These keys live in the `pipeline_config` table, not in environment variables. Write them with a direct SQL update or via `PUT /api/config`.

| Key | Default | Description |
|---|---|---|
| `scheduleTickIntervalMs` | `30000` | How often the scheduler checks for due schedules (ms). Minimum enforced: 1000 ms |
| `scheduleCatchup` | `none` | Global fallback catchup policy after downtime. `none` = skip missed windows; `once` = fire a single catch-up run. Overridable per schedule via its `catchup` field |

## Plugin lifecycle

Plugins are discovered from `DASHBOARD_PLUGIN_DIR` and tracked in the database `plugin`
table, which is the source of truth for each plugin's state. The legacy `plugins.enabled`
runtime setting is **superseded** by this table; on first boot the server migrates any
existing `plugins.enabled` value into it automatically.

### Manifest v2 (`plugin.json`)

Alongside the v1 fields (`id`, `name`, `version`, `capabilities`, `addr`, `command`,
`env`), a v2 manifest may declare four optional sections. v1 manifests still load
unchanged.

| Field | Shape | Purpose |
|---|---|---|
| `slots` | list of `{ slot, priority, mode }` | UI contributions into a named host slot. `mode` is `override` (replace) or `extend` (wrap the parent); higher `priority` renders first. Consumed by the frontend in **SP4** |
| `settings` | list of `{ key, type, label, secret, enum? }` | Per-plugin configurable settings. `type` is `string\|url\|int\|bool\|enum`. `secret: true` fields are encrypted at rest and masked in the API |
| `lifecycle` | `{ install, postInstall, activate, deactivate, update, uninstall }` | Optional HTTP hook paths (on the plugin's `addr`) invoked on the matching state transition. An empty path means the transition runs without a hook |
| `permissions` | list of strings | Permissions the plugin requests |

### States

A plugin moves through three states, persisted in the `plugin` table:

- `discovered` — found on disk, not yet installed
- `inactive` — installed but not serving
- `active` — installed and activated

> In SP1 the state machine and its hooks are wired, but **activation's serving effects
> (route mounting and UI slot rendering) are not yet live** — those land with **SP2**
> (route/dispatch) and **SP4** (frontend slots). Activating a plugin today records the
> state transition and runs any configured lifecycle hook, nothing more.

### Lifecycle API

All endpoints live under `/api/plugins` and require the usual session auth. Plugin
addresses, commands, and secrets are never returned to the client.

| Endpoint | Purpose |
|---|---|
| `GET /api/plugins` | List plugins with `state`, capabilities, and `hasSettings` |
| `POST /api/plugins/{id}/install` | `discovered` → `inactive` |
| `POST /api/plugins/{id}/activate` | `inactive` → `active` |
| `POST /api/plugins/{id}/deactivate` | `active` → `inactive` |
| `POST /api/plugins/{id}/uninstall` | back to `discovered` |
| `GET /api/plugins/{id}/settings` | Schema + current values; secret values are returned masked as `********` |
| `PUT /api/plugins/{id}/settings` | Update values; sending the `********` sentinel for a secret leaves the stored value unchanged |

### Secret settings at rest

Settings marked `secret: true` are encrypted with AES-256-GCM using the
`DASHBOARD_SECRET_KEY` master key (see [Bootstrap configuration](#bootstrap-configuration-env--flags))
before they touch the database, and are masked in every API response. Losing the master
key makes existing secrets undecryptable — you must re-enter them.

## Multi-machine (advanced)

Binding to a non-loopback address is gated by the `DASHBOARD_REMOTES_ENABLED` bootstrap variable (see [Bootstrap configuration](#bootstrap-configuration-env--flags)). Remote dashboard instances to aggregate are registered through the API and stored in the database — there is no longer a `DASHBOARD_REMOTES` env var.

> Multi-machine mode requires remote instances to be network-accessible. Use a VPN or SSH tunnel — **never** bind to `0.0.0.0` on an untrusted network. The dashboard reads sensitive Claude session data.

## Eval / drift detection

Passive drift detection measures agent execution quality over time per `(spawner, model, stage)` from the data the pipeline already persists in `stage_run`, and flags degradation against a rolling baseline. No agents are spawned — it only reads existing rows.

Its tuning knobs are runtime settings (`eval.scanIntervalMs`, `eval.windowHours`, `eval.minSamples`, `eval.rateDropPP`, `eval.stddevK`) — see the [Runtime settings](#runtime-settings-settings-ui-or-dashboard-settings-cli) table.

> Drift is detected by comparing the recent window against the immediately preceding baseline window. Because the baseline is built from prior metric snapshots, no alerts fire until roughly `2 × eval.windowHours` of history exists — this cold-start gap is expected. Alerts surface at `GET /api/eval/drift` and in the dashboard's Eval view.
