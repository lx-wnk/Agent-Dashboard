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
```

The server binds exclusively to `127.0.0.1`. Never change this — the server reads sensitive Claude session data.

Frontend path alias: `@/*` maps to `./src/*`.

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
