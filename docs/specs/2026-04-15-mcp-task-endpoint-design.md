# MCP Task Endpoint — Design Spec

**Date:** 2026-04-15  
**Status:** Approved  
**Author:** Claude (brainstorming session with Alexander Wink)

---

## Problem

The task pipeline is currently only operable through the browser dashboard. External Claude agents (in other projects, CI/CD pipelines) and internally spawned stage agents have no structured, machine-readable interface to create tasks, steer them through stages, correct errors, or observe state. The REST API is browser-only (CORS-restricted, no auth tokens).

## Goal

Add a fully functional MCP server at `POST /api/mcp` that exposes the entire task lifecycle to any Claude agent, secured by named API keys with hierarchical scopes. Agents can create tasks, progress them through the pipeline, provide feedback, and complete them — all via MCP tools.

---

## Architecture

### Transport

**Streamable HTTP** (`@modelcontextprotocol/sdk` `StreamableHTTPServerTransport`) mounted as an Express route on `POST /api/mcp`. This is the modern MCP standard (post-2024) and keeps everything inside the already-running Express server on port 13120.

Stateless mode (`sessionIdGenerator: undefined`) — no server-side session map. Every MCP request is self-contained. Correct for a pipeline API where state lives in SQLite, not in MCP sessions.

```
External Agent / Stage Agent
        │
        │  HTTP POST /api/mcp
        │  Authorization: Bearer mcp_<token>
        ▼
  Express (server/index.ts:13120)
        │
        ├─ Auth middleware (mcpAuth.ts)
        │    ├─ SHA-256 hash token → lookup api_keys
        │    ├─ Resolve effective scopes (implied)
        │    └─ Attach { keyId, scopes } to request
        │
        └─ StreamableHTTPServerTransport
               └─ McpServer (mcpServer.ts)
                    ├─ tool: list_tasks       → db/tasksRepo
                    ├─ tool: progress_task    → orchestrator.progressTask()
                    └─ ... (15 tools total)
```

### New Files

```
server/db/apiKeysRepo.ts         — CRUD for api_keys table
server/mcp/mcpServer.ts          — McpServer instance + all 15 tool registrations
server/mcp/mcpRouter.ts          — Express Router: POST /api/mcp
server/mcp/mcpAuth.ts            — Bearer token validation, scope resolution
server/routes/apiKeyRoutes.ts    — REST for dashboard UI key management
src/components/ApiKeySettings.vue — Dashboard settings page
```

### Modified Files

```
server/db/schema.ts              — Add api_keys DDL
server/index.ts                  — Mount mcpRouter + apiKeyRoutes; inject token into spawner
server/pipeline/agentSpawner.ts  — Accept DASHBOARD_MCP_TOKEN, inject into env
src/App.vue                      — Add Settings nav + route
src/types.ts                     — Add ApiKey interface
```

---

## API Key System

### Database Schema

```sql
CREATE TABLE IF NOT EXISTS api_keys (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL UNIQUE,     -- human-readable, e.g. "ci-pipeline"
  key_hash     TEXT NOT NULL UNIQUE,     -- SHA-256 of raw token (never store plain)
  scopes       TEXT NOT NULL,            -- JSON array of atomic scope strings
  active       INTEGER NOT NULL DEFAULT 1,
  created_at   TEXT NOT NULL,
  last_used_at TEXT                      -- updated async on each successful auth
);
```

**Token format:** `mcp_<32 random hex chars>`  
Generated fresh, stored hashed, returned exactly once on creation (shown in a copy-to-clipboard modal in the dashboard).

### Scope Model

**Atomic scopes:**

| Scope | Covers |
|---|---|
| `tasks:read` | list_tasks, get_task, list_stage_runs, list_audit, list_permission_requests |
| `tasks:write` | create_task, update_task, delete_task |
| `pipeline:control` | progress_task, approve_task, request_changes, cancel_task, retry_task, grant_permission, resolve_permission_request |
| `keys:manage` | list_api_keys, create_api_key, revoke_api_key |

**Implied scope rules (write implies read):**
- `tasks:write` → also grants `tasks:read`
- `pipeline:control` → also grants `tasks:read`
- `keys:manage` → grants all other scopes

**Preset groups for quick key creation:**

| Group | Scopes granted |
|---|---|
| `viewer` | tasks:read |
| `operator` | tasks:read, pipeline:control |
| `developer` | tasks:read, tasks:write, pipeline:control |
| `admin` | all scopes |

### Auth Middleware Flow

```
1. Extract: Authorization: Bearer mcp_<token>
2. SHA-256 hash token → query api_keys WHERE key_hash=? AND active=1
3. Not found → HTTP 401 before MCP is involved
4. Expand scopes: resolve implied scopes (e.g. tasks:write → add tasks:read)
5. Attach { keyId, effectiveScopes } to Express request
6. Fire-and-forget: UPDATE api_keys SET last_used_at=now WHERE id=?
7. Per tool call: hasScopeFor(toolName, effectiveScopes)
   → false: MCP error { code: -32003, message: "Insufficient scope: requires <scope>" }
```

---

## MCP Tools (15 total)

### tasks:read (5 tools)

| Tool | Parameters | Returns |
|---|---|---|
| `list_tasks` | `stage?: PipelineStage` | `PipelineTask[]` |
| `get_task` | `id_or_slug: string` | `PipelineTask` |
| `list_stage_runs` | `task_id: string` | `StageRun[]` |
| `list_audit` | `task_id: string` | `AuditEntry[]` |
| `list_permission_requests` | `task_id: string` | `PermissionRequest[]` |

### tasks:write (3 tools)

| Tool | Parameters | Returns |
|---|---|---|
| `create_task` | `slug, title, cwd, description?, priority?, silverBullet?, metadata?, useWorktree?, sourceBranch?, targetBranch?, maxIterations?, tokenBudget?, costBudgetCents?` | `PipelineTask` |
| `update_task` | `id, fields: Partial<UpdateTaskInput>` | `PipelineTask` |
| `delete_task` | `id: string` | `{ success: true }` |

### pipeline:control (7 tools)

| Tool | Parameters | Returns |
|---|---|---|
| `progress_task` | `id: string` | `{ task, stageRun }` |
| `approve_task` | `id: string` | `{ task }` |
| `request_changes` | `id: string, feedback: string` | `{ task }` |
| `cancel_task` | `id: string` | `{ task }` |
| `retry_task` | `id: string` | `{ task, stageRun }` |
| `grant_permission` | `task_id: string, tool: string, pattern?: string` | `TaskPermission` |
| `resolve_permission_request` | `request_id: string, outcome: 'granted'\|'denied'` | `PermissionRequest` |

### keys:manage (3 tools) — requires keys:manage scope

| Tool | Parameters | Returns |
|---|---|---|
| `list_api_keys` | — | `ApiKey[]` (no key_hash) |
| `create_api_key` | `name: string, scopes: MpcScope[]` | `{ key: ApiKey, token: string }` |
| `revoke_api_key` | `id: string` | `{ success: true }` |

---

## MCP Server Implementation Pattern

```typescript
// server/mcp/mcpRouter.ts
// A new McpServer is created per request so tool handlers can close over
// the authenticated scopes without shared mutable state.
export function createMcpRouter(orchestrator: PipelineOrchestrator): Router {
  const router = Router()
  router.post('/mcp', mcpAuthMiddleware, async (req, res) => {
    const { effectiveScopes } = req.mcpAuth  // set by mcpAuthMiddleware
    const server = buildMcpServer(orchestrator, effectiveScopes)
    const transport = new StreamableHTTPServerTransport({ sessionIdGenerator: undefined })
    await server.connect(transport)
    await transport.handleRequest(req, res, req.body)
  })
  return router
}

// server/mcp/mcpServer.ts
// buildMcpServer(orchestrator, scopes) creates a McpServer with tool handlers
// that close over the authenticated scope set. Tools without the required scope
// throw an MCP error ({ code: -32003 }) before touching the DB.
export function buildMcpServer(
  orchestrator: PipelineOrchestrator,
  scopes: Set<McpScope>
): McpServer {
  const server = new McpServer({ name: 'dashboard-tasks', version: '1.0.0' })

  if (scopes.has('tasks:read')) {
    server.tool('list_tasks', { stage: z.string().optional() }, async ({ stage }) => {
      const tasks = stage ? listTasksByStage(stage as PipelineStage) : listTasks()
      return { content: [{ type: 'text', text: JSON.stringify(tasks) }] }
    })
    // ... other tasks:read tools
  }
  // ... other scope blocks
  return server
}
```

---

## Internal Agent Token Injection

Stage agents spawned by the orchestrator automatically receive a short-lived MCP token so they can use the MCP API without manual configuration.

**Flow in `agentSpawner.ts`:**
1. Before spawn: generate `mcp_<token>`, insert into api_keys with name `stage-run:{stageRunId}`, scopes `['tasks:read', 'pipeline:control']`, active=1
2. Inject into spawned agent env: `DASHBOARD_MCP_TOKEN=mcp_<token>`, `DASHBOARD_MCP_URL=http://127.0.0.1:{port}/api/mcp`
3. On stage-run completion or failure: `revokeApiKey(keyId)` (active=0)

The spawned agent can read `DASHBOARD_MCP_TOKEN` and `DASHBOARD_MCP_URL` to self-configure the MCP client.

---

## Dashboard UI — API Key Settings

New `ApiKeySettings.vue` component accessible from a Settings nav item in `App.vue`:

**Key list table:**
- Columns: Name | Group/Scopes | Created | Last Used | Status | Actions
- Rows: active keys show "Active" badge; revoked keys show "Revoked" and are grayed out

**Create key dialog:**
- Name input (required, must be unique)
- Group dropdown: viewer / operator / developer / admin
- Advanced: manual scope picker (checkboxes per atomic scope)
- On submit: POST `/api/settings/api-keys` → token shown once in copy-to-clipboard modal

**Revoke:**
- Confirmation prompt → DELETE `/api/settings/api-keys/:id` → row grays out immediately (optimistic update)

---

## Layering

Strictly follows existing layering rules — no upward imports:

```
server/db/apiKeysRepo.ts         ← node:*, better-sqlite3, src/types.ts only
server/mcp/*                     ← db/*, pipeline/orchestrator (type-only), src/types.ts
server/routes/apiKeyRoutes.ts    ← db/apiKeysRepo only
server/index.ts                  ← composition root: wires everything
```

---

## Verification

1. `pnpm typecheck` — zero errors
2. `pnpm test` — existing tests pass; new unit tests for:
   - `apiKeysRepo`: create, lookup by hash, revoke
   - `mcpAuth`: scope expansion (write→read implied), hasScopeFor edge cases
3. Manual E2E:
   - Create admin key via dashboard UI → copy token
   - Configure Claude agent: `{ url: "http://localhost:13120/api/mcp", headers: { Authorization: "Bearer mcp_..." } }`
   - `create_task` → task appears in kanban
   - `progress_task` → stage run starts
   - `list_stage_runs` → running run visible
   - `list_api_keys` with operator-scope key → MCP error "Insufficient scope"
4. Internal agent: spawn stage via dashboard → confirm `DASHBOARD_MCP_TOKEN` in process environment
