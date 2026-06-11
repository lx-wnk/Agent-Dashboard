# ADR-0003: Pluggable Spawners for Stage Agents

**Status:** Accepted
**Date:** 2026-05-20

## Context

Every stage agent that the pipeline spawns executes a hard-coded `claude`
binary. There is no mechanism to substitute a different CLI, pass extra
arguments, set environment variables, or select a non-default model at
the project or task level. This blocks three concrete use-cases:

1. **Multi-profile setups** — users running separate `~/.claude-personal`
   and `~/.claude-work` directories need different binaries or
   `CLAUDE_CONFIG_DIR` values per project without editing the dashboard's
   own config.
2. **Model overrides** — some tasks benefit from a cheaper/faster model;
   passing `--model sonnet` today requires patching `agentSpawner.go`.
3. **Wrapper scripts** — teams want to intercept the `claude` invocation
   (cost attribution, compliance logging, air-gapped proxies) via a thin
   wrapper that accepts the same CLI contract.

Solving this piecemeal (per-task env overrides, a model field on the task,
a global binary env var) would scatter the configuration surface and make
it impossible to share a spawner definition across many tasks and projects.

## Decision

Introduce a first-class **Spawner** entity backed by a `spawners` SQLite
table. Projects and tasks each carry a nullable `spawner_id` foreign key.
Effective spawner for a stage agent is resolved once at spawn time by
`server/internal/services/spawner_resolver.go`:

```
task.spawner_id ?? task.project.default_spawner_id ?? claude-default
```

A seed row with `id = 'claude-default'` and `builtin = true` is
inserted at migration time and cannot be deleted. It represents the
current behaviour (`claude` binary, no extra args, no custom env, no model
override) so existing tasks are unaffected.

### `spawners` table shape

| Column | Type | Notes |
|---|---|---|
| `id` | TEXT PK | human-readable slug, e.g. `personal-claude` |
| `label` | TEXT | display name |
| `command` | TEXT | binary path or name, validated against allow-list |
| `args` | TEXT (JSON) | extra CLI args, e.g. `["--model","claude-opus-4-5"]` |
| `env` | TEXT (JSON) | key-value pairs injected into the spawn env |
| `builtin` | BOOLEAN | `true` only for `claude-default`; blocks deletion |
| `created_at` | DATETIME | |

### Resolution and env-merge precedence

Resolution walks `task → project → claude-default` and stops at the
first non-null spawner_id. Resolution happens once, immediately before
`exec`, inside `server/internal/pipeline/spawner.go`. The resolved
spawner's `env` map is applied first; dashboard-controlled vars
(`DASHBOARD_*`, `CLAUDE_*`) are then overlaid so they always win.
`DASHBOARD_JWT_SECRET` and `DASHBOARD_HOOKS_SECRET` are never forwarded
to spawned agents regardless of what a custom spawner's `env` map
declares.

### Security

**Command allow-list.** The `command` field is validated at create/update
time by `services.ValidateSpawnerCommand` (in
`server/internal/services/spawn_policy.go`, co-located with the cwd
`SpawnPolicy` and reusing its `canonicalize`/`isUnder` helpers). The same
function is the single authority for both the spawner CRUD handlers and the
agent spawn path (`api/agents`), eliminating the former lateral
`api/agents → api/spawners` import.

The rule is an **allow-list**, not a blacklist:

- Bare names: `claude`, `claude-code`, `npx` (plus bare entries of
  `DASHBOARD_SPAWNER_ALLOWED_COMMANDS`)
- Absolute paths must `EvalSymlinks`-resolve (the file must exist) **and** the
  resolved binary's parent directory must lie under a **trusted bin dir**:
  `/usr/bin`, `/bin`, `/usr/local/bin`, `/opt/homebrew/bin`, `~/.local/bin`,
  the resolved directory of the `claude` binary on `PATH`, plus absolute-path
  entries of `DASHBOARD_SPAWNER_ALLOWED_COMMANDS`
- Anything else — unresolvable paths, or resolved paths outside every trusted
  dir — is hard-rejected with a specific reason

Resolving symlinks **before** the trust check closes the previous
symlink-into-`/tmp` bypass: the old blacklist allowed any path that was not
literally under `/tmp`/`/var/tmp` and never followed symlinks, so a trusted-
looking path symlinked to a `/tmp` target slipped through.

The trusted-dir set can be extended at server startup via
`DASHBOARD_SPAWNER_ALLOWED_COMMANDS` (comma-separated; bare names add to the
bare allow-list, absolute paths add trusted bin dirs) without code changes.

> **Migration note (allow-list tightening).** Existing spawner rows are not
> re-validated on upgrade, but the next create/update of a row with an
> absolute `command` outside a trusted bin dir will now be rejected. Operators
> relying on a custom binary location must add its directory to
> `DASHBOARD_SPAWNER_ALLOWED_COMMANDS`.

**CRUD gating.** Creating, updating, and deleting spawners requires the
`keys:manage` MCP scope (the highest scope tier). Arbitrary spawner
configuration is equivalent to arbitrary binary execution on the server
machine; it must be guarded at the same level as API key management.

**Channel bridge.** A custom spawner binary must accept the same CLI
contract as `claude` for the dashboard-channel MCP to function
(`DASHBOARD_MCP_TOKEN` / `DASHBOARD_MCP_URL` injected into env,
`--mcp-server` flag accepted). If the binary does not support the channel
bridge, the stage agent simply cannot call back into the dashboard — it
will run to completion without the permission gate and tool-approval flow.
The orchestrator handles this gracefully: if no permission requests arrive
and the PID exits normally, completion detection proceeds as usual.

## Consequences

### Positive

- `claude-default` preserves current behaviour exactly — no migration
  needed for existing projects and tasks (`project_id` / `spawner_id`
  NULL columns resolve to the built-in).
- Spawner configuration is shared across many tasks via the project
  default; changing the project's `default_spawner_id` retroactively
  affects all tasks that do not override it.
- Model overrides, custom env, and wrapper binaries are expressed in one
  place and are inspectable in the UI, rather than being hidden in
  per-task metadata or environment files.
- The conservative allow-list and `keys:manage` gate make the RCE surface
  explicit and auditable.

### Negative / Trade-offs

- Spawner misconfiguration (wrong binary path, incompatible CLI contract)
  fails at spawn time with a `fail` transition, not at creation time.
  Validation is structural (allow-list, JSON parse) not behavioural
  (we do not test-exec the binary on save).
- Custom spawner env can shadow `CLAUDE_*` vars set by the user's shell
  profile — the env-merge precedence makes this predictable but it can
  surprise users who expect their shell env to take effect inside stage
  agents.
- Spawner deletion is blocked when any project or task references the
  spawner, or when `builtin = true`. This is intentional (referential
  integrity) but means cleanup requires updating referencing records first.

### Follow-ups

- `spawner_resolver.go` in `server/internal/services/` is the canonical
  resolution implementation. Any change to resolution order must update
  this ADR.
- The allow-list default is conservative by design. If production use
  reveals legitimate binaries outside the default set, extend via
  `DASHBOARD_SPAWNER_ALLOWED_COMMANDS` before widening the code default.
- A future ADR may introduce per-spawner resource budgets (max token
  spend, wall-clock cap) if cost attribution becomes a requirement.

## Alternatives Considered

- **Per-task `spawnCommand` metadata field** — rejected: no sharing across
  tasks, duplicates configuration, no CRUD UI, no allow-list enforcement.
- **Global `DASHBOARD_CLAUDE_BINARY` env var** — rejected: single global
  value, cannot vary per project or task; does not address args, env, or
  model overrides.
- **Stage-level spawner config** — rejected: complexity is unjustified for
  the current use-cases; stage agents within one task should use the same
  binary identity.
- **Inline `env` on the task record** — already exists (`task.metadata`).
  Retained for one-off overrides; the spawner entity is the right home for
  reusable, named configurations.

## A: Adapter/Spawner Merge (2026-05)

### Why

The original ADR introduced `Spawner` (HOW to invoke a binary — command, args,
env, model override) while a separate global `AdapterConfig` singleton in
`config/` still configured WHICH LLM backend the pipeline talked to (claude
native, ollama, openai, custom shim). The two concepts overlapped and forced
operators to know which knob applied: `Spawner` for claude, `AdapterConfig`
for everything else. Per-task / per-project selection only worked for the
Spawner half — the adapter half was a single global value with no UI.

### Resolution

Each `Spawner` row now carries `adapter_type` (enum: `claude`, `ollama`,
`openai`, `custom`) plus a free-form `adapter_config map[string]string` for
adapter-specific keys (`host`, `default_model`, `base_url`, `api_key_env`).
The catalog (`pipeline.AvailableAdapters`) is the descriptor of "what
adapter types exist and what config keys each takes" — consumed by the UI
to render a dynamic editor and by the factory to validate input.

The resolution chain is unchanged from the original ADR:

```
task.spawner_id ?? task.project.default_spawner_id ?? claude-default
```

### Dispatch

`stage_handlers.go::Execute` reads the resolved row's `adapter_type`:

- `claude` (or empty) → native subprocess via `SpawnStageAgent` (existing path).
- `ollama` / `openai` / `custom` → adapter built via
  `pipeline.NewLLMSpawnerFromSpawner(row)`, then `LLMSpawner.Spawn(...)`.
  Custom adapters use `CustomCommandSpawner` keyed off the row's `command`
  column; the factory rejects `custom` rows missing a command.

### Migration

`migrateAdapterConfigToSpawners` runs once on boot in `di_seed.go`. Legacy
`adapter-config.json` and the `DASHBOARD_SPAWN_COMMAND` env var are read
and seeded into Spawner rows with reserved slugs:

- `imported-ollama` — created when `Adapters.Ollama.Host` is non-empty.
- `imported-openai` — created when `Adapters.OpenAI.BaseURL` is non-empty.
- `imported-custom` — created when `DASHBOARD_SPAWN_COMMAND` is non-empty.

A legacy `Adapters.Default` value is surfaced via `slog.Warn` because the
new model has no concept of a global default — operators must assign
`default_spawner_id` per project, or `spawner_id` per task. The migration is
idempotent (slug-keyed); editing `adapter-config.json` after the first boot
has no effect.

### Deprecated

- `config.AdapterConfig` types remain only to deserialize the legacy file
  during migration. New code MUST NOT read or write them at runtime.
- `DASHBOARD_SPAWN_COMMAND` has no runtime effect after the first boot.
- HTTP routes `POST /api/adapters/current`, `GET /api/adapters/current`,
  `GET /api/settings/adapters`, `PUT /api/settings/adapters` return
  HTTP 410 Gone with a migration pointer; `GET /api/adapters` remains as
  the read-only catalog endpoint.
