# Phase 1 — Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix data correctness, reduce poll latency to ~0ms via hooks, prevent runaway agents, and make JSONL reads incremental for performance.

**Architecture:** Four targeted changes to `jsonlParser.ts` (incremental reads, compaction baseline, convergence detection, watchdog) + cost budget enforcement in the orchestrator tick + a new hooks ingestion route that triggers debounced SSE rescans.

**Tech Stack:** Node.js fs/promises (byte-range reads), Vitest, Express 5, better-sqlite3

---

## Task 1: Incremental JSONL byte-offset reads + compaction-aware token baseline

**Tracking IDs:** RT-2, CI-4

**Files:**
- Modify: `server/jsonlParser.ts`
- Modify: `server/jsonlParser.test.ts`

### Steps

- [ ] **1.1 — Export `incrementalRead` function**

  Add the following export to `server/jsonlParser.ts`, below the existing `tailRead` and `headRead` functions:

  ```typescript
  export interface IncrementalReadResult {
    raw: string
    endOffset: number
  }

  export async function incrementalRead(
    filePath: string,
    fromOffset: number,
  ): Promise<IncrementalReadResult> {
    const handle = await open(filePath, 'r')
    try {
      const fileStat = await handle.stat()
      const fileSize = fileStat.size
      if (fromOffset >= fileSize) {
        return { raw: '', endOffset: fromOffset }
      }
      const readSize = fileSize - fromOffset
      const buffer = Buffer.alloc(readSize)
      await handle.read(buffer, 0, readSize, fromOffset)
      return { raw: buffer.toString('utf-8'), endOffset: fileSize }
    }
    finally {
      await handle.close()
    }
  }
  ```

- [ ] **1.2 — Export `mergeIncrementalInfo` function**

  Add the following export to `server/jsonlParser.ts`, below `incrementalRead`:

  ```typescript
  export function mergeIncrementalInfo(
    prev: Partial<SessionData>,
    next: Partial<SessionData>,
  ): Partial<SessionData> {
    // Token usage: sum all four fields
    const tokenUsage: TokenUsageData = {
      inputTokens: (prev.tokenUsage?.inputTokens ?? 0) + (next.tokenUsage?.inputTokens ?? 0),
      outputTokens: (prev.tokenUsage?.outputTokens ?? 0) + (next.tokenUsage?.outputTokens ?? 0),
      cacheCreationTokens:
        (prev.tokenUsage?.cacheCreationTokens ?? 0) + (next.tokenUsage?.cacheCreationTokens ?? 0),
      cacheReadTokens:
        (prev.tokenUsage?.cacheReadTokens ?? 0) + (next.tokenUsage?.cacheReadTokens ?? 0),
    }

    // toolCounts: sum each key across both maps
    const toolCounts: Record<string, number> = { ...(prev.toolCounts ?? {}) }
    for (const [k, v] of Object.entries(next.toolCounts ?? {})) {
      toolCounts[k] = (toolCounts[k] ?? 0) + v
    }

    // tasks: start from prev, then upsert from next (update status if id matches, append if new)
    const taskMap = new Map<string, { id: string, subject: string, status: string }>()
    for (const t of prev.tasks ?? []) taskMap.set(t.id, t)
    for (const t of next.tasks ?? []) {
      if (taskMap.has(t.id)) {
        taskMap.set(t.id, { ...taskMap.get(t.id)!, status: t.status })
      }
      else {
        taskMap.set(t.id, t)
      }
    }

    return {
      // Scalar fields: prefer next if truthy/non-unknown, else fall back to prev
      sessionId:
        (next.sessionId && next.sessionId !== 'unknown' ? next.sessionId : undefined) ?? prev.sessionId,
      model:
        (next.model && next.model !== 'unknown' ? next.model : undefined) ?? prev.model,
      codeVersion:
        (next.codeVersion && next.codeVersion !== 'unknown' ? next.codeVersion : undefined)
        ?? prev.codeVersion,
      entrypoint:
        (next.entrypoint && next.entrypoint !== 'unknown' ? next.entrypoint : undefined)
        ?? prev.entrypoint,
      // last-wins fields: use next if non-null/non-empty, else prev
      currentAction: next.currentAction ?? prev.currentAction,
      lastTools: next.lastTools && next.lastTools.length > 0 ? next.lastTools : prev.lastTools,
      lastOutput: next.lastOutput ?? prev.lastOutput,
      lastBtw: next.lastBtw ?? prev.lastBtw,
      // Summed fields
      conversationTurns: (prev.conversationTurns ?? 0) + (next.conversationTurns ?? 0),
      tokenUsage,
      toolCounts,
      tasks: [...taskMap.values()],
    }
  }
  ```

- [ ] **1.3 — Add module-level `incrementalCache` map**

  Add the following constant to `server/jsonlParser.ts`, directly below the `sessionCache` map declaration (after line 47):

  ```typescript
  interface IncrementalCacheEntry {
    endOffset: number
    accumulated: Partial<SessionData>
  }

  const incrementalCache = new Map<string, IncrementalCacheEntry>()
  ```

- [ ] **1.4 — Wire incremental cache into `findSessionForProject`**

  In `findSessionForProject`, replace the block that starts at `const fileStat = await stat(sessionFilePath)` (currently line 347) with the following. The head-read fallback and subagent/meta logic below remain unchanged — only the read strategy for the main body changes.

  ```typescript
  const fileStat = await stat(sessionFilePath)

  // mtime+size cache: skip everything when the file hasn't changed at all
  const cached = sessionCache.get(sessionFilePath)
  if (cached && cached.mtimeMs === fileStat.mtimeMs && cached.size === fileStat.size)
    return cached.result

  let info: Partial<SessionData>
  const incr = incrementalCache.get(sessionFilePath)

  if (incr && fileStat.size > incr.endOffset) {
    // File grew since last read — read only the new bytes
    const { raw: newRaw, endOffset } = await incrementalRead(sessionFilePath, incr.endOffset)
    const newParsed = parseJsonlLines(newRaw)
    const newInfo = extractSessionInfo(newParsed)
    info = mergeIncrementalInfo(incr.accumulated, newInfo)
    incrementalCache.set(sessionFilePath, { endOffset, accumulated: info })
  }
  else if (!incr) {
    // First read for this path — full tail read, then seed incremental cache
    const raw = await tailRead(sessionFilePath)
    const parsed = parseJsonlLines(raw)
    info = extractSessionInfo(parsed)
    incrementalCache.set(sessionFilePath, { endOffset: fileStat.size, accumulated: info })
  }
  else {
    // Cache exists but size hasn't grown (file unchanged or truncated) — reuse accumulated
    info = incr.accumulated
  }
  ```

  Replace all downstream references in this function from the old separate `raw`/`parsed`/`info` variables to use `info` directly.

- [ ] **1.5 — Add compaction baseline detection to `extractSessionInfo`**

  Inside `extractSessionInfo`, add a `compactionBaseline` accumulator directly below the `tokenUsage` variable declaration:

  ```typescript
  const compactionBaseline: TokenUsageData = {
    inputTokens: 0,
    outputTokens: 0,
    cacheCreationTokens: 0,
    cacheReadTokens: 0,
  }
  ```

  Replace the token-accumulation block inside the `entry.type === 'assistant'` branch with the following (compaction detection added before the accumulation):

  ```typescript
  // Token usage from message.usage
  const usage = entry.message?.usage
  if (usage) {
    const newInput = usage.input_tokens || 0
    const prevInput = tokenUsage.inputTokens

    // Compaction detection: if input_tokens drops by >= 80% compared to the
    // running total, a context reset occurred. Save the baseline and restart.
    if (prevInput > 0 && newInput > 0 && newInput <= prevInput * 0.20) {
      compactionBaseline.inputTokens += tokenUsage.inputTokens
      compactionBaseline.outputTokens += tokenUsage.outputTokens
      compactionBaseline.cacheCreationTokens += tokenUsage.cacheCreationTokens
      compactionBaseline.cacheReadTokens += tokenUsage.cacheReadTokens
      tokenUsage.inputTokens = 0
      tokenUsage.outputTokens = 0
      tokenUsage.cacheCreationTokens = 0
      tokenUsage.cacheReadTokens = 0
    }

    tokenUsage.inputTokens += newInput
    tokenUsage.outputTokens += usage.output_tokens || 0
    tokenUsage.cacheCreationTokens += usage.cache_creation_input_tokens || 0
    tokenUsage.cacheReadTokens += usage.cache_read_input_tokens || 0
  }
  ```

  Update the `return` statement in `extractSessionInfo` to sum the baseline into the final token totals:

  ```typescript
  return {
    sessionId,
    entrypoint,
    currentAction,
    lastTools,
    tasks,
    tokenUsage: {
      inputTokens: compactionBaseline.inputTokens + tokenUsage.inputTokens,
      outputTokens: compactionBaseline.outputTokens + tokenUsage.outputTokens,
      cacheCreationTokens: compactionBaseline.cacheCreationTokens + tokenUsage.cacheCreationTokens,
      cacheReadTokens: compactionBaseline.cacheReadTokens + tokenUsage.cacheReadTokens,
    },
    model,
    codeVersion,
    conversationTurns,
    toolCounts,
    lastOutput,
    lastBtw: pendingBtwMessage
      ? { message: pendingBtwMessage, response: null }
      : lastBtw,
  }
  ```

- [ ] **1.6 — Write tests for `incrementalRead`**

  Add a new `describe('incrementalRead', ...)` block to `server/jsonlParser.test.ts`. Tests use a real temp directory via `node:fs/promises` and `node:os`.

  ```typescript
  import { mkdir, rm, writeFile } from 'node:fs/promises'
  import { tmpdir } from 'node:os'
  import { join } from 'node:path'
  import { afterAll, beforeAll, describe, expect, it } from 'vitest'
  import { incrementalRead } from './jsonlParser'

  describe('incrementalRead', () => {
    let dir: string

    beforeAll(async () => {
      dir = join(tmpdir(), `jsonl-incr-test-${Date.now()}`)
      await mkdir(dir, { recursive: true })
    })

    afterAll(async () => {
      await rm(dir, { recursive: true, force: true })
    })

    it('reads exactly the bytes after fromOffset', async () => {
      const filePath = join(dir, 'incr1.jsonl')
      const content = '{"type":"user"}\n{"type":"assistant"}\n'
      await writeFile(filePath, content, 'utf-8')
      const firstLineBytes = Buffer.byteLength('{"type":"user"}\n', 'utf-8')

      const result = await incrementalRead(filePath, firstLineBytes)

      expect(result.raw).toBe('{"type":"assistant"}\n')
      expect(result.endOffset).toBe(Buffer.byteLength(content, 'utf-8'))
    })

    it('returns empty raw and same offset when fromOffset equals file size', async () => {
      const filePath = join(dir, 'incr2.jsonl')
      const content = '{"type":"user"}\n'
      await writeFile(filePath, content, 'utf-8')
      const size = Buffer.byteLength(content, 'utf-8')

      const result = await incrementalRead(filePath, size)

      expect(result.raw).toBe('')
      expect(result.endOffset).toBe(size)
    })

    it('returns empty raw when fromOffset exceeds file size', async () => {
      const filePath = join(dir, 'incr3.jsonl')
      await writeFile(filePath, 'hello\n', 'utf-8')
      const size = Buffer.byteLength('hello\n', 'utf-8')

      const result = await incrementalRead(filePath, size + 100)

      expect(result.raw).toBe('')
      expect(result.endOffset).toBe(size + 100)
    })

    it('reads entire file when fromOffset is 0', async () => {
      const filePath = join(dir, 'incr4.jsonl')
      const content = '{"a":1}\n{"b":2}\n'
      await writeFile(filePath, content, 'utf-8')

      const result = await incrementalRead(filePath, 0)

      expect(result.raw).toBe(content)
      expect(result.endOffset).toBe(Buffer.byteLength(content, 'utf-8'))
    })
  })
  ```

- [ ] **1.7 — Write tests for `mergeIncrementalInfo`**

  Add a new `describe('mergeIncrementalInfo', ...)` block to `server/jsonlParser.test.ts`:

  ```typescript
  import { mergeIncrementalInfo } from './jsonlParser'

  describe('mergeIncrementalInfo', () => {
    it('sums token usage fields from prev and next', () => {
      const prev = {
        tokenUsage: { inputTokens: 100, outputTokens: 50, cacheCreationTokens: 10, cacheReadTokens: 5 },
      }
      const next = {
        tokenUsage: { inputTokens: 200, outputTokens: 80, cacheCreationTokens: 20, cacheReadTokens: 15 },
      }
      const result = mergeIncrementalInfo(prev, next)

      expect(result.tokenUsage).toEqual({
        inputTokens: 300,
        outputTokens: 130,
        cacheCreationTokens: 30,
        cacheReadTokens: 20,
      })
    })

    it('sums conversationTurns', () => {
      const result = mergeIncrementalInfo({ conversationTurns: 3 }, { conversationTurns: 7 })
      expect(result.conversationTurns).toBe(10)
    })

    it('merges toolCounts by summing per-key values', () => {
      const result = mergeIncrementalInfo(
        { toolCounts: { Read: 2, Write: 1 } },
        { toolCounts: { Read: 3, Bash: 4 } },
      )
      expect(result.toolCounts).toEqual({ Read: 5, Write: 1, Bash: 4 })
    })

    it('updates status of existing task and appends new tasks', () => {
      const prev = {
        tasks: [
          { id: 't1', subject: 'alpha', status: 'pending' },
          { id: 't2', subject: 'beta', status: 'pending' },
        ],
      }
      const next = {
        tasks: [
          { id: 't1', subject: 'alpha', status: 'completed' },
          { id: 't3', subject: 'gamma', status: 'pending' },
        ],
      }
      const result = mergeIncrementalInfo(prev, next)

      expect(result.tasks).toHaveLength(3)
      expect(result.tasks!.find(t => t.id === 't1')!.status).toBe('completed')
      expect(result.tasks!.find(t => t.id === 't2')!.status).toBe('pending')
      expect(result.tasks!.find(t => t.id === 't3')!.status).toBe('pending')
    })

    it('uses next.lastOutput if non-null, else falls back to prev', () => {
      expect(mergeIncrementalInfo({ lastOutput: 'old' }, { lastOutput: 'new' }).lastOutput).toBe('new')
      expect(mergeIncrementalInfo({ lastOutput: 'old' }, { lastOutput: null }).lastOutput).toBe('old')
      expect(mergeIncrementalInfo({ lastOutput: null }, { lastOutput: null }).lastOutput).toBeNull()
    })

    it('prefers non-unknown next.model over prev', () => {
      expect(
        mergeIncrementalInfo({ model: 'claude-3-5-sonnet' }, { model: null }).model,
      ).toBe('claude-3-5-sonnet')
      expect(
        mergeIncrementalInfo({ model: 'claude-3-5-sonnet' }, { model: 'claude-3-7-sonnet' }).model,
      ).toBe('claude-3-7-sonnet')
    })

    it('handles both prev and next being empty objects without throwing', () => {
      const result = mergeIncrementalInfo({}, {})
      expect(result.tokenUsage).toEqual({
        inputTokens: 0,
        outputTokens: 0,
        cacheCreationTokens: 0,
        cacheReadTokens: 0,
      })
      expect(result.conversationTurns).toBe(0)
      expect(result.tasks).toEqual([])
    })
  })
  ```

- [ ] **1.8 — Write tests for compaction baseline detection**

  Add a new `describe` block to `server/jsonlParser.test.ts`:

  ```typescript
  describe('extractSessionInfo — compaction detection', () => {
    function makeAssistantEntry(inputTokens: number, outputTokens: number) {
      return {
        type: 'assistant',
        message: {
          usage: { input_tokens: inputTokens, output_tokens: outputTokens },
          content: [],
        },
      }
    }

    it('accumulates tokens normally when no compaction occurs', () => {
      const entries = [
        makeAssistantEntry(100, 50),
        makeAssistantEntry(120, 60),
        makeAssistantEntry(140, 70),
      ]
      const result = extractSessionInfo(entries)
      expect(result.tokenUsage!.inputTokens).toBe(360)
      expect(result.tokenUsage!.outputTokens).toBe(180)
    })

    it('detects compaction when input_tokens drops by >= 80% and preserves baseline', () => {
      const entries = [
        makeAssistantEntry(500, 200),
        makeAssistantEntry(500, 200),
        // 90% drop from 1000 — compaction event
        makeAssistantEntry(100, 40),
        makeAssistantEntry(110, 45),
      ]
      const result = extractSessionInfo(entries)
      // Baseline 1000 input preserved; post-compaction 210 added
      expect(result.tokenUsage!.inputTokens).toBe(1210)
    })

    it('does not trigger compaction when drop is less than 80%', () => {
      const entries = [
        makeAssistantEntry(1000, 400),
        makeAssistantEntry(250, 100), // 75% drop — not a compaction
      ]
      const result = extractSessionInfo(entries)
      expect(result.tokenUsage!.inputTokens).toBe(1250)
    })

    it('handles multiple compaction events in sequence', () => {
      const entries = [
        makeAssistantEntry(1000, 400),
        makeAssistantEntry(50, 20),    // first compaction
        makeAssistantEntry(800, 320),
        makeAssistantEntry(40, 16),    // second compaction
        makeAssistantEntry(200, 80),
      ]
      const result = extractSessionInfo(entries)
      // Baseline = 1000 + 50 + 800 = 1850; post-compaction = 240
      expect(result.tokenUsage!.inputTokens).toBe(2090)
    })
  })
  ```

- [ ] **1.9 — Run tests and verify they pass**

  ```bash
  pnpm test server/jsonlParser.test.ts
  ```

  Expected: all new describe blocks pass. No regressions in existing `parseJsonlLines`, `encodePath`, `extractSessionInfo`, or `pickBestJsonlFile` describe blocks.

- [ ] **1.10 — Commit**

  ```
  git commit -m "feat(jsonlParser): incremental byte-offset reads + compaction-aware token baseline"
  ```

---

## Task 2: Agent-level convergence detection + watchdog error scanner

**Tracking IDs:** RT-4, HD-1

**Files:**
- Modify: `src/types.ts`
- Modify: `server/jsonlParser.ts`
- Modify: `server/agentMerger.ts`
- Modify: `server/agentMerger.test.ts`

### Steps

- [ ] **2.1 — Add new fields to `Agent` in `src/types.ts`**

  In the `Agent` interface (currently ending at `pipelineTaskTitle?`), add the following three fields:

  ```typescript
  /** True when the last 5 tool calls are identical (same tool name and input). */
  convergenceAlert: boolean
  /** The tool name that triggered convergence detection; null otherwise. */
  convergenceToolName: string | null
  /** Non-null when the agent's JSONL contains a recognisable error signature. */
  errorState: 'quota_exhausted' | 'rate_limited' | 'auth_failed' | null
  ```

- [ ] **2.2 — Add new fields to `SessionData` in `server/jsonlParser.ts`**

  In the `SessionData` interface (lines 11-28), add the following fields at the end:

  ```typescript
  convergenceAlert: boolean
  convergenceToolName: string | null
  errorState: 'quota_exhausted' | 'rate_limited' | 'auth_failed' | null
  ```

- [ ] **2.3 — Implement convergence detection in `extractSessionInfo`**

  Add the following local state inside `extractSessionInfo`, below the `toolCounts` declaration:

  ```typescript
  const recentToolCalls: Array<{ name: string, inputHash: string }> = []
  let convergenceAlert = false
  let convergenceToolName: string | null = null
  ```

  Inside the `block.type === 'tool_use' && block.name` branch, after the `toolCounts` increment line, add:

  ```typescript
  // Convergence detection: track last 8 tool calls; alert when last 5 are identical
  const inputHash = JSON.stringify(block.input ?? {})
  recentToolCalls.push({ name: block.name, inputHash })
  if (recentToolCalls.length > 8)
    recentToolCalls.shift()
  if (recentToolCalls.length >= 5) {
    const last5 = recentToolCalls.slice(-5)
    if (
      last5.every(c => c.name === last5[0].name)
      && last5.every(c => c.inputHash === last5[0].inputHash)
    ) {
      convergenceAlert = true
      convergenceToolName = block.name
    }
  }
  ```

- [ ] **2.4 — Implement watchdog error scanner in `extractSessionInfo`**

  Add the following local state below the `convergenceToolName` declaration:

  ```typescript
  const recentToolResults: string[] = []
  const recentAssistantTexts: string[] = []
  let errorState: SessionData['errorState'] = null
  ```

  Inside the `entry.type === 'user'` branch, inside the content-block loop for `tool_result` entries, add:

  ```typescript
  if (block.type === 'tool_result' && block.content) {
    const text = typeof block.content === 'string'
      ? block.content
      : JSON.stringify(block.content)
    recentToolResults.push(text)
    if (recentToolResults.length > 20)
      recentToolResults.shift()
  }
  ```

  Inside the `entry.type === 'assistant'` branch, in the `block.type === 'text'` clause where `lastOutput` is set, also add:

  ```typescript
  recentAssistantTexts.push(text)
  if (recentAssistantTexts.length > 10)
    recentAssistantTexts.shift()
  ```

  After the `entries` loop, before the `return` statement, add error classification:

  ```typescript
  const QUOTA_RE = /quota exceeded|usage limit|monthly limit/i
  const RATE_RE = /rate limit|429|too many requests|throttl/i
  const AUTH_RE = /invalid api key|authentication|unauthorized|401/i

  for (const text of [...recentToolResults, ...recentAssistantTexts]) {
    if (QUOTA_RE.test(text)) { errorState = 'quota_exhausted'; break }
    if (RATE_RE.test(text)) { errorState = 'rate_limited'; break }
    if (AUTH_RE.test(text)) { errorState = 'auth_failed'; break }
  }
  ```

- [ ] **2.5 — Include new fields in `extractSessionInfo` return value**

  Add the three new fields to the `return` statement of `extractSessionInfo`:

  ```typescript
  convergenceAlert,
  convergenceToolName,
  errorState,
  ```

- [ ] **2.6 — Pass new fields through `findSessionForProject`**

  In the `result: SessionData` object literal inside `findSessionForProject`, add:

  ```typescript
  convergenceAlert: info.convergenceAlert ?? false,
  convergenceToolName: info.convergenceToolName ?? null,
  errorState: info.errorState ?? null,
  ```

- [ ] **2.7 — Pass new fields through `agentMerger.ts`**

  In `getAgents`, inside the `processes.map(...)` return literal, add the three fields after `lastBtw`:

  ```typescript
  convergenceAlert: session?.convergenceAlert ?? false,
  convergenceToolName: session?.convergenceToolName ?? null,
  errorState: session?.errorState ?? null,
  ```

- [ ] **2.8 — Add tests to `server/jsonlParser.test.ts`**

  Add the following describe blocks:

  ```typescript
  describe('extractSessionInfo — watchdog error scanner', () => {
    function makeToolResultEntry(content: string) {
      return {
        type: 'user',
        message: {
          role: 'user',
          content: [{ type: 'tool_result', tool_use_id: 'x', content }],
        },
      }
    }

    it('detects quota_exhausted from tool result content', () => {
      const entries = [makeToolResultEntry('Error: usage limit exceeded for this month')]
      const result = extractSessionInfo(entries)
      expect(result.errorState).toBe('quota_exhausted')
    })

    it('detects rate_limited from tool result content', () => {
      const entries = [makeToolResultEntry('HTTP 429: too many requests — try again later')]
      const result = extractSessionInfo(entries)
      expect(result.errorState).toBe('rate_limited')
    })

    it('detects auth_failed from tool result content', () => {
      const entries = [makeToolResultEntry('Error: invalid API key provided')]
      const result = extractSessionInfo(entries)
      expect(result.errorState).toBe('auth_failed')
    })

    it('returns null errorState when no error patterns match', () => {
      const entries = [makeToolResultEntry('Successfully wrote 42 lines to the file')]
      const result = extractSessionInfo(entries)
      expect(result.errorState).toBeNull()
    })

    it('detects convergence when last 5 tool calls share the same name and input hash', () => {
      const repeatedEntry = {
        type: 'assistant',
        message: {
          usage: { input_tokens: 100, output_tokens: 40 },
          content: [
            { type: 'tool_use', name: 'Bash', input: { command: 'ls /tmp' } },
          ],
        },
      }
      const entries = Array.from({ length: 5 }, () => repeatedEntry)
      const result = extractSessionInfo(entries)
      expect(result.convergenceAlert).toBe(true)
      expect(result.convergenceToolName).toBe('Bash')
    })

    it('does not flag convergence when tool inputs vary across calls', () => {
      const makeEntry = (cmd: string) => ({
        type: 'assistant',
        message: {
          usage: { input_tokens: 10, output_tokens: 5 },
          content: [{ type: 'tool_use', name: 'Bash', input: { command: cmd } }],
        },
      })
      const entries = ['ls', 'pwd', 'cat file.txt', 'echo hello', 'ls -la'].map(makeEntry)
      const result = extractSessionInfo(entries)
      expect(result.convergenceAlert).toBe(false)
      expect(result.convergenceToolName).toBeNull()
    })
  })
  ```

  Also add a shape test in `server/agentMerger.test.ts`:

  ```typescript
  describe('getAgents — new Agent fields present', () => {
    it('Agent interface has convergenceAlert, convergenceToolName, and errorState fields', () => {
      // Compile-time type check that the interface shape is correct.
      // We verify the type contract by constructing a minimal conforming object.
      const agent: Pick<import('../src/types').Agent, 'convergenceAlert' | 'convergenceToolName' | 'errorState'> = {
        convergenceAlert: false,
        convergenceToolName: null,
        errorState: null,
      }
      expect(agent.convergenceAlert).toBe(false)
      expect(agent.convergenceToolName).toBeNull()
      expect(agent.errorState).toBeNull()
    })
  })
  ```

- [ ] **2.9 — Run tests and verify they pass**

  ```bash
  pnpm test server/jsonlParser.test.ts server/agentMerger.test.ts
  ```

  Expected: all new describe blocks pass. Zero regressions.

- [ ] **2.10 — Run typecheck**

  ```bash
  pnpm typecheck
  ```

  Expected: no errors.

- [ ] **2.11 — Commit**

  ```
  git commit -m "feat(agents): convergence detection + watchdog error scanner on Agent"
  ```

---

## Task 3: Cost budget enforcement in orchestrator

**Tracking ID:** CI-2

**Files:**
- Modify: `server/db/stageRunsRepo.ts`
- Modify: `server/pipeline/orchestrator.ts`
- Create: `server/pipeline/orchestrator.costBudget.test.ts`

### Steps

- [ ] **3.1 — Add `sumCompletedCostCents` to `stageRunsRepo.ts`**

  Add the following export at the end of `server/db/stageRunsRepo.ts`:

  ```typescript
  /**
   * Sum cost_cents across all stage_runs that reached `done` status for a task.
   * Used by the orchestrator's budget-enforcement branch in finalizeCompletedAsyncRuns.
   */
  export function sumCompletedCostCents(taskId: string, db: Database = getDb()): number {
    const row = db
      .prepare(
        `SELECT COALESCE(SUM(cost_cents), 0) AS total
         FROM stage_runs
         WHERE task_id = ? AND status = 'done'`,
      )
      .get(taskId) as { total: number }
    return row.total
  }
  ```

- [ ] **3.2 — Import `sumCompletedCostCents` in `orchestrator.ts`**

  In `server/pipeline/orchestrator.ts`, add `sumCompletedCostCents` to the named imports from `'../db/stageRunsRepo.js'`.

- [ ] **3.3 — Add budget check in `finalizeCompletedAsyncRuns`**

  In `server/pipeline/orchestrator.ts`, inside the `result.kind === 'still_running'` branch, locate the existing guard:

  ```typescript
  const hasPendingPerms = listPendingPermissionRequests(run.id).length > 0
  if (hasPendingPerms)
    continue
  ```

  Add the following block immediately after (before the stage timeout check):

  ```typescript
  // Cost budget enforcement: kill and fail the current stage run if the
  // task has already spent more than its configured cost ceiling.
  if (task.costBudgetCents != null && task.costBudgetCents > 0) {
    const completedCents = sumCompletedCostCents(task.id)
    if (completedCents > task.costBudgetCents) {
      consola.warn(
        `[orchestrator] task ${task.id} exceeded cost budget`
        + ` (${completedCents}¢ > ${task.costBudgetCents}¢) — killing PID ${run.pid}`,
      )
      try {
        process.kill(run.pid!, 'SIGTERM')
      }
      catch { /* race with natural process exit */ }
      const fresh2 = getStageRunById(run.id)
      if (fresh2 && fresh2.status === 'running') {
        this.applyTransition(task, fresh2, {
          kind: 'fail',
          error: `cost budget exceeded: ${completedCents}¢ spent, limit ${task.costBudgetCents}¢`,
        })
      }
      continue
    }
  }
  ```

- [ ] **3.4 — Create `server/pipeline/orchestrator.costBudget.test.ts`**

  ```typescript
  import type { PipelineTask, StageRun } from '../../src/types.js'
  import { afterEach, beforeEach, describe, expect, it } from 'vitest'
  import Database from 'better-sqlite3'
  import { sumCompletedCostCents } from '../db/stageRunsRepo.js'

  // ─── Helpers (same pattern as agentSpawner.test.ts) ──────────────────────────

  function makeTask(overrides: Partial<PipelineTask> = {}): PipelineTask {
    return {
      id: 't-budget-1',
      slug: 'budget-test',
      title: 'Budget test task',
      description: null,
      cwd: '/tmp/project',
      worktreePath: null,
      sourceBranch: null,
      targetBranch: null,
      currentStage: 'implementation',
      parentTaskId: null,
      maxIterations: 20,
      tokenBudget: null,
      costBudgetCents: 500,
      stageTimeoutSeconds: 1800,
      createdAt: '2026-05-09T10:00:00Z',
      updatedAt: '2026-05-09T10:00:00Z',
      metadata: null,
      silverBullet: false,
      priority: 'medium',
      userId: null,
      ...overrides,
    }
  }

  // ─── sumCompletedCostCents ────────────────────────────────────────────────────

  describe('sumCompletedCostCents', () => {
    let db: ReturnType<typeof Database>

    beforeEach(() => {
      db = new Database(':memory:')
      db.exec(`
        CREATE TABLE stage_runs (
          id       TEXT PRIMARY KEY,
          task_id  TEXT NOT NULL,
          stage    TEXT NOT NULL,
          status   TEXT NOT NULL,
          cost_cents INTEGER NOT NULL DEFAULT 0
        )
      `)
    })

    afterEach(() => {
      db.close()
    })

    it('returns 0 when there are no done stage_runs for the task', () => {
      expect(sumCompletedCostCents('t-budget-1', db as any)).toBe(0)
    })

    it('sums cost_cents of done stage_runs only', () => {
      db.prepare('INSERT INTO stage_runs VALUES (?,?,?,?,?)').run('sr-1', 't-budget-1', 'concept', 'done', 120)
      db.prepare('INSERT INTO stage_runs VALUES (?,?,?,?,?)').run('sr-2', 't-budget-1', 'implementation', 'done', 380)
      db.prepare('INSERT INTO stage_runs VALUES (?,?,?,?,?)').run('sr-3', 't-budget-1', 'self_review', 'running', 200)

      expect(sumCompletedCostCents('t-budget-1', db as any)).toBe(500)
    })

    it('excludes failed and running stage_runs from the sum', () => {
      db.prepare('INSERT INTO stage_runs VALUES (?,?,?,?,?)').run('sr-1', 't-budget-1', 'implementation', 'failed', 999)
      db.prepare('INSERT INTO stage_runs VALUES (?,?,?,?,?)').run('sr-2', 't-budget-1', 'implementation', 'running', 999)

      expect(sumCompletedCostCents('t-budget-1', db as any)).toBe(0)
    })

    it('excludes stage_runs belonging to a different task', () => {
      db.prepare('INSERT INTO stage_runs VALUES (?,?,?,?,?)').run('sr-other', 't-OTHER', 'concept', 'done', 9999)
      db.prepare('INSERT INTO stage_runs VALUES (?,?,?,?,?)').run('sr-mine', 't-budget-1', 'concept', 'done', 100)

      expect(sumCompletedCostCents('t-budget-1', db as any)).toBe(100)
    })
  })

  // ─── Budget guard condition logic ─────────────────────────────────────────────

  describe('orchestrator budget guard condition', () => {
    function shouldEnforceBudget(task: PipelineTask, spentCents: number): boolean {
      return (
        task.costBudgetCents != null
        && task.costBudgetCents > 0
        && spentCents > task.costBudgetCents
      )
    }

    it('triggers enforcement when spent exceeds budget by 1 cent', () => {
      expect(shouldEnforceBudget(makeTask({ costBudgetCents: 500 }), 501)).toBe(true)
    })

    it('does not trigger enforcement when spent equals the budget exactly', () => {
      expect(shouldEnforceBudget(makeTask({ costBudgetCents: 500 }), 500)).toBe(false)
    })

    it('does not trigger enforcement when spent is below the budget', () => {
      expect(shouldEnforceBudget(makeTask({ costBudgetCents: 500 }), 100)).toBe(false)
    })

    it('does not trigger enforcement when costBudgetCents is null', () => {
      expect(shouldEnforceBudget(makeTask({ costBudgetCents: null }), 99999)).toBe(false)
    })

    it('does not trigger enforcement when costBudgetCents is 0 (sentinel for disabled)', () => {
      expect(shouldEnforceBudget(makeTask({ costBudgetCents: 0 }), 99999)).toBe(false)
    })
  })
  ```

- [ ] **3.5 — Run tests and verify they pass**

  ```bash
  pnpm test server/pipeline/orchestrator.costBudget.test.ts
  ```

  Expected: 9 tests pass across both describe blocks. Zero failures.

- [ ] **3.6 — Run typecheck**

  ```bash
  pnpm typecheck
  ```

  Expected: no errors.

- [ ] **3.7 — Commit**

  ```
  git commit -m "feat(orchestrator): enforce costBudgetCents — SIGTERM and fail stage when exceeded"
  ```

---

## Task 4: Claude Code hooks ingestion endpoint

**Tracking ID:** RT-1

**Files:**
- Create: `scripts/hooks/notify.js`
- Create: `server/routes/hooksRoutes.ts`
- Modify: `server/index.ts`
- Create: `docs/hooks-setup.md`

### Steps

- [ ] **4.1 — Create `scripts/hooks/notify.js`**

  Create the directory and file (CommonJS, zero third-party deps — uses built-in `fetch` available in Node 18+):

  ```javascript
  #!/usr/bin/env node
  // Claude Code lifecycle hook — fire-and-forget POST to the dashboard.
  // Install by adding to ~/.claude/settings.json hooks (see docs/hooks-setup.md).
  //
  // Env vars (all optional):
  //   DASHBOARD_HOOKS_URL    — target URL, default http://127.0.0.1:13120/api/hooks/event
  //   DASHBOARD_HOOKS_SECRET — shared secret; must match DASHBOARD_HOOKS_SECRET on server
  //   CLAUDE_HOOK_TYPE       — set automatically by Claude Code for each hook event

  const DASHBOARD_HOOKS_URL
    = process.env.DASHBOARD_HOOKS_URL
    || 'http://127.0.0.1:13120/api/hooks/event'
  const DASHBOARD_HOOKS_SECRET = process.env.DASHBOARD_HOOKS_SECRET || ''

  const chunks = []
  process.stdin.on('data', c => chunks.push(c))
  process.stdin.on('end', async () => {
    let body = {}
    try {
      body = JSON.parse(Buffer.concat(chunks).toString('utf-8'))
    }
    catch {
      // stdin may be empty for some hook types — not an error
    }

    const headers = { 'Content-Type': 'application/json' }
    if (DASHBOARD_HOOKS_SECRET) {
      headers['Authorization'] = `Bearer ${DASHBOARD_HOOKS_SECRET}`
    }

    try {
      await Promise.race([
        fetch(DASHBOARD_HOOKS_URL, {
          method: 'POST',
          headers,
          body: JSON.stringify({
            hookType: process.env.CLAUDE_HOOK_TYPE || 'unknown',
            ...body,
          }),
        }),
        new Promise((_, rej) => setTimeout(() => rej(new Error('timeout')), 500)),
      ])
    }
    catch {
      // Hooks must never block the Claude Code session — swallow all errors
    }

    process.exit(0)
  })
  ```

  Make the script executable:

  ```bash
  chmod +x scripts/hooks/notify.js
  ```

- [ ] **4.2 — Create `server/routes/hooksRoutes.ts`**

  ```typescript
  import type { Router } from 'express'
  import express from 'express'

  export interface HooksRouterOptions {
    /** Callback invoked for every authenticated incoming hook event. */
    onEvent: () => void
    /**
     * Shared secret. When non-empty, incoming requests must supply
     * `Authorization: Bearer <secret>`. When empty, all POSTs from localhost
     * are accepted.
     */
    secret: string
  }

  export function createHooksRouter(opts: HooksRouterOptions): Router {
    const router = express.Router()

    router.post('/event', (req, res) => {
      if (opts.secret) {
        const auth = req.headers.authorization ?? ''
        if (auth !== `Bearer ${opts.secret}`) {
          res.status(401).end()
          return
        }
      }

      // Acknowledge immediately — the hook script has a 500ms timeout
      res.status(204).end()

      // Trigger debounced SSE rescan without blocking the response
      opts.onEvent()
    })

    return router
  }
  ```

- [ ] **4.3 — Extract `broadcastAgents` helper in `server/index.ts`**

  The SSE fan-out logic inside `startSSEBroadcast`'s setInterval callback currently fetches agents, builds the cost trend, and fans out per-user payloads all inline. Extract it into a `broadcastAgents(localAgents)` helper so both the interval and the hooks debounce call the same path.

  Add the following helper function inside `start()`, just before `function startSSEBroadcast()`:

  ```typescript
  async function broadcastAgents(
    localAgents: Awaited<ReturnType<typeof getAgents>>,
  ): Promise<void> {
    const envRemotes = getEnvRemoteTargets()

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
    const userPayloadCache = new Map<string, string>()

    await Promise.all(
      [...sseClients].map(async (client) => {
        try {
          if (client.res.writableEnded)
            return
          if (!userPayloadCache.has(client.userId)) {
            const userRemotes = isAuthEnabled()
              ? listRemoteRegistrationsForUser(client.userId).map(r => ({
                  url: r.url,
                  bearerKey: r.bearerKey,
                  name: r.name,
                }))
              : []
            const allRemotes = [...envRemotes, ...userRemotes]
            const agents = await aggregateAgents(localAgents, allRemotes)
            userPayloadCache.set(client.userId, JSON.stringify({ agents, trend: trendSlice }))
          }
          const payload = userPayloadCache.get(client.userId)!
          client.res.write(`data: ${payload}\n\n`)
        }
        catch {
          sseClients.delete(client)
        }
      }),
    )
  }
  ```

  Simplify the `setInterval` body inside `startSSEBroadcast` to delegate to `broadcastAgents`:

  ```typescript
  sseBroadcastId = setInterval(async () => {
    try {
      const localAgents = await getAgents()
      await broadcastAgents(localAgents)
    }
    catch (err) {
      console.error('SSE broadcast error:', err)
    }
  }, SSE_INTERVAL_MS)
  ```

- [ ] **4.4 — Register hooks route and debounce in `server/index.ts`**

  Add the import at the top of `server/index.ts` with the other route imports:

  ```typescript
  import { createHooksRouter } from './routes/hooksRoutes.js'
  ```

  Inside `start()`, read the two new env vars near the other env-var reads at the top of the function:

  ```typescript
  const HOOKS_SECRET = process.env.DASHBOARD_HOOKS_SECRET ?? ''
  const HOOKS_DEBOUNCE_MS = (() => {
    const val = Number(process.env.DASHBOARD_HOOKS_DEBOUNCE_MS ?? 100)
    return Number.isFinite(val) && val >= 0 ? val : 100
  })()
  ```

  Add the debounce handle and scheduler inside `start()`, after the `sseClients` set and before `startSSEBroadcast`:

  ```typescript
  let hooksDebounceId: ReturnType<typeof setTimeout> | null = null

  function scheduleHooksRescan(): void {
    if (hooksDebounceId)
      clearTimeout(hooksDebounceId)
    hooksDebounceId = setTimeout(async () => {
      hooksDebounceId = null
      if (sseClients.size === 0)
        return
      try {
        const localAgents = await getAgents()
        await broadcastAgents(localAgents)
      }
      catch (err) {
        console.warn('[hooks] rescan failed:', err)
      }
    }, HOOKS_DEBOUNCE_MS)
  }
  ```

  Register the hooks router BEFORE the `app.use('/api', requireAuth)` line (currently line 101). Insert the following two lines immediately before it:

  ```typescript
  // Hooks endpoint is exempt from session auth — protected by shared secret only.
  app.use('/api/hooks', createHooksRouter({ onEvent: scheduleHooksRescan, secret: HOOKS_SECRET }))
  ```

- [ ] **4.5 — Add env var documentation to `.agent-context/conventions.md`**

  In the Pipeline Env Vars table, add two rows:

  ```
  | `DASHBOARD_HOOKS_SECRET`      | Shared secret for `/api/hooks/event`; recommended when hooks script runs outside localhost |
  | `DASHBOARD_HOOKS_DEBOUNCE_MS` | Debounce window before SSE rescan after a hook event, default 100ms                       |
  ```

- [ ] **4.6 — Create `docs/hooks-setup.md`**

  ```markdown
  # Hooks Setup

  Claude Code lifecycle hooks let the dashboard receive push notifications on every
  tool call rather than waiting for the next SSE poll interval (default 3 s).

  ## How it works

  `scripts/hooks/notify.js` is a fire-and-forget Node.js script. Claude Code runs it
  after each tool call. The script reads the event payload from stdin and sends it
  to the dashboard. The dashboard acknowledges immediately and triggers a debounced
  SSE rescan within `DASHBOARD_HOOKS_DEBOUNCE_MS` milliseconds (default 100 ms).

  ## Installation

  ### 1. Add the hook to `~/.claude/settings.json`

  Open `~/.claude/settings.json` (create it if it does not exist) and add:

  ```json
  {
    "hooks": {
      "PostToolUse": [
        {
          "matcher": "",
          "hooks": [
            {
              "type": "command",
              "command": "node /absolute/path/to/agent-dashboard/scripts/hooks/notify.js"
            }
          ]
        }
      ]
    }
  }
  ```

  Replace `/absolute/path/to/agent-dashboard` with the actual path on your machine.

  ### 2. Set environment variables

  In `~/.zshrc` (or wherever you configure your shell environment):

  ```bash
  # Required on both the hook script side and the server side — must match
  export DASHBOARD_HOOKS_SECRET=your-random-secret

  # Optional — defaults shown
  export DASHBOARD_HOOKS_URL=http://127.0.0.1:13120/api/hooks/event
  ```

  The dashboard server also needs `DASHBOARD_HOOKS_SECRET` set (add it to the
  terminal session where you run `pnpm dev`, or add it to a `.env.local` file if
  your start script loads one).

  ### 3. Restart the dashboard server

  ```bash
  pnpm dev
  ```

  ## Verification

  After setup, open a Claude Code session and trigger any tool use. The dashboard
  cards should update within ~100 ms rather than waiting 3 s.

  To manually test the endpoint (no secret configured):

  ```bash
  curl -s -o /dev/null -w "%{http_code}" -X POST \
    http://127.0.0.1:13120/api/hooks/event \
    -H "Content-Type: application/json" \
    -d '{"hookType":"PostToolUse","tool":"Read"}'
  # Expected: 204
  ```

  With a secret:

  ```bash
  curl -s -o /dev/null -w "%{http_code}" -X POST \
    http://127.0.0.1:13120/api/hooks/event \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer your-random-secret" \
    -d '{"hookType":"PostToolUse","tool":"Read"}'
  # Expected: 204

  # Without secret header — should be rejected
  curl -s -o /dev/null -w "%{http_code}" -X POST \
    http://127.0.0.1:13120/api/hooks/event \
    -H "Content-Type: application/json" \
    -d '{"hookType":"PostToolUse"}'
  # Expected: 401
  ```

  ## Tuning

  | Env var                       | Default | Effect                                                                    |
  | ----------------------------- | ------- | ------------------------------------------------------------------------- |
  | `DASHBOARD_HOOKS_DEBOUNCE_MS` | `100`   | Batches rapid hook events; lower = fresher data, higher = less CPU load   |
  | `DASHBOARD_HOOKS_SECRET`      | (none)  | Shared bearer token; highly recommended for any multi-user deployment     |

  ## Security note

  `/api/hooks/event` is exempt from the dashboard's session-cookie authentication
  so that `notify.js` can POST without a login cookie. It is protected only by the
  `DASHBOARD_HOOKS_SECRET` bearer token. Because the server always binds to
  `127.0.0.1` by default, external hosts cannot reach this endpoint without
  explicit SSH tunnelling or a VPN — the same constraint that applies to the rest
  of the dashboard.
  ```

- [ ] **4.7 — Run typecheck**

  ```bash
  pnpm typecheck
  ```

  Expected: no errors.

- [ ] **4.8 — Smoke-test hooks route manually**

  Start the dev server:

  ```bash
  pnpm dev
  ```

  In a second terminal, verify a 204 response (no secret configured):

  ```bash
  curl -s -o /dev/null -w "%{http_code}" -X POST \
    http://127.0.0.1:13120/api/hooks/event \
    -H "Content-Type: application/json" \
    -d '{"hookType":"PostToolUse","tool":"Bash"}'
  # Expected: 204
  ```

  Restart with `DASHBOARD_HOOKS_SECRET=test-secret pnpm dev` and verify:

  ```bash
  # No header — expect 401
  curl -s -o /dev/null -w "%{http_code}" -X POST \
    http://127.0.0.1:13120/api/hooks/event \
    -H "Content-Type: application/json" \
    -d '{}'

  # Correct header — expect 204
  curl -s -o /dev/null -w "%{http_code}" -X POST \
    http://127.0.0.1:13120/api/hooks/event \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer test-secret" \
    -d '{"hookType":"PostToolUse"}'
  ```

- [ ] **4.9 — Commit**

  ```
  git commit -m "feat(hooks): add /api/hooks/event endpoint + notify.js script + debounced SSE rescan"
  ```

---

## Completion Checklist

After all four tasks are complete, run the full suite:

```bash
pnpm test && pnpm typecheck && pnpm lint
```

Expected: all tests pass, zero type errors, zero lint errors.

If any cross-task polish was needed:

```
git commit -m "chore(phase1): final integration polish — lint + typecheck clean"
```
