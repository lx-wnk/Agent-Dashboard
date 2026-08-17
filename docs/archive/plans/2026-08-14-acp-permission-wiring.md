# ACP Permission Wiring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make an ACP session's permission gate actually gate — force the session's permission mode, refuse options that would widen it, and turn the dashboard's asynchronous approval flow into the synchronous answer ACP requires.

**Architecture:** Three additions to the existing `server/internal/acp` package, none of which touch the database or the HTTP layer. Mode enforcement is a helper over `SetSessionMode`. Option safety is a rule inside the existing `RequestPermission`. The synchronous answer is a `PollingGate` that wraps two injected functions — one that files a request, one that reports its status — and blocks until the status leaves pending or the deadline passes. Binding those functions to the real repository is step 3's job, so everything here is testable without a database, a process, or a network.

**Tech Stack:** Go 1.26, `github.com/coder/acp-go-sdk` (replaced onto `github.com/lx-wnk/acp-go-sdk v1.20.0-lxwnk.alpha.1`), `stretchr/testify`.

## Global Constraints

- Go 1.26; the server module is `github.com/lx-wnk/agent-dashboard/server`. Package `server/internal/acp` already exists from the previous plan.
- The SDK is imported as `sdkacp "github.com/coder/acp-go-sdk"` in every file.
- Assertions use `github.com/stretchr/testify/require`.
- Tests must not spawn a process, touch the network, open a database, or sleep for a fixed wall-clock duration.
- Nothing outside `server/internal/acp/` may change.
- The fail-closed contract from the previous plan still binds: `DecisionDeny` is the zero value, no callback denies, a callback error denies, and only an explicit `DecisionAllow` with a nil error may select an allow option.
- Work on branch `feat/acp-permission-wiring`, cut from **`feat/acp-client`**, not from `main`. The `server/internal/acp` package lives on that branch and is still open as PR #358; branching from `main` gives you a tree without the package. Never commit to `main`.
- Gate after every task: `cd server && go vet ./... && go test -race ./internal/acp/...`. Full gate before the last commit: `task test` and `task lint` from the repository root.
- `task test` regenerates `server/internal/db/ent/`. Run `git checkout -- server/internal/db/ent/` before committing unless that regeneration is the change.

## Background the tasks assume

Verified against a live session on 2026-08-14, recorded in `docs/local/spec-acp-client.md`:

- A new ACP session starts in whatever the operator's `permissions.defaultMode` setting says. On the machine this was measured on that is `auto`, a model classifier that approves without ever raising `session/request_permission`. Relying on the inherited mode means having no gate at all.
- The agent advertises its modes on `session/new`: `available=[auto default acceptEdits plan dontAsk bypassPermissions]`.
- A permission option may carry a mode change in its `_meta`, for example
  `{"permission":{"changes":[{"description":"Set Claude Code permission mode to acceptEdits","lifetime":{"scope":"session"},"mode":"acceptEdits","operation":"set"}]}}`.
  Selecting such an option does not approve one call, it widens the rest of the session.
- The dashboard's existing approval flow is asynchronous: a request row is created, a human resolves it later, and the agent is respawned. ACP blocks the agent instead, so the answer has to come back on the same call.

---

### Task 1: Force the session's permission mode

**Files:**
- Create: `server/internal/acp/mode.go`
- Test: `server/internal/acp/mode_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `ModeSetter` interface with `SetSessionMode(ctx context.Context, params sdkacp.SetSessionModeRequest) (sdkacp.SetSessionModeResponse, error)`; `EnsureMode(ctx context.Context, s ModeSetter, sessionID sdkacp.SessionId, state *sdkacp.SessionModeState, want sdkacp.SessionModeId) error`; the exported constant `ModeGated`.

- [ ] **Step 1: Create the branch**

```bash
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
git checkout feat/acp-client && git pull --ff-only
git checkout -b feat/acp-permission-wiring
ls server/internal/acp/   # must list client.go and connection_test.go
```

If that directory is missing you branched from the wrong base — stop and re-cut from `feat/acp-client`.

- [ ] **Step 2: Write the failing tests**

Create `server/internal/acp/mode_test.go`:

```go
package acp

import (
	"context"
	"errors"
	"testing"

	sdkacp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
)

type fakeModeSetter struct {
	got  []sdkacp.SessionModeId
	err  error
}

func (f *fakeModeSetter) SetSessionMode(ctx context.Context, p sdkacp.SetSessionModeRequest) (sdkacp.SetSessionModeResponse, error) {
	f.got = append(f.got, p.ModeId)
	return sdkacp.SetSessionModeResponse{}, f.err
}

func modeState(current string, available ...string) *sdkacp.SessionModeState {
	st := &sdkacp.SessionModeState{CurrentModeId: sdkacp.SessionModeId(current)}
	for _, a := range available {
		st.AvailableModes = append(st.AvailableModes, sdkacp.SessionMode{Id: sdkacp.SessionModeId(a), Name: a})
	}
	return st
}

func TestEnsureModeSetsTheRequestedMode(t *testing.T) {
	f := &fakeModeSetter{}
	err := EnsureMode(context.Background(), f, "sess-1", modeState("auto", "auto", "default"), ModeGated)
	require.NoError(t, err)
	require.Equal(t, []sdkacp.SessionModeId{ModeGated}, f.got)
}

func TestEnsureModeFailsWhenTheAgentCannotOfferIt(t *testing.T) {
	f := &fakeModeSetter{}
	err := EnsureMode(context.Background(), f, "sess-1", modeState("auto", "auto", "bypassPermissions"), ModeGated)
	require.Error(t, err)
	require.Empty(t, f.got, "must not ask for a mode the agent did not advertise")
}

func TestEnsureModeFailsWhenTheAgentRejectsTheChange(t *testing.T) {
	f := &fakeModeSetter{err: errors.New("refused")}
	err := EnsureMode(context.Background(), f, "sess-1", modeState("auto", "auto", "default"), ModeGated)
	require.Error(t, err)
}

func TestEnsureModeFailsOnMissingModeState(t *testing.T) {
	f := &fakeModeSetter{}
	err := EnsureMode(context.Background(), f, "sess-1", nil, ModeGated)
	require.Error(t, err, "an agent that advertises no modes cannot be gated")
	require.Empty(t, f.got)
}

func TestEnsureModeStillSetsWhenCurrentModeAlreadyMatches(t *testing.T) {
	f := &fakeModeSetter{}
	err := EnsureMode(context.Background(), f, "sess-1", modeState("default", "auto", "default"), ModeGated)
	require.NoError(t, err)
	require.Equal(t, []sdkacp.SessionModeId{ModeGated}, f.got, "the advertised current mode is a claim, not a guarantee")
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd server && go test ./internal/acp/... -run TestEnsureMode`
Expected: FAIL — `undefined: EnsureMode`.

- [ ] **Step 4: Write the implementation**

Create `server/internal/acp/mode.go`:

```go
package acp

import (
	"context"
	"fmt"

	sdkacp "github.com/coder/acp-go-sdk"
)

// ModeGated is the only session mode under which the agent asks before acting.
// The other modes either approve silently or refuse without asking.
const ModeGated sdkacp.SessionModeId = "default"

// ModeSetter is the part of the ACP connection EnsureMode needs.
type ModeSetter interface {
	SetSessionMode(ctx context.Context, params sdkacp.SetSessionModeRequest) (sdkacp.SetSessionModeResponse, error)
}

// EnsureMode pins a session to want. A session inherits its mode from the
// operator's settings, so an unpinned session may approve every tool call
// without ever reaching the gate.
func EnsureMode(ctx context.Context, s ModeSetter, sessionID sdkacp.SessionId, state *sdkacp.SessionModeState, want sdkacp.SessionModeId) error {
	if state == nil {
		return fmt.Errorf("acp: session %s advertises no modes, cannot pin to %q", sessionID, want)
	}
	offered := false
	for _, m := range state.AvailableModes {
		if m.Id == want {
			offered = true
			break
		}
	}
	if !offered {
		return fmt.Errorf("acp: session %s does not offer mode %q", sessionID, want)
	}
	if _, err := s.SetSessionMode(ctx, sdkacp.SetSessionModeRequest{SessionId: sessionID, ModeId: want}); err != nil {
		return fmt.Errorf("acp: pinning session %s to %q: %w", sessionID, want, err)
	}
	return nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd server && go test -race ./internal/acp/... && go vet ./...`
Expected: PASS, vet clean.

- [ ] **Step 6: Commit**

```bash
git add server/internal/acp/
git commit -S -m "feat(acp): pin a session to the gated permission mode"
```

---

### Task 2: Refuse permission options that widen the session

**Files:**
- Modify: `server/internal/acp/client.go`
- Test: `server/internal/acp/client_test.go`

**Interfaces:**
- Consumes: `Client.RequestPermission` and its option search from the previous plan.
- Produces: an unexported `widensSession(o sdkacp.PermissionOption) bool`; `RequestPermission` skips any option for which it reports true, on both the allow and the deny side.

- [ ] **Step 1: Write the failing tests**

Append to `server/internal/acp/client_test.go`:

```go
func widening(id string, kind sdkacp.PermissionOptionKind) sdkacp.PermissionOption {
	return sdkacp.PermissionOption{
		Kind: kind, Name: "Allow and don't ask again", OptionId: sdkacp.PermissionOptionId(id),
		Meta: map[string]any{"permission": map[string]any{"changes": []any{
			map[string]any{"operation": "set", "mode": "acceptEdits",
				"lifetime": map[string]any{"scope": "session"}},
		}}},
	}
}

func TestRequestPermissionSkipsAWideningAllowOption(t *testing.T) {
	c := &Client{OnPermission: func(context.Context, PermissionRequest) (PermissionDecision, error) {
		return DecisionAllow, nil
	}}

	resp, err := c.RequestPermission(context.Background(), sdkacp.RequestPermissionRequest{
		SessionId: "sess-1",
		Options: []sdkacp.PermissionOption{
			widening("wide", sdkacp.PermissionOptionKindAllowOnce),
			{Kind: sdkacp.PermissionOptionKindAllowOnce, Name: "Allow Once", OptionId: "narrow"},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp.Outcome.Selected)
	require.Equal(t, sdkacp.PermissionOptionId("narrow"), resp.Outcome.Selected.OptionId)
}

func TestRequestPermissionCancelsWhenOnlyWideningOptionsRemain(t *testing.T) {
	c := &Client{OnPermission: func(context.Context, PermissionRequest) (PermissionDecision, error) {
		return DecisionAllow, nil
	}}

	resp, err := c.RequestPermission(context.Background(), sdkacp.RequestPermissionRequest{
		SessionId: "sess-1",
		Options:   []sdkacp.PermissionOption{widening("wide", sdkacp.PermissionOptionKindAllowOnce)},
	})

	require.NoError(t, err)
	require.Nil(t, resp.Outcome.Selected)
	require.NotNil(t, resp.Outcome.Cancelled)
}

func TestRequestPermissionSkipsAWideningDenyOption(t *testing.T) {
	c := &Client{}

	resp, err := c.RequestPermission(context.Background(), sdkacp.RequestPermissionRequest{
		SessionId: "sess-1",
		Options: []sdkacp.PermissionOption{
			widening("wide", sdkacp.PermissionOptionKindRejectOnce),
			{Kind: sdkacp.PermissionOptionKindRejectOnce, Name: "Deny", OptionId: "narrow"},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp.Outcome.Selected)
	require.Equal(t, sdkacp.PermissionOptionId("narrow"), resp.Outcome.Selected.OptionId)
}

func TestRequestPermissionKeepsOptionsWithUnrelatedMeta(t *testing.T) {
	c := &Client{OnPermission: func(context.Context, PermissionRequest) (PermissionDecision, error) {
		return DecisionAllow, nil
	}}

	opt := sdkacp.PermissionOption{
		Kind: sdkacp.PermissionOptionKindAllowOnce, Name: "Allow Once", OptionId: "narrow",
		Meta: map[string]any{"somethingElse": true},
	}
	resp, err := c.RequestPermission(context.Background(), sdkacp.RequestPermissionRequest{
		SessionId: "sess-1", Options: []sdkacp.PermissionOption{opt},
	})

	require.NoError(t, err)
	require.NotNil(t, resp.Outcome.Selected)
	require.Equal(t, sdkacp.PermissionOptionId("narrow"), resp.Outcome.Selected.OptionId)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd server && go test ./internal/acp/... -run TestRequestPermission`
Expected: FAIL — the widening option is selected because nothing filters it yet.

- [ ] **Step 3: Write the implementation**

In `server/internal/acp/client.go`, add:

```go
// An option may carry a mode change in its _meta, which grants for the rest of
// the session rather than for this call.
func widensSession(o sdkacp.PermissionOption) bool {
	perm, ok := o.Meta["permission"].(map[string]any)
	if !ok {
		return false
	}
	changes, ok := perm["changes"].([]any)
	if !ok {
		return false
	}
	return len(changes) > 0
}
```

`RequestPermission` already searches with a nested loop over `wantKinds` — the deny path is `[reject_once, reject_always]`, the allow path is `[allow_once]` only. Add one condition to the inner loop and change nothing else:

```go
	for _, want := range wantKinds {
		for _, o := range params.Options {
			if o.Kind == want && !widensSession(o) {
				return sdkacp.RequestPermissionResponse{Outcome: sdkacp.RequestPermissionOutcome{
					Selected: &sdkacp.RequestPermissionOutcomeSelected{OptionId: o.OptionId},
				}}, nil
			}
		}
	}
```

Keep the `wantKinds` construction, its comment, and the `Cancelled` fallback exactly as they are.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd server && go test -race ./internal/acp/... && go vet ./...`
Expected: PASS, vet clean. The tests from the previous plan must still pass unchanged.

- [ ] **Step 5: Commit**

```bash
git add server/internal/acp/
git commit -S -m "feat(acp): refuse permission options that widen the session mode"
```

---

### Task 3: Answer synchronously from an asynchronous approval flow

**Files:**
- Create: `server/internal/acp/gate.go`
- Test: `server/internal/acp/gate_test.go`

**Interfaces:**
- Consumes: `PermissionRequest` and `PermissionDecision` from the previous plan.
- Produces: `type RequestStatus int` with `StatusPending`, `StatusGranted`, `StatusDenied`; `type PollingGate struct` with fields `File func(context.Context, PermissionRequest) (string, error)`, `Status func(context.Context, string) (RequestStatus, error)`, `Interval time.Duration`, `Timeout time.Duration`, and `Now func() time.Time`; method `Decide(ctx context.Context, req PermissionRequest) (PermissionDecision, error)` whose signature matches `Client.OnPermission`.

The dashboard files a permission request and a human resolves it later; ACP blocks the agent until an answer comes back. `PollingGate` bridges the two. Binding `File` and `Status` to the repository happens in a later step, not here.

- [ ] **Step 1: Write the failing tests**

Create `server/internal/acp/gate_test.go`:

```go
package acp

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func gateFor(t *testing.T, status func(context.Context, string) (RequestStatus, error)) *PollingGate {
	t.Helper()
	return &PollingGate{
		File:     func(context.Context, PermissionRequest) (string, error) { return "req-1", nil },
		Status:   status,
		Interval: time.Millisecond,
		Timeout:  50 * time.Millisecond,
	}
}

func TestPollingGateAllowsOnceGranted(t *testing.T) {
	var calls atomic.Int32
	g := gateFor(t, func(context.Context, string) (RequestStatus, error) {
		if calls.Add(1) < 3 {
			return StatusPending, nil
		}
		return StatusGranted, nil
	})

	d, err := g.Decide(context.Background(), PermissionRequest{SessionID: "s", ToolCallID: "t"})
	require.NoError(t, err)
	require.Equal(t, DecisionAllow, d)
}

func TestPollingGateDeniesWhenDenied(t *testing.T) {
	g := gateFor(t, func(context.Context, string) (RequestStatus, error) { return StatusDenied, nil })

	d, err := g.Decide(context.Background(), PermissionRequest{})
	require.NoError(t, err)
	require.Equal(t, DecisionDeny, d)
}

func TestPollingGateDeniesOnTimeout(t *testing.T) {
	g := gateFor(t, func(context.Context, string) (RequestStatus, error) { return StatusPending, nil })

	d, err := g.Decide(context.Background(), PermissionRequest{})
	require.Error(t, err, "a timeout is an error, and the decision is still deny")
	require.Equal(t, DecisionDeny, d)
}

func TestPollingGateDeniesOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g := gateFor(t, func(context.Context, string) (RequestStatus, error) { return StatusPending, nil })

	d, err := g.Decide(ctx, PermissionRequest{})
	require.Error(t, err)
	require.Equal(t, DecisionDeny, d)
}

func TestPollingGateDeniesWhenFilingFails(t *testing.T) {
	g := gateFor(t, func(context.Context, string) (RequestStatus, error) { return StatusGranted, nil })
	g.File = func(context.Context, PermissionRequest) (string, error) { return "", errors.New("db down") }

	d, err := g.Decide(context.Background(), PermissionRequest{})
	require.Error(t, err)
	require.Equal(t, DecisionDeny, d)
}

func TestPollingGateDeniesWhenStatusKeepsErroring(t *testing.T) {
	g := gateFor(t, func(context.Context, string) (RequestStatus, error) { return StatusPending, errors.New("read failed") })

	d, err := g.Decide(context.Background(), PermissionRequest{})
	require.Error(t, err)
	require.Equal(t, DecisionDeny, d)
}

func TestPollingGateSurvivesATransientStatusError(t *testing.T) {
	var calls atomic.Int32
	g := gateFor(t, func(context.Context, string) (RequestStatus, error) {
		if calls.Add(1) == 1 {
			return StatusPending, errors.New("transient")
		}
		return StatusGranted, nil
	})

	d, err := g.Decide(context.Background(), PermissionRequest{})
	require.NoError(t, err)
	require.Equal(t, DecisionAllow, d)
}

func TestPollingGateWithoutFileDenies(t *testing.T) {
	g := &PollingGate{}
	d, err := g.Decide(context.Background(), PermissionRequest{})
	require.Error(t, err)
	require.Equal(t, DecisionDeny, d)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd server && go test ./internal/acp/... -run TestPollingGate`
Expected: FAIL — `undefined: PollingGate`.

- [ ] **Step 3: Write the implementation**

Create `server/internal/acp/gate.go`:

```go
package acp

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// RequestStatus is the lifecycle of a filed permission request.
type RequestStatus int

const (
	StatusPending RequestStatus = iota
	StatusGranted
	StatusDenied
)

const (
	defaultPollInterval = 500 * time.Millisecond
	defaultPollTimeout  = 30 * time.Minute
)

// PollingGate answers an ACP permission request from the dashboard's
// asynchronous approval flow: it files the request, then blocks until someone
// resolves it. Every failure path returns DecisionDeny.
type PollingGate struct {
	File     func(ctx context.Context, req PermissionRequest) (string, error)
	Status   func(ctx context.Context, id string) (RequestStatus, error)
	Interval time.Duration
	Timeout  time.Duration
	Now      func() time.Time
}

// Decide satisfies Client.OnPermission.
func (g *PollingGate) Decide(ctx context.Context, req PermissionRequest) (PermissionDecision, error) {
	if g.File == nil || g.Status == nil {
		return DecisionDeny, errors.New("acp: gate is not wired, denying")
	}

	interval := g.Interval
	if interval <= 0 {
		interval = defaultPollInterval
	}
	timeout := g.Timeout
	if timeout <= 0 {
		timeout = defaultPollTimeout
	}

	id, err := g.File(ctx, req)
	if err != nil {
		return DecisionDeny, fmt.Errorf("acp: filing permission request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastErr error
	for {
		switch st, sErr := g.Status(ctx, id); {
		case sErr != nil:
			// A transient read must not decide anything; keep waiting.
			lastErr = sErr
		case st == StatusGranted:
			return DecisionAllow, nil
		case st == StatusDenied:
			return DecisionDeny, nil
		}

		select {
		case <-ctx.Done():
			if lastErr != nil {
				return DecisionDeny, fmt.Errorf("acp: permission request %s unresolved: %w", id, lastErr)
			}
			return DecisionDeny, fmt.Errorf("acp: permission request %s unresolved: %w", id, ctx.Err())
		case <-ticker.C:
		}
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd server && go test -race ./internal/acp/... -run TestPollingGate -v`
Expected: PASS for all eight tests, no race.

- [ ] **Step 5: Run the full gate**

```bash
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
task test
task lint
git checkout -- server/internal/db/ent/
git status --short
```

Expected: both green; `git status` shows only the intended files.

- [ ] **Step 6: Commit and push**

```bash
git add server/internal/acp/
git commit -S -m "feat(acp): answer permission requests synchronously from the approval flow"
git push -u origin feat/acp-permission-wiring
```

---

## Done when

- `EnsureMode` refuses to proceed when the wanted mode is not advertised, and pins it otherwise.
- No permission option carrying a `_meta.permission.changes` entry can be selected, on either the allow or the deny side.
- `PollingGate.Decide` returns `DecisionDeny` on every failure path: unwired, filing error, persistent status error, timeout, and cancelled context.
- `task test` and `task lint` are green with the raw output pasted into the PR.
- No test spawns a process, opens a database, touches the network, or sleeps for a fixed duration.
- Nothing outside `server/internal/acp/` changed.

## Deliberately not in this plan

Binding `PollingGate.File` and `PollingGate.Status` to the repository, calling `EnsureMode` from a real spawn path, and surfacing any of it in the UI. Those need the ACP session to be created by dashboard code, which is step 3 of `docs/local/spec-acp-client.md`.
