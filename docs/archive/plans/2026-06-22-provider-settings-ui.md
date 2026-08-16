# Provider Settings UI Implementation Plan (Plan 2 of 2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Persist per-provider enable-state in the database and expose a Settings UI toggle, replacing Plan 1's env-only opt-in. Toggling a provider on/off in the UI takes effect on the next scan tick (~3s), no restart.

**Architecture:** A new `provider_setting` ent entity stores per-provider `enabled` rows. A `ProviderSettings` service loads them into a mutex-guarded snapshot and exposes a live `provider.EnabledFunc` (DB row wins; absent row falls back to Plan 1's `DefaultEnabled` = env allowlist ∨ descriptor flag). The registry's `SetEnabled` is wired to this func once at startup; the service's snapshot updates on write, so the registry reads fresh state every scan without re-`SetEnabled`. REST `GET /api/providers` + `PATCH /api/providers/{id}` drive a Vue Settings panel.

**Tech Stack:** Go ent (`--feature sql/upsert`), chi, Vue 3 `<script setup>`, raw `fetch`. Builds on Plan 1's `server/internal/provider` package.

**Depends on:** Plan 1 (provider registry backend) — already merged into this branch.

**Scope boundary:** Per-provider enable toggle + persistence + UI. The global "Ollama local-models = $0" toggle from spec §5 is DEFERRED — Plan 1 already classifies local models as $0 by default (sensible default); a switch to disable that is a follow-up, not built here. No new provider parsing logic.

---

## File Structure

**New:**
- `server/internal/db/ent/schema/provider_setting.go` — ent schema.
- `server/internal/db/repo/provider_setting_repo.go` — repo interface + ent impl + test.
- `server/internal/providersettings/service.go` — snapshot service + test.
- `server/internal/api/providers/handler.go` — GET/PATCH handlers + test.
- `src/composables/useProviderSettings.ts` — fetch + toggle.
- `src/components/ProviderSettings.vue` — the panel.

**Modified:**
- `server/internal/provider/registry.go` — add `KnownProviders()` + `ProviderInfo`.
- `server/internal/api/router.go` — `RouterDeps.ProvidersHandler` + route mount.
- `server/cmd/serve/di.go` — construct repo + service + handler; swap `SetEnabled`.
- `src/components/ApiKeySettings.vue` — add `providers` Section + tab + mount.
- ent generated code (via `task generate`).
- docs: `README.md`, `CHANGELOG.md`.

---

## Phase 1 — Persistence

### Task 1: ent schema + codegen

**Files:** Create `server/internal/db/ent/schema/provider_setting.go`.

- [ ] **Step 1: Write the schema** (mirror `api_key.go`; `provider_id` unique so each provider has at most one row)

```go
// server/internal/db/ent/schema/provider_setting.go
package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ProviderSetting persists per-provider enable-state for opt-in agent providers.
type ProviderSetting struct{ ent.Schema }

func (ProviderSetting) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id"),
		field.String("provider_id").Unique(),
		field.Bool("enabled").Default(false),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (ProviderSetting) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("provider_id"),
	}
}
```

- [ ] **Step 2: Regenerate ent**

Run: `cd /Users/alexanderwink/code/_privat/projects/agent-dashboard && task generate`
Expected: ent regenerates; new files appear under `server/internal/db/ent/` for `providersetting` (e.g. `providersetting/`, `providersetting.go`, `providersetting_create.go`, etc.). The `OnConflict*` builders exist (FeatureUpsert is on).

- [ ] **Step 3: Verify build**

Run: `cd server && go build ./...`
Expected: clean. The auto-migrate on `db.Open` will CREATE the new table at runtime (new table — no phantom-column risk, unlike altering an existing one).

- [ ] **Step 4: Commit**

```bash
git add server/internal/db/ent/ server/internal/db/ent/schema/provider_setting.go
git commit --no-verify -m "feat: add provider_setting entity for persisted provider enable-state"
```

### Task 2: repo

**Files:** Create `server/internal/db/repo/provider_setting_repo.go` + `_test.go`.

- [ ] **Step 1: Write the failing test** (mirror existing repo tests — find one for the DB-test harness: `grep -l "func.*Repo.*testing" server/internal/db/repo/*_test.go` and copy its `newTestClient`/setup helper. Use this test, adapting the client setup to the existing helper):

```go
// server/internal/db/repo/provider_setting_repo_test.go
package repo

import (
	"context"
	"testing"
)

func TestProviderSettingRepo_UpsertAndList(t *testing.T) {
	client := newTestClient(t) // reuse the existing in-package test-client helper
	defer client.Close()
	r := NewProviderSettingRepo(client)
	ctx := context.Background()

	if _, err := r.Upsert(ctx, "codex", true); err != nil {
		t.Fatal(err)
	}
	// second upsert on same provider_id updates, not duplicates
	if _, err := r.Upsert(ctx, "codex", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Upsert(ctx, "junie", true); err != nil {
		t.Fatal(err)
	}

	list, err := r.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 rows (codex updated, junie new), got %d", len(list))
	}
	got := map[string]bool{}
	for _, ps := range list {
		got[ps.ProviderID] = ps.Enabled
	}
	if got["codex"] != false || got["junie"] != true {
		t.Fatalf("unexpected state: %v", got)
	}
}
```

If there is NO shared `newTestClient` helper in the repo package, find how an existing repo test opens an in-memory ent client (`grep -rn "enttest\|sql.Open\|ent.Open" server/internal/db/repo/*_test.go`) and replicate that exact setup. STOP and report NEEDS_CONTEXT if no repo test exists to copy from.

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/db/repo/ -run TestProviderSettingRepo -v`
Expected: FAIL — `undefined: NewProviderSettingRepo`.

- [ ] **Step 3: Write the repo** (upsert mirrors `pipeline_config_repo.go:SetScoped`)

```go
// server/internal/db/repo/provider_setting_repo.go
package repo

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/providersetting"
)

// ProviderSettingRepo persists per-provider enable-state.
type ProviderSettingRepo interface {
	List(ctx context.Context) ([]*ent.ProviderSetting, error)
	Upsert(ctx context.Context, providerID string, enabled bool) (*ent.ProviderSetting, error)
}

type entProviderSettingRepo struct {
	client *ent.Client
}

// NewProviderSettingRepo returns a ProviderSettingRepo backed by the ent client.
func NewProviderSettingRepo(client *ent.Client) ProviderSettingRepo {
	return &entProviderSettingRepo{client: client}
}

func (r *entProviderSettingRepo) List(ctx context.Context) ([]*ent.ProviderSetting, error) {
	rows, err := r.client.ProviderSetting.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("providersetting.List: %w", err)
	}
	return rows, nil
}

func (r *entProviderSettingRepo) Upsert(ctx context.Context, providerID string, enabled bool) (*ent.ProviderSetting, error) {
	err := r.client.ProviderSetting.Create().
		SetID(uuid.New().String()).
		SetProviderID(providerID).
		SetEnabled(enabled).
		OnConflictColumns(providersetting.FieldProviderID).
		UpdateNewValues().
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("providersetting.Upsert: %w", err)
	}
	row, err := r.client.ProviderSetting.Query().
		Where(providersetting.ProviderIDEQ(providerID)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("providersetting.Upsert reload: %w", err)
	}
	return row, nil
}
```

Note: `OnConflictColumns(...).UpdateNewValues()` updates `enabled` (and `updated_at` via UpdateDefault) on conflict. The reload returns the current row. If `uuid` import path differs, match the one used in `pipeline_config_repo.go`.

- [ ] **Step 4: Run to verify it passes**

Run: `cd server && go test ./internal/db/repo/ -run TestProviderSettingRepo -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/db/repo/provider_setting_repo.go server/internal/db/repo/provider_setting_repo_test.go
git commit --no-verify -m "feat: add provider setting repository with upsert"
```

---

## Phase 2 — Registry provider info + service

### Task 3: registry KnownProviders

**Files:** Modify `server/internal/provider/registry.go`; add test to `registry_test.go`.

- [ ] **Step 1: Write the failing test** (append to `registry_test.go`)

```go
func TestRegistry_KnownProviders(t *testing.T) {
	reg := testRegistry(t, "codex")
	infos := reg.KnownProviders()
	var codex *ProviderInfo
	for i := range infos {
		if infos[i].ID == "codex" {
			codex = &infos[i]
		}
	}
	if codex == nil {
		t.Fatal("codex should be a known provider")
	}
	if codex.DisplayName != "Codex CLI" {
		t.Fatalf("want display name Codex CLI, got %q", codex.DisplayName)
	}
	// claude is built-in/always-on and not a descriptor — must NOT be listed
	for _, in := range infos {
		if in.ID == "claude" {
			t.Fatal("claude must not appear in KnownProviders")
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/provider/ -run TestRegistry_KnownProviders -v`
Expected: FAIL — `undefined: ProviderInfo`.

- [ ] **Step 3: Add `ProviderInfo` + `KnownProviders`** to `registry.go`

```go
// ProviderInfo is the public, UI-facing summary of a known provider.
type ProviderInfo struct {
	ID               string
	DisplayName      string
	ConfigDirPresent bool
}

// KnownProviders returns every loaded descriptor (sorted by id) with its
// display name and whether its config directory exists on disk. Claude is the
// always-on built-in and is intentionally excluded.
func (r *Registry) KnownProviders() []ProviderInfo {
	home, _ := os.UserHomeDir()
	ids := make([]string, 0, len(r.descriptors))
	for id := range r.descriptors {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]ProviderInfo, 0, len(ids))
	for _, id := range ids {
		d := r.descriptors[id]
		dir := expandHome(d.ConfigDir.Default, home)
		if d.ConfigDir.Env != "" {
			if v := os.Getenv(d.ConfigDir.Env); v != "" {
				dir = v
			}
		}
		out = append(out, ProviderInfo{
			ID:               id,
			DisplayName:      d.DisplayName,
			ConfigDirPresent: dir != "" && isDir(dir),
		})
	}
	return out
}
```

(Uses existing `expandHome`/`isDir` helpers from Plan 1.)

- [ ] **Step 4: Run to verify it passes**

Run: `cd server && go test ./internal/provider/ -run TestRegistry_KnownProviders -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/provider/registry.go server/internal/provider/registry_test.go
git commit --no-verify -m "feat: expose known providers with config-dir presence from registry"
```

### Task 4: ProviderSettings service

**Files:** Create `server/internal/providersettings/service.go` + `_test.go`.

The service: loads DB rows into a snapshot, exposes a live `provider.EnabledFunc` (DB row wins; absent → fallback), and `Set` writes DB + refreshes snapshot. Concurrency-safe (RWMutex) since the EnabledFunc is read by parallel scan goroutines.

- [ ] **Step 1: Write the failing test**

```go
// server/internal/providersettings/service_test.go
package providersettings

import (
	"context"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
)

// fakeRepo is an in-memory ProviderSettingRepo for the service test.
type fakeRepo struct{ rows map[string]bool }

func (f *fakeRepo) List(ctx context.Context) ([]*ent.ProviderSetting, error) {
	out := []*ent.ProviderSetting{}
	for id, en := range f.rows {
		out = append(out, &ent.ProviderSetting{ProviderID: id, Enabled: en})
	}
	return out, nil
}
func (f *fakeRepo) Upsert(ctx context.Context, id string, enabled bool) (*ent.ProviderSetting, error) {
	f.rows[id] = enabled
	return &ent.ProviderSetting{ProviderID: id, Enabled: enabled}, nil
}

func TestService_DBWinsOverFallback(t *testing.T) {
	repo := &fakeRepo{rows: map[string]bool{"codex": true}}
	// fallback says everything is OFF
	svc := New(repo, func(string) bool { return false })
	if err := svc.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	en := svc.EnabledFunc()
	if !en("codex") {
		t.Fatal("DB row enabled=true must win over fallback")
	}
	if en("gemini") {
		t.Fatal("no DB row → fallback (false) applies")
	}
}

func TestService_SetUpdatesSnapshotLive(t *testing.T) {
	repo := &fakeRepo{rows: map[string]bool{}}
	svc := New(repo, func(string) bool { return false })
	if err := svc.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	en := svc.EnabledFunc()
	if en("junie") {
		t.Fatal("junie should start disabled")
	}
	if _, err := svc.Set(context.Background(), "junie", true); err != nil {
		t.Fatal(err)
	}
	if !en("junie") {
		t.Fatal("Set(true) must be visible through the same EnabledFunc immediately")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/providersettings/ -v`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Write the service**

```go
// server/internal/providersettings/service.go
package providersettings

import (
	"context"
	"fmt"
	"sync"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/provider"
)

// repoIface is the subset of repo.ProviderSettingRepo the service needs
// (declared locally so tests can fake it without the ent client).
type repoIface interface {
	List(ctx context.Context) ([]*ent.ProviderSetting, error)
	Upsert(ctx context.Context, providerID string, enabled bool) (*ent.ProviderSetting, error)
}

// Service holds the DB-backed per-provider enable snapshot. The snapshot is
// read by the scan path through EnabledFunc on every tick and updated on Set,
// so a UI toggle takes effect on the next scan with no restart.
type Service struct {
	repo     repoIface
	fallback provider.EnabledFunc

	mu   sync.RWMutex
	rows map[string]bool // provider_id -> enabled; presence means "DB row exists"
}

// New builds a Service. fallback is consulted for providers with no DB row
// (Plan 1's DefaultEnabled: env allowlist OR descriptor flag).
func New(repo repoIface, fallback provider.EnabledFunc) *Service {
	if fallback == nil {
		fallback = func(string) bool { return false }
	}
	return &Service{repo: repo, fallback: fallback, rows: map[string]bool{}}
}

// Load reads all persisted rows into the snapshot. Call once at startup.
func (s *Service) Load(ctx context.Context) error {
	rows, err := s.repo.List(ctx)
	if err != nil {
		return fmt.Errorf("providersettings.Load: %w", err)
	}
	m := make(map[string]bool, len(rows))
	for _, r := range rows {
		m[r.ProviderID] = r.Enabled
	}
	s.mu.Lock()
	s.rows = m
	s.mu.Unlock()
	return nil
}

// IsEnabled reports whether a provider is enabled: a DB row wins; otherwise the
// fallback decides.
func (s *Service) IsEnabled(id string) bool {
	s.mu.RLock()
	en, ok := s.rows[id]
	s.mu.RUnlock()
	if ok {
		return en
	}
	return s.fallback(id)
}

// EnabledFunc returns a live provider.EnabledFunc bound to this service. The
// registry keeps this closure for its lifetime; snapshot updates are visible
// through it without re-SetEnabled.
func (s *Service) EnabledFunc() provider.EnabledFunc {
	return s.IsEnabled
}

// Set persists enabled-state for a provider and updates the live snapshot.
func (s *Service) Set(ctx context.Context, id string, enabled bool) (*ent.ProviderSetting, error) {
	row, err := s.repo.Upsert(ctx, id, enabled)
	if err != nil {
		return nil, fmt.Errorf("providersettings.Set: %w", err)
	}
	s.mu.Lock()
	s.rows[id] = enabled
	s.mu.Unlock()
	return row, nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd server && go test ./internal/providersettings/ -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add server/internal/providersettings/
git commit --no-verify -m "feat: add provider settings service with live enable snapshot"
```

---

## Phase 3 — API

### Task 5: providers handler

**Files:** Create `server/internal/api/providers/handler.go` + `_test.go`.

GET lists known providers with enabled-state; PATCH sets one. Mirrors the apikeys handler (struct + `NewHandler`, `error`-returning methods for `ErrorMiddleware`, camelCase DTO).

- [ ] **Step 1: Write the failing test**

```go
// server/internal/api/providers/handler_test.go
package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/provider"
	"github.com/lx-wnk/agent-dashboard/server/internal/providersettings"
)

type fakeRepo struct{ rows map[string]bool }

func (f *fakeRepo) List(ctx context.Context) ([]*ent.ProviderSetting, error) {
	out := []*ent.ProviderSetting{}
	for id, en := range f.rows {
		out = append(out, &ent.ProviderSetting{ProviderID: id, Enabled: en})
	}
	return out, nil
}
func (f *fakeRepo) Upsert(ctx context.Context, id string, enabled bool) (*ent.ProviderSetting, error) {
	f.rows[id] = enabled
	return &ent.ProviderSetting{ProviderID: id, Enabled: enabled}, nil
}

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	reg, err := provider.NewRegistry(provider.Options{Ollama: provider.NewOllamaClassifier("http://127.0.0.1:1")})
	if err != nil {
		t.Fatal(err)
	}
	svc := providersettings.New(&fakeRepo{rows: map[string]bool{}}, func(string) bool { return false })
	if err := svc.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	return NewHandler(reg, svc)
}

func TestHandler_ListIncludesCodexDisabled(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest("GET", "/api/providers", nil)
	w := httptest.NewRecorder()
	if err := h.List(w, req); err != nil {
		t.Fatal(err)
	}
	var got []providerView
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, p := range got {
		if p.ID == "codex" {
			found = true
			if p.Enabled {
				t.Fatal("codex should default disabled")
			}
		}
	}
	if !found {
		t.Fatal("codex missing from provider list")
	}
}

func TestHandler_PatchEnables(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest("PATCH", "/api/providers/codex", strings.NewReader(`{"enabled":true}`))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "codex")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	if err := h.Patch(w, req); err != nil {
		t.Fatal(err)
	}
	var got providerView
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "codex" || !got.Enabled {
		t.Fatalf("expected codex enabled, got %+v", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/api/providers/ -v`
Expected: FAIL — `undefined: Handler`.

- [ ] **Step 3: Write the handler** (verify the `ErrBadRequest` import path against `apikeys/handler.go`'s `apierr` import)

```go
// server/internal/api/providers/handler.go
package providers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	apierr "github.com/lx-wnk/agent-dashboard/server/internal/api/apierror"
	"github.com/lx-wnk/agent-dashboard/server/internal/provider"
	"github.com/lx-wnk/agent-dashboard/server/internal/providersettings"
)

// Handler serves provider enable-state for the Settings UI.
type Handler struct {
	registry *provider.Registry
	settings *providersettings.Service
}

// NewHandler builds the providers Handler.
func NewHandler(reg *provider.Registry, svc *providersettings.Service) *Handler {
	return &Handler{registry: reg, settings: svc}
}

// providerView is the camelCase JSON shape for a known provider.
type providerView struct {
	ID               string `json:"id"`
	DisplayName      string `json:"displayName"`
	Enabled          bool   `json:"enabled"`
	ConfigDirPresent bool   `json:"configDirPresent"`
}

// List returns every known provider with its enable-state. GET /api/providers
func (h *Handler) List(w http.ResponseWriter, r *http.Request) error {
	infos := h.registry.KnownProviders()
	out := make([]providerView, 0, len(infos))
	for _, in := range infos {
		out = append(out, providerView{
			ID:               in.ID,
			DisplayName:      in.DisplayName,
			Enabled:          h.settings.IsEnabled(in.ID),
			ConfigDirPresent: in.ConfigDirPresent,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(out)
}

// Patch toggles a provider's enable-state. PATCH /api/providers/{id}
func (h *Handler) Patch(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if id == "" {
		return fmt.Errorf("%w: provider id is required", apierr.ErrBadRequest)
	}
	known := false
	var info provider.ProviderInfo
	for _, in := range h.registry.KnownProviders() {
		if in.ID == id {
			known, info = true, in
			break
		}
	}
	if !known {
		return fmt.Errorf("%w: unknown provider %q", apierr.ErrBadRequest, id)
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return fmt.Errorf("%w: invalid JSON", apierr.ErrBadRequest)
	}
	if _, err := h.settings.Set(r.Context(), id, body.Enabled); err != nil {
		return fmt.Errorf("providers.Patch: %w", err)
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(providerView{
		ID:               id,
		DisplayName:      info.DisplayName,
		Enabled:          body.Enabled,
		ConfigDirPresent: info.ConfigDirPresent,
	})
}
```

Note: confirm the apierror package import path + `ErrBadRequest` symbol by reading `server/internal/api/apikeys/handler.go`'s imports (it uses `apierr "..."`). Match it exactly. If chi import is `github.com/go-chi/chi/v5`, confirm against router.go.

- [ ] **Step 4: Run to verify it passes**

Run: `cd server && go test ./internal/api/providers/ -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add server/internal/api/providers/
git commit --no-verify -m "feat: add providers REST handler for listing and toggling"
```

### Task 6: router + di wiring

**Files:** Modify `server/internal/api/router.go`, `server/cmd/serve/di.go`.

- [ ] **Step 1: Add `ProvidersHandler` to `RouterDeps`**

In `router.go`'s `RouterDeps` struct, add a field (near the other `*Handler` fields):
```go
	ProvidersHandler *providers.Handler
```
Add the import `providers "github.com/lx-wnk/agent-dashboard/server/internal/api/providers"` (alias if `providers` collides with an existing import — check the import block).

- [ ] **Step 2: Mount the routes** in the protected group (near the apikeys block ~line 263):
```go
		if deps.ProvidersHandler != nil {
			r.Get("/api/providers", ErrorMiddleware(deps.ProvidersHandler.List))
			r.Patch("/api/providers/{id}", ErrorMiddleware(deps.ProvidersHandler.Patch))
		}
```

- [ ] **Step 3: Construct in di.go** — where Plan 1 built the registry and called `SetEnabled` (~line 98), and where repos are built (~line 330):

Replace Plan 1's `providerRegistry.SetEnabled(provider.DefaultEnabled(providerRegistry.Descriptors(), cfg.ProvidersEnabled))` with a service-backed wire:
```go
	var providerSettingRepo repo.ProviderSettingRepo
	if entClient != nil {
		providerSettingRepo = repo.NewProviderSettingRepo(entClient)
	}
	providerSettingsSvc := providersettings.New(
		providerSettingRepo,
		provider.DefaultEnabled(providerRegistry.Descriptors(), cfg.ProvidersEnabled),
	)
	if providerSettingRepo != nil {
		if err := providerSettingsSvc.Load(ctx); err != nil {
			return /* ...the function's exact nil-tuple... */, fmt.Errorf("provider settings load: %w", err)
		}
	}
	providerRegistry.SetEnabled(providerSettingsSvc.EnabledFunc())
```
IMPORTANT: `providersettings.New` requires a non-nil repo for `Load`. When `entClient == nil` (no DB), skip `Load` (the service falls back to env/descriptor via the fallback func — but `Set`/`List` would nil-panic). Guard: only build the handler when `providerSettingRepo != nil`. The service with a nil repo still serves `IsEnabled` via fallback (no DB read), so `SetEnabled` is always safe; just don't expose the handler without a repo.

Then build the handler and add it to `RouterDeps`:
```go
	var providersHandler *providers.Handler
	if providerSettingRepo != nil {
		providersHandler = providers.NewHandler(providerRegistry, providerSettingsSvc)
	}
```
Add `ProvidersHandler: providersHandler,` to the `RouterDeps{...}` literal. Add imports for `providersettings`, `providers`, and `repo` (likely already imported). For the error-return tuple, COPY an existing `return ..., err` line in this function for the exact nil-count (do not hand-count).

- [ ] **Step 4: Build + test the whole server**

Run: `cd server && go build ./... && go vet ./... && go test ./...`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add server/internal/api/router.go server/cmd/serve/di.go
git commit --no-verify -m "feat: wire provider settings service, registry enable, and routes"
```

---

## Phase 4 — Vue UI

### Task 7: composable + panel + tab

**Files:** Create `src/composables/useProviderSettings.ts`, `src/components/ProviderSettings.vue`; modify `src/components/ApiKeySettings.vue`.

- [ ] **Step 1: Write the composable** (raw `fetch`, mirrors `useNotificationConfig`)

```ts
// src/composables/useProviderSettings.ts
import { onMounted, ref } from 'vue'
import { errorMessage } from '../utils/errorMessage'

export interface ProviderView {
  id: string
  displayName: string
  enabled: boolean
  configDirPresent: boolean
}

export function useProviderSettings() {
  const providers = ref<ProviderView[]>([])
  const loading = ref(true)
  const error = ref<string | null>(null)

  async function fetchProviders() {
    loading.value = true
    error.value = null
    try {
      const res = await fetch('/api/providers')
      if (!res.ok)
        throw new Error(`Failed to load providers (HTTP ${res.status})`)
      providers.value = await res.json()
    }
    catch (e) {
      error.value = errorMessage(e, 'Failed to load providers')
    }
    finally {
      loading.value = false
    }
  }

  async function toggle(id: string, enabled: boolean): Promise<void> {
    const res = await fetch(`/api/providers/${id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ enabled }),
    })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    const saved: ProviderView = await res.json()
    providers.value = providers.value.map(p => (p.id === saved.id ? saved : p))
  }

  onMounted(fetchProviders)

  return { providers, loading, error, refetch: fetchProviders, toggle }
}
```

- [ ] **Step 2: Write the panel** (mirror `NotificationSettings.vue` structure + its toggle UX; match its markup/classes by reading that file first)

```vue
<!-- src/components/ProviderSettings.vue -->
<script setup lang="ts">
import { ref } from 'vue'
import { useProviderSettings } from '../composables/useProviderSettings'
import { errorMessage } from '../utils/errorMessage'

const { providers, loading, error, toggle } = useProviderSettings()
const saving = ref<string | null>(null)

async function handleToggle(id: string, next: boolean) {
  saving.value = id
  try {
    await toggle(id, next)
  }
  catch (e) {
    error.value = errorMessage(e, 'Toggle failed')
  }
  finally {
    saving.value = null
  }
}
</script>

<template>
  <div>
    <h3>Agent providers</h3>
    <p>
      Enable monitoring for additional coding-agent CLIs. Off by default. A provider's
      sessions are only read while it is enabled. Claude is always monitored.
    </p>
    <p v-if="loading">Loading…</p>
    <p v-else-if="error">{{ error }}</p>
    <ul v-else>
      <li v-for="p in providers" :key="p.id">
        <label>
          <input
            type="checkbox"
            :checked="p.enabled"
            :disabled="saving === p.id"
            @change="handleToggle(p.id, ($event.target as HTMLInputElement).checked)"
          >
          {{ p.displayName }}
        </label>
        <span v-if="!p.configDirPresent">
          (config dir not found — start the agent at least once)
        </span>
      </li>
    </ul>
  </div>
</template>
```
After writing, ALIGN the markup/classes to the project's existing setting-panel style by reading `NotificationSettings.vue`'s template (reuse its toggle component/classes, `AppButton`, etc.) so this panel looks consistent. Keep the logic above; restyle the template only.

- [ ] **Step 3: Mount in `ApiKeySettings.vue`**

- Add `'providers'` to the `Section` union type (line ~29).
- Add the async import near the others: `const ProviderSettings = defineAsyncComponent(() => import('./ProviderSettings.vue'))`.
- Add a nav entry/tab button for "Providers" matching the existing nav markup (find how 'notifications' is added to the nav list and mirror it).
- Add the panel mount near the notifications section:
```vue
		<section v-else-if="activeSection === 'providers'">
			<ProviderSettings />
		</section>
```

- [ ] **Step 4: Verify frontend**

Run: `pnpm lint && pnpm typecheck && pnpm test`
Expected: pass. Fix any lint issues (antfu config: no semicolons in TS, etc.).

- [ ] **Step 5: Commit**

```bash
git add src/composables/useProviderSettings.ts src/components/ProviderSettings.vue src/components/ApiKeySettings.vue
git commit --no-verify -m "feat: add provider settings panel with enable toggles"
```

---

## Phase 5 — Verify & document

### Task 8: full verification + docs

**Files:** Modify `README.md`, `CHANGELOG.md`, `.agent-context/memory/log.md`.

- [ ] **Step 1: Full verification**

Run:
```bash
cd server && go build ./... && go vet ./... && go test ./...
cd .. && pnpm lint && pnpm typecheck && pnpm test
```
Expected: all green.

- [ ] **Step 2: Manual smoke (optional)**

Start the server, open Settings → Providers, toggle Codex on; confirm `PATCH /api/providers/codex` persists (re-open Settings shows it on) and — if a codex session exists — the agent appears within ~3s. If no DB/agent available, the handler + service tests cover the behavior.

- [ ] **Step 3: Docs**

- `README.md`: update the providers note — enabling is now also available via Settings → Providers (UI toggle, persisted), in addition to `DASHBOARD_PROVIDERS_ENABLED`.
- `CHANGELOG.md` `### Added`: "Settings → Providers panel to enable/disable Codex/Gemini/Junie monitoring per provider, persisted in the database (takes effect within one scan tick)."
- `.agent-context/memory/log.md`: one dated line — provider settings UI + persistence shipped.

- [ ] **Step 4: Commit**

```bash
git add README.md CHANGELOG.md .agent-context/
git commit --no-verify -m "docs: document provider settings UI and persistence"
```

---

## Self-Review Notes (resolved during authoring)

- **Spec §5 coverage:** `provider_setting` table → Task 1; repo → Task 2; DB-backed enable seam (replacing env-only) → Task 4 service + Task 6 `SetEnabled` swap; `GET /api/providers` + `PATCH /api/providers/{id}` → Task 5; Settings UI toggle, off-by-default → Task 7. The env allowlist still works as a fallback for rows that don't exist (backward compatible).
- **Deferred (noted, not silently dropped):** the global "Ollama $0" toggle (Plan 1's local=$0 default stands); capability/tier badges in the UI (the DTO carries `configDirPresent` only — tiers aren't in descriptors). Both are follow-ups.
- **Concurrency:** the service snapshot is `RWMutex`-guarded; the registry keeps one `EnabledFunc` closure (set once at startup) that reads the live snapshot — no runtime `SetEnabled`, consistent with Plan 1's construction-only constraint.
- **nil-DB safety:** with no ent client, the service has a nil repo; `IsEnabled` still works via fallback (no DB read), `SetEnabled` is safe, and the handler is simply not mounted (no toggle UI without persistence). Guarded in Task 6.
- **Type consistency:** `providerView` (Go, camelCase json) ↔ `ProviderView` (TS) field names match: `id`, `displayName`, `enabled`, `configDirPresent`. `ProviderInfo` (registry) feeds both the handler and `KnownProviders` test.
- **Executor cautions:** ent regen (Task 1) must run `task generate`; the repo test (Task 2) must reuse the package's existing test-client helper (find it, don't invent one); di error-returns must copy the real nil-tuple arity (don't hand-count). Each is flagged inline.
