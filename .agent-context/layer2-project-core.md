# Layer 2 — Project Core

> Development principles and critical project rules.

## Development Principles

> Shared base: @.agent-context/base-principles.md

## Critical Rules

- Server MUST bind to `127.0.0.1` — never `0.0.0.0` (reads sensitive Claude session data)
- Subagents discovered from `~/.claude/projects/{sessionId}/subagents/*.jsonl`
- Agent status thresholds: active < 30s, waiting < 5min, idle > 5min (since last activity)

## Testing Strategy

- **Unit:** Vitest — `npm test` / `npm run test:watch`
- **E2E:** Playwright — `npm run test:e2e` (auto-starts dev server on port 13120)

## Commit Convention

- Conventional Commits: `feat:`, `fix:`, `refactor:`, `init:`
- Feature branches: `feat/*`
- PRs merged to `main`
