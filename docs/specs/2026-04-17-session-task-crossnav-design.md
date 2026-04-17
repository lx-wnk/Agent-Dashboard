# Session ↔ Task Cross-Navigation & Live Output in Task Modal

**Date:** 2026-04-17
**Branch:** feat/mcp-controlling (or new feature branch)

## Summary

Two related features:
1. **Cross-navigation**: Jump from a Session modal (AgentModal) to the linked Kanban Task modal, and vice versa — with a single click.
2. **Live output + prompts in Task modal**: The "Session" tab in TaskModal gains a fully functional `PromptInput`, matching the AgentModal experience.

## Decisions

| Decision | Choice |
|---|---|
| Navigation behavior | Close current modal, open target (no stacking) |
| Live output scope | Only when task is actively running (live agent present) |
| Navigation UI | Info-Banner (Option C): blue banner below titlebar |

---

## Design

### 1. Backend — Enrich `Agent` with `pipelineTaskId`

**File:** `server/agentMerger.ts`

When merging PID data with session data, perform an additional DB lookup:
```
findStageRunBySessionId(sessionId) → { task_id } | null
```

The result is attached as `pipelineTaskId?: number` and `pipelineTaskTitle?: string` (from `getTaskById(task_id).title`) to the merged `Agent` object. `agentMerger.ts` can import from `server/db/` directly — no layering violation.

**File:** `src/types.ts`

Add `pipelineTaskId?: number` and `pipelineTaskTitle?: string` to the `Agent` interface.

**Data flow:** The field propagates automatically through the existing SSE stream (`/api/agents/stream`) — no new endpoint required.

**DB dependency:** `server/db/stageRunsRepo.ts` already exposes `findStageRunBySessionId`. No schema changes needed.

---

### 2. Frontend — Cross-Navigation in `App.vue`

Add a `navigateTo` handler in `App.vue`:

```ts
function navigateTo(target: { agent?: Agent; taskId?: number }) {
  selectedAgent.value = null
  selectedTask.value = null
  nextTick(() => {
    if (target.agent) selectedAgent.value = target.agent
    if (target.taskId) selectedTask.value = tasks.value.find(t => t.id === target.taskId) ?? null
  })
}
```

- `AgentModal` gets a new emit `navigate(taskId: number)` → calls `navigateTo({ taskId })`
- `TaskModal` gets a new emit `navigate(agent: Agent)` → calls `navigateTo({ agent })`
- The modals themselves remain unaware of each other

**Task data source:** `tasks` is already available in `App.vue` via `useTasks()`.

---

### 3. UI — Info-Banner in Both Modals

**In `AgentModal.vue`** — shown when `agent.pipelineTaskId` is set:

```html
<div v-if="agent.pipelineTaskId" class="task-link-banner">
  ⬡ Teil von Task #{{ agent.pipelineTaskId }}
  <span v-if="agent.pipelineTaskTitle"> – {{ agent.pipelineTaskTitle }}</span>
  <button @click="emit('navigate', agent.pipelineTaskId)">öffnen →</button>
</div>
```

`pipelineTaskTitle` comes directly from the enriched `Agent` object — no prop-drilling or composable needed.

**In `TaskModal.vue`** — shown when `pipelineAgent` (live agent) is found:

```html
<div v-if="pipelineAgent" class="task-link-banner">
  ⬡ Läuft als Session in {{ pipelineAgent.projectName }}
  <button @click="emit('navigate', pipelineAgent)">öffnen →</button>
</div>
```

**Styling:** Both banners share the same `.task-link-banner` CSS class (scoped per component or extracted to a shared style). Blue background (`#1e3a5f`), blue border bottom, `#93c5fd` text, `#60a5fa` link. Placed immediately after the modal's titlebar / tab bar.

**No new component** — inline `<div v-if>` in each modal template.

---

### 4. Live Output + Prompt in TaskModal

**File:** `src/components/TaskModal.vue`

The "Session" tab already renders `AgentChatStream` when `pipelineAgent` is set. Add `PromptInput` below it, with the same `localMessages` pattern used in `AgentModal`:

```ts
const localMessages = ref<OutputMessage[]>([])
function onMessageSent(msg: OutputMessage) {
  localMessages.value.push(msg)
  nextTick(() => chatStreamRef.value?.scrollToBottom())
}
// Reset on agent change
watch(() => pipelineAgent.value?.sessionId, () => { localMessages.value = [] })
```

`PromptInput` is rendered only when `pipelineAgent` is present (same condition as `AgentChatStream`). The `variant="full"` prop matches the AgentModal style.

---

## Affected Files

| File | Change |
|---|---|
| `src/types.ts` | Add `pipelineTaskId?: number` and `pipelineTaskTitle?: string` to `Agent` |
| `server/agentMerger.ts` | Lookup `stageRunBySessionId` + `getTaskById`, attach both fields |
| `src/components/AgentModal.vue` | Add info-banner, new `navigate` emit |
| `src/components/TaskModal.vue` | Add info-banner, `PromptInput` + `localMessages` in Session tab, new `navigate` emit |
| `src/App.vue` | Add `navigateTo` handler, wire new emits from both modals |

## Out of Scope

- Navigation when task is not found (graceful no-op: banner simply doesn't render)
- Historical session output for completed tasks (only live output when agent is running)
- Multiple sessions per task (use `activeSessionId` / latest stage run — existing logic)
