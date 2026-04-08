# Card View Toggle with Prompt Submit and Output Display

## Overview

Add a toggle between list (table) and card (tile) view to the Claude Agent Overview dashboard. Cards use a terminal-style design showing the last assistant output and an inline prompt input. Clicking a card opens a full-screen modal overlay with the complete session output, replacing the current off-canvas AgentDetail panel. A search input in the header filters agents across all views.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Card style | Terminal-style (Option C) | Matches the CLI nature of Claude Code agents |
| Expand behavior | Modal overlay (Option C) | Maximum space for output, clear focus state |
| Modal vs Detail panel | Modal replaces Detail panel | Single unified detail mechanism, less complexity |
| Prompt mechanism (running) | Channel messaging | Direct communication with running agents via MCP |
| Prompt mechanism (stopped) | Resume via spawn | Resumes stopped sessions with `resumeSessionId` |
| Toggle position | In header | No extra vertical space, header is already the place for global actions |
| Search | Header search input | Client-side filtering across multiple fields |

## 1. Header Changes

### View Toggle
- Icon-pair button group (≡ list / ⊞ cards) in the header, positioned before the Sessions and + New Agent buttons
- Active view highlighted with `--accent` color
- Preference persisted in `localStorage`

### Search Input
- Text input field in the header
- Filters agents in real-time (client-side) across: `projectName`, `projectPath`, `lastOutput`, `sessionId`, `currentAction`
- Debounced input (200ms) to avoid excessive re-renders
- Clear button (✕) when input is non-empty

## 2. Card View (Terminal-Style Tiles)

### Layout
- CSS Grid, responsive columns:
  - `< 768px`: 1 column
  - `768px–1200px`: 2 columns
  - `> 1200px`: 3 columns
- Gap: 12px
- Cards fill available width within their column

### Card Structure
Each card resembles a mini terminal window:

```
┌─────────────────────────────────────────┐
│ ● my-project  · sonnet-4 · $0.12       │  ← Title bar (dark bg)
│   45k tok · 12min                       │
├─────────────────────────────────────────┤
│ I've updated the authentication         │  ← Output body (darker bg)
│ middleware to handle token refresh.     │     Last assistant message
│ The changes include a new               │     Markdown-rendered
│ refreshToken() function that...         │     Max ~6 lines, overflow hidden
├─────────────────────────────────────────┤
│ ❯ Enter prompt...                    ↵  │  ← Prompt input
└─────────────────────────────────────────┘
```

**Title bar contents:**
- Status dot (green=active, yellow=waiting, gray=idle)
- Project name (bold)
- Model name (shortened)
- Cost estimate
- Token count and uptime (secondary text)

**Output body:**
- Last assistant text message from the JSONL session
- Rendered as Markdown (using existing styling conventions)
- Max height with overflow hidden, fade-out gradient at bottom
- If no output available: show "No output yet" placeholder

**Prompt input:**
- Single-line input with ❯ cursor indicator
- Submit button (↵) or Enter key to send
- Disabled state when agent is idle/completed without channel

### Card Click Behavior
- Clicking the card body (not the prompt input) opens the Modal
- Clicking the prompt input focuses it for typing

## 3. Modal Overlay (Replaces AgentDetail)

The modal replaces the existing off-canvas `AgentDetail` panel entirely. Both list and card view use the same modal for detail display.

### Structure
```
┌─────────────────────────────────────────────────┐
│ ● my-project · sonnet-4 · $0.12 · 45k tok  ⤢ ✕ │  ← Title bar
├─────────────────────────────────────────────────┤
│                                                   │
│ $ claude                                          │
│                                                   │
│ I've updated the authentication middleware to     │  ← Scrollable output
│ handle token refresh. The changes include:        │     Full session content
│                                                   │
│ + export async function refreshToken() {          │     Assistant messages +
│ +   const decoded = jwt.verify(token);            │     Tool calls/results
│ +   ...                                           │
│ + }                                               │
│                                                   │
│ ── Tool: Edit src/auth/middleware.ts ──            │
│                                                   │
│ Tests pass. The refresh logic handles edge cases. │
│                                                   │
├─────────────────────────────────────────────────┤
│ ❯ Enter prompt...                              ↵ │  ← Prompt input
└─────────────────────────────────────────────────┘
```

**Title bar:**
- Status dot + project name + model + cost + tokens + uptime
- Expand button (⤢) for potential future fullscreen
- Close button (✕) or Escape key

**Output area:**
- Full scrollable session output
- Fetched via new API endpoint on modal open
- Content types rendered:
  - Assistant text messages: Markdown-rendered
  - Tool calls: collapsed header showing tool name and file path
  - Tool results: hidden by default, expandable
- Auto-scroll to bottom on open

**Prompt input:**
- Multi-line textarea (auto-grows)
- Submit via Ctrl+Enter or button
- Same logic as card prompt: channel for running, resume for stopped

### Backdrop
- Semi-transparent dark overlay behind modal
- Click backdrop to close

## 4. Backend: Session Output Endpoint

### New Endpoint: `GET /api/agents/:sessionId/output`

Parses the JSONL session file and returns structured conversation content.

**Response format:**
```typescript
interface SessionOutput {
  messages: OutputMessage[]
}

interface OutputMessage {
  role: 'assistant' | 'tool_call' | 'tool_result'
  content: string          // text content or tool name
  timestamp?: string
  toolName?: string        // for tool_call/tool_result
  filePath?: string        // for file-related tool calls
}
```

**Query parameters:**
- `?last=1` — returns only the last assistant message (used by card preview)

**Implementation:**
- Reads the full JSONL session file (not just tail 32KB like current parser)
- Extracts assistant text blocks, tool_use entries, and tool_result entries
- Returns structured array of messages

### Extension to `/api/agents` Response

Add `lastOutput: string | null` field to the `Agent` interface:
- Contains the last assistant text message (plain text, max 500 chars)
- Extracted during the existing JSONL parsing in `jsonlParser.ts`
- Used by card view for preview without extra API calls

## 5. Prompt Submit Logic

### Running Agents (channelAvailable: true)
- Use `POST /api/agents/:sessionId/message` (Channel messaging via MCP)
- Show inline feedback: "Sent" / "Error" status on the prompt input

### Stopped/Old Sessions
- Use `POST /api/agents/spawn` with `resumeSessionId`
- Show "Resuming..." state, then update card/modal when new agent appears in polling

### Error Handling
- Channel not available on running agent: show tooltip explaining channel setup needed
- Spawn fails: show error inline below prompt input
- Rate limiting: respect existing 5/min limit on spawn endpoint

## 6. Search/Filter

### Implementation
- Client-side filtering in `useAgents.ts` composable
- New `searchQuery` ref, exposed alongside `agents`
- Computed `filteredAgents` that applies search across:
  - `agent.projectName`
  - `agent.projectPath`
  - `agent.lastOutput`
  - `agent.sessionId`
  - `agent.currentAction`
- Case-insensitive substring matching
- Debounced at 200ms

## 7. Components Affected

### New Components
- `AgentCard.vue` — single terminal-style card tile
- `AgentCardGrid.vue` — responsive grid of AgentCard components
- `AgentModal.vue` — full-screen modal overlay (replaces AgentDetail)

### Modified Components
- `App.vue` — add view toggle, search input, swap AgentDetail for AgentModal
- `useAgents.ts` — add `lastOutput` field, `searchQuery`, `filteredAgents`

### Removed Components
- `AgentDetail.vue` — replaced by AgentModal

### Backend Modified
- `server/jsonlParser.ts` — extract `lastOutput` from session JSONL
- `server/index.ts` — add `GET /api/agents/:sessionId/output` endpoint
- `src/types.ts` — add `lastOutput` to `Agent`, add `OutputMessage` interface

## 8. Non-Goals

- ANSI color rendering in output (plain text / Markdown only)
- Real-time streaming of agent output (polling-based, same as current)
- Keyboard navigation between cards
- Drag-and-drop card reordering
