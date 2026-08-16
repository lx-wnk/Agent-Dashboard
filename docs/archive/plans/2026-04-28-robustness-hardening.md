# Robustness Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix 7 concrete bugs across refinement chat, pipeline state machine, and permission system — process timeout, stderr capture, history windowing, review cycle cap, spawn error handler, last-JSON-block consistency, settings.json isolation, and preset reset UI.

**Architecture:** Each fix is independent; implement in order to minimise merge conflicts. All backend fixes include new/updated Vitest unit tests. The frontend C2 section extends the existing `ApiKeySettings.vue` with a new nav item and section panel. No new files are created except the preset route module.

**Tech Stack:** Bun + Express (server), Vue 3 `<script setup>` (frontend), Vitest (tests), bun:sqlite via `server/db/client.ts`

---

## Pre-work: Create the branch

- [ ] **Create and check out new branch**

```bash
git checkout main
git pull
git checkout -b feat/robustness-hardening
```

---

## Task 1: C1 — Last JSON block in sessionOutputReader

**Files:**
- Modify: `server/pipeline/sessionOutputReader.ts` (line 14 — regex constant, and `extractJsonBlock` function)
- Modify: `server/pipeline/sessionOutputReader.test.ts` (update the "first block" test to "last block")

- [ ] **Step 1: Update the test to expect the last block**

In `server/pipeline/sessionOutputReader.test.ts`, change the test at the bottom of `describe('extractJsonBlock', ...)`:

```ts
// BEFORE:
it('extracts only the first block when multiple are present', () => {
  const text = '```json\n{"a": 1}\n```\n```json\n{"b": 2}\n```'
  expect(extractJsonBlock(text)).toEqual({ a: 1 })
})

// AFTER:
it('extracts the LAST block when multiple are present', () => {
  const text = '```json\n{"a": 1}\n```\n```json\n{"b": 2}\n```'
  expect(extractJsonBlock(text)).toEqual({ b: 2 })
})
```

- [ ] **Step 2: Run the test to confirm it fails**

```bash
pnpm test server/pipeline/sessionOutputReader.test.ts
```

Expected: FAIL — `Expected: {"b": 2}, Received: {"a": 1}`

- [ ] **Step 3: Fix extractJsonBlock to use the last match**

In `server/pipeline/sessionOutputReader.ts`, replace:

```ts
const JSON_BLOCK_RE = /```json\b([\s\S]*?)```/i
```

with:

```ts
const JSON_BLOCK_RE_G = /```json\b([\s\S]*?)```/gi
```

And replace the `extractJsonBlock` function body:

```ts
export function extractJsonBlock(text: string): Record<string, unknown> | null {
  const match = text.match(JSON_BLOCK_RE)
  if (!match)
    return null
  try {
    const parsed = JSON.parse(match[1].trim())
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed))
      return null
    return parsed as Record<string, unknown>
  }
  catch {
    return null
  }
}
```

with:

```ts
export function extractJsonBlock(text: string): Record<string, unknown> | null {
  const matches = [...text.matchAll(JSON_BLOCK_RE_G)]
  const match = matches.at(-1)
  if (!match)
    return null
  try {
    const parsed = JSON.parse(match[1].trim())
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed))
      return null
    return parsed as Record<string, unknown>
  }
  catch {
    return null
  }
}
```

- [ ] **Step 4: Run the test suite**

```bash
pnpm test server/pipeline/sessionOutputReader.test.ts
```

Expected: all tests pass.

- [ ] **Step 5: Typecheck**

```bash
pnpm typecheck
```

Expected: 0 errors.

- [ ] **Step 6: Commit**

```bash
git add server/pipeline/sessionOutputReader.ts server/pipeline/sessionOutputReader.test.ts
git commit -m "fix(C1): extractJsonBlock returns last json block, not first"
```

---

## Task 2: B2 — Spawn error handler in agentSpawner

**Context:** `isPidAlive(0)` already returns `false` (the existing `pid <= 0` guard in `sessionManager.ts` handles it). This task only adds the `error` event handler so a failed spawn doesn't crash the process with an unhandled EventEmitter error.

**Files:**
- Modify: `server/pipeline/agentSpawner.ts` (after `child.unref()`)
- Modify: `server/pipeline/agentSpawner.test.ts` (note: `spawnStageAgent` isn't easily unit-testable without real `claude` binary; document via existing pattern)

- [ ] **Step 1: Add error handler to spawnStageAgent**

In `server/pipeline/agentSpawner.ts`, in `spawnStageAgent`, after `child.unref()` (currently line 160), add:

```ts
child.on('error', (err) => {
  consola.error(`[agentSpawner] spawn failed for task ${opts.task.id} stage ${opts.stageRun.stage}: ${err.message}`)
})
```

So the full spawn block looks like:

```ts
const child = spawn('claude', args, {
  cwd,
  detached: true,
  stdio: ['ignore', 'ignore', 'pipe'],
  env: buildSpawnEnv(opts),
})

child.stderr?.on('data', () => { /* drain */ })
child.stderr?.on('error', () => { /* child exit may trigger EPIPE */ })

child.on('error', (err) => {
  consola.error(`[agentSpawner] spawn failed for task ${opts.task.id} stage ${opts.stageRun.stage}: ${err.message}`)
})

child.unref()
```

Make sure `consola` is already imported — it is already imported at the top of the file via `import { consola } from 'consola'`. If not, add it.

- [ ] **Step 2: Verify consola import exists**

```bash
grep "from 'consola'" server/pipeline/agentSpawner.ts
```

If absent, add `import { consola } from 'consola'` near the top of the imports section.

- [ ] **Step 3: Run all tests**

```bash
pnpm test
```

Expected: all existing tests pass (no change to unit-testable pure functions).

- [ ] **Step 4: Typecheck**

```bash
pnpm typecheck
```

Expected: 0 errors.

- [ ] **Step 5: Commit**

```bash
git add server/pipeline/agentSpawner.ts
git commit -m "fix(B2): add error handler to stage agent spawn to prevent unhandled EventEmitter crash"
```

---

## Task 3: B1 — Review cycle cap in orchestrator

**Files:**
- Modify: `server/pipeline/orchestrator.ts` (`decideCompletedTransition`, selbstreview branch)
- Modify: `server/pipeline/orchestrator.test.ts` (add new test for cycle cap)

- [ ] **Step 1: Write the failing test**

In `server/pipeline/orchestrator.test.ts`, find the end of the file and add a new `describe` block. First check if there's already a test for `decideCompletedTransition` behavior — if not, add:

```ts
describe('selbstreview cycle cap', () => {
  it('loops to umsetzung on first failed review, escalates after cap', async () => {
    setPipelineConfig('maxReviewCycles', '2')
    const task = createTask({ slug: 'cycle-cap', title: 'Cycle', cwd: '/cycle' })
    updateTask(task.id, { currentStage: 'selbstreview' })

    // Stub selbstreview to return async_running with a dead PID (so tick can finalize it)
    orchestrator.setHandler('selbstreview', makeStubHandler('selbstreview', {
      kind: 'async_running',
      pid: 2147483647,
    }))
    // Stub umsetzung too — prevents real claude spawn when tick picks the task up
    orchestrator.setHandler('umsetzung', makeStubHandler('umsetzung', {
      kind: 'wait_user',
      reason: 'stub',
    }))
    // Completion detector always returns failed review
    orchestrator.setCompletionDetector(async () => ({
      kind: 'completed' as const,
      output: { passed: false, findings: [], summary: 'bad' },
    }))

    // --- First cycle: review_cycles goes from 0 to 1, cap is 2, should loop ---
    await orchestrator.progressTask(task.id)
    await orchestrator.tick()
    const afterFirst = getTaskById(task.id)
    expect(afterFirst?.currentStage).toBe('umsetzung')
    expect((afterFirst?.metadata as Record<string, unknown>)?.review_cycles).toBe(1)

    // --- Second cycle: push back to selbstreview, review_cycles will hit 2 >= 2 ---
    updateTask(task.id, { currentStage: 'selbstreview' })
    await orchestrator.progressTask(task.id)
    await orchestrator.tick()
    const run = getLatestStageRun(task.id, 'selbstreview')
    expect(run?.status).toBe('awaiting_user')
  })
})
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
pnpm test server/pipeline/orchestrator.test.ts
```

Expected: FAIL — the second cycle transitions to umsetzung instead of awaiting_user.

- [ ] **Step 3: Implement the cycle cap in decideCompletedTransition**

In `server/pipeline/orchestrator.ts`, find `private decideCompletedTransition` and replace the `if (run.stage === 'selbstreview')` block:

```ts
// BEFORE:
if (run.stage === 'selbstreview') {
  const passed = output.passed === true
  if (!passed) {
    const feedback = summarizeReviewFindings(output)
    const nextMeta = { ...(task.metadata ?? {}), review_feedback: feedback }
    return { kind: 'next', toStage: 'umsetzung', output, taskMetadataPatch: nextMeta }
  }
  // Passed — clear any lingering feedback so the finalisierung
  // handler doesn't read stale review notes from metadata.
  if (task.metadata && typeof task.metadata === 'object' && 'review_feedback' in task.metadata) {
    const { review_feedback: _drop, ...rest } = task.metadata
    const cleared = Object.keys(rest).length > 0 ? rest : null
    return { kind: 'next', toStage: 'finalisierung', output, taskMetadataPatch: cleared }
  }
  return { kind: 'next', toStage: 'finalisierung', output }
}

// AFTER:
if (run.stage === 'selbstreview') {
  const passed = output.passed === true
  if (!passed) {
    const cycles = ((task.metadata?.review_cycles as number | undefined) ?? 0) + 1
    const maxCycles = (task.metadata?.maxReviewCycles as number | undefined)
      ?? getPipelineConfigNumber('maxReviewCycles', 3)
    if (cycles >= maxCycles) {
      return {
        kind: 'wait_user',
        reason: `review cycle limit (${maxCycles}) reached — manual intervention required`,
        output,
      }
    }
    const feedback = summarizeReviewFindings(output)
    const nextMeta = { ...(task.metadata ?? {}), review_feedback: feedback, review_cycles: cycles }
    return { kind: 'next', toStage: 'umsetzung', output, taskMetadataPatch: nextMeta }
  }
  // Passed — clear review tracking state so finalisierung is clean.
  if (task.metadata && typeof task.metadata === 'object'
    && ('review_feedback' in task.metadata || 'review_cycles' in task.metadata)) {
    const { review_feedback: _rf, review_cycles: _rc, ...rest } = task.metadata as Record<string, unknown>
    const cleared = Object.keys(rest).length > 0 ? rest as Record<string, unknown> : null
    return { kind: 'next', toStage: 'finalisierung', output, taskMetadataPatch: cleared }
  }
  return { kind: 'next', toStage: 'finalisierung', output }
}
```

- [ ] **Step 4: Run the new test**

```bash
pnpm test server/pipeline/orchestrator.test.ts
```

Expected: all tests pass including the new `selbstreview cycle cap` test.

- [ ] **Step 5: Full test suite**

```bash
pnpm test
```

Expected: all tests pass.

- [ ] **Step 6: Typecheck**

```bash
pnpm typecheck
```

Expected: 0 errors.

- [ ] **Step 7: Commit**

```bash
git add server/pipeline/orchestrator.ts server/pipeline/orchestrator.test.ts
git commit -m "fix(B1): cap selbstreview->umsetzung cycle with review_cycles metadata counter"
```

---

## Task 4: A2 — Capture stderr in refinementSpawner

**Files:**
- Modify: `server/services/refinementSpawner.ts` (`SpawnRefinementResult` interface, `spawnRefinementTurn` function)
- Modify: `server/services/refinementSpawner.test.ts` (no existing test for stderr; skip)
- Modify: `server/routes/refineRoutes.ts` (pass `getStderr()` into error SSE events)

- [ ] **Step 1: Extend SpawnRefinementResult and add stderr ring-buffer**

In `server/services/refinementSpawner.ts`, change `SpawnRefinementResult`:

```ts
// BEFORE:
export interface SpawnRefinementResult {
  stdout: Readable
  waitForExit: () => Promise<void>
}

// AFTER:
export interface SpawnRefinementResult {
  stdout: Readable
  waitForExit: () => Promise<void>
  getStderr: () => string
}
```

Then in `spawnRefinementTurn`, replace the silent stderr drain:

```ts
// BEFORE:
child.stderr?.on('data', () => { /* drain to prevent pipe buffer fill */ })
child.stderr?.on('error', () => { /* swallow EPIPE on exit */ })

// AFTER:
let stderrBuf = ''
child.stderr?.on('data', (chunk: Buffer) => {
  if (stderrBuf.length < 500)
    stderrBuf += chunk.toString().slice(0, 500 - stderrBuf.length)
})
child.stderr?.on('error', () => { /* swallow EPIPE on exit */ })
```

And extend the return value:

```ts
// BEFORE:
return { stdout: child.stdout, waitForExit }

// AFTER:
return { stdout: child.stdout, waitForExit, getStderr: () => stderrBuf }
```

- [ ] **Step 2: Use getStderr() in refineRoutes.ts error paths**

In `server/routes/refineRoutes.ts`, the `spawnRefinementTurn` call currently returns `{ stdout, waitForExit }`. Change it to destructure `getStderr`:

```ts
// BEFORE:
const { stdout, waitForExit } = spawnRefinementTurn(spawnMessage, history, task.cwd)

// AFTER:
const { stdout, waitForExit, getStderr } = spawnRefinementTurn(spawnMessage, history, task.cwd)
```

Then update all three error write calls in the route to include stderr when available:

**In `stdout.on('error', ...)` handler:**
```ts
// BEFORE:
res.write(`event: error\ndata: ${JSON.stringify({ error: String(streamErr) })}\n\n`)

// AFTER:
const stderrSnippet = getStderr()
res.write(`event: error\ndata: ${JSON.stringify({ error: String(streamErr), ...(stderrSnippet ? { stderr: stderrSnippet } : {}) })}\n\n`)
```

**In the `catch (err)` block:**
```ts
// BEFORE:
res.write(`event: error\ndata: ${JSON.stringify({ error: err instanceof Error ? err.message : 'spawn failed' })}\n\n`)

// AFTER:
const stderrSnippet = getStderr()
res.write(`event: error\ndata: ${JSON.stringify({ error: err instanceof Error ? err.message : 'spawn failed', ...(stderrSnippet ? { stderr: stderrSnippet } : {}) })}\n\n`)
```

- [ ] **Step 3: Run tests**

```bash
pnpm test server/services/refinementSpawner.test.ts
```

Expected: all existing tests still pass (they test `serializeHistory` and `REFINEMENT_SYSTEM_PROMPT`, which are untouched).

- [ ] **Step 4: Typecheck**

```bash
pnpm typecheck
```

Expected: 0 errors.

- [ ] **Step 5: Commit**

```bash
git add server/services/refinementSpawner.ts server/routes/refineRoutes.ts
git commit -m "fix(A2): capture first 500 chars of stderr and surface in error SSE event"
```

---

## Task 5: A1 — Process timeout + connection-close guard

**Files:**
- Modify: `server/services/refinementSpawner.ts` (add `child: ChildProcess` to `SpawnRefinementResult`)
- Modify: `server/routes/refineRoutes.ts` (add timeout + close handler)

- [ ] **Step 1: Expose child in SpawnRefinementResult**

In `server/services/refinementSpawner.ts`, add the `ChildProcess` import and extend the result type:

At the top, `Readable` is already imported from `node:stream`. Also add `ChildProcess`:

```ts
// BEFORE:
import type { Readable } from 'node:stream'
import { spawn } from 'node:child_process'

// AFTER:
import type { ChildProcess } from 'node:child_process'
import type { Readable } from 'node:stream'
import { spawn } from 'node:child_process'
```

Then add `child` to `SpawnRefinementResult`:

```ts
// BEFORE:
export interface SpawnRefinementResult {
  stdout: Readable
  waitForExit: () => Promise<void>
  getStderr: () => string
}

// AFTER:
export interface SpawnRefinementResult {
  child: ChildProcess
  stdout: Readable
  waitForExit: () => Promise<void>
  getStderr: () => string
}
```

And update the return statement:

```ts
// BEFORE:
return { stdout: child.stdout, waitForExit, getStderr: () => stderrBuf }

// AFTER:
return { child, stdout: child.stdout, waitForExit, getStderr: () => stderrBuf }
```

- [ ] **Step 2: Add timeout and close-guard to the turn route**

In `server/routes/refineRoutes.ts`, first verify `process` is imported (all other server files import it explicitly). The existing imports include `node:buffer`, `node:fs`, `node:os`, `node:path` — add if missing:

```ts
import process from 'node:process'
```

Then at the top of the file where env constants are read, add:

```ts
const REFINEMENT_TIMEOUT_MS = Number(process.env.REFINEMENT_TIMEOUT_MS) || 5 * 60 * 1000
```

Then in the POST `/:taskId/turn` handler, update the destructure and add the guards. Find the line:

```ts
const { stdout, waitForExit, getStderr } = spawnRefinementTurn(spawnMessage, history, task.cwd)
```

And change to:

```ts
const { child, stdout, waitForExit, getStderr } = spawnRefinementTurn(spawnMessage, history, task.cwd)

// Kill the process if the client disconnects mid-stream.
const onClose = () => {
  child.kill('SIGTERM')
}
res.on('close', onClose)

// Hard timeout backstop — prevents activeTurns from leaking forever on hangs.
const timeoutHandle = setTimeout(() => {
  child.kill('SIGTERM')
}, REFINEMENT_TIMEOUT_MS)
```

Then in the `finally` block, add cleanup:

```ts
finally {
  clearTimeout(timeoutHandle)
  res.removeListener('close', onClose)
  activeTurns.delete(task.id)
  for (const f of tempFiles) {
    try { unlinkSync(f) } catch {}
  }
}
```

Note: the existing `finally` block already has `activeTurns.delete(task.id)` and the temp-file cleanup — just add the two new lines (`clearTimeout` and `removeListener`) at the top of the finally block.

- [ ] **Step 3: Run tests**

```bash
pnpm test server/services/refinementSpawner.test.ts
```

Expected: all pass.

- [ ] **Step 4: Typecheck**

```bash
pnpm typecheck
```

Expected: 0 errors.

- [ ] **Step 5: Commit**

```bash
git add server/services/refinementSpawner.ts server/routes/refineRoutes.ts
git commit -m "fix(A1): add SIGTERM timeout and connection-close guard to refinement turn handler"
```

---

## Task 6: A3 — Phase-aware history windowing

**Files:**
- Modify: `server/services/refinementSpawner.ts` (`serializeHistory` → `buildWindowedHistory`, update `spawnRefinementTurn` to use it)
- Modify: `server/services/refinementSpawner.test.ts` (add windowing tests)

- [ ] **Step 1: Write the failing tests**

In `server/services/refinementSpawner.test.ts`, add a new `describe` block after the existing tests:

```ts
import { buildWindowedHistory } from './refinementSpawner'

describe('buildWindowedHistory', () => {
  function makeTurn(role: 'user' | 'assistant', content: string, phase?: string): RefinementTurn {
    const phaseContent = phase ? `${content}\n\n__phase_done: ${phase}` : content
    return { id: '1', taskId: 't', role, content: phaseContent, phase: phase ?? null, createdAt: '' }
  }

  it('returns empty string for no turns', () => {
    expect(buildWindowedHistory([])).toBe('')
  })

  it('always includes turns containing __phase_done markers', () => {
    const turns: RefinementTurn[] = [
      makeTurn('user', 'question 1'),
      makeTurn('assistant', 'answer 1', 'analyse'),
      makeTurn('user', 'question 2'),
      makeTurn('assistant', 'answer 2', 'spec'),
      makeTurn('user', 'current question'),
    ]
    const result = buildWindowedHistory(turns)
    expect(result).toContain('__phase_done: analyse')
    expect(result).toContain('__phase_done: spec')
    expect(result).toContain('current question')
  })

  it('truncates earlier non-phase turns when over maxChars', () => {
    const bigContent = 'x'.repeat(5000)
    const turns: RefinementTurn[] = [
      makeTurn('user', bigContent),
      makeTurn('assistant', bigContent),
      makeTurn('user', bigContent),
      makeTurn('assistant', bigContent),
      makeTurn('user', bigContent),
      makeTurn('assistant', bigContent),
      makeTurn('user', bigContent),
      makeTurn('assistant', bigContent),
      makeTurn('user', 'final question'),
    ]
    const result = buildWindowedHistory(turns, 20_000)
    expect(result).toContain('final question')
    expect(result.length).toBeLessThanOrEqual(22_000) // some buffer for header/labels
  })
})
```

Also update the import in the test file to import `buildWindowedHistory` alongside `serializeHistory`.

- [ ] **Step 2: Run tests to confirm they fail**

```bash
pnpm test server/services/refinementSpawner.test.ts
```

Expected: FAIL — `buildWindowedHistory` is not exported yet.

- [ ] **Step 3: Implement buildWindowedHistory**

In `server/services/refinementSpawner.ts`, add the new function BEFORE `spawnRefinementTurn`. Keep `serializeHistory` as it is (it's tested), and make `spawnRefinementTurn` use `buildWindowedHistory` instead.

Add after `serializeHistory`:

```ts
const PHASE_DONE_SIGNAL_RE = /__phase_done:\s*\w+/

/**
 * Phase-aware history builder. Always includes turns that contain a
 * __phase_done signal (they anchor the agent's phase state). From the
 * remaining turns keeps at most the last 2 per phase, then applies a
 * hard char ceiling to guard against context overflow.
 */
export function buildWindowedHistory(
  turns: RefinementTurn[],
  maxChars = 40_000,
): string {
  if (turns.length === 0)
    return ''

  // Separate anchor turns (contain __phase_done) from regular turns.
  const anchorIdxs = new Set<number>()
  for (let i = 0; i < turns.length; i++) {
    if (turns[i].role === 'assistant' && PHASE_DONE_SIGNAL_RE.test(turns[i].content))
      anchorIdxs.add(i)
  }

  // Always include the last 2 non-anchor turns (most recent exchange).
  const nonAnchorIdxs = turns
    .map((_, i) => i)
    .filter(i => !anchorIdxs.has(i))
  const lastTwoNonAnchor = new Set(nonAnchorIdxs.slice(-2))

  // Walk forward and collect what to include.
  const included: RefinementTurn[] = []
  for (let i = 0; i < turns.length; i++) {
    if (anchorIdxs.has(i) || lastTwoNonAnchor.has(i))
      included.push(turns[i])
  }

  // Build the serialized block and apply the char limit.
  const lines = included.map(t =>
    `${t.role === 'user' ? 'Human' : 'Assistant'}: ${t.content}`,
  )
  let result = `Previous conversation:\n${lines.join('\n\n')}\n\n`

  // If still over limit, drop non-anchor turns from the front until it fits.
  if (result.length > maxChars) {
    const anchorTurns = included.filter(t => anchorIdxs.has(turns.indexOf(t)))
    const lastTwo = included.filter(t => lastTwoNonAnchor.has(turns.indexOf(t)))
    const mustKeep = [...anchorTurns, ...lastTwo]
    const mustLines = mustKeep.map(t =>
      `${t.role === 'user' ? 'Human' : 'Assistant'}: ${t.content}`,
    )
    result = `Previous conversation:\n${mustLines.join('\n\n')}\n\n`
  }

  return result
}
```

- [ ] **Step 4: Update spawnRefinementTurn to use buildWindowedHistory**

In `spawnRefinementTurn`, replace `serializeHistory` call:

```ts
// BEFORE:
const historyBlock = serializeHistory(history)

// AFTER:
const historyBlock = buildWindowedHistory(history)
```

- [ ] **Step 5: Run tests**

```bash
pnpm test server/services/refinementSpawner.test.ts
```

Expected: all pass including the new windowing tests.

- [ ] **Step 6: Full test suite**

```bash
pnpm test
```

Expected: all pass.

- [ ] **Step 7: Typecheck**

```bash
pnpm typecheck
```

Expected: 0 errors.

- [ ] **Step 8: Commit**

```bash
git add server/services/refinementSpawner.ts server/services/refinementSpawner.test.ts
git commit -m "fix(A3): phase-aware history windowing — always keep phase-done markers, cap at 40k chars"
```

---

## Task 7: C2 — Permission preset reset (backend)

**Files:**
- Modify: `server/db/permissionPresetsRepo.ts` (add `deletePresetsForProject`, `listPresetsByProject`)
- Create: `server/routes/presetRoutes.ts`
- Modify: `server/index.ts` (mount presetRouter)

- [ ] **Step 1: Add repo functions**

In `server/db/permissionPresetsRepo.ts`, add after `listPresets`:

```ts
export interface PresetProjectSummary {
  cwd: string
  count: number
}

/**
 * Returns a summary of distinct project cwds that have presets for the
 * given user (or any global/null-user presets).
 */
export function listPresetProjectSummaries(
  userId: string | null,
  db: Database = getDb(),
): PresetProjectSummary[] {
  const rows = db
    .prepare(`
      SELECT project_cwd AS cwd, COUNT(*) AS count
      FROM permission_presets
      WHERE user_id = @user_id OR user_id IS NULL
      GROUP BY project_cwd
      ORDER BY project_cwd ASC
    `)
    .all({ user_id: userId }) as Array<{ cwd: string, count: number }>
  return rows
}

/**
 * Deletes all permission presets for the given (userId, projectCwd) pair.
 * Null userId deletes only global (user_id IS NULL) presets for the cwd.
 */
export function deletePresetsForProject(
  userId: string | null,
  projectCwd: string,
  db: Database = getDb(),
): void {
  if (userId === null) {
    db.prepare(`
      DELETE FROM permission_presets
      WHERE project_cwd = @project_cwd AND user_id IS NULL
    `).run({ project_cwd: projectCwd })
  }
  else {
    db.prepare(`
      DELETE FROM permission_presets
      WHERE project_cwd = @project_cwd AND user_id = @user_id
    `).run({ project_cwd: projectCwd, user_id: userId })
  }
}
```

- [ ] **Step 2: Write tests for the new repo functions**

Create `server/db/permissionPresetsRepo.test.ts`:

```ts
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { closeDb, getDb } from './client.js'
import {
  deletePresetsForProject,
  listPresetProjectSummaries,
  listPresets,
  upsertPreset,
} from './permissionPresetsRepo.js'

let tmpDir: string

beforeEach(() => {
  tmpDir = mkdtempSync(join(tmpdir(), 'preset-test-'))
  process.env.DASHBOARD_DB_PATH = join(tmpDir, 'test.db')
  getDb()
})

afterEach(() => {
  closeDb()
  rmSync(tmpDir, { recursive: true, force: true })
  delete process.env.DASHBOARD_DB_PATH
})

describe('upsertPreset + listPresets', () => {
  it('idempotently stores and retrieves presets', () => {
    upsertPreset('user1', '/proj', 'Read', null)
    upsertPreset('user1', '/proj', 'Read', null) // duplicate — no-op
    const entries = listPresets('user1', '/proj')
    expect(entries).toHaveLength(1)
    expect(entries[0]).toEqual({ tool: 'Read', pattern: null })
  })

  it('returns global presets (user_id IS NULL) alongside user presets', () => {
    upsertPreset(null, '/proj', 'Glob', null)
    upsertPreset('user1', '/proj', 'Read', null)
    const entries = listPresets('user1', '/proj')
    expect(entries).toHaveLength(2)
    const tools = entries.map(e => e.tool)
    expect(tools).toContain('Read')
    expect(tools).toContain('Glob')
  })
})

describe('listPresetProjectSummaries', () => {
  it('groups presets by cwd with count', () => {
    upsertPreset('u1', '/a', 'Read', null)
    upsertPreset('u1', '/a', 'Write', null)
    upsertPreset('u1', '/b', 'Bash', 'npm *')
    const summaries = listPresetProjectSummaries('u1')
    expect(summaries).toHaveLength(2)
    const a = summaries.find(s => s.cwd === '/a')
    expect(a?.count).toBe(2)
  })
})

describe('deletePresetsForProject', () => {
  it('deletes all presets for a given user+cwd pair', () => {
    upsertPreset('u1', '/a', 'Read', null)
    upsertPreset('u1', '/a', 'Write', null)
    upsertPreset('u1', '/b', 'Bash', 'npm *')
    deletePresetsForProject('u1', '/a')
    expect(listPresets('u1', '/a')).toHaveLength(0)
    expect(listPresets('u1', '/b')).toHaveLength(1)
  })

  it('does not delete other users presets for the same cwd', () => {
    upsertPreset('u1', '/a', 'Read', null)
    upsertPreset('u2', '/a', 'Read', null)
    deletePresetsForProject('u1', '/a')
    expect(listPresets('u2', '/a')).toHaveLength(1)
  })

  it('deletes only null-user presets when userId is null', () => {
    upsertPreset(null, '/a', 'Read', null)
    upsertPreset('u1', '/a', 'Write', null)
    deletePresetsForProject(null, '/a')
    expect(listPresets(null, '/a')).toHaveLength(1) // u1's Write preset still there via null query
    expect(listPresets('u1', '/a')).toHaveLength(1) // u1's own preset untouched
  })
})
```

- [ ] **Step 3: Run the new tests**

```bash
pnpm test server/db/permissionPresetsRepo.test.ts
```

Expected: all tests pass.

- [ ] **Step 4: Create the preset route**

Create `server/routes/presetRoutes.ts`:

```ts
import type express from 'express'
import { Router } from 'express'
import { deletePresetsForProject, listPresetProjectSummaries } from '../db/permissionPresetsRepo.js'

type RejectCrossOrigin = (req: express.Request, res: express.Response) => boolean

export function createPresetRouter(rejectCrossOrigin: RejectCrossOrigin): Router {
  const router = Router()

  // GET /api/settings/permission-presets
  // Returns [{ cwd, count }] for the authenticated user.
  router.get('/settings/permission-presets', (req, res) => {
    if (rejectCrossOrigin(req, res))
      return
    const userId = req.user?.id ?? null
    res.json(listPresetProjectSummaries(userId))
  })

  // DELETE /api/settings/permission-presets
  // Body: { cwd: string }
  // Deletes all presets for (userId, cwd).
  router.delete('/settings/permission-presets', (req, res) => {
    if (rejectCrossOrigin(req, res))
      return
    const { cwd } = req.body as { cwd?: string }
    if (!cwd || typeof cwd !== 'string' || !cwd.trim()) {
      res.status(400).json({ error: 'cwd is required' })
      return
    }
    const userId = req.user?.id ?? null
    deletePresetsForProject(userId, cwd.trim())
    res.json({ ok: true })
  })

  return router
}
```

- [ ] **Step 5: Mount presetRouter in server/index.ts**

In `server/index.ts`, find where `createApiKeyRouter` is imported and mounted (around line 21 and 311), and add alongside it:

```ts
// Near the imports (line ~21):
import { createPresetRouter } from './routes/presetRoutes.js'

// Near the mount (line ~311):
app.use('/api', createPresetRouter(rejectCrossOrigin))
```

- [ ] **Step 6: Run all tests**

```bash
pnpm test
```

Expected: all pass.

- [ ] **Step 7: Typecheck**

```bash
pnpm typecheck
```

Expected: 0 errors.

- [ ] **Step 8: Commit backend**

```bash
git add server/db/permissionPresetsRepo.ts server/db/permissionPresetsRepo.test.ts server/routes/presetRoutes.ts server/index.ts
git commit -m "feat(C2): permission preset list + reset endpoints"
```

---

## Task 8: C2 — Permission preset reset (frontend)

**Files:**
- Modify: `src/components/ApiKeySettings.vue` (add nav item + section panel)

- [ ] **Step 1: Add the permissionPresets section to the script**

In `src/components/ApiKeySettings.vue`, in the `<script setup>` block:

1. Add `'permissionPresets'` to the `Section` type:

```ts
// BEFORE:
type Section = 'appearance' | 'apiKeys' | 'remotes'

// AFTER:
type Section = 'appearance' | 'apiKeys' | 'remotes' | 'permissionPresets'
```

2. Add preset state and functions after the `revokeKey` block (around line 103):

```ts
// --- Permission Presets ---
interface PresetSummary { cwd: string, count: number }
const presets = ref<PresetSummary[]>([])
const presetsLoading = ref(false)
const presetsError = ref('')
const confirmResetCwd = ref<string | null>(null)

async function loadPresets() {
  presetsLoading.value = true
  presetsError.value = ''
  try {
    const res = await fetch('/api/settings/permission-presets')
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    presets.value = await res.json()
  }
  catch (e) {
    presetsError.value = (e as Error).message
  }
  finally {
    presetsLoading.value = false
  }
}

async function resetPresets(cwd: string) {
  confirmResetCwd.value = null
  try {
    const res = await fetch('/api/settings/permission-presets', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ cwd }),
    })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    presets.value = presets.value.filter(p => p.cwd !== cwd)
  }
  catch (e) {
    presetsError.value = (e as Error).message
  }
}
```

3. Update the `watch` on `props.open` to also load presets when switching to that section:

```ts
// BEFORE:
watch(() => props.open, (val) => {
  if (val)
    loadKeys()
})

// AFTER:
watch(() => props.open, (val) => {
  if (val)
    loadKeys()
})

watch(activeSection, (val) => {
  if (val === 'permissionPresets')
    loadPresets()
})
```

- [ ] **Step 2: Add the nav item in the template**

In the template's `<ul>` in the sidebar, after the `apiKeys` `<li>` (around line 220), add:

```html
<li>
  <button
    type="button"
    class="w-full flex items-center gap-2 px-2.5 py-1.5 rounded-md border-none font-sans text-[13px] cursor-pointer text-left transition-colors"
    :class="activeSection === 'permissionPresets'
      ? 'bg-slate-200 dark:bg-slate-800 text-slate-900 dark:text-slate-100 font-semibold'
      : 'bg-transparent text-slate-500 dark:text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800/50 hover:text-slate-900 dark:hover:text-slate-100'"
    @click="activeSection = 'permissionPresets'"
  >
    <span class="text-sm flex-shrink-0">⚙</span> Berechtigungen
  </button>
</li>
```

- [ ] **Step 3: Add the content section in the template**

In the content area (after the `apiKeys` `<section>` block and before `</div>`), add:

```html
<!-- Permission Presets -->
<section v-if="activeSection === 'permissionPresets'">
  <h3 class="text-[17px] font-bold text-slate-900 dark:text-slate-100 mb-1">
    Permission Presets
  </h3>
  <p class="text-xs text-slate-400 dark:text-slate-600 mb-5">
    Saved tool permissions per project. Resetting a project removes all auto-applied grants for future tasks.
  </p>
  <p v-if="presetsError" class="text-xs text-red-500 mb-3">
    {{ presetsError }}
  </p>
  <p v-else-if="presetsLoading" class="text-xs text-slate-400 dark:text-slate-600">
    Loading…
  </p>
  <p v-else-if="presets.length === 0" class="text-xs text-slate-400 dark:text-slate-600">
    No presets saved yet.
  </p>
  <ul v-else class="list-none p-0 m-0 flex flex-col gap-2">
    <li
      v-for="p in presets"
      :key="p.cwd"
      class="flex items-center justify-between gap-3 px-3 py-2.5 rounded-lg bg-slate-50 dark:bg-slate-800/50 border border-slate-200 dark:border-slate-700"
    >
      <div class="min-w-0">
        <p class="text-[13px] font-medium text-slate-900 dark:text-slate-100 truncate">
          {{ p.cwd }}
        </p>
        <p class="text-[11px] text-slate-400 dark:text-slate-600 mt-0.5">
          {{ p.count }} preset{{ p.count === 1 ? '' : 's' }}
        </p>
      </div>
      <div v-if="confirmResetCwd === p.cwd" class="flex gap-2 flex-shrink-0">
        <AppButton size="sm" variant="danger" @click="resetPresets(p.cwd)">Confirm</AppButton>
        <AppButton size="sm" variant="ghost" @click="confirmResetCwd = null">Cancel</AppButton>
      </div>
      <AppButton v-else size="sm" variant="ghost" @click="confirmResetCwd = p.cwd">
        Reset
      </AppButton>
    </li>
  </ul>
</section>
```

- [ ] **Step 4: Typecheck**

```bash
pnpm typecheck
```

Expected: 0 errors.

- [ ] **Step 5: Commit frontend**

```bash
git add src/components/ApiKeySettings.vue
git commit -m "feat(C2): permission preset reset UI — list projects and reset per-project"
```

---

## Task 9: C3 — settings.json isolation

**Files:**
- Modify: `server/pipeline/agentSpawner.ts` (`writeSettingsFile`, `spawnStageAgent`, `SpawnResult`)

- [ ] **Step 1: Write test for the managed-file marker**

In `server/pipeline/agentSpawner.test.ts`, add a new describe block testing the `writeSettingsFile`-and-cleanup behavior. Since `writeSettingsFile` is not currently exported, we test via `spawnStageAgent`'s returned `settingsPath`. However, since `spawnStageAgent` calls real `spawn`, we test the new `shouldCleanSettingsFile` helper directly. We'll export it.

Add at the end of `agentSpawner.test.ts`:

```ts
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { shouldCleanSettingsFile, writeIsolatedSettings } from './agentSpawner.js'

describe('writeIsolatedSettings + shouldCleanSettingsFile', () => {
  let dir: string

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), 'agent-settings-test-'))
  })

  afterEach(() => {
    rmSync(dir, { recursive: true, force: true })
  })

  it('writes _dashboardManaged marker when no settings.json exists', () => {
    const path = writeIsolatedSettings(dir, ['Read'])
    const content = JSON.parse(readFileSync(path!, 'utf8'))
    expect(content._dashboardManaged).toBe(true)
    expect(content.permissions.allow).toContain('Read')
  })

  it('does not overwrite an existing user-authored settings.json', () => {
    mkdirSync(join(dir, '.claude'), { recursive: true })
    writeFileSync(join(dir, '.claude', 'settings.json'), JSON.stringify({ _userOwned: true }))
    const path = writeIsolatedSettings(dir, ['Read'])
    // Should not overwrite: reads existing, returns path but didn't clobber
    const content = JSON.parse(readFileSync(join(dir, '.claude', 'settings.json'), 'utf8'))
    expect(content._userOwned).toBe(true)
    // path is null since we didn't write
    expect(path).toBeNull()
  })

  it('shouldCleanSettingsFile returns true only for dashboard-managed files', () => {
    const path = writeIsolatedSettings(dir, ['Read'])!
    expect(shouldCleanSettingsFile(path)).toBe(true)
    // A user-authored file is not cleaned
    const userPath = join(dir, '.claude', 'user.json')
    writeFileSync(userPath, JSON.stringify({ foo: 1 }))
    expect(shouldCleanSettingsFile(userPath)).toBe(false)
  })
})
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
pnpm test server/pipeline/agentSpawner.test.ts
```

Expected: FAIL — `writeIsolatedSettings` and `shouldCleanSettingsFile` not exported yet.

- [ ] **Step 3: Refactor writeSettingsFile into writeIsolatedSettings**

In `server/pipeline/agentSpawner.ts`, replace the private `writeSettingsFile` function with an exported `writeIsolatedSettings` that checks for an existing user-authored file:

```ts
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs'

/**
 * Write dashboard-managed permissions to .claude/settings.json ONLY if the
 * file does not already exist (indicating it belongs to the user).
 * Stamps a _dashboardManaged: true marker so `shouldCleanSettingsFile` can
 * identify files we created and clean them up after the run.
 *
 * Returns the settings file path if we wrote it, null if skipped (pre-existing).
 */
export function writeIsolatedSettings(
  cwd: string,
  allow: string[],
): string | null {
  if (allow.length === 0)
    return null

  const settingsDir = join(cwd, '.claude')
  const settingsPath = join(settingsDir, 'settings.json')

  if (existsSync(settingsPath))
    return null

  mkdirSync(settingsDir, { recursive: true })
  const settings = { _dashboardManaged: true, permissions: { allow } }
  writeFileSync(settingsPath, JSON.stringify(settings, null, 2))
  return settingsPath
}

/**
 * Returns true if the settings file at `path` was written by the dashboard
 * (has `_dashboardManaged: true`). Safe to call even if the file no longer
 * exists (returns false).
 */
export function shouldCleanSettingsFile(path: string): boolean {
  try {
    const content = JSON.parse(readFileSync(path, 'utf8')) as Record<string, unknown>
    return content._dashboardManaged === true
  }
  catch {
    return false
  }
}
```

Then update `spawnStageAgent` to call `writeIsolatedSettings` instead of the old private `writeSettingsFile`:

```ts
// BEFORE:
const settingsPath = writeSettingsFile(cwd, opts.permissions, enableChannel)

// AFTER:
const allow = buildAllowList(opts.permissions, enableChannel)
const settingsPath = writeIsolatedSettings(cwd, allow)
```

Also add `cleanup` to `SpawnResult` so the orchestrator can clean up after transition:

```ts
// Add to SpawnResult interface:
export interface SpawnResult {
  child: ChildProcess
  pid: number
  cwd: string
  settingsPath: string | null
  cleanup: () => void   // <-- new
}

// In spawnStageAgent return:
return {
  child,
  pid: child.pid ?? 0,
  cwd,
  settingsPath,
  cleanup: () => {
    if (settingsPath && shouldCleanSettingsFile(settingsPath)) {
      try { unlinkSync(settingsPath) } catch {}
    }
  },
}
```

Add `unlinkSync` import:

```ts
import { existsSync, mkdirSync, readFileSync, unlinkSync, writeFileSync } from 'node:fs'
```

- [ ] **Step 4: Call cleanup in orchestrator after a completed stage run**

In `server/pipeline/orchestrator.ts`, in `finalizeCompletedAsyncRuns`, after `this.applyTransition(task, fresh, transition)`:

The `SpawnResult.cleanup()` needs to be called, but the orchestrator doesn't have a direct reference to the `SpawnResult` — the handler returned `async_running` with just a PID. We need a different approach.

**Simpler solution:** Call cleanup directly from `applyTransition` using the settingsPath stored in the stage_run's output field. The stage_run's output already gets `settingsPath` if we add it to the `async_running` output.

Actually, the simplest correct approach is to look up the path from the cwd and clean up if it exists and has the marker. Update `applyTransition`'s `async_running` case is not the right place — we want cleanup on transition OUT of `running`.

Instead, in `finalizeCompletedAsyncRuns`, after calling `this.applyTransition`, add a cleanup call:

```ts
// After each applyTransition call in finalizeCompletedAsyncRuns that moves
// the run out of 'running', clean up the managed settings file:
if (result.kind !== 'still_running') {
  const settingsPath = join(cwd, '.claude', 'settings.json')
  if (shouldCleanSettingsFile(settingsPath)) {
    try { unlinkSync(settingsPath) } catch {}
  }
}
```

Add the needed imports to `orchestrator.ts`. Check existing imports first with `grep "from 'node:" server/pipeline/orchestrator.ts` — only `node:process` is currently imported. Add:

```ts
import { unlinkSync } from 'node:fs'
import { join } from 'node:path'
import { shouldCleanSettingsFile } from './agentSpawner.js'
```

Note: `shouldCleanSettingsFile` is in `agentSpawner.ts` which is already imported by `stageHandlers.ts` (same pipeline layer) — importing it from orchestrator is allowed per layering rules.

- [ ] **Step 5: Run tests**

```bash
pnpm test server/pipeline/agentSpawner.test.ts
```

Expected: all pass including new writeIsolatedSettings tests.

- [ ] **Step 6: Full test suite + typecheck**

```bash
pnpm test && pnpm typecheck
```

Expected: all pass, 0 errors.

- [ ] **Step 7: Commit**

```bash
git add server/pipeline/agentSpawner.ts server/pipeline/agentSpawner.test.ts server/pipeline/orchestrator.ts
git commit -m "fix(C3): isolate settings.json writes — only create if absent, clean up after run"
```

---

## Final: Full validation

- [ ] **Run full test suite**

```bash
pnpm test
```

Expected: all tests pass.

- [ ] **Typecheck**

```bash
pnpm typecheck
```

Expected: 0 errors.

- [ ] **Lint**

```bash
pnpm lint
```

Expected: 0 errors.
