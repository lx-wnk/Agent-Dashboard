# Configuration

All configuration is via environment variables. A documented template lives in [`.env.dist`](../../.env.dist) — copy it to `.env` and edit, or export the variables directly.

```bash
cp .env.dist .env
```

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
| `DASHBOARD_ALLOW_GIT_PUSH` | `false` | Allow `git push` in spawned agents. Per-task override: `metadata.allowGitPush=true` |
| `DASHBOARD_ALLOW_GIT_PULL` | `false` | Enable `POST /api/tasks/:id/git-action` with `action:'pull'` (ff-only) |
| `DASHBOARD_SPAWNER_ALLOWED_COMMANDS` | — | Comma-separated extra command names / path prefixes permitted in the `spawners.command` field |

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

## Multi-machine (advanced)

| Variable | Default | Description |
|---|---|---|
| `DASHBOARD_REMOTES_ENABLED` | `false` | Opt-in to binding on a non-loopback address |
| `DASHBOARD_REMOTES` | — | Remote dashboard instances to aggregate |

> Multi-machine mode requires remote instances to be network-accessible. Use a VPN or SSH tunnel — **never** bind to `0.0.0.0` on an untrusted network. The dashboard reads sensitive Claude session data.
