# Remote Spawner Node — Design

**Date:** 2026-08-16
**Status:** Design, decisions recorded (see *Decisions*). No implementation.
**Decision record:** [ADR-0013](../../architecture/adr/0013-remote-spawner-nodes.md)

## Problem

Run agents somewhere other than the operator's laptop: a binary on an arbitrary server owns
spawning, the desktop app registers it as a spawner, several spawners can live on one server, and
each user brings their own Claude account. Administration of server, accounts and spawners must be
possible on the server itself.

## What already exists

Verified against the code before designing — these are seams, not wishes:

| Seam | Where | State |
|---|---|---|
| `Machine` on the agent wire type | `sdk/types.go:387` | Field exists, **nothing sets it** |
| Client branches on `machine` | `AgentCard.vue:110,216`, `AgentModal.vue:80,95,245`, `useAgents.ts:105` | Badge, hidden prompt input, skipped transcript fetch, search |
| Spawner rows with adapter dispatch | `ent/schema/spawner.go`, `api/agents/spawn.go` | `adapter_type` + `adapter_config` map, `claude`/`custom`/`anthropic`/`openai`/`ollama` |
| Per-IP token-bucket rate limiting | `api/middleware.go:157` | In use for the local API |
| Scope-checked bearer auth | `internal/mcp/auth.go:17` (`ToolScopeMap`) | Every MCP tool names its minimum scope |
| Live session I/O over a local broker | `internal/channel/ptyhost.go`, `api/agents/{pid}/terminal` | pty broker with token-authenticated loopback HTTP + WebSocket proxy |

The last row matters most: the dashboard already talks to a *separate process* that owns a pty and
answers authenticated HTTP on loopback. A spawner node is that same shape with the loopback
assumption removed and a real trust boundary added.

## Components

```
operator laptop                          spawner host
┌───────────────────────────┐            ┌──────────────────────────────┐
│ dashboard (unchanged)     │            │ agent-dashboard-node          │
│  spawner row              │  HTTPS     │  ├── /v1/spawn                │
│   adapter_type: "remote"  ├───────────►│  ├── /v1/agents (list/stream) │
│   adapter_config:         │  bearer    │  ├── /v1/agents/{id}/input    │
│     url, token_ref        │            │  ├── /v1/agents/{id}/stream   │
│  Agent.Machine = node name│            │  └── admin CLI (users,        │
└───────────────────────────┘            │      spawners, tokens)        │
                                         └──────────────────────────────┘
```

The node is its own binary with its own store. It never reaches back into the dashboard: all calls
are dashboard → node, so the node needs no inbound knowledge of the operator's machine and the
dashboard keeps its `127.0.0.1` bind.

**`token_ref` has no home yet.** The dashboard sends the bearer token *outbound*, so it must be able
to read it back in cleartext at call time. The nearest existing store cannot do that:
`remote_registration.bearer_key` holds a SHA-256 hex digest, and its schema comment says so verbatim
— "not usable as an outbound Authorization header"
(`server/internal/db/ent/schema/remote_registration.go:20-21`). Hashing is correct there, because
that column authenticates an *inbound* caller; an outbound credential is the opposite problem. Phase
1 therefore stores the node token **encrypted but recoverable**, not hashed — see *Security model* §6
for the primitive.

## Wire surface (proposed, minimal)

| Endpoint | Purpose |
|---|---|
| `POST /v1/spawn` | Start an agent. Body mirrors the local spawn request (cwd, prompt, model, permission mode, spawner slug). Returns an agent id. |
| `GET /v1/agents` | Roster for this node: the same `sdk.Agent` shape the dashboard already renders, with `machine` set to the node name. |
| `GET /v1/agents/stream` | SSE, same frames as the local `/api/agents/stream`. |
| `POST /v1/agents/{id}/input` | Deliver keystrokes/prompt to a live session (the node's local pty broker does the work). |
| `GET /v1/agents/{id}/output` | Transcript, so the client's "no transcript for remote agents" branch can eventually be lifted. |
| `GET /v1/health` | Liveness plus node name and version, for the spawner-settings connection test. |

Reusing `sdk.Agent` verbatim is deliberate: the dashboard merges remote rosters into its own list
with no translation layer, and the SDK package is already the shared vocabulary between the two Go
modules.

## Security model

Non-negotiable, because this is the first component allowed off loopback:

1. **Transport.** TLS required; refuse to start on a non-loopback bind without a certificate. Plain
   HTTP allowed only when bound to `127.0.0.1` (development, or behind an operator's own tunnel).
2. **Authentication.** Bearer token per dashboard client, stored hashed on the node (same treatment
   as the existing API keys). Tokens name a **user**, which is what makes per-user spawners work.
3. **Authorization.** A token may only use spawners its user owns, and may only address agents its
   user started. Scope names follow the existing `ToolScopeMap` convention rather than inventing a
   second vocabulary.
4. **Rate limiting.** Per-token token bucket, reusing `IPRateLimiterConfig`'s shape. Spawn is the
   expensive verb and gets its own tighter bucket.
5. **Command policy.** The node applies the same allow-list the local spawner does
   (`services.ValidateSpawnerCommand`) — a remote token must not become arbitrary remote code
   execution by way of a crafted spawner row.
6. **Secrets.** Per-user API keys (Anthropic and friends) live on the node, encrypted at rest with
   `server/internal/secretbox/secretbox.go` (AES-256-GCM, master key bootstrapped by
   `LoadOrGenerateMasterKey`), and are never returned by any endpoint. Not `envsec`:
   `server/internal/envsec/envsec.go` is a 16-line deny-list of four environment-variable *names*
   that must not be forwarded to a spawned process — it contains no cryptography at all. The same
   `secretbox` primitive covers the dashboard side's recoverable `token_ref` (see *Components*).

## Administration

The node ships with its own CLI — no desktop app required on the server:

```
agent-dashboard-node serve            # run the node
agent-dashboard-node user add <name>  # create a user
agent-dashboard-node token issue <user>
agent-dashboard-node spawner add <user> --slug … --command … --adapter …
agent-dashboard-node spawner list
```

This mirrors the dashboard's existing `settings set` CLI, so operators meet one idiom, not two.

## Phasing

Each phase is independently useful and independently revertable:

1. **Node skeleton** — binary, config file, `/v1/health`, TLS/bind guard, token store, admin CLI for
   users and tokens. No spawning yet.
2. **Spawn + roster** — `/v1/spawn`, `/v1/agents`, per-user spawner rows, command allow-list. The
   dashboard gets the `remote` adapter and merges the roster; `Machine` finally gets set, and the
   six existing client branches light up for free.
3. **Live I/O** — `/v1/agents/stream`, `/v1/agents/{id}/input`, transcript passthrough. Remote
   agents become as controllable as local ones and the "no prompt input" branches can relax.
4. **Quota and cost attribution per user** — usage follows the account, not the host.

Not every decision below gates phase 1. Phase 1 ships a token store, an admin CLI and a config file
and spawns nothing, so only the decisions that shape those artifacts bind it:

| Decision | Gates |
|---|---|
| D2 identity — node-local users | Phase 1 (the token store and `user add` / `token issue` are its schema) |
| D3 discovery — manual URL | Phase 1 (the config file and the connection test) |
| D5 packaging — separate binary | Phase 1 (there is no phase 1 artifact until the binary exists) |
| D1 pipeline ownership — dashboard-owned | Phase 2 (first stage run dispatched to a node) |
| D4 filesystem — node clones on demand | Phase 2 (first `cwd` a remote agent has to work in) |

## Decisions

Recorded 2026-08-17. These refine [ADR-0013](../../architecture/adr/0013-remote-spawner-nodes.md);
none of them contradicts it, and where one is forced by it that is said outright.

### D1 — The dashboard owns the pipeline. A node never runs its own.

Not a preference, and not a choice this spec was free to make. ADR-0013's Decision text already
settles it: "Neither side shares a database, a runner-slot pool, or an SSE fan-out with the other."
A node with its own pipeline is a second runner-slot pool by another name, and ADR-0013's
Alternative 3 rejects exactly that shape ("control-plane / worker split") for a single-operator
tool. A node-owned pipeline is therefore not an implementation option but a **new ADR superseding
0013**.

The mechanism to keep the pipeline dashboard-side already exists and is already load-bearing for the
HTTP adapters. `server/internal/pipeline/stage_handlers.go:103-112` dispatches a stage
asynchronously and returns `AsyncRunningTransition{PID: 0}` — a running stage run with no local
process — and `server/internal/pipeline/sweeps.go:167-169` documents that a nil PID alone is not
evidence of a zombie, so the orphan sweep does not reap it. A remote stage is that same shape with a
longer wire.

The cost is honest and accepted: when the laptop sleeps, remote work stops. Buying the other
property would mean distributing orchestrator state, which is the thing ADR-0010 and ADR-0013 exist
to avoid.

### D2 — Identity is node-local. Several humans may share one node.

The node keeps its own user table: an opaque id, a display name, and a hashed credential. It mirrors
`server/internal/db/ent/schema/user.go` minus `provider_login` — there is no upstream identity
provider to carry a login name from — with the credential stored the way
`server/internal/db/ent/schema/api_key.go:20` stores `key_hash`: hashed, unique, `Sensitive()`. That
a node serves several people is the requirement, not a side effect; per-user spawners exist so each
human brings their own Claude account.

The OAuth plugins are explicitly **not** reused. Two reasons, both structural:
`server/internal/settings/registry.go:108` caps `auth.mode` at `none|plugin`, so there is no third
mode to slot a node into without widening a settings enum that the dashboard's own auth depends on;
and `plugin` means a browser redirect handled by the plugin subprocess runtime, which assumes a
human at a browser on the same machine. A headless server has neither. Revisiting this later is
cheap — the node's user row is the join point — but it is not phase 1.

### D3 — Discovery is a manual URL. No LAN advertisement, no discovery code.

This one is already built, and hardened past what a fresh implementation would get right.
`server/internal/api/remotes/handler.go` stores a per-user remote (`url`, optional `name`, bearer
key), validates the URL through `isSafeRemoteURL` (`handler.go:38-65`), probes connectivity under a
15 s timeout (`handler.go:180`) with a client whose `DialContext` is `validation.SafeDialContext`
(`handler.go:68-72`) so a DNS rebind cannot walk the resolved address back to loopback, strips
`Authorization` on cross-origin redirects, and caps the table at 50 rows (`handler.go:193`). The UI
is `src/features/settings/components/RemoteSettings.vue`. Broadcast discovery would add an
unauthenticated network surface to a component whose entire justification is a narrow one.

**One change, and only one:** the probe currently requests `{baseURL}/api/agents`
(`handler.go:89-92`) — a dashboard route. A node speaks `/v1/*`, so the probe must target
`/v1/health`, which the wire surface already lists for this purpose.

**Known constraint, recorded so it is not discovered during phase 1:** `validation.IsBlockedIP`
(`server/internal/validation/ssrf.go:26-36`) rejects `IsPrivate()` alongside loopback, link-local,
CGNAT and multicast. A node on `192.168.x.x` or `10.x.x.x` is therefore *unreachable through this
form today*. That is correct for the SSRF-prone use it was written for; it is wrong for a node
deliberately placed on the operator's own LAN. Phase 1 must decide whether the node URL gets a
separate, opt-in validation path or the operator is told to front the node with a public name.

### D4 — The node clones repositories itself, on demand.

Decided against the cheaper alternative ("the working directory must already exist"). The cheap
option makes every node a hand-provisioned pet and defeats the point of pointing the dashboard at an
arbitrary server.

Nothing in the repo clones anything today, so this is net-new work in three parts, all of which
belong to phase 2 and none of which should be discovered late:

1. **Git credentials on the node**, stored in the same phase-1 secret store as the API keys — a
   private repo is the normal case, not the exception.
2. **A repo cache with a disk budget.** N users × M repositories on someone else's server fills a
   disk; an eviction rule is part of the feature, not an operational afterthought.
3. **A per-user authorization rule for which repositories a user may clone.** Without it, any token
   on the node is a read primitive against every repo the node's git credentials can reach.

There is one consequence that follows from D1 rather than from D4 itself, and it is the sharper one.
Today the *dashboard* creates the worktree: `server/internal/pipeline/worktree.go:23`
(`ensureTaskWorktree`) runs `git worktree add` in `task.Cwd` on the dashboard's filesystem
(`worktree.go:46`), and `server/internal/pipeline/progress_guards.go:73-82` persists the resulting
path to `task.WorktreePath`. `server/internal/pipeline/stage_handlers.go:77-80` then hands that path
to the spawner as `cwd`. For a remote node that path names a directory that does not exist. Phase 2
must therefore **delegate worktree creation to the node** — a `/v1/worktree` verb whose response is
the node-side path — rather than pass the local one across the wire.

The post-clone hook already exists and should be reused rather than reinvented: `project.setup_command`
runs once in a freshly created worktree under a 5-minute timeout
(`server/serverapp/di_pipeline.go:38-72`), wired as `SetupWorktreeFn` and invoked right after the
worktree is persisted (`progress_guards.go:88-92`). "Clone, then run setup_command" is the same
sequence with a different first step.

### D5 — The desktop app never becomes a node. Separate binary only.

A new `server/cmd/node/` alongside the existing `server/cmd/serve` and `server/cmd/cli`, plus its own
Taskfile target next to `build:cli` and `build:desktop`. `desktop/main.go` stays what its own package
comment says it is: a macOS wails shell that starts the dashboard in-process on loopback.

The rationale is the security argument of ADR-0013 restated: the node is acceptable off loopback
*because* `/v1/*` is a narrow, purpose-built surface. Letting the desktop app answer for a node would
not expose that surface — it would expose the dashboard's.

The trap this avoids is not hypothetical; the switch already exists.
`server/internal/config/config.go:142-152` lets the dashboard bind a non-loopback address as long as
`DASHBOARD_REMOTES_ENABLED=true` is set, with a warning and nothing else. Anyone who reaches for that
flag to "make the laptop a node" publishes the *entire* dashboard API — including the terminal
WebSocket, which `server/internal/api/agents/terminal.go:90` describes in its own words as giving
"full read/write access to the spawned agent, i.e. remote code execution". Two binaries keep that
flag from ever looking like the answer.
