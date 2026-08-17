# ACP Adapter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a pipeline stage run on an ACP agent by adding an `acp` LLM adapter, selectable per spawner row and visible in the adapter catalog the settings UI reads.

**Architecture:** The pipeline already has an in-process adapter seam. `stage_handlers.go` routes any spawner row whose `adapter_type` is not `claude` to `llmadapter.NewLLMSpawnerFromSpawner`, and the resulting `LLMSpawner` owns the whole stage: it runs the model, writes a synthetic JSONL session file, and returns. That is exactly the shape ACP needs, because an ACP client must hold the agent's stdio for the entire turn to answer permission requests. So this is a new `LLMSpawner` implementation plus one factory case and one catalog entry — the pipeline, the orchestrator and the completion detector are untouched.

**Tech Stack:** Go 1.26, `server/internal/acp` (the ACP client from the two previous plans), `github.com/coder/acp-go-sdk` via the fork replace, `stretchr/testify`.

## Global Constraints

- Go 1.26; the server module is `github.com/lx-wnk/agent-dashboard/server`.
- The ACP SDK is imported as `sdkacp "github.com/coder/acp-go-sdk"`; our own client package as `"github.com/lx-wnk/agent-dashboard/server/internal/acp"`.
- Assertions use `github.com/stretchr/testify/require`.
- Tests must not spawn a process, touch the network, open a database, or sleep for a fixed duration.
- Only files under `server/internal/llmadapter/` may change.
- Work on branch `feat/acp-adapter`, cut from **`feat/acp-permission-wiring`**, not from `main`. The `acp` package and its permission gate live on that branch, still open as PR #360 stacked on #358. Branching from `main` gives you a tree without them.
- Gate after every task: `cd server && go vet ./... && go test -race ./internal/llmadapter/...`. Full gate before the last commit: `task test` and `task lint` from the repository root.
- `task test` regenerates `server/internal/db/ent/`. Run `git checkout -- server/internal/db/ent/` before committing unless that regeneration is the change.

## Background the tasks assume

- `LLMSpawner` is `Name() string` and `Spawn(ctx, LLMSpawnArgs) (LLMSpawnResult, error)`, declared in `llm_spawner.go:44-49`.
- `LLMSpawnArgs` carries `TaskID`, `StageRunID`, `Stage`, `SystemPrompt`, `UserPrompt`, `Model`, `WorkDir`, `AllowedTools`.
- `LLMSpawnResult` carries `PID` (0 for non-subprocess adapters), `SessionID`, and `SessionFile` — the JSONL path the completion detector reads. A non-subprocess adapter must write that file itself.
- `writeSyntheticSession(workDir, stageRunID, content string) (string, error)` in `llm_ollama.go:100` already writes that file in the expected shape. Reuse it; do not write a second one.
- `NewLLMSpawnerFromSpawner` in `adapter_factory.go:21` switches on `s.AdapterType`. `AvailableAdapters` in `adapter_catalog.go` is the catalog both the factory and the settings UI read.
- The `acp` package provides: `Client` with `OnEvent func(Event)` and `OnPermission func(context.Context, PermissionRequest) (PermissionDecision, error)`; `EnsureMode(ctx, ModeSetter, sessionID, *sdkacp.SessionModeState, sdkacp.SessionModeId) error`; the constant `ModeGated`.

### The deliberate limitation in this increment

`OnPermission` is left nil. The gate built in the previous plan denies when unwired, so an ACP-run stage can do anything the agent's own allow-rules already permit and nothing else — every request that reaches the gate is refused. Binding the gate to the dashboard's approval flow needs a dashboard URL and token that `LLMSpawnArgs` does not carry, so it is the next increment, not this one. Until then the adapter is honest for read-mostly stages such as review, and useless for stages that need approvals. Say so in the catalog description so nobody discovers it by surprise.

---

### Task 1: The ACP spawner

**Files:**
- Create: `server/internal/llmadapter/llm_acp.go`
- Test: `server/internal/llmadapter/llm_acp_test.go`

**Interfaces:**
- Consumes: `LLMSpawner`, `LLMSpawnArgs`, `LLMSpawnResult`, `writeSyntheticSession` from the package; `acp.Client`, `acp.EnsureMode`, `acp.ModeGated` from `server/internal/acp`.
- Produces: `type ACPSpawner struct` with fields `Command string`, `Args []string`, and `Permission func(context.Context, acp.PermissionRequest) (acp.PermissionDecision, error)`; methods `Name() string` returning `"acp"` and `Spawn(ctx, LLMSpawnArgs) (LLMSpawnResult, error)`; the unexported seam `newConn func(client *acp.Client, in io.Writer, out io.Reader) acpConn` and the interface `acpConn`.

- [ ] **Step 1: Create the branch**

```bash
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
git checkout feat/acp-permission-wiring && git pull --ff-only
git checkout -b feat/acp-adapter
ls server/internal/acp/   # must list client.go, gate.go and mode.go
```

If that directory is missing you branched from the wrong base — stop and re-cut.

- [ ] **Step 2: Write the failing tests**

Create `server/internal/llmadapter/llm_acp_test.go`:

```go
package llmadapter

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	sdkacp "github.com/coder/acp-go-sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/acp"
	"github.com/stretchr/testify/require"
)

// fakeConn stands in for a live ACP connection so no process is started.
type fakeConn struct {
	client    *acp.Client
	modes     *sdkacp.SessionModeState
	setModes  []sdkacp.SessionModeId
	initErr   error
	newErr    error
	promptErr error
	reply     string
}

func (f *fakeConn) Initialize(ctx context.Context, p sdkacp.InitializeRequest) (sdkacp.InitializeResponse, error) {
	return sdkacp.InitializeResponse{ProtocolVersion: sdkacp.ProtocolVersionNumber}, f.initErr
}

func (f *fakeConn) NewSession(ctx context.Context, p sdkacp.NewSessionRequest) (sdkacp.NewSessionResponse, error) {
	return sdkacp.NewSessionResponse{SessionId: "sess-1", Modes: f.modes}, f.newErr
}

func (f *fakeConn) SetSessionMode(ctx context.Context, p sdkacp.SetSessionModeRequest) (sdkacp.SetSessionModeResponse, error) {
	f.setModes = append(f.setModes, p.ModeId)
	return sdkacp.SetSessionModeResponse{}, nil
}

func (f *fakeConn) Prompt(ctx context.Context, p sdkacp.PromptRequest) (sdkacp.PromptResponse, error) {
	if f.promptErr != nil {
		return sdkacp.PromptResponse{}, f.promptErr
	}
	_ = f.client.SessionUpdate(ctx, sdkacp.SessionNotification{
		SessionId: "sess-1",
		Update: sdkacp.SessionUpdate{
			AgentMessageChunk: &sdkacp.SessionUpdateAgentMessageChunk{Content: sdkacp.TextBlock(f.reply)},
		},
	})
	return sdkacp.PromptResponse{StopReason: sdkacp.StopReasonEndTurn}, nil
}

func gatedModes() *sdkacp.SessionModeState {
	return &sdkacp.SessionModeState{
		CurrentModeId:  "auto",
		AvailableModes: []sdkacp.SessionMode{{Id: "auto", Name: "auto"}, {Id: acp.ModeGated, Name: "default"}},
	}
}

// spawnerWith wires an ACPSpawner to a fakeConn, bypassing process start.
func spawnerWith(t *testing.T, f *fakeConn) *ACPSpawner {
	t.Helper()
	s := &ACPSpawner{Command: "true"}
	s.newConn = func(c *acp.Client, _ io.Writer, _ io.Reader) acpConn {
		f.client = c
		return f
	}
	return s
}

func testArgs(t *testing.T) LLMSpawnArgs {
	t.Helper()
	return LLMSpawnArgs{
		TaskID: "task-1", StageRunID: "sr-1", Stage: "review",
		SystemPrompt: "sys", UserPrompt: "do the thing", WorkDir: t.TempDir(),
	}
}

func TestACPSpawnerName(t *testing.T) {
	require.Equal(t, "acp", (&ACPSpawner{}).Name())
}

func TestACPSpawnerWritesTheAgentReplyToASessionFile(t *testing.T) {
	f := &fakeConn{modes: gatedModes(), reply: "the answer"}
	res, err := spawnerWith(t, f).Spawn(context.Background(), testArgs(t))

	require.NoError(t, err)
	require.Equal(t, 0, res.PID, "the adapter owns the process, so it reports no PID")
	require.NotEmpty(t, res.SessionFile)

	b, readErr := os.ReadFile(res.SessionFile)
	require.NoError(t, readErr)
	require.Contains(t, string(b), "the answer")
}

func TestACPSpawnerPinsTheGatedMode(t *testing.T) {
	f := &fakeConn{modes: gatedModes(), reply: "ok"}
	_, err := spawnerWith(t, f).Spawn(context.Background(), testArgs(t))

	require.NoError(t, err)
	require.Equal(t, []sdkacp.SessionModeId{acp.ModeGated}, f.setModes)
}

func TestACPSpawnerRefusesASessionItCannotGate(t *testing.T) {
	f := &fakeConn{
		modes: &sdkacp.SessionModeState{CurrentModeId: "auto",
			AvailableModes: []sdkacp.SessionMode{{Id: "auto", Name: "auto"}}},
		reply: "ok",
	}
	_, err := spawnerWith(t, f).Spawn(context.Background(), testArgs(t))

	require.Error(t, err, "an ungatable session must not run")
	require.Empty(t, f.setModes)
}

func TestACPSpawnerFailsWhenTheSessionCannotStart(t *testing.T) {
	f := &fakeConn{modes: gatedModes(), newErr: errors.New("refused")}
	_, err := spawnerWith(t, f).Spawn(context.Background(), testArgs(t))
	require.Error(t, err)
}

func TestACPSpawnerFailsWhenThePromptFails(t *testing.T) {
	f := &fakeConn{modes: gatedModes(), promptErr: errors.New("model down")}
	_, err := spawnerWith(t, f).Spawn(context.Background(), testArgs(t))
	require.Error(t, err)
}

func TestACPSpawnerSendsBothPromptParts(t *testing.T) {
	f := &fakeConn{modes: gatedModes(), reply: "ok"}
	args := testArgs(t)
	_, err := spawnerWith(t, f).Spawn(context.Background(), args)
	require.NoError(t, err)
	// The reply file is the observable surface; assert the prompt reached the
	// agent by checking the spawner did not error and produced a session file.
	require.NotEmpty(t, f.setModes)
	require.True(t, strings.Contains(args.SystemPrompt+args.UserPrompt, "do the thing"))
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd server && go test ./internal/llmadapter/... -run TestACPSpawner`
Expected: FAIL — `undefined: ACPSpawner`.

- [ ] **Step 4: Write the implementation**

Create `server/internal/llmadapter/llm_acp.go`:

```go
package llmadapter

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"

	sdkacp "github.com/coder/acp-go-sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/acp"
)

// acpConn is the part of an ACP client connection the adapter drives. It exists
// so tests can run the adapter without starting a process.
type acpConn interface {
	Initialize(ctx context.Context, p sdkacp.InitializeRequest) (sdkacp.InitializeResponse, error)
	NewSession(ctx context.Context, p sdkacp.NewSessionRequest) (sdkacp.NewSessionResponse, error)
	SetSessionMode(ctx context.Context, p sdkacp.SetSessionModeRequest) (sdkacp.SetSessionModeResponse, error)
	Prompt(ctx context.Context, p sdkacp.PromptRequest) (sdkacp.PromptResponse, error)
}

// ACPSpawner runs one stage on an agent that speaks the Agent Client Protocol.
// Unlike the subprocess adapters it owns the connection for the whole turn,
// because an ACP agent blocks on permission requests until the client answers.
type ACPSpawner struct {
	Command    string
	Args       []string
	Permission func(context.Context, acp.PermissionRequest) (acp.PermissionDecision, error)

	newConn func(client *acp.Client, in io.Writer, out io.Reader) acpConn
}

func (s *ACPSpawner) Name() string { return "acp" }

func (s *ACPSpawner) Spawn(ctx context.Context, args LLMSpawnArgs) (LLMSpawnResult, error) {
	var mu sync.Mutex
	var reply strings.Builder
	client := &acp.Client{
		OnEvent: func(e acp.Event) {
			if e.Kind != "agent_message" {
				return
			}
			mu.Lock()
			reply.WriteString(e.Text)
			mu.Unlock()
		},
		OnPermission: s.Permission,
	}

	conn, closeConn, err := s.connect(ctx, client)
	if err != nil {
		return LLMSpawnResult{}, err
	}
	defer closeConn()

	if _, err := conn.Initialize(ctx, sdkacp.InitializeRequest{
		ProtocolVersion: sdkacp.ProtocolVersionNumber,
	}); err != nil {
		return LLMSpawnResult{}, fmt.Errorf("acp adapter: initialize: %w", err)
	}

	sess, err := conn.NewSession(ctx, sdkacp.NewSessionRequest{
		Cwd: args.WorkDir, McpServers: []sdkacp.McpServer{},
	})
	if err != nil {
		return LLMSpawnResult{}, fmt.Errorf("acp adapter: new session: %w", err)
	}

	// A session that cannot be pinned is a session without a permission gate.
	if err := acp.EnsureMode(ctx, conn, sess.SessionId, sess.Modes, acp.ModeGated); err != nil {
		return LLMSpawnResult{}, fmt.Errorf("acp adapter: %w", err)
	}

	prompt := args.SystemPrompt
	if prompt != "" && args.UserPrompt != "" {
		prompt += "\n\n"
	}
	prompt += args.UserPrompt

	if _, err := conn.Prompt(ctx, sdkacp.PromptRequest{
		SessionId: sess.SessionId,
		Prompt:    []sdkacp.ContentBlock{sdkacp.TextBlock(prompt)},
	}); err != nil {
		return LLMSpawnResult{}, fmt.Errorf("acp adapter: prompt: %w", err)
	}

	mu.Lock()
	text := reply.String()
	mu.Unlock()

	file, err := writeSyntheticSession(args.WorkDir, args.StageRunID, text)
	if err != nil {
		return LLMSpawnResult{}, fmt.Errorf("acp adapter: %w", err)
	}
	return LLMSpawnResult{PID: 0, SessionID: string(sess.SessionId), SessionFile: file}, nil
}

// connect starts the agent process and returns the driven connection plus a
// teardown that stops the process and releases its pipes.
func (s *ACPSpawner) connect(ctx context.Context, client *acp.Client) (acpConn, func(), error) {
	if s.newConn != nil {
		return s.newConn(client, io.Discard, strings.NewReader("")), func() {}, nil
	}
	if s.Command == "" {
		return nil, nil, fmt.Errorf("acp adapter: no command configured")
	}
	cmd := exec.CommandContext(ctx, s.Command, s.Args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("acp adapter: stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("acp adapter: stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("acp adapter: start %q: %w", s.Command, err)
	}
	conn := sdkacp.NewClientSideConnection(client, stdin, stdout)
	return conn, func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd server && go test -race ./internal/llmadapter/... && go vet ./...`
Expected: PASS, vet clean. If `sdkacp.NewClientSideConnection`'s return type does not satisfy `acpConn`, read the real method set with `go doc github.com/coder/acp-go-sdk.ClientSideConnection` and adjust the interface to match — the SDK is authoritative, not this plan.

- [ ] **Step 6: Commit**

```bash
git add server/internal/llmadapter/
git commit -S -m "feat(llmadapter): run a stage on an ACP agent"
```

---

### Task 2: Make it selectable

**Files:**
- Modify: `server/internal/llmadapter/adapter_factory.go`
- Modify: `server/internal/llmadapter/adapter_catalog.go`
- Test: `server/internal/llmadapter/adapter_factory_test.go`

**Interfaces:**
- Consumes: `ACPSpawner` from Task 1.
- Produces: `NewLLMSpawnerFromSpawner` returns an `*ACPSpawner` for `adapter_type == "acp"`; `AvailableAdapters` gains an `acp` entry with the config keys `command` and `args`.

- [ ] **Step 1: Write the failing tests**

`adapter_factory_test.go` is in the EXTERNAL test package `llmadapter_test`, so every symbol needs the `llmadapter.` prefix. `llm_acp_test.go` from Task 1 is in the internal package `llmadapter` because it sets the unexported `newConn` seam — do not move either.

Append to `server/internal/llmadapter/adapter_factory_test.go`:

```go
func TestFactoryBuildsTheACPAdapter(t *testing.T) {
	s, err := llmadapter.NewLLMSpawnerFromSpawner(&ent.Spawner{
		AdapterType:   "acp",
		AdapterConfig: map[string]string{"command": "npx", "args": "-y @agentclientprotocol/claude-agent-acp@latest"},
	})
	require.NoError(t, err)
	require.NotNil(t, s)
	require.Equal(t, "acp", s.Name())

	a, ok := s.(*llmadapter.ACPSpawner)
	require.True(t, ok)
	require.Equal(t, "npx", a.Command)
	require.Equal(t, []string{"-y", "@agentclientprotocol/claude-agent-acp@latest"}, a.Args)
}

func TestFactoryDefaultsTheACPCommand(t *testing.T) {
	s, err := llmadapter.NewLLMSpawnerFromSpawner(&ent.Spawner{AdapterType: "acp"})
	require.NoError(t, err)

	a, ok := s.(*llmadapter.ACPSpawner)
	require.True(t, ok)
	require.NotEmpty(t, a.Command, "an unconfigured acp row must still be runnable")
}

func TestCatalogListsTheACPAdapter(t *testing.T) {
	var found *llmadapter.AdapterMeta
	for i := range llmadapter.AvailableAdapters {
		if llmadapter.AvailableAdapters[i].Name == "acp" {
			found = &llmadapter.AvailableAdapters[i]
		}
	}
	require.NotNil(t, found, "the settings UI reads this catalog")
	require.NotEmpty(t, found.Description)
}
```

Match the import list and the `ent.Spawner` construction style already used in that file; if the existing tests build spawner rows differently, follow them rather than this snippet.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd server && go test ./internal/llmadapter/... -run 'TestFactoryBuildsTheACP|TestFactoryDefaultsTheACP|TestCatalogLists'`
Expected: FAIL — the factory returns an error for the unknown type `acp`.

- [ ] **Step 3: Add the factory case**

In `adapter_factory.go`, next to the `anthropic` and `custom` cases:

```go
	case "acp":
		// The ACP adapter owns the agent process for the whole stage, because
		// an ACP agent blocks on permission requests until the client answers.
		a := &ACPSpawner{Command: s.AdapterConfig["command"]}
		if a.Command == "" {
			a.Command = "npx"
			a.Args = []string{"-y", "@agentclientprotocol/claude-agent-acp@latest"}
		} else if raw := s.AdapterConfig["args"]; raw != "" {
			a.Args = strings.Fields(raw)
		}
		return a, nil
```

Add `"strings"` to the file's imports if it is not already there.

- [ ] **Step 4: Add the catalog entry**

In `adapter_catalog.go`, append to `AvailableAdapters`:

```go
	{
		Name:        "acp",
		Description: "Agent Client Protocol adapter — drives an ACP agent for the whole stage. Permission requests are denied until the approval gate is wired, so use it for stages that need no approvals.",
		ConfigKeys: []ConfigKeyDoc{
			{Key: "command", Type: "string", Required: false, Note: "Agent executable, default npx"},
			{Key: "args", Type: "string", Required: false, Note: "Space-separated arguments, default -y @agentclientprotocol/claude-agent-acp@latest"},
		},
	},
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd server && go test -race ./internal/llmadapter/... && go vet ./...`
Expected: PASS, vet clean.

- [ ] **Step 6: Run the full gate**

```bash
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
task test
task lint
git checkout -- server/internal/db/ent/
git status --short
```

Expected: both green; `git status` shows only the intended files.

- [ ] **Step 7: Commit and push**

```bash
git add server/internal/llmadapter/
git commit -S -m "feat(llmadapter): make the ACP adapter selectable"
git push -u origin feat/acp-adapter
```

---

## Done when

- A spawner row with `adapter_type: "acp"` produces an `*ACPSpawner`, with or without config.
- `Spawn` pins the gated mode, refuses a session it cannot pin, and writes the agent's reply to a synthetic session file the completion detector can read.
- The catalog lists `acp`, including the sentence that permission requests are denied until the gate is wired.
- `task test` and `task lint` are green with the raw output pasted into the PR.
- No test spawns a process, opens a database, or touches the network.
- Nothing outside `server/internal/llmadapter/` changed.

## Deliberately not in this plan

Binding `ACPSpawner.Permission` to the dashboard's approval flow. That needs a dashboard URL and token which `LLMSpawnArgs` does not carry, and the four acceptance criteria recorded under step 3 in `docs/local/spec-acp-client.md` — context-honouring status reads, withdrawing an orphaned request, widening `PermissionRequest` with `Kind`/`Locations`/`RawInput`, and stopping a session on mode drift. That is the next increment.
