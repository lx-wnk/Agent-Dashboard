# Permission-Ingress Bearer Auth (Origin-403 Fix) — Design

**Date:** 2026-06-09
**Status:** Approved (design), pending implementation plan
**Branch:** `fix/permission-ingress-bearer-auth` (off `upcoming`)
**Follow-up to:** the stage-output-write-tool work (PR #127); this is §6 of `docs/superpowers/specs/2026-06-08-stage-output-write-tool-design.md`, split out as agreed.

## Problem

The channel bridge (`server/internal/channel/bridge.go`) forwards an agent's permission
requests to the dashboard with a server-to-server `callDashboard(...)` POST carrying
`Authorization: Bearer <DASHBOARD_MCP_TOKEN>` — **no `Origin` header, no JWT cookie**:

- `POST /api/permission-requests` (`createPermissionRequest`)
- `POST /api/permission-requests/bulk` (`bulkCreatePermissionRequests`)

Both are mounted by `TaskHandler.Mount` **inside** the protected `r.Group` in
`router.go` that applies `RequireSameOriginForMutations` + `RequireAuth(JWT)` +
`RequireLoopbackHost`. Consequences:

- In bypass mode (`DASHBOARD_AUTH=none`, the default): `RequireAuth` is skipped, but
  `RequireSameOriginForMutations` (middleware.go) fails closed on a request with no
  `Origin`/`Referer` → **403 `missing Origin header`** (the exact error in the
  stage-output report footnote).
- In auth mode: the request also has no JWT cookie → **401** at `RequireAuth`.

Either way the bridge's permission-request POST cannot succeed. The endpoints have
**no bearer-token check of their own** — they rely entirely on the JWT group.

`/api/channel-reply` was correctly exempted (mounted outside the group), and PR #127's
`/api/channel-stage-output` follows the same bearer-authed-outside-the-group pattern.
The two permission-create endpoints were simply never moved.

## Scope (decided)

- **Move only the two agent-ingress CREATE endpoints** out of the protected group into a
  bearer-authenticated group, authenticated by `McpAuthMiddleware(ApiKeyRepo)` — the same
  `api_keys` SHA-256 mechanism `/api/mcp` uses and the same token the bridge already sends.
- **Resolution/grant endpoints stay in the JWT/same-origin group** (browser-driven):
  `GET /api/tasks/{id}/permission-requests` (list), `POST /api/permission-requests/{id}/resolve`
  (single), `POST /api/permission-requests/bulk-resolve` (bulk), `POST /api/tasks/{id}/permissions/bulk`
  (grant). Verified browser-only via `src/composables/useTasks.ts` + `useSlashCommands.ts`.
- **No handler-logic changes.** Pure routing/mounting + a new mount method. The create
  handlers already take `stageRunId` from the body and read no JWT payload.
- **No scope gate beyond a valid key** — matches `/api/channel-stage-output`. The shared
  `MCPToken` trust model already lets any agent call MCP control tools; creating a
  permission request is strictly less powerful.

### Verified facts (investigation)

| Fact | Evidence |
|---|---|
| CREATE endpoints are bridge-only (browser never calls them) | no `src/` fetch to `POST /api/permission-requests` or `/bulk`; bridge.go:162 is the sole caller |
| Browser calls list + resolve only | `useTasks.ts:307,314,336`, `useSlashCommands.ts:127` |
| `McpAuthMiddleware(ApiKeyRepo)` exists + is chi-compatible | `server/internal/mcp/auth.go:110-140` |
| Create handlers read no JWT payload | `permission_request_routes.go:43,211` decode body only |
| `TaskHandler.Mount` runs inside the protected group | `router.go:263` inside `r.Group` (`:194-356`) |
| Bearer-outside-group pattern to mirror | `router.go:360-376` (`channel-reply`, `channel-stage-output`, `mcp`) |

## Architecture

```
channel bridge ──Bearer DASHBOARD_MCP_TOKEN, no Origin/JWT──► POST /api/permission-requests[/bulk]
                                                                       │
                                              McpAuthMiddleware(ApiKeyRepo): HashToken → GetByHash
                                                                       │ valid key
                                                                       ▼
                                              createPermissionRequest / bulkCreatePermissionRequests
                                                              (unchanged handler bodies)
```

**Router mounting after the change:**

```
r.Group(protected):                      // RequireLoopbackHost + RequireSameOriginForMutations + RequireAuth
  TaskHandler.Mount(r)                    // list, resolve, bulk-resolve, grant  ← create routes REMOVED
  ...
(outside the group, bearer-authed):
  /api/channel-reply                      // existing
  /api/channel-stage-output               // existing (PR #127)
  r.Group { Use(McpAuthMiddleware) ; TaskHandler.MountAgentIngress(r) }   // NEW
    POST /api/permission-requests
    POST /api/permission-requests/bulk
  /api/mcp                                // existing
```

## Components

### 1. `TaskHandler.MountAgentIngress(r chi.Router)` (new method)
In `server/internal/api/tasks/handler.go`. Registers exactly the two create routes,
wrapped in `apierr.ErrorMiddleware` (same as today):
```go
func (h *Handler) MountAgentIngress(r chi.Router) {
	r.Post("/api/permission-requests", apierr.ErrorMiddleware(h.createPermissionRequest))
	r.Post("/api/permission-requests/bulk", apierr.ErrorMiddleware(h.bulkCreatePermissionRequests))
}
```

### 2. Remove the two create routes from `TaskHandler.Mount`
Delete the `r.Post("/api/permission-requests", ...)` and
`r.Post("/api/permission-requests/bulk", ...)` lines from `Mount` (handler.go:142-143).
Everything else in `Mount` stays.

### 3. Mount the bearer group in `router.go`
Outside the protected `r.Group`, next to the `/api/channel-stage-output` block:
```go
// Agent-ingress permission-request creation — bearer token auth via api_keys
// (MCP token), no JWT/Origin/loopback middleware: server-to-server call from the bridge.
if deps.TaskHandler != nil && deps.ApiKeyRepo != nil {
	r.Group(func(r chi.Router) {
		r.Use(mcp.McpAuthMiddleware(deps.ApiKeyRepo))
		deps.TaskHandler.MountAgentIngress(r)
	})
}
```
`router.go` already imports `internal/mcp` (used at `:376` for `/api/mcp`) and has
`deps.ApiKeyRepo`.

## Security

- New auth is `api_keys` SHA-256 (`McpAuthMiddleware`) — strictly stronger than today,
  where these endpoints had **no** bearer check and only "worked" by accident in dev.
- Moving out of `RequireSameOriginForMutations` is CSRF-safe: bearer tokens cannot be
  forged cross-site, so same-origin was never protecting these — it only blocked the
  legitimate bridge call.
- Resolution endpoints (state-changing grants the operator makes) keep full JWT +
  same-origin + loopback protection. No browser-facing surface is weakened.
- IDOR note: `MCPToken` is a single shared secret, so any agent can create a permission
  request against any `stageRunId` — but that is the existing trust model (all agents
  share the token and can already call MCP control tools). Out of scope to mint per-stage
  tokens; tracked as a known limitation.
- Behavioral change: in bypass mode the bridge call now requires `MCPToken` to be a valid
  `api_keys` entry. It already must be for `/api/mcp` to work for stage agents, so this is
  not a new requirement — a misconfigured (non-registered) token fails both paths.

## Testing

- **Unit (router/mount):** extend `bypass_auth_smoke_test.go` — the two create routes move
  to the bearer-skip allow-list (`bypassSkip`): an unauth request must not be asserted as a
  protected-route failure (they 401 by design now, like `/api/channel-stage-output`). Add
  both paths to the `bypassSkip` case.
- **Integration test (new):** mount the real router with `BypassAuth=true` + in-memory
  SQLite + a seeded `api_keys` row; assert:
  - `POST /api/permission-requests/bulk` with **no Origin** + **valid Bearer** → 2xx
    (regression: previously 403). 
  - same with **no/invalid Bearer** → 401.
  - `POST /api/permission-requests/bulk-resolve` (resolution) with no Origin → still 403
    (proves resolution stayed in the protected group).
- **Manual:** run a task to a stage that triggers `request_permission`; confirm the ON HOLD
  flow now reaches the dashboard instead of the bridge logging a 403.

## Out of scope

- Per-stage MCP tokens (replacing the global `MCPToken`) — separate hardening.
- The `/api/permission-requests/{id}/resolve` path-vs-route mismatch noted during
  investigation (frontend posts `/api/permission-requests/{id}/resolve`; backend route is
  `/api/tasks/{id}/permission-requests/{reqID}/resolve`) — pre-existing, unrelated, not
  touched here.
