# Security & Performance Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix 8 confirmed security and performance issues from the OWASP audit + performance analysis — no behavioral changes, no dependency additions, no architecture changes.

**Architecture:** Targeted in-place edits across 5 independent files. Tasks 1–5 touch disjoint files and can be executed in parallel by separate agents.

**Tech Stack:** Node.js 22, Express 5, Vue 3, TypeScript, better-sqlite3, Vitest

---

## Parallel Execution Map

```
Task 1 — server/index.ts                    ─┐
Task 2 — server/mcp/mcpAuth.ts              ─┤  ALL INDEPENDENT
         server/routes/agentRoutes.ts        ─┤  Run in parallel
Task 3 — server/jsonlParser.ts              ─┤
Task 4 — src/composables/useTasks.ts        ─┤
Task 5 — server/pipeline/orchestrator.ts    ─┘
```

---

## Task 1: CSP Security Headers

**Files:**
- Modify: `server/index.ts`

> **Why:** The server currently sends no security headers. Any response — error pages, API JSON — lacks `Content-Security-Policy`, `X-Frame-Options`, `X-Content-Type-Options`. For the frontend SPA these headers prevent XSS escalation and clickjacking. No new dependency needed; a small inline middleware suffices.

- [ ] **Step 1: Read current index.ts header section**

```bash
head -80 server/index.ts
```

Expected: see `app.use(express.json(...))` and `app.use(cookieParser())` near line 68-70.

- [ ] **Step 2: Add security headers middleware before any route registration**

In `server/index.ts`, after line 70 (`app.use(cookieParser())`), insert:

```typescript
  // Security headers — applied to every response before any route handler
  app.use((_req, res, next) => {
    // Prevent MIME-type sniffing
    res.setHeader('X-Content-Type-Options', 'nosniff')
    // Disallow embedding in iframes (clickjacking)
    res.setHeader('X-Frame-Options', 'DENY')
    // Strict CSP: only same-origin resources; inline styles allowed (Vue SFC scoped)
    const isDev = process.env.NODE_ENV !== 'production'
    const csp = [
      `default-src 'self'`,
      // Vite HMR in dev needs eval; strip in prod
      isDev ? `script-src 'self' 'unsafe-eval'` : `script-src 'self'`,
      `style-src 'self' 'unsafe-inline'`,
      // SSE + Vite WebSocket
      `connect-src 'self' ${isDev ? 'ws: wss:' : ''}`.trim(),
      `img-src 'self' data:`,
      `font-src 'self'`,
      `frame-ancestors 'none'`,
    ].join('; ')
    res.setHeader('Content-Security-Policy', csp)
    next()
  })
```

- [ ] **Step 3: Run type check**

```bash
pnpm typecheck
```

Expected: no errors.

- [ ] **Step 4: Start dev server, verify headers**

```bash
pnpm dev &
sleep 3
curl -s -I http://127.0.0.1:13120/api/agents | grep -i "content-security\|x-frame\|x-content"
kill %1
```

Expected output (contains):
```
content-security-policy: default-src 'self'; ...
x-frame-options: DENY
x-content-type-options: nosniff
```

- [ ] **Step 5: Commit**

```bash
git add server/index.ts
git commit -m "fix(security): add CSP, X-Frame-Options, X-Content-Type-Options headers"
```

---

## Task 2: Auth-Failure and Rate-Limit Logging

**Files:**
- Modify: `server/mcp/mcpAuth.ts`
- Modify: `server/routes/agentRoutes.ts`

> **Why:** Failed auth attempts and rate-limit hits are currently silent — no log entry, no IP, no timestamp. OWASP A09 (Security Logging). Logging these events makes brute-force visible and aids incident response. Both changes are one-liners; `consola` is already imported in the project.

### 2a — MCP auth failure logging (`server/mcp/mcpAuth.ts`)

- [ ] **Step 1: Read the current middleware**

```bash
sed -n '1,90p' server/mcp/mcpAuth.ts
```

Expected: `mcpAuthMiddleware` function visible around line 63.

- [ ] **Step 2: Add import + log on invalid token**

Add `import { consola } from 'consola'` at the top of `server/mcp/mcpAuth.ts` (after the existing imports).

Then in `mcpAuthMiddleware`, replace the block (around line 71-74):

```typescript
  const key = getApiKeyByHash(hash)
  if (!key) {
    res.status(401).json({ error: 'Invalid or revoked API key' })
    return
  }
```

with:

```typescript
  const key = getApiKeyByHash(hash)
  if (!key) {
    const ip = req.ip ?? req.socket.remoteAddress ?? 'unknown'
    consola.warn(`[mcpAuth] invalid token from ${ip}`)
    res.status(401).json({ error: 'Invalid or revoked API key' })
    return
  }
```

- [ ] **Step 3: Run type check**

```bash
pnpm typecheck
```

Expected: no errors.

### 2b — Rate-limit hit logging (`server/routes/agentRoutes.ts`)

- [ ] **Step 4: Read the current rate-limit block**

```bash
sed -n '54,70p' server/routes/agentRoutes.ts
```

Expected: `if (!spawnManager.isSpawnAllowed())` block visible.

- [ ] **Step 5: Add log on rate-limit hit**

In `server/routes/agentRoutes.ts`, `consola` is not yet imported. Add at the top (after existing imports):

```typescript
import { consola } from 'consola'
```

Then replace the rate-limit block (around line 59-63):

```typescript
    if (!spawnManager.isSpawnAllowed()) {
      const windowSecs = Math.round(spawnManager.getRateLimitConfig().windowMs / 1000)
      const { max } = spawnManager.getRateLimitConfig()
      res.status(429).json({ error: `Too many spawn requests. Max ${max} per ${windowSecs} seconds.` })
      return
    }
```

with:

```typescript
    if (!spawnManager.isSpawnAllowed()) {
      const { windowMs, max } = spawnManager.getRateLimitConfig()
      const windowSecs = Math.round(windowMs / 1000)
      const ip = req.ip ?? req.socket.remoteAddress ?? 'unknown'
      consola.warn(`[spawnManager] rate limit hit from ${ip} (max ${max}/${windowSecs}s)`)
      res.status(429).json({ error: `Too many spawn requests. Max ${max} per ${windowSecs} seconds.` })
      return
    }
```

- [ ] **Step 6: Run type check**

```bash
pnpm typecheck
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add server/mcp/mcpAuth.ts server/routes/agentRoutes.ts
git commit -m "fix(security): log auth failures and rate-limit hits (OWASP A09)"
```

---

## Task 3: JSONL Hardening — UUID Validation + Parallel Subagent Reads

**Files:**
- Modify: `server/jsonlParser.ts`

> **Why:** Two independent improvements in the same file. (A) `parseFullSession` constructs a filesystem path from a caller-supplied `sessionId` without validating it is a UUID first — `UUID_RE` already exists in `server/constants.ts`. (B) `findSubagents` reads each `.jsonl` serially with `await` inside a `for` loop; `Promise.all` gives the same result with parallel I/O, cutting latency proportionally to the number of subagents.

### 3a — UUID validation in parseFullSession

- [ ] **Step 1: Read parseFullSession**

```bash
sed -n '496,515p' server/jsonlParser.ts
```

Expected: `export async function parseFullSession(sessionId: string, ...` visible.

- [ ] **Step 2: Check existing UUID_RE import**

```bash
grep -n "UUID_RE\|constants" server/jsonlParser.ts | head -5
```

If `UUID_RE` is not yet imported, note which constants are imported from `./constants.js`.

- [ ] **Step 3: Add UUID_RE import (if missing) and guard**

If `UUID_RE` is not imported in `server/jsonlParser.ts`, add it to the constants import line, e.g.:
```typescript
import { UUID_RE, WHITESPACE_RE } from './constants.js'
```

Then in `parseFullSession`, add the guard as the very first line of the function body:

```typescript
export async function parseFullSession(sessionId: string, lastOnly: boolean = false): Promise<OutputMessage[]> {
  if (!UUID_RE.test(sessionId))
    return []

  const projectDirs = await readdir(CLAUDE_PROJECTS_DIR, { withFileTypes: true })
  // ... rest unchanged
```

- [ ] **Step 4: Run type check**

```bash
pnpm typecheck
```

Expected: no errors.

### 3b — Parallel subagent reads in findSubagents

- [ ] **Step 5: Read findSubagents**

```bash
sed -n '413,470p' server/jsonlParser.ts
```

Expected: `async function findSubagents(subagentDir: string)` with `for (const entry of entries)` loop visible.

- [ ] **Step 6: Replace serial loop with Promise.all**

Replace the entire body of `findSubagents` from the `const subagents: SubAgentData[] = []` line to `return subagents` with:

```typescript
  const results = await Promise.all(
    entries
      .filter(entry => entry.isFile() && entry.name.endsWith('.jsonl'))
      .map(async (entry) => {
        const filePath = join(subagentDir, entry.name)
        const s = await stat(filePath)
        const raw = await tailRead(filePath)
        const parsed = parseJsonlLines(raw)

        let type = 'unknown'
        let currentAction: string | null = null
        for (const e of parsed) {
          if (e.type === 'user' && e.message?.content) {
            const text = typeof e.message.content === 'string'
              ? e.message.content
              : Array.isArray(e.message.content)
                ? e.message.content.find((b: any) => b.type === 'text')?.text || ''
                : ''
            if (text.length > 0)
              type = text.substring(0, 80)
          }
          if (e.type === 'assistant' && e.message?.content && Array.isArray(e.message.content)) {
            for (const block of e.message.content) {
              if (block.type === 'tool_use')
                currentAction = block.name
            }
          }
        }

        const age = Date.now() - s.mtime.getTime()
        return {
          id: entry.name.replace('.jsonl', ''),
          type,
          status: (age < 60000 ? 'active' : 'completed') as 'active' | 'completed',
          currentAction,
          sessionFile: filePath,
        } satisfies SubAgentData
      }),
  )
  return results
```

- [ ] **Step 7: Run type check**

```bash
pnpm typecheck
```

Expected: no errors.

- [ ] **Step 8: Run unit tests**

```bash
pnpm test
```

Expected: all tests pass.

- [ ] **Step 9: Commit**

```bash
git add server/jsonlParser.ts
git commit -m "fix(security): UUID validation in parseFullSession; perf: parallel subagent JSONL reads"
```

---

## Task 4: Vue shallowRef for Task List

**Files:**
- Modify: `src/composables/useTasks.ts`

> **Why:** `tasks` is currently `ref<PipelineTask[]>([])`. Vue's deep `ref` recursively tracks every nested property — each `task_updated` SSE event triggers reactivity on all 50+ properties of every `PipelineTask`. `shallowRef` tracks only the array reference itself; since tasks are server snapshots (replaced wholesale via `.map()`), deep tracking is wasted work. `useAgents.ts` already correctly uses `shallowRef` for the same pattern.

- [ ] **Step 1: Read the current useTasks.ts import line**

```bash
head -5 src/composables/useTasks.ts
```

Expected: `import { computed, onUnmounted, ref } from 'vue'`

- [ ] **Step 2: Replace ref with shallowRef**

In `src/composables/useTasks.ts`, change the import on line 2 from:

```typescript
import { computed, onUnmounted, ref } from 'vue'
```

to:

```typescript
import { computed, onUnmounted, ref, shallowRef } from 'vue'
```

Then change line 4 from:

```typescript
const tasks = ref<PipelineTask[]>([])
```

to:

```typescript
const tasks = shallowRef<PipelineTask[]>([])
```

- [ ] **Step 3: Run type check**

```bash
pnpm typecheck
```

Expected: no errors.

- [ ] **Step 4: Run unit tests**

```bash
pnpm test
```

Expected: all tests pass.

- [ ] **Step 5: Manual smoke test**

```bash
pnpm dev &
```

Open http://127.0.0.1:13120, navigate to the Pipeline board, create a task. Verify the task appears in the Kanban without console errors. Verify no reactivity warnings in browser console.

```bash
kill %1
```

- [ ] **Step 6: Commit**

```bash
git add src/composables/useTasks.ts
git commit -m "perf(frontend): shallowRef for tasks array — avoid deep reactivity on SSE updates"
```

---

## Task 5: Orchestrator Performance — Deduplicate DB Scans per Tick

**Files:**
- Modify: `server/pipeline/orchestrator.ts`

> **Why:** The orchestrator's tick loop currently calls `listRunningStageRuns()` (full table scan) twice per tick — once in `finalizeCompletedAsyncRuns` (line 565) and once in `pickNextTasksForFreeSlots` (line 793). Additionally, `getPipelineConfigNumber` hits the DB on every call for values (`stageTimeoutSeconds`, `maxParallelOrchestrators`, `maxReviewCycles`) that almost never change, causing 10–50 unnecessary queries/second. Fixes: (A) pass the already-fetched `running` list from `finalizeCompletedAsyncRuns` to `pickNextTasksForFreeSlots`, and (B) add a 5s TTL in-memory cache for `pipeline_config` values.

### 5a — Deduplicate listRunningStageRuns

- [ ] **Step 1: Read progressPendingTasks and its two callees**

```bash
sed -n '559,570p' server/pipeline/orchestrator.ts
sed -n '789,798p' server/pipeline/orchestrator.ts
```

Expected: `progressPendingTasks` calls `finalizeCompletedAsyncRuns()` then `pickNextTasksForFreeSlots()`.

- [ ] **Step 2: Change finalizeCompletedAsyncRuns signature to accept pre-fetched list**

Find the signature `private async finalizeCompletedAsyncRuns(): Promise<void>` and change it to:

```typescript
private async finalizeCompletedAsyncRuns(allRunning?: StageRun[]): Promise<void> {
  const running = (allRunning ?? listRunningStageRuns()).filter(r => r.status === 'running' && r.pid !== null)
  // rest of function body unchanged — only this first line changes
```

The only change is the method signature + the first `const running = ...` line. Everything after that stays identical.

`StageRun` is already imported at the top of the file from `'../../src/types.js'`.

- [ ] **Step 3: Change pickNextTasksForFreeSlots to accept pre-fetched list**

Find `private pickNextTasksForFreeSlots(): void` (around line 789) and change its opening to:

```typescript
private pickNextTasksForFreeSlots(allRunning?: StageRun[]): void {
  const max = getCachedPipelineConfigNumber(MAX_PARALLEL_KEY, DEFAULT_MAX_PARALLEL)
  const running = (allRunning ?? listRunningStageRuns()).filter(r => r.status === 'running')
  // rest of function body unchanged — only signature + first two lines change
```

Note: `getCachedPipelineConfigNumber` is defined in Step 6 of Task 5b below. Apply both changes in the same editing session so the file compiles.

- [ ] **Step 4: Wire the two together in progressPendingTasks**

Change `progressPendingTasks` (around line 559) to:

```typescript
private async progressPendingTasks(): Promise<void> {
  const allRunning = listRunningStageRuns()
  await this.finalizeCompletedAsyncRuns(allRunning)
  this.pickNextTasksForFreeSlots(allRunning)
}
```

- [ ] **Step 5: Run type check**

```bash
pnpm typecheck
```

Expected: no errors.

### 5b — TTL cache for getPipelineConfigNumber

- [ ] **Step 6: Add a TTL-cache helper at the top of orchestrator.ts**

After the constants block (around line 35), add:

```typescript
// 5-second TTL cache for pipeline_config values. These change only via manual
// DB writes — the small staleness window is an acceptable trade-off.
const _configCache = new Map<string, { value: number, expiresAt: number }>()

function getCachedPipelineConfigNumber(key: string, fallback: number): number {
  const cached = _configCache.get(key)
  if (cached && cached.expiresAt > Date.now())
    return cached.value
  const value = getPipelineConfigNumber(key, fallback)
  _configCache.set(key, { value, expiresAt: Date.now() + 5000 })
  return value
}
```

- [ ] **Step 7: Replace all getPipelineConfigNumber calls with the cached version**

Search the file for every call to `getPipelineConfigNumber(` (there are 4: lines ~611, 749, 790, ~900+ for maxReviewCycles). Replace each with `getCachedPipelineConfigNumber(`:

```bash
grep -n "getPipelineConfigNumber" server/pipeline/orchestrator.ts
```

Expected: 4 matches. For each match, replace `getPipelineConfigNumber(` with `getCachedPipelineConfigNumber(` — the arguments are identical.

Note: The `import { getPipelineConfigNumber }` at the top of the file stays — `getCachedPipelineConfigNumber` delegates to it internally.

- [ ] **Step 8: Run type check**

```bash
pnpm typecheck
```

Expected: no errors.

- [ ] **Step 9: Run full test suite**

```bash
pnpm test
```

Expected: all orchestrator tests pass. The TTL cache uses `Date.now()` which is not mocked in most tests, so tests that call `getPipelineConfigNumber` via the orchestrator will still hit the real function on the first call per key per test (cache is module-level; tests that clear module state are unaffected).

If any test spies on `getPipelineConfigNumber` call count: update the spy assertion — the function will now be called once per 5s window instead of per-call, meaning some tests that previously asserted N calls will now assert ≤ N calls.

- [ ] **Step 10: Commit**

```bash
git add server/pipeline/orchestrator.ts
git commit -m "perf(backend): deduplicate listRunningStageRuns per tick, TTL-cache pipeline_config reads"
```

---

## Final: Integration Smoke Test

- [ ] **Step 1: Build and start production server**

```bash
pnpm build && node dist/server/index.js &
sleep 3
```

- [ ] **Step 2: Verify security headers**

```bash
curl -s -I http://127.0.0.1:13120/ | grep -i "content-security\|x-frame\|x-content"
```

Expected: all three headers present.

- [ ] **Step 3: Verify 401 log on bad MCP token**

```bash
curl -s -o /dev/null -w "%{http_code}" \
  -H "Authorization: Bearer mcp_badbadbadbad" \
  -H "Accept: application/json, text/event-stream" \
  -X POST http://127.0.0.1:13120/api/mcp
```

Expected: `401`. Check server logs for `[mcpAuth] invalid token from`.

- [ ] **Step 4: Kill server**

```bash
kill %1
```
