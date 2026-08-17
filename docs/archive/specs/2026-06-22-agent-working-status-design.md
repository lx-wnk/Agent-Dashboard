# Agent "Working" status overlay

**Date:** 2026-06-22
**Branch:** `feat/agent-working-status` (stacked on `feat/spawn-live-ux` / PR #211 — B-pty needs the pty broker)
**Status:** Design — awaiting review

## Problem

The agent badge (`active`/`waiting`/`idle`/`finished`) is a **staleness proxy**:
`active` = JSONL write < 30s, `waiting` < 5min, `idle` older (`CalculateStatus`,
merger.go). It cannot tell *"the agent is generating right now"* from *"the agent
is idle, waiting for the user."* A live session that just answered and is
awaiting input shows "Waiting" (correct), but a session actively generating a
long reply also shows "Waiting" once 30s pass without a JSONL write — so the user
can't see that it's working.

## Goal

Add an orthogonal **`working`** signal (overlay, not a new status value) derived
from two complementary sources, and surface it in the badge as an animated
"Working" state that takes priority over the staleness status.

- **A — turn-state (all agents):** the agent owes a response → working.
- **B — live output (live sessions):** output flowed in the last few seconds →
  working. Covers the "thinking quietly during a long generation" gap that A
  misses (no JSONL writes mid-stream).

`working = turnOpen(A) || recentOutput(B)`

## Decisions

- **Overlay flag**, not a new `AgentStatus` value: add `Working bool` to
  `sdk.Agent`. `Status` (active/waiting/idle/finished) stays staleness-based. The
  badge shows "Working" when `Working==true`, else the status. Non-breaking;
  `STATUS_ORDER` and every status consumer untouched.
- **B covers both transports:** pty broker `lastOutputAt` (pty sessions) **and**
  a `tmux #{window_activity}` query (tmux sessions).

## Architecture

### A — turn-state (parser + merger)
- **Parser:** add `TurnOpen bool` to `parser.SessionData`. Set it while parsing
  the session: `TurnOpen = (PendingToolUse != nil) || (last parsed message role
  == "user")`. I.e. the agent has a tool in flight, or the user sent the latest
  message and no assistant turn has closed it. A completed trailing `assistant`
  message → `TurnOpen=false`. (Track the last entry's role/type during the
  existing parse loop; `PendingToolUse` is already computed.)
- **merger `buildAgent`:** `working := session.TurnOpen`. (B may also set it —
  see merge below.)

### B — live output activity
A session is "live" when it has a channel discovery file (pty or tmux pane).

- **B-pty:** `RunHeadlessPTY` (added in #211) currently drains output with
  `io.Copy(io.Discard, ptmx)`. Replace the discard with a small writer that
  records `lastOutputAt` (atomic, monotonic). The broker writes `lastOutputAt`
  into its `{pid}.pty.json` discovery file, refreshed when output flows
  (debounced to ≤ ~1s, alongside the existing token-rotation rewrite). `cwd()`
  etc. stay.
- **B-tmux:** for an agent whose discovery file carries a `tmuxPane`, the merger
  queries `tmux display-message -p -t <pane> '#{window_activity}'` (a unix
  timestamp of the pane's last activity). Behind a small seam
  (`var tmuxActivityFn = realTmuxActivity`) so tests inject it; failures (no
  tmux / dead pane) contribute nothing (not working from B).
- **merger:** `recentOutput := now - lastOutputAt < outputThreshold` (pty) OR
  `now - windowActivity < outputThreshold` (tmux). `outputThreshold` ≈ 5s
  (const next to `activeThreshold`).

### Merge + Agent field
`buildAgent`: `agent.Working = session.TurnOpen || recentOutput`. New field
`Working bool json:"working"` on `sdk.Agent` (regen `sdk.generated.ts` via
`task generate`).

### Frontend
- `AppBadge` (or the status-rendering site): when `agent.working`, render a
  distinct animated "Working" badge (e.g. pulsing dot + "Working" label) instead
  of the staleness label. Add `working` handling to `statusLabel`/tone if those
  are reused. Keep it accessible (text label, not color-only).
- `agentSort` (optional, recommended): sort working agents above idle/waiting so
  active work is visible. Decide in plan; keep minimal.

## Error handling

- Parser: if the last entry fails to parse, `TurnOpen` defaults false (agent
  looks not-working — consistent with the existing "looks idle" fallback).
- B-tmux: `tmux display-message` error/timeout → no B signal (rely on A). Never
  block the tick — use a short timeout (e.g. 500ms) on the tmux call.
- B-pty: missing/old `lastOutputAt` → no B signal.
- Performance: the tmux query runs once per tmux-agent per tick; cap with the
  timeout. If this proves heavy, cache per pane for a tick.

## Testing

- **Parser unit:** `TurnOpen` true when last entry is a `user` message; true when
  `PendingToolUse` set; false when the trailing entry is a completed `assistant`
  message. Use JSONL fixtures.
- **merger unit:** `buildAgent` sets `Working` from `TurnOpen`; the B-tmux seam
  (`tmuxActivityFn`) → recent activity flips `Working` true; stale activity does
  not. Inject the seam; no real tmux.
- **B-pty unit:** `RunHeadlessPTY` records `lastOutputAt` when the child emits
  output and the discovery file reflects it (extend the existing
  `headlesspty_test.go` with a child that prints, assert `lastOutputAt` advances).
- **Frontend (vitest):** badge renders "Working" when `agent.working` regardless
  of `status`; falls back to the status label when not working.
- No real-claude/real-tmux — seams + fixtures.

## Out of scope

- Replacing the staleness statuses (kept as-is).
- A persistent "was working" history.
- Sub-agent working state (only top-level agents for now).

## Risks

- tmux `#{window_activity}` reflects ANY pane output (incl. the seeded prompt
  echo), so a just-started session may briefly show working — acceptable (it IS
  starting). The ≤5s threshold keeps it tight.
- Debounced pty `lastOutputAt` writes add minor file churn; bounded by the
  debounce interval.
