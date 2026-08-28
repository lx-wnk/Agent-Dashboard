# AgenticOS — Capability Gate

**Date:** 2026-08-27
**Status:** Approved design
**Stage:** MVP (unit K2)
**Parent:** `2026-08-27-agenticos-overview-design.md`
**Implements:** decision D6 (one policy model, several enforcement adapters)

---

## 1. Purpose

Answer one question in one place: *may this happen?* — for agents, applications and routines
alike, with an answer that survives being asked from four different call sites.

---

## 2. What exists today

Four mechanisms carry the word "permission". They share no type, no table and no decision
function.

| # | Mechanism | Storage | Decides for | Failure posture |
|---|---|---|---|---|
| 1 | Static Bash/tool allow-list — `server/internal/permissions/allowlist.go` | none (compiled map + one process-global mutable extras map) | what a grant may even contain | silently drops the entry at spawn (`pipeline/spawner.go:131`) |
| 2 | DB round trip — `task_permission`, `permission_request`, `permission_preset` | SQLite | orchestrated tasks | pending forever; no wall-clock TTL |
| 3 | Hook bridge — `server/internal/api/hooks/permission.go` | in-memory maps only | hand-started terminal sessions | **fails open** (`permission.go:362-372`) |
| 4 | MCP scopes — `server/internal/mcp/auth.go` | `api_key.scopes` JSON | who may call which RPC | fails closed |

Two further gates exist with a third and fourth posture: the edit gate fails **closed**
(`api/hooks/handler.go:272-276`), and the ACP gate fails **closed** on every path
(`server/internal/acp/gate.go:35,51,65,89-94`).

### 2.1 The static allow-list, precisely

`IsSafeBashPattern` (`permissions/allowlist.go:144-186`) takes `filepath.Base` of the first field,
lowercases it, strips a trailing `*`, and looks it up in `safeBashCommands`
(`allowlist.go:21-85`) — 60-odd allowed commands. Exactly **one** command is an explicit `false`:

```go
"curl":          false, // explicitly false for documentation — never safe in this context
```

Everything else dangerous (`sudo`, `bash`, `sh`, `xargs`, `wget`, `nc`, `rm`, `chmod`, `ssh`) is
blocked only by *absence*. A second regex (`allowlist.go:121-135`) then rejects command
substitution, process substitution, backticks, `eval`, hex and unicode escapes, pipes, `&&`, `;`,
redirection, a lone `&`, and newlines.

The real spawn-time gate is `BuildAllowList` (`pipeline/spawner.go:98-151`), which filters on
granted → not expired → allowed tool → non-empty pattern → manual override → git-push gate →
safe pattern, and then writes `.claude/settings.json` with `_dashboardManaged: true`
(`spawner.go:367-391`).

### 2.2 The grant tables, precisely

`task_permission` (`schema/task_permission.go:14-40`) has `expires_at`, `manual_override`,
`granted`, `pre_approved`, `decided_by`, `decided_at`. `permission_request`
(`schema/permission_request.go:14-38`) has a nullable `outcome` where NULL means pending.
`permission_preset` (`schema/permission_preset.go:12-29`) is `(user_id, project_cwd, tool,
pattern)` with a unique index that **does not hold**, documented in the schema itself:

```go
// NOTE: SQLite treats two NULL values as distinct, so this UNIQUE index
// will not prevent duplicate (NULL user_id, cwd, tool, NULL pattern) rows.
```

---

## 3. What cannot be expressed today

Each item is a finding from reading the code, not a supposition.

**G1 — Only tool-shaped capabilities exist.** `allowedToolNames` (`allowlist.go:271-290`) is a
closed set of Claude Code tool names plus two `mcp__dashboard-channel__*` entries. "Send an
email", "spend $X", "publish a release" cannot be named at all. This alone blocks every
Application in the roadmap.

**G2 — Two vocabularies that never meet.** `permissions.allowedToolNames` is task-scoped,
per-tool, pattern-bearing. `mcp.ToolScopeMap` (`mcp/auth.go:18-50`) is key-scoped, per-RPC,
pattern-free. No shared type, no shared table, no shared enforcement point.

**G3 — Pattern matching is exact string equality.** `isCovered`
(`api/tasks/permission_request_routes.go:548-566`) and `presetCovers` (`:571-587`) implement: nil
pattern is a wildcard, otherwise `*a == *b`. `Bash(git status)` does not cover
`Bash(git status --short)`. Only the deny side understands `prefix:*` and `domain:`
(`claudesettings/deny.go:42-60`), and the pipeline never consults it.

**G4 — Expiry is half-plumbed.** `expires_at` exists on `task_permission` and is honoured in two
reads (`repo/permission_repo.go:127-130`, `pipeline/spawner.go:111-113`). But **every human
approval path drops it**: `grantOverrideEntries` (`api/tasks/permission_service.go:103-132`),
bulk-resolve (`permission_request_routes.go:477`) and single-resolve (`api/tasks/handler.go:932`)
all build a `GrantEntry` without `ExpiresAt`. Clicking Allow always produces a permanent grant.
`permission_preset` and `api_key` have no expiry column at all.

**G5 — No rate limits or quotas anywhere in the model.** The only limiter is a per-IP HTTP
limiter (`api/router.go:278-281`). `maxHoldsPerSession = 8` (`api/hooks/permission.go:53`) is a
concurrency cap whose overflow fails open.

**G6 — `outcome` is a free-form string with five live values and a dialect mismatch.** Observed
writes: `granted`, `denied`, `approved`, `expired`, plus NULL for pending. The ACP gate treats
anything other than `"granted"` as denied (`pipeline/stage_handlers.go:342-348`), so
`ApproveAllPending`'s `"approved"` (`api/tasks/permission_service.go:64`) **reads as a denial**.
This is a live defect, not a design gap.

**G7 — No identity on a decision.** `task_permission.decided_by` and `decided_at` are written
nowhere outside generated ent code. `permission_request` has no `resolved_by`. Audit rows for
auto-approval, bulk approval and MCP grants all pass `userID = nil`. "Who allowed this" is
unanswerable except on one REST path.

**G8 — No negative grant, no revocation.** `granted` defaults to `false` but **no code path ever
writes `false`** — every writer hardcodes `true` (`repo/permission_repo.go:107`,
`mcp/tools/control.go:339,396`). Removal is a hard `DELETE`. A preset or inherited grant cannot
be overridden by a narrower deny.

**G9 — Presets are unconditional standing allows.** No expiry, no `granted` flag, and
`ListForCwd` always unions global rows in (`repo/permission_preset_repo.go:151-163`), so a global
preset cannot be narrowed by a user-scoped rule.

**G10 — The Bash extras list is a process-global mutable singleton.**
`extraBashCmds` (`allowlist.go:89-92`) is one map for the whole server, fed from one unscoped
config key, set at boot (`serverapp/di.go:550-551`) and on config write
(`api/tasks/pipeline_config_routes.go:219-220`). It cannot be scoped per project, task, agent or
user — and because only `curl` is an explicit deny, extras can enable `sudo`, `bash`, `sh`.

**G11 — `manual_override` is Bash-only, boolean, unscoped and unaudited.** Honoured at exactly one
line, inside the `Tool == "Bash"` branch (`pipeline/spawner.go:122-126`). For every other tool it
is stored and ignored.

**G12 — Four contradictory failure postures with no way to declare intent.** Fail-open hook
bridge, fail-closed edit gate, fail-closed ACP gate, silent-drop spawn filter. Nothing in any
schema says which posture a given decision should take.

**G13 — Dead fields.** `pre_approved` is written in four places and read nowhere.
`decided_by`/`decided_at` are never written. `WriteToolNames` (the exported slice) has zero
non-test readers.

**G14 — Grant writes are not atomic outside one path.** `BulkGrantPermissions`
(`repo/permission_repo.go:98-119`) is a serial loop returning partial results *and* an error.
Only REST bulk-grant wraps it in a transaction.

---

## 4. Design

### 4.1 Capability

A capability is a named, coarse permission that outlives the tools implementing it.

```
capability: id, name, class, enforceable_by, requires_pattern, reversible, description
class          ∈ { tool, reach, resource, spend }
enforceable_by ⊆ { server, spawn, hook }
```

- `tool` — maps onto a Claude Code tool name. `Bash`, `Edit`, `WebFetch`.
- `reach` — an Application action. `mail.send`, `obsidian.delete`, `calendar.write`.
- `resource` — an internal resource action. `memory.write`, `skill.publish`.
- `spend` — a budget dimension. `tokens`, `cost`.

`enforceable_by` closes G12 and is the honest core of D6: a `tool` capability is enforceable at
spawn and, weakly, at the hook; a `reach` capability is enforceable server-side and **only**
server-side. The UI states this where the grant is made. A capability that claims an enforcement
point it does not have is the one failure mode this design must not ship.

`reversible = false` marks actions that cannot be undone (`mail.send`, `obsidian.delete`). Those
never auto-grant from a preset alone; they require an explicit grant with an explicit scope.

### 4.2 Grant

```
grant: id, capability_id, context_kind, context_ref, pattern, mode,
       limit_count, limit_window_seconds, expires_at, granted_by, granted_at,
       revoked_at, reason, node_id
context_kind ∈ { global, project, task, routine, application, agent_session }
mode         ∈ { allow, deny, ask }
```

Four gaps close here at once:

- `mode = deny` closes **G8**: negative grants become expressible, and a narrow deny beats a broad
  allow by specificity.
- `revoked_at` closes the tombstone half of **G8**: revocation stops being a `DELETE`.
- `limit_count` + `limit_window_seconds` close **G5**.
- `granted_by` + `granted_at` close **G7**, and they are **required**, not nillable — the mistake
  that produced `decided_by` was making identity optional.

`expires_at` already exists conceptually; **G4** closes not by adding a column but by making every
approval path carry it. A default expiry per capability class is applied when the human does not
choose one, so "Allow" stops silently meaning "forever".

### 4.3 Resolution

Specificity wins, then deny wins, then expiry excludes:

1. Collect every grant whose `capability_id` matches and whose context contains the request
   context.
2. Drop expired and revoked grants.
3. Rank by context specificity: `agent_session` > `task` > `routine` > `application` > `project` >
   `global`.
4. At the most specific level that has any grant, `deny` beats `allow` beats `ask`.
5. No grant at any level → the capability's default: `ask` for `tool`, `reach`, and `resource`;
   `deny` for `spend` above the budget; `deny` for any class the resolver does not recognise, so a
   typo'd or future class fails closed instead of silently becoming a prompt a human clicks
   through.

Pattern matching becomes a small, tested algebra — exact, prefix (`git status*`), and domain
(`domain:docs.example.com`) — closing **G3**. It lives in one package and is used by both the
allow and deny sides, which the deny parser already half-implements
(`claudesettings/deny.go:42-60`) and which is therefore a DRY consolidation, not a new invention.

### 4.4 One decision, several enforcers

```
Decision { Effect: allow|deny|ask, GrantID, Reason, Enforceable bool }

type Decider interface {
    Decide(ctx, CapabilityRequest) Decision
}

type Enforcer interface {          // one per interception point
    Point() EnforcementPoint       // server | spawn | hook
    Enforce(ctx, Decision) error
}
```

Three enforcers, all consuming the same `Decision`:

- **server** — intercepts Application calls in-process. Complete coverage, synchronous.
- **spawn** — translates the decision set into the allow/deny lists written into
  `.claude/settings.json` (`pipeline/spawner.go:367-391`). Static for the process lifetime, which
  is why a mid-run grant still needs `ResumeFromUser`.
- **hook** — answers the `PreToolUse` hold. Bounded by a 25 s hold against a 28 s curl budget
  against a 30 s Claude Code timeout (`api/hooks/permission.go:19-60`, `cmd/serve/hooks.go:16-20`).
  It stays **fail-open**, deliberately: the alternative is a dashboard outage silently blocking
  every hand-started session. That posture is now declared in `enforceable_by` rather than being
  an implicit property nobody can see.

The `ask` effect routes to the existing request round trip, which keeps working: SSE broadcast
(`api/tasks/handler.go:249-260`), roster enrichment (`agentbroadcast/enricher.go:89-124`), and the
attention band (`src/utils/attention.ts:38-39`).

### 4.5 Normalization carried by this unit

These are not optional cleanups; the design cannot be correct while they stand.

| Item | Change | Closes |
|---|---|---|
| `outcome` values | One enum: `granted`, `denied`, `expired`, `revoked`. `approved` is migrated to `granted` | **G6**, including the live ACP defect |
| `decided_by`/`decided_at` | Wired on every path, or dropped. They are wired | G7, G13 |
| `pre_approved` | Dropped — four writers, zero readers | G13 |
| `WriteToolNames` | Dropped — the exported slice has no non-test reader; `IsWriteTool` is the SSOT | G13, DRY |
| `permission_preset` unique index | `user_id` and `pattern` use the empty-string sentinel, matching `pipeline_config.project_id` (`schema/pipeline_config.go:12-13,21-23`) | G9 |
| Bash extras | Scoped to a context like every other grant, not a process global | G10 |
| Grant writes | One transactional path, used by every caller | G14 |

The `outcome` migration is the one with a user-visible failure today, so it ships first and
carries a regression test asserting that an approve-all decision reaches an ACP agent as granted.

---

## 5. Migration

The gate is an **extension of existing tables**, not a replacement. Sequence:

1. Add `capability` and `grant` tables. Nothing reads them yet.
2. Backfill: every `task_permission` row becomes a `grant` with `context_kind = task`,
   `capability_id` resolved from the tool name, `mode = allow`. Every `permission_preset` row
   becomes a `grant` with `context_kind = project`.
3. Route `BuildAllowList` through the `Decider`. Behaviour must be identical; a golden test over
   a fixture permission set proves it before the switch.
4. Route the hook bridge through the same `Decider`, keeping its own posture.
5. Normalize `outcome` and wire identity.
6. Delete the duplicated coverage logic (`isCovered`, `presetCovers`) in favour of the shared
   pattern algebra.

Steps 1–3 are additive. Step 6 is the first destructive one, and it comes last on purpose.

> **Migration hazard.** New tables with unique indexes must pre-create the index under ent's exact
> generated name before auto-migrate runs, or SQLite's 12-step table rebuild fails on existing
> databases with `NOT NULL constraint failed` (`server/internal/db/client.go:429-435`, PR #207).
> Read the generated name from `server/internal/db/ent/migrate/schema.go` — a differently named
> pre-seeded index does not prevent the rebuild.

---

## 6. Failure modes

| Situation | Behaviour |
|---|---|
| Decider unavailable (bug, panic) | `deny` for `server` and `spawn`; the hook stays fail-open. Panics in the MCP layer are already recovered into JSON-RPC errors (`mcp/jsonrpc.go:103-114`) |
| Grant expires mid-run | The next decision returns `ask`, producing a permission request. The task parks rather than failing |
| Limit exhausted | `ask`, with the limit named in the reason. Never a silent denial |
| Two grants tie at the same specificity | `deny` wins. Ties are logged, because a tie usually means a modelling mistake |
| Capability unknown | `deny`, and the tool registration fails at construction — the same stance MCP already takes, where a missing `ToolScopeMap` entry panics `Register` (`mcp/registry.go:51-61`) |
| Nobody is watching | The request persists. Web push is **not** wired today (`webpush/service.go:89` has no production callers); until it is, an unanswered request waits for a browser |

---

## 7. Testing

- **Resolution table** — the core: capability × context × mode × specificity × expiry × limit.
  This is where a wrong answer is a security bug, so it is exhaustive and table-driven.
- **Golden parity** — the same fixture permission set through the old `BuildAllowList` and the new
  `Decider` must produce byte-identical allow and deny lists. This gates step 3 of the migration.
- **Pattern algebra** — exact, prefix and domain matching, including the cases the current code
  gets wrong: `Bash(git status)` must not cover `Bash(git status --short)` unless the pattern says
  so.
- **ACP regression** — approve-all on an ACP-backed stage must reach the gate as granted. This
  test would fail today.
- **Posture** — one test per enforcer asserting its declared failure posture, including an
  explicit assertion that the hook adapter fails open and says so.
- **Identity** — no grant can be written without `granted_by`.
- **Migration** — a database seeded with the old tables migrates to grants with identical
  effective permissions; a fresh database and a pre-index database both open twice without error,
  mirroring `server/internal/db/client_test.go:191-235`.

---

## 8. Deferred

| Item | Why not now |
|---|---|
| Cross-node grant sync | V2, with the node registry |
| Team roles | Deferred with everything team-shaped |
| Automatic risk classification of new capabilities | Needs data the system does not have yet; capabilities are declared by hand |
| Replacing the static Bash allow-list with a parser | The allow-list is crude but conservative. Replacing it is a security change that deserves its own spec |
| Web push for pending requests | Real gap, but it is a notification feature, not a gate feature |
