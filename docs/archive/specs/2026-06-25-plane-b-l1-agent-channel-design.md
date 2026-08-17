# Plane B — L1: Agent↔Agent Channel (peer messaging)

> Status: approved design (2026-06-25). Sibling layer to L0 (coordination primitives, PR #223).
> Prerequisite for L2 (PM-agent). Independent of L2 and shippable on its own.

## Purpose

Give pipeline tasks a first-class, durable way to send each other ordered messages.
The primary consumer is the upcoming **L2 PM-agent**: a coordinator task sends
directives to worker subtasks, and workers report status back. Both sides are
usually headless pipeline stage agents, so delivery is **pull-based** (a durable
inbox the receiver reads), with an opportunistic push for the live-session case.

L1 ships as a passive, independently-testable primitive. The robust worker-delivery
mechanism (injecting unread inbox into a spawning worker's stage prompt) is **L2's**
responsibility, not L1's — this keeps the layer boundary clean.

## Non-goals

- No stage-prompt injection of inbox into spawning workers (that is L2).
- No PM polling loop / orchestration logic (that is L2).
- No message threading (`in_reply_to`) — `from_task` is sufficient for L2. YAGNI.
- No retention/pruning policy — single-user dashboard, low volume.

## Architecture

L1 is structurally a sibling of L0: same identity resolution (`ownerFromCtx`),
same `agent:coord` MCP scope, same shape (ent table → repo → MCP tools →
read-only UI). The only genuinely new mechanism is the opportunistic tmux nudge,
and that delivery path already exists (channel inbound delivery, gated by
`tmuxInjectable`).

### 1. Scope & MCP API

Reuse the existing **`agent:coord`** scope (peer messaging is coordination; avoids
scope proliferation; single-user means any task may message any task). No change to
`scopeImplies`. Two new tools registered in `ToolScopeMap` (auth.go) — a missing
entry panics `AddTool`, so this registration is mandatory:

- `send_peer_message(toTaskId: string, body: string)`
  - `from` is resolved server-side via `ownerFromCtx(ctx, args)` (reuse `coord.go`):
    auth KeyID → owning task, falling back to an optional `ownerTaskId` arg for
    unauthenticated/local callers.
  - Stores the message, then performs opportunistic delivery (§3).
  - Returns `{ id, from_task, to_task, created_at }`.
- `read_inbox(markRead?: bool = true, fromTaskId?: string)`
  - `to` = `ownerFromCtx(ctx, args)`.
  - Returns this task's messages oldest-first (`{ id, from_task, body, created_at, read_at }[]`).
  - When `markRead` is true (default), stamps `read_at = now` on every returned row
    in the same call. Optional `fromTaskId` filters to one sender.

The calling agent already knows its own task id via the `DASHBOARD_TASK_ID` env var
injected at spawn, so it can populate `ownerTaskId` when auth context is absent.

### 2. Data model

New ent schema `peer_message`:

| field        | type      | notes                                   |
| ------------ | --------- | --------------------------------------- |
| `id`         | string    | immutable PK (uuid)                     |
| `from_task`  | string    | sender task id                          |
| `to_task`    | string    | recipient task id                       |
| `body`       | string    | message text                            |
| `created_at` | time.Time | default now, immutable                  |
| `read_at`    | time.Time | nullable; set on first `read_inbox`     |

Indexes: `(to_task, read_at)` for unread lookups, `(to_task, created_at)` for
ordered reads.

`MessageRepo` interface (mirrors the L0 dependency/scratchpad repos):

- `Send(ctx, fromTask, toTask, body) (*ent.PeerMessage, error)`
- `ListInbox(ctx, toTask string, unreadOnly bool, fromTask string) ([]*ent.PeerMessage, error)`
- `MarkRead(ctx, ids []string) error`

> **ent codegen note:** regenerate with `gen.FeatureUpsert` enabled (see the L0/
> dependency repos) or `OnConflict*` helpers are stripped and the repo breaks.
> Do not hand-edit generated files. After codegen, revert any drift in
> `internal/db/ent/runtime/runtime.go` and `sdk.generated.ts`/`go.sum` that an
> errant `task generate` introduces.

### 3. Delivery (opportunistic nudge)

On `send_peer_message`:

1. Always persist the message (durable, pull-readable).
2. Reverse-lookup `toTask` → live session id (reuse the SpawnManager / ReplyStore
   reverse-lookup by task id).
3. If a live session exists and is `tmuxInjectable`, fire the existing tmux
   `send-keys` nudge with a short prompt: *"Peer message from `<fromTask>`; call
   `read_inbox` to read it."*
4. Headless / no-session / non-injectable target → store only; the message awaits
   the recipient's next `read_inbox` pull (which L2 will trigger via stage-prompt
   injection).

Nudge failure is **non-fatal** — log a warning and return success, because the
message is already durably stored. The nudge is a latency optimization, not the
delivery guarantee.

### 4. UI

Extend the existing read-only **Coordination** tab (built in L0) with a messages
view: a per-task inbox/sent list. Backed by a new REST read endpoint
`GET /api/tasks/{id}/messages` (read-only, mirrors L0's coordination endpoints).
No write UI — sending is an agent action, not a human one.

### 5. Error handling

- Unknown `toTaskId` is accepted and stored (the recipient task may be created
  later, or be on another machine); no existence check — consistent with L0's
  free-string namespaces. The message simply sits unread.
- Empty `body` → validation error from the tool.
- All repo errors surface as MCP tool errors; the nudge step never propagates
  errors to the caller.

## Testing strategy

- **Repo:** `Send`, `ListInbox` (unread-only + from filter), `MarkRead`, ordering
  (oldest-first), `read_at` set correctly.
- **MCP tools:** `send_peer_message` resolves `from` via `ownerFromCtx` (KeyID and
  fallback arg); `read_inbox` returns ordered messages, `markRead=true` stamps
  `read_at`, `markRead=false` leaves it null; `fromTaskId` filter.
- **Delivery:** a fake injectable session receives the tmux nudge; a headless
  target is a no-op (message stored, no nudge); nudge error is swallowed and the
  send still succeeds.
- **Auth:** `send_peer_message` / `read_inbox` require `agent:coord` (rejected
  without it).

## Open micro-decisions (resolved)

- `read_inbox` auto-marks read: **yes**, default `true` with override.
- `in_reply_to` threading: **no** (YAGNI).
- Retention/pruning: **none now**.

## Build size

Comparable to the L0 build: one ent table + repo, two MCP tools (+ scope-map
entries), one REST read endpoint, one read-only UI panel, and the tests above.
