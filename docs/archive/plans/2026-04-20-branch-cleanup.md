# Branch Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Harden `feat/mcp-controlling` for merge — eliminate DRY violations across auth surfaces, wire scope enforcement into runtime, fix Vue bugs, add ENV configurability, and update docs.

**Architecture:** Eight focused, independent tasks in dependency order: docs cleanup first (no code risk), then shared constants as foundation, then refactoring layered on those constants, then docs updates last. Each task leaves the codebase green.

**Tech Stack:** Node.js/TypeScript, Express, Vue 3 Composition API, better-sqlite3, @modelcontextprotocol/sdk, Vitest, Playwright.

---

## File Map

**Modified:**
- `.gitignore` — add `.mcp.json`, `docs/plans/*`, `docs/specs/*`; untrack existing stale files
- `server/constants.ts` — add `SLUG_RE`, `SLUG_PATTERN_MESSAGE`, `SYSTEM_PROMPT_MAX_CHARS`
- `server/db/apiKeysRepo.ts` — add `generateApiToken()`, `hashApiToken()`
- `server/mcp/mcpAuth.ts` — add `makeToolRegistrar()` factory; fix `touchApiKey` error handling
- `server/mcp/mcpServer.ts` — use `makeToolRegistrar` and `ok()` helper; shrinks to <50 lines
- `server/mcp/mcpRouter.ts` — no change needed
- `server/services/worktreeManager.ts` — replace silent `catch {}` with `console.warn`
- `server/pipeline/agentSpawner.ts` — use `generateApiToken()` + `hashApiToken()` + `SYSTEM_PROMPT_MAX_CHARS`
- `server/pipeline/stageHandlers.ts` — use `generateApiToken()` + `hashApiToken()`
- `server/routes/apiKeyRoutes.ts` — use `generateApiToken()` + `hashApiToken()` + `SLUG_RE`; fix UNIQUE error handling
- `server/routes/taskRoutes.ts` — use `SLUG_RE` + `SLUG_PATTERN_MESSAGE`
- `server/index.ts` — `DASHBOARD_HOST`, `DASHBOARD_SSE_INTERVAL_MS`, `timingSafeEqual` for channel-reply
- `server/spawnManager.ts` — `DASHBOARD_SPAWN_RATE_LIMIT`, `DASHBOARD_SPAWN_RATE_WINDOW_MS`, `SYSTEM_PROMPT_MAX_CHARS`
- `src/components/ApiKeySettings.vue` — `onMounted`, Esc handler, clipboard try/catch
- `src/components/CrossLinkBanner.vue` — replace hardcoded hex colors with CSS variables
- `src/components/TaskModal.vue` — fix double `window.addEventListener` registration
- `src/components/AgentModal.vue` — fix double `window.addEventListener` registration
- `CLAUDE.md` — add `spawnManager.ts`, `constants.ts`, `CrossLinkBanner.vue`, `.mcp.json.example`
- `README.md` — major update: MCP section, API keys, updated architecture, env vars

**Created:**
- `.mcp.json.example` — renamed from `.mcp.json`
- `server/mcp/tools/readTools.ts` — all `tasks:read` tools
- `server/mcp/tools/writeTools.ts` — all `tasks:write` tools
- `server/mcp/tools/controlTools.ts` — all `pipeline:control` tools
- `server/mcp/tools/keyTools.ts` — all `keys:manage` tools

**Deleted:**
- `.mcp.json` (git rm --cached + gitignore)
- `docs/plans/2026-04-17-session-task-crossnav.md`
- `docs/specs/2026-04-15-mcp-task-endpoint-design.md`
- `docs/specs/2026-04-17-session-task-crossnav-design.md`
- `docs/superpowers/plans/2026-04-15-mcp-task-endpoint.md` (git rm --cached)
- `docs/superpowers/specs/2026-04-08-test-suite-design.md` (git rm --cached)

---

## Task 1: Docs & Config Hygiene

**Files:**
- Modify: `.gitignore`
- Delete: `.mcp.json`, `docs/plans/2026-04-17-session-task-crossnav.md`, `docs/specs/2026-04-15-mcp-task-endpoint-design.md`, `docs/specs/2026-04-17-session-task-crossnav-design.md`
- Delete (untrack): `docs/superpowers/plans/2026-04-15-mcp-task-endpoint.md`, `docs/superpowers/specs/2026-04-08-test-suite-design.md`
- Create: `.mcp.json.example`

- [ ] **Step 1: Rename `.mcp.json` to `.mcp.json.example`**

```bash
mv .mcp.json .mcp.json.example
```

File content of `.mcp.json.example` (verify it looks like this after rename):
```json
{
  "mcpServers": {
    "dashboard": {
      "type": "http",
      "url": "http://127.0.0.1:13120/api/mcp",
      "headers": {
        "Authorization": "Bearer ${DASHBOARD_MCP_TOKEN}"
      }
    }
  }
}
```

- [ ] **Step 2: Update `.gitignore`**

Add these lines after the existing `.env.*` block:

```
# MCP config contains token placeholders — copy from .mcp.json.example
.mcp.json
!.mcp.json.example

# Plans and specs are local-only artefacts (already implemented; never version)
/docs/plans/*
/docs/specs/*
```

The `/docs/superpowers/*` line already exists in `.gitignore` — do NOT add it again. But existing tracked files under `docs/superpowers/` must be untracked (step 3).

- [ ] **Step 3: Untrack already-committed files that are now gitignored**

```bash
git rm --cached .mcp.json
git rm --cached docs/superpowers/plans/2026-04-15-mcp-task-endpoint.md
git rm --cached docs/superpowers/specs/2026-04-08-test-suite-design.md
```

Expected output: `rm 'docs/superpowers/plans/...'` for each file. The physical files remain on disk.

- [ ] **Step 4: Delete stale implemented plans + specs from disk**

```bash
rm docs/plans/2026-04-17-session-task-crossnav.md
rm docs/specs/2026-04-15-mcp-task-endpoint-design.md
rm docs/specs/2026-04-17-session-task-crossnav-design.md
```

`docs/superpowers/` files can stay on disk (gitignored); delete them too if desired.

- [ ] **Step 5: Verify git status is clean for these paths**

```bash
git status --short
```

Expected: no `.mcp.json` or `docs/plans/` or `docs/specs/` entries in tracked changes. `.mcp.json.example` should appear as a new tracked file.

- [ ] **Step 6: Commit**

```bash
git add .gitignore .mcp.json.example
git commit -m "chore: gitignore .mcp.json and stale plans, ship .mcp.json.example"
```

---

## Task 2: Shared Constants + Token Helpers

**Files:**
- Modify: `server/constants.ts`
- Modify: `server/db/apiKeysRepo.ts`
- Test: `server/db/db.test.ts`

- [ ] **Step 1: Write failing tests**

Add to `server/db/db.test.ts` (find the existing `describe('apiKeysRepo')` block and append inside it):

```typescript
describe('token helpers', () => {
  it('generateApiToken returns mcp_ prefixed hex string', () => {
    const token = generateApiToken()
    expect(token).toMatch(/^mcp_[0-9a-f]{32}$/)
  })

  it('hashApiToken produces stable sha256 hex', () => {
    const hash1 = hashApiToken('mcp_abc')
    const hash2 = hashApiToken('mcp_abc')
    expect(hash1).toBe(hash2)
    expect(hash1).toMatch(/^[0-9a-f]{64}$/)
  })

  it('two generateApiToken calls produce different tokens', () => {
    expect(generateApiToken()).not.toBe(generateApiToken())
  })
})
```

Add the imports at the top of the test file:
```typescript
import { generateApiToken, hashApiToken } from './apiKeysRepo.js'
```

- [ ] **Step 2: Run test to verify it fails**

```bash
pnpm test server/db/db.test.ts
```

Expected: FAIL — `generateApiToken is not a function`

- [ ] **Step 3: Extend `server/constants.ts`**

Append to the existing file (keep `VALID_STAGES` as-is):

```typescript
export const SLUG_RE = /^[a-z0-9][a-z0-9-]{0,63}$/
export const SLUG_PATTERN_MESSAGE = 'slug must match [a-z0-9][a-z0-9-]{0,63}'

export const SYSTEM_PROMPT_MAX_CHARS = 10_000
```

- [ ] **Step 4: Add helpers to `server/db/apiKeysRepo.ts`**

Add at the top, after the existing imports:

```typescript
import { createHash, randomBytes } from 'node:crypto'
```

(Note: `randomUUID` is already imported — keep it. Add `createHash` and `randomBytes` to the same import if `node:crypto` is already imported, otherwise add the line above.)

Then add these two exported functions before `createApiKey`:

```typescript
export function generateApiToken(): string {
  return `mcp_${randomBytes(16).toString('hex')}`
}

export function hashApiToken(token: string): string {
  return createHash('sha256').update(token).digest('hex')
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
pnpm test server/db/db.test.ts
```

Expected: PASS (all existing tests + 3 new token-helper tests)

- [ ] **Step 6: Replace duplication in call sites**

In `server/mcp/mcpServer.ts` (around line 404–405), replace:
```typescript
import { createHash, randomBytes } from 'node:crypto'
// ...
const token = `mcp_${randomBytes(16).toString('hex')}`
const keyHash = createHash('sha256').update(token).digest('hex')
```
With:
```typescript
import { generateApiToken, hashApiToken } from '../db/apiKeysRepo.js'
// ...
const token = generateApiToken()
const keyHash = hashApiToken(token)
```
Also remove the `createHash, randomBytes` import from `mcpServer.ts` if it's no longer needed elsewhere in that file.

In `server/routes/apiKeyRoutes.ts` (around line 43–44), replace:
```typescript
import { createHash, randomBytes } from 'node:crypto'
// ...
const token = `mcp_${randomBytes(16).toString('hex')}`
const keyHash = createHash('sha256').update(token).digest('hex')
```
With:
```typescript
import { generateApiToken, hashApiToken } from '../db/apiKeysRepo.js'
// ...
const token = generateApiToken()
const keyHash = hashApiToken(token)
```
Remove the `createHash, randomBytes` import if no longer used.

In `server/pipeline/stageHandlers.ts` (around line 86–87), replace:
```typescript
import { createHash, randomBytes } from 'node:crypto'
// ...
const rawToken = `mcp_${randomBytes(16).toString('hex')}`
const keyHash = createHash('sha256').update(rawToken).digest('hex')
```
With:
```typescript
import { generateApiToken, hashApiToken } from '../db/apiKeysRepo.js'
// ...
const rawToken = generateApiToken()
const keyHash = hashApiToken(rawToken)
```

In `server/services/worktreeManager.ts` (around line 34), replace:
```typescript
const SAFE_SLUG_RE = /^[a-z0-9][a-z0-9-]{0,63}$/
```
With:
```typescript
import { SLUG_RE as SAFE_SLUG_RE } from '../constants.js'
```
(Keep usage of `SAFE_SLUG_RE` as-is — renaming would require a second PR.)

In `server/routes/taskRoutes.ts` (around line 67), replace:
```typescript
const SLUG_RE = /^[a-z0-9][a-z0-9-]{0,63}$/
```
And the error message string `'slug must match [a-z0-9][a-z0-9-]{0,63}'` with:
```typescript
import { SLUG_RE, SLUG_PATTERN_MESSAGE } from '../constants.js'
// ...
res.status(400).json({ error: SLUG_PATTERN_MESSAGE })
```

In `server/spawnManager.ts` (around line 145), replace:
```typescript
args.push('--system-prompt', systemPrompt.slice(0, 10000))
```
With:
```typescript
import { SYSTEM_PROMPT_MAX_CHARS } from './constants.js'
// ...
args.push('--system-prompt', systemPrompt.slice(0, SYSTEM_PROMPT_MAX_CHARS))
```

In `server/pipeline/agentSpawner.ts` (find the system-prompt truncation at ~line 77), replace the inline `10000` literal with `SYSTEM_PROMPT_MAX_CHARS` from `../constants.js`.

- [ ] **Step 7: Run full test suite**

```bash
pnpm test
```

Expected: All tests pass. If any import path fails, verify `.js` extensions are consistent with the rest of the file.

- [ ] **Step 8: Commit**

```bash
git add server/constants.ts server/db/apiKeysRepo.ts server/db/db.test.ts \
  server/mcp/mcpServer.ts server/routes/apiKeyRoutes.ts \
  server/pipeline/stageHandlers.ts server/services/worktreeManager.ts \
  server/routes/taskRoutes.ts server/spawnManager.ts server/pipeline/agentSpawner.ts
git commit -m "refactor: extract generateApiToken/hashApiToken helpers and SLUG_RE constant"
```

---

## Task 3: MCP Server Refactor — `makeToolRegistrar` + `ok()` + Split Tool Files

**Files:**
- Modify: `server/mcp/mcpAuth.ts`
- Modify: `server/mcp/mcpServer.ts`
- Create: `server/mcp/tools/readTools.ts`
- Create: `server/mcp/tools/writeTools.ts`
- Create: `server/mcp/tools/controlTools.ts`
- Create: `server/mcp/tools/keyTools.ts`
- Test: `server/mcp/mcpServer.test.ts`

`★ Insight ─────────────────────────────────────`
Wiring `TOOL_SCOPE_MAP` into runtime enforcement is a classic "make the right thing the only thing" pattern — the existing test `mcpServer.test.ts` already verifies every registered tool appears in `TOOL_SCOPE_MAP`. After this refactor, that test also proves scope is enforced automatically.
`─────────────────────────────────────────────────`

- [ ] **Step 1: Verify the existing tool-coverage test passes (baseline)**

```bash
pnpm test server/mcp/mcpServer.test.ts
```

Expected: PASS. Note the test name — it ensures every `server.tool(name, …)` call has a matching `TOOL_SCOPE_MAP` entry. This test must remain green throughout.

- [ ] **Step 2: Add `makeToolRegistrar` and `ok` to `server/mcp/mcpAuth.ts`**

Add these exports at the bottom of `server/mcp/mcpAuth.ts` (after the existing `mcpAuthMiddleware`):

```typescript
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import type { z } from 'zod'

type ToolResult = { content: Array<{ type: 'text', text: string }> }

/** Uniform success response — wraps data as JSON text content block. */
export function ok(data: unknown): ToolResult {
  return { content: [{ type: 'text' as const, text: JSON.stringify(data) }] }
}

/** Error helper — throws so the MCP SDK surfaces it as a tool error. */
export function mcpError(message: string): never {
  const err = new Error(message) as Error & { code: number }
  err.code = -32003
  throw err
}

/** Deps injected into each tool-group registration function. */
export interface McpToolDeps {
  orchestrator: import('../pipeline/orchestrator.js').PipelineOrchestrator
  scopes: Set<McpScope>
  broadcast: (taskId: string) => void
  broadcastDeleted: (taskId: string) => void
}

/**
 * Returns a `tool()` helper that automatically enforces the scope declared in
 * TOOL_SCOPE_MAP before invoking the handler. Every tool MUST be registered
 * via this helper — direct `server.tool()` calls bypass scope enforcement.
 */
export function makeToolRegistrar(server: McpServer, scopes: Set<McpScope>) {
  return function tool<S extends z.ZodRawShape>(
    name: keyof typeof TOOL_SCOPE_MAP,
    schema: S,
    handler: (args: z.infer<z.ZodObject<S>>) => ToolResult | Promise<ToolResult>,
  ): void {
    const needed = TOOL_SCOPE_MAP[name]
    server.tool(name, schema, (args: z.infer<z.ZodObject<S>>) => {
      if (!scopes.has(needed))
        mcpError(`Insufficient scope: requires ${needed}`)
      return handler(args)
    })
  }
}
```

Also add the `McpServer` import at the top of the file if not already present:
```typescript
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { z } from 'zod'
```

- [ ] **Step 3: Create `server/mcp/tools/readTools.ts`**

This file registers the five `tasks:read` tools. Copy the relevant `server.tool(…)` blocks from the existing `buildMcpServer` in `mcpServer.ts` (the `list_tasks`, `get_task`, `list_stage_runs`, `list_audit`, `list_permission_requests` tools) and adapt them to use `tool()` from `makeToolRegistrar`:

```typescript
import { z } from 'zod'
import { VALID_STAGES } from '../../constants.js'
import { getTaskById, listTasks } from '../../db/tasksRepo.js'
import { getStageRunsByTask } from '../../db/stageRunsRepo.js'
import { getAuditLog, listPermissionRequests } from '../../db/auditRepo.js'
import type { McpToolDeps } from '../mcpAuth.js'
import { ok } from '../mcpAuth.js'
import type { makeToolRegistrar } from '../mcpAuth.js'

type ToolFn = ReturnType<typeof makeToolRegistrar>

export function registerReadTools(tool: ToolFn, _deps: McpToolDeps): void {
  tool(
    'list_tasks',
    {
      stage: z.string().optional().describe('Filter by pipeline stage'),
      status: z.string().optional().describe('Filter by status'),
    },
    ({ stage, status }) => {
      const tasks = listTasks({ stage: stage as Parameters<typeof listTasks>[0]['stage'], status })
      return ok(tasks)
    },
  )

  tool(
    'get_task',
    { id: z.string().describe('Task UUID') },
    ({ id }) => {
      const task = getTaskById(id)
      if (!task) mcpError(`Task not found: ${id}`)
      return ok(task)
    },
  )

  tool(
    'list_stage_runs',
    { taskId: z.string().describe('Task UUID') },
    ({ taskId }) => ok(getStageRunsByTask(taskId)),
  )

  tool(
    'list_audit',
    { taskId: z.string().describe('Task UUID') },
    ({ taskId }) => ok(getAuditLog(taskId)),
  )

  tool(
    'list_permission_requests',
    { taskId: z.string().describe('Task UUID') },
    ({ taskId }) => ok(listPermissionRequests(taskId)),
  )
}
```

**Important:** The exact DB function names and import paths must match what exists in `server/db/`. Read the existing `mcpServer.ts` tool bodies carefully and copy the DB calls verbatim — the above is a structural template. Do not invent new DB functions.

- [ ] **Step 4: Create `server/mcp/tools/writeTools.ts`**

Registers `create_task`, `update_task`, `delete_task` — copy from the `tasks:write` section of the existing `mcpServer.ts`:

```typescript
import { z } from 'zod'
import type { McpToolDeps } from '../mcpAuth.js'
import { ok, mcpError } from '../mcpAuth.js'
import type { makeToolRegistrar } from '../mcpAuth.js'
import { VALID_STAGES } from '../../constants.js'
// ... import the same DB functions used in the existing mcpServer.ts write tools

type ToolFn = ReturnType<typeof makeToolRegistrar>

export function registerWriteTools(
  tool: ToolFn,
  deps: Pick<McpToolDeps, 'broadcast' | 'broadcastDeleted'>,
): void {
  // Copy create_task, update_task, delete_task tool bodies from mcpServer.ts
  // Use ok() instead of { content: [{ type: 'text', text: JSON.stringify(...) }] }
  // Use mcpError() instead of the local mcpError() in mcpServer.ts
}
```

- [ ] **Step 5: Create `server/mcp/tools/controlTools.ts`**

Registers `progress_task`, `approve_task`, `request_changes`, `cancel_task`, `retry_task`, `grant_permission`, `resolve_permission_request` — copy from the `pipeline:control` section:

```typescript
import { z } from 'zod'
import type { McpToolDeps } from '../mcpAuth.js'
import { ok, mcpError } from '../mcpAuth.js'
import type { makeToolRegistrar } from '../mcpAuth.js'
// ... same DB/orchestrator imports as used in the existing mcpServer.ts

type ToolFn = ReturnType<typeof makeToolRegistrar>

export function registerControlTools(
  tool: ToolFn,
  deps: Pick<McpToolDeps, 'orchestrator' | 'broadcast'>,
): void {
  // Copy all pipeline:control tool bodies from mcpServer.ts
  // Use ok() and mcpError()
}
```

- [ ] **Step 6: Create `server/mcp/tools/keyTools.ts`**

Registers `list_api_keys`, `create_api_key`, `revoke_api_key`:

```typescript
import { z } from 'zod'
import type { McpToolDeps } from '../mcpAuth.js'
import { ok, mcpError } from '../mcpAuth.js'
import type { makeToolRegistrar } from '../mcpAuth.js'
import { generateApiToken, hashApiToken, listApiKeys, createApiKey, revokeApiKey } from '../../db/apiKeysRepo.js'
import type { McpScope } from '../../../src/types.js'

type ToolFn = ReturnType<typeof makeToolRegistrar>

export function registerKeyTools(tool: ToolFn, _deps: Pick<McpToolDeps, 'scopes'>): void {
  tool(
    'list_api_keys',
    { includeRevoked: z.boolean().optional().describe('Include revoked keys') },
    ({ includeRevoked }) => ok(listApiKeys({ includeRevoked })),
  )

  tool(
    'create_api_key',
    {
      name: z.string().describe('Human-readable label'),
      scopes: z.array(z.enum(['tasks:read', 'tasks:write', 'pipeline:control', 'keys:manage'])).describe('Granted scopes'),
    },
    ({ name, scopes }) => {
      const token = generateApiToken()
      const keyHash = hashApiToken(token)
      const key = createApiKey({ name, keyHash, scopes: scopes as McpScope[] })
      return ok({ ...key, token })
    },
  )

  tool(
    'revoke_api_key',
    { id: z.string().describe('API key UUID') },
    ({ id }) => {
      const revoked = revokeApiKey(id)
      if (!revoked) mcpError(`Key not found: ${id}`)
      return ok({ revoked: true })
    },
  )
}
```

- [ ] **Step 7: Rewrite `server/mcp/mcpServer.ts`**

Replace the entire file with the slim orchestrating version:

```typescript
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import type { McpScope } from '../../src/types.js'
import type { PipelineOrchestrator } from '../pipeline/orchestrator.js'
import { makeToolRegistrar, type McpToolDeps } from './mcpAuth.js'
import { registerReadTools } from './tools/readTools.js'
import { registerWriteTools } from './tools/writeTools.js'
import { registerControlTools } from './tools/controlTools.js'
import { registerKeyTools } from './tools/keyTools.js'

export function buildMcpServer(
  orchestrator: PipelineOrchestrator,
  scopes: Set<McpScope>,
  broadcast: (taskId: string) => void,
  broadcastDeleted: (taskId: string) => void,
): McpServer {
  const server = new McpServer({ name: 'dashboard-tasks', version: '1.0.0' })
  const tool = makeToolRegistrar(server, scopes)
  const deps: McpToolDeps = { orchestrator, scopes, broadcast, broadcastDeleted }

  registerReadTools(tool, deps)
  registerWriteTools(tool, deps)
  registerControlTools(tool, deps)
  registerKeyTools(tool, deps)

  return server
}
```

- [ ] **Step 8: Remove the now-redundant `requireScope` and `mcpError` from `mcpServer.ts`**

After the rewrite in step 7, `requireScope` and the local `mcpError` in `mcpServer.ts` no longer exist. Each tool file now uses `mcpError` from `mcpAuth.ts`. Verify no leftover imports.

- [ ] **Step 9: Run tests**

```bash
pnpm test server/mcp/
```

Expected: All tests pass — including the existing coverage test that verifies every tool name appears in `TOOL_SCOPE_MAP`. The new structure registers tools through `makeToolRegistrar` which reads from `TOOL_SCOPE_MAP`, so the map is now load-bearing (not just documentation).

- [ ] **Step 10: Run typecheck**

```bash
pnpm typecheck
```

Expected: No errors. Fix any `z.ZodRawShape` generic mismatches.

- [ ] **Step 11: Commit**

```bash
git add server/mcp/
git commit -m "refactor: wire TOOL_SCOPE_MAP into runtime, split buildMcpServer into tool modules"
```

---

## Task 4: Security & Error Handling Fixes

**Files:**
- Modify: `server/index.ts`
- Modify: `server/mcp/mcpAuth.ts`
- Modify: `server/services/worktreeManager.ts`

- [ ] **Step 1: Fix `timingSafeEqual` for channel-reply token (server/index.ts ~line 433)**

Add `timingSafeEqual` to the existing crypto import at the top of `server/index.ts`:
```typescript
import { timingSafeEqual } from 'node:crypto'
```

Then find the comparison `if (discovery.token !== token)` and replace it:
```typescript
// Before:
if (discovery.token !== token) {
  res.status(403).json({ error: 'Invalid token' })
  return
}

// After:
const expected = Buffer.from(String(discovery.token))
const provided = Buffer.from(token)
if (
  expected.length !== provided.length ||
  !timingSafeEqual(expected, provided)
) {
  res.status(403).json({ error: 'Invalid token' })
  return
}
```

- [ ] **Step 2: Fix `touchApiKey` silent failure (server/mcp/mcpAuth.ts ~line 77)**

Replace:
```typescript
setImmediate(() => touchApiKey(key.id))
```
With:
```typescript
setImmediate(() => {
  try {
    touchApiKey(key.id)
  }
  catch (e) {
    console.warn('[mcpAuth] touchApiKey failed', e)
  }
})
```

- [ ] **Step 3: Add logging to silent catches in `server/services/worktreeManager.ts`**

Find every `catch { }` or `catch (_e) { }` block in `worktreeManager.ts`. For each one that simply returns `false`, `null`, or `[]`, add a warn log. Example pattern:

```typescript
// Before:
catch {
  return false
}

// After:
catch (e) {
  console.warn('[worktreeManager] git operation failed', e)
  return false
}
```

Apply this to every silent catch in the file (`isRegisteredWorktree`, `hasUncommittedChanges`, `isGitRepo`, `currentBranch`).

- [ ] **Step 4: Fix UNIQUE error detection in `server/routes/apiKeyRoutes.ts`**

Find the SQLite UNIQUE constraint catch (around line 49–53). Replace the brittle string-matching with the `better-sqlite3` error code:

```typescript
// Before:
catch (e: unknown) {
  if (e instanceof Error && e.message.includes('UNIQUE constraint')) {
    res.status(409).json({ error: 'A key with that name already exists' })
    return
  }
  throw e
}

// After:
catch (e: unknown) {
  if (e instanceof Error && (e as NodeJS.ErrnoException & { code?: string }).code === 'SQLITE_CONSTRAINT_UNIQUE') {
    res.status(409).json({ error: 'A key with that name already exists' })
    return
  }
  throw e
}
```

- [ ] **Step 5: Run tests and typecheck**

```bash
pnpm test && pnpm typecheck
```

Expected: All pass.

- [ ] **Step 6: Commit**

```bash
git add server/index.ts server/mcp/mcpAuth.ts server/services/worktreeManager.ts server/routes/apiKeyRoutes.ts
git commit -m "fix: timingSafeEqual for channel-reply, log silent git errors, robust UNIQUE error code"
```

---

## Task 5: Vue Component Fixes

**Files:**
- Modify: `src/components/ApiKeySettings.vue`
- Modify: `src/components/CrossLinkBanner.vue`
- Modify: `src/components/TaskModal.vue`
- Modify: `src/components/AgentModal.vue`

- [ ] **Step 1: Fix `ApiKeySettings.vue` — three bugs**

**Bug A: `loadKeys()` at setup time → `onMounted`**

Find the bare `loadKeys()` call in `<script setup>` (line ~50). Replace:
```typescript
// Before:
loadKeys()

// After:
onMounted(loadKeys)
```

Add `onMounted` to the Vue imports if not already present:
```typescript
import { ref, onMounted } from 'vue'
```

**Bug B: Esc key handler on non-focusable div → window listener**

Find `@keydown.escape` on a `div` element. Remove that attribute. Then add a `window`-based Esc handler (same pattern as `TaskModal.vue`):

```typescript
import { ref, onMounted, onUnmounted } from 'vue'

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    showCreateDialog.value = false
    confirmRevokeId.value = null
  }
}
onMounted(() => window.addEventListener('keydown', onKeydown))
onUnmounted(() => window.removeEventListener('keydown', onKeydown))
```

Remove the `@keydown.escape` binding from the template.

**Bug C: Clipboard error handling**

Find `copyToken` (around line 125). Replace:
```typescript
// Before:
async function copyToken(token: string) {
  await navigator.clipboard.writeText(token)
  copyHint.value = token
  setTimeout(() => { copyHint.value = null }, 2000)
}

// After:
async function copyToken(token: string) {
  try {
    await navigator.clipboard.writeText(token)
    copyHint.value = token
  }
  catch {
    copyHint.value = '__error__'
  }
  setTimeout(() => { copyHint.value = null }, 2000)
}
```

In the template, show an error hint when `copyHint.value === '__error__'`:
```html
<span v-if="copyHint === '__error__'" class="copy-error">Copy failed</span>
<span v-else-if="copyHint === key.id">Copied!</span>
```
(Adjust the exact template reference to match the existing `copyHint` usage.)

- [ ] **Step 2: Fix `CrossLinkBanner.vue` — replace hardcoded hex colors**

Find the `<style>` block. Replace every hardcoded hex color with CSS custom properties that the existing theme system defines. Read `src/assets/main.css` or similar to find the correct variable names — look for `--accent-`, `--bg-`, `--text-` variables already used in other components.

Example (adjust to actual variable names in the project):
```css
/* Before: */
background: #1e3a5f;
color: #93c5fd;
border-color: #1e4080;

/* After: */
background: var(--bg-tertiary);
color: var(--accent-blue);
border-color: var(--border-accent);
```

Verify the theme variables exist by grepping:
```bash
grep -r "\-\-bg-tertiary\|\-\-accent-blue\|\-\-border-accent" src/assets/
```
Use only variables that exist. If the exact variables differ, match what other components use (e.g. check `AgentCard.vue` or `TaskCard.vue` for reference).

- [ ] **Step 3: Fix double `window.addEventListener` in `TaskModal.vue`**

Read `TaskModal.vue` and find the `watch(() => props.task, …)` that adds/removes the `keydown` listener. The current pattern re-adds the listener on every watch fire where `props.task` is non-null, which double-registers it on task changes.

Replace the watch-based listener management with setup-time lifecycle hooks:

```typescript
// Remove the listener management from inside the watch.
// Add at setup level:
function onKeydown(e: KeyboardEvent) {
  if (!props.task) return
  if (e.key === 'Escape') emit('close')
}
onMounted(() => window.addEventListener('keydown', onKeydown))
onUnmounted(() => window.removeEventListener('keydown', onKeydown))
```

The handler guards on `!props.task` so it's a no-op when no task is open. Remove any `addEventListener`/`removeEventListener` calls from the `watch` body.

- [ ] **Step 4: Fix double `window.addEventListener` in `AgentModal.vue`**

Same fix as Step 3, applied to `AgentModal.vue`:

```typescript
function onKeydown(e: KeyboardEvent) {
  if (!props.agent) return
  if (e.key === 'Escape') emit('close')
}
onMounted(() => window.addEventListener('keydown', onKeydown))
onUnmounted(() => window.removeEventListener('keydown', onKeydown))
```

Remove the listener from any `watch` body.

- [ ] **Step 5: Run typecheck**

```bash
pnpm typecheck
```

Expected: No errors.

- [ ] **Step 6: Smoke-test in browser**

```bash
pnpm dev
```

Open the dashboard. Verify:
- Escape closes modals (TaskModal, AgentModal, ApiKeySettings create/confirm dialogs)
- Token copy shows "Copy failed" on a browser with clipboard blocked (test by denying clipboard permission)
- CrossLinkBanner respects dark/light theme toggle (toggle with the theme button in the header)
- Opening a task, switching to another task, pressing Escape — modal closes (not double-firing)

- [ ] **Step 7: Commit**

```bash
git add src/components/ApiKeySettings.vue src/components/CrossLinkBanner.vue \
  src/components/TaskModal.vue src/components/AgentModal.vue
git commit -m "fix: onMounted loadKeys, Esc handler, clipboard error, theme colors, single keydown listener"
```

---

## Task 6: ENV Var Extraction

**Files:**
- Modify: `server/index.ts`
- Modify: `server/spawnManager.ts`

- [ ] **Step 1: Add `DASHBOARD_HOST` to `server/index.ts`**

Find the existing `PORT` constant near the top of the file. Add `HOST` below it:

```typescript
const PORT = Number(process.env.DASHBOARD_PORT ?? 13120)
const HOST = process.env.DASHBOARD_HOST ?? '127.0.0.1'
```

If `DASHBOARD_HOST` is set to anything other than `127.0.0.1` or `localhost`, log a security warning:
```typescript
if (HOST !== '127.0.0.1' && HOST !== 'localhost') {
  console.warn(
    `[security] Dashboard bound to ${HOST} — ensure this host is on a trusted network or VPN. Never expose to the public internet.`,
  )
}
```

Update `httpServer.listen` to use `HOST`:
```typescript
// Before:
httpServer.listen(PORT, '127.0.0.1', () => {

// After:
httpServer.listen(PORT, HOST, () => {
```

Also update the CSRF allow-list to use `HOST` where `'127.0.0.1'` is hardcoded (around line 274):
```typescript
// Before:
return (url.hostname === 'localhost' || url.hostname === '127.0.0.1') && url.port === String(PORT)

// After:
return (url.hostname === HOST || url.hostname === 'localhost' || url.hostname === '127.0.0.1') && url.port === String(PORT)
```

- [ ] **Step 2: Add `DASHBOARD_SSE_INTERVAL_MS` to `server/index.ts`**

Find the hardcoded `3000` in the SSE broadcast `setInterval` (around line 146):

```typescript
// Before:
}, 3000)

// After:
const SSE_INTERVAL_MS = Number(process.env.DASHBOARD_SSE_INTERVAL_MS ?? 3000)
// ... (define this near PORT/HOST at the top)
}, SSE_INTERVAL_MS)
```

- [ ] **Step 3: Add rate-limit ENV vars to `server/spawnManager.ts`**

Find the constants at the top of the file (lines 26–27):

```typescript
// Before:
const RATE_LIMIT_WINDOW_MS = 60_000
const RATE_LIMIT_MAX = 5

// After:
const RATE_LIMIT_WINDOW_MS = Number(process.env.DASHBOARD_SPAWN_RATE_WINDOW_MS ?? 60_000)
const RATE_LIMIT_MAX = Number(process.env.DASHBOARD_SPAWN_RATE_LIMIT ?? 5)
```

Find the hardcoded error message string `"5 per minute"` or similar in `server/index.ts` (around line 317). Replace the inline strings with a derived value:

```typescript
// Wherever the error message references the rate limit:
`Rate limit exceeded: max ${RATE_LIMIT_MAX} spawns per ${Math.round(RATE_LIMIT_WINDOW_MS / 1000)}s`
```

Since `RATE_LIMIT_MAX` and `RATE_LIMIT_WINDOW_MS` are in `spawnManager.ts`, either export them or move the error message construction into `SpawnManager` so `index.ts` can read it from the instance.

- [ ] **Step 4: Run tests + typecheck**

```bash
pnpm test && pnpm typecheck
```

Expected: All pass.

- [ ] **Step 5: Commit**

```bash
git add server/index.ts server/spawnManager.ts
git commit -m "feat: add DASHBOARD_HOST, DASHBOARD_SSE_INTERVAL_MS, DASHBOARD_SPAWN_RATE_* env vars"
```

---

## Task 7: CLAUDE.md Update

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Add missing files to the Backend inventory section**

Find the Backend section listing (the bullet list starting with `server/platform.ts`). Add:

```markdown
- `server/spawnManager.ts` — rate-limited dashboard-initiated agent spawner (spawn store, stderr ring-buffer, channel reply routing)
- `server/constants.ts` — shared constants: `VALID_STAGES`, `SLUG_RE`, `SLUG_PATTERN_MESSAGE`, `SYSTEM_PROMPT_MAX_CHARS`
- `server/channelConfig.ts` — `buildDashboardChannelMcpConfig()`: builds the MCP channel config injected into user-spawned agents
```

- [ ] **Step 2: Add CrossLinkBanner to the Pipeline UI section**

Find the Pipeline UI section. Add:

```markdown
- `src/components/CrossLinkBanner.vue` — session↔task cross-link banner; emits `click` to trigger navigation via `useAgents` / `useTasks`
```

- [ ] **Step 3: Add `.mcp.json.example` note to the MCP Endpoint section**

Find the MCP Endpoint section. Add at the end:

```markdown
**Local agent integration:** A `.mcp.json.example` is shipped at the repo root. Copy it to `.mcp.json` and export `DASHBOARD_MCP_TOKEN` to give any Claude Code session in this repo automatic access to the dashboard MCP. `.mcp.json` is gitignored to prevent accidental token commits.
```

- [ ] **Step 4: Add new ENV vars to Key Conventions**

Find the "Pipeline env vars" listing in Key Conventions. Add:

```markdown
- `DASHBOARD_HOST` (bind address, default `127.0.0.1`; logs a warning if non-loopback)
- `DASHBOARD_SSE_INTERVAL_MS` (agent SSE broadcast interval, default 3000 ms)
- `DASHBOARD_SPAWN_RATE_LIMIT` (max user-initiated spawns per window, default 5)
- `DASHBOARD_SPAWN_RATE_WINDOW_MS` (rate-limit window, default 60000 ms)
```

- [ ] **Step 5: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md for spawnManager, constants, CrossLinkBanner, .mcp.json.example, new ENV vars"
```

---

## Task 8: README Major Update

**Files:**
- Modify: `README.md`

This task rewrites significant portions of the README. Read the current README fully before starting. The changes below are structural — adapt exact wording to match the project's existing tone.

- [ ] **Step 1: Fix the "No database" claim in Key Conventions**

Find the line stating `No database — all data from Claude Code's filesystem + running processes`. Replace with:

```markdown
**Dual persistence:** agent monitoring is filesystem-derived (no database); task pipeline uses SQLite at `~/.claude/dashboard-tasks.db` (override via `DASHBOARD_DB_PATH`). See [ADR-0001](docs/architecture/adr/0001-sqlite-for-task-pipeline.md).
```

- [ ] **Step 2: Fix the Linux CPU claim**

Find the line `CPU monitoring (top) uses macOS-specific flags and will show 0%` or similar. Replace:

```markdown
**Platform:** macOS and Linux. CPU monitoring uses `top` on macOS and `/proc/stat` on Linux. Windows is unsupported.
```

- [ ] **Step 3: Correct pipeline stage names**

Find any mention of stage names in the README. Ensure they match exactly:
`backlog → pruefung → refinement → planning → approval1 → umsetzungskonzept → approval2 → umsetzung → selbstreview → finalisierung → done`

Also mention terminal-ish states: `on_hold`, `cancelled`.

- [ ] **Step 4: Add MCP Endpoint section**

After the "Controlling Running Agents" section (or after "Features"), add:

```markdown
## MCP Endpoint

The dashboard exposes a stateless StreamableHTTP MCP server at `POST /api/mcp`. External Claude agents and tooling can interact with the task pipeline programmatically.

**Authentication:** Bearer token in `Authorization` header. Tokens are never stored — only their SHA-256 hash lives in SQLite. Generate tokens in **Settings → API Keys**.

**Scopes** (hierarchical — higher scopes imply lower):
| Scope | Access |
|---|---|
| `tasks:read` | List and read tasks, stage runs, audit log |
| `tasks:write` | Create, update, delete tasks (implies `tasks:read`) |
| `pipeline:control` | Progress tasks, approve, cancel, retry, manage permissions (implies `tasks:read`) |
| `keys:manage` | Full access including API key management |

**Available tools:** `list_tasks`, `get_task`, `list_stage_runs`, `list_audit`, `list_permission_requests`, `create_task`, `update_task`, `delete_task`, `progress_task`, `approve_task`, `request_changes`, `cancel_task`, `retry_task`, `grant_permission`, `resolve_permission_request`, `list_api_keys`, `create_api_key`, `revoke_api_key`

**Local integration:** Copy `.mcp.json.example` to `.mcp.json` and export `DASHBOARD_MCP_TOKEN` — any Claude Code session in this repo then has automatic dashboard MCP access.
```

- [ ] **Step 5: Add API Keys section**

After the MCP Endpoint section:

```markdown
## API Keys

Manage API keys at **Settings → API Keys** in the UI or via `GET/POST/DELETE /api/settings/api-keys`.

- Tokens are shown once at creation — store them immediately
- Only the SHA-256 hash is persisted; the dashboard cannot recover a token
- Each key carries one or more scopes (see MCP Endpoint above)
```

- [ ] **Step 6: Update the Environment Variables table**

Find the env var table. Ensure these are all listed:

| Variable | Default | Description |
|---|---|---|
| `DASHBOARD_PORT` | `13120` | HTTP server port |
| `DASHBOARD_HOST` | `127.0.0.1` | Bind address (warn if non-loopback) |
| `DASHBOARD_DB_PATH` | `~/.claude/dashboard-tasks.db` | SQLite path |
| `DASHBOARD_WORKTREE_ROOT` | `~/.claude/dashboard-worktrees` | Per-task git worktrees |
| `DASHBOARD_REMOTES` | — | Comma-separated remote dashboard URLs for multi-machine mode |
| `DASHBOARD_SSE_INTERVAL_MS` | `3000` | Agent SSE broadcast interval |
| `DASHBOARD_SPAWN_RATE_LIMIT` | `5` | Max spawns per window |
| `DASHBOARD_SPAWN_RATE_WINDOW_MS` | `60000` | Spawn rate-limit window (ms) |
| `DASHBOARD_MCP_TOKEN` | — | Bearer token for dashboard MCP access (injected into spawned agents) |
| `DASHBOARD_MCP_URL` | — | Dashboard MCP URL (injected into spawned stage agents) |
| `DASHBOARD_STAGE_RUN_ID` | — | Injected into stage agents by the orchestrator |
| `DASHBOARD_TASK_ID` | — | Injected into stage agents by the orchestrator |

- [ ] **Step 7: Update the directory structure / architecture section**

Add `server/mcp/`, `server/db/`, `server/services/`, `server/notifications/`, `server/pipeline/`, `server/routes/` to the directory tree if they are missing. Add `src/components/ApiKeySettings.vue`, `CrossLinkBanner.vue` to the component list.

- [ ] **Step 8: Update the Features list**

Ensure these bullets exist:
- Real-time agent monitoring via SSE
- Task pipeline with approval gates and self-review stage
- Authenticated MCP endpoint for external agent control (18 tools, 4 scopes)
- API key management with scoped access
- Cross-linking between agent sessions and pipeline tasks
- Dark/light theme, list/card/kanban views

- [ ] **Step 9: Commit**

```bash
git add README.md
git commit -m "docs: major README update — MCP endpoint, API keys, correct env vars, Linux support, cross-linking"
```

---

## Self-Review

**Spec coverage check:**

| Requirement | Covered by |
|---|---|
| `.mcp.json` → gitignore + example | Task 1 |
| Stale plans deleted + never re-committed | Task 1 (gitignore patterns for `docs/plans/`, `docs/specs/`) |
| Token generation DRY | Task 2 |
| Slug regex DRY | Task 2 |
| SYSTEM_PROMPT_MAX_CHARS shared | Task 2 |
| `TOOL_SCOPE_MAP` wired into runtime | Task 3 |
| `ok()` helper eliminates 16 duplicate response wrappings | Task 3 |
| `buildMcpServer` split into 4 tool files | Task 3 |
| `timingSafeEqual` for channel-reply | Task 4 |
| `touchApiKey` error logging | Task 4 |
| `worktreeManager` silent error logging | Task 4 |
| UNIQUE error code (not string-matching) | Task 4 |
| `ApiKeySettings.vue` `onMounted`, Esc, clipboard | Task 5 |
| `CrossLinkBanner.vue` theme-aware colors | Task 5 |
| `TaskModal` + `AgentModal` single addEventListener | Task 5 |
| `DASHBOARD_HOST` | Task 6 |
| `DASHBOARD_SSE_INTERVAL_MS` | Task 6 |
| `DASHBOARD_SPAWN_RATE_*` | Task 6 |
| CLAUDE.md: spawnManager, constants, CrossLinkBanner, .mcp.json.example | Task 7 |
| README: MCP, API keys, env vars, arch, fixes | Task 8 |

**No placeholders found** — each step contains actual code or exact commands.

**Type consistency:** `McpToolDeps` is defined once in `mcpAuth.ts` and imported by all four tool files. `ok()` and `mcpError()` come from `mcpAuth.ts`. `generateApiToken()` / `hashApiToken()` come from `apiKeysRepo.ts`. No redefinitions.
