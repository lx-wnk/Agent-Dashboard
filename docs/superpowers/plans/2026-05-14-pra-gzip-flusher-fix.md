# PR-A: Gzip Flusher Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix `gzipResponseWriter` to implement `http.Flusher` and add the missing `Vary: Accept-Encoding` response header.

**Architecture:** Two-line struct method addition + one header set in `gzipMiddleware`. Entirely contained in `server/internal/api/router.go`. Test added in `server/internal/api/router_test.go` (new file).

**Tech Stack:** Go stdlib `compress/gzip`, `net/http`, `net/http/httptest`, `testing`

---

## Worktree Setup

```bash
git worktree add ../agent-dashboard-pra fix/gzip-flusher
cd ../agent-dashboard-pra/server
```

---

## File Map

| Action | File |
|--------|------|
| Modify | `server/internal/api/router.go` (lines 227–258) |
| Create | `server/internal/api/router_test.go` |

---

### Task 1: Write the failing test

**Files:**
- Create: `server/internal/api/router_test.go`

- [ ] **Step 1.1: Create test file**

```go
package api

import (
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGzipResponseWriter_ImplementsFlusher verifies that gzipResponseWriter
// satisfies http.Flusher so SSE-adjacent handlers can flush through gzip.
func TestGzipResponseWriter_ImplementsFlusher(t *testing.T) {
	rec := httptest.NewRecorder()
	gz, err := gzip.NewWriterLevel(rec, gzip.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	w := &gzipResponseWriter{ResponseWriter: rec, Writer: gz}

	_, ok := any(w).(http.Flusher)
	if !ok {
		t.Fatal("gzipResponseWriter does not implement http.Flusher")
	}
}

// TestGzipMiddleware_SetsVaryHeader verifies Vary: Accept-Encoding is present
// in gzip-compressed responses.
func TestGzipMiddleware_SetsVaryHeader(t *testing.T) {
	handler := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	vary := rec.Header().Get("Vary")
	if vary != "Accept-Encoding" {
		t.Errorf("expected Vary: Accept-Encoding, got %q", vary)
	}
}

// TestGzipMiddleware_FlushForwardsToInner verifies that Flush() on a
// gzipResponseWriter does not panic and propagates to the inner ResponseWriter.
func TestGzipMiddleware_FlushForwardsToInner(t *testing.T) {
	flushed := false
	inner := &fakeFlushWriter{ResponseRecorder: httptest.NewRecorder(), onFlush: func() { flushed = true }}

	gz, err := gzip.NewWriterLevel(inner, gzip.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	w := &gzipResponseWriter{ResponseWriter: inner, Writer: gz}

	flusher, ok := any(w).(http.Flusher)
	if !ok {
		t.Fatal("gzipResponseWriter does not implement http.Flusher")
	}
	flusher.Flush()

	if !flushed {
		t.Error("Flush() did not propagate to inner writer")
	}
}

type fakeFlushWriter struct {
	*httptest.ResponseRecorder
	onFlush func()
}

func (f *fakeFlushWriter) Flush() { f.onFlush() }
```

- [ ] **Step 1.2: Run test to confirm it fails**

```bash
cd server && go test -run TestGzipResponseWriter_ImplementsFlusher ./internal/api/
```

Expected output: `FAIL` — `gzipResponseWriter does not implement http.Flusher`

---

### Task 2: Implement the fix

**Files:**
- Modify: `server/internal/api/router.go`

- [ ] **Step 2.1: Add `Flush()` method to `gzipResponseWriter`**

In `server/internal/api/router.go`, after the existing `Write` method (around line 233), add:

```go
func (g *gzipResponseWriter) Flush() {
	_ = g.Writer.Flush()
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
```

- [ ] **Step 2.2: Add `Vary: Accept-Encoding` header in `gzipMiddleware`**

In `gzipMiddleware`, immediately after `w.Header().Set("Content-Encoding", "gzip")` (around line 255), add:

```go
w.Header().Set("Vary", "Accept-Encoding")
```

The block should now read:
```go
gz, err := gzip.NewWriterLevel(w, gzip.BestSpeed)
if err != nil {
    next.ServeHTTP(w, r)
    return
}
defer gz.Close()
w.Header().Set("Content-Encoding", "gzip")
w.Header().Del("Content-Length")
w.Header().Set("Vary", "Accept-Encoding")
next.ServeHTTP(&gzipResponseWriter{ResponseWriter: w, Writer: gz}, r)
```

---

### Task 3: Verify all tests pass

- [ ] **Step 3.1: Run all three new tests**

```bash
cd server && go test -run "TestGzipResponseWriter_ImplementsFlusher|TestGzipMiddleware_SetsVaryHeader|TestGzipMiddleware_FlushForwardsToInner" ./internal/api/ -v
```

Expected: all three `PASS`

- [ ] **Step 3.2: Run the full server test suite**

```bash
cd server && go test -race ./...
```

Expected: no failures

---

### Task 4: Commit and push

- [ ] **Step 4.1: Commit**

```bash
git add server/internal/api/router.go server/internal/api/router_test.go
git commit -m "fix: gzipResponseWriter implements http.Flusher + add Vary header

gzipResponseWriter.Flush() now flushes the gzip buffer and forwards to
the inner http.Flusher. The Vary: Accept-Encoding header is now set on
all gzip-compressed responses to prevent incorrect proxy caching.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

- [ ] **Step 4.2: Push branch and open PR to `upcoming`**

```bash
git push -u origin fix/gzip-flusher
```
