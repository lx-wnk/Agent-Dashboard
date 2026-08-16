# PR-E: LLM Adapter System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Introduce an `LLMSpawner` interface so pipeline stage agents can use non-Claude LLMs (Ollama, OpenAI-compatible, custom commands) via config-driven adapter selection.

**Architecture:** New interface `LLMSpawner` in `server/internal/pipeline/llm_spawner.go`. Four implementations: `ClaudeSpawner` (wraps existing `SpawnStageAgent`), `OllamaSpawner`, `OpenAISpawner`, `CustomCommandSpawner`. Orchestrator receives the spawner via `OrchestratorOptions`. Non-subprocess adapters write a synthetic JSONL session file for the completion detector. Adapter config lives in `server/internal/config/adapters.go` and is read from settings JSON / env vars.

**Tech Stack:** Go stdlib `net/http`, `encoding/json`, `os/exec`, existing ent pipeline types

---

## Worktree Setup

```bash
git worktree add ../agent-dashboard-pre feat/llm-adapters
cd ../agent-dashboard-pre/server
```

---

## File Map

| Action | File |
|--------|------|
| Create | `server/internal/pipeline/llm_spawner.go` |
| Create | `server/internal/pipeline/llm_claude.go` |
| Create | `server/internal/pipeline/llm_ollama.go` |
| Create | `server/internal/pipeline/llm_openai.go` |
| Create | `server/internal/pipeline/llm_custom.go` |
| Create | `server/internal/pipeline/llm_spawner_test.go` |
| Create | `server/internal/config/adapters.go` |
| Modify | `server/internal/pipeline/types.go` (`OrchestratorOptions` gets `Spawner LLMSpawner`) |
| Modify | `server/internal/pipeline/stage_handlers.go` (use `LLMSpawner` instead of calling `SpawnStageAgent` directly) |
| Modify | `server/internal/api/router.go` (add GET/PUT /api/settings/adapters routes) |
| Create | `server/internal/api/adapters/handler.go` |

---

### Task 1: LLMSpawner interface + types

**Files:**
- Create: `server/internal/pipeline/llm_spawner.go`

- [ ] **Step 1.1: Write the interface**

```go
package pipeline

import "context"

// LLMSpawnArgs carries everything an LLM adapter needs to run a stage.
type LLMSpawnArgs struct {
	TaskID       string
	StageRunID   string
	SystemPrompt string
	UserPrompt   string
	Model        string
	WorkDir      string
	AllowedTools []string
	Env          []string
}

// LLMSpawnResult is returned by a successful Spawn call.
type LLMSpawnResult struct {
	// PID of the spawned process, or 0 for non-subprocess adapters.
	PID int
	// SessionID is a Claude session ID (for ClaudeSpawner) or synthetic ID.
	SessionID string
	// SessionFile is the path to the JSONL file the completion detector reads.
	// For ClaudeSpawner this is discovered by the completion detector normally.
	// For non-subprocess adapters this must be written by the adapter itself.
	SessionFile string
}

// LLMSpawner is implemented by each LLM adapter.
type LLMSpawner interface {
	// Name returns the adapter identifier (e.g. "claude", "openai", "ollama").
	Name() string
	// Spawn starts the LLM agent and returns a handle.
	Spawn(ctx context.Context, args LLMSpawnArgs) (LLMSpawnResult, error)
}
```

- [ ] **Step 1.2: Build check**

```bash
go build ./internal/pipeline/
```

---

### Task 2: ClaudeSpawner

**Files:**
- Create: `server/internal/pipeline/llm_claude.go`

- [ ] **Step 2.1: Write `ClaudeSpawner`**

```go
package pipeline

import (
	"context"
	"fmt"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
)

// ClaudeSpawner is the default LLMSpawner that delegates to SpawnStageAgent.
type ClaudeSpawner struct {
	MCPToken     string
	MCPUrl       string
	AllowGitPush bool
}

func (c *ClaudeSpawner) Name() string { return "claude" }

func (c *ClaudeSpawner) Spawn(_ context.Context, args LLMSpawnArgs) (LLMSpawnResult, error) {
	// Re-use the existing SpawnStageAgent by constructing a minimal Task + StageRun.
	// SpawnStageAgent reads Task.Cwd, Task.WorktreePath, Task.Metadata (for allowGitPush).
	task := &ent.Task{ID: args.TaskID, Cwd: args.WorkDir}
	sr := &ent.StageRun{ID: args.StageRunID}
	opts := SpawnAgentOptions{
		Task:         task,
		StageRun:     sr,
		Prompt:       args.UserPrompt,
		SystemPrompt: args.SystemPrompt,
		Model:        args.Model,
		EnableChannel: true,
		MCPToken:     c.MCPToken,
		MCPUrl:       c.MCPUrl,
	}
	result, err := SpawnStageAgent(opts)
	if err != nil {
		return LLMSpawnResult{}, fmt.Errorf("ClaudeSpawner.Spawn: %w", err)
	}
	return LLMSpawnResult{PID: result.PID}, nil
}
```

Note: `ClaudeSpawner` is a thin delegation layer. The existing `SpawnStageAgent` handles all the settings-file and channel-config complexity. Permissions are passed separately in `OrchestratorOptions` before the spawner is called.

- [ ] **Step 2.2: Build check**

```bash
go build ./internal/pipeline/
```

---

### Task 3: OllamaSpawner

**Files:**
- Create: `server/internal/pipeline/llm_ollama.go`

- [ ] **Step 3.1: Write `OllamaSpawner`**

```go
package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// OllamaSpawner calls the Ollama HTTP API synchronously and writes a
// synthetic JSONL session file so the completion detector can parse the output.
type OllamaSpawner struct {
	Host         string // e.g. "http://localhost:11434"
	DefaultModel string
}

func (o *OllamaSpawner) Name() string { return "ollama" }

func (o *OllamaSpawner) Spawn(ctx context.Context, args LLMSpawnArgs) (LLMSpawnResult, error) {
	model := args.Model
	if model == "" {
		model = o.DefaultModel
	}
	host := o.Host
	if host == "" {
		host = "http://localhost:11434"
	}

	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type request struct {
		Model    string    `json:"model"`
		Messages []message `json:"messages"`
		Stream   bool      `json:"stream"`
	}
	body, _ := json.Marshal(request{
		Model: model,
		Messages: []message{
			{Role: "system", Content: args.SystemPrompt},
			{Role: "user", Content: args.UserPrompt},
		},
		Stream: false,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, host+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return LLMSpawnResult{}, fmt.Errorf("OllamaSpawner: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return LLMSpawnResult{}, fmt.Errorf("OllamaSpawner: POST /api/chat: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return LLMSpawnResult{}, fmt.Errorf("OllamaSpawner: decode response: %w", err)
	}

	sessionFile, err := writeSyntheticSession(args.WorkDir, args.StageRunID, result.Message.Content)
	if err != nil {
		return LLMSpawnResult{}, err
	}
	return LLMSpawnResult{PID: 0, SessionID: "ollama-" + args.StageRunID, SessionFile: sessionFile}, nil
}

// writeSyntheticSession writes a single-line JSONL file in the format the
// completion detector expects: one assistant message whose text contains the
// ```json ... ``` block produced by the LLM.
func writeSyntheticSession(workDir, stageRunID, content string) (string, error) {
	dir := filepath.Join(os.TempDir(), "dashboard-synthetic-sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("writeSyntheticSession: mkdir: %w", err)
	}
	sessionID := "synthetic-" + stageRunID
	path := filepath.Join(dir, sessionID+".jsonl")

	type contentBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type line struct {
		Type      string         `json:"type"`
		Role      string         `json:"role"`
		Content   []contentBlock `json:"content"`
		Timestamp string         `json:"timestamp"`
	}
	entry := line{
		Type: "message",
		Role: "assistant",
		Content: []contentBlock{
			{Type: "text", Text: content},
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return "", fmt.Errorf("writeSyntheticSession: write: %w", err)
	}
	return path, nil
}
```

- [ ] **Step 3.2: Build check**

```bash
go build ./internal/pipeline/
```

---

### Task 4: OpenAISpawner

**Files:**
- Create: `server/internal/pipeline/llm_openai.go`

- [ ] **Step 4.1: Write `OpenAISpawner`**

```go
package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

// OpenAISpawner calls any OpenAI-compatible chat completions endpoint.
type OpenAISpawner struct {
	BaseURL      string // e.g. "https://api.openai.com/v1"
	APIKeyEnv    string // env var name holding the API key, e.g. "OPENAI_API_KEY"
	DefaultModel string
}

func (o *OpenAISpawner) Name() string { return "openai" }

func (o *OpenAISpawner) Spawn(ctx context.Context, args LLMSpawnArgs) (LLMSpawnResult, error) {
	model := args.Model
	if model == "" {
		model = o.DefaultModel
	}
	baseURL := o.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	apiKey := os.Getenv(o.APIKeyEnv)

	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type request struct {
		Model    string    `json:"model"`
		Messages []message `json:"messages"`
	}
	body, _ := json.Marshal(request{
		Model: model,
		Messages: []message{
			{Role: "system", Content: args.SystemPrompt},
			{Role: "user", Content: args.UserPrompt},
		},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return LLMSpawnResult{}, fmt.Errorf("OpenAISpawner: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return LLMSpawnResult{}, fmt.Errorf("OpenAISpawner: POST completions: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return LLMSpawnResult{}, fmt.Errorf("OpenAISpawner: decode: %w", err)
	}
	if len(result.Choices) == 0 {
		return LLMSpawnResult{}, fmt.Errorf("OpenAISpawner: no choices in response")
	}

	content := result.Choices[0].Message.Content
	sessionFile, err := writeSyntheticSession(args.WorkDir, args.StageRunID, content)
	if err != nil {
		return LLMSpawnResult{}, err
	}
	return LLMSpawnResult{PID: 0, SessionID: "openai-" + args.StageRunID, SessionFile: sessionFile}, nil
}
```

- [ ] **Step 4.2: Build check**

```bash
go build ./internal/pipeline/
```

---

### Task 5: CustomCommandSpawner

**Files:**
- Create: `server/internal/pipeline/llm_custom.go`

- [ ] **Step 5.1: Write `CustomCommandSpawner`**

```go
package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// CustomCommandSpawner runs an arbitrary executable, passes LLMSpawnArgs as JSON
// on stdin, and reads LLMSpawnResult JSON from stdout.
// Set DASHBOARD_SPAWN_COMMAND=/path/to/executable to activate.
type CustomCommandSpawner struct {
	Command string // path to executable
}

func (c *CustomCommandSpawner) Name() string { return "custom" }

func (c *CustomCommandSpawner) Spawn(ctx context.Context, args LLMSpawnArgs) (LLMSpawnResult, error) {
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return LLMSpawnResult{}, err
	}
	cmd := exec.CommandContext(ctx, c.Command)
	cmd.Stdin = bytes.NewReader(argsJSON)
	out, err := cmd.Output()
	if err != nil {
		return LLMSpawnResult{}, fmt.Errorf("CustomCommandSpawner: exec %s: %w", c.Command, err)
	}
	var result LLMSpawnResult
	if err := json.Unmarshal(out, &result); err != nil {
		return LLMSpawnResult{}, fmt.Errorf("CustomCommandSpawner: decode result: %w", err)
	}
	return result, nil
}
```

- [ ] **Step 5.2: Build check**

```bash
go build ./internal/pipeline/
```

---

### Task 6: Adapter config

**Files:**
- Create: `server/internal/config/adapters.go`

- [ ] **Step 6.1: Write adapter config types**

```go
package config

import "os"

// AdapterConfig selects which LLM adapter to use per stage.
type AdapterConfig struct {
	Default string            `koanf:"default"` // "claude" | "openai" | "ollama" | "custom"
	Stages  map[string]string `koanf:"stages"`  // stage name → adapter name
	Ollama  OllamaConfig      `koanf:"ollama"`
	OpenAI  OpenAIConfig      `koanf:"openai"`
}

type OllamaConfig struct {
	Host         string `koanf:"host"`
	DefaultModel string `koanf:"default_model"`
}

type OpenAIConfig struct {
	BaseURL      string `koanf:"base_url"`
	APIKeyEnv    string `koanf:"api_key_env"`
	DefaultModel string `koanf:"default_model"`
}

// AdapterForStage returns the configured adapter name for the given stage,
// falling back to Default, then "claude".
func (a AdapterConfig) AdapterForStage(stage string) string {
	if name, ok := a.Stages[stage]; ok && name != "" {
		return name
	}
	if a.Default != "" {
		return a.Default
	}
	return "claude"
}

// SpawnCommandFromEnv returns the value of DASHBOARD_SPAWN_COMMAND, or "".
func SpawnCommandFromEnv() string { return os.Getenv("DASHBOARD_SPAWN_COMMAND") }
```

Add `Adapters AdapterConfig \`koanf:"adapters"\`` to the `Config` struct in `config.go`.

- [ ] **Step 6.2: Build check**

```bash
go build ./internal/config/
```

---

### Task 7: Tests

**Files:**
- Create: `server/internal/pipeline/llm_spawner_test.go`

- [ ] **Step 7.1: Write tests for OllamaSpawner and OpenAISpawner** (using httptest servers)

```go
package pipeline_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
)

func TestOllamaSpawner_Spawn_WritesSessionFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/chat", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{"content": "```json\n{\"summary\":\"ok\"}\n```"},
		})
	}))
	defer srv.Close()

	spawner := &pipeline.OllamaSpawner{Host: srv.URL, DefaultModel: "llama3"}
	result, err := spawner.Spawn(context.Background(), pipeline.LLMSpawnArgs{
		TaskID:       "t1",
		StageRunID:   "sr1",
		UserPrompt:   "do the thing",
		SystemPrompt: "you are helpful",
		WorkDir:      t.TempDir(),
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.PID)
	assert.NotEmpty(t, result.SessionFile)

	// Session file must exist and contain valid JSONL.
	data, err := os.ReadFile(result.SessionFile)
	require.NoError(t, err)
	assert.Contains(t, string(data), "assistant")

	// Cleanup
	os.Remove(result.SessionFile)
}

func TestOpenAISpawner_Spawn_WritesSessionFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/completions", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]any{"content": "```json\n{\"result\":\"done\"}\n```"}},
			},
		})
	}))
	defer srv.Close()

	spawner := &pipeline.OpenAISpawner{BaseURL: srv.URL, DefaultModel: "gpt-4o"}
	result, err := spawner.Spawn(context.Background(), pipeline.LLMSpawnArgs{
		TaskID:     "t2",
		StageRunID: "sr2",
		UserPrompt: "do a thing",
		WorkDir:    t.TempDir(),
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.PID)
	assert.NotEmpty(t, result.SessionFile)

	data, err := os.ReadFile(result.SessionFile)
	require.NoError(t, err)
	assert.Contains(t, string(data), "assistant")

	os.Remove(result.SessionFile)
}

func TestClaudeSpawner_Name(t *testing.T) {
	s := &pipeline.ClaudeSpawner{}
	assert.Equal(t, "claude", s.Name())
}

func TestOllamaSpawner_Name(t *testing.T) {
	s := &pipeline.OllamaSpawner{}
	assert.Equal(t, "ollama", s.Name())
}

func TestOpenAISpawner_Name(t *testing.T) {
	s := &pipeline.OpenAISpawner{}
	assert.Equal(t, "openai", s.Name())
}

func TestCustomCommandSpawner_Name(t *testing.T) {
	s := &pipeline.CustomCommandSpawner{Command: "/bin/echo"}
	assert.Equal(t, "custom", s.Name())
}
```

- [ ] **Step 7.2: Run**

```bash
go test -race -run "TestOllamaSpawner|TestOpenAISpawner|TestClaudeSpawner|TestCustomCommandSpawner" \
  ./internal/pipeline/ -v
```

Expected: all pass.

- [ ] **Step 7.3: Commit spawner impls + tests**

```bash
git add server/internal/pipeline/llm_*.go server/internal/config/adapters.go
git commit -m "feat: LLMSpawner interface with Claude, Ollama, OpenAI, and CustomCommand adapters"
```

---

### Task 8: Wire spawner into orchestrator

- [ ] **Step 8.1: Add `Spawner LLMSpawner` to `OrchestratorOptions`**

In `server/internal/pipeline/types.go`, add to `OrchestratorOptions`:
```go
// Spawner selects which LLM backend runs stage agents.
// Defaults to ClaudeSpawner when nil.
Spawner LLMSpawner
```

- [ ] **Step 8.2: Update `stage_handlers.go` to use the spawner**

In `createAgentStage` factory (or wherever `SpawnStageAgent` is called directly):

```go
// Before (direct call):
result, err := SpawnStageAgent(opts)

// After (through spawner):
spawner := o.opts.Spawner
if spawner == nil {
    spawner = &ClaudeSpawner{MCPToken: o.opts.MCPToken, MCPUrl: o.opts.MCPUrl}
}
spawnArgs := LLMSpawnArgs{
    TaskID:       sc.Task.ID,
    StageRunID:   sc.StageRun.ID,
    SystemPrompt: bundle.SystemPrompt,
    UserPrompt:   bundle.UserPrompt,
    Model:        sc.Task.Metadata["model"].(string), // safe cast with ok-check
    WorkDir:      sc.Task.Cwd,
}
llmResult, err := spawner.Spawn(sc.Ctx, spawnArgs)
```

Use `llmResult.PID` in the `AsyncRunningTransition` and `llmResult.SessionFile` in the completion detector path if non-empty.

- [ ] **Step 8.3: Wire spawner in `cmd/serve/wire.go`**

```go
spawnCmd := config.SpawnCommandFromEnv()
var spawner pipeline.LLMSpawner
switch {
case spawnCmd != "":
    spawner = &pipeline.CustomCommandSpawner{Command: spawnCmd}
case cfg.Adapters.Default == "ollama":
    spawner = &pipeline.OllamaSpawner{Host: cfg.Adapters.Ollama.Host, DefaultModel: cfg.Adapters.Ollama.DefaultModel}
case cfg.Adapters.Default == "openai":
    spawner = &pipeline.OpenAISpawner{BaseURL: cfg.Adapters.OpenAI.BaseURL, APIKeyEnv: cfg.Adapters.OpenAI.APIKeyEnv, DefaultModel: cfg.Adapters.OpenAI.DefaultModel}
default:
    spawner = &pipeline.ClaudeSpawner{MCPToken: cfg.MCPToken, MCPUrl: "http://" + cfg.Addr() + "/api/mcp"}
}
```

Pass `spawner` to `OrchestratorOptions.Spawner`.

- [ ] **Step 8.4: Build + test**

```bash
go build ./... && go test -race ./...
```

---

### Task 9: Settings API

**Files:**
- Create: `server/internal/api/adapters/handler.go`

- [ ] **Step 9.1: Write handler**

```go
package adapters

import (
	"encoding/json"
	"net/http"

	"github.com/lx-wnk/agent-dashboard/server/internal/config"
)

// Handler serves GET/PUT /api/settings/adapters.
type Handler struct {
	cfg *config.AdapterConfig
}

func NewHandler(cfg *config.AdapterConfig) *Handler {
	return &Handler{cfg: cfg}
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.cfg)
}

func (h *Handler) Put(w http.ResponseWriter, r *http.Request) {
	var incoming config.AdapterConfig
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	*h.cfg = incoming
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.cfg)
}
```

- [ ] **Step 9.2: Mount in router**

In `server/internal/api/router.go`, in the authenticated settings block:
```go
r.Get("/api/settings/adapters", adapterHandler.Get)
r.Put("/api/settings/adapters", adapterHandler.Put)
```

Add `AdapterHandler *adapters.Handler` to `RouterDeps`.

- [ ] **Step 9.3: Build + test**

```bash
go build ./... && go test -race ./...
```

- [ ] **Step 9.4: Commit and push**

```bash
git add server/
git commit -m "feat: wire LLMSpawner into orchestrator + settings API for adapter config"
git push -u origin feat/llm-adapters
```
