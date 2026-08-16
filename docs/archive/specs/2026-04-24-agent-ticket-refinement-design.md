# Agent-Based Ticket Refinement — Design Spec

**Date:** 2026-04-24
**Branch:** feat/agent-based-ticket-refinement
**Status:** Approved, ready for implementation

---

## Overview

Replace the static `BacklogForm` with an interactive, Claude-powered chat that guides the user through the full planning cycle — Analysis, Spec, Implementation Concept, and Approval — before a task enters the execution pipeline.

The chat runs as a series of short-lived `claude --print` spawns (one per user turn), using the user's existing Claude subscription. No additional API key or extra cost.

---

## New Pipeline

```
konzept → backlog → umsetzung → selbstreview → finalisierung → done
```

**Removed stages (full cleanup — no backward compat needed):**
`pruefung`, `refinement`, `planning`, `approval1`, `umsetzungskonzept`, `approval2`

Existing tasks in those stages will be deleted as a one-time migration. All stage handlers, stage prompts, and route logic for those stages are removed from the codebase.

**Stage semantics:**
- `konzept` — Interactive refinement chat is in progress
- `backlog` — "Ready for Doing": chat complete, task fully specified
- `umsetzung` → `selbstreview` → `finalisierung` → `done` — unchanged execution flow

---

## Chat Phases

The Claude system prompt divides the conversation into four sequential phases. Claude signals completion of each phase with `__phase_done: <phase>` in its response.

| Phase | What happens |
|---|---|
| `analyse` | Claude asks for cwd, source/target branch, problem description, complexity estimate |
| `spec` | Refined title, description, success criteria, assumptions, out-of-scope |
| `umsetzungskonzept` | Implementation steps, tool permission list (`toolRequests`) |
| `approval` | Claude summarises everything; user confirms → "Task erstellen" button appears |

The `toolRequests` output from the chat replaces what `umsetzungskonzept` stage used to produce. It is stored in `task.metadata` and consumed by the `umsetzung` agent to build the `.claude/settings.json` allow-list.

---

## Backend Architecture

### Respawn-per-Turn

For each user message:

1. Load full conversation history from `refinement_turns` (DB)
2. Spawn `claude --print` with serialised history injected into the system prompt
3. Capture stdout, stream to client via SSE in real time
4. Persist Claude's response as a new `refinement_turns` row
5. Detect `__phase_done: <phase>` signal → emit `phase_change` SSE event

No long-running process between turns. No extra API key. History survives browser reloads.

### New DB Table

```sql
CREATE TABLE refinement_turns (
  id          TEXT PRIMARY KEY,
  task_id     TEXT NOT NULL REFERENCES pipeline_tasks(id) ON DELETE CASCADE,
  role        TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
  content     TEXT NOT NULL,
  phase       TEXT,
  created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
```

### New API Endpoints

```
POST  /api/refine/:taskId/turn     — Send user message; streams Claude response via SSE
POST  /api/refine/:taskId/confirm  — Persist final spec+plan into task.metadata, advance to backlog
```

Task creation itself reuses the existing `POST /api/tasks` endpoint (stage = `konzept`).

### Spawn Mechanism

Thin variant of the existing spawner — non-detached, stdout/stderr piped, no MCP channel, no settings.json injection. Exits after each response.

---

## Frontend

### `RefinementChat.vue` — replaces `BacklogForm.vue`

The "New Task" button opens the chat modal. The task is created in the background immediately on open (stage = `konzept`) so the conversation is persisted from the first message.

**Layout:**
- Greeting message + 3–4 example chips ("Ein neues Feature", "Ein Bug-Fix", "Refactoring", "Freitext…")
- Chat bubble stream with SSE streaming for Claude responses
- Inline phase milestone badges: `── ✓ Analyse abgeschlossen ──`
- "Task erstellen →" button appears after the `approval` phase is complete
- Input bar always visible at the bottom

### Resuming a konzept task

A `konzept`-stage task card in the Kanban shows a "Chat fortsetzen" button. Clicking it opens the same chat modal with the full history loaded from `refinement_turns`.

### Kanban Board Changes

| Before | After |
|---|---|
| pruefung, refinement, planning, approval1, umsetzungskonzept, approval2, backlog, umsetzung, … | **Konzept**, **Backlog** (Ready for Doing), umsetzung, selbstreview, finalisierung, done |

All removed stage columns are deleted from the board. No legacy column.

---

## Cleanup Checklist

- [ ] Delete `VALID_STAGES` entries: `pruefung`, `refinement`, `planning`, `approval1`, `umsetzungskonzept`, `approval2`
- [ ] Remove from `STAGE_ORDER`
- [ ] Delete stage handlers from `stageHandlers.ts`
- [ ] Delete stage prompts from `stagePrompts.ts`
- [ ] Delete `approval1Handler`, `approval2Handler`, `umsetzungskonzeptHandler`
- [ ] Remove `approval1`, `approval2`, `umsetzungskonzept` from route logic / orchestrator
- [ ] One-time DB migration: delete tasks in removed stages
- [ ] Remove `BacklogForm.vue`, replace with `RefinementChat.vue`
- [ ] Add `konzept` to `VALID_STAGES` and `STAGE_ORDER`
- [ ] Add `refinement_turns` table migration
- [ ] Add `/api/refine` routes
- [ ] Update Kanban board columns
- [ ] Update `umsetzung` handler to read `toolRequests` from `task.metadata` instead of stage output
