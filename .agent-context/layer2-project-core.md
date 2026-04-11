# Layer 2 — Project Core

> Development principles and critical project rules.

## Development Principles

@.agent-context/base-principles.md

## Critical Rules

- Server MUST bind to `127.0.0.1` — never `0.0.0.0` (reads sensitive Claude session data)
- Subagents discovered from `~/.claude/projects/{encoded_path}/{sessionId}/subagents/*.jsonl`
- Agent status thresholds: active < 30s, waiting < 5min, idle > 5min (since last activity)

## Testing Strategy

- **Unit:** Vitest — `pnpm test` / `pnpm test:watch`
- **E2E:** Playwright — `pnpm test:e2e` (auto-starts dev server on port 13120)

## Commit Convention

- Conventional Commits: `feat:`, `fix:`, `refactor:`, `init:`
- Feature branches: `feat/*`
- PRs merged to `main`
