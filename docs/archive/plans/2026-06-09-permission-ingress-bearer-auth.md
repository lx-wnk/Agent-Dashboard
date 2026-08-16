# Permission-Ingress Bearer Auth (Origin-403 Fix) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the channel bridge create permission requests by moving the two agent-ingress endpoints (`POST /api/permission-requests`, `POST /api/permission-requests/bulk`) out of the JWT/same-origin group into an `api_keys`-bearer-authenticated group, fixing the `403 missing Origin header` failure.

**Architecture:** Pure routing change. Add `TaskHandler.MountAgentIngress`, remove the two create routes from `TaskHandler.Mount`, and mount them in `router.go` outside the protected group wrapped with `mcp.McpAuthMiddleware(deps.ApiKeyRepo)` — mirroring `/api/channel-stage-output`. No handler-logic changes. Resolution/grant endpoints stay JWT-protected.

**Tech Stack:** Go 1.26 (chi, ent), `testify`, `httptest`. Build/test via `task`.

**Spec:** `docs/superpowers/specs/2026-06-09-permission-ingress-bearer-auth-design.md`.

---

## File Structure

| File | Responsibility | Action |
|---|---|---|
| `server/internal/api/tasks/handler.go` | Add `MountAgentIngress`; remove 2 create routes from `Mount` | Modify |
| `server/internal/api/router.go` | Mount the bearer group outside the protected group | Modify |
| `server/internal/api/bypass_auth_smoke_test.go` | Add the 2 create paths to `bypassSkip` | Modify |
| `server/internal/api/permission_ingress_auth_test.go` | New integration test: no-Origin+valid-Bearer → 2xx; bad-Bearer → 401; resolution no-Origin → 403 | Create |

**Reused, not modified:** `mcp.McpAuthMiddleware` (`mcp/auth.go:110`), `apierr.ErrorMiddleware`, the create handlers (`permission_request_routes.go:43,211`), the full-router test harness already in `bypass_auth_smoke_test.go`.

---

## Task 1: Move the agent-ingress create routes to a bearer-authed group

**Files:**
- Create: `server/internal/api/permission_ingress_auth_test.go`
- Modify: `server/internal/api/tasks/handler.go`, `server/internal/api/router.go`, `server/internal/api/bypass_auth_smoke_test.go`

This is a cohesive routing change implemented test-first. The new integration test reuses the full-router-in-bypass-mode harness already present in `bypass_auth_smoke_test.go` (it builds the production router with `BypassAuth=true` against in-memory SQLite). Read that file first to reuse its router/DB setup helper rather than reinventing it.

- [ ] **Step 1: Write the failing integration test**

Create `server/internal/api/permission_ingress_auth_test.go` (package `api_test`). Reuse the existing harness in `bypass_auth_smoke_test.go` to build the router + in-memory ent client; seed one `api_keys` row and capture its raw token. Assert three behaviors:

```go
package api_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// NOTE: build the production router in BypassAuth=true with an in-memory ent
// client EXACTLY as bypass_auth_smoke_test.go does (reuse its setup helper —
// e.g. buildBypassRouter / newTestDeps, whatever it is named there). Seed one
// api key via repo.ApiKeyRepo.Create(...) and keep the raw token string.
//
// The three sub-tests below are the contract this task must satisfy.

func TestPermissionIngress_NoOriginValidBearer_Succeeds(t *testing.T) {
	router, rawToken := buildBypassRouterWithApiKey(t) // <- helper you add, see Step 1b

	body := []byte(`{"stageRunId":"sr-x","entries":[{"tool":"Bash","pattern":"pnpm test*"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/permission-requests/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+rawToken)
	// deliberately NO Origin header — this is the server-to-server bridge case.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// Must NOT be 403 (the old missing-Origin failure) and must NOT be 401.
	// The handler may return 2xx or a 4xx for business reasons (e.g. unknown
	// stage_run) — the contract is only that AUTH/CSRF no longer block it.
	require.NotEqual(t, http.StatusForbidden, rec.Code, "must not 403 on missing Origin")
	require.NotEqual(t, http.StatusUnauthorized, rec.Code, "valid bearer must authenticate")
}

func TestPermissionIngress_NoBearer_401(t *testing.T) {
	router, _ := buildBypassRouterWithApiKey(t)

	body := []byte(`{"stageRunId":"sr-x","entries":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/permission-requests/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// no Authorization header
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestPermissionIngress_ResolutionStaysProtected(t *testing.T) {
	router, rawToken := buildBypassRouterWithApiKey(t)

	// bulk-resolve is browser-driven and must STAY in the same-origin group:
	// a no-Origin mutation must still be rejected (403), even with a bearer.
	body := []byte(`{"taskId":"t-x","decision":"deny","all":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/permission-requests/bulk-resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+rawToken)
	// no Origin
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code, "resolution must remain same-origin protected")
}
```

- [ ] **Step 1b: Add the test helper**

If `bypass_auth_smoke_test.go` already exposes a router-builder you can call, use it and just seed an api key. Otherwise add a small helper in the new test file:

```go
// buildBypassRouterWithApiKey builds the production router in bypass mode with an
// in-memory ent client (mirroring bypass_auth_smoke_test.go's setup) and seeds one
// api key, returning the router and the raw (unhashed) token.
func buildBypassRouterWithApiKey(t *testing.T) (http.Handler, string) {
	t.Helper()
	// 1. open in-memory ent client (copy the open+migrate lines from bypass_auth_smoke_test.go)
	// 2. apiKeyRepo := repo.NewApiKeyRepo(client)
	// 3. rawToken := "mcp_" + <hex>; create the key:
	//    apiKeyRepo.Create(ctx, repo.CreateApiKeyInput{ Name: "test", Hash: mcp.HashToken(rawToken), Scopes: []string{"pipeline:control"} })
	//    (match the actual CreateApiKeyInput field names — read apikey repo)
	// 4. build api.RouterDeps with BypassAuth=true config + apiKeyRepo + TaskHandler
	//    (copy from the smoke test's deps assembly)
	// 5. return api.NewRouter(deps), rawToken
}
```

Read `bypass_auth_smoke_test.go` and `server/internal/db/repo/api_key_repo.go` to fill in the exact `CreateApiKeyInput` shape and the router-deps assembly. Do not guess field names — match them.

- [ ] **Step 2: Run the test to confirm it fails**

Run: `cd server && go test ./internal/api/ -run TestPermissionIngress -v`
Expected: `TestPermissionIngress_NoOriginValidBearer_Succeeds` FAILS with 403 (routes still inside the same-origin group). The other two may pass or fail depending on current wiring — the first one is the key red.

- [ ] **Step 3: Add `MountAgentIngress` to the task handler**

In `server/internal/api/tasks/handler.go`, add a new method (place it right after `Mount`):

```go
// MountAgentIngress registers the agent-initiated permission-request CREATE
// routes. These are called server-to-server by the channel bridge with a bearer
// MCP token (no Origin, no JWT), so the caller mounts them OUTSIDE the JWT/
// same-origin group, behind McpAuthMiddleware. Resolution/grant routes stay in
// Mount (browser-driven, JWT-protected).
func (h *Handler) MountAgentIngress(r chi.Router) {
	r.Post("/api/permission-requests", apierr.ErrorMiddleware(h.createPermissionRequest))
	r.Post("/api/permission-requests/bulk", apierr.ErrorMiddleware(h.bulkCreatePermissionRequests))
}
```

- [ ] **Step 4: Remove the two create routes from `Mount`**

In `server/internal/api/tasks/handler.go` `Mount`, delete these two lines (handler.go:142-143):

```go
	r.Post("/api/permission-requests", apierr.ErrorMiddleware(h.createPermissionRequest))
	r.Post("/api/permission-requests/bulk", apierr.ErrorMiddleware(h.bulkCreatePermissionRequests))
```

Leave the other permission routes in `Mount` untouched (`GET /api/tasks/{id}/permission-requests`, `POST /api/tasks/{id}/permissions/bulk`, `POST /api/permission-requests/bulk-resolve`).

- [ ] **Step 5: Mount the bearer group in `router.go`**

In `server/internal/api/router.go`, immediately after the `/api/channel-stage-output` mount block (around line 367-369, outside the protected `r.Group`), add:

```go
	// Agent-ingress permission-request creation — bearer token auth via api_keys
	// (MCP token), no JWT/Origin/loopback middleware: server-to-server call from
	// the channel bridge. Resolution endpoints stay in the protected group above.
	if deps.TaskHandler != nil && deps.ApiKeyRepo != nil {
		r.Group(func(r chi.Router) {
			r.Use(mcp.McpAuthMiddleware(deps.ApiKeyRepo))
			deps.TaskHandler.MountAgentIngress(r)
		})
	}
```

`router.go` already imports `internal/mcp` (used for `/api/mcp` at ~line 376) and has `deps.ApiKeyRepo`. If the `mcp` import alias differs, match the existing usage.

- [ ] **Step 6: Add the two paths to the bypass smoke-test allow-list**

In `server/internal/api/bypass_auth_smoke_test.go`, function `bypassSkip`, the create routes now 401 for unauthenticated callers by design (bearer-only). Add them to the bearer-auth case (the same one listing `/api/mcp`, `/api/channel-reply`, `/api/channel-stage-output`):

```go
	case pattern == "/api/mcp", pattern == "/api/channel-reply",
		pattern == "/api/channel-stage-output",
		pattern == "/api/permission-requests",
		pattern == "/api/permission-requests/bulk": // bearer-token (MCP) auth, outside JWT group
```

(Match the exact existing case structure in the file; the `/api/channel-stage-output` entry exists only if PR #127 merged first — if this branch is off `upcoming` before #127 merges, add `/api/channel-stage-output` too only if the route exists, otherwise just the two create paths. Verify by grepping the current `bypassSkip`.)

- [ ] **Step 7: Run the new test + the smoke test**

Run: `cd server && go test ./internal/api/ -run 'TestPermissionIngress|TestBypassAuth' -v`
Expected: all PASS — `NoOriginValidBearer_Succeeds` no longer 403s; `NoBearer_401`; `ResolutionStaysProtected` 403; the bypass smoke test green (no protected route returns 401/403 except the allow-list).

- [ ] **Step 8: Full build + lint + tests**

Run: `cd server && go build ./... && task test && task lint`
Expected: all green.

- [ ] **Step 9: Commit**

```bash
git add server/internal/api/tasks/handler.go server/internal/api/router.go \
        server/internal/api/bypass_auth_smoke_test.go \
        server/internal/api/permission_ingress_auth_test.go
git commit -m "fix(pipeline): authenticate permission-request creation via api_keys bearer (fixes Origin-403)"
```

---

## Task 2: Documentation

**Files:**
- Modify: `.agent-context/permissions.md`

The "Channel-Bridge Bulk Request" section describes `POST /api/permission-requests/bulk`. Add one line noting the auth + mount.

- [ ] **Step 1: Note the auth model**

In `.agent-context/permissions.md`, in the Channel-Bridge Bulk Request section, append:

> The two CREATE endpoints (`POST /api/permission-requests`, `POST /api/permission-requests/bulk`) are mounted **outside** the JWT/same-origin group and authenticated by the MCP `api_keys` bearer token (`McpAuthMiddleware`), because the channel bridge calls them server-to-server with no `Origin`/JWT. Resolution endpoints (`/resolve`, `/bulk-resolve`, grant) remain JWT + same-origin protected.

- [ ] **Step 2: Commit**

```bash
git add .agent-context/permissions.md
git commit -m "docs(permissions): record agent-ingress bearer auth for permission-request creation"
```

---

## Final Verification

- [ ] `cd server && task test && task lint && task build` — all green.
- [ ] **Manual:** run a task to a stage that triggers `request_permission`; confirm the ON HOLD request reaches the dashboard (no `403 missing Origin header` in the bridge log), and that the browser permission-resolution flow still works (grant/deny in the task modal).

---

## Self-Review (completed during authoring)

- **Spec coverage:** §Components 1-3 → Task 1 Steps 3-5; §Testing → Task 1 Steps 1,6,7; resolution-stays-protected invariant → `TestPermissionIngress_ResolutionStaysProtected`. Docs → Task 2.
- **Placeholder scan:** the test helper (Step 1b) is intentionally a fill-in-from-existing-harness stub with explicit instructions to read `bypass_auth_smoke_test.go` + `api_key_repo.go` for exact field names — this is delegation to existing code, not an unspecified requirement. Flagged as such.
- **Type consistency:** `MountAgentIngress(r chi.Router)` defined in Task 1 Step 3, called in Step 5. `mcp.McpAuthMiddleware(deps.ApiKeyRepo)` matches `mcp/auth.go:112`. Route paths identical across move (Mount removal) and re-mount (MountAgentIngress) and allow-list.
