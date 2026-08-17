# Bun Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate the runtime from Node.js + tsx to Bun, replace `better-sqlite3` with `bun:sqlite`, split `server/index.ts` into focused route files, and enable single-binary deployment.

**Architecture:** Drop-in runtime swap — all `node:*` APIs, Express 5, and the Vue/Vite frontend are unchanged. SQLite type imports are migrated mechanically across the DB layer. `server/index.ts` (700 lines, 24 inline route handlers) is split into `middleware.ts`, `authRoutes.ts`, `agentRoutes.ts`, and `systemRoutes.ts`; only SSE state management and orchestrator wiring stay in `index.ts`.

**Tech Stack:** Bun 1.x, `bun:sqlite` (built-in), Express 5, Vue 3 + Vite (unchanged), TypeScript (natively compiled by Bun).

---

## File Map

| Action | File | Responsibility |
|--------|------|---------------|
| Modify | `package.json` | Update scripts, remove tsx, add bun-types |
| Create | `.bun-version` | Pin Bun version |
| Create | `server/bun-env.d.ts` | Triple-slash bun-types reference |
| Modify | `server/db/client.ts` | Replace better-sqlite3 with bun:sqlite, extract migration fns |
| Modify | `server/db/*.ts` (9 files) | Update `import type { Database }` source |
| Modify | `server/constants.ts` | Add UUID_RE (moved from index.ts) |
| Create | `server/middleware.ts` | `requireApiToken` + `createRejectCrossOrigin` |
| Create | `server/routes/authRoutes.ts` | `/auth/*` + `/api/me` |
| Create | `server/routes/agentRoutes.ts` | `/api/agents/*`, `/api/sessions`, `/api/channel-reply` |
| Create | `server/routes/systemRoutes.ts` | `/api/config`, `/api/system` |
| Modify | `server/index.ts` | Composition root only: SSE state, orchestrator wiring, router mounts |

---

## Task 1: Bun Tooling Setup

**Files:**
- Modify: `package.json`
- Create: `.bun-version`
- Create: `server/bun-env.d.ts`

- [ ] **Step 1: Install Bun globally (if not present)**

```bash
curl -fsSL https://bun.sh/install | bash
bun --version
```

Expected: prints `1.x.x`

- [ ] **Step 2: Pin Bun version**

Run `bun --version` and create `.bun-version` with that exact version:

```
1.2.13
```
(Replace with the output of `bun --version`)

- [ ] **Step 3: Add bun-types dev dependency**

```bash
bun add -d bun-types
```

- [ ] **Step 4: Create bun-env.d.ts for TypeScript type resolution**

Create `server/bun-env.d.ts`:
```ts
/// <reference types="bun-types" />
```

This makes `bun:sqlite` and other Bun globals available to TypeScript without overriding the existing `@types/*` auto-resolution in tsconfig.

- [ ] **Step 5: Update package.json scripts and remove tsx**

Replace the `scripts` block and `devDependencies` tsx entry in `package.json`:

```json
{
  "scripts": {
    "dev": "bun --watch server/index.ts",
    "build": "vite build",
    "build:server": "bun build --compile server/index.ts --outfile=dashboard",
    "start": "NODE_ENV=production bun server/index.ts",
    "lint": "eslint .",
    "lint:fix": "eslint . --fix",
    "test": "vitest run",
    "test:watch": "vitest",
    "test:e2e": "playwright test",
    "typecheck": "vue-tsc --noEmit",
    "fix": "eslint . --fix && vue-tsc --noEmit && vitest run"
  }
}
```

Remove `"tsx": "^4.19.4"` from `devDependencies`.

- [ ] **Step 6: Install dependencies and verify**

```bash
pnpm install
bun --version
```

Expected: no errors, tsx is gone from node_modules.

- [ ] **Step 7: Commit**

```bash
git add package.json pnpm-lock.yaml .bun-version server/bun-env.d.ts
git commit -m "chore: migrate tooling from tsx to bun"
```

---

## Task 2: SQLite Migration — server/db/client.ts

**Files:**
- Modify: `server/db/client.ts`

- [ ] **Step 1: Run the existing DB tests to establish a green baseline**

```bash
bun vitest run server/db/db.test.ts
```

Expected: all tests pass (this is the before-state to protect).

- [ ] **Step 2: Rewrite server/db/client.ts**

Replace the entire file content:

```ts
import { Database } from 'bun:sqlite'
import { existsSync, mkdirSync, readFileSync, unlinkSync } from 'node:fs'
import { homedir } from 'node:os'
import { dirname, join } from 'node:path'
import process from 'node:process'
import { fileURLToPath } from 'node:url'

const DEFAULT_DB_PATH = join(homedir(), '.claude', 'dashboard-tasks.db')

let db: Database | null = null

export function getDbPath(): string {
  return process.env.DASHBOARD_DB_PATH || DEFAULT_DB_PATH
}

export function getDb(): Database {
  if (db)
    return db
  const path = getDbPath()
  const dir = dirname(path)
  if (!existsSync(dir))
    mkdirSync(dir, { recursive: true })
  db = new Database(path)
  db.run('PRAGMA journal_mode = WAL')
  db.run('PRAGMA foreign_keys = ON')
  runMigrations(db)
  return db
}

export function closeDb(): void {
  if (db) {
    db.close()
    db = null
  }
}

function migrate_v1_base_schema(db: Database): void {
  const schemaPath = join(dirname(fileURLToPath(import.meta.url)), 'schema.sql')
  db.exec(readFileSync(schemaPath, 'utf-8'))

  const taskCols = db.prepare('PRAGMA table_info(tasks)').all() as Array<{ name: string }>
  const hasTaskCol = (name: string) => taskCols.some(c => c.name === name)

  if (!hasTaskCol('silver_bullet'))
    db.run('ALTER TABLE tasks ADD COLUMN silver_bullet INTEGER NOT NULL DEFAULT 0')
  if (!hasTaskCol('priority'))
    db.run(`ALTER TABLE tasks ADD COLUMN priority TEXT NOT NULL DEFAULT 'medium'`)

  db.run('CREATE INDEX IF NOT EXISTS idx_tasks_picker ON tasks(silver_bullet DESC, priority, created_at)')
  db.run('DROP INDEX IF EXISTS idx_stage_runs_session')
  db.run('CREATE UNIQUE INDEX IF NOT EXISTS idx_stage_runs_session ON stage_runs(session_id) WHERE session_id IS NOT NULL')

  db.exec(`
    CREATE TABLE IF NOT EXISTS task_dependencies (
      id               TEXT PRIMARY KEY,
      task_id          TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
      depends_on_id    TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
      required_stage   TEXT NOT NULL DEFAULT 'done'
                       CHECK (required_stage IN ('done', 'cancelled')),
      on_cancel_action TEXT NOT NULL DEFAULT 'on_hold'
                       CHECK (on_cancel_action IN ('cancel', 'start', 'on_hold')),
      created_at       TEXT NOT NULL,
      UNIQUE(task_id, depends_on_id)
    )
  `)
  db.exec('CREATE INDEX IF NOT EXISTS idx_task_dependencies_task ON task_dependencies(task_id)')
  db.exec('CREATE INDEX IF NOT EXISTS idx_task_dependencies_depends_on ON task_dependencies(depends_on_id)')

  db.exec(`
    CREATE TABLE IF NOT EXISTS agent_cost_trend (
      t      INTEGER NOT NULL UNIQUE,
      cost   REAL    NOT NULL,
      tokens INTEGER NOT NULL
    )
  `)
  db.exec('CREATE INDEX IF NOT EXISTS idx_agent_cost_trend_t ON agent_cost_trend(t)')
}

function migrate_v2_multi_user(db: Database): void {
  const taskCols = db.prepare('PRAGMA table_info(tasks)').all() as Array<{ name: string }>
  const hasTaskCol = (name: string) => taskCols.some(c => c.name === name)
  const apiKeyCols = db.prepare('PRAGMA table_info(api_keys)').all() as Array<{ name: string }>

  if (!apiKeyCols.some(c => c.name === 'user_id'))
    db.run('ALTER TABLE api_keys ADD COLUMN user_id TEXT REFERENCES users(id) ON DELETE SET NULL')
  if (!hasTaskCol('user_id'))
    db.run('ALTER TABLE tasks ADD COLUMN user_id TEXT REFERENCES users(id) ON DELETE SET NULL')

  db.run('CREATE INDEX IF NOT EXISTS idx_tasks_user ON tasks(user_id)')
  db.run('CREATE INDEX IF NOT EXISTS idx_api_keys_user ON api_keys(user_id)')
}

function runMigrations(db: Database): void {
  migrate_v1_base_schema(db)

  const version = db.prepare('SELECT MAX(version) as v FROM schema_version')
    .get() as { v: number | null }

  if (version.v === null) {
    db.run('INSERT INTO schema_version (version, applied_at) VALUES (?, ?)',
      [1, new Date().toISOString()])
  }

  migrate_v2_multi_user(db)

  if ((version.v ?? 0) < 2) {
    db.run('INSERT INTO schema_version (version, applied_at) VALUES (?, ?)',
      [2, new Date().toISOString()])
  }
}

export function resetDb(): void {
  closeDb()
  const path = getDbPath()
  if (existsSync(path))
    unlinkSync(path)
}
```

- [ ] **Step 3: Run DB tests**

```bash
bun vitest run server/db/db.test.ts
```

Expected: all tests pass. If any fail, the error will point to a specific API difference — `bun:sqlite` statement `.run()` accepts rest params identically to `better-sqlite3`.

- [ ] **Step 4: Commit**

```bash
git add server/db/client.ts
git commit -m "feat: migrate server/db/client.ts from better-sqlite3 to bun:sqlite"
```

---

## Task 3: Repo Type Import Migration

**Files:**
- Modify: `server/db/apiKeysRepo.ts`, `server/db/auditRepo.ts`, `server/db/feedbackRepo.ts`, `server/db/notificationConfigRepo.ts`, `server/db/permissionsRepo.ts`, `server/db/stageRunsRepo.ts`, `server/db/taskDependenciesRepo.ts`, `server/db/tasksRepo.ts`

All 8 repo files have `import type { Database } from 'better-sqlite3'` as their first line. This is a mechanical single-line change in each file — the `Database` type is used as a default parameter type (`db: Database = getDb()`), which is structurally compatible with `bun:sqlite`'s `Database`.

- [ ] **Step 1: Migrate all type imports in one sed command**

```bash
find server/db -name "*.ts" ! -name "client.ts" ! -name "*.test.ts" \
  -exec sed -i '' "s|import type { Database } from 'better-sqlite3'|import type { Database } from 'bun:sqlite'|g" {} \;
```

- [ ] **Step 2: Verify the change**

```bash
grep -rn "better-sqlite3" server/
```

Expected: no output (zero remaining references).

- [ ] **Step 3: Run all DB-layer tests**

```bash
bun vitest run server/db/
```

Expected: all tests pass.

- [ ] **Step 4: Remove better-sqlite3 from dependencies**

In `package.json`, remove `"better-sqlite3": "^12.8.0"` from `dependencies`.
Also remove `"@types/better-sqlite3": "^7.6.13"` from `devDependencies`.

```bash
pnpm install
```

- [ ] **Step 5: Commit**

```bash
git add server/db/ package.json pnpm-lock.yaml
git commit -m "feat: migrate all DB repo files from better-sqlite3 to bun:sqlite"
```

---

## Task 4: Smoke Test — bun server/index.ts Starts

- [ ] **Step 1: Start the server under Bun**

```bash
bun server/index.ts
```

Expected: `Claude Agent Overview (development) running at http://localhost:13120`

Check for any import errors or startup crashes. If `bun:sqlite` types cause TypeScript complaints, verify `server/bun-env.d.ts` contains `/// <reference types="bun-types" />`.

- [ ] **Step 2: Run the full vitest suite**

```bash
bun vitest run
```

Expected: all existing tests pass. Note down the pass count as a regression baseline.

- [ ] **Step 3: Quick API smoke test**

```bash
curl -s http://localhost:13120/api/agents | head -c 200
```

Expected: JSON array (may be empty `[]` if no agents running — that is correct).

Stop the server (Ctrl+C).

---

## Task 5: Extract server/middleware.ts

`requireApiToken` (lines 156–174) and `rejectCrossOrigin` (lines 431–450) are inline functions inside `start()`. Extracting them to a dedicated module makes them importable by route files and independently testable.

**Files:**
- Create: `server/middleware.ts`
- Modify: `server/index.ts` (remove the two inline function bodies, import from middleware.ts)

- [ ] **Step 1: Create server/middleware.ts**

```ts
import { Buffer } from 'node:buffer'
import { timingSafeEqual } from 'node:crypto'
import type { NextFunction, Request, Response } from 'express'

export function requireApiToken(req: Request, res: Response, next: NextFunction): void {
  const apiToken = process.env.DASHBOARD_API_TOKEN
  if (!apiToken) {
    next()
    return
  }
  const auth = req.headers.authorization
  if (!auth?.startsWith('Bearer ')) {
    res.status(401).json({ error: 'Missing Authorization header' })
    return
  }
  const provided = Buffer.from(auth.slice(7))
  const expected = Buffer.from(apiToken)
  if (provided.length !== expected.length || !timingSafeEqual(provided, expected)) {
    res.status(403).json({ error: 'Invalid token' })
    return
  }
  next()
}

export function createRejectCrossOrigin(host: string, port: number) {
  return function rejectCrossOrigin(req: Request, res: Response): boolean {
    const origin = req.headers.origin ?? ''
    const referer = req.headers.referer ?? ''
    if (!origin && !referer)
      return false
    const allowed = (s: string): boolean => {
      try {
        const url = new URL(s)
        return (
          (url.hostname === host || url.hostname === 'localhost' || url.hostname === '127.0.0.1')
          && url.port === String(port)
        )
      }
      catch {
        return false
      }
    }
    if (allowed(origin) || allowed(referer))
      return false
    res.status(403).json({ error: 'Cross-origin request blocked' })
    return true
  }
}
```

- [ ] **Step 2: Update server/index.ts — replace inline functions with imports**

At the top of `server/index.ts`, add:
```ts
import { createRejectCrossOrigin, requireApiToken } from './middleware.js'
```

Remove the `timingSafeEqual` import from `node:crypto` (no longer used in index.ts) and the `Buffer` import from `node:buffer`.

Remove the `function requireApiToken(...)` body (lines 156–174).

Replace the inline `function rejectCrossOrigin(...)` body (lines 431–450) with:
```ts
const rejectCrossOrigin = createRejectCrossOrigin(HOST, PORT)
```

- [ ] **Step 3: Add UUID_RE to server/constants.ts**

Open `server/constants.ts` and add at the end:
```ts
export const UUID_RE = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i
```

Remove the `const UUID_RE = ...` declaration from `server/index.ts` (line 64) and add the import:
```ts
import { UUID_RE } from './constants.js'
```

- [ ] **Step 4: Verify server starts**

```bash
bun server/index.ts
```

Expected: starts cleanly, no errors.

- [ ] **Step 5: Run vitest suite**

```bash
bun vitest run
```

Expected: same pass count as Task 4 baseline.

- [ ] **Step 6: Commit**

```bash
git add server/middleware.ts server/constants.ts server/index.ts
git commit -m "refactor: extract requireApiToken and rejectCrossOrigin to server/middleware.ts"
```

---

## Task 6: Extract server/routes/authRoutes.ts

**Files:**
- Create: `server/routes/authRoutes.ts`
- Modify: `server/index.ts`

- [ ] **Step 1: Create server/routes/authRoutes.ts**

```ts
import { Router } from 'express'
import { exchangeCodeForToken, getGitHubUser, isOrgMember } from '../auth/githubOAuth.js'
import { signJwt } from '../auth/jwtUtils.js'
import { isAuthEnabled, requireAuth } from '../auth/requireAuth.js'
import { upsertUser } from '../db/usersRepo.js'

interface AuthRouterDeps {
  host: string
  port: number
}

export function createAuthRouter({ host, port }: AuthRouterDeps): Router {
  const router = Router()

  router.get('/auth/login', (_req, res) => {
    if (!isAuthEnabled()) {
      res.redirect('/')
      return
    }
    const params = new URLSearchParams({
      client_id: process.env.GITHUB_CLIENT_ID!,
      scope: 'read:org',
      redirect_uri: `http://${host}:${port}/auth/callback`,
    })
    res.redirect(`https://github.com/login/oauth/authorize?${params}`)
  })

  router.get('/auth/callback', async (req, res) => {
    const code = req.query.code as string | undefined
    if (!code) {
      res.status(400).send('Missing code')
      return
    }
    try {
      const accessToken = await exchangeCodeForToken(code)
      const ghUser = await getGitHubUser(accessToken)
      const member = await isOrgMember(ghUser.login, accessToken)
      if (!member) {
        res.status(403).send('You must be a member of the required GitHub org to access this dashboard.')
        return
      }
      const jwtSecret = process.env.JWT_SECRET
      if (!jwtSecret) {
        console.error('[auth] JWT_SECRET is not set — refusing to issue session token')
        res.status(500).send('Server misconfiguration: JWT_SECRET is not set')
        return
      }
      const user = upsertUser({
        id: ghUser.id,
        githubLogin: ghUser.login,
        displayName: ghUser.name,
        avatarUrl: ghUser.avatar_url,
      })
      const token = signJwt(
        { sub: user.id, login: user.githubLogin, isAdmin: user.isAdmin },
        jwtSecret,
        8 * 3600,
      )
      res.cookie('dashboard_session', token, { httpOnly: true, sameSite: 'lax', maxAge: 8 * 3600 * 1000 })
      res.redirect('/')
    }
    catch (err) {
      console.error('[auth] OAuth callback error:', err)
      res.status(500).send('Authentication failed')
    }
  })

  router.post('/auth/logout', (_req, res) => {
    res.clearCookie('dashboard_session')
    res.redirect('/auth/login')
  })

  router.get('/api/me', requireAuth, (req, res) => {
    if (!isAuthEnabled()) {
      res.json({ user: null, isAdmin: true, authEnabled: false })
      return
    }
    res.json({ user: req.user, isAdmin: req.user?.isAdmin ?? false, authEnabled: true })
  })

  return router
}
```

- [ ] **Step 2: Update server/index.ts — mount createAuthRouter, remove inline routes**

Add import at top:
```ts
import { createAuthRouter } from './routes/authRoutes.js'
```

Remove the 4 inline route handlers (`/auth/login`, `/auth/callback`, `/auth/logout`, `/api/me`) from `start()`.

Replace them with a single mount — place this **before** `app.use('/api', requireAuth)`:
```ts
app.use(createAuthRouter({ host: HOST, port: PORT }))
```

Also remove these imports that are now only used in authRoutes.ts (if no longer referenced in index.ts):
- `exchangeCodeForToken`, `getGitHubUser`, `isOrgMember` from `./auth/githubOAuth.js`
- `signJwt` from `./auth/jwtUtils.js`
- `upsertUser` from `./db/usersRepo.js`

- [ ] **Step 3: Verify server starts and auth routes respond**

```bash
bun server/index.ts &
curl -si http://localhost:13120/auth/login | head -5
# Expected: HTTP/1.1 302 Found  (redirect to GitHub OAuth or / if auth disabled)
kill %1
```

- [ ] **Step 4: Run vitest suite**

```bash
bun vitest run
```

Expected: same pass count.

- [ ] **Step 5: Commit**

```bash
git add server/routes/authRoutes.ts server/index.ts
git commit -m "refactor: extract auth routes to server/routes/authRoutes.ts"
```

---

## Task 7: Extract server/routes/agentRoutes.ts

Extracts the 8 non-SSE agent route handlers from `server/index.ts`. The SSE endpoint (`/api/agents/stream`) stays in `index.ts` because it owns the `sseClients` set and the broadcast interval.

**Files:**
- Create: `server/routes/agentRoutes.ts`
- Modify: `server/index.ts`

- [ ] **Step 1: Create server/routes/agentRoutes.ts**

```ts
import { readFile } from 'node:fs/promises'
import { join } from 'node:path'
import { timingSafeEqual } from 'node:crypto'
import { Buffer } from 'node:buffer'
import type { RequestHandler } from 'express'
import { Router } from 'express'
import { UUID_RE } from '../constants.js'
import { DISCOVERY_DIR } from '../paths.js'
import { getAgents } from '../agentMerger.js'
import { parseFullSession } from '../jsonlParser.js'
import { aggregateAgents, getEnvRemoteTargets, isRemoteFetch } from '../remoteAggregator.js'
import { getSessions } from '../sessionScanner.js'
import { getChannelMap } from '../channelDiscovery.js'
import type { SpawnManager } from '../spawnManager.js'

interface AgentRouterDeps {
  spawnManager: SpawnManager
  requireApiToken: RequestHandler
  rejectCrossOrigin: (req: import('express').Request, res: import('express').Response) => boolean
}

export function createAgentRouter({ spawnManager, requireApiToken, rejectCrossOrigin }: AgentRouterDeps): Router {
  const router = Router()

  router.get('/agents', requireApiToken, async (req, res) => {
    try {
      const localAgents = await getAgents()
      if (isRemoteFetch(req.headers)) {
        res.json(localAgents)
        return
      }
      const remotes = getEnvRemoteTargets()
      const agents = remotes.length > 0 ? await aggregateAgents(localAgents, remotes) : localAgents
      res.json(agents)
    }
    catch (err) {
      console.error('Error fetching agents:', err)
      res.status(500).json({ error: 'Failed to fetch agents' })
    }
  })

  router.get('/sessions', async (_req, res) => {
    try {
      const sessions = await getSessions()
      res.json(sessions)
    }
    catch (err) {
      console.error('Error fetching sessions:', err)
      res.status(500).json({ error: 'Failed to fetch sessions' })
    }
  })

  router.post('/agents/spawn', (req, res) => {
    if (rejectCrossOrigin(req, res))
      return
    if (!spawnManager.isSpawnAllowed()) {
      const windowSecs = Math.round(spawnManager.getRateLimitConfig().windowMs / 1000)
      const { max } = spawnManager.getRateLimitConfig()
      res.status(429).json({ error: `Too many spawn requests. Max ${max} per ${windowSecs} seconds.` })
      return
    }
    const result = spawnManager.spawnAgent(req.body)
    if (!result.ok) {
      res.status(result.status).json({ error: result.error })
      return
    }
    res.json({ ok: true, pid: result.pid })
  })

  router.get('/agents/spawn/:pid/status', (req, res) => {
    const pid = Number.parseInt(req.params.pid, 10)
    if (Number.isNaN(pid) || String(pid) !== req.params.pid) {
      res.status(400).json({ error: 'Invalid PID' })
      return
    }
    const status = spawnManager.getStatus(pid)
    if (!status) {
      res.status(404).json({ error: 'Unknown spawn PID' })
      return
    }
    res.json(status)
  })

  router.get('/agents/:sessionId/output', async (req, res) => {
    try {
      const { sessionId } = req.params
      if (!UUID_RE.test(sessionId)) {
        res.status(400).json({ error: 'Invalid sessionId format' })
        return
      }
      const lastOnly = req.query.last === '1'
      const messages = await parseFullSession(sessionId, lastOnly)
      res.json({ messages })
    }
    catch {
      res.status(500).json({ error: 'Failed to read session output' })
    }
  })

  router.post('/agents/:sessionId/message', async (req, res) => {
    if (rejectCrossOrigin(req, res))
      return
    try {
      const { sessionId } = req.params
      if (!UUID_RE.test(sessionId)) {
        res.status(400).json({ error: 'Invalid sessionId format' })
        return
      }
      const { message } = req.body
      if (!message || typeof message !== 'string') {
        res.status(400).json({ error: 'Missing "message" field' })
        return
      }
      const agents = await getAgents()
      const agent = agents.find(a => a.sessionId === sessionId)
      if (!agent) {
        res.status(404).json({ error: 'Agent not found' })
        return
      }
      if (!agent.channelAvailable) {
        res.status(404).json({ error: 'Channel not available' })
        return
      }
      const channelMap = await getChannelMap()
      const result = await spawnManager.sendMessageToChannel(agent, message, channelMap)
      switch (result.kind) {
        case 'not_found':
          res.status(404).json({ error: 'Channel not available' })
          return
        case 'timeout':
          res.status(504).json({ error: 'Channel request timed out' })
          return
        case 'unreachable':
          res.status(502).json({ error: `Channel unreachable: ${result.message}` })
          return
        case 'response':
          res.status(result.status).json(result.body)
      }
    }
    catch (err) {
      console.error('Error sending message:', err)
      res.status(500).json({ error: 'Internal error' })
    }
  })

  router.post('/channel-reply', async (req, res) => {
    try {
      const { parentPid, message, timestamp } = req.body
      if (!parentPid || !message || !timestamp) {
        res.status(400).json({ error: 'Missing required fields' })
        return
      }
      const authHeader = req.headers.authorization
      if (!authHeader?.startsWith('Bearer ')) {
        res.status(401).json({ error: 'Missing authorization' })
        return
      }
      const token = authHeader.slice(7)
      try {
        const discoveryPath = join(DISCOVERY_DIR, `${parentPid}.json`)
        const raw = await readFile(discoveryPath, 'utf-8')
        const discovery = JSON.parse(raw)
        const expected = Buffer.from(String(discovery.token))
        const provided = Buffer.from(token)
        if (expected.length !== provided.length || !timingSafeEqual(expected, provided)) {
          res.status(403).json({ error: 'Invalid token' })
          return
        }
      }
      catch {
        res.status(403).json({ error: 'Invalid token' })
        return
      }
      spawnManager.storeReply(parentPid, message, timestamp)
      res.json({ ok: true })
    }
    catch (err) {
      console.error('Error handling channel reply:', err)
      res.status(500).json({ error: 'Internal error' })
    }
  })

  router.get('/agents/:sessionId/replies', async (req, res) => {
    try {
      const { sessionId } = req.params
      if (!UUID_RE.test(sessionId)) {
        res.status(400).json({ error: 'Invalid sessionId format' })
        return
      }
      const since = req.query.since as string | undefined
      const agents = await getAgents()
      const agent = agents.find(a => a.sessionId === sessionId)
      if (!agent) {
        res.status(404).json({ error: 'Agent not found' })
        return
      }
      const replies = spawnManager.getReplies(agent.pid, since)
      res.json({ replies })
    }
    catch (err) {
      console.error('Error fetching replies:', err)
      res.status(500).json({ error: 'Internal error' })
    }
  })

  return router
}
```

- [ ] **Step 2: Update server/index.ts — mount createAgentRouter, remove 8 inline route handlers**

Add import at top:
```ts
import { createAgentRouter } from './routes/agentRoutes.js'
```

Remove the following inline route handlers from `start()`:
- `app.get('/api/agents', ...)` — the REST one (NOT the SSE stream)
- `app.get('/api/sessions', ...)`
- `app.post('/api/agents/spawn', ...)`
- `app.get('/api/agents/spawn/:pid/status', ...)`
- `app.get('/api/agents/:sessionId/output', ...)`
- `app.post('/api/agents/:sessionId/message', ...)`
- `app.post('/api/channel-reply', ...)`
- `app.get('/api/agents/:sessionId/replies', ...)`

Add the router mount after `app.use('/api', requireAuth)`:
```ts
app.use('/api', createAgentRouter({ spawnManager, requireApiToken, rejectCrossOrigin }))
```

Remove now-unused imports from index.ts (only if not referenced elsewhere):
- `parseFullSession` from `./jsonlParser.js`
- `getSessions` from `./sessionScanner.js`
- `getChannelMap` from `./channelDiscovery.js`
- `timingSafeEqual` from `node:crypto` (if only used in channel-reply)
- `readFile` from `node:fs/promises` (if only used in channel-reply)

Keep: `getAgents`, `aggregateAgents`, `getEnvRemoteTargets`, `isRemoteFetch` — still used in the SSE broadcast loop.

- [ ] **Step 3: Verify server starts and agent endpoint responds**

```bash
bun server/index.ts &
curl -s http://localhost:13120/api/agents
# Expected: JSON array
kill %1
```

- [ ] **Step 4: Run vitest suite**

```bash
bun vitest run
```

Expected: same pass count.

- [ ] **Step 5: Commit**

```bash
git add server/routes/agentRoutes.ts server/index.ts
git commit -m "refactor: extract agent routes to server/routes/agentRoutes.ts"
```

---

## Task 8: Extract server/routes/systemRoutes.ts

**Files:**
- Create: `server/routes/systemRoutes.ts`
- Modify: `server/index.ts`

- [ ] **Step 1: Create server/routes/systemRoutes.ts**

```ts
import { homedir } from 'node:os'
import { join } from 'node:path'
import { Router } from 'express'
import { getSystemInfo } from '../systemMonitor.js'

interface SystemRouterDeps {
  serverDir: string
}

export function createSystemRouter({ serverDir }: SystemRouterDeps): Router {
  const router = Router()

  router.get('/config', (_req, res) => {
    const home = homedir()
    const scriptAbsolute = join(serverDir, '..', 'scripts', 'claude-with-channel.sh')
    const scriptPath = scriptAbsolute.startsWith(home)
      ? `~${scriptAbsolute.slice(home.length)}`
      : scriptAbsolute
    res.json({ scriptPath, homedir: home })
  })

  router.get('/system', async (_req, res) => {
    try {
      const info = await getSystemInfo()
      res.json(info)
    }
    catch (err) {
      console.error('Error fetching system info:', err)
      res.status(500).json({ error: 'Failed to fetch system info' })
    }
  })

  return router
}
```

- [ ] **Step 2: Update server/index.ts — mount createSystemRouter, remove 2 inline handlers**

Add import:
```ts
import { createSystemRouter } from './routes/systemRoutes.js'
```

Remove `app.get('/api/config', ...)` and `app.get('/api/system', ...)` inline handlers.

Add mount after the other router registrations:
```ts
app.use('/api', createSystemRouter({ serverDir: import.meta.dirname }))
```

Remove `getSystemInfo` import from index.ts if no longer referenced.

- [ ] **Step 3: Verify**

```bash
bun server/index.ts &
curl -s http://localhost:13120/api/system | python3 -m json.tool | head -10
# Expected: JSON with cpu, memory, disk fields
kill %1
```

- [ ] **Step 4: Run vitest suite**

```bash
bun vitest run
```

Expected: same pass count.

- [ ] **Step 5: Commit**

```bash
git add server/routes/systemRoutes.ts server/index.ts
git commit -m "refactor: extract system routes to server/routes/systemRoutes.ts"
```

---

## Task 9: Final index.ts Cleanup + Full Test Suite

- [ ] **Step 1: Verify index.ts is now focused**

```bash
wc -l server/index.ts
```

Expected: under 300 lines. If still significantly above, check for leftover route handlers from Tasks 6–8.

- [ ] **Step 2: Clean up any remaining unused imports in index.ts**

```bash
bun run typecheck 2>&1 | grep "index.ts" | grep "is declared but"
```

Remove any flagged unused imports.

- [ ] **Step 3: Run the full test suite**

```bash
bun vitest run
```

Expected: all tests pass.

- [ ] **Step 4: Run ESLint**

```bash
bun run lint
```

Fix any errors (typically unused import warnings surfaced by extraction).

- [ ] **Step 5: Run typecheck**

```bash
bun run typecheck
```

Expected: zero errors.

- [ ] **Step 6: Run Playwright E2E tests**

```bash
bun run test:e2e
```

Expected: all E2E tests pass (this validates full request/response flow through the new router structure).

- [ ] **Step 7: Commit**

```bash
git add server/index.ts
git commit -m "refactor: slim server/index.ts to composition root after route extraction"
```

---

## Task 10: Single Binary Build

**Files:**
- No new files — verifies the build pipeline added in Task 1

- [ ] **Step 1: Build the Vue frontend**

```bash
pnpm build
```

Expected: `dist/` directory created with compiled Vue SPA.

- [ ] **Step 2: Compile server to single binary**

```bash
bun build --compile server/index.ts --outfile=dashboard
```

Expected: `dashboard` binary created in the project root (~30 MB).

Note: If `schema.sql` read fails at runtime (because `import.meta.dirname` resolves differently in a compiled binary), copy `server/db/schema.sql` alongside the binary or set `DASHBOARD_DB_PATH` to a writable path. Test in Step 4.

- [ ] **Step 3: Verify binary is self-contained**

```bash
file ./dashboard
ls -lh ./dashboard
```

Expected: `Mach-O 64-bit executable` (macOS) or `ELF 64-bit LSB executable` (Linux). Size ~25–35 MB.

- [ ] **Step 4: Start binary in production mode and smoke-test**

```bash
NODE_ENV=production ./dashboard &
sleep 2
curl -s http://localhost:13120/api/agents
curl -s http://localhost:13120/api/system | python3 -m json.tool | head -5
kill %1
```

Expected: both endpoints return valid JSON. If `/api/config` fails with schema.sql not found, see Step 2 note.

- [ ] **Step 5: Add .gitignore entry for the binary**

Add to `.gitignore`:
```
/dashboard
```

- [ ] **Step 6: Commit**

```bash
git add .gitignore
git commit -m "chore: add dashboard binary to gitignore after confirming bun build --compile works"
```

---

## Self-Review Notes

**Spec coverage:**
- ✓ Runtime swap (Node.js → Bun): Tasks 1, 4
- ✓ SQLite migration (better-sqlite3 → bun:sqlite): Tasks 2–3
- ✓ tsx removal: Task 1
- ✓ R1 server/index.ts split: Tasks 5–9
- ✓ R2 DB migrations cleanup (named functions): Task 2
- ✓ R3 tsx removal: Task 1
- ✓ Single binary build: Task 10
- ✓ Dep compatibility: Task 4 smoke test covers Express 5 + MCP SDK + nodemailer via full server start

**Type consistency:** `Database` type from `bun:sqlite` used consistently across all tasks. `UUID_RE` defined in `constants.ts` (Task 5), imported in `agentRoutes.ts` (Task 7). `rejectCrossOrigin` returned by `createRejectCrossOrigin` (Task 5), consumed in `agentRoutes.ts` deps (Task 7).

**Edge case:** `schema.sql` is read via `readFileSync` at DB init. In a `bun build --compile` binary, the file is NOT bundled — co-deploy `server/db/schema.sql` alongside `dashboard` binary, or run with a pre-existing DB file. Document this in deployment notes.
