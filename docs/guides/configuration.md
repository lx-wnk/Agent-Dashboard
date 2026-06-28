# Configuration

All configuration is via environment variables. A documented template lives in [`.env.dist`](../../.env.dist) — copy it to `.env` and edit, or export the variables directly.

```bash
cp .env.dist .env
```

A `.env` file in the working directory is loaded automatically at startup for both
`task dev` and `./bin/agent-dashboard serve` — no manual sourcing needed. An explicit
shell `export` always wins over a value in `.env`. The file is read from the current
working directory (the repository root in the standard layout); run the binary from
there, or export the variables, if you keep `.env` elsewhere.

## Core

| Variable | Default | Description |
|---|---|---|
| `DASHBOARD_PORT` | `13120` | HTTP server port |
| `DASHBOARD_HOST` | `127.0.0.1` | Bind address. A non-loopback address fails to boot unless `DASHBOARD_REMOTES_ENABLED=true` |
| `DASHBOARD_DB_PATH` | `~/.claude/dashboard-tasks.db` | SQLite path for the task pipeline |
| `DASHBOARD_WORKTREE_ROOT` | `~/.claude/dashboard-worktrees` | Per-task git worktree root |
| `DASHBOARD_SSE_INTERVAL_MS` | `3000` | Agent SSE broadcast interval (ms) |
| `DASHBOARD_CLAUDE_CONFIG_DIRS` | — | Comma-separated extra Claude config dirs to scan for sessions, e.g. `~/.claude-personal,~/.claude-work` |

## Authentication

| Variable | Default | Description |
|---|---|---|
| `DASHBOARD_JWT_SECRET` | auto-generated (ephemeral) | Secret for signing JWT session tokens (min 32 chars). Set a stable value to survive restarts |
| `DASHBOARD_GITHUB_CLIENT_ID` | — | GitHub OAuth app client ID. Omit for loopback dev — auth bypass activates automatically |
| `DASHBOARD_GITHUB_CLIENT_SECRET` | — | GitHub OAuth app client secret |

> When no GitHub OAuth is configured **and** the server is on loopback (the default), all API requests are allowed without login. This is safe for a single-user developer machine but means full local trust. For shared or multi-user machines, configure GitHub OAuth to enforce per-user authentication. See [Security](security.md).

## Spawning & permissions

| Variable | Default | Description |
|---|---|---|
| `DASHBOARD_SPAWN_RATE_LIMIT` | `5` | Max user-initiated spawns per window |
| `DASHBOARD_SPAWN_RATE_WINDOW_MS` | `60000` | Spawn rate-limit window (ms) |
| `DASHBOARD_INJECT_RATE_LIMIT` | `30` | Max live message injections per user per window (`429` on exceed) |
| `DASHBOARD_INJECT_RATE_WINDOW_MS` | `60000` | Inject rate-limit window (ms) |
| `DASHBOARD_INJECT_TOKEN_ROTATE_MS` | `300000` | Discovery bearer-token rotation interval (ms); `<= 0` disables. Previous token honored one extra interval (grace) |
| `DASHBOARD_ALLOW_GIT_PUSH` | `false` | Allow `git push` in spawned agents. Per-task override: `metadata.allowGitPush=true` |
| `DASHBOARD_ALLOW_GIT_PULL` | `false` | Enable `POST /api/tasks/:id/git-action` with `action:'pull'` (ff-only) |
| `DASHBOARD_SPAWNER_ALLOWED_COMMANDS` | — | Comma-separated extension of the `spawners.command` allow-list: bare entries add permitted bare names, absolute entries add trusted bin directories. Absolute commands must resolve (via `EvalSymlinks`) under a trusted dir — the old "outside /tmp" rule is replaced by this allow-list |

## Channel & hooks (injected automatically)

These are normally injected into spawned stage agents by the orchestrator — you rarely set them by hand.

| Variable | Default | Description |
|---|---|---|
| `DASHBOARD_MCP_TOKEN` | — | Bearer token for dashboard MCP access |
| `DASHBOARD_MCP_URL` | — | Dashboard MCP URL injected into stage agents |
| `DASHBOARD_STAGE_RUN_ID` | — | Stage-run ID injected into stage agents |
| `DASHBOARD_TASK_ID` | — | Task ID injected into stage agents |
| `DASHBOARD_HOOKS_SECRET` | auto-generated & persisted | Shared bearer token for `/api/hooks/*`. Persisted to `~/.claude/dashboard-hooks-secret` if unset |
| `DASHBOARD_HOOKS_DEBOUNCE_MS` | `100` | Debounce before SSE rescan after a hook event |
| `DASHBOARD_HOOK_EVENTS_PER_SESSION` | `50` | Max recent lifecycle-hook events kept in memory per session for the agent-modal **Hook events** view. Must be positive |

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

## Multi-machine (advanced)

| Variable | Default | Description |
|---|---|---|
| `DASHBOARD_REMOTES_ENABLED` | `false` | Opt-in to binding on a non-loopback address |
| `DASHBOARD_REMOTES` | — | Remote dashboard instances to aggregate |

> Multi-machine mode requires remote instances to be network-accessible. Use a VPN or SSH tunnel — **never** bind to `0.0.0.0` on an untrusted network. The dashboard reads sensitive Claude session data.

## Eval / drift detection

Passive drift detection measures agent execution quality over time per `(spawner, model, stage)` from the data the pipeline already persists in `stage_run`, and flags degradation against a rolling baseline. No agents are spawned — it only reads existing rows.

| Variable | Default | Description |
|---|---|---|
| `DASHBOARD_EVAL_SCAN_INTERVAL_MS` | `3600000` | Drift-scan interval (ms). `<= 0` runs a single boot scan only |
| `DASHBOARD_EVAL_WINDOW_HOURS` | `168` | Length of the recent window (hours). The baseline window is the preceding window of equal length, so `2 × window` of history must accumulate before any alert can fire |
| `DASHBOARD_EVAL_MIN_SAMPLES` | `20` | Minimum stage-run sample count on both the recent and baseline side; thinner data is suppressed |
| `DASHBOARD_EVAL_RATE_DROP_PP` | `15` | Percentage-point worsening of a rate metric (e.g. success rate) required to raise an alert |
| `DASHBOARD_EVAL_STDDEV_K` | `3` | Standard-deviation multiplier for continuous metrics: a recent mean above `baseline_mean + k × baseline_stddev` raises an alert |

> Drift is detected by comparing the recent window against the immediately preceding baseline window. Because the baseline is built from prior metric snapshots, no alerts fire until roughly `2 × DASHBOARD_EVAL_WINDOW_HOURS` of history exists — this cold-start gap is expected. Alerts surface at `GET /api/eval/drift` and in the dashboard's Eval view.
