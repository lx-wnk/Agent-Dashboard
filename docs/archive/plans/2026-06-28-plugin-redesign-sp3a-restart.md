# Plugin Redesign SP3a — Web-Triggered Supervised Restart Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `POST /api/admin/restart` that validates (won't lock out auth), drains, then re-execs the binary (default) or exits for a supervisor — applying boot-wired plugin changes safely.

**Architecture:** A `restart.Controller` (channel + mode + a `Restarter` seam) is created in `main`, threaded into the DI so the new `admin.Handler` can signal it, and selected on in the serve run-loop: a restart cancels the errgroup, drains, runs `cleanup()`, then re-execs or exits. A `RestartValidator` refuses (409) if an active `auth_provider` plugin is unhealthy.

**Tech Stack:** Go, chi, cobra, errgroup, `syscall` (re-exec), testify.

---

## File Structure

| File | Responsibility | Action |
|---|---|---|
| `server/internal/config/config.go` | `RestartMode` field + default `reexec` + validation | Modify |
| `server/internal/restart/restart.go` | `Mode`, `Controller` (chan+mode), `Restarter` seam, real re-exec/exit impl, `Execute` | Create |
| `server/internal/restart/restart_test.go` | mode dispatch via fake Restarter; controller trigger | Create |
| `server/internal/api/admin/handler.go` | `POST /api/admin/restart`: validate → 202 / 409 | Create |
| `server/internal/api/admin/handler_test.go` | 202 on ok, 409 on validator fail | Create |
| `server/internal/restart/validator.go` | `AuthProviderValidator` (active auth_provider must be healthy) | Create |
| `server/internal/restart/validator_test.go` | passes w/o auth_provider; fails when unhealthy | Create |
| `server/internal/api/router.go` | mount admin handler in authed group | Modify |
| `server/cmd/serve/di.go` | construct controller-backed admin handler + validator | Modify |
| `server/cmd/serve/main.go` | create controller, thread it, select in run-loop, re-exec/exit after drain | Modify |
| `README.md`, `CHANGELOG.md` | supervisor modes + restart endpoint | Modify |

**Commands:** Test `cd server && go test ./internal/restart/... ./internal/api/admin/... -v`. Build `cd server && go build ./...`. Lint `golangci-lint run ./internal/... ./cmd/...`. Do NOT run `go test ./...` (regenerates ent). Commits `--no-gpg-sign`, English, no phase labels, trailers:
```
Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6
```

---

### Task 1: Config RestartMode

**Files:** Modify `server/internal/config/config.go`
- Test: `server/internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

Add to `config_test.go` (match the existing test style there):

```go
func TestRestartModeDefaultsToReexec(t *testing.T) {
	cfg := config.Defaults()
	require.Equal(t, "reexec", cfg.RestartMode)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/config/ -run TestRestartModeDefaultsToReexec -v`
Expected: FAIL — `cfg.RestartMode` empty.

- [ ] **Step 3: Implement**

In `config.go`, add to the `Config` struct (near the other bootstrap keys):

```go
	// RestartMode controls how POST /api/admin/restart relaunches the server:
	// "reexec" (default) replaces the process image in place (no supervisor needed);
	// "exit" exits 0 so an external supervisor (systemd/launchd/wrapper) restarts it.
	RestartMode string `koanf:"restart_mode"`
```

In `Defaults()` add `RestartMode: "reexec"`. After the koanf merge in `Load()` (before returning cfg), normalize+validate:

```go
	if cfg.RestartMode != "reexec" && cfg.RestartMode != "exit" {
		if cfg.RestartMode != "" {
			slog.Warn("invalid DASHBOARD_RESTART_MODE — falling back to reexec", "value", cfg.RestartMode)
		}
		cfg.RestartMode = "reexec"
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/config/ -run TestRestartMode -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/internal/config/config.go server/internal/config/config_test.go
git commit --no-gpg-sign -m "feat: add restart mode config (reexec default, exit for supervisors)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

### Task 2: restart Controller + Restarter seam

**Files:** Create `server/internal/restart/restart.go`, `server/internal/restart/restart_test.go`

- [ ] **Step 1: Write the failing test**

Create `server/internal/restart/restart_test.go`:

```go
package restart_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/restart"
)

type fakeRestarter struct{ reexec, exit int }

func (f *fakeRestarter) Reexec() error { f.reexec++; return nil }
func (f *fakeRestarter) Exit()         { f.exit++ }

func TestExecuteReexecMode(t *testing.T) {
	f := &fakeRestarter{}
	restart.Execute(restart.ModeReexec, f)
	require.Equal(t, 1, f.reexec)
	require.Equal(t, 0, f.exit)
}

func TestExecuteExitMode(t *testing.T) {
	f := &fakeRestarter{}
	restart.Execute(restart.ModeExit, f)
	require.Equal(t, 1, f.exit)
	require.Equal(t, 0, f.reexec)
}

func TestControllerTriggerIsNonBlocking(t *testing.T) {
	c := restart.NewController("reexec")
	c.Trigger() // must not block even with no reader
	c.Trigger() // second send coalesces (buffered size 1)
	select {
	case <-c.C():
	default:
		t.Fatal("expected a pending restart signal")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/restart/ -run 'TestExecute|TestController' -v`
Expected: FAIL — package/symbols undefined.

- [ ] **Step 3: Implement**

Create `server/internal/restart/restart.go`:

```go
// Package restart performs a graceful, web-triggered server restart: either an
// in-place re-exec of the current binary (default) or a clean exit for an
// external supervisor to relaunch.
package restart

import (
	"log/slog"
	"os"
	"syscall"
)

type Mode string

const (
	ModeReexec Mode = "reexec"
	ModeExit   Mode = "exit"
)

// Restarter performs the actual relaunch. Seam so the run-loop is testable
// without replacing the process.
type Restarter interface {
	Reexec() error
	Exit()
}

// OSRestarter is the production Restarter.
type OSRestarter struct{}

// Reexec replaces the current process image with a fresh run of the same binary,
// preserving args + environment (same PID). It only returns on error.
func (OSRestarter) Reexec() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}

// Exit terminates the process cleanly so a supervisor restarts it.
func (OSRestarter) Exit() { os.Exit(0) }

// Execute relaunches per mode. For reexec, a failure is fatal (log + exit 1) so
// the process never hangs in a half-down state.
func Execute(mode Mode, r Restarter) {
	if mode == ModeExit {
		r.Exit()
		return
	}
	if err := r.Reexec(); err != nil {
		slog.Error("restart: re-exec failed", "err", err)
		os.Exit(1)
	}
}

// Controller carries the restart signal from the HTTP handler to the run-loop
// and records the configured mode.
type Controller struct {
	ch   chan struct{}
	mode Mode
}

func NewController(mode string) *Controller {
	m := Mode(mode)
	if m != ModeExit {
		m = ModeReexec
	}
	return &Controller{ch: make(chan struct{}, 1), mode: m}
}

// Trigger requests a restart; non-blocking and coalescing (buffered size 1).
func (c *Controller) Trigger() {
	select {
	case c.ch <- struct{}{}:
	default:
	}
}

// C is the receive end the run-loop selects on.
func (c *Controller) C() <-chan struct{} { return c.ch }

// Mode is the configured relaunch mode (for the 202 response + Execute).
func (c *Controller) Mode() Mode { return c.mode }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/restart/ -run 'TestExecute|TestController' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/internal/restart/restart.go server/internal/restart/restart_test.go
git commit --no-gpg-sign -m "feat: add restart controller and relaunch seam

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

### Task 3: RestartValidator (lockout guard)

**Files:** Create `server/internal/restart/validator.go`, `server/internal/restart/validator_test.go`

- [ ] **Step 1: Write the failing test**

Create `server/internal/restart/validator_test.go`:

```go
package restart_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
	"github.com/lx-wnk/agent-dashboard/server/internal/restart"
)

type fakeAuthProbe struct {
	entries []plugin.Entry
}

func (f fakeAuthProbe) AllWithCapability(string) []plugin.Entry { return f.entries }

func TestValidatePassesWithNoAuthProvider(t *testing.T) {
	v := restart.NewAuthProviderValidator(fakeAuthProbe{})
	require.NoError(t, v.Validate(context.Background()))
}

func TestValidateFailsWhenAuthProviderUnhealthy(t *testing.T) {
	dead := plugin.NewHealthyEntryForTest(plugin.Descriptor{ID: "oauth", Capabilities: []string{plugin.CapAuthProvider}})
	// NewHealthyEntryForTest yields healthy=true; build an unhealthy one via the test seam:
	dead = plugin.NewEntryForTest(plugin.Descriptor{ID: "oauth", Capabilities: []string{plugin.CapAuthProvider}}, false)
	v := restart.NewAuthProviderValidator(fakeAuthProbe{entries: []plugin.Entry{dead}})
	require.Error(t, v.Validate(context.Background()))
}
```

> This test needs an exported seam to build an UNHEALTHY entry. If `plugin.NewEntryForTest(desc, healthy)` does not exist, add it next to `NewHealthyEntryForTest` in `registry.go`: `func NewEntryForTest(d Descriptor, healthy bool) Entry { return Entry{Descriptor: d, BaseURL: "http://" + d.Addr, healthy: healthy} }`. (Commit that one-liner with this task.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/restart/ -run TestValidate -v`
Expected: FAIL — `NewAuthProviderValidator` undefined.

- [ ] **Step 3: Implement**

Create `server/internal/restart/validator.go`:

```go
package restart

import (
	"context"
	"fmt"

	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
)

// authProbe is the registry slice the validator needs.
type authProbe interface {
	AllWithCapability(capability string) []plugin.Entry
}

// AuthProviderValidator refuses a restart that would brick boot: the boot
// fatal-safety check aborts startup when a configured auth_provider is unhealthy,
// so if any running auth_provider is currently unhealthy we must not restart.
type AuthProviderValidator struct{ reg authProbe }

func NewAuthProviderValidator(reg authProbe) *AuthProviderValidator {
	return &AuthProviderValidator{reg: reg}
}

func (v *AuthProviderValidator) Validate(_ context.Context) error {
	for _, e := range v.reg.AllWithCapability(plugin.CapAuthProvider) {
		if !e.Healthy() {
			return fmt.Errorf("auth_provider plugin %q is unhealthy — restart would lock out auth", e.Descriptor.ID)
		}
	}
	return nil
}
```

> `*plugin.Registry` already has `AllWithCapability(string) []plugin.Entry` and `Entry.Healthy()` (from SP2), so the real registry satisfies `authProbe` directly.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/restart/ -run TestValidate -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/internal/restart/validator.go server/internal/restart/validator_test.go server/internal/plugin/registry.go
git commit --no-gpg-sign -m "feat: add restart validator guarding against auth lockout

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

### Task 4: Admin restart endpoint

**Files:** Create `server/internal/api/admin/handler.go`, `server/internal/api/admin/handler_test.go`

- [ ] **Step 1: Write the failing test**

Create `server/internal/api/admin/handler_test.go`:

```go
package admin_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/api/admin"
)

type fakeValidator struct{ err error }

func (f fakeValidator) Validate(context.Context) error { return f.err }

func mount(h *admin.Handler) http.Handler {
	r := chi.NewRouter()
	h.Mount(r)
	return r
}

func TestRestartReturns202OnSuccess(t *testing.T) {
	triggered := make(chan struct{}, 1)
	h := admin.New(fakeValidator{}, "reexec", func() { triggered <- struct{}{} })
	rec := httptest.NewRecorder()
	mount(h).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/admin/restart", nil))

	require.Equal(t, http.StatusAccepted, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "restarting", body["status"])
	require.Equal(t, "reexec", body["mode"])
	select {
	case <-triggered:
	default:
		t.Fatal("expected restart trigger to fire")
	}
}

func TestRestartReturns409WhenValidatorFails(t *testing.T) {
	h := admin.New(fakeValidator{err: errors.New("auth would lock out")}, "reexec", func() { t.Fatal("must not trigger") })
	rec := httptest.NewRecorder()
	mount(h).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/admin/restart", nil))
	require.Equal(t, http.StatusConflict, rec.Code)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/api/admin/ -v`
Expected: FAIL — package undefined.

- [ ] **Step 3: Implement**

Create `server/internal/api/admin/handler.go`:

```go
// Package admin serves privileged server-control endpoints (currently restart).
package admin

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Validator decides whether a restart is safe (e.g. won't lock out auth).
type Validator interface {
	Validate(ctx context.Context) error
}

// Handler serves POST /api/admin/restart. trigger signals the run-loop to
// restart; mode is reported in the 202 body.
type Handler struct {
	validator Validator
	mode      string
	trigger   func()
}

func New(v Validator, mode string, trigger func()) *Handler {
	return &Handler{validator: v, mode: mode, trigger: trigger}
}

func (h *Handler) Mount(r chi.Router) {
	r.Post("/api/admin/restart", h.restart)
}

func (h *Handler) restart(w http.ResponseWriter, r *http.Request) {
	if err := h.validator.Validate(r.Context()); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	h.trigger()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "restarting", "mode": h.mode})
}
```

> The handler writes 409/202 directly (no `apierr` sentinel needed). If the project requires `apierr.ErrorMiddleware` for consistency, wrapping is optional here since this handler manages its own status codes.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/api/admin/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/internal/api/admin/handler.go server/internal/api/admin/handler_test.go
git commit --no-gpg-sign -m "feat: add admin restart endpoint with validation

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

### Task 5: Wire endpoint (router + DI) and run-loop re-exec

**Files:** Modify `server/internal/api/router.go`, `server/cmd/serve/di.go`, `server/cmd/serve/main.go`

- [ ] **Step 1: Add to RouterDeps + mount (router.go)**

In `RouterDeps` add `AdminHandler *admin.Handler` (import `"github.com/lx-wnk/agent-dashboard/server/internal/api/admin"`). Inside the protected `r.Group` (where the other `*.Mount(r)` calls are), add:

```go
		if deps.AdminHandler != nil {
			deps.AdminHandler.Mount(r)
		}
```

- [ ] **Step 2: Thread the controller into DI**

`initializeServer`/`provideRouter` in `cmd/serve/di.go` must construct the admin handler from a `*restart.Controller` passed in from `main`. Change the `initializeServer` signature to accept `restartCtl *restart.Controller` (add the import). Where `RouterDeps` is assembled, add:

```go
		AdminHandler: admin.New(
			restart.NewAuthProviderValidator(pluginRegistry),
			string(restartCtl.Mode()),
			restartCtl.Trigger,
		),
```

(`pluginRegistry` is already in scope from SP1/SP2; it satisfies the validator's `authProbe`.)

- [ ] **Step 3: Create controller + run-loop handling (main.go)**

In `serve`'s `RunE` (`cmd/serve/main.go`), before `initializeServer`:

```go
			restartCtl := restart.NewController(cfg.RestartMode)
```
Pass `restartCtl` into `initializeServer(ctx, cfg, cfgFile, restartCtl)`. Replace the `defer cleanup()` pattern so cleanup runs before a re-exec (deferred funcs do NOT run after a successful `syscall.Exec`). Add a restart watcher goroutine and post-wait handling:

```go
			restarting := false
			g.Go(func() error {
				select {
				case <-ctx.Done():
					return nil
				case <-restartCtl.C():
					restarting = true
					stop() // cancel the signal context → graceful shutdown of all g.Go members
					return nil
				}
			})

			err = g.Wait()
			cleanup() // stop plugins etc. BEFORE any re-exec (deferred funcs won't run after Exec)
			if restarting {
				slog.Info("restart: relaunching", "mode", restartCtl.Mode())
				restart.Execute(restartCtl.Mode(), restart.OSRestarter{})
			}
			return err
```

Remove the now-redundant `defer cleanup()` (cleanup is called explicitly on both paths — the normal path falls through to `return err` after `cleanup()`).

> Import `"github.com/lx-wnk/agent-dashboard/server/internal/restart"` and `"log/slog"` (already imported) in main.go. Ensure `cleanup` is not double-invoked: replace `defer cleanup()` with the explicit call shown.

- [ ] **Step 4: Build + targeted tests**

Run: `cd server && go build ./... && go test ./internal/restart/... ./internal/api/admin/... -v`
Expected: build clean, tests PASS. (Run-loop re-exec itself is covered by the seam tests in Task 2; do not actually exec in CI.)

- [ ] **Step 5: Commit**

```bash
git add server/internal/api/router.go server/cmd/serve/di.go server/cmd/serve/main.go
git commit --no-gpg-sign -m "feat: wire web-triggered restart into server lifecycle

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

### Task 6: Docs (supervisor modes)

**Files:** Modify `README.md`, `CHANGELOG.md`

- [ ] **Step 1: Document + commit**

README: add a "Restart" section — `POST /api/admin/restart` triggers a validated, graceful restart. Default `reexec` works for plain `./bin/agent-dashboard serve` (no supervisor). For supervised setups set `DASHBOARD_RESTART_MODE=exit` and run under systemd (`Restart=always`) / launchd (`KeepAlive`) / a wrapper loop (`while true; do ./bin/agent-dashboard serve; done`). Note that activating an `auth_provider` plugin requires a restart to apply. `CHANGELOG.md` Unreleased `### Added` bullet.

```bash
git add README.md CHANGELOG.md
git commit --no-gpg-sign -m "docs: document web-triggered restart and supervisor modes

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

## Self-Review

**Spec coverage:** RestartMode config (T1) ✓; controller + reexec/exit seam (T2) ✓; validate-before-restart (T3) ✓; endpoint 202/409 async-trigger (T4) ✓; router+DI+run-loop re-exec/exit with cleanup-before-exec (T5) ✓; supervisor docs (T6) ✓. Reuses existing signal/shutdown path ✓. No ent change ✓.
**Placeholder scan:** the one seam-add (`plugin.NewEntryForTest` if absent, T3) is explicit with the exact one-liner. `initializeServer` signature change is spelled out. No vague TODOs.
**Type consistency:** `restart.Controller` (`C()`, `Trigger`, `Mode()`), `restart.Execute(Mode, Restarter)`, `restart.OSRestarter`, `restart.NewAuthProviderValidator`, `admin.New(Validator, mode, trigger)`, `admin.Handler.Mount`, `RouterDeps.AdminHandler` — consistent across tasks. Validator consumes `plugin.Entry.Healthy()` + `AllWithCapability` (SP2 APIs).
