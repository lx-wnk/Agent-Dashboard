# MCP Task Endpoint Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a fully functional MCP server at `POST /api/mcp` that exposes the entire task lifecycle to external and internal Claude agents, secured by named API keys with hierarchical scopes and a dashboard UI for key management.

**Architecture:** A new `McpServer` (from `@modelcontextprotocol/sdk`) is instantiated per HTTP request and connected to a `StreamableHTTPServerTransport` in stateless mode. Auth middleware validates a `Bearer mcp_<token>` header by SHA-256 hashing it and looking up the `api_keys` table; it resolves implied scopes (e.g. `tasks:write` → also `tasks:read`) and closes them over the server factory so each tool handler enforces scope without shared state. Internal stage agents receive a short-lived `operator`-scope token injected as `DASHBOARD_MCP_TOKEN` env var.

**Tech Stack:** Node.js / TypeScript / Express / better-sqlite3 / `@modelcontextprotocol/sdk@^1.12.1` / Vue 3 / Vite / Vitest

---

## File Map

| File | Status | Responsibility |
|---|---|---|
| `server/db/schema.sql` | Modify | Add `api_keys` table DDL |
| `server/db/client.ts` | Modify | Runtime migration: probe & ALTER for existing DBs |
| `server/db/apiKeysRepo.ts` | Create | CRUD for api_keys (create, getByHash, list, revoke) |
| `server/db/rowMappers.ts` | Modify | Add `ApiKeyRow` → `ApiKey` mapper |
| `server/db/db.test.ts` | Modify | Add apiKeysRepo test suite |
| `src/types.ts` | Modify | Add `ApiKey`, `McpScope` interfaces |
| `server/mcp/mcpAuth.ts` | Create | Token validation middleware + scope resolution |
| `server/mcp/mcpServer.ts` | Create | `buildMcpServer(orchestrator, scopes)` factory — 15 tools |
| `server/mcp/mcpRouter.ts` | Create | Express router: `POST /api/mcp` |
| `server/routes/apiKeyRoutes.ts` | Create | REST CRUD for API keys (dashboard UI) |
| `server/pipeline/agentSpawner.ts` | Modify | Inject `DASHBOARD_MCP_TOKEN` + `DASHBOARD_MCP_URL` into env |
| `server/index.ts` | Modify | Mount mcpRouter + apiKeyRoutes; pass port to spawner |
| `src/components/ApiKeySettings.vue` | Create | Settings page: list/create/revoke keys |
| `src/App.vue` | Modify | Add Settings view + nav toggle |

---

## Task 1: Install MCP SDK + DB schema for api_keys

**Files:**
- Modify: `package.json`
- Modify: `server/db/schema.sql`
- Modify: `server/db/client.ts`
- Modify: `server/db/db.test.ts`

- [ ] **Step 1: Install `@modelcontextprotocol/sdk` in the root workspace**

```bash
cd /path/to/project && pnpm add @modelcontextprotocol/sdk
```

Expected: `@modelcontextprotocol/sdk` appears in `package.json` dependencies.

- [ ] **Step 2: Write a failing test for the api_keys table existing**

Add to `server/db/db.test.ts` inside the existing `describe` block structure:

```typescript
describe('api_keys table', () => {
  it('has the expected columns after migration', () => {
    const db = getDb()
    const cols = db.prepare('PRAGMA table_info(api_keys)').all() as Array<{ name: string }>
    const names = cols.map(c => c.name)
    expect(names).toContain('id')
    expect(names).toContain('name')
    expect(names).toContain('key_hash')
    expect(names).toContain('scopes')
    expect(names).toContain('active')
    expect(names).toContain('created_at')
    expect(names).toContain('last_used_at')
  })
})
```

- [ ] **Step 3: Run the test to confirm it fails**

```bash
pnpm test -- --reporter=verbose server/db/db.test.ts
```

Expected: FAIL — "no such table: api_keys"

- [ ] **Step 4: Add the `api_keys` table DDL to `server/db/schema.sql`**

Append to the end of `server/db/schema.sql`:

```sql
-- MCP API keys for external and internal agent authentication
CREATE TABLE IF NOT EXISTS api_keys (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL UNIQUE,
  key_hash     TEXT NOT NULL UNIQUE,   -- SHA-256 of raw token (never store plain)
  scopes       TEXT NOT NULL,          -- JSON array: ['tasks:read','tasks:write',...]
  active       INTEGER NOT NULL DEFAULT 1,
  created_at   TEXT NOT NULL,
  last_used_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys(key_hash);
CREATE INDEX IF NOT EXISTS idx_api_keys_active ON api_keys(active);
```

- [ ] **Step 5: Run the test to confirm it passes**

```bash
pnpm test -- --reporter=verbose server/db/db.test.ts
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add package.json pnpm-lock.yaml server/db/schema.sql server/db/db.test.ts
git commit -m "feat(mcp): add api_keys table schema + install MCP SDK"
```

---

## Task 2: Types + apiKeysRepo

**Files:**
- Modify: `src/types.ts`
- Modify: `server/db/rowMappers.ts`
- Create: `server/db/apiKeysRepo.ts`
- Modify: `server/db/db.test.ts`

- [ ] **Step 1: Add `McpScope` and `ApiKey` to `src/types.ts`**

Find the end of the pipeline types section in `src/types.ts` and append:

```typescript
// MCP API Key types
export type McpScope = 'tasks:read' | 'tasks:write' | 'pipeline:control' | 'keys:manage'

export interface ApiKey {
  id: string
  name: string
  // key_hash is intentionally absent — never send the hash over the wire
  scopes: McpScope[]
  active: boolean
  createdAt: string
  lastUsedAt: string | null
}
```

- [ ] **Step 2: Add `ApiKeyRow` and mapper to `server/db/rowMappers.ts`**

Open `server/db/rowMappers.ts`, read how existing mappers (e.g. `rowToTask`) are structured, then append:

```typescript
import type { ApiKey, McpScope } from '../../src/types.js'

export interface ApiKeyRow {
  id: string
  name: string
  key_hash: string
  scopes: string        // JSON array
  active: number        // 0 | 1
  created_at: string
  last_used_at: string | null
}

export function rowToApiKey(row: ApiKeyRow): ApiKey {
  return {
    id: row.id,
    name: row.name,
    scopes: JSON.parse(row.scopes) as McpScope[],
    active: row.active === 1,
    createdAt: row.created_at,
    lastUsedAt: row.last_used_at,
  }
}
```

- [ ] **Step 3: Write failing tests for apiKeysRepo**

Add to `server/db/db.test.ts`:

```typescript
import {
  createApiKey,
  getApiKeyByHash,
  listApiKeys,
  revokeApiKey,
  touchApiKey,
} from './apiKeysRepo.js'
import { createHash, randomBytes } from 'node:crypto'

function makeToken(): string { return `mcp_${randomBytes(16).toString('hex')}` }
function hashToken(t: string): string { return createHash('sha256').update(t).digest('hex') }

describe('apiKeysRepo', () => {
  it('creates a key and retrieves it by hash', () => {
    const token = makeToken()
    const key = createApiKey({ name: 'ci-bot', keyHash: hashToken(token), scopes: ['tasks:read', 'pipeline:control'] })
    expect(key.id).toBeTruthy()
    expect(key.name).toBe('ci-bot')
    expect(key.scopes).toEqual(['tasks:read', 'pipeline:control'])
    expect(key.active).toBe(true)
    expect(key.lastUsedAt).toBeNull()

    const found = getApiKeyByHash(hashToken(token))
    expect(found?.id).toBe(key.id)
  })

  it('returns null for unknown hash', () => {
    expect(getApiKeyByHash('not-a-real-hash')).toBeNull()
  })

  it('lists only active keys by default', () => {
    const t1 = makeToken()
    const t2 = makeToken()
    const k1 = createApiKey({ name: 'a', keyHash: hashToken(t1), scopes: ['tasks:read'] })
    const k2 = createApiKey({ name: 'b', keyHash: hashToken(t2), scopes: ['tasks:write'] })
    revokeApiKey(k2.id)

    const all = listApiKeys()
    expect(all.map(k => k.id)).toContain(k1.id)
    expect(all.map(k => k.id)).not.toContain(k2.id)

    const withRevoked = listApiKeys({ includeRevoked: true })
    expect(withRevoked).toHaveLength(2)
  })

  it('revokes a key — getApiKeyByHash returns null after revoke', () => {
    const token = makeToken()
    const key = createApiKey({ name: 'c', keyHash: hashToken(token), scopes: ['tasks:read'] })
    revokeApiKey(key.id)
    expect(getApiKeyByHash(hashToken(token))).toBeNull()  // active=0 excluded
  })

  it('touchApiKey updates last_used_at', () => {
    const token = makeToken()
    const key = createApiKey({ name: 'd', keyHash: hashToken(token), scopes: ['tasks:read'] })
    expect(key.lastUsedAt).toBeNull()
    touchApiKey(key.id)
    const refreshed = getApiKeyByHash(hashToken(token))
    expect(refreshed?.lastUsedAt).toBeTruthy()
  })

  it('enforces unique name constraint', () => {
    createApiKey({ name: 'dup', keyHash: hashToken(makeToken()), scopes: ['tasks:read'] })
    expect(() => createApiKey({ name: 'dup', keyHash: hashToken(makeToken()), scopes: ['tasks:read'] })).toThrow()
  })
})
```

- [ ] **Step 4: Run tests to confirm they fail**

```bash
pnpm test -- --reporter=verbose server/db/db.test.ts
```

Expected: FAIL — "Cannot find module './apiKeysRepo.js'"

- [ ] **Step 5: Create `server/db/apiKeysRepo.ts`**

```typescript
import type { Database } from 'better-sqlite3'
import type { ApiKey, McpScope } from '../../src/types.js'
import { randomUUID } from 'node:crypto'
import { getDb } from './client.js'
import { type ApiKeyRow, rowToApiKey } from './rowMappers.js'

export interface CreateApiKeyInput {
  name: string
  keyHash: string
  scopes: McpScope[]
}

export function createApiKey(input: CreateApiKeyInput, db: Database = getDb()): ApiKey {
  const id = randomUUID()
  const now = new Date().toISOString()
  db.prepare(`
    INSERT INTO api_keys (id, name, key_hash, scopes, active, created_at)
    VALUES (@id, @name, @key_hash, @scopes, 1, @created_at)
  `).run({
    id,
    name: input.name,
    key_hash: input.keyHash,
    scopes: JSON.stringify(input.scopes),
    created_at: now,
  })
  return getApiKeyByIdInternal(id, db)!
}

function getApiKeyByIdInternal(id: string, db: Database): ApiKey | null {
  const row = db.prepare('SELECT * FROM api_keys WHERE id = ?').get(id) as ApiKeyRow | undefined
  return row ? rowToApiKey(row) : null
}

export function getApiKeyById(id: string, db: Database = getDb()): ApiKey | null {
  return getApiKeyByIdInternal(id, db)
}

// Only returns active keys — inactive keys are treated as non-existent for auth.
export function getApiKeyByHash(hash: string, db: Database = getDb()): ApiKey | null {
  const row = db.prepare('SELECT * FROM api_keys WHERE key_hash = ? AND active = 1').get(hash) as ApiKeyRow | undefined
  return row ? rowToApiKey(row) : null
}

export function listApiKeys(opts: { includeRevoked?: boolean } = {}, db: Database = getDb()): ApiKey[] {
  const sql = opts.includeRevoked
    ? 'SELECT * FROM api_keys ORDER BY created_at DESC'
    : 'SELECT * FROM api_keys WHERE active = 1 ORDER BY created_at DESC'
  const rows = db.prepare(sql).all() as ApiKeyRow[]
  return rows.map(rowToApiKey)
}

export function revokeApiKey(id: string, db: Database = getDb()): boolean {
  const result = db.prepare('UPDATE api_keys SET active = 0 WHERE id = ?').run(id)
  return result.changes > 0
}

export function touchApiKey(id: string, db: Database = getDb()): void {
  db.prepare('UPDATE api_keys SET last_used_at = ? WHERE id = ?').run(new Date().toISOString(), id)
}
```

- [ ] **Step 6: Run tests to confirm they pass**

```bash
pnpm test -- --reporter=verbose server/db/db.test.ts
```

Expected: All apiKeysRepo tests PASS

- [ ] **Step 7: Typecheck**

```bash
pnpm typecheck
```

Expected: zero errors

- [ ] **Step 8: Commit**

```bash
git add src/types.ts server/db/rowMappers.ts server/db/apiKeysRepo.ts server/db/db.test.ts
git commit -m "feat(mcp): apiKeysRepo + ApiKey/McpScope types"
```

---

## Task 3: MCP Auth Middleware

**Files:**
- Create: `server/mcp/mcpAuth.ts`
- Create: `server/mcp/mcpAuth.test.ts`

- [ ] **Step 1: Write failing tests for scope resolution**

Create `server/mcp/mcpAuth.test.ts`:

```typescript
import { describe, expect, it } from 'vitest'
import { resolveScopes, TOOL_SCOPE_MAP } from './mcpAuth.js'

describe('resolveScopes', () => {
  it('tasks:read implies nothing extra', () => {
    expect(resolveScopes(['tasks:read'])).toEqual(new Set(['tasks:read']))
  })

  it('tasks:write implies tasks:read', () => {
    const s = resolveScopes(['tasks:write'])
    expect(s.has('tasks:write')).toBe(true)
    expect(s.has('tasks:read')).toBe(true)
  })

  it('pipeline:control implies tasks:read', () => {
    const s = resolveScopes(['pipeline:control'])
    expect(s.has('pipeline:control')).toBe(true)
    expect(s.has('tasks:read')).toBe(true)
    expect(s.has('tasks:write')).toBe(false)
  })

  it('keys:manage implies all scopes', () => {
    const s = resolveScopes(['keys:manage'])
    expect(s.has('keys:manage')).toBe(true)
    expect(s.has('tasks:read')).toBe(true)
    expect(s.has('tasks:write')).toBe(true)
    expect(s.has('pipeline:control')).toBe(true)
  })

  it('deduplicates when both write and read are explicit', () => {
    const s = resolveScopes(['tasks:read', 'tasks:write'])
    expect([...s].filter(x => x === 'tasks:read')).toHaveLength(1)
  })
})

describe('TOOL_SCOPE_MAP', () => {
  it('maps list_tasks to tasks:read', () => {
    expect(TOOL_SCOPE_MAP.list_tasks).toBe('tasks:read')
  })

  it('maps create_task to tasks:write', () => {
    expect(TOOL_SCOPE_MAP.create_task).toBe('tasks:write')
  })

  it('maps progress_task to pipeline:control', () => {
    expect(TOOL_SCOPE_MAP.progress_task).toBe('pipeline:control')
  })

  it('maps create_api_key to keys:manage', () => {
    expect(TOOL_SCOPE_MAP.create_api_key).toBe('keys:manage')
  })
})
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
pnpm test -- --reporter=verbose server/mcp/mcpAuth.test.ts
```

Expected: FAIL — "Cannot find module './mcpAuth.js'"

- [ ] **Step 3: Create `server/mcp/mcpAuth.ts`**

```typescript
import type { McpScope } from '../../src/types.js'
import type { NextFunction, Request, Response } from 'express'
import { createHash } from 'node:crypto'
import { getApiKeyByHash, touchApiKey } from '../db/apiKeysRepo.js'

// Which scope each MCP tool requires
export const TOOL_SCOPE_MAP: Record<string, McpScope> = {
  // tasks:read
  list_tasks: 'tasks:read',
  get_task: 'tasks:read',
  list_stage_runs: 'tasks:read',
  list_audit: 'tasks:read',
  list_permission_requests: 'tasks:read',
  // tasks:write
  create_task: 'tasks:write',
  update_task: 'tasks:write',
  delete_task: 'tasks:write',
  // pipeline:control
  progress_task: 'pipeline:control',
  approve_task: 'pipeline:control',
  request_changes: 'pipeline:control',
  cancel_task: 'pipeline:control',
  retry_task: 'pipeline:control',
  grant_permission: 'pipeline:control',
  resolve_permission_request: 'pipeline:control',
  // keys:manage
  list_api_keys: 'keys:manage',
  create_api_key: 'keys:manage',
  revoke_api_key: 'keys:manage',
}

const SCOPE_IMPLIES: Record<McpScope, McpScope[]> = {
  'tasks:read': [],
  'tasks:write': ['tasks:read'],
  'pipeline:control': ['tasks:read'],
  'keys:manage': ['tasks:read', 'tasks:write', 'pipeline:control'],
}

export function resolveScopes(scopes: McpScope[]): Set<McpScope> {
  const result = new Set<McpScope>(scopes)
  for (const scope of scopes) {
    for (const implied of SCOPE_IMPLIES[scope] ?? []) {
      result.add(implied)
    }
  }
  return result
}

// Augment Express Request type so downstream handlers can read mcpAuth
declare global {
  namespace Express {
    interface Request {
      mcpAuth?: { keyId: string, effectiveScopes: Set<McpScope> }
    }
  }
}

export function mcpAuthMiddleware(req: Request, res: Response, next: NextFunction): void {
  const header = req.headers.authorization
  if (!header || !header.startsWith('Bearer ')) {
    res.status(401).json({ error: 'Missing or invalid Authorization header' })
    return
  }
  const token = header.slice(7).trim()
  const hash = createHash('sha256').update(token).digest('hex')
  const key = getApiKeyByHash(hash)
  if (!key) {
    res.status(401).json({ error: 'Invalid or revoked API key' })
    return
  }
  req.mcpAuth = { keyId: key.id, effectiveScopes: resolveScopes(key.scopes) }
  // Fire-and-forget last_used_at update — never blocks the request
  setImmediate(() => touchApiKey(key.id))
  next()
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
pnpm test -- --reporter=verbose server/mcp/mcpAuth.test.ts
```

Expected: PASS

- [ ] **Step 5: Typecheck**

```bash
pnpm typecheck
```

Expected: zero errors

- [ ] **Step 6: Commit**

```bash
git add server/mcp/mcpAuth.ts server/mcp/mcpAuth.test.ts
git commit -m "feat(mcp): auth middleware with scope resolution"
```

---

## Task 4: MCP Server — `buildMcpServer`

**Files:**
- Create: `server/mcp/mcpServer.ts`

Note: This task has no unit tests (tool handlers are integration-tested in Task 7). The focus here is getting all 15 tools wired correctly. `pnpm typecheck` is the verification gate.

- [ ] **Step 1: Read the approve route logic (lines 357–415 of `server/routes/taskRoutes.ts`) to understand the bulk-grant pattern you'll replicate in the `approve_task` tool**

Key logic to replicate:
- Map `approval1` → `umsetzungskonzept`, `approval2` → `umsetzung`
- For `approval2`: fetch `umsetzungskonzept` latest run output, iterate `toolRequests`, call `createTaskPermission` for each not-yet-granted entry, append audit
- Call `updateTask(id, { currentStage: next })`

- [ ] **Step 2: Read the request-changes route (lines 428–482 of `server/routes/taskRoutes.ts`) to understand the feedback + stage regression pattern**

Key logic to replicate:
- Map `approval1` → `planning`, `approval2` → `umsetzungskonzept`
- Validate feedback length (1–4000 chars)
- Find last `done` stage_run on the regression stage, call `createFeedback`
- Call `updateTask(id, { currentStage: regressionStage })`
- Append audit

- [ ] **Step 3: Create `server/mcp/mcpServer.ts`**

```typescript
import type { McpScope, PipelineStage } from '../../src/types.js'
import type { PipelineOrchestrator } from '../pipeline/orchestrator.js'
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { z } from 'zod'
import { appendAudit, listAuditForTask } from '../db/auditRepo.js'
import { createApiKey, listApiKeys, revokeApiKey } from '../db/apiKeysRepo.js'
import { createHash, randomBytes } from 'node:crypto'
import { createFeedback } from '../db/feedbackRepo.js'
import { listPendingPermissionRequests, createTaskPermission, getPermissionRequestById, resolvePermissionRequest } from '../db/permissionsRepo.js'
import { getLatestStageRun, listStageRunsForTask } from '../db/stageRunsRepo.js'
import { createTask, deleteTask, getTaskById, getTaskBySlug, listTasks, listTasksByStage, updateTask } from '../db/tasksRepo.js'

function mcpError(message: string): never {
  throw { code: -32003, message }
}

function requireScope(scopes: Set<McpScope>, needed: McpScope): void {
  if (!scopes.has(needed))
    mcpError(`Insufficient scope: requires ${needed}`)
}

const VALID_STAGES = new Set<string>([
  'backlog','pruefung','refinement','planning','approval1',
  'umsetzungskonzept','approval2','umsetzung','selbstreview',
  'finalisierung','done','on_hold','cancelled',
])

export function buildMcpServer(orchestrator: PipelineOrchestrator, scopes: Set<McpScope>): McpServer {
  const server = new McpServer({ name: 'dashboard-tasks', version: '1.0.0' })

  // ─── tasks:read ───────────────────────────────────────────────

  server.tool(
    'list_tasks',
    'List all pipeline tasks, optionally filtered by stage',
    { stage: z.string().optional().describe('Filter by pipeline stage') },
    async ({ stage }) => {
      requireScope(scopes, 'tasks:read')
      if (stage && !VALID_STAGES.has(stage))
        mcpError(`Invalid stage: ${stage}`)
      const tasks = stage ? listTasksByStage(stage as PipelineStage) : listTasks()
      return { content: [{ type: 'text', text: JSON.stringify(tasks) }] }
    },
  )

  server.tool(
    'get_task',
    'Get a single task by ID or slug',
    { id_or_slug: z.string().describe('Task UUID or slug') },
    async ({ id_or_slug }) => {
      requireScope(scopes, 'tasks:read')
      const task = getTaskById(id_or_slug) ?? getTaskBySlug(id_or_slug)
      if (!task) mcpError(`Task not found: ${id_or_slug}`)
      return { content: [{ type: 'text', text: JSON.stringify(task) }] }
    },
  )

  server.tool(
    'list_stage_runs',
    'List all stage runs for a task',
    { task_id: z.string() },
    async ({ task_id }) => {
      requireScope(scopes, 'tasks:read')
      if (!getTaskById(task_id)) mcpError(`Task not found: ${task_id}`)
      return { content: [{ type: 'text', text: JSON.stringify(listStageRunsForTask(task_id)) }] }
    },
  )

  server.tool(
    'list_audit',
    'List audit log entries for a task',
    { task_id: z.string() },
    async ({ task_id }) => {
      requireScope(scopes, 'tasks:read')
      if (!getTaskById(task_id)) mcpError(`Task not found: ${task_id}`)
      return { content: [{ type: 'text', text: JSON.stringify(listAuditForTask(task_id)) }] }
    },
  )

  server.tool(
    'list_permission_requests',
    'List pending permission requests for a task (across all stage runs)',
    { task_id: z.string() },
    async ({ task_id }) => {
      requireScope(scopes, 'tasks:read')
      const runs = listStageRunsForTask(task_id)
      const requests = runs.flatMap(r => listPendingPermissionRequests(r.id))
      return { content: [{ type: 'text', text: JSON.stringify(requests) }] }
    },
  )

  // ─── tasks:write ───────────────────────────────────────────────

  const SLUG_RE = /^[a-z0-9][a-z0-9-]{0,63}$/

  server.tool(
    'create_task',
    'Create a new pipeline task',
    {
      slug: z.string().describe('Unique slug matching [a-z0-9][a-z0-9-]{0,63}'),
      title: z.string(),
      cwd: z.string().describe('Absolute working directory path'),
      description: z.string().optional(),
      priority: z.enum(['high', 'medium', 'low']).optional(),
      silverBullet: z.boolean().optional().describe('Jump-queue flag'),
      metadata: z.record(z.unknown()).optional(),
      sourceBranch: z.string().optional(),
      targetBranch: z.string().optional(),
      maxIterations: z.number().int().positive().optional(),
      tokenBudget: z.number().int().positive().optional(),
      costBudgetCents: z.number().int().positive().optional(),
    },
    async (args) => {
      requireScope(scopes, 'tasks:write')
      if (!SLUG_RE.test(args.slug)) mcpError('slug must match [a-z0-9][a-z0-9-]{0,63}')
      if (getTaskBySlug(args.slug)) mcpError(`slug already exists: ${args.slug}`)
      const task = createTask({
        slug: args.slug,
        title: args.title,
        description: args.description ?? null,
        cwd: args.cwd,
        sourceBranch: args.sourceBranch ?? null,
        targetBranch: args.targetBranch ?? null,
        metadata: args.metadata ?? null,
        silverBullet: args.silverBullet ?? false,
        priority: args.priority,
        maxIterations: args.maxIterations,
        tokenBudget: args.tokenBudget ?? null,
        costBudgetCents: args.costBudgetCents ?? null,
      })
      return { content: [{ type: 'text', text: JSON.stringify(task) }] }
    },
  )

  server.tool(
    'update_task',
    'Update whitelisted fields of an existing task',
    {
      id: z.string(),
      title: z.string().optional(),
      description: z.string().nullable().optional(),
      priority: z.enum(['high', 'medium', 'low']).optional(),
      silverBullet: z.boolean().optional(),
      maxIterations: z.number().int().positive().optional(),
      tokenBudget: z.number().int().positive().nullable().optional(),
      costBudgetCents: z.number().int().positive().nullable().optional(),
      metadata: z.record(z.unknown()).nullable().optional(),
    },
    async (args) => {
      requireScope(scopes, 'tasks:write')
      const { id, ...fields } = args
      const task = updateTask(id, fields)
      if (!task) mcpError(`Task not found: ${id}`)
      return { content: [{ type: 'text', text: JSON.stringify(task) }] }
    },
  )

  server.tool(
    'delete_task',
    'Delete a task and its associated data',
    { id: z.string() },
    async ({ id }) => {
      requireScope(scopes, 'tasks:write')
      const ok = deleteTask(id)
      if (!ok) mcpError(`Task not found: ${id}`)
      return { content: [{ type: 'text', text: JSON.stringify({ success: true }) }] }
    },
  )

  // ─── pipeline:control ─────────────────────────────────────────

  server.tool(
    'progress_task',
    'Advance a task to its next pipeline stage',
    { id: z.string() },
    async ({ id }) => {
      requireScope(scopes, 'pipeline:control')
      const stageRun = await orchestrator.progressTask(id)
      if (!stageRun) mcpError('Task cannot progress (terminal, not found, or no free runner slot)')
      const task = getTaskById(id)
      return { content: [{ type: 'text', text: JSON.stringify({ task, stageRun }) }] }
    },
  )

  server.tool(
    'approve_task',
    'Approve a task at an approval gate (approval1 or approval2)',
    { id: z.string() },
    async ({ id }) => {
      requireScope(scopes, 'pipeline:control')
      const task = getTaskById(id)
      if (!task) mcpError(`Task not found: ${id}`)
      const nextMap: Record<string, PipelineStage> = { approval1: 'umsetzungskonzept', approval2: 'umsetzung' }
      const next = nextMap[task.currentStage]
      if (!next) mcpError(`Task in stage ${task.currentStage} cannot be approved`)

      if (task.currentStage === 'approval2') {
        const konzeptRun = getLatestStageRun(task.id, 'umsetzungskonzept')
        const rawRequests = (konzeptRun?.output as Record<string, unknown> | null)?.toolRequests
        if (Array.isArray(rawRequests)) {
          const { listTaskPermissions } = await import('../db/permissionsRepo.js')
          const existing = listTaskPermissions(task.id)
          for (const req of rawRequests) {
            if (typeof req !== 'object' || req === null) continue
            const r = req as Record<string, unknown>
            const tool = typeof r.tool === 'string' ? r.tool.trim() : null
            const pattern = typeof r.pattern === 'string' && r.pattern.trim() ? r.pattern.trim() : null
            if (!tool) continue
            const alreadyGranted = existing.some(p => p.tool === tool && (p.pattern ?? null) === pattern && p.granted)
            if (alreadyGranted) continue
            createTaskPermission({ taskId: task.id, tool, pattern, granted: true, preApproved: true, decidedBy: 'user' })
          }
          appendAudit({ taskId: task.id, actor: 'user', action: 'bulk_granted_tool_permissions', details: { source: 'umsetzungskonzept_toolRequests', count: rawRequests.length } })
        }
      }
      updateTask(id, { currentStage: next })
      return { content: [{ type: 'text', text: JSON.stringify(getTaskById(id)) }] }
    },
  )

  server.tool(
    'request_changes',
    'Reject an approval artifact and regress the task with feedback',
    { id: z.string(), feedback: z.string().min(1).max(4000) },
    async ({ id, feedback }) => {
      requireScope(scopes, 'pipeline:control')
      const task = getTaskById(id)
      if (!task) mcpError(`Task not found: ${id}`)
      const stageMap: Record<string, 'planning' | 'umsetzungskonzept'> = { approval1: 'planning', approval2: 'umsetzungskonzept' }
      const regressionStage = stageMap[task.currentStage]
      if (!regressionStage) mcpError(`Task in stage ${task.currentStage} cannot receive change requests`)

      const priorRun = listStageRunsForTask(task.id)
        .filter(r => r.stage === regressionStage && r.status === 'done')
        .at(-1) ?? null

      const feedbackRow = createFeedback({ taskId: task.id, stage: regressionStage, stageRunId: priorRun?.id ?? null, feedback })
      updateTask(id, { currentStage: regressionStage })
      appendAudit({ taskId: task.id, actor: 'user', action: 'request_changes', details: { fromStage: task.currentStage, toStage: regressionStage, feedbackId: feedbackRow.id } })
      return { content: [{ type: 'text', text: JSON.stringify({ task: getTaskById(id), feedback: feedbackRow }) }] }
    },
  )

  server.tool(
    'cancel_task',
    'Cancel a task',
    { id: z.string() },
    async ({ id }) => {
      requireScope(scopes, 'pipeline:control')
      const task = getTaskById(id)
      if (!task) mcpError(`Task not found: ${id}`)
      if (task.currentStage === 'done' || task.currentStage === 'cancelled')
        mcpError(`Task is already ${task.currentStage}`)
      updateTask(id, { currentStage: 'cancelled' })
      appendAudit({ taskId: id, actor: 'user', action: 'cancelled' })
      return { content: [{ type: 'text', text: JSON.stringify(getTaskById(id)) }] }
    },
  )

  server.tool(
    'retry_task',
    'Retry a task whose latest stage run failed',
    { id: z.string() },
    async ({ id }) => {
      requireScope(scopes, 'pipeline:control')
      const task = getTaskById(id)
      if (!task) mcpError(`Task not found: ${id}`)
      const stageRun = await orchestrator.progressTask(id)
      if (!stageRun) mcpError('Task cannot be retried (check stage run status)')
      return { content: [{ type: 'text', text: JSON.stringify({ task: getTaskById(id), stageRun }) }] }
    },
  )

  server.tool(
    'grant_permission',
    'Pre-approve a tool for a task',
    { task_id: z.string(), tool: z.string(), pattern: z.string().optional() },
    async ({ task_id, tool, pattern }) => {
      requireScope(scopes, 'pipeline:control')
      if (!getTaskById(task_id)) mcpError(`Task not found: ${task_id}`)
      const perm = createTaskPermission({ taskId: task_id, tool, pattern: pattern ?? null, granted: true, preApproved: true, decidedBy: 'user' })
      return { content: [{ type: 'text', text: JSON.stringify(perm) }] }
    },
  )

  server.tool(
    'resolve_permission_request',
    'Grant or deny a runtime permission request from a stage agent',
    { request_id: z.string(), outcome: z.enum(['granted', 'denied']) },
    async ({ request_id, outcome }) => {
      requireScope(scopes, 'pipeline:control')
      const req = getPermissionRequestById(request_id)
      if (!req) mcpError(`Permission request not found: ${request_id}`)
      const resolved = resolvePermissionRequest(request_id, outcome)
      if (outcome === 'granted') {
        // Find the task via the stage run
        const { getStageRunById } = await import('../db/stageRunsRepo.js')
        const run = getStageRunById(req.stageRunId)
        if (run) {
          createTaskPermission({ taskId: run.taskId, tool: req.tool, pattern: req.pattern ?? null, granted: true, preApproved: false })
          await orchestrator.resumeFromUser(run.taskId)
        }
      }
      return { content: [{ type: 'text', text: JSON.stringify(resolved) }] }
    },
  )

  // ─── keys:manage ─────────────────────────────────────────────

  server.tool(
    'list_api_keys',
    'List all MCP API keys (active and revoked)',
    { include_revoked: z.boolean().optional() },
    async ({ include_revoked }) => {
      requireScope(scopes, 'keys:manage')
      return { content: [{ type: 'text', text: JSON.stringify(listApiKeys({ includeRevoked: include_revoked })) }] }
    },
  )

  server.tool(
    'create_api_key',
    'Create a new MCP API key — token is returned once and never stored',
    {
      name: z.string().describe('Unique human-readable name'),
      scopes: z.array(z.enum(['tasks:read', 'tasks:write', 'pipeline:control', 'keys:manage'])),
    },
    async (args) => {
      requireScope(scopes, 'keys:manage')
      const token = `mcp_${randomBytes(16).toString('hex')}`
      const keyHash = createHash('sha256').update(token).digest('hex')
      const key = createApiKey({ name: args.name, keyHash, scopes: args.scopes as McpScope[] })
      return { content: [{ type: 'text', text: JSON.stringify({ key, token }) }] }
    },
  )

  server.tool(
    'revoke_api_key',
    'Revoke an MCP API key by ID',
    { id: z.string() },
    async ({ id }) => {
      requireScope(scopes, 'keys:manage')
      const ok = revokeApiKey(id)
      if (!ok) mcpError(`API key not found: ${id}`)
      return { content: [{ type: 'text', text: JSON.stringify({ success: true }) }] }
    },
  )

  return server
}
```

- [ ] **Step 4: Typecheck**

```bash
pnpm typecheck
```

Expected: zero errors. Fix any type mismatches (e.g. `listTaskPermissions` import, `decidedBy` field if optional in repo).

- [ ] **Step 5: Commit**

```bash
git add server/mcp/mcpServer.ts
git commit -m "feat(mcp): buildMcpServer factory — 15 tools"
```

---

## Task 5: MCP Router + Wire into Express

**Files:**
- Create: `server/mcp/mcpRouter.ts`
- Modify: `server/index.ts`

- [ ] **Step 1: Create `server/mcp/mcpRouter.ts`**

```typescript
import type { PipelineOrchestrator } from '../pipeline/orchestrator.js'
import { Router } from 'express'
import { StreamableHTTPServerTransport } from '@modelcontextprotocol/sdk/server/streamableHttp.js'
import { mcpAuthMiddleware } from './mcpAuth.js'
import { buildMcpServer } from './mcpServer.js'

export function createMcpRouter(orchestrator: PipelineOrchestrator): Router {
  const router = Router()

  // A fresh McpServer per request so tool handlers close over authenticated
  // scopes. Stateless transport (no sessionIdGenerator) — state lives in SQLite.
  router.post('/mcp', mcpAuthMiddleware, async (req, res) => {
    const { effectiveScopes } = req.mcpAuth!
    const server = buildMcpServer(orchestrator, effectiveScopes)
    const transport = new StreamableHTTPServerTransport({ sessionIdGenerator: undefined })
    await server.connect(transport)
    await transport.handleRequest(req, res, req.body)
  })

  return router
}
```

- [ ] **Step 2: Mount the MCP router in `server/index.ts`**

Open `server/index.ts`. Find the block where `createTaskRouter` is mounted (around line 307):

```typescript
app.use('/api', createTaskRouter({ ... }))
```

Add the import and mounting just before or after that block:

```typescript
import { createMcpRouter } from './mcp/mcpRouter.js'
// ...
app.use('/api', createMcpRouter(orchestrator))
```

- [ ] **Step 3: Typecheck**

```bash
pnpm typecheck
```

Expected: zero errors

- [ ] **Step 4: Smoke-test the MCP endpoint manually**

```bash
# Start the server
pnpm dev

# Without auth → 401
curl -s -X POST http://localhost:13120/api/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' | head -20
```

Expected: `{"error":"Missing or invalid Authorization header"}`

- [ ] **Step 5: Run full test suite**

```bash
pnpm test
```

Expected: all tests pass

- [ ] **Step 6: Commit**

```bash
git add server/mcp/mcpRouter.ts server/index.ts
git commit -m "feat(mcp): mount MCP router at POST /api/mcp"
```

---

## Task 6: REST Routes for API Key Management (Dashboard UI)

**Files:**
- Create: `server/routes/apiKeyRoutes.ts`
- Modify: `server/index.ts`

- [ ] **Step 1: Create `server/routes/apiKeyRoutes.ts`**

```typescript
import { createHash, randomBytes } from 'node:crypto'
import { Router } from 'express'
import type { McpScope } from '../../src/types.js'
import { createApiKey, getApiKeyById, listApiKeys, revokeApiKey } from '../db/apiKeysRepo.js'

const VALID_SCOPES = new Set<McpScope>(['tasks:read', 'tasks:write', 'pipeline:control', 'keys:manage'])

const GROUP_SCOPES: Record<string, McpScope[]> = {
  viewer: ['tasks:read'],
  operator: ['tasks:read', 'pipeline:control'],
  developer: ['tasks:read', 'tasks:write', 'pipeline:control'],
  admin: ['tasks:read', 'tasks:write', 'pipeline:control', 'keys:manage'],
}

export function createApiKeyRouter(): Router {
  const router = Router()

  // List all keys (include_revoked=true for full list)
  router.get('/settings/api-keys', (req, res) => {
    const includeRevoked = req.query.include_revoked === 'true'
    res.json(listApiKeys({ includeRevoked }))
  })

  // Create a new key — returns the key + the plain token (once only)
  router.post('/settings/api-keys', (req, res) => {
    const { name, scopes, group } = req.body ?? {}
    if (!name || typeof name !== 'string' || !name.trim()) {
      res.status(400).json({ error: 'name is required' })
      return
    }

    let resolvedScopes: McpScope[]
    if (group && typeof group === 'string') {
      resolvedScopes = GROUP_SCOPES[group]
      if (!resolvedScopes) {
        res.status(400).json({ error: `Unknown group: ${group}. Valid: ${Object.keys(GROUP_SCOPES).join(', ')}` })
        return
      }
    }
    else if (Array.isArray(scopes)) {
      const invalid = scopes.find((s: unknown) => typeof s !== 'string' || !VALID_SCOPES.has(s as McpScope))
      if (invalid) {
        res.status(400).json({ error: `Invalid scope: ${invalid}` })
        return
      }
      resolvedScopes = scopes as McpScope[]
    }
    else {
      res.status(400).json({ error: 'Either group or scopes array is required' })
      return
    }

    try {
      const token = `mcp_${randomBytes(16).toString('hex')}`
      const keyHash = createHash('sha256').update(token).digest('hex')
      const key = createApiKey({ name: name.trim(), keyHash, scopes: resolvedScopes })
      res.status(201).json({ key, token })
    }
    catch (err) {
      const msg = (err as Error).message
      if (msg.includes('UNIQUE constraint'))
        res.status(409).json({ error: 'An API key with this name already exists' })
      else
        res.status(500).json({ error: msg })
    }
  })

  // Revoke a key by ID
  router.delete('/settings/api-keys/:id', (req, res) => {
    const key = getApiKeyById(req.params.id)
    if (!key) {
      res.status(404).json({ error: 'API key not found' })
      return
    }
    revokeApiKey(req.params.id)
    res.status(204).end()
  })

  return router
}
```

- [ ] **Step 2: Mount the router in `server/index.ts`**

Import and mount near the other route registrations:

```typescript
import { createApiKeyRouter } from './routes/apiKeyRoutes.js'
// ...
app.use('/api', createApiKeyRouter())
```

- [ ] **Step 3: Typecheck**

```bash
pnpm typecheck
```

Expected: zero errors

- [ ] **Step 4: Manual smoke-test**

```bash
# Start server
pnpm dev

# List keys (empty)
curl -s http://localhost:13120/api/settings/api-keys
# Expected: []

# Create a developer key
curl -s -X POST http://localhost:13120/api/settings/api-keys \
  -H "Content-Type: application/json" \
  -d '{"name":"test-key","group":"developer"}'
# Expected: { "key": {...}, "token": "mcp_..." }

# Save the token, then use it against the MCP endpoint
TOKEN="mcp_..."
curl -s -X POST http://localhost:13120/api/mcp \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
# Expected: JSON list of available tools
```

- [ ] **Step 5: Commit**

```bash
git add server/routes/apiKeyRoutes.ts server/index.ts
git commit -m "feat(mcp): REST endpoints for API key management"
```

---

## Task 7: Internal Agent Token Injection

**Files:**
- Modify: `server/pipeline/agentSpawner.ts`

Stage agents spawned by the orchestrator automatically receive a short-lived MCP token so they can call the MCP API without being manually configured.

- [ ] **Step 1: Read current `buildSpawnEnv` (lines 85–91 of `server/pipeline/agentSpawner.ts`)**

Current implementation:
```typescript
export function buildSpawnEnv(opts: SpawnAgentOptions): NodeJS.ProcessEnv {
  return {
    ...process.env,
    DASHBOARD_STAGE_RUN_ID: opts.stageRun.id,
    DASHBOARD_TASK_ID: opts.task.id,
  }
}
```

- [ ] **Step 2: Extend `SpawnAgentOptions` to accept optional MCP credentials**

In `server/pipeline/agentSpawner.ts`, extend the `SpawnAgentOptions` interface:

```typescript
export interface SpawnAgentOptions {
  task: PipelineTask
  stageRun: StageRun
  prompt: string
  systemPrompt?: string
  model?: string
  permissions: TaskPermission[]
  enableChannel?: boolean
  resumeSessionId?: string | null
  mcpToken?: string | null        // ← add
  mcpUrl?: string | null          // ← add
}
```

- [ ] **Step 3: Update `buildSpawnEnv` to inject the MCP credentials when present**

```typescript
export function buildSpawnEnv(opts: SpawnAgentOptions): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = {
    ...process.env,
    DASHBOARD_STAGE_RUN_ID: opts.stageRun.id,
    DASHBOARD_TASK_ID: opts.task.id,
  }
  if (opts.mcpToken) env.DASHBOARD_MCP_TOKEN = opts.mcpToken
  if (opts.mcpUrl) env.DASHBOARD_MCP_URL = opts.mcpUrl
  return env
}
```

- [ ] **Step 4: Generate and inject token in `stageHandlers.ts` inside `createAgentStage`**

Open `server/pipeline/stageHandlers.ts`. Find where `spawnStageAgent` is called inside `createAgentStage`. Before the spawn call, generate a key and after the spawn succeeds schedule revocation.

Add at the top of the file:
```typescript
import { createHash, randomBytes } from 'node:crypto'
import { createApiKey, revokeApiKey } from '../db/apiKeysRepo.js'
```

Inside `createAgentStage`, before calling `spawnStageAgent`, add:
```typescript
// Generate a short-lived operator-scope token for this stage run
const rawToken = `mcp_${randomBytes(16).toString('hex')}`
const keyHash = createHash('sha256').update(rawToken).digest('hex')
const mcpKey = createApiKey({
  name: `stage-run:${ctx.stageRun.id}`,
  keyHash,
  scopes: ['tasks:read', 'pipeline:control'],
})
const dashboardPort = process.env.DASHBOARD_PORT ?? '13120'
const mcpUrl = `http://127.0.0.1:${dashboardPort}/api/mcp`
```

Pass to spawn:
```typescript
const result = spawnStageAgent({
  ...existingOpts,
  mcpToken: rawToken,
  mcpUrl,
})
```

After the stage run ends (in the completion or finalization path), revoke the key:
```typescript
revokeApiKey(mcpKey.id)
```

Note: The exact insertion point depends on `createAgentStage`'s structure. Read `server/pipeline/stageHandlers.ts` carefully before editing — add the generate/revoke calls adjacent to the spawn call.

- [ ] **Step 5: Typecheck**

```bash
pnpm typecheck
```

Expected: zero errors

- [ ] **Step 6: Run existing tests**

```bash
pnpm test
```

Expected: all pass (spawner tests use `buildSpawnEnv` directly — the new optional fields don't break existing call sites)

- [ ] **Step 7: Commit**

```bash
git add server/pipeline/agentSpawner.ts server/pipeline/stageHandlers.ts
git commit -m "feat(mcp): inject DASHBOARD_MCP_TOKEN into spawned stage agents"
```

---

## Task 8: Dashboard UI — ApiKeySettings.vue

**Files:**
- Create: `src/components/ApiKeySettings.vue`
- Modify: `src/App.vue`

- [ ] **Step 1: Create `src/components/ApiKeySettings.vue`**

```vue
<script setup lang="ts">
import type { ApiKey } from '../types'
import { ref, onMounted } from 'vue'

const keys = ref<ApiKey[]>([])
const showCreate = ref(false)
const newName = ref('')
const newGroup = ref<'viewer' | 'operator' | 'developer' | 'admin'>('developer')
const createdToken = ref<string | null>(null)
const isCreating = ref(false)
const error = ref<string | null>(null)

async function loadKeys() {
  const res = await fetch('/api/settings/api-keys?include_revoked=true')
  keys.value = await res.json()
}

async function createKey() {
  error.value = null
  isCreating.value = true
  try {
    const res = await fetch('/api/settings/api-keys', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: newName.value.trim(), group: newGroup.value }),
    })
    if (!res.ok) {
      const { error: msg } = await res.json()
      error.value = msg
      return
    }
    const { token } = await res.json()
    createdToken.value = token
    newName.value = ''
    showCreate.value = false
    await loadKeys()
  }
  finally {
    isCreating.value = false
  }
}

async function revokeKey(id: string) {
  if (!confirm('Revoke this API key? This cannot be undone.')) return
  await fetch(`/api/settings/api-keys/${id}`, { method: 'DELETE' })
  await loadKeys()
}

function copyToken() {
  if (createdToken.value) navigator.clipboard.writeText(createdToken.value)
}

onMounted(loadKeys)
</script>

<template>
  <div class="api-key-settings">
    <div class="settings-header">
      <h2>MCP API Keys</h2>
      <button class="btn-primary" @click="showCreate = !showCreate">
        {{ showCreate ? 'Cancel' : '+ New Key' }}
      </button>
    </div>

    <div v-if="createdToken" class="token-reveal">
      <p><strong>Copy your token — it will not be shown again:</strong></p>
      <div class="token-box">
        <code>{{ createdToken }}</code>
        <button @click="copyToken">Copy</button>
      </div>
      <button class="btn-secondary" @click="createdToken = null">Dismiss</button>
    </div>

    <form v-if="showCreate" class="create-form" @submit.prevent="createKey">
      <label>
        Name
        <input v-model="newName" type="text" required placeholder="e.g. ci-pipeline" />
      </label>
      <label>
        Group
        <select v-model="newGroup">
          <option value="viewer">viewer — tasks:read</option>
          <option value="operator">operator — tasks:read + pipeline:control</option>
          <option value="developer">developer — tasks:read + tasks:write + pipeline:control</option>
          <option value="admin">admin — all scopes</option>
        </select>
      </label>
      <p v-if="error" class="error">{{ error }}</p>
      <button type="submit" :disabled="isCreating">
        {{ isCreating ? 'Creating…' : 'Create Key' }}
      </button>
    </form>

    <table class="keys-table">
      <thead>
        <tr>
          <th>Name</th>
          <th>Scopes</th>
          <th>Created</th>
          <th>Last Used</th>
          <th>Status</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="key in keys" :key="key.id" :class="{ revoked: !key.active }">
          <td>{{ key.name }}</td>
          <td><span v-for="s in key.scopes" :key="s" class="scope-badge">{{ s }}</span></td>
          <td>{{ new Date(key.createdAt).toLocaleDateString() }}</td>
          <td>{{ key.lastUsedAt ? new Date(key.lastUsedAt).toLocaleDateString() : '—' }}</td>
          <td><span :class="key.active ? 'badge-active' : 'badge-revoked'">{{ key.active ? 'Active' : 'Revoked' }}</span></td>
          <td>
            <button v-if="key.active" class="btn-danger-sm" @click="revokeKey(key.id)">Revoke</button>
          </td>
        </tr>
        <tr v-if="keys.length === 0">
          <td colspan="6" class="empty">No API keys yet.</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.api-key-settings { padding: 1.5rem; max-width: 900px; }
.settings-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 1rem; }
.token-reveal { background: var(--color-surface, #1e2a1e); border: 1px solid #4caf50; border-radius: 6px; padding: 1rem; margin-bottom: 1rem; }
.token-box { display: flex; gap: 0.5rem; align-items: center; margin: 0.5rem 0; }
.token-box code { flex: 1; font-size: 0.85rem; word-break: break-all; }
.create-form { display: flex; flex-direction: column; gap: 0.75rem; background: var(--color-surface, #1a1a1a); padding: 1rem; border-radius: 6px; margin-bottom: 1rem; }
.create-form label { display: flex; flex-direction: column; gap: 0.25rem; font-size: 0.875rem; }
.keys-table { width: 100%; border-collapse: collapse; font-size: 0.875rem; }
.keys-table th { text-align: left; padding: 0.5rem; border-bottom: 1px solid var(--color-border, #333); }
.keys-table td { padding: 0.5rem; border-bottom: 1px solid var(--color-border, #222); }
.keys-table tr.revoked { opacity: 0.5; }
.scope-badge { display: inline-block; font-size: 0.75rem; padding: 0.1rem 0.4rem; border-radius: 3px; background: var(--color-surface, #2a2a2a); margin-right: 0.25rem; }
.badge-active { color: #4caf50; }
.badge-revoked { color: #888; }
.btn-primary { padding: 0.4rem 0.8rem; border-radius: 4px; }
.btn-secondary { padding: 0.3rem 0.6rem; font-size: 0.8rem; }
.btn-danger-sm { color: #e57373; background: none; border: 1px solid #e57373; border-radius: 3px; padding: 0.2rem 0.5rem; cursor: pointer; font-size: 0.8rem; }
.empty { text-align: center; color: #666; padding: 2rem; }
.error { color: #e57373; font-size: 0.875rem; }
</style>
```

- [ ] **Step 2: Add Settings view to `src/App.vue`**

Open `src/App.vue`. Find the `<script setup>` block. Add:

```typescript
import ApiKeySettings from './components/ApiKeySettings.vue'
const showSettings = ref(false)
```

In the header template, add a Settings button next to the theme button:

```html
<button class="toggle-btn" :class="{ active: showSettings }" @click="showSettings = !showSettings; viewMode = showSettings ? viewMode : viewMode">
  Settings
</button>
```

In the main content area, add a conditional before the existing view blocks:

```html
<ApiKeySettings v-if="showSettings" />
<template v-else>
  <!-- existing view switch content -->
</template>
```

Adjust the exact insertion point to match App.vue's existing template structure.

- [ ] **Step 3: Start dev server and verify the Settings page renders**

```bash
pnpm dev
```

Navigate to `http://localhost:13120`. Click "Settings" → verify the API Key management page loads with an empty table and a "New Key" button.

Create a key, verify the token is shown in the reveal box and can be copied. Verify the key appears in the table. Click Revoke, verify it grays out.

- [ ] **Step 4: Run typecheck + tests**

```bash
pnpm typecheck && pnpm test
```

Expected: zero errors, all tests pass

- [ ] **Step 5: Commit**

```bash
git add src/components/ApiKeySettings.vue src/App.vue
git commit -m "feat(mcp): API key settings UI"
```

---

## Task 9: Final Integration Test + CLAUDE.md Update

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Full end-to-end integration test**

```bash
# 1. Start the dev server
pnpm dev

# 2. Create an admin key via the UI
#    → Navigate to Settings → New Key → name: "e2e-test" → admin → Create
#    → Copy the token

# 3. Tools list
TOKEN="mcp_..."
curl -s -X POST http://localhost:13120/api/mcp \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' | python3 -m json.tool | grep '"name"'
# Expected: list of 15 tool names

# 4. Create a task
curl -s -X POST http://localhost:13120/api/mcp \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"create_task","arguments":{"slug":"e2e-test-task","title":"E2E Test Task","cwd":"/tmp"}}}' | python3 -m json.tool
# Expected: task object with currentStage="backlog"

# 5. List tasks — verify it appears
curl -s -X POST http://localhost:13120/api/mcp \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_tasks","arguments":{}}}' | python3 -m json.tool | grep '"slug"'
# Expected: "e2e-test-task" in output

# 6. Scope enforcement: create a viewer key + verify it can't create_task
curl -s -X POST http://localhost:13120/api/settings/api-keys \
  -H "Content-Type: application/json" \
  -d '{"name":"viewer-test","group":"viewer"}' | python3 -m json.tool
# Save viewer token
VIEWER_TOKEN="mcp_..."
curl -s -X POST http://localhost:13120/api/mcp \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $VIEWER_TOKEN" \
  -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"create_task","arguments":{"slug":"blocked","title":"Blocked","cwd":"/tmp"}}}' | python3 -m json.tool
# Expected: error containing "Insufficient scope"

# 7. Invalid token → 401
curl -s -X POST http://localhost:13120/api/mcp \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer mcp_invalid" \
  -d '{"jsonrpc":"2.0","id":5,"method":"tools/list","params":{}}' | python3 -m json.tool
# Expected: {"error":"Invalid or revoked API key"}
```

- [ ] **Step 2: Run full test suite one final time**

```bash
pnpm typecheck && pnpm test
```

Expected: zero errors, all tests pass

- [ ] **Step 3: Update `CLAUDE.md` with MCP architecture section**

In `CLAUDE.md`, in the "Task Pipeline Architecture" section, add after the "Channel bridge" paragraph:

```markdown
**MCP server** (`server/mcp/`):
- `mcpServer.ts` — `buildMcpServer(orchestrator, scopes)` factory producing a `McpServer` with 15 tools gated by the caller's effective scope set.
- `mcpRouter.ts` — Express router: `POST /api/mcp` runs auth middleware then instantiates a fresh `McpServer` + `StreamableHTTPServerTransport` per request (stateless mode).
- `mcpAuth.ts` — `mcpAuthMiddleware` validates `Bearer mcp_<token>` via SHA-256 → `api_keys` table; `resolveScopes` expands implied scopes (write→read, control→read, manage→all).

**MCP API key management** (`server/db/apiKeysRepo.ts`, `server/routes/apiKeyRoutes.ts`):
- Token format: `mcp_<32 random hex>` — plain token returned once on creation, only SHA-256 hash stored.
- REST: `GET/POST /api/settings/api-keys`, `DELETE /api/settings/api-keys/:id` — used by the dashboard UI.
- Scopes: `tasks:read | tasks:write | pipeline:control | keys:manage` with implied hierarchy.

**Internal agent MCP access:** Stage agents spawned by `stageHandlers.ts` receive `DASHBOARD_MCP_TOKEN` + `DASHBOARD_MCP_URL` env vars pointing to `POST /api/mcp`. The token is a short-lived `operator`-scope key (`tasks:read + pipeline:control`) named `stage-run:{stageRunId}`, revoked when the run ends.
```

- [ ] **Step 4: Final commit**

```bash
git add CLAUDE.md
git commit -m "docs: document MCP server, API key system, and internal agent MCP access"
```

---

## Self-Review Checklist

**Spec coverage:**
- [x] API key table + hash storage — Task 1 + 2
- [x] Scope model with implied scopes — Task 3
- [x] Auth middleware — Task 3
- [x] All 15 MCP tools — Task 4
- [x] Streamable HTTP transport — Task 5
- [x] REST endpoints for key management — Task 6
- [x] Internal agent token injection — Task 7
- [x] Dashboard UI — Task 8
- [x] CLAUDE.md update — Task 9

**Type consistency check:**
- `createApiKey` in apiKeysRepo takes `{ name, keyHash, scopes }` — used identically in mcpServer.ts and apiKeyRoutes.ts ✓
- `resolveScopes(key.scopes)` in mcpAuth.ts — `key.scopes` is `McpScope[]` from `rowToApiKey` ✓
- `buildMcpServer(orchestrator, effectiveScopes)` — `effectiveScopes` is `Set<McpScope>` set by mcpAuthMiddleware, read via `req.mcpAuth!.effectiveScopes` ✓
- `revokeApiKey(id)` called in apiKeyRoutes.ts and stageHandlers.ts — signature `(id: string) => boolean` ✓
- `touchApiKey(id)` called in mcpAuth.ts — `setImmediate` wrapper, no return value needed ✓
