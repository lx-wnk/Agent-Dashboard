# Contributing

Thanks for your interest in contributing to Claude Agent Overview!

## Setup

```bash
# Clone the repository
git clone https://github.com/lx-wnk/Agent-Dashboard.git
cd Agent-Dashboard

# Install dependencies (pnpm workspace — installs root + channel/)
pnpm install

# Start dev server (Express + Vite on port 13120)
pnpm dev
```

**Prerequisites:** Node.js 20+, pnpm, at least one running Claude Code agent for the dashboard to display.

**Platform:** macOS fully supported, Linux mostly supported (CPU monitoring uses `/proc/stat`), Windows not supported.

## Development Commands

```bash
pnpm dev           # Express + Vite with hot reload
pnpm build         # Production build
pnpm lint          # ESLint check
pnpm lint:fix      # ESLint auto-fix
pnpm test          # Vitest unit tests
pnpm test:e2e      # Playwright E2E tests
pnpm typecheck     # vue-tsc type checking
```

## Pull Requests

1. Create a feature branch from `main`
2. Make your changes
3. Ensure `pnpm typecheck`, `pnpm lint`, and `pnpm test` pass
4. Submit a PR with a clear description of what and why

## Architecture

See [README.md](README.md#architecture) for the architecture overview and directory structure.

Key conventions:
- **Path alias:** `@/*` maps to `./src/*`
- **Server:** binds to `127.0.0.1` only — never expose to network
- **No database** — all data from Claude Code's filesystem and running processes
- **Package manager:** pnpm (workspace setup)

## Reporting Issues

Use [GitHub Issues](https://github.com/lx-wnk/Agent-Dashboard/issues/new/choose) to report bugs or request features.
