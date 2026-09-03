# Security

The dashboard reads sensitive Claude session data from your machine. It is designed local-first and defensive by default.

- **Loopback only** — the server binds to `127.0.0.1` and is never exposed to the network. (Multi-machine mode is opt-in and expects a VPN/SSH tunnel — see [Configuration](configuration.md).)
- **Local-trust auth bypass** — when on loopback with no GitHub OAuth configured, all API requests are allowed without login. This is safe for a single-user developer machine. Any process with access to `127.0.0.1:13120` can create API keys, spawn agents, and read session data — so for shared or multi-user machines, configure GitHub OAuth (`DASHBOARD_GITHUB_CLIENT_ID` + `DASHBOARD_GITHUB_CLIENT_SECRET`).
- **Ephemeral JWT secret** — `DASHBOARD_JWT_SECRET` is auto-generated if unset (sessions reset on restart). Set a stable value for production.
- **Hashed tokens** — bearer tokens are SHA-256 hashed before storage; raw tokens are shown once and never persisted in plaintext.
- **Authenticated channel replies** — per-agent bearer tokens authenticate channel replies.
- **Sanitized output** — markdown is sanitized via DOMPurify before any `v-html` rendering.
- **Rate-limited spawns** — user-initiated spawns are rate-limited (default 5/min, configurable).
- **Dangerous-command block-list** — a block-list in the spawner rejects `curl`/`wget`/`eval`/shell-substitution in agent tool grants.
- **`git push` hard-blocked** by default even when granted; opt out with `DASHBOARD_ALLOW_GIT_PUSH=true` or per-task `metadata.allowGitPush=true`.

See also the [Privacy policy](../../PRIVACY.md).

## Capabilities and the permission gate

A **capability** is a named permission coarser than a raw tool name — `Bash`,
`WebFetch`, or a future action like `mail.send` that has no Claude Code tool
behind it at all. Claude Code's own permission system can only grant or deny
a *tool*; it has no unit for "this agent may reach the network" or "this
agent may spend up to $5", so those questions had nowhere to live. Every
capability also carries a **class** (`tool`, `reach`, `resource`, `spend`)
that decides its default when nothing has granted or denied it explicitly:
`tool`/`reach`/`resource` default to asking, `spend` and any unrecognised
class default to deny.

One pure **Decider** (`server/internal/capability`) resolves a capability
request to allow / deny / ask by ranking the grants that apply to it. The
model defines six context levels, from most to least specific — agent
session, task, routine, application, project, global — and the Decider
ranks grants across all six, with a deny beating an allow beating an ask
within whichever level wins. Grants are rows with an optional expiry and
rate limit, bound to one of those contexts.

**State this plainly: that ranking is now partially exercised, not entirely
theoretical.** `SpawnEnforcer` (`server/internal/pipeline/spawner.go`),
which resolves every spawned task-pipeline agent's permissions, still never
reads the `grants` table — it builds a `GrantView` on the fly from each
granted `TaskPermission` row, pinned to one fixed, synthetic task-level
context. A migration does backfill real rows into `grants` (`task` context
from `task_permissions`, `project` context from `permission_presets`, both
idempotent — see [`PRIVACY.md`](../../PRIVACY.md)). The memory store's
`internal/memory.Authorize` (gating `memory.read`/`memory.write` for both
`/api/memory/*` and the `memory_search`/`memory_write` MCP tools — see
[`CHANGELOG.md`](../../CHANGELOG.md)) is now a real, production caller of
`GrantRepo.ListForCapability`, and it resolves against up to two context
levels — a memory scope's own context (project or application) plus the
global fallback, or just the global level alone when the request itself is
global-scoped — rather than one fixed level. So the six-level hierarchy has
a second level to rank against for memory today; and a third, `routine`, for
the subset of those calls that a routine started: a task materialized by a
schedule carries that schedule's id in `task.routine_id`, and the pipeline's
memory push passes it to `memory.Gate.Authorize` as an extra context, so a
grant made with `--scope routine:<schedule id>` decides that push. **It is no longer only the push.** Every pipeline stage run is now spawned
with a second, ephemeral `dashboard-tasks` MCP server entry alongside
`dashboard-channel` — a per-stage-run bearer credential
(`server/internal/mcp/stagekey.go`, `StageKeyIssuer.Issue`), distinct from
`DASHBOARD_MCP_TOKEN`. That env var is still a single value taken from
config and handed to every spawn — it has not changed and still identifies
no task — but it was never the credential that reaches `/api/mcp` in the
first place: it belongs to the channel bridge alone
(`server/internal/channel/bridge.go:55`), authenticating the three callback
tools (`dashboard_reply`, `request_permission`, `set_stage_output`), a
different endpoint from the one the Decider's grants govern. The stage-run
key's row carries `stage_run_id`, and `mcp.CallerResolver.Contexts`
(`server/internal/mcp/caller.go`) resolves it through
`stage_run.task_id → task.routine_id` into the same `[{task, <id>},
{routine, <id>}]` pair the memory push builds, passed to `Gate.Authorize` as
extra context on every call. It is wired into the memory MCP tools
(`memory_search`, `memory_write`) and the four `obsidian_*` tools
(`server/internal/mcp/tools/memory.go`, `.../tools/obsidian.go`) — the tools
that already run through the capability gate — so a `task:` or `routine:`
grant now decides those calls too, not just the automatic push.

A stage-run key carries two independent expiries, because either one alone
leaves a hole. The orchestrator revokes it (`active = false`, via
`RevokeForStageRun`) the moment its stage run reaches a terminal state; and
independently, the row's `expires_at` (the stage's timeout plus a
five-minute buffer, `mcp.StageKeyTTLBuffer`) is enforced by `GetByHash`
(`server/internal/db/repo/api_key_repo.go`) itself, refusing an expired row
with the same "Invalid or revoked API key" message an unknown token gets —
an expired key never reaches a tool handler, and a distinct message would
tell an attacker the token was once real. The buffer covers a server that
dies between spawn and the orchestrator recording the transition; without it
that stuck run's key stays valid forever (the same class of gap as
`lesson_worktree_failure_silent_stall`). A background sweep
(`server/internal/mcp/sweep.go`, `SweepExpiredKeys`, started alongside the
cost-history importer and the drift-detection scanner) hard-deletes expired
rows hourly — they are deleted, not tombstoned, because they carry no audit
value beyond the stage run's own record.

Every issued key gets the same fixed scope set — `tasks:read`, `agent:coord`,
`memory:read`, `memory:write`, `obsidian:read`, `obsidian:write`. That set is
narrower than the first draft, which also included `pipeline:control` and
`tasks:write`. **The scope set is the actual boundary for everything except
memory and Obsidian.** Only `memory_search`/`memory_write`
(`server/internal/mcp/tools/memory.go`) and the four `obsidian_*` tools
(`server/internal/mcp/tools/obsidian.go`) call `Gate.Authorize` and resolve
against the capability gate's grants; every other MCP tool checks nothing
beyond `ToolScopeMap` (`server/internal/mcp/auth.go`) — a scope in the set
above hands out that scope's tools outright, with no gate to narrow it
later. `pipeline:control` reaches `grant_permission`,
`resolve_permission_request` and `approve_all_pending` on scope alone, which
would let a `spec_gated` agent approve its own spec and resolve its own
permission requests; `tasks:write` reaches `manage_task`'s
`grant_permissions` action, which lets a caller widen its own permissions.
Both are excluded for that reason. `keys:manage` is excluded the same way an
agent able to mint its own keys could mint one with no stage run and escape
its own attribution.

Agent session still has no `grants`-table reader — an interactive session
started by hand keeps using the machine-wide key and resolves with no task
context, so the `agent_session` context level stays without a producer.
`SpawnEnforcer` still bypasses the `grants` table entirely via the mechanism
above; that allow-list reading real grants instead of synthesizing them from
`task_permissions` remains a separate, unstarted change.

A capability also declares which enforcement points it can be applied at,
and a resolved decision carries that same list forward — but the two wired
enforcement points treat it differently. `SpawnEnforcer` renders a shared
batch of decisions into one allow-list, so it must filter out capabilities
that aren't its concern: a point with no standing over a capability (e.g.
one only enforceable at the server) does not act on it there just because
the Decider returned "allow". `ServerEnforcer` deliberately does the
opposite and ignores `Enforceable` entirely: it is the complete backstop
judging one decision at its point of use — the other two points are each
incomplete in their own way (the hook fails open on timeout, the spawn
point is static and cannot ask) — so it enforces every decision handed to
it regardless of where else that capability is declared enforceable
(`server/internal/capability/enforcer_server.go`, pinned by
`TestServerEnforcerIgnoresEnforceable`).

That one Decider is shared by all three enforcement points below, but they
are not otherwise identical — each has a different guarantee, and one of
them (the hook) cannot be bypassed by a network outage without also being
able to give up on purpose:

| Enforcement point | What it covers |
|---|---|
| **Server** (`ServerEnforcer`) | The only point with complete coverage once a call site invokes it — nothing routes around it, and it cannot time out into an implicit allow. It is implemented and tested (`server/internal/capability/enforcer_server.go`) and has real production callers: every `/api/memory/*` request, both memory MCP tools (`memory_search`, `memory_write`), the task pipeline's automatic memory push into a stage's spawn prompt, the `POST /api/obsidian/index` trigger, and the four `obsidian_*` MCP tools, all through `memory.Gate.Authorize` (`server/internal/memory/authorize.go`). **An `Asker` is now wired to it — but only when authentication is on.** With `DASHBOARD_AUTH` in any real mode, `server/serverapp/di.go` builds a `serverask.Asker` (`server/internal/serverask/asker.go`): an `ask` decision holds the caller's request open, the pending ask rides the agent SSE frame into the dashboard's triage band, and **Allow**/**Deny** there posts to `POST /api/capabilities/decisions/respond`, which releases the held call. Under `DASHBOARD_AUTH=none` no asker is constructed and an `ask` still fails closed to `ErrAskRequired` — deliberately: the respond route is not mounted in that mode either, so "a human decided" would reduce to "any local process decided". Only three of the call sites that reach the gate may block for a human — the HTTP memory handler (`server/internal/api/memory/handler.go`), the memory MCP tools (`server/internal/mcp/tools/memory.go`), and the four `obsidian_*` MCP tools (`server/internal/mcp/tools/obsidian.go`, sharing the same `Asker` the memory tools use — an agent is genuinely waiting on the tool response either way). The pipeline's memory push (`server/serverapp/di_pipeline.go`) and the Obsidian index trigger's three checks — `memory.write` on the target space, `obsidian.search`, `obsidian.read` (`server/internal/apps/obsidian/index.go`, called from `POST /api/obsidian/index`) — construct their own `Gate` with no `Asker` on purpose: nothing is waiting on either, so an unanswerable ask must deny rather than stall a spawn or a background index run. The two gates do not check the same set: `IndexNotes` checks all three — `memory.write`, `obsidian.search`, `obsidian.read` — together on every run (`server/internal/apps/obsidian/index.go:66,82,92`), while each MCP tool checks exactly one capability per call — `obsidian_read` only `obsidian.read`, `obsidian_search` only `obsidian.search`, `obsidian_write` only `obsidian.write`, `obsidian_delete` only `obsidian.delete` — and none of the four ever checks `memory.write`. What the two gates share is only the asker asymmetry: a fresh install denies the index trigger outright on a missing grant, while the identical kind of `ask` decision through an MCP tool call can surface as a card instead. An ask nobody answers within 25 seconds (`askHoldTimeout`) denies, and so does an ask still pending when the server restarts: a pending ask lives only in memory, is never persisted, and the caller's request fails closed when it disappears. A `deny` decision never reaches the asker at all — `ServerEnforcer.Enforce` returns `ErrDenied` for `EffectDeny` before the ask branch — so nobody can click an explicit denial into an allow; the only decision a human sees is one the Decider itself resolved to `ask`. One such case is a rate limit: an `allow` whose winning grant is exhausted is downgraded to `ask` by `Enforce`, with a reason naming the limit, so with an asker wired a human can now be asked to permit one use past a cap they set themselves. The other half of the old gap closed separately: the `agent-dashboard grants` CLI now creates standing grants, so a `memory.read`/`memory.write` request can be allowed once and for all rather than one call at a time. That is no longer the only surface for it — `GET`/`POST /api/grants` and `DELETE /api/grants/{id}` (`server/internal/api/grants/handler.go`) and **Settings → Grants** create and revoke `grants` rows too — but a fresh install still denies every memory call until a grant exists through one of these or an ask is answered by hand. |
| **Spawn** (`SpawnEnforcer`) | Complete for every agent the dashboard's task pipeline spawns itself: each granted `TaskPermission` is resolved through the Decider and rendered into that process's `--allowedTools` list (`server/internal/pipeline/spawner.go`). It cannot ask — the file is written before the process starts — so an `ask` decision is simply omitted, and the agent falls back to its own permission prompt for that call. |
| **Hook** (`HookEnforcer`) | The only point that can reach a session you started by hand, because it rides Claude Code's own `PreToolUse` hook instead of a start-time handshake. **It fails open on a timeout, by design** — see below. |

### Creating and revoking grants

The `agent-dashboard grants` CLI, `GET`/`POST /api/grants` plus
`DELETE /api/grants/{id}` (`server/internal/api/grants/handler.go`), and
**Settings → Grants** all create and revoke `grants` rows (the boot
backfill migration writes rows too, but nobody invokes it by hand). Only
the CLI opens the SQLite database directly, the same way
`agent-dashboard settings` and `agent-dashboard plugins` do, so it is the
one that still works while the server is down.

```bash
agent-dashboard grants add memory.read --pattern '*' --scope global --mode allow
agent-dashboard grants list --capability memory.read
```

`add`, `list`, `revoke`, and `capabilities` are the four subcommands —
`capabilities` lists the grantable names, `list` lists existing grants
(newest first, optionally filtered by capability, with an `ENFORCEMENT`
column saying whether anything reads that capability's grants and a
`GRANTED-BY` column saying who created each one), and `revoke <id>`
tombstones a grant (`revoked_at`/`revoked_by` set) rather than deleting it,
so the audit trail survives. `revoke` refuses a grant that is already
revoked, so a second call can never overwrite who revoked it first.

`--pattern` is required on `add`: pass `--pattern '*'` to cover every value,
or a specific pattern (a prefix pattern ends in `*`, e.g. `'git status*'`).
`*` is stored literally and behaves exactly like the empty wildcard the
schema already uses, because the matcher routes a trailing `*` through a
prefix test with an empty prefix — the requirement exists so the widest grant
the system can express has to be asked for out loud, not arrived at by
accepting three defaults.

Two further inputs are rejected rather than stored inert: `--expires-in` must
be a positive duration (a zero or negative one used to create a grant that
was already expired), and a non-`global` `--scope` must carry a ref —
`--scope project:/home/me/app`, not bare `--scope project`. A scope with an
empty ref can never match anything, because the caller's context chain
collapses an empty ref to `global` and the Decider's scope test is an exact
match on kind *and* ref.

`add` also warns on stderr when nothing reads the grant it just wrote. The
`grants` table has exactly one production reader — `internal/memory.Authorize`
via the server enforcer — so a grant for a capability that is not enforceable
at the **server** point (which is every Claude Code tool name, `Bash`
included) is recorded and read by nobody. The grant is still created and the
exit code is unchanged; the warning only says it will take effect once a
reader exists.

#### Specificity is resolved before mode

**A narrower grant wins outright, whatever its mode.** `capability.Decide`
ranks matching grants by context specificity first (agent session → task →
routine → application → project → global) and the most specific level with
*any* matching grant decides on its own; `deny` beats `allow` beats `ask`
only among grants at that same level. A broader grant never gets a vote once
a narrower one matches.

So these two grants do **not** compose the way they read:

```bash
agent-dashboard grants add memory.read --pattern '*' --scope global --mode deny
agent-dashboard grants add memory.read --pattern '*' --scope project:/home/me/app --mode allow
```

Inside `/home/me/app` the request is **allowed**. The global `deny` is not a
safety net over the project `allow` — it only applies where no project-,
application-, task-, routine-, or session-level grant matches. To close a
capability off in one project, revoke the project-level grant; a global deny
cannot do it. (Pinned by "a global deny does NOT overrule a task allow" in
`server/internal/capability/decide_test.go`.)

`--mode` accepts `ask` and stores it without complaint, but **the server
enforcer has no `Asker` wired to it** (see the table above), so an `ask`
grant resolves to a denial there just like an unset one. `allow` is the only
mode that actually opens the gate at the server point today.

### The hook point's three outcomes

A hook call that gets no explicit decision looks the same from the terminal
in every case, but `HookEnforcer.Point()` (`server/internal/api/hooks/permission.go`)
distinguishes three situations that must not be flattened into one:

- **Actively vetoed** — the call matches one of your own `permissions.deny`
  rules. It is held and offered in the dashboard without an Allow button,
  and the server refuses to turn a "deny" into an "allow" even if the client
  is asked to. This is the one guarantee this enforcement point makes.
- **Never observed** — the session was never armed, or the hook payload was
  malformed. Nothing was evaluated at all, so this is neither open nor
  closed: it is exactly as if the hook were not installed, and Claude
  Code's own terminal prompt runs unmodified.
- **Deliberately lapsed — fails open.** A call was genuinely held (an armed
  session, a valid payload, no deny rule matched) and nobody answered within
  25 seconds. The hold gives up before Claude Code's own hook timeout does,
  on purpose, so a dashboard that is down, slow, or simply not being watched
  degrades the session back to its normal terminal prompt instead of hanging
  it forever. **State this plainly: an armed session nobody is watching is
  not protected any more strongly than an unarmed one.** The deny-rule check
  above is the only thing this enforcement point guarantees regardless of
  whether a human ever answers.

The hook point also does not consult the Decider's own pattern matcher for
its one active protection. The deny-rule check runs its own matcher
(`server/internal/claudesettings/deny.go:52-53`), which treats a
`domain:host` rule as matching whenever `strings.Contains(arg, host)` —
broader than the Decider's matcher (`server/internal/capability/pattern.go:84-97`),
which compares hostnames label by label so `example.com` cannot match
`evilexample.com`. This is deliberate, not an oversight: on the deny side,
matching *more* is the safe direction, because this matcher never grants
anything — it only decides whether to offer the Allow button, and the user
can still answer for real in their terminal. Swapping the Decider's strict
matcher onto the deny side would make deny rules match *less*, and start
offering Allow on calls the user's own settings already forbid. The two
matchers stay separate on purpose.

### Obsidian's TLS trust model

The Obsidian Application (`server/internal/apps/obsidian`) talks to Obsidian's
Local REST API, which serves HTTPS with a self-signed certificate — the
common workaround is `curl -k`, verification off entirely. `obsidian.Client`
makes that a decision instead of a silent default, through `Config.TLSMode`:

| Mode | What it does | When `NewClient` refuses it |
|---|---|---|
| `verify` | Normal certificate verification against the system trust store. | Never — but only works if you have separately installed and trusted the vault's certificate. |
| `pinned` | Trust-on-first-use: the first certificate seen is trusted and its SHA-256 fingerprint kept in memory; any later connection presenting a different fingerprint is refused. | Never at construction. The fingerprint is in-memory only and lost on restart — a fresh boot re-trusts whatever certificate answers first. |
| `insecure-loopback` | Skips certificate verification outright. | Unless the configured host actually resolves to loopback. That is the one case a network attacker cannot be the party presenting the certificate, which is what makes skipping verification tolerable here and nowhere else. |

Independent of `TLSMode`, the client resolves its configured host to a
single IP exactly once, at construction, and its dialer refuses to connect
to any other host or port afterwards — a DNS answer that changes after that
resolution (rebinding) is never consulted again. This runs *alongside*, not
instead of, the server-wide SSRF guard (`validation.IsBlockedIP`): that
guard blocks loopback on purpose, and Obsidian's API lives on loopback, so
the client carries its own narrow, named dial policy rather than widening
the shared guard to accommodate one application.

**The client is now constructed at boot, from `Settings → Obsidian`.**
`buildObsidianClient` (`server/serverapp/di_obsidian.go`) reads
`obsidian.baseURL`, `obsidian.vaultRoot`, `obsidian.apiKey`, and
`obsidian.tlsMode` from the settings registry after `obsidian.Register` runs.
Nothing set at all leaves the integration off; any one of `baseURL`,
`vaultRoot`, or `apiKey` set without the other two fails the boot outright,
naming the missing keys — a half-configured client would otherwise look
healthy while every request shipped an empty `Authorization` header and
failed 401. `obsidian.apiKey` is registered `Secret: true`
(`server/internal/settings/registry.go`): it is encrypted at rest with the
same AES-256-GCM `internal/secretbox` path the plugin secret mechanism
already used, and reads back as `********` on every surface except
`settings.Service.Secret`, the one accessor `buildObsidianClient` itself
calls to decrypt it. `Client.Read`/`Write`/`Search`/`Delete` still take no
capability repos and enforce nothing themselves — every production caller
(the `POST /api/obsidian/index` trigger, and the four `obsidian_*` MCP
tools below) authorizes through a `memory.Gate` before reaching the client,
so a future caller that reaches the client directly instead would bypass
that gate entirely.

`VaultRoot` is a second, independent boundary: `Read`/`Write`/`Delete`
resolve their note path inside it (`resolveVaultPath`) and refuse anything
that escapes, and the vault's own search endpoint — which is vault-wide by
design — is only ever reached through `Client.SearchUnderRoot`, which drops
results outside the root and rewrites the survivors to the root-relative
form `obsidian_read` accepts. Both callers use it: the index pass and the
`obsidian_search` MCP tool. Returning raw search results would disclose the
names and existence of notes outside the configured root to any holder of a
single `obsidian.search` grant, even though reading them would still be
refused. See [`PRIVACY.md`](../../PRIVACY.md) for what an indexing pass
persists.

## Reporting a vulnerability

Please report security issues privately via [GitHub Security Advisories](https://github.com/lx-wnk/Agent-Dashboard/security/advisories/new) rather than opening a public issue.
