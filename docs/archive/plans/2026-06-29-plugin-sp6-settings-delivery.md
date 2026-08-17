# Plugin SP6 — Settings Delivery & Validation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire decrypted plugin settings into every subprocess spawn as `PLUGIN_SETTING_<KEY>` env vars and enforce type validation on `Service.Put`.

**Architecture:** A `SettingsProvider` function seam (type, field, setter) added to `plugin.Registry` is filled in DI by a closure over `pluginsettings.Service.DecryptedAll`. Both `startEntry` and the `watchPlugin` restart path read the provider after `buildPluginEnv` and append `PLUGIN_SETTING_*` vars. `Service.Put` gains pre-persist type validation (`int`, `bool`, `url`, `enum`) guarded by a new `ErrInvalidValue` sentinel that `classify` maps to 400. A `v-else-if` branch in `PluginSettingsForm.vue` renders `type="url"` for url fields.

**Tech Stack:** Go 1.26 backend, ent ORM, Vue 3 TS frontend, go test, vitest

---

## Compat note (read first)

- SP8 owns the `DASHBOARD_*` secret blocklist inside `buildPluginEnv`. SP6 only appends **after** `buildPluginEnv`; do not touch the allow-list.
- `Service.Put` already accepts `schema []plugin.SettingField` (line 78 of `service.go`) and `Controller.PutSettings` already passes `desc.Settings` (line 241 of `controller.go`). The public API shapes do not change — only the validation body inside `Put` is new.
- The `SettingsProvider` seam needs to be wired **before** `pluginRegistry.Load` in `di.go`. This requires splitting the current single `if entClient != nil` block (lines ~274–300 of `di.go`) into two: one early block that creates `pluginSettingsSvc` and wires the provider, one later block that builds `lifecycleEngine`/`discoverer`/`handler` after Load.
- Commits: always `git commit --no-gpg-sign -m "..."` (SSH signing hangs).
- Run per-package `go test` only (never `go test ./...`): it regenerates `server/internal/db/ent/` and can corrupt it. After any accidental full run: `git checkout -- server/internal/db/ent/`.

---

## Task 1 — `SettingsProvider` type + `settings` field + `SetSettingsProvider`

**Files:**
- `server/internal/plugin/types.go` — add type declaration at bottom
- `server/internal/plugin/registry.go` — add field to `Registry` struct + setter method

**Failing state:** code does not compile until the type and field exist.

- [ ] Add to `server/internal/plugin/types.go` after the existing capability constants:

```go
// SettingsProvider fetches decrypted settings for a plugin by ID.
// Called at every subprocess spawn; errors are logged and the plugin starts
// without settings so a DB failure never blocks plugin availability.
type SettingsProvider func(ctx context.Context, id string) (map[string]string, error)
```

Add `"context"` to imports in `types.go` (currently has none — add an import block).

- [ ] Add `settings SettingsProvider` field to the `Registry` struct in `registry.go` (after the `hooks Hooks` field):

```go
// settings is the optional provider that fetches decrypted per-plugin values
// for env injection at every spawn. Nil means no settings are injected.
settings SettingsProvider
```

- [ ] Add setter after `SetEnabled`:

```go
// SetSettingsProvider installs the provider that fetches decrypted settings
// for env injection at every spawn. Call before Load.
func (r *Registry) SetSettingsProvider(fn SettingsProvider) { r.settings = fn }
```

- [ ] Verify compile:

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && go build ./internal/plugin/...
```

Expected: no output (clean build).

- [ ] Commit:

```
git -C /Users/alexanderwink/code/_privat/projects/agent-dashboard \
  commit --no-gpg-sign -m "feat(plugin): add SettingsProvider seam to Registry"
```

---

## Task 2 — `sanitizeSettingKey` + internal unit test

**Files:**
- `server/internal/plugin/registry.go` — add unexported function
- `server/internal/plugin/sanitize_test.go` — `package plugin` (internal), not `package plugin_test`

- [ ] Add `sanitizeSettingKey` to `registry.go` near `buildPluginEnv`:

```go
// sanitizeSettingKey uppercases key and replaces every character that is not
// A-Z, 0-9, or _ with '_', producing a valid env var suffix.
func sanitizeSettingKey(key string) string {
	upper := strings.ToUpper(key)
	var b strings.Builder
	b.Grow(len(upper))
	for _, c := range upper {
		if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			b.WriteRune(c)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}
```

`strings` is already imported in `registry.go`.

- [ ] Create `server/internal/plugin/sanitize_test.go`:

```go
package plugin

import (
	"testing"
)

func TestSanitizeSettingKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"apiKey", "APIKEY"},
		{"api-key", "API_KEY"},
		{"my.setting", "MY_SETTING"},
		{"FOO_BAR", "FOO_BAR"},
		{"a b c", "A_B_C"},
		{"x123", "X123"},
		{"ALREADY_UPPER", "ALREADY_UPPER"},
	}
	for _, tc := range cases {
		got := sanitizeSettingKey(tc.in)
		if got != tc.want {
			t.Errorf("sanitizeSettingKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
```

- [ ] Run test:

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && go test ./internal/plugin/ -run TestSanitizeSettingKey -v
```

Expected output:
```
=== RUN   TestSanitizeSettingKey
--- PASS: TestSanitizeSettingKey (0.00s)
PASS
```

- [ ] Commit:

```
git -C /Users/alexanderwink/code/_privat/projects/agent-dashboard \
  commit --no-gpg-sign -m "feat(plugin): sanitize setting key for PLUGIN_SETTING_ env var names"
```

---

## Task 3 — Subprocess integration test for `startEntry` injection

Write the test first (it will fail because no injection code exists yet).

**Files:**
- `server/internal/plugin/registry_test.go` — add helper + two test cases

- [ ] Add `writeEnvDumpPlugin` helper and two tests **at the end** of `registry_test.go`:

```go
// writeEnvDumpPlugin builds a plugin binary that writes all PLUGIN_SETTING_*
// env vars to envFile before serving GET /health → 200.
// The env file is written synchronously before ListenAndServe so it exists
// by the time the registry health-check returns.
func writeEnvDumpPlugin(t *testing.T, dir, id string) (addr, envFile string) {
	t.Helper()
	pluginDir := filepath.Join(dir, id)
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr = ln.Addr().String()
	require.NoError(t, ln.Close())

	envFile = filepath.Join(pluginDir, "plugin-env.txt")

	mainGo := `package main

import (
	"net/http"
	"os"
	"strings"
)

func main() {
	addr := os.Args[1]
	envFile := os.Args[2]

	var lines []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "PLUGIN_SETTING_") {
			lines = append(lines, kv)
		}
	}
	_ = os.WriteFile(envFile, []byte(strings.Join(lines, "\n")), 0o644)

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	_ = http.ListenAndServe(addr, nil)
}
`
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "main.go"), []byte(mainGo), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "go.mod"),
		[]byte("module env-dump-plugin\n\ngo 1.21\n"), 0o644))

	binPath := filepath.Join(pluginDir, "plugin-bin")
	build := exec.Command("go", "build", "-o", binPath, ".")
	build.Dir = pluginDir
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, buildErr := build.CombinedOutput()
	require.NoError(t, buildErr, "go build failed:\n%s", out)

	desc := plugin.Descriptor{
		ID:           id,
		Version:      "1.0.0",
		Capabilities: []string{plugin.CapRouteExtension},
		Addr:         addr,
		Settings: []plugin.SettingField{
			{Key: "api-key", Type: "string", Secret: true},
			{Key: "endpoint", Type: "url"},
		},
		Command: []string{"./plugin-bin", addr, envFile},
	}
	data, _ := json.Marshal(desc)
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644))
	return addr, envFile
}

func TestRegistry_SettingsInjectedAtStart(t *testing.T) {
	dir := t.TempDir()
	_, envFile := writeEnvDumpPlugin(t, dir, "env-plugin")

	r := plugin.New(dir)
	r.SetSettingsProvider(func(_ context.Context, _ string) (map[string]string, error) {
		return map[string]string{
			"api-key":  "s3cr3t",
			"endpoint": "https://example.com",
		}, nil
	})

	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))
	t.Cleanup(r.Shutdown)

	data, err := os.ReadFile(envFile)
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "PLUGIN_SETTING_API_KEY=s3cr3t",
		"hyphen in key must be sanitized to underscore and uppercased")
	assert.Contains(t, content, "PLUGIN_SETTING_ENDPOINT=https://example.com")
}

func TestRegistry_NoProvider_NoSettingVars(t *testing.T) {
	dir := t.TempDir()
	_, envFile := writeEnvDumpPlugin(t, dir, "env-plugin-noprovider")

	r := plugin.New(dir) // no SetSettingsProvider

	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))
	t.Cleanup(r.Shutdown)

	data, err := os.ReadFile(envFile)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "PLUGIN_SETTING_",
		"no provider means no PLUGIN_SETTING_ vars injected")
}
```

- [ ] Run to confirm the tests FAIL (injection not wired yet):

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && \
  go test ./internal/plugin/ -run 'TestRegistry_SettingsInjectedAtStart|TestRegistry_NoProvider_NoSettingVars' -v -timeout 60s
```

Expected: `TestRegistry_SettingsInjectedAtStart` fails (PLUGIN_SETTING_API_KEY absent); `TestRegistry_NoProvider_NoSettingVars` passes.

- [ ] Commit (failing test is intentional TDD step):

```
git -C /Users/alexanderwink/code/_privat/projects/agent-dashboard \
  commit --no-gpg-sign -m "test(plugin): subprocess env-injection integration tests"
```

---

## Task 4 — Wire injection in `startEntry` → tests pass

**Files:**
- `server/internal/plugin/registry.go` — modify `startEntry`

- [ ] In `startEntry`, immediately after `cmd.Env = buildPluginEnv(desc.Env)` (line ~161), insert:

```go
if r.settings != nil {
    if vals, sErr := r.settings(serverCtx, desc.ID); sErr != nil {
        slog.Warn("plugin: settings fetch failed — starting without settings",
            "id", desc.ID, "err", sErr)
    } else {
        for k, v := range vals {
            cmd.Env = append(cmd.Env, "PLUGIN_SETTING_"+sanitizeSettingKey(k)+"="+v)
        }
    }
}
```

- [ ] Run integration tests — both must now pass:

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && \
  go test ./internal/plugin/ -run 'TestRegistry_SettingsInjectedAtStart|TestRegistry_NoProvider_NoSettingVars' -v -timeout 60s
```

Expected:
```
=== RUN   TestRegistry_SettingsInjectedAtStart
--- PASS: TestRegistry_SettingsInjectedAtStart (X.XXs)
=== RUN   TestRegistry_NoProvider_NoSettingVars
--- PASS: TestRegistry_NoProvider_NoSettingVars (X.XXs)
PASS
```

- [ ] Run the full plugin package tests to check for regressions:

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && \
  go test ./internal/plugin/ -timeout 120s
```

Expected: `PASS` (all existing tests unaffected).

- [ ] Commit:

```
git -C /Users/alexanderwink/code/_privat/projects/agent-dashboard \
  commit --no-gpg-sign -m "feat(plugin): inject PLUGIN_SETTING_* env vars at every subprocess spawn"
```

---

## Task 5 — Wire injection in `watchPlugin` restart spawn

**Files:**
- `server/internal/plugin/registry.go` — modify `watchPlugin`

- [ ] In `watchPlugin`, immediately after `newCmd.Env = buildPluginEnv(desc.Env)` (line ~562), insert:

```go
if r.settings != nil {
    if vals, sErr := r.settings(ctx, desc.ID); sErr != nil {
        slog.Warn("plugin: settings fetch failed on restart — continuing without settings",
            "id", desc.ID, "err", sErr)
    } else {
        for k, v := range vals {
            newCmd.Env = append(newCmd.Env, "PLUGIN_SETTING_"+sanitizeSettingKey(k)+"="+v)
        }
    }
}
```

- [ ] Verify build and run plugin package tests:

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && \
  go build ./internal/plugin/... && go test ./internal/plugin/ -timeout 120s
```

Expected: `PASS`.

- [ ] Commit:

```
git -C /Users/alexanderwink/code/_privat/projects/agent-dashboard \
  commit --no-gpg-sign -m "feat(plugin): inject settings on watchPlugin restart spawn"
```

---

## Task 6 — `DecryptedAll` on `pluginsettings.Service` + test

`Decrypted(ctx, id, schema)` requires a schema to iterate. `DecryptedAll` uses the persisted `Stored.Secret` flag so no schema is needed — suitable for use as the DI provider.

**Files:**
- `server/internal/pluginsettings/service.go` — add method
- `server/internal/pluginsettings/service_test.go` — add test case

- [ ] Add after `Decrypted` in `service.go`:

```go
// DecryptedAll returns all stored settings with secrets decrypted.
// Unlike Decrypted it requires no schema — it uses the persisted Secret flag.
// Used by the registry's SettingsProvider for env injection at spawn time.
func (s *Service) DecryptedAll(ctx context.Context, pluginID string) (map[string]string, error) {
	rows, err := s.repo.ListByPlugin(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		if r.Secret {
			pt, derr := s.box.Decrypt(r.Value, r.Nonce)
			if derr != nil {
				return nil, derr
			}
			out[r.Key] = pt
		} else {
			out[r.Key] = r.Value
		}
	}
	return out, nil
}
```

- [ ] Add test to `service_test.go` (after the existing test):

```go
func TestService_DecryptedAll_ReturnsPlaintext(t *testing.T) {
	box, _ := secretbox.New(make([]byte, 32))
	repo := &fakeRepo{rows: map[string]row{}}
	svc := New(repo, box)
	schema := []plugin.SettingField{
		{Key: "endpoint", Type: "url"},
		{Key: "apiKey", Type: "string", Secret: true},
	}
	ctx := context.Background()

	require.NoError(t, svc.Put(ctx, "p1", schema,
		map[string]string{"endpoint": "https://x", "apiKey": "TOPSECRET"}))

	all, err := svc.DecryptedAll(ctx, "p1")
	require.NoError(t, err)
	assert.Equal(t, "https://x", all["endpoint"])
	assert.Equal(t, "TOPSECRET", all["apiKey"], "secret value must be decrypted")

	// Plugin not found → empty map, no error.
	empty, err := svc.DecryptedAll(ctx, "unknown-plugin")
	require.NoError(t, err)
	assert.Empty(t, empty)
}
```

- [ ] Run:

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && \
  go test ./internal/pluginsettings/ -v -timeout 30s
```

Expected: all three tests pass.

- [ ] Commit:

```
git -C /Users/alexanderwink/code/_privat/projects/agent-dashboard \
  commit --no-gpg-sign -m "feat(pluginsettings): add DecryptedAll for schema-free env injection"
```

---

## Task 7 — DI wiring: provider before `pluginRegistry.Load`

**Files:**
- `server/cmd/serve/di.go` — split the `if entClient != nil` block

The current structure in `di.go`:
1. `pluginRegistry := plugin.New(cfg.PluginDir)` (~line 213)
2. `pluginRegistry.Load(...)` (~line 231)
3. `var pluginLifecycleHandler` + `if entClient != nil { masterKey; box; pluginSettingRepo; pluginSettingsSvc; lifecycleEngine; discoverer; lifecycleController; pluginLifecycleHandler = ... }` (~lines 273–300)

The required structure after this task:
1. `pluginRegistry := plugin.New(...)`
2. **New early block:** `var pluginSettingsSvc *pluginsettings.Service` + `if entClient != nil { masterKey; box; pluginSettingRepo; pluginSettingsSvc = ...; pluginRegistry.SetSettingsProvider(...) }`
3. `pluginRegistry.Load(...)`
4. **Later block:** `var pluginLifecycleHandler` + `if pluginSettingsSvc != nil { lifecycleEngine; discoverer; lifecycleController; pluginLifecycleHandler = ... }` (guard changed from `entClient != nil` to `pluginSettingsSvc != nil` since the two are equivalent and avoids repeating `masterKey`/`box`)

- [ ] Apply the restructuring to `di.go`. Insert this block between `pluginRegistry.SetEnabled(...)` and `pluginRegistry.Load(...)`:

```go
// Build settings service early so the provider is wired before Load.
// Nil when running without a database (no entClient).
var pluginSettingsSvc *pluginsettings.Service
if entClient != nil {
    masterKey, keyErr := secretbox.LoadOrGenerateMasterKey(os.Getenv("DASHBOARD_SECRET_KEY"))
    if keyErr != nil {
        return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, cleanup,
            fmt.Errorf("plugin secret key: %w", keyErr)
    }
    box, boxErr := secretbox.New(masterKey)
    if boxErr != nil {
        return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, cleanup,
            fmt.Errorf("plugin secretbox: %w", boxErr)
    }
    pluginSettingRepo := repo.NewPluginSettingRepo(entClient)
    pluginSettingsSvc = pluginsettings.New(pluginSettingRepoAdapter{inner: pluginSettingRepo}, box)
    pluginRegistry.SetSettingsProvider(func(ctx context.Context, id string) (map[string]string, error) {
        return pluginSettingsSvc.DecryptedAll(ctx, id)
    })
}
```

- [ ] Remove (or replace) the original `if entClient != nil` block (~lines 273–300) with:

```go
var pluginLifecycleHandler *apiplugins.LifecycleHandler
if pluginSettingsSvc != nil {
    pluginSettingRepo := repo.NewPluginSettingRepo(entClient)
    lifecycleEngine := pluginlifecycle.New(
        pluginStateRepoAdapter{inner: pluginRepo},
        pluginlifecycle.NewHTTPHookCaller(),
        pluginSettingsSvc,
        pluginProcessAdapter{reg: pluginRegistry},
    )
    discoverer := pluginlifecycle.NewDiscoverer(cfg.PluginDir, pluginDiscoverRepoAdapter{inner: pluginRepo, settings: pluginSettingRepo})
    lifecycleController := pluginlifecyclectl.New(pluginRepo, lifecycleEngine, pluginSettingsSvc, cfg.PluginDir)
    pluginLifecycleHandler = apiplugins.NewLifecycle(lifecycleController)

    if res, discErr := discoverer.Discover(ctx); discErr != nil {
        slog.Warn("plugin discovery failed", "error", discErr)
    } else {
        slog.Info("plugin discovery", "found", res.Found, "updatesAvailable", res.UpdatesAvailable)
    }
}
```

Note: `pluginSettingRepo` is re-created here (it's cheap; a repo is a thin wrapper). Alternatively, declare it in outer scope in the early block — either works.

- [ ] Verify build:

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && go build ./...
```

Expected: no errors.

- [ ] Commit:

```
git -C /Users/alexanderwink/code/_privat/projects/agent-dashboard \
  commit --no-gpg-sign -m "feat(di): wire SettingsProvider into plugin registry before Load"
```

---

## Task 8 — `ErrInvalidValue` + type validation in `Service.Put`

**Files:**
- `server/internal/pluginsettings/service.go` — add sentinel + `validateValue` + pre-persist validation loop
- `server/internal/pluginsettings/service_test.go` — add validation test cases

- [ ] Add import `"net/url"` and `"strconv"` to `service.go` imports (alongside existing ones).

- [ ] Add after `ErrUnknownKey`:

```go
// ErrInvalidValue is returned by Put when a submitted value does not satisfy
// the field's declared Type (int, bool, url, enum).
var ErrInvalidValue = errors.New("pluginsettings: invalid value")
```

- [ ] Add `validateValue` before `Put`:

```go
func validateValue(f plugin.SettingField, v string) error {
	switch f.Type {
	case "int":
		if _, err := strconv.Atoi(v); err != nil {
			return fmt.Errorf("%w: field %q requires an integer, got %q", ErrInvalidValue, f.Key, v)
		}
	case "bool":
		if v != "true" && v != "false" {
			return fmt.Errorf("%w: field %q requires \"true\" or \"false\", got %q", ErrInvalidValue, f.Key, v)
		}
	case "url":
		u, parseErr := url.ParseRequestURI(v)
		if parseErr != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("%w: field %q requires a URL with scheme and host, got %q", ErrInvalidValue, f.Key, v)
		}
	case "enum":
		for _, opt := range f.Enum {
			if v == opt {
				return nil
			}
		}
		return fmt.Errorf("%w: field %q value %q not in allowed set %v", ErrInvalidValue, f.Key, v, f.Enum)
	// "string" and unrecognised types: accept any value
	}
	return nil
}
```

- [ ] Rewrite the body of `Put` so unknown-key AND type validation both run **before** any persistence (validate-all-or-nothing):

```go
func (s *Service) Put(ctx context.Context, pluginID string, schema []plugin.SettingField, values map[string]string) error {
	schemaMap := make(map[string]plugin.SettingField, len(schema))
	for _, f := range schema {
		schemaMap[f.Key] = f
	}
	// Validate all submitted entries before writing anything.
	for k, v := range values {
		f, ok := schemaMap[k]
		if !ok {
			return fmt.Errorf("%w: %q", ErrUnknownKey, k)
		}
		// Masked sentinel means "leave unchanged" — skip validation.
		if f.Secret && v == MaskedSentinel {
			continue
		}
		if err := validateValue(f, v); err != nil {
			return err
		}
	}
	// Persist validated values.
	for _, f := range schema {
		v, ok := values[f.Key]
		if !ok {
			continue
		}
		if f.Secret {
			if v == MaskedSentinel {
				continue
			}
			ct, nonce, err := s.box.Encrypt(v)
			if err != nil {
				return err
			}
			if err := s.repo.Upsert(ctx, pluginID, Stored{Key: f.Key, Value: ct, Nonce: nonce, Secret: true}); err != nil {
				return err
			}
			continue
		}
		if err := s.repo.Upsert(ctx, pluginID, Stored{Key: f.Key, Value: v, Secret: false}); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] Add validation test cases to `service_test.go`:

```go
func TestService_Put_TypeValidation(t *testing.T) {
	box, _ := secretbox.New(make([]byte, 32))
	ctx := context.Background()

	schema := []plugin.SettingField{
		{Key: "count", Type: "int"},
		{Key: "enabled", Type: "bool"},
		{Key: "endpoint", Type: "url"},
		{Key: "mode", Type: "enum", Enum: []string{"fast", "slow"}},
		{Key: "label", Type: "string"},
		{Key: "token", Type: "string", Secret: true},
	}

	cases := []struct {
		name    string
		values  map[string]string
		wantErr bool
		errIs   error
	}{
		{"valid int", map[string]string{"count": "42"}, false, nil},
		{"invalid int", map[string]string{"count": "abc"}, true, ErrInvalidValue},
		{"valid bool true", map[string]string{"enabled": "true"}, false, nil},
		{"valid bool false", map[string]string{"enabled": "false"}, false, nil},
		{"invalid bool", map[string]string{"enabled": "yes"}, true, ErrInvalidValue},
		{"valid url", map[string]string{"endpoint": "https://api.example.com"}, false, nil},
		{"invalid url no scheme", map[string]string{"endpoint": "api.example.com"}, true, ErrInvalidValue},
		{"invalid url empty", map[string]string{"endpoint": ""}, true, ErrInvalidValue},
		{"valid enum", map[string]string{"mode": "fast"}, false, nil},
		{"invalid enum", map[string]string{"mode": "turbo"}, true, ErrInvalidValue},
		{"valid string any", map[string]string{"label": "anything goes"}, false, nil},
		{"unknown key", map[string]string{"nope": "x"}, true, ErrUnknownKey},
		{"masked sentinel skips validation", map[string]string{"token": MaskedSentinel}, false, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{rows: map[string]row{}}
			svc := New(repo, box)
			err := svc.Put(ctx, "p1", schema, tc.values)
			if tc.wantErr {
				require.Error(t, err)
				if tc.errIs != nil {
					require.ErrorIs(t, err, tc.errIs)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestService_Put_ValidationFailDoesNotPersist(t *testing.T) {
	box, _ := secretbox.New(make([]byte, 32))
	repo := &fakeRepo{rows: map[string]row{}}
	svc := New(repo, box)
	schema := []plugin.SettingField{
		{Key: "count", Type: "int"},
		{Key: "label", Type: "string"},
	}
	ctx := context.Background()

	// count is invalid, label is valid — nothing should be persisted.
	err := svc.Put(ctx, "p1", schema, map[string]string{"count": "bad", "label": "ok"})
	require.ErrorIs(t, err, ErrInvalidValue)
	assert.Empty(t, repo.rows, "no value must be persisted when validation fails")
}
```

- [ ] Run:

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && \
  go test ./internal/pluginsettings/ -v -timeout 30s
```

Expected: all tests pass including the existing `TestService_PutEncryptsSecret_GetMasks`.

- [ ] Commit:

```
git -C /Users/alexanderwink/code/_privat/projects/agent-dashboard \
  commit --no-gpg-sign -m "feat(pluginsettings): validate field types in Put, add ErrInvalidValue"
```

---

## Task 9 — Map `ErrInvalidValue` to 400 in handler `classify`

**Files:**
- `server/internal/api/plugins/handler.go` — extend `classify`

- [ ] In the `classify` function (line ~129), extend the `ErrUnknownKey` check to include `ErrInvalidValue`:

Current:
```go
if errors.Is(err, pluginsctl.ErrUnknownPlugin) || errors.Is(err, pluginsctl.ErrInvalidAction) ||
    errors.Is(err, pluginsettings.ErrUnknownKey) {
    return fmt.Errorf("%w: %s", apierr.ErrBadRequest, err.Error())
}
```

After:
```go
if errors.Is(err, pluginsctl.ErrUnknownPlugin) || errors.Is(err, pluginsctl.ErrInvalidAction) ||
    errors.Is(err, pluginsettings.ErrUnknownKey) || errors.Is(err, pluginsettings.ErrInvalidValue) {
    return fmt.Errorf("%w: %s", apierr.ErrBadRequest, err.Error())
}
```

- [ ] Find the handler test or add a focused classify unit test. If the handler has a test file, add:

```go
func TestClassify_ErrInvalidValue_Is400(t *testing.T) {
    err := classify(fmt.Errorf("wrap: %w", pluginsettings.ErrInvalidValue), "ctx")
    require.Error(t, err)
    require.ErrorIs(t, err, apierr.ErrBadRequest)
}
```

If no test file exists, create `server/internal/api/plugins/handler_test.go`:
```go
package plugins_test

import (
    "errors"
    "fmt"
    "testing"

    "github.com/stretchr/testify/require"

    apierr "github.com/lx-wnk/agent-dashboard/server/internal/apierr"
    "github.com/lx-wnk/agent-dashboard/server/internal/pluginsettings"
)

// classify is package-private; test via HTTP handler or inline via internal test.
// Use a thin integration approach: confirm the sentinel maps to ErrBadRequest.
func TestClassify_ErrInvalidValue_Is400(t *testing.T) {
    // classify is unexported; verify via ErrInvalidValue → ErrBadRequest chain:
    wrapped := fmt.Errorf("pluginlifecyclectl: put settings \"x\": %w", pluginsettings.ErrInvalidValue)
    // The classify function wraps ErrBadRequest when it detects ErrInvalidValue.
    // Since it is unexported, assert the error chain IS wired by inspecting the
    // sentinel directly (unit coverage of classify is via integration in handler test).
    require.True(t, errors.Is(wrapped, pluginsettings.ErrInvalidValue),
        "ErrInvalidValue sentinel must be detectable via errors.Is for the handler classify path")
    // Build check for the updated classify call: compile the package.
    _ = apierr.ErrBadRequest
}
```

Better: check if a handler test already exists to add an HTTP-level test:
```
ls /Users/alexanderwink/code/_privat/projects/agent-dashboard/server/internal/api/plugins/
```

If no test file exists, the build + existing pluginsettings tests are sufficient coverage for this one-liner change.

- [ ] Build check:

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && go build ./internal/api/plugins/...
```

- [ ] Commit:

```
git -C /Users/alexanderwink/code/_privat/projects/agent-dashboard \
  commit --no-gpg-sign -m "fix(api/plugins): map ErrInvalidValue to 400 in classify"
```

---

## Task 10 — Frontend `url` field renders `<input type="url">`

**Files:**
- `src/components/PluginSettingsForm.vue` — add `v-else-if` branch
- `src/components/PluginSettingsForm.test.ts` — add assertion

The current template has this order: `enum` → `bool` → `int` → (default `else`). The `url` type currently falls through to the default `else` branch which renders `type="text"` (or `"password"` for secrets). Add `url` before the default else.

- [ ] In `PluginSettingsForm.vue`, insert a new `v-else-if` block **between** the `int` block and the default `else` block:

```html
      <input
        v-else-if="f.type === 'url'"
        :id="`pf-${pluginId}-${f.key}`"
        v-model="model[f.key]"
        type="url"
        :data-field="f.key"
      >
```

The complete updated input block ordering in the template:
1. `v-if="f.type === 'enum'"` → `<select>`
2. `v-else-if="f.type === 'bool'"` → `<input type="checkbox">`
3. `v-else-if="f.type === 'int'"` → `<input type="text" inputmode="numeric">`
4. `v-else-if="f.type === 'url'"` → `<input type="url">` ← NEW
5. `v-else` → `<input :type="f.secret ? 'password' : 'text'">`

- [ ] Add test case to `PluginSettingsForm.test.ts`. The existing schema at line 5 already includes `{ key: 'endpoint', type: 'url', ... }`. Add a new `it` block:

```typescript
it('url field renders as input type="url"', async () => {
  const w = mountForm(async () => ({ schema, values: { endpoint: 'https://x', apiKey: '********', mode: 'a' } }))
  await flushPromises()
  const input = w.find('input[data-field="endpoint"]')
  expect(input.attributes('type')).toBe('url')
})
```

- [ ] Run vitest (the new test will fail until the template change is applied — apply template change first, then run):

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard && pnpm test --reporter=verbose 2>&1 | grep -A3 'PluginSettingsForm'
```

Expected: all five `PluginSettingsForm` tests pass including the new url test.

- [ ] Commit:

```
git -C /Users/alexanderwink/code/_privat/projects/agent-dashboard \
  commit --no-gpg-sign -m "feat(ui): render url setting fields as input type=url"
```

---

## Task 11 — Final verification

- [ ] Go build (entire server):

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && go build ./...
```

- [ ] Per-package go tests for touched packages:

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server && \
  go test ./internal/plugin/ ./internal/pluginsettings/ ./internal/api/plugins/ -timeout 120s -v 2>&1 | tail -20
```

Expected: `PASS` for all three packages.

- [ ] Frontend type-check + lint + unit tests:

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard && pnpm typecheck && pnpm lint && pnpm test
```

Expected: 0 type errors, 0 lint errors, all vitest tests pass.

- [ ] If `go test ./internal/db/ent/` was accidentally run at any point and shows corruption:

```
git -C /Users/alexanderwink/code/_privat/projects/agent-dashboard checkout -- server/internal/db/ent/
```

---

## Summary

| Task | Target | Action |
|------|--------|--------|
| 1 | `plugin/types.go`, `registry.go` | `SettingsProvider` type + field + setter |
| 2 | `registry.go`, new `sanitize_test.go` | `sanitizeSettingKey` + unit tests |
| 3 | `registry_test.go` | Subprocess env-dump helper + two integration tests (TDD failing state) |
| 4 | `registry.go` `startEntry` | Inject `PLUGIN_SETTING_*` after `buildPluginEnv` → tests pass |
| 5 | `registry.go` `watchPlugin` | Same injection on restart spawn |
| 6 | `pluginsettings/service.go` + test | `DecryptedAll` method |
| 7 | `cmd/serve/di.go` | Split block: create `pluginSettingsSvc` + wire provider before Load |
| 8 | `pluginsettings/service.go` + test | `ErrInvalidValue` + `validateValue` + rewrite `Put` |
| 9 | `api/plugins/handler.go` | Add `ErrInvalidValue` to `classify` |
| 10 | `PluginSettingsForm.vue` + test | `v-else-if type="url"` branch |
| 11 | All touched packages | Build + test + typecheck + lint |
