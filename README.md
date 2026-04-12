# Claude Agent Overview

Real-time monitoring and control dashboard for locally running Claude Code agents. Displays token usage, costs, tool activity, tasks, and subagents — and lets you send instructions or spawn new agents directly from the browser.

![Status](https://img.shields.io/badge/status-development-yellow)

## Features

- **Live agent monitoring** — all running Claude Code processes with status, model, tokens, cost, uptime (list, card, and kanban views)
- **Chat-style session view** — full conversation transcript with markdown rendering, collapsible tool groups, inline task checklists, sub-agent badges
- **Channel control** — send follow-up instructions and /btw interrupts to running agents via MCP Channels
- **Agent spawning** — start new Claude agents with custom prompts, models, and system prompts from the UI
- **Multi-machine support** — aggregate agents from remote machines via `DASHBOARD_REMOTES`
- **SSE streaming** — real-time updates via Server-Sent Events with polling fallback

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

### Directory Structure

```
├── server/                    # Express backend
│   ├── index.ts               # API server + SSE + Vite middleware
│   ├── processScanner.ts      # Finds Claude processes via ps/lsof
│   ├── jsonlParser.ts         # Reads JSONL session logs
│   ├── agentMerger.ts         # Merges process + session data, cost calc
│   ├── pricing.ts             # MODEL_PRICING lookup table
│   ├── channelDiscovery.ts    # Reads channel discovery files
│   ├── remoteAggregator.ts    # Multi-machine agent aggregation
│   └── systemMonitor.ts       # CPU/disk monitoring (macOS + Linux)
├── src/                       # Vue 3 frontend
│   ├── App.vue                # Root: header stats, view toggle, search
│   ├── components/
│   │   ├── AgentTable.vue     # Sortable agent table (list view)
│   │   ├── AgentRow.vue       # Table row per agent
│   │   ├── SubAgentRow.vue    # Indented subagent rows
│   │   ├── AgentCard.vue      # Card view tile with output preview
│   │   ├── AgentCardGrid.vue  # Responsive grid for cards
│   │   ├── AgentModal.vue     # Chat-style session modal
│   │   ├── KanbanBoard.vue    # Kanban board (tasks across agents)
│   │   ├── SpawnDialog.vue    # Spawn new agents modal
│   │   ├── PromptInput.vue    # Prompt input with slash autocomplete
│   │   ├── ToolTimeline.vue   # Recent tool calls timeline
│   │   ├── TaskList.vue       # Agent task tracker
│   │   └── SubAgentList.vue   # Subagent list in detail panel
│   ├── composables/
│   │   ├── useAgents.ts       # SSE + polling for agent data
│   │   ├── useAgentPrompt.ts  # Send prompts to agents
│   │   └── useTheme.ts        # Dark/light theme with OS detection
│   ├── types.ts               # Shared TypeScript interfaces
│   └── utils/format.ts        # Token, cost, uptime formatters
└── channel/                   # MCP Channel server (separate package)
    ├── dashboard-channel.ts   # Standalone MCP server for agent control
    └── package.json           # @modelcontextprotocol/sdk dependency
```

## Controlling Running Agents

The dashboard can send instructions to agents that were started with the Channel MCP server. Agents spawned from the dashboard get this automatically. For manually started agents:

```bash
claude --mcp-config '{"mcpServers":{"dashboard-channel":{"command":"node","args":["--import","tsx/esm","/path/to/channel/dashboard-channel.ts"]}}}'
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
pnpm dev           # Express + Vite with hot reload on :13120
pnpm build         # Production build via Vite
pnpm lint          # ESLint check
pnpm test          # Vitest unit tests
pnpm test:e2e      # Playwright E2E tests
pnpm typecheck     # vue-tsc type checking
```

### Prerequisites

- Node.js 22+
- Claude Code installed and running (at least one agent process for the dashboard to display)
- For channel control: agents started with `--mcp-config with the dashboard-channel server`
- **Platform:** macOS is fully supported. Linux: process scanning and disk work, but CPU monitoring (`top`) uses macOS-specific flags and will show 0%. Windows: not supported (requires `ps`, `lsof`, `top`, `df`).

### Key Conventions

- Path alias: `@/*` maps to `./src/*`
- No database — all data from Claude Code's filesystem + running processes
- Cost estimation uses API-equivalent pricing (not actual billing for Pro/Max users)
- Agent status thresholds: active < 30s, waiting < 5min, idle > 5min
