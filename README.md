# Claude Agent Overview

Real-time monitoring and control dashboard for locally running Claude Code agents. Displays token usage, costs, tool activity, tasks, and subagents — and lets you send instructions or spawn new agents directly from the browser.

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](./LICENSE)
[![Go 1.26](https://img.shields.io/badge/go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Vue 3](https://img.shields.io/badge/vue-3-4FC08D?logo=vue.js&logoColor=white)](https://vuejs.org/)
[![pnpm](https://img.shields.io/badge/pnpm-10-F69220?logo=pnpm&logoColor=white)](https://pnpm.io/)
[![Status](https://img.shields.io/badge/status-active-brightgreen)](https://github.com/lx-wnk/Agent-Dashboard)

## Use this if…

- You run multiple Claude Code agents in parallel and want a single pane of glass for all of them
- You want to track token usage and API-equivalent cost across sessions without sending data to a third-party SaaS
- You need a structured multi-stage task pipeline (concept → implementation → review → done) with permission gates and parallel execution
- You want to send follow-up instructions to running agents, spawn new ones, and get browser push notifications — all from a local web UI
- Privacy matters: the dashboard is **local-first**, binds to `127.0.0.1` only, and makes no outbound connections unless you opt in to an integration

> 📄 [Privacy policy](./PRIVACY.md)

## Features

- **Real-time agent monitoring via SSE** — all running Claude Code processes with status, model, tokens, cost, uptime (list, card, and kanban views)
- **Chat-style session view** — full conversation transcript with markdown rendering, collapsible tool groups, inline task checklists, sub-agent badges
- **Channel control** — send follow-up instructions and /btw interrupts to running agents via MCP Channels
- **Agent spawning** — start new Claude agents with custom prompts, models, and system prompts from the UI
- **Task pipeline** — multi-stage agentic workflow (concept → backlog → implementation → self_review → finalization → done) with permission gates per stage; permission template shortcuts (`feature_implementation`, `research_only`, `test_only`, `review_only`)
- **Refinement Chat** — interactive concept refinement before pipeline tasks enter implementation
- **Authenticated MCP endpoint (19 tools, 4 scopes)** for external agent control
- **API key management** — generate and revoke scoped Bearer tokens in Settings
- **GitHub OAuth + JWT session auth**
- **FTS5 Full-Text Search** — spotlight search (Cmd+K) across tasks and agents
- **Web Push notifications** (VAPID)
- **Historical Session Import** — streaming SSE progress for importing past session cost data
- **Remote registrations** — aggregate agents from remote dashboard instances
- **Cross-linking** between agent sessions and pipeline tasks
- **N-gram Pattern Discovery** — surfaces common tool-use sequences across sessions
- **Memory Browser** — read and write Claude agent memory files from the dashboard
- **Python Statusline** — shell PS1 integration (`scripts/statusline.py`)
- **Slash Commands** — `/spawn`, `/grant`, `/cancel`, `/status`, `/session`
- **Notification Settings UI** — per-event enable/disable toggles with per-channel routing (webhook, email, browser, system); delivery config for webhook URL and email recipient
- **LLM Adapter Settings** — switch active backend pipeline adapter (Claude CLI, etc.) with config key reference table
- **Plugin Status** — view loaded sidecar plugins, their capabilities, and base URLs; add plugins via `DASHBOARD_PLUGIN_DIR`
- **Webhooks, email, browser, and system notifications**
- **Dark/light theme, PWA support**

## Prerequisites

| Tool | Install |
|---|---|
| Go 1.26+ | `brew install go` |
| Task (Taskfile runner) | `brew install go-task/tap/go-task` |
| air (hot-reload) | `go install github.com/air-verse/air@latest` |
| golangci-lint | `brew install golangci-lint` |
| Node.js 22+ + pnpm | [pnpm.io/installation](https://pnpm.io/installation) |
| Claude Code | Required — dashboard reads live agent sessions |

**Platform:** macOS and Linux only. Windows is unsupported.

> **PATH note:** `go install` puts binaries in `$(go env GOPATH)/bin` (typically `~/go/bin`). Add it to your shell:
> ```bash
> echo 'export PATH="$PATH:$(go env GOPATH)/bin"' >> ~/.zshrc && source ~/.zshrc
> ```

## Installation

```bash
git clone https://github.com/lx-wnk/Agent-Dashboard.git
cd Agent-Dashboard

# Install frontend dependencies
pnpm install
```

## Running (Development)

**Backend only (most common):**
```bash
task dev
```
Starts the Go server with `air` hot-reload on port 13120. The last-built Vue SPA is served as embedded static files. Open [http://localhost:13120](http://localhost:13120) — running Claude Code agents appear automatically.

> **Auth:** When no GitHub OAuth is configured and the server is on loopback (default), all API requests are allowed without login.
>
> **Security note:** Auth bypass means full local trust — any process with access to `127.0.0.1:13120` can create API keys, spawn agents, and read session data. This is safe for single-user developer machines. For shared machines or multi-user environments, configure GitHub OAuth (`DASHBOARD_GITHUB_CLIENT_ID` + `DASHBOARD_GITHUB_CLIENT_SECRET`) to enforce per-user authentication.

**With frontend hot-reload** (run in two terminals when iterating on UI):
```bash
# Terminal 1
task dev       # Go backend on :13120

# Terminal 2
pnpm dev       # Vite dev server on :5173, proxies /api → :13120
```

`air` only watches `.go` files. Frontend changes require `pnpm build` (or use `pnpm dev` for HMR).

## Building (Production)

```bash
task build    # Compile Go server binary → bin/agent-dashboard
pnpm build    # Build Vue SPA → server/frontend/dist/
```

The binary embeds the Vue SPA via `go:embed`. Deploy by copying `bin/agent-dashboard` — no Node.js needed at runtime.

## Running (Production)

```bash
export DASHBOARD_JWT_SECRET=<min-32-char-secret>
export DASHBOARD_GITHUB_CLIENT_ID=<client-id>
export DASHBOARD_GITHUB_CLIENT_SECRET=<client-secret>
./bin/agent-dashboard serve
```

## Development Commands

### Go / Backend

| Command | Description |
|---|---|
| `task dev` | Go server with air hot-reload + Vite SPA on :13120 |
| `task build` | Compile binary → `bin/agent-dashboard` |
| `task test` | Run all tests (both modules, race detector) |
| `task lint` | golangci-lint |
| `task generate` | Regenerate Wire DI + ent schemas |
| `task fmt` | gofmt all Go code |

### Frontend

| Command | Description |
|---|---|
| `pnpm dev` | Vite dev server only (useful when iterating on UI) |
| `pnpm build` | Production SPA build |
| `pnpm test` | Vitest unit tests |
| `pnpm test:e2e` | Playwright E2E tests |
| `pnpm typecheck` | vue-tsc type checking |

## Architecture

**Stack:** Go 1.26 backend (chi router, ent ORM, modernc/sqlite, Wire DI) + Vue 3 SPA (Vite, TypeScript, pnpm)

**Go workspace:** `go.work` with two modules — `./sdk` and `./server`

```
+------------------------+      +---------------------------+      +-------------------+
|  Browser (Vue 3 SPA)   |      |  Go Backend (:13120)      |      | Claude Code Agents|
+------------------------+      +---------------------------+      +-------------------+
| List / Cards / Kanban  | <--- | GET  /api/agents/stream   | ---> | ps / lsof         |
| Agent Modal (chat)     |      |      (SSE + polling fb)   |      | ~/.claude/        |
| Prompt Input           | ---> | POST /agents/:id/message  |      | JSONL logs        |
| Spawn Dialog           | ---> | POST /agents/spawn        |      |                   |
+------------------------+      +---------------------------+      +-------------------+
                                            |
                                            v
                                +---------------------------+
                                |   dashboard-channel SDK   |
                                |  (Go MCP stdio binary)    |
                                +---------------------------+
```

### Go Backend (`server/`)

| Package | Responsibility |
|---|---|
| `server/cmd/serve/` | cobra CLI entrypoint, Wire DI wiring |
| `server/internal/api/` | chi router, HTTP handlers by domain |
| `server/internal/pipeline/` | state machine, orchestrator, stage handlers, completion detector, agent spawner |
| `server/internal/db/` | ent ORM schemas + repositories |
| `server/internal/mcp/` | MCP server (19 tools, 4 scopes) |
| `server/internal/auth/` | JWT + GitHub OAuth |
| `server/internal/scanner/` | ps/lsof process scanner |
| `server/internal/parser/` | JSONL session parser |
| `server/internal/merger/` | agent data merger + cost estimation |
| `server/internal/sse/` | SSE broadcaster |
| `server/internal/channel/` | channel discovery + proxy |
| `server/internal/refine/` | refinement chat repo + spawner |
| `server/internal/history/` | cost history importer |
| `server/internal/webpush/` | web push VAPID service |

### SDK (`sdk/`)

Go module for the `dashboard-channel` MCP stdio server. Injected into every spawned pipeline stage agent to provide the two-way permission gate and channel reply bridge.

### Data Flow

1. **Process scanning** — `ps aux` + `lsof` (macOS) or `/proc/<pid>/cwd` (Linux) find running `claude` processes and their working directories
2. **Session matching** — PIDs are matched to JSONL session files in `~/.claude/projects/{encoded_path}/`
3. **Log parsing** — tail-reads last 32KB of each session file for tokens, tools, tasks, model info; full session read on modal open
4. **Cost estimation** — `MODEL_PRICING` table in `server/internal/merger/` calculates API-equivalent costs
5. **Status classification** — active (< 30s), waiting (< 5min), idle (> 5min) since last activity
6. **Real-time updates** — browser subscribes to `/api/agents/stream` (SSE) with polling fallback

## Task Pipeline

Multi-stage agentic workflow. Tasks progress through: `concept → backlog → implementation → self_review → finalization → done`. Terminal states also include `on_hold` and `cancelled`. The `concept` and `backlog` stages are agent-less; `implementation` / `self_review` / `finalization` each spawn a detached `claude` CLI process in an isolated git worktree.

**Key characteristics:**

- Each agent-driven stage spawns a detached `claude` CLI process in an isolated git worktree
- LLM output is validated against a per-stage JSON schema; one auto-retry with feedback injection before escalating to the user
- Up to 3 tasks run in parallel (configurable via `maxParallelOrchestrators` in the pipeline DB config)
- Permission requests from stage agents are gated through the dashboard channel; bulk-resolve UI lets the user grant/deny every pending in one click
- Notifications (email, webhook, browser, system) dispatched on hold/failure events

See [ADR-0001](docs/architecture/adr/0001-sqlite-for-task-pipeline.md) (SQLite rationale) and [ADR-0002](docs/architecture/adr/0002-runner-slot-priority-model.md) (runner-slot priority model).

## MCP Endpoint

The dashboard exposes a stateless StreamableHTTP MCP server at `POST /api/mcp` for external agent control.

**Authentication:** `Authorization: Bearer <token>` — generate tokens in **Settings → API Keys**. Only the SHA-256 hash is stored; tokens are shown once at creation.

**Scopes** (hierarchical — higher scopes imply lower):

| Scope | Access |
|---|---|
| `tasks:read` | List and read tasks, stage runs, audit log, permission requests |
| `tasks:write` | Create, update, delete tasks (implies `tasks:read`) |
| `pipeline:control` | Progress, approve, cancel, retry tasks; manage permissions (implies `tasks:read`) |
| `keys:manage` | Full access including API key management |

**Tools (19):** `list_tasks`, `get_task`, `list_stage_runs`, `list_audit`, `list_permission_requests`, `create_task`, `update_task`, `delete_task`, `manage_task`, `progress_task`, `cancel_task`, `retry_task`, `grant_permission`, `resolve_permission_request`, `add_dependency`, `remove_dependency`, `list_api_keys`, `create_api_key`, `revoke_api_key`

**Local integration:** Copy `.mcp.json.example` → `.mcp.json` and export `DASHBOARD_MCP_TOKEN`. Any Claude Code session opened in this repo will auto-connect to the dashboard MCP.

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `DASHBOARD_JWT_SECRET` | auto-generated (ephemeral) | Secret for signing JWT session tokens (min 32 chars; set a stable value to survive restarts) |
| `DASHBOARD_GITHUB_CLIENT_ID` | — | GitHub OAuth app client ID (omit for loopback dev — auth bypass activates automatically) |
| `DASHBOARD_GITHUB_CLIENT_SECRET` | — | GitHub OAuth app client secret |
| `DASHBOARD_PORT` | `13120` | HTTP server port |
| `DASHBOARD_HOST` | `127.0.0.1` | Bind address (warns if non-loopback) |
| `DASHBOARD_DB_PATH` | `~/.claude/dashboard-tasks.db` | SQLite path |
| `DASHBOARD_WORKTREE_ROOT` | `~/.claude/dashboard-worktrees` | Per-task git worktrees |
| `DASHBOARD_SSE_INTERVAL_MS` | `3000` | Agent SSE broadcast interval (ms) |
| `DASHBOARD_SPAWN_RATE_LIMIT` | `5` | Max user-initiated spawns per window |
| `DASHBOARD_SPAWN_RATE_WINDOW_MS` | `60000` | Spawn rate-limit window (ms) |
| `DASHBOARD_MCP_TOKEN` | — | Bearer token for dashboard MCP access |
| `DASHBOARD_MCP_URL` | — | Dashboard MCP URL injected into stage agents |
| `DASHBOARD_STAGE_RUN_ID` | — | Injected into stage agents by orchestrator |
| `DASHBOARD_TASK_ID` | — | Injected into stage agents by orchestrator |
| `DASHBOARD_ALLOW_GIT_PUSH` | `false` | Enable `git push` in spawned agents |
| `DASHBOARD_ALLOW_GIT_PULL` | `false` | Enable `POST /api/tasks/:id/git-action` with `action:'pull'` |
| `DASHBOARD_HOOKS_SECRET` | — | Shared bearer token for `/api/hooks/event` |
| `DASHBOARD_HOOKS_DEBOUNCE_MS` | `100` | Debounce before SSE rescan after hook event |

## Controlling Running Agents

Agents spawned from the dashboard get the channel MCP server automatically. When a channel is active, a green **CH** badge appears in the agent table. Open the agent modal to send messages and view replies.

For agents started manually outside the dashboard, inject the channel binary:

```bash
claude --mcp-config '{"mcpServers":{"dashboard-channel":{"command":"/path/to/bin/dashboard-channel"}}}'
```

## Spawning New Agents

Click **"+ New Agent"** in the header to open the spawn dialog.

| Field | Required | Description |
|---|---|---|
| Prompt | Yes | What the agent should do |
| Working Directory | Yes | Project path the agent runs in |
| Model | No | claude-opus-4-6, claude-sonnet-4-6, claude-haiku-4-5 |
| System Prompt | No | Custom system instructions |
| Enable Channel | No | Dashboard control channel (default: on) |

Spawned agents run detached — they survive dashboard restarts and appear in the table within ~3 seconds.

## Security

- Server binds to `127.0.0.1` only — never exposed to the network
- Auth bypass active when loopback + no GitHub OAuth configured; all API requests allowed without login (safe for local dev)
- `DASHBOARD_JWT_SECRET` auto-generated if unset (ephemeral — sessions reset on restart); set a stable value for production
- Bearer tokens are SHA-256 hashed before storage; never stored in plaintext
- Channel replies authenticated via per-agent Bearer tokens
- Markdown output sanitized via DOMPurify before `v-html` rendering
- Spawn rate-limited to 5 requests/minute (configurable)
- `DANGEROUS_BASH_RE` block-list in spawner prevents curl/wget/eval/shell-substitution in agent tool grants
- Multi-machine mode (`DASHBOARD_REMOTES`) requires remote instances to be network-accessible; use a VPN or SSH tunnel — never bind to `0.0.0.0` on an untrusted network

## Python Statusline

`scripts/statusline.py` integrates the dashboard into your shell prompt, showing live agent count, active cost rate, and total tokens.

```bash
# zsh — add to ~/.zshrc
_agent_status() {
    local out
    out=$(python3 /path/to/agent-dashboard/scripts/statusline.py 2>/dev/null)
    [[ -n "$out" ]] && echo " [$out]"
}
PROMPT='%n@%m %~$(_agent_status) %# '

# bash — add to ~/.bashrc
_agent_status() {
    python3 /path/to/agent-dashboard/scripts/statusline.py 2>/dev/null
}
PROMPT_COMMAND='export PS1="\u@\h \w [$(_agent_status)] \$ "'
```

**Options:**

| Flag | Default | Description |
|---|---|---|
| `--port PORT` | `13120` | Dashboard HTTP port |
| `--timeout SECS` | `0.5` | Request timeout (keep low for PS1 responsiveness) |
| `--format text\|json` | `text` | Output format |

**Environment:**

| Variable | Description |
|---|---|
| `DASHBOARD_API_URL` | Override base URL (default: `http://127.0.0.1:<port>`) |
| `DASHBOARD_API_TOKEN` | Bearer token if auth is enabled |

If the dashboard is not running or the request times out, the script exits silently — your prompt is never stalled.

## Agent Skills

Project-specific AI agent skills are tracked in `skills-lock.json`. The actual skill files are not committed — install them locally after cloning.

```bash
# Claude Code
cat skills-lock.json | jq -r '.skills[] | "\(.source) .claude/skills/\(.name)/SKILL.md"' | while read url dest; do
  mkdir -p "$(dirname "$dest")" && curl -sL "$url" -o "$dest"
done

# Other agents (Copilot, Cursor, etc.)
cat skills-lock.json | jq -r '.skills[] | "\(.source) .agents/skills/\(.name)/SKILL.md"' | while read url dest; do
  mkdir -p "$(dirname "$dest")" && curl -sL "$url" -o "$dest"
done
```

**Current skills:**

| Skill | Description |
|---|---|
| `vue` | Vue 3 Composition API, script setup, reactivity |
| `vitest` | Vitest unit testing with Jest-compatible API |
| `vite` | Vite build tool configuration and plugin API |
| `vueuse-functions` | VueUse composables for Vue features |
| `playwright-best-practices` | Playwright E2E testing patterns |

## Key Conventions

- Path alias: `@/*` maps to `./src/*`
- **Dual persistence:** agent monitoring is filesystem-derived (no database); the task pipeline uses SQLite at `~/.claude/dashboard-tasks.db` (override via `DASHBOARD_DB_PATH`). See [ADR-0001](docs/architecture/adr/0001-sqlite-for-task-pipeline.md).
- Cost estimation uses API-equivalent pricing (not actual billing for Pro/Max users)
- Agent status thresholds: active < 30s, waiting < 5min, idle > 5min
- Subagent discovery: `~/.claude/projects/{encoded_path}/{sessionId}/subagents/*.jsonl`
