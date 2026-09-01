# AgenticOS Visibility Wave Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the four AgenticOS surfaces that exist today only as HTTP endpoints or repo methods — memory spaces and entries, memory injections, the resource registry, and a grant's enforceability — a visible interface in the dashboard.

**Architecture:** Three of the four deliverables are client-only: a Vue composable per surface that wraps `fetch`, plus a panel registered in the Settings modal (`ApiKeySettings.vue`) or a block inside the task modal's Stages tab. The fourth adds one read-only Go route, `GET /api/resources`, over the already-complete `repo.ResourceRepo`, answering a hand-written camelCase DTO rather than a raw `*ent.Resource`. No schema change, no new table, no new capability: every backing query, gate and constant already exists.

**Tech Stack:** Go 1.26 (chi, ent ORM), Vue 3 + TypeScript SPA (Vite, pnpm, Vitest, Vue Test Utils)

**Spec:** `docs/superpowers/specs/2026-08-27-agenticos-overview-design.md` — this plan serves its exit criterion's final clause, "with every step visible in the UI".

## Global Constraints

- **HARD DEPENDENCY — Tasks 4, 5 and 6 are blocked.** The sibling plan `docs/superpowers/plans/2026-09-01-wire-format-dto-sweep.md` converts the `/api/memory/*` routes from raw ent entities to camelCase DTOs. Today those routes emit snake_case (`space_id`, `created_at`, `stage_run_id`, `char_budget`, …) because they encode `*ent.Resource` / `*ent.MemoryInjection` straight out, and `GET /api/memory/entries` emits **PascalCase** (`ID`, `SpaceID`, `Summary`, `CreatedAt`) because `memory.Entry` (`server/internal/memory/retrieve.go:48-56`) carries no json tags at all. Tasks 4 and 5 (memory panel) and Task 6 (injections view) MUST be built against the post-sweep camelCase shape and MUST NOT start until that plan's memory task has merged. Tasks 1, 2 and 3 have no such dependency and run first, in order. Task 5 additionally depends on Task 4, and Task 3 on Task 2.
- **Never run `go test ./...` or `task test`.** Both regenerate `server/internal/db/ent/`, which then shows up as unrelated noise in the diff. Scope every Go test run to the package under change. If the tree is already dirty under `server/internal/db/ent/`, restore it with `git checkout -- server/internal/db/ent/`.
- **`gofmt -l` is mandatory in every Go gate.** CI runs `golangci-lint fmt --diff`, which fails on struct-field and comment alignment that `go build`, `go vet` and `go test` all accept. A green build is not evidence of a green CI.
- **The server stays bound to `127.0.0.1`.** Nothing in this plan opens a listener; do not change any bind address while working in `server/`.
- **Adding a route breaks the route golden.** `server/internal/api/testdata/routes.golden` is asserted by `server/internal/api/route_golden_test.go`. Task 2 adds a line to it deliberately; regenerate with `go test -count=1 ./internal/api/ -run TestRouteGolden -update-golden` and review the diff — exactly one added line is expected.
- **Cross-feature deep imports are an ESLint error.** `eslint.config.ts` registers a local `boundary/feature-internals` rule: a file under `src/features/<a>/` may not import `@/features/<b>/composables/...`, only the barrel `@/features/<b>`. Every composable this plan adds is used by exactly one feature and lives inside that feature, so no barrel is needed and none is added.
- **Never add a manual `unmount()` to a settings-panel test.** `src/test/setup.ts:20` calls `enableAutoUnmount(afterEach)` globally — added after a forgotten `unmount()` let a live poller keep writing into shared module-level mocks across test files (#327). Wrappers are torn down for you. Do not re-add per-test unmounts, and do not omit `attachTo: document.body`, which the `AppSelect` option-panel helper depends on.
- **One commit per task**, Conventional Commits (`feat:`, `fix:`, `refactor:`), English subject and body, no phase or task-number references in the message.
- **Gate per task.**
  - Go: `cd server && go build ./... && go vet ./... && gofmt -l ./internal/api/ && go test -count=1 ./internal/api/<pkg>/...`
  - Frontend: `pnpm lint && pnpm typecheck && pnpm test`
  - Paste the raw output. A summary is not evidence.

---

### Task 1: Show a grant's enforceability in the Grants panel

**Problem.** `dashboard grants list` prints an `ENFORCEMENT` column (`server` / `none`, `server/internal/cli/cmd_grants.go:238`) that the Settings panel does not. A user can therefore create a grant from the UI for a capability no enforcement point reads, and the UI reports success with no hint that the grant is inert. The CLI even warns on `grants add` (`cmd_grants.go:46`); the HTTP path does not.

**No backend change.** `GET /api/capabilities` already ships `enforceable_by` (`server/internal/db/ent/schema/capability.go`, `server/internal/api/grants/handler.go:69-79`) and `useGrants.ts:11` already types it as `string[]`. `ent.Grant` correctly has no enforcement field — enforceability is a property of the capability, not of the grant — so nothing on the server needs touching.

### Files

- `src/features/settings/components/GrantSettings.vue` — computed lookup + one table column
- `src/features/settings/components/GrantSettings.test.ts` — one new test

### Steps

- [ ] **RED — add the failing test.** Append to the `describe('grantSettings', …)` block in `src/features/settings/components/GrantSettings.test.ts`, after the existing `renders the grant list…` test:

```ts
  it('marks a grant on a capability no enforcement point reads as not enforced', () => {
    capabilities.value = [
      { id: 'c1', name: 'bash.exec', class: 'shell', enforceable_by: [], requires_pattern: true, reversible: false, description: '' },
      { id: 'c2', name: 'memory.read', class: 'resource', enforceable_by: ['server'], requires_pattern: false, reversible: true, description: '' },
    ]
    grants.value = [
      { ...baseGrant, id: 'g1', capability_name: 'bash.exec' },
      { ...baseGrant, id: 'g2', capability_name: 'memory.read' },
      { ...baseGrant, id: 'g3', capability_name: 'not.in.catalogue' },
    ]
    const wrapper = mount(GrantSettings, { attachTo: document.body })

    expect(wrapper.get('[data-testid="grant-enforcement-g1"]').text()).toBe('none')
    expect(wrapper.get('[data-testid="grant-enforcement-g2"]').text()).toBe('server')
    expect(wrapper.get('[data-testid="grant-enforcement-g3"]').text()).toBe('unknown')
  })
```

- [ ] **Run it and confirm it fails for the right reason.** `pnpm test src/features/settings/components/GrantSettings.test.ts` — expect three failures of the form `Unable to get [data-testid="grant-enforcement-g1"] within: <div>`. A failure with any other message means the fixture is wrong, not the feature.

- [ ] **GREEN — add the lookup.** In `src/features/settings/components/GrantSettings.vue`, in the `// ── Display helpers ──` block (after `formatLimit`, before `isLegacy`), add:

```ts
// Mirrors isServerEnforceable / enforcementByCapability in
// server/internal/cli/cmd_grants.go:252-274 — "server" is the one production
// enforcement point that reads stored grants (internal/memory.Authorize), so a
// capability that does not name it produces a grant the system records and
// never applies.
const ENFORCER_SERVER = 'server'

const enforcementByCapability = computed(() => {
  const byName = new Map<string, string>()
  for (const c of capabilities.value)
    byName.set(c.name, c.enforceable_by.includes(ENFORCER_SERVER) ? ENFORCER_SERVER : 'none')
  return byName
})

// A grant whose capability is absent from the catalogue answers "unknown"
// rather than "none": the catalogue may simply not have loaded yet, and
// claiming the grant is inert would be a stronger statement than the data
// supports. Same fallback as the CLI table.
function enforcementOf(capabilityName: string): string {
  return enforcementByCapability.value.get(capabilityName) ?? 'unknown'
}
```

- [ ] **GREEN — add the table column.** In the same file, in the `<thead>` row, insert a header between the `Mode` and `Limit` headers:

```html
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Enforcement
          </th>
```

  and in the `<tbody>` row, insert the matching cell between the `mode` cell and the `formatLimit` cell:

```html
          <td class="px-3 py-2.5 border-b border-line">
            <span
              v-if="enforcementOf(g.capability_name) === 'server'"
              class="inline-block rounded px-2 py-0.5 text-[11px] font-semibold bg-success-soft text-success-text"
              :data-testid="`grant-enforcement-${g.id}`"
              title="Enforced server-side by internal/memory.Authorize"
            >server</span>
            <span
              v-else
              class="inline-block rounded px-2 py-0.5 text-[11px] font-semibold bg-warning-soft text-warning-text"
              :data-testid="`grant-enforcement-${g.id}`"
              title="No enforcement point reads stored grants for this capability today — the grant is recorded and will apply once a reader exists"
            >{{ enforcementOf(g.capability_name) }}</span>
          </td>
```

- [ ] **Run the test and see it green.** `pnpm test src/features/settings/components/GrantSettings.test.ts`.

- [ ] **Run the full gate and paste the raw output.** `pnpm lint && pnpm typecheck && pnpm test`

- [ ] **Commit.** `feat(ui): show whether a grant's capability is actually enforced`

---

### Task 2: Read-only HTTP route over the resource registry

**Problem.** `repo.ResourceRepo` (`server/internal/db/repo/resource_repo.go:59-72`) is complete — `Get`, `Resolve`, `ListMerged`, `ListForKind`, `ListForScope`, `SetState`, `Delete` — and **no HTTP route exposes any of it.** Verified: `grep -rn "ResourceRepo" server/internal/api/` returns nothing. The only window onto the `resource` table is `GET /api/memory/spaces`, hard-filtered to `kind = memory_space` by `MemoryRepo.ListSpaces`. The three other kinds — `application`, `routine`, `skill` — have zero read exposure, including the Obsidian application that `serverapp/di.go:290` registers on every boot.

**Not gated by a capability.** `GET /api/grants` and `GET /api/capabilities` are read-only catalogue routes mounted in the session-authenticated group with no capability check, and this route is their sibling: it returns registry identity metadata, no content. It gets the same treatment. Do not add a `resource.read` capability — no such capability is seeded (`server/internal/db/repo/capability_seed.go`) and inventing one would put a permanent fail-closed gate in front of a list the UI needs to render on first load.

**Expect `kind=memory_space` to disagree with `GET /api/memory/spaces`.** `MemoryRepo.ListSpaces` (`server/internal/db/repo/memory_repo.go:159-165`) calls `ListForScope` — the scope's own rows only. This route calls `ListMerged` — global rows plus the scope's, scope winning a slug collision. For a non-global scope the two lists therefore differ, deliberately: the registry panel answers "what resolves here", the memory panel answers "what is defined here". Do not "fix" one to match the other.

### Files

- `server/internal/api/resources/handler.go` (new)
- `server/internal/api/resources/handler_test.go` (new)
- `server/internal/api/router.go` — `Deps` field + nil-guarded mount
- `server/internal/api/testdata/routes.golden` — one added line
- `server/serverapp/di.go` — hoist `resourceRepo`, build the handler, wire it into `Deps`

### Steps

- [ ] **RED — write the test first.** Create `server/internal/api/resources/handler_test.go`. The fixture mirrors `server/internal/api/memory/handler_test.go:26-40`: a real in-memory SQLite database through `db.Open(":memory:")`, because `ResourceRepo` has no interface fake and `Upsert` relies on a real unique index for its `OnConflictColumns` path.

```go
package resources_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	apiresources "github.com/lx-wnk/agent-dashboard/server/internal/api/resources"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// newMux wires an apiresources.Handler against a real in-memory SQLite
// database and returns the mux plus the repo tests seed fixtures through.
func newMux(t *testing.T) (*chi.Mux, repo.ResourceRepo, context.Context) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })

	resourceRepo := repo.NewResourceRepo(bundle.Client)
	mux := chi.NewRouter()
	apiresources.NewHandler(resourceRepo).Mount(mux)
	return mux, resourceRepo, context.Background()
}

func get(t *testing.T, mux *chi.Mux, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func decodeList(t *testing.T, w *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	var out []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	return out
}

func TestList_AnswersCamelCaseDTO(t *testing.T) {
	mux, resourceRepo, ctx := newMux(t)
	_, err := resourceRepo.Upsert(ctx, repo.UpsertResourceInput{
		Kind:      repo.ResourceKindApplication,
		Slug:      "obsidian",
		Name:      "Obsidian",
		Scope:     repo.GlobalScope(),
		State:     repo.ResourceStateEnabled,
		Version:   "1.0.0",
		Origin:    repo.ResourceOriginBuiltin,
		OriginRef: "builtin:obsidian",
	})
	require.NoError(t, err)

	w := get(t, mux, "/api/resources?kind=application")
	require.Equal(t, http.StatusOK, w.Code)

	rows := decodeList(t, w)
	require.Len(t, rows, 1)
	row := rows[0]
	require.Equal(t, "obsidian", row["slug"])
	require.Equal(t, "Obsidian", row["name"])
	require.Equal(t, "application", row["kind"])
	require.Equal(t, "global", row["scopeKind"])
	require.Equal(t, "", row["scopeRef"])
	require.Equal(t, "local", row["nodeId"])
	require.Equal(t, "enabled", row["state"])
	require.Equal(t, "1.0.0", row["version"])
	require.Equal(t, "builtin", row["origin"])
	require.Equal(t, "builtin:obsidian", row["originRef"])
	require.Contains(t, row, "createdAt")
	require.Contains(t, row, "updatedAt")

	// The wire shape is camelCase, never ent's snake_case struct tags — the
	// whole reason this is a hand-written DTO rather than *ent.Resource.
	for _, snake := range []string{"scope_kind", "scope_ref", "node_id", "origin_ref", "created_at", "updated_at"} {
		require.NotContains(t, row, snake)
	}
}

func TestList_EmptyKindIsAnEmptyArrayNotNull(t *testing.T) {
	mux, _, _ := newMux(t)

	w := get(t, mux, "/api/resources?kind=routine")
	require.Equal(t, http.StatusOK, w.Code)
	// routine and skill have no writer anywhere in the codebase yet, so this
	// list is legitimately empty. It must encode as [] so the client can
	// render "none yet" instead of crashing on a null.
	require.Equal(t, "[]\n", w.Body.String())
}

func TestList_ScopeRowShadowsGlobalOnSlugCollision(t *testing.T) {
	mux, resourceRepo, ctx := newMux(t)
	_, err := resourceRepo.Upsert(ctx, repo.UpsertResourceInput{
		Kind: repo.ResourceKindMemorySpace, Slug: "notes", Name: "Global notes",
		Scope: repo.GlobalScope(),
	})
	require.NoError(t, err)
	_, err = resourceRepo.Upsert(ctx, repo.UpsertResourceInput{
		Kind: repo.ResourceKindMemorySpace, Slug: "notes", Name: "Project notes",
		Scope: repo.ProjectScope("/tmp/demo"),
	})
	require.NoError(t, err)

	rows := decodeList(t, get(t, mux, "/api/resources?kind=memory_space&scope=project&scopeRef=/tmp/demo"))
	require.Len(t, rows, 1)
	require.Equal(t, "Project notes", rows[0]["name"])
	require.Equal(t, "project", rows[0]["scopeKind"])
}

func TestList_RejectsMissingAndUnknownKind(t *testing.T) {
	mux, _, _ := newMux(t)

	require.Equal(t, http.StatusBadRequest, get(t, mux, "/api/resources").Code)

	w := get(t, mux, "/api/resources?kind=nonsense")
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "unknown kind")
}

func TestList_RejectsScopeMissingItsRef(t *testing.T) {
	mux, _, _ := newMux(t)
	// Fails closed rather than silently answering the global scope's rows —
	// the same rule memory.ParseScope enforces for every other transport.
	w := get(t, mux, "/api/resources?kind=application&scope=project")
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "scopeRef is required")
}
```

- [ ] **Run it and confirm it fails for the right reason.** `cd server && go test -count=1 ./internal/api/resources/...` — expect a build failure, `no required module provides package .../internal/api/resources` or `no Go files in .../internal/api/resources`. Anything else means the test file is wrong.

- [ ] **GREEN — write the handler.** Create `server/internal/api/resources/handler.go`:

```go
// Package resources implements the read-only HTTP surface over the ARMS
// resource registry: GET /api/resources. The registry is the identity table
// behind applications, routines, skills and memory spaces; until this route,
// GET /api/memory/spaces was the only window onto it and it is hard-filtered
// to kind = memory_space.
package resources

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	mem "github.com/lx-wnk/agent-dashboard/server/internal/memory"
)

// Handler serves the resource registry read view. Read-only by construction:
// state transitions and deletion stay with the subsystem that owns the
// resource (the plugin lifecycle handler, the memory handler), so this route
// cannot become a second, unvalidated write path into the same table.
type Handler struct {
	resources repo.ResourceRepo
}

// NewHandler creates a Handler.
func NewHandler(r repo.ResourceRepo) *Handler {
	return &Handler{resources: r}
}

// Mount registers the resource registry routes on r.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/api/resources", apierr.ErrorMiddleware(h.list))
}

// resourceKinds is the set a ?kind= may name, ordered as the UI lists them.
// Enumerated here rather than derived from the table's distinct values so a
// typo is rejected as unknown even when the registry is empty.
var resourceKinds = []string{
	repo.ResourceKindApplication,
	repo.ResourceKindRoutine,
	repo.ResourceKindSkill,
	repo.ResourceKindMemorySpace,
}

func isValidKind(kind string) bool {
	for _, k := range resourceKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// resourceView is the camelCase JSON shape of a registry row. Hand-written
// rather than encoding *ent.Resource directly: ent's generated struct tags are
// snake_case, and a raw entity on the wire also republishes every column added
// to the schema later, whether or not the client should see it.
type resourceView struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	ScopeKind string    `json:"scopeKind"`
	ScopeRef  string    `json:"scopeRef"`
	NodeID    string    `json:"nodeId"`
	State     string    `json:"state"`
	Version   string    `json:"version"`
	Origin    string    `json:"origin"`
	OriginRef string    `json:"originRef"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func viewOf(row *ent.Resource) resourceView {
	return resourceView{
		ID:        row.ID,
		Kind:      row.Kind,
		Slug:      row.Slug,
		Name:      row.Name,
		ScopeKind: row.ScopeKind,
		ScopeRef:  row.ScopeRef,
		NodeID:    row.NodeID,
		State:     row.State,
		Version:   row.Version,
		Origin:    row.Origin,
		OriginRef: row.OriginRef,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

// scopeFromQuery parses the shared "scope"/"scopeRef" query params through
// mem.ParseScope — the one parser every transport that accepts a caller-
// supplied scope uses (MCP tool args, the memory HTTP routes), so the accepted
// set cannot drift between them.
func scopeFromQuery(q url.Values) (repo.Scope, error) {
	scope, err := mem.ParseScope(q.Get("scope"), q.Get("scopeRef"))
	if err != nil {
		return repo.Scope{}, apierr.NewAppError(http.StatusBadRequest, err.Error())
	}
	return scope, nil
}

// list answers GET /api/resources?kind=&scope=&scopeRef=.
func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()

	kind := q.Get("kind")
	if kind == "" {
		return apierr.NewAppError(http.StatusBadRequest,
			fmt.Sprintf("kind is required (valid: %s)", strings.Join(resourceKinds, ", ")))
	}
	if !isValidKind(kind) {
		return apierr.NewAppError(http.StatusBadRequest,
			fmt.Sprintf("unknown kind %q (valid: %s)", kind, strings.Join(resourceKinds, ", ")))
	}

	scope, err := scopeFromQuery(q)
	if err != nil {
		return err
	}

	// ListMerged, not ListForScope: the UI wants the effective set a caller in
	// this scope would resolve — the global rows plus the scope's own, the
	// scope winning a slug collision — which is the same rule
	// ResourceRepo.Resolve applies to a single slug.
	rows, err := h.resources.ListMerged(r.Context(), kind, scope)
	if err != nil {
		return fmt.Errorf("resources.list: %w", err)
	}

	// make(..., 0, n) rather than a nil slice: a nil encodes as null, and a
	// kind with no writer yet (routine, skill) would then reach the client as
	// null on every request.
	out := make([]resourceView, 0, len(rows))
	for _, row := range rows {
		out = append(out, viewOf(row))
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(out)
}
```

- [ ] **Run the package test and see it green.** `cd server && go test -count=1 ./internal/api/resources/...`

- [ ] **Wire the route into the router.** In `server/internal/api/router.go`, add the field to `Deps` immediately after `MemoryHandler` (line ~174):

```go
	ResourcesHandler       *resources.Handler
```

  add the import next to the other API package imports:

```go
	"github.com/lx-wnk/agent-dashboard/server/internal/api/resources"
```

  and add the nil-guarded mount immediately after the `MemoryHandler` block (line ~418):

```go
		// Read-only registry catalogue; sits with the other session-authenticated
		// read routes and is never mounted in the hook/MCP bearer-token group.
		if deps.ResourcesHandler != nil {
			deps.ResourcesHandler.Mount(r)
		}
```

- [ ] **Wire the handler in DI.** In `server/serverapp/di.go`, hoist `resourceRepo` out of the `if entClient != nil` block so the `Deps` literal can reach it. Change the declaration block at line ~251 from

```go
	var pluginRepo repo.PluginRepo
	if entClient != nil {
```

  to

```go
	var pluginRepo repo.PluginRepo
	// Hoisted out of the block below so the resources HTTP handler, built near
	// the other handlers, can share this one instance.
	var resourceRepo repo.ResourceRepo
	if entClient != nil {
```

  and change line ~262 from `resourceRepo := repo.NewResourceRepo(entClient)` to `resourceRepo = repo.NewResourceRepo(entClient)`. Then, next to the `memoryHandler` construction (line ~578), add:

```go
	var resourcesHandler *apiresources.Handler
	if entClient != nil {
		resourcesHandler = apiresources.NewHandler(resourceRepo)
	}
```

  with the import `apiresources "github.com/lx-wnk/agent-dashboard/server/internal/api/resources"`, and add to the `Deps` literal beside `MemoryHandler:` (line ~802):

```go
		ResourcesHandler:       resourcesHandler,
```

- [ ] **Regenerate the route golden and review the diff.** `cd server && go test -count=1 ./internal/api/ -run TestRouteGolden -update-golden && git diff internal/api/testdata/routes.golden` — exactly one added line, `GET /api/resources`, is expected. Any other change means something else was rewired.

- [ ] **Run the full Go gate and paste the raw output.** `cd server && go build ./... && go vet ./... && gofmt -l ./internal/api/ && go test -count=1 ./internal/api/resources/... ./internal/api/`

- [ ] **Check the ent tree is clean.** `git status --short server/internal/db/ent/` must come back empty. If not: `git checkout -- server/internal/db/ent/`.

- [ ] **Commit.** `feat(api): expose the resource registry over a read-only route`

---

### Task 3: Registry panel in Settings

**Problem.** With Task 2 merged the registry is readable over HTTP and still invisible. This adds the panel.

**Expect empty lists.** `routine` and `skill` have no writer anywhere in the codebase — nothing calls `ResourceRepo.Upsert` with those kinds. Their lists will be legitimately empty and the panel must say so as a normal state ("None yet"), never as an error or a spinner that never resolves. `application` will hold at least the Obsidian row `serverapp/di.go:290` registers on every boot; `memory_space` will hold whatever `MemoryRepo.CreateSpace` has written.

### Files

- `src/features/settings/composables/useResources.ts` (new)
- `src/features/settings/components/ResourceSettings.vue` (new)
- `src/features/settings/components/ResourceSettings.test.ts` (new)
- `src/features/settings/components/ApiKeySettings.vue` — three edits (union, `SECTIONS`, template branch)

### Steps

- [ ] **Write the composable.** Create `src/features/settings/composables/useResources.ts`:

```ts
import { onMounted, ref } from 'vue'
import { errorMessage } from '@/utils/errorMessage'

// Mirrors resourceView in server/internal/api/resources/handler.go — a
// hand-written camelCase DTO, not an ent row, so these names are the
// codebase's normal convention rather than useGrants' snake_case exception.
export interface ResourceView {
  id: string
  kind: string
  slug: string
  name: string
  scopeKind: string
  scopeRef: string
  nodeId: string
  state: string
  version: string
  origin: string
  originRef: string
  createdAt: string
  updatedAt: string
}

// Mirrors resourceKinds in server/internal/api/resources/handler.go, same
// order. The route answers 400 for anything else.
export const RESOURCE_KINDS = ['application', 'routine', 'skill', 'memory_space'] as const
export type ResourceKind = typeof RESOURCE_KINDS[number]

export const RESOURCE_KIND_LABELS: Record<ResourceKind, string> = {
  application: 'Applications',
  routine: 'Routines',
  skill: 'Skills',
  memory_space: 'Memory spaces',
}

// Mirrors memory.ScopeKinds (server/internal/memory/authorize.go:16), which is
// the accepted set for every transport's scope/scopeRef pair.
export const RESOURCE_SCOPE_KINDS = ['global', 'project', 'application'] as const
export type ResourceScopeKind = typeof RESOURCE_SCOPE_KINDS[number]

export interface ResourceQuery {
  kind: ResourceKind
  scopeKind: ResourceScopeKind
  scopeRef: string
}

export function useResources() {
  const resources = ref<ResourceView[]>([])
  const query = ref<ResourceQuery>({ kind: 'application', scopeKind: 'global', scopeRef: '' })
  const loading = ref(true)
  const error = ref<string | null>(null)

  async function fetchResources(next?: Partial<ResourceQuery>): Promise<void> {
    if (next)
      query.value = { ...query.value, ...next }
    const { kind, scopeKind, scopeRef } = query.value

    // The server refuses a non-global scope with no ref (memory.ParseScope) —
    // hold the request rather than firing a known-400 on every keystroke.
    if (scopeKind !== 'global' && scopeRef.trim() === '') {
      resources.value = []
      loading.value = false
      error.value = null
      return
    }

    loading.value = true
    error.value = null
    try {
      const params = new URLSearchParams({ kind, scope: scopeKind })
      if (scopeKind !== 'global')
        params.set('scopeRef', scopeRef.trim())
      const res = await fetch(`/api/resources?${params.toString()}`)
      if (!res.ok)
        throw new Error(`Failed to load ${kind} resources (HTTP ${res.status})`)
      resources.value = await res.json()
    }
    catch (e) {
      // Clear on failure: keeping the previous kind's rows on screen under a
      // new kind's heading would misreport what the registry holds.
      resources.value = []
      error.value = errorMessage(e, 'Failed to load resources')
    }
    finally {
      loading.value = false
    }
  }

  onMounted(() => {
    void fetchResources()
  })

  return { resources, query, loading, error, fetchResources }
}
```

- [ ] **RED — write the component test.** Create `src/features/settings/components/ResourceSettings.test.ts`. No manual `unmount()` — `src/test/setup.ts:20` handles teardown globally.

```ts
import type { Ref } from 'vue'
import type { ResourceQuery, ResourceView } from '@/features/settings/composables/useResources'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import ResourceSettings from '@/features/settings/components/ResourceSettings.vue'
import { useResources } from '@/features/settings/composables/useResources'

vi.mock('@/features/settings/composables/useResources', async () => {
  const actual = await vi.importActual<typeof import('@/features/settings/composables/useResources')>('@/features/settings/composables/useResources')
  return {
    ...actual,
    useResources: vi.fn(),
  }
})

const baseResource: ResourceView = {
  id: 'r1',
  kind: 'application',
  slug: 'obsidian',
  name: 'Obsidian',
  scopeKind: 'global',
  scopeRef: '',
  nodeId: 'local',
  state: 'enabled',
  version: '1.0.0',
  origin: 'builtin',
  originRef: 'builtin:obsidian',
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-02T00:00:00Z',
}

describe('resourceSettings', () => {
  let resources: Ref<ResourceView[]>
  let query: Ref<ResourceQuery>
  let loading: Ref<boolean>
  let error: Ref<string | null>
  let fetchResources: ReturnType<typeof vi.fn>

  beforeEach(() => {
    resources = ref([{ ...baseResource }])
    query = ref<ResourceQuery>({ kind: 'application', scopeKind: 'global', scopeRef: '' })
    loading = ref(false)
    error = ref(null)
    fetchResources = vi.fn(async () => {})

    vi.mocked(useResources).mockReturnValue({
      resources,
      query,
      loading,
      error,
      fetchResources,
    } as unknown as ReturnType<typeof useResources>)
  })

  it('renders a registry row with its state, origin and scope', () => {
    const wrapper = mount(ResourceSettings, { attachTo: document.body })

    const row = wrapper.get('[data-testid="resource-row-r1"]')
    expect(row.text()).toContain('obsidian')
    expect(row.text()).toContain('Obsidian')
    expect(row.text()).toContain('enabled')
    expect(row.text()).toContain('builtin')
    expect(row.text()).toContain('global')
  })

  it('reads an empty kind as "none yet", not as a failure', () => {
    resources.value = []
    query.value = { kind: 'routine', scopeKind: 'global', scopeRef: '' }
    const wrapper = mount(ResourceSettings, { attachTo: document.body })

    expect(wrapper.get('[data-testid="resource-empty"]').text()).toContain('No routines registered yet')
    expect(wrapper.find('[data-testid="resource-error"]').exists()).toBe(false)
  })

  it('surfaces a load failure as an error, distinct from an empty registry', () => {
    resources.value = []
    error.value = 'Failed to load application resources (HTTP 500)'
    const wrapper = mount(ResourceSettings, { attachTo: document.body })

    expect(wrapper.get('[data-testid="resource-error"]').text()).toContain('HTTP 500')
    expect(wrapper.find('[data-testid="resource-empty"]').exists()).toBe(false)
  })

  it('refetches with the new kind when the kind is switched', async () => {
    const wrapper = mount(ResourceSettings, { attachTo: document.body })

    await wrapper.get('[data-testid="resource-kind-memory_space"]').trigger('click')
    await flushPromises()

    expect(fetchResources).toHaveBeenCalledWith({ kind: 'memory_space' })
  })

  it('clears the scope ref and refetches when the scope kind goes back to global', async () => {
    query.value = { kind: 'application', scopeKind: 'project', scopeRef: '/tmp/demo' }
    const wrapper = mount(ResourceSettings, { attachTo: document.body })

    await wrapper.get('[data-testid="resource-scope-global"]').trigger('click')
    await flushPromises()

    expect(fetchResources).toHaveBeenCalledWith({ scopeKind: 'global', scopeRef: '' })
  })
})
```

- [ ] **Run it and confirm it fails for the right reason.** `pnpm test src/features/settings/components/ResourceSettings.test.ts` — expect `Failed to resolve import "@/features/settings/components/ResourceSettings.vue"`.

- [ ] **GREEN — write the panel.** Create `src/features/settings/components/ResourceSettings.vue`:

```vue
<script setup lang="ts">
import type { ResourceKind, ResourceScopeKind } from '@/features/settings/composables/useResources'
import {
  RESOURCE_KIND_LABELS,
  RESOURCE_KINDS,
  RESOURCE_SCOPE_KINDS,
  useResources,
} from '@/features/settings/composables/useResources'

const { resources, query, loading, error, fetchResources } = useResources()

function selectKind(kind: ResourceKind) {
  if (kind !== query.value.kind)
    void fetchResources({ kind })
}

// The server refuses a ref on `global` and requires one on every other kind,
// so clearing it here keeps that half of the rule unreachable from the form —
// the same handling GrantSettings applies to a grant's context ref.
function selectScopeKind(scopeKind: ResourceScopeKind) {
  if (scopeKind === query.value.scopeKind)
    return
  if (scopeKind === 'global')
    void fetchResources({ scopeKind, scopeRef: '' })
  else
    void fetchResources({ scopeKind })
}

function onScopeRefInput(event: Event) {
  query.value.scopeRef = (event.target as HTMLInputElement).value
}

const EMPTY_MESSAGES: Record<ResourceKind, string> = {
  application: 'No applications registered yet.',
  routine: 'No routines registered yet. Nothing writes routines to the registry today.',
  skill: 'No skills registered yet. Nothing writes skills to the registry today.',
  memory_space: 'No memory spaces registered yet. Create one from the Memory panel.',
}

function formatScope(scopeKind: string, scopeRef: string): string {
  return scopeRef ? `${scopeKind}: ${scopeRef}` : scopeKind
}

function formatDate(iso: string) {
  return new Date(iso).toLocaleString(undefined, { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <div>
      <h3 class="text-[17px] font-bold text-fg mb-1">
        Registry
      </h3>
      <p class="text-xs text-fg-mute">
        Every managed resource the system knows about — applications, routines, skills and memory spaces — with its scope, lifecycle state and where it came from. Read-only: state changes belong to the subsystem that owns the resource.
      </p>
    </div>

    <div class="flex flex-wrap items-center gap-1">
      <button
        v-for="k in RESOURCE_KINDS"
        :key="k"
        type="button"
        :data-testid="`resource-kind-${k}`"
        class="px-2.5 py-1 rounded text-xs border border-line cursor-pointer"
        :class="k === query.kind ? 'bg-accent-soft text-accent border-transparent font-semibold' : 'bg-transparent text-fg-mute hover:text-fg'"
        @click="selectKind(k)"
      >
        {{ RESOURCE_KIND_LABELS[k] }}
      </button>
    </div>

    <div class="flex flex-wrap items-center gap-2">
      <span class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">Scope</span>
      <button
        v-for="s in RESOURCE_SCOPE_KINDS"
        :key="s"
        type="button"
        :data-testid="`resource-scope-${s}`"
        class="px-2 py-0.5 rounded text-[11px] border border-line cursor-pointer"
        :class="s === query.scopeKind ? 'bg-info-soft text-info-text border-transparent font-semibold' : 'bg-transparent text-fg-mute hover:text-fg'"
        @click="selectScopeKind(s)"
      >
        {{ s }}
      </button>
      <input
        v-if="query.scopeKind !== 'global'"
        :value="query.scopeRef"
        data-testid="resource-scope-ref"
        type="text"
        placeholder="scope ref, e.g. /home/me/project"
        class="w-72 bg-card border border-line rounded px-2.5 py-1 text-xs text-fg font-mono focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
        @input="onScopeRefInput"
        @change="fetchResources()"
      >
    </div>

    <div v-if="loading" class="text-center py-12 text-fg-mute text-sm">
      Loading registry...
    </div>
    <div v-else-if="error" data-testid="resource-error" class="rounded border border-danger-line bg-danger-soft text-danger-text px-3 py-2 text-xs">
      {{ error }}
    </div>
    <div v-else-if="!resources.length" data-testid="resource-empty" class="text-center py-8 text-fg-mute text-sm">
      {{ EMPTY_MESSAGES[query.kind] }}
    </div>

    <table v-else class="w-full border-collapse text-[13px]">
      <thead>
        <tr>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Slug
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Name
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Scope
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            State
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Version
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Origin
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Updated
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="r in resources" :key="r.id" :data-testid="`resource-row-${r.id}`">
          <td class="px-3 py-2.5 border-b border-line text-fg font-mono text-xs">
            {{ r.slug }}
          </td>
          <td class="px-3 py-2.5 border-b border-line text-fg">
            {{ r.name || '—' }}
          </td>
          <td class="px-3 py-2.5 border-b border-line text-fg-mute font-mono text-xs">
            {{ formatScope(r.scopeKind, r.scopeRef) }}
          </td>
          <td class="px-3 py-2.5 border-b border-line text-fg-mute">
            {{ r.state }}
          </td>
          <td class="px-3 py-2.5 border-b border-line text-fg-mute font-mono text-xs">
            {{ r.version || '—' }}
          </td>
          <td class="px-3 py-2.5 border-b border-line text-fg-mute" :title="r.originRef">
            {{ r.origin }}
          </td>
          <td class="px-3 py-2.5 border-b border-line text-fg-mute font-mono text-xs whitespace-nowrap">
            {{ formatDate(r.updatedAt) }}
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
```

- [ ] **Register the panel — three edits in `src/features/settings/components/ApiKeySettings.vue`.** Statically imported, like `GrantSettings`: the panel is a single table with no forms, so an async chunk would buy nothing.

  1. Import, next to the other static settings imports (line ~16):

```ts
import ResourceSettings from '@/features/settings/components/ResourceSettings.vue'
```

  2. Add `'registry'` to the `Section` union (line ~44), after `'grants'`:

```ts
type Section = 'appearance' | 'apiKeys' | 'grants' | 'registry' | 'remotes' | 'permissionPresets' | 'analytics' | 'systemPrompts' | 'plugins' | 'notifications' | 'providers' | 'tracker' | 'projects' | 'spawners' | 'pipelineConfig' | 'server'
```

  3. Add the `SECTIONS` entry after the `grants` entry (line ~50):

```ts
  { id: 'registry', icon: '▤', label: 'Registry' },
```

  4. Add the template branch after the `grants` section (line ~633):

```html
        <!-- Registry -->
        <section v-else-if="activeSection === 'registry'">
          <ResourceSettings />
        </section>
```

- [ ] **Run the test and see it green.** `pnpm test src/features/settings/components/ResourceSettings.test.ts`

- [ ] **Run the full gate and paste the raw output.** `pnpm lint && pnpm typecheck && pnpm test`

- [ ] **Commit.** `feat(ui): add a registry panel listing every managed resource`

---

### Task 4: Memory panel — read side

> **BLOCKED.** Do not start this task until `docs/superpowers/plans/2026-09-01-wire-format-dto-sweep.md` has merged its memory task. Until then `GET /api/memory/spaces` answers snake_case (`scope_kind`, `created_at`, …) and `GET /api/memory/entries` answers **PascalCase** (`ID`, `SpaceID`, `Summary`, `Content`, `Kind`, `Confidence`, `CreatedAt`), because `memory.Entry` (`server/internal/memory/retrieve.go:48-56`) carries no json tags at all. Every field name below is the **post-sweep** camelCase shape. Verify before writing a line: `curl -s 'http://127.0.0.1:13120/api/memory/spaces' | head -c 400` must show camelCase keys.

**Problem.** All seven memory routes exist (`server/internal/api/memory/handler.go:37-45`) and nothing in `src/` references `/api/memory` — verified: `grep -rn "api/memory" src/` returns nothing. There is no composable, no panel and no registration.

**Task 4 covers the read side only** — list spaces, search entries, and the denial state. The four write routes are Task 5. Split because one task covering all seven routes would run well past 300 changed lines across five files, which is past the point where a single review can be trusted.

**403 is a first-class state, not an error.** Every route is capability-gated: `listSpaces` and `searchEntries` call `authorize(ctx, repo.CapabilityMemoryRead, "", scope)` and return `403` with the gate's own message when denied. `memory.read` is seeded with `EnforceableBy: ["server"]` (`server/internal/db/repo/capability_seed.go:45`), so this denial is real and will be the default experience on a fresh install with no grant. The panel must render it as an explanation with the fix, not as a red failure.

### Files

- `src/features/settings/composables/useMemory.ts` (new)
- `src/features/settings/components/MemorySettings.vue` (new)
- `src/features/settings/components/MemorySettings.test.ts` (new)
- `src/features/settings/components/ApiKeySettings.vue` — three edits

### Steps

- [ ] **Write the composable.** Create `src/features/settings/composables/useMemory.ts`. `MemorySpace` is an alias of `ResourceView`: `GET /api/memory/spaces` returns rows from the same `resource` table filtered to `kind = memory_space`, so a second interface would be a copy that can drift.

```ts
import type { ResourceScopeKind, ResourceView } from '@/features/settings/composables/useResources'
import { onMounted, ref } from 'vue'

// A memory space IS a registry row (kind = memory_space) — MemoryRepo.ListSpaces
// queries the same `resource` table ResourceRepo does. Aliased rather than
// redeclared so the two views cannot drift apart.
export type MemorySpace = ResourceView

// Mirrors memory.Entry (server/internal/memory/retrieve.go:48-56) as emitted by
// GET /api/memory/entries after the wire-format DTO sweep. Note this is the
// *search hit* shape, which is narrower than the row POST returns.
export interface MemoryEntryHit {
  id: string
  spaceId: string
  summary: string
  content: string
  kind: string
  confidence: number
  createdAt: string
}

// Mirrors ent.MemoryEntry (server/internal/db/ent/schema/memory_entry.go) as
// emitted by POST and PATCH /api/memory/entries after the sweep.
export interface MemoryEntryRow {
  id: string
  spaceId: string
  summary: string
  content: string
  kind: string
  sourceKind: string
  sourceRef: string | null
  confidence: number
  validFrom: string
  validUntil: string | null
  supersededBy: string | null
  userId: string | null
  createdAt: string
  updatedAt: string
}

// Mirrors the kind column's documented values in memory_entry.go.
export const MEMORY_ENTRY_KINDS = ['fact', 'preference', 'lesson', 'entity', 'pointer'] as const
export type MemoryEntryKind = typeof MEMORY_ENTRY_KINDS[number]

// Mirrors the source_kind column's documented values in memory_entry.go.
export const MEMORY_SOURCE_KINDS = ['agent', 'user', 'application', 'import'] as const
export type MemorySourceKind = typeof MEMORY_SOURCE_KINDS[number]

export interface MemoryScope {
  scopeKind: ResourceScopeKind
  scopeRef: string
}

async function readError(res: Response, fallback: string): Promise<string> {
  const body = await res.json().catch(() => ({ error: fallback })) as { error?: string }
  return body.error || fallback
}

function scopeParams(scope: MemoryScope): URLSearchParams {
  const params = new URLSearchParams({ scope: scope.scopeKind })
  if (scope.scopeKind !== 'global')
    params.set('scopeRef', scope.scopeRef.trim())
  return params
}

export function useMemory() {
  const spaces = ref<MemorySpace[]>([])
  const entries = ref<MemoryEntryHit[]>([])
  const scope = ref<MemoryScope>({ scopeKind: 'global', scopeRef: '' })
  const searchText = ref('')
  const loading = ref(true)
  const error = ref<string | null>(null)
  // Held apart from `error`: a 403 means the capability gate refused, which is
  // a configuration state with a known fix, not a failure of the request.
  const denied = ref<string | null>(null)

  async function fetchSpaces(): Promise<void> {
    loading.value = true
    error.value = null
    denied.value = null
    try {
      const res = await fetch(`/api/memory/spaces?${scopeParams(scope.value).toString()}`)
      if (res.status === 403) {
        denied.value = await readError(res, 'memory.read is not granted in this scope')
        spaces.value = []
        return
      }
      if (!res.ok)
        throw new Error(await readError(res, `HTTP ${res.status}`))
      spaces.value = await res.json()
    }
    catch (e) {
      error.value = e instanceof Error ? e.message : 'Failed to load memory spaces'
      spaces.value = []
    }
    finally {
      loading.value = false
    }
  }

  async function searchEntries(): Promise<void> {
    error.value = null
    denied.value = null
    try {
      const params = scopeParams(scope.value)
      params.set('q', searchText.value)
      const res = await fetch(`/api/memory/entries?${params.toString()}`)
      if (res.status === 403) {
        denied.value = await readError(res, 'memory.read is not granted in this scope')
        entries.value = []
        return
      }
      if (!res.ok)
        throw new Error(await readError(res, `HTTP ${res.status}`))
      entries.value = await res.json()
    }
    catch (e) {
      error.value = e instanceof Error ? e.message : 'Failed to search memory'
      entries.value = []
    }
  }

  async function setScope(next: MemoryScope): Promise<void> {
    scope.value = next
    if (next.scopeKind !== 'global' && next.scopeRef.trim() === '') {
      // The server refuses a non-global scope with no ref (memory.ParseScope) —
      // hold the request instead of firing a known-400.
      spaces.value = []
      entries.value = []
      loading.value = false
      return
    }
    await fetchSpaces()
  }

  onMounted(() => {
    void fetchSpaces()
  })

  return { spaces, entries, scope, searchText, loading, error, denied, fetchSpaces, searchEntries, setScope }
}
```

- [ ] **RED — write the component test.** Create `src/features/settings/components/MemorySettings.test.ts`. No manual `unmount()`; `src/test/setup.ts:20` handles teardown.

```ts
import type { Ref } from 'vue'
import type { MemoryEntryHit, MemoryScope, MemorySpace } from '@/features/settings/composables/useMemory'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import MemorySettings from '@/features/settings/components/MemorySettings.vue'
import { useMemory } from '@/features/settings/composables/useMemory'

vi.mock('@/features/settings/composables/useMemory', async () => {
  const actual = await vi.importActual<typeof import('@/features/settings/composables/useMemory')>('@/features/settings/composables/useMemory')
  return {
    ...actual,
    useMemory: vi.fn(),
  }
})

const baseSpace: MemorySpace = {
  id: 's1',
  kind: 'memory_space',
  slug: 'project-notes',
  name: 'Project notes',
  scopeKind: 'global',
  scopeRef: '',
  nodeId: 'local',
  state: 'enabled',
  version: '',
  origin: 'local',
  originRef: '',
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

const baseHit: MemoryEntryHit = {
  id: 'e1',
  spaceId: 's1',
  summary: 'The dashboard binds to 127.0.0.1',
  content: 'Long form content.',
  kind: 'fact',
  confidence: 0.9,
  createdAt: '2026-01-01T00:00:00Z',
}

describe('memorySettings', () => {
  let spaces: Ref<MemorySpace[]>
  let entries: Ref<MemoryEntryHit[]>
  let scope: Ref<MemoryScope>
  let searchText: Ref<string>
  let loading: Ref<boolean>
  let error: Ref<string | null>
  let denied: Ref<string | null>
  let searchEntries: ReturnType<typeof vi.fn>
  let setScope: ReturnType<typeof vi.fn>

  beforeEach(() => {
    spaces = ref([{ ...baseSpace }])
    entries = ref([])
    scope = ref<MemoryScope>({ scopeKind: 'global', scopeRef: '' })
    searchText = ref('')
    loading = ref(false)
    error = ref(null)
    denied = ref(null)
    searchEntries = vi.fn(async () => { entries.value = [{ ...baseHit }] })
    setScope = vi.fn(async () => {})

    vi.mocked(useMemory).mockReturnValue({
      spaces,
      entries,
      scope,
      searchText,
      loading,
      error,
      denied,
      fetchSpaces: vi.fn(),
      searchEntries,
      setScope,
    } as unknown as ReturnType<typeof useMemory>)
  })

  it('lists the memory spaces in scope', () => {
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    const row = wrapper.get('[data-testid="memory-space-s1"]')
    expect(row.text()).toContain('project-notes')
    expect(row.text()).toContain('Project notes')
  })

  it('renders a capability denial as an explanation, not as an error', () => {
    denied.value = 'capability memory.read denied in scope global'
    spaces.value = []
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    const notice = wrapper.get('[data-testid="memory-denied"]')
    expect(notice.text()).toContain('memory.read')
    expect(notice.text()).toContain('Grants')
    expect(wrapper.find('[data-testid="memory-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="memory-empty"]').exists()).toBe(false)
  })

  it('renders a transport failure as an error, distinct from a denial', () => {
    error.value = 'HTTP 500'
    spaces.value = []
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    expect(wrapper.get('[data-testid="memory-error"]').text()).toContain('HTTP 500')
    expect(wrapper.find('[data-testid="memory-denied"]').exists()).toBe(false)
  })

  it('searches entries and renders the hits with their confidence', async () => {
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-search-input"]').setValue('binds')
    await wrapper.get('[data-testid="memory-search-submit"]').trigger('click')
    await flushPromises()

    expect(searchEntries).toHaveBeenCalled()
    const hit = wrapper.get('[data-testid="memory-entry-e1"]')
    expect(hit.text()).toContain('The dashboard binds to 127.0.0.1')
    expect(hit.text()).toContain('fact')
    expect(hit.text()).toContain('0.90')
  })

  it('switches scope through setScope rather than mutating the ref directly', async () => {
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-scope-project"]').trigger('click')
    await flushPromises()

    expect(setScope).toHaveBeenCalledWith({ scopeKind: 'project', scopeRef: '' })
  })
})
```

- [ ] **Run it and confirm it fails for the right reason.** `pnpm test src/features/settings/components/MemorySettings.test.ts` — expect `Failed to resolve import "@/features/settings/components/MemorySettings.vue"`.

- [ ] **GREEN — write the panel.** Create `src/features/settings/components/MemorySettings.vue`:

```vue
<script setup lang="ts">
import type { ResourceScopeKind } from '@/features/settings/composables/useResources'
import { useMemory } from '@/features/settings/composables/useMemory'
import { RESOURCE_SCOPE_KINDS } from '@/features/settings/composables/useResources'

const { spaces, entries, scope, searchText, loading, error, denied, searchEntries, setScope } = useMemory()

function selectScopeKind(scopeKind: ResourceScopeKind) {
  if (scopeKind !== scope.value.scopeKind)
    void setScope({ scopeKind, scopeRef: scopeKind === 'global' ? '' : scope.value.scopeRef })
}

function onScopeRefChange(event: Event) {
  void setScope({ scopeKind: scope.value.scopeKind, scopeRef: (event.target as HTMLInputElement).value })
}

function formatConfidence(value: number): string {
  return value.toFixed(2)
}

function spaceLabel(spaceId: string): string {
  return spaces.value.find(s => s.id === spaceId)?.slug ?? spaceId
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <div>
      <h3 class="text-[17px] font-bold text-fg mb-1">
        Memory
      </h3>
      <p class="text-xs text-fg-mute">
        What the system has learned and kept: spaces group entries, entries hold one conclusion each with a confidence and a source. Reading requires the <code>memory.read</code> capability in the selected scope.
      </p>
    </div>

    <div class="flex flex-wrap items-center gap-2">
      <span class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">Scope</span>
      <button
        v-for="s in RESOURCE_SCOPE_KINDS"
        :key="s"
        type="button"
        :data-testid="`memory-scope-${s}`"
        class="px-2 py-0.5 rounded text-[11px] border border-line cursor-pointer"
        :class="s === scope.scopeKind ? 'bg-info-soft text-info-text border-transparent font-semibold' : 'bg-transparent text-fg-mute hover:text-fg'"
        @click="selectScopeKind(s)"
      >
        {{ s }}
      </button>
      <input
        v-if="scope.scopeKind !== 'global'"
        :value="scope.scopeRef"
        data-testid="memory-scope-ref"
        type="text"
        placeholder="scope ref, e.g. /home/me/project"
        class="w-72 bg-card border border-line rounded px-2.5 py-1 text-xs text-fg font-mono focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
        @change="onScopeRefChange"
      >
    </div>

    <div v-if="denied" data-testid="memory-denied" class="rounded border border-warning-line bg-warning-soft text-warning-text px-3 py-2 text-xs">
      <strong>memory.read is not granted here.</strong>
      {{ denied }}
      Open the Grants panel and add an <code>allow</code> grant for <code>memory.read</code> in this context to read memory from the dashboard.
    </div>
    <div v-else-if="error" data-testid="memory-error" class="rounded border border-danger-line bg-danger-soft text-danger-text px-3 py-2 text-xs">
      {{ error }}
    </div>

    <template v-else>
      <div v-if="loading" class="text-center py-12 text-fg-mute text-sm">
        Loading memory spaces...
      </div>
      <div v-else-if="!spaces.length" data-testid="memory-empty" class="text-center py-8 text-fg-mute text-sm">
        No memory spaces in this scope yet.
      </div>
      <table v-else class="w-full border-collapse text-[13px]">
        <thead>
          <tr>
            <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
              Space
            </th>
            <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
              Name
            </th>
            <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
              Scope
            </th>
            <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
              State
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="s in spaces" :key="s.id" :data-testid="`memory-space-${s.id}`">
            <td class="px-3 py-2.5 border-b border-line text-fg font-mono text-xs">
              {{ s.slug }}
            </td>
            <td class="px-3 py-2.5 border-b border-line text-fg">
              {{ s.name || '—' }}
            </td>
            <td class="px-3 py-2.5 border-b border-line text-fg-mute font-mono text-xs">
              {{ s.scopeRef ? `${s.scopeKind}: ${s.scopeRef}` : s.scopeKind }}
            </td>
            <td class="px-3 py-2.5 border-b border-line text-fg-mute">
              {{ s.state }}
            </td>
          </tr>
        </tbody>
      </table>

      <div class="flex items-center gap-2 pt-2 border-t border-line">
        <input
          v-model="searchText"
          data-testid="memory-search-input"
          type="text"
          placeholder="Search entries in this scope"
          class="flex-1 bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
          @keyup.enter="searchEntries()"
        >
        <button
          type="button"
          data-testid="memory-search-submit"
          class="px-3 py-1.5 rounded border border-line bg-raised text-fg text-sm cursor-pointer hover:bg-card"
          @click="searchEntries()"
        >
          Search
        </button>
      </div>

      <div v-for="e in entries" :key="e.id" :data-testid="`memory-entry-${e.id}`" class="px-3 py-2.5 bg-app rounded-md">
        <div class="flex items-center gap-2.5 mb-1">
          <span class="font-semibold text-xs text-fg">{{ e.summary }}</span>
          <span class="ml-auto font-mono text-[11px] text-fg-mute">{{ e.kind }} · {{ formatConfidence(e.confidence) }}</span>
        </div>
        <div class="text-[11px] text-fg-mute">
          in <code>{{ spaceLabel(e.spaceId) }}</code>
        </div>
        <p class="text-[11px] text-fg-mute mt-1 whitespace-pre-wrap">
          {{ e.content }}
        </p>
      </div>
    </template>
  </div>
</template>
```

- [ ] **Register the panel — four edits in `src/features/settings/components/ApiKeySettings.vue`.** `defineAsyncComponent` here, unlike `ResourceSettings`: Task 5 adds three forms to this panel, and the Settings modal mounts on every dashboard load.

  1. Next to the other async component declarations (line ~28):

```ts
const MemorySettings = defineAsyncComponent(() => import('@/features/settings/components/MemorySettings.vue'))
```

  2. Add `'memory'` to the `Section` union, after `'registry'`.
  3. Add the `SECTIONS` entry after the `registry` entry:

```ts
  { id: 'memory', icon: '🧠', label: 'Memory' },
```

  4. Add the template branch after the `registry` section:

```html
        <!-- Memory -->
        <section v-else-if="activeSection === 'memory'">
          <MemorySettings />
        </section>
```

- [ ] **Run the test and see it green.** `pnpm test src/features/settings/components/MemorySettings.test.ts`

- [ ] **Run the full gate and paste the raw output.** `pnpm lint && pnpm typecheck && pnpm test`

- [ ] **Commit.** `feat(ui): add a memory panel listing spaces and searching entries`

---

### Task 5: Memory panel — write side

> **BLOCKED** on the same wire-format sweep as Task 4, and on Task 4 itself (it extends the composable and the panel Task 4 creates).

**Problem.** Task 4 makes memory readable. Creating a space, adding an entry, superseding an entry and expiring one still require `curl`. The four routes are `POST /api/memory/spaces`, `POST /api/memory/entries`, `PATCH /api/memory/entries/{id}` and `DELETE /api/memory/entries/{id}` (204, no body).

**All four are gated on `memory.write`, not `memory.read`.** `createSpace` authorizes on the new slug; `createEntry` authorizes on `spaceSlug` **before** resolving the space, deliberately, so an ungranted caller cannot use 404-vs-403 as a space-existence oracle; `supersedeEntry` and `expireEntry` must resolve the entry's space first because the path carries only an entry id, and they authorize on that space's slug in that space's own scope. A user with `memory.read` but not `memory.write` therefore sees the panel and gets 403 on every write — render that per-action, do not hide the buttons based on the read grant.

### Files

- `src/features/settings/composables/useMemory.ts` — extend
- `src/features/settings/components/MemorySettings.vue` — extend
- `src/features/settings/components/MemorySettings.test.ts` — extend

### Steps

- [ ] **RED — add the failing tests.** Append to `describe('memorySettings', …)` in `src/features/settings/components/MemorySettings.test.ts`, and add the four new mocked functions to the `beforeEach` return object:

```ts
    createSpace = vi.fn(async () => ({ ...baseSpace, id: 's2', slug: 'new-space' }))
    createEntry = vi.fn(async () => {})
    supersedeEntry = vi.fn(async () => {})
    expireEntry = vi.fn(async () => {})
```

  (declare them alongside the other `let` bindings, and add `createSpace, createEntry, supersedeEntry, expireEntry` to the `vi.mocked(useMemory).mockReturnValue({ … })` object), then:

**Every write carries the panel's scope, and the composable is what puts it there.**
`mem.ParseScope` maps an empty `scope` to **global with no error**, and both write bodies
carry the fields (`createSpaceBody{Slug,Name,Scope,ScopeRef}`,
`createEntryBody{SpaceSlug,Scope,ScopeRef,…}` in `server/internal/api/memory/handler.go`),
so a write that omits them is authorized against, and lands in, the global scope no matter
what the panel is showing — a success message, a scoped list refresh that does not show the
new row, and no error anywhere.

Make that impossible rather than tested: the composable assembles `scope`/`scopeRef` from
its own `scope` ref for all four writes, so no call site can omit or contradict it. The
panel therefore passes only its own fields, which is what the assertions below assert. The
scope assertions belong in `src/features/settings/composables/__tests__/useMemory.test.ts`,
where the real request body is observable — including one test that threads a non-global
scope end to end (`setScope({ scopeKind: 'project', scopeRef: '/x' })`, then create, then
assert the body carries `scope: 'project', scopeRef: '/x'`). Without that last test a
hard-coded `scope: 'global'` passes everything else.


```ts
  it('creates a space with the slug and name from the form', async () => {
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-space-new"]').trigger('click')
    await wrapper.get('[data-testid="memory-space-slug"]').setValue('new-space')
    await wrapper.get('[data-testid="memory-space-name"]').setValue('New space')
    await wrapper.get('[data-testid="memory-space-submit"]').trigger('click')
    await flushPromises()

    expect(createSpace).toHaveBeenCalledWith({ slug: 'new-space', name: 'New space' })
  })

  it('creates an entry with its kind, source kind and confidence', async () => {
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-entry-new"]').trigger('click')
    await wrapper.get('[data-testid="memory-entry-space"]').setValue('project-notes')
    await wrapper.get('[data-testid="memory-entry-summary"]').setValue('Binds to loopback')
    await wrapper.get('[data-testid="memory-entry-content"]').setValue('The server binds 127.0.0.1 only.')
    await wrapper.get('[data-testid="memory-entry-submit"]').trigger('click')
    await flushPromises()

    expect(createEntry).toHaveBeenCalledWith({
      spaceSlug: 'project-notes',
      summary: 'Binds to loopback',
      content: 'The server binds 127.0.0.1 only.',
      kind: 'fact',
      sourceKind: 'user',
      sourceRef: '',
      confidence: 1,
    })
  })

  it('supersedes an entry with the replacement id from the inline form', async () => {
    entries.value = [{ ...baseHit }]
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-supersede-e1"]').trigger('click')
    await wrapper.get('[data-testid="memory-supersede-input-e1"]').setValue('e2')
    await wrapper.get('[data-testid="memory-supersede-confirm-e1"]').trigger('click')
    await flushPromises()

    expect(supersedeEntry).toHaveBeenCalledWith('e1', 'e2')
  })

  it('expires an entry only after the confirm step', async () => {
    entries.value = [{ ...baseHit }]
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-expire-e1"]').trigger('click')
    expect(expireEntry).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="memory-expire-confirm-e1"]').trigger('click')
    await flushPromises()

    expect(expireEntry).toHaveBeenCalledWith('e1')
  })
```

- [ ] **Run them and confirm they fail for the right reason.** `pnpm test src/features/settings/components/MemorySettings.test.ts` — expect four `Unable to get [data-testid="memory-space-new"] …` style failures. The Task 4 tests must still pass.

- [ ] **GREEN — extend the composable.** Add to `src/features/settings/composables/useMemory.ts`, above `export function useMemory()`:

```ts
// Body of POST /api/memory/spaces (createSpaceBody in
// server/internal/api/memory/handler.go) minus scope/scopeRef, which the
// composable fills from the panel's current scope.
export interface CreateSpaceInput {
  slug: string
  name: string
}

// Body of POST /api/memory/entries (createEntryBody, same file), minus
// scope/scopeRef for the same reason.
export interface CreateEntryInput {
  spaceSlug: string
  summary: string
  content: string
  kind: MemoryEntryKind
  sourceKind: MemorySourceKind
  sourceRef: string
  confidence: number
}
```

  and inside `useMemory()`, before the `onMounted` call:

```ts
  function scopeBody(): { scope: string, scopeRef: string } {
    return { scope: scope.value.scopeKind, scopeRef: scope.value.scopeKind === 'global' ? '' : scope.value.scopeRef.trim() }
  }

  async function createSpace(input: CreateSpaceInput): Promise<MemorySpace> {
    const res = await fetch('/api/memory/spaces', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...input, ...scopeBody() }),
    })
    if (!res.ok)
      throw new Error(await readError(res, 'Failed to create space'))
    const created = await res.json() as MemorySpace
    spaces.value.unshift(created)
    return created
  }

  // No optimistic insert, unlike createSpace: the entries list is a ranked
  // search result, not a table. Where a new entry lands — or whether it matches
  // the current query at all — is decided by the server's bm25 ranking, so the
  // client cannot place it without inventing a position the server never gave.
  async function createEntry(input: CreateEntryInput): Promise<void> {
    const res = await fetch('/api/memory/entries', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...input, ...scopeBody() }),
    })
    if (!res.ok)
      throw new Error(await readError(res, 'Failed to create entry'))
    await searchEntries()
  }

  async function supersedeEntry(id: string, supersededBy: string): Promise<void> {
    const res = await fetch(`/api/memory/entries/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ supersededBy }),
    })
    if (!res.ok)
      throw new Error(await readError(res, 'Failed to supersede entry'))
    await searchEntries()
  }

  // 204, no response body — refetch so the entry's disappearance comes from the
  // server's own visibility rules (Retrieve drops expired hits) rather than
  // being guessed on the client.
  async function expireEntry(id: string): Promise<void> {
    const res = await fetch(`/api/memory/entries/${encodeURIComponent(id)}`, { method: 'DELETE' })
    if (!res.ok)
      throw new Error(await readError(res, 'Failed to expire entry'))
    await searchEntries()
  }
```

  and add them to the returned object: `createSpace, createEntry, supersedeEntry, expireEntry`.

- [ ] **GREEN — extend the panel.** In `src/features/settings/components/MemorySettings.vue`, add to the `<script setup>` block:

```ts
import type { CreateEntryInput, MemoryEntryKind, MemorySourceKind } from '@/features/settings/composables/useMemory'
import { ref } from 'vue'
import { toast } from '@/composables/useToast'
import { MEMORY_ENTRY_KINDS, MEMORY_SOURCE_KINDS } from '@/features/settings/composables/useMemory'
import { errorMessage } from '@/utils/errorMessage'
```

  (extend the existing `useMemory` destructure with `createSpace, createEntry, supersedeEntry, expireEntry`), then:

```ts
// ── Create space ─────────────────────────────────────────────────────────────
const spaceFormVisible = ref(false)
const spaceForm = ref({ slug: '', name: '' })
const spaceSaving = ref(false)

async function handleCreateSpace() {
  if (!spaceForm.value.slug.trim()) {
    toast.error('Slug is required')
    return
  }
  spaceSaving.value = true
  try {
    await createSpace({ slug: spaceForm.value.slug.trim(), name: spaceForm.value.name.trim() })
    spaceFormVisible.value = false
    spaceForm.value = { slug: '', name: '' }
  }
  catch (e) {
    toast.error(errorMessage(e))
  }
  finally {
    spaceSaving.value = false
  }
}

// ── Create entry ─────────────────────────────────────────────────────────────
function emptyEntryForm(): CreateEntryInput {
  return { spaceSlug: '', summary: '', content: '', kind: 'fact', sourceKind: 'user', sourceRef: '', confidence: 1 }
}

const entryFormVisible = ref(false)
const entryForm = ref<CreateEntryInput>(emptyEntryForm())
const entrySaving = ref(false)

async function handleCreateEntry() {
  if (!entryForm.value.spaceSlug || !entryForm.value.summary || !entryForm.value.content) {
    toast.error('Space, summary and content are required')
    return
  }
  entrySaving.value = true
  try {
    await createEntry({ ...entryForm.value })
    entryFormVisible.value = false
    entryForm.value = emptyEntryForm()
  }
  catch (e) {
    toast.error(errorMessage(e))
  }
  finally {
    entrySaving.value = false
  }
}

// ── Supersede / expire ───────────────────────────────────────────────────────
const supersedingId = ref<string | null>(null)
const supersededBy = ref('')
const confirmExpireId = ref<string | null>(null)

async function handleSupersede(id: string) {
  if (!supersededBy.value.trim()) {
    toast.error('The replacing entry id is required')
    return
  }
  try {
    await supersedeEntry(id, supersededBy.value.trim())
    supersedingId.value = null
    supersededBy.value = ''
  }
  catch (e) {
    toast.error(errorMessage(e))
  }
}

async function handleExpire(id: string) {
  try {
    await expireEntry(id)
    confirmExpireId.value = null
  }
  catch (e) {
    toast.error(errorMessage(e))
  }
}
```

  and to the template, a create-space control above the spaces table:

```html
      <div class="flex items-center justify-between">
        <h4 class="text-sm font-semibold text-fg">
          Spaces
        </h4>
        <button type="button" data-testid="memory-space-new" class="px-3 py-1 rounded border border-line bg-raised text-fg text-xs cursor-pointer hover:bg-card" @click="spaceFormVisible = !spaceFormVisible">
          + New space
        </button>
      </div>
      <div v-if="spaceFormVisible" class="grid grid-cols-2 gap-3">
        <input
          v-model="spaceForm.slug"
          data-testid="memory-space-slug"
          type="text"
          placeholder="slug, e.g. project-notes"
          class="bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg font-mono focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
        >
        <input
          v-model="spaceForm.name"
          data-testid="memory-space-name"
          type="text"
          placeholder="display name"
          class="bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
        >
        <button type="button" data-testid="memory-space-submit" :disabled="spaceSaving" class="col-span-2 justify-self-start px-3 py-1.5 rounded border border-line bg-info-soft text-info-text text-sm cursor-pointer disabled:opacity-50" @click="handleCreateSpace">
          {{ spaceSaving ? 'Creating…' : 'Create space' }}
        </button>
      </div>
```

  a create-entry control above the entries list:

```html
      <div class="flex items-center justify-between pt-2">
        <h4 class="text-sm font-semibold text-fg">
          Entries
        </h4>
        <button type="button" data-testid="memory-entry-new" class="px-3 py-1 rounded border border-line bg-raised text-fg text-xs cursor-pointer hover:bg-card" @click="entryFormVisible = !entryFormVisible">
          + New entry
        </button>
      </div>
      <div v-if="entryFormVisible" class="grid grid-cols-2 gap-3">
        <input
          v-model="entryForm.spaceSlug"
          data-testid="memory-entry-space"
          type="text"
          placeholder="space slug"
          class="bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg font-mono focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
        >
        <input
          v-model="entryForm.summary"
          data-testid="memory-entry-summary"
          type="text"
          placeholder="summary — this is what gets pushed into a spawn"
          class="bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
        >
        <textarea
          v-model="entryForm.content"
          data-testid="memory-entry-content"
          rows="4"
          placeholder="content — pulled on demand"
          class="col-span-2 bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
        />
        <select v-model="entryForm.kind" data-testid="memory-entry-kind" class="bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg">
          <option v-for="k in MEMORY_ENTRY_KINDS" :key="k" :value="k">{{ k }}</option>
        </select>
        <select v-model="entryForm.sourceKind" data-testid="memory-entry-source-kind" class="bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg">
          <option v-for="s in MEMORY_SOURCE_KINDS" :key="s" :value="s">{{ s }}</option>
        </select>
        <button type="button" data-testid="memory-entry-submit" :disabled="entrySaving" class="col-span-2 justify-self-start px-3 py-1.5 rounded border border-line bg-info-soft text-info-text text-sm cursor-pointer disabled:opacity-50" @click="handleCreateEntry">
          {{ entrySaving ? 'Creating…' : 'Create entry' }}
        </button>
      </div>
```

  and, inside the `v-for="e in entries"` block, an action row:

```html
        <div class="flex items-center gap-2 mt-2">
          <template v-if="supersedingId === e.id">
            <input
              v-model="supersededBy"
              :data-testid="`memory-supersede-input-${e.id}`"
              type="text"
              placeholder="id of the replacing entry"
              class="w-64 bg-card border border-line rounded px-2 py-1 text-xs text-fg font-mono"
            >
            <button type="button" :data-testid="`memory-supersede-confirm-${e.id}`" class="px-2 py-1 rounded border border-line bg-info-soft text-info-text text-xs cursor-pointer" @click="handleSupersede(e.id)">
              Confirm
            </button>
            <button type="button" class="px-2 py-1 rounded border border-line bg-transparent text-fg-mute text-xs cursor-pointer" @click="supersedingId = null">
              Cancel
            </button>
          </template>
          <button v-else type="button" :data-testid="`memory-supersede-${e.id}`" class="px-2 py-1 rounded border border-line bg-transparent text-fg-mute text-xs cursor-pointer hover:text-fg" @click="supersedingId = e.id; supersededBy = ''">
            Supersede
          </button>

          <template v-if="confirmExpireId === e.id">
            <button type="button" :data-testid="`memory-expire-confirm-${e.id}`" class="px-2 py-1 rounded border border-danger-line bg-danger-soft text-danger-text text-xs cursor-pointer" @click="handleExpire(e.id)">
              Confirm expire
            </button>
            <button type="button" class="px-2 py-1 rounded border border-line bg-transparent text-fg-mute text-xs cursor-pointer" @click="confirmExpireId = null">
              Cancel
            </button>
          </template>
          <button v-else type="button" :data-testid="`memory-expire-${e.id}`" class="px-2 py-1 rounded border border-line bg-transparent text-fg-mute text-xs cursor-pointer hover:text-red-600 dark:hover:text-red-400" @click="confirmExpireId = e.id">
            Expire
          </button>
        </div>
```

- [ ] **Run the test and see it green.** `pnpm test src/features/settings/components/MemorySettings.test.ts` — all nine tests pass.

- [ ] **Run the full gate and paste the raw output.** `pnpm lint && pnpm typecheck && pnpm test`

- [ ] **Commit.** `feat(ui): create, supersede and expire memory entries from the panel`

---

### Task 6: Show the memory injection on each stage run

> **BLOCKED** on the wire-format sweep, same as Tasks 4 and 5. `GET /api/memory/injections?stageRun=<id>` today encodes `*ent.MemoryInjection` directly and therefore emits `stage_run_id`, `entry_ids`, `char_budget`, `chars_used`, `candidate_count`, `created_at`. The field names below are the **post-sweep** camelCase shape. Verify first: `curl -s 'http://127.0.0.1:13120/api/memory/injections?stageRun=<a real id>'`.

**Problem.** `ent.MemoryInjection` (`server/internal/db/ent/schema/memory_injection.go`) records one row per memory push into a stage spawn: which entries went in, the character budget, how many characters were spent, and how many candidates the ranker had to choose from. The schema comment states the reason plainly — "Without it the retrieval heuristic can never be improved, only argued about" — and the design doc makes the ranking measurable from day one. Today that record is reachable only by `curl`. A stage run is already rendered in the task modal's Stages tab (`src/features/pipeline/components/task/TaskStagesTab.vue`), which is where the injection belongs.

**One request per stage run, batched in the composable, not per component.** `GET /api/memory/injections` takes exactly one `stageRun` id — there is no bulk form — so N runs mean N requests. The composable issues them together and keeps one shared `denied` flag, so a denial renders as a single notice for the tab rather than the same warning repeated on every row.

**The gate is `memory.read` at *global* scope, unconditionally.** `listInjections` (`server/internal/api/memory/handler.go`) calls `h.authorize(ctx, repo.CapabilityMemoryRead, "", repo.GlobalScope())` — an injection can span several spaces and so has no single owning space to match a pattern against. A project-scoped `memory.read` grant does **not** back this call. Say so in the denial notice; a user holding only a project grant will otherwise chase the wrong fix.

### Files

- `src/features/pipeline/composables/useStageInjections.ts` (new)
- `src/features/pipeline/components/task/TaskStagesTab.vue` — extend
- `src/features/pipeline/components/task/TaskStagesTab.test.ts` (new)

The composable lives inside `src/features/pipeline/`, not in `src/composables/` and not in a barrel: the Stages tab is its only consumer, and a cross-feature import would need the `@/features/pipeline` barrel that `eslint.config.ts`'s `boundary/feature-internals` rule enforces. No consumer outside this feature exists, so no barrel entry is added.

### Steps

- [ ] **RED — write the test.** Create `src/features/pipeline/components/task/TaskStagesTab.test.ts`:

```ts
import type { StageRun } from '@/types'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import TaskStagesTab from '@/features/pipeline/components/task/TaskStagesTab.vue'
import { TaskDetailsKey } from '@/features/pipeline/composables/taskModalContext'

vi.mock('@/features/pipeline/components/StageOutputView.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/ui/AppChip.vue', () => ({ default: { template: '<span><slot /></span>' } }))

function makeRun(overrides: Partial<StageRun> = {}): StageRun {
  return {
    id: 'run-1',
    taskId: 'task-1',
    stage: 'concept',
    sessionId: null,
    sessionName: null,
    pid: null,
    status: 'completed',
    startedAt: '2026-01-01T00:00:00Z',
    endedAt: '2026-01-01T00:05:00Z',
    iteration: 1,
    output: null,
    tokensUsed: 0,
    costCents: 0,
    lastGrantAt: null,
    ...overrides,
  } as StageRun
}

function mountTab(stageRuns: StageRun[]) {
  return mount(TaskStagesTab, {
    global: {
      provide: {
        [TaskDetailsKey as symbol]: { stageRuns: ref(stageRuns) },
      },
    },
  })
}

describe('taskStagesTab — memory injections', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('shows the injection budget, spend and candidate count for a stage run', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify([{
      id: 'inj-1',
      stageRunId: 'run-1',
      entryIds: ['e1', 'e2'],
      charBudget: 2000,
      charsUsed: 1450,
      candidateCount: 9,
      createdAt: '2026-01-01T00:00:00Z',
      updatedAt: '2026-01-01T00:00:00Z',
    }]), { status: 200 })))

    const wrapper = mountTab([makeRun()])
    await flushPromises()

    const block = wrapper.get('[data-testid="stage-injection-run-1"]')
    expect(block.text()).toContain('2 entries')
    expect(block.text()).toContain('1450 / 2000')
    expect(block.text()).toContain('9 candidates')
  })

  it('renders nothing for a stage run that received no memory push', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('[]', { status: 200 })))

    const wrapper = mountTab([makeRun()])
    await flushPromises()

    expect(wrapper.find('[data-testid="stage-injection-run-1"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="stage-injection-denied"]').exists()).toBe(false)
  })

  it('shows one denial notice for the whole tab, naming the global scope', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: 'capability memory.read denied' }), { status: 403 })))

    const wrapper = mountTab([makeRun(), makeRun({ id: 'run-2', iteration: 2 })])
    await flushPromises()

    const notices = wrapper.findAll('[data-testid="stage-injection-denied"]')
    expect(notices).toHaveLength(1)
    expect(notices[0].text()).toContain('global')
  })
})
```

- [ ] **Run it and confirm it fails for the right reason.** `pnpm test src/features/pipeline/components/task/TaskStagesTab.test.ts` — expect `Unable to get [data-testid="stage-injection-run-1"] …` on the first case. If instead the mount throws `useInjectedTaskDetails must be used within a TaskModal`, the `provide` key is wrong, not the feature.

- [ ] **GREEN — write the composable.** Create `src/features/pipeline/composables/useStageInjections.ts`:

```ts
import type { Ref } from 'vue'
import type { StageRun } from '@/types'
import { ref, watch } from 'vue'

// Mirrors ent.MemoryInjection (server/internal/db/ent/schema/memory_injection.go)
// as emitted by GET /api/memory/injections after the wire-format DTO sweep.
export interface MemoryInjection {
  id: string
  stageRunId: string
  entryIds: string[]
  charBudget: number
  charsUsed: number
  candidateCount: number
  createdAt: string
  updatedAt: string
}

export function useStageInjections(stageRuns: Ref<StageRun[]>) {
  const byStageRun = ref<Record<string, MemoryInjection[]>>({})
  // One flag for the whole tab. The route gates on memory.read at global scope
  // regardless of which run is asked for, so a denial is a property of the
  // session, not of an individual row — repeating it per run would read as N
  // separate problems.
  const denied = ref(false)

  async function fetchOne(stageRunId: string): Promise<MemoryInjection[]> {
    const res = await fetch(`/api/memory/injections?stageRun=${encodeURIComponent(stageRunId)}`)
    if (res.status === 403) {
      denied.value = true
      return []
    }
    if (!res.ok)
      return []
    return await res.json() as MemoryInjection[]
  }

  async function load(runs: StageRun[]): Promise<void> {
    denied.value = false
    const results = await Promise.all(runs.map(async run => [run.id, await fetchOne(run.id)] as const))
    const next: Record<string, MemoryInjection[]> = {}
    for (const [id, rows] of results) {
      if (rows.length)
        next[id] = rows
    }
    byStageRun.value = next
  }

  // immediate so the first render after the modal's stage-run fetch resolves is
  // already covered; the tab is mounted before stageRuns has any rows.
  watch(stageRuns, runs => void load(runs), { immediate: true, deep: false })

  return { byStageRun, denied }
}
```

- [ ] **GREEN — extend the Stages tab.** In `src/features/pipeline/components/task/TaskStagesTab.vue`, change the `<script setup>` block to:

```ts
import AppChip from '@/components/ui/AppChip.vue'
import StageOutputView from '@/features/pipeline/components/StageOutputView.vue'
import { useInjectedTaskDetails } from '@/features/pipeline/composables/taskModalContext'
import { useStageInjections } from '@/features/pipeline/composables/useStageInjections'
import { runStatusTone } from '@/utils/statusColors'
import { formatTaskDate } from '@/utils/taskFormat'

const { stageRuns } = useInjectedTaskDetails()
const { byStageRun, denied } = useStageInjections(stageRuns)
```

  add the tab-level denial notice directly under the opening `<section class="p-5">`:

```html
    <div v-if="denied" data-testid="stage-injection-denied" class="rounded border border-warning-line bg-warning-soft text-warning-text px-3 py-2 text-[11px] mb-2">
      Memory injections are hidden: <code>memory.read</code> is not granted at <strong>global</strong> scope. This route always checks the global context — a project-scoped grant does not cover it.
    </div>
```

  and, inside the `v-for="run in stageRuns"` block, after the `run.output` block:

```html
      <div
        v-for="inj in (byStageRun[run.id] ?? [])"
        :key="inj.id"
        :data-testid="`stage-injection-${run.id}`"
        class="mt-1.5 text-[11px] text-fg-mute flex flex-wrap items-center gap-x-3 gap-y-1"
      >
        <span>memory push: <strong class="text-fg">{{ inj.entryIds.length }} entries</strong></span>
        <span>{{ inj.charsUsed }} / {{ inj.charBudget }} chars</span>
        <span>from {{ inj.candidateCount }} candidates</span>
      </div>
```

- [ ] **Run the test and see it green.** `pnpm test src/features/pipeline/components/task/TaskStagesTab.test.ts`

- [ ] **Run the full gate and paste the raw output.** `pnpm lint && pnpm typecheck && pnpm test`

- [ ] **Commit.** `feat(ui): show each stage run's memory injection in the Stages tab`

---

## Definition of Done

- [ ] All six tasks committed, each with its own gate output pasted.
- [ ] `git status --short server/internal/db/ent/` is empty.
- [ ] `server/internal/api/testdata/routes.golden` differs from `main` by exactly one line, `GET /api/resources`.
- [ ] Docs updated in the same change per `.agent-context/layer2-project-core.md`: `README.md` (the Settings modal gains a Registry and a Memory panel; a new `GET /api/resources` route) and `CHANGELOG.md` under `### Added`, Keep a Changelog headings.
- [ ] Branch pushed; `git log origin/<branch>..HEAD` comes back empty.
