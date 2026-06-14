# Architecture

## What This Is

A real-time monitoring dashboard for locally running Claude Code agents. Reads Claude Code's internal JSONL session logs and process metadata to display token usage, costs, tool activity, tasks, and subagents across all running agent processes.

**Stack:** Go 1.26 backend (chi, ent ORM, modernc/sqlite, Wire DI) + Vue 3 TypeScript SPA (Vite, pnpm). Go workspace with `./sdk` and `./server` modules. Build via `task` (Taskfile.yml), hot-reload via `air`.

## Backend (`server/`)

Go modules. Entrypoint: `server/cmd/serve/main.go` (cobra CLI + Wire DI).

### Core packages (`server/internal/`)

| Package | Responsibility |
|---|---|
| `api/` | chi router (`router.go`), HTTP handlers by domain sub-package |
| `api/agents/` | Agent SSE broadcast, spawn, message forwarding, channel reply |
| `api/tasks/` | Task CRUD, pipeline progression, permissions, dependencies |
| `api/auth/` | GitHub OAuth flow, JWT session cookies |
| `api/apikeys/` | API key CRUD endpoints |
| `api/hooks/` | Debounced rescan hook endpoint |
| `api/memory/` | Claude memory file read/write |
| `api/presets/` | Permission preset CRUD |
| `api/refine/` | Refinement chat SSE stream |
| `api/remotes/` | Remote dashboard registration + SSRF-guarded proxy |
| `api/search/` | FTS5 spotlight search (tasks + in-memory agents) |
| `api/sessions/` | Resumable session listing |
| `api/system/` | Health check, CPU/memory metrics |
| `api/wphandler/` | Web Push subscription + send |
| `api/history/` | Historical session cost import SSE |
| `pipeline/` | State machine, orchestrator, stage handlers, completion detector, agent spawner |
| `llmadapter/` | Leaf: pluggable-spawner transport (`LLMSpawner`/`StreamingLLMSpawner`, `NewLLMSpawnerFromSpawner`, `AvailableAdapters`, Ollama/OpenAI/custom adapters). Deps: `db/ent` only. Extracted from `pipeline/` per ADR-0005 |
| `worktree/` | Leaf: git-worktree primitives (`Runner` with `Output`/`Combined` + 15s timeout, `DefaultRoot`, `PathFor`, `CreateBranch`, `DefaultRootDirName`/`BranchPrefix`). Deps: stdlib only. Consumed by `pipeline/` (lifecycle), `services/` (inspection), `config/` (default root) per ADR-0006 |
| `db/` | ent ORM schemas + repos (tasks, stage_runs, users, api_keys, presets, remotes, refine, cost_history, web_push subscriptions) |
| `mcp/` | Stateless StreamableHTTP MCP server — 19 tools, 4 scope tiers |
| `auth/` | JWT helpers, GitHub OAuth client |
| `scanner/` | ps/lsof process scanner |
| `parser/` | JSONL session parser (tail-reads 32KB) |
| `merger/` | Agent data merger + cost estimation (`MODEL_PRICING`) |
| `sse/` | SSE broadcaster (agents + tasks) |
| `channel/` | Channel discovery + proxy to per-agent MCP stdio server |
| `channelconfig/` | Builds dashboard-channel MCP config for spawned agents |
| `refine/` | Refinement chat repo + spawner |
| `history/` | Cost history importer service |
| `webpush/` | VAPID service + subscription management |
| `platform/` | `IS_LINUX` constant |
| `config/` | koanf-based config (env + JSON, validation) |

### SDK (`sdk/`)

Shared-types Go module (`github.com/lx-wnk/agent-dashboard/sdk`). Defines `Agent`, `AgentStatus`, `TokenUsage`, `SessionMeta`, `SubAgent`, `TaskInfo` — the canonical data model consumed by both the server (`merger/`, `api/agents/`, `agentbroadcast/`) and the dashboard-channel MCP stdio server injected into spawned stage agents. The channel bridge binary is compiled separately but shares these types with the server.

## Frontend (`src/`)

Vue 3 + TypeScript SPA (unchanged structure from TypeScript-server era).

- `src/composables/useAgents.ts` — SSE-first real-time updates via `/api/agents/stream` with polling fallback; view mode + search state
- `src/composables/useAgentPrompt.ts` — send prompts to agents (via channel or spawn/resume)
- `src/composables/useTheme.ts` — dark/light theme with OS preference + localStorage
- `src/composables/useNotificationConfig.ts` — fetch/persist notification preferences and channel config via `/api/notifications/*`
- `src/composables/useMemory.ts` — list, read, and write Claude memory files via `/api/memory`
- `src/composables/useSessions.ts` — fetch resumable session list via `/api/sessions`
- `src/composables/useSystemResources.ts` — poll CPU/memory/disk metrics via `/api/system` with visibility-aware interval
- `src/composables/usePlugins.ts` — fetch installed plugin list via `/api/settings/plugins`
- `src/composables/usePluginSlots.ts` — loads `addon.js` from `route_extension` plugins for a named slot; route_extension plugins (e.g. voice-whisper, voice-webspeech) contribute frontend UI this way
- `src/composables/useCostHeatmap.ts` — fetch 7×24 cost heatmap grid via `/api/analytics/heatmap`
- `src/composables/useCostForecast.ts` — fetch cost trend, forecast points, and budget alerts via `/api/analytics/cost-forecast`
- `src/composables/useRemotes.ts` — CRUD for remote dashboard registrations via `/api/remotes`
- `src/App.vue` — root: header stats + view toggle + search + agent list/cards/kanban + modal
- `src/components/AgentRow.vue` / `SubAgentRow.vue` — list view rows
- `src/components/AgentCard.vue` — card view tile
- `src/components/AgentModal.vue` — full session modal (transcript, ToolTimeline, TaskList, SubAgentList, prompt)
- `src/components/KanbanBoard.vue` — task kanban across agents
- `src/components/PluginSlot.vue` — mount host that renders plugin addons into a named slot
- `src/types.ts` — shared TypeScript interfaces (`Agent`, `TokenUsage`, `SessionMeta`, etc.)
- `src/utils/format.ts` — token, cost, uptime, model name formatting
- `src/utils/plugins.ts` — `fetchPluginList`, single source for the `/api/settings/plugins` fetch shared by `usePlugins` and `usePluginSlots`
- `src/utils/pluginSlot.ts` — generic, voice-agnostic plugin-slot contract (`SlotContext`, `SlotAddon`)

## Pipeline UI (`src/components/` + `src/composables/`)

- `src/components/PipelineBoard.vue` — kanban-style task pipeline board
- `src/components/TaskCard.vue` — task card with status chip and run-status badge
- `src/components/TaskModal.vue` — full task detail: stage output, chat stream, feedback, permissions
- `src/components/StageOutputView.vue` — per-stage LLM output with expand/collapse
- `src/components/AgentChatStream.vue` — live SSE-streamed agent message view
- `src/components/BacklogForm.vue` — task creation / backlog entry form
- `src/components/CrossLinkBanner.vue` — session↔task cross-link banner
- `src/composables/useTasks.ts` — SSE-first task list with 60s polling fallback
- `src/composables/useRole.ts` — role-based access control

## Data Flow

Browser connects to `/api/agents/stream` (SSE) with polling fallback → Go backend scans processes (`ps`/`lsof`) → matches PIDs to `~/.claude/projects/{encoded_path}/{sessionId}.jsonl` → tail-reads JSONL + reads `~/.claude/usage-data/session-meta/{sessionId}.json` → merges, calculates cost/status → broadcasts `Agent[]` to SSE clients.

**Agent status thresholds:** active < 30s, waiting < 5min, idle > 5min (since last activity).
