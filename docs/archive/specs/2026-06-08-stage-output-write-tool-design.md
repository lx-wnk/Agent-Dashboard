# Stage-Output Write Tool — Design

**Date:** 2026-06-08
**Status:** Approved (design), pending implementation plan
**Branch:** `feat/voice-refinement-plugin-seam` (new branch off `upcoming` for this work)

## Problem

The pipeline captures each stage agent's structured result by **scraping a
` ```json ` block out of the agent's free-form last assistant turn**
(`session_reader.go:ExtractJsonBlock`, fed by `lastAssistantText`). This is
structurally unreliable:

1. **Last-turn-only.** `lastAssistantText` (`session_reader.go:143`) returns the
   *first* assistant entry (scanning backwards) that has text parts. If the agent
   emits the ` ```json ` block and then does *one more* assistant turn — a "done"
   remark, a `dashboard_reply` confirmation, a trailing insight box — the block
   lived in a prior turn and is never read. Result: `ExtractJsonBlock` runs on the
   wrong text, returns `nil`, and the stage fails with
   `agent did not produce a ```json output block`
   (`completion_detector.go:173`) **even though the UI panel clearly shows the
   block** (the panel renders more than the validator reads).

2. **Regex-on-free-text + 32 KB tail.** `TailRead` (`session_reader.go:174`) reads
   only the last 32 KB; a long final turn can push the block out of the window. Any
   stray ` ``` ` fence-art in prose (e.g. the "★ Insight" box) can confuse parsing.

The root issue: **extracting a machine contract out of free-form prose depends on
the agent formatting its output exactly right, with no feedback loop.** We cannot
reliably instruct an agent to shape its final message. A dedicated write path the
agent *calls* removes the parsing layer entirely and lets us validate while the
agent is still alive.

## Scope (decided)

- **Stage-output only.** A write tool by which the agent explicitly submits its
  structured per-stage result (`summary`/`commits`/`passed`/`findings`/…). Replaces
  the ` ```json ` scrape as the **primary** path. *Not* a cross-stage shared
  blackboard — that is a possible future Phase 2, out of scope here.
- **Synchronous validation.** The write path validates the payload against the
  stage schema *at write time* and returns an error to the live agent on mismatch,
  so it corrects and re-calls — replacing the expensive retry-after-death loop.
- **Tool primary, ` ```json ` scraping as fallback.** Strictly additive. If the
  stored tool output is present it is used; otherwise the existing scrape path runs
  unchanged. No regression; non-channel adapters keep working.
- **Origin-403 fix included** (see §6). Same-root-cause CSRF-middleware bug on the
  channel-bridge ingress endpoints, fixed in the same work package.

## Architecture

```
stage agent  --MCP-->  dashboard-channel tool  --loopback POST-->  /api/channel-stage-output
   set_stage_output({output})                                          │ (bearer token, no Origin)
                                                          ValidateStageOutput(stage, payload)
                                                          ├─ fail → 422 {error: <msg>}  ─┐
                                                          └─ pass → stage_runs.output=payload, 200
                                                                                          │
   MCP error w/ reason  <──────────────────────────────────────────────────────────────┘
   (live agent corrects, calls set_stage_output again)

  [agent PID dies]
        │
        ▼
  completion_detector: stage_runs.output populated?
        ├─ yes → use it (already validated; skip scrape + skip iterate-on-schema-fail)
        └─ no  → today's ExtractJsonBlock scrape → validate → iterate/wait_user (unchanged)
```

**Storage:** reuse the existing `stage_runs.output` JSON column
(`schema/stage_run.go:25`). No new table, no migration. The completion detector
already parses the scraped block into this column today; the write tool populates
the same column directly. Rejected alternative: a dedicated `stage_outputs` table —
splits the source of truth and needs a migration for no gain.

## Components

### 1. Channel tool `set_stage_output` (`channel/dashboard-channel.ts`)

- Args: `{ output: object, stageRunId?: string }`. `stageRunId` auto-injected from
  `DASHBOARD_STAGE_RUN_ID` env when omitted (mirrors `request_permission`).
- Mirrors the `dashboard_reply` fetch shape (`dashboard-channel.ts:176`): loopback
  POST, `Authorization: Bearer ${TOKEN}`, `Content-Type: application/json`.
- On `2xx`: return MCP text `"Stage output accepted."`.
- On non-2xx: return an **MCP error** whose text is the dashboard's `error` body, so
  the live agent reads the validation reason and self-corrects. (MCP error, not a
  silent text reply — the agent must treat it as a failed action.)
- Registered alongside the two existing tools; added to the tool list the bridge
  advertises.

### 2. Endpoint `POST /api/channel-stage-output`

- New handler in `internal/api/` (sibling to `ChannelReply`).
- **Auth: bearer token via discovery file, like `channel-reply`. No Origin check,
  no loopback-host middleware** (server-to-server call carries no `Origin`).
- Body: `{ stageRunId: string, output: map[string]any }`.
- Logic:
  1. Look up stage_run by id → 404 if absent/terminal.
  2. Derive `stage` from the row.
  3. `ValidateStageOutput(stage, output)` →
     - error → `422 {"error": "<validation message>"}`.
     - ok → persist `stage_runs.output = output`, `200 {"ok": true}`.
- Does **not** drive the state machine — it only records validated output. The
  orchestrator's existing completion path consumes it after PID death.

### 3. Validation reuse (layering)

`ValidateStageOutput` (`completion_detector.go:17`) is a pure, state-machine-free
function. Add it to the **api→pipeline runtime import whitelist** in
`task-pipeline.md` (the "Runtime import whitelist for routes" table) with
justification: *"pure schema validator, no orchestrator/state-machine touch —
called by the channel-stage-output ingress handler."* No logic duplicated.

### 4. completion_detector consumes stored output

In the completion path that today calls `ReadLastStageJsonOutput`:

- If `stage_runs.output` is **non-empty** → treat as the stage result directly. It
  was validated at write time, so **skip both the scrape and the
  schema-fail→iterate branch**. (A re-validate is cheap and may be kept as a
  belt-and-suspenders assertion; if it ever fails here that's a logic bug, not an
  agent-format issue → `fail`, no iterate.)
- If **empty** → existing behaviour verbatim: `ExtractJsonBlock` → validate →
  `iterate` (1st miss) → `wait_user` (2nd miss); hard-fail on no session / no JSON.

### 5. Stage prompt contract (`stage_prompts.go`)

For each agent-driven stage (`implementation`, `self_review`, `finalization`):

- Change the closing contract line from *"end your output with a ```json block of
  this shape"* to:
  > **"As your final action, call the `set_stage_output` MCP tool with an `output`
  > object of this exact shape: { … }. If `set_stage_output` is unavailable, also
  > emit the same object as a ` ```json ` block."**
- Keep the identical schema description per stage. The fallback sentence keeps
  non-channel adapters (and any bridge outage) working.

### 6. Origin-403 fix (channel-bridge ingress endpoints)

**Root cause:** `RequireSameOriginForMutations` (`middleware.go:69`) fails closed on
any mutating request without an `Origin`/`Referer` header (line 84). `channel-reply`
is correctly mounted **outside** the protected group (`router.go:357`), but the
permission-request *creation* endpoints the bridge POSTs to
(`/api/permission-requests`, `/api/permission-requests/bulk`) are mounted **inside**
the protected `r.Group` (via `TaskHandler.Mount`, `router.go:262`) that applies
`RequireSameOriginForMutations` (line 202) **and** `RequireAuth` (JWT, line 208).
A server-side `fetch` from the bridge sends neither an `Origin` header nor a JWT
cookie → 403 (`missing Origin header`), the exact failure in the report.

**Fix:** move the **channel-bridge-ingress mutation endpoints** to a bearer-token
group mounted *outside* the JWT/same-origin/loopback protected group, mirroring
`channel-reply`:

- `POST /api/permission-requests` (agent creates a request)
- `POST /api/permission-requests/bulk` (agent bulk-creates)
- `POST /api/channel-stage-output` (new, this design)

These authenticate **by the stage-scoped bearer token only**. This is CSRF-safe:
bearer tokens cannot be forged cross-site, so `RequireSameOriginForMutations` never
protected them — it only ever blocked the legitimate bridge call.

**Stays inside the protected (browser/JWT + same-origin) group:**
- Permission **resolution** endpoints (`grant` / `resolve` / `bulk-resolve`) — these
  are browser-driven UI actions and must keep CSRF + JWT protection.

**Open implementation detail for the plan:** confirm the exact route inventory that
`TaskHandler.Mount` registers and split agent-ingress (bearer) from browser-driven
(JWT) cleanly. If any creation endpoint is *also* called from the browser, keep a
JWT-mounted copy and add the bearer-mounted route for the bridge — do not weaken the
browser path.

## Security

- New endpoint and the moved permission-creation endpoints rely on the stage-scoped
  bearer token (`DASHBOARD_MCP_TOKEN`-class discovery-file token) as sole auth —
  same trust model as `channel-reply` and the MCP endpoint.
- No `Origin`/loopback requirement on these bearer-only paths is intentional and
  safe (server-to-server; bearer unforgeable cross-site).
- `set_stage_output` cannot drive the state machine, cannot grant permissions, and
  writes only the `output` column of the stage_run named by its own injected
  `stageRunId` — no cross-task write surface.

## Testing

- **Endpoint** (`/api/channel-stage-output`): valid payload → 200 + `output` column
  written; schema-invalid → 422 + `error` body; unknown/terminal stage_run → 404;
  request without bearer → 401; request **without `Origin`** → **200** (proves the
  fix; must not 403).
- **completion_detector**: `output` column present → result used, no scrape, no
  iterate; column empty → falls back to scrape path (existing tests stay green).
- **Origin-fix regression**: a bearer-token POST with no `Origin` to
  `/api/permission-requests/bulk` → succeeds (previously 403). Browser-path CSRF
  tests for resolution endpoints stay green (no weakening).
- **Channel tool**: 2xx → success text; non-2xx → MCP error carrying dashboard
  error body.

## Out of scope (possible Phase 2)

- Cross-stage shared task blackboard (key-value store readable/writable by later
  stages). Would build on the same write-tool primitive but needs its own schema,
  visibility, and conflict-resolution design.
