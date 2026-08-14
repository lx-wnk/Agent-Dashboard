# ACP Client Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `server/internal/acp` package that speaks the Agent Client Protocol as a client, so the dashboard can later drive agents over ACP instead of scraping a terminal.

**Architecture:** One type, `Client`, implements the SDK's `acp.Client` interface. It owns no transport and no process: it translates protocol callbacks into two function fields the caller supplies — one for streamed session updates, one for permission decisions. Filesystem and terminal methods refuse, because the dashboard does not lend those capabilities to an agent. Nothing in this plan is wired into the pipeline, the API, or the UI; step 1 of the spec ships no user-visible surface.

**Tech Stack:** Go 1.26, `github.com/coder/acp-go-sdk` (via a `replace` onto our fork), `stretchr/testify` for assertions.

## Global Constraints

- Go 1.26; the `server` module is `github.com/lx-wnk/agent-dashboard/server`.
- Dependency is `github.com/coder/acp-go-sdk v0.13.5` with `replace github.com/coder/acp-go-sdk => github.com/lx-wnk/acp-go-sdk v1.20.0-lxwnk.alpha.1`.
- The `replace` goes in `server/go.mod` **only**. Never in `sdk/go.mod` — that module is published and a `replace` there breaks external consumers.
- The SDK's package name is `acp` and so is ours. Import the SDK as `sdkacp "github.com/coder/acp-go-sdk"` in every file.
- Assertions use `stretchr/testify` (`require`), matching the rest of the `server` module.
- Tests must not invoke `npx`, spawn a process, or touch the network.
- Work on branch `feat/acp-client`. Never commit to `main`.
- Gate after every task: `cd server && go vet ./...` and `go test ./internal/acp/...`. Full gate before the final commit: `task test`.
- `task test` and `go test ./...` regenerate `server/internal/db/ent/`. Run `git checkout -- server/internal/db/ent/` before committing unless the regeneration is the change.
- After the dependency change, run `go mod tidy -diff` in every module and confirm the dependency count in `THIRD_PARTY_LICENSES.md` did not shrink. `scripts/gen-licenses.sh` drops whole sections silently when a module is untidy.

---

### Task 1: Dependency and interface conformance

**Files:**
- Modify: `server/go.mod`
- Create: `server/internal/acp/client.go`
- Test: `server/internal/acp/client_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `type Client struct` with exported fields `OnEvent func(Event)` and `OnPermission func(context.Context, PermissionRequest) (PermissionDecision, error)`; `type Event struct`; `type PermissionRequest struct`; `type PermissionDecision int` with constants `DecisionAllow` and `DecisionDeny`. Later tasks fill the method bodies.

- [ ] **Step 1: Create the branch**

```bash
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
git checkout -b feat/acp-client
```

- [ ] **Step 2: Add the dependency and the replace**

```bash
cd server
GOFLAGS=-mod=mod go get github.com/coder/acp-go-sdk@v0.13.5
go mod edit -replace github.com/coder/acp-go-sdk=github.com/lx-wnk/acp-go-sdk@v1.20.0-lxwnk.alpha.1
GOFLAGS=-mod=mod go mod tidy
grep -E 'acp-go-sdk' go.mod
```

Expected: a `require` line for `github.com/coder/acp-go-sdk v0.13.5` and a `replace` line pointing at `github.com/lx-wnk/acp-go-sdk v1.20.0-lxwnk.alpha.1`.

- [ ] **Step 3: Write the failing conformance test**

Create `server/internal/acp/client_test.go`:

```go
package acp

import (
	"testing"

	sdkacp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
)

func TestClientSatisfiesSDKInterface(t *testing.T) {
	var c any = &Client{}
	_, ok := c.(sdkacp.Client)
	require.True(t, ok, "Client must implement sdkacp.Client")
}
```

- [ ] **Step 4: Run the test to verify it fails**

Run: `cd server && go test ./internal/acp/...`
Expected: FAIL — `undefined: Client`.

- [ ] **Step 5: Write the minimal implementation**

Create `server/internal/acp/client.go`:

```go
// Package acp adapts the Agent Client Protocol to the dashboard. It implements
// the client half: the agent runs as a separate process and calls into us.
package acp

import (
	"context"
	"errors"

	sdkacp "github.com/coder/acp-go-sdk"
)

// PermissionDecision is the caller's answer to a permission request.
type PermissionDecision int

const (
	// DecisionDeny is the zero value so an unset or failed decision blocks.
	DecisionDeny PermissionDecision = iota
	DecisionAllow
)

// PermissionRequest describes a tool call the agent wants to run.
type PermissionRequest struct {
	SessionID string
	ToolCallID string
	Title      string
}

// Event is a normalized session update.
type Event struct {
	SessionID string
	Kind      string
	Text      string
	ToolCallID string
	Status     string
}

// Client implements sdkacp.Client. Both callbacks may be nil.
type Client struct {
	OnEvent      func(Event)
	OnPermission func(context.Context, PermissionRequest) (PermissionDecision, error)
}

var _ sdkacp.Client = (*Client)(nil)

var errUnsupported = errors.New("acp: capability not offered by this client")

func (c *Client) SessionUpdate(ctx context.Context, params sdkacp.SessionNotification) error {
	return nil
}

func (c *Client) RequestPermission(ctx context.Context, params sdkacp.RequestPermissionRequest) (sdkacp.RequestPermissionResponse, error) {
	return sdkacp.RequestPermissionResponse{}, errUnsupported
}

func (c *Client) ReadTextFile(ctx context.Context, params sdkacp.ReadTextFileRequest) (sdkacp.ReadTextFileResponse, error) {
	return sdkacp.ReadTextFileResponse{}, errUnsupported
}

func (c *Client) WriteTextFile(ctx context.Context, params sdkacp.WriteTextFileRequest) (sdkacp.WriteTextFileResponse, error) {
	return sdkacp.WriteTextFileResponse{}, errUnsupported
}

func (c *Client) CreateTerminal(ctx context.Context, params sdkacp.CreateTerminalRequest) (sdkacp.CreateTerminalResponse, error) {
	return sdkacp.CreateTerminalResponse{}, errUnsupported
}

func (c *Client) KillTerminal(ctx context.Context, params sdkacp.KillTerminalRequest) (sdkacp.KillTerminalResponse, error) {
	return sdkacp.KillTerminalResponse{}, errUnsupported
}

func (c *Client) TerminalOutput(ctx context.Context, params sdkacp.TerminalOutputRequest) (sdkacp.TerminalOutputResponse, error) {
	return sdkacp.TerminalOutputResponse{}, errUnsupported
}

func (c *Client) ReleaseTerminal(ctx context.Context, params sdkacp.ReleaseTerminalRequest) (sdkacp.ReleaseTerminalResponse, error) {
	return sdkacp.ReleaseTerminalResponse{}, errUnsupported
}

func (c *Client) WaitForTerminalExit(ctx context.Context, params sdkacp.WaitForTerminalExitRequest) (sdkacp.WaitForTerminalExitResponse, error) {
	return sdkacp.WaitForTerminalExitResponse{}, errUnsupported
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `cd server && go test ./internal/acp/... && go vet ./...`
Expected: PASS, vet clean.

- [ ] **Step 7: Check the license table did not shrink**

```bash
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
for m in sdk server desktop plugins/oauthkit; do (cd "$m" && go mod tidy -diff >/dev/null && echo "$m tidy"); done
grep -c '^|' THIRD_PARTY_LICENSES.md
```

Expected: every module reports tidy. Record the row count; it must not be lower after regenerating licenses.

- [ ] **Step 8: Commit**

```bash
git add server/go.mod server/go.sum server/internal/acp/
git commit -S -m "feat(acp): add an ACP client that satisfies the SDK interface"
```

---

### Task 2: Translate session updates into events

**Files:**
- Modify: `server/internal/acp/client.go`
- Test: `server/internal/acp/client_test.go`

**Interfaces:**
- Consumes: `Client`, `Event` from Task 1.
- Produces: `SessionUpdate` populates `Event` and calls `OnEvent` once per update. `Event.Kind` is one of `"agent_message"`, `"agent_thought"`, `"tool_call"`, `"tool_call_update"`, `"plan"`, or `"other"`.

- [ ] **Step 1: Write the failing tests**

Append to `server/internal/acp/client_test.go`:

```go
func TestSessionUpdateEmitsAgentMessage(t *testing.T) {
	var got []Event
	c := &Client{OnEvent: func(e Event) { got = append(got, e) }}

	err := c.SessionUpdate(context.Background(), sdkacp.SessionNotification{
		SessionId: "sess-1",
		Update: sdkacp.SessionUpdate{
			AgentMessageChunk: &sdkacp.SessionUpdateAgentMessageChunk{
				Content: sdkacp.TextBlock("hello"),
			},
		},
	})

	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "sess-1", got[0].SessionID)
	require.Equal(t, "agent_message", got[0].Kind)
	require.Equal(t, "hello", got[0].Text)
}

func TestSessionUpdateEmitsToolCall(t *testing.T) {
	var got []Event
	c := &Client{OnEvent: func(e Event) { got = append(got, e) }}

	err := c.SessionUpdate(context.Background(), sdkacp.SessionNotification{
		SessionId: "sess-1",
		Update: sdkacp.SessionUpdate{
			ToolCall: &sdkacp.SessionUpdateToolCall{
				ToolCallId: "tc-1",
				Title:      "Write file",
			},
		},
	})

	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "tool_call", got[0].Kind)
	require.Equal(t, "tc-1", got[0].ToolCallID)
	require.Equal(t, "Write file", got[0].Text)
}

func TestSessionUpdateWithoutCallbackDoesNotPanic(t *testing.T) {
	c := &Client{}
	err := c.SessionUpdate(context.Background(), sdkacp.SessionNotification{
		SessionId: "sess-1",
		Update: sdkacp.SessionUpdate{
			AgentMessageChunk: &sdkacp.SessionUpdateAgentMessageChunk{
				Content: sdkacp.TextBlock("hello"),
			},
		},
	})
	require.NoError(t, err)
}
```

Add `"context"` to the test file's imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd server && go test ./internal/acp/... -run TestSessionUpdate`
Expected: FAIL — no events recorded, `Len(got, 1)` fails.

- [ ] **Step 3: Implement the translation**

Replace the `SessionUpdate` body in `server/internal/acp/client.go`:

```go
func (c *Client) SessionUpdate(ctx context.Context, params sdkacp.SessionNotification) error {
	if c.OnEvent == nil {
		return nil
	}
	e := Event{SessionID: string(params.SessionId), Kind: "other"}
	switch u := params.Update; {
	case u.AgentMessageChunk != nil:
		e.Kind = "agent_message"
		if t := u.AgentMessageChunk.Content.Text; t != nil {
			e.Text = t.Text
		}
	case u.AgentThoughtChunk != nil:
		e.Kind = "agent_thought"
		if t := u.AgentThoughtChunk.Content.Text; t != nil {
			e.Text = t.Text
		}
	case u.ToolCall != nil:
		e.Kind = "tool_call"
		e.ToolCallID = string(u.ToolCall.ToolCallId)
		e.Text = u.ToolCall.Title
		e.Status = string(u.ToolCall.Status)
	case u.ToolCallUpdate != nil:
		e.Kind = "tool_call_update"
		e.ToolCallID = string(u.ToolCallUpdate.ToolCallId)
		if s := u.ToolCallUpdate.Status; s != nil {
			e.Status = string(*s)
		}
	case u.Plan != nil:
		e.Kind = "plan"
	}
	c.OnEvent(e)
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd server && go test ./internal/acp/... && go vet ./...`
Expected: PASS, vet clean. If a field name or type does not compile, read the exact definition with `go doc github.com/coder/acp-go-sdk.SessionUpdateToolCall` and adjust — the generated names are authoritative, this plan is not.

- [ ] **Step 5: Commit**

```bash
git add server/internal/acp/
git commit -S -m "feat(acp): translate session updates into dashboard events"
```

---

### Task 3: Route permission requests through the callback

**Files:**
- Modify: `server/internal/acp/client.go`
- Test: `server/internal/acp/client_test.go`

**Interfaces:**
- Consumes: `Client`, `PermissionRequest`, `PermissionDecision` from Task 1.
- Produces: `RequestPermission` calls `OnPermission` and maps the answer onto an SDK outcome. With no callback, or on callback error, the request is denied.

- [ ] **Step 1: Write the failing tests**

Append to `server/internal/acp/client_test.go`:

```go
func permissionOptions() []sdkacp.PermissionOption {
	return []sdkacp.PermissionOption{
		{Kind: sdkacp.PermissionOptionKindAllowOnce, Name: "Allow", OptionId: "allow"},
		{Kind: sdkacp.PermissionOptionKindRejectOnce, Name: "Reject", OptionId: "reject"},
	}
}

func TestRequestPermissionAllowSelectsAllowOption(t *testing.T) {
	c := &Client{OnPermission: func(context.Context, PermissionRequest) (PermissionDecision, error) {
		return DecisionAllow, nil
	}}

	resp, err := c.RequestPermission(context.Background(), sdkacp.RequestPermissionRequest{
		SessionId: "sess-1",
		Options:   permissionOptions(),
		ToolCall:  sdkacp.ToolCallUpdate{ToolCallId: "tc-1"},
	})

	require.NoError(t, err)
	require.NotNil(t, resp.Outcome.Selected)
	require.Equal(t, sdkacp.PermissionOptionId("allow"), resp.Outcome.Selected.OptionId)
}

func TestRequestPermissionWithoutCallbackDenies(t *testing.T) {
	c := &Client{}

	resp, err := c.RequestPermission(context.Background(), sdkacp.RequestPermissionRequest{
		SessionId: "sess-1",
		Options:   permissionOptions(),
	})

	require.NoError(t, err)
	require.NotNil(t, resp.Outcome.Selected)
	require.Equal(t, sdkacp.PermissionOptionId("reject"), resp.Outcome.Selected.OptionId)
}

func TestRequestPermissionCallbackErrorDenies(t *testing.T) {
	c := &Client{OnPermission: func(context.Context, PermissionRequest) (PermissionDecision, error) {
		return DecisionAllow, errors.New("gate unreachable")
	}}

	resp, err := c.RequestPermission(context.Background(), sdkacp.RequestPermissionRequest{
		SessionId: "sess-1",
		Options:   permissionOptions(),
	})

	require.NoError(t, err)
	require.NotNil(t, resp.Outcome.Selected)
	require.Equal(t, sdkacp.PermissionOptionId("reject"), resp.Outcome.Selected.OptionId)
}

func TestRequestPermissionWithoutRejectOptionCancels(t *testing.T) {
	c := &Client{}

	resp, err := c.RequestPermission(context.Background(), sdkacp.RequestPermissionRequest{
		SessionId: "sess-1",
		Options:   []sdkacp.PermissionOption{{Kind: sdkacp.PermissionOptionKindAllowOnce, Name: "Allow", OptionId: "allow"}},
	})

	require.NoError(t, err)
	require.Nil(t, resp.Outcome.Selected)
	require.NotNil(t, resp.Outcome.Cancelled)
}
```

Add `"errors"` to the test file's imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd server && go test ./internal/acp/... -run TestRequestPermission`
Expected: FAIL — `RequestPermission` returns `errUnsupported`.

- [ ] **Step 3: Implement the routing**

Replace the `RequestPermission` body in `server/internal/acp/client.go`:

```go
func (c *Client) RequestPermission(ctx context.Context, params sdkacp.RequestPermissionRequest) (sdkacp.RequestPermissionResponse, error) {
	decision := DecisionDeny
	if c.OnPermission != nil {
		req := PermissionRequest{
			SessionID:  string(params.SessionId),
			ToolCallID: string(params.ToolCall.ToolCallId),
		}
		if t := params.ToolCall.Title; t != nil {
			req.Title = *t
		}
		// An unreachable gate must not widen access.
		if d, err := c.OnPermission(ctx, req); err == nil {
			decision = d
		}
	}

	want := sdkacp.PermissionOptionKindRejectOnce
	if decision == DecisionAllow {
		want = sdkacp.PermissionOptionKindAllowOnce
	}
	for _, o := range params.Options {
		if o.Kind == want {
			return sdkacp.RequestPermissionResponse{Outcome: sdkacp.RequestPermissionOutcome{
				Selected: &sdkacp.RequestPermissionOutcomeSelected{OptionId: o.OptionId},
			}}, nil
		}
	}
	return sdkacp.RequestPermissionResponse{Outcome: sdkacp.RequestPermissionOutcome{
		Cancelled: &sdkacp.RequestPermissionOutcomeCancelled{},
	}}, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd server && go test ./internal/acp/... && go vet ./...`
Expected: PASS, vet clean.

- [ ] **Step 5: Commit**

```bash
git add server/internal/acp/
git commit -S -m "feat(acp): deny permission requests unless the gate allows them"
```

---

### Task 4: Prove the client over a real connection

**Files:**
- Create: `server/internal/acp/connection_test.go`

**Interfaces:**
- Consumes: `Client`, `Event` from Tasks 1-2.
- Produces: nothing. This task adds coverage only.

- [ ] **Step 1: Write the failing test**

Create `server/internal/acp/connection_test.go`:

```go
package acp

import (
	"context"
	"io"
	"testing"
	"time"

	sdkacp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
)

// agentDouble is the peer half of the connection. sdkacp.Agent requires all
// twelve methods below; only Initialize, NewSession and Prompt do anything.
type agentDouble struct {
	conn *sdkacp.AgentSideConnection
}

func (a *agentDouble) Authenticate(ctx context.Context, p sdkacp.AuthenticateRequest) (sdkacp.AuthenticateResponse, error) {
	return sdkacp.AuthenticateResponse{}, nil
}

func (a *agentDouble) Logout(ctx context.Context, p sdkacp.LogoutRequest) (sdkacp.LogoutResponse, error) {
	return sdkacp.LogoutResponse{}, nil
}

func (a *agentDouble) Cancel(ctx context.Context, p sdkacp.CancelNotification) error { return nil }

func (a *agentDouble) CloseSession(ctx context.Context, p sdkacp.CloseSessionRequest) (sdkacp.CloseSessionResponse, error) {
	return sdkacp.CloseSessionResponse{}, nil
}

func (a *agentDouble) DeleteSession(ctx context.Context, p sdkacp.DeleteSessionRequest) (sdkacp.DeleteSessionResponse, error) {
	return sdkacp.DeleteSessionResponse{}, nil
}

func (a *agentDouble) ListSessions(ctx context.Context, p sdkacp.ListSessionsRequest) (sdkacp.ListSessionsResponse, error) {
	return sdkacp.ListSessionsResponse{}, nil
}

func (a *agentDouble) ResumeSession(ctx context.Context, p sdkacp.ResumeSessionRequest) (sdkacp.ResumeSessionResponse, error) {
	return sdkacp.ResumeSessionResponse{}, nil
}

func (a *agentDouble) SetSessionConfigOption(ctx context.Context, p sdkacp.SetSessionConfigOptionRequest) (sdkacp.SetSessionConfigOptionResponse, error) {
	return sdkacp.SetSessionConfigOptionResponse{}, nil
}

func (a *agentDouble) SetSessionMode(ctx context.Context, p sdkacp.SetSessionModeRequest) (sdkacp.SetSessionModeResponse, error) {
	return sdkacp.SetSessionModeResponse{}, nil
}

func (a *agentDouble) Initialize(ctx context.Context, p sdkacp.InitializeRequest) (sdkacp.InitializeResponse, error) {
	return sdkacp.InitializeResponse{ProtocolVersion: sdkacp.ProtocolVersionNumber}, nil
}

func (a *agentDouble) NewSession(ctx context.Context, p sdkacp.NewSessionRequest) (sdkacp.NewSessionResponse, error) {
	return sdkacp.NewSessionResponse{SessionId: "sess-1"}, nil
}

func (a *agentDouble) Prompt(ctx context.Context, p sdkacp.PromptRequest) (sdkacp.PromptResponse, error) {
	_ = a.conn.SessionUpdate(ctx, sdkacp.SessionNotification{
		SessionId: "sess-1",
		Update: sdkacp.SessionUpdate{
			AgentMessageChunk: &sdkacp.SessionUpdateAgentMessageChunk{
				Content: sdkacp.TextBlock("pong"),
			},
		},
	})
	return sdkacp.PromptResponse{StopReason: sdkacp.StopReasonEndTurn}, nil
}

func TestClientReceivesUpdatesOverAConnection(t *testing.T) {
	clientReads, agentWrites := io.Pipe()
	agentReads, clientWrites := io.Pipe()

	events := make(chan Event, 4)
	client := &Client{OnEvent: func(e Event) { events <- e }}
	conn := sdkacp.NewClientSideConnection(client, clientWrites, clientReads)

	agent := &agentDouble{}
	agent.conn = sdkacp.NewAgentSideConnection(agent, agentWrites, agentReads)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := conn.Initialize(ctx, sdkacp.InitializeRequest{ProtocolVersion: sdkacp.ProtocolVersionNumber})
	require.NoError(t, err)

	sess, err := conn.NewSession(ctx, sdkacp.NewSessionRequest{Cwd: t.TempDir(), McpServers: []sdkacp.McpServer{}})
	require.NoError(t, err)
	require.Equal(t, sdkacp.SessionId("sess-1"), sess.SessionId)

	_, err = conn.Prompt(ctx, sdkacp.PromptRequest{
		SessionId: sess.SessionId,
		Prompt:    []sdkacp.ContentBlock{sdkacp.TextBlock("ping")},
	})
	require.NoError(t, err)

	select {
	case e := <-events:
		require.Equal(t, "agent_message", e.Kind)
		require.Equal(t, "pong", e.Text)
	case <-ctx.Done():
		t.Fatal("no session update arrived")
	}
}
```

- [ ] **Step 2: Run the test**

Run: `cd server && go test ./internal/acp/... -run TestClientReceivesUpdates -v`
Expected: PASS. `sdkacp.Agent` requires exactly the twelve methods above at schema 1.20.0. If a future schema adds one, `go doc github.com/coder/acp-go-sdk.Agent` names it; add a stub returning a zero value and `nil`.

- [ ] **Step 3: Run with the race detector**

Run: `cd server && go test -race ./internal/acp/...`
Expected: PASS, no race reported.

- [ ] **Step 4: Run the full gate**

```bash
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
task test
task lint
git checkout -- server/internal/db/ent/
git status --short
```

Expected: both green; `git status` shows only the intended files.

- [ ] **Step 5: Commit and push**

```bash
git add server/internal/acp/
git commit -S -m "test(acp): cover the client over an in-memory connection"
git push -u origin feat/acp-client
```

---

## Done when

- `server/internal/acp` compiles, `var _ sdkacp.Client = (*Client)(nil)` holds, and every test passes under `-race`.
- `task test` and `task lint` are green with the raw output pasted into the PR.
- No process is spawned and no network call is made by any test.
- `THIRD_PARTY_LICENSES.md` still lists at least as many dependencies as before.
- Nothing outside `server/internal/acp`, `server/go.mod` and `server/go.sum` changed.
