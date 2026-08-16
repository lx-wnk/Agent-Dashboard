# ADR-0013: Remote Spawner Nodes

**Status:** Proposed — decision pending, no code written
**Date:** 2026-08-16
**Relates to:** [ADR-0010](0010-single-process-boundary.md), [ADR-0003](0003-pluggable-spawners.md)

## Context

The request (`docs/local/todos/human-noted.md`, *Feature-Sets*) is to run agents on a machine that
is not the operator's laptop: a separate binary on an arbitrary server that owns spawning, which
the desktop app registers as one more spawner, so work does not have to run locally. On that server
there may be several spawners and per-user spawners — one Claude Max account per user — plus a way
to administer server, accounts, and spawners.

ADR-0010 records the opposite as an invariant: one process, `127.0.0.1` only, one local SQLite file,
no remote deployment target. It also states the escape hatch — crossing that boundary "is a new
architectural decision requiring its own ADR". This is that ADR.

Two facts make the crossing smaller than it sounds:

- The agent wire format already carries `Machine` (`sdk/types.go:387`), and the client already
  branches on it in six places: the badge (`AgentCard.vue:110`, `AgentModal.vue:95`), the prompt
  input is hidden for a remote agent (`AgentCard.vue:216`, `AgentModal.vue:245`), the transcript
  fetch is skipped (`AgentModal.vue:80`), and search matches it (`useAgents.ts:105`). Nothing on
  the server ever sets the field. An earlier design pass anticipated remote agents; only the
  server half was never built.
- Spawners are already rows with an `adapter_type` and an `adapter_config` map
  (`ent/schema/spawner.go`), dispatched per adapter. A remote node is another adapter, not a new
  concept in the UI.

## Decision

Allow **remote spawner nodes** as an explicitly opt-in deployment, without dissolving the local-first
boundary:

> The dashboard remains a single local process serving `127.0.0.1`, owning its own SQLite file and
> its own in-process coordination. A **spawner node** is a *separate binary* that owns process
> spawning and live-session I/O on its own host, and exposes a narrow, authenticated HTTP API. The
> dashboard talks to it as a client through a `remote` spawner adapter. Neither side shares a
> database, a runner-slot pool, or an SSE fan-out with the other.

Consequences of that shape, which are the point of choosing it:

- ADR-0010 stays intact for the dashboard. No distributed locking, no shared cache, no clustering
  inside the dashboard process; the node is a peer service, not a second replica.
- The node — not the dashboard — is the only component that may bind a non-loopback interface, and
  only when its operator configures it to. The dashboard's `127.0.0.1` invariant is untouched.
- Remote agents surface through the existing `Machine` field, so the client work is mostly already
  done and degrades honestly: no local transcript, no direct prompt input.
- A remote node is a trust boundary in a way the local process never was. Authentication, transport
  security, rate limiting, and per-user isolation are part of the feature, not a later hardening
  pass — see the design spec.

## Consequences

- **New failure modes become first-class.** A node can be unreachable, slow, or lying. Every remote
  call needs a timeout and a visible degraded state; a dead node must not stall a scan tick, the
  way `RealScreenProbe` already bounds its loopback probes at 250 ms.
- **Cost and quota accounting spans hosts.** Per-user spawners exist so several people can each use
  their own Claude subscription; usage attribution has to follow the account, not the machine.
- **The admin surface grows.** Users, per-user spawners, and node configuration need an interface on
  the node itself, reachable without the desktop app.
- **Reversible.** Nothing here changes local behaviour: with no remote spawner configured, the
  system is byte-for-byte the local-first tool it is today.

## Alternatives Considered

1. **Drive a remote machine over SSH from the dashboard.** Rejected: it makes the dashboard a
   remote-execution client with the operator's SSH credentials, gives no place to manage per-user
   accounts, and provides no story for live session I/O beyond a second SSH channel per agent.
2. **One dashboard per machine, federated read-only.** Rejected: it solves monitoring, not the
   request — work still has to be started on the target machine by hand.
3. **Control-plane / worker split (dashboard becomes a scheduler over N workers).** Rejected for
   now: it dissolves ADR-0010 outright — runner slots, orchestrator state, and SSE fan-out would all
   need redesign for distribution, to serve a single-operator tool. Revisit only if several
   operators ever share one dashboard.
4. **Do nothing.** Rejected: the request is concrete and recurring, and the wire format already
   anticipates it.
