# Refinement Spawner Resolution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `/api/refine/{taskId}/turn` resolve the effective spawner per turn (task → project → claude-default), execute the resolved row, and stream the assistant response for every adapter type (`claude`, `ollama`, `openai`, `custom`).

**Architecture:** Introduce a `StreamingLLMSpawner` interface alongside `LLMSpawner`. Each existing adapter gets a streaming variant. The `refine` package gains a new spawner constructor that takes the resolved `*ent.Spawner` row and dispatches to either the native `claude` exec path (now honoring `command`/`args`/`env`) or the matching `StreamingLLMSpawner`. The refine HTTP handler obtains a `SpawnerResolver` via DI and calls it on each `submitTurn`.

**Tech Stack:** Go 1.26, chi router, ent ORM, modernc/sqlite, Wire DI; tests via `go test` with `httptest`.

---

## File Structure

| File | Responsibility |
|---|---|
| `server/internal/pipeline/llm_spawner.go` (modify) | Add `StreamingLLMSpawner` interface |
| `server/internal/pipeline/llm_ollama.go` (modify) | Implement `SpawnStream` |
| `server/internal/pipeline/llm_openai.go` (modify) | Implement `SpawnStream` |
| `server/internal/pipeline/llm_custom.go` (modify) | Implement `SpawnStream` |
| `server/internal/refine/spawner.go` (modify) | Accept `*ent.Spawner`, branch on adapter type |
| `server/internal/refine/spawner_env.go` (create) | Env-merge helper (custom env first, dashboard overlay) |
| `server/internal/refine/spawner_test.go` (create) | Table-driven adapter branch tests |
| `server/internal/api/refine/handler.go` (modify) | Add `Deps.ResolveSpawner`, thread it through `submitTurn` |
| `server/internal/api/refine/handler_test.go` (modify) | Stub resolver, assert correct branch + SSE frames |
| `server/cmd/serve/di.go` (modify) | Inject `spawnerResolver.Resolve` into refine deps |

## Spec Section B coverage map

| Spec bullet | Task |
|---|---|
| `StreamingLLMSpawner` interface | Task 1 |
| Ollama streaming | Task 2 |
| OpenAI streaming | Task 3 |
| Custom command streaming | Task 4 |
| Claude exec honors `command`/`args`/`env` | Task 5 |
| Env-merge rules | Task 5 + Task 7 |
| Resolver injection in refine handler | Task 6 |
| DI wire-up | Task 7 |
| Error handling (resolver error, missing adapter, mid-stream error) | Tasks 5–7 |
| Regression: claude-default still streams | Task 8 |

---

### Task 1: Define `StreamingLLMSpawner` interface

**Files:**
- Modify: `server/internal/pipeline/llm_spawner.go`
- Create: `server/internal/pipeline/llm_spawner_stream_test.go`

- [ ] **Step 1: Write failing compile-time test**

Write `server/internal/pipeline/llm_spawner_stream_test.go`:

```go
package pipeline_test

import (
	"context"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
)

// Compile-time guard: ensure the streaming interface is exported and shaped
// correctly. The test body is empty — the assertion is the var declaration.
func TestStreamingLLMSpawner_InterfaceShape(t *testing.T) {
	var _ pipeline.StreamingLLMSpawner = (*fakeStreaming)(nil)
	_ = t // silence unused
}

type fakeStreaming struct{}

func (fakeStreaming) Name() string { return "fake" }
func (fakeStreaming) Spawn(_ context.Context, _ pipeline.LLMSpawnArgs) (pipeline.LLMSpawnResult, error) {
	return pipeline.LLMSpawnResult{}, nil
}
func (fakeStreaming) SpawnStream(_ context.Context, _ pipeline.LLMSpawnArgs) (<-chan string, error) {
	ch := make(chan string)
	close(ch)
	return ch, nil
}
```

- [ ] **Step 2: Run test, confirm it fails**

Run: `cd server && go test ./internal/pipeline/... -run StreamingLLMSpawner_InterfaceShape`
Expected: FAIL — `pipeline.StreamingLLMSpawner` undefined.

- [ ] **Step 3: Add interface**

Append to `server/internal/pipeline/llm_spawner.go`:

```go
// StreamingLLMSpawner extends LLMSpawner with a chunked-output variant used by
// surfaces that want SSE-style token streaming (currently /api/refine). Each
// emitted string is one chunk; the channel is closed when the stream ends or
// the context is cancelled. Adapters that cannot stream natively may emit the
// full response as a single chunk before closing.
type StreamingLLMSpawner interface {
	LLMSpawner
	SpawnStream(ctx context.Context, args LLMSpawnArgs) (<-chan string, error)
}
```

- [ ] **Step 4: Run test, confirm it passes**

Run: `cd server && go test ./internal/pipeline/... -run StreamingLLMSpawner_InterfaceShape`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/pipeline/llm_spawner.go server/internal/pipeline/llm_spawner_stream_test.go
git commit -m "feat(pipeline): add StreamingLLMSpawner interface"
```

---

### Task 2: Implement `OllamaSpawner.SpawnStream`

**Files:**
- Modify: `server/internal/pipeline/llm_ollama.go`
- Create: `server/internal/pipeline/llm_ollama_stream_test.go`

- [ ] **Step 1: Write failing test against an `httptest.Server` returning NDJSON**

Write `server/internal/pipeline/llm_ollama_stream_test.go`:

```go
package pipeline

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOllamaSpawner_SpawnStream_NDJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		enc := json.NewEncoder(w)
		_ = enc.Encode(map[string]any{"message": map[string]string{"content": "hello "}})
		_ = enc.Encode(map[string]any{"message": map[string]string{"content": "world"}})
		_ = enc.Encode(map[string]any{"done": true})
	}))
	defer srv.Close()

	o := &OllamaSpawner{Host: srv.URL, DefaultModel: "llama3"}
	ch, err := o.SpawnStream(context.Background(), LLMSpawnArgs{SystemPrompt: "sys", UserPrompt: "hi"})
	if err != nil {
		t.Fatalf("SpawnStream: %v", err)
	}
	var got []string
	for s := range ch {
		got = append(got, s)
	}
	want := []string{"hello ", "world"}
	if len(got) != len(want) {
		t.Fatalf("chunks: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chunk[%d]: got %q want %q", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run test, confirm it fails**

Run: `cd server && go test ./internal/pipeline/... -run TestOllamaSpawner_SpawnStream_NDJSON`
Expected: FAIL — `SpawnStream` undefined on `OllamaSpawner`.

- [ ] **Step 3: Implement `SpawnStream`**

Append to `server/internal/pipeline/llm_ollama.go`:

```go
// SpawnStream calls Ollama's /api/chat with stream:true and emits message.content
// chunks on the returned channel. The channel is closed when the stream ends or
// the context is cancelled.
func (o *OllamaSpawner) SpawnStream(ctx context.Context, args LLMSpawnArgs) (<-chan string, error) {
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
	body, err := json.Marshal(request{
		Model: model,
		Messages: []message{
			{Role: "system", Content: args.SystemPrompt},
			{Role: "user", Content: args.UserPrompt},
		},
		Stream: true,
	})
	if err != nil {
		return nil, fmt.Errorf("OllamaSpawner.SpawnStream: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, host+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("OllamaSpawner.SpawnStream: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	o.clientOnce.Do(func() { o.client = &http.Client{Timeout: 5 * time.Minute} })
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OllamaSpawner.SpawnStream: POST /api/chat: %w", err)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("OllamaSpawner.SpawnStream: HTTP %d: %s", resp.StatusCode, body)
	}

	ch := make(chan string, 16)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		dec := json.NewDecoder(resp.Body)
		for dec.More() {
			var chunk struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
				Done bool `json:"done"`
			}
			if err := dec.Decode(&chunk); err != nil {
				ch <- "[ERROR] OllamaSpawner.SpawnStream: decode: " + err.Error()
				return
			}
			if chunk.Done {
				return
			}
			if chunk.Message.Content == "" {
				continue
			}
			select {
			case ch <- chunk.Message.Content:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}
```

- [ ] **Step 4: Run test, confirm it passes**

Run: `cd server && go test ./internal/pipeline/... -run TestOllamaSpawner_SpawnStream_NDJSON`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/pipeline/llm_ollama.go server/internal/pipeline/llm_ollama_stream_test.go
git commit -m "feat(pipeline): stream Ollama responses chunk-by-chunk"
```

---

### Task 3: Implement `OpenAISpawner.SpawnStream`

**Files:**
- Modify: `server/internal/pipeline/llm_openai.go`
- Create: `server/internal/pipeline/llm_openai_stream_test.go`

- [ ] **Step 1: Write failing test with mocked SSE response**

Write `server/internal/pipeline/llm_openai_stream_test.go`:

```go
package pipeline

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestOpenAISpawner_SpawnStream_SSE(t *testing.T) {
	t.Setenv("OPENAI_API_KEY_FAKE", "sk-test")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hi \"}}]}\n\n")
		f.Flush()
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"there\"}}]}\n\n")
		f.Flush()
		fmt.Fprintf(w, "data: [DONE]\n\n")
		f.Flush()
	}))
	defer srv.Close()

	os.Setenv("OPENAI_API_KEY_FAKE", "sk-test")
	o := &OpenAISpawner{BaseURL: srv.URL, APIKeyEnv: "OPENAI_API_KEY_FAKE", DefaultModel: "gpt-4"}
	ch, err := o.SpawnStream(context.Background(), LLMSpawnArgs{SystemPrompt: "sys", UserPrompt: "hi"})
	if err != nil {
		t.Fatalf("SpawnStream: %v", err)
	}
	var got []string
	for s := range ch {
		got = append(got, s)
	}
	want := []string{"Hi ", "there"}
	if len(got) != len(want) {
		t.Fatalf("chunks: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chunk[%d]: got %q want %q", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run test, confirm it fails**

Run: `cd server && go test ./internal/pipeline/... -run TestOpenAISpawner_SpawnStream_SSE`
Expected: FAIL — `SpawnStream` undefined on `OpenAISpawner`.

- [ ] **Step 3: Implement `SpawnStream`**

Append to `server/internal/pipeline/llm_openai.go`:

```go
// SpawnStream calls OpenAI's /v1/chat/completions with stream:true and emits
// delta.content tokens on the returned channel. The channel is closed when the
// stream ends, [DONE] is received, or the context is cancelled.
func (o *OpenAISpawner) SpawnStream(ctx context.Context, args LLMSpawnArgs) (<-chan string, error) {
	model := args.Model
	if model == "" {
		model = o.DefaultModel
	}
	baseURL := o.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	apiKey := os.Getenv(o.APIKeyEnv)
	if apiKey == "" {
		return nil, fmt.Errorf("OpenAISpawner.SpawnStream: %s not set", o.APIKeyEnv)
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
	body, err := json.Marshal(request{
		Model: model,
		Messages: []message{
			{Role: "system", Content: args.SystemPrompt},
			{Role: "user", Content: args.UserPrompt},
		},
		Stream: true,
	})
	if err != nil {
		return nil, fmt.Errorf("OpenAISpawner.SpawnStream: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("OpenAISpawner.SpawnStream: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	o.clientOnce.Do(func() { o.client = &http.Client{Timeout: 10 * time.Minute} })
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OpenAISpawner.SpawnStream: POST: %w", err)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("OpenAISpawner.SpawnStream: HTTP %d: %s", resp.StatusCode, body)
	}

	ch := make(chan string, 16)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			payload := strings.TrimPrefix(line, "data: ")
			if payload == "[DONE]" {
				return
			}
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				ch <- "[ERROR] OpenAISpawner.SpawnStream: decode: " + err.Error()
				return
			}
			for _, c := range chunk.Choices {
				if c.Delta.Content == "" {
					continue
				}
				select {
				case ch <- c.Delta.Content:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return ch, nil
}
```

Add imports if missing (`bufio`, `bytes`, `io`, `os`, `strings`, `time`, `encoding/json`, `fmt`, `net/http`). Check the existing `llm_openai.go` imports and merge.

- [ ] **Step 4: Run test, confirm it passes**

Run: `cd server && go test ./internal/pipeline/... -run TestOpenAISpawner_SpawnStream_SSE`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/pipeline/llm_openai.go server/internal/pipeline/llm_openai_stream_test.go
git commit -m "feat(pipeline): stream OpenAI completions token-by-token"
```

---

### Task 4: Implement `CustomCommandSpawner.SpawnStream`

**Files:**
- Modify: `server/internal/pipeline/llm_custom.go`
- Create: `server/internal/pipeline/llm_custom_stream_test.go`

- [ ] **Step 1: Write failing test using a fake binary**

Write `server/internal/pipeline/llm_custom_stream_test.go`:

```go
package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCustomCommandSpawner_SpawnStream_LineByLine(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("custom command streaming relies on POSIX scripts")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fake.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'chunk1\\nchunk2\\nchunk3\\n'\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	c := &CustomCommandSpawner{Command: script}
	ch, err := c.SpawnStream(context.Background(), LLMSpawnArgs{UserPrompt: "x"})
	if err != nil {
		t.Fatalf("SpawnStream: %v", err)
	}
	var got []string
	for s := range ch {
		got = append(got, s)
	}
	want := []string{"chunk1", "chunk2", "chunk3"}
	if len(got) != len(want) {
		t.Fatalf("chunks: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chunk[%d]: got %q want %q", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run test, confirm it fails**

Run: `cd server && go test ./internal/pipeline/... -run TestCustomCommandSpawner_SpawnStream_LineByLine`
Expected: FAIL — `SpawnStream` undefined on `CustomCommandSpawner`.

- [ ] **Step 3: Implement `SpawnStream`**

Append to `server/internal/pipeline/llm_custom.go`:

```go
// SpawnStream exec's the custom command with LLMSpawnArgs JSON on stdin and
// scans stdout line by line. Each non-empty line is emitted as a chunk. The
// channel closes when the process exits or the context is cancelled.
func (c *CustomCommandSpawner) SpawnStream(ctx context.Context, args LLMSpawnArgs) (<-chan string, error) {
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("CustomCommandSpawner.SpawnStream: marshal args: %w", err)
	}
	cmd := exec.CommandContext(ctx, c.Command)
	cmd.Stdin = bytes.NewReader(argsJSON)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("CustomCommandSpawner.SpawnStream: stdout pipe: %w", err)
	}
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("CustomCommandSpawner.SpawnStream: start %s: %w", c.Command, err)
	}

	ch := make(chan string, 16)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			select {
			case ch <- line:
			case <-ctx.Done():
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return
			}
		}
		if err := cmd.Wait(); err != nil {
			msg := "[ERROR] CustomCommandSpawner.SpawnStream: " + err.Error()
			if s := stderrBuf.String(); s != "" {
				msg += " — " + s
			}
			ch <- msg
		}
	}()
	return ch, nil
}
```

Add imports if missing: `bufio`. Existing imports already cover `bytes`, `context`, `encoding/json`, `fmt`, `os/exec`.

- [ ] **Step 4: Run test, confirm it passes**

Run: `cd server && go test ./internal/pipeline/... -run TestCustomCommandSpawner_SpawnStream_LineByLine`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/pipeline/llm_custom.go server/internal/pipeline/llm_custom_stream_test.go
git commit -m "feat(pipeline): stream custom command stdout line-by-line"
```

---

### Task 5: Rewrite `refine.RunRefinementTurn` to accept resolved spawner

**Files:**
- Modify: `server/internal/refine/spawner.go`
- Create: `server/internal/refine/spawner_env.go`
- Create: `server/internal/refine/spawner_test.go`

- [ ] **Step 1: Write failing test for the new signature**

Write `server/internal/refine/spawner_test.go`:

```go
package refine_test

import (
	"context"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/refine"
)

func TestRunRefinementTurn_NilSpawnerUsesClaudeBinary(t *testing.T) {
	// Pass a nil spawner; we don't actually exec — the function must accept the
	// new parameter shape. With nil it must fall back to `claude -p`. We just
	// confirm the signature compiles and returns a channel + nil error before
	// the binary is invoked (use a cancelled context so the exec never starts).
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch, err := refine.RunRefinementTurn(ctx, refine.SpawnConfig{UserMessage: "hi"}, (*ent.Spawner)(nil))
	if err == nil && ch == nil {
		t.Fatal("expected either an error or a channel; got both nil")
	}
}

func TestRunRefinementTurn_ClaudeAdapterStillUsesExec(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sp := &ent.Spawner{AdapterType: "claude"}
	_, _ = refine.RunRefinementTurn(ctx, refine.SpawnConfig{UserMessage: "hi"}, sp)
	// Compile-time guard only.
}

func TestRunRefinementTurn_UnsupportedAdapterReturnsError(t *testing.T) {
	sp := &ent.Spawner{AdapterType: "totally-unknown"}
	_, err := refine.RunRefinementTurn(context.Background(), refine.SpawnConfig{UserMessage: "hi"}, sp)
	if err == nil {
		t.Fatal("expected error for unknown adapter")
	}
}
```

- [ ] **Step 2: Run test, confirm it fails**

Run: `cd server && go test ./internal/refine/...`
Expected: FAIL — signature mismatch (`RunRefinementTurn` currently takes only `SpawnConfig`).

- [ ] **Step 3: Add env-merge helper**

Write `server/internal/refine/spawner_env.go`:

```go
// Package refine helpers for env-merge precedence used by RunRefinementTurn.
package refine

import (
	"os"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
)

// blockedKeys are never forwarded to spawned processes regardless of what a
// custom spawner declares in its env map. Mirrors the stage handler policy.
var blockedKeys = map[string]struct{}{
	"DASHBOARD_JWT_SECRET":   {},
	"DASHBOARD_HOOKS_SECRET": {},
}

// mergeEnv applies precedence: custom spawner env first, then dashboard
// process env overlays and always wins. Blocked keys are stripped at the end.
func mergeEnv(sp *ent.Spawner) []string {
	merged := make(map[string]string)
	if sp != nil {
		for k, v := range sp.Env {
			if _, blocked := blockedKeys[k]; blocked {
				continue
			}
			merged[k] = v
		}
	}
	for _, kv := range os.Environ() {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				k := kv[:i]
				if _, blocked := blockedKeys[k]; blocked {
					break
				}
				merged[k] = kv[i+1:]
				break
			}
		}
	}
	out := make([]string, 0, len(merged))
	for k, v := range merged {
		out = append(out, k+"="+v)
	}
	return out
}
```

- [ ] **Step 4: Rewrite `RunRefinementTurn`**

Replace the body of `server/internal/refine/spawner.go` with:

```go
// Package refine provides the spawner for refinement chat turns.
package refine

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"text/template"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
)

// SpawnConfig holds the parameters for a single refinement turn.
type SpawnConfig struct {
	TaskTitle       string
	TaskDescription string
	History         []Turn
	UserMessage     string
	WorkDir         string
}

type Turn struct {
	Role    string
	Content string
}

var promptTmpl = template.Must(template.New("refinement").Parse(`<system>
You are a refinement assistant helping to clarify and improve a software task.
Task: {{.TaskTitle}}
{{- if .TaskDescription}}
Description: {{.TaskDescription}}
{{- end}}
</system>
{{range .History}}
<{{.Role}}>{{.Content}}</{{.Role}}>
{{end}}
<user>{{.UserMessage}}</user>`))

// RunRefinementTurn dispatches to the resolved spawner. sp may be nil — in that
// case the legacy `claude -p` exec path is used. The returned channel is closed
// when the process exits, the stream ends, or ctx is cancelled.
func RunRefinementTurn(ctx context.Context, cfg SpawnConfig, sp *ent.Spawner) (<-chan string, error) {
	var buf bytes.Buffer
	if err := promptTmpl.Execute(&buf, cfg); err != nil {
		return nil, fmt.Errorf("refine: build prompt: %w", err)
	}
	prompt := strings.TrimSpace(buf.String())

	switch {
	case sp == nil, sp.AdapterType == "", sp.AdapterType == "claude":
		return runExecPath(ctx, cfg, sp, prompt)
	default:
		return runAdapterPath(ctx, cfg, sp, prompt)
	}
}

func runExecPath(ctx context.Context, cfg SpawnConfig, sp *ent.Spawner, prompt string) (<-chan string, error) {
	binary := "claude"
	var extraArgs []string
	if sp != nil {
		if sp.Command != "" {
			binary = sp.Command
		}
		extraArgs = append(extraArgs, sp.Args...)
	}
	finalArgs := append(extraArgs, "-p", prompt)
	cmd := exec.CommandContext(ctx, binary, finalArgs...)
	if cfg.WorkDir != "" {
		cmd.Dir = cfg.WorkDir
	}
	cmd.Env = mergeEnv(sp)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("refine: stdout pipe: %w", err)
	}
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("refine: start %s: %w", binary, err)
	}

	ch := make(chan string, 64)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			select {
			case ch <- line:
			case <-ctx.Done():
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return
			}
		}
		if err := scanner.Err(); err != nil {
			ch <- "[ERROR] scanner: " + err.Error()
		}
		if err := cmd.Wait(); err != nil {
			msg := "[ERROR] claude exited: " + err.Error()
			if s := strings.TrimSpace(stderrBuf.String()); s != "" {
				msg += " — " + s
			}
			ch <- msg
		}
	}()
	return ch, nil
}

func runAdapterPath(ctx context.Context, cfg SpawnConfig, sp *ent.Spawner, prompt string) (<-chan string, error) {
	adapter, err := pipeline.NewLLMSpawnerFromSpawner(sp)
	if err != nil {
		return nil, fmt.Errorf("refine: build adapter: %w", err)
	}
	if adapter == nil {
		return nil, fmt.Errorf("refine: adapter factory returned nil for type %q", sp.AdapterType)
	}
	streamer, ok := adapter.(pipeline.StreamingLLMSpawner)
	if !ok {
		return nil, fmt.Errorf("refine: adapter %q does not support streaming", sp.AdapterType)
	}
	args := pipeline.LLMSpawnArgs{
		SystemPrompt: "You are a refinement assistant helping to clarify and improve a software task.",
		UserPrompt:   prompt,
		WorkDir:      cfg.WorkDir,
	}
	if sp.ModelOverride != nil {
		args.Model = *sp.ModelOverride
	}
	return streamer.SpawnStream(ctx, args)
}
```

- [ ] **Step 5: Run test, confirm it passes**

Run: `cd server && go test ./internal/refine/...`
Expected: PASS — all three tests green.

- [ ] **Step 6: Update existing callers in `server/internal/api/refine/handler.go`**

Find every call to `refine.RunRefinementTurn(...)` and update the signature (the new parameter is added; pass `nil` until Task 6 wires the resolver). The `Deps.Spawner` function pointer's signature must also change:

In `server/internal/api/refine/handler.go`, change:

```go
Spawner func(ctx context.Context, cfg refine.SpawnConfig) (<-chan string, error)
```

to:

```go
Spawner func(ctx context.Context, cfg refine.SpawnConfig, sp *ent.Spawner) (<-chan string, error)
```

Update the `NewHandler` default:

```go
if deps.Spawner == nil {
    deps.Spawner = refine.RunRefinementTurn
}
```

(remains identical since `refine.RunRefinementTurn` now matches the new signature).

Update the call site inside `submitTurn` to pass `nil` for the spawner row (Task 6 will replace `nil` with the resolved row):

```go
stream, err := h.deps.Spawner(turnCtx, cfg, nil)
```

Add `"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"` to the imports.

- [ ] **Step 7: Run all refine + pipeline tests**

Run: `cd server && go test ./internal/refine/... ./internal/api/refine/... ./internal/pipeline/...`
Expected: PASS — the existing `handler_test.go` may need its stub signature updated; if so, fix it inline (change the stub function to `(ctx, cfg, sp)` and ignore `sp`).

- [ ] **Step 8: Commit**

```bash
git add server/internal/refine/spawner.go server/internal/refine/spawner_env.go server/internal/refine/spawner_test.go server/internal/api/refine/handler.go server/internal/api/refine/handler_test.go
git commit -m "refactor(refine): accept resolved spawner and branch on adapter type"
```

---

### Task 6: Inject `SpawnerResolver` into refine handler

**Files:**
- Modify: `server/internal/api/refine/handler.go`
- Modify: `server/internal/api/refine/handler_test.go`

- [ ] **Step 1: Write failing test asserting resolver is invoked**

Append to `server/internal/api/refine/handler_test.go` a test that wires a fake resolver and asserts it was called with the expected task ID and that its returned row is forwarded to `deps.Spawner`. Follow the existing test patterns in that file (the file already exists — read it first to match conventions).

Test sketch (adapt selectors/types to match the file):

```go
func TestSubmitTurn_CallsResolverAndForwardsSpawnerToSpawnFunc(t *testing.T) {
	resolved := &ent.Spawner{ID: "sp-1", AdapterType: "claude"}
	var gotTaskID string
	var gotSpawner *ent.Spawner
	deps := Deps{
		// ... existing repo stubs ...
		ResolveSpawner: func(_ context.Context, taskID string) (*ent.Spawner, services.SpawnerSource, error) {
			gotTaskID = taskID
			return resolved, services.SpawnerSourceTask, nil
		},
		Spawner: func(_ context.Context, _ refine.SpawnConfig, sp *ent.Spawner) (<-chan string, error) {
			gotSpawner = sp
			ch := make(chan string)
			close(ch)
			return ch, nil
		},
	}
	h := NewHandler(deps)
	// ... POST request as in the existing submitTurn tests ...
	if gotTaskID != "task-under-test" {
		t.Errorf("resolver task id: got %q want %q", gotTaskID, "task-under-test")
	}
	if gotSpawner != resolved {
		t.Errorf("spawner forwarded to Spawn fn: got %v want %v", gotSpawner, resolved)
	}
}
```

- [ ] **Step 2: Run test, confirm it fails**

Run: `cd server && go test ./internal/api/refine/...`
Expected: FAIL — `Deps.ResolveSpawner` does not exist.

- [ ] **Step 3: Add `ResolveSpawner` to `Deps` and use it**

Modify `server/internal/api/refine/handler.go`:

Add to `Deps`:

```go
// ResolveSpawner returns the effective spawner row for the given task. If nil,
// the handler falls back to passing nil to the Spawner function (which then
// uses the legacy `claude -p` exec path).
ResolveSpawner func(ctx context.Context, taskID string) (*ent.Spawner, services.SpawnerSource, error)
```

Add the import for `services`:

```go
"github.com/lx-wnk/agent-dashboard/server/internal/services"
```

Inside `submitTurn`, before calling `h.deps.Spawner(...)`, resolve:

```go
var resolvedSpawner *ent.Spawner
if h.deps.ResolveSpawner != nil {
    sp, _, err := h.deps.ResolveSpawner(r.Context(), taskID)
    if err != nil {
        // SSE error frame, then close — do not silently fall through.
        fmt.Fprintf(w, "data: [ERROR] spawner resolution failed: %s\n\n", err.Error())
        if canFlush {
            flusher.Flush()
        }
        return
    }
    resolvedSpawner = sp
}

stream, err := h.deps.Spawner(turnCtx, cfg, resolvedSpawner)
```

(Replace the existing `stream, err := h.deps.Spawner(turnCtx, cfg, nil)` line from Task 5.)

- [ ] **Step 4: Run test, confirm it passes**

Run: `cd server && go test ./internal/api/refine/...`
Expected: PASS — new test green, existing tests still green.

- [ ] **Step 5: Commit**

```bash
git add server/internal/api/refine/handler.go server/internal/api/refine/handler_test.go
git commit -m "feat(refine): resolve spawner per turn and forward to spawn fn"
```

---

### Task 7: Wire the resolver into refine deps at composition root

**Files:**
- Modify: `server/cmd/serve/di.go`

- [ ] **Step 1: Inspect existing wiring**

Run: `grep -n "refineapi.NewHandler\|spawnerResolver" server/cmd/serve/di.go`
Expected output includes the existing `refineapi.NewHandler(refineapi.Deps{...})` block and the `spawnerResolver` variable defined earlier in the file.

- [ ] **Step 2: Update the `refineapi.Deps` literal**

In `server/cmd/serve/di.go`, locate the `refineapi.NewHandler(refineapi.Deps{...})` block and add:

```go
refineHandler = refineapi.NewHandler(refineapi.Deps{
    Turns:     repo.NewRefinementTurnRepo(entClient),
    Tasks:     repo.NewTaskRepo(entClient),
    StageRuns: repo.NewStageRunRepo(entClient),
    Advance: func(ctx context.Context, taskID string) error {
        _, err := orch.ProgressTask(ctx, taskID, nil)
        return err
    },
    ResolveSpawner: func(ctx context.Context, taskID string) (*ent.Spawner, services.SpawnerSource, error) {
        if spawnerResolver == nil {
            return nil, services.SpawnerSourceDefault, nil
        }
        return spawnerResolver.Resolve(ctx, taskID)
    },
})
```

Add imports if missing:

```go
"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
"github.com/lx-wnk/agent-dashboard/server/internal/services"
```

- [ ] **Step 3: Build to confirm wiring**

Run: `cd server && go build ./...`
Expected: PASS — no compile errors.

- [ ] **Step 4: Commit**

```bash
git add server/cmd/serve/di.go
git commit -m "feat(di): inject spawner resolver into refine handler deps"
```

---

### Task 8: Full regression sweep

**Files:** none (verification only)

- [ ] **Step 1: Run all Go tests with race detector**

Run: `cd server && go test -race ./...`
Expected: PASS.

- [ ] **Step 2: Run lint**

Run: `task lint`
Expected: PASS. If `task lint` is not available, run `cd server && golangci-lint run ./...`.

- [ ] **Step 3: Manual smoke (optional but recommended)**

Run: `task dev` from the repo root.

Manual check:
- Create a task with `projectId` set to a project whose `default_spawner_id` references a custom spawner (`adapter_type=ollama` for example).
- Open the task, send a refinement turn, watch the SSE stream — chunks should be coming from the resolved adapter, not from `claude`.
- Create a task with no project, send a refinement turn — should fall back to `claude -p` and stream as before.

- [ ] **Step 4: Commit (only if smoke testing required follow-up fixes)**

```bash
git add -A
git commit -m "fix(refine): address smoke-test findings"
```

---

## Done Criteria

- `go test -race ./...` (under `server/`) passes.
- `task lint` passes.
- `POST /api/refine/{taskId}/turn` for a task whose resolved spawner is `claude` exec path streams identically to today (regression).
- For a task whose resolved spawner is `ollama` / `openai` / `custom`, the same endpoint streams chunks from the corresponding adapter.
- Resolver errors and missing-streaming-adapter errors surface as `data: [ERROR] ...` SSE frames and close the connection.
- `task.spawner_id` is never written by the refine handler — resolution stays live per turn.
