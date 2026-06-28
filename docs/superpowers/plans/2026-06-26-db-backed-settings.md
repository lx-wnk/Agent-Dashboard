# DB-backed Settings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move all non-bootstrap configuration into one DB-backed settings store, editable from the UI and a direct-DB CLI, with plugin enablement toggleable live (default all-off).

**Architecture:** A generic `app_setting` KV table is the storage; a Go *setting registry* declares every key's type/default/apply-semantics/validation (the SSOT replacing koanf defaults); a `settings.Service` reads DB-first with registry-default fallback and notifies live subsystems on `Set`. Env keeps only bootstrap+secrets. The CLI opens the SQLite file directly (never HTTP) as the lockout-safe escape hatch.

**Tech Stack:** Go 1.26, ent ORM, modernc/sqlite, chi router, cobra CLI; Vue 3 + TS frontend.

**Spec:** `docs/superpowers/specs/2026-06-26-db-backed-settings-design.md`

**Branch:** `feat/db-backed-settings`

**Conventions for every task:** run `cd server && go build ./... && go test ./...` before committing backend changes; `pnpm lint && pnpm typecheck && pnpm test` before committing frontend changes. Commit messages: Conventional Commits, English, with the trailers used in this repo. Do NOT reference phase/task numbers in commit messages.

---

## File Structure

**New (backend):**
- `server/internal/db/ent/schema/app_setting.go` — KV ent schema.
- `server/internal/db/repo/app_setting_repo.go` — KV repo.
- `server/internal/settings/registry.go` — setting definitions (SSOT).
- `server/internal/settings/service.go` — typed DB-first accessor + Set + live hooks.
- `server/internal/settings/registry_test.go`, `service_test.go`.
- `server/internal/api/settings/handler.go` — `GET /api/settings`, `PATCH /api/settings/{key}`.
- `server/internal/api/settings/handler_test.go`.
- `server/internal/api/plugins/handler.go` — `GET /api/plugins`, `PATCH /api/plugins/{id}`.
- `server/internal/api/plugins/handler_test.go`.
- `server/cmd/cli/dbstore.go` — direct-DB open helper + settings read/write.
- `server/cmd/cli/cmd_settings.go` — `config`(extended)/`plugins`/`auth` direct-DB subcommands.
- `server/cmd/cli/cmd_settings_test.go`.

**Modified (backend):**
- `server/internal/plugin/registry.go` — enablement filter, `startEntry` refactor, `StartOne`/`StopOne`.
- `server/internal/plugin/registry_test.go` — lifecycle + skip-disabled tests.
- `server/cmd/serve/di.go` — construct settings.Service, wire plugin enablement, auth guard.
- `server/cmd/serve/di_router.go` — read `auth.mode` from settings.
- `server/internal/config/config.go` — trim Config to bootstrap set; env-ignored warnings.
- `server/internal/config/config_test.go` — trim to bootstrap; add ignored-key test.
- consumers of moved keys (Phase 3 checklist).

**New/Modified (frontend):**
- `src/composables/useSettings.ts` — generic settings fetch/patch.
- `src/composables/usePluginSettings.ts` — plugin list + toggle.
- `src/components/PluginSettings.vue` — read-only → toggle list.
- `src/components/AppSettings.vue` — generic settings panel (registry-driven), auth-mode selector.
- `src/components/ApiKeySettings.vue` — mount the new panel.
- matching `*.test.ts`.

**Docs:** `docs/guides/configuration.md`, `CHANGELOG.md`, `.env.dist`.

---

# PHASE 1 — Foundation

## Task 1: `app_setting` ent schema

**Files:**
- Create: `server/internal/db/ent/schema/app_setting.go`

- [ ] **Step 1: Write the schema**

```go
package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// AppSetting is a generic key/value store for DB-backed, non-bootstrap
// configuration. The value is an opaque string interpreted by the Go setting
// registry (internal/settings).
type AppSetting struct{ ent.Schema }

func (AppSetting) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.String("key").Unique(),
		field.String("value"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
```

- [ ] **Step 2: Regenerate ent**

Run the repo's ent generate entrypoint: `cd server && go generate ./internal/db/ent/...`
(equivalent to what `task generate` runs). The generate config already enables the Upsert feature (existing `provider_setting` uses `OnConflict`), so `AppSetting.Create().OnConflictColumns(...).UpdateNewValues()` will be generated.

Expected: new package `server/internal/db/ent/appsetting/` exists; `ent.Client` gains `AppSetting`.

- [ ] **Step 3: Verify build**

Run: `cd server && go build ./...`
Expected: compiles. If `runtime.go`/`go.sum` show unrelated drift, revert those non-schema changes (`git checkout -- server/internal/db/ent/runtime/runtime.go`).

- [ ] **Step 4: Commit**

```bash
git add server/internal/db/ent server/internal/db/ent/schema/app_setting.go
git commit -m "feat: add app_setting ent schema for DB-backed config"
```

## Task 2: `app_setting` repo

**Files:**
- Create: `server/internal/db/repo/app_setting_repo.go`
- Test: `server/internal/db/repo/app_setting_repo_test.go`

- [ ] **Step 1: Write the failing test**

```go
package repo_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/testdb" // existing in-memory ent helper
)

func TestAppSettingRepo_UpsertGetList(t *testing.T) {
	client := testdb.New(t) // returns *ent.Client backed by in-memory sqlite
	r := repo.NewAppSettingRepo(client)
	ctx := context.Background()

	_, err := r.Upsert(ctx, "auth.mode", "plugin")
	require.NoError(t, err)
	// upsert again updates value, not duplicates
	_, err = r.Upsert(ctx, "auth.mode", "none")
	require.NoError(t, err)

	v, ok, err := r.Get(ctx, "auth.mode")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "none", v)

	_, ok, err = r.Get(ctx, "missing.key")
	require.NoError(t, err)
	assert.False(t, ok)

	all, err := r.List(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 1)
}
```

> If `internal/db/testdb` does not exist, look at how `provider_setting_repo` tests open an ent client (grep `OpenInMemory`/`enttest` in `server/internal/db`) and use that exact helper instead.

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/db/repo/ -run TestAppSettingRepo -v`
Expected: FAIL — `repo.NewAppSettingRepo` undefined.

- [ ] **Step 3: Implement the repo**

```go
package repo

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/appsetting"
)

// AppSettingRepo persists generic key/value configuration.
type AppSettingRepo interface {
	Get(ctx context.Context, key string) (string, bool, error)
	List(ctx context.Context) ([]*ent.AppSetting, error)
	Upsert(ctx context.Context, key, value string) (*ent.AppSetting, error)
}

type entAppSettingRepo struct{ client *ent.Client }

// NewAppSettingRepo returns an AppSettingRepo backed by the ent client.
func NewAppSettingRepo(client *ent.Client) AppSettingRepo {
	return &entAppSettingRepo{client: client}
}

func (r *entAppSettingRepo) Get(ctx context.Context, key string) (string, bool, error) {
	row, err := r.client.AppSetting.Query().Where(appsetting.KeyEQ(key)).Only(ctx)
	if ent.IsNotFound(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("appsetting.Get: %w", err)
	}
	return row.Value, true, nil
}

func (r *entAppSettingRepo) List(ctx context.Context) ([]*ent.AppSetting, error) {
	rows, err := r.client.AppSetting.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("appsetting.List: %w", err)
	}
	return rows, nil
}

func (r *entAppSettingRepo) Upsert(ctx context.Context, key, value string) (*ent.AppSetting, error) {
	err := r.client.AppSetting.Create().
		SetID(uuid.New().String()).
		SetKey(key).
		SetValue(value).
		OnConflictColumns(appsetting.FieldKey).
		UpdateNewValues().
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("appsetting.Upsert: %w", err)
	}
	row, err := r.client.AppSetting.Query().Where(appsetting.KeyEQ(key)).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("appsetting.Upsert reload: %w", err)
	}
	return row, nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd server && go test ./internal/db/repo/ -run TestAppSettingRepo -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/db/repo/app_setting_repo.go server/internal/db/repo/app_setting_repo_test.go
git commit -m "feat: add app_setting repo (get/list/upsert)"
```

## Task 3: Setting registry (SSOT)

**Files:**
- Create: `server/internal/settings/registry.go`
- Test: `server/internal/settings/registry_test.go`

- [ ] **Step 1: Write the failing test**

```go
package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_DefaultsAndValidation(t *testing.T) {
	d, ok := Lookup("spawn.rateLimit")
	require.True(t, ok)
	assert.Equal(t, TypeInt, d.Type)
	assert.Equal(t, ApplyRestart, d.Apply)
	assert.Equal(t, "5", d.Default)

	// validator rejects non-int
	require.Error(t, d.Validate("abc"))
	require.NoError(t, d.Validate("10"))

	// enum auth.mode
	a, _ := Lookup("auth.mode")
	require.NoError(t, a.Validate("none"))
	require.NoError(t, a.Validate("plugin"))
	require.Error(t, a.Validate("github"))

	// positive-int constraint
	h, _ := Lookup("hooks.eventsPerSession")
	require.Error(t, h.Validate("0"))
	require.NoError(t, h.Validate("1"))

	_, ok = Lookup("nope")
	assert.False(t, ok)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/settings/ -run TestRegistry -v`
Expected: FAIL — package/symbols undefined.

- [ ] **Step 3: Implement the registry**

```go
// Package settings defines the DB-backed configuration registry (the single
// source of truth for non-bootstrap settings) and a service to read/write them.
package settings

import (
	"fmt"
	"strconv"
)

// Type is the value type of a setting; the stored value is always a string.
type Type string

const (
	TypeBool        Type = "bool"
	TypeInt         Type = "int"
	TypeFloat       Type = "float"
	TypeString      Type = "string"
	TypeStringSlice Type = "stringSlice" // comma-joined in storage
	TypeEnum        Type = "enum"
)

// Apply describes when a change takes effect.
type Apply string

const (
	ApplyLive    Apply = "live"
	ApplyRestart Apply = "restart"
)

// Definition declares one setting. Default is the string form of the value.
type Definition struct {
	Key      string
	Type     Type
	Default  string
	Apply    Apply
	Category string
	Enum     []string                 // for TypeEnum
	validate func(raw string) error   // extra constraint beyond type parsing
}

// Validate checks raw against the type, enum, and any extra constraint.
func (d Definition) Validate(raw string) error {
	switch d.Type {
	case TypeBool:
		if _, err := strconv.ParseBool(raw); err != nil {
			return fmt.Errorf("%s: must be a boolean", d.Key)
		}
	case TypeInt:
		if _, err := strconv.Atoi(raw); err != nil {
			return fmt.Errorf("%s: must be an integer", d.Key)
		}
	case TypeFloat:
		if _, err := strconv.ParseFloat(raw, 64); err != nil {
			return fmt.Errorf("%s: must be a number", d.Key)
		}
	case TypeEnum:
		for _, e := range d.Enum {
			if raw == e {
				if d.validate != nil {
					return d.validate(raw)
				}
				return nil
			}
		}
		return fmt.Errorf("%s: must be one of %v", d.Key, d.Enum)
	case TypeString, TypeStringSlice:
		// any string accepted
	}
	if d.validate != nil {
		return d.validate(raw)
	}
	return nil
}

func positiveInt(key string) func(string) error {
	return func(raw string) error {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return fmt.Errorf("%s: must be a positive integer", key)
		}
		return nil
	}
}

func nonNegativeInt(key string) func(string) error {
	return func(raw string) error {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return fmt.Errorf("%s: must be >= 0", key)
		}
		return nil
	}
}

func nonNegativeFloat(key string) func(string) error {
	return func(raw string) error {
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil || f < 0 {
			return fmt.Errorf("%s: must be >= 0", key)
		}
		return nil
	}
}

// definitions is the SSOT for every DB-backed setting.
var definitions = func() map[string]Definition {
	list := []Definition{
		{Key: "auth.mode", Type: TypeEnum, Enum: []string{"none", "plugin"}, Default: "none", Apply: ApplyRestart, Category: "auth"},
		{Key: "providers.enabled", Type: TypeStringSlice, Default: "", Apply: ApplyLive, Category: "providers"},
		{Key: "plugins.enabled", Type: TypeStringSlice, Default: "", Apply: ApplyLive, Category: "plugins"},
		{Key: "git.allowPush", Type: TypeBool, Default: "false", Apply: ApplyRestart, Category: "git"},
		{Key: "worktree.force", Type: TypeBool, Default: "false", Apply: ApplyRestart, Category: "worktree"},
		{Key: "sse.intervalMs", Type: TypeInt, Default: "3000", Apply: ApplyRestart, Category: "sse"},
		{Key: "shutdown.timeoutSeconds", Type: TypeInt, Default: "10", Apply: ApplyRestart, Category: "server"},
		{Key: "hooks.debounceMs", Type: TypeInt, Default: "100", Apply: ApplyRestart, Category: "hooks"},
		{Key: "hooks.eventsPerSession", Type: TypeInt, Default: "50", Apply: ApplyRestart, Category: "hooks", validate: positiveInt("hooks.eventsPerSession")},
		{Key: "spawn.rateLimit", Type: TypeInt, Default: "5", Apply: ApplyRestart, Category: "spawn"},
		{Key: "spawn.rateWindowMs", Type: TypeInt, Default: "60000", Apply: ApplyRestart, Category: "spawn"},
		{Key: "inject.rateLimit", Type: TypeInt, Default: "30", Apply: ApplyRestart, Category: "inject"},
		{Key: "inject.rateWindowMs", Type: TypeInt, Default: "60000", Apply: ApplyRestart, Category: "inject"},
		{Key: "cost.scanIntervalMs", Type: TypeInt, Default: "300000", Apply: ApplyRestart, Category: "cost"},
		{Key: "eval.scanIntervalMs", Type: TypeInt, Default: "3600000", Apply: ApplyRestart, Category: "eval"},
		{Key: "eval.windowHours", Type: TypeInt, Default: "168", Apply: ApplyRestart, Category: "eval", validate: positiveInt("eval.windowHours")},
		{Key: "eval.minSamples", Type: TypeInt, Default: "20", Apply: ApplyRestart, Category: "eval", validate: nonNegativeInt("eval.minSamples")},
		{Key: "eval.rateDropPP", Type: TypeFloat, Default: "15", Apply: ApplyRestart, Category: "eval", validate: nonNegativeFloat("eval.rateDropPP")},
		{Key: "eval.stddevK", Type: TypeFloat, Default: "3", Apply: ApplyRestart, Category: "eval", validate: nonNegativeFloat("eval.stddevK")},
	}
	m := make(map[string]Definition, len(list))
	for _, d := range list {
		m[d.Key] = d
	}
	return m
}()

// Lookup returns the definition for key.
func Lookup(key string) (Definition, bool) { d, ok := definitions[key]; return d, ok }

// All returns every definition (unordered).
func All() []Definition {
	out := make([]Definition, 0, len(definitions))
	for _, d := range definitions {
		out = append(out, d)
	}
	return out
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd server && go test ./internal/settings/ -run TestRegistry -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/settings/registry.go server/internal/settings/registry_test.go
git commit -m "feat: add settings registry as config SSOT"
```

## Task 4: `settings.Service`

**Files:**
- Create: `server/internal/settings/service.go`
- Test: `server/internal/settings/service_test.go`

- [ ] **Step 1: Write the failing test**

```go
package settings

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRepo struct{ m map[string]string }

func (f *fakeRepo) Get(_ context.Context, k string) (string, bool, error) {
	v, ok := f.m[k]
	return v, ok, nil
}
func (f *fakeRepo) Set(_ context.Context, k, v string) error { f.m[k] = v; return nil }
func (f *fakeRepo) ListAll(_ context.Context) (map[string]string, error) { return f.m, nil }

func TestService_DefaultsAndTypedAccess(t *testing.T) {
	svc := New(&fakeRepo{m: map[string]string{}})
	require.NoError(t, svc.Load(context.Background()))

	// registry default when no DB row
	assert.Equal(t, 5, svc.Int("spawn.rateLimit"))
	assert.False(t, svc.Bool("worktree.force"))
	assert.Equal(t, "none", svc.String("auth.mode"))
	assert.Empty(t, svc.StringSlice("plugins.enabled"))

	// Set persists + updates snapshot, validated
	require.NoError(t, svc.Set(context.Background(), "worktree.force", "true"))
	assert.True(t, svc.Bool("worktree.force"))

	// invalid value rejected
	require.Error(t, svc.Set(context.Background(), "spawn.rateLimit", "abc"))
	// unknown key rejected
	require.Error(t, svc.Set(context.Background(), "nope", "1"))

	// stringSlice round-trips
	require.NoError(t, svc.Set(context.Background(), "plugins.enabled", "voice-whisper,voice-webspeech"))
	assert.Equal(t, []string{"voice-whisper", "voice-webspeech"}, svc.StringSlice("plugins.enabled"))
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/settings/ -run TestService -v`
Expected: FAIL — `New`/accessors undefined.

- [ ] **Step 3: Implement the service**

```go
package settings

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// Repo is the persistence the service needs (subset of repo.AppSettingRepo,
// declared locally so tests can fake it).
type Repo interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, value string) error
	ListAll(ctx context.Context) (map[string]string, error)
}

// LiveHook is invoked after a successful Set of an ApplyLive key, so a
// subsystem (e.g. the plugin registry) can apply the change without a restart.
type LiveHook func(ctx context.Context, key, value string) error

// Service reads settings DB-first with registry-default fallback.
type Service struct {
	repo Repo

	mu        sync.RWMutex
	snapshot  map[string]string // key -> raw DB value (present only if a row exists)
	liveHooks map[string]LiveHook
}

// New builds a Service.
func New(repo Repo) *Service {
	return &Service{repo: repo, snapshot: map[string]string{}, liveHooks: map[string]LiveHook{}}
}

// RegisterLiveHook attaches a hook for an ApplyLive key.
func (s *Service) RegisterLiveHook(key string, fn LiveHook) {
	s.mu.Lock()
	s.liveHooks[key] = fn
	s.mu.Unlock()
}

// Load reads all rows into the snapshot. Call once at startup.
func (s *Service) Load(ctx context.Context) error {
	all, err := s.repo.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("settings.Load: %w", err)
	}
	s.mu.Lock()
	s.snapshot = all
	s.mu.Unlock()
	return nil
}

// raw returns the effective string value: DB row if present, else registry default.
func (s *Service) raw(key string) string {
	s.mu.RLock()
	v, ok := s.snapshot[key]
	s.mu.RUnlock()
	if ok {
		return v
	}
	if d, ok := Lookup(key); ok {
		return d.Default
	}
	return ""
}

// Typed accessors. They assume the key exists in the registry (programmer error otherwise).
func (s *Service) String(key string) string { return s.raw(key) }
func (s *Service) Bool(key string) bool      { b, _ := strconv.ParseBool(s.raw(key)); return b }
func (s *Service) Int(key string) int        { n, _ := strconv.Atoi(s.raw(key)); return n }
func (s *Service) Float(key string) float64  { f, _ := strconv.ParseFloat(s.raw(key), 64); return f }

func (s *Service) StringSlice(key string) []string {
	raw := s.raw(key)
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// Effective returns key -> current effective value for every registry key
// (DB value or default). Used by the API to render the settings UI.
func (s *Service) Effective() map[string]string {
	out := map[string]string{}
	for _, d := range All() {
		out[d.Key] = s.raw(d.Key)
	}
	return out
}

// Set validates against the registry, persists, updates the snapshot, and runs
// the live hook (if any). Returns the definition's Apply semantics.
func (s *Service) Set(ctx context.Context, key, value string) error {
	def, ok := Lookup(key)
	if !ok {
		return fmt.Errorf("settings.Set: unknown key %q", key)
	}
	if err := def.Validate(value); err != nil {
		return fmt.Errorf("settings.Set: %w", err)
	}
	if err := s.repo.Set(ctx, key, value); err != nil {
		return fmt.Errorf("settings.Set: %w", err)
	}
	s.mu.Lock()
	s.snapshot[key] = value
	hook := s.liveHooks[key]
	s.mu.Unlock()
	if def.Apply == ApplyLive && hook != nil {
		if err := hook(ctx, key, value); err != nil {
			return fmt.Errorf("settings.Set live-apply: %w", err)
		}
	}
	return nil
}

// ApplyOf reports the apply-semantics of a key (for API responses).
func (s *Service) ApplyOf(key string) Apply {
	if d, ok := Lookup(key); ok {
		return d.Apply
	}
	return ApplyRestart
}
```

> The `Repo` interface uses `Set`/`ListAll`; the ent `AppSettingRepo` (Task 2) exposes `Upsert`/`List`. Add a thin adapter in di.go (Task 5) that maps `Set`→`Upsert` and `ListAll`→`List`+map-build, OR add `Set`/`ListAll` methods to the ent repo. Choose the adapter to keep the ent repo minimal.

- [ ] **Step 4: Run to verify it passes**

Run: `cd server && go test ./internal/settings/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/settings/service.go server/internal/settings/service_test.go
git commit -m "feat: add settings.Service (DB-first typed config + live hooks)"
```

## Task 5: Wire `settings.Service` into serve

**Files:**
- Modify: `server/cmd/serve/di.go` (near the existing provider-settings wiring, ~lines 100-116)

- [ ] **Step 1: Add a repo adapter + construct the service**

In `di.go`, after the ent client + repos are built, add:

```go
// settingsRepoAdapter maps settings.Repo onto the ent AppSettingRepo.
type settingsRepoAdapter struct{ inner repo.AppSettingRepo }

func (a settingsRepoAdapter) Get(ctx context.Context, k string) (string, bool, error) {
	return a.inner.Get(ctx, k)
}
func (a settingsRepoAdapter) Set(ctx context.Context, k, v string) error {
	_, err := a.inner.Upsert(ctx, k, v)
	return err
}
func (a settingsRepoAdapter) ListAll(ctx context.Context) (map[string]string, error) {
	rows, err := a.inner.List(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[string]string, len(rows))
	for _, r := range rows {
		m[r.Key] = r.Value
	}
	return m, nil
}
```

Then where repos are constructed:

```go
appSettingRepo := repo.NewAppSettingRepo(entClient)
settingsSvc := settings.New(settingsRepoAdapter{inner: appSettingRepo})
if err := settingsSvc.Load(ctx); err != nil {
	return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("settings load: %w", err)
}
```

> Match the exact 11-value error-return arity of `initializeServer` (pad with `nil`s as the surrounding code does). If `entClient` is nil (no DB), guard as the existing code guards other repos.

- [ ] **Step 2: Verify build**

Run: `cd server && go build ./...`
Expected: compiles (service is constructed but not yet consumed — that lands in later tasks).

- [ ] **Step 3: Commit**

```bash
git add server/cmd/serve/di.go
git commit -m "feat: construct settings.Service at startup"
```

## Task 6: Generic settings API

**Files:**
- Create: `server/internal/api/settings/handler.go`
- Test: `server/internal/api/settings/handler_test.go`
- Modify: `server/cmd/serve/di.go` (route registration, mirror the `/api/providers` lines)

- [ ] **Step 1: Write the failing test**

```go
package settings_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	settingssvc "github.com/lx-wnk/agent-dashboard/server/internal/settings"
	settingsapi "github.com/lx-wnk/agent-dashboard/server/internal/api/settings"
)

type memRepo struct{ m map[string]string }

func (r *memRepo) Get(_ context.Context, k string) (string, bool, error) { v, ok := r.m[k]; return v, ok, nil }
func (r *memRepo) Set(_ context.Context, k, v string) error             { r.m[k] = v; return nil }
func (r *memRepo) ListAll(_ context.Context) (map[string]string, error) { return r.m, nil }

func newRouter(t *testing.T) (http.Handler, *settingssvc.Service) {
	svc := settingssvc.New(&memRepo{m: map[string]string{}})
	require.NoError(t, svc.Load(context.Background()))
	h := settingsapi.NewHandler(svc)
	r := chi.NewRouter()
	h.Mount(r)
	return r, svc
}

func TestSettingsAPI_ListAndPatch(t *testing.T) {
	r, _ := newRouter(t)

	// GET returns definitions with current values
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	assert.NotEmpty(t, list)

	// PATCH a restart key -> applied:"restart"
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/settings/spawn.rateLimit", strings.NewReader(`{"value":"9"}`))
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "restart", resp["applied"])
	assert.Equal(t, "9", resp["value"])

	// PATCH invalid value -> 400
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/settings/spawn.rateLimit", strings.NewReader(`{"value":"abc"}`))
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// PATCH unknown key -> 400
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/settings/nope", strings.NewReader(`{"value":"1"}`))
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/api/settings/ -v`
Expected: FAIL — package undefined.

- [ ] **Step 3: Implement the handler**

```go
package settings

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
	settingssvc "github.com/lx-wnk/agent-dashboard/server/internal/settings"
)

// Handler serves the DB-backed settings registry.
type Handler struct{ svc *settingssvc.Service }

// NewHandler builds the settings Handler.
func NewHandler(svc *settingssvc.Service) *Handler { return &Handler{svc: svc} }

// Mount registers routes. Mirror the apierr.ErrorMiddleware wrapper used by
// the providers handler when wiring this in di.go (see Step 4).
func (h *Handler) Mount(r chi.Router) {
	r.Get("/api/settings", apierr.ErrorMiddleware(h.list))
	r.Patch("/api/settings/{key}", apierr.ErrorMiddleware(h.patch))
}

type settingView struct {
	Key      string   `json:"key"`
	Type     string   `json:"type"`
	Value    string   `json:"value"`
	Default  string   `json:"default"`
	Apply    string   `json:"apply"`
	Category string   `json:"category"`
	Enum     []string `json:"enum,omitempty"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	eff := h.svc.Effective()
	defs := settingssvc.All()
	out := make([]settingView, 0, len(defs))
	for _, d := range defs {
		out = append(out, settingView{
			Key: d.Key, Type: string(d.Type), Value: eff[d.Key],
			Default: d.Default, Apply: string(d.Apply), Category: d.Category, Enum: d.Enum,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(out)
}

func (h *Handler) patch(w http.ResponseWriter, r *http.Request) error {
	key := chi.URLParam(r, "key")
	if _, ok := settingssvc.Lookup(key); !ok {
		return fmt.Errorf("%w: unknown setting %q", apierr.ErrBadRequest, key)
	}
	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return fmt.Errorf("%w: invalid JSON", apierr.ErrBadRequest)
	}
	if err := h.svc.Set(r.Context(), key, body.Value); err != nil {
		return fmt.Errorf("%w: %s", apierr.ErrBadRequest, err.Error())
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(map[string]string{
		"key": key, "value": body.Value, "applied": string(h.svc.ApplyOf(key)),
	})
}
```

> Verify `apierr.ErrBadRequest` exists (the providers handler uses it). If the repo error should map to 400 only for validation but 500 for DB failure, split: return `apierr.ErrBadRequest` wrap for validation (check before `repo.Set`) and a plain wrapped error otherwise. Keep simple: validation runs inside `Set` before persistence, so a `Set` error here is overwhelmingly validation → 400 is acceptable.

- [ ] **Step 4: Register the route in di.go**

Mirror the providers registration:

```go
settingsHandler := settingsapihandler.NewHandler(settingsSvc) // import alias as needed
settingsHandler.Mount(r)
```

- [ ] **Step 5: Run to verify it passes**

Run: `cd server && go test ./internal/api/settings/ -v && go build ./...`
Expected: PASS + build.

- [ ] **Step 6: Commit**

```bash
git add server/internal/api/settings server/cmd/serve/di.go
git commit -m "feat: add /api/settings list+patch endpoints"
```

## Task 7: Direct-DB CLI core (`config get/set/list`)

**Files:**
- Create: `server/cmd/cli/dbstore.go`
- Create: `server/cmd/cli/cmd_settings.go`
- Test: `server/cmd/cli/cmd_settings_test.go`
- Modify: `server/cmd/cli/main.go` (register new commands)

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDBStore_SetGetList(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	store, err := openDBStore(dbPath)
	require.NoError(t, err)
	defer store.Close()
	ctx := context.Background()

	require.NoError(t, store.Set(ctx, "auth.mode", "plugin"))
	require.NoError(t, store.Set(ctx, "auth.mode", "none")) // upsert

	v, ok, err := store.Get(ctx, "auth.mode")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "none", v)

	all, err := store.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, "none", all["auth.mode"])
}

func TestDBStore_RejectsUnknownKey(t *testing.T) {
	store, err := openDBStore(t.TempDir() + "/test.db")
	require.NoError(t, err)
	defer store.Close()
	// CLI validates against the registry before writing.
	require.Error(t, store.SetValidated(context.Background(), "nope", "x"))
	require.Error(t, store.SetValidated(context.Background(), "spawn.rateLimit", "abc"))
	require.NoError(t, store.SetValidated(context.Background(), "spawn.rateLimit", "7"))
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./cmd/cli/ -run TestDBStore -v`
Expected: FAIL — `openDBStore` undefined.

- [ ] **Step 3: Implement the direct-DB store**

```go
package main

import (
	"context"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/settings"
)

// dbStore opens the dashboard SQLite file directly (no HTTP), so settings can
// be changed while the server is down — the lockout-safe escape hatch.
type dbStore struct {
	client *ent.Client
	repo   repo.AppSettingRepo
}

// openDBStore opens (and migrates) the dashboard DB at path.
func openDBStore(path string) (*dbStore, error) {
	// Reuse the server's db.Open so schema/migration matches exactly.
	client, err := db.Open(path) // verify signature in server/internal/db; adapt if it returns a bundle
	if err != nil {
		return nil, err
	}
	return &dbStore{client: client, repo: repo.NewAppSettingRepo(client)}, nil
}

func (s *dbStore) Close() error { return s.client.Close() }

func (s *dbStore) Get(ctx context.Context, key string) (string, bool, error) {
	return s.repo.Get(ctx, key)
}

func (s *dbStore) List(ctx context.Context) (map[string]string, error) {
	rows, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[string]string, len(rows))
	for _, r := range rows {
		m[r.Key] = r.Value
	}
	return m, nil
}

func (s *dbStore) Set(ctx context.Context, key, value string) error {
	_, err := s.repo.Upsert(ctx, key, value)
	return err
}

// SetValidated checks the registry before writing.
func (s *dbStore) SetValidated(ctx context.Context, key, value string) error {
	def, ok := settings.Lookup(key)
	if !ok {
		return errUnknownKey(key)
	}
	if err := def.Validate(value); err != nil {
		return err
	}
	return s.Set(ctx, key, value)
}
```

Add a small error helper in the same file:

```go
import "fmt"

func errUnknownKey(key string) error { return fmt.Errorf("unknown setting key %q", key) }
```

> `db.Open`'s exact signature: check `server/internal/db`. The serve path uses `provideDB(cfg)` which wraps it. Use the lowest-level open that returns an `*ent.Client` and runs auto-migration, so `app_setting` exists. If only a bundle is exported, use it and extract `.Client`.

- [ ] **Step 4: Implement the cobra commands**

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/lx-wnk/agent-dashboard/server/internal/settings"
)

// resolveDBPath: --db flag > DASHBOARD_DB_PATH > default ~/.claude/dashboard-tasks.db
func resolveDBPath(cmd *cobra.Command) (string, error) {
	if p, _ := cmd.Flags().GetString("db"); p != "" {
		return p, nil
	}
	if p := os.Getenv("DASHBOARD_DB_PATH"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return home + "/.claude/dashboard-tasks.db", nil
}

func withStore(cmd *cobra.Command, fn func(ctx context.Context, s *dbStore) error) error {
	path, err := resolveDBPath(cmd)
	if err != nil {
		return err
	}
	store, err := openDBStore(path)
	if err != nil {
		return fmt.Errorf("open db %s: %w", path, err)
	}
	defer store.Close()
	return fn(cmd.Context(), store)
}

func newConfigDBCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Read/write DB-backed server settings (direct DB access)"}
	cmd.PersistentFlags().String("db", "", "Path to the dashboard SQLite DB (default: $DASHBOARD_DB_PATH or ~/.claude/dashboard-tasks.db)")

	list := &cobra.Command{Use: "list", Short: "List all settings (effective values)", RunE: func(cmd *cobra.Command, _ []string) error {
		return withStore(cmd, func(ctx context.Context, s *dbStore) error {
			rows, err := s.List(ctx)
			if err != nil {
				return err
			}
			for _, d := range settings.All() {
				val := d.Default
				if v, ok := rows[d.Key]; ok {
					val = v
				}
				fmt.Printf("%-28s = %-12s (%s, %s)\n", d.Key, val, d.Type, d.Apply)
			}
			return nil
		})
	}}

	get := &cobra.Command{Use: "get <key>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return withStore(cmd, func(ctx context.Context, s *dbStore) error {
			v, ok, err := s.Get(ctx, args[0])
			if err != nil {
				return err
			}
			if !ok {
				if d, found := settings.Lookup(args[0]); found {
					fmt.Println(d.Default)
					return nil
				}
				return errUnknownKey(args[0])
			}
			fmt.Println(v)
			return nil
		})
	}}

	set := &cobra.Command{Use: "set <key> <value>", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		return withStore(cmd, func(ctx context.Context, s *dbStore) error {
			if err := s.SetValidated(ctx, args[0], args[1]); err != nil {
				return err
			}
			fmt.Printf("set %s = %s\n", args[0], args[1])
			return nil
		})
	}}

	cmd.AddCommand(list, get, set)
	return cmd
}
```

> NOTE: the existing `newConfigCmd` in `cmd_config.go` manages the CLI's own host/token (HTTP client config). Rename this new command group to avoid the clash: register the new one as `server-config` OR rename the old HTTP one to `client-config`. RECOMMENDED: keep the old `config` for CLI client config, register this new group as `settings` (`dashboard settings list|get|set`). Adjust `Use:` to `"settings"` and the command constructor name to `newSettingsCmd`.

- [ ] **Step 5: Register in main.go**

In `server/cmd/cli/main.go`, add to `root.AddCommand(...)`:

```go
newSettingsCmd(),
```

(no `*CLIConfig` arg — these commands use the DB, not the HTTP client).

- [ ] **Step 6: Run to verify it passes**

Run: `cd server && go test ./cmd/cli/ -run TestDBStore -v && go build ./cmd/cli/...`
Expected: PASS + build.

- [ ] **Step 7: Commit**

```bash
git add server/cmd/cli/dbstore.go server/cmd/cli/cmd_settings.go server/cmd/cli/cmd_settings_test.go server/cmd/cli/main.go
git commit -m "feat: direct-DB CLI settings commands (list/get/set)"
```

## Task 8: Generic settings UI panel

**Files:**
- Create: `src/composables/useSettings.ts`
- Create: `src/components/AppSettings.vue`
- Modify: `src/components/ApiKeySettings.vue` (mount panel under a `settings`/`server` section)
- Test: `src/composables/useSettings.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it, vi } from 'vitest'
import { useSettings } from './useSettings'

describe('useSettings', () => {
  it('patches a setting and updates local state', async () => {
    const settings = [{ key: 'spawn.rateLimit', type: 'int', value: '5', default: '5', apply: 'restart', category: 'spawn' }]
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => settings }) // initial GET
      .mockResolvedValueOnce({ ok: true, json: async () => ({ key: 'spawn.rateLimit', value: '9', applied: 'restart' }) })

    const s = useSettings()
    await s.refetch()
    const applied = await s.update('spawn.rateLimit', '9')
    expect(applied).toBe('restart')
    expect(s.items.value.find(i => i.key === 'spawn.rateLimit')?.value).toBe('9')
  })
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `pnpm test src/composables/useSettings.test.ts`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement the composable**

```ts
import { ref } from 'vue'
import { errorMessage } from '../utils/errorMessage'

export interface SettingView {
  key: string
  type: string
  value: string
  default: string
  apply: 'live' | 'restart'
  category: string
  enum?: string[]
}

export function useSettings() {
  const items = ref<SettingView[]>([])
  const loading = ref(true)
  const error = ref<string | null>(null)

  async function refetch() {
    loading.value = true
    error.value = null
    try {
      const res = await fetch('/api/settings')
      if (!res.ok)
        throw new Error(`Failed to load settings (HTTP ${res.status})`)
      items.value = await res.json()
    }
    catch (e) {
      error.value = errorMessage(e, 'Failed to load settings')
    }
    finally {
      loading.value = false
    }
  }

  // update returns the apply-semantics so the caller can raise the right toast.
  async function update(key: string, value: string): Promise<'live' | 'restart'> {
    const res = await fetch(`/api/settings/${key}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ value }),
    })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    const saved = await res.json() as { key: string, value: string, applied: 'live' | 'restart' }
    items.value = items.value.map(i => (i.key === saved.key ? { ...i, value: saved.value } : i))
    return saved.applied
  }

  return { items, loading, error, refetch, update }
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `pnpm test src/composables/useSettings.test.ts`
Expected: PASS.

- [ ] **Step 5: Build `AppSettings.vue`**

Render `items` grouped by `category`. For each: a control by `type` (checkbox for bool, number for int/float, text for string/stringSlice, select for enum). On change call `update(key, value)`; if it returns `restart`, raise a warning toast "Applies after a server restart" (use the existing toast utility — grep `useToast`/`toast` in `src/`; if none exists, add a minimal one and note it). Mount in `ApiKeySettings.vue` under a new section (the `Section` union already lists `plugins`, `providers`; add `server` for this generic panel). Follow `ProviderSettings.vue` for layout + loading/error handling.

- [ ] **Step 6: Verify**

Run: `pnpm lint && pnpm typecheck && pnpm test`
Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add src/composables/useSettings.ts src/components/AppSettings.vue src/components/ApiKeySettings.vue src/composables/useSettings.test.ts
git commit -m "feat: generic DB-backed settings panel in UI"
```

---

# PHASE 2 — Live domain (plugins, auth, providers)

## Task 9: Plugin registry — enablement filter + `startEntry` refactor

**Files:**
- Modify: `server/internal/plugin/registry.go`
- Test: `server/internal/plugin/registry_test.go`

- [ ] **Step 1: Write the failing test** (skip-disabled)

```go
func TestRegistry_LoadSkipsDisabled(t *testing.T) {
	dir := t.TempDir()
	writePluginJSON(t, dir, "enabled-one", []string{"route_extension"})
	writePluginJSON(t, dir, "disabled-auth", []string{"auth_provider"})

	r := New(dir)
	r.SetEnabled(func(id string) bool { return id == "enabled-one" })

	// Neither plugin actually starts (no Command/health), but the capability
	// recording must reflect ONLY enabled plugins.
	_ = r.Load(context.Background(), Hooks{})

	assert.False(t, r.HasAttemptedCapability(CapAuthProvider),
		"disabled auth_provider plugin must not be recorded as attempted")
}
```

> `writePluginJSON` helper: write `<dir>/<id>/plugin.json` with `{"id","capabilities","addr":"127.0.0.1:0"}`. Add it to the test file if absent.

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/plugin/ -run TestRegistry_LoadSkipsDisabled -v`
Expected: FAIL — `SetEnabled` undefined / capability still recorded.

- [ ] **Step 3: Implement filter + refactor**

Add to the `Registry` struct:

```go
	enabled   func(id string) bool
	serverCtx context.Context
	hooks     Hooks
```

Add setter + default:

```go
// SetEnabled installs the predicate that decides which plugins Load starts and
// records. Defaults to all-enabled if never set (callers should set it).
func (r *Registry) SetEnabled(fn func(id string) bool) { r.enabled = fn }

func (r *Registry) isEnabled(id string) bool {
	if r.enabled == nil {
		return true
	}
	return r.enabled(id)
}
```

In `Load`, save the context+hooks for later `StartOne`, and skip disabled plugins **before** recording capabilities. Replace the per-entry body (current lines 84-151) so that right after a valid `desc` is parsed and the ID validated:

```go
		if !r.isEnabled(desc.ID) {
			slog.Info("plugin: skip — disabled", "id", desc.ID)
			continue
		}
```

is checked **before** the `for _, cap := range desc.Capabilities { r.attemptedCapabilities[cap] = true }` loop. Then extract the spawn+health+watch+register block (current lines 99-150) into:

```go
// startEntry starts (if it has a Command), health-checks, registers, and wires
// hooks for one descriptor. The caller holds no lock.
func (r *Registry) startEntry(serverCtx, startupCtx context.Context, pluginDir string, desc Descriptor, hooks Hooks) error {
	host, _, err := net.SplitHostPort(desc.Addr)
	if err != nil {
		return fmt.Errorf("invalid addr %q: %w", desc.Addr, err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("addr %q must be loopback", desc.Addr)
	}
	entry := Entry{Descriptor: desc, BaseURL: "http://" + desc.Addr, pluginDir: pluginDir}
	if len(desc.Command) > 0 {
		cmd := exec.CommandContext(serverCtx, desc.Command[0], desc.Command[1:]...)
		cmd.Dir = pluginDir
		cmd.Env = buildPluginEnv(desc.Env)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start: %w", err)
		}
		entry.cmd = cmd
	}
	if err := r.waitHealthy(startupCtx, entry.BaseURL); err != nil {
		if entry.cmd != nil {
			gracefulStop(entry.cmd, nil)
		}
		return fmt.Errorf("health: %w", err)
	}
	if entry.cmd != nil {
		done := make(chan struct{})
		entry.cmdDone = done
		go r.watchPlugin(serverCtx, entry.pluginDir, desc, entry.cmd, done)
	}
	r.mu.Lock()
	r.plugins = append(r.plugins, entry)
	r.mu.Unlock()
	if desc.HasCapability(CapAuthProvider) && hooks.SetAuth != nil {
		hooks.SetAuth(NewAuthProvider(entry), entry.BaseURL+"/login")
	}
	return nil
}
```

Rewrite the `Load` loop to: parse desc → validate ID → `isEnabled` check → record capabilities → `startEntry(...)` (logging errors with `continue` as today). Save `r.serverCtx = serverCtx; r.hooks = hooks` at the top of `Load`.

- [ ] **Step 4: Run to verify it passes**

Run: `cd server && go test ./internal/plugin/ -v`
Expected: PASS (existing plugin tests still green).

- [ ] **Step 5: Commit**

```bash
git add server/internal/plugin/registry.go server/internal/plugin/registry_test.go
git commit -m "feat: plugin registry enablement filter + startEntry refactor"
```

## Task 10: Plugin `StartOne` / `StopOne`

**Files:**
- Modify: `server/internal/plugin/registry.go`
- Test: `server/internal/plugin/registry_test.go`

- [ ] **Step 1: Write the failing test**

Use a real in-process HTTP plugin stub (a tiny `httptest`-style listener serving `/health` 200). Pattern:

```go
func TestRegistry_StartOneStopOne(t *testing.T) {
	dir := t.TempDir()
	// A plugin with no Command — registry only health-checks an external addr.
	ln := startHealthStub(t) // net.Listener on 127.0.0.1 serving 200 on /health
	addr := ln.Addr().String()
	writePluginJSONAddr(t, dir, "live-plugin", []string{"route_extension"}, addr)

	r := New(dir)
	r.SetEnabled(func(string) bool { return false }) // start disabled
	require.NoError(t, r.Load(context.Background(), Hooks{}))
	require.Nil(t, r.FindByCapability("route_extension"))

	require.NoError(t, r.StartOne(context.Background(), "live-plugin"))
	require.NotNil(t, r.FindByCapability("route_extension"))

	require.NoError(t, r.StopOne("live-plugin"))
	require.Nil(t, r.FindByCapability("route_extension"))
}
```

> `startHealthStub` + `writePluginJSONAddr` helpers go in the test file. The stub avoids spawning a real subprocess (descriptor has no `Command`, so the registry only does the health probe).

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/plugin/ -run TestRegistry_StartOneStopOne -v`
Expected: FAIL — `StartOne`/`StopOne` undefined.

- [ ] **Step 3: Implement**

```go
// StartOne starts a single plugin by id (reads its plugin.json fresh from the
// dir). Used by the live-enable path. No-op if already running.
func (r *Registry) StartOne(ctx context.Context, id string) error {
	if !pluginIDRe.MatchString(id) {
		return fmt.Errorf("plugin: invalid id %q", id)
	}
	r.mu.RLock()
	for i := range r.plugins {
		if r.plugins[i].Descriptor.ID == id {
			r.mu.RUnlock()
			return nil // already running
		}
	}
	serverCtx, hooks := r.serverCtx, r.hooks
	r.mu.RUnlock()
	if serverCtx == nil {
		serverCtx = ctx
	}
	descPath := filepath.Join(r.dir, id, "plugin.json")
	data, err := os.ReadFile(descPath)
	if err != nil {
		return fmt.Errorf("plugin: read %s: %w", descPath, err)
	}
	var desc Descriptor
	if err := json.Unmarshal(data, &desc); err != nil {
		return fmt.Errorf("plugin: invalid plugin.json for %q: %w", id, err)
	}
	if desc.ID != id {
		return fmt.Errorf("plugin: id mismatch in %s", descPath)
	}
	r.mu.Lock()
	for _, c := range desc.Capabilities {
		r.attemptedCapabilities[c] = true
	}
	r.mu.Unlock()
	startupCtx, cancel := context.WithTimeout(serverCtx, 30*time.Second)
	defer cancel()
	return r.startEntry(serverCtx, startupCtx, filepath.Join(r.dir, id), desc, hooks)
}

// StopOne stops and deregisters a single plugin by id. No-op if not running.
func (r *Registry) StopOne(id string) error {
	r.mu.Lock()
	var target *Entry
	for i := range r.plugins {
		if r.plugins[i].Descriptor.ID == id {
			e := r.plugins[i]
			target = &e
			break
		}
	}
	r.mu.Unlock()
	if target == nil {
		return nil
	}
	if target.cmd != nil {
		gracefulStop(target.cmd, target.cmdDone)
	}
	r.removeByID(id)
	return nil
}
```

> `watchPlugin` will also call `removeByID` if a stopped process's `Wait` returns an error; that's idempotent. For `StopOne` on a plugin with a `Command`, `gracefulStop` signals the watcher via `cmdDone`; the watcher sees `ctx`/clean-exit and returns without restarting (its restart guard checks `ctx.Err()` — for a non-shutdown stop the process exit is clean SIGTERM → `err==nil` path → returns). Acceptable for v1.

- [ ] **Step 4: Run to verify it passes**

Run: `cd server && go test ./internal/plugin/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/plugin/registry.go server/internal/plugin/registry_test.go
git commit -m "feat: live plugin StartOne/StopOne"
```

## Task 11: Plugins API (`GET /api/plugins`, `PATCH /api/plugins/{id}`)

**Files:**
- Create: `server/internal/api/plugins/handler.go`
- Test: `server/internal/api/plugins/handler_test.go`
- Modify: `server/cmd/serve/di.go` (route + construction)

- [ ] **Step 1: Write the failing test**

```go
package plugins_test
// GET returns discovered plugins (id, capabilities, enabled, healthy).
// PATCH {enabled:true} on a non-auth plugin -> calls StartOne, applied:"live".
// PATCH on an auth_provider plugin -> persists, applied:"restart".
// Use a fake with: Discovered() []PluginInfo, IsEnabled(id), SetEnabled(ctx,id,bool),
// Start(ctx,id), Stop(id). Assert applied + that Start was (not) called.
```

Write concrete assertions mirroring `providers/handler_test.go` shape (find it and copy structure). Define the handler's dependency as a small interface so the test can fake it:

```go
type pluginController interface {
	Discovered() []PluginInfo          // id, capabilities, healthy
	IsEnabled(id string) bool
	IsAuthProvider(id string) bool
	SetEnabled(ctx context.Context, id string, enabled bool) (applyLive bool, err error)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/api/plugins/ -v`
Expected: FAIL — package undefined.

- [ ] **Step 3: Implement the handler**

```go
package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
)

// PluginInfo is the API view of a discovered plugin.
type PluginInfo struct {
	ID           string   `json:"id"`
	Capabilities []string `json:"capabilities"`
	Enabled      bool     `json:"enabled"`
	Healthy      bool     `json:"healthy"`
	AuthProvider bool     `json:"authProvider"`
}

type Controller interface {
	Discovered() []PluginInfo
	SetEnabled(ctx context.Context, id string, enabled bool) (applyLive bool, err error)
}

type Handler struct{ c Controller }

func NewHandler(c Controller) *Handler { return &Handler{c: c} }

func (h *Handler) Mount(r chi.Router) {
	r.Get("/api/plugins", apierr.ErrorMiddleware(h.list))
	r.Patch("/api/plugins/{id}", apierr.ErrorMiddleware(h.patch))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(h.c.Discovered())
}

func (h *Handler) patch(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return fmt.Errorf("%w: invalid JSON", apierr.ErrBadRequest)
	}
	applyLive, err := h.c.SetEnabled(r.Context(), id, body.Enabled)
	if err != nil {
		return fmt.Errorf("%w: %s", apierr.ErrBadRequest, err.Error())
	}
	applied := "restart"
	if applyLive {
		applied = "live"
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(map[string]any{"id": id, "enabled": body.Enabled, "applied": applied})
}
```

- [ ] **Step 4: Implement the concrete Controller** (in `di.go` or a small `internal/pluginsctl` package)

```go
// pluginsController bridges the plugin registry + settings.Service to the API.
type pluginsController struct {
	reg      *plugin.Registry
	settings *settings.Service
	dir      string
}

func (c *pluginsController) Discovered() []pluginsapi.PluginInfo {
	// Read every plugin.json in dir for the full catalogue (incl. disabled),
	// mark Enabled from settings, Healthy from registry.FindByID presence.
	// Implement by scanning c.dir (mirror registry.Load's read loop, read-only).
	...
}

func (c *pluginsController) SetEnabled(ctx context.Context, id string, enabled bool) (bool, error) {
	cur := c.settings.StringSlice("plugins.enabled")
	next := addOrRemove(cur, id, enabled) // helper
	if err := c.settings.Set(ctx, "plugins.enabled", strings.Join(next, ",")); err != nil {
		return false, err
	}
	if isAuthProvider(c.dir, id) { // reads plugin.json capabilities
		return false, nil // restart-to-apply (router wiring is boot-time)
	}
	if enabled {
		return true, c.reg.StartOne(ctx, id)
	}
	return true, c.reg.StopOne(id)
}
```

> Register a `LiveHook` is unnecessary here because the controller calls `StartOne`/`StopOne` directly. Keep the registry's `SetEnabled` predicate bound to `settings` so a restart re-applies the same set: `reg.SetEnabled(func(id string) bool { for _, e := range settings.StringSlice("plugins.enabled") { if e==id {return true} }; return false })`.

Implement `Discovered`, `addOrRemove`, `isAuthProvider` fully (no placeholders) when writing the task — scan `c.dir` for subdirs with `plugin.json`, unmarshal `Descriptor`, set `Enabled` via the settings slice, `Healthy` via `reg.Infos()` membership, `AuthProvider` via `HasCapability("auth_provider")`.

- [ ] **Step 5: Wire route in di.go**

```go
pluginsCtl := &pluginsController{reg: pluginRegistry, settings: settingsSvc, dir: cfg.PluginDir}
pluginsHandler := pluginsapi.NewHandler(pluginsCtl)
pluginsHandler.Mount(r)
```

- [ ] **Step 6: Run + commit**

Run: `cd server && go test ./internal/api/plugins/ ./internal/plugin/ -v && go build ./...`
```bash
git add server/internal/api/plugins server/cmd/serve/di.go
git commit -m "feat: /api/plugins list + live enable/disable"
```

## Task 12: Bind plugin enablement at boot (guard now respects enabled)

**Files:**
- Modify: `server/cmd/serve/di.go` (before `pluginRegistry.Load`, ~line 136)

- [ ] **Step 1: Set the predicate before Load**

```go
pluginRegistry := plugin.New(cfg.PluginDir)
pluginRegistry.SetEnabled(func(id string) bool {
	for _, e := range settingsSvc.StringSlice("plugins.enabled") {
		if e == id {
			return true
		}
	}
	return false
})
```

Because `Load` now skips disabled plugins **before** recording capabilities (Task 9), the existing fatal guard at di.go:158-164 (`HasAttemptedCapability(CapAuthProvider)`) only fires when an **enabled** auth_provider plugin failed health-check — which is the correct fail-closed behavior. No change to the guard itself.

- [ ] **Step 2: Manual verification (the original bug)**

Run: `cd server && go build -o /tmp/ad ./cmd/serve`. With a fresh DB (empty `plugins.enabled`) and the OAuth plugin dirs present, start it:
`DASHBOARD_DB_PATH=/tmp/settings-test.db /tmp/ad serve`
Expected: boots — logs no "auth_provider plugin configured but failed health-check"; logs no attempt to start `github-oauth`/`office365-oauth`. Stop it.

- [ ] **Step 3: Commit**

```bash
git add server/cmd/serve/di.go
git commit -m "feat: gate plugin load on DB enablement (fixes unbuilt-auth-plugin boot block)"
```

## Task 13: Auth mode from settings

**Files:**
- Modify: `server/cmd/serve/di_router.go:13`
- Test: extend an existing di/router test or add `di_router_test.go`

- [ ] **Step 1: Write/extend a failing test** asserting `bypassAuth` follows `settings.auth.mode`:

```go
// With settings auth.mode="none" -> bypassAuth true; "plugin" -> false.
```

If `di_router` builds the router config from `cfg`, thread `settingsSvc` into `provideRouterConfig` and read `settingsSvc.String("auth.mode")` instead of `cfg.Auth`.

- [ ] **Step 2: Run → fail → implement**

Change `bypassAuth := cfg.Auth == "none"` to `bypassAuth := settingsSvc.String("auth.mode") == "none"` (pass `settingsSvc` into the function; update its callers in di.go).

- [ ] **Step 3: Run + commit**

```bash
cd server && go test ./cmd/serve/... -v && go build ./...
git add server/cmd/serve/di_router.go server/cmd/serve/di.go
git commit -m "feat: read auth mode from settings.Service"
```

## Task 14: Provider enablement — drop env fallback (single source)

**Files:**
- Modify: `server/cmd/serve/di.go` (provider settings wiring ~lines 106-115)
- Modify: `server/internal/provider/enabled.go` (remove allow-list param usage) or just pass empty allow

- [ ] **Step 1:** Change the provider `DefaultEnabled(...)` fallback to use an **empty** allowlist (no `cfg.ProvidersEnabled`), so the DB `provider_setting` table is the only source:

```go
fallback := provider.DefaultEnabled(providerRegistry.Descriptors(), nil)
```

- [ ] **Step 2:** Verify existing provider tests still pass; update any test asserting env-based enable.

Run: `cd server && go test ./internal/providersettings/ ./internal/provider/ -v`

- [ ] **Step 3: Commit**

```bash
git add server/cmd/serve/di.go server/internal/provider/enabled.go
git commit -m "feat: providers enablement DB-only (drop env allowlist)"
```

## Task 15: PluginSettings.vue → toggle list + toast

**Files:**
- Create: `src/composables/usePluginSettings.ts`
- Modify: `src/components/PluginSettings.vue`
- Test: `src/composables/usePluginSettings.test.ts`

- [ ] **Step 1: Write the failing test** (mirror `useProviderSettings` + the `applied` field)

```ts
// fetchPlugins GETs /api/plugins; toggle(id,true) PATCHes and returns applied.
// On applied==='restart' the caller shows a warning toast (assert returned value).
```

- [ ] **Step 2: Implement the composable** (copy `useProviderSettings.ts`, swap endpoint to `/api/plugins`, type `PluginInfo {id,capabilities,enabled,healthy,authProvider}`, `toggle` returns `applied`).

- [ ] **Step 3: Update `PluginSettings.vue`** from read-only to a toggle list: each plugin shows id, capabilities, health dot, enable switch. On toggle: call composable; if `applied==='restart'` (auth plugins) raise warning toast "Takes effect after a server restart — enabling will require login"; else success toast.

- [ ] **Step 4: Verify + commit**

```bash
pnpm lint && pnpm typecheck && pnpm test
git add src/composables/usePluginSettings.ts src/components/PluginSettings.vue src/composables/usePluginSettings.test.ts
git commit -m "feat: plugin enable/disable toggles in Settings UI"
```

---

# PHASE 3 — Operational migration + env trim

## Task 16: Migrate operational-key consumers to settings.Service

Each moved key currently read from `cfg.<Field>` must read from `settingsSvc.<Typed>("<key>")` at its construction site. **Worked example first, then the full checklist.**

**Files (example):** `server/cmd/serve/di.go` (spawn rate-limit middleware construction)

- [ ] **Step 1 (example): find the consumer**

Run: `cd server && grep -rn "cfg.SpawnRateLimit\|cfg.SpawnRateWindowMs" cmd/ internal/`

- [ ] **Step 2 (example): replace**

Where the spawn rate-limit middleware is built from `cfg.SpawnRateLimit`/`cfg.SpawnRateWindowMs`, change to:

```go
spawnLimiter := ratelimit.New(settingsSvc.Int("spawn.rateLimit"), time.Duration(settingsSvc.Int("spawn.rateWindowMs"))*time.Millisecond)
```

(adapt to the actual middleware constructor signature found in Step 1).

- [ ] **Step 3: Repeat for every moved key** — checklist (each: grep `cfg.<Field>`, replace with the accessor, no behavior change):

| Config field | settings accessor | consumer (grep target) |
|---|---|---|
| `cfg.Auth` | `settingsSvc.String("auth.mode")` | di_router (done in Task 13) |
| `cfg.AllowGitPush` | `settingsSvc.Bool("git.allowPush")` | `grep cfg.AllowGitPush` |
| `cfg.ForceWorktrees` | `settingsSvc.Bool("worktree.force")` | di_pipeline.go:73 |
| `cfg.SSEIntervalMs` | `settingsSvc.Int("sse.intervalMs")` | `grep cfg.SSEIntervalMs` |
| `cfg.ShutdownTimeoutSeconds` | `settingsSvc.Int("shutdown.timeoutSeconds")` | `grep cfg.ShutdownTimeoutSeconds` |
| `cfg.HooksDebounceMs` | `settingsSvc.Int("hooks.debounceMs")` | `grep cfg.HooksDebounceMs` |
| `cfg.HookEventsPerSession` | `settingsSvc.Int("hooks.eventsPerSession")` | `grep cfg.HookEventsPerSession` |
| `cfg.SpawnRateLimit` | `settingsSvc.Int("spawn.rateLimit")` | spawn middleware |
| `cfg.SpawnRateWindowMs` | `settingsSvc.Int("spawn.rateWindowMs")` | spawn middleware |
| `cfg.InjectRateLimit` | `settingsSvc.Int("inject.rateLimit")` | inject middleware |
| `cfg.InjectRateWindowMs` | `settingsSvc.Int("inject.rateWindowMs")` | inject middleware |
| `cfg.CostScanIntervalMs` | `settingsSvc.Int("cost.scanIntervalMs")` | cost scanner |
| `cfg.EvalScanIntervalMs` | `settingsSvc.Int("eval.scanIntervalMs")` | eval service |
| `cfg.EvalWindowHours` | `settingsSvc.Int("eval.windowHours")` | eval service |
| `cfg.EvalMinSamples` | `settingsSvc.Int("eval.minSamples")` | eval service |
| `cfg.EvalRateDropPP` | `settingsSvc.Float("eval.rateDropPP")` | eval service |
| `cfg.EvalStddevK` | `settingsSvc.Float("eval.stddevK")` | eval service |
| `cfg.ProvidersEnabled` | (removed — Task 14) | provider wiring |

- [ ] **Step 4: Build + test after each cluster of edits**

Run: `cd server && go build ./... && go test ./...`
Expected: green.

- [ ] **Step 5: Commit** (one commit, or grouped by subsystem)

```bash
git add server/cmd/serve server/internal
git commit -m "refactor: read operational settings from settings.Service"
```

## Task 17: Trim `config.Config` to bootstrap + ignored-key warnings

**Files:**
- Modify: `server/internal/config/config.go`
- Modify: `server/internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestLoad_MovedKeyInEnvIsIgnoredAndWarned(t *testing.T) {
	t.Setenv("DASHBOARD_SPAWN_RATE_LIMIT", "999")
	cfg, err := Load("")
	require.NoError(t, err)
	// Field no longer exists on Config; this test asserts Load still succeeds
	// and (optionally) that a warning was emitted. Assert via a captured slog
	// handler that a "no longer read from env" warning fired for the key.
	_ = cfg
}
```

- [ ] **Step 2: Run → fail** (compile error if test references removed field — keep the test field-free as above).

- [ ] **Step 3: Implement**
  - Remove moved fields from the `Config` struct and `Defaults()` (keep only: `Host`, `Port`, `JWTSecret`, `DBPath`, `PluginDir`, `ProviderDir`, `WorktreeRoot`, `HooksSecret`, `MCPToken`, `AuthPluginSecret`, `RemotesEnabled`).
  - Remove the moved keys from the `defaults` confmap and the moved validators (auth/eval/hooks validators now live in the settings registry).
  - After env load, warn for any still-set moved key:

```go
movedKeys := []string{
	"DASHBOARD_AUTH", "DASHBOARD_PROVIDERS_ENABLED", "DASHBOARD_ALLOW_GIT_PUSH",
	"DASHBOARD_FORCE_WORKTREES", "DASHBOARD_SSE_INTERVAL_MS", "DASHBOARD_SHUTDOWN_TIMEOUT_SECONDS",
	"DASHBOARD_HOOKS_DEBOUNCE_MS", "DASHBOARD_HOOK_EVENTS_PER_SESSION",
	"DASHBOARD_SPAWN_RATE_LIMIT", "DASHBOARD_SPAWN_RATE_WINDOW_MS",
	"DASHBOARD_INJECT_RATE_LIMIT", "DASHBOARD_INJECT_RATE_WINDOW_MS",
	"DASHBOARD_COST_SCAN_INTERVAL_MS", "DASHBOARD_EVAL_SCAN_INTERVAL_MS",
	"DASHBOARD_EVAL_WINDOW_HOURS", "DASHBOARD_EVAL_MIN_SAMPLES",
	"DASHBOARD_EVAL_RATE_DROP_PP", "DASHBOARD_EVAL_STDDEV_K",
}
for _, k := range movedKeys {
	if _, ok := os.LookupEnv(k); ok {
		slog.Warn("config: env var is no longer read — manage it via the Settings UI or 'dashboard settings set'", "key", k)
	}
}
```

  - Delete the now-obsolete `config_test.go` cases that asserted moved keys from env (eval defaults/validation, providers comma-split, spawn-from-env, hooks-positive). Their coverage moved to `settings` registry/service tests.

- [ ] **Step 4: Build whole tree**

Run: `cd server && go build ./... && go test ./...`
Expected: green. Fix any remaining `cfg.<RemovedField>` references the compiler flags (these are the Task 16 consumers — all must be migrated first).

- [ ] **Step 5: Commit**

```bash
git add server/internal/config
git commit -m "refactor: trim Config to bootstrap+secrets, warn on moved env keys"
```

## Task 18: Docs

**Files:**
- Modify: `docs/guides/configuration.md`, `CHANGELOG.md`, `.env.dist`

- [ ] **Step 1:** Rewrite `configuration.md`: split into "Bootstrap (env/flags)" — only the retained keys — and "Runtime settings (UI / `dashboard settings`)" — a table generated from the registry inventory (key, type, default, apply). Document the direct-DB CLI (`dashboard settings list|get|set`, `dashboard plugins`, `dashboard auth`) as the lockout-safe path. Note auth/plugin-auth changes need a restart.

- [ ] **Step 2:** `.env.dist`: remove the moved keys, leaving only the bootstrap+secret set; add a header comment pointing to the Settings UI / CLI for everything else.

- [ ] **Step 3:** `CHANGELOG.md` under `[Unreleased] → Added/Changed`: describe DB-backed settings, the UI panel, plugin enable/disable, the direct-DB CLI, and the **breaking change** that listed env vars are no longer read (Changed/BREAKING).

- [ ] **Step 4: Commit**

```bash
git add docs/guides/configuration.md CHANGELOG.md .env.dist
git commit -m "docs: document DB-backed settings + CLI, mark env-key removal"
```

---

## Final verification (before PR)

- [ ] `cd server && go build ./... && go test ./...` — all green.
- [ ] `cd server && golangci-lint run` — 0 issues.
- [ ] `pnpm lint && pnpm typecheck && pnpm test` — all green.
- [ ] Regenerate `THIRD_PARTY_LICENSES.md` only if deps changed (none expected — godotenv is on the other branch). If `go.sum` changed via ent regen, run `./scripts/gen-licenses.sh` and verify no spurious drift.
- [ ] Manual: fresh DB boots with broken OAuth plugins present (Task 12 Step 2); enable a voice plugin in the UI → starts live; `dashboard auth set none` recovers a `plugin`-locked DB.
- [ ] Confirm `runtime.go` shows only the intended schema change (revert non-schema ent drift).

## Self-review notes (author)

- Spec coverage: app_setting (T1-2), registry/SSOT (T3), service (T4), env split + warnings (T17), CLI direct-DB (T7), plugin live enable + guard (T9-12), auth mode (T13), providers DB-only (T14), UI (T8, T15), apply-semantics live/restart (registry + API + toasts), lockout-safety (T7 + T12). `worktree_root`/`adapters` correctly excluded.
- Type consistency: `settings.Service` accessors (`Bool/Int/Float/String/StringSlice`), `Apply`/`Type` constants, `applied` JSON field, and the plugin `applied:"live"|"restart"` shape are used identically across backend + frontend tasks.
- Open follow-ups deferred by design: live auth re-wiring (kept restart), per-project setting scoping, adapters migration.
