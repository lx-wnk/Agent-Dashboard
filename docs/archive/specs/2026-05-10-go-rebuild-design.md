# Go Rebuild Design Spec

**Date:** 2026-05-10
**Branch:** feat/go-rework
**Status:** Approved

## Overview

Full backend rebuild of agent-dashboard in Go. Frontend (Vue 3 + Vite) stays unchanged, embedded into the Go binary via `embed.FS`. Migration strategy: Big Bang — clean new build in `feat/go-rework`, then cut-over. Current TypeScript/Node.js backend is replaced entirely.

Two new user-facing binaries in addition to the server:
1. `agent-dashboard ctl` — server control CLI
2. `agent-dashboard tui` — full terminal UI alternative to the web app (Bubble Tea)

---

## 1. Module Structure

Go workspace (`go.work` committed, `go.work.sum` committed alongside it):

```
agent-dashboard/
├── go.work
├── go.work.sum
├── sdk/                  # github.com/<owner>/agent-dashboard/sdk  ← replace <owner> before init
│   ├── go.mod
│   ├── plugin/           # Plugin interfaces + gRPC shims
│   │   ├── interfaces.go
│   │   ├── grpc.go
│   │   └── manifest.schema.json   # embedded via embed.FS
│   └── types.go          # Shared API types (Agent, Task, StageRun, ...)
│
├── server/               # github.com/<owner>/agent-dashboard/server
│   ├── go.mod
│   ├── cmd/
│   │   ├── serve/main.go
│   │   └── ctl/main.go
│   ├── internal/
│   │   ├── api/          # chi handlers, router, error middleware
│   │   ├── db/
│   │   │   ├── schema/       # ent entity definitions (handwritten)
│   │   │   ├── ent/          # go generate output — never edit manually
│   │   │   ├── migrations/   # Atlas-generated versioned SQL files
│   │   │   └── repo/         # Repository interface implementations
│   │   ├── pipeline/     # orchestrator, stage handlers, types
│   │   ├── scanner/      # ps/lsof process scanning
│   │   ├── parser/       # JSONL session log parser
│   │   ├── plugin/       # plugin manager + registry
│   │   └── sse/          # SSE broadcaster
│   └── frontend/         # embed.FS: built Vue SPA (dist/)
│
├── tui/                  # github.com/<owner>/agent-dashboard/tui
│   ├── go.mod
│   ├── cmd/tui/main.go
│   └── internal/
│       ├── app/          # root model, page routing, keybindings
│       ├── views/        # agentlist, agentdetail, kanban, taskdetail, backlog, settings
│       ├── client/       # HTTP + SSE client against server API
│       └── styles/       # lipgloss styles (central)
│
└── channel/              # github.com/<owner>/agent-dashboard/channel
    ├── go.mod
    └── main.go           # MCP bridge — single-file binary, no internal/ needed (exception to cmd/internal convention)
```

**Key constraint:** `tui` and `server` import from `sdk` only — never from each other. Layer direction enforced via module boundaries.

---

## 2. Tech Stack

| Area | Library | Notes |
|---|---|---|
| HTTP Router | `go-chi/chi` v5 | stdlib-compatible, middleware chain |
| CLI | `spf13/cobra` | subcommands: serve, ctl, tui |
| Config | `knadh/koanf` | replaces viper; preserves key casing |
| ORM | `entgo.io/ent` | schema-first, type-safe, graph-aware |
| Migrations | `entgo.io/ent` + Atlas | versioned SQL files, auto-applied at startup |
| TUI | `github.com/charmbracelet/bubbletea/v2` + lipgloss + bubbles | Elm architecture; v2 stable Feb 2025 |
| Plugin System | `hashicorp/go-plugin` | gRPC-based subprocess isolation |
| MCP | `modelcontextprotocol/go-sdk` v1.x | official, Google-backed |
| DI | `google/wire` | compile-time codegen, no reflection |
| Logging | `log/slog` | stdlib, structured JSON |
| Testing | `testing` + `testify/require` | table-driven test pattern |
| Mocks | `mockery v3` | generates mocks for all interfaces |
| Linting | `golangci-lint` | govet, staticcheck, errcheck, revive, gosec, misspell |
| Live Reload | `air` | Go-aware, `.air.toml` |
| Task Runner | `go-task/task` (Taskfile.yml) | cross-platform |
| Security | `govulncheck` | official Go vuln scanner |
| SQLite Driver | `modernc.org/sqlite` | CGO-free, cross-compile |

---

## 3. Server Architecture

### Request Lifecycle

```
Request → chi Router → Global Middleware → Auth Middleware → Handler → Central Error Middleware → Response
```

### Global Middleware Stack

- `chimiddleware.RequestID`
- `chimiddleware.RealIP`
- `slogMiddleware` (structured request logging)
- `chimiddleware.Recoverer` (panic → 500)
- `securityHeaders` (CORP/COEP)

### Handler Pattern

Handlers return `error` instead of writing directly. A central error middleware maps domain errors to HTTP status codes via `errors.Is` / `errors.As`:

```go
type handlerFunc func(w http.ResponseWriter, r *http.Request) error

func errorHandler(next handlerFunc) http.HandlerFunc { ... }
```

Unknown errors → 500. Sentinel errors (`ErrNotFound`, `ErrConflict`) → mapped status. `*AppError` with explicit status → used as-is.

### SSE

Implemented via `http.Flusher` (stdlib). Broadcaster uses **buffered channels** (capacity 10) per subscriber to prevent slow-consumer blocking.

**Overflow policy:** broadcaster uses non-blocking send (`select { case ch <- data: default: }`). If subscriber buffer is full, the frame is dropped silently — the next tick delivers a fresh snapshot. No subscriber is ever blocked or disconnected due to slowness.

Subscriber lifecycle tied to `r.Context().Done()` — automatic cleanup on client disconnect.

### Vue SPA Embedding

```go
//go:embed dist/*
var Embedded embed.FS
```

SPA handler serves files from `embed.FS`. Unknown paths → `dist/index.html` (Vue Router history mode support).

**Dev mode routing:** Vite dev server runs on `:5173`, Go server on `:13120`. Vite's `vite.config.ts` proxy forwards `/api/**` and `/auth/**` to `:13120`. Browser talks to Vite only — no CORS issue. `air` restarts Go on server changes; Vite HMR handles frontend changes independently.

### Graceful Shutdown

`signal.NotifyContext` + `errgroup.WithContext`. SIGTERM/SIGINT cancels root context → all goroutines (server, orchestrator, SSE broadcaster) drain cleanly.

Shutdown timeout: `const ShutdownTimeout = 10 * time.Second` in `server/internal/api/server.go`. Overridable via `DASHBOARD_SHUTDOWN_TIMEOUT_SECONDS` env var.

---

## 4. Database Layer (ent)

### Entity Schema (examples)

```go
// internal/db/schema/task.go
func (Task) Fields() []ent.Field { ... }
func (Task) Edges() []ent.Edge {
    return []ent.Edge{
        edge.To("stage_runs", StageRun.Type),
        edge.To("permissions", TaskPermission.Type),
        edge.To("dependencies", Task.Type).Through("task_dependencies", TaskDependency.Type),
        edge.From("dependents", Task.Type).Ref("dependencies"),
    }
}
```

### Code Generation

```bash
go generate ./internal/db/ent/...
```

Generated code in `internal/db/ent/` — never modified manually. Committed to VCS.

### Migrations

Atlas generates versioned SQL migration files from ent schema diffs:

```bash
atlas migrate diff <name> \
  --dir "file://internal/db/migrations" \
  --to "ent://internal/db/ent/schema" \
  --dev-url "sqlite://dev?mode=memory"
```

Applied automatically at server startup via `entmigrate.NewSchemaApply` (Atlas embedded as library — no shell-out). Migration history stored in `atlas_schema_revisions` table. On failure, startup aborts with error; no partial migrations. Rollback requires manual SQL (Atlas versioned migration files are the rollback source of truth). New migration files generated during development via the `atlas migrate diff` CLI — not at runtime.

### Repository Pattern

Each domain has a Repository interface. Handlers and pipeline depend on interfaces, not concrete ent queries — enables mockery-generated mocks for unit tests.

### Transactions

`ent.WithTx(ctx, client, func(tx *ent.Tx) error { ... })` — auto-commit on nil, auto-rollback on error.

---

## 5. Pipeline Orchestrator

### State Machine Types

```go
type StageTransition interface{ isTransition() }

type (
    NextTransition         struct{ Stage string; MetadataPatch map[string]any }
    DoneTransition         struct{}
    FailTransition         struct{ Reason string }
    WaitUserTransition     struct{ Reason string }
    IterateTransition      struct{ Feedback string; RejectedOutput string }
    AsyncRunningTransition struct{ PID int; SessionID string }
)
```

Exhaustive switch with `default: panic("unhandled transition")` in `applyTransition`.

### Orchestrator Design

- Tick loop via `time.NewTicker(2s)` in a goroutine, lifetime managed by `errgroup`
- Per-task locking via `sync.Map` of `*sync.Mutex` — `TryLock()` re-entry guard: returns `nil` immediately (skip, not block) if lock is already held by another tick goroutine
- Runner slots cap: `maxParallelOrchestrators` (default 3) from `pipeline_config` table — matches existing config key
- Stage handlers registered in a `map[string]StageHandler` — new stages added without touching orchestrator
- All side effects (SSE broadcast, notification dispatch) via injected callbacks in `OrchestratorOptions` — orchestrator imports nothing from `notifications/` or `routes/`

### Zombie Sweeps

Four sweeps run every tick (in order):
1. `sweepTimedOutRuns` — SIGTERM + fail for running stage_runs past `stageTimeoutSeconds`
2. `sweepAwaitingUserRuns` — dead-PID awaiting_user runs → fail immediately; live runs past `awaitingUserTimeoutSeconds` → SIGTERM + fail
3. `sweepOrphanRuns` — non-terminal stage_runs whose parent task is done/cancelled/on_hold; also reaps pending stage_runs not promoted to running within 5 minutes
4. `sweepLingeringPending` — before `ensureStageRun`, checks if latest stage_run is terminal or zombie awaiting_user with unresolved permission_requests → returns nil without spawning (prevents bulk-resolve cascade spawning a second agent on top of a running one)

### Additional Pipeline Invariants

- **Re-entry guard:** after `ensureStageRun`, checks if returned stage_run is already `status=running` with live PID → returns without calling `handler.Execute` again
- **DB invariant (V7):** partial unique index `ON stage_runs(task_id) WHERE status = 'running'` — enforced at DB level; `SQLITE_CONSTRAINT_UNIQUE` if re-entry guard is bypassed
- `task.blockedByPendingPermissions` computed in `enrichTask` and surfaced in SSE payload so kanban card shows why task is parked

### Orchestrator Lifecycle

```go
g, ctx := errgroup.WithContext(rootCtx)
g.Go(func() error { return orchestrator.Run(ctx) })
g.Go(func() error { return httpServer.ListenAndServe() })
```

---

## 6. TUI (Bubble Tea v2)

### Architecture

Elm: `Init() → (Model, Cmd)` → event loop → `Update(msg) → (Model, Cmd)` → `View() → string`

Import path: `github.com/charmbracelet/bubbletea/v2` (breaking change from v1).
Key v2 differences: `tea.KeyPressMsg` (not `KeyMsg`), `Init()` returns `(tea.Model, tea.Cmd)`.

### Pages

- `pageAgentList` — running agents, status, cost, uptime
- `pageAgentDetail` — tool timeline, tasks, subagents, prompt input
- `pageKanban` — task pipeline kanban (h/j/k/l navigation)
- `pageTaskDetail` — stage output, permissions, feedback
- `pageBacklog` — task creation form (equivalent to BacklogForm.vue)
- `pageSettings` — API key management, notification config, pipeline config

### SSE in TUI

Command chaining: after each SSE frame, `Update()` returns `subscribeToAgentSSE(client)` immediately — continuous stream without goroutine management.

### Shared Types

TUI imports only `sdk.Agent`, `sdk.Task` etc. No direct ent or SQLite dependency. Server serializes ent entities to SDK types for API responses.

### Keybindings

`bubbles/key` for all bindings. Vim-style: `h/j/k/l` navigation, `enter` to open, `esc` to go back, `q` to quit. `bubbles/help` renders contextual key hint bar.

---

## 7. Plugin System

### Capabilities

| Interface | Description |
|---|---|
| `RouteRegistrar` | Register additional HTTP routes (proxied through chi router) |
| `EventSubscriber` | Subscribe to system events (TaskCreated, AgentStarted, StageCompleted, ...) |
| `StageProvider` | Add new pipeline stages |
| `TUIExtension` | Add new TUI views/panels |

### Transport

`hashicorp/go-plugin` gRPC over Unix socket. Plugin crash does not affect host. Handshake version-checks for compatibility.

### Plugin Directory

```
~/.agent-dashboard/plugins/
└── my-plugin/
    ├── plugin           # binary
    └── manifest.json
```

### Manifest

Validated against `manifest.schema.json` (embedded in `sdk/` module via `embed.FS`) at load time using `santhosh-tekuri/jsonschema`. Schema covers: name, version, sdkVersion, capabilities, permissions, config schema, lifecycle hooks.

### Route Namespacing

Plugin routes are mounted under `/api/plugins/{plugin-name}/`. Collision between plugins is prevented by the name-prefix. All plugin routes require the same auth as core API routes (JWT cookie or Bearer token). Plugin manifest `permissions` field declares which scopes the plugin's routes need — enforced by plugin manager at load time.

### Plugin Author Experience

Plugin authors import `sdk/` module, implement desired interfaces, call `plugin.Serve()` in `main()`. No knowledge of gRPC or socket management required.

---

## 8. Channel Bridge (MCP)

Standalone `channel/` binary. Injected into spawned stage agents via `DASHBOARD_MCP_URL`, `DASHBOARD_MCP_TOKEN`, `DASHBOARD_STAGE_RUN_ID`, `DASHBOARD_TASK_ID` env vars.

Implements `request_permission` MCP tool using `modelcontextprotocol/go-sdk`. Forwards permission requests to `POST /api/permission-requests/bulk` on the dashboard server with Bearer auth.

StreamableHTTP transport, random port, port reported to Claude via stdout.

---

## 9. Dependency Injection

`google/wire` compile-time code generation. Wire descriptor in `cmd/serve/wire.go` with `//go:build wireinject`. Generated `wire_gen.go` is committed and human-readable.

Full dependency graph wired in composition root — no runtime reflection, no DI container.

---

## 10. Error Handling

- Wrap at layer boundaries: `fmt.Errorf("db.GetTask: %w", err)`
- Sentinel errors for expected conditions: `var ErrNotFound = errors.New("not found")`
- `errors.Is` for identity checks, `errors.As` for structured error extraction
- Central HTTP error middleware: `errors.As(err, &appErr)` → status; `errors.Is(err, ErrNotFound)` → 404; unknown → 500
- `golang.org/x/sync/errgroup` for concurrent goroutine error aggregation

---

## 11. CI Pipeline (GitHub Actions)

```yaml
on:
  push:    { branches: [main, "feat/**"] }
  pull_request: { branches: [main] }

jobs:
  test:    matrix [server, tui, sdk, channel] — go test -race -coverprofile=coverage.out ./...
  lint:    matrix [server, tui, sdk, channel] — golangci-lint
  security: govulncheck across all modules
  build:   pnpm build (Vue) → go build all binaries
  pr-checks: commitlint + manifest schema validation (PR only)
```

**Coverage enforcement:** `vladopajic/go-test-coverage` action — reads `coverage.out`, fails build if total coverage < 70%. Threshold configurable per module via `.testcoverage.yml`.

`go.work` and `go.work.sum` committed — no `GOWORK=off` needed. CI resolves all modules via workspace, identical to local dev.

---

## 12. Route Scope (Rebuild Coverage)

All existing route surfaces are in-scope for the rebuild unless marked deferred:

| Route group | Status | Notes |
|---|---|---|
| `agentRoutes` | ✅ in-scope | `/api/agents`, `/api/agents/stream` SSE |
| `taskRoutes` | ✅ in-scope | full CRUD + `/api/tasks/stream` SSE |
| `apiKeyRoutes` | ✅ in-scope | `/api/settings/api-keys` |
| `authRoutes` | ✅ in-scope | GitHub OAuth + JWT cookie |
| `hooksRoutes` | ✅ in-scope | `/api/hooks/event` |
| `mcpRouter` | ✅ in-scope | `/api/mcp` StreamableHTTP |
| `systemRoutes` | ✅ in-scope | `/api/system` (health, config) |
| `presetRoutes` | ✅ in-scope | `/api/presets` (permission presets) |
| `webpushRoutes` | ✅ in-scope | `/api/webpush` |
| `historyRoutes` | ✅ in-scope | `/api/history` |
| `memoryRoutes` | ✅ in-scope | `/api/memory` |
| `searchRoutes` | ✅ in-scope | `/api/search` |
| `refineRoutes` | ✅ in-scope | `/api/refine` (agent-based ticket refinement) |
| `remoteRoutes` | ✅ in-scope | `/api/remotes` (multi-machine mode) |

---

## 13. Conventions

- **Test pattern:** Table-driven (`tests := []struct{ ... }{ ... }; for _, tt := range tests { t.Run(tt.name, ...) }`)
- **Mock generation:** `mockery v3` — config-driven, generates mocks for all interfaces automatically
- **`require` over `assert`:** Fail fast on precondition checks
- **No `pkg/`:** Only `cmd/` + `internal/` in each module. `sdk/` module serves as the public API surface.
- **Interfaces:** Defined in consuming package, not providing package (Go idiom)
- **Build tags:** `//go:build wireinject` for wire descriptors; `//go:build integration` for integration tests
- **Linters enabled:** `govet`, `staticcheck`, `errcheck`, `revive`, `gosec`, `misspell`
