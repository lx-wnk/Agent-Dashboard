# Server Architecture Review — 2026-05-14

## Summary

The Go server (`server/internal/`) is in good structural health: the layer compliance audit found zero upward imports across `db/`, `pipeline/`, and `mcp/`, and the middleware chain is correctly ordered with full security headers. The main problems are (1) one P0 context-propagation bug in the spawn message path, (2) pervasive use of `http.Error()` outside the `apierr` convention in ~15 handlers, and (3) a handful of silently-swallowed errors where failures should be logged.

**confidence:** high (>90%) — full static import scan, all handler files read, tests passing.

---

## Methodology

- Static import grep across all `*.go` non-test files in `internal/db/`, `internal/pipeline/`, `internal/mcp/`, and `internal/api/`
- Manual review of all handler files under `internal/api/*/handler.go` and supporting route files
- grep for `context.Background()`, `http.Error()`, `w.WriteHeader()`, and `_ =` patterns
- Test run: `go test -race ./...` (all 20 packages pass)

---

## Layer Compliance

Allowed direction: `db/* ← pipeline/* ← services (none) ← routes/mcp/* ← cmd/serve`

| Package | Imports internal layers | Status |
|---|---|---|
| `internal/db/` (ent, repo, rawrepo) | stdlib, ent-generated only | ✓ |
| `internal/pipeline/` | `db/ent`, `db/repo`, `parser`, `channelconfig`, `platform` | ✓ |
| `internal/mcp/` | `db/repo`, `db/ent`, `pipeline/` (orchestrator runtime), `validation` | ✓ |
| `internal/api/tasks/` | `db/ent`, `db/repo`, `pipeline/`, `apierr`, `sse`, `validation`, `auth` | ✓ |
| `internal/api/analytics/` | `analytics/`, `db/rawrepo`, `db/repo`, `apierr` | ✓ |
| `internal/api/agents/` | `auth`, `channelconfig` | ✓ |
| `internal/analytics/` | `parser` | ✓ |
| `internal/api/history/` | `history/`, `auth` | ✓ |
| `internal/api/refine/` | `refine/`, `db/repo`, `auth` | ✓ |
| All other `internal/api/*` | `apierr` or `db/repo` only | ✓ |

**No upward imports found.** Every dependency arrow points in the permitted direction.

---

## Handler Responsibility

Handlers are generally well-scoped: they decode requests, delegate to repos or orchestrators, and encode responses. The following specific concerns were noted:

### `internal/api/tasks/enrich.go`

`EnrichTask` and `EnrichTasksBulk` are pure business-logic functions (computing `needsUser`, `blockedByPendingPermissions`, etc.) that reference `pipeline.IsPidAlive` directly. This is appropriate — it is a stateless utility call, not a pipeline state-machine operation. No violation.

### `internal/api/tasks/cost_stage_routes.go:79`

`sessionID, _ = pipeline.FindNewestSessionID(task.Cwd, "")` — error silently swallowed. If session discovery fails for a non-obvious reason (permissions, path issue), the caller gets an empty `rawText` response with no indication. This is a P2 (the empty-response behaviour is safe, just unhelpful for debugging).

### `internal/api/agents/spawn.go`

`SendMessageToChannel` contains business logic (reading the channel discovery file, building an HTTP request to the channel bridge). This belongs in a service layer but is currently embedded in the `SpawnManager` type. Acceptable for a local dashboard with no service layer; classify as P2 refactor.

### `internal/api/tasks/handler.go:347-348`

Two intentional `_ =` ignores:
- `json.NewDecoder(r.Body).Decode(&body)` — body is optional for the retry endpoint; ignore is safe.
- `h.auditRepo.Append(...)` — audit is non-critical side effect; swallowing is intentional.

These are P2 at most — a log line on the audit failure would be helpful.

### `internal/api/memory/handler.go`

Functions `List`, `Get`, `Put` are free-standing (not struct methods) and contain inline path-traversal validation logic. This is a P2 — moving the validation into the `safePath` helper is already done; the `http.Error` pattern is the only inconsistency.

---

## Middleware Chain

Checked `server/internal/api/router.go` lines 76–97.

| # | Middleware | Status | Notes |
|---|---|---|---|
| 1 | `chimiddleware.RequestID` | ✓ | First in chain |
| 2 | `chimiddleware.RealIP` | ✓ | Before logging |
| 3 | `SlogMiddleware` | ✓ | Logs with `requestID` from context; debug paths suppressed |
| 4 | `chimiddleware.Recoverer` | ✓ | Before custom middleware |
| 5 | `SecurityHeaders` | ✓ | Applied globally |
| 6 | Body size limit (8 MB) | ✓ | Inline MaxBytesReader |
| 7 | `gzipMiddleware` | ✓ | After security headers, before routes; SSE excluded |

### Security Headers

All required headers present in `SecurityHeaders` middleware (`middleware.go:95-107`):

| Header | Value | Status |
|---|---|---|
| `X-Content-Type-Options` | `nosniff` | ✓ |
| `X-Frame-Options` | `DENY` | ✓ |
| `Referrer-Policy` | `strict-origin-when-cross-origin` | ✓ |
| `Cross-Origin-Resource-Policy` | `same-origin` | ✓ |
| `Cross-Origin-Embedder-Policy` | `require-corp` | ✓ |
| `Cross-Origin-Opener-Policy` | `same-origin` | ✓ |
| `Content-Security-Policy` | Set (restrictive) | ✓ |

Middleware chain: no ordering issues found.

---

## Error Handling

### `apierr` adoption

Most task/analytics/apikey/auth handlers use `apierr.ErrorMiddleware` and `apierr.JSONError`/`apierr.WriteJSON` consistently. The following handler groups do **not** adopt `apierr` and use raw `http.Error()` instead:

| File | Count | Assessment |
|---|---|---|
| `internal/api/memory/handler.go` | 6 | P1 — inconsistent, but isolated package |
| `internal/api/agents/channelreply.go` | 4 | P1 |
| `internal/api/agents/spawn.go` | 8 | P1 |
| `internal/api/hooks/handler.go` | 7 | P1 |
| `internal/api/sessions/handler.go` | 5 | P1 |
| `internal/api/history/handler.go` | 2 | P1 |
| `internal/api/tasks/handler.go:458` | 1 | P1 — stream fallback |
| `internal/api/middleware.go:82,87` | 2 | Acceptable — middleware, not handler |

**Fact:** 31 `http.Error()` call sites across handler code; `apierr`-based handlers have 0.

### Direct `w.WriteHeader()` without JSON

Several handlers write a bare `w.WriteHeader(http.StatusNoContent)` after completing an operation. This is correct for 204 responses (no body expected). No violation.

Exceptions noted:
- `internal/api/hooks/handler.go:72,76` — writes 401 then 204 without JSON body. The 401 path is missing a body (should be JSON); the 204 path is fine.
- `internal/api/agents/spawn.go:330` — sets header then writes JSON body; consistent.

### Swallowed errors

| Location | Expression | Risk | Assessment |
|---|---|---|---|
| `internal/api/tasks/enrich.go:26` | `pendingCount, _ = permRepo.CountForStageRun(...)` | Silent zero count if DB fails | P1 |
| `internal/api/refine/handler.go:196` | `_, _ = h.turns.Create(...)` | Audit turn not persisted; silent | P1 |
| `internal/api/tasks/cost_stage_routes.go:79` | `sessionID, _ = pipeline.FindNewestSessionID(...)` | Falls back to empty; safe | P2 |
| `internal/api/system/sysinfo.go:63` | `scriptAbs, _ = filepath.Abs(scriptAbs)` | Path resolution; fallback to relative; safe | P2 |
| `internal/api/tasks/handler.go:347-348` | Decode + audit append | Both intentional | P2 |
| `internal/api/memory/handler.go:92,126,145,170` | `json.NewEncoder(w).Encode(...)` | Headers already sent; ignore is idiomatic | Acceptable |

---

## Findings & Recommendations

### P0 — Fix now (correctness)

**P0-1: `context.Background()` in `SendMessageToChannel` — `internal/api/agents/spawn.go:267`**

`SendMessageToChannel` is called synchronously from the `Message` HTTP handler (`spawn.go:371`). It creates an outbound HTTP request to the channel bridge using `context.Background()`, which means:
- If the client disconnects, the request to the channel bridge is never cancelled.
- There is no way for the server shutdown signal to interrupt an in-flight channel message request.

The function does not have access to `ctx` because it takes only `(pid int, message string)`. The `channelMsgTimeout` constant (5s) bounds the worst case, but request-context cancellation is the correct mechanism.

**Fix:** Thread `ctx context.Context` as first parameter through `SendMessageToChannel` and the `Message` handler.

---

### P1 — Fix soon (maintainability)

**P1-1: `http.Error()` used instead of `apierr` across 6 handler packages**

Files: `memory/handler.go`, `agents/channelreply.go`, `agents/spawn.go`, `hooks/handler.go`, `sessions/handler.go`, `history/handler.go`.

These handlers predate the `apierr` package or were written without adopting it. The result is inconsistent Content-Type headers (some `http.Error()` callers send `text/plain`, others inline JSON strings), and error responses that bypass the central logging in `ErrorMiddleware`.

**Fix:** Migrate each to `apierr.JSONError()` or convert to `apierr.HandlerFunc` + `ErrorMiddleware`.

**P1-2: Swallowed `permRepo.CountForStageRun` error — `internal/api/tasks/enrich.go:26`**

`pendingCount, _ = permRepo.CountForStageRun(ctx, latest.ID)` silently zeros the count if the DB fails. In the `needsUser` computation this means a task that is blocked by pending permissions appears as "not blocked", potentially sending the kanban card into an incorrect state.

**Fix:** Log the error at `slog.Warn` level; return the error from `EnrichTask` so callers can handle it.

**P1-3: Swallowed `turns.Create` error — `internal/api/refine/handler.go:196`**

`_, _ = h.turns.Create(r.Context(), ...)` — refinement turns are not persisted on failure, with no log. History will silently be incomplete.

**Fix:** Log at `slog.Warn` level if `Create` fails.

---

### P2 — Consider later (nice-to-have)

**P2-1: `SendMessageToChannel` business logic in `SpawnManager`**

The discovery-file read + outbound HTTP call is service logic that would sit better in a dedicated `ChannelService`. Not an architectural violation because there is no separate service layer today; low priority.

**P2-2: `memory/handler.go` uses free functions, not struct methods**

Minor inconsistency with the rest of the API package. No correctness issue.

**P2-3: Missing log on `pipeline.FindNewestSessionID` error — `cost_stage_routes.go:79`**

Falls back to empty response; a debug log would help diagnose session lookup failures.

**P2-4: Audit append swallowed without log — `tasks/handler.go:348`**

Intentional, but a `slog.Warn` on failure would make it auditable.

---

## Action Items

- [x] P0: Thread `ctx` through `SendMessageToChannel` — `internal/api/agents/spawn.go:246–289` *(fixed in this commit)*
- [ ] P1: Migrate `http.Error()` callers to `apierr.JSONError` — 6 handler files
- [ ] P1: Log error in `enrich.go:26` (`permRepo.CountForStageRun`) and propagate
- [ ] P1: Log error in `refine/handler.go:196` (`turns.Create`)
- [ ] P2: Move `SendMessageToChannel` logic to a `ChannelService`
- [ ] P2: Add debug log for `FindNewestSessionID` failure in `cost_stage_routes.go:79`
