# Claude Agent Overview

Real-time monitoring and control dashboard for locally running Claude Code agents. Displays token usage, costs, tool activity, tasks, and subagents — and lets you send instructions or spawn new agents directly from the browser.

![Status](https://img.shields.io/badge/status-development-yellow)

## Features

- **Live agent table** — all running Claude Code processes with status, model, tokens, cost, uptime
- **Detail panel** — token usage breakdown, tool histogram, task list, subagents, session metadata
- **Channel control** — send follow-up instructions to running agents via MCP Channels
- **Agent spawning** — start new Claude agents with custom prompts, models, and system prompts from the UI
- **Auto-refresh** — 3-second polling, no manual refresh needed

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
Browser (Vue 3 SPA)         Express Backend (:13120)        Claude Code Agents
┌─────────────────┐        ┌─────────────────────┐        ┌─────────────────┐
│ Agent Table     │──poll──▶│ GET /api/agents     │──scan──▶│ ps / lsof       │
│ Detail Panel    │        │                     │──read──▶│ ~/.claude/      │
│ Channel Panel   │──send──▶│ POST /agents/:id/msg│──proxy─▶│ JSONL logs      │
│ Spawn Dialog    │──spawn─▶│ POST /agents/spawn  │         │                 │
└─────────────────┘        └─────────────────────┘        └─────────────────┘
                                    ▲
                           Channel MCP Server
                           (per agent, random port)
```

### Data Flow

1. **Process scanning** — `ps aux` + `lsof` find running `/claude` processes and their working directories
2. **Session matching** — PIDs are matched to JSONL session files in `~/.claude/projects/`
3. **Log parsing** — tail-reads last 32KB of each session file for tokens, tools, tasks, model info
4. **Cost estimation** — `MODEL_PRICING` table in `agentMerger.ts` calculates API-equivalent costs
5. **Status classification** — active (< 30s), waiting (< 5min), idle (> 5min) since last activity

### Directory Structure

```
├── server/                  # Express backend
│   ├── index.ts             # API server + Vite middleware
│   ├── processScanner.ts    # Finds Claude processes via ps/lsof
│   ├── jsonlParser.ts       # Reads JSONL session logs
│   ├── agentMerger.ts       # Merges process + session data, cost calc
│   └── channelDiscovery.ts  # Reads channel discovery files
├── src/                     # Vue 3 frontend
│   ├── App.vue              # Root: header stats, table, panels
│   ├── components/
│   │   ├── AgentTable.vue   # Sortable agent table
│   │   ├── AgentRow.vue     # Table row per agent
│   │   ├── SubAgentRow.vue  # Indented subagent rows
│   │   ├── AgentDetail.vue  # Off-canvas detail panel
│   │   ├── ChannelPanel.vue # Send messages to agents
│   │   ├── SpawnDialog.vue  # Spawn new agents modal
│   │   ├── ToolTimeline.vue # Recent tool calls timeline
│   │   ├── TaskList.vue     # Agent task tracker
│   │   └── SubAgentList.vue # Subagent list in detail panel
│   ├── composables/
│   │   ├── useAgents.ts     # Polls /api/agents every 3s
│   │   └── useChannel.ts    # Channel message send + reply polling
│   ├── types.ts             # Shared TypeScript interfaces
│   └── utils/format.ts      # Token, cost, uptime formatters
└── channel/                 # MCP Channel server (separate package)
    ├── dashboard-channel.ts # Standalone MCP server for agent control
    └── package.json         # @modelcontextprotocol/sdk dependency
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
| `GET` | `/api/agents` | List all running agents with full metadata |
| `POST` | `/api/agents/spawn` | Spawn a new Claude agent process |
| `POST` | `/api/agents/:sessionId/message` | Send instruction to agent via channel |
| `GET` | `/api/agents/:sessionId/replies` | Get agent replies (supports `?since=` filter) |
| `POST` | `/api/channel-reply` | Internal: receives replies from channel servers |

## Security

- Server binds to `127.0.0.1` only — never exposed to the network
- Channel replies are authenticated via per-agent Bearer tokens
- Discovery files are validated for process liveness (stale files are cleaned up)
- All user-generated content rendered with `v-text` (no `v-html`) to prevent XSS

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
pnpm dev    # Express + Vite with hot reload on :13120
```

No separate build, lint, or test scripts. The single `dev` command runs the full stack. The Vite dev server runs as Express middleware with HMR enabled.

### Prerequisites

- Node.js 20+
- Claude Code installed and running (at least one agent process for the dashboard to display)
- For channel control: agents started with `--mcp-config with the dashboard-channel server`
- **Platform:** macOS is fully supported. Linux: process scanning and disk work, but CPU monitoring (`top`) uses macOS-specific flags and will show 0%. Windows: not supported (requires `ps`, `lsof`, `top`, `df`).

### Key Conventions

- Path alias: `@/*` maps to `./src/*`
- No database — all data from Claude Code's filesystem + running processes
- Cost estimation uses API-equivalent pricing (not actual billing for Pro/Max users)
- Agent status thresholds: active < 30s, waiting < 5min, idle > 5min
