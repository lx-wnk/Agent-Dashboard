# Agent-Based Ticket Refinement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the static BacklogForm with an interactive Claude-powered chat (`konzept` stage) that guides users through Analysis → Spec → Umsetzungskonzept → Approval before a task enters the execution pipeline.

**Architecture:** A `konzept` pipeline stage hosts the refinement chat. Each user turn spawns a short-lived `claude -p` process with the full conversation history serialised in the prompt (no extra API key — uses the existing Claude subscription). The confirmed spec+plan is stored in `task.metadata` and the task advances to `backlog` ("Ready for Doing"). The old autonomous stages `pruefung`, `refinement`, `planning`, `approval1`, `umsetzungskonzept`, `approval2` are fully removed.

**Tech Stack:** TypeScript, Express SSE, better-sqlite3, Vue 3 Composition API, existing `claude` CLI spawn pattern.

---

## File Map

**Modified:**
- `src/types.ts` — PipelineStage union
- `server/constants.ts` — VALID_STAGES
- `server/pipeline/types.ts` — STAGE_ORDER
- `server/pipeline/stageHandlers.ts` — remove 6 handlers, add konzeptHandler, update backlog/umsetzung
- `server/pipeline/stagePrompts.ts` — remove 4 prompts
- `server/pipeline/completionDetector.ts` — remove old stage cases
- `server/pipeline/orchestrator.ts` — remove approval/planning/umsetzungskonzept special cases
- `server/db/tasksRepo.ts` — exclude konzept from listPickableTasks
- `server/db/schema.sql` — add refinement_turns table
- `server/routes/taskRoutes.ts` — remove approval routes, update enrichTask
- `server/services/approvalUtils.ts` — read toolRequests from task.metadata
- `server/index.ts` — wire refineRoutes
- `src/components/PipelineBoard.vue` — new column definitions
- `src/App.vue` — swap BacklogForm → RefinementChat

**Created:**
- `server/db/refinementTurnsRepo.ts` — CRUD for refinement_turns
- `server/pipeline/refinementSpawner.ts` — thin claude -p spawn + system prompt
- `server/routes/refineRoutes.ts` — POST /turn (SSE), POST /confirm
- `src/composables/useRefinementChat.ts` — chat state & SSE wiring
- `src/components/RefinementChat.vue` — chat modal UI

**Deleted:**
- `src/components/BacklogForm.vue`

---

## Task 1: Types & Constants — Remove old stages, add konzept

**Files:**
- Modify: `src/types.ts`
- Modify: `server/constants.ts`
- Modify: `server/pipeline/types.ts`

- [ ] **Step 1: Update PipelineStage in src/types.ts**

Replace the `PipelineStage` union (currently 13 members) with the trimmed version:

```typescript
export type PipelineStage
  = | 'konzept'
    | 'backlog'
    | 'umsetzung'
    | 'selbstreview'
    | 'finalisierung'
    | 'done'
    | 'on_hold'
    | 'cancelled'
```

- [ ] **Step 2: Update VALID_STAGES in server/constants.ts**

```typescript
export const VALID_STAGES = new Set<PipelineStage>([
  'konzept',
  'backlog',
  'umsetzung',
  'selbstreview',
  'finalisierung',
  'done',
  'on_hold',
  'cancelled',
])
```

- [ ] **Step 3: Update STAGE_ORDER in server/pipeline/types.ts**

```typescript
export const STAGE_ORDER: PipelineStage[] = [
  'konzept',
  'backlog',
  'umsetzung',
  'selbstreview',
  'finalisierung',
  'done',
]
```

- [ ] **Step 4: Run typecheck and fix any cascading type errors**

```bash
pnpm typecheck 2>&1 | head -60
```

Expected: errors from files that still reference removed stages — these are fixed in later tasks.

- [ ] **Step 5: Commit**

```bash
git add src/types.ts server/constants.ts server/pipeline/types.ts
git commit -m "refactor: remove old pipeline stages, add konzept to type system"
```

---

## Task 2: Remove old stage handlers and prompts

**Files:**
- Modify: `server/pipeline/stageHandlers.ts`
- Modify: `server/pipeline/stagePrompts.ts`

- [ ] **Step 1: Remove old prompt functions from stagePrompts.ts**

Delete the following exported functions entirely:
- `pruefungPrompt`
- `refinementPrompt`
- `planningPrompt`
- `umsetzungskonzeptPrompt`

Keep: `umsetzungPrompt`, `selbstreviewPrompt`, `finalisierungPrompt`, `buildUserFeedbackPrefix`, `SHARED_CONTEXT`.

Also remove unused imports (`TaskFeedback` if only used by removed prompts — check first).

- [ ] **Step 2: Rewrite stageHandlers.ts**

Replace the full file content. Key changes:
- Remove imports for `pruefungPrompt`, `refinementPrompt`, `planningPrompt`, `umsetzungskonzeptPrompt`
- Remove handlers: `pruefungHandler`, `refinementHandler`, `planningHandler`, `approval1Handler`, `umsetzungskonzeptHandler`, `approval2Handler`, `createPruefungHandler`
- Add `konzeptHandler`
- Update `backlogHandler` to go to `umsetzung`
- Update `umsetzungBuilder` to read from `task.metadata`
- Update `handlersByStage`

```typescript
// konzeptHandler — agent-less; orchestrator never picks up konzept tasks
// (filtered in listPickableTasks), so this handler is only a safety net.
export const konzeptHandler: StageHandler = {
  stage: 'konzept',
  requiresAgent: false,
  async execute(ctx: StageContext): Promise<StageTransition> {
    ctx.recordAudit('konzept_chat_pending')
    return { kind: 'wait_user', reason: 'Refinement chat in progress' }
  },
}

// backlogHandler — "Ready for Doing": immediately proceeds to umsetzung.
export const backlogHandler: StageHandler = {
  stage: 'backlog',
  requiresAgent: false,
  async execute(ctx: StageContext): Promise<StageTransition> {
    ctx.recordAudit('backlog_entered')
    return { kind: 'next', toStage: 'umsetzung' }
  },
}

// umsetzungBuilder now reads konzept output from task.metadata
// (populated by POST /api/refine/:taskId/confirm).
const umsetzungBuilder: PromptBuilder = (ctx) => {
  const feedback = typeof ctx.task.metadata?.review_feedback === 'string'
    ? ctx.task.metadata.review_feedback as string
    : undefined
  const konzeptOutput = (ctx.task.metadata ?? {}) as Record<string, unknown>
  return umsetzungPrompt(ctx.task, konzeptOutput, feedback)
}

export const handlersByStage: Record<string, StageHandler> = {
  konzept: konzeptHandler,
  backlog: backlogHandler,
  umsetzung: umsetzungHandler,
  selbstreview: selbstreviewHandler,
  finalisierung: finalisierungHandler,
}
```

- [ ] **Step 3: Run tests**

```bash
pnpm test 2>&1 | tail -30
```

Expected: stageHandlers tests that reference old handlers will fail — delete those test cases (they test deleted code). Fix remaining failures.

- [ ] **Step 4: Commit**

```bash
git add server/pipeline/stageHandlers.ts server/pipeline/stagePrompts.ts
git commit -m "refactor: remove pruefung/refinement/planning/approval stage handlers and prompts"
```

---

## Task 3: Orchestrator, completionDetector, listPickableTasks

**Files:**
- Modify: `server/db/tasksRepo.ts`
- Modify: `server/pipeline/completionDetector.ts`
- Modify: `server/pipeline/orchestrator.ts`

- [ ] **Step 1: Exclude konzept from listPickableTasks in tasksRepo.ts**

Find the `listPickableTasks` function (line ~158). The WHERE clause currently reads:
```sql
WHERE tasks.current_stage NOT IN ('done','cancelled','on_hold','approval1','approval2')
```

Replace with:
```sql
WHERE tasks.current_stage NOT IN ('konzept','done','cancelled','on_hold')
```

`konzept` tasks are only advanced by `POST /api/refine/:taskId/confirm`, never by the orchestrator.

- [ ] **Step 2: Remove old stage cases from completionDetector.ts**

In `completionDetector.ts`, find the `validateStageOutput` function (line ~66). Remove the cases:
- `case 'pruefung':`
- `case 'refinement':`
- `case 'planning':`
- `case 'umsetzungskonzept':`

Keep `umsetzung`, `selbstreview`, `finalisierung`, and the default/backlog cases.

- [ ] **Step 3: Clean up orchestrator.ts**

Find and fix these three locations:

**Location A** (~line 330): Remove the `planning`/`umsetzungskonzept` special case:
```typescript
// REMOVE this block entirely:
if (stageRun.stage === 'planning' || stageRun.stage === 'umsetzungskonzept')
  ...
```

**Location B** (~line 484): Remove the comment referencing `planning→approval1` and any logic that treats approval stages as special in the tick loop.

**Location C** (~line 824): Remove the comment about `approval1/2`. The on_hold → backlog promotion logic stays:
```typescript
// Move on_hold tasks to backlog so they become pickable.
updateTask(dep.taskId, { currentStage: 'backlog' })
```

- [ ] **Step 4: Run tests**

```bash
pnpm test 2>&1 | tail -30
```

Fix any test failures caused by removed stage references.

- [ ] **Step 5: Commit**

```bash
git add server/db/tasksRepo.ts server/pipeline/completionDetector.ts server/pipeline/orchestrator.ts
git commit -m "refactor: exclude konzept from orchestrator, remove old stage logic"
```

---

## Task 4: Clean up taskRoutes and enrichTask

**Files:**
- Modify: `server/routes/taskRoutes.ts`

- [ ] **Step 1: Update USER_WAIT_STAGES**

Line ~73, change:
```typescript
const USER_WAIT_STAGES = new Set<PipelineStage>(['on_hold', 'approval1', 'approval2'])
```
to:
```typescript
const USER_WAIT_STAGES = new Set<PipelineStage>(['on_hold'])
```

- [ ] **Step 2: Fix enrichTask — remove syntheticGateStatus for approval1/2**

Find the `enrichTask` function. Remove the `syntheticGateStatus` logic that checks for `approval1`/`approval2`:

```typescript
// REMOVE this block:
const syntheticGateStatus: StageRunStatus | null
  = (task.currentStage === 'approval1' || task.currentStage === 'approval2')
    ? 'awaiting_user'
    : null
```

Replace with just `null` or remove the variable entirely and update its usage below.

- [ ] **Step 3: Remove the /tasks/:id/approve route**

Delete the entire `mutationRouter.post('/tasks/:id/approve', ...)` handler — this was the approval1/approval2 gate. The `konzept → backlog` transition is handled by `POST /api/refine/:taskId/confirm` instead.

- [ ] **Step 4: Remove the /tasks/:id/request-changes route**

Delete the entire `mutationRouter.post('/tasks/:id/request-changes', ...)` handler — it only served planning/umsetzungskonzept iterations, which no longer exist.

- [ ] **Step 5: Remove bulkGrantKonzeptPermissions import from taskRoutes.ts**

It is no longer called from taskRoutes — it moves to refineRoutes.

```typescript
// REMOVE this import line:
import { bulkGrantKonzeptPermissions } from '../services/approvalUtils.js'
```

- [ ] **Step 6: Run typecheck**

```bash
pnpm typecheck 2>&1 | head -30
```

Expected: clean or only refine-route errors (not yet created).

- [ ] **Step 7: Commit**

```bash
git add server/routes/taskRoutes.ts
git commit -m "refactor: remove approval gate routes, update enrichTask for new pipeline"
```

---

## Task 5: Update approvalUtils to read toolRequests from task.metadata

**Files:**
- Modify: `server/services/approvalUtils.ts`

- [ ] **Step 1: Write the failing test**

In `server/services/approvalUtils.test.ts` (create if it doesn't exist):

```typescript
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { bulkGrantKonzeptPermissions } from './approvalUtils'

vi.mock('../db/tasksRepo', () => ({
  getTaskById: vi.fn(),
}))
vi.mock('../db/permissionsRepo', () => ({
  listTaskPermissions: vi.fn(() => []),
  createTaskPermission: vi.fn(),
}))
vi.mock('../db/auditRepo', () => ({ appendAudit: vi.fn() }))

import { getTaskById } from '../db/tasksRepo'

describe('bulkGrantKonzeptPermissions', () => {
  it('reads toolRequests from task.metadata', () => {
    vi.mocked(getTaskById).mockReturnValue({
      id: 'task-1',
      metadata: {
        toolRequests: [{ tool: 'Read', pattern: null, reason: 'read files' }],
      },
    } as any)

    const { createTaskPermission } = await import('../db/permissionsRepo')
    bulkGrantKonzeptPermissions('task-1')

    expect(vi.mocked(createTaskPermission)).toHaveBeenCalledWith(
      expect.objectContaining({ tool: 'Read', granted: true }),
    )
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
pnpm test server/services/approvalUtils 2>&1 | tail -20
```

Expected: FAIL — `bulkGrantKonzeptPermissions` still reads from stage run, not metadata.

- [ ] **Step 3: Update bulkGrantKonzeptPermissions**

Replace the function body in `server/services/approvalUtils.ts`:

```typescript
import { getTaskById } from '../db/tasksRepo.js'

export function bulkGrantKonzeptPermissions(taskId: string): void {
  const task = getTaskById(taskId)
  const rawRequests = (task?.metadata as Record<string, unknown> | null)?.toolRequests
  if (!Array.isArray(rawRequests))
    return

  const existing = listTaskPermissions(taskId)
  for (const req of rawRequests) {
    if (typeof req !== 'object' || req === null)
      continue
    const r = req as Record<string, unknown>
    const tool = typeof r.tool === 'string' ? r.tool.trim() : null
    const pattern = typeof r.pattern === 'string' && r.pattern.trim() ? r.pattern.trim() : null
    if (!tool || !ALLOWED_TOOLS.has(tool))
      continue
    const alreadyGranted = existing.some(p => p.tool === tool && (p.pattern ?? null) === pattern && p.granted)
    if (alreadyGranted)
      continue
    createTaskPermission({ taskId, tool, pattern, granted: true, preApproved: true, decidedBy: 'user' })
  }

  appendAudit({
    taskId,
    actor: 'user',
    action: 'bulk_granted_tool_permissions',
    details: { source: 'konzept_metadata_toolRequests', count: rawRequests.length },
  })
}
```

Remove the `getLatestStageRun` import (no longer needed).

- [ ] **Step 4: Run tests**

```bash
pnpm test server/services/approvalUtils 2>&1 | tail -10
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/services/approvalUtils.ts server/services/approvalUtils.test.ts
git commit -m "feat: bulkGrantKonzeptPermissions reads toolRequests from task.metadata"
```

---

## Task 6: DB — refinement_turns table and repo

**Files:**
- Modify: `server/db/schema.sql`
- Create: `server/db/refinementTurnsRepo.ts`

- [ ] **Step 1: Add refinement_turns to schema.sql**

Append to the end of `server/db/schema.sql`:

```sql
-- Conversation turns for the konzept-stage refinement chat.
-- Each user/assistant exchange is stored as one row per turn.
CREATE TABLE IF NOT EXISTS refinement_turns (
  id          TEXT PRIMARY KEY,
  task_id     TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  role        TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
  content     TEXT NOT NULL,
  phase       TEXT,
  created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_refinement_turns_task
  ON refinement_turns(task_id, created_at);
```

`CREATE TABLE IF NOT EXISTS` is idempotent — no separate ALTER migration needed.

- [ ] **Step 2: Write the failing test for refinementTurnsRepo**

Create `server/db/refinementTurnsRepo.test.ts`:

```typescript
import { describe, it, expect, beforeEach } from 'vitest'
import { resetDb } from './client'
import { createTurn, listTurns } from './refinementTurnsRepo'

beforeEach(() => resetDb())

describe('refinementTurnsRepo', () => {
  it('createTurn and listTurns round-trip', () => {
    createTurn({ taskId: 'task-1', role: 'user', content: 'Hello', phase: null })
    createTurn({ taskId: 'task-1', role: 'assistant', content: 'Hi', phase: 'analyse' })

    const turns = listTurns('task-1')
    expect(turns).toHaveLength(2)
    expect(turns[0].role).toBe('user')
    expect(turns[1].phase).toBe('analyse')
  })

  it('listTurns returns empty array for unknown task', () => {
    expect(listTurns('unknown')).toEqual([])
  })
})
```

- [ ] **Step 3: Run test to verify it fails**

```bash
pnpm test server/db/refinementTurnsRepo 2>&1 | tail -10
```

Expected: FAIL — module not found.

- [ ] **Step 4: Create server/db/refinementTurnsRepo.ts**

```typescript
import { nanoid } from 'nanoid'
import { getDb } from './client.js'

export interface RefinementTurn {
  id: string
  taskId: string
  role: 'user' | 'assistant'
  content: string
  phase: string | null
  createdAt: string
}

export function createTurn(input: Omit<RefinementTurn, 'id' | 'createdAt'>): RefinementTurn {
  const db = getDb()
  const id = nanoid()
  const createdAt = new Date().toISOString()
  db.prepare(`
    INSERT INTO refinement_turns (id, task_id, role, content, phase, created_at)
    VALUES (@id, @task_id, @role, @content, @phase, @created_at)
  `).run({
    id,
    task_id: input.taskId,
    role: input.role,
    content: input.content,
    phase: input.phase ?? null,
    created_at: createdAt,
  })
  return { ...input, id, createdAt }
}

export function listTurns(taskId: string): RefinementTurn[] {
  const rows = getDb().prepare(`
    SELECT id, task_id, role, content, phase, created_at
    FROM refinement_turns
    WHERE task_id = ?
    ORDER BY created_at ASC
  `).all(taskId) as Array<{
    id: string
    task_id: string
    role: string
    content: string
    phase: string | null
    created_at: string
  }>
  return rows.map(r => ({
    id: r.id,
    taskId: r.task_id,
    role: r.role as 'user' | 'assistant',
    content: r.content,
    phase: r.phase,
    createdAt: r.created_at,
  }))
}
```

- [ ] **Step 5: Run tests**

```bash
pnpm test server/db/refinementTurnsRepo 2>&1 | tail -10
```

Expected: PASS. Note: `resetDb()` requires there to be an existing task with id `task-1` for the FK constraint — either mock the FK or use `PRAGMA foreign_keys = OFF` in the test setup, or insert a tasks row first in `beforeEach`.

If FK fails, update the test's `beforeEach`:
```typescript
import { getDb } from './client'
beforeEach(() => {
  resetDb()
  getDb().prepare(`INSERT INTO tasks (id, slug, title, cwd, current_stage, max_iterations, stage_timeout_seconds, silver_bullet, priority, created_at, updated_at) VALUES ('task-1','t','T','/tmp','konzept',20,1800,0,'medium','2026-01-01','2026-01-01')`).run()
})
```

- [ ] **Step 6: Commit**

```bash
git add server/db/schema.sql server/db/refinementTurnsRepo.ts server/db/refinementTurnsRepo.test.ts
git commit -m "feat: add refinement_turns table and repo"
```

---

## Task 7: refinementSpawner — thin claude -p spawner

**Files:**
- Create: `server/pipeline/refinementSpawner.ts`

- [ ] **Step 1: Write failing tests**

Create `server/pipeline/refinementSpawner.test.ts`:

```typescript
import { describe, it, expect } from 'vitest'
import { serializeHistory, REFINEMENT_SYSTEM_PROMPT } from './refinementSpawner'
import type { RefinementTurn } from '../db/refinementTurnsRepo'

describe('serializeHistory', () => {
  it('returns empty string for no history', () => {
    expect(serializeHistory([])).toBe('')
  })

  it('serializes turns with correct prefixes', () => {
    const turns: RefinementTurn[] = [
      { id: '1', taskId: 't', role: 'user', content: 'hello', phase: null, createdAt: '' },
      { id: '2', taskId: 't', role: 'assistant', content: 'hi there', phase: null, createdAt: '' },
    ]
    const result = serializeHistory(turns)
    expect(result).toContain('Human: hello')
    expect(result).toContain('Assistant: hi there')
  })
})

describe('REFINEMENT_SYSTEM_PROMPT', () => {
  it('mentions all four phases', () => {
    expect(REFINEMENT_SYSTEM_PROMPT).toContain('ANALYSE')
    expect(REFINEMENT_SYSTEM_PROMPT).toContain('SPEC')
    expect(REFINEMENT_SYSTEM_PROMPT).toContain('UMSETZUNGSKONZEPT')
    expect(REFINEMENT_SYSTEM_PROMPT).toContain('APPROVAL')
  })

  it('includes __phase_done signal instructions', () => {
    expect(REFINEMENT_SYSTEM_PROMPT).toContain('__phase_done:')
  })
})
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
pnpm test server/pipeline/refinementSpawner 2>&1 | tail -10
```

Expected: FAIL — module not found.

- [ ] **Step 3: Create server/pipeline/refinementSpawner.ts**

```typescript
import type { RefinementTurn } from '../db/refinementTurnsRepo.js'
import type { Readable } from 'node:stream'
import { spawn } from 'node:child_process'

export const REFINEMENT_SYSTEM_PROMPT = `You are a ticket refinement assistant that helps software teams create well-defined tasks through structured dialogue. Work through exactly four phases in strict order. Never skip phases.

**Phase 1: ANALYSE**
Ask for: working directory (cwd), source branch, target branch, problem description, complexity estimate. Ask ONE question at a time. When you have all required information, end your message with exactly: __phase_done: analyse

**Phase 2: SPEC**
Write a refined title, description, success criteria (bullet list), assumptions, out-of-scope. Present it and accept feedback. When the spec is accepted by the user, end with: __phase_done: spec

**Phase 3: UMSETZUNGSKONZEPT**
Break down the implementation into numbered steps. List all tool permissions needed. For each tool: name, optional glob pattern, reason. Common tools: Read, Write, Edit, Glob, Grep, Bash. Bash always needs a pattern (e.g. "npm run *"). Never include "Bash(git push *)". Present and accept feedback. When accepted, end with: __phase_done: umsetzungskonzept

**Phase 4: APPROVAL**
Summarise the complete spec and plan. Ask the user to confirm. When confirmed, output ONLY this JSON block and nothing after it:
\`\`\`json
{
  "refinedTitle": "...",
  "refinedDescription": "...",
  "successCriteria": ["..."],
  "assumptions": ["..."],
  "outOfScope": ["..."],
  "cwd": "...",
  "sourceBranch": "...",
  "targetBranch": "...",
  "steps": [{"n": 1, "description": "..."}],
  "toolRequests": [{"tool": "...", "pattern": "...", "reason": "..."}]
}
\`\`\`
Then end with: __phase_done: approval`

export function serializeHistory(turns: RefinementTurn[]): string {
  if (turns.length === 0)
    return ''
  const lines = turns.map(t =>
    `${t.role === 'user' ? 'Human' : 'Assistant'}: ${t.content}`,
  )
  return `Previous conversation:\n${lines.join('\n\n')}\n\n`
}

export interface SpawnRefinementResult {
  stdout: Readable
  waitForExit: () => Promise<void>
}

export function spawnRefinementTurn(
  message: string,
  history: RefinementTurn[],
  cwd: string,
): SpawnRefinementResult {
  const historyBlock = serializeHistory(history)
  const fullPrompt = `${historyBlock}Human: ${message}\n\nContinue the conversation as the assistant. Follow the phase instructions exactly.`

  const child = spawn('claude', [
    '-p', fullPrompt,
    '--system-prompt', REFINEMENT_SYSTEM_PROMPT,
    '--permission-mode', 'default',
  ], {
    cwd,
    stdio: ['ignore', 'pipe', 'pipe'],
  })

  child.stderr?.on('data', () => { /* drain to prevent pipe buffer fill */ })
  child.stderr?.on('error', () => { /* swallow EPIPE on exit */ })

  const waitForExit = (): Promise<void> =>
    new Promise((resolve, reject) => {
      child.on('close', code =>
        code === 0 ? resolve() : reject(new Error(`refinement spawn exited ${code}`)),
      )
      child.on('error', reject)
    })

  return { stdout: child.stdout as Readable, waitForExit }
}
```

- [ ] **Step 4: Run tests**

```bash
pnpm test server/pipeline/refinementSpawner 2>&1 | tail -10
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/pipeline/refinementSpawner.ts server/pipeline/refinementSpawner.test.ts
git commit -m "feat: add refinementSpawner — thin claude -p turn spawner"
```

---

## Task 8: refineRoutes — POST /turn (SSE) + POST /confirm

**Files:**
- Create: `server/routes/refineRoutes.ts`

- [ ] **Step 1: Create server/routes/refineRoutes.ts**

```typescript
import { Router } from 'express'
import { getTaskById, updateTask } from '../db/tasksRepo.js'
import { createTurn, listTurns } from '../db/refinementTurnsRepo.js'
import { bulkGrantKonzeptPermissions } from '../services/approvalUtils.js'
import { spawnRefinementTurn } from '../pipeline/refinementSpawner.js'

export function createRefineRouter(
  broadcastEnrichedUpdate: (taskId: string) => void,
): Router {
  const router = Router()

  // Stream a refinement chat turn via SSE.
  // Spawns claude -p with the full history, streams stdout, persists the response.
  router.post('/:taskId/turn', async (req, res) => {
    const task = getTaskById(req.params.taskId)
    if (!task || task.currentStage !== 'konzept') {
      res.status(404).json({ error: 'Task not found or not in konzept stage' })
      return
    }

    const body = req.body as { message?: unknown }
    const message = typeof body.message === 'string' ? body.message.trim() : ''
    if (!message) {
      res.status(400).json({ error: 'message is required' })
      return
    }

    const history = listTurns(req.params.taskId)
    createTurn({ taskId: task.id, role: 'user', content: message, phase: null })

    res.setHeader('Content-Type', 'text/event-stream')
    res.setHeader('Cache-Control', 'no-cache')
    res.setHeader('Connection', 'keep-alive')
    res.flushHeaders()

    const { stdout, waitForExit } = spawnRefinementTurn(message, history, task.cwd)

    let fullResponse = ''
    stdout.on('data', (chunk: Buffer) => {
      const text = chunk.toString()
      fullResponse += text
      res.write(`data: ${JSON.stringify({ text })}\n\n`)
    })

    try {
      await waitForExit()

      // Detect __phase_done signal in the full response after streaming completes.
      const phaseMatch = fullResponse.match(/__phase_done:\s*(\w+)/)
      const detectedPhase = phaseMatch ? phaseMatch[1] : null

      createTurn({ taskId: task.id, role: 'assistant', content: fullResponse, phase: detectedPhase })

      if (detectedPhase) {
        res.write(`event: phase_change\ndata: ${JSON.stringify({ phase: detectedPhase })}\n\n`)
      }
      res.write(`event: done\ndata: {}\n\n`)
    }
    catch (err) {
      createTurn({ taskId: task.id, role: 'assistant', content: fullResponse || '[error]', phase: null })
      res.write(`event: error\ndata: ${JSON.stringify({ error: String(err) })}\n\n`)
    }
    res.end()
  })

  // Finalise the refinement chat: extract JSON from last assistant turn,
  // persist to task.metadata, grant permissions, advance to backlog.
  router.post('/:taskId/confirm', (req, res) => {
    const task = getTaskById(req.params.taskId)
    if (!task || task.currentStage !== 'konzept') {
      res.status(404).json({ error: 'Task not found or not in konzept stage' })
      return
    }

    const turns = listTurns(req.params.taskId)
    const lastAssistant = [...turns].reverse().find(t => t.role === 'assistant')
    if (!lastAssistant) {
      res.status(409).json({ error: 'No assistant message found' })
      return
    }

    const jsonMatch = lastAssistant.content.match(/```json\s*([\s\S]*?)```/)
    if (!jsonMatch) {
      res.status(409).json({ error: 'No JSON block found in last assistant message' })
      return
    }

    let konzeptOutput: Record<string, unknown>
    try {
      konzeptOutput = JSON.parse(jsonMatch[1])
    }
    catch {
      res.status(409).json({ error: 'Invalid JSON in assistant output' })
      return
    }

    updateTask(task.id, {
      title: typeof konzeptOutput.refinedTitle === 'string' ? konzeptOutput.refinedTitle : task.title,
      description: typeof konzeptOutput.refinedDescription === 'string'
        ? konzeptOutput.refinedDescription
        : task.description,
      // Update cwd from what Claude confirmed during analyse — this is the real
      // working directory the umsetzung agent will use.
      ...(typeof konzeptOutput.cwd === 'string' ? { cwd: konzeptOutput.cwd } : {}),
      metadata: { ...(task.metadata ?? {}), ...konzeptOutput },
      currentStage: 'backlog',
    })

    bulkGrantKonzeptPermissions(task.id)
    broadcastEnrichedUpdate(task.id)

    res.json(getTaskById(task.id))
  })

  // Return all turns for a task (used when resuming a konzept chat).
  router.get('/:taskId/turns', (req, res) => {
    const task = getTaskById(req.params.taskId)
    if (!task) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    res.json(listTurns(req.params.taskId))
  })

  return router
}
```

- [ ] **Step 2: Run typecheck**

```bash
pnpm typecheck 2>&1 | grep refineRoutes
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add server/routes/refineRoutes.ts
git commit -m "feat: add refineRoutes — POST /turn SSE + POST /confirm"
```

---

## Task 9: Wire refineRoutes into server/index.ts

**Files:**
- Modify: `server/index.ts`

- [ ] **Step 1: Add import and mount in server/index.ts**

Add the import near the other route imports:

```typescript
import { createRefineRouter } from './routes/refineRoutes.js'
```

Add the router mount after the existing task router (around line 310):

```typescript
app.use('/api/refine', createRefineRouter((taskId) => {
  const task = getTaskById(taskId)
  if (task)
    broadcastTaskEvent({ type: 'task_updated', taskId, payload: enrichTask(task) })
}))
```

- [ ] **Step 2: Run typecheck**

```bash
pnpm typecheck 2>&1 | head -20
```

Expected: clean.

- [ ] **Step 3: Start dev server and test endpoints manually**

```bash
pnpm dev &
# Create a konzept task first via POST /api/tasks
curl -s -X POST http://localhost:13120/api/tasks \
  -H "Content-Type: application/json" \
  -d '{"slug":"test-refinement","title":"Test","cwd":"/tmp","stage":"konzept"}' | jq .id
# Test the turns endpoint
curl -s http://localhost:13120/api/refine/<task-id>/turns | jq .
```

Expected: `[]` (empty turns array).

- [ ] **Step 4: Commit**

```bash
git add server/index.ts
git commit -m "feat: mount refineRoutes at /api/refine"
```

---

## Task 10: PipelineBoard — new column definitions

**Files:**
- Modify: `src/components/PipelineBoard.vue`

- [ ] **Step 1: Replace COLUMNS constant**

Find the `COLUMNS` array (line ~27) and replace with:

```typescript
const COLUMNS: ColumnDef[] = [
  {
    id: 'needs-you',
    label: 'Needs You',
    stages: [],
    group: 'needs-you',
    hint: 'User action required',
  },
  { id: 'konzept', label: 'Konzept', stages: ['konzept'], group: 'active' },
  { id: 'backlog', label: 'Backlog', stages: ['backlog'], group: 'active' },
  {
    id: 'umsetzung',
    label: 'Implementation',
    stages: ['umsetzung', 'selbstreview'],
    group: 'active',
  },
  { id: 'finalisierung', label: 'Completion', stages: ['finalisierung'], group: 'active' },
  { id: 'done', label: 'Done', stages: ['done'], group: 'terminal' },
  { id: 'cancelled', label: 'Cancelled', stages: ['cancelled'], group: 'terminal' },
]
```

- [ ] **Step 2: Start dev server and visually verify the board**

```bash
pnpm dev
```

Open `http://localhost:13120` → Pipeline view. Expected columns: Needs You | Konzept | Backlog | Implementation | Completion | Done | Cancelled.

- [ ] **Step 3: Commit**

```bash
git add src/components/PipelineBoard.vue
git commit -m "feat: update kanban columns — konzept replaces analysis/approval columns"
```

---

## Task 11: useRefinementChat composable + RefinementChat component

**Files:**
- Create: `src/composables/useRefinementChat.ts`
- Create: `src/components/RefinementChat.vue`

- [ ] **Step 1: Create src/composables/useRefinementChat.ts**

```typescript
import type { PipelineTask } from '../types'
import { ref } from 'vue'

export interface ChatMessage {
  role: 'user' | 'assistant'
  content: string
  phase?: string | null
}

export type RefinementPhase = 'analyse' | 'spec' | 'umsetzungskonzept' | 'approval' | null

const PHASE_LABELS: Record<string, string> = {
  analyse: 'Analyse',
  spec: 'Spec',
  umsetzungskonzept: 'Umsetzungskonzept',
  approval: 'Approval',
}

export function useRefinementChat(taskId: () => string | null) {
  const messages = ref<ChatMessage[]>([])
  const currentPhase = ref<RefinementPhase>(null)
  const completedPhases = ref<Set<string>>(new Set())
  const isStreaming = ref(false)
  const error = ref<string | null>(null)
  const approvalReady = ref(false)

  async function loadHistory() {
    const id = taskId()
    if (!id) return
    try {
      const res = await fetch(`/api/refine/${id}/turns`)
      const turns = await res.json() as Array<{ role: string, content: string, phase: string | null }>
      messages.value = turns.map(t => ({ role: t.role as 'user' | 'assistant', content: t.content, phase: t.phase }))
      // Rebuild completed phases from history
      for (const t of turns) {
        if (t.phase) completedPhases.value.add(t.phase)
      }
      if (completedPhases.value.has('approval')) approvalReady.value = true
    }
    catch {
      error.value = 'Failed to load history'
    }
  }

  async function sendMessage(message: string) {
    const id = taskId()
    if (!id || isStreaming.value) return
    messages.value.push({ role: 'user', content: message })
    isStreaming.value = true
    error.value = null

    let assistantContent = ''
    const assistantIdx = messages.value.push({ role: 'assistant', content: '' }) - 1

    try {
      const res = await fetch(`/api/refine/${id}/turn`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message }),
      })

      const reader = res.body!.getReader()
      const decoder = new TextDecoder()
      let buffer = ''

      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        const parts = buffer.split('\n\n')
        buffer = parts.pop() ?? ''

        for (const part of parts) {
          const lines = part.split('\n')
          const eventLine = lines.find(l => l.startsWith('event:'))
          const dataLine = lines.find(l => l.startsWith('data:'))
          if (!dataLine) continue
          const data = JSON.parse(dataLine.slice(5))
          const event = eventLine ? eventLine.slice(7) : 'message'

          if (event === 'phase_change' && data.phase) {
            completedPhases.value.add(data.phase)
            messages.value[assistantIdx].phase = data.phase
            if (data.phase === 'approval') approvalReady.value = true
          }
          else if (data.text) {
            assistantContent += data.text
            messages.value[assistantIdx].content = assistantContent
          }
        }
      }
    }
    catch (err) {
      error.value = String(err)
    }
    finally {
      isStreaming.value = false
    }
  }

  async function confirm(): Promise<PipelineTask | null> {
    const id = taskId()
    if (!id) return null
    const res = await fetch(`/api/refine/${id}/confirm`, { method: 'POST' })
    if (!res.ok) {
      error.value = (await res.json()).error
      return null
    }
    return res.json()
  }

  function phaseLabel(phase: string) {
    return PHASE_LABELS[phase] ?? phase
  }

  return {
    messages,
    currentPhase,
    completedPhases,
    isStreaming,
    error,
    approvalReady,
    loadHistory,
    sendMessage,
    confirm,
    phaseLabel,
  }
}
```

- [ ] **Step 2: Create src/components/RefinementChat.vue**

```vue
<script setup lang="ts">
import type { PipelineTask } from '../types'
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRefinementChat } from '../composables/useRefinementChat'

const props = defineProps<{ open: boolean, task: PipelineTask | null }>()
const emit = defineEmits<{ close: [], confirmed: [task: PipelineTask] }>()

const inputText = ref('')
const chatEl = ref<HTMLElement | null>(null)

const taskId = computed(() => props.task?.id ?? null)
const {
  messages, completedPhases, isStreaming, error,
  approvalReady, loadHistory, sendMessage, confirm, phaseLabel,
} = useRefinementChat(() => taskId.value)

const EXAMPLE_CHIPS = [
  'Ein neues Feature implementieren',
  'Einen Bug beheben',
  'Code refactoren',
  'Eine neue API-Integration',
]

watch(() => props.open, async (val) => {
  if (val && props.task) {
    await loadHistory()
  }
})

onMounted(() => {
  if (props.open && props.task) loadHistory()
})

watch(messages, async () => {
  await nextTick()
  chatEl.value?.scrollTo({ top: chatEl.value.scrollHeight, behavior: 'smooth' })
}, { deep: true })

async function handleSend() {
  const msg = inputText.value.trim()
  if (!msg || isStreaming.value) return
  inputText.value = ''
  await sendMessage(msg)
}

async function handleConfirm() {
  const updated = await confirm()
  if (updated) emit('confirmed', updated)
}

function isPhaseMarker(idx: number): string | null {
  const msg = messages.value[idx]
  if (msg.role !== 'assistant' || !msg.phase) return null
  return msg.phase
}
</script>

<template>
  <div v-if="open" class="chat-backdrop" @click.self="emit('close')">
    <div class="chat-modal">
      <div class="chat-header">
        <span class="chat-title">Neues Ticket</span>
        <button class="chat-close" @click="emit('close')">✕</button>
      </div>

      <div ref="chatEl" class="chat-messages">
        <!-- Empty state with example chips -->
        <div v-if="messages.length === 0" class="chat-empty">
          <p class="chat-greeting">Was möchtest du umsetzen?</p>
          <div class="chip-row">
            <button
              v-for="chip in EXAMPLE_CHIPS"
              :key="chip"
              class="chip"
              @click="inputText = chip"
            >
              {{ chip }}
            </button>
          </div>
        </div>

        <!-- Message bubbles -->
        <template v-for="(msg, idx) in messages" :key="idx">
          <!-- Phase milestone marker -->
          <div v-if="isPhaseMarker(idx)" class="phase-marker">
            ── ✓ {{ phaseLabel(isPhaseMarker(idx)!) }} abgeschlossen ──
          </div>

          <div :class="['bubble', msg.role]">
            <span class="bubble-content">{{ msg.content }}</span>
          </div>
        </template>

        <div v-if="isStreaming" class="bubble assistant streaming">
          <span class="dot-pulse" />
        </div>
      </div>

      <div v-if="error" class="chat-error">{{ error }}</div>

      <!-- Confirm button after approval phase -->
      <div v-if="approvalReady" class="confirm-bar">
        <button class="btn-confirm" @click="handleConfirm">
          Task erstellen →
        </button>
      </div>

      <!-- Input bar -->
      <div class="chat-input-bar">
        <input
          v-model="inputText"
          class="chat-input"
          placeholder="Nachricht..."
          :disabled="isStreaming || approvalReady"
          @keydown.enter.exact.prevent="handleSend"
        />
        <button
          class="btn-send"
          :disabled="isStreaming || !inputText.trim() || approvalReady"
          @click="handleSend"
        >→</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.chat-backdrop {
  position: fixed; inset: 0; background: rgba(0,0,0,0.5);
  display: flex; align-items: center; justify-content: center; z-index: 100;
}
.chat-modal {
  background: var(--color-surface, #1a1a2e);
  border-radius: 12px; width: min(680px, 95vw);
  height: min(720px, 90vh);
  display: flex; flex-direction: column; overflow: hidden;
}
.chat-header {
  display: flex; justify-content: space-between; align-items: center;
  padding: 16px 20px; border-bottom: 1px solid var(--color-border, #333);
}
.chat-title { font-size: 1.1rem; font-weight: 600; }
.chat-close { background: none; border: none; cursor: pointer; font-size: 1.2rem; }
.chat-messages {
  flex: 1; overflow-y: auto; padding: 20px;
  display: flex; flex-direction: column; gap: 12px;
}
.chat-empty { text-align: center; margin-top: 40px; }
.chat-greeting { font-size: 1.1rem; margin-bottom: 16px; }
.chip-row { display: flex; flex-wrap: wrap; gap: 8px; justify-content: center; }
.chip {
  padding: 8px 14px; border-radius: 20px; border: 1px solid var(--color-border, #444);
  background: none; cursor: pointer; font-size: 0.9rem;
}
.chip:hover { background: var(--color-hover, #2a2a3e); }
.bubble {
  max-width: 80%; padding: 10px 14px; border-radius: 12px; line-height: 1.5;
  white-space: pre-wrap; word-break: break-word;
}
.bubble.user { align-self: flex-end; background: var(--color-primary, #4c6ef5); color: white; }
.bubble.assistant { align-self: flex-start; background: var(--color-surface-alt, #252540); }
.phase-marker {
  text-align: center; font-size: 0.8rem; color: var(--color-success, #69db7c);
  opacity: 0.8; padding: 4px 0;
}
.chat-error { padding: 8px 20px; color: var(--color-error, #ff6b6b); font-size: 0.85rem; }
.confirm-bar { padding: 12px 20px; border-top: 1px solid var(--color-border, #333); }
.btn-confirm {
  width: 100%; padding: 12px; border-radius: 8px;
  background: var(--color-success, #69db7c); color: #000;
  border: none; cursor: pointer; font-size: 1rem; font-weight: 600;
}
.chat-input-bar {
  display: flex; gap: 8px; padding: 12px 20px;
  border-top: 1px solid var(--color-border, #333);
}
.chat-input {
  flex: 1; padding: 10px 14px; border-radius: 8px;
  border: 1px solid var(--color-border, #444); background: var(--color-surface-alt, #252540);
  color: inherit; font-size: 0.95rem;
}
.chat-input:disabled { opacity: 0.5; }
.btn-send {
  padding: 10px 16px; border-radius: 8px;
  background: var(--color-primary, #4c6ef5); color: white;
  border: none; cursor: pointer; font-size: 1.1rem;
}
.btn-send:disabled { opacity: 0.4; cursor: default; }
.dot-pulse::before { content: '●●●'; animation: pulse 1s infinite; }
@keyframes pulse { 0%, 100% { opacity: 0.3 } 50% { opacity: 1 } }
</style>
```

- [ ] **Step 3: Commit**

```bash
git add src/composables/useRefinementChat.ts src/components/RefinementChat.vue
git commit -m "feat: add RefinementChat component and useRefinementChat composable"
```

---

## Task 12: Wire RefinementChat in App.vue, create konzept task on open, delete BacklogForm

**Files:**
- Modify: `src/App.vue`
- Delete: `src/components/BacklogForm.vue`

- [ ] **Step 1: Update App.vue imports and state**

Find the `BacklogForm` import and replace:
```typescript
// REMOVE:
import BacklogForm from './components/BacklogForm.vue'

// ADD:
import RefinementChat from './components/RefinementChat.vue'
```

Find the `showBacklogForm` ref (or similar) and add:
```typescript
const activeKonzeptTask = ref<PipelineTask | null>(null)
```

- [ ] **Step 2: Update the "New Task" button handler**

Find the handler that sets `showBacklogForm = true` and replace with logic that creates a `konzept` task immediately, then opens the chat:

```typescript
import { createTask } from './composables/useTasks'

async function openNewTask() {
  // Create the task in konzept stage immediately so chat history is persisted from turn 1.
  const task = await createTask({
    slug: `konzept-${Date.now()}`,
    title: 'New Task',
    cwd: '/',          // Claude will ask for the real cwd during analyse phase
    stage: 'konzept',
  })
  if (task) activeKonzeptTask.value = task
}
```

- [ ] **Step 3: Replace BacklogForm usage in template**

Find the `<BacklogForm ... />` component and replace:
```html
<RefinementChat
  :open="activeKonzeptTask !== null"
  :task="activeKonzeptTask"
  @close="activeKonzeptTask = null"
  @confirmed="activeKonzeptTask = null"
/>
```

- [ ] **Step 4: Verify createTask supports stage parameter**

Check `src/composables/useTasks.ts` — ensure `createTask` accepts a `stage` field in its input. If not, add it:

In `useTasks.ts`, find the `createTask` function and verify the payload includes `stage`:
```typescript
export async function createTask(input: {
  slug: string
  title: string
  cwd: string
  stage?: string
  // ... other fields
}): Promise<PipelineTask | null>
```

If the API endpoint (`POST /api/tasks`) does not yet accept a `stage` field, add support in `taskRoutes.ts` at the task-creation handler — set `currentStage` to `input.stage ?? 'konzept'` (defaulting to `konzept` for new tasks going forward).

- [ ] **Step 5: Delete BacklogForm.vue**

```bash
rm src/components/BacklogForm.vue
```

- [ ] **Step 6: Start dev server and test the full flow**

```bash
pnpm dev
```

1. Click "New Task" — chat modal should open with example chips
2. Type a message — Claude should respond (requires `claude` CLI in PATH)
3. Verify phase milestone badge appears after analyse phase
4. Verify "Task erstellen →" button appears after approval phase
5. Click "Task erstellen →" — task should appear in Backlog column

- [ ] **Step 7: Add "Chat fortsetzen" button to konzept task cards**

In `src/components/TaskCard.vue` (the card shown in the pipeline board), add a button for `konzept` stage tasks:

```vue
<button
  v-if="task.currentStage === 'konzept'"
  class="btn-resume-chat"
  @click.stop="emit('openChat', task)"
>
  Chat fortsetzen →
</button>
```

Add `openChat` to the emits definition. In `src/components/PipelineBoard.vue`, handle this emit:

```vue
<TaskCard
  v-for="task in col.tasks"
  :key="task.id"
  :task="task"
  @openChat="activeKonzeptTask = $event"
/>
```

Wire `activeKonzeptTask` and `<RefinementChat>` in `PipelineBoard.vue` the same way as in `App.vue` (same props/emits pattern). Alternatively, lift this state up to `App.vue` and pass down via a composable.

- [ ] **Step 8: Commit**

```bash
git add src/App.vue src/composables/useTasks.ts src/components/TaskCard.vue src/components/PipelineBoard.vue
git rm src/components/BacklogForm.vue
git commit -m "feat: wire RefinementChat in App.vue, resume button on konzept cards, remove BacklogForm"
```

---

## Task 13: One-time DB migration — delete tasks in old stages

- [ ] **Step 1: Delete existing tasks in removed stages**

```bash
sqlite3 ~/.claude/dashboard-tasks.db "
DELETE FROM tasks
WHERE current_stage IN ('pruefung','refinement','planning','approval1','umsetzungskonzept','approval2');
"
```

- [ ] **Step 2: Verify**

```bash
sqlite3 ~/.claude/dashboard-tasks.db "SELECT current_stage, COUNT(*) FROM tasks GROUP BY current_stage;"
```

Expected: only `konzept`, `backlog`, `umsetzung`, `selbstreview`, `finalisierung`, `done`, `on_hold`, `cancelled` rows.

- [ ] **Step 3: Run full test suite**

```bash
pnpm test 2>&1 | tail -20
```

Expected: all tests pass.

- [ ] **Step 4: Final commit**

```bash
git add -A
git commit -m "feat: agent-based ticket refinement — full implementation complete"
```
