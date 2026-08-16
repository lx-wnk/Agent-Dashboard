# Multi-User Server Deployment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add GitHub OAuth (org-gated), user-scoped pipeline tasks, and per-user local session registration to the dashboard, with full standalone backwards compatibility when auth env vars are absent.

**Architecture:** A new `server/auth/` layer provides JWT middleware and GitHub OAuth routes. The DB gains `users` + `remote_registrations` tables and `user_id` columns on `tasks`/`api_keys`. Task queries are filtered by the authenticated user's ID; remote registrations are fetched per-user at SSE-broadcast time. When `GITHUB_CLIENT_ID` is unset, every auth guard is a no-op so standalone mode is unaffected.

**Tech Stack:** Express + Node.js crypto (HMAC-SHA256 JWT, no external jwt lib), better-sqlite3, Vue 3 + TypeScript, Vitest for unit tests.

---

## File Map

### Phase 1 — Bug Fixes (independent, no auth dependency)

| Action | File |
|---|---|
| Modify | `server/processScanner.ts` — batch macOS lsof into one call |
| Modify | `server/processScanner.test.ts` — add `parseLsofBatch` unit tests |
| Modify | `server/sessionScanner.ts` — fix `decodeProjectDir` fallback |
| Modify | `server/index.ts` — persist cost-trend ring buffer to `pipeline_config` |

### Phase 2 — DB Schema + Auth Foundation

| Action | File |
|---|---|
| Modify | `server/db/schema.sql` — add `users`, `remote_registrations` tables |
| Modify | `server/db/client.ts` — runtime migration: `user_id` ALTER TABLEs + new tables |
| Create | `server/db/usersRepo.ts` — upsert / find by GitHub ID |
| Create | `server/db/remoteRegistrationsRepo.ts` — CRUD scoped to `user_id` |
| Create | `server/db/usersRepo.test.ts` |
| Create | `server/db/remoteRegistrationsRepo.test.ts` |
| Create | `server/auth/jwtUtils.ts` — sign / verify HMAC-SHA256 JWT |
| Create | `server/auth/githubOAuth.ts` — exchange code, check org membership |
| Create | `server/auth/requireAuth.ts` — Express middleware, standalone bypass |
| Create | `server/auth/jwtUtils.test.ts` |
| Modify | `server/index.ts` — mount `/auth/*` routes, apply `requireAuth` to `/api/*` |

### Phase 3 — User-Scoped Tasks

| Action | File |
|---|---|
| Modify | `server/db/tasksRepo.ts` — add `userId` to `CreateTaskInput`, `listTasksForUser(userId, isAdmin)` |
| Modify | `server/routes/taskRoutes.ts` — read `req.user` from JWT, scope `listTasks` and `createTask` |

### Phase 4 — Remote Registration

| Action | File |
|---|---|
| Create | `server/routes/remoteRoutes.ts` — CRUD for `/api/remotes` |
| Modify | `server/remoteAggregator.ts` — accept `RemoteRegistration[]` instead of `string[]`, add bearer header |
| Modify | `server/index.ts` — `requireApiToken` middleware on `/api/agents`, load per-user remotes in SSE handler, mount remoteRouter |

### Phase 5 — Frontend

| Action | File |
|---|---|
| Create | `src/composables/useUser.ts` — fetch `/api/me`, expose `user`, `isAdmin`, `isAuthenticated` |
| Create | `src/components/LoginPage.vue` — GitHub login redirect page |
| Create | `src/components/RemoteSettings.vue` — register/test/delete local dashboard URLs |
| Modify | `src/App.vue` — show `LoginPage` when unauthenticated |
| Modify | `src/components/ApiKeySettings.vue` (or existing Settings panel) — add Remotes tab |

---

## Phase 1 — Bug Fixes

### Task 1: Batch macOS lsof calls in processScanner

**Files:**
- Modify: `server/processScanner.ts`
- Modify: `server/processScanner.test.ts`

- [ ] **Step 1: Write failing unit tests for `parseLsofBatch`**

Add to `server/processScanner.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { parseLsofBatch, parseElapsedTime, scanProcesses } from './processScanner'

describe('parseLsofBatch', () => {
  it('parses a single process entry', () => {
    const stdout = 'p123\nn/Users/alex/my-project\n'
    expect(parseLsofBatch(stdout)).toEqual(new Map([[123, '/Users/alex/my-project']]))
  })

  it('parses multiple process entries', () => {
    const stdout = 'p100\nn/home/a\np200\nn/home/b\n'
    const result = parseLsofBatch(stdout)
    expect(result.get(100)).toBe('/home/a')
    expect(result.get(200)).toBe('/home/b')
    expect(result.size).toBe(2)
  })

  it('returns empty map for empty input', () => {
    expect(parseLsofBatch('')).toEqual(new Map())
  })

  it('ignores lines that are not p- or n-prefixed', () => {
    const stdout = 'p99\nf3\nn/some/path\n'
    expect(parseLsofBatch(stdout)).toEqual(new Map([[99, '/some/path']]))
  })

  it('skips a pid entry that has no following n-line', () => {
    const stdout = 'p99\np100\nn/some/path\n'
    const result = parseLsofBatch(stdout)
    // p99 has no n-line — only p100 gets a path
    expect(result.get(99)).toBeUndefined()
    expect(result.get(100)).toBe('/some/path')
  })
})
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
pnpm test server/processScanner.test.ts
```

Expected: FAIL — `parseLsofBatch is not a function`

- [ ] **Step 3: Implement `parseLsofBatch` and batch `getCwdsMac` in processScanner.ts**

Replace the entire file `server/processScanner.ts` with:

```ts
import { execFile } from 'node:child_process'
import { readlink } from 'node:fs/promises'
import { promisify } from 'node:util'
import { WHITESPACE_RE } from './paths.js'
import { IS_LINUX } from './platform.js'

const execFileAsync = promisify(execFile)

export interface ProcessInfo {
  pid: number
  cwd: string
  uptime: number
  command: string
}

export function parseElapsedTime(etime: string): number {
  const parts = etime.trim().replace(/-/g, ':').split(':').reverse()
  const seconds = Number.parseInt(parts[0] || '0', 10)
  const minutes = Number.parseInt(parts[1] || '0', 10)
  const hours = Number.parseInt(parts[2] || '0', 10)
  const days = Number.parseInt(parts[3] || '0', 10)
  return days * 86400 + hours * 3600 + minutes * 60 + seconds
}

/**
 * Parse `lsof -Fn` output into a pid→cwd map.
 * Each process block starts with a `p{pid}` line followed by `n{path}`.
 */
export function parseLsofBatch(stdout: string): Map<number, string> {
  const result = new Map<number, string>()
  let currentPid: number | null = null
  for (const line of stdout.split('\n')) {
    if (line.startsWith('p')) {
      currentPid = Number.parseInt(line.slice(1), 10)
    }
    else if (line.startsWith('n') && currentPid !== null) {
      result.set(currentPid, line.slice(1))
      currentPid = null
    }
  }
  return result
}

async function getCwdsLinux(pids: number[]): Promise<Map<number, string>> {
  const result = new Map<number, string>()
  await Promise.all(
    pids.map(async (pid) => {
      try {
        const cwd = await readlink(`/proc/${pid}/cwd`)
        result.set(pid, cwd)
      }
      catch {
        // process may have exited
      }
    }),
  )
  return result
}

async function getCwdsMac(pids: number[]): Promise<Map<number, string>> {
  if (pids.length === 0)
    return new Map()
  try {
    const { stdout } = await execFileAsync('lsof', [
      '-a', '-d', 'cwd', '-p', pids.join(','), '-Fn',
    ])
    return parseLsofBatch(stdout)
  }
  catch {
    return new Map()
  }
}

const getCwds = IS_LINUX ? getCwdsLinux : getCwdsMac

export async function scanProcesses(): Promise<ProcessInfo[]> {
  const { stdout } = await execFileAsync('ps', ['-eo', 'pid,etime,comm'])
  const lines = stdout.trim().split('\n').slice(1)

  const parsed = lines
    .filter((line) => {
      const comm = line.trim().split(WHITESPACE_RE).slice(2).join(' ')
      return comm.endsWith('/claude') || comm === 'claude'
    })
    .map((line) => {
      const parts = line.trim().split(WHITESPACE_RE)
      return {
        pid: Number.parseInt(parts[0], 10),
        etime: parts[1],
        command: parts.slice(2).join(' '),
      }
    })

  const cwdMap = await getCwds(parsed.map(p => p.pid))

  return parsed
    .map(p => ({ ...p, cwd: cwdMap.get(p.pid) ?? null }))
    .filter(p => p.cwd && p.cwd !== '/')
    .map(p => ({
      pid: p.pid,
      cwd: p.cwd!,
      uptime: parseElapsedTime(p.etime),
      command: p.command,
    }))
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
pnpm test server/processScanner.test.ts
```

Expected: all tests PASS (parseLsofBatch unit tests + existing scanProcesses integration tests)

- [ ] **Step 5: Type-check**

```bash
pnpm typecheck
```

Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add server/processScanner.ts server/processScanner.test.ts
git commit -m "perf: batch macOS lsof into single call for CWD lookup"
```

---

### Task 2: Fix decodeProjectDir fallback

**Files:**
- Modify: `server/sessionScanner.ts`

The bug: when `headInfo.cwd` is null, `decodeProjectDir` replaces all `-` with `/`, producing a wrong-looking path (e.g., `my-project` → `my/project`). The fix: return the raw encoded string as the fallback `projectPath`. It's opaque but not misleading.

- [ ] **Step 1: Update `decodeProjectDir` and its call site in `sessionScanner.ts`**

In `server/sessionScanner.ts`, replace the `decodeProjectDir` function and its usage:

```ts
// Replace the existing decodeProjectDir function (lines 33-40) with:
/**
 * Last-resort fallback display path when the JSONL cwd entry is missing.
 * Returns the raw encoded string rather than a lossy guess — the encoded
 * form is at least unambiguous even if it looks odd.
 */
export function decodeProjectDir(encoded: string): string {
  return encoded
}
```

Then on line 173, verify it reads:
```ts
const projectPath = headInfo.cwd || decodeProjectDir(entry.projectDirEncoded)
```
No change needed there — the call site is already correct, `decodeProjectDir` is only the fallback.

- [ ] **Step 2: Run existing tests**

```bash
pnpm test
```

Expected: all tests PASS (no test relies on the broken decode behaviour)

- [ ] **Step 3: Commit**

```bash
git add server/sessionScanner.ts
git commit -m "fix: decodeProjectDir returns raw encoded path instead of lossy decode"
```

---

### Task 3: Persist cost-trend ring buffer across restarts

**Files:**
- Modify: `server/index.ts`

The cost trend ring buffer (1h × 3s = 1200 points) is lost on server restart. Persist it to `pipeline_config` every 60s and reload on startup.

- [ ] **Step 1: Add persistence helpers inside `start()` in `server/index.ts`**

Add these imports at the top of the file (after existing imports):
```ts
import { getDb } from './db/client.js'
```

Then, inside the `start()` function, replace the cost trend ring buffer block (currently around lines 119–122) with:

```ts
// Cost trend history: ring buffer, 1h at 3s interval = 1200 entries
const MAX_TREND_POINTS = 1200
const TREND_CONFIG_KEY = 'cost_trend_history'
const TREND_TTL_MS = 24 * 60 * 60 * 1000 // 24h

// Load persisted trend on startup
const costTrend: Array<{ t: number, cost: number, tokens: number }> = (() => {
  try {
    const db = getDb()
    const row = db.prepare('SELECT value FROM pipeline_config WHERE key = ?').get(TREND_CONFIG_KEY) as { value: string } | undefined
    if (!row)
      return []
    const parsed: Array<{ t: number, cost: number, tokens: number }> = JSON.parse(row.value)
    const cutoff = Date.now() - TREND_TTL_MS
    return parsed.filter(p => p.t > cutoff)
  }
  catch {
    return []
  }
})()

// Persist trend every 60s
setInterval(() => {
  try {
    const db = getDb()
    db.prepare(
      'INSERT INTO pipeline_config (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value',
    ).run(TREND_CONFIG_KEY, JSON.stringify(costTrend))
  }
  catch {
    // best-effort — never crash the server for trend persistence
  }
}, 60_000)
```

- [ ] **Step 2: Run tests and type-check**

```bash
pnpm test && pnpm typecheck
```

Expected: all tests PASS, no type errors

- [ ] **Step 3: Commit**

```bash
git add server/index.ts
git commit -m "feat: persist cost-trend ring buffer to pipeline_config (24h TTL)"
```

---

## Phase 2 — DB Schema + Auth Foundation

### Task 4: Add users and remote_registrations tables + user_id migrations

**Files:**
- Modify: `server/db/schema.sql`
- Modify: `server/db/client.ts`

- [ ] **Step 1: Add new tables to schema.sql**

Append to the end of `server/db/schema.sql` (before the final newline):

```sql
-- Dashboard users (populated on first GitHub OAuth login)
CREATE TABLE IF NOT EXISTS users (
  id            TEXT PRIMARY KEY,   -- GitHub numeric user ID (stable across username renames)
  github_login  TEXT NOT NULL,      -- for display only; can change
  display_name  TEXT,
  avatar_url    TEXT,
  is_admin      INTEGER NOT NULL DEFAULT 0,
  created_at    TEXT NOT NULL,
  last_login_at TEXT
);

-- Per-user registered local dashboard instances (for remote session aggregation)
CREATE TABLE IF NOT EXISTS remote_registrations (
  id          TEXT PRIMARY KEY,
  user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  url         TEXT NOT NULL,
  name        TEXT,
  bearer_key  TEXT,
  created_at  TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_remote_reg_user_url
  ON remote_registrations(user_id, url);
```

- [ ] **Step 2: Add runtime ALTER TABLE migrations in client.ts**

In `server/db/client.ts`, at the end of `runMigrations()` before the closing `}`, add:

```ts
  // Runtime migration: add user_id to tasks and api_keys for user-scoped multi-user support.
  const apiKeyCols = connection.prepare('PRAGMA table_info(api_keys)').all() as Array<{ name: string }>
  const hasApiKeyUserId = apiKeyCols.some(c => c.name === 'user_id')
  if (!hasApiKeyUserId)
    connection.prepare('ALTER TABLE api_keys ADD COLUMN user_id TEXT REFERENCES users(id) ON DELETE SET NULL').run()

  if (!hasCol('user_id'))
    connection.prepare('ALTER TABLE tasks ADD COLUMN user_id TEXT REFERENCES users(id) ON DELETE SET NULL').run()

  // Ensure users and remote_registrations tables exist (schema.sql CREATE IF NOT EXISTS is idempotent).
  // The schema.sql exec above already handles this — no additional ALTER needed.
```

Note: `hasCol` already exists as a helper in `runMigrations` (it checks `taskCols`). For `api_keys` we use `hasApiKeyUserId` separately since `hasCol` only checks the `tasks` table.

- [ ] **Step 3: Verify migration runs without error**

```bash
pnpm test server/db/db.test.ts
```

Expected: all PASS (existing DB tests should still pass with new columns)

- [ ] **Step 4: Commit**

```bash
git add server/db/schema.sql server/db/client.ts
git commit -m "feat(db): add users and remote_registrations tables, user_id columns on tasks/api_keys"
```

---

### Task 5: usersRepo.ts — upsert and lookup

**Files:**
- Create: `server/db/usersRepo.ts`
- Create: `server/db/usersRepo.test.ts`

- [ ] **Step 1: Write failing tests**

Create `server/db/usersRepo.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { getDb, resetDb } from './client'
import { findUserById, upsertUser } from './usersRepo'

describe('usersRepo', () => {
  beforeEach(() => {
    resetDb()
    getDb() // initialise schema
  })
  afterEach(() => resetDb())

  it('upserts a new user and returns it', () => {
    const user = upsertUser({ id: '12345', githubLogin: 'alex', displayName: 'Alex W', avatarUrl: 'https://gh.io/av' })
    expect(user.id).toBe('12345')
    expect(user.githubLogin).toBe('alex')
    expect(user.isAdmin).toBe(false)
    expect(user.createdAt).toBeTruthy()
  })

  it('updates githubLogin and lastLoginAt on subsequent upsert', () => {
    upsertUser({ id: '12345', githubLogin: 'alex', displayName: null, avatarUrl: null })
    const updated = upsertUser({ id: '12345', githubLogin: 'alex-new', displayName: null, avatarUrl: null })
    expect(updated.githubLogin).toBe('alex-new')
    expect(updated.lastLoginAt).toBeTruthy()
  })

  it('findUserById returns null for unknown id', () => {
    expect(findUserById('unknown')).toBeNull()
  })

  it('findUserById returns the user after upsert', () => {
    upsertUser({ id: '99', githubLogin: 'bob', displayName: null, avatarUrl: null })
    const found = findUserById('99')
    expect(found?.githubLogin).toBe('bob')
  })
})
```

- [ ] **Step 2: Run to confirm failure**

```bash
pnpm test server/db/usersRepo.test.ts
```

Expected: FAIL — module not found

- [ ] **Step 3: Implement usersRepo.ts**

Create `server/db/usersRepo.ts`:

```ts
import { randomUUID } from 'node:crypto'
import { getDb } from './client.js'

export interface User {
  id: string
  githubLogin: string
  displayName: string | null
  avatarUrl: string | null
  isAdmin: boolean
  createdAt: string
  lastLoginAt: string | null
}

export interface UpsertUserInput {
  id: string
  githubLogin: string
  displayName: string | null
  avatarUrl: string | null
}

function rowToUser(row: Record<string, unknown>): User {
  return {
    id: row.id as string,
    githubLogin: row.github_login as string,
    displayName: (row.display_name as string | null) ?? null,
    avatarUrl: (row.avatar_url as string | null) ?? null,
    isAdmin: (row.is_admin as number) === 1,
    createdAt: row.created_at as string,
    lastLoginAt: (row.last_login_at as string | null) ?? null,
  }
}

export function upsertUser(input: UpsertUserInput): User {
  const db = getDb()
  const now = new Date().toISOString()
  db.prepare(`
    INSERT INTO users (id, github_login, display_name, avatar_url, created_at, last_login_at)
    VALUES (?, ?, ?, ?, ?, ?)
    ON CONFLICT(id) DO UPDATE SET
      github_login  = excluded.github_login,
      display_name  = excluded.display_name,
      avatar_url    = excluded.avatar_url,
      last_login_at = excluded.last_login_at
  `).run(input.id, input.githubLogin, input.displayName, input.avatarUrl, now, now)

  return rowToUser(
    db.prepare('SELECT * FROM users WHERE id = ?').get(input.id) as Record<string, unknown>,
  )
}

export function findUserById(id: string): User | null {
  const db = getDb()
  const row = db.prepare('SELECT * FROM users WHERE id = ?').get(id) as Record<string, unknown> | undefined
  return row ? rowToUser(row) : null
}

export function setUserAdmin(id: string, isAdmin: boolean): void {
  getDb().prepare('UPDATE users SET is_admin = ? WHERE id = ?').run(isAdmin ? 1 : 0, id)
}
```

- [ ] **Step 4: Run tests**

```bash
pnpm test server/db/usersRepo.test.ts
```

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add server/db/usersRepo.ts server/db/usersRepo.test.ts
git commit -m "feat(db): add usersRepo with upsert and findUserById"
```

---

### Task 6: remoteRegistrationsRepo.ts — user-scoped CRUD

**Files:**
- Create: `server/db/remoteRegistrationsRepo.ts`
- Create: `server/db/remoteRegistrationsRepo.test.ts`

- [ ] **Step 1: Write failing tests**

Create `server/db/remoteRegistrationsRepo.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { getDb, resetDb } from './client'
import { upsertUser } from './usersRepo'
import {
  createRemoteRegistration,
  deleteRemoteRegistration,
  listRemoteRegistrationsForUser,
} from './remoteRegistrationsRepo'

function seedUser(id = 'u1') {
  upsertUser({ id, githubLogin: 'alex', displayName: null, avatarUrl: null })
}

describe('remoteRegistrationsRepo', () => {
  beforeEach(() => { resetDb(); getDb() })
  afterEach(() => resetDb())

  it('creates a registration and lists it for the owner', () => {
    seedUser()
    const reg = createRemoteRegistration({ userId: 'u1', url: 'http://192.168.1.5:13120', name: 'MacBook', bearerKey: 'tok' })
    const list = listRemoteRegistrationsForUser('u1')
    expect(list).toHaveLength(1)
    expect(list[0].id).toBe(reg.id)
    expect(list[0].url).toBe('http://192.168.1.5:13120')
  })

  it('does not return registrations for another user', () => {
    seedUser('u1')
    seedUser('u2')
    createRemoteRegistration({ userId: 'u1', url: 'http://a:13120', name: 'A', bearerKey: null })
    expect(listRemoteRegistrationsForUser('u2')).toHaveLength(0)
  })

  it('deletes a registration only when userId matches', () => {
    seedUser('u1')
    seedUser('u2')
    const reg = createRemoteRegistration({ userId: 'u1', url: 'http://a:13120', name: 'A', bearerKey: null })
    expect(deleteRemoteRegistration(reg.id, 'u2')).toBe(false)
    expect(deleteRemoteRegistration(reg.id, 'u1')).toBe(true)
    expect(listRemoteRegistrationsForUser('u1')).toHaveLength(0)
  })
})
```

- [ ] **Step 2: Run to confirm failure**

```bash
pnpm test server/db/remoteRegistrationsRepo.test.ts
```

Expected: FAIL — module not found

- [ ] **Step 3: Implement remoteRegistrationsRepo.ts**

Create `server/db/remoteRegistrationsRepo.ts`:

```ts
import { randomUUID } from 'node:crypto'
import { getDb } from './client.js'

export interface RemoteRegistration {
  id: string
  userId: string
  url: string
  name: string | null
  bearerKey: string | null
  createdAt: string
}

export interface CreateRemoteInput {
  userId: string
  url: string
  name: string | null
  bearerKey: string | null
}

function rowToReg(row: Record<string, unknown>): RemoteRegistration {
  return {
    id: row.id as string,
    userId: row.user_id as string,
    url: row.url as string,
    name: (row.name as string | null) ?? null,
    bearerKey: (row.bearer_key as string | null) ?? null,
    createdAt: row.created_at as string,
  }
}

export function createRemoteRegistration(input: CreateRemoteInput): RemoteRegistration {
  const db = getDb()
  const id = randomUUID()
  const now = new Date().toISOString()
  db.prepare(`
    INSERT INTO remote_registrations (id, user_id, url, name, bearer_key, created_at)
    VALUES (?, ?, ?, ?, ?, ?)
  `).run(id, input.userId, input.url, input.name, input.bearerKey, now)
  return rowToReg(db.prepare('SELECT * FROM remote_registrations WHERE id = ?').get(id) as Record<string, unknown>)
}

/** Always filters by userId — no admin override. */
export function listRemoteRegistrationsForUser(userId: string): RemoteRegistration[] {
  const rows = getDb()
    .prepare('SELECT * FROM remote_registrations WHERE user_id = ? ORDER BY created_at ASC')
    .all(userId) as Record<string, unknown>[]
  return rows.map(rowToReg)
}

/** Returns true only when the registration belonged to userId. */
export function deleteRemoteRegistration(id: string, userId: string): boolean {
  const result = getDb()
    .prepare('DELETE FROM remote_registrations WHERE id = ? AND user_id = ?')
    .run(id, userId)
  return result.changes > 0
}
```

- [ ] **Step 4: Run tests**

```bash
pnpm test server/db/remoteRegistrationsRepo.test.ts
```

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add server/db/remoteRegistrationsRepo.ts server/db/remoteRegistrationsRepo.test.ts
git commit -m "feat(db): add remoteRegistrationsRepo with strict user_id scoping"
```

---

### Task 7: JWT utilities + GitHub OAuth helpers

**Files:**
- Create: `server/auth/jwtUtils.ts`
- Create: `server/auth/githubOAuth.ts`
- Create: `server/auth/requireAuth.ts`
- Create: `server/auth/jwtUtils.test.ts`

- [ ] **Step 1: Write failing JWT tests**

Create `server/auth/jwtUtils.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { signJwt, verifyJwt } from './jwtUtils'

const SECRET = 'test-secret-at-least-32-bytes-long!!'

describe('jwtUtils', () => {
  it('signs and verifies a payload round-trip', () => {
    const token = signJwt({ sub: '123', login: 'alex', isAdmin: false }, SECRET, 3600)
    const payload = verifyJwt(token, SECRET)
    expect(payload?.sub).toBe('123')
    expect(payload?.login).toBe('alex')
    expect(payload?.isAdmin).toBe(false)
  })

  it('returns null for a tampered token', () => {
    const token = signJwt({ sub: '123', login: 'alex', isAdmin: false }, SECRET, 3600)
    const tampered = token.slice(0, -5) + 'XXXXX'
    expect(verifyJwt(tampered, SECRET)).toBeNull()
  })

  it('returns null for an expired token', () => {
    const token = signJwt({ sub: '1', login: 'x', isAdmin: false }, SECRET, -1)
    expect(verifyJwt(token, SECRET)).toBeNull()
  })

  it('returns null for a token signed with a different secret', () => {
    const token = signJwt({ sub: '1', login: 'x', isAdmin: false }, SECRET, 3600)
    expect(verifyJwt(token, 'wrong-secret')).toBeNull()
  })
})
```

- [ ] **Step 2: Run to confirm failure**

```bash
pnpm test server/auth/jwtUtils.test.ts
```

Expected: FAIL — module not found

- [ ] **Step 3: Implement jwtUtils.ts**

Create `server/auth/jwtUtils.ts`:

```ts
import { createHmac, timingSafeEqual } from 'node:crypto'

export interface JwtPayload {
  sub: string    // GitHub numeric user ID
  login: string  // GitHub username (display only)
  isAdmin: boolean
  exp: number    // Unix timestamp
}

function base64url(buf: Buffer | string): string {
  const b = typeof buf === 'string' ? Buffer.from(buf) : buf
  return b.toString('base64').replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '')
}

function sign(data: string, secret: string): string {
  return base64url(createHmac('sha256', secret).update(data).digest())
}

export function signJwt(payload: Omit<JwtPayload, 'exp'>, secret: string, expiresInSeconds: number): string {
  const header = base64url(JSON.stringify({ alg: 'HS256', typ: 'JWT' }))
  const body = base64url(JSON.stringify({ ...payload, exp: Math.floor(Date.now() / 1000) + expiresInSeconds }))
  const sig = sign(`${header}.${body}`, secret)
  return `${header}.${body}.${sig}`
}

export function verifyJwt(token: string, secret: string): JwtPayload | null {
  try {
    const parts = token.split('.')
    if (parts.length !== 3)
      return null
    const [header, body, sig] = parts
    const expected = sign(`${header}.${body}`, secret)
    const expectedBuf = Buffer.from(expected)
    const sigBuf = Buffer.from(sig)
    if (expectedBuf.length !== sigBuf.length || !timingSafeEqual(expectedBuf, sigBuf))
      return null
    const payload: JwtPayload = JSON.parse(Buffer.from(body, 'base64').toString())
    if (payload.exp < Math.floor(Date.now() / 1000))
      return null
    return payload
  }
  catch {
    return null
  }
}
```

- [ ] **Step 4: Implement githubOAuth.ts**

Create `server/auth/githubOAuth.ts`:

```ts
import process from 'node:process'

const GH_TOKEN_URL = 'https://github.com/login/oauth/access_token'
const GH_API = 'https://api.github.com'

export interface GitHubUser {
  id: string         // numeric, as string
  login: string
  name: string | null
  avatar_url: string
}

export async function exchangeCodeForToken(code: string): Promise<string> {
  const res = await fetch(GH_TOKEN_URL, {
    method: 'POST',
    headers: { 'Accept': 'application/json', 'Content-Type': 'application/json' },
    body: JSON.stringify({
      client_id: process.env.GITHUB_CLIENT_ID,
      client_secret: process.env.GITHUB_CLIENT_SECRET,
      code,
    }),
  })
  const data = await res.json() as { access_token?: string; error?: string }
  if (!data.access_token)
    throw new Error(data.error ?? 'No access_token returned')
  return data.access_token
}

export async function getGitHubUser(accessToken: string): Promise<GitHubUser> {
  const res = await fetch(`${GH_API}/user`, {
    headers: { Authorization: `Bearer ${accessToken}`, 'User-Agent': 'claude-agent-dashboard' },
  })
  if (!res.ok)
    throw new Error(`GitHub /user returned ${res.status}`)
  const u = await res.json() as { id: number; login: string; name: string | null; avatar_url: string }
  return { id: String(u.id), login: u.login, name: u.name, avatar_url: u.avatar_url }
}

/**
 * Check org membership. Uses the user's token when GITHUB_ORG_MEMBERSHIP_PUBLIC=true,
 * otherwise uses GITHUB_SERVER_TOKEN for private-membership orgs.
 */
export async function isOrgMember(githubLogin: string, userAccessToken: string): Promise<boolean> {
  const org = process.env.GITHUB_ORG
  if (!org)
    return true // no org restriction configured

  const token = process.env.GITHUB_ORG_MEMBERSHIP_PUBLIC === 'true'
    ? userAccessToken
    : (process.env.GITHUB_SERVER_TOKEN ?? userAccessToken)

  const res = await fetch(`${GH_API}/orgs/${org}/members/${githubLogin}`, {
    headers: { Authorization: `Bearer ${token}`, 'User-Agent': 'claude-agent-dashboard' },
  })
  // 204 = member, 302/404 = not a member
  return res.status === 204
}
```

- [ ] **Step 5: Implement requireAuth.ts**

Create `server/auth/requireAuth.ts`:

```ts
import type { NextFunction, Request, Response } from 'express'
import process from 'node:process'
import { verifyJwt } from './jwtUtils.js'

declare global {
  namespace Express {
    interface Request {
      user?: { id: string; login: string; isAdmin: boolean }
    }
  }
}

export function isAuthEnabled(): boolean {
  return !!(process.env.GITHUB_CLIENT_ID && process.env.GITHUB_CLIENT_SECRET)
}

export function requireAuth(req: Request, res: Response, next: NextFunction): void {
  if (!isAuthEnabled()) {
    // Standalone mode: treat every request as an unauthenticated admin
    req.user = { id: 'standalone', login: 'local', isAdmin: true }
    next()
    return
  }
  const token = req.cookies?.dashboard_session
  if (!token) {
    res.status(401).json({ error: 'Not authenticated' })
    return
  }
  const payload = verifyJwt(token, process.env.JWT_SECRET ?? '')
  if (!payload) {
    res.clearCookie('dashboard_session')
    res.status(401).json({ error: 'Session expired' })
    return
  }
  req.user = { id: payload.sub, login: payload.login, isAdmin: payload.isAdmin }
  next()
}
```

- [ ] **Step 6: Run JWT tests**

```bash
pnpm test server/auth/jwtUtils.test.ts
```

Expected: all PASS

- [ ] **Step 7: Type-check**

```bash
pnpm typecheck
```

Expected: no errors

- [ ] **Step 8: Commit**

```bash
git add server/auth/
git commit -m "feat(auth): JWT utils, GitHub OAuth helpers, requireAuth middleware"
```

---

### Task 8: Wire auth routes + middleware into index.ts

**Files:**
- Modify: `server/index.ts`

The `cookie-parser` package is needed. Install it first.

- [ ] **Step 1: Install cookie-parser**

```bash
pnpm add cookie-parser
pnpm add -D @types/cookie-parser
```

- [ ] **Step 2: Add auth imports and cookie middleware in index.ts**

At the top of `server/index.ts`, add after existing imports:

```ts
import cookieParser from 'cookie-parser'
import process from 'node:process'
import { exchangeCodeForToken, getGitHubUser, isOrgMember } from './auth/githubOAuth.js'
import { isAuthEnabled, requireAuth } from './auth/requireAuth.js'
import { signJwt } from './auth/jwtUtils.js'
import { upsertUser } from './db/usersRepo.js'
```

- [ ] **Step 3: Register cookie-parser and public auth routes in start()**

Inside `start()`, immediately after `app.use(express.json())`, add:

```ts
  app.use(cookieParser())

  // ─── Auth routes (public — before requireAuth) ───────────

  app.get('/auth/login', (_req, res) => {
    if (!isAuthEnabled()) {
      res.redirect('/')
      return
    }
    const params = new URLSearchParams({
      client_id: process.env.GITHUB_CLIENT_ID!,
      scope: 'read:org',
      redirect_uri: `http://${HOST}:${PORT}/auth/callback`,
    })
    res.redirect(`https://github.com/login/oauth/authorize?${params}`)
  })

  app.get('/auth/callback', async (req, res) => {
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
      const user = upsertUser({ id: ghUser.id, githubLogin: ghUser.login, displayName: ghUser.name, avatarUrl: ghUser.avatar_url })
      const token = signJwt(
        { sub: user.id, login: user.githubLogin, isAdmin: user.isAdmin },
        process.env.JWT_SECRET ?? 'change-me',
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

  app.post('/auth/logout', (_req, res) => {
    res.clearCookie('dashboard_session')
    res.redirect('/auth/login')
  })

  app.get('/api/me', requireAuth, (req, res) => {
    if (!isAuthEnabled()) {
      res.json({ user: null, isAdmin: true, authEnabled: false })
      return
    }
    res.json({ user: req.user, isAdmin: req.user?.isAdmin ?? false, authEnabled: true })
  })
```

- [ ] **Step 4: Apply requireAuth to all /api/* routes**

After the auth routes block and before any other `app.get('/api/...')` calls, add:

```ts
  // All /api/* routes (except /auth/* and /api/me above) require authentication
  app.use('/api', requireAuth)
```

Remove the duplicate `/api/me` route if it was placed after this middleware. Ensure `/auth/*` routes are registered BEFORE `app.use('/api', requireAuth)`.

- [ ] **Step 5: Run tests and type-check**

```bash
pnpm test && pnpm typecheck
```

Expected: all PASS, no errors

- [ ] **Step 6: Commit**

```bash
git add server/index.ts package.json pnpm-lock.yaml
git commit -m "feat(auth): wire GitHub OAuth routes and requireAuth middleware into Express server"
```

---

## Phase 3 — User-Scoped Tasks

### Task 9: Add user_id filtering to tasksRepo

**Files:**
- Modify: `server/db/tasksRepo.ts`

- [ ] **Step 1: Add `userId` to `CreateTaskInput` and a new `listTasksForUser` function**

In `server/db/tasksRepo.ts`:

Add `userId?: string | null` to `CreateTaskInput` interface (after `priority?`):

```ts
export interface CreateTaskInput {
  // ... existing fields ...
  priority?: TaskPriority
  userId?: string | null   // add this
}
```

In the `createTask` function body, add `user_id` to the INSERT. Find the INSERT statement and add the column:

```ts
// In the INSERT prepared statement, add:
// Column list:  ..., user_id
// Values:       ..., ?
// And pass input.userId ?? null as the corresponding argument
```

Add a new exported function after `listTasks`:

```ts
/**
 * Lists tasks visible to a user.
 * Admins see all tasks. Regular users see only their own tasks.
 * Tasks with user_id = NULL are visible to admins only (system/legacy tasks).
 */
export function listTasksForUser(userId: string, isAdmin: boolean, db: Database = getDb()): PipelineTask[] {
  const query = isAdmin
    ? `SELECT tasks.*, ${IS_BLOCKED_EXPR}, ${IS_UNSATISFIABLE_EXPR} FROM tasks ORDER BY created_at DESC`
    : `SELECT tasks.*, ${IS_BLOCKED_EXPR}, ${IS_UNSATISFIABLE_EXPR} FROM tasks WHERE user_id = ? ORDER BY created_at DESC`
  const rows = isAdmin
    ? (db.prepare(query).all() as TaskRow[])
    : (db.prepare(query).all(userId) as TaskRow[])
  return rows.map(rowToTask)
}
```

Note: `IS_BLOCKED_EXPR` and `IS_UNSATISFIABLE_EXPR` are already defined as constants at the top of the file — use them exactly as the existing `listTasks` function does.

- [ ] **Step 2: Run existing tests**

```bash
pnpm test server/db/
```

Expected: all PASS

- [ ] **Step 3: Commit**

```bash
git add server/db/tasksRepo.ts
git commit -m "feat(db): add userId to CreateTaskInput and listTasksForUser with admin bypass"
```

---

### Task 10: Scope task routes by authenticated user

**Files:**
- Modify: `server/routes/taskRoutes.ts`

- [ ] **Step 1: Replace `listTasks()` with `listTasksForUser()` in the GET /tasks route**

In `server/routes/taskRoutes.ts`, find the `router.get('/tasks', ...)` handler (around line 147) and update it:

```ts
router.get('/tasks', (req, res) => {
  const stage = req.query.stage as string | undefined
  const user = req.user!   // set by requireAuth middleware
  if (stage) {
    if (!VALID_STAGES.has(stage as PipelineStage)) {
      res.status(400).json({ error: 'Invalid stage' })
      return
    }
    // For stage-filtered queries, apply user scoping inline
    const all = listTasksForUser(user.id, user.isAdmin)
    res.json(all.filter(t => t.currentStage === stage).map(enrichTask))
    return
  }
  res.json(listTasksForUser(user.id, user.isAdmin).map(enrichTask))
})
```

Add the import for `listTasksForUser` at the top of the file next to the existing `listTasks` import:

```ts
import {
  // ... existing imports ...
  listTasksForUser,
} from '../db/tasksRepo.js'
```

- [ ] **Step 2: Pass userId when creating tasks**

In the `mutationRouter.post('/tasks', ...)` handler, find the `createTask({...})` call and add `userId: req.user!.id` to the input:

```ts
const task = createTask({
  slug,
  title,
  // ... existing fields ...
  userId: req.user!.id,
})
```

- [ ] **Step 3: Type-check**

```bash
pnpm typecheck
```

Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add server/routes/taskRoutes.ts
git commit -m "feat: scope task list and creation by authenticated user"
```

---

## Phase 4 — Remote Registration

### Task 11: Remote registration REST routes

**Files:**
- Create: `server/routes/remoteRoutes.ts`

- [ ] **Step 1: Implement remoteRoutes.ts**

Create `server/routes/remoteRoutes.ts`:

```ts
import type { Router } from 'express'
import { Router as createRouter } from 'express'
import {
  createRemoteRegistration,
  deleteRemoteRegistration,
  listRemoteRegistrationsForUser,
} from '../db/remoteRegistrationsRepo.js'

const REMOTE_TIMEOUT_MS = 5000

async function testRemoteConnection(url: string, bearerKey: string | null): Promise<boolean> {
  try {
    const controller = new AbortController()
    const timeout = setTimeout(() => controller.abort(), REMOTE_TIMEOUT_MS)
    const res = await fetch(`${url}/api/agents`, {
      signal: controller.signal,
      headers: bearerKey ? { Authorization: `Bearer ${bearerKey}` } : {},
    })
    clearTimeout(timeout)
    return res.ok
  }
  catch {
    return false
  }
}

export function createRemoteRouter(): Router {
  const router = createRouter()

  router.get('/', (req, res) => {
    const registrations = listRemoteRegistrationsForUser(req.user!.id)
    // Strip bearerKey from responses — never send tokens to the browser
    res.json(registrations.map(({ bearerKey: _, ...r }) => r))
  })

  router.post('/', async (req, res) => {
    const { url, name, bearerKey } = req.body ?? {}
    if (!url || typeof url !== 'string') {
      res.status(400).json({ error: 'url is required' })
      return
    }
    try {
      new URL(url) // validate URL format
    }
    catch {
      res.status(400).json({ error: 'url is not a valid URL' })
      return
    }
    const ok = await testRemoteConnection(url, typeof bearerKey === 'string' ? bearerKey : null)
    const reg = createRemoteRegistration({
      userId: req.user!.id,
      url,
      name: typeof name === 'string' ? name : null,
      bearerKey: typeof bearerKey === 'string' ? bearerKey : null,
    })
    const { bearerKey: _, ...safeReg } = reg
    res.status(201).json({ ...safeReg, connectionOk: ok })
  })

  router.delete('/:id', (req, res) => {
    const deleted = deleteRemoteRegistration(req.params.id, req.user!.id)
    if (!deleted) {
      res.status(404).json({ error: 'Not found' })
      return
    }
    res.status(204).end()
  })

  return router
}
```

- [ ] **Step 2: Mount the router in index.ts**

In `server/index.ts`, add after existing route imports:

```ts
import { createRemoteRouter } from './routes/remoteRoutes.js'
```

Then inside `start()`, after mounting the task router, add:

```ts
  app.use('/api/remotes', requireAuth, createRemoteRouter())
```

- [ ] **Step 3: Type-check**

```bash
pnpm typecheck
```

Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add server/routes/remoteRoutes.ts server/index.ts
git commit -m "feat: add /api/remotes REST routes for user-scoped local dashboard registration"
```

---

### Task 12: Update remoteAggregator to accept RemoteRegistration[]

**Files:**
- Modify: `server/remoteAggregator.ts`

- [ ] **Step 1: Update aggregateAgents signature and fetchRemoteAgents**

In `server/remoteAggregator.ts`, replace the `aggregateAgents` function and the `fetchRemoteAgents` helper signature:

```ts
export interface RemoteTarget {
  url: string
  bearerKey?: string | null
  name?: string | null
}

async function fetchRemoteAgents(remote: RemoteTarget): Promise<(Agent & { machine: string })[]> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), REMOTE_TIMEOUT_MS)

  const headers: Record<string, string> = { [ORIGIN_HEADER]: localHostname }
  if (remote.bearerKey)
    headers.Authorization = `Bearer ${remote.bearerKey}`

  try {
    const res = await fetch(`${remote.url}/api/agents`, {
      signal: controller.signal,
      headers,
    })
    clearTimeout(timeout)
    if (!res.ok) {
      consola.warn(`[remotes] ${remote.url} responded with HTTP ${res.status}`)
      return []
    }

    const contentLength = res.headers.get('content-length')
    if (contentLength && Number.parseInt(contentLength, 10) > MAX_RESPONSE_BYTES) {
      consola.warn(`[remotes] ${remote.url} response too large`)
      return []
    }

    const text = await res.text()
    if (text.length > MAX_RESPONSE_BYTES) {
      consola.warn(`[remotes] ${remote.url} response too large`)
      return []
    }

    const data = JSON.parse(text)
    if (!Array.isArray(data)) {
      consola.warn(`[remotes] ${remote.url} returned non-array`)
      return []
    }

    const label = remote.name ?? new URL(remote.url).hostname
    return data
      .filter((a: Record<string, unknown>) => !a.machine)
      .filter((a: unknown) => {
        if (!validateAgent(a)) {
          consola.debug(`[remotes] Skipping invalid agent from ${remote.url}`)
          return false
        }
        return true
      })
      .map((a: Agent) => ({ ...a, machine: label }))
  }
  catch (err) {
    clearTimeout(timeout)
    const reason = (err as Error).name === 'AbortError' ? 'timeout' : (err as Error).message
    consola.warn(`[remotes] Failed to reach ${remote.url}: ${reason}`)
    return []
  }
}

export async function aggregateAgents(localAgents: Agent[], remotes: RemoteTarget[]): Promise<Agent[]> {
  if (remotes.length === 0)
    return localAgents
  const tagged = localAgents.map(a => ({ ...a, machine: localHostname }))
  const remoteResults = await Promise.all(remotes.map(r => fetchRemoteAgents(r)))
  return [...tagged, ...remoteResults.flat()]
}
```

Update `getRemoteUrls()` to return `RemoteTarget[]` and rename it:

```ts
export function getEnvRemoteTargets(): RemoteTarget[] {
  const env = process.env.DASHBOARD_REMOTES
  if (!env)
    return []
  return env.split(',')
    .map(u => u.trim())
    .filter(Boolean)
    .filter((u) => {
      try {
        const parsed = new URL(u)
        if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:')
          return false
        const selfHosts = ['localhost', '127.0.0.1', '[::1]', '0.0.0.0']
        if (selfHosts.includes(parsed.hostname) && parsed.port === String(process.env.DASHBOARD_PORT || '13120')) {
          consola.warn(`[remotes] Skipping ${u} — points to this dashboard instance`)
          return false
        }
        return true
      }
      catch {
        consola.warn(`[remotes] Ignoring invalid URL: ${u}`)
        return false
      }
    })
    .map(url => ({ url }))
}
```

- [ ] **Step 2: Update all callers of getRemoteUrls in index.ts**

In `server/index.ts`, replace all occurrences of `getRemoteUrls` with `getEnvRemoteTargets` and update `aggregateAgents` call sites to merge env remotes with DB remotes per user:

In the SSE broadcast interval function, replace the remote aggregation block:

```ts
// Replace:
//   const remoteUrls = getRemoteUrls()
//   const agents = remoteUrls.length > 0 ? await aggregateAgents(localAgents, remoteUrls) : localAgents
// With (for SSE — user-scoped, so we can't scope here; use env remotes only in SSE):
const envRemotes = getEnvRemoteTargets()
const agents = await aggregateAgents(localAgents, envRemotes)
```

In the `GET /api/agents` handler, same substitution.

Note: Per-user remote loading for SSE is handled in Task 14 which updates the SSE handler further.

- [ ] **Step 3: Type-check**

```bash
pnpm typecheck
```

Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add server/remoteAggregator.ts server/index.ts
git commit -m "feat: update remoteAggregator to accept RemoteTarget[] with optional bearer key"
```

---

### Task 13: Add DASHBOARD_API_TOKEN protection to /api/agents

**Files:**
- Modify: `server/index.ts`

When `DASHBOARD_API_TOKEN` is set, `/api/agents` (the endpoint other servers pull) requires a Bearer token. This protects local instances that are registered with a central server.

- [ ] **Step 1: Add token middleware before the /api/agents route**

In `server/index.ts`, before the `app.get('/api/agents', ...)` handler, add:

```ts
  // Optional bearer-token protection for the agents endpoint.
  // Set DASHBOARD_API_TOKEN to require remote callers to authenticate.
  function requireApiToken(req: express.Request, res: express.Response, next: express.NextFunction): void {
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
```

Then update the route to use both middleware:

```ts
  app.get('/api/agents', requireApiToken, requireAuth, async (req, res) => {
    // ... existing body unchanged ...
  })

  app.get('/api/agents/stream', requireApiToken, (req, res) => {
    // ... existing body unchanged ...
  })
```

- [ ] **Step 2: Type-check + run tests**

```bash
pnpm typecheck && pnpm test
```

Expected: no errors, all PASS

- [ ] **Step 3: Commit**

```bash
git add server/index.ts
git commit -m "feat: add optional DASHBOARD_API_TOKEN bearer protection on /api/agents"
```

---

### Task 14: Load per-user remotes in SSE broadcast and /api/agents

**Files:**
- Modify: `server/index.ts`

The SSE handler broadcasts to all connected clients at once — it can't differentiate users per SSE connection without tracking which user owns which `res` object. We need to track `{ res, userId }` pairs.

- [ ] **Step 1: Track user with each SSE client connection**

In `server/index.ts`, replace `const sseClients = new Set<express.Response>()` with:

```ts
  interface SseClient { res: express.Response; userId: string; isAdmin: boolean }
  const sseClients = new Set<SseClient>()
```

Update the SSE connection handler:

```ts
  app.get('/api/agents/stream', requireApiToken, requireAuth, (req, res) => {
    res.writeHead(200, {
      'Content-Type': 'text/event-stream',
      'Cache-Control': 'no-cache',
      'Connection': 'keep-alive',
      'X-Accel-Buffering': 'no',
    })
    res.flushHeaders()

    const client: SseClient = { res, userId: req.user!.id, isAdmin: req.user!.isAdmin }
    sseClients.add(client)
    startSSEBroadcast()
    req.on('close', () => {
      sseClients.delete(client)
      stopSSEBroadcast()
    })
  })
```

- [ ] **Step 2: Load per-user remotes in the SSE broadcast loop**

Replace the SSE broadcast interval body in `server/index.ts` with a version that builds per-user agent lists:

```ts
    sseBroadcastId = setInterval(async () => {
      try {
        const localAgents = await getAgents()
        const envRemotes = getEnvRemoteTargets()

        // Record trend from local + env remotes only (aggregated baseline)
        const baselineAgents = await aggregateAgents(localAgents, envRemotes)
        const totalCost = baselineAgents.reduce((sum, a) => sum + a.costEstimate, 0)
        const totalTokens = baselineAgents.reduce((sum, a) => {
          const u = a.tokenUsage
          return sum + u.inputTokens + u.outputTokens + u.cacheReadTokens + u.cacheCreationTokens
        }, 0)
        costTrend.push({ t: Date.now(), cost: totalCost, tokens: totalTokens })
        if (costTrend.length > MAX_TREND_POINTS)
          costTrend.shift()

        const trendSlice = costTrend.slice(-60)

        // Broadcast per-user: each client gets local agents + their own remotes
        await Promise.all([...sseClients].map(async (client) => {
          try {
            if (client.res.writableEnded)
              return

            const userRemotes = isAuthEnabled()
              ? listRemoteRegistrationsForUser(client.userId).map(r => ({
                  url: r.url,
                  bearerKey: r.bearerKey,
                  name: r.name,
                }))
              : []

            const allRemotes = [...envRemotes, ...userRemotes]
            const agents = await aggregateAgents(localAgents, allRemotes)
            const payload = JSON.stringify({ agents, trend: trendSlice })
            client.res.write(`data: ${payload}\n\n`)
          }
          catch {
            sseClients.delete(client)
          }
        }))
      }
      catch (err) {
        console.error('SSE broadcast error:', err)
      }
    }, SSE_INTERVAL_MS)
```

Add the import for `listRemoteRegistrationsForUser` at the top:

```ts
import { listRemoteRegistrationsForUser } from './db/remoteRegistrationsRepo.js'
```

- [ ] **Step 3: Update stopSSEBroadcast**

```ts
  function stopSSEBroadcast() {
    if (sseBroadcastId && sseClients.size === 0) {
      clearInterval(sseBroadcastId)
      sseBroadcastId = null
    }
  }
```

No change needed here — `sseClients.size` still works with the new Set type.

- [ ] **Step 4: Type-check**

```bash
pnpm typecheck
```

Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add server/index.ts
git commit -m "feat: SSE broadcasts per-user agent list including registered local remotes"
```

---

## Phase 5 — Frontend

### Task 15: useUser composable

**Files:**
- Create: `src/composables/useUser.ts`

- [ ] **Step 1: Implement useUser.ts**

Create `src/composables/useUser.ts`:

```ts
import { ref, readonly } from 'vue'

export interface DashboardUser {
  id: string
  login: string
  isAdmin: boolean
}

const user = ref<DashboardUser | null>(null)
const isAdmin = ref(true) // default true for standalone
const authEnabled = ref(false)
const loaded = ref(false)

async function loadUser(): Promise<void> {
  try {
    const res = await fetch('/api/me')
    if (!res.ok) {
      // 401 = not authenticated in auth-enabled mode
      authEnabled.value = true
      user.value = null
      return
    }
    const data = await res.json() as { user: DashboardUser | null; isAdmin: boolean; authEnabled: boolean }
    user.value = data.user
    isAdmin.value = data.isAdmin
    authEnabled.value = data.authEnabled
  }
  catch {
    // network error — assume standalone
  }
  finally {
    loaded.value = true
  }
}

export function useUser() {
  return {
    user: readonly(user),
    isAdmin: readonly(isAdmin),
    authEnabled: readonly(authEnabled),
    isAuthenticated: readonly(ref(user.value !== null || !authEnabled.value)),
    loaded: readonly(loaded),
    loadUser,
  }
}
```

- [ ] **Step 2: Commit**

```bash
git add src/composables/useUser.ts
git commit -m "feat(frontend): add useUser composable for auth state"
```

---

### Task 16: LoginPage component

**Files:**
- Create: `src/components/LoginPage.vue`

- [ ] **Step 1: Implement LoginPage.vue**

Create `src/components/LoginPage.vue`:

```vue
<script setup lang="ts">
const org = import.meta.env.VITE_GITHUB_ORG ?? ''
</script>

<template>
  <div class="min-h-screen flex items-center justify-center bg-bg-base">
    <div class="flex flex-col items-center gap-6 p-10 rounded-2xl border border-border-subtle bg-bg-surface shadow-lg max-w-sm w-full">
      <div class="text-2xl font-semibold text-text-primary">Claude Agent Dashboard</div>
      <p v-if="org" class="text-sm text-text-muted text-center">
        Access restricted to members of <strong>{{ org }}</strong>
      </p>
      <a
        href="/auth/login"
        class="flex items-center gap-2 px-5 py-2.5 rounded-lg bg-surface-raised border border-border-subtle text-text-primary hover:bg-surface-hover transition-colors text-sm font-medium"
      >
        <svg viewBox="0 0 16 16" class="w-4 h-4 fill-current" aria-hidden="true">
          <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z"/>
        </svg>
        Login with GitHub
      </a>
    </div>
  </div>
</template>
```

- [ ] **Step 2: Commit**

```bash
git add src/components/LoginPage.vue
git commit -m "feat(frontend): add LoginPage component with GitHub OAuth redirect"
```

---

### Task 17: RemoteSettings component

**Files:**
- Create: `src/components/RemoteSettings.vue`

- [ ] **Step 1: Implement RemoteSettings.vue**

Create `src/components/RemoteSettings.vue`:

```vue
<script setup lang="ts">
import { ref, onMounted } from 'vue'

interface Remote { id: string; url: string; name: string | null; createdAt: string; connectionOk?: boolean }

const remotes = ref<Remote[]>([])
const form = ref({ url: '', name: '', bearerKey: '' })
const saving = ref(false)
const error = ref<string | null>(null)

async function load() {
  try {
    const res = await fetch('/api/remotes')
    if (res.ok) remotes.value = await res.json()
  } catch {}
}

async function add() {
  if (!form.value.url) return
  saving.value = true
  error.value = null
  try {
    const res = await fetch('/api/remotes', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url: form.value.url, name: form.value.name || null, bearerKey: form.value.bearerKey || null }),
    })
    if (!res.ok) {
      const data = await res.json() as { error: string }
      error.value = data.error
      return
    }
    const reg = await res.json() as Remote
    remotes.value.push(reg)
    form.value = { url: '', name: '', bearerKey: '' }
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    saving.value = false
  }
}

async function remove(id: string) {
  await fetch(`/api/remotes/${id}`, { method: 'DELETE' })
  remotes.value = remotes.value.filter(r => r.id !== id)
}

onMounted(load)
</script>

<template>
  <div class="flex flex-col gap-4">
    <h3 class="text-sm font-semibold text-text-primary">Meine lokalen Dashboard-Instanzen</h3>
    <p class="text-xs text-text-muted">Registriere deine lokale Dashboard-Instanz, damit deine lokalen Claude-Sessions hier angezeigt werden. Die lokale Instanz muss über Netzwerk erreichbar sein.</p>

    <div v-if="remotes.length" class="flex flex-col gap-2">
      <div v-for="r in remotes" :key="r.id" class="flex items-center justify-between p-3 rounded-lg border border-border-subtle bg-bg-surface text-sm">
        <div>
          <div class="font-medium text-text-primary">{{ r.name ?? r.url }}</div>
          <div v-if="r.name" class="text-xs text-text-muted">{{ r.url }}</div>
          <span v-if="r.connectionOk !== undefined" class="text-xs" :class="r.connectionOk ? 'text-green-500' : 'text-red-500'">
            {{ r.connectionOk ? '● Verbunden' : '● Nicht erreichbar' }}
          </span>
        </div>
        <button class="text-xs text-red-400 hover:text-red-300 transition-colors" @click="remove(r.id)">Entfernen</button>
      </div>
    </div>
    <p v-else class="text-xs text-text-muted">Keine Registrierungen.</p>

    <form class="flex flex-col gap-2" @submit.prevent="add">
      <input v-model="form.url" type="url" placeholder="http://192.168.1.5:13120" required class="input-field text-sm" />
      <input v-model="form.name" type="text" placeholder="Name (z.B. MacBook)" class="input-field text-sm" />
      <input v-model="form.bearerKey" type="password" placeholder="DASHBOARD_API_TOKEN (optional)" class="input-field text-sm" />
      <p v-if="error" class="text-xs text-red-400">{{ error }}</p>
      <button type="submit" :disabled="saving" class="btn-primary text-sm self-start">
        {{ saving ? 'Wird gespeichert…' : 'Hinzufügen & testen' }}
      </button>
    </form>
  </div>
</template>
```

- [ ] **Step 2: Commit**

```bash
git add src/components/RemoteSettings.vue
git commit -m "feat(frontend): add RemoteSettings component for local dashboard registration"
```

---

### Task 18: Wire auth into App.vue and Settings panel

**Files:**
- Modify: `src/App.vue`
- Modify: `src/components/ApiKeySettings.vue` (or the settings modal component)

- [ ] **Step 1: Add auth guard and login redirect in App.vue**

In `src/App.vue`, add the `useUser` import and call `loadUser` on mount. Add a conditional render that shows `LoginPage` when not authenticated:

At the top of the `<script setup>` block, add:
```ts
import { onMounted, computed } from 'vue'
import LoginPage from './components/LoginPage.vue'
import { useUser } from './composables/useUser'

const { user, isAdmin, authEnabled, loaded, loadUser } = useUser()
const showLogin = computed(() => authEnabled.value && !user.value)

onMounted(() => loadUser())
```

In the `<template>`, wrap the existing content with:
```html
<template>
  <LoginPage v-if="loaded && showLogin" />
  <div v-else-if="loaded">
    <!-- existing App.vue template content here -->
  </div>
  <div v-else class="min-h-screen bg-bg-base" /><!-- loading state -->
</template>
```

- [ ] **Step 2: Add "Meine Remotes" tab to the Settings panel**

Find the settings modal (in `ApiKeySettings.vue` or wherever tabs are rendered) and add a "Meine Remotes" tab that shows `RemoteSettings`:

```vue
<script setup>
import RemoteSettings from './RemoteSettings.vue'
import { useUser } from '../composables/useUser'
const { authEnabled } = useUser()
</script>
```

In the tab list, add (only when auth is enabled):
```html
<button v-if="authEnabled" :class="activeTab === 'remotes' ? 'tab-active' : 'tab'" @click="activeTab = 'remotes'">
  Meine Remotes
</button>
```

In the tab content panels, add:
```html
<RemoteSettings v-if="activeTab === 'remotes'" />
```

- [ ] **Step 3: Run lint and type-check**

```bash
pnpm lint && pnpm typecheck
```

Expected: no errors

- [ ] **Step 4: Start dev server and verify login flow**

```bash
pnpm dev
```

- Without auth env vars: dashboard loads normally, no login prompt — standalone mode confirmed
- With `GITHUB_CLIENT_ID` + `GITHUB_CLIENT_SECRET` set: navigating to `http://localhost:13120` should show the LoginPage

- [ ] **Step 5: Final commit**

```bash
git add src/App.vue src/components/ApiKeySettings.vue src/components/RemoteSettings.vue src/composables/useUser.ts
git commit -m "feat(frontend): wire auth guard, login page, and Remotes settings tab into App"
```

---

## Self-Review Against Spec

**Spec coverage check:**

| Requirement | Covered |
|---|---|
| GitHub OAuth + org-gated | Task 7 (githubOAuth.ts) + Task 8 (index.ts routes) |
| JWT HttpOnly cookie | Task 7 (jwtUtils.ts, requireAuth.ts) + Task 8 |
| Standalone bypass | Task 7 (`isAuthEnabled()`), Task 8 (requireAuth no-op) |
| `users` table | Task 4 (schema.sql) + Task 5 (usersRepo.ts) |
| `remote_registrations` table | Task 4 + Task 6 |
| `user_id` on tasks/api_keys | Task 4 (client.ts migration) |
| Task visibility scoping | Task 9 (listTasksForUser) + Task 10 (taskRoutes) |
| No admin override on remotes | Task 6 (`listRemoteRegistrationsForUser` always filters by userId) |
| Local session registration UI | Task 11 (remoteRoutes) + Task 17 (RemoteSettings.vue) |
| Bearer token on local dashboard | Task 13 (`DASHBOARD_API_TOKEN` middleware) |
| Per-user SSE aggregation | Task 14 (SSE handler loads user remotes) |
| Batch lsof bug fix | Task 1 |
| decodeProjectDir fix | Task 2 |
| Cost trend persistence | Task 3 |
| useUser composable | Task 15 |
| LoginPage | Task 16 |

All spec requirements are covered. No gaps found.
