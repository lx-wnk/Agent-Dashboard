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
time against a conservative allow-list:

- Exact names: `claude`, `claude-code`
- Prefix `npx` (for `npx @anthropic-ai/claude-code`)
- Absolute paths that resolve under the current user's home directory
  (`~/bin`, `~/.local/bin`, standard Homebrew/nix prefixes) or under
  `/usr/local/bin`, `/opt/homebrew/bin`
- Hard-blocked: any path under `/tmp`, `/var/tmp`, or any world-writable
  directory

The allow-list can be extended at server startup via
`DASHBOARD_SPAWNER_ALLOWED_COMMANDS` (comma-separated command names or
absolute path prefixes) without code changes.

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
