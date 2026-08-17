# Finished controllable agents persist on the dashboard

**Issue:** [#192 — Finished agents silently disappear from dashboard](https://github.com/lx-wnk/Agent-Dashboard/issues/192)
**Date:** 2026-06-21
**Status:** Approved design — ready for implementation plan

## Problem

Agents shown on the main dashboard vanish the instant their process exits. The
result is only reachable afterwards via the "Sessions" overview, forcing the
user to hunt for it. The stated goal: **a finished agent's result must stay
clearly visible on the dashboard, then be explicitly dismissable.**

### Root cause

The agent roster is built from **live processes only**. `merger.GetAgents()`
scans `ScanProcesses()` (ps/lsof); once a PID is gone, the next SSE tick drops
the agent. There is no terminal status — `AgentStatus` is only
`active`/`waiting`/`idle`, all of which require a live process.

## Scope

Persist a "finished" card **only for controllable agents** — those connected to
the dashboard channel/MCP, i.e. that have a discovery file in
`~/.claude/dashboard-channel/{pid}.json` (written by `channel/bridge.go`). This
includes pipeline-task agents (no exclusion). Terminal-started agents without a
channel are out of scope and still vanish as today.

Decided scope qualifiers:

- **Restart scope: session-scoped.** Only agents the dashboard observed
  transition live→dead during the current server run become finished cards.
  After a server restart, finished cards are gone (still available in the
  Sessions list). This avoids surfacing a pile of historical discovery files on
  startup and matches the "I was watching it finish" mental model.
- **Dismissal is durable.** Dismissing deletes the discovery file, so a
  dismissed agent never reappears, even across restarts.

## Enabling mechanic

`~/.claude/dashboard-channel/{pid}.json` discovery files **survive the agent
process exit** — `bridge.go` never removes them, and `merger.channelDiscovery()`
only stats them. The blocker is purely the live-process gating of the roster.
So the fix is "emit controllable agents even when their PID is dead," and
dismissal-by-delete additionally fixes the existing discovery-file leak.

## Design

### A. Detection (server — `merger.GetAgents`)

Each tick:

1. Build live agents as today; each is already enriched with
   `ChannelAvailable`/`LiveInjectable` via `channelDiscovery(pid)`.
2. Maintain an in-memory `seenLive` map (`pid → sessionId`) of channel-connected
   agents observed live during this server run.
3. Scan `~/.claude/dashboard-channel/` for `{pid}.json` whose PID is **not** in
   the live process set. For each that was previously in `seenLive`, emit a
   **stale agent**:
   - Resolve `sessionId` from the `seenLive` cache (fallback: newest `.jsonl` by
     mtime in the discovery file's `cwd`).
   - Reconstruct the card from disk — title, cost, tokens, last activity, last
     assistant message — reusing existing parser code
     (`parser` session helpers; `GET /api/agents/{sessionId}/output` already
     exists for the click-through result).
4. Merge live + stale agents, dedup by `sessionId` (a session is never both).

**Rejected alternative — write `sessionId` back into the discovery file:** the
bridge periodically rotates its token and rewrites `{pid}.json`, which would
clobber any server-written field. The in-memory `seenLive` cache avoids this.

### B. Status model (SSOT — Go + TS kept in parity by hand)

Add a new terminal status `finished` to `AgentStatus`:

- Go: `sdk/types.go` (`AGENT_STATUSES` equivalent / `AgentStatus`).
- TS: `src/types.ts` (`AGENT_STATUSES`, `AgentStatus`).
- Sort: `src/utils/agentSort.ts` (`STATUS_ORDER`) — `finished` sorts to the
  bottom, below `idle`.

A dead-process controllable agent gets `status: "finished"` regardless of
last-activity age. `LiveInjectable` is `false` (the tmux pane / pty is gone) but
`ChannelAvailable` stays `true`, so the card keeps its control affordance;
sending a message uses the existing **resume** path (`claude --resume
{sessionId}`), consistent with current non-injectable behaviour.

### C. Dismissal (server)

New endpoint `DELETE /api/agents/{pid}/channel`:

- JWT-protected and rate-limited, consistent with the other control endpoints
  (`/api/agents/{pid}/message`).
- Deletes `~/.claude/dashboard-channel/{pid}.json` and the `{pid}.pty.json`
  sidecar if present.
- **Guard:** refuse (e.g. 409) if the PID is currently a live process — only
  finished agents are dismissable. This enforces the "X enabled only when
  finished" requirement and prevents breaking control of a live agent.
- `pid` is parsed as an int, so the path component cannot escape the discovery
  directory.

### D. Frontend

- `src/components/AgentCard.vue`: when `status === 'finished'`, render a
  "finished" badge and an **X** dismiss button (top-right). Clicking the card
  still opens the result via the existing `/api/agents/{sessionId}/output`. The X
  calls `DELETE /api/agents/{pid}/channel`, then optimistically removes the card
  from the store; the next SSE frame confirms removal.
- `src/composables/useAgents.ts`: the store already replaces the agent list per
  SSE frame, so finished agents simply arrive in the frame instead of
  disappearing. No structural change beyond wiring the dismiss action.
- `src/components/AgentCardGrid.vue`: already keyed by `pid`; add/remove handled
  by Vue reactivity.

### Data flow

```
process exits
  → next merger tick: PID absent from ScanProcesses
  → seenLive has pid→sessionId, discovery file still present
  → emit stale agent (status=finished) reconstructed from JSONL
  → SSE frame includes finished card
  → user clicks card → /output result; or clicks X
  → DELETE /api/agents/{pid}/channel removes discovery file
  → next tick: file gone → agent no longer emitted
```

## Error handling

- Missing/unreadable JSONL for a stale agent: skip emitting that card (no broken
  card); log at debug.
- `seenLive` miss AND no resolvable JSONL in `cwd`: skip the card.
- `DELETE` on a live PID: `409 Conflict`, no file change.
- `DELETE` when the file is already gone: idempotent `204`.

## Testing

- **Go unit (merger):** given a `seenLive` entry plus a discovery file for a dead
  PID, `GetAgents` emits exactly one finished agent with reconstructed fields;
  dedup when the same sessionId is also live; no emission for a PID never seen
  live (session-scoped guarantee).
- **Go unit (handler):** `DELETE …/channel` removes both files; returns 409 for a
  live PID; idempotent when already absent; rejects non-int pid.
- **Vitest:** `AgentCard` renders badge + X only for `status === 'finished'`; X
  emits the dismiss action; finished sorts last (`STATUS_ORDER`).
- **Playwright (optional):** spawn → finish → finished card visible → dismiss →
  card removed.

## Out of scope / YAGNI

- Auto-expiry / TTL of finished cards (session-scoped + manual dismiss is
  enough).
- Disk-scoped persistence across restarts.
- Persisting finished cards for non-channel (terminal-started) agents.

## Docs to update with the change (per project rule)

`README.md`, `CHANGELOG.md` (Keep a Changelog), `CONTRIBUTING.md` if a
command/workflow changes, and any affected `docs/`.
