# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

A real-time monitoring dashboard for locally running Claude Code agents. It reads Claude Code's internal JSONL session logs and process metadata to display token usage, costs, tool activity, tasks, and subagents across all running agent processes.

## Development

```bash
pnpm dev           # Starts Express (port 13120) + Vite SPA middleware with hot reload
pnpm build         # Production build via Vite
pnpm lint          # ESLint check
pnpm test          # Vitest unit tests (single run)
pnpm test:e2e      # Playwright E2E tests
pnpm typecheck     # vue-tsc type checking
```

**Package manager:** pnpm (workspace setup — `pnpm install` in root installs both root and `channel/` dependencies).

## Architecture

**Backend** (Express + Node CLI tools in `server/`):
- `server/platform.ts` — Shared `IS_LINUX` constant for platform detection
- `server/index.ts` — Express server with `/api/agents` REST + `/api/agents/stream` SSE endpoint, integrates Vite dev middleware
- `server/processScanner.ts` — Uses `ps` and `lsof` to find running `/claude` processes and their working directories
- `server/jsonlParser.ts` — Tail-reads last 32KB of JSONL session files, extracts tokens, model, tools, tasks
- `server/agentMerger.ts` — Matches PIDs to session data, calculates costs via `estimateCost` (from `pricing.ts`), determines status

**Frontend** (Vue 3 + TypeScript SPA in `src/`):
- `src/composables/useAgents.ts` — SSE-first real-time updates via `/api/agents/stream` with polling fallback; manages view mode (list/cards/kanban) with localStorage persistence and search query state
- `src/composables/useAgentPrompt.ts` — Shared composable for sending prompts to agents (via channel or spawn/resume)
- `src/composables/useTheme.ts` — Dark/light theme composable with OS preference detection and localStorage persistence
- `src/App.vue` — Root: header stats + view toggle (list/cards/kanban) + search bar + agent list or card grid or kanban + modal
- `src/components/AgentRow.vue` / `SubAgentRow.vue` — Table rows for list view (subagents indented under parent)
- `src/components/AgentCard.vue` — Card view tile with status, output preview, and inline prompt input
- `src/components/AgentCardGrid.vue` — Responsive grid wrapper for AgentCard tiles
- `src/components/AgentModal.vue` — Full session modal with output transcript, agent details (ToolTimeline, TaskList, SubAgentList), and prompt input
- `src/components/ToolTimeline.vue` — Recent tool pills; expects `:tools` (string[])
- `src/components/TaskList.vue` — Task list with status icons; expects `:tasks` (TaskInfo[])
- `src/components/SubAgentList.vue` — Subagent cards; expects `:subagents` (SubAgent[])
- `src/components/KanbanBoard.vue` — Kanban board aggregating tasks across agents into status columns (pending/in-progress/completed)
- `src/types.ts` — All shared TypeScript interfaces (`Agent`, `TokenUsage`, `SessionMeta`, etc.)
- `src/utils/format.ts` — Token, cost, uptime, and model name formatting utilities (including `shortModel`)

**Data flow:** Browser connects to `/api/agents/stream` (SSE) with polling fallback → Express scans processes (`ps`/`lsof`) → matches PIDs to `~/.claude/projects/{encoded_path}/{sessionId}.jsonl` → tail-reads JSONL + reads `~/.claude/usage-data/session-meta/{sessionId}.json` → merges, calculates cost/status → broadcasts `Agent[]` to SSE clients.

**Agent status thresholds:** active < 30s, waiting < 5min, idle > 5min (since last activity).

## Key Conventions

- Path alias: `@/*` maps to `./src/*` (configured in tsconfig.json and vite.config.ts)
- Server binds to `127.0.0.1` only — never expose to network (reads sensitive session data). **Multi-machine mode** (`DASHBOARD_REMOTES` env var) requires remote instances to be network-accessible; use a VPN or SSH tunnel — never bind to `0.0.0.0` on an untrusted network.
- No database — all data sourced from Claude Code's filesystem and running processes
- Subagents discovered from `~/.claude/projects/{encoded_path}/{sessionId}/subagents/*.jsonl`
- Cost estimation uses `MODEL_PRICING` lookup table in `server/pricing.ts`
- **Platform:** macOS and Linux. `server/systemMonitor.ts` uses `top` on macOS and `/proc/stat` on Linux for CPU; `server/processScanner.ts` uses `lsof` on macOS and `/proc/<pid>/cwd` on Linux. Windows is unsupported.
