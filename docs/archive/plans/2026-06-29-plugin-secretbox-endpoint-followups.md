# Plugin Follow-ups: Secretbox Key Path + Endpoint Consolidation

**Goal:** Two independent bug-fixes shipped together in one PR on branch `feat/plugin-followups`.
**Architecture:** Go 1.26 backend (chi, ent ORM, cobra), Vue 3 TypeScript SPA (Vite, pnpm).
**Tech Stack:** `go test` (scoped per package, never `./...`), vitest (`pnpm test`).

---

## Context

### Item 1 — secretbox master-key path

**Finding:** `server/internal/secretbox/secretbox.go:LoadOrGenerateMasterKey` already reads `CLAUDE_CONFIG_DIR` (line 80) and the existing test `TestLoadOrGenerateMasterKey_GeneratesPersistsAndReuses` already covers generate → persist → reuse under a `CLAUDE_CONFIG_DIR` override. On this machine `~/.claude` is a symlink to `~/.claude-personal`, so both paths resolve to the same inode — no live breakage today.

**Gap:** When `CLAUDE_CONFIG_DIR` points to a directory that has no key file, and the legacy `~/.claude/dashboard-secret.key` exists (e.g. key was generated before the env var was set, on a machine where `~/.claude` is not a symlink), the current code silently generates a fresh key and persists it at the new path. All previously encrypted plugin settings become unreadable.

**Back-compat behavior to implement:** After getting `ErrNotExist` on the primary path, check `~/.claude/<keyfile>` only when that path differs (string comparison) from the primary base. If found and valid, return it with a `slog.Warn` log line. Do not write the key to the new path (user must do that migration explicitly). If the legacy path is also absent, proceed to generate.

### Item 2 — `/api/settings/plugins` ↔ `/api/plugins` consolidation

**Finding:**
- `GET /api/settings/plugins` — served by `api/plugins.Handler`, JWT-auth (any logged-in user), returns `{ id, capabilities, enabled, healthy, authProvider }`. Used via `src/utils/plugins.ts:fetchPluginList()` by `usePlugins` and `usePluginSlots`.
- `GET /api/plugins` — served by `api/plugins.LifecycleHandler`, **admin-only** auth, returns `{ id, name, version, state, updateAvailable, healthy, capabilities, hasSettings }`.

`fetchPluginList()` only reads `id` and `capabilities` from the response; both fields are present in `PluginView` (the `/api/plugins` DTO). The consolidation is safe **only if** `GET /api/plugins` is moved out of the admin group into the JWT-only group first, otherwise `usePluginSlots` (called for all authenticated users) would receive 403.

`PluginSettings.vue` uses `usePluginSettings` → `/api/plugins` (already fine for admin context). It also embeds `<PluginSlot>` which calls `loadSlotAddons` → `fetchPluginList` → currently `/api/settings/plugins`. After the change `<PluginSlot>` will call `/api/plugins`.

**Conclusion:** Safe to fully remove `/api/settings/plugins`. Requires splitting `LifecycleHandler.Mount` into `MountList` (JWT group) + remaining `Mount` (admin group).

---

## Task Plan

### Item 1 — Legacy key fallback in secretbox (1 TDD task)

#### Task 1.1 — Legacy-path fallback: test → impl → commit

**Files:**
- `server/internal/secretbox/secretbox_test.go`
- `server/internal/secretbox/secretbox.go`

**Step 1 — Write the failing test (add to `secretbox_test.go`):**

```go
func TestLoadOrGenerateMasterKey_LegacyFallback(t *testing.T) {
	// Redirect HOME so UserHomeDir() resolves to a controlled temp directory.
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create a valid key at the legacy ~/.claude path.
	legacyDir := filepath.Join(home, ".claude")
	require.NoError(t, os.MkdirAll(legacyDir, 0o700))
	const legacyKeyHex = "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	wantKey, err := hex.DecodeString(legacyKeyHex)
	require.NoError(t, err)
	legacyPath := filepath.Join(legacyDir, secretKeyFileName)
	require.NoError(t, os.WriteFile(legacyPath, []byte(legacyKeyHex+"\n"), 0o600))

	// CLAUDE_CONFIG_DIR points to a different, empty directory.
	newDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", newDir)

	got, err := LoadOrGenerateMasterKey("")
	require.NoError(t, err)
	require.Equal(t, wantKey, got, "must return legacy ~/.claude key, not generate a new one")

	// No new key file must appear at the configured path.
	_, statErr := os.Stat(filepath.Join(newDir, secretKeyFileName))
	require.True(t, os.IsNotExist(statErr), "must not generate a new key when legacy key exists")
}
```

Note: the test needs `"encoding/hex"` added to the import block of `secretbox_test.go`.

**Step 2 — Run; observe failure:**
```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server
go test ./internal/secretbox/...
```
Fails: `got != wantKey` (a freshly generated 32-byte key is returned instead).

**Step 3 — Implement fallback in `LoadOrGenerateMasterKey` (`secretbox.go`):**

Replace the block starting at `} else if !errors.Is(err, os.ErrNotExist) {` with:

```go
} else if !errors.Is(err, os.ErrNotExist) {
	return nil, fmt.Errorf("secretbox: read key file %s: %w", path, err)
} else {
	// Primary path has no key. Check the legacy ~/.claude path when it differs
	// from baseDir (covers machines where CLAUDE_CONFIG_DIR is set but the key
	// was originally generated under the ~/.claude default).
	home, herr := os.UserHomeDir()
	if herr == nil {
		legacyBase := filepath.Join(home, ".claude")
		if legacyBase != baseDir {
			legacyPath := filepath.Join(legacyBase, secretKeyFileName)
			if raw, lerr := os.ReadFile(legacyPath); lerr == nil {
				trimmed := strings.TrimSpace(string(raw))
				if trimmed != "" {
					key, derr := hex.DecodeString(trimmed)
					if derr == nil && len(key) == 32 {
						slog.Warn("plugin secret key loaded from legacy path; consider migrating it to CLAUDE_CONFIG_DIR",
							"legacy", legacyPath, "configured", path)
						return key, nil
					}
				}
			}
		}
	}
	// Neither path has a key — generate and persist at the primary path.
}
```

Remove the original `// empty file — fall through to generate` comment block and collapse the ErrNotExist branch into the else above.

**Step 4 — Run; observe pass:**
```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server
go test ./internal/secretbox/...
```

**Step 5 — Build check:**
```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && go build ./...
```

**Step 6 — Commit:**
```
git commit --no-gpg-sign -m "fix(secretbox): fall back to legacy ~/.claude key when CLAUDE_CONFIG_DIR path has none"
```

---

### Item 2 — Consolidate plugin list endpoint (3 TDD tasks)

#### Task 2.1 — Split GET /api/plugins out of admin gate

**Files:**
- `server/internal/api/plugins/handler.go`
- `server/internal/api/plugins/handler_test.go`
- `server/internal/api/router.go`

**Step 1 — Write failing test in `handler_test.go`:**

```go
// TestLifecycleList_NonAdminJWTAllowed verifies that GET /api/plugins is
// accessible to any authenticated user, not only admins.
func TestLifecycleList_NonAdminJWTAllowed(t *testing.T) {
	ctl := &fakeLifecycle{views: []plugins.PluginView{}}
	h := plugins.NewLifecycle(ctl)

	r := chi.NewRouter()
	r.Use(auth.RequireAuth(testJWTSecret))
	// Simulate the split mount: list on JWT group, rest on admin group.
	h.MountList(r)

	nonAdminToken, err := auth.SignJWT(auth.JWTPayload{Sub: "u2", Login: "reader", IsAdmin: false}, testJWTSecret, 3600)
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/plugins", nil)
	req.AddCookie(&http.Cookie{Name: "auth_token", Value: nonAdminToken})
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for non-admin list, got %d: %s", rr.Code, rr.Body.String())
	}
}
```

**Step 2 — Run; observe compile error** (method `MountList` does not exist yet):
```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server
go test ./internal/api/plugins/...
```

**Step 3 — Add `MountList` to `LifecycleHandler` and remove the GET route from `Mount`:**

In `handler.go`, replace `Mount`:
```go
// MountList registers GET /api/plugins for any authenticated user.
// Mount the write endpoints (transitions, settings) separately under an admin gate.
func (h *LifecycleHandler) MountList(r chi.Router) {
	r.Get("/api/plugins", apierr.ErrorMiddleware(h.list))
}

// Mount registers the lifecycle write endpoints under r. Callers must apply
// RequireAdminOrBypass before calling Mount.
func (h *LifecycleHandler) Mount(r chi.Router) {
	r.Post("/api/plugins/{id}/{action}", apierr.ErrorMiddleware(h.transition))
	r.Get("/api/plugins/{id}/settings", apierr.ErrorMiddleware(h.getSettings))
	r.Put("/api/plugins/{id}/settings", apierr.ErrorMiddleware(h.putSettings))
}
```

**Step 4 — Update `router.go` to mount the list in the JWT group:**

Locate the block that currently mounts `PluginLifecycleHandler`:
```go
if deps.PluginLifecycleHandler != nil {
    r.Group(func(r chi.Router) {
        r.Use(authpkg.RequireAdminOrBypass(deps.Config.BypassAuth))
        deps.PluginLifecycleHandler.Mount(r)
    })
}
```

Replace with:
```go
if deps.PluginLifecycleHandler != nil {
    // List is read-only and needed by non-admin users for slot discovery.
    deps.PluginLifecycleHandler.MountList(r)
    r.Group(func(r chi.Router) {
        r.Use(authpkg.RequireAdminOrBypass(deps.Config.BypassAuth))
        deps.PluginLifecycleHandler.Mount(r)
    })
}
```

**Step 5 — Also update the existing `TestLifecycleList_ShapeAndLeakGuard` test** in `handler_test.go` to call `mountLifecycle` via `MountList` so it still compiles. Change `mountLifecycle` helper to use `MountList`:

```go
func mountLifecycle(t *testing.T, ctl *fakeLifecycle) http.Handler {
	t.Helper()
	h := plugins.NewLifecycle(ctl)
	r := chi.NewRouter()
	r.Use(auth.RequireAuth(testJWTSecret))
	h.MountList(r)
	r.Group(func(r chi.Router) {
		h.Mount(r)
	})
	return r
}
```

**Step 6 — Run; observe pass:**
```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server
go test ./internal/api/plugins/...
```

**Step 7 — Build check:**
```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && go build ./...
```

**Step 8 — Commit:**
```
git commit --no-gpg-sign -m "refactor(plugins): split GET /api/plugins list out of admin gate via MountList"
```

---

#### Task 2.2 — Remove legacy `/api/settings/plugins` handler (Go side)

**Files:**
- `server/internal/api/plugins/handler.go`
- `server/internal/api/plugins/handler_test.go`
- `server/internal/api/router.go`
- `server/cmd/serve/di.go`

**Step 1 — Write failing test to prove the legacy route must return 404:**

Add to `handler_test.go`:
```go
// TestLegacySettingsPluginsRouteGone asserts that /api/settings/plugins no
// longer exists. When Handler is removed this test becomes a compile-time proof
// (the type is gone); until then the test is the TDD anchor.
func TestLegacySettingsPluginsRouteGone(t *testing.T) {
	r := chi.NewRouter()
	// Mount only the lifecycle handler — no Handler.Mount() call.
	h := plugins.NewLifecycle(&fakeLifecycle{})
	h.MountList(r)
	h.Mount(r)

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/settings/plugins", nil))
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for removed route /api/settings/plugins, got %d", rr.Code)
	}
}
```

**Step 2 — Run; this test already passes** (the test-local router above doesn't mount Handler). The failing test is the *compile-time* one: after deleting `Handler`, any call to `plugins.New()` in production code will fail compilation. Use this test as documentation; the next steps are the real gate.

**Step 3 — Delete legacy Handler code from `handler.go`:**

Remove:
- `Controller` interface (lines 19-22)
- `Handler` struct (lines 24-26)
- `New` function (lines 29-31)
- `Handler.Mount` method (lines 33-38)
- `pluginInfo` DTO (lines 40-49)
- `Handler.list` method (lines 51-73)
- The `pluginsctl` import line (used only by `Controller` interface and `Handler`).

Keep `pluginsctl` in the import only if `classify()` still references `pluginsctl.ErrUnknownPlugin` — it does, so that import must stay. Remove only the line that imports `pluginsctl` if it was separate; keep it for the `classify` function.

After deletion the file contains only `PluginView`, `controller`, `lifecycleActions`, `LifecycleHandler`, `NewLifecycle`, `MountList`, `Mount`, `classify`, and the four lifecycle methods.

**Step 4 — Remove `PluginsHandler` from `RouterDeps` in `router.go`:**

Remove the field:
```go
PluginsHandler         *apiplugins.Handler
```

Remove the mount block:
```go
if deps.PluginsHandler != nil {
    deps.PluginsHandler.Mount(r)
}
```

**Step 5 — Clean up `di.go`:**

Remove lines:
```go
pluginsHandler := apiplugins.New(pluginsctl.New(pluginRegistry, pluginRepo, cfg.PluginDir))
```
and:
```go
PluginsHandler:         pluginsHandler,
```

Remove the `pluginsctl` import if it is now unused in `di.go` (it will be — `pluginsctl.New` was the only call site there).

**Step 6 — Remove now-dead tests from `handler_test.go`:**

Delete:
- `stubController` (lines 25-29)
- `fakeController` (lines 32-36)
- `mount` helper (lines 49-56)
- `TestList_ShapeAndLeakGuard` (lines 58-115)
- `TestPluginsEnabledPatchRouteRemoved` (lines 117-131)

**Step 7 — Run; observe pass:**
```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server
go test ./internal/api/plugins/... ./internal/api/...
```

**Step 8 — Build check:**
```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && go build ./...
```

**Step 9 — Commit:**
```
git commit --no-gpg-sign -m "feat(plugins): remove legacy /api/settings/plugins endpoint and pluginsctl handler"
```

---

#### Task 2.3 — Update frontend `fetchPluginList` to call `/api/plugins`

**Files:**
- `src/utils/plugins.ts`
- `src/composables/__tests__/usePlugins.test.ts`
- `src/composables/usePluginSlots.ts` (comment update only)
- `src/components/PluginSettings.test.ts`

**Step 1 — Write failing vitest test in `src/composables/__tests__/usePlugins.test.ts`:**

Add a URL-verification assertion at the end of the existing `'fetches plugins on mount'` test:
```ts
it('fetches plugins on mount', async () => {
  const { result } = withSetup(() => usePlugins())
  await vi.waitUntil(() => !result.loading.value)

  expect(result.plugins.value).toHaveLength(1)
  expect(result.plugins.value[0].id).toBe('my-auth-plugin')
  // Must call the lifecycle list endpoint, not the legacy settings endpoint.
  expect(vi.mocked(globalThis.fetch)).toHaveBeenCalledWith(
    '/api/plugins',
    expect.objectContaining({ credentials: 'same-origin' }),
  )
})
```

**Step 2 — Run; observe failure:**
```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
pnpm test src/composables/__tests__/usePlugins.test.ts
```
Fails: `fetch` was called with `/api/settings/plugins`.

**Step 3 — Update `src/utils/plugins.ts`:**

```ts
// Single source for the plugin list — both usePlugins and usePluginSlots use this.
export interface PluginInfo {
  id: string
  capabilities: string[]
}

export async function fetchPluginList(): Promise<PluginInfo[]> {
  const res = await fetch('/api/plugins', { credentials: 'same-origin' })
  if (!res.ok)
    throw new Error(`Failed to load plugins (HTTP ${res.status}: ${res.statusText})`)
  const data = await res.json()
  return Array.isArray(data) ? data : []
}
```

**Step 4 — Update `src/composables/usePluginSlots.ts` comment (line 100):**

Change:
```ts
 * Security: only plugins enumerated by `/api/settings/plugins` (registry-discovered,
```
to:
```ts
 * Security: only plugins enumerated by `/api/plugins` (registry-discovered,
```

**Step 5 — Update `src/components/PluginSettings.test.ts`:**

The stub currently handles `/api/settings/plugins` separately (line 16-17) and returns an empty list. After the change, both `usePluginSettings` and `usePluginSlots` call `/api/plugins`. Update `stubFetch` to serve both use cases from the single route:

```ts
function stubFetch(pluginList: object[]) {
  vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) => {
    if (url === '/api/plugins')
      return Promise.resolve({ ok: true, json: async () => pluginList })
    if (String(url).includes('/deactivate') || String(url).includes('/activate'))
      return Promise.resolve({ ok: true, json: async () => ({}) })
    return Promise.resolve({ ok: true, json: async () => [] })
  }))
}
```

Note: `usePluginSettings` fetches `/api/plugins` for the initial list and `usePluginSlots` (via `loadSlotAddons`) now also fetches `/api/plugins`. Both will receive `pluginList` from the stub, which is the same data. The test pluginList items already carry `capabilities` (e.g. `['ui_extension']`), so slot detection continues to work.

**Step 6 — Run; observe pass:**
```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
pnpm test src/composables/__tests__/usePlugins.test.ts
pnpm test src/components/PluginSettings.test.ts
pnpm test src/composables/usePluginSlots.test.ts
```

**Step 7 — Full frontend verify:**
```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
pnpm typecheck && pnpm lint
```

**Step 8 — Commit:**
```
git commit --no-gpg-sign -m "refactor(plugins): fetchPluginList migrates to /api/plugins, removes legacy settings endpoint caller"
```

---

## Final Verification

```
# Go: all touched packages (avoid go test ./... — corrupts ent)
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server
go build ./...
go test ./internal/secretbox/... ./internal/api/plugins/... ./internal/api/...

# Frontend
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
pnpm test && pnpm typecheck && pnpm lint
```

Go worktree note: LSP may report "undefined" or "internal not allowed" for packages in the worktree — these are false positives. Trust `go build` and `go test` output only.

---

## Commit Order Summary

| # | Commit message | Files touched |
|---|---|---|
| 1 | `fix(secretbox): fall back to legacy ~/.claude key when CLAUDE_CONFIG_DIR path has none` | `secretbox.go`, `secretbox_test.go` |
| 2 | `refactor(plugins): split GET /api/plugins list out of admin gate via MountList` | `handler.go`, `handler_test.go`, `router.go` |
| 3 | `feat(plugins): remove legacy /api/settings/plugins endpoint and pluginsctl handler` | `handler.go`, `handler_test.go`, `router.go`, `di.go` |
| 4 | `refactor(plugins): fetchPluginList migrates to /api/plugins, removes legacy settings endpoint caller` | `plugins.ts`, `usePluginSlots.ts`, `usePlugins.test.ts`, `PluginSettings.test.ts` |

All commits: `git commit --no-gpg-sign` (SSH signing hangs in this env).
