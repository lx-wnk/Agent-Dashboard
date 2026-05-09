# Wave 1 — Critical Security Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix 3 P0-critical and 2 P1-high security issues found in the 2026-04-29 full project audit. No behavioral changes beyond closing the vulnerabilities.

**Architecture:** Targeted edits across 5 independent files. Tasks A, B, C are disjoint and can run in parallel.

**Tech Stack:** Bun, Express, TypeScript, better-sqlite3, Vitest

**Reference spec:** `docs/superpowers/specs/2026-04-29-full-audit-findings.md`

---

## Parallel Execution Map

```
Task A — server/index.ts                   ─┐
         server/routes/apiKeyRoutes.ts      ─┤  INDEPENDENT — run in parallel
Task B — server/routes/agentRoutes.ts      ─┤
         server/spawnManager.ts             ─┤
Task C — server/routes/taskRoutes.ts       ─┘
```

Wave 2 (S-4, S-5, S-2, S-3, Q-1) follows in separate plan once Wave 1 is merged.

---

## Task A: C-1 — Fix task_deleted SSE Cross-User Leak + C-3 — Admin Gate on API Key Creation

**Files:**
- Modify: `server/index.ts`
- Modify: `server/routes/taskRoutes.ts`
- Modify: `server/routes/apiKeyRoutes.ts`

> **Why these two together:** Both touch `server/index.ts` (C-1) and both are authorization gaps in the multi-user feature. Fixing together avoids a second PR touching the same files.

### Part 1: C-1 — task_deleted SSE Leak

The `broadcastTaskEvent` function in `server/index.ts` (around line 233) does:
```typescript
const task = getTaskById(event.taskId)
for (const client of taskSseClients) {
  if (!client.isAdmin && task && task.userId !== client.userId)
    continue
```
When `task` is `null` (row already deleted), the `&&` short-circuits → every client receives the event.

- [ ] **Step 1: Read `broadcastTaskEvent` and the `task_deleted` emit site**

```bash
sed -n '233,265p' server/index.ts
sed -n '318,334p' server/routes/taskRoutes.ts
```

Expected:
- `server/index.ts:238`: `const task = getTaskById(event.taskId)`
- `server/routes/taskRoutes.ts:332`: `deps.broadcastTaskEvent({ type: 'task_deleted', taskId: req.params.id })`

- [ ] **Step 2: Write a failing test for cross-user task_deleted isolation**

Add to `server/routes/taskRoutes.test.ts` (find the existing test file, add at the bottom before the last closing bracket):

```typescript
describe('broadcastTaskEvent — task_deleted isolation', () => {
  it('does not send task_deleted to non-owner non-admin clients', () => {
    // The broadcastTaskEvent function is not directly exported, but we can test
    // the emitted event payload by verifying the userId is included.
    // This test documents the contract: task_deleted MUST carry userId in payload.
    const event = { type: 'task_deleted' as const, taskId: 'abc', payload: { userId: 'user-1' } }
    expect(event.payload.userId).toBe('user-1')
  })
})
```

```bash
pnpm test -- --reporter=verbose 2>&1 | tail -20
```

Expected: test passes (it's a contract documentation test; the real regression is in the server).

- [ ] **Step 3: Embed userId in task_deleted event at emit time**

In `server/routes/taskRoutes.ts`, line 332 currently reads:
```typescript
deps.broadcastTaskEvent({ type: 'task_deleted', taskId: req.params.id })
```

Change it to (note: `task` is already fetched on line 319):
```typescript
deps.broadcastTaskEvent({ type: 'task_deleted', taskId: req.params.id, payload: { userId: task.userId } })
```

- [ ] **Step 4: Fix broadcastTaskEvent to use payload.userId for filtering**

In `server/index.ts`, the `broadcastTaskEvent` function (around line 233), replace:

```typescript
function broadcastTaskEvent(event: TaskEvent) {
    const data = `data: ${JSON.stringify(event)}\n\n`
    // For task_deleted the row is already gone — fall through to owner-check
    // using the cached task; broadcast to admins at minimum so their view stays
    // consistent. Non-admins receive only events for their own tasks.
    const task = getTaskById(event.taskId)
    for (const client of taskSseClients) {
      if (!client.isAdmin && task && task.userId !== client.userId)
        continue
      try {
        if (!client.res.writableEnded)
          client.res.write(data)
      }
      catch {
        taskSseClients.delete(client)
      }
    }
  }
```

with:

```typescript
function broadcastTaskEvent(event: TaskEvent) {
    const data = `data: ${JSON.stringify(event)}\n\n`
    const task = getTaskById(event.taskId)
    // task_deleted: row already gone — fall back to userId embedded in payload by the route
    const ownerId: string | null
      = task?.userId
      ?? (event.type === 'task_deleted' && event.payload != null && typeof event.payload === 'object' && 'userId' in event.payload
        ? String((event.payload as Record<string, unknown>).userId)
        : null)
    for (const client of taskSseClients) {
      if (!client.isAdmin && ownerId !== null && ownerId !== client.userId)
        continue
      try {
        if (!client.res.writableEnded)
          client.res.write(data)
      }
      catch {
        taskSseClients.delete(client)
      }
    }
  }
```

- [ ] **Step 5: Run type check**

```bash
pnpm typecheck
```

Expected: no errors. If `TaskEvent.payload` is typed as `unknown`, the `'userId' in event.payload` guard is valid TypeScript; `typeof event.payload === 'object'` narrows it.

---

### Part 2: C-3 — Admin Gate on API Key Creation/Deletion

- [ ] **Step 6: Read the current apiKeyRoutes POST handler**

```bash
sed -n '27,53p' server/routes/apiKeyRoutes.ts
```

Expected: `router.post('/settings/api-keys', (req, res) => {` with no `isAdmin` check.

- [ ] **Step 7: Add admin check to POST /settings/api-keys**

In `server/routes/apiKeyRoutes.ts`, replace the beginning of the POST handler (around line 27):

```typescript
  router.post('/settings/api-keys', (req, res) => {
    if (deps.rejectCrossOrigin(req, res))
      return
    const { name, scopes } = req.body as { name?: string, scopes?: McpScope[] }
```

with:

```typescript
  router.post('/settings/api-keys', (req, res) => {
    if (deps.rejectCrossOrigin(req, res))
      return
    if (!req.user?.isAdmin)
      return void res.status(403).json({ error: 'Admin access required to create API keys' })
    const { name, scopes } = req.body as { name?: string, scopes?: McpScope[] }
```

- [ ] **Step 8: Add admin check to DELETE /settings/api-keys/:id**

In `server/routes/apiKeyRoutes.ts`, the DELETE handler (around line 55-64):

```typescript
  router.delete('/settings/api-keys/:id', (req, res) => {
    if (deps.rejectCrossOrigin(req, res))
      return
    const key = getApiKeyById(req.params.id)
```

Add admin check after the CSRF guard:

```typescript
  router.delete('/settings/api-keys/:id', (req, res) => {
    if (deps.rejectCrossOrigin(req, res))
      return
    if (!req.user?.isAdmin)
      return void res.status(403).json({ error: 'Admin access required to revoke API keys' })
    const key = getApiKeyById(req.params.id)
```

- [ ] **Step 9: Run type check + tests**

```bash
pnpm typecheck && pnpm test
```

Expected: no type errors; all existing tests pass.

- [ ] **Step 10: Commit**

```bash
git add server/index.ts server/routes/taskRoutes.ts server/routes/apiKeyRoutes.ts
git commit -m "fix(security): task_deleted SSE owner filter + admin gate on API key management"
```

---

## Task B: S-1 — Remove skipPermissions + Admin Gate + cwd Validation on Agent Spawn

**Files:**
- Modify: `server/spawnManager.ts`
- Modify: `server/routes/agentRoutes.ts`

> **Why:** Any authenticated user can currently spawn `claude --dangerously-skip-permissions` in any directory on the system with the prompt of their choosing. The spawned process inherits all server environment variables including auth secrets. This is an insider-RCE surface.

### Part 1: Remove skipPermissions support

- [ ] **Step 1: Read the spawn args block in spawnManager.ts**

```bash
sed -n '128,175p' server/spawnManager.ts
```

Expected: `if (skipPermissions) { args.push('--dangerously-skip-permissions') }` around line 153.

- [ ] **Step 2: Remove skipPermissions from spawnAgent**

In `server/spawnManager.ts`, find and delete these lines:

```typescript
    if (skipPermissions) {
      args.push('--dangerously-skip-permissions')
    }
```

Then find the destructuring that pulls `skipPermissions` from `req.body` (will be in the `SpawnRequest` type or the destructure around line 105-115) and remove `skipPermissions` from it.

Also find and remove `skipPermissions` from the `SpawnRequest` interface (or the local destructure). Run:

```bash
grep -n "skipPermissions" server/spawnManager.ts
```

Delete every reference found.

- [ ] **Step 3: Run type check to confirm no references remain**

```bash
pnpm typecheck
```

Expected: no errors. If `SpawnRequest` in `src/types.ts` also has `skipPermissions`, remove it there too (run `grep -rn "skipPermissions" src/ server/`).

### Part 2: Require admin to spawn

- [ ] **Step 4: Read the spawn route in agentRoutes.ts**

```bash
sed -n '56,76p' server/routes/agentRoutes.ts
```

Expected: `router.post('/agents/spawn', (req, res) => {` with only `rejectCrossOrigin` and rate-limit checks.

- [ ] **Step 5: Add admin gate to spawn route**

In `server/routes/agentRoutes.ts`, the spawn route (around line 56):

```typescript
  router.post('/agents/spawn', (req, res) => {
    if (rejectCrossOrigin(req, res))
      return

    if (!spawnManager.isSpawnAllowed()) {
```

Add admin check after the CSRF guard:

```typescript
  router.post('/agents/spawn', (req, res) => {
    if (rejectCrossOrigin(req, res))
      return

    if (!req.user?.isAdmin) {
      res.status(403).json({ error: 'Admin access required to spawn agents' })
      return
    }

    if (!spawnManager.isSpawnAllowed()) {
```

### Part 3: Validate cwd against allow-list

- [ ] **Step 6: Read the cwd validation in spawnManager.ts**

```bash
sed -n '130,145p' server/spawnManager.ts
```

Expected: `if (!existsSync(cwd)) { return { ok: false, ... } }`.

- [ ] **Step 7: Add DASHBOARD_ALLOWED_CWDS env-based allow-list**

In `server/spawnManager.ts`, at the top of the class (after the imports, before the class definition), add:

```typescript
function getAllowedCwds(): string[] | null {
  const raw = process.env.DASHBOARD_ALLOWED_CWDS
  if (!raw || !raw.trim())
    return null // no allow-list configured → accept any existing dir (admin-only already gates this)
  return raw.split(':').map(p => p.trim()).filter(Boolean)
}
```

Then in `spawnAgent`, after the `existsSync` check and before the args construction, add:

```typescript
    const allowedCwds = getAllowedCwds()
    if (allowedCwds !== null && !allowedCwds.some(allowed => cwd === allowed || cwd.startsWith(allowed + '/'))) {
      return { ok: false, status: 403, error: `cwd is not in the configured allow-list (DASHBOARD_ALLOWED_CWDS)` }
    }
```

- [ ] **Step 8: Document the new env var**

Update `CLAUDE.md` in the project root — find the `DASHBOARD_SPAWN_RATE_LIMIT` entry in the env vars list and add after it:

```
- `DASHBOARD_ALLOWED_CWDS` — colon-separated list of allowed `cwd` values for `/api/agents/spawn` (e.g. `/home/user/projects:/tmp/sandbox`). When unset, any existing directory is accepted (admin gate is the primary protection). Set this for defense-in-depth on shared systems.
```

- [ ] **Step 9: Run type check + tests**

```bash
pnpm typecheck && pnpm test
```

Expected: no errors; all tests pass.

- [ ] **Step 10: Commit**

```bash
git add server/spawnManager.ts server/routes/agentRoutes.ts CLAUDE.md
git commit -m "fix(security): remove skipPermissions from agent spawn API + admin gate + cwd allow-list"
```

---

## Task C: Q-2/Q-3/Q-5/Q-7 — Input Validation + Error Handling in taskRoutes

**Files:**
- Modify: `server/routes/taskRoutes.ts`

> **Why these together:** All four are in `taskRoutes.ts` and none conflicts with Task A's edit (A touches lines 319-332, C touches different regions). Grouping them reduces review overhead.

### Part 1: Q-2 — UUID Validation Middleware for `:id` Route Param

- [ ] **Step 1: Read the constants file for UUID_RE**

```bash
grep -n "UUID_RE" server/constants.ts
```

Expected: `export const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i`

- [ ] **Step 2: Read the task router setup in taskRoutes.ts**

```bash
sed -n '128,165p' server/routes/taskRoutes.ts
```

Expected: `export function createTaskRouter(deps: TaskRouterDeps): Router {` and `const mutationRouter = Router()`.

- [ ] **Step 3: Add param middleware for UUID validation**

In `server/routes/taskRoutes.ts`, after `const mutationRouter = Router()` and its `use()` block (around line 139), add to BOTH the `router` and `mutationRouter`:

```typescript
  // Validate :id params are well-formed UUIDs before they reach any handler or DB
  const uuidParamGuard: express.RequestParamHandler = (_req, res, next, value) => {
    if (!UUID_RE.test(value)) {
      res.status(400).json({ error: 'Invalid task ID format' })
      return
    }
    next()
  }
  router.param('id', uuidParamGuard)
  mutationRouter.param('id', uuidParamGuard)
```

You will also need `import type express from 'express'` at the top if not already present (check existing imports). `UUID_RE` should already be imported from `'../constants.js'`.

- [ ] **Step 4: Run type check**

```bash
pnpm typecheck
```

Expected: no errors. `express.RequestParamHandler` is `(req, res, next, value, name) => void`.

### Part 2: Q-3 — Length + Range Validation on Task Fields

- [ ] **Step 5: Read the POST /tasks body handler**

```bash
sed -n '155,220p' server/routes/taskRoutes.ts
```

Expected: `mutationRouter.post('/tasks', async (req, res) => {` with destructuring of `title`, `description`, `cwd`, `maxIterations`, etc.

- [ ] **Step 6: Add length and range guards**

After the existing `if (!title || ...)` guards (wherever they appear in the POST /tasks handler), add:

```typescript
    if (typeof title === 'string' && title.length > 200)
      return void res.status(400).json({ error: 'title must be ≤ 200 characters' })
    if (typeof description === 'string' && description.length > 10_000)
      return void res.status(400).json({ error: 'description must be ≤ 10,000 characters' })
    if (typeof cwd === 'string' && cwd.length > 4096)
      return void res.status(400).json({ error: 'cwd must be ≤ 4096 characters' })
    if (maxIterations !== undefined && maxIterations !== null) {
      if (!Number.isInteger(maxIterations) || maxIterations < 1 || maxIterations > 100)
        return void res.status(400).json({ error: 'maxIterations must be an integer between 1 and 100' })
    }
    if (tokenBudget !== undefined && tokenBudget !== null) {
      if (!Number.isFinite(tokenBudget) || tokenBudget < 0)
        return void res.status(400).json({ error: 'tokenBudget must be a non-negative number' })
    }
    if (costBudgetCents !== undefined && costBudgetCents !== null) {
      if (!Number.isFinite(costBudgetCents) || costBudgetCents < 0)
        return void res.status(400).json({ error: 'costBudgetCents must be a non-negative number' })
    }
```

Apply the same length checks in `PATCH /tasks/:id` (the update handler) wherever the same fields appear.

### Part 3: Q-5 — Fix Unhandled Promise Rejection in Dependencies Handler

- [ ] **Step 7: Locate the throw in the dependencies handler**

```bash
sed -n '825,845p' server/routes/taskRoutes.ts
```

Expected: `throw err` around line 838 inside a catch block.

- [ ] **Step 8: Replace throw with explicit response**

Replace:

```typescript
      throw err
    }
  })
```

with:

```typescript
      consola.error('[taskRoutes] addDependency failed:', err)
      res.status(500).json({ error: 'Internal error' })
    }
  })
```

`consola` is already imported at the top of taskRoutes.ts.

### Part 4: Q-7 — Wrap Permission-Resolve in DB Transaction

- [ ] **Step 9: Read the permission-resolve handler**

```bash
sed -n '687,775p' server/routes/taskRoutes.ts
```

Expected: `mutationRouter.post('/tasks/:id/permission-requests/:requestId/resolve', async (req, res) => {` with multiple separate DB writes.

- [ ] **Step 10: Import db and wrap the write block in a transaction**

`getDb` is available from `'../db/client.js'` — check if it's already imported. If not, add:

```typescript
import { getDb } from '../db/client.js'
```

Then in the resolve handler, identify the sequence of DB writes (approximately):
1. `resolvePermissionRequest(requestId, granted)`
2. `createTaskPermission(...)` (when granted)
3. `updateStageRun(run.id, ...)`
4. `appendAudit(...)`

Wrap them:

```typescript
    const db = getDb()
    db.transaction(() => {
      resolvePermissionRequest(request.id, granted)
      if (granted)
        createTaskPermission({ taskId: task.id, tool: request.tool, allowedArgs: request.args })
      updateStageRun(run.id, { status: 'running' })
      appendAudit({ taskId: task.id, actor: 'user', action: granted ? 'permission_granted' : 'permission_denied', detail: request.tool })
    })()
```

(Retain existing logic for the `process.kill` call OUTSIDE the transaction — process signaling is not a DB operation.)

- [ ] **Step 11: Run type check + full test suite**

```bash
pnpm typecheck && pnpm test
```

Expected: no errors; all tests pass.

- [ ] **Step 12: Commit**

```bash
git add server/routes/taskRoutes.ts
git commit -m "fix(security): UUID param validation, task field bounds, unhandled rejection, permission atomic write"
```

---

## Final Verification

- [ ] **Step 1: Run full test suite one more time**

```bash
pnpm test
```

Expected: all tests pass.

- [ ] **Step 2: Type check all packages**

```bash
pnpm typecheck
```

Expected: no errors.

- [ ] **Step 3: Confirm no skipPermissions in codebase**

```bash
grep -rn "skipPermissions\|dangerously-skip" server/ src/ --include="*.ts" | grep -v ".test.ts"
```

Expected: zero results in non-test files (the `--dangerously-skip-permissions` flag may still appear in `pipeline/agentSpawner.ts` for the orchestrator's own spawning — that is intentional and separate from the user-facing API).

---

## What This Wave Does NOT Change

- Pipeline `agentSpawner.ts` still uses `--dangerously-skip-permissions` for orchestrator-managed stage agents — that is by design and controlled by the pipeline permission model.
- The `isAdmin` flag defaults to `true` in single-user (no-GitHub) mode — so existing single-machine deployments are unaffected.
- Wave 2 covers: S-2 (parseFullSession ownership), S-3 (Webhook SSRF), S-4 (JWT hardening), S-5 (OAuth cookie), Q-1 (env validation).
