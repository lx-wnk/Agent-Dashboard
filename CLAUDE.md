# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

A real-time monitoring dashboard for locally running Claude Code agents. It reads Claude Code's internal JSONL session logs and process metadata to display token usage, costs, tool activity, tasks, and subagents across all running agent processes.

## Development

```bash
npm run dev        # Starts Express (port 13120) + Vite SPA middleware with hot reload
```

No separate build, lint, or test scripts are configured. The single dev command runs the full stack.

## Architecture

**Backend** (Express + Node CLI tools in `server/`):
- `server/index.ts` — Express server with `/api/agents` endpoint, integrates Vite dev middleware
- `server/processScanner.ts` — Uses `ps` and `lsof` to find running `/claude` processes and their working directories
- `server/jsonlParser.ts` — Tail-reads last 32KB of JSONL session files, extracts tokens, model, tools, tasks
- `server/agentMerger.ts` — Matches PIDs to session data, calculates costs via `MODEL_PRICING`, determines status

**Frontend** (Vue 3 + TypeScript SPA in `src/`):
- `src/composables/useAgents.ts` — Polls `/api/agents` every 3 seconds
- `src/App.vue` — Root: header stats + agent table + off-canvas detail panel
- `src/components/AgentRow.vue` / `SubAgentRow.vue` — Table rows (subagents indented under parent)
- `src/components/AgentDetail.vue` — Detail panel with token bars, tool histogram, tasks, subagents
- `src/types.ts` — All shared TypeScript interfaces (`Agent`, `TokenUsage`, `SessionMeta`, etc.)
- `src/utils/format.ts` — Token, cost, and uptime formatting utilities

**Data flow:** Browser polls `/api/agents` → Express scans processes (`ps`/`lsof`) → matches PIDs to `~/.claude/projects/{encoded_path}/{sessionId}.jsonl` → tail-reads JSONL + reads `~/.claude/usage-data/session-meta/{sessionId}.json` → merges, calculates cost/status → returns `Agent[]`.

**Agent status thresholds:** active < 30s, waiting < 5min, idle > 5min (since last activity).

## Key Conventions

- Path alias: `@/*` maps to `./src/*` (configured in tsconfig.json and vite.config.ts)
- Server binds to `127.0.0.1` only — never expose to network (reads sensitive session data)
- No database — all data sourced from Claude Code's filesystem and running processes
- Subagents discovered from `~/.claude/projects/{sessionId}/subagents/*.jsonl`
- Cost estimation uses a `MODEL_PRICING` lookup table in `agentMerger.ts`
- **Platform:** macOS only. `server/systemMonitor.ts` uses macOS-specific `top` flags; process scanning relies on `ps` and `lsof` (Unix-only). Linux/Windows are unsupported.
