# PR-C: Server Architecture Review Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Audit the Go server architecture for layer compliance, handler responsibility, middleware correctness, and error handling consistency. Produce `docs/architecture/server-review.md` and fix any P0 issues inline.

**Architecture:** Read-heavy analysis across `server/internal/`. Findings go in a structured markdown doc. Fixes are minimal inline patches committed alongside the doc.

**Tech Stack:** Go, chi, ent ORM, apierr package

---

## Worktree Setup

```bash
git worktree add ../agent-dashboard-prc chore/server-arch-review
cd ../agent-dashboard-prc
```

---

## File Map

| Action | File |
|--------|------|
| Create | `docs/architecture/server-review.md` |
| Modify | `server/internal/api/router.go` (if P0 issues found) |
| Modify | Various handler files (if P0 issues found) |

---

### Task 1: Layer Compliance Audit

- [ ] **Step 1.1: Map all inter-package imports**

Run the following to list all non-stdlib imports per package:

```bash
cd server && grep -r "\"github.com/lx-wnk/agent-dashboard/server/internal/" --include="*.go" -h | \
  sort -u | grep -v "_test.go"
```

- [ ] **Step 1.2: Check for upward imports (violations)**

The allowed import direction is:
```
db/*  ←  pipeline/*  ←  services/*  ←  routes/* and mcp/*  ←  server/index (cmd/serve)
```

Check specifically:
```bash
# db/* must not import pipeline, services, routes, mcp, notifications
grep -r "internal/pipeline\|internal/api\|internal/mcp\|internal/notifications" \
  server/internal/db/ --include="*.go"

# pipeline/* must not import services, routes, mcp, notifications
grep -r "internal/api\|internal/mcp\|internal/notifications" \
  server/internal/pipeline/ --include="*.go"

# services/* must not import routes, mcp, notifications
grep -r "internal/api\|internal/mcp" \
  server/internal/services/ --include="*.go" 2>/dev/null || echo "no services dir"
```

- [ ] **Step 1.3: Record findings**

For each violation found, classify as P0 (upward runtime import) or P1 (type-only import exceeding allowed scope). Add to the findings doc (Task 5).

---

### Task 2: Handler Responsibility Audit

Each handler in `server/internal/api/*/` should follow: decode request → call repo/service → encode response. Business logic belongs in pipeline or services, not handlers.

- [ ] **Step 2.1: Read all handler files**

```bash
find server/internal/api -name "handler.go" | sort
```

Read each file and answer for each handler function:
1. Does it contain business logic beyond request/response translation?
2. Does it directly touch DB repos that belong in a service?
3. Is error handling consistent (uses `apierr` package)?

- [ ] **Step 2.2: Check apierr usage**

```bash
# Handlers should return errors via apierr or use apierr.ErrorMiddleware
grep -rL "apierr" server/internal/api/ --include="handler.go"
# Lists handlers that don't use apierr at all — investigate each
```

- [ ] **Step 2.3: Check context propagation**

```bash
# Every DB call should use r.Context(), not context.Background()
grep -rn "context\.Background()" server/internal/api/ --include="*.go"
```

Flag any `context.Background()` inside handler functions (not in middleware or init code).

---

### Task 3: Middleware Chain Review

- [ ] **Step 3.1: Read the middleware chain in `router.go`**

Read `server/internal/api/router.go` lines 76–100. Verify:
1. `RequestID` is first — ✓/✗
2. `RealIP` before logging — ✓/✗
3. `SlogMiddleware` logs with request ID — ✓/✗
4. `Recoverer` is before custom middleware — ✓/✗
5. `SecurityHeaders` applied globally — ✓/✗
6. Body size limit set — ✓/✗
7. `gzipMiddleware` positioned correctly (after security headers, before routes) — ✓/✗

- [ ] **Step 3.2: Verify security headers**

```bash
grep -n "SecurityHeaders\|X-Frame-Options\|Content-Security\|X-Content-Type" \
  server/internal/api/router.go server/internal/api/middleware.go
```

Check that at minimum: `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy` are set.

---

### Task 4: Error Handling Consistency

- [ ] **Step 4.1: Find inconsistent error responses**

```bash
# Look for bare http.Error() calls in handlers (should use apierr instead)
grep -rn "http\.Error(" server/internal/api/ --include="*.go" | grep -v "_test.go"
```

- [ ] **Step 4.2: Find handlers that write status codes directly without JSON**

```bash
grep -rn "w\.WriteHeader(" server/internal/api/ --include="*.go" | grep -v "_test.go" | \
  grep -v "apierr\|jsonReply"
```

- [ ] **Step 4.3: Check for swallowed errors**

```bash
grep -rn "_ = " server/internal/api/ --include="*.go" | grep -v "_test.go" | \
  grep -v "fmt.Fprintf\|flusher\|Close\|Body.Close"
```

Investigate each: is the error genuinely safe to ignore?

---

### Task 5: Write the Findings Document

- [ ] **Step 5.1: Create `docs/architecture/`**

```bash
mkdir -p docs/architecture
```

- [ ] **Step 5.2: Write `docs/architecture/server-review.md`**

Use this structure (fill in findings from Tasks 1–4):

```markdown
# Server Architecture Review — 2026-05-14

## Summary
[2-3 sentence executive summary of overall health]

## Layer Compliance
[Table: package → imports → status (✓ / violation)]
[List of violations with file:line references]

## Handler Responsibility
[Per-handler group findings]
[List of handlers with business logic that should move to services]

## Middleware Chain
[Ordered list with ✓/✗ for each item reviewed in Task 3]
[Missing headers or ordering issues]

## Error Handling
[List of inconsistencies found]
[Bare http.Error calls, missing apierr usage]

## Findings & Recommendations

### P0 — Fix now (correctness/security)
[Each P0 finding with file:line and recommended fix]

### P1 — Fix soon (maintainability)
[Each P1 finding]

### P2 — Consider later (nice-to-have)
[Each P2 finding]

## Action Items
- [ ] P0: [description] — `file:line`
- [ ] P1: [description] — `file:line`
```

---

### Task 6: Fix P0 Issues

- [ ] **Step 6.1: Apply each P0 fix inline**

For each P0 issue identified in the findings document, apply the minimal fix. Common P0 patterns:

**Upward import (e.g., `db` importing `pipeline`):**
Extract the dependency to an interface, inject via constructor.

**`context.Background()` in handler:**
Replace with `r.Context()`.

**Missing error check on `json.Encode`:**
```go
// Before:
json.NewEncoder(w).Encode(v)
// After:
if err := json.NewEncoder(w).Encode(v); err != nil {
    slog.Warn("encode response", "err", err)
}
```

- [ ] **Step 6.2: Run tests after each fix**

```bash
cd server && go test -race ./...
```

Expected: no failures.

---

### Task 7: Commit and push

- [ ] **Step 7.1: Commit doc + fixes together**

```bash
git add docs/architecture/server-review.md
git add server/  # add any fixed files
git commit -m "chore: server architecture review + P0 fixes

Audits layer compliance, handler responsibility, middleware chain,
and error handling across server/internal/. Fixes any P0 violations
found (upward imports, bare context.Background() in handlers).

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

- [ ] **Step 7.2: Push and open PR to `upcoming`**

```bash
git push -u origin chore/server-arch-review
```
