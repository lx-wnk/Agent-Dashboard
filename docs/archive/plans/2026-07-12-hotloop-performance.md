# Plan: Hot-loop performance hardening

**Goal:** The scan→parse→merge→SSE-broadcast loop runs every ~3s and is O(Σ bytes-on-disk) in
two hot spots: every active agent's entire JSONL is re-read + JSON-decoded each tick just to sum
tokens (bench: `ParseSessionFile` = 2.79 ms / 1.2 MB / 15,956 allocs at 500 lines, *linear* →
~5–56 ms and 2.4–24 MB alloc per active agent per tick on real 1k–10k-line sessions), and an
unindexed `stage_run.session_id` lookup runs a full-table scan per agent per tick that degrades
silently as history grows. This plan makes the token total incremental (O(bytes-appended)), adds
the missing index behind a migration guard, batches the enricher N+1, and clears three lower-tier
loop costs — with zero behaviour change to the emitted `Agent[]` payload.

**Architecture:** (all backend Go — no frontend touched; SSE frame shape `{agents, trend}` unchanged)
- `server/internal/db/ent/schema/stage_run.go` + `server/internal/db/client.go` — new `session_id`
  index + idempotent pre-seed migration guard (avoids the PR #207 phantom-column rebuild crash)
- `server/internal/db/repo/stage_run_repo.go` + `permission_repo.go` — new batch `IN (...)` queries
- `server/internal/agentbroadcast/enricher.go` — replace 3 serial queries/agent with 3 batched queries
- `server/internal/parser/scan.go` + `messages.go` + `parser.go` — over-long-line-safe scan + an
  incremental per-file token accumulator (inode+size invalidation, error → full rescan)
- `server/internal/merger/merger.go` — read each discovery file once/agent/tick; hoist `UserHomeDir`
- `server/internal/api/agents/handler.go` + `server/internal/sse/broadcaster.go` — serve the last
  broadcast frame on SSE connect / GET instead of an un-shared full scan

**Tech Stack:** Go 1.26 + ent (sql/upsert feature) + modernc/sqlite. Backend-only change set.

**Sequencing / PR grouping:** DB1 → DB2 (PR 1) · LOW4 → HOT1 (PR 2) · LOW3 → LOW2 (PR 3).
DB1 precedes DB2 (the not-found lookup is a scan until the index lands, so DB2's win compounds it).
LOW4 precedes HOT1 (an over-long line must skip-not-fail *before* incremental scanning, else a torn
line aborts the scan and would poison the running accumulator). The three PRs are independent.

---

## Task 1 — PERF-DB1: `stage_run.session_id` index + phantom-column-safe migration guard

**PR 1.** Kills a per-agent-per-tick full-table scan (`GetBySessionID`, `stage_run_repo.go:104`).
The gotcha: an ent index change can force SQLite's 12-step table rebuild, which crashes on existing
DBs with `NOT NULL constraint failed: stage_runs.id` (PR #207). Guard: pre-seed the index via raw
`CREATE INDEX IF NOT EXISTS` *before* ent auto-migrate so ent's diff finds it already present and
never rebuilds. Belt-and-suspenders (Option C): schema for fresh DBs + pre-seed for existing DBs.

### Files
- `server/internal/db/ent/schema/stage_run.go` (add index)
- `server/internal/db/ent/` (deliberate regen — commit separately)
- `server/internal/db/client.go` (new `migrateEnsureStageRunSessionIndex`, wired before `Schema.Create`)
- `server/internal/db/client_test.go` (migration test)

### Steps
1. **Add the index** to `StageRun.Indexes()` (stage_run.go:45, alongside the existing entries):
   ```go
   index.Fields("session_id"),
   ```
2. **Regen ent** (the ONLY deliberate regen this task): `cd server && go generate ./internal/db/ent/...`
   then confirm the diff is index-only: `git diff --name-only server/internal/db/ent/ | grep -v migrate/schema.go` should be near-empty.
3. **Read the exact generated index name** — open `server/internal/db/ent/migrate/schema.go`, find the
   `stage_runs` table `Indexes` block, and copy ent's generated `Name:` for the new `session_id` index
   (e.g. `stage_runs_session_id`). The pre-seed CREATE INDEX **must** use this exact name, or ent will
   still try to create *its* differently-named index and may trigger the rebuild.
4. **Add the guard** in `client.go` (follow the `migrateLegacyPipelineConfig` pattern at :382), wired
   **before** `client.Schema.Create` at client.go:89:
   ```go
   // Pre-seed the stage_run.session_id index so ent's auto-migrate sees it already
   // present and does a no-op instead of a table rebuild (which crashes on existing
   // DBs with NOT NULL id — see PR #207). Idempotent via IF NOT EXISTS.
   if err := migrateEnsureStageRunSessionIndex(sqlDB); err != nil { ... }
   ```
   ```go
   func migrateEnsureStageRunSessionIndex(db *sql.DB) error {
       _, err := db.Exec(`CREATE INDEX IF NOT EXISTS <EXACT_ENT_NAME> ON stage_runs(session_id)`)
       if err != nil { return fmt.Errorf("ensure stage_run session_id index: %w", err) }
       return nil
   }
   ```
5. **Test** (client_test.go): open a fresh in-memory DB via `db.Open`, then assert
   `PRAGMA index_list(stage_runs)` contains the index; and that a second `db.Open` on the same file is a
   clean no-op (idempotency). Verify plan: `EXPLAIN QUERY PLAN SELECT * FROM stage_runs WHERE session_id=?`
   reports `USING INDEX`, not `SCAN`.
6. **Commit** schema, then regen, then guard+test as separate commits.

---

## Task 2 — PERF-DB2: batch the enricher N+1

**PR 1.** `enricher.go:37` issues `GetBySessionID` → `GetByID` → `ListPendingForStageRun` serially
per agent per tick (1–3 round-trips × N agents). Batch to 3 `IN (...)` queries joined in-memory.
`TaskRepo.ListByIDs` already exists (task_repo.go:27) — reuse it.

### Files
- `server/internal/db/repo/stage_run_repo.go` (+ `ListBySessionIDs`)
- `server/internal/db/repo/permission_repo.go` (+ `ListPendingForStageRuns`)
- `server/internal/agentbroadcast/enricher.go` (rewrite loop)
- repo `_test.go` for the two new batch methods

### Steps
1. **Add batch repo methods** (mirror the existing single-row queries):
   ```go
   // stage_run_repo.go
   ListBySessionIDs(ctx, ids []string) ([]*ent.StageRun, error) // WHERE session_id IN (ids)
   // permission_repo.go
   ListPendingForStageRuns(ctx, stageRunIDs []string) ([]*ent.PermissionRequest, error) // pending, IN
   ```
   Both short-circuit `len(ids)==0 → nil, nil`. Add them to the repo interfaces.
2. **Rewrite the enricher** (`enricher.go:33-`): collect all non-empty `SessionID`s up front →
   `ListBySessionIDs` → map `sessionID→stageRun`; collect `TaskID`s → `ListByIDs` → map `taskID→task`;
   collect `stageRun.ID`s → `ListPendingForStageRuns` → map `stageRunID→[]req`. Then a single pass over
   `agents` assigns `PipelineTaskID`/`PipelineTaskTitle`/`PendingPermissions` from the maps. Preserve the
   existing best-effort semantics: nil repos → early return; not-found/query errors leave fields empty
   (log at Debug, never fail the scan).
3. **Test**: seed 3 sessions (one with a stage_run + task + pending perm, one stage_run-only, one
   ad-hoc/no-row); assert the enriched slice matches and that exactly 3 queries ran (inject counting repos).
4. **Commit.**

---

## Task 3 — PERF-LOW4: over-long JSONL lines skip, not fail

**PR 2.** `scan.go:56` caps `bufio.Scanner` at 4 MB; a single line >4 MB returns `bufio.ErrTooLong`,
aborting the whole token scan → silent tail-only undercount. This must land before HOT1 so the
incremental accumulator never aborts mid-append.

### Files
- `server/internal/parser/scan.go` (`ScanJSONLLines`)
- `server/internal/parser/scan_test.go`

### Steps
1. **Failing test**: build a reader with a normal line, one >4 MB line, and a trailing normal line;
   assert the callback fires for both normal lines and returns no error (today it errors after the big one).
2. **Impl**: replace the `bufio.Scanner` in `ScanJSONLLines` with a `bufio.Reader` + `ReadBytes('\n')`
   loop. For each line: if `len > maxLine` (keep 4 MB as the per-line inspect cap), skip it (do not invoke
   `fn`) and `slog.Info("parser: skipping over-long JSONL line", "bytes", len)`; otherwise trim + skip
   empty + invoke `fn`, honouring `ErrStopScan`. Handle a final non-newline-terminated chunk. This keeps
   the small-line fast path and removes the abort-the-whole-file failure mode.
3. **Run-pass** + **Commit.**

---

## Task 4 — PERF-HOT1: incremental token accumulator (Option A)

**PR 2.** Replace the whole-file re-scan at `parser.go:806` (`scanFullFileTokenUsage`, full file) with
an incremental accumulator. Per-message `usage` is per-turn, not cumulative (parser.go:326-355), and
the JSONL is append-only, so the lifetime total is an exact running sum — appended bytes can be summed
without re-reading history. **Invalidation (inode + size):** same inode & `size ≥ lastOffset` →
`Seek(lastOffset)`, sum only appended complete lines, advance offset to the last newline; inode change
or `size < lastOffset` (truncation/rotation) → full rescan + reset; **any scan error → discard the
partial delta, reset, and fall back to a one-shot full rescan** (never trust a partial sum). Keep the
32 KB tail read (live state) untouched.

Storage: a dedicated package-level `tokenOffsetCache` keyed by inode, guarded by a mutex — chosen over
widening `sessionCacheEntry`/`ParseSessionFile`'s signature; same lifecycle, localized change.

### Files
- `server/internal/parser/messages.go` (+ `ScanMessagesFrom(path, offset) (usage, newOffset, error)`)
- `server/internal/parser/parser.go` (`tokenOffsetCache` + `tokenUsageForFile`; call site at :806)
- `server/internal/parser/parser_bench_test.go` (append-retick bench)
- `server/internal/parser/parser_incremental_test.go` (new — correctness)

### Steps
1. **Add `ScanMessagesFrom`** (messages.go, beside `ScanMessages`): `os.Open` + `Stat` (capture size),
   `Seek(offset)`, scan to EOF via `ScanJSONLLines`, invoking the *same* per-message usage extraction
   `ScanMessages` uses (share the decode helper — must never diverge). Return the summed usage and
   `newOffset` = offset + bytes up to and including the **last** `'\n'` (a trailing partial line is left
   for the next tick). Empty appended region → zero usage, offset unchanged.
2. **Add `tokenUsageForFile(path)`** in parser.go: `os.Stat` for `{inode, size}`; look up
   `tokenOffsetCache[inode]`. If present and `size ≥ entry.offset`: `usage, newOff, err :=
   ScanMessagesFrom(path, entry.offset)`; on err → full rescan (`scanFullFileTokenUsage`, reset entry);
   else `entry.running += usage; entry.offset = newOff`. If absent or `size < entry.offset` (truncation):
   full rescan, seed `entry = {inode, offset: size, running: full}`. Return `entry.running`. Guard with a
   mutex; add a simple bound (evict on inode-miss or cap map size) so idle files don't accumulate forever.
3. **Swap the call site**: at parser.go:806 replace `scanFullFileTokenUsage(path)` with
   `tokenUsageForFile(path)`; keep the tail-only fallback branch as the error path.
4. **Correctness test** (parser_incremental_test.go): write N lines → total T1; append M lines →
   assert incremental total == full-scan total; truncate/rewrite (new inode) → assert reset re-sums
   correctly; inject a `ScanMessagesFrom` error → assert fallback equals the full-scan total.
5. **Bench** (parser_bench_test.go): `BenchmarkParseSessionFile_Incremental` — seed a large file, then
   loop: append one turn + call `tokenUsageForFile`; assert B/op and allocs/op drop ~1–3 orders vs the
   full-scan `BenchmarkParseSessionFile`.
6. **Commit.**

---

## Task 5 — PERF-LOW3 / CQ-15: read each discovery file once per agent per tick

**PR 3.** `buildAgent` (merger.go:424) calls `channelDiscovery(pid)` AND (via `Working`, :441)
`recentChannelOutput(pid)`; each reads both `{pid}.json` + `{pid}.pty.json` and calls
`os.UserHomeDir()` — so 2 files are read twice + 2× `UserHomeDir` per agent per tick.

### Files
- `server/internal/merger/merger.go`
- `server/internal/merger/merger_test.go`

### Steps
1. **Hoist home**: package-level `var homeDir = sync.OnceValues(os.UserHomeDir)` (or once-value helper);
   replace the two `os.UserHomeDir()` calls (:51, :113).
2. **Single combined read**: add `readChannelDiscovery(home, pid) → {channelAvailable, liveInjectable,
   recentOutput bool}` that reads `{pid}.json` once (decode `TmuxPane` + `LastOutputAt`) and
   `{pid}.pty.json` once (decode `PtyInject` + `LastOutputAt`), deriving all three flags from those two
   decodes (fold in the existing `outputThreshold` / `tmuxActivityFn` logic). Call it **once** in
   `buildAgent`; feed its results into both `ChannelAvailable/LiveInjectable` and `Working`. Keep
   `recentChannelOutput`/`channelDiscovery` as thin wrappers over it if other callers exist (DRY), else
   inline.
3. **Test**: table-drive the flag derivation (bridge-only, pty-only, both, neither, recent vs stale
   output) and assert each discovery file is opened exactly once (temp files + read counter, or refactor
   read behind a countable seam).
4. **Commit.**

---

## Task 6 — PERF-LOW2: serve the last broadcast frame on SSE connect / GET

**PR 3.** `handler.go:60` calls `getAgents` (full scan) on every SSE connect and every GET
`/api/agents`, repeated per reconnect, bypassing the loop's hash-dedupe. Serve the last already-marshaled
broadcast frame instead.

### Files
- `server/internal/sse/broadcaster.go` (+ `LastFrame() []byte`)
- `server/internal/api/agents/handler.go` (Stream initial-send + GET path)
- tests for both

### Steps
1. **`LastFrame`**: have the broadcaster retain the most recent frame it published (guarded by its
   existing mutex) and expose `LastFrame() []byte` (nil before the first tick). The frame is already the
   `{agents, trend}` SSE-shaped payload.
2. **Wire the handler** (handler.go:59-66): if `broadcaster.LastFrame()` is non-nil, write it raw +
   flush; else fall back to the current `getAgents` marshal (cold start, before first tick). Apply the
   same to the GET `/api/agents` path.
3. **Test**: first connect before any tick → falls back to `getAgents`; after a tick → initial-send
   equals the retained frame and `getAgents` is **not** called (inject a counting `GetAgentsFn`).
   Assert the emitted shape stays exactly `{agents, trend}` (client contract).
4. **Commit.**

---

## Final Verify

Run from repo root unless noted. **ent-regen caveat:** `go test ./...` regenerates
`server/internal/db/ent/` and can corrupt it — after any full test run, `git status
server/internal/db/ent/`; if the generated tree drifted beyond Task 1's intended index,
`git checkout -- server/internal/db/ent/`. Commit only the deliberate Task 1 regen.

```bash
# Build + static checks
cd server && go build ./... && go vet ./... && gofmt -l internal/ cmd/   # gofmt prints nothing

# Targeted tests per PR
cd server && go test ./internal/db/... ./internal/agentbroadcast/... -count=1   # PR 1
cd server && go test ./internal/parser/... -count=1                             # PR 2
cd server && go test ./internal/merger/... ./internal/api/agents/... ./internal/sse/... -count=1  # PR 3

# HOT1 regression bench (before vs after; expect B/op + allocs/op down ~1–3 orders on the incremental path)
cd server && go test ./internal/parser/ -run '^$' -bench 'ParseSessionFile' -benchmem -count=3

# DB1 migration safety — copied REAL pre-index DB (do NOT validate only against a fresh DB):
cp ~/.agent-dashboard/*.db /tmp/preindex.db      # adjust to the machine's actual sqlite path
#   point a built server at /tmp/preindex.db (db.Open) and confirm startup logs no error, THEN:
sqlite3 /tmp/preindex.db 'PRAGMA index_list(stage_runs);'    # new session_id index present, no crash
sqlite3 /tmp/preindex.db 'EXPLAIN QUERY PLAN SELECT * FROM stage_runs WHERE session_id = "x";'  # USING INDEX

# Full suite last (mind the ent-regen caveat above)
cd server && go test ./... -count=1
```

No frontend build required — this change set is backend-only and the SSE `{agents, trend}` frame shape
is unchanged.
