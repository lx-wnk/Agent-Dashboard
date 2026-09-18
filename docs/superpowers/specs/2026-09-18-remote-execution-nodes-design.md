# Remote Execution Nodes — Design Spec

> Status: **proposed, not approved**. Written 2026-09-18. Nothing below is built.
>
> Goal, in the requester's words: *a second binary I can run on any server, which handles
> spawning there; registered in my desktop app as a spawner so jobs do not have to run
> locally. Security, rate limiting and authentication matter. A server can host several
> spawners, spawners can belong to a user (a separate Claude Max account), and there has
> to be an interface for configuring the server and its accounts.*

---

## 1. Status Quo

### What exists and is reusable

- **A spawner abstraction with a stable contract.** `LLMSpawner` is two methods —
  `Name()` and `Spawn(ctx, LLMSpawnArgs) (LLMSpawnResult, error)`
  (`server/internal/llmadapter/llm_spawner.go:44-49`). `PerStageSpawner` already routes a
  different spawner per pipeline stage (`llm_spawner.go:63-91`). A new adapter type is a
  `case` in `NewLLMSpawnerFromSpawner` (`adapter_factory.go:20-68`) plus a row.
- **A machine-to-machine credential system.** `api_keys` holds SHA-256 hashed bearer
  tokens with scopes, an `active` flag, a `kind`, an optional `stage_run_id` and an
  expiry (`server/internal/db/ent/schema/api_key.go:18-35`). The pipeline already mints
  one per stage run and revokes it afterwards (`serverapp/di_pipeline.go:168-171`). Auth
  is `Bearer` → hash → lookup → scope check (`server/internal/mcp/auth.go:161-189`).
- **Per-user remote registration, half-built.** `RemoteRegistration` stores
  `{user_id, url, name, bearer_key}` with the key hashed, and `/api/remotes` registers
  and connectivity-tests one (`server/internal/api/remotes/handler.go`). **Nothing reads
  registrations back** — the repo is referenced only by DI and its own handler. It is
  bookkeeping plus a ping, not a dispatch mechanism.
- **Spawn rate limiting.** A sliding window keyed by the authenticated subject, with
  configurable max and window (`server/internal/api/agents/spawn.go:70-115,155,357`).
  In-process and in-memory, so it is per dashboard instance, not distributed.

### What does not exist

- **No adapter runs a process on another machine.** `claude`, `custom`, `anthropic` and
  `acp` all `os/exec` on the dashboard's own host (`pipeline/spawner.go:360-369`,
  `llm_custom.go:31`, `llm_acp.go:131-152`). `ollama` and `openai` make an HTTP call from
  the dashboard process to an LLM API — network egress, not remote spawning.
- **No node, worker or agent subcommand.** `server/cmd/main.go` has `serve`, `channel`
  (a local re-exec of the same binary as an MCP stdio server) and `ptyhost`. No `Node` or
  `Worker` type exists anywhere.
- **`sdk.Agent.Machine` is declared and never written.** Grep finds the field
  (`sdk/types.go:474`) and no writer. It is a placeholder, not a foundation.
- **Spawners are global.** The `Spawner` schema has no `user_id`
  (`server/internal/db/ent/schema/spawner.go:16-46`). `Task` has an optional one
  (`task.go:29`), and `RemoteRegistration` has one — so per-user scoping exists in the
  codebase, just not on spawners.
- **The admin role is not grantable.** `is_admin` exists on `User` and nothing sets it;
  `router.go:369-372` says so in its own comment. Issue #427.

### The constraint that shapes everything

`validation.IsBlockedIP` rejects loopback, link-local, **all RFC1918 private ranges**
and **CGNAT 100.64.0.0/10** — and the comment names Tailscale as the reason for the last
one (`server/internal/validation/ssrf.go:19-36`). Its two production call sites are
`api/remotes/handler.go:54,60,72` and `apps/github/client.go:159`.

So the one path that already resembles "reach another machine" refuses every address a
home or office server is likely to have. The codebase has a recorded position on exactly
this, in `apps/github/client.go:128-137`:

> *"loosening the shared SSRF guard for one application is exactly the trade the Obsidian
> client refused, and it solved the mirror-image problem with a narrow per-client dial
> policy instead. The follow-up, if GHE-on-LAN is ever wanted, is that narrow policy, not
> a change to `validation.IsBlockedIP`."*

That is the project's own answer, and this spec follows it rather than re-opening it.

---

## 2. Decisions

### D1 — Which way does the connection go?

A remote node needs a channel to the dashboard. Who dials whom.

| Option | Pro | Con |
|---|---|---|
| **A. Dashboard → node over HTTPS** | Simplest mental model; node is an ordinary server; reuses the existing HTTP client shape. | Needs an inbound port on the node, so NAT and firewalls are the user's problem. Needs an SSRF carve-out for every private address, which is the widest possible version of that carve-out. |
| **B. Node → dashboard, long-lived connection, work pulled** | No inbound port, no port forwarding, works behind NAT and on a VPN. **No SSRF question at all** — the dashboard never dials out. Authentication is the node presenting a bearer token, which `api_keys` already does. | A persistent connection to keep alive, reconnect and fence. Work must be queued and leased rather than called. |
| **C. SSH transport around the existing exec path** | Nearly free: `CustomCommandSpawner{Command: "ssh node …"}` works today with no new code. | Ties the design to SSH keys and a shell; no per-user spawners, no rate limiting, no revocation, no health signal. Debugging is reading someone else's `~/.ssh/config`. |

**Recommendation: B.** It is the only option that removes the SSRF question instead of
carving around it, and the requirement is *any* server — which in practice means one
behind NAT, on a VPN, or on a CGNAT address the guard blocks by name. It also inverts
the trust direction the right way: the node proves who it is to the dashboard, rather
than the dashboard being talked into dialling an arbitrary address. C is worth naming as
the thing to use *today* if the need is urgent and single-user; it is a stopgap, not this.

### D2 — What is a node, thin or full?

A spawn touches a lot of local state: the working directory or git worktree, a settings
file written into it, a temp MCP config, the channel discovery files under `$HOME`, the
session JSONL read back afterwards, a filtered environment, an issued task-API token, and
the dashboard binary itself re-executed as `agent-dashboard channel`
(`pipeline/spawner.go:451-736`, `channelconfig.go:47-48,152`).

| Option | Pro | Con |
|---|---|---|
| **A. Thin executor** — node runs a command, streams stdout back | Small node. | Every item above has to be proxied over the wire: a filesystem, a home directory, a re-exec of a binary that is not there. This is a distributed filesystem wearing a spawner costume. |
| **B. Full host** — node carries the repo, the CLI, the binary and credentials; the dashboard sends *work*, not a command line | Each of those resources stays local to the process that uses it, which is what every one of them assumes. The node re-uses the spawn code unchanged. | The node has real prerequisites: a checkout, a Claude CLI, credentials. Provisioning is a thing the user has to do once per machine. |

**Recommendation: B.** A is not smaller, it is the same size moved onto the network and
made unreliable. B also matches what the user asked for — *a server that handles spawning
there* — rather than a remote shell.

Concretely: **the second binary is the same binary.** `agent-dashboard node` is a new
cobra subcommand next to `serve`, `channel` and `ptyhost`. One build, one release, one
version stamp, and `/api/system/health`'s `stale` check keeps working on the node.

### D3 — How does a node authenticate?

| Option | Pro | Con |
|---|---|---|
| **A. Reuse `api_keys`** with a new `kind: "node"` and node-specific scopes | The hashing, lookup, scoping, expiry and revocation all exist and are already used for a machine presenting a credential. One auth surface to reason about. | Scopes need a new group (`node:claim`, `node:report`). |
| **B. mTLS** | Strongest binding of identity to machine. | A certificate authority to run, rotate and explain. Nothing in this project issues certificates today. |
| **C. A shared secret in config** | Trivial. | No revocation, no per-node identity, no expiry — the three things the requirement explicitly asks for. |

**Recommendation: A.** It is the only one that is already load-bearing in this codebase.
The gap the survey names — *no per-host identity, no revoke-by-host* — is closed by a
`node` row that owns its key, not by a different credential system.

### D4 — What happens to the SSRF guard?

**Recommendation: nothing.** Under D1-B the dashboard never dials the node, so
`IsBlockedIP` is never on this path. The existing `/api/remotes` registration keeps its
guard, and `validation.IsBlockedIP` is not touched — which is the position
`apps/github/client.go:128-137` already recorded. If a future feature does need to dial a
LAN host, the answer stays a narrow per-client dial policy on that client.

### D5 — How do per-user spawners work?

The requirement names the reason: a separate Claude Max account per user.

| Option | Pro | Con |
|---|---|---|
| **A. Add `user_id` to `Spawner`** | Mirrors `Task.user_id` and `RemoteRegistration.user_id`; one concept, applied consistently. | Existing rows need a null-means-shared migration, and every spawner read needs a scope filter — a place to forget one. |
| **B. Scope by node only** — a node belongs to a user, its spawners inherit that | No spawner migration. | A shared node cannot host two users' spawners, which is exactly the "several spawners on one server" case asked for. |

**Recommendation: A**, with `user_id` nullable and null meaning shared, because B
contradicts a stated requirement. The filter belongs in the repository, not at call
sites — the survey already shows what happens when an obligation lives in a comment
(`router.go:369-372`).

**Blocked by #427.** Per-user anything is unenforceable while no code sets `is_admin` and
the admin gate passes everything through. #427 is a prerequisite of this slice, not a
neighbour of it.

### D6 — What configures a node?

| Option | Pro | Con |
|---|---|---|
| **A. Configure from the dashboard**; the node holds only its identity and its connection | One place to configure, which is the whole premise of "the app is the workplace". | A node that cannot reach the dashboard cannot be diagnosed on the node. |
| **B. A second web UI on the node** | Works standalone. | A second frontend to build, style and keep in step — the drift the user explicitly does not want. |
| **C. `agent-dashboard node` CLI subcommands** for enrolment and local diagnosis, everything else from the dashboard | Enrolment and "why am I not connecting" are exactly the things needed before a connection exists; everything else is a dashboard concern. | Two places, but split along a real line. |

**Recommendation: C.** `node enrol`, `node status`, `node logs` locally; accounts,
spawners, limits and assignment in the dashboard. B is the layout drift the user called
out in the same list this requirement came from.

---

## 3. What Changes

### 3.1 New entity: `node`

`id, name, slug, user_id (nullable), api_key_id, status, last_seen_at, version, platform,
capabilities, created_at, updated_at`. `status` is derived from `last_seen_at`, the same
shape `merger.CalculateStatus` already uses for agents — one rule for "how old is this",
not a second.

### 3.2 `Spawner` gains `node_id` and `user_id`

Both nullable. `node_id` null means the local host, which is every existing row — no
data migration, and the default stays what it is today.

### 3.3 New adapter type `node`

`NewLLMSpawnerFromSpawner` gains a `case "node"` returning a `NodeSpawner` that enqueues
work against `node_id` and waits for the result, instead of `os/exec`. `ValidAdapterTypes`
gains `node`. Everything above `LLMSpawner` is unchanged — the pipeline does not learn
that a stage ran elsewhere.

### 3.4 The node protocol

Node dials the dashboard, presents its bearer token, and holds a connection. Over it:
`claim` (lease a unit of work), `heartbeat`, `report` (progress and result), `release`.
Leases expire, so a node that dies returns its work rather than stranding it — the same
reasoning as the pipeline's existing lingering-pending gate.

### 3.5 Rate limiting

The existing sliding window is per dashboard instance and keyed by subject
(`spawn.go:155`). Node work is dispatched by the dashboard, so the same limiter covers
it; a **per-node concurrency cap** is added separately, because a rate is not a
concurrency limit and a node with four cores should say so.

---

## 4. Error Handling

- A node that stops heartbeating has its leases expire and its work requeued, reusing the
  requeue path that already exists for infrastructure failures.
- A node presenting a revoked or expired key is refused at the same place an MCP token is.
- A spawner pointing at a node that no longer exists fails the stage with that sentence,
  rather than silently falling back to local execution — a job that runs on the wrong
  machine is worse than one that does not run.

## 5. Testing

- Node auth: valid, revoked, expired, wrong-scope, all against the real `api_keys` path.
- Lease expiry: a node that stops heartbeating mid-work returns it exactly once.
- Adapter dispatch: a `node` spawner routes to `NodeSpawner`, a `claude` spawner still
  routes locally, and a spawner naming a dead node fails loudly.
- Per-user scoping: a spawner owned by user A is invisible to user B — asserted at the
  repository, which is where the filter lives.

## 6. Out of Scope

- Scheduling across several nodes. One node per spawner; picking between nodes is a
  later question and a different spec.
- Streaming a remote agent's terminal into the dashboard. The pty broker is local by
  construction; a remote session shows transcript and status, not a live terminal.
- Aggregating a remote *dashboard's* agents. That is what `RemoteRegistration` was for,
  and this spec does not revive it.

## 7. Delivery

Slices, each shippable on its own:

1. **#427 first** — make the admin role grantable and reinstate the gate. Prerequisite.
2. `node` entity, `node enrol` / `node status`, key issuance and revocation, dashboard
   list showing nodes and their last-seen. No work dispatched yet.
3. The connection and the lease protocol, with a synthetic work unit. Proves reconnect,
   expiry and requeue before a real agent is involved.
4. `node` adapter type and `NodeSpawner`; a pipeline stage runs on a node end to end.
5. `user_id` on spawners, repository-level scoping, per-user keys.
6. Per-node concurrency cap and the node's page in the dashboard.
