# Claude Agent Overview — Design Spec

## Context

When running multiple Claude Code sessions in parallel (CLI + Desktop App), there's no way to see at a glance what each agent is doing, which project it's working in, or what its current status is. This dashboard provides a real-time web overview of all running Claude Code agents.

Inspired by [pixel-agents](https://github.com/pablodelucca/pixel-agents), but as a standalone web dashboard (not VS Code-specific).

## Requirements

### Core (Phase 1)
- Real-time overview of all running Claude Code agents
- Table view with: Status, Project, Current Action, Uptime, PID
- Subagent rows indented below parent sessions
- Offcanvas detail panel on row click (right sidebar)
- Detail shows: full path, session ID, entrypoint, tool timeline, tasks, subagents
- 3-second polling interval
- Works with both CLI (`claude`) and Desktop App sessions

### Later (Phase 2)
- Session history: browse and search past sessions
- Session start/stop from dashboard

## Architecture

```
Browser (Vue 3 SPA)  ──polling 3s──▶  Node.js Express (localhost:3120)
                                         ├── ProcessScanner (ps + lsof)
                                         ├── JsonlParser (~/.claude/projects/)
                                         └── AgentMerger (PID ↔ Session)
```

### Data Sources

1. **Process list** — `ps aux | grep claude` identifies running instances with PIDs
2. **CWD mapping** — `lsof -d cwd -p <PID>` maps each PID to its working directory
3. **JSONL transcripts** — `~/.claude/projects/<encoded-path>/<session-id>.jsonl` contains session data (messages, tool calls, timestamps, entrypoint)
4. **Subagent files** — `<session-id>/subagents/agent-<id>.jsonl` for spawned subagents
5. **Project mapping** — directory name in `~/.claude/projects/` encodes the project path (dashes replace slashes)

### Status Heuristics

- **active** — process running AND JSONL modified within last 30s
- **waiting** — process running, JSONL unchanged for 30s–5min (awaiting user input/approval)
- **idle** — process running, JSONL unchanged for >5min

## Data Model

```typescript
interface Agent {
  pid: number
  sessionId: string
  projectPath: string
  projectName: string
  cwd: string
  entrypoint: 'cli' | 'desktop' | 'unknown'
  status: 'active' | 'waiting' | 'idle'
  uptime: number
  lastActivity: string
  currentAction: string | null
  lastTools: string[]
  tasks: TaskInfo[]
  subagents: SubAgent[]
}

interface SubAgent {
  id: string
  type: string
  status: 'active' | 'completed'
  currentAction: string | null
  sessionFile: string
}

interface TaskInfo {
  id: string
  subject: string
  status: 'pending' | 'in_progress' | 'completed'
}
```

## API

```
GET /api/agents → Agent[]
```

Single endpoint, stateless, polled every 3 seconds by the frontend.

## Tech Stack

- **Backend:** Node.js + Express + TypeScript
- **Frontend:** Vue 3 (Composition API) + Vite + TypeScript
- **Dev:** Vite dev server with Express middleware (single process)
- **No external DB** — all data read live from filesystem + process list

## Frontend Components

```
App.vue
├── AgentTable.vue          // Main table
│   ├── AgentRow.vue        // One row per agent
│   └── SubAgentRow.vue     // Indented subagent rows
├── AgentDetail.vue         // Offcanvas right sidebar
│   ├── DetailHeader.vue    // Name, status, PID, path
│   ├── ToolTimeline.vue    // Recent tool calls
│   ├── TaskList.vue        // Session tasks
│   └── SubAgentList.vue    // Subagents with detail
└── StatusBadge.vue         // Reusable: active/waiting/idle
```

## Project Structure

```
claude-agent-overview/
├── package.json
├── vite.config.ts
├── server/
│   ├── index.ts            // Express + Vite dev middleware
│   ├── processScanner.ts   // ps + lsof
│   ├── jsonlParser.ts      // JSONL tail-reader
│   └── agentMerger.ts      // PID ↔ session matching
├── src/
│   ├── main.ts
│   ├── App.vue
│   ├── components/
│   ├── composables/
│   │   └── useAgents.ts    // Polling logic
│   └── types.ts
└── index.html
```

## Commit Strategy

Each development step gets its own commit:

1. `init: project scaffold with Vite + Vue 3 + Express`
2. `feat: process scanner module (ps + lsof)`
3. `feat: JSONL parser with tail-reading`
4. `feat: agent merger (PID ↔ session matching)`
5. `feat: REST API endpoint /api/agents`
6. `feat: agent table component with status badges`
7. `feat: subagent rows (indented under parent)`
8. `feat: offcanvas detail panel`
9. `feat: polling composable with 3s interval`
10. `feat: tool timeline + task list in detail view`

## UI Design

- Dark theme (matches terminal aesthetic)
- Table as main view with sortable columns
- Status indicated by colored dot (green/yellow/gray)
- Row click opens right-side offcanvas with full details
- Subagents shown as indented rows with smaller font/lighter color
- Responsive — works on different screen widths

## Startup

```bash
cd claude-agent-overview
npm install
npm run dev
# Opens http://localhost:3120
```
