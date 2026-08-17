# Remote Spawner Node — Design

**Date:** 2026-08-16
**Status:** Design, awaiting decisions (see *Open questions*). No implementation.
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
   the existing `envsec` machinery, and are never returned by any endpoint.

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

## Open questions — need a decision before phase 1

1. **Who owns the pipeline for remote work?** Does the dashboard's orchestrator drive stage runs on
   a node (node stays dumb), or may a node run its own pipeline? The first keeps ADR-0002's
   single-owner runner slots true; the second is what makes a node useful when the laptop is shut.
   *Recommendation: dashboard-owned first.*
2. **Identity source.** Node-local users, or reuse of the OAuth plugins (`auth.mode=plugin`) so the
   same accounts work in both places? *Recommendation: node-local first, plugin reuse later.*
3. **Discovery.** Manual URL entry per spawner, or advertisement on the LAN? *Recommendation:
   manual — it is one line of config and no attack surface.*
4. **Filesystem expectations.** A remote agent works on the node's checkout, not the laptop's. Does
   the node clone repositories itself, or is the working directory assumed to exist?
5. **Does the desktop app ever become a node?** Running the node binary next to the dashboard on the
   same laptop would let a second machine borrow it — cheap if the surface is identical, confusing
   if it is not.
