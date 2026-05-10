# Agent Dashboard — Unified Feature Roadmap (2026-05-09)

## 1. Current State

The dashboard monitors locally running Claude Code CLI agents via JSONL tail-reads and process scanning. It ships:
- SSE-first agent list (`/api/agents/stream`) with polling fallback
- Task pipeline (SQLite-backed) with kanban board and stage state machine
- Permission system with template presets and MCP channel bridge
- Notification adapters (email, webhook, browser, system)
- Spawn manager with rate-limiting
- Cost estimation via `MODEL_PRICING` lookup

**18 open pipeline tasks** are already fully specified in the DB (all at `implementation` or `concept` stage). This spec unifies them with **17 new ideas** discovered via competitor analysis into a single prioritized roadmap.

---

## 2. Feature Catalog

### 2.1 Real-time & Performance

| ID | Feature | Source | Status |
|----|---------|--------|--------|
| RT-1 | **Claude Code Hooks ingestion** — lifecycle hooks (`PreToolUse`/`PostToolUse`/`SessionStart`/`Stop`) POST to `/api/hooks/event`; triggers debounced rescan → SSE broadcast. Polling stays as safety net. New env vars: `DASHBOARD_HOOKS_SECRET`, `DASHBOARD_HOOKS_DEBOUNCE_MS`. | DB: `claude-code-hooks-ingestion` | Open |
| RT-2 | **Incremental JSONL byte-offset reads** — cache last read position per session file in memory; skip re-parsing entire tail on each poll. ~50× speedup for long sessions. Affects `server/jsonlParser.ts`. | Competitor: hoangsonww | New |
| RT-3 | **Tab visibility → pause SSE** — `visibilitychange` listener in `useAgents.ts`; suspend SSE reconnect + polling when tab is backgrounded. Resume immediately on focus. | Competitor: mission-control | New |
| RT-4 | **Convergence detection** — detect agent stuck in tool-call loop: same tool called >N times with identical args within a sliding window. Expose `convergenceAlert` on `Agent` type; show warning badge in AgentRow/AgentCard. | DB: `convergence-detection` | Open |

### 2.2 Cost Intelligence

| ID | Feature | Source | Status |
|----|---------|--------|--------|
| CI-1 | **Per-task cost attribution with stage breakdown** — accumulate token/cost per `stage_run`; expose in TaskModal as a cost waterfall (stage → cost). Requires joining `stage_runs` with session JSONL cost data. | DB: `per-task-cost-attribution` | Open |
| CI-2 | **Cost budget enforcement** — orchestrator tick checks `task.cost_budget_cents` against accumulated cost; sends SIGTERM + `fail` transition when exceeded. Surfaces as `budget_exceeded` termination reason. | DB: `cost-budget-enforcement` | Open |
| CI-3 | **Cache token cost split** — parse `cache_creation_input_tokens` and `cache_read_input_tokens` from JSONL `usage` blocks separately. Show as distinct sub-rows in token breakdown UI. Data is already in JSONL; requires parser + type changes. | Competitor: phuryn/hoangsonww | New |
| CI-4 | **Compaction-aware token baseline** — detect `context_window_exceeded`/compaction events in JSONL; preserve pre-compaction token totals as a baseline offset so cumulative counts survive session resets. | DB: `compaction-token-baseline` | Open |
| CI-5 | **Hourly/daily usage heatmap** — day-of-week × hour-of-day heatmap sourced from `agent_cost_trend` table. Shows when token spend concentrates. New component `CostHeatmap.vue`. | Competitor: ccboard | New |
| CI-6 | **30-day cost forecast + budget alerts** — linear regression over last 30 days of `agent_cost_trend`; project next 7 days. Alert threshold tiers (warn/critical) configurable in Settings. | Competitor: ccboard | New |
| CI-7 | **Agent health/anomaly score** — composite 0–100 score per agent: success rate (40%), cache hit % (25%), error rate (25%), cost spike vs 7-day avg (10%). Displayed as a ring/chip on AgentCard. | Competitor: hoangsonww + Lokesh-shiva | New |
| CI-8 | **Claude Pro/Max quota tracking** — read `~/.claude/usage-data/` session-meta for subscription period token totals; show quota progress bar in header. Graceful no-op if data absent. | Competitor: phuryn | New |

### 2.3 Visualization & Analytics

| ID | Feature | Source | Status |
|----|---------|--------|--------|
| VA-1 | **Execution waterfall (Gantt timeline)** — per-session Gantt chart of tool calls with start/end timestamps derived from JSONL. New `ExecutionWaterfall.vue` shown as a tab in `AgentModal`. | DB: `execution-waterfall-timeline` | Open |
| VA-2 | **D3 workflow visualizations** — 4 on-demand chart types: tool-call Sankey, session DAG, spawn-tree, tool co-occurrence matrix. New view toggle entry "Workflows". 4 new endpoints: `GET /api/visualizations/{sankey,dag,spawn-tree,co-occurrence}`. | DB: `d3-workflow-visualizations` | Open |
| VA-3 | **Task dependency graph** — visual DAG of task dependencies using existing `task_dependencies` table. Dedicated tab in PipelineBoard or TaskModal. D3 force-directed layout. | Competitor: mukul975 | New |
| VA-4 | **Epic grouping with completion %** — group tasks by `parent_task_id`; collapse into epic rows with `done/total` progress ring. Applies to kanban and list views. | Competitor: epatel | New |

### 2.4 Developer Experience

| ID | Feature | Source | Status |
|----|---------|--------|--------|
| DX-1 | **Git status panel for task worktrees** — `GET /api/tasks/:id/git` returns `{branch, aheadCount, behindCount, staged, unstaged, lastCommit}`. `POST /api/tasks/:id/git/actions` accepts `{action: fetch|pull|log}`. New `<GitStatusPanel>` section in TaskModal. | DB: `git-dashboard-worktree` | Open |
| DX-2 | **Worktree command runner (one-shot HTTP)** — `POST /api/tasks/:id/run` executes a pre-approved bash command in the task worktree; streams stdout/stderr. No PTY — one-shot only. Safeguarded by `ALLOWED_COMMANDS` denylist. | DB: `pty-terminal-panel` | Open |
| DX-3 | **Slash-command vocabulary in prompt input** — `/` prefix in prompt textarea triggers autocomplete menu with scoped commands (e.g. `/retry`, `/reset-stage`, `/grant`). New `SlashCommandMenu.vue`. | DB: `slash-command-vocabulary` | Open |
| DX-4 | **AI Edit Gate with diff preview** — before an AI-proposed file edit lands, show a JSON diff preview in a modal with Accept/Reject. Requires hooking into the hooks ingestion endpoint (RT-1 prerequisite). | DB: `ai-edit-gate-diff-preview` | Open |
| DX-5 | **Spotlight / Cmd+K global search** — full-text search across agents, tasks, and session output. `Cmd+K` opens overlay. Backend: SQLite FTS5 virtual table over task titles/descriptions + in-memory agent index. | Competitor: epatel | New |
| DX-6 | **Memory file browser** — browse and inline-edit `~/.claude/projects/*/memory/` files via new `GET/PUT /api/memory` endpoints. New `MemoryBrowser.vue` panel accessible from Settings. | Competitor: tenacitOS | New |

### 2.5 Security & Audit

| ID | Feature | Source | Status |
|----|---------|--------|--------|
| SA-1 | **Audit log UI** — new "Audit" tab in TaskModal showing `audit_log` rows for this task. Global admin page `/settings/audit` with date filter + export. | DB: `audit-log-ui` | Open |
| SA-2 | **Webhook HMAC-SHA256 signing** — opt-in signing of outbound webhooks using Stripe-compatible `X-Dashboard-Signature` header. Config keys `webhook_hmac_enabled` + `webhook_hmac_secret` in `notification_config`. | DB: `webhook-hmac-signing` | Open |
| SA-3 | **Permission re-request transparency** — show re-request count per permission type in TaskModal; surface `blockedByPendingPermissions` badge on kanban card. Auto-grant button for trusted tools (behind explicit click). | DB: `permission-loop-ui-transparency` | Open |
| SA-4 | **Permission template picker UI** — named profile chips (Research Only / Standard / Full Access / Tests Only) instead of raw tool lists in task creation form. Maps to existing `permissionTemplates.ts`. | Competitor: mission-control | New |

### 2.6 Notifications & Export

| ID | Feature | Source | Status |
|----|---------|--------|--------|
| NE-1 | **Web Push (VAPID) notification adapter** — browser push notifications for task completion/failure/on_hold events via Web Push API. New `adapters/webpush.ts`. VAPID key pair stored in `notification_config`. | DB: `web-push-vapid` | Open |
| NE-2 | **JSON/CSV export** — `GET /api/tasks/export?format=json|csv` downloads full task list with stage history. Client-side trigger in PipelineBoard toolbar. | Competitor: mukul975 | New |
| NE-3 | **N-gram workflow pattern discovery** — scan session JSONL tool sequences for recurring N-grams; surface top patterns as suggestions ("This sequence appears in 12 sessions — consider extracting as a CLAUDE.md skill"). Background job, results cached. | Competitor: ccboard | New |

### 2.7 History & Data Quality

| ID | Feature | Source | Status |
|----|---------|--------|--------|
| HD-1 | **Watchdog: API error detection** — scan JSONL transcripts for quota exhausted / rate-limited / auth failed strings that Claude's lifecycle hooks never emit. Extend `Agent` type with `errorState: 'quota_exhausted' | 'rate_limited' | 'auth_failed' | null`. | DB: `watchdog-error-scanner` | Open |
| HD-2 | **Historical session import (bulk rescan)** — `POST /api/history/import` triggers full rescan of `~/.claude/projects/` JSONL archive; persists aggregated stats to `agent_cost_trend`. Progress streamed via SSE. | DB: `history-import-pipeline` | Open |

### 2.8 Identity & Platform

| ID | Feature | Source | Status |
|----|---------|--------|--------|
| IP-1 | **Per-agent color/emoji identity** — user-configurable color + emoji per agent slug (stored in `localStorage` or a lightweight `agent_identities` table). Shown on AgentRow, AgentCard, AgentModal header. | Competitor: tenacitOS | New |
| IP-2 | **Python statusline CLI** — `python3 scripts/statusline.py` prints a one-line agent summary for shell prompt integration (PS1/RPROMPT). Reads `/api/agents` endpoint. | DB: `python-statusline-cli` | Open |
| IP-3 | **PWA support** — service worker with shell caching; `manifest.json` for installability. UI loads when local server temporarily unreachable (cached shell only; data degrades gracefully). | Competitor: mukul975 | New |
| IP-4 | **Cross-session full-text search** — SQLite FTS5 virtual table `session_fts` over assistant message content. Enables `/api/search?q=` endpoint for deep session transcript search. Feeds into DX-5 (Spotlight). | Competitor: ccboard | New |

---

## 3. Priority Matrix

### P0 — Infrastructure & Correctness (ship first; unblock other features)

| ID | Feature | Why P0 |
|----|---------|--------|
| RT-1 | Claude Code Hooks ingestion | Unblocks DX-4 (diff gate); reduces polling lag to ~0ms |
| RT-2 | Incremental JSONL byte-offset reads | Performance foundation; prevents O(n) re-parse on every poll |
| CI-4 | Compaction-aware token baseline | Data correctness — all cost/token displays wrong without this |
| CI-2 | Cost budget enforcement | Prevents runaway cost on pipeline tasks |
| RT-4 | Convergence detection | Prevents infinite-loop agent burns |

### P1 — High Value, Manageable Scope

| ID | Feature |
|----|---------|
| CI-3 | Cache token cost split |
| CI-1 | Per-task cost attribution with stage breakdown |
| VA-1 | Execution waterfall (Gantt timeline) |
| DX-1 | Git status panel for worktrees |
| SA-1 | Audit log UI |
| SA-3 | Permission re-request transparency |
| DX-3 | Slash-command vocabulary |
| RT-3 | Tab visibility → pause SSE |
| CI-7 | Agent health/anomaly score |
| SA-2 | Webhook HMAC-SHA256 signing |
| HD-1 | Watchdog: API error detection |

### P2 — Medium Value

| ID | Feature |
|----|---------|
| VA-2 | D3 workflow visualizations |
| CI-5 | Hourly/daily usage heatmap |
| CI-6 | 30-day cost forecast + budget alerts |
| VA-3 | Task dependency graph |
| DX-4 | AI Edit Gate with diff preview (requires RT-1) |
| DX-2 | Worktree command runner |
| SA-4 | Permission template picker UI |
| NE-2 | JSON/CSV export |
| NE-1 | Web Push (VAPID) |
| HD-2 | Historical session import |

### P3 — Nice-to-Have

| ID | Feature |
|----|---------|
| DX-5 | Spotlight / Cmd+K global search (requires IP-4) |
| VA-4 | Epic grouping with completion % |
| DX-6 | Memory file browser |
| IP-4 | Cross-session FTS (SQLite FTS5) |
| NE-3 | N-gram workflow pattern discovery |
| IP-1 | Per-agent color/emoji identity |
| IP-2 | Python statusline CLI |
| CI-8 | Claude Pro/Max quota tracking |
| IP-3 | PWA support |

---

## 4. Phased Implementation Roadmap

### Phase 1 — Foundation (P0, ~2 weeks)
Goal: Fix data correctness, performance, and runaway-agent protection.

1. RT-2: Incremental JSONL byte-offset reads (`jsonlParser.ts`)
2. CI-4: Compaction-aware token baseline (`jsonlParser.ts`)
3. RT-4: Convergence detection (`agentMerger.ts` + `Agent` type)
4. HD-1: Watchdog error detection (`agentMerger.ts` + `Agent` type)
5. CI-2: Cost budget enforcement (`pipeline/orchestrator.ts` tick)
6. RT-1: Hooks ingestion (`server/routes/hooksRoutes.ts` + script)

### Phase 2 — Cost & Monitoring Intelligence (P1 part 1, ~2 weeks)
Goal: Give users clear cost visibility and actionable monitoring.

7. CI-3: Cache token cost split (parser + types + UI)
8. CI-1: Per-task cost attribution (stage_run join + TaskModal waterfall)
9. CI-7: Agent health/anomaly score (new `healthScore` field + AgentCard ring)
10. RT-3: Tab visibility → pause SSE (one-liner in `useAgents.ts`)

### Phase 3 — Developer UX & Security (P1 part 2, ~2 weeks)
Goal: Reduce friction in daily agent work.

11. DX-1: Git status panel (new endpoint + GitStatusPanel.vue)
12. DX-3: Slash-command vocabulary (SlashCommandMenu.vue)
13. SA-3: Permission re-request transparency (TaskModal badge + kanban chip)
14. SA-1: Audit log UI (TaskModal tab + /settings/audit page)
15. SA-2: Webhook HMAC signing (notification_config + adapter.ts)

### Phase 4 — Visualization (P2 part 1, ~3 weeks)
Goal: Make agent behavior visually legible.

16. VA-1: Execution waterfall Gantt (AgentModal tab)
17. VA-3: Task dependency graph (D3 DAG, PipelineBoard tab)
18. CI-5: Hourly usage heatmap (CostHeatmap.vue)
19. CI-6: 30-day cost forecast (linear regression + alert thresholds)
20. VA-2: D3 workflow visualizations (4 chart types + endpoint)

### Phase 5 — Integrations & Data (P2 part 2, ~2 weeks)

21. DX-4: AI Edit Gate with diff preview
22. DX-2: Worktree command runner
23. SA-4: Permission template picker UI
24. NE-2: JSON/CSV export
25. NE-1: Web Push VAPID
26. HD-2: Historical session import

### Phase 6 — Power Features (P3, ~3 weeks)

27. IP-4: Cross-session FTS (SQLite FTS5 virtual table)
28. DX-5: Spotlight / Cmd+K global search
29. VA-4: Epic grouping with completion %
30. DX-6: Memory file browser
31. NE-3: N-gram workflow pattern discovery
32. IP-1: Per-agent color/emoji identity
33. IP-2: Python statusline CLI
34. CI-8: Claude Pro/Max quota tracking
35. IP-3: PWA support

---

## 5. Architecture Notes

### Backend Changes

**New endpoints required:**
- `POST /api/hooks/event` — hooks ingestion (RT-1)
- `GET /api/tasks/:id/git` + `POST /api/tasks/:id/git/actions` — git panel (DX-1)
- `POST /api/tasks/:id/run` — command runner (DX-2)
- `GET /api/visualizations/{sankey,dag,spawn-tree,co-occurrence}` — D3 data (VA-2)
- `GET /api/tasks/export` — CSV/JSON export (NE-2)
- `POST /api/history/import` — bulk rescan (HD-2)
- `GET/PUT /api/memory` — memory browser (DX-6)
- `GET /api/search` — FTS search (IP-4 + DX-5)

**DB migrations required:**
- `agent_cost_trend` extension: `cache_write_tokens`, `cache_read_tokens` columns (CI-3)
- `stage_runs` extension: `cost_cents_accumulated` column (CI-1)
- `agent_identities` table (optional) OR localStorage (IP-1)
- SQLite FTS5 virtual table `session_fts` (IP-4)

**Modified modules:**
- `server/jsonlParser.ts` — byte-offset caching (RT-2), compaction baseline (CI-4), cache token split (CI-3), convergence detection (RT-4), watchdog (HD-1)
- `server/agentMerger.ts` — health score (CI-7), errorState (HD-1)
- `server/pipeline/orchestrator.ts` — cost budget enforcement (CI-2)
- `server/notifications/adapters/webhook.ts` — HMAC signing (SA-2)

### Frontend Changes

**New components:**
- `ExecutionWaterfall.vue` (VA-1)
- `CostHeatmap.vue` (CI-5)
- `GitStatusPanel.vue` (DX-1)
- `SlashCommandMenu.vue` (DX-3)
- `DependencyGraph.vue` (VA-3)
- `SpotlightSearch.vue` (DX-5)
- `MemoryBrowser.vue` (DX-6)

**Modified components:**
- `AgentModal.vue` — add Waterfall tab, Git panel section
- `AgentCard.vue` — add health score ring, color/emoji identity
- `TaskModal.vue` — add Audit tab, permission transparency badge, cost waterfall
- `PipelineBoard.vue` — add dependency graph tab, epic grouping, export button
- `App.vue` — add Spotlight overlay, tab visibility listener
- `useAgents.ts` — tab visibility pause (RT-3)

### Shared Concerns

- **Security:** `POST /api/tasks/:id/run` must validate commands against an `ALLOWED_COMMANDS` allowlist. Memory browser `PUT /api/memory` must restrict writes to `~/.claude/` subtree only.
- **Performance:** D3 visualization endpoints are on-demand only (no materialized views). FTS5 index updated on session close, not on every tail-read.
- **Layering:** All new endpoints follow the layer rules from `task-pipeline.md`. No new runtime dependencies on `pipeline/` internals from `routes/`.
