# Usage Consumption Windows — Design Spec

> Date: 2026-06-29 · Status: Approved · Branch: `feat/usage-consumption-windows` (off `upcoming`)
> Replaces the dead/fake "quota" status-bar feature (#8). Supersedes the file-based `/api/quota` handler.

## Why

The existing QUOTA status-bar bar reads `~/.claude/usage-data/*.json` with an assumed `{periodStart, periodEnd, tokensUsed, limit}` schema. **That file is never written by Claude Code** (verified: no `usage-data/` dir, no quota/limit file anywhere under `~/.claude`). The bar is therefore permanently dead (always "—").

Research verdict (docs + GitHub issues #40395/#13585): Claude Code persists **no** quota/limit state to disk; the session (~5h) and weekly subscription limits are **server-side only**, surfaced only via the interactive `/usage` TUI, the browser, or a custom statusline script — **no CLI JSON command, no env var, no local file**. So a true subscription **headroom %** cannot be computed by a local backend.

What IS available locally: per-message token **consumption** in the session JSONL (`usage.input_tokens`, `output_tokens`, `cache_read_input_tokens`, `cache_creation_input_tokens`) plus a timestamp — which the dashboard already parses for cost. Decision (user-approved): **show honest consumption** over rolling windows that mirror Claude's limit windows, instead of a fake percentage.

## Scope

In: a backend usage aggregator that sums tokens + cost over a rolling **5h** (session-equivalent) and **7d** (weekly-equivalent) window from session JSONL across all config dirs, grouped by config dir (account); `GET /api/usage` returning the windows + per-account breakdown; optional per-window **soft budget** (user setting) deriving a %; a `useUsage` composable (polling); a status-bar segment replacing QUOTA + an expandable popover; optional budget inputs in settings.

Out: real subscription headroom % (unknowable — server-side only); scraping the `/usage` TUI; API-key `anthropic-ratelimit-*` headers (1-min window, not session/weekly); any new ent schema beyond the two `app_setting` budget keys (which use the existing KV store).

## Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | Replace the file-read `/api/quota` with a JSONL-derived `/api/usage`; delete the dead `usage-data` reader | The file never exists; consumption from JSONL is the only real local signal. |
| D2 | Two fixed rolling windows: **5h** (session) + **7d** (weekly) | Mirror Claude's actual session + weekly limit windows so the numbers are meaningful even without the limit itself. |
| D3 | Aggregate across **all config dirs**, grouped by config dir = account | "Account for both spawners": multiple Claude spawners write sessions under different `CLAUDE_CONFIG_DIR`s; summing all of them is the total, grouping gives the per-account view. |
| D4 | **Optional** soft budgets (`usage.budget.session`, `usage.budget.weekly`, token counts) in `app_setting`; when set, derive `pct = used/budget`; when unset, show raw consumption (no fake %) | Honest: no invented denominator. Users who know their plan can opt into a %-bar; others see real numbers. |
| D5 | Status bar shows the **worst-case** window (nearest its budget) as a % bar when budgets exist, else compact consumption text; click/hover → popover with both windows + per-account + cost | Reuse the agreed worst-case+expandable pattern; degrade gracefully without budgets. |
| D6 | Cache the JSONL scan for ~60s server-side | Polling (5 min) plus any extra callers must not re-scan all JSONL each hit. |

## Architecture

### Backend — usage aggregator (`server/internal/api/system/` + a usage package)
- **Config-dir union:** reuse the existing all-config-dirs helper (env `DASHBOARD_CLAUDE_CONFIG_DIRS` + `CLAUDE_CONFIG_DIR`; the parser already has `allClaudeConfigDirs()`). Each dir = one account; label = dir basename.
- **Scan:** for each config dir, enumerate `projects/*/*.jsonl` modified within the last 7d (mtime prefilter to bound work); reuse the existing JSONL scan/parse primitive. For each assistant message with a `usage` block and a timestamp, add `(input + output + cache_read + cache_creation)` tokens and the message cost to whichever trailing windows contain its timestamp (a message counts toward 7d always, and toward 5h if within now−5h). Cost reuses the existing per-model pricing used by the cost calculator (single source — do not reimplement pricing).
- **Aggregate:** produce per-account `{label, w5h:{tokens,costCents}, w7d:{tokens,costCents}}` and a total across accounts. Attach optional budgets from settings and compute `pct` per window when a budget is present.
- **Cache:** memoize the scan result for ~60s (timestamp-stamped) to absorb polling; invalidation is purely time-based.
- **Endpoint:** `GET /api/usage` (replace the `system.Quota` handler/route). Response:
  ```json
  {
    "windows": [
      {"key":"5h","tokens":2100000,"costCents":430,"budgetTokens":null,"pct":null},
      {"key":"7d","tokens":14000000,"costCents":2900,"budgetTokens":50000000,"pct":0.28}
    ],
    "accounts": [
      {"label":".claude","w5h":{"tokens":...,"costCents":...},"w7d":{...}},
      {"label":".claude-work","w5h":{...},"w7d":{...}}
    ]
  }
  ```
  `budgetTokens`/`pct` are null when no budget is set. `accounts` omitted/empty when only one account.

### Settings (budgets)
- Two optional integer settings via the existing settings registry / `app_setting` KV: `usage.budget.session` and `usage.budget.weekly` (token counts; 0/empty = unset). No restart needed (read at request time).

### Frontend
- **`src/composables/useUsage.ts`:** fetch `/api/usage` on mount + poll every 5 min (interval cleared on unmount). Exposes `windows`, `accounts`, and a derived `worst` (the window with the highest `pct`, only among windows that have a budget).
- **Status-bar segment** (replaces the QUOTA segment in `AppStatusBar.vue`): if any window has a budget → render the worst-case window's `pct` as a bar with the existing green/amber/red thresholds, label `SESSION` or `WEEKLY`; else → compact text (`5h 2.1M · 7d 14M`). Click/hover opens a popover: both windows (tokens + cost), and the per-account breakdown when >1 account.
- **Settings panel:** optional budget inputs (session/weekly token budget) writing the two `app_setting` keys; empty = no budget = consumption-only display.

## Data flow
```
poll (5 min) → GET /api/usage
  → union(config dirs from env)            # accounts
  → for each dir: scan projects/*/*.jsonl (mtime ≤ 7d), sum usage tokens+cost per message into 5h/7d windows   [cached 60s]
  → attach budgets (app_setting) → pct
  → { windows[], accounts[] }
UI: worst-case %-bar (if budgets) or consumption text; popover = both windows + per-account + cost
```

## Error handling
- Missing/empty config dir, unreadable or malformed JSONL line → skipped (debug log); aggregation continues.
- No budget set → `pct=null`, UI shows consumption only (never a fabricated %).
- Zero usage in a window → `tokens:0` (bar/“0”, not "—").
- Scan error for one dir → that account omitted; other accounts still returned.

## Testing
- **Aggregator:** temp config-dir(s) with fixture `*.jsonl`; assert messages inside vs outside the 5h/7d windows sum correctly; multi-dir grouping + total; mtime prefilter skips >7d-old files; malformed line tolerated; cost matches the existing pricing for a known model.
- **Budget/pct:** with a budget set, `pct = used/budget`; without, `pct=null`. Worst-case selection picks the higher-pct budgeted window.
- **Cache:** two calls within 60s scan once (inject a clock/scan-count seam).
- **`useUsage`:** polls on the interval (fake timers); clears interval on unmount.
- **Status bar:** budget present → %-bar + threshold color + SESSION/WEEKLY label; no budget → consumption text; popover renders both windows + per-account when >1.

## Risks / notes
- Scanning all JSONL is the cost; the mtime≤7d prefilter + 60s cache bound it. If still heavy on large histories, a follow-up can persist incremental per-day usage — out of scope here.
- The 5h/7d windows are an honest proxy for Claude's session/weekly limits, not the limits themselves; the UI must not imply it knows the subscription cap (no "%" unless the user supplied a budget).
- Reuse the existing JSONL parse + cost pricing (SSOT); do not duplicate token/cost logic.
- `agent_cost_trend`/`stage_run` DB tables hold only pipeline-task usage, not all interactive sessions → not a substitute for the JSONL scan.
