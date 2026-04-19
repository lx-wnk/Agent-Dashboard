# Session ↔ Task Cross-Navigation & Live Output Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a blue info-banner to AgentModal and TaskModal that lets users jump between a linked session and its pipeline task with one click, and make the TaskModal's Session tab auto-focus when a live agent starts.

**Architecture:** Backend enriches each `Agent` with `pipelineTaskId`/`pipelineTaskTitle` via a synchronous SQLite lookup in `agentMerger.ts`. Frontend adds a consistent banner + navigate emit to both modals; `App.vue` owns the cross-modal transition logic.

**Tech Stack:** TypeScript, Vue 3 Composition API, better-sqlite3 (sync), Vitest

---

## File Map

| File | Change |
|---|---|
| `src/types.ts` | Add `pipelineTaskId?: string` and `pipelineTaskTitle?: string` to `Agent` |
| `server/agentMerger.ts` | Add `enrichWithPipelineTask()` + call it in `getAgents()` |
| `server/agentMerger.test.ts` | Tests for `enrichWithPipelineTask()` |
| `src/components/AgentModal.vue` | Add info-banner + `navigate` emit |
| `src/components/TaskModal.vue` | Add info-banner + `navigate` emit + auto-switch to session tab |
| `src/App.vue` | Add `navigateTo()` + wire `@navigate` on both modals |

---

## Task 1: Extend Agent type and enrich in backend

**Files:**
- Modify: `src/types.ts`
- Modify: `server/agentMerger.ts`
- Modify: `server/agentMerger.test.ts`

- [ ] **Step 1: Write the failing test**

Add to `server/agentMerger.test.ts` after the existing imports and before the `describe` block:

```ts
import type { Agent } from '../src/types'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { calculateStatus, enrichWithPipelineTask } from './agentMerger'
import { findStageRunBySessionId } from './db/stageRunsRepo.js'
import { getTaskById } from './db/tasksRepo.js'

vi.mock('./db/stageRunsRepo.js', () => ({
  findStageRunBySessionId: vi.fn(),
}))
vi.mock('./db/tasksRepo.js', () => ({
  getTaskById: vi.fn(),
}))
```

Then add a new `describe` block at the end of the file:

```ts
describe('enrichWithPipelineTask', () => {
  const mockFindStageRun = vi.mocked(findStageRunBySessionId)
  const mockGetTask = vi.mocked(getTaskById)

  afterEach(() => vi.clearAllMocks())

  function makeAgent(sessionId: string): Agent {
    return {
      pid: 1,
      sessionId,
      projectPath: '/tmp',
      projectName: 'proj',
      cwd: '/tmp',
      entrypoint: 'cli',
      status: 'active',
      uptime: 1,
      lastActivity: new Date().toISOString(),
      currentAction: null,
      lastTools: [],
      tasks: [],
      subagents: [],
      tokenUsage: { inputTokens: 0, outputTokens: 0, cacheCreationTokens: 0, cacheReadTokens: 0 },
      costEstimate: 0,
      model: null,
      codeVersion: null,
      conversationTurns: 0,
      toolCounts: {},
      meta: null,
      lastOutput: null,
      lastBtw: null,
      channelAvailable: false,
    }
  }

  it('attaches pipelineTaskId and pipelineTaskTitle when stage run and task exist', () => {
    mockFindStageRun.mockReturnValue({
      id: 'run-1',
      taskId: 'task-abc',
      stage: 'umsetzung',
      sessionId: 'sess-1',
      sessionName: null,
      pid: 1,
      status: 'running',
      startedAt: null,
      endedAt: null,
      iteration: 1,
      output: null,
      tokensUsed: 0,
      costCents: 0,
    })
    mockGetTask.mockReturnValue({
      id: 'task-abc',
      slug: 'my-task',
      title: 'My Task',
      description: null,
      cwd: '/tmp',
      worktreePath: null,
      sourceBranch: null,
      targetBranch: null,
      currentStage: 'umsetzung',
      parentTaskId: null,
      maxIterations: 5,
      tokenBudget: null,
      costBudgetCents: null,
      stageTimeoutSeconds: 3600,
      createdAt: '',
      updatedAt: '',
      metadata: null,
      silverBullet: false,
      priority: 'medium',
    })
    const agents = [makeAgent('sess-1')]
    enrichWithPipelineTask(agents)
    expect(agents[0].pipelineTaskId).toBe('task-abc')
    expect(agents[0].pipelineTaskTitle).toBe('My Task')
  })

  it('leaves fields undefined when no stage run matches', () => {
    mockFindStageRun.mockReturnValue(null)
    const agents = [makeAgent('sess-2')]
    enrichWithPipelineTask(agents)
    expect(agents[0].pipelineTaskId).toBeUndefined()
    expect(agents[0].pipelineTaskTitle).toBeUndefined()
  })

  it('leaves fields undefined when stage run has no matching task', () => {
    mockFindStageRun.mockReturnValue({
      id: 'run-2',
      taskId: 'missing-task',
      stage: 'umsetzung',
      sessionId: 'sess-3',
      sessionName: null,
      pid: null,
      status: 'running',
      startedAt: null,
      endedAt: null,
      iteration: 1,
      output: null,
      tokensUsed: 0,
      costCents: 0,
    })
    mockGetTask.mockReturnValue(null)
    const agents = [makeAgent('sess-3')]
    enrichWithPipelineTask(agents)
    expect(agents[0].pipelineTaskId).toBeUndefined()
    expect(agents[0].pipelineTaskTitle).toBeUndefined()
  })

  it('silently skips an agent when the DB throws', () => {
    mockFindStageRun.mockImplementation(() => { throw new Error('DB not ready') })
    const agents = [makeAgent('sess-4')]
    expect(() => enrichWithPipelineTask(agents)).not.toThrow()
    expect(agents[0].pipelineTaskId).toBeUndefined()
  })
})
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
pnpm test server/agentMerger.test.ts
```

Expected: FAIL — `enrichWithPipelineTask is not a function` and import errors for the mock modules.

- [ ] **Step 3: Add fields to Agent type**

In `src/types.ts`, in the `Agent` interface after `lastBtw`:

```ts
  lastBtw: { message: string, response: string | null } | null
  machine?: string
  /** Set when this agent session is running as part of a pipeline task stage. */
  pipelineTaskId?: string
  pipelineTaskTitle?: string
```

- [ ] **Step 4: Add enrichWithPipelineTask to agentMerger.ts**

Add imports at the top of `server/agentMerger.ts` (after existing imports):

```ts
import { findStageRunBySessionId } from './db/stageRunsRepo.js'
import { getTaskById } from './db/tasksRepo.js'
```

Add the exported function before `getAgents()`:

```ts
export function enrichWithPipelineTask(agents: Agent[]): void {
  for (const agent of agents) {
    try {
      const run = findStageRunBySessionId(agent.sessionId)
      if (!run)
        continue
      const task = getTaskById(run.taskId)
      if (!task)
        continue
      agent.pipelineTaskId = run.taskId
      agent.pipelineTaskTitle = task.title
    }
    catch {
      // DB not yet initialized on first boot — skip silently
    }
  }
}
```

At the end of `getAgents()`, before the `return agents` line:

```ts
enrichWithPipelineTask(agents)

return agents
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
pnpm test server/agentMerger.test.ts
```

Expected: all tests PASS.

- [ ] **Step 6: Run typecheck**

```bash
pnpm typecheck
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add src/types.ts server/agentMerger.ts server/agentMerger.test.ts
git commit -m "feat: enrich Agent with pipelineTaskId/pipelineTaskTitle from stage_runs"
```

---

## Task 2: Cross-navigation handler in App.vue

**Files:**
- Modify: `src/App.vue`

- [ ] **Step 1: Add nextTick import**

In `src/App.vue`, change:

```ts
import { computed, ref } from 'vue'
```

to:

```ts
import { computed, nextTick, ref } from 'vue'
```

- [ ] **Step 2: Add navigateTo function**

In `src/App.vue`, after the `function copyScript()` block, add:

```ts
function navigateTo(target: { agent?: import('./types').Agent, taskId?: string }) {
  selectAgent(null)
  selectTask(null)
  nextTick(() => {
    if (target.agent)
      selectAgent(target.agent)
    if (target.taskId) {
      const t = tasks.value.find(t => t.id === target.taskId)
      if (t)
        selectTask(t)
    }
  })
}
```

- [ ] **Step 3: Wire navigate emits on both modals**

In `src/App.vue`, replace:

```html
    <AgentModal
      :agent="selectedAgent"
      @close="selectAgent(null)"
    />
    <TaskModal
      :task="selectedTask"
      @close="selectTask(null)"
    />
```

with:

```html
    <AgentModal
      :agent="selectedAgent"
      @close="selectAgent(null)"
      @navigate="(taskId) => navigateTo({ taskId })"
    />
    <TaskModal
      :task="selectedTask"
      @close="selectTask(null)"
      @navigate="(agent) => navigateTo({ agent })"
    />
```

- [ ] **Step 4: Run typecheck**

```bash
pnpm typecheck
```

Expected: errors on AgentModal and TaskModal (emits not yet defined) — that's fine, Task 3 and 4 fix them.

- [ ] **Step 5: Commit**

```bash
git add src/App.vue
git commit -m "feat: add navigateTo for cross-modal session↔task navigation"
```

---

## Task 3: Info-banner in AgentModal

**Files:**
- Modify: `src/components/AgentModal.vue`

- [ ] **Step 1: Add navigate emit**

In `src/components/AgentModal.vue`, change:

```ts
const emit = defineEmits<{ close: [] }>()
```

to:

```ts
const emit = defineEmits<{ close: [], navigate: [taskId: string] }>()
```

- [ ] **Step 2: Add the banner template**

In `src/components/AgentModal.vue`, in the template, after the closing `</div>` of `.modal-titlebar` and before `<AgentChatStream`, insert:

```html
        <div v-if="agent.pipelineTaskId" class="task-link-banner">
          <span class="task-link-text">
            ⬡ Teil von
            <strong>{{ agent.pipelineTaskTitle ?? `Task ${agent.pipelineTaskId.slice(0, 8)}` }}</strong>
          </span>
          <button class="task-link-btn" @click="emit('navigate', agent.pipelineTaskId)">
            öffnen →
          </button>
        </div>
```

- [ ] **Step 3: Add banner CSS**

In `src/components/AgentModal.vue`, in `<style scoped>`, add after `.modal-close:hover`:

```css
.task-link-banner {
  background: #1e3a5f;
  border-bottom: 1px solid #1e4080;
  padding: 5px 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-shrink: 0;
}
.task-link-text {
  font-size: 11px;
  color: #93c5fd;
}
.task-link-text strong { color: #60a5fa; }
.task-link-btn {
  background: none;
  border: none;
  color: #60a5fa;
  font-size: 11px;
  cursor: pointer;
  text-decoration: underline;
  padding: 0;
  white-space: nowrap;
}
.task-link-btn:hover { color: #93c5fd; }
```

- [ ] **Step 4: Run typecheck**

```bash
pnpm typecheck
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add src/components/AgentModal.vue
git commit -m "feat: add task link banner to AgentModal"
```

---

## Task 4: Info-banner + auto-tab-switch in TaskModal

**Files:**
- Modify: `src/components/TaskModal.vue`

- [ ] **Step 1: Add navigate emit and Agent type import**

In `src/components/TaskModal.vue`, change:

```ts
import type { OutputMessage, PermissionRequest, PipelineTask, StageRun, TaskFeedback, TaskPermission } from '../types'
```

to:

```ts
import type { Agent, OutputMessage, PermissionRequest, PipelineTask, StageRun, TaskFeedback, TaskPermission } from '../types'
```

Change:

```ts
const emit = defineEmits<{ close: [] }>()
```

to:

```ts
const emit = defineEmits<{ close: [], navigate: [agent: Agent] }>()
```

- [ ] **Step 2: Add auto-tab-switch watcher**

In `src/components/TaskModal.vue`, after the existing `watch(() => props.task?.id, ...)` block, add:

```ts
// Auto-switch to session tab when a live agent appears for this task.
// Only fires once per task open (guarded by the activeTab reset above).
watch(pipelineAgent, (agent, prev) => {
  if (agent && !prev && activeTab.value === 'overview')
    activeTab.value = 'session'
})
```

- [ ] **Step 3: Add the banner template**

In `src/components/TaskModal.vue`, find the session tab section (starts with `<section v-if="activeTab === 'session'"`). Add the banner as the first child inside `<template v-else>`, before the existing `<div class="session-header">`:

```html
              <div v-if="pipelineAgent" class="task-link-banner">
                <span class="task-link-text">
                  ⬡ Läuft als Session in
                  <strong>{{ pipelineAgent.projectName }}</strong>
                </span>
                <button class="task-link-btn" @click="emit('navigate', pipelineAgent)">
                  Session öffnen →
                </button>
              </div>
```

The `<template v-else>` block now looks like:

```html
            <template v-else>
              <div v-if="pipelineAgent" class="task-link-banner">
                <span class="task-link-text">
                  ⬡ Läuft als Session in
                  <strong>{{ pipelineAgent.projectName }}</strong>
                </span>
                <button class="task-link-btn" @click="emit('navigate', pipelineAgent)">
                  Session öffnen →
                </button>
              </div>
              <div class="session-header">
                <!-- ... existing content unchanged ... -->
```

Note: the `<div class="task-link-banner">` inside `<template v-else>` is only rendered when `task.activeSessionId` is set, which is the correct guard — same condition as the rest of the template block.

- [ ] **Step 4: Add banner CSS**

In `src/components/TaskModal.vue`, in `<style scoped>`, add after `.session-stream`:

```css
.task-link-banner {
  background: #1e3a5f;
  border-bottom: 1px solid #1e4080;
  padding: 5px 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-shrink: 0;
}
.task-link-text {
  font-size: 11px;
  color: #93c5fd;
}
.task-link-text strong { color: #60a5fa; }
.task-link-btn {
  background: none;
  border: none;
  color: #60a5fa;
  font-size: 11px;
  cursor: pointer;
  text-decoration: underline;
  padding: 0;
  white-space: nowrap;
}
.task-link-btn:hover { color: #93c5fd; }
```

- [ ] **Step 5: Run typecheck**

```bash
pnpm typecheck
```

Expected: no errors.

- [ ] **Step 6: Run all tests**

```bash
pnpm test
```

Expected: all tests PASS.

- [ ] **Step 7: Commit**

```bash
git add src/components/TaskModal.vue
git commit -m "feat: add session link banner and auto-tab-switch to TaskModal"
```

---

## Task 5: Manual smoke test

- [ ] Start the dev server: `pnpm dev`
- [ ] Open a running pipeline task with an active stage run (status `running`)
- [ ] Verify: TaskModal auto-switches to "Session" tab
- [ ] Verify: blue banner appears in Session tab with "Session öffnen →" button
- [ ] Click "Session öffnen →" — TaskModal closes, AgentModal opens for that session
- [ ] Verify: AgentModal shows blue banner "Teil von Task #..." with "öffnen →" button
- [ ] Click "öffnen →" in AgentModal — AgentModal closes, TaskModal opens for the linked task
- [ ] Verify: no JS errors in browser console
