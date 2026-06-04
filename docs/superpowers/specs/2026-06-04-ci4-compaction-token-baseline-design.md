# Compaction-Aware Token Baseline (CI-4)

**Date:** 2026-06-04
**Status:** Draft
**Roadmap ref:** `2026-05-09-agent-dashboard-unified-roadmap-design.md` → CI-4 (P2)

## Revision (2026-06-04, post-PR-#119 review)

The original design (Sections A–F below) partitioned token counting between a
full "baseline" scan (pre-final epochs only) and the 32 KB tail parse (final
epoch only). Final review of PR #119 found a **P1 correctness bug** in that
partition: when the FINAL (post-last-compaction) epoch itself exceeds 32 KB, the
tail window starts *after* the last `compact_boundary`, so the bytes between the
boundary and the tail-window start are counted by neither path and are lost
(reproduced: ~80 KB final epoch undercounted by ~60%, expected 2227 got 904).
This is exactly CI-4's target scenario — the spec itself notes that session files
over ~300 KB have all markers outside the tail.

**Simplified design (implemented):** Each assistant message's `usage` is
**per-message** (the tokens for that single API turn), not a cumulative running
total — compaction does not reset per-message usage, only the cumulative
context-window size. Therefore the correct lifetime token total is simply the
**sum of every assistant message's `usage` across the WHOLE file**. The full
linear scan (`scanFullFileTokenUsage`) is now the single authoritative source for
`SessionData.TokenUsage`; it no longer partitions into epochs and no longer
returns a "baseline offset". The 32 KB tail parse keeps doing only its non-token
work (most-recent tool counts, model, last-activity timestamp, error state,
conversation turns, tasks, BTW) and no longer contributes to — nor zeroes — the
token total. `compact_boundary` lines are still detected, but only to set a
`hasCompaction` diagnostic flag; they have no effect on the total.

**Strict-improvement side effect:** This also fixes long **non-compacted**
sessions over 32 KB. The old tail-only token sum undercounted them too (it only
summed the last 32 KB of messages). The whole-file sum now counts every turn, so
output values change for long non-compacted sessions — not just compacted ones.
This is strictly more correct. Caching, the cache-miss-only scan trigger, and the
"no compaction → totals unchanged for short sessions" property are all preserved.

Sections A–F below describe the original epoch-partition mechanism and are
retained for historical context; the partition / tail-final-epoch split they
describe has been replaced by the whole-file sum above.

## Problem

Claude Code sessions undergo automatic context compaction. When compaction fires, the session's JSONL log continues in the same file but the cumulative `usage` counters on subsequent `assistant` messages restart from near zero (the post-compact summary context, typically 10–25 K tokens). Because `server/internal/parser/parser.go::ParseSessionFile` sums `usage` fields across all entries visible in its 32 KB tail window, every compaction that occurred before the tail window is silently ignored. The dashboard therefore shows drastically under-counted token totals and costs for any session that has been compacted.

Measured on a real 17.5 MB session file (`cf1fdb94-2f07-4779-8c41-28c4ef3dac1c.jsonl`) with 15 compaction events: the tail-read parser returned 31 input tokens and 7 025 output tokens against a true cumulative total of 21 598 input and 1 856 151 output — a 100 % under-count for both dimensions. All 15 `compact_boundary` markers fell outside the 32 KB tail window.

## Compaction Marker Schema (Confirmed)

The Claude CLI writes a `compact_boundary` entry to the JSONL at the instant of compaction. The exact JSON shape is:

```json
{
  "type": "system",
  "subtype": "compact_boundary",
  "content": "Conversation compacted",
  "isMeta": false,
  "timestamp": "2026-05-10T19:14:13.608Z",
  "uuid": "335ae7b9-0eee-404e-9c9d-0bc52829a958",
  "parentUuid": null,
  "logicalParentUuid": "1146598b-4eb8-4168-80a3-5150518356cb",
  "isSidechain": false,
  "level": "info",
  "compactMetadata": {
    "trigger": "auto",
    "preTokens": 167918,
    "postTokens": 10536,
    "preCompactDiscoveredTools": ["TaskCreate", "TaskUpdate"],
    "durationMs": 101755
  },
  "userType": "external",
  "entrypoint": "cli",
  "cwd": "/path/to/project",
  "sessionId": "cf1fdb94-...",
  "version": "2.1.138",
  "gitBranch": "feat/go-rework",
  "slug": "golden-squishing-porcupine"
}
```

Key fields:

- `type: "system"`, `subtype: "compact_boundary"` — reliable programmatic discriminant.
- `compactMetadata.preTokens` — total context window token count immediately before compaction.
- `compactMetadata.postTokens` — total context window token count after compaction (the summary only).
- `compactMetadata.trigger` — `"auto"` for automatic compaction; `"manual"` is also observed.

`preTokens` and `postTokens` are context-window sizes, not per-message usage delta pairs. They do not directly map to the `input_tokens` / `output_tokens` counters on individual `assistant` messages. The per-message usage accumulation and the context-window sizes are separate accounting dimensions.

## Design Constraint: The 32 KB Tail Window

`TailRead` reads the last 32 768 bytes of a JSONL file. For any session that has done meaningful work after a compaction, the `compact_boundary` marker lies before the tail boundary. In the sample corpus (26 session files with compaction markers, sizes 136 KB–18 MB) only the smallest subagent file (136 KB) had markers visible in the tail at all; every session file over ~300 KB had all markers outside the tail. The fix cannot assume the tail window will ever contain `compact_boundary` entries for active sessions.

## Goals

- Cumulative `TokenUsage` in `SessionData` reflects all tokens across the entire session lifetime, including all pre-compaction epochs.
- Token totals and cost estimates in the SSE broadcast are accurate for compacted sessions.
- No change to the 32 KB tail-read limit (it is intentional — full reads on 18 MB files per SSE tick are not acceptable).
- Server restart must not silently drop accumulated baselines for ongoing sessions.
- Multiple compactions in one session are handled correctly.

## Non-Goals

- No retroactive backfill of historical sessions in the DB. CI-4 only fixes the live SSE display.
- No change to `sdk.TokenUsage` field names or JSON shape (downstream consumers depend on it).
- No UI changes beyond the existing token/cost display getting accurate numbers.
- No per-compaction-epoch breakdown in the UI in v1.
- No support for providers other than Claude (Codex/Gemini do not have compaction yet).

## Decisions

| # | Question | Choice |
|---|---|---|
| 1 | Baseline derivation | Full scan for `compact_boundary` markers on cache miss; markers carry per-epoch token offsets directly |
| 2 | Offset accumulation | Sum of `preTokens - postTokens` across all `compact_boundary` entries (details in Section A.3) |
| 3 | Storage location | In-process per-session cache keyed by `(sessionID, fileSize)` inside `parser`; no DB write |
| 4 | Persistence across server restart | Re-derive from full scan on first cache miss; no separate persistence file |
| 5 | Full scan trigger | On cache miss only — same path that already tail-reads; no extra I/O on cache hit |
| 6 | Layering | All logic lives in `parser` package; `sdk.TokenUsage` gains an unexported baseline offset struct; `merger` is unchanged |

## Solution Approaches

### Option A — Detect Token Count Decrease Between Polls (Recommended)

**Description:** During each poll cycle, compare the new `TokenUsage` accumulated from the tail against the previously broadcast value stored in the session result cache. A significant decrease in any counter (e.g. output tokens drop by > 50 %) signals that a compaction event has occurred since the last poll and the running totals have reset. When a decrease is detected, treat the previous broadcast total as the new baseline offset and add the current tail-accumulated value on top.

**Pros:**
- Zero additional I/O — operates entirely on values already in memory.
- Detects compaction regardless of where the `compact_boundary` entry falls in the file.
- Simple diff logic, no JSON schema coupling.

**Cons:**
- Requires at least two successive polls to detect a compaction — the first poll after compaction briefly shows a low count, then the second poll adds the offset. For a 3-second SSE interval this is a maximum 3-second display glitch.
- False positives are possible if the session file is replaced or truncated (new session under same path).
- Cannot recover accurate history after a server restart — the previous broadcast total is lost, so restarts start fresh from the tail.
- Does not leverage `preTokens`/`postTokens` which encode the true context size and could recover accurate history from a cold start.

**Effort:** 1.5 PD

---

### Option B — Full File Scan for `compact_boundary` on Cache Miss (Recommended)

**Description:** When `ParseSessionFile` is called for a cache-miss path, scan the entire file (not just the tail) to locate all `compact_boundary` entries. For each such entry, accumulate a baseline offset. Then tail-read as now and add the offset to the tail-accumulated counters before returning `SessionData.TokenUsage`. Cache the baseline offset alongside the existing session result cache entry, keyed by `(path, fileSize, lastCompactBoundaryOffset)` so it is invalidated when new content is appended but preserved across SSE ticks.

**Implementation sketch:**

1. Introduce `compactionBaseline` struct in `parser` (unexported):
   ```go
   type compactionBaseline struct {
       // Last byte-offset at which a compact_boundary was found.
       // Used as part of the cache invalidation key.
       lastMarkerByteOffset int64
       // Per-counter accumulated offsets from all compact_boundary epochs.
       // Derived from actual usage sums from the preceding epoch (not preTokens).
       inputOffset         int
       outputOffset        int
       cacheCreateOffset   int
       cacheReadOffset     int
   }
   ```

2. In `ParseSessionFile`, after `TailRead`, check: is this a cache hit? If yes, return cached data immediately (unchanged). On cache miss, before or alongside the tail parse, call `scanCompactionBaseline(path)` which opens the file linearly, collects per-epoch `assistant` usage sums, and resets the running sum each time it encounters a `compact_boundary`.

3. `scanCompactionBaseline` returns a `compactionBaseline`. The accumulated offsets are the sum of all per-epoch `assistant` usage counters from epochs _before_ the final (current) epoch.

4. The tail-read `ParseSessionFile` then adds `baseline.inputOffset` etc. to the tail-accumulated `TokenUsage` before returning.

5. Store the baseline in `sessionCacheEntry`. On the next tick the cache check passes and `scanCompactionBaseline` is not called again unless the file grows past a new compaction.

**Cache invalidation:** when `mtime` or `inode` changes (existing check), invalidate both the cached `SessionData` and the `compactionBaseline`. A new compaction writes a `compact_boundary` entry to the file, bumping `mtime`, which triggers a cache miss and a fresh full scan.

**Pros:**
- Fully accurate from cold start — no polling gap.
- Handles any number of compactions correctly.
- Works correctly after a server restart (re-scans once per cache miss).
- Leverages actual per-epoch `assistant` usage counters (more accurate than `preTokens - postTokens` which counts context-window size, not billable usage).
- No additional I/O on cache hits.

**Cons:**
- The full scan on cache miss has O(file size) cost — for an 18 MB file this is ~100 ms of sequential I/O. This happens once per mtime change, not per SSE tick.
- Slightly more complex implementation than Option A.

**Effort:** 2.5 PD

---

### Option C — Persist Baseline to SQLite

**Description:** On each full scan, write the derived `compactionBaseline` to the existing `dashboard-tasks.db` SQLite database, keyed by `sessionID`. On parser startup (or on every cache miss), read from DB first, fall back to full scan.

**Pros:**
- Survives server restarts with zero re-scan cost.
- Consistent across multiple processes reading the same JSONL.

**Cons:**
- Violates the layering rule — `parser` must not import `db/repo` or any pipeline package (see task-pipeline.md §Go Layer Direction). A write from `parser` into SQLite would require either a callback injection or breaking the layer boundary. Either path adds significant complexity.
- The existing `sessionCacheEntry` already provides per-process caching. The restart-cold-scan cost (one O(file size) read per active session on boot) is acceptable given SSE broadcasts only start after the first scan completes.

**Effort:** 4 PD

**Verdict:** Not recommended. Layer violation cost outweighs the marginal startup-time benefit.

## Recommendation

**Option B — Full File Scan on Cache Miss** is the recommended approach.

The key reasons: it is accurate from a cold start, handles multiple compactions and server restarts without additional persistence, and the full-scan cost is amortized across the session cache TTL (3 seconds by default) rather than paid on every SSE tick. The single incremental complexity over Option A is the `scanCompactionBaseline` function and the `compactionBaseline` struct, both of which are small, testable, and entirely contained within the `parser` package.

## Section A — Backend Design

### A.1 New Type: `compactionBaseline`

In `server/internal/parser/parser.go` (unexported):

```go
// compactionBaseline accumulates token counts for all epochs before the current
// (post-final-compaction) tail window.
type compactionBaseline struct {
    lastMarkerOffset int64 // byte offset of last compact_boundary in file
    InputTokens      int
    OutputTokens     int
    CacheCreationTokens int
    CacheReadTokens  int
}
```

### A.2 Epoch Scanner: `scanCompactionBaseline`

```go
// scanCompactionBaseline does a single linear pass over path to accumulate
// per-epoch assistant usage counters. It resets the running counter each time
// it encounters a compact_boundary entry and keeps a running offset sum.
// Returns zero baseline if no compact_boundary entries are found.
func scanCompactionBaseline(path string) (compactionBaseline, error)
```

Algorithm:

1. Open file, create `bufio.Scanner` with a 256 KB buffer.
2. Maintain `epochInput, epochOutput, epochCacheCreate, epochCacheRead int` and `baseline compactionBaseline`.
3. For each line:
   - If `type == "system"` and `subtype == "compact_boundary"`: add epoch counters to baseline offsets, reset epoch counters, update `lastMarkerOffset` to current byte position.
   - If `type == "assistant"` or `type == "message"` with `role == "assistant"`: accumulate `usage` into epoch counters (same logic as tail parse).
4. Do NOT add the final epoch's counters to the baseline — those are the post-last-compaction tokens that will be picked up by the tail scan.
5. Return `baseline`.

The function reads the full file once. It does not call `TailRead`.

### A.3 Integration into `ParseSessionFile`

Current flow:
```
TailRead → scan lines → accumulate usage → return SessionData
```

New flow (cache miss path only):
```
scanCompactionBaseline → TailRead → scan lines → accumulate usage
                      → add baseline offsets → return SessionData
```

On cache hit, `sessionCacheEntry.baseline` is returned as-is with no additional I/O.

`sessionCacheEntry` gains one new field:

```go
type sessionCacheEntry struct {
    // ... existing fields ...
    baseline compactionBaseline
}
```

The invalidation key for `baseline` is the same as for `data` — `inode + mtime` change on any write to the file.

### A.4 TokenUsage Offset Application

After the tail scan loop in `ParseSessionFile`:

```go
data.TokenUsage.InputTokens         += baseline.InputTokens
data.TokenUsage.OutputTokens        += baseline.OutputTokens
data.TokenUsage.CacheCreationTokens += baseline.CacheCreationTokens
data.TokenUsage.CacheReadTokens     += baseline.CacheReadTokens
```

### A.5 Layering

All changes are confined to `server/internal/parser/parser.go`. The `scanner`, `merger`, `sdk`, and `api` packages are unchanged. `sdk.TokenUsage` gains no new fields — offset addition is an internal implementation detail of `ParseSessionFile`. No imports are added that would violate the Go layer rules.

`compactionBaseline` is unexported; it is not part of the `SessionData` struct and is not exposed to callers.

### A.6 Performance Envelope

| Scenario | I/O cost |
|---|---|
| Cache hit (no mtime change) | Zero — returns cached `SessionData` |
| Cache miss, no compaction in file | One sequential read of full file (same as any first parse) |
| Cache miss, N compaction events | Same single sequential read — `scanCompactionBaseline` is a single pass |
| Active session, SSE tick every 3s, file written | One full scan per write event (mtime changes). Between writes: cache hit. |

For an 18 MB file, a sequential buffered read on a modern SSD takes ~30–80 ms. This is acceptable given it occurs once per session per mtime change, not per tick.

## Section B — sdk.TokenUsage

No changes required. The `sdk.TokenUsage` struct remains:

```go
type TokenUsage struct {
    InputTokens         int `json:"inputTokens"`
    OutputTokens        int `json:"outputTokens"`
    CacheCreationTokens int `json:"cacheCreationTokens"`
    CacheReadTokens     int `json:"cacheReadTokens"`
}
```

The baseline offset is added before the struct is populated. Callers (merger, api) see the already-corrected totals.

## Section C — Merger and Agent

`server/internal/merger/merger.go::buildAgent` is unchanged. It receives the corrected `SessionData.TokenUsage` from the parser and passes it to `EstimateCostForProvider` as now. Cost re-computation is consistent because it operates on the already-corrected totals.

## Section D — Edge Cases

| Case | Handling |
|---|---|
| Multiple compactions in one session | `scanCompactionBaseline` accumulates across all epochs; each `compact_boundary` resets the epoch counter and adds to the running baseline sum. |
| Session with zero compactions | `scanCompactionBaseline` returns a zero baseline; no change to token counts. |
| Compaction fires during an active SSE interval | `mtime` changes on the next JSONL write, cache miss fires, full scan picks up all `compact_boundary` entries including the new one. One possible SSE tick shows a momentary dip before the miss fires — acceptable. |
| Server restart | First cache miss after restart triggers `scanCompactionBaseline` — accurate from boot. |
| Session file replaced (new session, same path) | `inode` changes, cache miss fires, fresh scan. |
| Compaction immediately before session ends (final entry is `compact_boundary`) | `scanCompactionBaseline` adds all prior epochs to baseline; tail window is nearly empty; correct. |
| Subagent JSONL files | Same `ParseSessionFile` code path; subagent files are treated identically. |
| `compactMetadata` absent in older Claude versions | Guard: if `subtype == "compact_boundary"` but `compactMetadata` is missing, the epoch reset still fires (epoch counters accumulate correctly regardless of `compactMetadata`). |

## Section E — Test Plan

### E.1 Unit Tests: `server/internal/parser/compaction_test.go`

**Fixture directory:** `server/internal/parser/testdata/compaction/`

**Fixtures required (synthetic JSONL files):**

| Fixture | Description |
|---|---|
| `no_compaction.jsonl` | 5 assistant turns, no `compact_boundary`. Baseline must be zero; totals equal naive sum. |
| `single_compaction.jsonl` | 3 assistant turns, one `compact_boundary`, 2 assistant turns after. Baseline = sum of first 3 turns. Total = sum of all 5 turns. |
| `double_compaction.jsonl` | 3 + 3 + 2 turns across two compactions. Baseline = sum of first 6 turns; total = sum of all 8. |
| `compaction_at_end.jsonl` | 3 turns then a `compact_boundary` with nothing after. Total = sum of 3 turns. |
| `missing_compact_metadata.jsonl` | `compact_boundary` entry with `compactMetadata` field absent. Must not panic; epoch resets correctly. |

**Test cases for `scanCompactionBaseline`:**

- `no_compaction.jsonl` → zero baseline, no error.
- `single_compaction.jsonl` → `baseline.OutputTokens` == sum of first 3 `output_tokens` values.
- `double_compaction.jsonl` → baseline equals sum of epochs 1 and 2; tail scan adds epoch 3.
- `missing_compact_metadata.jsonl` → no panic, returns non-zero baseline.

**Test cases for `ParseSessionFile` (end-to-end):**

- `single_compaction.jsonl` → `SessionData.TokenUsage` equals the correct all-epoch total.
- `double_compaction.jsonl` → same, two-compaction scenario.
- `no_compaction.jsonl` → totals unchanged from current behavior (regression guard).

**Cache invalidation test:**

Call `ParseSessionFile` twice on `single_compaction.jsonl`. Confirm second call is a cache hit (no additional I/O — mock or counter in test). Append a line to the fixture and call again; confirm cache miss and re-scan.

### E.2 Existing Tests

Run `task test` to confirm all existing parser tests still pass. The change is additive — existing tests with non-compacted fixtures get a zero baseline, so their expected token counts are unchanged.

### E.3 Acceptance Criteria

- Open a session known to have one or more `compact_boundary` entries in its JSONL.
- Dashboard card for that session shows a `costEstimate` greater than zero and consistent with the all-epoch token counts.
- Refreshing the dashboard within the `SessionCacheTTL` window does not re-trigger a full scan (no second full-file I/O event visible in `slog` debug output).
- After a server restart, the same session shows the same corrected counts without requiring a new write to the JSONL.

## Section F — Affected Components

| Component | Change |
|---|---|
| `server/internal/parser/parser.go` | Add `compactionBaseline`, `scanCompactionBaseline`, integrate into `ParseSessionFile`, extend `sessionCacheEntry`. |
| `server/internal/parser/testdata/compaction/*.jsonl` | New synthetic fixture files (5 files). |
| `server/internal/parser/compaction_test.go` | New test file. |
| `sdk/types.go` | No change. |
| `server/internal/merger/merger.go` | No change. |
| `src/` (frontend) | No change — token totals are already rendered correctly; fixing the source values is sufficient. |

## Effort Estimate

| Phase | Effort | Description |
|---|---|---|
| `scanCompactionBaseline` + integration | 1.0 PD | Implement linear scanner, integrate into `ParseSessionFile`, extend cache entry |
| Fixtures + unit tests | 0.75 PD | 5 synthetic JSONL fixtures, table-driven tests for scanner and end-to-end |
| Cache invalidation verification + slog instrumentation | 0.25 PD | Debug-level log on full-scan trigger, verify in acceptance criteria |
| Review + QA | 0.5 PD | Code review, run `task test`, manual verification on a real compacted session |
| Buffer (20%) | 0.5 PD | Unforeseen edge cases in real-world JSONL variants |
| **Total** | **3.0 PD** | |

## Risks

| Risk | Probability | Impact | Mitigation |
|---|---|---|---|
| Full scan on 18 MB+ files causes visible SSE latency spike | Medium | Medium | Full scan is O(file size) but sequential; benchmark on the largest fixture before shipping. If > 200 ms, cap at a configurable max file size (default 50 MB) and log a warning. |
| Claude CLI changes `compact_boundary` schema in a future version | Low | Medium | Guard with nil-check on `compactMetadata`; the epoch-reset logic is based on `subtype` detection, not metadata values, so token accumulation remains correct even if metadata fields change. |
| `scanCompactionBaseline` and `TailRead` produce overlapping coverage near the tail boundary | Low | Medium | The scanner excludes the final epoch from baseline (the tail scan covers it). The tail window's first line is always skipped as potentially truncated. Overlap would at worst double-count a few lines — verify with the `single_compaction.jsonl` fixture that the total matches the ground truth. |
| High-frequency compactions in pipeline sub-agents cause repeated full scans | Low | Low | Pipeline sub-agents are short-lived; their session files are small (< 1 MB). Full scan on small files is < 10 ms. Non-issue in practice. |
