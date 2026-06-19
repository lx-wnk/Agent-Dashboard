# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

From the first tagged release onward, release notes are generated automatically
from [Conventional Commits](https://www.conventionalcommits.org/) by GoReleaser.

## [Unreleased]

Preparing the first public release.

### Added

- Multi-provider session resolution — Codex and Gemini agents resolve their JSONL
  session logs under each provider's own config dir, so foreign CLI agents can surface
  in the roster (cost reported as unknown until a real foreign session schema lands)
  (PR #199).
- In-dashboard `~/.claude` config explorer — browse and edit skills, slash commands,
  and memory files from the UI without leaving the dashboard (PR #190).
- Git worktree panel — create and remove worktrees for pipeline tasks directly from
  the task UI (PR #193).
- Opt-in HTTP hook receiver (`/api/hooks/event`, `/api/hooks/pre-tool`) for
  per-event agent rescans, gated by an auto-generated shared secret — keeps the
  no-hooks default intact.
- Frontend plugin slot framework — named extension points (`refinement`, `settings`,
  and others) that sidecar plugins can mount custom UI into (PR #168).
- Lean, front-door `README.md` and a structured `docs/` tree (configuration, MCP,
  agent control, security, statusline, architecture overview).
- Release tooling: GoReleaser config + `release` workflow producing cross-compiled
  binaries (macOS/Linux, amd64/arm64) with the SPA embedded, plus an `install.sh`
  one-liner installer.
- `agent-dashboard --version` (version injected at build time via ldflags).
- `task setup`, `task doctor`, `task build:frontend`, and `task build:all` to lower
  the contributor entry barrier.
- Community health files: `SECURITY.md`, `CODE_OF_CONDUCT.md`, Dependabot config,
  and `FUNDING.yml`.

### Changed

- Accessibility: clickable agent rows are now native `<button>` elements, and the
  agent-modal summary uses a higher-contrast token.
- SSE poll and retry intervals are centralized in `src/utils/sse.ts` instead of
  being hard-coded at call sites.

### Removed

- Pruned unused TS-era dependencies never imported by the shipped app: `express`,
  `nodemailer`, `web-push`, `cookie-parser`, `supertest`, and their `@types/*`.

### Fixed

- Accessibility: the light-mode `--fg-faint` text token now meets WCAG 2.2 AA
  contrast (4.97:1) on raised surfaces; it previously fell to 4.34:1 on
  `--raised`, below the 4.5:1 threshold at the small sizes used across the UI.
- Accessibility: the login gate now uses a `<main>` landmark and an `<h1>`
  heading, moves keyboard focus to the login control when it appears, and
  announces OAuth failures (`?error=`) via a `role="alert"` region.
- Worktree panel now emits a `change` event after create/remove so the task view
  can react; previously the action ran but the parent was never notified.
- Production build now embeds the real Vue SPA. `vite build` writes to
  `server/frontend/dist` (the `go:embed` source); previously it emitted to the
  repo-root `./dist`, so `task build` silently shipped the placeholder frontend.

### Security

- Resolve all `pnpm audit` advisories: bump `dompurify` to `>=3.4.11` (the only
  production-reachable one) and `vite` to `>=6.4.3`; pin transitive `undici`
  (`^7.28.0`), `esbuild`, `@babel/core`, and `brace-expansion` via workspace
  overrides. `pnpm audit` reports no known vulnerabilities.
- Hardened the live-injection endpoint (`POST /api/agents/{pid}/message`): rate
  limiting, audit logging, per-session token rotation, and control-character
  sanitization (PR #188).

[Unreleased]: https://github.com/lx-wnk/Agent-Dashboard/commits/main
