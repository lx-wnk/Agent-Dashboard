# Key Conventions

- **Path alias:** `@/*` maps to `./src/*` (configured in `tsconfig.json` and `vite.config.ts`).
- **Network binding:** Server binds to `127.0.0.1` only — never expose to network (reads sensitive session data). **Multi-machine mode** (`DASHBOARD_REMOTES` env var) requires remote instances to be network-accessible; use a VPN or SSH tunnel — never bind to `0.0.0.0` on an untrusted network.
- **Dual persistence model:** Agent monitoring is filesystem-derived (no database); the task pipeline uses SQLite at `~/.claude/dashboard-tasks.db` (override via `DASHBOARD_DB_PATH`; see ADR-0001). One deliberate crossing: `server/agentMerger.ts` performs an opportunistic read-only pipeline lookup (`enrichWithPipelineTask`) to annotate agents with their linked task ID/title. This is one-way (pipeline → agent annotation only) and fails gracefully if the DB is unavailable.
- **Subagents discovery:** Read from `~/.claude/projects/{encoded_path}/{sessionId}/subagents/*.jsonl`.
- **Cost estimation:** Uses the `MODEL_PRICING` lookup table in `server/pricing.ts`.
- **Spawner env-merge precedence:** Custom-spawner `env` (from the `spawners` table) is injected into the process environment first. Dashboard-controlled vars (`DASHBOARD_*`, `CLAUDE_*`) are then overlaid and always win. `DASHBOARD_JWT_SECRET` and `DASHBOARD_HOOKS_SECRET` are never forwarded to spawned agents regardless of what a custom spawner's `env` map declares. See [ADR-0003](../docs/architecture/adr/0003-pluggable-spawners.md).
- **Platform support:** macOS and Linux. `server/systemMonitor.ts` uses `top` on macOS and `/proc/stat` on Linux for CPU; `server/processScanner.ts` uses `lsof` on macOS and `/proc/<pid>/cwd` on Linux. Windows is unsupported.
- **Live prompt injection requires tmux (conditional hard requirement).** Pushing a prompt into a *running interactive* Claude session is only possible by injecting real keyboard input into its pty — MCP has no inbound primitive for this (go-sdk `ss.Log` is gated behind a client `setLevel` Claude never sends; TIOCSTI is disabled by default on Linux ≥6.2). The only robust, non-root mechanism is a terminal multiplexer's send-input command. The dashboard uses `tmux send-keys`; the session must run inside tmux (start it via `scripts/claude-tmux-channel.sh`), and `tmux` must be on the PATH of the host running the dashboard server. Without tmux this single feature is unavailable — sessions are then resume-only (the UI signals this with an amber ⤳). tmux is **not** required for anything else (monitoring, pipeline, agent→dashboard replies/permissions all work without it). GNU screen (`screen -X stuff`) is an equivalent mechanism not currently implemented.

## Pipeline Env Vars

| Var                              | Purpose                                                                                                       |
| -------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| `DASHBOARD_DB_PATH`              | SQLite path                                                                                                   |
| `DASHBOARD_WORKTREE_ROOT`        | Per-task git worktree root, default `~/.claude/dashboard-worktrees`                                           |
| `DASHBOARD_STAGE_RUN_ID`         | Injected into spawned stage agents for the channel bridge                                                     |
| `DASHBOARD_TASK_ID`              | Injected into spawned stage agents for the channel bridge                                                     |
| `DASHBOARD_MCP_TOKEN`            | Injected for MCP callback access                                                                              |
| `DASHBOARD_MCP_URL`              | Injected for MCP callback access                                                                              |
| `DASHBOARD_HOST`                 | Bind address, default `127.0.0.1`; non-loopback address causes boot failure unless `DASHBOARD_REMOTES_ENABLED=true` |
| `DASHBOARD_REMOTES_ENABLED`      | `true` or `false`, default `false`; opt-in to binding on a non-loopback address (use a VPN or SSH tunnel — never expose to an untrusted network) |
| `DASHBOARD_SSE_INTERVAL_MS`      | Agent SSE broadcast interval ms, default `3000`                                                               |
| `DASHBOARD_COST_SCAN_INTERVAL_MS` | Interval ms for the server-side cost-history scan that fills `agent_cost_trends` (read by the Cost Analytics view). Default `300000` (5 min). A scan also always runs once at boot. Set `<= 0` for boot-scan-only (no periodic loop) — note this does NOT fully disable scanning. The scan reads session JSONL across all `DASHBOARD_CLAUDE_CONFIG_DIRS` and providers; it never has agents write cost data. |
| `DASHBOARD_SPAWN_RATE_LIMIT`     | Max user-initiated spawns per window, default `5`; must be positive integer                                   |
| `DASHBOARD_SPAWN_RATE_WINDOW_MS` | Spawn rate-limit window ms, default `60000`; must be positive integer                                         |
| `DASHBOARD_ALLOW_GIT_PUSH`       | `true` or `false`, default `false`; when `true`, removes the global `git push` filter from spawned-agent allow-lists. Per-task override: `metadata.allowGitPush=true`. |
| `DASHBOARD_HOOKS_SECRET`         | Shared bearer token for all `/api/hooks/*` endpoints; always required — if unset, `config.Load` auto-generates and persists a secret to `~/.claude/dashboard-hooks-secret`. Set explicitly via env var or config file to keep the secret stable across restarts. |
| `DASHBOARD_HOOKS_DEBOUNCE_MS`    | Debounce window before SSE rescan after a hook event, default `100`ms |
| `DASHBOARD_ALLOW_GIT_PULL`       | `true` or `false`, default `false`; enables `POST /api/tasks/:id/git-action` with `action:'pull'` (git pull --ff-only on task worktree) |
| `DASHBOARD_CLAUDE_CONFIG_DIRS`   | Comma-separated list of Claude config directories to search for session JSONL files, e.g. `~/.claude-personal,~/.claude-work`. Searched before auto-detection. Useful when the dashboard process is not started with `CLAUDE_CONFIG_DIR` set, or on shared machines with multiple profiles. |
| `DASHBOARD_SPAWNER_ALLOWED_COMMANDS` | Comma-separated list of additional command names or absolute path prefixes that are permitted in the `spawners.command` field, e.g. `my-claude-wrapper,/opt/company/bin`. Extends the conservative built-in allow-list without requiring a code change. Optional. |

## Legacy Adapter Migration (post-merge)

- `DASHBOARD_SPAWN_COMMAND` is deprecated. On first boot it is migrated to a Spawner row with `slug='imported-custom'` (`adapter_type='custom'`); after migration the env var has no runtime effect. Prefer creating Custom-adapter spawners via the UI (`/settings/spawners`) or `POST /api/spawners`.
- `adapter-config.json` migration is idempotent and only seeds rows that are missing by reserved slug: `imported-ollama`, `imported-openai`, `imported-custom`. Editing the JSON file post-migration has no effect — edit the spawner row instead (UI or `PATCH /api/spawners/:id`).
- The legacy `Adapters.Default` key has no equivalent in the new model. If present in `adapter-config.json` it surfaces as `slog.Warn` on boot; pick a per-project `default_spawner_id` (or per-task `spawner_id`) explicitly.

See [ADR-0003 section A](../docs/architecture/adr/0003-pluggable-spawners.md) for full rationale.

## Compaction Preservation

When compacting context, always preserve:

- List of modified/created files in this session
- Active test/lint commands and their last results
- Unfinished tasks and next steps
