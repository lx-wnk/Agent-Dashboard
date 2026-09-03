# Stage-Run MCP Credentials

**Date:** 2026-09-03
**Status:** Approved design
**Parent:** `2026-08-27-agenticos-capability-gate-design.md`
**Closes:** the limitation `docs/guides/security.md` records after PR #421 — a routine grant
governs the pipeline's memory push and nothing else.

---

## 1. Purpose

Let the server tell which task, and therefore which routine, an MCP call came from.

Today it cannot, and the consequence is concrete: `capability.Decide` ranks six context levels,
but a grant made at `task:` or `routine:` is inert for every one of the 47 MCP tools. A user can
create it, the Grants panel lists it as active, and it decides nothing.

---

## 2. What exists today

Read first-hand while writing this document.

| Piece | Where | State |
|---|---|---|
| Bearer auth for `/api/mcp` | `server/internal/mcp/auth.go:145` `McpAuthMiddleware` | Hashes the token, looks the row up by `key_hash` with `active = true` |
| What auth carries forward | `server/internal/mcp/auth.go:99-104` `MCPAuthInfo{KeyID, Scopes}` | No task, no session, no owner |
| The key row | `server/internal/db/ent/schema/api_key.go:18-24` | `id, name, key_hash, scopes, active, created_at, last_used_at` |
| Key creation | `server/internal/mcp/tools/keys.go:70` (`create_api_key`, scope `keys:manage`) | Human-initiated only |
| Per-spawn MCP config | `server/internal/channelconfig/channelconfig.go:49` `buildConfig` | One entry: `dashboard-channel`, stdio |
| Where the spawner passes it | `server/internal/pipeline/spawner.go:353` `--mcp-config` | Written at `:607`, deleted at `:626`/`:633` |
| `DASHBOARD_MCP_TOKEN` | `server/internal/pipeline/spawner.go:433` → `server/internal/channel/bridge.go:55` | Belongs to the **channel bridge**, not to `/api/mcp`. One `config.MCPToken` value for every spawn |
| Caller-supplied contexts at the gate | `server/internal/memory/authorize.go` `Contexts(scope, extra...)`, `Gate.Authorize(…, extra ...capability.Context)` | Built by PR #421, one production caller (the memory push) |
| Context ranking | `server/internal/capability/decide.go:82-88` | `agent_session 0, task 1, routine 2, application 3, project 4, global 5` |
| Task → routine link | `server/internal/db/ent/schema/task.go` `routine_id` | Written by the scheduler's materializer (PR #421) |

**The gap in one sentence:** a pipeline agent reaches `/api/mcp` only through a machine-wide key
the user registered by hand, so the request that arrives carries no task identity — and
`capability.Decide` drops every grant whose context the request does not name.

**The seam already exists.** The spawner writes a fresh `--mcp-config` per run, and
`Gate.Authorize` already accepts extra contexts. This design fills both rather than adding a
mechanism beside them.

---

## 3. Decisions

### D1 — A key identifies a stage run

Not a task, not an agent session.

A stage run has exactly the lifetime of the agent process that holds the key, which makes
revocation a moment that already exists in the orchestrator. Task and routine are derived from
it (`stage_run.task_id → task.routine_id`), so `task:` and `routine:` grants are both covered
without a second key kind.

*Rejected:* one key per task — it outlives each agent by hours, and revocation only lands when
the whole task ends. *Rejected:* one key per agent session — the session id exists only after
the agent is running, so the key would have to be issued first and attributed afterwards.

### D2 — Identity lives in columns, not in the token

The token stays an opaque `mcp_<hex>`; the row carries the attribution.

*Rejected:* signed claims in the token. It removes a lookup and removes revocation with it —
a contradiction with D4, and a second crypto surface next to the hash table that already works.

*Rejected:* a separate `stage_run_key` table. Its one real advantage — keeping the human-facing
key list free of ephemeral rows — is bought for a `kind` column instead of a second auth path
through `McpAuthMiddleware`.

### D3 — Every issued key gets the same, fixed, narrow scope set

`tasks:read`, `agent:coord`, `memory:read`, `memory:write`, `obsidian:read`, `obsidian:write` —
six scopes.

Only the memory and Obsidian MCP tools (`memory_search`/`memory_write`,
`server/internal/mcp/tools/memory.go`; the four `obsidian_*` tools,
`server/internal/mcp/tools/obsidian.go`) resolve a call through `capability.Decide`. Every other
MCP tool is gated by its scope alone (`mcp.ToolScopeMap`, `server/internal/mcp/auth.go`). So a
scope handed out here is a capability granted outright to the tools it unlocks — the gate will
not narrow it later, because for every tool but those two there is no gate in the call path at
all.

That is why `pipeline:control` and `tasks:write` are excluded, not merely trimmed for caution:
`pipeline:control` reaches `grant_permission`, `resolve_permission_request` and
`approve_all_pending` on scope alone, which would let a spawned agent approve its own spec and
resolve its own permission requests; `tasks:write` reaches `manage_task`'s `grant_permissions`
action, which would let it widen its own permissions. `keys:manage` stays excluded for the
neighbouring reason: an agent that can mint keys can mint one without a stage run and escape its
own attribution.

*Rejected:* a minimal set plus opt-in per spawner — new configuration surface, and an agent
loses tools silently when nobody remembers to grant them. *Rejected:* deriving scopes from
`task.autonomy` — that field steers permission approval today; giving it a second meaning makes
both harder to reason about.

**Corrected 2026-09-03.** The original version of this decision included `pipeline:control` and
`tasks:write`, and argued the opposite of what is written above: "narrowing is the capability
gate's job, per capability and per value," so granting scopes broadly and trusting the gate to
cut them down later was safe. That was false for every MCP tool except the memory and Obsidian
ones — the gate that sentence relied on does not sit in those tools' call path at all — and a
whole-branch review caught it as a privilege escalation before this branch shipped: a spawned,
`spec_gated` agent could reach `approve_spec` and `resolve_permission_request` on scope alone.
The record is kept rather than silently rewritten, because a decision that was wrong and got
caught is worth more to the next reader than one that reads as if it was always right.

### D4 — Two independent expiries

The orchestrator revokes on the transition into a terminal stage-run state, **and** the row
carries `expires_at` (stage timeout plus a buffer).

Each alone leaves a hole. Without the timestamp, a server that dies between spawn and transition
leaves a key valid forever — a failure this project has seen (`lesson_worktree_failure_silent_stall`:
a stuck implementation with no run and no pid). Without the revoke, a stage that finishes early
leaves its key usable for the rest of the window.

### D5 — Interactive sessions get nothing

A session a human started in a terminal keeps using the machine-wide key and resolves without a
task context, exactly as today. The `agent_session` context level stays empty.

Attribution there needs a different mechanism (the session id is not known at spawn time), and
mixing it in would make this change an auth rewrite.

---

## 4. Design

### 4.1 Storage

`api_key` gains three nillable-or-defaulted fields — additive, so ent's non-destructive
auto-migrate handles it:

| Field | Type | Meaning |
|---|---|---|
| `kind` | string, default `"user"` | `"user"` or `"stage_run"`. Discriminates the ephemeral rows |
| `stage_run_id` | string, optional | The stage run this key was issued for. Empty for `kind = "user"` |
| `expires_at` | time, optional | Hard stop. Empty means never, which is what every existing row means |

An index on `stage_run_id` serves revocation; an index on `expires_at` serves the sweep.

### 4.2 Authentication

`McpAuthMiddleware` (`mcp/auth.go:145`) gains one rejection and one field:

- A row whose `expires_at` is in the past is refused with the same message an unknown key gets.
  It is checked in the middleware, not in the tools — an expired key must not reach a handler at
  all, and a distinct error message would tell an attacker that the token was once real.
- `MCPAuthInfo` gains `StageRunID string`. Empty for a user key.

`GetByHash` already filters `active = true` (`api_key_repo.go:58-60`), so revocation needs no
change there.

### 4.3 Issuance and delivery

In the pipeline spawner, immediately before the `--mcp-config` is written
(`spawner.go:604-617`):

1. Mint a token, insert `api_key` with `kind = "stage_run"`, `stage_run_id`, the D3 scope set and
   `expires_at = now + task.stage_timeout_seconds + 300s`. The five-minute buffer covers the
   window between an agent hitting its timeout and the orchestrator recording the transition;
   it is a named constant, not a literal at the call site.
2. `channelconfig` writes a **second** server entry:

```json
{"mcpServers":{
  "dashboard-channel":{"command":"<binary>","args":["channel"]},
  "dashboard-tasks":{"type":"http","url":"http://127.0.0.1:<port>/api/mcp",
                     "headers":{"Authorization":"Bearer mcp_…"}}}}
```

The file is written `0600` inside a per-user `0700` directory (`channelconfig.go:119-131`) — the
same posture the settings file has. It is **not** currently removed after the spawn:
`SpawnStageAgent` returns a `SpawnResult.Cleanup` closure that does remove it
(`spawner.go:650-661`), but nothing in production calls `Cleanup` — the hook was already dead
before this branch (`grep -rn "Cleanup" server --include='*.go'` finds only
`serverapp.go`'s unrelated `comps.Cleanup()`). So one file per stage run accumulates under
`$TMPDIR/dashboard-<uid>/dashboard-channel-mcp-*.json`, and each now carries
`Authorization: Bearer mcp_…` alongside the channel config it always held. Two things bound the
exposure without removing the file: the credential inside it is revoked at the stage run's
terminal write (4.5) regardless of whether the file is ever cleaned up, and it expires on its own
schedule even if revocation is missed. Wiring `Cleanup` into production is out of scope for this
change — see §5.

Issuance failure is **not** fatal to the spawn: the entry is omitted, a warning is logged, and
the agent runs with the channel bridge alone, which is exactly today's behaviour. A spawn that
dies because a credential could not be minted would be a worse outcome than one that runs with
less reach.

### 4.4 Reaching the gate

A helper in the `mcp` package:

```go
// CallerContexts resolves the capability contexts the caller's key implies.
// A user key implies none, which is why an unattributed call behaves exactly
// as it did before this existed.
func CallerContexts(ctx context.Context) []capability.Context
```

It resolves `StageRunID → stage_run.task_id → task.routine_id` and returns
`[{task, <taskID>}, {routine, <routineID>}]`, dropping the routine entry when `routine_id` is
empty — the same rule `memory.RoutineContext` already applies, and for the same reason: a context
with an empty ref matches every grant stored with an empty `ContextRef`.

Every MCP tool that calls `Gate.Authorize` passes the result as `extra`. That is the variadic
parameter PR #421 added; no gate change is needed.

The resolution is one joined read per call. It is cached per request, not per process: a key
revoked mid-run must stop working on the next call, not when a cache expires.

### 4.5 Lifecycle

| Moment | What happens |
|---|---|
| Stage run starts | Key issued (4.3) |
| Stage run reaches a terminal state | Orchestrator revokes by `stage_run_id` (`active = false`) |
| `expires_at` passes | Middleware refuses it, whatever the orchestrator did |
| Sweep | Expired `kind = "stage_run"` rows are deleted, not tombstoned |

The sweep deletes rather than deactivating because these rows carry no audit value: they name a
stage run that has its own record, and keeping one per stage run per retry forever turns the key
table into a log. User keys are still soft-deleted, unchanged.

### 4.6 What the human-facing surfaces show

`ApiKeyRepo.List` filters to `kind = "user"`, so the Settings panel and `list_api_keys` keep
showing what a person created. A stage run's key is visible where it belongs — on the stage run —
not in a list that would otherwise grow by one row per retry.

---

## 5. Explicitly out of scope

- **`pipeline/spawner.go`'s allow-list.** `resolvePermissionDecisions` builds synthetic grants
  from `task_permissions` at one fixed context and never reads the `grants` table. Making the
  `--allowedTools` rendering read real grants is a separate change with its own failure modes.
- **Interactive sessions** (D5).
- **The `agent_session` context level**, which stays without a producer.
- **The channel bridge's own token.** `DASHBOARD_MCP_TOKEN` keeps identifying the dashboard to
  its callback bridge. Renaming or splitting it is a different cleanup.
- **Wiring `SpawnResult.Cleanup` into production** (4.3). It already removes the temp MCP config
  file and settings file when called, but nothing calls it — that gap predates this branch. Now
  that the file can carry a bearer credential, leaving it unremoved is a marginally worse version
  of a pre-existing gap, not a new one this change should absorb fixing. It is bounded on its own:
  the credential inside is revoked at the stage run's terminal write and expires regardless of
  whether the file is ever deleted, so the file going unremoved is not itself a live-credential
  exposure. Wiring `Cleanup` in is a separate, single-purpose change against the existing gap.

---

## 6. Testing

The decisive test mirrors the live run that verified PR #421: one grant, two callers, one
difference.

| Test | Asserts |
|---|---|
| Grant at `routine:<id>`, call with a stage-run key of a task that routine fired | allowed |
| Same grant, same tool, call with the machine-wide key | denied |
| Same grant, stage-run key of a task with `routine_id` empty | denied |
| Key whose `expires_at` has passed | 401 from the middleware, handler never runs |
| Key revoked by `stage_run_id` | 401 on the next call |
| `CallerContexts` with no auth, and with a user key | returns nothing, both times |
| Issuance failure at spawn | spawn proceeds, config carries only `dashboard-channel` |

A key must never be logged. One test asserts that the issued token does not appear in the
spawn's log output, because the config path and the arg list are both logged today.

---

## 7. Risks

| Risk | Mitigation |
|---|---|
| A leaked stage-run token reaches every tool its scopes unlock — for most tools that is the whole mitigation, since only memory and Obsidian calls pass through the gate afterward (D3) | The scope set itself is deliberately narrow (six scopes, `pipeline:control`/`tasks:write`/`keys:manage` excluded); short `expires_at`; revoke on terminal state; loopback-only binding as today |
| Key-table growth: one row per stage run per retry | Sweep deletes expired ephemeral rows (4.5); the list filters them out (4.6) |
| A per-call join on the hot path | One indexed read, cached per request; measure before adding anything cleverer |
| Two truths about "what may this agent do" — scopes and grants, for the tools that reach the gate at all | D3's scope set is the single narrowing decision for every tool the gate never sees; grants only add a second, independent narrowing for the memory and Obsidian tools specifically |
