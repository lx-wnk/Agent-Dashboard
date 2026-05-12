# Development Commands

## Go / Backend (via Taskfile.yml)

```bash
task dev           # Start Go server with air hot-reload + SPA on :13120
task build         # Compile binary → bin/agent-dashboard
task test          # Run all tests (sdk + server, race detector)
task lint          # golangci-lint (sdk + server)
task generate      # Regenerate Wire DI + ent schemas
task fmt           # gofmt all Go code
task wire          # Regenerate Wire DI only
task vuln          # govulncheck
```

**Prerequisites:** Go 1.26+, `task` (`brew install go-task/tap/go-task`), `air` (`go install github.com/air-verse/air@latest`).

## Frontend (pnpm)

```bash
pnpm install       # Install frontend deps (root only — not channel/)
pnpm build         # Production SPA build → server/frontend/dist/
pnpm test          # Vitest unit tests (single run)
pnpm test:e2e      # Playwright E2E tests
pnpm typecheck     # vue-tsc type checking
```

**Package manager:** pnpm. `pnpm install` from root installs frontend dependencies only (Go deps fetched automatically by `go build`/`task build`).
