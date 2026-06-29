# Plugin SP8 — Security Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close four security findings from the post-integration audit: admin-gate the restart endpoint, validate plugin IDs uniformly in the lifecycle handler and discovery, blocklist dashboard secret env names from the plugin spawn environment, and fill two spec-mandated test gaps (secretbox key persistence and CLI enable-unknown error).

**Architecture:** All changes are backend-only with no ent migration and no frontend changes. `plugin.ValidID` is a new tiny exported helper wrapping the existing `pluginIDRe` in `internal/plugin` — it becomes the single ID-validation SSOT consumed by the lifecycle handler and discovery. `buildPluginEnv` in `registry.go` gains a `dashboardSecretEnv` blocklist map that prevents forwarding dashboard secrets even when a plugin's `desc.Env` allow-list names them. The restart endpoint is wrapped in `RequireAdminOrBypass` inside a `r.Group` closure in `router.go`, matching exactly the pattern already used for `SystemPromptsHandler` (lines 337–340) and the spawners group (lines 302–306).

**Tech Stack:** Go 1.26 backend, chi router, ent ORM, go test

---

## Gotcha: ent regeneration

`go test ./...` regenerates the entire `server/internal/db/ent/` tree and can corrupt it. Prefer per-package `go test` throughout this plan. If `./...` was run, restore:
```bash
git checkout -- server/internal/db/ent/
```

---

## Task 1: Export `plugin.ValidID` (prerequisite for Tasks 2 and 3)

**Files:**
- `server/internal/plugin/validate.go` — new file
- `server/internal/plugin/validate_test.go` — new file, `package plugin_test`

`pluginIDRe` lives at `registry.go:23` (unexported). `ValidID` wraps it, exports it, and becomes the project-wide SSOT for plugin ID validation. The registry's own `Load` and `StartOne` already use the raw regex; after this task the handler and discovery will use `ValidID` instead of reinventing the pattern.

- [ ] **Step 1 — failing test.** Create `server/internal/plugin/validate_test.go`:
  ```go
  package plugin_test

  import (
  	"testing"

  	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
  )

  func TestValidID(t *testing.T) {
  	valid := []string{"my-plugin", "abc", "a1", "a-b-c-1", "a0"}
  	for _, id := range valid {
  		if !plugin.ValidID(id) {
  			t.Errorf("expected ValidID(%q) = true", id)
  		}
  	}
  	invalid := []string{"", "My-Plugin", "UPPER", "../traversal", "-bad", "has space", "under_score"}
  	for _, id := range invalid {
  		if plugin.ValidID(id) {
  			t.Errorf("expected ValidID(%q) = false", id)
  		}
  	}
  }
  ```

  Run:
  ```bash
  cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && \
    go test ./internal/plugin/ 2>&1 | head -5
  ```
  Expected: `./internal/plugin/validate_test.go:8:16: undefined: plugin.ValidID`

- [ ] **Step 2 — minimal implementation.** Create `server/internal/plugin/validate.go`:
  ```go
  package plugin

  // ValidID reports whether id is a well-formed plugin identifier.
  // Uses pluginIDRe as the single source of truth for the validation pattern.
  func ValidID(id string) bool {
  	return pluginIDRe.MatchString(id)
  }
  ```

- [ ] **Step 3 — passing test:**
  ```bash
  cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && \
    go test ./internal/plugin/ -run TestValidID -v 2>&1 | tail -5
  ```
  Expected: `--- PASS: TestValidID` and `PASS`

- [ ] **Step 4 — commit:**
  ```bash
  cd /Users/alexanderwink/code/_privat/projects/agent-dashboard && \
    git add server/internal/plugin/validate.go server/internal/plugin/validate_test.go && \
    git commit --no-gpg-sign -m "feat: export plugin.ValidID as single regex SSOT for plugin ID validation"
  ```

---

## Task 2: Apply `plugin.ValidID` in the lifecycle handler

**Files:**
- `server/internal/api/plugins/handler.go` — add `ValidID` guard in `transition` (after line 158), `getSettings` (after line 180), `putSettings` (after line 198)
- `server/internal/api/plugins/handler_test.go` — append three malformed-ID → 400 tests

The handler already imports `"github.com/lx-wnk/agent-dashboard/server/internal/plugin"` (line 13) for `plugin.SettingField`, so no import change is needed.

- [ ] **Step 1 — failing tests.** Append to `server/internal/api/plugins/handler_test.go`:
  ```go
  func TestLifecycleTransition_MalformedID_400(t *testing.T) {
  	ctl := &fakeLifecycle{}
  	req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/plugins/My-PLUGIN/activate", nil))
  	rr := httptest.NewRecorder()
  	mountLifecycle(t, ctl).ServeHTTP(rr, req)

  	if rr.Code != http.StatusBadRequest {
  		t.Fatalf("expected 400 for malformed id, got %d: %s", rr.Code, rr.Body.String())
  	}
  	if ctl.gotID != "" {
  		t.Errorf("malformed id must not reach controller, got %q", ctl.gotID)
  	}
  }

  func TestLifecycleGetSettings_MalformedID_400(t *testing.T) {
  	ctl := &fakeLifecycle{}
  	req := withAuth(t, httptest.NewRequest(http.MethodGet, "/api/plugins/UPPER/settings", nil))
  	rr := httptest.NewRecorder()
  	mountLifecycle(t, ctl).ServeHTTP(rr, req)

  	if rr.Code != http.StatusBadRequest {
  		t.Fatalf("expected 400 for malformed id, got %d: %s", rr.Code, rr.Body.String())
  	}
  }

  func TestLifecyclePutSettings_MalformedID_400(t *testing.T) {
  	ctl := &fakeLifecycle{}
  	body := `{"values":{}}`
  	req := withAuth(t, httptest.NewRequest(http.MethodPut, "/api/plugins/BAD_ID/settings", strings.NewReader(body)))
  	rr := httptest.NewRecorder()
  	mountLifecycle(t, ctl).ServeHTTP(rr, req)

  	if rr.Code != http.StatusBadRequest {
  		t.Fatalf("expected 400 for malformed id, got %d: %s", rr.Code, rr.Body.String())
  	}
  }
  ```

  Run:
  ```bash
  cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && \
    go test ./internal/api/plugins/ \
      -run "TestLifecycleTransition_MalformedID_400|TestLifecycleGetSettings_MalformedID_400|TestLifecyclePutSettings_MalformedID_400" \
      -v 2>&1 | tail -15
  ```
  Expected: three FAIL lines — `expected 400 for malformed id, got 200`

- [ ] **Step 2 — implementation.** In `server/internal/api/plugins/handler.go`, add the guard after the `id == ""` check in each of the three lifecycle methods.

  In `transition` — after line 158 (`return fmt.Errorf("%w: plugin id is required"...)`):
  ```go
  if !plugin.ValidID(id) {
  	return fmt.Errorf("%w: invalid plugin id %q", apierr.ErrBadRequest, id)
  }
  ```

  In `getSettings` — after line 180 (the same `id == ""` return):
  ```go
  if !plugin.ValidID(id) {
  	return fmt.Errorf("%w: invalid plugin id %q", apierr.ErrBadRequest, id)
  }
  ```

  In `putSettings` — after line 198 (the same `id == ""` return):
  ```go
  if !plugin.ValidID(id) {
  	return fmt.Errorf("%w: invalid plugin id %q", apierr.ErrBadRequest, id)
  }
  ```

- [ ] **Step 3 — passing tests:**
  ```bash
  cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && \
    go test ./internal/api/plugins/ -v 2>&1 | tail -15
  ```
  Expected: all tests PASS, including the three new ones.

- [ ] **Step 4 — commit:**
  ```bash
  cd /Users/alexanderwink/code/_privat/projects/agent-dashboard && \
    git add server/internal/api/plugins/handler.go server/internal/api/plugins/handler_test.go && \
    git commit --no-gpg-sign -m "fix: reject malformed plugin IDs with 400 in lifecycle handler"
  ```

---

## Task 3: Apply `plugin.ValidID` in discovery

**Files:**
- `server/internal/pluginlifecycle/discovery.go` — add ValidID guard after line 69 (the `desc.ID == ""` check); also add `"log/slog"` to imports
- `server/internal/pluginlifecycle/discovery_test.go` — append one test (file is `package pluginlifecycle`, internal)

`discovery.go` already imports `"github.com/lx-wnk/agent-dashboard/server/internal/plugin"` (line 13).

- [ ] **Step 1 — failing test.** Append to `server/internal/pluginlifecycle/discovery_test.go`:
  ```go
  func TestDiscover_SkipsMalformedID(t *testing.T) {
  	dir := t.TempDir()
  	writeManifest(t, dir, "valid-plugin", "1.0.0")
  	// Malformed ID: uppercase letters fail pluginIDRe.
  	require.NoError(t, os.MkdirAll(filepath.Join(dir, "Bad-Plugin"), 0o755))
  	badBody := `{"id":"Bad-Plugin","version":"1.0.0","capabilities":["route_extension"],"addr":"127.0.0.1:1"}`
  	require.NoError(t, os.WriteFile(filepath.Join(dir, "Bad-Plugin", "plugin.json"), []byte(badBody), 0o644))

  	repo := &memDiscoverRepo{rows: map[string]DiscoverRow{}}
  	res, err := NewDiscoverer(dir, repo).Discover(context.Background())
  	require.NoError(t, err)

  	assert.Equal(t, 1, res.Found, "only the valid plugin should be counted")
  	assert.Contains(t, repo.rows, "valid-plugin")
  	assert.NotContains(t, repo.rows, "Bad-Plugin")
  }
  ```

  Run:
  ```bash
  cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && \
    go test ./internal/pluginlifecycle/ -run TestDiscover_SkipsMalformedID -v 2>&1 | tail -5
  ```
  Expected: FAIL — `assert.Equal: 1 != 2` or `"Bad-Plugin" found in repo.rows`

- [ ] **Step 2 — implementation.** In `server/internal/pluginlifecycle/discovery.go`:

  1. Add `"log/slog"` to the import block.

  2. After line 69 (the existing `if err := json.Unmarshal(raw, &desc); err != nil || desc.ID == ""` check), insert:
  ```go
  if !plugin.ValidID(desc.ID) {
  	slog.Warn("discover: skip — plugin id is malformed", "dir", e.Name(), "id", desc.ID)
  	continue
  }
  ```

- [ ] **Step 3 — passing test:**
  ```bash
  cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && \
    go test ./internal/pluginlifecycle/ -v 2>&1 | tail -10
  ```
  Expected: all tests PASS including `TestDiscover_SkipsMalformedID`.

- [ ] **Step 4 — commit:**
  ```bash
  cd /Users/alexanderwink/code/_privat/projects/agent-dashboard && \
    git add server/internal/pluginlifecycle/discovery.go server/internal/pluginlifecycle/discovery_test.go && \
    git commit --no-gpg-sign -m "fix: skip manifests with malformed plugin IDs during discovery"
  ```

---

## Task 4: Admin-gate the `/api/admin/restart` endpoint

**Files:**
- `server/internal/api/router.go` — replace lines 453–456 (unguarded `AdminHandler.Mount`) with the guarded group pattern
- `server/internal/api/admin/handler_auth_test.go` — new file, `package admin_test`, documents the security contract

`RequireAdminOrBypass(bypassAuth bool) func(http.Handler) http.Handler` is defined at `server/internal/auth/middleware.go:85`. The exact guard pattern to use mirrors the `SystemPromptsHandler` block at `router.go:337–340`.

Current code at `router.go:453–456`:
```go
if deps.AdminHandler != nil {
    deps.AdminHandler.Mount(r)
}
```

Target:
```go
if deps.AdminHandler != nil {
    r.Group(func(r chi.Router) {
        r.Use(authpkg.RequireAdminOrBypass(deps.Config.BypassAuth))
        deps.AdminHandler.Mount(r)
    })
}
```

- [ ] **Step 1 — security contract test.** Create `server/internal/api/admin/handler_auth_test.go`:
  ```go
  package admin_test

  import (
  	"net/http"
  	"net/http/httptest"
  	"testing"

  	"github.com/go-chi/chi/v5"
  	"github.com/stretchr/testify/require"

  	"github.com/lx-wnk/agent-dashboard/server/internal/api/admin"
  	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
  )

  const testAdminJWTSecret = "test-sp8-admin-secret"

  // TestRestart_NonAdminUserGets403 verifies the security property that the restart
  // endpoint must enforce: non-admin authenticated users receive 403. The router
  // wraps AdminHandler.Mount with RequireAdminOrBypass (same pattern as spawners and
  // system-prompts). This test builds that exact chi group to document the contract.
  func TestRestart_NonAdminUserGets403(t *testing.T) {
  	h := admin.New(fakeValidator{}, "reexec", func() { t.Fatal("must not trigger") })

  	r := chi.NewRouter()
  	r.Use(auth.RequireAuth(testAdminJWTSecret))
  	r.Group(func(r chi.Router) {
  		r.Use(auth.RequireAdminOrBypass(false)) // not bypass mode
  		h.Mount(r)
  	})

  	token, err := auth.SignJWT(
  		auth.JWTPayload{Sub: "u2", Login: "viewer", IsAdmin: false},
  		testAdminJWTSecret, 3600,
  	)
  	require.NoError(t, err)
  	req := httptest.NewRequest(http.MethodPost, "/api/admin/restart", nil)
  	req.AddCookie(&http.Cookie{Name: "auth_token", Value: token})

  	rec := httptest.NewRecorder()
  	r.ServeHTTP(rec, req)
  	require.Equal(t, http.StatusForbidden, rec.Code)
  }

  func TestRestart_BypassModeAllowsAnyRequest(t *testing.T) {
  	triggered := make(chan struct{}, 1)
  	h := admin.New(fakeValidator{}, "reexec", func() { triggered <- struct{}{} })

  	r := chi.NewRouter()
  	r.Group(func(r chi.Router) {
  		r.Use(auth.RequireAdminOrBypass(true)) // bypass = local single-user mode
  		h.Mount(r)
  	})

  	rec := httptest.NewRecorder()
  	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/admin/restart", nil))
  	require.Equal(t, http.StatusAccepted, rec.Code)
  	select {
  	case <-triggered:
  	default:
  		t.Fatal("trigger must fire in bypass mode")
  	}
  }
  ```

  Note: `fakeValidator` is already defined in `handler_test.go` within the same `admin_test` package — no redefinition needed.

  Run:
  ```bash
  cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && \
    go test ./internal/api/admin/ \
      -run "TestRestart_NonAdminUserGets403|TestRestart_BypassModeAllowsAnyRequest" \
      -v 2>&1 | tail -10
  ```
  Expected: both PASS (the middleware already works correctly; these tests document the contract the router must enforce).

- [ ] **Step 2 — router fix.** In `server/internal/api/router.go`, replace lines 453–456 as described above.

- [ ] **Step 3 — verify build:**
  ```bash
  cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && go build ./... 2>&1
  ```
  Expected: no output.

- [ ] **Step 4 — commit:**
  ```bash
  cd /Users/alexanderwink/code/_privat/projects/agent-dashboard && \
    git add server/internal/api/router.go server/internal/api/admin/handler_auth_test.go && \
    git commit --no-gpg-sign -m "fix: require admin authorization for POST /api/admin/restart"
  ```

---

## Task 5: Blocklist dashboard secret env vars in `buildPluginEnv`

**Files:**
- `server/internal/plugin/registry.go` — add `dashboardSecretEnv` package-level var above `buildPluginEnv` (around line 666); change the allow-list population loop inside `buildPluginEnv` (lines 672–676)
- `server/internal/plugin/buildenv_test.go` — new file, `package plugin` (internal — accesses unexported `buildPluginEnv` directly)

The test file must be `package plugin` (not `plugin_test`) because `buildPluginEnv` is unexported.

- [ ] **Step 1 — failing tests.** Create `server/internal/plugin/buildenv_test.go`:
  ```go
  package plugin

  import (
  	"strings"
  	"testing"
  )

  func TestBuildPluginEnv_BlocklistWinsOverAllowList(t *testing.T) {
  	t.Setenv("MY_PLUGIN_KEY", "hello")
  	t.Setenv("DASHBOARD_SECRET_KEY", "should-never-appear")
  	t.Setenv("DASHBOARD_JWT_SECRET", "also-blocked")

  	env := buildPluginEnv([]string{"MY_PLUGIN_KEY", "DASHBOARD_SECRET_KEY", "DASHBOARD_JWT_SECRET"})

  	byKey := make(map[string]string, len(env))
  	for _, kv := range env {
  		if idx := strings.Index(kv, "="); idx > 0 {
  			byKey[kv[:idx]] = kv[idx+1:]
  		}
  	}

  	if byKey["MY_PLUGIN_KEY"] != "hello" {
  		t.Errorf("expected MY_PLUGIN_KEY=hello in env, got %q", byKey["MY_PLUGIN_KEY"])
  	}
  	if _, found := byKey["DASHBOARD_SECRET_KEY"]; found {
  		t.Error("DASHBOARD_SECRET_KEY must not be forwarded even when allow-listed")
  	}
  	if _, found := byKey["DASHBOARD_JWT_SECRET"]; found {
  		t.Error("DASHBOARD_JWT_SECRET must not be forwarded even when allow-listed")
  	}
  }

  func TestBuildPluginEnv_AllBlocklistNamesAreBlocked(t *testing.T) {
  	blocked := []string{
  		"DASHBOARD_SECRET_KEY",
  		"DASHBOARD_JWT_SECRET",
  		"DASHBOARD_AUTH_PLUGIN_SECRET",
  		"DASHBOARD_MCP_TOKEN",
  		"DASHBOARD_HOOKS_SECRET",
  	}
  	for _, k := range blocked {
  		t.Setenv(k, "secret-value")
  	}

  	// Pass all blocked names as the allow-list — blocklist must still win.
  	env := buildPluginEnv(blocked)

  	byKey := make(map[string]string, len(env))
  	for _, kv := range env {
  		if idx := strings.Index(kv, "="); idx > 0 {
  			byKey[kv[:idx]] = kv[idx+1:]
  		}
  	}
  	for _, k := range blocked {
  		if _, found := byKey[k]; found {
  			t.Errorf("%s must not appear in plugin env", k)
  		}
  	}
  }
  ```

  Run:
  ```bash
  cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && \
    go test ./internal/plugin/ -run "TestBuildPluginEnv" -v 2>&1 | tail -10
  ```
  Expected: FAIL — both secrets appear in the env (no blocklist exists yet).

- [ ] **Step 2 — implementation.** In `server/internal/plugin/registry.go`:

  1. Add the blocklist var directly above the `buildPluginEnv` function (around line 666):
  ```go
  // dashboardSecretEnv names env vars that carry dashboard secrets.
  // These are never forwarded to plugins even if listed in desc.Env.
  var dashboardSecretEnv = map[string]bool{
  	"DASHBOARD_SECRET_KEY":         true,
  	"DASHBOARD_JWT_SECRET":         true,
  	"DASHBOARD_AUTH_PLUGIN_SECRET": true,
  	"DASHBOARD_MCP_TOKEN":          true,
  	"DASHBOARD_HOOKS_SECRET":       true,
  }
  ```

  2. Inside `buildPluginEnv`, change the allow-list loop from:
  ```go
  for _, k := range allowedKeys {
  	allowed[k] = true
  }
  ```
  To:
  ```go
  for _, k := range allowedKeys {
  	if !dashboardSecretEnv[k] { // blocklist wins over allow-list
  		allowed[k] = true
  	}
  }
  ```

- [ ] **Step 3 — passing tests:**
  ```bash
  cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && \
    go test ./internal/plugin/ -v 2>&1 | grep -E "PASS|FAIL|ok" | tail -15
  ```
  Expected: all lines are PASS and the final line is `ok`.

- [ ] **Step 4 — commit:**
  ```bash
  cd /Users/alexanderwink/code/_privat/projects/agent-dashboard && \
    git add server/internal/plugin/registry.go server/internal/plugin/buildenv_test.go && \
    git commit --no-gpg-sign -m "fix: blocklist dashboard secret env names from plugin spawn environment"
  ```

---

## Task 6 (test gap): Secretbox key generate → persist at 0600 → reuse

**Files:**
- `server/internal/secretbox/secretbox_test.go` — append one test (file is `package secretbox`, internal)

`LoadOrGenerateMasterKey` (lines 71–117) reads `CLAUDE_CONFIG_DIR` for the key file path. `t.Setenv` controls that path without touching `~/.claude`. The `secretKeyFileName` constant (`"dashboard-secret.key"`, line 19) is accessible because the test is in the same package.

- [ ] **Step 1 — test (expected to pass immediately).** Append to `server/internal/secretbox/secretbox_test.go`:
  ```go
  func TestLoadOrGenerateMasterKey_GeneratesPersistsAndReuses(t *testing.T) {
  	dir := t.TempDir()
  	t.Setenv("CLAUDE_CONFIG_DIR", dir)

  	// First call: no key file exists — must generate and persist.
  	key1, err := LoadOrGenerateMasterKey("")
  	require.NoError(t, err)
  	require.Len(t, key1, 32, "master key must be 32 bytes")

  	// Key file must be persisted with owner-only permissions.
  	keyPath := filepath.Join(dir, secretKeyFileName)
  	info, err := os.Stat(keyPath)
  	require.NoError(t, err)
  	require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "key file must be 0600")

  	// Second call: must read and return the same key (idempotent bootstrap).
  	key2, err := LoadOrGenerateMasterKey("")
  	require.NoError(t, err)
  	require.Equal(t, key1, key2, "second call must return the same persisted key")
  }
  ```

  Run:
  ```bash
  cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && \
    go test ./internal/secretbox/ -run TestLoadOrGenerateMasterKey_GeneratesPersistsAndReuses -v 2>&1 | tail -5
  ```
  Expected: `--- PASS: TestLoadOrGenerateMasterKey_GeneratesPersistsAndReuses`. If it fails, the implementation has a bug — investigate before proceeding.

- [ ] **Step 2 — commit:**
  ```bash
  cd /Users/alexanderwink/code/_privat/projects/agent-dashboard && \
    git add server/internal/secretbox/secretbox_test.go && \
    git commit --no-gpg-sign -m "test: add secretbox key generate-persist-reuse happy path"
  ```

---

## Task 7 (test gap): CLI `enable <unknown-id>` returns non-zero

**Files:**
- `server/cmd/cli/cmd_plugins_test.go` — append one test mirroring `TestPluginsDisableUnknownErrors` (lines 55–62)

`setPluginActive` already errors on unknown id via `repo.IsNotFound` (cmd_plugins.go:51–54). This test closes the coverage gap for the `enable` path.

- [ ] **Step 1 — test (expected to pass immediately).** Append to `server/cmd/cli/cmd_plugins_test.go`:
  ```go
  func TestPluginsEnableUnknownErrors(t *testing.T) {
  	dbPath := t.TempDir() + "/test.db"
  	seedPlugin(t, dbPath, "other", false) // ensures DB is initialised

  	cmd := newPluginsCmd()
  	cmd.SetArgs([]string{"enable", "nope", "--db", dbPath})
  	require.Error(t, cmd.Execute())
  }
  ```

  Run:
  ```bash
  cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && \
    go test ./cmd/cli/ -run TestPluginsEnableUnknownErrors -v 2>&1 | tail -5
  ```
  Expected: `--- PASS: TestPluginsEnableUnknownErrors`. If it fails, investigate before proceeding.

- [ ] **Step 2 — commit:**
  ```bash
  cd /Users/alexanderwink/code/_privat/projects/agent-dashboard && \
    git add server/cmd/cli/cmd_plugins_test.go && \
    git commit --no-gpg-sign -m "test: add CLI enable-unknown-plugin error coverage"
  ```

---

## Final verification

- [ ] **Full build clean:**
  ```bash
  cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && go build ./... 2>&1
  ```
  Expected: no output.

- [ ] **All touched packages pass:**
  ```bash
  cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && \
    go test \
      ./internal/plugin/ \
      ./internal/api/plugins/ \
      ./internal/api/admin/ \
      ./internal/pluginlifecycle/ \
      ./internal/secretbox/ \
      ./cmd/cli/ \
      2>&1 | grep -E "^ok|^FAIL|^---"
  ```
  Expected: all lines start with `ok`.

- [ ] **Restore ent if accidentally regenerated:**
  ```bash
  git checkout -- server/internal/db/ent/
  ```
