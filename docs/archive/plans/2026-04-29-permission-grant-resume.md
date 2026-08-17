# Permission-Grant Resume with Handoff Note Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When a user grants a permission request, the restarted stage agent resumes via `--resume <sessionId>` and receives a short handoff note instead of restarting completely blind.

**Architecture:** The single change is in `server/routes/taskRoutes.ts` in the permission-grant path. Before calling `progressTask`, capture `run.sessionId`, build a one-line handoff note from the permission's tool/pattern, and pass both as opts to `progressTask`. The orchestrator, spawner, and `StageContext` already thread both fields through unchanged.

**Tech Stack:** TypeScript, Express, Vitest, better-sqlite3

---

### Task 1: Write the failing tests

**Files:**
- Modify: `server/routes/taskRoutes.test.ts`

- [ ] **Step 1: Add `vi` to vitest import**

In `server/routes/taskRoutes.test.ts`, line 9, change:

```typescript
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
```

to:

```typescript
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
```

- [ ] **Step 2: Add two new tests to the `permission request resolution` describe block**

Append inside the `describe('permission request resolution', ...)` block (after the `rejects invalid outcome` test, before the closing `}`):

```typescript
  it('passes resumeSessionId and handoff note to progressTask when session is attached', async () => {
    const spy = vi.spyOn(orchestrator, 'progressTask')

    const { data: task } = await api<{ id: string }>('POST', '/tasks', {
      slug: 'rs',
      title: 'RS',
      cwd: '/rs',
    })

    const { createStageRun, updateStageRun } = await import('../db/stageRunsRepo.js')
    const run = createStageRun({ taskId: task.id, stage: 'umsetzung' })
    updateStageRun(run.id, { status: 'awaiting_user', sessionId: 'test-session-xyz' })

    const { data: reqRow } = await api<{ id: string }>('POST', '/permission-requests', {
      stageRunId: run.id,
      tool: 'Bash',
      pattern: 'git *',
    })

    await api('POST', `/permission-requests/${reqRow.id}/resolve`, { outcome: 'granted' })

    expect(spy).toHaveBeenCalledWith(
      task.id,
      expect.objectContaining({
        resumeSessionId: 'test-session-xyz',
        userAdditionalPrompt: expect.stringContaining('[PERMISSION GRANTED]'),
      }),
    )
    spy.mockRestore()
  })

  it('passes only handoff note (no resumeSessionId) when session_id is null', async () => {
    const spy = vi.spyOn(orchestrator, 'progressTask')

    const { data: task } = await api<{ id: string }>('POST', '/tasks', {
      slug: 'rn',
      title: 'RN',
      cwd: '/rn',
    })

    const { createStageRun, updateStageRun } = await import('../db/stageRunsRepo.js')
    const run = createStageRun({ taskId: task.id, stage: 'umsetzung' })
    updateStageRun(run.id, { status: 'awaiting_user' }) // sessionId remains null

    const { data: reqRow } = await api<{ id: string }>('POST', '/permission-requests', {
      stageRunId: run.id,
      tool: 'WebFetch',
    })

    await api('POST', `/permission-requests/${reqRow.id}/resolve`, { outcome: 'granted' })

    const [, opts] = spy.mock.calls[0]!
    expect(opts?.resumeSessionId).toBeUndefined()
    expect(opts?.userAdditionalPrompt).toContain('[PERMISSION GRANTED]')
    spy.mockRestore()
  })
```

- [ ] **Step 3: Run tests to verify they fail**

```bash
pnpm test -- server/routes/taskRoutes.test.ts
```

Expected: two new tests fail — `progressTask` is called without opts so the assertions on `resumeSessionId` and `userAdditionalPrompt` fail.

---

### Task 2: Implement the fix

**Files:**
- Modify: `server/routes/taskRoutes.ts:714-737`

- [ ] **Step 1: Replace the bare `progressTask` call in the permission-grant restart path**

In `server/routes/taskRoutes.ts`, find the block starting at line 714:

```typescript
        if (run.status === 'awaiting_user') {
          if (run.pid !== null) {
            try {
              process.kill(run.pid, 'SIGTERM')
            }
            catch { /* already dead */ }
          }
          updateStageRun(run.id, {
            status: 'failed',
            output: { error: 'restarting after permission grant' },
            endedAt: new Date().toISOString(),
          })
          appendAudit({
            taskId: run.taskId,
            actor: 'user',
            action: 'permission_granted_restart',
            details: {
              permissionRequestId: existing.id,
              tool: existing.tool,
              pattern: existing.pattern,
              stageRunId: run.id,
            },
          })
          await deps.orchestrator.progressTask(run.taskId)
        }
```

Replace with:

```typescript
        if (run.status === 'awaiting_user') {
          if (run.pid !== null) {
            try {
              process.kill(run.pid, 'SIGTERM')
            }
            catch { /* already dead */ }
          }
          updateStageRun(run.id, {
            status: 'failed',
            output: { error: 'restarting after permission grant' },
            endedAt: new Date().toISOString(),
          })
          appendAudit({
            taskId: run.taskId,
            actor: 'user',
            action: 'permission_granted_restart',
            details: {
              permissionRequestId: existing.id,
              tool: existing.tool,
              pattern: existing.pattern,
              stageRunId: run.id,
            },
          })
          const handoffNote = `[PERMISSION GRANTED] You requested permission for "${existing.tool}"${existing.pattern ? ` (${existing.pattern})` : ''}. It has been granted. Resume exactly where you left off.`
          await deps.orchestrator.progressTask(run.taskId, {
            resumeSessionId: run.sessionId ?? undefined,
            userAdditionalPrompt: handoffNote,
          })
        }
```

- [ ] **Step 2: Run tests to verify they pass**

```bash
pnpm test -- server/routes/taskRoutes.test.ts
```

Expected: all tests pass including the two new ones.

- [ ] **Step 3: Run full test suite and typecheck**

```bash
pnpm test && pnpm typecheck
```

Expected: all tests pass, no type errors.

- [ ] **Step 4: Commit**

```bash
git add server/routes/taskRoutes.ts server/routes/taskRoutes.test.ts
git commit -m "feat(pipeline): resume stage agent with --resume and handoff note after permission grant"
```
