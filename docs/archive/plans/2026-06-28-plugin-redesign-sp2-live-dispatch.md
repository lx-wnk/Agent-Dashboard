# Plugin Redesign SP2 — Live Backend Dispatch + Process Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `route_extension` plugins enable/disable live (zero server restart) via a catch-all reverse-proxy dispatcher, and harden process management (process groups, suppress-restart-on-intentional-stop, observable unhealthy state, transient start for lifecycle hooks).

**Architecture:** A single catch-all route `/api/plugins/{id}/proxy/*` (inside the authed `/api` group) resolves an in-memory registry per request instead of chi mounting one route per plugin at boot (chi #480 freezes routes after serve). The SP1 lifecycle engine gains a `ProcessManager` seam (implemented by an adapter over `*plugin.Registry`) so `Activate`/`Deactivate` start/stop the process and `Install`/`Update`/`Uninstall` transiently start a stopped plugin to deliver its hook. Crash supervision marks a plugin unhealthy in the DB instead of silently vanishing.

**Tech Stack:** Go 1.26, chi v5, `net/http/httputil` reverse proxy, `os/exec` + `syscall` (Setpgid / group-kill), ent (no schema change in SP2), testify.

---

## File Structure

| File | Responsibility | Action |
|---|---|---|
| `server/internal/plugin/registry.go` | `Entry.healthy`/`intentionalStop` fields + locked accessors; `Lookup`; `WithTransient`; Setpgid spawn + group-kill; suppress-restart-on-intentional-stop; mark-unhealthy on give-up | Modify |
| `server/internal/plugin/hooks.go` | `Hooks.OnUnhealthy` callback field | Modify |
| `server/internal/plugin/dispatcher.go` | Catch-all HTTP handler resolving `{id}` → registry → 400/503/reverse-proxy | Create |
| `server/internal/plugin/dispatcher_test.go` | Dispatcher routing tests (fake resolver + httptest plugin) | Create |
| `server/internal/plugin/registry_test.go` | Add tests for Lookup, WithTransient, suppress-restart, mark-unhealthy, group-kill | Modify |
| `server/internal/api/router.go` | Replace boot per-plugin Mount loop with one catch-all dispatcher registration | Modify |
| `server/internal/pluginlifecycle/engine.go` | `ProcessManager` interface; engine transitions drive start/stop/transient (nil-safe); `New` 4th param | Modify |
| `server/internal/pluginlifecycle/engine_test.go` | Fake `ProcessManager` asserting call order + rollback | Modify |
| `server/internal/api/plugins/handler.go` | Remove interim `PATCH /api/settings/plugins-enabled/{id}` + `patch`/`patchResponse`; drop `SetEnabled` from `Controller` | Modify |
| `server/internal/pluginsctl/controller.go` | Remove `SetEnabled`/`Applied`/`persist`/`findDescriptor` (keep `List`/`discover`/`enabledSet`/error sentinels) | Modify |
| `server/internal/pluginsctl/controller_test.go` | Drop `SetEnabled` tests | Modify |
| `server/cmd/serve/di.go` | `pluginProcessAdapter`; inject into engine `New`; wire `Hooks.OnUnhealthy` → `pluginRepo.SetActive(false)` | Modify |
| `README.md`, `CHANGELOG.md`, plugin docs | URL-contract change (`/api/plugins/{id}/proxy/*`), live enable/disable, interim removal | Modify |

**Test/build commands (scope to touched packages — `go test ./...` regenerates ent and can corrupt `server/internal/db/ent/`; SP2 has NO ent change):**
- Tests: `cd server && go test ./internal/plugin/... ./internal/pluginlifecycle/... ./internal/api/plugins/... ./internal/pluginsctl/... -race`
- Build: `cd server && go build ./...`
- Lint: `cd server && golangci-lint run ./internal/plugin/... ./internal/pluginlifecycle/... ./internal/api/... ./internal/pluginsctl/... ./cmd/...` (must be 0)
- Every `git commit` step uses `--no-gpg-sign` (commit signing hangs in this env).

---

### Task 1: Registry `Lookup` + Entry health/stop fields

**Files:**
- Modify: `server/internal/plugin/registry.go:36-49` (Entry struct), add accessors + `Lookup`
- Test: `server/internal/plugin/registry_test.go`

- [ ] **Step 1: Write the failing test**

Add to `server/internal/plugin/registry_test.go`:

```go
func TestLookupReturnsHealthyEntryCopy(t *testing.T) {
	r := plugin.New("")
	require.False(t, exists(r, "missing"))

	// Inject a running, healthy entry through the exported test seam.
	r.InjectEntryForTest(plugin.Descriptor{ID: "voice", Addr: "127.0.0.1:19010"}, true)

	got, ok := r.Lookup("voice")
	require.True(t, ok)
	require.Equal(t, "voice", got.Descriptor.ID)
	require.True(t, got.Healthy())

	_, ok = r.Lookup("nope")
	require.False(t, ok)
}

func exists(r *plugin.Registry, id string) bool {
	_, ok := r.Lookup(id)
	return ok
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/plugin/ -run TestLookupReturnsHealthyEntryCopy -v`
Expected: FAIL — `r.InjectEntryForTest undefined` / `r.Lookup undefined` / `got.Healthy undefined`.

- [ ] **Step 3: Write minimal implementation**

In `server/internal/plugin/registry.go`, add two fields to `Entry` (after `pluginDir`):

```go
	pluginDir string // directory containing plugin.json, needed for restarts

	// healthy is true once the process passed its health check and false once a
	// give-up path (exhausted restarts / failed restart) marks it dead. The
	// dispatcher serves 503 for an unhealthy entry.
	healthy bool
	// intentionalStop is set by StopOne before signalling so the watcher knows
	// the exit was deliberate and must NOT respawn (the real orphan-restart fix).
	intentionalStop bool
```

Add an exported accessor and `Lookup` (place near `All`):

```go
// Healthy reports whether this entry's process is currently considered healthy.
func (e Entry) Healthy() bool { return e.healthy }

// Lookup returns a copy of the entry for id and whether it is present. The copy
// avoids handing out a pointer into the value-backed plugins slice (which
// reallocates on append/remove). Plugin count is single-digit, so the linear
// scan under RLock matches the existing All/FindByCapability patterns.
func (r *Registry) Lookup(id string) (Entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for i := range r.plugins {
		if r.plugins[i].Descriptor.ID == id {
			return r.plugins[i], true
		}
	}
	return Entry{}, false
}

// InjectEntryForTest registers a synthetic entry without starting a process.
// Test-only seam; production code paths go through startEntry.
func (r *Registry) InjectEntryForTest(d Descriptor, healthy bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins = append(r.plugins, Entry{Descriptor: d, BaseURL: "http://" + d.Addr, healthy: healthy})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/plugin/ -run TestLookupReturnsHealthyEntryCopy -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
git add server/internal/plugin/registry.go server/internal/plugin/registry_test.go
git commit --no-gpg-sign -m "feat: add plugin registry lookup with health state

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: Mark entries healthy on successful start

**Files:**
- Modify: `server/internal/plugin/registry.go` — set `healthy=true` after `waitHealthy` in `startEntry`, and after a successful restart in `watchPlugin`
- Test: `server/internal/plugin/registry_test.go`

- [ ] **Step 1: Write the failing test**

Add to `registry_test.go` (this exercises the real spawn path via the existing test fixture pattern — reuse the helper that writes a plugin.json + a fake plugin binary already present in the file; if the file uses `writeFakePlugin(t, dir, id)`, follow it):

```go
func TestStartedPluginIsHealthy(t *testing.T) {
	dir := t.TempDir()
	writeHealthyPlugin(t, dir, "alive") // existing helper in this test file

	r := plugin.New(dir)
	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))
	t.Cleanup(r.Shutdown)

	got, ok := r.Lookup("alive")
	require.True(t, ok)
	require.True(t, got.Healthy())
}
```

> If `writeHealthyPlugin` does not exist, reuse whatever helper the existing passing tests in `registry_test.go` use to stand up a real health-serving plugin (search the file for `/health`). Do NOT invent a new fixture mechanism.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/plugin/ -run TestStartedPluginIsHealthy -v`
Expected: FAIL — `got.Healthy()` is false (field never set).

- [ ] **Step 3: Write minimal implementation**

In `startEntry`, set healthy before appending. Change the append block:

```go
	if entry.cmd != nil {
		done := make(chan struct{})
		entry.cmdDone = done
		go r.watchPlugin(serverCtx, entry.pluginDir, desc, entry.cmd, done)
	}
	entry.healthy = true
	r.mu.Lock()
	r.plugins = append(r.plugins, entry)
	r.mu.Unlock()
```

In `watchPlugin`, after a successful restart, set healthy in the slice update:

```go
		r.mu.Lock()
		for i := range r.plugins {
			if r.plugins[i].Descriptor.ID == desc.ID {
				r.plugins[i].cmd = newCmd
				r.plugins[i].restartCount = restartCount
				r.plugins[i].healthy = true
				break
			}
		}
		r.mu.Unlock()
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/plugin/ -run TestStartedPluginIsHealthy -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/internal/plugin/registry.go server/internal/plugin/registry_test.go
git commit --no-gpg-sign -m "feat: mark plugin entries healthy after successful start

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: Catch-all dispatcher

**Files:**
- Create: `server/internal/plugin/dispatcher.go`
- Create: `server/internal/plugin/dispatcher_test.go`

- [ ] **Step 1: Write the failing test**

Create `server/internal/plugin/dispatcher_test.go`:

```go
package plugin_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
)

// fakeResolver implements plugin.ProxyResolver.
type fakeResolver struct {
	entry plugin.Entry
	ok    bool
}

func (f fakeResolver) Lookup(string) (plugin.Entry, bool) { return f.entry, f.ok }

func mountDispatcher(res plugin.ProxyResolver) http.Handler {
	r := chi.NewRouter()
	r.Handle("/api/plugins/{id}/proxy/*", plugin.NewDispatcher(res))
	return r
}

func TestDispatcherMalformedIDReturns400(t *testing.T) {
	h := mountDispatcher(fakeResolver{ok: false})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/plugins/Bad_ID/proxy/x", nil))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDispatcherUnknownOrStoppedReturns503(t *testing.T) {
	h := mountDispatcher(fakeResolver{ok: false})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/plugins/voice/proxy/x", nil))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestDispatcherUnhealthyReturns503(t *testing.T) {
	res := fakeResolver{ok: true, entry: plugin.Entry{
		Descriptor: plugin.Descriptor{ID: "voice", Addr: "127.0.0.1:1", Capabilities: []string{plugin.CapRouteExtension}},
	}} // healthy defaults false
	h := mountDispatcher(res)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/plugins/voice/proxy/x", nil))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestDispatcherNonProxyableReturns503(t *testing.T) {
	res := fakeResolver{ok: true, entry: plugin.NewHealthyEntryForTest(
		plugin.Descriptor{ID: "authonly", Addr: "127.0.0.1:1", Capabilities: []string{plugin.CapAuthProvider}})}
	h := mountDispatcher(res)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/plugins/authonly/proxy/x", nil))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestDispatcherProxiesHealthyRouteExtension(t *testing.T) {
	var gotPath string
	var gotCookie string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotCookie = r.Header.Get("Cookie")
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()
	addr := strings.TrimPrefix(upstream.URL, "http://")

	res := fakeResolver{ok: true, entry: plugin.NewHealthyEntryForTest(
		plugin.Descriptor{ID: "voice", Addr: addr, Capabilities: []string{plugin.CapRouteExtension}})}
	h := mountDispatcher(res)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/plugins/voice/proxy/hello", nil)
	req.Header.Set("Cookie", "session=secret")
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "ok", rec.Body.String())
	require.Equal(t, "/hello", gotPath, "prefix must be stripped to plugin-relative path")
	require.Empty(t, gotCookie, "Cookie must be stripped before forwarding")
}
```

Also add this exported test seam to `registry.go` (used by the dispatcher tests to build a healthy entry):

```go
// NewHealthyEntryForTest builds a healthy Entry for tests in other packages.
func NewHealthyEntryForTest(d Descriptor) Entry {
	return Entry{Descriptor: d, BaseURL: "http://" + d.Addr, healthy: true}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/plugin/ -run TestDispatcher -v`
Expected: FAIL — `plugin.NewDispatcher undefined`, `plugin.ProxyResolver undefined`.

- [ ] **Step 3: Write minimal implementation**

Create `server/internal/plugin/dispatcher.go`:

```go
package plugin

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ProxyResolver is the registry behaviour the dispatcher needs. Declared here so
// tests can fake it without standing up real processes.
type ProxyResolver interface {
	Lookup(id string) (Entry, bool)
}

// NewDispatcher returns the single catch-all handler mounted at
// /api/plugins/{id}/proxy/*. It resolves the live registry per request — chi
// freezes its route tree after ListenAndServe (chi #480), so live enable/disable
// cannot add or remove routes; routing must be data, not router structure.
//
// Responses: 400 for a malformed id; 503 when the plugin is not currently
// serving (stopped, unhealthy, or not a route/ui extension); otherwise the
// request is reverse-proxied with Cookie/Authorization stripped (NewReverseProxy).
func NewDispatcher(res ProxyResolver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if !pluginIDRe.MatchString(id) {
			http.Error(w, "invalid plugin id", http.StatusBadRequest)
			return
		}
		entry, ok := res.Lookup(id)
		if !ok || !entry.healthy {
			http.Error(w, "plugin not available", http.StatusServiceUnavailable)
			return
		}
		if !entry.Descriptor.HasCapability(CapRouteExtension) && !entry.Descriptor.HasCapability(CapUIExtension) {
			http.Error(w, "plugin not available", http.StatusServiceUnavailable)
			return
		}
		NewReverseProxy(entry, "/api/plugins/"+id+"/proxy").ServeHTTP(w, r)
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/plugin/ -run TestDispatcher -v`
Expected: PASS (all 5 dispatcher tests)

- [ ] **Step 5: Commit**

```bash
git add server/internal/plugin/dispatcher.go server/internal/plugin/dispatcher_test.go server/internal/plugin/registry.go
git commit --no-gpg-sign -m "feat: add catch-all plugin proxy dispatcher

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: Wire dispatcher in router; remove boot per-plugin mount

**Files:**
- Modify: `server/internal/api/router.go:443-453` (the per-plugin Mount loop)

- [ ] **Step 1: Write the failing test**

Router wiring is integration-level; assert via a focused test in `server/internal/api` if a router test harness exists (search `router_test.go`). If one exists, add:

```go
func TestRouterMountsPluginProxyDispatcher(t *testing.T) {
	// Build the router with a registry holding one healthy route_extension whose
	// upstream is an httptest server; assert GET /api/plugins/{id}/proxy/x reaches it.
	// Follow the existing router_test.go construction pattern for RouterDeps.
}
```

> If `server/internal/api/router_test.go` has no harness to construct `RouterDeps` cheaply, SKIP adding a router-level test (the dispatcher itself is covered in Task 3) and rely on `go build` + manual verification in Task 10. Do not fabricate a heavy harness.

- [ ] **Step 2: Run build to verify current state**

Run: `cd server && go build ./...`
Expected: PASS (baseline before edit).

- [ ] **Step 3: Write minimal implementation**

In `server/internal/api/router.go`, replace the per-plugin Mount loop (lines ~443-453) with a single catch-all registration:

```go
		// SP1 lifecycle + settings endpoints under the clean /api/plugins namespace.
		if deps.PluginLifecycleHandler != nil {
			deps.PluginLifecycleHandler.Mount(r)
		}
		// Live route/ui-extension dispatch. One catch-all resolves the registry per
		// request: chi freezes routes after serve (chi #480), so enable/disable
		// cannot mutate the route tree. Mounted inside the authed group so it
		// inherits JWT + same-origin guards; the proxy strips Cookie/Authorization
		// before forwarding to the plugin.
		if deps.PluginRegistry != nil {
			r.Handle("/api/plugins/{id}/proxy/*", plugin.NewDispatcher(deps.PluginRegistry))
		}
```

(Delete the entire `for _, entry := range deps.PluginRegistry.All() { ... r.Mount("/api/settings/plugins/"+id, ...) }` block.)

- [ ] **Step 4: Run build + dispatcher tests**

Run: `cd server && go build ./... && go test ./internal/plugin/ -run TestDispatcher -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/internal/api/router.go
git commit --no-gpg-sign -m "feat: serve plugin routes via live catch-all dispatcher

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: Process groups (Setpgid) + group-kill

**Files:**
- Modify: `server/internal/plugin/registry.go` — `startEntry`, `watchPlugin` restart spawn, `gracefulStop`

- [ ] **Step 1: Write the failing test**

Add to `registry_test.go` (spawns a plugin that forks a child; after Shutdown the child must be gone):

```go
func TestGroupKillReapsChildren(t *testing.T) {
	dir := t.TempDir()
	// writeForkingPlugin starts a health server then spawns a long-lived child
	// (e.g. `sleep 600`) in the SAME process group, writing the child PID to a
	// file. Implement it alongside the existing fixture helpers in this file.
	childPidFile := writeForkingPlugin(t, dir, "forker")

	r := plugin.New(dir)
	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))
	_, ok := r.Lookup("forker")
	require.True(t, ok)

	r.Shutdown()

	childPid := readPidFile(t, childPidFile)
	require.False(t, processAlive(childPid), "group-kill must reap the plugin's child process")
}

func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
```

> `writeForkingPlugin` / `readPidFile` are new helpers — model them on the existing health-plugin fixture in this file. The forking plugin is a tiny shell script: start `/health` responder, then `sleep 600 & echo $! > childpid`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/plugin/ -run TestGroupKillReapsChildren -v`
Expected: FAIL — child still alive (only the parent got SIGTERM; the child is orphaned, not group-killed).

- [ ] **Step 3: Write minimal implementation**

In `registry.go`, set the process group on every spawn. In `startEntry`:

```go
		cmd := exec.CommandContext(serverCtx, desc.Command[0], desc.Command[1:]...)
		cmd.Dir = pluginDir
		cmd.Env = buildPluginEnv(desc.Env)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start: %w", err)
		}
```

In `watchPlugin`'s restart spawn (the `newCmd` block):

```go
		newCmd := exec.CommandContext(ctx, desc.Command[0], desc.Command[1:]...)
		newCmd.Dir = pluginDir
		newCmd.Env = buildPluginEnv(desc.Env)
		newCmd.Stdout = os.Stdout
		newCmd.Stderr = os.Stderr
		newCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
```

Rewrite `gracefulStop` to signal the process group (negative pgid). Replace the two `cmd.Process.Signal`/`cmd.Process.Kill` calls:

```go
func gracefulStop(cmd *exec.Cmd, watcherDone <-chan struct{}) {
	if cmd.Process == nil {
		return
	}
	signalGroup(cmd.Process.Pid, syscall.SIGTERM)

	done := watcherDone
	if done == nil {
		ownDone := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(ownDone)
		}()
		done = ownDone
	}

	go func() {
		select {
		case <-done:
			// process exited — nothing to do
		case <-time.After(5 * time.Second):
			signalGroup(cmd.Process.Pid, syscall.SIGKILL)
		}
	}()
}

// signalGroup sends sig to the process group led by pid (Setpgid makes the child
// its own group leader, so its pgid == pid). The negative target reaches the
// leader and all descendants, so a plugin's child processes die with it.
// Falls back to the single process if the group send fails.
func signalGroup(pid int, sig syscall.Signal) {
	if err := syscall.Kill(-pid, sig); err != nil {
		_ = syscall.Kill(pid, sig)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/plugin/ -run TestGroupKillReapsChildren -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/internal/plugin/registry.go server/internal/plugin/registry_test.go
git commit --no-gpg-sign -m "feat: kill plugin process groups to reap child processes

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 6: Suppress restart on intentional stop

**Files:**
- Modify: `server/internal/plugin/registry.go` — `StopOne` sets the flag; `watchPlugin` checks it; add `setIntentionalStop`/`isIntentionalStop`

- [ ] **Step 1: Write the failing test**

Add to `registry_test.go`:

```go
func TestStopOneDoesNotRespawn(t *testing.T) {
	dir := t.TempDir()
	writeHealthyPlugin(t, dir, "stoppable")

	r := plugin.New(dir)
	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))
	t.Cleanup(r.Shutdown)

	require.NoError(t, r.StopOne("stoppable"))

	// Give any (buggy) restart path time to fire, then assert it stayed down.
	require.Eventually(t, func() bool {
		_, ok := r.Lookup("stoppable")
		return !ok
	}, 3*time.Second, 50*time.Millisecond, "intentional stop must not respawn")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/plugin/ -run TestStopOneDoesNotRespawn -v`
Expected: FAIL — after SIGTERM the watcher sees a non-nil `Wait` error, `ctx.Err()` is nil (server not shutting down), so it respawns; the entry reappears.

- [ ] **Step 3: Write minimal implementation**

Add helpers in `registry.go`:

```go
// setIntentionalStop marks the entry's pending exit as deliberate so watchPlugin
// will not respawn it.
func (r *Registry) setIntentionalStop(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.plugins {
		if r.plugins[i].Descriptor.ID == id {
			r.plugins[i].intentionalStop = true
			return
		}
	}
}

// isIntentionalStop reports whether the watcher should refrain from restarting.
// An absent entry counts as intentional: by the time the watcher checks, StopOne
// may already have deregistered it, and a removed plugin must never respawn.
func (r *Registry) isIntentionalStop(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for i := range r.plugins {
		if r.plugins[i].Descriptor.ID == id {
			return r.plugins[i].intentionalStop
		}
	}
	return true
}
```

In `StopOne`, set the flag before signalling (insert before the `gracefulStop` call):

```go
	if target == nil {
		return nil
	}
	r.setIntentionalStop(id)
	if target.cmd != nil {
		gracefulStop(target.cmd, target.cmdDone)
	}
	r.removeByID(id)
	return nil
```

In `watchPlugin`, after `current.Wait()` returns and `done` is closed, short-circuit before the crash/restart logic. Insert immediately after the `firstWait` block:

```go
		if firstWait {
			close(done)
			firstWait = false
		}
		if r.isIntentionalStop(desc.ID) {
			return
		}
		if err == nil {
			return
		}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/plugin/ -run TestStopOneDoesNotRespawn -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/internal/plugin/registry.go server/internal/plugin/registry_test.go
git commit --no-gpg-sign -m "fix: do not respawn plugins stopped on purpose

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 7: Mark unhealthy instead of silent remove

**Files:**
- Modify: `server/internal/plugin/hooks.go` — add `OnUnhealthy`
- Modify: `server/internal/plugin/registry.go` — `watchPlugin` give-up paths call `markUnhealthy`; add `markUnhealthy`
- Test: `server/internal/plugin/registry_test.go`

- [ ] **Step 1: Write the failing test**

Add to `registry_test.go` (a plugin that crashes repeatedly; after exhausted restarts it must be retained-but-unhealthy and OnUnhealthy must fire):

```go
func TestExhaustedRestartsMarkUnhealthy(t *testing.T) {
	dir := t.TempDir()
	// writeCrashingPlugin: health server that exits non-zero shortly after each
	// start, so every restart attempt eventually gives up.
	writeCrashingPlugin(t, dir, "flaky")

	var unhealthyID string
	var mu sync.Mutex
	r := plugin.New(dir)
	require.NoError(t, r.Load(context.Background(), plugin.Hooks{
		OnUnhealthy: func(id string) { mu.Lock(); unhealthyID = id; mu.Unlock() },
	}))
	t.Cleanup(r.Shutdown)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return unhealthyID == "flaky"
	}, 60*time.Second, 200*time.Millisecond, "OnUnhealthy must fire after exhausted restarts")

	entry, ok := r.Lookup("flaky")
	require.True(t, ok, "entry must be retained so the dispatcher can answer 503")
	require.False(t, entry.Healthy())
}
```

> `writeCrashingPlugin` is a new fixture: a script serving `/health` 200 once, then exiting code 1 a few hundred ms later. Backoff is 1s→5s→30s over 3 attempts, so the test budget is 60s.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/plugin/ -run TestExhaustedRestartsMarkUnhealthy -v`
Expected: FAIL — `plugin.Hooks{}.OnUnhealthy` undefined; and current code calls `removeByID` (entry vanishes; Lookup returns false).

- [ ] **Step 3: Write minimal implementation**

In `hooks.go`, add the field:

```go
	SetAuth func(provider authpkg.OAuthProvider, loginURL string)
	// OnUnhealthy is called when a plugin exhausts its restart budget. The entry
	// is retained (so the dispatcher can answer 503); the callback lets the
	// server persist the dead state (e.g. mark the plugin inactive in the DB).
	OnUnhealthy func(id string)
```

In `registry.go`, add `markUnhealthy`:

```go
// markUnhealthy flips the entry to unhealthy and notifies the OnUnhealthy hook.
// The entry is kept so the dispatcher serves a knowing 503 rather than the
// ambiguous 503 of a missing plugin.
func (r *Registry) markUnhealthy(id string) {
	r.mu.Lock()
	for i := range r.plugins {
		if r.plugins[i].Descriptor.ID == id {
			r.plugins[i].healthy = false
			break
		}
	}
	hook := r.hooks.OnUnhealthy
	r.mu.Unlock()
	if hook != nil {
		hook(id)
	}
}
```

In `watchPlugin`, replace the three give-up `r.removeByID(desc.ID)` calls with `r.markUnhealthy(desc.ID)`:

```go
		if restartCount >= maxPluginRestarts {
			slog.Error("plugin: exceeded restart limit — marking unhealthy", "id", desc.ID, "restarts", restartCount)
			r.markUnhealthy(desc.ID)
			return
		}
```

```go
		if startErr := newCmd.Start(); startErr != nil {
			slog.Error("plugin: restart failed — could not start process", "id", desc.ID, "err", startErr)
			r.markUnhealthy(desc.ID)
			return
		}
```

```go
		if healthErr := r.waitHealthy(ctx, baseURL); healthErr != nil {
			slog.Error("plugin: restart failed — health check did not pass", "id", desc.ID, "err", healthErr)
			gracefulStop(newCmd, nil)
			r.markUnhealthy(desc.ID)
			return
		}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/plugin/ -run TestExhaustedRestartsMarkUnhealthy -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/internal/plugin/hooks.go server/internal/plugin/registry.go server/internal/plugin/registry_test.go
git commit --no-gpg-sign -m "feat: retain crashed plugins as unhealthy and notify supervisor

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 8: Registry `WithTransient`

**Files:**
- Modify: `server/internal/plugin/registry.go` — add `WithTransient`
- Test: `server/internal/plugin/registry_test.go`

- [ ] **Step 1: Write the failing test**

Add to `registry_test.go`:

```go
func TestWithTransientStartsStoppedPluginAndStops(t *testing.T) {
	dir := t.TempDir()
	writeHealthyPlugin(t, dir, "trans")

	r := plugin.New(dir)
	r.SetEnabled(func(string) bool { return false }) // not started at Load
	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))
	t.Cleanup(r.Shutdown)

	_, ok := r.Lookup("trans")
	require.False(t, ok, "precondition: plugin not running")

	ran := false
	err := r.WithTransient(context.Background(), "trans", func() error {
		_, up := r.Lookup("trans")
		require.True(t, up, "plugin must be running inside the callback")
		ran = true
		return nil
	})
	require.NoError(t, err)
	require.True(t, ran)

	require.Eventually(t, func() bool {
		_, up := r.Lookup("trans")
		return !up
	}, 3*time.Second, 50*time.Millisecond, "transient plugin must be stopped after the callback")
}

func TestWithTransientLeavesRunningPluginUp(t *testing.T) {
	dir := t.TempDir()
	writeHealthyPlugin(t, dir, "persistent")

	r := plugin.New(dir)
	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))
	t.Cleanup(r.Shutdown)

	require.NoError(t, r.WithTransient(context.Background(), "persistent", func() error { return nil }))

	_, up := r.Lookup("persistent")
	require.True(t, up, "an already-running plugin must stay up after WithTransient")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/plugin/ -run TestWithTransient -v`
Expected: FAIL — `r.WithTransient undefined`.

- [ ] **Step 3: Write minimal implementation**

Add to `registry.go`:

```go
// WithTransient ensures the plugin is running for the duration of fn. If it was
// not already up, WithTransient starts it, runs fn, then stops it; if it was
// already running, fn runs and the process is left untouched. Used to deliver
// lifecycle hooks to an installed-but-stopped plugin.
func (r *Registry) WithTransient(ctx context.Context, id string, fn func() error) error {
	if _, running := r.Lookup(id); running {
		return fn()
	}
	if err := r.StartOne(ctx, id); err != nil {
		return fmt.Errorf("transient start %s: %w", id, err)
	}
	defer func() { _ = r.StopOne(id) }()
	return fn()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/plugin/ -run TestWithTransient -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/internal/plugin/registry.go server/internal/plugin/registry_test.go
git commit --no-gpg-sign -m "feat: add transient plugin start for lifecycle hooks

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 9: Lifecycle engine drives processes via ProcessManager

**Files:**
- Modify: `server/internal/pluginlifecycle/engine.go` — `ProcessManager` interface, `New` 4th param, transition strategies
- Modify: `server/internal/pluginlifecycle/engine_test.go` — fake `ProcessManager`, assert order + rollback
- Modify: `server/cmd/serve/di.go` — `pluginProcessAdapter`, pass to `New`

- [ ] **Step 1: Write the failing test**

In `engine_test.go`, add a fake recording process manager and tests. (Adapt `newEngine`/repo fakes to the existing helpers in this file; the existing `New(repo, hooks, settings)` calls must change to the 4-arg form.)

```go
type fakeProc struct{ events *[]string }

func (f fakeProc) Start(_ context.Context, id string) error {
	*f.events = append(*f.events, "start:"+id)
	return nil
}
func (f fakeProc) Stop(_ context.Context, id string) error {
	*f.events = append(*f.events, "stop:"+id)
	return nil
}
func (f fakeProc) WithTransient(_ context.Context, id string, fn func() error) error {
	*f.events = append(*f.events, "transient-begin:"+id)
	err := fn()
	*f.events = append(*f.events, "transient-end:"+id)
	return err
}

func TestActivateStartsBeforeHookThenSetsActive(t *testing.T) {
	var events []string
	repo := newFakeRepo(t) // installed, inactive — follow existing helper
	repo.installed("voice")
	hooks := &recordingHooks{events: &events} // existing recording hook caller, or add one
	eng := pluginlifecycle.New(repo, hooks, noopClearer{}, fakeProc{events: &events})

	d := plugin.Descriptor{ID: "voice", Lifecycle: plugin.LifecycleHooks{Activate: "/lifecycle/activate"}}
	require.NoError(t, eng.Activate(context.Background(), d))

	require.Equal(t, []string{"start:voice", "hook:/lifecycle/activate", "setActive:true"}, events)
}

func TestActivateHookFailureStopsAndDoesNotActivate(t *testing.T) {
	var events []string
	repo := newFakeRepo(t)
	repo.installed("voice")
	hooks := &recordingHooks{events: &events, fail: true}
	eng := pluginlifecycle.New(repo, hooks, noopClearer{}, fakeProc{events: &events})

	d := plugin.Descriptor{ID: "voice", Lifecycle: plugin.LifecycleHooks{Activate: "/lifecycle/activate"}}
	require.Error(t, eng.Activate(context.Background(), d))
	require.Equal(t, []string{"start:voice", "hook:/lifecycle/activate", "stop:voice"}, events)
	require.False(t, repo.active("voice"))
}

func TestInstallWrapsHooksInTransient(t *testing.T) {
	var events []string
	repo := newFakeRepo(t)
	hooks := &recordingHooks{events: &events}
	eng := pluginlifecycle.New(repo, hooks, noopClearer{}, fakeProc{events: &events})

	d := plugin.Descriptor{ID: "voice", Lifecycle: plugin.LifecycleHooks{Install: "/lifecycle/install"}}
	require.NoError(t, eng.Install(context.Background(), d))

	require.Equal(t, "transient-begin:voice", events[0])
	require.Equal(t, "transient-end:voice", events[len(events)-1])
}
```

> Adapt fake repo/hook helper names to whatever `engine_test.go` already defines. The recording hook caller must append `"hook:"+path`. `noopClearer` is a no-op `SettingsClearer`. If a 3-arg `New` is used elsewhere in the test file, update every call site to 4 args.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/pluginlifecycle/ -run 'TestActivate|TestInstall' -v`
Expected: FAIL — `New` takes 3 args / `ProcessManager` undefined.

- [ ] **Step 3: Write minimal implementation**

In `engine.go`, add the interface, the field, nil-safe helpers, and rewrite transitions:

```go
// ProcessManager runs plugin processes for the engine. The registry implements
// it (via an adapter); a nil manager makes every method a no-op so the engine is
// testable in isolation.
type ProcessManager interface {
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string) error
	WithTransient(ctx context.Context, id string, fn func() error) error
}

type Engine struct {
	repo     StateRepo
	hooks    HookCaller
	settings SettingsClearer
	proc     ProcessManager
}

func New(repo StateRepo, hooks HookCaller, settings SettingsClearer, proc ProcessManager) *Engine {
	return &Engine{repo: repo, hooks: hooks, settings: settings, proc: proc}
}

func (e *Engine) start(ctx context.Context, id string) error {
	if e.proc == nil {
		return nil
	}
	return e.proc.Start(ctx, id)
}

func (e *Engine) stop(ctx context.Context, id string) error {
	if e.proc == nil {
		return nil
	}
	return e.proc.Stop(ctx, id)
}

func (e *Engine) withTransient(ctx context.Context, id string, fn func() error) error {
	if e.proc == nil {
		return fn()
	}
	return e.proc.WithTransient(ctx, id, fn)
}
```

Rewrite the transitions:

```go
func (e *Engine) Install(ctx context.Context, d plugin.Descriptor) error {
	st, err := e.repo.GetState(ctx, d.ID)
	if err != nil {
		return err
	}
	if st.InstalledAt != nil {
		return fmt.Errorf("pluginlifecycle: %s already installed", d.ID)
	}
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

func (e *Engine) Activate(ctx context.Context, d plugin.Descriptor) error {
	st, err := e.repo.GetState(ctx, d.ID)
	if err != nil {
		return err
	}
	if st.InstalledAt == nil {
		return fmt.Errorf("pluginlifecycle: %s must be installed before activate", d.ID)
	}
	if err := e.start(ctx, d.ID); err != nil {
		return fmt.Errorf("activate start: %w", err)
	}
	if err := e.callHook(ctx, d, d.Lifecycle.Activate); err != nil {
		_ = e.stop(ctx, d.ID)
		return fmt.Errorf("activate hook: %w", err)
	}
	return e.repo.SetActive(ctx, d.ID, true)
}

func (e *Engine) Deactivate(ctx context.Context, d plugin.Descriptor) error {
	if err := e.callHook(ctx, d, d.Lifecycle.Deactivate); err != nil {
		return fmt.Errorf("deactivate hook: %w", err)
	}
	if err := e.repo.SetActive(ctx, d.ID, false); err != nil {
		return err
	}
	return e.stop(ctx, d.ID)
}

func (e *Engine) Update(ctx context.Context, d plugin.Descriptor) error {
	if err := e.withTransient(ctx, d.ID, func() error {
		return e.callHook(ctx, d, d.Lifecycle.Update)
	}); err != nil {
		return fmt.Errorf("update hook: %w", err)
	}
	return e.repo.SetVersion(ctx, d.ID, d.Version)
}

func (e *Engine) Uninstall(ctx context.Context, d plugin.Descriptor) error {
	st, err := e.repo.GetState(ctx, d.ID)
	if err != nil {
		return err
	}
	if st.Active {
		if err := e.Deactivate(ctx, d); err != nil {
			return err
		}
	}
	if err := e.withTransient(ctx, d.ID, func() error {
		return e.callHook(ctx, d, d.Lifecycle.Uninstall)
	}); err != nil {
		return fmt.Errorf("uninstall hook: %w", err)
	}
	if err := e.settings.Clear(ctx, d.ID); err != nil {
		return err
	}
	return e.repo.SetInstalledAt(ctx, d.ID, nil)
}
```

In `di.go`, add the adapter (near the other `plugin*Adapter` types) and pass it to `New`:

```go
// pluginProcessAdapter lets the lifecycle engine drive the plugin registry's
// process lifecycle without the plugin package importing pluginlifecycle.
type pluginProcessAdapter struct{ reg *plugin.Registry }

func (a pluginProcessAdapter) Start(ctx context.Context, id string) error { return a.reg.StartOne(ctx, id) }
func (a pluginProcessAdapter) Stop(_ context.Context, id string) error    { return a.reg.StopOne(id) }
func (a pluginProcessAdapter) WithTransient(ctx context.Context, id string, fn func() error) error {
	return a.reg.WithTransient(ctx, id, fn)
}
```

Update the engine construction in `di.go` (line ~275):

```go
		lifecycleEngine := pluginlifecycle.New(
			pluginStateRepoAdapter{inner: pluginRepo},
			pluginlifecycle.NewHTTPHookCaller(),
			pluginSettingsSvc,
			pluginProcessAdapter{reg: pluginRegistry},
		)
```

- [ ] **Step 4: Run tests + build**

Run: `cd server && go test ./internal/pluginlifecycle/ -v && go build ./...`
Expected: PASS (engine tests green; di.go compiles with the 4-arg `New`).

- [ ] **Step 5: Commit**

```bash
git add server/internal/pluginlifecycle/engine.go server/internal/pluginlifecycle/engine_test.go server/cmd/serve/di.go
git commit --no-gpg-sign -m "feat: drive plugin processes from lifecycle transitions

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 10: Wire OnUnhealthy in DI

**Files:**
- Modify: `server/cmd/serve/di.go` — set `Hooks.OnUnhealthy` in the `Load` call

- [ ] **Step 1: Write the failing test**

No unit test (DI wiring; the callback behaviour is covered by Task 7's registry test). Verify by build + the manual check in Task 12.

- [ ] **Step 2: Build baseline**

Run: `cd server && go build ./...`
Expected: PASS

- [ ] **Step 3: Write minimal implementation**

In `di.go`, extend the `plugin.Hooks` literal passed to `Load` (line ~229) so a crashed plugin is persisted inactive. `pluginRepo` is already in scope:

```go
	if err := pluginRegistry.Load(ctx, plugin.Hooks{
		SetAuth: func(p authpkg.OAuthProvider, loginURL string) {
			oauthProvider = p
			pluginLoginURL = loginURL
			slog.Info("auth: using plugin provider", "loginURL", loginURL)
		},
		OnUnhealthy: func(id string) {
			if pluginRepo == nil {
				return
			}
			if err := pluginRepo.SetActive(ctx, id, false); err != nil {
				slog.Error("plugin: failed to persist unhealthy state", "id", id, "err", err)
			}
		},
	}); err != nil {
```

- [ ] **Step 4: Build**

Run: `cd server && go build ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/cmd/serve/di.go
git commit --no-gpg-sign -m "feat: persist plugins inactive when they become unhealthy

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 11: Remove interim restart-to-apply enablement

**Files:**
- Modify: `server/internal/api/plugins/handler.go` — drop `patch` route/handler/`patchResponse`; drop `SetEnabled` from `Controller`
- Modify: `server/internal/pluginsctl/controller.go` — drop `SetEnabled`/`Applied`/`AppliedLive`/`AppliedRestart`/`persist`/`findDescriptor` (keep `List`/`discover`/`enabledSet`/`ErrUnknownPlugin`/`ErrInvalidAction`)
- Modify: `server/internal/pluginsctl/controller_test.go` — drop `SetEnabled` tests
- Modify: `server/internal/api/plugins/handler_test.go` (if it tests `patch`) — drop those tests

- [ ] **Step 1: Write the failing test**

Add to `server/internal/api/plugins/handler_test.go` (asserts the interim route is gone):

```go
func TestPluginsEnabledPatchRouteRemoved(t *testing.T) {
	r := chi.NewRouter()
	plugins.New(stubController{}).Mount(r) // stubController implements only List now

	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"enabled":true}`)
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/api/settings/plugins-enabled/voice", body))

	require.Equal(t, http.StatusMethodNotAllowed, rec.Code) // chi: no route → 405/404
}
```

> Adapt `stubController` to the trimmed `Controller` interface (List only). Follow the existing handler_test.go fakes.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/api/plugins/ -run TestPluginsEnabledPatchRouteRemoved -v`
Expected: FAIL — the route still exists (`patch` handler answers), so the status is not 405/404. (Or compile error because `stubController` no longer satisfies the still-fat interface — fix in Step 3.)

- [ ] **Step 3: Write minimal implementation**

In `handler.go`, trim the `Controller` interface and `Mount`, and delete `patch`/`patchResponse`:

```go
// Controller is the behaviour the handler needs from pluginsctl; faked in tests.
type Controller interface {
	List() ([]pluginsctl.PluginState, error)
}
```

```go
// Mount registers the read-only plugin listing on r. Enable/disable is handled
// live by the lifecycle endpoints (/api/plugins/{id}/activate|deactivate).
func (h *Handler) Mount(r chi.Router) {
	r.Get("/api/settings/plugins", apierr.ErrorMiddleware(h.list))
}
```

Delete the `patchResponse` struct and the entire `patch` method. Remove now-unused imports (`errors` if only `patch` used it — verify with the compiler).

In `pluginsctl/controller.go`, delete `Applied`, `AppliedLive`, `AppliedRestart`, `SetEnabled`, `persist`, and `findDescriptor`. Keep `ErrUnknownPlugin` and `ErrInvalidAction` (the lifecycle handler's `classify` references them). Remove the now-unused `mu sync.Mutex` field and the `sync`/`time` imports if nothing else uses them (verify with the compiler).

In `controller_test.go` and `handler_test.go`, delete every test that calls `SetEnabled`/`patch`.

- [ ] **Step 4: Run tests + build**

Run: `cd server && go test ./internal/api/plugins/ ./internal/pluginsctl/ -v && go build ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/internal/api/plugins/handler.go server/internal/api/plugins/handler_test.go server/internal/pluginsctl/controller.go server/internal/pluginsctl/controller_test.go
git commit --no-gpg-sign -m "refactor: remove interim restart-to-apply plugin enablement

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 12: Full verification + docs

**Files:**
- Modify: `README.md`, `CHANGELOG.md`, and the plugin authoring docs (search `docs/` for the plugin route/URL contract, e.g. `/api/settings/plugins/`)

- [ ] **Step 1: Full scoped test + build + lint**

Run:
```bash
cd server && go test ./internal/plugin/... ./internal/pluginlifecycle/... ./internal/api/plugins/... ./internal/pluginsctl/... -race
cd server && go build ./...
cd server && golangci-lint run ./internal/plugin/... ./internal/pluginlifecycle/... ./internal/api/... ./internal/pluginsctl/... ./cmd/...
```
Expected: all PASS, lint 0. Confirm `git status` shows NO changes under `server/internal/db/ent/` (no ent regen happened).

- [ ] **Step 2: Manual live-toggle smoke test**

Run the server with a `route_extension` plugin (e.g. `voice-whisper`) and verify, with NO restart between steps:
```bash
# activate → route live
curl -fsS -X POST -H "Origin: http://127.0.0.1:13120" http://127.0.0.1:13120/api/plugins/voice-whisper/activate
curl -fsS http://127.0.0.1:13120/api/plugins/voice-whisper/proxy/health   # → 200
# deactivate → route gone, process stopped
curl -fsS -X POST -H "Origin: http://127.0.0.1:13120" http://127.0.0.1:13120/api/plugins/voice-whisper/deactivate
curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:13120/api/plugins/voice-whisper/proxy/health  # → 503
```
Expected: activate makes `/proxy/*` reachable instantly; deactivate makes it 503; the plugin process is gone (`pgrep -f voice-whisper` empty) with no orphaned children.

- [ ] **Step 3: Update docs**

Update `README.md` and plugin docs: plugin route extensions are served at `/api/plugins/{id}/proxy/*` (was `/api/settings/plugins/{id}`); enable/disable is **live** via `POST /api/plugins/{id}/activate|deactivate` (no restart); the interim `PATCH /api/settings/plugins-enabled/{id}` is removed. Add a `CHANGELOG.md` entry under the Unreleased `Changed`/`Added` headings (Keep a Changelog):

```markdown
### Changed
- Plugin route extensions now serve under `/api/plugins/{id}/proxy/*` and enable/disable live via the lifecycle endpoints (no server restart). The interim `PATCH /api/settings/plugins-enabled/{id}` and per-plugin boot-mounted routes are removed.

### Added
- Plugin process groups with group-kill (no orphaned child processes), suppression of restarts for intentionally stopped plugins, and crash supervision that marks a plugin unhealthy (HTTP 503) instead of silently removing it.
```

- [ ] **Step 4: Verify docs claims**

Re-read each edited doc line against the code (grep for any remaining `/api/settings/plugins/` references that are now stale). Fix stragglers.

- [ ] **Step 5: Commit**

```bash
git add README.md CHANGELOG.md docs/
git commit --no-gpg-sign -m "docs: live plugin dispatch + process management

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Self-Review

**Spec coverage:**
- Catch-all dispatcher + atomic registry → Tasks 1, 3, 4. ✓
- Live activate/deactivate (start/stop, zero restart) → Task 9 (engine) + 1/3/4 (dispatch). ✓
- `ProcessManager` seam → Task 9. ✓
- Transient start for hooks → Tasks 8 (registry) + 9 (engine wraps Install/Update/Uninstall). ✓
- Setpgid + group-kill → Task 5. ✓
- Suppress-restart-on-intentional-stop → Task 6. ✓
- Mark-unhealthy in DB → Task 7 (registry/hook) + Task 10 (DI persists `active=false`). ✓
- Remove interim PATCH + boot per-plugin mount → Tasks 4 (mount) + 11 (PATCH). ✓
- Docs / URL-contract change → Task 12. ✓
- Error handling (activate rollback, 400/503, transient defer-stop) → Tasks 3, 8, 9. ✓

**Placeholder scan:** Fixture helpers (`writeHealthyPlugin`, `writeForkingPlugin`, `writeCrashingPlugin`, `readPidFile`) are flagged as "model on the existing fixture in this file" rather than invented blind — the worker must reuse the established `/health`-serving fixture pattern in `registry_test.go`. The two skip-guards (router harness in Task 4, fixture reuse) are explicit, not vague TODOs.

**Type/name consistency:** `Lookup(id) (Entry, bool)`, `Entry.Healthy()`, `Entry.healthy`/`intentionalStop`, `WithTransient(ctx,id,fn)`, `markUnhealthy`, `setIntentionalStop`/`isIntentionalStop`, `Hooks.OnUnhealthy`, `ProxyResolver`, `NewDispatcher`, `NewHealthyEntryForTest`, `ProcessManager{Start,Stop,WithTransient}`, `pluginProcessAdapter`, `pluginlifecycle.New(repo,hooks,settings,proc)` — used consistently across tasks.

**Deviations from the spec (intentional, grounded in the real code):**
1. No build-tag platform file — `Setpgid`/`syscall.Kill` compile on darwin+linux (only supported OSes); placed directly in `registry.go` (which already imports `syscall`).
2. `Lookup` returns an `Entry` value copy via RLock'd scan, not a `map[string]*Entry` index — entries live in a value slice that reallocates; pointers would alias. N is single-digit.
3. Dispatcher returns 503 (not 404) for unknown/stopped/unhealthy/non-proxyable; distinguishing unknown→404 would need a per-request DB read. 400 only for a malformed id.
4. Interim removal keeps the read-only `GET /api/settings/plugins` list and the `pluginsctl` error sentinels (the lifecycle handler's `classify` depends on them); only the enablement write-path is removed.
