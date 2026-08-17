# Permission-Grant Resume with Handoff Note

**Date:** 2026-04-29  
**Branch:** feat/robustness-hardening  
**Status:** Approved

## Problem

When a stage agent requests a permission mid-execution and the user grants it, the agent is currently killed and restarted from scratch. The new agent has no memory of what was already accomplished, what tools it already used, or where it was in the task. This means redundant work and agents that may fail to reproduce the same state they were in when the permission was needed.

## Goal

After a permission is granted, the restarted agent should:
1. Resume the prior conversation via `--resume <sessionId>` (full transcript context)
2. Receive a short handoff note explaining it was paused for a permission request that has now been granted
3. Fall back gracefully (handoff note only, no `--resume`) when `session_id` is not yet attached

## Approach

Use `--resume <sessionId>` as the primary continuation mechanism, plus inject a `userAdditionalPrompt` handoff note. Both mechanisms already exist in the spawn infrastructure and are threaded through `progressTask` → `buildContext` → `createAgentStage` → `spawnStageAgent`. The permission-grant path simply never passes them.

## Design

### Data Flow

```
POST /api/permission-requests/:id/resolve (outcome=granted)
  │
  ├── write task_permissions row (new tool in allow-list)
  ├── capture: run.session_id, permission.tool, permission.pattern
  ├── SIGTERM running agent
  ├── mark stage_run failed (output.error = 'restarting after permission grant')
  ├── build handoffNote:
  │     "[PERMISSION GRANTED] You requested permission for \"<tool>\" (<pattern>).
  │      It has been granted. Resume exactly where you left off."
  │
  └── progressTask(taskId, {
        resumeSessionId: run.session_id ?? undefined,
        userAdditionalPrompt: handoffNote
      })
        └── createAgentStage
              ├── spawns: claude --resume <sessionId>   (if session_id present)
              ├── writes: new settings.json with granted tool in allow-list
              └── appends: userAdditionalPrompt to user prompt
```

### Changes Required

**`server/routes/taskRoutes.ts` — permission-grant path (~line 714)**

This is the only file that needs to change. Before calling `progressTask`:

1. Capture `run.session_id` (already on the `run` object loaded earlier in the handler)
2. Look up the resolved permission request to get `tool` and `pattern` (already fetched as part of `resolvePermissionRequest`)
3. Build `handoffNote` string
4. Pass `resumeSessionId` (or `undefined` if null) and `userAdditionalPrompt` to `progressTask`

No other files change.

### Fallback Behaviour

| Condition | `--resume` | Handoff note | Result |
|---|---|---|---|
| `session_id` present | ✓ | ✓ | Full context + briefing |
| `session_id` null | ✗ | ✓ | Fresh start + briefing |
| Deny outcome | ✗ | ✗ | Unchanged (clean restart) |

If `--resume` points to a session not found on disk, Claude CLI falls back to a fresh conversation automatically — same result as the `session_id` null case.

### Handoff Note Format

```
[PERMISSION GRANTED] You requested permission for "<tool>" (<pattern>). It has been granted. Resume exactly where you left off.
```

Kept intentionally short. The full conversation transcript (loaded via `--resume`) provides all prior context; the note only needs to explain the interruption.

## What Is Not Changing

- DB schema: no new columns needed
- `priorIterationOutput` / `buildFeedbackPrefix`: untouched (schema-validation retries only)
- Dormant `on_hold` transition path (path B in orchestrator)
- User-driven "Resume Stage" button flow
- Deny outcome handling

## Infrastructure Already in Place

| Mechanism | File | Relevant Lines |
|---|---|---|
| `--resume` flag in spawner | `server/pipeline/agentSpawner.ts` | 88-103 |
| `resumeSessionId` in `StageContext` | `server/pipeline/types.ts` | 42-46 |
| `userAdditionalPrompt` in `StageContext` | `server/pipeline/types.ts` | 42-46 |
| `buildContext` threads both through | `server/pipeline/orchestrator.ts` | 243-278 |
| `createAgentStage` consumes both | `server/pipeline/stageHandlers.ts` | 76-100 |
| Permission-grant path to change | `server/routes/taskRoutes.ts` | 682-752 |
| `session_id` stored on stage_run | `server/db/schema.sql` | 51-77 |
