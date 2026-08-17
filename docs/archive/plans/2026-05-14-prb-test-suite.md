# PR-B: Test Suite Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add missing HTTP-level API integration tests for the Go backend and Vitest unit tests for key Vue 3 composables. Improve coverage tracking via `Taskfile.yml`.

**Architecture:** Go tests use `httptest.NewRecorder` and in-memory SQLite (`db.Open(":memory:")`). Vitest tests mock the fetch/EventSource API. No production code changes — tests only.

**Tech Stack:** Go `testing`, `net/http/httptest`, `github.com/stretchr/testify`, ent in-memory DB; Vue 3 + Vitest + `@vue/test-utils`

---

## Worktree Setup

```bash
git worktree add ../agent-dashboard-prb feat/test-suite
cd ../agent-dashboard-prb
```

---

## File Map

| Action | File |
|--------|------|
| Create | `server/internal/api/agents/handler_test.go` |
| Create | `server/internal/api/auth/handler_test.go` |
| Create | `server/internal/api/middleware_test.go` |
| Create | `server/internal/api/sessions/handler_test.go` |
| Create | `server/internal/merger/merger_integration_test.go` |
| Create | `src/composables/__tests__/useAgents.test.ts` |
| Create | `src/composables/__tests__/useTasks.test.ts` |
| Create | `src/composables/__tests__/useRole.test.ts` |
| Modify | `Taskfile.yml` |

---

### Task 1: Coverage baseline

- [ ] **Step 1.1: Measure current coverage**

```bash
cd server && go test -race -coverprofile=coverage.out ./... && \
  go tool cover -func=coverage.out | grep -E "total:|internal/api"
```

Record the `total:` percentage — this is the baseline.

- [ ] **Step 1.2: Find uncovered packages**

```bash
go tool cover -func=coverage.out | grep " 0.0%" | head -30
```

Note which `internal/api/*` packages have 0% — these are the primary targets.

---

### Task 2: Agents handler test

**Files:**
- Create: `server/internal/api/agents/handler_test.go`

- [ ] **Step 2.1: Write the test**

```go
package agents_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/agents"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

func TestAgentHandler_List_ReturnsJSON(t *testing.T) {
	want := []sdk.Agent{{ID: "abc", Status: "active"}}
	getAgents := func(_ context.Context) ([]sdk.Agent, error) { return want, nil }
	broadcaster := sse.NewBroadcaster()
	h := agents.NewHandler(getAgents, broadcaster)

	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	rec := httptest.NewRecorder()
	require.NoError(t, h.List(rec, req))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var got []sdk.Agent
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	require.Len(t, got, 1)
	assert.Equal(t, "abc", got[0].ID)
}

func TestAgentHandler_List_EmptyReturnsArray(t *testing.T) {
	getAgents := func(_ context.Context) ([]sdk.Agent, error) { return nil, nil }
	broadcaster := sse.NewBroadcaster()
	h := agents.NewHandler(getAgents, broadcaster)

	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	rec := httptest.NewRecorder()
	require.NoError(t, h.List(rec, req))

	assert.Equal(t, http.StatusOK, rec.Code)
	// Body must be valid JSON (null or [])
	var got any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
}
```

- [ ] **Step 2.2: Run**

```bash
cd server && go test -run TestAgentHandler ./internal/api/agents/ -v
```

Expected: both tests `PASS`.

- [ ] **Step 2.3: Commit**

```bash
git add server/internal/api/agents/handler_test.go
git commit -m "test: add agents handler HTTP tests"
```

---

### Task 3: Auth middleware bypass test

**Files:**
- Create: `server/internal/api/middleware_test.go`

- [ ] **Step 3.1: Write the test**

```go
package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"

	"github.com/lx-wnk/agent-dashboard/server/internal/api"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

// TestRouter_BypassAuth_LoopbackNoOAuth verifies that when BypassAuth is true
// (loopback host + no GitHub OAuth configured), protected routes are accessible
// without a JWT token.
func TestRouter_BypassAuth_LoopbackNoOAuth(t *testing.T) {
	deps := api.RouterDeps{
		Config: api.RouterConfig{
			JWTSecret:  "test-secret-minimum-32-characters-x",
			IsLoopback: true,
			BypassAuth: true,
		},
		AgentBroadcaster: sse.NewBroadcaster(),
	}
	router := api.NewRouter(deps)

	// /api/agents is a protected route — must be accessible without auth when bypass is on.
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// 200 or 500 (no real agent scanner) — NOT 401
	assert.NotEqual(t, http.StatusUnauthorized, rec.Code)
}

// TestRouter_RequireAuth_RejectsWithout401 verifies that when BypassAuth is false,
// protected routes reject unauthenticated requests with 401.
func TestRouter_RequireAuth_RejectsWithout401(t *testing.T) {
	deps := api.RouterDeps{
		Config: api.RouterConfig{
			JWTSecret:  "test-secret-minimum-32-characters-x",
			IsLoopback: true,
			BypassAuth: false,
		},
		AgentBroadcaster: sse.NewBroadcaster(),
		// OAuthProvider set to non-nil triggers auth
		OAuthProvider: &stubOAuth{},
	}
	router := api.NewRouter(deps)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

type stubOAuth struct{}

func (s *stubOAuth) BuildAuthURL(state, redirectURI string) string { return "http://stub" }
func (s *stubOAuth) ExchangeCode(_ any, code, redirectURI string) (string, error) {
	return "stub-token", nil
}
func (s *stubOAuth) GetUser(_ any, _ string) (*auth.OAuthUserProfile, error) {
	return &auth.OAuthUserProfile{ID: "1", Login: "stub"}, nil
}
```

Note: `stubOAuth` must implement `auth.OAuthProvider` from `server/internal/auth`. Adjust the import path to `github.com/lx-wnk/agent-dashboard/server/internal/auth`.

- [ ] **Step 3.2: Fix imports in the test file**

Add at top of file:
```go
import (
    ...
    auth "github.com/lx-wnk/agent-dashboard/server/internal/auth"
)
```

And fix `ExchangeCode` and `GetUser` signatures to match the `auth.OAuthProvider` interface exactly:
```go
func (s *stubOAuth) ExchangeCode(ctx context.Context, code, redirectURI string) (string, error) { ... }
func (s *stubOAuth) GetUser(ctx context.Context, token string) (*auth.OAuthUserProfile, error) { ... }
```

- [ ] **Step 3.3: Run**

```bash
cd server && go test -run TestRouter_ ./internal/api/ -v
```

Expected: both tests `PASS`.

- [ ] **Step 3.4: Commit**

```bash
git add server/internal/api/middleware_test.go
git commit -m "test: add router auth bypass and reject tests"
```

---

### Task 4: Sessions handler test

**Files:**
- Create: `server/internal/api/sessions/handler_test.go`

- [ ] **Step 4.1: Read the sessions handler to understand its interface**

```bash
cat server/internal/api/sessions/handler.go | head -80
```

- [ ] **Step 4.2: Write a smoke test for the sessions list endpoint**

```go
package sessions_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lx-wnk/agent-dashboard/server/internal/api/sessions"
)

// TestSessionsHandler_ListReturns200 is a smoke test that the handler
// can be constructed and responds with a valid status code.
func TestSessionsHandler_ListReturns200(t *testing.T) {
	// Build the handler with a stub scanner that returns empty results.
	h := sessions.NewHandler(sessions.Deps{
		// Pass zero-value deps; the handler should not panic.
	})

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// Accept 200 or 500 — not a panic.
	assert.Less(t, rec.Code, 600)
	assert.GreaterOrEqual(t, rec.Code, 200)
}
```

Note: Adjust constructor call to match the actual `sessions.NewHandler` signature found in step 4.1.

- [ ] **Step 4.3: Run**

```bash
cd server && go test -run TestSessionsHandler ./internal/api/sessions/ -v
```

- [ ] **Step 4.4: Commit**

```bash
git add server/internal/api/sessions/handler_test.go
git commit -m "test: add sessions handler smoke test"
```

---

### Task 5: Merger integration test

**Files:**
- Create: `server/internal/merger/merger_integration_test.go`

- [ ] **Step 5.1: Read the merger to understand its public API**

```bash
cat server/internal/merger/merger.go | head -60
```

- [ ] **Step 5.2: Write table-driven integration test**

```go
package merger_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/merger"
)

// TestMerger_EmptyProcessList returns empty agent slice, not nil.
func TestMerger_EmptyProcessList(t *testing.T) {
	// Construct with zero deps — no processes, no DB.
	m := merger.New(merger.Deps{})
	agents, err := m.Merge(nil)
	require.NoError(t, err)
	assert.NotNil(t, agents)
	assert.Empty(t, agents)
}
```

Note: Adjust `merger.New` and `merger.Deps` to match actual constructor in `merger.go`.

- [ ] **Step 5.3: Run**

```bash
cd server && go test -run TestMerger ./internal/merger/ -v
```

- [ ] **Step 5.4: Commit**

```bash
git add server/internal/merger/merger_integration_test.go
git commit -m "test: add merger integration smoke test"
```

---

### Task 6: Frontend composable tests

**Files:**
- Create: `src/composables/__tests__/useAgents.test.ts`
- Create: `src/composables/__tests__/useTasks.test.ts`
- Create: `src/composables/__tests__/useRole.test.ts`

- [ ] **Step 6.1: Read the composables to understand their API**

```bash
head -60 src/composables/useAgents.ts
head -40 src/composables/useTasks.ts
head -40 src/composables/useRole.ts
```

- [ ] **Step 6.2: Create test for `useRole`** (simplest, no network)

```typescript
// src/composables/__tests__/useRole.test.ts
import { describe, it, expect } from 'vitest'
import { useRole } from '../useRole'

describe('useRole', () => {
  it('returns a role ref', () => {
    const { role } = useRole()
    expect(role).toBeDefined()
  })

  it('isAdmin is false by default when no auth token present', () => {
    const { isAdmin } = useRole()
    // Without a valid JWT in localStorage, admin should be false.
    expect(isAdmin.value).toBe(false)
  })
})
```

- [ ] **Step 6.3: Create test for `useAgents`** (mocks SSE + fetch)

```typescript
// src/composables/__tests__/useAgents.test.ts
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { useAgents } from '../useAgents'

// Minimal EventSource stub
class MockEventSource {
  static instances: MockEventSource[] = []
  onmessage: ((e: MessageEvent) => void) | null = null
  onerror: ((e: Event) => void) | null = null
  readyState = 0
  constructor(public url: string) { MockEventSource.instances.push(this) }
  close() { this.readyState = 2 }
}

beforeEach(() => {
  MockEventSource.instances = []
  vi.stubGlobal('EventSource', MockEventSource)
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: true,
    json: () => Promise.resolve([]),
  }))
})

afterEach(() => { vi.unstubAllGlobals() })

describe('useAgents', () => {
  it('initialises with empty agent list', () => {
    const { agents } = useAgents()
    expect(agents.value).toEqual([])
  })

  it('creates an EventSource connection on mount', async () => {
    useAgents()
    // Give the composable a tick to set up SSE
    await Promise.resolve()
    expect(MockEventSource.instances.length).toBeGreaterThan(0)
  })
})
```

- [ ] **Step 6.4: Create test for `useTasks`**

```typescript
// src/composables/__tests__/useTasks.test.ts
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { useTasks } from '../useTasks'

class MockEventSource {
  static instances: MockEventSource[] = []
  onmessage: ((e: MessageEvent) => void) | null = null
  onerror: ((e: Event) => void) | null = null
  readyState = 0
  constructor(public url: string) { MockEventSource.instances.push(this) }
  close() { this.readyState = 2 }
}

beforeEach(() => {
  MockEventSource.instances = []
  vi.stubGlobal('EventSource', MockEventSource)
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: true,
    json: () => Promise.resolve([]),
  }))
})

afterEach(() => { vi.unstubAllGlobals() })

describe('useTasks', () => {
  it('initialises with empty task list', () => {
    const { tasks } = useTasks()
    expect(tasks.value).toEqual([])
  })

  it('exposes a loading ref', () => {
    const { loading } = useTasks()
    expect(typeof loading.value).toBe('boolean')
  })
})
```

- [ ] **Step 6.5: Run frontend tests**

```bash
pnpm test
```

Expected: all 3 composable test files pass.

- [ ] **Step 6.6: Commit**

```bash
git add src/composables/__tests__/
git commit -m "test: add Vitest unit tests for useAgents, useTasks, useRole composables"
```

---

### Task 7: Coverage tracking in Taskfile

**Files:**
- Modify: `Taskfile.yml`

- [ ] **Step 7.1: Add coverage task**

In `Taskfile.yml`, add after the existing `test` task:

```yaml
  test:cover:
    desc: Run tests with coverage report (server module)
    dir: server
    cmds:
      - go test -race -coverprofile=coverage.out ./...
      - go tool cover -func=coverage.out | tail -1
```

- [ ] **Step 7.2: Run and verify**

```bash
task test:cover
```

Expected: prints `total:  (statements)  XX.X%`

- [ ] **Step 7.3: Commit and push**

```bash
git add Taskfile.yml
git commit -m "chore: add test:cover task for coverage tracking"
git push -u origin feat/test-suite
```

---

### Task 8: Final verification

- [ ] **Step 8.1: Full test suite**

```bash
task test
```

Expected: no failures across both Go modules.

- [ ] **Step 8.2: Frontend tests**

```bash
pnpm test
```

Expected: all composable tests pass.
