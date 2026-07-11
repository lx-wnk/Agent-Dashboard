# Contributing to agent-dashboard

Real-time monitoring dashboard for locally running Claude Code agents. Go 1.26 backend, Vue 3 + TypeScript frontend.

## Prerequisites

| Tool | Install |
|------|---------|
| Go 1.26+ | `brew install go` |
| Task runner | `brew install go-task/tap/go-task` |
| air (hot-reload) | `go install github.com/air-verse/air@latest` |
| Node.js 22+ + pnpm | [pnpm.io/installation](https://pnpm.io/installation) |

**Platform:** macOS and Linux. Windows is unsupported.

Building the macOS desktop shell (`desktop/`) additionally requires Xcode command-line tools (`xcode-select --install`). The `wails` CLI (`go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0`) is only needed later for `.app`/`.dmg` bundling — not for a dev build. See [Desktop shell (macOS)](#desktop-shell-macos) below.

## Setup

```bash
git clone https://github.com/lx-wnk/Agent-Dashboard.git
cd Agent-Dashboard
pnpm install        # frontend dependencies only
```

Go dependencies are fetched automatically on first build. The project uses a Go workspace (`go.work`) with two modules: `./sdk` and `./server`.

## Running the dev server

```bash
task dev
```

Starts the Go backend via `air` (hot-reload on `.go` file changes) and serves the Vue SPA, both on port 13120.

To re-trigger the first-run onboarding flow (e.g. to test it) once you've already completed or skipped it, reset its setting: `dashboard settings set onboarding.completed false`.

## Production build

```bash
pnpm build          # build the Vue SPA (embedded into the binary via go:embed)
task build          # compile → bin/agent-dashboard
DASHBOARD_JWT_SECRET=<32+ chars> ./bin/agent-dashboard serve
```

Build the SPA **before** the binary — `go:embed` bakes the compiled frontend into `bin/agent-dashboard`, so the result is self-contained (no Node.js at runtime). Deploy by copying the binary. End users do not build from source; see [docs/guides/install.md](docs/guides/install.md) for binary/Homebrew/Docker installs.

## Commands

### Backend (Go)

| Command | Description |
|---------|-------------|
| `task dev` | Start with air hot-reload |
| `task build` | Compile production binary → `bin/agent-dashboard` |
| `task test` | Run all tests with race detector |
| `task lint` | Run golangci-lint |
| `task generate` | Run ent schema + tygo TS code generation |
| `task fmt` | Format with gofmt |

### Frontend (Vue)

| Command | Description |
|---------|-------------|
| `pnpm build` | Build Vue SPA for production |
| `pnpm test` | Vitest unit tests |
| `pnpm test:e2e` | Playwright end-to-end tests |
| `pnpm typecheck` | vue-tsc type checking |

## Architecture

```
server/cmd/serve/main.go          Go entrypoint (cobra CLI + hand-written DI)
server/internal/api/router.go     Chi router and route registration
server/internal/pipeline/         Task pipeline state machine
server/internal/db/ent/schema/    Ent ORM schemas
sdk/                              dashboard-channel MCP stdio binary (Go module)
src/                              Vue 3 TypeScript SPA
plugins/anthropic-spawner/        Out-of-process Anthropic Messages API binary (own go.mod)
```

### Out-of-process spawner pattern

`plugins/anthropic-spawner/` is its own Go module, built with `GOWORK=off` so that `anthropic-sdk-go` is never imported by the server. The server invokes it through the standard custom-exec contract (stdin: `LLMSpawnArgs` JSON; stdout: `LLMSpawnResult` JSON). A `Stream` flag on `LLMSpawnArgs` selects between a single-shot response (`Spawn`) and server-sent events (`SpawnStream`). New out-of-process spawners follow the same contract.

The server binds exclusively to `127.0.0.1`. Never change this — the server reads sensitive Claude session data.

Frontend path alias: `@/*` maps to `./src/*`.

### Adding a provider

For a CLI that writes file-per-session JSONL, add a descriptor YAML under `server/internal/provider/providers/` (or ship one via `DASHBOARD_PROVIDER_DIR`) — no Go code is needed. The descriptor declares the exe names, config dir, session glob, token/model/cost field-paths, and the token aggregation mode (`cumulative` or `perMessage`).

IDE-embedded tools (Cursor, Copilot-in-VSCode, Windsurf) don't write file-per-session JSONL, so they need a Go `Adapter` (source `custom:<id>`) instead — not yet implemented.

### Desktop shell (macOS)

`desktop/` is its own Go module (wails v2) that wraps the dashboard server in a native WKWebView window. It is `//go:build darwin`-gated — the module does not build on other platforms; a `main_other.go` stub keeps `go build ./...` and CI green off macOS. Build and run it for a dev smoke (requires Go 1.26 + Xcode command-line tools, no `wails` CLI needed):

```bash
task desktop:run        # or: task desktop:build  (writes bin/agent-dashboard-desktop)
```

Three non-obvious build requirements the `desktop:*` tasks encapsulate — a bare `cd desktop && go build .` runs but produces a broken app:

- **`-tags production`** — without a `production` or `dev` build tag, wails compiles a fallback that errors at runtime (`Wails applications will not build without the correct build tags`).
- **`-ldflags "-extldflags '-framework UniformTypeIdentifiers'"`** — wails v2 references `UTType` on the macOS 15 SDK; a plain `go build` fails to link it (`Undefined symbols: _OBJC_CLASS_$_UTType`).
- **the SPA must be built first** (`task build:frontend`) — the shell starts the dashboard server in-process, and that server (not the shell) embeds and serves `server/frontend/dist` via `go:embed`; an empty dist makes the webview hit a `/` → `./` redirect loop instead of the app.

The `wails` CLI (`go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0`) is only needed later, for producing a signed `.app`/`.dmg` bundle — not for this dev build.

## Pull Request Process

1. Branch from `main`.
2. Use feature branch naming: `feat/<short-description>`.
3. Before submitting, verify:
   - `task test` passes (race detector included)
   - `task lint` passes
   - `pnpm typecheck` passes
4. Write a clear PR description explaining what changed and why.

## Commit Convention

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add worktree cleanup on task cancel
fix: correct SSE reconnect delay after server restart
refactor: extract cost estimation into shared util
```

All commit messages must be written in English.

## Code Guidelines

- Keep functions small and focused on a single responsibility.
- Every constant or validation rule used in more than one place lives in exactly one canonical location — do not duplicate.
- Server must never bind to `0.0.0.0`.
- New side effects (metrics, notifications, new channels) → add optional callback to the relevant Options struct, wire in `server/cmd/serve/`. Do not import side-effect modules from inside `server/internal/pipeline/`.

## Reporting Issues

Use [GitHub Issues](https://github.com/lx-wnk/Agent-Dashboard/issues/new/choose) to report bugs or request features.
