# Layer 2 — Project Core

> Development principles and critical project rules.

## Development Principles

@.agent-context/base-principles.md

## Critical Rules

- Server MUST bind to `127.0.0.1` — never `0.0.0.0` (reads sensitive Claude session data)
- Subagents discovered from `~/.claude/projects/{encoded_path}/{sessionId}/subagents/*.jsonl`
- Agent status thresholds: `activeThreshold` (30s) and `waitingThreshold` (5min) in `server/internal/merger/merger.go` (`CalculateStatus`); idle is the default case beyond `waitingThreshold` (no const)
- **Keep project docs current with code.** Whenever a change adds, removes, or alters a user-facing feature, dependency, command, config, or workflow, update the affected docs in the SAME change — at minimum `README.md`, `CHANGELOG.md` (Keep a Changelog headings), and `CONTRIBUTING.md`, plus any other touched file (`docs/`, `PRIVACY.md`, `SECURITY.md`, `THIRD_PARTY_LICENSES.md`). Stale docs are a defect, not a follow-up. Verify every doc claim against actual code before writing it.

## Single Source of Truth (SSOT)

**Rule:** Every constant, regex, validation rule, or type that is used in more than one place MUST live in exactly one canonical location. Duplication between implementation branches is forbidden.

**Canonical locations by category:**

| Category | Location | Example |
|---|---|---|
| Shared validation (client + server) | `src/utils/validation.ts` | `SLUG_RE`, `SLUG_PATTERN_MESSAGE`, `slugify()` |
| Shared type constants | `src/types.ts` | `AGENT_STATUSES`, `AgentStatus` |
| Shared UI utilities | `src/utils/format.ts`, `src/utils/agentSort.ts`, `src/utils/sse.ts` | `formatCost`, `STATUS_ORDER`, `SSE_RETRY_DELAY_MS` |
| Shared model/config lists | `src/utils/models.ts` | `AVAILABLE_MODELS` |
| Server-only defaults | `server/db/defaults.ts` | `DEFAULT_STAGE_TIMEOUT_SECONDS` |
| Server-only constants | `server/constants.ts` | `VALID_STAGES`, `MAX_DESCRIPTION_CHARS` (re-exports from `src/utils/` where shared) |
| Server path/host constants | `server/paths.ts`, `server/constants.ts` | `WHITESPACE_RE`, `LOOPBACK_HOST` |
| Pipeline stage labels | `src/utils/stageLabels.ts` | `STAGE_LABELS`, `STAGE_DESCRIPTIONS` |

**Server consuming shared `src/` code:** Import via relative path with `.js` extension — e.g. `import { SLUG_RE } from '../src/utils/validation.js'`. `server/constants.ts` re-exports these so server-internal consumers don't need the long path.

**How to apply:** Before adding any constant or utility function, grep the codebase for existing implementations. If one already exists, import it — never copy it. If adding a new shared value, put it in the canonical location first, then import everywhere.

## Testing Strategy

- **Unit:** Vitest — `pnpm test` / `pnpm test:watch`
- **E2E:** Playwright — `pnpm test:e2e` (auto-starts dev server on port 13120)

## Commit Convention

- Conventional Commits: `feat:`, `fix:`, `refactor:`, `init:`
- Feature branches: `feat/*`
- PRs merged to `main`
