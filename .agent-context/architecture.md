# Architecture

## What This Is

A real-time monitoring dashboard for locally running Claude Code agents. It reads Claude Code's internal JSONL session logs and process metadata to display token usage, costs, tool activity, tasks, and subagents across all running agent processes.

## Backend (`server/`)

Express + Node CLI tools.

- `server/platform.ts` — Shared `IS_LINUX` constant for platform detection
- `server/paths.ts` — Shared path constants (`CLAUDE_PROJECTS_DIR`, `SESSION_META_DIR`, `DISCOVERY_DIR`, `WHITESPACE_RE`)
- `server/constants.ts` — Shared constants: `VALID_STAGES`, `SLUG_RE`, `SLUG_PATTERN_MESSAGE`, `SYSTEM_PROMPT_MAX_CHARS`
- `server/index.ts` — Express server with `/api/agents` REST + `/api/agents/stream` SSE endpoint, integrates Vite dev middleware
- `server/processScanner.ts` — Uses `ps` and `lsof` to find running `/claude` processes and their working directories
- `server/jsonlParser.ts` — Tail-reads last 32KB of JSONL session files, extracts tokens, model, tools, tasks
- `server/agentMerger.ts` — Matches PIDs to session data, calculates costs via `estimateCost` (from `pricing.ts`), determines status
- `server/spawnManager.ts` — Rate-limited dashboard-initiated agent spawner; manages spawn slots, stderr ring-buffer, and channel-reply routing (configurable via `DASHBOARD_SPAWN_RATE_LIMIT`, `DASHBOARD_SPAWN_RATE_WINDOW_MS`)
- `server/channelConfig.ts` — `buildDashboardChannelMcpConfig()`: builds the MCP channel config injected into user-spawned agents

## Frontend (`src/`)

Vue 3 + TypeScript SPA.

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

## Pipeline UI (`src/components/` + `src/composables/`)

- `src/components/PipelineBoard.vue` — kanban-style task pipeline board
- `src/components/TaskCard.vue` — task card with status chip and run-status badge
- `src/components/TaskModal.vue` — full task detail: stage output, chat stream, feedback, permissions
- `src/components/StageOutputView.vue` — renders per-stage LLM output with expand/collapse
- `src/components/AgentChatStream.vue` — live SSE-streamed agent message view
- `src/components/BacklogForm.vue` — task creation / backlog entry form
- `src/components/CrossLinkBanner.vue` — session↔task cross-link banner rendered in both `AgentModal.vue` and `TaskModal.vue`; emits `click` to trigger cross-navigation
- `src/composables/useTasks.ts` — SSE-first task list state with 60s polling fallback
- `src/composables/useRole.ts` — role-based access control composable

## Data Flow

Browser connects to `/api/agents/stream` (SSE) with polling fallback → Express scans processes (`ps`/`lsof`) → matches PIDs to `~/.claude/projects/{encoded_path}/{sessionId}.jsonl` → tail-reads JSONL + reads `~/.claude/usage-data/session-meta/{sessionId}.json` → merges, calculates cost/status → broadcasts `Agent[]` to SSE clients.

**Agent status thresholds:** active < 30s, waiting < 5min, idle > 5min (since last activity).
