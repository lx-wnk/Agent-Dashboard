# Claude Agent Overview

Real-time monitoring and control dashboard for locally running Claude Code agents. Displays token usage, costs, tool activity, tasks, and subagents — and lets you send instructions or spawn new agents directly from the browser.

![Status](https://img.shields.io/badge/status-development-yellow)

## Features

- **Real-time agent monitoring via SSE** — all running Claude Code processes with status, model, tokens, cost, uptime (list, card, and kanban views)
- **Chat-style session view** — full conversation transcript with markdown rendering, collapsible tool groups, inline task checklists, sub-agent badges
- **Channel control** — send follow-up instructions and /btw interrupts to running agents via MCP Channels
- **Agent spawning** — start new Claude agents with custom prompts, models, and system prompts from the UI
- **Task pipeline with approval gates** — multi-stage agentic workflow with human review checkpoints
- **Authenticated MCP endpoint (18 tools, 4 scopes)** for external agent control
- **API key management with scoped access** — generate and revoke bearer tokens in Settings
- **Cross-linking between agent sessions and pipeline tasks**
- **Multi-machine support** — aggregate agents from remote machines via `DASHBOARD_REMOTES`
- **Dark/light theme, list/card/kanban views**

## Quick Start

```bash
# Install dependencies (pnpm workspace — installs root + channel/)
pnpm install

# Start dashboard (Express + Vite on port 13120)
pnpm dev
```

Open [http://localhost:13120](http://localhost:13120) — running Claude Code agents appear automatically.

## Architecture

```
+------------------------+      +----------------------------+      +-------------------+
|  Browser (Vue 3 SPA)   |      |  Express Backend (:13120)  |      | Claude Code Agents|
+------------------------+      +----------------------------+      +-------------------+
| List / Cards / Kanban  | <--- | GET  /api/agents/stream    | ---> | ps / lsof         |
| Agent Modal (chat)     |      |      (SSE + polling fb)    |      | ~/.claude/        |
| Prompt Input           | ---> | POST /agents/:id/message   |      | JSONL logs        |
| Spawn Dialog           | ---> | POST /agents/spawn         |      |                   |
+------------------------+      +----------------------------+      +-------------------+
                                            |
                                            v
                                +----------------------------+
                                |    Channel MCP Server      |
                                |  (per agent, random port)  |
                                +----------------------------+
```

### Data Flow

1. **Process scanning** — `ps aux` + `lsof` (macOS) or `/proc/<pid>/cwd` (Linux) find running `claude` processes and their working directories
2. **Session matching** — PIDs are matched to JSONL session files in `~/.claude/projects/{encoded_path}/`
3. **Log parsing** — tail-reads last 32KB of each session file for tokens, tools, tasks, model info; full session read on modal open
4. **Cost estimation** — `MODEL_PRICING` table in `pricing.ts` calculates API-equivalent costs
5. **Status classification** — active (< 30s), waiting (< 5min), idle (> 5min) since last activity
6. **Real-time updates** — browser subscribes to `/api/agents/stream` (SSE) with polling fallback


## Task Pipeline

An agentic task pipeline runs multi-stage work items through Claude Code automatically. Tasks progress through these stages: `backlog → pruefung → refinement → planning → approval1 → umsetzungskonzept → approval2 → umsetzung → selbstreview → finalisierung → done`, with human approval gates at `approval1` and `approval2`. Terminal states also include `on_hold` and `cancelled`.

**Key characteristics:**
- Each agent-driven stage spawns a detached `claude` CLI process in an isolated git worktree
- LLM output is validated against a per-stage JSON schema; one auto-retry with feedback injection before escalating to the user
- Up to 3 tasks run in parallel (configurable via `maxParallelOrchestrators` in the pipeline DB config)
- Notifications (email, webhook, browser, system) dispatched on hold/failure events

See [ADR-0001](docs/architecture/adr/0001-sqlite-for-task-pipeline.md) (SQLite rationale) and [ADR-0002](docs/architecture/adr/0002-runner-slot-priority-model.md) (runner-slot priority model).

## MCP Endpoint

The dashboard exposes a stateless StreamableHTTP MCP server at `POST /api/mcp` for external agent control.

**Authentication:** `Authorization: Bearer <token>` — tokens are generated in **Settings → API Keys**. Only the SHA-256 hash is stored; tokens are shown once at creation.

**Scopes** (hierarchical — higher scopes imply lower):

| Scope | Access |
|---|---|
| `tasks:read` | List and read tasks, stage runs, audit log, permission requests |
| `tasks:write` | Create, update, delete tasks (implies `tasks:read`) |
| `pipeline:control` | Progress, approve, cancel, retry tasks; manage permissions (implies `tasks:read`) |
| `keys:manage` | Full access including API key management |

**Tools (18):** `list_tasks`, `get_task`, `list_stage_runs`, `list_audit`, `list_permission_requests`, `create_task`, `update_task`, `delete_task`, `progress_task`, `approve_task`, `request_changes`, `cancel_task`, `retry_task`, `grant_permission`, `resolve_permission_request`, `list_api_keys`, `create_api_key`, `revoke_api_key`

**Local integration:** Copy `.mcp.json.example` → `.mcp.json` and export `DASHBOARD_MCP_TOKEN`. Any Claude Code session opened in this repo will auto-connect to the dashboard MCP.

## API Keys

Manage keys via **Settings → API Keys** in the UI or `GET/POST/DELETE /api/settings/api-keys`.

- Tokens are shown once at creation — store them immediately
- Only the SHA-256 hash is persisted
- Each key carries one or more scopes (see MCP Endpoint above)

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `DASHBOARD_PORT` | `13120` | HTTP server port |
| `DASHBOARD_HOST` | `127.0.0.1` | Bind address (warns if non-loopback) |
| `DASHBOARD_DB_PATH` | `~/.claude/dashboard-tasks.db` | SQLite path |
| `DASHBOARD_WORKTREE_ROOT` | `~/.claude/dashboard-worktrees` | Per-task git worktrees |
| `DASHBOARD_REMOTES` | — | Comma-separated remote dashboard URLs |
| `DASHBOARD_SSE_INTERVAL_MS` | `3000` | Agent SSE broadcast interval |
| `DASHBOARD_SPAWN_RATE_LIMIT` | `5` | Max spawns per window |
| `DASHBOARD_SPAWN_RATE_WINDOW_MS` | `60000` | Spawn rate-limit window (ms) |
| `DASHBOARD_MCP_TOKEN` | — | Bearer token for dashboard MCP access |
| `DASHBOARD_MCP_URL` | — | Dashboard MCP URL (injected into stage agents) |
| `DASHBOARD_STAGE_RUN_ID` | — | Injected into stage agents by orchestrator |
| `DASHBOARD_TASK_ID` | — | Injected into stage agents by orchestrator |

## Controlling Running Agents

The dashboard can send instructions to agents that were started with the Channel MCP server. Agents spawned from the dashboard get this automatically. For manually started agents:

```bash
claude --mcp-config '{"mcpServers":{"dashboard-channel":{"command":"bun","args":["/path/to/channel/dashboard-channel.ts"]}}}'
```

When an agent has a channel active, a green **CH** badge appears in the agent table. Click the agent to open the detail panel — the **Channel** section lets you send messages and see replies.

### How it works

1. Claude Code spawns `channel/dashboard-channel.ts` as an MCP subprocess
2. The channel server opens an HTTP port on localhost and writes a discovery file to `~/.claude/dashboard-channel/{pid}.json`
3. The dashboard backend reads discovery files and proxies messages to the channel HTTP port
4. The channel forwards messages into Claude's context via `notifications/claude/channel`
5. Claude responds using the `dashboard_reply` tool, which POSTs back to the dashboard

## Spawning New Agents

Click **"+ New Agent"** in the header to open the spawn dialog. Available options:

| Field | Required | Description |
|-------|----------|-------------|
| Prompt | Yes | What the agent should do |
| Working Directory | Yes | Project path the agent runs in |
| Model | No | claude-opus-4-6, claude-sonnet-4-6, claude-haiku-4-5 (default: auto) |
| System Prompt | No | Custom system instructions |
| Enable Channel | No | Dashboard control channel (default: on) |

Spawned agents run detached — they survive dashboard restarts and appear in the table within ~3 seconds.

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/config` | Dashboard configuration (remotes, hostname) |
| `GET` | `/api/agents` | List all running agents with full metadata |
| `GET` | `/api/agents/stream` | Server-Sent Events stream of agent updates |
| `GET` | `/api/agents/:sessionId/output` | Full parsed session transcript |
| `GET` | `/api/agents/:sessionId/replies` | Channel replies (supports `?since=` filter) |
| `POST` | `/api/agents/:sessionId/message` | Send instruction to agent via channel |
| `POST` | `/api/agents/spawn` | Spawn a new Claude agent process |
| `GET` | `/api/agents/spawn/:pid/status` | Check spawn status and captured stderr |
| `GET` | `/api/sessions` | List resumable session files |
| `GET` | `/api/system` | CPU, memory, and disk usage |
| `POST` | `/api/channel-reply` | Internal: receives replies from channel servers |

## Security

- Server binds to `127.0.0.1` only — never exposed to the network
- Channel replies are authenticated via per-agent Bearer tokens
- Discovery files are validated for process liveness (stale files are cleaned up)
- Markdown output sanitized via DOMPurify before `v-html` rendering
- Spawn rate-limited to 5 requests/minute
- **`skipPermissions` option:** The spawn dialog offers a "skip permission prompts" checkbox that passes `--dangerously-skip-permissions` to Claude Code. This bypasses all safety confirmations (file writes, deletions, shell commands). The UI shows a warning and requires double-click confirmation. Use only in isolated environments or with trusted prompts.

## Agent Skills

Project-specific AI agent skills are tracked in `skills-lock.json`. The actual skill files are not committed — install them locally after cloning.

### Install for Claude Code

```bash
cat skills-lock.json | jq -r '.skills[] | "\(.source) .claude/skills/\(.name)/SKILL.md"' | while read url dest; do
  mkdir -p "$(dirname "$dest")" && curl -sL "$url" -o "$dest"
done
```

### Install for other agents (Copilot, Cursor, etc.)

```bash
cat skills-lock.json | jq -r '.skills[] | "\(.source) .agents/skills/\(.name)/SKILL.md"' | while read url dest; do
  mkdir -p "$(dirname "$dest")" && curl -sL "$url" -o "$dest"
done
```

### Current skills

| Skill | Description |
|-------|-------------|
| `vue` | Vue 3 Composition API, script setup, reactivity |
| `vitest` | Vitest unit testing with Jest-compatible API |
| `vite` | Vite build tool configuration and plugin API |
| `vueuse-functions` | VueUse composables for Vue features |
| `playwright-best-practices` | Playwright E2E testing patterns |

## Development

```bash
pnpm dev             # Bun + Vite with hot reload on :13120
pnpm build           # Production build via Vite (frontend)
pnpm build:server    # Compile server to self-contained binary (bun build --compile)
pnpm lint            # ESLint check
pnpm test            # Vitest (frontend) + bun test (server)
pnpm test:server     # Server-only tests via bun test
pnpm test:e2e        # Playwright E2E tests
pnpm typecheck       # vue-tsc type checking
```

### Prerequisites

- [Bun](https://bun.sh) 1.x (`curl -fsSL https://bun.sh/install | bash`)
- Claude Code installed and running (at least one agent process for the dashboard to display)
- For channel control: agents started with `--mcp-config with the dashboard-channel server`
- **Platform:** macOS and Linux. CPU monitoring uses `top` on macOS and `/proc/stat` on Linux. Windows is unsupported.

### Single Binary Deployment

For team servers or self-contained distribution, compile the backend into a single executable:

```bash
pnpm build                                          # Build Vue frontend → dist/
bun build --compile server/index.ts --outfile=dashboard  # Compile server (~67 MB)
NODE_ENV=production ./dashboard                     # Start — no node_modules needed
```

The compiled binary embeds the Bun runtime and all server-side dependencies. Deploy by copying `dashboard` + `dist/` to the target machine. No Node.js or npm install required.

### Key Conventions

- Path alias: `@/*` maps to `./src/*`
- **Dual persistence:** agent monitoring is filesystem-derived (no database); the task pipeline uses SQLite at `~/.claude/dashboard-tasks.db` (override via `DASHBOARD_DB_PATH`). See [ADR-0001](docs/architecture/adr/0001-sqlite-for-task-pipeline.md).
- Cost estimation uses API-equivalent pricing (not actual billing for Pro/Max users)
- Agent status thresholds: active < 30s, waiting < 5min, idle > 5min
