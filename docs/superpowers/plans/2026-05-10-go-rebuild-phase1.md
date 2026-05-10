# Go Rebuild — Phase 1: Foundation + Agent Monitoring

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A working Go HTTP server on `:13120` that monitors running Claude Code agents via `/api/agents` REST and `/api/agents/stream` SSE — replacing the Node.js server for agent monitoring, with Vue SPA embedded and all tooling in place.

**Architecture:** Go workspace with two active modules in Phase 1: `sdk/` (shared types) and `server/` (HTTP server + business logic). The server runs chi HTTP, reads Claude JSONL session logs via tail-read, scans processes via `ps`/`lsof`, and broadcasts agent state via SSE. Full auth (GitHub OAuth) deferred to Phase 2; JWT verification middleware is included.

**Tech Stack:** Go 1.23+, go-chi/chi v5, spf13/cobra, knadh/koanf, google/wire, log/slog, testify/require, golangci-lint, air, go-task/task

---

> **Go module path:** Use `github.com/lx-wnk/agent-dashboard` throughout. Replace `lx-wnk` with your actual GitHub username before running `go mod init`.

---

## File Map

```
go.work                                         ← workspace root
go.work.sum
Taskfile.yml                                    ← task runner
.golangci.yml                                   ← linter config
.air.toml                                       ← live reload
.github/workflows/ci.yml                        ← CI pipeline
.testcoverage.yml                               ← coverage threshold (server module)

sdk/
  go.mod
  types.go                                      ← Agent, TokenUsage, SessionMeta, SubAgent

server/
  go.mod
  cmd/serve/
    main.go                                     ← cobra root + serve subcommand
    wire.go                                     ← wire DI descriptor (wireinject build tag)
    wire_gen.go                                 ← wire generated output (committed)
  internal/
    platform/
      platform.go                               ← IS_LINUX constant
    config/
      config.go                                 ← koanf config struct + loader
    api/
      errors.go                                 ← AppError, ErrNotFound, ErrConflict, errorHandler
      encode.go                                 ← encode() / decode() JSON helpers
      middleware.go                             ← slogMiddleware, securityHeaders
      spa.go                                    ← Vue SPA embed handler
      router.go                                 ← chi router + all route mounts
      server.go                                 ← Server struct, ListenAndServe, graceful shutdown
    auth/
      jwt.go                                    ← signJwt, verifyJwt (HS256, matches TS impl)
      jwt_test.go
      middleware.go                             ← requireAuth chi middleware
    scanner/
      scanner.go                                ← scanProcesses(), parseLsofBatch(), parseElapsed()
      scanner_test.go
    parser/
      encoder.go                                ← encodePath()
      encoder_test.go
      parser.go                                 ← tailRead(), parseSession(), findSessionForProject()
      parser_test.go
      pricing.go                                ← MODEL_PRICING, EstimateCost()
      pricing_test.go
    sse/
      broadcaster.go                            ← Broadcaster (buffered chan, non-blocking send)
      broadcaster_test.go
    merger/
      merger.go                                 ← GetAgents(), CalculateStatus()
      merger_test.go
    api/agents/
      handler.go                                ← GET /api/agents, GET /api/agents/stream
    api/system/
      handler.go                                ← GET /api/system/health
  frontend/
    embed.go                                    ← //go:embed dist/* var Embedded embed.FS
    placeholder/                                ← placeholder dist/ for build without Vue
      dist/
        index.html
```

---

## Task 1: Workspace + Module Initialization

**Files:**
- Create: `go.work`
- Create: `sdk/go.mod`
- Create: `server/go.mod`

- [ ] **Step 1: Verify Go version**

```bash
go version
```
Expected: `go version go1.23` or higher. If lower, install from https://go.dev/dl/

- [ ] **Step 2: Initialize sdk module**

```bash
mkdir -p sdk && cd sdk && go mod init github.com/lx-wnk/agent-dashboard/sdk && cd ..
```

- [ ] **Step 3: Initialize server module**

```bash
mkdir -p server && cd server && go mod init github.com/lx-wnk/agent-dashboard/server && cd ..
```

- [ ] **Step 4: Create go.work**

```bash
go work init ./sdk ./server
```

Expected: `go.work` created at repo root.

- [ ] **Step 5: Verify go.work content**

```bash
cat go.work
```
Expected output:
```
go 1.23

use (
	./sdk
	./server
)
```

- [ ] **Step 6: Add server dependencies**

```bash
cd server
go get github.com/go-chi/chi/v5@latest
go get github.com/spf13/cobra@latest
go get github.com/knadh/koanf/v2@latest
go get github.com/knadh/koanf/providers/env/v2@latest
go get github.com/knadh/koanf/providers/file/v2@latest
go get github.com/knadh/koanf/parsers/json/v2@latest
go get github.com/stretchr/testify@latest
go get entgo.io/ent@latest
go get golang.org/x/sync@latest
cd ..
```

- [ ] **Step 7: Update go.work.sum**

```bash
go work sync
```

- [ ] **Step 8: Commit**

```bash
git add go.work go.work.sum sdk/go.mod server/go.mod server/go.sum
git commit -m "init: initialize Go workspace with sdk and server modules"
```

---

## Task 2: Tooling — Taskfile, golangci-lint, air

**Files:**
- Create: `Taskfile.yml`
- Create: `.golangci.yml`
- Create: `.air.toml`

- [ ] **Step 1: Create Taskfile.yml**

```yaml
# Taskfile.yml
version: '3'

tasks:
  default:
    cmds: [task --list]

  dev:
    desc: Start server with live reload (requires air)
    dir: server
    cmd: air -c ../.air.toml

  build:
    desc: Build server binary
    dir: server
    cmd: go build -o ../bin/agent-dashboard ./cmd/serve/...

  test:
    desc: Run all tests (all modules)
    cmds:
      - cd sdk && go test -race ./...
      - cd server && go test -race ./...

  test:watch:
    desc: Watch mode tests (server module)
    dir: server
    cmd: go test -race ./... -count=1

  lint:
    desc: Run golangci-lint (all modules)
    cmds:
      - cd sdk && golangci-lint run ./...
      - cd server && golangci-lint run ./...

  generate:
    desc: Run go generate (wire + ent)
    dir: server
    cmd: go generate ./...

  vuln:
    desc: Run govulncheck
    cmds:
      - cd sdk && govulncheck ./...
      - cd server && govulncheck ./...

  wire:
    desc: Regenerate wire DI
    dir: server
    cmd: wire ./cmd/serve/...

  fmt:
    desc: Format all Go code
    cmd: gofmt -w sdk/ server/
```

- [ ] **Step 2: Create .golangci.yml**

```yaml
# .golangci.yml
linters:
  enable:
    - govet
    - staticcheck
    - errcheck
    - revive
    - gosec
    - misspell
    - gofmt

linters-settings:
  revive:
    rules:
      - name: exported
        disabled: false

issues:
  exclude-rules:
    - path: "_test.go"
      linters: [gosec, errcheck]
    - path: "wire_gen.go"
      linters: [all]
    - path: "ent/"
      linters: [all]

run:
  timeout: 5m
```

- [ ] **Step 3: Create .air.toml**

```toml
# .air.toml
root = "."
tmp_dir = ".air-tmp"

[build]
  # Build the server binary — run from repo root, air chdir to server/
  cmd = "cd server && go build -o ../.air-tmp/agent-dashboard ./cmd/serve/..."
  bin = ".air-tmp/agent-dashboard"
  full_bin = ".air-tmp/agent-dashboard serve"
  include_ext = ["go"]
  exclude_dir = ["server/frontend", "server/internal/db/ent", "node_modules", "dist"]
  delay = 500

[log]
  time = true

[color]
  main = "yellow"
  watcher = "cyan"
  build = "green"
  runner = "magenta"

[misc]
  clean_on_exit = true
```

- [ ] **Step 4: Install tools**

```bash
go install github.com/go-task/task/v3/cmd/task@latest
go install github.com/air-verse/air@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install golang.org/x/vuln/cmd/govulncheck@latest
go install github.com/google/wire/cmd/wire@latest
go install github.com/vektra/mockery/v3@latest
```

- [ ] **Step 5: Verify task works**

```bash
task --list
```
Expected: lists all tasks defined above.

- [ ] **Step 6: Commit**

```bash
git add Taskfile.yml .golangci.yml .air.toml
git commit -m "init: add Taskfile, golangci-lint config, air live reload"
```

---

## Task 3: CI Pipeline

**Files:**
- Create: `.github/workflows/ci.yml`
- Create: `server/.testcoverage.yml`
- Create: `sdk/.testcoverage.yml`

- [ ] **Step 1: Create .github/workflows/ci.yml**

```yaml
# .github/workflows/ci.yml
name: CI

on:
  push:
    branches: [main, "feat/**"]
  pull_request:
    branches: [main]

jobs:
  test:
    name: Test (${{ matrix.module }})
    runs-on: ubuntu-latest
    strategy:
      fail-fast: false
      matrix:
        module: [server, sdk]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: ${{ matrix.module }}/go.mod
          cache-dependency-path: go.work.sum

      - name: Test
        working-directory: ${{ matrix.module }}
        run: go test -race -coverprofile=coverage.out ./...

      - name: Coverage
        uses: vladopajic/go-test-coverage@v2
        with:
          config: ${{ matrix.module }}/.testcoverage.yml
          profile: ${{ matrix.module }}/coverage.out

  lint:
    name: Lint (${{ matrix.module }})
    runs-on: ubuntu-latest
    strategy:
      fail-fast: false
      matrix:
        module: [server, sdk]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: ${{ matrix.module }}/go.mod
      - uses: golangci/golangci-lint-action@v6
        with:
          working-directory: ${{ matrix.module }}
          version: latest

  security:
    name: Security
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: stable
      - name: govulncheck
        run: |
          go install golang.org/x/vuln/cmd/govulncheck@latest
          for mod in server sdk; do
            echo "=== $mod ===" && (cd $mod && govulncheck ./...)
          done

  build:
    name: Build
    runs-on: ubuntu-latest
    needs: [test, lint]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: stable
      - name: Build server binary
        working-directory: server
        run: go build -o /dev/null ./cmd/serve/...

  pr-checks:
    name: PR Validation
    runs-on: ubuntu-latest
    if: github.event_name == 'pull_request'
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: wagoid/commitlint-github-action@v6
```

- [ ] **Step 2: Create server/.testcoverage.yml**

```yaml
# server/.testcoverage.yml
threshold:
  total: 70
```

- [ ] **Step 3: Create sdk/.testcoverage.yml**

```yaml
# sdk/.testcoverage.yml
threshold:
  total: 70
```

- [ ] **Step 4: Commit**

```bash
git add .github/ server/.testcoverage.yml sdk/.testcoverage.yml
git commit -m "ci: add GitHub Actions pipeline with test, lint, security, build jobs"
```

---

## Task 4: SDK Types

**Files:**
- Create: `sdk/types.go`

- [ ] **Step 1: Create sdk/types.go**

```go
// sdk/types.go
package sdk

// TokenUsage mirrors the Claude Code session token counters.
type TokenUsage struct {
	InputTokens        int `json:"inputTokens"`
	OutputTokens       int `json:"outputTokens"`
	CacheCreationTokens int `json:"cacheCreationTokens"`
	CacheReadTokens    int `json:"cacheReadTokens"`
}

// SessionMeta is read from ~/.claude/usage-data/session-meta/{sessionId}.json
type SessionMeta struct {
	InputTokens   int    `json:"inputTokens"`
	OutputTokens  int    `json:"outputTokens"`
	LinesAdded    int    `json:"linesAdded"`
	LinesRemoved  int    `json:"linesRemoved"`
	FilesModified int    `json:"filesModified"`
	GitCommits    int    `json:"gitCommits"`
	ToolErrors    int    `json:"toolErrors"`
	UsesMCP       bool   `json:"usesMcp"`
	FirstPrompt   string `json:"firstPrompt"`
}

// SubAgent represents a sub-agent spawned by a parent Claude session.
type SubAgent struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	Status        string `json:"status"` // "active" | "completed"
	CurrentAction string `json:"currentAction"`
	SessionFile   string `json:"sessionFile"`
}

// TaskInfo is a task tracked by Claude Code's internal task list.
type TaskInfo struct {
	ID      string `json:"id"`
	Subject string `json:"subject"`
	Status  string `json:"status"`
}

// AgentStatus is the computed activity state of an agent process.
type AgentStatus string

const (
	AgentStatusActive  AgentStatus = "active"
	AgentStatusWaiting AgentStatus = "waiting"
	AgentStatusIdle    AgentStatus = "idle"
)

// Agent is the unified view of a running Claude Code process.
type Agent struct {
	PID                      int            `json:"pid"`
	SessionID                string         `json:"sessionId"`
	ProjectPath              string         `json:"projectPath"`
	ProjectName              string         `json:"projectName"`
	CWD                      string         `json:"cwd"`
	Entrypoint               string         `json:"entrypoint"` // "cli" | "desktop" | "unknown"
	Status                   AgentStatus    `json:"status"`
	Uptime                   int64          `json:"uptime"` // seconds
	LastActivity             string         `json:"lastActivity"`
	CurrentAction            string         `json:"currentAction"`
	LastTools                []string       `json:"lastTools"`
	Tasks                    []TaskInfo     `json:"tasks"`
	Subagents                []SubAgent     `json:"subagents"`
	TokenUsage               TokenUsage     `json:"tokenUsage"`
	CostEstimate             float64        `json:"costEstimate"`
	CacheCreationCostEstimate float64       `json:"cacheCreationCostEstimate"`
	CacheReadCostEstimate    float64        `json:"cacheReadCostEstimate"`
	HealthScore              int            `json:"healthScore"`
	Model                    string         `json:"model"`
	CodeVersion              string         `json:"codeVersion"`
	ConversationTurns        int            `json:"conversationTurns"`
	ToolCounts               map[string]int `json:"toolCounts"`
	Meta                     *SessionMeta   `json:"meta"`
	ChannelAvailable         bool           `json:"channelAvailable"`
	LastOutput               string         `json:"lastOutput"`
	ConvergenceAlert         bool           `json:"convergenceAlert"`
	ConvergenceToolName      string         `json:"convergenceToolName"`
	ErrorState               string         `json:"errorState"` // "" | "quota_exhausted" | "rate_limited" | "auth_failed"
	PipelineTaskID           string         `json:"pipelineTaskId,omitempty"`
	PipelineTaskTitle        string         `json:"pipelineTaskTitle,omitempty"`
	Machine                  string         `json:"machine,omitempty"`
}
```

- [ ] **Step 2: Verify sdk compiles**

```bash
cd sdk && go build ./...
```
Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add sdk/
git commit -m "feat(sdk): add shared Agent, TokenUsage, SessionMeta types"
```

---

## Task 5: Platform + Config

**Files:**
- Create: `server/internal/platform/platform.go`
- Create: `server/internal/config/config.go`

- [ ] **Step 1: Create platform.go**

```go
// server/internal/platform/platform.go
package platform

import "runtime"

// IsLinux is true when running on Linux. Used to switch between
// /proc/<pid>/cwd (Linux) and lsof (macOS) for process CWD resolution.
var IsLinux = runtime.GOOS == "linux"
```

- [ ] **Step 2: Create config.go**

```go
// server/internal/config/config.go
package config

import (
	"fmt"
	"os"
	"time"

	"github.com/knadh/koanf/parsers/json/v2"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file/v2"
	"github.com/knadh/koanf/v2"
)

// Config holds all server configuration. Keys match environment variable
// names after stripping the DASHBOARD_ prefix and lowercasing.
type Config struct {
	Host                   string        `koanf:"host"`
	Port                   int           `koanf:"port"`
	JWTSecret              string        `koanf:"jwt_secret"`
	GitHubClientID         string        `koanf:"github_client_id"`
	GitHubClientSecret     string        `koanf:"github_client_secret"`
	DBPath                 string        `koanf:"db_path"`
	SSEIntervalMs          int           `koanf:"sse_interval_ms"`
	ShutdownTimeoutSeconds int           `koanf:"shutdown_timeout_seconds"`
	PluginDir              string        `koanf:"plugin_dir"`
	AllowGitPush           bool          `koanf:"allow_git_push"`
	HooksSecret            string        `koanf:"hooks_secret"`
	HooksDebounceMs        int           `koanf:"hooks_debounce_ms"`
}

// Defaults returns a Config populated with safe defaults.
func Defaults() Config {
	home, _ := os.UserHomeDir()
	return Config{
		Host:                   "127.0.0.1",
		Port:                   13120,
		DBPath:                 home + "/.claude/dashboard-tasks.db",
		SSEIntervalMs:          3000,
		ShutdownTimeoutSeconds: 10,
		HooksDebounceMs:        100,
	}
}

// Load returns a Config merged from defaults → optional JSON file → env vars.
// Env vars are prefixed with DASHBOARD_ and case-insensitive.
func Load(cfgFile string) (Config, error) {
	k := koanf.New(".")
	cfg := Defaults()

	// Load defaults as base
	if err := k.Load(structProvider(cfg), nil); err != nil {
		return Config{}, fmt.Errorf("config defaults: %w", err)
	}

	// Optional file override
	if cfgFile != "" {
		if err := k.Load(file.Provider(cfgFile), json.Parser()); err != nil {
			return Config{}, fmt.Errorf("config file %s: %w", cfgFile, err)
		}
	}

	// Env vars: DASHBOARD_HOST → host, DASHBOARD_JWT_SECRET → jwt_secret
	if err := k.Load(env.Provider("DASHBOARD_", ".", func(s string) string {
		return strings.ToLower(strings.TrimPrefix(s, "DASHBOARD_"))
	}), nil); err != nil {
		return Config{}, fmt.Errorf("config env: %w", err)
	}

	if err := k.Unmarshal("", &cfg); err != nil {
		return Config{}, fmt.Errorf("config unmarshal: %w", err)
	}
	return cfg, nil
}

// ShutdownTimeout returns the graceful shutdown duration.
func (c Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.ShutdownTimeoutSeconds) * time.Second
}

// Addr returns the bind address string.
func (c Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}
```

> **Note:** `strings` import is missing above — add `"strings"` to the import block. Also `structProvider` is a koanf helper — use `confmap.Provider(defaults, ".")` from `github.com/knadh/koanf/providers/confmap/v2` instead. Add that dependency:

```bash
cd server && go get github.com/knadh/koanf/providers/confmap/v2
```

Then replace `structProvider(cfg)` with:
```go
import "github.com/knadh/koanf/providers/confmap/v2"

// Convert struct to map for confmap provider
defaults := map[string]any{
    "host":                     cfg.Host,
    "port":                     cfg.Port,
    "db_path":                  cfg.DBPath,
    "sse_interval_ms":          cfg.SSEIntervalMs,
    "shutdown_timeout_seconds": cfg.ShutdownTimeoutSeconds,
    "hooks_debounce_ms":        cfg.HooksDebounceMs,
}
k.Load(confmap.Provider(defaults, "."), nil)
```

- [ ] **Step 3: Verify compile**

```bash
cd server && go build ./internal/config/... ./internal/platform/...
```
Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add server/internal/platform/ server/internal/config/
git commit -m "feat(server): add platform detection and koanf config loader"
```

---

## Task 6: Error Handling + JSON Helpers

**Files:**
- Create: `server/internal/api/errors.go`
- Create: `server/internal/api/encode.go`

- [ ] **Step 1: Create errors.go**

```go
// server/internal/api/errors.go
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// Sentinel errors — use errors.Is() to check.
var (
	ErrNotFound   = errors.New("not found")
	ErrConflict   = errors.New("conflict")
	ErrBadRequest = errors.New("bad request")
	ErrForbidden  = errors.New("forbidden")
)

// AppError carries an explicit HTTP status code.
// Use when a handler needs precise control over the response status.
type AppError struct {
	Status  int    `json:"-"`
	Message string `json:"error"`
}

func (e *AppError) Error() string { return e.Message }

// NewAppError creates an AppError with the given status and message.
func NewAppError(status int, msg string) *AppError {
	return &AppError{Status: status, Message: msg}
}

// handlerFunc is a handler that returns an error instead of writing it directly.
// The returned error is mapped to an HTTP status by errorMiddleware.
type handlerFunc func(http.ResponseWriter, *http.Request) error

// errorMiddleware wraps a handlerFunc and maps errors to HTTP responses.
// This is the central place where domain errors become HTTP status codes.
func errorMiddleware(next handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := next(w, r); err == nil {
			return
		} else {
			var appErr *AppError
			switch {
			case errors.As(err, &appErr):
				encode(w, appErr.Status, appErr)
			case errors.Is(err, ErrNotFound):
				encode(w, http.StatusNotFound, map[string]string{"error": "not found"})
			case errors.Is(err, ErrConflict):
				encode(w, http.StatusConflict, map[string]string{"error": "conflict"})
			case errors.Is(err, ErrBadRequest):
				encode(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
			case errors.Is(err, ErrForbidden):
				encode(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			default:
				slog.Error("unhandled handler error", "err", err, "path", r.URL.Path)
				encode(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			}
		}
	}
}
```

- [ ] **Step 2: Create encode.go**

```go
// server/internal/api/encode.go
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// encode writes v as JSON with the given status code.
func encode[T any](w http.ResponseWriter, status int, v T) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	return nil
}

// decode reads JSON from r.Body into v.
func decode[T any](r *http.Request) (T, error) {
	var v T
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		return v, fmt.Errorf("%w: %s", ErrBadRequest, err.Error())
	}
	return v, nil
}
```

> **Go generics note:** `encode[T any]` uses Go 1.18+ generics. The compiler infers `T` from the argument — call as `encode(w, 200, myStruct)`, no explicit type parameter needed.

- [ ] **Step 3: Write tests for errors**

```go
// server/internal/api/errors_test.go
package api_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/lx-wnk/agent-dashboard/server/internal/api"
)

func TestErrorMiddleware_NotFound(t *testing.T) {
	handler := api.ErrorMiddleware(func(w http.ResponseWriter, r *http.Request) error {
		return api.ErrNotFound
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestErrorMiddleware_AppError(t *testing.T) {
	handler := api.ErrorMiddleware(func(w http.ResponseWriter, r *http.Request) error {
		return api.NewAppError(http.StatusTeapot, "I am a teapot")
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	require.Equal(t, http.StatusTeapot, rec.Code)
}

func TestErrorMiddleware_Unknown(t *testing.T) {
	handler := api.ErrorMiddleware(func(w http.ResponseWriter, r *http.Request) error {
		return errors.New("boom")
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}
```

> **Note:** Export `errorMiddleware` as `ErrorMiddleware` (capital E) for tests in the `api_test` package. Update errors.go accordingly.

- [ ] **Step 4: Run tests**

```bash
cd server && go test ./internal/api/... -v -run TestErrorMiddleware
```
Expected:
```
--- PASS: TestErrorMiddleware_NotFound
--- PASS: TestErrorMiddleware_AppError
--- PASS: TestErrorMiddleware_Unknown
```

- [ ] **Step 5: Commit**

```bash
git add server/internal/api/errors.go server/internal/api/errors_test.go server/internal/api/encode.go
git commit -m "feat(server/api): add error middleware, AppError, sentinel errors, JSON encode helpers"
```

---

## Task 7: Process Scanner

**Files:**
- Create: `server/internal/scanner/scanner.go`
- Create: `server/internal/scanner/scanner_test.go`

- [ ] **Step 1: Write failing tests**

```go
// server/internal/scanner/scanner_test.go
package scanner_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/lx-wnk/agent-dashboard/server/internal/scanner"
)

func TestParseElapsedTime(t *testing.T) {
	tests := []struct {
		name  string
		etime string
		want  int64
	}{
		{"seconds only", "42", 42},
		{"minutes and seconds", "05:30", 330},
		{"hours minutes seconds", "01:05:30", 3930},
		{"days hours minutes seconds", "2-01:05:30", 176730},
		{"leading space", "  12", 12},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scanner.ParseElapsedTime(tt.etime)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseLsofBatch(t *testing.T) {
	input := "p1234\nn/home/user/project\np5678\nn/tmp/other\n"
	got := scanner.ParseLsofBatch(input)
	require.Equal(t, "/home/user/project", got[1234])
	require.Equal(t, "/tmp/other", got[5678])
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd server && go test ./internal/scanner/... -v
```
Expected: `FAIL` — scanner package does not exist yet.

- [ ] **Step 3: Create scanner.go**

```go
// server/internal/scanner/scanner.go
package scanner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lx-wnk/agent-dashboard/server/internal/platform"
)

// ProcessInfo holds metadata about a running Claude Code process.
type ProcessInfo struct {
	PID     int
	CWD     string
	Uptime  int64 // seconds
	Command string
}

// ParseElapsedTime converts ps etime format (e.g. "2-01:05:30") to seconds.
// Format: [[DD-]HH:]MM:SS — reversed and split on : after normalizing - to :
func ParseElapsedTime(etime string) int64 {
	parts := strings.Split(strings.ReplaceAll(strings.TrimSpace(etime), "-", ":"), ":")
	// Reverse so parts[0]=seconds, parts[1]=minutes, parts[2]=hours, parts[3]=days
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	multipliers := []int64{1, 60, 3600, 86400}
	var total int64
	for i, p := range parts {
		if i >= len(multipliers) {
			break
		}
		n, _ := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		total += n * multipliers[i]
	}
	return total
}

// ParseLsofBatch parses `lsof -a -d cwd -Fn` output into a pid→cwd map.
// Output format: p<pid>\nn<path>\n per process.
func ParseLsofBatch(stdout string) map[int]string {
	result := make(map[int]string)
	var currentPID int
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "p") {
			pid, err := strconv.Atoi(strings.TrimPrefix(line, "p"))
			if err == nil {
				currentPID = pid
			}
		} else if strings.HasPrefix(line, "n") && currentPID != 0 {
			result[currentPID] = strings.TrimPrefix(line, "n")
			currentPID = 0
		}
	}
	return result
}

func getCWDsLinux(pids []int) map[int]string {
	result := make(map[int]string)
	for _, pid := range pids {
		target, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
		if err == nil {
			result[pid] = target
		}
	}
	return result
}

func getCWDsMac(ctx context.Context, pids []int) map[int]string {
	if len(pids) == 0 {
		return nil
	}
	pidStrs := make([]string, len(pids))
	for i, p := range pids {
		pidStrs[i] = strconv.Itoa(p)
	}
	out, err := exec.CommandContext(ctx, "lsof", append([]string{"-a", "-d", "cwd", "-p", strings.Join(pidStrs, ","), "-Fn"})...).Output()
	if err != nil {
		return nil
	}
	return ParseLsofBatch(string(out))
}

// ScanProcesses returns all running Claude Code processes with their CWDs.
func ScanProcesses(ctx context.Context) ([]ProcessInfo, error) {
	out, err := exec.CommandContext(ctx, "ps", "-eo", "pid,etime,comm").Output()
	if err != nil {
		return nil, fmt.Errorf("ps: %w", err)
	}

	type rawProc struct {
		pid     int
		etime   string
		command string
	}

	var raws []rawProc
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		comm := strings.Join(fields[2:], " ")
		if !strings.HasSuffix(comm, "/claude") && comm != "claude" {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		raws = append(raws, rawProc{pid: pid, etime: fields[1], command: comm})
	}

	pids := make([]int, len(raws))
	for i, r := range raws {
		pids[i] = r.pid
	}

	var cwdMap map[int]string
	if platform.IsLinux {
		cwdMap = getCWDsLinux(pids)
	} else {
		cwdMap = getCWDsMac(ctx, pids)
	}

	var result []ProcessInfo
	for _, r := range raws {
		cwd, ok := cwdMap[r.pid]
		if !ok || cwd == "" || cwd == "/" {
			continue
		}
		result = append(result, ProcessInfo{
			PID:     r.pid,
			CWD:     cwd,
			Uptime:  ParseElapsedTime(r.etime),
			Command: r.command,
		})
	}
	return result, nil
}

// ProjectName returns the last path component of a CWD.
func ProjectName(cwd string) string {
	return filepath.Base(cwd)
}
```

- [ ] **Step 4: Run tests**

```bash
cd server && go test ./internal/scanner/... -v
```
Expected:
```
--- PASS: TestParseElapsedTime/seconds_only
--- PASS: TestParseElapsedTime/minutes_and_seconds
--- PASS: TestParseElapsedTime/hours_minutes_seconds
--- PASS: TestParseElapsedTime/days_hours_minutes_seconds
--- PASS: TestParseElapsedTime/leading_space
--- PASS: TestParseLsofBatch
```

- [ ] **Step 5: Commit**

```bash
git add server/internal/scanner/
git commit -m "feat(server/scanner): add process scanner with ps/lsof and Linux /proc support"
```

---

## Task 8: JSONL Parser + Pricing

**Files:**
- Create: `server/internal/parser/encoder.go`
- Create: `server/internal/parser/encoder_test.go`
- Create: `server/internal/parser/pricing.go`
- Create: `server/internal/parser/pricing_test.go`
- Create: `server/internal/parser/parser.go`
- Create: `server/internal/parser/parser_test.go`

- [ ] **Step 1: Create encoder.go**

```go
// server/internal/parser/encoder.go
package parser

import "strings"

// EncodePath converts an absolute path to Claude Code's directory-encoding scheme.
// Claude replaces /, ., and _ all with - when naming project directories.
func EncodePath(absPath string) string {
	return strings.NewReplacer("/", "-", ".", "-", "_", "-").Replace(absPath)
}
```

- [ ] **Step 2: Create encoder_test.go**

```go
// server/internal/parser/encoder_test.go
package parser_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

func TestEncodePath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"home dir", "/home/user/project", "-home-user-project"},
		{"dot claude", "/home/user/.claude", "-home-user--claude"},
		{"underscore", "/home/user/my_project", "-home-user-my-project"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, parser.EncodePath(tt.input))
		})
	}
}
```

- [ ] **Step 3: Create pricing.go**

```go
// server/internal/parser/pricing.go
package parser

import "github.com/lx-wnk/agent-dashboard/sdk"

// modelPricing stores per-million-token USD prices for known Claude models.
var modelPricing = map[string]struct {
	Input, Output, CacheRead, CacheCreate float64
}{
	"claude-opus-4-6":   {15, 75, 1.5, 18.75},
	"claude-opus-4-0":   {15, 75, 1.5, 18.75},
	"claude-sonnet-4-6": {3, 15, 0.3, 3.75},
	"claude-sonnet-4-5": {3, 15, 0.3, 3.75},
	"claude-haiku-4-5":  {0.8, 4, 0.08, 1},
}

const defaultModel = "claude-sonnet-4-6"

// EstimateCost returns the estimated USD cost for a given token usage + model.
func EstimateCost(usage sdk.TokenUsage, model string) float64 {
	p, ok := modelPricing[model]
	if !ok {
		p = modelPricing[defaultModel]
	}
	const m = 1_000_000.0
	return float64(usage.InputTokens)*p.Input/m +
		float64(usage.OutputTokens)*p.Output/m +
		float64(usage.CacheReadTokens)*p.CacheRead/m +
		float64(usage.CacheCreationTokens)*p.CacheCreate/m
}

// EstimateCacheCreationCost returns only the cache-write cost component.
func EstimateCacheCreationCost(usage sdk.TokenUsage, model string) float64 {
	p, ok := modelPricing[model]
	if !ok {
		p = modelPricing[defaultModel]
	}
	return float64(usage.CacheCreationTokens) * p.CacheCreate / 1_000_000.0
}

// EstimateCacheReadCost returns only the cache-read cost component.
func EstimateCacheReadCost(usage sdk.TokenUsage, model string) float64 {
	p, ok := modelPricing[model]
	if !ok {
		p = modelPricing[defaultModel]
	}
	return float64(usage.CacheReadTokens) * p.CacheRead / 1_000_000.0
}
```

- [ ] **Step 4: Create pricing_test.go**

```go
// server/internal/parser/pricing_test.go
package parser_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

func TestEstimateCost(t *testing.T) {
	usage := sdk.TokenUsage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
	// sonnet-4-6: input $3 + output $15 = $18 per 1M each
	got := parser.EstimateCost(usage, "claude-sonnet-4-6")
	require.InDelta(t, 18.0, got, 0.001)
}

func TestEstimateCost_UnknownModel_UsesDefault(t *testing.T) {
	usage := sdk.TokenUsage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
	got := parser.EstimateCost(usage, "claude-unknown")
	// Falls back to sonnet-4-6 default
	require.InDelta(t, 18.0, got, 0.001)
}
```

- [ ] **Step 5: Create parser.go**

```go
// server/internal/parser/parser.go
package parser

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/lx-wnk/agent-dashboard/sdk"
)

const (
	tailBytes = 32768 // 32KB from end — matches TS implementation
	headBytes = 8192  // 8KB from start — for model/version extraction
)

var (
	uuidRE  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	quotaRE = regexp.MustCompile(`(?i)quota exceeded|usage limit|monthly limit`)
	rateRE  = regexp.MustCompile(`(?i)rate limit|429|too many requests|throttl`)
	authRE  = regexp.MustCompile(`(?i)invalid api key|authentication|unauthorized|401`)
)

// claudeProjectsDir returns ~/.claude/projects
func claudeProjectsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}

// sessionMetaDir returns ~/.claude/usage-data/session-meta
func sessionMetaDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "usage-data", "session-meta")
}

// TailRead reads the last tailBytes of a file. Partial first line is expected.
func TailRead(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", filePath, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", filePath, err)
	}
	size := info.Size()
	readSize := int64(tailBytes)
	if readSize > size {
		readSize = size
	}
	if _, err := f.Seek(size-readSize, io.SeekStart); err != nil {
		return "", fmt.Errorf("seek: %w", err)
	}
	buf := make([]byte, readSize)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return "", fmt.Errorf("read: %w", err)
	}
	return string(buf[:n]), nil
}

// jsonlMessage is the minimal structure of a JSONL session log entry.
type jsonlMessage struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message"`
}

type msgContent struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Model   string          `json:"model"`
	Usage   *struct {
		InputTokens        int `json:"input_tokens"`
		OutputTokens       int `json:"output_tokens"`
		CacheCreationTokens int `json:"cache_creation_input_tokens"`
		CacheReadTokens    int `json:"cache_read_input_tokens"`
	} `json:"usage"`
}

type toolUseBlock struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Input any    `json:"input"`
}

// SessionData is the parsed output of a Claude Code JSONL session log.
type SessionData struct {
	SessionID         string
	ProjectPath       string
	Entrypoint        string // "cli" | "desktop" | "unknown"
	LastActivity      time.Time
	CurrentAction     string
	LastTools         []string
	Tasks             []sdk.TaskInfo
	TokenUsage        sdk.TokenUsage
	Model             string
	ConversationTurns int
	ToolCounts        map[string]int
	LastOutput        string
	ConvergenceAlert  bool
	ConvergenceToolName string
	ErrorState        string
	Meta              *sdk.SessionMeta
}

// FindSessionForProject locates the most recently active JSONL session for cwd.
func FindSessionForProject(cwd string, uptimeSeconds int64) (*SessionData, error) {
	encoded := EncodePath(cwd)
	projectDir := filepath.Join(claudeProjectsDir(), encoded)

	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return nil, fmt.Errorf("readdir %s: %w", projectDir, err)
	}

	// Filter to JSONL files with UUID names
	type candidate struct {
		path  string
		mtime time.Time
	}
	var candidates []candidate
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		id := strings.TrimSuffix(name, ".jsonl")
		if !uuidRE.MatchString(id) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		candidates = append(candidates, candidate{
			path:  filepath.Join(projectDir, name),
			mtime: info.ModTime(),
		})
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no session files in %s", projectDir)
	}

	// Most recently modified first
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].mtime.After(candidates[j].mtime)
	})

	for _, c := range candidates {
		data, err := parseSessionFile(c.path)
		if err != nil {
			continue
		}
		// Only return sessions active within uptime window
		age := time.Since(data.LastActivity)
		if age < time.Duration(uptimeSeconds+10)*time.Second {
			data.SessionID = strings.TrimSuffix(filepath.Base(c.path), ".jsonl")
			data.ProjectPath = cwd
			data.Meta = loadSessionMeta(data.SessionID)
			return data, nil
		}
	}
	return nil, fmt.Errorf("no active session for %s", cwd)
}

func parseSessionFile(path string) (*SessionData, error) {
	content, err := TailRead(path)
	if err != nil {
		return nil, err
	}

	data := &SessionData{
		ToolCounts: make(map[string]int),
		Entrypoint: "unknown",
	}

	var recentToolNames []string

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry jsonlMessage
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Type != "message" {
			continue
		}
		var msg msgContent
		if err := json.Unmarshal(entry.Message, &msg); err != nil {
			continue
		}

		if msg.Role == "assistant" {
			data.ConversationTurns++
			if msg.Model != "" {
				data.Model = msg.Model
			}
			if msg.Usage != nil {
				data.TokenUsage.InputTokens += msg.Usage.InputTokens
				data.TokenUsage.OutputTokens += msg.Usage.OutputTokens
				data.TokenUsage.CacheCreationTokens += msg.Usage.CacheCreationTokens
				data.TokenUsage.CacheReadTokens += msg.Usage.CacheReadTokens
			}

			// Parse content blocks
			var blocks []toolUseBlock
			if err := json.Unmarshal(msg.Content, &blocks); err == nil {
				for _, b := range blocks {
					if b.Type == "tool_use" {
						data.ToolCounts[b.Name]++
						recentToolNames = append(recentToolNames, b.Name)
					} else if b.Type == "text" {
						// Extract text for lastOutput
						if raw, ok := b.Input.(string); ok {
							data.LastOutput = raw
						}
					}
				}
			}
			// Update last activity
			data.LastActivity = time.Now() // approximated; real impl reads timestamp field
		}
	}

	// Last 5 tools for display
	if len(recentToolNames) > 5 {
		data.LastTools = recentToolNames[len(recentToolNames)-5:]
	} else {
		data.LastTools = recentToolNames
	}

	// Convergence detection: last 5 tools identical
	if len(recentToolNames) >= 5 {
		last5 := recentToolNames[len(recentToolNames)-5:]
		allSame := true
		for _, t := range last5[1:] {
			if t != last5[0] {
				allSame = false
				break
			}
		}
		if allSame {
			data.ConvergenceAlert = true
			data.ConvergenceToolName = last5[0]
		}
	}

	return data, nil
}

func loadSessionMeta(sessionID string) *sdk.SessionMeta {
	path := filepath.Join(sessionMetaDir(), sessionID+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var meta sdk.SessionMeta
	if err := json.Unmarshal(b, &meta); err != nil {
		return nil
	}
	return &meta
}

// ensure fs import used — remove if not needed
var _ fs.FileInfo
```

- [ ] **Step 6: Create parser_test.go**

```go
// server/internal/parser/parser_test.go
package parser_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

func TestTailRead_ReturnsContent(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "session*.jsonl")
	require.NoError(t, err)
	_, err = f.WriteString(`{"type":"message","message":{"role":"assistant","model":"claude-sonnet-4-6"}}` + "\n")
	require.NoError(t, err)
	f.Close()

	content, err := parser.TailRead(f.Name())
	require.NoError(t, err)
	require.Contains(t, content, "claude-sonnet-4-6")
}

func TestTailRead_MissingFile(t *testing.T) {
	_, err := parser.TailRead(filepath.Join(t.TempDir(), "missing.jsonl"))
	require.Error(t, err)
}
```

- [ ] **Step 7: Run tests**

```bash
cd server && go test ./internal/parser/... -v
```
Expected: all tests pass.

- [ ] **Step 8: Commit**

```bash
git add server/internal/parser/
git commit -m "feat(server/parser): add JSONL session parser, encodePath, pricing"
```

---

## Task 9: SSE Broadcaster

**Files:**
- Create: `server/internal/sse/broadcaster.go`
- Create: `server/internal/sse/broadcaster_test.go`

- [ ] **Step 1: Write failing test**

```go
// server/internal/sse/broadcaster_test.go
package sse_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

func TestBroadcaster_SubscribeReceivesData(t *testing.T) {
	b := sse.NewBroadcaster()
	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	b.Broadcast([]byte(`{"test":true}`))

	select {
	case data := <-ch:
		require.Equal(t, `{"test":true}`, string(data))
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout: no data received")
	}
}

func TestBroadcaster_SlowSubscriberDropsFrame(t *testing.T) {
	b := sse.NewBroadcaster()
	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	// Fill buffer (capacity 10) without reading
	for i := 0; i < 15; i++ {
		b.Broadcast([]byte(`x`))
	}
	// Should not block — frames are dropped, not queued
	// Drain what's there
	drained := 0
	for len(ch) > 0 {
		<-ch
		drained++
	}
	require.LessOrEqual(t, drained, 10) // at most buffer capacity
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd server && go test ./internal/sse/... -v
```
Expected: `FAIL` — package does not exist.

- [ ] **Step 3: Create broadcaster.go**

```go
// server/internal/sse/broadcaster.go
package sse

import "sync"

const subscriberBufferSize = 10

// Broadcaster distributes byte slices to all active SSE subscribers.
// Sends are non-blocking: if a subscriber's buffer is full, the frame is
// dropped and the subscriber catches up on the next broadcast.
type Broadcaster struct {
	mu          sync.Mutex
	subscribers map[chan []byte]struct{}
}

// NewBroadcaster creates a ready-to-use Broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		subscribers: make(map[chan []byte]struct{}),
	}
}

// Subscribe returns a channel that will receive broadcast data.
// Call Unsubscribe when the subscriber is done to avoid goroutine leaks.
func (b *Broadcaster) Subscribe() chan []byte {
	ch := make(chan []byte, subscriberBufferSize)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber and closes its channel.
func (b *Broadcaster) Unsubscribe(ch chan []byte) {
	b.mu.Lock()
	delete(b.subscribers, ch)
	b.mu.Unlock()
	close(ch)
}

// Broadcast sends data to all subscribers. Non-blocking: frames are dropped
// for slow consumers rather than blocking the broadcaster.
func (b *Broadcaster) Broadcast(data []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subscribers {
		select {
		case ch <- data:
		default: // subscriber buffer full — drop frame
		}
	}
}

// SubscriberCount returns the current number of active subscribers.
func (b *Broadcaster) SubscriberCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subscribers)
}
```

- [ ] **Step 4: Run tests**

```bash
cd server && go test ./internal/sse/... -v
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/sse/
git commit -m "feat(server/sse): add SSE broadcaster with buffered non-blocking sends"
```

---

## Task 10: Agent Merger

**Files:**
- Create: `server/internal/merger/merger.go`
- Create: `server/internal/merger/merger_test.go`

- [ ] **Step 1: Write failing tests**

```go
// server/internal/merger/merger_test.go
package merger_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/merger"
)

func TestCalculateStatus(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name         string
		lastActivity time.Time
		want         sdk.AgentStatus
	}{
		{"active: 10s ago", now.Add(-10 * time.Second), sdk.AgentStatusActive},
		{"waiting: 2min ago", now.Add(-2 * time.Minute), sdk.AgentStatusWaiting},
		{"idle: 10min ago", now.Add(-10 * time.Minute), sdk.AgentStatusIdle},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := merger.CalculateStatus(tt.lastActivity)
			require.Equal(t, tt.want, got)
		})
	}
}
```

- [ ] **Step 2: Create merger.go**

```go
// server/internal/merger/merger.go
package merger

import (
	"context"
	"path/filepath"
	"time"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
	"github.com/lx-wnk/agent-dashboard/server/internal/scanner"
)

const (
	activeThreshold  = 30 * time.Second
	waitingThreshold = 5 * time.Minute
)

// CalculateStatus returns the agent status based on time since last activity.
func CalculateStatus(lastActivity time.Time) sdk.AgentStatus {
	age := time.Since(lastActivity)
	switch {
	case age < activeThreshold:
		return sdk.AgentStatusActive
	case age < waitingThreshold:
		return sdk.AgentStatusWaiting
	default:
		return sdk.AgentStatusIdle
	}
}

// GetAgents scans running Claude processes and merges them with session data.
func GetAgents(ctx context.Context) ([]sdk.Agent, error) {
	processes, err := scanner.ScanProcesses(ctx)
	if err != nil {
		return nil, err
	}

	agents := make([]sdk.Agent, 0, len(processes))
	for _, proc := range processes {
		session, err := parser.FindSessionForProject(proc.CWD, proc.Uptime)
		if err != nil {
			// Process running but no matching session — skip
			continue
		}

		cost := parser.EstimateCost(session.TokenUsage, session.Model)
		cacheCreate := parser.EstimateCacheCreationCost(session.TokenUsage, session.Model)
		cacheRead := parser.EstimateCacheReadCost(session.TokenUsage, session.Model)

		agent := sdk.Agent{
			PID:                       proc.PID,
			SessionID:                 session.SessionID,
			ProjectPath:               proc.CWD,
			ProjectName:               filepath.Base(proc.CWD),
			CWD:                       proc.CWD,
			Entrypoint:                session.Entrypoint,
			Status:                    CalculateStatus(session.LastActivity),
			Uptime:                    proc.Uptime,
			LastActivity:              session.LastActivity.Format(time.RFC3339),
			CurrentAction:             session.CurrentAction,
			LastTools:                 session.LastTools,
			Tasks:                     session.Tasks,
			Subagents:                 []sdk.SubAgent{},
			TokenUsage:                session.TokenUsage,
			CostEstimate:              cost,
			CacheCreationCostEstimate: cacheCreate,
			CacheReadCostEstimate:     cacheRead,
			Model:                     session.Model,
			ConversationTurns:         session.ConversationTurns,
			ToolCounts:                session.ToolCounts,
			Meta:                      session.Meta,
			ConvergenceAlert:          session.ConvergenceAlert,
			ConvergenceToolName:       session.ConvergenceToolName,
			ErrorState:                session.ErrorState,
		}
		agents = append(agents, agent)
	}
	return agents, nil
}
```

- [ ] **Step 3: Run tests**

```bash
cd server && go test ./internal/merger/... -v
```
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add server/internal/merger/
git commit -m "feat(server/merger): combine process scanner + JSONL parser into Agent list"
```

---

## Task 11: JWT Auth

**Files:**
- Create: `server/internal/auth/jwt.go`
- Create: `server/internal/auth/jwt_test.go`
- Create: `server/internal/auth/middleware.go`

- [ ] **Step 1: Write failing test**

```go
// server/internal/auth/jwt_test.go
package auth_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
)

func TestSignAndVerifyJwt(t *testing.T) {
	secret := "test-secret-32chars-long-minimum!"
	payload := auth.JWTPayload{Sub: "12345", Login: "testuser", IsAdmin: false}

	token, err := auth.SignJWT(payload, secret, 3600)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	got, err := auth.VerifyJWT(token, secret)
	require.NoError(t, err)
	require.Equal(t, "12345", got.Sub)
	require.Equal(t, "testuser", got.Login)
}

func TestVerifyJwt_Expired(t *testing.T) {
	secret := "test-secret-32chars-long-minimum!"
	payload := auth.JWTPayload{Sub: "12345", Login: "testuser"}

	token, err := auth.SignJWT(payload, secret, -1) // expired 1s ago
	require.NoError(t, err)

	_, err = auth.VerifyJWT(token, secret)
	require.Error(t, err)
}

func TestVerifyJwt_WrongSecret(t *testing.T) {
	token, _ := auth.SignJWT(auth.JWTPayload{Sub: "x", Login: "x"}, "secret1", 3600)
	_, err := auth.VerifyJWT(token, "secret2")
	require.Error(t, err)
}
```

- [ ] **Step 2: Create jwt.go**

```go
// server/internal/auth/jwt.go
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// JWTPayload is the JWT body — matches the TypeScript JwtPayload interface.
type JWTPayload struct {
	Sub     string `json:"sub"`     // GitHub numeric user ID
	Login   string `json:"login"`   // GitHub username
	IsAdmin bool   `json:"isAdmin"`
	Exp     int64  `json:"exp"`     // Unix timestamp
}

var ErrTokenInvalid = errors.New("token invalid")
var ErrTokenExpired = errors.New("token expired")

func base64url(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func sign(data, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(data))
	return base64url(mac.Sum(nil))
}

// SignJWT creates an HS256 JWT token. expiresInSeconds is added to now().
func SignJWT(payload JWTPayload, secret string, expiresInSeconds int64) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", fmt.Errorf("marshal header: %w", err)
	}
	payload.Exp = time.Now().Unix() + expiresInSeconds
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}
	h := base64url(header)
	b := base64url(body)
	sig := sign(h+"."+b, secret)
	return h + "." + b + "." + sig, nil
}

// VerifyJWT validates an HS256 JWT and returns the payload.
func VerifyJWT(token, secret string) (JWTPayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return JWTPayload{}, ErrTokenInvalid
	}
	h, b, sig := parts[0], parts[1], parts[2]

	// Verify header
	headerBytes, err := base64.RawURLEncoding.DecodeString(h)
	if err != nil {
		return JWTPayload{}, ErrTokenInvalid
	}
	var header map[string]string
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return JWTPayload{}, ErrTokenInvalid
	}
	if header["alg"] != "HS256" || header["typ"] != "JWT" {
		return JWTPayload{}, ErrTokenInvalid
	}

	// Verify signature (timing-safe)
	expected := sign(h+"."+b, secret)
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return JWTPayload{}, ErrTokenInvalid
	}

	// Decode payload
	bodyBytes, err := base64.RawURLEncoding.DecodeString(b)
	if err != nil {
		return JWTPayload{}, ErrTokenInvalid
	}
	var payload JWTPayload
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		return JWTPayload{}, ErrTokenInvalid
	}
	if time.Now().Unix() > payload.Exp {
		return JWTPayload{}, ErrTokenExpired
	}
	return payload, nil
}
```

- [ ] **Step 3: Create middleware.go**

```go
// server/internal/auth/middleware.go
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

type contextKey string

const payloadKey contextKey = "jwt_payload"

// RequireAuth is a chi middleware that validates the JWT from cookie or
// Authorization header. Unauthenticated requests receive 401.
func RequireAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractToken(r)
			if token == "" {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			payload, err := VerifyJWT(token, secret)
			if err != nil {
				if errors.Is(err, ErrTokenExpired) {
					http.Error(w, `{"error":"token expired"}`, http.StatusUnauthorized)
					return
				}
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), payloadKey, payload)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// PayloadFromContext retrieves the JWT payload stored by RequireAuth.
func PayloadFromContext(ctx context.Context) (JWTPayload, bool) {
	p, ok := ctx.Value(payloadKey).(JWTPayload)
	return p, ok
}

func extractToken(r *http.Request) string {
	// Cookie first (web app uses httpOnly cookie)
	if c, err := r.Cookie("auth_token"); err == nil {
		return c.Value
	}
	// Fallback: Bearer header (API clients, MCP)
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}
```

- [ ] **Step 4: Run tests**

```bash
cd server && go test ./internal/auth/... -v
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/auth/
git commit -m "feat(server/auth): JWT sign/verify (HS256) + chi auth middleware"
```

---

## Task 12: Middleware + Router

**Files:**
- Create: `server/internal/api/middleware.go`
- Create: `server/internal/api/router.go`

- [ ] **Step 1: Create middleware.go**

```go
// server/internal/api/middleware.go
package api

import (
	"log/slog"
	"net/http"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// SlogMiddleware logs each request with method, path, status, duration.
// chi's built-in Logger middleware uses its own format; this uses slog.
func SlogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		next.ServeHTTP(ww, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"duration", time.Since(start),
			"requestID", chimiddleware.GetReqID(r.Context()),
		)
	})
}

// SecurityHeaders sets security-relevant HTTP response headers.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// CORP/COEP for SharedArrayBuffer (used by some WASM deps)
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Cross-Origin-Embedder-Policy", "require-corp")
		next.ServeHTTP(w, r)
	})
}
```

- [ ] **Step 2: Create router.go**

```go
// server/internal/api/router.go
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/agents"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/system"
	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/merger"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

// RouterDeps holds all dependencies injected into the router.
// In Phase 1 this is small; later phases add Orchestrator, PluginManager etc.
type RouterDeps struct {
	Config          RouterConfig
	AgentBroadcaster *sse.Broadcaster
}

// RouterConfig holds router-specific config values.
type RouterConfig struct {
	JWTSecret string
	Embedded  http.FileSystem // Vue SPA embed
}

// NewRouter builds the chi router with all middleware and routes.
func NewRouter(deps RouterDeps) http.Handler {
	r := chi.NewRouter()

	// Global middleware (applied to every request)
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(SlogMiddleware)
	r.Use(chimiddleware.Recoverer)
	r.Use(SecurityHeaders)

	// Public routes (no auth required)
	r.Get("/api/system/health", system.HealthHandler)

	// Protected routes
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(deps.Config.JWTSecret))

		agentHandler := agents.NewHandler(merger.GetAgents, deps.AgentBroadcaster)
		r.Get("/api/agents", ErrorMiddleware(agentHandler.List))
		r.Get("/api/agents/stream", agentHandler.Stream)
	})

	// Vue SPA — must be last (catch-all)
	r.Handle("/*", NewSPAHandler(deps.Config.Embedded))

	return r
}
```

- [ ] **Step 3: Compile check**

```bash
cd server && go build ./internal/api/...
```
Expected: errors about missing packages (`agents`, `system`, `spa`) — that's fine, we add them next.

- [ ] **Step 4: Commit middleware**

```bash
git add server/internal/api/middleware.go server/internal/api/router.go
git commit -m "feat(server/api): add slog middleware, security headers, chi router scaffold"
```

---

## Task 13: Agent Handler + System Handler

**Files:**
- Create: `server/internal/api/agents/handler.go`
- Create: `server/internal/api/system/handler.go`

- [ ] **Step 1: Create agents/handler.go**

```go
// server/internal/api/agents/handler.go
package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

// GetAgentsFn is the function signature for retrieving agents.
// Defined as a type so it can be swapped in tests.
type GetAgentsFn func(ctx context.Context) ([]sdk.Agent, error)

// Handler handles /api/agents requests.
type Handler struct {
	getAgents   GetAgentsFn
	broadcaster *sse.Broadcaster
}

// NewHandler creates a Handler with the given dependencies.
func NewHandler(getAgents GetAgentsFn, broadcaster *sse.Broadcaster) *Handler {
	return &Handler{getAgents: getAgents, broadcaster: broadcaster}
}

// List handles GET /api/agents — returns current agent list as JSON.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) error {
	agents, err := h.getAgents(r.Context())
	if err != nil {
		return fmt.Errorf("get agents: %w", err)
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(agents)
}

// Stream handles GET /api/agents/stream — SSE endpoint.
// Sends the current agent list immediately, then on each broadcast.
func (h *Handler) Stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Send current state immediately so client doesn't wait for first tick
	if agents, err := h.getAgents(r.Context()); err == nil {
		if data, err := json.Marshal(agents); err == nil {
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}

	sub := h.broadcaster.Subscribe()
	defer h.broadcaster.Unsubscribe(sub)

	for {
		select {
		case data, ok := <-sub:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
```

- [ ] **Step 2: Create system/handler.go**

```go
// server/internal/api/system/handler.go
package system

import (
	"encoding/json"
	"net/http"
	"runtime"
	"time"
)

var startTime = time.Now()

// HealthHandler handles GET /api/system/health.
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":   "ok",
		"uptime":   time.Since(startTime).Seconds(),
		"go":       runtime.Version(),
		"platform": runtime.GOOS,
	})
}
```

- [ ] **Step 3: Compile check**

```bash
cd server && go build ./internal/api/...
```
Expected: success (or only missing SPA handler).

- [ ] **Step 4: Commit**

```bash
git add server/internal/api/agents/ server/internal/api/system/
git commit -m "feat(server/api): add agent list + SSE handler, system health endpoint"
```

---

## Task 14: Vue SPA Embedding

**Files:**
- Create: `server/frontend/embed.go`
- Create: `server/frontend/placeholder/dist/index.html`
- Create: `server/internal/api/spa.go`

- [ ] **Step 1: Create placeholder dist for development builds**

```bash
mkdir -p server/frontend/placeholder/dist
```

```html
<!-- server/frontend/placeholder/dist/index.html -->
<!DOCTYPE html>
<html>
<head><title>agent-dashboard</title></head>
<body>
  <p>Run <code>pnpm build</code> at the repo root to build the frontend.</p>
</body>
</html>
```

- [ ] **Step 2: Create frontend/embed.go**

```go
// server/frontend/embed.go
package frontend

import "embed"

// Embedded holds the compiled Vue SPA from the dist/ directory.
// In dev mode (no pnpm build), this falls back to the placeholder.
//
//go:embed dist
var Embedded embed.FS
```

> **Note:** `dist/` must exist at `server/frontend/dist/`. In dev mode with Vite running separately, create a symlink or copy: `ln -s ../../dist server/frontend/dist`. The `.gitignore` should exclude `server/frontend/dist` (the built assets, not the source).

- [ ] **Step 3: Create a fallback dist for compile-time embedding**

```bash
# Copy placeholder as dist so embed.go compiles even without Vue build
cp -r server/frontend/placeholder/dist server/frontend/dist
```

- [ ] **Step 4: Create spa.go**

```go
// server/internal/api/spa.go
package api

import (
	"io/fs"
	"net/http"
)

// NewSPAHandler returns an http.Handler that serves static files from fsys.
// For paths that don't match a file, it serves index.html (for Vue Router
// history mode: all client-side routes fall through to index.html).
func NewSPAHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try opening the requested path
		f, err := fsys.Open(r.URL.Path)
		if err != nil {
			// Not found → serve index.html (Vue Router handles routing)
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/index.html"
			fileServer.ServeHTTP(w, r2)
			return
		}
		f.Close()
		fileServer.ServeHTTP(w, r)
	})
}
```

> **embed.FS and sub-FS:** `embed.FS` paths include the directory prefix (`dist/index.html`). Use `fs.Sub(frontend.Embedded, "dist")` when passing to `NewSPAHandler` to strip the prefix — otherwise paths don't match.

Update router.go:
```go
import (
    "io/fs"
    "github.com/lx-wnk/agent-dashboard/server/frontend"
)

// In NewRouter:
sub, _ := fs.Sub(frontend.Embedded, "dist")
r.Handle("/*", NewSPAHandler(sub))
```

- [ ] **Step 5: Compile check**

```bash
cd server && go build ./...
```
Expected: success.

- [ ] **Step 6: Add dist to gitignore**

```bash
echo "server/frontend/dist/" >> .gitignore
```

- [ ] **Step 7: Commit**

```bash
git add server/frontend/ server/internal/api/spa.go
git commit -m "feat(server): embed Vue SPA via embed.FS with Vue Router history-mode fallback"
```

---

## Task 15: Server + Graceful Shutdown

**Files:**
- Create: `server/internal/api/server.go`

- [ ] **Step 1: Create server.go**

```go
// server/internal/api/server.go
package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/sync/errgroup"
)

const ShutdownTimeout = 10 * time.Second

// Server wraps net/http.Server with graceful shutdown support.
type Server struct {
	httpSrv *http.Server
}

// NewServer creates a Server bound to addr serving handler.
func NewServer(addr string, handler http.Handler) *Server {
	return &Server{
		httpSrv: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second, // prevent Slowloris
		},
	}
}

// Run starts the HTTP server and blocks until ctx is cancelled or an error occurs.
// On ctx cancellation, performs graceful shutdown within ShutdownTimeout.
func (s *Server) Run(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		slog.Info("server starting", "addr", s.httpSrv.Addr)
		if err := s.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("listen: %w", err)
		}
		return nil
	})

	g.Go(func() error {
		<-ctx.Done() // wait for shutdown signal
		slog.Info("server shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
		defer cancel()
		return s.httpSrv.Shutdown(shutdownCtx)
	})

	return g.Wait()
}
```

- [ ] **Step 2: Verify compile**

```bash
cd server && go build ./internal/api/...
```
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add server/internal/api/server.go
git commit -m "feat(server/api): HTTP server with errgroup-based graceful shutdown"
```

---

## Task 16: SSE Tick Loop (Agent Broadcaster)

**Files:**
- Create: `server/internal/agentbroadcast/loop.go`

- [ ] **Step 1: Create agentbroadcast/loop.go**

```go
// server/internal/agentbroadcast/loop.go
package agentbroadcast

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/merger"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

// Run starts a ticker loop that scans agents every interval and broadcasts
// the JSON result to all SSE subscribers. Stops when ctx is cancelled.
func Run(ctx context.Context, broadcaster *sse.Broadcaster, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			agents, err := merger.GetAgents(ctx)
			if err != nil {
				slog.Error("agent scan failed", "err", err)
				continue
			}
			data, err := json.Marshal(agents)
			if err != nil {
				slog.Error("agent marshal failed", "err", err)
				continue
			}
			broadcaster.Broadcast(data)
		case <-ctx.Done():
			return
		}
	}
}
```

- [ ] **Step 2: Commit**

```bash
git add server/internal/agentbroadcast/
git commit -m "feat(server): agent SSE broadcast tick loop"
```

---

## Task 17: Wire DI + Cobra CLI

**Files:**
- Create: `server/cmd/serve/wire.go`
- Create: `server/cmd/serve/wire_gen.go`
- Create: `server/cmd/serve/main.go`

- [ ] **Step 1: Add wire dependency**

```bash
cd server && go get github.com/google/wire@latest
```

- [ ] **Step 2: Create wire.go (DI descriptor)**

```go
//go:build wireinject
// +build wireinject

// server/cmd/serve/wire.go
// This file is the wire input — wire reads it and generates wire_gen.go.
// The wireinject build tag ensures this file is excluded from normal builds.
package main

import (
	"github.com/google/wire"
	"github.com/lx-wnk/agent-dashboard/server/internal/api"
	"github.com/lx-wnk/agent-dashboard/server/internal/config"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

func initializeServer(cfg config.Config) (*api.Server, *sse.Broadcaster, error) {
	wire.Build(
		provideRouterConfig,
		provideRouterDeps,
		api.NewRouter,
		provideServer,
		sse.NewBroadcaster,
	)
	return nil, nil, nil
}
```

- [ ] **Step 3: Create wire_gen.go (manually for now, wire overwrites later)**

```go
// Code generated by Wire. DO NOT EDIT.
// server/cmd/serve/wire_gen.go
package main

import (
	"io/fs"

	"github.com/lx-wnk/agent-dashboard/server/frontend"
	"github.com/lx-wnk/agent-dashboard/server/internal/api"
	"github.com/lx-wnk/agent-dashboard/server/internal/config"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

func initializeServer(cfg config.Config) (*api.Server, *sse.Broadcaster, error) {
	broadcaster := sse.NewBroadcaster()
	routerConfig := provideRouterConfig(cfg)
	routerDeps := provideRouterDeps(routerConfig, broadcaster)
	router := api.NewRouter(routerDeps)
	server := provideServer(cfg, router)
	return server, broadcaster, nil
}

func provideRouterConfig(cfg config.Config) api.RouterConfig {
	sub, _ := fs.Sub(frontend.Embedded, "dist")
	return api.RouterConfig{
		JWTSecret: cfg.JWTSecret,
		Embedded:  http.FS(sub),
	}
}

func provideRouterDeps(cfg api.RouterConfig, b *sse.Broadcaster) api.RouterDeps {
	return api.RouterDeps{Config: cfg, AgentBroadcaster: b}
}

func provideServer(cfg config.Config, handler http.Handler) *api.Server {
	return api.NewServer(cfg.Addr(), handler)
}
```

- [ ] **Step 4: Create main.go**

```go
// server/cmd/serve/main.go
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"github.com/lx-wnk/agent-dashboard/server/internal/agentbroadcast"
	"github.com/lx-wnk/agent-dashboard/server/internal/config"
)

func main() {
	// Configure slog to output structured JSON
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	var cfgFile string

	root := &cobra.Command{
		Use:   "agent-dashboard",
		Short: "Claude Code agent monitoring dashboard",
	}

	serve := &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgFile)
			if err != nil {
				return err
			}

			// Signal context: SIGINT or SIGTERM triggers graceful shutdown
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			srv, broadcaster, err := initializeServer(cfg)
			if err != nil {
				return err
			}

			g, ctx := errgroup.WithContext(ctx)

			// Agent broadcast loop
			interval := time.Duration(cfg.SSEIntervalMs) * time.Millisecond
			g.Go(func() error {
				agentbroadcast.Run(ctx, broadcaster, interval)
				return nil
			})

			// HTTP server
			g.Go(func() error {
				return srv.Run(ctx)
			})

			return g.Wait()
		},
	}
	serve.Flags().StringVar(&cfgFile, "config", "", "path to JSON config file")

	root.AddCommand(serve)

	if err := root.Execute(); err != nil {
		slog.Error("startup failed", "err", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 5: Add missing `net/http` import to wire_gen.go**

```go
import (
    "io/fs"
    "net/http"
    // ... rest
)
```

- [ ] **Step 6: Build and verify binary compiles**

```bash
cd server && go build ./cmd/serve/...
```
Expected: binary created at `server/agent-dashboard` (or `server/cmd/serve/serve`). No errors.

- [ ] **Step 7: Smoke test — start server**

```bash
cd server && go run ./cmd/serve/... serve
```
Expected output:
```json
{"time":"...","level":"INFO","msg":"server starting","addr":"127.0.0.1:13120"}
```
Stop with CTRL+C. Expected:
```json
{"time":"...","level":"INFO","msg":"server shutting down"}
```

- [ ] **Step 8: Test health endpoint**

```bash
curl -s http://127.0.0.1:13120/api/system/health | python3 -m json.tool
```
Expected:
```json
{
  "go": "go1.23.x",
  "platform": "darwin",
  "status": "ok",
  "uptime": 0.001
}
```

- [ ] **Step 9: Commit**

```bash
git add server/cmd/serve/
git commit -m "feat(server): add cobra CLI, wire DI, server entrypoint with graceful shutdown"
```

---

## Task 18: Dev Mode — Vite Proxy Config

**Files:**
- Modify: `vite.config.ts`

- [ ] **Step 1: Update vite.config.ts to proxy API + auth to Go server in dev**

```typescript
// vite.config.ts — add proxy block to existing defineConfig
export default defineConfig({
  // ... existing config ...
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:13120',
        changeOrigin: true,
        ws: true,
      },
      '/auth': {
        target: 'http://127.0.0.1:13120',
        changeOrigin: true,
      },
    },
  },
})
```

- [ ] **Step 2: Verify dev mode works (run both in parallel)**

Terminal 1:
```bash
cd server && go run ./cmd/serve/... serve
```
Terminal 2:
```bash
pnpm dev
```
Open http://localhost:5173 — should serve Vue app with API proxied to Go server.

- [ ] **Step 3: Commit**

```bash
git add vite.config.ts
git commit -m "feat: configure Vite dev proxy to forward /api and /auth to Go server"
```

---

## Task 19: Run Full Test Suite + Lint

- [ ] **Step 1: Run all tests**

```bash
task test
```
Expected: all tests pass across sdk/ and server/ modules.

- [ ] **Step 2: Run linter**

```bash
task lint
```
Fix any reported issues. Common first-run issues:
- `exported function X should have comment` → add `// FunctionName does ...` above each exported function
- `errcheck: Error return value of json.NewEncoder(w).Encode(v) is not checked` → assign to `_ =` or handle

- [ ] **Step 3: Run security scan**

```bash
task vuln
```
Expected: `No vulnerabilities found.` (or review and accept any findings)

- [ ] **Step 4: Commit any lint fixes**

```bash
git add -A
git commit -m "fix: resolve golangci-lint findings (exported comments, errcheck)"
```

---

## Task 20: Integration Smoke Test

- [ ] **Step 1: Start server**

```bash
cd server && go run ./cmd/serve/... serve &
SERVER_PID=$!
sleep 1
```

- [ ] **Step 2: Health check**

```bash
curl -s http://127.0.0.1:13120/api/system/health
```
Expected: JSON with `"status":"ok"`

- [ ] **Step 3: SPA is served**

```bash
curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:13120/
```
Expected: `200`

- [ ] **Step 4: API without auth returns 401**

```bash
curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:13120/api/agents
```
Expected: `401`

- [ ] **Step 5: Stop server**

```bash
kill $SERVER_PID
```

- [ ] **Step 6: Commit final Phase 1 state**

```bash
git add -A
git commit -m "feat: Phase 1 complete — Go server with agent monitoring, SSE, auth, embedded Vue SPA"
```

---

## Self-Review Checklist

### Spec coverage

| Spec section | Task(s) |
|---|---|
| Module structure (go.work, 4 modules) | Task 1 |
| Tooling (Taskfile, golangci-lint, air) | Task 2 |
| CI pipeline | Task 3 |
| SDK types | Task 4 |
| Platform + Config (koanf) | Task 5 |
| Error handling + encode helpers | Task 6 |
| Process scanner (ps/lsof/proc) | Task 7 |
| JSONL parser + pricing | Task 8 |
| SSE broadcaster (buffered, non-blocking) | Task 9 |
| Agent merger (CalculateStatus, GetAgents) | Task 10 |
| JWT auth (HS256, matching TS impl) | Task 11 |
| Middleware (slog, security headers) | Task 12 |
| Agent handler + SSE stream | Task 13 |
| System health endpoint | Task 13 |
| Vue SPA embed.FS + history fallback | Task 14 |
| Graceful shutdown (errgroup + signal) | Task 15 |
| SSE tick loop | Task 16 |
| Wire DI + Cobra serve subcommand | Task 17 |
| Vite proxy (dev mode) | Task 18 |

**Phase 1 deferred (covered in later phases):**
- Full GitHub OAuth flow → Phase 2
- Database (ent + Atlas) → Phase 2
- Pipeline orchestrator → Phase 2
- All 14 route groups (task, keys, hooks, mcp, etc.) → Phase 2 + 3
- TUI → Phase 4
- Plugin system → Phase 4
- Channel bridge → Phase 3
- CTL subcommand → Phase 3

### Type consistency
- `sdk.Agent`, `sdk.TokenUsage`, `sdk.SessionMeta`, `sdk.SubAgent`, `sdk.TaskInfo` defined in Task 4, used in Tasks 8, 10, 13 ✓
- `api.ErrorMiddleware` exported (capital E) in Task 6, used in Task 12 router ✓
- `sse.Broadcaster` created in Task 9, injected in Tasks 13, 16, 17 ✓
- `merger.GetAgents` signature `(ctx context.Context) ([]sdk.Agent, error)` matches `agents.GetAgentsFn` type ✓
- `config.Config.Addr()` returns `"host:port"` string, used in Task 17 server init ✓
