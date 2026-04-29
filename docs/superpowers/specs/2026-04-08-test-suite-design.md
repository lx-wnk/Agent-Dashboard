# Test Suite Design — Claude Agent Overview

## Scope

Vitest unit/integration tests (Tier 1-3) + Playwright E2E tests. Co-located test files next to source modules.

## Prerequisite Refactoring

Before writing tests, export currently-private pure functions to enable direct testing:

| File | Function to export | Purpose |
|------|--------------------|---------|
| `server/jsonlParser.ts` | `encodePath` | Path encoding (`/Users/x` -> `-Users-x`) |
| `server/jsonlParser.ts` | `extractSessionInfo` | Token/model/tool extraction from JSONL entries |
| `server/processScanner.ts` | `parseElapsedTime` | Parse `[[dd-]hh:]mm:ss` to seconds |
| `server/agentMerger.ts` | `calculateStatus` | Status thresholds (active/waiting/idle) |
| `server/sessionScanner.ts` | `decodeProjectDir` | Reverse of `encodePath` |

Also consolidate duplicated `encodePath` from `sessionScanner.ts` into `jsonlParser.ts` (DRY).

## Tier 1 — Pure Function Unit Tests

### `src/utils/format.test.ts` (~15 tests)

- `totalTokenCount`: sum of 4 fields, with zero values
- `formatTokens`: `0` -> `"-"`, `1500` -> `"1.5k"`, `2_500_000` -> `"2.5M"`
- `formatCost`: `0` -> `"-"`, `0.005` -> `"< $0.01"`, `1.234` -> `"$1.23"`
- `formatUptime`: seconds, minutes, hours+min, days+hours
- `shortModel`: `"claude-sonnet-4-20250514"` -> `"sonnet-4"`, `null` -> `"unknown"`

### `server/pricing.test.ts` (~8 tests)

- `estimateCost`: known model, unknown model (fallback), null model, cache-only tokens, zero tokens
- `MODEL_PRICING`: all models have positive values for all price fields

## Tier 2 — Exported Private Function Unit Tests

### `server/jsonlParser.test.ts` (~12 tests)

- `parseJsonlLines`: valid lines, broken JSON (skipped), empty string, mixed valid/invalid
- `encodePath`: standard path, path with underscores, root path
- `extractSessionInfo`: token aggregation across multiple entries, model extraction, tool list dedup, task list with statuses, empty entries array

### `server/processScanner.test.ts` (~6 tests)

- `parseElapsedTime`: `"05:23"` (mm:ss), `"01:05:23"` (hh:mm:ss), `"2-01:05:23"` (dd-hh:mm:ss), edge cases (zero, malformed)

### `server/agentMerger.test.ts` (~5 tests)

- `calculateStatus`: timestamp < 30s ago -> active, < 5min -> waiting, > 5min -> idle, null/undefined -> idle

### `server/sessionScanner.test.ts` (~3 tests)

- `decodeProjectDir`: `-Users-alex-code` -> `/Users/alex/code`, edge cases

## Tier 3 — Server Integration Tests (mocked I/O)

### `server/processScanner.test.ts` (extended, ~4 tests)

- `scanProcesses` with mocked `child_process.execFile`:
  - Fake `ps` output -> correct PIDs + uptimes
  - Fake `lsof` output -> correct cwds
  - Empty process list -> empty array
  - Process with no `lsof` match -> cwd undefined

### `server/systemMonitor.test.ts` (~3 tests)

- `getSystemInfo` with mocked `child_process.execFile` + `os`:
  - Fake `top` output -> CPU percentage
  - Fake `df` output -> disk usage values
  - `os` module mocked for memory/uptime

### `server/sessionScanner.test.ts` (extended, ~5 tests)

- `getSessions` with mocked `fs/promises` + dependencies:
  - Fake directory structure -> SessionInfo array
  - Empty projects directory -> empty array
  - Malformed session-meta JSON -> graceful skip

## E2E — Playwright (`e2e/dashboard.spec.ts`, 14 tests)

All tests run against the dev server at `localhost:13120`. API responses come from the real backend (testing actual process scanning on the machine).

| # | Test | Assertion |
|---|------|-----------|
| 1 | Dashboard loads | Title "Claude Agent Overview" visible |
| 2 | Header stats visible | Agent count badge rendered |
| 3 | ResourceBar renders | CPU/Memory/Disk bars visible with values |
| 4 | View toggle: list -> cards | Click cards button -> AgentCardGrid visible |
| 5 | View toggle: cards -> list | Click list button -> AgentTable visible |
| 6 | View toggle persists on reload | Switch to cards, reload, still cards |
| 7 | Search filters agents | Type in search -> agent count decreases |
| 8 | Search clear restores all | Clear search -> full list |
| 9 | Agent click opens modal | Click agent row -> modal visible |
| 10 | Modal shows details | Modal contains session info, output |
| 11 | Modal close via ESC | Press Escape -> modal hidden |
| 12 | Modal close via button | Click close button -> modal hidden |
| 13 | Sessions dialog opens | Click "Sessions" -> dialog visible |
| 14 | Spawn dialog opens | Click "+ New Agent" -> dialog visible |

## Test Infrastructure

- **Vitest config**: `vitest.config.ts` (already exists) — jsdom for `src/`, node for `server/`
- **Playwright config**: `playwright.config.ts` (already exists) — webServer auto-starts dev
- **E2E directory**: `e2e/` for Playwright tests
- **No test database or fixtures** — server reads live filesystem, E2E tests what's actually running

## Estimated Test Count

| Layer | Tests |
|-------|-------|
| Tier 1 (pure functions) | ~23 |
| Tier 2 (exported privates) | ~26 |
| Tier 3 (mocked I/O) | ~12 |
| E2E (Playwright) | 14 |
| **Total** | **~75** |
