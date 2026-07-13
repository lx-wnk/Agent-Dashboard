# ADR-0010: Local-First Single-Process Boundary

**Status:** Proposed
**Date:** 2026-07-12

## Context

The dashboard is a monitoring tool for Claude Code agents running on the same
machine as the operator. Several structural properties follow from — but were
never explicitly recorded as — a single deliberate boundary:

- The HTTP server MUST bind `127.0.0.1`, never `0.0.0.0`, because it reads
  sensitive local Claude session data (`layer2-project-core.md`).
- Backend and frontend are one process on the desktop path: `serverapp`
  bootstraps the Go server in-process inside the Wails/WKWebView shell.
- Persistence is a single local SQLite file (ADR-0001); there is no network
  database, no clustering, no shared cache.
- Runner slots, the pipeline orchestrator, and the SSE broadcasters assume a
  single in-process owner (ADR-0002) — cross-tick state lives in one `Merger`.
- Auth exists to gate a locally-exposed port, not to serve multi-tenant traffic.

These assumptions are load-bearing for many downstream decisions (no
distributed locking, in-memory fan-out, process-local liveness probes) but are
scattered across constraints rather than stated as one boundary (finding
ARCH-P4-1).

## Decision

Record the **local-first single-process boundary** as an explicit architectural
invariant:

> The application runs as a single process on the operator's own machine,
> serving only `127.0.0.1`, backed by a single local SQLite file. There is no
> multi-node, multi-tenant, or remote-server deployment target. State
> coordination (runner slots, orchestrator, SSE fan-out, liveness) MAY assume a
> single in-process owner and MUST NOT be redesigned for distribution without a
> superseding ADR.

Any feature that would require crossing this boundary (remote server, shared
database, horizontal scaling, exposing a non-loopback interface) is a new
architectural decision requiring its own ADR, not an incremental change.

## Consequences

- **Simplicity is sanctioned.** In-memory broadcasters, process-local liveness
  (`internal/proc`, ADR-0009), and non-distributed locking are correct by
  design, not shortcuts to be "fixed."
- **Security posture is anchored.** The `127.0.0.1`-only bind is a boundary
  invariant, not a config default — closing the door on accidental
  `0.0.0.0` exposure.
- **Scope discipline.** Proposals for remote/multi-tenant operation are surfaced
  as boundary-crossing decisions requiring an ADR, preventing silent
  architectural drift.
- **No code change.** This ADR documents an existing boundary; it constrains
  future decisions rather than altering present behaviour.

## Alternatives Considered

1. **Leave it implicit.** Rejected — the boundary is already relied upon by
   ADR-0001/0002 and the security rules; leaving it unstated invites a future
   contributor to "add clustering" as if it were incremental.
2. **Design for optional remote operation now.** Rejected — no requirement
   exists; speculative distribution would tax every state-coordination decision
   for zero current benefit. Revisit only under a superseding ADR.
