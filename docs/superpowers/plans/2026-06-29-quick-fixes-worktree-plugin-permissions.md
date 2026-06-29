# Quick Fixes: worktree.force default, plugin auto-install, permissions panel

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship three independent bug-fixes in a single PR: (1) flip `worktree.force` default to `true`, (2) auto-install a discovered plugin when the user activates it, (3) fix the Permissions settings panel so it renders project names, entry counts, and Reset correctly.

**Architecture:** Three independent, non-overlapping changes. Fix 1 is a one-line registry edit. Fix 2 extracts a private `performInstall` helper in the lifecycle engine so the auto-install-on-activate path reuses existing hook logic without duplication. Fix 3 replaces the Permissions panel's hand-rolled fetch + drifted local type with the existing `usePermissionPresets` composable and corrects the template field names. No ent schema change in any fix.

**Tech Stack:** Go 1.26 + Vue 3 TS; `go test` (scoped to touched packages only), vitest.

**Branch:** `feat/plugin-followups`

---

## Gotchas (read before starting)

- **NEVER run `go test ./...`** from `server/` — it regenerates the entire `server/internal/db/ent/` tree and can corrupt it with unused-import build failures. Scope every `go test` call to the touched package (e.g. `go test ./internal/settings/`, `go test ./internal/pluginlifecycle/`). If `./...` runs by accident, restore immediately with `git checkout -- server/internal/db/ent/`.
- All commits **must** use `--no-gpg-sign` (SSH signing hangs in this env).
- Conventional Commits, English only, no phase/task labels in the message.
- Final verify before the PR: `cd server && go build ./...`; `pnpm test` (scoped); `pnpm typecheck`; `pnpm lint`.

---

## Fix 1 — `worktree.force` default → `true`

### Task F1: Registry default, test update, docs

**Files**
- Modify: `server/internal/settings/registry.go` (line 111)
- Modify: `server/internal/settings/registry_test.go`
- Modify: `server/internal/settings/service_test.go` (line 26)
- Modify: `docs/guides/configuration.md` (table row for `worktree.force`)
- Modify: `CHANGELOG.md` (`### Changed` section under `[Unreleased]`)

- [ ] **Step 1: Write failing test in `registry_test.go`**

Add a new assertion inside `TestRegistry_DefaultsAndValidation` immediately after the existing `spawn.rateLimit` block:

```go
wt, ok := Lookup("worktree.force")
require.True(t, ok)
assert.Equal(t, TypeBool, wt.Type)
assert.Equal(t, "true", wt.Default)
```

Run: `cd server && go test ./internal/settings/` → FAIL (`"false" != "true"`).

- [ ] **Step 2: Change the registry default**

In `server/internal/settings/registry.go`, line 111, change:

```go
{Key: "worktree.force", Type: TypeBool, Default: "false", Apply: ApplyRestart, Category: "worktree"},
```

to:

```go
{Key: "worktree.force", Type: TypeBool, Default: "true", Apply: ApplyRestart, Category: "worktree"},
```

- [ ] **Step 3: Fix the service test that asserts the old default**

In `server/internal/settings/service_test.go`, line 26, change:

```go
assert.False(t, svc.Bool("worktree.force"))
```

to:

```go
assert.True(t, svc.Bool("worktree.force"))
```

Run: `cd server && go test ./internal/settings/` → PASS.

- [ ] **Step 4: Update docs**

In `docs/guides/configuration.md`, find the table row (currently around line 75):

```
| `worktree.force` | bool | `false` | restart |
```

Change to:

```
| `worktree.force` | bool | `true` | restart |
```

In `CHANGELOG.md`, under `## [Unreleased]` → `### Changed`, append:

```
- `worktree.force` setting now defaults to `true` — pipeline tasks automatically create a git worktree per task without requiring explicit `SourceBranch`. Set to `false` to restore the previous opt-in behaviour.
```

- [ ] **Step 5: Commit**

```bash
git add server/internal/settings/registry.go \
        server/internal/settings/registry_test.go \
        server/internal/settings/service_test.go \
        docs/guides/configuration.md \
        CHANGELOG.md
git commit --no-gpg-sign -m "fix: default worktree.force to true so pipeline tasks always use isolated worktrees"
```

---

## Fix 2 — Plugin auto-install on activate (voice-whisper 409)

### Task F2: Tests + engine refactor

**Files**
- Modify: `server/internal/pluginlifecycle/engine_test.go`
- Modify: `server/internal/pluginlifecycle/engine.go`

#### Sub-step A: Write failing tests

- [ ] **Step 1: Add two new tests to `engine_test.go`**

Append after `TestInstallWrapsHooksInTransient`:

```go
// TestActivate_DiscoveredPlugin_AutoInstallsAndActivates verifies that calling
// Activate on a plugin with no installed_at (state=discovered) implicitly
// installs it first then activates it, without requiring a separate Install call.
func TestActivate_DiscoveredPlugin_AutoInstallsAndActivates(t *testing.T) {
	pr := &fakePluginRepo{} // installedAt=nil → discovered
	hk := &recordingHooks{}
	e := New(pr, hk, &fakeClearer{}, nil)
	ctx := context.Background()

	d := plugin.Descriptor{
		ID:      "voice-whisper",
		Version: "1.0.0",
		Lifecycle: plugin.LifecycleHooks{
			Install:  "/install",
			Activate: "/activate",
		},
	}

	require.NoError(t, e.Activate(ctx, d))
	assert.NotNil(t, pr.installedAt, "installedAt must be set by the implicit install")
	assert.True(t, pr.active, "plugin must be active after Activate")
	assert.Contains(t, hk.called, "/install", "install hook must run")
	assert.Contains(t, hk.called, "/activate", "activate hook must run")
}

// TestActivate_InactivePlugin_NoDoubleInstall verifies that calling Activate on
// an already-installed (inactive) plugin does not re-run the install hook.
func TestActivate_InactivePlugin_NoDoubleInstall(t *testing.T) {
	now := time.Now()
	pr := &fakePluginRepo{installedAt: &now, active: false}
	hk := &recordingHooks{}
	e := New(pr, hk, &fakeClearer{}, nil)
	ctx := context.Background()

	d := plugin.Descriptor{
		ID:      "voice-whisper",
		Version: "1.0.0",
		Lifecycle: plugin.LifecycleHooks{
			Install:  "/install",
			Activate: "/activate",
		},
	}

	require.NoError(t, e.Activate(ctx, d))
	assert.True(t, pr.active)
	assert.NotContains(t, hk.called, "/install", "install hook must NOT run for an already-installed plugin")
	assert.Contains(t, hk.called, "/activate")
}

// TestActivate_DiscoveredPlugin_InstallHookFail_AbortsBeforeActivate verifies
// that a failing install hook during auto-install aborts before the activate
// hook runs and before the plugin is marked active.
func TestActivate_DiscoveredPlugin_InstallHookFail_AbortsBeforeActivate(t *testing.T) {
	pr := &fakePluginRepo{}
	hk := &recordingHooks{failOn: "/install"}
	e := New(pr, hk, &fakeClearer{}, nil)
	ctx := context.Background()

	d := plugin.Descriptor{
		ID:      "voice-whisper",
		Version: "1.0.0",
		Lifecycle: plugin.LifecycleHooks{
			Install:  "/install",
			Activate: "/activate",
		},
	}

	require.Error(t, e.Activate(ctx, d))
	assert.Nil(t, pr.installedAt, "installedAt must not be set when install hook fails")
	assert.False(t, pr.active, "plugin must not be active when install aborts")
	assert.NotContains(t, hk.called, "/activate", "activate hook must not run when install fails")
}
```

Run: `cd server && go test ./internal/pluginlifecycle/` → FAIL (Activate still returns `ErrIllegalTransition` for discovered plugins).

#### Sub-step B: Implement `performInstall` + update `Activate`

- [ ] **Step 2: Extract `performInstall` helper and update `Activate` in `engine.go`**

Replace the existing `Install` and `Activate` methods with the following. The new private `performInstall` holds the hook-run + stamp logic that both paths share; `Install` guards against already-installed and delegates; `Activate` auto-installs when `InstalledAt == nil`.

```go
// performInstall runs the install/post-install hooks and stamps InstalledAt.
// It does not guard against already-installed — callers are responsible for
// that check. This is the shared install step used by both Install and the
// auto-install path in Activate.
func (e *Engine) performInstall(ctx context.Context, d plugin.Descriptor) error {
	if err := e.withTransient(ctx, d.ID, func() error {
		if err := e.callHook(ctx, d, d.Lifecycle.Install); err != nil {
			return fmt.Errorf("install hook: %w", err)
		}
		return e.callHook(ctx, d, d.Lifecycle.PostInstall)
	}); err != nil {
		return err
	}
	now := time.Now()
	return e.repo.SetInstalledAt(ctx, d.ID, &now)
}

func (e *Engine) Install(ctx context.Context, d plugin.Descriptor) error {
	st, err := e.repo.GetState(ctx, d.ID)
	if err != nil {
		return err
	}
	if st.InstalledAt != nil {
		return fmt.Errorf("%w: %s already installed", ErrIllegalTransition, d.ID)
	}
	return e.performInstall(ctx, d)
}

func (e *Engine) Activate(ctx context.Context, d plugin.Descriptor) error {
	st, err := e.repo.GetState(ctx, d.ID)
	if err != nil {
		return err
	}
	if st.InstalledAt == nil {
		if err := e.performInstall(ctx, d); err != nil {
			return fmt.Errorf("auto-install on activate: %w", err)
		}
	}
	if err := e.start(ctx, d.ID); err != nil {
		return fmt.Errorf("activate start: %w", err)
	}
	if err := e.callHook(ctx, d, d.Lifecycle.Activate); err != nil {
		_ = e.stop(ctx, d.ID)
		return fmt.Errorf("activate hook: %w", err)
	}
	if err := e.repo.SetActive(ctx, d.ID, true); err != nil {
		_ = e.stop(ctx, d.ID)
		return err
	}
	return nil
}
```

Remove the old `Install` and `Activate` bodies (they are replaced entirely above). The rest of `engine.go` (`Deactivate`, `Update`, `Uninstall`, helpers) is unchanged.

- [ ] **Step 3: Run and verify**

```bash
cd server && go test ./internal/pluginlifecycle/
```

All tests must pass, including the three new ones and all pre-existing ones (`TestEngine_InstallActivateDeactivateUninstall`, `TestEngine_ActivateBeforeInstallRejected`, `TestEngine_HookFailureAbortsTransition`, `TestActivateStartsBeforeHookThenSetsActive`, `TestActivateHookFailureStopsAndDoesNotActivate`, `TestInstallWrapsHooksInTransient`).

Note that `TestEngine_ActivateBeforeInstallRejected` now tests a path that no longer exists in `Activate` — the test name is misleading but it will still pass because the engine now auto-installs rather than erroring. **Rename** that test to `TestEngine_ActivateOnDiscoveredPlugin_AutoInstalls` and update its assertions to confirm success (not an error), matching the new behaviour:

```go
func TestEngine_ActivateOnDiscoveredPlugin_AutoInstalls(t *testing.T) {
	pr := &fakePluginRepo{}
	e := New(pr, &recordingHooks{}, &fakeClearer{}, nil)
	err := e.Activate(context.Background(), desc())
	require.NoError(t, err)
	assert.NotNil(t, pr.installedAt)
	assert.True(t, pr.active)
}
```

Re-run: `cd server && go test ./internal/pluginlifecycle/` → all PASS.

- [ ] **Step 4: Commit**

```bash
git add server/internal/pluginlifecycle/engine.go \
        server/internal/pluginlifecycle/engine_test.go
git commit --no-gpg-sign -m "fix: auto-install discovered plugin on activate instead of returning 409"
```

---

## Fix 3 — Permissions panel: blank project, wrong count, broken Reset

### Task F3-A: Composable contract test

**Files**
- Create: `src/composables/usePermissionPresets.test.ts`

The component currently reads `p.cwd` and `p.count` but the API returns `projectCwd` and `entries[]`. Before fixing the component, write tests that lock in the correct composable contract so a regression is immediately visible.

- [ ] **Step 1: Write `usePermissionPresets.test.ts`**

```ts
import { afterEach, expect, it, vi } from 'vitest'
import { usePermissionPresets } from './usePermissionPresets'

afterEach(() => {
  vi.restoreAllMocks()
})

it('load populates presets with projectCwd and entries array', async () => {
  const payload = [
    {
      projectCwd: '/home/user/my-project',
      entries: [
        { tool: 'Bash', pattern: null },
        { tool: 'Read', pattern: '/src/**' },
      ],
    },
  ]
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(
    new Response(JSON.stringify(payload), { status: 200 }),
  )
  const { presets, load } = usePermissionPresets()
  await load()
  expect(presets.value).toHaveLength(1)
  expect(presets.value[0].projectCwd).toBe('/home/user/my-project')
  expect(presets.value[0].entries).toHaveLength(2)
  expect(presets.value[0].entries[0].tool).toBe('Bash')
  expect(presets.value[0].entries[1].pattern).toBe('/src/**')
})

it('revoke sends DELETE to /api/settings/permission-presets with cwd in body', async () => {
  const fetchMock = vi.spyOn(globalThis, 'fetch')
    .mockResolvedValue(new Response(JSON.stringify([]), { status: 200 }))
  const { revoke } = usePermissionPresets()
  await revoke('/home/user/my-project')
  const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
  expect(url).toBe('/api/settings/permission-presets')
  expect(init.method).toBe('DELETE')
  expect(JSON.parse(init.body as string)).toEqual({ cwd: '/home/user/my-project' })
})

it('revoke throws when the server responds with an error', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(
    new Response('Bad Request', { status: 400 }),
  )
  const { revoke } = usePermissionPresets()
  await expect(revoke('/home/user/my-project')).rejects.toThrow()
})
```

Run: `pnpm test src/composables/usePermissionPresets.test.ts` → should PASS (the composable is already correct; the test locks its API shape).

- [ ] **Step 2: Commit the test**

```bash
git add src/composables/usePermissionPresets.test.ts
git commit --no-gpg-sign -m "test: lock usePermissionPresets API contract (projectCwd + entries shape)"
```

### Task F3-B: Fix the Permissions panel in ApiKeySettings.vue

**Files**
- Modify: `src/components/ApiKeySettings.vue`

- [ ] **Step 1: Replace the broken local state + fetch with the composable**

In the `<script setup>` block, make the following targeted changes:

**a) Add the composable import** (alongside the other composable imports near the top of script setup):

```ts
import { usePermissionPresets } from '../composables/usePermissionPresets'
```

**b) Replace the local `presets` ref and the preset-related functions.** Remove lines 71–74 (the drifted local type and refs):

```ts
// REMOVE these four lines:
const presets = ref<{ cwd: string, count: number }[]>([])
const presetsLoading = ref(false)
const presetsError = ref<string | null>(null)
const confirmResetCwd = ref<string | null>(null)
```

Replace with:

```ts
const { presets, load: loadPresetsData, revoke: revokePreset } = usePermissionPresets()
const presetsLoading = ref(false)
const presetsError = ref<string | null>(null)
const confirmResetCwd = ref<string | null>(null)
```

**c) Replace the `loadPresets` function** (lines 108–123 in the original):

```ts
async function loadPresets() {
  presetsLoading.value = true
  presetsError.value = null
  try {
    await loadPresetsData()
  }
  catch (e) {
    presetsError.value = errorMessage(e, 'Failed to load')
  }
  finally {
    presetsLoading.value = false
  }
}
```

**d) Replace the `resetPresets` function** (lines 125–140 in the original):

```ts
async function resetPresets(projectCwd: string) {
  try {
    await revokePreset(projectCwd)
    confirmResetCwd.value = null
  }
  catch (e) {
    presetsError.value = errorMessage(e, 'Failed to reset')
  }
}
```

**e) Add a `basename` helper** (after `resetPresets`):

```ts
function basename(path: string): string {
  return path.split('/').filter(Boolean).pop() ?? path
}
```

- [ ] **Step 2: Fix the Permissions panel template**

Locate the `v-for="p in presets"` table body (currently around line 823). Apply the following changes:

**Row key** — change `:key="p.cwd"` to `:key="p.projectCwd"`.

**Project cell** — change:

```html
<td class="px-3 py-2.5 border-b border-line text-fg font-mono text-xs break-all">
  {{ p.cwd }}
</td>
```

to:

```html
<td class="px-3 py-2.5 border-b border-line text-fg font-mono text-xs" :title="p.projectCwd">
  {{ basename(p.projectCwd) }}
</td>
```

**Count cell** — change:

```html
{{ p.count }} {{ p.count === 1 ? 'Tool' : 'Tools' }}
```

to:

```html
{{ p.entries.length }} {{ p.entries.length === 1 ? 'Tool' : 'Tools' }}
```

**Confirm-reset guard** — change `confirmResetCwd === p.cwd` to `confirmResetCwd === p.projectCwd`.

**Confirm Yes button** — change `@click="resetPresets(p.cwd)"` to `@click="resetPresets(p.projectCwd)"`.

**Reset trigger button** — change `@click="confirmResetCwd = p.cwd"` to `@click="confirmResetCwd = p.projectCwd"`.

- [ ] **Step 3: Verify**

```bash
pnpm typecheck
pnpm lint
pnpm test src/composables/usePermissionPresets.test.ts
```

All must pass with zero errors/warnings.

- [ ] **Step 4: Commit**

```bash
git add src/components/ApiKeySettings.vue
git commit --no-gpg-sign -m "fix: permissions panel uses correct DTO fields (projectCwd, entries) and composable revoke"
```

---

## Final verify

Run all of the following before opening the PR:

```bash
cd server && go build ./...
cd server && go test ./internal/settings/ ./internal/pluginlifecycle/
pnpm test
pnpm typecheck
pnpm lint
```

All must be green.
