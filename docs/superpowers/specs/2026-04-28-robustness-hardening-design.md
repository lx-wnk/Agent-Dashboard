# Robustness Hardening — Refinement, Pipeline, Permissions

**Date:** 2026-04-28
**Branch:** `feat/robustness-hardening`
**Scope:** Three subsystems — Refinement Chat (A), Pipeline State Machine (B), Permission System (C)

---

## Background

A systematic review of the `feat/agent-based-ticket-refinement` branch surfaced 7 concrete issues across three layers:

- **Refinement Chat** — no process timeout, stderr context lost, unbounded history in prompt
- **Pipeline State Machine** — selbstreview↔umsetzung cycle cap missing, wrong PID fallback on spawn failure
- **Permission System** — first-vs-last JSON-block inconsistency, no preset reset, settings.json leaks into repo

None of these are blocking today but all represent correctness or cost risks in production use.

---

## A — Refinement Chat

### A1 · Process timeout + connection-close guard

**Problem:** `spawnRefinementTurn` returns a child process with no timeout. If Claude hangs (rate-limit, pipe buffer, tool loop), `activeTurns.has(taskId)` stays true forever — all subsequent turns for that task return 409.

**Design (Option C — dual guard):**

In `server/routes/refineRoutes.ts`, wrap the turn handler with two guards:

1. **Connection-close guard** — `res.on('close', () => child.kill('SIGTERM'))` kills the process if the browser tab closes mid-stream. Clears `activeTurns` in the same handler.
2. **Hard timeout** — `setTimeout(() => child.kill('SIGTERM'), REFINEMENT_TIMEOUT_MS)` as an absolute backstop. Default 5 minutes (300 000 ms). Configurable via `REFINEMENT_TIMEOUT_MS` env var. Timer is cleared in `finally`.

Both paths must also call `activeTurns.delete(task.id)` and write a partial assistant turn to the DB (so history doesn't have a dangling user message with no response).

The child reference needs to be lifted out of `spawnRefinementTurn`'s return value so the route can call `.kill()`. Extend `SpawnRefinementResult` to include `child: ChildProcess`.

```ts
// Extended return type
export interface SpawnRefinementResult {
  child: ChildProcess
  stdout: Readable
  waitForExit: () => Promise<void>
}
```

**No UI change required.**

---

### A2 · stderr captured and surfaced in error SSE event

**Problem:** `child.stderr` is drained silently. Claude CLI errors (bad API key, model not found, rate limit) write to stderr — the user sees an opaque "spawn exited code=1" message.

**Design (Option A — first 500 chars in error event):**

In `spawnRefinementTurn`, replace the drain-only stderr handler with a ring-buffer accumulator capped at 500 chars:

```ts
let stderrSnippet = ''
child.stderr?.on('data', (chunk: Buffer) => {
  if (stderrSnippet.length < 500)
    stderrSnippet += chunk.toString().slice(0, 500 - stderrSnippet.length)
})
```

Expose `stderrSnippet` via a `getStderr: () => string` accessor added to the same `SpawnRefinementResult` extension defined in A1. In the route's error event:

```ts
res.write(`event: error\ndata: ${JSON.stringify({ error: msg, stderr: stderrSnippet || undefined })}\n\n`)
```

The frontend composable `useRefinementChat` already handles the `error` event — it can optionally append the `stderr` string to the error message displayed to the user.

---

### A3 · Phase-based history windowing

**Problem:** `serializeHistory` concatenates all turns verbatim. A long multi-phase conversation can push the prompt past context limits. There's no token budget guard.

**Design (Option C — phase-aware windowing):**

In `server/services/refinementSpawner.ts`, replace the current `serializeHistory` with `buildWindowedHistory(turns, maxChars = 40_000)`:

Algorithm:
1. Identify **phase boundary turns** — any assistant turn whose content contains `__phase_done:<phase>`. Always include these (they anchor the agent's phase state).
2. From the remaining turns, keep the **last 2 turns per phase** (user + assistant pairs, walking backwards).
3. If the serialized result still exceeds `maxChars`, truncate earlier turns (keep the phase-done markers and the last user/assistant exchange unconditionally).

This preserves the phase progression signals while bounding prompt size. The `__phase_done` markers are lightweight (~20 chars each) so they're never dropped.

The function signature stays the same (`serializeHistory(turns: RefinementTurn[]): string`) — callers don't change.

---

## B — Pipeline State Machine

### B1 · Cross-stage selbstreview↔umsetzung cycle cap

**Problem:** `maxIterations` applies per-stage within a single stage_run iteration chain. But the selbstreview→umsetzung→selbstreview cycle creates a *new* stage_run for each umsetzung with `iteration=0`, bypassing the cap entirely. A task can spawn infinite Claude processes across this cycle.

**Design (Option A — `review_cycles` in task.metadata):**

In `orchestrator.decideCompletedTransition` for the selbstreview `passed: false` path:

1. Read `(task.metadata?.review_cycles ?? 0) + 1` to get `nextCycles`.
2. Compute `maxCycles` from `task.metadata?.maxReviewCycles ?? 3` (task-level override) or a pipeline config key `maxReviewCycles` (global default: 3).
3. If `nextCycles >= maxCycles`, return `{ kind: 'wait_user', reason: 'review cycle limit reached...' }` — do NOT loop back to umsetzung.
4. Otherwise, add `review_cycles: nextCycles` to `taskMetadataPatch` alongside the existing `review_feedback`. Both land in the same SQLite transaction.

On the `passed: true` path, clear `review_cycles` alongside `review_feedback` in the metadata patch.

**No schema migration required** — metadata is a JSON blob column.

```ts
// In decideCompletedTransition, selbstreview branch:
const cycles = (task.metadata?.review_cycles as number ?? 0) + 1
const maxCycles = (task.metadata?.maxReviewCycles as number)
  ?? getPipelineConfigNumber('maxReviewCycles', 3)

if (!passed) {
  if (cycles >= maxCycles) {
    return { kind: 'wait_user', reason: `review cycle limit (${maxCycles}) reached` }
  }
  const nextMeta = { ...(task.metadata ?? {}), review_feedback: feedback, review_cycles: cycles }
  return { kind: 'next', toStage: 'umsetzung', output, taskMetadataPatch: nextMeta }
}
```

---

### B2 · PID=0 fallback + spawn-error handler

**Problem:** If `spawn('claude', ...)` fails (command not found, EPERM), `child.pid` is `undefined`. `pid ?? 0` assigns 0 to the stage_run. `kill(0, 0)` on Linux/macOS sends to the process group — typically succeeds — so `isPidAlive(0)` returns `true` and the run appears stuck forever.

**Design (Option C — two-layer fix):**

**Layer 1 — `isPidAlive`** in `server/pipeline/sessionManager.ts`:
```ts
if (!pid || pid === 0) return false
```
Makes `isPidAlive(0)` return false unconditionally regardless of kernel behavior.

**Layer 2 — `spawnStageAgent`** in `server/pipeline/agentSpawner.ts`:
After `spawn(...)`, register an `error` handler on the child. The handler fires asynchronously when the binary doesn't exist or EPERM:
```ts
child.on('error', (err) => {
  // pid will be 0 or undefined — the run will naturally fail via
  // the isPidAlive(0) fix, but we also push an explicit audit entry.
  consola.error(`[agentSpawner] spawn failed: ${err.message}`)
})
```

The run then gets detected as "PID not alive" on the next tick (isPidAlive returns false), reads no session JSONL, and fails with "no session JSONL found" — visible in the UI.

Optionally: expose a `spawnError` promise on `SpawnResult` that the caller can await to detect synchronous spawn failures before writing the stage_run, allowing an immediate `fail` transition. This is a stretch goal — the two-layer fix handles the correctness problem without it.

---

## C — Permission System & Consistency

### C1 · Last JSON block in `sessionOutputReader`

**Problem:** `extractJsonBlock` in `server/pipeline/sessionOutputReader.ts` uses a non-global regex and `.match()` — returns the *first* `\`\`\`json` block. `refineRoutes.ts` already uses `.matchAll().at(-1)` for the *last* block. If an agent writes an example JSON before its final output, the orchestrator parses the wrong block.

**Design (Option A — one-liner fix):**

Change `extractJsonBlock` to use `matchAll` and take the last match:

```ts
const JSON_BLOCK_RE_G = /```json\b([\s\S]*?)```/gi

export function extractJsonBlock(text: string): Record<string, unknown> | null {
  const matches = [...text.matchAll(JSON_BLOCK_RE_G)]
  const match = matches.at(-1)
  if (!match) return null
  // ... rest unchanged
}
```

No other callers need to change — `extractJsonBlock` is only called from `readLastStageJsonOutput`.

---

### C2 · Permission preset reset UI

**Problem:** `saveGrantsToPresets` accumulates grants indefinitely. There's no way to remove a mistaken grant (e.g., `Bash(rm -rf *)` accidentally approved). Presets become permanently inherited by all future tasks in the same project.

**Design (Option B — reset-by-project in existing Settings dialog):**

**Backend:** Add `DELETE /api/settings/permission-presets` route (protected by CSRF guard) that accepts `{ cwd: string }` in the body and deletes all presets for `(req.userId, cwd)`.

**Frontend:** In `src/components/ApiKeySettings.vue` (existing settings panel), add a "Permission Presets" section:
- Show a list of distinct `(cwd, count)` pairs from `GET /api/settings/permission-presets`.
- "Reset" button per project → calls the DELETE endpoint with that cwd.
- No per-tool granularity (kept simple per design decision).

**Backend GET endpoint:** `GET /api/settings/permission-presets` — returns `[{ cwd, count }]` grouped by cwd for the authenticated user.

New repo function: `deletePresetsForProject(userId, cwd)` in `server/db/permissionPresetsRepo.ts`.

---

### C3 · `.claude/settings.json` isolation

**Problem:** `writeSettingsFile` overwrites `.claude/settings.json` in the task's cwd/worktree for every stage spawn. This permanently contaminates the repo's Claude settings and removes any user-authored permissions.

**Design (Option A-first, C-fallback):**

**Step 1 — detect `--settings` flag support:** In `agentSpawner.ts`, lazily on first spawn, run `claude --help` and check for `--settings` in stdout. Cache the result. If supported, write permissions to a temp file in `os.tmpdir()` and pass `--settings <tmpfile>` instead of writing into the repo. Clean up the temp file after the run via the `cleanup()` call on `SpawnResult`.

**Step 2 — if `--settings` not supported (current Claude CLI):** Fall back to Option C with a marker. Before writing, check if `.claude/settings.json` exists. If it does NOT exist, write the file and record that the dashboard created it via a top-level `_dashboardManaged: true` field in the JSON. After the stage run ends (in the finally block of the tick or completion detector), delete the file **only if** `_dashboardManaged` is present — user-authored files are never touched.

The `SpawnResult` type already returns `settingsPath: string | null` — use this to track what to clean up.

In `agentSpawner.ts`, expose `cleanup(): void` on `SpawnResult` so the caller (orchestrator's `finalizeCompletedAsyncRuns`) can call it after transition.

---

## Implementation Order

The fixes are largely independent. Suggested order to minimize merge conflicts:

1. **C1** (one-liner, no deps)
2. **B2** (two-liner, touches `sessionManager` + `agentSpawner`)
3. **B1** (orchestrator logic + pipeline config)
4. **A2** (refinement spawner extension)
5. **A1** (route-level timeout, touches `SpawnRefinementResult`)
6. **A3** (history windowing — most logic, isolated to spawner)
7. **C2** (new route + UI component section)
8. **C3** (spawn isolation — most moving parts, can be merged last)

---

## Files Touched

| File | Change |
|------|--------|
| `server/pipeline/sessionOutputReader.ts` | C1: last-match extractJsonBlock |
| `server/pipeline/sessionManager.ts` | B2: isPidAlive(0) → false |
| `server/pipeline/agentSpawner.ts` | B2: error handler; C3: --settings or managed-file marker |
| `server/pipeline/orchestrator.ts` | B1: review_cycles cap in decideCompletedTransition |
| `server/db/notificationConfigRepo.ts` | B1: `maxReviewCycles` pipeline config key |
| `server/services/refinementSpawner.ts` | A2: stderr ring-buffer; A3: phase windowing; A1: expose child |
| `server/routes/refineRoutes.ts` | A1: timeout + connection-close guard |
| `server/db/permissionPresetsRepo.ts` | C2: deletePresetsForProject |
| `server/routes/taskRoutes.ts` (or new file) | C2: GET + DELETE preset endpoints |
| `src/components/ApiKeySettings.vue` | C2: preset reset UI section |

---

## Out of Scope

- Multi-tenant preset isolation (userId=null global presets are a separate decision)
- Refinement history persistence across server restarts (full SSE reconnection story)
- Full audit trail for preset deletions (basic append-audit call is sufficient)
