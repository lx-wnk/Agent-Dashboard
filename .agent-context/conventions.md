# Key Conventions

- **Path alias:** `@/*` maps to `./src/*` (configured in `tsconfig.json` and `vite.config.ts`).
- **Network binding:** Server binds to `127.0.0.1` only — never expose to network (reads sensitive session data). **Multi-machine mode** (`DASHBOARD_REMOTES` env var) requires remote instances to be network-accessible; use a VPN or SSH tunnel — never bind to `0.0.0.0` on an untrusted network.
- **Dual persistence model:** Agent monitoring is filesystem-derived (no database); the task pipeline uses SQLite at `~/.claude/dashboard-tasks.db` (override via `DASHBOARD_DB_PATH`; see ADR-0001). One deliberate crossing: `server/agentMerger.ts` performs an opportunistic read-only pipeline lookup (`enrichWithPipelineTask`) to annotate agents with their linked task ID/title. This is one-way (pipeline → agent annotation only) and fails gracefully if the DB is unavailable.
- **Subagents discovery:** Read from `~/.claude/projects/{encoded_path}/{sessionId}/subagents/*.jsonl`.
- **Cost estimation:** Uses the `MODEL_PRICING` lookup table in `server/pricing.ts`.
- **Platform support:** macOS and Linux. `server/systemMonitor.ts` uses `top` on macOS and `/proc/stat` on Linux for CPU; `server/processScanner.ts` uses `lsof` on macOS and `/proc/<pid>/cwd` on Linux. Windows is unsupported.

## Pipeline Env Vars

| Var                              | Purpose                                                                                                       |
| -------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| `DASHBOARD_DB_PATH`              | SQLite path                                                                                                   |
| `DASHBOARD_WORKTREE_ROOT`        | Per-task git worktree root, default `~/.claude/dashboard-worktrees`                                           |
| `DASHBOARD_STAGE_RUN_ID`         | Injected into spawned stage agents for the channel bridge                                                     |
| `DASHBOARD_TASK_ID`              | Injected into spawned stage agents for the channel bridge                                                     |
| `DASHBOARD_MCP_TOKEN`            | Injected for MCP callback access                                                                              |
| `DASHBOARD_MCP_URL`              | Injected for MCP callback access                                                                              |
| `DASHBOARD_HOST`                 | Bind address, default `127.0.0.1`; logs a security warning if non-loopback                                    |
| `DASHBOARD_SSE_INTERVAL_MS`      | Agent SSE broadcast interval ms, default `3000`                                                               |
| `DASHBOARD_SPAWN_RATE_LIMIT`     | Max user-initiated spawns per window, default `5`; must be positive integer                                   |
| `DASHBOARD_SPAWN_RATE_WINDOW_MS` | Spawn rate-limit window ms, default `60000`; must be positive integer                                         |
| `DASHBOARD_ALLOW_GIT_PUSH`       | `true` or `false`, default `false`; when `true`, removes the global `git push` filter from spawned-agent allow-lists. Per-task override: `metadata.allowGitPush=true`. |
| `DASHBOARD_HOOKS_SECRET`         | Shared bearer token for `/api/hooks/event`; recommended when hooks script runs outside localhost |
| `DASHBOARD_HOOKS_DEBOUNCE_MS`    | Debounce window before SSE rescan after a hook event, default `100`ms |
| `DASHBOARD_ALLOW_GIT_PULL`       | `true` or `false`, default `false`; enables `POST /api/tasks/:id/git-action` with `action:'pull'` (git pull --ff-only on task worktree) |
| `DASHBOARD_CLAUDE_CONFIG_DIRS`   | Comma-separated list of Claude config directories to search for session JSONL files, e.g. `~/.claude-personal,~/.claude-work`. Searched before auto-detection. Useful when the dashboard process is not started with `CLAUDE_CONFIG_DIR` set, or on shared machines with multiple profiles. |

## Compaction Preservation

When compacting context, always preserve:

- List of modified/created files in this session
- Active test/lint commands and their last results
- Unfinished tasks and next steps
