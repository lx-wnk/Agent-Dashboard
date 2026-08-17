# Plugin SP7 — Lifecycle Completeness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface plugin runtime health in the `PluginView` DTO and wire the `update` lifecycle verb end-to-end so the "Update" button in the UI has a working backend.

**Architecture:** A `HealthProbe` function type is injected into `pluginlifecyclectl.Controller` via its constructor; `List` calls it per plugin to populate the new `PluginView.Healthy` field. The `update` verb joins the closed `lifecycleActions` set and is dispatched from `controller.Transition` to `engine.Update`, which now also calls `StateRepo.SetManifestHash` so `updateAvailable` clears after a successful update. DI wires the probe from `plugin.Registry.Lookup` + `Entry.Healthy()`.

**Tech Stack:** Go 1.26 backend, ent ORM, Vue 3 TS frontend, go test, vitest

---

## Compatibility notes before starting

- This plan stacks on `feat/plugin-followups`. Do **not** disturb the SP6 settings-provider wiring already in `di.go`; all di.go edits here are additive (a new `probe` closure + passing it to `New`).
- `go test ./...` regenerates `server/internal/db/ent/` and can corrupt it. Prefer per-package `go test ./internal/<pkg>/...` throughout. If you run `go test ./...`, restore ent with `git checkout -- server/internal/db/ent/` afterwards.
- SSH commit signing hangs. All `git commit` calls must use `--no-gpg-sign`.
- All commits: Conventional English, no phase labels (SP7, etc.).

---

## Task 1 — Add `Healthy` to `PluginView` + update shape guard test

**Why first:** Every later task builds on the struct field existing; the shape test must be updated before the field is added or it fails in the wrong direction.

**Files:**
- `server/internal/api/plugins/handler_test.go`
- `server/internal/api/plugins/handler.go`

### Step 1a — Write failing test (update existing shape guard)

In `TestLifecycleList_ShapeAndLeakGuard` in `handler_test.go`:

1. Add `"healthy"` to the `required` slice:
```go
for _, required := range []string{"id", "name", "version", "state", "updateAvailable", "healthy", "capabilities", "hasSettings"} {
```

2. Change the key-count assertion:
```go
if len(item) != 8 {
    t.Errorf("response item has %d keys, want exactly 8; got: %v", len(item), item)
}
```

- [ ] Run: `cd server && go test ./internal/api/plugins/...`
- [ ] Expected result: `TestLifecycleList_ShapeAndLeakGuard` FAILS ("response item has 7 keys, want exactly 8")

### Step 1b — Add the field (minimal impl)

In `handler.go`, add `Healthy bool \`json:"healthy"\`` to `PluginView` after `UpdateAvailable`:

```go
type PluginView struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Version         string   `json:"version"`
	State           string   `json:"state"`
	UpdateAvailable bool     `json:"updateAvailable"`
	Healthy         bool     `json:"healthy"`
	Capabilities    []string `json:"capabilities"`
	HasSettings     bool     `json:"hasSettings"`
}
```

- [ ] Run: `cd server && go test ./internal/api/plugins/...`
- [ ] Expected result: ALL tests in the package pass (green)

### Step 1c — Commit

```bash
git commit --no-gpg-sign -m "feat(plugins): add Healthy field to PluginView DTO"
```

---

## Task 2 — Extend `StateRepo` and `Engine.Update` to refresh `manifest_hash`

**Why:** `Engine.Update` currently calls only `SetVersion`. After update, `manifest_hash` in the DB still mismatches the current manifest hash, so `updateAvailable` stays true. This task adds `SetManifestHash` to the repo interface and wires it in `Engine.Update`.

**Files:**
- `server/internal/pluginlifecycle/engine_test.go`
- `server/internal/pluginlifecycle/engine.go`
- `server/internal/db/repo/plugin_repo.go`

### Step 2a — Write failing test

In `engine_test.go`:

Add `manifestHash` field + `SetManifestHash` method to `fakePluginRepo`:
```go
type fakePluginRepo struct {
	installedAt  *time.Time
	active       bool
	version      string
	manifestHash string
}

func (f *fakePluginRepo) SetManifestHash(_ context.Context, _ string, h string) error {
	f.manifestHash = h
	return nil
}
```

Add new test at the end of the file:
```go
func TestEngine_UpdateRefreshesManifestHash(t *testing.T) {
	pr := &fakePluginRepo{}
	e := New(pr, &recordingHooks{}, &fakeClearer{}, nil)
	d := plugin.Descriptor{
		ID: "p1", Version: "2.0.0",
		Lifecycle: plugin.LifecycleHooks{Update: "/update"},
	}
	require.NoError(t, e.Update(context.Background(), d, "hash-v2"))
	assert.Equal(t, "2.0.0", pr.version)
	assert.Equal(t, "hash-v2", pr.manifestHash)
}
```

- [ ] Run: `cd server && go test ./internal/pluginlifecycle/...`
- [ ] Expected result: compilation failure — `SetManifestHash` not on `StateRepo`, `Update` has wrong signature

### Step 2b — Extend `StateRepo` interface and `Engine.Update`

In `engine.go`, add `SetManifestHash` to `StateRepo`:
```go
type StateRepo interface {
	GetState(ctx context.Context, id string) (State, error)
	SetInstalledAt(ctx context.Context, id string, at *time.Time) error
	SetActive(ctx context.Context, id string, active bool) error
	SetVersion(ctx context.Context, id, version string) error
	SetManifestHash(ctx context.Context, id, hash string) error
}
```

Change `Engine.Update` signature and body:
```go
func (e *Engine) Update(ctx context.Context, d plugin.Descriptor, manifestHash string) error {
	if err := e.withTransient(ctx, d.ID, func() error {
		return e.callHook(ctx, d, d.Lifecycle.Update)
	}); err != nil {
		return fmt.Errorf("update hook: %w", err)
	}
	if err := e.repo.SetVersion(ctx, d.ID, d.Version); err != nil {
		return err
	}
	return e.repo.SetManifestHash(ctx, d.ID, manifestHash)
}
```

### Step 2c — Add `SetManifestHash` to `repo.PluginRepo`

In `server/internal/db/repo/plugin_repo.go`:

Add to the `PluginRepo` interface:
```go
SetManifestHash(ctx context.Context, id, hash string) error
```

Add the implementation on `entPluginRepo`:
```go
func (r *entPluginRepo) SetManifestHash(ctx context.Context, id, hash string) error {
	return r.client.Plugin.UpdateOneID(id).SetManifestHash(hash).Exec(ctx)
}
```

- [ ] Run: `cd server && go test ./internal/pluginlifecycle/...`
- [ ] Expected result: ALL tests pass including `TestEngine_UpdateRefreshesManifestHash` (green)

> Note: `repo.PluginRepo` is also satisfied by `pluginStateRepoAdapter` in `plugin_adapters.go`. That adapter will be updated in Task 6 (DI wiring). The build in Task 6 verifies the complete chain.

### Step 2d — Commit

```bash
git commit --no-gpg-sign -m "feat(pluginlifecycle): refresh manifest_hash on Engine.Update to clear updateAvailable"
```

---

## Task 3 — Add `Update` to the controller's `Engine` interface and `case "update"` to `Transition`

**Files:**
- `server/internal/pluginlifecyclectl/controller_test.go`
- `server/internal/pluginlifecyclectl/controller.go`

### Step 3a — Write failing test; extend fakes

In `controller_test.go`:

Add `Update` to `fakeEngine` (after `Uninstall`):
```go
func (f *fakeEngine) Update(_ context.Context, d plugin.Descriptor, hash string) error {
	f.calls = append(f.calls, "update:"+d.ID+":"+hash)
	return nil
}
```

Add `Update` to `slowEngine` (after `Uninstall`):
```go
func (e *slowEngine) Update(_ context.Context, d plugin.Descriptor, _ string) error {
	e.record("start:update:" + d.ID)
	time.Sleep(e.delay)
	e.record("end:update:" + d.ID)
	return nil
}
```

Add new test (after `TestTransition_DispatchesAndReturnsState`):
```go
func TestTransition_UpdateDispatchesToEngine(t *testing.T) {
	now := time.Now()
	// ManifestHash == loader hash → updateAvailable=false after update
	repo := &fakeRepo{rows: map[string]*ent.Plugin{
		"p1": {ID: "p1", Name: "P1", Version: "1.0", InstalledAt: ptrTime(now), ManifestHash: "h-new"},
	}}
	engine := &fakeEngine{}
	loader := &fakeLoader{
		manifests: map[string]plugin.Descriptor{"p1": {ID: "p1", Version: "2.0"}},
		hashes:    map[string]string{"p1": "h-new"},
	}
	c := pluginlifecyclectl.NewWithLoader(repo, engine, &fakeSettings{}, loader)

	view, err := c.Transition(context.Background(), "p1", "update")
	if err != nil {
		t.Fatalf("Transition update: %v", err)
	}
	if len(engine.calls) != 1 || engine.calls[0] != "update:p1:h-new" {
		t.Errorf("engine calls: %v", engine.calls)
	}
	if view.UpdateAvailable {
		t.Error("updateAvailable should be false after update (stored hash now matches manifest hash)")
	}
}
```

> Note: This test uses the current `NewWithLoader` signature (no probe parameter). The probe parameter is added in Task 5; at that point all existing `NewWithLoader` calls will be updated together.

- [ ] Run: `cd server && go test ./internal/pluginlifecyclectl/...`
- [ ] Expected result: compilation failure — `Engine` interface missing `Update`, `fakeEngine`/`slowEngine` methods added but the interface in `controller.go` doesn't declare `Update` yet so dispatching "update" returns `ErrInvalidAction`

### Step 3b — Add `Update` to Engine interface + `case "update"` to Transition

In `controller.go`, add to the `Engine` interface:
```go
type Engine interface {
	Install(ctx context.Context, d plugin.Descriptor) error
	Activate(ctx context.Context, d plugin.Descriptor) error
	Deactivate(ctx context.Context, d plugin.Descriptor) error
	Uninstall(ctx context.Context, d plugin.Descriptor) error
	Update(ctx context.Context, d plugin.Descriptor, manifestHash string) error
}
```

Add to the `switch action` in `Transition` (before `default`):
```go
case "update":
    err = c.engine.Update(ctx, desc, hash)
```

- [ ] Run: `cd server && go test ./internal/pluginlifecyclectl/...`
- [ ] Expected result: ALL tests pass including `TestTransition_UpdateDispatchesToEngine` (green)

### Step 3c — Commit

```bash
git commit --no-gpg-sign -m "feat(pluginlifecyclectl): wire update verb to Engine.Update with manifest hash"
```

---

## Task 4 — Add `"update"` to `lifecycleActions` + handler-level test

**Files:**
- `server/internal/api/plugins/handler_test.go`
- `server/internal/api/plugins/handler.go`

### Step 4a — Write failing test

Add to `handler_test.go` (after `TestLifecycleTransition_IllegalTransition_409`):
```go
func TestLifecycleTransition_UpdateAccepted(t *testing.T) {
	ctl := &fakeLifecycle{transition: plugins.PluginView{ID: "p1", State: "active", Capabilities: []string{}}}
	req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/plugins/p1/update", nil))
	rr := httptest.NewRecorder()
	mountLifecycle(t, ctl).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for update action, got %d: %s", rr.Code, rr.Body.String())
	}
	if ctl.gotAction != "update" {
		t.Errorf("controller action: got %q, want update", ctl.gotAction)
	}
}
```

Also add a test confirming `update` on a non-installed plugin returns 409:
```go
func TestLifecycleTransition_UpdateIllegalTransition_409(t *testing.T) {
	ctl := &fakeLifecycle{transErr: fmt.Errorf("%w: p1 not installed", pluginlifecycle.ErrIllegalTransition)}
	req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/plugins/p1/update", nil))
	rr := httptest.NewRecorder()
	mountLifecycle(t, ctl).ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409 for illegal update, got %d: %s", rr.Code, rr.Body.String())
	}
}
```

- [ ] Run: `cd server && go test ./internal/api/plugins/...`
- [ ] Expected result: `TestLifecycleTransition_UpdateAccepted` FAILS (400 — "update" not in lifecycleActions)

### Step 4b — Add `"update"` to `lifecycleActions`

In `handler.go`:
```go
var lifecycleActions = map[string]bool{
	"install":    true,
	"activate":   true,
	"deactivate": true,
	"uninstall":  true,
	"update":     true,
}
```

- [ ] Run: `cd server && go test ./internal/api/plugins/...`
- [ ] Expected result: ALL tests pass including both new tests (green)

### Step 4c — Commit

```bash
git commit --no-gpg-sign -m "feat(api/plugins): accept update as a valid lifecycle action verb"
```

---

## Task 5 — HealthProbe seam: add probe to `Controller` + `NewWithLoader`

**Files:**
- `server/internal/pluginlifecyclectl/controller_test.go`
- `server/internal/pluginlifecyclectl/controller.go`

### Step 5a — Write failing test + update all existing NewWithLoader calls

First, add the new test for probe behaviour (at the end of `controller_test.go`):
```go
func TestList_HealthProbeSetHealthy(t *testing.T) {
	now := time.Now()
	repo := &fakeRepo{list: []*ent.Plugin{
		{ID: "p1", InstalledAt: ptrTime(now), Active: true, ManifestHash: "h"},
		{ID: "p2", InstalledAt: ptrTime(now), Active: true, ManifestHash: "h"},
		{ID: "p3"},
	}}
	loader := &fakeLoader{
		manifests: map[string]plugin.Descriptor{"p1": {}, "p2": {}, "p3": {}},
		hashes:    map[string]string{"p1": "h", "p2": "h", "p3": "h"},
	}
	// p1 running+healthy, p2 and p3 not running
	probe := func(id string) (bool, bool) {
		if id == "p1" {
			return true, true
		}
		return false, false
	}
	c := pluginlifecyclectl.NewWithLoader(repo, &fakeEngine{}, &fakeSettings{}, loader, probe)

	views, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(views) != 3 {
		t.Fatalf("expected 3 views, got %d", len(views))
	}
	healthy := map[string]bool{}
	for _, v := range views {
		healthy[v.ID] = v.Healthy
	}
	if !healthy["p1"] {
		t.Error("p1: running+healthy probe should yield Healthy=true")
	}
	if healthy["p2"] {
		t.Error("p2: not running should yield Healthy=false")
	}
	if healthy["p3"] {
		t.Error("p3: absent from registry should yield Healthy=false")
	}
}
```

Then update ALL existing `NewWithLoader` calls in `controller_test.go` to pass `nil` as the last argument:

| Test | Old call | New call |
|------|----------|----------|
| `TestList_DerivesStateAndFlags` | `NewWithLoader(repo, &fakeEngine{}, &fakeSettings{}, loader)` | `NewWithLoader(repo, &fakeEngine{}, &fakeSettings{}, loader, nil)` |
| `TestTransition_DispatchesAndReturnsState` | `NewWithLoader(repo, engine, &fakeSettings{}, loader)` | `NewWithLoader(repo, engine, &fakeSettings{}, loader, nil)` |
| `TestTransition_InvalidAction` | `NewWithLoader(repo, &fakeEngine{}, &fakeSettings{}, loader)` | `NewWithLoader(repo, &fakeEngine{}, &fakeSettings{}, loader, nil)` |
| `TestTransition_UnknownPlugin` | `NewWithLoader(repo, &fakeEngine{}, &fakeSettings{}, loader)` | `NewWithLoader(repo, &fakeEngine{}, &fakeSettings{}, loader, nil)` |
| `TestGetSettings_DelegatesWithSchema` | `NewWithLoader(repo, &fakeEngine{}, settings, loader)` | `NewWithLoader(repo, &fakeEngine{}, settings, loader, nil)` |
| `TestTransition_SamePluginSerializes` | `NewWithLoader(repo, eng, &fakeSettings{}, loader)` | `NewWithLoader(repo, eng, &fakeSettings{}, loader, nil)` |
| `TestPutSettings_DelegatesWithSchema` | `NewWithLoader(repo, &fakeEngine{}, settings, loader)` | `NewWithLoader(repo, &fakeEngine{}, settings, loader, nil)` |
| `TestTransition_UpdateDispatchesToEngine` | `NewWithLoader(repo, engine, &fakeSettings{}, loader)` | `NewWithLoader(repo, engine, &fakeSettings{}, loader, nil)` |

- [ ] Run: `cd server && go test ./internal/pluginlifecyclectl/...`
- [ ] Expected result: compilation failure — `NewWithLoader` still takes 4 args

### Step 5b — Add `HealthProbe` + probe field to Controller; update `New`/`NewWithLoader`; fill `Healthy` in `List`

In `controller.go`:

Add the type after the package comment block:
```go
// HealthProbe reports the runtime status of a plugin. A nil probe always
// returns false, false (plugin considered not running / not healthy).
type HealthProbe func(id string) (running bool, healthy bool)
```

Add `probe` to the `Controller` struct:
```go
type Controller struct {
	repo     Repo
	engine   Engine
	settings Settings
	loader   ManifestLoader
	probe    HealthProbe

	locksMu        sync.Mutex
	perPluginLocks map[string]*sync.Mutex
}
```

Update `New`:
```go
func New(r Repo, engine Engine, settings Settings, dir string, probe HealthProbe) *Controller {
	return NewWithLoader(r, engine, settings, FileManifestLoader{Dir: dir}, probe)
}
```

Update `NewWithLoader`:
```go
func NewWithLoader(r Repo, engine Engine, settings Settings, loader ManifestLoader, probe HealthProbe) *Controller {
	return &Controller{
		repo:           r,
		engine:         engine,
		settings:       settings,
		loader:         loader,
		probe:          probe,
		perPluginLocks: make(map[string]*sync.Mutex),
	}
}
```

In `List`, add probe call after the manifest block:
```go
func (c *Controller) List(ctx context.Context) ([]plugins.PluginView, error) {
	rows, err := c.repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("pluginlifecyclectl.List: %w", err)
	}
	out := make([]plugins.PluginView, 0, len(rows))
	for _, p := range rows {
		view := plugins.PluginView{
			ID:           p.ID,
			Name:         p.Name,
			Version:      p.Version,
			State:        deriveState(p),
			Capabilities: []string{},
		}
		if desc, hash, lerr := c.loader.Load(p.ID, p.Path); lerr == nil {
			view.Capabilities = nonNilCaps(desc.Capabilities)
			view.HasSettings = len(desc.Settings) > 0
			view.UpdateAvailable = hash != "" && hash != p.ManifestHash
		}
		if c.probe != nil {
			running, healthy := c.probe(p.ID)
			view.Healthy = running && healthy
		}
		out = append(out, view)
	}
	return out, nil
}
```

- [ ] Run: `cd server && go test ./internal/pluginlifecyclectl/...`
- [ ] Expected result: ALL tests pass including `TestList_HealthProbeSetHealthy` (green)

### Step 5c — Commit

```bash
git commit --no-gpg-sign -m "feat(pluginlifecyclectl): inject HealthProbe seam and populate Healthy in List"
```

---

## Task 6 — DI wiring: probe closure + `SetManifestHash` adapter

**Files:**
- `server/cmd/serve/plugin_adapters.go`
- `server/cmd/serve/di.go`

### Step 6a — Add `SetManifestHash` to `pluginStateRepoAdapter`

In `plugin_adapters.go`, add after `SetVersion`:
```go
func (a pluginStateRepoAdapter) SetManifestHash(ctx context.Context, id, hash string) error {
	return a.inner.SetManifestHash(ctx, id, hash)
}
```

### Step 6b — Build probe closure and pass it to `pluginlifecyclectl.New` in `di.go`

Locate the line (around line 292 in `di.go`):
```go
lifecycleController := pluginlifecyclectl.New(pluginRepo, lifecycleEngine, pluginSettingsSvc, cfg.PluginDir)
```

Replace it with (keep the `pluginlifecyclectl.New` call; add probe closure just before it):
```go
lifecycleProbe := func(id string) (bool, bool) {
    e, ok := pluginRegistry.Lookup(id)
    return ok, ok && e.Healthy()
}
lifecycleController := pluginlifecyclectl.New(pluginRepo, lifecycleEngine, pluginSettingsSvc, cfg.PluginDir, lifecycleProbe)
```

Do not disturb any other line in the `if entClient != nil` block.

- [ ] Run: `cd server && go build ./...`
- [ ] Expected result: clean build, no errors

### Step 6c — Run full package tests for all touched packages

```bash
cd server && go test ./internal/pluginlifecycle/... ./internal/pluginlifecyclectl/... ./internal/api/plugins/... ./internal/db/repo/...
```

- [ ] Expected result: all pass

### Step 6d — Commit

```bash
git commit --no-gpg-sign -m "feat(di): wire HealthProbe from plugin registry into lifecycle controller"
```

---

## Task 7 — Frontend composable: `healthy` field + `update()` function

**Files:**
- `src/composables/__tests__/usePluginSettings.spec.ts` (new)
- `src/composables/usePluginSettings.ts`

### Step 7a — Write failing vitest test

Create `src/composables/__tests__/usePluginSettings.spec.ts`:

```ts
import { afterEach, describe, expect, it, vi } from 'vitest'
import { usePluginSettings } from '../usePluginSettings'

// onMounted is registered but never invoked in a plain vitest context (no
// component instance), so no initial fetchPlugins fires automatically.

describe('usePluginSettings.update', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('posts to /api/plugins/{id}/update with Origin header', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true }))
    const { update } = usePluginSettings()
    await update('my-plugin')
    expect(vi.mocked(global.fetch)).toHaveBeenCalledWith(
      '/api/plugins/my-plugin/update',
      expect.objectContaining({
        method: 'POST',
        headers: expect.objectContaining({ Origin: expect.any(String) }),
      }),
    )
  })

  it('throws with HTTP status when response is not ok', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 409,
      json: async () => ({ error: 'illegal transition' }),
    }))
    const { update } = usePluginSettings()
    await expect(update('my-plugin')).rejects.toThrow('illegal transition')
  })
})

describe('PluginView type', () => {
  it('healthy field is present in the interface (type-only check)', () => {
    // If PluginView does not have healthy, TypeScript compilation fails here.
    const view: import('../usePluginSettings').PluginView = {
      id: 'p',
      name: 'P',
      version: '1.0',
      state: 'active',
      updateAvailable: false,
      healthy: true,
      capabilities: [],
      hasSettings: false,
    }
    expect(view.healthy).toBe(true)
  })
})
```

- [ ] Run: `pnpm test --run src/composables/__tests__/usePluginSettings.spec.ts`
- [ ] Expected result: compilation error on `update` (method does not exist) and on `healthy` field (not in interface)

### Step 7b — Add `healthy` to `PluginView` and `update()` to composable

In `src/composables/usePluginSettings.ts`:

Update `PluginView` interface (add `healthy: boolean`):
```ts
export interface PluginView {
  id: string
  name: string
  version: string
  state: 'discovered' | 'inactive' | 'active'
  updateAvailable: boolean
  healthy: boolean
  capabilities: string[]
  hasSettings: boolean
}
```

Add `update` function after `putSettings`:
```ts
async function update(id: string): Promise<void> {
  const res = await fetch(`/api/plugins/${id}/update`, {
    method: 'POST',
    headers: { Origin: window.location.origin },
  })
  if (!res.ok) {
    let detail = `HTTP ${res.status}`
    try {
      const b = await res.json()
      if (b?.error)
        detail = b.error
    }
    catch { /* no body */ }
    throw new Error(detail)
  }
  await fetchPlugins()
}
```

Update the `return` statement:
```ts
return { plugins, loading, error, refetch: fetchPlugins, setActive, getSettings, putSettings, update }
```

- [ ] Run: `pnpm test --run src/composables/__tests__/usePluginSettings.spec.ts`
- [ ] Expected result: all 3 tests pass (green)

- [ ] Run: `pnpm typecheck`
- [ ] Expected result: no type errors

### Step 7c — Commit

```bash
git commit --no-gpg-sign -m "feat(frontend): add healthy field to PluginView and update() composable method"
```

---

## Task 8 — Frontend template: health dot hint + Update button

**Files:**
- `src/components/PluginSettings.vue`

### Step 8a — Destructure `update` from composable

In `PluginSettings.vue`, update the destructuring line:
```ts
const { plugins, loading, error, setActive, getSettings, putSettings, update } = usePluginSettings()
```

### Step 8b — Add `handleUpdate` handler

Add after `handleToggle`:
```ts
async function handleUpdate(id: string) {
  saving.value = id
  try {
    await update(id)
    showNotice('success', 'Plugin updated successfully')
  }
  catch (e) {
    error.value = errorMessage(e, 'Update failed')
  }
  finally {
    saving.value = null
  }
}
```

### Step 8c — Update state dot to reflect health

Replace the existing `<span>` state dot (lines 136–140 in the current file) with:
```html
<span
  class="inline-block h-2 w-2 rounded-full shrink-0"
  :class="p.state === 'active' && p.healthy
    ? 'bg-success-text'
    : p.state === 'active'
      ? 'bg-warning-text'
      : 'bg-line-strong'"
  :title="p.state === 'active' && !p.healthy
    ? 'Active — not currently running'
    : p.state === 'active'
      ? 'Active'
      : p.state"
  :aria-label="p.state === 'active' && !p.healthy
    ? 'Active — not currently running'
    : p.state === 'active'
      ? 'Active'
      : p.state"
/>
```

### Step 8d — Add Update button

Add the Update button inside the `<div class="flex items-center gap-3 shrink-0">` block, before the settings button:
```html
<button
  v-if="p.updateAvailable"
  type="button"
  class="text-xs text-accent underline-offset-2 hover:underline disabled:opacity-50"
  :disabled="saving === p.id"
  @click="handleUpdate(p.id)"
>
  Update
</button>
```

- [ ] Run: `pnpm typecheck`
- [ ] Expected result: no errors

- [ ] Run: `pnpm test`
- [ ] Expected result: all frontend tests pass

### Step 8e — Commit

```bash
git commit --no-gpg-sign -m "feat(PluginSettings): show health dot hint and Update button when updateAvailable"
```

---

## Task 9 — Final verification

- [ ] `cd server && go build ./...` — clean build
- [ ] `cd server && go test ./internal/pluginlifecycle/... ./internal/pluginlifecyclectl/... ./internal/api/plugins/...`
- [ ] `cd /Users/alexanderwink/code/_privat/projects/agent-dashboard && pnpm test`
- [ ] `pnpm typecheck`
- [ ] Restore ent if corrupted: `git checkout -- server/internal/db/ent/`

If any step fails, fix and commit before declaring done. Do not report finished while CI is red.
